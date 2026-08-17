package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/store"
)

// legacyPathConfig is a pre-layout config whose retired `nibs.path` names a
// store outside `.nibs` — the population the layout step's store relocation
// exists for.
func legacyPathConfig(storeName string) string {
	return "nibs:\n  prefix: leg-\n  id_length: 4\n  path: " + storeName + "\n"
}

// TestMigrateRelocatesAStoreOutsideDotNibs pins the layout step's first move: a
// pre-layout store whose `.nibs.yml` pointed at it through the retired
// `nibs.path` key is moved to `<project>/.nibs`.
//
// Without the move the migration produced a correctly shaped store that
// discovery can never find — `store.FindStore` keys on the directory NAME and
// nothing replaces `nibs.path` as a root pointer — so the next `nibs list`
// answered "run `nibs init`", which creates an empty second store beside the real
// data. The assertion that matters is therefore not the file layout but that
// plain `nibs list`, with no --nibs-path, works from the project root AND from a
// subdirectory afterwards.
func TestMigrateRelocatesAStoreOutsideDotNibs(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetMigrateFlags)
	resetMigrateFlags()

	projectDir, storeDir := writeLegacyStoreNamed(t, "nibdata", legacyPathConfig("nibdata"), map[string]string{
		"leg-a1--one.md":         layoutNib,
		"archive/leg-c3--old.md": layoutNib,
	})

	out, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty")
	if err != nil {
		t.Fatalf("migrate: %v\nout: %s", err, out)
	}

	relocated := filepath.Join(projectDir, store.DirName)
	if _, statErr := os.Stat(storeDir); !os.IsNotExist(statErr) {
		t.Errorf("the store is still at %s (stat err = %v)", storeDir, statErr)
	}
	l := store.NewLayout(relocated)
	for _, want := range []string{
		l.ConfigPath(),
		filepath.Join(l.DataDir(), "leg-a1--one.md"),
		filepath.Join(l.ArchiveDir(), "leg-c3--old.md"),
	} {
		if _, statErr := os.Stat(want); statErr != nil {
			t.Errorf("%s is missing after the relocation: %v", want, statErr)
		}
	}
	if _, statErr := os.Stat(filepath.Join(projectDir, store.LegacyProjectConfigFileName)); !os.IsNotExist(statErr) {
		t.Errorf("the legacy .nibs.yml survived the migration (stat err = %v)", statErr)
	}
	// The retired key must be gone, or the relocated store refuses to load.
	body, readErr := os.ReadFile(l.ConfigPath())
	if readErr != nil {
		t.Fatalf("reading the relocated config: %v", readErr)
	}
	if strings.Contains(string(body), "path:") {
		t.Errorf("the retired `nibs.path` key survived into the store config:\n%s", body)
	}

	// Discovery, which is the whole point: no --nibs-path, from two directories.
	t.Setenv("NIBS_CONFIG_ROOT", projectDir)
	t.Setenv("NIBS_PATH", "")
	deep := filepath.Join(projectDir, "src", "internal")
	mkdirAllT(t, deep)
	for _, dir := range []string{projectDir, deep} {
		resetRootPersistentFlags()
		resetListFlags()
		t.Cleanup(resetListFlags)
		t.Chdir(dir)
		listOut, listErr := runRootWith(t, "list", "--all", "--json")
		if listErr != nil {
			t.Fatalf("list from %s after migrating: %v", dir, listErr)
		}
		ids := envelopeIDs(parseListEnvelope(t, listOut))
		for _, want := range []string{"leg-a1", "leg-c3"} {
			if !ids[want] {
				t.Errorf("list from %s: ids = %v, want %s", dir, ids, want)
			}
		}
	}
}

// TestMigrateRelocationCarriesTheStoresGitRepository pins that the relocation is
// ONE directory rename rather than a file-by-file rebuild: a store that is its
// own git repository — the shape this project's own `.nibs/` has — must arrive
// with its history intact, or the migration destroys the rollback it tells the
// user to rely on.
func TestMigrateRelocationCarriesTheStoresGitRepository(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetMigrateFlags)
	resetMigrateFlags()

	projectDir, storeDir := writeLegacyStoreNamed(t, "nibdata", legacyPathConfig("nibdata"), map[string]string{
		"leg-a1--one.md": layoutNib,
	})
	if out, err := exec.Command("git", "-C", storeDir, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init failed (git unavailable?): %v\n%s", err, out)
	}
	gitCommitAll(t, storeDir)
	before, err := exec.Command("git", "-C", storeDir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Skipf("git rev-parse failed: %v", err)
	}

	if out, migrateErr := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty"); migrateErr != nil {
		t.Fatalf("migrate: %v\nout: %s", migrateErr, out)
	}

	relocated := filepath.Join(projectDir, store.DirName)
	after, err := exec.Command("git", "-C", relocated, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("the relocated store is no longer a git repository: %v", err)
	}
	if strings.TrimSpace(string(after)) != strings.TrimSpace(string(before)) {
		t.Errorf("HEAD changed across the relocation: %s -> %s", before, after)
	}
}

// TestMigrateRefusesToRelocateOntoAnExistingStore pins that the relocation never
// merges: two candidate stores mean one is stale, and choosing silently would
// either strand the real data or bury it under an empty store. Both ways out of
// the refusal converge, so the message names removing one of them.
func TestMigrateRefusesToRelocateOntoAnExistingStore(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetMigrateFlags)
	resetMigrateFlags()

	projectDir, storeDir := writeLegacyStoreNamed(t, "nibdata", legacyPathConfig("nibdata"), map[string]string{
		"leg-a1--one.md": layoutNib,
	})
	occupied := filepath.Join(projectDir, store.DirName)
	mkdirAllT(t, occupied)

	out, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty")
	if err == nil {
		t.Fatalf("migrate merged a second store into %s\nout: %s", occupied, out)
	}
	for _, want := range []string{storeDir, occupied, "re-run `nibs migrate`"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal = %v, want it to mention %q", err, want)
		}
	}
	// Nothing moved, in either direction.
	if _, statErr := os.Stat(filepath.Join(storeDir, "leg-a1--one.md")); statErr != nil {
		t.Errorf("the refusal moved the store's nib file anyway: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(projectDir, store.LegacyProjectConfigFileName)); statErr != nil {
		t.Errorf("the refusal deleted the legacy config anyway: %v", statErr)
	}

	// Removing the empty candidate converges.
	if rmErr := os.Remove(occupied); rmErr != nil {
		t.Fatalf("removing the empty store: %v", rmErr)
	}
	resetRootPersistentFlags()
	resetMigrateFlags()
	if _, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty"); err != nil {
		t.Fatalf("migrate did not converge after the conflict was resolved: %v", err)
	}
	if _, statErr := os.Stat(store.NewLayout(occupied).ConfigPath()); statErr != nil {
		t.Errorf("the converged run did not relocate the store: %v", statErr)
	}
}

// TestMigrateDropsAStaleNibsPathRecord pins the convergence rule that keeps a
// half-relocated project from wedging.
//
// A `nibs.path` naming a directory OTHER than the store being migrated is
// refused, because discarding the only record of where a project's nibs live
// would strand them. Once that directory is GONE there is nothing left to
// strand — the record is stale — and the run must drop the key rather than refuse
// forever. This is exactly the state a run interrupted between the store
// relocation and the config write leaves behind: the store is at `.nibs`, and the
// key still names the directory it came from.
func TestMigrateDropsAStaleNibsPathRecord(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetMigrateFlags)
	resetMigrateFlags()

	// The crash state: `nibs.path: nibdata`, no nibdata/, store already at .nibs.
	projectDir, storeDir := writeLegacyStore(t, legacyPathConfig("nibdata"), map[string]string{
		"leg-a1--one.md": layoutNib,
	})

	out, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty")
	if err != nil {
		t.Fatalf("migrate did not converge on a stale `nibs.path` record: %v\nout: %s", err, out)
	}
	if !strings.Contains(out, "stale record") {
		t.Errorf("dropping the retired key silently is not acceptable; output was:\n%s", out)
	}
	if _, statErr := os.Stat(filepath.Join(projectDir, store.LegacyProjectConfigFileName)); !os.IsNotExist(statErr) {
		t.Errorf("the legacy config survived (stat err = %v)", statErr)
	}
	body, readErr := os.ReadFile(store.NewLayout(storeDir).ConfigPath())
	if readErr != nil {
		t.Fatalf("reading the relocated config: %v", readErr)
	}
	if strings.Contains(string(body), "path:") {
		t.Errorf("the stale key survived into the store config:\n%s", body)
	}
}

// TestMigrateRefusesALiveNibsPathMismatchAndNamesAWorkingRemedy is the other side
// of the stale-record rule: while the declared directory still holds data, the key
// is a real pointer and the run must refuse. Both remedies the message names have
// to converge — the previous wording told the user to move files into a directory
// no filesystem action could make the config VALUE agree with, so re-running
// after following it refused identically, forever.
func TestMigrateRefusesALiveNibsPathMismatchAndNamesAWorkingRemedy(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetMigrateFlags)
	resetMigrateFlags()

	// Two live candidates: the store at .nibs and the directory nibs.path names.
	projectDir, storeDir := writeLegacyStore(t, legacyPathConfig("nibdata"), map[string]string{
		"leg-a1--one.md": layoutNib,
	})
	otherDir := filepath.Join(projectDir, "nibdata")
	mkdirAllT(t, otherDir)
	writeFileT(t, filepath.Join(otherDir, "leg-b2--two.md"), layoutNib)

	_, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty")
	if err == nil {
		t.Fatal("migrate silently dropped a `nibs.path` still naming a live directory")
	}
	legacy := filepath.Join(projectDir, store.LegacyProjectConfigFileName)
	for _, want := range []string{otherDir, storeDir, legacy, "remove the retired `nibs.path` key"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal = %v, want it to mention %q", err, want)
		}
	}

	// The remedy the message names: remove the key. It must converge.
	writeFileT(t, legacy, "nibs:\n  prefix: leg-\n  id_length: 4\n")
	resetRootPersistentFlags()
	resetMigrateFlags()
	if _, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty"); err != nil {
		t.Fatalf("removing the retired key did not converge: %v", err)
	}
	if _, statErr := os.Stat(store.NewLayout(storeDir).ConfigPath()); statErr != nil {
		t.Errorf("the converged run did not relocate the config: %v", statErr)
	}
}

// TestMigrateLayoutRaisesEveryRefusalBeforeMovingAnything pins the plan/apply
// split. Every refusal the layout step can raise comes from planning, which
// touches nothing — so a store it will not finish migrating is left exactly as it
// was and repairing plus re-running is one clean pass.
//
// The config-relocation validation used to run AFTER the file-move loop, so a
// `nibs.path` refusal arrived with the store already half-moved.
func TestMigrateLayoutRaisesEveryRefusalBeforeMovingAnything(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetMigrateFlags)
	resetMigrateFlags()

	projectDir, storeDir := writeLegacyStore(t, legacyPathConfig("elsewhere"), map[string]string{
		"leg-a1--one.md": layoutNib,
		"leg-b2--two.md": layoutNib,
	})
	// The declared directory exists, so the mismatch is a genuine refusal.
	mkdirAllT(t, filepath.Join(projectDir, "elsewhere"))

	_, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty")
	if err == nil {
		t.Fatal("migrate accepted a `nibs.path` naming a live directory other than the store")
	}
	for _, base := range []string{"leg-a1--one.md", "leg-b2--two.md"} {
		if _, statErr := os.Stat(filepath.Join(storeDir, base)); statErr != nil {
			t.Errorf("the refusal came after %s had already moved: %v", base, statErr)
		}
	}
	if _, statErr := os.Stat(store.NewLayout(storeDir).DataDir()); !os.IsNotExist(statErr) {
		t.Errorf("the refusal created data/ before validating (stat err = %v)", statErr)
	}
}

// TestMigrateReportsBothFaultsWhenTwoConfigsMeetANibsPathMismatch pins the
// diagnostic completeness the read order buys. With a mismatched `nibs.path` AND
// a genuine second config, the rewrite's refusal used to fire first and the
// second config was never mentioned — one extra fix-and-rerun cycle, and a
// user who resolves only what they were told still refuses.
func TestMigrateReportsBothFaultsWhenTwoConfigsMeetANibsPathMismatch(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetMigrateFlags)
	resetMigrateFlags()

	projectDir, storeDir := writeLegacyStore(t, legacyPathConfig("elsewhere"), map[string]string{
		"data/leg-a1--one.md": layoutNib,
	})
	mkdirAllT(t, filepath.Join(projectDir, "elsewhere"))
	dest := store.NewLayout(storeDir).ConfigPath()
	writeFileT(t, dest, "nibs:\n  prefix: other-\n")

	_, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty")
	if err == nil {
		t.Fatal("migrate chose between two configs")
	}
	legacy := filepath.Join(projectDir, store.LegacyProjectConfigFileName)
	for _, want := range []string{legacy, dest, "nibs.path"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal = %v, want it to mention %q", err, want)
		}
	}
}

// TestMigrateConfigRelocationRefusesANonRegularDestination pins the symlink
// hazards around `<store>/config.yml`, both of which reported SUCCESS before:
//
//   - a config.yml symlinked AT the legacy `.nibs.yml` read back byte-identical
//     through os.Stat, so the byte-identity resume deleted the only real config
//     and left a broken link; `nibs check --json` then said success:true and the
//     next `nibs new` minted ids under no prefix at all;
//   - a DANGLING config.yml symlink made os.Stat fail, so the fresh-write branch
//     ran and os.WriteFile followed the link, creating the config outside the
//     store — after which the legacy config was deleted and migrate exited 0.
func TestMigrateConfigRelocationRefusesANonRegularDestination(t *testing.T) {
	tests := []struct {
		name string
		// target returns what the destination symlink points at, given the
		// project and store directories.
		target func(projectDir, storeDir string) string
		// escapee, when non-empty, is a path the write must NOT create.
		escapee func(projectDir string) string
	}{
		{
			name: "a config.yml symlinked at the legacy config",
			target: func(projectDir, _ string) string {
				return filepath.Join(projectDir, store.LegacyProjectConfigFileName)
			},
		},
		{
			name: "a dangling config.yml symlink pointing outside the store",
			target: func(projectDir, _ string) string {
				return filepath.Join(projectDir, "escaped.yml")
			},
			escapee: func(projectDir string) string { return filepath.Join(projectDir, "escaped.yml") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(resetRootPersistentFlags)
			t.Cleanup(resetMigrateFlags)
			resetMigrateFlags()

			projectDir, storeDir := writeLegacyStore(t, "nibs:\n  prefix: leg-\n  id_length: 4\n", map[string]string{
				"data/leg-a1--one.md": layoutNib,
			})
			legacy := filepath.Join(projectDir, store.LegacyProjectConfigFileName)
			dest := store.NewLayout(storeDir).ConfigPath()
			if err := os.Symlink(tt.target(projectDir, storeDir), dest); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}

			_, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty")
			if err == nil {
				t.Fatal("migrate treated a symlinked config.yml as its own interrupted write")
			}
			if !strings.Contains(err.Error(), "not a regular file") {
				t.Errorf("refusal = %v, want it to say the destination is not a regular file", err)
			}
			// The project keeps its only real config.
			if _, statErr := os.Lstat(legacy); statErr != nil {
				t.Errorf("the refusal deleted the project's config: %v", statErr)
			}
			if tt.escapee != nil {
				if _, statErr := os.Lstat(tt.escapee(projectDir)); !os.IsNotExist(statErr) {
					t.Errorf("the write followed the symlink out of the store (stat err = %v)", statErr)
				}
			}
		})
	}
}

// TestMigrateConfigRelocationPreservesTheSourceMode pins that a MOVE does not
// widen permissions. The write hardcoded 0644, so a 0600 legacy config arrived
// world-readable.
func TestMigrateConfigRelocationPreservesTheSourceMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not carry POSIX permission bits through os.Chmod the way this asserts")
	}
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetMigrateFlags)
	resetMigrateFlags()

	projectDir, storeDir := writeLegacyStore(t, "nibs:\n  prefix: leg-\n  id_length: 4\n", map[string]string{
		"data/leg-a1--one.md": layoutNib,
	})
	legacy := filepath.Join(projectDir, store.LegacyProjectConfigFileName)
	if err := os.Chmod(legacy, 0600); err != nil {
		t.Fatalf("chmod legacy config: %v", err)
	}

	if out, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty"); err != nil {
		t.Fatalf("migrate: %v\nout: %s", err, out)
	}
	info, err := os.Stat(store.NewLayout(storeDir).ConfigPath())
	if err != nil {
		t.Fatalf("stat the relocated config: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Errorf("relocated config mode = %v, want 0600 — a move must not widen permissions", got)
	}
}

// TestMigrateNamesTheBrokenConfigRatherThanOfferingAChoice pins the asymmetry the
// two-config refusal has to respect. "Keep the one you want and delete the other"
// is right for two genuine configs and wrong for a destination that is not a whole
// config: a user who keeps the one in the new canonical location deletes the only
// complete copy, and the next command fails on a YAML unmarshal error with nothing
// left to recover from.
func TestMigrateNamesTheBrokenConfigRatherThanOfferingAChoice(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetMigrateFlags)
	resetMigrateFlags()

	projectDir, storeDir := writeLegacyStore(t, "nibs:\n  prefix: leg-\n  id_length: 4\n", map[string]string{
		"data/leg-a1--one.md": layoutNib,
	})
	legacy := filepath.Join(projectDir, store.LegacyProjectConfigFileName)
	dest := store.NewLayout(storeDir).ConfigPath()
	// Not YAML at all — a torn write, or a hand edit.
	writeFileT(t, dest, "nibs:\n  prefix: [unterminated\n")

	_, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty")
	if err == nil {
		t.Fatal("migrate accepted an unparseable config.yml")
	}
	if !strings.Contains(err.Error(), "not valid YAML") {
		t.Errorf("refusal = %v, want it to say the destination is not valid YAML", err)
	}
	if !strings.Contains(err.Error(), legacy+" is intact") {
		t.Errorf("refusal = %v, want it to say %s is the intact copy", err, legacy)
	}
	if strings.Contains(err.Error(), "keep the one you want") {
		t.Errorf("refusal offers a symmetric choice over an unparseable file: %v", err)
	}
	if _, statErr := os.Stat(legacy); statErr != nil {
		t.Errorf("the refusal deleted the intact config: %v", statErr)
	}
}

// TestMigrateDryRunPreviewsEveryRefusalGate walks migrateGates and requires each
// entry to have a fixture that trips it, previewed by --dry-run AND raised by the
// real run.
//
// Sharing a predicate per gate is not enough, and that is the point of this test:
// two gates — the store's load gate and the dirty-git refusal — had no preview at
// all, so `--dry-run` printed step counts with no warning for a store the real run
// exited 5 on. A gate added to migrateGates without a row here fails this test,
// which is what makes the completeness of the SET enforced rather than assumed.
func TestMigrateDryRunPreviewsEveryRefusalGate(t *testing.T) {
	const v0Nib = "---\ntitle: One\nstatus: todo\n---\n\nBody.\n"

	// build returns the store to migrate; allowDirty mirrors what the real
	// invocation passes, since two gates are disabled by --allow-dirty.
	fixtures := map[string]struct {
		build      func(t *testing.T) string
		allowDirty bool
	}{
		"dirty-store": {build: func(t *testing.T) string {
			_, storeDir := writeLegacyStore(t, "", map[string]string{"leg-a1--one.md": layoutNib})
			if out, err := exec.Command("git", "-C", storeDir, "init", "-q").CombinedOutput(); err != nil {
				t.Skipf("git init failed (git unavailable?): %v\n%s", err, out)
			}
			// A committed baseline plus one uncommitted change.
			gitCommitAll(t, storeDir)
			writeFileT(t, filepath.Join(storeDir, "leg-a1--one.md"), layoutNib+"\nedited\n")
			return storeDir
		}},
		"dirty-legacy-config": {build: func(t *testing.T) string {
			projectDir, storeDir := writeLegacyStore(t, "nibs:\n  prefix: leg-\n", map[string]string{
				"leg-a1--one.md": layoutNib,
			})
			// The store repo is clean, so only the PROJECT repo can refuse.
			if out, err := exec.Command("git", "-C", storeDir, "init", "-q").CombinedOutput(); err != nil {
				t.Skipf("git init failed (git unavailable?): %v\n%s", err, out)
			}
			gitCommitAll(t, storeDir)
			if out, err := exec.Command("git", "-C", projectDir, "init", "-q").CombinedOutput(); err != nil {
				t.Skipf("git init failed (git unavailable?): %v\n%s", err, out)
			}
			return storeDir
		}},
		"layout-plan": {allowDirty: true, build: func(t *testing.T) string {
			projectDir, storeDir := writeLegacyStoreNamed(t, "nibdata", legacyPathConfig("nibdata"), map[string]string{
				"leg-a1--one.md": layoutNib,
			})
			mkdirAllT(t, filepath.Join(projectDir, store.DirName))
			return storeDir
		}},
		"unclassifiable-content": {allowDirty: true, build: func(t *testing.T) string {
			_, storeDir := writeLegacyStore(t, "", map[string]string{"leg-a1--one.md": v0Nib})
			link := filepath.Join(storeDir, ".#leg-a1--one.md")
			if err := os.Symlink(filepath.Join(storeDir, "no-such-target"), link); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			return storeDir
		}},
		"store-loads-cleanly": {allowDirty: true, build: func(t *testing.T) string {
			// Only a CONTENT step pending (the files are already under data/),
			// with two files claiming one id.
			_, storeDir := writeLegacyStore(t, "", map[string]string{
				"data/leg-a1--one.md": v0Nib,
				"data/leg-a1--two.md": v0Nib,
			})
			return storeDir
		}},
	}

	// The two sets must match in BOTH directions. Iterating migrateGates alone
	// catches a gate added with no test; iterating the fixtures alone catches a
	// gate deleted from the list, which would otherwise silently stop being
	// checked here and in the preview at the same time.
	gateNames := make(map[string]bool, len(migrateGates))
	for _, gate := range migrateGates {
		gateNames[gate.name] = true
		if _, ok := fixtures[gate.name]; !ok {
			t.Errorf("gate %q has no fixture here: every precondition must be shown to be previewed AND raised", gate.name)
		}
	}
	for name := range fixtures {
		if !gateNames[name] {
			t.Errorf("fixture %q names no gate in migrateGates: a precondition was removed from the shared list", name)
		}
	}

	for _, gate := range migrateGates {
		fixture, ok := fixtures[gate.name]
		if !ok {
			continue
		}
		t.Run(gate.name, func(t *testing.T) {
			t.Cleanup(resetRootPersistentFlags)
			t.Cleanup(resetMigrateFlags)
			resetMigrateFlags()
			storeDir := fixture.build(t)

			args := []string{"--nibs-path", storeDir, "migrate", "--dry-run"}
			if fixture.allowDirty {
				args = append(args, "--allow-dirty")
			}
			out, err := runRootWith(t, args...)
			if err != nil {
				t.Fatalf("--dry-run must preview, not fail: %v\nout: %s", err, out)
			}
			if !strings.Contains(out, "the real run will refuse") {
				t.Errorf("--dry-run did not predict the refusal:\n%s", out)
			}
			if !strings.Contains(out, gate.name+":") {
				t.Errorf("--dry-run did not label the refusal with the gate %q:\n%s", gate.name, out)
			}

			resetRootPersistentFlags()
			resetMigrateFlags()
			realArgs := []string{"--nibs-path", storeDir, "migrate"}
			if fixture.allowDirty {
				realArgs = append(realArgs, "--allow-dirty")
			}
			if _, runErr := runRootWith(t, realArgs...); runErr == nil {
				t.Errorf("the real run did not refuse, so the preview was wrong about gate %q", gate.name)
			}
		})
	}
}
