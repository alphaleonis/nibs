package nibcore

import (
	"reflect"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
)

// TestCheckAllLinksInMapFlagsAssignmentConflict pins the assignment-exclusivity
// finding: a nib assigned to a milestone while an ancestor is assigned too is
// reported naming both nibs and both milestones. The nearest assigned
// ancestor is the one named — a grandchild under an assigned grandparent
// reports the grandparent when the parent between them is unassigned — and an
// assigned ancestor of an assigned ancestor yields one finding per assigned
// descendant, not one per pair. The findings arrive sorted by nib id and count
// as issues.
func TestCheckAllLinksInMapFlagsAssignmentConflict(t *testing.T) {
	nibs := map[string]*nib.Nib{
		"chk-ms1": {ID: "chk-ms1", Status: "todo", Type: "milestone", Path: "data/chk-ms1--one.md"},
		"chk-ms2": {ID: "chk-ms2", Status: "todo", Type: "milestone", Path: "data/chk-ms2--two.md"},
		// Assigned epic with an unassigned feature and an assigned grandchild:
		// the grandchild conflicts with the epic, across the unassigned rung.
		"chk-ep1": {ID: "chk-ep1", Status: "todo", Type: "epic", Path: "data/chk-ep1--epic.md", Milestone: "chk-ms1"},
		"chk-ft1": {ID: "chk-ft1", Status: "todo", Type: "feature", Path: "data/chk-ft1--feature.md", Parent: "chk-ep1"},
		"chk-tk1": {ID: "chk-tk1", Status: "todo", Type: "task", Path: "data/chk-tk1--task.md", Parent: "chk-ft1", Milestone: "chk-ms2"},
		// Direct child assigned under an assigned parent, spelled short-form.
		"chk-tk2": {ID: "chk-tk2", Status: "todo", Type: "task", Path: "data/chk-tk2--task.md", Parent: "ep1", Milestone: "ms1"},
		// The control: assigned under an unassigned epic stays silent.
		"chk-ep2": {ID: "chk-ep2", Status: "todo", Type: "epic", Path: "data/chk-ep2--epic.md"},
		"chk-tk3": {ID: "chk-tk3", Status: "todo", Type: "task", Path: "data/chk-tk3--task.md", Parent: "chk-ep2", Milestone: "chk-ms1"},
	}

	result := CheckAllLinksInMap(nibs, "", "chk-")

	want := []AssignmentConflict{
		{NibID: "chk-tk1", Path: "data/chk-tk1--task.md", Milestone: "chk-ms2", AncestorID: "chk-ep1", AncestorMilestone: "chk-ms1"},
		{NibID: "chk-tk2", Path: "data/chk-tk2--task.md", Milestone: "chk-ms1", AncestorID: "chk-ep1", AncestorMilestone: "chk-ms1"},
	}
	if !reflect.DeepEqual(result.AssignmentConflicts, want) {
		t.Errorf("AssignmentConflicts = %+v, want %+v", result.AssignmentConflicts, want)
	}
	if result.AssignmentIssues() != len(want) {
		t.Errorf("AssignmentIssues() = %d, want %d", result.AssignmentIssues(), len(want))
	}
	if result.TotalIssues() != len(want) {
		t.Errorf("TotalIssues() = %d, want %d (the assignment findings must count)", result.TotalIssues(), len(want))
	}
}

// TestCheckAllLinksInMapAssignmentResolution pins which assignments the scan
// judges: only RESOLVED ones, the membership rule the write path also applies.
// A dangling assignment is already a broken link, and one naming a
// non-milestone schedules nothing, so neither conflicts with anything; a
// hand-edited parent cycle terminates.
func TestCheckAllLinksInMapAssignmentResolution(t *testing.T) {
	tests := []struct {
		name           string
		childMilestone string
		wantConflicts  int
	}{
		{"resolved assignment conflicts", "chk-ms1", 1},
		{"dangling assignment is no assignment", "chk-nope", 0},
		{"assignment naming a non-milestone is no assignment", "chk-ep0", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibs := map[string]*nib.Nib{
				"chk-ms1": {ID: "chk-ms1", Status: "todo", Type: "milestone", Path: "data/chk-ms1--one.md"},
				"chk-ep0": {ID: "chk-ep0", Status: "todo", Type: "epic", Path: "data/chk-ep0--other.md"},
				"chk-ep1": {ID: "chk-ep1", Status: "todo", Type: "epic", Path: "data/chk-ep1--epic.md", Milestone: "chk-ms1"},
				"chk-tk1": {ID: "chk-tk1", Status: "todo", Type: "task", Path: "data/chk-tk1--task.md", Parent: "chk-ep1", Milestone: tt.childMilestone},
			}
			result := CheckAllLinksInMap(nibs, "", "chk-")
			if got := len(result.AssignmentConflicts); got != tt.wantConflicts {
				t.Errorf("conflicts = %d, want %d: %+v", got, tt.wantConflicts, result.AssignmentConflicts)
			}
		})
	}

	t.Run("a parent cycle terminates", func(t *testing.T) {
		nibs := map[string]*nib.Nib{
			"chk-ms1": {ID: "chk-ms1", Status: "todo", Type: "milestone", Path: "data/chk-ms1--one.md"},
			"chk-a":   {ID: "chk-a", Status: "todo", Type: "task", Path: "data/chk-a.md", Parent: "chk-b", Milestone: "chk-ms1"},
			"chk-b":   {ID: "chk-b", Status: "todo", Type: "task", Path: "data/chk-b.md", Parent: "chk-a"},
		}
		result := CheckAllLinksInMap(nibs, "", "chk-")
		if len(result.AssignmentConflicts) != 0 {
			t.Errorf("conflicts = %+v, want none (no assigned ancestor on the cycle)", result.AssignmentConflicts)
		}
	})
}
