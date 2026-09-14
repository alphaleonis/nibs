package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/reprefix"
)

// resumeFixture is a store with a link in every direction a rewrite has to
// retarget, so a rerun that skipped any half-done file leaves a finding
// `nibs check` reports.
func resumeFixture(t *testing.T) (nibsDir, cfgPath string) {
	t.Helper()
	_, nibsDir, cfgPath = setupSetPrefixTest(t, "tnib-",
		testNibSpec{filename: "tnib-aaa--root.md", id: "tnib-aaa", body: "See #tnib-ccc."},
		testNibSpec{filename: "tnib-bbb--child.md", id: "tnib-bbb", blockedBy: []string{"tnib-aaa"}},
		testNibSpec{filename: "tnib-ccc--blocked.md", id: "tnib-ccc", blockedBy: []string{"tnib-aaa", "tnib-bbb"}},
	)
	return nibsDir, cfgPath
}

var resumeFixtureFiles = []string{"aaa--root.md", "bbb--child.md", "ccc--blocked.md"}

// renameForTest renames one fixture file from the old prefix to the new one,
// the way Execute's rename pass leaves it before its rewrite pass runs.
func renameForTest(t *testing.T, nibsDir, rest string) {
	t.Helper()
	if err := os.Rename(dataPath(nibsDir, "tnib-"+rest), dataPath(nibsDir, "new-"+rest)); err != nil {
		t.Fatalf("rename %s: %v", rest, err)
	}
}

// rerunSetPrefixForced runs `set-prefix new- --force` as a fresh invocation.
func rerunSetPrefixForced(t *testing.T, nibsDir, cfgPath string) error {
	t.Helper()
	setPrefixDryRun, setPrefixForce, setPrefixJSON = false, false, false
	reprefixExecuteFn = reprefix.Execute
	return runSetPrefixCmd(t, cfgPath, nibsDir, "new-", "--force", "--json")
}

// assertConvergedOnNewPrefix checks the store a completed set-prefix leaves:
// every file and the config on the new prefix, and nothing for check to report.
func assertConvergedOnNewPrefix(t *testing.T, nibsDir, cfgPath string) {
	t.Helper()
	for _, rest := range resumeFixtureFiles {
		if _, err := os.Stat(dataPath(nibsDir, "new-"+rest)); err != nil {
			t.Errorf("new-%s missing: %v", rest, err)
		}
		if _, err := os.Stat(dataPath(nibsDir, "tnib-"+rest)); !os.IsNotExist(err) {
			t.Errorf("tnib-%s still on disk, stat err=%v", rest, err)
		}
	}
	for _, rest := range resumeFixtureFiles {
		// Parse derives no id, so the rendered id line is read from the bytes.
		raw, err := os.ReadFile(dataPath(nibsDir, "new-"+rest))
		if err != nil {
			t.Fatal(err)
		}
		if id := "new-" + strings.SplitN(rest, "--", 2)[0]; !strings.Contains(string(raw), "# "+id+"\n") {
			t.Errorf("new-%s does not render id %q:\n%s", rest, id, raw)
		}
		b := parseNibFile(t, dataPath(nibsDir, "new-"+rest))
		for _, ref := range b.BlockedBy {
			if !strings.HasPrefix(ref, "new-") {
				t.Errorf("new-%s blocked_by = %v, want every id on the new prefix", rest, b.BlockedBy)
			}
		}
		if strings.Contains(b.Body, "#tnib-") {
			t.Errorf("new-%s body still mentions the old prefix: %q", rest, b.Body)
		}
	}
	if cfg := loadCfg(t, cfgPath); cfg.Nibs.Prefix != "new-" {
		t.Errorf("cfg prefix = %q, want %q", cfg.Nibs.Prefix, "new-")
	}

	t.Cleanup(resetCheckFlags)
	resetCheckFlags()
	var warnings bytes.Buffer
	core := remedyCore(t, nibsDir, &warnings)
	var issues int
	var err error
	out := captureStdout(t, func() { issues, err = runCheck(&App{Core: core}) })
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if issues != 0 {
		t.Errorf("check reports %d issue(s) after the rerun:\n%s", issues, out)
	}
	if warnings.Len() != 0 {
		t.Errorf("loading the converged store warned:\n%s", warnings.String())
	}
}

func TestSetPrefix_RerunAfterRenameFailure(t *testing.T) {
	nibsDir, cfgPath := resumeFixture(t)
	reprefixExecuteFn = func(*reprefix.RenamePlan, string) error {
		renameForTest(t, nibsDir, resumeFixtureFiles[0])
		return errors.New("injected rename failure")
	}

	if err := runSetPrefixCmd(t, cfgPath, nibsDir, "new-", "--json"); err == nil {
		t.Fatal("the injected rename failure was not reported")
	}
	if err := rerunSetPrefixForced(t, nibsDir, cfgPath); err != nil {
		t.Fatalf("the rerun after a rename-pass failure failed: %v", err)
	}
	assertConvergedOnNewPrefix(t, nibsDir, cfgPath)
}

func TestSetPrefix_RerunAfterRewriteFailure(t *testing.T) {
	nibsDir, cfgPath := resumeFixture(t)
	reprefixExecuteFn = func(plan *reprefix.RenamePlan, root string) error {
		for _, rest := range resumeFixtureFiles {
			renameForTest(t, nibsDir, rest)
		}
		// The first file is fully rewritten, the rest only renamed: a rewrite
		// pass that failed on its second file.
		done := *plan
		done.Files = []reprefix.FilePlan{plan.Files[0]}
		done.Files[0].OldPath = done.Files[0].NewPath
		if err := reprefix.Execute(&done, root); err != nil {
			t.Fatalf("rewriting the first file: %v", err)
		}
		return errors.New("injected rewrite failure")
	}

	if err := runSetPrefixCmd(t, cfgPath, nibsDir, "new-", "--json"); err == nil {
		t.Fatal("the injected rewrite failure was not reported")
	}
	if err := rerunSetPrefixForced(t, nibsDir, cfgPath); err != nil {
		t.Fatalf("the rerun after a rewrite-pass failure failed: %v", err)
	}
	assertConvergedOnNewPrefix(t, nibsDir, cfgPath)
}

func TestSetPrefix_RerunAfterConfigWriteFailure(t *testing.T) {
	nibsDir, cfgPath := resumeFixture(t)
	original, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	reprefixExecuteFn = func(plan *reprefix.RenamePlan, root string) error {
		if err := reprefix.Execute(plan, root); err != nil {
			return err
		}
		// A directory where the config file was: the atomic replace cannot
		// rename over it.
		if err := os.Remove(cfgPath); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(cfgPath, "blocker"), 0o755); err != nil {
			t.Fatal(err)
		}
		return nil
	}

	err = runSetPrefixCmd(t, cfgPath, nibsDir, "new-", "--json")
	if err == nil {
		t.Fatal("the config write over a directory did not fail")
	}
	if !strings.Contains(err.Error(), "nibs config set-prefix new- --force") {
		t.Errorf("the config-write failure does not prescribe the rerun: %q", err)
	}
	if strings.Contains(err.Error(), "manually") {
		t.Errorf("the config-write failure still prescribes a manual edit: %q", err)
	}

	// The failed write left the config as it was.
	if err := os.RemoveAll(cfgPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, original, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := rerunSetPrefixForced(t, nibsDir, cfgPath); err != nil {
		t.Fatalf("the rerun after a config-write failure failed: %v", err)
	}
	assertConvergedOnNewPrefix(t, nibsDir, cfgPath)
}

// TestSetPrefix_OverlappingResumeNamesTheUndo pins that a resume BuildPlan
// refuses prescribes the undo and no rerun, since the rerun is the refused run.
func TestSetPrefix_OverlappingResumeNamesTheUndo(t *testing.T) {
	_, nibsDir, cfgPath := setupSetPrefixTest(t, "a-",
		testNibSpec{filename: "a-b-xyz--done.md", id: "a-b-xyz"},
		testNibSpec{filename: "a-qrs--todo.md", id: "a-qrs"},
	)
	storeGitStateFn = func(string) (bool, bool, error) { return true, true, nil }

	err := runSetPrefixCmd(t, cfgPath, nibsDir, "a-b-", "--force", "--json")
	if err == nil {
		t.Fatal("an overlapping-prefix resume was not refused")
	}
	msg := err.Error()
	if cmds := diagnosticNibsCommands(msg); len(cmds) != 0 {
		t.Errorf("the refusal prescribes %v, want no nibs command: %q", cmds, msg)
	}
	if !strings.Contains(msg, "restore --source=HEAD --staged --worktree -- .") || !strings.Contains(msg, "clean -fd -- .") {
		t.Errorf("the refusal does not name the undo: %q", msg)
	}
	if _, statErr := os.Stat(dataPath(nibsDir, "a-qrs--todo.md")); statErr != nil {
		t.Errorf("the refusal renamed a file: %v", statErr)
	}
}

// TestSetPrefix_FailureNamesRecovery pins the two remedies an Execute failure
// prints: a rerun that the dirtiness guard will not refuse, and in a git-tracked
// store an undo scoped to the store, so neither touches the rest of the project.
func TestSetPrefix_FailureNamesRecovery(t *testing.T) {
	tests := []struct {
		name     string
		isRepo   bool
		wantUndo bool
	}{
		{name: "a git-tracked store names the undo", isRepo: true, wantUndo: true},
		{name: "a store outside git names only the rerun"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsDir, cfgPath := resumeFixture(t)
			storeGitStateFn = func(string) (bool, bool, error) { return true, tt.isRepo, nil }
			reprefixExecuteFn = func(*reprefix.RenamePlan, string) error {
				return errors.New("injected rename failure")
			}

			err := runSetPrefixCmd(t, cfgPath, nibsDir, "new-", "--json")
			if err == nil {
				t.Fatal("the injected failure was not reported")
			}
			msg := err.Error()
			if !strings.Contains(msg, "injected rename failure") {
				t.Errorf("the error lost its cause: %q", msg)
			}
			cmds := diagnosticNibsCommands(msg)
			if len(cmds) != 1 || "nibs "+strings.Join(cmds[0], " ") != "nibs config set-prefix new- --force" {
				t.Errorf("the error prescribes %v, want exactly `nibs config set-prefix new- --force`: %q", cmds, msg)
			}
			hasUndo := strings.Contains(msg, "restore --source=HEAD --staged --worktree -- .") && strings.Contains(msg, "clean -fd -- .")
			if hasUndo != tt.wantUndo {
				t.Errorf("undo present = %v, want %v: %q", hasUndo, tt.wantUndo, msg)
			}
			if tt.wantUndo && !strings.Contains(msg, nibsDir) {
				t.Errorf("the undo is not scoped to the store %s: %q", nibsDir, msg)
			}
		})
	}
}
