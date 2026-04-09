package graph

import (
	"fmt"

	"github.com/alphaleonis/nibs/internal/nib"
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
func (o *Orderer) getRootSiblings() []*nib.Nib {
	all := o.reader.All()
	var roots []*nib.Nib
	for _, b := range all {
		if b.Parent == "" {
			roots = append(roots, b)
		}
	}
	o.backfillOrderKeys(roots)
	nib.SortByOrder(roots)
	return roots
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
// At most one of afterID, beforeID, first may be specified.
// For child nibs: if none specified, inserts last among siblings of same priority.
// For root nibs: always appends last among other root-level nibs. Explicit positioning
// flags (afterId/beforeId/first) are not supported on create — use ReorderNib to
// reposition root nibs after creation.
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

	// Root-level nibs: compute position among other root-level nibs
	if b.Parent == "" {
		if count > 0 {
			return fmt.Errorf("positioning requires a parent")
		}
		rootSiblings := o.getRootSiblings()
		if len(rootSiblings) == 0 {
			b.Order = nib.OrderInitial()
		} else {
			b.Order = nib.OrderLast(rootSiblings[len(rootSiblings)-1].Order)
		}
		return nil
	}

	// Get sorted siblings
	siblings := o.GetSortedSiblings(b.Parent)

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

	// Default: insert last among siblings of same priority
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

	// Assign keys to unordered nibs, appending after the last ordered one
	for _, b := range nibs {
		if b.Order != "" {
			continue
		}
		newKey := nib.OrderBetween(lastKey, "")

		// Compute ETag BEFORE mutation so it matches the on-disk version
		etag := b.ETag()
		b.Order = newKey
		lastKey = newKey

		// Best-effort persist: ordering falls back to title sort if this fails
		_ = o.writer.Update(b, &etag)
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
			if s.Parent != b.Parent {
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
			if s.Parent != b.Parent {
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
	if b.Parent == "" {
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

	siblings := o.GetSortedSiblings(b.Parent)
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
