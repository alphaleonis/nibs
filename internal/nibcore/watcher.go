package nibcore

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/alphaleonis/nibs/internal/nib"
)

const debounceDelay = 100 * time.Millisecond

// EventType represents the type of change that occurred to a nib.
type EventType int

const (
	// EventCreated indicates a new nib was created.
	EventCreated EventType = iota
	// EventUpdated indicates an existing nib changed in place: either its
	// content was modified, or it was moved OUT of the archive (unarchived) —
	// a location-only change with no body edit. Both keep the nib live in the
	// store, so both surface as an update rather than a create/delete/archive.
	EventUpdated
	// EventDeleted indicates a nib was deleted.
	EventDeleted
	// EventArchived indicates a nib was moved into the archive directory. The
	// nib still exists — it lives at its new archive path and remains readable
	// and updatable — so this is distinct from EventDeleted.
	EventArchived
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
	default:
		return "unknown"
	}
}

// NibEvent represents a change to a nib.
type NibEvent struct {
	Type   EventType  // The type of change
	Nib   *nib.Nib // The nib (nil for Deleted events)
	NibID string     // Always set, useful for Deleted when Nib is nil
}

// subscription represents a subscriber to nib events.
type subscription struct {
	ch chan []NibEvent
	id uint64
}

// Subscribe creates a new subscription to nib change events.
// Returns the event channel and an unsubscribe function.
// The channel receives batches of events after debouncing.
// Callers should use defer to call the unsubscribe function.
// Internal state is committed before events are delivered: once an event
// arrives, Get/All already reflect it. Subscribers may therefore act on the
// event alone and re-read the store rather than trusting the payload.
func (c *Core) Subscribe() (<-chan []NibEvent, func()) {
	c.subMu.Lock()
	defer c.subMu.Unlock()

	id := atomic.AddUint64(&c.nextSubID, 1)
	ch := make(chan []NibEvent, 16)

	sub := &subscription{ch: ch, id: id}
	c.subscribers[id] = sub

	unsubscribe := func() {
		c.subMu.Lock()
		defer c.subMu.Unlock()
		if _, ok := c.subscribers[id]; ok {
			close(ch)
			delete(c.subscribers, id)
		}
	}

	return ch, unsubscribe
}

// fanOut sends events to all subscribers (non-blocking).
// Slow subscribers will have events dropped rather than blocking others.
func (c *Core) fanOut(events []NibEvent) {
	if len(events) == 0 {
		return
	}

	c.subMu.RLock()
	defer c.subMu.RUnlock()

	for _, sub := range c.subscribers {
		select {
		case sub.ch <- events:
			// Sent successfully
		default:
			// Subscriber is slow, drop events
		}
	}
}

// StartWatching starts watching the .nibs directory for changes, updating
// internal state incrementally (after debouncing) as nibs are created,
// modified, or deleted. Use Subscribe() to receive the resulting nib change
// events via a channel. Calling it while already watching is a no-op.
//
// Subdirectories present at this point are watched too, on a best-effort basis;
// ones created later are not, so a directory the walk missed stays unwatched
// for the watcher's lifetime. Incremental updates also mean a change the
// watcher never observes stays stale until the next full Load.
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

	// Watch all subdirectories (best effort - don't fail if any can't be watched)
	_ = filepath.WalkDir(c.root, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() || path == c.root {
			return nil
		}
		_ = watcher.Add(path)
		return nil
	})

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

// Unwatch stops watching the .nibs directory.
func (c *Core) Unwatch() error {
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

	// Close all subscriber channels
	c.subMu.Lock()
	for id, sub := range c.subscribers {
		close(sub.ch)
		delete(c.subscribers, id)
	}
	c.subMu.Unlock()

	return nil
}

// watchLoop processes filesystem events with debouncing until done is closed.
//
// done is a parameter rather than a read of c.done because the loop must be
// bound to the watch it was started for. StartWatching installs a new c.done on
// every restart, so a loop selecting on the field would see the successor's
// open channel and run forever — holding its fsnotify watcher and descriptors
// open past the Unwatch that was meant to release them.
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

			// Only care about .md files within the .nibs directory tree
			if !strings.HasSuffix(event.Name, ".md") {
				continue
			}

			// Verify the file is within the .nibs directory
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

	for path, op := range changes {
		filename := filepath.Base(path)
		// Intentionally ignore the parse error: an unparseable filename yields
		// id="", which no-ops every downstream c.nibs[id] / c.mentionIdx.Remove(id)
		// lookup — the malformed path simply falls out of the handler as a silent
		// skip. Widening this to surface the error would require a scheme for
		// reporting watcher-level errors that today's callers don't expect.
		id, _ := nib.ParseFilename(filename)

		// Handle removes/renames (file is gone from this path)
		if op&fsnotify.Remove != 0 || op&fsnotify.Rename != 0 {
			stored, exists := c.nibs[id]
			if !exists {
				continue
			}

			// Check if the file actually exists (rename might be followed by create)
			if c.fileExists(path) {
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
				archiveRel := filepath.ToSlash(filepath.Join(ArchiveDir, filename))
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
			// store at its new path and report it as updated. Evicting here would
			// silently drop a live nib whose file is present on disk.
			if fromArchive {
				mainRel := filepath.ToSlash(filename)
				if c.fileExists(filepath.Join(c.root, mainRel)) {
					stored.Path = mainRel
					c.nibs[id] = stored
					events = append(events, NibEvent{
						Type:  EventUpdated,
						Nib:   stored,
						NibID: id,
					})
					continue
				}
			}

			// Genuinely gone: a real deletion, or a delete of an already-archived
			// nib (the file that vanished is the very one stored.Path pointed at,
			// and it exists at neither the archive nor the main location now).
			delete(c.nibs, id)

			// Drop from reverse-mention index.
			c.mentionIdx.Remove(id)

			// Update search index
			if c.searchIndex != nil {
				if err := c.searchIndex.DeleteNib(id); err != nil {
					c.logWarn("failed to remove nib %s from search index: %v", id, err)
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
			// Read-only load (loadNib, NOT the persisting loadNibReconciledLocked):
			// a legacy `priority: deferred` file arriving here (e.g. a git pull in
			// the separate .nibs repo) is normalized to `low` in memory by
			// nib.Parse, but we deliberately do NOT persist from the always-on
			// watcher path. Writing back here would be an unguarded
			// read-modify-write racing an external writer, would dirty the
			// separate .nibs git tree, and would fire a spurious self-write event.
			// Disk converges on the next explicit Update/Load; bulk-Load
			// persistence lives in loadNibReconciledLocked (loadFromDisk).
			newNib, err := c.loadNib(path)
			if err != nil {
				c.logWarn("failed to load nib from %s: %v", path, err)
				continue
			}

			_, existed := c.nibs[newNib.ID]
			c.nibs[newNib.ID] = newNib

			// Refresh reverse-mention index with the new body's edges.
			c.mentionIdx.Replace(newNib.ID, newNib.Body)

			// Update search index
			if c.searchIndex != nil {
				if err := c.searchIndex.IndexNib(newNib); err != nil {
					c.logWarn("failed to index nib %s: %v", newNib.ID, err)
				}
			}

			if existed {
				events = append(events, NibEvent{
					Type:   EventUpdated,
					Nib:   newNib,
					NibID: newNib.ID,
				})
			} else {
				events = append(events, NibEvent{
					Type:   EventCreated,
					Nib:   newNib,
					NibID: newNib.ID,
				})
			}
		}
	}

	c.mu.Unlock()

	// Load-bearing ordering, not incidental: state is committed above before any
	// event is delivered, so a subscriber that re-reads via Get/All on an event
	// sees the change. The TUI does exactly that — it discards the payload and
	// re-reads — so emitting events before applying state would break it
	// silently and intermittently. Fan out outside the lock.
	c.fanOut(events)
}

// fileExists checks if a file exists at the given path.
func (c *Core) fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
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
