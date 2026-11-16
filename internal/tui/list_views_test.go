package tui

import (
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/alphaleonis/nibs/internal/area"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/viewshape"
	"github.com/charmbracelet/x/exp/golden"
)

// groupedNibs has two milestones whose order keys put ms2 first, a nib in
// each, nibs in no milestone, and nibs in a declared area, a nested one, an
// undeclared one and none.
func groupedNibs() []*nib.Nib {
	return []*nib.Nib{
		{ID: "ms1", Title: "Launch", Type: "milestone", Status: "todo", Order: "a1"},
		{ID: "ms2", Title: "Beta", Type: "milestone", Status: "todo", Order: "a0"},
		{ID: "t1", Title: "One", Type: "task", Status: "todo", Order: "b1", Milestone: "ms1", Area: "api"},
		{ID: "t2", Title: "Two", Type: "task", Status: "todo", Order: "b2", Milestone: "ms2", Area: "api/webhooks"},
		{ID: "t3", Title: "Three", Type: "task", Status: "todo", Order: "b3"},
		{ID: "t4", Title: "Four", Type: "task", Status: "todo", Order: "b4", Area: "nowhere"},
	}
}

const groupedAreas = `
areas:
  - name: docs
  - name: api
    color: teal
    children:
      - name: webhooks
`

// setupGroupedApp is setupTestApp on groupedNibs, with groupedAreas declared.
func setupGroupedApp(t *testing.T) (*App, *StubBackend) {
	t.Helper()
	app, stub := setupTestApp(t, groupedNibs())
	vocab, err := area.Parse([]byte(groupedAreas))
	if err != nil {
		t.Fatal(err)
	}
	stub.AreaVocab = vocab
	reload(app)
	return app, stub
}

// reload sends the message a change on disk sends, and runs the reload.
func reload(app *App) {
	_, cmd := app.Update(nibsChangedMsg{})
	processCmd(app, cmd)
}

// rowIDs returns the id of each row the list shows, top to bottom.
func rowIDs(app *App) []string {
	var ids []string
	for _, item := range app.list.list.VisibleItems() {
		ids = append(ids, item.(listRow).base().id)
	}
	return ids
}

// focusRow moves the cursor to the row whose id is id, a section row's
// included. It reports false when no row has it.
func focusRow(app *App, id string) bool {
	i := slices.Index(rowIDs(app), id)
	if i < 0 {
		return false
	}
	pressKeys(app, "home"+strings.Repeat(" down", i))
	return app.list.focusedRowID() == id
}

var (
	backlogRow = sectionRowPrefix + viewshape.BacklogSection
	noAreaRow  = sectionRowPrefix + viewshape.NoAreaSection
)

func TestVOpensTheViewPickerOnTheCurrentView(t *testing.T) {
	app, _ := setupTestApp(t, groupedNibs())
	pressKeys(app, "v")

	if got := app.keyContext(); got != ctxViewPicker {
		t.Fatalf("v opened the %s, want the view picker", got)
	}
	view := stripAnsi(app.viewPicker.View())
	for _, name := range []string{"Tree", "Flat", "Milestones", "Areas"} {
		if acceleratorOf(view, name) == "" {
			t.Errorf("the picker shows no row for %s with an accelerator:\n%s", name, view)
		}
	}
	if !regexpLine(view, `Tree\s+current`) {
		t.Errorf("the picker does not mark Tree as the current view:\n%s", view)
	}
	pressKeys(app, "esc")
	if app.state != viewList || app.list.level != viewshape.LevelTree {
		t.Errorf("esc left state %d on %q, want the list on Tree", app.state, app.list.level)
	}
}

// regexpLine reports whether a line of text matches pattern.
func regexpLine(text, pattern string) bool {
	for _, line := range strings.Split(text, "\n") {
		if matched, _ := regexp.MatchString(pattern, line); matched {
			return true
		}
	}
	return false
}

// Milestones come in the order of their order keys, and the Backlog takes the
// rest last, as on the web.
func TestMilestonesShowsASectionPerMilestoneAndTheBacklogLast(t *testing.T) {
	app, _ := setupGroupedApp(t)
	pressKeys(app, "v m")

	if app.list.level != viewshape.LevelMilestones {
		t.Fatalf("v m showed %q, want Milestones", app.list.level)
	}
	want := []string{"ms2", "t2", "ms1", "t1", backlogRow, "t3", "t4"}
	if got := rowIDs(app); !slices.Equal(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
	if got := headerOf(app); !strings.HasPrefix(got, "Milestones") {
		t.Errorf("the header = %q, want it to name Milestones", got)
	}
	if !strings.Contains(plainView(app), "Backlog") {
		t.Errorf("no row reads Backlog:\n%s", plainView(app))
	}
}

// An area's section is labeled with its own name, and an undeclared area falls
// into No area with the nibs that have none.
func TestAreasShowsTheDeclaredAreasAndNoArea(t *testing.T) {
	app, _ := setupGroupedApp(t)
	pressKeys(app, "v a")

	want := []string{
		sectionRowPrefix + "api", sectionRowPrefix + "api/webhooks", "t2", "t1",
		sectionRowPrefix + "docs",
		noAreaRow, "ms2", "ms1", "t3", "t4",
	}
	if got := rowIDs(app); !slices.Equal(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
	screen := plainView(app)
	for _, label := range []string{"api", "webhooks", "docs", "No area"} {
		if !regexpLine(screen, `[▾▸ ] `+label+`\s*$`) {
			t.Errorf("no section row reads %q:\n%s", label, screen)
		}
	}
	if strings.Contains(screen, "api/webhooks") {
		t.Errorf("a section row shows the area's whole path:\n%s", screen)
	}
}

// v v returns to the view before, with the folds and cursor it had there.
func TestVVReturnsToThePreviousViewWithItsFoldsAndCursor(t *testing.T) {
	app, _ := setupTestApp(t, shapeNibs())
	if !focusOn(app, "f1") {
		t.Fatal("premise failed: could not focus f1")
	}
	pressKeys(app, "left v m")
	if app.list.level != viewshape.LevelMilestones {
		t.Fatalf("premise failed: v m showed %q", app.list.level)
	}
	if !focusOn(app, "r1") {
		t.Fatal("premise failed: could not focus r1 in Milestones")
	}

	pressKeys(app, "v v")

	if app.list.level != viewshape.LevelTree || !app.list.collapsed()["f1"] {
		t.Errorf("v v showed %q with folds %v, want Tree with f1 folded", app.list.level, app.list.collapsed())
	}
	if got := selectedID(app); got != "f1" {
		t.Errorf("the cursor is on %q back in Tree, want f1", got)
	}

	pressKeys(app, "v v")

	if got := selectedID(app); app.list.level != viewshape.LevelMilestones || got != "r1" {
		t.Errorf("v v again showed %q with the cursor on %q, want Milestones on r1", app.list.level, got)
	}
}

func TestThePickerMarksThePreviousViewWithItsKey(t *testing.T) {
	app, _ := setupTestApp(t, groupedNibs())
	pressKeys(app, "v a v")
	if view := stripAnsi(app.viewPicker.View()); !regexpLine(view, `Tree\s+v previous`) {
		t.Errorf("the picker does not offer v for Tree:\n%s", view)
	}
}

func TestFoldingASectionHidesItsRows(t *testing.T) {
	t.Run("an area", func(t *testing.T) {
		app, _ := setupGroupedApp(t)
		pressKeys(app, "v a")
		if !focusRow(app, sectionRowPrefix+"api") {
			t.Fatal("premise failed: could not focus the api section")
		}
		pressKeys(app, "left")
		want := []string{sectionRowPrefix + "api", sectionRowPrefix + "docs", noAreaRow, "ms2", "ms1", "t3", "t4"}
		if got := rowIDs(app); !slices.Equal(got, want) {
			t.Errorf("rows after folding api = %v, want %v", got, want)
		}
		if line := strings.Split(plainView(app), "\n")[2]; !strings.Contains(line, "▸") {
			t.Errorf("the folded api row draws no folded mark: %q", line)
		}
		pressKeys(app, "right")
		if got := len(rowIDs(app)); got != 10 {
			t.Errorf("right left %d rows, want all 10", got)
		}
	})
	t.Run("a milestone", func(t *testing.T) {
		app, _ := setupGroupedApp(t)
		pressKeys(app, "v m")
		if !focusRow(app, "ms2") {
			t.Fatal("premise failed: could not focus ms2")
		}
		pressKeys(app, "left")
		want := []string{"ms2", "ms1", "t1", backlogRow, "t3", "t4"}
		if got := rowIDs(app); !slices.Equal(got, want) {
			t.Errorf("rows after folding ms2 = %v, want %v", got, want)
		}
	})
	t.Run("every section", func(t *testing.T) {
		app, _ := setupGroupedApp(t)
		pressKeys(app, "v m <")
		want := []string{"ms2", "ms1", backlogRow}
		if got := rowIDs(app); !slices.Equal(got, want) {
			t.Errorf("rows after < = %v, want %v", got, want)
		}
	})
}

// Folds belong to the view they were made in.
func TestASectionFoldStaysInItsView(t *testing.T) {
	app, _ := setupGroupedApp(t)
	pressKeys(app, "v a")
	if !focusRow(app, noAreaRow) {
		t.Fatal("premise failed: could not focus No area")
	}
	pressKeys(app, "left v m")
	if got := rowIDs(app); !slices.Contains(got, "t3") {
		t.Errorf("Milestones hides t3 after No area was folded in Areas: %v", got)
	}
	pressKeys(app, "v a")
	if got := rowIDs(app); slices.Contains(got, "t3") || app.list.focusedRowID() != noAreaRow {
		t.Errorf("back in Areas the rows are %v with the cursor on %q, want No area folded under the cursor",
			got, app.list.focusedRowID())
	}
}

// A change on disk keeps the cursor and folds of every view, shown or not.
func TestAReloadKeepsEveryViewsCursorAndFolds(t *testing.T) {
	app, stub := setupGroupedApp(t)
	pressKeys(app, "v m")
	if !focusRow(app, "ms2") {
		t.Fatal("premise failed: could not focus ms2")
	}
	pressKeys(app, "left")
	if !focusRow(app, "t3") {
		t.Fatal("premise failed: could not focus t3")
	}
	pressKeys(app, "v a")
	if !focusRow(app, sectionRowPrefix+"api") {
		t.Fatal("premise failed: could not focus the api section")
	}
	pressKeys(app, "left")
	if !focusRow(app, "t4") {
		t.Fatal("premise failed: could not focus t4")
	}

	stub.AllNibs = append(stub.AllNibs, &nib.Nib{ID: "t5", Title: "Five", Type: "task", Status: "todo", Order: "0", Milestone: "ms2", Area: "api"})
	reload(app)

	if got := app.list.focusedRowID(); got != "t4" || !app.list.state().collapsedSections["api"] {
		t.Errorf("after the reload Areas has the cursor on %q and folds %v, want t4 and api folded",
			got, app.list.state().collapsedSections)
	}
	pressKeys(app, "v v")
	if got := app.list.focusedRowID(); app.list.level != viewshape.LevelMilestones || got != "t3" || !app.list.collapsed()["ms2"] {
		t.Errorf("Milestones after the reload: level %q, cursor %q, folds %v; want the cursor on t3 and ms2 folded",
			app.list.level, got, app.list.collapsed())
	}
}

// A section row names no nib, so it cannot be marked, and an action that
// needs a nib is not ready on it.
func TestASectionRowIsNotANib(t *testing.T) {
	app, _ := setupGroupedApp(t)
	pressKeys(app, "v m")
	if !focusRow(app, backlogRow) {
		t.Fatal("premise failed: could not focus the Backlog")
	}

	pressKeys(app, "space")
	if len(app.list.selectedNibs) != 0 {
		t.Errorf("space marked %v on the Backlog row", app.list.selectedNibs)
	}
	st := app.actionState()
	for _, name := range []actionName{actOpen, actEdit, actStatus, actArchive, actCopyID} {
		if ok, _ := findAction(app.keys.actions, name).Ready(st); ok {
			t.Errorf("%s is ready on the Backlog row", name)
		}
	}
	pressKeys(app, "enter")
	if app.state != viewList {
		t.Errorf("enter on the Backlog row opened state %d", app.state)
	}
	lines := strings.Split(plainView(app), "\n")
	if hints := lines[len(lines)-1]; strings.Contains(hints, "open") {
		t.Errorf("the footer hints open on the Backlog row: %q", hints)
	}
}

func TestViewNextAndPreviousHaveNoKeyAndStepThroughTheViews(t *testing.T) {
	for _, name := range []actionName{actViewNext, actViewPrevious} {
		if keys := findAction(defaultActions, name).Keys; len(keys) != 0 {
			t.Errorf("%s is bound to %v by default", name, keys)
		}
	}
	app, _ := setupTestApp(t, groupedNibs())
	_, cmd := app.Update(actionMsg{name: actViewPrevious})
	processCmd(app, cmd)
	if app.list.level != viewshape.LevelAreas {
		t.Errorf("view.previous from Tree showed %q, want Areas", app.list.level)
	}
	_, cmd = app.Update(actionMsg{name: actViewNext})
	processCmd(app, cmd)
	if app.list.level != viewshape.LevelTree {
		t.Errorf("view.next from Areas showed %q, want Tree", app.list.level)
	}
}

func TestEachViewHasItsOwnColumns(t *testing.T) {
	path := userConfigFile(t, "tui:\n  icons: unicode\n")
	app := setupTestAppWithUserLayer(t, groupedNibs(), config.Default(), config.LoadUserLayerFrom(path))
	for _, view := range []struct{ key, name string }{{"t", treeView}, {"f", flatView}, {"m", milestonesView}, {"a", areasView}} {
		pressKeys(app, "v "+view.key)
		if got := app.list.columns; !slices.Equal(got, defaultColumns[view.name]) {
			t.Errorf("%s shows %v, want its defaults %v", view.name, got, defaultColumns[view.name])
		}
	}
	for name, columns := range map[string][]columnName{milestonesView: defaultColumns[milestonesView], areasView: defaultColumns[areasView]} {
		grouping := map[string]columnName{milestonesView: colMilestone, areasView: colArea}[name]
		if slices.Contains(columns, grouping) {
			t.Errorf("%s shows the %s column by default, which its sections already show", name, grouping)
		}
	}

	pressKeys(app, "v m")
	toggleColumns(t, app, colArea)
	if got := savedColumns(t, path, milestonesView); !slices.Equal(got, []string{"title", "tags", "id"}) {
		t.Errorf("tui.columns.milestones = %v, want [title tags id]", got)
	}
	if got := savedColumns(t, path, treeView); got != nil {
		t.Errorf("a choice in Milestones saved tui.columns.tree = %v", got)
	}
	pressKeys(app, "v v")
	if got := app.list.columns; !slices.Equal(got, defaultColumns[areasView]) {
		t.Errorf("back in Areas the columns are %v, want %v", got, defaultColumns[areasView])
	}
	pressKeys(app, "v m")
	if got := app.list.columns; !slices.Equal(got, []columnName{colTags, colID}) {
		t.Errorf("back in Milestones the columns are %v, want the chosen [tags id]", got)
	}
}

// groupedViewKeys are the picker keys of the grouped views, by their names in
// tui.columns.
var groupedViewKeys = map[string]string{milestonesView: "m", areasView: "a"}

// The grouped views' whole screen at 100 columns, on the sample fixture,
// without color. Regenerate with `-update`.
func TestGroupedViewGolden(t *testing.T) {
	for name, key := range groupedViewKeys {
		t.Run(name, func(t *testing.T) {
			app := sampleApp(t, 100)
			pressKeys(app, "v "+key+" home")
			golden.RequireEqual(t, plainView(app))
		})
	}
}

// The rows of each grouped view on the sample fixture are the rows viewshape
// shapes for it from the store's nibs and declared areas, so their sections
// come in the order the web's shared cases pin.
func TestGroupedViewsShowTheSectionsViewshapeShapes(t *testing.T) {
	for name, key := range groupedViewKeys {
		t.Run(name, func(t *testing.T) {
			app := sampleApp(t, 100)
			pressKeys(app, "v "+key)
			if app.list.backend.Areas().IsEmpty() {
				t.Fatal("premise failed: the fixture declares no areas")
			}
			nibs, err := app.list.backend.ListNibs(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			rows, err := viewshape.Shape(nibs, viewshape.View{
				Level: app.list.level, Filter: app.list.filter.meta, Sort: viewshape.SortOrder, Areas: app.list.backend.Areas(),
			})
			if err != nil {
				t.Fatal(err)
			}
			want := make([]string, len(rows))
			for i, r := range rows {
				want[i] = rowIDOf(r)
			}
			if got := rowIDs(app); !slices.Equal(got, want) {
				t.Errorf("rows differ from viewshape's:\n got %v\nwant %v", got, want)
			}
			var sections []string
			for _, r := range rows {
				if r.Header && r.Depth == 0 {
					sections = append(sections, r.Section)
				}
			}
			if last := sections[len(sections)-1]; !viewshape.IsLeftoverSection(last) {
				t.Errorf("the last section is %q, want the leftover", last)
			}
		})
	}
}

// A folded section still holds its nibs, so they stay marked and counted.
func TestAFoldedSectionKeepsItsMarksAndCount(t *testing.T) {
	app, _ := setupGroupedApp(t)
	pressKeys(app, "v a")
	if !focusOn(app, "t1") {
		t.Fatal("premise failed: could not focus t1")
	}
	pressKeys(app, "space")
	count := app.list.matchCount()
	if !focusRow(app, sectionRowPrefix+"api") {
		t.Fatal("premise failed: could not focus the api section")
	}
	pressKeys(app, "left")
	reload(app)

	if !app.list.selectedNibs["t1"] {
		t.Errorf("t1 lost its mark when its area was folded: %v", app.list.selectedNibs)
	}
	if got := app.list.matchCount(); got != count {
		t.Errorf("folding api changed the count from %d to %d", count, got)
	}
}

func TestFoldingEveryRowPutsTheCursorOnTheEnclosingSection(t *testing.T) {
	app, _ := setupGroupedApp(t)
	pressKeys(app, "v a")
	if !focusOn(app, "t1") {
		t.Fatal("premise failed: could not focus t1")
	}
	pressKeys(app, "<")
	if got := app.list.focusedRowID(); got != sectionRowPrefix+"api" {
		t.Errorf("< left the cursor on %q, want the api section", got)
	}
}

func TestUnfoldingASectionsSubtreeReachesTheAreasInIt(t *testing.T) {
	app, _ := setupGroupedApp(t)
	pressKeys(app, "v a <")
	if !focusRow(app, sectionRowPrefix+"api") {
		t.Fatal("premise failed: could not focus the api section")
	}
	pressKeys(app, "ctrl+right")
	folds := app.list.state().collapsedSections
	if want := map[string]bool{"api": true, viewshape.NoAreaSection: true}; !maps.Equal(folds, want) {
		t.Errorf("ctrl+right on api left the folds %v, want %v", folds, want)
	}
	pressKeys(app, "right")
	want := []string{sectionRowPrefix + "api", sectionRowPrefix + "api/webhooks", "t2", "t1", sectionRowPrefix + "docs", noAreaRow}
	if got := rowIDs(app); !slices.Equal(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
}

// A change to areas.yml alone reaches the TUI as a reload, which redraws the
// Areas view from the new vocabulary.
func TestAnAreasChangeReloadsTheList(t *testing.T) {
	app, stub := setupGroupedApp(t)
	pressKeys(app, "v a")
	msgs := make(chan tea.Msg, 1)
	stop := forwardStoreChanges(stub, func(msg tea.Msg) { msgs <- msg })
	defer stop()

	vocab, err := area.Parse([]byte(groupedAreas + "  - name: zeta\n"))
	if err != nil {
		t.Fatal(err)
	}
	stub.AreaVocab = vocab
	stub.AreaEvents <- struct{}{}
	select {
	case msg := <-msgs:
		_, cmd := app.Update(msg)
		processCmd(app, cmd)
	case <-time.After(5 * time.Second):
		t.Fatal("an areas change sent the TUI nothing")
	}
	if !slices.Contains(rowIDs(app), sectionRowPrefix+"zeta") {
		t.Errorf("the Areas view has no zeta section after the change: %v", rowIDs(app))
	}
}

// A punctuation key is outside the accelerator pool, yet still returns to the
// previous view when typed twice.
func TestAPunctuationViewKeyTypedTwiceReturnsToThePreviousView(t *testing.T) {
	app := setupTestAppWithUserConfig(t, groupedNibs(), config.Default(), "tui:\n  keys:\n    view.pick: ','\n")
	pressKeys(app, ", a")
	if app.list.level != viewshape.LevelAreas {
		t.Fatalf("premise failed: , a showed %q", app.list.level)
	}
	pressKeys(app, ", ,")
	if app.state != viewList || app.list.level != viewshape.LevelTree {
		t.Errorf(", , left state %d on %q, want the list on Tree", app.state, app.list.level)
	}
}

// An area's name comes from areas.yml, so a control character in it is drawn
// as a space rather than sent to the terminal.
func TestAnAreaNameCannotDriveTheTerminal(t *testing.T) {
	app, stub := setupTestApp(t, groupedNibs())
	vocab, err := area.Parse([]byte("areas:\n  - name: \"ev\\e[2Jil\"\n"))
	if err != nil {
		t.Fatalf("premise failed: areas.yml with an ESC in a name does not parse: %v", err)
	}
	stub.AreaVocab = vocab
	reload(app)
	pressKeys(app, "v a")
	screen := app.View().Content
	if strings.Contains(screen, "\x1b[2J") {
		t.Errorf("the area's escape sequence reached the screen: %q", screen)
	}
	if !strings.Contains(plainView(app), "ev [2Jil") {
		t.Errorf("no section row reads the stripped name:\n%s", plainView(app))
	}
}
