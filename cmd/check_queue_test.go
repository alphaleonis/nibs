package cmd

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/nibcore"
)

// closedMilestoneQueueFiles is the fixture the closed-milestone-queue tests
// share: a milestone closed as completed while two open nibs are still assigned
// to it (the shape both write surfaces refuse — decision 1.5), beside a
// milestone whose whole queue is closed and a parked one that keeps its queue
// on purpose, as the controls.
func closedMilestoneQueueFiles() map[string]string {
	return map[string]string{
		"chk-ms1--one.md":    "---\nversion: 2\ntitle: Waypoint one\nstatus: completed\ntype: milestone\n---\n",
		"chk-tk1--task.md":   "---\nversion: 2\ntitle: Still open\nstatus: todo\ntype: task\nmilestone: chk-ms1\nmilestone_order: a0\n---\n",
		"chk-tk2--task.md":   "---\nversion: 2\ntitle: Underway\nstatus: in-progress\ntype: task\nmilestone: chk-ms1\nmilestone_order: b0\n---\n",
		"chk-tk3--done.md":   "---\nversion: 2\ntitle: Finished\nstatus: completed\ntype: task\nmilestone: chk-ms1\nmilestone_order: c0\n---\n",
		"chk-ms2--two.md":    "---\nversion: 2\ntitle: Waypoint two\nstatus: completed\ntype: milestone\n---\n",
		"chk-tk4--done.md":   "---\nversion: 2\ntitle: Also finished\nstatus: completed\ntype: task\nmilestone: chk-ms2\nmilestone_order: a0\n---\n",
		"chk-ms3--parked.md": "---\nversion: 2\ntitle: Waypoint three\nstatus: deferred\ntype: milestone\n---\n",
		"chk-tk5--task.md":   "---\nversion: 2\ntitle: Waiting\nstatus: todo\ntype: task\nmilestone: chk-ms3\nmilestone_order: a0\n---\n",
	}
}

// TestCheckReportsClosedMilestoneQueue pins the read-side half of decision 1.5:
// a milestone carrying a releasing status over an open queue — which `nibs close`
// and updateNib both refuse to CLOSE into, though the assignment door still
// reaches it (nibs-l5df) — is reported
// by `nibs check` naming the milestone and its open entries, counted as an
// issue, rendered under Nib Links (suppressing the link success line), carried
// in the --json envelope, and left alone by --fix since disposing of a queue is
// not provable intent.
func TestCheckReportsClosedMilestoneQueue(t *testing.T) {
	t.Run("text report names the milestone and its open queue", func(t *testing.T) {
		app, _ := setupCheckTest(t, closedMilestoneQueueFiles())
		var total int
		var runErr error
		out := captureStdout(t, func() { total, runErr = runCheck(app) })
		if runErr != nil {
			t.Fatalf("runCheck error = %v", runErr)
		}
		if total != 1 {
			t.Errorf("total issues = %d, want 1 (the closed queue)", total)
		}
		for _, want := range []string{
			"chk-ms1",
			"data/chk-ms1--one.md",
			"closed as completed while 2 open nibs are still assigned to its queue",
			"chk-tk1, chk-tk2",
			"--clear milestone",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("report should contain %q, got:\n%s", want, out)
			}
		}
		for _, unwanted := range []string{"chk-ms2", "chk-ms3", "chk-tk3"} {
			if strings.Contains(out, unwanted) {
				t.Errorf("%s must stay unflagged, got:\n%s", unwanted, out)
			}
		}
		if strings.Contains(out, "No link issues found") {
			t.Errorf("a closed queue is a link issue and must suppress the success line, got:\n%s", out)
		}
	})

	t.Run("json envelope carries closed_milestone_queues", func(t *testing.T) {
		app, _ := setupCheckTest(t, closedMilestoneQueueFiles())
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
			t.Error("success = true; want false with a closed queue present")
		}
		if got.NibIssues == nil || len(got.NibIssues.ClosedMilestoneQueues) != 1 {
			t.Fatalf("closed_milestone_queues = %+v, want exactly 1 entry", got.NibIssues)
		}
		want := nibcore.ClosedMilestoneQueue{
			NibID: "chk-ms1", Path: "data/chk-ms1--one.md", Status: "completed",
			Open: []string{"chk-tk1", "chk-tk2"},
		}
		if !reflect.DeepEqual(got.NibIssues.ClosedMilestoneQueues[0], want) {
			t.Errorf("closed_milestone_queues[0] = %+v, want %+v", got.NibIssues.ClosedMilestoneQueues[0], want)
		}
	})

	t.Run("--fix leaves every file alone and says it cannot fix it", func(t *testing.T) {
		app, nibsDir := setupCheckTest(t, closedMilestoneQueueFiles())
		checkFix = true
		before := map[string]string{}
		for name := range closedMilestoneQueueFiles() {
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
				t.Errorf("--fix rewrote %s; disposing of a queue is not provable intent:\n%s", name, after)
			}
		}
		if !strings.Contains(out, "Cannot auto-fix chk-ms1") || !strings.Contains(out, "not provable intent") {
			t.Errorf("--fix should refuse with the not-provable-intent wording, got:\n%s", out)
		}
		if total != 1 {
			t.Errorf("total issues after --fix = %d, want 1 (the closed queue remains outstanding)", total)
		}
	})
}

// TestCheckClosedMilestoneQueueCleanStore is the false-positive guard at the
// command layer: the states decision 1.5 permits report nothing and the store
// passes.
func TestCheckClosedMilestoneQueueCleanStore(t *testing.T) {
	files := closedMilestoneQueueFiles()
	delete(files, "chk-ms1--one.md")
	delete(files, "chk-tk1--task.md")
	delete(files, "chk-tk2--task.md")
	delete(files, "chk-tk3--done.md")

	app, _ := setupCheckTest(t, files)
	var total int
	var runErr error
	out := captureStdout(t, func() { total, runErr = runCheck(app) })
	if runErr != nil {
		t.Fatalf("runCheck error = %v", runErr)
	}
	if total != 0 {
		t.Errorf("total issues = %d, want 0, got:\n%s", total, out)
	}
	if !strings.Contains(out, "No link issues found") {
		t.Errorf("a clean store should report the link success line, got:\n%s", out)
	}
}
