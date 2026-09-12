package graph

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
)

// reorderChildrenImpl validates the inputs, optionally pre-checks per-child
// ETags, then assigns fresh evenly-spaced order keys to each listed child and
// persists them, returning the children in the requested order.
func (r *mutationResolver) reorderChildrenImpl(parentID string, childIDs []string, ifMatch []*model.ChildEtag) ([]*nib.Nib, error) {
	ordered, err := r.validateBulkChildren(parentID, childIDs)
	if err != nil {
		return nil, err
	}

	etagByID, err := r.validateIfMatchETags(ordered, ifMatch, r.requireIfMatch())
	if err != nil {
		return nil, err
	}

	keys := nib.OrderKeyN(len(ordered))
	for i, b := range ordered {
		// Mutate an OWNED clone from GetForUpdate, never the shared Reader.Get
		// pointer: a mid-loop Update rejection must not leave the failing item's
		// shared in-memory nib showing an order that was never persisted.
		clone, err := r.Reader.GetForUpdate(b.ID)
		if err != nil {
			return nil, err
		}
		clone.Order = keys[i]
		if err := r.Writer.Update(clone, ifMatchPtr(etagByID, b.ID)); err != nil {
			// Name the nib, as the pre-validation refusal does: a racing
			// ETagMismatchError reads "etag mismatch: provided X, current is Y"
			// and names nothing, so the currentEtag token it carries is
			// unattributable in a bulk mutation. %w keeps errors.As routing intact.
			return nil, fmt.Errorf("failed to reorder %s: %w", b.ID, err)
		}
		// The write installed the clone as the new c.nibs[id]; return that.
		ordered[i] = clone
	}
	// Detached snapshots: no live c.nibs pointer may escape to gqlgen's async
	// marshaler (see NibReader.GetSnapshot).
	return r.snapshotResults(ordered)
}

// reorderSiblingsImpl validates the inputs, optionally pre-checks per-sibling
// ETags, then computes the destination order keys from the anchor and direction
// and persists the block in its requested order.
func (r *mutationResolver) reorderSiblingsImpl(siblingIDs []string, afterID *string, beforeID *string, first *bool, ifMatch []*model.ChildEtag) ([]*nib.Nib, error) {
	block, pos, anchor, parentID, err := r.validateBulkSiblings(siblingIDs, afterID, beforeID, first)
	if err != nil {
		return nil, err
	}

	etagByID, err := r.validateIfMatchETags(block, ifMatch, r.requireIfMatch())
	if err != nil {
		return nil, err
	}

	// The non-moved siblings, in order — the block's own keys are about to be
	// overwritten, so the bounds around the anchor come from here.
	allSiblings := r.Orderer.Members(ScopeParent, parentID)
	blockSet := make(map[string]struct{}, len(block))
	for _, b := range block {
		blockSet[b.ID] = struct{}{}
	}
	others := make([]*nib.Nib, 0, len(allSiblings))
	for _, s := range allSiblings {
		if _, ok := blockSet[s.ID]; !ok {
			others = append(others, s)
		}
	}

	var lower, upper string
	switch pos.kind {
	case posFirst:
		lower = ""
		if len(others) > 0 {
			upper = others[0].Order
		}
	case posBefore:
		upper = anchor.Order
		for i, s := range others {
			if s.ID == anchor.ID {
				if i > 0 {
					lower = others[i-1].Order
				}
				break
			}
		}
	case posAfter:
		lower = anchor.Order
		for i, s := range others {
			if s.ID == anchor.ID {
				if i+1 < len(others) {
					upper = others[i+1].Order
				}
				break
			}
		}
	}

	prev := lower
	for i, b := range block {
		newKey := nib.OrderBetween(prev, upper)
		// An OWNED clone, and a nib-naming error wrap, for the reasons given in
		// reorderChildrenImpl above.
		clone, err := r.Reader.GetForUpdate(b.ID)
		if err != nil {
			return nil, err
		}
		clone.Order = newKey
		if err := r.Writer.Update(clone, ifMatchPtr(etagByID, b.ID)); err != nil {
			return nil, fmt.Errorf("failed to reorder %s: %w", b.ID, err)
		}
		block[i] = clone
		prev = newKey
	}
	// Detached snapshots: no live c.nibs pointer may escape to gqlgen's async
	// marshaler (see NibReader.GetSnapshot).
	return r.snapshotResults(block)
}

// ifMatchPtr returns a pointer to the pre-validated etag for id, or nil when id
// isn't covered by ifMatch. Under require_if_match: true nibcore.Update rejects
// a nil ifMatch, so the per-nib write needs the etag threaded through even
// though validateIfMatchETags has already checked it.
func ifMatchPtr(etagByID map[string]string, id string) *string {
	if etagByID == nil {
		return nil
	}
	if e, ok := etagByID[id]; ok {
		return &e
	}
	return nil
}

// validateBulkSiblings resolves a bulk sibling move: the listed nibs in the
// requested order (the block to move), the resolved position, the resolved
// anchor (nil for First), and the parent inferred from the block.
func (r *mutationResolver) validateBulkSiblings(siblingIDs []string, afterID *string, beforeID *string, first *bool) ([]*nib.Nib, Position, *nib.Nib, string, error) {
	pos, err := PositionFromArgs(afterID, beforeID, first)
	if err != nil {
		return nil, Position{}, nil, "", err
	}

	// Existence resolution runs before duplicate detection so duplicates are
	// caught on the canonical id ("a" and "nibs-a" are one nib).
	block := make([]*nib.Nib, 0, len(siblingIDs))
	seen := make(map[string]struct{}, len(siblingIDs))
	var parentID string
	for i, id := range siblingIDs {
		normalizedID, _ := r.Reader.NormalizeID(id)
		b, err := r.Reader.Get(normalizedID)
		if err != nil {
			return nil, Position{}, nil, "", fmt.Errorf("sibling nib not found: %s", notFoundDetail(id, normalizedID))
		}
		if _, dup := seen[b.ID]; dup {
			return nil, Position{}, nil, "", fmt.Errorf("duplicate id in sibling list: %s (resolved to %s)", id, b.ID)
		}
		seen[b.ID] = struct{}{}
		// Group on the RESOLVED parent, the set Members enumerates below.
		bParentID := resolvedParentID(b, r.Reader)
		if i == 0 {
			parentID = bParentID
		} else if bParentID != parentID {
			return nil, Position{}, nil, "", fmt.Errorf("siblings span multiple parents: %s has parent %s, expected %q",
				id, describeParent(b, bParentID), parentID)
		}
		block = append(block, b)
	}

	var anchor *nib.Nib
	if pos.kind == posAfter || pos.kind == posBefore {
		anchorID := pos.anchor
		normalizedAnchor, _ := r.Reader.NormalizeID(anchorID)
		a, err := r.Reader.Get(normalizedAnchor)
		if err != nil {
			return nil, Position{}, nil, "", fmt.Errorf("anchor nib not found: %s", notFoundDetail(anchorID, normalizedAnchor))
		}
		if aParentID := resolvedParentID(a, r.Reader); aParentID != parentID {
			return nil, Position{}, nil, "", fmt.Errorf("anchor %s is not a sibling (parent=%s, expected %q)",
				anchorID, describeParent(a, aParentID), parentID)
		}
		for _, b := range block {
			if b.ID == a.ID {
				return nil, Position{}, nil, "", fmt.Errorf("anchor %s (resolved to %s) must not appear in siblingIds", anchorID, a.ID)
			}
		}
		anchor = a
	}

	return block, pos, anchor, parentID, nil
}

// validateBulkChildren resolves the child IDs against the parent's current
// children and returns them in the requested order. Check order:
// parent-existence -> per-child existence -> duplicate (on canonical ID) ->
// parent-membership -> completeness.
func (r *mutationResolver) validateBulkChildren(parentID string, childIDs []string) ([]*nib.Nib, error) {
	// "" is the root sentinel and needs no lookup. Without this check a typo'd
	// parent with empty childIDs succeeds as a silent no-op. The fetched id
	// replaces the supplied one so membership compares two resolved ids.
	if parentID != "" {
		parent, err := r.Reader.Get(parentID)
		if err != nil {
			return nil, fmt.Errorf("parent nib not found: %s", parentID)
		}
		parentID = parent.ID
	}

	ordered := make([]*nib.Nib, 0, len(childIDs))
	requested := make(map[string]struct{}, len(childIDs))
	for _, id := range childIDs {
		normalizedID, _ := r.Reader.NormalizeID(id)
		b, err := r.Reader.Get(normalizedID)
		if err != nil {
			return nil, fmt.Errorf("child nib not found: %s", notFoundDetail(id, normalizedID))
		}
		if _, dup := requested[b.ID]; dup {
			return nil, fmt.Errorf("duplicate id in reorder list: %s (resolved to %s)", id, b.ID)
		}
		// Membership uses the RESOLVED parent (resolvedParentID), the same set
		// the completeness loop below reads out of the ordering surface.
		if bParentID := resolvedParentID(b, r.Reader); bParentID != parentID {
			return nil, fmt.Errorf("nib %s is not a child of %s (parent=%s)",
				id, parentID, describeParent(b, bParentID))
		}
		ordered = append(ordered, b)
		requested[b.ID] = struct{}{}
	}

	for _, b := range r.Orderer.Members(ScopeParent, parentID) {
		if _, ok := requested[b.ID]; !ok {
			return nil, fmt.Errorf("missing child in reorder list: %s", b.ID)
		}
	}

	return ordered, nil
}

// notFoundDetail formats an unresolved-id error fragment, adding the canonical
// form when normalization changed it.
func notFoundDetail(raw, canonical string) string {
	if raw == canonical {
		return raw
	}
	return fmt.Sprintf("%s (resolved to %s)", raw, canonical)
}

// describeParent formats a nib's parent for a same-parent error: the resolved
// value the comparison used, plus the stored spelling when a dangling link
// makes the two differ.
func describeParent(b *nib.Nib, resolved string) string {
	if b.Parent == resolved {
		return fmt.Sprintf("%q", b.Parent)
	}
	return fmt.Sprintf("%q (resolves to %q)", b.Parent, resolved)
}

// requireIfMatch reports whether the project config requires ifMatch on every
// write.
func (r *mutationResolver) requireIfMatch() bool {
	cfg := r.Reader.Config()
	return cfg != nil && cfg.Nibs.RequireIfMatch
}

// validateIfMatchETags checks the supplied per-child ETag entries against the
// listed nibs — the ones that will be written — in order: duplicate id
// (canonicalized), unknown id, completeness (when requireIfMatch), per-entry
// mismatch. The first failure aborts, and no writes happen here: callers run it
// before Writer.Update.
//
// ifMatch is optional when requireIfMatch is false; nibs without an entry skip
// the mismatch check. Returns a canonical-id -> etag map for ifMatchPtr.
func (r *mutationResolver) validateIfMatchETags(listed []*nib.Nib, ifMatch []*model.ChildEtag, requireIfMatch bool) (map[string]string, error) {
	if len(ifMatch) == 0 && !requireIfMatch {
		return nil, nil
	}

	// Normalize both sides: an entry id may be a short form, and a cross-form
	// duplicate still collides.
	etags := make(map[string]string, len(ifMatch))
	for _, e := range ifMatch {
		if e == nil {
			continue
		}
		canonical, _ := r.Reader.NormalizeID(e.ID)
		if _, dup := etags[canonical]; dup {
			return nil, fmt.Errorf("duplicate id in ifMatch: %s (resolved to %s)", e.ID, canonical)
		}
		etags[canonical] = e.Etag
	}

	listedSet := make(map[string]*nib.Nib, len(listed))
	for _, b := range listed {
		listedSet[b.ID] = b
	}

	for canonical := range etags {
		if _, ok := listedSet[canonical]; !ok {
			return nil, fmt.Errorf("ifMatch references nib not in this reorder: %s", canonical)
		}
	}

	if requireIfMatch {
		if len(etags) == 0 {
			return nil, fmt.Errorf("require_if_match: true but no ifMatch provided; supply an entry for each listed nib")
		}
		var missing []string
		for _, b := range listed {
			if _, ok := etags[b.ID]; !ok {
				missing = append(missing, b.ID)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			return nil, fmt.Errorf("require_if_match: true but ifMatch is missing entries for: %s", strings.Join(missing, ", "))
		}
	}

	for _, b := range listed {
		want, ok := etags[b.ID]
		if !ok {
			continue
		}
		current, err := r.Reader.CurrentETag(b.ID)
		if err != nil {
			// An uncertifiable on-disk file surfaces OnDiskUnparseableError,
			// which carries no etag token and is NOT reconcilable. Propagate it
			// (wrapped, so errors.As still finds it) rather than collapsing it
			// into an "etag mismatch" the client could retry past.
			var unparseable *nibcore.OnDiskUnparseableError
			if errors.As(err, &unparseable) {
				return nil, fmt.Errorf("failed to reorder %s: %w", b.ID, err)
			}
			return nil, fmt.Errorf("failed to read current etag for %s: %w", b.ID, err)
		}
		if current != want {
			// The TYPED, reconcilable conflict — the same error a racing per-nib
			// write raises — wrapped only to name the nib. Both surfaces read it
			// with errors.As: extensions.code = "ETAG_MISMATCH" on the wire
			// (cmd/serve.go), and the CLI's CONFLICT exit with its currentEtag
			// reconcile token (cmd/set.go, cmd/mv.go). A bare fmt.Errorf would
			// leave only message text to identify it by.
			return nil, fmt.Errorf("failed to reorder %s: %w", b.ID,
				&nibcore.ETagMismatchError{Provided: want, Current: current})
		}
	}
	return etags, nil
}
