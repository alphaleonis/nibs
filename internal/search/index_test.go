package search

import (
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

func TestSearch_DefaultLimit(t *testing.T) {
	idx := setupTestIndex(t)

	b := &nib.Nib{
		ID:    "abc1",
		Title: "Test",
		Body:  "",
	}
	if err := idx.IndexNib(b); err != nil {
		t.Fatalf("IndexNib() error = %v", err)
	}

	// Search with 0 limit should use default
	ids, err := idx.Search("Test", 0)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if len(ids) != 1 {
		t.Errorf("Search with limit 0 (default) returned %d results, want 1", len(ids))
	}
}
