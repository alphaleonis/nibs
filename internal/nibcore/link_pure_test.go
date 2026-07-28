package nibcore

import (
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
)

// releasesDependentsForTest is the "does this blocker still count" predicate
// the pure map queries take. It is the real config definition, so these tests
// exercise the same releasing set the Core wrappers supply instead of a private
// copy that could drift from it. Note it is narrower than the closed set:
// deferred is closed but does not release, so it keeps blocking.
var releasesDependentsForTest = config.Default().StatusReleasesDependents

func TestFindIncomingLinksInMap(t *testing.T) {
	nibs := map[string]*nib.Nib{
		"a": {ID: "a", Status: "todo", Parent: "c"},
		"b": {ID: "b", Status: "todo", BlockedBy: []string{"a", "d"}},
		"c": {ID: "c", Status: "todo"},
		"d": {ID: "d", Status: "todo"},
	}

	t.Run("finds blocked_by incoming links", func(t *testing.T) {
		// "a" is in b's BlockedBy, so a has an incoming blocked_by link from b
		links := findIncomingLinksInMap(nibs, "a")
		var blockedByLinks []nib.IncomingLink
		for _, l := range links {
			if l.LinkType == "blocked_by" {
				blockedByLinks = append(blockedByLinks, l)
			}
		}
		if len(blockedByLinks) != 1 {
			t.Errorf("expected 1 blocked_by incoming link to a, got %d", len(blockedByLinks))
		}
		if len(blockedByLinks) > 0 && blockedByLinks[0].FromNib.ID != "b" {
			t.Errorf("expected blocked_by from b, got from %s", blockedByLinks[0].FromNib.ID)
		}
	})

	t.Run("finds parent incoming links", func(t *testing.T) {
		// "a" has parent "c", so c has an incoming parent link from a
		links := findIncomingLinksInMap(nibs, "c")
		if len(links) != 1 {
			t.Fatalf("expected 1 incoming link to c, got %d", len(links))
		}
		if links[0].LinkType != "parent" || links[0].FromNib.ID != "a" {
			t.Errorf("expected parent link from a, got %s from %s", links[0].LinkType, links[0].FromNib.ID)
		}
	})

	t.Run("returns empty for nib with no incoming links", func(t *testing.T) {
		links := findIncomingLinksInMap(nibs, "b")
		if len(links) != 0 {
			t.Errorf("expected 0 incoming links to b, got %d", len(links))
		}
	})

	t.Run("returns empty for nonexistent target", func(t *testing.T) {
		links := findIncomingLinksInMap(nibs, "nonexistent")
		if len(links) != 0 {
			t.Errorf("expected 0 incoming links, got %d", len(links))
		}
	})
}

func TestIsBlockedInMap(t *testing.T) {
	nibs := map[string]*nib.Nib{
		"active-blocker":    {ID: "active-blocker", Status: "todo"},
		"completed-blocker": {ID: "completed-blocker", Status: "completed"},
		"scrapped-blocker":  {ID: "scrapped-blocker", Status: "scrapped"},
		"deferred-blocker":  {ID: "deferred-blocker", Status: "deferred"},
		"blocked-by-active": {
			ID: "blocked-by-active", Status: "todo",
			BlockedBy: []string{"active-blocker"},
		},
		"blocked-by-deferred": {
			ID: "blocked-by-deferred", Status: "todo",
			BlockedBy: []string{"deferred-blocker"},
		},
		"blocked-by-completed": {
			ID: "blocked-by-completed", Status: "todo",
			BlockedBy: []string{"completed-blocker"},
		},
		"blocked-by-scrapped": {
			ID: "blocked-by-scrapped", Status: "todo",
			BlockedBy: []string{"scrapped-blocker"},
		},
		"not-blocked": {ID: "not-blocked", Status: "todo"},
		"blocked-by-broken": {
			ID: "blocked-by-broken", Status: "todo",
			BlockedBy: []string{"nonexistent"},
		},
		"mixed-blockers": {
			ID: "mixed-blockers", Status: "todo",
			BlockedBy: []string{"active-blocker", "completed-blocker"},
		},
		"all-resolved": {
			ID: "all-resolved", Status: "todo",
			BlockedBy: []string{"completed-blocker", "scrapped-blocker"},
		},
	}

	tests := []struct {
		name  string
		nibID string
		want  bool
	}{
		{"blocked by active", "blocked-by-active", true},
		{"blocked by completed", "blocked-by-completed", false},
		{"blocked by scrapped", "blocked-by-scrapped", false},
		// deferred is a closed status but does not satisfy the dependency —
		// parked work is coming back, so its dependents stay blocked. Using the
		// closed predicate here would report false.
		{"blocked by deferred", "blocked-by-deferred", true},
		{"not blocked", "not-blocked", false},
		{"broken blocker link", "blocked-by-broken", false},
		{"mixed blockers (one active)", "mixed-blockers", true},
		{"all resolved blockers", "all-resolved", false},
		{"nonexistent nib", "nonexistent", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isBlockedInMap(nibs, tt.nibID, releasesDependentsForTest)
			if got != tt.want {
				t.Errorf("isBlockedInMap(%q) = %v, want %v", tt.nibID, got, tt.want)
			}
		})
	}
}

func TestFindActiveBlockersInMap(t *testing.T) {
	nibs := map[string]*nib.Nib{
		"active-1":  {ID: "active-1", Status: "in-progress"},
		"active-2":  {ID: "active-2", Status: "todo"},
		"completed": {ID: "completed", Status: "completed"},
		"deferred":  {ID: "deferred", Status: "deferred"},
		"target": {
			ID: "target", Status: "todo",
			BlockedBy: []string{"active-1", "active-2", "completed", "deferred"},
		},
		"no-blockers": {ID: "no-blockers", Status: "todo"},
	}

	// The deferred blocker is the discriminating case: it is closed, so the
	// closed predicate would drop it, but it has not released its dependents.
	t.Run("returns only active blockers, deferred included", func(t *testing.T) {
		blockers := findActiveBlockersInMap(nibs, "target", releasesDependentsForTest)
		if len(blockers) != 3 {
			t.Fatalf("expected 3 active blockers, got %d", len(blockers))
		}
		ids := map[string]bool{}
		for _, b := range blockers {
			ids[b.ID] = true
		}
		if !ids["active-1"] || !ids["active-2"] || !ids["deferred"] {
			t.Errorf("expected active-1, active-2 and deferred, got %v", ids)
		}
		if ids["completed"] {
			t.Errorf("completed blocker should have been released, got %v", ids)
		}
	})

	t.Run("returns nil for nib with no blockers", func(t *testing.T) {
		blockers := findActiveBlockersInMap(nibs, "no-blockers", releasesDependentsForTest)
		if len(blockers) != 0 {
			t.Errorf("expected 0 blockers, got %d", len(blockers))
		}
	})

	t.Run("returns nil for nonexistent nib", func(t *testing.T) {
		blockers := findActiveBlockersInMap(nibs, "nonexistent", releasesDependentsForTest)
		if blockers != nil {
			t.Errorf("expected nil, got %v", blockers)
		}
	})
}

func TestIsBlockingInMap(t *testing.T) {
	nibs := map[string]*nib.Nib{
		"active-blocker":    {ID: "active-blocker", Status: "todo"},
		"completed-blocker": {ID: "completed-blocker", Status: "completed"},
		"deferred-blocker":  {ID: "deferred-blocker", Status: "deferred"},
		"active-target": {
			ID: "active-target", Status: "todo",
			BlockedBy: []string{"active-blocker", "completed-blocker", "deferred-blocker"},
		},
		"completed-target": {
			ID: "completed-target", Status: "completed",
			BlockedBy: []string{"active-blocker"},
		},
		"not-blocking": {ID: "not-blocking", Status: "todo"},
	}

	tests := []struct {
		name  string
		nibID string
		want  bool
	}{
		{"active nib blocking active target", "active-blocker", true},
		{"completed nib cannot be blocking", "completed-blocker", false},
		// Closed, but it never released active-target — so it still blocks.
		{"deferred nib is still blocking", "deferred-blocker", true},
		{"not blocking anything", "not-blocking", false},
		{"nonexistent nib", "nonexistent", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isBlockingInMap(nibs, tt.nibID, releasesDependentsForTest)
			if got != tt.want {
				t.Errorf("isBlockingInMap(%q) = %v, want %v", tt.nibID, got, tt.want)
			}
		})
	}
}

func TestValidateParentInMap(t *testing.T) {
	nibs := map[string]*nib.Nib{
		"milestone-1": {ID: "milestone-1", Type: "milestone", Status: "todo"},
		"epic-1":      {ID: "epic-1", Type: "epic", Status: "todo"},
		"feature-1":   {ID: "feature-1", Type: "feature", Status: "todo"},
		"task-1":      {ID: "task-1", Type: "task", Status: "todo"},
	}

	t.Run("valid: task under feature", func(t *testing.T) {
		child := &nib.Nib{ID: "new-task", Type: "task"}
		err := ValidateParentInMap(nibs, child, "feature-1")
		if err != nil {
			t.Errorf("expected nil, got %v", err)
		}
	})

	t.Run("valid: epic under milestone", func(t *testing.T) {
		child := &nib.Nib{ID: "new-epic", Type: "epic"}
		err := ValidateParentInMap(nibs, child, "milestone-1")
		if err != nil {
			t.Errorf("expected nil, got %v", err)
		}
	})

	t.Run("invalid: milestone cannot have parent", func(t *testing.T) {
		child := &nib.Nib{ID: "new-ms", Type: "milestone"}
		err := ValidateParentInMap(nibs, child, "milestone-1")
		if err == nil {
			t.Error("expected error for milestone parent, got nil")
		}
	})

	t.Run("invalid: epic under task", func(t *testing.T) {
		child := &nib.Nib{ID: "new-epic", Type: "epic"}
		err := ValidateParentInMap(nibs, child, "task-1")
		if err == nil {
			t.Error("expected error for epic under task, got nil")
		}
	})

	t.Run("parent not found", func(t *testing.T) {
		child := &nib.Nib{ID: "new-task", Type: "task"}
		err := ValidateParentInMap(nibs, child, "nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent parent, got nil")
		}
	})

	t.Run("empty parent is valid", func(t *testing.T) {
		child := &nib.Nib{ID: "new-task", Type: "task"}
		err := ValidateParentInMap(nibs, child, "")
		if err != nil {
			t.Errorf("expected nil for empty parent, got %v", err)
		}
	})
}

func TestFindCyclesInMap(t *testing.T) {
	t.Run("detects blocked_by cycle", func(t *testing.T) {
		nibs := map[string]*nib.Nib{
			"a": {ID: "a", Status: "todo", BlockedBy: []string{"b"}},
			"b": {ID: "b", Status: "todo", BlockedBy: []string{"a"}},
			"c": {ID: "c", Status: "todo"},
		}

		cycles := FindCyclesInMap(nibs, "blocked_by")
		if len(cycles) != 1 {
			t.Fatalf("expected 1 cycle, got %d", len(cycles))
		}
		if cycles[0].LinkType != "blocked_by" {
			t.Errorf("expected blocked_by link type, got %s", cycles[0].LinkType)
		}
		if len(cycles[0].Path) < 3 {
			t.Errorf("cycle path too short: %v", cycles[0].Path)
		}
	})

	t.Run("detects parent cycle", func(t *testing.T) {
		nibs := map[string]*nib.Nib{
			"x": {ID: "x", Status: "todo", Parent: "y"},
			"y": {ID: "y", Status: "todo", Parent: "z"},
			"z": {ID: "z", Status: "todo", Parent: "x"},
		}

		cycles := FindCyclesInMap(nibs, "parent")
		if len(cycles) != 1 {
			t.Fatalf("expected 1 cycle, got %d", len(cycles))
		}
	})

	t.Run("no cycles in clean data", func(t *testing.T) {
		nibs := map[string]*nib.Nib{
			"a": {ID: "a", Status: "todo", BlockedBy: []string{"b"}},
			"b": {ID: "b", Status: "todo"},
		}

		cycles := FindCyclesInMap(nibs, "blocked_by")
		if len(cycles) != 0 {
			t.Errorf("expected 0 cycles, got %d", len(cycles))
		}
	})

	t.Run("deduplicates cycles", func(t *testing.T) {
		// A -> B -> A is the same cycle regardless of starting point
		nibs := map[string]*nib.Nib{
			"a": {ID: "a", Status: "todo", BlockedBy: []string{"b"}},
			"b": {ID: "b", Status: "todo", BlockedBy: []string{"a"}},
		}

		cycles := FindCyclesInMap(nibs, "blocked_by")
		if len(cycles) != 1 {
			t.Errorf("expected exactly 1 deduplicated cycle, got %d", len(cycles))
		}
	})

	t.Run("skips self-references", func(t *testing.T) {
		nibs := map[string]*nib.Nib{
			"a": {ID: "a", Status: "todo", BlockedBy: []string{"a"}},
		}

		cycles := FindCyclesInMap(nibs, "blocked_by")
		if len(cycles) != 0 {
			t.Errorf("self-references should not be reported as cycles, got %d", len(cycles))
		}
	})
}

func TestCheckAllLinksInMap(t *testing.T) {
	t.Run("detects broken links", func(t *testing.T) {
		nibs := map[string]*nib.Nib{
			"a": {ID: "a", Status: "todo", Parent: "nonexistent"},
			"b": {ID: "b", Status: "todo", BlockedBy: []string{"nonexistent2"}},
		}

		result := CheckAllLinksInMap(nibs, "")
		if len(result.BrokenLinks) != 2 {
			t.Errorf("expected 2 broken links, got %d", len(result.BrokenLinks))
		}
	})

	t.Run("detects self-references", func(t *testing.T) {
		nibs := map[string]*nib.Nib{
			"a": {ID: "a", Status: "todo", Parent: "a"},
			"b": {ID: "b", Status: "todo", BlockedBy: []string{"b"}},
		}

		result := CheckAllLinksInMap(nibs, "")
		if len(result.SelfLinks) != 2 {
			t.Errorf("expected 2 self-links, got %d", len(result.SelfLinks))
		}
	})

	t.Run("detects cycles", func(t *testing.T) {
		nibs := map[string]*nib.Nib{
			"a": {ID: "a", Status: "todo", BlockedBy: []string{"b"}},
			"b": {ID: "b", Status: "todo", BlockedBy: []string{"a"}},
		}

		result := CheckAllLinksInMap(nibs, "")
		if len(result.Cycles) != 1 {
			t.Errorf("expected 1 cycle, got %d", len(result.Cycles))
		}
	})

	t.Run("clean data has no issues", func(t *testing.T) {
		nibs := map[string]*nib.Nib{
			"a": {ID: "a", Status: "todo"},
			"b": {ID: "b", Status: "todo", BlockedBy: []string{"a"}},
		}

		result := CheckAllLinksInMap(nibs, "")
		if result.HasIssues() {
			t.Errorf("expected no issues, got broken=%d self=%d cycles=%d docs=%d",
				len(result.BrokenLinks), len(result.SelfLinks),
				len(result.Cycles), len(result.BrokenDocuments))
		}
	})

	t.Run("skips document checks when projectRoot is empty", func(t *testing.T) {
		nibs := map[string]*nib.Nib{
			"a": {ID: "a", Status: "todo", Documents: []string{"nonexistent/file.go"}},
		}

		result := CheckAllLinksInMap(nibs, "")
		if len(result.BrokenDocuments) != 0 {
			t.Errorf("expected 0 broken documents when projectRoot empty, got %d", len(result.BrokenDocuments))
		}
	})

	t.Run("checks documents when projectRoot provided", func(t *testing.T) {
		nibs := map[string]*nib.Nib{
			"a": {ID: "a", Status: "todo", Documents: []string{"definitely/does/not/exist.go"}},
		}

		result := CheckAllLinksInMap(nibs, "/tmp/nonexistent-project-root")
		if len(result.BrokenDocuments) != 1 {
			t.Errorf("expected 1 broken document, got %d", len(result.BrokenDocuments))
		}
	})

	t.Run("comprehensive: mixed issues", func(t *testing.T) {
		nibs := map[string]*nib.Nib{
			"a": {
				ID: "a", Status: "todo",
				BlockedBy: []string{"b", "a"}, // b valid, a self-ref
				Parent:    "nonexistent",       // broken
			},
			"b": {
				ID: "b", Status: "todo",
				BlockedBy: []string{"a"}, // creates cycle with a
			},
		}

		result := CheckAllLinksInMap(nibs, "")
		if len(result.BrokenLinks) != 1 {
			t.Errorf("broken links: want 1, got %d", len(result.BrokenLinks))
		}
		if len(result.SelfLinks) != 1 {
			t.Errorf("self links: want 1, got %d", len(result.SelfLinks))
		}
		if len(result.Cycles) != 1 {
			t.Errorf("cycles: want 1, got %d", len(result.Cycles))
		}
		if result.TotalIssues() != 3 {
			t.Errorf("total issues: want 3, got %d", result.TotalIssues())
		}
	})
}

func TestDetectCycleInMap(t *testing.T) {
	t.Run("detects blocked_by cycle", func(t *testing.T) {
		// Chain: A blocked_by B, B blocked_by C
		// Adding C blocked_by A would create cycle: C -> A -> B -> C
		nibs := map[string]*nib.Nib{
			"a": {ID: "a", Status: "todo", BlockedBy: []string{"b"}},
			"b": {ID: "b", Status: "todo", BlockedBy: []string{"c"}},
			"c": {ID: "c", Status: "todo"},
		}

		cycle := DetectCycleInMap(nibs, "c", "blocked_by", "a")
		if cycle == nil {
			t.Fatal("expected cycle, got nil")
		}
		if len(cycle) < 3 {
			t.Errorf("cycle path too short: %v", cycle)
		}
	})

	t.Run("detects parent cycle", func(t *testing.T) {
		// Chain: X.parent=Y, Y.parent=Z
		// Adding Z.parent=X would create cycle: Z -> X -> Y -> Z
		nibs := map[string]*nib.Nib{
			"x": {ID: "x", Status: "todo", Parent: "y"},
			"y": {ID: "y", Status: "todo", Parent: "z"},
			"z": {ID: "z", Status: "todo"},
		}

		cycle := DetectCycleInMap(nibs, "z", "parent", "x")
		if cycle == nil {
			t.Fatal("expected cycle, got nil")
		}
		if len(cycle) < 3 {
			t.Errorf("cycle path too short: %v", cycle)
		}
	})

	t.Run("no cycle when safe", func(t *testing.T) {
		nibs := map[string]*nib.Nib{
			"a": {ID: "a", Status: "todo", BlockedBy: []string{"b"}},
			"b": {ID: "b", Status: "todo"},
			"c": {ID: "c", Status: "todo"},
		}

		cycle := DetectCycleInMap(nibs, "c", "blocked_by", "a")
		if cycle != nil {
			t.Errorf("expected no cycle, got: %v", cycle)
		}
	})

	t.Run("ignores non-hierarchical link types", func(t *testing.T) {
		nibs := map[string]*nib.Nib{
			"a": {ID: "a", Status: "todo"},
			"b": {ID: "b", Status: "todo"},
		}

		cycle := DetectCycleInMap(nibs, "a", "some_other_type", "b")
		if cycle != nil {
			t.Errorf("expected nil for non-hierarchical link type, got: %v", cycle)
		}
	})

	t.Run("handles missing nib in chain", func(t *testing.T) {
		// A blocked_by B, but B references nonexistent C
		nibs := map[string]*nib.Nib{
			"a": {ID: "a", Status: "todo", BlockedBy: []string{"b"}},
			"b": {ID: "b", Status: "todo", BlockedBy: []string{"nonexistent"}},
		}

		cycle := DetectCycleInMap(nibs, "b", "blocked_by", "a")
		if cycle == nil {
			t.Fatal("expected cycle A->B->A, got nil")
		}
	})
}
