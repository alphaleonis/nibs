package graph

import (
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/membership"
	"github.com/alphaleonis/nibs/internal/nib"
)

// nextFixture builds a reader plus a blocking checker that answers IsBlocked
// the way Core does — an unreleased blocker in blocked_by blocks — so the walk
// under test sees the same startability the `--ready` filter sees.
func nextFixture(nibs ...*nib.Nib) (NibReader, BlockingChecker) {
	reader := newOrdererReader(nibs...)
	blocked := map[string]bool{}
	for _, b := range nibs {
		for _, raw := range b.BlockedBy {
			blocker, err := reader.Get(raw)
			if err != nil {
				continue
			}
			if !reader.Config().StatusReleasesDependents(blocker.Status) {
				blocked[b.ID] = true
			}
		}
	}
	return reader, &stubBlockingChecker{blocked: blocked}
}

func pathIDs(res NextResult) string {
	ids := make([]string, len(res.Path))
	for i, b := range res.Path {
		ids[i] = b.ID
	}
	return strings.Join(ids, ">")
}

// TestActiveMilestoneIsDerived pins decision 1.4: the active milestone is the
// in-progress one that comes first in milestone order, derived per call and
// never stored.
func TestActiveMilestoneIsDerived(t *testing.T) {
	t.Run("earliest in-progress milestone wins", func(t *testing.T) {
		view := membership.Compute([]*nib.Nib{
			{ID: "ms3", Title: "Third", Type: "milestone", Status: "in-progress", Order: "c"},
			{ID: "ms1", Title: "First", Type: "milestone", Status: "completed", Order: "a"},
			{ID: "ms2", Title: "Second", Type: "milestone", Status: "in-progress", Order: "b"},
		})
		got := ActiveMilestone(view)
		if got == nil || got.ID != "ms2" {
			t.Fatalf("active milestone = %v, want ms2 (in-progress, earliest order)", got)
		}
	})

	t.Run("an open but not in-progress milestone is not active", func(t *testing.T) {
		for _, status := range []string{"todo", "draft", "deferred", "completed", "scrapped"} {
			view := membership.Compute([]*nib.Nib{
				{ID: "ms1", Title: "Only", Type: "milestone", Status: status, Order: "a"},
			})
			if got := ActiveMilestone(view); got != nil {
				t.Errorf("status %s: active milestone = %s, want none", status, got.ID)
			}
		}
	})

	t.Run("no milestones at all", func(t *testing.T) {
		view := membership.Compute([]*nib.Nib{{ID: "t1", Title: "Task", Status: "todo"}})
		if got := ActiveMilestone(view); got != nil {
			t.Errorf("active milestone = %s, want none", got.ID)
		}
	})

	t.Run("an unkeyed milestone sorts after a keyed one", func(t *testing.T) {
		view := membership.Compute([]*nib.Nib{
			{ID: "ms-unkeyed", Title: "Aaa", Type: "milestone", Status: "in-progress"},
			{ID: "ms-keyed", Title: "Zzz", Type: "milestone", Status: "in-progress", Order: "b"},
		})
		got := ActiveMilestone(view)
		if got == nil || got.ID != "ms-keyed" {
			t.Fatalf("active milestone = %v, want ms-keyed (a keyed milestone precedes an unkeyed one)", got)
		}
	})
}

// TestNextWalksTheQueueToTheFirstStartableLeaf pins the core walk (decision
// 2.4): queue order at the top, decomposition order inside a container, and
// the first startable leaf as the answer with its full provenance path.
func TestNextWalksTheQueueToTheFirstStartableLeaf(t *testing.T) {
	reader, blocking := nextFixture(
		&nib.Nib{ID: "ms1", Title: "Wave 1", Type: "milestone", Status: "in-progress", Order: "a"},
		&nib.Nib{ID: "e1", Title: "Epic One", Type: "epic", Status: "in-progress", Milestone: "ms1", MilestoneOrder: "a"},
		&nib.Nib{ID: "f1", Title: "Feature One", Type: "feature", Status: "in-progress", Parent: "e1", Order: "a"},
		&nib.Nib{ID: "t1", Title: "Done task", Status: "completed", Parent: "f1", Order: "a"},
		&nib.Nib{ID: "t2", Title: "Live task", Status: "in-progress", Parent: "f1", Order: "b"},
		&nib.Nib{ID: "t3", Title: "The answer", Status: "todo", Parent: "f1", Order: "c"},
		&nib.Nib{ID: "e2", Title: "Epic Two", Type: "epic", Status: "todo", Milestone: "ms1", MilestoneOrder: "b"},
	)

	res := Next(reader, blocking)
	if res.Action == nil || res.Action.ID != "t3" {
		t.Fatalf("action = %v, want t3", res.Action)
	}
	if res.Milestone == nil || res.Milestone.ID != "ms1" {
		t.Fatalf("milestone = %v, want ms1", res.Milestone)
	}
	if got, want := pathIDs(res), "e1>f1>t3"; got != want {
		t.Errorf("path = %s, want %s (queue entry, then the descent)", got, want)
	}
	if res.Position != 1 {
		t.Errorf("queue position = %d, want 1", res.Position)
	}
	if res.NoAnswerReason != "" || res.FallbackReason != "" {
		t.Errorf("an answered walk carries no reason, got fallback=%q noanswer=%q", res.FallbackReason, res.NoAnswerReason)
	}
}

// TestNextSkipsAQueueEntryWithNothingStartable pins the acceptance case: the
// queue HEAD is passed over when nothing under it can be started, and the walk
// carries on to the next entry rather than reporting an empty answer.
func TestNextSkipsAQueueEntryWithNothingStartable(t *testing.T) {
	reader, blocking := nextFixture(
		&nib.Nib{ID: "ms1", Title: "Wave 1", Type: "milestone", Status: "in-progress", Order: "a"},
		&nib.Nib{ID: "e1", Title: "Epic One", Type: "epic", Status: "draft", Milestone: "ms1", MilestoneOrder: "a"},
		&nib.Nib{ID: "t1", Title: "Draft task", Status: "draft", Parent: "e1", Order: "a"},
		&nib.Nib{ID: "e2", Title: "Epic Two", Type: "epic", Status: "todo", Milestone: "ms1", MilestoneOrder: "b"},
		&nib.Nib{ID: "t2", Title: "The answer", Status: "todo", Parent: "e2", Order: "a"},
	)

	res := Next(reader, blocking)
	if res.Action == nil || res.Action.ID != "t2" {
		t.Fatalf("action = %v, want t2 (the head entry yields nothing startable)", res.Action)
	}
	if got, want := pathIDs(res), "e2>t2"; got != want {
		t.Errorf("path = %s, want %s", got, want)
	}
	if res.Position != 2 {
		t.Errorf("queue position = %d, want 2", res.Position)
	}
}

// TestNextOffersAContainerWhoseChildrenAreAllClosed pins the second half of
// decision 2.4: a container with nothing open left below it IS the action, so
// the walk stops there instead of stepping over it.
func TestNextOffersAContainerWhoseChildrenAreAllClosed(t *testing.T) {
	reader, blocking := nextFixture(
		&nib.Nib{ID: "ms1", Title: "Wave 1", Type: "milestone", Status: "in-progress", Order: "a"},
		&nib.Nib{ID: "e1", Title: "Epic One", Type: "epic", Status: "todo", Milestone: "ms1", MilestoneOrder: "a"},
		&nib.Nib{ID: "t1", Title: "Done", Status: "completed", Parent: "e1", Order: "a"},
		&nib.Nib{ID: "t2", Title: "Dropped", Status: "scrapped", Parent: "e1", Order: "b"},
		&nib.Nib{ID: "t3", Title: "Parked", Status: "deferred", Parent: "e1", Order: "c"},
		&nib.Nib{ID: "e2", Title: "Epic Two", Type: "epic", Status: "todo", Milestone: "ms1", MilestoneOrder: "b"},
		&nib.Nib{ID: "t4", Title: "Later", Status: "todo", Parent: "e2", Order: "a"},
	)

	res := Next(reader, blocking)
	if res.Action == nil || res.Action.ID != "e1" {
		t.Fatalf("action = %v, want e1 (all its children are closed, so the container is the action)", res.Action)
	}
	if got, want := pathIDs(res), "e1"; got != want {
		t.Errorf("path = %s, want %s", got, want)
	}
}

// TestNextSkipsQueueInversions pins decision 2.3 through the ONE definition
// (QueueInversionsInvolving): a queue entry sitting ahead of a blocker that is
// itself in the queue is passed over with its whole subtree, and the skip is
// reported rather than silent.
func TestNextSkipsQueueInversions(t *testing.T) {
	build := func(blockerStatus string) (NibReader, BlockingChecker) {
		return nextFixture(
			&nib.Nib{ID: "ms1", Title: "Wave 1", Type: "milestone", Status: "in-progress", Order: "a"},
			// e1 comes first in the queue but is blocked by e2, which comes second.
			&nib.Nib{ID: "e1", Title: "Epic One", Type: "epic", Status: "todo", Milestone: "ms1", MilestoneOrder: "a", BlockedBy: []string{"e2"}},
			&nib.Nib{ID: "t1", Title: "Under the inverted entry", Status: "todo", Parent: "e1", Order: "a"},
			&nib.Nib{ID: "e2", Title: "Epic Two", Type: "epic", Status: blockerStatus, Milestone: "ms1", MilestoneOrder: "b"},
			&nib.Nib{ID: "t2", Title: "Under the blocker", Status: "todo", Parent: "e2", Order: "a"},
		)
	}

	t.Run("an inverted entry is passed over with its subtree", func(t *testing.T) {
		reader, blocking := build("todo")
		res := Next(reader, blocking)
		if res.Action == nil || res.Action.ID != "t2" {
			t.Fatalf("action = %v, want t2 — t1 sits under an inverted queue entry", res.Action)
		}
		if res.Tally.Inverted != 1 {
			t.Errorf("inverted tally = %d, want 1", res.Tally.Inverted)
		}
		if len(res.Inversions) != 1 || res.Inversions[0].Ahead.ID != "e1" || res.Inversions[0].Blocker.ID != "e2" {
			t.Errorf("inversions = %v, want the single (e1, e2) pair", res.Inversions)
		}
	})

	t.Run("a released blocker is no inversion, so the entry is walked", func(t *testing.T) {
		reader, blocking := build("completed")
		res := Next(reader, blocking)
		if res.Action == nil || res.Action.ID != "t1" {
			t.Fatalf("action = %v, want t1 — a completed blocker inverts nothing", res.Action)
		}
		if res.Tally.Inverted != 0 {
			t.Errorf("inverted tally = %d, want 0", res.Tally.Inverted)
		}
	})
}

// TestNextRespectsBlockersAndStatus pins that startability is the `--ready`
// pair — a startable status AND no active blocker — with a deferred blocker
// still blocking.
func TestNextRespectsBlockersAndStatus(t *testing.T) {
	for _, tc := range []struct {
		name          string
		blockerStatus string
		wantAction    string
	}{
		{"an open blocker withholds the leaf", "todo", "t2"},
		{"a deferred blocker still blocks", "deferred", "t2"},
		{"a completed blocker releases the leaf", "completed", "t1"},
		{"a scrapped blocker releases the leaf", "scrapped", "t1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader, blocking := nextFixture(
				&nib.Nib{ID: "ms1", Title: "Wave 1", Type: "milestone", Status: "in-progress", Order: "a"},
				&nib.Nib{ID: "e1", Title: "Epic One", Type: "epic", Status: "todo", Milestone: "ms1", MilestoneOrder: "a"},
				&nib.Nib{ID: "t1", Title: "Blocked leaf", Status: "todo", Parent: "e1", Order: "a", BlockedBy: []string{"dep"}},
				&nib.Nib{ID: "t2", Title: "Free leaf", Status: "todo", Parent: "e1", Order: "b"},
				// The blocker is outside every queue, so no inversion can fire.
				&nib.Nib{ID: "dep", Title: "Dependency", Status: tc.blockerStatus},
			)
			res := Next(reader, blocking)
			if res.Action == nil || res.Action.ID != tc.wantAction {
				t.Fatalf("action = %v, want %s", res.Action, tc.wantAction)
			}
		})
	}
}

// TestNextDegradesHonestly pins the no-answer shapes: each names its situation
// with a reason code rather than guessing, and only the two no-active-milestone
// shapes fall back to the store-wide walk.
func TestNextDegradesHonestly(t *testing.T) {
	t.Run("no milestones at all falls back and says so", func(t *testing.T) {
		reader, blocking := nextFixture(
			&nib.Nib{ID: "t1", Title: "Root work", Status: "todo", Order: "a"},
			&nib.Nib{ID: "t2", Title: "More work", Status: "todo", Order: "b"},
		)
		res := Next(reader, blocking)
		if res.FallbackReason != NextReasonNoMilestones {
			t.Errorf("fallback reason = %q, want %q", res.FallbackReason, NextReasonNoMilestones)
		}
		if res.Action == nil || res.Action.ID != "t1" {
			t.Fatalf("action = %v, want t1 (first startable in tree order)", res.Action)
		}
		if res.Milestone != nil {
			t.Errorf("milestone = %s, want none", res.Milestone.ID)
		}
	})

	t.Run("milestones exist but none is in progress", func(t *testing.T) {
		reader, blocking := nextFixture(
			&nib.Nib{ID: "ms1", Title: "Wave 1", Type: "milestone", Status: "todo", Order: "a"},
			&nib.Nib{ID: "e1", Title: "Epic One", Type: "epic", Status: "todo", Milestone: "ms1", MilestoneOrder: "a"},
			&nib.Nib{ID: "t1", Title: "Planned work", Status: "todo", Parent: "e1", Order: "a"},
		)
		res := Next(reader, blocking)
		if res.FallbackReason != NextReasonNoActiveMilestone {
			t.Errorf("fallback reason = %q, want %q", res.FallbackReason, NextReasonNoActiveMilestone)
		}
		if res.Action == nil || res.Action.ID != "t1" {
			t.Fatalf("action = %v, want t1 via the store-wide walk", res.Action)
		}
	})

	t.Run("an active milestone with an empty queue does not fall back", func(t *testing.T) {
		reader, blocking := nextFixture(
			&nib.Nib{ID: "ms1", Title: "Wave 1", Type: "milestone", Status: "in-progress", Order: "a"},
			&nib.Nib{ID: "t1", Title: "Unplanned work", Status: "todo", Order: "a"},
		)
		res := Next(reader, blocking)
		if res.Action != nil {
			t.Fatalf("action = %s, want none — a plan exists, so next speaks only for it", res.Action.ID)
		}
		if res.NoAnswerReason != NextReasonEmptyQueue {
			t.Errorf("no-answer reason = %q, want %q", res.NoAnswerReason, NextReasonEmptyQueue)
		}
		if res.FallbackReason != "" {
			t.Errorf("fallback reason = %q, want none", res.FallbackReason)
		}
		if res.Milestone == nil || res.Milestone.ID != "ms1" {
			t.Errorf("milestone = %v, want ms1 named even with nothing to offer", res.Milestone)
		}
	})

	t.Run("a queue with nothing startable names why", func(t *testing.T) {
		reader, blocking := nextFixture(
			&nib.Nib{ID: "ms1", Title: "Wave 1", Type: "milestone", Status: "in-progress", Order: "a"},
			&nib.Nib{ID: "e1", Title: "Epic One", Type: "epic", Status: "in-progress", Milestone: "ms1", MilestoneOrder: "a"},
			&nib.Nib{ID: "t1", Title: "Closed", Status: "completed", Parent: "e1", Order: "a"},
			&nib.Nib{ID: "t2", Title: "Blocked", Status: "todo", Parent: "e1", Order: "b", BlockedBy: []string{"dep"}},
			&nib.Nib{ID: "t3", Title: "Draft", Status: "draft", Parent: "e1", Order: "c"},
			&nib.Nib{ID: "dep", Title: "Dependency", Status: "todo"},
		)
		res := Next(reader, blocking)
		if res.Action != nil {
			t.Fatalf("action = %s, want none", res.Action.ID)
		}
		if res.NoAnswerReason != NextReasonNothingStartable {
			t.Errorf("no-answer reason = %q, want %q", res.NoAnswerReason, NextReasonNothingStartable)
		}
		if res.FallbackReason != "" {
			t.Errorf("fallback reason = %q, want none — an active milestone is not routed around", res.FallbackReason)
		}
		want := NextTally{Closed: 1, Blocked: 1, Open: 1}
		if res.Tally != want {
			t.Errorf("tally = %+v, want %+v", res.Tally, want)
		}
	})

	t.Run("the fallback walk can come up empty too", func(t *testing.T) {
		reader, blocking := nextFixture(
			&nib.Nib{ID: "t1", Title: "Closed", Status: "completed", Order: "a"},
		)
		res := Next(reader, blocking)
		if res.Action != nil {
			t.Fatalf("action = %s, want none", res.Action.ID)
		}
		if res.FallbackReason != NextReasonNoMilestones {
			t.Errorf("fallback reason = %q, want %q", res.FallbackReason, NextReasonNoMilestones)
		}
		if res.NoAnswerReason != NextReasonNothingStartable {
			t.Errorf("no-answer reason = %q, want %q", res.NoAnswerReason, NextReasonNothingStartable)
		}
	})
}

// TestNextTerminatesOnACyclicParentChain pins that illegal data cannot hang the
// walk: membership resolves a parent cycle into an adjacency the descent would
// otherwise follow forever.
func TestNextTerminatesOnACyclicParentChain(t *testing.T) {
	reader, blocking := nextFixture(
		&nib.Nib{ID: "ms1", Title: "Wave 1", Type: "milestone", Status: "in-progress", Order: "a"},
		&nib.Nib{ID: "a", Title: "A", Type: "epic", Status: "todo", Parent: "b", Milestone: "ms1", MilestoneOrder: "a"},
		&nib.Nib{ID: "b", Title: "B", Type: "feature", Status: "todo", Parent: "a", Order: "a"},
	)
	res := Next(reader, blocking)
	if res.Action == nil || res.Action.ID != "b" {
		t.Fatalf("action = %v, want b — the cycle must terminate at the revisit", res.Action)
	}
}

// TestNextReadsAnUnkeyedQueueWithoutInventingKeys pins the read-only
// discipline: `next` reads the queue the way QueueInversionsInvolving does —
// enumerate and sort — so an unkeyed member sorts last by title and is never
// handed a milestone_order as a side effect of asking a question. (Next takes
// no writer at all, so the mutation this guards is a signature change; the
// on-disk half is TestNextLeavesTheStoreUnchanged in package cmd.)
func TestNextReadsAnUnkeyedQueueWithoutInventingKeys(t *testing.T) {
	unkeyed := &nib.Nib{ID: "e2", Title: "Aaa unkeyed", Type: "epic", Status: "todo", Milestone: "ms1"}
	reader, blocking := nextFixture(
		&nib.Nib{ID: "ms1", Title: "Wave 1", Type: "milestone", Status: "in-progress", Order: "a"},
		&nib.Nib{ID: "e1", Title: "Zzz keyed", Type: "epic", Status: "todo", Milestone: "ms1", MilestoneOrder: "a"},
		unkeyed,
	)
	res := Next(reader, blocking)
	if res.Action == nil || res.Action.ID != "e1" {
		t.Fatalf("action = %v, want e1 — a keyed member precedes an unkeyed one whatever the titles say", res.Action)
	}
	if unkeyed.MilestoneOrder != "" {
		t.Errorf("next put milestone_order = %q on an unkeyed member; a read must not write", unkeyed.MilestoneOrder)
	}
}

// TestNextOffersAContainerOnlyWhenNothingOpenIsBelowIt pins the leaf test of
// decision 2.4 against the walk's own bookkeeping: "nothing open below" is a
// property of the container's children, not of which of them the walk happens
// to have entered already. The visited set exists to make the descent total
// over a cyclic parent chain, and it must not double as evidence that a
// container is finished.
//
// The last shape is the one that needs illegal data to reach: a nib and its
// ancestor BOTH carrying an assignment puts them both in the queue, so the
// child is walked (and marked visited) as its own queue entry before the walk
// reaches its parent. Mutations refuse that shape (resolver.go's assignment
// rule), which is why it is built from nib values directly.
func TestNextOffersAContainerOnlyWhenNothingOpenIsBelowIt(t *testing.T) {
	ms := func() *nib.Nib {
		return &nib.Nib{ID: "ms1", Title: "Wave 1", Type: "milestone", Status: "in-progress", Order: "a"}
	}
	for _, tc := range []struct {
		name       string
		nibs       []*nib.Nib
		wantAction string
		wantReason NextReason
		wantTally  NextTally
	}{
		{
			name: "a genuine leaf is the action",
			nibs: []*nib.Nib{
				ms(),
				{ID: "t1", Title: "Leaf", Status: "todo", Milestone: "ms1", MilestoneOrder: "a"},
			},
			wantAction: "t1",
		},
		{
			name: "a container whose children are all closed is itself the action",
			nibs: []*nib.Nib{
				ms(),
				{ID: "e1", Title: "Epic", Type: "epic", Status: "todo", Milestone: "ms1", MilestoneOrder: "a"},
				{ID: "t1", Title: "Done", Status: "completed", Parent: "e1", Order: "a"},
			},
			wantAction: "e1",
			wantTally:  NextTally{Closed: 1},
		},
		{
			name: "a container with one open child is descended, never offered",
			nibs: []*nib.Nib{
				ms(),
				{ID: "e1", Title: "Epic", Type: "epic", Status: "todo", Milestone: "ms1", MilestoneOrder: "a"},
				{ID: "t1", Title: "Open child", Status: "todo", Parent: "e1", Order: "a"},
			},
			wantAction: "t1",
		},
		{
			name: "a container whose one open child was already visited is not offered either",
			nibs: []*nib.Nib{
				ms(),
				// t1 is walked first as its own queue entry, which marks it
				// visited; e1 follows and must still see an open child.
				{ID: "t1", Title: "Drafting", Status: "draft", Parent: "e1", Milestone: "ms1", MilestoneOrder: "a"},
				{ID: "e1", Title: "Epic", Type: "epic", Status: "todo", Milestone: "ms1", MilestoneOrder: "b"},
			},
			wantReason: NextReasonNothingStartable,
			wantTally:  NextTally{Open: 1},
		},
		{
			name: "an already-visited child that is BLOCKED still keeps its parent off the answer",
			nibs: []*nib.Nib{
				ms(),
				{ID: "t1", Title: "Blocked", Status: "todo", Parent: "e1", Milestone: "ms1", MilestoneOrder: "a", BlockedBy: []string{"dep"}},
				{ID: "e1", Title: "Epic", Type: "epic", Status: "todo", Milestone: "ms1", MilestoneOrder: "b"},
				{ID: "dep", Title: "Dependency", Status: "todo"},
			},
			wantReason: NextReasonNothingStartable,
			wantTally:  NextTally{Blocked: 1},
		},
		{
			name: "a cyclic parent chain terminates and still answers",
			nibs: []*nib.Nib{
				ms(),
				{ID: "a", Title: "A", Type: "epic", Status: "todo", Parent: "b", Milestone: "ms1", MilestoneOrder: "a"},
				{ID: "b", Title: "B", Type: "feature", Status: "todo", Parent: "a", Order: "a"},
			},
			wantAction: "b",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader, blocking := nextFixture(tc.nibs...)
			res := Next(reader, blocking)
			switch {
			case tc.wantAction == "" && res.Action != nil:
				t.Fatalf("action = %s, want none", res.Action.ID)
			case tc.wantAction != "" && (res.Action == nil || res.Action.ID != tc.wantAction):
				t.Fatalf("action = %v, want %s", res.Action, tc.wantAction)
			}
			if res.NoAnswerReason != tc.wantReason {
				t.Errorf("no-answer reason = %q, want %q", res.NoAnswerReason, tc.wantReason)
			}
			if res.Tally != tc.wantTally {
				t.Errorf("tally = %+v, want %+v", res.Tally, tc.wantTally)
			}
		})
	}
}
