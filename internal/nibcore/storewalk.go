package nibcore

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/alphaleonis/nibs/internal/store"
)

// ErrNotRegularFile marks a store entry named like a nib file that is not a
// regular file — a FIFO, socket or device, or a symlink leading to one.
//
// It arrives through the walk's error channel but is NOT an enumeration
// failure: the walk enumerated the entry and is declining to hand it over. Skip
// the entry and carry on; do not abort the walk.
var ErrNotRegularFile = errors.New("not a regular file")

// OpenRegularFile opens path for reading and refuses anything that is not a
// regular file, with ErrNotRegularFile.
//
// THE OPEN IS NON-BLOCKING: os.Open on a FIFO blocks in open(2) until a writer
// appears, and a stat-then-open would still hang in the window between the two.
// O_NONBLOCK makes the open itself return, so the mode is read from the fd that
// was actually opened. Windows defines O_NONBLOCK and ignores it.
//
// Open every store file through here, including the paths no walk feeds: the
// fsnotify watcher loads a single path on a Create event under the write lock,
// where a hang wedges every reader too.
func OpenRegularFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("%s: %w", path, ErrNotRegularFile)
	}
	return f, nil
}

// WalkStoreContent walks every file that is STORE CONTENT for a store laid out
// at l: the .md files under data/ and archive/, and nothing else. A .md file
// sitting at the store ROOT is NOT content — that is the pre-migration shape,
// which `nibs migrate` relocates.
//
// A missing data/ or archive/ directory is not an error: a store with nothing
// archived has no archive/, and one created but not yet written to has an
// empty data/.
func WalkStoreContent(l store.Layout, fn func(path string, err error) error) error {
	for _, dir := range []string{l.DataDir(), l.ArchiveDir()} {
		if _, err := os.Stat(dir); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err := fn(dir, err); err != nil {
				return err
			}
			continue
		}
		if err := WalkStoreFiles(dir, fn); err != nil {
			return err
		}
	}
	return nil
}

// WalkStoreFiles calls fn for every .md file under root, subdirectories
// included, EXCEPT inside dot directories: a dot directory (`.git`, `.obsidian`,
// `.trash`) is pruned wholesale, while dot FILES (an editor lock like `.#x.md`)
// are visited and classified like any other. root itself is exempt from the dot
// rule — a store directory is typically named `.nibs`.
//
// fn receives THREE kinds of call, separated by the error argument: a .md file
// with a nil error; a path the walk could not enumerate, with that error, which
// a caller returns to abort the walk; and a .md entry the walk DECLINED to hand
// over, with ErrNotRegularFile, which a caller skips (see that sentinel).
// Treating the third as the second turns one FIFO into a store no command will
// touch. Every path handed to fn is rooted at the caller's spelling of root.
//
// The error handed to fn names that rooted path: os.dirFS trims the root prefix
// from its *PathError, so the unwrapped error is relative to root and names no
// root at all — and WalkStoreContent walks data/ and archive/ with two separate
// calls and no directory tag. errors.Is still reaches the underlying
// fs.ErrNotExist / fs.ErrPermission.
//
// Walk through os.DirFS, not filepath.WalkDir: WalkDir Lstats its root, so a
// store reached through a SYMLINK yields one non-directory entry and no files at
// all. Symlinked SUBDIRECTORIES are not followed either way — os.DirFS's ReadDir
// reports a link as a link.
func WalkStoreFiles(root string, fn func(path string, err error) error) error {
	return fs.WalkDir(os.DirFS(root), ".", func(rel string, d fs.DirEntry, err error) error {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err != nil {
			return fn(path, fmt.Errorf("%s: %w", path, err))
		}
		if d.IsDir() {
			if rel != "." && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		if !leadsToRegularFile(path, d) {
			return fn(path, fmt.Errorf("%s: %w", path, ErrNotRegularFile))
		}
		return fn(path, nil)
	})
}

// WalkStoreDirs calls fn for every DIRECTORY inside root that store content can
// live in, root itself first, applying WalkStoreFiles' dot rule.
//
// The file watcher enumerates through this rather than its own walk, so the
// watched directory set and the scans' file set come from one definition.
//
// An unreadable directory is still handed to fn; only its subtree is skipped,
// and the walk continues rather than failing.
func WalkStoreDirs(root string, fn func(dir string) error) error {
	return fs.WalkDir(os.DirFS(root), ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if rel != "." && strings.HasPrefix(d.Name(), ".") {
			return fs.SkipDir
		}
		return fn(filepath.Join(root, filepath.FromSlash(rel)))
	})
}

// leadsToRegularFile reports whether a walked entry is a regular file, or a
// symlink leading to one.
//
// THE ANSWER MUST COME FROM THE DIRECTORY ENTRY, not from reading the file:
// opening a FIFO blocks until a writer appears, so unlike a malformed nib, an
// irregular file cannot be read to discover that it is bad. Reading IS the hang,
// so the guard sits in the walk rather than at each opener.
//
// A SYMLINK IS RESOLVED BEFORE IT IS JUDGED: os.DirFS reports a link as a link,
// so `d.Type().IsRegular()` alone is false for every symlinked nib file, and a
// link to a real nib file IS a nib file (a dotfile manager or a partially-synced
// store produces them). A link AT a FIFO is the same hang wearing a different
// name.
//
// A LINK THAT CANNOT BE RESOLVED IS HANDED ON rather than skipped here: the
// opener's own error names what is wrong ("no such file"), and opening a broken
// link cannot block. os.Stat is safe on any of these — stat(2) does not open, so
// it never blocks on a FIFO the way an open would.
func leadsToRegularFile(path string, d fs.DirEntry) bool {
	if d.Type().IsRegular() {
		return true
	}
	if d.Type()&fs.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(path)
	return err != nil || info.Mode().IsRegular()
}
