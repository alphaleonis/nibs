package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vektah/gqlparser/v2/gqlerror"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/input"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/output"
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
	err := formatGraphQLErrors(gqlerror.List{notFoundErr(), notFoundErr()})
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
	if formatGraphQLErrors(gqlerror.List{}) != nil {
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
			err := formatGraphQLErrors(tt.errs)
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
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatal(err)
	}
	// version: 1 and the `# id` comment match Render() output, so the etag
	// computed from the file agrees with the one the core computes.
	content := "---\n# " + id + "\nversion: 1\ntitle: Test\nstatus: todo\ntype: task\norder: a0\n---\n\n"
	path := filepath.Join(nibsDir, id+"--test.md")
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
