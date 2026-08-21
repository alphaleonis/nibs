package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/testskip"
)

// testNibSpec is a minimal on-disk description of a nib used by
// setupSetPrefixTest. Filename is the full basename (e.g. "tnib-aaa--root.md")
// and id is the nib ID that filename encodes (e.g. "tnib-aaa").
type testNibSpec struct {
	filename  string
	id        string
	parent    string
	blockedBy []string
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
			BlockedBy: spec.blockedBy,
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
		testNibSpec{filename: "tnib-ccc--blocked.md", id: "tnib-ccc", parent: "tnib-aaa", blockedBy: []string{"tnib-bbb"}},
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

	// Blocked nib's parent + blocked_by must be updated.
	blocked := parseNibFile(t, dataPath(nibsDir, "new-ccc--blocked.md"))
	if blocked.Parent != "new-aaa" {
		t.Errorf("blocked parent = %q, want %q", blocked.Parent, "new-aaa")
	}
	if len(blocked.BlockedBy) != 1 || blocked.BlockedBy[0] != "new-bbb" {
		t.Errorf("blocked blocked_by = %v, want [new-bbb]", blocked.BlockedBy)
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
