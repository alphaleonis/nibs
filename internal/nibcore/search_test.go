package nibcore

import (
	"os"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
)

func TestSearch(t *testing.T) {
	core, _ := setupTestCore(t)
	defer func() { _ = core.Close() }()

	// Create nibs with searchable content
	nibs := []*nib.Nib{
		{ID: "aaa1", Slug: "auth", Title: "User Authentication", Body: "Implement login system"},
		{ID: "bbb2", Slug: "db", Title: "Database Schema", Body: "Create tables for users"},
		{ID: "ccc3", Slug: "api", Title: "API Endpoints", Body: "REST interface for authentication"},
	}

	for _, b := range nibs {
		if err := core.Create(b); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	// Search by title
	results, err := core.Search("Authentication")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Search(Authentication) returned %d results, want 2", len(results))
	}
}

func TestSearch_ByBody(t *testing.T) {
	core, _ := setupTestCore(t)
	defer func() { _ = core.Close() }()

	nibs := []*nib.Nib{
		{ID: "aaa1", Title: "Feature A", Body: "Implement JWT tokens"},
		{ID: "bbb2", Title: "Feature B", Body: "Add database migrations"},
	}

	for _, b := range nibs {
		if err := core.Create(b); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	results, err := core.Search("JWT")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if len(results) != 1 || results[0].ID != "aaa1" {
		t.Errorf("Search(JWT) = %v, want [aaa1]", results)
	}
}

func TestSearch_LazyInit(t *testing.T) {
	core, _ := setupTestCore(t)
	defer func() { _ = core.Close() }()

	// Create a nib first (before any search)
	b := &nib.Nib{
		ID:    "abc1",
		Title: "Test Nib",
		Body:  "Content",
	}
	if err := core.Create(b); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Search should lazily initialize the index and index existing nibs
	results, err := core.Search("Test")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if len(results) != 1 {
		t.Errorf("Search(Test) returned %d results, want 1 (lazy init should index existing nibs)", len(results))
	}
}

func TestSearch_CreateUpdatesIndex(t *testing.T) {
	core, _ := setupTestCore(t)
	defer func() { _ = core.Close() }()

	// Initialize search index by doing a search first
	_, _ = core.Search("anything")

	// Create a new nib
	b := &nib.Nib{
		ID:    "new1",
		Title: "New Nib",
		Body:  "Fresh content",
	}
	if err := core.Create(b); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Search should find the new nib
	results, err := core.Search("Fresh")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if len(results) != 1 || results[0].ID != "new1" {
		t.Errorf("Search(Fresh) = %v, want [new1]", results)
	}
}

func TestSearch_UpdateUpdatesIndex(t *testing.T) {
	core, _ := setupTestCore(t)
	defer func() { _ = core.Close() }()

	// Create and index a nib
	b := &nib.Nib{
		ID:    "upd1",
		Title: "Original Title",
		Body:  "Original content",
	}
	if err := core.Create(b); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Initialize index
	_, _ = core.Search("Original")

	// Update the nib
	b.Title = "Updated Title"
	b.Body = "Modified content"
	if err := core.Update(b, nil); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	// Search should find by new content
	results, err := core.Search("Modified")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if len(results) != 1 || results[0].ID != "upd1" {
		t.Errorf("Search(Modified) = %v, want [upd1]", results)
	}
}

func TestSearch_DeleteUpdatesIndex(t *testing.T) {
	core, _ := setupTestCore(t)
	defer func() { _ = core.Close() }()

	// Create and index a nib
	b := &nib.Nib{
		ID:    "del1",
		Title: "To Delete",
		Body:  "Unique keyword deleteme",
	}
	if err := core.Create(b); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Initialize index
	results, _ := core.Search("deleteme")
	if len(results) != 1 {
		t.Fatal("nib should be indexed before delete")
	}

	// Delete the nib
	if err := core.Delete("del1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// Search should NOT find the deleted nib
	results, err := core.Search("deleteme")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if len(results) != 0 {
		t.Errorf("Search(deleteme) after delete = %v, want []", results)
	}
}

func TestSearch_LoadRebuildsIndex(t *testing.T) {
	core, nibsDir := setupTestCore(t)
	defer func() { _ = core.Close() }()

	// Create a nib
	b := &nib.Nib{
		ID:    "abc1",
		Title: "Initial Nib",
		Body:  "Content",
	}
	if err := core.Create(b); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Initialize index
	_, _ = core.Search("Initial")

	// Write a new nib file directly (simulating external change)
	content := `---
title: External Nib
status: todo
---

External content keyword.
`
	if err := writeTestFile(nibsDir, "ext1--external.md", content); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Reload from disk
	if err := core.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Search should find the externally added nib
	results, err := core.Search("External")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if len(results) != 1 || results[0].ID != "ext1" {
		t.Errorf("Search(External) = %v, want [ext1]", results)
	}
}

func TestSearch_NoResults(t *testing.T) {
	core, _ := setupTestCore(t)
	defer func() { _ = core.Close() }()

	b := &nib.Nib{
		ID:    "abc1",
		Title: "Test Nib",
		Body:  "Content",
	}
	if err := core.Create(b); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	results, err := core.Search("nonexistent")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if len(results) != 0 {
		t.Errorf("Search(nonexistent) = %v, want []", results)
	}
}

func TestClose_ClosesSearchIndex(t *testing.T) {
	core, _ := setupTestCore(t)

	// Create a nib and search to initialize index
	b := &nib.Nib{
		ID:    "abc1",
		Title: "Test Nib",
		Body:  "Content",
	}
	if err := core.Create(b); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_, _ = core.Search("Test")

	// Close should not error
	if err := core.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

// spySearchIndex records calls for verification in tests.
type spySearchIndex struct {
	NoOpSearchIndex
	indexed []string
	deleted []string
}

func (s *spySearchIndex) IndexNib(b *nib.Nib) error {
	s.indexed = append(s.indexed, b.ID)
	return nil
}

func (s *spySearchIndex) IndexNibs(nibs []*nib.Nib) error {
	for _, b := range nibs {
		s.indexed = append(s.indexed, b.ID)
	}
	return nil
}

func (s *spySearchIndex) DeleteNib(id string) error {
	s.deleted = append(s.deleted, id)
	return nil
}

func TestSearch_InjectedIndexReceivesCRUDCalls(t *testing.T) {
	t.Run("Create indexes nib", func(t *testing.T) {
		core, _ := setupTestCore(t)
		defer func() { _ = core.Close() }()
		spy := &spySearchIndex{}
		core.SetSearchIndex(spy)

		b := &nib.Nib{ID: "spy1", Title: "Spy Test", Body: "content"}
		if err := core.Create(b); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if len(spy.indexed) != 1 || spy.indexed[0] != "spy1" {
			t.Errorf("indexed = %v, want [spy1]", spy.indexed)
		}
	})

	t.Run("Update re-indexes nib", func(t *testing.T) {
		core, _ := setupTestCore(t)
		defer func() { _ = core.Close() }()
		spy := &spySearchIndex{}
		core.SetSearchIndex(spy)

		b := &nib.Nib{ID: "spy1", Title: "Spy Test", Body: "content"}
		if err := core.Create(b); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		spy.indexed = nil // reset after setup

		b.Title = "Updated"
		if err := core.Update(b, nil); err != nil {
			t.Fatalf("Update() error = %v", err)
		}
		if len(spy.indexed) != 1 || spy.indexed[0] != "spy1" {
			t.Errorf("indexed = %v, want [spy1]", spy.indexed)
		}
	})

	t.Run("Delete removes nib from index", func(t *testing.T) {
		core, _ := setupTestCore(t)
		defer func() { _ = core.Close() }()
		spy := &spySearchIndex{}
		core.SetSearchIndex(spy)

		b := &nib.Nib{ID: "spy1", Title: "Spy Test", Body: "content"}
		if err := core.Create(b); err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		if err := core.Delete("spy1"); err != nil {
			t.Fatalf("Delete() error = %v", err)
		}
		if len(spy.deleted) != 1 || spy.deleted[0] != "spy1" {
			t.Errorf("deleted = %v, want [spy1]", spy.deleted)
		}
	})
}

func TestSearch_LoadRebuildsInjectedIndex(t *testing.T) {
	core, _ := setupTestCore(t)
	defer func() { _ = core.Close() }()

	spy := &spySearchIndex{}
	core.SetSearchIndex(spy)

	// Create a nib so there's data to re-index on Load
	b := &nib.Nib{ID: "rld1", Title: "Reload Test", Body: "content"}
	if err := core.Create(b); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Reset spy counters
	spy.indexed = nil

	// Reload from disk — should re-index existing nibs into the search index
	if err := core.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// After reload, the nib should be re-indexed
	if len(spy.indexed) != 1 || spy.indexed[0] != "rld1" {
		t.Errorf("after Load: indexed = %v, want [rld1]", spy.indexed)
	}
}

func TestSearch_WithInjectedNoOpIndex(t *testing.T) {
	core, _ := setupTestCore(t)
	defer func() { _ = core.Close() }()

	// Inject a no-op search index — should skip Bleve initialization entirely
	core.SetSearchIndex(NoOpSearchIndex{})

	// Create a nib (should not error despite no-op index)
	b := &nib.Nib{
		ID:    "noop1",
		Title: "NoOp Test",
		Body:  "Should not be indexed in Bleve",
	}
	if err := core.Create(b); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Search should return empty results (no-op)
	results, err := core.Search("NoOp")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Search() with NoOpSearchIndex returned %d results, want 0", len(results))
	}
}

// Helper to write test files
func writeTestFile(dir, name, content string) error {
	return os.WriteFile(dir+"/"+name, []byte(content), 0644)
}
