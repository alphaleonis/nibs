package nibcore

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/fsnotify/fsnotify"
)

// countWatchLoops reports how many goroutines are currently executing
// watchLoop, read out of a full goroutine dump by frame name.
//
// runtime.NumGoroutine is unusable here: it also counts fsnotify's internal
// reader goroutines and runtime timer goroutines, which come and go on their
// own schedule, so its value swings independently of the leak under test.
//
// The trailing paren in the frame name is load-bearing. Go names a function
// literal after its parent — watchLoop's deferred Close and its time.AfterFunc
// debounce callback appear as watchLoop.func1 and watchLoop.func2 — so an
// unanchored match also counts those. The debounce callback runs on its own
// goroutine, so a census sampled while it is in flight would read one loop too
// many. Only the real call frame carries the argument list.
func countWatchLoops() int {
	// Dumps here run to a few KB; start small and grow rather than allocating a
	// megabyte on every poll.
	buf := make([]byte, 64<<10)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return bytes.Count(buf[:n], []byte("nibcore.(*Core).watchLoop("))
		}
		buf = make([]byte, 2*len(buf))
	}
}

// waitForWatchLoops polls the goroutine census until it reads want, returning
// the last count seen. A retiring watchLoop needs a moment to notice its done
// channel closed, so a single sample would be racy; an orphaned one never
// settles at all, because nothing will ever close the channel it has latched
// onto.
func waitForWatchLoops(want int, timeout time.Duration) (int, bool) {
	deadline := time.Now().Add(timeout)
	for {
		n := countWatchLoops()
		if n == want {
			return n, true
		}
		if !time.Now().Before(deadline) {
			return n, false
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// restartTwice drives StopWatching -> StartWatching twice with nothing in between.
//
// The second restart is what does the work. The first one spawns a loop that
// has almost certainly not reached its select yet, and the second reassigns
// c.done while it is still short of it. A loop that binds its exit channel at
// the `go` statement is unaffected; a loop that reads c.done at select entry
// reads the successor's channel instead of its own and never exits.
//
// A single restart per cycle only exercises this on the very first cycle:
// afterwards the caller's census poll hands every fresh loop the microseconds
// it needs to block in select, and a blocked loop has already evaluated its
// channel operand, so closing it wakes the select and the loop returns without
// ever re-reading the field. That is why a quiet restart proves nothing, and why
// this helper never waits between the pair.
func restartTwice(t *testing.T, core *Core, cycle int) {
	t.Helper()
	for i := range 2 {
		if err := core.StopWatching(); err != nil {
			t.Fatalf("cycle %d: StopWatching() #%d error = %v", cycle, i+1, err)
		}
		if err := core.StartWatching(); err != nil {
			t.Fatalf("cycle %d: StartWatching() #%d error = %v", cycle, i+1, err)
		}
	}
}

// TestRestartWatchingDoesNotOrphanWatchLoop covers StopWatching -> StartWatching.
// watchLoop must exit on the done channel it was started with, not on whatever
// c.done happens to hold when it next reaches the select — otherwise a restart
// leaves the old loop running against a fresh, open channel, leaking the
// goroutine and the fsnotify watcher and file descriptors its deferred Close
// would have released, and double-servicing the directory.
func TestRestartWatchingDoesNotOrphanWatchLoop(t *testing.T) {
	const restartCycles = 50

	core, _ := setupTestCore(t)

	if err := core.StartWatching(); err != nil {
		t.Fatalf("StartWatching() error = %v", err)
	}
	defer func() { _ = core.StopWatching() }()

	for cycle := range restartCycles {
		restartTwice(t, core, cycle)

		if n, ok := waitForWatchLoops(1, 2*time.Second); !ok {
			t.Fatalf("cycle %d: %d watchLoop goroutines live after restart, want 1 — "+
				"the pre-restart loop was orphaned onto the new done channel", cycle, n)
		}
	}
}

// collectBatchArrivals reads ch for the whole window and returns the arrival
// time of every batch it receives. It keeps batch boundaries — unlike
// collectNibEvents, which flattens them — because the duplicate-loop symptom is
// *when* batches land relative to each other, not what they carry. fanOut never
// delivers an empty batch, and this test's only filesystem activity is its one
// write, so every batch received here is that write surfacing.
func collectBatchArrivals(ch <-chan []NibEvent, window time.Duration) []time.Time {
	var times []time.Time
	deadline := time.After(window)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return times
			}
			times = append(times, time.Now())
		case <-deadline:
			return times
		}
	}
}

// TestRestartWatchingDoesNotDuplicateEvents is the user-visible half of the
// same leak: an orphaned loop keeps servicing the directory, so a single write
// is debounced, handled and fanned out once per live loop. The census above
// asserts the cause; this asserts the symptom. The two are separable, because
// each loop owns its own pendingChanges map and debounce timer.
//
// The subscription must be taken after the restart: unwatchLocked closes and
// drops every subscriber channel, and StartWatching does not restore them.
func TestRestartWatchingDoesNotDuplicateEvents(t *testing.T) {
	// Each round is an independent chance to orphan a loop, and one orphan is
	// enough to double-deliver. A round that fails to orphan simply sees one
	// batch, the same as a healthy watch.
	const rounds = 3

	core, nibsDir := setupTestCore(t)

	if err := core.StartWatching(); err != nil {
		t.Fatalf("StartWatching() error = %v", err)
	}
	defer func() { _ = core.StopWatching() }()

	for round := range rounds {
		restartTwice(t, core, round)

		events, unsubscribe := core.Subscribe()

		path := filepath.Join(nibsDir, fmt.Sprintf("dup%d--duplicate.md", round))
		body := fmt.Sprintf("---\ntitle: Duplicate %d\nstatus: todo\n---\n", round)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("round %d: writing nib to drive the watch: %v", round, err)
		}

		// Presence, on a generous timeout, recording when the first batch lands. A
		// round that never sees the write has tested nothing and must say so rather
		// than pass quietly.
		var arrivals []time.Time
		presence := time.After(2 * time.Second)
	awaitFirst:
		for {
			select {
			case _, ok := <-events:
				if !ok {
					t.Fatalf("round %d: subscription closed while watching", round)
				}
				arrivals = append(arrivals, time.Now())
				break awaitFirst
			case <-presence:
				t.Fatalf("round %d: no event batch delivered for a written nib — "+
					"the watch never observed the write, so this round proves nothing", round)
			}
		}

		// Then collect any further batches over a window well past the debounce,
		// without unsubscribing early: a late duplicate must not be dropped the way
		// a hard absence window plus an immediate unsubscribe would drop it.
		arrivals = append(arrivals, collectBatchArrivals(events, 4*debounceDelay)...)

		// One servicing loop cannot deliver two batches for a single write closer
		// together than a full debounce window: its timer only re-arms on a fresh
		// event and then waits the whole delay again, so its successive batches are
		// always more than debounceDelay apart. That is exactly the shape of the
		// Windows false-failure — an fsnotify Create/Write that splits past the
		// 100 ms debounce surfaces as two *spread* batches from the one loop, and
		// must not fail this test. An orphaned loop is different in kind: it and its
		// successor are armed by the same events at the same instant and fire
		// together, so their batches land within a few milliseconds of each other.
		// A gap below half the debounce window therefore isolates a real duplicate —
		// a second loop servicing the directory — from a benign split.
		for i := 1; i < len(arrivals); i++ {
			if gap := arrivals[i].Sub(arrivals[i-1]); gap < debounceDelay/2 {
				t.Fatalf("round %d: two batches for one write landed %v apart (< %v) — "+
					"an orphaned watchLoop is servicing the directory alongside its successor",
					round, gap, debounceDelay/2)
			}
		}

		unsubscribe()
	}
}

// The removal branch of handleChanges must decide archive-vs-unarchive-vs-delete
// from the filesystem's current truth, never from the possibly-stale stored Path
// and never from the order the debounce batch happens to iterate. The two tests
// below pin that for a move performed by a mover OTHER than in-process Archive:
// an external archive (a nib renamed INTO archive/ by the CLI against a running
// server or a pull in the separate .nibs repo) and an unarchive (a nib renamed
// OUT of archive/ by LoadAndUnarchive when an archived nib is edited).
//
// Both are exercised for BOTH batch orderings. Ordering is forced by driving the
// two halves of the move as two separate single-entry debounce batches — one map
// key per handleChanges call — rather than one two-key batch, which would iterate
// as a Go map and pick an order at random. handleChanges no-ops unless watching,
// so the flag is set directly instead of via StartWatching: a real fsnotify loop
// would compete to deliver these same events with an uncontrolled order and
// defeat the point of the test.

// watchingCore returns a core with a nib created and a filename, with the
// watching flag forced on (no real watch loop) so handleChanges runs.
func watchingCore(t *testing.T, nibID string) (*Core, string, string) {
	t.Helper()
	core, nibsDir := setupTestCore(t)
	b := createTestNib(t, core, nibID, "Move Test", "todo")
	filename := filepath.Base(b.Path)
	return core, nibsDir, filename
}

func setWatching(core *Core) {
	core.mu.Lock()
	core.watching = true
	core.mu.Unlock()
}

// driveMove feeds the rename-at-old-path and create-at-new-path halves of a move
// as two separate debounce batches, in the requested order.
func driveMove(core *Core, removeAbs, createAbs string, createFirst bool) {
	removeBatch := func() { core.handleChanges(map[string]fsnotify.Op{removeAbs: fsnotify.Rename}) }
	createBatch := func() { core.handleChanges(map[string]fsnotify.Op{createAbs: fsnotify.Create}) }
	if createFirst {
		createBatch()
		removeBatch()
	} else {
		removeBatch()
		createBatch()
	}
}

func assertNoDeletedFor(t *testing.T, events []NibEvent, nibID string) {
	t.Helper()
	for _, e := range events {
		if e.NibID == nibID && e.Type == EventDeleted {
			t.Fatalf("got a deleted event for %s; a move was misreported as a deletion (events: %+v)", nibID, events)
		}
	}
}

func assertHasTypeFor(t *testing.T, events []NibEvent, nibID string, want EventType) {
	t.Helper()
	for _, e := range events {
		if e.NibID == nibID && e.Type == want {
			return
		}
	}
	t.Fatalf("expected a %s event for %s, got: %+v", want, nibID, events)
}

func assertNoTypeFor(t *testing.T, events []NibEvent, nibID string, unwanted EventType) {
	t.Helper()
	for _, e := range events {
		if e.NibID == nibID && e.Type == unwanted {
			t.Fatalf("unexpected %s event for %s: %+v", unwanted, nibID, events)
		}
	}
}

// TestWatcherExternalArchiveReportsArchivedNotDeleted covers nibs-y56n: an
// archive performed OUTSIDE this process (so the stored Path is never rewritten
// and stays stale) must report as archived, not deleted, and must not evict the
// live nib — for either batch ordering.
func TestWatcherExternalArchiveReportsArchivedNotDeleted(t *testing.T) {
	const nibID = "arx1"

	for _, createFirst := range []bool{false, true} {
		name := "remove-first"
		if createFirst {
			name = "create-first"
		}
		t.Run(name, func(t *testing.T) {
			core, nibsDir, filename := watchingCore(t, nibID)

			// The archive dir must exist for the archive-path create to be a real
			// on-disk fact the handler can stat.
			archiveDir := filepath.Join(nibsDir, ArchiveDir)
			if err := os.MkdirAll(archiveDir, 0o755); err != nil {
				t.Fatalf("mkdir archive: %v", err)
			}

			mainAbs := filepath.Join(nibsDir, filename)
			archiveAbs := filepath.Join(archiveDir, filename)

			// External archive: move the file on disk WITHOUT calling core.Archive,
			// so the stored Path stays "<filename>" (stale), exactly as the CLI
			// against a running server or a .nibs pull would leave it.
			if err := os.Rename(mainAbs, archiveAbs); err != nil {
				t.Fatalf("external archive rename: %v", err)
			}

			setWatching(core)
			ch, unsub := core.Subscribe()
			defer unsub()

			driveMove(core, mainAbs, archiveAbs, createFirst)

			got := collectNibEvents(t, ch, nibID, 150*time.Millisecond)
			assertNoDeletedFor(t, got, nibID)
			assertHasTypeFor(t, got, nibID, EventArchived)

			n, err := core.Get(nibID)
			if err != nil {
				t.Fatalf("nib evicted from store after external archive: Get(%q) = %v", nibID, err)
			}
			if !core.isArchivedPath(n.Path) {
				t.Errorf("stored Path = %q, want an archive path", n.Path)
			}
		})
	}
}

// TestWatcherUnarchiveReportsUnarchivedNotDeleted covers nibs-ow1k and nibs-2fgz:
// unarchiving a nib while the watcher is running (via the production
// LoadAndUnarchive path, reached when an archived nib is edited) must NOT emit a
// deleted event and must NOT evict the nib from the store while its file exists
// on disk — for either batch ordering. Create-first is the ordering the
// investigation measured as real data loss; it must be covered, not just
// remove-first. The remove-half of the move must report the distinct
// EventUnarchived (not EventUpdated), so a viewer can clear an "archived" banner
// on the reverse direction (nibs-2fgz).
func TestWatcherUnarchiveReportsUnarchivedNotDeleted(t *testing.T) {
	const nibID = "unx1"

	for _, createFirst := range []bool{false, true} {
		name := "remove-first"
		if createFirst {
			name = "create-first"
		}
		t.Run(name, func(t *testing.T) {
			core, nibsDir, filename := watchingCore(t, nibID)

			// Start it archived, then unarchive via the production path
			// (LoadAndUnarchive rewrites the stored Path to the main dir under the
			// lock, BEFORE the debounced handler runs).
			if err := core.Archive(nibID); err != nil {
				t.Fatalf("Archive: %v", err)
			}
			archiveAbs := filepath.Join(nibsDir, ArchiveDir, filename)
			mainAbs := filepath.Join(nibsDir, filename)

			if _, err := core.LoadAndUnarchive(nibID); err != nil {
				t.Fatalf("LoadAndUnarchive: %v", err)
			}
			if core.fileExists(archiveAbs) || !core.fileExists(mainAbs) {
				t.Fatalf("precondition: expected file at %s and not %s", mainAbs, archiveAbs)
			}

			setWatching(core)
			ch, unsub := core.Subscribe()
			defer unsub()

			// The move left archive/ (rename half) and arrived in main (create half).
			driveMove(core, archiveAbs, mainAbs, createFirst)

			got := collectNibEvents(t, ch, nibID, 150*time.Millisecond)
			assertNoDeletedFor(t, got, nibID)

			// The unarchive (remove-half) branch must report the move as its own
			// distinct EventUnarchived, never as updated or archived. This pins the
			// direction precisely: the create-half of the same move independently
			// emits an EventUpdated for the already-stored nib, so EventUnarchived is
			// the only witness that the REMOVE-half classified the move as an
			// unarchive rather than falling back to a plain update (the nibs-2fgz
			// bug). The plausible mislabel is a copy/paste of the sibling archive
			// branch's EventArchived, so also assert that NO archived event surfaces
			// for this nib.
			assertHasTypeFor(t, got, nibID, EventUnarchived)
			assertNoTypeFor(t, got, nibID, EventArchived)

			// Data-loss guard: the nib must remain in the store and its file must
			// still exist on disk.
			n, err := core.Get(nibID)
			if err != nil {
				t.Fatalf("nib evicted from store during unarchive (data loss): Get(%q) = %v", nibID, err)
			}
			if !core.fileExists(filepath.Join(nibsDir, n.Path)) {
				t.Errorf("stored Path %q has no file on disk", n.Path)
			}
			if core.isArchivedPath(n.Path) {
				t.Errorf("stored Path = %q, want a main-directory path after unarchive", n.Path)
			}
		})
	}
}

// TestWatcherSameIdSlugRenameNotEvicted covers nibs-ab2s: a same-id slug rename
// that moves a file within the same non-archive directory
// (ab21--move-test.md -> ab21--new-slug.md) must NOT emit a deleted event and
// must NOT evict the nib from the store — its file exists on disk under the new
// name, and nib.ParseFilename yields the same id for both names. Both cheap
// basename checks in the removal branch (archive-in, unarchive-out) miss because
// the basename changed, so without the bounded by-ID scan the nib falls through
// to the genuine-delete path. Both batch orderings are covered: create-first
// evicts permanently (no later create re-adds it), remove-first evicts
// transiently but still lies with a deleted event.
func TestWatcherSameIdSlugRenameNotEvicted(t *testing.T) {
	const nibID = "ab21"

	for _, createFirst := range []bool{false, true} {
		name := "remove-first"
		if createFirst {
			name = "create-first"
		}
		t.Run(name, func(t *testing.T) {
			core, nibsDir, oldFilename := watchingCore(t, nibID)

			oldAbs := filepath.Join(nibsDir, oldFilename)
			newAbs := filepath.Join(nibsDir, nibID+"--new-slug.md")

			// Physical rename so the old path is gone and the new one loads —
			// exactly what a slug rename by any mover leaves on disk. Same id, new
			// slug, same non-archive directory.
			if err := os.Rename(oldAbs, newAbs); err != nil {
				t.Fatalf("slug rename: %v", err)
			}
			if core.fileExists(oldAbs) || !core.fileExists(newAbs) {
				t.Fatalf("precondition: expected file at %s and not %s", newAbs, oldAbs)
			}

			setWatching(core)
			ch, unsub := core.Subscribe()
			defer unsub()

			driveMove(core, oldAbs, newAbs, createFirst)

			got := collectNibEvents(t, ch, nibID, 150*time.Millisecond)

			// The move must never surface as a deletion. This is the assertion that
			// bites remove-first, where the create-half re-adds the nib so Get would
			// succeed even against the bug.
			assertNoDeletedFor(t, got, nibID)

			// Data-loss guard: the nib must remain in the store and its file must
			// still exist on disk at the new name. This is the assertion that bites
			// create-first, where the remove-half evicts permanently.
			n, err := core.Get(nibID)
			if err != nil {
				t.Fatalf("nib evicted from store during slug rename (data loss): Get(%q) = %v", nibID, err)
			}
			if !core.fileExists(filepath.Join(nibsDir, n.Path)) {
				t.Errorf("stored Path %q has no file on disk", n.Path)
			}
			if core.isArchivedPath(n.Path) {
				t.Errorf("stored Path = %q, want a main-directory path", n.Path)
			}
		})
	}
}

// TestWatcherSameIdSluglessRenameNotEvicted covers nibs-mccz: a same-id rename
// that DROPS the slug from a prefixed nib (nibs-x9z2--move-test.md ->
// nibs-x9z2.md) must NOT evict the nib. Every configured id prefix ends in a
// dash, so the legacy single-dash filename parse split the slugless target name
// (nibs-x9z2.md) into id "nibs" — a different id than the stored "nibs-x9z2".
// findRelPathByID then failed to match the renamed file by id, so the removal
// branch fell through to genuine-delete, dropping a live nib whose file exists on
// disk. Mirrors TestWatcherSameIdSlugRenameNotEvicted but renames to the SLUGLESS
// {id}.md form and requires a PREFIXED core, which is what the prefix bug uniquely
// breaks. Both batch orderings are covered.
func TestWatcherSameIdSluglessRenameNotEvicted(t *testing.T) {
	const fullID = "nibs-x9z2"

	for _, createFirst := range []bool{false, true} {
		name := "remove-first"
		if createFirst {
			name = "create-first"
		}
		t.Run(name, func(t *testing.T) {
			// A prefixed core is essential: the bug bites only when the id carries
			// the configured prefix, whose trailing dash the legacy parse mis-split.
			core, nibsDir := mustLoadPrefixedCore(t)
			createTestNib(t, core, fullID, "Move Test", "todo")

			oldAbs := filepath.Join(nibsDir, nib.BuildFilename(fullID, nib.Slugify("Move Test")))
			newAbs := filepath.Join(nibsDir, nib.BuildFilename(fullID, "")) // slugless {id}.md

			// Physical rename so the old path is gone and the new (slugless) one
			// loads — exactly what a slug-dropping rename leaves on disk.
			if err := os.Rename(oldAbs, newAbs); err != nil {
				t.Fatalf("slugless rename: %v", err)
			}
			if core.fileExists(oldAbs) || !core.fileExists(newAbs) {
				t.Fatalf("precondition: expected file at %s and not %s", newAbs, oldAbs)
			}

			setWatching(core)
			ch, unsub := core.Subscribe()
			defer unsub()

			driveMove(core, oldAbs, newAbs, createFirst)

			got := collectNibEvents(t, ch, fullID, 150*time.Millisecond)

			// The move must never surface as a deletion (bites remove-first, where
			// the create-half would otherwise re-add the nib and mask the eviction).
			assertNoDeletedFor(t, got, fullID)

			// Data-loss guard: the nib must remain in the store under its full id,
			// its file present on disk at the slugless name.
			n, err := core.Get(fullID)
			if err != nil {
				t.Fatalf("nib evicted from store during slugless rename (data loss): Get(%q) = %v", fullID, err)
			}
			if n.ID != fullID {
				t.Errorf("stored ID = %q, want %q", n.ID, fullID)
			}
			if !core.fileExists(filepath.Join(nibsDir, n.Path)) {
				t.Errorf("stored Path %q has no file on disk", n.Path)
			}
			if wantPath := filepath.ToSlash(nib.BuildFilename(fullID, "")); n.Path != wantPath {
				t.Errorf("stored Path = %q, want slugless %q", n.Path, wantPath)
			}
		})
	}
}

// TestWatcherSameIdSlugRenameUpdatesSlug covers nibs-41xj: the same-id
// slug-rename branch of handleChanges (nibs-x--old-slug.md -> nibs-x--new-slug.md)
// must re-derive the in-memory Slug from the new filename, not merely rewrite
// Path. Slug is a filename-derived field, and that branch keeps the nib live by
// pointing Path at the file's new location WITHOUT reloading its content — so a
// Slug left untouched stays stale, desynced from both Path and the file on disk.
//
// Only the rename half is driven (no following Create). That is what isolates
// this branch: were a Create event also handled, the create/write branch would
// reload the nib from disk and mask the stale Slug. This is why the sibling
// TestWatcherSameIdSlugRenameNotEvicted (which drives both halves) does not catch
// it.
func TestWatcherSameIdSlugRenameUpdatesSlug(t *testing.T) {
	const nibID = "sr41"
	const newSlug = "new-slug"

	core, nibsDir, oldFilename := watchingCore(t, nibID)

	// Precondition: the created nib carries a DIFFERENT slug, so a stale-slug bug
	// leaves Slug unchanged and the assertion below still bites.
	before, err := core.Get(nibID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if before.Slug == newSlug {
		t.Fatalf("precondition: nib already has slug %q before rename", newSlug)
	}

	oldAbs := filepath.Join(nibsDir, oldFilename)
	newName := nib.BuildFilename(nibID, newSlug)
	newAbs := filepath.Join(nibsDir, newName)
	if err := os.Rename(oldAbs, newAbs); err != nil {
		t.Fatalf("slug rename: %v", err)
	}

	setWatching(core)
	ch, unsub := core.Subscribe()
	defer unsub()

	// Drive ONLY the rename half so the same-id slug-rename branch is the last
	// word (no create/write reload to overwrite a stale Slug).
	core.handleChanges(map[string]fsnotify.Op{oldAbs: fsnotify.Rename})

	got, err := core.Get(nibID)
	if err != nil {
		t.Fatalf("nib evicted during slug rename: Get(%q) = %v", nibID, err)
	}
	if got.Slug != newSlug {
		t.Errorf("stored Slug = %q, want %q (slug not re-derived from renamed file)", got.Slug, newSlug)
	}
	if wantPath := filepath.ToSlash(newName); got.Path != wantPath {
		t.Errorf("stored Path = %q, want %q", got.Path, wantPath)
	}

	// The emitted event must carry the re-derived slug too, not just the store: a
	// copy-on-write that reinstalled the store but published a stale-slug payload
	// would pass the Get assertions above yet strand a wrong Slug in every
	// subscriber. Mirrors the event assertions in the sibling rename tests.
	events := collectNibEvents(t, ch, nibID, 150*time.Millisecond)
	var updated *NibEvent
	for i := range events {
		if events[i].Type == EventUpdated {
			updated = &events[i]
		}
	}
	if updated == nil {
		t.Fatalf("no EventUpdated emitted for %q; got %v", nibID, events)
	}
	if updated.Nib == nil {
		t.Fatalf("emitted EventUpdated has a nil Nib")
	}
	if updated.Nib.Slug != newSlug {
		t.Errorf("emitted EventUpdated Slug = %q, want %q", updated.Nib.Slug, newSlug)
	}
}

// TestEventPayloadsAreImmutableSnapshots pins the payload contract from
// nibs-y5nb: an event's Nib is a snapshot taken at publish time, so a subscriber
// holding a payload never sees its fields change even when the store later
// mutates that same nib in place. Archive, Unarchive, and LoadAndUnarchive all
// rewrite Path on the stored *nib.Nib under c.mu; without a clone at publish, a
// held EventArchived payload aliases that pointer and its Path flips under the
// subscriber.
//
// The move is driven create-first so the EventArchived branch publishes the
// pointer that STAYS in the store (the create half replaces c.nibs[id] with the
// reloaded nib; the remove half then re-stores and archives that same pointer).
// Remove-first would publish an orphaned pointer that no later mutation touches,
// so it could not reproduce the bug.
//
// The deterministic guard is the Path assertion below: on unfixed code the held
// payload aliases the live store nib, so Unarchive's in-place Path write is
// visible through it. The concurrent reader is a best-effort race surfacer under
// -race, but does not reliably trip the detector (measured 0/5) — do not rely on
// it as the guard. Cloning at publish removes the value change (and closes the
// underlying race regardless of whether the detector observes it).
func TestEventPayloadsAreImmutableSnapshots(t *testing.T) {
	const nibID = "snap1"
	core, nibsDir, filename := watchingCore(t, nibID)

	archiveDir := filepath.Join(nibsDir, ArchiveDir)
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatalf("mkdir archive: %v", err)
	}
	mainAbs := filepath.Join(nibsDir, filename)
	archiveAbs := filepath.Join(archiveDir, filename)

	// Move the file into archive/ on disk, then drive the move through
	// handleChanges so it emits EventArchived carrying the stored nib (Path
	// rewritten to the archive location).
	if err := os.Rename(mainAbs, archiveAbs); err != nil {
		t.Fatalf("archive rename: %v", err)
	}

	setWatching(core)
	ch, unsub := core.Subscribe()
	defer unsub()

	driveMove(core, mainAbs, archiveAbs, true /* createFirst */)

	got := collectNibEvents(t, ch, nibID, 150*time.Millisecond)

	var held *NibEvent
	for i := range got {
		if got[i].Type == EventArchived {
			held = &got[i]
			break
		}
	}
	if held == nil || held.Nib == nil {
		t.Fatalf("expected an EventArchived carrying a nib, got: %+v", got)
	}

	wantPath := held.Nib.Path
	if !core.isArchivedPath(wantPath) {
		t.Fatalf("precondition: held payload Path = %q, want an archive path", wantPath)
	}

	// Read the held payload's Path continuously while Unarchive mutates the stored
	// nib's Path in place under c.mu. On unfixed code the payload aliases the store
	// pointer, so this is a read/write data race AND the observed value changes.
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		var sink string
		for {
			select {
			case <-stop:
				_ = sink
				return
			default:
				sink = held.Nib.Path
			}
		}
	}()

	if err := core.Unarchive(nibID); err != nil {
		t.Fatalf("Unarchive: %v", err)
	}
	close(stop)
	<-done

	if held.Nib.Path != wantPath {
		t.Fatalf("held payload Path mutated after Unarchive: got %q, want %q — "+
			"the payload aliased the live store nib instead of an immutable snapshot",
			held.Nib.Path, wantPath)
	}
}

// TestHasPayloadSubscribersPredicate pins the clone-gating decision from
// nibs-hz5b: handleChanges pays for the per-nib payload clone only when a payload
// subscriber (Subscribe) is attached — a signal-only subscriber (SubscribeSignal)
// must not flip the predicate, because it never receives a payload. This is the
// directly testable seam for "should we clone?".
//
// It bites: an implementation that always clones (hasPayloadSubscribers hardcoded
// to return true, or counting signal-only subscribers) fails the only-signal and
// no-subscribers cases below.
func TestHasPayloadSubscribersPredicate(t *testing.T) {
	tests := []struct {
		name string
		// attach registers whatever subscribers the case needs and returns a
		// cleanup that unsubscribes them.
		attach func(c *Core) func()
		want   bool
	}{
		{
			name:   "no subscribers",
			attach: func(*Core) func() { return func() {} },
			want:   false,
		},
		{
			name: "only signal-only subscriber",
			attach: func(c *Core) func() {
				_, unsub := c.SubscribeSignal()
				return unsub
			},
			want: false,
		},
		{
			name: "payload subscriber",
			attach: func(c *Core) func() {
				_, unsub := c.Subscribe()
				return unsub
			},
			want: true,
		},
		{
			name: "payload and signal-only subscribers",
			attach: func(c *Core) func() {
				_, unsubP := c.Subscribe()
				_, unsubS := c.SubscribeSignal()
				return func() { unsubP(); unsubS() }
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, _ := setupTestCore(t)
			cleanup := tt.attach(core)
			defer cleanup()

			if got := core.hasPayloadSubscribers(); got != tt.want {
				t.Fatalf("hasPayloadSubscribers() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestHasPayloadSubscribersAfterUnsubscribe pins that the count follows the
// payload map back down: once the sole payload subscriber unsubscribes, the
// predicate reverts to false and cloning is skipped again.
func TestHasPayloadSubscribersAfterUnsubscribe(t *testing.T) {
	core, _ := setupTestCore(t)

	_, unsub := core.Subscribe()
	if !core.hasPayloadSubscribers() {
		t.Fatal("hasPayloadSubscribers() = false right after Subscribe, want true")
	}
	unsub()
	if core.hasPayloadSubscribers() {
		t.Fatal("hasPayloadSubscribers() = true after unsubscribe, want false")
	}
}

// TestSubscribeSignalReceivesTick verifies a signal-only subscriber is notified
// when a debounced change is published — without requiring any payload.
func TestSubscribeSignalReceivesTick(t *testing.T) {
	core, nibsDir, filename := watchingCore(t, "sig1")
	setWatching(core)

	sigCh, unsub := core.SubscribeSignal()
	defer unsub()

	// A write to the existing nib publishes a non-empty batch (EventUpdated).
	abs := filepath.Join(nibsDir, filename)
	core.handleChanges(map[string]fsnotify.Op{abs: fsnotify.Write})

	select {
	case _, ok := <-sigCh:
		if !ok {
			t.Fatal("signal channel closed instead of ticking")
		}
	case <-time.After(time.Second):
		t.Fatal("no signal tick delivered for a published change")
	}
}

// TestFanOutSkipPathDropsPayloadDelivery pins constraint 2 of nibs-hz5b directly:
// on the skip path (payloadsCloned == false) fanOut must deliver NOTHING to a
// payload subscriber — even one that raced in after the decision — because the
// batch still holds live c.nibs pointers. Signal-only subscribers must still tick.
//
// It bites: a fanOut that delivered the uncloned batch to payload subscribers on
// the skip path would surface the batch on payloadCh below and fail the test —
// exactly the y5nb live-pointer leak.
func TestFanOutSkipPathDropsPayloadDelivery(t *testing.T) {
	core, _ := setupTestCore(t)

	payloadCh, unsubP := core.Subscribe()
	defer unsubP()
	sigCh, unsubS := core.SubscribeSignal()
	defer unsubS()

	// Stand in for the uncloned batch handleChanges would leave when the clone is
	// skipped: a live-pointer Nib that must not escape to a payload subscriber.
	live := &nib.Nib{ID: "leak1", Path: "leak1.md"}
	events := []NibEvent{{Type: EventUpdated, NibID: "leak1", Nib: live}}

	core.fanOut(events, false /* payloadsCloned */)

	// Signal-only subscriber must tick.
	select {
	case <-sigCh:
	case <-time.After(time.Second):
		t.Fatal("signal-only subscriber got no tick on the skip path")
	}

	// Payload subscriber must receive nothing (dropped), never the live-pointer batch.
	// Sound as a negative assertion, not a flaky "wait and hope nothing arrives":
	// fanOut is synchronous and already returned above, so no async sender can race
	// this select — payloadCh is provably drained-empty and stays so.
	select {
	case got := <-payloadCh:
		t.Fatalf("payload subscriber received a batch on the skip path (%+v) — "+
			"a live/uncloned pointer leaked off-lock", got)
	case <-time.After(50 * time.Millisecond):
		// Correct: the batch was dropped for the payload subscriber.
	}
}

// TestSignalOnlySkipPathRaceClean exercises the skip path under concurrent load
// with -race: a signal-only subscriber, a producer spinning handleChanges (each
// iteration loads a fresh *nib.Nib and swaps the store pointer, so the discarded
// batch holds live pointers), and store readers, all at once. It
// verifies two things: the skip path is concurrency-clean — handleChanges racing
// All/Get (and a concurrent sigCh consumer) surfaces no data race — and the
// two-WaitGroup shutdown joins cleanly (the drainer consumes sigCh concurrently
// with the producer's sends and is itself joined, so it can't leak past the
// test). Run it under `go test -race`.
//
// It does NOT pin the live-pointer-leak invariant. With no payload subscriber
// attached, fanOut has no payload channel to (mis)deliver to, and the producer
// swaps in a brand-new pointer each iteration rather than mutating a
// previously-delivered one in place, so the leak cannot manifest here. That
// invariant is pinned directly and synchronously by
// TestFanOutSkipPathDropsPayloadDelivery.
func TestSignalOnlySkipPathRaceClean(t *testing.T) {
	core, nibsDir, filename := watchingCore(t, "race1")
	setWatching(core)

	sigCh, unsub := core.SubscribeSignal()
	defer unsub()

	abs := filepath.Join(nibsDir, filename)

	stop := make(chan struct{})
	var wg sync.WaitGroup    // producer + readers: finite work
	var drain sync.WaitGroup // drainer: runs until stop is closed

	// Consume ticks concurrently with the producer so a reader races the signal
	// sends under -race. (fanOut's tick send is non-blocking — select with a
	// default — so a full sigCh never wedges the producer anyway; excess ticks
	// are simply dropped.) The drainer is on its OWN WaitGroup, not wg: it only
	// returns once stop is closed, and stop is closed after wg.Wait() — putting it
	// in wg would deadlock (wg.Wait would block on a goroutine that waits on
	// close(stop)).
	drain.Add(1)
	go func() {
		defer drain.Done()
		for {
			select {
			case <-sigCh:
			case <-stop:
				return
			}
		}
	}()

	// Producer: repeatedly publish change batches on the skip path (no payload
	// subscriber attached), so every batch carries a live c.nibs pointer.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 300 {
			core.handleChanges(map[string]fsnotify.Op{abs: fsnotify.Write})
		}
	}()

	// Concurrent store readers: if the skip path ever handed a live pointer to a
	// reader off-lock, the detector would fire here.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 300 {
			_ = core.All()
			_, _ = core.Get("race1")
		}
	}()

	wg.Wait()    // producer + readers done
	close(stop)  // signal the drainer to exit
	drain.Wait() // join the drainer so it can't outlive the test
}

// The two tests below cover nibs-oakc: an external edit to a nib file must reach
// watchers on every platform.
//
// Every nib write commits through atomicWriteFile, i.e. a rename over the
// existing file. Windows reports that replacing rename on the TARGET path as
// REMOVE followed by CREATE (verified with fsnotify v1.9.0 on Windows 11); both
// halves land inside one 100 ms debounce window and are OR-ed into a single op.
// So on Windows the routine shape of an ordinary edit is Remove|Create on a path
// whose file is very much present — and a removal branch that swallows the whole
// entry once it sees the file still there drops the edit on the floor, leaving
// the TUI and web UI showing stale data until a full reload.

// TestWatcherReplacingRenameBatchReportsUpdate drives the Windows batch shape
// directly, so it pins the behavior on every platform rather than only where the
// filesystem happens to produce it. The batch is fed as a single entry carrying
// both bits, exactly as watchLoop's `pendingChanges[name] |= op` accumulates it.
func TestWatcherReplacingRenameBatchReportsUpdate(t *testing.T) {
	const nibID = "rrn1"
	const newTitle = "Edited By Another Process"

	core, nibsDir, filename := watchingCore(t, nibID)
	abs := filepath.Join(nibsDir, filename)

	// Precondition: the on-disk edit is a real change, so a handler that ignores
	// the batch entirely cannot pass by accident.
	before, err := core.Get(nibID)
	if err != nil {
		t.Fatalf("Get before: %v", err)
	}
	if before.Title == newTitle || before.Priority == "high" {
		t.Fatalf("precondition: nib already matches the post-edit state (%+v)", before)
	}

	// The external edit itself, committed the way every writer commits: a temp
	// file renamed over the target.
	edited := fmt.Sprintf("---\ntitle: %s\nstatus: todo\npriority: high\n---\n\nbody\n", newTitle)
	if err := atomicWriteFile(abs, []byte(edited), 0o644); err != nil {
		t.Fatalf("atomic write: %v", err)
	}

	setWatching(core)
	ch, unsub := core.Subscribe()
	defer unsub()

	core.handleChanges(map[string]fsnotify.Op{abs: fsnotify.Remove | fsnotify.Create})

	got, err := core.Get(nibID)
	if err != nil {
		t.Fatalf("nib evicted by a replacing rename: Get(%q) = %v", nibID, err)
	}
	if got.Title != newTitle {
		t.Errorf("stored Title = %q, want %q — the external edit never reached the store", got.Title, newTitle)
	}
	if got.Priority != "high" {
		t.Errorf("stored Priority = %q, want %q — the external edit never reached the store", got.Priority, "high")
	}

	events := collectNibEvents(t, ch, nibID, 150*time.Millisecond)
	assertNoTypeFor(t, events, nibID, EventDeleted)
	assertHasTypeFor(t, events, nibID, EventUpdated)
}

// TestWatcherObservesExternalAtomicWrite is the end-to-end counterpart: a real
// fsnotify watch over a real replacing rename. It asserts only what a subscriber
// observes, so it reproduces natively on whichever platform emits the offending
// event shape (Windows today) and stays a valid regression guard elsewhere.
func TestWatcherObservesExternalAtomicWrite(t *testing.T) {
	const nibID = "eaw1"
	const newTitle = "Externally Edited"

	core, nibsDir := setupTestCore(t)
	b := createTestNib(t, core, nibID, "Original Title", "todo")
	abs := filepath.Join(nibsDir, filepath.Base(b.Path))

	if err := core.StartWatching(); err != nil {
		t.Fatalf("StartWatching() error = %v", err)
	}
	defer func() { _ = core.StopWatching() }()

	ch, unsub := core.Subscribe()
	defer unsub()

	// Stand in for a second process (`nibs set <id> -p high`) rewriting the file.
	edited := fmt.Sprintf("---\ntitle: %s\nstatus: todo\npriority: high\n---\n\nbody\n", newTitle)
	if err := atomicWriteFile(abs, []byte(edited), 0o644); err != nil {
		t.Fatalf("atomic write: %v", err)
	}

	// Generous relative to the 100 ms debounce: the point is whether the edit ever
	// arrives, not how promptly.
	events := collectNibEvents(t, ch, nibID, 2*time.Second)
	assertNoTypeFor(t, events, nibID, EventDeleted)
	assertHasTypeFor(t, events, nibID, EventUpdated)

	got, err := core.Get(nibID)
	if err != nil {
		t.Fatalf("Get after external write: %v", err)
	}
	if got.Title != newTitle {
		t.Errorf("stored Title = %q, want %q — the watcher never applied the external edit", got.Title, newTitle)
	}
}
