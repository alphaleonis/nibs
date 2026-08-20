package nib

import (
	"cmp"
	"slices"
	"sort"
	"strings"
)

// PositionMap returns a map of nib.ID -> 1-based natural position among its
// siblings (sorted by Order). Positions are per-parent: root-level nibs are
// siblings of each other, children of the same parent are siblings, etc.
//
// Used by list rendering to surface a stable, human-friendly position number
// independent of the current --sort flag (so an agent can reference "move
// from 2 to 5" and have it survive sort changes).
func PositionMap(nibs []*Nib) map[string]int {
	byParent := make(map[string][]*Nib)
	for _, b := range nibs {
		byParent[b.Parent] = append(byParent[b.Parent], b)
	}
	positions := make(map[string]int, len(nibs))
	for _, group := range byParent {
		SortByOrder(group)
		for i, b := range group {
			positions[b.ID] = i + 1
		}
	}
	return positions
}

// SortByOrder sorts nibs by their Order field lexicographically.
// Nibs with an order key come first; nibs without one are appended sorted by title.
func SortByOrder(nibs []*Nib) {
	SortByKey(nibs, func(n *Nib) string { return n.Order })
}

// SortByKey sorts nibs by a caller-chosen ordering key, with SortByOrder's
// semantics: keyed nibs first in lexicographic key order, unkeyed nibs
// appended sorted by title. The multi-scope ordering engine sorts each scope
// by its own key field through this.
func SortByKey(nibs []*Nib, key func(*Nib) string) {
	slices.SortStableFunc(nibs, func(a, b *Nib) int {
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
// Nibs without priority are treated as "normal" (via the ranker) and nibs without
// a type as "task" (via EffectiveType) for sorting purposes, so a default-omitting
// nib sorts at the same position it would if the default were written to disk.
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
		// Tertiary: type order (EffectiveType so a type-less nib sorts as "task")
		ti, tj := getTypeOrder(nibs[i].EffectiveType()), getTypeOrder(nibs[j].EffectiveType())
		if ti != tj {
			return ti < tj
		}
		// Quaternary: title (case-insensitive) for stable, user-friendly ordering
		return strings.ToLower(nibs[i].Title) < strings.ToLower(nibs[j].Title)
	})
}
