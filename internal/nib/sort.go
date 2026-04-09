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
	slices.SortStableFunc(nibs, func(a, b *Nib) int {
		aHas := a.Order != ""
		bHas := b.Order != ""

		switch {
		case aHas && bHas:
			if c := cmp.Compare(a.Order, b.Order); c != 0 {
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
	})
}

// PriorityRanker maps a priority string to its sort rank.
// Used by SortByStatusPriorityAndType to decouple from config package.
type PriorityRanker interface {
	PriorityRank(priority string) int
}

// SortByStatusPriorityAndType sorts nibs by status order, then priority, then type, then title.
// Available via CLI --sort status-priority. The default sort is SortByOrder.
// Unrecognized statuses, priorities, and types are sorted last within their category.
// Nibs without priority are treated as "normal" priority for sorting purposes.
func SortByStatusPriorityAndType(nibs []*Nib, statusNames, typeNames []string, ranker PriorityRanker) {
	statusOrder := make(map[string]int)
	for i, s := range statusNames {
		statusOrder[s] = i
	}
	typeOrder := make(map[string]int)
	for i, t := range typeNames {
		typeOrder[t] = i
	}

	// Helper to get order with unrecognized values sorted last
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

	sort.Slice(nibs, func(i, j int) bool {
		// Primary: status order
		oi, oj := getStatusOrder(nibs[i].Status), getStatusOrder(nibs[j].Status)
		if oi != oj {
			return oi < oj
		}
		// Secondary: priority order
		pi, pj := ranker.PriorityRank(nibs[i].Priority), ranker.PriorityRank(nibs[j].Priority)
		if pi != pj {
			return pi < pj
		}
		// Tertiary: type order
		ti, tj := getTypeOrder(nibs[i].Type), getTypeOrder(nibs[j].Type)
		if ti != tj {
			return ti < tj
		}
		// Quaternary: title (case-insensitive) for stable, user-friendly ordering
		return strings.ToLower(nibs[i].Title) < strings.ToLower(nibs[j].Title)
	})
}
