package tui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// helpEntryOnScreen is how an entry reads once the frame is collapsed to single
// spaces. Only the first nine characters of the description are asked for:
// minHelpDescWidth is ten and the cut marker takes one of them, so that much
// survives any narrowing the panel does — which lets the assertion read the
// painted panel without re-deriving the truncation it is checking.
func helpEntryOnScreen(e helpEntry) string {
	desc := []rune(e.Desc)
	desc = desc[:min(len(desc), 9)]
	return strings.Join(strings.Fields(e.Key+" "+string(desc)), " ")
}

// assertHelpPanelIntact fails for every keybinding the expanded panel promises
// but does not paint. Entries fill down then across, so a panel that ran past
// the terminal's last row loses the bottom of each column — ? and q among them
// — with nothing on screen to say those keys exist.
func assertHelpPanelIntact(t *testing.T, frame string, entries []helpEntry) {
	t.Helper()
	for _, e := range entries {
		if want := helpEntryOnScreen(e); !strings.Contains(frame, want) {
			t.Errorf("the help panel lost %q:\n%s", want, frame)
		}
	}
}

// expandedHelpView is one of the three compositions the panel is drawn into.
// Each pays for the panel out of a different frame, so each has its own hold-back.
type expandedHelpView struct {
	name    string
	enter   func(t *testing.T, app *App, width int)
	entries func(app *App) []helpEntry
}

func expandedHelpViews() []expandedHelpView {
	return []expandedHelpView{
		{
			name:    "list",
			entries: func(app *App) []helpEntry { return app.list.expandedHelpEntries() },
		},
		{
			name: "detail",
			enter: func(t *testing.T, app *App, _ int) {
				sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
				if app.state != viewDetail {
					t.Fatalf("premise failed: expected the detail view, got state %d", app.state)
				}
			},
			entries: func(*App) []helpEntry { return detailExpandedEntries() },
		},
		{
			// W leaves wide mode, which is what puts the list beside the
			// preview pane — a different composition, with its own footer
			// arithmetic.
			name: "two-column",
			enter: func(t *testing.T, app *App, width int) {
				sendKey(app, tea.KeyPressMsg{Code: 'W', Text: "W"})
				if want := width >= TwoColumnMinWidth; app.isTwoColumnMode() != want {
					t.Fatalf("premise failed: two-column mode = %v at width %d, want %v", app.isTwoColumnMode(), width, want)
				}
			},
			entries: func(app *App) []helpEntry { return app.list.expandedHelpEntries() },
		},
	}
}

// openExpandedHelp drives the real key path into one composition with the help
// panel open, and refuses a status change first when refused is set, so the
// model is holding a message the region has to find room for.
func openExpandedHelp(t *testing.T, view expandedHelpView, width, height int, refused bool) *App {
	t.Helper()
	app := setupRealBackendApp(t, queueRefusalNibs())
	app.Update(tea.WindowSizeMsg{Width: width, Height: height})
	if !focusOn(app, "ms1") {
		t.Fatal("premise failed: could not focus the milestone")
	}
	if view.enter != nil {
		view.enter(t, app, width)
	}
	sendKey(app, tea.KeyPressMsg{Code: '?', Text: "?"})
	if !app.helpExpanded {
		t.Fatal("premise failed: ? did not expand the help panel")
	}
	if refused {
		sendKey(app, tea.KeyPressMsg{Code: 's', Text: "s"})
		selectStatus(t, app, "completed")
		held := app.list.statusMessage
		if view.name == "detail" {
			held = app.detail.statusMessage
		}
		if want := "Status change failed: " + queueRefusal(); held != want {
			t.Fatalf("premise failed: the model is not holding the refusal, got %q", held)
		}
	}
	return app
}

// The expanded help panel replaces the footer's help keys — and used to take
// the status message down with them, since both View methods returned
// content+panel and never rendered the message the model was still holding. A
// user with ? open got exactly the pre-nibs-au40 silence: the picker closed,
// the list refreshed, and the refusal was nowhere.
//
// The panel is itself tall, so this pins both halves at once: the refusal has to
// be legible, and the panel's own rows have to survive sharing the screen with
// it.
func TestTheExpandedHelpPanelAndTheStatusMessageShareTheScreen(t *testing.T) {
	const termHeight = 24
	wantRefusal := "Status change failed: " + queueRefusal()

	for _, refused := range []bool{false, true} {
		for _, width := range []int{80, 100, 120, 160} {
			for _, view := range expandedHelpViews() {
				name := fmt.Sprintf("%s/%d", view.name, width)
				if refused {
					name += "/refused"
				}
				t.Run(name, func(t *testing.T) {
					app := openExpandedHelp(t, view, width, termHeight, refused)
					content := app.View().Content
					painted := frameText(content, width, termHeight)

					if refused && !strings.Contains(painted, wantRefusal) {
						t.Errorf("the frame does not carry the refusal with the help panel open.\nwant: %q\ngot:  %q", wantRefusal, painted)
					}
					assertHelpPanelIntact(t, painted, view.entries(app))

					if got := lipgloss.Height(content); got > termHeight {
						t.Errorf("frame is %d lines tall for a %d-line terminal — the bottom is clipped", got, termHeight)
					}
					if got := lipgloss.Width(content); got > width {
						t.Errorf("frame is %d columns wide for a %d-column terminal — the right edge is cut", got, width)
					}
				})
			}
		}
	}
}

// The panel is not exempt from the budget the status message keeps. Given a
// row bound it packs into more and narrower columns instead of running past it,
// which is what lets a message share the region: the list's twenty-four
// keybindings at eighty columns laid out as one column of twenty-four rows,
// taller than the terminal on its own.
func TestHelpPanelPacksIntoItsRowBudget(t *testing.T) {
	entries := append(listHelpEntries(), helpEntry{"?", "less"}, helpEntry{"q", "quit"})

	tests := []struct {
		name    string
		width   int
		maxRows int
	}{
		{name: "narrow terminal, no message", width: 80, maxRows: helpRowBudget(24, listBoxFloor, 0)},
		{name: "narrow terminal, three-line message", width: 80, maxRows: helpRowBudget(24, listBoxFloor, 3)},
		{name: "wide terminal", width: 160, maxRows: helpRowBudget(24, listBoxFloor, 0)},
		{name: "unbounded", width: 80, maxRows: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			panel := renderHelpPanel(entries, tt.width, tt.maxRows)
			rows := lipgloss.Height(panel)

			if got := helpPanelHeight(entries, tt.width, tt.maxRows); got != rows {
				t.Errorf("helpPanelHeight reports %d rows but the panel renders %d — the footer would be budgeted against the wrong number", got, rows)
			}
			if tt.maxRows > 0 && rows > tt.maxRows {
				t.Errorf("panel is %d rows for a %d-row budget", rows, tt.maxRows)
			}
			if got := lipgloss.Width(panel); got > tt.width {
				t.Errorf("panel is %d columns wide for a %d-column terminal", got, tt.width)
			}
			assertHelpPanelIntact(t, strings.Join(strings.Fields(ansiSGR.ReplaceAllString(panel, "")), " "), entries)
		})
	}
}

// helpHiddenMarkerRe reads the count the panel's marker names. It is read off
// the painted frame rather than the layout, because a marker the terminal
// clipped is not a marker the user was given.
var helpHiddenMarkerRe = regexp.MustCompile(`\x{2026} (\d+) more keys`)

func helpHiddenCount(frame string) int {
	m := helpHiddenMarkerRe.FindStringSubmatch(frame)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// assertHelpPanelAccountsForEveryKey fails when the panel is missing a
// keybinding it does not own up to. Entries fill down then across, so a panel
// the frame clipped loses the bottom of each column — ? and q among them — and
// a count of what is missing is the difference between a panel that was narrowed
// and one that was quietly cut short. A region with no room for a panel at all
// draws none, which the reader can see; that is the one case with nothing to
// account for.
func assertHelpPanelAccountsForEveryKey(t *testing.T, frame string, entries []helpEntry) {
	t.Helper()
	var missing []string
	for _, e := range entries {
		if !strings.Contains(frame, helpEntryOnScreen(e)) {
			missing = append(missing, e.Key)
		}
	}
	if len(missing) == 0 || len(missing) == len(entries) {
		return
	}
	if got := helpHiddenCount(frame); got != len(missing) {
		t.Errorf("the panel dropped %d keybindings (%s) but says %d are missing:\n%s",
			len(missing), strings.Join(missing, ", "), got, frame)
	}
}

// The footer region is budgeted against each view's own floor — the fewest rows
// it occupies however small a height it is handed. The list box floors at six
// lines because lipgloss pads a short render out to the border's height rather
// than truncating a tall one; the detail view's floor is its rendered header
// plus a body border around one viewport row. Neither gives a row back to a
// taller region, so a region budgeted against a shared constant instead runs off
// the bottom of the frame, and the panel's last keybindings go with it.
//
// This sweeps the geometries that arithmetic turns on: heights short enough for
// each floor to bind, and widths from where the panel starts packing to where it
// stops. Widths below 40 are left out deliberately — there the list box renders
// taller than its floor whatever it is handed, wrapped content pushed through
// the border, and the frame already overruns a 24-row terminal with the panel
// closed, so nothing here governs it.
func TestTheExpandedFooterRegionFitsTheTerminal(t *testing.T) {
	for _, refused := range []bool{false, true} {
		for _, height := range []int{8, 12, 16, 20, 24, 30} {
			for _, width := range []int{40, 46, 48, 80, 100, 120, 160, 200} {
				for _, view := range expandedHelpViews() {
					name := fmt.Sprintf("%s/%dx%d", view.name, width, height)
					if refused {
						name += "/refused"
					}
					t.Run(name, func(t *testing.T) {
						app := openExpandedHelp(t, view, width, height, refused)
						content := app.View().Content

						if got := lipgloss.Height(content); got > height {
							t.Errorf("frame is %d lines tall for a %d-line terminal — the bottom is clipped:\n%s",
								got, height, frameText(content, width, height))
						}

						painted := frameText(content, width, height)
						assertHelpPanelAccountsForEveryKey(t, painted, view.entries(app))

						// A refusal drawn over is the defect the region exists
						// to fix; the message wraps and may be cut, but its
						// opening has to be on screen.
						if refused && !strings.Contains(painted, "Status change failed") {
							t.Errorf("the frame does not carry the refusal with the help panel open:\n%s", painted)
						}
					})
				}
			}
		}
	}
}

// A key measured in bytes narrows two decisions this layout leans on: how far
// the panel may pack, and where descriptions are cut. Two list keys are
// multi-byte, so the byte count overstates the column by three cells.
func TestHelpKeyWidthCountsCellsNotBytes(t *testing.T) {
	tests := []struct {
		name    string
		entries []helpEntry
		want    int
	}{
		{name: "ascii", entries: []helpEntry{{"shift+tab", "collapse all"}}, want: 9},
		{name: "arrows", entries: []helpEntry{{"\u2190/\u2192", "collapse/expand"}}, want: 3},
		{name: "modifier and arrows", entries: []helpEntry{{"ctrl+\u2191/\u2193", "reorder"}}, want: 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := helpKeyWidth(tt.entries); got != tt.want {
				t.Errorf("helpKeyWidth = %d, want %d cells", got, tt.want)
			}
		})
	}

	// And the consequence the width feeds: at 46 columns the key column decides
	// whether the panel can pack at all.
	entries := append(listHelpEntries(), helpEntry{"?", "less"}, helpEntry{"q", "quit"})
	if got := helpPanelLayout(entries, 46, helpRowBudget(24, listBoxFloor, 0)).cols; got < 2 {
		t.Errorf("the panel packs into %d column(s) at 46 columns; a key column of %d cells leaves room for 2", got, helpKeyWidth(entries))
	}
}

// A description narrowed to fit says so. One that simply stops reads as the
// whole of what the key does — the same reason a truncated status message
// carries an ellipsis.
func TestANarrowedHelpDescriptionIsMarked(t *testing.T) {
	entries := append(listHelpEntries(), helpEntry{"?", "less"}, helpEntry{"q", "quit"})
	panel := ansiSGR.ReplaceAllString(renderHelpPanel(entries, 80, helpRowBudget(24, listBoxFloor, 3)), "")

	const longest = "reorder (block if multi-selected)"
	if strings.Contains(panel, longest) {
		t.Fatalf("premise failed: %q fits at 80 columns, so nothing is narrowed here", longest)
	}
	if !strings.Contains(panel, "\u2026") {
		t.Errorf("a narrowed description carries no cut marker:\n%s", panel)
	}
}
