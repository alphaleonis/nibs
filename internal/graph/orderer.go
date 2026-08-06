package graph

import (
	"errors"
	"fmt"
	"os"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
)

// Orderer handles nib ordering operations with only read/write dependencies.
type Orderer struct {
	reader NibReader
	writer NibWriter
}

// NewOrderer creates an Orderer with the given reader and writer.
func NewOrderer(reader NibReader, writer NibWriter) *Orderer {
	return &Orderer{reader: reader, writer: writer}
}

// getRootSiblings returns all nibs with no parent, sorted by order key.
// Backfills order keys on any unordered root nibs before sorting.
//
// Root-ness comes from resolvedParentID, so the root set here is the same one
// the query surfaces report.
func (o *Orderer) getRootSiblings() []*nib.Nib {
	all := o.reader.All()
	var roots []*nib.Nib
	for _, b := range all {
		if resolvedParentID(b, o.reader) == "" {
			roots = append(roots, b)
		}
	}
	o.backfillOrderKeys(roots)
	nib.SortByOrder(roots)
	return roots
}

// siblingsForParent returns the ordered sibling set under parentID — the root
// nibs when it is empty, otherwise that parent's children. parentID must be a
// RESOLVED parent id (see resolvedParentID); a raw stored field would send a
// dangling link down the children branch, forming a sibling group of one keyed
// on a nib that does not exist.
func (o *Orderer) siblingsForParent(parentID string) []*nib.Nib {
	if parentID == "" {
		return o.getRootSiblings()
	}
	return o.GetSortedSiblings(parentID)
}

// siblingsOf returns the ordered sibling set b belongs to (see
// resolvedParentID).
func (o *Orderer) siblingsOf(b *nib.Nib) []*nib.Nib {
	return o.siblingsForParent(resolvedParentID(b, o.reader))
}

// sameParent reports whether x and y sit in the same sibling set (see
// resolvedParentID). Two nibs whose parent links both name no nib are roots,
// and so siblings of each other and of every genuine root, rather than a pair
// keyed on a phantom parent.
func (o *Orderer) sameParent(x, y *nib.Nib) bool {
	return resolvedParentID(x, o.reader) == resolvedParentID(y, o.reader)
}

// GetSortedSiblings returns all children of parentID, sorted by order key.
// Backfills order keys on any unordered siblings before sorting.
func (o *Orderer) GetSortedSiblings(parentID string) []*nib.Nib {
	incoming := o.reader.FindIncomingLinks(parentID)
	var siblings []*nib.Nib
	for _, link := range incoming {
		if link.LinkType == "parent" {
			siblings = append(siblings, link.FromNib)
		}
	}
	o.backfillOrderKeys(siblings)
	nib.SortByOrder(siblings)
	return siblings
}

// ApplyPositioning computes an order key for a new nib based on positioning flags.
// At most one of afterID, beforeID, first may be specified. The flags work for
// both root-level and child nibs (siblings are looked up among other roots when
// b has no parent, otherwise among that parent's children).
// When no flag is given: child nibs are inserted last among siblings of the same
// priority; root nibs are appended last (no priority-aware positioning).
func (o *Orderer) ApplyPositioning(b *nib.Nib, afterID, beforeID *string, first *bool) error {
	hasAfter := afterID != nil && *afterID != ""
	hasBefore := beforeID != nil && *beforeID != ""
	hasFirst := first != nil && *first

	// Count how many positioning flags are set
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
	if count > 1 {
		return fmt.Errorf("at most one of afterId, beforeId, first may be specified")
	}

	// Look up siblings uniformly: roots when b has no parent, else children of
	// its parent. Both the root/child choice and the child lookup key come from
	// the resolved parent (see resolvedParentID).
	parentID := resolvedParentID(b, o.reader)
	siblings := o.siblingsForParent(parentID)

	if len(siblings) == 0 {
		b.Order = nib.OrderInitial()
		return nil
	}

	if hasAfter {
		return o.positionAfter(b, *afterID, siblings)
	}
	if hasBefore {
		return o.positionBefore(b, *beforeID, siblings)
	}
	if hasFirst {
		b.Order = nib.OrderFirst(siblings[0].Order)
		return nil
	}

	// Default — no positioning flag.
	// Root nibs: append last (no priority-aware positioning, matching RecalculateOrder).
	// Child nibs: insert last among siblings of the same priority.
	if parentID == "" {
		b.Order = nib.OrderLast(siblings[len(siblings)-1].Order)
		return nil
	}
	return o.positionDefaultByPriority(b, siblings)
}

// backfillOrderKeys assigns order keys to siblings that lack them.
// Unordered nibs are appended after the last ordered sibling.
func (o *Orderer) backfillOrderKeys(nibs []*nib.Nib) {
	if len(nibs) == 0 {
		return
	}

	needsBackfill := false
	for _, b := range nibs {
		if b.Order == "" {
			needsBackfill = true
			break
		}
	}
	if !needsBackfill {
		return
	}

	// Sort for stable baseline (ordered first by key, unordered by title)
	nib.SortByOrder(nibs)

	// Find the last existing order key
	lastKey := ""
	for _, b := range nibs {
		if b.Order != "" && b.Order > lastKey {
			lastKey = b.Order
		}
	}

	// Assign keys to unordered nibs, appending after the last ordered one.
	for i := range nibs {
		b := nibs[i]
		if b.Order != "" {
			continue
		}
		newKey := nib.OrderBetween(lastKey, "")

		// Compute ETag BEFORE mutation so it matches the on-disk version.
		etag := b.ETag()
		lastKey = newKey

		// Mutate an OWNED clone from GetForUpdate, never the shared reader pointer
		// (b is c.nibs[id]): a refused write must not leave the shared in-memory
		// sibling showing a phantom Order that was never persisted.
		// GetForUpdate fails only not-found: the sibling was deleted between the
		// snapshot above and here (a concurrent external/`serve` delete). It's gone,
		// so there is nothing to backfill — quietly skip it (not a write failure, and
		// the nib no longer exists, so no warning is warranted).
		clone, err := o.reader.GetForUpdate(b.ID)
		if err != nil {
			continue
		}
		clone.Order = newKey

		// Best-effort persist: ordering falls back to title sort if this fails.
		// backfillOrderKeys runs on the hot Children/root READ path (once per
		// parent per tree render/poll), and a persistently unwritable sibling
		// keeps Order=="" so needsBackfill never clears — meaning this Update is
		// re-attempted on EVERY read. Classify the error so a steady-state
		// failure does not flood stderr under a long-running `nibs serve`:
		//   - *ETagMismatchError: a stable on-disk etag divergence (e.g. a
		//     hand-authored nib missing an order key AND both timestamps, whose
		//     synthesized-from-mtime in-memory etag permanently differs from the
		//     stored one). This is the already-accepted best-effort fallback — the
		//     failed clone's computed key is DISCARDED (nibs[i] keeps its pre-write,
		//     Order=="" pointer), so the sibling falls back to title sort; the write
		//     simply cannot land. Stay quiet.
		//   - *OnDiskUnparseableError: the file is corrupt/unreadable. Suppressing
		//     our OWN warning here avoids the orderer emitting a line per read on the
		//     hot Children/root path; the condition is still surfaced where it
		//     matters — at the write/pre-validation boundary (cmd/update.go →
		//     FILE_ERROR, bulk-reorder pre-validation). nibcore.computeStoredETag now
		//     RETURNS this error instead of logging it, so suppressing here means the
		//     read path emits no warning at all (no orderer line, no nibcore
		//     double-log, no flood).
		// Warn only on a genuinely unexpected write failure (disk I/O, etc.) so a
		// real problem stays diagnosable (matches activateParentChain's stderr
		// warning). Propagating is not an option: getRootSiblings/GetSortedSiblings
		// return no error and have many callers, so this stays best-effort.
		if err := o.writer.Update(clone, &etag); err != nil {
			var etagMismatch *nibcore.ETagMismatchError
			var unparseable *nibcore.OnDiskUnparseableError
			if !errors.As(err, &etagMismatch) && !errors.As(err, &unparseable) {
				fmt.Fprintf(os.Stderr, "warning: could not backfill order key for %s: %v — this sibling stays unordered (falls back to title sort) until the next successful write\n", b.ID, err)
			}
			continue
		}
		// The write installed the clone as the new c.nibs[id]; reflect the
		// persisted order in the returned slice without touching the pre-write
		// pointer.
		nibs[i] = clone
	}
}

// positionAfter places b after the target sibling.
func (o *Orderer) positionAfter(b *nib.Nib, targetID string, siblings []*nib.Nib) error {
	normalizedID, ok := o.reader.NormalizeID(targetID)
	if !ok {
		return fmt.Errorf("sibling nib not found: %s", targetID)
	}
	targetID = normalizedID
	for i, s := range siblings {
		if s.ID == targetID {
			// Defensive: every production caller passes a sibling slice already
			// filtered by parent (getRootSiblings or GetSortedSiblings). This guard
			// fires only for direct unit tests that hand-build a mixed list.
			if !o.sameParent(s, b) {
				return fmt.Errorf("nib %s is not a sibling (different parent)", targetID)
			}
			// Find the next sibling with a different order key to get a real boundary.
			// Duplicate keys (from legacy data) would cause OrderBetween to produce
			// a key that collides with the nib's current order.
			nextKey := ""
			for j := i + 1; j < len(siblings); j++ {
				if siblings[j].Order != s.Order {
					nextKey = siblings[j].Order
					break
				}
			}
			b.Order = nib.OrderBetween(s.Order, nextKey)
			return nil
		}
	}
	// Target was resolved (exists) but not in the sibling list — that means
	// it has a different parent. Surface a clearer error than "not found".
	if t, err := o.reader.Get(targetID); err == nil && !o.sameParent(t, b) {
		return fmt.Errorf("nib %s is not a sibling (different parent)", targetID)
	}
	return fmt.Errorf("sibling nib not found: %s", targetID)
}

// positionBefore places b before the target sibling.
func (o *Orderer) positionBefore(b *nib.Nib, targetID string, siblings []*nib.Nib) error {
	normalizedID, ok := o.reader.NormalizeID(targetID)
	if !ok {
		return fmt.Errorf("sibling nib not found: %s", targetID)
	}
	targetID = normalizedID
	for i, s := range siblings {
		if s.ID == targetID {
			// Defensive: every production caller passes a sibling slice already
			// filtered by parent (getRootSiblings or GetSortedSiblings). This guard
			// fires only for direct unit tests that hand-build a mixed list.
			if !o.sameParent(s, b) {
				return fmt.Errorf("nib %s is not a sibling (different parent)", targetID)
			}
			// Find the previous sibling with a different order key to get a real boundary.
			// Duplicate keys (from legacy data) would cause OrderBetween to produce
			// a key that collides with the nib's current order.
			prevKey := ""
			for j := i - 1; j >= 0; j-- {
				if siblings[j].Order != s.Order {
					prevKey = siblings[j].Order
					break
				}
			}
			b.Order = nib.OrderBetween(prevKey, s.Order)
			return nil
		}
	}
	// Target was resolved (exists) but not in the sibling list — that means
	// it has a different parent. Surface a clearer error than "not found".
	if t, err := o.reader.Get(targetID); err == nil && !o.sameParent(t, b) {
		return fmt.Errorf("nib %s is not a sibling (different parent)", targetID)
	}
	return fmt.Errorf("sibling nib not found: %s", targetID)
}

// positionDefaultByPriority inserts b last among siblings of the same or higher priority.
func (o *Orderer) positionDefaultByPriority(b *nib.Nib, siblings []*nib.Nib) error {
	cfg := o.reader.Config()

	newRank := cfg.PriorityRank(b.Priority)

	// Find the last sibling with priority >= new nib's priority (rank <= newRank)
	insertAfterIdx := -1
	for i, s := range siblings {
		if cfg.PriorityRank(s.Priority) <= newRank {
			insertAfterIdx = i
		}
	}

	if insertAfterIdx == -1 {
		// All siblings have lower priority — insert first
		b.Order = nib.OrderFirst(siblings[0].Order)
	} else if insertAfterIdx == len(siblings)-1 {
		// Insert after the last sibling
		b.Order = nib.OrderLast(siblings[insertAfterIdx].Order)
	} else {
		// Insert between insertAfterIdx and insertAfterIdx+1
		b.Order = nib.OrderBetween(siblings[insertAfterIdx].Order, siblings[insertAfterIdx+1].Order)
	}
	return nil
}

// RecalculateOrder assigns a new order key to b based on its current parent.
// Root-level nibs are appended last (no priority-aware positioning, matching
// ApplyPositioning behavior — use ReorderNib to reposition).
// Child nibs are positioned last among siblings of the same priority.
func (o *Orderer) RecalculateOrder(b *nib.Nib) {
	parentID := resolvedParentID(b, o.reader)
	if parentID == "" {
		rootSiblings := o.getRootSiblings()
		// Exclude self from siblings
		filtered := make([]*nib.Nib, 0, len(rootSiblings))
		for _, s := range rootSiblings {
			if s.ID != b.ID {
				filtered = append(filtered, s)
			}
		}
		if len(filtered) == 0 {
			b.Order = nib.OrderInitial()
		} else {
			b.Order = nib.OrderLast(filtered[len(filtered)-1].Order)
		}
		return
	}

	siblings := o.GetSortedSiblings(parentID)
	// Exclude self from siblings
	filtered := make([]*nib.Nib, 0, len(siblings))
	for _, s := range siblings {
		if s.ID != b.ID {
			filtered = append(filtered, s)
		}
	}

	if len(filtered) == 0 {
		b.Order = nib.OrderInitial()
		return
	}

	// Use priority-aware positioning (same as create default).
	_ = o.positionDefaultByPriority(b, filtered)
}
