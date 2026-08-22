package graph

import (
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
)

// TestQueueInversionsInvolving pins the one definition of a queue inversion:
// same queue, earlier position, and a blocker whose status still blocks — on
// either side of the subject.
func TestQueueInversionsInvolving(t *testing.T) {
	build := func(nibs ...*nib.Nib) NibReader {
		return newOrdererReader(append([]*nib.Nib{
			{ID: "ms1", Title: "Waypoint", Type: "milestone", Status: "todo"},
			{ID: "ms2", Title: "Other", Type: "milestone", Status: "todo"},
		}, nibs...)...)
	}
	pairs := func(inv []QueueInversion) [][2]string {
		out := make([][2]string, len(inv))
		for i, x := range inv {
			out[i] = [2]string{x.Ahead.ID, x.Blocker.ID}
		}
		return out
	}
	want := func(t *testing.T, got []QueueInversion, expect ...[2]string) {
		t.Helper()
		p := pairs(got)
		if len(p) != len(expect) {
			t.Fatalf("inversions = %v, want %v", p, expect)
		}
		for i := range expect {
			if p[i] != expect[i] {
				t.Errorf("inversion[%d] = %v, want %v", i, p[i], expect[i])
			}
		}
		for _, x := range got {
			if x.Milestone != "ms1" {
				t.Errorf("inversion milestone = %q, want ms1", x.Milestone)
			}
		}
	}

	t.Run("subject ahead of its open blocker", func(t *testing.T) {
		r := build(
			&nib.Nib{ID: "a", Title: "A", Status: "todo", Milestone: "ms1", MilestoneOrder: "a0", BlockedBy: []string{"b"}},
			&nib.Nib{ID: "b", Title: "B", Status: "todo", Milestone: "ms1", MilestoneOrder: "b0"},
		)
		want(t, QueueInversionsInvolving(r, "a"), [2]string{"a", "b"})
		// The same pair is visible from the blocker's side.
		want(t, QueueInversionsInvolving(r, "b"), [2]string{"a", "b"})
	})

	t.Run("subject behind its blocker is no inversion", func(t *testing.T) {
		r := build(
			&nib.Nib{ID: "b", Title: "B", Status: "todo", Milestone: "ms1", MilestoneOrder: "a0"},
			&nib.Nib{ID: "a", Title: "A", Status: "todo", Milestone: "ms1", MilestoneOrder: "b0", BlockedBy: []string{"b"}},
		)
		want(t, QueueInversionsInvolving(r, "a"))
	})

	t.Run("a released blocker does not invert, a deferred one does", func(t *testing.T) {
		for _, tc := range []struct {
			status string
			n      int
		}{{"completed", 0}, {"scrapped", 0}, {"deferred", 1}, {"in-progress", 1}} {
			r := build(
				&nib.Nib{ID: "a", Title: "A", Status: "todo", Milestone: "ms1", MilestoneOrder: "a0", BlockedBy: []string{"b"}},
				&nib.Nib{ID: "b", Title: "B", Status: tc.status, Milestone: "ms1", MilestoneOrder: "b0"},
			)
			if got := len(QueueInversionsInvolving(r, "a")); got != tc.n {
				t.Errorf("blocker status %s: inversions = %d, want %d", tc.status, got, tc.n)
			}
		}
	})

	t.Run("a blocker in another queue or no queue is out of scope", func(t *testing.T) {
		r := build(
			&nib.Nib{ID: "a", Title: "A", Status: "todo", Milestone: "ms1", MilestoneOrder: "a0", BlockedBy: []string{"b", "c", "gone"}},
			&nib.Nib{ID: "b", Title: "B", Status: "todo", Milestone: "ms2", MilestoneOrder: "b0"},
			&nib.Nib{ID: "c", Title: "C", Status: "todo"},
		)
		want(t, QueueInversionsInvolving(r, "a"))
	})

	t.Run("an unassigned subject has no inversions", func(t *testing.T) {
		r := build(
			&nib.Nib{ID: "a", Title: "A", Status: "todo", BlockedBy: []string{"b"}},
			&nib.Nib{ID: "b", Title: "B", Status: "todo", Milestone: "ms1", MilestoneOrder: "b0"},
		)
		want(t, QueueInversionsInvolving(r, "a"))
		if got := QueueInversionsInvolving(r, "no-such"); got != nil {
			t.Errorf("unknown id: inversions = %v, want nil", pairs(got))
		}
	})

	t.Run("several pairs are all reported, in queue order", func(t *testing.T) {
		r := build(
			&nib.Nib{ID: "x", Title: "X", Status: "todo", Milestone: "ms1", MilestoneOrder: "a0", BlockedBy: []string{"s"}},
			&nib.Nib{ID: "y", Title: "Y", Status: "todo", Milestone: "ms1", MilestoneOrder: "b0", BlockedBy: []string{"s"}},
			&nib.Nib{ID: "s", Title: "Subject", Status: "todo", Milestone: "ms1", MilestoneOrder: "c0", BlockedBy: []string{"z"}},
			&nib.Nib{ID: "z", Title: "Z", Status: "todo", Milestone: "ms1", MilestoneOrder: "d0"},
		)
		want(t, QueueInversionsInvolving(r, "s"), [2]string{"x", "s"}, [2]string{"y", "s"}, [2]string{"s", "z"})
	})

	t.Run("no key is written while reading", func(t *testing.T) {
		reader := newOrdererReader(
			&nib.Nib{ID: "ms1", Title: "Waypoint", Type: "milestone", Status: "todo"},
			&nib.Nib{ID: "a", Title: "A", Status: "todo", Milestone: "ms1", BlockedBy: []string{"b"}},
			&nib.Nib{ID: "b", Title: "B", Status: "todo", Milestone: "ms1"},
		)
		_ = QueueInversionsInvolving(reader, "a")
		for _, id := range []string{"a", "b"} {
			if b, _ := reader.Get(id); b.MilestoneOrder != "" {
				t.Errorf("%s gained queue key %q from a read", id, b.MilestoneOrder)
			}
		}
	})
}
