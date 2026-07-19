package graph

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/executor"
	"github.com/alphaleonis/nibs/internal/nib"
)

// execUpdateNib runs the updateNib mutation through the real gqlgen executor so
// the JSON null-vs-omitted distinction is exercised end-to-end (the resolver
// receives exactly what the wire produced, not a hand-built input struct).
func execUpdateNib(t *testing.T, resolver *Resolver, variables map[string]any) {
	t.Helper()
	es := NewExecutableSchema(Config{Resolvers: resolver})
	exec := executor.New(es)

	ctx := graphql.StartOperationTrace(context.Background())
	params := &graphql.RawParams{
		Query: `mutation Update($id: ID!, $input: UpdateNibInput!) {
			updateNib(id: $id, input: $input) { id priority estimate }
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
	// Sanity-check the response is well-formed JSON.
	var data map[string]any
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		t.Fatalf("unmarshal response data: %v", err)
	}
}

// TestUpdateNibNullClearsField pins null-vs-omit semantics: an explicit JSON `null` for a
// clearable optional string field (priority/estimate) must CLEAR the stored
// value, while an OMITTED field must leave it unchanged. Driven through the
// real executor because the null-vs-omit distinction only exists on the wire —
// a resolver-level test with a *string input can't tell them apart.
func TestUpdateNibNullClearsField(t *testing.T) {
	tests := []struct {
		name         string
		startPri     string
		startEst     string
		input        map[string]any
		wantPriority string
		wantEstimate string
	}{
		{
			name:         "explicit null priority clears it",
			startPri:     "high",
			startEst:     "l",
			input:        map[string]any{"priority": nil},
			wantPriority: "", // cleared
			wantEstimate: "l",
		},
		{
			name:         "explicit null estimate clears it",
			startPri:     "high",
			startEst:     "l",
			input:        map[string]any{"estimate": nil},
			wantPriority: "high",
			wantEstimate: "", // cleared
		},
		{
			name:         "omitted priority leaves it unchanged",
			startPri:     "high",
			startEst:     "l",
			input:        map[string]any{"status": "in-progress"},
			wantPriority: "high", // unchanged (guard against over-clearing)
			wantEstimate: "l",
		},
		{
			name:         "explicit value sets priority",
			startPri:     "high",
			startEst:     "l",
			input:        map[string]any{"priority": "low"},
			wantPriority: "low",
			wantEstimate: "l",
		},
		{
			name:         "null both clears both",
			startPri:     "critical",
			startEst:     "xl",
			input:        map[string]any{"priority": nil, "estimate": nil},
			wantPriority: "",
			wantEstimate: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver, core := setupTestResolver(t)
			b := &nib.Nib{
				ID:       "clear-test",
				Title:    "Clear Test",
				Status:   "todo",
				Type:     "task",
				Priority: tt.startPri,
				Estimate: tt.startEst,
			}
			mustCreate(t, core, b)

			execUpdateNib(t, resolver, map[string]any{
				"id":    "clear-test",
				"input": tt.input,
			})

			got, err := core.Get("clear-test")
			if err != nil {
				t.Fatalf("core.Get: %v", err)
			}
			if got.Priority != tt.wantPriority {
				t.Errorf("stored Priority = %q, want %q", got.Priority, tt.wantPriority)
			}
			if got.Estimate != tt.wantEstimate {
				t.Errorf("stored Estimate = %q, want %q", got.Estimate, tt.wantEstimate)
			}
		})
	}
}
