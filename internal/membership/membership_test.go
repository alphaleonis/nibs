package membership

import (
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
)

// n builds a test nib. Membership is pure over the slice, so every fixture is
// a plain literal — no store, no mocks.
func n(id, typ, parent string) *nib.Nib {
	return &nib.Nib{ID: id, Title: id, Type: typ, Parent: parent}
}

func ids(nibs []*nib.Nib) []string {
	out := make([]string, len(nibs))
	for i, b := range nibs {
		out[i] = b.ID
	}
	return out
}

func wantIDs(t *testing.T, label string, got []*nib.Nib, want ...string) {
	t.Helper()
	g := ids(got)
	if len(g) != len(want) {
		t.Fatalf("%s = %v, want %v", label, g, want)
	}
	for i := range g {
		if g[i] != want[i] {
			t.Fatalf("%s = %v, want %v", label, g, want)
		}
	}
}

// standardFixture: two milestones, an epic chain three levels deep under m1
// (legal shape: milestone → epic → feature → task), a direct milestone item,
// an unscheduled epic with an item, a loose root task, and a task whose parent
// link dangles.
//
//	m1 ── e1 ── f1 ── t1
//	  └── b1 (direct bug)
//	m2 (empty)
//	e2 ── t2        (unscheduled epic)
//	t3              (root task)
//	t4 → ghost      (dangling parent)
func standardFixture() []*nib.Nib {
	return []*nib.Nib{
		n("m1", "milestone", ""),
		n("e1", "epic", "m1"),
		n("f1", "feature", "e1"),
		n("t1", "task", "f1"),
		n("b1", "bug", "m1"),
		n("m2", "milestone", ""),
		n("e2", "epic", ""),
		n("t2", "task", "e2"),
		n("t3", "task", ""),
		n("t4", "task", "ghost"),
	}
}

func TestMilestones(t *testing.T) {
	v := Compute(standardFixture())
	wantIDs(t, "Milestones()", v.Milestones(), "m1", "m2")
}

func TestChildrenAndDirectMembers(t *testing.T) {
	v := Compute(standardFixture())

	// Children is the structural parent axis, in input order.
	wantIDs(t, `Children("m1")`, v.Children("m1"), "e1", "b1")
	wantIDs(t, `Children("e1")`, v.Children("e1"), "f1")
	wantIDs(t, `Children("f1")`, v.Children("f1"), "t1")
	wantIDs(t, `Children("m2")`, v.Children("m2"))
	// The dangling t4 resolves to the root set, not to a phantom "ghost" group.
	wantIDs(t, `Children("")`, v.Children(""), "m1", "m2", "e2", "t3", "t4")

	// DirectMembers agrees with Children in step 1 (the assignment axis IS the
	// parent axis until milestone: exists) — modulo the container exclusion
	// TestMilestoneTypedNibsAreNeverMembers pins.
	wantIDs(t, `DirectMembers("m1")`, v.DirectMembers("m1"), "e1", "b1")
	wantIDs(t, `DirectMembers("e2")`, v.DirectMembers("e2"), "t2")
}

func TestMembersIsTheFullDepthClosure(t *testing.T) {
	v := Compute(standardFixture())

	// Ledger delta (a): the closure is FULL depth. A task under a feature
	// under an epic under the milestone is a member — the roadmap's old
	// two-level walk could not see t1.
	wantIDs(t, `Members("m1")`, v.Members("m1"), "e1", "b1", "f1", "t1")
	wantIDs(t, `Members("e1")`, v.Members("e1"), "f1", "t1")
	wantIDs(t, `Members("m2")`, v.Members("m2"))
}

func TestMilestoneOf(t *testing.T) {
	v := Compute(standardFixture())
	cases := map[string]string{
		"e1": "m1", "f1": "m1", "t1": "m1", "b1": "m1",
		"e2": "", "t2": "", "t3": "", "t4": "",
		// Containers are not scheduled into themselves or each other.
		"m1": "", "m2": "",
	}
	for id, want := range cases {
		if got := v.MilestoneOf(id); got != want {
			t.Errorf("MilestoneOf(%q) = %q, want %q", id, got, want)
		}
	}
	if got := v.MilestoneOf("no-such-nib"); got != "" {
		t.Errorf("MilestoneOf(unknown) = %q, want \"\"", got)
	}
}

func TestGrouped(t *testing.T) {
	v := Compute(standardFixture())
	tree := v.Grouped("m1")
	if len(tree.Epics) != 1 || tree.Epics[0].Epic.ID != "e1" {
		t.Fatalf("Grouped(m1).Epics = %v, want [e1]", tree.Epics)
	}
	wantIDs(t, "Grouped(m1).Epics[0].Items", tree.Epics[0].Items, "f1")
	wantIDs(t, "Grouped(m1).Other", tree.Other, "b1")

	empty := v.Grouped("m2")
	if len(empty.Epics) != 0 || len(empty.Other) != 0 {
		t.Errorf("Grouped(m2) = %+v, want empty", empty)
	}
}

func TestUnscheduled(t *testing.T) {
	v := Compute(standardFixture())
	rem := v.Unscheduled()

	if len(rem.Epics) != 1 || rem.Epics[0].Epic.ID != "e2" {
		t.Fatalf("Unscheduled().Epics = %v, want [e2]", rem.Epics)
	}
	wantIDs(t, "Unscheduled().Epics[0].Items", rem.Epics[0].Items, "t2")

	// Ledger delta (b): t4's parent link names no nib, so t4 IS a root by the
	// resolved reading every query surface uses — the old raw `Parent != ""`
	// orphan scan hid it from the roadmap entirely.
	wantIDs(t, "Unscheduled().Other", rem.Other, "t3", "t4")
}

// TestMilestoneTypedNibsAreNeverMembers pins ledger delta (c): a
// milestone-typed nib under another container (illegal data — milestones
// cannot have parents) is not a member of anything, and the closure does not
// descend through it — its subtree belongs to its own queue.
func TestMilestoneTypedNibsAreNeverMembers(t *testing.T) {
	v := Compute([]*nib.Nib{
		n("m1", "milestone", ""),
		n("mx", "milestone", "m1"), // illegal nest
		n("tx", "task", "mx"),
		n("b1", "bug", "m1"),
	})
	wantIDs(t, `DirectMembers("m1")`, v.DirectMembers("m1"), "b1")
	wantIDs(t, `Members("m1")`, v.Members("m1"), "b1")
	// The nested milestone still answers for its own subtree, and belongs to
	// no milestone itself — even nested, it is a container, not a member.
	wantIDs(t, `Members("mx")`, v.Members("mx"), "tx")
	if got := v.MilestoneOf("mx"); got != "" {
		t.Errorf(`MilestoneOf("mx") = %q, want "" — a milestone is never scheduled into another`, got)
	}
	// But the structural child axis still reports it — ChildCount stays honest.
	wantIDs(t, `Children("m1")`, v.Children("m1"), "mx", "b1")
}

// TestCycleTermination: invariant-violating data with a parent cycle still
// produces a deterministic View, and every accessor terminates.
func TestCycleTermination(t *testing.T) {
	v := Compute([]*nib.Nib{
		n("m1", "milestone", ""),
		n("a", "task", "b"),
		n("b", "task", "a"),
		n("c", "task", "m1"),
	})
	wantIDs(t, `Members("m1")`, v.Members("m1"), "c")
	if got := v.MilestoneOf("a"); got != "" {
		t.Errorf("MilestoneOf inside a parent cycle = %q, want \"\"", got)
	}
	// The cycle participants belong to no root and no milestone; they are not
	// in the root set (their parents resolve to real nibs).
	wantIDs(t, `Children("")`, v.Children(""), "m1")
}

// TestDeterminism: two Computes over the same slice agree on every output —
// no map-iteration order reaches any accessor.
func TestDeterminism(t *testing.T) {
	all := standardFixture()
	v1, v2 := Compute(all), Compute(all)
	for _, id := range []string{"", "m1", "m2", "e1", "e2"} {
		wantIDs(t, "Children mismatch", v2.Children(id), ids(v1.Children(id))...)
		wantIDs(t, "Members mismatch", v2.Members(id), ids(v1.Members(id))...)
	}
	wantIDs(t, "Unscheduled mismatch", v2.Unscheduled().Other, ids(v1.Unscheduled().Other)...)
}

// TestResolvedMilestoneID pins THE step-1 definition of "directly assigned":
// b's resolved parent iff milestone-typed, with resolvedParentID's
// dangling-link rule. The ordering engine consumes this via a Lookup closure;
// step 2 swaps the body to read the milestone: field and every caller
// survives unchanged.
func TestResolvedMilestoneID(t *testing.T) {
	m1 := n("m1", "milestone", "")
	e1 := n("e1", "epic", "m1")
	t1 := n("t1", "task", "e1")
	t4 := n("t4", "task", "ghost")
	byID := map[string]*nib.Nib{"m1": m1, "e1": e1, "t1": t1, "t4": t4}
	lookup := func(id string) *nib.Nib { return byID[id] }

	cases := []struct {
		b    *nib.Nib
		want string
	}{
		{e1, "m1"},
		{t1, ""}, // parent is an epic, not a milestone
		{t4, ""}, // dangling link resolves to no parent
		{m1, ""}, // no parent at all
	}
	for _, tc := range cases {
		if got := ResolvedMilestoneID(tc.b, lookup); got != tc.want {
			t.Errorf("ResolvedMilestoneID(%s) = %q, want %q", tc.b.ID, got, tc.want)
		}
	}
}
