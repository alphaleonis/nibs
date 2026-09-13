// Package toposort sorts nodes topologically and deterministically, and reports
// cycles. Edges touching a node outside the input are ignored.
package toposort

// Sort orders nodes so that for each edge [from, to], from comes before to, with
// input order breaking ties. Duplicate edges count once. Nodes in a cycle, and
// every node downstream of one, are left out of ordered; cycles lists each cycle
// (a multi-node strongly connected component, or a self-loop) as an unordered set.
func Sort(nodes []string, edges [][2]string) (ordered []string, cycles [][]string) {
	idx := make(map[string]int, len(nodes))
	for i, n := range nodes {
		idx[n] = i
	}

	// Drop edges touching foreign nodes and collapse duplicates; tarjanSCCs also
	// reads seen to detect self-loops.
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

	// Kahn's algorithm, emitting the first ready node in input order. The rescan
	// per emission makes it O(V² + E).
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

	// What remains is in or downstream of a cycle; report only the cycles.
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

// tarjanSCCs returns the strongly connected components among nodes that form a
// cycle: more than one node, or one node with a self-loop. Node order within a
// component is Tarjan's pop order, not a walk around the cycle.
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
