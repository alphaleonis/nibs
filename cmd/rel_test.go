package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// setupRelCobraTest writes actual nib files to disk and returns the
// .nibs directory path so the full Cobra + PersistentPreRunE pipeline
// can exercise the rel command.
func setupRelCobraTest(t *testing.T, files map[string]string) string {
	t.Helper()
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetRelFlags)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	// Belt-and-braces: reset rootCmd's writers in case a sibling test set
	// them via rootCmd.SetOut/SetErr and forgot to defer the reset.
	// Passing nil restores Cobra's default (os.Stdout / os.Stderr), so
	// captureStdout-based assertions in subsequent tests aren't silently
	// drained into a stale buffer.
	t.Cleanup(func() { rootCmd.SetOut(nil); rootCmd.SetErr(nil) })
	resetRelFlags()

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

// resetRelFlags clears the package-level flag vars used by relCmd so
// tests don't pollute each other via rootCmd's singleton state.
func resetRelFlags() {
	relKinds = nil
	relDepth = ""
	relOrder = ""
	relFlat = false
	relJSON = false
	relView = ""
	relFields = ""
	relNoHeader = false
	relLimit = 0
	relStatus = nil
	relNoStatus = nil
	relType = nil
	relNoType = nil
	relPriority = nil
	relNoPriority = nil
	relTag = nil
	relEstimate = nil
	relNoEstimate = nil
	relOpen = false
	relAll = false
	if relCmd != nil {
		relCmd.Flags().Visit(func(f *pflag.Flag) {
			f.Changed = false
		})
	}
}

// relRequiresRelFlag reports whether `nibs rel` genuinely demands --rel. Two
// things would make it required: the flag marked required at registration, or a
// parse that turns an omitted --rel into no relation at all. The grammar
// surfaces are asserted against this rather than against a literal, so
// documentation that says --rel is optional cannot outlive the arity it
// describes.
func relRequiresRelFlag(t *testing.T) bool {
	t.Helper()
	f := relCmd.Flags().Lookup("rel")
	if f == nil {
		t.Fatalf("rel command has no --rel flag")
	}
	if vals, ok := f.Annotations[cobra.BashCompOneRequiredFlag]; ok && slices.Contains(vals, "true") {
		return true
	}
	rels, err := parseRels(nil)
	return err != nil || len(rels) == 0
}

// relDefaultDoc matches text that names relDefaultKind AS the default: the word
// "default" and the relation name in the same sentence, in either order. Mere
// containment of the name would pass on every surface that lists the accepted
// relations, which is exactly the state this guards against.
var relDefaultDoc = regexp.MustCompile(`(?i)(?:default[^.\n]{0,60}` + regexp.QuoteMeta(string(relDefaultKind)) +
	`|` + regexp.QuoteMeta(string(relDefaultKind)) + `[^.\n]{0,60}default)`)

// statesRelDefault reports whether the given help text tells a caller what an
// omitted --rel resolves to.
func statesRelDefault(text string) bool {
	return relDefaultDoc.MatchString(text)
}

// relFixture is the standard fixture for rel tests. It covers:
//   - a1 mentions b2, c3, d4 (outbound of a1)
//   - e5 mentions a1 (inbound to a1)
//   - f6 mentions a1 (inbound, scrapped)
var relFixture = map[string]string{
	"a1--alpha.md":   "---\nversion: 2\ntitle: Alpha\nstatus: todo\ntype: task\n---\n\nSee #b2 and #c3 and #d4.\n",
	"b2--beta.md":    "---\nversion: 2\ntitle: Beta\nstatus: todo\ntype: task\npriority: high\n---\n\nNo refs.\n",
	"c3--gamma.md":   "---\nversion: 2\ntitle: Gamma\nstatus: completed\ntype: task\npriority: low\n---\n\nNo refs.\n",
	"d4--delta.md":   "---\nversion: 2\ntitle: Delta\nstatus: todo\ntype: bug\npriority: high\n---\n\nNo refs.\n",
	"e5--epsilon.md": "---\nversion: 2\ntitle: Epsilon\nstatus: todo\ntype: task\n---\n\nRefs #a1.\n",
	"f6--zeta.md":    "---\nversion: 2\ntitle: Zeta\nstatus: scrapped\ntype: task\n---\n\nRefs #a1.\n",
}

// hierarchyFixture: a parent epic with three children. child2 mentions child1.
var hierarchyFixture = map[string]string{
	"p1--parent-epic.md": "---\nversion: 2\ntitle: Parent Epic\nstatus: in-progress\ntype: epic\n---\n\nTop level.\n",
	"c1--child-one.md":   "---\nversion: 2\ntitle: Child One\nstatus: todo\ntype: task\nparent: p1\norder: a0\n---\n\nFirst child.\n",
	"c2--child-two.md":   "---\nversion: 2\ntitle: Child Two\nstatus: todo\ntype: task\nparent: p1\norder: a1\n---\n\nDepends on #c1.\n",
	"c3--child-three.md": "---\nversion: 2\ntitle: Child Three\nstatus: completed\ntype: task\nparent: p1\norder: a2\n---\n\nFinished.\n",
	"orphan--loner.md":   "---\nversion: 2\ntitle: Loner\nstatus: todo\ntype: task\n---\n\nNo parent.\n",
}

// blockingFixture: src is blocked by tgt1 (todo) and tgt2 (todo/high).
var blockingFixture = map[string]string{
	"src--source.md":   "---\nversion: 2\ntitle: Src\nstatus: todo\ntype: task\nblocked_by: [tgt1, tgt2]\n---\n\nBlocked work.\n",
	"tgt1--target1.md": "---\nversion: 2\ntitle: Target1\nstatus: todo\ntype: task\n---\n\nPrereq 1.\n",
	"tgt2--target2.md": "---\nversion: 2\ntitle: Target2\nstatus: todo\ntype: task\npriority: high\n---\n\nPrereq 2.\n",
}

// ancestryFixture: root → mid → leaf chain for ancestor/descendant tests.
var ancestryFixture = map[string]string{
	"root--r.md":   "---\nversion: 2\ntitle: Root\nstatus: in-progress\ntype: milestone\n---\n",
	"mid--m.md":    "---\nversion: 2\ntitle: Mid\nstatus: in-progress\ntype: epic\nparent: root\norder: a0\n---\n",
	"leaf--l.md":   "---\nversion: 2\ntitle: Leaf\nstatus: todo\ntype: task\nparent: mid\norder: a0\n---\n",
	"grand--gl.md": "---\nversion: 2\ntitle: Grandleaf\nstatus: todo\ntype: task\nparent: leaf\norder: a0\n---\n",
}

// relEnvelope is the {nibs,count,truncated} shape rel and list share. The
// projected nibs are flat objects whose keys depend on the projection, so we
// decode them as generic maps and read the fields the test cares about.
type relEnvelope struct {
	Nibs      []map[string]any `json:"nibs"`
	Count     int              `json:"count"`
	Truncated bool             `json:"truncated"`
}

func decodeRelEnvelope(t *testing.T, raw string) relEnvelope {
	t.Helper()
	// The envelope is a flat list: it must never carry a per-rel "relations" key.
	if strings.Contains(raw, `"relations"`) {
		t.Fatalf("rel envelope should not contain a relations key; got:\n%s", raw)
	}
	var env relEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, raw)
	}
	return env
}

// relEnvIDs returns the set of projected ids in the envelope.
func relEnvIDs(env relEnvelope) map[string]bool {
	ids := map[string]bool{}
	for _, n := range env.Nibs {
		if id, ok := n["id"].(string); ok {
			ids[id] = true
		}
	}
	return ids
}

// relEnvIDOrder returns the projected ids in envelope order.
func relEnvIDOrder(env relEnvelope) []string {
	var ids []string
	for _, n := range env.Nibs {
		if id, ok := n["id"].(string); ok {
			ids = append(ids, id)
		}
	}
	return ids
}

func runRelJSON(t *testing.T, args ...string) string {
	t.Helper()
	var execErr error
	out := captureStdout(t, func() {
		rootCmd.SetArgs(args)
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("rel cmd failed: %v\nout: %s", execErr, out)
	}
	return out
}

func runRelExpectError(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var execErr error
	out := captureStdout(t, func() {
		rootCmd.SetArgs(args)
		execErr = rootCmd.Execute()
	})
	return out, execErr
}

// --- Direct rels ---

// TestRelCommand_MentionsOut_JSON is the tracer bullet — it proves the shared
// {nibs,count,truncated} envelope and the `mentions-out` atomic rel both work
// end-to-end.
func TestRelCommand_MentionsOut_JSON(t *testing.T) {
	nibsDir := setupRelCobraTest(t, relFixture)
	// --all keeps c3 (completed) in the result (rel is open-by-default).
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "a1", "--rel", "mentions-out", "--all", "--json")
	env := decodeRelEnvelope(t, out)
	if env.Count != 3 {
		t.Fatalf("count = %d, want 3 (b2, c3, d4)\nraw: %s", env.Count, out)
	}
	ids := relEnvIDs(env)
	for _, want := range []string{"b2", "c3", "d4"} {
		if !ids[want] {
			t.Errorf("expected %q in mentions-out, got %v", want, ids)
		}
	}
}

func TestRelCommand_MentionsIn_JSON(t *testing.T) {
	nibsDir := setupRelCobraTest(t, relFixture)
	// --all keeps f6 (scrapped) in the result (rel is open-by-default).
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "a1", "--rel", "mentions-in", "--all", "--json")
	env := decodeRelEnvelope(t, out)
	ids := relEnvIDs(env)
	// e5 and f6 both mention a1.
	if !ids["e5"] || !ids["f6"] {
		t.Errorf("mentions-in got %v, want {e5, f6}", ids)
	}
}

func TestRelCommand_Parent_JSON(t *testing.T) {
	nibsDir := setupRelCobraTest(t, hierarchyFixture)
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "c1", "--rel", "parent", "--json")
	env := decodeRelEnvelope(t, out)
	if env.Count != 1 || relEnvIDOrder(env)[0] != "p1" {
		t.Errorf("parent got %v, want [p1]", relEnvIDOrder(env))
	}
}

func TestRelCommand_Parent_Empty_JSON(t *testing.T) {
	nibsDir := setupRelCobraTest(t, hierarchyFixture)
	// orphan has no parent → empty envelope.
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "orphan", "--rel", "parent", "--json")
	env := decodeRelEnvelope(t, out)
	if env.Count != 0 || len(env.Nibs) != 0 {
		t.Errorf("parent for orphan got %v, want empty", relEnvIDOrder(env))
	}
	// empty nibs must be [] not null.
	if !strings.Contains(out, `"nibs":[]`) && !strings.Contains(out, `"nibs": []`) {
		t.Errorf("expected empty `nibs: []` in output, got:\n%s", out)
	}
}

func TestRelCommand_Children_JSON(t *testing.T) {
	nibsDir := setupRelCobraTest(t, hierarchyFixture)
	// --all keeps c3 (completed) in the child set (rel is open-by-default).
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "p1", "--rel", "children", "--all", "--json")
	env := decodeRelEnvelope(t, out)
	if env.Count != 3 {
		t.Fatalf("children count = %d, want 3 (c1, c2, c3)\nraw: %s", env.Count, out)
	}
	ids := relEnvIDs(env)
	for _, want := range []string{"c1", "c2", "c3"} {
		if !ids[want] {
			t.Errorf("expected %q in children, got %v", want, ids)
		}
	}
}

func TestRelCommand_Blocking_JSON(t *testing.T) {
	nibsDir := setupRelCobraTest(t, blockingFixture)
	// tgt1 is blocking src (src.blocked_by contains tgt1).
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "tgt1", "--rel", "blocking", "--json")
	env := decodeRelEnvelope(t, out)
	if !relEnvIDs(env)["src"] {
		t.Errorf("blocking got %v, want {src}", relEnvIDs(env))
	}
}

func TestRelCommand_BlockedBy_JSON(t *testing.T) {
	nibsDir := setupRelCobraTest(t, blockingFixture)
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "src", "--rel", "blocked-by", "--json")
	env := decodeRelEnvelope(t, out)
	ids := relEnvIDs(env)
	if !ids["tgt1"] || !ids["tgt2"] {
		t.Errorf("blocked-by got %v, want {tgt1, tgt2}", ids)
	}
}

func TestRelCommand_Siblings_WithParent_JSON(t *testing.T) {
	nibsDir := setupRelCobraTest(t, hierarchyFixture)
	// c1 under parent p1 has siblings c2 and c3 (self excluded). --all keeps
	// c3 (completed) visible (rel is open-by-default).
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "c1", "--rel", "siblings", "--all", "--json")
	env := decodeRelEnvelope(t, out)
	ids := relEnvIDs(env)
	if ids["c1"] {
		t.Errorf("siblings should exclude self (c1); got %v", ids)
	}
	if !ids["c2"] || !ids["c3"] {
		t.Errorf("siblings got %v, want {c2, c3}", ids)
	}
}

func TestRelCommand_Siblings_NoParent_JSON(t *testing.T) {
	files := map[string]string{
		"root1--r1.md": "---\nversion: 2\ntitle: R1\nstatus: todo\ntype: task\n---\n",
		"root2--r2.md": "---\nversion: 2\ntitle: R2\nstatus: todo\ntype: task\n---\n",
		"root3--r3.md": "---\nversion: 2\ntitle: R3\nstatus: todo\ntype: task\n---\n",
		"child--c.md":  "---\nversion: 2\ntitle: C\nstatus: todo\ntype: task\nparent: root1\n---\n",
	}
	nibsDir := setupRelCobraTest(t, files)
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "root1", "--rel", "siblings", "--json")
	env := decodeRelEnvelope(t, out)
	ids := relEnvIDs(env)
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

// --- Multi-rel: union + dedup (the flat envelope collapses across rels) ---

func TestRelCommand_MultiRel_UnionDedup_JSON(t *testing.T) {
	// c1's mentions-out {c2} and siblings {c2} both include c2 — the union
	// must dedup it to a single entry.
	files := map[string]string{
		"p--p.md":   "---\nversion: 2\ntitle: P\nstatus: in-progress\ntype: epic\n---\n",
		"c1--c1.md": "---\nversion: 2\ntitle: C1\nstatus: todo\ntype: task\nparent: p\norder: a0\n---\n\nRefs #c2.\n",
		"c2--c2.md": "---\nversion: 2\ntitle: C2\nstatus: todo\ntype: task\nparent: p\norder: a1\n---\n",
	}
	nibsDir := setupRelCobraTest(t, files)
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "c1", "--rel", "mentions-out,siblings", "--json")
	env := decodeRelEnvelope(t, out)
	count := map[string]int{}
	for _, id := range relEnvIDOrder(env) {
		count[id]++
	}
	if count["c2"] != 1 {
		t.Errorf("expected c2 exactly once (deduped across rels); got order=%v", relEnvIDOrder(env))
	}
	if env.Count != len(env.Nibs) {
		t.Errorf("count (%d) must equal len(nibs) (%d)", env.Count, len(env.Nibs))
	}
}

func TestRelCommand_Neighbours_UnionsSevenRels(t *testing.T) {
	nibsDir := setupRelCobraTest(t, hierarchyFixture)
	// c1's neighbours: parent p1 and siblings c2/c3. All deduped, self excluded.
	// --all keeps c3 (completed sibling) visible (rel is open-by-default).
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "c1", "--rel", "neighbours", "--all", "--json")
	env := decodeRelEnvelope(t, out)
	ids := relEnvIDs(env)
	for _, want := range []string{"p1", "c2", "c3"} {
		if !ids[want] {
			t.Errorf("neighbours missing %q; got %v", want, ids)
		}
	}
	if ids["c1"] {
		t.Errorf("neighbours should never include self (c1); got %v", ids)
	}
}

func TestRelCommand_NeighboursActive_ExcludesCompleted(t *testing.T) {
	nibsDir := setupRelCobraTest(t, hierarchyFixture)
	// p1's children include c3 (completed). neighbours-active should exclude it.
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "p1", "--rel", "neighbours-active", "--json")
	env := decodeRelEnvelope(t, out)
	if relEnvIDs(env)["c3"] {
		t.Errorf("neighbours-active should drop completed c3, got %v", relEnvIDOrder(env))
	}
}

// --- The omitted --rel default ---

// TestRelCommand_OmittedRel_RunsTheDocumentedDefault proves relDefaultKind is
// the relation an omitted --rel actually queries, not merely the one the help
// text claims: a bare invocation must return exactly what naming that kind
// returns. The documentation surfaces are asserted against the same constant,
// so a default changed in parseRels alone breaks this rather than leaving the
// help quietly wrong.
func TestRelCommand_OmittedRel_RunsTheDocumentedDefault(t *testing.T) {
	nibsDir := setupRelCobraTest(t, hierarchyFixture)
	// p1 is queried because it relates in more than one direction — three
	// children plus one root-level sibling. A nib whose only relation is the
	// default's would return the same set for several defaults, and the
	// comparison would prove nothing about which one ran.
	explicit := relEnvIDOrder(decodeRelEnvelope(t,
		runRelJSON(t, "--nibs-path", nibsDir, "rel", "p1", "--rel", string(relDefaultKind), "--all", "--json")))
	resetRelFlags()
	bare := relEnvIDOrder(decodeRelEnvelope(t,
		runRelJSON(t, "--nibs-path", nibsDir, "rel", "p1", "--all", "--json")))

	if len(bare) == 0 {
		t.Fatalf("bare 'nibs rel p1' returned nothing; the comparison below would be vacuous")
	}
	if !slices.Equal(bare, explicit) {
		t.Errorf("bare 'nibs rel p1' = %v, but '--rel %s' = %v; the documented default is not the one the command applies",
			bare, relDefaultKind, explicit)
	}
}

// TestRelHelpDocumentsTheOmittedRelDefault asserts `nibs rel --help` says what
// omitting --rel returns. Omitting it yields a plausible-looking related set
// instead of an error, so a caller who never learns the default reads someone
// else's relationships as this nib's. Every other default on the command
// (--depth, --view, the output mode) is stated; this one is the least
// guessable.
func TestRelHelpDocumentsTheOmittedRelDefault(t *testing.T) {
	if relRequiresRelFlag(t) {
		t.Skip("--rel is required, so there is no omitted-flag default to document")
	}
	usage := relCmd.Flags().Lookup("rel").Usage
	if !statesRelDefault(usage) {
		t.Errorf("--rel usage string never names %q as the default: %q", relDefaultKind, usage)
	}
	if !statesRelDefault(relCmd.Long) {
		t.Errorf("rel long help never names %q as the default an omitted --rel resolves to:\n%s", relDefaultKind, relCmd.Long)
	}
}

// --- --order topo ---

func TestRelCommand_Children_OrderTopo(t *testing.T) {
	// Edges come from `blocked_by` declarations only.
	// Input order: y (a0), x (a1), z (a2). y.blocked_by=[x]. So topo: x before y.
	files := map[string]string{
		"p--p.md": "---\nversion: 2\ntitle: P\nstatus: in-progress\ntype: epic\n---\n",
		"y--y.md": "---\nversion: 2\ntitle: Y\nstatus: todo\ntype: task\nparent: p\norder: a0\nblocked_by:\n  - x\n---\n",
		"x--x.md": "---\nversion: 2\ntitle: X\nstatus: todo\ntype: task\nparent: p\norder: a1\n---\n",
		"z--z.md": "---\nversion: 2\ntitle: Z\nstatus: todo\ntype: task\nparent: p\norder: a2\n---\n",
	}
	nibsDir := setupRelCobraTest(t, files)
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "p", "--rel", "children", "--order", "topo", "--json")
	env := decodeRelEnvelope(t, out)
	if env.Count != 3 {
		t.Fatalf("topo got %d items, want 3\nraw: %s", env.Count, out)
	}
	pos := map[string]int{}
	for i, id := range relEnvIDOrder(env) {
		pos[id] = i
	}
	if pos["x"] >= pos["y"] {
		t.Errorf("topo order invalid: x@%d, y@%d (want x before y)", pos["x"], pos["y"])
	}
}

func TestRelCommand_Children_OrderTopo_BlockedByEdges(t *testing.T) {
	files := map[string]string{
		"p--p.md": "---\nversion: 2\ntitle: P\nstatus: in-progress\ntype: epic\n---\n",
		// b is listed first (a0) but is blocked by a (a1) — topo must reorder.
		"b--b.md": "---\nversion: 2\ntitle: B\nstatus: todo\ntype: task\nparent: p\norder: a0\nblocked_by:\n  - a\n---\n",
		"a--a.md": "---\nversion: 2\ntitle: A\nstatus: todo\ntype: task\nparent: p\norder: a1\n---\n",
	}
	nibsDir := setupRelCobraTest(t, files)
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "p", "--rel", "children", "--order", "topo", "--json")
	env := decodeRelEnvelope(t, out)
	pos := map[string]int{}
	for i, id := range relEnvIDOrder(env) {
		pos[id] = i
	}
	if pos["a"] >= pos["b"] {
		t.Errorf("topo order: a@%d, b@%d (want a before b — b.blocked_by=[a])", pos["a"], pos["b"])
	}
}

func TestRelCommand_Children_OrderTopo_CrossMentionsAreNotACycle(t *testing.T) {
	// `#<id>` body mentions are NOT topo edges (only blocked_by is).
	files := map[string]string{
		"p--p.md": "---\nversion: 2\ntitle: P\nstatus: in-progress\ntype: epic\n---\n",
		"a--a.md": "---\nversion: 2\ntitle: A\nstatus: todo\ntype: task\nparent: p\norder: a0\n---\n\nSee #b for context.\n",
		"b--b.md": "---\nversion: 2\ntitle: B\nstatus: todo\ntype: task\nparent: p\norder: a1\n---\n\nSee #a for context.\n",
	}
	nibsDir := setupRelCobraTest(t, files)
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "p", "--rel", "children", "--order", "topo", "--json")
	env := decodeRelEnvelope(t, out)
	// No edges → stable insertion order = [a, b].
	if order := relEnvIDOrder(env); len(order) != 2 || order[0] != "a" || order[1] != "b" {
		t.Errorf("topo order = %v, want [a, b]", order)
	}
}

func TestRelCommand_Children_OrderTopo_BlockedByCycle_Errors(t *testing.T) {
	files := map[string]string{
		"p--p.md": "---\nversion: 2\ntitle: P\nstatus: in-progress\ntype: epic\n---\n",
		"a--a.md": "---\nversion: 2\ntitle: A\nstatus: todo\ntype: task\nparent: p\norder: a0\nblocked_by:\n  - b\n---\n",
		"b--b.md": "---\nversion: 2\ntitle: B\nstatus: todo\ntype: task\nparent: p\norder: a1\nblocked_by:\n  - a\n---\n",
	}
	nibsDir := setupRelCobraTest(t, files)
	_, err := runRelExpectError(t, "--nibs-path", nibsDir, "rel", "p", "--rel", "children", "--order", "topo")
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	if !errors.Is(err, errRelCycle) {
		t.Errorf("expected errRelCycle, got: %v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "[a, b]") && !strings.Contains(msg, "[b, a]") {
		t.Errorf("expected bracketed cycle members ([a, b] or [b, a]); got: %v", err)
	}
}

func TestRelCommand_Children_OrderTopo_ExternalBlockedByDropped(t *testing.T) {
	files := map[string]string{
		"p--p.md": "---\nversion: 2\ntitle: P\nstatus: in-progress\ntype: epic\n---\n",
		"a--a.md": "---\nversion: 2\ntitle: A\nstatus: todo\ntype: task\nparent: p\norder: a0\n---\n",
		"b--b.md": "---\nversion: 2\ntitle: B\nstatus: todo\ntype: task\nparent: p\norder: a1\nblocked_by:\n  - z\n---\n",
		// 'z' exists but is unrelated (not a child of p).
		"z--z.md": "---\nversion: 2\ntitle: Z\nstatus: todo\ntype: task\n---\n",
	}
	nibsDir := setupRelCobraTest(t, files)
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "p", "--rel", "children", "--order", "topo", "--json")
	env := decodeRelEnvelope(t, out)
	ids := relEnvIDs(env)
	if !ids["a"] || !ids["b"] || len(env.Nibs) != 2 {
		t.Errorf("expected both a and b present (external blocked_by dropped); got %v", relEnvIDOrder(env))
	}
}

func TestRelCommand_Children_OrderTopo_SelfBlockedByIgnored(t *testing.T) {
	files := map[string]string{
		"p--p.md": "---\nversion: 2\ntitle: P\nstatus: in-progress\ntype: epic\n---\n",
		// a lists itself plus a real dep b.
		"a--a.md": "---\nversion: 2\ntitle: A\nstatus: todo\ntype: task\nparent: p\norder: a0\nblocked_by:\n  - a\n  - b\n---\n",
		"b--b.md": "---\nversion: 2\ntitle: B\nstatus: todo\ntype: task\nparent: p\norder: a1\n---\n",
	}
	nibsDir := setupRelCobraTest(t, files)
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "p", "--rel", "children", "--order", "topo", "--json")
	env := decodeRelEnvelope(t, out)
	pos := map[string]int{}
	for i, id := range relEnvIDOrder(env) {
		pos[id] = i
	}
	// b must come before a (a.blocked_by=[a (self, dropped), b]).
	if pos["b"] >= pos["a"] {
		t.Errorf("topo order: b@%d, a@%d (want b before a — self-edge dropped, real dep on b kept)", pos["b"], pos["a"])
	}
}

// TestRelCommand_Children_OrderTopo_SkipsFilteredSibling pins that an edge
// whose source is filtered out of the candidate set is dropped, not collapsed
// into a synthetic edge between the survivors.
func TestRelCommand_Children_OrderTopo_SkipsFilteredSibling(t *testing.T) {
	files := map[string]string{
		"p--p.md": "---\nversion: 2\ntitle: P\nstatus: in-progress\ntype: epic\n---\n",
		"a--a.md": "---\nversion: 2\ntitle: A\nstatus: todo\ntype: task\nparent: p\norder: a0\n---\n",
		"b--b.md": "---\nversion: 2\ntitle: B\nstatus: completed\ntype: task\nparent: p\norder: a1\n---\n",
		"c--c.md": "---\nversion: 2\ntitle: C\nstatus: todo\ntype: task\nparent: p\norder: a2\nblocked_by:\n  - b\n---\n",
	}
	nibsDir := setupRelCobraTest(t, files)
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "p", "--rel", "children", "--order", "topo", "--open", "--json")
	env := decodeRelEnvelope(t, out)
	if env.Count != 2 {
		t.Fatalf("topo+open got %d items, want 2 (a, c; b filtered out)\nraw: %s", env.Count, out)
	}
	order := relEnvIDOrder(env)
	// c's edge to b is dropped (b not in candidate set), so order falls back
	// to stable insertion order [a, c] with no synthetic c→a constraint.
	if order[0] != "a" || order[1] != "c" {
		t.Errorf("topo order = %v, want [a, c] (any other order implies a synthetic edge from filtered-out b)", order)
	}
}

func TestRelCommand_Parent_OrderTopo_Errors(t *testing.T) {
	nibsDir := setupRelCobraTest(t, hierarchyFixture)
	_, err := runRelExpectError(t, "--nibs-path", nibsDir, "rel", "c1", "--rel", "parent", "--order", "topo")
	if err == nil {
		t.Fatal("expected --order topo on parent to error, got nil")
	}
	if !errors.Is(err, errRelOrderInapplicable) {
		t.Errorf("expected errRelOrderInapplicable, got: %v", err)
	}
}

// --- Transitive + --depth ---

func TestRelCommand_Ancestors_Depth2(t *testing.T) {
	nibsDir := setupRelCobraTest(t, ancestryFixture)
	// grand's ancestors depth 2 → leaf, mid (not root), closest first.
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "grand", "--rel", "ancestors", "--depth", "2", "--json")
	env := decodeRelEnvelope(t, out)
	order := relEnvIDOrder(env)
	if len(order) != 2 || order[0] != "leaf" || order[1] != "mid" {
		t.Errorf("ancestors depth=2 order = %v, want [leaf, mid]", order)
	}
}

// TestRelCommand_Ancestors_DanglingLinkEndsChain pins that a parent link naming
// no nib ends the ancestor chain at that rung rather than being skipped over or
// failing the command — the resolved-parent rule, applied by the same walk the
// hierarchy filters use.
func TestRelCommand_Ancestors_DanglingLinkEndsChain(t *testing.T) {
	files := map[string]string{
		"root--r.md": "---\nversion: 2\ntitle: Root\nstatus: in-progress\ntype: milestone\n---\n",
		// Hand-edited: mid's parent names a nib that does not exist, so the
		// chain from leaf must stop at mid and never reach root.
		"mid--m.md":  "---\nversion: 2\ntitle: Mid\nstatus: in-progress\ntype: epic\nparent: ghost\norder: a0\n---\n",
		"leaf--l.md": "---\nversion: 2\ntitle: Leaf\nstatus: todo\ntype: task\nparent: mid\norder: a0\n---\n",
	}
	nibsDir := setupRelCobraTest(t, files)
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "leaf", "--rel", "ancestors", "--depth", "all", "--json")
	env := decodeRelEnvelope(t, out)
	order := relEnvIDOrder(env)
	if len(order) != 1 || order[0] != "mid" {
		t.Errorf("ancestors of leaf = %v, want [mid] (the chain ends at the dangling link)", order)
	}
}

// TestRelCommand_Ancestors_CycleTerminates pins that a hand-edited parent cycle
// terminates: the walk stops at the first rung it has already visited, and the
// starting nib is never reported as its own ancestor.
func TestRelCommand_Ancestors_CycleTerminates(t *testing.T) {
	files := map[string]string{
		"a--a.md": "---\nversion: 2\ntitle: A\nstatus: todo\ntype: task\nparent: b\norder: a0\n---\n",
		"b--b.md": "---\nversion: 2\ntitle: B\nstatus: todo\ntype: task\nparent: a\norder: a0\n---\n",
	}
	nibsDir := setupRelCobraTest(t, files)
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "a", "--rel", "ancestors", "--depth", "all", "--json")
	env := decodeRelEnvelope(t, out)
	order := relEnvIDOrder(env)
	if len(order) != 1 || order[0] != "b" {
		t.Errorf("ancestors of a = %v, want [b] (the walk stops when it reaches a again)", order)
	}
}

func TestRelCommand_Descendants_DepthAll(t *testing.T) {
	nibsDir := setupRelCobraTest(t, ancestryFixture)
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "root", "--rel", "descendants", "--depth", "all", "--json")
	env := decodeRelEnvelope(t, out)
	ids := relEnvIDs(env)
	for _, want := range []string{"mid", "leaf", "grand"} {
		if !ids[want] {
			t.Errorf("descendants missing %q; got %v", want, ids)
		}
	}
}

// TestRelCommand_Descendants_StatusFilter_MatchesSubtree pins match-only
// semantics on a transitive rel: the filter selects which reached nodes are
// emitted, but never gates traversal — a matching node behind a non-matching
// intermediate is still returned.
func TestRelCommand_Descendants_StatusFilter_MatchesSubtree(t *testing.T) {
	files := map[string]string{
		"root--r.md": "---\nversion: 2\ntitle: Root\nstatus: todo\ntype: epic\n---\n",
		"mid--m.md":  "---\nversion: 2\ntitle: Mid\nstatus: in-progress\ntype: task\nparent: root\norder: a0\n---\n",
		"leaf--l.md": "---\nversion: 2\ntitle: Leaf\nstatus: todo\ntype: task\nparent: mid\norder: a0\n---\n",
	}
	nibsDir := setupRelCobraTest(t, files)
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "root", "--rel", "descendants", "--status", "todo", "--depth", "all", "--json")
	env := decodeRelEnvelope(t, out)
	ids := relEnvIDs(env)
	if !ids["leaf"] {
		t.Errorf("leaf (todo) should be reached through mid and matched by --status todo; got %v", ids)
	}
	if ids["mid"] {
		t.Errorf("mid (in-progress) should be excluded by --status todo; got %v", ids)
	}
}

// TestRelCommand_Descendants_TypeFilter_MatchesUnderNonMatchingIntermediate is
// the canonical footgun case: a bug nested under an epic (which fails -t bug).
// descendants -t bug must return the bug regardless of the intermediate's type.
func TestRelCommand_Descendants_TypeFilter_MatchesUnderNonMatchingIntermediate(t *testing.T) {
	files := map[string]string{
		"root--r.md": "---\nversion: 2\ntitle: Root\nstatus: todo\ntype: milestone\n---\n",
		"epic--e.md": "---\nversion: 2\ntitle: Epic\nstatus: todo\ntype: epic\nparent: root\norder: a0\n---\n",
		"bug--b.md":  "---\nversion: 2\ntitle: Bug\nstatus: todo\ntype: bug\nparent: epic\norder: a0\n---\n",
	}
	nibsDir := setupRelCobraTest(t, files)
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "root", "--rel", "descendants", "--type", "bug", "--depth", "all", "--json")
	env := decodeRelEnvelope(t, out)
	ids := relEnvIDs(env)
	if !ids["bug"] {
		t.Errorf("bug nested under a non-bug epic must be returned by descendants -t bug; got %v", ids)
	}
	if ids["epic"] || ids["root"] {
		t.Errorf("only the bug should match -t bug; got %v", ids)
	}
}

// TestRelCommand_Descendants_Depth_CountsStructuralHops pins that --depth counts
// structural edges, not matching hops: with match-only traversal a deep match is
// out of range at a shallow depth even though nearer nodes were filtered out.
func TestRelCommand_Descendants_Depth_CountsStructuralHops(t *testing.T) {
	files := map[string]string{
		"root--r.md": "---\nversion: 2\ntitle: Root\nstatus: todo\ntype: milestone\n---\n",
		"epic--e.md": "---\nversion: 2\ntitle: Epic\nstatus: todo\ntype: epic\nparent: root\norder: a0\n---\n",
		"bug--b.md":  "---\nversion: 2\ntitle: Bug\nstatus: todo\ntype: bug\nparent: epic\norder: a0\n---\n",
	}
	nibsDir := setupRelCobraTest(t, files)
	// bug is 2 structural hops down (root→epic→bug); depth 1 must not reach it.
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "root", "--rel", "descendants", "--type", "bug", "--depth", "1", "--json")
	env := decodeRelEnvelope(t, out)
	if ids := relEnvIDs(env); ids["bug"] {
		t.Errorf("bug at structural depth 2 should be out of range at --depth 1; got %v", ids)
	}
}

func TestRelCommand_BlockersTransitive_DepthAll(t *testing.T) {
	// chain: a ← b ← c (a blocked_by b, b blocked_by c).
	files := map[string]string{
		"a--a.md": "---\nversion: 2\ntitle: A\nstatus: todo\ntype: task\nblocked_by: [b]\n---\n",
		"b--b.md": "---\nversion: 2\ntitle: B\nstatus: todo\ntype: task\nblocked_by: [c]\n---\n",
		"c--c.md": "---\nversion: 2\ntitle: C\nstatus: todo\ntype: task\n---\n",
	}
	nibsDir := setupRelCobraTest(t, files)
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "a", "--rel", "blockers-transitive", "--depth", "all", "--json")
	env := decodeRelEnvelope(t, out)
	ids := relEnvIDs(env)
	if !ids["b"] || !ids["c"] {
		t.Errorf("blockers-transitive got %v, want {b, c}", ids)
	}
}

func TestRelCommand_MentionsOutTransitive_Depth2(t *testing.T) {
	// a mentions b; b mentions c; c mentions d.
	files := map[string]string{
		"a--a.md": "---\nversion: 2\ntitle: A\nstatus: todo\ntype: task\n---\n\nRefs #b.\n",
		"b--b.md": "---\nversion: 2\ntitle: B\nstatus: todo\ntype: task\n---\n\nRefs #c.\n",
		"c--c.md": "---\nversion: 2\ntitle: C\nstatus: todo\ntype: task\n---\n\nRefs #d.\n",
		"d--d.md": "---\nversion: 2\ntitle: D\nstatus: todo\ntype: task\n---\n",
	}
	nibsDir := setupRelCobraTest(t, files)
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "a", "--rel", "mentions-out-transitive", "--depth", "2", "--json")
	env := decodeRelEnvelope(t, out)
	ids := relEnvIDs(env)
	if !ids["b"] || !ids["c"] {
		t.Errorf("expected b and c in mentions-out-transitive depth=2; got %v", ids)
	}
	if ids["d"] {
		t.Errorf("d should NOT appear at depth=2; got %v", ids)
	}
}

func TestRelCommand_DirectRel_WithDepth_Errors(t *testing.T) {
	nibsDir := setupRelCobraTest(t, hierarchyFixture)
	_, err := runRelExpectError(t, "--nibs-path", nibsDir, "rel", "p1", "--rel", "children", "--depth", "3")
	if err == nil {
		t.Fatal("expected error for --rel children --depth 3, got nil")
	}
	if !errors.Is(err, errRelDepthInapplicable) {
		t.Errorf("expected errRelDepthInapplicable, got: %v", err)
	}
}

func TestRelCommand_Depth_TrailingGarbage_Errors(t *testing.T) {
	nibsDir := setupRelCobraTest(t, ancestryFixture)
	cases := []string{"3abc", "1.5", "-5", "3 foo", "2,3"}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			_, err := runRelExpectError(t, "--nibs-path", nibsDir, "rel", "grand", "--rel", "ancestors", "--depth", raw)
			if err == nil {
				t.Fatalf("expected error for --depth %q, got nil", raw)
			}
			if !errors.Is(err, errRelInvalidDepth) {
				t.Errorf("expected errRelInvalidDepth for %q, got: %v", raw, err)
			}
		})
	}
}

// --- Filters ---

func TestRelCommand_Children_StatusAndTypeFilter(t *testing.T) {
	files := map[string]string{
		"p1--parent.md": "---\nversion: 2\ntitle: Parent\nstatus: in-progress\ntype: epic\n---\n",
		"c1--task1.md":  "---\nversion: 2\ntitle: T1\nstatus: todo\ntype: task\nparent: p1\norder: a0\n---\n",
		"c2--task2.md":  "---\nversion: 2\ntitle: T2\nstatus: in-progress\ntype: task\nparent: p1\norder: a1\n---\n",
		"c3--bug1.md":   "---\nversion: 2\ntitle: B1\nstatus: todo\ntype: bug\nparent: p1\norder: a2\n---\n",
	}
	nibsDir := setupRelCobraTest(t, files)
	// --status todo --type bug → c3 only.
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "p1", "--rel", "children", "--status", "todo", "--type", "bug", "--json")
	env := decodeRelEnvelope(t, out)
	if env.Count != 1 || relEnvIDOrder(env)[0] != "c3" {
		t.Errorf("filtered children got %v, want [c3]", relEnvIDOrder(env))
	}
}

func TestRelCommand_Parent_WithFilter_Errors(t *testing.T) {
	nibsDir := setupRelCobraTest(t, hierarchyFixture)
	_, err := runRelExpectError(t, "--nibs-path", nibsDir, "rel", "c1", "--rel", "parent", "--status", "todo")
	if err == nil {
		t.Fatal("expected error for --rel parent with --status, got nil")
	}
	if !errors.Is(err, errRelFilterInapplicable) {
		t.Errorf("expected errRelFilterInapplicable, got: %v", err)
	}
}

func TestRelCommand_Siblings_OpenAndType(t *testing.T) {
	files := map[string]string{
		"p1--parent.md": "---\nversion: 2\ntitle: Parent\nstatus: in-progress\ntype: epic\n---\n",
		"c1--task1.md":  "---\nversion: 2\ntitle: T1\nstatus: todo\ntype: task\nparent: p1\norder: a0\n---\n",
		"c2--task2.md":  "---\nversion: 2\ntitle: T2\nstatus: todo\ntype: task\nparent: p1\norder: a1\n---\n",
		"c3--bug1.md":   "---\nversion: 2\ntitle: B1\nstatus: todo\ntype: bug\nparent: p1\norder: a2\n---\n",
		"c4--task3.md":  "---\nversion: 2\ntitle: T3\nstatus: completed\ntype: task\nparent: p1\norder: a3\n---\n",
	}
	nibsDir := setupRelCobraTest(t, files)
	// c1's siblings with --open --type task → c2 only (c3 wrong type, c4 completed).
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "c1", "--rel", "siblings", "--open", "--type", "task", "--json")
	env := decodeRelEnvelope(t, out)
	if env.Count != 1 || relEnvIDOrder(env)[0] != "c2" {
		t.Errorf("siblings with --open --type task got %v, want [c2]", relEnvIDOrder(env))
	}
}

func TestRelCommand_MentionsOut_NoPriorityExclusion(t *testing.T) {
	files := map[string]string{
		"a1--alpha.md": "---\nversion: 2\ntitle: Alpha\nstatus: todo\ntype: task\n---\n\nSee #b2 and #c3 and #d4.\n",
		"b2--beta.md":  "---\nversion: 2\ntitle: Beta\nstatus: todo\ntype: task\npriority: high\n---\n",
		"c3--gamma.md": "---\nversion: 2\ntitle: Gamma\nstatus: todo\ntype: task\npriority: normal\n---\n",
		"d4--delta.md": "---\nversion: 2\ntitle: Delta\nstatus: todo\ntype: task\npriority: low\n---\n",
	}
	nibsDir := setupRelCobraTest(t, files)
	// --no-priority low → excludes d4; b2 and c3 remain.
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "a1", "--rel", "mentions-out", "--no-priority", "low", "--json")
	env := decodeRelEnvelope(t, out)
	ids := relEnvIDs(env)
	if ids["d4"] {
		t.Errorf("d4 (low) should be excluded; got %v", ids)
	}
	if !ids["b2"] || !ids["c3"] {
		t.Errorf("expected b2 and c3 present; got %v", ids)
	}
}

// TestRelCommand_Neighbours_WithFilter_DropsFilterOnParent pins the meta-rel
// asymmetry: with neighbours + a filter, the filter applies to non-singular
// constituents but is silently dropped for the singular constituent (parent),
// so the parent still appears in the union.
func TestRelCommand_Neighbours_WithFilter_DropsFilterOnParent(t *testing.T) {
	files := map[string]string{
		"parent--p.md": "---\nversion: 2\ntitle: P\nstatus: completed\ntype: epic\n---\n",
		"child--c.md":  "---\nversion: 2\ntitle: C\nstatus: todo\ntype: task\nparent: parent\n---\n",
	}
	nibsDir := setupRelCobraTest(t, files)
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "child", "--rel", "neighbours", "--status", "todo", "--json")
	env := decodeRelEnvelope(t, out)
	if !relEnvIDs(env)["parent"] {
		t.Errorf("parent (completed) should still appear under neighbours+filter (filter dropped for singular); got %v", relEnvIDOrder(env))
	}
}

// --- Projection + output shape ---

// TestRelCommand_Projection_FieldsAppliesToRelated proves -f projects the
// RELATED nibs (not the source): only the requested keys are present.
func TestRelCommand_Projection_FieldsAppliesToRelated(t *testing.T) {
	nibsDir := setupRelCobraTest(t, hierarchyFixture)
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "p1", "--rel", "children", "-f", "id,status", "--json")
	env := decodeRelEnvelope(t, out)
	if len(env.Nibs) == 0 {
		t.Fatalf("expected projected children; got none\nraw: %s", out)
	}
	for _, n := range env.Nibs {
		keys := make([]string, 0, len(n))
		for k := range n {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(keys) != 2 || keys[0] != "id" || keys[1] != "status" {
			t.Errorf("projected keys = %v, want [id status]", keys)
		}
	}
}

// TestRelCommand_DefaultView_Ref pins that the default projection is the ref
// tier (id, title, status, type, priority) when neither --view nor -f is given.
func TestRelCommand_DefaultView_Ref(t *testing.T) {
	nibsDir := setupRelCobraTest(t, hierarchyFixture)
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "p1", "--rel", "children", "--json")
	env := decodeRelEnvelope(t, out)
	if len(env.Nibs) == 0 {
		t.Fatalf("expected projected children; got none\nraw: %s", out)
	}
	for _, want := range []string{"id", "title", "status", "type", "priority"} {
		if _, ok := env.Nibs[0][want]; !ok {
			t.Errorf("default view (ref) should include %q; got keys %v", want, env.Nibs[0])
		}
	}
	// ref does not include body.
	if _, ok := env.Nibs[0]["body"]; ok {
		t.Errorf("ref view should not include body; got %v", env.Nibs[0])
	}
}

// TestRelCommand_TSV_DefaultHeader checks the default (non-JSON) output is TSV
// under a "# <n> nibs" header.
func TestRelCommand_TSV_DefaultHeader(t *testing.T) {
	nibsDir := setupRelCobraTest(t, hierarchyFixture)
	// --all keeps all 3 children (c3 is completed) so the header count is stable.
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "p1", "--rel", "children", "-f", "id,status", "--all")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if lines[0] != "# 3 nibs" {
		t.Errorf("header = %q, want %q\nraw:\n%s", lines[0], "# 3 nibs", out)
	}
	// Body rows are tab-separated id\tstatus.
	if !strings.Contains(out, "\t") {
		t.Errorf("expected tab-separated rows; got:\n%s", out)
	}
}

func TestRelCommand_NoHeader_DropsHeader(t *testing.T) {
	nibsDir := setupRelCobraTest(t, hierarchyFixture)
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "p1", "--rel", "children", "-f", "id", "--no-header")
	if strings.Contains(out, "# ") {
		t.Errorf("--no-header should drop the comment header; got:\n%s", out)
	}
}

// TestRelCommand_JSON_EnvelopeKeysMatchList is the byte-shape parity acceptance
// criterion: rel --json and list --json share the identical top-level envelope
// keys ({nibs, count, truncated}). Both use --all so the optional hidden_closed
// key is absent — this pins the base envelope shape; hidden_closed presence is
// covered in the open-default tests.
func TestRelCommand_JSON_EnvelopeKeysMatchList(t *testing.T) {
	nibsDir := setupRelCobraTest(t, hierarchyFixture)

	relOut := runRelJSON(t, "--nibs-path", nibsDir, "rel", "c1", "--rel", "siblings", "--all", "--json")
	resetRelFlags()
	listOut := runRelJSON(t, "--nibs-path", nibsDir, "list", "--all", "--json")

	relKeys := topLevelKeys(t, relOut)
	listKeys := topLevelKeys(t, listOut)
	if !stringSliceEqual(relKeys, listKeys) {
		t.Errorf("top-level envelope keys differ:\n rel:  %v\n list: %v", relKeys, listKeys)
	}
	// And the concrete contract.
	if want := []string{"count", "nibs", "truncated"}; !stringSliceEqual(relKeys, want) {
		t.Errorf("rel envelope keys = %v, want %v", relKeys, want)
	}
}

func TestRelCommand_Limit_SetsTruncated(t *testing.T) {
	nibsDir := setupRelCobraTest(t, hierarchyFixture)
	// p1 has 3 children; --limit 1 projects one and marks truncated.
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "p1", "--rel", "children", "--limit", "1", "--json")
	env := decodeRelEnvelope(t, out)
	if env.Count != 1 {
		t.Errorf("count = %d, want 1 (post-limit size)", env.Count)
	}
	if !env.Truncated {
		t.Errorf("truncated = false, want true (limit dropped rows)")
	}
}

// --- Alias + retired commands ---

func TestRelCommand_LinksAlias_Works(t *testing.T) {
	nibsDir := setupRelCobraTest(t, hierarchyFixture)
	// The `links` alias must resolve to the rel command. --all keeps all 3
	// children (c3 is completed) so the count is stable (rel is open-by-default).
	out := runRelJSON(t, "--nibs-path", nibsDir, "links", "p1", "--rel", "children", "--all", "--json")
	env := decodeRelEnvelope(t, out)
	if env.Count != 3 {
		t.Errorf("links alias children count = %d, want 3\nraw: %s", env.Count, out)
	}
}

func TestRefsCommand_Retired(t *testing.T) {
	nibsDir := setupRelCobraTest(t, relFixture)
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
	nibsDir := setupRelCobraTest(t, relFixture)
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

// --- Error paths ---

func TestRelCommand_UnknownRel_Errors(t *testing.T) {
	nibsDir := setupRelCobraTest(t, relFixture)
	_, err := runRelExpectError(t, "--nibs-path", nibsDir, "rel", "a1", "--rel", "bogus")
	if err == nil {
		t.Fatal("expected error for unknown rel, got nil")
	}
	if !errors.Is(err, errRelInvalidRel) {
		t.Errorf("expected errRelInvalidRel, got: %v", err)
	}
	if !strings.Contains(err.Error(), "accepted:") {
		t.Errorf("error should include 'accepted:' list of valid rels; got: %v", err)
	}
}

func TestRelCommand_BadFields_Errors(t *testing.T) {
	nibsDir := setupRelCobraTest(t, hierarchyFixture)
	// A bad -f is a VALIDATION error surfaced before any traversal.
	_, err := runRelExpectError(t, "--nibs-path", nibsDir, "rel", "p1", "--rel", "children", "-f", "bogus-field")
	if err == nil {
		t.Fatal("expected error for bad -f, got nil")
	}
	var ce *output.CodedError
	if !errors.As(err, &ce) || ce.Code != output.ErrValidation {
		t.Errorf("expected VALIDATION coded error, got: %v", err)
	}
}

func TestRelCommand_NibNotFound_Errors(t *testing.T) {
	nibsDir := setupRelCobraTest(t, relFixture)
	_, err := runRelExpectError(t, "--nibs-path", nibsDir, "rel", "does-not-exist", "--rel", "children")
	if err == nil {
		t.Fatal("expected error for unknown nib id, got nil")
	}
	if !errors.Is(err, errRelNotFound) {
		t.Errorf("expected errRelNotFound, got: %v", err)
	}
}

func TestRelCommand_NibNotFound_JSON(t *testing.T) {
	nibsDir := setupRelCobraTest(t, relFixture)
	out, err := runRelExpectError(t, "--nibs-path", nibsDir, "rel", "does-not-exist", "--rel", "children", "--json")
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

// TestRelCommand_JSONMode_FailingCommand_NoUsageNoAutoError pins the error
// boundary contract in --json mode (mirrors the list/get contract).
func TestRelCommand_JSONMode_FailingCommand_NoUsageNoAutoError(t *testing.T) {
	nibsDir := setupRelCobraTest(t, relFixture)

	var cobraStdout, cobraStderr bytes.Buffer
	rootCmd.SetOut(&cobraStdout)
	rootCmd.SetErr(&cobraStderr)
	t.Cleanup(func() { rootCmd.SetOut(nil); rootCmd.SetErr(nil) })

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "rel", "bogus-id", "--rel", "children", "--json"})

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

	if got := cobraStdout.String(); strings.Contains(got, "Usage:") || strings.Contains(got, "Error:") {
		t.Errorf("rootCmd OutOrStdout contains Usage:/Error:; got:\n%s", got)
	}
	if got := cobraStderr.String(); strings.Contains(got, "Usage:") || strings.Contains(got, "Error:") {
		t.Errorf("rootCmd OutOrStderr contains Usage:/Error:; got:\n%s", got)
	}
}

func TestRelCommand_TextMode_FailingCommand_NoUsageNoAutoError(t *testing.T) {
	nibsDir := setupRelCobraTest(t, relFixture)

	var cobraStdout, cobraStderr bytes.Buffer
	rootCmd.SetOut(&cobraStdout)
	rootCmd.SetErr(&cobraStderr)
	t.Cleanup(func() { rootCmd.SetOut(nil); rootCmd.SetErr(nil) })

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "rel", "bogus-id", "--rel", "children"})

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

	if strings.Contains(stdout, "Usage:") {
		t.Errorf("captured stdout contains Usage: block; got:\n%s", stdout)
	}
	if got := cobraStdout.String(); strings.Contains(got, "Usage:") || strings.Contains(got, "Error:") {
		t.Errorf("rootCmd OutOrStdout contains Usage:/Error: in text mode; got:\n%s", got)
	}
	if got := cobraStderr.String(); strings.Contains(got, "Usage:") || strings.Contains(got, "Error:") {
		t.Errorf("rootCmd OutOrStderr contains Usage:/Error: in text mode; got:\n%s", got)
	}
}

// TestBfsTraverseReportsRefusedFilter pins the eighth ApplyFilter call site —
// the one inside the rel traversal — against swallowing a refused filter.
//
// No rel flag populates an id-valued filter field today (buildNibFilter fills
// only the metadata facets), so this is not reachable from the command line and
// is exercised directly. That is the reason to write it rather than to skip it:
// the day a rel flag gains one, a swallow here would hand back a plausible
// subtree listing for a target that does not exist, and nothing else in the
// suite would notice.
func TestBfsTraverseReportsRefusedFilter(t *testing.T) {
	resolver, core := setupParentLinkTest(t, map[string]string{
		"nibs-par": "",
		"nibs-chi": "parent: nibs-par\n",
	})

	parent := mustGet(t, core, "nibs-par")
	unknown := "nonexistent"
	got, err := bfsDescendants(context.Background(), resolver,
		parent, &model.NibFilter{ParentID: &unknown}, -1)
	if err == nil {
		t.Fatalf("returned %d nibs and no error; the traversal swallowed the refusal", len(got))
	}
	if !errors.Is(err, nib.ErrNotFound) {
		t.Errorf("error does not carry nib.ErrNotFound: %v", err)
	}
}

// TestRelFetchErrCodeClassifiesFilterRefusals pins the exit code the traversal's
// error sites give a refused filter. bfsTraverse propagating the refusal (above)
// is only half of it: RunE sees an opaque error, and the code it picks is what
// an agent branching on $? reads as "no such nib" (3) versus "the tracker broke"
// (5). Reporting a mistyped id as a file error is the miscategorization the
// error classes exist to remove, so the fallback must be consulted last.
//
// The rows are wrapped exactly as the call sites wrap them, because a classifier
// that only worked on the bare error would pass an unwrapped table and still
// misclassify everything RunE actually hands it.
func TestRelFetchErrCodeClassifiesFilterRefusals(t *testing.T) {
	notFound := &graph.FilterTargetNotFoundError{Field: "parentId", ID: "nonexistent"}
	unreadable := &graph.FilterTargetUnreadableError{Field: "siblingId", ID: "nibs-a", ReaderErr: nib.ErrNotFound}
	empty := &graph.FilterTargetEmptyError{Field: "parentId"}

	tests := []struct {
		name string
		err  error
		want string
	}{
		{"unknown filter target", notFound, output.ErrNotFound},
		{"unknown filter target, wrapped by the fetch site", fmt.Errorf("fetching descendants: %w", notFound), output.ErrNotFound},
		{"target that vanished mid-filter", unreadable, output.ErrFileError},
		{"target that vanished mid-filter, wrapped", fmt.Errorf("fetching siblings: %w", unreadable), output.ErrFileError},
		// An empty id is the caller's malformed input, so it is exit 2 like
		// every other bad argument — not the exit 5 the fallback would give it,
		// which claims the tracker broke.
		{"empty filter target", empty, output.ErrValidation},
		{"empty filter target, wrapped by the fetch site", fmt.Errorf("fetching descendants: %w", empty), output.ErrValidation},
		{"dependency cycle", fmt.Errorf("%w detected: a, b", errRelCycle), output.ErrValidation},
		{"anything else", errors.New("reading nibs directory"), output.ErrFileError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := relFetchErrCode(tt.err); got != tt.want {
				t.Errorf("relFetchErrCode(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

// topLevelKeys returns the sorted top-level object keys of a JSON document.
func topLevelKeys(t *testing.T, raw string) []string {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("unmarshal top-level object: %v\nraw: %s", err, raw)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
