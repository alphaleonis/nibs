package graph

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/executor"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
)

// unkeyedGroupFixture creates a parent with one keyed child and three unkeyed
// ones, plus an unparented task and a milestone queue of three unkeyed tasks.
// "Aardvark" and the keyed "Zulu" invert title order, so only a persisted key
// puts either one where a write placed it.
func unkeyedGroupFixture(t *testing.T) (*Resolver, *nibcore.Core) {
	t.Helper()
	resolver, core := setupTestResolver(t)
	for _, b := range []*nib.Nib{
		{ID: "par", Title: "Parent", Status: "todo", Type: "epic"},
		{ID: "kzulu", Title: "Zulu", Status: "todo", Type: "task", Parent: "par", Order: nib.OrderInitial()},
		{ID: "ucherry", Title: "Cherry", Status: "todo", Type: "task", Parent: "par"},
		{ID: "uapple", Title: "Apple", Status: "todo", Type: "task", Parent: "par"},
		{ID: "ubanana", Title: "Banana", Status: "todo", Type: "task", Parent: "par"},
		{ID: "outsider", Title: "Aardvark", Status: "todo", Type: "task"},
		{ID: "ms", Title: "Release", Status: "todo", Type: "milestone"},
		{ID: "qcherry", Title: "Cherry", Status: "todo", Type: "task", Milestone: "ms"},
		{ID: "qapple", Title: "Apple", Status: "todo", Type: "task", Milestone: "ms"},
		{ID: "qbanana", Title: "Banana", Status: "todo", Type: "task", Milestone: "ms"},
	} {
		b.Version = 1
		mustCreate(t, core, b)
	}
	return resolver, core
}

// TestChildrenQueryWritesNothing runs the children field through the real
// executor, the path `nibs query`, `nibs rel --rel children` and the web UI share.
func TestChildrenQueryWritesNothing(t *testing.T) {
	resolver, core := unkeyedGroupFixture(t)
	before := storeSnapshot(t, core.Root())

	es := NewExecutableSchema(Config{Resolvers: resolver})
	exec := executor.New(es)
	ctx := graphql.StartOperationTrace(context.Background())
	opCtx, errs := exec.CreateOperationContext(ctx, &graphql.RawParams{
		Query: `{ nib(id: "par") { children { id order } } }`,
	})
	if len(errs) > 0 {
		t.Fatalf("CreateOperationContext: %v", errs)
	}
	ctx = graphql.WithOperationContext(ctx, opCtx)
	handler, ctx := exec.DispatchOperation(ctx, opCtx)
	resp := handler(ctx)
	if len(resp.Errors) > 0 {
		t.Fatalf("children query errors: %v", resp.Errors)
	}

	assertStoreUnchanged(t, before, core.Root())

	var data struct {
		Nib struct {
			Children []struct {
				ID    string `json:"id"`
				Order string `json:"order"`
			} `json:"children"`
		} `json:"nib"`
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var ids []string
	for _, c := range data.Nib.Children {
		ids = append(ids, c.ID)
		if c.ID != "kzulu" && c.Order != "" {
			t.Errorf("%s reports order %q; a read must not key a member, even in memory", c.ID, c.Order)
		}
	}
	if want := []string{"kzulu", "uapple", "ubanana", "ucherry"}; !slices.Equal(ids, want) {
		t.Errorf("children = %v, want %v (keyed first, then unkeyed by title)", ids, want)
	}
}

// TestPositionedWritesPersistTheGroupOrder drives each write that positions a
// nib among unkeyed members, then reads the order back from a fresh load. A key
// computed against members whose keys exist only in memory would sort the moved
// nib ahead of them on that reload.
func TestPositionedWritesPersistTheGroupOrder(t *testing.T) {
	str := func(s string) *string { return &s }
	yes := true
	taskType := "task"

	tests := []struct {
		name  string
		scope Scope
		group string
		write func(ctx context.Context, r *Resolver) error
		want  []string
	}{
		{
			name: "createNib after an unkeyed sibling", scope: ScopeParent, group: "par",
			write: func(ctx context.Context, r *Resolver) error {
				m := r.Mutation()
				_, err := m.CreateNib(ctx, model.CreateNibInput{Title: "Aaa new", Type: &taskType, Parent: str("par"), AfterID: str("uapple")})
				return err
			},
			want: []string{"kzulu", "uapple", "NEW", "ubanana", "ucherry"},
		},
		{
			name: "createNib with default placement", scope: ScopeParent, group: "par",
			write: func(ctx context.Context, r *Resolver) error {
				m := r.Mutation()
				_, err := m.CreateNib(ctx, model.CreateNibInput{Title: "Aaa new", Type: &taskType, Parent: str("par")})
				return err
			},
			want: []string{"kzulu", "uapple", "ubanana", "ucherry", "NEW"},
		},
		{
			name: "reorderNib after an unkeyed sibling", scope: ScopeParent, group: "par",
			write: func(ctx context.Context, r *Resolver) error {
				m := r.Mutation()
				_, err := m.ReorderNib(ctx, "ucherry", str("uapple"), nil, nil, nil, nil, model.OrderScopeParent)
				return err
			},
			want: []string{"kzulu", "uapple", "ucherry", "ubanana"},
		},
		{
			// The subject's own file must stay as the caller's etag saw it.
			name: "reorderNib of an unkeyed nib under if-match", scope: ScopeParent, group: "par",
			write: func(ctx context.Context, r *Resolver) error {
				m := r.Mutation()
				subject, err := r.Reader.Get("ucherry")
				if err != nil {
					return err
				}
				etag := subject.ETag()
				_, err = m.ReorderNib(ctx, "ucherry", str("uapple"), nil, nil, nil, &etag, model.OrderScopeParent)
				return err
			},
			want: []string{"kzulu", "uapple", "ucherry", "ubanana"},
		},
		{
			name: "reorderNib first", scope: ScopeParent, group: "par",
			write: func(ctx context.Context, r *Resolver) error {
				m := r.Mutation()
				_, err := m.ReorderNib(ctx, "ubanana", nil, nil, &yes, nil, nil, model.OrderScopeParent)
				return err
			},
			want: []string{"ubanana", "kzulu", "uapple", "ucherry"},
		},
		{
			name: "reorderSiblings before an unkeyed anchor", scope: ScopeParent, group: "par",
			write: func(ctx context.Context, r *Resolver) error {
				m := r.Mutation()
				_, err := m.ReorderSiblings(ctx, []string{"ucherry"}, nil, str("ubanana"), nil, nil)
				return err
			},
			want: []string{"kzulu", "uapple", "ucherry", "ubanana"},
		},
		{
			name: "reorderSiblings after an unkeyed anchor", scope: ScopeParent, group: "par",
			write: func(ctx context.Context, r *Resolver) error {
				m := r.Mutation()
				_, err := m.ReorderSiblings(ctx, []string{"uapple"}, str("ubanana"), nil, nil, nil)
				return err
			},
			want: []string{"kzulu", "ubanana", "uapple", "ucherry"},
		},
		{
			name: "setParent into a group of unkeyed children", scope: ScopeParent, group: "par",
			write: func(ctx context.Context, r *Resolver) error {
				m := r.Mutation()
				_, err := m.SetParent(ctx, "outsider", str("par"), nil)
				return err
			},
			want: []string{"kzulu", "uapple", "ubanana", "ucherry", "outsider"},
		},
		{
			name: "reorderNib within an unkeyed milestone queue", scope: ScopeMilestone, group: "ms",
			write: func(ctx context.Context, r *Resolver) error {
				m := r.Mutation()
				_, err := m.ReorderNib(ctx, "qcherry", nil, str("qbanana"), nil, nil, nil, model.OrderScopeMilestone)
				return err
			},
			want: []string{"qapple", "qcherry", "qbanana"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver, core := unkeyedGroupFixture(t)
			if err := tt.write(context.Background(), resolver); err != nil {
				t.Fatalf("write: %v", err)
			}

			reloaded := nibcore.New(core.Root(), config.Default())
			if err := reloaded.Load(); err != nil {
				t.Fatalf("reload: %v", err)
			}
			var got []string
			for _, b := range NewOrderer(reloaded, reloaded).Members(tt.scope, tt.group) {
				id := b.ID
				if b.Title == "Aaa new" {
					id = "NEW"
				}
				got = append(got, id)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("order after reload = %v, want %v", got, tt.want)
			}
		})
	}
}
