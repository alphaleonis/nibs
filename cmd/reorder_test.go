package cmd

import (
	"encoding/hex"
	"encoding/json"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/spf13/pflag"
)

// onDiskETag computes the FNV-64a hex etag of the nib file at <nibsDir>/<id>.md.
// Mirrors nibcore.Core.computeStoredETag's hashing behaviour so CLI tests can
// build valid --child-if-match arguments without spinning up a resolver.
func onDiskETag(t *testing.T, nibsDir, id string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(nibsDir, id+".md"))
	if err != nil {
		t.Fatalf("read nib %s: %v", id, err)
	}
	h := fnv.New64a()
	h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
}

// resetReorderFlags clears the package-level flag vars used by reorderCmd AND
// Cobra's Changed-state tracking so tests don't pollute each other via
// rootCmd's singleton state.
func resetReorderFlags() {
	reorderAfter = ""
	reorderBefore = ""
	reorderFirst = false
	reorderIfMatch = ""
	reorderJSON = false
	reorderChildrenOf = ""
	reorderChildIfMatch = nil
	reorderCmd.Flags().Visit(func(f *pflag.Flag) {
		f.Changed = false
	})
}

// setupReorderCobraTest writes nib files and returns the .nibs directory so
// `rootCmd.SetArgs([...])` can drive the full Cobra pipeline.
func setupReorderCobraTest(t *testing.T, files map[string]string) string {
	t.Helper()
	t.Cleanup(resetReorderFlags)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	resetReorderFlags()

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

// reorderFixture produces a parent epic1 with three children a, b, c with
// strictly increasing order keys.
func reorderFixture() map[string]string {
	return map[string]string{
		"epic1.md": "---\nversion: 1\ntitle: Epic\nstatus: todo\ntype: epic\norder: a0\n---\n",
		"a.md":     "---\nversion: 1\ntitle: A\nstatus: todo\ntype: task\nparent: epic1\norder: a0\n---\n",
		"b.md":     "---\nversion: 1\ntitle: B\nstatus: todo\ntype: task\nparent: epic1\norder: b0\n---\n",
		"c.md":     "---\nversion: 1\ntitle: C\nstatus: todo\ntype: task\nparent: epic1\norder: c0\n---\n",
	}
}

// listChildrenOrder runs `nibs list --parent <id> --json` and returns the
// resulting children in disk order.
func listChildrenOrder(t *testing.T, nibsDir, parentID string) []*nib.Nib {
	t.Helper()
	t.Cleanup(resetListFlags)
	resetListFlags()
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "list", "--parent", parentID, "--json"})

	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("list --parent %s failed: %v", parentID, execErr)
	}
	var results []*nib.Nib
	if err := json.Unmarshal([]byte(out), &results); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}
	return results
}

// TestReorderCommand_ChildrenOf exercises the end-to-end Mode A dispatch:
// `nibs reorder --children-of <parent> <id1> <id2> <id3>`.
func TestReorderCommand_ChildrenOf(t *testing.T) {
	nibsDir := setupReorderCobraTest(t, reorderFixture())

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
		"epic1.md": "---\nversion: 1\ntitle: Epic\nstatus: todo\ntype: epic\norder: a0\n---\n",
		"a.md":     "---\nversion: 1\ntitle: A\nstatus: todo\ntype: task\nparent: epic1\norder: a0\n---\n",
		"b.md":     "---\nversion: 1\ntitle: B\nstatus: todo\ntype: task\nparent: epic1\norder: b0\n---\n",
		"c.md":     "---\nversion: 1\ntitle: C\nstatus: todo\ntype: task\nparent: epic1\norder: c0\n---\n",
		"d.md":     "---\nversion: 1\ntitle: D\nstatus: todo\ntype: task\nparent: epic1\norder: d0\n---\n",
		"e.md":     "---\nversion: 1\ntitle: E\nstatus: todo\ntype: task\nparent: epic1\norder: e0\n---\n",
	}
	nibsDir := setupReorderCobraTest(t, files)

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
		"r1.md": "---\nversion: 1\ntitle: Root1\nstatus: todo\ntype: task\norder: a0\n---\n",
		"r2.md": "---\nversion: 1\ntitle: Root2\nstatus: todo\ntype: task\norder: b0\n---\n",
	}
	nibsDir := setupReorderCobraTest(t, files)

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
func listRootOrder(t *testing.T, nibsDir string) []*nib.Nib {
	t.Helper()
	t.Cleanup(resetListFlags)
	resetListFlags()
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "list", "--no-parent", "--json"})

	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("list --no-parent failed: %v", execErr)
	}
	var results []*nib.Nib
	if err := json.Unmarshal([]byte(out), &results); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}
	return results
}

// TestReorderCommand_SingleNibBoundary locks in the boundary at len(args)==1:
// `nibs reorder a --first` is single-nib mode (existing path), NOT Mode B.
// Without this test, a future refactor could silently shift the regime
// boundary by changing the dispatch order.
func TestReorderCommand_SingleNibBoundary(t *testing.T) {
	nibsDir := setupReorderCobraTest(t, reorderFixture())

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
	nibsDir := setupReorderCobraTest(t, reorderFixture())

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
	nibsDir := setupReorderCobraTest(t, reorderFixture())

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
	nibsDir := setupReorderCobraTest(t, reorderFixture())

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
	nibsDir := setupReorderCobraTest(t, reorderFixture())

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
			nibsDir := setupReorderCobraTest(t, reorderFixture())
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
	nibsDir := setupReorderCobraTest(t, reorderFixture())

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
	nibsDir := setupReorderCobraTest(t, reorderFixture())

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
