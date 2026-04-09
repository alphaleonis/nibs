package graph

import (
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/graph/model"
)

// ApplyFilter applies NibFilter to a slice of nibs and returns filtered results.
// This is used by both the top-level nibs query and relationship field resolvers.
func ApplyFilter(nibs []*nib.Nib, filter *model.NibFilter, reader NibReader, blocking BlockingChecker) []*nib.Nib {
	if filter == nil {
		return nibs
	}

	result := nibs

	// String field filters
	result = filterByField(result, filter.Status, func(b *nib.Nib) string { return b.Status })
	result = excludeByField(result, filter.ExcludeStatus, func(b *nib.Nib) string { return b.Status })
	result = filterByField(result, filter.Type, func(b *nib.Nib) string { return b.Type })
	result = excludeByField(result, filter.ExcludeType, func(b *nib.Nib) string { return b.Type })

	// Priority with default (empty → "normal")
	result = filterByFieldWithDefault(result, filter.Priority, "normal", func(b *nib.Nib) string { return b.Priority })
	result = excludeByField(result, filter.ExcludePriority, func(b *nib.Nib) string {
		if b.Priority == "" {
			return "normal"
		}
		return b.Priority
	})

	// Estimate filters
	result = filterByField(result, filter.Estimate, func(b *nib.Nib) string { return b.Estimate })
	result = excludeByField(result, filter.ExcludeEstimate, func(b *nib.Nib) string { return b.Estimate })

	// Slice field filters
	result = filterBySliceField(result, filter.Tags, func(b *nib.Nib) []string { return b.Tags })
	result = excludeBySliceField(result, filter.ExcludeTags, func(b *nib.Nib) []string { return b.Tags })

	// Parent predicate filters
	result = filterByPredicate(result, filter.HasParent, func(b *nib.Nib) bool { return b.Parent != "" })
	result = filterByPredicate(result, filter.NoParent, func(b *nib.Nib) bool { return b.Parent == "" })
	if filter.ParentID != nil && *filter.ParentID != "" {
		result = filterByField(result, []string{*filter.ParentID}, func(b *nib.Nib) string { return b.Parent })
	}

	// Blocking filters (computed via BlockingChecker)
	result = filterByPredicate(result, filter.HasBlocking, func(b *nib.Nib) bool { return blocking.IsBlocking(b.ID) })
	result = filterByPredicate(result, filter.NoBlocking, func(b *nib.Nib) bool { return !blocking.IsBlocking(b.ID) })
	result = filterByPredicate(result, filter.IsBlocked, func(b *nib.Nib) bool { return blocking.IsBlocked(b.ID) })

	// BlockingID (special: needs reader to look up target nib)
	if filter.BlockingID != nil && *filter.BlockingID != "" {
		result = filterByBlockingID(result, *filter.BlockingID, reader)
	}

	// Blocked-by filters (from direct blocked_by field)
	result = filterByPredicate(result, filter.HasBlockedBy, func(b *nib.Nib) bool { return len(b.BlockedBy) > 0 })
	result = filterByPredicate(result, filter.NoBlockedBy, func(b *nib.Nib) bool { return len(b.BlockedBy) == 0 })
	if filter.BlockedByID != nil && *filter.BlockedByID != "" {
		result = filterBySliceField(result, []string{*filter.BlockedByID}, func(b *nib.Nib) []string { return b.BlockedBy })
	}

	return result
}

// filterByField filters nibs to include only those where getter returns a value in values (OR logic).
// Returns input unchanged if values is empty.
func filterByField(nibs []*nib.Nib, values []string, getter func(*nib.Nib) string) []*nib.Nib {
	if len(values) == 0 {
		return nibs
	}

	valueSet := make(map[string]bool, len(values))
	for _, v := range values {
		valueSet[v] = true
	}

	var result []*nib.Nib
	for _, b := range nibs {
		if valueSet[getter(b)] {
			result = append(result, b)
		}
	}
	return result
}

// excludeByField filters nibs to exclude those where getter returns a value in values.
// Returns input unchanged if values is empty.
func excludeByField(nibs []*nib.Nib, values []string, getter func(*nib.Nib) string) []*nib.Nib {
	if len(values) == 0 {
		return nibs
	}

	valueSet := make(map[string]bool, len(values))
	for _, v := range values {
		valueSet[v] = true
	}

	var result []*nib.Nib
	for _, b := range nibs {
		if !valueSet[getter(b)] {
			result = append(result, b)
		}
	}
	return result
}

// filterByPredicate keeps nibs where predicate(nib) matches *apply.
// If apply is nil, returns the input unchanged (no-op).
func filterByPredicate(nibs []*nib.Nib, apply *bool, predicate func(*nib.Nib) bool) []*nib.Nib {
	if apply == nil {
		return nibs
	}
	var result []*nib.Nib
	for _, b := range nibs {
		if predicate(b) == *apply {
			result = append(result, b)
		}
	}
	return result
}

// filterBySliceField filters nibs where ANY value in the nib's slice field
// matches ANY value in the filter list (OR semantics).
// Returns input unchanged if values is empty.
func filterBySliceField(nibs []*nib.Nib, values []string, getter func(*nib.Nib) []string) []*nib.Nib {
	if len(values) == 0 {
		return nibs
	}

	valueSet := make(map[string]bool, len(values))
	for _, v := range values {
		valueSet[v] = true
	}

	var result []*nib.Nib
	for _, b := range nibs {
		for _, v := range getter(b) {
			if valueSet[v] {
				result = append(result, b)
				break
			}
		}
	}
	return result
}

// excludeBySliceField excludes nibs where ANY value in the nib's slice field
// matches ANY value in the filter list.
// Returns input unchanged if values is empty.
func excludeBySliceField(nibs []*nib.Nib, values []string, getter func(*nib.Nib) []string) []*nib.Nib {
	if len(values) == 0 {
		return nibs
	}

	valueSet := make(map[string]bool, len(values))
	for _, v := range values {
		valueSet[v] = true
	}

	var result []*nib.Nib
outer:
	for _, b := range nibs {
		for _, v := range getter(b) {
			if valueSet[v] {
				continue outer
			}
		}
		result = append(result, b)
	}
	return result
}

// filterByFieldWithDefault works like filterByField but applies a default
// value when the getter returns empty string.
// Returns input unchanged if values is empty.
func filterByFieldWithDefault(nibs []*nib.Nib, values []string, defaultVal string, getter func(*nib.Nib) string) []*nib.Nib {
	if len(values) == 0 {
		return nibs
	}

	valueSet := make(map[string]bool, len(values))
	for _, v := range values {
		valueSet[v] = true
	}

	var result []*nib.Nib
	for _, b := range nibs {
		val := getter(b)
		if val == "" {
			val = defaultVal
		}
		if valueSet[val] {
			result = append(result, b)
		}
	}
	return result
}

// filterByBlockingID filters nibs that are blocking a specific nib ID.
// Computed: checks if targetID has this nib in its blockedBy.
func filterByBlockingID(nibs []*nib.Nib, targetID string, reader NibReader) []*nib.Nib {
	targetNib, err := reader.Get(targetID)
	if err != nil {
		return nil
	}
	blockerSet := make(map[string]bool)
	for _, id := range targetNib.BlockedBy {
		blockerSet[id] = true
	}

	var result []*nib.Nib
	for _, b := range nibs {
		if blockerSet[b.ID] {
			result = append(result, b)
		}
	}
	return result
}
