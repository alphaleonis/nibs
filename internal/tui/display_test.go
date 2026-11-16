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

// The tags column draws each tag whole, without fitText.
func TestTagsCellStripsEachTag(t *testing.T) {
	cell := tagsCell([]string{"ok", "tag" + hostile}, 80, tagStyle, "…", nil)
	requireNoInjectedEscape(t, "the tags cell", cell, "tag")
}
