package graph

import (
	"context"
	"testing"
	"time"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
)

// createSortTestNib creates a nib with specified fields for sorting tests.
// Note: core.Create() overwrites CreatedAt/UpdatedAt with time.Now(), so
// timestamp-sensitive tests should construct nib slices directly and call ApplySorting.
func createSortTestNib(t *testing.T, core *nibcore.Core, id, title, status, priority, order string) *nib.Nib {
	t.Helper()
	b := &nib.Nib{
		ID:       id,
		Slug:     nib.Slugify(title),
		Title:    title,
		Status:   status,
		Type:     "task",
		Priority: priority,
		Version:  1,
		Order:    order,
	}
	if err := core.Create(b); err != nil {
		t.Fatalf("failed to create test nib %s: %v", id, err)
	}
	return b
}

func TestNibsSortByTitle(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	// Create in non-alphabetical order so default ≠ title-sorted
	createSortTestNib(t, core, "sort-1", "Charlie", "todo", "normal", "a0")
	createSortTestNib(t, core, "sort-2", "Bravo", "todo", "normal", "b0")
	createSortTestNib(t, core, "sort-3", "alpha", "todo", "normal", "c0")

	sort := &model.NibSort{Field: model.NibSortFieldTitle}
	got, err := resolver.Query().Nibs(ctx, nil, sort)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d nibs, want 3", len(got))
	}

	// Case-insensitive: alpha, Bravo, Charlie
	wantOrder := []string{"sort-3", "sort-2", "sort-1"}
	for i, want := range wantOrder {
		if got[i].ID != want {
			t.Errorf("position %d: got %s (%s), want %s", i, got[i].ID, got[i].Title, want)
		}
	}
}

func TestNibsSortByCreatedAt(t *testing.T) {
	t1 := time.Date(2026, 3, 19, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 3, 19, 11, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)

	// Non-chronological input order
	nibs := []*nib.Nib{
		{ID: "sort-1", Title: "Third", CreatedAt: &t3},
		{ID: "sort-2", Title: "First", CreatedAt: &t1},
		{ID: "sort-3", Title: "Second", CreatedAt: &t2},
	}

	sort := &model.NibSort{Field: model.NibSortFieldCreatedAt}
	ApplySorting(nibs, sort, nil)

	wantOrder := []string{"sort-2", "sort-3", "sort-1"}
	for i, want := range wantOrder {
		if nibs[i].ID != want {
			t.Errorf("position %d: got %s, want %s", i, nibs[i].ID, want)
		}
	}
}

func TestNibsSortByUpdatedAt(t *testing.T) {
	t1 := time.Date(2026, 3, 17, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 3, 18, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC)

	nibs := []*nib.Nib{
		{ID: "sort-1", Title: "A", UpdatedAt: &t3},
		{ID: "sort-2", Title: "B", UpdatedAt: &t1},
		{ID: "sort-3", Title: "C", UpdatedAt: &t2},
	}

	sort := &model.NibSort{Field: model.NibSortFieldUpdatedAt}
	ApplySorting(nibs, sort, nil)

	wantOrder := []string{"sort-2", "sort-3", "sort-1"}
	for i, want := range wantOrder {
		if nibs[i].ID != want {
			t.Errorf("position %d: got %s, want %s", i, nibs[i].ID, want)
		}
	}
}

func TestNibsSortByPriority(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	createSortTestNib(t, core, "sort-1", "Low task", "todo", "low", "a0")
	createSortTestNib(t, core, "sort-2", "Critical task", "todo", "critical", "b0")
	createSortTestNib(t, core, "sort-3", "Normal task", "todo", "", "c0") // empty = normal
	createSortTestNib(t, core, "sort-4", "High task", "todo", "high", "d0")

	sort := &model.NibSort{Field: model.NibSortFieldPriority}
	got, err := resolver.Query().Nibs(ctx, nil, sort)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d nibs, want 4", len(got))
	}

	// Config order: critical, high, normal, low
	wantOrder := []string{"sort-2", "sort-4", "sort-3", "sort-1"}
	for i, want := range wantOrder {
		if got[i].ID != want {
			t.Errorf("position %d: got %s (%s), want %s", i, got[i].ID, got[i].Priority, want)
		}
	}
}

func TestNibsSortDescending(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	createSortTestNib(t, core, "sort-1", "Alpha", "todo", "normal", "a0")
	createSortTestNib(t, core, "sort-2", "Bravo", "todo", "normal", "b0")
	createSortTestNib(t, core, "sort-3", "Charlie", "todo", "normal", "c0")

	desc := model.SortDirectionDesc
	sort := &model.NibSort{Field: model.NibSortFieldOrder, Direction: &desc}
	got, err := resolver.Query().Nibs(ctx, nil, sort)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d nibs, want 3", len(got))
	}

	// Reversed: c0, b0, a0
	wantOrder := []string{"sort-3", "sort-2", "sort-1"}
	for i, want := range wantOrder {
		if got[i].ID != want {
			t.Errorf("position %d: got %s, want %s", i, got[i].ID, want)
		}
	}
}

func TestNibsSortWithFilter(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	createSortTestNib(t, core, "sort-1", "Charlie", "todo", "normal", "c0")
	createSortTestNib(t, core, "sort-2", "Alpha", "completed", "normal", "a0")
	createSortTestNib(t, core, "sort-3", "Bravo", "todo", "normal", "b0")

	filter := &model.NibFilter{Status: []string{"todo"}}
	sort := &model.NibSort{Field: model.NibSortFieldTitle}
	got, err := resolver.Query().Nibs(ctx, filter, sort)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d nibs, want 2 (filtered out completed)", len(got))
	}

	// Filtered to todo only, then sorted by title: Bravo, Charlie
	wantOrder := []string{"sort-3", "sort-1"}
	for i, want := range wantOrder {
		if got[i].ID != want {
			t.Errorf("position %d: got %s (%s), want %s", i, got[i].ID, got[i].Title, want)
		}
	}
}

func TestNibsSortNilPreservesOrder(t *testing.T) {
	nibs := []*nib.Nib{
		{ID: "c", Title: "Charlie"},
		{ID: "a", Title: "Alpha"},
		{ID: "b", Title: "Bravo"},
	}

	ApplySorting(nibs, nil, nil)

	// Should preserve original insertion order
	wantOrder := []string{"c", "a", "b"}
	for i, want := range wantOrder {
		if nibs[i].ID != want {
			t.Errorf("position %d: got %s, want %s", i, nibs[i].ID, want)
		}
	}
}

func TestChildrenSortOverridesDefaultOrder(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	// Create parent epic
	parent := &nib.Nib{ID: "parent-1", Slug: "parent", Title: "Parent", Status: "todo", Type: "epic", Version: 1}
	if err := core.Create(parent); err != nil {
		t.Fatalf("failed to create parent: %v", err)
	}

	// Create children with order keys that differ from title-alphabetical order
	for _, c := range []struct{ id, title, order string }{
		{"child-1", "Charlie", "a0"}, // first by order, last by title
		{"child-2", "Alpha", "b0"},   // second by order, first by title
		{"child-3", "Bravo", "c0"},   // third by order, second by title
	} {
		b := &nib.Nib{ID: c.id, Slug: nib.Slugify(c.title), Title: c.title, Status: "todo", Type: "task", Version: 1, Parent: "parent-1", Order: c.order}
		if err := core.Create(b); err != nil {
			t.Fatalf("failed to create child %s: %v", c.id, err)
		}
	}

	t.Run("nil sort uses default order key sort", func(t *testing.T) {
		got, err := resolver.Nib().Children(ctx, parent, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Default: sorted by order key (a0, b0, c0)
		wantOrder := []string{"child-1", "child-2", "child-3"}
		for i, want := range wantOrder {
			if got[i].ID != want {
				t.Errorf("position %d: got %s, want %s", i, got[i].ID, want)
			}
		}
	})

	t.Run("sort by title overrides default order", func(t *testing.T) {
		sort := &model.NibSort{Field: model.NibSortFieldTitle}
		got, err := resolver.Nib().Children(ctx, parent, nil, sort)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Title sort: Alpha, Bravo, Charlie
		wantOrder := []string{"child-2", "child-3", "child-1"}
		for i, want := range wantOrder {
			if got[i].ID != want {
				t.Errorf("position %d: got %s (%s), want %s", i, got[i].ID, got[i].Title, want)
			}
		}
	})
}

func TestNibsSortByStatus(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	// Create nibs with different statuses in non-config order
	createSortTestNib(t, core, "sort-1", "Completed task", "completed", "normal", "a0")
	createSortTestNib(t, core, "sort-2", "In-progress task", "in-progress", "normal", "b0")
	createSortTestNib(t, core, "sort-3", "Todo task", "todo", "normal", "c0")

	sort := &model.NibSort{Field: model.NibSortFieldStatus}
	got, err := resolver.Query().Nibs(ctx, nil, sort)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d nibs, want 3", len(got))
	}

	// Config status order: in-progress, todo, draft, completed, scrapped
	wantOrder := []string{"sort-2", "sort-3", "sort-1"}
	for i, want := range wantOrder {
		if got[i].ID != want {
			t.Errorf("position %d: got %s (%s), want %s", i, got[i].ID, got[i].Status, want)
		}
	}
}

func TestNibsSortByStatusDescending(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	createSortTestNib(t, core, "sort-1", "Completed task", "completed", "normal", "a0")
	createSortTestNib(t, core, "sort-2", "In-progress task", "in-progress", "normal", "b0")
	createSortTestNib(t, core, "sort-3", "Todo task", "todo", "normal", "c0")

	desc := model.SortDirectionDesc
	sort := &model.NibSort{Field: model.NibSortFieldStatus, Direction: &desc}
	got, err := resolver.Query().Nibs(ctx, nil, sort)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Reversed: completed, todo, in-progress
	wantOrder := []string{"sort-1", "sort-3", "sort-2"}
	for i, want := range wantOrder {
		if got[i].ID != want {
			t.Errorf("position %d: got %s (%s), want %s", i, got[i].ID, got[i].Status, want)
		}
	}
}

func TestNibsSortByID(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	createSortTestNib(t, core, "sort-c", "Charlie", "todo", "normal", "a0")
	createSortTestNib(t, core, "sort-a", "Alpha", "todo", "normal", "b0")
	createSortTestNib(t, core, "sort-b", "Bravo", "todo", "normal", "c0")

	sort := &model.NibSort{Field: model.NibSortFieldID}
	got, err := resolver.Query().Nibs(ctx, nil, sort)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d nibs, want 3", len(got))
	}

	wantOrder := []string{"sort-a", "sort-b", "sort-c"}
	for i, want := range wantOrder {
		if got[i].ID != want {
			t.Errorf("position %d: got %s, want %s", i, got[i].ID, want)
		}
	}
}

func TestNibsSortByStatusPriority(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	// Same status, different priorities and types
	createSortTestNib(t, core, "sort-1", "Low bug", "todo", "low", "a0")
	// Use custom create for type control
	for _, c := range []struct{ id, title, status, priority, typ, order string }{
		{"sort-2", "Critical feature", "todo", "critical", "feature", "b0"},
		{"sort-3", "Critical bug", "todo", "critical", "bug", "c0"},
		{"sort-4", "Done task", "completed", "critical", "task", "d0"},
	} {
		b := &nib.Nib{
			ID: c.id, Slug: nib.Slugify(c.title), Title: c.title,
			Status: c.status, Type: c.typ, Priority: c.priority,
			Version: 1, Order: c.order,
		}
		if err := core.Create(b); err != nil {
			t.Fatalf("failed to create %s: %v", c.id, err)
		}
	}

	sort := &model.NibSort{Field: model.NibSortFieldStatusPriority}
	got, err := resolver.Query().Nibs(ctx, nil, sort)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d nibs, want 4", len(got))
	}

	// Status order: in-progress, todo, draft, completed, scrapped
	// Within todo: critical bug, critical feature, low bug (sort-1 is task type)
	// Then completed: sort-4
	wantOrder := []string{"sort-3", "sort-2", "sort-1", "sort-4"}
	for i, want := range wantOrder {
		if got[i].ID != want {
			t.Errorf("position %d: got %s (%s/%s/%s), want %s",
				i, got[i].ID, got[i].Status, got[i].Priority, got[i].Type, want)
		}
	}
}

// TestApplySortingByStatus_OrderIncludesDeferred locks in the canonical status
// sort order derived from config.DefaultStatuses, including the deferred status
// which sits between draft (live, unrefined) and completed (terminal).
func TestApplySortingByStatus_OrderIncludesDeferred(t *testing.T) {
	// Deliberately scrambled input.
	nibs := []*nib.Nib{
		{ID: "scr", Status: "scrapped"},
		{ID: "cmp", Status: "completed"},
		{ID: "def", Status: "deferred"},
		{ID: "drf", Status: "draft"},
		{ID: "tod", Status: "todo"},
		{ID: "inp", Status: "in-progress"},
	}

	ApplySorting(nibs, &model.NibSort{Field: model.NibSortFieldStatus}, config.Default())

	wantOrder := []string{"in-progress", "todo", "draft", "deferred", "completed", "scrapped"}
	for i, want := range wantOrder {
		if nibs[i].Status != want {
			t.Errorf("position %d: got status %q, want %q", i, nibs[i].Status, want)
		}
	}
}

func TestNibsSortByCreatedAtWithNil(t *testing.T) {
	t1 := time.Date(2026, 3, 19, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)

	t.Run("ASC nil sorts first (zero time)", func(t *testing.T) {
		nibs := []*nib.Nib{
			{ID: "sort-1", Title: "Has time", CreatedAt: &t2},
			{ID: "sort-2", Title: "No time", CreatedAt: nil},
			{ID: "sort-3", Title: "Earlier", CreatedAt: &t1},
		}
		sort := &model.NibSort{Field: model.NibSortFieldCreatedAt}
		ApplySorting(nibs, sort, nil)

		wantOrder := []string{"sort-2", "sort-3", "sort-1"}
		for i, want := range wantOrder {
			if nibs[i].ID != want {
				t.Errorf("position %d: got %s, want %s", i, nibs[i].ID, want)
			}
		}
	})

	t.Run("DESC nil sorts last", func(t *testing.T) {
		nibs := []*nib.Nib{
			{ID: "sort-1", Title: "Has time", CreatedAt: &t2},
			{ID: "sort-2", Title: "No time", CreatedAt: nil},
			{ID: "sort-3", Title: "Earlier", CreatedAt: &t1},
		}
		desc := model.SortDirectionDesc
		sort := &model.NibSort{Field: model.NibSortFieldCreatedAt, Direction: &desc}
		ApplySorting(nibs, sort, nil)

		wantOrder := []string{"sort-1", "sort-3", "sort-2"}
		for i, want := range wantOrder {
			if nibs[i].ID != want {
				t.Errorf("position %d: got %s, want %s", i, nibs[i].ID, want)
			}
		}
	})
}

func TestNibsSortByStatusUnknownStatusSortsLast(t *testing.T) {
	nibs := []*nib.Nib{
		{ID: "sort-1", Title: "Unknown", Status: "custom-status"},
		{ID: "sort-2", Title: "In progress", Status: "in-progress"},
		{ID: "sort-3", Title: "Todo", Status: "todo"},
	}

	sort := &model.NibSort{Field: model.NibSortFieldStatus}
	ApplySorting(nibs, sort, nil)

	// Unknown statuses should sort LAST, not first
	wantOrder := []string{"sort-2", "sort-3", "sort-1"}
	for i, want := range wantOrder {
		if nibs[i].ID != want {
			t.Errorf("position %d: got %s (%s), want %s", i, nibs[i].ID, nibs[i].Status, want)
		}
	}
}

func TestNibsSortByStatusPriorityIgnoresDirection(t *testing.T) {
	// STATUS_PRIORITY is a multi-key composite sort. Reversing via slices.Reverse
	// would invert ALL keys simultaneously, producing semantically wrong results.
	// Direction should be ignored for this sort field.
	nibs := []*nib.Nib{
		{ID: "sort-1", Title: "Low task", Status: "todo", Type: "task", Priority: "low"},
		{ID: "sort-2", Title: "Critical bug", Status: "in-progress", Type: "bug", Priority: "critical"},
		{ID: "sort-3", Title: "Normal feature", Status: "todo", Type: "feature", Priority: "normal"},
	}

	asc := &model.NibSort{Field: model.NibSortFieldStatusPriority}
	desc := model.SortDirectionDesc
	descSort := &model.NibSort{Field: model.NibSortFieldStatusPriority, Direction: &desc}

	nibsCopy := make([]*nib.Nib, len(nibs))
	copy(nibsCopy, nibs)

	ApplySorting(nibs, asc, nil)
	ApplySorting(nibsCopy, descSort, nil)

	// ASC and DESC should produce identical results for STATUS_PRIORITY
	for i := range nibs {
		if nibs[i].ID != nibsCopy[i].ID {
			t.Errorf("position %d: ASC=%s DESC=%s — direction should be ignored for STATUS_PRIORITY",
				i, nibs[i].ID, nibsCopy[i].ID)
		}
	}
}

func TestNibsSortByOrder(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	createSortTestNib(t, core, "sort-1", "Charlie", "todo", "normal", "c0")
	createSortTestNib(t, core, "sort-2", "Alpha", "todo", "normal", "a0")
	createSortTestNib(t, core, "sort-3", "Bravo", "todo", "normal", "b0")

	sort := &model.NibSort{Field: model.NibSortFieldOrder}
	got, err := resolver.Query().Nibs(ctx, nil, sort)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d nibs, want 3", len(got))
	}

	wantOrder := []string{"sort-2", "sort-3", "sort-1"} // a0, b0, c0
	for i, want := range wantOrder {
		if got[i].ID != want {
			t.Errorf("position %d: got %s, want %s", i, got[i].ID, want)
		}
	}
}
