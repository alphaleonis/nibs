package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/output"
	"github.com/spf13/pflag"
)

// resetArchiveFlags clears the package-level flag vars used by archiveCmd AND
// Cobra's Changed-state tracking so tests don't pollute each other via
// rootCmd's singleton state.
func resetArchiveFlags() {
	archiveJSON = false
	archiveCmd.Flags().Visit(func(f *pflag.Flag) {
		f.Changed = false
	})
}

// setupArchiveTest writes nib files into a fresh .nibs dir and returns its path.
func setupArchiveTest(t *testing.T, files map[string]string) string {
	t.Helper()
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetArchiveFlags)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	resetArchiveFlags()

	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(storeDataDir(nibsDir), 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(dataPath(nibsDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return nibsDir
}

// TestArchiveRejectsArgAndArchivesNothing is the core guard for nibs-ejbe:
// `nibs archive <id>` must NOT archive anything (the original bug archived every
// eligible nib while silently discarding the id) and must exit 2 with an error
// that names `nibs rm` as the id-taking alternative.
//
// This bites: against the pre-fix archive.go (no Args validator) the command
// ignores the id, archives the completed nib, and returns nil — so both the
// "returns an error" and the "done--task.md not archived" assertions fail.
func TestArchiveRejectsArgAndArchivesNothing(t *testing.T) {
	nibsDir := setupArchiveTest(t, map[string]string{
		"done--task.md": "---\nversion: 1\ntitle: Done\nstatus: completed\ntype: task\n---\n\nBody.\n",
		"open--task.md": "---\nversion: 1\ntitle: Open\nstatus: todo\ntype: task\n---\n\nBody.\n",
	})

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "archive", "done"})
	var execErr error
	captureStdout(t, func() { execErr = rootCmd.Execute() })

	if execErr == nil {
		t.Fatal("expected `archive <id>` to error, not silently archive everything eligible")
	}
	var ce *output.CodedError
	if !errors.As(execErr, &ce) {
		t.Fatalf("error = %T, want *output.CodedError", execErr)
	}
	if output.ExitCode(ce.Code) != output.ExitValidation {
		t.Errorf("exit = %d, want %d (validation)", output.ExitCode(ce.Code), output.ExitValidation)
	}
	if !strings.Contains(ce.Error(), "nibs rm") {
		t.Errorf("error %q should name `nibs rm` as the id-taking alternative", ce.Error())
	}

	// Nothing may have been archived — this is the load-bearing assertion. The
	// original bug moved every completed/scrapped nib despite the id argument.
	if fileExists(filepath.Join(nibsDir, "archive", "done--task.md")) {
		t.Error("`archive <id>` must archive NOTHING; the completed nib was moved to archive/")
	}
	if !fileExists(dataPath(nibsDir, "done--task.md")) {
		t.Error("the completed nib must stay in the main .nibs dir when the command is rejected")
	}
}

// TestArchiveRejectsArgJSON verifies the rejection surfaces as the
// {"error":{"code","message"}} envelope (exit 2) under --json, matching the
// other validation errors, and still archives nothing.
func TestArchiveRejectsArgJSON(t *testing.T) {
	nibsDir := setupArchiveTest(t, map[string]string{
		"done--task.md": "---\nversion: 1\ntitle: Done\nstatus: completed\ntype: task\n---\n\nBody.\n",
	})

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "archive", "done", "--json"})
	var execErr error
	out := captureStdout(t, func() { execErr = rootCmd.Execute() })
	if execErr == nil {
		t.Fatal("expected `archive <id> --json` to error")
	}

	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("error output is not valid JSON: %v\n%s", err, out)
	}
	if env.Error.Code != output.ErrValidation {
		t.Errorf("envelope code = %q, want %q", env.Error.Code, output.ErrValidation)
	}
	if !strings.Contains(env.Error.Message, "nibs rm") {
		t.Errorf("envelope message %q should name `nibs rm`", env.Error.Message)
	}
	// The returned error still carries the code for the exit boundary.
	var ce *output.CodedError
	if !errors.As(execErr, &ce) {
		t.Fatalf("error = %T, want *output.CodedError", execErr)
	}
	if output.ExitCode(ce.Code) != output.ExitValidation {
		t.Errorf("exit = %d, want %d (validation)", output.ExitCode(ce.Code), output.ExitValidation)
	}
	if fileExists(filepath.Join(nibsDir, "archive", "done--task.md")) {
		t.Error("`archive <id> --json` must archive NOTHING")
	}
}

// TestArchiveNoArgsArchivesAllEligible pins the unchanged behavior: bare
// `nibs archive` still archives every completed/scrapped nib and leaves the
// rest alone.
func TestArchiveNoArgsArchivesAllEligible(t *testing.T) {
	nibsDir := setupArchiveTest(t, map[string]string{
		"done--task.md":  "---\nversion: 1\ntitle: Done\nstatus: completed\ntype: task\n---\n\nBody.\n",
		"scrap--task.md": "---\nversion: 1\ntitle: Scrapped\nstatus: scrapped\ntype: task\n---\n\nBody.\n",
		"open--task.md":  "---\nversion: 1\ntitle: Open\nstatus: todo\ntype: task\n---\n\nBody.\n",
	})

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "archive"})
	var execErr error
	captureStdout(t, func() { execErr = rootCmd.Execute() })
	if execErr != nil {
		t.Fatalf("bare `archive` failed: %v", execErr)
	}

	if !fileExists(filepath.Join(nibsDir, "archive", "done--task.md")) {
		t.Error("completed nib should have been archived")
	}
	if !fileExists(filepath.Join(nibsDir, "archive", "scrap--task.md")) {
		t.Error("scrapped nib should have been archived")
	}
	if fileExists(filepath.Join(nibsDir, "archive", "open--task.md")) {
		t.Error("open (todo) nib must not be archived")
	}
	if !fileExists(dataPath(nibsDir, "open--task.md")) {
		t.Error("open (todo) nib must stay in the main .nibs dir")
	}
}
