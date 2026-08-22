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

	t.Run("re-asserting the same closed status on a standing offense is refused", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedCloseGateFixture(t, core)
		writeStatusOnDisk(t, core, "msq", "completed")

		if _, err := resolver.Mutation().UpdateNib(ctx, "msq", statusInput("completed")); err == nil {
			t.Fatal("UpdateNib(status=completed) on a standing offense succeeded; the invariant is about the state the write leaves")
		}
	})

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
// Core, and reloads. That hand edit is the only way the offending state arises
// once every write surface refuses to create it.
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
