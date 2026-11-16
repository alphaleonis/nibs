package tui

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/ui"
)

// footerLine is the frame's last row with its colors removed.
func footerLine(app *App) string {
	lines := strings.Split(stripAnsi(app.View().Content), "\n")
	return lines[len(lines)-1]
}

// paletteLine is the rendered palette line that names the action, colors kept.
func paletteLine(t *testing.T, app *App, name actionName) string {
	t.Helper()
	for _, line := range strings.Split(app.palette.View(), "\n") {
		if strings.Contains(stripAnsi(line), string(name)) {
			return line
		}
	}
	t.Fatalf("the palette shows no %s line:\n%s", name, stripAnsi(app.palette.View()))
	return ""
}

func paletteNames(app *App) []actionName {
	var names []actionName
	for _, e := range app.palette.entries {
		names = append(names, e.act.Name)
	}
	return names
}

// estimate has no key of its own, so the palette is a way to reach it.
func TestThePaletteRunsTheEstimateActionOnTheFocusedNib(t *testing.T) {
	app, _ := setupTestApp(t, listKeysNibs())
	if !focusOn(app, "nib-4") {
		t.Fatal("premise failed: could not focus nib-4")
	}

	pressKeys(app, "? e s t enter")

	if app.state != viewEstimatePicker {
		t.Fatalf("state = %d after ? est enter, want the estimate picker", app.state)
	}
	if !slices.Equal(app.estimatePicker.nibIDs, []string{"nib-4"}) {
		t.Errorf("the estimate picker edits %v, want [nib-4]", app.estimatePicker.nibIDs)
	}
}

func TestThePaletteListsTheActionsActiveWhereItOpened(t *testing.T) {
	tests := []struct {
		name    string
		enter   string
		want    []actionName
		notWant []actionName
	}{
		{
			name:    "list",
			want:    []actionName{actArchive, actCreate, actHideClosed, actQuit, actFocusNext},
			notWant: []actionName{actConfirmYes, actInputAccept, actPaletteRun},
		},
		{
			name:    "nib view",
			enter:   "enter",
			want:    []actionName{actFocusNext, actJumpBack, actEdit},
			notWant: []actionName{actArchive, actCreate, actConfirmYes, actPaletteRun},
		},
		{
			name:    "status picker",
			enter:   "s",
			want:    []actionName{actOpen, actFilter, actBottom},
			notWant: []actionName{actEdit, actArchive, actFocusNext},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, _ := setupTestApp(t, listKeysNibs())
			pressKeys(app, tt.enter+" ?")
			if app.state != viewPalette {
				t.Fatalf("premise failed: state = %d, want the palette", app.state)
			}
			names := paletteNames(app)
			for _, n := range tt.want {
				if !slices.Contains(names, n) {
					t.Errorf("the palette does not list %s", n)
				}
			}
			for _, n := range tt.notWant {
				if slices.Contains(names, n) {
					t.Errorf("the palette lists %s, which is not active here", n)
				}
			}
		})
	}
}

func TestAPaletteLineShowsTheKeyAndTheConfigName(t *testing.T) {
	app, _ := setupTestApp(t, listKeysNibs())
	pressKeys(app, "? a r c h i v e")
	line := stripAnsi(paletteLine(t, app, actArchive))
	if !regexp.MustCompile(`▸ +a +archive +nib\.archive`).MatchString(line) {
		t.Errorf("the archive line reads %q, want the cursor, the key a, the description and the config name", line)
	}
}

func TestTypingFiltersThePalette(t *testing.T) {
	app, _ := setupTestApp(t, listKeysNibs())
	pressKeys(app, "? c o p y")
	shown := app.palette.shown()
	if len(shown) != 1 || shown[0].act.Name != actCopyID {
		var names []actionName
		for _, e := range shown {
			names = append(names, e.act.Name)
		}
		t.Errorf("typing copy shows %v, want only %s", names, actCopyID)
	}
}

// Archive needs a nib under the cursor, which an empty list does not have.
func TestAnActionThatCannotRunIsDimmedWithItsReasonAndEnterLeavesIt(t *testing.T) {
	app, stub := setupTestApp(t, nil)
	pressKeys(app, "?")
	dim := fgSGRParams(ui.ColorGrayDim)
	if ready := paletteLine(t, app, actUp); strings.Contains(ready, dim) {
		t.Fatalf("premise failed: a ready line is dimmed too, so the dimming says nothing: %q", ready)
	}
	pressKeys(app, "a r c h i v e")

	line := paletteLine(t, app, actArchive)
	if !strings.Contains(stripAnsi(line), "no nib under the cursor") {
		t.Errorf("the archive line gives no reason it cannot run: %q", stripAnsi(line))
	}
	if !strings.Contains(line, dim) {
		t.Errorf("the archive line is not dimmed (%s): %q", dim, line)
	}

	pressKeys(app, "enter")
	if app.state != viewPalette {
		t.Errorf("state = %d after enter on archive, want the palette still open", app.state)
	}
	if len(stub.ArchiveCalls) != 0 {
		t.Errorf("archived %v", stub.ArchiveCalls)
	}
}

// The action runs in the view the palette covered, as its key would there: s in
// the nib view changes that nib's status and returns to the nib view.
func TestThePaletteRunsAnActionInTheViewItWasOpenedFrom(t *testing.T) {
	app, _ := setupTestApp(t, listKeysNibs())
	focusOn(app, "nib-4")
	pressKeys(app, "enter")
	if app.state != viewExpanded {
		t.Fatalf("premise failed: state = %d, want the nib view", app.state)
	}

	pressKeys(app, "? s t a t u s enter")
	if app.state != viewStatusPicker {
		t.Fatalf("state = %d, want the status picker", app.state)
	}
	if !slices.Equal(app.statusPicker.nibIDs, []string{"nib-4"}) {
		t.Errorf("the status picker edits %v, want [nib-4]", app.statusPicker.nibIDs)
	}
	pressKeys(app, "esc")
	if app.state != viewExpanded {
		t.Errorf("state = %d after esc, want the nib view the palette was opened from", app.state)
	}
}

func TestEscClosesThePalette(t *testing.T) {
	app, _ := setupTestApp(t, listKeysNibs())
	pressKeys(app, "? esc")
	if app.state != viewList {
		t.Errorf("state = %d after ? esc, want the list", app.state)
	}
}

func TestTheFooterShowsBulkActionsWithASelection(t *testing.T) {
	app, _ := setupTestApp(t, listKeysNibs())
	bulk := []string{"s status", "t type", "p parent", "m modify", "esc clear"}

	before := footerLine(app)
	for _, h := range bulk {
		if strings.Contains(before, h) {
			t.Errorf("premise failed: the footer shows %q with nothing selected: %q", h, before)
		}
	}

	pressKeys(app, "space")
	after := footerLine(app)
	for _, h := range bulk {
		if !strings.Contains(after, h) {
			t.Errorf("the footer lacks %q with a nib selected: %q", h, after)
		}
	}
	if strings.Contains(after, "n create") {
		t.Errorf("the footer still offers create with a nib selected: %q", after)
	}
}

func TestTheFooterSaysHowToClearAFilter(t *testing.T) {
	nibs := listKeysNibs()
	nibs[0].Tags = []string{"quux"}
	app, _ := setupTestApp(t, nibs)
	if got := footerLine(app); strings.Contains(got, "esc clear filter") {
		t.Fatalf("premise failed: the footer offers to clear a filter that is not set: %q", got)
	}
	pressKeys(app, "g t enter")
	if app.list.tagFilter != "quux" {
		t.Fatalf("premise failed: tag filter = %q, want quux", app.list.tagFilter)
	}
	if got := footerLine(app); !strings.Contains(got, "esc clear filter") {
		t.Errorf("the footer does not say how to clear the tag filter: %q", got)
	}
}

func TestAfterAPrefixTheFooterListsItsContinuations(t *testing.T) {
	tests := []struct {
		prefix  string
		want    []string
		notWant []string
	}{
		{prefix: "g", want: []string{"g then", "g top", "t filter by tag"}, notWant: []string{"n create", "h hide"}},
		{prefix: "z", want: []string{"z then", "h toggle closed"}, notWant: []string{"n create", "g top"}},
	}
	for _, tt := range tests {
		t.Run(tt.prefix, func(t *testing.T) {
			app, _ := setupTestApp(t, listKeysNibs())
			pressKeys(app, tt.prefix)
			got := footerLine(app)
			for _, h := range tt.want {
				if !strings.Contains(got, h) {
					t.Errorf("after %s the footer lacks %q: %q", tt.prefix, h, got)
				}
			}
			for _, h := range tt.notWant {
				if strings.Contains(got, h) {
					t.Errorf("after %s the footer shows %q: %q", tt.prefix, h, got)
				}
			}
		})
	}
}

// A status message takes the footer's row, so the hints give way to it.
func TestAStatusMessageTakesTheListFooter(t *testing.T) {
	app, _ := setupTestApp(t, []*nib.Nib{{ID: "nib-1", Title: "One", Type: "task", Status: "todo"}})
	app.list.statusMessage = "Copied nib-1 to clipboard"
	got := footerLine(app)
	if !strings.Contains(got, "Copied nib-1") || strings.Contains(got, "q quit") {
		t.Errorf("footer = %q, want the status message without the hints", got)
	}
}

// overNibView opens nib-4's nib view, then the overlay that open types: the
// palette (?) or the status picker (s).
func overNibView(t *testing.T, open string) (*App, *StubBackend) {
	t.Helper()
	app, stub := setupTestApp(t, listKeysNibs())
	focusOn(app, "nib-4")
	pressKeys(app, "enter "+open)
	if app.state == viewExpanded || app.state == viewList {
		t.Fatalf("premise failed: %s opened no overlay over the nib view, state = %d", open, app.state)
	}
	return app, stub
}

// The nib view under an overlay is on screen and is what esc returns to, so a
// change on disk reaches it as it would with no overlay open.
func TestAReloadRefreshesTheNibViewUnderAnOverlay(t *testing.T) {
	for _, open := range []string{"?", "s"} {
		t.Run(open+"/changed", func(t *testing.T) {
			app, stub := overNibView(t, open)
			renamed := *stub.Nibs["nib-4"]
			renamed.Title = "Renamed on disk"
			stub.Nibs["nib-4"] = &renamed

			_, cmd := app.Update(nibsChangedMsg{})
			processCmd(app, cmd)
			pressKeys(app, "esc")

			if app.state != viewExpanded {
				t.Fatalf("state = %d after esc, want the nib view", app.state)
			}
			if got := app.expanded.nib.Title; got != renamed.Title {
				t.Errorf("the nib view shows %q, want the title on disk %q", got, renamed.Title)
			}
		})
		t.Run(open+"/deleted", func(t *testing.T) {
			app, stub := overNibView(t, open)
			delete(stub.Nibs, "nib-4")

			_, cmd := app.Update(nibsChangedMsg{})
			processCmd(app, cmd)

			if app.state != viewList {
				t.Errorf("state = %d after the nib was deleted, want the list", app.state)
			}
		})
	}
}

// pickerHintRow is the hint row inside a picker modal, without its border: the
// modal's last line that names esc, since a description above it may too.
func pickerHintRow(t *testing.T, modal string) string {
	t.Helper()
	row := ""
	for _, line := range strings.Split(stripAnsi(modal), "\n") {
		if strings.Contains(line, "esc cancel") {
			row = strings.TrimSpace(strings.Trim(strings.TrimSpace(line), "│"))
		}
	}
	if row == "" {
		t.Fatalf("the modal has no hint row:\n%s", stripAnsi(modal))
	}
	return row
}

// At 80 columns the picker modals are narrower than their hint rows, so the
// rows end at the last hint that fits whole.
func TestAPickerHintRowEndsAtAWholeHint(t *testing.T) {
	tests := []struct {
		open  string
		view  func(*App) string
		hints []string
	}{
		{"s", func(a *App) string { return a.statusPicker.View() }, []string{"enter choose", "esc cancel", "/ filter", "? help"}},
		{"p", func(a *App) string { return a.parentPicker.View() }, []string{"enter choose", "esc cancel", "z h toggle closed", "/ filter", "? help"}},
		{"b", func(a *App) string { return a.blockingPicker.View() }, []string{"space toggle", "enter apply", "esc cancel", "/ filter", "? help"}},
	}
	for _, tt := range tests {
		t.Run(tt.open, func(t *testing.T) {
			app, _ := setupTestApp(t, listKeysNibs())
			app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			focusOn(app, "nib-4")
			pressKeys(app, tt.open)
			row := pickerHintRow(t, tt.view(app))
			whole := 0
			for _, h := range strings.Split(row, "  ") {
				if slices.Contains(tt.hints, h) {
					whole++
				} else {
					t.Errorf("the hint row ends in a partial hint %q: %q", h, row)
				}
			}
			if whole == len(tt.hints) {
				t.Errorf("premise failed: every hint fits at 80 columns, so nothing is cut: %q", row)
			}
		})
	}
}
