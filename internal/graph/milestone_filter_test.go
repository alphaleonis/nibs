package graph

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/executor"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
)

// milestoneFixture is the assignment-axis analogue of hierarchyFixture: two
// milestones, direct assignees on both axes' interesting shapes, and the
// hand-editable garbage the resolution rule exists to drop.
//
//	nibs-m1, nibs-m2  milestones
//	nibs-e1           epic, ASSIGNED to m1 (queue key a0)
//	nibs-t1           task under e1, unassigned — planned only DERIVEDLY
//	nibs-t2           task, ASSIGNED to m1 (queue key b0)
//	nibs-t3           root task, unassigned — the backlog
//	nibs-t4           task whose assignment names no nib (dangling)
//	nibs-t5           task whose assignment names t3, a non-milestone
func milestoneFixture() *stubReader {
	nibs := []*nib.Nib{
		{ID: "nibs-m1", Title: "First milestone", Type: "milestone", Status: "todo"},
		{ID: "nibs-m2", Title: "Second milestone", Type: "milestone", Status: "todo"},
		{ID: "nibs-e1", Title: "Assigned epic", Type: "epic", Status: "todo", Milestone: "nibs-m1", MilestoneOrder: "a0"},
		{ID: "nibs-t1", Title: "Child of the assigned epic", Parent: "nibs-e1", Status: "todo"},
		{ID: "nibs-t2", Title: "Directly assigned task", Status: "todo", Milestone: "nibs-m1", MilestoneOrder: "b0"},
		{ID: "nibs-t3", Title: "Backlog task", Status: "todo"},
		{ID: "nibs-t4", Title: "Dangling assignment", Status: "todo", Milestone: "nibs-ghost"},
		{ID: "nibs-t5", Title: "Assignment naming a non-milestone", Status: "todo", Milestone: "nibs-t3"},
	}
	byID := make(map[string]*nib.Nib, len(nibs))
	for _, b := range nibs {
		byID[b.ID] = b
	}
	return &stubReader{nibs: byID, allNibs: nibs, prefix: "nibs-"}
}

// TestApplyFilterMilestoneSelectsTheResolvedDirectAssignment pins the milestone
// filter to DIRECT assignment under the shared resolution rule: exactly the
// nibs whose `milestone:` field resolves to the target — not the structural
// children of an assignee (t1 stays out even though the derived reading plans
// it), and not a nib whose assignment fails to resolve (t4, t5).
func TestApplyFilterMilestoneSelectsTheResolvedDirectAssignment(t *testing.T) {
	reader := milestoneFixture()
	blocking := &stubBlockingChecker{}

	got := applyFilterOK(t, context.Background(), reader.allNibs, &model.NibFilter{Milestone: strPtr("nibs-m1")}, reader, blocking)
	assertNibIDs(t, got, []string{"nibs-e1", "nibs-t2"})
}

// TestApplyFilterMilestoneShortForm pins that the milestone target is
// normalized like every other id-valued filter: the short form selects the
// same queue the full id does.
func TestApplyFilterMilestoneShortForm(t *testing.T) {
	reader := milestoneFixture()
	blocking := &stubBlockingChecker{}

	got := applyFilterOK(t, context.Background(), reader.allNibs, &model.NibFilter{Milestone: strPtr("m1")}, reader, blocking)
	assertNibIDs(t, got, []string{"nibs-e1", "nibs-t2"})
}

// The unknown-target and empty-string refusals are NOT tested here: milestone
// is enrolled in idValuedFilterFields (via idValuedNonIdNamedFields), so the
// reflective walks in filters_test.go and filter_errors_test.go drive it
// through the shared id-filter contract alongside the suffix-named eight.

// TestApplyFilterMilestoneNonMilestoneTargetIsRefused: a target that exists
// but is not milestone-typed is refused naming its type, not answered with the
// empty set. No assignment can RESOLVE to a non-milestone (t5 names t3 and is
// dropped by the same rule that keeps it out of every membership view), so an
// empty answer would read as "this milestone has no members" for an id that
// names no milestone — the same mistake the write path refuses. The class is
// validation, not not-found: the id is real, and the refusal must not carry
// the sentinel that would route it to the "no such nib" reading.
func TestApplyFilterMilestoneNonMilestoneTargetIsRefused(t *testing.T) {
	reader := milestoneFixture()
	blocking := &stubBlockingChecker{}

	got, err := ApplyFilter(context.Background(), reader.allNibs, &model.NibFilter{Milestone: strPtr("t3")}, reader, blocking)
	if err == nil {
		t.Fatalf("got %d nibs and no error; a non-milestone target must be refused", len(got))
	}
	if got != nil {
		t.Errorf("result = %v, want nil alongside the error", got)
	}
	var wrongType *FilterTargetTypeError
	if !errors.As(err, &wrongType) {
		t.Fatalf("error = %T (%v), want *FilterTargetTypeError", err, err)
	}
	if wrongType.Field != "milestone" || wrongType.ID != "nibs-t3" || wrongType.Got != "task" || wrongType.Want != "milestone" {
		t.Errorf("refusal = %+v, want field milestone, the normalized id nibs-t3, got task, want milestone", *wrongType)
	}
	for _, want := range []string{"milestone filter", `"nibs-t3"`, "has type task, not milestone"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want substring %q", err.Error(), want)
		}
	}
	if errors.Is(err, nib.ErrNotFound) {
		t.Error("a wrong-typed target carries nib.ErrNotFound, so it classifies as NOT_FOUND — the id names a nib")
	}
}

// TestApplyFilterNoMilestoneReadsDerivedMembership pins noMilestone to the
// DERIVED reading (membership.MilestoneOf): true is the backlog, and a child
// of an assigned epic is planned work, not backlog — the distinction that
// separates this field from "milestone: is empty".
func TestApplyFilterNoMilestoneReadsDerivedMembership(t *testing.T) {
	reader := milestoneFixture()
	blocking := &stubBlockingChecker{}
	ctx := context.Background()

	t.Run("true keeps the backlog", func(t *testing.T) {
		got := applyFilterOK(t, ctx, reader.allNibs, &model.NibFilter{NoMilestone: boolPtr(true)}, reader, blocking)
		// t1 is NOT here: its parent e1 is assigned, so it is planned work.
		// t4 and t5 are: an assignment that resolves to nothing schedules
		// nothing. m1 and m2 are: a milestone belongs to no milestone itself.
		assertNibIDs(t, got, []string{"nibs-m1", "nibs-m2", "nibs-t3", "nibs-t4", "nibs-t5"})
	})

	t.Run("false keeps exactly the complement", func(t *testing.T) {
		got := applyFilterOK(t, ctx, reader.allNibs, &model.NibFilter{NoMilestone: boolPtr(false)}, reader, blocking)
		assertNibIDs(t, got, []string{"nibs-e1", "nibs-t1", "nibs-t2"})
	})

	t.Run("membership derives from the whole store, not the candidate slice", func(t *testing.T) {
		// Hand ApplyFilter only the child: its assigned ancestor is outside the
		// slice, and the answer must not change — derivation reads the store.
		child := reader.nibs["nibs-t1"]
		got := applyFilterOK(t, ctx, []*nib.Nib{child}, &model.NibFilter{NoMilestone: boolPtr(true)}, reader, blocking)
		if len(got) != 0 {
			t.Errorf("got %d nibs, want none — t1's derived membership must survive its ancestor being filtered out of the working set", len(got))
		}
	})
}

// TestNibAxisFieldsAreQueryable drives the three axis fields through the real
// gqlgen executor: the schema exposes milestone, milestoneOrder and area as
// the STORED values off the autobound nib.
func TestNibAxisFieldsAreQueryable(t *testing.T) {
	resolver, core := setupTestResolver(t)

	mustCreate(t, core, &nib.Nib{ID: "ms1", Slug: "milestone", Title: "Milestone", Type: "milestone", Status: "todo"})
	mustCreate(t, core, &nib.Nib{
		ID:             "tk1",
		Slug:           "task",
		Title:          "Assigned task",
		Status:         "todo",
		Milestone:      "ms1",
		MilestoneOrder: "a0",
		Area:           "web/ui",
	})

	es := NewExecutableSchema(Config{Resolvers: resolver})
	exec := executor.New(es)

	ctx := graphql.StartOperationTrace(context.Background())
	params := &graphql.RawParams{
		Query: `{ nib(id: "tk1") { milestone milestoneOrder area } }`,
	}
	opCtx, errs := exec.CreateOperationContext(ctx, params)
	if len(errs) > 0 {
		t.Fatalf("CreateOperationContext: %v", errs)
	}
	ctx = graphql.WithOperationContext(ctx, opCtx)
	handler, ctx := exec.DispatchOperation(ctx, opCtx)
	resp := handler(ctx)
	if len(resp.Errors) > 0 {
		t.Fatalf("query returned errors: %v", resp.Errors)
	}

	var payload struct {
		Nib struct {
			Milestone      string `json:"milestone"`
			MilestoneOrder string `json:"milestoneOrder"`
			Area           string `json:"area"`
		} `json:"nib"`
	}
	if err := json.Unmarshal(resp.Data, &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload.Nib.Milestone != "ms1" {
		t.Errorf("milestone = %q, want %q", payload.Nib.Milestone, "ms1")
	}
	if payload.Nib.MilestoneOrder != "a0" {
		t.Errorf("milestoneOrder = %q, want %q", payload.Nib.MilestoneOrder, "a0")
	}
	if payload.Nib.Area != "web/ui" {
		t.Errorf("area = %q, want %q", payload.Nib.Area, "web/ui")
	}
}
