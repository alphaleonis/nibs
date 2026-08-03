package graph

import (
	"context"
	"slices"

	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
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
//   - Future extension point: this is the single place where every
//     filter.*ID branch can gain shared behavior — e.g. the 128-char
//     input-length cap — without touching each
//     branch individually.
//
// Every filter.*ID branch in ApplyFilter pairs this call with an explicit
// `if !ok { return nil }` so an unknown target short-circuits the whole
// filter chain to nil. The pattern is repeated by design rather than folded
// into a loop: spelled out per branch it stays grep-findable.
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
// RequestCache lookup). ApplyFilter does not check cancellation or honor
// deadlines — passing a canceled ctx will not short-circuit; every filter
// branch runs to completion.
//
// Callers threading filter.MentionsID / filter.MentionedByID must let
// ApplyFilter handle ID resolution via resolveFilterID. Pre-normalizing in
// the caller is not required for correctness, but mixing short and full
// forms across resolvers within the same request will desync the cache
// keys (keyed on the full normalized ID) and silently degrade memoization.
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

	// Parent predicate filters. Parent-ness is "the link resolves", not "the
	// field is non-empty" — see resolvedParentID for why, and for the surfaces
	// this has to agree with.
	result = filterByPredicate(result, filter.HasParent, func(b *nib.Nib) bool {
		return resolvedParentID(b, reader) != ""
	})
	if filter.ParentID != nil && *filter.ParentID != "" {
		// Normalize the parent id like every other *ID filter: the loader
		// canonicalizes stored link ids, so b.Parent is a full (prefixed) id and
		// a short --parent must be resolved first or it silently matches
		// nothing. Unknown target short-circuits to nil (shared contract for all
		// *ID filters).
		//
		// Matching the raw b.Parent agrees with resolvedParentID for any nib that
		// came through that canonicalization pass: a resolvable link is stored in
		// full form there, so only a link naming the resolved target can match.
		// On a reader that skipped the pass the two part ways — a raw short-form
		// link fails this comparison while resolvedParentID, and so hasParent:true,
		// still resolves it. A link naming no nib matches neither, under either
		// reading.
		fullID, ok := resolveFilterID(reader, *filter.ParentID)
		if !ok {
			return nil
		}
		result = filterByField(result, []string{fullID}, func(b *nib.Nib) string { return b.Parent })
	}

	// Transitive hierarchy filters. Like every other *ID filter, each field
	// names the relationship the MATCHED nib holds toward the supplied target:
	// ancestorId keeps nibs whose ancestor is the target (its descendants),
	// descendantId keeps nibs whose descendant is the target (its ancestors),
	// siblingId keeps nibs sharing the target's parent. The target itself is
	// none of those things to itself, so all three exclude it here.
	//
	// "Here" is load-bearing: queryResolver.Nibs runs includeAncestors AFTER
	// ApplyFilter whenever search is set, re-adding every survivor's ancestors
	// so the client can render a complete tree. For ancestorId that puts the
	// target back into the response, and for siblingId it brings in the shared
	// parent. The schema descriptions state this; it is not a bug to fix in
	// this file — the web UI's tree rendering and ancestor dimming depend on
	// that completion.
	//
	// Unknown target short-circuits to nil (shared contract for all *ID
	// filters). Unlike the five pre-existing *ID branches, these three guards
	// are not independently observable: resolveFilterID echoes the input back
	// on a miss, and what the echoed string is then matched against can only
	// hold ids that DO resolve — a parentChain banks fetched nibs' ids, and
	// filterByDescendantID/filterBySiblingID open with a Get applying the same
	// exact-then-prefix rule NormalizeID just failed. So an unresolvable target
	// already matches nothing without them. They are kept anyway: the contract
	// stays grep-findable across all eight branches, and they become
	// load-bearing the moment NormalizeID stops echoing on a miss.
	//
	// Cost: filterByAncestorID walks each candidate's chain independently
	// instead of sharing work between candidates, which is O(N x depth) reader
	// lookups. Deliberate. Reader.Get is an in-memory map lookup under an
	// RLock, so the walk is bounded by tree depth rather than I/O, and the
	// obvious sharing is unsound: one seen set across candidates makes a later
	// candidate's walk stop at an ancestor an earlier one already marked,
	// truncating its chain before the target can be reached or ruled out. Any
	// memoization must cache the per-id ANSWER, not the visited flag.
	if filter.AncestorID != nil && *filter.AncestorID != "" {
		fullID, ok := resolveFilterID(reader, *filter.AncestorID)
		if !ok {
			return nil
		}
		result = filterByAncestorID(result, fullID, reader)
	}
	if filter.DescendantID != nil && *filter.DescendantID != "" {
		fullID, ok := resolveFilterID(reader, *filter.DescendantID)
		if !ok {
			return nil
		}
		result = filterByDescendantID(result, fullID, reader)
	}
	if filter.SiblingID != nil && *filter.SiblingID != "" {
		fullID, ok := resolveFilterID(reader, *filter.SiblingID)
		if !ok {
			return nil
		}
		result = filterBySiblingID(result, fullID, reader)
	}

	// Blocking filters (computed via BlockingChecker)
	result = filterByPredicate(result, filter.HasBlocking, func(b *nib.Nib) bool { return blocking.IsBlocking(b.ID) })
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
// targetID must already be a full (normalized) ID — callers resolve via
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
// sourceID must already be a full (normalized) ID — callers resolve via
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

// parentChain returns the IDs on b's parent chain, nearest ancestor first,
// walking up to the root.
//
// Every id in the chain is a RESOLVED id — the ID of a nib that was actually
// fetched — never the spelling stored in the child's parent field. Reader.Get
// follows a short-form link (`parent: e1`) by prepending the configured
// prefix, so banking the raw spelling would put an id that no nib answers to
// on the chain while the walk carried on past it, reporting a hierarchy with a
// missing rung. Through a loaded store the loader has already canonicalized
// every resolvable link id, so the raw and resolved spellings coincide; banking
// the resolved one costs nothing there and keeps the walk correct for a reader
// that has not run that pass. By the same rule a parent that cannot be fetched
// contributes nothing — the walk ends there and the chain stops, rather than
// failing the query.
//
// Ancestry is always resolved through reader, never from the candidate slice
// the caller is filtering. ApplyFilter runs over genuinely narrowed slices
// from the relationship resolvers (Children, Blocking, Mentions, ...), so an
// intermediate ancestor an earlier filter removed is still on the chain, and
// `--status todo --ancestor <epic>` still reaches through a completed
// intermediate. Any memoization added here must keep reader as the source of
// ancestry: indexing the candidate slice instead would truncate every chain at
// the first filtered-out ancestor, silently and with the suite still green.
//
// b's own ID is never in the result: the visited set is seeded with it, so a
// cycle passing back through b stops there. Termination is a SEPARATE guard —
// the seed alone does not provide it — and comes from the `seen[parent.ID]`
// check-and-mark pair inside the loop: every iteration either records a new
// resolved id or breaks, so the set strictly grows and the walk is bounded by
// the number of nibs in the store. Cycles should not exist (the mutation
// resolvers reject them), but a hand-edited nib file can still produce one,
// and an unguarded `for parent != ""` walk hangs on it.
func parentChain(b *nib.Nib, reader NibReader) []string {
	var chain []string
	seen := map[string]bool{b.ID: true}
	parentID := b.Parent
	for parentID != "" {
		parent, err := reader.Get(parentID)
		if err != nil {
			break
		}
		if seen[parent.ID] {
			break
		}
		seen[parent.ID] = true
		chain = append(chain, parent.ID)
		parentID = parent.Parent
	}
	return chain
}

// resolvedParentID returns b's parent as the rest of the nib surface presents
// it: the parent's resolved ID, or "" when b has no parent AND when b's parent
// link names no nib.
//
// This is the project's one definition of "has a parent". Every decision point
// that needs the rule calls this function, so `grep resolvedParentID` is the
// authoritative list of them — do not restate the rationale below at a call
// site, and do not enumerate the call sites here. Both forms of duplication go
// stale silently, since nothing couples prose to the code.
//
// The rule has one home because a surface that re-derives it from the raw
// b.Parent string stays self-consistent while disagreeing with every other
// surface. A dangling link is what separates the two readings: the stored field
// is non-empty, but nothing can be fetched through it. Under the raw reading
// such a nib presents as a root everywhere the object graph is walked, yet
// reports as parented to a filter, is missing from a root-level sibling query,
// and is offered as a root's sibling by one surface while being refused as that
// root's reorder anchor by another.
//
// One caller is deliberately not routed through here: the parentId branch
// matches the raw b.Parent, which agrees with this helper for any nib that came
// through the loader's canonicalization pass. See that branch for what it does
// on a reader that has not.
//
// Resolving is also what compares a short-form link under its resolved
// spelling. Through a loaded store that case does not arise: the loader
// canonicalizes every resolvable link id to its full form at the disk-read
// boundary. Resolving anyway keeps the helper correct for a nib that has not
// been through that pass.
func resolvedParentID(b *nib.Nib, reader NibReader) string {
	if b.Parent == "" {
		return ""
	}
	parent, err := reader.Get(b.Parent)
	if err != nil {
		return ""
	}
	return parent.ID
}

// filterByAncestorID keeps nibs with targetID somewhere in their parent chain —
// the target's descendants at any depth, the target itself excluded.
// targetID must already be a full (normalized) ID — callers resolve via
// resolveFilterID before invoking.
//
// Unlike its two siblings this never fetches the target, so it has no
// defensive nil return for a concurrent delete: matching is decided entirely
// by the candidates' own chains. Since parentChain banks only resolved ids, a
// target deleted between resolveFilterID and the walk simply stops appearing
// on any chain, and its former descendants stop matching. Adding a Get here to
// look symmetric would change behavior, not tidy it.
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
// The chain is walked once up front rather than per candidate.
// targetID must already be a full (normalized) ID — callers resolve via
// resolveFilterID before invoking. Returns nil if the target nib cannot be
// fetched (defensive: the caller already proved the ID resolves, but Get may
// still fail on a concurrent delete).
func filterByDescendantID(nibs []*nib.Nib, targetID string, reader NibReader) []*nib.Nib {
	target, err := reader.Get(targetID)
	if err != nil {
		return nil
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
	return result
}

// filterBySiblingID keeps nibs sharing the target's parent, the target itself
// excluded. A parentless target selects the other root nibs — the root-level
// branch fetchSiblings implements for `nibs rel siblings` — which falls out of
// matching on the target's empty parent instead of needing its own case.
// Both sides go through resolvedParentID, so root-ness means the same thing
// here as it does to nibResolver.Parent and fetchSiblings; see that helper for
// the rule and why it has a single home.
// targetID must already be a full (normalized) ID — callers resolve via
// resolveFilterID before invoking. Returns nil if the target nib cannot be
// fetched (defensive: see filterByBlockingID).
func filterBySiblingID(nibs []*nib.Nib, targetID string, reader NibReader) []*nib.Nib {
	target, err := reader.Get(targetID)
	if err != nil {
		return nil
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
// targetID must already be a full (normalized) ID — callers resolve via
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
