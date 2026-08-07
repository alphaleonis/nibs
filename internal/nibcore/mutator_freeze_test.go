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
// (Archive/Unarchive/LoadAndUnarchive and the watcher's archive/unarchive move
// branches rewrite it under c.mu; the watcher's slug-rename branch is instead
// copy-on-write), so it is excluded from the comparison.
//
// The "before" snapshot is a DEEP clone (not a shallow *p copy) on purpose: a
// shallow copy would share slice/map backing storage with the live pointer, so an
// in-place element write (e.g. p.BlockedBy[0] = x) would mutate both sides and
// slip past DeepEqual. The deep clone makes that class visible for the top-level
// slices Clone() copies (Tags, BlockedBy, Blocking, Documents); Extra's nested
// yaml.Node.Content is shared by Clone(), so an in-place edit there is not
// covered (no current mutator touches it).
func TestCoreMutators_FreezeGuard(t *testing.T) {
	for _, tt := range freezeGuardCases() {
		t.Run(tt.name, func(t *testing.T) {
			newCore := tt.newCore
			if newCore == nil {
				newCore = setupTestCore
			}
			c, dir := newCore(t)
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

// freezeGuardCase is one row of the TestCoreMutators_FreezeGuard table. It is a
// named (rather than anonymous) struct so TestCoreMutators_FreezePartition can
// read the table's covers metadata and prove that every registered mutator is
// exercised by a subtest.
type freezeGuardCase struct {
	name string
	// newCore builds the Core under guard. Defaults to setupTestCore; a case
	// needs its own only when the default's configuration cannot express the
	// state it guards (short-form link ids require a configured id prefix).
	newCore func(t *testing.T) (*Core, string)
	// setup arranges already-published nibs and any pre-state (e.g. archiving a
	// nib, enabling the watcher). It runs BEFORE the pointers are snapshotted.
	setup func(t *testing.T, c *Core, dir string)
	// mutate runs the mutator under guard, AFTER the pointers are snapshotted.
	mutate func(t *testing.T, c *Core, dir string)
	// covers names the EXPORTED *Core mutator(s) this subtest exercises. It links
	// the table to the freezeMutators registry in TestCoreMutators_FreezePartition,
	// which asserts every registered mutator has at least one covering subtest.
	// Watcher cases drive the UNEXPORTED handleChanges and therefore name no
	// exported mutator (covers stays nil) — see the scope note on that test.
	covers []string
}

// freezeGuardCases is the shared table backing both TestCoreMutators_FreezeGuard
// (which runs each case) and TestCoreMutators_FreezePartition (which reads each
// case's covers to prove mutator coverage). It is a function, not a package var,
// so the closures capture nothing at package-init time.
func freezeGuardCases() []freezeGuardCase {
	return []freezeGuardCase{
		{
			name:   "Create", // No pre-published target of its own; asserts Create
			covers: []string{"Create"},
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
			name:   "Update",
			covers: []string{"Update"},
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
			name:   "reorder-store-write-via-Update", // Approximates the reorder path.
			covers: []string{"Update"},
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
			name:   "Delete",
			covers: []string{"Delete"},
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
			name:   "Create-resolves-a-dangling-link", // Re-points a link on a BYSTANDER nib: copy-on-write.
			covers: []string{"Create"},
			// Creating the nib that a dangling short-form link names re-resolves that
			// link on a nib published long before. Parent is a non-Path field, so the
			// rewrite must install a fresh pointer rather than edit the one an off-lock
			// reader may still hold.
			//
			// The prefixed core is load-bearing, not incidental: canonicalizeLinksInMap
			// and canonicalizeStoreLocked both early-return when no id prefix is
			// configured, so under the default core NONE of the code this case exists to
			// guard would run and the case would pass vacuously.
			newCore: mustLoadPrefixedCore,
			setup: func(t *testing.T, c *Core, dir string) {
				writeLinkNibFile(t, dir, "nibs-t1", "todo", "parent: zz9\n")
				if err := c.Load(); err != nil {
					t.Fatalf("Load: %v", err)
				}
				if b, _ := c.Get("nibs-t1"); b == nil || b.Parent != "zz9" {
					t.Fatalf("setup premise failed: parent = %+v, want the verbatim %q", b, "zz9")
				}
			},
			mutate: func(t *testing.T, c *Core, _ string) {
				if err := c.Create(&nib.Nib{ID: "nibs-zz9", Version: 1, Title: "Arrived", Status: "todo", Type: "task"}); err != nil {
					t.Fatalf("Create: %v", err)
				}
				if b, _ := c.Get("nibs-t1"); b == nil || b.Parent != "nibs-zz9" {
					t.Fatalf("mutation did not fire: parent = %+v, want nibs-zz9", b)
				}
			},
		},
		{
			name:   "Delete-unmasks-prefixed-twin", // Re-points a link on a BYSTANDER nib: copy-on-write.
			covers: []string{"Delete"},
			// Deleting a bare-token nib whose prefixed twin is still stored re-resolves
			// the links that named the bare spelling, and the nib holding such a link
			// has been published for a while — Parent is a non-Path field, so the
			// rewrite must install a fresh pointer rather than edit the one an off-lock
			// reader may still hold. The default core configures no id prefix, so the
			// bare/prefixed pair cannot exist there.
			newCore: mustLoadPrefixedCore,
			setup: func(t *testing.T, c *Core, dir string) {
				writeLinkNibFile(t, dir, "e1", "todo", "")
				writeLinkNibFile(t, dir, "nibs-e1", "todo", "")
				writeLinkNibFile(t, dir, "nibs-t1", "todo", "parent: e1\n")
				if err := c.Load(); err != nil {
					t.Fatalf("Load: %v", err)
				}
				if b, _ := c.Get("nibs-t1"); b == nil || b.Parent != "e1" {
					t.Fatalf("setup premise failed: parent = %+v, want the bare spelling %q", b, "e1")
				}
			},
			mutate: func(t *testing.T, c *Core, _ string) {
				if err := c.Delete("e1"); err != nil {
					t.Fatalf("Delete: %v", err)
				}
				if b, _ := c.Get("nibs-t1"); b == nil || b.Parent != "nibs-e1" {
					t.Fatalf("mutation did not fire: parent = %+v, want nibs-e1", b)
				}
			},
		},
		{
			name:   "Archive", // Rewrites Path in place — the sanctioned exception.
			covers: []string{"Archive"},
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
			name:   "Unarchive", // Rewrites Path in place — the sanctioned exception.
			covers: []string{"Unarchive"},
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
			name:   "LoadAndUnarchive", // Rewrites Path in place — the sanctioned exception.
			covers: []string{"LoadAndUnarchive"},
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
			name:   "RemoveLinksTo",
			covers: []string{"RemoveLinksTo"},
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
			name:   "FixBrokenLinks",
			covers: []string{"FixBrokenLinks"},
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
			// covers is nil: the watcher path drives the UNEXPORTED handleChanges,
			// not an exported mutator, so it contributes nothing to the exported-
			// mutator coverage set (that is by design — see the scope note).
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
			name: "watcher-slug-rename", // Same-id rename: copy-on-write (new Path + re-derived Slug).
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
		{
			name: "watcher-canonicalize-late-target", // Resolves a link on a BYSTANDER nib: copy-on-write.
			// The batch's own file is not the one that changes here. A nib loaded
			// with an unresolvable short-form parent is rewritten when the nib it
			// names finally arrives, and that nib has been published for a while —
			// Parent is a non-Path field, so the rewrite must install a fresh
			// pointer rather than edit the one an off-lock reader may still hold.
			newCore: mustLoadPrefixedCore,
			setup: func(t *testing.T, c *Core, dir string) {
				writeLinkNibFile(t, dir, "nibs-wlate", "todo", "parent: wtarget\n")
				if err := c.Load(); err != nil {
					t.Fatalf("Load: %v", err)
				}
				if b, _ := c.Get("nibs-wlate"); b == nil || b.Parent != "wtarget" {
					t.Fatalf("setup premise failed: parent = %+v, want it unresolved", b)
				}
				setWatching(c)
			},
			mutate: func(t *testing.T, c *Core, dir string) {
				path := writeLinkNibFile(t, dir, "nibs-wtarget", "todo", "")
				c.handleChanges(map[string]fsnotify.Op{path: fsnotify.Create})
				if b, _ := c.Get("nibs-wlate"); b == nil || b.Parent != "nibs-wtarget" {
					t.Fatalf("mutation did not fire: parent = %+v, want nibs-wtarget", b)
				}
			},
		},
	}
}

// TestCoreMutators_FreezePartition is the COMPLETENESS backstop for
// TestCoreMutators_FreezeGuard. That guard drives a HARDCODED table of mutators;
// nothing forces a NEWLY-ADDED exported *Core mutator into it, so a new writer
// could silently reintroduce the very copy-on-write race the freeze guard exists
// to catch. This test closes that gap deterministically: it reflects over
// *Core's exported method set and requires every method to be classified into
// exactly one of two explicit registries, and every mutator registry entry to be
// exercised by a freeze-guard subtest. A newly-added exported method is in
// NEITHER registry, so it fails here until someone classifies it (and, if it is a
// mutator, wires up a covering subtest).
//
// SCOPE / LIMITATIONS. This guard enforces one thing well — that no new EXPORTED
// *Core method ships without being classified, and no registered mutator ships
// without a covering freeze subtest. It deliberately proves no more than that;
// three gaps it does NOT close:
//   - Exported only. Reflection enumerates only EXPORTED methods, so a new
//     UNEXPORTED mutator (e.g. a new branch of handleChanges) is not auto-enforced
//     and must still be covered by hand with a watcher-* subtest in
//     TestCoreMutators_FreezeGuard.
//   - Completeness, not correctness. The partition proves every method is
//     CLASSIFIED, not that it is classified CORRECTLY. A mutator misfiled into
//     freezeNonMutators, or a new in-place non-Path write added inside an
//     already-classified method, is invisible here — only the freeze guard's own
//     subtests catch an actual in-place mutation.
//   - covers is self-declared. The coverage check trusts each subtest's `covers`
//     label; it does not witness that the subtest's mutate closure actually calls
//     the named mutator. A label pinned to a non-exercising case would still pass.
//     The mistake it DOES catch is forgetting to add covers for a registered mutator.
func TestCoreMutators_FreezePartition(t *testing.T) {
	// freezeMutators: exported *Core methods that can mutate an already-published
	// c.nibs pointer and therefore MUST have freeze-guard coverage. Verified
	// against the nibcore sources: Create/Update/Delete/Archive/Unarchive/
	// LoadAndUnarchive write the store (core.go); RemoveLinksTo/FixBrokenLinks
	// rewrite linking nibs copy-on-write (link_health.go).
	freezeMutators := map[string]bool{
		"Create":           true,
		"Update":           true,
		"Delete":           true,
		"Archive":          true,
		"Unarchive":        true,
		"LoadAndUnarchive": true,
		"RemoveLinksTo":    true,
		"FixBrokenLinks":   true,
	}

	// freezeNonMutators: every OTHER exported *Core method (readers, lifecycle,
	// helpers). None installs or rewrites a published c.nibs pointer in a way that
	// can tear a non-Path field for an off-lock reader: readers return live
	// pointers or GetSnapshot clones; Load swaps the whole c.nibs map with fresh
	// pointers (leaving any held pointer frozen); GetForUpdate returns a clone.
	freezeNonMutators := map[string]bool{
		"All":                true,
		"CheckAllLinks":      true,
		"Close":              true,
		"Config":             true,
		"CurrentETag":        true,
		"DetectCycle":        true,
		"FindActiveBlockers": true,
		"FindIncomingLinks":  true,
		"FindMentionedBy":    true,
		"FindMentions":       true,
		"FullPath":           true,
		"Get":                true,
		"GetForUpdate":       true,
		"GetFromArchive":     true,
		"GetSnapshot":        true,
		"Init":               true,
		"IsArchived":         true,
		"IsBlocked":          true,
		"IsBlocking":         true,
		"Load":               true,
		"NormalizeID":        true,
		"Root":               true,
		"Search":             true,
		"SetSearchIndex":     true,
		"SetWarnWriter":      true,
		"StartWatching":      true,
		"StopWatching":       true,
		"Subscribe":          true,
		"SubscribeSignal":    true,
		"ValidateEnums":      true,
		"ValidateParent":     true,
	}

	// Reflect the exported method set of *Core.
	rt := reflect.TypeOf((*Core)(nil))
	reflected := make(map[string]bool, rt.NumMethod())
	for i := 0; i < rt.NumMethod(); i++ {
		reflected[rt.Method(i).Name] = true
	}

	// 1. Partition completeness: every reflected exported method is in EXACTLY
	// ONE registry. A method in neither is the new-mutator failure mode this
	// whole test exists to catch; a method in both is a classification mistake.
	for name := range reflected {
		inMut := freezeMutators[name]
		inNon := freezeNonMutators[name]
		switch {
		case inMut && inNon:
			t.Errorf("Core method %q is in BOTH freezeMutators and freezeNonMutators — classify it into exactly one", name)
		case !inMut && !inNon:
			t.Errorf("Core method %q is unclassified — add it to freezeMutators (and a freeze subtest) or freezeNonMutators", name)
		}
	}

	// 2. No stale registry entries: a name in either registry that is no longer a
	// real exported method (renamed/removed) must be flagged, otherwise the
	// partition check above could be satisfied by a phantom.
	for name := range freezeMutators {
		if !reflected[name] {
			t.Errorf("freezeMutators names %q, which is not an exported *Core method — remove or rename it", name)
		}
	}
	for name := range freezeNonMutators {
		if !reflected[name] {
			t.Errorf("freezeNonMutators names %q, which is not an exported *Core method — remove or rename it", name)
		}
	}

	// 3. Mutator coverage: every freezeMutators entry is exercised by at least one
	// TestCoreMutators_FreezeGuard subtest, established via that subtest's covers
	// field. This is what makes ADDING a mutator to the registry insufficient on
	// its own — it must also gain a covering subtest.
	covered := make(map[string]bool)
	for _, tc := range freezeGuardCases() {
		for _, m := range tc.covers {
			if !reflected[m] {
				t.Errorf("freeze subtest %q lists covers %q, which is not an exported *Core method", tc.name, m)
			}
			if freezeMutators[m] {
				covered[m] = true
			}
		}
	}
	for name := range freezeMutators {
		if !covered[name] {
			t.Errorf("freezeMutators names %q, but no TestCoreMutators_FreezeGuard subtest covers it — add covers:[]string{%q} to the subtest that exercises it", name, name)
		}
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
		after := *s.ptr.Clone() // deep clone of the SAME captured pointer, post-mutation
		before.Path = ""
		after.Path = ""
		if !reflect.DeepEqual(before, after) {
			t.Errorf("%s mutated a non-Path field in place on the published pointer for %q\n  before: %+v\n  after:  %+v",
				mutator, s.id, before, after)
		}
	}
}
