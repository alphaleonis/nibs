package graph

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
)

// compareTimePtr compares two *time.Time values. Nil is treated as zero time,
// which sorts first in ASC. For CLI time sorts (which default to DESC via
// buildNibSort), nil-timestamp nibs end up last after reversal.
func compareTimePtr(a, b *time.Time) int {
	var at, bt time.Time
	if a != nil {
		at = *a
	}
	if b != nil {
		bt = *b
	}
	return at.Compare(bt)
}

// ApplySorting sorts nibs in-place according to the given sort options.
// If sort is nil, no sorting is applied (preserves input order).
// Single-field sorts use slices.SortStableFunc: equal elements preserve input
// order rather than using explicit ID tiebreakers.
//
// cfg is passed to the vocabulary-ordered fields (priority, status, and the two
// combined). It is TOLERANT OF NIL TODAY and deliberately not guarded: statuses,
// types and priorities are hardcoded, so every accessor those branches reach
// (PriorityRank, PriorityNames, StatusNames, TypeNames) reads a package-level
// list and ignores its receiver. Both production call sites pass
// Reader.Config() regardless, which is what makes this safe to leave unenforced
// rather than a latent nil deref waiting for a caller.
//
// It stops being safe the moment any of those accessors starts reading the
// config — a per-store vocabulary is the obvious reason one would — and at that
// point the nil callers in this package's tests become panics. Guard it then.
func ApplySorting(nibs []*nib.Nib, sort *model.NibSort, cfg *config.Config) {
	if sort == nil {
		return
	}

	switch sort.Field {
	case model.NibSortFieldOrder:
		nib.SortByOrder(nibs)
	case model.NibSortFieldTitle:
		slices.SortStableFunc(nibs, func(a, b *nib.Nib) int {
			return cmp.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
		})
	case model.NibSortFieldCreatedAt:
		slices.SortStableFunc(nibs, func(a, b *nib.Nib) int {
			return compareTimePtr(a.CreatedAt, b.CreatedAt)
		})
	case model.NibSortFieldUpdatedAt:
		slices.SortStableFunc(nibs, func(a, b *nib.Nib) int {
			return compareTimePtr(a.UpdatedAt, b.UpdatedAt)
		})
	case model.NibSortFieldPriority:
		slices.SortStableFunc(nibs, func(a, b *nib.Nib) int {
			return cmp.Compare(cfg.PriorityRank(a.Priority), cfg.PriorityRank(b.Priority))
		})
	case model.NibSortFieldID:
		slices.SortStableFunc(nibs, func(a, b *nib.Nib) int {
			return cmp.Compare(a.ID, b.ID)
		})
	case model.NibSortFieldStatus:
		statusOrder := make(map[string]int)
		for i, s := range cfg.StatusNames() {
			statusOrder[s] = i
		}
		numStatuses := len(cfg.StatusNames())
		slices.SortStableFunc(nibs, func(a, b *nib.Nib) int {
			oa, ok := statusOrder[a.Status]
			if !ok {
				oa = numStatuses
			}
			ob, ok := statusOrder[b.Status]
			if !ok {
				ob = numStatuses
			}
			return cmp.Compare(oa, ob)
		})
	case model.NibSortFieldStatusPriority:
		nib.SortByStatusPriorityAndType(nibs, cfg.StatusNames(), cfg.TypeNames(), cfg)
		// Multi-key composite sort; simple reversal would invert all keys
		// simultaneously rather than just the primary key. Direction is ignored.
		return
	}

	if sort.Direction != nil && *sort.Direction == model.SortDirectionDesc {
		slices.Reverse(nibs)
	}
}
