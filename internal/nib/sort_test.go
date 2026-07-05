package nib

import (
	"testing"
)

// testRanker mirrors Config.PriorityRank: empty->"normal", unknown->last.
// If PriorityRank contract changes, update this and see TestPriorityRank
// in internal/config/config_test.go.
type testRanker struct {
	names []string
}

func (r *testRanker) PriorityRank(priority string) int {
	if priority == "" {
		priority = "normal"
	}
	for i, n := range r.names {
		if n == priority {
			return i
		}
	}
	return len(r.names)
}

func TestSortByOrderDeterminism(t *testing.T) {
	t.Run("different order keys sort correctly", func(t *testing.T) {
		nibs := []*Nib{
			{ID: "c", Title: "Third", Order: "c0"},
			{ID: "a", Title: "First", Order: "a0"},
			{ID: "b", Title: "Second", Order: "b0"},
		}
		SortByOrder(nibs)
		if nibs[0].ID != "a" || nibs[1].ID != "b" || nibs[2].ID != "c" {
			t.Errorf("got %s,%s,%s want a,b,c", nibs[0].ID, nibs[1].ID, nibs[2].ID)
		}
	})

	t.Run("equal order keys use title tiebreaker", func(t *testing.T) {
		nibs := []*Nib{
			{ID: "z", Title: "Zebra", Order: "a0"},
			{ID: "a", Title: "Apple", Order: "a0"},
			{ID: "m", Title: "Mango", Order: "a0"},
		}
		SortByOrder(nibs)
		if nibs[0].Title != "Apple" || nibs[1].Title != "Mango" || nibs[2].Title != "Zebra" {
			t.Errorf("got %s,%s,%s want Apple,Mango,Zebra", nibs[0].Title, nibs[1].Title, nibs[2].Title)
		}
	})

	t.Run("equal order keys and titles use ID tiebreaker", func(t *testing.T) {
		nibs := []*Nib{
			{ID: "nib-c", Title: "Same", Order: "a0"},
			{ID: "nib-a", Title: "Same", Order: "a0"},
			{ID: "nib-b", Title: "Same", Order: "a0"},
		}
		SortByOrder(nibs)
		if nibs[0].ID != "nib-a" || nibs[1].ID != "nib-b" || nibs[2].ID != "nib-c" {
			t.Errorf("got %s,%s,%s want nib-a,nib-b,nib-c", nibs[0].ID, nibs[1].ID, nibs[2].ID)
		}
	})

	t.Run("deterministic across multiple runs with equal order keys", func(t *testing.T) {
		// Simulate map-like non-determinism by running multiple times with shuffled input
		for run := 0; run < 20; run++ {
			// Alternate input orders to simulate map iteration
			var nibs []*Nib
			if run%2 == 0 {
				nibs = []*Nib{
					{ID: "n1nb", Title: "Add investigation", Order: "a0"},
					{ID: "kofy", Title: "Status indicators", Order: "a0"},
					{ID: "an8t", Title: "Generalize context", Order: "a0"},
				}
			} else {
				nibs = []*Nib{
					{ID: "an8t", Title: "Generalize context", Order: "a0"},
					{ID: "n1nb", Title: "Add investigation", Order: "a0"},
					{ID: "kofy", Title: "Status indicators", Order: "a0"},
				}
			}
			SortByOrder(nibs)
			if nibs[0].ID != "n1nb" || nibs[1].ID != "an8t" || nibs[2].ID != "kofy" {
				t.Fatalf("run %d: got %s,%s,%s — non-deterministic sort detected",
					run, nibs[0].ID, nibs[1].ID, nibs[2].ID)
			}
		}
	})

	t.Run("ordered nibs before unordered", func(t *testing.T) {
		nibs := []*Nib{
			{ID: "u", Title: "Unordered", Order: ""},
			{ID: "o", Title: "Ordered", Order: "a0"},
		}
		SortByOrder(nibs)
		if nibs[0].ID != "o" || nibs[1].ID != "u" {
			t.Errorf("got %s,%s want o,u", nibs[0].ID, nibs[1].ID)
		}
	})

	t.Run("unordered nibs sorted by title then ID", func(t *testing.T) {
		nibs := []*Nib{
			{ID: "z", Title: "Zebra", Order: ""},
			{ID: "a", Title: "Apple", Order: ""},
			{ID: "b", Title: "Apple", Order: ""},
		}
		SortByOrder(nibs)
		if nibs[0].ID != "a" || nibs[1].ID != "b" || nibs[2].ID != "z" {
			t.Errorf("got %s,%s,%s want a,b,z", nibs[0].ID, nibs[1].ID, nibs[2].ID)
		}
	})
}

func TestPositionMap(t *testing.T) {
	t.Run("per-parent 1-based numbering", func(t *testing.T) {
		nibs := []*Nib{
			// Roots
			{ID: "r1", Order: "a0"},
			{ID: "r2", Order: "b0"},
			{ID: "r3", Order: "c0"},
			// Children of r1
			{ID: "c1a", Parent: "r1", Order: "a0"},
			{ID: "c1b", Parent: "r1", Order: "b0"},
			// Children of r2
			{ID: "c2a", Parent: "r2", Order: "a0"},
		}

		positions := PositionMap(nibs)

		want := map[string]int{
			"r1": 1, "r2": 2, "r3": 3,
			"c1a": 1, "c1b": 2,
			"c2a": 1,
		}
		for id, expected := range want {
			if got := positions[id]; got != expected {
				t.Errorf("positions[%q] = %d, want %d", id, got, expected)
			}
		}
		if len(positions) != len(want) {
			t.Errorf("got %d entries, want %d", len(positions), len(want))
		}
	})

	t.Run("position reflects natural order, ignoring input order", func(t *testing.T) {
		// Pass nibs in scrambled order; positions should follow Order key.
		nibs := []*Nib{
			{ID: "third", Order: "c0"},
			{ID: "first", Order: "a0"},
			{ID: "second", Order: "b0"},
		}

		positions := PositionMap(nibs)

		if positions["first"] != 1 || positions["second"] != 2 || positions["third"] != 3 {
			t.Errorf("got first=%d second=%d third=%d, want 1,2,3",
				positions["first"], positions["second"], positions["third"])
		}
	})

	t.Run("nibs without an order key sort after ordered ones", func(t *testing.T) {
		// SortByOrder places unordered nibs after ordered ones (sorted by title).
		// PositionMap should reflect that.
		nibs := []*Nib{
			{ID: "noord", Title: "ZZZ"},
			{ID: "ord", Title: "AAA", Order: "a0"},
		}

		positions := PositionMap(nibs)

		if positions["ord"] != 1 {
			t.Errorf("ordered nib should be position 1, got %d", positions["ord"])
		}
		if positions["noord"] != 2 {
			t.Errorf("unordered nib should be position 2, got %d", positions["noord"])
		}
	})

	t.Run("empty input returns empty map", func(t *testing.T) {
		positions := PositionMap(nil)
		if len(positions) != 0 {
			t.Errorf("got %d entries, want 0", len(positions))
		}
	})
}

func TestSortByStatusPriorityAndType(t *testing.T) {
	statusNames := []string{"draft", "todo", "in-progress", "completed"}
	ranker := &testRanker{names: []string{"critical", "high", "normal", "low"}}
	typeNames := []string{"bug", "feature", "task"}

	t.Run("sorts by status first", func(t *testing.T) {
		nibs := []*Nib{
			{ID: "1", Title: "A", Status: "completed", Priority: "critical"},
			{ID: "2", Title: "B", Status: "todo", Priority: "low"},
			{ID: "3", Title: "C", Status: "draft", Priority: "high"},
		}

		SortByStatusPriorityAndType(nibs, statusNames, typeNames, ranker)

		if nibs[0].Status != "draft" {
			t.Errorf("First nib status = %q, want \"draft\"", nibs[0].Status)
		}
		if nibs[1].Status != "todo" {
			t.Errorf("Second nib status = %q, want \"todo\"", nibs[1].Status)
		}
		if nibs[2].Status != "completed" {
			t.Errorf("Third nib status = %q, want \"completed\"", nibs[2].Status)
		}
	})

	t.Run("sorts by priority within same status", func(t *testing.T) {
		nibs := []*Nib{
			{ID: "1", Title: "E Low", Status: "todo", Priority: "low"},
			{ID: "2", Title: "A Critical", Status: "todo", Priority: "critical"},
			{ID: "3", Title: "B High", Status: "todo", Priority: "high"},
			{ID: "4", Title: "C Normal", Status: "todo", Priority: "normal"},
			{ID: "5", Title: "D No Priority", Status: "todo", Priority: ""},
		}

		SortByStatusPriorityAndType(nibs, statusNames, typeNames, ranker)

		// Order by priority: critical, high, normal (and empty), low
		// Within same priority, order by title alphabetically
		expectedOrder := []string{"A Critical", "B High", "C Normal", "D No Priority", "E Low"}
		for i, expected := range expectedOrder {
			if nibs[i].Title != expected {
				t.Errorf("nibs[%d].Title = %q, want %q", i, nibs[i].Title, expected)
			}
		}
	})

	t.Run("empty priority treated as normal", func(t *testing.T) {
		nibs := []*Nib{
			{ID: "1", Title: "Low", Status: "todo", Priority: "low"},
			{ID: "2", Title: "Empty", Status: "todo", Priority: ""},
			{ID: "3", Title: "Normal", Status: "todo", Priority: "normal"},
			{ID: "4", Title: "High", Status: "todo", Priority: "high"},
		}

		SortByStatusPriorityAndType(nibs, statusNames, typeNames, ranker)

		// High should come first, then Normal and Empty (same priority level), then Low
		if nibs[0].Title != "High" {
			t.Errorf("First nib = %q, want \"High\"", nibs[0].Title)
		}
		if nibs[3].Title != "Low" {
			t.Errorf("Last nib = %q, want \"Low\"", nibs[3].Title)
		}
		// Empty and Normal should be adjacent (both at normal priority level)
		normalIdx, emptyIdx := -1, -1
		for i, b := range nibs {
			if b.Title == "Normal" {
				normalIdx = i
			}
			if b.Title == "Empty" {
				emptyIdx = i
			}
		}
		if normalIdx != 1 && normalIdx != 2 {
			t.Errorf("Normal should be at index 1 or 2, got %d", normalIdx)
		}
		if emptyIdx != 1 && emptyIdx != 2 {
			t.Errorf("Empty should be at index 1 or 2, got %d", emptyIdx)
		}
	})

	t.Run("sorts by type after priority", func(t *testing.T) {
		nibs := []*Nib{
			{ID: "1", Title: "Task", Status: "todo", Priority: "high", Type: "task"},
			{ID: "2", Title: "Bug", Status: "todo", Priority: "high", Type: "bug"},
			{ID: "3", Title: "Feature", Status: "todo", Priority: "high", Type: "feature"},
		}

		SortByStatusPriorityAndType(nibs, statusNames, typeNames, ranker)

		if nibs[0].Type != "bug" {
			t.Errorf("First nib type = %q, want \"bug\"", nibs[0].Type)
		}
		if nibs[1].Type != "feature" {
			t.Errorf("Second nib type = %q, want \"feature\"", nibs[1].Type)
		}
		if nibs[2].Type != "task" {
			t.Errorf("Third nib type = %q, want \"task\"", nibs[2].Type)
		}
	})

	t.Run("empty type treated as task (default), not sorted last", func(t *testing.T) {
		// A type-less nib must sort at the "task" position, exactly as it did when
		// loadNib synthesized Type="task". This distinguishes the fix from a naive
		// "unknown type sorts last": task is NOT last in this order, so an empty
		// type ranked as "task" sorts BEFORE "research", whereas ranked as unknown
		// it would sort AFTER it (nibs-7d3o behavior preservation).
		typeNamesFull := []string{"milestone", "epic", "bug", "feature", "task", "research"}
		nibs := []*Nib{
			{ID: "research1", Title: "A Research", Status: "todo", Priority: "normal", Type: "research"},
			{ID: "notype1", Title: "B No Type", Status: "todo", Priority: "normal", Type: ""},
		}

		SortByStatusPriorityAndType(nibs, statusNames, typeNamesFull, ranker)

		if nibs[0].ID != "notype1" {
			t.Errorf("nibs[0].ID = %q, want notype1 (empty type must rank as task, before research)", nibs[0].ID)
		}
		if nibs[1].ID != "research1" {
			t.Errorf("nibs[1].ID = %q, want research1", nibs[1].ID)
		}
	})

	t.Run("sorts by title after type", func(t *testing.T) {
		nibs := []*Nib{
			{ID: "1", Title: "Zebra", Status: "todo", Priority: "high", Type: "bug"},
			{ID: "2", Title: "Apple", Status: "todo", Priority: "high", Type: "bug"},
			{ID: "3", Title: "Mango", Status: "todo", Priority: "high", Type: "bug"},
		}

		SortByStatusPriorityAndType(nibs, statusNames, typeNames, ranker)

		if nibs[0].Title != "Apple" {
			t.Errorf("First nib title = %q, want \"Apple\"", nibs[0].Title)
		}
		if nibs[1].Title != "Mango" {
			t.Errorf("Second nib title = %q, want \"Mango\"", nibs[1].Title)
		}
		if nibs[2].Title != "Zebra" {
			t.Errorf("Third nib title = %q, want \"Zebra\"", nibs[2].Title)
		}
	})

	t.Run("handles empty ranker names gracefully", func(t *testing.T) {
		nibs := []*Nib{
			{ID: "1", Title: "A", Status: "todo", Priority: "high"},
			{ID: "2", Title: "B", Status: "todo", Priority: ""},
		}

		// With no known priorities, all priorities get the same rank (0).
		// Ties break by type, then title, so alphabetical order is preserved.
		emptyRanker := &testRanker{names: nil}
		SortByStatusPriorityAndType(nibs, statusNames, typeNames, emptyRanker)

		if nibs[0].Title != "A" || nibs[1].Title != "B" {
			t.Errorf("got [%s, %s], want [A, B] (all priorities equal, sorted by title)",
				nibs[0].Title, nibs[1].Title)
		}
	})
}

