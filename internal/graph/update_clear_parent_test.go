package graph

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/executor"
	"github.com/alphaleonis/nibs/internal/nib"
)

// updateNibParentResult is the decoded on-the-wire updateNib response for the
// fields execUpdateNibParent selects. parentId is nil when the resolver returns
// GraphQL null (nib has no parent).
type updateNibParentResult struct {
	ID       string  `json:"id"`
	ParentID *string `json:"parentId"`
	Type     string  `json:"type"`
}

// execUpdateNibParent runs the updateNib mutation through the real gqlgen
// executor, selecting parentId so the JSON null-vs-omitted distinction on the
// parent field is exercised end-to-end. A resolver-level test that hands the
// resolver a pre-built input struct cannot reproduce the wire-level difference
// between an explicit `null` and an omitted field. The decoded response is
// returned so callers can assert on the wire result, not just the stored nib.
func execUpdateNibParent(t *testing.T, resolver *Resolver, variables map[string]any) updateNibParentResult {
	t.Helper()
	es := NewExecutableSchema(Config{Resolvers: resolver})
	exec := executor.New(es)

	ctx := graphql.StartOperationTrace(context.Background())
	params := &graphql.RawParams{
		Query: `mutation Update($id: ID!, $input: UpdateNibInput!) {
			updateNib(id: $id, input: $input) { id parentId type }
		}`,
		Variables: variables,
	}

	opCtx, errs := exec.CreateOperationContext(ctx, params)
	if len(errs) > 0 {
		t.Fatalf("CreateOperationContext: %v", errs)
	}
	ctx = graphql.WithOperationContext(ctx, opCtx)
	handler, ctx := exec.DispatchOperation(ctx, opCtx)
	resp := handler(ctx)
	if len(resp.Errors) > 0 {
		t.Fatalf("updateNib mutation errors: %v", resp.Errors)
	}
	var data struct {
		UpdateNib updateNibParentResult `json:"updateNib"`
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		t.Fatalf("unmarshal response data: %v", err)
	}
	return data.UpdateNib
}

// wireParent flattens a wire parentId (nil → "") for comparison against the
// stored Nib.Parent, which uses "" for the root.
func wireParent(r updateNibParentResult) string {
	if r.ParentID == nil {
		return ""
	}
	return *r.ParentID
}

// TestUpdateNibParentNullClears pins the clear-semantics for the parent field:
// an explicit JSON `null` (or empty string) must CLEAR the parent (move the nib
// to root), an OMITTED parent must leave it unchanged, and a non-empty id must
// set it. Driven through the real executor because the null-vs-omit distinction
// only exists on the wire.
//
// The "type change + explicit null clear composes" case additionally pins the
// interaction that the `!input.Parent.IsSet()` guard governs: with an explicit
// parent:null, the type-change path must NOT re-validate the OLD parent against
// the new type (that path is for an OMITTED parent). If the guard were reverted
// to treat set-null like omitted, changing child-x to `epic` while its stored
// parent is still the epic par-1 would fail (epic may only sit under a
// milestone) — so this case fails loudly if the guard regresses.
func TestUpdateNibParentNullClears(t *testing.T) {
	tests := []struct {
		name       string
		input      map[string]any
		wantParent string // "" means root
		wantType   string
	}{
		{
			name:       "explicit null clears the parent",
			input:      map[string]any{"parent": nil},
			wantParent: "",
			wantType:   "task",
		},
		{
			name:       "empty string clears the parent",
			input:      map[string]any{"parent": ""},
			wantParent: "",
			wantType:   "task",
		},
		{
			name:       "omitted parent leaves it unchanged",
			input:      map[string]any{"status": "in-progress"},
			wantParent: "par-1",
			wantType:   "task",
		},
		{
			name:       "non-empty id sets the parent",
			input:      map[string]any{"parent": "par-2"},
			wantParent: "par-2",
			wantType:   "task",
		},
		{
			name:       "type change + explicit null clear composes",
			input:      map[string]any{"type": "epic", "parent": nil},
			wantParent: "", // parent cleared to root; NOT re-validated against the new type
			wantType:   "epic",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver, core := setupTestResolver(t)
			mustCreate(t, core, &nib.Nib{ID: "par-1", Title: "Parent 1", Status: "todo", Type: "epic"})
			mustCreate(t, core, &nib.Nib{ID: "par-2", Title: "Parent 2", Status: "todo", Type: "epic"})
			mustCreate(t, core, &nib.Nib{ID: "child-x", Title: "Child", Status: "todo", Type: "task", Parent: "par-1"})

			res := execUpdateNibParent(t, resolver, map[string]any{
				"id":    "child-x",
				"input": tt.input,
			})

			// Assert the on-the-wire response (parentId is null for root).
			if got := wireParent(res); got != tt.wantParent {
				t.Errorf("wire parentId = %q, want %q", got, tt.wantParent)
			}
			if res.Type != tt.wantType {
				t.Errorf("wire type = %q, want %q", res.Type, tt.wantType)
			}

			// Assert the stored nib matches the wire response.
			got, err := core.Get("child-x")
			if err != nil {
				t.Fatalf("core.Get: %v", err)
			}
			if got.Parent != tt.wantParent {
				t.Errorf("stored Parent = %q, want %q", got.Parent, tt.wantParent)
			}
			if got.EffectiveType() != tt.wantType {
				t.Errorf("stored Type = %q, want %q", got.EffectiveType(), tt.wantType)
			}
		})
	}
}

// TestUpdateNibOmittedParentTypeRevalidation guards the "re-validate existing
// parent when the type changes" path: when the type changes and the parent is
// OMITTED (not being changed), the existing parent must be re-validated against
// the new type and left in place. Making parent omittable must not break this.
func TestUpdateNibOmittedParentTypeRevalidation(t *testing.T) {
	resolver, core := setupTestResolver(t)
	mustCreate(t, core, &nib.Nib{ID: "rev-epic", Title: "Epic", Status: "todo", Type: "epic"})
	mustCreate(t, core, &nib.Nib{ID: "rev-child", Title: "Child", Status: "todo", Type: "task", Parent: "rev-epic"})

	// Change the child's type to "feature" (valid under an epic parent) with the
	// parent OMITTED — the existing parent must be re-validated and retained.
	res := execUpdateNibParent(t, resolver, map[string]any{
		"id":    "rev-child",
		"input": map[string]any{"type": "feature"},
	})
	if got := wireParent(res); got != "rev-epic" {
		t.Errorf("wire parentId = %q, want %q (parent must be unchanged when omitted)", got, "rev-epic")
	}
	if res.Type != "feature" {
		t.Errorf("wire type = %q, want %q", res.Type, "feature")
	}

	got, err := core.Get("rev-child")
	if err != nil {
		t.Fatalf("core.Get: %v", err)
	}
	if got.Parent != "rev-epic" {
		t.Errorf("stored Parent = %q, want %q (parent must be unchanged when omitted)", got.Parent, "rev-epic")
	}
	if got.Type != "feature" {
		t.Errorf("stored Type = %q, want %q", got.Type, "feature")
	}
}
