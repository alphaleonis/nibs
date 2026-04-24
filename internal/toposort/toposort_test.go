package toposort

import (
	"sort"
	"strings"
	"testing"
)

// nodeSet collects a cycle's participants into a set so tests don't over-
// specify the emitted order (which is implementation-defined per the
// package docs — consumers are told to treat cycles as unordered).
func nodeSet(nodes []string) map[string]bool {
	m := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		m[n] = true
	}
	return m
}

func TestSort_LinearChain(t *testing.T) {
	nodes := []string{"a", "b", "c"}
	edges := [][2]string{{"a", "b"}, {"b", "c"}}

	ordered, cycles := Sort(nodes, edges)
	if len(cycles) != 0 {
		t.Errorf("cycles = %v, want empty", cycles)
	}
	want := []string{"a", "b", "c"}
	if len(ordered) != len(want) {
		t.Fatalf("ordered = %v, want %v", ordered, want)
	}
	for i, n := range ordered {
		if n != want[i] {
			t.Errorf("ordered[%d] = %q, want %q", i, n, want[i])
		}
	}
}

func TestSort_ParallelIndependentNodes(t *testing.T) {
	// No edges — output must preserve the input order exactly. This pins
	// the stable-tiebreaking contract.
	nodes := []string{"x", "y", "z"}
	ordered, cycles := Sort(nodes, nil)
	if len(cycles) != 0 {
		t.Errorf("cycles = %v, want empty", cycles)
	}
	if len(ordered) != 3 {
		t.Fatalf("ordered len = %d, want 3", len(ordered))
	}
	for i, n := range ordered {
		if n != nodes[i] {
			t.Errorf("ordered[%d] = %q, want %q", i, n, nodes[i])
		}
	}
}

func TestSort_SimpleTwoCycle(t *testing.T) {
	nodes := []string{"a", "b"}
	edges := [][2]string{{"a", "b"}, {"b", "a"}}

	ordered, cycles := Sort(nodes, edges)
	if len(ordered) != 0 {
		t.Errorf("ordered = %v, want empty (both nodes in cycle)", ordered)
	}
	if len(cycles) != 1 {
		t.Fatalf("cycles = %v, want exactly 1", cycles)
	}
	if set := nodeSet(cycles[0]); !set["a"] || !set["b"] {
		t.Errorf("cycles[0] = %v, want {a, b}", cycles[0])
	}
}

func TestSort_ThreeCycle(t *testing.T) {
	nodes := []string{"a", "b", "c"}
	edges := [][2]string{{"a", "b"}, {"b", "c"}, {"c", "a"}}

	ordered, cycles := Sort(nodes, edges)
	if len(ordered) != 0 {
		t.Errorf("ordered = %v, want empty (all three in one cycle)", ordered)
	}
	if len(cycles) != 1 {
		t.Fatalf("cycles len = %d, want 1", len(cycles))
	}
	set := nodeSet(cycles[0])
	for _, n := range []string{"a", "b", "c"} {
		if !set[n] {
			t.Errorf("cycles[0] missing %q: got %v", n, cycles[0])
		}
	}
}

func TestSort_DisjointCycles(t *testing.T) {
	// Two independent cycles: {a↔b} and {c↔d}. e is standalone.
	nodes := []string{"a", "b", "c", "d", "e"}
	edges := [][2]string{
		{"a", "b"}, {"b", "a"},
		{"c", "d"}, {"d", "c"},
	}

	ordered, cycles := Sort(nodes, edges)
	// e is cycle-free and must appear in ordered.
	if len(ordered) != 1 || ordered[0] != "e" {
		t.Errorf("ordered = %v, want [e]", ordered)
	}
	if len(cycles) != 2 {
		t.Fatalf("cycles len = %d, want 2", len(cycles))
	}
	// Sort each cycle's members for stable comparison. We don't specify
	// which cycle comes first — just that both are present.
	gotCycles := make([]string, 0, 2)
	for _, cyc := range cycles {
		c := append([]string(nil), cyc...)
		sort.Strings(c)
		gotCycles = append(gotCycles, strings.Join(c, ","))
	}
	sort.Strings(gotCycles)
	want := []string{"a,b", "c,d"}
	for i, w := range want {
		if gotCycles[i] != w {
			t.Errorf("cycles[%d] = %q, want %q", i, gotCycles[i], w)
		}
	}
}

func TestSort_DuplicateEdgesCollapsed(t *testing.T) {
	// Same edge repeated three times. If the implementation double-counts
	// in-degrees, b would never become ready and a spurious cycle would
	// appear.
	nodes := []string{"a", "b"}
	edges := [][2]string{{"a", "b"}, {"a", "b"}, {"a", "b"}}

	ordered, cycles := Sort(nodes, edges)
	if len(cycles) != 0 {
		t.Errorf("cycles = %v, want empty", cycles)
	}
	if len(ordered) != 2 || ordered[0] != "a" || ordered[1] != "b" {
		t.Errorf("ordered = %v, want [a, b]", ordered)
	}
}

func TestSort_EdgeToExternalNodeIgnored(t *testing.T) {
	// Edge references a node not in `nodes` — must be silently dropped
	// so callers can mix in external references without pre-filtering.
	nodes := []string{"a", "b"}
	edges := [][2]string{{"a", "b"}, {"a", "external"}, {"nope", "b"}}

	ordered, cycles := Sort(nodes, edges)
	if len(cycles) != 0 {
		t.Errorf("cycles = %v, want empty", cycles)
	}
	if len(ordered) != 2 || ordered[0] != "a" || ordered[1] != "b" {
		t.Errorf("ordered = %v, want [a, b]", ordered)
	}
}

// containsAll checks that `nodes` contains every element in `want`.
func containsAll(nodes []string, want []string) bool {
	set := nodeSet(nodes)
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}

// contains reports whether `nodes` contains `target`.
func contains(nodes []string, target string) bool {
	for _, n := range nodes {
		if n == target {
			return true
		}
	}
	return false
}

func TestSort_SelfLoopReportedAsCycle(t *testing.T) {
	// A self-loop {a→a} plus a downstream edge a→b. The self-loop means
	// `a` is its own cycle (single-node SCC with a self-edge). `b` is
	// downstream of the cycle because it depends on a, which never
	// becomes ready. b must therefore be omitted from ordered AND must
	// NOT be reported as its own cycle — that's the subtle invariant in
	// toposort.go:90-92.
	nodes := []string{"a", "b"}
	edges := [][2]string{{"a", "a"}, {"a", "b"}}
	ordered, cycles := Sort(nodes, edges)
	if len(cycles) != 1 || !containsAll(cycles[0], []string{"a"}) {
		t.Errorf("expected one cycle containing [a], got %v", cycles)
	}
	// b must be excluded from ordered (it transitively depends on a,
	// which is cyclic and never emitted).
	if len(ordered) != 0 {
		t.Errorf("ordered = %v, want empty (b is downstream of a's cycle)", ordered)
	}
	// b must NOT leak into cycles as a singleton — it's downstream, not
	// a cycle member. This is exactly the "downstream-of-cycle must not
	// be reported" branch the second test also pins.
	for _, c := range cycles {
		if contains(c, "b") {
			t.Errorf("b leaked into cycle: %v", c)
		}
	}
}

func TestSort_DownstreamOfCycleNotReportedAsCycle(t *testing.T) {
	// a↔b is a cycle; c depends on b (downstream of the cycle, not a
	// cycle member itself). Exactly one cycle {a, b} is expected; c must
	// NOT appear as a singleton cycle.
	nodes := []string{"a", "b", "c"}
	edges := [][2]string{{"a", "b"}, {"b", "a"}, {"b", "c"}}
	_, cycles := Sort(nodes, edges)
	if len(cycles) != 1 {
		t.Fatalf("cycles=%v, want exactly one", cycles)
	}
	for _, c := range cycles {
		if contains(c, "c") {
			t.Errorf("c leaked into cycle: %v", c)
		}
	}
}
