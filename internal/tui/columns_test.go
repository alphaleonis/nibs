package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/charmbracelet/x/ansi"
	"gopkg.in/yaml.v3"
)

// commentedUserConfig is a user config whose comments a saved column choice
// must keep.
const commentedUserConfig = `# My nibs settings.
nibs:
  id_length: 4 # four is enough
tui:
  # Icons I like.
  icons: unicode
`

// userConfigFile writes text as a user config in a fresh directory and returns
// its path.
func userConfigFile(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nibs.yml")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// rowLine returns the screen line of id's row, without color.
func rowLine(t *testing.T, app *App, id string) string {
	t.Helper()
	for _, r := range visibleRows(t, app) {
		if r.item.nib.ID == id {
			return r.line
		}
	}
	t.Fatalf("no row of %s on screen", id)
	return ""
}

// savedColumns returns tui.columns.<view> as the file at path holds it.
func savedColumns(t *testing.T, path, view string) []string {
	t.Helper()
	var doc struct {
		TUI struct {
			Columns map[string][]string `yaml:"columns"`
		} `yaml:"tui"`
	}
	if err := yaml.Unmarshal([]byte(readFile(t, path)), &doc); err != nil {
		t.Fatalf("the user config does not parse: %v", err)
	}
	return doc.TUI.Columns[view]
}

// toggleColumns opens the column picker, presses the accelerator of each
// named column and confirms.
func toggleColumns(t *testing.T, app *App, names ...columnName) {
	t.Helper()
	pressKeys(app, "z c")
	if app.state != viewColumnPicker {
		t.Fatalf("z c opened state %v, want the column picker", app.state)
	}
	for _, name := range names {
		pressKeys(app, columnKey(t, app, name))
	}
	pressKeys(app, "enter")
}

// columnKey returns the accelerator the open column picker gives name.
func columnKey(t *testing.T, app *App, name columnName) string {
	t.Helper()
	for _, row := range app.columnPicker.rows {
		if row.name == name {
			if row.key == "" {
				t.Fatalf("the %s row has no accelerator", name)
			}
			return row.key
		}
	}
	t.Fatalf("the column picker has no %s row", name)
	return ""
}

// z c saves the choice into the user config, keeping its comments, and a TUI
// started from that file opens with the same columns.
func TestZCHidesTagsAndTheChoiceSurvivesARestart(t *testing.T) {
	path := userConfigFile(t, commentedUserConfig)
	app := sampleAppWithUser(t, 100, config.LoadUserLayerFrom(path))
	if line := rowLine(t, app, "tnib-t036"); !strings.Contains(line, "security") {
		t.Fatalf("premise failed: the tags column is not shown: %q", line)
	}

	toggleColumns(t, app, colTags)

	if app.state != viewList {
		t.Errorf("enter left state %v, want the list", app.state)
	}
	if line := rowLine(t, app, "tnib-t036"); strings.Contains(line, "security") {
		t.Errorf("the tags column is still shown: %q", line)
	}
	text := readFile(t, path)
	for _, comment := range []string{"# My nibs settings.", "# four is enough", "# Icons I like."} {
		if !strings.Contains(text, comment) {
			t.Errorf("the save dropped the comment %q:\n%s", comment, text)
		}
	}
	if got := savedColumns(t, path, "tree"); !slices.Equal(got, []string{"title", "id"}) {
		t.Errorf("tui.columns.tree = %v, want [title id]:\n%s", got, text)
	}

	// A change on disk reloads the nibs, not the columns.
	_, cmd := app.Update(nibsChangedMsg{})
	processCmd(app, cmd)
	if line := rowLine(t, app, "tnib-t036"); strings.Contains(line, "security") {
		t.Errorf("a reload brought the tags column back: %q", line)
	}

	restarted := sampleAppWithUser(t, 100, config.LoadUserLayerFrom(path))
	if line := rowLine(t, restarted, "tnib-t036"); strings.Contains(line, "security") {
		t.Errorf("after a restart the tags column is shown: %q", line)
	}
	if len(restarted.ignored) != 0 {
		t.Errorf("the saved file has ignored entries: %+v", restarted.ignored)
	}
}

func TestEscClosesTheColumnPickerWithoutSaving(t *testing.T) {
	path := userConfigFile(t, commentedUserConfig)
	app := sampleAppWithUser(t, 100, config.LoadUserLayerFrom(path))
	pressKeys(app, "z c")
	pressKeys(app, columnKey(t, app, colTags))
	pressKeys(app, "esc")
	if app.state != viewList {
		t.Errorf("esc left state %v, want the list", app.state)
	}
	if line := rowLine(t, app, "tnib-t036"); !strings.Contains(line, "security") {
		t.Errorf("esc applied the choice: %q", line)
	}
	if text := readFile(t, path); text != commentedUserConfig {
		t.Errorf("esc wrote the user config:\n%s", text)
	}
}

// A user config with an error is not written, and the choice still applies
// until the TUI quits.
func TestAColumnChoiceOverABrokenUserConfigWarnsAndApplies(t *testing.T) {
	const broken = "tui: [unclosed\n"
	path := userConfigFile(t, broken)
	user := config.LoadUserLayerFrom(path)
	if user.Failure == nil {
		t.Fatal("premise failed: the broken user config loaded")
	}
	app := sampleAppWithUser(t, 100, user)

	toggleColumns(t, app, colTags)

	if line := rowLine(t, app, "tnib-t036"); strings.Contains(line, "security") {
		t.Errorf("the choice did not apply: %q", line)
	}
	if msg := app.list.statusMessage; !strings.Contains(msg, "until you quit") || !strings.Contains(msg, "has an error") || app.list.statusKind != statusWarn {
		t.Errorf("status = %q (kind %v), want a warning with the refusal", msg, app.list.statusKind)
	}
	if text := readFile(t, path); text != broken {
		t.Errorf("the broken user config was written:\n%s", text)
	}
}

// A user config changed since the TUI read it is read again after the save,
// so its other settings follow the file and the session keeps its state.
func TestAColumnChoiceReloadsAUserConfigChangedSinceLoad(t *testing.T) {
	path := userConfigFile(t, commentedUserConfig)
	app := sampleAppWithUser(t, 100, config.LoadUserLayerFrom(path))
	if !focusOn(app, "tnib-f020") {
		t.Fatal("premise failed: could not focus tnib-f020")
	}
	pressKeys(app, "left")
	if !app.list.collapsed()["tnib-f020"] {
		t.Fatal("premise failed: left did not fold tnib-f020")
	}
	if err := os.WriteFile(path, []byte(commentedUserConfig+"  keys:\n    nib.create: N\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	toggleColumns(t, app, colTags)

	if got := app.keys.firstKey(actCreate); got != "N" {
		t.Errorf("nib.create is bound to %q, want the reloaded N", got)
	}
	if !app.list.collapsed()["tnib-f020"] {
		t.Error("the reload lost the fold")
	}
	if line := rowLine(t, app, "tnib-t036"); strings.Contains(line, "security") {
		t.Errorf("the reload brought the tags column back: %q", line)
	}
	text := readFile(t, path)
	if !strings.Contains(text, "nib.create: N") {
		t.Errorf("the save lost the change made since load:\n%s", text)
	}
}

// After a save the TUI holds the file as written, so a second save over it
// does not read it again.
func TestASaveOverTheFileAsWrittenDoesNotReloadIt(t *testing.T) {
	path := userConfigFile(t, commentedUserConfig)
	app := sampleAppWithUser(t, 100, config.LoadUserLayerFrom(path))
	toggleColumns(t, app, colTags)

	// A key the file does not bind, which a reload would undo.
	app.keys.actions = slices.Clone(app.keys.actions)
	findAction(app.keys.actions, actCreate).Keys = []string{"x"}
	toggleColumns(t, app, colTags)

	if got := app.keys.firstKey(actCreate); got != "x" {
		t.Errorf("the second save reloaded the user config: nib.create is bound to %q", got)
	}
	if got := savedColumns(t, path, "tree"); !slices.Equal(got, []string{"title", "tags", "id"}) {
		t.Errorf("tui.columns.tree = %v, want [title tags id]", got)
	}
}

func TestUnusableColumnEntriesAreIgnoredWithTheirLines(t *testing.T) {
	text := "tui:\n  columns:\n    tree: [tags, bogus, id]\n    nowhere: [tags]\n"
	app := setupTestAppWithUserConfig(t, listKeysNibs(), config.Default(), text)
	want := []ignoredEntry{
		{entry: "columns.tree: bogus", line: 3, reason: "no column has this name"},
		{entry: "columns.nowhere: [tags]", line: 4, reason: "no view has this name"},
	}
	if !slices.Equal(app.ignored, want) {
		t.Errorf("ignored = %+v, want %+v", app.ignored, want)
	}
	if !slices.Equal(app.list.columns, []columnName{colTags, colID}) {
		t.Errorf("columns = %v, want the known names [tags id]", app.list.columns)
	}
	if footer := footerLine(app); !strings.Contains(footer, "Ignored 2 TUI settings") {
		t.Errorf("footer = %q, want the warning", footer)
	}
}

func TestColumnsOfTheWrongShapeAreIgnored(t *testing.T) {
	tests := []struct {
		text  string
		entry string
		line  int
	}{
		{"tui:\n  columns: [tags]\n", "tui.columns: [tags]", 2},
		{"tui:\n  columns:\n    tree: tags\n", "columns.tree: tags", 3},
		{"tui:\n  columns:\n    tree: [[tags]]\n", "columns.tree: [tags]", 3},
	}
	for _, tt := range tests {
		app := setupTestAppWithUserConfig(t, listKeysNibs(), config.Default(), tt.text)
		if len(app.ignored) != 1 || app.ignored[0].entry != tt.entry || app.ignored[0].line != tt.line {
			t.Errorf("%q: ignored %+v, want %q on line %d", tt.text, app.ignored, tt.entry, tt.line)
		}
	}
}

// columnNibs give every column a value on the row of c-kid, except children.
func columnNibs() []*nib.Nib {
	created := columnTestNow.Add(-72 * time.Hour)
	updated := columnTestNow.Add(-2 * time.Hour)
	return []*nib.Nib{
		{ID: "c-ms", Title: "Launch", Type: "milestone", Status: "todo"},
		{ID: "c-par", Title: "Payments rework", Type: "epic", Status: "in-progress"},
		{ID: "c-kid", Title: "Charge cards", Type: "task", Status: "in-progress", Parent: "c-par",
			Priority: "high", Estimate: "m", Tags: []string{"ux"}, Milestone: "c-ms", Area: "web/ui",
			BlockedBy: []string{"c-blk"}, CreatedAt: &created, UpdatedAt: &updated},
		{ID: "c-done", Title: "Refunds", Type: "task", Status: "completed", Parent: "c-par"},
		{ID: "c-drop", Title: "Cheques", Type: "task", Status: "scrapped", Parent: "c-par"},
		{ID: "c-blk", Title: "Card vault", Type: "task", Status: "todo"},
		{ID: "c-dep", Title: "Receipts", Type: "task", Status: "todo", BlockedBy: []string{"c-kid"}},
	}
}

var columnTestNow = time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

// allColumnsConfig shows every column in the tree view.
const allColumnsConfig = "tui:\n  columns:\n    tree: [id, parent, type, title, status, priority, estimate, children, tags, milestone, area, blocking, blockedBy, created, modified]\n"

// Every column after the title keeps its width while the terminal widens, and
// the title takes the difference.
func TestEveryColumnHasAFixedWidthAndTheTitleFlexes(t *testing.T) {
	forceUTF8(t, true)
	saved := timeNow
	timeNow = func() time.Time { return columnTestNow }
	t.Cleanup(func() { timeNow = saved })

	// Each column's cell on c-kid's row, in the order the row draws them, with
	// the width the cell takes.
	cells := []struct {
		name  columnName
		width int
		text  string
	}{
		{colParent, 20, "Payments rework"},
		{colType, 9, "task"},
		{colStatus, 11, "in-progress"},
		{colPriority, 8, "high"},
		{colEstimate, 2, "M"},
		{colChildren, 7, ""},
		{colTags, 20, "ux"},
		{colMilestone, 20, "Launch"},
		{colArea, 16, "web/ui"},
		{colBlocking, 4, "◆1"},
		{colBlockedBy, 4, "●1"},
		{colCreated, 8, "3d ago"},
		{colModified, 8, "2h ago"},
		{colID, 6, "c-kid"},
	}
	var after string
	for _, width := range []int{220, 260} {
		app := setupTestAppWithUserConfig(t, columnNibs(), config.Default(), allColumnsConfig)
		app.Update(tea.WindowSizeMsg{Width: width, Height: 40})
		line := rowLine(t, app, "c-kid")
		if got := lipgloss.Width(strings.TrimRight(line, " ")); got > width {
			t.Fatalf("at %d columns the row is %d cells wide: %q", width, got, line)
		}
		end := width
		var childrenCell [2]int
		for i := len(cells) - 1; i >= 0; i-- {
			c := cells[i]
			start := end - c.width
			if got := strings.TrimRight(ansi.Cut(line, start, end), " "); got != c.text {
				t.Errorf("at %d columns the %s cell [%d, %d) = %q, want %q: %q", width, c.name, start, end, got, c.text, line)
			}
			if sep := ansi.Cut(line, start-1, start); sep != " " {
				t.Errorf("at %d columns no space precedes the %s cell: %q", width, c.name, line)
			}
			if c.name == colChildren {
				childrenCell = [2]int{start, end}
			}
			end = start - 1
		}
		if title := strings.TrimSpace(ansi.Cut(line, rowLeadWidth, end)); !strings.HasSuffix(title, "Charge cards") {
			t.Errorf("at %d columns the title cell [%d, %d) = %q", width, rowLeadWidth, end, title)
		}
		tail := ansi.Cut(line, end, width)
		if after != "" && tail != after {
			t.Errorf("widening the terminal changed the columns:\n%q\n%q", after, tail)
		}
		after = tail

		// c-par has a completed, an in-progress and a scrapped child, and a
		// scrapped child is not counted; see progress.ByCount.
		parent := rowLine(t, app, "c-par")
		if got := strings.TrimSpace(ansi.Cut(parent, childrenCell[0], childrenCell[1])); got != "1/2" {
			t.Errorf("at %d columns the children cell of c-par = %q, want 1/2: %q", width, got, parent)
		}
	}
}

// Columns that do not leave the title its room drop from the right, and the
// ID stays.
func TestColumnsThatDoNotFitDropFromTheRight(t *testing.T) {
	text := "tui:\n  columns:\n    tree: [tags, milestone, area, id]\n"
	tests := []struct {
		width int
		shown []string
		gone  []string
	}{
		{80, []string{"ux", "c-kid"}, []string{"Launch", "web/ui"}},
		{100, []string{"ux", "Launch", "c-kid"}, []string{"web/ui"}},
		{140, []string{"ux", "Launch", "web/ui", "c-kid"}, nil},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.width), func(t *testing.T) {
			app := setupTestAppWithUserConfig(t, columnNibs(), config.Default(), text)
			app.Update(tea.WindowSizeMsg{Width: tt.width, Height: 40})
			line := rowLine(t, app, "c-kid")
			for _, s := range tt.shown {
				if !strings.Contains(line, s) {
					t.Errorf("the row lacks %q: %q", s, line)
				}
			}
			for _, s := range tt.gone {
				if strings.Contains(line, s) {
					t.Errorf("the row shows %q, which should have dropped: %q", s, line)
				}
			}
			if !strings.HasSuffix(strings.TrimRight(line, " "), " c-kid") {
				t.Errorf("the ID is not last: %q", line)
			}
		})
	}
}

// The column picker keeps one line per column on an 80x24 terminal and on the
// narrowest one its modal fits, so its bottom border and hint row stay on
// screen.
func TestTheColumnPickerFitsTheScreen(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {50, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			app := sampleApp(t, size[0])
			app.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			pressKeys(app, "z c")
			lines := strings.Split(plainView(app), "\n")
			top := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "╭") })
			bottom := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "╰") })
			if top < 0 || bottom < 0 {
				t.Fatalf("the modal's borders are not both on screen:\n%s", strings.Join(lines, "\n"))
			}
			// The header, a blank line, one line per column, a blank line and the hints.
			if got, want := bottom-top-1, 2+len(columnOrder)+2; got != want {
				t.Errorf("the modal holds %d lines, want %d:\n%s", got, want, strings.Join(lines, "\n"))
			}
			if hints := lines[bottom-1]; !strings.Contains(hints, "enter apply") {
				t.Errorf("the line above the bottom border is %q, want the hint row", hints)
			}
		})
	}
}
