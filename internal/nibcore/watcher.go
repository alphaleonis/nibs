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
	// EventUpdated indicates an existing nib was modified.
	EventUpdated
	// EventDeleted indicates a nib was deleted.
	EventDeleted
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

// StartWatching begins filesystem monitoring.
// Use Subscribe() to receive nib change events via a channel.
// This is the preferred API for new code; Watch() is kept for backward compatibility.
func (c *Core) StartWatching() error {
	return c.Watch(nil)
}

// Watch starts watching the .nibs directory for changes.
// The onChange callback is invoked (after debouncing) whenever nibs are created, modified, or deleted.
// The internal state is automatically reloaded before the callback is invoked.
// Deprecated: Use StartWatching() + Subscribe() for new code.
func (c *Core) Watch(onChange func()) error {
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

	c.watching = true
	c.done = make(chan struct{})
	c.onChange = onChange
	c.mu.Unlock()

	// Start the watcher goroutine
	go c.watchLoop(watcher)

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
	c.onChange = nil

	// Close all subscriber channels
	c.subMu.Lock()
	for id, sub := range c.subscribers {
		close(sub.ch)
		delete(c.subscribers, id)
	}
	c.subMu.Unlock()

	return nil
}

// watchLoop processes filesystem events with debouncing.
func (c *Core) watchLoop(watcher *fsnotify.Watcher) {
	defer func() { _ = watcher.Close() }()

	var debounceTimer *time.Timer
	var pendingMu sync.Mutex
	pendingChanges := make(map[string]fsnotify.Op)

	for {
		select {
		case <-c.done:
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

		// Handle removes/renames (file is gone)
		if op&fsnotify.Remove != 0 || op&fsnotify.Rename != 0 {
			// Check if the file actually exists (rename might be followed by create)
			if _, exists := c.nibs[id]; exists {
				// Only delete if it was in our map and file is actually gone
				if !c.fileExists(path) {
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
						Type:   EventDeleted,
						Nib:   nil,
						NibID: id,
					})
				}
			}
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

	callback := c.onChange
	c.mu.Unlock()

	// Fan out to subscribers (outside lock)
	c.fanOut(events)

	// Invoke legacy callback
	if callback != nil {
		callback()
	}
}

// fileExists checks if a file exists at the given path.
func (c *Core) fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
