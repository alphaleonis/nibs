package graph

import "github.com/alphaleonis/nibs/internal/nib"

// ParentStep resolves one hop up the parent chain: given a nib, it returns that
// nib's parent, or nil when the chain ends there.
//
// The step — not WalkParentChain — owns three decisions, so each walk states
// them at its own call site rather than inheriting them from the traversal:
//
//   - What "no parent" means. A step MUST return nil for a link that names no
//     nib, so an unresolvable link ends the chain instead of putting an id no
//     nib answers to on it. That is the resolved-parent rule (see
//     resolvedParent for the rule and why it has one home), and it is what
//     makes the walk agree with every other hierarchy surface.
//   - Whether a failure to resolve is an error or an ending. Returning an error
//     aborts the walk and discards the partial chain; returning nil ends the
//     chain and keeps everything reached before it.
//   - Whether the walk yields live store pointers or detached snapshots.
//     WalkParentChain hands back exactly what the step returned, so a caller
//     whose result outlives the store lock must supply a snapshotting step (see
//     NibReader.GetSnapshot for the live-pointer / copy-on-write invariant).
type ParentStep func(*nib.Nib) (*nib.Nib, error)

// WalkParentChain walks up from start via step and returns the ancestors it
// reached, nearest first. depth caps the number of hops; depth < 0 walks to the
// root.
//
// It is the graph layer's one read-only parent-chain traversal: the hierarchy
// filters, the ancestor-completion step, and the `rel ancestors` CLI walk all
// run on it.
//
// Walks outside the graph layer are deliberately not on it, for one of two
// reasons. A walk that WRITES as it goes carries its own partial-failure
// semantics that a read primitive has no place deciding (Resolver
// .activateParentChain). A walk at the PRESENTATION layer has no NibReader at
// all — it works over an already-fetched set and answers "does this parent link
// resolve" by membership in it, which is a re-derivation of the rule rather
// than a call to it (see resolvedParent).
//
// visited belongs to the CALLER, and its lifetime is the one thing the walks
// disagree on, so it is a parameter rather than an internal detail:
//
//   - A per-call set seeded with start.ID gives an independent walk that keeps
//     start off its own chain. Two walks sharing nothing may each report the
//     same ancestor, which is what a per-candidate predicate wants.
//   - A set that outlives one call memoizes across walks: an ancestor already
//     banked in it is neither reported again nor walked through again. A batch
//     completing every member's ancestry wants exactly that.
//   - A nil set is neither, and is accepted rather than a precondition: the
//     walk allocates one for itself, so it memoizes nothing beyond this call
//     and seeds nothing, which means a cycle through start reports start.
//
// Termination is a SEPARATE guard from the seed — the seed alone does not
// provide it. It comes from the check-and-mark pair below: every iteration
// either banks a new id or stops, so visited strictly grows and the walk is
// bounded by the number of nibs in the store. Cycles should not exist (the
// mutation resolvers reject them), but a hand-edited nib file can still produce
// one, and an unguarded parent walk hangs on it.
//
// Ids are banked as the RESOLVED ids the step hands back, never the spelling
// stored in a child's parent field. A short-form link (`parent: e1`) resolves
// to a nib whose ID carries the configured prefix, so banking the raw spelling
// would both put an id no nib answers to on the chain and fail to match the
// same ancestor reached under its full spelling. Canonicalization makes the two
// spellings coincide for every link that was resolvable at load or when its
// target arrived — but that is a load-time and watcher-batch invariant, not a
// store invariant. Later store mutations can reintroduce the divergence,
// notably a delete that promotes a bare-token id to its prefixed twin, and a
// reader that never ran the pass never had the coincidence at all. Banking
// resolved ids is what keeps the walk correct in both states.
func WalkParentChain(start *nib.Nib, step ParentStep, visited map[string]bool, depth int) ([]*nib.Nib, error) {
	if visited == nil {
		visited = make(map[string]bool)
	}
	var chain []*nib.Nib
	cur := start
	for steps := 0; depth < 0 || steps < depth; steps++ {
		parent, err := step(cur)
		if err != nil {
			return nil, err
		}
		if parent == nil {
			break
		}
		if visited[parent.ID] {
			break
		}
		visited[parent.ID] = true
		chain = append(chain, parent)
		cur = parent
	}
	return chain, nil
}

// liveParentStep is the ParentStep for a walk over the reader: it applies the
// resolved-parent rule, so a link that names no nib ends the chain rather than
// failing the walk. It hands back the reader's LIVE store pointers (see
// NibReader.Get) — callers whose result reaches gqlgen must snapshot it.
func liveParentStep(reader NibReader) ParentStep {
	return func(b *nib.Nib) (*nib.Nib, error) {
		return resolvedParent(b, reader), nil
	}
}

// liveParentChain walks b's ancestors through reader, to the root, banking them
// in the caller's visited set. Every rung is one of the reader's LIVE store
// pointers, which the name carries because the return type does not — see
// NibReader.GetSnapshot for the copy-on-write invariant a caller outliving the
// store lock has to satisfy.
//
// It is the single place the filter pipeline's chain walk decides what a
// traversal failure means: liveParentStep never reports an error — an
// unresolvable link ends the chain — so the walk's error return is always nil
// here and is discarded rather than propagated to callers that have nowhere to
// put it.
func liveParentChain(b *nib.Nib, reader NibReader, visited map[string]bool) []*nib.Nib {
	chain, _ := WalkParentChain(b, liveParentStep(reader), visited, -1)
	return chain
}
