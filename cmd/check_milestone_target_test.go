package cmd

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/nibcore"
)

// invalidMilestoneTargetFiles is the fixture the milestone-target tests share:
// a task whose `milestone:` names a FEATURE (the shape the write path refuses),
// beside a task assigned to a real milestone as the control.
func invalidMilestoneTargetFiles() map[string]string {
	return map[string]string{
		"chk-ms1--one.md":     "---\nversion: 2\ntitle: Waypoint one\nstatus: todo\ntype: milestone\n---\n",
		"chk-ft1--feature.md": "---\nversion: 2\ntitle: A feature\nstatus: todo\ntype: feature\n---\n",
		"chk-tk1--task.md":    "---\nversion: 2\ntitle: Misassigned\nstatus: todo\ntype: task\nmilestone: chk-ft1\nmilestone_order: a0\n---\n",
		"chk-tk2--legal.md":   "---\nversion: 2\ntitle: Legal member\nstatus: todo\ntype: task\nmilestone: chk-ms1\nmilestone_order: b0\n---\n",
	}
}

// TestCheckReportsInvalidMilestoneTarget pins the read-side half of the
// milestone-target rule: an assignment whose target exists but is not
// milestone-typed — reachable only by hand edit, since the write path refuses
// it — is reported by `nibs check` naming the file, the target and the target's
// type, counted as an issue and rendered under Nib Links (suppressing the link
// success line).
func TestCheckReportsInvalidMilestoneTarget(t *testing.T) {
	t.Run("text report names the target and its type", func(t *testing.T) {
		app, _ := setupCheckTest(t, invalidMilestoneTargetFiles())
		var total int
		var runErr error
		out := captureStdout(t, func() { total, runErr = runCheck(app) })
		if runErr != nil {
			t.Fatalf("runCheck error = %v", runErr)
		}
		if total != 1 {
			t.Errorf("total issues = %d, want 1 (the invalid target)", total)
		}
		for _, want := range []string{
			"chk-tk1",
			"data/chk-tk1--task.md",
			"chk-ft1",
			"feature",
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
			t.Errorf("an invalid target is a link issue and must suppress the success line, got:\n%s", out)
		}
	})

	t.Run("json envelope carries invalid_milestone_targets", func(t *testing.T) {
		app, _ := setupCheckTest(t, invalidMilestoneTargetFiles())
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
			t.Error("success = true; want false with an invalid target present")
		}
		if got.NibIssues == nil || len(got.NibIssues.InvalidMilestoneTargets) != 1 {
			t.Fatalf("invalid_milestone_targets = %+v, want exactly 1 entry", got.NibIssues)
		}
		want := nibcore.InvalidMilestoneTarget{
			NibID: "chk-tk1", Path: "data/chk-tk1--task.md",
			Target: "chk-ft1", TargetType: "feature",
		}
		if !reflect.DeepEqual(got.NibIssues.InvalidMilestoneTargets[0], want) {
			t.Errorf("invalid_milestone_targets[0] = %+v, want %+v", got.NibIssues.InvalidMilestoneTargets[0], want)
		}
	})

	t.Run("--fix leaves the file byte-identical and says it cannot fix it", func(t *testing.T) {
		app, nibsDir := setupCheckTest(t, invalidMilestoneTargetFiles())
		checkFix = true
		before := map[string]string{}
		for _, name := range []string{"chk-tk1--task.md", "chk-ft1--feature.md"} {
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
				t.Errorf("--fix rewrote %s; choosing the right milestone is not provable intent:\n%s", name, after)
			}
		}
		if !strings.Contains(out, "Cannot auto-fix chk-tk1") || !strings.Contains(out, "not provable intent") {
			t.Errorf("--fix should refuse with the not-provable-intent wording, got:\n%s", out)
		}
		if !strings.Contains(out, "--milestone <id>") || !strings.Contains(out, "--clear milestone") {
			t.Errorf("--fix should name both remedies, got:\n%s", out)
		}
		if total != 1 {
			t.Errorf("total issues after --fix = %d, want 1 (the invalid target remains outstanding)", total)
		}
	})

	// The subtest above pins the path where `--fix` never reaches the writer at
	// all: runCheck calls FixBrokenLinks only when a broken link, self link or
	// broken document is present, and this fixture has none, so the files are
	// trivially unchanged and its byte-identity assertion cannot fail. That makes
	// it a guard on the message, not on the write. This one opens the guard with a
	// broken blocked_by so the rewrite genuinely runs, then asserts the
	// hand-authored `milestone:` survives it.
	//
	// The codebase learned this exact lesson once already: `--fix` deleted a live
	// blocked_by edge, which is why TestFixBrokenLinksLeavesResolvableShortIDsOnDisk
	// exists in internal/nibcore.
	t.Run("--fix runs and still leaves the assignment alone", func(t *testing.T) {
		files := invalidMilestoneTargetFiles()
		files["chk-tk1--task.md"] = "---\nversion: 2\ntitle: Misassigned\nstatus: todo\ntype: task\n" +
			"milestone: chk-ft1\nmilestone_order: a0\nblocked_by:\n  - chk-gone\n---\n"
		app, nibsDir := setupCheckTest(t, files)
		checkFix = true

		out := captureStdout(t, func() { _, _ = runCheck(app) })

		// Guard the guard: with no broken link the writer never runs, and the
		// assertion below would then pass against any implementation at all.
		if !strings.Contains(out, "removed broken link") {
			t.Fatalf("premise failed: --fix never reached FixBrokenLinks, so this proves nothing:\n%s", out)
		}
		if after := readFileT(t, dataPath(nibsDir, "chk-tk1--task.md")); !strings.Contains(after, "milestone: chk-ft1") {
			t.Errorf("--fix destroyed the hand-authored assignment while rewriting the file:\n%s", after)
		}
	})
}

// TestCheckInvalidMilestoneTargetEmitsEmptyArray pins the constructor entry
// rather than the finding: with no invalid target in the store the --json
// field is an empty array, never null. A consumer that ranges over it must not
// have to special-case a missing collection.
func TestCheckInvalidMilestoneTargetEmitsEmptyArray(t *testing.T) {
	files := invalidMilestoneTargetFiles()
	delete(files, "chk-tk1--task.md")
	app, _ := setupCheckTest(t, files)
	checkJSON = true
	var runErr error
	out := captureStdout(t, func() { _, runErr = runCheck(app) })
	if runErr != nil {
		t.Fatalf("runCheck error = %v", runErr)
	}
	if !strings.Contains(out, `"invalid_milestone_targets": []`) {
		t.Errorf("envelope should carry an empty array, got:\n%s", out)
	}
}

// TestCheckMilestoneTargetCleanStore is the false-positive guard at the command
// layer: assignments naming real milestones report nothing and the store passes.
func TestCheckMilestoneTargetCleanStore(t *testing.T) {
	files := invalidMilestoneTargetFiles()
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
	if strings.Contains(out, "not a milestone") {
		t.Errorf("clean store output mentions an invalid milestone target:\n%s", out)
	}
}
