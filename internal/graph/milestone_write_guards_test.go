package graph

import (
	"context"
	"strings"
	"testing"

	"github.com/99designs/gqlgen/graphql"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
)

// seedWriteGuardFixture builds one released milestone, one holding (deferred)
// milestone, one live milestone with an open queue, and work of both openness.
func seedWriteGuardFixture(t *testing.T, core *nibcore.Core) {
	t.Helper()
	for _, b := range []*nib.Nib{
		{ID: "wdone", Title: "Finished wave", Type: "milestone", Status: "completed"},
		{ID: "wdrop", Title: "Abandoned wave", Type: "milestone", Status: "scrapped"},
		{ID: "wpark", Title: "Parked wave", Type: "milestone", Status: "deferred"},
		{ID: "wlive", Title: "Live wave", Type: "milestone", Status: "in-progress"},
		{ID: "topen", Title: "Open work", Type: "task", Status: "todo"},
		{ID: "tshut", Title: "Finished work", Type: "task", Status: "completed"},
		{ID: "tqueued", Title: "Queued work", Type: "task", Status: "todo", Milestone: "wlive", MilestoneOrder: "a0"},
	} {
		mustCreate(t, core, b)
	}
}

func assignInputFor(milestoneID string) model.UpdateNibInput {
	return model.UpdateNibInput{Milestone: graphql.OmittableOf(&milestoneID)}
}

// TestUpdateNibRefusesAssigningOpenWorkToAReleasedMilestone is decision 1.5's
// ASSIGNMENT door.
//
// Closing a milestone over a live queue is refused on every client, but the same
// end-state — open work planned for a wave that has finished — was reachable from
// the other side, in one supported command: assign the work AFTER the milestone
// closes. `validateAndSetMilestone` checked the target's existence, its type,
// ValidateAxes and 1.2 exclusivity, and nothing about its status. Until this is
// refused, 1.5 is enforced against one verb and one door rather than as a model
// invariant.
func TestUpdateNibRefusesAssigningOpenWorkToAReleasedMilestone(t *testing.T) {
	ctx := context.Background()

	for _, target := range []string{"wdone", "wdrop"} {
		t.Run("refused for "+target, func(t *testing.T) {
			resolver, core := setupTestResolver(t)
			seedWriteGuardFixture(t, core)

			_, err := resolver.Mutation().UpdateNib(ctx, "topen", assignInputFor(target))
			if err == nil {
				t.Fatalf("assigning open work to %s succeeded; a released milestone takes no new work", target)
			}
			msg := err.Error()
			if !strings.Contains(msg, target) {
				t.Errorf("refusal should name the milestone, got: %v", err)
			}
			// The subject must be left alone: a refused assignment that still
			// rewrote the nib would be worse than the write it refused.
			got, gerr := resolver.Query().Nib(ctx, "topen")
			if gerr != nil {
				t.Fatalf("Nib(topen): %v", gerr)
			}
			if got.Milestone != "" {
				t.Errorf("refused assignment still wrote milestone = %q", got.Milestone)
			}
		})
	}

	// A HOLDING reason keeps its queue by decision 1.5, so a parked wave must go
	// on accepting work — it is coming back.
	t.Run("a deferred milestone still accepts open work", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedWriteGuardFixture(t, core)

		if _, err := resolver.Mutation().UpdateNib(ctx, "topen", assignInputFor("wpark")); err != nil {
			t.Fatalf("assigning to a deferred milestone: %v; a parked wave keeps its queue", err)
		}
	})

	// Retro-assigning finished work to a finished wave is how a record gets
	// written after the fact, and carries none of the harm: the work is closed,
	// so nothing is left planned for a wave that ended.
	t.Run("closed work may still be assigned to a released milestone", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedWriteGuardFixture(t, core)

		if _, err := resolver.Mutation().UpdateNib(ctx, "tshut", assignInputFor("wdone")); err != nil {
			t.Fatalf("assigning closed work to a released milestone: %v; the record must stay writable", err)
		}
	})

	t.Run("an open milestone accepts open work", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedWriteGuardFixture(t, core)

		if _, err := resolver.Mutation().UpdateNib(ctx, "topen", assignInputFor("wlive")); err != nil {
			t.Fatalf("assigning to a live milestone: %v", err)
		}
	})
}

// TestUpdateNibRefusesRetypingAMilestoneThatHoldsAQueue is decision 1.5's
// TYPE-FLIP door, and a defect in its own right.
//
// A milestone with a live queue could be retyped away from milestone-hood, which
// silently invalidates every assignment pointing at it — each member becomes an
// InvalidMilestoneTarget, conferring no membership while its `milestone:` field
// still reads back the now-typeless target. It also opened a three-call route to
// the state the close guard exists to refuse: retype to epic, close (the guard
// skips a non-milestone), retype back (the guard keys on the status CHANGING, and
// it did not change).
//
// Refusing at the retype closes that sequence at its first step, and needs
// nothing from internal/membership: the stored type is still `milestone` here, so
// View.DirectMembers answers on the assignment axis as it always does.
func TestUpdateNibRefusesRetypingAMilestoneThatHoldsAQueue(t *testing.T) {
	ctx := context.Background()
	epic := "epic"

	t.Run("refused while work is assigned", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedWriteGuardFixture(t, core)

		_, err := resolver.Mutation().UpdateNib(ctx, "wlive", model.UpdateNibInput{Type: &epic})
		if err == nil {
			t.Fatal("retyping a milestone that holds a queue succeeded; every assignment pointing at it would be invalidated")
		}
		if !strings.Contains(err.Error(), "tqueued") {
			t.Errorf("refusal should name the work that would be orphaned, got: %v", err)
		}
	})

	t.Run("the three-call flip cannot start", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedWriteGuardFixture(t, core)

		// Step 1 of the sequence. With it refused, steps 2 and 3 are unreachable.
		if _, err := resolver.Mutation().UpdateNib(ctx, "wlive", model.UpdateNibInput{Type: &epic}); err == nil {
			t.Fatal("step 1 of the type-flip route succeeded")
		}
		got, err := resolver.Query().Nib(ctx, "wlive")
		if err != nil {
			t.Fatalf("Nib(wlive): %v", err)
		}
		if got.Type != "milestone" || got.Status != "in-progress" {
			t.Errorf("refused retype still wrote: type=%q status=%q", got.Type, got.Status)
		}
	})

	t.Run("an empty milestone may be retyped", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedWriteGuardFixture(t, core)

		// wpark holds nothing; retyping it orphans no assignment.
		if _, err := resolver.Mutation().UpdateNib(ctx, "wpark", model.UpdateNibInput{Type: &epic}); err != nil {
			t.Fatalf("retyping an empty milestone: %v", err)
		}
	})

	t.Run("a queue of only CLOSED work still blocks the retype", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedWriteGuardFixture(t, core)
		// Close the one member: its assignment is still a live link, and retyping
		// would still invalidate it, so openness is not the test here — the close
		// gate's "open work" rule is about closing, not about retyping.
		done := "completed"
		if _, err := resolver.Mutation().UpdateNib(ctx, "tqueued", model.UpdateNibInput{Status: &done}); err != nil {
			t.Fatalf("closing the member: %v", err)
		}
		if _, err := resolver.Mutation().UpdateNib(ctx, "wlive", model.UpdateNibInput{Type: &epic}); err == nil {
			t.Fatal("retyping a milestone still holding assignments succeeded, even though they are closed")
		}
	})

	// The no-op trap a79672f already paid for: the web edit form sends `type` on
	// every save, so a title-only edit arrives carrying the current type.
	t.Run("a no-op type resend does not trip the guard", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedWriteGuardFixture(t, core)

		milestone, title := "milestone", "Renamed by the web form"
		if _, err := resolver.Mutation().UpdateNib(ctx, "wlive", model.UpdateNibInput{
			Type: &milestone, Title: &title,
		}); err != nil {
			t.Fatalf("a no-op type resend was refused: %v; the web form sends type on every save", err)
		}
	})
}
