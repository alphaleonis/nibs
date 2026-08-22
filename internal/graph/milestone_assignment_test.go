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

// seedQueueFixture creates two milestones and a keyed queue under the first:
// ms1 holds ka (a0) and kb (b0); ms2 holds kc (a0). ep1 is an unassigned root
// epic with an unassigned child task t1, and t2 is an unassigned root task.
func seedQueueFixture(t *testing.T, core *nibcore.Core) {
	t.Helper()
	for _, b := range []*nib.Nib{
		{ID: "ms1", Title: "Waypoint one", Type: "milestone", Status: "todo"},
		{ID: "ms2", Title: "Waypoint two", Type: "milestone", Status: "todo"},
		{ID: "ka", Title: "Queued A", Type: "task", Status: "todo", Milestone: "ms1", MilestoneOrder: "a0"},
		{ID: "kb", Title: "Queued B", Type: "task", Status: "todo", Milestone: "ms1", MilestoneOrder: "b0"},
		{ID: "kc", Title: "Queued C", Type: "task", Status: "todo", Milestone: "ms2", MilestoneOrder: "a0"},
		{ID: "ep1", Title: "Epic", Type: "epic", Status: "todo"},
		{ID: "t1", Title: "Child task", Type: "task", Status: "todo", Parent: "ep1"},
		{ID: "t2", Title: "Loose task", Type: "task", Status: "todo"},
	} {
		mustCreate(t, core, b)
	}
}

func assignInput(id string) model.UpdateNibInput {
	return model.UpdateNibInput{Milestone: graphql.OmittableOf(&id)}
}

// TestUpdateNibMilestoneAssignment pins the write half of the scheduling axis:
// updateNib's milestone field assigns, the ordering engine places the nib at
// the end of the target queue, a reassignment re-enters the new queue, and a
// clear drops both the assignment and the queue key.
func TestUpdateNibMilestoneAssignment(t *testing.T) {
	ctx := context.Background()

	t.Run("assigns and places last in the queue", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedQueueFixture(t, core)
		got, err := resolver.Mutation().UpdateNib(ctx, "t2", assignInput("ms1"))
		if err != nil {
			t.Fatalf("UpdateNib(milestone=ms1): %v", err)
		}
		if got.Milestone != "ms1" {
			t.Errorf("Milestone = %q, want ms1", got.Milestone)
		}
		if got.MilestoneOrder <= "b0" {
			t.Errorf("MilestoneOrder = %q, want a key after b0 (appended last)", got.MilestoneOrder)
		}
		if got.Order != "" {
			t.Errorf("Order = %q; assigning must not touch the sibling key", got.Order)
		}
	})

	t.Run("the first member takes the initial key", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		mustCreate(t, core, &nib.Nib{ID: "ms-empty", Title: "Empty", Type: "milestone", Status: "todo"})
		mustCreate(t, core, &nib.Nib{ID: "lone", Title: "Lone", Type: "task", Status: "todo"})
		got, err := resolver.Mutation().UpdateNib(ctx, "lone", assignInput("ms-empty"))
		if err != nil {
			t.Fatalf("UpdateNib: %v", err)
		}
		if got.MilestoneOrder != nib.OrderInitial() {
			t.Errorf("MilestoneOrder = %q, want the initial key %q", got.MilestoneOrder, nib.OrderInitial())
		}
	})

	t.Run("reassignment re-enters the new queue at its end", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedQueueFixture(t, core)
		got, err := resolver.Mutation().UpdateNib(ctx, "ka", assignInput("ms2"))
		if err != nil {
			t.Fatalf("UpdateNib(milestone=ms2): %v", err)
		}
		if got.Milestone != "ms2" {
			t.Errorf("Milestone = %q, want ms2", got.Milestone)
		}
		if got.MilestoneOrder <= "a0" {
			t.Errorf("MilestoneOrder = %q, want a key after kc's a0 — the old ms1 key must not be carried over", got.MilestoneOrder)
		}
	})

	t.Run("reassigning to the same milestone keeps the queue position", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedQueueFixture(t, core)
		got, err := resolver.Mutation().UpdateNib(ctx, "ka", assignInput("ms1"))
		if err != nil {
			t.Fatalf("UpdateNib: %v", err)
		}
		if got.MilestoneOrder != "a0" {
			t.Errorf("MilestoneOrder = %q, want a0 unchanged (no change of queue)", got.MilestoneOrder)
		}
	})

	t.Run("a short id normalizes to the full id", func(t *testing.T) {
		resolver, core := setupTestResolverWithPrefix(t, "nibs-")
		mustCreate(t, core, &nib.Nib{ID: "nibs-ms1", Title: "Waypoint", Type: "milestone", Status: "todo"})
		mustCreate(t, core, &nib.Nib{ID: "nibs-t1", Title: "Task", Type: "task", Status: "todo"})
		got, err := resolver.Mutation().UpdateNib(ctx, "nibs-t1", assignInput("ms1"))
		if err != nil {
			t.Fatalf("UpdateNib(milestone=ms1 short form): %v", err)
		}
		if got.Milestone != "nibs-ms1" {
			t.Errorf("Milestone = %q, want the normalized full id nibs-ms1", got.Milestone)
		}
	})

	t.Run("explicit null clears the assignment and the queue key", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedQueueFixture(t, core)
		got, err := resolver.Mutation().UpdateNib(ctx, "ka", model.UpdateNibInput{Milestone: graphql.OmittableOf[*string](nil)})
		if err != nil {
			t.Fatalf("UpdateNib(milestone=null): %v", err)
		}
		if got.Milestone != "" || got.MilestoneOrder != "" {
			t.Errorf("after clear: Milestone=%q MilestoneOrder=%q, want both empty", got.Milestone, got.MilestoneOrder)
		}
	})

	t.Run("empty string clears like null", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedQueueFixture(t, core)
		got, err := resolver.Mutation().UpdateNib(ctx, "kb", assignInput(""))
		if err != nil {
			t.Fatalf("UpdateNib(milestone=\"\"): %v", err)
		}
		if got.Milestone != "" || got.MilestoneOrder != "" {
			t.Errorf("after clear: Milestone=%q MilestoneOrder=%q, want both empty", got.Milestone, got.MilestoneOrder)
		}
	})

	t.Run("omitting the field leaves both unchanged", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedQueueFixture(t, core)
		title := "Renamed"
		got, err := resolver.Mutation().UpdateNib(ctx, "ka", model.UpdateNibInput{Title: &title})
		if err != nil {
			t.Fatalf("UpdateNib(title): %v", err)
		}
		if got.Milestone != "ms1" || got.MilestoneOrder != "a0" {
			t.Errorf("Milestone=%q MilestoneOrder=%q, want ms1/a0 untouched", got.Milestone, got.MilestoneOrder)
		}
	})
}

// TestUpdateNibMilestoneRefusals pins the write-strict half: an unknown or
// non-milestone target, a milestone-typed subject, and the exclusivity rule
// (decision 1.2 — never a nib and one of its ancestors both assigned) are all
// refused naming why, and a refusal leaves the shared nib untouched.
func TestUpdateNibMilestoneRefusals(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		subject     string
		target      string
		errContains []string
	}{
		{"missing target", "t2", "no-such-ms", []string{"milestone nib not found", "no-such-ms"}},
		{"non-milestone target names its type", "t2", "ep1", []string{"ep1", "epic", "not milestone"}},
		{"milestone-typed subject", "ms2", "ms1", []string{"a milestone cannot be assigned to a milestone"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver, core := setupTestResolver(t)
			seedQueueFixture(t, core)
			_, err := resolver.Mutation().UpdateNib(ctx, tt.subject, assignInput(tt.target))
			if err == nil {
				t.Fatalf("UpdateNib(%s, milestone=%s) succeeded, want refusal", tt.subject, tt.target)
			}
			for _, want := range tt.errContains {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want substring %q", err.Error(), want)
				}
			}
			after, _ := core.Get(tt.subject)
			if after.Milestone != "" || after.MilestoneOrder != "" {
				t.Errorf("refusal leaked into the store: Milestone=%q MilestoneOrder=%q", after.Milestone, after.MilestoneOrder)
			}
		})
	}

	t.Run("ancestor already assigned", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedQueueFixture(t, core)
		if _, err := resolver.Mutation().UpdateNib(ctx, "ep1", assignInput("ms1")); err != nil {
			t.Fatalf("assigning the epic: %v", err)
		}
		_, err := resolver.Mutation().UpdateNib(ctx, "t1", assignInput("ms2"))
		if err == nil {
			t.Fatal("assigning a child of an assigned epic succeeded, want refusal")
		}
		for _, want := range []string{"t1", "ep1", "ms1", "ancestor"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error = %q, want it to name %q", err.Error(), want)
			}
		}
	})

	t.Run("descendant already assigned", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		seedQueueFixture(t, core)
		// A grandchild, so the refusal proves the walk is the whole subtree.
		mustCreate(t, core, &nib.Nib{ID: "t1-sub", Title: "Grandchild", Type: "task", Status: "todo", Parent: "t1"})
		if _, err := resolver.Mutation().UpdateNib(ctx, "t1-sub", assignInput("ms1")); err != nil {
			t.Fatalf("assigning the grandchild: %v", err)
		}
		_, err := resolver.Mutation().UpdateNib(ctx, "ep1", assignInput("ms2"))
		if err == nil {
			t.Fatal("assigning the ancestor of an assigned nib succeeded, want refusal")
		}
		for _, want := range []string{"ep1", "t1-sub", "ms1", "descendant"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error = %q, want it to name %q", err.Error(), want)
			}
		}
	})
}

// seedExclusivityFixture seeds the exclusivity scenarios: ms1 and ms2 are
// milestones; ep-a is an epic assigned to ms1 carrying an unassigned child
// task tk-under; ft-a is a root feature assigned to ms2; ft-u is an
// unassigned root feature whose child task tk-a is assigned to ms2; ft-x is
// an unassigned, childless root feature; ep-free is an unassigned root epic.
func seedExclusivityFixture(t *testing.T) (*Resolver, *nibcore.Core) {
	t.Helper()
	resolver, core := setupTestResolver(t)
	for _, b := range []*nib.Nib{
		{ID: "ms1", Title: "Waypoint one", Type: "milestone", Status: "todo"},
		{ID: "ms2", Title: "Waypoint two", Type: "milestone", Status: "todo"},
		{ID: "ep-a", Title: "Assigned epic", Type: "epic", Status: "todo", Milestone: "ms1", MilestoneOrder: "a0"},
		{ID: "tk-under", Title: "Task under assigned epic", Type: "task", Status: "todo", Parent: "ep-a"},
		{ID: "ft-a", Title: "Assigned feature", Type: "feature", Status: "todo", Milestone: "ms2", MilestoneOrder: "a0"},
		{ID: "ft-u", Title: "Unassigned feature", Type: "feature", Status: "todo"},
		{ID: "tk-a", Title: "Assigned task", Type: "task", Status: "todo", Parent: "ft-u", Milestone: "ms2", MilestoneOrder: "b0"},
		{ID: "ft-x", Title: "Loose feature", Type: "feature", Status: "todo"},
		{ID: "ep-free", Title: "Free epic", Type: "epic", Status: "todo"},
	} {
		mustCreate(t, core, b)
	}
	return resolver, core
}

// TestUpdateNibCombinedParentAndMilestone pins one updateNib carrying BOTH a
// parent change and a milestone change: it is judged on the state it leaves,
// so it succeeds exactly when the two changes issued as separate mutations
// would, whichever order they would have to run in — a clear of either axis
// opens the way for the other, while an assignment that conflicts with the
// chain the nib will sit on is still refused.
func TestUpdateNibCombinedParentAndMilestone(t *testing.T) {
	ctx := context.Background()
	nullMilestone := graphql.OmittableOf[*string](nil)
	str := func(s string) *string { return &s }

	t.Run("reparent under an assigned epic while clearing the assignment", func(t *testing.T) {
		for _, clear := range []struct {
			name  string
			value graphql.Omittable[*string]
		}{{"null", nullMilestone}, {"empty string", graphql.OmittableOf(str(""))}} {
			t.Run(clear.name, func(t *testing.T) {
				resolver, _ := seedExclusivityFixture(t)
				got, err := resolver.Mutation().UpdateNib(ctx, "ft-a", model.UpdateNibInput{Parent: graphql.OmittableOf(str("ep-a")), Milestone: clear.value})
				if err != nil {
					t.Fatalf("reparent + clear was refused: %v (the two as separate mutations both succeed)", err)
				}
				if got.Parent != "ep-a" || got.Milestone != "" || got.MilestoneOrder != "" {
					t.Errorf("Parent=%q Milestone=%q MilestoneOrder=%q, want ep-a, empty, empty", got.Parent, got.Milestone, got.MilestoneOrder)
				}
			})
		}
	})

	// Both block orderings refuse this one — the reverted order refuses in the
	// parent block instead, via checkReparentExclusivity, because by then the
	// subject carries the assignment. What only the fixed order gets right is
	// WHICH operation the refusal blames: ft-x is unassigned, so the move alone
	// is legal and blaming it sends the caller to retry a step that was never
	// the problem. The assignment against the chain the nib will sit on is.
	t.Run("reparent under an assigned epic with an assignment is still refused", func(t *testing.T) {
		resolver, core := seedExclusivityFixture(t)
		_, err := resolver.Mutation().UpdateNib(ctx, "ft-x", model.UpdateNibInput{Parent: graphql.OmittableOf(str("ep-a")), Milestone: graphql.OmittableOf(str("ms2"))})
		if err == nil {
			t.Fatal("assigning while moving under an assigned epic succeeded, want refusal")
		}
		for _, want := range []string{"cannot assign ft-x to milestone ms2", "its ancestor ep-a is already assigned to milestone ms1"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error = %q, want it to blame the assignment with %q", err.Error(), want)
			}
		}
		after, _ := core.Get("ft-x")
		if after.Parent != "" || after.Milestone != "" {
			t.Errorf("refusal leaked: Parent=%q Milestone=%q", after.Parent, after.Milestone)
		}
	})

	t.Run("clear the parent while assigning", func(t *testing.T) {
		resolver, _ := seedExclusivityFixture(t)
		got, err := resolver.Mutation().UpdateNib(ctx, "tk-under", model.UpdateNibInput{Parent: graphql.OmittableOf[*string](nil), Milestone: graphql.OmittableOf(str("ms2"))})
		if err != nil {
			t.Fatalf("clear parent + assign was refused: %v", err)
		}
		if got.Parent != "" || got.Milestone != "ms2" {
			t.Errorf("Parent=%q Milestone=%q, want root and ms2", got.Parent, got.Milestone)
		}
	})

	t.Run("reparent under an unassigned epic while assigning", func(t *testing.T) {
		resolver, _ := seedExclusivityFixture(t)
		got, err := resolver.Mutation().UpdateNib(ctx, "tk-under", model.UpdateNibInput{Parent: graphql.OmittableOf(str("ep-free")), Milestone: graphql.OmittableOf(str("ms2"))})
		if err != nil {
			t.Fatalf("reparent + assign was refused: %v (the assignment is judged against the chain the nib will sit on, not the one it leaves)", err)
		}
		if got.Parent != "ep-free" || got.Milestone != "ms2" {
			t.Errorf("Parent=%q Milestone=%q, want ep-free and ms2", got.Parent, got.Milestone)
		}
	})
}

// TestUpdateNibCombinedTypeAndMilestoneClear pins one updateNib carrying BOTH
// a type change to milestone and a clear of the assignment. The axis rule (a
// milestone takes no assignment) judges the state the request LEAVES, so the
// combined call succeeds exactly as the two mutations issued separately do. A
// type change that leaves an assignment standing — alone, or beside an
// assignment to another milestone — is still refused.
func TestUpdateNibCombinedTypeAndMilestoneClear(t *testing.T) {
	ctx := context.Background()
	str := func(s string) *string { return &s }
	milestoneType := "milestone"

	t.Run("type milestone while clearing the assignment", func(t *testing.T) {
		for _, clear := range []struct {
			name  string
			value graphql.Omittable[*string]
		}{{"null", graphql.OmittableOf[*string](nil)}, {"empty string", graphql.OmittableOf(str(""))}} {
			t.Run(clear.name, func(t *testing.T) {
				resolver, core := seedExclusivityFixture(t)
				got, err := resolver.Mutation().UpdateNib(ctx, "ft-a", model.UpdateNibInput{Type: &milestoneType, Milestone: clear.value})
				if err != nil {
					t.Fatalf("type change + clear was refused: %v (the two as separate mutations both succeed)", err)
				}
				if got.Type != "milestone" || got.Milestone != "" || got.MilestoneOrder != "" {
					t.Errorf("Type=%q Milestone=%q MilestoneOrder=%q, want milestone, empty, empty", got.Type, got.Milestone, got.MilestoneOrder)
				}
				after, err := core.Get("ft-a")
				if err != nil {
					t.Fatalf("re-read ft-a: %v", err)
				}
				if after.Type != "milestone" || after.Milestone != "" || after.MilestoneOrder != "" {
					t.Errorf("on disk: Type=%q Milestone=%q MilestoneOrder=%q, want milestone, empty, empty", after.Type, after.Milestone, after.MilestoneOrder)
				}
			})
		}
	})

	for _, tt := range []struct {
		name    string
		subject string
		input   model.UpdateNibInput
	}{
		{"type milestone alone on an assigned nib", "ft-a", model.UpdateNibInput{Type: &milestoneType}},
		{"type milestone beside an assignment to another milestone", "ft-a", model.UpdateNibInput{Type: &milestoneType, Milestone: graphql.OmittableOf(str("ms1"))}},
		{"type milestone beside an assignment on an unassigned nib", "ft-x", model.UpdateNibInput{Type: &milestoneType, Milestone: graphql.OmittableOf(str("ms1"))}},
	} {
		t.Run(tt.name+" is still refused", func(t *testing.T) {
			resolver, core := seedExclusivityFixture(t)
			before, err := core.Get(tt.subject)
			if err != nil {
				t.Fatalf("read %s: %v", tt.subject, err)
			}
			wasMilestone := before.Milestone
			_, err = resolver.Mutation().UpdateNib(ctx, tt.subject, tt.input)
			if err == nil {
				t.Fatal("the type change succeeded, want the axis refusal")
			}
			if !strings.Contains(err.Error(), "a milestone cannot be assigned to a milestone") {
				t.Errorf("error = %q, want the axis refusal", err.Error())
			}
			after, err := core.Get(tt.subject)
			if err != nil {
				t.Fatalf("re-read %s: %v", tt.subject, err)
			}
			if after.Type == "milestone" || after.Milestone != wasMilestone {
				t.Errorf("refusal leaked: Type=%q Milestone=%q, want the pre-call type and Milestone=%q", after.Type, after.Milestone, wasMilestone)
			}
		})
	}
}

// TestReparentHonorsAssignmentExclusivity pins the same invariant on the
// re-parent path: a move that would put an assigned nib under an assigned
// ancestor — or an assigned descendant under one — is refused on every
// surface that reparents (updateNib's parent, setParent, reorderNib's
// parentId). A no-op reparent onto the current parent does not trip on a
// pre-existing conflict, so the type-change re-validation cannot dead-end
// hand-edited data.
func TestReparentHonorsAssignmentExclusivity(t *testing.T) {
	ctx := context.Background()
	seed := seedExclusivityFixture

	wantRefused := func(t *testing.T, err error, names ...string) {
		t.Helper()
		if err == nil {
			t.Fatal("reparent succeeded, want the exclusivity refusal")
		}
		for _, want := range names {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error = %q, want it to name %q", err.Error(), want)
			}
		}
	}

	t.Run("updateNib parent: assigned nib under assigned epic", func(t *testing.T) {
		resolver, core := seed(t)
		parent := "ep-a"
		_, err := resolver.Mutation().UpdateNib(ctx, "ft-a", model.UpdateNibInput{Parent: graphql.OmittableOf(&parent)})
		wantRefused(t, err, "ft-a", "ep-a", "ms1", "ms2")
		after, _ := core.Get("ft-a")
		if after.Parent != "" {
			t.Errorf("refused move leaked: Parent = %q", after.Parent)
		}
	})

	t.Run("setParent: assigned nib under assigned epic", func(t *testing.T) {
		resolver, _ := seed(t)
		parent := "ep-a"
		_, err := resolver.Mutation().SetParent(ctx, "ft-a", &parent, nil)
		wantRefused(t, err, "ft-a", "ep-a")
	})

	t.Run("reorderNib parentId: assigned nib under assigned epic", func(t *testing.T) {
		resolver, _ := seed(t)
		parent := "ep-a"
		first := true
		_, err := resolver.Mutation().ReorderNib(ctx, "ft-a", nil, nil, &first, &parent, nil, model.OrderScopeParent)
		wantRefused(t, err, "ft-a", "ep-a")
	})

	t.Run("unassigned nib carrying an assigned descendant under an assigned epic", func(t *testing.T) {
		resolver, _ := seed(t)
		parent := "ep-a"
		_, err := resolver.Mutation().UpdateNib(ctx, "ft-u", model.UpdateNibInput{Parent: graphql.OmittableOf(&parent)})
		wantRefused(t, err, "tk-a", "ep-a")
	})

	t.Run("assigned nib under an unassigned epic is fine", func(t *testing.T) {
		resolver, _ := seed(t)
		parent := "ep-free"
		got, err := resolver.Mutation().UpdateNib(ctx, "ft-a", model.UpdateNibInput{Parent: graphql.OmittableOf(&parent)})
		if err != nil {
			t.Fatalf("reparent under an unassigned epic: %v", err)
		}
		if got.Parent != "ep-free" {
			t.Errorf("Parent = %q, want ep-free", got.Parent)
		}
	})

	t.Run("a no-op reparent does not trip on a pre-existing conflict", func(t *testing.T) {
		resolver, core := seed(t)
		// Hand-edited shape: an assigned feature already under an assigned
		// epic, written straight to the store.
		mustCreate(t, core, &nib.Nib{ID: "ft-legacy", Title: "Legacy", Type: "feature", Status: "todo", Parent: "ep-a", Milestone: "ms2", MilestoneOrder: "c0"})
		typ := "bug"
		if _, err := resolver.Mutation().UpdateNib(ctx, "ft-legacy", model.UpdateNibInput{Type: &typ}); err != nil {
			t.Fatalf("type change on a nib with a pre-existing conflict was refused: %v", err)
		}
	})
}

// TestReorderNibMilestoneScope pins the queue arm of reorderNib: scope
// MILESTONE routes the move through the ordering engine's milestone scope,
// the engine's two refusals (no queue, anchor in another queue) surface as
// errors, a container change is refused in this scope, and exactly one nib is
// written.
func TestReorderNibMilestoneScope(t *testing.T) {
	ctx := context.Background()

	build := func() (*ordererStubReader, *stubWriter, *Resolver) {
		reader := newOrdererReader(
			&nib.Nib{ID: "ms1", Title: "Waypoint", Type: "milestone", Status: "todo"},
			&nib.Nib{ID: "ms2", Title: "Other", Type: "milestone", Status: "todo"},
			&nib.Nib{ID: "ka", Title: "A", Milestone: "ms1", MilestoneOrder: "a0", Order: "keep"},
			&nib.Nib{ID: "kb", Title: "B", Milestone: "ms1", MilestoneOrder: "b0", Order: "keep"},
			&nib.Nib{ID: "kc", Title: "C", Milestone: "ms1", MilestoneOrder: "c0", Order: "keep"},
			&nib.Nib{ID: "str-1", Title: "Stranger", Milestone: "ms2", MilestoneOrder: "a0"},
			&nib.Nib{ID: "loose", Title: "Loose", Order: "a0"},
		)
		writer := &stubWriter{store: &reader.stubReader}
		resolver := &Resolver{Reader: reader, Writer: writer, Validator: &stubValidator{}, Blocking: &stubBlockingChecker{}, Orderer: NewOrderer(reader, writer)}
		return reader, writer, resolver
	}

	t.Run("moves within the queue and writes only the subject", func(t *testing.T) {
		_, writer, resolver := build()
		first := true
		got, err := resolver.Mutation().ReorderNib(ctx, "kc", nil, nil, &first, nil, nil, model.OrderScopeMilestone)
		if err != nil {
			t.Fatalf("ReorderNib(scope=MILESTONE, first): %v", err)
		}
		if got.MilestoneOrder >= "a0" {
			t.Errorf("MilestoneOrder = %q, want a key before a0", got.MilestoneOrder)
		}
		if got.Order != "keep" {
			t.Errorf("Order = %q; a queue move must not touch the sibling key", got.Order)
		}
		if len(writer.updated) != 1 || writer.updated[0].ID != "kc" {
			t.Errorf("writes = %v, want exactly one, to kc", nibIDs(writer.updated))
		}
	})

	t.Run("after and before anchor within the queue", func(t *testing.T) {
		_, _, resolver := build()
		after := "ka"
		got, err := resolver.Mutation().ReorderNib(ctx, "kc", &after, nil, nil, nil, nil, model.OrderScopeMilestone)
		if err != nil {
			t.Fatalf("ReorderNib(after ka): %v", err)
		}
		if got.MilestoneOrder <= "a0" || got.MilestoneOrder >= "b0" {
			t.Errorf("MilestoneOrder = %q, want between a0 and b0", got.MilestoneOrder)
		}
		_, _, resolver = build()
		before := "kb"
		got, err = resolver.Mutation().ReorderNib(ctx, "kc", nil, &before, nil, nil, nil, model.OrderScopeMilestone)
		if err != nil {
			t.Fatalf("ReorderNib(before kb): %v", err)
		}
		if got.MilestoneOrder <= "a0" || got.MilestoneOrder >= "b0" {
			t.Errorf("MilestoneOrder = %q, want between a0 and b0", got.MilestoneOrder)
		}
	})

	t.Run("an unassigned subject has no queue position", func(t *testing.T) {
		_, writer, resolver := build()
		first := true
		_, err := resolver.Mutation().ReorderNib(ctx, "loose", nil, nil, &first, nil, nil, model.OrderScopeMilestone)
		if err == nil || !strings.Contains(err.Error(), "assigned to no milestone") {
			t.Fatalf("error = %v, want the no-queue refusal", err)
		}
		if len(writer.updated) != 0 {
			t.Errorf("a refused move wrote %v", nibIDs(writer.updated))
		}
	})

	t.Run("an anchor in another queue is refused", func(t *testing.T) {
		_, _, resolver := build()
		after := "str-1"
		_, err := resolver.Mutation().ReorderNib(ctx, "kc", &after, nil, nil, nil, nil, model.OrderScopeMilestone)
		if err == nil || !strings.Contains(err.Error(), "not in the same milestone queue") {
			t.Fatalf("error = %v, want the other-queue refusal", err)
		}
	})

	t.Run("a container change is refused in the milestone scope", func(t *testing.T) {
		_, writer, resolver := build()
		first := true
		parent := "loose"
		_, err := resolver.Mutation().ReorderNib(ctx, "kc", nil, nil, &first, &parent, nil, model.OrderScopeMilestone)
		if err == nil || !strings.Contains(err.Error(), "parentId") {
			t.Fatalf("error = %v, want the parentId-in-queue-scope refusal", err)
		}
		if len(writer.updated) != 0 {
			t.Errorf("a refused move wrote %v", nibIDs(writer.updated))
		}
	})

	t.Run("the parent scope is untouched by the new argument", func(t *testing.T) {
		_, _, resolver := build()
		first := true
		got, err := resolver.Mutation().ReorderNib(ctx, "kc", nil, nil, &first, nil, nil, model.OrderScopeParent)
		if err != nil {
			t.Fatalf("ReorderNib(scope=PARENT): %v", err)
		}
		if got.MilestoneOrder != "c0" {
			t.Errorf("MilestoneOrder = %q; a parent-scope move must not touch the queue key", got.MilestoneOrder)
		}
	})
}
