package graph

import (
	"context"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
)

// setupBulkReorderFixture builds a parent with three ordered children (a, b, c)
// for Mode A bulk-children scenarios.
func setupBulkReorderFixture(t *testing.T) (*Resolver, *nibcore.Core, string) {
	t.Helper()
	resolver, core := setupTestResolver(t)
	parent := &nib.Nib{ID: "epic1", Title: "Epic", Status: "todo", Type: "epic", Version: 1}
	if err := core.Create(parent); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ id, title, order string }{
		{"a", "First", "a0"}, {"b", "Second", "b0"}, {"c", "Third", "c0"},
	} {
		child := &nib.Nib{ID: c.id, Title: c.title, Status: "todo", Type: "task", Parent: "epic1", Order: c.order, Version: 1}
		if err := core.Create(child); err != nil {
			t.Fatal(err)
		}
	}
	return resolver, core, "epic1"
}

func TestReorderChildren_MissingIDs(t *testing.T) {
	ctx := context.Background()
	resolver, _, parentID := setupBulkReorderFixture(t)

	_, err := resolver.Mutation().ReorderChildren(ctx, parentID, []string{"a", "b"})
	if err == nil {
		t.Fatal("expected error for missing children")
	}
	if !strings.Contains(err.Error(), "c") {
		t.Errorf("error should mention missing id 'c'; got: %v", err)
	}
}

func TestReorderChildren_NonExistentID(t *testing.T) {
	ctx := context.Background()
	resolver, _, parentID := setupBulkReorderFixture(t)

	_, err := resolver.Mutation().ReorderChildren(ctx, parentID, []string{"a", "b", "x"})
	if err == nil {
		t.Fatal("expected error for non-existent child")
	}
	if !strings.Contains(err.Error(), "x") {
		t.Errorf("error should mention non-existent id 'x'; got: %v", err)
	}
}

func TestReorderChildren_WrongParent(t *testing.T) {
	ctx := context.Background()
	resolver, core, parentID := setupBulkReorderFixture(t)

	otherParent := &nib.Nib{ID: "epic2", Title: "Epic 2", Status: "todo", Type: "epic", Version: 1}
	if err := core.Create(otherParent); err != nil {
		t.Fatal(err)
	}
	otherChild := &nib.Nib{ID: "x1", Title: "X1", Status: "todo", Type: "task", Parent: "epic2", Order: "a0", Version: 1}
	if err := core.Create(otherChild); err != nil {
		t.Fatal(err)
	}

	_, err := resolver.Mutation().ReorderChildren(ctx, parentID, []string{"a", "b", "c", "x1"})
	if err == nil {
		t.Fatal("expected error for child of different parent")
	}
	if !strings.Contains(err.Error(), "x1") {
		t.Errorf("error should mention misplaced id 'x1'; got: %v", err)
	}
}

func TestReorderChildren_DuplicateID(t *testing.T) {
	ctx := context.Background()
	resolver, _, parentID := setupBulkReorderFixture(t)

	_, err := resolver.Mutation().ReorderChildren(ctx, parentID, []string{"a", "b", "b"})
	if err == nil {
		t.Fatal("expected error for duplicate id")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error should mention duplicate; got: %v", err)
	}
	if !strings.Contains(err.Error(), "b") {
		t.Errorf("error should mention duplicated id 'b'; got: %v", err)
	}
}

func TestReorderChildren_RootParent(t *testing.T) {
	ctx := context.Background()
	resolver, core := setupTestResolver(t)
	// Three root-level (no parent) nibs.
	for _, c := range []struct{ id, title, order string }{
		{"r1", "Root 1", "a0"}, {"r2", "Root 2", "b0"}, {"r3", "Root 3", "c0"},
	} {
		root := &nib.Nib{ID: c.id, Title: c.title, Status: "todo", Type: "task", Order: c.order, Version: 1}
		if err := core.Create(root); err != nil {
			t.Fatal(err)
		}
	}

	got, err := resolver.Mutation().ReorderChildren(ctx, "", []string{"r3", "r1", "r2"})
	if err != nil {
		t.Fatalf("ReorderChildren error: %v", err)
	}
	wantIDs := []string{"r3", "r1", "r2"}
	if len(got) != len(wantIDs) {
		t.Fatalf("got %d nibs, want %d", len(got), len(wantIDs))
	}
	for i, b := range got {
		if b.ID != wantIDs[i] {
			t.Errorf("got[%d].ID = %q, want %q", i, b.ID, wantIDs[i])
		}
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].Order >= got[i].Order {
			t.Errorf("order keys not strictly increasing at %d/%d: %q vs %q",
				i-1, i, got[i-1].Order, got[i].Order)
		}
	}
}

func TestReorderChildren_SingleChild(t *testing.T) {
	ctx := context.Background()
	resolver, core := setupTestResolver(t)
	parent := &nib.Nib{ID: "epic1", Title: "Epic", Status: "todo", Type: "epic", Version: 1}
	if err := core.Create(parent); err != nil {
		t.Fatal(err)
	}
	child := &nib.Nib{ID: "a", Title: "Only", Status: "todo", Type: "task", Parent: "epic1", Order: "a0", Version: 1}
	if err := core.Create(child); err != nil {
		t.Fatal(err)
	}

	got, err := resolver.Mutation().ReorderChildren(ctx, "epic1", []string{"a"})
	if err != nil {
		t.Fatalf("ReorderChildren error: %v", err)
	}
	if len(got) != 1 || got[0].ID != "a" {
		t.Errorf("got %v, want [a]", got)
	}
}

// setupBlockMoveFixture: parent has 5 ordered children [a, b, c, d, e].
func setupBlockMoveFixture(t *testing.T) (*Resolver, *nibcore.Core, string) {
	t.Helper()
	resolver, core := setupTestResolver(t)
	parent := &nib.Nib{ID: "epic1", Title: "Epic", Status: "todo", Type: "epic", Version: 1}
	if err := core.Create(parent); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ id, title, order string }{
		{"a", "A", "a0"}, {"b", "B", "b0"}, {"c", "C", "c0"}, {"d", "D", "d0"}, {"e", "E", "e0"},
	} {
		child := &nib.Nib{ID: c.id, Title: c.title, Status: "todo", Type: "task", Parent: "epic1", Order: c.order, Version: 1}
		if err := core.Create(child); err != nil {
			t.Fatal(err)
		}
	}
	return resolver, core, "epic1"
}

func boolPtr(b bool) *bool { return &b }

func TestReorderSiblings_AfterIdTracer(t *testing.T) {
	ctx := context.Background()
	resolver, _, parentID := setupBlockMoveFixture(t)

	got, err := resolver.Mutation().ReorderSiblings(ctx, []string{"c", "e"}, strPtr("a"), nil, nil)
	if err != nil {
		t.Fatalf("ReorderSiblings error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d nibs, want 2", len(got))
	}
	wantBlock := []string{"c", "e"}
	for i, b := range got {
		if b.ID != wantBlock[i] {
			t.Errorf("got[%d].ID = %q, want %q", i, b.ID, wantBlock[i])
		}
	}

	// Re-read siblings — order should be [a, c, e, b, d]
	siblings := resolver.Orderer.GetSortedSiblings(parentID)
	want := []string{"a", "c", "e", "b", "d"}
	if len(siblings) != len(want) {
		t.Fatalf("got %d siblings, want %d", len(siblings), len(want))
	}
	for i, b := range siblings {
		if b.ID != want[i] {
			t.Errorf("siblings[%d].ID = %q, want %q (order=%q)", i, b.ID, want[i], b.Order)
		}
	}
}

func TestReorderSiblings_BeforeId(t *testing.T) {
	ctx := context.Background()
	resolver, _, parentID := setupBlockMoveFixture(t)

	got, err := resolver.Mutation().ReorderSiblings(ctx, []string{"c", "e"}, nil, strPtr("a"), nil)
	if err != nil {
		t.Fatalf("ReorderSiblings error: %v", err)
	}
	if len(got) != 2 || got[0].ID != "c" || got[1].ID != "e" {
		t.Errorf("got %+v, want block [c, e]", got)
	}

	// Re-read siblings — order should be [c, e, a, b, d]
	siblings := resolver.Orderer.GetSortedSiblings(parentID)
	want := []string{"c", "e", "a", "b", "d"}
	if len(siblings) != len(want) {
		t.Fatalf("got %d siblings, want %d", len(siblings), len(want))
	}
	for i, b := range siblings {
		if b.ID != want[i] {
			t.Errorf("siblings[%d].ID = %q, want %q (order=%q)", i, b.ID, want[i], b.Order)
		}
	}
}

func TestReorderSiblings_First(t *testing.T) {
	ctx := context.Background()
	resolver, _, parentID := setupBlockMoveFixture(t)

	got, err := resolver.Mutation().ReorderSiblings(ctx, []string{"c", "e"}, nil, nil, boolPtr(true))
	if err != nil {
		t.Fatalf("ReorderSiblings error: %v", err)
	}
	if len(got) != 2 || got[0].ID != "c" || got[1].ID != "e" {
		t.Errorf("got %+v, want block [c, e]", got)
	}

	// Re-read siblings — order should be [c, e, a, b, d]
	siblings := resolver.Orderer.GetSortedSiblings(parentID)
	want := []string{"c", "e", "a", "b", "d"}
	if len(siblings) != len(want) {
		t.Fatalf("got %d siblings, want %d", len(siblings), len(want))
	}
	for i, b := range siblings {
		if b.ID != want[i] {
			t.Errorf("siblings[%d].ID = %q, want %q (order=%q)", i, b.ID, want[i], b.Order)
		}
	}
}

func TestReorderSiblings_AnchorInBlock(t *testing.T) {
	ctx := context.Background()
	resolver, _, _ := setupBlockMoveFixture(t)

	_, err := resolver.Mutation().ReorderSiblings(ctx, []string{"a", "c"}, strPtr("a"), nil, nil)
	if err == nil {
		t.Fatal("expected error when anchor appears in siblingIds")
	}
	if !strings.Contains(err.Error(), "anchor") {
		t.Errorf("error should mention 'anchor'; got: %v", err)
	}
}

func TestReorderSiblings_MixedParents(t *testing.T) {
	ctx := context.Background()
	resolver, core, _ := setupBlockMoveFixture(t)

	otherParent := &nib.Nib{ID: "epic2", Title: "Epic 2", Status: "todo", Type: "epic", Version: 1}
	if err := core.Create(otherParent); err != nil {
		t.Fatal(err)
	}
	otherChild := &nib.Nib{ID: "x1", Title: "X1", Status: "todo", Type: "task", Parent: "epic2", Order: "a0", Version: 1}
	if err := core.Create(otherChild); err != nil {
		t.Fatal(err)
	}

	_, err := resolver.Mutation().ReorderSiblings(ctx, []string{"a", "x1"}, strPtr("b"), nil, nil)
	if err == nil {
		t.Fatal("expected error for siblings with different parents")
	}
	if !strings.Contains(err.Error(), "parent") {
		t.Errorf("error should mention 'parent'; got: %v", err)
	}
}

func TestReorderSiblings_NonExistentSiblingID(t *testing.T) {
	ctx := context.Background()
	resolver, _, _ := setupBlockMoveFixture(t)

	_, err := resolver.Mutation().ReorderSiblings(ctx, []string{"c", "missing-z"}, strPtr("a"), nil, nil)
	if err == nil {
		t.Fatal("expected error for non-existent sibling")
	}
	if !strings.Contains(err.Error(), "missing-z") {
		t.Errorf("error should mention 'missing-z'; got: %v", err)
	}
}

func TestReorderSiblings_ModeMutex(t *testing.T) {
	ctx := context.Background()
	resolver, _, _ := setupBlockMoveFixture(t)

	t.Run("none specified", func(t *testing.T) {
		_, err := resolver.Mutation().ReorderSiblings(ctx, []string{"c", "e"}, nil, nil, nil)
		if err == nil {
			t.Fatal("expected error when no positioning flag is specified")
		}
	})

	t.Run("after and before", func(t *testing.T) {
		_, err := resolver.Mutation().ReorderSiblings(ctx, []string{"c", "e"}, strPtr("a"), strPtr("b"), nil)
		if err == nil {
			t.Fatal("expected error when both afterId and beforeId are specified")
		}
	})

	t.Run("after and first", func(t *testing.T) {
		_, err := resolver.Mutation().ReorderSiblings(ctx, []string{"c", "e"}, strPtr("a"), nil, boolPtr(true))
		if err == nil {
			t.Fatal("expected error when both afterId and first are specified")
		}
	})

	t.Run("before and first", func(t *testing.T) {
		_, err := resolver.Mutation().ReorderSiblings(ctx, []string{"c", "e"}, nil, strPtr("a"), boolPtr(true))
		if err == nil {
			t.Fatal("expected error when both beforeId and first are specified")
		}
	})

	t.Run("all three", func(t *testing.T) {
		_, err := resolver.Mutation().ReorderSiblings(ctx, []string{"c", "e"}, strPtr("a"), strPtr("b"), boolPtr(true))
		if err == nil {
			t.Fatal("expected error when all three positioning flags are specified")
		}
	})
}

// setupBulkReorderFixturePrefixed builds a parent + three children under a
// "nibs-" prefix so tests can exercise short-vs-full ID resolution paths.
func setupBulkReorderFixturePrefixed(t *testing.T) (*Resolver, *nibcore.Core, string) {
	t.Helper()
	resolver, core := setupTestResolverWithPrefix(t, "nibs-")
	parent := &nib.Nib{ID: "nibs-epic1", Title: "Epic", Status: "todo", Type: "epic", Version: 1}
	if err := core.Create(parent); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ id, title, order string }{
		{"nibs-a", "First", "a0"}, {"nibs-b", "Second", "b0"}, {"nibs-c", "Third", "c0"},
	} {
		child := &nib.Nib{ID: c.id, Title: c.title, Status: "todo", Type: "task", Parent: "nibs-epic1", Order: c.order, Version: 1}
		if err := core.Create(child); err != nil {
			t.Fatal(err)
		}
	}
	return resolver, core, "nibs-epic1"
}

// setupBlockMoveFixturePrefixed: parent + 5 children under "nibs-" prefix.
func setupBlockMoveFixturePrefixed(t *testing.T) (*Resolver, *nibcore.Core, string) {
	t.Helper()
	resolver, core := setupTestResolverWithPrefix(t, "nibs-")
	parent := &nib.Nib{ID: "nibs-epic1", Title: "Epic", Status: "todo", Type: "epic", Version: 1}
	if err := core.Create(parent); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ id, title, order string }{
		{"nibs-a", "A", "a0"}, {"nibs-b", "B", "b0"}, {"nibs-c", "C", "c0"}, {"nibs-d", "D", "d0"}, {"nibs-e", "E", "e0"},
	} {
		child := &nib.Nib{ID: c.id, Title: c.title, Status: "todo", Type: "task", Parent: "nibs-epic1", Order: c.order, Version: 1}
		if err := core.Create(child); err != nil {
			t.Fatal(err)
		}
	}
	return resolver, core, "nibs-epic1"
}

func TestReorderChildren_DuplicateAcrossForms(t *testing.T) {
	ctx := context.Background()
	resolver, _, parentID := setupBulkReorderFixturePrefixed(t)

	// Pass the same nib twice — once short, once full — under a configured
	// prefix. Without canonical-ID dedup, both forms slip through and the
	// completeness check passes but persistence is silently broken.
	_, err := resolver.Mutation().ReorderChildren(ctx, parentID, []string{"a", "nibs-a", "b", "c"})
	if err == nil {
		t.Fatal("expected error for duplicate id in cross-form input")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error should mention duplicate; got: %v", err)
	}
	if !strings.Contains(err.Error(), "nibs-a") {
		t.Errorf("error should mention canonical id 'nibs-a'; got: %v", err)
	}
}

func TestReorderSiblings_DuplicateAcrossForms(t *testing.T) {
	ctx := context.Background()
	resolver, _, _ := setupBlockMoveFixturePrefixed(t)

	// Same nib in two surface forms inside the block to move.
	_, err := resolver.Mutation().ReorderSiblings(ctx, []string{"c", "nibs-c"}, strPtr("a"), nil, nil)
	if err == nil {
		t.Fatal("expected error for duplicate id in cross-form sibling list")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error should mention duplicate; got: %v", err)
	}
	if !strings.Contains(err.Error(), "nibs-c") {
		t.Errorf("error should mention canonical id 'nibs-c'; got: %v", err)
	}
}

func TestReorderChildren_ShortIDsResolveCorrectly(t *testing.T) {
	ctx := context.Background()
	resolver, _, parentID := setupBulkReorderFixturePrefixed(t)

	// Pass short IDs (without prefix). Reorder should succeed and return
	// canonical (full-prefix) IDs in the requested order.
	got, err := resolver.Mutation().ReorderChildren(ctx, parentID, []string{"c", "a", "b"})
	if err != nil {
		t.Fatalf("ReorderChildren error: %v", err)
	}
	wantIDs := []string{"nibs-c", "nibs-a", "nibs-b"}
	if len(got) != len(wantIDs) {
		t.Fatalf("got %d nibs, want %d", len(got), len(wantIDs))
	}
	for i, b := range got {
		if b.ID != wantIDs[i] {
			t.Errorf("got[%d].ID = %q, want %q", i, b.ID, wantIDs[i])
		}
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].Order >= got[i].Order {
			t.Errorf("order keys not strictly increasing at %d/%d: %q vs %q",
				i-1, i, got[i-1].Order, got[i].Order)
		}
	}
}

func TestReorderChildren_BogusParentEmptyChildren(t *testing.T) {
	ctx := context.Background()
	resolver, _, _ := setupBulkReorderFixture(t)

	// A non-empty bogus parent with empty childIDs would previously be a
	// silent no-op success. Now it errors with "parent nib not found".
	_, err := resolver.Mutation().ReorderChildren(ctx, "ghost-parent", []string{})
	if err == nil {
		t.Fatal("expected error for non-existent parent")
	}
	if !strings.Contains(err.Error(), "parent") {
		t.Errorf("error should mention 'parent'; got: %v", err)
	}
	if !strings.Contains(err.Error(), "ghost-parent") {
		t.Errorf("error should mention the bogus parent id; got: %v", err)
	}
}

func TestReorderChildren_RequireIfMatch(t *testing.T) {
	ctx := context.Background()
	resolver, core := setupTestResolverWithRequireIfMatch(t)
	parent := &nib.Nib{ID: "epic1", Title: "Epic", Status: "todo", Type: "epic", Version: 1}
	if err := core.Create(parent); err != nil {
		t.Fatal(err)
	}
	child := &nib.Nib{ID: "a", Title: "A", Status: "todo", Type: "task", Parent: "epic1", Order: "a0", Version: 1}
	if err := core.Create(child); err != nil {
		t.Fatal(err)
	}

	_, err := resolver.Mutation().ReorderChildren(ctx, "epic1", []string{"a"})
	if err == nil {
		t.Fatal("expected error under require_if_match: true")
	}
	if !strings.Contains(err.Error(), "require_if_match") {
		t.Errorf("error should mention require_if_match; got: %v", err)
	}
	if !strings.Contains(err.Error(), "nibs-n3zb") {
		t.Errorf("error should reference deferral nib nibs-n3zb; got: %v", err)
	}
}

func TestReorderSiblings_RequireIfMatch(t *testing.T) {
	ctx := context.Background()
	resolver, core := setupTestResolverWithRequireIfMatch(t)
	parent := &nib.Nib{ID: "epic1", Title: "Epic", Status: "todo", Type: "epic", Version: 1}
	if err := core.Create(parent); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ id, order string }{
		{"a", "a0"}, {"b", "b0"}, {"c", "c0"},
	} {
		child := &nib.Nib{ID: c.id, Title: c.id, Status: "todo", Type: "task", Parent: "epic1", Order: c.order, Version: 1}
		if err := core.Create(child); err != nil {
			t.Fatal(err)
		}
	}

	_, err := resolver.Mutation().ReorderSiblings(ctx, []string{"c"}, strPtr("a"), nil, nil)
	if err == nil {
		t.Fatal("expected error under require_if_match: true")
	}
	if !strings.Contains(err.Error(), "require_if_match") {
		t.Errorf("error should mention require_if_match; got: %v", err)
	}
	if !strings.Contains(err.Error(), "nibs-n3zb") {
		t.Errorf("error should reference deferral nib nibs-n3zb; got: %v", err)
	}
}

func TestReorderSiblings_AnchorInBlock_CrossForm(t *testing.T) {
	ctx := context.Background()
	resolver, _, _ := setupBlockMoveFixturePrefixed(t)

	// Pass the anchor in one form (full) and have the same nib appear in
	// siblingIds in another form (short). Resolved IDs catch the collision;
	// the error should reference both surface forms (or the canonical).
	_, err := resolver.Mutation().ReorderSiblings(ctx, []string{"a", "c"}, strPtr("nibs-a"), nil, nil)
	if err == nil {
		t.Fatal("expected error when anchor (cross-form) appears in siblingIds")
	}
	if !strings.Contains(err.Error(), "anchor") {
		t.Errorf("error should mention 'anchor'; got: %v", err)
	}
	if !strings.Contains(err.Error(), "nibs-a") {
		t.Errorf("error should mention canonical id 'nibs-a'; got: %v", err)
	}
}

func TestReorderChildren_Tracer(t *testing.T) {
	ctx := context.Background()
	resolver, _, parentID := setupBulkReorderFixture(t)

	got, err := resolver.Mutation().ReorderChildren(ctx, parentID, []string{"c", "a", "b"})
	if err != nil {
		t.Fatalf("ReorderChildren error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d nibs, want 3", len(got))
	}

	// Returned nibs must reflect the requested order
	wantIDs := []string{"c", "a", "b"}
	for i, b := range got {
		if b.ID != wantIDs[i] {
			t.Errorf("got[%d].ID = %q, want %q", i, b.ID, wantIDs[i])
		}
	}

	// Order keys must be strictly increasing in the requested order
	for i := 1; i < len(got); i++ {
		if got[i-1].Order >= got[i].Order {
			t.Errorf("order keys not strictly increasing: got[%d]=%q >= got[%d]=%q",
				i-1, got[i-1].Order, i, got[i].Order)
		}
	}

	// Re-read the siblings via GetSortedSiblings to confirm persistence
	siblings := resolver.Orderer.GetSortedSiblings(parentID)
	if len(siblings) != 3 {
		t.Fatalf("got %d siblings after reorder, want 3", len(siblings))
	}
	for i, b := range siblings {
		if b.ID != wantIDs[i] {
			t.Errorf("siblings[%d].ID = %q, want %q", i, b.ID, wantIDs[i])
		}
	}
}
