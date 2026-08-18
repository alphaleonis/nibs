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
// It travels through the walk's ERROR channel because that is the only channel
// there is, but it is not an enumeration failure and callers must not treat it
// as one: the walk enumerated the entry perfectly well and is declining to hand
// it over. Every caller skips it and records a per-file diagnostic, which is what
// keeps one bad entry from bricking a store (see Core.loadFromDisk).
var ErrNotRegularFile = errors.New("not a regular file")

// OpenRegularFile opens path for reading and refuses anything that is not a
// regular file, with ErrNotRegularFile.
//
// THE OPEN IS NON-BLOCKING, and that is the whole mechanism: os.Open on a FIFO
// blocks in open(2) until a writer appears, so a stat-then-open would still hang
// in the window between the two — and the callers that need this most reach it
// from an fsnotify event, where the path was created a moment ago and can change
// again. O_NONBLOCK makes the open itself return, so the mode is read from the fd
// that was actually opened rather than from a second look at the path. Windows
// defines O_NONBLOCK and ignores it; it holds no FIFOs at filesystem paths for it
// to matter to.
//
// WalkStoreFiles answers a similar question one layer up, and both earn their
// place. The walk decides what IS a nib file: a caller that never opens one — the
// layout step, which relocates by name — has to classify it the same way, and the
// diagnostic is better produced without touching the file at all. This is the
// invariant for every OPENER, including the ones no walk feeds: the fsnotify
// watcher loads a single path on a Create event (under the write lock, so a hang
// there wedges every reader too), and computeStoredETag re-reads a path recorded
// at load time.
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
// sitting at the store ROOT is deliberately NOT content — that is the
// pre-migration shape, and the migration gate refuses every command until
// `nibs migrate` relocates it, so loading it would let a store nobody may
// operate on answer queries anyway.
//
// A missing data/ or archive/ directory is not an error: a store with nothing
// archived has no archive/, and one created but not yet written to has an
// empty data/. Any other enumeration failure reaches fn exactly as
// WalkStoreFiles reports it.
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

// WalkStoreFiles is the raw walk shared by Core.Load's disk load and
// cmd/migrate's scans (scanStore, filesMatching), so the two sides classify
// individual FILES identically: every .md file under root, subdirectories
// included, EXCEPT inside dot directories. A dot directory (`.git` of a store
// repo, editor state like `.obsidian` or `.trash`) is not store content and is
// pruned wholesale. Dot FILES (an editor lock like `.#x.md`) are still visited
// and classified per file like any other. The root passed in is exempt from
// the dot rule — the store directory is typically named `.nibs`.
//
// The two callers pass DIFFERENT roots, and that difference is the point.
// Load walks the store's content directories (data/ and archive/, via
// WalkStoreContent) because after the store layout inversion a .md file at the
// store root is not store content. Migration's scans walk the store ROOT,
// because a root-level .md file is exactly the pre-migration shape they exist
// to find and relocate. Neither may be expressed in terms of the other: a
// migrate scan restricted to data/ would never see the files it must move,
// and a Load widened to the root would answer queries from a store every
// command is refusing to touch.
//
// fn receives THREE kinds of call, and the error argument is what separates
// them: a .md file with a nil error; a path the walk could not enumerate, with
// that error, which every caller returns and so aborts the walk; and a .md entry
// the walk DECLINED to hand over, with ErrNotRegularFile, which every caller
// skips and records as one bad file (see that sentinel). A caller that treats the
// third as the second turns one FIFO into a store no command will touch. Because
// a dot directory is pruned at its own entry, an unreadable dot directory never
// reaches fn. Every path handed to fn is rooted at the caller's spelling of
// root, so store-relative derivations hold.
//
// The ERROR handed to fn names that same rooted path. os.dirFS trims the root
// prefix from its *PathError, so the raw error says `open deep: permission
// denied` about a store whose only clue to WHICH deep that is comes from the
// caller — and `nibs check`, the one command that reports a load failure without
// re-annotating it, printed exactly that. Wrapping here rather than at each
// caller also disambiguates data/ from archive/, which WalkStoreContent walks with
// two separate calls and no directory tag. errors.Is still reaches the underlying
// fs.ErrNotExist / fs.ErrPermission.
//
// The ROOT is opened rather than Lstat'd, which is why this goes through
// os.DirFS instead of filepath.WalkDir. filepath.WalkDir Lstats its root, so a
// store reached through a SYMLINK — the ordinary spelling of "the nibs live on
// another volume", and what a dotfile manager produces — yielded ONE non-directory
// entry and no files at all: the migration then moved nothing, reported success,
// and left the store's nibs unreachable while `nibs check` called it healthy.
// Symlinked SUBDIRECTORIES are still not followed, matching filepath.WalkDir:
// os.DirFS's ReadDir reports a link as a link, not as the directory it points at.
func WalkStoreFiles(root string, fn func(path string, err error) error) error {
	return fs.WalkDir(os.DirFS(root), ".", func(rel string, d fs.DirEntry, err error) error {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err != nil {
			return fn(path, fmt.Errorf("%s: %w", path, err))
		}
		if d.IsDir() {
			// rel == "." IS the root, which is exempt from the dot rule: the
			// store directory is typically named `.nibs`.
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

// leadsToRegularFile reports whether a walked entry is a regular file, or a
// symlink leading to one.
//
// THE ANSWER MUST COME FROM THE DIRECTORY ENTRY, not from reading the file.
// Every caller's next move is os.Open, and opening a FIFO blocks until a writer
// appears — so unlike a malformed nib, an irregular file cannot be read to
// discover that it is bad. Reading IS the hang, which is why this sits in the
// walk and not at each opener (there are three: Core.loadNib,
// cmd.readFrontMatterHeader, and the corroboration probe store resolution runs).
//
// A SYMLINK IS RESOLVED BEFORE IT IS JUDGED, and that is not incidental:
// os.DirFS reports a link as a link, so `d.Type().IsRegular()` alone is false for
// every symlinked nib file — and a link to a real nib file IS a nib file, which a
// dotfile manager or a partially-synced store produces routinely. Judging the
// entry rather than its destination drops those nibs out of every query in
// silence. A link AT a FIFO is the same hang wearing a
// different name, so following it is also what makes the guard complete.
//
// A LINK THAT CANNOT BE RESOLVED IS HANDED ON rather than skipped here. The
// opener's own error names what is wrong ("no such file"), which is a better
// diagnostic than "not a regular file", and opening a broken link cannot block.
// os.Stat is safe to call on any of these: stat(2) does not open, so it never
// blocks on a FIFO the way an open would.
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
