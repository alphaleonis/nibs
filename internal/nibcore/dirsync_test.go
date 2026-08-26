package nibcore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/fsutil"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/store"
)

// sweptNibs is the number of nibs in multiDirStore's fixture that every bulk
// caller rewrites, and it is deliberately larger than the number of directories
// they occupy: two of them share data/, so a sweep that still synced per WRITE
// would flush data/ twice and fail the "exactly once" guard.
const sweptNibs = 4

// multiDirStore builds a store whose swept nibs span THREE distinct
// directories: data/ (two of them), a subdirectory of data/, and archive/. That
// spread is what makes "one fsync of the data directory after the loop" wrong —
// archived nibs stay in the store and are still swept, and a nib's Path
// preserves any data/ subdirectory it sits in.
//
// Every swept nib carries a broken parent (which FixBrokenLinks removes), a
// blocked_by pointing at a nib that exists (which RemoveLinksTo removes) and a
// legacy `priority: deferred` (which NormalizeLegacyPriorities rewrites), so a
// single fixture drives all three bulk callers. Ids sort aaa1 < aab1 < bbb2 <
// ccc3, which is the order persistClonesLocked writes them in.
//
// It returns the core, the store directory, and the three absolute directories
// a full sweep must flush.
//
// It deliberately does NOT take the store lock: only the migration entry point
// wants one (it takes proof-of-lock as a parameter), while the two link sweeps
// acquire the same flock themselves — and the flock is per descriptor, so a lock
// held here would deadlock them. storeLockFor is what the migration case uses.
func multiDirStore(t *testing.T) (*Core, string, []string) {
	t.Helper()
	nibsDir := filepath.Join(t.TempDir(), store.DirName)
	layout := store.NewLayout(nibsDir)

	subDir := filepath.Join(layout.DataDir(), "sub")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("create data subdirectory: %v", err)
	}

	sweepable := func(title string) string {
		return fmt.Sprintf("---\nversion: 1\ntitle: %s\nstatus: todo\npriority: deferred\nparent: gone9\nblocked_by:\n    - zzz9\n---\n\nBody.\n", title)
	}
	files := map[string]string{
		filepath.Join(storeData(t, nibsDir), "aaa1--root.md"):    sweepable("Root"),
		filepath.Join(storeData(t, nibsDir), "aab1--sibling.md"): sweepable("Sibling"),
		filepath.Join(subDir, "bbb2--nested.md"):                 sweepable("Nested"),
		filepath.Join(storeArchive(t, nibsDir), "ccc3--gone.md"): sweepable("Archived"),
		filepath.Join(storeData(t, nibsDir), "zzz9--blocker.md"): "---\nversion: 1\ntitle: Blocker\nstatus: todo\n---\n\nBody.\n",
	}
	for path, content := range files {
		writeNibFileAtomic(t, path, content)
	}

	core := New(nibsDir, config.Default())
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("Load(): %v", err)
	}
	return core, nibsDir, []string{layout.ArchiveDir(), layout.DataDir(), subDir}
}

// storeLockFor takes the store lock the migration mutators require as proof,
// released at the end of the test that asked for it.
func storeLockFor(t *testing.T, nibsDir string) *StoreLock {
	t.Helper()
	lock, err := AcquireStoreLock(nibsDir)
	if err != nil {
		t.Fatalf("AcquireStoreLock: %v", err)
	}
	t.Cleanup(func() { _ = lock.Release() })
	return lock
}

// recordDirSyncs swaps the directory-sync seam for one that records every
// directory flushed and still performs the flush, and restores it afterwards.
// Nothing else can observe a directory sync — it returns nothing and swallows
// its errors by contract.
func recordDirSyncs(t *testing.T) *[]string {
	t.Helper()
	var synced []string
	orig := fsutil.SyncDirFn
	fsutil.SyncDirFn = func(dir string) {
		synced = append(synced, dir)
		orig(dir)
	}
	t.Cleanup(func() { fsutil.SyncDirFn = orig })
	return &synced
}

// TestBulkWritesSyncEachDirectoryExactlyOnce is the guard on the batching: each
// bulk caller rewrites four nibs spread over three directories and must flush
// each of those directories once — not once per nib (the cost this replaced,
// which the two nibs sharing data/ would expose), and not just the data
// directory (which would silently drop the durability of the archived and
// nested writes).
func TestBulkWritesSyncEachDirectoryExactlyOnce(t *testing.T) {
	cases := []struct {
		name string
		run  func(t *testing.T, core *Core, nibsDir string)
	}{
		{
			name: "NormalizeLegacyPriorities",
			run: func(t *testing.T, core *Core, nibsDir string) {
				n, err := core.NormalizeLegacyPriorities(storeLockFor(t, nibsDir))
				if err != nil {
					t.Fatalf("NormalizeLegacyPriorities: %v", err)
				}
				if n != sweptNibs {
					t.Fatalf("normalized %d nibs, want %d — the fixture no longer drives the bulk loop", n, sweptNibs)
				}
			},
		},
		{
			name: "RemoveLinksTo",
			run: func(t *testing.T, core *Core, _ string) {
				n, err := core.RemoveLinksTo("zzz9")
				if err != nil {
					t.Fatalf("RemoveLinksTo: %v", err)
				}
				if n != sweptNibs {
					t.Fatalf("removed %d links, want %d — the fixture no longer drives the bulk loop", n, sweptNibs)
				}
			},
		},
		{
			name: "FixBrokenLinks",
			run: func(t *testing.T, core *Core, _ string) {
				n, err := core.FixBrokenLinks()
				if err != nil {
					t.Fatalf("FixBrokenLinks: %v", err)
				}
				if n != sweptNibs {
					t.Fatalf("fixed %d links, want %d — the fixture no longer drives the bulk loop", n, sweptNibs)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			core, nibsDir, wantDirs := multiDirStore(t)
			synced := recordDirSyncs(t)

			tc.run(t, core, nibsDir)

			got := append([]string(nil), *synced...)
			sort.Strings(got)
			if len(got) != len(wantDirs) {
				t.Fatalf("directories synced = %v (%d calls), want each of %v exactly once",
					got, len(got), wantDirs)
			}
			for i, want := range wantDirs {
				if got[i] != want {
					t.Errorf("directories synced = %v, want %v", got, wantDirs)
					break
				}
			}
		})
	}
}

// TestBulkWriteFlushesDirectoriesWhenTheLoopAbortsEarly covers the error path
// the deferred flush exists for: the first write failure aborts the batch with
// the earlier files already renamed into place, so their directory entries still
// need flushing. A flush placed after the loop rather than deferred would be
// skipped exactly when a partial batch is on disk.
//
// The failing write's own directory must NOT be flushed — nothing was renamed
// into it — which is the other half of what the seam observes here.
func TestBulkWriteFlushesDirectoriesWhenTheLoopAbortsEarly(t *testing.T) {
	core, nibsDir, dirs := multiDirStore(t)
	archiveDir, dataDir, subDir := dirs[0], dirs[1], dirs[2]

	// persistClonesLocked writes in sorted id order — aaa1 and aab1 (data/),
	// bbb2 (data/sub/), ccc3 (archive/) — so failing the fourth rename lands
	// three files in two directories and none in the archive.
	orig := fsutil.RenameFn
	writes := 0
	fsutil.RenameFn = func(oldpath, newpath string) error {
		writes++
		if writes == sweptNibs {
			return errors.New("injected crash")
		}
		return orig(oldpath, newpath)
	}
	t.Cleanup(func() { fsutil.RenameFn = orig })

	synced := recordDirSyncs(t)

	if _, err := core.NormalizeLegacyPriorities(storeLockFor(t, nibsDir)); err == nil {
		t.Fatal("expected the injected rename failure to abort the batch, got nil")
	}

	got := append([]string(nil), *synced...)
	sort.Strings(got)
	want := []string{dataDir, subDir}
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("directories synced = %v, want the directories of the writes that landed, %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("directories synced = %v, want %v", got, want)
		}
	}
	for _, dir := range got {
		if dir == archiveDir {
			t.Errorf("flushed %s, but its write never reached its rename", archiveDir)
		}
	}
}

// TestSingleWriteSyncsItsDirectory pins the acceptance criterion the batching
// must not cost: a single write still flushes its directory before it returns.
// Both single-write entry points reach the disk through a deferred-sync writer,
// so the flush each owes is its own — no test below it can catch its loss.
func TestSingleWriteSyncsItsDirectory(t *testing.T) {
	core, nibsDir := setupTestCore(t)
	dataDir := store.NewLayout(nibsDir).DataDir()

	t.Run("Create", func(t *testing.T) {
		synced := recordDirSyncs(t)
		b := &nib.Nib{ID: "sync-create-1", Title: "Created", Status: "todo", Version: 1}
		if err := core.Create(b); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if got := *synced; len(got) != 1 || got[0] != dataDir {
			t.Errorf("directories synced = %v, want exactly [%s]", got, dataDir)
		}
	})

	t.Run("Update", func(t *testing.T) {
		b, err := core.Get("sync-create-1")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		updated := b.Clone()
		updated.Title = "Updated"

		synced := recordDirSyncs(t)
		if err := core.Update(updated, nil); err != nil {
			t.Fatalf("Update: %v", err)
		}
		if got := *synced; len(got) != 1 || got[0] != dataDir {
			t.Errorf("directories synced = %v, want exactly [%s]", got, dataDir)
		}
	})
}
