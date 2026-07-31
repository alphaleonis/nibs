package nibcore

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/fsnotify/fsnotify"
)

// linkTargets returns the ids that link TO targetID, one entry per incoming
// link, so a reverse traversal can be asserted without depending on map order.
func linkTargets(t *testing.T, core *Core, targetID, linkType string) []string {
	t.Helper()
	var ids []string
	for _, l := range core.FindIncomingLinks(targetID) {
		if l.LinkType == linkType {
			ids = append(ids, l.FromNib.ID)
		}
	}
	slices.Sort(ids)
	return ids
}

// TestLoadCanonicalizesShortFormLinks is the primary guard for nibs-lzch: a
// hand-written short-form `parent`/`blocked_by` used to resolve when followed
// FORWARD from the nib holding it and be invisible from the other end, because
// every reverse traversal walks exact map keys. Canonicalizing at the disk-read
// boundary makes both directions agree.
//
// It asserts the reverse direction (which was broken) AND the forward direction
// (which must keep working), plus the stored spelling itself — the mechanism
// that fixes the traversals.
func TestLoadCanonicalizesShortFormLinks(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	writeLinkNibFile(t, nibsDir, "nibs-par", "todo", "")
	writeLinkNibFile(t, nibsDir, "nibs-blk", "in-progress", "")
	writeLinkNibFile(t, nibsDir, "nibs-dep", "todo", "parent: par\nblocked_by: [blk]\n")
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Premise: the short ids name real nibs. Without this the reverse
	// assertions below could pass for the wrong reason.
	if got, err := core.Get("par"); err != nil || got.ID != "nibs-par" {
		t.Fatalf(`Get("par") = %v, %v; want nibs-par, nil`, got, err)
	}

	dep, err := core.Get("nibs-dep")
	if err != nil {
		t.Fatalf(`Get("nibs-dep"): %v`, err)
	}
	if dep.Parent != "nibs-par" {
		t.Errorf("stored parent = %q, want %q — the short form must be resolved on load", dep.Parent, "nibs-par")
	}
	if len(dep.BlockedBy) != 1 || dep.BlockedBy[0] != "nibs-blk" {
		t.Errorf("stored blocked_by = %v, want [nibs-blk]", dep.BlockedBy)
	}

	// Reverse traversals — the half that was invisible.
	if got := linkTargets(t, core, "nibs-par", "parent"); !slices.Equal(got, []string{"nibs-dep"}) {
		t.Errorf("FindIncomingLinks(nibs-par) parent sources = %v, want [nibs-dep]", got)
	}
	if got := linkTargets(t, core, "nibs-blk", "blocked_by"); !slices.Equal(got, []string{"nibs-dep"}) {
		t.Errorf("FindIncomingLinks(nibs-blk) blocked_by sources = %v, want [nibs-dep]", got)
	}
	if !core.IsBlocking("nibs-blk") {
		t.Error(`IsBlocking("nibs-blk") = false, want true — nibs-dep names it as a blocker`)
	}

	// Forward traversal must keep working.
	if !core.IsBlocked("nibs-dep") {
		t.Error(`IsBlocked("nibs-dep") = false, want true`)
	}
}

// TestLoadCanonicalizationDetectsShortFormCycle covers the silent-detection half
// of nibs-lzch: two nibs each naming the other as parent in short form formed a
// real cycle for the forward resolver while `nibs check` reported nothing,
// because FindCyclesInMap walks exact map keys.
func TestLoadCanonicalizationDetectsShortFormCycle(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	writeLinkNibFile(t, nibsDir, "nibs-pxx", "todo", "parent: pyy\n")
	writeLinkNibFile(t, nibsDir, "nibs-pyy", "todo", "parent: pxx\n")
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	result := core.CheckAllLinks()
	if len(result.Cycles) == 0 {
		t.Fatalf("CheckAllLinks() reported no cycles; want the nibs-pxx <-> nibs-pyy parent cycle (result: %+v)", result)
	}
	found := false
	for _, cyc := range result.Cycles {
		if cyc.LinkType == "parent" &&
			slices.Contains(cyc.Path, "nibs-pxx") && slices.Contains(cyc.Path, "nibs-pyy") {
			found = true
		}
	}
	if !found {
		t.Errorf("cycles = %+v; want a parent cycle through nibs-pxx and nibs-pyy", result.Cycles)
	}
}

// TestCanonicalizationLeavesUnresolvableShortIDVerbatim is the counterweight:
// an id that names no nib under either spelling cannot be canonicalized, so it
// must survive exactly as written and stay reported as broken. Both spellings
// are driven because prefix-prepending gives a short id a second chance to
// resolve, and neither chance may succeed here.
func TestCanonicalizationLeavesUnresolvableShortIDVerbatim(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	writeLinkNibFile(t, nibsDir, "nibs-blk", "in-progress", "")
	writeLinkNibFile(t, nibsDir, "nibs-orphan", "todo", "parent: nope\nblocked_by: [nibs-nope, blk]\n")
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	orphan, err := core.Get("nibs-orphan")
	if err != nil {
		t.Fatalf(`Get("nibs-orphan"): %v`, err)
	}
	if orphan.Parent != "nope" {
		t.Errorf("parent = %q, want it left verbatim as %q", orphan.Parent, "nope")
	}
	// The resolvable entry canonicalizes; the unresolvable one does not.
	if !slices.Equal(orphan.BlockedBy, []string{"nibs-nope", "nibs-blk"}) {
		t.Errorf("blocked_by = %v, want [nibs-nope nibs-blk]", orphan.BlockedBy)
	}

	result := core.CheckAllLinks()
	gotBroken := map[string]bool{}
	for _, bl := range result.BrokenLinks {
		gotBroken[bl.NibID+":"+bl.Target] = true
	}
	for _, want := range []string{"nibs-orphan:nope", "nibs-orphan:nibs-nope"} {
		if !gotBroken[want] {
			t.Errorf("CheckAllLinks() did not report %q as broken; reported %+v", want, result.BrokenLinks)
		}
	}
	if len(result.BrokenLinks) != 2 {
		t.Errorf("broken links = %d, want 2: %+v", len(result.BrokenLinks), result.BrokenLinks)
	}
}

// TestCanonicalizationDoesNotRewriteFilesOnLoad pins the settled design point:
// canonicalization is in-memory only. Rewriting on load would touch files the
// user did not edit and fight the file watcher.
func TestCanonicalizationDoesNotRewriteFilesOnLoad(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	writeLinkNibFile(t, nibsDir, "nibs-par", "todo", "")
	depPath := writeLinkNibFile(t, nibsDir, "nibs-dep", "todo", "parent: par\n")
	before := hashFile(t, depPath)

	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if dep, _ := core.Get("nibs-dep"); dep == nil || dep.Parent != "nibs-par" {
		t.Fatalf("premise failed: in-memory parent was not canonicalized (%+v)", dep)
	}
	if after := hashFile(t, depPath); after != before {
		data, _ := os.ReadFile(depPath)
		t.Errorf("Load rewrote %s; file is now:\n%s", filepath.Base(depPath), data)
	}
}

// TestCanonicalizationDedupesResolvedDuplicates covers the one way
// canonicalization can collapse entries: two spellings of the same blocker
// resolve to one id. Keeping both would render a duplicated `blocked_by` on the
// next save and double-count the edge in every reverse traversal.
func TestCanonicalizationDedupesResolvedDuplicates(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	writeLinkNibFile(t, nibsDir, "nibs-blk", "in-progress", "")
	writeLinkNibFile(t, nibsDir, "nibs-dep", "todo", "blocked_by: [blk, nibs-blk]\n")
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	dep, err := core.Get("nibs-dep")
	if err != nil {
		t.Fatalf(`Get("nibs-dep"): %v`, err)
	}
	if !slices.Equal(dep.BlockedBy, []string{"nibs-blk"}) {
		t.Errorf("blocked_by = %v, want [nibs-blk] — both spellings name the same nib", dep.BlockedBy)
	}
	if got := linkTargets(t, core, "nibs-blk", "blocked_by"); !slices.Equal(got, []string{"nibs-dep"}) {
		t.Errorf("incoming blocked_by sources = %v, want [nibs-dep] exactly once", got)
	}
}

// TestShortFormIfMatchUpdateDoesNotFalseConflict guards the etag consequence of
// canonicalizing in memory: the in-memory nib now renders full ids while the
// file still holds the short form, so the stored etag must be computed the same
// way or every if-match Update on a hand-edited nib would conflict forever with
// no on-disk change.
func TestShortFormIfMatchUpdateDoesNotFalseConflict(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	// Timestamps are spelled out because loadNib synthesizes a missing created_at
	// from the file mtime while computeStoredETag's bare parse does not — a
	// separate, pre-existing divergence that would mask the one under test.
	const stamps = "created_at: 2026-07-30T10:00:00Z\nupdated_at: 2026-07-30T10:00:00Z\n"
	writeLinkNibFile(t, nibsDir, "nibs-par", "todo", stamps)
	writeLinkNibFile(t, nibsDir, "nibs-dep", "todo", stamps+"parent: par\n")
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	dep, err := core.Get("nibs-dep")
	if err != nil {
		t.Fatalf(`Get("nibs-dep"): %v`, err)
	}
	etag := dep.ETag()

	current, err := core.CurrentETag("nibs-dep")
	if err != nil {
		t.Fatalf("CurrentETag: %v", err)
	}
	if current != etag {
		t.Errorf("CurrentETag = %q, in-memory ETag = %q; a short-form file must not diverge from its canonicalized nib", current, etag)
	}

	edited := dep.Clone()
	edited.Title = "Retitled"
	if err := core.Update(edited, &etag); err != nil {
		t.Fatalf("Update with the nib's own etag failed: %v", err)
	}
}

// TestWatcherReloadCanonicalizesLinks covers the incremental path: a single file
// arriving after the initial load must canonicalize against the current store,
// not wait for a full reload.
func TestWatcherReloadCanonicalizesLinks(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	writeLinkNibFile(t, nibsDir, "nibs-par", "todo", "")
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	setWatching(core)

	depPath := writeLinkNibFile(t, nibsDir, "nibs-dep", "todo", "parent: par\n")
	core.handleChanges(map[string]fsnotify.Op{depPath: fsnotify.Create})

	dep, err := core.Get("nibs-dep")
	if err != nil {
		t.Fatalf(`Get("nibs-dep") after watcher create: %v`, err)
	}
	if dep.Parent != "nibs-par" {
		t.Errorf("parent = %q, want %q", dep.Parent, "nibs-par")
	}
	if got := linkTargets(t, core, "nibs-par", "parent"); !slices.Equal(got, []string{"nibs-dep"}) {
		t.Errorf("FindIncomingLinks(nibs-par) parent sources = %v, want [nibs-dep]", got)
	}
}

// TestWatcherCanonicalizesWhenTargetArrivesLater is the ordering case the
// single-file path cannot cover on its own: the link is written BEFORE the nib
// it names exists, so it is genuinely unresolvable at load and must stay
// verbatim. Once the target's file appears, the store has to revisit it — the
// forward resolver would still answer, so without this the reverse half stays
// invisible until the next full reload.
func TestWatcherCanonicalizesWhenTargetArrivesLater(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	writeLinkNibFile(t, nibsDir, "nibs-dep", "todo", "parent: par\n")
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Premise: with no target loaded the link is unresolvable and stays as written.
	if dep, _ := core.Get("nibs-dep"); dep == nil || dep.Parent != "par" {
		t.Fatalf("premise failed: parent = %+v, want it left verbatim as \"par\"", dep)
	}
	setWatching(core)

	parPath := writeLinkNibFile(t, nibsDir, "nibs-par", "todo", "")
	core.handleChanges(map[string]fsnotify.Op{parPath: fsnotify.Create})

	dep, err := core.Get("nibs-dep")
	if err != nil {
		t.Fatalf(`Get("nibs-dep"): %v`, err)
	}
	if dep.Parent != "nibs-par" {
		t.Errorf("parent = %q, want %q once the target exists", dep.Parent, "nibs-par")
	}
	if got := linkTargets(t, core, "nibs-par", "parent"); !slices.Equal(got, []string{"nibs-dep"}) {
		t.Errorf("FindIncomingLinks(nibs-par) parent sources = %v, want [nibs-dep]", got)
	}
}

// TestWatcherCanonicalizesWithinOneBatch drives both files in a SINGLE debounce
// batch. handleChanges ranges a map, so the two arrive in an arbitrary order and
// the dependent may well be loaded before its target is in the store —
// canonicalization therefore cannot happen per file as it is loaded.
func TestWatcherCanonicalizesWithinOneBatch(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	setWatching(core)

	depPath := writeLinkNibFile(t, nibsDir, "nibs-dep", "todo", "parent: par\n")
	parPath := writeLinkNibFile(t, nibsDir, "nibs-par", "todo", "")
	core.handleChanges(map[string]fsnotify.Op{
		depPath: fsnotify.Create,
		parPath: fsnotify.Create,
	})

	dep, err := core.Get("nibs-dep")
	if err != nil {
		t.Fatalf(`Get("nibs-dep"): %v`, err)
	}
	if dep.Parent != "nibs-par" {
		t.Errorf("parent = %q, want %q regardless of intra-batch order", dep.Parent, "nibs-par")
	}
	if got := linkTargets(t, core, "nibs-par", "parent"); !slices.Equal(got, []string{"nibs-dep"}) {
		t.Errorf("FindIncomingLinks(nibs-par) parent sources = %v, want [nibs-dep]", got)
	}
}

// TestWatcherCanonicalizationPublishesOneEventPerNib pins the event shape of the
// batch pass: a nib canonicalized as part of its own arrival keeps its single
// created/updated event carrying the canonicalized payload, rather than picking
// up a second, contradictory one.
func TestWatcherCanonicalizationPublishesOneEventPerNib(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	writeLinkNibFile(t, nibsDir, "nibs-par", "todo", "")
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	setWatching(core)
	ch, unsub := core.Subscribe()
	defer unsub()

	depPath := writeLinkNibFile(t, nibsDir, "nibs-dep", "todo", "parent: par\n")
	core.handleChanges(map[string]fsnotify.Op{depPath: fsnotify.Create})

	events := collectNibEvents(t, ch, "nibs-dep", 150*time.Millisecond)
	if len(events) != 1 {
		t.Fatalf("got %d events for nibs-dep, want exactly 1: %+v", len(events), events)
	}
	if events[0].Type != EventCreated {
		t.Errorf("event type = %s, want %s", events[0].Type, EventCreated)
	}
	if events[0].Nib == nil || events[0].Nib.Parent != "nibs-par" {
		t.Errorf("published payload parent = %+v, want the canonicalized nibs-par", events[0].Nib)
	}
}

// TestCanonicalizeLinksInMapPure exercises the pure helper directly, including
// the shapes the Core-level tests cannot reach cheaply: a v0 `blocking:` list
// and a no-op on an already-canonical nib.
func TestCanonicalizeLinksInMapPure(t *testing.T) {
	nibs := map[string]*nib.Nib{
		"nibs-aaa1": {ID: "nibs-aaa1", Title: "A", Status: "todo"},
		"nibs-bbb2": {ID: "nibs-bbb2", Title: "B", Status: "todo"},
	}

	tests := []struct {
		name          string
		in            *nib.Nib
		wantChanged   bool
		wantParent    string
		wantBlockedBy []string
		wantBlocking  []string
	}{
		{
			name:        "already canonical is a no-op",
			in:          &nib.Nib{ID: "nibs-ccc3", Parent: "nibs-aaa1", BlockedBy: []string{"nibs-bbb2"}},
			wantChanged: false,
		},
		{
			name:          "short parent and blocker resolve",
			in:            &nib.Nib{ID: "nibs-ccc3", Parent: "aaa1", BlockedBy: []string{"bbb2"}},
			wantChanged:   true,
			wantParent:    "nibs-aaa1",
			wantBlockedBy: []string{"nibs-bbb2"},
		},
		{
			name:          "unresolvable entries survive verbatim",
			in:            &nib.Nib{ID: "nibs-ccc3", Parent: "ghost", BlockedBy: []string{"ghost", "bbb2"}},
			wantChanged:   true,
			wantParent:    "ghost",
			wantBlockedBy: []string{"ghost", "nibs-bbb2"},
		},
		{
			name:         "legacy blocking list resolves too",
			in:           &nib.Nib{ID: "nibs-ccc3", Blocking: []string{"aaa1"}},
			wantChanged:  true,
			wantBlocking: []string{"nibs-aaa1"},
		},
		{
			name:        "empty links change nothing",
			in:          &nib.Nib{ID: "nibs-ccc3"},
			wantChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set := canonicalizeLinksInMap(nibs, tt.in, "nibs-")
			if set.changed != tt.wantChanged {
				t.Fatalf("changed = %v, want %v", set.changed, tt.wantChanged)
			}
			if !set.changed {
				return
			}
			set.applyTo(tt.in)
			if tt.in.Parent != tt.wantParent {
				t.Errorf("parent = %q, want %q", tt.in.Parent, tt.wantParent)
			}
			if !slices.Equal(tt.in.BlockedBy, tt.wantBlockedBy) {
				t.Errorf("blocked_by = %v, want %v", tt.in.BlockedBy, tt.wantBlockedBy)
			}
			if !slices.Equal(tt.in.Blocking, tt.wantBlocking) {
				t.Errorf("blocking = %v, want %v", tt.in.Blocking, tt.wantBlocking)
			}
		})
	}
}

// TestCanonicalizeLinksInMapNoPrefix pins the fast path: with no configured
// prefix, resolution degrades to an exact map lookup, so nothing can ever be
// rewritten and the pass must report no change.
func TestCanonicalizeLinksInMapNoPrefix(t *testing.T) {
	nibs := map[string]*nib.Nib{"aaa1": {ID: "aaa1", Title: "A", Status: "todo"}}
	b := &nib.Nib{ID: "bbb2", Parent: "aaa1", BlockedBy: []string{"aaa1"}}
	if set := canonicalizeLinksInMap(nibs, b, ""); set.changed {
		t.Errorf("changed = true with no prefix; nothing is resolvable to a different spelling")
	}
}
