package tui

import (
	"fmt"
	"slices"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
)

// writes returns each reorder the stub received, as "id before|after anchor
// SCOPE" or "id first SCOPE".
func writes(stub *StubBackend) []string {
	var out []string
	for _, c := range stub.ReorderCalls {
		switch {
		case c.BeforeID != nil:
			out = append(out, fmt.Sprintf("%s before %s %s", c.ID, *c.BeforeID, c.Scope))
		case c.AfterID != nil:
			out = append(out, fmt.Sprintf("%s after %s %s", c.ID, *c.AfterID, c.Scope))
		default:
			out = append(out, fmt.Sprintf("%s first %s", c.ID, c.Scope))
		}
	}
	return out
}

// mark marks the nibs ids in turn.
func mark(t *testing.T, app *App, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if !focusRow(app, id) {
			t.Fatalf("premise failed: no row shows %s", id)
		}
		pressKeys(app, "space")
	}
}

// expectMove focuses id, presses key, and checks the writes and the footer.
func expectMove(t *testing.T, app *App, stub *StubBackend, id, key string, want []string, status string) {
	t.Helper()
	if id != "" && !focusRow(app, id) {
		t.Fatalf("premise failed: no row shows %s", id)
	}
	stub.ReorderCalls = nil
	pressKeys(app, key)
	if got := writes(stub); !slices.Equal(got, want) || app.list.statusMessage != status {
		t.Errorf("%s on %s wrote %q and said %q, want %q and %q", key, id, got, app.list.statusMessage, want, status)
	}
}

// queueNibs gives ms1 the queue q1, q2, q3, where q1 has the child k1, and
// leaves b1 and b2 in the Backlog.
func queueNibs() []*nib.Nib {
	return []*nib.Nib{
		{ID: "ms1", Title: "Launch", Type: "milestone", Status: "todo", Order: "a0"},
		{ID: "q1", Title: "Q one", Type: "feature", Status: "todo", Order: "b3", Milestone: "ms1", MilestoneOrder: "a0"},
		{ID: "k1", Title: "K one", Type: "task", Status: "todo", Order: "a0", Parent: "q1"},
		{ID: "q2", Title: "Q two", Type: "task", Status: "todo", Order: "b1", Milestone: "ms1", MilestoneOrder: "a1"},
		{ID: "q3", Title: "Q three", Type: "task", Status: "todo", Order: "b2", Milestone: "ms1", MilestoneOrder: "a2"},
		{ID: "b1", Title: "B one", Type: "task", Status: "todo", Order: "c0"},
		{ID: "b2", Title: "B two", Type: "task", Status: "todo", Order: "c1"},
	}
}

func setupQueueApp(t *testing.T) (*App, *StubBackend) {
	t.Helper()
	app, stub := setupTestApp(t, queueNibs())
	pressKeys(app, "v m")
	want := []string{"ms1", "q1", "k1", "q2", "q3", backlogRow, "b1", "b2"}
	if got := rowIDs(app); !slices.Equal(got, want) {
		t.Fatalf("premise failed: Milestones shows %v, want %v", got, want)
	}
	return app, stub
}

func TestMilestonesReordersTheQueueInTheMilestoneScope(t *testing.T) {
	app, stub := setupQueueApp(t)
	expectMove(t, app, stub, "q2", "ctrl+up", []string{"q2 before q1 MILESTONE"}, "")
	expectMove(t, app, stub, "q2", "ctrl+down", []string{"q2 after q3 MILESTONE"}, "")
	// q1's child k1 is drawn between q1 and q2, and q1 passes q2 with it.
	expectMove(t, app, stub, "q1", "ctrl+down", []string{"q1 after q2 MILESTONE"}, "")
	expectMove(t, app, stub, "q3", "ctrl+home", []string{"q3 before q1 MILESTONE"}, "")
	expectMove(t, app, stub, "q1", "ctrl+end", []string{"q1 after q3 MILESTONE"}, "")
}

func TestMilestonesStopsAtTheSectionEdge(t *testing.T) {
	app, stub := setupQueueApp(t)
	expectMove(t, app, stub, "q1", "ctrl+up", nil, reorderReasonAtTop)
	expectMove(t, app, stub, "q1", "ctrl+home", nil, reorderReasonAtTop)
	expectMove(t, app, stub, "q3", "ctrl+down", nil, reorderReasonAtBottom)
	expectMove(t, app, stub, "q3", "ctrl+end", nil, reorderReasonAtBottom)
	// The milestone's row is the last of the milestone rows; the Backlog's row
	// below it heads no nib.
	expectMove(t, app, stub, "ms1", "ctrl+down", nil, reorderReasonAtBottom)
	expectMove(t, app, stub, "b1", "ctrl+up", nil, reorderReasonAtTop)
	expectMove(t, app, stub, "b2", "ctrl+down", nil, reorderReasonAtBottom)
}

// The Backlog orders its rows by the parent order.
func TestMilestonesReordersTheBacklogInTheParentScope(t *testing.T) {
	app, stub := setupQueueApp(t)
	expectMove(t, app, stub, "b2", "ctrl+up", []string{"b2 before b1 PARENT"}, "")
}

func TestMilestonesMovesAMarkedBlockOfTheQueue(t *testing.T) {
	app, stub := setupQueueApp(t)
	mark(t, app, "q2", "q3")
	expectMove(t, app, stub, "", "ctrl+up", []string{"q1 after q3 MILESTONE"}, "")
	expectMove(t, app, stub, "", "ctrl+home", []string{"q2 before q1 MILESTONE", "q3 after q2 MILESTONE"}, "")
}

func TestMilestonesRefusesMarksAcrossGroupsOrSections(t *testing.T) {
	t.Run("a queue and the Backlog", func(t *testing.T) {
		app, stub := setupQueueApp(t)
		mark(t, app, "q3", "b1")
		expectMove(t, app, stub, "", "ctrl+up", nil, reorderReasonDifferentGroups)
	})
	t.Run("a milestone and a Backlog root, both in the root parent group", func(t *testing.T) {
		app, stub := setupQueueApp(t)
		mark(t, app, "ms1", "b1")
		expectMove(t, app, stub, "", "ctrl+down", nil, reorderReasonDifferentSections)
	})
}

func TestAreasRefusesToReorder(t *testing.T) {
	app, stub := setupGroupedApp(t)
	pressKeys(app, "v a")
	expectMove(t, app, stub, "t4", "ctrl+up", nil, reorderReasonAreas)
	expectMove(t, app, stub, "t4", "ctrl+home", nil, reorderReasonAreas)
}

func TestTreeMovesToTheTopAndBottomOfTheSiblings(t *testing.T) {
	app, stub := setupTestApp(t, blockTestNibs())
	expectMove(t, app, stub, "C", "ctrl+home", []string{"C before A PARENT"}, "")
	expectMove(t, app, stub, "B", "ctrl+end", []string{"B after D PARENT"}, "")
	expectMove(t, app, stub, "A", "ctrl+home", nil, reorderReasonAtTop)
	expectMove(t, app, stub, "D", "ctrl+end", nil, reorderReasonAtBottom)
	expectMove(t, app, stub, "C", "ctrl+up", []string{"C before B PARENT"}, "")
	expectMove(t, app, stub, "C", "ctrl+down", []string{"C after D PARENT"}, "")
}

func TestTreeMovesAMarkedBlockToTheTopAndBottom(t *testing.T) {
	app, stub := setupTestApp(t, blockTestNibs())
	mark(t, app, "B", "C")
	expectMove(t, app, stub, "", "ctrl+home", []string{"B before A PARENT", "C after B PARENT"}, "")
	expectMove(t, app, stub, "", "ctrl+end", []string{"C after D PARENT", "B before C PARENT"}, "")
}

// A's children are drawn between A and B, and a move passes A with them.
func TestTreeMovesPastASubtreeAsOneRow(t *testing.T) {
	app, stub := setupTestApp(t, []*nib.Nib{
		{ID: "A", Title: "A", Type: "feature", Status: "todo", Order: "1"},
		{ID: "A1", Title: "A1", Type: "task", Status: "todo", Order: "1", Parent: "A"},
		{ID: "A2", Title: "A2", Type: "task", Status: "todo", Order: "2", Parent: "A"},
		{ID: "B", Title: "B", Type: "task", Status: "todo", Order: "2"},
	})
	expectMove(t, app, stub, "B", "ctrl+up", []string{"B before A PARENT"}, "")
	expectMove(t, app, stub, "A", "ctrl+down", []string{"A after B PARENT"}, "")
	expectMove(t, app, stub, "A2", "ctrl+down", nil, reorderReasonAtBottom)
}

// t3 is hidden under the folded f1, and still the last of t1's siblings.
func TestAFoldedSiblingIsTheBottomOfTheGroup(t *testing.T) {
	app, stub := setupTestApp(t, []*nib.Nib{
		{ID: "f1", Title: "Feature", Type: "feature", Status: "todo", Order: "a0"},
		{ID: "t1", Title: "One", Type: "task", Status: "todo", Parent: "f1", Order: "a1"},
		{ID: "t2", Title: "Two", Type: "task", Status: "todo", Parent: "f1", Order: "a2"},
		{ID: "t3", Title: "Three", Type: "task", Status: "todo", Parent: "f1", Order: "a3"},
	})
	mark(t, app, "t1")
	pressKeys(app, "home left")
	if !app.list.collapsed()["f1"] {
		t.Fatal("premise failed: left did not fold f1")
	}
	expectMove(t, app, stub, "", "ctrl+end", []string{"t1 after t3 PARENT"}, "")
}

// interleavedNibs puts ms1's children c1 and c2 among the roots r1 and r2,
// where Tree draws them, because the milestone has no row there.
func interleavedNibs() []*nib.Nib {
	return []*nib.Nib{
		{ID: "r1", Title: "R one", Type: "task", Status: "todo", Order: "a0"},
		{ID: "ms1", Title: "Launch", Type: "milestone", Status: "todo", Order: "a1"},
		{ID: "c1", Title: "C one", Type: "task", Status: "todo", Order: "a2", Parent: "ms1"},
		{ID: "r2", Title: "R two", Type: "task", Status: "todo", Order: "a3"},
		{ID: "c2", Title: "C two", Type: "task", Status: "todo", Order: "a4", Parent: "ms1"},
	}
}

// A move passes only rows of its own ordering group, so in Tree no move can
// pass a root while a milestone's child is drawn among the roots.
func TestTreeRefusesToPassARowOfAnotherGroup(t *testing.T) {
	app, stub := setupTestApp(t, interleavedNibs())
	if got, want := rowIDs(app), []string{"r1", "c1", "r2", "c2"}; !slices.Equal(got, want) {
		t.Fatalf("premise failed: Tree shows %v, want %v", got, want)
	}
	for _, tc := range []struct{ id, key string }{
		{"c2", "ctrl+up"},
		{"c1", "ctrl+up"},
		{"c1", "ctrl+down"},
		{"r1", "ctrl+down"},
		{"r2", "ctrl+up"},
		{"c2", "ctrl+home"},
		{"r1", "ctrl+end"},
	} {
		expectMove(t, app, stub, tc.id, tc.key, nil, reorderReasonOtherGroup)
	}
	expectMove(t, app, stub, "r1", "ctrl+up", nil, reorderReasonAtTop)
	expectMove(t, app, stub, "c2", "ctrl+down", nil, reorderReasonAtBottom)
	mark(t, app, "r1", "c1")
	expectMove(t, app, stub, "", "ctrl+down", nil, reorderReasonDifferentParents)
}

// descendantQueueNibs gives m1 the queue b1, x, e1, where b1 descends from e1
// through the milestone m2, which e1 holds. b1 has status b1Status, and x and
// e1 carry the tag "t".
func descendantQueueNibs(b1Status string) []*nib.Nib {
	return []*nib.Nib{
		{ID: "m1", Type: "milestone", Title: "Release", Status: "todo", Order: "a0"},
		{ID: "m2", Type: "milestone", Title: "Nested", Status: "todo", Parent: "e1", Order: "a1"},
		{ID: "b1", Type: "bug", Title: "Bug", Status: b1Status, Parent: "m2", Milestone: "m1", MilestoneOrder: "a0", Order: "a3"},
		{ID: "x", Type: "task", Title: "Other", Status: "todo", Milestone: "m1", MilestoneOrder: "a05", Order: "a4", Tags: []string{"t"}},
		{ID: "e1", Type: "epic", Title: "Epic", Status: "todo", Milestone: "m1", MilestoneOrder: "a1", Order: "a2", Tags: []string{"t"}},
	}
}

// e1 cannot move above its own descendant b1, alone or in a block, as the web
// refuses a drop there. A move to the top passes b1 also while a filter hides
// it.
func TestAQueueMoveRefusesToPlaceANibAboveItsOwnDescendant(t *testing.T) {
	t.Run("shown", func(t *testing.T) {
		app, stub := setupTestApp(t, descendantQueueNibs("todo"))
		pressKeys(app, "v m")
		if got, want := rowIDs(app), []string{"m1", "b1", "x", "e1", "m2"}; !slices.Equal(got, want) {
			t.Fatalf("premise failed: Milestones shows %v, want %v", got, want)
		}
		expectMove(t, app, stub, "e1", "ctrl+home", nil, reorderReasonOwnDescendant)
		expectMove(t, app, stub, "b1", "ctrl+end", nil, reorderReasonOwnDescendant)
		expectMove(t, app, stub, "x", "ctrl+home", []string{"x before b1 MILESTONE"}, "")
		mark(t, app, "x", "e1")
		expectMove(t, app, stub, "", "ctrl+up", nil, reorderReasonOwnDescendant)
		expectMove(t, app, stub, "", "ctrl+home", nil, reorderReasonOwnDescendant)
	})
	for _, tc := range []struct {
		name, b1Status, tag string
	}{
		{"hidden as closed", "completed", ""},
		{"hidden by a tag filter", "todo", "t"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, stub := setupTestApp(t, descendantQueueNibs(tc.b1Status))
			pressKeys(app, "v m")
			if tc.tag != "" {
				app.list.setQuery("tags:" + tc.tag)
			}
			if rows := rowIDs(app); slices.Contains(rows, "b1") || !slices.Contains(rows, "e1") {
				t.Fatalf("premise failed: Milestones shows %v, want e1 without b1", rows)
			}
			expectMove(t, app, stub, "e1", "ctrl+home", nil, reorderReasonOwnDescendant)
			expectMove(t, app, stub, "x", "ctrl+home", []string{"x before b1 MILESTONE"}, "")
			mark(t, app, "x", "e1")
			expectMove(t, app, stub, "", "ctrl+home", nil, reorderReasonOwnDescendant)
		})
	}
}

// Closed nibs are hidden by default, and a move to the top or bottom still
// passes the hidden ones of its group.
func TestTopAndBottomPassHiddenNibsOfTheGroup(t *testing.T) {
	t.Run("Tree", func(t *testing.T) {
		app, stub := setupTestApp(t, []*nib.Nib{
			{ID: "X", Title: "X", Type: "task", Status: "completed", Order: "0"},
			{ID: "A", Title: "A", Type: "task", Status: "todo", Order: "1"},
			{ID: "B", Title: "B", Type: "task", Status: "todo", Order: "2"},
			{ID: "Z", Title: "Z", Type: "task", Status: "completed", Order: "3"},
		})
		if got, want := rowIDs(app), []string{"A", "B"}; !slices.Equal(got, want) {
			t.Fatalf("premise failed: Tree shows %v, want %v", got, want)
		}
		expectMove(t, app, stub, "A", "ctrl+up", nil, reorderReasonAtTop)
		expectMove(t, app, stub, "A", "ctrl+home", []string{"A before X PARENT"}, "")
		expectMove(t, app, stub, "B", "ctrl+end", []string{"B after Z PARENT"}, "")
		mark(t, app, "A", "B")
		expectMove(t, app, stub, "", "ctrl+home", []string{"A before X PARENT", "B after A PARENT"}, "")
	})
	t.Run("Milestones", func(t *testing.T) {
		app, stub := setupTestApp(t, []*nib.Nib{
			{ID: "ms1", Title: "Launch", Type: "milestone", Status: "todo", Order: "a0"},
			{ID: "q0", Title: "Done", Type: "task", Status: "completed", Order: "b0", Milestone: "ms1", MilestoneOrder: "a0"},
			{ID: "q1", Title: "One", Type: "task", Status: "todo", Order: "b1", Milestone: "ms1", MilestoneOrder: "a1"},
			{ID: "q2", Title: "Two", Type: "task", Status: "todo", Order: "b2", Milestone: "ms1", MilestoneOrder: "a2"},
		})
		pressKeys(app, "v m")
		if got, want := rowIDs(app), []string{"ms1", "q1", "q2"}; !slices.Equal(got, want) {
			t.Fatalf("premise failed: Milestones shows %v, want %v", got, want)
		}
		expectMove(t, app, stub, "q2", "ctrl+home", []string{"q2 before q0 MILESTONE"}, "")
		expectMove(t, app, stub, "q1", "ctrl+home", []string{"q1 before q0 MILESTONE"}, "")
		expectMove(t, app, stub, "q2", "ctrl+end", nil, reorderReasonAtBottom)
	})
}

// A block's later writes depend on the first, so a failed write stops the
// rest.
func TestAFailedWriteStopsTheBlock(t *testing.T) {
	app, stub := setupTestApp(t, blockTestNibs())
	mark(t, app, "B", "C")
	stub.ReorderErr = fmt.Errorf("disk full")
	pressKeys(app, "ctrl+home")
	if got := app.list.statusMessage; got != "Reorder failed: disk full" {
		t.Errorf("status = %q, want the failure", got)
	}
	if got, want := writes(stub), []string{"B before A PARENT"}; !slices.Equal(got, want) {
		t.Errorf("attempted %q, want only %q", got, want)
	}
}

// A later write of a block failing leaves the earlier ones on disk.
func TestAPartlyAppliedBlockSaysSo(t *testing.T) {
	app, stub := setupTestApp(t, blockTestNibs())
	mark(t, app, "B", "C")
	stub.ReorderErrByID = map[string]error{"C": fmt.Errorf("disk full")}
	// The store as B's write left it, which only a reload shows.
	moved := *stub.Nibs["B"]
	moved.Order = "0"
	stub.AllNibs = []*nib.Nib{stub.Nibs["A"], &moved, stub.Nibs["C"], stub.Nibs["D"]}
	pressKeys(app, "ctrl+home")
	if got, want := rowIDs(app), []string{"B", "A", "C", "D"}; !slices.Equal(got, want) {
		t.Errorf("rows after the failure = %v, want the reloaded %v", got, want)
	}
	if got, want := writes(stub), []string{"B before A PARENT", "C after B PARENT"}; !slices.Equal(got, want) {
		t.Errorf("attempted %q, want %q", got, want)
	}
	if got, want := app.list.statusMessage, "Reorder partly applied (1 of 2 nibs moved), list reloaded: disk full"; got != want {
		t.Errorf("status = %q, want %q", got, want)
	}
}
