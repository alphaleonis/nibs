// Package fsutil holds filesystem primitives shared by layers that cannot import
// each other. It is stdlib-only, like internal/store, so internal/config and
// internal/nibcore can both use it (nibcore imports config, so config must not
// import nibcore).
package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
)

// RenameFn is a seam over os.Rename so tests can simulate a crash between the
// temp write and the rename. Production always uses os.Rename.
var RenameFn = os.Rename

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
func AtomicWriteFile(path string, data []byte, perm os.FileMode) (err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
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
		return fmt.Errorf("writing temp file: %w", err)
	}
	// Flush to disk before the rename so a crash cannot leave a renamed-but-empty
	// file (the rename would otherwise be durable while the data is not).
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("syncing temp file: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}
	// os.CreateTemp makes the file 0600; restore the intended permissions before
	// it becomes the visible file.
	if err = os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err = RenameFn(tmpName, path); err != nil {
		return fmt.Errorf("renaming temp file over %s: %w", path, err)
	}
	syncDir(dir)
	return nil
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
