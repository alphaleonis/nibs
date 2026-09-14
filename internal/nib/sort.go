package nib

import (
	"cmp"
	"slices"
	"sort"
	"strings"
)

// SortByOrder sorts nibs by their Order field lexicographically.
// Nibs with an order key come first; nibs without one are appended sorted by title.
func SortByOrder(nibs []*Nib) {
	SortByKey(nibs, func(n *Nib) string { return n.Order })
}

// SortByMilestoneOrder sorts nibs by MilestoneOrder with SortByOrder's semantics.
// Use it for a milestone's queue; Order is a nib's structural position.
func SortByMilestoneOrder(nibs []*Nib) {
	SortByKey(nibs, func(n *Nib) string { return n.MilestoneOrder })
}

// SortByKey sorts nibs stably by key, with SortByOrder's semantics.
func SortByKey(nibs []*Nib, key func(*Nib) string) {
	slices.SortStableFunc(nibs, func(a, b *Nib) int {
		return CompareByKey(a, b, key)
	})
}

// CompareByKey is SortByKey's comparison: keyed before unkeyed, then key, then
// case-insensitive title, then ID. Use it to sort slices that wrap nibs.
func CompareByKey(a, b *Nib, key func(*Nib) string) int {
	aKey, bKey := key(a), key(b)
	aHas := aKey != ""
	bHas := bKey != ""

	switch {
	case aHas && bHas:
		if c := cmp.Compare(aKey, bKey); c != 0 {
			return c
		}
		// Tiebreaker for equal order keys: sort by title, then ID
		if c := cmp.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	case aHas:
		return -1 // a (ordered) before b (unordered)
	case bHas:
		return 1 // b (ordered) before a (unordered)
	default:
		if c := cmp.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	}
}

// PriorityRanker maps a priority string to its sort rank.
// Used by SortByStatusPriorityAndType to decouple from config package.
type PriorityRanker interface {
	PriorityRank(priority string) int
}

// SortByStatusPriorityAndType sorts nibs by status order, priority rank, type
// order (EffectiveType), then case-insensitive title. Statuses and types missing
// from statusNames or typeNames sort last.
func SortByStatusPriorityAndType(nibs []*Nib, statusNames, typeNames []string, ranker PriorityRanker) {
	less := LessByStatusPriorityAndType(statusNames, typeNames, ranker)
	sort.Slice(nibs, func(i, j int) bool { return less(nibs[i], nibs[j]) })
}

// LessByStatusPriorityAndType returns the comparison SortByStatusPriorityAndType
// sorts by, for slices that wrap nibs. It builds the order lookups once, so call
// it outside the sort.
func LessByStatusPriorityAndType(statusNames, typeNames []string, ranker PriorityRanker) func(a, b *Nib) bool {
	statusOrder := make(map[string]int)
	for i, s := range statusNames {
		statusOrder[s] = i
	}
	typeOrder := make(map[string]int)
	for i, t := range typeNames {
		typeOrder[t] = i
	}

	getStatusOrder := func(status string) int {
		if order, ok := statusOrder[status]; ok {
			return order
		}
		return len(statusNames) // Unrecognized statuses come last
	}
	getTypeOrder := func(typ string) int {
		if order, ok := typeOrder[typ]; ok {
			return order
		}
		return len(typeNames) // Unrecognized types come last
	}

	return func(a, b *Nib) bool {
		oi, oj := getStatusOrder(a.Status), getStatusOrder(b.Status)
		if oi != oj {
			return oi < oj
		}
		pi, pj := ranker.PriorityRank(a.Priority), ranker.PriorityRank(b.Priority)
		if pi != pj {
			return pi < pj
		}
		// EffectiveType so a type-less nib sorts as "task".
		ti, tj := getTypeOrder(a.EffectiveType()), getTypeOrder(b.EffectiveType())
		if ti != tj {
			return ti < tj
		}
		return strings.ToLower(a.Title) < strings.ToLower(b.Title)
	}
}
