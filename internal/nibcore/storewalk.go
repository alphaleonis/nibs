package nibcore

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/alphaleonis/nibs/internal/store"
)

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
// fn receives each .md file with a nil error, and any path the walk failed to
// enumerate with that error — the caller decides whether an enumeration
// failure aborts the walk (both callers return it, which does). Because a dot
// directory is pruned at its own entry, an unreadable dot directory never
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
		return fn(path, nil)
	})
}
