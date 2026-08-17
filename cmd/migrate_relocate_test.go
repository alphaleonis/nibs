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
		"step-plan": {allowDirty: true, build: func(t *testing.T) string {
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

// legacyV0BlockingStore is a pre-layout store whose only edge is a SHORT-FORM
// `blocking:` target. Resolving `a2` to `leg-a2` needs the project's prefix, and
// the project's prefix lives in the config the layout step relocates — so this
// fixture fails loudly whenever a content step runs against a stale config.
func legacyV0BlockingStore() map[string]string {
	return map[string]string{
		"leg-a1--one.md": "---\ntitle: One\nstatus: todo\nblocking:\n    - a2\n---\n\nBody A.\n",
		"leg-a2--two.md": "---\ntitle: Two\nstatus: todo\n---\n\nBody B.\n",
	}
}

// TestMigrateReadsTheConfigThatMovedWithTheStore pins that the config a CONTENT
// step reads follows the store the layout step just relocated.
//
// --config names a config INSIDE the store, so its value is a path under the
// pre-relocation root. Once the store moves, that path names a file under a
// directory that no longer exists — and an absent config reads as "use the
// defaults", so the content steps loaded the store under the EMPTY prefix, the
// short-form `blocking: a2` target became unresolvable, and the edge was dropped
// while the run printed "Migration complete." and exited 0.
//
// Both routes to the same store must produce the same store, which is what makes
// --nibs-path a usable control rather than a second assertion.
func TestMigrateReadsTheConfigThatMovedWithTheStore(t *testing.T) {
	routes := []struct {
		name string
		args func(storeDir string) []string
	}{
		{"--nibs-path", func(storeDir string) []string { return []string{"--nibs-path", storeDir} }},
		{"--config", func(storeDir string) []string {
			return []string{"--config", filepath.Join(storeDir, store.ConfigFileName)}
		}},
	}

	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			t.Cleanup(resetRootPersistentFlags)
			t.Cleanup(resetMigrateFlags)
			resetMigrateFlags()

			projectDir, storeDir := writeLegacyStoreNamed(t, "nibdata", legacyPathConfig("nibdata"), legacyV0BlockingStore())

			args := append(route.args(storeDir), "migrate", "--allow-dirty")
			out, err := runRootWith(t, args...)
			if err != nil {
				t.Fatalf("migrate via %s: %v\nout: %s", route.name, err, out)
			}
			if strings.Contains(out, "dropping the edge") {
				t.Errorf("the migration dropped an edge it should have resolved:\n%s", out)
			}

			target := filepath.Join(store.NewLayout(filepath.Join(projectDir, store.DirName)).DataDir(), "leg-a2--two.md")
			data, readErr := os.ReadFile(target)
			if readErr != nil {
				t.Fatalf("reading the migrated target: %v", readErr)
			}
			if !strings.Contains(string(data), "leg-a1") {
				t.Errorf("%s did not receive the inverted edge; content:\n%s", target, data)
			}
		})
	}
}

// TestMigrateRefusesARetiredNibsPathItCannotStat pins the THREE-WAY answer
// stripRetiredNibsPath has to give about the directory the retired key names.
//
// Dropping the key discards the only record of where a project's nibs live, so it
// is allowed exactly when that directory is definitely gone. os.Stat fails for far
// more than ENOENT — EACCES on an ancestor, an unmounted mount point, ELOOP — and
// storing the nibs on another volume is precisely why `nibs.path` existed, so
// every other failure must refuse and KEEP the file rather than assert a falsehood
// and delete it.
func TestMigrateRefusesARetiredNibsPathItCannotStat(t *testing.T) {
	tests := []struct {
		name string
		// build materializes whatever the declared value names and returns the
		// value to write into `nibs.path`.
		build func(t *testing.T, projectDir string) string
		// want are substrings of the refusal; empty means the run must SUCCEED.
		want []string
	}{
		{
			name:  "a directory that is really gone is a stale record",
			build: func(t *testing.T, projectDir string) string { return "gone" },
		},
		{
			name: "a directory that still exists is refused",
			build: func(t *testing.T, projectDir string) string {
				mkdirAllT(t, filepath.Join(projectDir, "elsewhere"))
				return "elsewhere"
			},
			want: []string{"still exists"},
		},
		{
			name: "a directory that cannot be stat'd is refused, not declared gone",
			build: func(t *testing.T, projectDir string) string {
				symlinkLoopT(t, filepath.Join(projectDir, "loop"))
				return "loop"
			},
			want: []string{"cannot be determined"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(resetRootPersistentFlags)
			t.Cleanup(resetMigrateFlags)
			resetMigrateFlags()

			projectDir := t.TempDir()
			storeDir := filepath.Join(projectDir, store.DirName)
			mkdirAllT(t, storeDir)
			writeFileT(t, filepath.Join(storeDir, "leg-a1--one.md"), layoutNib)
			declared := tt.build(t, projectDir)
			legacy := filepath.Join(projectDir, store.LegacyProjectConfigFileName)
			writeFileT(t, legacy, "nibs:\n  prefix: leg-\n  id_length: 4\n  path: "+declared+"\n")

			out, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty")

			if len(tt.want) == 0 {
				if err != nil {
					t.Fatalf("migrate: %v\nout: %s", err, out)
				}
				if !strings.Contains(out, "no longer exists") {
					t.Errorf("the stale-record note is missing:\n%s", out)
				}
				if _, statErr := os.Stat(legacy); !os.IsNotExist(statErr) {
					t.Errorf("%s survived a completed relocation (stat err = %v)", legacy, statErr)
				}
				return
			}

			if err == nil {
				t.Fatalf("migrate did not refuse\nout: %s", out)
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal = %q, want it to mention %q", err.Error(), want)
				}
			}
			if strings.Contains(err.Error(), "no longer exists") {
				t.Errorf("refusal = %q asserts the directory is gone, which is not what was observed", err.Error())
			}
			// The record of where the nibs live must survive the refusal, and
			// nothing may have moved.
			if _, statErr := os.Stat(legacy); statErr != nil {
				t.Errorf("%s was removed by a refused run: %v", legacy, statErr)
			}
			if _, statErr := os.Stat(filepath.Join(storeDir, "leg-a1--one.md")); statErr != nil {
				t.Errorf("a refused run moved a nib file anyway: %v", statErr)
			}
		})
	}
}

// TestMigrateRefusesToRelocateALinkedGitWorktree pins that the relocation does not
// destroy the rollback net the run just verified.
//
// The move is one os.Rename, justified by the store's own git repository travelling
// with it. A linked worktree's `.git` is a FILE pointing into the main repository's
// worktrees/<name>/, and that repository still records the pre-rename path — so
// after the move git reports the worktree `prunable`, and after any routine prune
// the relocated store answers "not a repository". gateStoreGitClean had just
// certified a recoverable baseline, and postMigrateCommitHint then tells the user to
// review the store's changes with git.
func TestMigrateRefusesToRelocateALinkedGitWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetMigrateFlags)
	resetMigrateFlags()

	projectDir, storeDir := writeLegacyStoreNamed(t, "nibdata", legacyPathConfig("nibdata"), nil)
	// A main repository elsewhere, with the store as a LINKED WORKTREE of it —
	// the shape a `.git` FILE marks.
	mainRepo := filepath.Join(t.TempDir(), "main")
	mkdirAllT(t, mainRepo)
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.email=t@example.com", "-c", "user.name=T", "commit", "-q", "--allow-empty", "-m", "root"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = mainRepo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.RemoveAll(storeDir); err != nil {
		t.Fatal(err)
	}
	add := exec.Command("git", "worktree", "add", "-q", "-b", "storebranch", storeDir)
	add.Dir = mainRepo
	if out, err := add.CombinedOutput(); err != nil {
		t.Skipf("git worktree add: %v\n%s", err, out)
	}
	writeFileT(t, filepath.Join(storeDir, "leg-a1--one.md"), layoutNib)
	if info, err := os.Lstat(filepath.Join(storeDir, ".git")); err != nil || !info.Mode().IsRegular() {
		t.Skipf("this git does not use a .git FILE for a linked worktree (err = %v)", err)
	}

	out, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty")
	if err == nil {
		t.Fatalf("migrate relocated a linked git worktree\nout: %s", out)
	}
	for _, want := range []string{"linked git worktree", "git worktree move"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal = %q, want it to mention %q", err.Error(), want)
		}
	}
	// Nothing may have moved: the store, its nib file and its git marker stay.
	for _, want := range []string{storeDir, filepath.Join(storeDir, "leg-a1--one.md"), filepath.Join(storeDir, ".git")} {
		if _, statErr := os.Lstat(want); statErr != nil {
			t.Errorf("%s is gone after a refused run: %v", want, statErr)
		}
	}
	if _, statErr := os.Lstat(filepath.Join(projectDir, store.DirName)); !os.IsNotExist(statErr) {
		t.Errorf("the refused run created %s anyway (stat err = %v)", filepath.Join(projectDir, store.DirName), statErr)
	}
	// A PLAIN repository is still relocated: the refusal is about the linkage,
	// not about git.
	resetRootPersistentFlags()
	resetMigrateFlags()
	plainProject, plainStore := writeLegacyStoreNamed(t, "nibdata", legacyPathConfig("nibdata"), map[string]string{
		"leg-a1--one.md": layoutNib,
	})
	initPlain := exec.Command("git", "init", "-q")
	initPlain.Dir = plainStore
	if out, initErr := initPlain.CombinedOutput(); initErr != nil {
		t.Skipf("git init: %v\n%s", initErr, out)
	}
	if out, plainErr := runRootWith(t, "--nibs-path", plainStore, "migrate", "--allow-dirty"); plainErr != nil {
		t.Fatalf("migrate refused a plain repository store: %v\nout: %s", plainErr, out)
	}
	if _, statErr := os.Stat(filepath.Join(plainProject, store.DirName, ".git")); statErr != nil {
		t.Errorf("the plain repository did not travel with the store: %v", statErr)
	}
}

// TestMigrateFindsFilesThroughASymlinkedStoreRoot pins that a store reached through
// a symlink is migrated rather than silently emptied.
//
// The store walk used to Lstat its root, so a symlinked store yielded no files at
// all: the layout step moved nothing, `nibs migrate` printed "Migration complete."
// and afterwards `nibs list` reported 0 nibs while `nibs check` printed
// "All nib files loaded" — with the nibs sitting unreachable at the old root. A
// symlink is the ordinary spelling of "the nibs live on another volume", which is
// what the retired `nibs.path` key existed for.
func TestMigrateFindsFilesThroughASymlinkedStoreRoot(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetMigrateFlags)
	resetMigrateFlags()

	projectDir := t.TempDir()
	real := filepath.Join(t.TempDir(), "elsewhere")
	mkdirAllT(t, real)
	writeFileT(t, filepath.Join(real, "leg-a1--one.md"), layoutNib)
	link := filepath.Join(projectDir, store.DirName)
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	out, err := runRootWith(t, "--nibs-path", link, "migrate", "--allow-dirty")
	if err != nil {
		t.Fatalf("migrate: %v\nout: %s", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(real, store.DataDirName, "leg-a1--one.md")); statErr != nil {
		t.Fatalf("the nib was not moved into %s/ through the link: %v", store.DataDirName, statErr)
	}

	resetRootPersistentFlags()
	resetListFlags()
	t.Cleanup(resetListFlags)
	listOut, listErr := runRootWith(t, "--nibs-path", link, "list", "--all", "--json")
	if listErr != nil {
		t.Fatalf("list: %v", listErr)
	}
	if ids := envelopeIDs(parseListEnvelope(t, listOut)); !ids["leg-a1"] {
		t.Errorf("list ids = %v, want leg-a1 — the store's only nib", ids)
	}
}

// TestMigrateDryRunAlwaysReportsTheUndecidableGate pins that a gate which cannot be
// answered before the run says so no matter what else the preview found.
//
// The note used to live in the `else` of the refusal branch, so it disappeared the
// moment any other gate fired — exactly the case where the user fixes what the
// preview named, re-runs, and meets the unpreviewed load refusal anyway. A note
// whose purpose is that its silence must not read as all-clear cannot be printed
// only when everything else is clear.
func TestMigrateDryRunAlwaysReportsTheUndecidableGate(t *testing.T) {
	// A version-less nib, so a CONTENT step is pending alongside the shape step.
	const v0Nib = "---\ntitle: One\nstatus: todo\n---\n\nBody.\n"
	const undecidableNote = "can only be checked once the layout step has moved the files"

	for _, tt := range []struct {
		name        string
		alsoRefuses bool
		build       func(t *testing.T) string
	}{
		{
			name: "no other gate fires",
			build: func(t *testing.T) string {
				_, storeDir := writeLegacyStore(t, "", map[string]string{"leg-a1--one.md": v0Nib})
				return storeDir
			},
		},
		{
			name:        "another gate also fires",
			alsoRefuses: true,
			build: func(t *testing.T) string {
				projectDir, storeDir := writeLegacyStoreNamed(t, "nibdata", legacyPathConfig("nibdata"), map[string]string{
					"leg-a1--one.md": v0Nib,
				})
				// An occupied relocation destination: the step-plan gate refuses,
				// and a content step is pending as well.
				mkdirAllT(t, filepath.Join(projectDir, store.DirName))
				return storeDir
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(resetRootPersistentFlags)
			t.Cleanup(resetMigrateFlags)
			resetMigrateFlags()
			storeDir := tt.build(t)

			out, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--dry-run", "--allow-dirty")
			if err != nil {
				t.Fatalf("--dry-run must preview, not fail: %v\nout: %s", err, out)
			}
			if strings.Contains(out, "the real run will refuse") != tt.alsoRefuses {
				t.Errorf("refusal prediction = %v, want %v\nout: %s", !tt.alsoRefuses, tt.alsoRefuses, out)
			}
			if !strings.Contains(out, undecidableNote) {
				t.Errorf("the un-previewable gate went unmentioned:\n%s", out)
			}
		})
	}
}

// TestMigrateConsultsGitOncePerQuestion pins that the narration and the gates that
// act on the same git observation share one invocation. Splitting them left every
// real run shelling out to git twice with identical arguments; --dry-run never did,
// because it only ran the gates.
func TestMigrateConsultsGitOncePerQuestion(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetMigrateFlags)
	resetMigrateFlags()

	projectDir, storeDir := writeLegacyStore(t, "nibs:\n  prefix: leg-\n", map[string]string{
		"leg-a1--one.md": layoutNib,
	})
	_ = projectDir

	storeCalls, legacyCalls := 0, 0
	origStore, origDirty := storeGitStateFn, gitIsDirtyFn
	storeGitStateFn = func(string) (bool, bool, error) { storeCalls++; return false, true, nil }
	gitIsDirtyFn = func(string, ...string) (bool, error) { legacyCalls++; return false, nil }
	t.Cleanup(func() { storeGitStateFn, gitIsDirtyFn = origStore, origDirty })

	if out, err := runRootWith(t, "--nibs-path", storeDir, "migrate"); err != nil {
		t.Fatalf("migrate: %v\nout: %s", err, out)
	}
	if storeCalls != 1 {
		t.Errorf("the store's git state was asked %d time(s), want 1", storeCalls)
	}
	if legacyCalls != 1 {
		t.Errorf("the legacy config's git state was asked %d time(s), want 1", legacyCalls)
	}
}

// TestMigrationChainInvariantsAreCheckable pins the two things about
// migrationSteps that nothing else in the engine encodes.
//
// The ORDER invariant — `layout` runs first — is carried only by this slice's
// position: it relocates the project config INTO the store, and every content step
// afterwards resolves short-form link ids under the prefix only that config carries.
// Run a content step first and its Core reads the empty default prefix, leaving a
// short-form `blocking:` target unresolvable and its edge dropped.
//
// The CAPABILITY invariant — the step that moves the files Core.Load reads says so —
// used to be inferred from "the step has no per-file predicate", which two gates read
// with opposite polarity. A second shape step would have silently disabled the load
// gate for the whole run.
func TestMigrationChainInvariantsAreCheckable(t *testing.T) {
	if len(migrationSteps) == 0 || migrationSteps[0].name != "layout" {
		t.Fatalf("migrationSteps[0] = %q, want \"layout\": every content step after it needs the config it relocates", migrationSteps[0].name)
	}
	for i, step := range migrationSteps {
		if step.isContent() == (step.shape != nil) {
			t.Errorf("step %q carries %d detectors, want exactly one (pred XOR shape)", step.name, i)
		}
		if step.invalidatesLoad && step.isContent() {
			t.Errorf("step %q is a content step yet claims to invalidate the load; a content step rewrites files in place", step.name)
		}
		if step.plan != nil && step.apply == nil {
			t.Errorf("step %q can plan but not apply", step.name)
		}
	}
	// Exactly one step moves the files today, and it is the first one. A second
	// would need reportDryRun's note and gateStoreLoadsCleanly revisited together.
	moving := 0
	for _, step := range migrationSteps {
		if step.invalidatesLoad {
			moving++
		}
	}
	if moving != 1 || !migrationSteps[0].invalidatesLoad {
		t.Errorf("%d step(s) invalidate the load and migrationSteps[0].invalidatesLoad = %v; want exactly the first step",
			moving, migrationSteps[0].invalidatesLoad)
	}
}

// TestMigrateStripsTheRetiredKeyFromAConfigInsideTheStore pins the way out of a
// terminal state.
//
// A user who hand-moves `.nibs.yml` to `.nibs/config.yml` — the obvious reading of
// the new layout — leaves the retired `nibs.path` key inside the store. config's
// loader refuses such a config outright and names `nibs migrate`, while
// `nibs migrate` answered "Store is up to date; no migrations pending": no legacy
// config beside the store, no movable files, no relocation. Every command failed
// with the one command that was supposed to fix it doing nothing.
func TestMigrateStripsTheRetiredKeyFromAConfigInsideTheStore(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetMigrateFlags)
	resetMigrateFlags()

	projectDir := t.TempDir()
	storeDir := filepath.Join(projectDir, store.DirName)
	mkdirAllT(t, filepath.Join(storeDir, store.DataDirName))
	writeFileT(t, filepath.Join(storeDir, store.DataDirName, "leg-a1--one.md"), layoutNib)
	// The hand-moved config: correct location, retired key still in it. `nibs.path`
	// named `.nibs` relative to the project, which IS this store.
	configFile := filepath.Join(storeDir, store.ConfigFileName)
	writeFileT(t, configFile, "nibs:\n  prefix: leg-\n  id_length: 4\n  path: .nibs\n")

	// Before: every command refuses and names this command.
	resetListFlags()
	t.Cleanup(resetListFlags)
	if _, err := runRootWith(t, "--nibs-path", storeDir, "list", "--all"); err == nil {
		t.Fatal("`nibs list` accepted a config carrying the retired key")
	} else if !strings.Contains(err.Error(), "nibs migrate") {
		t.Errorf("refusal = %q, want it to name `nibs migrate`", err.Error())
	}

	resetRootPersistentFlags()
	out, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty")
	if err != nil {
		t.Fatalf("migrate: %v\nout: %s", err, err)
	}
	if strings.Contains(out, "up to date") {
		t.Errorf("migrate reported the store up to date while every command refuses it:\n%s", out)
	}

	// After: the key is gone, the prefix survived, and commands work.
	data, readErr := os.ReadFile(configFile)
	if readErr != nil {
		t.Fatalf("reading the rewritten config: %v", readErr)
	}
	if strings.Contains(string(data), "path:") {
		t.Errorf("the retired key survived the migration:\n%s", data)
	}
	if !strings.Contains(string(data), "prefix: leg-") {
		t.Errorf("the rewrite lost the rest of the config:\n%s", data)
	}
	resetRootPersistentFlags()
	resetListFlags()
	listOut, listErr := runRootWith(t, "--nibs-path", storeDir, "list", "--all", "--json")
	if listErr != nil {
		t.Fatalf("list after the migration: %v", listErr)
	}
	if ids := envelopeIDs(parseListEnvelope(t, listOut)); !ids["leg-a1"] {
		t.Errorf("list ids = %v, want leg-a1", ids)
	}
}
