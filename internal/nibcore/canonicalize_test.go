package nibcore

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alphaleonis/nibs/internal/config"
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

// TestLoadCanonicalizesShortFormMilestone drives the milestone link through the
// load pass: a hand-written short-form `milestone:` must resolve in memory
// exactly like `parent:` while the file keeps its spelling, and one naming no
// nib must survive verbatim. Milestone has no reverse traversal yet (membership
// still derives from Parent), so the stored spelling is the whole assertion.
func TestLoadCanonicalizesShortFormMilestone(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	writeLinkNibFile(t, nibsDir, "nibs-mst", "todo", "")
	depPath := writeLinkNibFile(t, nibsDir, "nibs-dep", "todo", "milestone: mst\n")
	writeLinkNibFile(t, nibsDir, "nibs-orp", "todo", "milestone: nope\n")
	before := hashFile(t, depPath)
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	dep, err := core.Get("nibs-dep")
	if err != nil {
		t.Fatalf(`Get("nibs-dep"): %v`, err)
	}
	if dep.Milestone != "nibs-mst" {
		t.Errorf("stored milestone = %q, want %q — the short form must be resolved on load", dep.Milestone, "nibs-mst")
	}
	if after := hashFile(t, depPath); after != before {
		data, _ := os.ReadFile(depPath)
		t.Errorf("Load rewrote %s; canonicalization is in-memory only. File is now:\n%s", filepath.Base(depPath), data)
	}

	orp, err := core.Get("nibs-orp")
	if err != nil {
		t.Fatalf(`Get("nibs-orp"): %v`, err)
	}
	if orp.Milestone != "nope" {
		t.Errorf("milestone = %q, want the unresolvable id left verbatim as %q", orp.Milestone, "nope")
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

// TestWatcherCanonicalizesMilestoneWhenTargetArrivesLater drives the milestone
// link through the watcher batch pass: written before its target exists it is
// genuinely unresolvable and stays verbatim, and the target's arrival must
// revisit it — the same ordering case the parent link needs.
func TestWatcherCanonicalizesMilestoneWhenTargetArrivesLater(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	writeLinkNibFile(t, nibsDir, "nibs-dep", "todo", "milestone: mst\n")
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Premise: with no target loaded the link is unresolvable and stays as written.
	if dep, _ := core.Get("nibs-dep"); dep == nil || dep.Milestone != "mst" {
		t.Fatalf("premise failed: milestone = %+v, want it left verbatim as \"mst\"", dep)
	}
	setWatching(core)

	mstPath := writeLinkNibFile(t, nibsDir, "nibs-mst", "todo", "")
	core.handleChanges(map[string]fsnotify.Op{mstPath: fsnotify.Create})

	dep, err := core.Get("nibs-dep")
	if err != nil {
		t.Fatalf(`Get("nibs-dep"): %v`, err)
	}
	if dep.Milestone != "nibs-mst" {
		t.Errorf("milestone = %q, want %q once the target exists", dep.Milestone, "nibs-mst")
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

// TestDeleteBareTokenIDRecanonicalizesLinks is the removal half of the
// canonicalization sweep. A store can hold a bare-token nib `e1` alongside its
// prefixed twin `nibs-e1`: nib.ParseFilename derives a bare id from any filename
// that does not carry the configured prefix. A link spelled `parent: e1` then
// resolves EXACTLY, so canonicalization correctly leaves it as written — until
// `e1` is deleted, when the exact key is gone and resolution falls through to
// the prefixed twin.
//
// Without a sweep on removal the stored spelling still reads `e1` while Get
// answers `nibs-e1`: the nib is re-parented with no event and no file change,
// and every reverse traversal disagrees with the forward resolver. The
// assertions therefore pin the STORED spelling and the reverse traversal — an
// assertion through Get alone passes on the broken code.
func TestDeleteBareTokenIDRecanonicalizesLinks(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	writeLinkNibFile(t, nibsDir, "e1", "todo", "")
	writeLinkNibFile(t, nibsDir, "nibs-e1", "todo", "")
	writeLinkNibFile(t, nibsDir, "nibs-t1", "todo", "parent: e1\n")
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Premise: the bare spelling resolves exactly, so load canonicalization
	// leaves it verbatim and it points at the bare-token nib. Without this the
	// assertions below could pass for the wrong reason.
	dep, err := core.Get("nibs-t1")
	if err != nil {
		t.Fatalf(`Get("nibs-t1"): %v`, err)
	}
	if dep.Parent != "e1" {
		t.Fatalf("premise failed: stored parent = %q, want the bare spelling %q to survive load", dep.Parent, "e1")
	}
	if got := linkTargets(t, core, "e1", "parent"); !slices.Equal(got, []string{"nibs-t1"}) {
		t.Fatalf("premise failed: FindIncomingLinks(e1) parent sources = %v, want [nibs-t1]", got)
	}

	if err := core.Delete("e1"); err != nil {
		t.Fatalf(`Delete("e1"): %v`, err)
	}

	dep, err = core.Get("nibs-t1")
	if err != nil {
		t.Fatalf(`Get("nibs-t1") after delete: %v`, err)
	}
	if dep.Parent != "nibs-e1" {
		t.Errorf("stored parent = %q, want %q — deleting the bare-token nib must re-resolve the link, not leave the store saying one thing while Get answers another", dep.Parent, "nibs-e1")
	}
	if got := linkTargets(t, core, "nibs-e1", "parent"); !slices.Equal(got, []string{"nibs-t1"}) {
		t.Errorf("FindIncomingLinks(nibs-e1) parent sources = %v, want [nibs-t1] — the reverse traversal must agree with the resolver", got)
	}
}

// TestDeleteBareTokenIDRecanonicalizesBlockedBy drives the removal sweep through
// a LIST field. BlockedBy is a slice header, so a rewrite editing it in place
// would be memory-unsafe for an off-lock reader in a way the Parent case is not.
//
// `blocked_by: [e1, nibs-e1]` names two DISTINCT stored nibs before the delete,
// so load leaves both spellings exactly as written; afterwards both resolve to
// nibs-e1 and the duplicate-collapse branch fires through this trigger.
func TestDeleteBareTokenIDRecanonicalizesBlockedBy(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	writeLinkNibFile(t, nibsDir, "e1", "in-progress", "")
	writeLinkNibFile(t, nibsDir, "nibs-e1", "in-progress", "")
	writeLinkNibFile(t, nibsDir, "nibs-t1", "todo", "blocked_by: [e1, nibs-e1]\n")
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Premise: the two spellings name two different nibs, so load canonicalization
	// has nothing to rewrite and both survive verbatim.
	published, err := core.Get("nibs-t1")
	if err != nil {
		t.Fatalf(`Get("nibs-t1"): %v`, err)
	}
	if !slices.Equal(published.BlockedBy, []string{"e1", "nibs-e1"}) {
		t.Fatalf("premise failed: blocked_by = %v, want both spellings to survive load", published.BlockedBy)
	}
	if got := linkTargets(t, core, "e1", "blocked_by"); !slices.Equal(got, []string{"nibs-t1"}) {
		t.Fatalf("premise failed: FindIncomingLinks(e1) blocked_by sources = %v, want [nibs-t1]", got)
	}

	if err := core.Delete("e1"); err != nil {
		t.Fatalf(`Delete("e1"): %v`, err)
	}

	dep, err := core.Get("nibs-t1")
	if err != nil {
		t.Fatalf(`Get("nibs-t1") after delete: %v`, err)
	}
	if !slices.Equal(dep.BlockedBy, []string{"nibs-e1"}) {
		t.Errorf("blocked_by = %v, want [nibs-e1] — both spellings now name the same nib", dep.BlockedBy)
	}
	// The pointer a reader took before the delete must be untouched: the rewrite
	// lands on a fresh nib rather than writing through a published slice.
	if !slices.Equal(published.BlockedBy, []string{"e1", "nibs-e1"}) {
		t.Errorf("the pre-delete pointer's blocked_by is now %v, want it left as [e1 nibs-e1]", published.BlockedBy)
	}
	if got := linkTargets(t, core, "nibs-e1", "blocked_by"); !slices.Equal(got, []string{"nibs-t1"}) {
		t.Errorf("FindIncomingLinks(nibs-e1) blocked_by sources = %v, want [nibs-t1] exactly once", got)
	}
	if !core.IsBlocked("nibs-t1") {
		t.Error(`IsBlocked("nibs-t1") = false, want true — nibs-e1 is in progress and still blocks it`)
	}
}

// TestDeleteBareTokenIDRecanonicalizesMilestone drives the removal sweep
// through the milestone link: a `milestone: e1` that resolved EXACTLY while the
// bare-token nib existed must fall through to the prefixed twin once it is
// deleted, or the stored spelling names one nib while the eventual consumer
// resolves another.
func TestDeleteBareTokenIDRecanonicalizesMilestone(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	writeLinkNibFile(t, nibsDir, "e1", "todo", "")
	writeLinkNibFile(t, nibsDir, "nibs-e1", "todo", "")
	writeLinkNibFile(t, nibsDir, "nibs-t1", "todo", "milestone: e1\n")
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Premise: the bare spelling resolves exactly, so load leaves it verbatim.
	dep, err := core.Get("nibs-t1")
	if err != nil {
		t.Fatalf(`Get("nibs-t1"): %v`, err)
	}
	if dep.Milestone != "e1" {
		t.Fatalf("premise failed: stored milestone = %q, want the bare spelling %q to survive load", dep.Milestone, "e1")
	}

	if err := core.Delete("e1"); err != nil {
		t.Fatalf(`Delete("e1"): %v`, err)
	}

	dep, err = core.Get("nibs-t1")
	if err != nil {
		t.Fatalf(`Get("nibs-t1") after delete: %v`, err)
	}
	if dep.Milestone != "nibs-e1" {
		t.Errorf("stored milestone = %q, want %q — deleting the bare-token nib must re-resolve the link", dep.Milestone, "nibs-e1")
	}
}

// TestDeleteWarnsAboutTheLinksItRepointed pins the announcement half of the
// removal sweep. Re-pointing a THIRD nib's link changes no file and publishes no
// event (no direct Core mutator publishes one), so the warning is the only
// signal a direct Core.Delete gives that it moved something else. Through the
// CLI this fires only for a legacy Blocking entry, since DeleteNib clears
// exact-match Parent/BlockedBy first (see Core.Delete).
func TestDeleteWarnsAboutTheLinksItRepointed(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	writeLinkNibFile(t, nibsDir, "e1", "todo", "")
	writeLinkNibFile(t, nibsDir, "nibs-e1", "todo", "")
	writeLinkNibFile(t, nibsDir, "nibs-t1", "todo", "parent: e1\n")
	writeLinkNibFile(t, nibsDir, "nibs-t2", "todo", "")
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Attached after Load so a load-time warning cannot be mistaken for one.
	var warnings bytes.Buffer
	core.SetWarnWriter(&warnings)

	if err := core.Delete("e1"); err != nil {
		t.Fatalf(`Delete("e1"): %v`, err)
	}

	got := warnings.String()
	for _, want := range []string{"e1", "nibs-t1.parent", "e1 -> nibs-e1"} {
		if !strings.Contains(got, want) {
			t.Errorf("delete warnings = %q, want them to mention %q", got, want)
		}
	}
	// Only the nib the sweep actually moved is named.
	if strings.Contains(got, "nibs-t2") {
		t.Errorf("delete warnings = %q, want no mention of the untouched nibs-t2", got)
	}
}

// TestWatcherCanonicalizesAfterRemoval is the watcher's counterpart: the sweep
// gate has to fire on a removal, not only on a create. An external delete of the
// bare-token nib (a `git pull` in the separate nibs repo, another process's
// `nibs delete`) reaches the store through handleChanges' removal branch, which
// used to leave the batch's canonicalization pass narrow — so the bystander
// holding the bare spelling was never revisited.
//
// It also pins that the rebind is ANNOUNCED: the bystander's new spelling
// reaches subscribers as an update rather than appearing out of nowhere on their
// next read.
func TestWatcherCanonicalizesAfterRemoval(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	barePath := writeLinkNibFile(t, nibsDir, "e1", "todo", "")
	writeLinkNibFile(t, nibsDir, "nibs-e1", "todo", "")
	writeLinkNibFile(t, nibsDir, "nibs-t1", "todo", "parent: e1\n")
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if dep, _ := core.Get("nibs-t1"); dep == nil || dep.Parent != "e1" {
		t.Fatalf("premise failed: parent = %+v, want the bare spelling \"e1\" to survive load", dep)
	}
	setWatching(core)
	ch, unsub := core.Subscribe()
	defer unsub()

	if err := os.Remove(barePath); err != nil {
		t.Fatalf("removing the bare-token file: %v", err)
	}
	core.handleChanges(map[string]fsnotify.Op{barePath: fsnotify.Remove})

	dep, err := core.Get("nibs-t1")
	if err != nil {
		t.Fatalf(`Get("nibs-t1") after watcher removal: %v`, err)
	}
	if dep.Parent != "nibs-e1" {
		t.Errorf("stored parent = %q, want %q once the bare-token nib is gone", dep.Parent, "nibs-e1")
	}
	if got := linkTargets(t, core, "nibs-e1", "parent"); !slices.Equal(got, []string{"nibs-t1"}) {
		t.Errorf("FindIncomingLinks(nibs-e1) parent sources = %v, want [nibs-t1]", got)
	}

	events := collectNibEvents(t, ch, "nibs-t1", 150*time.Millisecond)
	if len(events) != 1 {
		t.Fatalf("got %d events for the bystander nibs-t1, want exactly 1 announcing the rebind: %+v", len(events), events)
	}
	if events[0].Type != EventUpdated {
		t.Errorf("event type = %s, want %s", events[0].Type, EventUpdated)
	}
	if events[0].Nib == nil || events[0].Nib.Parent != "nibs-e1" {
		t.Errorf("published payload parent = %+v, want the re-resolved nibs-e1", events[0].Nib)
	}
}

// storedLinkSpellings renders every stored nib's link fields in the exact
// spelling the store holds them in. Two cores over the same directory must agree
// on this map — an assertion through Get alone would not notice a divergence,
// because the forward resolver normalizes both spellings to the same nib.
func storedLinkSpellings(t *testing.T, core *Core) map[string]string {
	t.Helper()
	out := make(map[string]string)
	for _, b := range core.All() {
		out[b.ID] = "parent=" + b.Parent +
			" milestone=" + b.Milestone +
			" blocked_by=[" + strings.Join(b.BlockedBy, " ") + "]" +
			// Blocking is included because canonicalizeLinksInMap resolves it too,
			// and it is the one field RemoveLinksTo deliberately does NOT clear — so
			// a live/fresh divergence confined to `blocking:` would otherwise pass.
			" blocking=[" + strings.Join(b.Blocking, " ") + "]"
	}
	return out
}

// loadFreshCore loads a SECOND, independent Core over an existing data directory
// — the `nibs list` a user runs while `nibs serve` holds the first one.
func loadFreshCore(t *testing.T, nibsDir string) *Core {
	t.Helper()
	fresh := New(nibsDir, config.DefaultWithPrefix("nibs-"))
	fresh.SetWarnWriter(nil)
	if err := fresh.Load(); err != nil {
		t.Fatalf("loading a fresh core over %s: %v", nibsDir, err)
	}
	return fresh
}

// TestCanonicalizationSurvivesADeleteAndRestore is the primary guard for
// nibs-4al1: a sweep must re-resolve from the FILE's spelling, never from the
// previous sweep's output.
//
// Deleting the bare-token nib `e1` re-points a bystander's `parent: e1` to the
// prefixed twin `nibs-e1` in memory, leaving the file untouched. Restoring the
// file — a `git checkout` or revert in the separately-versioned .nibs repo, or a
// re-create — fires the arrival sweep. Re-resolving from the STORED `nibs-e1`
// finds it already exact and changes nothing, so the live store keeps answering
// `nibs-e1` while the untouched file (and therefore any fresh load) says `e1`.
// The divergence is stable and permanent: a running `nibs serve` and a fresh
// `nibs list` answer differently for the same edge, indefinitely.
//
// The comparison is against a freshly-loaded core rather than a literal, because
// what the two must agree on is the whole store, not one hand-picked field.
func TestCanonicalizationSurvivesADeleteAndRestore(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	barePath := writeLinkNibFile(t, nibsDir, "e1", "in-progress", "")
	writeLinkNibFile(t, nibsDir, "nibs-e1", "in-progress", "")
	writeLinkNibFile(t, nibsDir, "nibs-t1", "todo", "parent: e1\nmilestone: e1\nblocked_by: [e1]\n")
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Premise: the bare spelling resolves EXACTLY while `e1` is stored, so load
	// canonicalization correctly leaves it verbatim.
	dep, err := core.Get("nibs-t1")
	if err != nil {
		t.Fatalf(`Get("nibs-t1"): %v`, err)
	}
	if dep.Parent != "e1" {
		t.Fatalf("premise failed: stored parent = %q, want the bare spelling %q to survive load", dep.Parent, "e1")
	}

	setWatching(core)

	if err := core.Delete("e1"); err != nil {
		t.Fatalf(`Delete("e1"): %v`, err)
	}
	// Premise: the removal sweep re-pointed the bystander in memory only.
	if dep, _ := core.Get("nibs-t1"); dep == nil || dep.Parent != "nibs-e1" {
		t.Fatalf("premise failed: parent = %+v, want the removal sweep to re-point it to nibs-e1", dep)
	}

	// Restore the file byte-for-byte and let the watcher see it arrive.
	writeLinkNibFile(t, nibsDir, "e1", "in-progress", "")
	core.handleChanges(map[string]fsnotify.Op{barePath: fsnotify.Create})

	live := storedLinkSpellings(t, core)
	want := storedLinkSpellings(t, loadFreshCore(t, nibsDir))
	if !maps.Equal(live, want) {
		t.Errorf("the live store and a fresh load of the same directory disagree:\n  live:  %v\n  fresh: %v", live, want)
	}
}

// TestCanonicalizationSweepIsIdempotent pins the other half of resolving from
// the file: a second sweep over an unchanged store must be a complete no-op.
//
// Resolving from the file spelling makes it tempting to report "changed"
// whenever the resolved id differs from the RAW one — but after the first sweep
// the stored value is ALREADY resolved, so that comparison stays true forever and
// every later sweep reinstalls a fresh clone. Nothing about the nib would look
// wrong; the cost lands downstream, where the watcher's batch pass turns each
// clone into a spurious EventUpdated for a nib nothing touched. The assertion is
// therefore on pointer IDENTITY, which is what a needless clone breaks.
func TestCanonicalizationSweepIsIdempotent(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	writeLinkNibFile(t, nibsDir, "e1", "in-progress", "")
	writeLinkNibFile(t, nibsDir, "nibs-e1", "in-progress", "")
	writeLinkNibFile(t, nibsDir, "nibs-t1", "todo", "parent: e1\nmilestone: e1\nblocked_by: [e1, nibs-e1]\n")
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Sweep #1, driven by a real key-set change so it genuinely re-points.
	if err := core.Delete("e1"); err != nil {
		t.Fatalf(`Delete("e1"): %v`, err)
	}
	if dep, _ := core.Get("nibs-t1"); dep == nil || dep.Parent != "nibs-e1" {
		t.Fatalf("premise failed: parent = %+v, want the first sweep to re-point it to nibs-e1", dep)
	}

	core.mu.RLock()
	before := maps.Clone(core.nibs)
	core.mu.RUnlock()

	// Sweep #2 over the very same store.
	core.mu.Lock()
	rebinds := core.canonicalizeStoreLocked()
	core.mu.Unlock()

	if len(rebinds) != 0 {
		t.Errorf("the second sweep re-pointed %v; re-resolving an unchanged store must change nothing", rebinds)
	}

	core.mu.RLock()
	defer core.mu.RUnlock()
	for id, b := range core.nibs {
		if b != before[id] {
			t.Errorf("the second sweep replaced the stored pointer for %q; a re-resolution that changes nothing must not clone (each clone becomes a spurious event on the watcher path)", id)
		}
	}
}

// TestCanonicalizationKeepsAnUpdatedLinkThroughALaterSweep guards the regression
// this design must not introduce. The file spelling a sweep resolves from is a
// MIRROR of the disk, not a load-time snapshot: Update writes a new link spelling
// without re-reading the file, so a mirror refreshed only on read goes stale, and
// the next sweep — fired by an unrelated create or delete — re-resolves the nib
// from its pre-Update spelling and silently reverts the user's edit in memory. A
// watcher would eventually repair it; a one-shot CLI run would not.
func TestCanonicalizationKeepsAnUpdatedLinkThroughALaterSweep(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	writeLinkNibFile(t, nibsDir, "nibs-par", "todo", "")
	writeLinkNibFile(t, nibsDir, "nibs-alt", "todo", "")
	writeLinkNibFile(t, nibsDir, "nibs-t1", "todo", "parent: par\n")
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Premise: the file's short spelling was resolved at load, so the stored
	// value and the file already disagree before the update.
	dep, err := core.Get("nibs-t1")
	if err != nil {
		t.Fatalf(`Get("nibs-t1"): %v`, err)
	}
	if dep.Parent != "nibs-par" {
		t.Fatalf("premise failed: stored parent = %q, want the load-canonicalized %q", dep.Parent, "nibs-par")
	}

	edited := dep.Clone()
	edited.Parent = "nibs-alt"
	if err := core.Update(edited, nil); err != nil {
		t.Fatalf("Update re-parenting nibs-t1: %v", err)
	}

	// An unrelated create: it changes the key set, so it sweeps the whole store.
	if err := core.Create(&nib.Nib{ID: "nibs-new1", Version: 1, Title: "New", Status: "todo", Type: "task"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := core.Get("nibs-t1")
	if err != nil {
		t.Fatalf(`Get("nibs-t1") after the sweep: %v`, err)
	}
	if got.Parent != "nibs-alt" {
		t.Errorf("parent = %q, want %q — an unrelated sweep reverted the update to the pre-update file spelling", got.Parent, "nibs-alt")
	}
	// And a fresh load agrees, because the update reached disk.
	fresh, err := loadFreshCore(t, nibsDir).Get("nibs-t1")
	if err != nil {
		t.Fatalf(`fresh Get("nibs-t1"): %v`, err)
	}
	if fresh.Parent != got.Parent {
		t.Errorf("fresh load parent = %q, live store parent = %q; they must agree", fresh.Parent, got.Parent)
	}
}

// TestCreateResolvesLinksNamingTheNewNib covers the arrival-direction hole
// Core.Create left open: it inserts a key without re-resolving, and the watcher
// cannot compensate because the in-process insert happens FIRST — by the time
// fsnotify reports the new file the id is already stored, so the batch pass sees
// it as an ordinary edit and runs the narrow touched-only sweep.
//
// A short-form link naming an id that does not exist yet is left verbatim by
// design, so only the target's arrival can resolve it. The assertions pin the
// stored spelling and the reverse traversal — through Get alone both spellings
// already answer with the same nib.
func TestCreateResolvesLinksNamingTheNewNib(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	writeLinkNibFile(t, nibsDir, "nibs-t1", "todo", "parent: zz9\nblocked_by: [zz9]\n")
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Premise: nothing named zz9 exists, so the link is unresolvable and stays
	// exactly as written.
	dep, err := core.Get("nibs-t1")
	if err != nil {
		t.Fatalf(`Get("nibs-t1"): %v`, err)
	}
	if dep.Parent != "zz9" {
		t.Fatalf("premise failed: parent = %q, want it left verbatim as %q", dep.Parent, "zz9")
	}

	if err := core.Create(&nib.Nib{ID: "nibs-zz9", Version: 1, Title: "Arrived", Status: "in-progress", Type: "task"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	dep, err = core.Get("nibs-t1")
	if err != nil {
		t.Fatalf(`Get("nibs-t1") after Create: %v`, err)
	}
	if dep.Parent != "nibs-zz9" {
		t.Errorf("stored parent = %q, want %q — creating the nib a dangling link names must resolve it", dep.Parent, "nibs-zz9")
	}
	if !slices.Equal(dep.BlockedBy, []string{"nibs-zz9"}) {
		t.Errorf("blocked_by = %v, want [nibs-zz9]", dep.BlockedBy)
	}
	if got := linkTargets(t, core, "nibs-zz9", "parent"); !slices.Equal(got, []string{"nibs-t1"}) {
		t.Errorf("FindIncomingLinks(nibs-zz9) parent sources = %v, want [nibs-t1] — the reverse traversal must agree with the resolver", got)
	}
	if !core.IsBlocked("nibs-t1") {
		t.Error(`IsBlocked("nibs-t1") = false, want true — nibs-zz9 is in progress and now blocks it`)
	}
}

// TestCreateWarnsAboutTheLinksItRepointed is the arrival counterpart to
// TestDeleteWarnsAboutTheLinksItRepointed: a create that re-points a THIRD nib's
// link changes no file and publishes no event, so the warning is the only signal
// a direct Core.Create gives that it moved something else.
func TestCreateWarnsAboutTheLinksItRepointed(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	writeLinkNibFile(t, nibsDir, "nibs-t1", "todo", "parent: zz9\n")
	writeLinkNibFile(t, nibsDir, "nibs-t2", "todo", "")
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Attached after Load so a load-time warning cannot be mistaken for one.
	var warnings bytes.Buffer
	core.SetWarnWriter(&warnings)

	if err := core.Create(&nib.Nib{ID: "nibs-zz9", Version: 1, Title: "Arrived", Status: "todo", Type: "task"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got := warnings.String()
	for _, want := range []string{"nibs-zz9", "nibs-t1.parent", "zz9 -> nibs-zz9"} {
		if !strings.Contains(got, want) {
			t.Errorf("create warnings = %q, want them to mention %q", got, want)
		}
	}
	if strings.Contains(got, "nibs-t2") {
		t.Errorf("create warnings = %q, want no mention of the untouched nibs-t2", got)
	}
}

// TestCreateResolvesMilestoneNamingTheNewNib is the arrival direction for the
// milestone link: a dangling `milestone:` left verbatim by design must resolve
// when the nib it names is created, and the rebind must be announced — the
// warning is the only signal a direct Core.Create gives that it moved a third
// nib's link.
func TestCreateResolvesMilestoneNamingTheNewNib(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	writeLinkNibFile(t, nibsDir, "nibs-t1", "todo", "milestone: zz9\n")
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Premise: nothing named zz9 exists, so the link stays exactly as written.
	dep, err := core.Get("nibs-t1")
	if err != nil {
		t.Fatalf(`Get("nibs-t1"): %v`, err)
	}
	if dep.Milestone != "zz9" {
		t.Fatalf("premise failed: milestone = %q, want it left verbatim as %q", dep.Milestone, "zz9")
	}

	// Attached after Load so a load-time warning cannot be mistaken for one.
	var warnings bytes.Buffer
	core.SetWarnWriter(&warnings)

	if err := core.Create(&nib.Nib{ID: "nibs-zz9", Version: 1, Title: "Arrived", Status: "todo", Type: "milestone"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	dep, err = core.Get("nibs-t1")
	if err != nil {
		t.Fatalf(`Get("nibs-t1") after Create: %v`, err)
	}
	if dep.Milestone != "nibs-zz9" {
		t.Errorf("stored milestone = %q, want %q — creating the nib a dangling link names must resolve it", dep.Milestone, "nibs-zz9")
	}
	got := warnings.String()
	for _, want := range []string{"nibs-t1.milestone", "zz9 -> nibs-zz9"} {
		if !strings.Contains(got, want) {
			t.Errorf("create warnings = %q, want them to mention %q", got, want)
		}
	}
}

// TestAreaIsNotALink pins the axis split: `area:` is a plain path-valued
// string, so a value that HAPPENS to spell a resolvable short id must pass
// through every canonicalization pass untouched — load and the create-driven
// full sweep alike.
func TestAreaIsNotALink(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	writeLinkNibFile(t, nibsDir, "nibs-blk", "todo", "")
	writeLinkNibFile(t, nibsDir, "nibs-t1", "todo", "area: blk\n")
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	dep, err := core.Get("nibs-t1")
	if err != nil {
		t.Fatalf(`Get("nibs-t1"): %v`, err)
	}
	if dep.Area != "blk" {
		t.Errorf("area = %q after load, want %q — area is not a link and must never be resolved", dep.Area, "blk")
	}

	// A create sweeps the whole store; the sweep must leave area alone too.
	if err := core.Create(&nib.Nib{ID: "nibs-new1", Version: 1, Title: "New", Status: "todo", Type: "task"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	dep, err = core.Get("nibs-t1")
	if err != nil {
		t.Fatalf(`Get("nibs-t1") after the sweep: %v`, err)
	}
	if dep.Area != "blk" {
		t.Errorf("area = %q after the sweep, want %q", dep.Area, "blk")
	}

	// And the pure helper agrees: a nib whose only resolvable-looking value is
	// its area reports no change at all.
	nibs := map[string]*nib.Nib{"nibs-blk": {ID: "nibs-blk", Title: "B", Status: "todo"}}
	b := &nib.Nib{ID: "nibs-x", Area: "blk"}
	if set := canonicalizeLinksInMap(nibs, b, "nibs-"); set.changed {
		t.Errorf("canonicalizeLinksInMap reported a change for a nib with only an area set; area must not be canonicalized")
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
		wantMilestone string
		wantBlockedBy []string
		wantBlocking  []string
	}{
		{
			name:        "already canonical is a no-op",
			in:          &nib.Nib{ID: "nibs-ccc3", Parent: "nibs-aaa1", Milestone: "nibs-bbb2", BlockedBy: []string{"nibs-bbb2"}},
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
			name:          "short milestone resolves",
			in:            &nib.Nib{ID: "nibs-ccc3", Milestone: "aaa1"},
			wantChanged:   true,
			wantMilestone: "nibs-aaa1",
		},
		{
			name:          "unresolvable entries survive verbatim",
			in:            &nib.Nib{ID: "nibs-ccc3", Parent: "ghost", Milestone: "ghost2", BlockedBy: []string{"ghost", "bbb2"}},
			wantChanged:   true,
			wantParent:    "ghost",
			wantMilestone: "ghost2",
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
			if tt.in.Milestone != tt.wantMilestone {
				t.Errorf("milestone = %q, want %q", tt.in.Milestone, tt.wantMilestone)
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
