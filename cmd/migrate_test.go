package cmd

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/store"
	"github.com/alphaleonis/nibs/internal/testskip"
	"github.com/spf13/pflag"
)

// v0MigrateFixture is a hand-written v0 store: no `version:` key (absent = 0,
// legacy) and a dual-side `blocking:` edge that the v0→v1 migration must invert
// onto the target's blocked_by. Fixtures are written by hand rather than via
// fixtures.CopySampleProject because that dataset is already current-format.
func v0MigrateFixture() map[string]string {
	return map[string]string{
		"aaa1--blocker.md": "---\ntitle: Blocker\nstatus: todo\nblocking:\n    - bbb2\n---\n\nBody A.\n",
		"bbb2--blocked.md": "---\ntitle: Blocked\nstatus: todo\n---\n\nBody B.\n",
	}
}

// runRootWith drives the real Cobra pipeline with the given args, capturing
// stdout and returning the execution error.
//
// It clears the root's persistent flags on the way IN as well as when the
// calling test ends. Those live in package-level vars on a shared rootCmd, and
// root.go reads `nibsPath` by value without consulting pflag's Changed bit — so
// a `--nibs-path` left set anywhere silently binds the store for a later run
// that meant to bind by discovery, an order dependence only `-shuffle=on`
// reveals.
//
// Clearing on entry is what makes this hold WITHIN one test too: t.Cleanup
// fires when the test ends, so a second call in the same test would otherwise
// inherit the first call's flag. Doing it here rather than asking every caller
// to remember is what keeps it true as callers are added — though a test that
// drives rootCmd.Execute() by hand still owes its own reset.
func runRootWith(t *testing.T, args ...string) (string, error) {
	t.Helper()
	resetRootPersistentFlags()
	t.Cleanup(resetRootPersistentFlags)
	rootCmd.SetArgs(args)
	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	return out, execErr
}

// writeStoreFiles materializes a store fixture in a fresh .nibs dir, in the
// CURRENT layout: the files land under data/ unless the name already places
// them somewhere the store owns (archive/, or a dot directory). Legacy-shape
// fixtures — files at the store ROOT — belong to the layout step and are built
// with writeLegacyStore instead.
func writeStoreFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	nibsDir := filepath.Join(t.TempDir(), ".nibs")
	if err := os.MkdirAll(storeDataDir(nibsDir), 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		path := filepath.Join(nibsDir, storeRelForFixture(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return nibsDir
}

// storeRelForFixture places a fixture file inside the store: names that
// already address a store directory (archive/, a dot directory such as .git)
// are taken as written; everything else goes under data/.
func storeRelForFixture(name string) string {
	slashed := filepath.ToSlash(name)
	if strings.HasPrefix(slashed, store.ArchiveDirName+"/") ||
		strings.HasPrefix(slashed, store.DataDirName+"/") ||
		strings.HasPrefix(slashed, ".") {
		return filepath.FromSlash(name)
	}
	return filepath.Join(store.DataDirName, filepath.FromSlash(name))
}

// pendingNames runs pendingMigrations over a fixture store and returns the
// pending step names.
func pendingNames(t *testing.T, nibsDir string) []string {
	t.Helper()
	pending, err := pendingMigrations(newMigrateEnv(nibsDir))
	if err != nil {
		t.Fatalf("pendingMigrations: %v", err)
	}
	names := make([]string, len(pending))
	for i, s := range pending {
		names[i] = s.name
	}
	return names
}

// TestPendingMigrationsDetectTable pins what each store shape reports as
// pending. Detection is a pure header probe, so these drive pendingMigrations
// directly over hand-written fixtures.
func TestPendingMigrationsDetectTable(t *testing.T) {
	const v0Nib = "---\n# leg-a1\nversion: 0\ntitle: Legacy\nstatus: todo\n---\n\nBody.\n"
	const v1Nib = "---\n# leg-a1\nversion: 1\ntitle: Old\nstatus: todo\n---\n\nBody.\n"
	const v2Nib = "---\n# leg-a1\nversion: 2\ntitle: Current\nstatus: todo\n---\n\nBody.\n"
	// A v0 file pends BOTH version-keyed steps: the chain lifts it to v1 and
	// then to v2 in one run.
	v0Pending := []string{"v0-blocking", "v2-axes"}

	tests := []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{"empty store has nothing pending", map[string]string{}, nil},
		{"all-v2 store has nothing pending", map[string]string{
			"a1--one.md": v2Nib,
			"b2--two.md": v2Nib,
		}, nil},
		{"all-v1 store needs the v2 step", map[string]string{
			"a1--one.md": v1Nib,
			"b2--two.md": v1Nib,
		}, []string{"v2-axes"}},
		{"v0 store needs the v0 step and the v2 step", map[string]string{
			"a1--one.md": v0Nib,
		}, v0Pending},
		{"mixed store needs both version steps", map[string]string{
			"a1--one.md": v0Nib,
			"b2--two.md": v2Nib,
		}, v0Pending},
		{"archived v0 nib still counts", map[string]string{
			"a1--one.md":         v2Nib,
			"archive/z9--old.md": v0Nib,
		}, v0Pending},
		{"version key in the body does not count as versioned", map[string]string{
			// The scan must stop at the closing fence: this file's header has no
			// version key, so it is v0 no matter what the body says.
			"a1--one.md": "---\ntitle: Legacy\nstatus: todo\n---\n\nversion: 2\n",
		}, v0Pending},
		{"non-md files are not probed", map[string]string{
			"notes.txt": "not front matter at all",
		}, nil},
		{"---yaml fence is front matter, so a v0 file behind it is pending", map[string]string{
			// nib.Parse accepts the `---yaml` opening fence, so the scan must
			// read it too or the two disagree on whether this file is a nib.
			"a1--one.md": "---yaml\ntitle: Legacy\nstatus: todo\n---\n\nBody.\n",
		}, v0Pending},

		// YAML trailing comments: the scan must read these headers the way the
		// authoritative parse will, or detect and apply disagree forever — the
		// refusal loop reproduced in review finding #7. Boundary rows bracket
		// the strip rule: a comment needs whitespace before its `#`.
		{"version with trailing comment is not v0", map[string]string{
			"a1--one.md": "---\nversion: 2 # migrated\ntitle: T\nstatus: todo\n---\n\nBody.\n",
		}, nil},
		{"version with tight trailing comment is not v0", map[string]string{
			"a1--one.md": "---\nversion: 2 #c\ntitle: T\nstatus: todo\n---\n\nBody.\n",
		}, nil},
		{"quoted version with trailing comment is not v0", map[string]string{
			"a1--one.md": "---\nversion: \"2\" # c\ntitle: T\nstatus: todo\n---\n\nBody.\n",
		}, nil},
		{"hash without preceding space is part of the value, not a comment", map[string]string{
			// YAML reads `2#nospace` as the scalar "2#nospace" — an invalid
			// int the load will refuse loudly; the scan must agree it is NOT
			// version 2 (v0 keeps the store gated until the file is repaired).
			"a1--one.md": "---\nversion: 2#nospace\ntitle: T\nstatus: todo\n---\n\nBody.\n",
		}, v0Pending},
		{"deferred priority with trailing comment is caught", map[string]string{
			"a1--one.md": "---\nversion: 2\ntitle: T\nstatus: todo\npriority: deferred # legacy\n---\n\nBody.\n",
		}, []string{"priority-deferred"}},
		{"quoted deferred priority with trailing comment is caught", map[string]string{
			"a1--one.md": "---\nversion: 2\ntitle: T\nstatus: todo\npriority: 'deferred' # note\n---\n\nBody.\n",
		}, []string{"priority-deferred"}},
		{"hash inside a quoted value is not a comment", map[string]string{
			// Not deferred, and the quoted ` # ` must not be stripped into one.
			"a1--one.md": "---\nversion: 2\ntitle: T\nstatus: todo\npriority: \"low # x\"\n---\n\nBody.\n",
		}, nil},
		{"version key after an over-64KiB header line is still read", map[string]string{
			// bufio's default 64 KiB token cap used to end the scan mid-header
			// with version 0 extracted; the buffer is raised to the full header
			// cap so scan and parse see the same keys.
			"a1--one.md": "---\nlong_note: " + strings.Repeat("x", 70*1024) + "\nversion: 2\ntitle: T\nstatus: todo\n---\n\nBody.\n",
		}, nil},
		{"version with tab before trailing comment is not v0", map[string]string{
			// YAML opens a comment after a tab just as after a space; the
			// strip rule must too.
			"a1--one.md": "---\nversion: 2\t# migrated\ntitle: T\nstatus: todo\n---\n\nBody.\n",
		}, nil},

		// Whitespace-padded fences: the frontmatter library's line handling is
		// a fixed bytes.TrimSpace, so nib.Parse accepts every padded spelling
		// below — the scan must classify them identically (TrimSpace-equivalence
		// with the fence token) or detect and load disagree on the same file.
		{"padded closing fence ends the header before a body version line", map[string]string{
			// The scan must recognize `---  ` as the close nib.Parse sees:
			// missing it reads the body's unindented `version: 2` as a header
			// key and reports this v0 file as already migrated — silently
			// never migrated.
			"a1--one.md": "---\ntitle: Legacy\nstatus: todo\n---  \n\nversion: 2\n",
		}, v0Pending},
		{"padded closing fence ends the header before a body priority line", map[string]string{
			// The inverse misread: a body line that is itself an unindented
			// `priority: deferred` must not be scanned as a header key — that
			// would report a migrated file as pending forever.
			"a1--one.md": "---\nversion: 2\ntitle: T\nstatus: todo\n--- \n\npriority: deferred\n",
		}, nil},
		{"tab-padded closing fence ends the header", map[string]string{
			"a1--one.md": "---\ntitle: Legacy\nstatus: todo\n---\t\n\nversion: 2\n",
		}, v0Pending},
		{"crlf closing fence with trailing spaces ends the header", map[string]string{
			"a1--one.md": "---\r\ntitle: Legacy\r\nstatus: todo\r\n---  \r\n\r\nversion: 2\r\n",
		}, v0Pending},
		{"padded opening fence is still front matter, so a v0 file behind it is pending", map[string]string{
			"a1--one.md": "---   \ntitle: Legacy\nstatus: todo\n---\n\nBody.\n",
		}, v0Pending},
		{"---yaml opening fence with trailing spaces is still front matter", map[string]string{
			"a1--one.md": "---yaml  \ntitle: Legacy\nstatus: todo\n---\n\nBody.\n",
		}, v0Pending},
		{"---- is not a fence, so the file is not a v0 nib", map[string]string{
			// The trimmed line must EQUAL the fence token: a dash run is a
			// horizontal rule / conflict marker, classified as a diagnostic
			// (nib.Parse refuses it too), never as a version-0 nib.
			"a1--one.md": "----\ntitle: T\nstatus: todo\n---\n\nBody.\n",
		}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsDir := writeStoreFiles(t, tt.files)
			got := pendingNames(t, nibsDir)
			if len(got) != len(tt.want) {
				t.Fatalf("pending = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("pending = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

// TestPendingMigrationsDetectNeverWrites pins detection's read-only contract:
// probing a store that needs migration leaves every file byte-identical with
// its mtime untouched. Detection runs on EVERY command via the pre-run
// refusal, so a write here would be a silent migration by another name.
func TestPendingMigrationsDetectNeverWrites(t *testing.T) {
	nibsDir := writeStoreFiles(t, map[string]string{
		"a1--legacy.md":  "---\ntitle: Legacy\nstatus: todo\nblocking:\n    - b2\n---\n\nBody.\n",
		"b2--target.md":  "---\ntitle: Target\nstatus: todo\n---\n\nBody.\n",
		"c3--current.md": "---\nversion: 2\ntitle: Current\nstatus: todo\n---\n\nBody.\n",
	})

	type fileState struct {
		bytes []byte
		mtime string
	}
	capture := func() map[string]fileState {
		states := map[string]fileState{}
		entries, err := os.ReadDir(storeDataDir(nibsDir))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			path := dataPath(nibsDir, e.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			states[e.Name()] = fileState{bytes: data, mtime: info.ModTime().String()}
		}
		return states
	}

	before := capture()
	if got := pendingNames(t, nibsDir); len(got) == 0 {
		t.Fatal("fixture should have a pending migration, or this pin is vacuous")
	}
	after := capture()

	for name, b := range before {
		a, ok := after[name]
		if !ok {
			t.Fatalf("file %s disappeared during detection", name)
		}
		if string(a.bytes) != string(b.bytes) {
			t.Errorf("detection changed the bytes of %s:\nbefore:\n%s\nafter:\n%s", name, b.bytes, a.bytes)
		}
		if a.mtime != b.mtime {
			t.Errorf("detection changed the mtime of %s: %s -> %s", name, b.mtime, a.mtime)
		}
	}
}

// TestMigratePriorityDeferredStep pins the priority-deferred step: a file
// carrying the legacy `priority: deferred` is rewritten to `low`, and
// detection finds it INDEPENDENTLY of the version key — a current-version file
// hand-edited back to deferred is still caught, refused by normal commands,
// and repaired by migrate.
func TestMigratePriorityDeferredStep(t *testing.T) {
	files := map[string]string{
		// version: 2 already — only the priority is legacy.
		"def1--deferred.md": "---\nversion: 2\ntitle: Set Aside\nstatus: todo\npriority: deferred\n---\n\nBody.\n",
		// Control: untouched by the run.
		"low1--control.md": "---\nversion: 2\ntitle: Control\nstatus: todo\npriority: low\n---\n\nBody.\n",
	}
	nibsDir := setupListCobraTest(t, files)

	// The pre-run refusal names the pending step even though every file is v1.
	_, err := runRootWith(t, "--nibs-path", nibsDir, "list", "-q")
	if err == nil {
		t.Fatal("list on a store with a deferred priority succeeded, want a refusal")
	}
	if !strings.Contains(err.Error(), "priority-deferred") {
		t.Errorf("refusal should name the priority-deferred step, got: %v", err)
	}

	out, err := runRootWith(t, "--nibs-path", nibsDir, "migrate")
	if err != nil {
		t.Fatalf("nibs migrate failed: %v\nout: %s", err, out)
	}

	defDisk, err := os.ReadFile(dataPath(nibsDir, "def1--deferred.md"))
	if err != nil {
		t.Fatalf("reading migrated file: %v", err)
	}
	disk := string(defDisk)
	if strings.Contains(disk, "deferred") {
		t.Errorf("migrated file still contains 'deferred':\n%s", disk)
	}
	if !strings.Contains(disk, "priority: low") {
		t.Errorf("migrated file missing 'priority: low':\n%s", disk)
	}
	if !strings.Contains(disk, "version: 2") {
		t.Errorf("migrated file lost its version:\n%s", disk)
	}

	ctlDisk, err := os.ReadFile(dataPath(nibsDir, "low1--control.md"))
	if err != nil {
		t.Fatalf("reading control file: %v", err)
	}
	if string(ctlDisk) != files["low1--control.md"] {
		t.Errorf("control file was rewritten:\n%s", ctlDisk)
	}

	if _, err := runRootWith(t, "--nibs-path", nibsDir, "list", "-q"); err != nil {
		t.Fatalf("list after migrate failed: %v", err)
	}
}

// TestMigrateCrashResume pins detect-gates-apply idempotency: a store where
// the earlier steps already ran for one file (it is version: 2) while another
// still pends the priority and v2 steps — the on-disk state a crash between
// steps leaves behind — re-runs with ONLY the remaining steps applied. There
// is no run journal to consult; the files themselves say what is left to do.
func TestMigrateCrashResume(t *testing.T) {
	files := map[string]string{
		"aaa1--done.md": "---\nversion: 2\ntitle: Already Migrated\nstatus: todo\nblocked_by:\n    - bbb2\n---\n\nBody A.\n",
		"bbb2--rest.md": "---\nversion: 1\ntitle: Still Deferred\nstatus: todo\npriority: deferred\n---\n\nBody B.\n",
	}
	nibsDir := setupListCobraTest(t, files)

	out, err := runRootWith(t, "--nibs-path", nibsDir, "migrate")
	if err != nil {
		t.Fatalf("nibs migrate failed: %v\nout: %s", err, out)
	}

	if strings.Contains(out, "applying v0-blocking") {
		t.Errorf("v0 step re-applied on a store with no v0 file; detect must gate apply.\nout:\n%s", out)
	}
	if !strings.Contains(out, "applying priority-deferred") {
		t.Errorf("remaining priority step was not applied.\nout:\n%s", out)
	}
	if !strings.Contains(out, "applying v2-axes") {
		t.Errorf("remaining v2 step was not applied.\nout:\n%s", out)
	}

	// The already-converted file is untouched; the remaining one is rewritten.
	aDisk, err := os.ReadFile(dataPath(nibsDir, "aaa1--done.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(aDisk) != files["aaa1--done.md"] {
		t.Errorf("already-migrated file was rewritten:\n%s", aDisk)
	}
	bDisk, err := os.ReadFile(dataPath(nibsDir, "bbb2--rest.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(bDisk), "deferred") || !strings.Contains(string(bDisk), "priority: low") {
		t.Errorf("remaining priority step did not convert the deferred file:\n%s", bDisk)
	}
	if !strings.Contains(string(bDisk), "version: 2") {
		t.Errorf("remaining v2 step did not stamp the file:\n%s", bDisk)
	}
}

// TestMigrateRefusesUnparseableStore pins the fail-loud posture that replaced
// the old deferral machinery: a store that does not load cleanly REFUSES to
// migrate, names the broken file and `nibs check`, and modifies NOTHING — no
// half-migrated store, no silently dropped edges.
func TestMigrateRefusesUnparseableStore(t *testing.T) {
	files := map[string]string{
		"aaa1--blocker.md": "---\ntitle: Blocker\nstatus: todo\nblocking:\n    - bbb2\n---\n\nBody A.\n",
		// Unparseable: duplicate modeled front-matter key (yaml.v3 hard-errors),
		// the real-world shape of a bad merge or hand-edit.
		"bbb2--broken.md": "---\ntitle: First\ntitle: Second\nstatus: todo\n---\n\nBody B.\n",
	}
	nibsDir := setupListCobraTest(t, files)

	_, err := runRootWith(t, "--nibs-path", nibsDir, "migrate")
	if err == nil {
		t.Fatal("migrate on a store with an unparseable nib succeeded, want a refusal")
	}
	if !strings.Contains(err.Error(), "bbb2--broken.md") {
		t.Errorf("refusal should name the unparseable file, got: %v", err)
	}
	if !strings.Contains(err.Error(), "nibs check") {
		t.Errorf("refusal should point at `nibs check`, got: %v", err)
	}

	// Byte-identical pin: the refusal must modify nothing — not even the
	// parseable v0 file, whose migration alone could drop its edge to the
	// broken target.
	for name, before := range files {
		after, err := os.ReadFile(dataPath(nibsDir, name))
		if err != nil {
			t.Fatalf("re-reading %s: %v", name, err)
		}
		if string(after) != before {
			t.Errorf("migrate's refusal modified %s:\nbefore:\n%s\nafter:\n%s", name, before, after)
		}
	}
}

// TestMigrateV2AxesStep pins the v2 step end to end: a v1 store where an epic
// and a bug are parented to a milestone is refused by a normal command; one
// `nibs migrate` moves both onto the assignment axis (milestone: from the
// parent, milestone_order: from the sibling order) and stamps every file
// version: 2; a second run detects nothing and rewrites nothing (convergence).
func TestMigrateV2AxesStep(t *testing.T) {
	files := map[string]string{
		"ms01--rel.md":  "---\nversion: 1\ntitle: Release\nstatus: todo\ntype: milestone\n---\n\nBody.\n",
		"epi1--auth.md": "---\nversion: 1\ntitle: Auth\nstatus: todo\ntype: epic\nparent: ms01\norder: a5\n---\n\nBody.\n",
		"tsk1--sub.md":  "---\nversion: 1\ntitle: Sub\nstatus: todo\ntype: task\nparent: epi1\norder: a0\n---\n\nBody.\n",
	}
	nibsDir := setupMigrateStore(t, files)

	// The pre-run refusal names the pending v2 step.
	_, err := runRootWith(t, "--nibs-path", nibsDir, "list", "-q")
	if err == nil {
		t.Fatal("list on a v1 store succeeded, want a refusal")
	}
	if !strings.Contains(err.Error(), "v2-axes") {
		t.Errorf("refusal should name the v2-axes step, got: %v", err)
	}

	out, err := runRootWith(t, "--nibs-path", nibsDir, "migrate")
	if err != nil {
		t.Fatalf("nibs migrate failed: %v\nout: %s", err, out)
	}

	epiDisk, err := os.ReadFile(dataPath(nibsDir, "epi1--auth.md"))
	if err != nil {
		t.Fatal(err)
	}
	epi := string(epiDisk)
	for _, want := range []string{"milestone: ms01", "milestone_order: a5", "version: 2"} {
		if !strings.Contains(epi, want) {
			t.Errorf("migrated epic missing %q:\n%s", want, epi)
		}
	}
	for _, gone := range []string{"parent:", "order: a5"} {
		if strings.Contains(strings.ReplaceAll(epi, "milestone_order: a5", ""), gone) {
			t.Errorf("migrated epic still carries %q:\n%s", gone, epi)
		}
	}
	tskDisk, err := os.ReadFile(dataPath(nibsDir, "tsk1--sub.md"))
	if err != nil {
		t.Fatal(err)
	}
	tsk := string(tskDisk)
	// The task's epic parent is decomposition, not milestone membership: it
	// stays on the parent axis, version-stamped only.
	for _, want := range []string{"parent: epi1", "order: a0", "version: 2"} {
		if !strings.Contains(tsk, want) {
			t.Errorf("migrated task missing %q:\n%s", want, tsk)
		}
	}
	if strings.Contains(tsk, "milestone") {
		t.Errorf("the task gained an assignment its epic parent never conferred:\n%s", tsk)
	}

	// Convergence: the second run finds nothing and changes nothing.
	after := map[string]string{}
	for name := range files {
		disk, readErr := os.ReadFile(dataPath(nibsDir, name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		after[name] = string(disk)
	}
	out, err = runRootWith(t, "--nibs-path", nibsDir, "migrate")
	if err != nil {
		t.Fatalf("second migrate failed: %v\nout: %s", err, out)
	}
	if !strings.Contains(out, "Store is up to date; no migrations pending.") {
		t.Errorf("second run should report the store up to date, got:\n%s", out)
	}
	for name, before := range after {
		disk, readErr := os.ReadFile(dataPath(nibsDir, name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(disk) != before {
			t.Errorf("second run rewrote %s:\n%s", name, disk)
		}
	}

	// And the normal command now runs.
	if _, err := runRootWith(t, "--nibs-path", nibsDir, "list", "-q", "--all"); err != nil {
		t.Fatalf("list after migrate failed: %v", err)
	}
}

// resetMigrateFlags clears migrateCmd's package-level flag vars and Cobra's
// Changed-state tracking so tests don't pollute each other via the rootCmd
// singleton (mirrors resetCheckFlags).
func resetMigrateFlags() {
	migrateDryRun = false
	migrateAllowDirty = false
	migrateForce = false
	migrateYes = false
	migrateCmd.Flags().Visit(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
}

// setupMigrateStore is setupListCobraTest plus migrate-flag hygiene: the
// rootCmd singleton keeps parsed flag values across Execute calls, so every
// migrate test resets --dry-run/--allow-dirty before and after itself.
func setupMigrateStore(t *testing.T, files map[string]string) string {
	t.Helper()
	t.Cleanup(resetMigrateFlags)
	resetMigrateFlags()
	return setupListCobraTest(t, files)
}

// TestMigrateDryRun pins --dry-run: it lists every pending step with its
// per-file count, exits successfully, and modifies NOTHING — the store still
// refuses normal commands afterwards.
func TestMigrateDryRun(t *testing.T) {
	files := map[string]string{
		"v0a--one.md":   "---\ntitle: One\nstatus: todo\n---\n\nBody.\n",
		"v0b--two.md":   "---\ntitle: Two\nstatus: todo\n---\n\nBody.\n",
		"def1--rest.md": "---\nversion: 1\ntitle: Rest\nstatus: todo\npriority: deferred\n---\n\nBody.\n",
	}
	nibsDir := setupMigrateStore(t, files)

	out, err := runRootWith(t, "--nibs-path", nibsDir, "migrate", "--dry-run")
	if err != nil {
		t.Fatalf("migrate --dry-run failed: %v\nout: %s", err, out)
	}
	if !strings.Contains(out, "v0-blocking") || !strings.Contains(out, "2 file(s)") {
		t.Errorf("dry-run should list the v0 step with its count of 2, got:\n%s", out)
	}
	if !strings.Contains(out, "priority-deferred") || !strings.Contains(out, "1 file(s)") {
		t.Errorf("dry-run should list the priority step with its count of 1, got:\n%s", out)
	}
	if !strings.Contains(out, "v2-axes") || !strings.Contains(out, "3 file(s)") {
		t.Errorf("dry-run should list the v2 step with its count of 3 (every pre-v2 file), got:\n%s", out)
	}

	for name, before := range files {
		after, err := os.ReadFile(dataPath(nibsDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != before {
			t.Errorf("--dry-run modified %s:\n%s", name, after)
		}
	}

	// Still unmigrated: the refusal is intact.
	if _, err := runRootWith(t, "--nibs-path", nibsDir, "list", "-q"); err == nil {
		t.Error("list succeeded after --dry-run; the store must still be pending")
	}
}

// TestMigrateDryRunWarnsWhenRealRunWillRefuse pins --dry-run's preview of the
// pending+problems refusal: the real run refuses outright when pending steps
// coexist with scan problems, so a preview that lists pending counts next to
// an unconnected skip note reads as "the skipped file is harmlessly excluded"
// — and the user is surprised when the real run refuses and applies nothing.
// The preview must say the refusal is coming, naming the files.
func TestMigrateDryRunWarnsWhenRealRunWillRefuse(t *testing.T) {
	files := map[string]string{
		"v0a--one.md": "---\ntitle: One\nstatus: todo\n---\n\nBody.\n",
		"README.md":   "# Store notes\n\nNo front matter here.\n",
	}
	nibsDir := setupMigrateStore(t, files)

	out, err := runRootWith(t, "--nibs-path", nibsDir, "migrate", "--dry-run")
	if err != nil {
		t.Fatalf("--dry-run must preview, not fail: %v\nout: %s", err, out)
	}
	if !strings.Contains(out, "refuse") || !strings.Contains(out, "README.md") {
		t.Errorf("dry-run should warn that the real run will refuse until the named files are repaired or moved, got:\n%s", out)
	}

	// The preview still modifies nothing.
	for name, before := range files {
		after, readErr := os.ReadFile(dataPath(nibsDir, name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(after) != before {
			t.Errorf("--dry-run modified %s:\n%s", name, after)
		}
	}
}

// TestStoreProbeDegradation pins the pre-run probe's degradation contract,
// which must match Core.Load's: ONE unreadable file (a dangling editor-lock
// symlink, a permission-denied file) or one fence-less .md (a README, a
// mid-conflict file) must not hard-fail every command — the loader skips and
// diagnoses per file, and the probe follows suit. Before this contract the
// probe aborted with a raw open error, deadlocking the whole CLI including
// `nibs check`, the diagnostic for exactly this state.
func TestStoreProbeDegradation(t *testing.T) {
	const v2Nib = "---\n# leg-a1\nversion: 2\ntitle: Current\nstatus: todo\n---\n\nBody.\n"

	t.Run("dangling symlink does not brick the CLI", func(t *testing.T) {
		nibsDir := setupListCobraTest(t, map[string]string{
			"cur1--one.md": v2Nib,
		})
		// The emacs lock-file shape: a .md-named symlink to a target that
		// does not exist. Opening it fails; probing must skip, not abort.
		if err := os.Symlink(dataPath(nibsDir, "no-such-target"), dataPath(nibsDir, ".#cur1--one.md")); err != nil {
			testskip.SymlinkUnavailable(t, err)
		}
		out, err := runRootWith(t, "--nibs-path", nibsDir, "list", "-q", "--all")
		if err != nil {
			t.Fatalf("list with a dangling symlink in the store failed: %v\nout: %s", err, out)
		}
	})

	t.Run("fence-less markdown is not counted pending", func(t *testing.T) {
		nibsDir := setupListCobraTest(t, map[string]string{
			"cur1--one.md": v2Nib,
			"README.md":    "# Store notes\n\nNo front matter here.\n",
		})
		// A fence-less file has no version key; it must land in the
		// diagnostics channel, NOT in the v0 bucket — the old classification
		// held every command hostage to a document.
		if got := pendingNames(t, nibsDir); len(got) != 0 {
			t.Errorf("fence-less file counted as pending migration: %v", got)
		}
		if _, err := runRootWith(t, "--nibs-path", nibsDir, "list", "-q", "--all"); err != nil {
			t.Fatalf("list with a fence-less .md in the store failed: %v", err)
		}
	})
}

// TestMigrateFencelessFileNeverRewritten pins the destructive-conversion
// regression: `nibs migrate` must never rewrite a fence-less .md into a nib
// render. With migrations pending it refuses, naming the file; with nothing
// pending it succeeds and names the file in a note.
func TestMigrateFencelessFileNeverRewritten(t *testing.T) {
	const readme = "# Store notes\n\nNo front matter here.\n"

	t.Run("pending store refuses, naming the file", func(t *testing.T) {
		files := map[string]string{
			"v0a--one.md": "---\ntitle: One\nstatus: todo\n---\n\nBody.\n",
			"README.md":   readme,
		}
		nibsDir := setupMigrateStore(t, files)
		out, err := runRootWith(t, "--nibs-path", nibsDir, "migrate")
		if err == nil {
			t.Fatalf("migrate with a fence-less file and pending steps succeeded, want a refusal\nout: %s", out)
		}
		if !strings.Contains(err.Error(), "README.md") {
			t.Errorf("refusal should name the fence-less file, got: %v", err)
		}
		for name, before := range files {
			after, readErr := os.ReadFile(dataPath(nibsDir, name))
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(after) != before {
				t.Errorf("refusal modified %s:\nbefore:\n%s\nafter:\n%s", name, before, after)
			}
		}
	})

	t.Run("up-to-date store notes the file and leaves it alone", func(t *testing.T) {
		files := map[string]string{
			"cur1--one.md": "---\nversion: 2\ntitle: One\nstatus: todo\n---\n\nBody.\n",
			"README.md":    readme,
		}
		nibsDir := setupMigrateStore(t, files)
		out, err := runRootWith(t, "--nibs-path", nibsDir, "migrate")
		if err != nil {
			t.Fatalf("migrate failed: %v\nout: %s", err, out)
		}
		if !strings.Contains(out, "README.md") {
			t.Errorf("run should note the skipped fence-less file by name, got:\n%s", out)
		}
		after, readErr := os.ReadFile(dataPath(nibsDir, "README.md"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(after) != readme {
			t.Errorf("migrate rewrote the fence-less file:\n%s", after)
		}
	})
}

// TestFencelessFileClassificationAgrees pins the ONE classification of a
// fence-less .md across every surface. The split it retires: scanStore called
// the file "not a nib file" while Core.Load parsed it into an empty v0 nib —
// so it appeared in list, `nibs check` reported all clear, and an ungated
// `nibs set` rewrote the document into a nib render (arming the v0 gate,
// after which migrate finished converting it). Now it is a load diagnostic
// everywhere: reported by check, absent from list, unreachable by set.
func TestFencelessFileClassificationAgrees(t *testing.T) {
	const readme = "# Store notes\n\nNo front matter here.\n"
	files := map[string]string{
		"cur1--one.md": "---\nversion: 2\ntitle: One\nstatus: todo\n---\n\nBody.\n",
		"README.md":    readme,
	}

	t.Run("list does not surface it", func(t *testing.T) {
		nibsDir := setupListCobraTest(t, files)
		out, err := runRootWith(t, "--nibs-path", nibsDir, "list", "-q", "--all")
		if err != nil {
			t.Fatalf("list failed: %v\nout: %s", err, out)
		}
		if strings.Contains(out, "README") {
			t.Errorf("fence-less file appears as a phantom nib row:\n%s", out)
		}
	})

	t.Run("set refuses it as not found and rewrites nothing", func(t *testing.T) {
		nibsDir := setupListCobraTest(t, files)
		t.Cleanup(resetSetFlags)
		resetSetFlags()
		_, err := runRootWith(t, "--nibs-path", nibsDir, "set", "README", "-p", "high")
		if err == nil {
			t.Fatal("set on a fence-less document succeeded; the document must be unreachable")
		}
		after, readErr := os.ReadFile(dataPath(nibsDir, "README.md"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(after) != readme {
			t.Errorf("set rewrote the fence-less document:\n%s", after)
		}
	})

	t.Run("check reports it with the no-front-matter reason", func(t *testing.T) {
		app, _ := setupCheckTest(t, files)
		var runErr error
		out := captureStdout(t, func() { _, runErr = runCheck(app) })
		if runErr != nil {
			t.Fatalf("runCheck error = %v", runErr)
		}
		if !strings.Contains(out, "README.md") || !strings.Contains(out, "no front matter") {
			t.Errorf("check should name the fence-less file with its reason, got:\n%s", out)
		}
	})
}

// TestMigrateUpToDateStore pins the no-op path end to end: on an all-v1 store
// both `nibs migrate` and `nibs migrate --dry-run` succeed, report "up to
// date", and modify nothing.
func TestMigrateUpToDateStore(t *testing.T) {
	files := map[string]string{
		"cur1--one.md": "---\nversion: 2\ntitle: One\nstatus: todo\n---\n\nBody.\n",
		"cur2--two.md": "---\nversion: 2\ntitle: Two\nstatus: todo\npriority: low\n---\n\nBody.\n",
	}
	nibsDir := setupMigrateStore(t, files)

	for _, args := range [][]string{
		{"--nibs-path", nibsDir, "migrate"},
		{"--nibs-path", nibsDir, "migrate", "--dry-run"},
	} {
		out, err := runRootWith(t, args...)
		if err != nil {
			t.Fatalf("%v failed: %v\nout: %s", args, err, out)
		}
		if !strings.Contains(out, "Store is up to date; no migrations pending.") {
			t.Errorf("%v should report the store up to date, got:\n%s", args, out)
		}
	}
	for name, before := range files {
		after, err := os.ReadFile(dataPath(nibsDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != before {
			t.Errorf("no-op migrate modified %s:\n%s", name, after)
		}
	}
}

// TestPendingMigrationsSkipsDotDirectories pins the scan's walk scope: files
// under dot directories (a store repo's .git most importantly) are not store
// content and must be neither probed nor counted — a v0-looking blob inside
// .git must not hold the whole CLI hostage.
func TestPendingMigrationsSkipsDotDirectories(t *testing.T) {
	nibsDir := writeStoreFiles(t, map[string]string{
		"cur1--one.md":       "---\nversion: 2\ntitle: One\nstatus: todo\n---\n\nBody.\n",
		".git/notes/x.md":    "---\ntitle: Not A Nib\nstatus: todo\n---\n\nLooks v0.\n",
		".obsidian/cache.md": "no front matter at all",
	})
	if got := pendingNames(t, nibsDir); len(got) != 0 {
		t.Errorf("dot-directory contents were probed as store files: pending = %v, want none", got)
	}
}

// TestMigrateDotDirectoryFilesStayUntouched is the composition pin for the
// walk-scope agreement between the migration scans and Core.Load: BOTH must
// use the shared store-content definition (nibcore.WalkStoreFiles), or a file
// the scan's problems gate never sees still loads as a nib and `nibs migrate`
// rewrites it. The behavior delta of the shared definition is dot-directory
// .md FILES only: Load never matched non-.md files anywhere (so `.nibs/.git`
// blobs were already invisible to it), and the scan skipped dot directories
// wholesale — so this fixture plants .md files (one fence-less, one
// v0-shaped) inside dot directories and pins that migrate converts only the
// genuine store file, leaves the dot-directory files byte-identical, and no
// query ever surfaces them.
func TestMigrateDotDirectoryFilesStayUntouched(t *testing.T) {
	files := map[string]string{
		"v0a--one.md":                 "---\ntitle: One\nstatus: todo\n---\n\nBody.\n",
		".obsidian/plugins/README.md": "# Plugin readme\n\nNo front matter here.\n",
		".trash/zz9--note.md":         "---\ntitle: Trashed Note\nstatus: todo\n---\n\nLooks v0.\n",
	}
	// setupListCobraTest does not create subdirectories, so plant only the
	// top-level file through it and write the dot-directory files by hand.
	nibsDir := setupMigrateStore(t, map[string]string{"v0a--one.md": files["v0a--one.md"]})
	for name, content := range files {
		if name == "v0a--one.md" {
			continue
		}
		path := filepath.Join(nibsDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	out, err := runRootWith(t, "--nibs-path", nibsDir, "migrate")
	if err != nil {
		t.Fatalf("migrate failed: %v\nout: %s", err, out)
	}
	if !strings.Contains(out, "migrated 1 nib(s)") {
		t.Errorf("migrate should convert exactly the one genuine v0 file, got:\n%s", out)
	}

	// Dot-directory files are not store content: byte-identical after the run.
	for _, name := range []string{".obsidian/plugins/README.md", ".trash/zz9--note.md"} {
		after, readErr := os.ReadFile(filepath.Join(nibsDir, filepath.FromSlash(name)))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(after) != files[name] {
			t.Errorf("migrate rewrote dot-directory file %s:\nbefore:\n%s\nafter:\n%s", name, files[name], after)
		}
	}

	// The genuine store file was converted.
	converted, err := os.ReadFile(dataPath(nibsDir, "v0a--one.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(converted), "version: 2") {
		t.Errorf("genuine v0 file was not converted:\n%s", converted)
	}

	// And no query surfaces the dot-directory files as nibs.
	out, err = runRootWith(t, "--nibs-path", nibsDir, "list", "-q", "--all")
	if err != nil {
		t.Fatalf("list after migrate failed: %v\nout: %s", err, out)
	}
	if strings.Contains(out, "zz9") || strings.Contains(out, "README") {
		t.Errorf("list surfaces dot-directory files as nibs:\n%s", out)
	}
}

// TestMigrateDirtyGitRefusal pins the git safety net: when the store directory
// is a git repository with uncommitted changes, migrate refuses (git is the
// rollback, so the pre-migration state must be committed) unless --allow-dirty
// overrides; a non-repo store proceeds with a printed backup suggestion.
func TestMigrateDirtyGitRefusal(t *testing.T) {
	v0Files := map[string]string{
		"v0a--one.md": "---\ntitle: One\nstatus: todo\n---\n\nBody.\n",
	}

	t.Run("dirty store repo refuses without --allow-dirty", func(t *testing.T) {
		nibsDir := setupMigrateStore(t, v0Files)
		gitOut, err := exec.Command("git", "-C", nibsDir, "init", "-q").CombinedOutput()
		if err != nil {
			t.Skipf("git init failed (git unavailable?): %v\n%s", err, gitOut)
		}
		// Untracked files count as dirty in --porcelain, which is exactly the
		// state that would be unrecoverable after a bad migration.

		out, err := runRootWith(t, "--nibs-path", nibsDir, "migrate")
		if err == nil {
			t.Fatalf("migrate on a dirty store repo succeeded, want a refusal\nout: %s", out)
		}
		if !strings.Contains(err.Error(), "--allow-dirty") {
			t.Errorf("refusal should mention the --allow-dirty override, got: %v", err)
		}
		before := v0Files["v0a--one.md"]
		after, readErr := os.ReadFile(dataPath(nibsDir, "v0a--one.md"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(after) != before {
			t.Errorf("dirty-git refusal modified the store:\n%s", after)
		}

		// --allow-dirty overrides and the migration runs.
		out, err = runRootWith(t, "--nibs-path", nibsDir, "migrate", "--allow-dirty")
		if err != nil {
			t.Fatalf("migrate --allow-dirty failed: %v\nout: %s", err, out)
		}
		after, readErr = os.ReadFile(dataPath(nibsDir, "v0a--one.md"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !strings.Contains(string(after), "version: 2") {
			t.Errorf("--allow-dirty run did not migrate the store:\n%s", after)
		}
		// The store IS a git repo, so the run must end with the commit hint —
		// the positive twin of the ignored-store subtest's absence assertion.
		if !strings.Contains(out, "commit the store's changes") {
			t.Errorf("--allow-dirty run in a store repo should print the post-run commit hint, got:\n%s", out)
		}
	})

	t.Run("non-repo store proceeds with a backup suggestion", func(t *testing.T) {
		nibsDir := setupMigrateStore(t, v0Files)
		out, err := runRootWith(t, "--nibs-path", nibsDir, "migrate")
		if err != nil {
			t.Fatalf("migrate on a non-repo store failed: %v\nout: %s", err, out)
		}
		if !strings.Contains(out, "not protected by git") {
			t.Errorf("non-repo run should print a backup suggestion, got:\n%s", out)
		}
	})

	t.Run("store gitignored by an enclosing repo gets the backup suggestion, not false safety", func(t *testing.T) {
		// The foreseeable external-user layout: .nibs/ inside a project repo
		// whose .gitignore excludes it, with no nested `git init`. The
		// enclosing repo can neither track nor restore the store (`git add`
		// refuses ignored paths), so it must NOT count as a rollback net: the
		// run proceeds like a non-repo store, with the backup suggestion and
		// without the post-run commit hint.
		nibsDir := setupMigrateStore(t, v0Files)
		projectDir := filepath.Dir(nibsDir)
		gitOut, err := exec.Command("git", "-C", projectDir, "init", "-q").CombinedOutput()
		if err != nil {
			t.Skipf("git init failed (git unavailable?): %v\n%s", err, gitOut)
		}
		if err := os.WriteFile(filepath.Join(projectDir, ".gitignore"), []byte(".nibs/\n"), 0644); err != nil {
			t.Fatal(err)
		}

		out, err := runRootWith(t, "--nibs-path", nibsDir, "migrate")
		if err != nil {
			t.Fatalf("migrate on an ignored store failed: %v\nout: %s", err, out)
		}
		if !strings.Contains(out, "not protected by git") {
			t.Errorf("ignored-store run should print the backup suggestion, got:\n%s", out)
		}
		if strings.Contains(out, "commit the store's changes") {
			t.Errorf("ignored-store run must not suggest committing (git add refuses ignored paths), got:\n%s", out)
		}
	})

	t.Run("enclosing repo tracking the store refuses when dirty", func(t *testing.T) {
		// Same layout minus the gitignore: the enclosing repo genuinely covers
		// the store, the untracked v0 file makes it dirty, and the refusal
		// fires exactly as for a nested store repo.
		nibsDir := setupMigrateStore(t, v0Files)
		projectDir := filepath.Dir(nibsDir)
		gitOut, err := exec.Command("git", "-C", projectDir, "init", "-q").CombinedOutput()
		if err != nil {
			t.Skipf("git init failed (git unavailable?): %v\n%s", err, gitOut)
		}

		_, err = runRootWith(t, "--nibs-path", nibsDir, "migrate")
		if err == nil {
			t.Fatal("migrate on a store dirty in its enclosing repo succeeded, want a refusal")
		}
		if !strings.Contains(err.Error(), "--allow-dirty") {
			t.Errorf("refusal should mention the --allow-dirty override, got: %v", err)
		}
	})

	t.Run("a genuine git failure warns and proceeds with the backup suggestion", func(t *testing.T) {
		nibsDir := setupMigrateStore(t, v0Files)
		orig := storeGitStateFn
		storeGitStateFn = func(string) (bool, bool, error) {
			return false, true, errors.New("simulated git failure")
		}
		t.Cleanup(func() { storeGitStateFn = orig })

		out, err := runRootWith(t, "--nibs-path", nibsDir, "migrate")
		if err != nil {
			t.Fatalf("migrate with a failing git probe should proceed, got: %v\nout: %s", err, out)
		}
		if !strings.Contains(out, "could not determine the store's git state") {
			t.Errorf("a genuine git failure must be surfaced as a warning, got:\n%s", out)
		}
		if !strings.Contains(out, "not protected by git") {
			t.Errorf("after a git failure the run should fall back to the backup suggestion, got:\n%s", out)
		}
	})
}

// TestRunMigrationsHoldsStoreLock pins the concurrency contract: runMigrations
// holds the store write lock for the WHOLE run, so a concurrent
// AcquireStoreLock (a serve process, another CLI) blocks until the run
// finishes. A probe step parks mid-apply while a second goroutine tries to
// take the lock; the acquisition must not complete until the run does.
func TestRunMigrationsHoldsStoreLock(t *testing.T) {
	origSteps := migrationSteps
	t.Cleanup(func() { migrationSteps = origSteps })

	entered := make(chan struct{})
	unpark := make(chan struct{})
	migrationSteps = []migrationStep{{
		name:  "probe",
		title: "park mid-apply so the test can contend for the lock",
		pred:  func(h fmHeader) bool { return h.priority == "probe-pending" },
		apply: func(env *migrateEnv, _ *nibcore.StoreLock, _ logf) error {
			close(entered)
			<-unpark
			// Clear our own detection marker so the engine's apply-detect
			// post-condition sees an honestly-completed step.
			return os.WriteFile(dataPath(env.nibsRoot, "a1--probe.md"),
				[]byte("---\nversion: 1\ntitle: Probe\nstatus: todo\n---\n\nBody.\n"), 0644)
		},
	}}

	// One store file carrying the probe marker so detection fires (detection
	// is per-file, so an empty store pends nothing).
	nibsDir := writeStoreFiles(t, map[string]string{
		"a1--probe.md": "---\nversion: 1\ntitle: Probe\nstatus: todo\npriority: probe-pending\n---\n\nBody.\n",
	})
	env := newMigrateEnv(nibsDir)

	runDone := make(chan error, 1)
	go func() { runDone <- runMigrations(env, func(string, ...any) {}) }()
	<-entered

	// While apply is parked, a concurrent AcquireStoreLock must stay blocked
	// (flock treats a second descriptor in the same process as a contender, so
	// this in-process probe exercises the real exclusion).
	acquired := make(chan struct{})
	go func() {
		lock, err := nibcore.AcquireStoreLock(nibsDir)
		if err == nil {
			_ = lock.Release()
		}
		close(acquired)
	}()

	select {
	case <-acquired:
		t.Fatal("concurrent AcquireStoreLock succeeded while a migration run held the store lock")
	case <-time.After(150 * time.Millisecond):
		// Still blocked mid-run — the lock is held.
	}

	close(unpark)
	if err := <-runDone; err != nil {
		t.Fatalf("runMigrations: %v", err)
	}
	select {
	case <-acquired:
		// Acquired promptly after the run released — the lock is not leaked.
	case <-time.After(2 * time.Second):
		t.Fatal("store lock never released after the migration run finished")
	}
}

// TestMigrateNewerStoreRefusal pins the forward-compatibility guard: a store
// carrying any file with a format version above nib.CurrentVersion was written
// by a newer nibs, and BOTH surfaces — the pre-run refusal on a normal command
// and the migrate command itself — refuse to touch it, telling the user to
// upgrade nibs instead. Nothing is modified.
func TestMigrateNewerStoreRefusal(t *testing.T) {
	files := map[string]string{
		"fut1--future.md": "---\nversion: 99\ntitle: From The Future\nstatus: todo\n---\n\nBody.\n",
		"cur1--now.md":    "---\nversion: 2\ntitle: Current\nstatus: todo\n---\n\nBody.\n",
	}
	nibsDir := setupListCobraTest(t, files)

	assertNewerRefusal := func(surface string, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s on a newer-version store succeeded, want a refusal", surface)
		}
		if !strings.Contains(err.Error(), "newer nibs") || !strings.Contains(err.Error(), "upgrade nibs") {
			t.Errorf("%s refusal should say the store was written by a newer nibs and to upgrade, got: %v", surface, err)
		}
		if !strings.Contains(err.Error(), "fut1--future.md") {
			t.Errorf("%s refusal should name the offending file, got: %v", surface, err)
		}
	}

	_, err := runRootWith(t, "--nibs-path", nibsDir, "list", "-q")
	assertNewerRefusal("list (pre-run check)", err)

	_, err = runRootWith(t, "--nibs-path", nibsDir, "migrate")
	assertNewerRefusal("migrate", err)

	for name, before := range files {
		after, err := os.ReadFile(dataPath(nibsDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != before {
			t.Errorf("newer-store refusal modified %s:\n%s", name, after)
		}
	}
}

// TestNewerStoreRefusalNamesCoexistingProblems pins that the newer-store
// refusal is raised AFTER the walk completes, carrying every other condition
// the scan collected: a store with both a newer-format file and an
// unclassifiable file must surface both in one refusal, not hide the second
// behind the first until the user repairs their way to it.
func TestNewerStoreRefusalNamesCoexistingProblems(t *testing.T) {
	nibsDir := setupListCobraTest(t, map[string]string{
		"cur1--now.md":    "---\nversion: 2\ntitle: Current\nstatus: todo\n---\n\nBody.\n",
		"fut1--future.md": "---\nversion: 99\ntitle: Future\nstatus: todo\n---\n\nBody.\n",
		"README.md":       "# Store notes\n\nNo front matter here.\n",
	})
	_, err := runRootWith(t, "--nibs-path", nibsDir, "list", "-q")
	if err == nil {
		t.Fatal("list on a newer-version store succeeded, want a refusal")
	}
	for _, want := range []string{"upgrade nibs", "fut1--future.md", "README.md"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal should carry %q, got: %v", want, err)
		}
	}
}

// TestMigrateCommentedVersionHeader is the end-to-end regression for the
// scanner/YAML divergence loop (review finding #7, reproduced live): a legal
// YAML trailing comment on the version line used to scan as v0 forever —
// every command refused, while `nibs migrate` found nothing to convert and
// printed "Migration complete". The store must instead be treated as what
// YAML says it is: up to date and usable.
func TestMigrateCommentedVersionHeader(t *testing.T) {
	files := map[string]string{
		"a1--one.md": "---\nversion: 2 # migrated\ntitle: One\nstatus: todo\n---\n\nBody.\n",
	}
	nibsDir := setupMigrateStore(t, files)

	out, err := runRootWith(t, "--nibs-path", nibsDir, "migrate")
	if err != nil {
		t.Fatalf("migrate on a commented-version store failed: %v\nout: %s", err, out)
	}
	if !strings.Contains(out, "Store is up to date; no migrations pending.") {
		t.Errorf("migrate should find nothing pending, got:\n%s", out)
	}
	// The loop-breaker assertion: a normal command must run afterwards.
	if _, err := runRootWith(t, "--nibs-path", nibsDir, "list", "-q"); err != nil {
		t.Fatalf("list still refused after migrate on a commented-version store: %v", err)
	}
}

// TestRunMigrationsPostConditionFailsLoud pins the engine's apply-detect
// agreement post-condition: when an applied step's detection STILL fires after
// its apply ran (the header scan disagrees with the parsed store — a scanner
// bug, or a header YAML reads differently than the scan), runMigrations must
// fail loudly naming the disagreeing files instead of printing success and
// leaving every other command refusing forever.
func TestRunMigrationsPostConditionFailsLoud(t *testing.T) {
	origSteps := migrationSteps
	t.Cleanup(func() { migrationSteps = origSteps })

	// A probe step whose apply deliberately does NOT clear its own detection.
	migrationSteps = []migrationStep{{
		name:  "probe-disagree",
		title: "fire on every file and repair nothing",
		pred:  func(fmHeader) bool { return true },
		apply: func(*migrateEnv, *nibcore.StoreLock, logf) error { return nil },
	}}

	nibsDir := writeStoreFiles(t, map[string]string{
		"a1--stuck.md": "---\nversion: 1\ntitle: Stuck\nstatus: todo\n---\n\nBody.\n",
	})
	err := runMigrations(newMigrateEnv(nibsDir), func(string, ...any) {})
	if err == nil {
		t.Fatal("runMigrations reported success while an applied step's detection still fires")
	}
	if !strings.Contains(err.Error(), "probe-disagree") {
		t.Errorf("post-condition failure should name the step, got: %v", err)
	}
	if !strings.Contains(err.Error(), "a1--stuck.md") {
		t.Errorf("post-condition failure should name the disagreeing file, got: %v", err)
	}
}

// TestCheckRunsOnPendingStore pins the diagnostic escape hatch in the pre-run
// migration gate: plain `nibs check` (read-only) must run on a store with
// pending migrations — it is the diagnostic migrate's own unclean-store
// refusal points at, so gating it would trap the user in a migrate→check→
// migrate circle — while `check --fix` (a writer) stays refused like every
// other mutating command.
func TestCheckRunsOnPendingStore(t *testing.T) {
	t.Cleanup(resetCheckFlags)
	resetCheckFlags()
	nibsDir := setupListCobraTest(t, v0MigrateFixture())

	// Sanity: the store is pending (a normal command refuses).
	if _, err := runRootWith(t, "--nibs-path", nibsDir, "list", "-q"); err == nil {
		t.Fatal("list on a v0 store succeeded; fixture should be pending or this test is vacuous")
	}

	// Plain check gets past the gate — and then reports the pending migration
	// rather than certifying a store no other command will touch. (The report
	// is driven directly because RunE exits the process on a non-empty one.)
	app := checkAppPastTheGate(t, nibsDir)
	var runErr error
	out := captureStdout(t, func() { _, runErr = runCheck(app) })
	if runErr != nil {
		t.Fatalf("runCheck on a pending store: %v", runErr)
	}
	if !strings.Contains(out, "v0-blocking") {
		t.Errorf("check should name the pending migration step, got:\n%s", out)
	}

	// check --fix writes, so it stays behind the gate.
	resetRootPersistentFlags()
	resetCheckFlags()
	_, err := runRootWith(t, "--nibs-path", nibsDir, "check", "--fix")
	if err == nil {
		t.Fatal("`nibs check --fix` on a pending store succeeded, want the migration refusal")
	}
	if !strings.Contains(err.Error(), "nibs migrate") {
		t.Errorf("check --fix refusal should name `nibs migrate`, got: %v", err)
	}
}

// TestMigrateRefusalsAreCoded pins the CLI error-code contract for migrate's
// RunE-level refusals (scripted agents key on exit codes): the dirty-git and
// newer-store refusals are VALIDATION (exit 2), a store that fails to load for
// a step is FILE_ERROR (exit 5) — matching upgrade.go and config.go. The
// shared PersistentPreRunE gate's refusals stay uncoded (out of scope), pinned
// here so a future "consistency" sweep does not code them by accident without
// deciding to.
func TestMigrateRefusalsAreCoded(t *testing.T) {
	codeOf := func(t *testing.T, err error) string {
		t.Helper()
		if err == nil {
			t.Fatal("want a refusal error, got nil")
		}
		var ce *output.CodedError
		if !errors.As(err, &ce) {
			t.Fatalf("refusal is uncoded (%T): %v", err, err)
		}
		return ce.Code
	}

	t.Run("newer-store refusal from migrate is VALIDATION", func(t *testing.T) {
		nibsDir := setupMigrateStore(t, map[string]string{
			"fut1--future.md": "---\nversion: 99\ntitle: Future\nstatus: todo\n---\n\nBody.\n",
		})
		_, err := runRootWith(t, "--nibs-path", nibsDir, "migrate")
		if got := codeOf(t, err); got != output.ErrValidation {
			t.Errorf("newer-store code = %q, want %q", got, output.ErrValidation)
		}

		// The same store through a normal command hits the shared gate, whose
		// refusals deliberately stay uncoded.
		_, err = runRootWith(t, "--nibs-path", nibsDir, "list", "-q")
		var ce *output.CodedError
		if err == nil || errors.As(err, &ce) {
			t.Errorf("pre-run gate refusal should stay uncoded, got: %v", err)
		}
	})

	t.Run("dirty-git refusal is VALIDATION", func(t *testing.T) {
		nibsDir := setupMigrateStore(t, map[string]string{
			"v0a--one.md": "---\ntitle: One\nstatus: todo\n---\n\nBody.\n",
		})
		gitOut, err := exec.Command("git", "-C", nibsDir, "init", "-q").CombinedOutput()
		if err != nil {
			t.Skipf("git init failed (git unavailable?): %v\n%s", err, gitOut)
		}
		_, runErr := runRootWith(t, "--nibs-path", nibsDir, "migrate")
		if got := codeOf(t, runErr); got != output.ErrValidation {
			t.Errorf("dirty-git code = %q, want %q", got, output.ErrValidation)
		}
	})

	t.Run("unclean-store load failure is FILE_ERROR", func(t *testing.T) {
		nibsDir := setupMigrateStore(t, map[string]string{
			"v0a--one.md":     "---\ntitle: One\nstatus: todo\n---\n\nBody.\n",
			"bbb2--broken.md": "---\ntitle: First\ntitle: Second\nstatus: todo\n---\n\nBody.\n",
		})
		_, err := runRootWith(t, "--nibs-path", nibsDir, "migrate")
		if got := codeOf(t, err); got != output.ErrFileError {
			t.Errorf("unclean-store code = %q, want %q", got, output.ErrFileError)
		}
	})
}

// TestMigrateTracer_V0StoreRefusedThenMigrated is the end-to-end tracer for the
// migration engine: a v0 store is REFUSED by a normal command with an error
// naming `nibs migrate`; running `nibs migrate` converts it (blocking →
// blocked_by inversion, then the v2 step's stamp); the normal command then
// succeeds.
func TestMigrateTracer_V0StoreRefusedThenMigrated(t *testing.T) {
	nibsDir := setupListCobraTest(t, v0MigrateFixture())

	// 1. A normal command refuses to run on the unmigrated store, and the
	// error tells the user what to do about it.
	_, err := runRootWith(t, "--nibs-path", nibsDir, "list", "-q")
	if err == nil {
		t.Fatal("list on a v0 store succeeded, want a refusal naming `nibs migrate`")
	}
	if !strings.Contains(err.Error(), "nibs migrate") {
		t.Fatalf("refusal error should name `nibs migrate`, got: %v", err)
	}

	// 2. `nibs migrate` converts the store.
	out, err := runRootWith(t, "--nibs-path", nibsDir, "migrate")
	if err != nil {
		t.Fatalf("nibs migrate failed: %v\nout: %s", err, out)
	}

	// 3. On disk: the blocker is v1 with its `blocking:` inverted away, and the
	// target carries the transferred blocked_by plus the version stamp.
	aBytes, err := os.ReadFile(dataPath(nibsDir, "aaa1--blocker.md"))
	if err != nil {
		t.Fatalf("reading migrated blocker file: %v", err)
	}
	aDisk := string(aBytes)
	if !strings.Contains(aDisk, "version: 2") {
		t.Errorf("blocker file missing `version: 2` after migrate:\n%s", aDisk)
	}
	if strings.Contains(aDisk, "blocking:") {
		t.Errorf("blocker file still carries `blocking:` after migrate:\n%s", aDisk)
	}

	bBytes, err := os.ReadFile(dataPath(nibsDir, "bbb2--blocked.md"))
	if err != nil {
		t.Fatalf("reading migrated target file: %v", err)
	}
	bDisk := string(bBytes)
	if !strings.Contains(bDisk, "version: 2") {
		t.Errorf("target file missing `version: 2` after migrate:\n%s", bDisk)
	}
	if !strings.Contains(bDisk, "blocked_by:") || !strings.Contains(bDisk, "aaa1") {
		t.Errorf("target file missing transferred `blocked_by: [aaa1]` after migrate:\n%s", bDisk)
	}

	// 4. The normal command now runs.
	out, err = runRootWith(t, "--nibs-path", nibsDir, "list", "-q", "--all")
	if err != nil {
		t.Fatalf("list after migrate failed: %v\nout: %s", err, out)
	}
	if !strings.Contains(out, "aaa1") || !strings.Contains(out, "bbb2") {
		t.Errorf("list after migrate should show both nibs, got:\n%s", out)
	}
}
