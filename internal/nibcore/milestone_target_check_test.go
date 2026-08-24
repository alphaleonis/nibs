package nibcore

import (
	"reflect"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
)

// TestCheckAllLinksInMapFlagsInvalidMilestoneTarget pins the milestone-target
// finding: an assignment naming a nib that exists but is not milestone-typed is
// reported with the target's resolved id and its effective type. Short-form
// spellings resolve the way every other link check resolves them, a type-less
// target is judged as the default type, findings arrive sorted by nib id, and
// they count as issues.
func TestCheckAllLinksInMapFlagsInvalidMilestoneTarget(t *testing.T) {
	nibs := map[string]*nib.Nib{
		"chk-ms1": {ID: "chk-ms1", Status: "todo", Type: "milestone", Path: "data/chk-ms1--one.md"},
		"chk-ft1": {ID: "chk-ft1", Status: "todo", Type: "feature", Path: "data/chk-ft1--feature.md"},
		// Type-less, so judged as the default type the write paths judge it as.
		"chk-un1": {ID: "chk-un1", Status: "todo", Path: "data/chk-un1--untyped.md"},
		"chk-tk2": {ID: "chk-tk2", Status: "todo", Type: "task", Path: "data/chk-tk2--task.md", Milestone: "chk-ft1"},
		// Short-form spelling: the report names the RESOLVED id.
		"chk-tk1": {ID: "chk-tk1", Status: "todo", Type: "task", Path: "data/chk-tk1--task.md", Milestone: "ft1"},
		"chk-tk3": {ID: "chk-tk3", Status: "todo", Type: "task", Path: "data/chk-tk3--task.md", Milestone: "chk-un1"},
		// The control: a real milestone target stays silent.
		"chk-tk4": {ID: "chk-tk4", Status: "todo", Type: "task", Path: "data/chk-tk4--task.md", Milestone: "chk-ms1"},
	}

	result := CheckAllLinksInMap(nibs, "", "chk-")

	want := []InvalidMilestoneTarget{
		{NibID: "chk-tk1", Path: "data/chk-tk1--task.md", Target: "chk-ft1", TargetType: "feature"},
		{NibID: "chk-tk2", Path: "data/chk-tk2--task.md", Target: "chk-ft1", TargetType: "feature"},
		{NibID: "chk-tk3", Path: "data/chk-tk3--task.md", Target: "chk-un1", TargetType: nib.DefaultType},
	}
	if !reflect.DeepEqual(result.InvalidMilestoneTargets, want) {
		t.Errorf("InvalidMilestoneTargets = %+v, want %+v", result.InvalidMilestoneTargets, want)
	}
	if result.MilestoneTargetIssues() != len(want) {
		t.Errorf("MilestoneTargetIssues() = %d, want %d", result.MilestoneTargetIssues(), len(want))
	}
	if result.TotalIssues() != len(want) {
		t.Errorf("TotalIssues() = %d, want %d (the milestone-target findings must count)", result.TotalIssues(), len(want))
	}
}

// TestCheckAllLinksInMapMilestoneTargetExclusions pins what the milestone-target
// scan does NOT claim. A target that does not resolve is already a broken link
// and one resolving to the nib itself is already a self link — their type is
// either unknowable or the subject's own, so neither is judged again here. And a
// MILESTONE-typed subject carrying `milestone:` is InvalidAxes' finding alone:
// its type takes no assignment axis, so the whole key has to go and naming the
// target's type would send the reader to repoint a key they are about to delete.
func TestCheckAllLinksInMapMilestoneTargetExclusions(t *testing.T) {
	base := func() map[string]*nib.Nib {
		return map[string]*nib.Nib{
			"chk-ms1": {ID: "chk-ms1", Status: "todo", Type: "milestone", Path: "data/chk-ms1--one.md"},
			"chk-ft1": {ID: "chk-ft1", Status: "todo", Type: "feature", Path: "data/chk-ft1--feature.md"},
		}
	}

	t.Run("a dangling milestone stays a broken link", func(t *testing.T) {
		nibs := base()
		nibs["chk-tk1"] = &nib.Nib{ID: "chk-tk1", Status: "todo", Type: "task", Path: "data/chk-tk1--task.md", Milestone: "chk-nope"}

		result := CheckAllLinksInMap(nibs, "", "chk-")

		if len(result.InvalidMilestoneTargets) != 0 {
			t.Errorf("InvalidMilestoneTargets = %+v, want none (a dangling target has no type to name)", result.InvalidMilestoneTargets)
		}
		want := []BrokenLink{{NibID: "chk-tk1", LinkType: "milestone", Target: "chk-nope"}}
		if !reflect.DeepEqual(result.BrokenLinks, want) {
			t.Errorf("BrokenLinks = %+v, want %+v", result.BrokenLinks, want)
		}
	})

	t.Run("a self-referential milestone stays a self link", func(t *testing.T) {
		nibs := base()
		nibs["chk-tk1"] = &nib.Nib{ID: "chk-tk1", Status: "todo", Type: "task", Path: "data/chk-tk1--task.md", Milestone: "chk-tk1"}

		result := CheckAllLinksInMap(nibs, "", "chk-")

		if len(result.InvalidMilestoneTargets) != 0 {
			t.Errorf("InvalidMilestoneTargets = %+v, want none (a self link is already reported as one)", result.InvalidMilestoneTargets)
		}
		want := []SelfLink{{NibID: "chk-tk1", LinkType: "milestone"}}
		if !reflect.DeepEqual(result.SelfLinks, want) {
			t.Errorf("SelfLinks = %+v, want %+v", result.SelfLinks, want)
		}
	})

	t.Run("a milestone-typed subject is left to the axis rule", func(t *testing.T) {
		nibs := base()
		nibs["chk-ms2"] = &nib.Nib{ID: "chk-ms2", Status: "todo", Type: "milestone", Path: "data/chk-ms2--two.md", Milestone: "chk-ft1"}

		result := CheckAllLinksInMap(nibs, "", "chk-")

		if len(result.InvalidMilestoneTargets) != 0 {
			t.Errorf("InvalidMilestoneTargets = %+v, want none (the whole key has to go, so the axis rule speaks for it alone)", result.InvalidMilestoneTargets)
		}
		if len(result.InvalidAxes) != 1 || result.InvalidAxes[0].NibID != "chk-ms2" {
			t.Errorf("InvalidAxes = %+v, want exactly the chk-ms2 finding", result.InvalidAxes)
		}
		if result.TotalIssues() != 1 {
			t.Errorf("TotalIssues() = %d, want 1 (one finding, not a double report)", result.TotalIssues())
		}
	})

	t.Run("a real milestone target is silent", func(t *testing.T) {
		nibs := base()
		nibs["chk-tk1"] = &nib.Nib{ID: "chk-tk1", Status: "todo", Type: "task", Path: "data/chk-tk1--task.md", Milestone: "chk-ms1"}

		result := CheckAllLinksInMap(nibs, "", "chk-")

		if result.TotalIssues() != 0 {
			t.Errorf("TotalIssues() = %d, want 0: %+v", result.TotalIssues(), result)
		}
	})
}
