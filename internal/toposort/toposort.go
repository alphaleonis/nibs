// Package toposort provides a deterministic topological sort with cycle
// reporting. Edges reference nodes that may or may not be in the input
// slice — edges to foreign nodes are silently ignored so callers can mix
// local and external references without pre-filtering.
package toposort

// Sort topologically orders nodes by edges. Each edge is [from, to] meaning
// "from must come before to". Edges to nodes outside the `nodes` slice are
// ignored. Returns ordered nodes (respecting input order as tiebreaker) plus
// cycles — each cycle is a list of node IDs forming the loop.
//
// When there are cycles, nodes participating in any cycle are omitted from
// `ordered`; cycle-free prefix/suffix is still returned.
//
// Algorithm: Kahn's algorithm with stable tiebreaking. Ready nodes (in-degree
// 0) are picked by scanning the original `nodes` slice in order, preserving
// the caller-provided sequence as a deterministic tiebreaker. Duplicate edges
// are collapsed via a per-pair set so in-degrees count each precedence once.
// After Kahn terminates, any remaining nodes are part of or downstream of a
// cycle; cycles are reported as the strongly connected components containing
// more than one node (plus self-loops).
func Sort(nodes []string, edges [][2]string) (ordered []string, cycles [][]string) {
	// Build index of valid nodes for O(1) edge filtering.
	idx := make(map[string]int, len(nodes))
	for i, n := range nodes {
		idx[n] = i
	}

	// Collapse duplicate edges and drop edges to nodes outside `nodes`.
	// `seen` is keyed on an anonymous struct so tarjanSCCs (which doesn't
	// import the local `edge` type) can consume the same map for
	// self-loop detection without an intermediate copy.
	seen := make(map[struct{ from, to string }]struct{})
	successors := make(map[string][]string, len(nodes))
	inDeg := make(map[string]int, len(nodes))
	for _, n := range nodes {
		inDeg[n] = 0
	}
	for _, e := range edges {
		from, to := e[0], e[1]
		if _, ok := idx[from]; !ok {
			continue
		}
		if _, ok := idx[to]; !ok {
			continue
		}
		key := struct{ from, to string }{from, to}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		successors[from] = append(successors[from], to)
		inDeg[to]++
	}

	// Kahn's algorithm with stable tiebreaking: scan `nodes` in order and
	// pick the first ready (in-degree 0) node not yet emitted. O(V² + E)
	// in the worst case — the linear rescan of `nodes` for each emission
	// dominates when V is large; for nib-count-sized inputs (tens to
	// hundreds) this is negligible.
	emitted := make(map[string]struct{}, len(nodes))
	ordered = make([]string, 0, len(nodes))
	for len(emitted) < len(nodes) {
		progress := false
		for _, n := range nodes {
			if _, done := emitted[n]; done {
				continue
			}
			if inDeg[n] != 0 {
				continue
			}
			ordered = append(ordered, n)
			emitted[n] = struct{}{}
			for _, succ := range successors[n] {
				inDeg[succ]--
			}
			progress = true
			break
		}
		if !progress {
			break
		}
	}

	if len(emitted) == len(nodes) {
		return ordered, nil
	}

	// Remaining nodes are in or downstream of cycles. Find strongly
	// connected components among them using Tarjan's algorithm. Any SCC
	// with more than one node is a cycle; a single-node SCC is a cycle
	// iff it has a self-loop. Non-cycle singletons are nodes downstream
	// of a cycle — they're already excluded from `ordered` and we do NOT
	// report them as cycles (they'd mislead consumers).
	remaining := make([]string, 0, len(nodes)-len(emitted))
	inRemaining := make(map[string]struct{})
	for _, n := range nodes {
		if _, done := emitted[n]; !done {
			remaining = append(remaining, n)
			inRemaining[n] = struct{}{}
		}
	}
	cycles = tarjanSCCs(remaining, successors, inRemaining, seen)

	return ordered, cycles
}

// tarjanSCCs returns strongly connected components among `nodes`, restricting
// edge traversal to edges whose targets are also in `nodes`. Only SCCs that
// actually form a cycle are returned: multi-node SCCs (by definition) or
// single-node SCCs with a self-loop. Other singletons are downstream of
// cycles and are intentionally omitted from the result.
//
// The order of nodes within each returned SCC follows the order in which
// Tarjan popped them from its stack — this is deterministic given a fixed
// input but not guaranteed to be a "walk around the cycle". Callers should
// treat each cycle as an unordered set.
func tarjanSCCs(nodes []string, successors map[string][]string, inNodes map[string]struct{}, edgeSet map[struct{ from, to string }]struct{}) [][]string {
	type state struct {
		index, lowlink int
		onStack        bool
	}
	states := make(map[string]*state)
	stack := make([]string, 0, len(nodes))
	nextIndex := 0
	var sccs [][]string

	var strongconnect func(v string)
	strongconnect = func(v string) {
		s := &state{index: nextIndex, lowlink: nextIndex, onStack: true}
		states[v] = s
		nextIndex++
		stack = append(stack, v)
		for _, w := range successors[v] {
			if _, ok := inNodes[w]; !ok {
				continue
			}
			ws, visited := states[w]
			if !visited {
				strongconnect(w)
				if states[w].lowlink < s.lowlink {
					s.lowlink = states[w].lowlink
				}
			} else if ws.onStack {
				if ws.index < s.lowlink {
					s.lowlink = ws.index
				}
			}
		}
		if s.lowlink == s.index {
			var comp []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				states[w].onStack = false
				comp = append(comp, w)
				if w == v {
					break
				}
			}
			// A single-node SCC is only a cycle when there's a self-loop.
			// Multi-node SCCs are always cycles by definition.
			if len(comp) > 1 {
				sccs = append(sccs, comp)
			} else if _, selfLoop := edgeSet[struct{ from, to string }{comp[0], comp[0]}]; selfLoop {
				sccs = append(sccs, comp)
			}
		}
	}

	for _, n := range nodes {
		if _, visited := states[n]; !visited {
			strongconnect(n)
		}
	}
	return sccs
}
