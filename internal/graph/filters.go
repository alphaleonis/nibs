package graph

import (
	"context"
	"slices"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
)

// resolveFilterID resolves one id-valued filter argument through the reader,
// returning what NibReader.NormalizeID returns: (full id, true) on a hit, (the
// id as supplied, false) on a miss.
//
// An id TRANSFORM added here runs AFTER resolveFilterTarget's emptiness test —
// see FilterTargetEmptyError for the policy a whitespace trim would break.
func resolveFilterID(reader NibReader, id string) (string, bool) {
	return reader.NormalizeID(id)
}

// resolveFilterTarget turns one id-valued filter field into the full id its
// branch matches on, or reports why the branch cannot run. Every filter.*ID
// branch in ApplyFilter goes through it, so an unknown target fails the whole
// filter chain instead of narrowing it.
//
// Keep the emptiness test here, not copied into each branch, and keep it EXACT
// — see FilterTargetEmptyError.
func resolveFilterTarget(reader NibReader, field, id string) (string, error) {
	if id == "" {
		return "", &FilterTargetEmptyError{Field: field}
	}
	fullID, ok := resolveFilterID(reader, id)
	if !ok {
		return "", &FilterTargetNotFoundError{Field: field, ID: id}
	}
	return fullID, nil
}

// refuseContradiction reports *FilterTargetContradictionError when an id-valued
// filter field is combined with the presence field covering the same
// relationship, set to false. Two pairs qualify, each empty by construction:
//
//   - parentId + hasParent. Both read the resolved parent (see resolvedParent).
//   - blockedById + hasBlockedBy. blockedById requires the target in
//     b.BlockedBy, which forces len(b.BlockedBy) > 0.
//
// Do not add blockingId + hasBlocking as a third. hasBlocking asks whether a nib
// is ACTIVELY blocking (nibcore's isBlockingInMap releases on the status at BOTH
// ends) while blockingId matches the target's stored blocked_by whatever the
// candidate's status, so the pair is a real query.
func refuseContradiction(field string, id *string, presenceField string, presence *bool) error {
	if id == nil || *id == "" || presence == nil || *presence {
		return nil
	}
	return &FilterTargetContradictionError{Field: field, PresenceField: presenceField, ID: *id}
}

// hasBoundingFilter reports whether the filter names a nib whose relationships
// already bound the set a search term can select from — which decides whether
// queryResolver.Nibs seeds from the capped Search or the uncapped SearchAll,
// where a cap would truncate the store rather than the answer.
//
// Classify a new field by whether it NAMES A NIB, not by how large a set it
// selects: siblingId on a root can select most of the store and is still
// bounding. TestEveryNibFilterFieldIsClassifiedAsBoundingOrNot requires every
// model.NibFilter field to be classified.
//
// A field set to the EMPTY STRING counts here; reading emptiness as "absent"
// would copy resolveFilterTarget's rule into this file.
func hasBoundingFilter(filter *model.NibFilter) bool {
	if filter == nil {
		return false
	}
	return filter.ParentID != nil ||
		filter.AncestorID != nil ||
		filter.DescendantID != nil ||
		filter.SiblingID != nil ||
		filter.BlockingID != nil ||
		filter.BlockedByID != nil ||
		filter.MentionsID != nil ||
		filter.MentionedByID != nil ||
		filter.Milestone != nil
}

// ApplyFilter applies NibFilter to a slice of nibs and returns filtered
// results. Every branch NARROWS the slice it was handed and none widens it,
// search included.
//
// ctx carries an optional per-operation RequestCache (see request_cache.go);
// callers without one pass context.Background(). That lookup is the ONLY thing
// ctx is consulted for: ApplyFilter checks no cancellation and honors no
// deadline, so every filter branch runs to completion.
//
// Do not fold a refusal class from filter_errors.go into an empty result: an
// empty list is a factual claim about the store, and an empty id read as
// "unset" drops its branch and widens the query to every nib.
func ApplyFilter(ctx context.Context, nibs []*nib.Nib, filter *model.NibFilter, reader NibReader, blocking BlockingChecker) ([]*nib.Nib, error) {
	if filter == nil {
		return nibs, nil
	}

	// Refused first: an unresolvable id in a contradictory pair reports the
	// contradiction, not the not-found.
	if err := refuseContradiction("parentId", filter.ParentID, "hasParent", filter.HasParent); err != nil {
		return nil, err
	}
	if err := refuseContradiction("blockedById", filter.BlockedByID, "hasBlockedBy", filter.HasBlockedBy); err != nil {
		return nil, err
	}

	result := nibs

	result = filterByField(result, filter.Status, func(b *nib.Nib) string { return b.Status })
	result = excludeByField(result, filter.ExcludeStatus, func(b *nib.Nib) string { return b.Status })

	// Type and Priority filter on the EFFECTIVE value, so a nib that omitted the
	// field matches as though nib.DefaultType / nib.DefaultPriority were on disk.
	result = filterByField(result, filter.Type, func(b *nib.Nib) string { return b.EffectiveType() })
	result = excludeByField(result, filter.ExcludeType, func(b *nib.Nib) string { return b.EffectiveType() })

	result = filterByField(result, filter.Priority, func(b *nib.Nib) string { return b.EffectivePriority() })
	result = excludeByField(result, filter.ExcludePriority, func(b *nib.Nib) string { return b.EffectivePriority() })

	result = filterByField(result, filter.Estimate, func(b *nib.Nib) string { return b.Estimate })
	result = excludeByField(result, filter.ExcludeEstimate, func(b *nib.Nib) string { return b.Estimate })

	result = filterBySliceField(result, filter.Tags, func(b *nib.Nib) []string { return b.Tags })
	result = excludeBySliceField(result, filter.ExcludeTags, func(b *nib.Nib) []string { return b.Tags })

	// Parent-ness is "the link resolves", not "the field is non-empty" — see
	// resolvedParent.
	result = filterByPredicate(result, filter.HasParent, func(b *nib.Nib) bool {
		return resolvedParentID(b, reader) != ""
	})
	if filter.ParentID != nil {
		fullID, err := resolveFilterTarget(reader, "parentId", *filter.ParentID)
		if err != nil {
			return nil, err
		}
		result = filterByField(result, []string{fullID}, func(b *nib.Nib) string {
			return resolvedParentID(b, reader)
		})
	}

	// Each field names the relationship the MATCHED nib holds toward the
	// supplied target: ancestorId keeps the target's descendants, descendantId
	// keeps its ancestors, siblingId keeps nibs sharing its parent. The target is
	// none of those to itself, so all three exclude it HERE — queryResolver.Nibs
	// puts the ancestorId target and the siblingId shared parent back by running
	// includeAncestors after ApplyFilter whenever search is set, which both schema
	// fields document and the web UI's tree rendering depends on. Do not "fix"
	// that in this file.
	if filter.AncestorID != nil {
		fullID, err := resolveFilterTarget(reader, "ancestorId", *filter.AncestorID)
		if err != nil {
			return nil, err
		}
		result = filterByAncestorID(result, fullID, reader)
	}
	if filter.DescendantID != nil {
		fullID, err := resolveFilterTarget(reader, "descendantId", *filter.DescendantID)
		if err != nil {
			return nil, err
		}
		if result, err = filterByDescendantID(result, fullID, reader); err != nil {
			return nil, err
		}
	}
	if filter.SiblingID != nil {
		fullID, err := resolveFilterTarget(reader, "siblingId", *filter.SiblingID)
		if err != nil {
			return nil, err
		}
		if result, err = filterBySiblingID(result, fullID, reader); err != nil {
			return nil, err
		}
	}

	result = filterByPredicate(result, filter.HasBlocking, func(b *nib.Nib) bool { return blocking.IsBlocking(b.ID) })
	result = filterByPredicate(result, filter.IsBlocked, func(b *nib.Nib) bool { return blocking.IsBlocked(b.ID) })

	if filter.BlockingID != nil {
		fullID, err := resolveFilterTarget(reader, "blockingId", *filter.BlockingID)
		if err != nil {
			return nil, err
		}
		if result, err = filterByBlockingID(result, fullID, reader); err != nil {
			return nil, err
		}
	}

	// Read from the nib's own blocked_by, not from BlockingChecker.
	result = filterByPredicate(result, filter.HasBlockedBy, func(b *nib.Nib) bool { return len(b.BlockedBy) > 0 })
	if filter.BlockedByID != nil {
		fullID, err := resolveFilterTarget(reader, "blockedById", *filter.BlockedByID)
		if err != nil {
			return nil, err
		}
		result = filterBySliceField(result, []string{fullID}, func(b *nib.Nib) []string { return b.BlockedBy })
	}

	// milestone matches the RESOLVED direct assignment —
	// membership.ResolvedMilestoneID's reading, the one the ordering engine's
	// queue scope groups by — so a dangling or non-milestone assignment matches
	// nothing here exactly as it schedules nothing there.
	if filter.Milestone != nil {
		fullID, err := resolveFilterTarget(reader, "milestone", *filter.Milestone)
		if err != nil {
			return nil, err
		}
		target, err := reader.Get(fullID)
		if err != nil {
			return nil, &FilterTargetUnreadableError{Field: "milestone", ID: fullID, ReaderErr: err}
		}
		if typ := target.EffectiveType(); typ != "milestone" {
			return nil, &FilterTargetTypeError{Field: "milestone", ID: fullID, Got: typ, Want: "milestone"}
		}
		result = filterByField(result, []string{fullID}, func(b *nib.Nib) string {
			return resolvedMilestoneID(b, reader)
		})
	}

	// noMilestone reads DERIVED membership: true is the backlog, and a child of
	// an assigned epic is planned work rather than backlog. The View covers the
	// WHOLE store, not the candidate slice, so an assigned ancestor an earlier
	// branch filtered out still schedules its subtree.
	if filter.NoMilestone != nil {
		view := cachedMembershipView(ctx, reader)
		result = filterByPredicate(result, filter.NoMilestone, func(b *nib.Nib) bool {
			return view.MilestoneOf(b.ID) == ""
		})
	}

	// area is DOWNWARD-CLOSED over the declared tree, so `area: "web"` selects
	// web/dashboard too; Areas.IsWithin owns that closure. It names no nib, so
	// there is no target to resolve and nothing here bounds a search (see
	// hasBoundingFilter).
	if filter.Area != nil {
		// ONE snapshot for both steps: the vocabulary reloads while the server
		// runs, so asking twice could refuse against one tree and filter against
		// another, accepting a path and returning nothing for it.
		areas := reader.Areas()
		if err := refuseUndeclaredArea(areas, "area", *filter.Area); err != nil {
			return nil, err
		}
		result = filterByAreaWithin(result, areas, *filter.Area)
	}

	if filter.MentionsID != nil {
		fullID, err := resolveFilterTarget(reader, "mentionsId", *filter.MentionsID)
		if err != nil {
			return nil, err
		}
		result = filterByMentionsID(ctx, result, fullID, reader)
	}
	if filter.MentionedByID != nil {
		fullID, err := resolveFilterTarget(reader, "mentionedById", *filter.MentionedByID)
		if err != nil {
			return nil, err
		}
		result = filterByMentionedByID(ctx, result, fullID, reader)
	}

	// Keep search LAST — it is the only branch that queries the index, so a
	// filter an earlier branch refuses pays for no query here.
	//
	// An empty term is not refused: "no keyword filter" is a real meaning,
	// unlike an empty id. cmd/list.go sets the field only for a non-empty -S.
	if filter.Search != nil && *filter.Search != "" {
		var err error
		if result, err = filterBySearch(ctx, result, *filter.Search, reader); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// filterBySearch INTERSECTS the working set with the search index's answer,
// keeping the input's own order, and never seeds from the index itself. It
// reads the UNCAPPED answer rather than Search's top hits, because the relation
// already bounds the working set (see hasBoundingFilter).
//
// Do not degrade an index failure to "nothing matched", even though on a
// relationship field that costs the WHOLE response: every such field is
// [Nib!]!, so null propagation carries the failure to the root.
//
// Compare by ID: the relationship resolvers hand ApplyFilter detached snapshots
// (NibReader.GetSnapshot) while the reader hands back store pointers.
func filterBySearch(ctx context.Context, nibs []*nib.Nib, query string, reader NibReader) ([]*nib.Nib, error) {
	matched, err := cachedSearchAllIDs(ctx, reader, query)
	if err != nil {
		return nil, err
	}

	var result []*nib.Nib
	for _, b := range nibs {
		if _, ok := matched[b.ID]; ok {
			result = append(result, b)
		}
	}
	return result, nil
}

// filterByMentionsID keeps nibs that mention the given target in their body.
// targetID must already be a full (normalized) ID — see cachedMentionedBy.
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

// filterByMentionedByID keeps nibs that are mentioned in the given source's
// body. sourceID must already be a full (normalized) ID — see cachedMentions.
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

// parentChain returns the IDs on b's parent chain, nearest ancestor first,
// walking up to the root — resolved ids only, and a link naming no nib ends the
// chain (see WalkParentChain and ParentStep for the two rules).
//
// The visited set is per-call and seeded with b.ID, which keeps b off its own
// chain. Two constraints on any memoization added here:
//
//   - Cache the per-id ANSWER, not the visited flag. One set shared across
//     candidates makes a later walk stop at an ancestor an earlier one banked.
//   - Keep reader as the source of ancestry. Indexing the candidate slice
//     truncates every chain at the first filtered-out ancestor: `--status todo
//     --ancestor <epic>` reaches through a completed intermediate.
func parentChain(b *nib.Nib, reader NibReader) []string {
	var ids []string
	for _, ancestor := range liveParentChain(b, reader, map[string]bool{b.ID: true}) {
		ids = append(ids, ancestor.ID)
	}
	return ids
}

// resolvedParent returns the nib b's parent link resolves to, or nil when b has
// no parent AND when the link names no nib.
//
// CANONICAL INVARIANT: "has a parent" means the link RESOLVES — state it here,
// do not re-derive it. Deciding parent-ness from whether the raw b.Parent
// string is empty is the mistake it prevents: a dangling link is non-empty and
// fetches nothing, so the two readings disagree on exactly that nib.
// resolvedParentID is the ID-shaped wrapper most callers reach for, so auditing
// the decision points means asking for references to BOTH names.
//
// The returned pointer is the reader's LIVE store pointer (NibReader.Get):
// snapshot it if the result outlives the store lock (NibReader.GetSnapshot).
func resolvedParent(b *nib.Nib, reader NibReader) *nib.Nib {
	if b.Parent == "" {
		return nil
	}
	parent, err := reader.Get(b.Parent)
	if err != nil {
		return nil
	}
	return parent
}

// resolvedParentID is resolvedParent in ID shape: the parent's resolved ID, or
// "" when b has no parent AND when the link names no nib. See resolvedParent.
func resolvedParentID(b *nib.Nib, reader NibReader) string {
	parent := resolvedParent(b, reader)
	if parent == nil {
		return ""
	}
	return parent.ID
}

// filterByAncestorID keeps nibs with targetID somewhere in their parent chain —
// the target's descendants at any depth, the target itself excluded. targetID
// must already be a full (normalized) ID.
//
// Do not add a Get here for symmetry with its siblings: never fetching the
// target lets a concurrent delete simply drop it off every chain.
func filterByAncestorID(nibs []*nib.Nib, targetID string, reader NibReader) []*nib.Nib {
	var result []*nib.Nib
	for _, b := range nibs {
		if slices.Contains(parentChain(b, reader), targetID) {
			result = append(result, b)
		}
	}
	return result
}

// filterByDescendantID keeps nibs with targetID somewhere in their descendant
// subtree — exactly the target's ancestor chain, the target itself excluded.
// targetID must already be a full (normalized) ID; the Get can still fail on a
// concurrent delete, which is what FilterTargetUnreadableError is for.
func filterByDescendantID(nibs []*nib.Nib, targetID string, reader NibReader) ([]*nib.Nib, error) {
	target, err := reader.Get(targetID)
	if err != nil {
		return nil, &FilterTargetUnreadableError{Field: "descendantId", ID: targetID, ReaderErr: err}
	}
	chain := parentChain(target, reader)
	ancestors := make(map[string]bool, len(chain))
	for _, id := range chain {
		ancestors[id] = true
	}

	var result []*nib.Nib
	for _, b := range nibs {
		if ancestors[b.ID] {
			result = append(result, b)
		}
	}
	return result, nil
}

// filterBySiblingID keeps nibs sharing the target's parent, the target itself
// excluded. A parentless target selects the other root nibs — the answer
// `nibs rel --rel siblings` gives — by matching on the empty parent, with no
// case of its own. targetID must already be a full (normalized) ID; see
// filterByDescendantID for the unreadable case.
func filterBySiblingID(nibs []*nib.Nib, targetID string, reader NibReader) ([]*nib.Nib, error) {
	target, err := reader.Get(targetID)
	if err != nil {
		return nil, &FilterTargetUnreadableError{Field: "siblingId", ID: targetID, ReaderErr: err}
	}
	targetParentID := resolvedParentID(target, reader)

	var result []*nib.Nib
	for _, b := range nibs {
		if b.ID == targetID {
			continue
		}
		if resolvedParentID(b, reader) == targetParentID {
			result = append(result, b)
		}
	}
	return result, nil
}

// filterByField keeps nibs whose getter value is any of values.
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

// excludeByField drops nibs whose getter value is any of values.
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

// refuseUndeclaredArea reports why an area filter cannot run, or nil when the
// value names a declared area. The empty string is refused and tested EXACTLY,
// for the reason resolveFilterTarget's is; a whitespace-only value is an
// ordinary undeclared path.
func refuseUndeclaredArea(areas *config.Areas, field, path string) error {
	if path == "" {
		return &FilterAreaError{Field: field}
	}
	if areas.IsValid(path) {
		return nil
	}
	declared := ""
	if areas.Declared() {
		declared = areas.List()
	}
	return &FilterAreaError{Field: field, Path: config.RenderAreaPath(path), Declared: declared}
}

// filterByAreaWithin keeps the nibs whose stored area is ancestor or sits below
// it in the DECLARED tree (Areas.IsWithin). A stored value the vocabulary no
// longer declares is within nothing, so a retired area stays out of the answer.
func filterByAreaWithin(nibs []*nib.Nib, areas *config.Areas, ancestor string) []*nib.Nib {
	var result []*nib.Nib
	for _, b := range nibs {
		if areas.IsWithin(b.Area, ancestor) {
			result = append(result, b)
		}
	}
	return result
}

// filterByPredicate keeps nibs where predicate(b) equals *apply.
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

// filterBySliceField keeps nibs whose slice field shares ANY value with values.
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

// excludeBySliceField drops nibs whose slice field shares ANY value with values.
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
// any missing ancestor nibs, so the client can build a complete tree hierarchy
// even when search or filters matched only leaves.
//
// One visited set spans the WHOLE batch, seeded with every input nib's ID,
// which keeps the output free of duplicates — the opposite of the per-call set
// parentChain needs (see WalkParentChain).
//
// The added ancestors are the reader's live store pointers, as the input nibs
// are; queryResolver.Nibs snapshots the whole result before gqlgen sees it.
func includeAncestors(nibs []*nib.Nib, reader NibReader) []*nib.Nib {
	present := make(map[string]bool, len(nibs))
	for _, b := range nibs {
		present[b.ID] = true
	}

	var extras []*nib.Nib
	for _, b := range nibs {
		extras = append(extras, liveParentChain(b, reader, present)...)
	}

	if len(extras) == 0 {
		return nibs
	}
	return append(nibs, extras...)
}

// filterByBlockingID keeps the nibs the target lists in its blocked_by, whatever
// their status — see the schema's blockingId description for why that is not the
// same question hasBlocking asks. targetID must already be a full (normalized)
// ID; see filterByDescendantID for the unreadable case.
func filterByBlockingID(nibs []*nib.Nib, targetID string, reader NibReader) ([]*nib.Nib, error) {
	targetNib, err := reader.Get(targetID)
	if err != nil {
		return nil, &FilterTargetUnreadableError{Field: "blockingId", ID: targetID, ReaderErr: err}
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
	return result, nil
}
