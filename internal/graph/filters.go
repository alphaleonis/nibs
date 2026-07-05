package graph

import (
	"context"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/graph/model"
)

// resolveFilterID normalizes a single-ID filter argument via the reader's
// NormalizeID method. It returns exactly what NibReader.NormalizeID returns
// — (fullID, true) on success; (echoed input id, false) on miss — so the
// two helpers agree on the miss convention and callers can use either
// interchangeably without surprise.
//
// Why this wrapper exists despite being a straight passthrough today:
//   - Semantic naming at call sites: inside ApplyFilter, `resolveFilterID`
//     reads as "resolve a filter ID" rather than "normalize a generic ID",
//     making intent explicit.
//   - Future extension point: this is the single place where all four
//     filter.*ID branches can gain shared behaviour — e.g. the 128-char
//     input-length cap tracked in nibs-puq1 — without touching each
//     branch individually.
//
// Every filter.*ID branch in ApplyFilter pairs this call with an explicit
// `if !ok { return nil }` so an unknown target short-circuits the whole
// filter chain to nil. The pattern is repeated by design: four branches
// keep it grep-findable and the set is stable.
func resolveFilterID(reader NibReader, id string) (string, bool) {
	return reader.NormalizeID(id)
}

// ApplyFilter applies NibFilter to a slice of nibs and returns filtered results.
// This is used by both the top-level nibs query and relationship field resolvers.
//
// ctx carries an optional per-request RequestCache (see request_cache.go); the
// mention filter branches route through it so duplicate mention lookups
// within a single GraphQL operation hit the cache instead of the reader.
// CLI callers or pure unit tests may pass context.Background(); the cache is
// keyed on ctx values only.
//
// ctx is currently consulted only by the mention-filter branches (for
// RequestCache lookup). ApplyFilter does not check cancellation or honour
// deadlines — passing a cancelled ctx will not short-circuit; every filter
// branch runs to completion.
//
// Callers threading filter.MentionsID / filter.MentionedByID must let
// ApplyFilter handle ID resolution via resolveFilterID. Pre-normalising in
// the caller is not required for correctness, but mixing short and full
// forms across resolvers within the same request will desync the cache
// keys (keyed on the full normalised ID) and silently degrade memoisation.
func ApplyFilter(ctx context.Context, nibs []*nib.Nib, filter *model.NibFilter, reader NibReader, blocking BlockingChecker) []*nib.Nib {
	if filter == nil {
		return nibs
	}

	result := nibs

	// String field filters. Type and Priority use the effective value so a
	// default-omitting nib filters as though the "task"/"normal" default were on
	// disk (matching the stored Nib's presentation defaults — see nib.DefaultType).
	result = filterByField(result, filter.Status, func(b *nib.Nib) string { return b.Status })
	result = excludeByField(result, filter.ExcludeStatus, func(b *nib.Nib) string { return b.Status })
	result = filterByField(result, filter.Type, func(b *nib.Nib) string { return b.EffectiveType() })
	result = excludeByField(result, filter.ExcludeType, func(b *nib.Nib) string { return b.EffectiveType() })

	result = filterByField(result, filter.Priority, func(b *nib.Nib) string { return b.EffectivePriority() })
	result = excludeByField(result, filter.ExcludePriority, func(b *nib.Nib) string { return b.EffectivePriority() })

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

	// BlockingID (special: needs reader to look up target nib).
	// Unknown target short-circuits to nil (shared contract for all *ID filters).
	if filter.BlockingID != nil && *filter.BlockingID != "" {
		fullID, ok := resolveFilterID(reader, *filter.BlockingID)
		if !ok {
			return nil
		}
		result = filterByBlockingID(result, fullID, reader)
	}

	// Blocked-by filters (from direct blocked_by field)
	result = filterByPredicate(result, filter.HasBlockedBy, func(b *nib.Nib) bool { return len(b.BlockedBy) > 0 })
	result = filterByPredicate(result, filter.NoBlockedBy, func(b *nib.Nib) bool { return len(b.BlockedBy) == 0 })
	if filter.BlockedByID != nil && *filter.BlockedByID != "" {
		fullID, ok := resolveFilterID(reader, *filter.BlockedByID)
		if !ok {
			return nil
		}
		result = filterBySliceField(result, []string{fullID}, func(b *nib.Nib) []string { return b.BlockedBy })
	}

	// Mention filters (computed via FindMentions/FindMentionedBy on the reader,
	// routed through the per-request cache so repeated lookups within one
	// GraphQL operation don't re-run the reader).
	if filter.MentionsID != nil && *filter.MentionsID != "" {
		fullID, ok := resolveFilterID(reader, *filter.MentionsID)
		if !ok {
			return nil
		}
		result = filterByMentionsID(ctx, result, fullID, reader)
	}
	if filter.MentionedByID != nil && *filter.MentionedByID != "" {
		fullID, ok := resolveFilterID(reader, *filter.MentionedByID)
		if !ok {
			return nil
		}
		result = filterByMentionedByID(ctx, result, fullID, reader)
	}

	return result
}

// filterByMentionsID keeps nibs that mention the given target in their body.
// targetID must already be a full (normalised) ID — callers resolve via
// resolveFilterID before invoking. Routes through the per-request mention
// cache attached to ctx (if any).
func filterByMentionsID(ctx context.Context, nibs []*nib.Nib, targetID string, reader NibReader) []*nib.Nib {
	inbound := cachedMentionedBy(ctx, reader, targetID)
	inboundSet := make(map[string]bool, len(inbound))
	for _, b := range inbound {
		inboundSet[b.ID] = true
	}
	var result []*nib.Nib
	for _, b := range nibs {
		if inboundSet[b.ID] {
			result = append(result, b)
		}
	}
	return result
}

// filterByMentionedByID keeps nibs that are mentioned in the given source's body.
// sourceID must already be a full (normalised) ID — callers resolve via
// resolveFilterID before invoking. Routes through the per-request mention
// cache attached to ctx (if any).
func filterByMentionedByID(ctx context.Context, nibs []*nib.Nib, sourceID string, reader NibReader) []*nib.Nib {
	outbound := cachedMentions(ctx, reader, sourceID)
	outboundSet := make(map[string]bool, len(outbound))
	for _, b := range outbound {
		outboundSet[b.ID] = true
	}
	var result []*nib.Nib
	for _, b := range nibs {
		if outboundSet[b.ID] {
			result = append(result, b)
		}
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

// includeAncestors walks the parent chain for every nib in the result and adds
// any missing ancestor nibs.  This ensures the client can always build a
// complete tree hierarchy even when search or filters matched only leaves.
func includeAncestors(nibs []*nib.Nib, reader NibReader) []*nib.Nib {
	present := make(map[string]bool, len(nibs))
	for _, b := range nibs {
		present[b.ID] = true
	}

	var extras []*nib.Nib
	for _, b := range nibs {
		parentID := b.Parent
		for parentID != "" && !present[parentID] {
			present[parentID] = true
			parent, err := reader.Get(parentID)
			if err != nil {
				break
			}
			extras = append(extras, parent)
			parentID = parent.Parent
		}
	}

	if len(extras) == 0 {
		return nibs
	}
	return append(nibs, extras...)
}

// filterByBlockingID filters nibs that are blocking a specific nib ID.
// Computed: checks if targetID has this nib in its blockedBy.
// targetID must already be a full (normalised) ID — callers resolve via
// resolveFilterID before invoking. Returns nil if the target nib cannot
// be fetched (defensive: the caller already proved the ID resolves, but
// Get may still fail on a concurrent delete).
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
