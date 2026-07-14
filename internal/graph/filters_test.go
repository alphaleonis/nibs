package graph

import (
	"context"
	"reflect"
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

	t.Run("returns echoed id and false for unknown target (matches NibReader.NormalizeID)", func(t *testing.T) {
		fullID, ok := resolveFilterID(reader, "nonexistent")
		if ok {
			t.Errorf("expected ok=false for unknown target, got ok=true (fullID=%q)", fullID)
		}
		// resolveFilterID is a pass-through to NormalizeID; on miss, NormalizeID
		// echoes the input id (Core convention). Callers gate on ok, not on the
		// string, so the echoed value is informational only.
		if fullID != "nonexistent" {
			t.Errorf("fullID = %q, want echoed input %q on miss", fullID, "nonexistent")
		}
	})
}

// TestApplyFilterBlockedByIDShortForm is the tracer bullet: a filter with
// a short `BlockedByID` must match nibs whose `blocked_by` contains the
// full (prefixed) ID. A short BlockedByID must be normalized before matching —
// passing it raw to filterBySliceField makes short IDs silently match nothing.
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
	got := ApplyFilter(context.Background(), reader.allNibs, filter, reader, blocking)

	if len(got) != 1 {
		t.Fatalf("got %d nibs, want 1 (nibs-blocked)", len(got))
	}
	if got[0].ID != "nibs-blocked" {
		t.Errorf("got %q, want %q", got[0].ID, "nibs-blocked")
	}
}

// TestApplyFilterUnknownIDReturnsNil verifies the "unknown target -> nil"
// contract across all four single-ID filter branches. All four route through
// resolveFilterID and short-circuit to nil on miss — the trap is a branch that
// passes a raw ID through and returns empty instead of nil.
//
// The table pairs a negative case (unknown → nil) with a positive control
// (known → non-nil with a specific ID in the result). Without the positive
// rows, a regression that short-circuited to nil unconditionally would
// pass the unknown-only suite silently.
func TestApplyFilterIDBranchesKnownAndUnknown(t *testing.T) {
	// Fixture: four nibs wired so every *ID filter has a non-trivial
	// positive case.
	//   - nibs-a: target of blocking queries (blocked_by: [nibs-b]) and
	//     source of an outbound mention to nibs-c
	//   - nibs-b: blocker of nibs-a; also blocks via blocked_by
	//   - nibs-c: mentioned by nibs-a (outbound set)
	//   - nibs-d: mentions nibs-a (inbound mentioner)
	nibA := &nib.Nib{ID: "nibs-a", Title: "A", BlockedBy: []string{"nibs-b"}}
	nibB := &nib.Nib{ID: "nibs-b", Title: "B", BlockedBy: []string{"nibs-a"}}
	nibC := &nib.Nib{ID: "nibs-c", Title: "C"}
	nibD := &nib.Nib{ID: "nibs-d", Title: "D"}

	reader := &stubReader{
		nibs: map[string]*nib.Nib{
			"nibs-a": nibA, "nibs-b": nibB, "nibs-c": nibC, "nibs-d": nibD,
		},
		allNibs: []*nib.Nib{nibA, nibB, nibC, nibD},
		prefix:  "nibs-",
		// MentionsID filter: "nibs that mention target". Seed so nibs-d
		// shows up as an inbound mentioner of nibs-a.
		mentionsIn: map[string][]*nib.Nib{"nibs-a": {nibD}},
		// MentionedByID filter: "nibs the source mentions". Seed so nibs-c
		// shows up in nibs-a's outbound set.
		mentionsOut: map[string][]*nib.Nib{"nibs-a": {nibC}},
	}
	blocking := &stubBlockingChecker{}

	tests := []struct {
		name    string
		filter  *model.NibFilter
		wantNil bool     // true → short-circuited to nil
		wantIDs []string // expected nib IDs in the result (when wantNil=false)
	}{
		// BlockingID — "nibs blocking the target"; target's blocked_by lists them.
		{"BlockingID known — returns target's blockers", &model.NibFilter{BlockingID: strPtr("a")}, false, []string{"nibs-b"}},
		{"BlockingID unknown — short-circuits to nil", &model.NibFilter{BlockingID: strPtr("nonexistent")}, true, nil},

		// BlockedByID — "nibs whose blocked_by contains target".
		{"BlockedByID known — returns nibs blocked by target", &model.NibFilter{BlockedByID: strPtr("a")}, false, []string{"nibs-b"}},
		{"BlockedByID unknown — short-circuits to nil", &model.NibFilter{BlockedByID: strPtr("nonexistent")}, true, nil},

		// MentionsID — "nibs that mention the target in their body".
		{"MentionsID known — returns inbound mentioners", &model.NibFilter{MentionsID: strPtr("a")}, false, []string{"nibs-d"}},
		{"MentionsID unknown — short-circuits to nil", &model.NibFilter{MentionsID: strPtr("nonexistent")}, true, nil},

		// MentionedByID — "nibs mentioned in the source's body".
		{"MentionedByID known — returns source's outbound mentions", &model.NibFilter{MentionedByID: strPtr("a")}, false, []string{"nibs-c"}},
		{"MentionedByID unknown — short-circuits to nil", &model.NibFilter{MentionedByID: strPtr("nonexistent")}, true, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ApplyFilter(context.Background(), reader.allNibs, tt.filter, reader, blocking)
			if tt.wantNil {
				if got != nil {
					t.Errorf("got %d nibs, want nil (unknown target short-circuit)", len(got))
				}
				return
			}
			if got == nil {
				t.Fatalf("got nil, want non-nil result with %v", tt.wantIDs)
			}
			gotIDs := make([]string, 0, len(got))
			for _, b := range got {
				gotIDs = append(gotIDs, b.ID)
			}
			sort.Strings(gotIDs)
			want := append([]string(nil), tt.wantIDs...)
			sort.Strings(want)
			if !reflect.DeepEqual(gotIDs, want) {
				t.Errorf("got IDs %v, want %v", gotIDs, want)
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

// TestApplyFilterDefaultAwarePriorityAndType is the direct coverage for
// ApplyFilter's default-aware Type/Priority filtering (the EffectiveType()/
// EffectivePriority() routing). A default-omitting nib (empty Priority/Type) must filter
// as though the "normal"/"task" presentation defaults were on disk: including it
// under Priority=["normal"] / Type=["task"], and excluding it under the symmetric
// ExcludePriority / ExcludeType. Each exclude row keeps a non-default control nib
// so a regression that dropped everything would not pass silently.
func TestApplyFilterDefaultAwarePriorityAndType(t *testing.T) {
	// defaulted omits both priority: and type: (empty fields); explicit carries
	// non-default values so each case has a surviving control.
	defaulted := &nib.Nib{ID: "nibs-defaulted", Title: "Defaulted"}
	explicit := &nib.Nib{ID: "nibs-explicit", Title: "Explicit", Priority: "high", Type: "bug"}

	reader := &stubReader{
		nibs: map[string]*nib.Nib{
			"nibs-defaulted": defaulted,
			"nibs-explicit":  explicit,
		},
		allNibs: []*nib.Nib{defaulted, explicit},
		prefix:  "nibs-",
	}
	blocking := &stubBlockingChecker{}

	tests := []struct {
		name    string
		filter  *model.NibFilter
		wantIDs []string
	}{
		{"Priority normal includes default-omitting nib", &model.NibFilter{Priority: []string{"normal"}}, []string{"nibs-defaulted"}},
		{"ExcludePriority normal excludes default-omitting nib", &model.NibFilter{ExcludePriority: []string{"normal"}}, []string{"nibs-explicit"}},
		{"Type task includes default-omitting nib", &model.NibFilter{Type: []string{"task"}}, []string{"nibs-defaulted"}},
		{"ExcludeType task excludes default-omitting nib", &model.NibFilter{ExcludeType: []string{"task"}}, []string{"nibs-explicit"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ApplyFilter(context.Background(), reader.allNibs, tt.filter, reader, blocking)
			gotIDs := make([]string, 0, len(got))
			for _, b := range got {
				gotIDs = append(gotIDs, b.ID)
			}
			sort.Strings(gotIDs)
			want := append([]string(nil), tt.wantIDs...)
			sort.Strings(want)
			if !reflect.DeepEqual(gotIDs, want) {
				t.Errorf("got IDs %v, want %v", gotIDs, want)
			}
		})
	}
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
