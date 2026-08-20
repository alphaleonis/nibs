package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/executor"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/gqlerror"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/input"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/output"
)

func setupQueryTestApp(t *testing.T) *App {
	t.Helper()
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(storeDataDir(nibsDir), 0755); err != nil {
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

func TestSchemaWithoutNibsDir(t *testing.T) {
	// Bug: --schema fails when no .nibs directory exists because
	// PersistentPreRunE errors before RunE can short-circuit with querySchemaOnly.
	// Both the primary name ("query") and the legacy alias ("graphql") must
	// short-circuit — the PersistentPreRunE bypass keys on cmd.Name(), which is
	// "query" (derived from Use) regardless of the invocation spelling.
	for _, name := range []string{"query", "graphql"} {
		t.Run(name, func(t *testing.T) {
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

			rootCmd.SetArgs([]string{name, "--schema"})
			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("%s --schema should work without .nibs directory, got: %v", name, err)
			}
		})
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

// resetQueryFlags restores the query command's global flag state (and the root
// persistent flags) to defaults so full-command tests don't leak into each
// other.
func resetQueryFlags() {
	queryJSON = false
	queryVariables = ""
	queryOperation = ""
	querySchemaOnly = false
	resetRootPersistentFlags()
}

// TestResolveQuery pins the positional query resolution: "@FILE" reads a file
// and a bare inline string passes through verbatim, while a missing "@FILE" is
// an *input.IOError (→ FILE_ERROR) and an empty "@" is a usage error.
func TestResolveQuery(t *testing.T) {
	dir := t.TempDir()
	qfile := filepath.Join(dir, "q.graphql")
	if err := os.WriteFile(qfile, []byte(`{ nib(id:"x"){ id } }`), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		args    []string
		want    string
		wantErr bool
		wantIO  bool // expect an *input.IOError (→ FILE_ERROR)
	}{
		{"inline passthrough", []string{`{ nibs { id } }`}, `{ nibs { id } }`, false, false},
		{"file", []string{"@" + qfile}, `{ nib(id:"x"){ id } }`, false, false},
		{"missing file", []string{"@" + filepath.Join(dir, "nope.graphql")}, "", true, true},
		{"empty @", []string{"@"}, "", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveQuery(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				var ioErr *input.IOError
				if gotIO := errors.As(err, &ioErr); gotIO != tt.wantIO {
					t.Errorf("IOError = %v, want %v (err = %v)", gotIO, tt.wantIO, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("resolveQuery() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestResolveQueryStdin covers the "-" channel by feeding a pipe through
// os.Stdin.
func TestResolveQueryStdin(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = orig })

	go func() {
		_, _ = io.WriteString(w, `{ nibs { id } }`)
		_ = w.Close()
	}()

	got, err := resolveQuery([]string{"-"})
	if err != nil {
		t.Fatalf("resolveQuery(\"-\") error = %v", err)
	}
	if got != `{ nibs { id } }` {
		t.Errorf("resolveQuery(\"-\") = %q, want the piped query", got)
	}
}

// TestResolveVariables pins --variables resolution: empty → nil, inline JSON and
// "@FILE" both parse, a missing file is an *input.IOError (→ FILE_ERROR), and
// malformed JSON is a validation-class error (not an IOError).
func TestResolveVariables(t *testing.T) {
	dir := t.TempDir()
	vfile := filepath.Join(dir, "vars.json")
	if err := os.WriteFile(vfile, []byte(`{"id":"abc"}`), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		value   string
		wantID  string // expected variables["id"]; "" means a nil map
		wantErr bool
		wantIO  bool
	}{
		{"empty means nil", "", "", false, false},
		{"inline json", `{"id":"abc"}`, "abc", false, false},
		{"file json", "@" + vfile, "abc", false, false},
		{"missing file", "@" + filepath.Join(dir, "nope.json"), "", true, true},
		{"bad json", `{not json}`, "", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vars, err := resolveVariables(tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				var ioErr *input.IOError
				if gotIO := errors.As(err, &ioErr); gotIO != tt.wantIO {
					t.Errorf("IOError = %v, want %v (err = %v)", gotIO, tt.wantIO, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantID == "" {
				if vars != nil {
					t.Errorf("expected nil variables, got %v", vars)
				}
				return
			}
			if got, _ := vars["id"].(string); got != tt.wantID {
				t.Errorf("variables[id] = %q, want %q", got, tt.wantID)
			}
		})
	}
}

// TestQueryCommandFileInputContract runs the real command end-to-end via an
// "@FILE" query and pins the output contract: the selection is returned
// directly under {nib} with NO data:/success: wrapper.
func TestQueryCommandFileInputContract(t *testing.T) {
	t.Cleanup(resetQueryFlags)
	resetQueryFlags()

	nibsDir, id := writeSetNib(t, "shape-1", "body")
	qfile := filepath.Join(t.TempDir(), "q.graphql")
	if err := os.WriteFile(qfile, []byte(`{ nib(id:"`+id+`"){ id } }`), 0644); err != nil {
		t.Fatal(err)
	}

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "query", "@" + qfile, "--json"})
	var execErr error
	out := captureStdout(t, func() { execErr = rootCmd.Execute() })
	if execErr != nil {
		t.Fatalf("query @FILE should succeed, got: %v", execErr)
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &top); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if _, ok := top["nib"]; !ok {
		t.Errorf("expected top-level {nib}, got: %s", out)
	}
	if _, ok := top["data"]; ok {
		t.Errorf("output must NOT carry a data: wrapper: %s", out)
	}
	if _, ok := top["success"]; ok {
		t.Errorf("output must NOT carry a success: wrapper: %s", out)
	}
}

// TestQueryCommandGraphqlAlias verifies the legacy "graphql" alias still runs
// the command and yields the same {nib} contract.
func TestQueryCommandGraphqlAlias(t *testing.T) {
	t.Cleanup(resetQueryFlags)
	resetQueryFlags()

	nibsDir, id := writeSetNib(t, "alias-1", "body")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "graphql", `{ nib(id:"` + id + `"){ id } }`, "--json"})
	var execErr error
	out := captureStdout(t, func() { execErr = rootCmd.Execute() })
	if execErr != nil {
		t.Fatalf("graphql alias should still work, got: %v", execErr)
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &top); err != nil {
		t.Fatalf("alias output is not valid JSON: %v\n%s", err, out)
	}
	if _, ok := top["nib"]; !ok {
		t.Errorf("alias output missing top-level {nib}: %s", out)
	}
}

// TestQueryCommandMissingFileIsFileError verifies a missing "@FILE" fails
// through the coded boundary as FILE_ERROR (exit 5).
func TestQueryCommandMissingFileIsFileError(t *testing.T) {
	t.Cleanup(resetQueryFlags)
	resetQueryFlags()

	nibsDir, _ := writeSetNib(t, "mf-1", "body")
	missing := filepath.Join(t.TempDir(), "nope.graphql")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "query", "@" + missing})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected an error for a missing @FILE, got nil")
	}
	var ce *output.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *output.CodedError, got %T: %v", err, err)
	}
	if ce.Code != output.ErrFileError {
		t.Errorf("code = %q, want %q", ce.Code, output.ErrFileError)
	}
	if got := output.ExitCode(ce.Code); got != output.ExitIO {
		t.Errorf("exit code = %d, want %d (ExitIO)", got, output.ExitIO)
	}
}

// TestQueryCommandGraphqlErrorIsCoded verifies a GraphQL validation failure
// routes through the coded boundary with a validation exit (2).
// TestExecuteQueryNestedFilterRefusalIsReportedOnce pins that one mistake
// produces one sentence.
//
// A nested resolver runs ApplyFilter per matched parent and each call raises its
// own error, so a single bad id in children(filter:) yields one error per nib the
// outer query matched — the whole store when the outer query is unfiltered. The
// copies are byte-identical and all of them land in one --json error.message, in
// front of an agent whose context is the scarce resource. The count of nibs is
// what varies here, so the assertion is on the count of occurrences, not on the
// text.
func TestExecuteQueryNestedFilterRefusalIsReportedOnce(t *testing.T) {
	app := setupQueryTestApp(t)
	for _, id := range []string{"dup-1", "dup-2", "dup-3", "dup-4"} {
		createQueryTestNib(t, app.Core, id, "Nib "+id, "todo")
	}

	_, err := executeQuery(app, `{ nibs { id children(filter: {parentId: "zz"}) { id } } }`, nil, "")
	if err != nil {
		msg := err.Error()
		if got := strings.Count(msg, `no nib with id "zz"`); got != 1 {
			t.Errorf("the refusal is reported %d times, want 1:\n%s", got, msg)
		}
		return
	}
	t.Fatal("a nested filter naming no nib returned no error")
}

// TestExecuteQueryEmptyFilterTargetIsAValidationError covers the surface that
// is the ONLY one for five of the eight id-valued filter fields. cmd/list.go
// refuses --parent "", --mentions "" and --mentioned-by "" on the flag layer,
// but ancestorId, descendantId, siblingId, blockingId and blockedById have no
// flag, so a query is the only way an empty value reaches ApplyFilter at all.
//
// ancestorId is the representative because it takes the GENERIC message branch,
// unlike the parentId every other construction in this package builds. A pass
// says the refusal survives the whole path — resolver, executor, gqlgen's error
// wrapping, classifier — and arrives as exit 2 rather than the exit 3 a
// not-found would give or the exit 0 the dropped branch used to.
func TestExecuteQueryEmptyFilterTargetIsAValidationError(t *testing.T) {
	app := setupQueryTestApp(t)
	createQueryTestNib(t, app.Core, "eft-1", "Nib one", "todo")

	_, err := executeQuery(app, `{ nibs(filter: {ancestorId: ""}) { id } }`, nil, "")
	if err == nil {
		t.Fatal(`an ancestorId of "" returned no error; the branch was dropped instead of refused`)
	}
	var ce *output.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *output.CodedError, got %T: %v", err, err)
	}
	if ce.Code != output.ErrValidation {
		t.Errorf("code = %q, want %q — an empty id is malformed input, not a missing nib", ce.Code, output.ErrValidation)
	}
	if got := output.ExitCode(ce.Code); got != output.ExitValidation {
		t.Errorf("exit code = %d, want %d (ExitValidation)", got, output.ExitValidation)
	}
	if !strings.Contains(ce.Error(), "ancestorId") {
		t.Errorf("the message does not name the field the caller wrote: %s", ce.Error())
	}
}

func TestQueryCommandGraphqlErrorIsCoded(t *testing.T) {
	t.Cleanup(resetQueryFlags)
	resetQueryFlags()

	nibsDir, _ := writeSetNib(t, "gqlerr-1", "body")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "query", `{ notAField { id } }`})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected an error for an invalid query, got nil")
	}
	var ce *output.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *output.CodedError, got %T: %v", err, err)
	}
	if got := output.ExitCode(ce.Code); got != output.ExitValidation {
		t.Errorf("exit code = %d, want %d (ExitValidation)", got, output.ExitValidation)
	}
}

// TestFormatGraphQLErrorsCarriesTheCode pins that the code decided from the
// gqlerror.List reaches the caller: the list does not outlive formatGraphQLErrors,
// so the returned error is the only channel left. The message contract
// (dedup to one sentence) is asserted alongside it because both ride on the
// same returned value.
func TestFormatGraphQLErrorsCarriesTheCode(t *testing.T) {
	err := formatGraphQLErrors(gqlerror.List{notFoundErr(), notFoundErr()}, rootFieldOutcome{})
	var ce *output.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("formatGraphQLErrors returned %T, want *output.CodedError", err)
	}
	if ce.Code != output.ErrNotFound {
		t.Errorf("code = %q, want %q", ce.Code, output.ErrNotFound)
	}
	if got := strings.Count(ce.Error(), `no nib with id "zz"`); got != 1 {
		t.Errorf("the refusal is reported %d times, want 1:\n%s", got, ce.Error())
	}
	// TWO classified failures, so no cause may be attributed — even though dedup
	// collapsed them to one sentence. The cause scan runs over the raw list for
	// exactly this reason; reusing the deduped one would see a sole failure here
	// and hand back a repair hint that speaks for only one of two lost races.
	if cause := ce.Unwrap(); cause != nil {
		t.Errorf("two identical-message refusals attributed a cause: %v", cause)
	}
	if formatGraphQLErrors(gqlerror.List{}, rootFieldOutcome{}) != nil {
		t.Error("an empty error list must format to nil, not to a coded failure")
	}
}

// TestFormatGraphQLErrorsCarriesASoleClassifiedCause pins WHICH responses hand a
// usable cause back to the caller, because that is what decides whether the
// query envelope can carry a repair hint like currentEtag.
//
// The rule is "exactly one classified error". Zero means there is nothing to
// attribute; two or more means attributing to any one of them would be a guess,
// and a single top-level currentEtag genuinely cannot represent N per-mutation
// etags in a batch. Every boundary of that condition is a row here: none, one
// that is a mismatch, one that is not, two mismatches, and a mismatch beside an
// unclassified failure.
func TestFormatGraphQLErrorsCarriesASoleClassifiedCause(t *testing.T) {
	tests := []struct {
		name         string
		errs         gqlerror.List
		wantCode     string
		wantCause    bool
		wantMismatch bool
	}{
		{
			name:     "no classified error carries no cause",
			errs:     gqlerror.List{gqlerror.Errorf("one"), gqlerror.Errorf("two")},
			wantCode: output.ErrValidation,
		},
		{
			name:         "a sole etag mismatch stays reachable",
			errs:         gqlerror.List{conflictErr()},
			wantCode:     output.ErrConflict,
			wantCause:    true,
			wantMismatch: true,
		},
		{
			// Classified, but not a conflict — the cause is carried all the same;
			// it is the CODE, not the cause, that gates the currentEtag enrichment.
			name:      "a sole non-conflict refusal is carried too",
			errs:      gqlerror.List{notFoundErr()},
			wantCode:  output.ErrNotFound,
			wantCause: true,
		},
		{
			// Both agree on CONFLICT, so the response IS a conflict — but neither
			// mismatch may speak for the other, so no cause is offered and the
			// envelope stays a bare CONFLICT.
			name:     "two etag mismatches carry neither",
			errs:     gqlerror.List{conflictErr(), otherConflictErr()},
			wantCode: output.ErrConflict,
		},
		{
			// Exactly one classified error, so "sole" is satisfied — but its own
			// class (CONFLICT) is not the response's (UNCATEGORIZED), so it may
			// not speak for the response. Withholding it here is what makes a
			// cause that contradicts its own code unrepresentable, rather than
			// merely unreachable because the RunE happens to gate on the code.
			name:     "a mismatch whose class is not the response's is not the response's cause",
			errs:     gqlerror.List{conflictErr(), gqlerror.Errorf("resolver blew up")},
			wantCode: output.ErrUncategorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := formatGraphQLErrors(tt.errs, rootFieldOutcome{})
			var ce *output.CodedError
			if !errors.As(err, &ce) {
				t.Fatalf("formatGraphQLErrors returned %T, want *output.CodedError", err)
			}
			if ce.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", ce.Code, tt.wantCode)
			}
			if got := ce.Unwrap() != nil; got != tt.wantCause {
				t.Errorf("carries a cause = %v, want %v (cause: %v)", got, tt.wantCause, ce.Unwrap())
			}
			var mismatch *nibcore.ETagMismatchError
			if got := errors.As(err, &mismatch); got != tt.wantMismatch {
				t.Errorf("errors.As finds an ETagMismatchError = %v, want %v", got, tt.wantMismatch)
			}
		})
	}
}

// runQueryCmd drives `nibs query <args...>` through the full Cobra pipeline
// against nibsDir and returns captured stdout plus the Execute error, so the
// exit status can be asserted through the real boundary (reportExitError).
func runQueryCmd(t *testing.T, nibsDir string, args ...string) (string, error) {
	t.Helper()
	rootCmd.SetArgs(append([]string{"--nibs-path", nibsDir, "query"}, args...))
	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	return out, execErr
}

// conflictEnvelope is the {code,message,currentEtag} shape a CONFLICT emits, used
// to compare `nibs query`'s envelope against `nibs set`'s for the same failure.
type conflictEnvelope struct {
	Error struct {
		Code        string `json:"code"`
		Message     string `json:"message"`
		CurrentEtag string `json:"currentEtag"`
	} `json:"error"`
}

// writeStaleEtagNib creates a nib, computes its etag, then advances the nib past
// that etag with one un-guarded mutation. It returns the nibs dir, the id, and
// the now-stale token — the setup every reconcilable-conflict assertion needs.
func writeStaleEtagNib(t *testing.T, id string) (string, string, string) {
	t.Helper()
	nibsDir := filepath.Join(t.TempDir(), ".nibs")
	if err := os.MkdirAll(storeDataDir(nibsDir), 0755); err != nil {
		t.Fatal(err)
	}
	// version: 1 and the `# id` comment match Render() output, so the etag
	// computed from the file agrees with the one the core computes.
	content := "---\n# " + id + "\nversion: 2\ntitle: Test\nstatus: todo\ntype: task\norder: a0\n---\n\n"
	path := dataPath(nibsDir, id+"--test.md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := nib.Parse(f)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	parsed.ID = id
	stale := parsed.ETag()

	resetSetFlags()
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "set", id, "--status", "in-progress"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("priming mutation should succeed, got: %v", err)
	}
	return nibsDir, id, stale
}

// TestQueryConflictEnvelopeMatchesTheDirectCommand pins that a CONFLICT reported
// through `nibs query` carries the same reconcile token as the same conflict
// reported through `nibs set`.
//
// The exit status alone is not the contract. internal/output documents an absent
// currentEtag on a CONFLICT as a POSITIVE signal — "no reusable token, this
// conflict cannot be reconciled" — so a query claiming exit 4 while omitting the
// field would steer an agent past a retry that would have worked. Both commands
// run against one nib and one stale token, and the two envelopes are compared
// field for field.
func TestQueryConflictEnvelopeMatchesTheDirectCommand(t *testing.T) {
	t.Cleanup(resetQueryFlags)
	t.Cleanup(resetSetFlags)

	nibsDir, id, stale := writeStaleEtagNib(t, "q-cnf")

	resetSetFlags()
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "set", id, "--status", "todo", "--if-match", stale, "--json"})
	var setErr error
	setOut := captureStdout(t, func() { setErr = rootCmd.Execute() })
	if setErr == nil {
		t.Fatal("`set --if-match <stale>` returned no error")
	}
	var setEnv conflictEnvelope
	if err := json.Unmarshal([]byte(setOut), &setEnv); err != nil {
		t.Fatalf("set output is not a JSON error envelope: %v\nraw: %s", err, setOut)
	}
	if setEnv.Error.CurrentEtag == "" {
		t.Fatalf("precondition failed: `set` emitted no currentEtag: %s", setOut)
	}

	resetQueryFlags()
	query := `mutation { updateNib(id: "` + id + `", input: {title: "x", ifMatch: "` + stale + `"}) { id } }`
	queryOut, queryErr := runQueryCmd(t, nibsDir, query, "--json")
	if queryErr == nil {
		t.Fatalf("query with a stale ifMatch returned no error; out: %q", queryOut)
	}
	if code := reportExitError(io.Discard, queryErr); code != output.ExitConflict {
		t.Errorf("query exit = %d, want %d (conflict)", code, output.ExitConflict)
	}
	var queryEnv conflictEnvelope
	if err := json.Unmarshal([]byte(queryOut), &queryEnv); err != nil {
		t.Fatalf("query output is not a JSON error envelope: %v\nraw: %s", err, queryOut)
	}
	if queryEnv.Error.Code != output.ErrConflict {
		t.Errorf("query envelope code = %q, want %q", queryEnv.Error.Code, output.ErrConflict)
	}
	if queryEnv.Error.CurrentEtag != setEnv.Error.CurrentEtag {
		t.Errorf("query currentEtag = %q, want %q (the token `set` reports for the same conflict)",
			queryEnv.Error.CurrentEtag, setEnv.Error.CurrentEtag)
	}
	if queryEnv.Error.CurrentEtag == stale {
		t.Errorf("currentEtag must differ from the stale token; both were %q", stale)
	}
	// The refusal still reads as one sentence prefixed by the transport, exactly
	// as the non-enriched path rendered it.
	if !strings.HasPrefix(queryEnv.Error.Message, "graphql: ") {
		t.Errorf("query message lost its transport prefix: %q", queryEnv.Error.Message)
	}
}

// TestQueryConflictEnrichmentIsWithheld pins the two command-level cases where a
// real, reachable ETagMismatchError must NOT produce a currentEtag. Each isolates
// one of the two conditions guarding the enrichment, so neither can be dropped
// without a failure here.
//
// A partial retry token is worse than none: an agent handed one etag for a
// two-mutation batch would retry believing it had reconciled the whole document.
func TestQueryConflictEnrichmentIsWithheld(t *testing.T) {
	tests := []struct {
		name     string
		mutation func(id, stale string) string
		wantCode string
		wantExit int
	}{
		{
			// Both errors agree on CONFLICT, so the response IS a conflict — but
			// two mismatches mean no single etag speaks for the document. Guards
			// soleClassifiedErr's "more than one" arm.
			name: "two conflicting mutations offer no single etag",
			mutation: func(id, stale string) string {
				return `mutation { a: updateNib(id: "` + id + `", input: {title: "x", ifMatch: "` + stale + `"}) { id } ` +
					`b: updateNib(id: "` + id + `", input: {title: "y", ifMatch: "also-stale"}) { id } }`
			},
			wantCode: output.ErrConflict,
			wantExit: output.ExitConflict,
		},
		{
			// Exactly ONE classified error, and it IS a mismatch — so the cause is
			// reachable — but an unclassified failure beside it makes the codes
			// disagree, so the response never claims CONFLICT and must not offer a
			// conflict's repair hint. Guards the code gate in the query RunE.
			name: "a mismatch beside an unclassified failure is not a conflict claim",
			mutation: func(id, stale string) string {
				return `mutation { a: updateNib(id: "` + id + `", input: {title: "x", ifMatch: "` + stale + `"}) { id } ` +
					`b: updateNib(id: "` + id + `", input: {status: "banana"}) { id } }`
			},
			wantCode: output.ErrUncategorized,
			wantExit: output.ExitError,
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(resetQueryFlags)
			t.Cleanup(resetSetFlags)

			nibsDir, id, stale := writeStaleEtagNib(t, fmt.Sprintf("q-wh%d", i))

			resetQueryFlags()
			out, err := runQueryCmd(t, nibsDir, tt.mutation(id, stale), "--json")
			if err == nil {
				t.Fatalf("mutation returned no error; out: %q", out)
			}
			if code := reportExitError(io.Discard, err); code != tt.wantExit {
				t.Errorf("exit = %d, want %d (%s)", code, tt.wantExit, tt.wantCode)
			}
			var env conflictEnvelope
			if uerr := json.Unmarshal([]byte(out), &env); uerr != nil {
				t.Fatalf("output is not a JSON error envelope: %v\nraw: %s", uerr, out)
			}
			if env.Error.Code != tt.wantCode {
				t.Errorf("envelope code = %q, want %q", env.Error.Code, tt.wantCode)
			}
			if env.Error.CurrentEtag != "" {
				t.Errorf("no currentEtag may be offered here, got %q", env.Error.CurrentEtag)
			}
		})
	}
}

// TestQueryConflictCarriesTheCommittedClauseThroughTheEnrichment pins the
// ifMatch retry hazard end to end, at the command boundary rather than at
// executeQuery.
//
// A CONFLICT is the one class that does not reach the caller as executeQuery's
// own error: the query RunE routes it through etagConflictError, which mints a
// FRESH output.ErrorConflict from err.Error() so the currentEtag can ride along.
// The clause naming what committed survives that re-wrap only because the re-wrap
// reuses the rendered message. An etagConflictError that composed its own
// sentence instead would drop it silently, leaving the caller to resend a batch
// whose first half already landed — with no test failing.
//
// The reported currentEtag is asserted to differ from the token `a` consumed,
// which is the churn in one line: `a`'s write moved the nib past the etag the
// caller held, so a blind resend of the whole batch loses the race again.
func TestQueryConflictCarriesTheCommittedClauseThroughTheEnrichment(t *testing.T) {
	t.Cleanup(resetQueryFlags)
	t.Cleanup(resetSetFlags)

	nibsDir, id, stale := writeStaleEtagNib(t, "q-cmt")

	readEtagAndTitle := func() (string, string) {
		t.Helper()
		resetQueryFlags()
		out, err := runQueryCmd(t, nibsDir, `{ nib(id: "`+id+`") { etag title } }`, "--json")
		if err != nil {
			t.Fatalf("reading the nib back failed: %v", err)
		}
		var read struct {
			Nib struct {
				Etag  string `json:"etag"`
				Title string `json:"title"`
			} `json:"nib"`
		}
		if uerr := json.Unmarshal([]byte(out), &read); uerr != nil {
			t.Fatalf("read-back is not JSON: %v\nraw: %s", uerr, out)
		}
		return read.Nib.Etag, read.Nib.Title
	}

	fresh, _ := readEtagAndTitle()
	if fresh == "" || fresh == stale {
		t.Fatalf("precondition failed: fresh etag %q must be non-empty and differ from the stale one %q", fresh, stale)
	}

	resetQueryFlags()
	mutation := `mutation { a: updateNib(id: "` + id + `", input: {title: "A2", ifMatch: "` + fresh + `"}) { id } ` +
		`b: updateNib(id: "` + id + `", input: {title: "B2", ifMatch: "` + stale + `"}) { id } }`
	out, err := runQueryCmd(t, nibsDir, mutation, "--json")
	if err == nil {
		t.Fatalf("a batch holding a stale ifMatch returned no error; out: %q", out)
	}

	// The premise, asserted before the message: `a` really landed, so naming it
	// is not a vacuous claim — and `b` really did not.
	if _, title := readEtagAndTitle(); title != "A2" {
		t.Fatalf("premise broken: title = %q, want %q (a must have committed and b must not have)", title, "A2")
	}

	if code := reportExitError(io.Discard, err); code != output.ExitConflict {
		t.Errorf("exit = %d, want %d (conflict)", code, output.ExitConflict)
	}
	var env conflictEnvelope
	if uerr := json.Unmarshal([]byte(out), &env); uerr != nil {
		t.Fatalf("output is not a JSON error envelope: %v\nraw: %s", uerr, out)
	}
	if env.Error.Code != output.ErrConflict {
		t.Errorf("envelope code = %q, want %q", env.Error.Code, output.ErrConflict)
	}
	if env.Error.CurrentEtag == "" {
		t.Fatalf("the conflict offered no currentEtag, so no retry is reconcilable: %s", out)
	}
	if env.Error.CurrentEtag == fresh {
		t.Errorf("currentEtag = %q, the token `a` consumed — `a`'s write must have moved it past that", fresh)
	}
	if !strings.Contains(env.Error.Message, "a succeeded") {
		t.Errorf("the enrichment dropped the committed clause: %q", env.Error.Message)
	}
	if !strings.Contains(env.Error.Message, "b failed") {
		t.Errorf("the enrichment dropped the failure attribution: %q", env.Error.Message)
	}
	if !strings.HasPrefix(env.Error.Message, "graphql: ") {
		t.Errorf("the enrichment lost the transport prefix: %q", env.Error.Message)
	}
}

// TestQueryBulkReorderPreValidationConflictIsAConflict pins that a bulk reorder
// refused by its PRE-VALIDATION etag check reports CONFLICT (exit 4) with the
// server's current etag, exactly as a stale single-nib `updateNib` does.
//
// It is the common bulk-reorder conflict — it fires whenever the caller's ifMatch
// is already stale on entry, while the racing per-nib mismatch needs the narrow
// window between pre-validation and the write — so an agent scripting `nibs query`
// against $? sees this one, not the racing one. Reporting it as a plain
// VALIDATION_ERROR would tell that agent its INPUT was malformed, when the repair
// is to re-read the etags and retry.
func TestQueryBulkReorderPreValidationConflictIsAConflict(t *testing.T) {
	t.Cleanup(resetQueryFlags)
	t.Cleanup(resetSetFlags)

	nibsDir, id, stale := writeStaleEtagNib(t, "q-brc")

	// A second root-level nib, so childIds can list every root sibling —
	// reorderChildren refuses an incomplete list before it ever checks an etag.
	const sibling = "q-brs"
	content := "---\n# " + sibling + "\nversion: 2\ntitle: Sibling\nstatus: todo\ntype: task\norder: b0\n---\n\n"
	if err := os.WriteFile(dataPath(nibsDir, sibling+"--test.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	resetQueryFlags()
	mutation := `mutation { reorderChildren(parentId: "", childIds: ["` + sibling + `", "` + id + `"], ` +
		`ifMatch: [{id: "` + id + `", etag: "` + stale + `"}]) { id } }`
	out, err := runQueryCmd(t, nibsDir, mutation, "--json")
	if err == nil {
		t.Fatalf("a bulk reorder holding a stale ifMatch returned no error; out: %q", out)
	}

	if code := reportExitError(io.Discard, err); code != output.ExitConflict {
		t.Errorf("exit = %d, want %d (conflict)", code, output.ExitConflict)
	}
	var env conflictEnvelope
	if uerr := json.Unmarshal([]byte(out), &env); uerr != nil {
		t.Fatalf("output is not a JSON error envelope: %v\nraw: %s", uerr, out)
	}
	if env.Error.Code != output.ErrConflict {
		t.Errorf("envelope code = %q, want %q", env.Error.Code, output.ErrConflict)
	}
	// The reconcile token: an agent retries the reorder with it. It comes off the
	// typed error's Current field, so its presence also witnesses the type.
	if env.Error.CurrentEtag == "" {
		t.Fatalf("the conflict offered no currentEtag, so no retry is reconcilable: %s", out)
	}
	if env.Error.CurrentEtag == stale {
		t.Errorf("currentEtag = %q, the stale token the caller sent", env.Error.CurrentEtag)
	}
	if !strings.Contains(env.Error.Message, id) {
		t.Errorf("message = %q, want it to name the nib whose etag was stale", env.Error.Message)
	}
}

// TestQueryCommandRefusalExitsLikeTheDirectCommand pins that `nibs query`
// reports the same structured class as the direct command that raises the same
// failure. `nibs list --parent nope` exits 3 and `nibs set --if-match stale`
// exits 4; routing the identical refusal through the general-purpose query
// surface used to flatten both to 2, so an agent branching on $? — which
// cmd/prompt-full.tmpl tells it to do — lost the distinction on the surface
// most likely to be scripted.
//
// The assertion is on the exit status via the real boundary, not merely on
// err != nil, and it runs in both output modes because the code has to survive
// the JSON envelope as well as the stderr path.
func TestQueryCommandRefusalExitsLikeTheDirectCommand(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		wantCode string
		wantExit int
	}{
		{
			name:     "unknown filter target is not-found",
			query:    `{ nibs(filter: {parentId: "zz"}) { id } }`,
			wantCode: output.ErrNotFound,
			wantExit: output.ExitNotFound,
		},
		{
			// The contradictory pair, driven through gqlgen's own parsing and
			// argument binding. Every other test of this class either builds the
			// error value by hand (errors_test.go, serve_errorpresenter_test.go)
			// or calls ApplyFilter with an already-built model.NibFilter
			// (internal/graph), so a regression in how parentId/hasParent bind
			// from query text would leave all of them green while this surface
			// stopped refusing. The id need not resolve: the pair is refused
			// before any lookup, which is why this row wants VALIDATION_ERROR
			// where the row above wants NOT_FOUND on the same id.
			name:     "contradictory filter pair is a validation error",
			query:    `{ nibs(filter: {parentId: "zz", hasParent: false}) { id } }`,
			wantCode: output.ErrValidation,
			wantExit: output.ExitValidation,
		},
		{
			name:     "unknown mutation target is not-found",
			query:    `mutation { updateNib(id: "nosuch", input: {title: "x"}) { id } }`,
			wantCode: output.ErrNotFound,
			wantExit: output.ExitNotFound,
		},
		{
			name:     "stale if-match is a conflict",
			query:    `mutation { updateNib(id: "q-etag", input: {title: "x", ifMatch: "stale"}) { id } }`,
			wantCode: output.ErrConflict,
			wantExit: output.ExitConflict,
		},
		{
			// Two refusals of the same class agree, so the class survives.
			name:     "two agreeing refusals keep the code",
			query:    `{ a: nibs(filter: {parentId: "zz"}) { id } b: nibs(filter: {ancestorId: "yy"}) { id } }`,
			wantCode: output.ErrNotFound,
			wantExit: output.ExitNotFound,
		},
		{
			// Codes disagree (CONFLICT + NOT_FOUND), so neither may be claimed —
			// and neither may VALIDATION_ERROR, which asserts a caller-input fault
			// no error here reports. The response is uncategorized (exit 1).
			name: "mixed codes are uncategorized",
			query: `mutation { a: updateNib(id: "q-etag", input: {title: "x", ifMatch: "stale"}) { id } ` +
				`b: updateNib(id: "nosuch", input: {title: "y"}) { id } }`,
			wantCode: output.ErrUncategorized,
			wantExit: output.ExitError,
		},
		{
			// gqlgen's own validation failure is a mistake in the caller's
			// document, not a nib-level refusal: it must stay exit 2.
			name:     "unknown field stays validation",
			query:    `{ notAField { id } }`,
			wantCode: output.ErrValidation,
			wantExit: output.ExitValidation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+" --json", func(t *testing.T) {
			t.Cleanup(resetQueryFlags)
			resetQueryFlags()
			nibsDir, _ := writeSetNib(t, "q-etag", "body")

			out, err := runQueryCmd(t, nibsDir, tt.query, "--json")
			if err == nil {
				t.Fatalf("query %s --json returned no error; out: %q", tt.query, out)
			}
			if code := reportExitError(io.Discard, err); code != tt.wantExit {
				t.Errorf("exit code = %d, want %d (%s)", code, tt.wantExit, tt.wantCode)
			}
			var env struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if uerr := json.Unmarshal([]byte(out), &env); uerr != nil {
				t.Fatalf("stdout is not a JSON error envelope: %v\nraw: %s", uerr, out)
			}
			if env.Error.Code != tt.wantCode {
				t.Errorf("envelope error.code=%q, want %q", env.Error.Code, tt.wantCode)
			}
		})

		t.Run(tt.name+" text", func(t *testing.T) {
			t.Cleanup(resetQueryFlags)
			resetQueryFlags()
			nibsDir, _ := writeSetNib(t, "q-etag", "body")

			out, err := runQueryCmd(t, nibsDir, tt.query)
			if err == nil {
				t.Fatalf("query %s returned no error; out: %q", tt.query, out)
			}
			if code := reportExitError(io.Discard, err); code != tt.wantExit {
				t.Errorf("exit code = %d, want %d (%s)", code, tt.wantExit, tt.wantCode)
			}
		})
	}
}

// addFeatureNib writes a second illegal-reparent subject into an existing
// fixture dir: a feature, whose legal parents (milestone, epic) differ from an
// epic's (milestone alone). Two refusals carrying DIFFERENT allowed sets are
// what makes a single-valued repair hint unrepresentable for a batch.
func addFeatureNib(t *testing.T, nibsDir, id string) string {
	t.Helper()
	content := "---\nversion: 2\ntitle: Feature\nstatus: todo\ntype: feature\norder: c0\n---\n"
	if err := os.WriteFile(dataPath(nibsDir, id+"--feature.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return id
}

// TestHierarchyEnvelopeIsTheSameOnEveryWriteSurface pins that the four surfaces
// that can attempt an illegal parent link — `nibs new`, `nibs mv`, `nibs set` and
// `nibs query` — refuse one identically: same code, same exit status, and the
// same allowedParentTypes repair hint.
//
// Comparing the envelopes rather than asserting four independent expectations is
// the point: the hint is the field that turns a refusal into a next action, and
// a surface that omits it leaves an agent to re-derive the hierarchy rule. All
// four run against one fixture and one refusal (an epic under a task), so any
// difference is the code under test.
//
// `query` is compared on code, exit and hint but not on message text: it renders
// every failure through the transport prefix its own contract documents, so the
// assertion is that its message is that prefix plus the sentence the direct
// commands print.
func TestHierarchyEnvelopeIsTheSameOnEveryWriteSurface(t *testing.T) {
	t.Cleanup(resetQueryFlags)
	t.Cleanup(resetSetFlags)
	t.Cleanup(resetMvFlags)
	t.Cleanup(resetNewFlags)
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")

	nibsDir, epicID, taskID := writeIllegalReparentFixture(t)

	run := func(t *testing.T, reset func(), args ...string) hierarchyEnvelope {
		t.Helper()
		reset()
		rootCmd.SetArgs(append([]string{"--nibs-path", nibsDir}, args...))
		var execErr error
		out := captureStdout(t, func() { execErr = rootCmd.Execute() })
		if execErr == nil {
			t.Fatalf("%v returned no error; out: %q", args, out)
		}
		if code := reportExitError(io.Discard, execErr); code != output.ExitValidation {
			t.Errorf("%v exit = %d, want %d (validation)", args, code, output.ExitValidation)
		}
		var env hierarchyEnvelope
		if err := json.Unmarshal([]byte(out), &env); err != nil {
			t.Fatalf("%v stdout is not a JSON error envelope: %v\nraw: %s", args, err, out)
		}
		return env
	}

	// `new` creates a fresh epic under the task rather than moving one, but the
	// refused link — and so the hint — is the same.
	newEnv := run(t, resetNewFlags, "new", "New Epic", "-t", "epic", "--parent", taskID, "--json")
	mvEnv := run(t, resetMvFlags, "mv", epicID, "--parent", taskID, "--json")
	setEnv := run(t, resetSetFlags, "set", epicID, "--parent", taskID, "--json")
	queryEnv := run(t, resetQueryFlags, "query", "--json",
		`mutation { updateNib(id: "`+epicID+`", input: {parent: "`+taskID+`"}) { id } }`)

	surfaces := map[string]hierarchyEnvelope{"new": newEnv, "mv": mvEnv, "set": setEnv, "query": queryEnv}
	for name, env := range surfaces {
		if env.Error.Code != output.ErrHierarchy {
			t.Errorf("%s envelope code = %q, want %q", name, env.Error.Code, output.ErrHierarchy)
		}
		want := []string{"milestone"}
		if !slices.Equal(env.Error.AllowedParentTypes, want) {
			t.Errorf("%s allowedParentTypes = %v, want %v", name, env.Error.AllowedParentTypes, want)
		}
	}
	if mvEnv.Error.Message != setEnv.Error.Message || mvEnv.Error.Message != newEnv.Error.Message {
		t.Errorf("direct commands disagree on the refusal sentence:\n  new: %q\n  mv:  %q\n  set: %q",
			newEnv.Error.Message, mvEnv.Error.Message, setEnv.Error.Message)
	}
	if want := "graphql: " + mvEnv.Error.Message; queryEnv.Error.Message != want {
		t.Errorf("query message = %q, want %q", queryEnv.Error.Message, want)
	}
}

// TestQueryHierarchyBatchExitsLikeTheDirectCommands pins how a response holding
// an illegal reparent aggregates, which is where classifying HIERARCHY at all
// could have cost more than it bought.
//
// HIERARCHY and VALIDATION_ERROR are different codes that share exit 2, so a
// rule comparing code strings reports UNCATEGORIZED — exit 1 — for a batch whose
// every failure the direct commands exit 2 for. The rows below fix each
// aggregation outcome at the command surface: alone the refusal keeps its own
// code and its hint; beside another exit-2 failure it generalizes to the class's
// code and withholds the hint; beside a different exit class it is uncategorized.
func TestQueryHierarchyBatchExitsLikeTheDirectCommands(t *testing.T) {
	tests := []struct {
		name       string
		mutation   func(epicID, featureID, taskID string) string
		wantCode   string
		wantExit   int
		wantHint   []string
		wantNoHint bool
	}{
		{
			// One refusal, one allowed set: the hint is representable and the
			// specific code survives.
			name: "a lone illegal reparent keeps HIERARCHY and its hint",
			mutation: func(epicID, _, taskID string) string {
				return `mutation { updateNib(id: "` + epicID + `", input: {parent: "` + taskID + `"}) { id } }`
			},
			wantCode: output.ErrHierarchy,
			wantExit: output.ExitValidation,
			wantHint: []string{"milestone"},
		},
		{
			// Two refusals of the same class agree on the code, so it survives —
			// but their allowed sets differ (milestone for an epic, milestone or
			// epic for a feature) and one field cannot speak for both.
			name: "two illegal reparents keep HIERARCHY but offer no single hint",
			mutation: func(epicID, featureID, taskID string) string {
				return `mutation { a: updateNib(id: "` + epicID + `", input: {parent: "` + taskID + `"}) { id } ` +
					`b: updateNib(id: "` + featureID + `", input: {parent: "` + taskID + `"}) { id } }`
			},
			wantCode:   output.ErrHierarchy,
			wantExit:   output.ExitValidation,
			wantNoHint: true,
		},
		{
			// The measured regression: both failures are caller-input faults the
			// direct commands exit 2 for (`nibs mv` HIERARCHY, `nibs set -s bogus`
			// VALIDATION_ERROR), so the batch must stay exit 2. The code
			// generalizes to the class rather than claiming either specific
			// refusal about the other's failure.
			name: "an illegal reparent beside a bad status stays exit 2",
			mutation: func(epicID, _, taskID string) string {
				return `mutation { a: updateNib(id: "` + epicID + `", input: {parent: "` + taskID + `"}) { id } ` +
					`b: updateNib(id: "` + taskID + `", input: {status: "bogus"}) { id } }`
			},
			wantCode:   output.ErrValidation,
			wantExit:   output.ExitValidation,
			wantNoHint: true,
		},
		{
			// Different exit classes: exit 2 asserts the caller's input was at
			// fault, which the missing id beside it does not support.
			name: "an illegal reparent beside an unknown id is uncategorized",
			mutation: func(epicID, _, taskID string) string {
				return `mutation { a: updateNib(id: "` + epicID + `", input: {parent: "` + taskID + `"}) { id } ` +
					`b: updateNib(id: "nosuch", input: {title: "x"}) { id } }`
			},
			wantCode:   output.ErrUncategorized,
			wantExit:   output.ExitError,
			wantNoHint: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(resetQueryFlags)
			resetQueryFlags()
			nibsDir, epicID, taskID := writeIllegalReparentFixture(t)
			featureID := addFeatureNib(t, nibsDir, "ft")

			out, err := runQueryCmd(t, nibsDir, tt.mutation(epicID, featureID, taskID), "--json")
			if err == nil {
				t.Fatalf("mutation returned no error; out: %q", out)
			}
			if code := reportExitError(io.Discard, err); code != tt.wantExit {
				t.Errorf("exit = %d, want %d (%s)", code, tt.wantExit, tt.wantCode)
			}
			var env hierarchyEnvelope
			if uerr := json.Unmarshal([]byte(out), &env); uerr != nil {
				t.Fatalf("stdout is not a JSON error envelope: %v\nraw: %s", uerr, out)
			}
			if env.Error.Code != tt.wantCode {
				t.Errorf("envelope code = %q, want %q", env.Error.Code, tt.wantCode)
			}
			if tt.wantNoHint {
				if len(env.Error.AllowedParentTypes) != 0 {
					t.Errorf("no allowedParentTypes may be offered here, got %v", env.Error.AllowedParentTypes)
				}
				return
			}
			if !slices.Equal(env.Error.AllowedParentTypes, tt.wantHint) {
				t.Errorf("allowedParentTypes = %v, want %v", env.Error.AllowedParentTypes, tt.wantHint)
			}
		})
	}
}

// writeReplaceFixture creates a nibs dir holding two nibs with bodies chosen so
// every surgical-replace outcome is reachable from one store: the subject's
// "dup" occurs twice and the other's "trip" three times, so an ambiguous replace
// on each carries a DIFFERENT count, and any absent text is a zero-match on
// either. Two counts that differ are what makes a single-valued occurrences
// field unrepresentable for a batch. It returns (nibsDir, subjectID, otherID).
func writeReplaceFixture(t *testing.T) (string, string, string) {
	t.Helper()
	nibsDir, subjectID := writeSetNib(t, "rp", "dup here\nand dup there\n")
	content := "---\nversion: 2\ntitle: Other\nstatus: todo\ntype: task\n---\ntrip\ntrip\ntrip\n"
	if err := os.WriteFile(dataPath(nibsDir, "ot--other.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return nibsDir, subjectID, "ot"
}

// replaceMutation builds a single-alias updateNib carrying one bodyMod.replace.
func replaceMutation(alias, id, old string) string {
	return alias + `: updateNib(id: "` + id + `", input: {bodyMod: {replace: [{old: "` + old + `", new: "x"}]}}) { id }`
}

// TestQueryTextMatchEnvelopeMatchesTheDirectCommand pins that the two surfaces
// that can refuse a surgical replace — `nibs body` and `nibs query` — refuse one
// identically: same code, same exit status, and the same occurrences count.
//
// Comparing the envelopes rather than asserting two independent expectations is
// the point: occurrences is the field an agent branches on to tell "your text
// was not there" (retry with different text) from "your text was ambiguous"
// (extend the search text), and both refusals exit 2, so a caller reading $?
// alone cannot tell them apart. cmd/prompt-full.tmpl steers agents at the query
// route specifically, so it is the surface that must not answer more weakly.
//
// `query` is compared on code, exit and count but not on message text: it
// renders every failure through the transport prefix its own contract
// documents, so the assertion is that its message is that prefix plus the
// sentence `nibs body` prints.
func TestQueryTextMatchEnvelopeMatchesTheDirectCommand(t *testing.T) {
	tests := []struct {
		name            string
		old             string
		wantCode        string
		wantOccurrences int
	}{
		{"a zero-match replace", "absent-text", output.ErrTextNotFound, 0},
		{"an ambiguous replace", "dup", output.ErrTextAmbiguous, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(resetQueryFlags)
			t.Cleanup(resetBodyFlags)
			nibsDir, subjectID, _ := writeReplaceFixture(t)

			run := func(reset func(), args ...string) textMatchEnvelope {
				t.Helper()
				reset()
				rootCmd.SetArgs(append([]string{"--nibs-path", nibsDir}, args...))
				var execErr error
				out := captureStdout(t, func() { execErr = rootCmd.Execute() })
				if execErr == nil {
					t.Fatalf("%v returned no error; out: %q", args, out)
				}
				if code := reportExitError(io.Discard, execErr); code != output.ExitValidation {
					t.Errorf("%v exit = %d, want %d (validation)", args, code, output.ExitValidation)
				}
				var env textMatchEnvelope
				if err := json.Unmarshal([]byte(out), &env); err != nil {
					t.Fatalf("%v stdout is not a JSON error envelope: %v\nraw: %s", args, err, out)
				}
				return env
			}

			bodyEnv := run(resetBodyFlags, "body", subjectID,
				"--replace-old", tt.old, "--replace-new", "x", "--json")
			queryEnv := run(resetQueryFlags, "query", "--json",
				`mutation { `+replaceMutation("a", subjectID, tt.old)+` }`)

			for name, env := range map[string]textMatchEnvelope{"body": bodyEnv, "query": queryEnv} {
				if env.Error.Code != tt.wantCode {
					t.Errorf("%s envelope code = %q, want %q", name, env.Error.Code, tt.wantCode)
				}
				if env.Error.Occurrences == nil {
					t.Errorf("%s envelope omits occurrences, want %d", name, tt.wantOccurrences)
					continue
				}
				if *env.Error.Occurrences != tt.wantOccurrences {
					t.Errorf("%s envelope occurrences = %d, want %d",
						name, *env.Error.Occurrences, tt.wantOccurrences)
				}
			}
			if want := "graphql: " + bodyEnv.Error.Message; queryEnv.Error.Message != want {
				t.Errorf("query message = %q, want %q", queryEnv.Error.Message, want)
			}
		})
	}
}

// TestQueryTextMatchBatchExitsLikeTheDirectCommands pins how a response holding
// a refused surgical replace aggregates.
//
// TEXT_NOT_FOUND, TEXT_AMBIGUOUS and VALIDATION_ERROR are three different codes
// on one exit status, so a rule comparing code strings reports UNCATEGORIZED —
// exit 1 — for a batch whose every failure `nibs body` exits 2 for. The rows fix
// each aggregation outcome at the command surface: alone the refusal keeps its
// own code and its count; beside another exit-2 failure it generalizes to the
// class's code and withholds the count; beside a different exit class it is
// uncategorized.
func TestQueryTextMatchBatchExitsLikeTheDirectCommands(t *testing.T) {
	tests := []struct {
		name            string
		mutation        func(subjectID, otherID string) string
		wantCode        string
		wantExit        int
		wantOccurrences int
		wantNoCount     bool
	}{
		{
			// One refusal, one count: it is representable and the specific code
			// survives.
			name: "a lone zero-match keeps TEXT_NOT_FOUND and its count",
			mutation: func(subjectID, _ string) string {
				return replaceMutation("a", subjectID, "absent-text")
			},
			wantCode:        output.ErrTextNotFound,
			wantExit:        output.ExitValidation,
			wantOccurrences: 0,
		},
		{
			name: "a lone ambiguous match keeps TEXT_AMBIGUOUS and its count",
			mutation: func(subjectID, _ string) string {
				return replaceMutation("a", subjectID, "dup")
			},
			wantCode:        output.ErrTextAmbiguous,
			wantExit:        output.ExitValidation,
			wantOccurrences: 2,
		},
		{
			// Two refusals of the same kind agree on the code, so it survives —
			// but their counts differ (2 and 3) and one field cannot speak for
			// both.
			name: "two ambiguous matches keep TEXT_AMBIGUOUS but offer no single count",
			mutation: func(subjectID, otherID string) string {
				return replaceMutation("a", subjectID, "dup") + " " +
					replaceMutation("b", otherID, "trip")
			},
			wantCode:    output.ErrTextAmbiguous,
			wantExit:    output.ExitValidation,
			wantNoCount: true,
		},
		{
			// Both are CLASSIFIED and both exit 2 — the case a code-string
			// comparison cannot express at all. Neither specific claim covers
			// the other's failure, so the response reports the class.
			name: "a zero-match beside an ambiguous match generalizes",
			mutation: func(subjectID, otherID string) string {
				return replaceMutation("a", subjectID, "absent-text") + " " +
					replaceMutation("b", otherID, "trip")
			},
			wantCode:    output.ErrValidation,
			wantExit:    output.ExitValidation,
			wantNoCount: true,
		},
		{
			// Both failures are caller-input faults the direct commands exit 2
			// for (`nibs body --replace-old` TEXT_NOT_FOUND, `nibs set -s bogus`
			// VALIDATION_ERROR), so the batch must stay exit 2 while asserting
			// neither specific claim.
			name: "a zero-match beside a bad status stays exit 2",
			mutation: func(subjectID, otherID string) string {
				return replaceMutation("a", subjectID, "absent-text") + " " +
					`b: updateNib(id: "` + otherID + `", input: {status: "bogus"}) { id }`
			},
			wantCode:    output.ErrValidation,
			wantExit:    output.ExitValidation,
			wantNoCount: true,
		},
		{
			// Different exit classes: exit 2 asserts the caller's input was at
			// fault, which the missing id beside it does not support.
			name: "a zero-match beside an unknown id is uncategorized",
			mutation: func(subjectID, _ string) string {
				return replaceMutation("a", subjectID, "absent-text") + " " +
					`b: updateNib(id: "nosuch", input: {title: "x"}) { id }`
			},
			wantCode:    output.ErrUncategorized,
			wantExit:    output.ExitError,
			wantNoCount: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(resetQueryFlags)
			resetQueryFlags()
			nibsDir, subjectID, otherID := writeReplaceFixture(t)

			out, err := runQueryCmd(t, nibsDir, "mutation { "+tt.mutation(subjectID, otherID)+" }", "--json")
			if err == nil {
				t.Fatalf("mutation returned no error; out: %q", out)
			}
			if code := reportExitError(io.Discard, err); code != tt.wantExit {
				t.Errorf("exit = %d, want %d (%s)", code, tt.wantExit, tt.wantCode)
			}
			var env textMatchEnvelope
			if uerr := json.Unmarshal([]byte(out), &env); uerr != nil {
				t.Fatalf("stdout is not a JSON error envelope: %v\nraw: %s", uerr, out)
			}
			if env.Error.Code != tt.wantCode {
				t.Errorf("envelope code = %q, want %q", env.Error.Code, tt.wantCode)
			}
			if tt.wantNoCount {
				if env.Error.Occurrences != nil {
					t.Errorf("no occurrences count may be offered here, got %d", *env.Error.Occurrences)
				}
				return
			}
			if env.Error.Occurrences == nil {
				t.Fatalf("envelope omits occurrences, want %d", tt.wantOccurrences)
			}
			if *env.Error.Occurrences != tt.wantOccurrences {
				t.Errorf("occurrences = %d, want %d", *env.Error.Occurrences, tt.wantOccurrences)
			}
		})
	}
}

// TestExecuteQueryBatchNamesTheMutationsThatCommitted pins the reproduction the
// whole change exists for: three deletes, the middle id unknown, and the two
// real files gone by the time the response is built.
//
// gqlgen runs root mutation fields serially and does NOT stop at the first
// failure — the _Mutation loop in internal/graph/generated.go assigns every
// out.Values[i] — and each resolver commits to disk on its own. So the response
// is a refusal that has already destroyed two files, and the message has to say
// which. The store assertions run ahead of the message assertions because they
// are the premise: without them a passing message would prove nothing about what
// landed.
func TestExecuteQueryBatchNamesTheMutationsThatCommitted(t *testing.T) {
	app := setupQueryTestApp(t)
	for _, id := range []string{"p-igir", "p-rxbx"} {
		createQueryTestNib(t, app.Core, id, "Nib "+id, "todo")
	}

	_, err := executeQuery(app,
		`mutation { d1: deleteNib(id:"p-igir") d2: deleteNib(id:"p-zzzz") d3: deleteNib(id:"p-rxbx") }`,
		nil, "")
	if err == nil {
		t.Fatal("a batch naming an unknown id returned no error")
	}

	for _, id := range []string{"p-igir", "p-rxbx"} {
		if _, getErr := app.Core.Get(id); getErr == nil {
			t.Fatalf("premise broken: %s survived the batch, so nothing was silently committed", id)
		}
	}

	var ce *output.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *output.CodedError, got %T: %v", err, err)
	}
	if ce.Code != output.ErrNotFound {
		t.Errorf("code = %q, want %q — naming what committed must not change the class", ce.Code, output.ErrNotFound)
	}
	if got := output.ExitCode(ce.Code); got != output.ExitNotFound {
		t.Errorf("exit code = %d, want %d (ExitNotFound)", got, output.ExitNotFound)
	}
	const want = "graphql: nib not found; d1, d3 succeeded; d2 failed"
	if ce.Error() != want {
		t.Errorf("message = %q, want %q", ce.Error(), want)
	}
}

// TestExecuteQueryBatchHalfCommitsDurably pins the contract the Mutation type
// documents: root mutation fields run serially in document order, execution
// does not stop at the first failure, and each field commits on its own. So a
// document whose one alias is refused leaves the other alias's write DURABLE —
// on disk, not merely agreed to by the store that issued it.
//
// That is why every assertion here reads the nibs directory instead of
// app.Core. The neighboring naming tests read the store on purpose, because
// what they pin is the message; a store-only assertion cannot tell a write that
// reached the filesystem from one that only updated the map beside it, and
// durability is the whole of what this contract promises.
//
// Rows come in pairs — the committing field before the refusal and after it —
// because those are different claims. Only the "after" rows can fail if
// execution ever stops at the first failure.
func TestExecuteQueryBatchHalfCommitsDurably(t *testing.T) {
	// nibFiles lists what the store's data directory holds for an id. It globs
	// the id prefix rather than reading the Path the store reports, because
	// asking the store where to look would be asking the store — the very thing
	// these assertions exist to avoid. Globbing also stays correct if a write
	// ever does relocate a file; today none does, since saveToDisk rebuilds a
	// filename only when Path is empty and a nib from GetForUpdate always
	// carries one.
	nibFiles := func(t *testing.T, root, id string) []string {
		t.Helper()
		matches, err := filepath.Glob(dataPath(root, id+"*.md"))
		if err != nil {
			t.Fatalf("globbing for %s under %s: %v", id, root, err)
		}
		return matches
	}
	goneFromDisk := func(id string) func(*testing.T, string) {
		return func(t *testing.T, root string) {
			t.Helper()
			if files := nibFiles(t, root, id); len(files) != 0 {
				t.Errorf("%s still has a file on disk (%v), so its delete did not commit", id, files)
			}
		}
	}
	titleOnDisk := func(id, want string) func(*testing.T, string) {
		return func(t *testing.T, root string) {
			t.Helper()
			files := nibFiles(t, root, id)
			if len(files) != 1 {
				t.Fatalf("want exactly one file on disk for %s, got %v", id, files)
			}
			f, err := os.Open(files[0])
			if err != nil {
				t.Fatalf("opening %s: %v", files[0], err)
			}
			defer func() { _ = f.Close() }()
			b, err := nib.Parse(f)
			if err != nil {
				t.Fatalf("parsing %s: %v", files[0], err)
			}
			if b.Title != want {
				t.Errorf("%s on disk has title %q, want %q — its update did not commit", id, b.Title, want)
			}
		}
	}

	const committedTitle = "Committed to disk"

	tests := []struct {
		name  string
		query string
		check func(*testing.T, string)
	}{
		{
			name:  "a delete before the refusal is gone from disk",
			query: `mutation { d1: deleteNib(id:"p-a") d2: deleteNib(id:"p-none") }`,
			check: goneFromDisk("p-a"),
		},
		{
			// The row that separates "does not stop at the first failure" from
			// "stops at the first failure": d2 only runs if the refusal ahead of
			// it did not end the operation.
			name:  "a delete after the refusal is gone from disk too",
			query: `mutation { d1: deleteNib(id:"p-none") d2: deleteNib(id:"p-b") }`,
			check: goneFromDisk("p-b"),
		},
		{
			// A rewrite rather than an unlink, so the other persistence path is
			// covered: the file survives and has to carry the new title.
			name: "an update before the refusal has its new title on disk",
			query: `mutation { u1: updateNib(id:"p-a", input:{title:"` + committedTitle + `"}) { id } ` +
				`d2: deleteNib(id:"p-none") }`,
			check: titleOnDisk("p-a", committedTitle),
		},
		{
			name: "an update after the refusal has its new title on disk",
			query: `mutation { d1: deleteNib(id:"p-none") ` +
				`u2: updateNib(id:"p-b", input:{title:"` + committedTitle + `"}) { id } }`,
			check: titleOnDisk("p-b", committedTitle),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := setupQueryTestApp(t)
			for _, id := range []string{"p-a", "p-b"} {
				createQueryTestNib(t, app.Core, id, "Nib "+id, "todo")
			}
			// A lookup that found nothing would make every "gone from disk"
			// assertion below pass without measuring anything, so establish
			// that it finds the files while they are still there.
			for _, id := range []string{"p-a", "p-b"} {
				if files := nibFiles(t, app.Core.Root(), id); len(files) != 1 {
					t.Fatalf("setup: want exactly one file on disk for %s, got %v", id, files)
				}
			}

			_, err := executeQuery(app, tt.query, nil, "")
			// The premise: the batch was refused as a whole. Without a refusal
			// there is no half-commit to be durable about.
			if err == nil {
				t.Fatal("the batch returned no error, so nothing here is a partial outcome")
			}
			var ce *output.CodedError
			if !errors.As(err, &ce) {
				t.Fatalf("expected *output.CodedError, got %T: %v", err, err)
			}
			if ce.Code != output.ErrNotFound {
				t.Errorf("code = %q, want %q", ce.Code, output.ErrNotFound)
			}

			tt.check(t, app.Core.Root())
		})
	}
}

// TestExecuteQueryCommittedFieldNaming walks every boundary of "which root
// fields does this response claim committed, and which does it blame".
//
// The claim is derived from the OPERATION DOCUMENT split by the root keys named
// by error paths, never from resp.Data. Data cannot answer it here: every field of
// the Mutation type is non-null, so one failed root field nullifies the whole
// object and resp.Data is the literal `null` — measured, not assumed. Error
// paths, by contrast, are populated on this path (gqlgen presents
// gqlerror.WrapPath(graphql.GetPath(ctx), err)) and their first element is the
// response key: the alias when the caller wrote one, the field name otherwise.
//
// Order comes from the document, not from the error list, so nothing here rests
// on an order gqlgen does not guarantee — root QUERY fields are dispatched
// concurrently and their error order is not stable (see graphQLResponseCode).
func TestExecuteQueryCommittedFieldNaming(t *testing.T) {
	// wroteTitle asserts a nib carries the title a committed update would have
	// left; survived and deleted assert a nib is still in, or gone from, the
	// store. Rows whose reason rests on what actually landed on disk carry one,
	// so the claim is measured rather than inferred from the message alone.
	wroteTitle := func(id, title string) func(*testing.T, *App) {
		return func(t *testing.T, app *App) {
			t.Helper()
			b, err := app.Core.Get(id)
			if err != nil {
				t.Fatalf("%s is gone: %v", id, err)
			}
			if b.Title != title {
				t.Errorf("%s title = %q, want %q", id, b.Title, title)
			}
		}
	}
	survived := func(id string) func(*testing.T, *App) {
		return func(t *testing.T, app *App) {
			t.Helper()
			if _, err := app.Core.Get(id); err != nil {
				t.Errorf("%s was deleted: %v", id, err)
			}
		}
	}
	deleted := func(id string) func(*testing.T, *App) {
		return func(t *testing.T, app *App) {
			t.Helper()
			if _, err := app.Core.Get(id); err == nil {
				t.Errorf("%s survived, so nothing was committed for it", id)
			}
		}
	}

	tests := []struct {
		name     string
		query    string
		wantOK   bool
		wantMsg  string
		wantCode string
		check    func(*testing.T, *App)
	}{
		{
			// The common path: one root field, and it failed. Naming a
			// "0 succeeded" list here would be noise on every single-mutation
			// failure an agent makes.
			name:     "a lone failing mutation names nothing",
			query:    `mutation { deleteNib(id:"p-none") }`,
			wantMsg:  "graphql: nib not found",
			wantCode: output.ErrNotFound,
		},
		{
			name:     "two fields, one committing, names the one that landed",
			query:    `mutation { d1: deleteNib(id:"p-a") d2: deleteNib(id:"p-none") }`,
			wantMsg:  "graphql: nib not found; d1 succeeded; d2 failed",
			wantCode: output.ErrNotFound,
		},
		{
			// A type condition on the mutation root — inline here, named in the
			// row below — is what makes CollectFields consult the implementor
			// set; plain field selections never reach that branch. Collecting
			// against the wrong set would drop d1 from the document's root
			// fields, so this is where a drifted implementor list would show.
			name:     "an inline fragment on Mutation contributes its fields",
			query:    `mutation { ... on Mutation { d1: deleteNib(id:"p-a") } d2: deleteNib(id:"p-none") }`,
			wantMsg:  "graphql: nib not found; d1 succeeded; d2 failed",
			wantCode: output.ErrNotFound,
			check:    deleted("p-a"),
		},
		{
			// The named-fragment form of the row above: the spread branch of
			// CollectFields consults the same implementor set.
			name:     "a named fragment spread on Mutation contributes its fields",
			query:    `mutation { ...F d2: deleteNib(id:"p-none") } fragment F on Mutation { d1: deleteNib(id:"p-a") }`,
			wantMsg:  "graphql: nib not found; d1 succeeded; d2 failed",
			wantCode: output.ErrNotFound,
			check:    deleted("p-a"),
		},
		{
			// Two refusals that do NOT dedup, so the message takes its
			// multi-error form and the outcome has to land on its own line
			// rather than trailing the last refusal. Their exit classes differ
			// (3 and 2), so the response is uncategorized — which the outcome
			// clause must not perturb.
			name: "distinct refusals put the outcome on its own line",
			query: `mutation { d1: deleteNib(id:"p-a") d2: deleteNib(id:"p-none") ` +
				`d3: updateNib(id:"p-b", input:{status:"bogus"}) { id } }`,
			wantMsg: "graphql errors:\n  nib not found\n  " +
				`invalid status "bogus": must be one of in-progress, todo, draft, deferred, completed, scrapped` +
				"\nd1 succeeded; d2, d3 failed",
			wantCode: output.ErrUncategorized,
			check:    deleted("p-a"),
		},
		{
			// Nothing landed, so no clause is added — and the two identical
			// refusals still dedup to one sentence.
			name:     "a batch where nothing commits is unchanged",
			query:    `mutation { d1: deleteNib(id:"p-x") d2: deleteNib(id:"p-y") }`,
			wantMsg:  "graphql: nib not found",
			wantCode: output.ErrNotFound,
		},
		{
			name:   "a batch where everything commits raises no error at all",
			query:  `mutation { d1: deleteNib(id:"p-a") d2: deleteNib(id:"p-b") }`,
			wantOK: true,
		},
		{
			// No alias, so the response key IS the field name — which is what
			// the error path would have carried had it failed.
			name:     "an unaliased committing field is named by its field name",
			query:    `mutation { deleteNib(id:"p-a") z: deleteNib(id:"p-none") }`,
			wantMsg:  "graphql: nib not found; deleteNib succeeded; z failed",
			wantCode: output.ErrNotFound,
		},
		{
			// p-a has no parent, so `parent` resolves to null with no error.
			// A null in the selection is not a failure, and keying off error
			// paths rather than data is what keeps the two apart. The title
			// assertion is the premise: without it a resolver that reported
			// success without persisting would leave this row green.
			name:     "a legitimately null selection is not a failure",
			query:    `mutation { u1: updateNib(id:"p-a", input:{title:"A2"}) { id parent { id } } d2: deleteNib(id:"p-none") }`,
			wantMsg:  "graphql: nib not found; u1 succeeded; d2 failed",
			wantCode: output.ErrNotFound,
			check:    wroteTitle("p-a", "A2"),
		},
		{
			// A failing query commits nothing, so there is nothing to name even
			// though the `a` field resolved.
			name:     "a query names nothing even when a root field resolved",
			query:    `{ a: nib(id:"p-a") { id } b: nibs(filter:{parentId:"zz"}) { id } }`,
			wantMsg:  `graphql: parentId filter: no nib with id "zz"`,
			wantCode: output.ErrNotFound,
		},
		{
			// The update committed — the title assertion proves it — and only
			// its nested read failed, but the error path is rooted at u1, so u1
			// is reported failed. This deliberately declines to separate the
			// two: calling it committed would claim a write landed on the
			// strength of an inference about where gqlgen rooted the error.
			// It is also why "failed" never means "wrote nothing".
			name:     "a nested failure reports the field it sits under as failed",
			query:    `mutation { u1: updateNib(id:"p-a", input:{title:"A2"}) { children(filter:{parentId:"zz"}) { id } } d2: deleteNib(id:"p-b") }`,
			wantMsg:  `graphql: parentId filter: no nib with id "zz"; d2 succeeded; u1 failed`,
			wantCode: output.ErrNotFound,
			check:    wroteTitle("p-a", "A2"),
		},
		{
			// An introspection meta-field is collected alongside the mutations
			// and never fails, so it would otherwise be reported as a write
			// that landed. It commits nothing, so it belongs to neither list.
			name:     "__typename is never named as a mutation that committed",
			query:    `mutation { __typename d1: deleteNib(id:"p-a") d2: deleteNib(id:"p-none") }`,
			wantMsg:  "graphql: nib not found; d1 succeeded; d2 failed",
			wantCode: output.ErrNotFound,
		},
		{
			// @skip(if: true) removes the field before execution, so k1 never
			// ran and no id was deleted. Naming it would be a fabricated write.
			name:     "a skipped field never ran and is never named",
			query:    `mutation { k1: deleteNib(id:"p-a") @skip(if: true) k2: deleteNib(id:"p-none") }`,
			wantMsg:  "graphql: nib not found",
			wantCode: output.ErrNotFound,
			check:    survived("p-a"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := setupQueryTestApp(t)
			for _, id := range []string{"p-a", "p-b", "p-c"} {
				createQueryTestNib(t, app.Core, id, "Nib "+id, "todo")
			}

			_, err := executeQuery(app, tt.query, nil, "")
			if tt.check != nil {
				tt.check(t, app)
			}
			if tt.wantOK {
				if err != nil {
					t.Fatalf("executeQuery() error = %v, want none", err)
				}
				return
			}
			if err == nil {
				t.Fatal("executeQuery() returned no error")
			}
			var ce *output.CodedError
			if !errors.As(err, &ce) {
				t.Fatalf("expected *output.CodedError, got %T: %v", err, err)
			}
			if ce.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", ce.Code, tt.wantCode)
			}
			if ce.Error() != tt.wantMsg {
				t.Errorf("message = %q, want %q", ce.Error(), tt.wantMsg)
			}
		})
	}
}

// TestFormatGraphQLErrorsCommittedNamesStayOutOfCodeAndDedup pins that naming
// what committed composes with the two rules that already run over the same
// error list, neither of which may see the added text.
//
// Dedup keys on each error's own Message and the class is decided by
// graphQLResponseCode scanning the errors themselves, so the names are appended
// to the FINISHED message and touch neither. The rows are the two shapes that
// would break if they did: N identical refusals that must still collapse to one
// sentence, and a same-exit mixture that must still generalize to its class
// rather than fall to UNCATEGORIZED.
//
// The last two rows fix the gate on the whole clause: it is the COMMITTED list
// that opens it, so a response with only failed names renders the bare refusal
// and nothing more.
func TestFormatGraphQLErrorsCommittedNamesStayOutOfCodeAndDedup(t *testing.T) {
	tests := []struct {
		name      string
		errs      gqlerror.List
		outcome   rootFieldOutcome
		wantCode  string
		wantMsg   string
		wantCause bool
	}{
		{
			// Two byte-identical refusals dedup to one sentence, and the names
			// ride once at the end rather than once per copy.
			name:     "identical refusals still dedup with names appended",
			errs:     gqlerror.List{notFoundErr(), notFoundErr()},
			outcome:  rootFieldOutcome{committed: []string{"d1", "d3"}, failed: []string{"d2"}},
			wantCode: output.ErrNotFound,
			wantMsg:  `graphql: parentId filter: no nib with id "zz"; d1, d3 succeeded; d2 failed`,
		},
		{
			// A HIERARCHY beside an unclassified failure shares exit 2, so the
			// response generalizes to VALIDATION_ERROR. The names must not
			// perturb that, and the multi-error rendering puts them on their own
			// line so they cannot read as part of the last refusal.
			name:     "a same-exit mixture still generalizes to its class",
			errs:     gqlerror.List{hierarchyErr(), gqlerror.Errorf("resolver blew up")},
			outcome:  rootFieldOutcome{committed: []string{"a"}, failed: []string{"b"}},
			wantCode: output.ErrValidation,
			wantMsg: "graphql errors:\n  " + hierarchyErr().Message +
				"\n  resolver blew up\na succeeded; b failed",
		},
		{
			// A sole classified failure still hands its cause back, so the
			// repair hint (currentEtag) survives alongside the names. No failed
			// key was attributable here, so only the succeeded clause renders.
			name:      "a sole classified cause is still attributable",
			errs:      gqlerror.List{conflictErr()},
			outcome:   rootFieldOutcome{committed: []string{"a", "b"}},
			wantCode:  output.ErrConflict,
			wantMsg:   "graphql: " + conflictErr().Message + "; a, b succeeded",
			wantCause: true,
		},
		{
			// The no-commit baseline: with no names to add, the rendering is
			// the plain refusal and nothing else.
			name:     "no committed names renders the plain refusal",
			errs:     gqlerror.List{notFoundErr(), notFoundErr()},
			wantCode: output.ErrNotFound,
			wantMsg:  `graphql: parentId filter: no nib with id "zz"`,
		},
		{
			// Every root field failed, so there is no partial outcome to
			// attribute — the failed names would only restate the refusal the
			// caller already has, once per field.
			name:     "failed names alone add nothing to the refusal",
			errs:     gqlerror.List{notFoundErr(), notFoundErr()},
			outcome:  rootFieldOutcome{failed: []string{"d1", "d2"}},
			wantCode: output.ErrNotFound,
			wantMsg:  `graphql: parentId filter: no nib with id "zz"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := formatGraphQLErrors(tt.errs, tt.outcome)
			var ce *output.CodedError
			if !errors.As(err, &ce) {
				t.Fatalf("formatGraphQLErrors returned %T, want *output.CodedError", err)
			}
			if ce.Code != tt.wantCode {
				t.Errorf("code = %q, want %q — the names must not reach the code decision", ce.Code, tt.wantCode)
			}
			if ce.Error() != tt.wantMsg {
				t.Errorf("message = %q, want %q", ce.Error(), tt.wantMsg)
			}
			if got := ce.Unwrap() != nil; got != tt.wantCause {
				t.Errorf("carries a cause = %v, want %v (cause: %v)", got, tt.wantCause, ce.Unwrap())
			}
		})
	}
}

// operationContextFor builds the gqlgen operation context for a document
// without executing it, so classifyRootFields can be driven against a real
// parsed document paired with a hand-built error list.
func operationContextFor(t *testing.T, app *App, query string) *graphql.OperationContext {
	t.Helper()
	es := graph.NewExecutableSchema(graph.Config{Resolvers: app.newResolver()})
	opCtx, errs := executor.New(es).CreateOperationContext(
		graphql.StartOperationTrace(context.Background()),
		&graphql.RawParams{Query: query},
	)
	if errs != nil {
		t.Fatalf("building the operation context for %q failed: %v", query, errs)
	}
	return opCtx
}

// TestClassifyRootFieldsRequiresEveryErrorToBeAttributable drives the guard the
// end-to-end rows above cannot reach: an error gqlgen did not root at a response
// key. Every resolver failure on that path carries one, so the list is built by
// hand.
//
// The whole claim is a split — the document's root fields, partitioned by the
// ones an error names — so an error that names nothing could belong to any of
// them. Calling the rest committed would assert a write landed on a field that
// had in fact just failed, and calling them failed would assert the mirror
// image. Reporting nothing is the only answer the evidence supports.
func TestClassifyRootFieldsRequiresEveryErrorToBeAttributable(t *testing.T) {
	rooted := func(key string) *gqlerror.Error {
		return &gqlerror.Error{Message: "boom", Path: ast.Path{ast.PathName(key)}}
	}

	tests := []struct {
		name string
		errs gqlerror.List
		want rootFieldOutcome
	}{
		{
			// The baseline the guards are measured against: the named field is
			// the failure, the other one committed.
			name: "an error rooted at a response key splits the document",
			errs: gqlerror.List{rooted("d2")},
			want: rootFieldOutcome{committed: []string{"d1"}, failed: []string{"d2"}},
		},
		{
			name: "an error with no path names nothing",
			errs: gqlerror.List{{Message: "boom"}},
		},
		{
			name: "an error rooted at a list index names nothing",
			errs: gqlerror.List{{Message: "boom", Path: ast.Path{ast.PathIndex(0)}}},
		},
		{
			// One unattributable error poisons the whole split, even beside an
			// error that IS attributable.
			name: "one unattributable error suppresses both lists",
			errs: gqlerror.List{rooted("d2"), {Message: "boom"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := setupQueryTestApp(t)
			opCtx := operationContextFor(t, app, `mutation { d1: deleteNib(id:"p-a") d2: deleteNib(id:"p-b") }`)
			got := classifyRootFields(opCtx, tt.errs)
			if !slices.Equal(got.committed, tt.want.committed) {
				t.Errorf("classifyRootFields().committed = %v, want %v", got.committed, tt.want.committed)
			}
			if !slices.Equal(got.failed, tt.want.failed) {
				t.Errorf("classifyRootFields().failed = %v, want %v", got.failed, tt.want.failed)
			}
		})
	}
}

// TestNewQueryContextCarriesARequestCache pins that a CLI GraphQL invocation
// runs under a per-operation cache, not a bare context.
//
// The resolver helpers treat a missing cache as "fall straight through to the
// reader" — the right default for a helper, and silent when it is wrong. One CLI
// document selecting `children(filter: {search: q})` across N parents evaluates
// the term N times without this, and a relationship-field search reads the
// UNCAPPED answer, so each of those is a full index query over the store. The
// dedup itself is pinned in internal/graph
// (TestNestedRelationshipSearchQueriesTheIndexOncePerRequest); what this test
// pins is that the CLI installs the mechanism at all.
func TestNewQueryContextCarriesARequestCache(t *testing.T) {
	ctx := newQueryContext()

	if graph.RequestCacheFrom(ctx) == nil {
		t.Error("newQueryContext() produced a context with no RequestCache; every relationship-field search in one CLI document would re-query the index per parent")
	}
	// The cache is layered ONTO the traced context rather than replacing it:
	// the operation context is still the executor's to install afterwards.
	if graphql.HasOperationContext(ctx) {
		t.Error("newQueryContext() should not carry an operation context yet; the executor installs one")
	}
}
