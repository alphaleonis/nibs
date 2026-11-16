package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/config"
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

func TestAClickFocusesThePaneOrRegionUnderIt(t *testing.T) {
	for _, tc := range dockPlacements {
		t.Run(tc.name, func(t *testing.T) {
			app := dockedApp(t, "hub", tc.width, tc.height)

			x, y := inPane(t, app, bodyMarker, true)
			click(app, x, y)
			if !app.docked.active || app.docked.focused() != regionBody {
				t.Errorf("a click on the body left active %v, region %d; want the body focused", app.docked.active, app.docked.focused())
			}

			x, y = inPane(t, app, "Second child", true)
			click(app, x, y)
			if !app.docked.active || app.docked.focused() != regionRelations {
				t.Errorf("a click on a relation left active %v, region %d; want the relations focused", app.docked.active, app.docked.focused())
			}
			if r := app.docked.selectedRelation(); r == nil || r.ID != "rc2" {
				t.Errorf("the relations cursor is on %v, want rc2", r)
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
	x, y := inPane(t, app, "Second child", true)
	click(app, x, y)
	if !app.docked.active || app.docked.focused() != regionRelations {
		t.Fatalf("premise failed: a click on a relation left active %v, region %d", app.docked.active, app.docked.focused())
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
			x, y = onScreen(t, app, "Dependent bug")
			click(app, x, y)
			if r := app.expanded.selectedRelation(); app.expanded.focused() != regionRelations || r == nil || r.ID != "rd" {
				t.Errorf("a click on a relation left region %d and relation %v, want rd in the relations", app.expanded.focused(), r)
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
