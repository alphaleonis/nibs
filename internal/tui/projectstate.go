package tui

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"unicode"

	"github.com/alphaleonis/nibs/internal/fsutil"
	"github.com/alphaleonis/nibs/internal/safetext"
	"github.com/alphaleonis/nibs/internal/viewshape"
	"github.com/alphaleonis/nibs/internal/yamlfile"
	"gopkg.in/yaml.v3"
)

// rememberedView is what the TUI remembers of a project between launches: the
// view and the filter's applied query, or "" for none.
type rememberedView struct {
	level viewshape.Level
	query string
}

// defaultView returns the view a project opens on when the TUI remembers
// nothing of it, with query as its filter.
func defaultView(query string) rememberedView {
	return rememberedView{level: viewshape.LevelMilestones, query: query}
}

// stateFileVersion is the format version the state file is written in.
// parseStateFile also reads stateFileVersion1, and a file of any other version
// is malformed.
const stateFileVersion = 2

// stateFileVersion1 is the format whose query is only the words of a text
// filter, which matched a nib's title or id. Closed nibs were hidden or not by
// the config, outside the query.
const stateFileVersion1 = 1

// stateFile is the YAML a state file holds.
type stateFile struct {
	Version int `yaml:"version"`
	// Store names the store the file is for, so a person can tell the files
	// apart. Nothing reads it.
	Store string `yaml:"store"`
	View  string `yaml:"view"`
	Query string `yaml:"query"`
}

// projectMemory is the state file of one store, which holds the store's
// rememberedView. The file sits in the user state directory, never in the
// store or the project.
type projectMemory struct {
	path  string // the state file
	store string // the store's absolute path with symlinks resolved
	saved rememberedView
}

// openProjectMemory returns the state file of the store at root, and the view
// it remembers. defaultQuery is the query of the default view, and what a
// version 1 file's words are added to.
//
// A missing or unreadable file remembers defaultView. So does a malformed one,
// and warning then says so for the status line; otherwise warning is "". The
// memory is nil when the OS gives no user state directory, and when
// refusedStateDir refuses the nibs directory in it that holds the state files.
// warning says so in the second case.
func openProjectMemory(root, defaultQuery string) (memory *projectMemory, view rememberedView, warning string) {
	defaultView := defaultView(defaultQuery)
	dir, err := userStateDir()
	if err != nil {
		return nil, defaultView, ""
	}
	files := resolvedPath(filepath.Join(dir, "nibs"))
	store, project := resolvedPath(root), resolvedPath(filepath.Dir(absPath(root)))
	if refusedStateDir(files, store, project) {
		return nil, defaultView, fmt.Sprintf("The view and the filter are not remembered: the state directory %s is inside the project",
			safetext.Strip(filepath.Join(dir, "nibs")))
	}
	memory = &projectMemory{path: stateFilePath(files, store), store: store, saved: defaultView}
	data, err := yamlfile.ReadFile(memory.path)
	if err != nil {
		return memory, defaultView, ""
	}
	view, ok := parseStateFile(data, defaultQuery)
	if !ok {
		return memory, defaultView, fmt.Sprintf("Opened on %s: the state file %s is malformed",
			levelNames[defaultView.level], safetext.Strip(memory.path))
	}
	memory.saved = view
	return memory, view, ""
}

// refusedStateDir reports whether the state files must not go in stateDir: a
// directory inside store, or inside project unless project is the user's home
// directory, which holds the default state directory. All three are resolved.
func refusedStateDir(stateDir, store, project string) bool {
	if within(stateDir, store) {
		return true
	}
	if home, err := os.UserHomeDir(); err == nil && resolvedPath(home) == project {
		return false
	}
	return within(stateDir, project)
}

// parseStateFile returns the view a state file's content remembers, and false
// when the content is malformed. A version 1 file's words become search words
// after defaultQuery, which then stands for the config's closed filter.
func parseStateFile(data []byte, defaultQuery string) (rememberedView, bool) {
	var f stateFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return rememberedView{}, false
	}
	level := viewshape.Level(f.View)
	// The filter input turns a tab or a line break into a space and drops every
	// other control rune, so a query holding one was not saved from it:
	// https://github.com/charmbracelet/bubbles/blob/v2.1.1/internal/runeutil/runeutil.go#L88
	if (f.Version != stateFileVersion && f.Version != stateFileVersion1) ||
		!slices.Contains(levels, level) || strings.ContainsFunc(f.Query, unicode.IsControl) {
		return rememberedView{}, false
	}
	query := f.Query
	if f.Version == stateFileVersion1 {
		query = strings.TrimSpace(defaultQuery + " " + f.Query)
	}
	return rememberedView{level: level, query: query}, true
}

// save writes view to the state file when it differs from what the file was
// last read or written with. A failed write is tried again only once view
// changes again.
func (p *projectMemory) save(view rememberedView) error {
	if view == p.saved {
		return nil
	}
	p.saved = view
	data, err := yaml.Marshal(stateFile{Version: stateFileVersion, Store: p.store, View: string(view.level), Query: view.query})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p.path), 0o700); err != nil {
		return err
	}
	return fsutil.AtomicWriteFile(p.path, data, 0o600)
}

// resolvedPath returns path as an absolute path with the symlinks of its
// nearest existing ancestor resolved, so every path that leads to one store
// names one state file.
func resolvedPath(path string) string {
	abs := absPath(path)
	var rest []string
	for dir := abs; ; dir = filepath.Dir(dir) {
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(append([]string{resolved}, rest...)...)
		}
		if filepath.Dir(dir) == dir {
			return abs
		}
		rest = append([]string{filepath.Base(dir)}, rest...)
	}
}

// absPath returns path as an absolute path, or path itself when the working
// directory is unknown.
func absPath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

// within reports whether path is dir or lies under it. Both are absolute.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// stateFilePath returns the state file of store in files, the nibs directory
// of the user state directory. Its name is the first 16 bytes of the store
// path's SHA-256, in lowercase hexadecimal, with a .yml extension.
func stateFilePath(files, store string) string {
	sum := sha256.Sum256([]byte(store))
	return filepath.Join(files, hex.EncodeToString(sum[:16])+".yml")
}

// userStateDir returns the directory the OS gives for the user's state; see
// stateDirFor.
func userStateDir() (string, error) {
	return stateDirFor(runtime.GOOS, os.Getenv)
}

// stateDirFor returns the user state directory on goos, reading the
// environment through getenv.
//
// It is %LocalAppData% on Windows and $HOME/Library/Application Support on
// macOS and iOS. Elsewhere it is $XDG_STATE_HOME, or $HOME/.local/state when
// that is unset or relative, as the XDG Base Directory Specification says:
// https://specifications.freedesktop.org/basedir/0.8/
func stateDirFor(goos string, getenv func(string) string) (string, error) {
	switch goos {
	case "windows":
		if dir := getenv("LocalAppData"); dir != "" {
			return dir, nil
		}
		return "", errors.New("%LocalAppData% is not defined")
	case "darwin", "ios":
		if home := getenv("HOME"); home != "" {
			return filepath.Join(home, "Library", "Application Support"), nil
		}
		return "", errors.New("$HOME is not defined")
	default:
		if dir := getenv("XDG_STATE_HOME"); filepath.IsAbs(dir) {
			return dir, nil
		}
		if home := getenv("HOME"); home != "" {
			return filepath.Join(home, ".local", "state"), nil
		}
		return "", errors.New("neither $XDG_STATE_HOME nor $HOME is defined")
	}
}
