package cmd

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/nibcore"
)

// assignmentConflictFiles is the fixture the assignment-exclusivity tests
// share: an epic assigned to one milestone with a child task assigned to
// another (the shape the write path refuses, decision 1.2), beside a legal
// assignment under an unassigned epic as the control.
func assignmentConflictFiles() map[string]string {
	return map[string]string{
		"chk-ms1--one.md":   "---\nversion: 2\ntitle: Waypoint one\nstatus: todo\ntype: milestone\n---\n",
		"chk-ms2--two.md":   "---\nversion: 2\ntitle: Waypoint two\nstatus: todo\ntype: milestone\n---\n",
		"chk-ep1--epic.md":  "---\nversion: 2\ntitle: Assigned epic\nstatus: todo\ntype: epic\nmilestone: chk-ms1\nmilestone_order: a0\n---\n",
		"chk-tk1--task.md":  "---\nversion: 2\ntitle: Assigned child\nstatus: todo\ntype: task\nparent: chk-ep1\nmilestone: chk-ms2\nmilestone_order: a0\n---\n",
		"chk-ep2--free.md":  "---\nversion: 2\ntitle: Free epic\nstatus: todo\ntype: epic\n---\n",
		"chk-tk2--legal.md": "---\nversion: 2\ntitle: Legal child\nstatus: todo\ntype: task\nparent: chk-ep2\nmilestone: chk-ms1\nmilestone_order: b0\n---\n",
	}
}

// TestCheckReportsAssignmentConflict pins the read-side half of assignment
// exclusivity: a nib assigned while its ancestor is assigned — reachable only
// by hand edit, since the write paths refuse it — is reported by `nibs check`
// naming both nibs and both milestones, counted as an issue, rendered under
// Nib Links (suppressing the link success line), carried in the --json
// envelope, and left alone by --fix since which assignment to drop is not
// provable intent.
func TestCheckReportsAssignmentConflict(t *testing.T) {
	t.Run("text report names the pair", func(t *testing.T) {
		app, _ := setupCheckTest(t, assignmentConflictFiles())
		var total int
		var runErr error
		out := captureStdout(t, func() { total, runErr = runCheck(app) })
		if runErr != nil {
			t.Fatalf("runCheck error = %v", runErr)
		}
		if total != 1 {
			t.Errorf("total issues = %d, want 1 (the conflict)", total)
		}
		for _, want := range []string{
			"chk-tk1",
			"data/chk-tk1--task.md",
			"assigned to milestone chk-ms2 while its ancestor chk-ep1 is assigned to milestone chk-ms1",
			"--clear milestone",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("report should contain %q, got:\n%s", want, out)
			}
		}
		if strings.Contains(out, "chk-tk2") {
			t.Errorf("the legal assignment must stay unflagged, got:\n%s", out)
		}
		if strings.Contains(out, "No link issues found") {
			t.Errorf("a conflict is a link issue and must suppress the success line, got:\n%s", out)
		}
	})

	t.Run("json envelope carries assignment_conflicts", func(t *testing.T) {
		app, _ := setupCheckTest(t, assignmentConflictFiles())
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
			t.Error("success = true; want false with a conflict present")
		}
		if got.NibIssues == nil || len(got.NibIssues.AssignmentConflicts) != 1 {
			t.Fatalf("assignment_conflicts = %+v, want exactly 1 entry", got.NibIssues)
		}
		want := nibcore.AssignmentConflict{
			NibID: "chk-tk1", Path: "data/chk-tk1--task.md", Milestone: "chk-ms2",
			AncestorID: "chk-ep1", AncestorMilestone: "chk-ms1",
		}
		if !reflect.DeepEqual(got.NibIssues.AssignmentConflicts[0], want) {
			t.Errorf("assignment_conflicts[0] = %+v, want %+v", got.NibIssues.AssignmentConflicts[0], want)
		}
	})

	t.Run("--fix leaves both files alone and says it cannot fix it", func(t *testing.T) {
		app, nibsDir := setupCheckTest(t, assignmentConflictFiles())
		checkFix = true
		before := map[string]string{}
		for _, name := range []string{"chk-tk1--task.md", "chk-ep1--epic.md"} {
			before[name] = readFileT(t, dataPath(nibsDir, name))
		}
		var total int
		var runErr error
		out := captureStdout(t, func() { total, runErr = runCheck(app) })
		if runErr != nil {
			t.Fatalf("runCheck error = %v", runErr)
		}
		for name, content := range before {
			if after := readFileT(t, dataPath(nibsDir, name)); after != content {
				t.Errorf("--fix rewrote %s; which assignment to drop is not provable intent:\n%s", name, after)
			}
		}
		if !strings.Contains(out, "Cannot auto-fix chk-tk1") || !strings.Contains(out, "not provable intent") {
			t.Errorf("--fix should refuse with the not-provable-intent wording, got:\n%s", out)
		}
		if total != 1 {
			t.Errorf("total issues after --fix = %d, want 1 (the conflict remains outstanding)", total)
		}
	})
}

// TestCheckAssignmentCleanStore is the false-positive guard at the command
// layer: assignments that respect exclusivity report nothing and the store
// passes.
func TestCheckAssignmentCleanStore(t *testing.T) {
	files := assignmentConflictFiles()
	delete(files, "chk-tk1--task.md")
	app, _ := setupCheckTest(t, files)
	var total int
	var runErr error
	out := captureStdout(t, func() { total, runErr = runCheck(app) })
	if runErr != nil {
		t.Fatalf("runCheck error = %v", runErr)
	}
	if total != 0 {
		t.Errorf("total issues = %d, want 0\noutput:\n%s", total, out)
	}
	if !strings.Contains(out, "All checks passed") || !strings.Contains(out, "No link issues found") {
		t.Errorf("clean store should report all clear, got:\n%s", out)
	}
	if strings.Contains(out, "while its ancestor") {
		t.Errorf("clean store output mentions an assignment conflict:\n%s", out)
	}
}
