// Package store defines the on-disk layout of a nibs store and how to find one.
// A store is the `.nibs` directory itself; derive every path it owns from Layout.
//
//	<project>/.nibs/config.yml   the project's configuration
//	<project>/.nibs/areas.yml    the declared areas vocabulary
//	<project>/.nibs/data/        active nib files
//	<project>/.nibs/archive/     archived nib files
//
// Keep it stdlib-only: internal/config and internal/nibcore both import it, and
// nibcore imports config.
package store

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DirName is the store directory's name, and the marker that identifies a
	// project root to FindStore and FindNearestMarker.
	DirName = ".nibs"
	// ConfigFileName is the project config file, stored INSIDE the store.
	ConfigFileName = "config.yml"
	// AreasFileName holds the declared areas vocabulary. Unlike config.yml, which a
	// process reads once, it is reloaded when it changes on disk. It is not
	// evidence that a directory is a store.
	AreasFileName = "areas.yml"
	// DataDirName holds the active nib files.
	DataDirName = "data"
	// ArchiveDirName holds the archived nib files.
	ArchiveDirName = "archive"
	// LegacyProjectConfigFileName is the pre-layout project config beside the
	// store. It is recognized to refuse such a project and for `nibs migrate` to
	// move; no command reads a store through it.
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

// AreasPath returns the declared areas vocabulary inside the store.
func (l Layout) AreasPath() string { return filepath.Join(l.root, AreasFileName) }

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

// IsArchivedRel reports whether a store-relative path names an archived nib,
// separated by '/' or the OS separator.
func (l Layout) IsArchivedRel(rel string) bool {
	return hasDirPrefix(rel, ArchiveDirName)
}

// IsDataRel reports whether a store-relative path names an active nib —
// anything under data/, subdirectories included.
func (l Layout) IsDataRel(rel string) bool {
	return hasDirPrefix(rel, DataDirName)
}

// hasDirPrefix reports whether rel starts with dir followed by '/' or the OS
// separator.
func hasDirPrefix(rel, dir string) bool {
	if len(rel) <= len(dir) || rel[:len(dir)] != dir {
		return false
	}
	sep := rel[len(dir)]
	return sep == '/' || sep == filepath.Separator
}

// WatchableDirs returns the store root plus data/ and archive/ where they exist.
// Watch the root too, so a data/ or archive/ created later arrives as a create
// event.
func (l Layout) WatchableDirs() []string {
	dirs := []string{l.root}
	for _, dir := range []string{l.DataDir(), l.ArchiveDir()} {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// MarkerKind names which kind of nibs marker an upward walk met.
type MarkerKind int

const (
	// MarkerNone means the walk met no marker before its ceiling.
	MarkerNone MarkerKind = iota
	// MarkerStore means a `.nibs` DIRECTORY: a store in the current layout.
	MarkerStore
	// MarkerLegacyProject means a pre-layout `.nibs.yml` FILE. It names the
	// project directory, not a store.
	MarkerLegacyProject
)

// Marker is the nearest nibs marker at or above a starting directory.
type Marker struct {
	Kind MarkerKind
	// Path is the marker itself — the `.nibs` directory for MarkerStore, the
	// `.nibs.yml` file for MarkerLegacyProject, empty for MarkerNone.
	Path string
}

// FindNearestMarker walks upward from startDir and returns the first nibs marker
// of either kind, in one pass; resolve a store through it. Within one directory a
// `.nibs` store wins over `.nibs.yml`. It matches `.nibs` by name, symlinks
// included: cmd's bindNamedStore decides whether the match is a store.
//
// When the walk finds no marker and startDir is in a linked git worktree, it
// checks the main working tree the same way; see mainWorktree. A worktree created
// outside the main clone has no route to a store the main repository does not track.
func FindNearestMarker(startDir string) (Marker, error) {
	marker, err := findUpward(startDir, markerIn)
	if err != nil || marker.Kind != MarkerNone {
		return marker, err
	}
	if mainTree := mainWorktree(startDir); mainTree != "" {
		if found, ok := markerIn(mainTree); ok {
			return found, nil
		}
	}
	return Marker{}, nil
}

// markerIn returns the nibs marker in dir itself, if any.
func markerIn(dir string) (Marker, bool) {
	if candidate := filepath.Join(dir, DirName); isDir(candidate) {
		return Marker{Kind: MarkerStore, Path: candidate}, true
	}
	if candidate := filepath.Join(dir, LegacyProjectConfigFileName); isNonDir(candidate) {
		return Marker{Kind: MarkerLegacyProject, Path: candidate}, true
	}
	return Marker{}, false
}

// gitTimeout bounds the git call, which normally takes milliseconds. The
// directory may come from an archive, and a named pipe where git expects a file
// blocks git until it is killed. The headroom is for a cold disk or a slow
// Windows process start.
var gitTimeout = 5 * time.Second

// mainWorktree returns the main working tree of the linked git worktree that
// holds startDir, or "" when there is none or git cannot tell.
//
// Only a common dir named `.git` has its working tree as its parent. A bare
// repository's common dir and a --separate-git-dir one have no such parent. A
// non-empty NIBS_CONFIG_ROOT must contain the main tree, as it bounds the walk.
func mainWorktree(startDir string) string {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--path-format=absolute", "--git-dir", "--git-common-dir", "--show-toplevel")
	cmd.Dir = startDir
	cmd.Env = withoutRepoLocation(os.Environ())
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSuffix(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n"), "\n")
	if len(lines) != 3 {
		return ""
	}
	gitDir := filepath.Clean(filepath.FromSlash(lines[0]))
	commonDir := filepath.Clean(filepath.FromSlash(lines[1]))
	toplevel := filepath.Clean(filepath.FromSlash(lines[2]))
	if filepath.Base(commonDir) != ".git" || !isRegisteredWorktree(gitDir, commonDir, toplevel) {
		return ""
	}
	mainTree := filepath.Dir(commonDir)
	if raw := os.Getenv("NIBS_CONFIG_ROOT"); raw != "" && !within(mainTree, raw) {
		return ""
	}
	return mainTree
}

// isRegisteredWorktree reports whether the main repository at commonDir lists
// toplevel as one of its linked worktrees, with gitDir as that worktree's entry.
//
// A `.git` file can name any gitdir and, through it, any common dir, so neither
// is evidence alone. The entry must sit under `<commonDir>/worktrees`, and its
// `gitdir` back-pointer, which the main repository holds, must name toplevel's
// `.git`. That `.git` must be a regular file, as `git worktree add` writes it,
// because a symlink to another worktree's `.git` resolves to what that entry names.
func isRegisteredWorktree(gitDir, commonDir, toplevel string) bool {
	if filepath.Dir(gitDir) != filepath.Join(commonDir, "worktrees") {
		return false
	}
	dotGit := filepath.Join(toplevel, ".git")
	if info, err := os.Lstat(dotGit); err != nil || !info.Mode().IsRegular() {
		return false
	}
	raw, ok := readBackPointer(filepath.Join(gitDir, "gitdir"))
	if !ok {
		return false
	}
	backPointer := filepath.FromSlash(strings.TrimSpace(string(raw)))
	if !filepath.IsAbs(backPointer) {
		backPointer = filepath.Join(gitDir, backPointer)
	}
	return resolve(backPointer) == resolve(dotGit)
}

// maxBackPointerBytes bounds a back-pointer read. The file holds one path.
const maxBackPointerBytes = 64 << 10

// readBackPointer returns the contents of a worktree's back-pointer file, or
// false when it cannot be read or is not a small regular file. git never opens
// this file, so gitTimeout does not cover the read, and a named pipe would block it.
func readBackPointer(path string) ([]byte, bool) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, false
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxBackPointerBytes+1))
	if err != nil || len(data) > maxBackPointerBytes {
		return nil, false
	}
	return data, true
}

// withoutRepoLocation drops the variables that point git at a repository other
// than the one around its working directory. A git hook sets GIT_DIR, and with
// it set git reports that repository for any directory, even one outside it.
func withoutRepoLocation(env []string) []string {
	kept := env[:0:0]
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(name) {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR":
			continue
		}
		kept = append(kept, kv)
	}
	return kept
}

// within reports whether path is dir or below it, comparing resolved paths
// because git prints path with its symlinks resolved.
func within(path, dir string) bool {
	rel, err := filepath.Rel(resolve(dir), resolve(path))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolve returns path absolute and with symlinks evaluated, or as absolute as
// it can be made when it cannot be resolved.
func resolve(path string) string {
	if r, err := filepath.EvalSymlinks(path); err == nil {
		path = r
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return filepath.Clean(path)
}

// FindStore searches upward from startDir for a `.nibs` directory and returns its
// absolute path, or "" when there is none. It ignores pre-layout projects, so use
// it only for diagnostics, never to resolve a store.
func FindStore(startDir string) (string, error) {
	return findUpward(startDir, func(dir string) (string, bool) {
		candidate := filepath.Join(dir, DirName)
		return candidate, isDir(candidate)
	})
}

// isDir reports whether path is a directory that can be stat'd.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// isNonDir reports whether path can be stat'd and is not a directory.
func isNonDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// findUpward walks from startDir toward the filesystem root and returns the first
// value match reports, or the zero value. A non-empty NIBS_CONFIG_ROOT bounds every
// locator here: that directory is checked but nothing above it, and a ceiling
// that is not an ancestor of startDir has no effect.
func findUpward[T any](startDir string, match func(dir string) (T, bool)) (T, error) {
	var zero T

	dir, err := filepath.Abs(startDir)
	if err != nil {
		return zero, err
	}

	var ceiling string
	if raw := os.Getenv("NIBS_CONFIG_ROOT"); raw != "" {
		ceiling, err = filepath.Abs(raw)
		if err != nil {
			return zero, err
		}
	}

	for {
		if found, ok := match(dir); ok {
			return found, nil
		}

		// Stop at the ceiling: this dir was checked, but do not ascend above it.
		if ceiling != "" && dir == ceiling {
			return zero, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return zero, nil // reached the filesystem root
		}
		dir = parent
	}
}
