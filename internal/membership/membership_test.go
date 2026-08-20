package membership

import (
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
)

// n builds a test nib on the structural parent axis. Membership is pure over
// the slice, so every fixture is a plain literal — no store, no mocks.
func n(id, typ, parent string) *nib.Nib {
	return &nib.Nib{ID: id, Title: id, Type: typ, Parent: parent}
}

// q builds a test nib assigned to a milestone queue via the milestone: field.
func q(id, typ, milestone string) *nib.Nib {
	return &nib.Nib{ID: id, Title: id, Type: typ, Milestone: milestone}
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

// standardFixture in the v2 three-axis shape: two milestones, an epic assigned
// to m1's queue carrying a structural chain (epic → feature → task), a direct
// queue item that is structurally a root, an unassigned epic with an item, a
// loose root task, and a task whose parent link dangles.
//
//	m1 ⇐ e1 ── f1 ── t1   (e1 assigned via milestone:)
//	m1 ⇐ b1               (direct queue item, structurally a root)
//	m2 (empty)
//	e2 ── t2              (unassigned epic)
//	t3                    (root task)
//	t4 → ghost            (dangling parent)
func standardFixture() []*nib.Nib {
	return []*nib.Nib{
		n("m1", "milestone", ""),
		q("e1", "epic", "m1"),
		n("f1", "feature", "e1"),
		n("t1", "task", "f1"),
		q("b1", "bug", "m1"),
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

	// Children is the structural parent axis, in input order. An assignee is
	// not a child: the milestone honestly reports no children while its queue
	// carries the members.
	wantIDs(t, `Children("m1")`, v.Children("m1"))
	wantIDs(t, `Children("e1")`, v.Children("e1"), "f1")
	wantIDs(t, `Children("f1")`, v.Children("f1"), "t1")
	wantIDs(t, `Children("m2")`, v.Children("m2"))
	// The dangling t4 resolves to the root set, not to a phantom "ghost"
	// group; the assignees e1 and b1 are structural roots too.
	wantIDs(t, `Children("")`, v.Children(""), "m1", "e1", "b1", "m2", "e2", "t3", "t4")

	// DirectMembers answers from the assignment axis for a milestone, and
	// from the structural decomposition for every other container.
	wantIDs(t, `DirectMembers("m1")`, v.DirectMembers("m1"), "e1", "b1")
	wantIDs(t, `DirectMembers("e2")`, v.DirectMembers("e2"), "t2")
	wantIDs(t, `DirectMembers("e1")`, v.DirectMembers("e1"), "f1")
}

func TestMembersIsTheFullDepthClosure(t *testing.T) {
	v := Compute(standardFixture())

	// The closure is still FULL depth: the assignees plus their structural
	// subtrees — a task under a feature under the assigned epic is a member.
	wantIDs(t, `Members("m1")`, v.Members("m1"), "e1", "b1", "f1", "t1")
	wantIDs(t, `Members("e1")`, v.Members("e1"), "f1", "t1")
	wantIDs(t, `Members("m2")`, v.Members("m2"))
}

func TestMilestoneOf(t *testing.T) {
	v := Compute(standardFixture())
	cases := map[string]string{
		// e1 and b1 by their own assignment; f1 and t1 through the structural
		// chain up to the assigned e1.
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

// TestMilestoneOfStopsAtAStructuralMilestoneAncestor pins the axis cutover's
// reading of a hand-authored milestone PARENT: the parent edge is decomposition
// only and confers no membership, so work structurally under a milestone with
// no assignment anywhere on its chain is unscheduled.
func TestMilestoneOfStopsAtAStructuralMilestoneAncestor(t *testing.T) {
	v := Compute([]*nib.Nib{
		n("m1", "milestone", ""),
		n("t1", "task", "m1"), // structural child of a milestone, unassigned
	})
	if got := v.MilestoneOf("t1"); got != "" {
		t.Errorf(`MilestoneOf("t1") = %q, want "" — a milestone parent is not an assignment`, got)
	}
	if rem := v.Unscheduled(); len(rem.Other) != 0 {
		// t1 is not a root (its parent resolves), so it is not backlog either.
		t.Errorf("Unscheduled().Other = %v, want none", ids(rem.Other))
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

	// t4's parent link names no nib, so t4 IS a root by the resolved reading
	// every query surface uses. b1 is a root too, but its assignment schedules
	// it — the backlog is roots with no milestone anywhere on their chain.
	wantIDs(t, "Unscheduled().Other", rem.Other, "t3", "t4")
}

// TestMilestoneTypedNibsAreNeverMembers pins the container exclusion on BOTH
// axes: a milestone-typed nib is a container of its own and never a member —
// not through an illegal structural nest, and not through a hand-authored
// milestone: assignment either.
func TestMilestoneTypedNibsAreNeverMembers(t *testing.T) {
	mx := q("mx", "milestone", "m1") // garbage assignment on a container
	mx.Parent = "m1"                 // and an illegal structural nest
	v := Compute([]*nib.Nib{
		n("m1", "milestone", ""),
		mx,
		q("tx", "task", "mx"),
		q("b1", "bug", "m1"),
	})
	wantIDs(t, `DirectMembers("m1")`, v.DirectMembers("m1"), "b1")
	wantIDs(t, `Members("m1")`, v.Members("m1"), "b1")
	// The nested milestone still answers for its own queue, and belongs to
	// no milestone itself — even nested, it is a container, not a member.
	wantIDs(t, `Members("mx")`, v.Members("mx"), "tx")
	if got := v.MilestoneOf("mx"); got != "" {
		t.Errorf(`MilestoneOf("mx") = %q, want "" — a milestone is never scheduled into another`, got)
	}
	// But the structural child axis still reports it — ChildCount stays honest.
	wantIDs(t, `Children("m1")`, v.Children("m1"), "mx")
}

// TestCycleTermination: invariant-violating data with a parent cycle still
// produces a deterministic View, and every accessor terminates — including the
// MilestoneOf walk up the structural chain.
func TestCycleTermination(t *testing.T) {
	inCycle := q("b", "task", "m1")
	inCycle.Parent = "a"
	v := Compute([]*nib.Nib{
		n("m1", "milestone", ""),
		n("a", "task", "b"),
		inCycle,
		q("c", "task", "m1"),
	})
	// The closure reaches a through b's structural subtree and terminates.
	wantIDs(t, `Members("m1")`, v.Members("m1"), "b", "c", "a")
	// a's walk enters the cycle and terminates at b's own assignment; b's own
	// assignment answers directly.
	if got := v.MilestoneOf("a"); got != "m1" {
		t.Errorf("MilestoneOf inside a parent cycle = %q, want %q", got, "m1")
	}
	// The cycle participants belong to no root (their parents resolve to real
	// nibs); the assigned c is a structural root.
	wantIDs(t, `Children("")`, v.Children(""), "m1", "c")
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

// TestResolvedMilestoneID pins THE definition of "directly assigned to a
// milestone" after the three-axis cutover: b's `milestone:` field resolved via
// the lookup, answered only when the target exists and is milestone-typed —
// the same dangling-link rule the resolved parent gives, so hand-edited
// garbage stays out of every view. The parent axis no longer confers
// membership at all.
func TestResolvedMilestoneID(t *testing.T) {
	m1 := n("m1", "milestone", "")
	e1 := q("e1", "epic", "m1")
	e2 := n("e2", "epic", "")
	t1 := q("t1", "task", "ghost") // dangling assignment
	t2 := q("t2", "task", "e2")    // assignment naming a non-milestone
	t3 := n("t3", "task", "m1")    // milestone PARENT, no assignment
	t4 := q("t4", "task", "m1")    // both axes: parent to an epic, assigned to m1
	t4.Parent = "e2"
	byID := map[string]*nib.Nib{"m1": m1, "e1": e1, "e2": e2, "t1": t1, "t2": t2, "t3": t3, "t4": t4}
	lookup := func(id string) *nib.Nib { return byID[id] }

	cases := []struct {
		b    *nib.Nib
		want string
	}{
		{e1, "m1"},
		{t1, ""}, // dangling milestone: resolves to no assignment
		{t2, ""}, // milestone: naming a non-milestone is no assignment
		{t3, ""}, // a milestone parent is the structural axis, not an assignment
		{t4, "m1"},
		{m1, ""}, // no assignment at all
	}
	for _, tc := range cases {
		if got := ResolvedMilestoneID(tc.b, lookup); got != tc.want {
			t.Errorf("ResolvedMilestoneID(%s) = %q, want %q", tc.b.ID, got, tc.want)
		}
	}
}
