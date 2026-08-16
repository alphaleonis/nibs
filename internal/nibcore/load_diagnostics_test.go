package nibcore

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/store"
)

// Minimal well-formed nib bodies for the load-diagnostics tests. The id comes
// from the filename, so the front matter carries no id of its own.
const (
	diagValidNib = `---
version: 1
title: Good
status: todo
type: task
priority: normal
created_at: 2026-01-02T03:04:05Z
updated_at: 2026-01-02T03:04:05Z
---

Body.
`

	// Duplicate MODELED front-matter key: yaml.v3 hard-errors on it, which is
	// exactly the "one hand-edited file poisons the store" case loadFromDisk
	// log-and-skips. Using a real parse failure (rather than arbitrary garbage)
	// keeps the fixture faithful to the failure users actually hit.
	diagUnparseableNib = `---
version: 1
title: First Title
title: Second Title
status: todo
---

Body.
`
)

// TestCheckAllLinksReportsUnparseableFile pins the first half of the gap this
// surface closes: a file skipped at load time is absent from every query, and
// before this the ONLY trace was a logWarn line on stderr that no production
// code redirects. CheckAllLinks must name the file so `nibs check` can report
// it.
//
// The archive case additionally pins the path shape: a diagnostic path is
// rendered the way nib.Path is — relative to the .nibs root, forward slashes —
// so the assertion below uses a forward slash deliberately, not a
// platform separator.
func TestCheckAllLinksReportsUnparseableFile(t *testing.T) {
	tests := []struct {
		name     string
		subdir   string
		filename string
		wantPath string
		wantID   string
	}{
		{
			name:     "under data",
			subdir:   store.DataDirName,
			filename: "nibs-bad1--broken.md",
			wantPath: "data/nibs-bad1--broken.md",
			wantID:   "nibs-bad1",
		},
		{
			name:     "under archive",
			subdir:   store.ArchiveDirName,
			filename: "nibs-bad2--broken.md",
			wantPath: "archive/nibs-bad2--broken.md",
			wantID:   "nibs-bad2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, nibsDir := mustLoadPrefixedCore(t)

			dir := filepath.Join(nibsDir, tt.subdir)
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatalf("mkdir %s: %v", dir, err)
			}
			writeNibFile(t, storeData(t, nibsDir), "nibs-good1--ok.md", diagValidNib)
			writeNibFile(t, dir, tt.filename, diagUnparseableNib)

			if err := core.Load(); err != nil {
				t.Fatalf("Load() error = %v, want nil", err)
			}

			result := core.CheckAllLinks()
			if len(result.UnparseableFiles) != 1 {
				t.Fatalf("UnparseableFiles = %+v, want exactly 1 entry", result.UnparseableFiles)
			}
			got := result.UnparseableFiles[0]
			if got.Path != tt.wantPath {
				t.Errorf("UnparseableFiles[0].Path = %q, want %q", got.Path, tt.wantPath)
			}
			if got.NibID != tt.wantID {
				t.Errorf("UnparseableFiles[0].NibID = %q, want %q", got.NibID, tt.wantID)
			}
			if got.Reason == "" {
				t.Error("UnparseableFiles[0].Reason is empty; want the underlying parse error")
			}

			// The whole point is that `nibs check` treats this as an issue.
			if !result.HasIssues() {
				t.Error("HasIssues() = false; want true with an unparseable file")
			}
			if result.TotalIssues() != 1 {
				t.Errorf("TotalIssues() = %d, want 1 (result: %+v)", result.TotalIssues(), result)
			}

			// Degrading to one missing nib, not a dead store: the good file loads.
			if _, err := core.Get("nibs-good1"); err != nil {
				t.Errorf("Get(nibs-good1) error = %v; the valid nib must still load", err)
			}
		})
	}
}

// TestLoadAndCheckReportOutOfEnumValues pins the authoritative backstop behind
// the migration gate's header-scan heuristic: an out-of-enum field value (the
// legacy `priority: deferred` slipping past the gate, a hand-edited typo)
// loads EXACTLY AS WRITTEN — no in-memory normalization, which would diverge
// the etag from the on-disk bytes — but never silently: Load warns by nib id,
// and CheckAllLinks reports it so `nibs check` makes it actionable.
func TestLoadAndCheckReportOutOfEnumValues(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)
	legacy := "---\nversion: 1\ntitle: Legacy\nstatus: todo\ntype: task\npriority: deferred\n---\n\nBody.\n"
	writeNibFile(t, storeData(t, nibsDir), "nibs-leg1--legacy.md", legacy)
	writeNibFile(t, storeData(t, nibsDir), "nibs-good1--ok.md", diagValidNib)

	var warnings strings.Builder
	core.SetWarnWriter(&warnings)
	if err := core.Load(); err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	// The load warning names the nib and the offending value.
	if w := warnings.String(); !strings.Contains(w, "nibs-leg1") || !strings.Contains(w, "deferred") {
		t.Errorf("load warning should name the nib and value, got:\n%s", w)
	}

	// Diagnostics, not normalization: the value is in memory as written.
	b, err := core.Get("nibs-leg1")
	if err != nil {
		t.Fatalf("Get(nibs-leg1): %v", err)
	}
	if b.Priority != "deferred" {
		t.Errorf("Priority = %q, want the on-disk %q — diagnostics must not normalize", b.Priority, "deferred")
	}

	result := core.CheckAllLinks()
	if len(result.InvalidEnums) != 1 {
		t.Fatalf("InvalidEnums = %+v, want exactly 1 entry", result.InvalidEnums)
	}
	got := result.InvalidEnums[0]
	if got.NibID != "nibs-leg1" {
		t.Errorf("InvalidEnums[0].NibID = %q, want %q", got.NibID, "nibs-leg1")
	}
	if !strings.Contains(got.Reason, "deferred") {
		t.Errorf("InvalidEnums[0].Reason = %q, want it to name the value", got.Reason)
	}
	if !result.HasIssues() || result.TotalIssues() != 1 {
		t.Errorf("TotalIssues() = %d, want 1 (result: %+v)", result.TotalIssues(), result)
	}

	// False-positive guard: valid enum values report nothing.
	if err := os.Remove(dataPath(nibsDir, "nibs-leg1--legacy.md")); err != nil {
		t.Fatal(err)
	}
	if err := core.Load(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if result := core.CheckAllLinks(); len(result.InvalidEnums) != 0 {
		t.Errorf("InvalidEnums = %+v after removing the legacy file, want none", result.InvalidEnums)
	}
}

// TestCheckAllLinksReportsDuplicateID pins the second half: two files claiming
// one id means one silently shadows the other, and which one wins depends on
// the lexical walk order. The report must name both files and which one was
// loaded, so the user can tell which file's content the store is answering with.
func TestCheckAllLinksReportsDuplicateID(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	writeCollisionNibFile(t, storeData(t, nibsDir), "nibs-x9z2--alpha.md", "Alpha")
	writeCollisionNibFile(t, storeData(t, nibsDir), "nibs-x9z2--beta.md", "Beta")

	if err := core.Load(); err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	result := core.CheckAllLinks()
	if len(result.DuplicateIDs) != 1 {
		t.Fatalf("DuplicateIDs = %+v, want exactly 1 entry", result.DuplicateIDs)
	}
	got := result.DuplicateIDs[0]
	if got.NibID != "nibs-x9z2" {
		t.Errorf("DuplicateIDs[0].NibID = %q, want %q", got.NibID, "nibs-x9z2")
	}
	// Direction matters: beta is lexically last, so beta is the file the store
	// answers with and alpha is the one nothing can reach. A swapped-args
	// regression would tell the user to keep the wrong file.
	if got.Loaded != "data/nibs-x9z2--beta.md" {
		t.Errorf("DuplicateIDs[0].Loaded = %q, want %q (lexically-last file wins)", got.Loaded, "data/nibs-x9z2--beta.md")
	}
	if got.Shadowed != "data/nibs-x9z2--alpha.md" {
		t.Errorf("DuplicateIDs[0].Shadowed = %q, want %q", got.Shadowed, "data/nibs-x9z2--alpha.md")
	}

	if !result.HasIssues() {
		t.Error("HasIssues() = false; want true with a duplicate id")
	}
	if result.TotalIssues() != 1 {
		t.Errorf("TotalIssues() = %d, want 1 (result: %+v)", result.TotalIssues(), result)
	}
}

// TestCheckAllLinksReportsChainedDuplicateIDs pins the N-file case: three files
// sharing an id produce two shadowing events, chained in load order, and only
// the last entry's Loaded file is the final occupant.
func TestCheckAllLinksReportsChainedDuplicateIDs(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	writeCollisionNibFile(t, storeData(t, nibsDir), "nibs-x9z2--alpha.md", "Alpha")
	writeCollisionNibFile(t, storeData(t, nibsDir), "nibs-x9z2--beta.md", "Beta")
	writeCollisionNibFile(t, storeData(t, nibsDir), "nibs-x9z2--gamma.md", "Gamma")

	if err := core.Load(); err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	result := core.CheckAllLinks()
	want := []DuplicateID{
		{NibID: "nibs-x9z2", Loaded: "data/nibs-x9z2--beta.md", Shadowed: "data/nibs-x9z2--alpha.md"},
		{NibID: "nibs-x9z2", Loaded: "data/nibs-x9z2--gamma.md", Shadowed: "data/nibs-x9z2--beta.md"},
	}
	if len(result.DuplicateIDs) != len(want) {
		t.Fatalf("DuplicateIDs = %+v, want %+v", result.DuplicateIDs, want)
	}
	for i, w := range want {
		if result.DuplicateIDs[i] != w {
			t.Errorf("DuplicateIDs[%d] = %+v, want %+v", i, result.DuplicateIDs[i], w)
		}
	}
}

// TestCheckAllLinksCleanStoreHasNoLoadDiagnostics is the false-positive guard:
// without it, a check that always fired would pass every test above.
func TestCheckAllLinksCleanStoreHasNoLoadDiagnostics(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	writeNibFile(t, storeData(t, nibsDir), "nibs-aaa1--one.md", diagValidNib)
	writeNibFile(t, storeData(t, nibsDir), "nibs-bbb2--two.md", diagValidNib)

	if err := core.Load(); err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	result := core.CheckAllLinks()
	if len(result.UnparseableFiles) != 0 {
		t.Errorf("UnparseableFiles = %+v, want none for a clean store", result.UnparseableFiles)
	}
	if len(result.DuplicateIDs) != 0 {
		t.Errorf("DuplicateIDs = %+v, want none for a clean store", result.DuplicateIDs)
	}
	if result.HasIssues() {
		t.Errorf("HasIssues() = true for a clean store (result: %+v)", result)
	}
	if result.TotalIssues() != 0 {
		t.Errorf("TotalIssues() = %d, want 0", result.TotalIssues())
	}
}

// TestCheckAllLinksLoadDiagnosticsRebuiltOnReload pins that the diagnostics
// describe the LAST load, not every load ever: repairing the offending files
// and reloading must clear the report, or `nibs check` would keep accusing
// files the user already fixed.
func TestCheckAllLinksLoadDiagnosticsRebuiltOnReload(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	writeNibFile(t, storeData(t, nibsDir), "nibs-bad1--broken.md", diagUnparseableNib)
	writeCollisionNibFile(t, storeData(t, nibsDir), "nibs-x9z2--alpha.md", "Alpha")
	writeCollisionNibFile(t, storeData(t, nibsDir), "nibs-x9z2--beta.md", "Beta")

	if err := core.Load(); err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if result := core.CheckAllLinks(); len(result.UnparseableFiles) != 1 || len(result.DuplicateIDs) != 1 {
		t.Fatalf("before repair: UnparseableFiles = %+v, DuplicateIDs = %+v; want 1 of each",
			result.UnparseableFiles, result.DuplicateIDs)
	}

	// Repair: make the broken file parse, and drop the shadowed duplicate.
	writeNibFile(t, storeData(t, nibsDir), "nibs-bad1--broken.md", diagValidNib)
	if err := os.Remove(dataPath(nibsDir, "nibs-x9z2--alpha.md")); err != nil {
		t.Fatalf("remove duplicate: %v", err)
	}

	if err := core.Load(); err != nil {
		t.Fatalf("reload error = %v, want nil", err)
	}
	result := core.CheckAllLinks()
	if len(result.UnparseableFiles) != 0 || len(result.DuplicateIDs) != 0 {
		t.Errorf("after repair: UnparseableFiles = %+v, DuplicateIDs = %+v; want none of either",
			result.UnparseableFiles, result.DuplicateIDs)
	}
}

// TestCheckAllLinksLoadDiagnosticsAreCopies pins that the result does not alias
// Core's retained state. CheckAllLinks holds only a read lock, so handing out
// the stored slice would let any caller mutate shared state without one — and a
// caller editing the report (sorting it, redacting a path) would silently
// rewrite what every later check reports.
func TestCheckAllLinksLoadDiagnosticsAreCopies(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	writeNibFile(t, storeData(t, nibsDir), "nibs-bad1--broken.md", diagUnparseableNib)
	writeCollisionNibFile(t, storeData(t, nibsDir), "nibs-x9z2--alpha.md", "Alpha")
	writeCollisionNibFile(t, storeData(t, nibsDir), "nibs-x9z2--beta.md", "Beta")

	if err := core.Load(); err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	first := core.CheckAllLinks()
	first.UnparseableFiles[0].Path = "clobbered"
	first.DuplicateIDs[0].Loaded = "clobbered"

	second := core.CheckAllLinks()
	if second.UnparseableFiles[0].Path == "clobbered" {
		t.Error("UnparseableFiles aliases Core's retained state; mutating the result changed the next check")
	}
	if second.DuplicateIDs[0].Loaded == "clobbered" {
		t.Error("DuplicateIDs aliases Core's retained state; mutating the result changed the next check")
	}
}

// TestFixBrokenLinksKeepsLinksToSkippedNibs is the guard for a data-loss bug
// this surface made newly reachable: `nibs check` now TELLS the user about an
// unparseable file, and the obvious next step is `--fix`.
//
// A skipped file's id never enters c.nibs, so every link naming it resolves to
// nothing and CheckAllLinks classifies it broken. FixBrokenLinks repeated that
// same lookup and deleted the link from disk — erasing a valid edge whose
// target merely needed a YAML repair, and which repairing the YAML does not
// bring back. In one run the command printed "Cannot auto-fix unparseable nib
// file …" and "removed broken link parent:…" together.
//
// The migrate command takes the same posture for the same hazard: it refuses
// to run at all while a file is unparseable, naming it instead of guessing.
// This pins the keep-don't-erase rule for FixBrokenLinks.
func TestFixBrokenLinksKeepsLinksToSkippedNibs(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	// The target: present on disk, unparseable, so it is skipped at load.
	if err := os.WriteFile(dataPath(nibsDir, "nibs-tgt1--target.md"), []byte(diagUnparseableNib), 0644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	// A bystander linking to it by parent AND blocked_by, plus a link to an id
	// that genuinely does not exist anywhere — the control, which MUST still be
	// removed so this test cannot pass by disabling the fixer wholesale.
	child := `---
version: 1
title: Child
status: todo
type: task
priority: normal
parent: nibs-tgt1
blocked_by:
    - nibs-tgt1
    - nibs-gone9
created_at: 2026-01-02T03:04:05Z
updated_at: 2026-01-02T03:04:05Z
---

Body.
`
	childPath := dataPath(nibsDir, "nibs-chd1--child.md")
	if err := os.WriteFile(childPath, []byte(child), 0644); err != nil {
		t.Fatalf("write child: %v", err)
	}
	if err := core.Load(); err != nil {
		t.Fatalf("reload: %v", err)
	}

	if _, err := core.FixBrokenLinks(); err != nil {
		t.Fatalf("FixBrokenLinks: %v", err)
	}

	stored, err := core.Get("nibs-chd1")
	if err != nil {
		t.Fatalf("get child: %v", err)
	}

	if stored.Parent != "nibs-tgt1" {
		t.Errorf("Parent = %q, want %q kept — the target is skipped, not missing; erasing it loses an edge a YAML repair would have restored", stored.Parent, "nibs-tgt1")
	}
	if !slices.Contains(stored.BlockedBy, "nibs-tgt1") {
		t.Errorf("BlockedBy = %v, want it to still contain %q for the same reason", stored.BlockedBy, "nibs-tgt1")
	}
	// The control: a genuinely absent target must still be dropped, so a
	// blanket "never fix anything" regression fails here.
	if slices.Contains(stored.BlockedBy, "nibs-gone9") {
		t.Errorf("BlockedBy = %v, want %q removed — it names no file on disk", stored.BlockedBy, "nibs-gone9")
	}

	// And the on-disk file must agree: the whole failure mode is bytes lost.
	raw, err := os.ReadFile(childPath)
	if err != nil {
		t.Fatalf("read child: %v", err)
	}
	if !strings.Contains(string(raw), "parent: nibs-tgt1") {
		t.Errorf("child file lost its parent line:\n%s", raw)
	}
}

// TestSkippedIDSet pins the one shared builder for "which ids are merely
// skipped this load": both spellings of each skipped id — as the filename
// derives it and prefix-trimmed — answer true to a plain map probe, so every
// consumer (FixBrokenLinks' keep rule, check's report partition) tests a link
// target with a single lookup of the spelling the file holds.
func TestSkippedIDSet(t *testing.T) {
	unparseable := []UnparseableFile{
		{NibID: "nibs-abc", Path: "nibs-abc--broken.md", Reason: "boom"},
		{NibID: "bare", Path: "bare--broken.md", Reason: "boom"},
		{NibID: "", Path: "noid.md", Reason: "boom"}, // filename yields no id
	}
	set := SkippedIDSet(unparseable, "nibs-")

	for _, want := range []string{"nibs-abc", "abc", "bare"} {
		if !set[want] {
			t.Errorf("SkippedIDSet missing %q: %v", want, set)
		}
	}
	for _, wantAbsent := range []string{"", "nibs-bare", "other"} {
		if set[wantAbsent] {
			t.Errorf("SkippedIDSet wrongly contains %q: %v", wantAbsent, set)
		}
	}

	if got := SkippedIDSet(nil, "nibs-"); len(got) != 0 {
		t.Errorf("SkippedIDSet(nil) = %v, want empty", got)
	}
}
