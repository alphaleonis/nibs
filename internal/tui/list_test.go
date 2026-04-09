package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/ui"
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
		sendKey(app, tea.KeyMsg{Type: tea.KeyDown})
	}
	if app.list.list.Cursor() == 0 {
		t.Fatal("expected cursor to not be on first line")
	}

	// PgUp should snap to first line of current page, NOT change page
	sendKey(app, tea.KeyMsg{Type: tea.KeyPgUp})

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
	sendKey(app, tea.KeyMsg{Type: tea.KeyPgDown})
	sendKey(app, tea.KeyMsg{Type: tea.KeyPgDown})
	if app.list.list.Paginator.Page != 1 {
		t.Fatalf("expected page 1, got %d", app.list.list.Paginator.Page)
	}

	// Cursor should be on first line of page 1 (bubbles default after NextPage)
	// PgUp while on first line should go back to previous page
	if app.list.list.Cursor() != 0 {
		// Snap to first line first
		sendKey(app, tea.KeyMsg{Type: tea.KeyPgUp})
	}
	sendKey(app, tea.KeyMsg{Type: tea.KeyPgUp})

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
	sendKey(app, tea.KeyMsg{Type: tea.KeyPgDown})

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
	sendKey(app, tea.KeyMsg{Type: tea.KeyPgDown})
	if app.list.list.Paginator.Page != 0 {
		t.Fatalf("expected to still be on page 0 after first PgDn, got %d", app.list.list.Paginator.Page)
	}

	// Second PgDn should change page since we're on the last line
	sendKey(app, tea.KeyMsg{Type: tea.KeyPgDown})
	if app.list.list.Paginator.Page != 1 {
		t.Errorf("expected page 1 after PgDn from last line, got %d", app.list.list.Paginator.Page)
	}
}

func TestPgDnOnLastPageLastLineStaysOnLastItem(t *testing.T) {
	testNibs := makeTestNibs(50)
	app, _ := setupTestApp(t, testNibs)

	// Navigate to end: keep pressing PgDn until we can't go further
	for range 20 {
		sendKey(app, tea.KeyMsg{Type: tea.KeyPgDown})
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

	sendKey(app, tea.KeyMsg{Type: tea.KeyPgDown})

	if app.list.list.Index() != 4 {
		t.Errorf("expected cursor at last item 4, got %d", app.list.list.Index())
	}
}

func TestPgUpSinglePageSnapsToFirstItem(t *testing.T) {
	testNibs := makeTestNibs(5)
	app, _ := setupTestApp(t, testNibs)

	// Move cursor down
	for range 3 {
		sendKey(app, tea.KeyMsg{Type: tea.KeyDown})
	}

	sendKey(app, tea.KeyMsg{Type: tea.KeyPgUp})

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
			{ID: "4", Status: "todo", Type: "task", Priority: "", Title: "D"},       // empty = normal
			{ID: "5", Status: "todo", Type: "task", Priority: "deferred", Title: "E"},
		}

		nib.SortByStatusPriorityAndType(nibs, statusNames, typeNames, config.Default())

		// Order: critical, high, normal (empty), low, deferred
		expectedPriorities := []string{"critical", "high", "", "low", "deferred"}
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

func TestCompareNibsByStatusPriorityAndType(t *testing.T) {
	statusNames := []string{"draft", "todo", "in-progress", "completed", "scrapped"}

	typeNames := []string{"milestone", "epic", "bug", "feature", "task"}

	t.Run("compares by status first", func(t *testing.T) {
		a := &nib.Nib{ID: "1", Status: "todo", Type: "task", Title: "A"}
		b := &nib.Nib{ID: "2", Status: "draft", Type: "task", Title: "B"}

		// draft < todo, so b should come before a
		if compareNibsByStatusPriorityAndType(a, b, statusNames, typeNames, config.Default()) {
			t.Error("draft nib should come before todo nib")
		}
		if !compareNibsByStatusPriorityAndType(b, a, statusNames, typeNames, config.Default()) {
			t.Error("draft nib should come before todo nib")
		}
	})

	t.Run("compares by priority within same status", func(t *testing.T) {
		a := &nib.Nib{ID: "1", Status: "todo", Type: "task", Priority: "low", Title: "A"}
		b := &nib.Nib{ID: "2", Status: "todo", Type: "task", Priority: "high", Title: "B"}

		// high < low, so b should come before a
		if compareNibsByStatusPriorityAndType(a, b, statusNames, typeNames, config.Default()) {
			t.Error("high priority nib should come before low priority nib")
		}
		if !compareNibsByStatusPriorityAndType(b, a, statusNames, typeNames, config.Default()) {
			t.Error("high priority nib should come before low priority nib")
		}
	})

	t.Run("compares by type within same status and priority", func(t *testing.T) {
		a := &nib.Nib{ID: "1", Status: "todo", Type: "task", Title: "A"}
		b := &nib.Nib{ID: "2", Status: "todo", Type: "bug", Title: "B"}

		// bug < task, so b should come before a
		if compareNibsByStatusPriorityAndType(a, b, statusNames, typeNames, config.Default()) {
			t.Error("bug nib should come before task nib")
		}
		if !compareNibsByStatusPriorityAndType(b, a, statusNames, typeNames, config.Default()) {
			t.Error("bug nib should come before task nib")
		}
	})

	t.Run("compares by title within same status, priority, and type", func(t *testing.T) {
		a := &nib.Nib{ID: "1", Status: "todo", Type: "task", Title: "Zebra"}
		b := &nib.Nib{ID: "2", Status: "todo", Type: "task", Title: "Apple"}

		// Apple < Zebra, so b should come before a
		if compareNibsByStatusPriorityAndType(a, b, statusNames, typeNames, config.Default()) {
			t.Error("Apple nib should come before Zebra nib")
		}
		if !compareNibsByStatusPriorityAndType(b, a, statusNames, typeNames, config.Default()) {
			t.Error("Apple nib should come before Zebra nib")
		}
	})

	t.Run("title comparison is case-insensitive", func(t *testing.T) {
		a := &nib.Nib{ID: "1", Status: "todo", Type: "task", Title: "zebra"}
		b := &nib.Nib{ID: "2", Status: "todo", Type: "task", Title: "APPLE"}

		// apple < zebra (case-insensitive), so b should come before a
		if compareNibsByStatusPriorityAndType(a, b, statusNames, typeNames, config.Default()) {
			t.Error("APPLE nib should come before zebra nib (case-insensitive)")
		}
	})

	t.Run("empty priority treated as normal", func(t *testing.T) {
		a := &nib.Nib{ID: "1", Status: "todo", Type: "task", Priority: "", Title: "A"}
		b := &nib.Nib{ID: "2", Status: "todo", Type: "task", Priority: "normal", Title: "B"}

		// Both should be equivalent in priority ordering
		// Since titles differ, A < B, so a should come before b
		if !compareNibsByStatusPriorityAndType(a, b, statusNames, typeNames, config.Default()) {
			t.Error("empty priority should be treated as normal")
		}
	})

	t.Run("unrecognized status sorts last", func(t *testing.T) {
		a := &nib.Nib{ID: "1", Status: "unknown", Type: "task", Title: "A"}
		b := &nib.Nib{ID: "2", Status: "scrapped", Type: "task", Title: "B"}

		// scrapped is last known status, unknown should be after it
		if compareNibsByStatusPriorityAndType(a, b, statusNames, typeNames, config.Default()) {
			t.Error("unknown status should sort after scrapped")
		}
		if !compareNibsByStatusPriorityAndType(b, a, statusNames, typeNames, config.Default()) {
			t.Error("scrapped should sort before unknown")
		}
	})

	t.Run("unrecognized type sorts last within status", func(t *testing.T) {
		a := &nib.Nib{ID: "1", Status: "todo", Type: "unknown", Title: "A"}
		b := &nib.Nib{ID: "2", Status: "todo", Type: "task", Title: "B"}

		// task is last known type, unknown should be after it
		if compareNibsByStatusPriorityAndType(a, b, statusNames, typeNames, config.Default()) {
			t.Error("unknown type should sort after task")
		}
		if !compareNibsByStatusPriorityAndType(b, a, statusNames, typeNames, config.Default()) {
			t.Error("task should sort before unknown")
		}
	})
}

func TestFindSiblings(t *testing.T) {
	// Build a tree: parent -> [child1, child2, child3]
	cfg := config.Default()
	m := newListModel(&StubBackend{Nibs: map[string]*nib.Nib{}}, cfg)
	m.tree = []*ui.TreeNode{
		{
			Nib: &nib.Nib{ID: "parent", Title: "Parent"},
			Children: []*ui.TreeNode{
				{Nib: &nib.Nib{ID: "c1", Title: "First", Parent: "parent"}},
				{Nib: &nib.Nib{ID: "c2", Title: "Second", Parent: "parent"}},
				{Nib: &nib.Nib{ID: "c3", Title: "Third", Parent: "parent"}},
			},
		},
	}

	t.Run("findSiblings returns all children of parent", func(t *testing.T) {
		sibs := m.findSiblings(&nib.Nib{ID: "c2", Parent: "parent"})
		if len(sibs) != 3 {
			t.Fatalf("expected 3 siblings, got %d", len(sibs))
		}
	})

	t.Run("findSiblings returns root siblings for root nib", func(t *testing.T) {
		sibs := m.findSiblings(&nib.Nib{ID: "parent"})
		if len(sibs) != 1 {
			t.Fatalf("expected 1 root sibling, got %d", len(sibs))
		}
		if sibs[0].ID != "parent" {
			t.Errorf("expected root sibling 'parent', got %q", sibs[0].ID)
		}
	})

	t.Run("findSiblings returns nil when tree is nil", func(t *testing.T) {
		m2 := newListModel(&StubBackend{Nibs: map[string]*nib.Nib{}}, cfg)
		sibs := m2.findSiblings(&nib.Nib{ID: "c1", Parent: "parent"})
		if sibs != nil {
			t.Errorf("expected nil with nil tree, got %v", sibs)
		}
	})

	t.Run("findPreviousSibling returns previous", func(t *testing.T) {
		prev := m.findPreviousSibling(&nib.Nib{ID: "c2", Parent: "parent"})
		if prev == nil || prev.ID != "c1" {
			t.Errorf("expected c1, got %v", prev)
		}
	})

	t.Run("findPreviousSibling returns nil for first", func(t *testing.T) {
		prev := m.findPreviousSibling(&nib.Nib{ID: "c1", Parent: "parent"})
		if prev != nil {
			t.Errorf("expected nil for first sibling, got %v", prev.ID)
		}
	})

	t.Run("findNextSibling returns next", func(t *testing.T) {
		next := m.findNextSibling(&nib.Nib{ID: "c2", Parent: "parent"})
		if next == nil || next.ID != "c3" {
			t.Errorf("expected c3, got %v", next)
		}
	})

	t.Run("findNextSibling returns nil for last", func(t *testing.T) {
		next := m.findNextSibling(&nib.Nib{ID: "c3", Parent: "parent"})
		if next != nil {
			t.Errorf("expected nil for last sibling, got %v", next.ID)
		}
	})
}

func TestBuildBorderTopLine(t *testing.T) {
	cfg := config.Default()

	t.Run("line width matches m.width", func(t *testing.T) {
		m := newListModel(&StubBackend{Nibs: map[string]*nib.Nib{}}, cfg)
		m.width = 80
		m.borderTitle = "Nibs - testproject"

		line := m.buildBorderTopLine()
		got := lipgloss.Width(line)
		if got != 80 {
			t.Errorf("expected line width 80, got %d", got)
		}
	})

	t.Run("contains title text", func(t *testing.T) {
		m := newListModel(&StubBackend{Nibs: map[string]*nib.Nib{}}, cfg)
		m.width = 80
		m.borderTitle = "Nibs - myproject"

		line := m.buildBorderTopLine()
		if !strings.Contains(line, "Nibs - myproject") {
			t.Error("expected border line to contain title text")
		}
	})

	t.Run("contains No completed badge when hiding", func(t *testing.T) {
		m := newListModel(&StubBackend{Nibs: map[string]*nib.Nib{}}, cfg)
		m.width = 80
		m.borderTitle = "Nibs - test"
		m.hideCompleted = true

		line := m.buildBorderTopLine()
		if !strings.Contains(line, "No completed") {
			t.Error("expected border line to contain 'No completed' badge")
		}
	})

	t.Run("no completed badge when showing completed", func(t *testing.T) {
		m := newListModel(&StubBackend{Nibs: map[string]*nib.Nib{}}, cfg)
		m.width = 80
		m.borderTitle = "Nibs - test"
		m.hideCompleted = false

		line := m.buildBorderTopLine()
		if strings.Contains(line, "completed") && strings.Contains(line, "No") {
			t.Error("expected no completed badge when not hiding")
		}
	})

	t.Run("contains Wide badge when wideMode is true", func(t *testing.T) {
		m := newListModel(&StubBackend{Nibs: map[string]*nib.Nib{}}, cfg)
		m.width = 80
		m.borderTitle = "Nibs - test"
		m.wideMode = true

		line := m.buildBorderTopLine()
		if !strings.Contains(line, "Wide") {
			t.Error("expected border line to contain Wide badge when wideMode is true")
		}
	})

	t.Run("no Wide badge when wideMode is false", func(t *testing.T) {
		m := newListModel(&StubBackend{Nibs: map[string]*nib.Nib{}}, cfg)
		m.width = 80
		m.borderTitle = "Nibs - test"
		m.wideMode = false

		line := m.buildBorderTopLine()
		if strings.Contains(line, "Wide") {
			t.Error("expected border line to NOT contain Wide badge when wideMode is false")
		}
	})

	t.Run("contains tag filter badge", func(t *testing.T) {
		m := newListModel(&StubBackend{Nibs: map[string]*nib.Nib{}}, cfg)
		m.width = 100
		m.borderTitle = "Nibs - test"
		m.tagFilter = "urgent"

		line := m.buildBorderTopLine()
		if !strings.Contains(line, "tag: urgent") {
			t.Error("expected border line to contain tag filter badge")
		}
	})

	t.Run("width matches with all badges active", func(t *testing.T) {
		m := newListModel(&StubBackend{Nibs: map[string]*nib.Nib{}}, cfg)
		m.width = 120
		m.borderTitle = "Nibs - myproject"
		m.tagFilter = "feature"
		m.wideMode = true
		m.hideCompleted = true

		line := m.buildBorderTopLine()
		got := lipgloss.Width(line)
		if got != 120 {
			t.Errorf("expected line width 120 with all badges, got %d", got)
		}
	})

	t.Run("starts with rounded corner and ends with rounded corner", func(t *testing.T) {
		m := newListModel(&StubBackend{Nibs: map[string]*nib.Nib{}}, cfg)
		m.width = 80
		m.borderTitle = "Nibs - test"

		line := m.buildBorderTopLine()
		plain := stripAnsi(line)
		if !strings.HasPrefix(plain, "╭") {
			t.Errorf("expected line to start with ╭, got %q", plain[:20])
		}
		if !strings.HasSuffix(plain, "╮") {
			t.Errorf("expected line to end with ╮, got %q", plain[len(plain)-20:])
		}
	})
}
