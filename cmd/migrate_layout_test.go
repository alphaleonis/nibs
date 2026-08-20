package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/store"
	"github.com/alphaleonis/nibs/internal/testskip"
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
	return writeLegacyStoreNamed(t, store.DirName, configBody, files)
}

// writeLegacyStoreNamed is writeLegacyStore with the store directory's NAME
// spelled out, so the tests for the retired `nibs.path` key can put a pre-layout
// store somewhere other than `.nibs`.
func writeLegacyStoreNamed(t *testing.T, storeName, configBody string, files map[string]string) (projectDir, storeDir string) {
	t.Helper()
	projectDir = t.TempDir()
	storeDir = filepath.Join(projectDir, storeName)
	mkdirAllT(t, storeDir)
	if configBody != "" {
		writeFileT(t, filepath.Join(projectDir, store.LegacyProjectConfigFileName), configBody)
	}
	for name, content := range files {
		path := filepath.Join(storeDir, name)
		mkdirAllT(t, filepath.Dir(path))
		writeFileT(t, path, content)
	}
	return projectDir, storeDir
}

// layoutNib is a file as nib.Render writes one: the id comment on the first line
// inside the fence, then the three keys renderFrontMatter never omits. Store
// corroboration keys on exactly that shape, so a fixture standing in for a nib
// has to carry it.
const layoutNib = "---\n# leg-a1\nversion: 1\ntitle: One\nstatus: todo\ntype: task\n---\n\nBody.\n"

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
//
// The exemption is only worth having if check REPORTS those states. Core.Load
// walks data/ and archive/, so on a pre-layout store — where every nib sits at
// the store root — it loads nothing, and a report that speaks only for what it
// loaded certifies the store as healthy while every other command refuses it.
// So the content of the report is pinned here too, not just the absence of a
// refusal.
func TestLegacyStoreCheckStaysExempt(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetCheckFlags)
	resetCheckFlags()
	_, storeDir := writeLegacyStore(t, "nibs:\n  prefix: leg-\n", map[string]string{
		"leg-a1--one.md": layoutNib,
	})

	app := checkAppPastTheGate(t, storeDir)
	var total int
	var runErr error
	out := captureStdout(t, func() { total, runErr = runCheck(app) })
	if runErr != nil {
		t.Fatalf("runCheck error = %v", runErr)
	}
	if total == 0 {
		t.Errorf("check counted 0 issues on a store no other command will touch:\n%s", out)
	}
	if strings.Contains(out, "All checks passed") {
		t.Errorf("check certified an unmigrated store as healthy:\n%s", out)
	}
	for _, want := range []string{"nibs migrate", "layout", store.DataDirName} {
		if !strings.Contains(out, want) {
			t.Errorf("check report does not mention %q:\n%s", want, out)
		}
	}

	resetRootPersistentFlags()
	resetCheckFlags()
	checkFix = true
	t.Cleanup(func() { checkFix = false })
	if _, err := runRootWith(t, "--nibs-path", storeDir, "check", "--fix"); err == nil {
		t.Error("check --fix ran on a legacy store; only the read-only check is exempt")
	}
}

// TestCheckOnACurrentStoreSaysNothingAboutMigration is the other half of the
// boundary: the migration line must appear only when something is pending, or
// every healthy store grows a permanent warning.
func TestCheckOnACurrentStoreSaysNothingAboutMigration(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetCheckFlags)
	resetCheckFlags()

	storeDir := writeStore(t, filepath.Join(t.TempDir(), "proj"), "nibs:\n  prefix: chk-\n", map[string]string{
		"chk-a1--one.md": layoutNib,
	})

	out, err := runRootWith(t, "--nibs-path", storeDir, "check")
	if err != nil {
		t.Fatalf("check on a current store: %v", err)
	}
	if !strings.Contains(out, "All checks passed") {
		t.Errorf("check on a healthy, current store did not pass:\n%s", out)
	}
	if strings.Contains(out, "nibs migrate") {
		t.Errorf("check warned about migration on an already-migrated store:\n%s", out)
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

// TestMigrateConfigRelocationConverges pins the crash-recovery half of the
// engine's idempotency contract over the one step that writes a file and then
// removes another. `relocateProjectConfig` writes <store>/config.yml and THEN
// deletes the legacy `.nibs.yml`; a crash between those two leaves both on
// disk, and a re-run must finish the job rather than refusing forever — every
// command refuses a store in that state, `nibs migrate` included, so a
// non-converging refusal is terminal.
//
// Convergence is deliberately narrow: only a config.yml BYTE-IDENTICAL to what
// this step would write is treated as its own interrupted write. Anything else
// is a genuine "which one did you mean?" and still refuses.
// The body deliberately CARRIES the retired `nibs.path` key, naming the store
// being migrated. Without it the rewrite is an identity no-op and the
// byte-identity comparison never sees the `yaml.Marshal` round trip that
// reindents the document from 2 spaces to 4 — the exact shape the resume exists
// to recognize.
func TestMigrateConfigRelocationConverges(t *testing.T) {
	const legacyBody = "nibs:\n  prefix: leg-\n  id_length: 4\n  path: " + store.DirName + "\n"

	// migrateOnce runs a full migration and returns the project and store dirs,
	// leaving the store in the current layout.
	migrateOnce := func(t *testing.T) (projectDir, storeDir string) {
		t.Helper()
		t.Cleanup(resetRootPersistentFlags)
		t.Cleanup(resetMigrateFlags)
		resetMigrateFlags()
		projectDir, storeDir = writeLegacyStore(t, legacyBody, map[string]string{
			"leg-a1--one.md": layoutNib,
		})
		if _, err := runRootWith(t, "--nibs-path", storeDir, "migrate"); err != nil {
			t.Fatalf("first migrate: %v", err)
		}
		return projectDir, storeDir
	}

	t.Run("an interrupted write finishes on the next run", func(t *testing.T) {
		projectDir, storeDir := migrateOnce(t)
		// Recreate the crash state: config.yml is exactly what the step wrote,
		// and the legacy file it should have removed is still there.
		legacy := filepath.Join(projectDir, store.LegacyProjectConfigFileName)
		if err := os.WriteFile(legacy, []byte(legacyBody), 0644); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(store.NewLayout(storeDir).ConfigPath())
		if err != nil {
			t.Fatal(err)
		}

		resetRootPersistentFlags()
		resetMigrateFlags()
		if _, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty"); err != nil {
			t.Fatalf("the re-run did not converge: %v", err)
		}
		if _, statErr := os.Stat(legacy); !os.IsNotExist(statErr) {
			t.Errorf("the legacy config survived the resumed run (stat err = %v)", statErr)
		}
		after, err := os.ReadFile(store.NewLayout(storeDir).ConfigPath())
		if err != nil {
			t.Fatalf("the store config vanished: %v", err)
		}
		if string(after) != string(before) {
			t.Errorf("the resumed run rewrote the store config:\nbefore:\n%s\nafter:\n%s", before, after)
		}
	})

	t.Run("two genuinely different configs still refuse", func(t *testing.T) {
		projectDir, storeDir := migrateOnce(t)
		legacy := filepath.Join(projectDir, store.LegacyProjectConfigFileName)
		// A second config that is NOT this step's own write: the two disagree
		// on the load-bearing field, so picking one is the user's call.
		if err := os.WriteFile(legacy, []byte("nibs:\n  prefix: other-\n"), 0644); err != nil {
			t.Fatal(err)
		}

		resetRootPersistentFlags()
		resetMigrateFlags()
		_, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty")
		if err == nil {
			t.Fatal("migrate silently discarded one of two differing configs")
		}
		// Name BOTH paths rather than matching the word "both": the refusal's
		// whole job is telling the user which two files it will not choose
		// between.
		for _, want := range []string{legacy, store.NewLayout(storeDir).ConfigPath()} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("refusal = %v, want it to name %s", err, want)
			}
		}
		if _, statErr := os.Stat(legacy); statErr != nil {
			t.Errorf("the refusal deleted the legacy config anyway: %v", statErr)
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
		// Faithful v0 renders: nib.Render has written the id comment since the
		// initial commit, and `version` carries no omitempty, so a nibs-written
		// v0 file renders `version: 0`. A fixture missing both is a file the
		// layout step can only ASSUME is a nib, which is a different test.
		"zzz-aaa1--blocker.md": "---\n# zzz-aaa1\nversion: 0\ntitle: Blocker\nstatus: todo\nblocking:\n    - bbb2\n---\n\nBody A.\n",
		"zzz-bbb2--blocked.md": "---\n# zzz-bbb2\nversion: 0\ntitle: Blocked\nstatus: todo\n---\n\nBody B.\n",
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

// TestMigrateScopesTheFailLoudGateToStoreContent pins the deviation that makes
// `.nibs/README.md` a legal place for a readme, end to end and on BOTH sides of
// the decision — the real run's refusal and --dry-run's preview of it.
//
// The gate exists because a CONTENT step rewrites edges and must see every file
// that could hold one. The files it will load are data/ and archive/, not the
// whole store tree the scans walk — so a fence-less .md at the store ROOT, which
// the layout step deliberately leaves behind and Core.Load then never sees,
// cannot endanger anything. Blocking on it wedges the CLI (every command
// refuses, and the one command that could fix it refuses too) over a file the
// new layout stops caring about.
func TestMigrateScopesTheFailLoudGateToStoreContent(t *testing.T) {
	const readme = "# Store notes\n\nNo front matter here.\n"
	const v0Nib = "---\n# leg-a1\nversion: 0\ntitle: One\nstatus: todo\n---\n\nBody.\n"

	t.Run("a content step runs past a fence-less file at the store root", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		t.Cleanup(resetMigrateFlags)
		resetMigrateFlags()
		_, storeDir := writeLegacyStore(t, "", map[string]string{
			"leg-a1--one.md": v0Nib,
			"README.md":      readme,
		})

		out, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty")
		if err != nil {
			t.Fatalf("migrate refused over a fence-less file the new layout leaves behind: %v\nout: %s", err, out)
		}
		// The readme stays where it is, unmodified and outside store content.
		after, readErr := os.ReadFile(filepath.Join(storeDir, "README.md"))
		if readErr != nil {
			t.Fatalf("the readme was moved or removed: %v", readErr)
		}
		if string(after) != readme {
			t.Errorf("migrate rewrote the readme:\n%s", after)
		}
		if _, statErr := os.Stat(filepath.Join(store.NewLayout(storeDir).DataDir(), "leg-a1--one.md")); statErr != nil {
			t.Errorf("the nib did not reach data/: %v", statErr)
		}
	})

	t.Run("--dry-run predicts that same run instead of announcing a refusal", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		t.Cleanup(resetMigrateFlags)
		resetMigrateFlags()
		// Only the layout step is pending here, so no content step is involved
		// at all — the run cannot refuse, and the preview must not say it will.
		_, storeDir := writeLegacyStore(t, "", map[string]string{
			"leg-a1--one.md": layoutNib,
			"README.md":      readme,
		})

		out, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--dry-run")
		if err != nil {
			t.Fatalf("--dry-run must preview, not fail: %v\nout: %s", err, out)
		}
		if strings.Contains(out, "refuse") {
			t.Errorf("dry-run announced a refusal the real run does not raise:\n%s", out)
		}

		resetRootPersistentFlags()
		resetMigrateFlags()
		if _, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty"); err != nil {
			t.Fatalf("the real run refused after a preview that promised success: %v", err)
		}
	})

	t.Run("blockingScanProblems classifies every position a problem can sit in", func(t *testing.T) {
		// The end-to-end subtests around this one need a symlink to produce an
		// UNREADABLE file, and skip where os.Symlink fails — CI's matrix includes
		// windows-latest, where symlinks commonly need elevation, so the
		// unreadable boundary would go unverified exactly there. This drives the
		// predicate directly over a synthesized scan instead: no filesystem, no
		// symlinks, same decision. Those skips are counted rather than silent now
		// (internal/testskip), so how much this stands in for is a number a run
		// reports rather than a thing to reason about.
		contentStep := -1
		for i, step := range migrationSteps {
			if step.isContent() {
				contentStep = i
				break
			}
		}
		if contentStep < 0 {
			t.Fatal("the chain has no content step, so this table tests nothing")
		}

		tests := []struct {
			name           string
			path           string
			unreadable     bool
			contentPending bool
			wantBlocked    bool
		}{
			{name: "a fence-less file under data/", path: "data/x.md", contentPending: true, wantBlocked: true},
			{name: "a fence-less file under archive/", path: "archive/x.md", contentPending: true, wantBlocked: true},
			{name: "a fence-less file nested under data/", path: "data/sub/x.md", contentPending: true, wantBlocked: true},
			{name: "a fence-less file at the store root", path: "README.md", contentPending: true},
			{name: "a fence-less file in a root subdirectory", path: "notes/README.md", contentPending: true},
			{
				// hasDirPrefix must match a whole path COMPONENT: a directory
				// merely beginning with "data" is not data/.
				name: "a fence-less file under a directory named dataset", path: "dataset/x.md", contentPending: true,
			},
			{name: "a fence-less file under a directory named archived", path: "archived/x.md", contentPending: true},
			{
				// The boundary the root scoping must not cross: the layout step
				// cannot prove an unreadable file is not a nib, so it moves it
				// into data/ where a content step would migrate around it.
				name: "an unreadable file at the store root", path: ".#x.md", unreadable: true, contentPending: true, wantBlocked: true,
			},
			{name: "an unreadable file already under data/", path: "data/.#x.md", unreadable: true, contentPending: true, wantBlocked: true},
			{
				// A shape step only MOVES files, and moves nothing it could not
				// classify, so nothing can block it.
				name: "an unreadable file with no content step pending", path: ".#x.md", unreadable: true,
			},
			{name: "a fence-less data/ file with no content step pending", path: "data/x.md"},
		}

		env := newMigrateEnv(t.TempDir())
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				scan := &storeScan{
					counts:   make([]int, len(migrationSteps)),
					problems: []scanProblem{{path: tt.path, reason: "probe", unreadable: tt.unreadable}},
				}
				if tt.contentPending {
					scan.counts[contentStep] = 1
				}
				blocking := blockingScanProblems(env, scan)
				if got := len(blocking) > 0; got != tt.wantBlocked {
					t.Errorf("blocked = %v, want %v for %s", got, tt.wantBlocked, tt.path)
				}
			})
		}
	})

	t.Run("an UNREADABLE root file still blocks a content step", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		t.Cleanup(resetMigrateFlags)
		resetMigrateFlags()
		// The boundary the scoping must not cross: unlike a fence-less file,
		// an unreadable one cannot be proven not to be a nib, so the layout
		// step moves it INTO data/ — where the content step would then have to
		// migrate around it, silently dropping edges pointing at it.
		_, storeDir := writeLegacyStore(t, "", map[string]string{
			"leg-a1--one.md": v0Nib,
		})
		link := filepath.Join(storeDir, ".#leg-a1--one.md")
		if err := os.Symlink(filepath.Join(storeDir, "no-such-target"), link); err != nil {
			testskip.SymlinkUnavailable(t, err)
		}

		out, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty")
		if err == nil {
			t.Fatalf("migrate ran past an unreadable file bound for data/\nout: %s", out)
		}
		if !strings.Contains(err.Error(), ".#leg-a1--one.md") {
			t.Errorf("refusal should name the unreadable file, got: %v", err)
		}
		// And it refuses BEFORE any step writes: the gate's whole posture is
		// that a store it will not finish migrating is left exactly as it was,
		// so repairing the file and re-running is one clean pass. (The content
		// step's own load gate would eventually catch this file too — but only
		// after the layout step had already moved the store.)
		if _, statErr := os.Stat(filepath.Join(storeDir, "leg-a1--one.md")); statErr != nil {
			t.Errorf("the refusal came after the layout step had already moved files: %v", statErr)
		}
	})
}

// TestMigrateRefusesAMisAimedLegacyConfig is the end-to-end guard for the one
// invocation the pre-layout docs recommended: `--config <project>/.nibs.yml`.
// Its directory is the PROJECT, so a migrate run that accepted it would walk
// the project tree, move every front-mattered .md into a freshly created
// <project>/data/ and rewrite each one as a nib render — while the real store
// at .nibs/ stayed untouched and unmigrated.
func TestMigrateRefusesAMisAimedLegacyConfig(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetMigrateFlags)
	resetMigrateFlags()

	projectDir, storeDir := writeLegacyStore(t, "nibs:\n  prefix: leg-\n", map[string]string{
		"leg-a1--one.md": layoutNib,
	})
	// An ordinary front-mattered document in the project tree: not a nib, and
	// the blast radius of resolving the store to the project directory.
	post := filepath.Join(projectDir, "docs", "post.md")
	if err := os.MkdirAll(filepath.Dir(post), 0755); err != nil {
		t.Fatal(err)
	}
	postBody := "---\ntitle: My blog post\nlayout: default\n---\n\nPost body.\n"
	if err := os.WriteFile(post, []byte(postBody), 0644); err != nil {
		t.Fatal(err)
	}

	legacy := filepath.Join(projectDir, store.LegacyProjectConfigFileName)
	out, err := runRootWith(t, "--config", legacy, "migrate", "--allow-dirty")
	if err == nil {
		t.Fatalf("migrate accepted --config at the pre-layout config\nout: %s", out)
	}
	// The refusal must come from the --config guard specifically. The fallback
	// store-evidence guard rejects this fixture too and names the project
	// directory in the same breath, so asserting only that passes with the
	// guard deleted.
	assertRefusedByConfigGuard(t, err, projectDir)

	// Nothing was created outside the store, and the unrelated document is
	// byte-identical.
	if _, statErr := os.Stat(filepath.Join(projectDir, store.DataDirName)); !os.IsNotExist(statErr) {
		t.Errorf("migrate created %s in the project directory (stat err = %v)", store.DataDirName, statErr)
	}
	got, readErr := os.ReadFile(post)
	if readErr != nil {
		t.Fatalf("the unrelated document was moved or removed: %v", readErr)
	}
	if string(got) != postBody {
		t.Errorf("the unrelated document was rewritten:\n%s", got)
	}
	// And the real store is still where it was, still unmigrated.
	if _, statErr := os.Stat(filepath.Join(storeDir, "leg-a1--one.md")); statErr != nil {
		t.Errorf("the real store's nib file moved: %v", statErr)
	}
}

// TestMigrateRefusesASiblingDirectoryOfAnUnmigratedProject is the end-to-end
// guard for the shape the evidence check used to accept on the weakest possible
// grounds: ANY sibling directory of an unmigrated project that happens to hold a
// markdown file. `--nibs-path <project>/docs` resolved docs/ as the store, and
// the layout step then DELETED the project's real `.nibs.yml` and relocated it
// into docs/ — after which `nibs check` certified the store healthy and
// `nibs init` silently re-prefixed the project, because the config the init
// guard looks for had been moved away.
//
// The fixture uses a fence-less README: front matter is not required to trip the
// old clause, so the guard must not depend on the file's content either.
func TestMigrateRefusesASiblingDirectoryOfAnUnmigratedProject(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetMigrateFlags)
	resetMigrateFlags()

	projectDir, storeDir := writeLegacyStore(t, "nibs:\n  prefix: real-\n  id_length: 6\n", map[string]string{
		"real-a1b2c3--one.md": layoutNib,
	})
	docs := filepath.Join(projectDir, "docs")
	mkdirAllT(t, docs)
	const readme = "# Docs\n\nOrdinary documentation, no front matter.\n"
	writeFileT(t, filepath.Join(docs, "README.md"), readme)

	out, err := runRootWith(t, "--nibs-path", docs, "migrate", "--allow-dirty")
	if err == nil {
		t.Fatalf("migrate accepted a sibling docs/ directory as the store\nout: %s", out)
	}
	if !strings.Contains(err.Error(), "not a nibs store") {
		t.Errorf("refusal = %v, want the store-evidence guard", err)
	}

	// The project's real config is untouched — that deletion is what made the
	// whole cascade silent.
	legacy := filepath.Join(projectDir, store.LegacyProjectConfigFileName)
	if _, statErr := os.Stat(legacy); statErr != nil {
		t.Errorf("the project's %s was deleted: %v", store.LegacyProjectConfigFileName, statErr)
	}
	if _, statErr := os.Stat(filepath.Join(docs, store.ConfigFileName)); !os.IsNotExist(statErr) {
		t.Errorf("migrate wrote a %s into docs/ (stat err = %v)", store.ConfigFileName, statErr)
	}
	if _, statErr := os.Stat(filepath.Join(docs, store.DataDirName)); !os.IsNotExist(statErr) {
		t.Errorf("migrate created docs/%s/ (stat err = %v)", store.DataDirName, statErr)
	}
	got, readErr := os.ReadFile(filepath.Join(docs, "README.md"))
	if readErr != nil {
		t.Fatalf("the readme was moved or removed: %v", readErr)
	}
	if string(got) != readme {
		t.Errorf("migrate rewrote the readme:\n%s", got)
	}
	// And the real store is still where it was, still unmigrated.
	if _, statErr := os.Stat(filepath.Join(storeDir, "real-a1b2c3--one.md")); statErr != nil {
		t.Errorf("the real store's nib file moved: %v", statErr)
	}
}

// TestLegacyStoreOutsideDotNibsStaysResolvable pins the escape hatch the
// store-evidence guard must leave open: a pre-layout project whose data lived
// somewhere other than `.nibs` (the retired `nibs.path` key) has no `.nibs`
// directory, so --nibs-path is the only way to name its store — and the guard
// must recognize the legacy shape rather than refusing the very stores
// `nibs migrate` exists to convert.
func TestLegacyStoreOutsideDotNibsStaysResolvable(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetListFlags)
	resetListFlags()

	projectDir := t.TempDir()
	storeDir := filepath.Join(projectDir, "nibdata")
	if err := os.MkdirAll(storeDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storeDir, "leg-a1--one.md"), []byte(layoutNib), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, store.LegacyProjectConfigFileName),
		[]byte("nibs:\n  prefix: leg-\n  path: nibdata\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := runRootWith(t, "--nibs-path", storeDir, "list")
	if err == nil {
		t.Fatal("list returned no error on a legacy store; every command must refuse until it is migrated")
	}
	// The refusal must be the MIGRATION gate, not the store-evidence guard:
	// the store resolved, it simply needs converting.
	if strings.Contains(err.Error(), "not a nibs store") {
		t.Errorf("the store-evidence guard rejected a genuine legacy store: %v", err)
	}
	if !strings.Contains(err.Error(), "nibs migrate") {
		t.Errorf("refusal = %v, want the migration gate naming `nibs migrate`", err)
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
