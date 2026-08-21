package cmd

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/nibcore"
)

// Fixture bodies for the hierarchy-scan tests. Ids come from the filenames.
const (
	chkTopMilestoneNib = `---
version: 2
title: Top waypoint
status: todo
type: milestone
---

Body.
`

	// A milestone parented under a milestone: the shape the v2 migration
	// deliberately leaves untouched, and the write paths refuse — so only the
	// check scan ever names it.
	chkNestedMilestoneNib = `---
version: 2
title: Nested waypoint
status: todo
type: milestone
parent: chk-ms1
---

Body.
`

	chkTopEpicNib = `---
version: 2
title: Top epic
status: todo
type: epic
---

Body.
`

	// A legal nest, present as the control: it must stay unflagged.
	chkTaskUnderEpicNib = `---
version: 2
title: Legal task
status: todo
type: task
priority: normal
parent: chk-ep1
---

Body.
`
)

// hierarchyOffenderFiles is the fixture the hierarchy tests share: one illegal
// nest (milestone under milestone) beside one legal nest (task under epic).
func hierarchyOffenderFiles() map[string]string {
	return map[string]string{
		"chk-ms1--top.md":    chkTopMilestoneNib,
		"chk-ms2--nested.md": chkNestedMilestoneNib,
		"chk-ep1--epic.md":   chkTopEpicNib,
		"chk-tk1--legal.md":  chkTaskUnderEpicNib,
	}
}

// TestCheckReportsInvalidHierarchy pins the hierarchy finding end to end: an
// illegal nest sitting in the files (here a milestone parented under a
// milestone, the shape the v2 migration deliberately leaves untouched) is
// reported by `nibs check` naming the file, both types and the rule, and
// counted as an issue — the rule is enforced on write paths only, so without
// the finding the shape is invisible everywhere. A legal nest stays unflagged,
// and because the finding renders under Nib Links, the link-section success
// line is suppressed.
func TestCheckReportsInvalidHierarchy(t *testing.T) {
	t.Run("text report names the file, the types and the rule", func(t *testing.T) {
		app, _ := setupCheckTest(t, hierarchyOffenderFiles())
		var total int
		var runErr error
		out := captureStdout(t, func() { total, runErr = runCheck(app) })
		if runErr != nil {
			t.Fatalf("runCheck error = %v", runErr)
		}
		if total != 1 {
			t.Errorf("total issues = %d, want 1 (the illegal nest)", total)
		}
		for _, want := range []string{
			"data/chk-ms2--nested.md",
			"milestone is parented under milestone chk-ms1",
			"milestone cannot have a parent",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("report should contain %q, got:\n%s", want, out)
			}
		}
		// The legal nest stays silent.
		if strings.Contains(out, "chk-tk1") {
			t.Errorf("a legal nest must stay unflagged, got:\n%s", out)
		}
		// The finding renders under Nib Links, so the section cannot also
		// claim the links are clean.
		if strings.Contains(out, "No link issues found") {
			t.Errorf("an illegal nest is a link issue and must suppress the success line, got:\n%s", out)
		}
	})

	t.Run("allowed parents are named for a type that takes some", func(t *testing.T) {
		// A feature under a task: unlike the milestone case, the rule names
		// the set of parent types that WOULD be legal.
		app, _ := setupCheckTest(t, map[string]string{
			"chk-ep1--epic.md":  chkTopEpicNib,
			"chk-tk1--task.md":  chkTaskUnderEpicNib,
			"chk-ft1--under.md": "---\nversion: 2\ntitle: Nested feature\nstatus: todo\ntype: feature\npriority: normal\nparent: chk-tk1\n---\n\nBody.\n",
		})
		var total int
		var runErr error
		out := captureStdout(t, func() { total, runErr = runCheck(app) })
		if runErr != nil {
			t.Fatalf("runCheck error = %v", runErr)
		}
		if total != 1 {
			t.Errorf("total issues = %d, want 1", total)
		}
		if !strings.Contains(out, "feature can only have a parent of type epic, not task") {
			t.Errorf("report should name the allowed parent types, got:\n%s", out)
		}
	})

	t.Run("json envelope carries invalid_hierarchies", func(t *testing.T) {
		app, _ := setupCheckTest(t, hierarchyOffenderFiles())
		checkJSON = true
		var runErr error
		out := captureStdout(t, func() { _, runErr = runCheck(app) })
		if runErr != nil {
			t.Fatalf("runCheck error = %v", runErr)
		}
		var got checkResult
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("unmarshal: %v\noutput: %s", err, out)
		}
		if got.Success {
			t.Error("success = true; want false with an illegal nest present")
		}
		if got.NibIssues == nil || len(got.NibIssues.InvalidHierarchies) != 1 {
			t.Fatalf("invalid_hierarchies = %+v, want exactly 1 entry", got.NibIssues)
		}
		want := nibcore.InvalidHierarchy{
			NibID: "chk-ms2", Path: "data/chk-ms2--nested.md", ParentID: "chk-ms1",
			ChildType: "milestone", ParentType: "milestone",
			Reason: "milestone cannot have a parent",
		}
		if !reflect.DeepEqual(got.NibIssues.InvalidHierarchies[0], want) {
			t.Errorf("invalid_hierarchies[0] = %+v, want %+v", got.NibIssues.InvalidHierarchies[0], want)
		}
	})

	t.Run("--fix leaves the nest alone and says it cannot fix it", func(t *testing.T) {
		app, nibsDir := setupCheckTest(t, hierarchyOffenderFiles())
		checkFix = true
		before, err := os.ReadFile(dataPath(nibsDir, "chk-ms2--nested.md"))
		if err != nil {
			t.Fatal(err)
		}
		var total int
		var runErr error
		out := captureStdout(t, func() { total, runErr = runCheck(app) })
		if runErr != nil {
			t.Fatalf("runCheck error = %v", runErr)
		}
		after, err := os.ReadFile(dataPath(nibsDir, "chk-ms2--nested.md"))
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != string(before) {
			t.Errorf("--fix rewrote the nested milestone's file; re-parenting is not provable intent:\n%s", after)
		}
		if !strings.Contains(out, "Cannot auto-fix") || !strings.Contains(out, "not provable intent") {
			t.Errorf("--fix should refuse with the not-provable-intent wording, got:\n%s", out)
		}
		if !strings.Contains(out, "nibs mv") {
			t.Errorf("--fix should point at the manual remedy, got:\n%s", out)
		}
		if total != 1 {
			t.Errorf("total issues after --fix = %d, want 1 (the nest remains outstanding)", total)
		}
	})
}

// TestCheckHierarchyCleanStore is the false-positive guard at the command
// layer: a store whose every nest is legal reports no hierarchy finding and
// still passes overall.
func TestCheckHierarchyCleanStore(t *testing.T) {
	app, _ := setupCheckTest(t, map[string]string{
		"chk-ms1--top.md":   chkTopMilestoneNib,
		"chk-ep1--epic.md":  chkTopEpicNib,
		"chk-tk1--legal.md": chkTaskUnderEpicNib,
	})
	var total int
	var runErr error
	out := captureStdout(t, func() { total, runErr = runCheck(app) })
	if runErr != nil {
		t.Fatalf("runCheck error = %v", runErr)
	}
	if total != 0 {
		t.Errorf("total issues = %d, want 0 for legal nests\noutput:\n%s", total, out)
	}
	if !strings.Contains(out, "All checks passed") || !strings.Contains(out, "No link issues found") {
		t.Errorf("clean store should report all clear, got:\n%s", out)
	}
	if strings.Contains(out, "parented under") {
		t.Errorf("clean store output mentions a hierarchy finding:\n%s", out)
	}
}
