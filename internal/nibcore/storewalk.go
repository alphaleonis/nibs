package nibcore

import (
	"errors"
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
// fn receives each .md file with a nil error, and any path WalkDir failed to
// enumerate with that error — the caller decides whether an enumeration
// failure aborts the walk (both callers return it, which does). Because a dot
// directory is pruned at its own entry, an unreadable dot directory never
// reaches fn.
func WalkStoreFiles(root string, fn func(path string, err error) error) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fn(path, err)
		}
		if d.IsDir() {
			if path != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		return fn(path, nil)
	})
}
