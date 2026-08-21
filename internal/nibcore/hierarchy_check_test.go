package nibcore

import (
	"reflect"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
)

// TestCheckAllLinksInMapFlagsInvalidHierarchy pins the hierarchy-rule finding:
// a nib whose parent's type the hierarchy rules refuse (a milestone parented
// under a milestone, a feature under a task) is reported with the file, both
// effective types, the resolved parent id, and the parent types that would be
// legal. The rule is strict on the write paths only and the v2 migration
// deliberately leaves an illegal nest untouched, so without the finding a
// file-level offender is invisible everywhere. The findings arrive sorted by
// nib id and count as issues.
func TestCheckAllLinksInMapFlagsInvalidHierarchy(t *testing.T) {
	nibs := map[string]*nib.Nib{
		// Ids deliberately out of map-literal order to exercise the sorting.
		"chk-ms2": {ID: "chk-ms2", Status: "todo", Type: "milestone", Path: "data/chk-ms2--nested.md", Parent: "chk-ms1"},
		"chk-ms1": {ID: "chk-ms1", Status: "todo", Type: "milestone", Path: "data/chk-ms1--top.md"},
		"chk-ft1": {ID: "chk-ft1", Status: "todo", Type: "feature", Path: "data/chk-ft1--under-task.md", Parent: "chk-tk1"},
		// Legal shape, present as the control: a task under an epic stays
		// unflagged.
		"chk-tk1": {ID: "chk-tk1", Status: "todo", Type: "task", Path: "data/chk-tk1--work.md", Parent: "chk-ep1"},
		"chk-ep1": {ID: "chk-ep1", Status: "todo", Type: "epic", Path: "data/chk-ep1--top.md"},
		// A type-less nib is judged as the default type, the way every write
		// path judges it — so its ChildType arrives as "task", not "".
		"chk-un1": {ID: "chk-un1", Status: "todo", Path: "data/chk-un1--typeless.md", Parent: "chk-tk1"},
	}

	result := CheckAllLinksInMap(nibs, "", "chk-")

	want := []InvalidHierarchy{
		{
			NibID: "chk-ft1", Path: "data/chk-ft1--under-task.md", ParentID: "chk-tk1",
			ChildType: "feature", ParentType: "task", Allowed: []string{"epic"},
			Reason: "feature can only have a parent of type epic, not task",
		},
		{
			NibID: "chk-ms2", Path: "data/chk-ms2--nested.md", ParentID: "chk-ms1",
			ChildType: "milestone", ParentType: "milestone",
			Reason: "milestone cannot have a parent",
		},
		{
			NibID: "chk-un1", Path: "data/chk-un1--typeless.md", ParentID: "chk-tk1",
			ChildType: "task", ParentType: "task", Allowed: []string{"epic", "feature", "bug"},
			Reason: "task can only have a parent of type epic, feature, or bug, not task",
		},
	}
	if !reflect.DeepEqual(result.InvalidHierarchies, want) {
		t.Errorf("InvalidHierarchies = %+v, want %+v", result.InvalidHierarchies, want)
	}
	if result.TotalIssues() != len(want) {
		t.Errorf("TotalIssues() = %d, want %d (the hierarchy findings must count)", result.TotalIssues(), len(want))
	}
	if result.HierarchyIssues() != len(want) {
		t.Errorf("HierarchyIssues() = %d, want %d", result.HierarchyIssues(), len(want))
	}
}

// TestCheckAllLinksInMapHierarchyParentResolution pins which parents the
// hierarchy scan judges. A parent is resolved the way every other link check
// resolves one — exact id, then the configured prefix prepended — so a
// short-form spelling is judged under the resolved nib's type and reported
// under its full id. A parent that resolves to nothing is left to the
// broken-link finding (its type is unknowable), and one resolving to the nib
// itself is left to the self-link finding, so neither shape is double-reported.
func TestCheckAllLinksInMapHierarchyParentResolution(t *testing.T) {
	tests := []struct {
		name          string
		parent        string
		wantHierarchy int
		wantBroken    int
		wantSelf      int
	}{
		{"short-form parent resolves and is judged", "ms1", 1, 0, 0},
		{"unresolvable parent is a broken link only", "chk-nope", 0, 1, 0},
		{"self parent is a self link only", "chk-sub1", 0, 0, 1},
		{"short-form self parent is a self link only", "sub1", 0, 0, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The subject is a milestone, so ANY judged parent is illegal for
			// it — a finding can only be absent because the scan skipped the
			// parent, not because the shape happened to be legal.
			nibs := map[string]*nib.Nib{
				"chk-ms1":  {ID: "chk-ms1", Status: "todo", Type: "milestone", Path: "data/chk-ms1--top.md"},
				"chk-sub1": {ID: "chk-sub1", Status: "todo", Type: "milestone", Path: "data/chk-sub1--subject.md", Parent: tt.parent},
			}

			result := CheckAllLinksInMap(nibs, "", "chk-")

			if got := len(result.InvalidHierarchies); got != tt.wantHierarchy {
				t.Errorf("hierarchy findings = %d, want %d (parent %q): %+v",
					got, tt.wantHierarchy, tt.parent, result.InvalidHierarchies)
			}
			if tt.wantHierarchy > 0 && result.InvalidHierarchies[0].ParentID != "chk-ms1" {
				t.Errorf("ParentID = %q, want the resolved full id %q",
					result.InvalidHierarchies[0].ParentID, "chk-ms1")
			}
			if got := len(result.BrokenLinks); got != tt.wantBroken {
				t.Errorf("broken links = %d, want %d (parent %q)", got, tt.wantBroken, tt.parent)
			}
			if got := len(result.SelfLinks); got != tt.wantSelf {
				t.Errorf("self links = %d, want %d (parent %q)", got, tt.wantSelf, tt.parent)
			}
			if want := tt.wantHierarchy + tt.wantBroken + tt.wantSelf; result.TotalIssues() != want {
				t.Errorf("TotalIssues() = %d, want %d (parent %q)", result.TotalIssues(), want, tt.parent)
			}
		})
	}
}

// TestCheckAllLinksInMapHierarchyCleanStore is the false-positive guard: every
// legal parent/type combination, roots included, reports nothing.
func TestCheckAllLinksInMapHierarchyCleanStore(t *testing.T) {
	nibs := map[string]*nib.Nib{
		"chk-ms1": {ID: "chk-ms1", Status: "todo", Type: "milestone", Path: "data/chk-ms1--way.md"},
		"chk-ep1": {ID: "chk-ep1", Status: "todo", Type: "epic", Path: "data/chk-ep1--top.md"},
		"chk-ft1": {ID: "chk-ft1", Status: "todo", Type: "feature", Path: "data/chk-ft1--f.md", Parent: "chk-ep1"},
		"chk-bg1": {ID: "chk-bg1", Status: "todo", Type: "bug", Path: "data/chk-bg1--b.md", Parent: "chk-ep1"},
		"chk-tk1": {ID: "chk-tk1", Status: "todo", Type: "task", Path: "data/chk-tk1--t.md", Parent: "chk-ft1"},
		"chk-tk2": {ID: "chk-tk2", Status: "todo", Type: "task", Path: "data/chk-tk2--t2.md", Parent: "chk-ep1"},
		"chk-rs1": {ID: "chk-rs1", Status: "todo", Type: "research", Path: "data/chk-rs1--r.md", Parent: "chk-bg1"},
	}

	result := CheckAllLinksInMap(nibs, "", "chk-")

	if len(result.InvalidHierarchies) != 0 {
		t.Errorf("InvalidHierarchies = %+v, want none for legal shapes", result.InvalidHierarchies)
	}
	if result.TotalIssues() != 0 {
		t.Errorf("TotalIssues() = %d, want 0", result.TotalIssues())
	}
}
