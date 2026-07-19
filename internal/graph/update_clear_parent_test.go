package graph

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/executor"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibtypes"
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

// TestUpdateNibNoOpTypePreservesInvalidParent pins no-op type handling: a nib
// that already has a pre-existing INVALID parent (e.g. a bug parented under a
// feature — a state the resolver never created but the fixture generator and
// core.Create can) must still be editable. The web edit form always sends `type`
// unconditionally, so a title-only change arrives with `type` PRESENT but EQUAL
// to the current type. That no-op type submission must NOT re-validate the
// untouched (already-invalid) parent relationship — otherwise every edit dead-
// ends with the hierarchy error and the nib becomes uneditable.
//
// This case pins the EXPLICIT-type no-op (raw == effective, "bug" == "bug"); the
// raw-vs-effective refinement (a type-less nib echoing back "task") is pinned
// separately by TestUpdateNibNoOpTypePreservesInvalidParentTypeless. Keep both:
// this one guards that a no-op gate exists at all (it fails if the gate is
// removed), the sibling guards that the gate compares EffectiveType().
func TestUpdateNibNoOpTypePreservesInvalidParent(t *testing.T) {
	resolver, core := setupTestResolver(t)
	// core.Create does NOT validate parent-type hierarchy (that lives at the
	// resolver layer), so we can construct the invalid bug-under-feature state
	// directly — exactly how the fixture generator seeds such relationships.
	mustCreate(t, core, &nib.Nib{ID: "abo2-feat", Title: "Feature", Status: "todo", Type: "feature"})
	mustCreate(t, core, &nib.Nib{ID: "abo2-bug", Title: "Bug", Status: "todo", Type: "bug", Parent: "abo2-feat"})

	// Title change with type PRESENT but UNCHANGED (bug == bug) — what the web
	// form sends. This must succeed without re-validating the invalid parent.
	res := execUpdateNibParent(t, resolver, map[string]any{
		"id":    "abo2-bug",
		"input": map[string]any{"title": "Renamed", "type": "bug"},
	})

	// The invalid parent must be retained on the wire result.
	if got := wireParent(res); got != "abo2-feat" {
		t.Errorf("wire parentId = %q, want %q (invalid parent must be retained)", got, "abo2-feat")
	}
	if res.Type != "bug" {
		t.Errorf("wire type = %q, want %q", res.Type, "bug")
	}

	// The stored nib must reflect the title change and keep its parent.
	got, err := core.Get("abo2-bug")
	if err != nil {
		t.Fatalf("core.Get: %v", err)
	}
	if got.Title != "Renamed" {
		t.Errorf("stored Title = %q, want %q", got.Title, "Renamed")
	}
	if got.Parent != "abo2-feat" {
		t.Errorf("stored Parent = %q, want %q (invalid parent must be retained)", got.Parent, "abo2-feat")
	}
}

// TestUpdateNibNoOpTypePreservesInvalidParentTypeless is the type-less sibling of
// TestUpdateNibNoOpTypePreservesInvalidParent. It pins that no-op detection uses
// the EFFECTIVE type, not the raw stored Type. A nib whose front matter omits
// `type:` legitimately carries raw Type == "" (an intentionally preserved on-disk
// state), but EffectiveType() — and hence the GraphQL Nib.type field the web form
// reads back — normalizes it to "task". So a title-only edit of such a nib echoes
// back type:"task"; the raw-string gate would see "" != "task" and mis-classify a
// semantic no-op (task -> task) as a real type change, re-validating and dead-
// ending on the untouched (pre-existing invalid) parent. Constructing the invalid
// state: a type-less child (effective "task") under a research parent — research
// is NOT a valid parent for a task, so this is invalid, yet must remain editable.
func TestUpdateNibNoOpTypePreservesInvalidParentTypeless(t *testing.T) {
	resolver, core := setupTestResolver(t)
	// core.Create does NOT validate parent-type hierarchy, so we can seed the
	// invalid type-less-child-under-research state directly.
	mustCreate(t, core, &nib.Nib{ID: "abo2-research", Title: "Research", Status: "todo", Type: "research"})
	mustCreate(t, core, &nib.Nib{ID: "abo2-typeless", Title: "Typeless", Status: "todo", Type: "", Parent: "abo2-research"})

	// Title change with type PRESENT as "task" — exactly what the web form echoes
	// back for a type-less nib (Nib.type resolves to EffectiveType() == "task").
	// This is a semantic no-op and must succeed without re-validating the parent.
	res := execUpdateNibParent(t, resolver, map[string]any{
		"id":    "abo2-typeless",
		"input": map[string]any{"title": "Renamed", "type": "task"},
	})

	// The invalid parent must be retained on the wire result.
	if got := wireParent(res); got != "abo2-research" {
		t.Errorf("wire parentId = %q, want %q (invalid parent must be retained)", got, "abo2-research")
	}
	if res.Type != "task" {
		t.Errorf("wire type = %q, want %q", res.Type, "task")
	}

	// The stored nib must reflect the title change and keep its parent.
	got, err := core.Get("abo2-typeless")
	if err != nil {
		t.Fatalf("core.Get: %v", err)
	}
	if got.Title != "Renamed" {
		t.Errorf("stored Title = %q, want %q", got.Title, "Renamed")
	}
	if got.Parent != "abo2-research" {
		t.Errorf("stored Parent = %q, want %q (invalid parent must be retained)", got.Parent, "abo2-research")
	}
}

// TestUpdateNibRealInvalidatingTypeChangeStillErrors is the companion guard to
// TestUpdateNibNoOpTypePreservesInvalidParent: the resolver gates parent
// re-validation on an ACTUAL type change, so we must confirm a genuine
// type change that invalidates the existing parent is STILL rejected. A direct
// resolver call is used because execUpdateNibParent fatals on any mutation error.
func TestUpdateNibRealInvalidatingTypeChangeStillErrors(t *testing.T) {
	resolver, core := setupTestResolver(t)
	// task-under-feature is a VALID hierarchy.
	mustCreate(t, core, &nib.Nib{ID: "abo2-feat2", Title: "Feature", Status: "todo", Type: "feature"})
	mustCreate(t, core, &nib.Nib{ID: "abo2-task", Title: "Task", Status: "todo", Type: "task", Parent: "abo2-feat2"})

	// Changing the task to an epic makes the hierarchy invalid (an epic may only
	// sit under a milestone), with the parent OMITTED (zero Omittable). This is a
	// REAL type change, so the parent re-validation must fire and reject it.
	ctx := context.Background()
	mr := resolver.Mutation()
	epicType := "epic"
	_, err := mr.UpdateNib(ctx, "abo2-task", model.UpdateNibInput{Type: &epicType})
	if err == nil {
		t.Fatal("expected error changing task to epic under a feature parent (invalid hierarchy), got nil")
	}
	// Assert the SPECIFIC error so the test's claim is self-verifying: the
	// hierarchy guard returns an unwrapped *nibtypes.HierarchyError, so a future
	// refactor that introduced an earlier unrelated failure on this input would
	// no longer satisfy this and the guard's coverage would not silently rot.
	var hierarchyErr *nibtypes.HierarchyError
	if !errors.As(err, &hierarchyErr) {
		t.Errorf("UpdateNib() error = %v, want a *nibtypes.HierarchyError", err)
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
