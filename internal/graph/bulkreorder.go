package graph

import (
	"fmt"

	"github.com/alphaleonis/nibs/internal/nib"
)

// reorderChildrenImpl is the shared core of ReorderChildren: validate inputs,
// assign fresh evenly-spaced order keys to each listed child, persist them,
// and return the children in the requested order.
func (r *mutationResolver) reorderChildrenImpl(parentID string, childIDs []string) ([]*nib.Nib, error) {
	if err := r.checkBulkReorderSupported(); err != nil {
		return nil, err
	}

	ordered, err := r.validateBulkChildren(parentID, childIDs)
	if err != nil {
		return nil, err
	}

	keys := nib.OrderKeyN(len(ordered))
	for i, b := range ordered {
		b.Order = keys[i]
		// Bulk reorder skips ifMatch in v1 (deferred to nibs-n3zb). Under
		// require_if_match: true, all bulk writes would fail; the entry-point
		// validates this via checkBulkReorderSupported before issuing writes.
		if err := r.Writer.Update(b, nil); err != nil {
			return nil, err
		}
	}
	return ordered, nil
}

// reorderSiblingsImpl is the shared core of ReorderSiblings: validate inputs,
// compute the destination order keys (based on the anchor and direction), and
// persist the block in its requested order.
func (r *mutationResolver) reorderSiblingsImpl(siblingIDs []string, afterID *string, beforeID *string, first *bool) ([]*nib.Nib, error) {
	if err := r.checkBulkReorderSupported(); err != nil {
		return nil, err
	}

	block, anchor, parentID, err := r.validateBulkSiblings(siblingIDs, afterID, beforeID, first)
	if err != nil {
		return nil, err
	}

	// Sorted siblings excluding the block — preserves the on-disk order of
	// non-moved siblings so we can find the upper/lower bound around the
	// anchor without colliding with about-to-be-moved keys.
	allSiblings := r.sortedSiblings(parentID)
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
	switch {
	case first != nil && *first:
		lower = ""
		if len(others) > 0 {
			upper = others[0].Order
		}
	case beforeID != nil:
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
	default: // afterID
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
	for _, b := range block {
		newKey := nib.OrderBetween(prev, upper)
		b.Order = newKey
		// Bulk reorder skips ifMatch in v1 (deferred to nibs-n3zb). Under
		// require_if_match: true, all bulk writes would fail; the entry-point
		// validates this via checkBulkReorderSupported before issuing writes.
		if err := r.Writer.Update(b, nil); err != nil {
			return nil, err
		}
		prev = newKey
	}
	return block, nil
}

// validateBulkSiblings resolves and validates a Mode B request. Returns the
// listed nibs in the requested order (the block to move), the resolved anchor
// (nil for first=true), the inferred parent ID, or an error.
func (r *mutationResolver) validateBulkSiblings(siblingIDs []string, afterID *string, beforeID *string, first *bool) ([]*nib.Nib, *nib.Nib, string, error) {
	// Mode mutex: exactly one of afterID/beforeID/first.
	hasAfter := afterID != nil && *afterID != ""
	hasBefore := beforeID != nil && *beforeID != ""
	hasFirst := first != nil && *first
	count := 0
	if hasAfter {
		count++
	}
	if hasBefore {
		count++
	}
	if hasFirst {
		count++
	}
	if count != 1 {
		if count == 0 {
			return nil, nil, "", fmt.Errorf("exactly one of afterId, beforeId, first must be specified (none given)")
		}
		return nil, nil, "", fmt.Errorf("exactly one of afterId, beforeId, first must be specified (got %d)", count)
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
			return nil, nil, "", fmt.Errorf("sibling nib not found: %s", notFoundDetail(id, normalizedID))
		}
		if _, dup := seen[b.ID]; dup {
			return nil, nil, "", fmt.Errorf("duplicate id in sibling list: %s (resolved to %s)", id, b.ID)
		}
		seen[b.ID] = struct{}{}
		if i == 0 {
			parentID = b.Parent
		} else if b.Parent != parentID {
			return nil, nil, "", fmt.Errorf("siblings span multiple parents: %s has parent %q, expected %q",
				id, b.Parent, parentID)
		}
		block = append(block, b)
	}

	// Resolve the anchor (only required when not first=true).
	var anchor *nib.Nib
	if hasAfter || hasBefore {
		anchorID := ""
		if hasAfter {
			anchorID = *afterID
		} else {
			anchorID = *beforeID
		}
		normalizedAnchor, _ := r.Reader.NormalizeID(anchorID)
		a, err := r.Reader.Get(normalizedAnchor)
		if err != nil {
			return nil, nil, "", fmt.Errorf("anchor nib not found: %s", notFoundDetail(anchorID, normalizedAnchor))
		}
		if a.Parent != parentID {
			return nil, nil, "", fmt.Errorf("anchor %s is not a sibling (parent=%q, expected %q)",
				anchorID, a.Parent, parentID)
		}
		// Block membership is checked against resolved IDs so short/full
		// forms in the input both surface as an error.
		for _, b := range block {
			if b.ID == a.ID {
				return nil, nil, "", fmt.Errorf("anchor %s (resolved to %s) must not appear in siblingIds", anchorID, a.ID)
			}
		}
		anchor = a
	}

	return block, anchor, parentID, nil
}

// sortedSiblings returns the children of parentID (or root-level siblings if
// parentID == "") sorted by order key. Wraps the Orderer's two distinct entry
// points so callers can pass parentID uniformly.
func (r *mutationResolver) sortedSiblings(parentID string) []*nib.Nib {
	if parentID == "" {
		return r.Orderer.getRootSiblings()
	}
	return r.Orderer.GetSortedSiblings(parentID)
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
	// would silently succeed as a no-op.
	if parentID != "" {
		if _, err := r.Reader.Get(parentID); err != nil {
			return nil, fmt.Errorf("parent nib not found: %s", parentID)
		}
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
		if b.Parent != parentID {
			return nil, fmt.Errorf("nib %s is not a child of %s (parent=%q)", id, parentID, b.Parent)
		}
		ordered = append(ordered, b)
		requested[b.ID] = struct{}{}
	}

	for _, b := range r.sortedSiblings(parentID) {
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

// checkBulkReorderSupported returns a directed error when the project is
// configured with require_if_match: true. Bulk reorder does not yet propagate
// per-nib etags through the GraphQL surface (deferred to nibs-n3zb); without
// this guard the first Writer.Update would fail with a generic "if-match etag
// is required" error that gives the user no path forward.
func (r *mutationResolver) checkBulkReorderSupported() error {
	cfg := r.Reader.Config()
	if cfg != nil && cfg.Nibs.RequireIfMatch {
		return fmt.Errorf("bulk reorder does not yet support require_if_match: true (tracked in nibs-n3zb); use single-nib `nibs reorder <id> --if-match <etag>` per nib in the meantime")
	}
	return nil
}
