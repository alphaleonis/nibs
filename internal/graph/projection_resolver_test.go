package graph

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
)

// TestProjectionResolver_NibByID covers the lookup used to expand nested
// relation sub-selections: a present id yields the shared pointer, a missing id
// yields (nil, false) rather than an error.
func TestProjectionResolver_NibByID(t *testing.T) {
	resolver, core := setupTestResolver(t)
	mustCreate(t, core, &nib.Nib{ID: "aaaa", Slug: "a", Title: "A", Status: "todo"})
	pr := resolver.ProjectionResolver(context.Background())

	if b, ok := pr.NibByID("aaaa"); !ok || b == nil || b.ID != "aaaa" {
		t.Fatalf("NibByID(aaaa) = %v, %v; want the nib", b, ok)
	}
	if b, ok := pr.NibByID("missing"); ok || b != nil {
		t.Errorf("NibByID(missing) = %v, %v; want (nil, false)", b, ok)
	}
}

// TestProjectionResolver_ChildCountAndProgress pins the direct-children rollups:
// ChildCount is the number of children; Progress is the canonical
// child-completion rollup (done = completed only, total excludes children closed
// without completing, those surfaced separately). A leaf reports zeros.
func TestProjectionResolver_ChildCountAndProgress(t *testing.T) {
	resolver, core := setupTestResolver(t)
	mustCreate(t, core, &nib.Nib{ID: "par", Slug: "par", Title: "Parent", Status: "in-progress"})
	mustCreate(t, core, &nib.Nib{ID: "c1", Slug: "c1", Title: "C1", Status: "completed", Parent: "par"})
	mustCreate(t, core, &nib.Nib{ID: "c2", Slug: "c2", Title: "C2", Status: "scrapped", Parent: "par"})
	mustCreate(t, core, &nib.Nib{ID: "c3", Slug: "c3", Title: "C3", Status: "todo", Parent: "par"})
	pr := resolver.ProjectionResolver(context.Background())

	// ChildCount counts every parent link, including the scrapped child.
	if got := pr.ChildCount("par"); got != 3 {
		t.Errorf("ChildCount(par) = %d, want 3", got)
	}
	if got := pr.ChildCount("c1"); got != 0 {
		t.Errorf("ChildCount(c1) = %d, want 0 (leaf)", got)
	}

	// Canonical rollup: 1 completed of 2 non-scrapped children -> 50%;
	// the scrapped child is excluded from Total/Done and reported separately.
	want := ProgressRollup{Total: 2, Done: 1, Percent: 50, Scrapped: 1}
	if got := pr.Progress("par"); !reflect.DeepEqual(got, want) {
		t.Errorf("Progress(par) = %#v, want %#v", got, want)
	}
	// Leaf: no children -> all zeros, no divide-by-zero.
	wantLeaf := ProgressRollup{Total: 0, Done: 0, Percent: 0, Scrapped: 0}
	if got := pr.Progress("c3"); !reflect.DeepEqual(got, wantLeaf) {
		t.Errorf("Progress(c3) = %#v, want %#v", got, wantLeaf)
	}
}

// TestProjectionRollupAxisSplit pins membership ledger delta (e)'s step-1
// half: the two rollups answer from DIFFERENT axes. ChildCount is the
// STRUCTURAL parent axis — every nib naming the parent, a milestone-typed
// child included — while Progress rolls over the MEMBERS, where a
// milestone-typed nib never counts (it is a container of its own). The split
// is what lets step 2 move membership to `milestone:` assignment without
// childCount silently changing meaning.
func TestProjectionRollupAxisSplit(t *testing.T) {
	resolver, core := setupTestResolver(t)
	mustCreate(t, core, &nib.Nib{ID: "par", Slug: "par", Title: "Parent", Status: "in-progress"})
	// A milestone-typed child is illegal data (milestones cannot have
	// parents), created below the validating resolver on purpose: the axes
	// only diverge on it, so it is the one probe that can tell them apart.
	mustCreate(t, core, &nib.Nib{ID: "ms", Slug: "ms", Title: "Nested milestone", Type: "milestone", Status: "todo", Parent: "par"})
	mustCreate(t, core, &nib.Nib{ID: "c1", Slug: "c1", Title: "C1", Status: "completed", Parent: "par"})
	pr := resolver.ProjectionResolver(context.Background())

	if got := pr.ChildCount("par"); got != 2 {
		t.Errorf("ChildCount(par) = %d, want 2 — the structural axis counts the milestone-typed child", got)
	}
	want := ProgressRollup{Total: 1, Done: 1, Percent: 100}
	if got := pr.Progress("par"); !reflect.DeepEqual(got, want) {
		t.Errorf("Progress(par) = %#v, want %#v — the member axis excludes the milestone-typed child", got, want)
	}
}

// TestComputeProgress pins the canonical progress rule directly, independent of
// the store: done = completed only, total excludes scrapped children and no
// others, percent rounds, and the two closed-but-not-completed statuses are
// disclosed by their own counters. The three closed statuses get three
// treatments — completed counts as done, scrapped leaves the denominator,
// deferred stays in it undone — so each is covered separately below.
func TestComputeProgress(t *testing.T) {
	cases := []struct {
		name     string
		statuses []string
		want     ProgressRollup
	}{
		{"no children", nil, ProgressRollup{}},
		{
			"mixed with scrapped excluded",
			[]string{"completed", "scrapped", "todo"},
			ProgressRollup{Total: 2, Done: 1, Percent: 50, Scrapped: 1},
		},
		{
			"all completed",
			[]string{"completed", "completed"},
			ProgressRollup{Total: 2, Done: 2, Percent: 100},
		},
		{
			"rounds to nearest",
			[]string{"completed", "todo", "todo"}, // 1/3 = 33.33 -> 33
			ProgressRollup{Total: 3, Done: 1, Percent: 33},
		},
		{
			"only scrapped -> zero denominator",
			[]string{"scrapped", "scrapped"},
			ProgressRollup{Total: 0, Done: 0, Percent: 0, Scrapped: 2},
		},
		{
			// Deferred is closed, but the work is coming back — it is set aside
			// scope, so it counts toward total and not toward done, exactly like
			// the open statuses. Only scrapped leaves the denominator.
			"draft and deferred both count toward total but not done",
			[]string{"draft", "deferred", "in-progress", "completed"},
			ProgressRollup{Total: 4, Done: 1, Percent: 25, Deferred: 1},
		},
		{
			// A deferred child holds the parent below 100%: there is still work
			// under it, and the roadmap still renders that child. Reporting 100%
			// here would contradict a view that lists the deferred item.
			"a deferred child holds the parent below 100%",
			[]string{"completed", "completed", "completed", "deferred"},
			ProgressRollup{Total: 4, Done: 3, Percent: 75, Deferred: 1},
		},
		{
			"only deferred -> all scope outstanding, 0%",
			[]string{"deferred", "deferred"},
			ProgressRollup{Total: 2, Done: 0, Percent: 0, Deferred: 2},
		},
		{
			// Unknown statuses (a hand-edited nib with no `status:`) are
			// outstanding scope, so they cannot inflate the percentage.
			"unknown status counts toward total but not done",
			[]string{"completed", "", "bogus"},
			ProgressRollup{Total: 3, Done: 1, Percent: 33},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ComputeProgress(tc.statuses); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ComputeProgress(%v) = %#v, want %#v", tc.statuses, got, tc.want)
			}
		})
	}
}

// TestProjectionResolver_Ready pins the startability rule: a startable status
// and no active blockers. A blocker that released its dependents does not keep
// a nib un-ready (mirroring BlockedByIds). A deferred blocker is the
// discriminating case for the blocker half: it is closed, so a closed-status
// test would release it, but it has not satisfied the dependency.
//
// The status half is covered separately below, one unblocked nib per declared
// status, because it is strictly narrower than "not closed": draft and
// in-progress are open and still not ready. A predicate that only rejected
// closed statuses would pass every other case in this test.
func TestProjectionResolver_Ready(t *testing.T) {
	resolver, core := setupTestResolver(t)
	mustCreate(t, core, &nib.Nib{ID: "blkactive", Slug: "ba", Title: "BA", Status: "todo"})
	mustCreate(t, core, &nib.Nib{ID: "blkdone", Slug: "bd", Title: "BD", Status: "completed"})
	mustCreate(t, core, &nib.Nib{ID: "blkdefer", Slug: "bp", Title: "BP", Status: "deferred"})
	mustCreate(t, core, &nib.Nib{ID: "free", Slug: "f", Title: "Free", Status: "todo"})
	mustCreate(t, core, &nib.Nib{ID: "blocked", Slug: "b", Title: "Blocked", Status: "todo", BlockedBy: []string{"blkactive"}})
	mustCreate(t, core, &nib.Nib{ID: "softblocked", Slug: "sb", Title: "SoftBlocked", Status: "todo", BlockedBy: []string{"blkdone"}})
	mustCreate(t, core, &nib.Nib{ID: "deferblocked", Slug: "pb", Title: "DeferBlocked", Status: "todo", BlockedBy: []string{"blkdefer"}})
	pr := resolver.ProjectionResolver(context.Background())

	cases := []struct {
		id   string
		want bool
	}{
		{"free", true},          // no blockers, startable status
		{"blocked", false},      // active blocker
		{"softblocked", true},   // only a released blocker -> still ready
		{"deferblocked", false}, // deferred blocker: closed, but never released
		{"missing", false},      // unknown id -> not ready
	}
	for _, tc := range cases {
		if got := pr.Ready(tc.id); got != tc.want {
			t.Errorf("Ready(%s) = %v, want %v", tc.id, got, tc.want)
		}
	}
}

// TestProjectionResolver_ReadyStatusHalf walks every declared status with the
// blocker half held constant (no blockers at all), so the only thing under test
// is which statuses `ready` will start work from. Expectations are literal, not
// derived from the Startable flag, so flipping that flag has to be restated
// here deliberately rather than following along.
func TestProjectionResolver_ReadyStatusHalf(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{"todo", true},
		{"in-progress", false}, // already underway; not something to start
		{"draft", false},       // needs refinement first
		{"deferred", false},    // closed
		{"completed", false},
		{"scrapped", false},
		{"", false}, // no status: in front matter -> not offered as work
	}

	// Every declared status must appear above, so the table cannot quietly stop
	// being exhaustive when the vocabulary grows.
	covered := map[string]bool{}
	for _, tc := range cases {
		covered[tc.status] = true
	}
	for _, s := range config.DefaultStatuses {
		if !covered[s.Name] {
			t.Fatalf("status %q has no case in this table; add one", s.Name)
		}
	}

	resolver, core := setupTestResolver(t)
	ids := make([]string, len(cases))
	for i, tc := range cases {
		ids[i] = fmt.Sprintf("s%d", i)
		mustCreate(t, core, &nib.Nib{ID: ids[i], Slug: ids[i], Title: "S", Status: tc.status})
	}
	pr := resolver.ProjectionResolver(context.Background())

	for i, tc := range cases {
		if got := pr.Ready(ids[i]); got != tc.want {
			t.Errorf("Ready(%s) with status %q = %v, want %v", ids[i], tc.status, got, tc.want)
		}
	}
}

// TestProjectionResolver_Relations pins the id-list relation delegation:
// blocking (incoming active blockers), and body-derived mentions in both
// directions. Each returns a non-nil slice.
func TestProjectionResolver_Relations(t *testing.T) {
	resolver, core := setupTestResolver(t)
	// src mentions dst via #dst in its body; dst is a pure inbound target.
	mustCreate(t, core, &nib.Nib{ID: "dst", Slug: "dst", Title: "Dst", Status: "todo"})
	mustCreate(t, core, &nib.Nib{ID: "src", Slug: "src", Title: "Src", Status: "todo", Body: "refers to #dst here"})
	// blockee is blocked by blk, so blk is blocking blockee.
	mustCreate(t, core, &nib.Nib{ID: "blk", Slug: "blk", Title: "Blk", Status: "todo"})
	mustCreate(t, core, &nib.Nib{ID: "blockee", Slug: "be", Title: "Blockee", Status: "todo", BlockedBy: []string{"blk"}})
	pr := resolver.ProjectionResolver(context.Background())

	if got := pr.Mentions("src"); !reflect.DeepEqual(got, []string{"dst"}) {
		t.Errorf("Mentions(src) = %v, want [dst]", got)
	}
	if got := pr.MentionedBy("dst"); !reflect.DeepEqual(got, []string{"src"}) {
		t.Errorf("MentionedBy(dst) = %v, want [src]", got)
	}
	if got := pr.Blocking("blk"); !reflect.DeepEqual(got, []string{"blockee"}) {
		t.Errorf("Blocking(blk) = %v, want [blockee]", got)
	}
	// Missing ids yield empty (non-nil) slices, never nil.
	if got := pr.Mentions("missing"); got == nil || len(got) != 0 {
		t.Errorf("Mentions(missing) = %v, want empty non-nil slice", got)
	}
	if got := pr.Blocking("missing"); got == nil || len(got) != 0 {
		t.Errorf("Blocking(missing) = %v, want empty non-nil slice", got)
	}
}

// TestProjectionResolver_NilContext verifies a nil ctx is tolerated (treated as
// Background) so callers that don't thread a request cache still work.
func TestProjectionResolver_NilContext(t *testing.T) {
	resolver, core := setupTestResolver(t)
	mustCreate(t, core, &nib.Nib{ID: "solo", Slug: "solo", Title: "Solo", Status: "todo"})
	//nolint:staticcheck // deliberately passing nil to exercise the fallback.
	pr := resolver.ProjectionResolver(nil)
	if got := pr.Ready("solo"); !got {
		t.Errorf("Ready(solo) with nil ctx = %v, want true", got)
	}
}
