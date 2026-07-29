package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/output"
)

func resetCloseFlags() {
	closeSummary = ""
	// --as is the one close flag whose zero value is not its default: Cobra
	// leaves the bound variable at whatever the last invocation parsed, so
	// clearing it to "" would make the next bare `close` fail its own --as
	// validation rather than produce the default close reason.
	closeAs = closeDefaultStatus
	closeForce = false
	closeIfMatch = ""
	closeJSON = false
}

func TestResetCloseFlagsClearsAllState(t *testing.T) {
	closeSummary = "dirty"
	closeAs = "dirty"
	closeForce = true
	closeIfMatch = "dirty"
	closeJSON = true

	resetCloseFlags()

	if closeSummary != "" {
		t.Errorf("closeSummary not reset: %q", closeSummary)
	}
	if closeAs != closeDefaultStatus {
		t.Errorf("closeAs = %q after reset, want the flag default %q", closeAs, closeDefaultStatus)
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
	t.Cleanup(resetRootPersistentFlags)
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

	withStdin(t, "All done\n")
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "abc-1", "--summary", "-",
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

	withStdin(t, "Implemented the feature and added tests.\n")
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "sum-1", "--summary", "-",
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
		"par-1--parent.md":    "---\ntitle: Parent Epic\nstatus: in-progress\ntype: epic\n---\n\nEpic body.\n",
		"ch-1--child-done.md": "---\ntitle: Child Done\nstatus: completed\ntype: task\nparent: par-1\n---\n\nDone.\n",
		"ch-2--child-wip.md":  "---\ntitle: Child WIP\nstatus: in-progress\ntype: task\nparent: par-1\n---\n\nStill working.\n",
	})

	withStdin(t, "Done\n")
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "par-1", "--summary", "-",
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

	withStdin(t, "Forced close\n")
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "frc-1", "--summary", "-", "--force",
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

	withStdin(t, "Phase 1 completed successfully.\n")
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "ph-1", "--summary", "-",
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
		"ph-2--phase.md":     "---\ntitle: Phase 2\nstatus: in-progress\ntype: epic\nparent: ms-2\n---\n\n## Key Decisions\n\n- Used GraphQL instead of REST\n- Chose table-driven tests\n",
	})

	withStdin(t, "Phase 2 done.\n")
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "ph-2", "--summary", "-",
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

	withStdin(t, "Done without parent\n")
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "nop-1", "--summary", "-",
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
		"ph-3--phase.md":     "---\ntitle: Phase 3\nstatus: in-progress\ntype: epic\nparent: ms-3\n---\n\n## Key Decisions\n\n- Chose mdsection for parsing\n",
	})

	withStdin(t, "Phase 3 completed.\n")
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "ph-3", "--summary", "-",
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

	withStdin(t, "Done\n")
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "json-1", "--summary", "-", "--json",
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

	withStdin(t, "All children resolved\n")
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "par-2", "--summary", "-",
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

// TestCloseAsSetsTheNamedClosedStatus asserts --as writes the status it names,
// for every status config declares closed. The cases are derived from
// ClosedStatusNames, so a status added to the vocabulary is covered here without
// an edit; the membership check keeps the loop from silently going empty.
func TestCloseAsSetsTheNamedClosedStatus(t *testing.T) {
	closed := config.Default().ClosedStatusNames()
	for _, want := range []string{"scrapped", "deferred"} {
		if !slices.Contains(closed, want) {
			t.Fatalf("test setup: %q is not among the closed statuses %v, so this test no longer covers it", want, closed)
		}
	}

	for _, status := range closed {
		t.Run(status, func(t *testing.T) {
			nibsDir := setupCloseTest(t, map[string]string{
				"as-1--my-task.md": "---\ntitle: My Task\nstatus: in-progress\ntype: task\n---\n\nBody.\n",
			})

			withStdin(t, "Closing as "+status+".\n")
			rootCmd.SetArgs([]string{
				"--nibs-path", nibsDir,
				"close", "as-1", "--as", status, "--summary", "-",
			})

			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("close --as %s failed: %v", status, err)
			}

			content := readNibFile(t, nibsDir, "as-1--my-task.md")
			if !strings.Contains(content, "status: "+status) {
				t.Errorf("expected status %s, got:\n%s", status, content)
			}
			// The summary is the record the close reason exists to carry, so a
			// reason written without one would defeat the point of the flag.
			if !strings.Contains(content, "Closing as "+status+".") {
				t.Errorf("expected the summary in the body, got:\n%s", content)
			}
		})
	}
}

// TestCloseRejectsAnOpenStatusAs asserts --as refuses every open status, naming
// the closed ones it would have accepted. Without this, `--as todo` would write
// an open status through the closing ritual — the exact move the `set` refusal
// exists to prevent, arriving by the other door.
func TestCloseRejectsAnOpenStatusAs(t *testing.T) {
	cfg := config.Default()
	open := cfg.OpenStatusNames()
	if len(open) == 0 {
		t.Fatal("test setup: no open statuses declared, so this test asserts nothing")
	}

	for _, status := range open {
		t.Run(status, func(t *testing.T) {
			nibsDir := setupCloseTest(t, map[string]string{
				"bad-1--my-task.md": "---\ntitle: My Task\nstatus: in-progress\ntype: task\n---\n\nBody.\n",
			})

			// Supply the summary so --as is the only thing wrong with this
			// invocation. Without it the command fails on the missing summary
			// whatever --as holds, and the test would pass with no --as check at
			// all.
			withStdin(t, "Should never be written.\n")
			rootCmd.SetArgs([]string{
				"--nibs-path", nibsDir,
				"close", "bad-1", "--as", status, "--summary", "-",
			})

			err := rootCmd.Execute()
			if err == nil {
				t.Fatalf("close --as %s should be rejected, got nil", status)
			}
			var ce *output.CodedError
			if !errors.As(err, &ce) {
				t.Fatalf("error = %T, want *output.CodedError", err)
			}
			if output.ExitCode(ce.Code) != output.ExitValidation {
				t.Errorf("close --as %s exit = %d, want %d (validation)", status, output.ExitCode(ce.Code), output.ExitValidation)
			}
			// The message has to name the choices, or an agent's only recovery is
			// to guess a second time.
			for _, name := range cfg.ClosedStatusNames() {
				if !strings.Contains(err.Error(), name) {
					t.Errorf("close --as %s error should name the valid choice %q, got: %s", status, name, err)
				}
			}

			content := readNibFile(t, nibsDir, "bad-1--my-task.md")
			if !strings.Contains(content, "status: in-progress") {
				t.Errorf("rejected close --as %s still wrote the file:\n%s", status, content)
			}
		})
	}
}

// TestCloseAsFollowsTheClosedFlag proves --as reads StatusConfig.Closed rather
// than a list of names kept in close.go: a status declared closed for the
// duration of this test is accepted with no edit to the command. The paired
// half — an open status being rejected — is TestCloseRejectsAnOpenStatusAs,
// which together with this rules out "accepts anything in the vocabulary".
func TestCloseAsFollowsTheClosedFlag(t *testing.T) {
	withExtraStatus(t, config.StatusConfig{
		Name:        "abandoned",
		Color:       "gray",
		Closed:      true,
		Description: "Guard status: closed, declared only for this test",
	})

	nibsDir := setupCloseTest(t, map[string]string{
		"drv-1--my-task.md": "---\ntitle: My Task\nstatus: in-progress\ntype: task\n---\n\nBody.\n",
	})

	withStdin(t, "Walked away from it.\n")
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "drv-1", "--as", "abandoned", "--summary", "-",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("close --as abandoned should succeed once abandoned is declared closed, got: %v", err)
	}

	content := readNibFile(t, nibsDir, "drv-1--my-task.md")
	if !strings.Contains(content, "status: abandoned") {
		t.Errorf("expected status abandoned, got:\n%s", content)
	}
}
