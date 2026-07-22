package nibcore

import (
	"fmt"
	"os"
	"path/filepath"
)

// renameFn is a seam over os.Rename so tests can simulate a crash between the
// temp write and the rename. Production always uses os.Rename.
var renameFn = os.Rename

// atomicWriteFile writes data to path atomically: it writes to a uniquely-named
// temp file in the same directory, fsyncs it, then renames it over path. Because
// the rename is atomic on the same filesystem, a concurrent reader observes
// either the old file or the fully-written new file — never a torn/partial write
// that would fail nib.Parse. A failure before the rename leaves any existing file
// at path untouched, and the temp file is removed on every error path.
//
// A unique temp name (not a fixed "<path>.tmp" suffix) is deliberate: two writers
// racing on the same nib — the exact hazard this hardening targets — must not
// collide on a shared temp file. Each renames its own complete temp; the later
// rename wins wholesale rather than interleaving bytes.
func atomicWriteFile(path string, data []byte, perm os.FileMode) (err error) {
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
	// it becomes the visible nib.
	if err = os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err = renameFn(tmpName, path); err != nil {
		return fmt.Errorf("renaming temp file over %s: %w", path, err)
	}
	return nil
}
