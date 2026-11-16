package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
)

// A list taking filter input owns every printable key, so q and ? become part
// of the filter text instead of quitting or opening the palette.
func TestAFilteringListTakesQAndQuestionMark(t *testing.T) {
	stubViews := []struct {
		name   string
		open   func(app *App)
		state  viewState
		filter func(app *App) list.Model
	}{
		{"list", func(*App) {}, viewList, func(a *App) list.Model { return a.list.list }},
		{"parent picker", func(a *App) { sendKey(a, tea.KeyPressMsg{Code: 'p', Text: "p"}) }, viewParentPicker, func(a *App) list.Model { return a.parentPicker.list }},
		{"status picker", func(a *App) { sendKey(a, tea.KeyPressMsg{Code: 's', Text: "s"}) }, viewStatusPicker, func(a *App) list.Model { return a.statusPicker.list }},
		{"priority picker", func(a *App) { processCmd(a, a.runAction(findAction(defaultActions, actPriority))) }, viewPriorityPicker, func(a *App) list.Model { return a.priorityPicker.list }},
		{"estimate picker", func(a *App) { processCmd(a, a.runAction(findAction(defaultActions, actEstimate))) }, viewEstimatePicker, func(a *App) list.Model { return a.estimatePicker.list }},
		{"blocking picker", func(a *App) { sendKey(a, tea.KeyPressMsg{Code: 'b', Text: "b"}) }, viewBlockingPicker, func(a *App) list.Model { return a.blockingPicker.list }},
		{"tag picker", func(a *App) { a.Update(openTagPickerMsg{}) }, viewTagPicker, func(a *App) list.Model { return a.tagPicker.list }},
		{"milestone picker", func(a *App) { pressKeys(a, "m m") }, viewAssignPicker, func(a *App) list.Model { return a.assignPicker.list }},
		{"area picker", func(a *App) { pressKeys(a, "m a") }, viewAssignPicker, func(a *App) list.Model { return a.assignPicker.list }},
	}

	type view struct {
		name   string
		setup  func(t *testing.T) *App
		state  viewState
		filter func(app *App) list.Model
	}
	var views []view
	for _, sv := range stubViews {
		views = append(views, view{sv.name, func(t *testing.T) *App {
			nibs := pickerTestNibs()
			nibs[1].Tags = []string{"quux"}
			app, _ := setupTestApp(t, nibs)
			if !focusOn(app, "nib-2") {
				t.Fatal("premise failed: could not focus a task")
			}
			sv.open(app)
			return app
		}, sv.state, sv.filter})
	}
	views = append(views, view{"detail links", func(t *testing.T) *App {
		app := detailOnLinkedNib(t, 2, 120, 40)
		if !app.detail.linksActive {
			t.Fatal("premise failed: the detail view did not focus its links")
		}
		return app
	}, viewDetail, func(a *App) list.Model { return a.detail.linkList }})

	for _, v := range views {
		t.Run(v.name, func(t *testing.T) {
			app := v.setup(t)
			if app.state != v.state {
				t.Fatalf("premise failed: state = %d, want %d", app.state, v.state)
			}
			app.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
			if got := v.filter(app).FilterState(); got != list.Filtering {
				t.Fatalf("premise failed: filter state = %v after /, want filtering", got)
			}

			_, quitCmd := app.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
			app.Update(tea.KeyPressMsg{Code: '?', Text: "?"})

			if app.state != v.state {
				t.Errorf("state = %d after typing q and ?, want %d", app.state, v.state)
			}
			if got := v.filter(app).FilterValue(); got != "q?" {
				t.Errorf("filter text = %q, want %q", got, "q?")
			}
			if quitCmd != nil {
				if _, quit := runCmdNonBlocking(quitCmd).(tea.QuitMsg); quit {
					t.Error("typing q into the filter quit the app")
				}
			}
		})
	}
}

// runCmdNonBlocking returns cmd's message, or nil when cmd blocks (a cursor
// blink does).
func runCmdNonBlocking(cmd tea.Cmd) tea.Msg {
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		return msg
	case <-time.After(10 * time.Millisecond):
		return nil
	}
}

// frameView is one of the three compositions a frame is drawn in. Each pays
// for its footer out of a different frame.
type frameView struct {
	name  string
	enter func(t *testing.T, app *App, width int)
}

func frameViews() []frameView {
	return []frameView{
		{name: "list"},
		{
			name: "detail",
			enter: func(t *testing.T, app *App, _ int) {
				sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
				if app.state != viewDetail {
					t.Fatalf("premise failed: expected the detail view, got state %d", app.state)
				}
			},
		},
		{
			// z p puts the nib view beside the list — a different
			// composition, with its own footer arithmetic.
			name: "two-column",
			enter: func(t *testing.T, app *App, width int) {
				pressKeys(app, "z p")
				if want := width >= previewMinWidth; app.isTwoColumnMode() != want {
					t.Fatalf("premise failed: two-column mode = %v at width %d, want %v", app.isTwoColumnMode(), width, want)
				}
			},
		},
	}
}

// enterFrameView drives the real key path into one composition at a given
// terminal size.
func enterFrameView(t *testing.T, view frameView, width, height int) *App {
	t.Helper()
	app := setupRealBackendApp(t, queueRefusalNibs())
	app.Update(tea.WindowSizeMsg{Width: width, Height: height})
	if !focusMilestone(app, "ms1") {
		t.Fatal("premise failed: could not focus the milestone")
	}
	if view.enter != nil {
		view.enter(t, app, width)
	}
	return app
}

// refuseStatusChange drives a status change the model refuses, so the view is
// holding a message the footer has to find room for.
func refuseStatusChange(t *testing.T, app *App, view frameView) {
	t.Helper()
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

// The palette is a modal over the view it was opened from, and has as many
// lines as that view has actions, so it scrolls instead of running past the
// terminal.
func TestThePaletteFitsTheTerminal(t *testing.T) {
	for _, height := range sweepHeights {
		for _, width := range sweepWidths {
			for _, view := range frameViews() {
				t.Run(fmt.Sprintf("%s/%dx%d", view.name, width, height), func(t *testing.T) {
					app := enterFrameView(t, view, width, height)
					pressKeys(app, "?")
					if app.state != viewPalette {
						t.Fatalf("premise failed: ? opened state %d, not the palette", app.state)
					}
					content := app.View().Content
					assertFrameFitsTerminal(t, content, width, height)
					if painted := frameText(content, width, height); !strings.Contains(painted, "Actions") {
						t.Errorf("the palette is not on screen:\n%s", painted)
					}
				})
			}
		}
	}
}

// minSupportedHeight is the shortest terminal nibs undertakes to draw a usable
// frame into. It is a decision rather than a measurement of what happens to
// work, and the arithmetic behind it is the detail view at its most demanding:
// the header's four rows, a status message wrapped onto two, and the hint row
// come to seven, so eight is the first height that carries all three and still
// leaves a row for the content between them.
//
// Every geometry sweep floors here, so the supported height is one value to
// change rather than a literal repeated per sweep. Heights below it are out of
// scope, not merely untested.
const minSupportedHeight = 8

// heightSweep is the shape every geometry sweep takes: contiguous from the
// supported floor up to dense, then whatever sparse heights follow.
//
// Contiguous at the bottom because that is where this layout's thresholds are.
// The header, the links box and the body each stop fitting within a few rows of
// each other, so a sweep that steps over the range decides nothing about it —
// twice a frame was found broken by hand at a height a sampling sweep jumped
// clean over. Sparse above, where nothing changes shape and each extra height
// only costs runtime.
func heightSweep(dense int, sparse ...int) []int {
	heights := make([]int, 0, dense-minSupportedHeight+1+len(sparse))
	for h := minSupportedHeight; h <= dense; h++ {
		heights = append(heights, h)
	}
	return append(heights, sparse...)
}

// The geometries the frame arithmetic turns on. Widths start at 30 because that
// is where the narrow-terminal defects live: a row too long for its column, a
// footer built from a fixed set of key/label pairs, and a rule or header whose
// parts are drawn whether or not the width can hold them.
var (
	sweepWidths  = []int{30, 34, 38, 40, 46, 48, 80, 100, 120, 160, 200}
	sweepHeights = heightSweep(16, 20, 24, 30)
)

// assertFrameFitsTerminal fails for a frame that does not fit the cell grid the
// alt screen paints it into.
//
// Width is measured on the RAW content, not on what frameText reads back: that
// helper clips each line with MaxWidth, which is exactly what the terminal does
// to the overflow — so reading the painted frame would hide every column that
// ran off the right edge.
func assertFrameFitsTerminal(t *testing.T, content string, width, height int) {
	t.Helper()
	if got := lipgloss.Height(content); got > height {
		t.Errorf("frame is %d lines tall for a %d-line terminal — the bottom is clipped:\n%s",
			got, height, frameText(content, width, height))
	}
	if got := lipgloss.Width(content); got > width {
		var overflow []string
		for i, line := range strings.Split(content, "\n") {
			if w := lipgloss.Width(line); w > width {
				overflow = append(overflow, fmt.Sprintf("  line %d is %d cells: %q", i, w, ansiSGR.ReplaceAllString(line, "")))
			}
		}
		t.Errorf("frame is %d columns wide for a %d-column terminal — the right edge is cut:\n%s",
			got, width, strings.Join(overflow, "\n"))
	}
}

// A row too long for the box it is rendered into wraps through the border and
// pushes the whole frame past the last row, whatever height the box was handed.
func TestTheFrameFitsTheTerminal(t *testing.T) {
	for _, refused := range []bool{false, true} {
		for _, height := range sweepHeights {
			for _, width := range sweepWidths {
				for _, view := range frameViews() {
					name := fmt.Sprintf("%s/%dx%d", view.name, width, height)
					if refused {
						name += "/refused"
					}
					t.Run(name, func(t *testing.T) {
						app := enterFrameView(t, view, width, height)
						if refused {
							refuseStatusChange(t, app, view)
						}
						assertFrameFitsTerminal(t, app.View().Content, width, height)
					})
				}
			}
		}
	}
}

// The detail body is what gives when the frame cannot hold everything: below
// the height where the header, a box around one viewport row and the footer all
// fit, the body is not drawn at all. What it gives its rows up for is the
// refusal and the keys to act on it, so those are the property — a frame that
// merely fits by dropping the message back off the bottom would be the defect
// again with a smaller frame.
//
// Heights stop where sweepHeights turns sparse instead of running its tail: the
// property is about the short end, where the body is what gives and the
// message's own cap moves with height/3, so every row from the floor up to
// where all three regions fit is swept and the tall geometries buy nothing.
func TestTheDetailBodyGivesItsRowsToTheRefusal(t *testing.T) {
	for _, width := range []int{30, 80, 200} {
		for height := minSupportedHeight; height <= 16; height++ {
			t.Run(fmt.Sprintf("%dx%d", width, height), func(t *testing.T) {
				view := detailSweepView(t)
				app := enterFrameView(t, view, width, height)
				refuseStatusChange(t, app, view)
				content := app.View().Content

				assertFrameFitsTerminal(t, content, width, height)

				painted := frameText(content, width, height)
				if !strings.Contains(painted, "Status change failed") {
					t.Errorf("the frame does not carry the refusal:\n%s", painted)
				}
				// An early hint, not a late one: the row is clipped to the
				// terminal, so at thirty columns q is off the right edge
				// whatever the height.
				if !strings.Contains(painted, "e edit") {
					t.Errorf("the frame does not carry the hint row the refusal is acted on from:\n%s", painted)
				}
			})
		}
	}
}

// detailSweepView returns the detail entry from frameViews, so this file
// has one definition of how the detail view is entered.
func detailSweepView(t *testing.T) frameView {
	t.Helper()
	for _, v := range frameViews() {
		if v.name == "detail" {
			return v
		}
	}
	t.Fatal("premise failed: frameViews no longer carries a detail view")
	return frameView{}
}

// The rules are drawn by hand, so nothing downstream keeps them inside the
// pane. The top rule carries the project name, which comes from the config and
// has no bound.
func TestTheRulesFillThePaneExactly(t *testing.T) {
	titles := map[string]string{
		"short": "Nibs - Nibs",
		"long":  "Nibs - " + strings.Repeat("a-long-project-name/", 4),
	}
	for name, title := range titles {
		for _, width := range []int{1, 4, 5, 6, 20, 24, 30, 34, 38, 46, 80, 200} {
			t.Run(fmt.Sprintf("%s/%d", name, width), func(t *testing.T) {
				m := listModel{width: width, title: title}
				for rule, line := range map[string]string{"top": m.topRule(width), "bottom": m.bottomRule(width)} {
					if got := lipgloss.Width(line); got != width {
						t.Errorf("the %s rule is %d cells in a %d-cell pane: %q", rule, got, width, ansiSGR.ReplaceAllString(line, ""))
					}
				}
			})
		}
	}
}

// A tag filter can empty the list, and the header's token is then the only
// thing on screen that says why. It comes before the default -status:closed
// and the count, which give way to it in a narrow header.
func TestTheTagFilterTokenOutlivesTheRestOfTheHeader(t *testing.T) {
	for _, width := range []int{30, 34, 38, 46, 80, 200} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := newListModel(&StubBackend{}, config.Default())
			m.width, m.tagFilter = width, "needs-triage"
			line := ansiSGR.ReplaceAllString(m.header(width), "")
			if !strings.Contains(line, "tags:needs-triage") {
				t.Errorf("the tag filter token is absent at %d columns: %q", width, line)
			}
			if got := lipgloss.Width(line); got > width {
				t.Errorf("the header is %d cells for a %d-column terminal: %q", got, width, line)
			}
		})
	}
}

// A title the rule cannot hold is cut, and the cut has to be visible: a
// silently shortened project name reads as a whole one, and the reader has no
// way to tell `Nibs - sample-` from a project actually called that.
func TestATruncatedTitleSaysItWasTruncated(t *testing.T) {
	const title = "Nibs - long-name/long-name/long-name/long-name/"
	m := listModel{width: 30, title: title}
	line := ansiSGR.ReplaceAllString(m.topRule(30), "")
	if strings.Contains(line, title) {
		t.Fatalf("premise failed: the whole title fits at 30 columns: %q", line)
	}
	if !strings.Contains(line, "…") {
		t.Errorf("the title was cut with nothing on screen to say so: %q", line)
	}
}

// widestVocabularyNibs is a milestone and a task queued on it, both
// in-progress, for checking that the list pane fits the terminal at every size.
func widestVocabularyNibs() []*nib.Nib {
	return []*nib.Nib{
		{ID: "ms1", Title: "Wave one", Type: "milestone", Status: "in-progress"},
		{ID: "t1", Title: "Still open", Type: "task", Status: "in-progress", Milestone: "ms1", MilestoneOrder: "a0"},
	}
}

// A title budget is not the whole of what makes a row fit. The columns around
// it are fixed-width and independent of the terminal, so a row can be wider than
// the pane with its title already cut to nothing.
func TestTheListPaneHoldsARowWiderThanItself(t *testing.T) {
	for _, height := range sweepHeights {
		for _, width := range sweepWidths {
			t.Run(fmt.Sprintf("%dx%d", width, height), func(t *testing.T) {
				app := setupRealBackendApp(t, widestVocabularyNibs())
				app.Update(tea.WindowSizeMsg{Width: width, Height: height})
				assertFrameFitsTerminal(t, app.View().Content, width, height)
			})
		}
	}
}
