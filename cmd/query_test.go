package cmd

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
