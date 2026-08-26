package reprefix

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/testdata/fixtures"
)

// newTestNib writes a fully-rendered nib to the given relative path under root.
// Title/Status/Version are set to non-empty defaults so nib.Parse round-trips
// successfully when the executor reads the file back.
func newTestNib(t *testing.T, root, relPath, id, parent string, blockedBy []string, body string) {
	t.Helper()
	writeTestNib(t, root, relPath, &nib.Nib{
		ID:        id,
		Title:     id,
		Status:    "todo",
		Version:   1,
		Parent:    parent,
		BlockedBy: blockedBy,
		Body:      body,
	})
}

// writeTestNib renders b and writes it to root/relPath, creating parent
// directories as needed. Callers that need link fields beyond parent/blocked_by
// build the nib themselves and use this instead of newTestNib.
func writeTestNib(t *testing.T, root, relPath string, b *nib.Nib) {
	t.Helper()
	data, err := b.Render()
	if err != nil {
		t.Fatalf("rendering test nib %q: %v", b.ID, err)
	}
	abs := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", filepath.Dir(abs), err)
	}
	if err := os.WriteFile(abs, data, 0o644); err != nil {
		t.Fatalf("write test nib %q: %v", abs, err)
	}
}

// readNib opens a rendered nib at root/relPath and parses it.
func readNib(t *testing.T, root, relPath string) *nib.Nib {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(relPath))
	f, err := os.Open(abs)
	if err != nil {
		t.Fatalf("open %q: %v", abs, err)
	}
	defer func() { _ = f.Close() }()
	b, err := nib.Parse(f)
	if err != nil {
		t.Fatalf("parse %q: %v", abs, err)
	}
	return b
}

func TestExecute_TracerBullet_ThreeNibHierarchy(t *testing.T) {
	root := t.TempDir()

	newTestNib(t, root, "tnib-aaa--root.md", "tnib-aaa", "", nil, "root nib body")
	newTestNib(t, root, "tnib-bbb--child.md", "tnib-bbb", "tnib-aaa", nil, "child nib body")
	newTestNib(t, root, "tnib-ccc--blocked.md", "tnib-ccc", "tnib-aaa", []string{"tnib-bbb"}, "blocked nib body")

	snapshot := []NibSnapshot{
		{ID: "tnib-aaa", Path: "tnib-aaa--root.md"},
		{ID: "tnib-bbb", Path: "tnib-bbb--child.md", Parent: "tnib-aaa"},
		{ID: "tnib-ccc", Path: "tnib-ccc--blocked.md", Parent: "tnib-aaa", BlockedBy: []string{"tnib-bbb"}},
	}

	plan, err := BuildPlan(snapshot, "tnib-", "new-", stubExists)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	if err := Execute(plan, root); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Old paths should be gone.
	for _, oldPath := range []string{"tnib-aaa--root.md", "tnib-bbb--child.md", "tnib-ccc--blocked.md"} {
		abs := filepath.Join(root, oldPath)
		if _, err := os.Stat(abs); !os.IsNotExist(err) {
			t.Errorf("expected old path %q to be gone, stat err = %v", oldPath, err)
		}
	}

	// New paths should exist.
	for _, newPath := range []string{"new-aaa--root.md", "new-bbb--child.md", "new-ccc--blocked.md"} {
		abs := filepath.Join(root, newPath)
		if _, err := os.Stat(abs); err != nil {
			t.Errorf("expected new path %q to exist, stat err = %v", newPath, err)
		}
	}

	// References must be rewritten in the renamed files.
	bChild := readNib(t, root, "new-bbb--child.md")
	if bChild.Parent != "new-aaa" {
		t.Errorf("new-bbb--child.md parent: got %q, want %q", bChild.Parent, "new-aaa")
	}

	bBlocked := readNib(t, root, "new-ccc--blocked.md")
	if bBlocked.Parent != "new-aaa" {
		t.Errorf("new-ccc--blocked.md parent: got %q, want %q", bBlocked.Parent, "new-aaa")
	}
	if len(bBlocked.BlockedBy) != 1 || bBlocked.BlockedBy[0] != "new-bbb" {
		t.Errorf("new-ccc--blocked.md blocked_by: got %v, want [new-bbb]", bBlocked.BlockedBy)
	}
}

func TestExecute_PreflightCollisionGuard(t *testing.T) {
	root := t.TempDir()
	newTestNib(t, root, "tnib-aaa--solo.md", "tnib-aaa", "", nil, "body")

	// Construct a plan by hand with a non-empty Collisions slice. This is the
	// only way to test the executor's guard in isolation — BuildPlan would
	// happily produce the same FilePlan but the guard short-circuits before
	// touching disk regardless of how the collisions got populated.
	plan := &RenamePlan{
		OldPrefix: "tnib-",
		NewPrefix: "new-",
		Files: []FilePlan{
			{
				OldPath: "tnib-aaa--solo.md",
				NewPath: "new-aaa--solo.md",
				OldID:   "tnib-aaa",
				NewID:   "new-aaa",
			},
		},
		Collisions: []string{"something.md"},
	}

	err := Execute(plan, root)
	if err == nil {
		t.Fatal("Execute with collisions returned nil, want error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "collision") {
		t.Errorf("error should mention collisions, got: %v", err)
	}

	// Source file must be untouched.
	if _, err := os.Stat(filepath.Join(root, "tnib-aaa--solo.md")); err != nil {
		t.Errorf("source file should still exist, stat err = %v", err)
	}
	// Target must NOT exist.
	if _, err := os.Stat(filepath.Join(root, "new-aaa--solo.md")); !os.IsNotExist(err) {
		t.Errorf("target file should not exist, stat err = %v", err)
	}
}

func TestExecute_MissingSourceFile(t *testing.T) {
	root := t.TempDir() // empty directory

	plan := &RenamePlan{
		OldPrefix: "tnib-",
		NewPrefix: "new-",
		Files: []FilePlan{
			{
				OldPath: "tnib-missing.md",
				NewPath: "new-missing.md",
				OldID:   "tnib-missing",
				NewID:   "new-missing",
			},
		},
	}

	err := Execute(plan, root)
	if err == nil {
		t.Fatal("Execute with missing source returned nil, want error")
	}
	if !strings.Contains(err.Error(), "tnib-missing.md") {
		t.Errorf("error should name the missing source file, got: %v", err)
	}
	// Pin the failure to the rename pass so a future refactor that
	// accidentally tried to rewrite a nonexistent source first would trip
	// this assertion rather than sneaking through on the same filename
	// substring match.
	if !strings.Contains(err.Error(), "rename") {
		t.Errorf("error should originate in rename pass, got: %v", err)
	}
}

func TestExecute_NoRefUpdatesStillRenamedAndRewritten(t *testing.T) {
	root := t.TempDir()
	const body = "standalone content"
	newTestNib(t, root, "tnib-solo--alone.md", "tnib-solo", "", nil, body)

	snapshot := []NibSnapshot{
		{ID: "tnib-solo", Path: "tnib-solo--alone.md"},
	}
	plan, err := BuildPlan(snapshot, "tnib-", "new-", stubExists)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if err := Execute(plan, root); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// New file exists.
	abs := filepath.Join(root, "new-solo--alone.md")
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("expected new-solo--alone.md to exist, stat err = %v", err)
	}

	// Body preserved across rewrite. The frontmatter parser preserves a
	// leading newline that nib.Render emits as the separator, so we trim
	// surrounding whitespace to verify content rather than whitespace.
	bNew := readNib(t, root, "new-solo--alone.md")
	if got := strings.TrimSpace(bNew.Body); got != body {
		t.Errorf("body: got %q, want %q", got, body)
	}

	// Raw bytes: the # comment must reflect the new ID, not the old one.
	raw, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read raw: %v", err)
	}
	rawStr := string(raw)
	if !strings.Contains(rawStr, "# new-solo") {
		t.Errorf("rendered file should contain `# new-solo` header, got:\n%s", rawStr)
	}
	if strings.Contains(rawStr, "# tnib-solo") {
		t.Errorf("rendered file should NOT contain `# tnib-solo` header, got:\n%s", rawStr)
	}
}

func TestExecute_ArchiveSubdirHandled(t *testing.T) {
	root := t.TempDir()
	newTestNib(t, root, "tnib-aaa--active.md", "tnib-aaa", "", nil, "active body")
	newTestNib(t, root, "archive/tnib-zzz--done.md", "tnib-zzz", "", nil, "done body")

	snapshot := []NibSnapshot{
		{ID: "tnib-aaa", Path: "tnib-aaa--active.md"},
		{ID: "tnib-zzz", Path: "archive/tnib-zzz--done.md"},
	}
	plan, err := BuildPlan(snapshot, "tnib-", "new-", stubExists)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if err := Execute(plan, root); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// New paths exist.
	if _, err := os.Stat(filepath.Join(root, "new-aaa--active.md")); err != nil {
		t.Errorf("new-aaa--active.md should exist, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "archive", "new-zzz--done.md")); err != nil {
		t.Errorf("archive/new-zzz--done.md should exist, stat err = %v", err)
	}

	// Old paths gone.
	if _, err := os.Stat(filepath.Join(root, "tnib-aaa--active.md")); !os.IsNotExist(err) {
		t.Errorf("tnib-aaa--active.md should be gone, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "archive", "tnib-zzz--done.md")); !os.IsNotExist(err) {
		t.Errorf("archive/tnib-zzz--done.md should be gone, stat err = %v", err)
	}
}

func TestExecute_BodyPreservedAcrossRewrite(t *testing.T) {
	root := t.TempDir()

	// Multi-paragraph body with code fence and bullet list. Use a raw string
	// literal so backticks survive verbatim.
	body := "First paragraph.\n" +
		"\n" +
		"```go\n" +
		"func example() {}\n" +
		"```\n" +
		"\n" +
		"- item one\n" +
		"- item two"

	newTestNib(t, root, "tnib-rich--body.md", "tnib-rich", "", nil, body)

	snapshot := []NibSnapshot{
		{ID: "tnib-rich", Path: "tnib-rich--body.md"},
	}
	plan, err := BuildPlan(snapshot, "tnib-", "new-", stubExists)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if err := Execute(plan, root); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Compare parsed body of the new file against the original. Trim trailing
	// whitespace on both sides to ignore parser/renderer newline pedantry —
	// the goal is catching FORMATTING drift (lost code fence, collapsed
	// blank lines), not whitespace pedantry.
	bNew := readNib(t, root, "new-rich--body.md")
	gotTrimmed := strings.TrimSpace(bNew.Body)
	wantTrimmed := strings.TrimSpace(body)
	if gotTrimmed != wantTrimmed {
		t.Errorf("body mismatch:\ngot:\n%s\n\nwant:\n%s", gotTrimmed, wantTrimmed)
	}

	// Spot-check that critical structural elements survived.
	for _, marker := range []string{"```go", "func example() {}", "- item one", "- item two"} {
		if !strings.Contains(bNew.Body, marker) {
			t.Errorf("body missing marker %q\nbody:\n%s", marker, bNew.Body)
		}
	}
}

// snapshotFromDisk walks nibsPath and builds the NibSnapshot slice the
// reprefix planner needs by parsing every .md file it encounters.
func snapshotFromDisk(t *testing.T, nibsPath string) []NibSnapshot {
	t.Helper()
	var snap []NibSnapshot
	err := filepath.WalkDir(nibsPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		rel, err := filepath.Rel(nibsPath, path)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		b, parseErr := nib.Parse(f)
		_ = f.Close()
		if parseErr != nil {
			return parseErr
		}
		id, _ := nib.ParseFilename(filepath.Base(path), "tnib-")
		snap = append(snap, NibSnapshot{
			ID:        id,
			Path:      filepath.ToSlash(rel),
			Parent:    b.Parent,
			Milestone: b.Milestone,
			BlockedBy: b.BlockedBy,
			Blocking:  b.Blocking,
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk %q: %v", nibsPath, err)
	}
	return snap
}

func TestExecute_SampleProjectEndToEnd(t *testing.T) {
	root := fixtures.CopySampleProject(t)
	nibsPath := fixtures.NibsPath(root)

	snapshot := snapshotFromDisk(t, nibsPath)
	if len(snapshot) == 0 {
		t.Fatal("snapshot is empty — fixture not found?")
	}

	plan, err := BuildPlan(snapshot, "tnib-", "demo-", stubExists)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if len(plan.Collisions) != 0 {
		t.Fatalf("expected no collisions, got %v", plan.Collisions)
	}

	if err := Execute(plan, nibsPath); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Zero files starting with `tnib-` remain anywhere under nibsPath.
	walkErr := filepath.WalkDir(nibsPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasPrefix(d.Name(), "tnib-") {
			t.Errorf("file %q still has old prefix", path)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk after rename: %v", walkErr)
	}

	// Every file in plan.Files[].NewPath exists.
	for _, fp := range plan.Files {
		abs := filepath.Join(nibsPath, filepath.FromSlash(fp.NewPath))
		if _, err := os.Stat(abs); err != nil {
			t.Errorf("plan file %q missing post-execute: %v", fp.NewPath, err)
		}
	}

	// Spot-check: pick any plan entry with a non-empty NewParent and verify
	// the rewritten file's Parent has the new prefix.
	var spot *FilePlan
	for i := range plan.Files {
		if plan.Files[i].NewParent != "" {
			spot = &plan.Files[i]
			break
		}
	}
	if spot == nil {
		t.Fatal("expected at least one nib with a parent in the sample-project fixture")
		return
	}
	bSpot := readNib(t, nibsPath, spot.NewPath)
	if !strings.HasPrefix(bSpot.Parent, "demo-") {
		t.Errorf("spot-check parent: file %q parent %q should start with demo-", spot.NewPath, bSpot.Parent)
	}

	// Same spot-check for milestone, the other single-valued link field. A
	// milestone left under the old prefix names a nib that no longer exists,
	// which is exactly the dangling-assignment defect this covers.
	var spotMilestone *FilePlan
	for i := range plan.Files {
		if plan.Files[i].NewMilestone != "" {
			spotMilestone = &plan.Files[i]
			break
		}
	}
	if spotMilestone == nil {
		t.Fatal("expected at least one nib with a milestone in the sample-project fixture")
		return
	}
	bMilestone := readNib(t, nibsPath, spotMilestone.NewPath)
	if !strings.HasPrefix(bMilestone.Milestone, "demo-") {
		t.Errorf("spot-check milestone: file %q milestone %q should start with demo-", spotMilestone.NewPath, bMilestone.Milestone)
	}

	// No link field anywhere may still name the retired prefix. The four fields
	// checked are nib.LinkSpelling's — every id-valued key a nib file can carry.
	for _, fp := range plan.Files {
		b := readNib(t, nibsPath, fp.NewPath)
		var stale []string
		if strings.HasPrefix(b.Parent, "tnib-") {
			stale = append(stale, "parent:"+b.Parent)
		}
		if strings.HasPrefix(b.Milestone, "tnib-") {
			stale = append(stale, "milestone:"+b.Milestone)
		}
		for _, id := range b.BlockedBy {
			if strings.HasPrefix(id, "tnib-") {
				stale = append(stale, "blocked_by:"+id)
			}
		}
		for _, id := range b.Blocking {
			if strings.HasPrefix(id, "tnib-") {
				stale = append(stale, "blocking:"+id)
			}
		}
		if len(stale) > 0 {
			t.Errorf("file %q still links to the retired prefix: %v", fp.NewPath, stale)
		}
	}

	// Loading a fresh Core pointed at the renamed project must succeed.
	cfg := config.Default()
	cfg.Nibs.Prefix = "demo-"
	cfg.SetStoreDir(nibsPath)

	core := nibcore.New(nibsPath, cfg)
	if err := core.Load(); err != nil {
		t.Fatalf("nibcore.Load post-rename: %v", err)
	}
	if got := len(core.All()); got == 0 {
		t.Errorf("core.All() = 0 after post-rename load, want > 0")
	}
}

// TestExecute_RewritesMilestoneAndBlocking covers the two id-valued front-matter
// fields the sample-project fixture cannot: it has no `blocking:` key at all,
// and its milestone assignments are only exercised end-to-end above.
func TestExecute_RewritesMilestoneAndBlocking(t *testing.T) {
	root := t.TempDir()
	writeTestNib(t, root, "tnib-m01--release.md", &nib.Nib{
		ID:      "tnib-m01",
		Title:   "release",
		Status:  "todo",
		Type:    "milestone",
		Version: 1,
		Body:    "milestone body",
	})
	writeTestNib(t, root, "tnib-aaa--enqueued.md", &nib.Nib{
		ID:      "tnib-aaa",
		Title:   "enqueued",
		Status:  "todo",
		Version: 1,
		// Blocking is the legacy v0 spelling; Render still re-emits it, so a
		// file carrying one must be retargeted rather than left dangling.
		Milestone:      "tnib-m01",
		MilestoneOrder: "b",
		Blocking:       []string{"tnib-m01"},
		Area:           "web/ui",
		Body:           "enqueued body",
	})

	snapshot := []NibSnapshot{
		{ID: "tnib-m01", Path: "tnib-m01--release.md"},
		{
			ID:        "tnib-aaa",
			Path:      "tnib-aaa--enqueued.md",
			Milestone: "tnib-m01",
			Blocking:  []string{"tnib-m01"},
		},
	}
	plan, err := BuildPlan(snapshot, "tnib-", "new-", stubExists)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if err := Execute(plan, root); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	b := readNib(t, root, "new-aaa--enqueued.md")
	if b.Milestone != "new-m01" {
		t.Errorf("milestone = %q, want %q", b.Milestone, "new-m01")
	}
	if len(b.Blocking) != 1 || b.Blocking[0] != "new-m01" {
		t.Errorf("blocking = %v, want [new-m01]", b.Blocking)
	}
	// MilestoneOrder is a fractional index and Area is a plain path — neither is
	// id-valued, so neither may be touched by a prefix change.
	if b.MilestoneOrder != "b" {
		t.Errorf("milestone_order = %q, want %q — it is a fractional index, not an id", b.MilestoneOrder, "b")
	}
	if b.Area != "web/ui" {
		t.Errorf("area = %q, want %q — it is a path, not an id", b.Area, "web/ui")
	}
	if !strings.Contains(b.Body, "enqueued body") {
		t.Errorf("body lost during rewrite: got %q", b.Body)
	}
}

// TestRewriteBodyMentions pins the body-mention rewrite in isolation: which
// mentions move, which are left exactly as the author wrote them, and that a
// body with nothing to rewrite comes back unchanged.
func TestRewriteBodyMentions(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"empty body", "", ""},
		{"no mention at all", "plain prose with a # and an old- prefix in text", "plain prose with a # and an old- prefix in text"},
		{"full form is retargeted", "see #old-abc for details", "see #new-abc for details"},
		{
			// A short id carries no prefix and resolves to the same nib under
			// either one, so rewriting it would invent a spelling the author
			// did not write.
			name: "short form is untouched",
			body: "see #abc for details",
			want: "see #abc for details",
		},
		{
			// The token scan deduplicates; a rewrite that inherited that would
			// move only the first occurrence.
			name: "every occurrence is retargeted",
			body: "#old-abc and #old-abc and #old-abc",
			want: "#new-abc and #new-abc and #new-abc",
		},
		{"at the very start of the body", "#old-abc leads", "#new-abc leads"},
		{"at the very end of the body", "trailing #old-abc", "trailing #new-abc"},
		{"adjacent to punctuation", "(#old-abc), then #old-def.", "(#new-abc), then #new-def."},
		{"inline code span", "run `#old-abc` verbatim", "run `#old-abc` verbatim"},
		{"fenced code block", "```\n#old-abc\n```", "```\n#old-abc\n```"},
		{"link destination", "[text](#old-abc)", "[text](#old-abc)"},
		{"word-like byte before the sigil", "email#old-abc", "email#old-abc"},
		{
			name: "mixed forms in one body",
			body: "#old-abc, #abc, `#old-abc`, #old-abc",
			want: "#new-abc, #abc, `#old-abc`, #new-abc",
		},
		{
			// The new prefix is longer than the old one, so every splice moves
			// the offsets of the text after it.
			name: "many mentions under a longer prefix",
			body: "#old-a x #old-b y #old-c z #old-d",
			want: "#much-longer-a x #much-longer-b y #much-longer-c z #much-longer-d",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newPrefix := "new-"
			if strings.Contains(tt.want, "much-longer-") {
				newPrefix = "much-longer-"
			}
			if got := rewriteBodyMentions(tt.body, "old-", newPrefix); got != tt.want {
				t.Fatalf("rewriteBodyMentions(%q) = %q, want %q", tt.body, got, tt.want)
			}
		})
	}
}

// TestRewriteBodyMentionsRefusesAnEmptyOldPrefix pins the local guard against
// the worst input this function can be handed. strings.CutPrefix succeeds
// against "" for every token, so without the guard an empty old prefix
// retargets every SHORT-form mention in every body — and Execute re-renders the
// whole store in one pass with no rollback. BuildPlan refuses the empty prefix,
// but it is a function Execute never calls and both types are exported, so this
// pins the defense at the site that would do the damage.
func TestRewriteBodyMentionsRefusesAnEmptyOldPrefix(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"short forms only", "#a #b #c"},
		{"one short form", "#anything"},
		{"short and full form together", "see #abc and #old-abc here"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rewriteBodyMentions(tt.body, "", "new-"); got != tt.body {
				t.Fatalf("rewriteBodyMentions(%q, \"\", \"new-\") = %q, want the body unchanged", tt.body, got)
			}
		})
	}
}

// TestExecute_RewritesFullFormBodyMentions runs the whole executor over a store
// whose bodies mention the renamed nibs, and pins that the file on disk carries
// the retargeted prose.
func TestExecute_RewritesFullFormBodyMentions(t *testing.T) {
	root := t.TempDir()

	body := "Blocked on #tnib-bbb; see #tnib-bbb again.\n\nShort form #bbb stays.\n\n`#tnib-bbb` is literal.\n\n```\n#tnib-bbb\n```\n"
	newTestNib(t, root, "tnib-aaa--root.md", "tnib-aaa", "", nil, body)
	newTestNib(t, root, "tnib-bbb--child.md", "tnib-bbb", "tnib-aaa", nil, "child body with no mentions")

	snapshot := []NibSnapshot{
		{ID: "tnib-aaa", Path: "tnib-aaa--root.md"},
		{ID: "tnib-bbb", Path: "tnib-bbb--child.md", Parent: "tnib-aaa"},
	}
	plan, err := BuildPlan(snapshot, "tnib-", "new-", stubExists)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if err := Execute(plan, root); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := readNib(t, root, "new-aaa--root.md").Body
	// Render writes a blank line between the front matter and a body that
	// does not already start with one, and Parse hands that separator back
	// as part of the body — so the round-tripped body leads with "\n".
	want := "\nBlocked on #new-bbb; see #new-bbb again.\n\nShort form #bbb stays.\n\n`#tnib-bbb` is literal.\n\n```\n#tnib-bbb\n```"
	if got != want {
		t.Fatalf("rewritten body =\n%q\nwant\n%q", got, want)
	}
}

// TestExecute_MentionFreeBodyIsUnchangedByTheRewrite pins that the body-mention
// rewrite contributes nothing when it has nothing to move. Execute re-renders
// every file in one pass with no rollback, so a rewrite bug would corrupt prose
// store-wide; this is the guard that the splice is a no-op on a body with no
// full-form mention.
//
// It is deliberately NOT a claim that Execute preserves body bytes in general —
// it does not. nib.Parse trims one trailing "\n" and nib.Render appends one, so
// any body ending in a blank line loses that line on every re-render, this
// command's included. That asymmetry is pre-existing and lives in nib, not here;
// the fixture below ends in exactly one "\n" (a Render fixed point) so the
// comparison isolates the rewrite instead of measuring that normalization.
func TestExecute_MentionFreeBodyIsUnchangedByTheRewrite(t *testing.T) {
	root := t.TempDir()

	// Deliberately awkward prose: a bare sigil, an old-prefix string that is
	// not a mention, a code fence, an HTML block, trailing spaces and a table.
	body := "# Heading\n\nA bare # sigil, and the literal text tnib-aaa with no sigil.\n\n" +
		"| a | b |\n| - | - |\n| 1 | 2 |\n\n" +
		"<div class=\"x\">#tnib-aaa</div>\n\n" +
		"```go\nfmt.Println(\"#tnib-aaa\")\n```\n\n" +
		"trailing spaces here   \n\nemail#tnib-aaa is not a mention.\n"
	newTestNib(t, root, "tnib-aaa--root.md", "tnib-aaa", "", nil, body)

	before, err := os.ReadFile(filepath.Join(root, "tnib-aaa--root.md"))
	if err != nil {
		t.Fatalf("read before: %v", err)
	}

	plan, err := BuildPlan([]NibSnapshot{{ID: "tnib-aaa", Path: "tnib-aaa--root.md"}}, "tnib-", "new-", stubExists)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if err := Execute(plan, root); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	after, err := os.ReadFile(filepath.Join(root, "new-aaa--root.md"))
	if err != nil {
		t.Fatalf("read after: %v", err)
	}

	if gotBody, wantBody := bodyBytes(t, after), bodyBytes(t, before); !bytes.Equal(gotBody, wantBody) {
		t.Fatalf("the rewrite changed a body holding no full-form mention:\nbefore %q\nafter  %q", wantBody, gotBody)
	}
}

// bodyBytes returns everything after a rendered nib's closing front-matter
// fence, so a test can compare body bytes without the front matter the rename
// is supposed to change.
func bodyBytes(t *testing.T, file []byte) []byte {
	t.Helper()
	const closing = "\n---\n"
	idx := bytes.Index(file, []byte(closing))
	if idx < 0 {
		t.Fatalf("rendered nib has no closing front-matter fence: %q", file)
	}
	return file[idx+len(closing):]
}
