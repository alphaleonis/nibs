package tui

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
)

// everyRelationNibs holds hub, a feature with a nib in every relation group: a
// parent, two children, a blocker, a dependent, a nib its body mentions, a nib
// whose body mentions it, and a document. Its body starts with bodyMarker. rz
// has no relation at all.
func everyRelationNibs() []*nib.Nib {
	return []*nib.Nib{
		{ID: "rms", Title: "Wave one", Type: "milestone", Status: "in-progress", Order: "0"},
		{ID: "rp", Title: "Parent epic", Type: "epic", Status: "in-progress", Order: "1"},
		{ID: "rb", Title: "Blocker task", Type: "task", Status: "todo", Order: "2"},
		{ID: "rm", Title: "Mentioned note", Type: "task", Status: "completed", Order: "3"},
		{
			ID: "hub", Title: "Hub feature", Type: "feature", Status: "in-progress", Priority: "high", Estimate: "m",
			Tags: []string{"tui", "views"}, Parent: "rp", BlockedBy: []string{"rb"}, Milestone: "rms", MilestoneOrder: "a0",
			Documents: []string{"docs/nib-view.md"}, Order: "4",
			Body: bodyMarker + " opens the body and links [[rm]].\n\n## Notes\n\n- one\n- two\n",
		},
		{ID: "rc1", Title: "First child", Type: "task", Status: "todo", Parent: "hub", Order: "a"},
		{ID: "rc2", Title: "Second child", Type: "task", Status: "draft", Parent: "hub", Order: "b"},
		{ID: "rd", Title: "Dependent bug", Type: "bug", Status: "todo", BlockedBy: []string{"hub"}, Order: "5"},
		{ID: "rmb", Title: "Mentioning task", Type: "task", Status: "todo", Body: "See [[hub]].", Order: "6"},
		{ID: "rz", Title: "Lone task", Type: "task", Status: "todo", Order: "7"},
	}
}

const bodyMarker = "BODYTEXT"

// expandOn opens the expanded nib view of id on nibs, at width x height.
func expandOn(t *testing.T, nibs []*nib.Nib, id string, width, height int) *App {
	t.Helper()
	app := setupRealBackendApp(t, nibs)
	app.Update(tea.WindowSizeMsg{Width: width, Height: height})
	app.list.restoreCursor(id)
	if app.list.focusedID() != id {
		t.Fatalf("premise failed: could not focus %s", id)
	}
	pressKeys(app, "enter")
	if app.state != viewExpanded || app.expanded.nib.ID != id {
		t.Fatalf("premise failed: enter opened state %d, want the expanded view of %s", app.state, id)
	}
	return app
}

// railText returns what the frame draws right of the divider on each line, or
// "" for a line with no divider.
func railText(frame string) []string {
	lines := strings.Split(frame, "\n")
	out := make([]string, len(lines))
	for i, line := range lines {
		if j := strings.LastIndex(line, "│"); j >= 0 {
			out[i] = line[j+len("│"):]
		}
	}
	return out
}

// lineOf returns the first line of lines that holds sub, or -1.
func lineOf(lines []string, sub string) int {
	return slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, sub) })
}

func TestEveryRelationGroupIsDrawnWithItsNibs(t *testing.T) {
	app := expandOn(t, everyRelationNibs(), "hub", 140, 40)
	want := []struct {
		label   string
		members []string // the titles of the nibs, or the paths
	}{
		{"Parent", []string{"Parent epic"}},
		{"Children 2", []string{"First child", "Second child"}},
		{"Blocked by", []string{"Blocker task"}},
		{"Blocking", []string{"Dependent bug"}},
		{"Mentions", []string{"Mentioned note"}},
		{"Mentioned by", []string{"Mentioning task"}},
		{"Documents", []string{"docs/nib-view.md"}},
	}
	rail := railText(plainView(app))
	prev := -1
	for _, g := range want {
		heading := lineOf(rail, g.label)
		if heading <= prev {
			t.Fatalf("the %q heading is on rail line %d, want it after line %d:\n%s", g.label, heading, prev, strings.Join(rail, "\n"))
		}
		prev = heading
		for _, member := range g.members {
			line := lineOf(rail, member)
			if line != prev+1 {
				t.Errorf("%q is on rail line %d, want %d, right under %q or the member before it:\n%s", member, line, prev+1, g.label, strings.Join(rail, "\n"))
			}
			prev = max(prev, line)
		}
	}
}

func TestAnEmptyRelationGroupIsLeftOut(t *testing.T) {
	// rc1 has a parent and nothing else.
	app := expandOn(t, everyRelationNibs(), "rc1", 140, 40)
	rail := strings.Join(railText(plainView(app)), "\n")
	if !strings.Contains(rail, "Parent") || !strings.Contains(rail, "Hub feature") {
		t.Fatalf("premise failed: the rail lacks the parent:\n%s", rail)
	}
	for _, label := range []string{"Children", "Blocked by", "Blocking", "Mentions", "Mentioned by", "Documents"} {
		if strings.Contains(rail, label) {
			t.Errorf("the rail draws the empty %q group:\n%s", label, rail)
		}
	}

	// A nib with no relation at all gets no rail: its body takes the width.
	app = expandOn(t, everyRelationNibs(), "rz", 140, 40)
	if frame := plainView(app); strings.Contains(frame, "│") {
		t.Errorf("a nib without relations draws a divider:\n%s", frame)
	}
}

// The rail threshold is railMinWidth.
func TestTheRelationsSitBesideTheBodyWhenWideAndAboveItWhenNarrow(t *testing.T) {
	for _, width := range []int{railMinWidth, 140, 200} {
		t.Run(fmt.Sprintf("wide %d", width), func(t *testing.T) {
			lines := strings.Split(plainView(expandOn(t, everyRelationNibs(), "hub", width, 40)), "\n")
			body := lineOf(lines, bodyMarker)
			parent := lineOf(lines, "Parent epic")
			if body < 0 || parent < 0 {
				t.Fatalf("premise failed: the body or the parent is not on screen:\n%s", strings.Join(lines, "\n"))
			}
			if cellOf(lines[parent], "Parent epic") <= cellOf(lines[body], bodyMarker) {
				t.Errorf("the parent's row starts left of the body, so the relations are not in a rail beside it:\n%s", strings.Join(lines, "\n"))
			}
			if last := lineOf(lines, "docs/nib-view.md"); last < body {
				t.Errorf("the relations end on line %d, above the body on line %d, so they are not beside it:\n%s", last, body, strings.Join(lines, "\n"))
			}
		})
	}
	for _, width := range []int{40, 80, railMinWidth - 1} {
		t.Run(fmt.Sprintf("narrow %d", width), func(t *testing.T) {
			lines := strings.Split(plainView(expandOn(t, everyRelationNibs(), "hub", width, 60)), "\n")
			body := lineOf(lines, bodyMarker)
			if body < 0 {
				t.Fatalf("premise failed: the body is not on screen:\n%s", strings.Join(lines, "\n"))
			}
			for _, member := range []string{"Parent epic", "Second child", "Mentioning task", "docs/nib-view.md"} {
				if line := lineOf(lines, member); line < 0 || line >= body {
					t.Errorf("%q is on line %d, want it above the body on line %d:\n%s", member, line, body, strings.Join(lines, "\n"))
				}
			}
			if strings.Contains(lines[body], "│") {
				t.Errorf("the body shares its line with a divider at %d columns:\n%s", width, lines[body])
			}
		})
	}
}

// actionTreeNibs is a feature, its parent and its child, and a fourth nib for
// the pickers to offer.
func actionTreeNibs() []*nib.Nib {
	return []*nib.Nib{
		{ID: "nib-1", Title: "Epic", Type: "epic", Status: "todo", Order: "1"},
		{ID: "nib-2", Title: "Feature", Type: "feature", Status: "todo", Parent: "nib-1", Order: "2"},
		{ID: "nib-3", Title: "Task", Type: "task", Status: "todo", Parent: "nib-2", Order: "3"},
		{ID: "nib-4", Title: "Other", Type: "task", Status: "todo", Order: "4"},
	}
}

// followedToChild expands nib-2 and follows its relation to nib-3, then puts
// the list cursor back on nib-2, so the view shows a nib other than the one
// under the list cursor.
func followedToChild(t *testing.T) (*App, *StubBackend) {
	t.Helper()
	app, stub := setupTestApp(t, actionTreeNibs())
	app.list.restoreCursor("nib-2")
	pressKeys(app, "enter")
	if app.state != viewExpanded || app.expanded.focus != regionRelations {
		t.Fatalf("premise failed: enter opened state %d focused on %d, want the relations of nib-2", app.state, app.expanded.focus)
	}
	pressKeys(app, "j enter")
	app.list.restoreCursor("nib-2")
	if app.expanded.nib.ID != "nib-3" || app.list.focusedID() != "nib-2" {
		t.Fatalf("premise failed: the view shows %s with the list on %s, want nib-3 over nib-2", app.expanded.nib.ID, app.list.focusedID())
	}
	return app, stub
}

// Each picker opens through App.showView, before any command runs.
func TestExpandedViewActionsWriteToTheNibOnScreen(t *testing.T) {
	pickers := []struct {
		keys  string
		state viewState
		ids   func(app *App) []string
	}{
		{"s", viewStatusPicker, func(a *App) []string { return a.statusPicker.nibIDs }},
		{"t", viewTypePicker, func(a *App) []string { return a.typePicker.nibIDs }},
		{"p", viewParentPicker, func(a *App) []string { return a.parentPicker.nibIDs }},
		{"b", viewBlockingPicker, func(a *App) []string { return []string{a.blockingPicker.nibID} }},
		{"m", viewModifyPicker, func(a *App) []string { return a.modifyPicker.targets.ids() }},
		{"m p", viewPriorityPicker, func(a *App) []string { return a.priorityPicker.nibIDs }},
	}
	for _, p := range pickers {
		t.Run(p.keys, func(t *testing.T) {
			app, _ := followedToChild(t)
			typeAhead(app, p.keys)
			if app.state != p.state {
				t.Fatalf("%s opened state %d, want %d", p.keys, app.state, p.state)
			}
			if got := p.ids(app); !slices.Equal(got, []string{"nib-3"}) {
				t.Errorf("%s targets %v, want [nib-3]", p.keys, got)
			}
		})
	}

	t.Run("status write", func(t *testing.T) {
		app, stub := followedToChild(t)
		pressKeys(app, "s")
		selectStatus(t, app, "completed")
		if len(stub.UpdateCalls) != 1 || stub.UpdateCalls[0].ID != "nib-3" {
			t.Errorf("the status write went to %+v, want nib-3", stub.UpdateCalls)
		}
		if app.state != viewExpanded || app.expanded.nib.ID != "nib-3" {
			t.Errorf("after the write the app is in state %d on %v, want the expanded view of nib-3", app.state, app.expanded.nib)
		}
	})

	t.Run("e", func(t *testing.T) {
		app, _ := followedToChild(t)
		_, cmd := app.Update(keyPress("e"))
		if cmd == nil {
			t.Fatal("e returned no command")
		}
		if msg, ok := cmd().(openEditorMsg); !ok || msg.nibID != "nib-3" {
			t.Errorf("e sent %#v, want an editor on nib-3", msg)
		}
	})

	t.Run("y", func(t *testing.T) {
		app, _ := followedToChild(t)
		var copied string
		orig := clipboardWriteAll
		t.Cleanup(func() { clipboardWriteAll = orig })
		clipboardWriteAll = func(s string) error { copied = s; return nil }
		pressKeys(app, "y")
		if copied != "nib-3" {
			t.Errorf("y copied %q, want nib-3", copied)
		}
	})
}

// esc returns to the list from a followed link. backspace steps back to the
// nib the link was followed from, and no further.
func TestEscLeavesTheNibViewAndBackspaceStepsBack(t *testing.T) {
	app, _ := followedToChild(t)
	pressKeys(app, "backspace")
	if app.state != viewExpanded || app.expanded.nib.ID != "nib-2" {
		t.Fatalf("backspace left state %d on %v, want the expanded view of nib-2", app.state, app.expanded.nib)
	}
	pressKeys(app, "backspace")
	if app.state != viewExpanded || app.expanded.nib.ID != "nib-2" {
		t.Errorf("a second backspace left state %d on %v, want the expanded view of nib-2", app.state, app.expanded.nib)
	}

	app, _ = followedToChild(t)
	pressKeys(app, "esc")
	if app.state != viewList {
		t.Errorf("esc left state %d, want the list", app.state)
	}
}

// The docked pane is drawn by the same component, so it carries the meta band
// and every relation group the expanded view does.
func TestTheDockedNibViewDrawsTheMetaBandAndRelations(t *testing.T) {
	app := setupRealBackendApp(t, everyRelationNibs())
	app.Update(tea.WindowSizeMsg{Width: 160, Height: 60})
	pressKeys(app, "z p")
	app.list.restoreCursor("hub")
	_, cmd := app.Update(cursorChangedMsg{})
	processCmd(app, cmd)
	if app.dockPlacement() != placementRight {
		t.Fatal("premise failed: z p did not dock the nib view")
	}
	pane := strings.Join(railText(plainView(app)), "\n")
	for _, want := range []string{
		"Status", "Priority", "Estimate", "Milestone Wave one", "Tags",
		"Parent", "Children 2", "Blocked by", "Blocking", "Mentions", "Mentioned by", "Documents",
		"Dependent bug", bodyMarker,
	} {
		if !strings.Contains(pane, want) {
			t.Errorf("the docked pane lacks %q:\n%s", want, pane)
		}
	}
}

// The expanded view of hub, without color. Regenerate with `-update`.
func TestNibViewGolden(t *testing.T) {
	for _, width := range []int{80, 140} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			golden.RequireEqual(t, plainView(expandOn(t, everyRelationNibs(), "hub", width, 40)))
		})
	}
}

// Glamour v2 emits the document's left margin as plain spaces before any escape
// sequence, so trimming whitespace would eat the first line's indent.
func TestRenderBodyKeepsFirstLineIndent(t *testing.T) {
	b := &nib.Nib{ID: "nibs-test", Title: "Indent", Status: "todo", Type: "task", Body: "## Description\n\nSome body text.\n"}
	m := newNibViewModel(b, &StubBackend{Nibs: map[string]*nib.Nib{b.ID: b}}, config.Default(), display{icons: iconsUnicode}, 120, 40)

	out := m.renderBody(glamourKey{width: 100})
	lines := strings.Split(out, "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatalf("renderBody produced no first line: %q", out)
	}
	if !strings.HasPrefix(lines[0], " ") {
		t.Errorf("first rendered line lost its left margin: %q", lines[0])
	}
	if strings.HasPrefix(out, "\n") {
		t.Errorf("renderBody should still strip glamour's leading blank line: %q", out)
	}
}

func TestMarkdownBodyWrapsToTheBodyWidth(t *testing.T) {
	body := strings.Repeat("lorem ipsum dolor sit amet ", 12)
	b := &nib.Nib{ID: "nibs-test", Title: "Wrap", Status: "todo", Type: "task", Body: body}
	for _, width := range []int{50, 100, 160} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := newNibViewModel(b, &StubBackend{Nibs: map[string]*nib.Nib{b.ID: b}}, config.Default(), display{icons: iconsUnicode}, width, 40)
			out := m.renderBody(glamourKey{width: width})
			assertLinesFit(t, out, width)
			if width > 80 && lipgloss.Height(out) >= lipgloss.Height(m.renderBody(glamourKey{width: 50})) {
				t.Errorf("body at %d cells is as tall as at 50: it is not using the wider pane", width)
			}
		})
	}
}

func assertLinesFit(t *testing.T, s string, width int) {
	t.Helper()
	for _, line := range strings.Split(s, "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("line is %d cells wide, pane holds %d: %q", w, width, stripAnsi(line))
			return
		}
	}
}

// A body of only blank lines renders to nothing, so the placeholder keys off
// what the body renders to.
func TestAWhitespaceOnlyBodyStillSaysNoDescription(t *testing.T) {
	for _, body := range []string{"", "\n", "\n\n", "\n\n\n", "   ", "\t\n  \n"} {
		t.Run(fmt.Sprintf("%q", body), func(t *testing.T) {
			m := nibViewModel{nib: &nib.Nib{ID: "x", Title: "T", Body: body}}
			if got := m.renderBody(glamourKey{width: 80}); !strings.Contains(got, "No description") {
				t.Errorf("a body of %q did not get the placeholder; rendered %q", body, got)
			}
		})
	}
}

// detailScrollPctRe reads the percentage off a painted frame. The footer's hint
// row is the frame's last line and the percentage is the first thing on it, so
// the last match is the one the reader sees.
var detailScrollPctRe = regexp.MustCompile(`(\d+)%`)

func detailScrollPct(t *testing.T, painted string) int {
	t.Helper()
	all := detailScrollPctRe.FindAllStringSubmatch(painted, -1)
	if len(all) == 0 {
		t.Fatalf("the frame carries no scroll percentage:\n%s", painted)
	}
	pct, err := strconv.Atoi(all[len(all)-1][1])
	if err != nil {
		t.Fatalf("reading the scroll percentage: %v", err)
	}
	return pct
}

// The percentage is measured against the body the frame paints. A body
// entirely on screen once reported 0%, and the opposite would be as wrong, so
// both directions are checked.
func TestTheDetailFooterMeasuresTheBodyItPaints(t *testing.T) {
	const width = 100
	for _, height := range sweepHeights {
		for paras := 1; paras <= 14; paras++ {
			t.Run(fmt.Sprintf("%dx%d/%dp", width, height, paras), func(t *testing.T) {
				lines := make([]string, paras)
				for i := range lines {
					lines[i] = fmt.Sprintf("L%02d", i)
				}
				app, _ := setupTestApp(t, []*nib.Nib{{
					ID: "nib-1", Title: "Scrolled", Type: "task", Status: "todo", Order: "1",
					Body: strings.Join(lines, "\n\n"),
				}})
				app.Update(tea.WindowSizeMsg{Width: width, Height: height})
				sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
				if app.state != viewExpanded {
					t.Fatalf("premise failed: expected the expanded view, got state %d", app.state)
				}

				painted := frameText(app.View().Content, width, height)
				if !strings.Contains(painted, "L00") {
					t.Skip("no body row is painted at this geometry")
				}
				pct := detailScrollPct(t, painted)
				if strings.Contains(painted, lines[paras-1]) {
					if pct != 100 {
						t.Errorf("every body line is on screen but the footer reads %d%%:\n%s", pct, painted)
					}
					return
				}
				if pct == 100 {
					t.Errorf("the footer reads 100%% while %q is off screen:\n%s", lines[paras-1], painted)
				}
			})
		}
	}
}

// A related nib's title is cut with an ellipsis rather than clipped at the
// edge.
func TestARelationRowTruncatesItsTitle(t *testing.T) {
	nibs := []*nib.Nib{
		{ID: "p1", Title: strings.Repeat("Long parent title ", 12), Status: "todo", Type: "epic", Order: "1"},
		{ID: "c1", Title: "Child", Status: "todo", Type: "feature", Parent: "p1", Order: "2"},
	}
	for _, width := range []int{60, 100, 160} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			lines := strings.Split(plainView(expandOn(t, nibs, "c1", width, 30)), "\n")
			row := lineOf(lines, "Long parent title")
			if row < 0 {
				t.Fatalf("premise failed: no relation row for p1:\n%s", strings.Join(lines, "\n"))
			}
			if !strings.Contains(lines[row], "…") {
				t.Errorf("the relation row was clipped rather than truncated: %q", lines[row])
			}
		})
	}
}

// Moving the relations cursor past the rows a short stacked view gives them
// scrolls them, so the selected relation stays on screen.
func TestTheSelectedRelationStaysOnScreen(t *testing.T) {
	app := detailOnLinkedNib(t, 12, 80, 20)
	if app.expanded.focus != regionRelations {
		t.Fatal("premise failed: the relations are not focused")
	}
	ns := app.expanded.relationNibs()
	if last := ns[len(ns)-1].ID; strings.Contains(plainView(app), last) {
		t.Fatalf("premise failed: every relation fits, %s included:\n%s", last, plainView(app))
	}
	for range 11 {
		pressKeys(app, "j")
		id := app.expanded.selectedRelation().ID
		lines := strings.Split(plainView(app), "\n")
		if line := lineOf(lines, id); line < 0 || !strings.Contains(lines[line], "▸") {
			t.Fatalf("the selected relation %s is not on screen with the cursor on it:\n%s", id, strings.Join(lines, "\n"))
		}
	}
}

// linkedNibs is a nib with n nibs blocked by it, so the expanded view opens on
// relations whose height the caller chooses.
func linkedNibs(n int) []*nib.Nib {
	nibs := []*nib.Nib{{ID: "hub", Title: "Hub", Type: "task", Status: "todo", Order: "1"}}
	for i := 0; i < n; i++ {
		nibs = append(nibs, &nib.Nib{
			ID:        fmt.Sprintf("lk%d", i),
			Title:     fmt.Sprintf("Blocker %d", i),
			Type:      "task",
			Status:    "todo",
			Order:     fmt.Sprintf("%d", i+2),
			BlockedBy: []string{"hub"},
		})
	}
	return nibs
}

// linkedNibsWithBody is linkedNibs whose hub carries a body taller than any
// viewport these tests give it, so a key that reaches the body moves the frame.
func linkedNibsWithBody(n int) []*nib.Nib {
	nibs := linkedNibs(n)
	lines := make([]string, 40)
	for i := range lines {
		lines[i] = fmt.Sprintf("L%02d", i)
	}
	nibs[0].Body = strings.Join(lines, "\n\n") // the hub, which linkedNibs puts first
	return nibs
}

// detailOnLinkedNib drives the real key path into the expanded view of a nib
// with n relations, at a given terminal size.
func detailOnLinkedNib(t *testing.T, n, width, height int) *App {
	t.Helper()
	return detailOnHub(t, linkedNibs(n), n, width, height)
}

// detailOnHub is detailOnLinkedNib for a hub the caller built.
func detailOnHub(t *testing.T, nibs []*nib.Nib, wantLinks, width, height int) *App {
	t.Helper()
	app := setupRealBackendApp(t, nibs)
	app.Update(tea.WindowSizeMsg{Width: width, Height: height})
	if !focusOn(app, "hub") {
		t.Fatal("premise failed: could not focus the linked nib")
	}
	sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.state != viewExpanded {
		t.Fatalf("premise failed: expected the expanded view, got state %d", app.state)
	}
	if got := len(app.expanded.relationNibs()); got != wantLinks {
		t.Fatalf("premise failed: %d relations resolved, want %d", got, wantLinks)
	}
	return app
}

// The relations hold the focus only while the frame draws them. Where they are
// drawn, enter follows the selected one. Where they are not, keys go to the
// body and enter follows nothing.
//
// Each geometry is reached twice: opened at that size, and shrunk to it from a
// size that drew the relations.
func TestTheDetailViewFocusesARegionTheFrameDrew(t *testing.T) {
	var relationsDrawn, relationsDropped int

	arrivals := []struct {
		name string
		open func(t *testing.T, links, width, height int) *App
	}{
		{"opened", func(t *testing.T, links, width, height int) *App {
			return detailOnHub(t, linkedNibsWithBody(links), links, width, height)
		}},
		{"resized", func(t *testing.T, links, width, height int) *App {
			const tall = 24
			app := detailOnHub(t, linkedNibsWithBody(links), links, width, tall)
			if app.expanded.focus != regionRelations {
				t.Fatalf("premise failed: the view did not open on the relations at %d rows", tall)
			}
			app.Update(tea.WindowSizeMsg{Width: width, Height: height})
			return app
		}},
	}

	for _, arrival := range arrivals {
		for _, refused := range []bool{false, true} {
			for _, links := range []int{2, 4, 6} {
				for _, width := range []int{30, 40, 80, 120, 200} {
					for height := minSupportedHeight; height <= 16; height++ {
						t.Run(fmt.Sprintf("%s/refused=%v/%dlinks/%dx%d", arrival.name, refused, links, width, height), func(t *testing.T) {
							app := arrival.open(t, links, width, height)
							if refused {
								// A refusal's rows come out of the content.
								app.reportFailure("Status change failed: " + queueRefusal())
							}
							before := frameText(app.View().Content, width, height)

							if strings.Contains(before, "lk0") {
								relationsDrawn++
								sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
								if got := app.expanded.nib.ID; got == "hub" {
									t.Errorf("the relations are on screen, yet enter did not follow the selected one:\n%s", before)
								}
								return
							}
							relationsDropped++
							if got := app.keyContext(); got != ctxNibBody {
								t.Errorf("the relations are not on screen, yet keys go to the %s:\n%s", got, before)
							}
							sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
							if got := app.expanded.nib.ID; got != "hub" {
								t.Errorf("enter followed a relation the frame never drew — the view is on %s now:\n%s", got, before)
							}
						})
					}
				}
			}
		}
	}

	for _, c := range []struct {
		name string
		n    int
	}{
		{"geometries with the relations drawn", relationsDrawn},
		{"geometries with the relations dropped", relationsDropped},
	} {
		if c.n == 0 {
			t.Errorf("premise failed: no %s, so the sweep only saw one side of the property", c.name)
		}
	}
}

// sendKeyNonBlocking sends a key the way sendKey does, but skips a command that
// does not return, such as a text input's cursor blink.
func sendKeyNonBlocking(app *App, key tea.KeyPressMsg) {
	_, cmd := app.Update(key)
	processCmdNonBlocking(app, cmd)
}

// At eight rows a refusal and the relations cannot both be drawn. The
// relations give way, because the refusal and the hint row it is acted on from
// are what the reader needs.
func TestTheRelationsGiveTheirRowsToARefusal(t *testing.T) {
	const (
		width  = 80
		height = 8
	)
	view := detailSweepView(t)
	app := enterFrameView(t, view, width, height)
	refuseStatusChange(t, app, view)

	content := app.View().Content
	assertFrameFitsTerminal(t, content, width, height)

	painted := frameText(content, width, height)
	if !strings.Contains(painted, "Status change failed") {
		t.Errorf("the frame does not carry the refusal:\n%s", painted)
	}
	if !strings.Contains(painted, "e edit") {
		t.Errorf("the frame does not carry the hint row the refusal is acted on from:\n%s", painted)
	}
}

// detailRelationsOnScreen reports whether ms1's relation reached the screen.
// b1 is its only relation and its id appears nowhere else in the frame.
func detailRelationsOnScreen(painted string) bool {
	return strings.Contains(painted, "b1")
}

// detailBodyOnScreen reports whether the body reached the screen. ms1 has no
// body, so the placeholder is the whole of what the body draws.
func detailBodyOnScreen(painted string) bool {
	return strings.Contains(painted, "No description")
}

// detailRegionsDrawn is which regions View paints, taken from the layout View
// uses. The sweep checks the sentinels against it, to know the frame is still
// legible enough to read them off.
func detailRegionsDrawn(t *testing.T, app *App) (relations, body bool) {
	t.Helper()
	if app.state != viewExpanded {
		t.Fatalf("premise failed: expected the expanded view, got state %d", app.state)
	}
	l := app.expanded.layout(app.expanded.width, app.expanded.contentHeight())
	return l.relRows > 0, l.bodyRows > 0
}

// detailFooterGeometries are sizes where the relations or the body are dropped
// and where both are drawn, each with a refusal, whose wrapped rows move the
// thresholds.
func detailFooterGeometries() []struct{ width, height int } {
	var geos []struct{ width, height int }
	for _, width := range []int{30, 38, 46, 80, 120, 200} {
		for height := minSupportedHeight; height <= 20; height++ {
			geos = append(geos, struct{ width, height int }{width, height})
		}
	}
	return geos
}

// The footer describes the frame in front of the reader: it names tab only
// when the relations are drawn, and prints a scroll position only when the body
// is. The sweep has to see each piece both drawn and withheld.
func TestTheDetailFooterDescribesTheFrameItPaints(t *testing.T) {
	var relationsDrawn, relationsDropped, bodyDrawn, bodyDropped int

	for _, geo := range detailFooterGeometries() {
		t.Run(fmt.Sprintf("%dx%d", geo.width, geo.height), func(t *testing.T) {
			view := detailSweepView(t)
			app := enterFrameView(t, view, geo.width, geo.height)
			refuseStatusChange(t, app, view)

			content := app.View().Content
			assertFrameFitsTerminal(t, content, geo.width, geo.height)
			painted := frameText(content, geo.width, geo.height)
			row := detailHintRow(t, app)
			wantRelations, wantBody := detailRegionsDrawn(t, app)

			relations := detailRelationsOnScreen(painted)
			if relations != wantRelations {
				t.Fatalf("premise failed: the relations sentinel says on screen = %v where View paints them = %v:\n%s", relations, wantRelations, painted)
			}
			if relations {
				relationsDrawn++
			} else {
				relationsDropped++
			}
			if got := strings.Contains(row, "tab switch"); got != relations {
				t.Errorf("the footer names tab = %v with the relations on screen = %v:\n%s\n%s", got, relations, row, painted)
			}

			body := detailBodyOnScreen(painted)
			if body != wantBody {
				t.Fatalf("premise failed: the body sentinel says on screen = %v where View paints it = %v:\n%s", body, wantBody, painted)
			}
			if body {
				bodyDrawn++
			} else {
				bodyDropped++
			}
			if got := detailScrollPctRe.MatchString(row); got != body {
				t.Errorf("the footer reports a scroll position = %v with the body on screen = %v:\n%s\n%s", got, body, row, painted)
			}
		})
	}

	for _, c := range []struct {
		name string
		n    int
	}{
		{"geometries with the relations drawn", relationsDrawn},
		{"geometries with the relations dropped", relationsDropped},
		{"geometries with the body drawn", bodyDrawn},
		{"geometries with the body dropped", bodyDropped},
	} {
		if c.n == 0 {
			t.Errorf("premise failed: no %s, so the sweep only saw one side of the property", c.name)
		}
	}
}

// detailHintRow is the frame's last row: the footer's hint row as the terminal
// shows it.
func detailHintRow(t *testing.T, app *App) string {
	t.Helper()
	if app.state != viewExpanded {
		t.Fatalf("premise failed: expected the expanded view, got state %d", app.state)
	}
	lines := strings.Split(ansiSGR.ReplaceAllString(app.View().Content, ""), "\n")
	return lines[len(lines)-1]
}

// contentHeight measures the footer with a zero paintedFrame, so the footer's
// height must not depend on what View painted.
func TestTheDetailFooterRegionHeightIgnoresWhatWasPainted(t *testing.T) {
	for _, refused := range []bool{false, true} {
		for _, geo := range detailFooterGeometries() {
			name := fmt.Sprintf("%dx%d", geo.width, geo.height)
			if refused {
				name += "/refused"
			}
			t.Run(name, func(t *testing.T) {
				view := detailSweepView(t)
				app := enterFrameView(t, view, geo.width, geo.height)
				if refused {
					refuseStatusChange(t, app, view)
				}
				d := app.expanded
				base := lipgloss.Height(d.renderFooter(paintedFrame{}))
				if got := lipgloss.Height(d.renderFooter(paintedFrame{body: true})); got != base {
					t.Errorf("the footer is %d rows with the body painted and %d rows for the zero value", got, base)
				}
			})
		}
	}
}

// overflowNibs is hub with 25 children and a document: more relations than any
// layout below gives rows to.
func overflowNibs() []*nib.Nib {
	nibs := []*nib.Nib{{ID: "hub", Title: "Hub", Type: "epic", Status: "todo", Order: "1", Documents: []string{"docs/the-plan.md"}}}
	for i := range 25 {
		nibs = append(nibs, &nib.Nib{ID: fmt.Sprintf("ch%02d", i), Title: fmt.Sprintf("Child %02d", i), Type: "task", Status: "todo", Parent: "hub", Order: fmt.Sprintf("c%02d", i)})
	}
	return nibs
}

// Every relation group keeps its heading on screen when the relations
// overflow, and the cursor reaches every row, the document included.
func TestEveryRelationRowIsReachableWhenTheRelationsOverflow(t *testing.T) {
	for _, geo := range []struct {
		name          string
		width, height int
	}{{"rail", 140, 20}, {"stacked", 80, 30}} {
		t.Run(geo.name, func(t *testing.T) {
			app := expandOn(t, overflowNibs(), "hub", geo.width, geo.height)
			frame := plainView(app)
			if strings.Contains(frame, "Child 24") {
				t.Fatalf("premise failed: every relation fits:\n%s", frame)
			}
			if !strings.Contains(frame, "Documents") || !strings.Contains(frame, "the-plan.md") {
				t.Errorf("the Documents group is off screen:\n%s", frame)
			}
			for i := 1; i <= 25; i++ {
				pressKeys(app, "j")
				want := "the-plan.md"
				if i < 25 {
					want = fmt.Sprintf("Child %02d", i)
				}
				lines := strings.Split(plainView(app), "\n")
				if line := lineOf(lines, want); line < 0 || !strings.Contains(lines[line], "▸") {
					t.Fatalf("after %d moves %q is not on screen under the cursor:\n%s", i, want, strings.Join(lines, "\n"))
				}
			}
			if frame := plainView(app); !strings.Contains(frame, "Children 25") {
				t.Errorf("the Children heading left the screen with the cursor on the document:\n%s", frame)
			}
			pressKeys(app, "enter")
			if app.state != viewExpanded || app.expanded.nib.ID != "hub" {
				t.Errorf("enter on the document left state %d on %v, want the view of hub unchanged", app.state, app.expanded.nib)
			}
		})
	}
	t.Run("docked", func(t *testing.T) {
		app := setupRealBackendApp(t, overflowNibs())
		app.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
		app.list.restoreCursor("hub")
		pressKeys(app, "z p")
		pane := strings.Join(railText(plainView(app)), "\n")
		if strings.Contains(pane, "Child 24") || !strings.Contains(pane, "Child 00") {
			t.Fatalf("premise failed: the docked view does not overflow:\n%s", pane)
		}
		if !strings.Contains(pane, "the-plan.md") {
			t.Errorf("the docked view hides the Documents group:\n%s", pane)
		}
	})
}

// blockedPairNibs is hub blocking lka and lkb, which sort by title while their
// statuses match.
func blockedPairNibs() []*nib.Nib {
	return []*nib.Nib{
		{ID: "hub", Title: "Hub", Type: "task", Status: "todo", Order: "1"},
		{ID: "lka", Title: "Alpha", Type: "task", Status: "todo", Order: "2", BlockedBy: []string{"hub"}},
		{ID: "lkb", Title: "Bravo", Type: "task", Status: "todo", Order: "3", BlockedBy: []string{"hub"}},
	}
}

// A reload keeps the selected relation by identity, though it re-sorts the
// groups, and falls back to the nearest row once the relation is gone.
func TestAReloadKeepsTheSelectedRelation(t *testing.T) {
	app := expandOn(t, blockedPairNibs(), "hub", 120, 30)
	pressKeys(app, "j")
	if got := app.expanded.selectedRelation().ID; got != "lkb" {
		t.Fatalf("premise failed: j selected %s, want lkb", got)
	}
	started := "in-progress"
	if _, err := app.backend.UpdateNib(t.Context(), "lkb", model.UpdateNibInput{Status: &started}); err != nil {
		t.Fatal(err)
	}
	app.Update(nibsChangedMsg{})
	if ns := app.expanded.relationNibs(); ns[0].ID != "lkb" {
		t.Fatalf("premise failed: the reload did not move lkb first: %s, %s", ns[0].ID, ns[1].ID)
	}
	if got := app.expanded.selectedRelation().ID; got != "lkb" {
		t.Errorf("after the reload the cursor is on %s, want lkb", got)
	}

	pressKeys(app, "j")
	if err := app.backend.DeleteNib(t.Context(), "lka"); err != nil {
		t.Fatal(err)
	}
	app.Update(nibsChangedMsg{})
	if got := app.expanded.selectedRelation(); got == nil || got.ID != "lkb" {
		t.Errorf("after lka was deleted the cursor is on %v, want the nearest row, lkb", got)
	}
}

// On a document row enter has nothing to follow, so it is not ready: the hint
// row drops it and the palette dims it.
func TestEnterIsNotReadyOnADocumentRow(t *testing.T) {
	app := expandOn(t, overflowNibs(), "hub", 140, 40)
	if row := detailHintRow(t, app); !strings.Contains(row, "enter follow") {
		t.Fatalf("premise failed: the hint row lacks enter on a child row: %q", row)
	}
	pressKeys(app, "G")
	if app.expanded.selectedRelation() != nil {
		t.Fatal("premise failed: G did not select the document")
	}
	if row := detailHintRow(t, app); strings.Contains(row, "enter") {
		t.Errorf("the hint row offers enter on a document row: %q", row)
	}
	ok, why := findAction(defaultActions, actOpen).Ready(app.actionState())
	if ok || why != "a document is not a nib" {
		t.Errorf("open on a document row is ready = %v, why %q; want not ready, a document is not a nib", ok, why)
	}
}
