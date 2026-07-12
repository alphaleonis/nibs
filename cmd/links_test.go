package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/spf13/pflag"
)

// setupLinksCobraTest writes actual nib files to disk and returns the
// .nibs directory path so the full Cobra + PersistentPreRunE pipeline
// can exercise the links command. Mirrors setupRefsCobraTest.
func setupLinksCobraTest(t *testing.T, files map[string]string) string {
	t.Helper()
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetLinksFlags)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	// Belt-and-braces: reset rootCmd's writers in case a sibling test set
	// them via rootCmd.SetOut/SetErr and forgot to defer the reset.
	// Passing nil restores Cobra's default (os.Stdout / os.Stderr), so
	// captureStdout-based assertions in subsequent tests aren't silently
	// drained into a stale buffer.
	t.Cleanup(func() { rootCmd.SetOut(nil); rootCmd.SetErr(nil) })
	resetLinksFlags()

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

// resetLinksFlags clears the package-level flag vars used by linksCmd so
// tests don't pollute each other via rootCmd's singleton state.
func resetLinksFlags() {
	linksRel = nil
	linksDepth = ""
	linksOrder = ""
	linksFlat = false
	linksJSON = false
	linksColumns = ""
	linksStatus = nil
	linksNoStatus = nil
	linksType = nil
	linksNoType = nil
	linksPriority = nil
	linksNoPriority = nil
	linksTag = nil
	linksEstimate = nil
	linksNoEstimate = nil
	linksActive = false
	if linksCmd != nil {
		linksCmd.Flags().Visit(func(f *pflag.Flag) {
			f.Changed = false
		})
	}
}

// linksFixture is the standard fixture for links tests. It covers:
//   - a1 mentions b2, c3, d4 (outbound of a1)
//   - e5 mentions a1 (inbound to a1)
//   - f6 mentions a1 (inbound, scrapped)
//   - parent/child: p-epic is parent of p-child1, p-child2, p-child3
//   - blocking: block-src is blocked by block-tgt1, block-tgt2
var linksFixture = map[string]string{
	"a1--alpha.md":   "---\ntitle: Alpha\nstatus: todo\ntype: task\n---\n\nSee #b2 and #c3 and #d4.\n",
	"b2--beta.md":    "---\ntitle: Beta\nstatus: todo\ntype: task\npriority: high\n---\n\nNo refs.\n",
	"c3--gamma.md":   "---\ntitle: Gamma\nstatus: completed\ntype: task\npriority: low\n---\n\nNo refs.\n",
	"d4--delta.md":   "---\ntitle: Delta\nstatus: todo\ntype: bug\npriority: high\n---\n\nNo refs.\n",
	"e5--epsilon.md": "---\ntitle: Epsilon\nstatus: todo\ntype: task\n---\n\nRefs #a1.\n",
	"f6--zeta.md":    "---\ntitle: Zeta\nstatus: scrapped\ntype: task\n---\n\nRefs #a1.\n",
}

// TestLinksCommand_MentionsOut_JSON is the tracer bullet — it proves the
// envelope, the dispatch table, and the `mentions-out` atomic rel all work
// end-to-end. Once this passes, every other rel is an entry in the table.
func TestLinksCommand_MentionsOut_JSON(t *testing.T) {
	nibsDir := setupLinksCobraTest(t, linksFixture)

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "links", "a1", "--rel", "mentions-out", "--json"})
	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("links --rel mentions-out --json failed: %v\nout: %s", execErr, out)
	}

	// Envelope: { id, relations: { "mentions-out": { nibs: [...] } } }
	// depth field MUST be absent when --depth was not passed.
	if strings.Contains(out, `"depth"`) {
		t.Errorf("depth key should be absent when --depth not passed, got:\n%s", out)
	}

	var env struct {
		ID        string `json:"id"`
		Relations map[string]struct {
			Nibs []*nib.Nib `json:"nibs"`
		} `json:"relations"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}
	if env.ID != "a1" {
		t.Errorf("env.ID = %q, want %q", env.ID, "a1")
	}
	body, ok := env.Relations["mentions-out"]
	if !ok {
		t.Fatalf("envelope missing mentions-out key. raw:\n%s", out)
	}
	if len(body.Nibs) != 3 {
		t.Fatalf("mentions-out nibs len = %d, want 3 (b2, c3, d4)", len(body.Nibs))
	}
	ids := map[string]bool{}
	for _, n := range body.Nibs {
		ids[n.ID] = true
	}
	for _, want := range []string{"b2", "c3", "d4"} {
		if !ids[want] {
			t.Errorf("expected %q in mentions-out, got %v", want, ids)
		}
	}
}

// decodeLinksEnvelope parses a LinksResult JSON response into a map-backed
// view for assertions.
func decodeLinksEnvelope(t *testing.T, raw string) struct {
	ID        string
	Depth     *int
	Relations map[string]struct {
		Nibs []*nib.Nib `json:"nibs"`
	}
	RelKeys []string // ordered keys as they appear in the JSON
} {
	t.Helper()
	var env struct {
		ID        string `json:"id"`
		Depth     *int   `json:"depth,omitempty"`
		Relations map[string]struct {
			Nibs []*nib.Nib `json:"nibs"`
		} `json:"relations"`
	}
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, raw)
	}

	// Extract relation keys in the order they appear in the JSON so the
	// caller-supplied rel order can be asserted.
	keys := extractRelKeyOrder(raw)

	return struct {
		ID        string
		Depth     *int
		Relations map[string]struct {
			Nibs []*nib.Nib `json:"nibs"`
		}
		RelKeys []string
	}{
		ID:        env.ID,
		Depth:     env.Depth,
		Relations: env.Relations,
		RelKeys:   keys,
	}
}

// extractRelKeyOrder scans the raw JSON for the order of keys inside the
// "relations" object. Needed because Go maps don't preserve order. Uses a
// json.Decoder that honours key order (via Token()).
func extractRelKeyOrder(raw string) []string {
	dec := json.NewDecoder(strings.NewReader(raw))
	// Walk tokens until we hit "relations" at top level, then collect keys.
	depth := 0
	inRelations := false
	var keys []string
	for {
		tok, err := dec.Token()
		if err != nil {
			return keys
		}
		switch v := tok.(type) {
		case json.Delim:
			switch v {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if inRelations && depth == 1 {
					// Finished the relations object.
					return keys
				}
			}
		case string:
			if !inRelations && depth == 1 && v == "relations" {
				inRelations = true
			} else if inRelations && depth == 2 {
				keys = append(keys, v)
				// Skip the value so we don't descend into nibs.
				var discard any
				_ = dec.Decode(&discard)
			}
		}
	}
}

func runLinksJSON(t *testing.T, args ...string) string {
	t.Helper()
	var execErr error
	out := captureStdout(t, func() {
		rootCmd.SetArgs(args)
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("links cmd failed: %v\nout: %s", execErr, out)
	}
	return out
}

func runLinksJSONExpectError(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var execErr error
	out := captureStdout(t, func() {
		rootCmd.SetArgs(args)
		execErr = rootCmd.Execute()
	})
	return out, execErr
}

// hierarchyFixture: a parent epic with three children. child2 mentions child1.
var hierarchyFixture = map[string]string{
	"p1--parent-epic.md": "---\ntitle: Parent Epic\nstatus: in-progress\ntype: epic\n---\n\nTop level.\n",
	"c1--child-one.md":   "---\ntitle: Child One\nstatus: todo\ntype: task\nparent: p1\norder: a0\n---\n\nFirst child.\n",
	"c2--child-two.md":   "---\ntitle: Child Two\nstatus: todo\ntype: task\nparent: p1\norder: a1\n---\n\nDepends on #c1.\n",
	"c3--child-three.md": "---\ntitle: Child Three\nstatus: completed\ntype: task\nparent: p1\norder: a2\n---\n\nFinished.\n",
	"orphan--loner.md":   "---\ntitle: Loner\nstatus: todo\ntype: task\n---\n\nNo parent.\n",
}

// blockingFixture: src is blocked by tgt1 (todo) and tgt2 (todo/high).
var blockingFixture = map[string]string{
	"src--source.md":   "---\ntitle: Src\nstatus: todo\ntype: task\nblocked_by: [tgt1, tgt2]\n---\n\nBlocked work.\n",
	"tgt1--target1.md": "---\ntitle: Target1\nstatus: todo\ntype: task\n---\n\nPrereq 1.\n",
	"tgt2--target2.md": "---\ntitle: Target2\nstatus: todo\ntype: task\npriority: high\n---\n\nPrereq 2.\n",
}

func TestLinksCommand_MentionsIn_JSON(t *testing.T) {
	nibsDir := setupLinksCobraTest(t, linksFixture)
	out := runLinksJSON(t, "--nibs-path", nibsDir, "links", "a1", "--rel", "mentions-in", "--json")
	env := decodeLinksEnvelope(t, out)
	body, ok := env.Relations["mentions-in"]
	if !ok {
		t.Fatalf("envelope missing mentions-in key. raw:\n%s", out)
	}
	ids := map[string]bool{}
	for _, n := range body.Nibs {
		ids[n.ID] = true
	}
	// e5 and f6 both mention a1.
	if !ids["e5"] || !ids["f6"] {
		t.Errorf("mentions-in got %v, want {e5, f6}", ids)
	}
}

func TestLinksCommand_Parent_JSON(t *testing.T) {
	nibsDir := setupLinksCobraTest(t, hierarchyFixture)
	// c1 has parent p1.
	out := runLinksJSON(t, "--nibs-path", nibsDir, "links", "c1", "--rel", "parent", "--json")
	env := decodeLinksEnvelope(t, out)
	body, ok := env.Relations["parent"]
	if !ok {
		t.Fatalf("missing parent key. raw:\n%s", out)
	}
	if len(body.Nibs) != 1 || body.Nibs[0].ID != "p1" {
		t.Errorf("parent got %+v, want [p1]", body.Nibs)
	}
}

func TestLinksCommand_Parent_Empty_JSON(t *testing.T) {
	nibsDir := setupLinksCobraTest(t, hierarchyFixture)
	// orphan has no parent.
	out := runLinksJSON(t, "--nibs-path", nibsDir, "links", "orphan", "--rel", "parent", "--json")
	env := decodeLinksEnvelope(t, out)
	body, ok := env.Relations["parent"]
	if !ok {
		t.Fatalf("missing parent key. raw:\n%s", out)
	}
	if len(body.Nibs) != 0 {
		t.Errorf("parent for orphan got %+v, want empty", body.Nibs)
	}
	// empty nibs must be [] not null — the raw JSON check guarantees shape.
	if !strings.Contains(out, `"nibs":[]`) && !strings.Contains(out, `"nibs": []`) {
		t.Errorf("expected empty `nibs: []` in output, got:\n%s", out)
	}
}

func TestLinksCommand_Children_JSON(t *testing.T) {
	nibsDir := setupLinksCobraTest(t, hierarchyFixture)
	out := runLinksJSON(t, "--nibs-path", nibsDir, "links", "p1", "--rel", "children", "--json")
	env := decodeLinksEnvelope(t, out)
	body, ok := env.Relations["children"]
	if !ok {
		t.Fatalf("missing children key. raw:\n%s", out)
	}
	if len(body.Nibs) != 3 {
		t.Fatalf("children len = %d, want 3 (c1, c2, c3)\nraw: %s", len(body.Nibs), out)
	}
	ids := map[string]bool{}
	for _, n := range body.Nibs {
		ids[n.ID] = true
	}
	for _, want := range []string{"c1", "c2", "c3"} {
		if !ids[want] {
			t.Errorf("expected %q in children, got %v", want, ids)
		}
	}
}

func TestLinksCommand_Blocking_JSON(t *testing.T) {
	nibsDir := setupLinksCobraTest(t, blockingFixture)
	// tgt1 is blocking src (src.blocked_by contains tgt1).
	out := runLinksJSON(t, "--nibs-path", nibsDir, "links", "tgt1", "--rel", "blocking", "--json")
	env := decodeLinksEnvelope(t, out)
	body, ok := env.Relations["blocking"]
	if !ok {
		t.Fatalf("missing blocking key. raw:\n%s", out)
	}
	ids := map[string]bool{}
	for _, n := range body.Nibs {
		ids[n.ID] = true
	}
	if !ids["src"] {
		t.Errorf("blocking got %v, want {src}", ids)
	}
}

func TestLinksCommand_BlockedBy_JSON(t *testing.T) {
	nibsDir := setupLinksCobraTest(t, blockingFixture)
	out := runLinksJSON(t, "--nibs-path", nibsDir, "links", "src", "--rel", "blocked-by", "--json")
	env := decodeLinksEnvelope(t, out)
	body, ok := env.Relations["blocked-by"]
	if !ok {
		t.Fatalf("missing blocked-by key. raw:\n%s", out)
	}
	ids := map[string]bool{}
	for _, n := range body.Nibs {
		ids[n.ID] = true
	}
	if !ids["tgt1"] || !ids["tgt2"] {
		t.Errorf("blocked-by got %v, want {tgt1, tgt2}", ids)
	}
}

func TestLinksCommand_Siblings_WithParent_JSON(t *testing.T) {
	nibsDir := setupLinksCobraTest(t, hierarchyFixture)
	// c1 under parent p1 has siblings c2 and c3 (self excluded).
	out := runLinksJSON(t, "--nibs-path", nibsDir, "links", "c1", "--rel", "siblings", "--json")
	env := decodeLinksEnvelope(t, out)
	body, ok := env.Relations["siblings"]
	if !ok {
		t.Fatalf("missing siblings key. raw:\n%s", out)
	}
	ids := map[string]bool{}
	for _, n := range body.Nibs {
		ids[n.ID] = true
	}
	if ids["c1"] {
		t.Errorf("siblings should exclude self (c1); got %v", ids)
	}
	if !ids["c2"] || !ids["c3"] {
		t.Errorf("siblings got %v, want {c2, c3}", ids)
	}
}

// --- Slice H: Retirement ---

func TestRefsCommand_Retired(t *testing.T) {
	// `nibs refs` must no longer be a known command.
	nibsDir := setupLinksCobraTest(t, linksFixture)
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "refs", "a1"})
	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stdout)
	defer rootCmd.SetOut(nil)
	defer rootCmd.SetErr(nil)

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected `nibs refs` to error (unknown command), got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "unknown command") && !strings.Contains(msg, "refs") {
		t.Errorf("expected 'unknown command' error for retired `refs`; got: %v", err)
	}
}

func TestDepsCommand_Retired(t *testing.T) {
	nibsDir := setupLinksCobraTest(t, linksFixture)
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "deps", "a1"})
	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stdout)
	defer rootCmd.SetOut(nil)
	defer rootCmd.SetErr(nil)

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected `nibs deps` to error (unknown command), got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "unknown command") && !strings.Contains(msg, "deps") {
		t.Errorf("expected 'unknown command' error for retired `deps`; got: %v", err)
	}
}

// --- Slice G: Error paths ---

func TestLinksCommand_UnknownRel_Errors(t *testing.T) {
	nibsDir := setupLinksCobraTest(t, linksFixture)
	_, err := runLinksJSONExpectError(t, "--nibs-path", nibsDir, "links", "a1", "--rel", "bogus")
	if err == nil {
		t.Fatal("expected error for unknown rel, got nil")
	}
	if !errors.Is(err, errLinksInvalidRel) {
		t.Errorf("expected errLinksInvalidRel, got: %v", err)
	}
	// Retain a structural marker that cannot accidentally match: the
	// "accepted:" prefix introduces the list of valid rel names.
	if !strings.Contains(err.Error(), "accepted:") {
		t.Errorf("error should include 'accepted:' list of valid rels; got: %v", err)
	}
}

func TestLinksCommand_NibNotFound_Errors(t *testing.T) {
	nibsDir := setupLinksCobraTest(t, linksFixture)
	_, err := runLinksJSONExpectError(t, "--nibs-path", nibsDir, "links", "does-not-exist", "--rel", "children")
	if err == nil {
		t.Fatal("expected error for unknown nib id, got nil")
	}
	if !errors.Is(err, errLinksNotFound) {
		t.Errorf("expected errLinksNotFound, got: %v", err)
	}
}

func TestLinksCommand_NibNotFound_JSON(t *testing.T) {
	nibsDir := setupLinksCobraTest(t, linksFixture)
	out, err := runLinksJSONExpectError(t, "--nibs-path", nibsDir, "links", "does-not-exist", "--rel", "children", "--json")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}
	if env.Error.Code != "NOT_FOUND" {
		t.Errorf("env.error.code = %q, want NOT_FOUND", env.Error.Code)
	}
}

// Note: the prior TestLinksCommand_Children_OrderTopo_SelfMentionIgnored
// has been replaced by TestLinksCommand_Children_OrderTopo_SelfBlockedByIgnored
// (defined further below). Mentions no longer affect topo order — only
// `blocked_by` does — so the self-mention property is no longer relevant.

// --- Slice F: --flat + text output ---

func TestLinksCommand_Flat_JSON_Dedupes(t *testing.T) {
	// Use neighbours where mentions-out and children overlap (for c1
	// in hierarchyFixture, self-mentioning overlaps are unlikely but we
	// can still verify dedup by constructing an overlap).
	files := map[string]string{
		"p--p.md":   "---\ntitle: P\nstatus: in-progress\ntype: epic\n---\n",
		"c1--c1.md": "---\ntitle: C1\nstatus: todo\ntype: task\nparent: p\norder: a0\n---\n\nRefs #c2.\n",
		"c2--c2.md": "---\ntitle: C2\nstatus: todo\ntype: task\nparent: p\norder: a1\n---\n",
	}
	nibsDir := setupLinksCobraTest(t, files)
	// c1's neighbours includes mentions-out {c2} and siblings {c2} — c2 appears in both.
	out := runLinksJSON(t, "--nibs-path", nibsDir, "links", "c1", "--rel", "neighbours", "--flat", "--json")
	// Flat envelope: { id, nibs: [...] } no relations key.
	if strings.Contains(out, `"relations"`) {
		t.Errorf("flat mode should not have relations key; got:\n%s", out)
	}
	var env struct {
		ID   string     `json:"id"`
		Nibs []*nib.Nib `json:"nibs"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}
	if env.ID != "c1" {
		t.Errorf("id = %q, want c1", env.ID)
	}
	seen := map[string]int{}
	for _, n := range env.Nibs {
		seen[n.ID]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Errorf("flat duplicate: %s seen %d times", id, count)
		}
	}
	// c2 should be in the deduped output once.
	if seen["c2"] != 1 {
		t.Errorf("expected c2 once in flat output; got seen=%v", seen)
	}
}

func TestLinksCommand_HumanOutput_PerRelSections(t *testing.T) {
	nibsDir := setupLinksCobraTest(t, hierarchyFixture)
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "links", "c1", "--rel", "parent,siblings"})
	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stdout)
	defer rootCmd.SetOut(nil)
	defer rootCmd.SetErr(nil)
	defer resetLinksFlags()
	defer func() { rootCmd.SetArgs(nil) }()

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("links human failed: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "parent:") {
		t.Errorf("expected 'parent:' section label; got:\n%s", out)
	}
	if !strings.Contains(out, "siblings:") {
		t.Errorf("expected 'siblings:' section label; got:\n%s", out)
	}
	iP := strings.Index(out, "parent:")
	iS := strings.Index(out, "siblings:")
	if iP > iS {
		t.Errorf("parent section should come before siblings; got:\n%s", out)
	}
}

// TestLinksCommand_Columns_Children covers `nibs links <id> --rel children
// --columns id,title`. Output must be flat (one row per linked nib, no
// per-rel section headers) so callers can split-on-tab cleanly across the
// whole stream.
func TestLinksCommand_Columns_Children(t *testing.T) {
	files := map[string]string{
		"p--p.md":   "---\ntitle: Parent\nstatus: in-progress\ntype: epic\n---\n",
		"c1--c1.md": "---\ntitle: Child One\nstatus: todo\ntype: task\nparent: p\norder: a0\n---\n",
		"c2--c2.md": "---\ntitle: Child Two\nstatus: todo\ntype: task\nparent: p\norder: a1\n---\n",
	}
	nibsDir := setupLinksCobraTest(t, files)
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "links", "p", "--rel", "children", "--columns", "id,title"})
	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stdout)
	defer rootCmd.SetOut(nil)
	defer rootCmd.SetErr(nil)
	defer resetLinksFlags()
	defer func() { rootCmd.SetArgs(nil) }()

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("links --columns failed: %v\nout: %s", err, stdout.String())
	}
	out := stdout.String()

	// Flat semantics: NO "children:" section header, NO indentation.
	if strings.Contains(out, "children:") {
		t.Errorf("--columns must not emit per-rel section header; got:\n%s", out)
	}
	want := "c1\tChild One\nc2\tChild Two\n"
	if out != want {
		t.Errorf("links --columns = %q\n              want %q", out, want)
	}
}

// TestLinksCommand_Columns_MutuallyExclusiveWithJSON rejects --columns
// combined with --json (links has no --quiet so only --json is checked).
func TestLinksCommand_Columns_MutuallyExclusiveWithJSON(t *testing.T) {
	files := map[string]string{
		"p--p.md":   "---\ntitle: Parent\nstatus: in-progress\n---\n",
		"c1--c1.md": "---\ntitle: Child\nstatus: todo\nparent: p\n---\n",
	}
	nibsDir := setupLinksCobraTest(t, files)
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "links", "p", "--rel", "children", "--columns", "id,title", "--json"})

	var execErr error
	_ = captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	t.Cleanup(resetLinksFlags)

	if execErr == nil {
		t.Fatal("links --columns --json should have failed")
	}
	if !strings.Contains(execErr.Error(), "--columns") || !strings.Contains(execErr.Error(), "--json") {
		t.Errorf("error %q does not mention both --columns and --json", execErr.Error())
	}
}

func TestLinksCommand_Flat_Human_SingleList(t *testing.T) {
	files := map[string]string{
		"p--p.md":   "---\ntitle: P\nstatus: in-progress\ntype: epic\n---\n",
		"c1--c1.md": "---\ntitle: C1\nstatus: todo\ntype: task\nparent: p\norder: a0\n---\n\nRefs #c2.\n",
		"c2--c2.md": "---\ntitle: C2\nstatus: todo\ntype: task\nparent: p\norder: a1\n---\n",
	}
	nibsDir := setupLinksCobraTest(t, files)
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "links", "c1", "--rel", "neighbours", "--flat"})
	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stdout)
	defer rootCmd.SetOut(nil)
	defer rootCmd.SetErr(nil)
	defer resetLinksFlags()
	defer func() { rootCmd.SetArgs(nil) }()

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("flat human failed: %v", err)
	}
	out := stdout.String()
	if strings.Contains(out, "parent:") || strings.Contains(out, "mentions-out:") {
		t.Errorf("flat human output should have NO section headers; got:\n%s", out)
	}
	if !strings.Contains(out, "c2") {
		t.Errorf("expected c2 in flat human output; got:\n%s", out)
	}
}

// --- Slice E: --order topo ---

func TestLinksCommand_Children_OrderTopo(t *testing.T) {
	// Edges come from `blocked_by` declarations only — mentions in body
	// prose are informational and do NOT contribute edges.
	// Input order: y (a0), x (a1), z (a2). y.blocked_by=[x]. So topo: x, y, z.
	files := map[string]string{
		"p--p.md": "---\ntitle: P\nstatus: in-progress\ntype: epic\n---\n",
		"y--y.md": "---\ntitle: Y\nstatus: todo\ntype: task\nparent: p\norder: a0\nblocked_by:\n  - x\n---\n",
		"x--x.md": "---\ntitle: X\nstatus: todo\ntype: task\nparent: p\norder: a1\n---\n",
		"z--z.md": "---\ntitle: Z\nstatus: todo\ntype: task\nparent: p\norder: a2\n---\n",
	}
	nibsDir := setupLinksCobraTest(t, files)
	out := runLinksJSON(t, "--nibs-path", nibsDir, "links", "p", "--rel", "children", "--order", "topo", "--json")
	env := decodeLinksEnvelope(t, out)
	body := env.Relations["children"]
	if len(body.Nibs) != 3 {
		t.Fatalf("topo got %d items, want 3\nraw: %s", len(body.Nibs), out)
	}
	// x must come before y (y is blocked by x).
	pos := map[string]int{}
	for i, n := range body.Nibs {
		pos[n.ID] = i
	}
	if pos["x"] >= pos["y"] {
		t.Errorf("topo order invalid: x@%d, y@%d (want x before y)", pos["x"], pos["y"])
	}
}

// Note: the prior TestLinksCommand_Children_OrderTopo_Cycle_Errors has been
// replaced by TestLinksCommand_Children_OrderTopo_BlockedByCycle_Errors
// (defined further below). The cycle is now driven by mutual `blocked_by`
// edges, not by mutual `#<id>` mentions (which no longer contribute edges).

// TestLinksCommand_Children_OrderTopo_SkipsFilteredSibling pins that an
// edge whose source is filtered out of the candidate set is dropped
// (the byID gate in topoSortNibs). It is NOT collapsed into a synthetic
// edge between the remaining nibs.
//
// Fixture: three siblings a/b/c under parent p.
//   - a: todo, no blocked_by
//   - b: completed, no blocked_by (excluded by --active)
//   - c: todo, blocked_by: [b]
//
// With --active, b is filtered out of the candidate set before topo.
// c's edge to b is then dropped by the byID gate. Result: a and c
// survive with NO topo edge between them — order falls back to stable
// insertion order, and there is no synthetic c→a constraint.
func TestLinksCommand_Children_OrderTopo_SkipsFilteredSibling(t *testing.T) {
	files := map[string]string{
		"p--p.md": "---\ntitle: P\nstatus: in-progress\ntype: epic\n---\n",
		"a--a.md": "---\ntitle: A\nstatus: todo\ntype: task\nparent: p\norder: a0\n---\n",
		"b--b.md": "---\ntitle: B\nstatus: completed\ntype: task\nparent: p\norder: a1\n---\n",
		"c--c.md": "---\ntitle: C\nstatus: todo\ntype: task\nparent: p\norder: a2\nblocked_by:\n  - b\n---\n",
	}
	nibsDir := setupLinksCobraTest(t, files)
	out := runLinksJSON(t, "--nibs-path", nibsDir, "links", "p", "--rel", "children", "--order", "topo", "--active", "--json")
	env := decodeLinksEnvelope(t, out)
	body := env.Relations["children"]
	if len(body.Nibs) != 2 {
		t.Fatalf("topo+active got %d items, want 2 (a, c; b is filtered out)\nraw: %s", len(body.Nibs), out)
	}
	ids := map[string]bool{}
	for _, n := range body.Nibs {
		ids[n.ID] = true
	}
	if ids["b"] {
		t.Errorf("b should be filtered out by --active; got %v", ids)
	}
	if !ids["a"] || !ids["c"] {
		t.Errorf("expected a and c present; got %v", ids)
	}
	// Invariant: c.blocked_by=[b] is dropped because b is not in the
	// candidate set, so there is no synthetic edge between a and c.
	// Order falls back to stable insertion order [a, c].
	if body.Nibs[0].ID != "a" || body.Nibs[1].ID != "c" {
		t.Errorf("topo order = [%s, %s], want [a, c] (stable insertion order; "+
			"any other order implies a synthetic edge from filtered-out b)",
			body.Nibs[0].ID, body.Nibs[1].ID)
	}
}

// TestLinksCommand_Children_OrderTopo_BlockedByEdges pins the post-q4pi
// contract: `blocked_by` declarations DO produce topo edges. If sibling
// B has `blocked_by: [a]`, A must come before B regardless of insertion
// order.
func TestLinksCommand_Children_OrderTopo_BlockedByEdges(t *testing.T) {
	files := map[string]string{
		"p--p.md": "---\ntitle: P\nstatus: in-progress\ntype: epic\n---\n",
		// b is listed first (a0) but is blocked by a (a1) — topo must reorder.
		"b--b.md": "---\ntitle: B\nstatus: todo\ntype: task\nparent: p\norder: a0\nblocked_by:\n  - a\n---\n",
		"a--a.md": "---\ntitle: A\nstatus: todo\ntype: task\nparent: p\norder: a1\n---\n",
	}
	nibsDir := setupLinksCobraTest(t, files)
	out := runLinksJSON(t, "--nibs-path", nibsDir, "links", "p", "--rel", "children", "--order", "topo", "--json")
	env := decodeLinksEnvelope(t, out)
	body := env.Relations["children"]
	if len(body.Nibs) != 2 {
		t.Fatalf("got %d items, want 2 (a, b)\nraw: %s", len(body.Nibs), out)
	}
	pos := map[string]int{}
	for i, n := range body.Nibs {
		pos[n.ID] = i
	}
	if pos["a"] >= pos["b"] {
		t.Errorf("topo order: a@%d, b@%d (want a before b — b.blocked_by=[a])",
			pos["a"], pos["b"])
	}
}

// TestLinksCommand_Children_OrderTopo_CrossMentionsAreNotACycle pins the
// post-q4pi contract: `#<id>` body mentions are NOT topo edges. Two
// siblings that cross-reference each other in prose (no `blocked_by`)
// must NOT trigger a cycle error, and `--order topo` returns them in
// the natural insertion order.
func TestLinksCommand_Children_OrderTopo_CrossMentionsAreNotACycle(t *testing.T) {
	files := map[string]string{
		"p--p.md": "---\ntitle: P\nstatus: in-progress\ntype: epic\n---\n",
		"a--a.md": "---\ntitle: A\nstatus: todo\ntype: task\nparent: p\norder: a0\n---\n\nSee #b for context.\n",
		"b--b.md": "---\ntitle: B\nstatus: todo\ntype: task\nparent: p\norder: a1\n---\n\nSee #a for context.\n",
	}
	nibsDir := setupLinksCobraTest(t, files)
	out := runLinksJSON(t, "--nibs-path", nibsDir, "links", "p", "--rel", "children", "--order", "topo", "--json")
	env := decodeLinksEnvelope(t, out)
	body := env.Relations["children"]
	if len(body.Nibs) != 2 {
		t.Fatalf("got %d items, want 2 (a, b)\nraw: %s", len(body.Nibs), out)
	}
	// No edges → stable insertion order = [a, b].
	if body.Nibs[0].ID != "a" || body.Nibs[1].ID != "b" {
		t.Errorf("topo order = [%s, %s], want [a, b]",
			body.Nibs[0].ID, body.Nibs[1].ID)
	}
}

// TestLinksCommand_Children_OrderTopo_BlockedByCycle_Errors pins that a
// real mutual `blocked_by` cycle still produces errLinksCycle with the
// cycle members named in the message.
func TestLinksCommand_Children_OrderTopo_BlockedByCycle_Errors(t *testing.T) {
	files := map[string]string{
		"p--p.md": "---\ntitle: P\nstatus: in-progress\ntype: epic\n---\n",
		"a--a.md": "---\ntitle: A\nstatus: todo\ntype: task\nparent: p\norder: a0\nblocked_by:\n  - b\n---\n",
		"b--b.md": "---\ntitle: B\nstatus: todo\ntype: task\nparent: p\norder: a1\nblocked_by:\n  - a\n---\n",
	}
	nibsDir := setupLinksCobraTest(t, files)
	_, err := runLinksJSONExpectError(t, "--nibs-path", nibsDir, "links", "p", "--rel", "children", "--order", "topo")
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	if !errors.Is(err, errLinksCycle) {
		t.Errorf("expected errLinksCycle, got: %v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "[a, b]") && !strings.Contains(msg, "[b, a]") {
		t.Errorf("expected bracketed cycle members ([a, b] or [b, a]); got: %v", err)
	}
}

// TestLinksCommand_Children_OrderTopo_ExternalBlockedByDropped pins that a
// `blocked_by` reference to a nib that is NOT in the candidate set is
// silently dropped — no error, no false constraint, the nib is included.
func TestLinksCommand_Children_OrderTopo_ExternalBlockedByDropped(t *testing.T) {
	files := map[string]string{
		"p--p.md": "---\ntitle: P\nstatus: in-progress\ntype: epic\n---\n",
		// 'z' is not a sibling under p — must be dropped from edge set.
		"a--a.md": "---\ntitle: A\nstatus: todo\ntype: task\nparent: p\norder: a0\n---\n",
		"b--b.md": "---\ntitle: B\nstatus: todo\ntype: task\nparent: p\norder: a1\nblocked_by:\n  - z\n---\n",
		// 'z' exists but is unrelated (not a child of p).
		"z--z.md": "---\ntitle: Z\nstatus: todo\ntype: task\n---\n",
	}
	nibsDir := setupLinksCobraTest(t, files)
	out := runLinksJSON(t, "--nibs-path", nibsDir, "links", "p", "--rel", "children", "--order", "topo", "--json")
	env := decodeLinksEnvelope(t, out)
	body := env.Relations["children"]
	if len(body.Nibs) != 2 {
		t.Fatalf("got %d items, want 2 (a, b)\nraw: %s", len(body.Nibs), out)
	}
	ids := map[string]bool{}
	for _, n := range body.Nibs {
		ids[n.ID] = true
	}
	if !ids["a"] || !ids["b"] {
		t.Errorf("expected both a and b present; got %v", ids)
	}
}

// TestLinksCommand_Children_OrderTopo_SelfBlockedByIgnored pins that a
// defensive self-reference in `blocked_by` is silently dropped, the
// nib is still included, and no cycle is reported.
func TestLinksCommand_Children_OrderTopo_SelfBlockedByIgnored(t *testing.T) {
	files := map[string]string{
		"p--p.md": "---\ntitle: P\nstatus: in-progress\ntype: epic\n---\n",
		// a lists itself plus a real dep b.
		"a--a.md": "---\ntitle: A\nstatus: todo\ntype: task\nparent: p\norder: a0\nblocked_by:\n  - a\n  - b\n---\n",
		"b--b.md": "---\ntitle: B\nstatus: todo\ntype: task\nparent: p\norder: a1\n---\n",
	}
	nibsDir := setupLinksCobraTest(t, files)
	out := runLinksJSON(t, "--nibs-path", nibsDir, "links", "p", "--rel", "children", "--order", "topo", "--json")
	env := decodeLinksEnvelope(t, out)
	body := env.Relations["children"]
	if len(body.Nibs) != 2 {
		t.Fatalf("got %d items, want 2 (a, b)\nraw: %s", len(body.Nibs), out)
	}
	pos := map[string]int{}
	for i, n := range body.Nibs {
		pos[n.ID] = i
	}
	// b must come before a (a.blocked_by=[a (self, dropped), b])
	if pos["b"] >= pos["a"] {
		t.Errorf("topo order: b@%d, a@%d (want b before a — self-edge on a dropped, real dep on b kept)",
			pos["b"], pos["a"])
	}
}

func TestLinksCommand_Parent_OrderTopo_Errors(t *testing.T) {
	nibsDir := setupLinksCobraTest(t, hierarchyFixture)
	_, err := runLinksJSONExpectError(t, "--nibs-path", nibsDir, "links", "c1", "--rel", "parent", "--order", "topo")
	if err == nil {
		t.Fatal("expected --order topo on parent to error, got nil")
	}
	if !errors.Is(err, errLinksOrderInapplicable) {
		t.Errorf("expected errLinksOrderInapplicable, got: %v", err)
	}
}

// --- Slice D: Transitive + --depth ---

// ancestryFixture: root → mid → leaf chain for ancestor/descendant tests.
var ancestryFixture = map[string]string{
	"root--r.md":   "---\ntitle: Root\nstatus: in-progress\ntype: milestone\n---\n",
	"mid--m.md":    "---\ntitle: Mid\nstatus: in-progress\ntype: epic\nparent: root\norder: a0\n---\n",
	"leaf--l.md":   "---\ntitle: Leaf\nstatus: todo\ntype: task\nparent: mid\norder: a0\n---\n",
	"grand--gl.md": "---\ntitle: Grandleaf\nstatus: todo\ntype: task\nparent: leaf\norder: a0\n---\n",
}

func TestLinksCommand_Ancestors_Depth2(t *testing.T) {
	nibsDir := setupLinksCobraTest(t, ancestryFixture)
	// grand's ancestors depth 2 → leaf, mid (not root).
	out := runLinksJSON(t, "--nibs-path", nibsDir, "links", "grand", "--rel", "ancestors", "--depth", "2", "--json")
	env := decodeLinksEnvelope(t, out)
	body := env.Relations["ancestors"]
	if len(body.Nibs) != 2 {
		t.Fatalf("ancestors depth=2 len = %d, want 2 (leaf, mid)\nraw: %s", len(body.Nibs), out)
	}
	ids := []string{body.Nibs[0].ID, body.Nibs[1].ID}
	if ids[0] != "leaf" || ids[1] != "mid" {
		t.Errorf("ancestors order = %v, want [leaf, mid]", ids)
	}
	// depth field must be present and equal 2.
	if env.Depth == nil || *env.Depth != 2 {
		t.Errorf("depth field = %v, want 2", env.Depth)
	}
}

// TestLinksCommand_Descendants_StatusFilter_PrunesSubtree pins the
// documented downward-closed prune semantic: when a transitive traversal
// hits a node that fails the filter, traversal stops there — nodes beyond
// it are NOT visited, even if they would match the filter.
func TestLinksCommand_Descendants_StatusFilter_PrunesSubtree(t *testing.T) {
	files := map[string]string{
		"root--r.md": "---\ntitle: Root\nstatus: todo\ntype: epic\n---\n",
		"mid--m.md":  "---\ntitle: Mid\nstatus: in-progress\ntype: task\nparent: root\norder: a0\n---\n",
		"leaf--l.md": "---\ntitle: Leaf\nstatus: todo\ntype: task\nparent: mid\norder: a0\n---\n",
	}
	nibsDir := setupLinksCobraTest(t, files)
	// descendants with --status todo: mid (in-progress) fails the filter and
	// prunes the subtree, so leaf (which would match) is NOT visited.
	out := runLinksJSON(t, "--nibs-path", nibsDir, "links", "root", "--rel", "descendants", "--status", "todo", "--depth", "all", "--json")
	env := decodeLinksEnvelope(t, out)
	body := env.Relations["descendants"]
	ids := map[string]bool{}
	for _, n := range body.Nibs {
		ids[n.ID] = true
	}
	if ids["leaf"] {
		t.Errorf("leaf should be pruned behind mid (in-progress fails --status todo); got %v", ids)
	}
	if ids["mid"] {
		t.Errorf("mid should be excluded by --status todo; got %v", ids)
	}
}

func TestLinksCommand_Descendants_DepthAll(t *testing.T) {
	nibsDir := setupLinksCobraTest(t, ancestryFixture)
	// root's descendants depth=all → mid, leaf, grand.
	out := runLinksJSON(t, "--nibs-path", nibsDir, "links", "root", "--rel", "descendants", "--depth", "all", "--json")
	env := decodeLinksEnvelope(t, out)
	body := env.Relations["descendants"]
	ids := map[string]bool{}
	for _, n := range body.Nibs {
		ids[n.ID] = true
	}
	for _, want := range []string{"mid", "leaf", "grand"} {
		if !ids[want] {
			t.Errorf("descendants missing %q; got %v", want, ids)
		}
	}
}

func TestLinksCommand_BlockersTransitive_DepthAll(t *testing.T) {
	// chain: a ← b ← c (a blocked_by b, b blocked_by c).
	files := map[string]string{
		"a--a.md": "---\ntitle: A\nstatus: todo\ntype: task\nblocked_by: [b]\n---\n",
		"b--b.md": "---\ntitle: B\nstatus: todo\ntype: task\nblocked_by: [c]\n---\n",
		"c--c.md": "---\ntitle: C\nstatus: todo\ntype: task\n---\n",
	}
	nibsDir := setupLinksCobraTest(t, files)
	out := runLinksJSON(t, "--nibs-path", nibsDir, "links", "a", "--rel", "blockers-transitive", "--depth", "all", "--json")
	env := decodeLinksEnvelope(t, out)
	body := env.Relations["blockers-transitive"]
	ids := map[string]bool{}
	for _, n := range body.Nibs {
		ids[n.ID] = true
	}
	if !ids["b"] || !ids["c"] {
		t.Errorf("blockers-transitive got %v, want {b, c}", ids)
	}
}

func TestLinksCommand_MentionsOutTransitive_Depth2(t *testing.T) {
	// a mentions b; b mentions c; c mentions d.
	files := map[string]string{
		"a--a.md": "---\ntitle: A\nstatus: todo\ntype: task\n---\n\nRefs #b.\n",
		"b--b.md": "---\ntitle: B\nstatus: todo\ntype: task\n---\n\nRefs #c.\n",
		"c--c.md": "---\ntitle: C\nstatus: todo\ntype: task\n---\n\nRefs #d.\n",
		"d--d.md": "---\ntitle: D\nstatus: todo\ntype: task\n---\n",
	}
	nibsDir := setupLinksCobraTest(t, files)
	// depth=2 → b, c (not d).
	out := runLinksJSON(t, "--nibs-path", nibsDir, "links", "a", "--rel", "mentions-out-transitive", "--depth", "2", "--json")
	env := decodeLinksEnvelope(t, out)
	body := env.Relations["mentions-out-transitive"]
	ids := map[string]bool{}
	for _, n := range body.Nibs {
		ids[n.ID] = true
	}
	if !ids["b"] || !ids["c"] {
		t.Errorf("expected b and c in mentions-out-transitive depth=2; got %v", ids)
	}
	if ids["d"] {
		t.Errorf("d should NOT appear at depth=2; got %v", ids)
	}
}

func TestLinksCommand_DirectRel_WithDepth_Errors(t *testing.T) {
	nibsDir := setupLinksCobraTest(t, hierarchyFixture)
	_, err := runLinksJSONExpectError(t, "--nibs-path", nibsDir, "links", "p1", "--rel", "children", "--depth", "3")
	if err == nil {
		t.Fatal("expected error for --rel children --depth 3, got nil")
	}
	if !errors.Is(err, errLinksDepthInapplicable) {
		t.Errorf("expected errLinksDepthInapplicable, got: %v", err)
	}
}

func TestLinksCommand_Depth_TrailingGarbage_Errors(t *testing.T) {
	nibsDir := setupLinksCobraTest(t, ancestryFixture)
	cases := []string{"3abc", "1.5", "-5", "3 foo", "2,3"}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			_, err := runLinksJSONExpectError(t, "--nibs-path", nibsDir, "links", "grand", "--rel", "ancestors", "--depth", raw)
			if err == nil {
				t.Fatalf("expected error for --depth %q, got nil", raw)
			}
			if !errors.Is(err, errLinksInvalidDepth) {
				t.Errorf("expected errLinksInvalidDepth for %q, got: %v", raw, err)
			}
		})
	}
}

func TestLinksCommand_DepthFieldAbsentWithoutFlag(t *testing.T) {
	nibsDir := setupLinksCobraTest(t, linksFixture)
	out := runLinksJSON(t, "--nibs-path", nibsDir, "links", "a1", "--rel", "mentions-out", "--json")
	if strings.Contains(out, `"depth"`) {
		t.Errorf("depth field should be absent when --depth not passed; got:\n%s", out)
	}
	env := decodeLinksEnvelope(t, out)
	if env.Depth != nil {
		t.Errorf("env.Depth = %v, want nil", env.Depth)
	}
}

// --- Slice C: Filters + error on inapplicable ---

func TestLinksCommand_Children_StatusAndTypeFilter(t *testing.T) {
	// Add a bug child under p1 for the type filter.
	files := map[string]string{
		"p1--parent.md": "---\ntitle: Parent\nstatus: in-progress\ntype: epic\n---\n",
		"c1--task1.md":  "---\ntitle: T1\nstatus: todo\ntype: task\nparent: p1\norder: a0\n---\n",
		"c2--task2.md":  "---\ntitle: T2\nstatus: in-progress\ntype: task\nparent: p1\norder: a1\n---\n",
		"c3--bug1.md":   "---\ntitle: B1\nstatus: todo\ntype: bug\nparent: p1\norder: a2\n---\n",
	}
	nibsDir := setupLinksCobraTest(t, files)
	// --status todo --type bug → c3 only.
	out := runLinksJSON(t, "--nibs-path", nibsDir, "links", "p1", "--rel", "children", "--status", "todo", "--type", "bug", "--json")
	env := decodeLinksEnvelope(t, out)
	body := env.Relations["children"]
	if len(body.Nibs) != 1 || body.Nibs[0].ID != "c3" {
		t.Errorf("filtered children got %+v, want [c3]", body.Nibs)
	}
}

func TestLinksCommand_Parent_WithFilter_Errors(t *testing.T) {
	nibsDir := setupLinksCobraTest(t, hierarchyFixture)
	// --rel parent + --status todo must error: parent is singular.
	_, err := runLinksJSONExpectError(t, "--nibs-path", nibsDir, "links", "c1", "--rel", "parent", "--status", "todo")
	if err == nil {
		t.Fatal("expected error for --rel parent with --status, got nil")
	}
	if !errors.Is(err, errLinksFilterInapplicable) {
		t.Errorf("expected errLinksFilterInapplicable, got: %v", err)
	}
}

func TestLinksCommand_Siblings_ActiveAndType(t *testing.T) {
	files := map[string]string{
		"p1--parent.md": "---\ntitle: Parent\nstatus: in-progress\ntype: epic\n---\n",
		"c1--task1.md":  "---\ntitle: T1\nstatus: todo\ntype: task\nparent: p1\norder: a0\n---\n",
		"c2--task2.md":  "---\ntitle: T2\nstatus: todo\ntype: task\nparent: p1\norder: a1\n---\n",
		"c3--bug1.md":   "---\ntitle: B1\nstatus: todo\ntype: bug\nparent: p1\norder: a2\n---\n",
		"c4--task3.md":  "---\ntitle: T3\nstatus: completed\ntype: task\nparent: p1\norder: a3\n---\n",
	}
	nibsDir := setupLinksCobraTest(t, files)
	// c1's siblings with --active --type task → c2 only (c3 wrong type, c4 completed).
	out := runLinksJSON(t, "--nibs-path", nibsDir, "links", "c1", "--rel", "siblings", "--active", "--type", "task", "--json")
	env := decodeLinksEnvelope(t, out)
	body := env.Relations["siblings"]
	if len(body.Nibs) != 1 || body.Nibs[0].ID != "c2" {
		t.Errorf("siblings with --active --type task got %+v, want [c2]", body.Nibs)
	}
}

func TestLinksCommand_MentionsOut_NoPriorityExclusion(t *testing.T) {
	// a1 mentions: b2 (high), c3 (normal), d4 (low).
	files := map[string]string{
		"a1--alpha.md": "---\ntitle: Alpha\nstatus: todo\ntype: task\n---\n\nSee #b2 and #c3 and #d4.\n",
		"b2--beta.md":  "---\ntitle: Beta\nstatus: todo\ntype: task\npriority: high\n---\n",
		"c3--gamma.md": "---\ntitle: Gamma\nstatus: todo\ntype: task\npriority: normal\n---\n",
		"d4--delta.md": "---\ntitle: Delta\nstatus: todo\ntype: task\npriority: low\n---\n",
	}
	nibsDir := setupLinksCobraTest(t, files)
	// --no-priority low → excludes d4; b2 and c3 remain.
	out := runLinksJSON(t, "--nibs-path", nibsDir, "links", "a1", "--rel", "mentions-out", "--no-priority", "low", "--json")
	env := decodeLinksEnvelope(t, out)
	body := env.Relations["mentions-out"]
	ids := map[string]bool{}
	for _, n := range body.Nibs {
		ids[n.ID] = true
	}
	if ids["d4"] {
		t.Errorf("d4 (low) should be excluded; got %v", ids)
	}
	if !ids["b2"] || !ids["c3"] {
		t.Errorf("expected b2 and c3 present; got %v", ids)
	}
}

// --- Slice B: Multi-rel envelope ---

func TestLinksCommand_MultiRel_PreservesOrder_JSON(t *testing.T) {
	nibsDir := setupLinksCobraTest(t, hierarchyFixture)
	// Request children then siblings; envelope must preserve that order.
	out := runLinksJSON(t, "--nibs-path", nibsDir, "links", "c1", "--rel", "children,siblings", "--json")
	env := decodeLinksEnvelope(t, out)
	if _, ok := env.Relations["children"]; !ok {
		t.Fatalf("missing children key. raw:\n%s", out)
	}
	if _, ok := env.Relations["siblings"]; !ok {
		t.Fatalf("missing siblings key. raw:\n%s", out)
	}
	if len(env.RelKeys) < 2 {
		t.Fatalf("expected at least 2 keys, got %v", env.RelKeys)
	}
	if env.RelKeys[0] != "children" || env.RelKeys[1] != "siblings" {
		t.Errorf("order = %v, want [children, siblings, ...]", env.RelKeys)
	}
}

func TestLinksCommand_Neighbours_ExpandsToSevenDirectRels(t *testing.T) {
	nibsDir := setupLinksCobraTest(t, hierarchyFixture)
	out := runLinksJSON(t, "--nibs-path", nibsDir, "links", "c1", "--rel", "neighbours", "--json")
	env := decodeLinksEnvelope(t, out)
	expectedKeys := []string{
		"mentions-out", "mentions-in", "parent", "children", "siblings", "blocking", "blocked-by",
	}
	if len(env.RelKeys) != 7 {
		t.Fatalf("neighbours produced %d keys, want 7: got %v", len(env.RelKeys), env.RelKeys)
	}
	for i, want := range expectedKeys {
		if env.RelKeys[i] != want {
			t.Errorf("key[%d] = %q, want %q", i, env.RelKeys[i], want)
		}
	}
}

// TestLinksCommand_Neighbours_WithFilter_DropsFilterOnParent pins the
// documented meta-rel asymmetry: with --rel neighbours (or neighbours-active)
// + a filter, the filter applies to the non-singular constituents and is
// silently dropped for the singular constituent (parent). The parent still
// appears even though the filter would exclude it on its own.
func TestLinksCommand_Neighbours_WithFilter_DropsFilterOnParent(t *testing.T) {
	// Parent is completed; filter is --status todo. Under --rel parent alone
	// this would be an inapplicable-filter error. Under --rel neighbours the
	// filter is silently dropped for parent, and the parent appears.
	files := map[string]string{
		"parent--p.md": "---\ntitle: P\nstatus: completed\ntype: epic\n---\n",
		"child--c.md":  "---\ntitle: C\nstatus: todo\ntype: task\nparent: parent\n---\n",
	}
	nibsDir := setupLinksCobraTest(t, files)
	out := runLinksJSON(t, "--nibs-path", nibsDir, "links", "child", "--rel", "neighbours", "--status", "todo", "--json")
	env := decodeLinksEnvelope(t, out)
	body, ok := env.Relations["parent"]
	if !ok {
		t.Fatalf("missing parent key under neighbours. raw:\n%s", out)
	}
	if len(body.Nibs) != 1 || body.Nibs[0].ID != "parent" {
		t.Errorf("parent under neighbours+filter got %+v, want [parent] (filter is silently dropped for singular constituent)", body.Nibs)
	}
}

func TestLinksCommand_NeighboursActive_ExcludesCompleted(t *testing.T) {
	nibsDir := setupLinksCobraTest(t, hierarchyFixture)
	// p1's children include c3 (completed). neighbours-active should exclude it.
	out := runLinksJSON(t, "--nibs-path", nibsDir, "links", "p1", "--rel", "neighbours-active", "--json")
	env := decodeLinksEnvelope(t, out)
	body, ok := env.Relations["children"]
	if !ok {
		t.Fatalf("missing children key under neighbours-active. raw:\n%s", out)
	}
	for _, n := range body.Nibs {
		if n.ID == "c3" {
			t.Errorf("neighbours-active should drop completed c3, got %+v", body.Nibs)
		}
	}
	// Verify the "neighbours" meta rel also produces 7 keys here.
	if len(env.RelKeys) != 7 {
		t.Errorf("neighbours-active keys len = %d, want 7 (got %v)", len(env.RelKeys), env.RelKeys)
	}
}

func TestLinksCommand_Siblings_NoParent_JSON(t *testing.T) {
	// Multiple root-level nibs; siblings = other roots.
	files := map[string]string{
		"root1--r1.md": "---\ntitle: R1\nstatus: todo\ntype: task\n---\n",
		"root2--r2.md": "---\ntitle: R2\nstatus: todo\ntype: task\n---\n",
		"root3--r3.md": "---\ntitle: R3\nstatus: todo\ntype: task\n---\n",
		"child--c.md":  "---\ntitle: C\nstatus: todo\ntype: task\nparent: root1\n---\n",
	}
	nibsDir := setupLinksCobraTest(t, files)
	out := runLinksJSON(t, "--nibs-path", nibsDir, "links", "root1", "--rel", "siblings", "--json")
	env := decodeLinksEnvelope(t, out)
	body, ok := env.Relations["siblings"]
	if !ok {
		t.Fatalf("missing siblings key. raw:\n%s", out)
	}
	ids := map[string]bool{}
	for _, n := range body.Nibs {
		ids[n.ID] = true
	}
	if ids["root1"] {
		t.Errorf("siblings should exclude self (root1); got %v", ids)
	}
	if !ids["root2"] || !ids["root3"] {
		t.Errorf("root-level siblings got %v, want {root2, root3}", ids)
	}
	if ids["child"] {
		t.Errorf("siblings should only include root-level nibs; got child too: %v", ids)
	}
}

// TestLinksCommand_JSONMode_FailingCommand_NoUsageNoAutoError pins the
// end-to-end contract for nib nibs-382a:
//
//   - In --json mode, a failing RunE returns an error that satisfies
//     errors.Is(err, output.ErrAlreadyReported) so Execute() suppresses
//     the duplicate stderr "Error: ..." line.
//   - Stdout contains exactly one parseable JSON document with success=false
//     (the JSON envelope written by output.Error).
//   - Neither cmd.OutOrStdout() nor cmd.OutOrStderr() contains a "Usage:"
//     block (Cobra's SilenceUsage=true) or an auto "Error:" line
//     (Cobra's SilenceErrors=true). The boundary owns error printing.
func TestLinksCommand_JSONMode_FailingCommand_NoUsageNoAutoError(t *testing.T) {
	nibsDir := setupLinksCobraTest(t, linksFixture)

	var cobraStdout, cobraStderr bytes.Buffer
	rootCmd.SetOut(&cobraStdout)
	rootCmd.SetErr(&cobraStderr)
	t.Cleanup(func() { rootCmd.SetOut(nil); rootCmd.SetErr(nil) })

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "links", "bogus-id", "--rel", "children", "--json"})

	var execErr error
	stdout := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})

	if execErr == nil {
		t.Fatal("expected execErr from failing JSON command, got nil")
	}
	if !errors.Is(execErr, output.ErrAlreadyReported) {
		t.Errorf("errors.Is(execErr, output.ErrAlreadyReported) = false, want true; err = %v", execErr)
	}

	// Captured os.Stdout must contain exactly one parseable JSON document
	// in the {error:{code,message}} contract (no success/data wrapper).
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("stdout is not a single parseable JSON document: %v\nraw: %s", err, stdout)
	}
	if env.Error.Code != "NOT_FOUND" {
		t.Errorf("env.error.code = %q, want NOT_FOUND; raw: %s", env.Error.Code, stdout)
	}

	// Cobra's writers must contain neither a Usage: block nor an auto Error: line.
	if got := cobraStdout.String(); strings.Contains(got, "Usage:") || strings.Contains(got, "Error:") {
		t.Errorf("rootCmd OutOrStdout contains Usage:/Error:; got:\n%s", got)
	}
	if got := cobraStderr.String(); strings.Contains(got, "Usage:") || strings.Contains(got, "Error:") {
		t.Errorf("rootCmd OutOrStderr contains Usage:/Error:; got:\n%s", got)
	}
}

// TestLinksCommand_TextMode_FailingCommand_NoUsageNoAutoError pins the
// text-mode side of the same contract: usage is for --help, not RunE
// errors. The error returned by Execute() does NOT satisfy
// errors.Is(err, output.ErrAlreadyReported) — the CLI boundary will print
// "Error: ..." to its own os.Stderr (tested separately in
// TestReportExitError); here we only assert that Cobra's auto-print is
// silenced and no Usage: block leaks.
func TestLinksCommand_TextMode_FailingCommand_NoUsageNoAutoError(t *testing.T) {
	nibsDir := setupLinksCobraTest(t, linksFixture)

	var cobraStdout, cobraStderr bytes.Buffer
	rootCmd.SetOut(&cobraStdout)
	rootCmd.SetErr(&cobraStderr)
	t.Cleanup(func() { rootCmd.SetOut(nil); rootCmd.SetErr(nil) })

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "links", "bogus-id", "--rel", "children"})

	var execErr error
	stdout := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})

	if execErr == nil {
		t.Fatal("expected execErr from failing text-mode command, got nil")
	}
	if errors.Is(execErr, output.ErrAlreadyReported) {
		t.Errorf("text-mode err should NOT satisfy ErrAlreadyReported; got: %v", execErr)
	}

	// No JSON envelope was written; stdout should be empty.
	if strings.Contains(stdout, "Usage:") {
		t.Errorf("captured stdout contains Usage: block; got:\n%s", stdout)
	}

	// Cobra's writers must contain neither a Usage: block nor an auto
	// "Error:" line.
	if got := cobraStdout.String(); strings.Contains(got, "Usage:") || strings.Contains(got, "Error:") {
		t.Errorf("rootCmd OutOrStdout contains Usage:/Error: in text mode; got:\n%s", got)
	}
	if got := cobraStderr.String(); strings.Contains(got, "Usage:") || strings.Contains(got, "Error:") {
		t.Errorf("rootCmd OutOrStderr contains Usage:/Error: in text mode; got:\n%s", got)
	}
}
