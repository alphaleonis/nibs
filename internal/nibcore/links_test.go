package nibcore

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
)

func TestFindIncomingLinks(t *testing.T) {
	core, _ := setupTestCore(t)

	// Create nibs with relationships
	// A blocks B (expressed as B.BlockedBy=[A,D])
	// A -> C (parent)
	// D blocks B (expressed as B.BlockedBy=[A,D])
	nibA := &nib.Nib{
		ID:     "aaa1",
		Title:  "Nib A",
		Status: "todo",
		Parent: "ccc3",
	}
	nibB := &nib.Nib{
		ID:        "bbb2",
		Title:     "Nib B",
		Status:    "todo",
		BlockedBy: []string{"aaa1", "ddd4"},
	}
	nibC := &nib.Nib{ID: "ccc3", Title: "Nib C", Status: "todo"}
	nibD := &nib.Nib{
		ID:     "ddd4",
		Title:  "Nib D",
		Status: "todo",
	}

	for _, b := range []*nib.Nib{nibA, nibB, nibC, nibD} {
		if err := core.Create(b); err != nil {
			t.Fatalf("Create error: %v", err)
		}
	}

	t.Run("multiple incoming blocked_by refs", func(t *testing.T) {
		// aaa1 is referenced in nibB's BlockedBy, so aaa1 has incoming blocked_by links
		links := core.FindIncomingLinks("aaa1")
		// nibB references aaa1 in BlockedBy, and nibA has parent=ccc3 (not incoming to aaa1)
		if len(links) != 1 {
			t.Errorf("FindIncomingLinks(aaa1) = %d links, want 1", len(links))
		}
		if len(links) > 0 && (links[0].FromNib.ID != "bbb2" || links[0].LinkType != "blocked_by") {
			t.Errorf("expected bbb2 -> aaa1 via blocked_by, got %s via %s", links[0].FromNib.ID, links[0].LinkType)
		}
	})

	t.Run("single incoming parent link", func(t *testing.T) {
		links := core.FindIncomingLinks("ccc3")
		if len(links) != 1 {
			t.Errorf("FindIncomingLinks(ccc3) = %d links, want 1", len(links))
		}
		if links[0].FromNib.ID != "aaa1" || links[0].LinkType != "parent" {
			t.Errorf("expected aaa1 -> ccc3 via parent, got %s -> ccc3 via %s", links[0].FromNib.ID, links[0].LinkType)
		}
	})

	t.Run("no incoming links", func(t *testing.T) {
		links := core.FindIncomingLinks("ddd4")
		// ddd4 is referenced in nibB's BlockedBy, so it has an incoming blocked_by link
		if len(links) != 1 {
			t.Errorf("FindIncomingLinks(ddd4) = %d links, want 1", len(links))
		}
	})

	t.Run("nib with no incoming links at all", func(t *testing.T) {
		// nibB is not referenced in any other nib's parent or BlockedBy
		links := core.FindIncomingLinks("bbb2")
		if len(links) != 0 {
			t.Errorf("FindIncomingLinks(bbb2) = %d links, want 0", len(links))
		}
	})

	t.Run("nonexistent nib", func(t *testing.T) {
		links := core.FindIncomingLinks("nonexistent")
		if len(links) != 0 {
			t.Errorf("FindIncomingLinks(nonexistent) = %d links, want 0", len(links))
		}
	})
}

func TestDetectCycle(t *testing.T) {
	core, _ := setupTestCore(t)

	// Create a chain: A blocks B, B blocks C (expressed via BlockedBy)
	// C.BlockedBy=[B], B.BlockedBy=[A]
	nibA := &nib.Nib{
		ID:     "aaa1",
		Title:  "Nib A",
		Status: "todo",
	}
	nibB := &nib.Nib{
		ID:        "bbb2",
		Title:     "Nib B",
		Status:    "todo",
		BlockedBy: []string{"aaa1"},
	}
	nibC := &nib.Nib{
		ID:        "ccc3",
		Title:     "Nib C",
		Status:    "todo",
		BlockedBy: []string{"bbb2"},
	}

	for _, b := range []*nib.Nib{nibA, nibB, nibC} {
		if err := core.Create(b); err != nil {
			t.Fatalf("Create error: %v", err)
		}
	}

	t.Run("would create cycle", func(t *testing.T) {
		// Adding A.BlockedBy=C would create: A -> C -> B -> A (via blocked_by chain)
		cycle := core.DetectCycle("aaa1", "blocked_by", "ccc3")
		if cycle == nil {
			t.Error("DetectCycle should return cycle path when cycle would be created")
		}
		if len(cycle) < 3 {
			t.Errorf("cycle path too short: %v", cycle)
		}
	})

	t.Run("no cycle", func(t *testing.T) {
		// Adding D.BlockedBy=A would not create a cycle (D doesn't exist in chain)
		nibD := &nib.Nib{ID: "ddd4", Title: "Nib D", Status: "todo"}
		if err := core.Create(nibD); err != nil {
			t.Fatalf("Create error: %v", err)
		}

		cycle := core.DetectCycle("ddd4", "blocked_by", "aaa1")
		if cycle != nil {
			t.Errorf("DetectCycle should return nil when no cycle, got: %v", cycle)
		}
	})

	t.Run("parent cycle detection", func(t *testing.T) {
		// Create parent chain: X -> Y -> Z (X has parent Y, Y has parent Z)
		nibX := &nib.Nib{
			ID:     "xxx1",
			Title:  "Nib X",
			Status: "todo",
			Parent: "yyy2",
		}
		nibY := &nib.Nib{
			ID:     "yyy2",
			Title:  "Nib Y",
			Status: "todo",
			Parent: "zzz3",
		}
		nibZ := &nib.Nib{
			ID:     "zzz3",
			Title:  "Nib Z",
			Status: "todo",
		}

		for _, b := range []*nib.Nib{nibX, nibY, nibZ} {
			if err := core.Create(b); err != nil {
				t.Fatalf("Create error: %v", err)
			}
		}

		// Adding Z parent of X would create: X -> Y -> Z -> X
		cycle := core.DetectCycle("zzz3", "parent", "xxx1")
		if cycle == nil {
			t.Error("DetectCycle should detect parent cycles")
		}
	})
}

func TestCheckAllLinks(t *testing.T) {
	core, _ := setupTestCore(t)

	// Create nibs with various link issues:
	// - Broken parent link to nonexistent nib (on nibA)
	// - Self-reference in blocked_by (on nibA)
	// - Cycle (A.BlockedBy=[B], B.BlockedBy=[A] via blocked_by)
	nibA := &nib.Nib{
		ID:        "aaa1",
		Title:     "Nib A",
		Status:    "todo",
		BlockedBy: []string{"bbb2", "aaa1"}, // aaa1 is self-reference
		Parent:    "nonexistent",
	}
	nibB := &nib.Nib{
		ID:        "bbb2",
		Title:     "Nib B",
		Status:    "todo",
		BlockedBy: []string{"aaa1"}, // creates cycle with nibA.BlockedBy=[bbb2]
	}

	for _, b := range []*nib.Nib{nibA, nibB} {
		if err := core.Create(b); err != nil {
			t.Fatalf("Create error: %v", err)
		}
	}

	result := core.CheckAllLinks()

	t.Run("detects broken links", func(t *testing.T) {
		if len(result.BrokenLinks) != 1 {
			t.Errorf("BrokenLinks = %d, want 1", len(result.BrokenLinks))
		}
		if len(result.BrokenLinks) > 0 {
			bl := result.BrokenLinks[0]
			if bl.NibID != "aaa1" || bl.LinkType != "parent" || bl.Target != "nonexistent" {
				t.Errorf("unexpected broken link: %+v", bl)
			}
		}
	})

	t.Run("detects self-references", func(t *testing.T) {
		if len(result.SelfLinks) != 1 {
			t.Errorf("SelfLinks = %d, want 1", len(result.SelfLinks))
		}
		if len(result.SelfLinks) > 0 {
			sl := result.SelfLinks[0]
			if sl.NibID != "aaa1" || sl.LinkType != "blocked_by" {
				t.Errorf("unexpected self-link: %+v", sl)
			}
		}
	})

	t.Run("detects cycles", func(t *testing.T) {
		if len(result.Cycles) != 1 {
			t.Errorf("Cycles = %d, want 1", len(result.Cycles))
		}
		if len(result.Cycles) > 0 {
			c := result.Cycles[0]
			if c.LinkType != "blocked_by" {
				t.Errorf("cycle link type = %q, want 'blocked_by'", c.LinkType)
			}
			if len(c.Path) < 3 {
				t.Errorf("cycle path too short: %v", c.Path)
			}
		}
	})

	t.Run("HasIssues returns true", func(t *testing.T) {
		if !result.HasIssues() {
			t.Error("HasIssues() should return true")
		}
	})

	t.Run("TotalIssues counts all", func(t *testing.T) {
		if result.TotalIssues() != 3 {
			t.Errorf("TotalIssues() = %d, want 3", result.TotalIssues())
		}
	})
}

func TestCheckAllLinksClean(t *testing.T) {
	core, _ := setupTestCore(t)

	// Create clean nibs with no issues
	nibA := &nib.Nib{
		ID:     "aaa1",
		Title:  "Nib A",
		Status: "todo",
	}
	nibB := &nib.Nib{
		ID:        "bbb2",
		Title:     "Nib B",
		Status:    "todo",
		BlockedBy: []string{"aaa1"},
	}

	for _, b := range []*nib.Nib{nibA, nibB} {
		if err := core.Create(b); err != nil {
			t.Fatalf("Create error: %v", err)
		}
	}

	result := core.CheckAllLinks()

	if result.HasIssues() {
		t.Errorf("HasIssues() should return false for clean nibs, got broken=%d self=%d cycles=%d",
			len(result.BrokenLinks), len(result.SelfLinks), len(result.Cycles))
	}
}

func TestRemoveLinksTo(t *testing.T) {
	core, _ := setupTestCore(t)

	// Create nibs where multiple nibs link to one target:
	// - nibA has parent=target (1 link to target)
	// - nibA has BlockedBy=[target] (1 link to target)
	// - nibB has BlockedBy=[target] (1 link to target)
	nibA := &nib.Nib{
		ID:        "aaa1",
		Title:     "Nib A",
		Status:    "todo",
		Parent:    "target",
		BlockedBy: []string{"target"},
	}
	nibB := &nib.Nib{
		ID:        "bbb2",
		Title:     "Nib B",
		Status:    "todo",
		BlockedBy: []string{"target"},
	}
	target := &nib.Nib{
		ID:     "target",
		Title:  "Target Nib",
		Status: "todo",
	}

	for _, b := range []*nib.Nib{nibA, nibB, target} {
		if err := core.Create(b); err != nil {
			t.Fatalf("Create error: %v", err)
		}
	}

	// Remove all links to target
	removed, err := core.RemoveLinksTo("target")
	if err != nil {
		t.Fatalf("RemoveLinksTo error: %v", err)
	}

	if removed != 3 {
		t.Errorf("removed = %d, want 3", removed)
	}

	// Verify links are gone
	loadedA, _ := core.Get("aaa1")
	if loadedA.Parent != "" || len(loadedA.BlockedBy) != 0 {
		t.Errorf("Nib A still has relationships: parent=%q blocked_by=%v", loadedA.Parent, loadedA.BlockedBy)
	}

	loadedB, _ := core.Get("bbb2")
	if len(loadedB.BlockedBy) != 0 {
		t.Errorf("Nib B still has %d blocked_by, want 0", len(loadedB.BlockedBy))
	}
}

func TestFixBrokenLinks(t *testing.T) {
	core, _ := setupTestCore(t)

	// Create nib with broken blocked_by link and self-reference
	nibA := &nib.Nib{
		ID:        "aaa1",
		Title:     "Nib A",
		Status:    "todo",
		BlockedBy: []string{"bbb2", "aaa1"}, // bbb2 is valid, aaa1 is self-reference
		Parent:    "nonexistent",             // broken
	}
	nibB := &nib.Nib{
		ID:     "bbb2",
		Title:  "Nib B",
		Status: "todo",
	}

	for _, b := range []*nib.Nib{nibA, nibB} {
		if err := core.Create(b); err != nil {
			t.Fatalf("Create error: %v", err)
		}
	}

	// Fix broken links
	fixed, err := core.FixBrokenLinks()
	if err != nil {
		t.Fatalf("FixBrokenLinks error: %v", err)
	}

	if fixed != 2 {
		t.Errorf("fixed = %d, want 2", fixed)
	}

	// Verify only valid blocked_by link remains
	loadedA, _ := core.Get("aaa1")
	if len(loadedA.BlockedBy) != 1 {
		t.Errorf("Nib A has %d blocked_by, want 1", len(loadedA.BlockedBy))
	}
	if !loadedA.IsBlockedBy("bbb2") {
		t.Error("valid 'blocked_by' link should be preserved")
	}
	if loadedA.Parent != "" {
		t.Errorf("broken parent should be removed, got %q", loadedA.Parent)
	}
}

// writeLinkNibFile writes a nib file into nibsDir under the `{id}--{slug}.md`
// naming the loader derives ID and Slug from, and returns its path. Tests that
// need a link spelled some particular way use it, because the writing
// resolvers normalize an id before storing it — a short spelling only ever
// reaches the store from a hand-edited file.
func writeLinkNibFile(t *testing.T, nibsDir, id, status, frontMatter string) string {
	t.Helper()
	path := filepath.Join(nibsDir, id+"--test.md")
	body := "---\ntitle: " + id + "\nstatus: " + status + "\ntype: task\n" + frontMatter + "---\n\nBody.\n"
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// hashFile returns a digest of the file's bytes, for asserting a file was left
// untouched without depending on its exact serialization.
func hashFile(t *testing.T, path string) [32]byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return sha256.Sum256(data)
}

// TestFixBrokenLinksLeavesResolvableShortIDsOnDisk is the on-disk half of the
// short-id resolution rule: a parent or blocked_by entry that resolves is not
// broken, so `nibs check` reports nothing and `nibs check --fix` writes
// nothing. It asserts the file bytes rather than only the report, because the
// defect this pins was a write — --fix deleted a live blocked_by edge from the
// file, after which the nib was handed out as ready work.
//
// The links are left spelled as they were written; a resolvable link is simply
// not an issue, and --fix does not canonicalize stored ids.
func TestFixBrokenLinksLeavesResolvableShortIDsOnDisk(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	writeLinkNibFile(t, nibsDir, "nibs-par", "todo", "")
	writeLinkNibFile(t, nibsDir, "nibs-blk", "in-progress", "")
	depPath := writeLinkNibFile(t, nibsDir, "nibs-dep", "todo", "parent: par\nblocked_by: [blk]\n")
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// The premise: the short ids name real nibs. Without this the test would
	// pass for the wrong reason.
	if got, err := core.Get("blk"); err != nil || got.ID != "nibs-blk" {
		t.Fatalf(`Get("blk") = %v, %v; want nibs-blk, nil`, got, err)
	}

	if result := core.CheckAllLinks(); result.HasIssues() {
		t.Errorf("CheckAllLinks() reported issues on resolvable short ids: broken=%+v self=%+v cycles=%+v documents=%+v",
			result.BrokenLinks, result.SelfLinks, result.Cycles, result.BrokenDocuments)
	}

	before := hashFile(t, depPath)
	fixed, err := core.FixBrokenLinks()
	if err != nil {
		t.Fatalf("FixBrokenLinks: %v", err)
	}
	if fixed != 0 {
		t.Errorf("FixBrokenLinks() = %d, want 0 — nothing here is broken", fixed)
	}
	if after := hashFile(t, depPath); after != before {
		data, _ := os.ReadFile(depPath)
		t.Errorf("FixBrokenLinks rewrote %s; file is now:\n%s", filepath.Base(depPath), data)
	}

	// The edge is still load-bearing: nibs-blk is in-progress, so the
	// dependent stays blocked and out of the ready queue.
	if !core.IsBlocked("nibs-dep") {
		t.Error(`IsBlocked("nibs-dep") = false, want true — its blocker "blk" resolves to the in-progress nibs-blk`)
	}
	if dep, err := core.Get("nibs-dep"); err != nil {
		t.Fatalf(`Get("nibs-dep"): %v`, err)
	} else if dep.Parent != "par" || len(dep.BlockedBy) != 1 || dep.BlockedBy[0] != "blk" {
		t.Errorf("stored links = parent %q, blocked_by %v; want them left as written (%q, %v)",
			dep.Parent, dep.BlockedBy, "par", []string{"blk"})
	}
}

// TestFixBrokenLinksStillRemovesUnresolvableLinks is the guard against an
// over-broad fix: a target that names no nib under either spelling is still
// broken, is still reported, and is still removed from the file. Both
// spellings are driven because prefix-prepending gives an unresolvable short
// id a second chance to be found, and neither chance may succeed here.
func TestFixBrokenLinksStillRemovesUnresolvableLinks(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	writeLinkNibFile(t, nibsDir, "nibs-blk", "in-progress", "")
	// "nope" and "nibs-nope" are the short and full spellings of an id no nib
	// answers to. "blk" resolves and must survive.
	writeLinkNibFile(t, nibsDir, "nibs-shortparent", "todo", "parent: nope\n")
	writeLinkNibFile(t, nibsDir, "nibs-fullparent", "todo", "parent: nibs-nope\n")
	depPath := writeLinkNibFile(t, nibsDir, "nibs-dep", "todo", "blocked_by: [nope, nibs-nope, blk]\n")
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	result := core.CheckAllLinks()
	gotBroken := map[string]bool{}
	for _, bl := range result.BrokenLinks {
		gotBroken[bl.NibID+":"+bl.Target] = true
	}
	for _, want := range []string{
		"nibs-shortparent:nope",
		"nibs-fullparent:nibs-nope",
		"nibs-dep:nope",
		"nibs-dep:nibs-nope",
	} {
		if !gotBroken[want] {
			t.Errorf("CheckAllLinks() did not report %q as broken; reported %+v", want, result.BrokenLinks)
		}
	}
	if len(result.BrokenLinks) != 4 {
		t.Errorf("broken links = %d, want 4: %+v", len(result.BrokenLinks), result.BrokenLinks)
	}

	before := hashFile(t, depPath)
	fixed, err := core.FixBrokenLinks()
	if err != nil {
		t.Fatalf("FixBrokenLinks: %v", err)
	}
	if fixed != 4 {
		t.Errorf("FixBrokenLinks() = %d, want 4", fixed)
	}
	if after := hashFile(t, depPath); after == before {
		t.Error("FixBrokenLinks left the dependent file untouched; the unresolvable blocked_by entries were not removed from disk")
	}

	for _, id := range []string{"nibs-shortparent", "nibs-fullparent"} {
		b, err := core.Get(id)
		if err != nil {
			t.Fatalf("Get(%q): %v", id, err)
		}
		if b.Parent != "" {
			t.Errorf("%s parent = %q, want it removed", id, b.Parent)
		}
	}
	dep, err := core.Get("nibs-dep")
	if err != nil {
		t.Fatalf(`Get("nibs-dep"): %v`, err)
	}
	if len(dep.BlockedBy) != 1 || dep.BlockedBy[0] != "blk" {
		t.Errorf("nibs-dep blocked_by = %v, want [blk] — only the resolvable entry survives", dep.BlockedBy)
	}
	if result := core.CheckAllLinks(); result.HasIssues() {
		t.Errorf("issues remain after --fix: broken=%+v self=%+v", result.BrokenLinks, result.SelfLinks)
	}
}

// TestCheckAllLinksReportsShortSelfReferenceAsSelfLink pins the categorization
// a bare map lookup got wrong: a nib naming itself by short id was reported as
// a broken link. --fix removes both categories, so the file ends up the same
// either way — the report is what was wrong.
func TestCheckAllLinksReportsShortSelfReferenceAsSelfLink(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	writeLinkNibFile(t, nibsDir, "nibs-slf", "todo", "parent: slf\nblocked_by: [slf]\n")
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	result := core.CheckAllLinks()
	if len(result.BrokenLinks) != 0 {
		t.Errorf("broken links = %+v, want none — the short id names the nib itself", result.BrokenLinks)
	}
	gotSelf := map[string]bool{}
	for _, sl := range result.SelfLinks {
		gotSelf[sl.LinkType] = true
	}
	if !gotSelf["parent"] || !gotSelf["blocked_by"] || len(result.SelfLinks) != 2 {
		t.Errorf("self links = %+v, want one parent and one blocked_by", result.SelfLinks)
	}

	// Still removed, as self links always were.
	fixed, err := core.FixBrokenLinks()
	if err != nil {
		t.Fatalf("FixBrokenLinks: %v", err)
	}
	if fixed != 2 {
		t.Errorf("FixBrokenLinks() = %d, want 2", fixed)
	}
	b, err := core.Get("nibs-slf")
	if err != nil {
		t.Fatalf(`Get("nibs-slf"): %v`, err)
	}
	if b.Parent != "" || len(b.BlockedBy) != 0 {
		t.Errorf("self links survived --fix: parent=%q blocked_by=%v", b.Parent, b.BlockedBy)
	}
}

func TestLinkCheckResultMethods(t *testing.T) {
	t.Run("empty result", func(t *testing.T) {
		r := &LinkCheckResult{
			BrokenLinks: []BrokenLink{},
			SelfLinks:   []SelfLink{},
			Cycles:      []Cycle{},
		}
		if r.HasIssues() {
			t.Error("empty result should not have issues")
		}
		if r.TotalIssues() != 0 {
			t.Errorf("TotalIssues() = %d, want 0", r.TotalIssues())
		}
	})

	t.Run("with issues", func(t *testing.T) {
		r := &LinkCheckResult{
			BrokenLinks: []BrokenLink{{NibID: "a", LinkType: "blocked_by", Target: "x"}},
			SelfLinks:   []SelfLink{{NibID: "b", LinkType: "parent"}},
			Cycles:      []Cycle{{LinkType: "blocked_by", Path: []string{"a", "b", "a"}}},
		}
		if !r.HasIssues() {
			t.Error("result with issues should have issues")
		}
		if r.TotalIssues() != 3 {
			t.Errorf("TotalIssues() = %d, want 3", r.TotalIssues())
		}
	})
}

func TestCanonicalCycleKey(t *testing.T) {
	tests := []struct {
		path []string
		want string
	}{
		{[]string{"a", "b", "c", "a"}, "a->b->c"},
		{[]string{"c", "a", "b", "c"}, "a->b->c"}, // same cycle, different start
		{[]string{"b", "c", "a", "b"}, "a->b->c"}, // same cycle, different start
		{[]string{"x", "y", "x"}, "x->y"},
		{[]string{"a"}, ""},
		{[]string{}, ""},
	}

	for _, tt := range tests {
		got := canonicalCycleKey(tt.path)
		if got != tt.want {
			t.Errorf("canonicalCycleKey(%v) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestIsBlocked(t *testing.T) {
	core, _ := setupTestCore(t)

	// Create test nibs with various blocking scenarios
	// All blocking relationships are expressed via BlockedBy on the target nibs.
	activeBlocker := &nib.Nib{
		ID:     "active-blocker",
		Title:  "Active Blocker",
		Status: "todo",
	}
	completedBlocker := &nib.Nib{
		ID:     "completed-blocker",
		Title:  "Completed Blocker",
		Status: "completed",
	}
	scrappedBlocker := &nib.Nib{
		ID:     "scrapped-blocker",
		Title:  "Scrapped Blocker",
		Status: "scrapped",
	}
	blockedByActive := &nib.Nib{
		ID:        "blocked-by-active",
		Title:     "Blocked by Active",
		Status:    "todo",
		BlockedBy: []string{"active-blocker"},
	}
	blockedByCompleted := &nib.Nib{
		ID:        "blocked-by-completed",
		Title:     "Blocked by Completed",
		Status:    "todo",
		BlockedBy: []string{"completed-blocker"},
	}
	blockedByScrapped := &nib.Nib{
		ID:        "blocked-by-scrapped",
		Title:     "Blocked by Scrapped",
		Status:    "todo",
		BlockedBy: []string{"scrapped-blocker"},
	}
	notBlocked := &nib.Nib{
		ID:     "not-blocked",
		Title:  "Not Blocked",
		Status: "todo",
	}
	// Nib with broken blocker link
	blockedByBroken := &nib.Nib{
		ID:        "blocked-by-broken",
		Title:     "Blocked by Broken Link",
		Status:    "todo",
		BlockedBy: []string{"nonexistent"},
	}
	// Nib with multiple blockers (one active, one completed)
	mixedBlockers := &nib.Nib{
		ID:        "mixed-blockers",
		Title:     "Mixed Blockers",
		Status:    "todo",
		BlockedBy: []string{"active-blocker", "completed-blocker"},
	}
	// Nib with multiple blockers (all completed)
	allResolvedBlockers := &nib.Nib{
		ID:        "all-resolved-blockers",
		Title:     "All Resolved Blockers",
		Status:    "todo",
		BlockedBy: []string{"completed-blocker", "scrapped-blocker"},
	}

	nibs := []*nib.Nib{
		activeBlocker, completedBlocker, scrappedBlocker,
		blockedByActive, blockedByCompleted, blockedByScrapped,
		notBlocked, blockedByBroken, mixedBlockers, allResolvedBlockers,
	}
	for _, b := range nibs {
		if err := core.Create(b); err != nil {
			t.Fatalf("Create error: %v", err)
		}
	}

	tests := []struct {
		name  string
		nibID string
		want  bool
	}{
		{"blocked by active blocker", "blocked-by-active", true},
		{"blocked by completed blocker", "blocked-by-completed", false},
		{"blocked by scrapped blocker", "blocked-by-scrapped", false},
		{"not blocked", "not-blocked", false},
		{"broken blocker link", "blocked-by-broken", false},
		{"mixed blockers (one active)", "mixed-blockers", true},
		{"all resolved blockers", "all-resolved-blockers", false},
		{"nonexistent nib", "nonexistent", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := core.IsBlocked(tt.nibID)
			if got != tt.want {
				t.Errorf("IsBlocked(%q) = %v, want %v", tt.nibID, got, tt.want)
			}
		})
	}
}

func TestIsBlocking(t *testing.T) {
	core, _ := setupTestCore(t)

	// All blocking relationships are expressed via BlockedBy on target nibs.
	// IsBlocking scans other nibs' BlockedBy to determine if a nib is blocking anything.
	activeBlocker := &nib.Nib{
		ID:     "active-blocker",
		Title:  "Active Blocker",
		Status: "todo",
	}
	completedBlocker := &nib.Nib{
		ID:     "completed-blocking",
		Title:  "Completed Blocking",
		Status: "completed",
	}
	// Active target that is blocked by both active-blocker and completed-blocking
	activeTarget := &nib.Nib{
		ID:        "active-target",
		Title:     "Active Target",
		Status:    "todo",
		BlockedBy: []string{"active-blocker", "completed-blocking"},
	}
	// Completed target that is blocked by active-blocker (but target is resolved, so skip)
	completedTarget := &nib.Nib{
		ID:        "completed-target",
		Title:     "Completed Target",
		Status:    "completed",
		BlockedBy: []string{"active-blocker"},
	}
	// Nib that doesn't block anything
	notBlocking := &nib.Nib{
		ID:     "not-blocking",
		Title:  "Not Blocking",
		Status: "todo",
	}

	nibs := []*nib.Nib{
		activeBlocker, completedBlocker,
		activeTarget, completedTarget, notBlocking,
	}
	for _, b := range nibs {
		if err := core.Create(b); err != nil {
			t.Fatalf("Create error: %v", err)
		}
	}

	tests := []struct {
		name  string
		nibID string
		want  bool
	}{
		{"active nib blocking active target", "active-blocker", true},
		{"completed nib blocking active target", "completed-blocking", false},
		{"not blocking anything", "not-blocking", false},
		{"nonexistent nib", "nonexistent", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := core.IsBlocking(tt.nibID)
			if got != tt.want {
				t.Errorf("IsBlocking(%q) = %v, want %v", tt.nibID, got, tt.want)
			}
		})
	}
}

func TestFindActiveBlockers(t *testing.T) {
	core, _ := setupTestCore(t)

	// Create test nibs - all blocking expressed via BlockedBy on target
	activeBlocker1 := &nib.Nib{
		ID:     "active-blocker-1",
		Title:  "Active Blocker 1",
		Status: "in-progress",
	}
	activeBlocker2 := &nib.Nib{
		ID:     "active-blocker-2",
		Title:  "Active Blocker 2",
		Status: "todo",
	}
	completedBlocker := &nib.Nib{
		ID:     "completed-blocker",
		Title:  "Completed Blocker",
		Status: "completed",
	}
	target := &nib.Nib{
		ID:        "target",
		Title:     "Target Nib",
		Status:    "todo",
		BlockedBy: []string{"active-blocker-1", "active-blocker-2", "completed-blocker"},
	}
	noBlockers := &nib.Nib{
		ID:     "no-blockers",
		Title:  "No Blockers",
		Status: "todo",
	}

	nibs := []*nib.Nib{activeBlocker1, activeBlocker2, completedBlocker, target, noBlockers}
	for _, b := range nibs {
		if err := core.Create(b); err != nil {
			t.Fatalf("Create error: %v", err)
		}
	}

	t.Run("returns active blockers from BlockedBy field", func(t *testing.T) {
		blockers := core.FindActiveBlockers("target")
		if len(blockers) != 2 {
			t.Errorf("FindActiveBlockers() returned %d blockers, want 2", len(blockers))
		}
		// Check that both active blockers are present
		ids := make(map[string]bool)
		for _, b := range blockers {
			ids[b.ID] = true
		}
		if !ids["active-blocker-1"] {
			t.Error("expected active-blocker-1 in result")
		}
		if !ids["active-blocker-2"] {
			t.Error("expected active-blocker-2 in result")
		}
		if ids["completed-blocker"] {
			t.Error("completed-blocker should not be in result")
		}
	})

	t.Run("returns nil for nib with no blockers", func(t *testing.T) {
		blockers := core.FindActiveBlockers("no-blockers")
		if len(blockers) != 0 {
			t.Errorf("FindActiveBlockers() returned %d blockers, want 0", len(blockers))
		}
	})

	t.Run("returns nil for nonexistent nib", func(t *testing.T) {
		blockers := core.FindActiveBlockers("nonexistent")
		if blockers != nil {
			t.Errorf("FindActiveBlockers() returned %v, want nil", blockers)
		}
	})
}

// TestBlockerLookupResolvesShortID drives the short-id blocker through a real
// prefixed Core, so the config prefix is the one the Core carries rather than a
// literal handed to the pure function. Both blocking queries have to see the
// blocker, and Get has to resolve the same short id to the same nib — that
// agreement is what keeps `nibs list --ready` (which reaches IsBlocked) from
// handing out a nib the projected `ready` field (which reaches Get) withholds.
func TestBlockerLookupResolvesShortID(t *testing.T) {
	core, _ := mustLoadPrefixedCore(t)

	blocker := &nib.Nib{ID: "nibs-blk", Title: "Blocker", Status: "in-progress"}
	// Spelled short, as a hand-edited nib file may spell it: the write
	// resolvers normalize a blocker id before it reaches the store.
	dep := &nib.Nib{ID: "nibs-dep", Title: "Dependent", Status: "todo", BlockedBy: []string{"blk"}}
	for _, b := range []*nib.Nib{blocker, dep} {
		if err := core.Create(b); err != nil {
			t.Fatalf("Create(%s): %v", b.ID, err)
		}
	}

	if got, err := core.Get("blk"); err != nil || got.ID != "nibs-blk" {
		t.Fatalf(`Get("blk") = %v, %v; want nibs-blk, nil — the premise that the short id resolves at all`, got, err)
	}
	if !core.IsBlocked("nibs-dep") {
		t.Error(`IsBlocked("nibs-dep") = false, want true — its blocker is named "blk", which resolves to the in-progress nibs-blk`)
	}
	blockers := core.FindActiveBlockers("nibs-dep")
	if len(blockers) != 1 || blockers[0].ID != "nibs-blk" {
		t.Errorf("FindActiveBlockers(\"nibs-dep\") = %v, want [nibs-blk]", blockers)
	}
}

// TestCoreReleasesDependentsPredicate covers the seam the pure link queries
// depend on: the Core wrappers hand them the config-derived releasing set,
// never a list of their own. deferred is the case that separates this from the
// closed set — it is closed but must still block. It runs against a config-less
// Core too, since several tests build one and the predicate must still answer
// (Config.StatusReleasesDependents is receiver-free).
func TestCoreReleasesDependentsPredicate(t *testing.T) {
	tests := []struct {
		status string
		want   bool
	}{
		{"completed", true},
		{"scrapped", true},
		{"todo", false},
		{"in-progress", false},
		{"draft", false},
		{"deferred", false},
		{"", false},
	}

	cores := map[string]*Core{
		"with config": New(t.TempDir(), config.Default()),
		"config-less": New(t.TempDir(), nil),
	}

	for name, core := range cores {
		t.Run(name, func(t *testing.T) {
			releasesDependents := core.releasesDependentsPredicate()
			for _, tt := range tests {
				if got := releasesDependents(tt.status); got != tt.want {
					t.Errorf("releasesDependentsPredicate()(%q) = %v, want %v", tt.status, got, tt.want)
				}
			}
		})
	}
}

