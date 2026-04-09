package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func resetCloseFlags() {
	closeSummary = ""
	closeForce = false
	closeIfMatch = ""
	closeJSON = false
}

func TestResetCloseFlagsClearsAllState(t *testing.T) {
	closeSummary = "dirty"
	closeForce = true
	closeIfMatch = "dirty"
	closeJSON = true

	resetCloseFlags()

	if closeSummary != "" {
		t.Errorf("closeSummary not reset: %q", closeSummary)
	}
	if closeForce {
		t.Error("closeForce not reset")
	}
	if closeIfMatch != "" {
		t.Errorf("closeIfMatch not reset: %q", closeIfMatch)
	}
	if closeJSON {
		t.Error("closeJSON not reset")
	}
}

func setupCloseTest(t *testing.T, files map[string]string) string {
	t.Helper()
	t.Cleanup(func() { resetCloseFlags() })
	resetCloseFlags()

	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(nibsDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return nibsDir
}

func readNibFile(t *testing.T, nibsDir, filename string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(nibsDir, filename))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCloseBasic(t *testing.T) {
	nibsDir := setupCloseTest(t, map[string]string{
		"abc-1--my-task.md": "---\ntitle: My Task\nstatus: in-progress\ntype: task\n---\n\nSome body content.\n",
	})

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "abc-1", "--summary", "All done",
	})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("expected close to succeed, got: %v", err)
	}

	content := readNibFile(t, nibsDir, "abc-1--my-task.md")
	if !strings.Contains(content, "status: completed") {
		t.Errorf("expected status to be completed, got:\n%s", content)
	}
}

func TestCloseSummaryAppended(t *testing.T) {
	nibsDir := setupCloseTest(t, map[string]string{
		"sum-1--my-task.md": "---\ntitle: My Task\nstatus: in-progress\ntype: task\n---\n\nExisting body.\n",
	})

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "sum-1", "--summary", "Implemented the feature and added tests.",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	content := readNibFile(t, nibsDir, "sum-1--my-task.md")
	if !strings.Contains(content, "## Summary") {
		t.Errorf("expected ## Summary heading in body, got:\n%s", content)
	}
	if !strings.Contains(content, "Implemented the feature and added tests.") {
		t.Errorf("expected summary text in body, got:\n%s", content)
	}
	// Original body should still be there
	if !strings.Contains(content, "Existing body.") {
		t.Errorf("expected original body to be preserved, got:\n%s", content)
	}
}

func TestCloseFailsWithIncompleteChildren(t *testing.T) {
	nibsDir := setupCloseTest(t, map[string]string{
		"par-1--parent.md": "---\ntitle: Parent Epic\nstatus: in-progress\ntype: epic\n---\n\nEpic body.\n",
		"ch-1--child-done.md": "---\ntitle: Child Done\nstatus: completed\ntype: task\nparent: par-1\n---\n\nDone.\n",
		"ch-2--child-wip.md": "---\ntitle: Child WIP\nstatus: in-progress\ntype: task\nparent: par-1\n---\n\nStill working.\n",
	})

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "par-1", "--summary", "Done",
	})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when children are incomplete, got nil")
	}
	if !strings.Contains(err.Error(), "incomplete") {
		t.Errorf("error should mention incomplete children, got: %s", err)
	}
	if !strings.Contains(err.Error(), "ch-2") {
		t.Errorf("error should mention the incomplete child ID, got: %s", err)
	}
}

func TestCloseForceWithIncompleteChildren(t *testing.T) {
	nibsDir := setupCloseTest(t, map[string]string{
		"frc-1--parent.md": "---\ntitle: Parent\nstatus: in-progress\ntype: epic\n---\n\nBody.\n",
		"frc-2--child.md":  "---\ntitle: Child\nstatus: todo\ntype: task\nparent: frc-1\n---\n\nTodo.\n",
	})

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "frc-1", "--summary", "Forced close", "--force",
	})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("expected --force to bypass children check, got: %v", err)
	}

	content := readNibFile(t, nibsDir, "frc-1--parent.md")
	if !strings.Contains(content, "status: completed") {
		t.Errorf("expected status completed, got:\n%s", content)
	}
}

func TestCloseUpdatesParentCurrentFocus(t *testing.T) {
	nibsDir := setupCloseTest(t, map[string]string{
		"ms-1--milestone.md": "---\ntitle: Milestone\nstatus: in-progress\ntype: milestone\n---\n\n## Current Focus\n\nWorking on phase 1.\n",
		"ph-1--phase.md":     "---\ntitle: Phase 1\nstatus: in-progress\ntype: epic\nparent: ms-1\n---\n\nPhase 1 body.\n",
	})

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "ph-1", "--summary", "Phase 1 completed successfully.",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	milestone := readNibFile(t, nibsDir, "ms-1--milestone.md")
	if !strings.Contains(milestone, "Phase 1 completed") {
		t.Errorf("expected parent Current Focus to be updated with summary, got:\n%s", milestone)
	}
}

func TestCloseUpdatesParentKeyDecisions(t *testing.T) {
	nibsDir := setupCloseTest(t, map[string]string{
		"ms-2--milestone.md": "---\ntitle: Milestone\nstatus: in-progress\ntype: milestone\n---\n\n## Key Decisions\n\n- Previous decision\n",
		"ph-2--phase.md": "---\ntitle: Phase 2\nstatus: in-progress\ntype: epic\nparent: ms-2\n---\n\n## Key Decisions\n\n- Used GraphQL instead of REST\n- Chose table-driven tests\n",
	})

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "ph-2", "--summary", "Phase 2 done.",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	milestone := readNibFile(t, nibsDir, "ms-2--milestone.md")
	if !strings.Contains(milestone, "Used GraphQL instead of REST") {
		t.Errorf("expected parent Key Decisions to include child's decisions, got:\n%s", milestone)
	}
	// Original decisions should be preserved
	if !strings.Contains(milestone, "Previous decision") {
		t.Errorf("expected parent's original Key Decisions to be preserved, got:\n%s", milestone)
	}
}

func TestCloseNoParent(t *testing.T) {
	nibsDir := setupCloseTest(t, map[string]string{
		"nop-1--solo.md": "---\ntitle: Solo Task\nstatus: in-progress\ntype: task\n---\n\nJust a task.\n",
	})

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "nop-1", "--summary", "Done without parent",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("close should work without parent, got: %v", err)
	}

	content := readNibFile(t, nibsDir, "nop-1--solo.md")
	if !strings.Contains(content, "status: completed") {
		t.Errorf("expected completed status, got:\n%s", content)
	}
}

func TestCloseParentMissingSections(t *testing.T) {
	nibsDir := setupCloseTest(t, map[string]string{
		"ms-3--milestone.md": "---\ntitle: Milestone\nstatus: in-progress\ntype: milestone\n---\n\nJust a goal.\n",
		"ph-3--phase.md": "---\ntitle: Phase 3\nstatus: in-progress\ntype: epic\nparent: ms-3\n---\n\n## Key Decisions\n\n- Chose mdsection for parsing\n",
	})

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "ph-3", "--summary", "Phase 3 completed.",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	milestone := readNibFile(t, nibsDir, "ms-3--milestone.md")
	if !strings.Contains(milestone, "## Current Focus") {
		t.Errorf("expected Current Focus section to be appended, got:\n%s", milestone)
	}
	if !strings.Contains(milestone, "## Key Decisions") {
		t.Errorf("expected Key Decisions section to be appended, got:\n%s", milestone)
	}
	if !strings.Contains(milestone, "Chose mdsection for parsing") {
		t.Errorf("expected child's key decisions to appear in parent, got:\n%s", milestone)
	}
}

func TestCloseAlreadyCompleted(t *testing.T) {
	nibsDir := setupCloseTest(t, map[string]string{
		"done-1--finished.md": "---\ntitle: Finished\nstatus: completed\ntype: task\n---\n\nAlready done.\n",
	})

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "done-1", "--summary", "Closing again",
	})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when closing already completed nib")
	}
	if !strings.Contains(err.Error(), "already") {
		t.Errorf("error should mention already completed, got: %s", err)
	}
}

func TestCloseJSONOutput(t *testing.T) {
	nibsDir := setupCloseTest(t, map[string]string{
		"json-1--task.md": "---\ntitle: JSON Task\nstatus: in-progress\ntype: task\n---\n\nBody.\n",
	})

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "json-1", "--summary", "Done", "--json",
	})

	// JSON output goes via output.Success which writes to stdout.
	// If no error, that's sufficient — the output.Success path was hit.
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("close --json failed: %v", err)
	}

	content := readNibFile(t, nibsDir, "json-1--task.md")
	if !strings.Contains(content, "status: completed") {
		t.Errorf("expected completed status, got:\n%s", content)
	}
}

func TestCloseSummaryRequired(t *testing.T) {
	nibsDir := setupCloseTest(t, map[string]string{
		"req-1--task.md": "---\ntitle: Task\nstatus: todo\ntype: task\n---\n\nBody.\n",
	})

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "req-1",
	})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when --summary is missing")
	}
	if !strings.Contains(err.Error(), "--summary") {
		t.Errorf("error should mention --summary, got: %s", err)
	}
}

func TestCloseSucceedsWithAllChildrenCompleted(t *testing.T) {
	nibsDir := setupCloseTest(t, map[string]string{
		"par-2--parent.md": "---\ntitle: Parent Epic\nstatus: in-progress\ntype: epic\n---\n\nEpic body.\n",
		"ch-3--child-a.md": "---\ntitle: Child A\nstatus: completed\ntype: task\nparent: par-2\n---\n\nDone.\n",
		"ch-4--child-b.md": "---\ntitle: Child B\nstatus: scrapped\ntype: task\nparent: par-2\n---\n\nScrapped.\n",
	})

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "par-2", "--summary", "All children resolved",
	})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("expected close to succeed when all children are resolved, got: %v", err)
	}

	content := readNibFile(t, nibsDir, "par-2--parent.md")
	if !strings.Contains(content, "status: completed") {
		t.Errorf("expected status completed, got:\n%s", content)
	}
	if !strings.Contains(content, "## Summary") {
		t.Errorf("expected summary section appended, got:\n%s", content)
	}
}
