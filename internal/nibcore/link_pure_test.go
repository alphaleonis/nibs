package nibcore

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"gopkg.in/yaml.v3"
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
		// the set-aside work is coming back, so its dependents stay blocked. Using the
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
			got := isBlockedInMap(nibs, tt.nibID, "", releasesDependentsForTest)
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
		blockers := findActiveBlockersInMap(nibs, "target", "", releasesDependentsForTest)
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
		blockers := findActiveBlockersInMap(nibs, "no-blockers", "", releasesDependentsForTest)
		if len(blockers) != 0 {
			t.Errorf("expected 0 blockers, got %d", len(blockers))
		}
	})

	t.Run("returns nil for nonexistent nib", func(t *testing.T) {
		blockers := findActiveBlockersInMap(nibs, "nonexistent", "", releasesDependentsForTest)
		if blockers != nil {
			t.Errorf("expected nil, got %v", blockers)
		}
	})
}

// TestFindActiveBlockersInMapBlockerIDSpelling pins how a blockedBy entry is
// resolved: exact match first, then the configured prefix prepended — the same
// rule Core.Get applies, and so the same rule the projected `ready` field
// reaches its blockers by. A bare map lookup here misses a blocker named by
// short id, which is how `nibs list --ready` came to hand out a nib whose
// `ready` field said it was blocked. A short spelling reaches the store by
// hand-editing the file: the createNib/addBlockedBy resolvers every writing
// surface routes through normalize a blocker id before storing it.
func TestFindActiveBlockersInMapBlockerIDSpelling(t *testing.T) {
	tests := []struct {
		name     string
		spelling string // what the dependent's blocked_by names
		prefix   string
		want     bool // is the blocker found, and so still blocking
	}{
		{"full id", "nibs-blk", "nibs-", true},
		{"short id under the configured prefix", "blk", "nibs-", true},
		// Nothing to prepend, so the short form names no nib — and Core.Get
		// cannot resolve it either, so both surfaces agree it is unblocked.
		{"short id with no configured prefix", "blk", "", false},
		{"id that names no nib", "nope", "nibs-", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibs := map[string]*nib.Nib{
				"nibs-blk": {ID: "nibs-blk", Status: "in-progress"},
				"nibs-dep": {ID: "nibs-dep", Status: "todo", BlockedBy: []string{tt.spelling}},
			}

			blockers := findActiveBlockersInMap(nibs, "nibs-dep", tt.prefix, releasesDependentsForTest)
			if got := len(blockers) > 0; got != tt.want {
				t.Errorf("findActiveBlockersInMap found a blocker = %v, want %v (blocked_by: [%s], prefix %q)",
					got, tt.want, tt.spelling, tt.prefix)
			}
			// However it was spelled, the blocker comes back under its own id.
			if tt.want {
				if len(blockers) != 1 {
					t.Fatalf("got %d blockers, want 1", len(blockers))
				}
				if blockers[0].ID != "nibs-blk" {
					t.Errorf("blocker id = %q, want %q", blockers[0].ID, "nibs-blk")
				}
			}
			if got := isBlockedInMap(nibs, "nibs-dep", tt.prefix, releasesDependentsForTest); got != tt.want {
				t.Errorf("isBlockedInMap = %v, want %v (blocked_by: [%s], prefix %q)",
					got, tt.want, tt.spelling, tt.prefix)
			}
		})
	}
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

	t.Run("valid: feature under epic", func(t *testing.T) {
		child := &nib.Nib{ID: "new-feature", Type: "feature"}
		err := ValidateParentInMap(nibs, child, "epic-1")
		if err != nil {
			t.Errorf("expected nil, got %v", err)
		}
	})

	t.Run("invalid: epic under milestone", func(t *testing.T) {
		child := &nib.Nib{ID: "new-epic", Type: "epic"}
		err := ValidateParentInMap(nibs, child, "milestone-1")
		if err == nil {
			t.Error("expected error for epic under milestone, got nil")
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
			"c": {ID: "c", Status: "todo", Milestone: "nonexistent3"},
		}

		result := CheckAllLinksInMap(nibs, "", "")
		if len(result.BrokenLinks) != 3 {
			t.Errorf("expected 3 broken links, got %d", len(result.BrokenLinks))
		}
	})

	t.Run("detects self-references", func(t *testing.T) {
		nibs := map[string]*nib.Nib{
			"a": {ID: "a", Status: "todo", Parent: "a"},
			"b": {ID: "b", Status: "todo", BlockedBy: []string{"b"}},
			"c": {ID: "c", Status: "todo", Milestone: "c"},
		}

		result := CheckAllLinksInMap(nibs, "", "")
		if len(result.SelfLinks) != 3 {
			t.Errorf("expected 3 self-links, got %d", len(result.SelfLinks))
		}
	})

	t.Run("detects cycles", func(t *testing.T) {
		nibs := map[string]*nib.Nib{
			"a": {ID: "a", Status: "todo", BlockedBy: []string{"b"}},
			"b": {ID: "b", Status: "todo", BlockedBy: []string{"a"}},
		}

		result := CheckAllLinksInMap(nibs, "", "")
		if len(result.Cycles) != 1 {
			t.Errorf("expected 1 cycle, got %d", len(result.Cycles))
		}
	})

	t.Run("clean data has no issues", func(t *testing.T) {
		nibs := map[string]*nib.Nib{
			"a": {ID: "a", Status: "todo"},
			"b": {ID: "b", Status: "todo", BlockedBy: []string{"a"}},
		}

		result := CheckAllLinksInMap(nibs, "", "")
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

		result := CheckAllLinksInMap(nibs, "", "")
		if len(result.BrokenDocuments) != 0 {
			t.Errorf("expected 0 broken documents when projectRoot empty, got %d", len(result.BrokenDocuments))
		}
	})

	t.Run("checks documents when projectRoot provided", func(t *testing.T) {
		nibs := map[string]*nib.Nib{
			"a": {ID: "a", Status: "todo", Documents: []string{"definitely/does/not/exist.go"}},
		}

		result := CheckAllLinksInMap(nibs, "/tmp/nonexistent-project-root", "")
		if len(result.BrokenDocuments) != 1 {
			t.Errorf("expected 1 broken document, got %d", len(result.BrokenDocuments))
		}
	})

	t.Run("comprehensive: mixed issues", func(t *testing.T) {
		nibs := map[string]*nib.Nib{
			"a": {
				ID: "a", Status: "todo",
				BlockedBy: []string{"b", "a"}, // b valid, a self-ref
				Parent:    "nonexistent",      // broken
			},
			"b": {
				ID: "b", Status: "todo",
				BlockedBy: []string{"a"}, // creates cycle with a
			},
		}

		result := CheckAllLinksInMap(nibs, "", "")
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

// TestCheckAllLinksInMapIDSpelling pins how a parent, milestone or blockedBy
// target is resolved here: exact id first, then the configured prefix prepended — the
// rule normalizeIDInMap already gives Core.Get, findActiveBlockersInMap and the
// mention resolvers. A bare map lookup called a resolvable short-form target
// broken, and Core.FixBrokenLinks repeats these checks and writes, so `nibs
// check --fix` deleted the entry from the file on disk.
//
// It also decides self versus broken: under the bare lookup a nib naming
// itself by short id was reported broken rather than self-referential.
//
// A short spelling reaches the store by hand-editing the file; the createNib
// and setParent/addBlockedBy resolvers normalize an id before storing it.
func TestCheckAllLinksInMapIDSpelling(t *testing.T) {
	// The subject nib. Its own id is spelled in full, as a loaded nib's ID
	// always is — it is derived from the filename.
	const subjectID = "nibs-dep"

	tests := []struct {
		name      string
		parent    string
		milestone string
		blockedBy []string
		prefix    string
		// Target of each expected broken link, and LinkType of each expected
		// self link. Both empty means the links are sound.
		wantBroken []string
		wantSelf   []string
	}{
		{name: "full parent", parent: "nibs-tgt", prefix: "nibs-"},
		{name: "short parent", parent: "tgt", prefix: "nibs-"},
		{name: "full milestone", milestone: "nibs-ms", prefix: "nibs-"},
		{name: "short milestone", milestone: "ms", prefix: "nibs-"},
		{name: "full blocked_by", blockedBy: []string{"nibs-tgt"}, prefix: "nibs-"},
		{name: "short blocked_by", blockedBy: []string{"tgt"}, prefix: "nibs-"},
		{
			name: "both spellings of the same blocker resolve",
			// Two entries naming one nib: neither is broken. They are not
			// deduplicated — this asserts only that no issue is reported.
			blockedBy: []string{"tgt", "nibs-tgt"},
			prefix:    "nibs-",
		},

		// Self-references. Short and full spellings of the subject's own id
		// both name the subject, so both are self links.
		{name: "full parent naming self", parent: subjectID, prefix: "nibs-", wantSelf: []string{"parent"}},
		{name: "short parent naming self", parent: "dep", prefix: "nibs-", wantSelf: []string{"parent"}},
		{name: "full milestone naming self", milestone: subjectID, prefix: "nibs-", wantSelf: []string{"milestone"}},
		{name: "short milestone naming self", milestone: "dep", prefix: "nibs-", wantSelf: []string{"milestone"}},
		{name: "full blocked_by naming self", blockedBy: []string{subjectID}, prefix: "nibs-", wantSelf: []string{"blocked_by"}},
		{name: "short blocked_by naming self", blockedBy: []string{"dep"}, prefix: "nibs-", wantSelf: []string{"blocked_by"}},

		// Genuinely broken links: no nib answers to the target under either
		// spelling. These must survive the change, or `--fix` stops fixing.
		{name: "parent naming no nib", parent: "nope", prefix: "nibs-", wantBroken: []string{"nope"}},
		{name: "parent naming no nib, spelled in full", parent: "nibs-nope", prefix: "nibs-", wantBroken: []string{"nibs-nope"}},
		{name: "milestone naming no nib", milestone: "nope", prefix: "nibs-", wantBroken: []string{"nope"}},
		{name: "milestone naming no nib, spelled in full", milestone: "nibs-nope", prefix: "nibs-", wantBroken: []string{"nibs-nope"}},
		{name: "blocked_by naming no nib", blockedBy: []string{"nope"}, prefix: "nibs-", wantBroken: []string{"nope"}},
		{name: "blocked_by naming no nib, spelled in full", blockedBy: []string{"nibs-nope"}, prefix: "nibs-", wantBroken: []string{"nibs-nope"}},

		// With no configured prefix there is nothing to prepend, so a short
		// target names no nib — the same answer findActiveBlockersInMap gives.
		{name: "short parent with no configured prefix", parent: "tgt", wantBroken: []string{"tgt"}},
		{name: "short milestone with no configured prefix", milestone: "tgt", wantBroken: []string{"tgt"}},
		{name: "short blocked_by with no configured prefix", blockedBy: []string{"tgt"}, wantBroken: []string{"tgt"}},
		// Nothing to prepend, so the subject's short id does not name the
		// subject either: broken, not self.
		{name: "short blocked_by naming self with no configured prefix", blockedBy: []string{"dep"}, wantBroken: []string{"dep"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibs := map[string]*nib.Nib{
				// The target is an epic so the parent cases stay hierarchy-legal
				// for the type-less (default task) subject: this test pins id
				// resolution, and a hierarchy finding would muddy the counts.
				"nibs-tgt": {ID: "nibs-tgt", Status: "in-progress", Type: "epic"},
				// The milestone rows that must RESOLVE need a target of their
				// own, for the same reason: a milestone is never a legal
				// parent and an epic is never a legal assignment target, so no
				// single nib is sound on both axes.
				"nibs-ms": {ID: "nibs-ms", Status: "todo", Type: "milestone"},
				subjectID: {ID: subjectID, Status: "todo", Parent: tt.parent, Milestone: tt.milestone, BlockedBy: tt.blockedBy},
			}

			result := CheckAllLinksInMap(nibs, "", tt.prefix)

			var gotBroken []string
			for _, bl := range result.BrokenLinks {
				gotBroken = append(gotBroken, bl.Target)
			}
			var gotSelf []string
			for _, sl := range result.SelfLinks {
				gotSelf = append(gotSelf, sl.LinkType)
			}
			slices.Sort(gotBroken)
			slices.Sort(gotSelf)
			wantBroken := slices.Clone(tt.wantBroken)
			wantSelf := slices.Clone(tt.wantSelf)
			slices.Sort(wantBroken)
			slices.Sort(wantSelf)

			if !slices.Equal(gotBroken, wantBroken) {
				t.Errorf("broken link targets = %v, want %v (parent %q, milestone %q, blocked_by %v, prefix %q)",
					gotBroken, wantBroken, tt.parent, tt.milestone, tt.blockedBy, tt.prefix)
			}
			if !slices.Equal(gotSelf, wantSelf) {
				t.Errorf("self link types = %v, want %v (parent %q, milestone %q, blocked_by %v, prefix %q)",
					gotSelf, wantSelf, tt.parent, tt.milestone, tt.blockedBy, tt.prefix)
			}
			// Nothing else may be reported: catches a cycle or document issue
			// invented by the resolution change.
			if want := len(wantBroken) + len(wantSelf); result.TotalIssues() != want {
				t.Errorf("TotalIssues() = %d, want %d; cycles=%v documents=%v",
					result.TotalIssues(), want, result.Cycles, result.BrokenDocuments)
			}
		})
	}
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

// TestCheckAllLinksInMapFlagsNearMissKeys pins the near-miss front-matter key
// finding: an Extra key that is a misspelling of a modeled key (dash, case,
// stray underscores — see nib.ModeledKeyResembling) is reported with the file,
// the key as spelled, and the modeled key it resembles, while a legitimately
// foreign key stays unflagged. The findings arrive sorted by nib id and then by
// key, so the report cannot shuffle run to run, and they count as issues.
func TestCheckAllLinksInMapFlagsNearMissKeys(t *testing.T) {
	extra := func(keys ...string) map[string]yaml.Node {
		m := make(map[string]yaml.Node, len(keys))
		for _, k := range keys {
			m[k] = yaml.Node{Kind: yaml.ScalarNode, Value: "v"}
		}
		return m
	}
	nibs := map[string]*nib.Nib{
		// Two near-miss keys on one nib, ids deliberately out of map-literal
		// order relative to the others, to exercise the sorting.
		"chk-b2": {ID: "chk-b2", Status: "todo", Path: "data/chk-b2--case.md", Extra: extra("Milestone")},
		"chk-a1": {ID: "chk-a1", Status: "todo", Path: "data/chk-a1--dash.md", Extra: extra("milestone-order", "Area")},
		"chk-c3": {ID: "chk-c3", Status: "todo", Path: "data/chk-c3--foreign.md", Extra: extra("assignee")},
		"chk-d4": {ID: "chk-d4", Status: "todo", Path: "data/chk-d4--plain.md"},
	}

	result := CheckAllLinksInMap(nibs, "", "chk-")

	want := []NearMissKey{
		{NibID: "chk-a1", Path: "data/chk-a1--dash.md", Key: "Area", Modeled: "area"},
		{NibID: "chk-a1", Path: "data/chk-a1--dash.md", Key: "milestone-order", Modeled: "milestone_order"},
		{NibID: "chk-b2", Path: "data/chk-b2--case.md", Key: "Milestone", Modeled: "milestone"},
	}
	if !reflect.DeepEqual(result.NearMissKeys, want) {
		t.Errorf("NearMissKeys = %+v, want %+v", result.NearMissKeys, want)
	}
	if result.TotalIssues() != len(want) {
		t.Errorf("TotalIssues() = %d, want %d (the near-miss findings must count)", result.TotalIssues(), len(want))
	}
	if result.NearMissIssues() != len(want) {
		t.Errorf("NearMissIssues() = %d, want %d", result.NearMissIssues(), len(want))
	}
}

// TestCheckAllLinksInMapFlagsInvalidAxes pins the axis-rule finding: a
// milestone-typed nib carrying a `milestone:` or `area:` value is reported with
// the file and nibtypes.ValidateAxes' reason, while the same values on a work
// type (and a clean milestone) stay unflagged. The findings arrive sorted by
// nib id and count as issues. Without this finding the write-side strictness
// makes a hand-edited offender un-updatable through nibs with no diagnostic
// naming the file.
func TestCheckAllLinksInMapFlagsInvalidAxes(t *testing.T) {
	nibs := map[string]*nib.Nib{
		// Ids deliberately out of map-literal order to exercise the sorting.
		"chk-b2": {ID: "chk-b2", Status: "todo", Type: "milestone", Path: "data/chk-b2--assigned.md", Milestone: "chk-d4"},
		"chk-a1": {ID: "chk-a1", Status: "todo", Type: "milestone", Path: "data/chk-a1--located.md", Area: "web/ui"},
		"chk-c3": {ID: "chk-c3", Status: "todo", Type: "task", Path: "data/chk-c3--work.md", Milestone: "chk-d4", Area: "web/ui"},
		"chk-d4": {ID: "chk-d4", Status: "todo", Type: "milestone", Path: "data/chk-d4--clean.md"},
		// Both axes at once: the escape has to drop both keys in one command,
		// because clearing either alone leaves the write refused by the other.
		"chk-e5": {ID: "chk-e5", Status: "todo", Type: "milestone", Path: "data/chk-e5--both.md", Milestone: "chk-d4", Area: "web/ui"},
	}

	result := CheckAllLinksInMap(nibs, "", "chk-")

	// Axes carries every key the escape has to drop, which for these two is the
	// one Reason speaks for.
	want := []InvalidAxis{
		{NibID: "chk-a1", Path: "data/chk-a1--located.md", Reason: "a milestone cannot have an area", Axes: []string{"area"}},
		{NibID: "chk-b2", Path: "data/chk-b2--assigned.md", Reason: "a milestone cannot be assigned to a milestone", Axes: []string{"milestone"}},
		{NibID: "chk-e5", Path: "data/chk-e5--both.md", Reason: "a milestone cannot be assigned to a milestone", Axes: []string{"milestone", "area"}},
	}
	if !reflect.DeepEqual(result.InvalidAxes, want) {
		t.Errorf("InvalidAxes = %+v, want %+v", result.InvalidAxes, want)
	}
	if result.TotalIssues() != len(want) {
		t.Errorf("TotalIssues() = %d, want %d (the axis findings must count)", result.TotalIssues(), len(want))
	}
	if result.AxisIssues() != len(want) {
		t.Errorf("AxisIssues() = %d, want %d", result.AxisIssues(), len(want))
	}
	// The one command every surface prescribes, assembled from Axes.
	if got := ClearAxesCommand("chk-e5", result.InvalidAxes[2].Axes); got != "nibs set chk-e5 --clear milestone --clear area" {
		t.Errorf("ClearAxesCommand = %q, want both keys dropped by one command", got)
	}
	if got := AxisKeysNoun(result.InvalidAxes[2].Axes); got != "both axis keys" {
		t.Errorf("AxisKeysNoun = %q, want it to match what the command clears", got)
	}
}

// TestCheckAllLinksInMapNearMissCleanStore is the false-positive guard: a store
// whose Extra keys are all foreign (or absent) reports no near-miss findings.
func TestCheckAllLinksInMapNearMissCleanStore(t *testing.T) {
	nibs := map[string]*nib.Nib{
		"chk-a1": {ID: "chk-a1", Status: "todo", Path: "data/chk-a1--x.md",
			Extra: map[string]yaml.Node{"assignee": {Kind: yaml.ScalarNode, Value: "someone"}}},
		"chk-b2": {ID: "chk-b2", Status: "todo", Path: "data/chk-b2--y.md"},
	}
	result := CheckAllLinksInMap(nibs, "", "chk-")
	if len(result.NearMissKeys) != 0 {
		t.Errorf("NearMissKeys = %+v, want none for foreign keys", result.NearMissKeys)
	}
	if result.TotalIssues() != 0 {
		t.Errorf("TotalIssues() = %d, want 0", result.TotalIssues())
	}
}

// orderKeys renders each finding as a NUL-joined key, so comparing two of these
// strings compares their fields in order — the same answer a field-by-field
// comparison gives. Nothing validates a document path or a broken-link target,
// so either can hold a NUL and defeat that; the fixture below uses none.
func orderKeys[T any](items []T, fields func(T) []string) []string {
	keys := make([]string, len(items))
	for i, item := range items {
		keys[i] = strings.Join(fields(item), "\x00")
	}
	return keys
}

// TestCheckAllLinksInMapFindingOrderIsStable pins the report order of the three
// collections the map walk fills: broken links, self links and broken
// documents. Go randomizes map iteration per call, so an order inherited from
// that walk differs run to run and `nibs check --json` answers one unchanged
// store several different ways.
//
// The sort keys have to be compound. One nib can hold a broken parent, a broken
// milestone and several broken blockers at once, so a NibID-only key leaves
// those entries tied and nothing pins their order. Every fixture nib below
// appends its own entries in DESCENDING key order, so a key too weak to be a
// total order leaves a collection out of ascending order.
//
// That the ascending check catches it on the FIRST assertion depends on the
// fixture staying small: sort.Slice is not stable in general, and at these
// sizes it happens to keep arrival order. Grow a collection past a dozen
// entries and a weak key can slip past the ascending check onto the repeat
// loop below, which alone does not reliably catch one.
func TestCheckAllLinksInMapFindingOrderIsStable(t *testing.T) {
	// Nothing is created under projectRoot, so every document link below is
	// broken.
	projectRoot := t.TempDir()

	nibs := map[string]*nib.Nib{
		// Four broken links and two broken documents on a single nib: the ties
		// a NibID-only sort cannot break.
		"nibs-a1": {ID: "nibs-a1", Status: "todo", Parent: "ghost-p", Milestone: "ghost-m",
			BlockedBy: []string{"ghost-b2", "ghost-b1"},
			Documents: []string{"docs/a1-two.md", "docs/a1-one.md"}},
		"nibs-a2": {ID: "nibs-a2", Status: "todo", Parent: "ghost-q",
			BlockedBy: []string{"ghost-z"},
			Documents: []string{"docs/a2.md"}},
		"nibs-a3": {ID: "nibs-a3", Status: "todo", BlockedBy: []string{"ghost-y2", "ghost-y1"}},
		// Self links, spelled both in full and short, so resolution decides
		// them the way it decides a broken target's spelling.
		"nibs-s1": {ID: "nibs-s1", Status: "todo", Parent: "nibs-s1", Milestone: "s1",
			BlockedBy: []string{"nibs-s1"}},
		"nibs-s2": {ID: "nibs-s2", Status: "todo", Parent: "s2", Milestone: "nibs-s2"},
		"nibs-s3": {ID: "nibs-s3", Status: "todo", BlockedBy: []string{"nibs-s3"}},
		"nibs-s4": {ID: "nibs-s4", Status: "todo", Parent: "s4"},
		"nibs-b1": {ID: "nibs-b1", Status: "todo",
			Documents: []string{"docs/b1-c.md", "docs/b1-b.md", "docs/b1-a.md"}},
		"nibs-b2": {ID: "nibs-b2", Status: "todo",
			Documents: []string{"docs/b2-b.md", "docs/b2-a.md"}},
	}

	keysOf := func(r *LinkCheckResult) (broken, self, docs []string) {
		return orderKeys(r.BrokenLinks, func(bl BrokenLink) []string {
				return []string{bl.NibID, bl.LinkType, bl.Target}
			}),
			orderKeys(r.SelfLinks, func(sl SelfLink) []string {
				return []string{sl.NibID, sl.LinkType}
			}),
			orderKeys(r.BrokenDocuments, func(bd BrokenDocument) []string {
				return []string{bd.NibID, bd.Path}
			})
	}

	first := CheckAllLinksInMap(nibs, projectRoot, "nibs-")
	firstBroken, firstSelf, firstDocs := keysOf(first)

	for _, c := range []struct {
		name string
		keys []string
		want int
	}{
		{"broken_links", firstBroken, 8},
		{"self_links", firstSelf, 7},
		{"broken_documents", firstDocs, 8},
	} {
		if len(c.keys) != c.want {
			t.Fatalf("%s: %d findings, want %d: %q", c.name, len(c.keys), c.want, c.keys)
		}
		// Every fixture entry is distinct, so a total order renders strictly
		// ascending.
		for i := 1; i < len(c.keys); i++ {
			if c.keys[i-1] >= c.keys[i] {
				t.Errorf("%s: not ascending at index %d: %q then %q\nfull: %q",
					c.name, i, c.keys[i-1], c.keys[i], c.keys)
			}
		}
		// The fixture only tells a compound key from a NibID-only one while
		// some nib contributes more than one entry here.
		perNib := map[string]int{}
		most := 0
		for _, k := range c.keys {
			id := strings.SplitN(k, "\x00", 2)[0]
			perNib[id]++
			most = max(most, perNib[id])
		}
		if most < 2 {
			t.Errorf("%s: no nib contributes two findings, so this fixture cannot tell a compound key from a NibID-only one", c.name)
		}
	}

	// Repeated over enough scans that a surviving map-order dependence shows up
	// rather than hiding behind one lucky iteration.
	for i := range 300 {
		got := CheckAllLinksInMap(nibs, projectRoot, "nibs-")
		gotBroken, gotSelf, gotDocs := keysOf(got)
		if !slices.Equal(gotBroken, firstBroken) {
			t.Fatalf("scan %d: broken link order drifted\n got: %q\nwant: %q", i, gotBroken, firstBroken)
		}
		if !slices.Equal(gotSelf, firstSelf) {
			t.Fatalf("scan %d: self link order drifted\n got: %q\nwant: %q", i, gotSelf, firstSelf)
		}
		if !slices.Equal(gotDocs, firstDocs) {
			t.Fatalf("scan %d: broken document order drifted\n got: %q\nwant: %q", i, gotDocs, firstDocs)
		}
	}
}

// TestCycleReportRotationAndOrderAreStable pins two of the three things the map
// walk leaves unstable in the cycle report, both of which reach
// `nibs check --json`: the ROTATION each cycle is written from (the DFS records
// the node it entered at) and the ORDER the cycles arrive in (whichever the walk
// reached first).
//
// The third is out of reach here and stays unfixed: `visited` spans the whole
// walk, so WHICH cycles are found at all still varies with the entry node. See
// FindCyclesInMap's own note.
//
// The two fixture cycles have five and four members, so a report echoing the
// entry node reproduces the canonical answer by luck alone — 201 of 20000 scans
// (1.0%) matched `want` before the rotation and sort landed. Asserting the
// canonical answer as a literal is what makes that a certainty; a check that
// only compared repeated runs to each other would accept an entry-node report
// whenever the entry happened not to vary.
func TestCycleReportRotationAndOrderAreStable(t *testing.T) {
	// a1 → a4 → a2 → a5 → a3 → a1 and b1 → b3 → b4 → b2 → b1.
	newFixture := func() map[string]*nib.Nib {
		return map[string]*nib.Nib{
			"a3": {ID: "a3", Status: "todo", BlockedBy: []string{"a1"}},
			"a1": {ID: "a1", Status: "todo", BlockedBy: []string{"a4"}},
			"a4": {ID: "a4", Status: "todo", BlockedBy: []string{"a2"}},
			"a2": {ID: "a2", Status: "todo", BlockedBy: []string{"a5"}},
			"a5": {ID: "a5", Status: "todo", BlockedBy: []string{"a3"}},

			"b3": {ID: "b3", Status: "todo", BlockedBy: []string{"b4"}},
			"b4": {ID: "b4", Status: "todo", BlockedBy: []string{"b2"}},
			"b2": {ID: "b2", Status: "todo", BlockedBy: []string{"b1"}},
			"b1": {ID: "b1", Status: "todo", BlockedBy: []string{"b3"}},
		}
	}

	// A canonical Path starts at the cycle's smallest id and still CLOSES BACK
	// on it, so `nibs check` renders the loop as "a1 → a4 → … → a3 → a1".
	want := []Cycle{
		{LinkType: "blocked_by", Path: []string{"a1", "a4", "a2", "a5", "a3", "a1"}},
		{LinkType: "blocked_by", Path: []string{"b1", "b3", "b4", "b2", "b1"}},
	}

	assertCanonical := func(t *testing.T, scan int, got []Cycle) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("scan %d: cycle report is not canonical\n got: %v\nwant: %v", scan, got, want)
		}
		// Restate the rule the literal encodes, so a later fixture edit cannot
		// quietly move `want` off the canonical form.
		for _, c := range got {
			body := c.Path[:len(c.Path)-1]
			if c.Path[len(c.Path)-1] != c.Path[0] {
				t.Fatalf("scan %d: path does not close back on its start: %v", scan, c.Path)
			}
			if c.Path[0] != slices.Min(body) {
				t.Fatalf("scan %d: path starts at %q, not the cycle's smallest id %q: %v",
					scan, c.Path[0], slices.Min(body), c.Path)
			}
		}
		for j := 1; j < len(got); j++ {
			if slices.Compare(got[j-1].Path, got[j].Path) >= 0 {
				t.Fatalf("scan %d: cycles not in ascending path order at index %d: %v then %v",
					scan, j, got[j-1].Path, got[j].Path)
			}
		}
	}

	for i := range 200 {
		assertCanonical(t, i, FindCyclesInMap(newFixture(), "blocked_by"))
	}

	// The same two halves through the surface `nibs check --json` serializes.
	// Nothing is created under projectRoot, but no fixture nib links a
	// document, so it stays unused.
	projectRoot := t.TempDir()
	for i := range 200 {
		assertCanonical(t, i, CheckAllLinksInMap(newFixture(), projectRoot, "").Cycles)
	}
}
