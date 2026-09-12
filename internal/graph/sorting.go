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

// compareTimePtr treats nil as the zero time, which sorts first in ASC — and so
// last in the DESC the CLI time sorts default to.
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

// ApplySorting sorts nibs in-place according to the given sort options; a nil
// sort preserves input order.
//
// cfg is tolerant of nil today and unguarded: the vocabulary accessors the
// sort branches reach (PriorityRank, StatusNames, TypeNames) read package-level
// lists and ignore the receiver. Add a nil guard here with the first one that
// starts reading the config.
func ApplySorting(nibs []*nib.Nib, sort *model.NibSort, cfg *config.Config) {
	if sort == nil {
		return
	}

	switch sort.Field {
	case model.NibSortFieldOrder:
		nib.SortByOrder(nibs)
	case model.NibSortFieldMilestoneOrder:
		nib.SortByMilestoneOrder(nibs)
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
		// Direction is ignored: reversing a composite sort would invert every
		// key, not just the primary one.
		return
	}

	if sort.Direction != nil && *sort.Direction == model.SortDirectionDesc {
		slices.Reverse(nibs)
	}
}
