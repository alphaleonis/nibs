package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
)

// setupRefsTestApp builds an App with the given prefix so mention tests can
// exercise both short- and full-form ID resolution.
func setupRefsTestApp(t *testing.T, prefix string) *App {
	t.Helper()
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatalf("failed to create test .nibs dir: %v", err)
	}
	cfg := config.DefaultWithPrefix(prefix)
	core := nibcore.New(nibsDir, cfg)
	if err := core.Load(); err != nil {
		t.Fatalf("failed to load core: %v", err)
	}
	return &App{Core: core}
}

// resetRefsFlags clears the package-level flag vars used by refsCmd so
// tests don't pollute each other via rootCmd's singleton state.
func resetRefsFlags() {
	refsInbound = false
	refsJSON = false
}

// stdoutMu serializes global os.Stdout mutations across tests that need to
// observe writes from the output package (which bypasses Cobra's writers).
// The mutex neutralises the race if anyone later adds t.Parallel() to a
// test in this package — stdout itself is process-global, so concurrent
// swaps would silently steal each other's output without this guard.
var stdoutMu sync.Mutex

// captureStdout captures writes made directly to os.Stdout while fn runs
// (used for output.Success*/Error which bypass Cobra's configured writers).
// NOT safe under t.Parallel() — the package-level mutex guards against
// concurrent swaps within the same process, but stdout itself is still
// global.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	stdoutMu.Lock()
	defer stdoutMu.Unlock()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()

	_ = w.Close()
	os.Stdout = orig
	return <-done
}

func TestMentionsGraphQLEndToEnd(t *testing.T) {
	app := setupRefsTestApp(t, "nibs-")

	// nibs-aaa1 mentions #bbb2 (short) and #nibs-ccc3 (full).
	// nibs-ddd4 mentions #aaa1 in its body.
	nibs := []*nib.Nib{
		{ID: "nibs-aaa1", Slug: "a", Title: "A", Status: "todo", Body: "See #bbb2 and #nibs-ccc3 for details."},
		{ID: "nibs-bbb2", Slug: "b", Title: "B", Status: "todo", Body: "No refs here."},
		{ID: "nibs-ccc3", Slug: "c", Title: "C", Status: "completed", Body: "Backref to #aaa1."},
		{ID: "nibs-ddd4", Slug: "d", Title: "D", Status: "todo", Body: "Also mentions #aaa1."},
	}
	for _, b := range nibs {
		if err := app.Core.Create(b); err != nil {
			t.Fatalf("create %s: %v", b.ID, err)
		}
	}

	t.Run("outbound mentions", func(t *testing.T) {
		query := `{ nib(id: "nibs-aaa1") { mentions { id } mentionIds } }`
		result, err := executeQuery(app, query, nil, "")
		if err != nil {
			t.Fatalf("executeQuery: %v", err)
		}
		var data struct {
			Nib struct {
				Mentions   []struct{ ID string } `json:"mentions"`
				MentionIds []string              `json:"mentionIds"`
			} `json:"nib"`
		}
		if err := json.Unmarshal(result, &data); err != nil {
			t.Fatalf("unmarshal: %v\nraw: %s", err, result)
		}
		if len(data.Nib.Mentions) != 2 {
			t.Fatalf("got %d mentions, want 2 (%s)", len(data.Nib.Mentions), result)
		}
		got := []string{data.Nib.Mentions[0].ID, data.Nib.Mentions[1].ID}
		if got[0] != "nibs-bbb2" || got[1] != "nibs-ccc3" {
			t.Errorf("mentions order = %v, want [nibs-bbb2 nibs-ccc3]", got)
		}
		wantIds := []string{"nibs-bbb2", "nibs-ccc3"}
		if len(data.Nib.MentionIds) != len(wantIds) {
			t.Fatalf("mentionIds len = %d, want %d", len(data.Nib.MentionIds), len(wantIds))
		}
		for i, want := range wantIds {
			if data.Nib.MentionIds[i] != want {
				t.Errorf("mentionIds[%d] = %s, want %s", i, data.Nib.MentionIds[i], want)
			}
		}
	})

	t.Run("inbound mentions", func(t *testing.T) {
		query := `{ nib(id: "nibs-aaa1") { mentionedBy { id } } }`
		result, err := executeQuery(app, query, nil, "")
		if err != nil {
			t.Fatalf("executeQuery: %v", err)
		}
		var data struct {
			Nib struct {
				MentionedBy []struct{ ID string } `json:"mentionedBy"`
			} `json:"nib"`
		}
		if err := json.Unmarshal(result, &data); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(data.Nib.MentionedBy) != 2 {
			t.Fatalf("got %d inbound mentions, want 2 (%s)", len(data.Nib.MentionedBy), result)
		}
		ids := map[string]bool{}
		for _, m := range data.Nib.MentionedBy {
			ids[m.ID] = true
		}
		if !ids["nibs-ccc3"] || !ids["nibs-ddd4"] {
			t.Errorf("got %v, want nibs-ccc3 and nibs-ddd4 in mentionedBy", ids)
		}
	})

	t.Run("filter mentions by excluding completed", func(t *testing.T) {
		query := `{ nib(id: "nibs-aaa1") { mentions(filter: { excludeStatus: ["completed"] }) { id } } }`
		result, err := executeQuery(app, query, nil, "")
		if err != nil {
			t.Fatalf("executeQuery: %v", err)
		}
		var data struct {
			Nib struct {
				Mentions []struct{ ID string } `json:"mentions"`
			} `json:"nib"`
		}
		if err := json.Unmarshal(result, &data); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(data.Nib.Mentions) != 1 || data.Nib.Mentions[0].ID != "nibs-bbb2" {
			t.Errorf("got %+v, want exactly [nibs-bbb2]", data.Nib.Mentions)
		}
	})

	t.Run("filter nibs by mentionsId", func(t *testing.T) {
		// Nibs whose bodies mention nibs-aaa1 → nibs-ccc3, nibs-ddd4.
		query := `{ nibs(filter: { mentionsId: "nibs-aaa1" }) { id } }`
		result, err := executeQuery(app, query, nil, "")
		if err != nil {
			t.Fatalf("executeQuery: %v", err)
		}
		var data struct {
			Nibs []struct{ ID string } `json:"nibs"`
		}
		if err := json.Unmarshal(result, &data); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(data.Nibs) != 2 {
			t.Fatalf("got %d, want 2 (%s)", len(data.Nibs), result)
		}
		ids := map[string]bool{data.Nibs[0].ID: true, data.Nibs[1].ID: true}
		if !ids["nibs-ccc3"] || !ids["nibs-ddd4"] {
			t.Errorf("got %v, want {nibs-ccc3, nibs-ddd4}", ids)
		}
	})

	t.Run("filter nibs by mentionedById", func(t *testing.T) {
		// Nibs mentioned in nibs-aaa1's body → nibs-bbb2, nibs-ccc3.
		query := `{ nibs(filter: { mentionedById: "nibs-aaa1" }) { id } }`
		result, err := executeQuery(app, query, nil, "")
		if err != nil {
			t.Fatalf("executeQuery: %v", err)
		}
		var data struct {
			Nibs []struct{ ID string } `json:"nibs"`
		}
		if err := json.Unmarshal(result, &data); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(data.Nibs) != 2 {
			t.Fatalf("got %d, want 2 (%s)", len(data.Nibs), result)
		}
		ids := map[string]bool{data.Nibs[0].ID: true, data.Nibs[1].ID: true}
		if !ids["nibs-bbb2"] || !ids["nibs-ccc3"] {
			t.Errorf("got %v, want {nibs-bbb2, nibs-ccc3}", ids)
		}
	})

	// Finding #24 — unresolvable target branch of the two filter helpers.
	t.Run("filter mentionsId with unknown target returns empty", func(t *testing.T) {
		query := `{ nibs(filter: { mentionsId: "nibs-nope" }) { id } }`
		result, err := executeQuery(app, query, nil, "")
		if err != nil {
			t.Fatalf("executeQuery: %v", err)
		}
		var data struct {
			Nibs []struct{ ID string } `json:"nibs"`
		}
		if err := json.Unmarshal(result, &data); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(data.Nibs) != 0 {
			t.Errorf("got %d results, want 0", len(data.Nibs))
		}
	})

	t.Run("filter mentionedById with unknown target returns empty", func(t *testing.T) {
		query := `{ nibs(filter: { mentionedById: "nibs-nope" }) { id } }`
		result, err := executeQuery(app, query, nil, "")
		if err != nil {
			t.Fatalf("executeQuery: %v", err)
		}
		var data struct {
			Nibs []struct{ ID string } `json:"nibs"`
		}
		if err := json.Unmarshal(result, &data); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(data.Nibs) != 0 {
			t.Errorf("got %d results, want 0", len(data.Nibs))
		}
	})
}

func TestRefsCommandFindsMentions(t *testing.T) {
	app := setupRefsTestApp(t, "nibs-")

	nibs := []*nib.Nib{
		{ID: "nibs-a1", Title: "A", Status: "todo", Body: "Refs #b2."},
		{ID: "nibs-b2", Title: "B", Status: "todo", Body: ""},
	}
	for _, b := range nibs {
		if err := app.Core.Create(b); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	t.Run("outbound via Core.FindMentions", func(t *testing.T) {
		got := app.Core.FindMentions("nibs-a1")
		if len(got) != 1 || got[0].ID != "nibs-b2" {
			t.Errorf("got %v, want [nibs-b2]", got)
		}
	})

	t.Run("inbound via Core.FindMentionedBy", func(t *testing.T) {
		got := app.Core.FindMentionedBy("nibs-b2")
		if len(got) != 1 || got[0].ID != "nibs-a1" {
			t.Errorf("got %v, want [nibs-a1]", got)
		}
	})
}

// setupRefsCobraTest writes actual nib files to disk and returns the
// .nibs directory path so `rootCmd.SetArgs(["--nibs-path", dir, ...])`
// can drive the full Cobra + PersistentPreRunE pipeline.
func setupRefsCobraTest(t *testing.T, files map[string]string) string {
	t.Helper()
	t.Cleanup(resetRefsFlags)
	resetRefsFlags()

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

func TestRefsCommand_OutboundHumanOutput(t *testing.T) {
	nibsDir := setupRefsCobraTest(t, map[string]string{
		"a1--alpha.md": "---\ntitle: Alpha\nstatus: todo\ntype: task\n---\n\nRefs #b2 here.\n",
		"b2--beta.md":  "---\ntitle: Beta\nstatus: in-progress\ntype: task\n---\n\nNo refs.\n",
	})

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "refs", "a1"})
	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stdout)
	defer rootCmd.SetOut(nil)
	defer rootCmd.SetErr(nil)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("refs failed: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "b2") {
		t.Errorf("expected outbound mention b2 in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Beta") {
		t.Errorf("expected mention target title in output, got:\n%s", out)
	}
}

func TestRefsCommand_InboundHumanOutput(t *testing.T) {
	nibsDir := setupRefsCobraTest(t, map[string]string{
		"a1--alpha.md": "---\ntitle: Alpha\nstatus: todo\ntype: task\n---\n\nRefs #b2 here.\n",
		"b2--beta.md":  "---\ntitle: Beta\nstatus: in-progress\ntype: task\n---\n\nNo refs.\n",
	})

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "refs", "b2", "--inbound"})
	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stdout)
	defer rootCmd.SetOut(nil)
	defer rootCmd.SetErr(nil)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("refs --inbound failed: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "a1") {
		t.Errorf("expected inbound mentioner a1 in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Alpha") {
		t.Errorf("expected mentioner title in output, got:\n%s", out)
	}
}

func TestRefsCommand_EmptyResultsMutedOutput(t *testing.T) {
	nibsDir := setupRefsCobraTest(t, map[string]string{
		"lone--solo.md": "---\ntitle: Solo\nstatus: todo\ntype: task\n---\n\nJust a body with no refs.\n",
	})

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "refs", "lone"})
	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stdout)
	defer rootCmd.SetOut(nil)
	defer rootCmd.SetErr(nil)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("refs failed: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "No outbound mentions") {
		t.Errorf("expected 'No outbound mentions' message, got:\n%s", out)
	}
	if !strings.Contains(out, "lone") {
		t.Errorf("expected the nib id in the empty-result message, got:\n%s", out)
	}
}

func TestRefsCommand_JSONOutput(t *testing.T) {
	nibsDir := setupRefsCobraTest(t, map[string]string{
		"a1--alpha.md": "---\ntitle: Alpha\nstatus: todo\ntype: task\n---\n\nRefs #b2 here.\n",
		"b2--beta.md":  "---\ntitle: Beta\nstatus: in-progress\ntype: task\n---\n\nNo refs.\n",
	})

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "refs", "a1", "--json"})

	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("refs --json failed: %v", execErr)
	}

	var nibs []*nib.Nib
	if err := json.Unmarshal([]byte(out), &nibs); err != nil {
		t.Fatalf("unmarshal JSON: %v\nraw: %s", err, out)
	}
	if len(nibs) != 1 {
		t.Fatalf("got %d nibs, want 1\nraw: %s", len(nibs), out)
	}
	if nibs[0].ID != "b2" {
		t.Errorf("nibs[0].ID = %s, want b2", nibs[0].ID)
	}
	if nibs[0].Title != "Beta" {
		t.Errorf("nibs[0].Title = %s, want Beta", nibs[0].Title)
	}
}

func TestRefsCommand_UnknownIDJSONError(t *testing.T) {
	nibsDir := setupRefsCobraTest(t, map[string]string{
		"a1--alpha.md": "---\ntitle: Alpha\nstatus: todo\ntype: task\n---\n\nRefs #b2 here.\n",
	})

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "refs", "does-not-exist", "--json"})

	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr == nil {
		t.Fatal("expected error for unknown nib id, got nil")
	}

	// JSON branch writes an envelope via output.Error before returning the
	// error. Verify the envelope has the NOT_FOUND code.
	var envelope struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
		Code    string `json:"code"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("unmarshal JSON envelope: %v\nraw: %s", err, out)
	}
	if envelope.Success {
		t.Errorf("envelope.Success = true, want false")
	}
	if envelope.Code != "NOT_FOUND" {
		t.Errorf("envelope.Code = %q, want NOT_FOUND\nraw: %s", envelope.Code, out)
	}
}

func TestRefsCommand_UnknownIDHumanError(t *testing.T) {
	nibsDir := setupRefsCobraTest(t, map[string]string{
		"a1--alpha.md": "---\ntitle: Alpha\nstatus: todo\ntype: task\n---\n\nRefs #b2 here.\n",
	})

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "refs", "does-not-exist"})
	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stdout)
	defer rootCmd.SetOut(nil)
	defer rootCmd.SetErr(nil)

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for unknown nib id, got nil")
	}
	if !strings.Contains(err.Error(), "does-not-exist") && !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected error to mention the missing id or 'not found', got: %v", err)
	}
}
