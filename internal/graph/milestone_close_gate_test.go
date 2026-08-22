package graph

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/99designs/gqlgen/graphql"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
)

// seedCloseGateFixture builds one milestone with an open queue: msq holds qa
// (todo) and qb (in-progress), plus qc which is already completed. msempty is a
// milestone with no queue at all, and msdone one whose whole queue is closed.
func seedCloseGateFixture(t *testing.T, core *nibcore.Core) {
	t.Helper()
	for _, b := range []*nib.Nib{
		{ID: "msq", Title: "Queued milestone", Type: "milestone", Status: "in-progress"},
		{ID: "msempty", Title: "Empty milestone", Type: "milestone", Status: "in-progress"},
		{ID: "msdone", Title: "Finished milestone", Type: "milestone", Status: "in-progress"},
		{ID: "qa", Title: "Queued A", Type: "task", Status: "todo", Milestone: "msq", MilestoneOrder: "a0"},
		{ID: "qb", Title: "Queued B", Type: "task", Status: "in-progress", Milestone: "msq", MilestoneOrder: "b0"},
		{ID: "qc", Title: "Queued C", Type: "task", Status: "completed", Milestone: "msq", MilestoneOrder: "c0"},
		{ID: "da", Title: "Done A", Type: "task", Status: "completed", Milestone: "msdone", MilestoneOrder: "a0"},
		{ID: "db", Title: "Dropped B", Type: "task", Status: "scrapped", Milestone: "msdone", MilestoneOrder: "b0"},
		{ID: "dc", Title: "Parked C", Type: "task", Status: "deferred", Milestone: "msdone", MilestoneOrder: "c0"},
	} {
		mustCreate(t, core, b)
	}
}

func statusInput(status string) model.UpdateNibInput {
	return model.UpdateNibInput{Status: &status}
}

func mustStatus(t *testing.T, resolver *Resolver, id string) string {
	t.Helper()
	b, err := resolver.Query().Nib(context.Background(), id)
	if err != nil {
		t.Fatalf("Nib(%s): %v", id, err)
	}
	return b.Status
}

// TestUpdateNibMilestoneCloseGate pins decision 1.5 as a MODEL invariant rather
// than a `nibs close` rule: every client reaching updateNib — the web status
// dropdown, the TUI status picker, `nibs graphql` — is refused the same close
// the CLI verb refuses, and is refused NOTHING ELSE.
func TestUpdateNibMilestoneCloseGate(t *testing.T) {
	ctx := context.Background()

	t.Run("refuses a releasing status while open work is queued", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedCloseGateFixture(t, core)

		_, err := resolver.Mutation().UpdateNib(ctx, "msq", statusInput("completed"))
		if err == nil {
			t.Fatal("UpdateNib(status=completed) succeeded; want a refusal while qa/qb are still queued")
		}
		var queueErr *MilestoneQueueOpenError
		if !errors.As(err, &queueErr) {
			t.Fatalf("error is %T (%v), want *MilestoneQueueOpenError", err, err)
		}
		if got, want := queueErr.Open, []string{"qa", "qb"}; !equalIDs(got, want) {
			t.Errorf("Open = %v, want %v (queue order, open members only)", got, want)
		}
		if queueErr.MilestoneID != "msq" || queueErr.Status != "completed" {
			t.Errorf("MilestoneID/Status = %q/%q, want msq/completed", queueErr.MilestoneID, queueErr.Status)
		}
		// The backstop is reached by clients that have no CLI flags, so its
		// message must name the capability rather than a flag spelling.
		msg := err.Error()
		for _, flag := range []string{"--move-open-to", "--unassign-open", "--as"} {
			if strings.Contains(msg, flag) {
				t.Errorf("message names the CLI flag %s; it must name the capability: %s", flag, msg)
			}
		}
		if !strings.Contains(msg, "qa") || !strings.Contains(msg, "qb") {
			t.Errorf("message does not name the open queue entries: %s", msg)
		}
		if got := mustStatus(t, resolver, "msq"); got != "in-progress" {
			t.Errorf("msq status = %q after the refusal, want it left at in-progress", got)
		}
	})

	t.Run("scrapped is refused too — the rule is the role, not a status name", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedCloseGateFixture(t, core)

		if _, err := resolver.Mutation().UpdateNib(ctx, "msq", statusInput("scrapped")); err == nil {
			t.Fatal("UpdateNib(status=scrapped) succeeded; scrapped releases dependents too")
		}
	})

	t.Run("a holding status keeps the queue and is accepted", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedCloseGateFixture(t, core)

		if _, err := resolver.Mutation().UpdateNib(ctx, "msq", statusInput("deferred")); err != nil {
			t.Fatalf("UpdateNib(status=deferred): %v; a parked milestone keeps its queue", err)
		}
		if got := mustStatus(t, resolver, "msq"); got != "deferred" {
			t.Errorf("msq status = %q, want deferred", got)
		}
		for _, id := range []string{"qa", "qb"} {
			b, err := resolver.Query().Nib(ctx, id)
			if err != nil {
				t.Fatalf("Nib(%s): %v", id, err)
			}
			if b.Milestone != "msq" {
				t.Errorf("%s milestone = %q, want msq — parking must not drain the queue", id, b.Milestone)
			}
		}
	})

	t.Run("an empty queue closes", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedCloseGateFixture(t, core)

		if _, err := resolver.Mutation().UpdateNib(ctx, "msempty", statusInput("completed")); err != nil {
			t.Fatalf("UpdateNib(msempty, status=completed): %v", err)
		}
	})

	t.Run("a queue whose members are all closed closes", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedCloseGateFixture(t, core)

		// dc is deferred: closed as a MEMBER, so it does not hold msdone open.
		if _, err := resolver.Mutation().UpdateNib(ctx, "msdone", statusInput("completed")); err != nil {
			t.Fatalf("UpdateNib(msdone, status=completed): %v", err)
		}
	})

	t.Run("clearing the queue member by member unblocks the close", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedCloseGateFixture(t, core)

		for _, id := range []string{"qa", "qb"} {
			empty := ""
			if _, err := resolver.Mutation().UpdateNib(ctx, id, model.UpdateNibInput{Milestone: graphql.OmittableOf(&empty)}); err != nil {
				t.Fatalf("UpdateNib(%s, milestone=\"\"): %v", id, err)
			}
		}
		if _, err := resolver.Mutation().UpdateNib(ctx, "msq", statusInput("completed")); err != nil {
			t.Fatalf("UpdateNib(msq, status=completed) after clearing the queue: %v", err)
		}
	})

	t.Run("an unrelated edit on an already-closed milestone is not refused", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedCloseGateFixture(t, core)
		// Reach the offending state the only way that exists: by hand.
		writeStatusOnDisk(t, core, "msq", "completed")

		title := "Renamed after the fact"
		if _, err := resolver.Mutation().UpdateNib(ctx, "msq", model.UpdateNibInput{Title: &title}); err != nil {
			t.Fatalf("UpdateNib(title): %v; a standing offense must stay editable", err)
		}
	})

	// The status-resent-unchanged shape — which is what the web edit form actually
	// sends on every save, and so the case that decides whether a standing offense
	// is editable in practice — is covered by
	// TestUpdateNibKeepsClosedMilestoneEditable below, together with its
	// complement (a real transition between two releasing reasons, still refused).
	// The subtest above deliberately omits status, so it alone does not settle it.

	t.Run("only DIRECT assignees count, not the transitive closure", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		mustCreate(t, core, &nib.Nib{ID: "msx", Title: "Milestone", Type: "milestone", Status: "in-progress"})
		mustCreate(t, core, &nib.Nib{ID: "epx", Title: "Epic", Type: "epic", Status: "completed", Milestone: "msx", MilestoneOrder: "a0"})
		mustCreate(t, core, &nib.Nib{ID: "tx", Title: "Open leaf", Type: "task", Status: "todo", Parent: "epx"})

		// tx is open and belongs to msx transitively, but it carries no
		// assignment of its own — nothing the remedy could act on.
		if _, err := resolver.Mutation().UpdateNib(ctx, "msx", statusInput("completed")); err != nil {
			t.Fatalf("UpdateNib(msx, status=completed): %v; only direct assignees gate the close", err)
		}
	})

	t.Run("a non-milestone container with open children is not gated", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		mustCreate(t, core, &nib.Nib{ID: "epy", Title: "Epic", Type: "epic", Status: "in-progress"})
		mustCreate(t, core, &nib.Nib{ID: "ty", Title: "Open child", Type: "task", Status: "todo", Parent: "epy"})

		if _, err := resolver.Mutation().UpdateNib(ctx, "epy", statusInput("completed")); err != nil {
			t.Fatalf("UpdateNib(epy, status=completed): %v; the gate is a milestone rule", err)
		}
	})
}

// writeStatusOnDisk rewrites one nib's status straight into its file, bypassing
// Core, and reloads. It is the shortest way to stand a test on the offending
// state, not the only way that state arises — the assignment door reaches it
// through ordinary writes (nibs-l5df).
func writeStatusOnDisk(t *testing.T, core *nibcore.Core, id, status string) {
	t.Helper()
	b, err := core.Get(id)
	if err != nil {
		t.Fatalf("Get(%s): %v", id, err)
	}
	edited := b.Clone()
	edited.Status = status
	content, err := edited.Render()
	if err != nil {
		t.Fatalf("render %s: %v", id, err)
	}
	if err := os.WriteFile(filepath.Join(core.Root(), edited.Path), content, 0644); err != nil {
		t.Fatalf("write %s: %v", id, err)
	}
	if err := core.Load(); err != nil {
		t.Fatalf("reload: %v", err)
	}
}

func equalIDs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// seedClosedMilestoneWithLiveQueue builds the state the CLOSING transition can no
// longer produce: a milestone already carrying a releasing status while an OPEN
// nib is assigned to its queue. Closing it is what the guard refuses; arriving
// here by the other door is not refused at all — assigning to an already-closed
// milestone checks the target's type and never its status (nibs-l5df) — so this
// is ordinary reachable state, not a hand-edit curiosity.
func seedClosedMilestoneWithLiveQueue(t *testing.T, core *nibcore.Core) {
	t.Helper()
	for _, b := range []*nib.Nib{
		{ID: "msshut", Title: "Shut milestone", Type: "milestone", Status: "completed"},
		{ID: "sa", Title: "Still open", Type: "task", Status: "todo", Milestone: "msshut", MilestoneOrder: "a0"},
	} {
		mustCreate(t, core, b)
	}
}

// TestUpdateNibKeepsClosedMilestoneEditable is the no-op half of the close gate.
//
// The web edit form sends `status` on EVERY save (web/src/lib/nibForm.svelte.ts),
// so a title-only edit arrives with status PRESENT but equal to what the nib
// already carries. Gating the refusal on "status was supplied" rather than
// "status is CHANGING" therefore makes a milestone that already holds a releasing
// status over a live queue permanently uneditable from the web — title, body,
// priority, every field — refused in the name of a close the caller never asked
// for. The queue is then unfixable through the one surface most likely to be
// fixing it.
//
// This is the same distinction the type branch draws for the same reason (see the
// oldEffectiveType comment in UpdateNib): a no-op submission must not re-validate
// a state the caller is not changing. The standing offense belongs to `nibs
// check`'s ClosedMilestoneQueue finding, which reports it without blocking the
// edit that would resolve it.
func TestUpdateNibKeepsClosedMilestoneEditable(t *testing.T) {
	ctx := context.Background()

	t.Run("an unrelated edit resending the same status is not a close", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedClosedMilestoneWithLiveQueue(t, core)

		title := "Renamed by the web form"
		status := "completed"
		got, err := resolver.Mutation().UpdateNib(ctx, "msshut", model.UpdateNibInput{
			Title:  &title,
			Status: &status,
		})
		if err != nil {
			t.Fatalf("editing a closed milestone with a live queue was refused: %v", err)
		}
		if got.Title != title {
			t.Errorf("Title = %q, want %q", got.Title, title)
		}
		if got.Status != status {
			t.Errorf("Status = %q, want %q", got.Status, status)
		}
	})

	// The complement, so the fix above cannot be "once releasing, allow anything":
	// revising one releasing reason to another IS a close of a milestone whose
	// queue is still live, and `nibs close` refuses it for the same reason.
	t.Run("revising one releasing reason to another is still refused", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedClosedMilestoneWithLiveQueue(t, core)

		_, err := resolver.Mutation().UpdateNib(ctx, "msshut", statusInput("scrapped"))
		var queueErr *MilestoneQueueOpenError
		if !errors.As(err, &queueErr) {
			t.Fatalf("re-closing for a different releasing reason should still be refused, got: %v", err)
		}
	})
}

// TestUpdateNibCloseGateReadsTheStoredType pins which type decides whether a nib
// HAS a queue.
//
// The guard judges the pending clone (type already applied), but the queue is
// read through membership.View.DirectMembers, which picks its axis from the
// STORED nib: milestone-typed containers answer with their assignees, everything
// else with its structural CHILDREN. So a request that retypes a container into
// a milestone while also closing it read that container's children as if they
// were queue entries and refused with a message naming work that carries no
// assignment at all — `--clear milestone` on it is a no-op, so the refusal names
// no remedy that would answer it.
//
// A nib only becoming a milestone in this very request has no queue to speak of:
// nothing could have been assigned to it while it was not one. The outcome is
// unchanged either way (a milestone can be nobody's parent, so the type change
// is refused regardless) — what changes is whether the reason given is true.
func TestUpdateNibCloseGateReadsTheStoredType(t *testing.T) {
	ctx := context.Background()
	resolver, core := setupTestResolver(t)
	mustCreate(t, core, &nib.Nib{ID: "ep1", Title: "Epic", Type: "epic", Status: "in-progress"})
	mustCreate(t, core, &nib.Nib{ID: "ft1", Title: "Feature", Type: "feature", Status: "todo", Parent: "ep1"})

	milestone, completed := "milestone", "completed"
	_, err := resolver.Mutation().UpdateNib(ctx, "ep1", model.UpdateNibInput{
		Type:   &milestone,
		Status: &completed,
	})
	if err == nil {
		t.Fatal("retyping an epic with an open child into a milestone should be refused")
	}
	var queueErr *MilestoneQueueOpenError
	if errors.As(err, &queueErr) {
		t.Fatalf("refused as a full queue naming %v, but ft1 is a structural CHILD with no assignment; "+
			"the queue refusal names a remedy that cannot apply: %v", queueErr.Open, err)
	}
	if !strings.Contains(err.Error(), "ft1") {
		t.Errorf("the refusal should still name the child that blocks the type change, got: %v", err)
	}
}
