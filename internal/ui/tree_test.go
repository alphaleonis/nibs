package ui

import (
	"maps"
	"slices"
	"sort"
	"strconv"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/viewshape"
)

func TestBuildTree(t *testing.T) {
	// Create test nibs with parent relationships:
	// milestone1
	//   └── epic1
	//       └── task1
	// task2 (orphan)

	milestone1 := &nib.Nib{ID: "m1", Title: "Milestone 1", Type: "milestone"}
	epic1 := &nib.Nib{ID: "e1", Title: "Epic 1", Type: "epic", Parent: "m1"}
	task1 := &nib.Nib{ID: "t1", Title: "Task 1", Type: "task", Parent: "e1"}
	task2 := &nib.Nib{ID: "t2", Title: "Task 2", Type: "task"} // orphan

	allNibs := []*nib.Nib{milestone1, epic1, task1, task2}

	// Identity sort function (no sorting)
	noSort := func(b []*nib.Nib) {}

	t.Run("all nibs matched", func(t *testing.T) {
		tree := BuildTree(allNibs, allNibs, noSort)

		// Should have 2 root nodes: milestone1 and task2
		if len(tree) != 2 {
			t.Errorf("expected 2 root nodes, got %d", len(tree))
		}

		// Find milestone node
		var milestoneNode *TreeNode
		for _, n := range tree {
			if n.Nib.ID == "m1" {
				milestoneNode = n
				break
			}
		}
		if milestoneNode == nil {
			t.Fatal("milestone node not found")
			return
		}
		if !milestoneNode.Matched {
			t.Error("milestone should be marked as matched")
		}

		// Milestone should have epic as child
		if len(milestoneNode.Children) != 1 {
			t.Errorf("milestone should have 1 child, got %d", len(milestoneNode.Children))
		}
		epicNode := milestoneNode.Children[0]
		if epicNode.Nib.ID != "e1" {
			t.Errorf("expected epic child, got %s", epicNode.Nib.ID)
		}

		// Epic should have task as child
		if len(epicNode.Children) != 1 {
			t.Errorf("epic should have 1 child, got %d", len(epicNode.Children))
		}
		taskNode := epicNode.Children[0]
		if taskNode.Nib.ID != "t1" {
			t.Errorf("expected task child, got %s", taskNode.Nib.ID)
		}
	})

	t.Run("filter leaf only - ancestors included", func(t *testing.T) {
		// Only task1 matched, but ancestors should be included
		matchedNibs := []*nib.Nib{task1}
		tree := BuildTree(matchedNibs, allNibs, noSort)

		// Should have 1 root: milestone (as ancestor)
		if len(tree) != 1 {
			t.Errorf("expected 1 root node, got %d", len(tree))
		}

		milestoneNode := tree[0]
		if milestoneNode.Nib.ID != "m1" {
			t.Errorf("expected milestone as root, got %s", milestoneNode.Nib.ID)
		}
		if milestoneNode.Matched {
			t.Error("milestone should NOT be marked as matched (it's an ancestor)")
		}

		// Should have epic as child (also ancestor)
		if len(milestoneNode.Children) != 1 {
			t.Fatalf("milestone should have 1 child, got %d", len(milestoneNode.Children))
		}
		epicNode := milestoneNode.Children[0]
		if epicNode.Matched {
			t.Error("epic should NOT be marked as matched (it's an ancestor)")
		}

		// Task should be matched
		if len(epicNode.Children) != 1 {
			t.Fatalf("epic should have 1 child, got %d", len(epicNode.Children))
		}
		taskNode := epicNode.Children[0]
		if !taskNode.Matched {
			t.Error("task should be marked as matched")
		}
	})

	t.Run("filter middle - ancestors included", func(t *testing.T) {
		// Only epic1 matched
		matchedNibs := []*nib.Nib{epic1}
		tree := BuildTree(matchedNibs, allNibs, noSort)

		// Should have 1 root: milestone (ancestor)
		if len(tree) != 1 {
			t.Errorf("expected 1 root node, got %d", len(tree))
		}

		milestoneNode := tree[0]
		if milestoneNode.Matched {
			t.Error("milestone should NOT be marked as matched")
		}

		epicNode := milestoneNode.Children[0]
		if !epicNode.Matched {
			t.Error("epic should be marked as matched")
		}

		// Epic should have no children (task1 was not matched)
		if len(epicNode.Children) != 0 {
			t.Errorf("epic should have 0 children (task not matched), got %d", len(epicNode.Children))
		}
	})

	t.Run("orphan nib", func(t *testing.T) {
		matchedNibs := []*nib.Nib{task2}
		tree := BuildTree(matchedNibs, allNibs, noSort)

		if len(tree) != 1 {
			t.Errorf("expected 1 root node, got %d", len(tree))
		}
		if tree[0].Nib.ID != "t2" {
			t.Errorf("expected task2 as root, got %s", tree[0].Nib.ID)
		}
		if !tree[0].Matched {
			t.Error("task2 should be marked as matched")
		}
	})

	t.Run("broken parent link", func(t *testing.T) {
		// Nib with parent that doesn't exist
		brokenNib := &nib.Nib{ID: "broken", Title: "Broken", Parent: "nonexistent"}
		matchedNibs := []*nib.Nib{brokenNib}
		allNibsWithBroken := append(allNibs, brokenNib)

		tree := BuildTree(matchedNibs, allNibsWithBroken, noSort)

		// Should be treated as root (parent not found)
		if len(tree) != 1 {
			t.Errorf("expected 1 root node, got %d", len(tree))
		}
		if tree[0].Nib.ID != "broken" {
			t.Errorf("expected broken nib as root, got %s", tree[0].Nib.ID)
		}
	})
}

// treeShape flattens a forest depth-first into "depth:id" entries, so a test can
// assert both which nibs rendered and where they sat.
func treeShape(nodes []*TreeNode) []string {
	var out []string
	var walk func(ns []*TreeNode, depth int)
	walk = func(ns []*TreeNode, depth int) {
		for _, n := range ns {
			out = append(out, strconv.Itoa(depth)+":"+n.Nib.ID)
			walk(n.Children, depth+1)
		}
	}
	walk(nodes, 0)
	return out
}

// sortByID orders nibs by id, making the forest fully deterministic even though
// BuildTree ranges a map internally.
func sortByID(bs []*nib.Nib) {
	sort.Slice(bs, func(i, j int) bool { return bs[i].ID < bs[j].ID })
}

// The fixture table below is mirrored verbatim in web/src/lib/tree.test.ts
// ("parent cycles degrade to a visible oddity"): same names, same inputs, same
// "depth:id" wants. Adding or changing a case here means changing it there too —
// that duplication is the only thing that catches a Go/TS divergence.
//
// TestBuildTreeParentCycle pins that a parent cycle degrades to a visible
// oddity. Every member of a cycle has a parent inside the needed set, so the
// ordinary "root = no parent, or parent outside the set" rule finds no root and
// the entire cycle used to vanish from the tree. One member is promoted to a
// root instead, and the rest of the cycle hangs off it as an ordinary chain.
func TestBuildTreeParentCycle(t *testing.T) {
	tests := []struct {
		name string
		nibs []*nib.Nib
		want []string
	}{
		{
			name: "self cycle renders once",
			nibs: []*nib.Nib{{ID: "a", Parent: "a"}},
			want: []string{"0:a"},
		},
		{
			name: "two-cycle renders both members",
			nibs: []*nib.Nib{{ID: "a", Parent: "b"}, {ID: "b", Parent: "a"}},
			want: []string{"0:a", "1:b"},
		},
		{
			// The lowest id is promoted, not the first one seen: "m" is second in
			// the input and still becomes the root.
			name: "promotion picks the lowest id, not the input order",
			nibs: []*nib.Nib{{ID: "z", Parent: "m"}, {ID: "m", Parent: "z"}},
			want: []string{"0:m", "1:z"},
		},
		{
			name: "three-cycle renders all members as a chain",
			nibs: []*nib.Nib{{ID: "a", Parent: "c"}, {ID: "b", Parent: "a"}, {ID: "c", Parent: "b"}},
			want: []string{"0:a", "1:b", "2:c"},
		},
		{
			name: "two disjoint cycles each get their own root",
			nibs: []*nib.Nib{
				{ID: "a", Parent: "b"}, {ID: "b", Parent: "a"},
				{ID: "x", Parent: "y"}, {ID: "y", Parent: "x"},
			},
			want: []string{"0:a", "1:b", "0:x", "1:y"},
		},
		{
			// A nib whose parent chain reaches a cycle but that is not itself in
			// it keeps rendering under its real parent.
			name: "child hanging off a cycle stays under its parent",
			nibs: []*nib.Nib{
				{ID: "a", Parent: "b"}, {ID: "b", Parent: "a"},
				{ID: "d", Parent: "b"}, {ID: "g", Parent: "d"},
			},
			want: []string{"0:a", "1:b", "2:d", "3:g"},
		},
		{
			name: "cycle alongside an ordinary tree",
			nibs: []*nib.Nib{
				{ID: "a", Parent: "b"}, {ID: "b", Parent: "a"},
				{ID: "m1"}, {ID: "m2", Parent: "m1"},
			},
			want: []string{"0:a", "1:b", "0:m1", "1:m2"},
		},
		{
			// Must-not-regress: an ordinary tree plus a root-level orphan is
			// unaffected by cycle handling.
			name: "ordinary tree with no cycle is unchanged",
			nibs: []*nib.Nib{
				{ID: "m1"}, {ID: "m2", Parent: "m1"}, {ID: "m3", Parent: "m2"}, {ID: "z1"},
			},
			want: []string{"0:m1", "1:m2", "2:m3", "0:z1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := treeShape(BuildTree(tt.nibs, tt.nibs, sortByID))
			if !slices.Equal(got, tt.want) {
				t.Errorf("BuildTree rendered %v, want %v", got, tt.want)
			}
		})
	}
}

// TestBuildTreeCycleReachedByAncestorWalk covers the filtered path: only one
// cycle member matched, so the rest arrive through the ancestor walk. The
// promoted root can then be a nib that is shown purely for context.
func TestBuildTreeCycleReachedByAncestorWalk(t *testing.T) {
	a := &nib.Nib{ID: "a", Parent: "b"}
	b := &nib.Nib{ID: "b", Parent: "a"}
	all := []*nib.Nib{a, b}

	tree := BuildTree([]*nib.Nib{b}, all, sortByID)

	if got, want := treeShape(tree), []string{"0:a", "1:b"}; !slices.Equal(got, want) {
		t.Fatalf("BuildTree rendered %v, want %v", got, want)
	}
	if tree[0].Matched {
		t.Error("a is an ancestor shown for context; it should not be marked matched")
	}
	if !tree[0].Children[0].Matched {
		t.Error("b matched the filter; it should be marked matched")
	}
}

// subtreeIDs collects the ids of a node and everything below it.
func subtreeIDs(n *TreeNode) map[string]bool {
	ids := map[string]bool{n.Nib.ID: true}
	for _, c := range n.Children {
		for id := range subtreeIDs(c) {
			ids[id] = true
		}
	}
	return ids
}

// TestBuildTreeCyclePromotionContract pins the three cycle-promotion properties
// that callers outside this package read the tree through; they are stated on
// BuildTree's doc comment. The known consumer is internal/tui's reorder scoping
// (inParentCycle in blockmove.go), which uses property 2 as its tell that a nib
// belongs to no parent's sibling list.
//
// The properties are pinned here rather than left to the consumer's tests
// because breaking one fails silently on this side: promote differently and the
// TUI reverts to describing a sibling list instead of naming the cycle, with
// every test in this package still green and the failure surfacing a package
// away, in message assertions.
func TestBuildTreeCyclePromotionContract(t *testing.T) {
	tests := []struct {
		name   string
		nibs   []*nib.Nib
		cycles [][]string // ids forming each parent cycle in nibs
	}{
		{
			name:   "self cycle",
			nibs:   []*nib.Nib{{ID: "a", Parent: "a"}},
			cycles: [][]string{{"a"}},
		},
		{
			name:   "two-cycle",
			nibs:   []*nib.Nib{{ID: "a", Parent: "b"}, {ID: "b", Parent: "a"}},
			cycles: [][]string{{"a", "b"}},
		},
		{
			name:   "three-cycle",
			nibs:   []*nib.Nib{{ID: "a", Parent: "c"}, {ID: "b", Parent: "a"}, {ID: "c", Parent: "b"}},
			cycles: [][]string{{"a", "b", "c"}},
		},
		{
			// A non-member child hanging off the cycle must not be mistaken for
			// a second promotion, nor suppress the real one.
			name: "cycle with a non-member child hanging off it",
			nibs: []*nib.Nib{
				{ID: "a", Parent: "b"}, {ID: "b", Parent: "a"},
				{ID: "d", Parent: "b"},
			},
			cycles: [][]string{{"a", "b"}},
		},
		{
			name: "two disjoint cycles alongside an ordinary tree",
			nibs: []*nib.Nib{
				{ID: "a", Parent: "b"}, {ID: "b", Parent: "a"},
				{ID: "x", Parent: "y"}, {ID: "y", Parent: "x"},
				{ID: "m1"}, {ID: "m2", Parent: "m1"},
			},
			cycles: [][]string{{"a", "b"}, {"x", "y"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibByID := make(map[string]*nib.Nib, len(tt.nibs))
			for _, b := range tt.nibs {
				nibByID[b.ID] = b
			}

			tree := BuildTree(tt.nibs, tt.nibs, sortByID)
			roots := make(map[string]*TreeNode, len(tree))
			for _, r := range tree {
				roots[r.Nib.ID] = r
			}

			for _, cycle := range tt.cycles {
				// (1) Exactly one member of the cycle is promoted to a root.
				var promoted []*TreeNode
				for _, id := range cycle {
					if r, ok := roots[id]; ok {
						promoted = append(promoted, r)
					}
				}
				if len(promoted) != 1 {
					t.Fatalf("cycle %v: %d members promoted to roots, want exactly 1", cycle, len(promoted))
				}
				p := promoted[0]

				// (2) Promotion severs the tree edge only — the stored parent
				// link survives, and the rest of the cycle nests underneath the
				// promoted member, so the stored parent lies in its own subtree.
				// That containment is the exact tell internal/tui reads.
				if p.Nib.Parent == "" {
					t.Fatalf("promoted root %s: Parent was cleared; promotion must leave the stored link intact", p.Nib.ID)
				}
				if FindNode(tree, p.Nib.Parent) == nil {
					t.Fatalf("promoted root %s: stored parent %s is absent from the tree", p.Nib.ID, p.Nib.Parent)
				}
				if sub := subtreeIDs(p); !sub[p.Nib.Parent] {
					t.Errorf("promoted root %s: stored parent %s lies outside its own subtree %v; the rest of the cycle must nest below the promoted member",
						p.Nib.ID, p.Nib.Parent, slices.Sorted(maps.Keys(sub)))
				}

				// (3) addAncestors closes the built set upward, so a cycle is
				// never partially present: filtering down to any single member
				// still drags the whole cycle in.
				for _, id := range cycle {
					filtered := BuildTree([]*nib.Nib{nibByID[id]}, tt.nibs, sortByID)
					for _, want := range cycle {
						if FindNode(filtered, want) == nil {
							t.Errorf("filtered to %s: cycle member %s is missing from the tree", id, want)
						}
					}
				}
			}
		})
	}
}

func TestTreePrefixes(t *testing.T) {
	// e1
	// ├─ f1 (folded)
	// └─ f2
	//    ├─ t1
	//    └─ t2
	// t3
	// e2
	// └─ t4
	rows := []viewshape.Row{
		{NibID: "e1", HasChildren: true},
		{NibID: "f1", Depth: 1, HasChildren: true},
		{NibID: "f2", Depth: 1, HasChildren: true},
		{NibID: "t1", Depth: 2},
		{NibID: "t2", Depth: 2},
		{NibID: "t3"},
		{NibID: "e2", HasChildren: true},
		{NibID: "t4", Depth: 1},
	}
	collapsed := map[string]bool{"f1": true, "t3": true}
	open, folded, leaf := CollapseIndicatorExpanded(false), CollapseIndicatorCollapsed(false), CollapseIndicatorLeaf
	want := []string{
		open,
		"├─ " + folded,
		"└─ " + open,
		"   ├─ " + leaf,
		"   └─ " + leaf,
		leaf,
		open,
		"└─ " + leaf,
	}
	isFolded := func(r viewshape.Row) bool { return collapsed[r.NibID] }
	if got := TreePrefixes(rows, isFolded, false); !slices.Equal(got, want) {
		t.Errorf("prefixes differ\n got: %q\nwant: %q", got, want)
	}
}

func TestTreePrefixesDrawsAPipeForAnAncestorWithASiblingBelow(t *testing.T) {
	rows := []viewshape.Row{
		{NibID: "e1", HasChildren: true},
		{NibID: "f1", Depth: 1, HasChildren: true},
		{NibID: "t1", Depth: 2},
		{NibID: "f2", Depth: 1},
	}
	got := TreePrefixes(rows, func(viewshape.Row) bool { return false }, false)
	if want := "│  └─ " + CollapseIndicatorLeaf; got[2] != want {
		t.Errorf("t1's prefix = %q, want %q", got[2], want)
	}
}
