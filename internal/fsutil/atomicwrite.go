// Package fsutil holds filesystem primitives shared by layers that cannot import
// each other. Keep it stdlib-only: internal/config and internal/nibcore both use
// it, and nibcore imports config.
package fsutil

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// RenameFn is a seam over os.Rename so tests can simulate a crash between the
// temp write and the rename.
var RenameFn = os.Rename

// SyncDirFn is the seam a test observes the directory flush through — syncDir
// returns nothing and swallows its errors, so a sync that stopped happening
// would otherwise be invisible. Call SyncDir instead; this is for tests.
var SyncDirFn = syncDir

// SyncDir flushes one directory entry — the call a batch writer makes for each
// distinct directory after a run of AtomicWriteFileDeferDirSync writes.
// Best-effort, like every directory sync here: see AtomicWriteFile's "does not
// promise" list.
func SyncDir(dir string) {
	SyncDirFn(dir)
}

// AtomicWriteFile writes data to path atomically, through a uniquely-named temp
// file in the same directory. A concurrent reader observes either the old file or
// the fully-written new one — never a torn write that would fail nib.Parse. A
// failure before the rename leaves any existing file at path untouched, and the
// temp file is removed on every error path.
//
// Keep the temp name unique. Two writers racing on the same nib must not share
// one temp file; each renames its own complete temp and the later rename wins
// wholesale.
//
// WHAT IT DOES NOT PROMISE:
//
//   - Anything about CONCURRENT writers beyond "no torn file": two writers of
//     different content both succeed and the later wins wholesale. This is not a
//     lock — see nibcore's store lock.
//   - Durability of the directory entry where the platform will not flush it. The
//     fsync after the rename is best-effort, because Windows refuses Sync on a
//     directory handle and the write has already succeeded there. A crash-recovery
//     path must not key on "the file is present".
//   - Durability of the directory ENTRY OF A DIRECTORY a caller created first. A
//     new directory's name lives in its PARENT, which nothing here flushes.
//   - Mode bits this function was not given. perm reaches Chmod unchanged, so pass
//     info.Mode().Perm() (or a literal without setuid/setgid/sticky) — nothing
//     here strips them for you.
//   - Anything else the OLD file carried, lost the way every write-temp-and-rename
//     loses it: OWNERSHIP (the temp belongs to the writing process), POSIX ACLs,
//     extended attributes, and any HARD LINK to the old path.
//   - Preservation of a SYMLINK at path: the rename replaces it, so the file the
//     link pointed at keeps its old contents. Writing through the link instead
//     would write wherever it leads, including outside the store. config.Save
//     documents and reports what the replacement means for a config.yml.
//   - Any protection against a cross-filesystem rename, which cannot arise: the
//     temp is always created in filepath.Dir(path), so EXDEV is impossible.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir, err := writeAndRename(path, data, perm)
	if err != nil {
		return err
	}
	SyncDirFn(dir)
	return nil
}

// AtomicWriteFileDeferDirSync is AtomicWriteFile without the trailing directory
// fsync, for a caller writing a BATCH into one directory. The per-write flush is
// a second full journal commit rather than something amortized behind the file's
// own fsync, so it roughly doubles the cost of an atomic write on a real disk.
//
// It returns the directory the file was renamed into, whose entry is NOT yet
// flushed, and the empty string with any error — an error means the rename never
// ran, so there is no new entry to flush.
//
// CANONICAL INVARIANT (the deferred directory-sync debt): internal/nibcore and
// internal/reprefix defer here; do not re-derive it.
//
// THE WEAKER GUARANTEE: until that directory reaches SyncDir, the file's CONTENTS
// are durable (the temp is fsynced before the rename) but its NAME may not survive
// a crash. Pass SyncDir one call per DISTINCT directory collected, on the ERROR
// path too — a batch that aborts midway has already committed every rename before
// the failure. Everything else in AtomicWriteFile's contract holds unchanged.
func AtomicWriteFileDeferDirSync(path string, data []byte, perm os.FileMode) (string, error) {
	return writeAndRename(path, data, perm)
}

// AtomicUpdateFileDeferDirSync is AtomicWriteFileDeferDirSync's non-creating
// sibling: it REFUSES, wrapping fs.ErrNotExist, when nothing is at path, and
// creates neither the file nor a temp file on that path.
//
// Use it when you believe you are UPDATING a file read earlier, from a path your
// in-memory copy carries. Every writer here ends in a rename and a rename creates
// unconditionally, so a path gone stale — another process renamed every nib file
// under a new prefix — otherwise yields a SECOND copy at the old path instead of
// an error.
//
// WHAT THE REFUSAL IS AND IS NOT: the check and the rename are separate steps, so
// this is not an atomic test-and-set — a writer that removes path in between still
// gets a created file. Close that window with the store lock, as nibcore's callers
// do. What no lock detects is a caller whose OWN path is stale, which is what this
// catches.
//
// The check is an Lstat, so a SYMLINK at path counts as present: the rename
// replaces the entry, and the entry is what the check asks about. Everything else
// in AtomicWriteFileDeferDirSync's contract holds unchanged, the returned
// directory's flush included.
func AtomicUpdateFileDeferDirSync(path string, data []byte, perm os.FileMode) (string, error) {
	// Ahead of the temp file: a stale path has often lost its DIRECTORY too, and
	// os.CreateTemp would fail first, burying the one fact the caller needs behind
	// an error about a temp file it never asked for.
	if _, err := os.Lstat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("updating %s: %w", path, fs.ErrNotExist)
		}
		return "", fmt.Errorf("checking %s before updating it: %w", path, err)
	}
	return writeAndRename(path, data, perm)
}

// writeAndRename is the mechanism behind all three writers: temp file, fsync,
// chmod, rename. It returns the directory whose entry the rename created, so the
// caller can decide when — or whether — to flush it.
func writeAndRename(path string, data []byte, perm os.FileMode) (_ string, err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()

	// Drop the temp file on any error after creation, so it never leaks into the
	// .nibs/ git repo.
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()

	if _, err = tmp.Write(data); err != nil {
		return "", fmt.Errorf("writing temp file: %w", err)
	}
	// Flush before the rename, or a crash leaves a durable name over data that is
	// not.
	if err = tmp.Sync(); err != nil {
		return "", fmt.Errorf("syncing temp file: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return "", fmt.Errorf("closing temp file: %w", err)
	}
	// os.CreateTemp makes the file 0600; set the intended mode before the rename
	// makes it visible.
	if err = os.Chmod(tmpName, perm); err != nil {
		return "", fmt.Errorf("chmod temp file: %w", err)
	}
	if err = RenameFn(tmpName, path); err != nil {
		return "", fmt.Errorf("renaming temp file over %s: %w", path, err)
	}
	return dir, nil
}

// syncDir flushes the directory entry the rename created, making the file's NAME
// as durable as its contents. Best-effort: see AtomicWriteFile's "does not
// promise" list for why a failure here goes unreported.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}
