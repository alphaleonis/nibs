package nibcore

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// WalkStoreFiles walks every file under root that is STORE CONTENT — the one
// definition shared by Core.Load's disk walk and cmd/migrate's scans
// (scanStore, filesMatching), so the two sides can never again disagree about
// which files are nibs. A file only one side sees is worse than either
// behavior alone: a nib-shaped file the migration scan skips but Load loads is
// invisible to every pre-migration gate yet visible to every query — and
// `nibs migrate` would rewrite it.
//
// Store content is every .md file under root, subdirectories included
// (archive/ most importantly — archived nibs stay loaded and migrate too),
// EXCEPT inside dot directories: a dot directory (`.git` of a store repo,
// editor state like `.obsidian` or `.trash`) is not store content and is
// pruned wholesale. Dot FILES (an editor lock like `.#x.md`) are still
// visited and classified per file like any other. The root itself is exempt
// from the dot rule — the store directory is typically named `.nibs`.
//
// fn receives each store .md file with a nil error, and any path WalkDir
// failed to enumerate with that error — the caller decides whether an
// enumeration failure aborts the walk (both callers return it, which does).
// Because a dot directory is pruned at its own entry, an unreadable dot
// directory never reaches fn.
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
