package nibcore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

	orig := renameFn
	renameFn = func(_, _ string) error { return errors.New("simulated crash before rename") }
	defer func() { renameFn = orig }()

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
		t.Skipf("symlinks unavailable: %v", err)
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
