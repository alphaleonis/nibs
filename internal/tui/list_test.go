package tui

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
)

// makeTestNibs creates n nibs with sequential IDs and titles, all todo/task.
func makeTestNibs(n int) []*nib.Nib {
	nibs := make([]*nib.Nib, n)
	for i := range n {
		nibs[i] = &nib.Nib{
			ID:     fmt.Sprintf("nib-%03d", i+1),
			Title:  fmt.Sprintf("Task %03d", i+1),
			Status: "todo",
			Type:   "task",
		}
	}
	return nibs
}

func TestPgUpSnapsToFirstLineOfPage(t *testing.T) {
	// 50 nibs, window 120x40 ~ 35 items/page
	testNibs := makeTestNibs(50)
	app, _ := setupTestApp(t, testNibs)

	// Move cursor down so we're mid-page
	for range 5 {
		sendKey(app, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if app.list.list.Cursor() == 0 {
		t.Fatal("expected cursor to not be on first line")
	}

	// PgUp should snap to first line of current page, NOT change page
	sendKey(app, tea.KeyPressMsg{Code: tea.KeyPgUp})

	if app.list.list.Cursor() != 0 {
		t.Errorf("expected page-local cursor 0 after PgUp, got %d", app.list.list.Cursor())
	}
	if app.list.list.Paginator.Page != 0 {
		t.Errorf("expected to stay on page 0, got %d", app.list.list.Paginator.Page)
	}
}

func TestPgUpOnFirstLineChangesPrevPage(t *testing.T) {
	testNibs := makeTestNibs(50)
	app, _ := setupTestApp(t, testNibs)

	// Navigate to page 1 (cursor lands on last line of page 0, then page change)
	// First PgDn snaps to last line of page 0, second PgDn goes to page 1
	sendKey(app, tea.KeyPressMsg{Code: tea.KeyPgDown})
	sendKey(app, tea.KeyPressMsg{Code: tea.KeyPgDown})
	if app.list.list.Paginator.Page != 1 {
		t.Fatalf("expected page 1, got %d", app.list.list.Paginator.Page)
	}

	// Cursor should be on first line of page 1 (bubbles default after NextPage)
	// PgUp while on first line should go back to previous page
	if app.list.list.Cursor() != 0 {
		// Snap to first line first
		sendKey(app, tea.KeyPressMsg{Code: tea.KeyPgUp})
	}
	sendKey(app, tea.KeyPressMsg{Code: tea.KeyPgUp})

	if app.list.list.Paginator.Page != 0 {
		t.Errorf("expected page 0 after PgUp from first line of page 1, got %d", app.list.list.Paginator.Page)
	}
}

func TestPgDnSnapsToLastLineOfPage(t *testing.T) {
	testNibs := makeTestNibs(50)
	app, _ := setupTestApp(t, testNibs)

	// Cursor starts at first line
	if app.list.list.Cursor() != 0 {
		t.Fatalf("expected cursor at line 0, got %d", app.list.list.Cursor())
	}

	// PgDn should snap to last line of current page, NOT change page
	sendKey(app, tea.KeyPressMsg{Code: tea.KeyPgDown})

	perPage := app.list.list.Paginator.PerPage
	if app.list.list.Cursor() != perPage-1 {
		t.Errorf("expected page-local cursor %d after PgDn, got %d", perPage-1, app.list.list.Cursor())
	}
	if app.list.list.Paginator.Page != 0 {
		t.Errorf("expected to stay on page 0, got %d", app.list.list.Paginator.Page)
	}
}

func TestPgDnOnLastLineChangesNextPage(t *testing.T) {
	testNibs := makeTestNibs(50)
	app, _ := setupTestApp(t, testNibs)

	// First PgDn snaps to last line
	sendKey(app, tea.KeyPressMsg{Code: tea.KeyPgDown})
	if app.list.list.Paginator.Page != 0 {
		t.Fatalf("expected to still be on page 0 after first PgDn, got %d", app.list.list.Paginator.Page)
	}

	// Second PgDn should change page since we're on the last line
	sendKey(app, tea.KeyPressMsg{Code: tea.KeyPgDown})
	if app.list.list.Paginator.Page != 1 {
		t.Errorf("expected page 1 after PgDn from last line, got %d", app.list.list.Paginator.Page)
	}
}

func TestPgDnOnLastPageLastLineStaysOnLastItem(t *testing.T) {
	testNibs := makeTestNibs(50)
	app, _ := setupTestApp(t, testNibs)

	// Navigate to end: keep pressing PgDn until we can't go further
	for range 20 {
		sendKey(app, tea.KeyPressMsg{Code: tea.KeyPgDown})
	}

	lastIndex := len(app.list.list.VisibleItems()) - 1
	if app.list.list.Index() != lastIndex {
		t.Errorf("expected cursor at last item %d, got %d", lastIndex, app.list.list.Index())
	}
}

func TestPgDnSinglePageSnapsToLastItem(t *testing.T) {
	// Only 5 items — fits on one page, so bubbles disables PrevPage/NextPage bindings.
	testNibs := makeTestNibs(5)
	app, _ := setupTestApp(t, testNibs)

	if app.list.list.Paginator.TotalPages != 1 {
		t.Fatalf("expected 1 page, got %d", app.list.list.Paginator.TotalPages)
	}

	sendKey(app, tea.KeyPressMsg{Code: tea.KeyPgDown})

	if app.list.list.Index() != 4 {
		t.Errorf("expected cursor at last item 4, got %d", app.list.list.Index())
	}
}

func TestPgUpSinglePageSnapsToFirstItem(t *testing.T) {
	testNibs := makeTestNibs(5)
	app, _ := setupTestApp(t, testNibs)

	// Move cursor down
	for range 3 {
		sendKey(app, tea.KeyPressMsg{Code: tea.KeyDown})
	}

	sendKey(app, tea.KeyPressMsg{Code: tea.KeyPgUp})

	if app.list.list.Index() != 0 {
		t.Errorf("expected cursor at index 0, got %d", app.list.list.Index())
	}
}

func TestSortNibs(t *testing.T) {
	// Define the expected order from DefaultStatuses, DefaultPriorities, and DefaultTypes
	statusNames := []string{"draft", "todo", "in-progress", "completed", "scrapped"}
	typeNames := []string{"milestone", "epic", "bug", "feature", "task"}

	t.Run("sorts by status order first", func(t *testing.T) {
		nibs := []*nib.Nib{
			{ID: "1", Status: "completed", Type: "task", Title: "A"},
			{ID: "2", Status: "draft", Type: "task", Title: "B"},
			{ID: "3", Status: "in-progress", Type: "task", Title: "C"},
			{ID: "4", Status: "todo", Type: "task", Title: "D"},
			{ID: "5", Status: "scrapped", Type: "task", Title: "E"},
		}

		nib.SortByStatusPriorityAndType(nibs, statusNames, typeNames, config.Default())

		expected := []string{"draft", "todo", "in-progress", "completed", "scrapped"}
		for i, want := range expected {
			if nibs[i].Status != want {
				t.Errorf("index %d: got status %q, want %q", i, nibs[i].Status, want)
			}
		}
	})

	t.Run("sorts by priority within same status", func(t *testing.T) {
		nibs := []*nib.Nib{
			{ID: "1", Status: "todo", Type: "task", Priority: "low", Title: "A"},
			{ID: "2", Status: "todo", Type: "task", Priority: "critical", Title: "B"},
			{ID: "3", Status: "todo", Type: "task", Priority: "high", Title: "C"},
			{ID: "4", Status: "todo", Type: "task", Priority: "", Title: "D"}, // empty = normal
		}

		nib.SortByStatusPriorityAndType(nibs, statusNames, typeNames, config.Default())

		// Order: critical, high, normal (empty), low
		expectedPriorities := []string{"critical", "high", "", "low"}
		for i, want := range expectedPriorities {
			if nibs[i].Priority != want {
				t.Errorf("index %d: got priority %q, want %q", i, nibs[i].Priority, want)
			}
		}
	})

	t.Run("sorts by type order within same status and priority", func(t *testing.T) {
		nibs := []*nib.Nib{
			{ID: "1", Status: "todo", Type: "task", Title: "A"},
			{ID: "2", Status: "todo", Type: "milestone", Title: "B"},
			{ID: "3", Status: "todo", Type: "bug", Title: "C"},
			{ID: "4", Status: "todo", Type: "epic", Title: "D"},
			{ID: "5", Status: "todo", Type: "feature", Title: "E"},
		}

		nib.SortByStatusPriorityAndType(nibs, statusNames, typeNames, config.Default())

		expected := []string{"milestone", "epic", "bug", "feature", "task"}
		for i, want := range expected {
			if nibs[i].Type != want {
				t.Errorf("index %d: got type %q, want %q", i, nibs[i].Type, want)
			}
		}
	})

	t.Run("sorts by title within same status, priority, and type", func(t *testing.T) {
		nibs := []*nib.Nib{
			{ID: "1", Status: "todo", Type: "task", Title: "Zebra"},
			{ID: "2", Status: "todo", Type: "task", Title: "Apple"},
			{ID: "3", Status: "todo", Type: "task", Title: "Mango"},
		}

		nib.SortByStatusPriorityAndType(nibs, statusNames, typeNames, config.Default())

		expected := []string{"Apple", "Mango", "Zebra"}
		for i, want := range expected {
			if nibs[i].Title != want {
				t.Errorf("index %d: got title %q, want %q", i, nibs[i].Title, want)
			}
		}
	})

	t.Run("title sort is case-insensitive", func(t *testing.T) {
		nibs := []*nib.Nib{
			{ID: "1", Status: "todo", Type: "task", Title: "zebra"},
			{ID: "2", Status: "todo", Type: "task", Title: "Apple"},
			{ID: "3", Status: "todo", Type: "task", Title: "MANGO"},
		}

		nib.SortByStatusPriorityAndType(nibs, statusNames, typeNames, config.Default())

		expected := []string{"Apple", "MANGO", "zebra"}
		for i, want := range expected {
			if nibs[i].Title != want {
				t.Errorf("index %d: got title %q, want %q", i, nibs[i].Title, want)
			}
		}
	})

	t.Run("combined sort order: status > priority > type > title", func(t *testing.T) {
		nibs := []*nib.Nib{
			{ID: "1", Status: "completed", Type: "bug", Title: "Z"},
			{ID: "2", Status: "todo", Type: "task", Priority: "low", Title: "A"},
			{ID: "3", Status: "todo", Type: "bug", Priority: "high", Title: "B"},
			{ID: "4", Status: "todo", Type: "bug", Priority: "high", Title: "A"},
			{ID: "5", Status: "draft", Type: "epic", Title: "X"},
		}

		nib.SortByStatusPriorityAndType(nibs, statusNames, typeNames, config.Default())

		// Expected order:
		// 1. draft/epic/X (ID: 5)
		// 2. todo/high/bug/A (ID: 4)
		// 3. todo/high/bug/B (ID: 3)
		// 4. todo/low/task/A (ID: 2)
		// 5. completed/bug/Z (ID: 1)
		expectedIDs := []string{"5", "4", "3", "2", "1"}
		for i, want := range expectedIDs {
			if nibs[i].ID != want {
				t.Errorf("index %d: got ID %q, want %q (status=%s, priority=%s, type=%s, title=%s)",
					i, nibs[i].ID, want, nibs[i].Status, nibs[i].Priority, nibs[i].Type, nibs[i].Title)
			}
		}
	})

	t.Run("unrecognized status sorts last", func(t *testing.T) {
		nibs := []*nib.Nib{
			{ID: "1", Status: "unknown", Type: "task", Title: "A"},
			{ID: "2", Status: "todo", Type: "task", Title: "B"},
			{ID: "3", Status: "draft", Type: "task", Title: "C"},
		}

		nib.SortByStatusPriorityAndType(nibs, statusNames, typeNames, config.Default())

		// unknown status should be last
		if nibs[2].Status != "unknown" {
			t.Errorf("unrecognized status should be last, got %q at position 2", nibs[2].Status)
		}
	})

	t.Run("unrecognized type sorts last within status", func(t *testing.T) {
		nibs := []*nib.Nib{
			{ID: "1", Status: "todo", Type: "unknown", Title: "A"},
			{ID: "2", Status: "todo", Type: "task", Title: "B"},
			{ID: "3", Status: "todo", Type: "bug", Title: "C"},
		}

		nib.SortByStatusPriorityAndType(nibs, statusNames, typeNames, config.Default())

		// unknown type should be last within todo status
		if nibs[2].Type != "unknown" {
			t.Errorf("unrecognized type should be last, got %q at position 2", nibs[2].Type)
		}
	})

	t.Run("empty slice does not panic", func(t *testing.T) {
		nibs := []*nib.Nib{}
		nib.SortByStatusPriorityAndType(nibs, statusNames, typeNames, config.Default())
		// No assertion needed, just checking it doesn't panic
	})

	t.Run("single nib does not panic", func(t *testing.T) {
		nibs := []*nib.Nib{
			{ID: "1", Status: "todo", Type: "task", Title: "A"},
		}
		nib.SortByStatusPriorityAndType(nibs, statusNames, typeNames, config.Default())
		if nibs[0].ID != "1" {
			t.Error("single nib should remain unchanged")
		}
	})
}

func TestLessByStatusPriorityAndType(t *testing.T) {
	statusNames := []string{"draft", "todo", "in-progress", "completed", "scrapped"}
	typeNames := []string{"milestone", "epic", "bug", "feature", "task"}
	less := nib.LessByStatusPriorityAndType(statusNames, typeNames, config.Default())

	t.Run("compares by status first", func(t *testing.T) {
		a := &nib.Nib{ID: "1", Status: "todo", Type: "task", Title: "A"}
		b := &nib.Nib{ID: "2", Status: "draft", Type: "task", Title: "B"}

		// draft < todo, so b should come before a
		if less(a, b) {
			t.Error("draft nib should come before todo nib")
		}
		if !less(b, a) {
			t.Error("draft nib should come before todo nib")
		}
	})

	t.Run("compares by priority within same status", func(t *testing.T) {
		a := &nib.Nib{ID: "1", Status: "todo", Type: "task", Priority: "low", Title: "A"}
		b := &nib.Nib{ID: "2", Status: "todo", Type: "task", Priority: "high", Title: "B"}

		// high < low, so b should come before a
		if less(a, b) {
			t.Error("high priority nib should come before low priority nib")
		}
		if !less(b, a) {
			t.Error("high priority nib should come before low priority nib")
		}
	})

	t.Run("compares by type within same status and priority", func(t *testing.T) {
		a := &nib.Nib{ID: "1", Status: "todo", Type: "task", Title: "A"}
		b := &nib.Nib{ID: "2", Status: "todo", Type: "bug", Title: "B"}

		// bug < task, so b should come before a
		if less(a, b) {
			t.Error("bug nib should come before task nib")
		}
		if !less(b, a) {
			t.Error("bug nib should come before task nib")
		}
	})

	t.Run("compares by title within same status, priority, and type", func(t *testing.T) {
		a := &nib.Nib{ID: "1", Status: "todo", Type: "task", Title: "Zebra"}
		b := &nib.Nib{ID: "2", Status: "todo", Type: "task", Title: "Apple"}

		// Apple < Zebra, so b should come before a
		if less(a, b) {
			t.Error("Apple nib should come before Zebra nib")
		}
		if !less(b, a) {
			t.Error("Apple nib should come before Zebra nib")
		}
	})

	t.Run("title comparison is case-insensitive", func(t *testing.T) {
		a := &nib.Nib{ID: "1", Status: "todo", Type: "task", Title: "zebra"}
		b := &nib.Nib{ID: "2", Status: "todo", Type: "task", Title: "APPLE"}

		// apple < zebra (case-insensitive), so b should come before a
		if less(a, b) {
			t.Error("APPLE nib should come before zebra nib (case-insensitive)")
		}
	})

	t.Run("empty priority treated as normal", func(t *testing.T) {
		a := &nib.Nib{ID: "1", Status: "todo", Type: "task", Priority: "", Title: "A"}
		b := &nib.Nib{ID: "2", Status: "todo", Type: "task", Priority: "normal", Title: "B"}

		// Both should be equivalent in priority ordering
		// Since titles differ, A < B, so a should come before b
		if !less(a, b) {
			t.Error("empty priority should be treated as normal")
		}
	})

	t.Run("unrecognized status sorts last", func(t *testing.T) {
		a := &nib.Nib{ID: "1", Status: "unknown", Type: "task", Title: "A"}
		b := &nib.Nib{ID: "2", Status: "scrapped", Type: "task", Title: "B"}

		// scrapped is last known status, unknown should be after it
		if less(a, b) {
			t.Error("unknown status should sort after scrapped")
		}
		if !less(b, a) {
			t.Error("scrapped should sort before unknown")
		}
	})

	t.Run("unrecognized type sorts last within status", func(t *testing.T) {
		a := &nib.Nib{ID: "1", Status: "todo", Type: "unknown", Title: "A"}
		b := &nib.Nib{ID: "2", Status: "todo", Type: "task", Title: "B"}

		// task is last known type, unknown should be after it
		if less(a, b) {
			t.Error("unknown type should sort after task")
		}
		if !less(b, a) {
			t.Error("task should sort before unknown")
		}
	})
}
