package nibcore

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/fsnotify/fsnotify"
)

// TestCoreMutators_FreezeGuard is the deterministic, store-level guard for the
// copy-on-write invariant documented canonically at NibReader.GetSnapshot
// (internal/graph/interfaces.go): a Core mutator may only ever change Path in
// place on a pointer already published in c.nibs; every OTHER field change must
// be copy-on-write (install a fresh *nib.Nib under the map key). An off-lock
// reader — the GraphQL Nibs filter/sort pipeline, gqlgen's async marshaler —
// still holding the old pointer must never observe a non-Path field change torn
// mid-write.
//
// The guard drives every Core mutator against an already-published nib and
// asserts that no non-Path field of the previously-published pointer changed.
// The mechanism is uniform and field-agnostic: before the mutation it captures
// EVERY live c.nibs pointer plus a deep clone of each (the frozen "before"),
// runs the mutator, then re-reads each SAME captured pointer and compares it to
// its clone with Path neutralized on both sides. Because the comparison is a
// whole-struct reflect.DeepEqual, a new VALUE-typed field on nib.Nib is covered
// automatically; a new slice/map/pointer field is covered only once
// nib.Nib.Clone() deep-copies it (so the "before" snapshot stays independent) —
// extend Clone() (and TestNibClone) alongside such a field.
//
// Why this bites the historical drift: every regression in this area reassigned
// a non-Path field (Title, Parent, BlockedBy, Order, ...) on the stored pointer
// instead of on a fresh clone. Such a reassignment makes the captured pointer's
// field diverge from its "before" clone, which DeepEqual reports. A copy-on-write
// mutator, by contrast, installs a DIFFERENT pointer and leaves the captured one
// fully frozen (even Path), which also passes. Path is the sole in-place exception
// (Archive/Unarchive/LoadAndUnarchive and the watcher's move/rename branches
// rewrite it under c.mu), so it is excluded from the comparison.
//
// The "before" snapshot is a DEEP clone (not a shallow *p copy) on purpose: a
// shallow copy would share slice/map backing storage with the live pointer, so an
// in-place element write (e.g. p.BlockedBy[0] = x) would mutate both sides and
// slip past DeepEqual. The deep clone makes that class visible for the top-level
// slices Clone() copies (Tags, BlockedBy, Blocking, Documents); Extra's nested
// yaml.Node.Content is shared by Clone(), so an in-place edit there is not
// covered (no current mutator touches it).
func TestCoreMutators_FreezeGuard(t *testing.T) {
	tests := []struct {
		name string
		// setup arranges already-published nibs and any pre-state (e.g. archiving
		// a nib, enabling the watcher). It runs BEFORE the pointers are snapshotted.
		setup func(t *testing.T, c *Core, dir string)
		// mutate runs the mutator under guard, AFTER the pointers are snapshotted.
		mutate func(t *testing.T, c *Core, dir string)
	}{
		{
			name: "Create", // No pre-published target of its own; asserts Create
			// does not disturb an already-published sibling in place.
			setup: func(t *testing.T, c *Core, _ string) {
				createTestNib(t, c, "existing", "Existing", "todo")
			},
			mutate: func(t *testing.T, c *Core, _ string) {
				if err := c.Create(&nib.Nib{ID: "fresh", Title: "Fresh", Status: "todo"}); err != nil {
					t.Fatalf("Create: %v", err)
				}
			},
		},
		{
			name: "Update",
			setup: func(t *testing.T, c *Core, _ string) {
				createTestNib(t, c, "upd", "Original", "todo")
			},
			mutate: func(t *testing.T, c *Core, _ string) {
				stored, err := c.Get("upd")
				if err != nil {
					t.Fatalf("Get: %v", err)
				}
				// The blessed write shape: mutate an OWNED clone, then Update. Update
				// must install the clone as a FRESH pointer, leaving the old one frozen.
				clone := stored.Clone()
				clone.Title = "Changed"
				clone.Status = "in-progress"
				clone.Tags = []string{"newtag"}
				if err := c.Update(clone, nil); err != nil {
					t.Fatalf("Update: %v", err)
				}
			},
		},
		{
			name: "reorder-store-write-via-Update", // Approximates the reorder path.
			// The Orderer / bulk-reorder resolvers (internal/graph) funnel every
			// stored-nib order write through Core.Update (clone from GetForUpdate,
			// set Order, Update) — see orderer.go:backfillOrderKeys and
			// bulkreorder.go. That graph orchestration can't be driven from an
			// internal nibcore test (graph imports nibcore — an import cycle), so we
			// drive its sole store-write funnel here with the exact Order-key change
			// it performs.
			setup: func(t *testing.T, c *Core, _ string) {
				createTestNib(t, c, "ord", "Orderable", "todo")
			},
			mutate: func(t *testing.T, c *Core, _ string) {
				stored, err := c.Get("ord")
				if err != nil {
					t.Fatalf("Get: %v", err)
				}
				clone := stored.Clone()
				clone.Order = nib.OrderInitial()
				if err := c.Update(clone, nil); err != nil {
					t.Fatalf("Update (reorder): %v", err)
				}
			},
		},
		{
			name: "Delete",
			setup: func(t *testing.T, c *Core, _ string) {
				createTestNib(t, c, "keep", "Keep", "todo")
				createTestNib(t, c, "drop", "Drop", "todo")
			},
			mutate: func(t *testing.T, c *Core, _ string) {
				if err := c.Delete("drop"); err != nil {
					t.Fatalf("Delete: %v", err)
				}
			},
		},
		{
			name: "Archive", // Rewrites Path in place — the sanctioned exception.
			setup: func(t *testing.T, c *Core, _ string) {
				createTestNib(t, c, "arch", "Archive Me", "todo")
			},
			mutate: func(t *testing.T, c *Core, _ string) {
				if err := c.Archive("arch"); err != nil {
					t.Fatalf("Archive: %v", err)
				}
			},
		},
		{
			name: "Unarchive", // Rewrites Path in place — the sanctioned exception.
			setup: func(t *testing.T, c *Core, _ string) {
				createTestNib(t, c, "unarch", "Unarchive Me", "todo")
				if err := c.Archive("unarch"); err != nil {
					t.Fatalf("setup Archive: %v", err)
				}
			},
			mutate: func(t *testing.T, c *Core, _ string) {
				if err := c.Unarchive("unarch"); err != nil {
					t.Fatalf("Unarchive: %v", err)
				}
			},
		},
		{
			name: "LoadAndUnarchive", // Rewrites Path in place — the sanctioned exception.
			setup: func(t *testing.T, c *Core, _ string) {
				createTestNib(t, c, "loadun", "Load And Unarchive", "todo")
				if err := c.Archive("loadun"); err != nil {
					t.Fatalf("setup Archive: %v", err)
				}
			},
			mutate: func(t *testing.T, c *Core, _ string) {
				if _, err := c.LoadAndUnarchive("loadun"); err != nil {
					t.Fatalf("LoadAndUnarchive: %v", err)
				}
			},
		},
		{
			name: "RemoveLinksTo",
			setup: func(t *testing.T, c *Core, _ string) {
				createTestNib(t, c, "target", "Target", "todo")
				// A child pointing at target via both parent and blocked_by so the
				// mutator has real links to strip (and must do it copy-on-write).
				if err := c.Create(&nib.Nib{
					ID: "linker", Title: "Linker", Status: "todo",
					Parent: "target", BlockedBy: []string{"target"},
				}); err != nil {
					t.Fatalf("setup Create linker: %v", err)
				}
			},
			mutate: func(t *testing.T, c *Core, _ string) {
				if _, err := c.RemoveLinksTo("target"); err != nil {
					t.Fatalf("RemoveLinksTo: %v", err)
				}
			},
		},
		{
			name: "FixBrokenLinks",
			setup: func(t *testing.T, c *Core, _ string) {
				// A parent link to a nonexistent nib is a broken link FixBrokenLinks
				// clears; Create does not validate parent existence.
				if err := c.Create(&nib.Nib{
					ID: "broken", Title: "Broken", Status: "todo", Parent: "ghost",
				}); err != nil {
					t.Fatalf("setup Create broken: %v", err)
				}
			},
			mutate: func(t *testing.T, c *Core, _ string) {
				if _, err := c.FixBrokenLinks(); err != nil {
					t.Fatalf("FixBrokenLinks: %v", err)
				}
			},
		},
		{
			name: "watcher-reload-write", // External edit: installs a FRESH pointer.
			setup: func(t *testing.T, c *Core, _ string) {
				createTestNib(t, c, "wreload", "Watch Reload", "todo")
				setWatching(c)
			},
			mutate: func(t *testing.T, c *Core, dir string) {
				stored, err := c.Get("wreload")
				if err != nil {
					t.Fatalf("Get: %v", err)
				}
				path := filepath.Join(dir, stored.Path)
				edited := stored.Clone()
				edited.Body = "externally edited body\n"
				content, err := edited.Render()
				if err != nil {
					t.Fatalf("Render: %v", err)
				}
				if err := os.WriteFile(path, content, 0644); err != nil {
					t.Fatalf("WriteFile: %v", err)
				}
				c.handleChanges(map[string]fsnotify.Op{path: fsnotify.Write})
			},
		},
		{
			name: "watcher-archive-move", // External archive: rewrites Path in place.
			setup: func(t *testing.T, c *Core, _ string) {
				createTestNib(t, c, "wmove", "Watch Move", "todo")
				setWatching(c)
			},
			mutate: func(t *testing.T, c *Core, dir string) {
				stored, err := c.Get("wmove")
				if err != nil {
					t.Fatalf("Get: %v", err)
				}
				oldPath := filepath.Join(dir, stored.Path)
				archiveDir := filepath.Join(dir, ArchiveDir)
				if err := os.MkdirAll(archiveDir, 0755); err != nil {
					t.Fatalf("MkdirAll: %v", err)
				}
				newPath := filepath.Join(archiveDir, filepath.Base(stored.Path))
				if err := os.Rename(oldPath, newPath); err != nil {
					t.Fatalf("Rename: %v", err)
				}
				c.handleChanges(map[string]fsnotify.Op{oldPath: fsnotify.Rename})
			},
		},
		{
			name: "watcher-slug-rename", // Same-id rename: rewrites Path in place.
			setup: func(t *testing.T, c *Core, _ string) {
				createTestNib(t, c, "wrename", "Watch Rename", "todo")
				setWatching(c)
			},
			mutate: func(t *testing.T, c *Core, dir string) {
				stored, err := c.Get("wrename")
				if err != nil {
					t.Fatalf("Get: %v", err)
				}
				oldPath := filepath.Join(dir, stored.Path)
				newName := nib.BuildFilename(stored.ID, "renamed-slug")
				newPath := filepath.Join(dir, newName)
				if err := os.Rename(oldPath, newPath); err != nil {
					t.Fatalf("Rename: %v", err)
				}
				c.handleChanges(map[string]fsnotify.Op{oldPath: fsnotify.Rename})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, dir := setupTestCore(t)
			tt.setup(t, c, dir)

			snaps := snapshotPublishedPointers(c)
			if len(snaps) == 0 {
				t.Fatal("setup published no nibs; the freeze check would be vacuous")
			}

			tt.mutate(t, c, dir)

			assertPublishedPointersFrozenExceptPath(t, c, snaps, tt.name)
		})
	}
}

// frozenPtr pairs a live c.nibs pointer captured before a mutation with a deep
// clone of the nib as it was at capture time (the frozen "before").
type frozenPtr struct {
	id     string
	ptr    *nib.Nib
	before *nib.Nib
}

// snapshotPublishedPointers captures every currently-published c.nibs pointer
// and a deep clone of each, under the read lock so the clones cannot race an
// in-place writer.
func snapshotPublishedPointers(c *Core) []frozenPtr {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make([]frozenPtr, 0, len(c.nibs))
	for id, p := range c.nibs {
		out = append(out, frozenPtr{id: id, ptr: p, before: p.Clone()})
	}
	return out
}

// assertPublishedPointersFrozenExceptPath re-reads each captured pointer and
// fails if any non-Path field diverged from its "before" clone — i.e. if the
// mutator wrote a non-Path field in place on a previously-published pointer.
// Path is neutralized on both sides because it is the one field a mutator may
// change in place (see the canonical invariant at NibReader.GetSnapshot).
func assertPublishedPointersFrozenExceptPath(t *testing.T, c *Core, snaps []frozenPtr, mutator string) {
	t.Helper()

	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, s := range snaps {
		// Compare two Clones so load-boundary-only fields that Clone() normalizes
		// (priorityMigrated, which Clone deliberately clears) are neutralized
		// SYMMETRICALLY — otherwise a future subtest that loads a migrated v0 nib
		// would spuriously differ. s.before is already a capture-time Clone; re-clone
		// the SAME captured pointer now to read its post-mutation state through the
		// same normalization. An in-place non-Path write still surfaces: Clone()
		// copies the pointer's current field values.
		before := *s.before     // deep, independent capture-time clone
		after := *s.ptr.Clone()  // deep clone of the SAME captured pointer, post-mutation
		before.Path = ""
		after.Path = ""
		if !reflect.DeepEqual(before, after) {
			t.Errorf("%s mutated a non-Path field in place on the published pointer for %q\n  before: %+v\n  after:  %+v",
				mutator, s.id, before, after)
		}
	}
}
