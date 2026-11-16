package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/testdata/fixtures"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"
)

// listWidths are the terminal widths the list's layout is pinned at.
var listWidths = []int{80, 100, 140}

const listHeight = 24

// sampleApp is an App on a copy of the sample fixture, sized width x
// listHeight. The copy sits in a directory named sample-project, so the top
// rule names the same project on every machine.
func sampleApp(t *testing.T, width int) *App {
	t.Helper()
	return sampleAppWithUser(t, width, config.UserLayer{})
}

// sampleAppWithUser is sampleApp with user as the user config.
func sampleAppWithUser(t *testing.T, width int, user config.UserLayer) *App {
	t.Helper()
	backend, cfg := sampleBackend(t, sampleProject(t))
	app := New(backend, cfg, user, "dev")
	initCmd := app.Init()
	app.Update(tea.WindowSizeMsg{Width: width, Height: listHeight})
	processCmd(app, initCmd)
	return app
}

// sampleProject returns a copy of the sample fixture's project, in a
// directory named sample-project.
func sampleProject(t *testing.T) string {
	t.Helper()
	project := filepath.Join(t.TempDir(), "sample-project")
	if err := os.Rename(fixtures.CopySampleProject(t), project); err != nil {
		t.Fatalf("moving the fixture copy: %v", err)
	}
	return project
}

// sampleBackend returns a loaded backend on the store of project, and the
// store's config.
func sampleBackend(t *testing.T, project string) (Backend, *config.Config) {
	t.Helper()
	storeDir := fixtures.NibsPath(project)
	cfg, err := config.LoadFromStore(storeDir)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	core := nibcore.New(storeDir, cfg)
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("load core: %v", err)
	}
	resolver := &graph.Resolver{Reader: core, Writer: core, Validator: core, Blocking: core, Orderer: graph.NewOrderer(core, core)}
	return NewRealBackend(core, resolver), cfg
}

// plainView is the screen as text, with every color escape removed.
func plainView(app *App) string {
	return ansi.Strip(app.View().Content)
}

// The list's whole screen at each width, on the sample fixture, without
// color. Regenerate with `-update`.
func TestListViewGolden(t *testing.T) {
	for _, width := range listWidths {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			golden.RequireEqual(t, plainView(sampleApp(t, width)))
		})
	}
}

// viewRow is one nib's row on screen.
type viewRow struct {
	item nibItem
	line string // without color
}

// visibleRows pairs the nib rows on the list's current page with their lines
// on screen, which start under the top rule and the header.
func visibleRows(t *testing.T, app *App) []viewRow {
	t.Helper()
	lines := strings.Split(plainView(app), "\n")
	l := app.list.list
	start, end := l.Paginator.GetSliceBounds(len(l.VisibleItems()))
	var rows []viewRow
	for i, item := range l.VisibleItems()[start:end] {
		if item, ok := item.(nibItem); ok {
			rows = append(rows, viewRow{item: item, line: lines[2+i]})
		}
	}
	if len(rows) == 0 {
		t.Fatal("premise failed: the list shows no rows")
	}
	return rows
}

// cellOf returns the cell at which sub starts in line, or -1 when line does
// not hold it.
func cellOf(line, sub string) int {
	i := strings.Index(line, sub)
	if i < 0 {
		return -1
	}
	return lipgloss.Width(line[:i])
}

func TestATopLevelTitleStartsBeforeCell15(t *testing.T) {
	for _, width := range listWidths {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			app := sampleApp(t, width)
			for _, r := range visibleRows(t, app) {
				if strings.ContainsAny(r.item.treePrefix, "│├└|+\\") {
					continue
				}
				// A long title is cut, so look for its start.
				head := r.item.nib.Title[:min(len(r.item.nib.Title), 12)]
				if cell := cellOf(r.line, head); cell < 0 || cell >= 15 {
					t.Errorf("the title of %s starts at cell %d: %q", r.item.nib.ID, cell, r.line)
				}
			}
		})
	}
}

func TestRowsHaveNoSideBorders(t *testing.T) {
	for _, width := range listWidths {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			for i, line := range strings.Split(plainView(sampleApp(t, width)), "\n") {
				if line == "" {
					continue
				}
				first, last := []rune(line)[0], []rune(line)[len([]rune(line))-1]
				if strings.ContainsRune("│|╭╰┃", first) || strings.ContainsRune("│|╮╯┃", last) {
					t.Errorf("line %d is framed by a side border: %q", i+1, line)
				}
			}
		})
	}
}

func TestTheIDIsLastAndShort(t *testing.T) {
	for _, width := range listWidths {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			app := sampleApp(t, width)
			for _, r := range visibleRows(t, app) {
				id := r.item.nib.ID
				short := strings.TrimPrefix(id, "tnib-")
				if !strings.HasSuffix(strings.TrimRight(r.line, " "), " "+short) || strings.Contains(r.line, id) {
					t.Errorf("the row of %s does not end with its short ID %q: %q", id, short, r.line)
				}
			}
		})
	}
}

// The ID is drawn in the terminal's normal color, and the type icon in its
// type's color even on the row under the cursor.
func TestTheCursorRowKeepsItsColors(t *testing.T) {
	app := sampleApp(t, 100)
	n := app.list.focusedNib()
	if n == nil {
		t.Fatal("premise failed: no nib under the cursor")
	}
	raw := strings.Split(app.View().Content, "\n")[2]
	if !strings.HasSuffix(raw, " "+app.display.id(n.ID)) {
		t.Errorf("the ID is not drawn plain at the end of the row: %q", raw)
	}
	if icon := app.display.icons.renderType(app.config, n.EffectiveType()); !strings.Contains(raw, icon) {
		t.Errorf("the type icon %q is not in its type's color on the cursor row: %q", icon, raw)
	}
}

func TestTheHeaderShowsTheFilterAndTheCount(t *testing.T) {
	app := sampleApp(t, 100)
	header := func() string { return strings.Split(plainView(app), "\n")[1] }
	matched := func() int {
		n := 0
		for _, item := range app.list.list.VisibleItems() {
			if item.(nibItem).matched {
				n++
			}
		}
		return n
	}

	if h := header(); !strings.HasPrefix(h, "Tree  -status:closed") || !strings.HasSuffix(h, fmt.Sprintf("%d nibs", matched())) {
		t.Errorf("the default header = %q, want the view, -status:closed and %d nibs", h, matched())
	}

	pressKeys(app, "z h")
	if h := header(); strings.Contains(h, "status:closed") || !strings.HasSuffix(h, fmt.Sprintf("%d nibs", matched())) {
		t.Errorf("with closed nibs shown the header = %q, want no token and %d nibs", h, matched())
	}

	pressKeys(app, "/")
	typeText(app, "tags:ux")
	pressKeys(app, "enter")
	if h := header(); !strings.Contains(h, "tags:ux") || !strings.HasSuffix(h, fmt.Sprintf("%d nibs", matched())) {
		t.Errorf("with a tag token the header = %q, want tags:ux and %d nibs", h, matched())
	}
}

func TestTheBottomRuleShowsTheRowPosition(t *testing.T) {
	app := sampleApp(t, 100)
	total := len(app.list.list.VisibleItems())
	rule := func() string {
		lines := strings.Split(plainView(app), "\n")
		return lines[len(lines)-2]
	}
	if want := fmt.Sprintf(" 1 / %d ", total); !strings.Contains(rule(), want) {
		t.Errorf("the rule = %q, want %q", rule(), want)
	}
	pressKeys(app, "j j j j j j")
	if want := fmt.Sprintf(" 7 / %d ", total); !strings.Contains(rule(), want) {
		t.Errorf("the rule after six moves = %q, want %q", rule(), want)
	}
}

func TestZPTogglesTheNibView(t *testing.T) {
	app := sampleApp(t, 140)
	n := app.list.focusedNib()
	meta := "Status " + app.display.icons.status(n.Status) + " " + n.Status
	if strings.Contains(plainView(app), meta) {
		t.Fatalf("the nib view is shown before z p:\n%s", plainView(app))
	}
	pressKeys(app, "z p")
	if !strings.Contains(plainView(app), meta) {
		t.Errorf("z p did not show the nib view:\n%s", plainView(app))
	}
	pressKeys(app, "z p")
	if strings.Contains(plainView(app), meta) {
		t.Errorf("a second z p did not hide the nib view:\n%s", plainView(app))
	}
}

func TestWDoesNothing(t *testing.T) {
	app := sampleApp(t, 140)
	before := app.View().Content
	pressKeys(app, "W")
	if after := app.View().Content; after != before {
		t.Errorf("W changed the screen:\n%s", ansi.Strip(after))
	}
}

// A reload from disk keeps the display settings the list was given.
func TestAReloadKeepsTheDisplay(t *testing.T) {
	app := sampleApp(t, 100)
	before := plainView(app)
	_, cmd := app.Update(nibsChangedMsg{})
	processCmd(app, cmd)
	if after := plainView(app); after != before {
		t.Errorf("the screen changed after a reload:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// headerCount reads the match count off the header line.
func headerCount(t *testing.T, app *App) string {
	t.Helper()
	fields := strings.Fields(strings.Split(plainView(app), "\n")[1])
	if len(fields) < 2 || !strings.HasPrefix(fields[len(fields)-1], "nib") {
		t.Fatalf("the header has no count: %v", fields)
	}
	return fields[len(fields)-2]
}

// Folding a parent hides rows, not matches.
func TestFoldingKeepsTheMatchCount(t *testing.T) {
	app := sampleApp(t, 100)
	before := headerCount(t, app)
	if !focusOn(app, "tnib-f020") {
		t.Fatal("premise failed: could not focus tnib-f020")
	}
	pressKeys(app, "left")
	if !app.list.collapsed()["tnib-f020"] {
		t.Fatal("premise failed: left did not fold tnib-f020")
	}
	if after := headerCount(t, app); after != before {
		t.Errorf("folding a parent changed the count from %s to %s", before, after)
	}
}

// typeText types each rune of s into the app.
func typeText(app *App, s string) {
	for _, r := range s {
		sendKeyNonBlocking(app, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// While a filter is typed the header keeps the end of the query in view: in
// the full width with the count, and in the list pane beside the nib view
// without it.
func TestTheFilterInputFitsTheHeader(t *testing.T) {
	// Too long for an 80-column header with a box as wide as the pane.
	query := strings.Repeat("implement batch ", 3) + "endpoint x"
	tests := []struct {
		name      string
		width     int
		nibView   bool
		pane      int
		wantCount bool
	}{
		{"full width", 80, false, 80, true},
		{"beside the nib view", 100, true, 39, true},
		{"too narrow for the count", 26, false, 26, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := sampleApp(t, tt.width)
			if tt.nibView {
				pressKeys(app, "z p")
			}
			pressKeys(app, "/")
			typeText(app, query)
			line := strings.Split(app.View().Content, "\n")[1]
			header := ansi.Strip(ansi.Truncate(line, tt.pane, ""))
			if !strings.Contains(header, "endpoint x") {
				t.Errorf("the end of the query is not in the %d-cell header: %q", tt.pane, header)
			}
			if hasCount := strings.HasSuffix(strings.TrimRight(header, " "), "nibs") || strings.HasSuffix(strings.TrimRight(header, " "), "nib"); hasCount != tt.wantCount {
				t.Errorf("the header shows a count = %v, want %v: %q", hasCount, tt.wantCount, header)
			}
		})
	}
}

// A query that matches nothing keeps its "0 nibs", also when the unfiltered
// count is "1 nib", one cell narrower.
func TestAQueryMatchingNothingKeepsItsCount(t *testing.T) {
	app := setupRealBackendApp(t, []*nib.Nib{{ID: "n1", Title: "Only one", Type: "task", Status: "todo"}})
	app.Update(tea.WindowSizeMsg{Width: 80, Height: listHeight})
	pressKeys(app, "/")
	typeText(app, "qqq")
	header := strings.TrimRight(strings.Split(plainView(app), "\n")[1], " ")
	if !strings.HasSuffix(header, "0 nibs") {
		t.Errorf("the header lost its count: %q", header)
	}
}
