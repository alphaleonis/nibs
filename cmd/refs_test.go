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
	"time"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/spf13/pflag"
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
// tests don't pollute each other via rootCmd's singleton state. Also
// clears Cobra's "Changed" tracking so MarkFlagsMutuallyExclusive (and
// similar per-invocation checks) don't see stale state from a prior
// Execute.
func resetRefsFlags() {
	refsInbound = false
	refsBoth = false
	refsJSON = false
	refsStatus = nil
	refsNoStatus = nil
	refsType = nil
	refsNoType = nil
	refsPriority = nil
	refsActive = false
	refsCmd.Flags().Visit(func(f *pflag.Flag) {
		f.Changed = false
	})
}

// TestResetRefsFlagsClearsAllState walks refsCmd's Cobra FlagSet and verifies
// that every registered flag is at its documented default after resetRefsFlags
// runs. Unlike a hand-enumerated check, this catches additive drift: if a new
// flag is registered on refsCmd but resetRefsFlags doesn't clear its backing
// var, the post-reset value will diverge from DefValue and this test fires.
// Also verifies Cobra's Changed state is cleared so MarkFlagsMutuallyExclusive
// (and similar per-invocation checks) don't see stale state from a prior
// Execute.
func TestResetRefsFlagsClearsAllState(t *testing.T) {
	t.Cleanup(resetRefsFlags)

	// Dirty every flag via the real FlagSet so Cobra's `actual` map is
	// populated (Set() is the only public way to do this) — this
	// exercises Changed-state clearing as well as value clearing.
	dirty := map[string]string{
		"inbound":   "true",
		"both":      "true",
		"json":      "true",
		"status":    "todo",
		"no-status": "draft",
		"type":      "task",
		"no-type":   "bug",
		"priority":  "high",
		"active":    "true",
	}
	for name, val := range dirty {
		if err := refsCmd.Flags().Set(name, val); err != nil {
			t.Fatalf("pre-populate --%s: %v", name, err)
		}
	}

	resetRefsFlags()

	// Walk the FlagSet and assert every flag is back to its registered
	// default. DefValue is a stringified form of the default registered
	// at pflag.*Var time; Value.String() is the same form of the current
	// value, so comparing them works uniformly across bool/string/
	// stringArray without per-type branching.
	refsCmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Value.String() != f.DefValue {
			t.Errorf("flag %q = %q after reset, want default %q",
				f.Name, f.Value.String(), f.DefValue)
		}
		if f.Changed {
			t.Errorf("flag %q Changed = true after reset, want false", f.Name)
		}
	})
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
//
// The os.Stdout restore and writer close are deferred so a panic inside
// fn() cannot leave stdout redirected for subsequent tests. The final
// read is wrapped in a timeout so a hung pipe fails fast per-test rather
// than ticking over to the suite-level timeout with no diagnostic.
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
	defer func() { os.Stdout = orig }()

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	// Double-close guard: the deferred close runs after a panic inside
	// fn() (where the explicit close below never reached). On the normal
	// path the flag is flipped and the defer becomes a no-op. Without
	// the guard a stray second Close() would hit an already-closed fd
	// and, under concurrent OS fd reuse, could race with whichever file
	// has inherited the fd slot.
	closed := false
	defer func() {
		if !closed {
			_ = w.Close()
		}
	}()
	fn()
	_ = w.Close()
	closed = true

	select {
	case s := <-done:
		return s
	case <-time.After(5 * time.Second):
		t.Fatal("captureStdout timed out waiting for goroutine (pipe deadlocked?)")
		return ""
	}
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
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
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

// refsFilterFixture returns a nib-file map used by the refs filter-flag tests.
//   - a1 mentions b2 (todo/task), c3 (completed/task), d4 (todo/bug).
//   - e5 mentions a1 (inbound).
//   - f6 mentions a1 (inbound, scrapped).
//
// The mix of statuses and types lets tests exercise each filter flag and
// compose them with --inbound and --both.
func refsFilterFixture() map[string]string {
	return map[string]string{
		"a1--alpha.md":   "---\ntitle: Alpha\nstatus: todo\ntype: task\n---\n\nSee #b2 and #c3 and #d4.\n",
		"b2--beta.md":    "---\ntitle: Beta\nstatus: todo\ntype: task\npriority: high\n---\n\nNo refs.\n",
		"c3--gamma.md":   "---\ntitle: Gamma\nstatus: completed\ntype: task\npriority: low\n---\n\nNo refs.\n",
		"d4--delta.md":   "---\ntitle: Delta\nstatus: todo\ntype: bug\npriority: high\n---\n\nNo refs.\n",
		"e5--epsilon.md": "---\ntitle: Epsilon\nstatus: todo\ntype: task\n---\n\nRefs #a1.\n",
		"f6--zeta.md":    "---\ntitle: Zeta\nstatus: scrapped\ntype: task\n---\n\nRefs #a1.\n",
	}
}

func TestRefsCommand_StatusFilter(t *testing.T) {
	nibsDir := setupRefsCobraTest(t, refsFilterFixture())

	// a1's outbound mentions with --status todo → b2 and d4 only.
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "refs", "a1", "--status", "todo", "--json"})
	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("refs --status failed: %v", execErr)
	}
	var nibs []*nib.Nib
	if err := json.Unmarshal([]byte(out), &nibs); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}
	if len(nibs) != 2 {
		t.Fatalf("got %d, want 2 (b2, d4)\nraw: %s", len(nibs), out)
	}
	ids := map[string]bool{nibs[0].ID: true, nibs[1].ID: true}
	if !ids["b2"] || !ids["d4"] {
		t.Errorf("got %v, want {b2, d4}", ids)
	}
}

func TestRefsCommand_NoStatusFilter(t *testing.T) {
	nibsDir := setupRefsCobraTest(t, refsFilterFixture())

	// a1's outbound with --no-status completed → excludes c3, leaves b2, d4.
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "refs", "a1", "--no-status", "completed", "--json"})
	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("refs --no-status failed: %v", execErr)
	}
	var nibs []*nib.Nib
	if err := json.Unmarshal([]byte(out), &nibs); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}
	if len(nibs) != 2 {
		t.Fatalf("got %d, want 2 (b2, d4 after --no-status completed)\nraw: %s", len(nibs), out)
	}
	ids := map[string]bool{}
	for _, n := range nibs {
		ids[n.ID] = true
	}
	if ids["c3"] {
		t.Errorf("completed c3 should have been excluded; got %v", ids)
	}
	if !ids["b2"] || !ids["d4"] {
		t.Errorf("got %v, want b2 and d4 present", ids)
	}
}

func TestRefsCommand_TypeFilter(t *testing.T) {
	nibsDir := setupRefsCobraTest(t, refsFilterFixture())

	// a1's outbound with --type bug → d4 only.
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "refs", "a1", "--type", "bug", "--json"})
	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("refs --type failed: %v", execErr)
	}
	var nibs []*nib.Nib
	if err := json.Unmarshal([]byte(out), &nibs); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}
	if len(nibs) != 1 || nibs[0].ID != "d4" {
		t.Errorf("got %+v, want exactly [d4]", nibs)
	}
}

func TestRefsCommand_NoTypeFilter(t *testing.T) {
	nibsDir := setupRefsCobraTest(t, refsFilterFixture())

	// --no-type bug → excludes d4, leaves b2, c3.
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "refs", "a1", "--no-type", "bug", "--json"})
	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("refs --no-type failed: %v", execErr)
	}
	var nibs []*nib.Nib
	if err := json.Unmarshal([]byte(out), &nibs); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}
	if len(nibs) != 2 {
		t.Fatalf("got %d, want 2 (b2, c3 after --no-type bug)\nraw: %s", len(nibs), out)
	}
	ids := map[string]bool{}
	for _, n := range nibs {
		ids[n.ID] = true
	}
	if ids["d4"] {
		t.Errorf("d4 (bug) should have been excluded; got %v", ids)
	}
	if !ids["b2"] || !ids["c3"] {
		t.Errorf("got %v, want b2 and c3 present", ids)
	}
}

func TestRefsCommand_PriorityFilter(t *testing.T) {
	nibsDir := setupRefsCobraTest(t, refsFilterFixture())

	// --priority high → b2 and d4.
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "refs", "a1", "--priority", "high", "--json"})
	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("refs --priority failed: %v", execErr)
	}
	var nibs []*nib.Nib
	if err := json.Unmarshal([]byte(out), &nibs); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}
	if len(nibs) != 2 {
		t.Fatalf("got %d, want 2 (b2, d4)\nraw: %s", len(nibs), out)
	}
	ids := map[string]bool{nibs[0].ID: true, nibs[1].ID: true}
	if !ids["b2"] || !ids["d4"] {
		t.Errorf("got %v, want {b2, d4}", ids)
	}
}

func TestRefsCommand_ActiveFlag(t *testing.T) {
	nibsDir := setupRefsCobraTest(t, refsFilterFixture())

	// --active → excludes completed (c3) and scrapped. Outbound = b2, d4.
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "refs", "a1", "--active", "--json"})
	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("refs --active failed: %v", execErr)
	}
	var nibs []*nib.Nib
	if err := json.Unmarshal([]byte(out), &nibs); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}
	ids := map[string]bool{}
	for _, n := range nibs {
		ids[n.ID] = true
	}
	if ids["c3"] {
		t.Errorf("c3 (completed) should have been excluded by --active; got %v", ids)
	}
	if !ids["b2"] || !ids["d4"] {
		t.Errorf("got %v, want b2 and d4 present", ids)
	}
}

func TestRefsCommand_ActiveFlag_Inbound(t *testing.T) {
	nibsDir := setupRefsCobraTest(t, refsFilterFixture())

	// --inbound --active on a1 → mentioners excluding scrapped = e5 only.
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "refs", "a1", "--inbound", "--active", "--json"})
	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("refs --inbound --active failed: %v", execErr)
	}
	var nibs []*nib.Nib
	if err := json.Unmarshal([]byte(out), &nibs); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}
	if len(nibs) != 1 || nibs[0].ID != "e5" {
		t.Errorf("got %+v, want exactly [e5]", nibs)
	}
}

func TestRefsCommand_BothMode_HumanOutput(t *testing.T) {
	nibsDir := setupRefsCobraTest(t, refsFilterFixture())

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "refs", "a1", "--both"})
	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stdout)
	defer rootCmd.SetOut(nil)
	defer rootCmd.SetErr(nil)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("refs --both failed: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "Outbound") {
		t.Errorf("expected 'Outbound' section label, got:\n%s", out)
	}
	if !strings.Contains(out, "Inbound") {
		t.Errorf("expected 'Inbound' section label, got:\n%s", out)
	}
	// Slice the output at section labels and verify IDs appear in the
	// correct section — guards against a regression that swaps the two
	// lists.
	iOut := strings.Index(out, "Outbound")
	iIn := strings.Index(out, "Inbound")
	if iOut < 0 || iIn < 0 || iOut > iIn {
		t.Fatalf("expected Outbound before Inbound; got:\n%s", out)
	}
	outSection := out[iOut:iIn]
	inSection := out[iIn:]
	for _, id := range []string{"b2", "c3", "d4"} {
		if !strings.Contains(outSection, id) {
			t.Errorf("outbound section missing %q; got:\n%s", id, outSection)
		}
	}
	for _, id := range []string{"e5", "f6"} {
		if !strings.Contains(inSection, id) {
			t.Errorf("inbound section missing %q; got:\n%s", id, inSection)
		}
	}
}

func TestRefsCommand_BothMode_JSONShape(t *testing.T) {
	nibsDir := setupRefsCobraTest(t, refsFilterFixture())

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "refs", "a1", "--both", "--json"})
	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("refs --both --json failed: %v", execErr)
	}

	// The envelope is a bare `{outbound, inbound}` payload — no `success`
	// wrapper. Matches `plan --json` convention; the mutation-style
	// `{success, ...}` envelope is reserved for mutations.
	if strings.Contains(out, `"success"`) {
		t.Errorf("expected NO 'success' key in refs --both --json output, got:\n%s", out)
	}
	var envelope struct {
		Outbound []*nib.Nib `json:"outbound"`
		Inbound  []*nib.Nib `json:"inbound"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}
	if len(envelope.Outbound) != 3 {
		t.Errorf("outbound len = %d, want 3 (b2, c3, d4)", len(envelope.Outbound))
	}
	if len(envelope.Inbound) != 2 {
		t.Errorf("inbound len = %d, want 2 (e5, f6)", len(envelope.Inbound))
	}
}

func TestRefsCommand_BothMode_EmptySections(t *testing.T) {
	// A nib with neither outbound nor inbound mentions — --both human output
	// should still show both labels with the muted "No ... mentions." line.
	nibsDir := setupRefsCobraTest(t, map[string]string{
		"solo--solo.md": "---\ntitle: Solo\nstatus: todo\ntype: task\n---\n\nNo refs.\n",
	})
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "refs", "solo", "--both"})
	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stdout)
	defer rootCmd.SetOut(nil)
	defer rootCmd.SetErr(nil)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("refs --both (empty) failed: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "Outbound") || !strings.Contains(out, "Inbound") {
		t.Errorf("expected both section labels, got:\n%s", out)
	}
	// Assert Outbound appears before Inbound so a section swap would be
	// caught rather than silently passing.
	iOut := strings.Index(out, "Outbound")
	iIn := strings.Index(out, "Inbound")
	if iOut < 0 || iIn < 0 || iOut > iIn {
		t.Fatalf("expected Outbound before Inbound; got:\n%s", out)
	}
	if !strings.Contains(out, "No outbound mentions") {
		t.Errorf("expected 'No outbound mentions', got:\n%s", out)
	}
	if !strings.Contains(out, "No inbound mentions") {
		t.Errorf("expected 'No inbound mentions', got:\n%s", out)
	}
}

func TestRefsCommand_BothMode_EmptySections_JSONAlwaysArrays(t *testing.T) {
	// --both --json for a nib with no mentions must emit `"outbound":[]`
	// and `"inbound":[]` (never null, never absent) so agent consumers can
	// rely on a stable shape.
	nibsDir := setupRefsCobraTest(t, map[string]string{
		"solo--solo.md": "---\ntitle: Solo\nstatus: todo\ntype: task\n---\n\nNo refs.\n",
	})
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "refs", "solo", "--both", "--json"})
	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("refs --both --json (empty) failed: %v", execErr)
	}
	if !strings.Contains(out, `"outbound": []`) {
		t.Errorf("expected `\"outbound\": []` in empty --both --json output, got:\n%s", out)
	}
	if !strings.Contains(out, `"inbound": []`) {
		t.Errorf("expected `\"inbound\": []` in empty --both --json output, got:\n%s", out)
	}
	if strings.Contains(out, `"success"`) {
		t.Errorf("expected NO 'success' key in bare envelope, got:\n%s", out)
	}
}

func TestRefsCommand_BothMode_WithFilterFlags(t *testing.T) {
	// Filter flags must compose with --both and apply to both directions.
	// --both --active on a1:
	//   outbound = b2, d4 (c3 is completed → dropped).
	//   inbound  = e5     (f6 is scrapped  → dropped).
	nibsDir := setupRefsCobraTest(t, refsFilterFixture())
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "refs", "a1", "--both", "--active", "--json"})
	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("refs --both --active failed: %v", execErr)
	}
	var envelope struct {
		Outbound []*nib.Nib `json:"outbound"`
		Inbound  []*nib.Nib `json:"inbound"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}
	if len(envelope.Outbound) != 2 {
		t.Errorf("outbound len = %d, want 2 (b2, d4 after --active)", len(envelope.Outbound))
	}
	if len(envelope.Inbound) != 1 || envelope.Inbound[0].ID != "e5" {
		t.Errorf("inbound = %+v, want exactly [e5]", envelope.Inbound)
	}
}

func TestRefsCommand_BothAndInbound_Rejected(t *testing.T) {
	nibsDir := setupRefsCobraTest(t, refsFilterFixture())

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "refs", "a1", "--both", "--inbound"})
	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stdout)
	defer rootCmd.SetOut(nil)
	defer rootCmd.SetErr(nil)

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for --both --inbound combo, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("expected 'mutually exclusive' in error, got: %v", err)
	}
}

func TestRefsCommand_BothAndInbound_JSONError(t *testing.T) {
	nibsDir := setupRefsCobraTest(t, refsFilterFixture())
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "refs", "a1", "--both", "--inbound", "--json"})

	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr == nil {
		t.Fatal("expected error for --both --inbound, got nil")
	}
	var env struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v\nraw: %s", err, out)
	}
	if env.Success {
		t.Errorf("envelope.Success = true, want false")
	}
	if env.Code != "VALIDATION_ERROR" {
		t.Errorf("envelope.Code = %q, want VALIDATION_ERROR", env.Code)
	}
}

func TestRefsCommand_ActiveWithConflictingStatus_HumanError(t *testing.T) {
	// --active excludes completed/scrapped; asking for --status completed on
	// top always yields empty results — reject the combo to surface the
	// intent instead of silently returning no mentions.
	nibsDir := setupRefsCobraTest(t, refsFilterFixture())

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "refs", "a1", "--active", "--status", "completed"})
	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stdout)
	defer rootCmd.SetOut(nil)
	defer rootCmd.SetErr(nil)

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for --active --status completed, got nil")
	}
	if !strings.Contains(err.Error(), "--active") || !strings.Contains(err.Error(), "completed") {
		t.Errorf("expected error to mention --active and completed, got: %v", err)
	}
}

func TestRefsCommand_ActiveWithConflictingStatus_JSONError(t *testing.T) {
	nibsDir := setupRefsCobraTest(t, refsFilterFixture())
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "refs", "a1", "--active", "--status", "scrapped", "--json"})

	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr == nil {
		t.Fatal("expected error for --active --status scrapped, got nil")
	}
	var env struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v\nraw: %s", err, out)
	}
	if env.Success {
		t.Errorf("envelope.Success = true, want false")
	}
	if env.Code != "VALIDATION_ERROR" {
		t.Errorf("envelope.Code = %q, want VALIDATION_ERROR", env.Code)
	}
}

func TestRefsCommand_InboundWithStatusFilter(t *testing.T) {
	// Compose filter flag with --inbound (no --both).
	nibsDir := setupRefsCobraTest(t, refsFilterFixture())

	// a1's inbound = e5 (todo), f6 (scrapped). --status todo → e5 only.
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "refs", "a1", "--inbound", "--status", "todo", "--json"})
	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("refs --inbound --status failed: %v", execErr)
	}
	var nibs []*nib.Nib
	if err := json.Unmarshal([]byte(out), &nibs); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}
	if len(nibs) != 1 || nibs[0].ID != "e5" {
		t.Errorf("got %+v, want exactly [e5]", nibs)
	}
}
