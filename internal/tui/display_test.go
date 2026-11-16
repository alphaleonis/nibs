package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/alphaleonis/nibs/internal/area"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/testdata/fixtures"
)

// hostile clears the screen and opens an OSC 8 hyperlink, neither of which is
// an SGR sequence the TUI's own styling writes.
const hostile = "\x1b[2J\x1b]8;;https://evil.example\x1b\\click\x1b]8;;\x1b\\"

// sgr matches the color and attribute sequences lipgloss writes.
var sgr = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

// requireNoInjectedEscape fails when view holds an ESC that is not part of an
// SGR sequence, or lacks want, the text that proves the nib was drawn at all.
func requireNoInjectedEscape(t *testing.T, where, view, want string) {
	t.Helper()
	rest := sgr.ReplaceAllString(view, "")
	if !strings.Contains(rest, want) {
		t.Fatalf("%s: premise failed, %q is not on screen:\n%s", where, want, view)
	}
	if i := strings.Index(rest, "\x1b"); i >= 0 {
		t.Errorf("%s draws a raw ESC from nib text: %q", where, rest[max(0, i-20):min(len(rest), i+40)])
	}
}

// hostileNibs are a milestone, an epic and the epic's child, with hostile in
// each title and in the child's tag, area, document and body.
func hostileNibs() []*nib.Nib {
	return []*nib.Nib{
		{ID: "evil-m", Title: "Milestone " + hostile, Status: "todo", Type: "milestone"},
		{ID: "evil-p", Title: "Parent " + hostile, Status: "todo", Type: "epic"},
		{
			ID: "evil-c", Title: "Child " + hostile, Status: "todo", Type: "task",
			Parent: "evil-p", Milestone: "evil-m", Area: "area" + hostile,
			Tags:      []string{"tag" + hostile},
			Documents: []string{"docs/" + hostile + ".md"},
			Body:      "Body " + hostile + "\n\n`span " + hostile + "`\n\n```\nfence " + hostile + "\n```\n",
		},
	}
}

func TestNibTextReachesNoViewAsAControlSequence(t *testing.T) {
	app, _ := setupTestApp(t, hostileNibs())
	toggleColumns(t, app, colParent, colMilestone, colArea)
	app.list.restoreCursor("evil-c")
	requireNoInjectedEscape(t, "the list", app.View().Content, "Child")

	pressKeys(app, "z p")
	if app.dockPlacement() == "" {
		t.Fatal("premise failed: z p did not dock the nib view")
	}
	// Moving onto the child is what loads it into the docked view.
	app.list.restoreCursor("evil-p")
	pressKeys(app, "down")
	requireNoInjectedEscape(t, "the docked nib view", app.View().Content, "Body")
	pressKeys(app, "z p")

	pickers := []struct {
		keys, close string
		state       viewState
		want        string // text of a nib that the picker draws
	}{
		{"s", "esc", viewStatusPicker, "Child"},
		{"p", "esc", viewParentPicker, "Parent"},
		{"b", "esc", viewBlockingPicker, "Parent"},
		{"m", "esc", viewModifyPicker, "Child"},
		{"a", "n", viewConfirmDialog, "Child"},
	}
	for _, p := range pickers {
		app.list.restoreCursor("evil-c")
		pressKeys(app, p.keys)
		if app.state != p.state {
			t.Fatalf("premise failed: %s opened state %v, want %v", p.keys, app.state, p.state)
		}
		requireNoInjectedEscape(t, "the picker of "+p.keys, app.View().Content, p.want)
		pressKeys(app, p.close)
	}

	app.list.restoreCursor("evil-c")
	pressKeys(app, "enter")
	if app.state != viewExpanded {
		t.Fatalf("premise failed: enter opened state %v, want the nib view", app.state)
	}
	requireNoInjectedEscape(t, "the nib view", app.View().Content, "Body")
	// The child's nib view lists its parent among its links.
	requireNoInjectedEscape(t, "the nib view's links", app.View().Content, "Parent")
}

// The TUI reads display copies, so a write that carried a copy's text would
// store the sanitized title.
func TestAStatusWriteKeepsTheTitleOnDisk(t *testing.T) {
	project := sampleProject(t)
	const id = "tnib-t001"
	path := filepath.Join(fixtures.NibsPath(project), "data", "tnib-t001--implement-bcrypt-hashing.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	title := regexp.MustCompile(`(?m)^title: .*$`)
	if !title.Match(data) {
		t.Fatalf("premise failed: %s has no title line", path)
	}
	const raw = "Hostile \x1b[2J title"
	if err := os.WriteFile(path, title.ReplaceAll(data, []byte(`title: "Hostile \e[2J title"`)), 0o644); err != nil {
		t.Fatal(err)
	}

	backend, cfg := sampleBackend(t, project)
	app := New(backend, cfg, config.UserLayer{}, "dev")
	initCmd := app.Init()
	app.Update(tea.WindowSizeMsg{Width: 120, Height: listHeight})
	processCmd(app, initCmd)
	if n, err := app.backend.GetNib(t.Context(), id); err != nil || n == nil || strings.Contains(n.Title, "\x1b") {
		t.Fatalf("premise failed: the TUI does not read %s with its title sanitized: %v, %v", id, n, err)
	}

	_, cmd := app.Update(statusSelectedMsg{nibIDs: []string{id}, status: "in-progress"})
	processCmd(app, cmd)

	reread := nibcore.New(fixtures.NibsPath(project), cfg)
	reread.SetWarnWriter(nil)
	if err := reread.Load(); err != nil {
		t.Fatal(err)
	}
	n, err := reread.Get(id)
	if err != nil || n == nil {
		t.Fatalf("%s does not read back after the status write: %v", id, err)
	}
	if n.Status != "in-progress" {
		t.Fatalf("premise failed: the status write did not land, status is %q", n.Status)
	}
	if n.Title != raw {
		t.Errorf("title on disk = %q, want %q", n.Title, raw)
	}
}

// Area is a key into the areas vocabulary, and areas.yml accepts a name that
// needs stripping, so a nib in such an area stays in that area's section.
func TestANibStaysInAnAreaWhoseNameNeedsStripping(t *testing.T) {
	const name = "ev\x1b[2Jil"
	vocab, err := area.Parse([]byte(fmt.Sprintf("areas:\n  - name: %q\n", name)))
	if err != nil || !vocab.Exists(name) {
		t.Fatalf("premise failed: areas.yml does not declare %q: %v", name, err)
	}
	app, stub := setupTestApp(t, []*nib.Nib{{ID: "a1", Title: "In the area", Status: "todo", Type: "task", Area: name}})
	stub.AreaVocab = vocab
	reload(app)
	pressKeys(app, "v a")
	if got, want := rowIDs(app), []string{sectionRowPrefix + name, "a1"}; !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
}

// A section header and an area cell draw an area's name the same way, so a
// name with a zero-width joiner keeps it in the header.
func TestAnAreaSectionKeepsAZeroWidthJoiner(t *testing.T) {
	const name = "dev\u200d"
	vocab, err := area.Parse([]byte(fmt.Sprintf("areas:\n  - name: %q\n", name)))
	if err != nil || !vocab.Exists(name) {
		t.Fatalf("premise failed: areas.yml does not declare %q: %v", name, err)
	}
	app, stub := setupTestApp(t, []*nib.Nib{{ID: "a1", Title: "In the area", Status: "todo", Type: "task", Area: name}})
	stub.AreaVocab = vocab
	reload(app)
	pressKeys(app, "v a")
	if !strings.Contains(plainView(app), name) {
		t.Errorf("no section header reads %q:\n%s", name, plainView(app))
	}
}

// A link that resolves to no nib is drawn as the stored id, which the display
// copy leaves raw.
func TestAnUnresolvedLinkIsDrawnStripped(t *testing.T) {
	app, _ := setupTestApp(t, []*nib.Nib{{
		ID: "d1", Title: "Dangling", Status: "todo", Type: "task",
		Parent: "dang" + hostile, Milestone: "mstone" + hostile,
	}})
	toggleColumns(t, app, colParent, colMilestone)
	requireNoInjectedEscape(t, "the parent cell", app.View().Content, "dang")
	requireNoInjectedEscape(t, "the milestone cell", app.View().Content, "mstone")
}

// evilID is the id of a nib file whose name holds an ESC sequence, which
// ParseFilename accepts.
const evilID = "tnib-\x1b[2Jevil"

// evilApp is an App on a copy of the sample fixture that holds a feature whose
// id is evilID, with link, a front-matter line such as "parent: tnib-e001".
func evilApp(t *testing.T, link string) *App {
	t.Helper()
	project := sampleProject(t)
	file := "---\nversion: 2\ntitle: Aaa hostile id\nstatus: todo\ntype: feature\n" + link + "\n---\n\nHostile body.\n"
	if err := os.WriteFile(filepath.Join(fixtures.NibsPath(project), "data", evilID+"--x.md"), []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	backend, cfg := sampleBackend(t, project)
	if n, err := backend.GetNib(t.Context(), evilID); err != nil || n == nil {
		t.Fatalf("premise failed: the store does not load %q: %v", evilID, err)
	}
	app := New(backend, cfg, config.UserLayer{}, "dev")
	initCmd := app.Init()
	app.Update(tea.WindowSizeMsg{Width: 140, Height: listHeight})
	processCmd(app, initCmd)
	return app
}

// openOn moves the cursor to id and presses keys, which must open state.
func openOn(t *testing.T, app *App, id, keys string, state viewState) {
	t.Helper()
	app.list.restoreCursor(id)
	if app.list.focusedID() != id {
		t.Fatalf("premise failed: the cursor is on %q, not on %q", app.list.focusedID(), id)
	}
	pressKeys(app, keys)
	if app.state != state {
		t.Fatalf("premise failed: %s on %q opened state %v, want %v", keys, id, app.state, state)
	}
}

func TestAnIDFromAFileNameIsDrawnStripped(t *testing.T) {
	app := evilApp(t, "parent: tnib-e001")
	var copied string
	orig := clipboardWriteAll
	t.Cleanup(func() { clipboardWriteAll = orig })
	clipboardWriteAll = func(s string) error { copied = s; return nil }

	openOn(t, app, evilID, "y", viewList)
	if copied != evilID {
		t.Errorf("y copied %q, want the stored id %q", copied, evilID)
	}
	requireNoInjectedEscape(t, "the ID column and the copy status line", app.View().Content, "Copied tnib-")

	pressKeys(app, "z p")
	_, cmd := app.Update(cursorChangedMsg{})
	processCmd(app, cmd)
	requireNoInjectedEscape(t, "the docked nib view", app.View().Content, "Hostile body.")
	pressKeys(app, "z p")

	// The status and modify pickers show the nib's id under its title, and the
	// parent and blocking pickers list it in a row.
	pickers := []struct {
		on, keys, filter string
		state            viewState
		want             string
	}{
		{evilID, "s", "", viewStatusPicker, "Aaa hostile id"},
		{evilID, "m", "", viewModifyPicker, "Aaa hostile id"},
		{"tnib-t005", "p", "/ A a a", viewParentPicker, "Aaa hostile id (tnib-"},
		{"tnib-t005", "b", "/ A a a", viewBlockingPicker, "Aaa hostile id (tnib-"},
	}
	for _, p := range pickers {
		openOn(t, app, p.on, p.keys, p.state)
		pressKeys(app, p.filter)
		requireNoInjectedEscape(t, "the picker of "+p.keys, app.View().Content, p.want)
		for range 3 {
			if app.state == viewList {
				break
			}
			pressKeys(app, "esc")
		}
	}

	openOn(t, app, evilID, "enter", viewExpanded)
	requireNoInjectedEscape(t, "the nib view's header", app.View().Content, "Hostile body.")
	pressKeys(app, "esc")

	openOn(t, app, "tnib-e001", "enter", viewExpanded)
	requireNoInjectedEscape(t, "the nib view's child link", app.View().Content, "Aaa hostile id")
}

// The tags column draws each tag whole, without fitText.
func TestTagsCellStripsEachTag(t *testing.T) {
	cell := tagsCell([]string{"ok", "tag" + hostile}, 80, tagStyle, "…")
	requireNoInjectedEscape(t, "the tags cell", cell, "tag")
}

// A backend refusal can quote a stored id, so the status line strips whatever
// message it draws. tnib-e001 is in milestone tnib-m001, so moving a nib of
// tnib-m002 under it is refused with a message that names the nib.
func TestAStatusLineQuotingAnIDFromAFileNameIsDrawnStripped(t *testing.T) {
	app := evilApp(t, "milestone: tnib-m002")
	refuse := parentSelectedMsg{nibIDs: []string{evilID}, parentID: "tnib-e001"}
	app.list.restoreCursor(evilID)
	_, cmd := app.Update(refuse)
	processCmd(app, cmd)
	if !strings.Contains(app.list.statusMessage, evilID) {
		t.Fatalf("premise failed: the refusal does not quote %q: %q", evilID, app.list.statusMessage)
	}
	requireNoInjectedEscape(t, "the list's status line", app.View().Content, "failed")

	openOn(t, app, evilID, "enter", viewExpanded)
	pressKeys(app, "p")
	if app.state != viewParentPicker {
		t.Fatalf("premise failed: p in the nib view opened state %v", app.state)
	}
	_, cmd = app.Update(refuse)
	processCmd(app, cmd)
	if app.state != viewExpanded || !strings.Contains(app.expanded.statusMessage, evilID) {
		t.Fatalf("premise failed: the nib view's status line does not quote %q: %q", evilID, app.expanded.statusMessage)
	}
	requireNoInjectedEscape(t, "the nib view's status line", app.View().Content, "failed")
}
