package tui

import (
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/ui"
)

// buildTestTree constructs a small tree for block-move tests.
//
// Shape:
//
//	A
//	├── A1
//	└── A2
//	B
//	C
//	├── C1
//	└── C2
func buildTestTree() []*ui.TreeNode {
	return []*ui.TreeNode{
		{
			Nib: &nib.Nib{ID: "A"},
			Children: []*ui.TreeNode{
				{Nib: &nib.Nib{ID: "A1", Parent: "A"}},
				{Nib: &nib.Nib{ID: "A2", Parent: "A"}},
			},
		},
		{Nib: &nib.Nib{ID: "B"}},
		{
			Nib: &nib.Nib{ID: "C"},
			Children: []*ui.TreeNode{
				{Nib: &nib.Nib{ID: "C1", Parent: "C"}},
				{Nib: &nib.Nib{ID: "C2", Parent: "C"}},
			},
		},
	}
}

// buildCycleTree builds a tree from nibs the way loadNibs does — the real
// ui.BuildTree over the full set, sorted by order — so the promotion that
// breaks a parent cycle is the production one rather than a hand-shaped
// stand-in.
func buildCycleTree(nibs ...*nib.Nib) []*ui.TreeNode {
	return ui.BuildTree(nibs, nibs, nib.SortByOrder)
}

// isTopLevel reports whether id is one of the tree's roots. The cycle tests
// rest on BuildTree having promoted their target to root level; asserting it
// keeps a change in that promotion from quietly turning them into tests of
// something else.
func isTopLevel(tree []*ui.TreeNode, id string) bool {
	for _, node := range tree {
		if node.Nib.ID == id {
			return true
		}
	}
	return false
}

func idsOf(nibs []*nib.Nib) []string {
	out := make([]string, len(nibs))
	for i, n := range nibs {
		out[i] = n.ID
	}
	return out
}

// Tracer: parent+descendant both marked → descendant dropped.
func TestEffectiveSelection_DropsDescendantOfMarkedAncestor(t *testing.T) {
	tree := buildTestTree()
	marked := map[string]bool{
		"A":  true,
		"A1": true,
		"B":  true,
	}

	got := effectiveSelection(marked, tree)

	want := []string{"A", "B"}
	if gotIDs := idsOf(got); !equalStrings(gotIDs, want) {
		t.Errorf("effectiveSelection = %v, want %v", gotIDs, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Behavior 2: all marked at root level → all included.
func TestEffectiveSelection_AllRootMarked(t *testing.T) {
	tree := buildTestTree()
	marked := map[string]bool{
		"A": true,
		"B": true,
		"C": true,
	}

	got := effectiveSelection(marked, tree)

	want := []string{"A", "B", "C"}
	if gotIDs := idsOf(got); !equalStrings(gotIDs, want) {
		t.Errorf("effectiveSelection = %v, want %v", gotIDs, want)
	}
}

// Behavior 3: empty input → empty output.
func TestEffectiveSelection_Empty(t *testing.T) {
	tree := buildTestTree()

	got := effectiveSelection(map[string]bool{}, tree)
	if len(got) != 0 {
		t.Errorf("expected empty result, got %v", idsOf(got))
	}

	got = effectiveSelection(nil, tree)
	if len(got) != 0 {
		t.Errorf("expected empty result from nil map, got %v", idsOf(got))
	}
}

// Behavior 4: single effective item under a parent → movable, single-index
// range, siblings are the parent's children.
func TestBlockMovable_SingleItemUnderParent(t *testing.T) {
	tree := buildTestTree()
	effective := []*nib.Nib{tree[0].Children[1].Nib} // A2

	siblings, start, end, reason := blockMovable(effective, tree)
	if reason != "" {
		t.Fatalf("expected a movable block for single item under parent, refused: %s", reason)
	}
	if start != 1 || end != 1 {
		t.Errorf("expected [1,1], got [%d,%d]", start, end)
	}
	wantSibIDs := []string{"A1", "A2"}
	if got := idsOf(siblings); !equalStrings(got, wantSibIDs) {
		t.Errorf("siblings = %v, want %v", got, wantSibIDs)
	}
}

// Behavior 5: two contiguous siblings → movable, range spans both.
func TestBlockMovable_TwoContiguousSiblings(t *testing.T) {
	tree := buildTestTree()
	effective := []*nib.Nib{tree[0].Children[0].Nib, tree[0].Children[1].Nib} // A1, A2

	siblings, start, end, reason := blockMovable(effective, tree)
	if reason != "" {
		t.Fatalf("expected a movable block for two contiguous siblings, refused: %s", reason)
	}
	if start != 0 || end != 1 {
		t.Errorf("expected [0,1], got [%d,%d]", start, end)
	}
	wantSibIDs := []string{"A1", "A2"}
	if got := idsOf(siblings); !equalStrings(got, wantSibIDs) {
		t.Errorf("siblings = %v, want %v", got, wantSibIDs)
	}
}

// Behavior 6: two siblings with a gap → refused as non-contiguous.
func TestBlockMovable_GapBetweenSiblings(t *testing.T) {
	// Extend tree: root level has A, B, C; select A and C → gap at B.
	tree := buildTestTree()
	effective := []*nib.Nib{tree[0].Nib, tree[2].Nib} // A, C (B in between)

	_, _, _, reason := blockMovable(effective, tree)
	if reason != reorderReasonNotContiguous {
		t.Errorf("expected %q for sibling gap, got %q", reorderReasonNotContiguous, reason)
	}
}

// Behavior 7: items from two different parents → refused as different parents.
func TestBlockMovable_MultipleParents(t *testing.T) {
	tree := buildTestTree()
	effective := []*nib.Nib{tree[0].Children[0].Nib, tree[2].Children[0].Nib} // A1, C1

	_, _, _, reason := blockMovable(effective, tree)
	if reason != reorderReasonDifferentParents {
		t.Errorf("expected %q for items under different parents, got %q", reorderReasonDifferentParents, reason)
	}
}

// Behavior 8: one at root + one under a parent → refused as different parents.
func TestBlockMovable_RootMixedWithChild(t *testing.T) {
	tree := buildTestTree()
	effective := []*nib.Nib{tree[0].Nib, tree[0].Children[0].Nib} // A (root), A1 (child of A)
	// Note: effectiveSelection would drop A1 here since A is its ancestor; this
	// test asserts blockMovable's own invariant when given mixed-scope input.

	_, _, _, reason := blockMovable(effective, tree)
	if reason != reorderReasonDifferentParents {
		t.Errorf("expected %q for root + child mix, got %q", reorderReasonDifferentParents, reason)
	}
}

// Behavior 8b: items agreeing on a parent that does not hold both of them.
// BuildTree cannot produce this, so the input is deliberately inconsistent —
// it pins the defensive branch to a message rather than a silent refusal.
func TestBlockMovable_ItemNotAmongSiblings(t *testing.T) {
	stray := &nib.Nib{ID: "A3", Parent: "A"}
	tree := buildTestTree()
	tree = append(tree, &ui.TreeNode{Nib: stray}) // claims parent A, but sits at root

	effective := []*nib.Nib{tree[0].Children[0].Nib, stray} // A1, A3

	_, _, _, reason := blockMovable(effective, tree)
	if reason != reorderReasonNotAmongSiblings {
		t.Errorf("expected %q, got %q", reorderReasonNotAmongSiblings, reason)
	}
}

// Behavior 8d: an empty tree yields no siblings to move among.
func TestBlockMovable_NoSiblings(t *testing.T) {
	effective := []*nib.Nib{{ID: "A"}, {ID: "B"}}

	_, _, _, reason := blockMovable(effective, nil)
	if reason != reorderReasonNoSiblings {
		t.Errorf("expected %q, got %q", reorderReasonNoSiblings, reason)
	}
}

// Behavior 8c: every single-item refusal names its reason instead of dropping
// the keypress. The tree is passed as the caller passes it, so a nib whose
// parent link is ordinary keeps the ordinary reason.
func TestSingleReorderCmd_RefusalsReportReason(t *testing.T) {
	siblings := []*nib.Nib{{ID: "A"}, {ID: "B"}}
	tree := buildTestTree()

	tests := []struct {
		name    string
		target  *nib.Nib
		up      bool
		wantMsg string
	}{
		{"no nib to move", nil, true, reorderReasonNothingSelected},
		{"nib absent from siblings", &nib.Nib{ID: "Z"}, true, reorderReasonNotInList},
		// In the tree with a parent that resolves — but resolves upward, not into
		// its own subtree — so this is absence from a list, not a cycle.
		{"nib in the tree but not in the list it was handed", tree[0].Children[0].Nib, true, reorderReasonNotInList},
		{"first sibling moving up", siblings[0], true, reorderReasonAtTop},
		{"last sibling moving down", siblings[1], false, reorderReasonAtBottom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := singleReorderCmd(tt.target, siblings, tree, tt.up)
			if cmd == nil {
				t.Fatal("expected a command reporting the refusal, got nil")
			}
			msg, ok := cmd().(reorderRefusedMsg)
			if !ok {
				t.Fatalf("expected reorderRefusedMsg, got %T", cmd())
			}
			if msg.reason != tt.wantMsg {
				t.Errorf("expected reason %q, got %q", tt.wantMsg, msg.reason)
			}
		})
	}
}

// cycleCase describes a nib set containing a parent cycle, the member
// ui.BuildTree is expected to promote to a root, and the selection under test.
type cycleCase struct {
	name         string
	nibs         []*nib.Nib
	promoted     string
	effectiveIDs []string
}

// promotedCycleCases are the shapes a promoted cycle root can take. They are
// shared by the block-move and single-move refusal tests so both paths answer
// for the same trees.
var promotedCycleCases = []cycleCase{
	{
		// Previously refused as reorderReasonNoSiblings: the promoted root's
		// stored parent is its own only child, which has no children at all.
		name:         "two-member cycle",
		nibs:         []*nib.Nib{{ID: "a", Parent: "b"}, {ID: "b", Parent: "a"}},
		promoted:     "a",
		effectiveIDs: []string{"a"},
	},
	{
		// Previously refused as reorderReasonNotAmongSiblings: the stored parent
		// does have children, but the promoted root is not among them.
		name:         "cycle whose other member has a child",
		nibs:         []*nib.Nib{{ID: "a", Parent: "b"}, {ID: "b", Parent: "a"}, {ID: "d", Parent: "b"}},
		promoted:     "a",
		effectiveIDs: []string{"a"},
	},
	{
		// The degenerate one-member cycle: a nib parented to itself.
		name:         "nib parented to itself",
		nibs:         []*nib.Nib{{ID: "s", Parent: "s"}, {ID: "r"}},
		promoted:     "s",
		effectiveIDs: []string{"s"},
	},
	{
		// A promoted root renders alongside ordinary roots, so it is selectable
		// with them. The cycle is the reason the block cannot move, not the
		// disagreement about parents that the selection also shows.
		name:         "cycle root selected with an ordinary root",
		nibs:         []*nib.Nib{{ID: "a", Parent: "b"}, {ID: "b", Parent: "a"}, {ID: "r"}},
		promoted:     "a",
		effectiveIDs: []string{"a", "r"},
	},
}

// Behavior: a block move whose selection includes a nib promoted out of a
// parent cycle is refused with the cycle named, rather than with a description
// of the sibling list the severed parent edge left behind.
func TestBlockMovable_PromotedCycleRootNamesTheCycle(t *testing.T) {
	for _, tt := range promotedCycleCases {
		t.Run(tt.name, func(t *testing.T) {
			tree := requireCycleTree(t, tt)

			_, _, _, reason := blockMovable(nibsByID(tt.nibs, tt.effectiveIDs...), tree)
			if reason != reorderReasonInParentCycle {
				t.Errorf("expected %q, got %q", reorderReasonInParentCycle, reason)
			}
		})
	}
}

// Behavior: the single-item path refuses the same selection with the same
// reason. It reaches the refusal through a different route — absent from the
// sibling list rather than absent from an index scan — so it is asserted
// separately.
func TestSingleReorderCmd_PromotedCycleRootNamesTheCycle(t *testing.T) {
	// The table is shared, so an edit made for the block test could leave nothing
	// here to run. Counting the survivors keeps that from passing silently.
	ran := 0
	for _, tt := range promotedCycleCases {
		// The single-item path moves one nib; the mixed selection belongs to the
		// block test only.
		if len(tt.effectiveIDs) != 1 {
			continue
		}
		ran++
		t.Run(tt.name, func(t *testing.T) {
			tree := requireCycleTree(t, tt)
			target := nibsByID(tt.nibs, tt.effectiveIDs...)[0]
			// Mirror listModel.findSiblings, the caller's sibling source.
			siblings := siblingsFromTree(tree, treeResolvedParentID(target, tree))

			for _, up := range []bool{true, false} {
				cmd := singleReorderCmd(target, siblings, tree, up)
				if cmd == nil {
					t.Fatalf("up=%v: expected a command reporting the refusal, got nil", up)
				}
				msg, ok := cmd().(reorderRefusedMsg)
				if !ok {
					t.Fatalf("up=%v: expected reorderRefusedMsg, got %T", up, cmd())
				}
				if msg.reason != reorderReasonInParentCycle {
					t.Errorf("up=%v: expected reason %q, got %q", up, reorderReasonInParentCycle, msg.reason)
				}
			}
		})
	}
	if ran == 0 {
		t.Fatal("no single-item cases in promotedCycleCases; this test asserted nothing")
	}
}

// Behavior: the refusal holds under the filtered tree the list actually builds.
// loadNibs hands ui.BuildTree the matched nibs plus the full set, and
// addAncestors closes that set upward — so a cycle reached only as ancestor
// context of a matched nib is still whole, still promoted, and still named as
// the cause. The cases above all build trees where matched == all, which is the
// one shape that cannot exercise this.
func TestPromotedCycleRoot_EntersAsAncestorContext(t *testing.T) {
	x := &nib.Nib{ID: "x", Parent: "a"}
	a := &nib.Nib{ID: "a", Parent: "b"}
	b := &nib.Nib{ID: "b", Parent: "a"}
	all := []*nib.Nib{x, a, b}

	// Only x matches; a and b are pulled in as x's ancestors.
	tree := ui.BuildTree([]*nib.Nib{x}, all, nib.SortByOrder)
	if !isTopLevel(tree, "a") {
		t.Fatalf("BuildTree did not promote %q to root level; roots are %v", "a", rootIDs(tree))
	}
	// Pins the premise: a is here as context, not as a filter match. Without
	// this, a change making BuildTree ignore the filter would leave the test
	// asserting the same thing the cases above already do.
	if node := ui.FindNode(tree, "a"); node == nil || node.Matched {
		t.Fatalf("expected %q to be present as unmatched ancestor context, got %+v", "a", node)
	}

	if _, _, _, reason := blockMovable([]*nib.Nib{a}, tree); reason != reorderReasonInParentCycle {
		t.Errorf("blockMovable: expected %q, got %q", reorderReasonInParentCycle, reason)
	}

	// Mirror listModel.findSiblings, the caller's sibling source.
	siblings := siblingsFromTree(tree, treeResolvedParentID(a, tree))
	for _, up := range []bool{true, false} {
		cmd := singleReorderCmd(a, siblings, tree, up)
		if cmd == nil {
			t.Fatalf("up=%v: expected a command reporting the refusal, got nil", up)
		}
		msg, ok := cmd().(reorderRefusedMsg)
		if !ok {
			t.Fatalf("up=%v: expected reorderRefusedMsg, got %T", up, cmd())
		}
		if msg.reason != reorderReasonInParentCycle {
			t.Errorf("up=%v: expected reason %q, got %q", up, reorderReasonInParentCycle, msg.reason)
		}
	}
}

// Behavior: only the promoted member is scoped out. A cycle member that kept
// its parent edge still sits among that parent's children, and reordering it
// there still produces a move rather than a refusal.
func TestSingleReorderCmd_UnpromotedCycleMemberStillMoves(t *testing.T) {
	// a <-> b is the cycle; c is an ordinary child of a. BuildTree promotes a,
	// leaving b and c as a's children — a real sibling list b can move within.
	nibs := []*nib.Nib{{ID: "a", Parent: "b"}, {ID: "b", Parent: "a"}, {ID: "c", Parent: "a"}}
	tree := buildCycleTree(nibs...)
	if !isTopLevel(tree, "a") {
		t.Fatalf("BuildTree did not promote %q to root level; roots are %v", "a", rootIDs(tree))
	}
	target := nibsByID(nibs, "b")[0]
	siblings := siblingsFromTree(tree, treeResolvedParentID(target, tree))
	if want := []string{"b", "c"}; !equalStrings(idsOf(siblings), want) {
		t.Fatalf("siblings = %v, want %v", idsOf(siblings), want)
	}

	cmd := singleReorderCmd(target, siblings, tree, false)
	if cmd == nil {
		t.Fatal("expected a reorder command, got nil")
	}
	msg, ok := cmd().(reorderNibMsg)
	if !ok {
		t.Fatalf("expected reorderNibMsg, got %T (%v)", cmd(), cmd())
	}
	if msg.nibID != "b" || msg.afterID == nil || *msg.afterID != "c" {
		t.Errorf("expected b to move after c, got %+v", msg)
	}
}

// requireCycleTree builds the case's tree and asserts BuildTree promoted the
// expected member, so a change in the promotion rule fails loudly here instead
// of silently retargeting the test.
func requireCycleTree(t *testing.T, tt cycleCase) []*ui.TreeNode {
	t.Helper()
	tree := buildCycleTree(tt.nibs...)
	if !isTopLevel(tree, tt.promoted) {
		t.Fatalf("BuildTree did not promote %q to root level; roots are %v", tt.promoted, rootIDs(tree))
	}
	return tree
}

// nibsByID returns the nibs with the given ids, in the order requested.
func nibsByID(nibs []*nib.Nib, ids ...string) []*nib.Nib {
	out := make([]*nib.Nib, 0, len(ids))
	for _, id := range ids {
		for _, n := range nibs {
			if n.ID == id {
				out = append(out, n)
				break
			}
		}
	}
	return out
}

// rootIDs returns the ids of the tree's top-level nodes, for failure messages.
func rootIDs(tree []*ui.TreeNode) []string {
	out := make([]string, len(tree))
	for i, node := range tree {
		out[i] = node.Nib.ID
	}
	return out
}

// Behavior 9: empty effective set → refused as nothing to move.
func TestBlockMovable_EmptyEffective(t *testing.T) {
	tree := buildTestTree()

	_, _, _, reason := blockMovable(nil, tree)
	if reason != reorderReasonNothingSelected {
		t.Errorf("expected %q for nil effective set, got %q", reorderReasonNothingSelected, reason)
	}
	_, _, _, reason = blockMovable([]*nib.Nib{}, tree)
	if reason != reorderReasonNothingSelected {
		t.Errorf("expected %q for empty effective set, got %q", reorderReasonNothingSelected, reason)
	}
}
