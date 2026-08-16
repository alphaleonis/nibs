package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/store"
)

// writeLegacyStore materializes a PRE-LAYOUT project: a `.nibs.yml` beside the
// store and nib files sitting directly in the store root. It returns the
// project directory and the store directory.
//
// configBody is written to `.nibs.yml` when non-empty; an empty string leaves
// the project config-less, which is legal (the defaults apply) and is the
// shape a store gets when only its files are legacy.
func writeLegacyStore(t *testing.T, configBody string, files map[string]string) (projectDir, storeDir string) {
	t.Helper()
	projectDir = t.TempDir()
	storeDir = filepath.Join(projectDir, store.DirName)
	if err := os.MkdirAll(storeDir, 0755); err != nil {
		t.Fatalf("mkdir store: %v", err)
	}
	if configBody != "" {
		if err := os.WriteFile(filepath.Join(projectDir, store.LegacyProjectConfigFileName), []byte(configBody), 0644); err != nil {
			t.Fatalf("write legacy config: %v", err)
		}
	}
	for name, content := range files {
		path := filepath.Join(storeDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return projectDir, storeDir
}

const layoutNib = "---\nversion: 1\ntitle: One\nstatus: todo\ntype: task\n---\n\nBody.\n"

// TestLegacyStoreRefusesEveryCommand pins behavior 9: a store still in the
// pre-layout shape — a `.nibs.yml` beside it, nib files at its root, or both —
// makes every command refuse and name `nibs migrate`. The refusal has to work
// on a CONFIG-LESS store: it runs before the config has moved into the store,
// so there is nothing at <store>/config.yml to read yet.
func TestLegacyStoreRefusesEveryCommand(t *testing.T) {
	tests := []struct {
		name       string
		configBody string
		files      map[string]string
	}{
		{
			name:       "config beside the store",
			configBody: "nibs:\n  prefix: leg-\n",
			files:      map[string]string{"data/leg-a1--one.md": layoutNib},
		},
		{
			name:  "nib files at the store root",
			files: map[string]string{"a1--one.md": layoutNib},
		},
		{
			name:       "both",
			configBody: "nibs:\n  prefix: leg-\n",
			files:      map[string]string{"leg-a1--one.md": layoutNib},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(resetRootPersistentFlags)
			t.Cleanup(resetListFlags)
			resetListFlags()
			_, storeDir := writeLegacyStore(t, tt.configBody, tt.files)

			_, err := runRootWith(t, "--nibs-path", storeDir, "list")
			if err == nil {
				t.Fatal("list returned no error on a legacy store; every command must refuse")
			}
			for _, want := range []string{"nibs migrate", "layout"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal = %q, want it to mention %q", err.Error(), want)
				}
			}
		})
	}
}

// TestLegacyStoreCheckStaysExempt pins the one exemption the gate already
// carries: plain `nibs check` is the read-only diagnostic built for exactly
// the store states the refusal creates, so gating it would send the user in a
// circle. --fix writes, so --fix stays gated.
func TestLegacyStoreCheckStaysExempt(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetCheckFlags)
	resetCheckFlags()
	_, storeDir := writeLegacyStore(t, "nibs:\n  prefix: leg-\n", map[string]string{
		"leg-a1--one.md": layoutNib,
	})

	if _, err := runRootWith(t, "--nibs-path", storeDir, "check"); err != nil {
		t.Errorf("plain check refused on a legacy store: %v", err)
	}

	resetRootPersistentFlags()
	resetCheckFlags()
	checkFix = true
	t.Cleanup(func() { checkFix = false })
	if _, err := runRootWith(t, "--nibs-path", storeDir, "check", "--fix"); err == nil {
		t.Error("check --fix ran on a legacy store; only the read-only check is exempt")
	}
}

// TestMigrateLayoutMovesTheWholeStore pins behavior 10: one pass relocates the
// config into the store and every root-level nib file into data/, with
// IDENTICAL basenames — ids derive from filenames, so a migration moves
// directories and never renames files.
func TestMigrateLayoutMovesTheWholeStore(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetMigrateFlags)
	resetMigrateFlags()

	projectDir, storeDir := writeLegacyStore(t, "nibs:\n  prefix: leg-\n  id_length: 4\n", map[string]string{
		"leg-a1--one.md":           layoutNib,
		"leg-b2--two.md":           layoutNib,
		"archive/leg-c3--old.md":   layoutNib,
		"nested/leg-d4--nested.md": layoutNib,
	})

	if _, err := runRootWith(t, "--nibs-path", storeDir, "migrate"); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	l := store.NewLayout(storeDir)
	// The config moved INTO the store, and the legacy file is gone.
	if _, err := os.Stat(l.ConfigPath()); err != nil {
		t.Errorf("config was not relocated into the store: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectDir, store.LegacyProjectConfigFileName)); !os.IsNotExist(err) {
		t.Errorf("the legacy .nibs.yml survived the migration (stat err = %v)", err)
	}

	// Root-level nib files moved into data/ under their original basenames.
	for _, base := range []string{"leg-a1--one.md", "leg-b2--two.md"} {
		if _, err := os.Stat(filepath.Join(l.DataDir(), base)); err != nil {
			t.Errorf("%s is not under data/: %v", base, err)
		}
		if _, err := os.Stat(filepath.Join(storeDir, base)); !os.IsNotExist(err) {
			t.Errorf("%s is still at the store root (stat err = %v)", base, err)
		}
	}
	// archive/ is already in the right place and must not be disturbed.
	if _, err := os.Stat(filepath.Join(l.ArchiveDir(), "leg-c3--old.md")); err != nil {
		t.Errorf("the archived nib moved or vanished: %v", err)
	}
	// A subdirectory of the store root is content too, and moves under data/
	// keeping its relative shape.
	if _, err := os.Stat(filepath.Join(l.DataDir(), "nested", "leg-d4--nested.md")); err != nil {
		t.Errorf("the nested nib is not under data/: %v", err)
	}

	// The migrated store answers queries under its own prefix.
	resetRootPersistentFlags()
	resetListFlags()
	t.Cleanup(resetListFlags)
	out, err := runRootWith(t, "--nibs-path", storeDir, "list", "--all", "--json")
	if err != nil {
		t.Fatalf("list after migrate: %v", err)
	}
	ids := envelopeIDs(parseListEnvelope(t, out))
	for _, want := range []string{"leg-a1", "leg-b2", "leg-c3", "leg-d4"} {
		if !ids[want] {
			t.Errorf("list ids = %v, want %s", ids, want)
		}
	}
}

// TestMigrateLayoutDryRunModifiesNothing pins that the preview counts the move
// without performing it — the store must be byte-for-byte where it was.
func TestMigrateLayoutDryRunModifiesNothing(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetMigrateFlags)
	resetMigrateFlags()

	projectDir, storeDir := writeLegacyStore(t, "nibs:\n  prefix: leg-\n", map[string]string{
		"leg-a1--one.md": layoutNib,
		"leg-b2--two.md": layoutNib,
	})

	out, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--dry-run")
	if err != nil {
		t.Fatalf("migrate --dry-run: %v", err)
	}
	if !strings.Contains(out, "layout") {
		t.Errorf("dry-run output does not mention the layout step:\n%s", out)
	}
	// Two nib files plus the config file.
	if !strings.Contains(out, "3 file(s)") {
		t.Errorf("dry-run should count 2 nib files plus the config, got:\n%s", out)
	}

	for _, base := range []string{"leg-a1--one.md", "leg-b2--two.md"} {
		if _, err := os.Stat(filepath.Join(storeDir, base)); err != nil {
			t.Errorf("dry-run moved %s: %v", base, err)
		}
	}
	if _, err := os.Stat(filepath.Join(projectDir, store.LegacyProjectConfigFileName)); err != nil {
		t.Errorf("dry-run moved the legacy config: %v", err)
	}
}

// TestMigrateLayoutConverges pins idempotency and crash resume: re-running on
// a migrated store is a no-op, and a run that died halfway — files moved, the
// config not yet — finishes the job rather than getting stuck.
func TestMigrateLayoutConverges(t *testing.T) {
	t.Run("re-running a migrated store is a no-op", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		t.Cleanup(resetMigrateFlags)
		resetMigrateFlags()
		_, storeDir := writeLegacyStore(t, "nibs:\n  prefix: leg-\n", map[string]string{
			"leg-a1--one.md": layoutNib,
		})
		if _, err := runRootWith(t, "--nibs-path", storeDir, "migrate"); err != nil {
			t.Fatalf("first migrate: %v", err)
		}

		resetRootPersistentFlags()
		resetMigrateFlags()
		out, err := runRootWith(t, "--nibs-path", storeDir, "migrate")
		if err != nil {
			t.Fatalf("second migrate: %v", err)
		}
		if !strings.Contains(out, "up to date") {
			t.Errorf("re-running migrate should report an up-to-date store, got:\n%s", out)
		}
	})

	t.Run("a half-finished run converges", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		t.Cleanup(resetMigrateFlags)
		resetMigrateFlags()
		projectDir, storeDir := writeLegacyStore(t, "nibs:\n  prefix: leg-\n", map[string]string{
			"leg-a1--one.md": layoutNib,
		})
		// Simulate a crash after the files moved but before the config did.
		l := store.NewLayout(storeDir)
		if err := os.MkdirAll(l.DataDir(), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(filepath.Join(storeDir, "leg-a1--one.md"), filepath.Join(l.DataDir(), "leg-a1--one.md")); err != nil {
			t.Fatal(err)
		}

		if _, err := runRootWith(t, "--nibs-path", storeDir, "migrate"); err != nil {
			t.Fatalf("resuming migrate: %v", err)
		}
		if _, err := os.Stat(l.ConfigPath()); err != nil {
			t.Errorf("the resumed run did not finish relocating the config: %v", err)
		}
		if _, err := os.Stat(filepath.Join(projectDir, store.LegacyProjectConfigFileName)); !os.IsNotExist(err) {
			t.Errorf("the legacy .nibs.yml survived the resumed run (stat err = %v)", err)
		}
	})
}

// TestMigrateLayoutRunsBeforeContentStepsAgainstTheRelocatedConfig pins
// behavior 11 — the ordering constraint that makes a doubly-legacy store
// convert correctly in ONE pass.
//
// The v0 fixture's `blocking:` edge names its target by SHORT id. Resolving it
// needs the project's prefix, and the prefix lives in the config the layout
// step has just MOVED. A migrate run holding a config captured before the move
// would canonicalize under the empty default prefix, fail to resolve the short
// id, and the edge would silently vanish instead of landing on the target's
// blocked_by.
func TestMigrateLayoutRunsBeforeContentStepsAgainstTheRelocatedConfig(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetMigrateFlags)
	resetMigrateFlags()

	_, storeDir := writeLegacyStore(t, "nibs:\n  prefix: zzz-\n  id_length: 4\n", map[string]string{
		"zzz-aaa1--blocker.md": "---\ntitle: Blocker\nstatus: todo\nblocking:\n    - bbb2\n---\n\nBody A.\n",
		"zzz-bbb2--blocked.md": "---\ntitle: Blocked\nstatus: todo\n---\n\nBody B.\n",
	})

	if _, err := runRootWith(t, "--nibs-path", storeDir, "migrate"); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	l := store.NewLayout(storeDir)
	blocked, err := os.ReadFile(filepath.Join(l.DataDir(), "zzz-bbb2--blocked.md"))
	if err != nil {
		t.Fatalf("reading the migrated target: %v", err)
	}
	if !strings.Contains(string(blocked), "zzz-aaa1") {
		t.Errorf("the target did not receive the inverted blocking edge under the project's own prefix:\n%s", blocked)
	}
	blocker, err := os.ReadFile(filepath.Join(l.DataDir(), "zzz-aaa1--blocker.md"))
	if err != nil {
		t.Fatalf("reading the migrated blocker: %v", err)
	}
	if strings.Contains(string(blocker), "blocking:") {
		t.Errorf("the legacy blocking edge survived on the source:\n%s", blocker)
	}
}

// TestMigrateLayoutGuardsTheProjectRepoToo pins the widened dirty-git gate.
// The layout step is the first migration that reaches OUTSIDE the store: it
// deletes `.nibs.yml` from the project repository. Guarding only the store
// repo would leave that deletion with no rollback, so migrate checks the
// project's own git state over the one path it touches there — and
// --allow-dirty still overrides.
func TestMigrateLayoutGuardsTheProjectRepoToo(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetMigrateFlags)
	resetMigrateFlags()

	projectDir, storeDir := writeLegacyStore(t, "nibs:\n  prefix: leg-\n", map[string]string{
		"leg-a1--one.md": layoutNib,
	})
	// The store itself is a committed, clean repo, so only the PROJECT repo's
	// state can make this refuse.
	if out, err := exec.Command("git", "-C", storeDir, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init failed (git unavailable?): %v\n%s", err, out)
	}
	gitCommitAll(t, storeDir)
	if out, err := exec.Command("git", "-C", projectDir, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init failed (git unavailable?): %v\n%s", err, out)
	}
	// .nibs.yml is untracked in the project repo, i.e. unrecoverable if the
	// migration deletes it.

	out, err := runRootWith(t, "--nibs-path", storeDir, "migrate")
	if err == nil {
		t.Fatalf("migrate succeeded with an uncommitted .nibs.yml in the project repo\nout: %s", out)
	}
	if !strings.Contains(err.Error(), "--allow-dirty") {
		t.Errorf("refusal should mention the --allow-dirty override, got: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(projectDir, store.LegacyProjectConfigFileName)); statErr != nil {
		t.Errorf("the refusal deleted the legacy config anyway: %v", statErr)
	}

	resetRootPersistentFlags()
	resetMigrateFlags()
	if _, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty"); err != nil {
		t.Fatalf("migrate --allow-dirty: %v", err)
	}
	if _, statErr := os.Stat(store.NewLayout(storeDir).ConfigPath()); statErr != nil {
		t.Errorf("--allow-dirty run did not relocate the config: %v", statErr)
	}
}

// gitCommitAll stages and commits everything in a repo, with an identity set
// locally so the test does not depend on the machine's git config.
func gitCommitAll(t *testing.T, repo string) {
	t.Helper()
	cmds := [][]string{
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"add", "-A"},
		{"commit", "-q", "-m", "baseline"},
	}
	for _, args := range cmds {
		full := append([]string{"-C", repo}, args...)
		if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
			t.Skipf("git %v failed: %v\n%s", args, err, out)
		}
	}
}

// TestCheckIgnoresTheStoreConfigFile pins behavior 13's quiet half: the store
// now holds a config.yml beside its data, and `nibs check` must never report it
// as an unparseable nib. The walk filters to .md, so config.yml is invisible to
// it — verified here rather than assumed, since a widening of that filter would
// make every store permanently "broken".
func TestCheckIgnoresTheStoreConfigFile(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetCheckFlags)
	resetCheckFlags()

	tmpDir := t.TempDir()
	projectDir := filepath.Join(tmpDir, "proj")
	storeDir := writeStore(t, projectDir, "nibs:\n  prefix: chk-\n", map[string]string{
		"chk-a1--one.md": layoutNib,
	})

	out, err := runRootWith(t, "--nibs-path", storeDir, "check")
	if err != nil {
		t.Fatalf("check on a healthy store: %v", err)
	}
	if strings.Contains(out, store.ConfigFileName) {
		t.Errorf("check reported the store's config file as a problem:\n%s", out)
	}
}
