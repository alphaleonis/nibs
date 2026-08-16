package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/alphaleonis/nibs/internal/output"
	"github.com/spf13/pflag"
)

// resetRmFlags clears the package-level flag vars used by rmCmd AND Cobra's
// Changed-state tracking so tests don't pollute each other via rootCmd's
// singleton state.
func resetRmFlags() {
	rmArchive = false
	rmDelete = false
	rmForce = false
	rmJSON = false
	rmCmd.Flags().Visit(func(f *pflag.Flag) {
		f.Changed = false
	})
}

// setupRmTest writes nib files into a fresh .nibs dir and returns its path.
func setupRmTest(t *testing.T, files map[string]string) string {
	t.Helper()
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetRmFlags)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	resetRmFlags()

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

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// TestRmArchiveDefault verifies `rm <id> -f` archives the nib (moves it into
// .nibs/archive/) by default.
func TestRmArchiveDefault(t *testing.T) {
	nibsDir := setupRmTest(t, map[string]string{
		"arc-1--task.md": "---\nversion: 1\ntitle: Task\nstatus: todo\ntype: task\n---\n\nBody.\n",
	})

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "rm", "arc-1", "-f"})
	var execErr error
	captureStdout(t, func() { execErr = rootCmd.Execute() })
	if execErr != nil {
		t.Fatalf("rm -f (archive) failed: %v", execErr)
	}

	if fileExists(filepath.Join(nibsDir, "arc-1--task.md")) {
		t.Error("nib should have been moved out of the main .nibs dir")
	}
	if !fileExists(filepath.Join(nibsDir, "archive", "arc-1--task.md")) {
		t.Error("nib should have been moved into .nibs/archive/")
	}
}

// TestRmDeleteForce verifies `rm <id> --delete -f` hard-deletes the nib.
func TestRmDeleteForce(t *testing.T) {
	nibsDir := setupRmTest(t, map[string]string{
		"del-1--task.md": "---\nversion: 1\ntitle: Task\nstatus: todo\ntype: task\n---\n\nBody.\n",
	})

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "rm", "del-1", "--delete", "-f"})
	var execErr error
	captureStdout(t, func() { execErr = rootCmd.Execute() })
	if execErr != nil {
		t.Fatalf("rm --delete -f failed: %v", execErr)
	}

	if fileExists(filepath.Join(nibsDir, "del-1--task.md")) {
		t.Error("nib file should have been deleted")
	}
	if fileExists(filepath.Join(nibsDir, "archive", "del-1--task.md")) {
		t.Error("hard delete must not archive the nib")
	}
}

// TestRmRefusesWithoutForceNonInteractive is the contract: a non-interactive
// caller (no TTY) that omits -f gets a clear VALIDATION error — the trap is
// silently canceling and exiting 0. withStdin forces a
// non-terminal stdin so the test is deterministic regardless of how it is run.
func TestRmRefusesWithoutForceNonInteractive(t *testing.T) {
	nibsDir := setupRmTest(t, map[string]string{
		"keep-1--task.md": "---\nversion: 1\ntitle: Task\nstatus: todo\ntype: task\n---\n\nBody.\n",
	})

	withStdin(t, "")
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "rm", "keep-1", "--delete"})
	var execErr error
	captureStdout(t, func() { execErr = rootCmd.Execute() })
	if execErr == nil {
		t.Fatal("expected a VALIDATION error, not a silent success, when rm has no -f and no TTY")
	}
	var ce *output.CodedError
	if !errors.As(execErr, &ce) {
		t.Fatalf("expected *output.CodedError, got %T: %v", execErr, execErr)
	}
	if ce.Code != output.ErrValidation {
		t.Errorf("code = %q, want %q", ce.Code, output.ErrValidation)
	}
	// And the nib must still be present (nothing was removed).
	if !fileExists(filepath.Join(nibsDir, "keep-1--task.md")) {
		t.Error("nib must be untouched when rm refuses")
	}
}

// TestRmArchiveJSONImpliesForce verifies --json implies force (no prompt) and
// emits the {nib} success contract.
func TestRmArchiveJSONImpliesForce(t *testing.T) {
	nibsDir := setupRmTest(t, map[string]string{
		"js-1--task.md": "---\nversion: 1\ntitle: Task\nstatus: todo\ntype: task\n---\n\nBody.\n",
	})

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "rm", "js-1", "--json"})
	var execErr error
	out := captureStdout(t, func() { execErr = rootCmd.Execute() })
	if execErr != nil {
		t.Fatalf("rm --json (archive) failed: %v", execErr)
	}
	var env struct {
		Success bool `json:"success"`
		Nib     struct {
			ID string `json:"id"`
		} `json:"nib"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("unmarshal rm --json: %v\nraw: %s", err, out)
	}
	if !env.Success || env.Nib.ID != "js-1" {
		t.Errorf("unexpected rm --json envelope: %s", out)
	}
	if !fileExists(filepath.Join(nibsDir, "archive", "js-1--task.md")) {
		t.Error("nib should have been archived")
	}
}

// TestRmNotFound verifies a missing id is a NOT_FOUND error.
func TestRmNotFound(t *testing.T) {
	nibsDir := setupRmTest(t, map[string]string{
		"present--task.md": "---\nversion: 1\ntitle: Task\nstatus: todo\ntype: task\n---\n\nBody.\n",
	})

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "rm", "nope", "-f"})
	var execErr error
	captureStdout(t, func() { execErr = rootCmd.Execute() })
	if execErr == nil {
		t.Fatal("expected NOT_FOUND error for a missing id")
	}
	var ce *output.CodedError
	if !errors.As(execErr, &ce) {
		t.Fatalf("expected *output.CodedError, got %T: %v", execErr, execErr)
	}
	if ce.Code != output.ErrNotFound {
		t.Errorf("code = %q, want %q", ce.Code, output.ErrNotFound)
	}
}

// TestRmArchiveDeleteMutex verifies --archive and --delete cannot be combined.
func TestRmArchiveDeleteMutex(t *testing.T) {
	nibsDir := setupRmTest(t, map[string]string{
		"mx-1--task.md": "---\nversion: 1\ntitle: Task\nstatus: todo\ntype: task\n---\n\nBody.\n",
	})

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "rm", "mx-1", "--archive", "--delete", "-f"})
	var execErr error
	captureStdout(t, func() { execErr = rootCmd.Execute() })
	if execErr == nil {
		t.Fatal("expected mutex error when --archive and --delete are both set")
	}
}

// TestRmArchiveAliasStillWorks verifies the legacy `archive` command still runs
// (it archives all completed/scrapped nibs).
func TestRmArchiveAliasStillWorks(t *testing.T) {
	nibsDir := setupRmTest(t, map[string]string{
		"done--task.md": "---\nversion: 1\ntitle: Done\nstatus: completed\ntype: task\n---\n\nBody.\n",
		"open--task.md": "---\nversion: 1\ntitle: Open\nstatus: todo\ntype: task\n---\n\nBody.\n",
	})

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "archive"})
	var execErr error
	captureStdout(t, func() { execErr = rootCmd.Execute() })
	if execErr != nil {
		t.Fatalf("legacy archive command failed: %v", execErr)
	}
	if !fileExists(filepath.Join(nibsDir, "archive", "done--task.md")) {
		t.Error("completed nib should have been archived by the legacy command")
	}
	if fileExists(filepath.Join(nibsDir, "archive", "open--task.md")) {
		t.Error("open nib must not be archived")
	}
}

// TestRmDeleteLegacyAliasStillWorks verifies the legacy `delete` command still
// hard-deletes.
func TestRmDeleteLegacyAliasStillWorks(t *testing.T) {
	nibsDir := setupRmTest(t, map[string]string{
		"leg-1--task.md": "---\nversion: 1\ntitle: Task\nstatus: todo\ntype: task\n---\n\nBody.\n",
	})

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "delete", "leg-1", "-f"})
	var execErr error
	captureStdout(t, func() { execErr = rootCmd.Execute() })
	if execErr != nil {
		t.Fatalf("legacy delete command failed: %v", execErr)
	}
	if fileExists(filepath.Join(nibsDir, "leg-1--task.md")) {
		t.Error("nib should have been deleted by the legacy command")
	}
}
