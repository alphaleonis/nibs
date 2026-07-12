package graph

import (
	"context"
	"reflect"
	"testing"

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
// ChildCount is the number of children, Progress counts resolved children
// (completed or scrapped) with a rounded percentage. A leaf reports zeros.
func TestProjectionResolver_ChildCountAndProgress(t *testing.T) {
	resolver, core := setupTestResolver(t)
	mustCreate(t, core, &nib.Nib{ID: "par", Slug: "par", Title: "Parent", Status: "in-progress"})
	mustCreate(t, core, &nib.Nib{ID: "c1", Slug: "c1", Title: "C1", Status: "completed", Parent: "par"})
	mustCreate(t, core, &nib.Nib{ID: "c2", Slug: "c2", Title: "C2", Status: "scrapped", Parent: "par"})
	mustCreate(t, core, &nib.Nib{ID: "c3", Slug: "c3", Title: "C3", Status: "todo", Parent: "par"})
	pr := resolver.ProjectionResolver(context.Background())

	if got := pr.ChildCount("par"); got != 3 {
		t.Errorf("ChildCount(par) = %d, want 3", got)
	}
	if got := pr.ChildCount("c1"); got != 0 {
		t.Errorf("ChildCount(c1) = %d, want 0 (leaf)", got)
	}

	// 2 of 3 children resolved (completed + scrapped) -> 67%.
	want := ProgressRollup{Total: 3, Done: 2, Percent: 67}
	if got := pr.Progress("par"); !reflect.DeepEqual(got, want) {
		t.Errorf("Progress(par) = %#v, want %#v", got, want)
	}
	// Leaf: no children -> all zeros, no divide-by-zero.
	wantLeaf := ProgressRollup{Total: 0, Done: 0, Percent: 0}
	if got := pr.Progress("c3"); !reflect.DeepEqual(got, wantLeaf) {
		t.Errorf("Progress(c3) = %#v, want %#v", got, wantLeaf)
	}
}

// TestProjectionResolver_Ready pins the startability rule: not resolved and no
// active blockers. A blocker that is itself resolved does not keep a nib
// un-ready (mirroring BlockedByIds), and a resolved nib is never ready.
func TestProjectionResolver_Ready(t *testing.T) {
	resolver, core := setupTestResolver(t)
	mustCreate(t, core, &nib.Nib{ID: "blkactive", Slug: "ba", Title: "BA", Status: "todo"})
	mustCreate(t, core, &nib.Nib{ID: "blkdone", Slug: "bd", Title: "BD", Status: "completed"})
	mustCreate(t, core, &nib.Nib{ID: "free", Slug: "f", Title: "Free", Status: "todo"})
	mustCreate(t, core, &nib.Nib{ID: "blocked", Slug: "b", Title: "Blocked", Status: "todo", BlockedBy: []string{"blkactive"}})
	mustCreate(t, core, &nib.Nib{ID: "softblocked", Slug: "sb", Title: "SoftBlocked", Status: "todo", BlockedBy: []string{"blkdone"}})
	mustCreate(t, core, &nib.Nib{ID: "donenib", Slug: "d", Title: "Done", Status: "completed"})
	pr := resolver.ProjectionResolver(context.Background())

	cases := []struct {
		id   string
		want bool
	}{
		{"free", true},         // no blockers, active
		{"blocked", false},     // active blocker
		{"softblocked", true},  // only a resolved blocker -> still ready
		{"donenib", false},     // resolved status is never ready
		{"missing", false},     // unknown id -> not ready
	}
	for _, tc := range cases {
		if got := pr.Ready(tc.id); got != tc.want {
			t.Errorf("Ready(%s) = %v, want %v", tc.id, got, tc.want)
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
