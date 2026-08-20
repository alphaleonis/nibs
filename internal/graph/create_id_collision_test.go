package graph

import (
	"context"
	"testing"

	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
)

// TestCreateNibCustomPrefixRetriesCollision pins the resolver half of the
// nibs-kafe guard: the custom-prefix path pre-generates the id itself, so a
// colliding draw there must be redrawn against the store rather than handed
// to Create — whose duplicate refusal would turn a random internal collision
// into a user-facing conflict error.
func TestCreateNibCustomPrefixRetriesCollision(t *testing.T) {
	resolver, core := setupTestResolver(t)
	taken := &nib.Nib{ID: "PX-tkn1", Slug: "taken", Title: "Original", Status: "todo"}
	mustCreate(t, core, taken)

	original := newPrefixedNibID
	t.Cleanup(func() { newPrefixedNibID = original })
	draws := []string{"PX-tkn1", "PX-frs1"}
	i := 0
	newPrefixedNibID = func(prefix string, length int) string {
		if i >= len(draws) {
			t.Fatalf("generator drawn %d times, only %d seeded", i+1, len(draws))
		}
		id := draws[i]
		i++
		return id
	}

	prefix := "PX-"
	got, err := resolver.Mutation().CreateNib(context.Background(), model.CreateNibInput{Title: "Newcomer", Prefix: &prefix})
	if err != nil {
		t.Fatalf("CreateNib() error = %v — a colliding prefixed draw must be redrawn", err)
	}
	if got.ID != "PX-frs1" {
		t.Errorf("CreateNib().ID = %q, want the redrawn PX-frs1", got.ID)
	}
	if orig, gerr := core.Get("PX-tkn1"); gerr != nil || orig.Title != "Original" {
		t.Errorf("the original nib was disturbed: %v, %v", orig, gerr)
	}
}
