package graph

import (
	"sort"
	"testing"

	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
)

// TestResolveFilterID exercises the shared helper used by all four
// filter.*ID branches in ApplyFilter. It must return the full ID for a
// known short form and ("", false) for an unknown target.
func TestResolveFilterID(t *testing.T) {
	target := &nib.Nib{ID: "nibs-target", Title: "Target"}
	reader := &stubReader{
		nibs:    map[string]*nib.Nib{"nibs-target": target},
		allNibs: []*nib.Nib{target},
		prefix:  "nibs-",
	}

	t.Run("returns full ID for known short form", func(t *testing.T) {
		fullID, ok := resolveFilterID(reader, "target")
		if !ok {
			t.Fatalf("expected ok=true for known short form")
		}
		if fullID != "nibs-target" {
			t.Errorf("fullID = %q, want %q", fullID, "nibs-target")
		}
	})

	t.Run("returns empty and false for unknown target", func(t *testing.T) {
		fullID, ok := resolveFilterID(reader, "nonexistent")
		if ok {
			t.Errorf("expected ok=false for unknown target, got ok=true (fullID=%q)", fullID)
		}
		if fullID != "" {
			t.Errorf("fullID = %q, want empty string on miss", fullID)
		}
	})
}

// TestApplyFilterBlockedByIDShortForm is the tracer bullet: a filter with
// a short `BlockedByID` must match nibs whose `blocked_by` contains the
// full (prefixed) ID. Prior to the fix, filter.BlockedByID was passed raw
// to filterBySliceField with no normalisation, so short IDs silently
// matched nothing.
func TestApplyFilterBlockedByIDShortForm(t *testing.T) {
	target := &nib.Nib{ID: "nibs-target", Title: "Target"}
	blocked := &nib.Nib{ID: "nibs-blocked", Title: "Blocked", BlockedBy: []string{"nibs-target"}}
	unrelated := &nib.Nib{ID: "nibs-other", Title: "Other"}

	reader := &stubReader{
		nibs: map[string]*nib.Nib{
			"nibs-target":  target,
			"nibs-blocked": blocked,
			"nibs-other":   unrelated,
		},
		allNibs: []*nib.Nib{target, blocked, unrelated},
		prefix:  "nibs-",
	}
	blocking := &stubBlockingChecker{}

	filter := &model.NibFilter{BlockedByID: strPtr("target")}
	got := ApplyFilter(reader.allNibs, filter, reader, blocking)

	if len(got) != 1 {
		t.Fatalf("got %d nibs, want 1 (nibs-blocked)", len(got))
	}
	if got[0].ID != "nibs-blocked" {
		t.Errorf("got %q, want %q", got[0].ID, "nibs-blocked")
	}
}

// TestApplyFilterUnknownIDReturnsNil verifies the "unknown target -> nil"
// contract across all four single-ID filter branches. Previously these
// branches disagreed: BlockingID via reader.Get returned nil, Mentions(ed)ByID
// via NormalizeID returned nil, but BlockedByID silently passed the raw ID
// through and returned empty. All four now route through resolveFilterID
// and short-circuit to nil on miss.
func TestApplyFilterUnknownIDReturnsNil(t *testing.T) {
	existing := &nib.Nib{ID: "nibs-a", Title: "A"}
	reader := &stubReader{
		nibs:    map[string]*nib.Nib{"nibs-a": existing},
		allNibs: []*nib.Nib{existing},
		prefix:  "nibs-",
	}
	blocking := &stubBlockingChecker{}

	tests := []struct {
		name   string
		filter *model.NibFilter
	}{
		{"BlockedByID unknown", &model.NibFilter{BlockedByID: strPtr("nonexistent")}},
		{"BlockingID unknown", &model.NibFilter{BlockingID: strPtr("nonexistent")}},
		{"MentionsID unknown", &model.NibFilter{MentionsID: strPtr("nonexistent")}},
		{"MentionedByID unknown", &model.NibFilter{MentionedByID: strPtr("nonexistent")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ApplyFilter(reader.allNibs, tt.filter, reader, blocking)
			if got != nil {
				t.Errorf("got %d nibs, want nil (unknown target)", len(got))
			}
		})
	}
}

func TestFilterByPredicate(t *testing.T) {
	nibs := []*nib.Nib{
		{ID: "a", Parent: "p1"},
		{ID: "b", Parent: ""},
		{ID: "c", Parent: "p2"},
	}

	hasParent := func(b *nib.Nib) bool { return b.Parent != "" }
	bTrue := true
	bFalse := false

	tests := []struct {
		name    string
		apply   *bool
		wantLen int
		wantIDs []string
	}{
		{"nil is no-op", nil, 3, []string{"a", "b", "c"}},
		{"true keeps matching", &bTrue, 2, []string{"a", "c"}},
		{"false keeps non-matching", &bFalse, 1, []string{"b"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterByPredicate(nibs, tt.apply, hasParent)
			if len(got) != tt.wantLen {
				t.Fatalf("got %d nibs, want %d", len(got), tt.wantLen)
			}
			for i, id := range tt.wantIDs {
				if got[i].ID != id {
					t.Errorf("got[%d].ID = %q, want %q", i, got[i].ID, id)
				}
			}
		})
	}
}

func TestFilterBySliceField(t *testing.T) {
	nibs := []*nib.Nib{
		{ID: "a", Tags: []string{"frontend", "urgent"}},
		{ID: "b", Tags: []string{"backend"}},
		{ID: "c", Tags: []string{"frontend", "backend"}},
		{ID: "d", Tags: nil},
	}

	getTags := func(b *nib.Nib) []string { return b.Tags }

	t.Run("include matches any", func(t *testing.T) {
		got := filterBySliceField(nibs, []string{"frontend"}, getTags)
		if len(got) != 2 {
			t.Fatalf("got %d nibs, want 2", len(got))
		}
		if got[0].ID != "a" || got[1].ID != "c" {
			t.Errorf("got IDs %s, %s, want a, c", got[0].ID, got[1].ID)
		}
	})

	t.Run("include with multiple values OR", func(t *testing.T) {
		got := filterBySliceField(nibs, []string{"urgent", "backend"}, getTags)
		if len(got) != 3 {
			t.Fatalf("got %d nibs, want 3", len(got))
		}
		wantIDs := []string{"a", "b", "c"}
		for i, id := range wantIDs {
			if got[i].ID != id {
				t.Errorf("got[%d].ID = %q, want %q", i, got[i].ID, id)
			}
		}
	})

	t.Run("nil values is no-op", func(t *testing.T) {
		got := filterBySliceField(nibs, nil, getTags)
		if len(got) != len(nibs) {
			t.Errorf("got %d nibs, want %d (no-op)", len(got), len(nibs))
		}
	})
}

func TestExcludeBySliceField(t *testing.T) {
	nibs := []*nib.Nib{
		{ID: "a", Tags: []string{"frontend", "urgent"}},
		{ID: "b", Tags: []string{"backend"}},
		{ID: "c", Tags: nil},
	}

	getTags := func(b *nib.Nib) []string { return b.Tags }

	t.Run("excludes matching", func(t *testing.T) {
		got := excludeBySliceField(nibs, []string{"frontend"}, getTags)
		if len(got) != 2 {
			t.Fatalf("got %d nibs, want 2", len(got))
		}
		if got[0].ID != "b" || got[1].ID != "c" {
			t.Errorf("got IDs %s, %s, want b, c", got[0].ID, got[1].ID)
		}
	})

	t.Run("nil values is no-op", func(t *testing.T) {
		got := excludeBySliceField(nibs, nil, getTags)
		if len(got) != len(nibs) {
			t.Errorf("got %d nibs, want %d (no-op)", len(got), len(nibs))
		}
	})
}

func TestFilterByEstimate(t *testing.T) {
	nibs := []*nib.Nib{
		{ID: "a", Estimate: "s"},
		{ID: "b", Estimate: "m"},
		{ID: "c", Estimate: "l"},
		{ID: "d", Estimate: ""},
	}

	getEstimate := func(b *nib.Nib) string { return b.Estimate }

	t.Run("include by estimate", func(t *testing.T) {
		got := filterByField(nibs, []string{"s", "l"}, getEstimate)
		if len(got) != 2 {
			t.Fatalf("got %d nibs, want 2", len(got))
		}
		if got[0].ID != "a" || got[1].ID != "c" {
			t.Errorf("got IDs %s, %s, want a, c", got[0].ID, got[1].ID)
		}
	})

	t.Run("exclude by estimate", func(t *testing.T) {
		got := excludeByField(nibs, []string{"m"}, getEstimate)
		if len(got) != 3 {
			t.Fatalf("got %d nibs, want 3", len(got))
		}
		wantIDs := []string{"a", "c", "d"}
		for i, id := range wantIDs {
			if got[i].ID != id {
				t.Errorf("got[%d].ID = %q, want %q", i, got[i].ID, id)
			}
		}
	})

	t.Run("include empty estimate", func(t *testing.T) {
		got := filterByField(nibs, []string{""}, getEstimate)
		if len(got) != 1 || got[0].ID != "d" {
			t.Errorf("got %v, want [d]", got)
		}
	})
}

func TestFilterByFieldWithDefault(t *testing.T) {
	nibs := []*nib.Nib{
		{ID: "a", Priority: "high"},
		{ID: "b", Priority: ""},     // should be treated as "normal"
		{ID: "c", Priority: "low"},
		{ID: "d", Priority: "normal"},
	}

	getPriority := func(b *nib.Nib) string { return b.Priority }

	t.Run("matches explicit and defaulted", func(t *testing.T) {
		got := filterByFieldWithDefault(nibs, []string{"normal"}, "normal", getPriority)
		if len(got) != 2 {
			t.Fatalf("got %d nibs, want 2", len(got))
		}
		if got[0].ID != "b" || got[1].ID != "d" {
			t.Errorf("got IDs %s, %s, want b, d", got[0].ID, got[1].ID)
		}
	})

	t.Run("matches non-default values", func(t *testing.T) {
		got := filterByFieldWithDefault(nibs, []string{"high"}, "normal", getPriority)
		if len(got) != 1 || got[0].ID != "a" {
			t.Errorf("got %v, want [a]", got)
		}
	})

	t.Run("nil values is no-op", func(t *testing.T) {
		got := filterByFieldWithDefault(nibs, nil, "normal", getPriority)
		if len(got) != len(nibs) {
			t.Errorf("got %d nibs, want %d (no-op)", len(got), len(nibs))
		}
	})
}

func TestIncludeAncestors(t *testing.T) {
	// Build a hierarchy: milestone -> epic -> task
	milestone := &nib.Nib{ID: "m1", Title: "Release", Type: "milestone"}
	epic := &nib.Nib{ID: "e1", Title: "Auth", Type: "epic", Parent: "m1"}
	task := &nib.Nib{ID: "t1", Title: "Login page", Type: "task", Parent: "e1"}
	unrelated := &nib.Nib{ID: "u1", Title: "Unrelated", Type: "task"}

	reader := &stubReader{
		nibs: map[string]*nib.Nib{
			"m1": milestone,
			"e1": epic,
			"t1": task,
			"u1": unrelated,
		},
	}

	t.Run("adds missing ancestors", func(t *testing.T) {
		input := []*nib.Nib{task}
		got := includeAncestors(input, reader)

		ids := make([]string, len(got))
		for i, b := range got {
			ids[i] = b.ID
		}
		sort.Strings(ids)

		want := []string{"e1", "m1", "t1"}
		if len(ids) != len(want) {
			t.Fatalf("got %v, want %v", ids, want)
		}
		for i, id := range ids {
			if id != want[i] {
				t.Errorf("ids[%d] = %q, want %q", i, id, want[i])
			}
		}
	})

	t.Run("does not duplicate already-present ancestors", func(t *testing.T) {
		input := []*nib.Nib{epic, task}
		got := includeAncestors(input, reader)

		ids := make([]string, len(got))
		for i, b := range got {
			ids[i] = b.ID
		}
		sort.Strings(ids)

		want := []string{"e1", "m1", "t1"}
		if len(ids) != len(want) {
			t.Fatalf("got %v, want %v", ids, want)
		}
	})

	t.Run("no-op when all ancestors present", func(t *testing.T) {
		input := []*nib.Nib{milestone, epic, task}
		got := includeAncestors(input, reader)
		if len(got) != 3 {
			t.Errorf("got %d nibs, want 3 (no extras)", len(got))
		}
	})

	t.Run("no-op for root nibs", func(t *testing.T) {
		input := []*nib.Nib{unrelated}
		got := includeAncestors(input, reader)
		if len(got) != 1 {
			t.Errorf("got %d nibs, want 1", len(got))
		}
	})
}
