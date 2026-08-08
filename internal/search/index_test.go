package search

import (
	"fmt"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
)

func setupTestIndex(t *testing.T) *Index {
	t.Helper()
	idx, err := NewIndex()
	if err != nil {
		t.Fatalf("NewIndex() error = %v", err)
	}
	t.Cleanup(func() {
		_ = idx.Close()
	})
	return idx
}

func TestNewIndex(t *testing.T) {
	idx, err := NewIndex()
	if err != nil {
		t.Fatalf("NewIndex() error = %v", err)
	}
	defer func() { _ = idx.Close() }()
}

func TestIndexNib(t *testing.T) {
	idx := setupTestIndex(t)

	b := &nib.Nib{
		ID:    "abc1",
		Title: "Authentication System",
		Body:  "Implement JWT tokens for user authentication",
	}

	err := idx.IndexNib(b)
	if err != nil {
		t.Fatalf("IndexNib() error = %v", err)
	}

	// Search should find by title
	ids, err := idx.Search("Authentication", 10)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(ids) != 1 {
		t.Errorf("Search() returned %d results, want 1", len(ids))
	}
}

func TestSearch_MatchTitle(t *testing.T) {
	idx := setupTestIndex(t)

	nibs := []*nib.Nib{
		{ID: "aaa1", Title: "User Authentication", Body: "Login system"},
		{ID: "bbb2", Title: "Database Schema", Body: "Table definitions"},
		{ID: "ccc3", Title: "API Endpoints", Body: "REST interface"},
	}

	for _, b := range nibs {
		if err := idx.IndexNib(b); err != nil {
			t.Fatalf("IndexNib() error = %v", err)
		}
	}

	ids, err := idx.Search("Authentication", 10)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if len(ids) != 1 || ids[0] != "aaa1" {
		t.Errorf("Search(Authentication) = %v, want [aaa1]", ids)
	}
}

func TestSearch_MatchBody(t *testing.T) {
	idx := setupTestIndex(t)

	nibs := []*nib.Nib{
		{ID: "aaa1", Title: "Feature A", Body: "Implement JWT tokens"},
		{ID: "bbb2", Title: "Feature B", Body: "Add database migrations"},
		{ID: "ccc3", Title: "Feature C", Body: "Update UI components"},
	}

	for _, b := range nibs {
		if err := idx.IndexNib(b); err != nil {
			t.Fatalf("IndexNib() error = %v", err)
		}
	}

	ids, err := idx.Search("JWT", 10)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if len(ids) != 1 || ids[0] != "aaa1" {
		t.Errorf("Search(JWT) = %v, want [aaa1]", ids)
	}
}

func TestSearch_MatchSlug(t *testing.T) {
	idx := setupTestIndex(t)

	nibs := []*nib.Nib{
		{ID: "aaa1", Slug: "auth-feature", Title: "Feature A", Body: "Some content"},
		{ID: "bbb2", Slug: "database-migration", Title: "Feature B", Body: "Other content"},
		{ID: "ccc3", Slug: "ui-update", Title: "Feature C", Body: "More content"},
	}

	for _, b := range nibs {
		if err := idx.IndexNib(b); err != nil {
			t.Fatalf("IndexNib() error = %v", err)
		}
	}

	// Search by slug content
	ids, err := idx.Search("auth", 10)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if len(ids) != 1 || ids[0] != "aaa1" {
		t.Errorf("Search(auth) = %v, want [aaa1]", ids)
	}
}

func TestSearch_MultipleResults(t *testing.T) {
	idx := setupTestIndex(t)

	nibs := []*nib.Nib{
		{ID: "aaa1", Title: "User Login", Body: "Authentication flow"},
		{ID: "bbb2", Title: "User Registration", Body: "Sign up form"},
		{ID: "ccc3", Title: "Admin Panel", Body: "Dashboard"},
	}

	for _, b := range nibs {
		if err := idx.IndexNib(b); err != nil {
			t.Fatalf("IndexNib() error = %v", err)
		}
	}

	ids, err := idx.Search("User", 10)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if len(ids) != 2 {
		t.Errorf("Search(User) returned %d results, want 2", len(ids))
	}
}

func TestSearch_NoResults(t *testing.T) {
	idx := setupTestIndex(t)

	b := &nib.Nib{
		ID:    "abc1",
		Title: "Test Nib",
		Body:  "Some content",
	}
	if err := idx.IndexNib(b); err != nil {
		t.Fatalf("IndexNib() error = %v", err)
	}

	ids, err := idx.Search("nonexistent", 10)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if len(ids) != 0 {
		t.Errorf("Search(nonexistent) = %v, want []", ids)
	}
}

func TestSearch_EmptyQuery(t *testing.T) {
	idx := setupTestIndex(t)

	b := &nib.Nib{
		ID:    "abc1",
		Title: "Test Nib",
		Body:  "Some content",
	}
	if err := idx.IndexNib(b); err != nil {
		t.Fatalf("IndexNib() error = %v", err)
	}

	// Empty query returns no results (Bleve matches nothing)
	ids, err := idx.Search("", 10)
	if err != nil {
		t.Fatalf("Search('') error = %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("Search('') = %v, want []", ids)
	}
}

func TestSearch_BooleanQuery(t *testing.T) {
	idx := setupTestIndex(t)

	nibs := []*nib.Nib{
		{ID: "aaa1", Title: "User Authentication", Body: "Login with password"},
		{ID: "bbb2", Title: "User Registration", Body: "Create account"},
		{ID: "ccc3", Title: "Admin Authentication", Body: "Admin login"},
	}

	for _, b := range nibs {
		if err := idx.IndexNib(b); err != nil {
			t.Fatalf("IndexNib() error = %v", err)
		}
	}

	// Search for "User AND Authentication"
	ids, err := idx.Search("User Authentication", 10)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	// Should match aaa1 (has both terms)
	found := false
	for _, id := range ids {
		if id == "aaa1" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Search(User Authentication) = %v, expected to include aaa1", ids)
	}
}

func TestSearch_Wildcard(t *testing.T) {
	idx := setupTestIndex(t)

	nibs := []*nib.Nib{
		{ID: "aaa1", Title: "Authentication", Body: ""},
		{ID: "bbb2", Title: "Authorization", Body: ""},
		{ID: "ccc3", Title: "Automation", Body: ""},
	}

	for _, b := range nibs {
		if err := idx.IndexNib(b); err != nil {
			t.Fatalf("IndexNib() error = %v", err)
		}
	}

	// Search with wildcard - note: Bleve wildcards are case-sensitive and work on lowercase tokens
	ids, err := idx.Search("auth*", 10)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if len(ids) != 2 {
		t.Errorf("Search(auth*) returned %d results, want 2 (Authentication, Authorization)", len(ids))
	}
}

func TestDeleteNib(t *testing.T) {
	idx := setupTestIndex(t)

	b := &nib.Nib{
		ID:    "abc1",
		Title: "Test Nib",
		Body:  "Some content",
	}
	if err := idx.IndexNib(b); err != nil {
		t.Fatalf("IndexNib() error = %v", err)
	}

	// Verify it's indexed
	ids, _ := idx.Search("Test", 10)
	if len(ids) != 1 {
		t.Fatal("nib should be indexed before delete")
	}

	// Delete
	if err := idx.DeleteNib("abc1"); err != nil {
		t.Fatalf("DeleteNib() error = %v", err)
	}

	// Verify it's gone
	ids, _ = idx.Search("Test", 10)
	if len(ids) != 0 {
		t.Errorf("Search after delete = %v, want []", ids)
	}
}

func TestIndexNibs(t *testing.T) {
	idx := setupTestIndex(t)

	nibs := []*nib.Nib{
		{ID: "aaa1", Title: "Nib One", Body: "First content"},
		{ID: "bbb2", Title: "Nib Two", Body: "Second content"},
		{ID: "ccc3", Title: "Nib Three", Body: "Third content"},
	}

	if err := idx.IndexNibs(nibs); err != nil {
		t.Fatalf("IndexNibs() error = %v", err)
	}

	// All nibs should be searchable
	ids, err := idx.Search("Nib", 10)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if len(ids) != 3 {
		t.Errorf("Search(Nib) returned %d results, want 3", len(ids))
	}
}

func TestIndexNib_Update(t *testing.T) {
	idx := setupTestIndex(t)

	// Index initial version
	b := &nib.Nib{
		ID:    "abc1",
		Title: "Original Title",
		Body:  "Original content",
	}
	if err := idx.IndexNib(b); err != nil {
		t.Fatalf("IndexNib() error = %v", err)
	}

	// Update the nib
	b.Title = "Updated Title"
	b.Body = "Updated content"
	if err := idx.IndexNib(b); err != nil {
		t.Fatalf("IndexNib() update error = %v", err)
	}

	// Should find by new title
	ids, _ := idx.Search("Updated", 10)
	if len(ids) != 1 || ids[0] != "abc1" {
		t.Errorf("Search(Updated) = %v, want [abc1]", ids)
	}

	// Should NOT find by old title
	ids, _ = idx.Search("Original", 10)
	if len(ids) != 0 {
		t.Errorf("Search(Original) after update = %v, want []", ids)
	}
}

func TestSearch_Limit(t *testing.T) {
	idx := setupTestIndex(t)

	// Index many nibs
	for i := 0; i < 20; i++ {
		b := &nib.Nib{
			ID:    nib.NewID("", 4),
			Title: "Test Nib",
			Body:  "Content",
		}
		if err := idx.IndexNib(b); err != nil {
			t.Fatalf("IndexNib() error = %v", err)
		}
	}

	// Search with limit
	ids, err := idx.Search("Test", 5)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if len(ids) != 5 {
		t.Errorf("Search with limit 5 returned %d results, want 5", len(ids))
	}
}

// TestSearch_NegativeLimitReturnsEveryMatch covers the other half of the
// sentinel. The contract is `limit <= 0` means no cap, not `limit == 0`, so a
// negative value must behave exactly like zero — an implementation that special-
// cases only zero would hand Bleve a negative Size, and the answer would come
// back empty rather than complete.
//
// The "no default substitution" half of the contract is pinned by
// TestSearch_ZeroLimitReturnsEveryMatch, whose fixture is sized past the old
// default; this one only has to be larger than any positive cap could be
// mistaken for.
func TestSearch_NegativeLimitReturnsEveryMatch(t *testing.T) {
	idx := setupTestIndex(t)

	const total = 20
	nibs := make([]*nib.Nib, 0, total)
	for i := range total {
		nibs = append(nibs, &nib.Nib{
			ID:    fmt.Sprintf("nn%04d", i),
			Title: "quarkfoo entry",
		})
	}
	if err := idx.IndexNibs(nibs); err != nil {
		t.Fatalf("IndexNibs() error = %v", err)
	}

	ids, err := idx.Search("quarkfoo", -1)
	if err != nil {
		t.Fatalf("Search(limit -1) error = %v", err)
	}
	if len(ids) != total {
		t.Errorf("Search(limit -1) returned %d ids, want all %d", len(ids), total)
	}
}

// Malformed/partial query strings — the transient states a user types on the way
// to a valid query (`type:` before `type:bug`, a lone `-`, a leading `/`, an
// unbalanced quote) — must NOT surface a query error. Bleve's query-string parser
// rejects each with a syntax error (the `/` case from a recovered parser panic);
// Search must fall back to a plain free-text match so any input degrades to a
// best-effort search rather than failing. Guards nibs-rv7c.
func TestSearch_MalformedQueryNeverErrors(t *testing.T) {
	idx := setupTestIndex(t)
	nibs := []*nib.Nib{
		{ID: "aaa1", Title: "type checking", Body: "notes on the type system"},
		{ID: "bbb2", Title: "unrelated", Body: "nothing here"},
	}
	if err := idx.IndexNibs(nibs); err != nil {
		t.Fatalf("IndexNibs() error = %v", err)
	}

	// Each of these makes Bleve's query-string grammar error; none may propagate.
	malformed := []string{"type:", "title:", "status:", "-", "/", "\"", "is:", "*", "AND"}
	for _, q := range malformed {
		if _, err := idx.Search(q, 10); err != nil {
			t.Errorf("Search(%q) returned error %v, want nil (fall back to free-text match)", q, err)
		}
	}
}

// The free-text fallback still matches: a bare `type:` (rejected by the grammar)
// falls back to matching the analyzed term "type", finding the doc that contains
// that word and not the one that doesn't.
func TestSearch_MalformedQueryFallsBackToFreeText(t *testing.T) {
	idx := setupTestIndex(t)
	nibs := []*nib.Nib{
		{ID: "aaa1", Title: "type checking", Body: "notes on the type system"},
		{ID: "bbb2", Title: "unrelated", Body: "nothing here"},
	}
	if err := idx.IndexNibs(nibs); err != nil {
		t.Fatalf("IndexNibs() error = %v", err)
	}

	ids, err := idx.Search("type:", 10)
	if err != nil {
		t.Fatalf("Search(\"type:\") error = %v", err)
	}
	if len(ids) != 1 || ids[0] != "aaa1" {
		t.Errorf("Search(\"type:\") = %v, want [aaa1] (free-text match on \"type\")", ids)
	}
}

// A well-formed field query still uses the query-string path (not the fallback):
// `title:` is a mapped field, so `title:checking` matches only via that field.
func TestSearch_ValidFieldQueryStillWorks(t *testing.T) {
	idx := setupTestIndex(t)
	nibs := []*nib.Nib{
		{ID: "aaa1", Title: "type checking", Body: "unrelated body"},
		{ID: "bbb2", Title: "plain title", Body: "checking the body only"},
	}
	if err := idx.IndexNibs(nibs); err != nil {
		t.Fatalf("IndexNibs() error = %v", err)
	}

	ids, err := idx.Search("title:checking", 10)
	if err != nil {
		t.Fatalf("Search(\"title:checking\") error = %v", err)
	}
	if len(ids) != 1 || ids[0] != "aaa1" {
		t.Errorf("Search(\"title:checking\") = %v, want [aaa1] (field-scoped match)", ids)
	}
}

// TestSearch_ZeroLimitReturnsEveryMatch pins what limit <= 0 means: no cap.
//
// It used to mean "substitute a default of 1000", which is a cap wearing the
// sentinel for its absence. nibcore.Core.SearchAll asks for 0 precisely because
// it must not silently drop a match, so a default here would reinstate the very
// bound that call exists to avoid — and would do it invisibly, since the caller
// asked for everything and got a plausible-looking 1000.
//
// The fixture is deliberately just past that old default, so the assertion fails
// if the substitution ever returns.
func TestSearch_ZeroLimitReturnsEveryMatch(t *testing.T) {
	idx := setupTestIndex(t)

	const total = 1005
	nibs := make([]*nib.Nib, 0, total)
	for i := range total {
		nibs = append(nibs, &nib.Nib{
			ID:    fmt.Sprintf("zz%04d", i),
			Title: "quarkfoo entry",
		})
	}
	if err := idx.IndexNibs(nibs); err != nil {
		t.Fatalf("IndexNibs() error = %v", err)
	}

	all, err := idx.Search("quarkfoo", 0)
	if err != nil {
		t.Fatalf("Search(limit 0) error = %v", err)
	}
	if len(all) != total {
		t.Errorf("Search(limit 0) returned %d ids, want all %d", len(all), total)
	}

	// A positive limit still caps, so "no cap" is a property of the sentinel
	// rather than of the index having become unbounded.
	capped, err := idx.Search("quarkfoo", 1000)
	if err != nil {
		t.Fatalf("Search(limit 1000) error = %v", err)
	}
	if len(capped) != 1000 {
		t.Errorf("Search(limit 1000) returned %d ids, want 1000", len(capped))
	}
}

// TestSearch_ZeroLimitOnEmptyIndex covers the degenerate store: "everything"
// over an index holding nothing is nothing, reported as an empty result rather
// than as an error.
func TestSearch_ZeroLimitOnEmptyIndex(t *testing.T) {
	idx := setupTestIndex(t)

	ids, err := idx.Search("anything", 0)
	if err != nil {
		t.Fatalf("Search(limit 0) on empty index error = %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("Search(limit 0) on empty index = %v, want no ids", ids)
	}
}
