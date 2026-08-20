package nibcore

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/fsnotify/fsnotify"
)

const debounceDelay = 100 * time.Millisecond

// EventType represents the type of change that occurred to a nib.
type EventType int

const (
	// EventCreated indicates a new nib was created.
	EventCreated EventType = iota
	// EventUpdated indicates an existing nib changed in place: its content was
	// modified while it stayed live at the same location. A move OUT of the
	// archive is reported separately as EventUnarchived, not as an update.
	EventUpdated
	// EventDeleted indicates a nib was deleted.
	EventDeleted
	// EventArchived indicates a nib was moved into the archive directory. The
	// nib still exists — it lives at its new archive path and remains readable
	// and updatable — so this is distinct from EventDeleted.
	EventArchived
	// EventUnarchived indicates a nib was moved OUT of the archive directory back
	// to the main data directory. The nib stays live in the store (its Path is
	// rewritten to the main location); this is the inverse of EventArchived and is
	// distinct from EventUpdated so a viewer can clear an "archived" banner rather
	// than treat the move as an in-place content edit.
	EventUnarchived
)

// String returns a human-readable representation of the event type.
func (e EventType) String() string {
	switch e {
	case EventCreated:
		return "created"
	case EventUpdated:
		return "updated"
	case EventDeleted:
		return "deleted"
	case EventArchived:
		return "archived"
	case EventUnarchived:
		return "unarchived"
	default:
		return "unknown"
	}
}

// NibEvent represents a change to a nib.
type NibEvent struct {
	Type  EventType // The type of change
	Nib   *nib.Nib  // The nib (nil for Deleted events)
	NibID string    // Always set, useful for Deleted when Nib is nil
}

// subscription represents a subscriber to nib events.
type subscription struct {
	ch chan []NibEvent
	id uint64
}

// Subscribe creates a new PAYLOAD subscription to nib change events. It returns
// the event channel and an unsubscribe function; callers should defer the
// unsubscribe. Callers that only need to know THAT something changed (not what)
// should use SubscribeSignal instead — it skips the per-change clone described
// below.
//
// The channel receives batches of events after debouncing. Internal state is
// committed before events are delivered: once an event arrives, Get/All already
// reflect it.
//
// Payloads are immutable snapshots. Each event's Nib is a Clone taken at publish
// time, so a payload's fields never change after delivery even when the store
// later mutates that nib in place (e.g. Archive/Unarchive rewriting Path). Trust
// the payload: re-reading the store is neither required nor, for a removal
// event, possible — a deleted nib is gone from the store, so the payload is the
// only record of it. This is the single statement of the payload contract; the
// snapshot is produced in one place, in handleChanges just before fan-out.
//
// Payload vs signal-only, and who pays for the clone:
//
//   - Signal-only subscribers (SubscribeSignal) receive a bare struct{} tick and
//     never a payload, so they need no snapshot.
//   - The per-nib clone is paid ONLY when at least one payload subscriber is
//     attached at the moment handleChanges decides. With no payload subscriber
//     (e.g. the TUI, which is signal-only), the clone loop is skipped entirely
//     and the batch's live c.nibs pointers are never handed out.
//   - Attach race: a payload subscriber that attaches in the narrow window AFTER
//     that "no payload subscribers" decision but BEFORE fan-out is dropped for
//     that one batch — it is delivered nothing rather than the uncloned live
//     pointers. Dropping is already part of the contract (see backpressure
//     below); leaking a live pointer off-lock is not. The subscriber receives
//     every subsequent batch normally.
//
// Sharp edges callers must account for:
//
//   - Events are dropped under backpressure. fanOut sends non-blocking on a
//     channel buffered at 16, so once 16 batches back up for a subscriber,
//     further batches are silently dropped for it. The stream is not a reliable
//     log.
//   - A change that produces no events notifies nobody. fanOut early-returns on
//     an empty batch, so an unparseable filename, a Remove for an untracked id,
//     or a loadNib error surfaces to no subscriber.
//   - StopWatching closes and drops every subscriber channel (payload and
//     signal-only alike). Consumers must handle the channel close, and the
//     unsubscribe returned here becomes a no-op afterwards (the subscription is
//     already gone from the map).
//   - Subscribing while nothing is watched registers silently and never
//     delivers. Subscribe does not check c.watching, and cmd/serve.go treats a
//     watcher start failure as non-fatal, so a server can run with subscribers
//     attached to a watcher that never started.
func (c *Core) Subscribe() (<-chan []NibEvent, func()) {
	c.subMu.Lock()
	defer c.subMu.Unlock()

	id := atomic.AddUint64(&c.nextSubID, 1)
	ch := make(chan []NibEvent, 16)

	sub := &subscription{ch: ch, id: id}
	c.subscribers[id] = sub
	// Bump the payload-subscriber count under subMu, mirroring the map insert, so
	// handleChanges' lock-free read of it stays consistent with the map.
	c.payloadSubCount.Add(1)

	unsubscribe := func() {
		c.subMu.Lock()
		defer c.subMu.Unlock()
		if _, ok := c.subscribers[id]; ok {
			close(ch)
			delete(c.subscribers, id)
			c.payloadSubCount.Add(-1)
		}
	}

	return ch, unsubscribe
}

// SubscribeSignal creates a SIGNAL-ONLY subscription. It returns a channel that
// receives a struct{} tick whenever a debounced change is published, and an
// unsubscribe function; callers should defer the unsubscribe.
//
// Unlike Subscribe, a signal-only subscriber carries no payload — it learns only
// THAT nibs changed, not which or how. Callers that re-read the store on every
// notification (the TUI does exactly this) want this variant: it never causes the
// per-nib clone that payload subscribers pay for, so an all-signal-only fan-out
// allocates nothing per changed nib. The delivery, drop-under-backpressure, and
// StopWatching-closes-the-channel semantics are otherwise identical to Subscribe.
func (c *Core) SubscribeSignal() (<-chan struct{}, func()) {
	c.subMu.Lock()
	defer c.subMu.Unlock()

	id := atomic.AddUint64(&c.nextSubID, 1)
	ch := make(chan struct{}, 16)
	c.signalSubscribers[id] = ch

	unsubscribe := func() {
		c.subMu.Lock()
		defer c.subMu.Unlock()
		if _, ok := c.signalSubscribers[id]; ok {
			close(ch)
			delete(c.signalSubscribers, id)
		}
	}

	return ch, unsubscribe
}

// hasPayloadSubscribers reports whether at least one payload subscriber is
// currently attached. It is the clone-gating predicate: handleChanges pays for
// the per-nib payload clone only when this is true. The read is a single atomic
// load and takes no lock, so handleChanges can call it while holding c.mu without
// acquiring subMu on that hot path — sparing the lock and its contention. This is
// not for deadlock safety: the established order is c.mu -> subMu (see
// unwatchLocked/Close) and nothing acquires c.mu while holding subMu.
func (c *Core) hasPayloadSubscribers() bool {
	return c.payloadSubCount.Load() > 0
}

// fanOut delivers a change batch to subscribers (non-blocking). Slow subscribers
// have the batch dropped rather than blocking others.
//
// payloadsCloned tells fanOut whether handleChanges actually cloned every
// event's Nib. It is load-bearing for correctness, not a mere optimization flag:
// when it is false the events still carry LIVE c.nibs pointers (the clone loop
// was skipped because no payload subscriber was attached at the decision point),
// so those events MUST NOT reach any payload subscriber — handing a live pointer
// out off-lock is the y5nb race. Signal-only subscribers carry no payload, so
// they always get a bare tick regardless.
//
// Payload subscribers therefore receive the batch only when payloadsCloned is
// true. A payload subscriber that attached after the (false) decision but before
// this call is consequently delivered nothing for this batch — the documented
// attach-race drop (see Subscribe). Dropping is safe; delivering the uncloned
// batch would not be.
func (c *Core) fanOut(events []NibEvent, payloadsCloned bool) {
	if len(events) == 0 {
		return
	}

	c.subMu.RLock()
	defer c.subMu.RUnlock()

	if payloadsCloned {
		for _, sub := range c.subscribers {
			select {
			case sub.ch <- events:
				// Sent successfully
			default:
				// Subscriber is slow, drop events
			}
		}
	}

	for _, ch := range c.signalSubscribers {
		select {
		case ch <- struct{}{}:
			// Ticked successfully
		default:
			// Subscriber is slow, drop the tick
		}
	}
}

// StartWatching starts watching the store for changes, updating internal state
// incrementally (after debouncing) as nibs are created, modified, or deleted.
// Use Subscribe() to receive the resulting nib change events via a channel.
// Calling it while already watching is a no-op.
//
// The watched set is the store's own directories (store.Layout.WatchableDirs:
// the root plus data/ and archive/ where they exist) plus any subdirectories
// under them, on a best-effort basis. The ROOT is watched even though it holds
// no nib files: a data/ or archive/ directory created after the watch starts
// arrives as a create event there, and watchLoop adds the new directory to the
// watch rather than leaving it a blind spot for the watcher's lifetime.
// Incremental updates still mean a change the watcher never observes stays
// stale until the next full Load.
func (c *Core) StartWatching() error {
	c.mu.Lock()
	if c.watching {
		c.mu.Unlock()
		return nil // Already watching
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		c.mu.Unlock()
		return err
	}

	if err := watcher.Add(c.root); err != nil {
		_ = watcher.Close()
		c.mu.Unlock()
		return err
	}

	// Watch the store's content directories and anything nested under them
	// (best effort — don't fail if any can't be watched).
	for _, dir := range c.layout.WatchableDirs() {
		if dir == c.root {
			continue // added above, and its failure is fatal
		}
		_ = watcher.Add(dir)
		_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || !d.IsDir() || path == dir {
				return nil
			}
			_ = watcher.Add(path)
			return nil
		})
	}

	// Capture the done channel in a local while the lock is still held, and hand
	// that local to the loop. The loop must never re-read c.done: a restart
	// assigns a fresh channel, and a loop that reads the field would latch onto
	// the new one and never exit. Reading the field at the `go` statement below
	// would be just as wrong — the lock is already released by then.
	c.watching = true
	done := make(chan struct{})
	c.done = done
	c.mu.Unlock()

	// Start the watcher goroutine
	go c.watchLoop(watcher, done)

	return nil
}

// StopWatching stops watching the .nibs directory.
func (c *Core) StopWatching() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.unwatchLocked()
}

// unwatchLocked stops watching (must be called with lock held).
func (c *Core) unwatchLocked() error {
	if !c.watching {
		return nil
	}

	close(c.done)
	c.watching = false

	// Close all subscriber channels (both kinds). Resetting payloadSubCount to 0
	// under subMu keeps it consistent with the now-empty payload map; a straggling
	// unsubscribe for one of these dropped subscriptions no-ops (its id is gone from
	// the map) and so cannot double-decrement below zero.
	c.subMu.Lock()
	for id, sub := range c.subscribers {
		close(sub.ch)
		delete(c.subscribers, id)
	}
	for id, ch := range c.signalSubscribers {
		close(ch)
		delete(c.signalSubscribers, id)
	}
	c.payloadSubCount.Store(0)
	c.subMu.Unlock()

	return nil
}

// watchLoop processes filesystem events with debouncing until done is closed.
//
// done is a parameter rather than a read of c.done because the loop must be
// bound to the watch it was started for. StartWatching installs a new c.done on
// every restart, so a loop selecting on the field would see the successor's
// open channel and run forever — holding its fsnotify watcher and descriptors
// open past the StopWatching that was meant to release them.
func (c *Core) watchLoop(watcher *fsnotify.Watcher, done <-chan struct{}) {
	defer func() { _ = watcher.Close() }()

	var debounceTimer *time.Timer
	var pendingMu sync.Mutex
	pendingChanges := make(map[string]fsnotify.Op)

	for {
		select {
		case <-done:
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return

		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			// A DIRECTORY appearing inside the store has to join the watch.
			// data/ and archive/ are created on demand — by `nibs migrate`, by
			// the first archive, by a `git pull` in the store repo — and a
			// directory that appears after the watch started would otherwise
			// stay a permanent blind spot: every nib file inside it is
			// invisible for the rest of the process's life.
			if event.Op&fsnotify.Create != 0 && c.isStoreSubdir(event.Name) {
				_ = watcher.Add(event.Name)
				// Fall through: a directory is never a .md file, so the .md
				// filter below drops it. Files that landed inside it before
				// the watch was added are picked up by the next full Load.
			}

			// Only care about .md files within the store's directory tree
			if !strings.HasSuffix(event.Name, ".md") {
				continue
			}

			// Verify the file is within the store directory
			relPath, err := filepath.Rel(c.root, event.Name)
			if err != nil || strings.HasPrefix(relPath, "..") {
				continue
			}

			// Check if this is a relevant event
			relevant := event.Op&fsnotify.Create != 0 ||
				event.Op&fsnotify.Write != 0 ||
				event.Op&fsnotify.Remove != 0 ||
				event.Op&fsnotify.Rename != 0

			if !relevant {
				continue
			}

			// Accumulate changes during debounce window
			pendingMu.Lock()
			pendingChanges[event.Name] |= event.Op
			pendingMu.Unlock()

			// Start/reset debounce timer
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(debounceDelay, func() {
				// Swap out pending changes atomically
				pendingMu.Lock()
				changes := pendingChanges
				pendingChanges = make(map[string]fsnotify.Op)
				pendingMu.Unlock()

				c.handleChanges(changes)
			})

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			// Log errors but continue watching
			_ = err // In production, you might want to log this
		}
	}
}

// handleChanges processes only the files that changed, updating state incrementally.
func (c *Core) handleChanges(changes map[string]fsnotify.Op) {
	if len(changes) == 0 {
		return
	}

	c.mu.Lock()

	// Check if we're still watching
	if !c.watching {
		c.mu.Unlock()
		return
	}

	var events []NibEvent

	// One batch's warning allowance, shared by every per-file diagnostic in the
	// loop below. A debounce window holds whatever changed at once — a `git pull`
	// in the separate .nibs repository is the event that makes that the whole
	// store — so an unbounded stream here reaches a long-lived serve's stderr the
	// way loadFromDisk's reached every command's (see warnBudget).
	warns := &warnBudget{c: c}

	// Whether this batch changed the set of stored ids in a way that can re-point
	// a link held by a nib the batch never touched, which widens the
	// canonicalization pass below from this batch's own nibs to the whole store.
	// Two shapes do that, and only those two:
	//
	//   - An id ARRIVING that was not in the store before. A short-form link
	//     written before the nib it names was unresolvable at load and correctly
	//     left verbatim; only the target's arrival can resolve it.
	//   - A bare-token id LEAVING while its prefixed twin remains. Resolution
	//     tries an exact map key before the prefix-prepended form, so a link
	//     stored with the bare spelling starts answering for the twin instead.
	//
	// A batch that only edits existing nibs pays the cheap touched-only pass.
	var scanAll bool

	for path, op := range changes {
		filename := filepath.Base(path)
		// Intentionally ignore the parse error: an unparseable filename yields
		// id="", which no-ops every downstream c.nibs[id] / c.mentionIdx.Remove(id)
		// lookup — the malformed path simply falls out of the handler as a silent
		// skip. Widening this to surface the error would require a scheme for
		// reporting watcher-level errors that today's callers don't expect.
		id, _ := nib.ParseFilename(filename, c.configPrefix())

		// Handle removes/renames, but only where the file really is gone from this
		// path. A removal bit on a path that still holds a file is not a removal at
		// all, and must fall through to the create/write handling below.
		//
		// This is the common case on Windows, not a corner: every nib write commits
		// through AtomicWriteFile, i.e. a rename over the existing file, and
		// ReadDirectoryChangesW reports that replacing rename on the TARGET path as
		// REMOVE followed by CREATE. Both halves land in one debounce window and
		// watchLoop ORs them into a single op, so an ordinary external edit arrives
		// here as Remove|Create on a file that exists. Swallowing the entry on the
		// removal branch dropped the edit entirely, leaving the TUI and web UI stale
		// until a full reload (nibs-oakc).
		if (op&fsnotify.Remove != 0 || op&fsnotify.Rename != 0) && !c.fileExists(path) {
			stored, exists := c.nibs[id]
			if !exists {
				continue
			}

			// A file leaving `path` is archive, unarchive, or deletion. Decide
			// from the filesystem's CURRENT truth — the event path plus where the
			// file lives now — never from stored.Path and never from batch order.
			//
			// stored.Path is authoritative only for a move THIS process made:
			// Archive/Unarchive/LoadAndUnarchive rewrite it under the lock this
			// handler takes. Any other mover (the CLI against a running server, a
			// pull in the separate .nibs repo) leaves it stale, so keying the
			// decision off it misreports the move. Both halves of a move — the
			// rename at the old path and the create at the new one — land in one
			// debounce batch that iterates as a Go map, so which half updates the
			// store first is not stable run to run. Reading only on-disk facts,
			// which are identical whichever half runs first, removes that
			// dependence entirely.
			fromArchive := c.isArchivedAbsPath(path)

			// Moved INTO the archive: the file left a main-directory path and now
			// exists at archive/<basename>. An archive by any mover, in-process or
			// not. Emitting archived REQUIRES the archive file to exist, so a
			// misdetection can never strand a phantom — the deliberate safe bias.
			if !fromArchive {
				archiveRel := c.layout.ArchiveRel(filename)
				if c.fileExists(filepath.Join(c.root, archiveRel)) {
					stored.Path = archiveRel
					c.nibs[id] = stored
					events = append(events, NibEvent{
						Type:  EventArchived,
						Nib:   stored,
						NibID: id,
					})
					continue
				}
			}

			// Moved OUT of the archive: the file left an archive path and now
			// exists at the main-directory <basename>. An unarchive
			// (LoadAndUnarchive/Unarchive). The nib is NOT gone — keep it in the
			// store at its new path and report it as EventUnarchived, the distinct
			// inverse of EventArchived (so a viewer can clear an "archived" banner
			// rather than treat this as an in-place edit — nibs-2fgz). Evicting here
			// would silently drop a live nib whose file is present on disk.
			if fromArchive {
				mainRel := c.layout.DataRel(filename)
				if c.fileExists(filepath.Join(c.root, mainRel)) {
					stored.Path = mainRel
					c.nibs[id] = stored
					events = append(events, NibEvent{
						Type:  EventUnarchived,
						Nib:   stored,
						NibID: id,
					})
					continue
				}
			}

			// Same-id slug rename: the file left `path`, but a file whose parsed id
			// equals this id lives elsewhere in the store. A slug rename changes the
			// basename (nibs-x--old-slug.md -> nibs-x--new-slug.md), so neither cheap
			// basename check above matched — yet nib.ParseFilename yields the same id
			// for both names. The nib is NOT gone: point it at the file's new
			// location, re-derive its filename-sourced Slug, and report the move as
			// an update, keeping it live in the store. Evicting here would drop a
			// live nib whose file is present on disk under a new name. Bounded —
			// reached only after both basename checks miss, right before the
			// genuine-delete fall-through.
			//
			// Copy-on-write: this changes Slug, a NON-Path field, so it must land on
			// a FRESH pointer rather than editing the published one — mutating Slug
			// in place would let an off-lock reader (the GraphQL Nibs pipeline)
			// observe it torn mid-write. Only Path may change in place on a stored
			// pointer (the archive/unarchive branches above do so); the slug-rename
			// case additionally re-derives Slug and therefore clones. See the
			// canonical invariant at NibReader.GetSnapshot (internal/graph/interfaces.go).
			if newRel, ok := c.findRelPathByID(id); ok {
				updated := stored.Clone()
				updated.Path = newRel
				_, updated.Slug = nib.ParseFilename(filepath.Base(newRel), c.configPrefix())
				c.nibs[id] = updated
				events = append(events, NibEvent{
					Type:  EventUpdated,
					Nib:   updated,
					NibID: id,
				})
				continue
			}

			// Genuinely gone: a real deletion, or a delete of an already-archived
			// nib (the file that vanished is the very one stored.Path pointed at,
			// and it exists at neither the archive nor the main location now).
			delete(c.nibs, id)

			// Evaluated per removal against the store as it stands, so a batch
			// deleting BOTH spellings of one id can set this from an intermediate
			// state that the final map no longer justifies (map iteration order
			// decides). Harmless in the direction that matters: the sweep re-resolves
			// against the post-batch map, so a spurious widening costs one pass and
			// rewrites nothing.
			//
			// The opposite direction — reading false here because the twin is not in
			// the map YET, while the final map does hold it — cannot happen, and it
			// is worth saying why because the reason lives in another branch: the
			// only key INSERTION in this loop is the create/write branch below, which
			// sets scanAll itself whenever the id was not already stored. A twin
			// appearing later in the batch is therefore covered by the arrival shape,
			// whatever the iteration order. Any future branch that installs a new key
			// (an unarchive discovering an unstored nib, a re-keying rename) must
			// preserve that bookkeeping or this gate silently stops firing.
			if c.removalCanRebindLinksLocked(id) {
				scanAll = true
			}

			// Drop from reverse-mention index.
			c.mentionIdx.Remove(id)

			// Update search index
			if c.searchIndex != nil {
				if err := c.searchIndex.DeleteNib(id); err != nil {
					warns.warn("failed to remove nib %s from search index: %v", id, err)
				}
			}

			events = append(events, NibEvent{
				Type:  EventDeleted,
				Nib:   nil,
				NibID: id,
			})
			continue
		}

		// Handle creates/writes (file exists or was created)
		if op&fsnotify.Create != 0 || op&fsnotify.Write != 0 {
			// Read-only load: like every load path, the watcher NEVER writes. A
			// write here would be an unguarded read-modify-write racing an
			// external writer (git checkout / editor / second instance), would
			// dirty the separate .nibs git tree, and would fire a spurious
			// self-write event. A legacy-shaped file arriving here (e.g. a git
			// pull in the separate .nibs repo) loads exactly as written;
			// rewriting it is `nibs migrate`'s job.
			newNib, err := c.loadNib(path)
			if err != nil {
				warns.warn("failed to load nib from %s: %v", path, err)
				continue
			}

			// One visible breadcrumb when the arriving file carries a shape
			// only `nibs migrate` (or a newer nibs) may rewrite. The pre-run
			// migration gate fired once at process start, so this ingress is
			// the one path a legacy or newer file takes into a LIVE store —
			// it loads as written (see above) and every query observes it
			// until migrate runs, which without this line happens with no
			// signal anywhere.
			// "Legacy shape" here restates the migration chain's detection
			// (migrationSteps in cmd/migrate.go): below-current version for
			// the version-keyed steps, plus each value-keyed step's condition
			// (today: the priority-deferred step). A future step not keyed on
			// the version must be mirrored into this condition, or a file it
			// pends arrives into a live serve with no breadcrumb at all.
			if newNib.Version > nib.CurrentVersion {
				warns.warn("nib file %s arrived with format version %d, newer than this build supports (%d); leave it to a newer nibs", path, newNib.Version, nib.CurrentVersion)
			} else if newNib.Version < nib.CurrentVersion || newNib.Priority == "deferred" {
				warns.warn("nib file %s arrived with a legacy shape; it loads as written until `nibs migrate` runs (stop serve first)", path)
			}

			// Two files parsing to one id, the arrival half. The load walk warns
			// about this from what it sees on disk; here the store is the only
			// witness to the file already answering for the id, and the arriving
			// file wins whichever it was — so without this line a live serve
			// swapped one for the other with nothing on stderr, and which one won
			// depended on the debounce batch's map iteration order.
			//
			// A batch holding BOTH files of a pair can warn twice, naming opposite
			// shadow directions. That is two real swaps narrated in the order they
			// happened, not a contradiction, and the last line always names the file
			// the store ends up answering with — collapsing the pair to the first
			// line would name a file that lost.
			existing, existed := c.nibs[newNib.ID]
			if existed && c.arrivingShadowsStored(newNib.Path, existing.Path) {
				warns.warn("duplicate nib id %q on disk: %s shadows %s (the arriving file wins; resolve the duplicate)",
					newNib.ID, path, filepath.Join(c.root, existing.Path))
			}
			c.nibs[newNib.ID] = newNib

			// Refresh reverse-mention index with the new body's edges.
			c.mentionIdx.Replace(newNib.ID, newNib.Body)

			// Update search index
			if c.searchIndex != nil {
				if err := c.searchIndex.IndexNib(newNib); err != nil {
					warns.warn("failed to index nib %s: %v", newNib.ID, err)
				}
			}

			if existed {
				events = append(events, NibEvent{
					Type:  EventUpdated,
					Nib:   newNib,
					NibID: newNib.ID,
				})
			} else {
				scanAll = true
				events = append(events, NibEvent{
					Type:  EventCreated,
					Nib:   newNib,
					NibID: newNib.ID,
				})
			}
		}
	}

	// Closed at the end of the per-file stream it speaks for, before the passes
	// below that warn about nothing.
	warns.close()

	// Resolve short-form link ids against the post-batch store (see
	// canonicalize.go). This runs AFTER the whole batch, not per file as it is
	// loaded: two files arriving together are visited in map order, so a
	// dependent can be read before the target it names is in the store.
	//
	// The index maps each changed nib to every event carrying its payload, so a
	// nib canonicalized as part of its own arrival has that event's payload
	// swapped for the canonicalized pointer instead of collecting a second,
	// contradictory event. It doubles as the candidate set for the narrow pass.
	touched := make(map[string][]int, len(events))
	for i, e := range events {
		if e.Nib != nil {
			touched[e.NibID] = append(touched[e.NibID], i)
		}
	}
	events = c.canonicalizeLinksAfterBatchLocked(events, touched, scanAll)

	// Snapshot every payload while the lock is still held: each event carries a
	// Clone, not the live c.nibs pointer, so a subscriber's payload fields cannot
	// change after delivery even when the store later mutates that same nib in
	// place — Archive, Unarchive, and LoadAndUnarchive all rewrite Path on the
	// stored pointer. This is the single choke point that makes every published
	// payload an immutable snapshot; the contract itself is documented on
	// Subscribe. Cloning here (under the lock) also keeps Clone's field reads from
	// racing those in-place writers, which hold the same lock. One allocation per
	// changed nib, not per subscriber.
	//
	// Gate the clone on at least one payload subscriber being attached. With only
	// signal-only subscribers (e.g. the TUI, which re-reads the store on every
	// tick and discards payloads) the clone is pure waste, so skip it. The read is
	// a single atomic load taking no lock, so on this hot path it avoids acquiring
	// subMu at all — no lock, no contention, and no coupling with the subsequent
	// fanOut's subMu.RLock. This is not about deadlock: the established lock order
	// is c.mu -> subMu (unwatchLocked/Close both take subMu.Lock under c.mu) and
	// nothing ever acquires c.mu while holding subMu, so a subMu-guarded read here
	// would be deadlock-free too — just needlessly locked. Rule for the future:
	// never acquire c.mu while holding subMu.
	//
	// When the clone is skipped, events[i].Nib stays a LIVE c.nibs pointer (set to
	// newNib/stored above, never nil'd), so this batch must never be delivered to a
	// payload subscriber. fanOut, told cloningPayloads == false, delivers to signal
	// subscribers only. The attach race — a payload subscriber appearing between
	// this decision and fanOut — resolves by dropping the batch for it (see fanOut
	// and Subscribe): correct under the drop contract, and strictly safer than
	// leaking a live pointer.
	cloningPayloads := c.hasPayloadSubscribers()
	if cloningPayloads {
		for i := range events {
			if events[i].Nib != nil {
				events[i].Nib = events[i].Nib.Clone()
			}
		}
	}

	c.mu.Unlock()

	// Load-bearing ordering, not incidental: state is committed above before any
	// event is delivered, so a subscriber that re-reads via Get/All on an event
	// sees the change. The TUI does exactly that — it discards the payload and
	// re-reads — so emitting events before applying state would break it
	// silently and intermittently. Fan out outside the lock.
	c.fanOut(events, cloningPayloads)
}

// arrivingShadowsStored reports whether an arriving file collides with a
// DIFFERENT file that already answers for its id, rather than being that same
// file again.
//
// Two conditions, and neither is redundant. A file rewritten in place arrives at
// the path the store already holds, so an equal path is an edit rather than a
// collision. And a move by an OUTSIDE mover — the CLI against a running server, a
// pull in the separate .nibs repository — reaches the create half with the store
// still holding the OLD path, because the removal half that updates it may not
// have run yet: both halves land in one debounce batch and iterate as a Go map.
// So a differing path alone reports an ordinary archive as a duplicate,
// non-deterministically. What separates a move from a collision is whether the old
// file is still THERE. An in-process Archive/Unarchive/LoadAndUnarchive is not in
// that family: each rewrites the stored Path under the lock this handler takes, so
// its create half arrives with the paths already equal — the same in-process /
// external split the removal branch above turns on.
//
// The answer drives a MESSAGE and nothing else, so it errs generous where it
// cannot tell a collision apart: a LINK, two directory entries for one file, which
// the load walk also calls a duplicate; a case-only difference between the stored
// spelling and the event's on a folding volume, which the load walk does not, and
// which nothing here can reproduce to prove a guard would work; a stored file that
// no longer parses as a nib, which the load walk calls unparseable instead; and
// [Inference, not reproduced] the window of a copy-then-delete slow enough to
// straddle the debounce. It errs quiet in one: a stat failing for any reason other
// than absence — an unreadable parent — reads as absence, so a real collision
// under one goes unreported.
func (c *Core) arrivingShadowsStored(arrivingRel, storedRel string) bool {
	if storedRel == arrivingRel {
		return false
	}
	return c.fileExists(filepath.Join(c.root, storedRel))
}

// fileExists checks if a file exists at the given path.
func (c *Core) fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// isStoreSubdir reports whether an absolute path is a DIRECTORY inside the
// store — the test watchLoop applies before extending the watch to a
// newly-created directory. Dot directories are excluded for the same reason
// WalkStoreFiles prunes them: `.git`, `.obsidian` and the like are not store
// content, and watching them would fill the debounce batches with noise.
func (c *Core) isStoreSubdir(absPath string) bool {
	rel, err := filepath.Rel(c.root, absPath)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if strings.HasPrefix(part, ".") {
			return false
		}
	}
	info, err := os.Stat(absPath)
	return err == nil && info.IsDir()
}

// isArchivedAbsPath reports whether an absolute filesystem path lies within the
// archive directory under the nibs root. It is the on-disk-location counterpart
// to isArchivedPath (which classifies a stored, root-relative Path); the removal
// branch uses it to read the move's direction from the event path rather than
// from the possibly-stale stored Path.
func (c *Core) isArchivedAbsPath(absPath string) bool {
	rel, err := filepath.Rel(c.root, absPath)
	if err != nil {
		return false
	}
	return c.isArchivedPath(filepath.ToSlash(rel))
}

// findRelPathByID scans the store's content directories (data/ and archive/)
// for a nib file whose parsed id equals id, returning its root-relative,
// forward-slash path. It recognizes a nib by id rather than by exact basename, so
// it locates a file that a same-id slug rename moved to a new name — the case the
// removal branch's two basename checks (archive-in, unarchive-out) cannot match.
// The scan is bounded to two os.ReadDir calls and is reached only on the removal
// branch's delete fall-through, so the two basename checks stay the cheap fast
// path.
//
// Scanning the store ROOT instead of data/ would misread every ordinary move:
// nib files no longer live there, so the scan would find nothing and the
// removal branch would fall through to a genuine deletion, dropping a live nib
// whose file is present on disk.
func (c *Core) findRelPathByID(id string) (string, bool) {
	if id == "" {
		return "", false
	}
	for _, dir := range []string{c.layout.DataDir(), c.layout.ArchiveDir()} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			fileID, _ := nib.ParseFilename(entry.Name(), c.configPrefix())
			if fileID != id {
				continue
			}
			rel, err := filepath.Rel(c.root, filepath.Join(dir, entry.Name()))
			if err != nil {
				continue
			}
			return filepath.ToSlash(rel), true
		}
	}
	return "", false
}
