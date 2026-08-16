// Package store defines the on-disk layout of a nibs store and the rule for
// finding one.
//
// A store is the `.nibs` DIRECTORY itself: it holds the project's config, its
// active nib files and its archive, so locating that one directory resolves
// everything else. Nothing outside it identifies a nibs project — which is why
// the locator stats for a directory rather than a marker file, and why every
// path a store owns is derived here rather than configured.
//
//	<project>/.nibs/config.yml   the project's configuration
//	<project>/.nibs/data/        active nib files
//	<project>/.nibs/archive/     archived nib files
//
// The package is deliberately stdlib-only and depends on nothing else in this
// module, so both internal/config and internal/nibcore can derive their paths
// from the same definitions (nibcore imports config, so config must not import
// nibcore).
package store

import (
	"os"
	"path/filepath"
)

const (
	// DirName is the store directory's name, and the marker that identifies a
	// project root to FindStore.
	DirName = ".nibs"
	// ConfigFileName is the project config file, stored INSIDE the store.
	ConfigFileName = "config.yml"
	// DataDirName holds the active nib files.
	DataDirName = "data"
	// ArchiveDirName holds the archived nib files.
	ArchiveDirName = "archive"
	// LegacyProjectConfigFileName is the pre-layout project config, which sat
	// beside the store rather than inside it. Recognized only so the migration
	// gate can refuse an unmigrated store and `nibs migrate` can relocate it;
	// no command reads a store through it.
	LegacyProjectConfigFileName = ".nibs.yml"
)

// Layout derives every path a store owns from its root directory. The zero
// Layout is not useful — build one with NewLayout.
type Layout struct {
	root string
}

// NewLayout returns the layout of the store rooted at storeRoot (the `.nibs`
// directory itself, not the project directory containing it).
func NewLayout(storeRoot string) Layout {
	return Layout{root: storeRoot}
}

// Root returns the store directory (`<project>/.nibs`).
func (l Layout) Root() string { return l.root }

// ConfigPath returns the project config file inside the store.
func (l Layout) ConfigPath() string { return filepath.Join(l.root, ConfigFileName) }

// DataDir returns the directory holding active nib files.
func (l Layout) DataDir() string { return filepath.Join(l.root, DataDirName) }

// ArchiveDir returns the directory holding archived nib files.
func (l Layout) ArchiveDir() string { return filepath.Join(l.root, ArchiveDirName) }

// ProjectDir returns the directory CONTAINING the store — the project root,
// and the name a project is known by (see config.Config.GetProjectName).
func (l Layout) ProjectDir() string { return filepath.Dir(l.root) }

// DataRel renders a store-relative path for an active nib file with the given
// basename ("data/x.md"). Store-relative paths always use forward slashes:
// they are stored in nib.Path and travel through the API and the web UI.
func (l Layout) DataRel(base string) string {
	return DataDirName + "/" + filepath.ToSlash(base)
}

// ArchiveRel renders a store-relative path for an archived nib file with the
// given basename ("archive/x.md").
func (l Layout) ArchiveRel(base string) string {
	return ArchiveDirName + "/" + filepath.ToSlash(base)
}

// IsArchivedRel reports whether a store-relative path names an archived nib.
// Both separators are accepted because a caller may hand over a path built
// with filepath.Join before normalization.
func (l Layout) IsArchivedRel(rel string) bool {
	return hasDirPrefix(rel, ArchiveDirName)
}

// IsDataRel reports whether a store-relative path names an active nib —
// anything under data/, subdirectories included.
func (l Layout) IsDataRel(rel string) bool {
	return hasDirPrefix(rel, DataDirName)
}

// hasDirPrefix reports whether rel begins with the named directory component,
// tolerating either path separator.
func hasDirPrefix(rel, dir string) bool {
	if len(rel) <= len(dir) || rel[:len(dir)] != dir {
		return false
	}
	sep := rel[len(dir)]
	return sep == '/' || sep == filepath.Separator
}

// WatchableDirs returns the directories a file watcher should observe: the
// store root plus data/ and archive/ where they exist. The ROOT is included
// even though it holds no nib files, so a data/ or archive/ directory created
// after the watch starts is observable as a create event in its parent.
func (l Layout) WatchableDirs() []string {
	dirs := []string{l.root}
	for _, dir := range []string{l.DataDir(), l.ArchiveDir()} {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// FindStore searches upward from startDir for a `.nibs` DIRECTORY and returns
// its absolute path, or an empty string when none is found. A `.nibs` FILE is
// not a store and does not stop the walk.
//
// The NIBS_CONFIG_ROOT environment variable, when set to a non-empty path,
// bounds the walk: each directory up to and including that ceiling is checked,
// but the walk never ascends above it. Comparison is on absolute paths, so a
// ceiling that is not an ancestor of startDir simply never triggers and the
// walk proceeds to the filesystem root as usual. It is a sandboxing and
// test-isolation knob — it keeps a stray ancestor store (e.g. /tmp/.nibs) from
// leaking into tests that expect no store to be found.
func FindStore(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}

	var ceiling string
	if raw := os.Getenv("NIBS_CONFIG_ROOT"); raw != "" {
		ceiling, err = filepath.Abs(raw)
		if err != nil {
			return "", err
		}
	}

	for {
		candidate := filepath.Join(dir, DirName)
		if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
			return candidate, nil
		}

		// Stop at the ceiling: this dir was checked, but do not ascend above it.
		if ceiling != "" && dir == ceiling {
			return "", nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil // reached the filesystem root
		}
		dir = parent
	}
}
