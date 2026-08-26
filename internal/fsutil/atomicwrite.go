// Package fsutil holds filesystem primitives shared by layers that cannot import
// each other. It is stdlib-only, like internal/store, so internal/config and
// internal/nibcore can both use it (nibcore imports config, so config must not
// import nibcore).
package fsutil

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// RenameFn is a seam over os.Rename so tests can simulate a crash between the
// temp write and the rename. Production always uses os.Rename.
var RenameFn = os.Rename

// SyncDirFn is the seam a test observes the directory flush through — syncDir
// returns nothing and swallows its errors by contract, so a sync that stopped
// happening would otherwise be invisible. Production always uses syncDir.
//
// Callers outside this package go through SyncDir, not this variable: a seam is
// owned by the package that declares it (as RenameFn here and storeRenameFn in
// cmd are), so the set of places that can silently disable a flush stays inside
// one file.
var SyncDirFn = syncDir

// SyncDir flushes one directory entry, and is the call a batch writer makes for
// each distinct directory after a run of AtomicWriteFileDeferDirSync writes.
// Best-effort, like every directory sync here: see AtomicWriteFile's "does not
// promise" list.
func SyncDir(dir string) {
	SyncDirFn(dir)
}

// AtomicWriteFile writes data to path atomically: it writes to a uniquely-named
// temp file in the same directory, fsyncs it, renames it over path, then fsyncs the
// containing directory. Because the rename is atomic on the same filesystem, a
// concurrent reader observes either the old file or the fully-written new file —
// never a torn/partial write that would fail nib.Parse. A failure before the rename
// leaves any existing file at path untouched, and the temp file is removed on every
// error path.
//
// A unique temp name (not a fixed "<path>.tmp" suffix) is deliberate: two writers
// racing on the same nib — the exact hazard this hardening targets — must not
// collide on a shared temp file. Each renames its own complete temp; the later
// rename wins wholesale rather than interleaving bytes.
//
// WHAT IT DOES NOT PROMISE:
//
//   - Nothing about CONCURRENT writers beyond "no torn file": two writers of
//     different content both succeed and the later rename wins wholesale. It is not
//     a lock (see nibcore's store lock for that).
//   - Durability of the directory entry where the platform will not flush it. The
//     directory fsync after the rename is best-effort: Windows rejects Sync on a
//     directory handle, and the write has already succeeded there, so failing would
//     report an error for a completed operation. On such a platform a crash-recovery
//     path keying on "the file is present" must not treat the rename as durable.
//     AtomicWriteFileDeferDirSync widens that same window deliberately, for a
//     caller writing a batch; it hands the flush obligation back rather than
//     dropping it.
//   - Durability of the directory ENTRY OF A DIRECTORY a caller had to create
//     first. A new directory's own name lives in its PARENT, which nothing here
//     flushes — the sync covers the directory the file was renamed into, not the
//     chain above it. Callers that MkdirAll before writing (nibcore's saveToDisk)
//     inherit that: after a crash the file's contents are durable and its
//     directory may not be, which the bullet above already says about the file
//     itself.
//   - Mode BITS beyond the permission bits. Both callers pass
//     info.Mode().Perm(), so setuid/setgid/sticky never reach Chmod and the new
//     file does not carry them. That is the safe direction, and no config or nib
//     file wants them.
//   - Anything else carried by the OLD file, all of it lost the way every
//     write-temp-and-rename loses it: OWNERSHIP (uid/gid — the temp belongs to the
//     writing process), POSIX ACLs, extended attributes, and any HARD LINK to the
//     old path, which now refers to the replaced content.
//   - Preservation of a SYMLINK at path. The rename replaces it, so the file the
//     link pointed at keeps its old contents — writing through it instead would
//     mean writing wherever the link leads, and a dangling one created the file
//     outside the store, after which callers deleted their source and reported
//     success. Every caller inherits the replacement; config.Save documents and
//     reports what it means for a config.yml.
//   - Any protection against a cross-filesystem rename, which cannot arise: the
//     temp file is always created in filepath.Dir(path), so EXDEV is impossible.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir, err := writeAndRename(path, data, perm)
	if err != nil {
		return err
	}
	SyncDirFn(dir)
	return nil
}

// AtomicWriteFileDeferDirSync is AtomicWriteFile without the trailing directory
// fsync, for a caller writing a BATCH of files: N writes into one directory pay
// N identical directory fsyncs where a single one after the batch is equivalent
// (measured on this project's ext4 volume, 500 same-directory writes: 2.1s
// without the per-write directory fsync against 4.1s with it — the flush is a
// second full journal commit per file, not something amortized behind the
// file's own fsync, and it roughly DOUBLES the cost of an atomic write. Take
// any figure here from a real disk: on tmpfs the same probe reports ~18us per
// write either way, because there the flush is very nearly free).
//
// It returns the directory the file was renamed into, whose entry is NOT yet
// flushed, and the empty string when it returns an error — an error means the
// rename never ran, so there is no new entry to flush.
//
// THE WEAKER GUARANTEE: until the caller passes that directory to SyncDir,
// the file's CONTENTS are durable (the temp is fsynced before the rename) but
// its NAME may not survive a crash, so a recovery path keying on "the file is
// present" cannot rely on it — precisely the state AtomicWriteFile's "does not
// promise" list describes for a platform that refuses a directory sync, except
// here it is the caller who ends it. A batch caller therefore owes SyncDir one
// call per DISTINCT directory it collected, and owes them on the ERROR path too:
// a batch that aborts midway has already committed every rename before the
// failure. Everything else in AtomicWriteFile's contract holds unchanged.
func AtomicWriteFileDeferDirSync(path string, data []byte, perm os.FileMode) (string, error) {
	return writeAndRename(path, data, perm)
}

// AtomicUpdateFileDeferDirSync is AtomicWriteFileDeferDirSync's non-creating
// sibling: it REFUSES, wrapping fs.ErrNotExist, when nothing is at path, and
// creates neither the file nor a temp file on that path.
//
// It exists for a caller that believes it is UPDATING a file it read earlier —
// nibcore's area cascade, which writes each nib back to the path its in-memory
// copy carries. Both writers here end in a rename, and a rename creates
// unconditionally, so a path gone stale (another process renamed every nib file
// under a new prefix) silently yields a SECOND copy of the nib at its old path
// instead of an error. Refusing turns that into a failure the caller reports,
// which is the only answer a stale path has.
//
// WHAT THE REFUSAL IS AND IS NOT: the check and the rename are separate steps,
// so this is not an atomic test-and-set — a writer that removes path in between
// still gets a created file. That window is the caller's to close, and nibcore's
// is: it holds the store lock across the whole verb. What no lock can detect is
// a caller whose OWN path is stale, and that is what this catches.
//
// The check is an Lstat, so a SYMLINK at path counts as present — the rename
// replaces the entry, and the entry is what the check asks about. Everything
// else in AtomicWriteFileDeferDirSync's contract holds unchanged, including the
// caller's obligation to hand the returned directory to SyncDir and the empty
// string returned with any error.
func AtomicUpdateFileDeferDirSync(path string, data []byte, perm os.FileMode) (string, error) {
	// Ahead of the temp file rather than beside the rename: a stale path has
	// often lost its DIRECTORY too, and there os.CreateTemp fails first with an
	// error about a temp file the caller never asked for, burying the one fact
	// it needs — that the file it meant to update is not there.
	if _, err := os.Lstat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("updating %s: %w", path, fs.ErrNotExist)
		}
		return "", fmt.Errorf("checking %s before updating it: %w", path, err)
	}
	return writeAndRename(path, data, perm)
}

// writeAndRename is the shared mechanism behind both writers: temp file, fsync,
// chmod, rename. It returns the directory whose entry the rename created, so
// the caller can decide when — or whether — to flush it.
func writeAndRename(path string, data []byte, perm os.FileMode) (_ string, err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()

	// On any error after creation, drop the temp file so it never leaks and never
	// gets committed to the .nibs/ git repo.
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()

	if _, err = tmp.Write(data); err != nil {
		return "", fmt.Errorf("writing temp file: %w", err)
	}
	// Flush to disk before the rename so a crash cannot leave a renamed-but-empty
	// file (the rename would otherwise be durable while the data is not).
	if err = tmp.Sync(); err != nil {
		return "", fmt.Errorf("syncing temp file: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return "", fmt.Errorf("closing temp file: %w", err)
	}
	// os.CreateTemp makes the file 0600; restore the intended permissions before
	// it becomes the visible file.
	if err = os.Chmod(tmpName, perm); err != nil {
		return "", fmt.Errorf("chmod temp file: %w", err)
	}
	if err = RenameFn(tmpName, path); err != nil {
		return "", fmt.Errorf("renaming temp file over %s: %w", path, err)
	}
	return dir, nil
}

// syncDir flushes the directory entry the rename created, so the file's NAME is as
// durable as its contents. Best-effort by contract: see AtomicWriteFile's
// "does not promise" list for why a failure here is not reported.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}
