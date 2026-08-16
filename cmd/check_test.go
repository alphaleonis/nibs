package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/spf13/pflag"
)

// Fixture bodies for the check tests. Ids come from the filenames.
const (
	chkValidNib = `---
version: 1
title: Good
status: todo
type: task
priority: normal
---

Body.
`

	// Duplicate MODELED front-matter key — yaml.v3 hard-errors on it, which is
	// the real-world shape of an unparseable nib (bad merge, hand-edit).
	chkUnparseableNib = `---
version: 1
title: First Title
title: Second Title
status: todo
---

Body.
`

	// Parent names a nib that does not exist: a broken link, which --fix CAN
	// repair. Present so the --fix assertions distinguish "left alone because
	// nothing was fixable" from "left alone because it is not auto-fixable".
	chkBrokenLinkNib = `---
version: 1
title: Dangling parent
status: todo
type: task
priority: normal
parent: chk-nope9
---

Body.
`
)

// resetCheckFlags clears checkCmd's package-level flag vars and Cobra's
// Changed-state tracking so tests don't pollute each other via the rootCmd
// singleton.
func resetCheckFlags() {
	checkJSON = false
	checkFix = false
	checkCmd.Flags().Visit(func(f *pflag.Flag) {
		f.Changed = false
	})
}

// setupCheckTest writes nib files into a fresh .nibs dir, loads a Core over it
// and returns the App plus the .nibs path.
//
// The tests drive runCheck directly rather than rootCmd.Execute: checkCmd's
// RunE calls os.Exit(1) whenever the report is non-empty, which would take the
// test binary down with it — and every case here is deliberately non-empty.
func setupCheckTest(t *testing.T, files map[string]string) (*App, string) {
	t.Helper()
	t.Cleanup(resetCheckFlags)
	resetCheckFlags()

	nibsDir := filepath.Join(t.TempDir(), ".nibs")
	if err := os.MkdirAll(storeDataDir(nibsDir), 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(dataPath(nibsDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	core := nibcore.New(nibsDir, config.Default())
	core.SetWarnWriter(nil) // the point of these tests is the report, not the stderr line
	if err := core.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return &App{Core: core}, nibsDir
}

// loadDiagnosticFiles is the fixture both load-time conditions need: one file
// that fails to parse (so its nib is missing from every query) and two files
// claiming one id (so one silently shadows the other).
func loadDiagnosticFiles() map[string]string {
	return map[string]string{
		"chk-good1--ok.md":     chkValidNib,
		"chk-bad1--broken.md":  chkUnparseableNib,
		"chk-dup1--alpha.md":   chkValidNib,
		"chk-dup1--beta.md":    chkValidNib,
		"chk-link1--broken.md": chkBrokenLinkNib,
	}
}

// TestCheckReportsInvalidEnumValues pins the field-integrity finding end to
// end: a loaded nib carrying an out-of-enum value (here the legacy
// `priority: deferred`) is reported by `nibs check` in text and counted as an
// issue, in --json it rides the invalid_enums array, and --fix does NOT touch
// it (rewriting is `nibs migrate`'s job for known legacy values).
func TestCheckReportsInvalidEnumValues(t *testing.T) {
	files := map[string]string{
		"chk-good1--ok.md": chkValidNib,
		"chk-leg1--old.md": "---\nversion: 1\ntitle: Legacy\nstatus: todo\ntype: task\npriority: deferred\n---\n\nBody.\n",
	}

	t.Run("text report names the nib and counts the issue", func(t *testing.T) {
		app, _ := setupCheckTest(t, files)
		var total int
		var runErr error
		out := captureStdout(t, func() { total, runErr = runCheck(app) })
		if runErr != nil {
			t.Fatalf("runCheck error = %v", runErr)
		}
		if total != 1 {
			t.Errorf("total issues = %d, want 1", total)
		}
		if !strings.Contains(out, "chk-leg1") || !strings.Contains(out, "deferred") {
			t.Errorf("report should name the nib and value, got:\n%s", out)
		}
	})

	t.Run("json envelope carries invalid_enums", func(t *testing.T) {
		app, _ := setupCheckTest(t, files)
		checkJSON = true
		var runErr error
		out := captureStdout(t, func() { _, runErr = runCheck(app) })
		if runErr != nil {
			t.Fatalf("runCheck error = %v", runErr)
		}
		var got checkResult
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("unmarshal: %v\noutput: %s", err, out)
		}
		if got.Success {
			t.Error("success = true; want false with an out-of-enum value")
		}
		if got.NibIssues == nil || len(got.NibIssues.InvalidEnums) != 1 {
			t.Fatalf("invalid_enums = %+v, want exactly 1 entry", got.NibIssues)
		}
		if id := got.NibIssues.InvalidEnums[0].NibID; id != "chk-leg1" {
			t.Errorf("invalid_enums[0].nib_id = %q, want %q", id, "chk-leg1")
		}
	})

	t.Run("--fix leaves the value alone", func(t *testing.T) {
		app, nibsDir := setupCheckTest(t, files)
		checkFix = true
		var runErr error
		_ = captureStdout(t, func() { _, runErr = runCheck(app) })
		if runErr != nil {
			t.Fatalf("runCheck error = %v", runErr)
		}
		raw, err := os.ReadFile(dataPath(nibsDir, "chk-leg1--old.md"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "priority: deferred") {
			t.Errorf("--fix rewrote the out-of-enum value; that is migrate's job:\n%s", raw)
		}
	})
}

// TestCheckNewerStore pins plain check's behavior on a store written by a
// newer nibs. The pre-run gate deliberately exempts plain check (it is the
// read-only diagnostic the newer-store refusal points users toward), which
// also bypasses the newer-store refusal — so check loads the newer files as
// written and may find values that are only "invalid" under THIS build's
// enums. Its field diagnostics must then say to upgrade nibs, not steer the
// user into hand-"repairing" (or migrating) values a newer format considers
// valid. Only --fix writes, so only --fix stays behind the refusal.
func TestCheckNewerStore(t *testing.T) {
	const futureNib = "---\nversion: 99\ntitle: Future\nstatus: superseded\n---\n\nBody.\n"

	t.Run("plain check proceeds with version-aware wording", func(t *testing.T) {
		app, _ := setupCheckTest(t, map[string]string{
			"chk-good1--ok.md":    chkValidNib,
			"chk-fut1--future.md": futureNib,
		})
		var total int
		var runErr error
		out := captureStdout(t, func() { total, runErr = runCheck(app) })
		if runErr != nil {
			t.Fatalf("runCheck error = %v", runErr)
		}
		if total != 1 {
			t.Errorf("total issues = %d, want 1 (the out-of-enum finding)", total)
		}
		if !strings.Contains(out, "newer nibs") || !strings.Contains(out, "upgrade nibs") {
			t.Errorf("diagnostic should say the file was written by a newer nibs and to upgrade, got:\n%s", out)
		}
		if strings.Contains(out, "nibs migrate") {
			t.Errorf("a newer-format value must not carry the legacy-value remediation, got:\n%s", out)
		}
	})

	t.Run("plain check runs end to end through the CLI gate", func(t *testing.T) {
		// A CLEAN newer-version store: checkCmd's os.Exit(1)-on-issues branch
		// is not reached, so the full Cobra pipeline is safe to drive. This is
		// the exemption pin: list/migrate refuse this store, check runs.
		nibsDir := setupListCobraTest(t, map[string]string{
			"chk-fut1--future.md": "---\nversion: 99\ntitle: Future\nstatus: todo\n---\n\nBody.\n",
		})
		t.Cleanup(resetCheckFlags)
		resetCheckFlags()
		out, err := runRootWith(t, "--nibs-path", nibsDir, "check")
		if err != nil {
			t.Fatalf("plain check on a newer-version store refused: %v\nout: %s", err, out)
		}
	})

	t.Run("check --fix stays behind the newer-store refusal", func(t *testing.T) {
		nibsDir := setupListCobraTest(t, map[string]string{
			"chk-fut1--future.md": futureNib,
		})
		t.Cleanup(resetCheckFlags)
		resetCheckFlags()
		_, err := runRootWith(t, "--nibs-path", nibsDir, "check", "--fix")
		if err == nil || !strings.Contains(err.Error(), "upgrade nibs") {
			t.Fatalf("check --fix on a newer-version store should refuse with the upgrade guidance, got: %v", err)
		}
	})
}

// TestCheckLegacyValueRemediation pins the remediation text for out-of-enum
// values against what `nibs migrate` can actually do, so check can never point
// at a command that no-ops (the circular-remediation loop: check exits 1
// naming migrate, migrate reports nothing pending, forever):
//
//   - a plain-scalar legacy value is exactly what the migration scan detects,
//     so the migrate pointer stays;
//   - the SAME legacy value in a spelling the header scan cannot see (a folded
//     scalar) gets the re-save-or-hand-fix wording instead;
//   - an arbitrary unknown value gets no migrate pointer at all — no step
//     rewrites it.
func TestCheckLegacyValueRemediation(t *testing.T) {
	runCheckOn := func(t *testing.T, files map[string]string) string {
		t.Helper()
		app, _ := setupCheckTest(t, files)
		var runErr error
		out := captureStdout(t, func() { _, runErr = runCheck(app) })
		if runErr != nil {
			t.Fatalf("runCheck error = %v", runErr)
		}
		return out
	}

	t.Run("plain-scalar legacy value keeps the migrate pointer", func(t *testing.T) {
		out := runCheckOn(t, map[string]string{
			"chk-leg1--old.md": "---\nversion: 1\ntitle: Legacy\nstatus: todo\npriority: deferred\n---\n\nBody.\n",
		})
		if !strings.Contains(out, "nibs migrate") {
			t.Errorf("plain-scalar deferred is migrate-fixable; the pointer must stay, got:\n%s", out)
		}
	})

	t.Run("legacy value in a scan-invisible spelling says so", func(t *testing.T) {
		// A folded scalar parses to the known legacy value "deferred", but the
		// migration header scan reads the literal `>-` — the gate never fires
		// and migrate reports nothing pending.
		app, nibsDir := setupCheckTest(t, map[string]string{
			"chk-fold1--folded.md": "---\nversion: 1\ntitle: Folded\nstatus: todo\npriority: >-\n    deferred\n---\n\nBody.\n",
		})
		// The circularity evidence: the scan genuinely cannot see it.
		if got := pendingNames(t, nibsDir); len(got) != 0 {
			t.Fatalf("scan sees the folded spelling (%v); fixture no longer reproduces the case", got)
		}
		var runErr error
		out := captureStdout(t, func() { _, runErr = runCheck(app) })
		if runErr != nil {
			t.Fatalf("runCheck error = %v", runErr)
		}
		if !strings.Contains(out, "migration scan cannot see") || !strings.Contains(out, "plain scalar") {
			t.Errorf("scan-invisible legacy value should get the re-save wording, got:\n%s", out)
		}
		if strings.Contains(out, "nibs migrate") {
			t.Errorf("pointing at migrate here is the circular-remediation loop, got:\n%s", out)
		}
	})

	t.Run("unknown value gets no migrate pointer", func(t *testing.T) {
		out := runCheckOn(t, map[string]string{
			"chk-odd1--odd.md": "---\nversion: 1\ntitle: Odd\nstatus: todo\npriority: made-up-nonsense\n---\n\nBody.\n",
		})
		if strings.Contains(out, "nibs migrate") {
			t.Errorf("no migration step rewrites an unknown value; the pointer must go, got:\n%s", out)
		}
		if !strings.Contains(out, "by hand") {
			t.Errorf("unknown value should say to repair the file by hand, got:\n%s", out)
		}
	})
}

// TestCheckJSONReportsLoadDiagnostics pins that both load-time conditions reach
// the --json envelope. JSON is where they were most invisible before: the
// stderr warning is not part of the document, so a scripted consumer had no way
// at all to learn the store was under-reporting.
func TestCheckJSONReportsLoadDiagnostics(t *testing.T) {
	app, _ := setupCheckTest(t, loadDiagnosticFiles())
	checkJSON = true

	var total int
	var runErr error
	out := captureStdout(t, func() { total, runErr = runCheck(app) })
	if runErr != nil {
		t.Fatalf("runCheck error = %v", runErr)
	}

	var got checkResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal check --json output: %v\noutput: %s", err, out)
	}
	if got.Success {
		t.Error("success = true; want false with load-time issues present")
	}
	if got.NibIssues == nil {
		t.Fatal("nib_issues is absent from the envelope")
	}

	if len(got.NibIssues.UnparseableFiles) != 1 {
		t.Fatalf("unparseable_files = %+v, want exactly 1 entry", got.NibIssues.UnparseableFiles)
	}
	if p := got.NibIssues.UnparseableFiles[0].Path; p != "data/chk-bad1--broken.md" {
		t.Errorf("unparseable_files[0].path = %q, want %q", p, "data/chk-bad1--broken.md")
	}

	if len(got.NibIssues.DuplicateIDs) != 1 {
		t.Fatalf("duplicate_ids = %+v, want exactly 1 entry", got.NibIssues.DuplicateIDs)
	}
	dup := got.NibIssues.DuplicateIDs[0]
	if dup.NibID != "chk-dup1" {
		t.Errorf("duplicate_ids[0].nib_id = %q, want %q", dup.NibID, "chk-dup1")
	}
	if dup.Loaded != "data/chk-dup1--beta.md" || dup.Shadowed != "data/chk-dup1--alpha.md" {
		t.Errorf("duplicate_ids[0] = {loaded:%q shadowed:%q}, want {loaded:%q shadowed:%q}",
			dup.Loaded, dup.Shadowed, "data/chk-dup1--beta.md", "data/chk-dup1--alpha.md")
	}

	// The two load-time issues must be counted, not merely listed: the exit
	// status and the "N issues found" summary both come off this number.
	if want := 3; total != want {
		t.Errorf("total issues = %d, want %d (1 unparseable + 1 duplicate + 1 broken link)", total, want)
	}
}

// TestCheckTextReportsLoadDiagnostics pins the human-readable branch: both
// conditions must be named, with the file paths the user has to go fix.
func TestCheckTextReportsLoadDiagnostics(t *testing.T) {
	app, _ := setupCheckTest(t, loadDiagnosticFiles())

	out := captureStdout(t, func() {
		if _, err := runCheck(app); err != nil {
			t.Errorf("runCheck error = %v", err)
		}
	})

	for _, want := range []string{
		"chk-bad1--broken.md",
		"chk-dup1--beta.md",
		"chk-dup1--alpha.md",
		"chk-dup1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("check output does not name %q\noutput:\n%s", want, out)
		}
	}

	// The parse error is what tells the user how to repair the file, and it must
	// arrive on the SAME line as the file it belongs to: yaml.v3 wraps its
	// message across two lines, which would otherwise spill out of the bullet
	// list and detach the diagnosis from the filename.
	var reported string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "chk-bad1--broken.md") {
			reported = line
			break
		}
	}
	if reported == "" {
		t.Fatalf("no output line names the unparseable file\noutput:\n%s", out)
	}
	if !strings.Contains(reported, `mapping key "title" already defined`) {
		t.Errorf("the unparseable-file line does not carry the parse error: %q", reported)
	}
}

// TestCheckFixLeavesLoadDiagnosticsUnfixed is the guard for the deliberate
// non-repair. Neither condition has a safe automatic fix — repairing YAML means
// editing what the user wrote, and resolving a duplicate means choosing which
// file to lose — so --fix must leave both files byte-identical, keep reporting
// them, and SAY it cannot fix them rather than letting "Fixed N issue(s)" imply
// it handled everything.
func TestCheckFixLeavesLoadDiagnosticsUnfixed(t *testing.T) {
	app, nibsDir := setupCheckTest(t, loadDiagnosticFiles())
	checkFix = true

	// Snapshot the bytes of every file --fix must not touch.
	untouchable := []string{"chk-bad1--broken.md", "chk-dup1--alpha.md", "chk-dup1--beta.md"}
	before := make(map[string][]byte, len(untouchable))
	for _, name := range untouchable {
		data, err := os.ReadFile(dataPath(nibsDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		before[name] = data
	}

	var total int
	out := captureStdout(t, func() {
		var err error
		if total, err = runCheck(app); err != nil {
			t.Errorf("runCheck error = %v", err)
		}
	})

	// Files on disk are untouched.
	for _, name := range untouchable {
		after, err := os.ReadFile(dataPath(nibsDir, name))
		if err != nil {
			t.Fatalf("read %s after --fix: %v", name, err)
		}
		if string(after) != string(before[name]) {
			t.Errorf("--fix rewrote %s; it must leave load-time problems alone", name)
		}
	}

	// The broken link WAS fixed, so this is not passing by fixing nothing.
	if b, err := app.Core.Get("chk-link1"); err != nil {
		t.Fatalf("Get(chk-link1) error = %v", err)
	} else if b.Parent != "" {
		t.Errorf("chk-link1 parent = %q after --fix, want it removed", b.Parent)
	}

	// The report says so, per file, instead of silently omitting them.
	for _, want := range []string{
		"Cannot auto-fix",
		"chk-bad1--broken.md",
		"chk-dup1--beta.md",
		"chk-dup1--alpha.md",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("--fix output does not contain %q\noutput:\n%s", want, out)
		}
	}
	// Nothing may claim these were removed/fixed.
	for _, forbidden := range []string{"removed chk-bad1", "removed chk-dup1"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("--fix output claims %q; it did no such thing\noutput:\n%s", forbidden, out)
		}
	}

	// Still counted as outstanding after --fix.
	if want := 2; total != want {
		t.Errorf("total issues after --fix = %d, want %d (the unparseable file and the duplicate id remain)", total, want)
	}
}

// TestCheckCleanStoreReportsNoLoadDiagnostics is the false-positive guard for
// the command layer: a healthy store must still report all-clear, so the new
// section cannot pass its tests by always firing.
func TestCheckCleanStoreReportsNoLoadDiagnostics(t *testing.T) {
	app, _ := setupCheckTest(t, map[string]string{
		"chk-aaa1--one.md": chkValidNib,
		"chk-bbb2--two.md": chkValidNib,
	})

	var total int
	out := captureStdout(t, func() {
		var err error
		if total, err = runCheck(app); err != nil {
			t.Errorf("runCheck error = %v", err)
		}
	})

	if total != 0 {
		t.Errorf("total issues = %d, want 0 for a clean store\noutput:\n%s", total, out)
	}
	if !strings.Contains(out, "All checks passed") {
		t.Errorf("clean store output does not report all clear\noutput:\n%s", out)
	}
	for _, forbidden := range []string{"unparseable", "duplicate id", "Cannot auto-fix"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("clean store output mentions %q\noutput:\n%s", forbidden, out)
		}
	}
}

// chkLinkToSkippedNib names the unparseable file's id. The target's file is
// present on disk and merely fails to parse, so this link is unresolvable for
// now — NOT broken — and --fix must leave it alone.
const chkLinkToSkippedNib = `---
version: 1
title: Links to the skipped nib
status: todo
type: task
priority: normal
parent: chk-bad1
---

Body.
`

// TestCheckFixKeepsLinksToSkippedNibs pins the data-loss guard and, just as
// importantly, that the REPORT matches what was done.
//
// Before this, `nibs check --fix` printed "Cannot auto-fix unparseable nib file
// chk-bad1--broken.md" and, in the very next line, "removed broken link
// parent:chk-bad1" — erasing an edge that repairing the YAML would have
// restored. The core now keeps such links; this test additionally pins that the
// command does not claim to have removed one, because a report that lies about
// a destructive action is its own defect.
//
// The genuinely-dangling link (chk-nope9, no file anywhere) is the control: it
// must still be removed, so this cannot pass by disabling the fixer.
func TestCheckFixKeepsLinksToSkippedNibs(t *testing.T) {
	files := loadDiagnosticFiles()
	files["chk-skip1--links-to-skipped.md"] = chkLinkToSkippedNib

	app, nibsDir := setupCheckTest(t, files)
	checkFix = true

	out := captureStdout(t, func() {
		if _, err := runCheck(app); err != nil {
			t.Fatalf("runCheck() error = %v", err)
		}
	})

	// The link to the skipped nib survives on disk — the whole point.
	raw, err := os.ReadFile(dataPath(nibsDir, "chk-skip1--links-to-skipped.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(raw), "parent: chk-bad1") {
		t.Errorf("link to the skipped nib was erased from disk:\n%s", raw)
	}

	// The report must not claim it removed it.
	if strings.Contains(out, "removed broken link parent:chk-bad1") {
		t.Errorf("report claims a removal that did not happen:\n%s", out)
	}
	if !strings.Contains(out, "kept parent:chk-bad1") {
		t.Errorf("report does not explain why the link was kept:\n%s", out)
	}

	// Control: the genuinely dangling link IS still removed, on disk and in the
	// report — so a blanket "stop fixing anything" regression fails here.
	brokenRaw, err := os.ReadFile(dataPath(nibsDir, "chk-link1--broken.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(brokenRaw), "parent: chk-nope9") {
		t.Errorf("a genuinely dangling link was NOT removed:\n%s", brokenRaw)
	}
	if !strings.Contains(out, "removed broken link parent:chk-nope9") {
		t.Errorf("report does not mention removing the dangling link:\n%s", out)
	}
}

// TestCheckDoesNotLeakANSIToRedirectedOutput pins the color-downsampling
// contract for every command that renders a style.
//
// Lip Gloss v2 dropped the global renderer that used to strip color inside
// Style.Render: Render now always emits truecolor ANSI, and downsampling
// happens at the writer. Printing a rendered string with the fmt helpers
// therefore dumps raw escape sequences into a pipe or a file, where v1 emitted
// clean text. The ui.Print* helpers exist to route that output through Lip
// Gloss instead; this fails if a command goes back to fmt.
//
// captureStdout hands the command an os.Pipe, which is not a terminal, so a
// correctly-routed report carries no escape sequences at all.
func TestCheckDoesNotLeakANSIToRedirectedOutput(t *testing.T) {
	app, _ := setupCheckTest(t, loadDiagnosticFiles())

	out := captureStdout(t, func() {
		if _, err := runCheck(app); err != nil {
			t.Errorf("runCheck error = %v", err)
		}
	})

	if out == "" {
		t.Fatal("check produced no output; the assertion below would pass vacuously")
	}
	if strings.ContainsRune(out, '\x1b') {
		t.Errorf("check leaked ANSI escapes into non-terminal output:\n%q", out)
	}
}
