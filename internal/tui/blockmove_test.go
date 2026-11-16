package tui

import (
	"slices"
	"testing"

	"github.com/alphaleonis/nibs/internal/area"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/viewshape"
)

// treeRows shapes nibs as the Tree view does with every row unfolded.
func treeRows(t *testing.T, nibs []*nib.Nib) []viewshape.Row {
	t.Helper()
	rows, err := viewshape.Shape(nibs, viewshape.View{Level: viewshape.LevelTree, Sort: viewshape.SortOrder})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// rowIndexes returns the index in rows of the row of each of ids.
func rowIndexes(t *testing.T, rows []viewshape.Row, ids ...string) []int {
	t.Helper()
	out := make([]int, len(ids))
	for k, id := range ids {
		out[k] = slices.IndexFunc(rows, func(r viewshape.Row) bool { return r.NibID == id })
		if out[k] < 0 {
			t.Fatalf("premise failed: no row shows %s", id)
		}
	}
	return out
}

func rowNibIDs(rows []viewshape.Row, indexes []int) []string {
	out := make([]string, len(indexes))
	for k, i := range indexes {
		out[k] = rows[i].NibID
	}
	return out
}

func TestEffectiveRowsDropsRowsNestedUnderAMarkedRow(t *testing.T) {
	// A holds A1 and A2, and A2 holds A21. B and C are roots.
	rows := treeRows(t, []*nib.Nib{
		{ID: "A", Order: "1"},
		{ID: "A1", Parent: "A", Order: "1"},
		{ID: "A2", Parent: "A", Order: "2"},
		{ID: "A21", Parent: "A2", Order: "1"},
		{ID: "B", Order: "2"},
		{ID: "C", Order: "3"},
	})
	for _, tc := range []struct {
		marked []string
		want   []string
	}{
		{[]string{"A", "A1", "B"}, []string{"A", "B"}},
		{[]string{"A2", "A21", "C"}, []string{"A2", "C"}},
		{[]string{"A1", "A21"}, []string{"A1", "A21"}},
		{nil, []string{}},
	} {
		marked := make(map[string]bool)
		for _, id := range tc.marked {
			marked[id] = true
		}
		if got := rowNibIDs(rows, effectiveRows(rows, marked)); !slices.Equal(got, tc.want) {
			t.Errorf("marks %v move %v, want %v", tc.marked, got, tc.want)
		}
	}
}

// The shapes a parent cycle's promoted member can take. Tree draws that member
// as a root, but its group is its stored parent's.
func TestPlanReorderNamesAParentCycle(t *testing.T) {
	for _, tc := range []struct {
		name     string
		nibs     []*nib.Nib
		subjects []string
	}{
		{"two-member cycle", []*nib.Nib{{ID: "a", Parent: "b"}, {ID: "b", Parent: "a"}}, []string{"a"}},
		{"cycle whose other member has a child", []*nib.Nib{{ID: "a", Parent: "b"}, {ID: "b", Parent: "a"}, {ID: "d", Parent: "b"}}, []string{"a"}},
		{"nib parented to itself", []*nib.Nib{{ID: "s", Parent: "s"}, {ID: "r"}}, []string{"s"}},
		{"cycle root marked with an ordinary root", []*nib.Nib{{ID: "a", Parent: "b"}, {ID: "b", Parent: "a"}, {ID: "r"}}, []string{"a", "r"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := treeRows(t, tc.nibs)
			if rows[rowIndexes(t, rows, tc.subjects[0])[0]].Depth != 0 {
				t.Fatalf("premise failed: %s is not promoted to a root", tc.subjects[0])
			}
			for _, dir := range []moveDir{moveUp, moveDown, moveTop, moveBottom} {
				if _, reason := planReorder(tc.nibs, rows, rows, rowIndexes(t, rows, tc.subjects...), dir); reason != reorderReasonInParentCycle {
					t.Errorf("direction %d: reason %q, want %q", dir, reason, reorderReasonInParentCycle)
				}
			}
		})
	}
}

// A cycle member nested under the promoted one keeps its parent link, and
// moves among that parent's children.
func TestPlanReorderMovesAnUnpromotedCycleMember(t *testing.T) {
	nibs := []*nib.Nib{{ID: "a", Parent: "b"}, {ID: "b", Parent: "a", Order: "1"}, {ID: "c", Parent: "a", Order: "2"}}
	rows := treeRows(t, nibs)
	plan, reason := planReorder(nibs, rows, rows, rowIndexes(t, rows, "b"), moveDown)
	if reason != "" || len(plan.writes) != 1 || plan.writes[0].id != "b" || plan.writes[0].afterID == nil || *plan.writes[0].afterID != "c" {
		t.Errorf("planReorder = %+v, %q, want b placed after c", plan, reason)
	}
}

// A cycle reached only as a match's ancestor context is still named.
func TestPlanReorderNamesAParentCycleShownAsContext(t *testing.T) {
	nibs := []*nib.Nib{{ID: "x", Parent: "a", Type: "bug"}, {ID: "a", Parent: "b", Type: "task"}, {ID: "b", Parent: "a", Type: "task"}}
	rows, err := viewshape.Shape(nibs, viewshape.View{Level: viewshape.LevelTree, Sort: viewshape.SortOrder, Filter: viewshape.Filter{Type: []string{"bug"}}})
	if err != nil {
		t.Fatal(err)
	}
	a := rowIndexes(t, rows, "a")
	if !rows[a[0]].Dimmed || rows[a[0]].Depth != 0 {
		t.Fatalf("premise failed: a is not a dimmed root: %+v", rows[a[0]])
	}
	if _, reason := planReorder(nibs, rows, rows, a, moveUp); reason != reorderReasonInParentCycle {
		t.Errorf("reason %q, want %q", reason, reorderReasonInParentCycle)
	}
}

func TestPlanReorderRefusesAnEmptySelection(t *testing.T) {
	if _, reason := planReorder(nil, nil, nil, nil, moveUp); reason != reorderReasonNothingSelected {
		t.Errorf("reason %q, want %q", reason, reorderReasonNothingSelected)
	}
}

// Areas draws api's webhooks section above api's own nibs at their depth. The
// planner itself stops at that row, whichever view the rows come from.
func TestPlanReorderStopsAtASectionRow(t *testing.T) {
	vocab, err := area.Parse([]byte("areas:\n  - name: api\n    children:\n      - name: webhooks\n"))
	if err != nil {
		t.Fatal(err)
	}
	nibs := []*nib.Nib{{ID: "a1", Area: "api", Order: "1"}, {ID: "a2", Area: "api", Order: "2"}}
	rows, err := viewshape.Shape(nibs, viewshape.View{Level: viewshape.LevelAreas, Sort: viewshape.SortOrder, Areas: vocab})
	if err != nil {
		t.Fatal(err)
	}
	a1 := rowIndexes(t, rows, "a1")
	if above := rows[a1[0]-1]; above.NibID != "" || above.Depth != rows[a1[0]].Depth {
		t.Fatalf("premise failed: the row above a1 is %+v", above)
	}
	for _, dir := range []moveDir{moveUp, moveTop} {
		if _, reason := planReorder(nibs, rows, rows, a1, dir); reason != reorderReasonAtTop {
			t.Errorf("direction %d: reason %q, want %q", dir, reason, reorderReasonAtTop)
		}
	}
}
