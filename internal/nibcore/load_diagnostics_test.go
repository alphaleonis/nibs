package nibcore

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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
			name:     "at the nibs root",
			filename: "nibs-bad1--broken.md",
			wantPath: "nibs-bad1--broken.md",
			wantID:   "nibs-bad1",
		},
		{
			name:     "under archive",
			subdir:   ArchiveDir,
			filename: "nibs-bad2--broken.md",
			wantPath: "archive/nibs-bad2--broken.md",
			wantID:   "nibs-bad2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, nibsDir := mustLoadPrefixedCore(t)

			dir := nibsDir
			if tt.subdir != "" {
				dir = filepath.Join(nibsDir, tt.subdir)
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatalf("mkdir %s: %v", dir, err)
				}
			}
			writeNibFile(t, nibsDir, "nibs-good1--ok.md", diagValidNib)
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

// TestCheckAllLinksReportsDuplicateID pins the second half: two files claiming
// one id means one silently shadows the other, and which one wins depends on
// the lexical walk order. The report must name both files and which one was
// loaded, so the user can tell which file's content the store is answering with.
func TestCheckAllLinksReportsDuplicateID(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	writeCollisionNibFile(t, nibsDir, "nibs-x9z2--alpha.md", "Alpha")
	writeCollisionNibFile(t, nibsDir, "nibs-x9z2--beta.md", "Beta")

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
	if got.Loaded != "nibs-x9z2--beta.md" {
		t.Errorf("DuplicateIDs[0].Loaded = %q, want %q (lexically-last file wins)", got.Loaded, "nibs-x9z2--beta.md")
	}
	if got.Shadowed != "nibs-x9z2--alpha.md" {
		t.Errorf("DuplicateIDs[0].Shadowed = %q, want %q", got.Shadowed, "nibs-x9z2--alpha.md")
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

	writeCollisionNibFile(t, nibsDir, "nibs-x9z2--alpha.md", "Alpha")
	writeCollisionNibFile(t, nibsDir, "nibs-x9z2--beta.md", "Beta")
	writeCollisionNibFile(t, nibsDir, "nibs-x9z2--gamma.md", "Gamma")

	if err := core.Load(); err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	result := core.CheckAllLinks()
	want := []DuplicateID{
		{NibID: "nibs-x9z2", Loaded: "nibs-x9z2--beta.md", Shadowed: "nibs-x9z2--alpha.md"},
		{NibID: "nibs-x9z2", Loaded: "nibs-x9z2--gamma.md", Shadowed: "nibs-x9z2--beta.md"},
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

	writeNibFile(t, nibsDir, "nibs-aaa1--one.md", diagValidNib)
	writeNibFile(t, nibsDir, "nibs-bbb2--two.md", diagValidNib)

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

	writeNibFile(t, nibsDir, "nibs-bad1--broken.md", diagUnparseableNib)
	writeCollisionNibFile(t, nibsDir, "nibs-x9z2--alpha.md", "Alpha")
	writeCollisionNibFile(t, nibsDir, "nibs-x9z2--beta.md", "Beta")

	if err := core.Load(); err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if result := core.CheckAllLinks(); len(result.UnparseableFiles) != 1 || len(result.DuplicateIDs) != 1 {
		t.Fatalf("before repair: UnparseableFiles = %+v, DuplicateIDs = %+v; want 1 of each",
			result.UnparseableFiles, result.DuplicateIDs)
	}

	// Repair: make the broken file parse, and drop the shadowed duplicate.
	writeNibFile(t, nibsDir, "nibs-bad1--broken.md", diagValidNib)
	if err := os.Remove(filepath.Join(nibsDir, "nibs-x9z2--alpha.md")); err != nil {
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

	writeNibFile(t, nibsDir, "nibs-bad1--broken.md", diagUnparseableNib)
	writeCollisionNibFile(t, nibsDir, "nibs-x9z2--alpha.md", "Alpha")
	writeCollisionNibFile(t, nibsDir, "nibs-x9z2--beta.md", "Beta")

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
// migrateV0ToV1 already takes the opposite posture for exactly this hazard
// (see the skipped-set note at the top of migrate.go), deferring rather than
// erasing. This pins the same rule for FixBrokenLinks.
func TestFixBrokenLinksKeepsLinksToSkippedNibs(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	// The target: present on disk, unparseable, so it is skipped at load.
	if err := os.WriteFile(filepath.Join(nibsDir, "nibs-tgt1--target.md"), []byte(diagUnparseableNib), 0644); err != nil {
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
	childPath := filepath.Join(nibsDir, "nibs-chd1--child.md")
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
