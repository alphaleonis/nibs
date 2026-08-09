package graph

import (
	"context"
	"encoding/hex"
	"errors"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/graph/model"
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

	_, err := resolver.Mutation().ReorderChildren(ctx, parentID, []string{"a", "b"}, nil)
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

	_, err := resolver.Mutation().ReorderChildren(ctx, parentID, []string{"a", "b", "x"}, nil)
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

	_, err := resolver.Mutation().ReorderChildren(ctx, parentID, []string{"a", "b", "c", "x1"}, nil)
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

	_, err := resolver.Mutation().ReorderChildren(ctx, parentID, []string{"a", "b", "b"}, nil)
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

	got, err := resolver.Mutation().ReorderChildren(ctx, "", []string{"r3", "r1", "r2"}, nil)
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

	got, err := resolver.Mutation().ReorderChildren(ctx, "epic1", []string{"a"}, nil)
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

	got, err := resolver.Mutation().ReorderSiblings(ctx, []string{"c", "e"}, strPtr("a"), nil, nil, nil)
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

	got, err := resolver.Mutation().ReorderSiblings(ctx, []string{"c", "e"}, nil, strPtr("a"), nil, nil)
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

	got, err := resolver.Mutation().ReorderSiblings(ctx, []string{"c", "e"}, nil, nil, boolPtr(true), nil)
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

	_, err := resolver.Mutation().ReorderSiblings(ctx, []string{"a", "c"}, strPtr("a"), nil, nil, nil)
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

	_, err := resolver.Mutation().ReorderSiblings(ctx, []string{"a", "x1"}, strPtr("b"), nil, nil, nil)
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

	_, err := resolver.Mutation().ReorderSiblings(ctx, []string{"c", "missing-z"}, strPtr("a"), nil, nil, nil)
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
		_, err := resolver.Mutation().ReorderSiblings(ctx, []string{"c", "e"}, nil, nil, nil, nil)
		if err == nil {
			t.Fatal("expected error when no positioning flag is specified")
		}
	})

	t.Run("after and before", func(t *testing.T) {
		_, err := resolver.Mutation().ReorderSiblings(ctx, []string{"c", "e"}, strPtr("a"), strPtr("b"), nil, nil)
		if err == nil {
			t.Fatal("expected error when both afterId and beforeId are specified")
		}
	})

	t.Run("after and first", func(t *testing.T) {
		_, err := resolver.Mutation().ReorderSiblings(ctx, []string{"c", "e"}, strPtr("a"), nil, boolPtr(true), nil)
		if err == nil {
			t.Fatal("expected error when both afterId and first are specified")
		}
	})

	t.Run("before and first", func(t *testing.T) {
		_, err := resolver.Mutation().ReorderSiblings(ctx, []string{"c", "e"}, nil, strPtr("a"), boolPtr(true), nil)
		if err == nil {
			t.Fatal("expected error when both beforeId and first are specified")
		}
	})

	t.Run("all three", func(t *testing.T) {
		_, err := resolver.Mutation().ReorderSiblings(ctx, []string{"c", "e"}, strPtr("a"), strPtr("b"), boolPtr(true), nil)
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
	_, err := resolver.Mutation().ReorderChildren(ctx, parentID, []string{"a", "nibs-a", "b", "c"}, nil)
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
	_, err := resolver.Mutation().ReorderSiblings(ctx, []string{"c", "nibs-c"}, strPtr("a"), nil, nil, nil)
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
	got, err := resolver.Mutation().ReorderChildren(ctx, parentID, []string{"c", "a", "b"}, nil)
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

	// A non-empty bogus parent with empty childIDs must error with "parent nib
	// not found" — the trap is silently succeeding as a no-op.
	_, err := resolver.Mutation().ReorderChildren(ctx, "ghost-parent", []string{}, nil)
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

// Behavior #10: Mode B with a stale etag aborts before any on-disk
// mutations. Sibling order remains in its pre-call state.
func TestReorderSiblings_IfMatch_StaleEtagAtomic(t *testing.T) {
	ctx := context.Background()
	resolver, _, parentID := setupBlockMoveFixture(t)

	ifMatch := childEtags(t, resolver, "c", "e")
	// Corrupt the etag for "e" to simulate a concurrent modification.
	for _, e := range ifMatch {
		if e.ID == "e" {
			e.Etag = "deadbeefdeadbeef"
		}
	}

	_, err := resolver.Mutation().ReorderSiblings(ctx, []string{"c", "e"}, strPtr("a"), nil, nil, ifMatch)
	if err == nil {
		t.Fatal("expected error for stale etag")
	}
	if !strings.Contains(err.Error(), "e") {
		t.Errorf("error should mention stale sibling 'e'; got: %v", err)
	}
	// Same typed-conflict contract as reorderChildren: the pre-validation refusal
	// is a reconcilable *nibcore.ETagMismatchError, so both the wire code and the
	// CLI exit status can be routed structurally.
	var mismatch *nibcore.ETagMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("got %T: %v, want a wrapped *nibcore.ETagMismatchError", err, err)
	}
	if mismatch.Provided != "deadbeefdeadbeef" {
		t.Errorf("mismatch.Provided = %q, want the stale token the caller sent", mismatch.Provided)
	}
	if mismatch.Current == "" {
		t.Error("mismatch.Current is empty, want the server's current etag (a token a retry can echo back)")
	}

	// Atomicity — order unchanged.
	siblings := resolver.Orderer.GetSortedSiblings(parentID)
	want := []string{"a", "b", "c", "d", "e"}
	if len(siblings) != len(want) {
		t.Fatalf("got %d siblings, want %d", len(siblings), len(want))
	}
	for i, b := range siblings {
		if b.ID != want[i] {
			t.Errorf("siblings[%d].ID = %q, want %q (order should be unchanged)", i, b.ID, want[i])
		}
	}
}

// Behavior #9: Mode B `reorderSiblings([c, e], afterId=a, ifMatch=[c,e])`
// with valid etags succeeds and writes the requested order.
func TestReorderSiblings_IfMatch_Tracer(t *testing.T) {
	ctx := context.Background()
	resolver, _, parentID := setupBlockMoveFixture(t)

	ifMatch := childEtags(t, resolver, "c", "e")
	got, err := resolver.Mutation().ReorderSiblings(ctx, []string{"c", "e"}, strPtr("a"), nil, nil, ifMatch)
	if err != nil {
		t.Fatalf("ReorderSiblings error: %v", err)
	}
	if len(got) != 2 || got[0].ID != "c" || got[1].ID != "e" {
		t.Errorf("got %+v, want block [c, e]", got)
	}

	// Re-read siblings — order should be [a, c, e, b, d]
	siblings := resolver.Orderer.GetSortedSiblings(parentID)
	want := []string{"a", "c", "e", "b", "d"}
	if len(siblings) != len(want) {
		t.Fatalf("got %d siblings, want %d", len(siblings), len(want))
	}
	for i, b := range siblings {
		if b.ID != want[i] {
			t.Errorf("siblings[%d].ID = %q, want %q", i, b.ID, want[i])
		}
	}
}

// Behavior #8: Mode A rejects ifMatch entries that reference the same nib
// twice (canonicalized), even when the surface forms differ. Without dedup
// the second value would silently shadow the first.
func TestReorderChildren_IfMatch_Duplicate(t *testing.T) {
	ctx := context.Background()
	resolver, _, parentID := setupBulkReorderFixture(t)

	etagA, err := resolver.Reader.CurrentETag("a")
	if err != nil {
		t.Fatal(err)
	}
	ifMatch := []*model.ChildEtag{
		{ID: "a", Etag: etagA},
		{ID: "a", Etag: "deadbeefdeadbeef"}, // same id, different etag
	}

	_, err = resolver.Mutation().ReorderChildren(ctx, parentID, []string{"c", "a", "b"}, ifMatch)
	if err == nil {
		t.Fatal("expected error for duplicate id in ifMatch")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error should mention 'duplicate'; got: %v", err)
	}
	if !strings.Contains(err.Error(), "a") {
		t.Errorf("error should mention duplicated id 'a'; got: %v", err)
	}
}

// Behavior #7: Under require_if_match: true, a partial ifMatch is rejected
// up front with the missing canonical ids listed in the error.
func TestReorderChildren_RequireIfMatch_PartialRejected(t *testing.T) {
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

	// Cover only "a" — leave "b" and "c" unmapped.
	ifMatch := childEtags(t, resolver, "a")
	_, err := resolver.Mutation().ReorderChildren(ctx, "epic1", []string{"c", "a", "b"}, ifMatch)
	if err == nil {
		t.Fatal("expected error for partial ifMatch under require_if_match: true")
	}
	if !strings.Contains(err.Error(), "require_if_match") {
		t.Errorf("error should mention require_if_match; got: %v", err)
	}
	if !strings.Contains(err.Error(), "b") || !strings.Contains(err.Error(), "c") {
		t.Errorf("error should list missing ids 'b' and 'c'; got: %v", err)
	}

	// Atomicity — order unchanged after rejection.
	siblings := resolver.Orderer.GetSortedSiblings("epic1")
	want := []string{"a", "b", "c"}
	for i, b := range siblings {
		if b.ID != want[i] {
			t.Errorf("siblings[%d].ID = %q, want %q (order should be unchanged)", i, b.ID, want[i])
		}
	}
}

// Behavior #6: Under require_if_match: true, a complete valid ifMatch lets
// the bulk reorder succeed. Without ifMatch the call is rejected (covered
// by TestReorderChildren_RequireIfMatch_MissingIfMatch in behavior #7).
func TestReorderChildren_RequireIfMatch(t *testing.T) {
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

	ifMatch := childEtags(t, resolver, "a", "b", "c")
	got, err := resolver.Mutation().ReorderChildren(ctx, "epic1", []string{"c", "a", "b"}, ifMatch)
	if err != nil {
		t.Fatalf("ReorderChildren error under require_if_match with complete ifMatch: %v", err)
	}
	wantIDs := []string{"c", "a", "b"}
	if len(got) != len(wantIDs) {
		t.Fatalf("got %d nibs, want %d", len(got), len(wantIDs))
	}
	for i, b := range got {
		if b.ID != wantIDs[i] {
			t.Errorf("got[%d].ID = %q, want %q", i, b.ID, wantIDs[i])
		}
	}
}

// Behavior #11: Mode B under require_if_match: true with a complete valid
// ifMatch succeeds; without ifMatch the call is rejected with the missing
// canonical ids listed in the error.
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

	t.Run("missing ifMatch is rejected", func(t *testing.T) {
		_, err := resolver.Mutation().ReorderSiblings(ctx, []string{"c"}, strPtr("a"), nil, nil, nil)
		if err == nil {
			t.Fatal("expected error under require_if_match: true with no ifMatch")
		}
		if !strings.Contains(err.Error(), "require_if_match") {
			t.Errorf("error should mention require_if_match; got: %v", err)
		}
		if !strings.Contains(err.Error(), "no ifMatch provided") {
			t.Errorf("error should call out missing ifMatch; got: %v", err)
		}
	})

	t.Run("complete valid ifMatch succeeds", func(t *testing.T) {
		ifMatch := childEtags(t, resolver, "c")
		got, err := resolver.Mutation().ReorderSiblings(ctx, []string{"c"}, strPtr("a"), nil, nil, ifMatch)
		if err != nil {
			t.Fatalf("ReorderSiblings error under require_if_match with complete ifMatch: %v", err)
		}
		if len(got) != 1 || got[0].ID != "c" {
			t.Errorf("got %+v, want [c]", got)
		}

		siblings := resolver.Orderer.GetSortedSiblings("epic1")
		want := []string{"a", "c", "b"}
		if len(siblings) != len(want) {
			t.Fatalf("got %d siblings, want %d", len(siblings), len(want))
		}
		for i, b := range siblings {
			if b.ID != want[i] {
				t.Errorf("siblings[%d].ID = %q, want %q", i, b.ID, want[i])
			}
		}
	})
}

func TestReorderSiblings_AnchorInBlock_CrossForm(t *testing.T) {
	ctx := context.Background()
	resolver, _, _ := setupBlockMoveFixturePrefixed(t)

	// Pass the anchor in one form (full) and have the same nib appear in
	// siblingIds in another form (short). Resolved IDs catch the collision;
	// the error should reference both surface forms (or the canonical).
	_, err := resolver.Mutation().ReorderSiblings(ctx, []string{"a", "c"}, strPtr("nibs-a"), nil, nil, nil)
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

	got, err := resolver.Mutation().ReorderChildren(ctx, parentID, []string{"c", "a", "b"}, nil)
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

// TestNibReader_CurrentETag_NotFound pins the error contract of the new
// CurrentETag interface method for unknown ids — childEtags() t.Fatalf's
// on errors, so the happy-path tests don't exercise this branch.
func TestNibReader_CurrentETag_NotFound(t *testing.T) {
	resolver, _, _ := setupBulkReorderFixture(t)
	_, err := resolver.Reader.CurrentETag("does-not-exist")
	if err == nil {
		t.Fatal("expected error for unknown id")
	}
	if !errors.Is(err, nibcore.ErrNotFound) {
		t.Errorf("expected nibcore.ErrNotFound, got %v", err)
	}
}

// TestReorderChildren_IfMatch_UnparseableFileNonReconcilable covers the fail-closed path
// for the bulk-reorder pre-validation path: when a listed child's on-disk file
// is unparseable, ReorderChildren must abort with the distinct, NON-RECONCILABLE
// *nibcore.OnDiskUnparseableError (carrying no reusable etag token), NOT a plain
// reconcilable "etag mismatch". A client that retries with a fabricated etag
// (e.g. a hash of the corrupt bytes) still cannot satisfy the guard, so the
// corrupt file survives.
func TestReorderChildren_IfMatch_UnparseableFileNonReconcilable(t *testing.T) {
	ctx := context.Background()
	resolver, core, parentID := setupBulkReorderFixture(t)

	// Capture valid etags for all three children while they are still parseable.
	ifMatch := childEtags(t, resolver, "a", "b", "c")

	// Corrupt child "b" on disk (git-merge-conflict markers → invalid YAML).
	bNib, err := core.Get("b")
	if err != nil {
		t.Fatalf("Get(b): %v", err)
	}
	bPath := filepath.Join(core.Root(), bNib.Path)
	const corrupt = `---
title: Second
status: todo
<<<<<<< HEAD
order: b0
=======
order: b9
>>>>>>> other
---

Body under edit.
`
	if err := os.WriteFile(bPath, []byte(corrupt), 0o644); err != nil {
		t.Fatalf("corrupting b: %v", err)
	}

	// Attempt 1: pre-validation must fail with the non-reconcilable error.
	_, err = resolver.Mutation().ReorderChildren(ctx, parentID, []string{"c", "a", "b"}, ifMatch)
	var unparseable *nibcore.OnDiskUnparseableError
	if !errors.As(err, &unparseable) {
		t.Fatalf("attempt 1: got %T: %v, want wrapped *nibcore.OnDiskUnparseableError", err, err)
	}

	// Attempt 2 hits the SAME branch as attempt 1 (validateIfMatchETags returns
	// the wrapped OnDiskUnparseableError before the `current != want` comparison),
	// so it adds no branch coverage — it just documents that an etag fabricated
	// from the corrupt bytes still cannot reach the comparison, hence cannot clobber.
	h := fnv.New64a()
	h.Write([]byte(corrupt))
	fabricated := hex.EncodeToString(h.Sum(nil))
	retry := []*model.ChildEtag{
		{ID: "a", Etag: ifMatch[0].Etag},
		{ID: "b", Etag: fabricated},
		{ID: "c", Etag: ifMatch[2].Etag},
	}
	_, err = resolver.Mutation().ReorderChildren(ctx, parentID, []string{"c", "a", "b"}, retry)
	if !errors.As(err, &unparseable) {
		t.Fatalf("attempt 2 (reconcile-retry): got %T: %v, want *nibcore.OnDiskUnparseableError (clobber must be impossible)", err, err)
	}

	// The corrupt bytes must survive both attempts (no reorder write clobbered it).
	after, err := os.ReadFile(bPath)
	if err != nil {
		t.Fatalf("reading b after refused reorders: %v", err)
	}
	if string(after) != corrupt {
		t.Errorf("a refused reorder overwrote the unparseable child file:\n got:\n%s\nwant:\n%s", after, corrupt)
	}
}

// failOnUpdateWriter wraps a NibWriter and fails Update for one target ID,
// delegating every other call (including successful Updates) to the embedded
// writer. It deterministically simulates a mid-loop write rejection — e.g. an
// on-disk divergence between bulk-reorder's T0 pre-validation and a later
// per-item write, or a transient disk error.
type failOnUpdateWriter struct {
	NibWriter
	failID  string
	failErr error
	updated []string // IDs successfully persisted (in call order)
}

func (w *failOnUpdateWriter) Update(b *nib.Nib, ifMatch *string) error {
	if b.ID == w.failID {
		return w.failErr
	}
	if err := w.NibWriter.Update(b, ifMatch); err != nil {
		return err
	}
	w.updated = append(w.updated, b.ID)
	return nil
}

func sliceContains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// TestBulkReorderMidLoopFailureLeavesNoPhantomOrder guards the
// corruption class in the bulk-reorder loops the sweep extends to.
// reorderChildrenImpl / reorderSiblingsImpl obtained each nib via Reader.Get (the
// shared c.nibs[id] pointer) and assigned b.Order = newKey in place before a
// per-item Writer.Update. A mid-loop Update rejection left earlier siblings
// persisted AND the failing one showing a phantom order in memory only. The fix
// mutates a CLONE per item and only swaps the returned pointer on success.
//
// A decorating writer forces the 2nd item in the block to fail. Each subtest
// asserts the failing item's shared in-memory nib shows no phantom order, while an
// earlier item IS persisted (proving the failure is genuinely mid-loop). RED
// against the mutate-shared-pointer code, GREEN after.
func TestBulkReorderMidLoopFailureLeavesNoPhantomOrder(t *testing.T) {
	ctx := context.Background()

	t.Run("ReorderChildren", func(t *testing.T) {
		_, core, parentID := setupBulkReorderFixture(t) // children a,b,c ordered a0,b0,c0
		fw := &failOnUpdateWriter{NibWriter: core, failID: "b", failErr: errors.New("simulated mid-loop write failure")}
		r := &Resolver{Reader: core, Writer: fw, Validator: core, Blocking: core, Orderer: NewOrderer(core, fw)}

		_, err := r.Mutation().ReorderChildren(ctx, parentID, []string{"a", "b", "c"}, nil)
		if err == nil {
			t.Fatal("expected mid-loop write failure, got nil error")
		}

		// The failing item's shared in-memory nib must show no phantom order.
		gotB, err := core.Get("b")
		if err != nil {
			t.Fatalf("get b: %v", err)
		}
		if gotB.Order != "b0" {
			t.Errorf("failing item 'b' left with phantom Order %q after refused write; want pre-call %q", gotB.Order, "b0")
		}
		// An earlier item was persisted before the failure — confirms this
		// exercises a genuine mid-loop rejection (not a first-item failure).
		if !sliceContains(fw.updated, "a") {
			t.Errorf("earlier item 'a' was not persisted before the failure (updated=%v) — not a mid-loop failure", fw.updated)
		}
	})

	t.Run("ReorderSiblings", func(t *testing.T) {
		_, core, _ := setupBlockMoveFixture(t) // children a..e ordered a0..e0
		fw := &failOnUpdateWriter{NibWriter: core, failID: "e", failErr: errors.New("simulated mid-loop write failure")}
		r := &Resolver{Reader: core, Writer: fw, Validator: core, Blocking: core, Orderer: NewOrderer(core, fw)}

		// Move block [c, e] after a; the loop writes c then e — e fails mid-loop.
		_, err := r.Mutation().ReorderSiblings(ctx, []string{"c", "e"}, strPtr("a"), nil, nil, nil)
		if err == nil {
			t.Fatal("expected mid-loop write failure, got nil error")
		}

		gotE, err := core.Get("e")
		if err != nil {
			t.Fatalf("get e: %v", err)
		}
		if gotE.Order != "e0" {
			t.Errorf("failing item 'e' left with phantom Order %q after refused write; want pre-call %q", gotE.Order, "e0")
		}
		// An earlier block item was persisted before the failure — confirms a
		// genuine mid-loop rejection.
		if !sliceContains(fw.updated, "c") {
			t.Errorf("earlier item 'c' was not persisted before the failure (updated=%v) — not a mid-loop failure", fw.updated)
		}
	})
}

// childEtags reads the current on-disk etag for each id and packs the
// results into a slice of *model.ChildEtag — the shape resolvers expect.
func childEtags(t *testing.T, resolver *Resolver, ids ...string) []*model.ChildEtag {
	t.Helper()
	out := make([]*model.ChildEtag, 0, len(ids))
	for _, id := range ids {
		etag, err := resolver.Reader.CurrentETag(id)
		if err != nil {
			t.Fatalf("CurrentETag(%q): %v", id, err)
		}
		out = append(out, &model.ChildEtag{ID: id, Etag: etag})
	}
	return out
}

// Behavior #5: Mode A with ifMatch=nil and require_if_match: false — the
// new code path must remain a strict superset of pre-ifMatch behavior. No
// validation, no on-disk reads, just reorder.
func TestReorderChildren_IfMatch_NilWithoutRequire(t *testing.T) {
	ctx := context.Background()
	resolver, _, parentID := setupBulkReorderFixture(t)

	got, err := resolver.Mutation().ReorderChildren(ctx, parentID, []string{"c", "a", "b"}, nil)
	if err != nil {
		t.Fatalf("ReorderChildren error: %v", err)
	}
	wantIDs := []string{"c", "a", "b"}
	for i, b := range got {
		if b.ID != wantIDs[i] {
			t.Errorf("got[%d].ID = %q, want %q", i, b.ID, wantIDs[i])
		}
	}
}

// Behavior #4: Mode A with a partial ifMatch (covers some children) and
// require_if_match: false — unchecked children skip validation; checked
// ones are validated; reorder succeeds.
func TestReorderChildren_IfMatch_PartialCoverage(t *testing.T) {
	ctx := context.Background()
	resolver, _, parentID := setupBulkReorderFixture(t)

	// Provide ifMatch only for "b". The other two children skip validation.
	ifMatch := childEtags(t, resolver, "b")

	got, err := resolver.Mutation().ReorderChildren(ctx, parentID, []string{"c", "a", "b"}, ifMatch)
	if err != nil {
		t.Fatalf("ReorderChildren error: %v", err)
	}
	wantIDs := []string{"c", "a", "b"}
	for i, b := range got {
		if b.ID != wantIDs[i] {
			t.Errorf("got[%d].ID = %q, want %q", i, b.ID, wantIDs[i])
		}
	}

	siblings := resolver.Orderer.GetSortedSiblings(parentID)
	for i, b := range siblings {
		if b.ID != wantIDs[i] {
			t.Errorf("siblings[%d].ID = %q, want %q (order=%q)", i, b.ID, wantIDs[i], b.Order)
		}
	}
}

// Behavior #3: Mode A errors when ifMatch references an id that is not in
// the listed childIds — the operation is ambiguous, so reject up front.
func TestReorderChildren_IfMatch_UnknownEntry(t *testing.T) {
	ctx := context.Background()
	resolver, core, parentID := setupBulkReorderFixture(t)

	// Create a stranger nib under a different parent so the reorder of
	// [a, b, c] under parentID is still complete.
	otherParent := &nib.Nib{ID: "epic2", Title: "Epic 2", Status: "todo", Type: "epic", Version: 1}
	if err := core.Create(otherParent); err != nil {
		t.Fatal(err)
	}
	stranger := &nib.Nib{ID: "stranger", Title: "Stranger", Status: "todo", Type: "task", Parent: "epic2", Order: "z0", Version: 1}
	if err := core.Create(stranger); err != nil {
		t.Fatal(err)
	}

	ifMatch := childEtags(t, resolver, "a", "b", "c", "stranger")
	_, err := resolver.Mutation().ReorderChildren(ctx, parentID, []string{"c", "a", "b"}, ifMatch)
	if err == nil {
		t.Fatal("expected error when ifMatch contains a nib outside the reorder list")
	}
	if !strings.Contains(err.Error(), "stranger") {
		t.Errorf("error should mention 'stranger'; got: %v", err)
	}
}

// Behavior #2: Mode A with a stale etag for one child errors and writes
// nothing. The error mentions the offending canonical id; on-disk siblings
// remain in their pre-call order.
func TestReorderChildren_IfMatch_StaleEtagAtomic(t *testing.T) {
	ctx := context.Background()
	resolver, _, parentID := setupBulkReorderFixture(t)

	ifMatch := childEtags(t, resolver, "a", "b", "c")
	// Corrupt the etag for "b" to simulate a concurrent modification.
	for _, e := range ifMatch {
		if e.ID == "b" {
			e.Etag = "deadbeefdeadbeef"
		}
	}

	_, err := resolver.Mutation().ReorderChildren(ctx, parentID, []string{"c", "a", "b"}, ifMatch)
	if err == nil {
		t.Fatal("expected error for stale etag")
	}
	if !strings.Contains(err.Error(), "b") {
		t.Errorf("error should mention stale child 'b'; got: %v", err)
	}
	// The refusal must be the TYPED, reconcilable conflict — that is what carries
	// extensions.code = "ETAG_MISMATCH" on the wire (cmd/serve.go's presenter) and
	// exit 4 (CONFLICT) on the CLI. Both key on errors.As, not on message text.
	var mismatch *nibcore.ETagMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("got %T: %v, want a wrapped *nibcore.ETagMismatchError", err, err)
	}
	if mismatch.Provided != "deadbeefdeadbeef" {
		t.Errorf("mismatch.Provided = %q, want the stale token the caller sent", mismatch.Provided)
	}
	if mismatch.Current == "" {
		t.Error("mismatch.Current is empty, want the server's current etag (a token a retry can echo back)")
	}
	if !strings.Contains(err.Error(), "etag mismatch") {
		t.Errorf("error should mention etag mismatch; got: %v", err)
	}

	// Atomicity: order must be unchanged.
	siblings := resolver.Orderer.GetSortedSiblings(parentID)
	want := []string{"a", "b", "c"}
	if len(siblings) != len(want) {
		t.Fatalf("got %d siblings, want %d", len(siblings), len(want))
	}
	for i, b := range siblings {
		if b.ID != want[i] {
			t.Errorf("siblings[%d].ID = %q, want %q (order should be unchanged after failed call)", i, b.ID, want[i])
		}
	}
}

// Behavior #1 (Tracer): Mode A with a complete, valid ifMatch reorders the
// children in the requested order and writes new strictly-increasing keys.
func TestReorderChildren_IfMatch_Tracer(t *testing.T) {
	ctx := context.Background()
	resolver, _, parentID := setupBulkReorderFixture(t)

	ifMatch := childEtags(t, resolver, "a", "b", "c")

	got, err := resolver.Mutation().ReorderChildren(ctx, parentID, []string{"c", "a", "b"}, ifMatch)
	if err != nil {
		t.Fatalf("ReorderChildren error: %v", err)
	}
	wantIDs := []string{"c", "a", "b"}
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
			t.Errorf("order keys not strictly increasing: got[%d]=%q >= got[%d]=%q",
				i-1, got[i-1].Order, i, got[i].Order)
		}
	}

	siblings := resolver.Orderer.GetSortedSiblings(parentID)
	if len(siblings) != 3 {
		t.Fatalf("got %d siblings, want 3", len(siblings))
	}
	for i, b := range siblings {
		if b.ID != wantIDs[i] {
			t.Errorf("siblings[%d].ID = %q, want %q (order=%q)", i, b.ID, wantIDs[i], b.Order)
		}
	}
}
