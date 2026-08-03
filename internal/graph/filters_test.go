package graph

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
)

// TestResolveFilterID exercises the shared helper used by every filter.*ID
// branch in ApplyFilter. It must return the full ID for a known short form
// and the echoed input with ok=false for an unknown target.
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

// TestApplyFilterParentIDShortForm pins that a short ParentID is normalized
// before matching, exactly like the other filter.*ID branches: a filter with
// ParentID "parent" must match nibs whose Parent field holds the full
// "nibs-parent". Passing the short id raw to the exact-match filter makes
// `list --parent <short-id>` silently return nothing.
func TestApplyFilterParentIDShortForm(t *testing.T) {
	parent := &nib.Nib{ID: "nibs-parent", Title: "Parent"}
	child := &nib.Nib{ID: "nibs-child", Title: "Child", Parent: "nibs-parent"}
	unrelated := &nib.Nib{ID: "nibs-other", Title: "Other"}

	reader := &stubReader{
		nibs: map[string]*nib.Nib{
			"nibs-parent": parent,
			"nibs-child":  child,
			"nibs-other":  unrelated,
		},
		allNibs: []*nib.Nib{parent, child, unrelated},
		prefix:  "nibs-",
	}
	blocking := &stubBlockingChecker{}

	filter := &model.NibFilter{ParentID: strPtr("parent")}
	got := ApplyFilter(context.Background(), reader.allNibs, filter, reader, blocking)

	if len(got) != 1 {
		t.Fatalf("got %d nibs, want 1 (nibs-child); short ParentID was not normalized", len(got))
	}
	if got[0].ID != "nibs-child" {
		t.Errorf("got %q, want %q", got[0].ID, "nibs-child")
	}
}

// TestApplyFilterParentIDUnknownReturnsNil pins the "unknown target -> nil"
// contract for ParentID, matching the other single-ID filter branches.
func TestApplyFilterParentIDUnknownReturnsNil(t *testing.T) {
	child := &nib.Nib{ID: "nibs-child", Title: "Child", Parent: "nibs-parent"}
	reader := &stubReader{
		nibs:    map[string]*nib.Nib{"nibs-child": child},
		allNibs: []*nib.Nib{child},
		prefix:  "nibs-",
	}
	filter := &model.NibFilter{ParentID: strPtr("nonexistent")}
	got := ApplyFilter(context.Background(), reader.allNibs, filter, reader, &stubBlockingChecker{})
	if got != nil {
		t.Errorf("unknown ParentID should short-circuit to nil, got %v", got)
	}
}

// TestApplyFilterIDBranchesKnownAndUnknown verifies the "unknown target -> nil"
// contract across the link-based single-ID filter branches. Each routes through
// resolveFilterID and short-circuits to nil on miss — the trap is a branch that
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

// hierarchyFixture builds the tree the hierarchy-predicate tests share:
//
//	nibs-m1 ── nibs-e1 ─┬─ nibs-f1 ── nibs-t1
//	                    └─ nibs-t2
//	nibs-r2 ── nibs-x1
//	nibs-r3
//
// Three roots (m1, r2, r3) give siblingId a non-trivial root-level case, and
// the m1→e1→f1→t1 chain is deep enough that a one-level-only implementation of
// ancestorId/descendantId fails instead of accidentally passing.
//
// nibs-e1 is the one completed nib: it sits mid-chain, so a status filter can
// remove it from the candidate slice while leaving its subtree in place. That
// is what the "resolves ancestry through the store" row needs.
func hierarchyFixture() *stubReader {
	nibs := []*nib.Nib{
		{ID: "nibs-m1", Title: "Milestone", Status: "todo"},
		{ID: "nibs-e1", Title: "Epic", Parent: "nibs-m1", Status: "completed"},
		{ID: "nibs-f1", Title: "Feature", Parent: "nibs-e1", Status: "todo"},
		{ID: "nibs-t1", Title: "Task", Parent: "nibs-f1", Status: "todo"},
		{ID: "nibs-t2", Title: "Second child of the epic", Parent: "nibs-e1", Status: "todo"},
		{ID: "nibs-r2", Title: "Second root", Status: "todo"},
		{ID: "nibs-x1", Title: "Only child of the second root", Parent: "nibs-r2", Status: "todo"},
		{ID: "nibs-r3", Title: "Third root", Status: "todo"},
	}
	byID := make(map[string]*nib.Nib, len(nibs))
	for _, b := range nibs {
		byID[b.ID] = b
	}
	return &stubReader{nibs: byID, allNibs: nibs, prefix: "nibs-"}
}

// TestParentChain pins what parentChain banks for each shape of parent link.
// It is the unit the three hierarchy predicates are built on, and the shapes
// below are not reachable through ApplyFilter: a dangling or short-form link
// can only be created by hand-editing a nib file, and an unresolvable filter
// target short-circuits before any walk starts.
//
// The load-bearing rule: every id in the chain is a RESOLVED id, so the chain
// only ever names nibs that exist. A link that resolves under a different
// spelling is banked under the resolved one; a link that resolves to nothing
// contributes nothing at all.
func TestParentChain(t *testing.T) {
	// f1's stored link is the short form "e1". Core.Get resolves it by
	// prepending the prefix, so the walk continues through it — the chain must
	// record "nibs-e1", the spelling every filter target is normalized to.
	shortLinker := &nib.Nib{ID: "nibs-f1", Title: "Short-form parent link", Parent: "e1"}
	orphan := &nib.Nib{ID: "nibs-orphan", Title: "Dangling parent link", Parent: "nibs-ghost"}
	selfParent := &nib.Nib{ID: "nibs-self", Title: "Self-parented", Parent: "nibs-self"}
	nibs := []*nib.Nib{
		{ID: "nibs-m1", Title: "Milestone"},
		{ID: "nibs-e1", Title: "Epic", Parent: "nibs-m1"},
		shortLinker,
		{ID: "nibs-t1", Title: "Task", Parent: "nibs-f1"},
		orphan,
		selfParent,
		{ID: "nibs-c1", Title: "C1", Parent: "nibs-c2"},
		{ID: "nibs-c2", Title: "C2", Parent: "nibs-c1"},
	}
	byID := make(map[string]*nib.Nib, len(nibs))
	for _, b := range nibs {
		byID[b.ID] = b
	}
	reader := &stubReader{nibs: byID, allNibs: nibs, prefix: "nibs-"}

	tests := []struct {
		name  string
		nibID string
		want  []string
	}{
		{"root has no chain", "nibs-m1", nil},
		{"walks to the root, nearest ancestor first", "nibs-e1", []string{"nibs-m1"}},
		{"records the resolved id for a short-form link, not the stored spelling",
			"nibs-f1", []string{"nibs-e1", "nibs-m1"}},
		{"a short-form rung stays resolved for chains passing through it",
			"nibs-t1", []string{"nibs-f1", "nibs-e1", "nibs-m1"}},
		{"a dangling link contributes nothing", "nibs-orphan", nil},
		{"a self-parented nib yields an empty chain (the seed excludes it)", "nibs-self", nil},
		{"a cycle terminates and never contains self", "nibs-c1", []string{"nibs-c2"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := reader.Get(tt.nibID)
			if err != nil {
				t.Fatalf("fixture nib %q missing: %v", tt.nibID, err)
			}
			got := parentChain(b, reader)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parentChain(%s) = %v, want %v", tt.nibID, got, tt.want)
			}
		})
	}
}

// TestApplyFilterHierarchyShortFormParentLink is the end-to-end consequence of
// parentChain banking resolved ids. A hand-edited `parent: e1` is followed by
// Core.Get, so the tree is intact as far as the walk is concerned; if the raw
// spelling were banked instead, the chain would name a nib that does not
// exist and the predicates would report a hierarchy with a missing rung.
func TestApplyFilterHierarchyShortFormParentLink(t *testing.T) {
	m1 := &nib.Nib{ID: "nibs-m1", Title: "Milestone"}
	e1 := &nib.Nib{ID: "nibs-e1", Title: "Epic", Parent: "nibs-m1"}
	// Hand-edited short form; validateAndSetParent would have stored "nibs-e1".
	f1 := &nib.Nib{ID: "nibs-f1", Title: "Feature", Parent: "e1"}
	t1 := &nib.Nib{ID: "nibs-t1", Title: "Task", Parent: "nibs-f1"}

	reader := &stubReader{
		nibs: map[string]*nib.Nib{
			"nibs-m1": m1, "nibs-e1": e1, "nibs-f1": f1, "nibs-t1": t1,
		},
		allNibs: []*nib.Nib{m1, e1, f1, t1},
		prefix:  "nibs-",
	}
	blocking := &stubBlockingChecker{}

	t.Run("AncestorID matches through a short-form rung", func(t *testing.T) {
		got := ApplyFilter(context.Background(), reader.allNibs, &model.NibFilter{AncestorID: strPtr("e1")}, reader, blocking)
		assertNibIDs(t, got, []string{"nibs-f1", "nibs-t1"})
	})

	t.Run("DescendantID reports the whole chain, not one with a hole in it", func(t *testing.T) {
		got := ApplyFilter(context.Background(), reader.allNibs, &model.NibFilter{DescendantID: strPtr("t1")}, reader, blocking)
		assertNibIDs(t, got, []string{"nibs-f1", "nibs-e1", "nibs-m1"})
	})
}

// TestApplyFilterHasParentResolvesParentLink pins that hasParent decides
// parent-ness the same way the rest of the surface does — through the resolved
// parent, not the raw stored string. A dangling link is the shape that
// separates the two: nibResolver.Parent collapses it to nil and fetchSiblings
// puts the nib in the root set, so a raw emptiness check here would report a
// parent that nothing else in the graph can show.
func TestApplyFilterHasParentResolvesParentLink(t *testing.T) {
	root := &nib.Nib{ID: "nibs-root", Title: "Root"}
	par := &nib.Nib{ID: "nibs-par", Title: "Parent"}
	child := &nib.Nib{ID: "nibs-chi", Title: "Child", Parent: "nibs-par"}
	// Short form resolves, so this nib genuinely has a parent.
	shortChild := &nib.Nib{ID: "nibs-sho", Title: "Short-form parent link", Parent: "par"}
	// Names no nib under either spelling, so it has no parent to resolve to.
	dangling := &nib.Nib{ID: "nibs-dng", Title: "Dangling parent link", Parent: "nibs-ghost"}

	all := []*nib.Nib{root, par, child, shortChild, dangling}
	byID := make(map[string]*nib.Nib, len(all))
	for _, b := range all {
		byID[b.ID] = b
	}
	reader := &stubReader{nibs: byID, allNibs: all, prefix: "nibs-"}
	blocking := &stubBlockingChecker{}

	tests := []struct {
		name    string
		want    bool
		wantIDs []string
	}{
		{"true keeps only the nibs whose parent link resolves", true,
			[]string{"nibs-chi", "nibs-sho"}},
		{"false keeps the parentless and the dangling alike", false,
			[]string{"nibs-root", "nibs-par", "nibs-dng"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := &model.NibFilter{HasParent: &tt.want}
			assertNibIDs(t, ApplyFilter(context.Background(), reader.allNibs, filter, reader, blocking), tt.wantIDs)
		})
	}
}

// TestApplyFilterSiblingIDResolvesParentLinks pins that siblingId decides
// "same parent" through the resolved parent, the way nibResolver.Parent and
// fetchSiblings do, rather than by comparing the raw stored strings. Two stored
// shapes make the two disagree:
//
//   - a dangling link presents as a root everywhere the object graph is
//     walked, so it belongs in a root-level sibling set;
//   - a short-form link names the same parent as its full-form spelling, so
//     the two spellings are siblings.
//
// Both are injected straight into the reader here, which pins the filter's own
// behavior rather than the loader's. Only the short form is a shape a loaded
// store canonicalizes away before a filter can see it; a dangling link survives
// load verbatim and does reach the filter through a real Load. The loaded-store
// paths are covered separately — the dangling one by
// TestDanglingParentClassifiedAlikeAcrossSurfaces, the short-form one by
// cmd.TestSiblingSurfacesAgreeOnShortFormParentLink.
func TestApplyFilterSiblingIDResolvesParentLinks(t *testing.T) {
	m1 := &nib.Nib{ID: "nibs-m1", Title: "Root"}
	r2 := &nib.Nib{ID: "nibs-r2", Title: "Second root"}
	orphan := &nib.Nib{ID: "nibs-orphan", Title: "Dangling parent link", Parent: "nibs-ghost"}
	e1 := &nib.Nib{ID: "nibs-e1", Title: "Epic", Parent: "nibs-m1"}
	// f1 and t2 are siblings: f1 spells the shared parent short, t2 spells it full.
	f1 := &nib.Nib{ID: "nibs-f1", Title: "Short-form parent link", Parent: "e1"}
	t2 := &nib.Nib{ID: "nibs-t2", Title: "Full-form parent link", Parent: "nibs-e1"}

	all := []*nib.Nib{m1, r2, orphan, e1, f1, t2}
	byID := make(map[string]*nib.Nib, len(all))
	for _, b := range all {
		byID[b.ID] = b
	}
	reader := &stubReader{nibs: byID, allNibs: all, prefix: "nibs-"}
	blocking := &stubBlockingChecker{}

	tests := []struct {
		name    string
		filter  *model.NibFilter
		wantIDs []string
	}{
		{"a root's siblings include a nib whose parent link is dangling",
			&model.NibFilter{SiblingID: strPtr("m1")}, []string{"nibs-r2", "nibs-orphan"}},
		{"a dangling-parent nib is itself treated as a root",
			&model.NibFilter{SiblingID: strPtr("orphan")}, []string{"nibs-m1", "nibs-r2"}},
		{"a full-form spelling finds the short-form sibling",
			&model.NibFilter{SiblingID: strPtr("t2")}, []string{"nibs-f1"}},
		{"a short-form spelling finds the full-form sibling",
			&model.NibFilter{SiblingID: strPtr("f1")}, []string{"nibs-t2"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertNibIDs(t, ApplyFilter(context.Background(), reader.allNibs, tt.filter, reader, blocking), tt.wantIDs)
		})
	}
}

// TestApplyFilterHierarchyPredicates covers the ancestorId / descendantId /
// siblingId branches over a multi-level tree.
//
// Direction is the thing to get right and the easy thing to invert: like every
// other *ID filter, each field names the relationship the MATCHED nib holds
// toward the supplied target. ancestorId: X therefore keeps the nibs whose
// ancestor is X (X's descendants), and descendantId: X keeps the nibs whose
// descendant is X (X's ancestors). The table pins both directions against the
// same fixture, so swapping the two branches fails rather than reshuffles.
//
// Most filter arguments are short IDs: the branches must normalize through
// resolveFilterID, since the stored Parent links are full prefixed IDs. One
// row per field passes the already-full form, which must resolve to the same
// answer rather than being double-prefixed into a miss.
//
// The "unknown target matches nothing" rows pin user-facing behavior only.
// They do NOT prove the `if !ok { return nil }` short-circuit in each branch:
// that guard is not independently observable here (see the note on the
// hierarchy branch block in ApplyFilter), so these rows would still pass with
// it deleted. What they pin is that an id naming no nib cannot match anything,
// which is the promise the API makes.
func TestApplyFilterHierarchyPredicates(t *testing.T) {
	reader := hierarchyFixture()
	blocking := &stubBlockingChecker{}

	tests := []struct {
		name    string
		filter  *model.NibFilter
		wantIDs []string // expected nib IDs (empty → matched nothing)
	}{
		// ancestorId — "nibs that have the target among their ancestors".
		{"AncestorID keeps descendants at every depth, target excluded",
			&model.NibFilter{AncestorID: strPtr("e1")},
			[]string{"nibs-f1", "nibs-t1", "nibs-t2"}},
		{"AncestorID on the root keeps the whole subtree, root excluded",
			&model.NibFilter{AncestorID: strPtr("m1")},
			[]string{"nibs-e1", "nibs-f1", "nibs-t1", "nibs-t2"}},
		{"AncestorID accepts the already-full form",
			&model.NibFilter{AncestorID: strPtr("nibs-e1")},
			[]string{"nibs-f1", "nibs-t1", "nibs-t2"}},
		{"AncestorID on a leaf matches nothing",
			&model.NibFilter{AncestorID: strPtr("t1")}, nil},
		{"AncestorID unknown matches nothing",
			&model.NibFilter{AncestorID: strPtr("nonexistent")}, nil},

		// descendantId — "nibs that have the target among their descendants",
		// i.e. exactly the target's ancestor chain.
		{"DescendantID keeps the whole ancestor chain, target excluded",
			&model.NibFilter{DescendantID: strPtr("t1")},
			[]string{"nibs-f1", "nibs-e1", "nibs-m1"}},
		{"DescendantID on a mid-level nib keeps only what is above it",
			&model.NibFilter{DescendantID: strPtr("e1")},
			[]string{"nibs-m1"}},
		{"DescendantID accepts the already-full form",
			&model.NibFilter{DescendantID: strPtr("nibs-t1")},
			[]string{"nibs-f1", "nibs-e1", "nibs-m1"}},
		{"DescendantID on a root matches nothing",
			&model.NibFilter{DescendantID: strPtr("m1")}, nil},
		{"DescendantID unknown matches nothing",
			&model.NibFilter{DescendantID: strPtr("nonexistent")}, nil},

		// siblingId — "nibs sharing the target's parent", root-level target
		// included (matches fetchSiblings in cmd/rel.go).
		{"SiblingID keeps the other children of the same parent, target excluded",
			&model.NibFilter{SiblingID: strPtr("f1")},
			[]string{"nibs-t2"}},
		{"SiblingID on a root nib keeps the other roots, target excluded",
			&model.NibFilter{SiblingID: strPtr("m1")},
			[]string{"nibs-r2", "nibs-r3"}},
		{"SiblingID accepts the already-full form",
			&model.NibFilter{SiblingID: strPtr("nibs-f1")},
			[]string{"nibs-t2"}},
		{"SiblingID on an only child matches nothing",
			&model.NibFilter{SiblingID: strPtr("x1")}, nil},
		{"SiblingID unknown matches nothing",
			&model.NibFilter{SiblingID: strPtr("nonexistent")}, nil},

		// Two hierarchy predicates AND-composed: each is a pure per-element
		// predicate, so the result is the intersection regardless of the order
		// ApplyFilter runs the branches in. m1's subtree is {e1,f1,t1,t2} and
		// t1's ancestor chain is {f1,e1,m1}.
		{"AncestorID and DescendantID compose as an intersection",
			&model.NibFilter{AncestorID: strPtr("m1"), DescendantID: strPtr("t1")},
			[]string{"nibs-e1", "nibs-f1"}},

		// Ancestry comes from the store, not from the candidate slice: the
		// status filter runs first and drops the completed nibs-e1, but its
		// subtree must still resolve through it up to m1. This row is what
		// fails if a future optimization indexes the candidate slice instead
		// of walking the reader.
		{"AncestorID resolves ancestry through the store, not the candidate slice",
			&model.NibFilter{AncestorID: strPtr("m1"), Status: []string{"todo"}},
			[]string{"nibs-f1", "nibs-t1", "nibs-t2"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertNibIDs(t, ApplyFilter(context.Background(), reader.allNibs, tt.filter, reader, blocking), tt.wantIDs)
		})
	}
}

// TestApplyFilterHierarchyPredicatesCycleSafe pins that the parent-chain walks
// behind ancestorId and descendantId terminate on a parent cycle, and that the
// target stays out of its own result even when a cycle makes it structurally
// reachable from itself. The mutation resolvers reject cycles, but a
// hand-edited nib file can still create one, and an unguarded
// `for parent != ""` walk hangs every query that uses these filters.
func TestApplyFilterHierarchyPredicatesCycleSafe(t *testing.T) {
	c1 := &nib.Nib{ID: "nibs-c1", Title: "C1", Parent: "nibs-c2"}
	c2 := &nib.Nib{ID: "nibs-c2", Title: "C2", Parent: "nibs-c1"}
	outside := &nib.Nib{ID: "nibs-out", Title: "Outside the cycle"}

	reader := &stubReader{
		nibs:    map[string]*nib.Nib{"nibs-c1": c1, "nibs-c2": c2, "nibs-out": outside},
		allNibs: []*nib.Nib{c1, c2, outside},
		prefix:  "nibs-",
	}
	blocking := &stubBlockingChecker{}

	t.Run("AncestorID terminates and excludes the target itself", func(t *testing.T) {
		got := ApplyFilter(context.Background(), reader.allNibs, &model.NibFilter{AncestorID: strPtr("c1")}, reader, blocking)
		assertNibIDs(t, got, []string{"nibs-c2"})
	})

	t.Run("DescendantID terminates and excludes the target itself", func(t *testing.T) {
		got := ApplyFilter(context.Background(), reader.allNibs, &model.NibFilter{DescendantID: strPtr("c1")}, reader, blocking)
		assertNibIDs(t, got, []string{"nibs-c2"})
	})
}

// TestQueryNibsSearchReAddsHierarchyTargets pins what queryResolver.Nibs
// actually returns when a hierarchy predicate is combined with search, which
// is not what ApplyFilter alone returns. The search branch runs
// includeAncestors afterwards so the client can render a complete tree, and
// that step re-adds ancestors of the survivors:
//
//   - ancestorId: the target comes back, contradicting "itself excluded" taken
//     as an absolute promise. The schema description says so.
//   - siblingId: the target stays out (it is nobody's ancestor), but the
//     shared parent arrives.
//   - descendantId: unaffected — every ancestor added is already on the
//     target's ancestor chain, so it satisfies the predicate anyway.
//
// This is deliberate, not a defect to fix in ApplyFilter: the web UI's tree
// rendering and ancestor dimming depend on the completion. The test exists so
// that changing it is a conscious decision with a visible cost.
func TestQueryNibsSearchReAddsHierarchyTargets(t *testing.T) {
	reader := hierarchyFixture()
	// Search is a wide net here so the hierarchy predicate, not the search
	// term, is what narrows the result; includeAncestors then widens it again.
	reader.searchOut = map[string][]*nib.Nib{"anything": reader.allNibs}

	resolver := &Resolver{
		Reader:    reader,
		Writer:    &stubWriter{store: reader},
		Validator: &stubValidator{},
		Blocking:  &stubBlockingChecker{},
		Orderer:   NewOrderer(reader, &stubWriter{store: reader}),
	}

	tests := []struct {
		name    string
		filter  *model.NibFilter
		wantIDs []string
	}{
		{"ancestorId: the excluded target is added back as an ancestor",
			&model.NibFilter{Search: strPtr("anything"), AncestorID: strPtr("e1")},
			// ApplyFilter alone returns f1, t1, t2; completion adds e1 and m1.
			[]string{"nibs-f1", "nibs-t1", "nibs-t2", "nibs-e1", "nibs-m1"}},
		{"siblingId: the target stays out but the shared parent arrives",
			&model.NibFilter{Search: strPtr("anything"), SiblingID: strPtr("f1")},
			// ApplyFilter alone returns t2; completion adds e1 and m1.
			[]string{"nibs-t2", "nibs-e1", "nibs-m1"}},
		{"descendantId: completion adds nothing the predicate did not already keep",
			&model.NibFilter{Search: strPtr("anything"), DescendantID: strPtr("t1")},
			[]string{"nibs-f1", "nibs-e1", "nibs-m1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolver.Query().Nibs(context.Background(), tt.filter, nil)
			if err != nil {
				t.Fatalf("Nibs: %v", err)
			}
			assertNibIDs(t, got, tt.wantIDs)
		})
	}
}

// assertNibIDs compares the IDs of got against want, order-insensitively.
// A nil result is treated as the empty set — ApplyFilter builds its results
// with append, so "matched nothing" and "short-circuited" both surface as nil;
// callers that care about the difference assert on nil-ness themselves.
func assertNibIDs(t *testing.T, got []*nib.Nib, want []string) {
	t.Helper()
	gotIDs := make([]string, 0, len(got))
	for _, b := range got {
		gotIDs = append(gotIDs, b.ID)
	}
	wantIDs := append([]string(nil), want...)
	sort.Strings(gotIDs)
	sort.Strings(wantIDs)
	if len(gotIDs) == 0 && len(wantIDs) == 0 {
		return
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Errorf("got IDs %v, want %v", gotIDs, wantIDs)
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

// TestApplyFilterPresenceTriState pins HasParent and HasBlocking as tri-state
// fields: nil filters nothing, &true keeps the nibs that have the thing, and
// &false keeps exactly the complement. The &false rows are what let the CLI's
// --no-parent/--no-blocking spellings route through a single field, so they are
// the model contract those flags stand on.
func TestApplyFilterPresenceTriState(t *testing.T) {
	root := &nib.Nib{ID: "nibs-root", Title: "Root"}
	// The child also carries a blocked_by entry, so the HasBlockedBy rows below
	// have something to discriminate on. It is consistent with the blocking stub:
	// root blocks the child, the child is blocked by root.
	child := &nib.Nib{ID: "nibs-child", Title: "Child", Parent: "nibs-root", BlockedBy: []string{"nibs-root"}}

	reader := &stubReader{
		nibs: map[string]*nib.Nib{
			"nibs-root":  root,
			"nibs-child": child,
		},
		allNibs: []*nib.Nib{root, child},
		prefix:  "nibs-",
	}
	// Only the root blocks anything, so each blocking answer is a singleton.
	blocking := &stubBlockingChecker{blocking: map[string]bool{"nibs-root": true}}

	yes, no := true, false
	tests := []struct {
		name    string
		filter  *model.NibFilter
		wantIDs []string
	}{
		{"HasParent nil filters nothing", &model.NibFilter{}, []string{"nibs-root", "nibs-child"}},
		{"HasParent true keeps parented nibs", &model.NibFilter{HasParent: &yes}, []string{"nibs-child"}},
		{"HasParent false keeps exactly the parentless nibs", &model.NibFilter{HasParent: &no}, []string{"nibs-root"}},
		{"HasBlocking true keeps blocking nibs", &model.NibFilter{HasBlocking: &yes}, []string{"nibs-root"}},
		{"HasBlocking false keeps exactly the non-blocking nibs", &model.NibFilter{HasBlocking: &no}, []string{"nibs-child"}},
		// HasBlockedBy had a NoBlockedBy twin until the pair was collapsed. These
		// two rows are what stop the survivor regressing to an include-only
		// filter, which is how the twin came to exist in the first place.
		{"HasBlockedBy true keeps nibs with blocked_by entries", &model.NibFilter{HasBlockedBy: &yes}, []string{"nibs-child"}},
		{"HasBlockedBy false keeps exactly those with none", &model.NibFilter{HasBlockedBy: &no}, []string{"nibs-root"}},
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
