package tui

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/alphaleonis/nibs/internal/area"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/testskip"
	"github.com/alphaleonis/nibs/internal/viewshape"
)

// stateDirVar returns the environment variable that userStateDir reads on
// goos, and the path under it that userStateDir returns.
func stateDirVar(goos string) (name string, under []string) {
	switch goos {
	case "windows":
		return "LocalAppData", nil
	case "darwin", "ios":
		return "HOME", []string{"Library", "Application Support"}
	default:
		return "XDG_STATE_HOME", nil
	}
}

// isolateStateDir points userStateDir at a fresh temp directory until the test
// ends, and returns the directory the state files go in. The redirect is a
// t.Setenv, so a caller cannot run in parallel.
func isolateStateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	name, under := stateDirVar(runtime.GOOS)
	t.Setenv(name, dir)
	want := filepath.Join(append([]string{dir}, under...)...)
	if got, err := userStateDir(); err != nil || got != want {
		t.Fatalf("userStateDir() = %q, %v, want %q", got, err, want)
	}
	return filepath.Join(want, "nibs")
}

// sessionOn starts a session on a stub store at root holding groupedNibs and
// groupedAreas, as Run starts one.
func sessionOn(t *testing.T, root string) *App {
	t.Helper()
	return startSession(t, newGroupedStub(t, root), config.Default(), config.UserLayer{})
}

// newGroupedStub is a stub store at root holding groupedNibs and groupedAreas.
func newGroupedStub(t *testing.T, root string) *StubBackend {
	t.Helper()
	vocab, err := area.Parse([]byte(groupedAreas))
	if err != nil {
		t.Fatal(err)
	}
	nibs := groupedNibs()
	stub := &StubBackend{Nibs: map[string]*nib.Nib{}, AllNibs: nibs, RootDir: root, AreaVocab: vocab}
	for _, n := range nibs {
		stub.Nibs[n.ID] = n
	}
	return stub
}

// startSession starts a session on backend with cfg and user, and loads its
// nibs, as Run does.
func startSession(t *testing.T, backend Backend, cfg *config.Config, user config.UserLayer) *App {
	t.Helper()
	app := newSession(backend, cfg, user, "dev")
	initCmd := app.Init()
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	processCmd(app, initCmd)
	return app
}

// newStoreDir returns the path of a store, .nibs in a fresh project
// directory. Nothing is created at the path.
func newStoreDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), ".nibs")
}

// stateFileOf returns the state file of the store at root, under the state
// directory dir that isolateStateDir returned.
func stateFileOf(dir, root string) string {
	return stateFilePath(resolvedPath(dir), resolvedPath(root))
}

// stateFiles returns the names of the files in the state directory dir.
func stateFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestStateDirFor(t *testing.T) {
	tests := []struct {
		name    string
		goos    string
		env     map[string]string
		want    string
		wantErr bool
	}{
		{name: "XDG_STATE_HOME on Linux", goos: "linux", env: map[string]string{"XDG_STATE_HOME": "/xdg", "HOME": "/home/u"}, want: filepath.Join("/xdg")},
		{name: "HOME on Linux without XDG_STATE_HOME", goos: "linux", env: map[string]string{"HOME": "/home/u"}, want: filepath.Join("/home/u", ".local", "state")},
		{name: "a relative XDG_STATE_HOME is ignored", goos: "freebsd", env: map[string]string{"XDG_STATE_HOME": "rel", "HOME": "/home/u"}, want: filepath.Join("/home/u", ".local", "state")},
		{name: "neither on Linux", goos: "linux", env: map[string]string{}, wantErr: true},
		{name: "macOS ignores XDG_STATE_HOME", goos: "darwin", env: map[string]string{"XDG_STATE_HOME": "/xdg", "HOME": "/Users/u"}, want: filepath.Join("/Users/u", "Library", "Application Support")},
		{name: "no HOME on macOS", goos: "darwin", env: map[string]string{}, wantErr: true},
		{name: "LocalAppData on Windows", goos: "windows", env: map[string]string{"LocalAppData": `C:\Users\u\AppData\Local`, "XDG_STATE_HOME": "/xdg"}, want: `C:\Users\u\AppData\Local`},
		{name: "no LocalAppData on Windows", goos: "windows", env: map[string]string{"HOME": "/home/u"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := stateDirFor(tt.goos, func(k string) string { return tt.env[k] })
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("stateDirFor(%q) = %q, %v, want %q (error: %v)", tt.goos, got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestANeverOpenedProjectOpensOnMilestones(t *testing.T) {
	dir := isolateStateDir(t)
	app := sessionOn(t, newStoreDir(t))

	if app.list.level != viewshape.LevelMilestones {
		t.Errorf("the session opened on %q, want Milestones", app.list.level)
	}
	if app.list.statusMessage != "" {
		t.Errorf("the status line reads %q, want nothing", app.list.statusMessage)
	}
	if files := stateFiles(t, dir); len(files) != 0 {
		t.Errorf("opening wrote %v, want no state file before the view or query changes", files)
	}
}

// Rows: groupedNibs in Areas under the query "type:task o", which t1 (One),
// t2 (Two) and t4 (Four) match. The state file holds the whole query, the
// tokens included.
func TestARelaunchReopensTheViewAndTheQuery(t *testing.T) {
	dir := isolateStateDir(t)
	root := newStoreDir(t)
	first := sessionOn(t, root)
	pressKeys(first, "v a / ctrl+u")
	typeText(first, "type:task o -tags:wip")
	pressKeys(first, "enter")
	const query = "type:task -tags:wip o"
	if first.list.level != viewshape.LevelAreas || first.list.editing || first.list.query != query {
		t.Fatalf("premise failed: the first session shows %q with the box open = %v and query %q", first.list.level, first.list.editing, first.list.query)
	}
	want := rowIDs(first)
	if data, err := os.ReadFile(stateFileOf(dir, root)); err != nil || !strings.Contains(string(data), "query: "+query+"\n") {
		t.Errorf("the state file holds %q (%v), want the query %q", data, err, query)
	}

	again := sessionOn(t, root)

	if again.list.level != viewshape.LevelAreas {
		t.Errorf("the relaunch opened on %q, want Areas", again.list.level)
	}
	if again.list.editing || again.list.query != query {
		t.Errorf("the relaunch has query %q with the box open = %v, want %q applied", again.list.query, again.list.editing, query)
	}
	if got := rowIDs(again); !slices.Equal(got, want) {
		t.Errorf("the relaunch shows rows %v, want %v", got, want)
	}
	if got := headerOf(again); !strings.HasPrefix(got, "Areas") {
		t.Errorf("the relaunch's header = %q, want it to name Areas", got)
	}
}

// esc in the box empties the query, -status:closed included, and a relaunch
// keeps it empty rather than opening on the default query.
func TestClearingTheQueryIsRemembered(t *testing.T) {
	isolateStateDir(t)
	root := newStoreDir(t)
	first := sessionOn(t, root)
	pressKeys(first, "/")
	typeText(first, "o")
	pressKeys(first, "enter / esc")
	if first.list.editing || first.list.query != "" {
		t.Fatalf("premise failed: esc left the box open = %v with query %q", first.list.editing, first.list.query)
	}

	if again := sessionOn(t, root); again.list.query != "" {
		t.Errorf("the relaunch has query %q, want none", again.list.query)
	}
}

// A version 1 file holds only the words of a text filter, which match the same
// nibs as search words, and the config decided whether closed nibs showed.
func TestAVersion1StateFileOpensWithItsWordsAfterTheDefaultQuery(t *testing.T) {
	shown := config.Default()
	shown.Nibs.HideCompleted = new(bool)
	for name, tt := range map[string]struct {
		cfg  *config.Config
		want string
	}{
		"closed hidden": {config.Default(), closedToken + " o"},
		"closed shown":  {shown, "o"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := isolateStateDir(t)
			root := newStoreDir(t)
			writeFile(t, stateFileOf(dir, root), "version: 1\nview: areas\nquery: o\n")

			app := startSession(t, newGroupedStub(t, root), tt.cfg, config.UserLayer{})

			if app.list.level != viewshape.LevelAreas || app.list.query != tt.want || app.list.statusMessage != "" {
				t.Errorf("the session opened on %q with query %q and status %q, want Areas with %q and no warning",
					app.list.level, app.list.query, app.list.statusMessage, tt.want)
			}
		})
	}
}

// The filter input keeps these format and space runes, so a query holding
// one reopens as it was typed.
func TestAQueryWithFormatOrSpaceRunesReopens(t *testing.T) {
	for name, query := range map[string]string{
		"a zero-width joiner":     "a\u200db",
		"a zero-width non-joiner": "a\u200cb",
		"a no-break space":        "a\u00a0b",
		"a right-to-left mark":    "a\u200fb",
	} {
		t.Run(name, func(t *testing.T) {
			isolateStateDir(t)
			root := newStoreDir(t)
			first := sessionOn(t, root)
			pressKeys(first, "v a / ctrl+u")
			typeText(first, query)
			pressKeys(first, "enter")
			if first.list.query != canonicalQuery(query, nil) {
				t.Fatalf("premise failed: the query is %q, want %q", first.list.query, canonicalQuery(query, nil))
			}

			again := sessionOn(t, root)

			if again.list.level != viewshape.LevelAreas || again.list.query != first.list.query || again.list.statusMessage != "" {
				t.Errorf("the relaunch opened on %q with query %q and status %q, want Areas with %q and no warning",
					again.list.level, again.list.query, again.list.statusMessage, first.list.query)
			}
		})
	}
}

// A query still being typed is not applied, so a relaunch does not reopen it.
func TestAQueryBeingTypedIsNotRemembered(t *testing.T) {
	dir := isolateStateDir(t)
	app := sessionOn(t, newStoreDir(t))
	pressKeys(app, "/")
	typeText(app, "o")

	if files := stateFiles(t, dir); len(files) != 0 {
		t.Errorf("typing wrote %v, want no state file", files)
	}
}

func TestEachStoreHasItsOwnState(t *testing.T) {
	isolateStateDir(t)
	one, other := newStoreDir(t), newStoreDir(t)
	pressKeys(sessionOn(t, one), "v a")

	if app := sessionOn(t, other); app.list.level != viewshape.LevelMilestones {
		t.Errorf("another store opened on %q, want Milestones", app.list.level)
	}
	if app := sessionOn(t, one); app.list.level != viewshape.LevelAreas {
		t.Errorf("the first store reopened on %q, want Areas", app.list.level)
	}
}

// A store reached through a symlink shares the state file of its target.
func TestAStoreThroughASymlinkSharesItsTargetsState(t *testing.T) {
	isolateStateDir(t)
	store := newStoreDir(t)
	if err := os.Mkdir(store, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), ".nibs")
	if err := os.Symlink(store, link); err != nil {
		testskip.SymlinkUnavailable(t, err)
	}
	pressKeys(sessionOn(t, store), "v a")

	if app := sessionOn(t, link); app.list.level != viewshape.LevelAreas {
		t.Errorf("the store through a link opened on %q, want Areas", app.list.level)
	}
}

func TestAMalformedStateFileOpensOnTheDefaultsWithAWarning(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "not YAML", content: "version: [1\n"},
		{name: "no version", content: "view: areas\nquery: o\n"},
		{name: "another version", content: "version: 3\nview: areas\nquery: o\n"},
		{name: "an unknown view", content: "version: 1\nview: epics\nquery: o\n"},
		{name: "a query that drives the terminal", content: "version: 1\nview: areas\nquery: \"\\e[31mo\"\n"},
		{name: "a query of invalid UTF-8", content: "version: 1\nview: areas\nquery: o\xff\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := isolateStateDir(t)
			root := newStoreDir(t)
			path := stateFileOf(dir, root)
			writeFile(t, path, tt.content)

			app := sessionOn(t, root)

			if app.list.level != viewshape.LevelMilestones || app.list.query != closedToken {
				t.Errorf("the session opened on %q with query %q, want Milestones and %s", app.list.level, app.list.query, closedToken)
			}
			if app.list.statusKind != statusWarn || !strings.Contains(app.list.statusMessage, "malformed") {
				t.Errorf("the status line reads %q, want a warning that the state file is malformed", app.list.statusMessage)
			}
		})
	}
}

func TestAMalformedStateFileWarnsBesideAUserConfigWarning(t *testing.T) {
	dir := isolateStateDir(t)
	root := newStoreDir(t)
	writeFile(t, stateFileOf(dir, root), "version: [1\n")
	user, path := writeUserConfig(t, "tui:\n  keys:\n    nib.prority: P\n")

	app := startSession(t, newGroupedStub(t, root), config.Default(), config.UserLayer{Config: user, Path: path})

	for _, want := range []string{"Ignored 1 TUI setting", "; opened on Milestones: the state file"} {
		if !strings.Contains(app.list.statusMessage, want) {
			t.Errorf("the status line reads %q, want it to hold %q", app.list.statusMessage, want)
		}
	}
}

// A field the format does not know is ignored, so the rest still applies.
func TestAStateFileWithAnUnknownFieldStillApplies(t *testing.T) {
	dir := isolateStateDir(t)
	root := newStoreDir(t)
	writeFile(t, stateFileOf(dir, root), "version: 2\nview: areas\nquery: o\nfolds: [t1]\n")

	app := sessionOn(t, root)

	if app.list.level != viewshape.LevelAreas || app.list.query != "o" || app.list.statusMessage != "" {
		t.Errorf("the session opened on %q with query %q and status %q, want Areas with o and no warning",
			app.list.level, app.list.query, app.list.statusMessage)
	}
}

// A directory where the state file goes cannot be read, which is not a
// malformed file.
func TestAnUnreadableStateFileOpensOnTheDefaultsSilently(t *testing.T) {
	dir := isolateStateDir(t)
	root := newStoreDir(t)
	if err := os.MkdirAll(stateFileOf(dir, root), 0o755); err != nil {
		t.Fatal(err)
	}

	app := sessionOn(t, root)

	if app.list.level != viewshape.LevelMilestones || app.list.statusMessage != "" {
		t.Errorf("the session opened on %q with status %q, want Milestones and no warning", app.list.level, app.list.statusMessage)
	}
}

func TestAFailedSaveWarns(t *testing.T) {
	dir := isolateStateDir(t)
	writeFile(t, dir, "a file where the state directory goes")
	app := sessionOn(t, newStoreDir(t))

	pressKeys(app, "v a")

	if app.list.statusKind != statusWarn || !strings.Contains(app.list.statusMessage, "remember") {
		t.Errorf("the status line reads %q, want a warning that the view was not remembered", app.list.statusMessage)
	}
}

// The sample fixture's store and project gain no file from a session that
// changes the view and the query; the state directory gains one.
func TestTheStateFileIsWrittenOutsideTheStoreAndTheProject(t *testing.T) {
	dir := isolateStateDir(t)
	project := sampleProject(t)
	backend, cfg := sampleBackend(t, project)
	before := treeOf(t, project)

	app := startSession(t, backend, cfg, config.UserLayer{})
	pressKeys(app, "v a /")
	typeText(app, "api")
	pressKeys(app, "enter")

	after := treeOf(t, project)
	for _, path := range after {
		if !slices.Contains(before, path) {
			t.Errorf("the session wrote %s in the project", path)
		}
	}
	for _, path := range before {
		if !slices.Contains(after, path) {
			t.Errorf("the session changed or removed %s in the project", path)
		}
	}
	files := stateFiles(t, dir)
	if len(files) != 1 || !strings.HasSuffix(files[0], ".yml") {
		t.Fatalf("the state directory holds %v, want one .yml file", files)
	}
	data, err := os.ReadFile(filepath.Join(dir, files[0]))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"version: 2", "view: areas", "query: " + closedToken + " api"} {
		if !strings.Contains(string(data), line+"\n") {
			t.Errorf("the state file lacks %q:\n%s", line, data)
		}
	}
}

// A state directory inside the store or its project is neither read nor
// written, so a state file planted there does not apply. That holds when a
// symlink leads into the project from the state directory, from its nibs
// directory, or from the store to the state files.
func TestAStateDirectoryInsideTheProjectIsRefused(t *testing.T) {
	tests := []struct {
		name string
		// stateHome returns the directory the state variable names.
		stateHome func(t *testing.T, project, store string) string
		// files, when set, links the state files' directory and the store.
		files func(t *testing.T, files, store string)
		home  bool // the project is the user's home directory
	}{
		{name: "the store", stateHome: mkdir(func(_, store string) string { return store })},
		{name: "under the project", stateHome: mkdir(func(project, _ string) string { return filepath.Join(project, ".state") })},
		{
			name: "a link into the project",
			stateHome: func(t *testing.T, project, _ string) string {
				target := filepath.Join(project, ".state")
				if err := os.Mkdir(target, 0o755); err != nil {
					t.Fatal(err)
				}
				return symlink(t, target, filepath.Join(t.TempDir(), "state"))
			},
		},
		{name: "the store of a project in the home directory", stateHome: mkdir(func(_, store string) string { return filepath.Join(store, "state") }), home: true},
		{
			name:      "a store linked to the state files",
			stateHome: func(t *testing.T, _, _ string) string { return t.TempDir() },
			files: func(t *testing.T, files, store string) {
				if err := os.MkdirAll(files, 0o755); err != nil {
					t.Fatal(err)
				}
				symlink(t, files, store)
			},
		},
		{
			name:      "the state files linked to the store",
			stateHome: func(t *testing.T, _, _ string) string { return t.TempDir() },
			files: func(t *testing.T, files, store string) {
				if err := os.MkdirAll(filepath.Dir(files), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(store, 0o755); err != nil {
					t.Fatal(err)
				}
				symlink(t, store, files)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			project := t.TempDir()
			store := filepath.Join(project, ".nibs")
			if tt.home {
				t.Setenv("HOME", project)
				t.Setenv("USERPROFILE", project)
			}
			name, _ := stateDirVar(runtime.GOOS)
			t.Setenv(name, tt.stateHome(t, project, store))
			dir, err := userStateDir()
			if err != nil {
				t.Fatal(err)
			}
			files := filepath.Join(dir, "nibs")
			if tt.files != nil {
				tt.files(t, files, store)
			}
			if err := os.MkdirAll(store, 0o755); err != nil {
				t.Fatal(err)
			}
			writeFile(t, stateFilePath(resolvedPath(files), resolvedPath(store)), "version: 1\nview: areas\nquery: o\n")
			before := append(treeOf(t, project), treeOf(t, resolvedPath(store))...)

			app := sessionOn(t, store)

			if app.list.level != viewshape.LevelMilestones || app.list.query != closedToken {
				t.Errorf("the session read the planted state file: it opened on %q with query %q", app.list.level, app.list.query)
			}
			if !strings.Contains(app.list.statusMessage, "not remembered") {
				t.Errorf("the status line reads %q, want a warning that nothing is remembered", app.list.statusMessage)
			}
			pressKeys(app, "v f")
			if after := append(treeOf(t, project), treeOf(t, resolvedPath(store))...); !slices.Equal(after, before) {
				t.Errorf("the session changed the project or the store from %v to %v", before, after)
			}
		})
	}
}

// mkdir returns a stateHome for TestAStateDirectoryInsideTheProjectIsRefused
// that creates the directory path returns.
func mkdir(path func(project, store string) string) func(*testing.T, string, string) string {
	return func(t *testing.T, project, store string) string {
		dir := path(project, store)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		return dir
	}
}

// symlink creates link pointing at target and returns link.
func symlink(t *testing.T, target, link string) string {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		testskip.SymlinkUnavailable(t, err)
	}
	return link
}

// A store at ~/.nibs has the home directory as its project, which holds the
// default state directory, so the state is still remembered there.
func TestAStoreInTheHomeDirectoryIsRemembered(t *testing.T) {
	home := t.TempDir()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("USERPROFILE", home)
		t.Setenv("LocalAppData", filepath.Join(home, "AppData", "Local"))
	default:
		t.Setenv("HOME", home)
		t.Setenv("XDG_STATE_HOME", "")
	}
	store := filepath.Join(home, ".nibs")
	if err := os.Mkdir(store, 0o755); err != nil {
		t.Fatal(err)
	}
	first := sessionOn(t, store)
	pressKeys(first, "v a")
	if first.list.statusMessage != "" {
		t.Fatalf("the status line reads %q, want nothing", first.list.statusMessage)
	}

	if again := sessionOn(t, store); again.list.level != viewshape.LevelAreas || again.list.statusMessage != "" {
		t.Errorf("the relaunch opened on %q with status %q, want Areas and no warning", again.list.level, again.list.statusMessage)
	}
}

// treeOf returns every path under dir, relative to it, with the size of each
// file.
func treeOf(t *testing.T, dir string) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		if d.IsDir() {
			paths = append(paths, filepath.ToSlash(rel)+"/")
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		paths = append(paths, fmt.Sprintf("%s (%d bytes)", filepath.ToSlash(rel), info.Size()))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

// The first session saves the view twice, then folds ms1 under the cursor.
func TestFoldsAndTheCursorAreNotRemembered(t *testing.T) {
	isolateStateDir(t)
	root := newStoreDir(t)
	first := sessionOn(t, root)
	pressKeys(first, "v a v v")
	if !focusRow(first, "ms1") {
		t.Fatal("premise failed: could not focus ms1")
	}
	pressKeys(first, "left")
	if first.list.level != viewshape.LevelMilestones || !first.list.collapsed()["ms1"] {
		t.Fatalf("premise failed: ms1 is not folded in Milestones")
	}

	again := sessionOn(t, root)

	if len(again.list.collapsed()) != 0 {
		t.Errorf("the relaunch folds %v, want nothing", again.list.collapsed())
	}
	if got, first := again.list.focusedRowID(), rowIDs(again)[0]; got != first {
		t.Errorf("the relaunch's cursor is on %q, want the first row, %q", got, first)
	}
}

// writeFile writes content to path, creating its directory.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
