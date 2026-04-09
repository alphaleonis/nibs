package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/config"
)

func setupQueryTestApp(t *testing.T) *App {
	t.Helper()
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatalf("failed to create test .nibs dir: %v", err)
	}

	cfg := config.Default()
	testCore := nibcore.New(nibsDir, cfg)
	if err := testCore.Load(); err != nil {
		t.Fatalf("failed to load core: %v", err)
	}

	return &App{Core: testCore}
}

func createQueryTestNib(t *testing.T, c *nibcore.Core, id, title, status string) *nib.Nib {
	t.Helper()
	b := &nib.Nib{
		ID:     id,
		Slug:   nib.Slugify(title),
		Title:  title,
		Status: status,
	}
	if err := c.Create(b); err != nil {
		t.Fatalf("failed to create test nib: %v", err)
	}
	return b
}

func TestExecuteQuery(t *testing.T) {
	app := setupQueryTestApp(t)

	// Create test nibs
	createQueryTestNib(t, app.Core, "test-1", "First Nib", "todo")
	createQueryTestNib(t, app.Core, "test-2", "Second Nib", "in-progress")
	createQueryTestNib(t, app.Core, "test-3", "Third Nib", "completed")

	t.Run("basic query all nibs", func(t *testing.T) {
		query := `{ nibs { id title status } }`
		result, err := executeQuery(app, query, nil, "")
		if err != nil {
			t.Fatalf("executeQuery() error = %v", err)
		}

		var data struct {
			Nibs []struct {
				ID     string `json:"id"`
				Title  string `json:"title"`
				Status string `json:"status"`
			} `json:"nibs"`
		}

		if err := json.Unmarshal(result, &data); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if len(data.Nibs) != 3 {
			t.Errorf("expected 3 nibs, got %d", len(data.Nibs))
		}
	})

	t.Run("query single nib by id", func(t *testing.T) {
		query := `{ nib(id: "test-1") { id title } }`
		result, err := executeQuery(app, query, nil, "")
		if err != nil {
			t.Fatalf("executeQuery() error = %v", err)
		}

		var data struct {
			Nib struct {
				ID    string `json:"id"`
				Title string `json:"title"`
			} `json:"nib"`
		}

		if err := json.Unmarshal(result, &data); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if data.Nib.ID != "test-1" {
			t.Errorf("expected id 'test-1', got %q", data.Nib.ID)
		}
		if data.Nib.Title != "First Nib" {
			t.Errorf("expected title 'First Nib', got %q", data.Nib.Title)
		}
	})

	t.Run("query with filter", func(t *testing.T) {
		query := `{ nibs(filter: { status: ["todo"] }) { id } }`
		result, err := executeQuery(app, query, nil, "")
		if err != nil {
			t.Fatalf("executeQuery() error = %v", err)
		}

		var data struct {
			Nibs []struct {
				ID string `json:"id"`
			} `json:"nibs"`
		}

		if err := json.Unmarshal(result, &data); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if len(data.Nibs) != 1 {
			t.Errorf("expected 1 nib with status 'todo', got %d", len(data.Nibs))
		}
		if len(data.Nibs) > 0 && data.Nibs[0].ID != "test-1" {
			t.Errorf("expected nib id 'test-1', got %q", data.Nibs[0].ID)
		}
	})

	t.Run("query with variables", func(t *testing.T) {
		query := `query GetNib($id: ID!) { nib(id: $id) { id title } }`
		variables := map[string]any{
			"id": "test-2",
		}
		result, err := executeQuery(app, query, variables, "GetNib")
		if err != nil {
			t.Fatalf("executeQuery() error = %v", err)
		}

		var data struct {
			Nib struct {
				ID    string `json:"id"`
				Title string `json:"title"`
			} `json:"nib"`
		}

		if err := json.Unmarshal(result, &data); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if data.Nib.ID != "test-2" {
			t.Errorf("expected id 'test-2', got %q", data.Nib.ID)
		}
	})

	t.Run("query nonexistent nib returns null", func(t *testing.T) {
		query := `{ nib(id: "nonexistent") { id } }`
		result, err := executeQuery(app, query, nil, "")
		if err != nil {
			t.Fatalf("executeQuery() error = %v", err)
		}

		var data struct {
			Nib *struct {
				ID string `json:"id"`
			} `json:"nib"`
		}

		if err := json.Unmarshal(result, &data); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if data.Nib != nil {
			t.Errorf("expected null nib, got %+v", data.Nib)
		}
	})

	t.Run("invalid query returns error", func(t *testing.T) {
		query := `{ invalid { field } }`
		_, err := executeQuery(app, query, nil, "")
		if err == nil {
			t.Fatal("expected error for invalid query, got nil")
		}
		if !strings.Contains(err.Error(), "graphql") {
			t.Errorf("expected error to contain 'graphql', got %q", err.Error())
		}
	})
}

func TestExecuteQueryWithRelationships(t *testing.T) {
	app := setupQueryTestApp(t)

	// Create parent nib
	parent := &nib.Nib{
		ID:     "parent-1",
		Slug:   "parent-nib",
		Title:  "Parent Nib",
		Status: "todo",
	}
	if err := app.Core.Create(parent); err != nil {
		t.Fatalf("failed to create parent nib: %v", err)
	}

	// Create child nib with parent link
	child := &nib.Nib{
		ID:     "child-1",
		Slug:   "child-nib",
		Title:  "Child Nib",
		Status: "todo",
		Parent: "parent-1",
	}
	if err := app.Core.Create(child); err != nil {
		t.Fatalf("failed to create child nib: %v", err)
	}

	// Create blocker nib
	blocker := &nib.Nib{
		ID:     "blocker-1",
		Slug:   "blocker-nib",
		Title:  "Blocker Nib",
		Status: "todo",
	}
	if err := app.Core.Create(blocker); err != nil {
		t.Fatalf("failed to create blocker nib: %v", err)
	}
	// Single-side: add blocking relationship via child's blockedBy
	child.BlockedBy = []string{"blocker-1"}
	if err := app.Core.Update(child, nil); err != nil {
		t.Fatalf("failed to update child nib: %v", err)
	}

	t.Run("query parent relationship", func(t *testing.T) {
		query := `{ nib(id: "child-1") { id parent { id title } } }`
		result, err := executeQuery(app, query, nil, "")
		if err != nil {
			t.Fatalf("executeQuery() error = %v", err)
		}

		var data struct {
			Nib struct {
				ID     string `json:"id"`
				Parent *struct {
					ID    string `json:"id"`
					Title string `json:"title"`
				} `json:"parent"`
			} `json:"nib"`
		}

		if err := json.Unmarshal(result, &data); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if data.Nib.Parent == nil {
			t.Fatal("expected parent to be set")
		}
		if data.Nib.Parent.ID != "parent-1" {
			t.Errorf("expected parent id 'parent-1', got %q", data.Nib.Parent.ID)
		}
	})

	t.Run("query children relationship", func(t *testing.T) {
		query := `{ nib(id: "parent-1") { id children { id title } } }`
		result, err := executeQuery(app, query, nil, "")
		if err != nil {
			t.Fatalf("executeQuery() error = %v", err)
		}

		var data struct {
			Nib struct {
				ID       string `json:"id"`
				Children []struct {
					ID    string `json:"id"`
					Title string `json:"title"`
				} `json:"children"`
			} `json:"nib"`
		}

		if err := json.Unmarshal(result, &data); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if len(data.Nib.Children) != 1 {
			t.Errorf("expected 1 child, got %d", len(data.Nib.Children))
		}
		if len(data.Nib.Children) > 0 && data.Nib.Children[0].ID != "child-1" {
			t.Errorf("expected child id 'child-1', got %q", data.Nib.Children[0].ID)
		}
	})

	t.Run("query blockedBy relationship", func(t *testing.T) {
		query := `{ nib(id: "child-1") { id blockedBy { id title } } }`
		result, err := executeQuery(app, query, nil, "")
		if err != nil {
			t.Fatalf("executeQuery() error = %v", err)
		}

		var data struct {
			Nib struct {
				ID        string `json:"id"`
				BlockedBy []struct {
					ID    string `json:"id"`
					Title string `json:"title"`
				} `json:"blockedBy"`
			} `json:"nib"`
		}

		if err := json.Unmarshal(result, &data); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if len(data.Nib.BlockedBy) != 1 {
			t.Errorf("expected 1 blocker, got %d", len(data.Nib.BlockedBy))
		}
		if len(data.Nib.BlockedBy) > 0 && data.Nib.BlockedBy[0].ID != "blocker-1" {
			t.Errorf("expected blocker id 'blocker-1', got %q", data.Nib.BlockedBy[0].ID)
		}
	})

	t.Run("query blocking relationship", func(t *testing.T) {
		query := `{ nib(id: "blocker-1") { id blocking { id title } } }`
		result, err := executeQuery(app, query, nil, "")
		if err != nil {
			t.Fatalf("executeQuery() error = %v", err)
		}

		var data struct {
			Nib struct {
				ID       string `json:"id"`
				Blocking []struct {
					ID    string `json:"id"`
					Title string `json:"title"`
				} `json:"blocking"`
			} `json:"nib"`
		}

		if err := json.Unmarshal(result, &data); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if len(data.Nib.Blocking) != 1 {
			t.Errorf("expected 1 blocked nib, got %d", len(data.Nib.Blocking))
		}
		if len(data.Nib.Blocking) > 0 && data.Nib.Blocking[0].ID != "child-1" {
			t.Errorf("expected blocked id 'child-1', got %q", data.Nib.Blocking[0].ID)
		}
	})
}

func TestExecuteQueryWithFilters(t *testing.T) {
	app := setupQueryTestApp(t)

	// Create nibs with different types and priorities
	b1 := &nib.Nib{
		ID:       "bug-1",
		Slug:     "bug-one",
		Title:    "Bug One",
		Status:   "todo",
		Type:     "bug",
		Priority: "critical",
		Tags:     []string{"frontend"},
	}
	b2 := &nib.Nib{
		ID:       "feat-1",
		Slug:     "feature-one",
		Title:    "Feature One",
		Status:   "in-progress",
		Type:     "feature",
		Priority: "high",
		Tags:     []string{"backend"},
	}
	b3 := &nib.Nib{
		ID:       "task-1",
		Slug:     "task-one",
		Title:    "Task One",
		Status:   "completed",
		Type:     "task",
		Priority: "normal",
		Tags:     []string{"frontend", "backend"},
	}

	if err := app.Core.Create(b1); err != nil {
		t.Fatalf("failed to create b1: %v", err)
	}
	if err := app.Core.Create(b2); err != nil {
		t.Fatalf("failed to create b2: %v", err)
	}
	if err := app.Core.Create(b3); err != nil {
		t.Fatalf("failed to create b3: %v", err)
	}

	t.Run("filter by type", func(t *testing.T) {
		query := `{ nibs(filter: { type: ["bug"] }) { id type } }`
		result, err := executeQuery(app, query, nil, "")
		if err != nil {
			t.Fatalf("executeQuery() error = %v", err)
		}

		var data struct {
			Nibs []struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"nibs"`
		}

		if err := json.Unmarshal(result, &data); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if len(data.Nibs) != 1 {
			t.Errorf("expected 1 nib with type 'bug', got %d", len(data.Nibs))
		}
	})

	t.Run("filter by priority", func(t *testing.T) {
		query := `{ nibs(filter: { priority: ["critical", "high"] }) { id priority } }`
		result, err := executeQuery(app, query, nil, "")
		if err != nil {
			t.Fatalf("executeQuery() error = %v", err)
		}

		var data struct {
			Nibs []struct {
				ID       string `json:"id"`
				Priority string `json:"priority"`
			} `json:"nibs"`
		}

		if err := json.Unmarshal(result, &data); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if len(data.Nibs) != 2 {
			t.Errorf("expected 2 nibs with priority 'critical' or 'high', got %d", len(data.Nibs))
		}
	})

	t.Run("filter by tags", func(t *testing.T) {
		query := `{ nibs(filter: { tags: ["frontend"] }) { id tags } }`
		result, err := executeQuery(app, query, nil, "")
		if err != nil {
			t.Fatalf("executeQuery() error = %v", err)
		}

		var data struct {
			Nibs []struct {
				ID   string   `json:"id"`
				Tags []string `json:"tags"`
			} `json:"nibs"`
		}

		if err := json.Unmarshal(result, &data); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if len(data.Nibs) != 2 {
			t.Errorf("expected 2 nibs with tag 'frontend', got %d", len(data.Nibs))
		}
	})

	t.Run("exclude by status", func(t *testing.T) {
		query := `{ nibs(filter: { excludeStatus: ["completed"] }) { id status } }`
		result, err := executeQuery(app, query, nil, "")
		if err != nil {
			t.Fatalf("executeQuery() error = %v", err)
		}

		var data struct {
			Nibs []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"nibs"`
		}

		if err := json.Unmarshal(result, &data); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if len(data.Nibs) != 2 {
			t.Errorf("expected 2 nibs (excluding completed), got %d", len(data.Nibs))
		}
		for _, b := range data.Nibs {
			if b.Status == "completed" {
				t.Errorf("should not include completed nibs, got nib with status %q", b.Status)
			}
		}
	})

	t.Run("combined filters", func(t *testing.T) {
		query := `{ nibs(filter: { status: ["todo", "in-progress"], type: ["bug", "feature"] }) { id } }`
		result, err := executeQuery(app, query, nil, "")
		if err != nil {
			t.Fatalf("executeQuery() error = %v", err)
		}

		var data struct {
			Nibs []struct {
				ID string `json:"id"`
			} `json:"nibs"`
		}

		if err := json.Unmarshal(result, &data); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if len(data.Nibs) != 2 {
			t.Errorf("expected 2 nibs matching combined filters, got %d", len(data.Nibs))
		}
	})
}

func TestGetGraphQLSchema(t *testing.T) {
	schema := GetGraphQLSchema()

	// Verify schema contains expected types
	expectedTypes := []string{
		"type Query",
		"type Nib",
		"input NibFilter",
	}

	for _, expected := range expectedTypes {
		if !strings.Contains(schema, expected) {
			t.Errorf("schema missing expected type: %s", expected)
		}
	}

	// Verify schema contains expected fields
	expectedFields := []string{
		"nib(id: ID!)",
		"nibs(filter: NibFilter, sort: NibSort)",
		"blockedBy",
		"blocking",
		"parent",
		"children",
	}

	for _, expected := range expectedFields {
		if !strings.Contains(schema, expected) {
			t.Errorf("schema missing expected field: %s", expected)
		}
	}

	// Verify no introspection fields
	if strings.Contains(schema, "__schema") || strings.Contains(schema, "__type") {
		t.Error("schema should not contain introspection fields")
	}
}

func TestGraphQLSchemaWithoutNibsDir(t *testing.T) {
	// Bug: graphql --schema fails when no .nibs directory exists because
	// PersistentPreRunE errors before RunE can short-circuit with querySchemaOnly.
	t.Setenv("NIBS_PATH", "")

	// Work from a temp dir with no .nibs directory
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	// Reset the global flag state after test
	oldVal := querySchemaOnly
	t.Cleanup(func() { querySchemaOnly = oldVal })

	rootCmd.SetArgs([]string{"graphql", "--schema"})
	err = rootCmd.Execute()
	if err != nil {
		t.Fatalf("graphql --schema should work without .nibs directory, got: %v", err)
	}
}

func TestReadFromStdin(t *testing.T) {
	// Note: Testing stdin behavior is tricky in unit tests.
	// This tests the function when stdin is a terminal (returns empty).
	// Integration tests would need to actually pipe data.
	t.Run("returns empty when stdin is terminal", func(t *testing.T) {
		// In a test environment, stdin is typically a terminal
		result, err := readFromStdin()
		if err != nil {
			t.Fatalf("readFromStdin() error = %v", err)
		}
		// Result will be empty string when stdin is a terminal
		if result != "" {
			t.Logf("readFromStdin() returned %q (may vary by test environment)", result)
		}
	})
}
