package fsutil

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/testskip"
)

func TestAtomicWriteFileCreatesAndReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nib.md")

	if err := AtomicWriteFile(path, []byte("first"), 0o644); err != nil {
		t.Fatalf("first write: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after first write: %v", err)
	}
	if string(got) != "first" {
		t.Fatalf("content after first write = %q, want %q", got, "first")
	}

	if err := AtomicWriteFile(path, []byte("second"), 0o644); err != nil {
		t.Fatalf("second write: %v", err)
	}
	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after second write: %v", err)
	}
	if string(got) != "second" {
		t.Fatalf("content after second write = %q, want %q", got, "second")
	}

	assertNoTempFiles(t, dir)
}

// TestAtomicWriteFileFailedRenameLeavesExistingIntact is the crash-safety guard:
// a failure after the temp file is written but before the rename must leave any
// existing file untouched (no torn/partial content) and clean up the temp file.
// A non-atomic in-place write (os.WriteFile over the target) would fail this test
// twice: the injected rename is never reached (so no error surfaces) and the old
// content is clobbered.
func TestAtomicWriteFileFailedRenameLeavesExistingIntact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nib.md")
	if err := os.WriteFile(path, []byte("OLD"), 0o644); err != nil {
		t.Fatalf("seed old file: %v", err)
	}

	orig := RenameFn
	RenameFn = func(_, _ string) error { return errors.New("simulated crash before rename") }
	defer func() { RenameFn = orig }()

	err := AtomicWriteFile(path, []byte("NEW"), 0o644)
	if err == nil {
		t.Fatal("expected error from injected rename failure, got nil")
	}

	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("old file should still exist: %v", readErr)
	}
	if string(got) != "OLD" {
		t.Fatalf("old file corrupted: got %q, want %q", got, "OLD")
	}
	assertNoTempFiles(t, dir)
}

// TestAtomicWriteFileReplacesASymlinkRatherThanFollowingIt pins the second
// property the CLI's migration engine relies on when it writes a store's
// config.yml: the rename REPLACES whatever sits at path, so a symlink there — a
// dangling one especially — cannot redirect the write outside the directory the
// caller named. os.WriteFile follows the link and creates the file wherever it
// points, which put a project's config outside its store and reported success.
func TestAtomicWriteFileReplacesASymlinkRatherThanFollowingIt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nib.md")
	outside := filepath.Join(t.TempDir(), "escaped.md")
	if err := os.Symlink(outside, path); err != nil {
		testskip.SymlinkUnavailable(t, err)
	}

	if err := AtomicWriteFile(path, []byte("contained"), 0o644); err != nil {
		t.Fatalf("write over a dangling symlink: %v", err)
	}
	if _, err := os.Lstat(outside); !os.IsNotExist(err) {
		t.Errorf("the write followed the symlink out of the directory (stat err = %v)", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat after write: %v", err)
	}
	if !info.Mode().IsRegular() {
		t.Errorf("path is still a %v, want a regular file", info.Mode().Type())
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after write: %v", err)
	}
	if string(got) != "contained" {
		t.Errorf("content = %q, want %q", got, "contained")
	}
	assertNoTempFiles(t, dir)
}

// TestAtomicWriteFileSyncsItsDirectory pins the durability half of the contract
// that the batch variant deliberately drops: the single-write path flushes the
// directory entry the rename created, exactly once, for the directory it wrote
// into. Nothing else can see this — syncDir returns nothing and swallows both
// its errors — so without SyncDirFn a deleted sync passes every other test here.
func TestAtomicWriteFileSyncsItsDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nib.md")
	synced := recordDirSyncs(t)

	if err := AtomicWriteFile(path, []byte("first"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if got := *synced; len(got) != 1 || got[0] != dir {
		t.Errorf("directories synced = %v, want exactly [%s]", got, dir)
	}
}

// TestAtomicWriteFileDeferDirSyncHandsTheFlushBack is the other side of that
// guard: the batch variant writes the file but leaves the directory entry
// unflushed and names the directory, so the caller can pay one fsync per
// distinct directory after its loop instead of one per file.
func TestAtomicWriteFileDeferDirSyncHandsTheFlushBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nib.md")
	synced := recordDirSyncs(t)

	gotDir, err := AtomicWriteFileDeferDirSync(path, []byte("batched"), 0o644)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if gotDir != dir {
		t.Errorf("returned directory = %q, want %q", gotDir, dir)
	}
	if got := *synced; len(got) != 0 {
		t.Errorf("directories synced = %v, want none — the flush is the caller's", got)
	}

	// The file itself is complete and correctly named; only its directory entry
	// is left unflushed.
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after write: %v", err)
	}
	if string(content) != "batched" {
		t.Errorf("content = %q, want %q", content, "batched")
	}
	assertNoTempFiles(t, dir)
}

// TestAtomicWriteFileDeferDirSyncReportsNoDirectoryWhenTheRenameFails covers the
// error path a batch caller feeds straight into its collected set: a failure
// happens before the rename, so there is no new directory entry to flush and the
// caller must not be told to flush one.
func TestAtomicWriteFileDeferDirSyncReportsNoDirectoryWhenTheRenameFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nib.md")

	orig := RenameFn
	RenameFn = func(_, _ string) error { return errors.New("simulated crash before rename") }
	defer func() { RenameFn = orig }()

	gotDir, err := AtomicWriteFileDeferDirSync(path, []byte("never lands"), 0o644)
	if err == nil {
		t.Fatal("expected error from injected rename failure, got nil")
	}
	if gotDir != "" {
		t.Errorf("returned directory = %q, want %q — nothing was renamed", gotDir, "")
	}
}

// recordDirSyncs swaps the directory-sync seam for one that records every
// directory flushed and still performs the flush, and restores it afterwards.
// The returned pointer is read after the code under test has run.
func recordDirSyncs(t *testing.T) *[]string {
	t.Helper()
	var synced []string
	orig := SyncDirFn
	SyncDirFn = func(dir string) {
		synced = append(synced, dir)
		orig(dir)
	}
	t.Cleanup(func() { SyncDirFn = orig })
	return &synced
}

// assertNoTempFiles fails if any file other than the canonical nib.md remains in
// dir — i.e. a temp file leaked.
func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "nib.md" {
			t.Errorf("leftover temp file in dir: %s", e.Name())
		}
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("leftover .tmp file: %s", e.Name())
		}
	}
}

// TestAtomicUpdateFileDeferDirSync pins the non-creating writer's whole
// contract in one table: it replaces what is there, and it REFUSES rather than
// creating anything when the entry it was told to update is gone. The refusal
// is what a caller holding a stale path gets instead of a duplicate file.
func TestAtomicUpdateFileDeferDirSync(t *testing.T) {
	tests := []struct {
		name string
		// seed prepares dir and returns the path to update.
		seed        func(t *testing.T, dir string) string
		wantErr     bool
		wantMissing bool   // the error must be errors.Is(err, fs.ErrNotExist)
		wantNotIn   string // wording the error must NOT carry
		wantContent string
	}{
		{
			name: "replaces an existing file",
			seed: func(t *testing.T, dir string) string {
				path := filepath.Join(dir, "nib.md")
				if err := os.WriteFile(path, []byte("OLD"), 0o644); err != nil {
					t.Fatalf("seed: %v", err)
				}
				return path
			},
			wantContent: "NEW",
		},
		{
			name: "refuses a target that is not there",
			seed: func(t *testing.T, dir string) string {
				return filepath.Join(dir, "nib.md")
			},
			wantErr:     true,
			wantMissing: true,
		},
		{
			name: "refuses a target whose directory is not there",
			seed: func(t *testing.T, dir string) string {
				return filepath.Join(dir, "gone", "nib.md")
			},
			wantErr:     true,
			wantMissing: true,
			// The check runs BEFORE the temp file, so a path whose directory is
			// also gone reports the missing target rather than a temp file the
			// caller never asked for.
			wantNotIn: "creating temp file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := tt.seed(t, dir)

			gotDir, err := AtomicUpdateFileDeferDirSync(path, []byte("NEW"), 0o644)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected a refusal, got nil")
				}
				if tt.wantMissing && !errors.Is(err, fs.ErrNotExist) {
					t.Errorf("error = %v, want it to wrap fs.ErrNotExist", err)
				}
				if tt.wantNotIn != "" && strings.Contains(err.Error(), tt.wantNotIn) {
					t.Errorf("error = %q, want it not to mention %q", err, tt.wantNotIn)
				}
				if gotDir != "" {
					t.Errorf("returned directory = %q, want %q — nothing was renamed, so nothing is owed a flush", gotDir, "")
				}
				if _, statErr := os.Lstat(path); !errors.Is(statErr, fs.ErrNotExist) {
					t.Errorf("the refusal created %s anyway", path)
				}
				assertOnlyEntries(t, dir)
				return
			}
			if err != nil {
				t.Fatalf("AtomicUpdateFileDeferDirSync: %v", err)
			}
			if gotDir != dir {
				t.Errorf("returned directory = %q, want %q", gotDir, dir)
			}
			got, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("read back: %v", readErr)
			}
			if string(got) != tt.wantContent {
				t.Errorf("content = %q, want %q", got, tt.wantContent)
			}
			assertNoTempFiles(t, dir)
		})
	}
}

// TestAtomicWriteFileDeferDirSyncStillCreates is the other half of the pair:
// the creating writer's contract is UNCHANGED by the non-creating sibling, and
// its failure mode on a missing DIRECTORY is the temp-file creation — which is
// why the sibling has to check before it writes rather than branch at the
// rename, and so cannot be this function plus a flag.
func TestAtomicWriteFileDeferDirSyncStillCreates(t *testing.T) {
	dir := t.TempDir()

	path := filepath.Join(dir, "nib.md")
	if _, err := AtomicWriteFileDeferDirSync(path, []byte("NEW"), 0o644); err != nil {
		t.Fatalf("AtomicWriteFileDeferDirSync over a missing file: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "NEW" {
		t.Errorf("content = %q, want %q", got, "NEW")
	}

	_, err = AtomicWriteFileDeferDirSync(filepath.Join(dir, "gone", "nib.md"), []byte("NEW"), 0o644)
	if err == nil {
		t.Fatal("expected an error writing into a directory that is not there")
	}
	if !strings.Contains(err.Error(), "creating temp file") {
		t.Errorf("error = %q, want it to name the temp file it could not create", err)
	}
}

// assertOnlyEntries fails if dir holds anything at all — used after a refusal,
// which must leave neither the target nor a temp file behind.
func assertOnlyEntries(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		t.Errorf("a refused update left %s behind", e.Name())
	}
}
