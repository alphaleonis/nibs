package tui

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/ui"
	"github.com/charmbracelet/x/ansi"
)

// click sends a left click on cell (x, y), as a terminal reports one, and runs
// what it returns.
func click(app *App, x, y int) {
	_, cmd := app.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	processCmdNonBlocking(app, cmd)
}

// turnWheel sends one step of the wheel, down or up, with the pointer on cell
// (x, y).
func turnWheel(app *App, x, y int, down bool) {
	button := tea.MouseWheelUp
	if down {
		button = tea.MouseWheelDown
	}
	_, cmd := app.Update(tea.MouseWheelMsg{X: x, Y: y, Button: button})
	processCmdNonBlocking(app, cmd)
}

// onScreen returns the cell where text first starts on the screen: its column
// and its line.
func onScreen(t *testing.T, app *App, text string) (x, y int) {
	t.Helper()
	for y, line := range strings.Split(plainView(app), "\n") {
		if x := cellOf(line, text); x >= 0 {
			return x, y
		}
	}
	t.Fatalf("premise failed: the screen does not show %q:\n%s", text, plainView(app))
	return 0, 0
}

// dockedPane returns the first cell of the docked nib view on the screen, as
// drawn: the divider's column for a view docked right, or the line of the
// view's top rule for one docked below.
func dockedPane(t *testing.T, app *App) (x, y int) {
	t.Helper()
	lines := strings.Split(plainView(app), "\n")
	if x := cellOf(lines[0], dividerGlyph(false)); x >= 0 {
		return x + 1, 0
	}
	for y, line := range lines[1:] {
		if regexpLine(line, `^─+ \pL`) {
			return 0, y + 1
		}
	}
	t.Fatalf("premise failed: no docked nib view on the screen:\n%s", plainView(app))
	return 0, 0
}

// inPane returns the cell where text first starts in the list pane, or with
// docked set in the docked nib view: its column and its line.
func inPane(t *testing.T, app *App, text string, docked bool) (x, y int) {
	t.Helper()
	paneX, paneY := dockedPane(t, app)
	for y, line := range strings.Split(plainView(app), "\n") {
		for from := 0; ; {
			i := strings.Index(line[from:], text)
			if i < 0 {
				break
			}
			x := lipgloss.Width(line[:from+i])
			if (x >= paneX && y >= paneY) == docked {
				return x, y
			}
			from += i + len(text)
		}
	}
	t.Fatalf("premise failed: the %s does not show %q:\n%s", map[bool]string{false: "list", true: "nib view"}[docked], text, plainView(app))
	return 0, 0
}

// pagedNibs are n top-level tasks titled "Row 00" on, with ids row-00 on.
func pagedNibs(n int) []*nib.Nib {
	var nibs []*nib.Nib
	for i := range n {
		nibs = append(nibs, &nib.Nib{ID: fmt.Sprintf("row-%02d", i), Title: fmt.Sprintf("Row %02d", i), Type: "task", Status: "todo", Order: fmt.Sprintf("%02d", i)})
	}
	return nibs
}

func TestMouseReportingIsOnUnlessTheUserConfigTurnsItOff(t *testing.T) {
	tests := []struct {
		name, text string
		want       tea.MouseMode
	}{
		{"no setting", "nibs:\n  id_length: 4\n", tea.MouseModeCellMotion},
		{"on", "tui:\n  mouse: true\n", tea.MouseModeCellMotion},
		{"off", "tui:\n  mouse: false\n", tea.MouseModeNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := setupTestAppWithUserConfig(t, listKeysNibs(), config.Default(), tt.text)
			if got := app.View().MouseMode; got != tt.want {
				t.Errorf("mouse mode = %v, want %v", got, tt.want)
			}
			if len(app.ignored) != 0 {
				t.Errorf("ignored %+v, want none", app.ignored)
			}
		})
	}
}

// A click that arrives with reporting off, as one a terminal queued before the
// switch can, does nothing.
func TestWithTheMouseOffAClickDoesNothing(t *testing.T) {
	app := setupTestAppWithUserConfig(t, listKeysNibs(), config.Default(), "tui:\n  mouse: false\n")
	x, y := onScreen(t, app, "Last")
	click(app, x, y)
	if got := selectedID(app); got != "nib-1" {
		t.Errorf("the cursor is on %s after a click with the mouse off, want nib-1", got)
	}
}

func TestAnUnusableMouseSettingKeepsTheMouseOnAndWarns(t *testing.T) {
	app := setupTestAppWithUserConfig(t, listKeysNibs(), config.Default(), "tui:\n  icons: unicode\n  mouse: maybe\n")
	if got := app.View().MouseMode; got != tea.MouseModeCellMotion {
		t.Errorf("mouse mode = %v, want cell motion", got)
	}
	if len(app.ignored) != 1 || app.ignored[0].entry != "mouse: maybe" || app.ignored[0].reason != "is not true or false" || app.ignored[0].line != 3 {
		t.Fatalf("ignored %+v, want mouse: maybe on line 3: is not true or false", app.ignored)
	}
	if footer := footerLine(app); !strings.Contains(footer, "Ignored 1 TUI setting in the user config") {
		t.Errorf("footer = %q, want the ignored-setting warning", footer)
	}
}

func TestAClickOnARowMovesTheCursorThere(t *testing.T) {
	t.Run("tree", func(t *testing.T) {
		app, _ := setupTestApp(t, listKeysNibs())
		x, y := onScreen(t, app, "Last")
		click(app, x, y)
		if got := selectedID(app); got != "nib-4" {
			t.Errorf("the cursor is on %s, want nib-4", got)
		}
	})

	t.Run("a later page", func(t *testing.T) {
		app, _ := setupTestApp(t, pagedNibs(60))
		pressKeys(app, "end")
		x, y := onScreen(t, app, "Row 40")
		click(app, x, y)
		if got := selectedID(app); got != "row-40" {
			t.Errorf("the cursor is on %s, want row-40", got)
		}
	})

	t.Run("the filter box open", func(t *testing.T) {
		app, _ := setupTestApp(t, pagedNibs(10))
		pressKeys(app, "/")
		if !app.list.editing {
			t.Fatal("premise failed: / did not open the filter box")
		}
		x, y := onScreen(t, app, "Row 07")
		click(app, x, y)
		if got := selectedID(app); got != "row-07" || !app.list.editing {
			t.Errorf("the cursor is on %s, editing %v; want row-07 with the box still open", got, app.list.editing)
		}
	})

	t.Run("milestones", func(t *testing.T) {
		app, _ := setupGroupedApp(t)
		pressKeys(app, "v m")
		x, y := onScreen(t, app, "Three")
		click(app, x, y)
		if got := app.list.focusedRowID(); got != "t3" {
			t.Errorf("the cursor is on %s, want t3", got)
		}
		x, y = onScreen(t, app, "Backlog")
		click(app, x, y)
		if got := app.list.focusedRowID(); got != backlogRow {
			t.Errorf("the cursor is on %s, want the Backlog row", got)
		}
	})
}

// A click below the last row, on the rules or on the header moves nothing.
func TestAClickOffTheRowsLeavesTheCursor(t *testing.T) {
	app, _ := setupTestApp(t, listKeysNibs())
	pressKeys(app, "down")
	lines := strings.Split(plainView(app), "\n")
	_, last := onScreen(t, app, "Last")
	for _, y := range []int{0, 1, last + 1, len(lines) - 1} {
		click(app, 10, y)
		if got := selectedID(app); got != "nib-2" {
			t.Errorf("a click on line %d moved the cursor to %s", y, got)
		}
	}
}

// dockPlacements are a terminal size for each placement auto docks the nib
// view at.
var dockPlacements = []struct {
	name          string
	width, height int
	want          placement
}{
	{"right", 140, 40, placementRight},
	{"below", 100, 40, placementBelow},
}

func TestTheWheelScrollsThePaneUnderThePointer(t *testing.T) {
	for _, tc := range dockPlacements {
		t.Run(tc.name, func(t *testing.T) {
			app := dockedApp(t, "rz", tc.width, tc.height)
			if got := app.dockPlacement(); got != tc.want {
				t.Fatalf("premise failed: the nib view docked %q, want %q", got, tc.want)
			}

			x, y := inPane(t, app, "Dependent bug", false)
			turnWheel(app, x, y, true)
			if got := selectedID(app); got != "long" {
				t.Fatalf("the cursor is on %s after the wheel over the list, want long", got)
			}

			x, y = inPane(t, app, "Line 0 of the long body", true)
			turnWheel(app, x, y, true)
			if app.docked.body.YOffset() == 0 {
				t.Error("the wheel over the nib view did not scroll its body")
			}
			if got := selectedID(app); got != "long" || app.docked.active {
				t.Errorf("the wheel over the nib view moved the cursor to %s or focused the view (%v)", got, app.docked.active)
			}

			x, y = inPane(t, app, "Dependent bug", false)
			turnWheel(app, x, y, false)
			if got := selectedID(app); got != "rz" {
				t.Errorf("the cursor is on %s after the wheel up over the list, want rz", got)
			}
		})
	}
}

func TestTheWheelScrollsTheExpandedBody(t *testing.T) {
	app := expandOn(t, dockNibs(), "long", 120, 40)
	x, y := onScreen(t, app, "Line 0 of the long body")
	turnWheel(app, x, y, true)
	if got := app.expanded.body.YOffset(); got == 0 {
		t.Error("the wheel over the expanded body did not scroll it")
	}
}

// hubDocument is the document hub names, a relations entry no click follows.
const hubDocument = "docs/nib-view.md"

func TestAClickFocusesThePaneOrRegionUnderIt(t *testing.T) {
	for _, tc := range dockPlacements {
		t.Run(tc.name, func(t *testing.T) {
			app := dockedApp(t, "hub", tc.width, tc.height)

			x, y := inPane(t, app, bodyMarker, true)
			click(app, x, y)
			if !app.docked.active || app.docked.focused() != regionBody {
				t.Errorf("a click on the body left active %v, region %d; want the body focused", app.docked.active, app.docked.focused())
			}

			x, y = inPane(t, app, hubDocument, true)
			click(app, x, y)
			if !app.docked.active || app.docked.focused() != regionRelations {
				t.Errorf("a click on a document left active %v, region %d; want the relations focused", app.docked.active, app.docked.focused())
			}
			if es := app.docked.entries(); app.docked.cursor >= len(es) || es[app.docked.cursor].doc != hubDocument {
				t.Errorf("the relations cursor is on entry %d, want %s", app.docked.cursor, hubDocument)
			}
			if got := selectedID(app); got != "hub" {
				t.Errorf("a click in the nib view moved the list cursor to %s", got)
			}

			x, y = inPane(t, app, "Lone task", false)
			click(app, x, y)
			if app.docked.active || selectedID(app) != "rz" {
				t.Errorf("a click on a list row left the view active %v and the cursor on %s; want the list focused on rz", app.docked.active, selectedID(app))
			}
		})
	}
}

// At 140 columns the view docked right stacks its relations over its body,
// with a rule between them. The divider and the rule are not the body.
func TestAClickAtTheEdgesOfTheDockedViewHitsWhatIsDrawnThere(t *testing.T) {
	app := dockedApp(t, "hub", 140, 40)
	paneX, _ := dockedPane(t, app)
	x, y := inPane(t, app, hubDocument, true)
	click(app, x, y)
	if !app.docked.active || app.docked.focused() != regionRelations {
		t.Fatalf("premise failed: a click on a document left active %v, region %d", app.docked.active, app.docked.focused())
	}

	_, bodyY := inPane(t, app, bodyMarker, true)
	lines := strings.Split(plainView(app), "\n")
	ruleY := bodyY - 1
	for ruleY > 0 && ansi.Cut(lines[ruleY], paneX, paneX+3) != "───" {
		ruleY--
	}
	for _, cell := range [][2]int{{paneX - 1, bodyY}, {paneX, ruleY}} {
		click(app, cell[0], cell[1])
		if !app.docked.active || app.docked.focused() != regionRelations {
			t.Errorf("a click on %v left active %v, region %d; want the relations still focused", cell, app.docked.active, app.docked.focused())
		}
	}

	click(app, paneX, bodyY)
	if app.docked.focused() != regionBody {
		t.Errorf("a click on the body's first column left region %d focused", app.docked.focused())
	}
}

// A short terminal shows the column picker's rows around its cursor, so the
// rows on screen start past the first.
func TestAClickOnAScrolledPickerRowIsPressingItsAccelerator(t *testing.T) {
	open := func() (*App, *StubBackend) {
		app, stub := setupTestApp(t, listKeysNibs())
		app.Update(tea.WindowSizeMsg{Width: 120, Height: 14})
		pressKeys(app, "z c end")
		if app.state != viewColumnPicker || app.columnPicker.cursor == 0 {
			t.Fatalf("premise failed: z c end left state %d, cursor %d", app.state, app.columnPicker.cursor)
		}
		return app, stub
	}
	byKey, _ := open()
	before := byKey.columnPicker.chosen()
	picked := byKey.columnPicker.rows[byKey.columnPicker.cursor-1]
	row := "[ ] " + string(picked.name)
	if picked.shown {
		row = "[x] " + string(picked.name)
	}
	pressKeys(byKey, acceleratorOf(byKey.columnPicker.View(), row))
	want := byKey.columnPicker.chosen()
	if reflect.DeepEqual(want, before) {
		t.Fatalf("premise failed: the accelerator of %q changed nothing", row)
	}

	byClick, _ := open()
	x, y := rowOnScreen(t, byClick, row)
	click(byClick, x, y)
	if got := byClick.columnPicker.chosen(); !reflect.DeepEqual(got, want) {
		t.Errorf("a click on %q chose %v, want %v as its accelerator does", row, got, want)
	}
}

func TestAClickFocusesARegionOfTheExpandedView(t *testing.T) {
	for _, width := range []int{80, 120} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			app := expandOn(t, everyRelationNibs(), "hub", width, 40)
			x, y := onScreen(t, app, bodyMarker)
			click(app, x, y)
			if app.expanded.focused() != regionBody {
				t.Errorf("a click on the body left region %d focused", app.expanded.focused())
			}
			x, y = onScreen(t, app, hubDocument)
			click(app, x, y)
			if es := app.expanded.entries(); app.expanded.focused() != regionRelations || app.expanded.cursor >= len(es) || es[app.expanded.cursor].doc != hubDocument {
				t.Errorf("a click on a document left region %d and entry %d, want %s in the relations", app.expanded.focused(), app.expanded.cursor, hubDocument)
			}
			if got := app.expanded.nib.ID; got != "hub" {
				t.Errorf("a click on a document showed %s, want hub still", got)
			}
		})
	}
}

// modalLines returns the lines of the open modal's box on the screen, from the
// line its top border is on, each cut to the box's columns, and the cell of
// the box's left edge.
func modalLines(t *testing.T, app *App) (lines []string, top, left int) {
	t.Helper()
	screen := strings.Split(plainView(app), "\n")
	for y, line := range screen {
		if x := cellOf(line, "╭"); x >= 0 {
			right := cellOf(line, "╮")
			for _, l := range screen[y:] {
				lines = append(lines, ansi.Cut(l, x, right+1))
			}
			return lines, y, x
		}
	}
	t.Fatalf("premise failed: no modal on the screen:\n%s", plainView(app))
	return nil, 0, 0
}

// rowOnScreen returns the cell where row's text starts in the open modal: the
// first line of the box that shows row after a key, as acceleratorOf reads it.
func rowOnScreen(t *testing.T, app *App, row string) (x, y int) {
	t.Helper()
	x, y, ok := findRowOnScreen(t, app, row)
	if !ok {
		lines, _, _ := modalLines(t, app)
		t.Fatalf("premise failed: the modal shows no row %q:\n%s", row, strings.Join(lines, "\n"))
	}
	return x, y
}

// findRowOnScreen is rowOnScreen, with ok false when the modal does not show
// row.
func findRowOnScreen(t *testing.T, app *App, row string) (x, y int, ok bool) {
	t.Helper()
	lines, top, left := modalLines(t, app)
	for i, line := range lines {
		if acceleratorOf(line, row) != "" {
			return left + cellOf(line, row), top + i, true
		}
	}
	return 0, 0, false
}

// A click on a picker row does what its accelerator does: it moves the cursor
// there, then chooses the row or marks it, as acceleratorMsg does. At 120x40
// every row is on screen. A short terminal gives a picker's list one line or
// two, so only the rows that fit are clicked.
func TestClickingAPickerRowIsPressingItsAccelerator(t *testing.T) {
	sizes := []struct {
		width, height int
		short         bool // whether the picker may show only some of its rows
	}{{120, 40, false}, {100, 14, true}, {80, 11, true}}
	for _, size := range sizes {
		for _, tc := range pickerCases {
			app, _ := openPickerAt(t, tc, size.width, size.height)
			view, rows := tc.view(app), tc.rows(app)
			clicked := 0
			for _, row := range rows {
				if _, _, ok := findRowOnScreen(t, app, row); !ok {
					if !size.short {
						t.Errorf("%dx%d: the %s picker does not show its row %q", size.width, size.height, tc.name, row)
					}
					continue
				}
				clicked++
				t.Run(fmt.Sprintf("%dx%d/%s/%s", size.width, size.height, tc.name, row), func(t *testing.T) {
					key := acceleratorOf(view, row)
					confirm := ""
					if tc.choose != "enter" {
						confirm = "enter"
					}

					byKey, byKeyStub := openPickerAt(t, tc, size.width, size.height)
					pressKeys(byKey, key+" "+confirm)

					byClick, byClickStub := openPickerAt(t, tc, size.width, size.height)
					x, y := rowOnScreen(t, byClick, row)
					click(byClick, x, y)
					pressKeys(byClick, confirm)

					got, want := outcomeOf(byClick, byClickStub), outcomeOf(byKey, byKeyStub)
					if want.State == openedState(tc) && confirm == "" {
						t.Fatalf("premise failed: accelerator %q changed nothing", key)
					}
					if !reflect.DeepEqual(got, want) {
						t.Errorf("click on %q:\n got %+v\nwant %+v", row, got, want)
					}
				})
			}
			if clicked == 0 {
				t.Errorf("%dx%d: the %s picker shows none of its rows:\n%s", size.width, size.height, tc.name, plainView(app))
			}
		}
	}
}

// openStatusPicker returns an App with the status picker open on nib-4, its
// cursor on the nib's status.
func openStatusPicker(t *testing.T) (*App, *StubBackend) {
	t.Helper()
	return openPicker(t, pickerCases[0])
}

func TestAClickOutsideAModalOrOffItsRowsDoesNothing(t *testing.T) {
	app, stub := openStatusPicker(t)
	before := app.statusPicker.list.Index()
	lines, top, left := modalLines(t, app)
	screen := strings.Split(plainView(app), "\n")
	outside := [][2]int{{0, 0}, {left - 1, top + 3}, {left + lipgloss.Width(lines[0]), top + 3}, {0, len(screen) - 1}}
	header := [2]int{left + 2, top + 1}
	for _, cell := range append(outside, header) {
		click(app, cell[0], cell[1])
		if cell != header {
			turnWheel(app, cell[0], cell[1], true)
		}
		if app.state != viewStatusPicker || app.statusPicker.list.Index() != before || len(stub.UpdateCalls) != 0 {
			t.Fatalf("a click on %v left state %d, picker row %d, updates %v; want the picker open on row %d",
				cell, app.state, app.statusPicker.list.Index(), stub.UpdateCalls, before)
		}
	}
	if got := selectedID(app); got != "nib-4" {
		t.Errorf("a click outside the picker moved the list cursor to %s", got)
	}
}

func TestTheWheelOverAPickerMovesItsCursor(t *testing.T) {
	app, stub := openStatusPicker(t)
	before := app.statusPicker.list.Index()
	x, y := rowOnScreen(t, app, "draft")
	turnWheel(app, x, y, true)
	if got := app.statusPicker.list.Index(); got != before+1 || len(stub.UpdateCalls) != 0 {
		t.Errorf("the picker is on row %d with updates %v after the wheel, want row %d and none", got, stub.UpdateCalls, before+1)
	}
}

// The hit test tells a row's fold mark from the rest of the row, for a row
// that has children only.
func TestTheHitTestFindsTheFoldMark(t *testing.T) {
	app, _ := setupTestApp(t, listKeysNibs())
	x, y := onScreen(t, app, "Epic")
	mark := lipgloss.Width(ui.CollapseIndicatorExpanded(app.display.ascii()))
	if h := app.hitAt(x-mark, y); h.area != hitListRow || h.rowID != "nib-1" || !h.fold {
		t.Errorf("hit on Epic's fold mark = %+v, want nib-1's row on its fold mark", h)
	}
	if h := app.hitAt(x, y); h.area != hitListRow || h.rowID != "nib-1" || h.fold {
		t.Errorf("hit on Epic's title = %+v, want nib-1's row off its fold mark", h)
	}
	x, y = onScreen(t, app, "Last")
	if h := app.hitAt(x-mark, y); h.area != hitListRow || h.rowID != "nib-4" || h.fold {
		t.Errorf("hit before Last = %+v, want nib-4's row without a fold mark", h)
	}
}

// The filter box's completion menu covers the rows it is drawn over, so a
// click on it does not reach the row underneath.
func TestAClickOnTheCompletionMenuDoesNotPickTheRowUnderIt(t *testing.T) {
	app, _ := setupTestApp(t, pagedNibs(10))
	pressKeys(app, "/ tab")
	if len(app.list.completions) == 0 || selectedID(app) != "row-00" {
		t.Fatalf("premise failed: tab showed %d completions with the cursor on %q", len(app.list.completions), selectedID(app))
	}
	x, y := onScreen(t, app, "priority:")
	click(app, x, y)
	if got := selectedID(app); got != "row-00" {
		t.Errorf("a click on the menu moved the cursor to %s", got)
	}
}

// While the filter box or the find box takes the keys, a click on the docked
// view does not take them: a letter typed next goes into the box, not to a
// nib-view action. The wheel there and a click on a list row leave them in the
// box too.
func TestAClickOnTheDockedViewLeavesTheKeysInAnOpenBox(t *testing.T) {
	boxes := []struct {
		name, open string
		ctx        keyContext
		text       func(*App) string
	}{
		{"filter box", "/", ctxFilterBox, func(a *App) string { return a.list.box.Value() }},
		{"find box", "f", ctxFindBox, func(a *App) string { return a.list.findBox.Value() }},
	}
	for _, tc := range dockPlacements {
		for _, box := range boxes {
			t.Run(tc.name+"/"+box.name, func(t *testing.T) {
				app := dockedApp(t, "hub", tc.width, tc.height)
				pressKeys(app, box.open)
				if got := app.keyContext(); got != box.ctx {
					t.Fatalf("premise failed: %s opened the %s", box.open, got)
				}
				status := app.docked.nib.Status
				for _, text := range []string{bodyMarker, "Second child"} {
					x, y := inPane(t, app, text, true)
					click(app, x, y)
					turnWheel(app, x, y, true)
					if got := app.keyContext(); got != box.ctx || app.docked.active {
						t.Fatalf("a click on %q in the nib view moved the keys to the %s (view active %v)", text, got, app.docked.active)
					}
				}
				x, y := inPane(t, app, "Lone task", false)
				click(app, x, y)
				if got := app.keyContext(); got != box.ctx || selectedID(app) != "rz" {
					t.Fatalf("a click on a list row left the keys in the %s and the cursor on %s, want the %s on rz", got, selectedID(app), box.ctx)
				}
				before := box.text(app)
				pressKeys(app, "s")
				if got := box.text(app); app.state != viewList || got != before+"s" {
					t.Errorf("s after the clicks left state %d and the box holding %q, want %q in the box", app.state, got, before+"s")
				}
				if n, _ := app.backend.GetNib(t.Context(), "hub"); n.Status != status {
					t.Errorf("hub's status changed to %s", n.Status)
				}
			})
		}
	}
}

// While a picker's filter is typed, a click on a row moves the picker's cursor
// there and leaves the keys in the filter: the row is not chosen.
func TestAClickOnAPickerRowWhileItsFilterIsTypedOnlyMovesTheCursor(t *testing.T) {
	app, stub := openStatusPicker(t)
	pressKeys(app, "/ e")
	if app.keyContext() != ctxFilterInput {
		t.Fatalf("premise failed: / e left the %s", app.keyContext())
	}
	x, y := rowOnScreen(t, app, "deferred")
	click(app, x, y)
	item, _ := app.statusPicker.list.SelectedItem().(statusItem)
	if app.state != viewStatusPicker || len(stub.UpdateCalls) != 0 || item.name != "deferred" {
		t.Fatalf("the click left state %d, updates %v and the cursor on %q; want the picker open on deferred", app.state, stub.UpdateCalls, item.name)
	}
	pressKeys(app, "x")
	if got := app.statusPicker.list.FilterValue(); app.keyContext() != ctxFilterInput || got != "ex" {
		t.Errorf("x after the click went to the %s with the filter %q, want ex in the filter", app.keyContext(), got)
	}
}

// stopClock has app time double clicks by a clock that moves only when the
// returned advance is called.
func stopClock(app *App) (advance func(time.Duration)) {
	now := time.Now()
	app.now = func() time.Time { return now }
	return func(d time.Duration) { now = now.Add(d) }
}

// clickTwice sends two left clicks, on (x1, y1) and then on (x2, y2), with gap
// between them on a stopped clock.
func clickTwice(app *App, x1, y1, x2, y2 int, gap time.Duration) {
	advance := stopClock(app)
	click(app, x1, y1)
	advance(gap)
	click(app, x2, y2)
}

// inView returns the cell where text first starts in the nib view on screen.
func inView(t *testing.T, app *App, text string) (x, y int) {
	t.Helper()
	if app.state == viewExpanded {
		return onScreen(t, app, text)
	}
	return inPane(t, app, text, true)
}

// doubleClick sends a double click on cell (x, y).
func doubleClick(app *App, x, y int) {
	clickTwice(app, x, y, x, y, doubleClickInterval/5)
}

// foldMarkOf returns the cell of the fold mark on the first line that shows
// title.
func foldMarkOf(t *testing.T, app *App, title string) (x, y int) {
	t.Helper()
	_, y = onScreen(t, app, title)
	line := strings.Split(plainView(app), "\n")[y]
	ascii := app.display.ascii()
	for _, mark := range []string{ui.CollapseIndicatorExpanded(ascii), ui.CollapseIndicatorCollapsed(ascii)} {
		if x := cellOf(line, strings.TrimSpace(mark)); x >= 0 {
			return x, y
		}
	}
	t.Fatalf("premise failed: the line of %q has no fold mark: %q", title, line)
	return 0, 0
}

func TestADoubleClickOnARowExpandsItsNib(t *testing.T) {
	app, _ := setupTestApp(t, listKeysNibs())
	x, y := onScreen(t, app, "Last")
	doubleClick(app, x, y)
	if app.state != viewExpanded || app.expanded.nib == nil || app.expanded.nib.ID != "nib-4" {
		t.Fatalf("a double click on Last left state %d on %v, want the expanded view of nib-4", app.state, app.expanded.nib)
	}
	pressKeys(app, "esc")
	if app.state != viewList || selectedID(app) != "nib-4" {
		t.Errorf("esc left state %d with the cursor on %s, want the list on nib-4", app.state, selectedID(app))
	}
}

// The two clicks of a double click on a row may land on two cells of it.
func TestADoubleClickAcrossOneRowExpandsItsNib(t *testing.T) {
	app, _ := setupTestApp(t, listKeysNibs())
	x, y := onScreen(t, app, "Last")
	clickTwice(app, x, y, x+2, y, doubleClickInterval/5)
	if app.state != viewExpanded || app.expanded.nib == nil || app.expanded.nib.ID != "nib-4" {
		t.Errorf("a double click across Last left state %d on %v, want the expanded view of nib-4", app.state, app.expanded.nib)
	}
}

func TestADoubleClickWithTheNibViewDockedExpands(t *testing.T) {
	for _, tc := range dockPlacements {
		t.Run(tc.name+"/a list row", func(t *testing.T) {
			app := dockedApp(t, "hub", tc.width, tc.height)
			x, y := inPane(t, app, "Lone task", false)
			doubleClick(app, x, y)
			if app.state != viewExpanded || app.expanded.nib.ID != "rz" {
				t.Errorf("a double click on Lone task left state %d on %v, want the expanded view of rz", app.state, app.expanded.nib)
			}
		})
		t.Run(tc.name+"/the docked view", func(t *testing.T) {
			app := dockedApp(t, "hub", tc.width, tc.height)
			x, y := inPane(t, app, bodyMarker, true)
			doubleClick(app, x, y)
			if app.state != viewExpanded || app.expanded.nib.ID != "hub" {
				t.Errorf("a double click on the docked body left state %d on %v, want the expanded view of hub", app.state, app.expanded.nib)
			}
		})
	}
}

// Two clicks too far apart in time, or on two rows, are two single clicks.
func TestTwoClicksThatAreNotADoubleClickDoNotExpand(t *testing.T) {
	t.Run("slow", func(t *testing.T) {
		app, _ := setupTestApp(t, listKeysNibs())
		x, y := onScreen(t, app, "Last")
		clickTwice(app, x, y, x, y, doubleClickInterval+time.Millisecond)
		if app.state != viewList || selectedID(app) != "nib-4" {
			t.Errorf("two slow clicks on Last left state %d with the cursor on %s, want the list on nib-4", app.state, selectedID(app))
		}
	})
	t.Run("two rows", func(t *testing.T) {
		app, _ := setupTestApp(t, listKeysNibs())
		x1, y1 := onScreen(t, app, "Child")
		x2, y2 := onScreen(t, app, "Last")
		clickTwice(app, x1, y1, x2, y2, doubleClickInterval/5)
		if app.state != viewList || selectedID(app) != "nib-4" {
			t.Errorf("quick clicks on Child and Last left state %d with the cursor on %s, want the list on nib-4", app.state, selectedID(app))
		}
	})
	t.Run("two cells of the docked body", func(t *testing.T) {
		app := dockedApp(t, "hub", 140, 40)
		x, y := inPane(t, app, bodyMarker, true)
		clickTwice(app, x, y, x+3, y, doubleClickInterval/5)
		if app.state != viewList {
			t.Errorf("quick clicks on two cells of the docked body left state %d, want the list", app.state)
		}
	})
	t.Run("a third click", func(t *testing.T) {
		app := dockedApp(t, "hub", 140, 40)
		x, y := inPane(t, app, "Lone task", false)
		advance := stopClock(app)
		click(app, x, y)
		advance(doubleClickInterval / 5)
		click(app, x, y)
		pressKeys(app, "esc")
		if app.state != viewList {
			t.Fatalf("premise failed: esc after the double click left state %d", app.state)
		}
		advance(doubleClickInterval / 5)
		click(app, x, y)
		if app.state != viewList {
			t.Errorf("a third quick click on Lone task left state %d, want the list", app.state)
		}
	})
}

// The first click of a double click on a relation follows it, which changes
// what its cell shows, so the second click does nothing at all.
func TestADoubleClickOnARelationFollowsItOnce(t *testing.T) {
	for _, mode := range followModes {
		t.Run(mode.name, func(t *testing.T) {
			app := mode.start(t, "hub")
			x, y := inView(t, app, "First child")
			doubleClick(app, x, y)
			if app.state != mode.state || shownID(app) != "rc1" {
				t.Errorf("the double click left state %d on %s, want state %d on rc1", app.state, shownID(app), mode.state)
			}
			if len(app.jumps) != 1 || app.jumps[0].nib != "hub" {
				t.Errorf("the jump history is %+v, want one jump back to hub", app.jumps)
			}
		})
	}
}

func TestAClickOnAFoldMarkTogglesThatRow(t *testing.T) {
	t.Run("tree", func(t *testing.T) {
		app, _ := setupTestApp(t, listKeysNibs())
		focusOn(app, "nib-4")
		x, y := foldMarkOf(t, app, "Epic")
		advance := stopClock(app)
		click(app, x, y)
		if got := rowIDs(app); !slices.Equal(got, []string{"nib-1", "nib-4"}) || selectedID(app) != "nib-4" {
			t.Fatalf("a click on Epic's fold mark left rows %v with the cursor on %s, want nib-2 folded away and the cursor on nib-4", got, selectedID(app))
		}
		advance(doubleClickInterval + time.Millisecond)
		click(app, x, y)
		if got := len(rowIDs(app)); got != 3 || selectedID(app) != "nib-4" {
			t.Errorf("a second click on the fold mark left %d rows with the cursor on %s, want all 3 with the cursor on nib-4", got, selectedID(app))
		}
	})

	t.Run("the cursor in the folded rows", func(t *testing.T) {
		app, _ := setupTestApp(t, listKeysNibs())
		focusOn(app, "nib-2")
		x, y := foldMarkOf(t, app, "Epic")
		click(app, x, y)
		if got := selectedID(app); got != "nib-1" || !app.list.collapsed()["nib-1"] {
			t.Errorf("the cursor is on %s with folds %v, want nib-1 folded with the cursor on it", got, app.list.collapsed())
		}
	})

	t.Run("a double click", func(t *testing.T) {
		app, _ := setupTestApp(t, listKeysNibs())
		x, y := foldMarkOf(t, app, "Epic")
		doubleClick(app, x, y)
		if app.state != viewList || !app.list.collapsed()["nib-1"] {
			t.Errorf("a double click on the fold mark left state %d with folds %v, want the list with nib-1 folded once", app.state, app.list.collapsed())
		}
	})

	t.Run("a childless row", func(t *testing.T) {
		app, _ := setupTestApp(t, listKeysNibs())
		x, _ := foldMarkOf(t, app, "Epic")
		_, y := onScreen(t, app, "Last")
		click(app, x, y)
		if got := len(rowIDs(app)); got != 3 || len(app.list.collapsed()) != 0 {
			t.Errorf("a click before Last left %d rows with folds %v, want all 3 and none", got, app.list.collapsed())
		}
	})

	t.Run("milestones", func(t *testing.T) {
		app, _ := setupGroupedApp(t)
		pressKeys(app, "v m")
		focusRow(app, backlogRow)
		x, y := foldMarkOf(t, app, "Beta")
		click(app, x, y)
		want := []string{"ms2", "ms1", "t1", backlogRow, "t3", "t4"}
		if got := rowIDs(app); !slices.Equal(got, want) || app.list.focusedRowID() != backlogRow {
			t.Errorf("rows after a click on Beta's fold mark = %v with the cursor on %s, want %v on the Backlog row", got, app.list.focusedRowID(), want)
		}
	})

	t.Run("areas", func(t *testing.T) {
		app, _ := setupGroupedApp(t)
		pressKeys(app, "v a")
		x, y := foldMarkOf(t, app, "api")
		click(app, x, y)
		want := []string{sectionRowPrefix + "api", sectionRowPrefix + "docs", noAreaRow, "ms2", "ms1", "t3", "t4"}
		if got := rowIDs(app); !slices.Equal(got, want) {
			t.Errorf("rows after a click on api's fold mark = %v, want %v", got, want)
		}
	})
}

func TestAClickOnARelationFollowsItAndBackspaceReturns(t *testing.T) {
	for _, mode := range followModes {
		t.Run(mode.name, func(t *testing.T) {
			app := mode.start(t, "hub")
			x, y := inView(t, app, "First child")
			click(app, x, y)
			if app.state != mode.state {
				t.Fatalf("the click left state %d, want %d", app.state, mode.state)
			}
			requireAt(t, app, "after the click", "rc1", "rc1")
			pressKeys(app, "backspace")
			requireAt(t, app, "after backspace", "hub", "hub")
			if got := app.keyContext(); got != ctxNibRelations {
				t.Errorf("backspace left the focus in the %s, want the relations", got)
			}
		})
	}
	for _, tc := range dockPlacements {
		t.Run("docked "+tc.name, func(t *testing.T) {
			app := dockedApp(t, "hub", tc.width, tc.height)
			x, y := inPane(t, app, "Second child", true)
			click(app, x, y)
			requireAt(t, app, "after the click", "rc2", "rc2")
			pressKeys(app, "backspace")
			requireAt(t, app, "after backspace", "hub", "hub")
		})
	}
}

// A click on a relation the filter hides follows it as enter does: the view
// shows the nib with the note, and the filter and the list cursor stay.
func TestAClickOnAFilteredOutRelationShowsItWithANote(t *testing.T) {
	for _, mode := range followModes {
		t.Run(mode.name, func(t *testing.T) {
			f := listFilters[1]
			app := listApp(t, "hub")
			if mode.state == viewList {
				pressKeys(app, "z p")
			}
			f.apply(t, app)
			if app.list.hasRow(f.target) || app.list.focusedID() != "hub" {
				t.Fatalf("premise failed: %s left %s in the list or moved the cursor to %s", f.name, f.target, app.list.focusedID())
			}
			if mode.state == viewExpanded {
				pressKeys(app, "enter")
			}
			before := filtersOf(app)
			x, y := inView(t, app, "First child")
			click(app, x, y)
			requireAt(t, app, "after the click", f.target, "hub")
			if !strings.Contains(plainView(app), notInListNote) {
				t.Errorf("the view of %s lacks the note:\n%s", f.target, plainView(app))
			}
			if got := filtersOf(app); got != before {
				t.Errorf("the click changed the filter from %+v to %+v", before, got)
			}
			pressKeys(app, "backspace")
			requireAt(t, app, "after backspace", "hub", "hub")
		})
	}
}

// While a box being typed in takes the keys, a double click is two single
// clicks and a click on a relation follows nothing. A click on a fold mark
// still folds, since folding leaves the keys in the box.
func TestWhileABoxIsOpenNoClickTakesTheKeys(t *testing.T) {
	for _, open := range []string{"/", "f"} {
		t.Run(open, func(t *testing.T) {
			app := dockedApp(t, "hub", 140, 40)
			pressKeys(app, open)
			ctx := app.keyContext()
			if !slices.Contains(textInputs, ctx) {
				t.Fatalf("premise failed: %s opened the %s", open, ctx)
			}
			x, y := inPane(t, app, bodyMarker, true)
			doubleClick(app, x, y)
			x, y = inPane(t, app, "Second child", true)
			click(app, x, y)
			x, y = inPane(t, app, "Lone task", false)
			doubleClick(app, x, y)
			if app.state != viewList || app.keyContext() != ctx || len(app.jumps) != 0 || selectedID(app) != "rz" {
				t.Fatalf("the clicks left state %d, the keys in the %s, jumps %+v and the cursor on %s; want the %s on rz with no jump",
					app.state, app.keyContext(), app.jumps, selectedID(app), ctx)
			}
			x, y = foldMarkOf(t, app, "Parent epic")
			click(app, x, y)
			if !app.list.collapsed()["rp"] || app.keyContext() != ctx {
				t.Errorf("a click on a fold mark left folds %v and the keys in the %s, want rp folded and the %s", app.list.collapsed(), app.keyContext(), ctx)
			}
		})
	}
}

func TestWithTheMouseOffNoClickFoldsExpandsOrFollows(t *testing.T) {
	app := setupTestAppWithUserConfig(t, everyRelationNibs(), config.Default(), "tui:\n  mouse: false\n")
	x, y := foldMarkOf(t, app, "Parent epic")
	click(app, x, y)
	if len(app.list.collapsed()) != 0 {
		t.Errorf("a click on a fold mark folded %v", app.list.collapsed())
	}
	x, y = onScreen(t, app, "Lone task")
	doubleClick(app, x, y)
	if app.state != viewList {
		t.Fatalf("a double click left state %d, want the list", app.state)
	}
	focusOn(app, "hub")
	pressKeys(app, "enter")
	if app.state != viewExpanded {
		t.Fatalf("premise failed: enter left state %d", app.state)
	}
	x, y = onScreen(t, app, "First child")
	click(app, x, y)
	if got := app.expanded.nib.ID; got != "hub" || len(app.jumps) != 0 {
		t.Errorf("a click on a relation left the view on %s with jumps %+v, want hub and none", got, app.jumps)
	}
}

// clickBetween clicks cell (x, y), runs between, then clicks the same cell
// again within doubleClickInterval on a stopped clock.
func clickBetween(app *App, x, y int, between func()) {
	advance := stopClock(app)
	click(app, x, y)
	between()
	advance(doubleClickInterval / 5)
	click(app, x, y)
}

// Whatever moves the cursor, changes the docked nib or opens a modal between
// two quick clicks on one cell makes them two single clicks, so the second
// expands nothing the first click did not hit.
func TestAChangeBetweenTwoClicksMakesThemTwoSingleClicks(t *testing.T) {
	t.Run("the wheel moves the cursor", func(t *testing.T) {
		app, _ := setupTestApp(t, listKeysNibs())
		x, y := onScreen(t, app, "Child")
		clickBetween(app, x, y, func() { turnWheel(app, x, y, true) })
		if app.state != viewList || selectedID(app) != "nib-2" {
			t.Errorf("the clicks left state %d with the cursor on %s, want the list on nib-2", app.state, selectedID(app))
		}
	})

	t.Run("a key moves the cursor", func(t *testing.T) {
		app, _ := setupTestApp(t, listKeysNibs())
		x, y := onScreen(t, app, "Child")
		clickBetween(app, x, y, func() { pressKeys(app, "j") })
		if app.state != viewList || selectedID(app) != "nib-2" {
			t.Errorf("the clicks left state %d with the cursor on %s, want the list on nib-2", app.state, selectedID(app))
		}
	})

	t.Run("the wheel changes the docked nib", func(t *testing.T) {
		app := dockedApp(t, "hub", 140, 40)
		x, y := inPane(t, app, bodyMarker, true)
		lx, ly := inPane(t, app, "Hub feature", false)
		clickBetween(app, x, y, func() { turnWheel(app, lx, ly, true) })
		if app.state != viewList || shownID(app) != "rc1" {
			t.Errorf("the clicks left state %d with the dock on %s, want the list with rc1 docked", app.state, shownID(app))
		}
	})

	t.Run("a picker opens", func(t *testing.T) {
		app, stub := setupTestApp(t, listKeysNibs())
		x, y := onScreen(t, app, "Child")
		clickBetween(app, x, y, func() { pressKeys(app, "s") })
		if app.state != viewStatusPicker || len(stub.UpdateCalls) != 0 {
			t.Errorf("the clicks left state %d with updates %v, want the status picker open and no update", app.state, stub.UpdateCalls)
		}
	})

	t.Run("a reload removes the clicked nib", func(t *testing.T) {
		app, stub := setupTestApp(t, listKeysNibs())
		x, y := onScreen(t, app, "Child")
		clickBetween(app, x, y, func() {
			delete(stub.Nibs, "nib-2")
			stub.AllNibs = slices.DeleteFunc(stub.AllNibs, func(n *nib.Nib) bool { return n.ID == "nib-2" })
			reload(app)
		})
		if app.state != viewList || selectedID(app) != "nib-4" {
			t.Errorf("the clicks left state %d with the cursor on %s, want the list on nib-4, which the reload moved under the cell", app.state, selectedID(app))
		}
	})

	t.Run("a reload moves another row under the cell", func(t *testing.T) {
		app, stub := setupTestApp(t, listKeysNibs())
		x, y := onScreen(t, app, "Child")
		clickBetween(app, x, y, func() {
			top := &nib.Nib{ID: "nib-0", Title: "Top", Type: "task", Status: "todo", Order: "0"}
			stub.Nibs[top.ID] = top
			stub.AllNibs = append([]*nib.Nib{top}, stub.AllNibs...)
			reload(app)
		})
		if app.state != viewList || selectedID(app) != "nib-1" {
			t.Errorf("the clicks left state %d with the cursor on %s, want the list on nib-1, which the reload moved under the cell", app.state, selectedID(app))
		}
	})

	t.Run("a resize hides the dock", func(t *testing.T) {
		app := dockedApp(t, "hub", 140, 40)
		x, y := inPane(t, app, bodyMarker, true)
		clickBetween(app, x, y, func() {
			app.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
			if app.dockPlacement() != "" {
				t.Fatal("premise failed: the view is docked at 60x20")
			}
		})
		if app.state != viewList {
			t.Errorf("the clicks left state %d, want the list", app.state)
		}
	})
}

// A key, a wheel turn or a resize between two quick clicks on one cell makes
// them two single clicks, even when the cell and the cursor still show what
// the first click hit.
func TestAKeyWheelOrResizeBetweenTwoClicksEndsTheDoubleClick(t *testing.T) {
	between := []struct {
		name string
		do   func(app *App, x, y int)
	}{
		{"a wheel turn that moves nothing", func(app *App, x, y int) { turnWheel(app, x, y, false) }},
		{"an unbound key", func(app *App, _, _ int) { pressKeys(app, "x") }},
		{"a resize to the same size", func(app *App, _, _ int) { app.Update(tea.WindowSizeMsg{Width: 120, Height: 40}) }},
	}
	for _, tc := range between {
		t.Run(tc.name, func(t *testing.T) {
			app, _ := setupTestApp(t, listKeysNibs())
			x, y := onScreen(t, app, "Epic")
			clickBetween(app, x, y, func() { tc.do(app, x, y) })
			if app.state != viewList || selectedID(app) != "nib-1" {
				t.Errorf("the clicks left state %d with the cursor on %s, want the list on nib-1", app.state, selectedID(app))
			}
		})
	}
}

// doubleClick acts only while what the first click hit is still current; on a
// stale target it reports that it did nothing, so the click acts as a single
// one.
func TestADoubleClickOnAStaleTargetDoesNothing(t *testing.T) {
	app := dockedApp(t, "hub", 140, 40)
	stale := []clickTarget{
		{state: viewList, area: hitListRow, rowID: "rz"},
		{state: viewList, area: hitNibBody, docked: true, nib: "rz"},
		{state: viewExpanded, area: hitListRow, rowID: "hub"},
		{state: viewExpanded, area: hitNibBody, nib: "hub"},
	}
	for _, target := range stale {
		if _, acted := app.doubleClick(target); acted || app.state != viewList || selectedID(app) != "hub" {
			t.Errorf("doubleClick(%+v) acted %v, leaving state %d on %s; want nothing done", target, acted, app.state, selectedID(app))
		}
	}
	if _, acted := app.doubleClick(clickTarget{state: viewList, area: hitNibBody, docked: true, nib: "hub"}); !acted || app.state != viewExpanded {
		t.Errorf("premise failed: doubleClick on the current docked nib acted %v, leaving state %d", acted, app.state)
	}
	if _, acted := app.doubleClick(clickTarget{state: viewExpanded, area: hitNibBody, nib: "rz"}); acted {
		t.Error("doubleClick on the expanded view of rz acted while it shows hub")
	}
}

// A second click that pairs with the first on what its cell shows, after the
// list cursor left that row without a key or a wheel turn, acts as a single
// click: it puts the cursor back on the row.
func TestASecondClickOnAStaleTargetActsAsASingleClick(t *testing.T) {
	app, _ := setupTestApp(t, listKeysNibs())
	x, y := onScreen(t, app, "Child")
	clickBetween(app, x, y, func() { app.list.restoreCursor("nib-4") })
	if app.state != viewList || selectedID(app) != "nib-2" {
		t.Errorf("the second click left state %d with the cursor on %s, want the list on nib-2", app.state, selectedID(app))
	}
}

// A picker that opens and closes between two clicks, by messages rather than
// keys, makes them two single clicks: the second puts the cursor on its row.
func TestAModalBetweenTwoClicksEndsTheDoubleClick(t *testing.T) {
	for _, moved := range []bool{false, true} {
		t.Run(fmt.Sprintf("cursor moved %v", moved), func(t *testing.T) {
			app, _ := setupTestApp(t, listKeysNibs())
			x, y := onScreen(t, app, "Child")
			clickBetween(app, x, y, func() {
				app.Update(openStatusPickerMsg{nibIDs: []string{"nib-2"}, nibTitle: "Child", currentStatus: "todo"})
				if app.state != viewStatusPicker {
					t.Fatalf("premise failed: the message opened state %d", app.state)
				}
				app.Update(closeStatusPickerMsg{})
				if app.state != viewList {
					t.Fatalf("premise failed: the message left state %d", app.state)
				}
				if moved {
					app.list.restoreCursor("nib-4")
				}
			})
			if app.state != viewList || selectedID(app) != "nib-2" {
				t.Errorf("the clicks left state %d with the cursor on %s, want the list on nib-2", app.state, selectedID(app))
			}
		})
	}
}

// statusPickerOnRow00 returns pagedNibs(40) with the status picker open on
// row-00, and the cell of its in-progress row, under which the list draws
// another row.
func statusPickerOnRow00(t *testing.T) (app *App, stub *StubBackend, x, y int) {
	t.Helper()
	app, stub = setupTestApp(t, pagedNibs(40))
	pressKeys(app, "s")
	x, y = rowOnScreen(t, app, "in-progress")
	return app, stub, x, y
}

// A click that chooses a picker row closes the picker, so its cell then shows
// what the picker covered. A quick second click there does nothing at all.
func TestADoubleClickOnAPickerRowChoosesItOnce(t *testing.T) {
	t.Run("over the list", func(t *testing.T) {
		app, stub, x, y := statusPickerOnRow00(t)
		doubleClick(app, x, y)
		if len(stub.UpdateCalls) != 1 || stub.UpdateCalls[0].ID != "row-00" {
			t.Fatalf("the double click made updates %+v, want one on row-00", stub.UpdateCalls)
		}
		if h := app.hitAt(x, y); h.area != hitListRow || h.rowID == "row-00" {
			t.Fatalf("premise failed: the cell now hits %+v, want a row other than row-00", h)
		}
		if app.state != viewList || selectedID(app) != "row-00" {
			t.Errorf("the double click left state %d with the cursor on %s, want the list on row-00", app.state, selectedID(app))
		}
	})

	t.Run("over the expanded view", func(t *testing.T) {
		app := expandedApp(t, "hub")
		pressKeys(app, "s")
		x, y := rowOnScreen(t, app, "in-progress")
		doubleClick(app, x, y)
		if app.state != viewExpanded || shownID(app) != "hub" || len(app.jumps) != 0 {
			t.Fatalf("the double click left state %d on %s with jumps %+v, want the expanded view of hub and no jump", app.state, shownID(app), app.jumps)
		}
		if got := app.expanded.focused(); got != regionRelations {
			t.Errorf("the double click moved the focus to region %d, want the relations", got)
		}
	})

	t.Run("a slow second click", func(t *testing.T) {
		app, _, x, y := statusPickerOnRow00(t)
		clickTwice(app, x, y, x, y, doubleClickInterval+time.Millisecond)
		if want := app.hitAt(x, y).rowID; app.state != viewList || selectedID(app) != want {
			t.Errorf("the slow second click left state %d with the cursor on %s, want the list on %s, the row under the cell", app.state, selectedID(app), want)
		}
	})

	t.Run("a second click on another cell", func(t *testing.T) {
		app, _, x, y := statusPickerOnRow00(t)
		advance := stopClock(app)
		click(app, x, y)
		advance(doubleClickInterval / 5)
		ox, oy := onScreen(t, app, "Row 03")
		click(app, ox, oy)
		if app.state != viewList || selectedID(app) != "row-03" {
			t.Errorf("the second click left state %d with the cursor on %s, want the list on row-03", app.state, selectedID(app))
		}
	})
}

// bigFoldNibs is a parent with 50 children and then 10 top-level tasks, so
// unfolding the parent with the cursor on the last task scrolls the list.
func bigFoldNibs() []*nib.Nib {
	nibs := []*nib.Nib{{ID: "big", Title: "Big", Type: "epic", Status: "todo", Order: "0"}}
	for i := range 50 {
		nibs = append(nibs, &nib.Nib{ID: fmt.Sprintf("c-%02d", i), Title: fmt.Sprintf("Child %02d", i), Type: "task", Status: "todo", Parent: "big", Order: fmt.Sprintf("%02d", i)})
	}
	for i := range 10 {
		nibs = append(nibs, &nib.Nib{ID: fmt.Sprintf("t-%02d", i), Title: fmt.Sprintf("Top %02d", i), Type: "task", Status: "todo", Order: fmt.Sprintf("%d", i+1)})
	}
	return nibs
}

// Unfolding a row can scroll another row under the fold mark's cell. A quick
// second click there does nothing at all.
func TestADoubleClickOnAFoldMarkThatScrollsTheListTogglesOnce(t *testing.T) {
	app, _ := setupTestApp(t, bigFoldNibs())
	pressKeys(app, "left")
	if !focusOn(app, "t-09") || !app.list.collapsed()["big"] {
		t.Fatalf("premise failed: Big folded %v with the cursor on %s", app.list.collapsed()["big"], selectedID(app))
	}
	x, y := foldMarkOf(t, app, "Big")
	doubleClick(app, x, y)
	if h := app.hitAt(x, y); h.rowID == "big" {
		t.Fatalf("premise failed: the unfold left Big under the cell")
	}
	if app.list.collapsed()["big"] || selectedID(app) != "t-09" {
		t.Errorf("the double click left Big folded %v with the cursor on %s, want Big unfolded with the cursor on t-09", app.list.collapsed()["big"], selectedID(app))
	}
}

// A reload can draw another part of the docked view under a cell while the
// view still shows the same nib, so the second click there is a single click.
func TestAReloadThatRedrawsTheClickedCellEndsTheDoubleClick(t *testing.T) {
	app := dockedApp(t, "hub", 140, 40)
	x, y := inPane(t, app, bodyMarker, true)
	clickBetween(app, x, y, func() {
		parent := "hub"
		if _, err := app.backend.CreateNib(t.Context(), model.CreateNibInput{Title: "Third child", Parent: &parent}); err != nil {
			t.Fatal(err)
		}
		reload(app)
		if h := app.hitAt(x, y); h.area == hitNibBody || shownID(app) != "hub" {
			t.Fatalf("premise failed: after the reload the cell hits %+v with %s docked, want another part of hub's view", h, shownID(app))
		}
	})
	if app.state != viewList {
		t.Errorf("the second click left state %d, want the list", app.state)
	}
}
