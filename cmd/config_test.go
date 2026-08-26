package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/fsutil"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/testskip"
)

// testNibSpec is a minimal on-disk description of a nib used by
// setupSetPrefixTest. Filename is the full basename (e.g. "tnib-aaa--root.md")
// and id is the nib ID that filename encodes (e.g. "tnib-aaa").
type testNibSpec struct {
	filename  string
	id        string
	parent    string
	milestone string
	blockedBy []string
	blocking  []string
	body      string
}

// setupSetPrefixTest creates a temporary project with a .nibs store — its
// config.yml carrying the given prefix, and one rendered nib file per spec
// under data/. It also registers a t.Cleanup that resets the package-level
// set-prefix flag vars and restores gitIsDirtyFn to realGitIsDirty. By default
// gitIsDirtyFn is overridden to report clean so tests don't accidentally shell
// out to git.
//
// Returns (projectRoot, nibsDir, cfgPath).
func setupSetPrefixTest(t *testing.T, prefix string, nibs ...testNibSpec) (string, string, string) {
	t.Helper()

	// Reset flag state and gitIsDirtyFn on cleanup. Persistent-flag reset
	// (--config/--nibs-path) is delegated to resetRootPersistentFlags so the
	// pflag Value and Changed bits are also cleared, not just the bound
	// package-level vars.
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(func() {
		setPrefixDryRun = false
		setPrefixForce = false
		setPrefixJSON = false
		gitIsDirtyFn = realGitIsDirty
	})
	setPrefixDryRun = false
	setPrefixForce = false
	setPrefixJSON = false
	// Stub git as clean by default so tests don't shell out unexpectedly.
	gitIsDirtyFn = func(string, ...string) (bool, error) { return false, nil }

	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(storeDataDir(nibsDir), 0o755); err != nil {
		t.Fatalf("mkdir nibs: %v", err)
	}

	cfgPath := filepath.Join(nibsDir, "config.yml")
	cfgYAML := "nibs:\n  prefix: " + prefix + "\n  id_length: 4\n"
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o644); err != nil {
		t.Fatalf("write cfg: %v", err)
	}

	for _, spec := range nibs {
		b := &nib.Nib{
			ID:        spec.id,
			Version:   2,
			Title:     spec.id,
			Status:    "todo",
			Type:      "task",
			Body:      spec.body,
			Parent:    spec.parent,
			Milestone: spec.milestone,
			BlockedBy: spec.blockedBy,
			Blocking:  spec.blocking,
		}
		data, err := b.Render()
		if err != nil {
			t.Fatalf("render %s: %v", spec.id, err)
		}
		if err := os.WriteFile(dataPath(nibsDir, spec.filename), data, 0o644); err != nil {
			t.Fatalf("write nib %s: %v", spec.filename, err)
		}
	}

	return tmpDir, nibsDir, cfgPath
}

// runSetPrefixCmd invokes `nibs --config <cfg> config set-prefix <args...>` using
// the package-level rootCmd. Returns the error from rootCmd.Execute().
//
// --config alone names the store: the config lives inside it, so its containing
// directory IS nibsDir here. Passing --nibs-path as well is refused, because a
// store and a config from different projects decouple a store from its own
// vocabulary.
func runSetPrefixCmd(t *testing.T, cfgPath, nibsDir string, args ...string) error {
	t.Helper()
	_ = nibsDir
	full := append([]string{"--config", cfgPath, "config", "set-prefix"}, args...)
	rootCmd.SetArgs(full)
	return rootCmd.Execute()
}

// parseNibFile loads and parses a single nib file by absolute path.
func parseNibFile(t *testing.T, path string) *nib.Nib {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	b, err := nib.Parse(f)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return b
}

// loadCfg reads the config file at cfgPath and returns it.
func loadCfg(t *testing.T, cfgPath string) *config.Config {
	t.Helper()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

func TestSetPrefix_HappyPath_RenamesFilesAndUpdatesReferences(t *testing.T) {
	_, nibsDir, cfgPath := setupSetPrefixTest(t, "tnib-",
		testNibSpec{filename: "tnib-aaa--root.md", id: "tnib-aaa"},
		testNibSpec{filename: "tnib-bbb--child.md", id: "tnib-bbb", parent: "tnib-aaa", body: "child body"},
		testNibSpec{
			filename:  "tnib-ccc--blocked.md",
			id:        "tnib-ccc",
			parent:    "tnib-aaa",
			milestone: "tnib-aaa",
			blockedBy: []string{"tnib-bbb"},
			blocking:  []string{"tnib-bbb"},
		},
	)

	if err := runSetPrefixCmd(t, cfgPath, nibsDir, "new-", "--json"); err != nil {
		t.Fatalf("set-prefix failed: %v", err)
	}

	// Old files must be gone.
	for _, old := range []string{"tnib-aaa--root.md", "tnib-bbb--child.md", "tnib-ccc--blocked.md"} {
		if _, err := os.Stat(dataPath(nibsDir, old)); !os.IsNotExist(err) {
			t.Errorf("expected old file %s to be gone, stat err=%v", old, err)
		}
	}

	// New files must exist.
	for _, newName := range []string{"new-aaa--root.md", "new-bbb--child.md", "new-ccc--blocked.md"} {
		if _, err := os.Stat(dataPath(nibsDir, newName)); err != nil {
			t.Errorf("expected new file %s to exist: %v", newName, err)
		}
	}

	// Child's parent reference must be updated.
	child := parseNibFile(t, dataPath(nibsDir, "new-bbb--child.md"))
	if child.Parent != "new-aaa" {
		t.Errorf("child parent = %q, want %q", child.Parent, "new-aaa")
	}
	if !strings.Contains(child.Body, "child body") {
		t.Errorf("child body lost during rewrite: got %q", child.Body)
	}

	// Every id-valued front-matter field on the blocked nib must be retargeted:
	// one left behind names a nib that no longer exists.
	blocked := parseNibFile(t, dataPath(nibsDir, "new-ccc--blocked.md"))
	if blocked.Parent != "new-aaa" {
		t.Errorf("blocked parent = %q, want %q", blocked.Parent, "new-aaa")
	}
	if blocked.Milestone != "new-aaa" {
		t.Errorf("blocked milestone = %q, want %q", blocked.Milestone, "new-aaa")
	}
	if len(blocked.BlockedBy) != 1 || blocked.BlockedBy[0] != "new-bbb" {
		t.Errorf("blocked blocked_by = %v, want [new-bbb]", blocked.BlockedBy)
	}
	if len(blocked.Blocking) != 1 || blocked.Blocking[0] != "new-bbb" {
		t.Errorf("blocked blocking = %v, want [new-bbb]", blocked.Blocking)
	}

	// Config must be rewritten with the new prefix.
	cfg := loadCfg(t, cfgPath)
	if cfg.Nibs.Prefix != "new-" {
		t.Errorf("cfg prefix = %q, want %q", cfg.Nibs.Prefix, "new-")
	}
}

func TestSetPrefix_InvalidNewPrefix_Rejected(t *testing.T) {
	_, nibsDir, cfgPath := setupSetPrefixTest(t, "tnib-",
		testNibSpec{filename: "tnib-aaa--only.md", id: "tnib-aaa"},
	)

	err := runSetPrefixCmd(t, cfgPath, nibsDir, "BAD_", "--json")
	if err == nil {
		t.Fatal("expected error for invalid prefix BAD_, got nil")
	}
	if !strings.Contains(err.Error(), "BAD_") && !strings.Contains(err.Error(), "invalid prefix") {
		t.Errorf("error message %q does not mention BAD_ or invalid prefix", err.Error())
	}

	// File must be unchanged.
	if _, statErr := os.Stat(dataPath(nibsDir, "tnib-aaa--only.md")); statErr != nil {
		t.Errorf("expected original file to remain, stat err=%v", statErr)
	}

	// Config must be unchanged.
	cfg := loadCfg(t, cfgPath)
	if cfg.Nibs.Prefix != "tnib-" {
		t.Errorf("cfg prefix = %q, want unchanged %q", cfg.Nibs.Prefix, "tnib-")
	}
}

func TestSetPrefix_GitDirtyWithoutForce_Rejected(t *testing.T) {
	_, nibsDir, cfgPath := setupSetPrefixTest(t, "tnib-",
		testNibSpec{filename: "tnib-aaa--only.md", id: "tnib-aaa"},
	)

	gitIsDirtyFn = func(string, ...string) (bool, error) { return true, nil }

	err := runSetPrefixCmd(t, cfgPath, nibsDir, "new-", "--json")
	if err == nil {
		t.Fatal("expected error for dirty git tree without --force, got nil")
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "uncommitted") && !strings.Contains(msg, "dirty") {
		t.Errorf("error message %q should mention 'uncommitted' or 'dirty'", err.Error())
	}

	// File must be unchanged.
	if _, statErr := os.Stat(dataPath(nibsDir, "tnib-aaa--only.md")); statErr != nil {
		t.Errorf("expected original file to remain, stat err=%v", statErr)
	}

	// Config must be unchanged.
	cfg := loadCfg(t, cfgPath)
	if cfg.Nibs.Prefix != "tnib-" {
		t.Errorf("cfg prefix = %q, want unchanged %q", cfg.Nibs.Prefix, "tnib-")
	}
}

func TestSetPrefix_GitDirtyWithForce_Proceeds(t *testing.T) {
	_, nibsDir, cfgPath := setupSetPrefixTest(t, "tnib-",
		testNibSpec{filename: "tnib-aaa--only.md", id: "tnib-aaa"},
	)

	gitIsDirtyFn = func(string, ...string) (bool, error) { return true, nil }

	if err := runSetPrefixCmd(t, cfgPath, nibsDir, "new-", "--force", "--json"); err != nil {
		t.Fatalf("set-prefix --force failed on dirty tree: %v", err)
	}

	// Old file gone.
	if _, err := os.Stat(dataPath(nibsDir, "tnib-aaa--only.md")); !os.IsNotExist(err) {
		t.Errorf("expected old file to be removed, stat err=%v", err)
	}
	// New file exists.
	if _, err := os.Stat(dataPath(nibsDir, "new-aaa--only.md")); err != nil {
		t.Errorf("expected new file to exist: %v", err)
	}

	// Config updated.
	cfg := loadCfg(t, cfgPath)
	if cfg.Nibs.Prefix != "new-" {
		t.Errorf("cfg prefix = %q, want %q", cfg.Nibs.Prefix, "new-")
	}
}

func TestSetPrefix_DryRun_DoesNotMutateOrCallGit(t *testing.T) {
	_, nibsDir, cfgPath := setupSetPrefixTest(t, "tnib-",
		testNibSpec{filename: "tnib-aaa--only.md", id: "tnib-aaa"},
	)

	// Track whether gitIsDirtyFn was called. Dry-run must NOT consult git.
	var gitCalled bool
	gitIsDirtyFn = func(string, ...string) (bool, error) {
		gitCalled = true
		return false, nil
	}

	if err := runSetPrefixCmd(t, cfgPath, nibsDir, "new-", "--dry-run", "--json"); err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}

	if gitCalled {
		t.Error("gitIsDirtyFn was called during --dry-run, should have been skipped")
	}

	// Original file still there.
	if _, err := os.Stat(dataPath(nibsDir, "tnib-aaa--only.md")); err != nil {
		t.Errorf("expected original file to remain after dry-run: %v", err)
	}
	// No renamed file.
	if _, err := os.Stat(dataPath(nibsDir, "new-aaa--only.md")); !os.IsNotExist(err) {
		t.Errorf("expected new file NOT to exist after dry-run, stat err=%v", err)
	}

	// Config unchanged.
	cfg := loadCfg(t, cfgPath)
	if cfg.Nibs.Prefix != "tnib-" {
		t.Errorf("cfg prefix = %q, want unchanged %q", cfg.Nibs.Prefix, "tnib-")
	}
}

// The --dry-run --json envelope is script-facing and nothing else pins its link
// fields, so a change in what buildSnapshot plans from moves the reported shape
// with no test to notice. The plan describes the bytes Execute would write, so a
// link the file spells short is reported short on BOTH sides of the rename: a
// short id carries no prefix to cut, and reporting it expanded would describe a
// rewrite that does not happen.
func TestSetPrefixDryRunJSONReportsTheFilesOwnSpelling(t *testing.T) {
	_, nibsDir, cfgPath := setupSetPrefixTest(t, "tnib-",
		testNibSpec{filename: "tnib-aaa--root.md", id: "tnib-aaa"},
		testNibSpec{filename: "tnib-bbb--short.md", id: "tnib-bbb", parent: "aaa", milestone: "aaa", blockedBy: []string{"aaa"}},
		testNibSpec{filename: "tnib-ccc--full.md", id: "tnib-ccc", parent: "tnib-aaa", milestone: "tnib-aaa", blockedBy: []string{"tnib-aaa"}},
	)

	var runErr error
	out := captureStdout(t, func() {
		runErr = runSetPrefixCmd(t, cfgPath, nibsDir, "new-", "--dry-run", "--json")
	})
	if runErr != nil {
		t.Fatalf("dry-run failed: %v", runErr)
	}

	var env struct {
		Success bool `json:"success"`
		Plan    struct {
			Files []struct {
				OldID        string   `json:"OldID"`
				OldParent    string   `json:"OldParent"`
				NewParent    string   `json:"NewParent"`
				OldMilestone string   `json:"OldMilestone"`
				NewMilestone string   `json:"NewMilestone"`
				OldBlockedBy []string `json:"OldBlockedBy"`
				NewBlockedBy []string `json:"NewBlockedBy"`
			} `json:"Files"`
		} `json:"plan"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("envelope is not JSON: %v\n%s", err, out)
	}
	if !env.Success {
		t.Fatalf("envelope reports success=false: %s", out)
	}

	byID := map[string]int{}
	for i, f := range env.Plan.Files {
		byID[f.OldID] = i
	}
	short, ok := byID["tnib-bbb"]
	if !ok {
		t.Fatalf("plan has no entry for tnib-bbb: %s", out)
	}
	full, ok := byID["tnib-ccc"]
	if !ok {
		t.Fatalf("plan has no entry for tnib-ccc: %s", out)
	}

	sf := env.Plan.Files[short]
	for _, c := range []struct{ field, got, want string }{
		{"OldParent", sf.OldParent, "aaa"},
		{"NewParent", sf.NewParent, "aaa"},
		{"OldMilestone", sf.OldMilestone, "aaa"},
		{"NewMilestone", sf.NewMilestone, "aaa"},
	} {
		if c.got != c.want {
			t.Errorf("short nib %s = %q, want %q — a short id has no prefix to rewrite", c.field, c.got, c.want)
		}
	}
	if len(sf.OldBlockedBy) != 1 || sf.OldBlockedBy[0] != "aaa" || len(sf.NewBlockedBy) != 1 || sf.NewBlockedBy[0] != "aaa" {
		t.Errorf("short nib blocked_by = old %v new %v, want [aaa] both", sf.OldBlockedBy, sf.NewBlockedBy)
	}

	ff := env.Plan.Files[full]
	if ff.OldParent != "tnib-aaa" || ff.NewParent != "new-aaa" {
		t.Errorf("full nib parent = old %q new %q, want %q -> %q", ff.OldParent, ff.NewParent, "tnib-aaa", "new-aaa")
	}
	if ff.OldMilestone != "tnib-aaa" || ff.NewMilestone != "new-aaa" {
		t.Errorf("full nib milestone = old %q new %q, want %q -> %q", ff.OldMilestone, ff.NewMilestone, "tnib-aaa", "new-aaa")
	}
}

func TestSetPrefix_SamePrefix_Rejected(t *testing.T) {
	_, nibsDir, cfgPath := setupSetPrefixTest(t, "tnib-",
		testNibSpec{filename: "tnib-aaa--only.md", id: "tnib-aaa"},
	)

	err := runSetPrefixCmd(t, cfgPath, nibsDir, "tnib-", "--json")
	if err == nil {
		t.Fatal("expected error for same prefix, got nil")
	}
	if !strings.Contains(err.Error(), "same") {
		t.Errorf("error message %q should mention 'same' (prefix)", err.Error())
	}

	// File must be unchanged.
	if _, statErr := os.Stat(dataPath(nibsDir, "tnib-aaa--only.md")); statErr != nil {
		t.Errorf("expected original file to remain, stat err=%v", statErr)
	}

	// Config must be unchanged.
	cfg := loadCfg(t, cfgPath)
	if cfg.Nibs.Prefix != "tnib-" {
		t.Errorf("cfg prefix = %q, want unchanged %q", cfg.Nibs.Prefix, "tnib-")
	}
}

func TestSetPrefix_Collision_Rejected(t *testing.T) {
	_, nibsDir, cfgPath := setupSetPrefixTest(t, "tnib-",
		testNibSpec{filename: "tnib-aaa--only.md", id: "tnib-aaa"},
	)
	// Pre-create a DIRECTORY at the target rename path so os.Stat reports
	// the path exists without core.Load() picking it up as a stray nib
	// (it walks only .md regular files). This drives targetExists to report
	// true and exercises the collision short-circuit in runSetPrefix.
	if err := os.Mkdir(dataPath(nibsDir, "new-aaa--only.md"), 0o755); err != nil {
		t.Fatalf("seed collision dir: %v", err)
	}

	err := runSetPrefixCmd(t, cfgPath, nibsDir, "new-", "--json")
	if err == nil {
		t.Fatal("expected error for target collision, got nil")
	}
	if !strings.Contains(err.Error(), "collide") {
		t.Errorf("error = %q, should mention collision", err.Error())
	}
	// Original file should still exist at its old path.
	if _, err := os.Stat(dataPath(nibsDir, "tnib-aaa--only.md")); err != nil {
		t.Errorf("original file clobbered: %v", err)
	}
	// Config should still have the old prefix.
	cfg := loadCfg(t, cfgPath)
	if cfg.Nibs.Prefix != "tnib-" {
		t.Errorf("config prefix = %q, want tnib-", cfg.Nibs.Prefix)
	}
}

func TestSetPrefix_GitCheckError_Surfaces(t *testing.T) {
	_, nibsDir, cfgPath := setupSetPrefixTest(t, "tnib-",
		testNibSpec{filename: "tnib-aaa--only.md", id: "tnib-aaa"},
	)
	gitIsDirtyFn = func(string, ...string) (bool, error) {
		return false, errors.New("git status exploded")
	}

	err := runSetPrefixCmd(t, cfgPath, nibsDir, "new-", "--json")
	if err == nil {
		t.Fatal("expected error when gitIsDirtyFn fails, got nil")
	}
	if !strings.Contains(err.Error(), "git") {
		t.Errorf("error %q should mention git", err.Error())
	}
	// Original file should still exist at its old path.
	if _, err := os.Stat(dataPath(nibsDir, "tnib-aaa--only.md")); err != nil {
		t.Errorf("original file clobbered: %v", err)
	}
}

// TestSetPrefix_AutoAppendsDash ensures users can type "bgt" or "bgt-"
// interchangeably — `nibs init` silently appends a dash, and set-prefix
// should match that behavior so the two forms produce identical results.
func TestSetPrefix_AutoAppendsDash(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"without dash", "new"},
		{"with dash", "new-"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, nibsDir, cfgPath := setupSetPrefixTest(t, "tnib-",
				testNibSpec{filename: "tnib-aaa--only.md", id: "tnib-aaa"},
			)
			if err := runSetPrefixCmd(t, cfgPath, nibsDir, tc.input, "--json"); err != nil {
				t.Fatalf("set-prefix %q failed: %v", tc.input, err)
			}
			// Same expected file regardless of which input form was used.
			if _, err := os.Stat(dataPath(nibsDir, "new-aaa--only.md")); err != nil {
				t.Errorf("expected new-aaa--only.md to exist: %v", err)
			}
			if _, err := os.Stat(dataPath(nibsDir, "tnib-aaa--only.md")); !os.IsNotExist(err) {
				t.Errorf("expected old file gone, got err=%v", err)
			}
			cfg := loadCfg(t, cfgPath)
			if cfg.Nibs.Prefix != "new-" {
				t.Errorf("cfg prefix = %q, want %q", cfg.Nibs.Prefix, "new-")
			}
		})
	}
}

// TestSetPrefix_UppercaseExplicitRejected locks in the rule that an
// explicit uppercase prefix passed to `config set-prefix` is NEVER silently
// lowercased — it must be rejected by the validator. Mirrors
// TestInit_ExplicitPrefix_UppercaseRejected to keep the init and set-prefix
// code paths aligned.
func TestSetPrefix_UppercaseExplicitRejected(t *testing.T) {
	_, nibsDir, cfgPath := setupSetPrefixTest(t, "tnib-",
		testNibSpec{filename: "tnib-aaa--only.md", id: "tnib-aaa"},
	)

	err := runSetPrefixCmd(t, cfgPath, nibsDir, "BGT-", "--json")
	if err == nil {
		t.Fatal("expected error for uppercase explicit prefix, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "BGT-") {
		t.Errorf("error message %q should contain failing prefix %q", msg, "BGT-")
	}
	if !strings.Contains(msg, "invalid prefix") {
		t.Errorf("error message %q should contain phrase %q", msg, "invalid prefix")
	}

	// Original file must be unchanged (not renamed to BGT-aaa--only.md or
	// any other form).
	if _, statErr := os.Stat(dataPath(nibsDir, "tnib-aaa--only.md")); statErr != nil {
		t.Errorf("expected original file to remain, stat err=%v", statErr)
	}

	// Config must be unchanged.
	cfg := loadCfg(t, cfgPath)
	if cfg.Nibs.Prefix != "tnib-" {
		t.Errorf("cfg prefix = %q, want unchanged %q", cfg.Nibs.Prefix, "tnib-")
	}
}

// TestSetPrefix_GrandfatheredOldPrefix verifies the command accepts a project
// whose existing prefix doesn't match the strict rules applied to new
// prefixes — e.g. a project initialized in a directory named "boardGameTracker"
// ends up with "boardGameTracker-" (17 chars, uppercase), which `nibs init`
// doesn't validate. This command's entire purpose is to let such projects
// escape their old prefix, so it must accept one as input.
func TestSetPrefix_GrandfatheredOldPrefix(t *testing.T) {
	_, nibsDir, cfgPath := setupSetPrefixTest(t, "boardGameTracker-",
		testNibSpec{filename: "boardGameTracker-aaa--only.md", id: "boardGameTracker-aaa"},
	)
	if err := runSetPrefixCmd(t, cfgPath, nibsDir, "bgt", "--json"); err != nil {
		t.Fatalf("set-prefix from grandfathered prefix failed: %v", err)
	}
	if _, err := os.Stat(dataPath(nibsDir, "bgt-aaa--only.md")); err != nil {
		t.Errorf("expected bgt-aaa--only.md to exist: %v", err)
	}
	if _, err := os.Stat(dataPath(nibsDir, "boardGameTracker-aaa--only.md")); !os.IsNotExist(err) {
		t.Errorf("expected old file gone, got err=%v", err)
	}
	cfg := loadCfg(t, cfgPath)
	if cfg.Nibs.Prefix != "bgt-" {
		t.Errorf("cfg prefix = %q, want %q", cfg.Nibs.Prefix, "bgt-")
	}
}

// TestSetPrefix_ReportsReplacingASymlinkedConfig pins the note this command
// prints when the config write replaces a symlink.
//
// config.Save reports that replacement rather than refusing it, because by the
// time it runs every nib file has already been renamed and a refusal would
// leave the store half-changed. The note is therefore the only thing telling a
// user whose config.yml is a link into a dotfile manager that the manager's
// copy still carries the old prefix and will restore it on the next apply.
// internal/config pinned the reporting; nothing pinned the printing, so
// disabling Save's symlink detection left this whole package green.
func TestSetPrefix_ReportsReplacingASymlinkedConfig(t *testing.T) {
	tests := []struct {
		name string
		args []string
		// message extracts the human-readable message from captured stdout,
		// which the two output modes carry differently.
		message func(t *testing.T, out string) string
	}{
		{
			name:    "printed line",
			message: func(_ *testing.T, out string) string { return out },
		},
		{
			// --json routes the same string through output.SuccessMessage, a
			// separate sink — agents read the envelope and never see the line.
			name: "json envelope",
			args: []string{"--json"},
			message: func(t *testing.T, out string) string {
				t.Helper()
				var env struct {
					Success bool   `json:"success"`
					Message string `json:"message"`
				}
				if err := json.Unmarshal([]byte(out), &env); err != nil {
					t.Fatalf("decoding the success envelope %q: %v", out, err)
				}
				if !env.Success {
					t.Fatalf("envelope reports failure: %s", out)
				}
				return env.Message
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir, nibsDir, cfgPath := setupSetPrefixTest(t, "tnib-",
				testNibSpec{filename: "tnib-aaa--only.md", id: "tnib-aaa"},
			)

			// The real config lives outside the store, the way a dotfile
			// manager keeps it, with the store's config.yml linking to it.
			external := filepath.Join(tmpDir, "dotfiles", "nibs-config.yml")
			if err := os.MkdirAll(filepath.Dir(external), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(cfgPath, external); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(external, cfgPath); err != nil {
				testskip.SymlinkUnavailable(t, err)
			}

			args := append([]string{"--config", cfgPath, "config", "set-prefix", "new-"}, tc.args...)
			out, err := runRootWith(t, args...)
			if err != nil {
				t.Fatalf("set-prefix: %v", err)
			}

			msg := tc.message(t, out)
			if !strings.Contains(msg, external) {
				t.Errorf("output does not name the stale file %s, which is the only way the user learns of it:\n%s", external, msg)
			}
			if !strings.Contains(msg, cfgPath) {
				t.Errorf("output does not name the config that was replaced (%s):\n%s", cfgPath, msg)
			}
			if !strings.Contains(msg, "symlink") {
				t.Errorf("output does not say a symlink was replaced, so the note reads as unrelated advice:\n%s", msg)
			}

			// Everything above asserts nothing unless the divergence is real:
			// the link must actually be gone and its target left behind.
			if info, lerr := os.Lstat(cfgPath); lerr != nil || info.Mode()&os.ModeSymlink != 0 {
				t.Fatalf("Lstat(%s) = %v, %v; the link was not replaced, so there was nothing to report", cfgPath, info, lerr)
			}
			stale, err := os.ReadFile(external)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(stale), "tnib-") {
				t.Fatalf("the symlink target was updated after all, so there is nothing to report:\n%s", stale)
			}
			// And the rename really did happen first, which is why Save
			// reports instead of refusing.
			if _, err := os.Stat(dataPath(nibsDir, "new-aaa--only.md")); err != nil {
				t.Errorf("expected new-aaa--only.md to exist: %v", err)
			}
		})
	}
}

// TestSetPrefix_PreservesTheProjectConfig pins what `set-prefix` may write.
//
// It finished by calling cfg.Save("") on the config the app had LOADED — the
// merged read model, with user-level values and system defaults layered onto the
// project's own IN PLACE. Marshaling that struct back over <store>/config.yml
// wrote advisory settings into the project's committed config, and destroyed
// everything the struct does not model.
//
// Rewriting only the one key it changes closes all of it at once, because nothing
// else in the file is touched.
func TestSetPrefix_PreservesTheProjectConfig(t *testing.T) {
	_, nibsDir, cfgPath := setupSetPrefixTest(t, "tnib-",
		testNibSpec{filename: "tnib-aaa--root.md", id: "tnib-aaa"},
	)

	// A project config that declares ONLY a prefix, plus a comment and a key
	// this build does not model — what a newer nibs might have written.
	const original = "# This project's nibs settings. Keep the prefix short.\n" +
		"nibs:\n" +
		"  prefix: \"tnib-\"\n" +
		"  # an experimental key a newer nibs might understand\n" +
		"  future_setting: keep-me\n"
	if err := os.WriteFile(cfgPath, []byte(original), 0o644); err != nil {
		t.Fatalf("writing the project config: %v", err)
	}

	if err := runSetPrefixCmd(t, cfgPath, nibsDir, "new-", "--json"); err != nil {
		t.Fatalf("set-prefix failed: %v", err)
	}

	body, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("reading the config back: %v", err)
	}
	got := string(body)

	if !strings.Contains(got, "prefix: new-") && !strings.Contains(got, `prefix: "new-"`) {
		t.Errorf("the prefix was not updated:\n%s", got)
	}
	// Data loss: an unmodeled key is content a newer nibs wrote, and an older
	// one must not delete it by round-tripping the file through its own struct.
	if !strings.Contains(got, "future_setting: keep-me") {
		t.Errorf("an unmodeled key was destroyed:\n%s", got)
	}
	if !strings.Contains(got, "Keep the prefix short") {
		t.Errorf("the file's comments were destroyed:\n%s", got)
	}
	// Advisory and system-level settings belong to the user's config and to the
	// defaults, not to the project's committed file.
	for _, unwanted := range []string{"id_length", "default_status", "default_type", "hide_completed", "wide_mode"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("set-prefix baked %s into the project config:\n%s", unwanted, got)
		}
	}
}

// TestSetPrefixHoldsTheStoreLockAcrossEverythingItMutates is the concurrency
// half of the fix, and it asks the question at the only two moments that settle
// it: while a nib file is being written, and while config.yml is being renamed
// into place, can anything else take the store's write lock?
//
// The command renames every nib file and then rewrites the whole config from a
// read of its own. Without the lock a concurrent `nibs area rename` — which
// holds this lock from the moment it plans its own config edit until it writes
// it — reads the pre-set-prefix config inside its own window and writes it back
// over this one. Measured with the acquisition below removed: four concurrent
// processes — one set-prefix and three area renames — against one store whose
// config declares 9,000 areas, which is only there to make the whole-file
// read-modify-write cost milliseconds rather than microseconds, lost the prefix
// in 40 of 40 runs. The store was left with every nib file renamed and a config
// still declaring the old prefix. At the fixture's config size the same four
// writers lost nothing in 20 runs, which is why the guard below probes the lock
// directly instead of racing anything.
func TestSetPrefixHoldsTheStoreLockAcrossEverythingItMutates(t *testing.T) {
	_, nibsDir, cfgPath := setupSetPrefixTest(t, "tnib-",
		testNibSpec{filename: "tnib-aaa--root.md", id: "tnib-aaa"},
		testNibSpec{filename: "tnib-bbb--child.md", id: "tnib-bbb", parent: "tnib-aaa"},
	)

	// One probe per moment, keyed by what the seam is renaming into place. Both
	// have to answer "held": the nib writes and the config write are two halves
	// of one change, and a lock dropped between them is no lock at all.
	freeAt := map[string]bool{}
	orig := fsutil.RenameFn
	fsutil.RenameFn = func(oldpath, newpath string) error {
		moment := ""
		switch {
		case strings.HasSuffix(newpath, "config.yml"):
			moment = "the config write"
		case strings.HasSuffix(newpath, ".md"):
			moment = "a nib write"
		}
		if _, seen := freeAt[moment]; moment != "" && !seen {
			freeAt[moment] = storeLockIsFree(t, nibsDir)
		}
		return orig(oldpath, newpath)
	}
	t.Cleanup(func() { fsutil.RenameFn = orig })

	if err := runSetPrefixCmd(t, cfgPath, nibsDir, "new-", "--json"); err != nil {
		t.Fatalf("set-prefix: %v", err)
	}

	for _, moment := range []string{"a nib write", "the config write"} {
		free, probed := freeAt[moment]
		if !probed {
			t.Fatalf("%s never happened, so the probe proves nothing", moment)
		}
		if free {
			t.Errorf("the store's write lock was free during %s — a concurrent config writer can read the pre-edit config here and write it back over this one", moment)
		}
	}
}

// TestSetPrefixRefusesAStoreThatDeclaresNoPrefix pins the gate that keeps the
// config editor's create-a-section paths off this command. set-prefix derives
// its OLD prefix from the loaded config and reprefix.BuildPlan refuses an empty
// one, so a store whose config declares no `nibs.prefix` — however that is
// written — never reaches config.PlanSetStoredPrefix at all. Its doc comment
// says so, and this is the sentence executed.
func TestSetPrefixRefusesAStoreThatDeclaresNoPrefix(t *testing.T) {
	tests := []struct {
		name   string
		config string
	}{
		{"a nibs key with no value under it", "nibs:\nareas:\n    - name: auth\n"},
		{"a nibs key written as an explicit null", "nibs: null\n"},
		{"a config with no nibs section", "areas:\n    - name: auth\n"},
		{"a config that is only comments", "# everything here is commented out\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, nibsDir, cfgPath := setupSetPrefixTest(t, "tnib-",
				testNibSpec{filename: "tnib-aaa--root.md", id: "tnib-aaa"},
			)
			if err := os.WriteFile(cfgPath, []byte(tt.config), 0o644); err != nil {
				t.Fatal(err)
			}

			err := runSetPrefixCmd(t, cfgPath, nibsDir, "zz-", "--json", "--force")
			if err == nil {
				t.Fatal("expected a refusal for a store that declares no prefix")
			}
			if !strings.Contains(err.Error(), "old prefix: must not be empty") {
				t.Errorf("error = %q, want the empty-old-prefix refusal that fires before the config edit is planned", err)
			}
			// The store is untouched, which is what makes the gate a gate.
			after, readErr := os.ReadFile(cfgPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(after) != tt.config {
				t.Errorf("the refused command rewrote config.yml:\n%s", after)
			}
			if _, statErr := os.Stat(dataPath(nibsDir, "tnib-aaa--root.md")); statErr != nil {
				t.Errorf("the refused command renamed a nib file anyway: %v", statErr)
			}
		})
	}
}

// TestSetPrefixRefusesAMultiDocumentConfigBeforeItRenamesAnything is the
// pre-write half of the fix. The re-marshal emits only the first document, so a
// config holding a second one cannot be rewritten without deleting it — and that
// has to be discovered while the store is still untouched, because a rename is
// durable the moment it lands and no rerun undoes it.
func TestSetPrefixRefusesAMultiDocumentConfigBeforeItRenamesAnything(t *testing.T) {
	_, nibsDir, cfgPath := setupSetPrefixTest(t, "tnib-",
		testNibSpec{filename: "tnib-aaa--root.md", id: "tnib-aaa"},
	)
	const twoDocs = `nibs:
  prefix: tnib-
  id_length: 4
---
# a second document some other tool appends
extra: true
`
	if err := os.WriteFile(cfgPath, []byte(twoDocs), 0o644); err != nil {
		t.Fatal(err)
	}

	err := runSetPrefixCmd(t, cfgPath, nibsDir, "new-", "--json")
	if err == nil {
		t.Fatal("expected a refusal for a multi-document config")
	}
	var ce *output.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("error = %T, want *output.CodedError", err)
	}
	if ce.Code != output.ErrValidation {
		t.Errorf("code = %q, want %q — the file's content is the caller's to fix", ce.Code, output.ErrValidation)
	}
	if !strings.Contains(err.Error(), "more than one YAML document") {
		t.Errorf("error = %q, want it to name the shape", err.Error())
	}

	// The two things a late refusal would have cost: the second document, and a
	// store whose files no longer match the prefix its config declares.
	after, readErr := os.ReadFile(cfgPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(after) != twoDocs {
		t.Errorf("the refused edit rewrote config.yml:\n%s", after)
	}
	if _, statErr := os.Stat(dataPath(nibsDir, "tnib-aaa--root.md")); statErr != nil {
		t.Errorf("the refused edit renamed a nib file anyway: %v", statErr)
	}
}

// writeNibFileForTest renders one nib into the store's data/ directory, the way
// another nibs process creating a nib would leave it.
func writeNibFileForTest(t *testing.T, nibsDir, filename, id string) {
	t.Helper()
	b := &nib.Nib{ID: id, Version: 2, Title: id, Status: "todo", Type: "task"}
	data, err := b.Render()
	if err != nil {
		t.Fatalf("render %s: %v", id, err)
	}
	if err := os.WriteFile(dataPath(nibsDir, filename), data, 0o644); err != nil {
		t.Fatalf("write nib %s: %v", id, err)
	}
}

// TestSetPrefixRenamesANibThatAppearedAfterTheSnapshot is the re-derivation
// guard for the rename plan.
//
// The plan used to be built from the store snapshot this process loaded at
// startup, dozens of lines before it took the store lock. A `nibs new` landing
// in that window created a file the plan never saw, and the run left it under
// the OLD prefix while every other file and the config carried the new one — an
// id no vocabulary in the store declares.
//
// gitIsDirtyFn is the injection point because of WHERE it runs: inside the
// locked section, and it is the last thing the command does before it derives
// what it will rename. A file that appears there is the latest a concurrent
// create can possibly be, so a plan that includes it was derived at the last
// read before the first write.
func TestSetPrefixRenamesANibThatAppearedAfterTheSnapshot(t *testing.T) {
	_, nibsDir, cfgPath := setupSetPrefixTest(t, "tnib-",
		testNibSpec{filename: "tnib-aaa--root.md", id: "tnib-aaa"},
	)

	gitIsDirtyFn = func(string, ...string) (bool, error) {
		writeNibFileForTest(t, nibsDir, "tnib-zzz--late.md", "tnib-zzz")
		return false, nil
	}

	if err := runSetPrefixCmd(t, cfgPath, nibsDir, "new-", "--json"); err != nil {
		t.Fatalf("set-prefix failed: %v", err)
	}

	if _, err := os.Stat(dataPath(nibsDir, "new-zzz--late.md")); err != nil {
		t.Errorf("the nib that appeared after the snapshot was not renamed: %v", err)
	}
	if _, err := os.Stat(dataPath(nibsDir, "tnib-zzz--late.md")); !os.IsNotExist(err) {
		t.Errorf("a file under the old prefix survived a completed set-prefix, stat err=%v", err)
	}
	if cfg := loadCfg(t, cfgPath); cfg.Nibs.Prefix != "new-" {
		t.Errorf("cfg prefix = %q, want %q", cfg.Nibs.Prefix, "new-")
	}
}

// TestSetPrefixRefusesAStoreWhoseIdsMovedUnderIt is the other direction of the
// re-derivation, and the one it does NOT paper over: a second `nibs config
// set-prefix` completing inside the window leaves every id under ITS new prefix,
// which the rebuilt plan refuses by name. The refusal lands while the store is
// still untouched, because the re-derivation runs before the first rename.
func TestSetPrefixRefusesAStoreWhoseIdsMovedUnderIt(t *testing.T) {
	_, nibsDir, cfgPath := setupSetPrefixTest(t, "tnib-",
		testNibSpec{filename: "tnib-aaa--root.md", id: "tnib-aaa"},
	)

	gitIsDirtyFn = func(string, ...string) (bool, error) {
		if err := os.Rename(dataPath(nibsDir, "tnib-aaa--root.md"), dataPath(nibsDir, "other-aaa--root.md")); err != nil {
			t.Fatalf("simulating the rename another set-prefix made: %v", err)
		}
		return false, nil
	}

	err := runSetPrefixCmd(t, cfgPath, nibsDir, "new-", "--json")
	if err == nil {
		t.Fatal("expected a refusal over a store whose ids no longer carry the old prefix, got nil")
	}
	if !strings.Contains(err.Error(), "other-aaa") || !strings.Contains(err.Error(), "tnib-") {
		t.Errorf("error = %q, want it to name the id it found and the prefix it expected", err)
	}
	if _, statErr := os.Stat(dataPath(nibsDir, "other-aaa--root.md")); statErr != nil {
		t.Errorf("the refusal renamed a file anyway: %v", statErr)
	}
	if _, statErr := os.Stat(dataPath(nibsDir, "new-aaa--root.md")); !os.IsNotExist(statErr) {
		t.Errorf("the refusal renamed a file anyway, stat err=%v", statErr)
	}
	if cfg := loadCfg(t, cfgPath); cfg.Nibs.Prefix != "tnib-" {
		t.Errorf("cfg prefix = %q, want unchanged %q", cfg.Nibs.Prefix, "tnib-")
	}
}

// reprefixOnDiskForTest stands in for another `nibs config set-prefix`
// completing: every nib file renamed and the config rewritten, with nothing in
// this process told about it.
func reprefixOnDiskForTest(t *testing.T, nibsDir, cfgPath, oldPrefix, newPrefix string) {
	t.Helper()
	entries, err := os.ReadDir(storeDataDir(nibsDir))
	if err != nil {
		t.Fatalf("read data dir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, oldPrefix) {
			continue
		}
		renamed := newPrefix + strings.TrimPrefix(name, oldPrefix)
		if err := os.Rename(dataPath(nibsDir, name), dataPath(nibsDir, renamed)); err != nil {
			t.Fatalf("rename %s: %v", name, err)
		}
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read cfg: %v", err)
	}
	rewritten := strings.Replace(string(raw), "prefix: "+oldPrefix, "prefix: "+newPrefix, 1)
	if rewritten == string(raw) {
		t.Fatalf("the config does not declare prefix %q:\n%s", oldPrefix, raw)
	}
	if err := os.WriteFile(cfgPath, []byte(rewritten), 0o644); err != nil {
		t.Fatalf("write cfg: %v", err)
	}
}

// setPrefixOverAStoreThatMoved runs `config set-prefix` the way the lock window
// leaves it: the command's Core carries the snapshot it loaded before another
// process renamed every file and rewrote the config, and disk carries what that
// process left. runSetPrefix is driven directly because the move has to land
// between the startup load and the command body, which is the one seam the
// Cobra pipeline does not expose — and with --force there is no injection point
// inside the locked section at all, since the git guard it skips is the only
// call the command makes there.
func setPrefixOverAStoreThatMoved(t *testing.T, nibsDir, cfgPath, newPrefix string, force bool) error {
	t.Helper()
	cfg, err := config.LoadFromStore(nibsDir)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	core := nibcore.New(nibsDir, cfg)
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("load store: %v", err)
	}

	reprefixOnDiskForTest(t, nibsDir, cfgPath, "tnib-", "zz-")

	setPrefixForce = force
	cmd := &cobra.Command{}
	cmd.SetContext(withApp(context.Background(), &App{Core: core}))
	return runSetPrefix(cmd, []string{newPrefix})
}

// TestSetPrefixStoreMovedRefusalNamesARerunThatWorks is the remedy half of the
// re-derivation's refusal.
//
// The re-derivation is what makes a second `set-prefix` completing in the lock
// window detectable at all, and the refusal it raises used to surface
// BuildPlan's own wording — "snapshot contains nib …", this command's
// bookkeeping — with nothing said about what happened or what to do, unlike
// every sibling refusal here. The message is now the one thing a printed remedy
// has to be: runnable. This runs it, against the store the refusal left behind.
//
// --force is carried into the rerun because the winner just renamed every file:
// in a git-tracked store the bare form then meets the dirtiness guard and exits
// 2, so the flag is part of what is asserted rather than cosmetic.
func TestSetPrefixStoreMovedRefusalNamesARerunThatWorks(t *testing.T) {
	tests := []struct {
		name      string
		force     bool
		wantRerun string
	}{
		{
			name:      "the rerun is the bare command",
			wantRerun: "nibs config set-prefix new-",
		},
		{
			name:      "a forced run prescribes a forced rerun",
			force:     true,
			wantRerun: "nibs config set-prefix new- --force",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, nibsDir, cfgPath := setupSetPrefixTest(t, "tnib-",
				testNibSpec{filename: "tnib-aaa--root.md", id: "tnib-aaa"},
				testNibSpec{filename: "tnib-bbb--leaf.md", id: "tnib-bbb", parent: "tnib-aaa"},
			)

			err := setPrefixOverAStoreThatMoved(t, nibsDir, cfgPath, "new-", tt.force)
			if err == nil {
				t.Fatal("expected a refusal over a store that moved under the lock, got nil")
			}
			if !strings.Contains(err.Error(), "waited for its write lock") {
				t.Errorf("the refusal does not say what happened: %q", err)
			}

			cmds := diagnosticNibsCommands(err.Error())
			if len(cmds) != 1 {
				t.Fatalf("the refusal names %d `nibs …` commands, want exactly 1: %q", len(cmds), err)
			}
			if got := "nibs " + strings.Join(cmds[0], " "); got != tt.wantRerun {
				t.Errorf("the refusal prescribes %q, want %q", got, tt.wantRerun)
			}

			// A fresh process is what the rerun means: it resolves the store
			// from disk, so it reads the prefix the winner left rather than the
			// one the refused run held.
			if err := runDiagnosticCommand(t, nibsDir, cmds[0]); err != nil {
				t.Fatalf("the rerun this refusal prescribes fails when run: %v", err)
			}

			for _, name := range []string{"new-aaa--root.md", "new-bbb--leaf.md"} {
				if _, statErr := os.Stat(dataPath(nibsDir, name)); statErr != nil {
					t.Errorf("the prescribed rerun left %s unwritten: %v", name, statErr)
				}
			}
			if cfg := loadCfg(t, cfgPath); cfg.Nibs.Prefix != "new-" {
				t.Errorf("after the prescribed rerun the config declares %q, want %q", cfg.Nibs.Prefix, "new-")
			}
		})
	}
}

// TestSetPrefixLeavesAnAlreadyLoadedStoreRefusingRatherThanResurrecting is the
// cross-process half of Core.Update's stale-path guard, against the real command
// that moves the files.
//
// A write verb loads the store, a `nibs config set-prefix` renames every nib
// file underneath it, and only then does the first process write its edit. The
// path it holds names nothing, and a creating write would put the edit in a
// second file under the retired prefix while the live nib kept the old value —
// and exit 0.
func TestSetPrefixLeavesAnAlreadyLoadedStoreRefusingRatherThanResurrecting(t *testing.T) {
	_, nibsDir, cfgPath := setupSetPrefixTest(t, "tnib-",
		testNibSpec{filename: "tnib-aaa--root.md", id: "tnib-aaa"},
	)

	// The store as the other process already holds it: loaded before the rename,
	// and with no watcher to refresh it afterwards.
	core := nibcore.New(nibsDir, loadCfg(t, cfgPath))
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("load core: %v", err)
	}

	if err := runSetPrefixCmd(t, cfgPath, nibsDir, "new-", "--json"); err != nil {
		t.Fatalf("set-prefix failed: %v", err)
	}

	edit, err := core.GetForUpdate("tnib-aaa")
	if err != nil {
		t.Fatalf("GetForUpdate: %v", err)
	}
	edit.Title = "Edited After The Rename"

	err = core.Update(edit, nil)
	if err == nil {
		t.Fatal("the already-loaded store wrote its edit to a renamed-away path instead of refusing")
	}
	if !strings.HasPrefix(err.Error(), "tnib-aaa: ") {
		t.Errorf("error = %q, want it to LEAD with the nib it could not write; the path "+
			"fsutil formats already begins with the id, so mere containment is satisfied "+
			"without Update naming it at all", err)
	}

	// The class the CLI reports for it. The store's files moved under a loaded
	// process, so the repair is to re-read the store — not to fix an argument,
	// which is what the VALIDATION_ERROR fallback would claim.
	code, ok := mutationErrCode(err)
	if !ok || code != output.ErrFileError {
		t.Errorf("mutationErrCode() = %q (classified=%v), want %q", code, ok, output.ErrFileError)
	}
	var coded *output.CodedError
	if !errors.As(setMutationError(false, err), &coded) {
		t.Fatalf("setMutationError() = %v, want *output.CodedError", err)
	}
	if got := output.ExitCode(coded.Code); got != output.ExitIO {
		t.Errorf("exit status = %d for code %q, want %d (io/filesystem)", got, coded.Code, output.ExitIO)
	}

	if _, statErr := os.Stat(dataPath(nibsDir, "tnib-aaa--root.md")); !os.IsNotExist(statErr) {
		t.Errorf("a file under the retired prefix was resurrected, stat err=%v", statErr)
	}
	if got := parseNibFile(t, dataPath(nibsDir, "new-aaa--root.md")).Title; got == "Edited After The Rename" {
		t.Error("the refused edit reached the live file")
	}
}

// TestSetPrefixLeavesAnAlreadyLoadedStoreRefusingToMint is the cross-process
// half of Core.Create's config re-read, against the real command that changes
// the vocabulary.
//
// `nibs new` takes the store's write lock; `nibs config set-prefix` holds it for
// the whole of its rename-plus-config-rewrite. A create that parked behind one
// therefore resumes into a store that already declares the new prefix, and
// neither answer it could give on its own is safe: drawing from the config it
// loaded names the new file under the retired prefix, and drawing from the store
// draws in an id space c.nibs — still keyed by the pre-rename ids — indexes
// nothing of, so the collision guard cannot see the nib a draw would land on.
// It refuses, and a rerun reads the store as the winner left it.
//
// The wait is not simulated; only its outcome is, by running set-prefix to
// completion against a store another Core already holds open.
func TestSetPrefixLeavesAnAlreadyLoadedStoreRefusingToMint(t *testing.T) {
	_, nibsDir, cfgPath := setupSetPrefixTest(t, "tnib-",
		testNibSpec{filename: "tnib-aaa--root.md", id: "tnib-aaa"},
	)

	// The store as the parked process already holds it: loaded before the
	// rename, with no watcher to refresh it afterwards.
	core := nibcore.New(nibsDir, loadCfg(t, cfgPath))
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("load core: %v", err)
	}

	if err := runSetPrefixCmd(t, cfgPath, nibsDir, "new-", "--json"); err != nil {
		t.Fatalf("set-prefix failed: %v", err)
	}

	minted := &nib.Nib{Title: "Minted After The Rename", Slug: "minted-after-the-rename", Status: "todo", Type: "task"}
	err := core.Create(minted)
	if err == nil {
		t.Fatalf("the already-loaded store minted %q at %q instead of refusing", minted.ID, minted.Path)
	}
	var rePrefixed *nibcore.StoreRePrefixedError
	if !errors.As(err, &rePrefixed) {
		t.Fatalf("Create error = %v, want a *StoreRePrefixedError", err)
	}
	if rePrefixed.Loaded != "tnib-" || rePrefixed.Declared != "new-" {
		t.Errorf("refusal names loaded %q / declared %q, want tnib- / new-", rePrefixed.Loaded, rePrefixed.Declared)
	}

	// The class the CLI reports for it — `nibs new` passes this code straight to
	// cmdError. The store's files moved under a loaded process, so the repair is
	// to re-read the store, not to fix an argument.
	code, ok := mutationErrCode(err)
	if !ok || code != output.ErrFileError {
		t.Errorf("mutationErrCode() = %q (classified=%v), want %q", code, ok, output.ErrFileError)
	}
	if got := output.ExitCode(code); got != output.ExitIO {
		t.Errorf("exit status = %d for code %q, want %d (io/filesystem)", got, code, output.ExitIO)
	}

	entries, rerr := os.ReadDir(storeDataDir(nibsDir))
	if rerr != nil {
		t.Fatalf("reading the data directory: %v", rerr)
	}
	if len(entries) != 1 || entries[0].Name() != "new-aaa--root.md" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("data/ holds %v after the refusal, want only the renamed nib", names)
	}
}

// TestSetPrefixNamesALiveServe is the operator's half of the refusal above.
//
// The store's prefix is read ONCE, at startup, by every nibs process — no
// watcher reloads config.yml — so a rename that lands under a live `nibs serve`
// or `nibs tui` leaves it working from a vocabulary no file in the store carries:
// its creates refuse (nibcore.StoreRePrefixedError) and every short id it is
// asked to resolve is prepended the retired prefix. Nothing that process can do
// clears it, and the reader who caused it is looking at this line and no other.
func TestSetPrefixNamesALiveServe(t *testing.T) {
	const note = "restart it"

	t.Run("with no other process holding the store", func(t *testing.T) {
		_, nibsDir, cfgPath := setupSetPrefixTest(t, "tnib-",
			testNibSpec{filename: "tnib-aaa--root.md", id: "tnib-aaa"},
		)
		out := captureStdout(t, func() {
			if err := runSetPrefixCmd(t, cfgPath, nibsDir, "new-"); err != nil {
				t.Fatalf("set-prefix failed: %v", err)
			}
		})
		if strings.Contains(out, note) {
			t.Errorf("output claims a process is holding the store when none is:\n%s", out)
		}
	})

	t.Run("with a serve holding the store", func(t *testing.T) {
		_, nibsDir, cfgPath := setupSetPrefixTest(t, "tnib-",
			testNibSpec{filename: "tnib-aaa--root.md", id: "tnib-aaa"},
		)
		// The shared side is what `nibs serve` holds for its whole lifetime.
		serving, err := nibcore.AcquireServeLock(nibsDir)
		if err != nil {
			t.Fatalf("AcquireServeLock: %v", err)
		}
		defer func() { _ = serving.Release() }()

		out := captureStdout(t, func() {
			if err := runSetPrefixCmd(t, cfgPath, nibsDir, "new-"); err != nil {
				t.Fatalf("set-prefix failed: %v", err)
			}
		})
		if !strings.Contains(out, note) {
			t.Errorf("output does not name the holder whose creates this rename just started refusing:\n%s", out)
		}
	})
}

// TestSetPrefixKeepsAShortLinkSpellingShort pins the rewrite to the FILE's
// spelling of a link rather than to the resolved value the loaded store holds.
//
// A short-form link (`parent: aaa`) needs no rewriting at all: it carries no
// prefix, so it resolves by prefix-prepending under the new prefix exactly as it
// did under the old. Planning from the canonicalized in-memory value instead
// hands the executor a full-form id and rewrites the file to one the author
// never wrote — a silent edit to hand-authored front matter, in a command whose
// whole contract is that it touches the prefix and nothing else.
func TestSetPrefixKeepsAShortLinkSpellingShort(t *testing.T) {
	_, nibsDir, cfgPath := setupSetPrefixTest(t, "tnib-",
		testNibSpec{filename: "tnib-aaa--root.md", id: "tnib-aaa"},
		testNibSpec{
			filename:  "tnib-bbb--short.md",
			id:        "tnib-bbb",
			parent:    "aaa",
			milestone: "aaa",
			blockedBy: []string{"aaa"},
			blocking:  []string{"aaa"},
		},
		testNibSpec{
			filename:  "tnib-ccc--full.md",
			id:        "tnib-ccc",
			parent:    "tnib-aaa",
			milestone: "tnib-aaa",
			blockedBy: []string{"tnib-aaa"},
			blocking:  []string{"tnib-aaa"},
		},
	)

	if err := runSetPrefixCmd(t, cfgPath, nibsDir, "new-", "--json"); err != nil {
		t.Fatalf("set-prefix failed: %v", err)
	}

	short := parseNibFile(t, dataPath(nibsDir, "new-bbb--short.md"))
	if short.Parent != "aaa" {
		t.Errorf("short parent = %q, want %q — a short id carries no prefix to rewrite", short.Parent, "aaa")
	}
	if short.Milestone != "aaa" {
		t.Errorf("short milestone = %q, want %q — a short id carries no prefix to rewrite", short.Milestone, "aaa")
	}
	if len(short.BlockedBy) != 1 || short.BlockedBy[0] != "aaa" {
		t.Errorf("short blocked_by = %v, want [aaa]", short.BlockedBy)
	}
	if len(short.Blocking) != 1 || short.Blocking[0] != "aaa" {
		t.Errorf("short blocking = %v, want [aaa]", short.Blocking)
	}

	// The full-form half of the same store still has to be retargeted, or the
	// assertions above would also pass for a command that rewrote nothing.
	full := parseNibFile(t, dataPath(nibsDir, "new-ccc--full.md"))
	if full.Parent != "new-aaa" {
		t.Errorf("full parent = %q, want %q", full.Parent, "new-aaa")
	}
	if full.Milestone != "new-aaa" {
		t.Errorf("full milestone = %q, want %q", full.Milestone, "new-aaa")
	}
	if len(full.BlockedBy) != 1 || full.BlockedBy[0] != "new-aaa" {
		t.Errorf("full blocked_by = %v, want [new-aaa]", full.BlockedBy)
	}
	if len(full.Blocking) != 1 || full.Blocking[0] != "new-aaa" {
		t.Errorf("full blocking = %v, want [new-aaa]", full.Blocking)
	}

	// The short spellings are left short because they still name the same nib:
	// a store loaded over the renamed files resolves them under the new prefix.
	core := nibcore.New(nibsDir, loadCfg(t, cfgPath))
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("load core over the renamed store: %v", err)
	}
	reloaded, err := core.Get("new-bbb")
	if err != nil {
		t.Fatalf("get new-bbb: %v", err)
	}
	if reloaded.Parent != "new-aaa" {
		t.Errorf("reloaded parent resolves to %q, want %q — the short spelling no longer names the root", reloaded.Parent, "new-aaa")
	}
}
