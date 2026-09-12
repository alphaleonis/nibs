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
// An id TRANSFORM added here would run AFTER resolveFilterTarget's emptiness
// test, so a trim meant to read a whitespace-only id as empty has to go above
// that test instead. See FilterTargetEmptyError for the policy it would break.
func resolveFilterID(reader NibReader, id string) (string, bool) {
	return reader.NormalizeID(id)
}

// resolveFilterTarget turns one id-valued filter field into the full id its
// branch matches on, or reports why the branch cannot run. Every filter.*ID
// branch in ApplyFilter goes through it, which is what makes an unknown target
// fail the whole filter chain instead of narrowing it.
//
// Keep the emptiness test here rather than copying it into each branch: a copy
// can be half-applied, and a branch that reads an empty id as "unset" skips
// itself and silently widens the query to the whole store. The test is EXACT —
// see FilterTargetEmptyError for what a trimming policy here would contradict.
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
// relationship, set to false. Two pairs qualify, and each is empty by
// construction rather than by store state:
//
//   - parentId + hasParent. Both read the resolved parent (see resolvedParent),
//     so no nib both has parent X and has none.
//   - blockedById + hasBlockedBy. blockedById requires the target in
//     b.BlockedBy, which forces len(b.BlockedBy) > 0, which is hasBlockedBy.
//
// Do not add blockingId + hasBlocking as a third. hasBlocking asks whether a nib
// is ACTIVELY blocking (nibcore's isBlockingInMap releases on the status at BOTH
// ends of the edge) while blockingId matches the target's stored blocked_by
// whatever the candidate's status, so the pair is a real query — the schema's
// blockingId description states which one, and says why it can return an open
// blocker.
//
// An empty id is left to FilterTargetEmptyError: it names no nib, so there is
// nothing for the presence field to contradict. A presence field set to TRUE is
// merely redundant and is left alone, as cmd/list.go leaves `--parent X
// --has-parent` alone.
func refuseContradiction(field string, id *string, presenceField string, presence *bool) error {
	if id == nil || *id == "" || presence == nil || *presence {
		return nil
	}
	return &FilterTargetContradictionError{Field: field, PresenceField: presenceField, ID: *id}
}

// hasBoundingFilter reports whether the filter names a nib whose relationships
// already bound the set a search term can select from.
//
// It decides which population queryResolver.Nibs truncates. A term on its own
// chooses from the whole store, so the store-wide cap IS the answer there — the
// top hits for q. Add one of these fields and the question becomes an
// intersection over a set the store's link structure already bounds — "the
// children of X matching q" — where a store-wide cap truncates the wrong
// population, dropping a genuine member that ranks below the global cutoff with
// no error and no signal. filterBySearch reads a relationship field the same
// way, which is what lets the two surfaces reach one answer.
//
// Classify a new field by whether it NAMES A NIB, not by how large a set it
// selects: siblingId on a root can select most of the store and is still
// bounding, because the question is still "of X's siblings, which match q".
// TestEveryNibFilterFieldIsClassifiedAsBoundingOrNot requires every
// model.NibFilter field to be classified, so a new bounding filter fails a test
// instead of quietly missing this list.
//
// A field set to the EMPTY STRING counts here, even though ApplyFilter goes on
// to refuse it: the refusal still happens, just after an uncapped search has
// already run. Reading emptiness as "absent" would put a second copy of the
// emptiness rule here to disagree with resolveFilterTarget's.
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

// ApplyFilter applies NibFilter to a slice of nibs and returns filtered results.
// This is used by both the top-level nibs query and relationship field resolvers.
//
// Every branch NARROWS the slice it was handed and none of them widens it,
// search included — see the search branch for what that means where the input is
// a relationship's members rather than the whole store.
//
// ctx carries an optional per-operation RequestCache (see request_cache.go); the
// mention branches and the search branch route through it, and CLI callers or
// pure unit tests may pass context.Background(). That lookup is the ONLY thing
// ctx is consulted for: ApplyFilter checks no cancellation and honors no
// deadline, so every filter branch runs to completion.
//
// The error return exists to keep the refusal classes in filter_errors.go out of
// the empty result. Folding any of them into it is what this signature prevents:
// "what is under nibs-abc1?" answered with an empty list is a factual claim
// about the store, and a caller that mistyped the id cannot tell it apart from
// the truth. An empty id is worse still — read as "unset" it drops its branch
// outright, so the query widens to every nib in the store and answers a question
// nobody asked.
func ApplyFilter(ctx context.Context, nibs []*nib.Nib, filter *model.NibFilter, reader NibReader, blocking BlockingChecker) ([]*nib.Nib, error) {
	if filter == nil {
		return nibs, nil
	}

	// Refused first, and the order is the verdict: an unresolvable id in a
	// contradictory pair reports the contradiction rather than the not-found.
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
	// resolvedParent for the rule and the surfaces it has to agree with.
	result = filterByPredicate(result, filter.HasParent, func(b *nib.Nib) bool {
		return resolvedParentID(b, reader) != ""
	})
	if filter.ParentID != nil {
		// Candidates are compared by RESOLVED parent, not stored spelling.
		// Comparing the stored string would agree on every link the loader's
		// canonicalization pass rewrote and disagree on one it never saw, making
		// the answer a property of the reader rather than of the data.
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
	// keeps its ancestors, siblingId keeps nibs sharing its parent. The target
	// is none of those to itself, so all three exclude it here.
	//
	// "Here" is load-bearing: queryResolver.Nibs runs includeAncestors AFTER
	// ApplyFilter whenever search is set, which puts the ancestorId target and
	// the siblingId shared parent back into the response. That is the documented
	// behavior of both schema fields and the web UI's tree rendering depends on
	// it — do not "fix" it in this file.
	//
	// Keep all three guards even though a missing one refuses differently:
	// ancestorId would narrow to the empty set (parentChain banks only fetched
	// ids, so the echoed miss matches nothing) while the other two would report
	// the unreadable class for what is a caller's typo.
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
	// nothing here exactly as it schedules nothing there. A target that exists
	// but is not milestone-typed is refused rather than answered with the empty
	// set, which would read as "this milestone has no members" for an id that
	// names no milestone.
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

	// noMilestone reads DERIVED membership (membership.MilestoneOf): true is the
	// backlog, and a child of an assigned epic is planned work rather than
	// backlog. The View covers the WHOLE store, not the candidate slice, so an
	// assigned ancestor an earlier branch filtered out still schedules its
	// subtree.
	if filter.NoMilestone != nil {
		view := cachedMembershipView(ctx, reader)
		result = filterByPredicate(result, filter.NoMilestone, func(b *nib.Nib) bool {
			return view.MilestoneOf(b.ID) == ""
		})
	}

	// The OWNERSHIP axis. area is DOWNWARD-CLOSED over the declared tree, so
	// `area: "web"` selects web/dashboard too; Areas.IsWithin owns that closure.
	// Unlike milestone it names no nib, so there is no target to resolve and
	// nothing here bounds a search (see hasBoundingFilter). What it shares with
	// milestone is the refusal: an undeclared value is rejected rather than
	// answered with the empty set, which would read as "no work is in this area"
	// for a path that names no area at all.
	if filter.Area != nil {
		// ONE snapshot for both steps. The vocabulary reloads while the server
		// runs, so asking twice could refuse against one tree and then filter
		// against another, accepting a path and returning nothing for it.
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

	// Keep search LAST. It is the only branch that queries the index, so a
	// filter that is going to be refused never pays for that query, and the
	// refusal rather than an index failure is what reaches the caller.
	//
	// An empty term leaves the set unfiltered, matching every other surface that
	// takes one (cmd/list.go only sets the field for a non-empty -S): "no
	// keyword filter" is a real meaning, unlike an empty id, which names no nib
	// and is refused above.
	if filter.Search != nil && *filter.Search != "" {
		var err error
		if result, err = filterBySearch(ctx, result, *filter.Search, reader); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// filterBySearch INTERSECTS the working set with the search index's answer,
// keeping the input's own order. It never chooses the set: a hit outside the
// relation the caller named is not a child of X, so admitting it would answer a
// different question. queryResolver.Nibs seeds from the index instead, and says
// there why.
//
// An index that cannot answer is reported rather than read as "nothing matched",
// even though on a relationship field that costs the WHOLE response — every such
// field is [Nib!]!, so GraphQL's null propagation carries the failure to the
// root and discards every other nib's successful result. That is accepted:
// degrading to "no match" would put this branch back in the business of
// answering a question it could not evaluate, and a malformed term is not what
// gets here — a query string the parser rejects degrades to a plain match query
// instead of failing (see search.Index.Search).
//
// Membership is decided by ID. The relationship resolvers hand ApplyFilter
// detached snapshots (see NibReader.GetSnapshot) while the reader hands back its
// own store pointers, so comparing pointers would match nothing.
//
// It reads the UNCAPPED answer, not the top hits Search returns, because the
// working set is already bounded by the relation — see hasBoundingFilter for
// which population a store-wide cap would truncate. cachedSearchAllIDs memoizes
// the membership set itself, so the per-parent cost is |relation| lookups and
// nothing proportional to the match set.
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
// chain (see WalkParentChain for both rules).
//
// The visited set is per-call and seeded with b.ID, so each candidate gets an
// independent walk that keeps b off its own chain. Two constraints on any
// memoization added here, both of which fail silently with the suite green:
//
//   - Cache the per-id ANSWER, not the visited flag. One set shared across
//     candidates makes a later walk stop at an ancestor an earlier one banked,
//     truncating its chain before the target can be reached or ruled out.
//   - Keep reader as the source of ancestry. ApplyFilter runs over genuinely
//     narrowed slices from the relationship resolvers, so indexing the candidate
//     slice instead would truncate every chain at the first filtered-out
//     ancestor — `--status todo --ancestor <epic>` has to reach through a
//     completed intermediate.
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
// CANONICAL INVARIANT (what "has a parent" means). This doc is its single
// authoritative statement; comments across internal/graph and cmd defer here
// rather than re-derive it. resolvedParentID is its ID-shaped wrapper and is
// what most callers reach for, so finding every decision point means asking for
// references to BOTH names.
//
// Deciding parent-ness from the raw b.Parent string is the mistake this exists
// to prevent, and a dangling link is what separates the two readings: the stored
// field is non-empty, but nothing can be fetched through it. Under the raw
// reading such a nib presents as a root everywhere the object graph is walked,
// yet reports as parented to a filter, is missing from a root-level sibling
// query, and is offered as a root's sibling by one surface while being refused
// as that root's reorder anchor by another.
//
// Reading b.Parent is not itself the mistake — most raw reads in this package
// are legitimate. Deciding parent-ness from whether that string is empty is.
//
// Surfaces outside this package read the raw link to decide root-ness, so what
// is worth checking of them is agreement on the ANSWER rather than on the
// reading: a link naming no nib is absent from the id set ui.BuildTree and
// membership.View each walk, so for a dangling link both reach this rule's
// answer without calling it. That dangling link is the whole of the claim.
//
// Resolving is also what makes a short-form link compare under its resolved
// spelling. Canonicalization (see canonicalize.go) makes the two coincide, but
// this does not lean on that: it stays correct on a reader that never ran the
// pass, so the rule is a property of this package rather than of the store's
// history.
//
// The returned pointer is the reader's LIVE store pointer (see NibReader.Get):
// a caller whose result outlives the store lock must snapshot it, see
// NibReader.GetSnapshot for the copy-on-write invariant.
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

// resolvedParentID is resolvedParent in ID shape — the parent's resolved ID, or
// "" when b has no parent AND when b's parent link names no nib — which is how
// the rest of the nib surface presents parent-ness. See resolvedParent for the
// rule itself, why it has one home, and how to audit the surfaces bound to it.
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
// Do not add a Get here for symmetry with its two siblings: never fetching the
// target is what lets a concurrent delete simply drop the target off every
// chain, rather than needing a defensive nil return.
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
// The chain is walked once up front rather than per candidate. targetID must
// already be a full (normalized) ID; the Get can still fail on a concurrent
// delete, which is what FilterTargetUnreadableError is for.
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
// excluded. A parentless target selects the other root nibs — the same answer
// fetchSiblings gives `nibs rel siblings` — which falls out of matching on the
// target's empty parent instead of needing its own case. Both sides go through
// resolvedParentID, so root-ness means the same thing here as it does there.
// targetID must already be a full (normalized) ID; see filterByDescendantID for
// the unreadable case.
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

// refuseUndeclaredArea reports why an area filter cannot run, or nil when the
// value names a declared area.
//
// The empty string is refused for the reason resolveFilterTarget refuses an
// empty id — read as "unset" the branch would be dropped and the query would
// widen to the whole store — and is tested EXACTLY for the same reason, leaving
// a whitespace-only value an ordinary undeclared path.
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
// it in the DECLARED tree (Areas.IsWithin). ancestor has already been checked as
// declared, so a candidate is judged only on its own value — and a stored value
// the vocabulary no longer declares is within nothing, which is how a retired
// area stays out of an answer about the tree that no longer holds it.
func filterByAreaWithin(nibs []*nib.Nib, areas *config.Areas, ancestor string) []*nib.Nib {
	var result []*nib.Nib
	for _, b := range nibs {
		if areas.IsWithin(b.Area, ancestor) {
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
// any missing ancestor nibs, so the client can build a complete tree hierarchy
// even when search or filters matched only leaves.
//
// One visited set spans the WHOLE batch, seeded with every input nib's ID, and
// that batch-wide lifetime is what keeps the output free of duplicates. It is
// the opposite of the per-call set parentChain needs; see WalkParentChain.
//
// The added ancestors are the reader's live store pointers, exactly as the
// input nibs are. queryResolver.Nibs — this function's only caller — snapshots
// the whole result before handing it to gqlgen, so nothing detaches here.
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
