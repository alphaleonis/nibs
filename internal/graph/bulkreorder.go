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

// reorderChildrenImpl is the shared core of ReorderChildren: validate inputs,
// optionally pre-check per-child ETags, assign fresh evenly-spaced order keys
// to each listed child, persist them, and return the children in the
// requested order.
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
		// pointer: a mid-loop Update rejection must not leave the
		// failing item's shared in-memory nib showing a phantom order that was
		// never persisted.
		clone, err := r.Reader.GetForUpdate(b.ID)
		if err != nil {
			return nil, err
		}
		clone.Order = keys[i]
		if err := r.Writer.Update(clone, ifMatchPtr(etagByID, b.ID)); err != nil {
			// Name the nib, exactly as the pre-validation refusal does
			// (validateIfMatchETags). A racing write's raw ETagMismatchError reads
			// "etag mismatch: provided X, current is Y" and names nothing, so the
			// currentEtag reconcile token it carries is unattributable in a bulk
			// mutation — the client cannot tell which of the listed nibs it holds a
			// token for. Wrapping with %w keeps errors.As routing intact.
			return nil, fmt.Errorf("failed to reorder %s: %w", b.ID, err)
		}
		// The write installed the clone as the new c.nibs[id]; reflect it in the
		// returned slice so callers see the persisted order.
		ordered[i] = clone
	}
	// Each element is now the live store pointer Writer.Update installed — return
	// detached snapshots so no live c.nibs pointer escapes to gqlgen's async
	// marshaler (see the canonical live-pointer / copy-on-write invariant at
	// NibReader.GetSnapshot).
	return r.snapshotResults(ordered)
}

// reorderSiblingsImpl is the shared core of ReorderSiblings: validate inputs,
// optionally pre-check per-sibling ETags, compute the destination order keys
// (based on the anchor and direction), and persist the block in its requested
// order.
func (r *mutationResolver) reorderSiblingsImpl(siblingIDs []string, afterID *string, beforeID *string, first *bool, ifMatch []*model.ChildEtag) ([]*nib.Nib, error) {
	block, pos, anchor, parentID, err := r.validateBulkSiblings(siblingIDs, afterID, beforeID, first)
	if err != nil {
		return nil, err
	}

	etagByID, err := r.validateIfMatchETags(block, ifMatch, r.requireIfMatch())
	if err != nil {
		return nil, err
	}

	// Sorted siblings excluding the block — preserves the on-disk order of
	// non-moved siblings so we can find the upper/lower bound around the
	// anchor without colliding with about-to-be-moved keys.
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

	// Determine the [lower, upper] range the new block keys go into.
	var lower, upper string
	switch pos.kind {
	case posFirst:
		lower = ""
		if len(others) > 0 {
			upper = others[0].Order
		}
	case posBefore:
		// Block goes immediately before the anchor — anchor is the upper
		// bound; lower bound is the previous non-moved sibling (or empty).
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
		// Block goes immediately after the anchor — anchor is the lower bound;
		// upper bound is the next non-moved sibling (or empty).
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

	// Generate keys strictly between lower and upper, one per block member.
	prev := lower
	for i, b := range block {
		newKey := nib.OrderBetween(prev, upper)
		// Mutate an OWNED clone from GetForUpdate, never the shared Reader.Get
		// pointer: a mid-loop Update rejection must not leave the
		// failing item's shared in-memory nib showing a phantom order that was
		// never persisted.
		clone, err := r.Reader.GetForUpdate(b.ID)
		if err != nil {
			return nil, err
		}
		clone.Order = newKey
		if err := r.Writer.Update(clone, ifMatchPtr(etagByID, b.ID)); err != nil {
			// Same identification contract as the reorderChildren arm above: a
			// racing mismatch must name its nib so its currentEtag is attributable.
			return nil, fmt.Errorf("failed to reorder %s: %w", b.ID, err)
		}
		// The write installed the clone as the new c.nibs[id]; reflect it in the
		// returned slice so callers see the persisted order.
		block[i] = clone
		prev = newKey
	}
	// Each element is now the live store pointer Writer.Update installed — return
	// detached snapshots so no live c.nibs pointer escapes to gqlgen's async
	// marshaler (see the canonical live-pointer / copy-on-write invariant at
	// NibReader.GetSnapshot).
	return r.snapshotResults(block)
}

// ifMatchPtr returns a pointer to the pre-validated etag for id, or nil
// when id isn't covered by ifMatch. The helper exists because:
//  1. Under require_if_match: true, nibcore.Update rejects nil — so a
//     nil ifMatch from validateIfMatchETags would fail the per-nib write
//     even after we've already verified the etag is current.
//  2. Threading the pre-validated etag avoids a second on-disk read
//     inside Update (which would re-hash the file we just hashed).
//
// Removing this helper would either re-introduce that redundant read or
// break the require_if_match: true bulk path.
func ifMatchPtr(etagByID map[string]string, id string) *string {
	if etagByID == nil {
		return nil
	}
	if e, ok := etagByID[id]; ok {
		return &e
	}
	return nil
}

// validateBulkSiblings resolves and validates a Mode B request. Returns the
// listed nibs in the requested order (the block to move), the resolved
// position, the resolved anchor (nil for First), the inferred parent ID, or
// an error.
func (r *mutationResolver) validateBulkSiblings(siblingIDs []string, afterID *string, beforeID *string, first *bool) ([]*nib.Nib, Position, *nib.Nib, string, error) {
	pos, err := PositionFromArgs(afterID, beforeID, first)
	if err != nil {
		return nil, Position{}, nil, "", err
	}

	// Resolve the listed siblings. Existence check runs before duplicate
	// detection so we can dedupe on the canonical (resolved) ID — this
	// catches cases where the same nib is passed in two surface forms (e.g.
	// "a" and "nibs-a" under a configured prefix).
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
		// Group on the resolved parent (see resolvedParentID) so the block matches
		// the sibling set Members enumerates below.
		bParentID := resolvedParentID(b, r.Reader)
		if i == 0 {
			parentID = bParentID
		} else if bParentID != parentID {
			return nil, Position{}, nil, "", fmt.Errorf("siblings span multiple parents: %s has parent %s, expected %q",
				id, describeParent(b, bParentID), parentID)
		}
		block = append(block, b)
	}

	// Resolve the anchor (only required for the anchored forms).
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
		// Block membership is checked against resolved IDs so short/full
		// forms in the input both surface as an error.
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
// children and returns them in the requested order. Order of checks:
// parent-existence -> per-child existence -> duplicate (on canonical ID) ->
// parent-membership -> completeness. Duplicate detection runs after existence
// resolution so the same nib passed in two surface forms (e.g. "a" and
// "nibs-a" under a prefix) is caught.
func (r *mutationResolver) validateBulkChildren(parentID string, childIDs []string) ([]*nib.Nib, error) {
	// Validate the parent exists (empty parentID is the root sentinel and
	// requires no lookup). Without this, a typo'd parent with empty childIDs
	// would silently succeed as a no-op. The fetched id replaces the supplied
	// one so membership below compares two resolved ids, the way the sibling
	// set is enumerated.
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
		// Membership uses the resolved parent (see resolvedParentID) so it draws
		// the same set the completeness loop below reads out of the ordering
		// surface. If the two disagreed, a project holding a nib whose parent link
		// names no nib could neither list that nib in a root-level reorder
		// (rejected here) nor omit it (rejected as missing there).
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

// notFoundDetail formats an unresolved-id error fragment, including the
// canonical form when normalization changed it (so the user can see what
// the lookup actually attempted).
func notFoundDetail(raw, canonical string) string {
	if raw == canonical {
		return raw
	}
	return fmt.Sprintf("%s (resolved to %s)", raw, canonical)
}

// describeParent formats a nib's parent for a same-parent error, naming the
// resolved value that the comparison actually used and the stored spelling when
// they differ. A dangling link is where the two part company: the file says
// `parent: nibs-ghost` while the comparison sees a root, and reporting only the
// resolved "" leaves nothing in .nibs/ to grep for.
func describeParent(b *nib.Nib, resolved string) string {
	if b.Parent == resolved {
		return fmt.Sprintf("%q", b.Parent)
	}
	return fmt.Sprintf("%q (resolves to %q)", b.Parent, resolved)
}

// requireIfMatch reports whether the project config requires ifMatch on
// every write. When true, bulk reorder must receive an ifMatch entry for
// every listed nib.
func (r *mutationResolver) requireIfMatch() bool {
	cfg := r.Reader.Config()
	return cfg != nil && cfg.Nibs.RequireIfMatch
}

// validateIfMatchETags checks the supplied per-child ETag entries against the
// listed nibs. Validation order:
//  1. duplicate-id check (canonicalized) — first duplicate aborts.
//  2. unknown-id check — every ifMatch entry must reference one of `listed`.
//  3. completeness check (when requireIfMatch is true) — every listed nib
//     must appear in the ifMatch map.
//  4. per-entry etag mismatch check — first mismatch aborts before any write.
//
// listed is the set of nibs that will be written. ifMatch is optional when
// requireIfMatch is false: nibs without entries skip step 4 entirely. No
// writes occur from this method — callers run it before Writer.Update.
//
// Returns a canonical-id -> etag map so the caller can thread the
// pre-validated etag through to Writer.Update; under require_if_match: true
// Writer.Update would otherwise reject the call for a missing etag.
func (r *mutationResolver) validateIfMatchETags(listed []*nib.Nib, ifMatch []*model.ChildEtag, requireIfMatch bool) (map[string]string, error) {
	if len(ifMatch) == 0 && !requireIfMatch {
		return nil, nil
	}

	// Build a canonical-id -> etag map. The id in the entry may be a short
	// form under a configured prefix; normalize both sides so cross-form
	// duplicates and unknown ids surface correctly.
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

	// Reject any ifMatch entry that doesn't correspond to a listed nib.
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

	// Pre-validate etags against on-disk content. First mismatch aborts; no
	// writes have happened yet so the operation is atomic.
	for _, b := range listed {
		want, ok := etags[b.ID]
		if !ok {
			continue
		}
		current, err := r.Reader.CurrentETag(b.ID)
		if err != nil {
			// An uncertifiable on-disk file (unparseable/unreadable) surfaces the
			// distinct, NON-RECONCILABLE OnDiskUnparseableError carrying no etag
			// token — mirroring Update's fail-closed path. Propagate it
			// (wrapped, so errors.As still finds it) rather than collapsing it into a
			// reconcilable "etag mismatch" the client could retry past.
			var unparseable *nibcore.OnDiskUnparseableError
			if errors.As(err, &unparseable) {
				return nil, fmt.Errorf("failed to reorder %s: %w", b.ID, err)
			}
			return nil, fmt.Errorf("failed to read current etag for %s: %w", b.ID, err)
		}
		if current != want {
			// The TYPED, reconcilable conflict — the same error a racing per-nib
			// write raises — wrapped only to name the offending nib. Both surfaces
			// that route a conflict structurally read it with errors.As: the wire
			// code extensions.code = "ETAG_MISMATCH" (cmd/serve.go) and the CLI's
			// CONFLICT exit status with its currentEtag reconcile token (cmd/set.go
			// for single-nib writes, cmd/mv.go for the bulk arms).
			// A bare fmt.Errorf here would present the COMMON bulk-reorder conflict —
			// a caller whose ifMatch was already stale on entry — as a generic
			// failure that only message text could identify.
			return nil, fmt.Errorf("failed to reorder %s: %w", b.ID,
				&nibcore.ETagMismatchError{Provided: want, Current: current})
		}
	}
	return etags, nil
}
