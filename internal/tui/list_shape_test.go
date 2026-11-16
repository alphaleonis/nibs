package tui

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/viewshape"
)

// shapeNibs is a tree whose only nested match for "zeta" is t1:
//
//	e1 Epic
//	  f1 Feature
//	    t1 Write the zeta docs
//	    t2 Other
//	r1 zeta
func shapeNibs() []*nib.Nib {
	return []*nib.Nib{
		{ID: "e1", Title: "Epic", Type: "epic", Status: "todo", Order: "a0"},
		{ID: "f1", Title: "Feature", Type: "feature", Status: "todo", Parent: "e1", Order: "a1"},
		{ID: "t1", Title: "Write the zeta docs", Type: "task", Status: "todo", Parent: "f1", Order: "a2"},
		{ID: "t2", Title: "Other", Type: "task", Status: "todo", Parent: "f1", Order: "a3"},
		{ID: "r1", Title: "zeta", Type: "task", Status: "todo", Order: "a4"},
	}
}

// shownRow is a row of the list as the tests compare it.
type shownRow struct {
	id     string
	dimmed bool
}

func shownRows(app *App) []shownRow {
	var rows []shownRow
	for _, item := range app.list.list.VisibleItems() {
		bi, ok := item.(nibItem)
		rows = append(rows, shownRow{item.(listRow).base().id, ok && !bi.matched})
	}
	return rows
}

func TestATreeFilterKeepsAncestorsDimmedInTreeOrder(t *testing.T) {
	app, _ := setupTestApp(t, shapeNibs())
	pressKeys(app, "/")
	typeText(app, "ZETA")

	want := []shownRow{{"e1", true}, {"f1", true}, {"t1", false}, {"r1", false}}
	if got := shownRows(app); !slices.Equal(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
}

func TestFlatShowsOnlyMatches(t *testing.T) {
	app, _ := setupTestApp(t, shapeNibs())
	pressKeys(app, "v f / z e t a")

	if app.list.level != viewshape.LevelFlat {
		t.Fatalf("premise failed: v left the list at level %q", app.list.level)
	}
	want := []shownRow{{"t1", false}, {"r1", false}}
	if got := shownRows(app); !slices.Equal(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
	if got := headerOf(app); got[:4] != "Flat" {
		t.Errorf("the header = %q, want it to name Flat", got)
	}
}

// A fuzzy ranking puts the short exact title first, so the rows keep their tree
// order only when nothing ranks them.
func TestTheTextFilterKeepsTreeOrder(t *testing.T) {
	nibs := []*nib.Nib{
		{ID: "n1", Title: "A long title that mentions zeta near its end", Type: "task", Status: "todo", Order: "a0"},
		{ID: "n2", Title: "Unrelated", Type: "task", Status: "todo", Order: "a1"},
		{ID: "n3", Title: "zeta", Type: "task", Status: "todo", Order: "a2"},
	}
	for _, accept := range []bool{false, true} {
		app, _ := setupTestApp(t, nibs)
		pressKeys(app, "/")
		typeText(app, "zeta")
		if accept {
			pressKeys(app, "enter")
			if app.list.list.FilterState() != list.FilterApplied {
				t.Fatalf("premise failed: enter left the filter %v", app.list.list.FilterState())
			}
		}
		want := []shownRow{{"n1", false}, {"n3", false}}
		if got := shownRows(app); !slices.Equal(got, want) {
			t.Errorf("accepted %v: rows = %v, want %v", accept, got, want)
		}
	}
}

func TestDimmedRowsAreNotCounted(t *testing.T) {
	app, _ := setupTestApp(t, shapeNibs())
	pressKeys(app, "/")
	typeText(app, "zeta")
	pressKeys(app, "enter")

	if got := headerCount(t, app); got != "2" {
		t.Errorf("the header counts %s nibs, want the 2 matches", got)
	}
}

func TestFoldingADimmedAncestorHidesTheMatchBeneath(t *testing.T) {
	app, _ := setupTestApp(t, shapeNibs())
	pressKeys(app, "/")
	typeText(app, "zeta")
	pressKeys(app, "enter home")
	if !focusOn(app, "e1") {
		t.Fatal("premise failed: could not focus e1")
	}

	pressKeys(app, "left")

	want := []shownRow{{"e1", true}, {"r1", false}}
	if got := shownRows(app); !slices.Equal(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
	if got := headerCount(t, app); got != "2" {
		t.Errorf("the header counts %s nibs after the fold, want 2", got)
	}
}

// Typing moves the cursor to the first match, past the dimmed rows above it.
func TestTypingAQueryFocusesTheFirstMatch(t *testing.T) {
	app, _ := setupTestApp(t, shapeNibs())
	pressKeys(app, "/")
	typeText(app, "zeta")
	if got := selectedID(app); got != "t1" {
		t.Errorf("the cursor is on %q, want the first match t1", got)
	}
}

func TestAReloadKeepsTheCursorFoldsAndFilter(t *testing.T) {
	for _, state := range []list.FilterState{list.Filtering, list.FilterApplied} {
		t.Run(state.String(), func(t *testing.T) {
			app, stub := setupTestApp(t, shapeNibs())
			pressKeys(app, "/")
			typeText(app, "e")
			pressKeys(app, "enter")
			if !focusOn(app, "f1") {
				t.Fatal("premise failed: could not focus f1")
			}
			pressKeys(app, "left")
			if !focusOn(app, "r1") {
				t.Fatal("premise failed: could not focus r1")
			}
			pressKeys(app, "/")
			typeText(app, "t")
			if state == list.FilterApplied {
				pressKeys(app, "enter")
			}
			if !focusOn(app, "r1") {
				t.Fatal("premise failed: could not focus r1 under the query")
			}
			rows := shownRows(app)

			// A match ahead of every row moves each row down one.
			stub.AllNibs = append(stub.AllNibs, &nib.Nib{ID: "z0", Title: "Beta", Type: "task", Status: "todo", Order: "0"})
			rows = append([]shownRow{{"z0", false}}, rows...)
			_, cmd := app.Update(nibsChangedMsg{})
			processCmd(app, cmd)

			if got := app.list.list.FilterState(); got != state || app.list.list.FilterValue() != "et" {
				t.Errorf("after the reload the filter is %v %q, want %v %q", got, app.list.list.FilterValue(), state, "et")
			}
			if !app.list.collapsed()["f1"] {
				t.Error("the reload unfolded f1")
			}
			if got := selectedID(app); got != "r1" {
				t.Errorf("the cursor is on %q after the reload, want r1", got)
			}
			if got := shownRows(app); !slices.Equal(got, rows) {
				t.Errorf("rows = %v after the reload, want %v", got, rows)
			}
		})
	}
}

// As on the web, a filter change drops the marks of nibs that stop matching,
// dimmed context included.
func TestAFilterKeepsOnlyMarksThatMatch(t *testing.T) {
	app, _ := setupTestApp(t, shapeNibs())
	for _, id := range []string{"e1", "t1", "t2"} {
		if !focusOn(app, id) {
			t.Fatalf("premise failed: could not focus %s", id)
		}
		pressKeys(app, "space")
	}
	pressKeys(app, "/")
	typeText(app, "zeta")

	if got := slices.Sorted(maps.Keys(app.list.selectedNibs)); !slices.Equal(got, []string{"t1"}) {
		t.Errorf("marked = %v, want [t1]", got)
	}
}

// Each view keeps its own folds and cursor when the list switches views.
func TestSwitchingViewsKeepsEachViewsFoldsAndCursor(t *testing.T) {
	app, _ := setupTestApp(t, shapeNibs())
	if !focusOn(app, "f1") {
		t.Fatal("premise failed: could not focus f1")
	}
	pressKeys(app, "left")
	pressKeys(app, "v f")
	if !focusOn(app, "t2") {
		t.Fatal("premise failed: could not focus t2 in Flat")
	}

	pressKeys(app, "v t")

	if app.list.level != viewshape.LevelTree || !app.list.collapsed()["f1"] {
		t.Errorf("back in %q, folds %v; want Tree with f1 folded", app.list.level, app.list.collapsed())
	}
	if got := selectedID(app); got != "f1" {
		t.Errorf("the cursor is on %q back in Tree, want f1", got)
	}
}

func headerOf(app *App) string {
	return strings.Split(plainView(app), "\n")[1]
}

// milestoneRootNibs puts a milestone among the roots, where Tree draws no row
// for it.
func milestoneRootNibs() []*nib.Nib {
	return []*nib.Nib{
		{ID: "t1", Title: "One", Type: "task", Status: "todo", Order: "a0"},
		{ID: "ms1", Title: "Wave", Type: "milestone", Status: "todo", Order: "a1"},
		{ID: "t2", Title: "Two", Type: "task", Status: "todo", Order: "a2"},
		{ID: "t3", Title: "Three", Type: "task", Status: "todo", Order: "a3"},
	}
}

func TestTreeReordersAmongTheRowsItShows(t *testing.T) {
	t.Run("the first row is at the top", func(t *testing.T) {
		app, stub := setupTestApp(t, milestoneRootNibs()[1:])
		if got := selectedID(app); got != "t2" {
			t.Fatalf("premise failed: the cursor is on %q, want the first row t2", got)
		}
		pressKeys(app, "ctrl+up")
		if len(stub.ReorderCalls) != 0 || app.list.statusMessage != reorderReasonAtTop {
			t.Errorf("ctrl+up on the first row wrote %+v and said %q, want nothing written and %q",
				stub.ReorderCalls, app.list.statusMessage, reorderReasonAtTop)
		}
	})
	t.Run("rows next to each other move as a block", func(t *testing.T) {
		app, stub := setupTestApp(t, milestoneRootNibs())
		for _, id := range []string{"t1", "t2"} {
			if !focusOn(app, id) {
				t.Fatalf("premise failed: could not focus %s", id)
			}
			pressKeys(app, "space")
		}
		pressKeys(app, "ctrl+down")
		if len(stub.ReorderCalls) != 1 || stub.ReorderCalls[0].ID != "t3" {
			t.Errorf("ctrl+down on t1 and t2 wrote %+v and said %q, want t3 moved above them",
				stub.ReorderCalls, app.list.statusMessage)
		}
	})
}

func TestFlatRefusesToReorder(t *testing.T) {
	app, stub := setupTestApp(t, milestoneRootNibs())
	pressKeys(app, "v f")
	if !focusOn(app, "t2") {
		t.Fatal("premise failed: could not focus t2")
	}
	pressKeys(app, "ctrl+up")
	if len(stub.ReorderCalls) != 0 || app.list.statusMessage != reorderReasonFlat {
		t.Errorf("ctrl+up in Flat wrote %+v and said %q, want nothing written and %q",
			stub.ReorderCalls, app.list.statusMessage, reorderReasonFlat)
	}
}

// An applied query whose matches are all renamed away leaves no rows, and the
// list widget then turns its own filter key off.
func TestSlashReopensAQueryThatMatchesNothing(t *testing.T) {
	app, stub := setupTestApp(t, shapeNibs())
	pressKeys(app, "/")
	typeText(app, "zeta")
	pressKeys(app, "enter")
	renamed := make([]*nib.Nib, len(stub.AllNibs))
	for i, n := range stub.AllNibs {
		c := *n
		c.Title = strings.ReplaceAll(c.Title, "zeta", "eta")
		renamed[i] = &c
	}
	stub.AllNibs = renamed
	_, cmd := app.Update(nibsChangedMsg{})
	processCmd(app, cmd)
	if len(shownRows(app)) != 0 || app.list.list.FilterState() != list.FilterApplied {
		t.Fatalf("premise failed: rows %v under filter state %v, want none under an applied filter",
			shownRows(app), app.list.list.FilterState())
	}

	pressKeys(app, "/ x")

	if got := app.list.list.FilterValue(); app.list.list.FilterState() != list.Filtering || got != "zetax" {
		t.Errorf("after / x the filter is %v %q, want filtering %q", app.list.list.FilterState(), got, "zetax")
	}
}

// Flat sorts every nib by its order key, so t1 comes before its parent there,
// and r0 ahead of it is the first row.
func TestAFirstVisitToAViewKeepsTheNib(t *testing.T) {
	app, _ := setupTestApp(t, []*nib.Nib{
		{ID: "r0", Title: "First", Type: "task", Status: "todo", Order: "0"},
		{ID: "r1", Title: "Root", Type: "task", Status: "todo", Order: "a1"},
		{ID: "e1", Title: "Epic", Type: "epic", Status: "todo", Order: "a2"},
		{ID: "t1", Title: "Child", Type: "task", Status: "todo", Parent: "e1", Order: "a0"},
	})
	if !focusOn(app, "t1") {
		t.Fatal("premise failed: could not focus t1")
	}
	pressKeys(app, "v f enter")
	if app.state != viewExpanded || app.expanded.nib.ID != "t1" {
		t.Errorf("enter after v opened %v in state %d, want t1", app.expanded.nib, app.state)
	}
}

func TestSwitchingViewsUnmarksNibsTheViewDoesNotShow(t *testing.T) {
	app, _ := setupTestApp(t, milestoneRootNibs())
	if !focusMilestone(app, "ms1") {
		t.Fatal("premise failed: could not focus ms1 in Flat")
	}
	pressKeys(app, "space")
	if !focusOn(app, "t2") {
		t.Fatal("premise failed: could not focus t2 in Flat")
	}
	pressKeys(app, "space v t")
	if got := slices.Sorted(maps.Keys(app.list.selectedNibs)); !slices.Equal(got, []string{"t2"}) {
		t.Errorf("marked in Tree = %v, want [t2]", got)
	}
}

// f1 is dimmed context in Tree, and Flat draws no row for context.
func TestAFirstVisitPutsTheCursorOnTheFirstRowWhenTheNibHasNoRow(t *testing.T) {
	app, _ := setupTestApp(t, shapeNibs())
	pressKeys(app, "/")
	typeText(app, "zeta")
	pressKeys(app, "enter home down")
	if got := selectedID(app); got != "f1" {
		t.Fatalf("premise failed: the cursor is on %q, want f1", got)
	}
	pressKeys(app, "v f")
	if got := selectedID(app); got != "t1" {
		t.Errorf("the cursor is on %q in Flat, want the first row t1", got)
	}
}

// As on the web, a move while a search narrows the rows is refused, because
// the rows it would pass may be hidden.
func TestATextFilterRefusesToReorder(t *testing.T) {
	app, stub := setupTestApp(t, shapeNibs())
	pressKeys(app, "/")
	typeText(app, "zeta")
	pressKeys(app, "enter")
	if !focusOn(app, "r1") {
		t.Fatal("premise failed: could not focus r1")
	}
	pressKeys(app, "ctrl+up")
	if len(stub.ReorderCalls) != 0 || app.list.statusMessage != reorderReasonTextFilter {
		t.Errorf("ctrl+up under a text filter wrote %+v and said %q, want nothing written and %q",
			stub.ReorderCalls, app.list.statusMessage, reorderReasonTextFilter)
	}
}

// t1 and t2 are hidden under the folded f1, yet still siblings of t3.
func TestFoldedRowsStillCountAsSiblings(t *testing.T) {
	app, stub := setupTestApp(t, []*nib.Nib{
		{ID: "f1", Title: "Feature", Type: "feature", Status: "todo", Order: "a0"},
		{ID: "t1", Title: "One", Type: "task", Status: "todo", Parent: "f1", Order: "a1"},
		{ID: "t2", Title: "Two", Type: "task", Status: "todo", Parent: "f1", Order: "a2"},
		{ID: "t3", Title: "Three", Type: "task", Status: "todo", Parent: "f1", Order: "a3"},
	})
	for _, id := range []string{"t1", "t2"} {
		if !focusOn(app, id) {
			t.Fatalf("premise failed: could not focus %s", id)
		}
		pressKeys(app, "space")
	}
	pressKeys(app, "home left")
	if !app.list.collapsed()["f1"] {
		t.Fatal("premise failed: left did not fold f1")
	}
	pressKeys(app, "ctrl+down")
	if len(stub.ReorderCalls) != 1 || stub.ReorderCalls[0].ID != "t3" {
		t.Errorf("ctrl+down on the folded t1 and t2 wrote %+v and said %q, want t3 moved above them",
			stub.ReorderCalls, app.list.statusMessage)
	}
}

// One keystroke is the whole query change, so the marks must be pruned against
// the rows that query shapes.
func TestOneKeystrokeOfQueryUnmarksWhatItHides(t *testing.T) {
	app, _ := setupTestApp(t, shapeNibs())
	if !focusOn(app, "t2") {
		t.Fatal("premise failed: could not focus t2")
	}
	pressKeys(app, "space / z")
	if app.list.selectedNibs["t2"] {
		t.Error("t2 is still marked under a query that hides it")
	}
}

func TestAReloadUnmarksANibThatClosed(t *testing.T) {
	app, stub := setupTestApp(t, shapeNibs())
	if !focusOn(app, "t2") {
		t.Fatal("premise failed: could not focus t2")
	}
	pressKeys(app, "space")
	closed := *stub.AllNibs[3]
	closed.Status = "completed"
	stub.AllNibs[3] = &closed
	_, cmd := app.Update(nibsChangedMsg{})
	processCmd(app, cmd)
	if app.list.selectedNibs["t2"] {
		t.Error("t2 is still marked after the reload hid it")
	}
}
