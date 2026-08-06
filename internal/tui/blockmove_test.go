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
// the keypress.
func TestSingleReorderCmd_RefusalsReportReason(t *testing.T) {
	siblings := []*nib.Nib{{ID: "A"}, {ID: "B"}}

	tests := []struct {
		name    string
		target  *nib.Nib
		up      bool
		wantMsg string
	}{
		{"no nib to move", nil, true, reorderReasonNothingSelected},
		{"nib absent from siblings", &nib.Nib{ID: "Z"}, true, reorderReasonNotInList},
		{"first sibling moving up", siblings[0], true, reorderReasonAtTop},
		{"last sibling moving down", siblings[1], false, reorderReasonAtBottom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := singleReorderCmd(tt.target, siblings, tt.up)
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
