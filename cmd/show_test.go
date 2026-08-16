package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

// resetGetFlags clears the package-level flag vars used by getCmd so tests
// don't pollute each other via rootCmd's singleton state, and clears Cobra's
// "Changed" tracking.
func resetGetFlags() {
	getJSON = false
	getView = ""
	getFields = ""
	getCmd.Flags().Visit(func(f *pflag.Flag) {
		f.Changed = false
	})
}

// TestResetGetFlagsClearsAllState walks getCmd's Cobra FlagSet and verifies
// every registered flag is at its documented default after resetGetFlags runs,
// catching additive drift if a new flag is registered but not reset here.
func TestResetGetFlagsClearsAllState(t *testing.T) {
	t.Cleanup(resetGetFlags)

	dirty := map[string]string{
		"json":   "true",
		"view":   "card",
		"fields": "id",
	}
	for name, val := range dirty {
		if err := getCmd.Flags().Set(name, val); err != nil {
			t.Fatalf("pre-populate --%s: %v", name, err)
		}
	}

	resetGetFlags()

	getCmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Value.String() != f.DefValue {
			t.Errorf("flag %q = %q after reset, want default %q",
				f.Name, f.Value.String(), f.DefValue)
		}
		if f.Changed {
			t.Errorf("flag %q Changed = true after reset, want false", f.Name)
		}
	})
}

// setupGetCobraTest writes nib files and returns the .nibs directory so
// `rootCmd.SetArgs(["--nibs-path", dir, "get", ...])` can drive the full Cobra
// pipeline.
func setupGetCobraTest(t *testing.T, files map[string]string) string {
	t.Helper()
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetGetFlags)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	resetGetFlags()

	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(storeDataDir(nibsDir), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(dataPath(nibsDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return nibsDir
}

// getFixture is the file set used by the get tests:
//   - task1: todo/high task, blocked by blk1 (todo) + blk2 (in-progress), body mentions #blk1.
//   - blk1, blk2: the two blockers.
//   - par1: in-progress epic parenting child1 (completed) + child2 (todo).
func getFixture() map[string]string {
	return map[string]string{
		"task1--task-one.md":   "---\nversion: 1\ntitle: Task One\nstatus: todo\ntype: task\npriority: high\nblocked_by:\n  - blk1\n  - blk2\n---\n\nDepends on #blk1.\n",
		"blk1--blocker-one.md": "---\nversion: 1\ntitle: Blocker One\nstatus: todo\ntype: task\n---\n\nFirst blocker.\n",
		"blk2--blocker-two.md": "---\nversion: 1\ntitle: Blocker Two\nstatus: in-progress\ntype: task\n---\n\nSecond blocker.\n",
		"par1--parent.md":      "---\nversion: 1\ntitle: Parent\nstatus: in-progress\ntype: epic\n---\n\nParent body.\n",
		"child1--child-one.md": "---\nversion: 1\ntitle: Child One\nstatus: completed\ntype: task\nparent: par1\n---\n\nChild one.\n",
		"child2--child-two.md": "---\nversion: 1\ntitle: Child Two\nstatus: todo\ntype: task\nparent: par1\n---\n\nChild two.\n",
	}
}

// runGetCmd drives `nibs get <args...>` through the full Cobra pipeline against
// nibsDir and returns captured stdout plus the Execute error.
func runGetCmd(t *testing.T, nibsDir string, args ...string) (string, error) {
	t.Helper()
	rootCmd.SetArgs(append([]string{"--nibs-path", nibsDir, "get"}, args...))
	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	return out, execErr
}

// parseJSONObject unmarshals s into a generic JSON object, failing the test on
// error with the raw output for diagnosis.
func parseJSONObject(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("parse JSON: %v\noutput:\n%s", err, s)
	}
	return m
}

func TestGet_DefaultPrintsDocument(t *testing.T) {
	dir := setupGetCobraTest(t, getFixture())
	out, err := runGetCmd(t, dir, "task1")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	// Front matter + body, the writeback format.
	for _, want := range []string{"title: Task One", "status: todo", "blocked_by:", "Depends on #blk1."} {
		if !strings.Contains(out, want) {
			t.Errorf("document output missing %q, got:\n%s", want, out)
		}
	}
}

func TestGet_TextProjectionSelectsExactFields(t *testing.T) {
	dir := setupGetCobraTest(t, getFixture())
	out, err := runGetCmd(t, dir, "task1", "-f", "status,priority")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if out != "status: todo\npriority: high\n" {
		t.Errorf("unexpected text projection:\n%q", out)
	}
	for _, absent := range []string{"body", "etag", "id:"} {
		if strings.Contains(out, absent) {
			t.Errorf("expected %q absent from status,priority projection, got:\n%s", absent, out)
		}
	}
}

func TestGet_ViewCardOmitsBodyAndEtag(t *testing.T) {
	dir := setupGetCobraTest(t, getFixture())
	out, err := runGetCmd(t, dir, "task1", "--view", "card")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if !strings.Contains(out, "id: task1") || !strings.Contains(out, "status: todo") {
		t.Errorf("card view missing expected fields, got:\n%s", out)
	}
	if strings.Contains(out, "body:") || strings.Contains(out, "etag:") {
		t.Errorf("card view must omit body/etag, got:\n%s", out)
	}
}

func TestGet_NestedBlockedByText(t *testing.T) {
	dir := setupGetCobraTest(t, getFixture())
	out, err := runGetCmd(t, dir, "task1", "-f", "id,blocked-by(id,status)")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	want := `blocked_by: [{"id":"blk1","status":"todo"},{"id":"blk2","status":"in-progress"}]`
	if !strings.Contains(out, want) {
		t.Errorf("nested blocked-by text missing %q, got:\n%s", want, out)
	}
}

func TestGet_JSONContractSingle(t *testing.T) {
	dir := setupGetCobraTest(t, getFixture())
	out, err := runGetCmd(t, dir, "task1", "--json")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	m := parseJSONObject(t, out)
	// Exactly one top-level key: nib. No success/data wrapper.
	if _, ok := m["success"]; ok {
		t.Error("--json must not carry a 'success' key")
	}
	if _, ok := m["data"]; ok {
		t.Error("--json must not carry a 'data' key")
	}
	nibObj, ok := m["nib"].(map[string]any)
	if !ok {
		t.Fatalf("top-level 'nib' object missing, got: %v", m)
	}
	if nibObj["id"] != "task1" {
		t.Errorf("nib.id = %v, want task1", nibObj["id"])
	}
	// Default --json view is card: no body/etag.
	if _, ok := nibObj["body"]; ok {
		t.Error("default --json (card) must omit body")
	}
	if _, ok := nibObj["etag"]; ok {
		t.Error("default --json (card) must omit etag")
	}
}

func TestGet_JSONBodyOptIn(t *testing.T) {
	dir := setupGetCobraTest(t, getFixture())
	out, err := runGetCmd(t, dir, "task1", "--json", "-f", "body")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	nibObj, ok := parseJSONObject(t, out)["nib"].(map[string]any)
	if !ok {
		t.Fatalf("top-level 'nib' object missing in:\n%s", out)
	}
	body, ok := nibObj["body"].(string)
	if !ok || !strings.Contains(body, "Depends on #blk1.") {
		t.Errorf("nib.body missing or wrong, got: %v", nibObj["body"])
	}
}

func TestGet_JSONNestedBlockedBy(t *testing.T) {
	dir := setupGetCobraTest(t, getFixture())
	out, err := runGetCmd(t, dir, "task1", "--json", "-f", "id,blocked-by(id,status)")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	nibObj := parseJSONObject(t, out)["nib"].(map[string]any)
	arr, ok := nibObj["blocked_by"].([]any)
	if !ok || len(arr) != 2 {
		t.Fatalf("nib.blocked_by should be a 2-element array, got: %v", nibObj["blocked_by"])
	}
	first := arr[0].(map[string]any)
	if first["id"] != "blk1" || first["status"] != "todo" {
		t.Errorf("first blocked_by = %v, want {id:blk1,status:todo}", first)
	}
	// Nested objects carry only id + status.
	if len(first) != 2 {
		t.Errorf("nested blocked_by object should carry only id+status, got: %v", first)
	}
}

func TestGet_JSONMultipleIDs(t *testing.T) {
	dir := setupGetCobraTest(t, getFixture())
	out, err := runGetCmd(t, dir, "task1", "blk1", "--json", "-f", "id,status")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	m := parseJSONObject(t, out)
	if _, ok := m["nib"]; ok {
		t.Error("multi-id --json must use 'nibs', not 'nib'")
	}
	arr, ok := m["nibs"].([]any)
	if !ok || len(arr) != 2 {
		t.Fatalf("expected 'nibs' array of 2, got: %v", m)
	}
	if arr[0].(map[string]any)["id"] != "task1" || arr[1].(map[string]any)["id"] != "blk1" {
		t.Errorf("unexpected ids/order in nibs array: %v", arr)
	}
}

func TestGet_JSONComputedFields(t *testing.T) {
	dir := setupGetCobraTest(t, getFixture())
	out, err := runGetCmd(t, dir, "par1", "--json", "-f", "children,progress,ready")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	nibObj := parseJSONObject(t, out)["nib"].(map[string]any)
	if nibObj["children"] != float64(2) {
		t.Errorf("children = %v, want 2", nibObj["children"])
	}
	// par1 is in-progress and unblocked. `ready` answers "can I start this?",
	// and work already underway is not something to start, so it is false —
	// this is the field's answer for every status but the startable ones.
	if nibObj["ready"] != false {
		t.Errorf("ready = %v, want false (par1 is in-progress)", nibObj["ready"])
	}
	prog, ok := nibObj["progress"].(map[string]any)
	if !ok {
		t.Fatalf("progress not an object: %v", nibObj["progress"])
	}
	// child1 completed of 2 -> total 2, done 1, percent 50.
	if prog["total"] != float64(2) || prog["done"] != float64(1) || prog["percent"] != float64(50) {
		t.Errorf("progress = %v, want {total:2,done:1,percent:50}", prog)
	}

	// child2 is todo and unblocked, so the field is not stuck at false: both
	// answers render through the same projection path.
	out, err = runGetCmd(t, dir, "child2", "--json", "-f", "ready")
	if err != nil {
		t.Fatalf("get child2 failed: %v", err)
	}
	childObj, ok := parseJSONObject(t, out)["nib"].(map[string]any)
	if !ok {
		t.Fatalf("child2 response has no \"nib\" object: %v", out)
	}
	if childObj["ready"] != true {
		t.Errorf("child2 ready = %v, want true (todo, unblocked)", childObj["ready"])
	}
}

func TestGet_ViewFullIncludesBodyAndEtag(t *testing.T) {
	dir := setupGetCobraTest(t, getFixture())
	out, err := runGetCmd(t, dir, "task1", "--json", "--view", "full")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	nibObj := parseJSONObject(t, out)["nib"].(map[string]any)
	for _, key := range []string{"body", "etag"} {
		if _, ok := nibObj[key]; !ok {
			t.Errorf("full view should include %q, keys: %v", key, keysOf(nibObj))
		}
	}
}

func TestGet_UnknownFieldErrors(t *testing.T) {
	dir := setupGetCobraTest(t, getFixture())
	_, err := runGetCmd(t, dir, "task1", "-f", "bogusfield")
	if err == nil {
		t.Fatal("expected an error for an unknown field")
	}
	if !strings.Contains(err.Error(), "unknown field") || !strings.Contains(err.Error(), "valid fields") {
		t.Errorf("error should name the field menu, got: %v", err)
	}
}

func TestGet_UnknownFieldJSONEnvelope(t *testing.T) {
	dir := setupGetCobraTest(t, getFixture())
	out, err := runGetCmd(t, dir, "task1", "--json", "-f", "bogusfield")
	if err == nil {
		t.Fatal("expected an error for an unknown field")
	}
	m := parseJSONObject(t, out)
	if _, ok := m["success"]; ok {
		t.Errorf("--json error must not carry a 'success' key; got: %v", m)
	}
	errObj, ok := m["error"].(map[string]any)
	if !ok {
		t.Fatalf("top-level 'error' object missing, got: %v", m)
	}
	if errObj["code"] != "VALIDATION_ERROR" {
		t.Errorf("error.code = %v, want VALIDATION_ERROR", errObj["code"])
	}
	if msg, _ := errObj["message"].(string); !strings.Contains(msg, "unknown field") {
		t.Errorf("error.message should name the invalid field, got: %v", errObj["message"])
	}
}

func TestGet_BadViewErrors(t *testing.T) {
	dir := setupGetCobraTest(t, getFixture())
	_, err := runGetCmd(t, dir, "task1", "--view", "bogus")
	if err == nil {
		t.Fatal("expected an error for an unknown view")
	}
	if !strings.Contains(err.Error(), "unknown view") || !strings.Contains(err.Error(), "valid views") {
		t.Errorf("error should name the view menu, got: %v", err)
	}
}

func TestGet_NotFoundText(t *testing.T) {
	dir := setupGetCobraTest(t, getFixture())
	out, err := runGetCmd(t, dir, "does-not-exist")
	if err == nil {
		t.Fatal("expected a not-found error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should indicate not found, got: %v", err)
	}
	// get's text-mode error is single-stream: written to stdout as
	// "error <CODE>: <message>" (design §5.5), not stderr.
	if !strings.Contains(out, "error NOT_FOUND: nib not found: does-not-exist") {
		t.Errorf("text error should be on stdout, got:\n%s", out)
	}
}

func TestGet_NotFoundJSONEnvelope(t *testing.T) {
	dir := setupGetCobraTest(t, getFixture())
	out, err := runGetCmd(t, dir, "does-not-exist", "--json")
	if err == nil {
		t.Fatal("expected a not-found error")
	}
	m := parseJSONObject(t, out)
	if _, ok := m["success"]; ok {
		t.Errorf("--json error must not carry a 'success' key; got: %v", m)
	}
	errObj, ok := m["error"].(map[string]any)
	if !ok {
		t.Fatalf("top-level 'error' object missing, got: %v", m)
	}
	if errObj["code"] != "NOT_FOUND" {
		t.Errorf("error.code = %v, want NOT_FOUND", errObj["code"])
	}
	if msg, _ := errObj["message"].(string); !strings.Contains(msg, "not found") {
		t.Errorf("error.message should indicate not found, got: %v", errObj["message"])
	}
}

func TestGet_ShowAliasResolves(t *testing.T) {
	dir := setupGetCobraTest(t, getFixture())
	rootCmd.SetArgs([]string{"--nibs-path", dir, "show", "task1", "-f", "id"})
	out := captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("show alias failed: %v", err)
		}
	})
	if strings.TrimSpace(out) != "id: task1" {
		t.Errorf("show alias output = %q, want 'id: task1'", out)
	}
}

// keysOf returns the sorted-insertion-independent key set of a JSON object for
// diagnostic messages.
func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
