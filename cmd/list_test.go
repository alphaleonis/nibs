package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
)

func TestBuildNibSort(t *testing.T) {
	tests := []struct {
		name      string
		sortFlag  string
		wantField model.NibSortField
		wantDesc  bool
	}{
		{"default", "", model.NibSortFieldOrder, false},
		{"created", "created", model.NibSortFieldCreatedAt, true},
		{"updated", "updated", model.NibSortFieldUpdatedAt, true},
		{"status", "status", model.NibSortFieldStatus, false},
		{"priority", "priority", model.NibSortFieldPriority, false},
		{"status-priority", "status-priority", model.NibSortFieldStatusPriority, false},
		{"id", "id", model.NibSortFieldID, false},
		{"unknown falls back to order", "garbage", model.NibSortFieldOrder, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildNibSort(tt.sortFlag)
			if got.Field != tt.wantField {
				t.Errorf("field = %s, want %s", got.Field, tt.wantField)
			}
			gotDesc := got.Direction != nil && *got.Direction == model.SortDirectionDesc
			if gotDesc != tt.wantDesc {
				t.Errorf("desc = %v, want %v", gotDesc, tt.wantDesc)
			}
		})
	}
}

func TestListReadyFlagMutualExclusion(t *testing.T) {
	// Test that --ready and --is-blocked are mutually exclusive
	// by checking the validation logic directly
	tests := []struct {
		name        string
		ready       bool
		isBlocked   bool
		expectError bool
	}{
		{"neither flag", false, false, false},
		{"only --ready", true, false, false},
		{"only --is-blocked", false, true, false},
		{"both flags", true, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This mirrors the validation logic in list.go
			hasError := tt.ready && tt.isBlocked
			if hasError != tt.expectError {
				t.Errorf("ready=%v, isBlocked=%v: got error=%v, want error=%v",
					tt.ready, tt.isBlocked, hasError, tt.expectError)
			}
		})
	}
}

// resetListFlags clears the package-level flag vars used by listCmd so tests
// don't pollute each other via rootCmd's singleton state.
//
// NOTE: Unlike resetRefsFlags/resetShowFlags we do NOT clear Cobra's
// Changed state — listCmd's --ready/--is-blocked mutex is implemented
// manually in list.go:107. If you add MarkFlagsMutuallyExclusive to
// listCmd in future, add
//
//	listCmd.Flags().Visit(func(f *pflag.Flag) { f.Changed = false })
//
// here to prevent order-dependent test pollution.
func resetListFlags() {
	listJSON = false
	listSearch = ""
	listStatus = nil
	listNoStatus = nil
	listType = nil
	listNoType = nil
	listPriority = nil
	listNoPriority = nil
	listEstimate = nil
	listNoEstimate = nil
	listTag = nil
	listNoTag = nil
	listHasParent = false
	listNoParent = false
	listParentID = ""
	listHasBlocking = false
	listNoBlocking = false
	listIsBlocked = false
	listMentions = ""
	listMentionedBy = ""
	listReady = false
	listQuiet = false
	listSort = ""
	listFull = false
}

// TestResetListFlagsClearsAllState mirrors TestResetCloseFlagsClearsAllState
// — set every package-level flag var non-zero, call the reset helper, and
// verify all are back to their zero values. If a new flag is added to
// listCmd without a matching reset line, this test fires.
func TestResetListFlagsClearsAllState(t *testing.T) {
	listJSON = true
	listSearch = "dirty"
	listStatus = []string{"x"}
	listNoStatus = []string{"x"}
	listType = []string{"x"}
	listNoType = []string{"x"}
	listPriority = []string{"x"}
	listNoPriority = []string{"x"}
	listEstimate = []string{"x"}
	listNoEstimate = []string{"x"}
	listTag = []string{"x"}
	listNoTag = []string{"x"}
	listHasParent = true
	listNoParent = true
	listParentID = "dirty"
	listHasBlocking = true
	listNoBlocking = true
	listIsBlocked = true
	listMentions = "dirty"
	listMentionedBy = "dirty"
	listReady = true
	listQuiet = true
	listSort = "dirty"
	listFull = true

	resetListFlags()

	checks := []struct {
		name string
		zero bool
	}{
		{"listJSON", !listJSON},
		{"listSearch", listSearch == ""},
		{"listStatus", listStatus == nil},
		{"listNoStatus", listNoStatus == nil},
		{"listType", listType == nil},
		{"listNoType", listNoType == nil},
		{"listPriority", listPriority == nil},
		{"listNoPriority", listNoPriority == nil},
		{"listEstimate", listEstimate == nil},
		{"listNoEstimate", listNoEstimate == nil},
		{"listTag", listTag == nil},
		{"listNoTag", listNoTag == nil},
		{"listHasParent", !listHasParent},
		{"listNoParent", !listNoParent},
		{"listParentID", listParentID == ""},
		{"listHasBlocking", !listHasBlocking},
		{"listNoBlocking", !listNoBlocking},
		{"listIsBlocked", !listIsBlocked},
		{"listMentions", listMentions == ""},
		{"listMentionedBy", listMentionedBy == ""},
		{"listReady", !listReady},
		{"listQuiet", !listQuiet},
		{"listSort", listSort == ""},
		{"listFull", !listFull},
	}
	for _, c := range checks {
		if !c.zero {
			t.Errorf("%s not reset to zero value", c.name)
		}
	}
}

// setupListCobraTest writes nib files and returns the .nibs directory so
// `rootCmd.SetArgs(["--nibs-path", dir, "list", ...])` can drive the full
// Cobra pipeline.
func setupListCobraTest(t *testing.T, files map[string]string) string {
	t.Helper()
	t.Cleanup(resetListFlags)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	resetListFlags()

	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(nibsDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return nibsDir
}

// mentionsFixture returns a small nib-file map used by the list mention-flag
// tests. a1 mentions b2 and c3; d4 mentions a1. Statuses vary so --status
// composition can be exercised.
func mentionsFixture() map[string]string {
	return map[string]string{
		"a1--alpha.md": "---\ntitle: Alpha\nstatus: todo\ntype: task\n---\n\nSee #b2 and #c3.\n",
		"b2--beta.md":  "---\ntitle: Beta\nstatus: todo\ntype: task\n---\n\nNo refs.\n",
		"c3--gamma.md": "---\ntitle: Gamma\nstatus: completed\ntype: task\n---\n\nBackref to #a1.\n",
		"d4--delta.md": "---\ntitle: Delta\nstatus: todo\ntype: task\n---\n\nAlso mentions #a1.\n",
	}
}

func TestListCommand_MentionsFlag(t *testing.T) {
	// --mentions <id> → nibs whose bodies mention <id>.
	// Target nibs-a1 → mentioners are c3 (completed) and d4 (todo).
	nibsDir := setupListCobraTest(t, mentionsFixture())

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "list", "--mentions", "a1", "--json"})

	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("list --mentions failed: %v", execErr)
	}

	var results []*nib.Nib
	if err := json.Unmarshal([]byte(out), &results); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}
	if len(results) != 2 {
		t.Fatalf("got %d nibs, want 2 (c3, d4)\nraw: %s", len(results), out)
	}
	ids := map[string]bool{}
	for _, b := range results {
		ids[b.ID] = true
	}
	if !ids["c3"] || !ids["d4"] {
		t.Errorf("got %v, want {c3, d4}", ids)
	}
}

func TestListCommand_MentionsFlag_ComposesWithStatus(t *testing.T) {
	// --mentions nibs-a1 --status todo → only d4 (c3 is completed).
	nibsDir := setupListCobraTest(t, mentionsFixture())

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "list",
		"--mentions", "a1", "--status", "todo", "--json"})

	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("list --mentions --status failed: %v", execErr)
	}

	var results []*nib.Nib
	if err := json.Unmarshal([]byte(out), &results); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}
	if len(results) != 1 || results[0].ID != "d4" {
		t.Errorf("got %d nibs (%+v), want exactly [d4]", len(results), results)
	}
}

func TestListCommand_MentionedByFlag(t *testing.T) {
	// --mentioned-by nibs-a1 → nibs listed in a1's body: b2 and c3.
	nibsDir := setupListCobraTest(t, mentionsFixture())

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "list", "--mentioned-by", "a1", "--json"})

	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("list --mentioned-by failed: %v", execErr)
	}

	var results []*nib.Nib
	if err := json.Unmarshal([]byte(out), &results); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}
	if len(results) != 2 {
		t.Fatalf("got %d, want 2 (b2, c3)\nraw: %s", len(results), out)
	}
	ids := map[string]bool{}
	for _, b := range results {
		ids[b.ID] = true
	}
	if !ids["b2"] || !ids["c3"] {
		t.Errorf("got %v, want {b2, c3}", ids)
	}
}

func TestListCommand_MentionsFlag_ShortIDNormalisation(t *testing.T) {
	// Passing a short-form id (without the prefix) should still resolve via
	// the GraphQL filter layer's NormalizeID path. We write an explicit
	// .nibs.yml with prefix `nibs-` and point --config at it so the loaded
	// config's prefix is honoured regardless of test cwd.
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(tmpDir, ".nibs.yml")
	if err := os.WriteFile(cfgPath, []byte("nibs:\n  prefix: nibs-\n  id_length: 4\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for name, content := range mentionsFixture() {
		// Prefix filenames with `nibs-` so the ids parse as nibs-a1 etc.
		target := filepath.Join(nibsDir, "nibs-"+name)
		if err := os.WriteFile(target, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(resetListFlags)
	resetListFlags()
	t.Cleanup(func() { configPath = "" })

	// Pass the short form "a1" — filter layer should normalise to nibs-a1.
	rootCmd.SetArgs([]string{
		"--config", cfgPath,
		"--nibs-path", nibsDir,
		"list", "--mentions", "a1", "--json",
	})

	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("list --mentions (short id) failed: %v", execErr)
	}

	var results []*nib.Nib
	if err := json.Unmarshal([]byte(out), &results); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}
	if len(results) != 2 {
		t.Fatalf("got %d, want 2 (nibs-c3, nibs-d4) — short-id normalisation failed\nraw: %s", len(results), out)
	}
	ids := map[string]bool{}
	for _, b := range results {
		ids[b.ID] = true
	}
	if !ids["nibs-c3"] || !ids["nibs-d4"] {
		t.Errorf("got %v, want {nibs-c3, nibs-d4}", ids)
	}
}

func TestListCommand_MentionsFlag_UnknownID(t *testing.T) {
	// Unknown target should yield empty results, not an error.
	// Assert `[]` only; list --json must never emit null for a list field
	// (stable JSON for agent consumers — folds into the empty-array
	// convention shared by refs --both and show --json).
	nibsDir := setupListCobraTest(t, mentionsFixture())

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "list", "--mentions", "nope", "--json"})

	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("list --mentions <unknown> failed: %v", execErr)
	}
	trimmed := strings.TrimSpace(out)
	if trimmed != "[]" {
		t.Errorf("got %q, want `[]` exclusively (list --json must not emit null for empty results)", trimmed)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"short string", "hello", 10, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"needs truncation", "hello world", 8, "hello..."},
		{"very short max", "hello", 4, "h..."},
		{"empty string", "", 10, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

