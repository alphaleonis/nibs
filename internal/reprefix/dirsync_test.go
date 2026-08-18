package reprefix

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/fsutil"
)

// planFiles is the number of nibs in multiDirPlan's fixture, deliberately
// larger than the number of directories they occupy: two of them share data/,
// so a run that still synced per FILE would flush data/ twice and fail the
// "exactly once" guard.
const planFiles = 4

// multiDirPlan lays out a store whose nibs span THREE distinct directories:
// data/ (two of them), a subdirectory of data/, and archive/. That spread is
// what makes "one fsync of the data directory after the loop" wrong — a nib's
// Path carries the content directory it lives in, archived nibs stay in the
// store and are still reprefixed, and data/ tolerates subdirectories that
// rewritePath preserves verbatim.
//
// skip names a relative path to leave off disk, so a caller can make the rename
// pass fail on a chosen file. The plan lists the files in the order below, and
// BuildPlan preserves that order, so the failing file is always the last one.
//
// It returns the store root, the plan, and the three absolute directories a
// full run must flush.
func multiDirPlan(t *testing.T, skip string) (string, *RenamePlan, []string) {
	t.Helper()
	root := t.TempDir()

	paths := []string{
		"data/tnib-aaa--one.md",
		"data/tnib-aab--two.md",
		"data/sub/tnib-bbb--nested.md",
		"archive/tnib-ccc--archived.md",
	}
	ids := []string{"tnib-aaa", "tnib-aab", "tnib-bbb", "tnib-ccc"}

	snapshot := make([]NibSnapshot, 0, len(paths))
	for i, p := range paths {
		if p != skip {
			newTestNib(t, root, p, ids[i], "", nil, "Body.")
		}
		snapshot = append(snapshot, NibSnapshot{ID: ids[i], Path: p})
	}
	if len(snapshot) != planFiles {
		t.Fatalf("fixture has %d nibs, want %d", len(snapshot), planFiles)
	}

	plan, err := BuildPlan(snapshot, "tnib-", "new-", stubExists)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	dirs := []string{
		filepath.Join(root, "archive"),
		filepath.Join(root, "data"),
		filepath.Join(root, "data", "sub"),
	}
	return root, plan, dirs
}

// recordDirSyncs swaps the directory-sync seam for one that records every
// directory flushed and still performs the flush, and restores it afterwards.
// Nothing else can observe a directory sync — it returns nothing and swallows
// its errors by contract, so without the seam a flush that stopped happening
// would pass every other test in this package.
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

// assertSynced compares the recorded flushes against the directories expected,
// each exactly once, order-insensitively.
func assertSynced(t *testing.T, got []string, want []string) {
	t.Helper()
	gotSorted := append([]string(nil), got...)
	sort.Strings(gotSorted)
	wantSorted := append([]string(nil), want...)
	sort.Strings(wantSorted)
	if len(gotSorted) != len(wantSorted) {
		t.Fatalf("directories synced = %v (%d calls), want each of %v exactly once",
			gotSorted, len(gotSorted), wantSorted)
	}
	for i := range wantSorted {
		if gotSorted[i] != wantSorted[i] {
			t.Fatalf("directories synced = %v, want %v", gotSorted, wantSorted)
		}
	}
}

// assertNoTempFiles walks the store and fails on any leftover temp file. The
// atomic writer removes its temp on every error path, so one surviving a run is
// a leak that would also get committed to the .nibs/ git repo.
func assertNoTempFiles(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.Contains(d.Name(), ".tmp") {
			t.Errorf("leftover temp file: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
}

// TestExecuteSyncsEachDistinctDirectoryExactlyOnce is the guard on the
// batching: a run rewrites four nibs spread over three directories and must
// flush each of those directories once — not once per nib (the cost the batch
// replaced, which the two nibs sharing data/ would expose), and not just the
// data directory (which would silently drop the durability of the archived and
// nested writes).
func TestExecuteSyncsEachDistinctDirectoryExactlyOnce(t *testing.T) {
	root, plan, wantDirs := multiDirPlan(t, "")
	synced := recordDirSyncs(t)

	if err := Execute(plan, root); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	assertSynced(t, *synced, wantDirs)
	assertNoTempFiles(t, root)
}

// TestExecuteFlushesDirectoriesWhenTheRenamePassAbortsEarly covers the error
// path the deferred flush exists for. The rename pass returns on its first
// failure with the earlier files already renamed into place, and the rewrite
// pass never runs, so nothing else would ever flush those entries. A flush
// placed after the passes rather than deferred would be skipped exactly when a
// partial run is on disk.
//
// The failing file's own directory must NOT be flushed — nothing was renamed
// into it — which is the other half of what the seam observes here.
func TestExecuteFlushesDirectoriesWhenTheRenamePassAbortsEarly(t *testing.T) {
	missing := "archive/tnib-ccc--archived.md"
	root, plan, dirs := multiDirPlan(t, missing)
	archiveDir, dataDir, subDir := dirs[0], dirs[1], dirs[2]
	synced := recordDirSyncs(t)

	if err := Execute(plan, root); err == nil {
		t.Fatalf("expected Execute to fail on the missing source %s, got nil", missing)
	}

	assertSynced(t, *synced, []string{dataDir, subDir})
	for _, dir := range *synced {
		if dir == archiveDir {
			t.Errorf("flushed %s, but no file was ever renamed into it", archiveDir)
		}
	}
}

// TestExecuteFlushesDirectoriesWhenTheRewritePassAbortsEarly covers the same
// error path one pass later, and pins that the rewrite reaches disk through
// fsutil: the failure is injected through fsutil.RenameFn, which a hand-rolled
// os.Rename would ignore, so this test also fails if the writer regresses to
// its own temp-and-rename. Every rename landed before the rewrite pass aborted,
// so all three directories are still owed a flush.
func TestExecuteFlushesDirectoriesWhenTheRewritePassAbortsEarly(t *testing.T) {
	root, plan, wantDirs := multiDirPlan(t, "")

	// Fail the last rewrite so three writes land first, in two directories.
	orig := fsutil.RenameFn
	writes := 0
	fsutil.RenameFn = func(oldpath, newpath string) error {
		writes++
		if writes == planFiles {
			return errors.New("injected crash")
		}
		return orig(oldpath, newpath)
	}
	t.Cleanup(func() { fsutil.RenameFn = orig })

	synced := recordDirSyncs(t)

	if err := Execute(plan, root); err == nil {
		t.Fatal("expected the injected rename failure to abort the rewrite pass, got nil")
	}
	if writes != planFiles {
		t.Fatalf("rewrite pass performed %d renames through fsutil, want %d — the writer is not going through fsutil", writes, planFiles)
	}

	assertSynced(t, *synced, wantDirs)
	assertNoTempFiles(t, root)
}
