package graph

import "github.com/alphaleonis/nibs/internal/nib"

// ParentStep resolves one hop up the parent chain: given a nib, it returns that
// nib's parent, or nil when the chain ends there.
//
// The step, not WalkParentChain, decides three things:
//
//   - What "no parent" means. Return nil for a link that names no nib (see
//     resolvedParent for the rule and its one home).
//   - Whether failing to resolve is an error or an ending. An error aborts the
//     walk and discards the partial chain; nil keeps what was reached.
//   - Whether the walk yields live store pointers or snapshots. WalkParentChain
//     returns what the step returned, so a caller whose result outlives the
//     store lock supplies a snapshotting step (see NibReader.GetSnapshot).
type ParentStep func(*nib.Nib) (*nib.Nib, error)

// WalkParentChain walks up from start via step and returns the ancestors
// reached, nearest first. depth caps the hops; depth < 0 walks to the root.
//
// visited belongs to the CALLER. Seed it with start.ID for an independent walk
// that keeps start off its own chain; share one across calls to memoize, so an
// ancestor already banked is neither reported nor walked through again; pass nil
// for neither — the walk allocates one, and a cycle through start reports start.
//
// Termination comes from the check-and-mark below, not the seed — the mutation
// resolvers reject cycles, but a hand-edited nib file can still produce one.
// Ids are banked as the step's RESOLVED ids: a short-form `parent: e1` resolves
// to a prefixed ID that the raw spelling would not match.
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

// liveParentStep is the ParentStep for a walk over the reader: a link that names
// no nib ends the chain rather than failing the walk. It returns the reader's
// LIVE store pointers (see NibReader.GetSnapshot) — snapshot before handing one
// to gqlgen.
func liveParentStep(reader NibReader) ParentStep {
	return func(b *nib.Nib) (*nib.Nib, error) {
		return resolvedParent(b, reader), nil
	}
}

// liveParentChain walks b's ancestors through reader, to the root, banking them
// in the caller's visited set. Every rung is one of the reader's LIVE store
// pointers (see NibReader.GetSnapshot). liveParentStep never reports an error,
// so the discarded error return is always nil.
func liveParentChain(b *nib.Nib, reader NibReader, visited map[string]bool) []*nib.Nib {
	chain, _ := WalkParentChain(b, liveParentStep(reader), visited, -1)
	return chain
}
