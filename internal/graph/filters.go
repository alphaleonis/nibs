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
//   - Semantic naming at the call site: `resolveFilterID` reads as "resolve a
//     filter ID" rather than "normalize a generic ID", making intent explicit.
//   - Future extension point: this is the single place where every
//     filter.*ID branch can gain a shared id TRANSFORM — trimming, case
//     folding, an input-length cap, should any of those ever be wanted —
//     without touching each branch individually. None is implemented: a filter
//     id is whatever length the caller sends, and an unresolvable one travels
//     verbatim into FilterTargetNotFoundError.ID. It does not reach the response
//     in full — that error's Error() caps the echo at maxEchoedIDBytes, and the
//     echo is where the amplification lives, since a relationship field refuses
//     once per nib while the request body bounds the input at 1x. See echoID for
//     the measurement. A cap HERE would be a
//     different thing from that one: it would change which ids RESOLVE, not
//     what a refusal prints.
//
// Its sole caller is resolveFilterTarget, which owns the refusal policy layered
// on top: this function reports whether an id resolves, that one decides what an
// id that does not resolve — or that is not an id at all — means. A transform
// added here therefore runs AFTER the emptiness test, so a trim would turn a
// whitespace-only id into an empty one this layer then hands to NormalizeID; a
// trim meant to read as "empty" has to go above the test, in resolveFilterTarget.
func resolveFilterID(reader NibReader, id string) (string, bool) {
	return reader.NormalizeID(id)
}

// resolveFilterTarget turns one id-valued filter field into the full id its
// branch matches on, or reports why the branch cannot run. It is the single
// place both refusals are decided, and every filter.*ID branch in ApplyFilter
// goes through it:
//
//   - The empty string is *FilterTargetEmptyError — malformed input, not a
//     question about the store.
//   - An id no nib answers to is *FilterTargetNotFoundError, so an unknown
//     target fails the whole filter chain instead of narrowing it.
//
// Both decisions live here rather than being spelled out per branch because a
// per-branch copy of the emptiness test can be half-applied, and a branch that
// reads an empty id as "unset" skips itself and silently widens the query to
// the whole store. One copy cannot be half-applied. What stays per branch is the
// field name and the `if err != nil` return, so each branch still names its own
// schema field in the error and the refusal stays grep-findable at the call
// site.
//
// The two cases are ordered, and the order is the whitespace policy: emptiness
// is an EXACT test that runs first, so only "" is malformed input and every
// other value — including a whitespace-only one — is an id that gets its lookup
// and is reported as not-found if it misses. Trimming here would contradict the
// id FilterTargetNotFoundError echoes back, and would make this layer stricter
// than cmd/list.go's flag checks, which test for "" the same exact way.
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
// ApplyFilter handle ID resolution via resolveFilterTarget. Pre-normalizing in
// the caller is not required for correctness, but mixing short and full
// forms across resolvers within the same request will desync the cache
// keys (keyed on the full normalized ID) and silently degrade memoization.
//
// Four outcomes are kept distinct, and the error return exists to separate the
// first three from the fourth:
//
//   - A filter field naming a single nib was given the empty string:
//     *FilterTargetEmptyError, the validation class — malformed input rather
//     than a question about the store.
//   - A filter field naming a single nib was given an id no nib answers to:
//     *FilterTargetNotFoundError, carrying nib.ErrNotFound.
//   - A target that resolved could not then be fetched:
//     *FilterTargetUnreadableError, deliberately not a not-found.
//   - Nothing matched: an empty result and a nil error.
//
// Folding any of the three refusals into the last is what this signature exists
// to prevent. "What is under nibs-abc1?" answered with an empty list is a
// factual claim about the store, and a caller that mistyped the id cannot tell
// it apart from the truth. An empty id is worse still: read as "unset" it drops
// its branch outright, so the query widens to every nib in the store and
// answers a question nobody asked.
func ApplyFilter(ctx context.Context, nibs []*nib.Nib, filter *model.NibFilter, reader NibReader, blocking BlockingChecker) ([]*nib.Nib, error) {
	if filter == nil {
		return nibs, nil
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
	// field is non-empty" — see resolvedParent for why, and for the surfaces
	// this has to agree with.
	result = filterByPredicate(result, filter.HasParent, func(b *nib.Nib) bool {
		return resolvedParentID(b, reader) != ""
	})
	if filter.ParentID != nil {
		// Normalize the parent id like every other *ID filter: the loader
		// canonicalizes stored link ids, so b.Parent is normally a full (prefixed) id and
		// a short --parent must be resolved first or it silently matches
		// nothing. An unknown target fails the filter (shared contract for all
		// *ID filters).
		//
		// Matching the raw b.Parent agrees with resolvedParentID for any nib whose
		// link that pass rewrote: it is stored in full form afterwards, so only a
		// link naming the resolved target can match. The two part ways wherever
		// the pass left a resolvable link alone — a reader that never ran it, or a
		// store state it does not cover, such as a short-form link the pass
		// matched exactly whose target a later delete promoted to its prefixed
		// twin. There a raw short-form link fails this comparison while
		// resolvedParentID, and so hasParent:true, still resolves it. A link
		// naming no nib matches neither, under either reading.
		fullID, err := resolveFilterTarget(reader, "parentId", *filter.ParentID)
		if err != nil {
			return nil, err
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
	// An unknown target fails the filter (shared contract for all *ID filters).
	// Each guard decides its branch's outcome, and what it prevents differs by
	// branch. Drop the ancestorId guard and the branch answers with the empty
	// set: resolveFilterID echoes the input back on a miss, and parentChain
	// banks only fetched nibs' ids, so the echoed string matches nothing. Drop
	// the descendantId or siblingId guard and the branch instead reports the
	// unreadable class, because each opens with a Get that fails the same
	// exact-then-prefix lookup NormalizeID just did — a refusal, but the wrong
	// one, naming an internal read failure for what is a caller's typo.
	//
	// The empty set is the answer worth guarding hardest against: "what is under
	// this nib" reads as a fact about the store rather than as a rejected
	// question.
	//
	// Cost: filterByAncestorID walks each candidate's chain independently
	// instead of sharing work between candidates, which is O(N x depth) reader
	// lookups. Deliberate. Reader.Get is an in-memory map lookup under an
	// RLock, so the walk is bounded by tree depth rather than I/O, and the
	// obvious sharing is unsound: one seen set across candidates makes a later
	// candidate's walk stop at an ancestor an earlier one already marked,
	// truncating its chain before the target can be reached or ruled out. Any
	// memoization must cache the per-id ANSWER, not the visited flag.
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

	// Blocking filters (computed via BlockingChecker)
	result = filterByPredicate(result, filter.HasBlocking, func(b *nib.Nib) bool { return blocking.IsBlocking(b.ID) })
	result = filterByPredicate(result, filter.IsBlocked, func(b *nib.Nib) bool { return blocking.IsBlocked(b.ID) })

	// BlockingID (special: needs reader to look up target nib).
	// An unknown target fails the filter (shared contract for all *ID filters).
	if filter.BlockingID != nil {
		fullID, err := resolveFilterTarget(reader, "blockingId", *filter.BlockingID)
		if err != nil {
			return nil, err
		}
		if result, err = filterByBlockingID(result, fullID, reader); err != nil {
			return nil, err
		}
	}

	// Blocked-by filters (from direct blocked_by field)
	result = filterByPredicate(result, filter.HasBlockedBy, func(b *nib.Nib) bool { return len(b.BlockedBy) > 0 })
	if filter.BlockedByID != nil {
		fullID, err := resolveFilterTarget(reader, "blockedById", *filter.BlockedByID)
		if err != nil {
			return nil, err
		}
		result = filterBySliceField(result, []string{fullID}, func(b *nib.Nib) []string { return b.BlockedBy })
	}

	// Mention filters (computed via FindMentions/FindMentionedBy on the reader,
	// routed through the per-request cache so repeated lookups within one
	// GraphQL operation don't re-run the reader).
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

	return result, nil
}

// filterByMentionsID keeps nibs that mention the given target in their body.
// targetID must already be a full (normalized) ID — callers resolve via
// resolveFilterTarget before invoking. Routes through the per-request mention
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
// resolveFilterTarget before invoking. Routes through the per-request mention
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
// walking up to the root. Every id is a RESOLVED id — the ID of a nib that was
// actually fetched — and a link that resolves to nothing ends the chain; see
// WalkParentChain for both rules.
//
// The visited set is per-call and seeded with b.ID, so each candidate gets an
// independent walk and b's own ID is never in its result. Sharing one set
// across candidates would be unsound here for a second reason beyond the
// seeding: a later candidate's walk would stop at an ancestor an earlier one
// banked, truncating its chain before the target could be reached or ruled out.
// Any memoization must cache the per-id ANSWER, not the visited flag.
//
// Ancestry is always resolved through reader, never from the candidate slice
// the caller is filtering. ApplyFilter runs over genuinely narrowed slices
// from the relationship resolvers (Children, Blocking, Mentions, ...), so an
// intermediate ancestor an earlier filter removed is still on the chain, and
// `--status todo --ancestor <epic>` still reaches through a completed
// intermediate. Any memoization added here must keep reader as the source of
// ancestry: indexing the candidate slice instead would truncate every chain at
// the first filtered-out ancestor, silently and with the suite still green.
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
// This function body is the project's one definition of "has a parent".
// resolvedParentID is its ID-shaped wrapper, and is what most callers reach
// for — so finding every decision point means asking for references to BOTH
// names, not just this one. Do not restate the rationale below at a call site.
//
// Deciding parent-ness from the raw b.Parent string instead is the mistake this
// exists to prevent, and prose has not been able to prevent it: a surface that
// needs the rule without a reader in hand has to re-derive it against whatever
// it does have, and every such re-derivation is a place the rule can drift.
// Each one is expected to say what its equivalence rests on. This comment
// deliberately does not enumerate them — successive attempts to keep a list
// here have each gone stale, which is why enforcement is being moved out of
// prose entirely.
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
// matches the raw b.Parent, which agrees with this helper for any nib whose
// stored link the loader's canonicalization pass rewrote. See that branch for
// the states where the two part ways.
//
// Resolving is also what compares a short-form link under its resolved
// spelling. Canonicalization makes the two spellings coincide for every link
// that was resolvable at load or when its target arrived — but that is a
// load-time and watcher-batch invariant, not a store invariant. Later store
// mutations can reintroduce the divergence, notably a delete that promotes a
// bare-token id to its prefixed twin. Resolving on every call is what keeps the
// helper correct in that state, and on a reader that never ran the pass at all.
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
// the target's descendants at any depth, the target itself excluded.
// targetID must already be a full (normalized) ID — callers resolve via
// resolveFilterTarget before invoking.
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
// resolveFilterTarget before invoking. Reports *FilterTargetUnreadableError if the
// target nib cannot be fetched (defensive: the caller already proved the ID
// resolves, but Get may still fail on a concurrent delete — which is why that
// is its own class and not a not-found).
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
// excluded. A parentless target selects the other root nibs — the root-level
// branch fetchSiblings implements for `nibs rel siblings` — which falls out of
// matching on the target's empty parent instead of needing its own case.
// Both sides go through resolvedParentID, so root-ness means the same thing
// here as it does to nibResolver.Parent and fetchSiblings; see that helper for
// the rule and why it has a single home.
// targetID must already be a full (normalized) ID — callers resolve via
// resolveFilterTarget before invoking. Reports *FilterTargetUnreadableError if the
// target nib cannot be fetched (defensive: see filterByBlockingID).
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
//
// One visited set spans the WHOLE batch, seeded with every input nib's ID: an
// ancestor already in the result — or already added while completing an earlier
// nib — is neither re-added nor walked through a second time. That batch-wide
// lifetime is what keeps the output free of duplicates, and it is the opposite
// of the per-call set parentChain needs; see WalkParentChain.
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

// filterByBlockingID filters nibs that are blocking a specific nib ID.
// Computed: checks if targetID has this nib in its blockedBy.
// targetID must already be a full (normalized) ID — callers resolve via
// resolveFilterTarget before invoking. Reports *FilterTargetUnreadableError if the
// target nib cannot be fetched (defensive: the caller already proved the ID
// resolves, but Get may still fail on a concurrent delete — which is why that
// is its own class and not a not-found).
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
