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

// Behavior 4: single effective item under a parent → ok=true, single-index
// range, siblings are the parent's children.
func TestBlockMovable_SingleItemUnderParent(t *testing.T) {
	tree := buildTestTree()
	effective := []*nib.Nib{tree[0].Children[1].Nib} // A2

	siblings, start, end, ok := blockMovable(effective, tree)
	if !ok {
		t.Fatal("expected ok=true for single item under parent")
	}
	if start != 1 || end != 1 {
		t.Errorf("expected [1,1], got [%d,%d]", start, end)
	}
	wantSibIDs := []string{"A1", "A2"}
	if got := idsOf(siblings); !equalStrings(got, wantSibIDs) {
		t.Errorf("siblings = %v, want %v", got, wantSibIDs)
	}
}

// Behavior 5: two contiguous siblings → ok=true, range spans both.
func TestBlockMovable_TwoContiguousSiblings(t *testing.T) {
	tree := buildTestTree()
	effective := []*nib.Nib{tree[0].Children[0].Nib, tree[0].Children[1].Nib} // A1, A2

	siblings, start, end, ok := blockMovable(effective, tree)
	if !ok {
		t.Fatal("expected ok=true for two contiguous siblings")
	}
	if start != 0 || end != 1 {
		t.Errorf("expected [0,1], got [%d,%d]", start, end)
	}
	wantSibIDs := []string{"A1", "A2"}
	if got := idsOf(siblings); !equalStrings(got, wantSibIDs) {
		t.Errorf("siblings = %v, want %v", got, wantSibIDs)
	}
}

// Behavior 6: two siblings with a gap → ok=false.
func TestBlockMovable_GapBetweenSiblings(t *testing.T) {
	// Extend tree: root level has A, B, C; select A and C → gap at B.
	tree := buildTestTree()
	effective := []*nib.Nib{tree[0].Nib, tree[2].Nib} // A, C (B in between)

	_, _, _, ok := blockMovable(effective, tree)
	if ok {
		t.Error("expected ok=false for sibling gap")
	}
}

// Behavior 7: items from two different parents → ok=false.
func TestBlockMovable_MultipleParents(t *testing.T) {
	tree := buildTestTree()
	effective := []*nib.Nib{tree[0].Children[0].Nib, tree[2].Children[0].Nib} // A1, C1

	_, _, _, ok := blockMovable(effective, tree)
	if ok {
		t.Error("expected ok=false for items under different parents")
	}
}

// Behavior 8: one at root + one under a parent → ok=false.
func TestBlockMovable_RootMixedWithChild(t *testing.T) {
	tree := buildTestTree()
	effective := []*nib.Nib{tree[0].Nib, tree[0].Children[0].Nib} // A (root), A1 (child of A)
	// Note: effectiveSelection would drop A1 here since A is its ancestor; this
	// test asserts blockMovable's own invariant when given mixed-scope input.

	_, _, _, ok := blockMovable(effective, tree)
	if ok {
		t.Error("expected ok=false for root + child mix")
	}
}

// Behavior 9: empty effective set → ok=false.
func TestBlockMovable_EmptyEffective(t *testing.T) {
	tree := buildTestTree()

	_, _, _, ok := blockMovable(nil, tree)
	if ok {
		t.Error("expected ok=false for nil effective set")
	}
	_, _, _, ok = blockMovable([]*nib.Nib{}, tree)
	if ok {
		t.Error("expected ok=false for empty effective set")
	}
}
