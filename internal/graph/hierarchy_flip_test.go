package graph

import (
	"context"
	"strings"
	"testing"

	"github.com/99designs/gqlgen/graphql"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
)

// TestCreateNibHierarchyFlip pins the axis-model hierarchy at the mutation
// boundary: milestones are outside the parent graph (nothing nests under
// them), epics top the work tree, and every type is creatable at root.
func TestCreateNibHierarchyFlip(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	mustCreate(t, core, &nib.Nib{ID: "ms-flip", Title: "Waypoint", Type: "milestone", Status: "todo"})

	t.Run("every type is creatable at root", func(t *testing.T) {
		for _, typ := range []string{"milestone", "epic", "feature", "bug", "task", "research"} {
			typ := typ
			got, err := resolver.Mutation().CreateNib(ctx, model.CreateNibInput{Title: "Root " + typ, Type: &typ})
			if err != nil {
				t.Fatalf("CreateNib(%s at root) refused: %v", typ, err)
			}
			if got.Parent != "" {
				t.Errorf("CreateNib(%s) parent = %q, want root", typ, got.Parent)
			}
		}
	})

	t.Run("nothing is creatable under a milestone", func(t *testing.T) {
		parent := "ms-flip"
		tests := []struct {
			typ         string
			errContains string
		}{
			{typ: "epic", errContains: "epic cannot have a parent"},
			{typ: "feature", errContains: "parent of type epic"},
			{typ: "bug", errContains: "parent of type epic"},
			{typ: "task", errContains: "epic, feature, or bug"},
			{typ: "research", errContains: "epic, feature, or bug"},
		}
		for _, tt := range tests {
			typ := tt.typ
			_, err := resolver.Mutation().CreateNib(ctx, model.CreateNibInput{Title: "Nested " + typ, Type: &typ, Parent: &parent})
			if err == nil {
				t.Fatalf("CreateNib(%s under milestone) succeeded, want refusal", typ)
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("CreateNib(%s under milestone) error = %q, want substring %q", typ, err.Error(), tt.errContains)
			}
		}
	})

	t.Run("reparenting onto a milestone is refused", func(t *testing.T) {
		mustCreate(t, core, &nib.Nib{ID: "task-flip", Title: "Root task", Type: "task", Status: "todo"})
		newParent := "ms-flip"
		_, err := resolver.Mutation().UpdateNib(ctx, "task-flip", model.UpdateNibInput{Parent: graphql.OmittableOf(&newParent)})
		if err == nil {
			t.Fatal("UpdateNib(parent=milestone) succeeded, want refusal")
		}
		if !strings.Contains(err.Error(), "epic, feature, or bug") {
			t.Errorf("error = %q, want the allowed parent types named", err.Error())
		}
	})
}

// TestUpdateNibTypeChangeValidatesAxes pins the type-change route into the
// axis rule: a nib carrying an assignment axis cannot become a milestone,
// because a waypoint is not work and takes neither axis.
func TestUpdateNibTypeChangeValidatesAxes(t *testing.T) {
	// The area fixtures below need a store that DECLARES `web/ui`: an area is
	// checked against the vocabulary on every write, so a store declaring none
	// refuses the seed before the axis rule under test is ever reached.
	resolver, core := setupTestResolverWithAreas(t)
	ctx := context.Background()

	mustCreate(t, core, &nib.Nib{ID: "ms-ax", Title: "Waypoint", Type: "milestone", Status: "todo"})

	t.Run("assigned nib cannot become a milestone", func(t *testing.T) {
		mustCreate(t, core, &nib.Nib{ID: "task-ax-m", Title: "Assigned", Type: "task", Status: "todo", Milestone: "ms-ax"})
		newType := "milestone"
		_, err := resolver.Mutation().UpdateNib(ctx, "task-ax-m", model.UpdateNibInput{Type: &newType})
		if err == nil {
			t.Fatal("UpdateNib(type=milestone) on an assigned nib succeeded, want refusal")
		}
		if !strings.Contains(err.Error(), "cannot be assigned to a milestone") {
			t.Errorf("error = %q, want the milestone-axis refusal", err.Error())
		}
	})

	t.Run("nib with an area cannot become a milestone", func(t *testing.T) {
		mustCreate(t, core, &nib.Nib{ID: "task-ax-a", Title: "Scoped", Type: "task", Status: "todo", Area: "web/ui"})
		newType := "milestone"
		_, err := resolver.Mutation().UpdateNib(ctx, "task-ax-a", model.UpdateNibInput{Type: &newType})
		if err == nil {
			t.Fatal("UpdateNib(type=milestone) on a nib with an area succeeded, want refusal")
		}
		if !strings.Contains(err.Error(), "cannot have an area") {
			t.Errorf("error = %q, want the area-axis refusal", err.Error())
		}
	})

	t.Run("refused retype leaves the nib untouched", func(t *testing.T) {
		mustCreate(t, core, &nib.Nib{ID: "task-ax-u", Title: "Assigned", Type: "task", Status: "todo", Milestone: "ms-ax"})
		newType := "milestone"
		if _, err := resolver.Mutation().UpdateNib(ctx, "task-ax-u", model.UpdateNibInput{Type: &newType}); err == nil {
			t.Fatal("expected refusal")
		}
		stored, err := resolver.Query().Nib(ctx, "task-ax-u")
		if err != nil {
			t.Fatalf("re-read: %v", err)
		}
		if stored.Type != "task" || stored.Milestone != "ms-ax" {
			t.Errorf("stored nib mutated by refused retype: type=%q milestone=%q", stored.Type, stored.Milestone)
		}
	})

	t.Run("assigned nib can change between work types", func(t *testing.T) {
		mustCreate(t, core, &nib.Nib{ID: "task-ax-ok", Title: "Assigned", Type: "task", Status: "todo", Milestone: "ms-ax", Area: "web/ui"})
		newType := "bug"
		got, err := resolver.Mutation().UpdateNib(ctx, "task-ax-ok", model.UpdateNibInput{Type: &newType})
		if err != nil {
			t.Fatalf("UpdateNib(type=bug) on an assigned nib refused: %v", err)
		}
		if got.Type != "bug" {
			t.Errorf("type = %q, want bug", got.Type)
		}
	})
}
