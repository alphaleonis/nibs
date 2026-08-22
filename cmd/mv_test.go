package cmd

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/spf13/pflag"
)

// onDiskETag computes the canonical etag of the nib file at <nibsDir>/<id>.md by
// parsing it and hashing its canonical render (with the id derived from the
// filename). Mirrors nibcore.Core.computeStoredETag so CLI tests can build valid
// --child-if-match arguments without spinning up a resolver.
func onDiskETag(t *testing.T, nibsDir, id string) string {
	t.Helper()
	f, err := os.Open(dataPath(nibsDir, id+".md"))
	if err != nil {
		t.Fatalf("read nib %s: %v", id, err)
	}
	defer func() { _ = f.Close() }()
	b, err := nib.Parse(f)
	if err != nil {
		t.Fatalf("parse nib %s: %v", id, err)
	}
	b.ID = id
	return b.ETag()
}

// resetMvFlags clears the package-level flag vars used by mvCmd AND Cobra's
// Changed-state tracking so tests don't pollute each other via rootCmd's
// singleton state.
func resetMvFlags() {
	mvAfter = ""
	mvBefore = ""
	mvFirst = false
	mvParent = ""
	mvQueue = false
	mvIfMatch = ""
	mvJSON = false
	mvChildrenOf = ""
	mvChildIfMatch = nil
	mvCmd.Flags().Visit(func(f *pflag.Flag) {
		f.Changed = false
	})
}

// setupMvCobraTest writes nib files and returns the .nibs directory so
// `rootCmd.SetArgs([...])` can drive the full Cobra pipeline.
func setupMvCobraTest(t *testing.T, files map[string]string) string {
	t.Helper()
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetMvFlags)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	resetMvFlags()

	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(storeDataDir(nibsDir), 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(dataPath(nibsDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return nibsDir
}

// reorderFixture produces a parent epic1 with three children a, b, c with
// strictly increasing order keys.
func reorderFixture() map[string]string {
	return map[string]string{
		"epic1.md": "---\nversion: 2\ntitle: Epic\nstatus: todo\ntype: epic\norder: a0\n---\n",
		"a.md":     "---\nversion: 2\ntitle: A\nstatus: todo\ntype: task\nparent: epic1\norder: a0\n---\n",
		"b.md":     "---\nversion: 2\ntitle: B\nstatus: todo\ntype: task\nparent: epic1\norder: b0\n---\n",
		"c.md":     "---\nversion: 2\ntitle: C\nstatus: todo\ntype: task\nparent: epic1\norder: c0\n---\n",
	}
}

// listChildrenOrder runs `nibs list --parent <id> --json` and returns the
// resulting children in disk order. It projects id+order explicitly (the
// default ref view omits order) and reads them off the {nibs,count,truncated}
// envelope, returning bare nibs carrying only the two fields the reorder
// assertions consult (ID for identity, Order for the failure diagnostic).
func listChildrenOrder(t *testing.T, nibsDir, parentID string) []*nib.Nib {
	t.Helper()
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetListFlags)
	resetListFlags()
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "list", "--parent", parentID, "-f", "id,order", "--json"})

	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("list --parent %s failed: %v", parentID, execErr)
	}
	var env struct {
		Nibs []struct {
			ID    string `json:"id"`
			Order string `json:"order"`
		} `json:"nibs"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}
	results := make([]*nib.Nib, len(env.Nibs))
	for i, n := range env.Nibs {
		results[i] = &nib.Nib{ID: n.ID, Order: n.Order}
	}
	return results
}

// TestReorderCommand_ChildrenOf exercises the end-to-end Mode A dispatch:
// `nibs reorder --children-of <parent> <id1> <id2> <id3>`.
func TestReorderCommand_ChildrenOf(t *testing.T) {
	nibsDir := setupMvCobraTest(t, reorderFixture())

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"reorder",
		"--children-of", "epic1",
		"c", "a", "b",
	})

	var execErr error
	captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("reorder --children-of failed: %v", execErr)
	}

	got := listChildrenOrder(t, nibsDir, "epic1")
	if len(got) != 3 {
		t.Fatalf("got %d children, want 3", len(got))
	}
	wantIDs := []string{"c", "a", "b"}
	for i, b := range got {
		if b.ID != wantIDs[i] {
			t.Errorf("got[%d].ID = %q, want %q (order=%q)", i, b.ID, wantIDs[i], b.Order)
		}
	}
}

// TestReorderCommand_BlockMove exercises the end-to-end Mode B dispatch:
// `nibs reorder <id1> <id2> --after <anchor>`.
func TestReorderCommand_BlockMove(t *testing.T) {
	files := map[string]string{
		"epic1.md": "---\nversion: 2\ntitle: Epic\nstatus: todo\ntype: epic\norder: a0\n---\n",
		"a.md":     "---\nversion: 2\ntitle: A\nstatus: todo\ntype: task\nparent: epic1\norder: a0\n---\n",
		"b.md":     "---\nversion: 2\ntitle: B\nstatus: todo\ntype: task\nparent: epic1\norder: b0\n---\n",
		"c.md":     "---\nversion: 2\ntitle: C\nstatus: todo\ntype: task\nparent: epic1\norder: c0\n---\n",
		"d.md":     "---\nversion: 2\ntitle: D\nstatus: todo\ntype: task\nparent: epic1\norder: d0\n---\n",
		"e.md":     "---\nversion: 2\ntitle: E\nstatus: todo\ntype: task\nparent: epic1\norder: e0\n---\n",
	}
	nibsDir := setupMvCobraTest(t, files)

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"reorder",
		"c", "e",
		"--after", "a",
	})

	var execErr error
	captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("reorder block move failed: %v", execErr)
	}

	got := listChildrenOrder(t, nibsDir, "epic1")
	want := []string{"a", "c", "e", "b", "d"}
	if len(got) != len(want) {
		t.Fatalf("got %d children, want %d", len(got), len(want))
	}
	for i, b := range got {
		if b.ID != want[i] {
			t.Errorf("got[%d].ID = %q, want %q (order=%q)", i, b.ID, want[i], b.Order)
		}
	}
}

// TestReorderCommand_ChildrenOf_RootEmptyString exercises the root-level
// dispatch via `--children-of ""` — Cobra treats the empty-string flag as
// "set" (Changed=true), so the CLI dispatches to Mode A with parentID="".
func TestReorderCommand_ChildrenOf_RootEmptyString(t *testing.T) {
	files := map[string]string{
		"r1.md": "---\nversion: 2\ntitle: Root1\nstatus: todo\ntype: task\norder: a0\n---\n",
		"r2.md": "---\nversion: 2\ntitle: Root2\nstatus: todo\ntype: task\norder: b0\n---\n",
	}
	nibsDir := setupMvCobraTest(t, files)

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"reorder",
		"--children-of", "",
		"r2", "r1",
	})

	var execErr error
	captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("reorder --children-of \"\" failed: %v", execErr)
	}

	// Verify the root-level order is now [r2, r1] via `list --no-parent`.
	got := listRootOrder(t, nibsDir)
	if len(got) != 2 {
		t.Fatalf("got %d root nibs, want 2", len(got))
	}
	wantIDs := []string{"r2", "r1"}
	for i, b := range got {
		if b.ID != wantIDs[i] {
			t.Errorf("got[%d].ID = %q, want %q (order=%q)", i, b.ID, wantIDs[i], b.Order)
		}
	}
}

// listRootOrder runs `nibs list --no-parent --json` to list root-level nibs.
// It projects id+order and reads them off the {nibs,count,truncated} envelope
// (same shape as listChildrenOrder), returning bare nibs carrying only ID and
// Order.
func listRootOrder(t *testing.T, nibsDir string) []*nib.Nib {
	t.Helper()
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetListFlags)
	resetListFlags()
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "list", "--no-parent", "-f", "id,order", "--json"})

	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("list --no-parent failed: %v", execErr)
	}
	var env struct {
		Nibs []struct {
			ID    string `json:"id"`
			Order string `json:"order"`
		} `json:"nibs"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}
	results := make([]*nib.Nib, len(env.Nibs))
	for i, n := range env.Nibs {
		results[i] = &nib.Nib{ID: n.ID, Order: n.Order}
	}
	return results
}

// TestReorderCommand_SingleNibBoundary locks in the boundary at len(args)==1:
// `nibs reorder a --first` is single-nib mode (existing path), NOT Mode B.
// Without this test, a future refactor could silently shift the regime
// boundary by changing the dispatch order.
func TestReorderCommand_SingleNibBoundary(t *testing.T) {
	nibsDir := setupMvCobraTest(t, reorderFixture())

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"reorder",
		"c",
		"--first",
	})

	var execErr error
	captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("reorder c --first failed: %v", execErr)
	}

	// Single-nib mode should reposition c to first; siblings now [c, a, b].
	got := listChildrenOrder(t, nibsDir, "epic1")
	if len(got) != 3 {
		t.Fatalf("got %d children, want 3", len(got))
	}
	want := []string{"c", "a", "b"}
	for i, b := range got {
		if b.ID != want[i] {
			t.Errorf("got[%d].ID = %q, want %q (order=%q)", i, b.ID, want[i], b.Order)
		}
	}
}

// TestReorderCommand_MutexEnforced verifies that `--children-of` and the
// positional positioning flags (`--after/--before/--first`) cannot be
// specified together.
func TestReorderCommand_MutexEnforced(t *testing.T) {
	nibsDir := setupMvCobraTest(t, reorderFixture())

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"reorder",
		"--children-of", "epic1",
		"a",
		"--after", "b",
	})

	var execErr error
	captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr == nil {
		t.Fatal("expected error when --children-of and --after are both specified")
	}
}

// TestReorderCommand_IfMatchRejectedInModeA pins the Cobra mutex between
// --children-of and --if-match. --if-match has no canonical owner in a
// multi-nib reorder; the bulk-mode equivalent is --child-if-match (per-id
// etags). Silently ignoring --if-match would mislead callers.
func TestReorderCommand_IfMatchRejectedInModeA(t *testing.T) {
	nibsDir := setupMvCobraTest(t, reorderFixture())

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"reorder",
		"--children-of", "epic1",
		"--if-match", "abc123",
		"c", "a", "b",
	})

	var execErr error
	captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr == nil {
		t.Fatal("expected error when --children-of and --if-match are both specified")
	}
}

// TestReorderCommand_IfMatchRejectedInModeB exercises the runtime guard in
// the Mode B branch (block move). Mode B has no flag that flips it on, so
// the Cobra mutex pattern doesn't apply — runtime check is the right shape.
// --if-match has no canonical owner in a multi-nib reorder; the bulk-mode
// equivalent is --child-if-match (per-id etags). Silently ignoring
// --if-match would mislead callers.
func TestReorderCommand_IfMatchRejectedInModeB(t *testing.T) {
	nibsDir := setupMvCobraTest(t, reorderFixture())

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"reorder",
		"a", "b",
		"--after", "c",
		"--if-match", "abc123",
	})

	var execErr error
	captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr == nil {
		t.Fatal("expected error when bulk-mode reorder is given --if-match")
	}
}

// Behavior #12: CLI Mode A end-to-end with `--child-if-match`. Compute
// each child's on-disk etag, pass them through, and verify the reorder
// applies cleanly.
func TestReorderCommand_ChildIfMatch(t *testing.T) {
	nibsDir := setupMvCobraTest(t, reorderFixture())

	// Compute the on-disk etag for each child before the reorder.
	etagA := onDiskETag(t, nibsDir, "a")
	etagB := onDiskETag(t, nibsDir, "b")
	etagC := onDiskETag(t, nibsDir, "c")

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"reorder",
		"--children-of", "epic1",
		"--child-if-match", "a=" + etagA,
		"--child-if-match", "b=" + etagB,
		"--child-if-match", "c=" + etagC,
		"c", "a", "b",
	})

	var execErr error
	captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("reorder --children-of with --child-if-match failed: %v", execErr)
	}

	got := listChildrenOrder(t, nibsDir, "epic1")
	wantIDs := []string{"c", "a", "b"}
	for i, b := range got {
		if b.ID != wantIDs[i] {
			t.Errorf("got[%d].ID = %q, want %q", i, b.ID, wantIDs[i])
		}
	}
}

// Behavior #13: malformed --child-if-match values are rejected at parse
// time. Three sub-cases: no '=', empty id, empty etag.
func TestReorderCommand_ChildIfMatch_MalformedRejected(t *testing.T) {
	cases := []struct {
		name string
		val  string
	}{
		{"no equals", "abc"},
		{"empty id", "=etag"},
		{"empty etag", "id="},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			nibsDir := setupMvCobraTest(t, reorderFixture())
			rootCmd.SetArgs([]string{
				"--nibs-path", nibsDir,
				"reorder",
				"--children-of", "epic1",
				"--child-if-match", tc.val,
				"c", "a", "b",
			})
			var execErr error
			captureStdout(t, func() {
				execErr = rootCmd.Execute()
			})
			if execErr == nil {
				t.Fatalf("expected parse error for malformed --child-if-match %q", tc.val)
			}
		})
	}
}

// Behavior #14: --child-if-match in single-nib mode is a runtime error
// redirecting to --if-match.
func TestReorderCommand_ChildIfMatchRejectedInSingleNib(t *testing.T) {
	nibsDir := setupMvCobraTest(t, reorderFixture())

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"reorder",
		"a",
		"--first",
		"--child-if-match", "a=abc123",
	})
	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr == nil {
		t.Fatal("expected error when --child-if-match is used in single-nib mode")
	}
	combined := execErr.Error() + out
	if !strings.Contains(combined, "--if-match") {
		t.Errorf("error should redirect user to --if-match; got: %v / out=%s", execErr, out)
	}
}

// Behavior #15: --if-match and --child-if-match together is a Cobra
// mutex error.
func TestReorderCommand_IfMatchAndChildIfMatchMutex(t *testing.T) {
	nibsDir := setupMvCobraTest(t, reorderFixture())

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"reorder",
		"--children-of", "epic1",
		"--if-match", "abc",
		"--child-if-match", "a=xyz",
		"c", "a", "b",
	})
	var execErr error
	captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr == nil {
		t.Fatal("expected mutex error when --if-match and --child-if-match are both set")
	}
}

// getNib runs `nibs get <id> -f <fields> --json` and returns the decoded {nib}
// contract so mv tests can assert on the resulting parent/order.
func getNib(t *testing.T, nibsDir, id, fields string) map[string]any {
	t.Helper()
	t.Cleanup(resetRootPersistentFlags)
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "get", id, "-f", fields, "--json"})
	var execErr error
	out := captureStdout(t, func() { execErr = rootCmd.Execute() })
	if execErr != nil {
		t.Fatalf("get %s failed: %v", id, execErr)
	}
	var env struct {
		Nib map[string]any `json:"nib"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("unmarshal get %s: %v\nraw: %s", id, err, out)
	}
	return env.Nib
}

// TestMvRepositionAfter drives the single-nib reposition path via `nibs mv`
// (the primary name) and checks the lean card echo names the moved nib.
func TestMvRepositionAfter(t *testing.T) {
	nibsDir := setupMvCobraTest(t, reorderFixture())

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "mv", "c", "--after", "a"})
	var execErr error
	out := captureStdout(t, func() { execErr = rootCmd.Execute() })
	if execErr != nil {
		t.Fatalf("mv c --after a failed: %v", execErr)
	}
	// Lean card echo: the closed/moved nib is echoed as a card carrying its id.
	if !strings.Contains(out, "id: c") {
		t.Errorf("expected lean card echo to name the moved nib, got:\n%s", out)
	}

	got := listChildrenOrder(t, nibsDir, "epic1")
	want := []string{"a", "c", "b"}
	for i, b := range got {
		if b.ID != want[i] {
			t.Errorf("got[%d].ID = %q, want %q (order=%q)", i, b.ID, want[i], b.Order)
		}
	}
}

// TestMvRepositionFirst drives `nibs mv <id> --first`.
func TestMvRepositionFirst(t *testing.T) {
	nibsDir := setupMvCobraTest(t, reorderFixture())

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "mv", "c", "--first"})
	var execErr error
	captureStdout(t, func() { execErr = rootCmd.Execute() })
	if execErr != nil {
		t.Fatalf("mv c --first failed: %v", execErr)
	}

	got := listChildrenOrder(t, nibsDir, "epic1")
	want := []string{"c", "a", "b"}
	for i, b := range got {
		if b.ID != want[i] {
			t.Errorf("got[%d].ID = %q, want %q", i, b.ID, want[i])
		}
	}
}

// reparentFixture has two root epics (ep1 with tasks a,b; ep2 empty) so a task
// can be legally reparented from one epic to the other.
func reparentFixture() map[string]string {
	return map[string]string{
		"ep1.md": "---\nversion: 2\ntitle: Epic1\nstatus: todo\ntype: epic\norder: a0\n---\n",
		"ep2.md": "---\nversion: 2\ntitle: Epic2\nstatus: todo\ntype: epic\norder: b0\n---\n",
		"a.md":   "---\nversion: 2\ntitle: A\nstatus: todo\ntype: task\nparent: ep1\norder: a0\n---\n",
		"b.md":   "---\nversion: 2\ntitle: B\nstatus: todo\ntype: task\nparent: ep1\norder: b0\n---\n",
	}
}

// TestMvReparentAppends moves a task under a new parent with no position flag;
// it should adopt the new parent (appended to the end of its children) and the
// lean card echo should reflect the new parent.
func TestMvReparentAppends(t *testing.T) {
	nibsDir := setupMvCobraTest(t, reparentFixture())

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "mv", "a", "--parent", "ep2"})
	var execErr error
	out := captureStdout(t, func() { execErr = rootCmd.Execute() })
	if execErr != nil {
		t.Fatalf("mv a --parent ep2 failed: %v", execErr)
	}
	if !strings.Contains(out, "parent: ep2") {
		t.Errorf("expected lean card echo to show the new parent, got:\n%s", out)
	}

	got := getNib(t, nibsDir, "a", "id,parent")
	if got["parent"] != "ep2" {
		t.Errorf("parent = %v, want ep2", got["parent"])
	}
}

// TestMvReparentToFirstUnderNewParent combines --parent with --first: the nib is
// reparented and positioned first among its new siblings atomically.
func TestMvReparentToFirstUnderNewParent(t *testing.T) {
	files := reparentFixture()
	// Give ep2 an existing child so "first" is observable.
	files["z.md"] = "---\nversion: 2\ntitle: Z\nstatus: todo\ntype: task\nparent: ep2\norder: a0\n---\n"
	nibsDir := setupMvCobraTest(t, files)

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "mv", "a", "--parent", "ep2", "--first"})
	var execErr error
	captureStdout(t, func() { execErr = rootCmd.Execute() })
	if execErr != nil {
		t.Fatalf("mv a --parent ep2 --first failed: %v", execErr)
	}

	got := listChildrenOrder(t, nibsDir, "ep2")
	want := []string{"a", "z"}
	if len(got) != len(want) {
		t.Fatalf("got %d children under ep2, want %d", len(got), len(want))
	}
	for i, b := range got {
		if b.ID != want[i] {
			t.Errorf("got[%d].ID = %q, want %q", i, b.ID, want[i])
		}
	}
}

// TestMvReparentToRoot uses --parent "" to clear the parent (move to root).
func TestMvReparentToRoot(t *testing.T) {
	nibsDir := setupMvCobraTest(t, reparentFixture())

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "mv", "a", "--parent", ""})
	var execErr error
	captureStdout(t, func() { execErr = rootCmd.Execute() })
	if execErr != nil {
		t.Fatalf("mv a --parent \"\" failed: %v", execErr)
	}

	got := getNib(t, nibsDir, "a", "id,parent")
	if p, ok := got["parent"]; ok && p != "" && p != nil {
		t.Errorf("expected a to be at root (empty parent), got parent=%v", p)
	}
}

// TestMvReparentIllegalHierarchy verifies an illegal reparent (feature under a
// task) surfaces a structured HIERARCHY error carrying the allowed parent types.
func TestMvReparentIllegalHierarchy(t *testing.T) {
	files := map[string]string{
		"tk.md": "---\nversion: 2\ntitle: Task\nstatus: todo\ntype: task\norder: a0\n---\n",
		"ft.md": "---\nversion: 2\ntitle: Feature\nstatus: todo\ntype: feature\norder: b0\n---\n",
	}
	nibsDir := setupMvCobraTest(t, files)

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "mv", "ft", "--parent", "tk", "--json"})
	var execErr error
	out := captureStdout(t, func() { execErr = rootCmd.Execute() })
	if execErr == nil {
		t.Fatal("expected HIERARCHY error moving a feature under a task")
	}

	var ce *output.CodedError
	if !errors.As(execErr, &ce) {
		t.Fatalf("expected *output.CodedError, got %T: %v", execErr, execErr)
	}
	if ce.Code != output.ErrHierarchy {
		t.Errorf("code = %q, want %q", ce.Code, output.ErrHierarchy)
	}
	if output.ExitCode(ce.Code) != output.ExitValidation {
		t.Errorf("exit code = %d, want %d", output.ExitCode(ce.Code), output.ExitValidation)
	}

	// The JSON envelope must carry the allowed parent types (epic for a feature).
	var env struct {
		Error struct {
			Code               string   `json:"code"`
			AllowedParentTypes []string `json:"allowedParentTypes"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("unmarshal error envelope: %v\nraw: %s", err, out)
	}
	if env.Error.Code != output.ErrHierarchy {
		t.Errorf("envelope code = %q, want %q", env.Error.Code, output.ErrHierarchy)
	}
	if len(env.Error.AllowedParentTypes) != 1 || env.Error.AllowedParentTypes[0] != "epic" {
		t.Errorf("allowedParentTypes = %v, want [epic]", env.Error.AllowedParentTypes)
	}
}

// TestMvIfMatchConflict verifies a stale --if-match on a single-nib move surfaces
// a CONFLICT carrying the server's current etag.
func TestMvIfMatchConflict(t *testing.T) {
	nibsDir := setupMvCobraTest(t, reorderFixture())

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"mv", "c", "--first", "--if-match", "deadbeefdeadbeef", "--json",
	})
	var execErr error
	out := captureStdout(t, func() { execErr = rootCmd.Execute() })
	if execErr == nil {
		t.Fatal("expected CONFLICT error with a stale --if-match")
	}
	var ce *output.CodedError
	if !errors.As(execErr, &ce) {
		t.Fatalf("expected *output.CodedError, got %T: %v", execErr, execErr)
	}
	if ce.Code != output.ErrConflict {
		t.Errorf("code = %q, want %q", ce.Code, output.ErrConflict)
	}
	var env struct {
		Error struct {
			Code        string `json:"code"`
			CurrentEtag string `json:"currentEtag"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("unmarshal conflict envelope: %v\nraw: %s", err, out)
	}
	if env.Error.Code != output.ErrConflict {
		t.Errorf("envelope code = %q, want %q", env.Error.Code, output.ErrConflict)
	}
	if env.Error.CurrentEtag == "" {
		t.Errorf("conflict envelope missing currentEtag: %s", out)
	}
}

// TestMvChildIfMatchConflict pins both BULK arms of `nibs mv` on the same class
// the single-nib arm reports for a stale etag: CONFLICT, exit 4, with the
// server's current etag as the reconcile token.
//
// The two arms fail through different resolvers — --children-of reaches
// reorderChildren, the block move reaches reorderSiblings — and both refuse in
// pre-validation, before any write. A caller branching on $? must not read that
// as exit 5 (io/filesystem), which the agent-facing prompt documents as "do not
// silently retry": the correct repair for a stale etag is to re-read it and
// retry, and the token to do so is in the envelope.
func TestMvChildIfMatchConflict(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "children-of",
			args: []string{"mv", "--children-of", "epic1", "c", "a", "b",
				"--child-if-match", "a=deadbeefdeadbeef", "--json"},
		},
		{
			name: "block move",
			args: []string{"mv", "a", "b", "--first",
				"--child-if-match", "a=deadbeefdeadbeef", "--json"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsDir := setupMvCobraTest(t, reorderFixture())
			rootCmd.SetArgs(append([]string{"--nibs-path", nibsDir}, tt.args...))

			var execErr error
			out := captureStdout(t, func() { execErr = rootCmd.Execute() })
			if execErr == nil {
				t.Fatal("expected CONFLICT error with a stale --child-if-match")
			}
			if code := reportExitError(io.Discard, execErr); code != output.ExitConflict {
				t.Errorf("exit = %d, want %d (conflict)", code, output.ExitConflict)
			}
			var env struct {
				Error struct {
					Code        string `json:"code"`
					CurrentEtag string `json:"currentEtag"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(out), &env); err != nil {
				t.Fatalf("unmarshal conflict envelope: %v\nraw: %s", err, out)
			}
			if env.Error.Code != output.ErrConflict {
				t.Errorf("envelope code = %q, want %q", env.Error.Code, output.ErrConflict)
			}
			if env.Error.CurrentEtag == "" {
				t.Errorf("conflict envelope missing currentEtag: %s", out)
			}
			if env.Error.CurrentEtag == "deadbeefdeadbeef" {
				t.Errorf("currentEtag echoes the provided (stale) etag: %s", out)
			}
		})
	}
}

// TestMvUnknownIdIsNotFound pins `nibs mv` on the same class every other direct
// command reports for an id no nib answers to: NOT_FOUND, exit 3.
//
// Of the five commands that route a mutation failure through mutationErrCode,
// mv is the only one with no id pre-check — set, close and body resolve the id
// themselves and emit NOT_FOUND before any mutation, while mv hands the id
// straight to updateNib/reorderNib. What reaches the classifier is
// therefore GetForUpdate's bare nib.ErrNotFound, and without a branch that
// recognizes the sentinel it falls to the VALIDATION_ERROR default. Exit 2
// claims the CALLER'S INPUT was at fault, so an agent that mistypes an id here
// does not take the id-correction path it takes for every other command.
//
// Both single-nib routes are covered because they fail through different
// resolvers: --first reaches reorderNib, --parent reaches updateNib.
//
// The assertion is on the exit status through the real boundary
// (reportExitError), not merely on err != nil: what a caller branches on is $?.
func TestMvUnknownIdIsNotFound(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"reposition", []string{"mv", "nosuch", "--first"}},
		{"reparent", []string{"mv", "nosuch", "--parent", "alsonosuch"}},
	}

	for _, tt := range tests {
		t.Run(tt.name+" --json", func(t *testing.T) {
			nibsDir := setupMvCobraTest(t, reorderFixture())
			args := []string{"--nibs-path", nibsDir}
			args = append(args, tt.args...)
			rootCmd.SetArgs(append(args, "--json"))
			var execErr error
			out := captureStdout(t, func() { execErr = rootCmd.Execute() })
			if execErr == nil {
				t.Fatalf("mv %v --json returned no error; out: %q", tt.args, out)
			}
			if code := reportExitError(io.Discard, execErr); code != output.ExitNotFound {
				t.Errorf("exit code = %d, want %d (NOT_FOUND)", code, output.ExitNotFound)
			}
			var env struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(out), &env); err != nil {
				t.Fatalf("stdout is not a JSON error envelope: %v\nraw: %s", err, out)
			}
			if env.Error.Code != output.ErrNotFound {
				t.Errorf("envelope error.code = %q, want %q", env.Error.Code, output.ErrNotFound)
			}
		})

		t.Run(tt.name+" text", func(t *testing.T) {
			nibsDir := setupMvCobraTest(t, reorderFixture())
			args := []string{"--nibs-path", nibsDir}
			args = append(args, tt.args...)
			rootCmd.SetArgs(args)
			var execErr error
			out := captureStdout(t, func() { execErr = rootCmd.Execute() })
			if execErr == nil {
				t.Fatalf("mv %v returned no error; out: %q", tt.args, out)
			}
			if code := reportExitError(io.Discard, execErr); code != output.ExitNotFound {
				t.Errorf("exit code = %d, want %d (NOT_FOUND)", code, output.ExitNotFound)
			}
			var ce *output.CodedError
			if !errors.As(execErr, &ce) {
				t.Fatalf("expected *output.CodedError, got %T: %v", execErr, execErr)
			}
			if ce.Code != output.ErrNotFound {
				t.Errorf("code = %q, want %q", ce.Code, output.ErrNotFound)
			}
		})
	}
}

// TestMvNoMoveSpecified rejects a single-nib mv with neither a position nor a
// parent flag.
func TestMvNoMoveSpecified(t *testing.T) {
	nibsDir := setupMvCobraTest(t, reorderFixture())

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "mv", "c"})
	var execErr error
	captureStdout(t, func() { execErr = rootCmd.Execute() })
	if execErr == nil {
		t.Fatal("expected error when mv is given no positioning or parent flag")
	}
	var ce *output.CodedError
	if errors.As(execErr, &ce) && ce.Code != output.ErrValidation {
		t.Errorf("code = %q, want %q", ce.Code, output.ErrValidation)
	}
}

// TestMvParentRejectedWithMultipleIds verifies --parent is a single-nib operation.
func TestMvParentRejectedWithMultipleIds(t *testing.T) {
	nibsDir := setupMvCobraTest(t, reparentFixture())

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "mv", "a", "b", "--parent", "ep2"})
	var execErr error
	captureStdout(t, func() { execErr = rootCmd.Execute() })
	if execErr == nil {
		t.Fatal("expected error when --parent is combined with multiple ids")
	}
}
