package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/spf13/pflag"
)

// setupDepsTest mirrors setupPlanTest but is duplicated here so each command
// keeps its fixtures local. The prefix is empty — tests use bare IDs like
// "epic-1" so they don't need to work around configured prefix matching.
func setupDepsTest(t *testing.T) (*graph.Resolver, func(id, title, status, typ, parent, order, body string) *nib.Nib) {
	t.Helper()
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	core := nibcore.New(nibsDir, cfg)
	if err := core.Load(); err != nil {
		t.Fatal(err)
	}
	resolver := &graph.Resolver{
		Reader:    core,
		Writer:    core,
		Validator: core,
		Blocking:  core,
		Orderer:   graph.NewOrderer(core, core),
	}
	create := func(id, title, status, typ, parent, order, body string) *nib.Nib {
		t.Helper()
		b := &nib.Nib{
			ID:     id,
			Slug:   nib.Slugify(title),
			Title:  title,
			Status: status,
			Type:   typ,
			Parent: parent,
			Order:  order,
			Body:   body,
		}
		if err := core.Create(b); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
		return b
	}
	return resolver, create
}

func TestBuildDeps_IndependentChildrenRetainSiblingOrder(t *testing.T) {
	resolver, create := setupDepsTest(t)

	// No mentions among children — topo sort must fall back to the
	// sibling order returned by resolver.Orderer.GetSortedSiblings.
	create("epic-1", "Parent", "in-progress", "epic", "", "", "")
	create("x", "X", "todo", "task", "epic-1", "a0", "")
	create("y", "Y", "todo", "task", "epic-1", "a1", "")
	create("z", "Z", "todo", "task", "epic-1", "a2", "")

	result, err := buildDeps(resolver, "epic-1", false, false)
	if err != nil {
		t.Fatalf("buildDeps: %v", err)
	}
	wantIDs := []string{"x", "y", "z"}
	if len(result.Items) != len(wantIDs) {
		t.Fatalf("items len = %d, want %d", len(result.Items), len(wantIDs))
	}
	for i, item := range result.Items {
		if item.ID != wantIDs[i] {
			t.Errorf("item[%d].ID = %q, want %q", i, item.ID, wantIDs[i])
		}
		if len(item.DependsOn) != 0 {
			t.Errorf("item[%d].DependsOn = %v, want empty", i, item.DependsOn)
		}
	}
}

func TestBuildDeps_ExternalMentionRecorded(t *testing.T) {
	resolver, create := setupDepsTest(t)

	// ext-99 lives outside the subtree. a's body mentions #ext-99 → this
	// appears in ExternalDeps and does NOT affect the internal sort or
	// a's DependsOn list.
	create("epic-1", "Parent", "in-progress", "epic", "", "", "")
	create("ext-99", "External", "todo", "task", "", "", "")
	create("a", "Alpha", "todo", "task", "epic-1", "a0", "needs #ext-99 work first")
	create("b", "Beta", "todo", "task", "epic-1", "a1", "")

	result, err := buildDeps(resolver, "epic-1", false, false)
	if err != nil {
		t.Fatalf("buildDeps: %v", err)
	}

	// Order within subtree unaffected (a, b keep sibling order).
	if len(result.Items) != 2 || result.Items[0].ID != "a" || result.Items[1].ID != "b" {
		t.Fatalf("items = %+v, want [a, b]", result.Items)
	}
	if len(result.Items[0].DependsOn) != 0 {
		t.Errorf("a.DependsOn = %v, want empty (external not in DependsOn)", result.Items[0].DependsOn)
	}

	if len(result.ExternalDeps) != 1 {
		t.Fatalf("ExternalDeps len = %d, want 1", len(result.ExternalDeps))
	}
	ed := result.ExternalDeps[0]
	if ed.From != "a" || ed.To != "ext-99" {
		t.Errorf("ExternalDeps[0] = %+v, want {From:a, To:ext-99}", ed)
	}
}

func TestBuildDeps_SelfMentionIgnored(t *testing.T) {
	resolver, create := setupDepsTest(t)

	create("epic-1", "Parent", "in-progress", "epic", "", "", "")
	create("solo", "Solo", "todo", "task", "epic-1", "a0", "refers to self via #solo shouldn't loop")

	result, err := buildDeps(resolver, "epic-1", false, false)
	if err != nil {
		t.Fatalf("buildDeps: %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].ID != "solo" {
		t.Fatalf("items = %+v, want [solo]", result.Items)
	}
	if len(result.Items[0].DependsOn) != 0 {
		t.Errorf("solo.DependsOn = %v, want empty (self-mention dropped)", result.Items[0].DependsOn)
	}
	if len(result.ExternalDeps) != 0 {
		t.Errorf("ExternalDeps = %v, want empty (self-mention is not external)", result.ExternalDeps)
	}
	if len(result.Cycles) != 0 {
		t.Errorf("Cycles = %v, want empty (self-mention must not create a cycle)", result.Cycles)
	}
}

func TestBuildDeps_ActiveFilter_ExcludesCompletedAndScrapped(t *testing.T) {
	resolver, create := setupDepsTest(t)

	// done mentions nothing but is a sibling. active1 mentions #done
	// (edge would drag active1 after done). With activeOnly=true, `done`
	// disappears from the subtree, so the edge drops and active1 is
	// neither reported as external nor forced to a later position.
	create("epic-1", "Parent", "in-progress", "epic", "", "", "")
	create("done", "Done Task", "completed", "task", "epic-1", "a0", "")
	create("scrap", "Scrapped Task", "scrapped", "task", "epic-1", "a1", "")
	create("active1", "Active 1", "todo", "task", "epic-1", "a2", "follows #done")
	create("active2", "Active 2", "in-progress", "task", "epic-1", "a3", "")

	result, err := buildDeps(resolver, "epic-1", true, false)
	if err != nil {
		t.Fatalf("buildDeps: %v", err)
	}

	wantIDs := []string{"active1", "active2"}
	if len(result.Items) != len(wantIDs) {
		t.Fatalf("items = %+v, want [active1, active2]", result.Items)
	}
	for i, item := range result.Items {
		if item.ID != wantIDs[i] {
			t.Errorf("item[%d].ID = %q, want %q", i, item.ID, wantIDs[i])
		}
		if len(item.DependsOn) != 0 {
			t.Errorf("item[%d].DependsOn = %v, want empty", i, item.DependsOn)
		}
	}

	// `done` was filtered out BEFORE edges were computed, so the mention
	// of #done must not surface as an external dep either — otherwise
	// consumers see completed siblings sneak back in via the "external"
	// list.
	if len(result.ExternalDeps) != 0 {
		t.Errorf("ExternalDeps = %v, want empty (filtered sibling must not leak as external)", result.ExternalDeps)
	}
}

func TestBuildDeps_CycleErrorsByDefault(t *testing.T) {
	resolver, create := setupDepsTest(t)

	create("epic-1", "Parent", "in-progress", "epic", "", "", "")
	create("p", "P", "todo", "task", "epic-1", "a0", "needs #q")
	create("q", "Q", "todo", "task", "epic-1", "a1", "needs #p")

	_, err := buildDeps(resolver, "epic-1", false, false)
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	// Classification is via errors.Is so it can't drift when the message
	// wording changes. Pairs with the sentinel-based RunE classifier in
	// deps.go.
	if !errors.Is(err, errDepsCycle) {
		t.Errorf("expected errors.Is(err, errDepsCycle), got: %v", err)
	}
	// Member listing: assert the bracketed format, which only appears if the
	// node names are actually emitted (a bare letter match could be fooled
	// by substrings in the wording like "dependency").
	msg := err.Error()
	if !strings.Contains(msg, "[p, q]") && !strings.Contains(msg, "[p,") && !strings.Contains(msg, ", q]") {
		t.Errorf("error should list cycle members in bracketed form (e.g. '[p, q]'), got: %q", msg)
	}
}

func TestBuildDeps_CycleModeReportsSeparately(t *testing.T) {
	resolver, create := setupDepsTest(t)

	create("epic-1", "Parent", "in-progress", "epic", "", "", "")
	// Cycle: p ↔ q. Independent: r has no mentions.
	create("p", "P", "todo", "task", "epic-1", "a0", "needs #q")
	create("q", "Q", "todo", "task", "epic-1", "a1", "needs #p")
	create("r", "R", "todo", "task", "epic-1", "a2", "")

	result, err := buildDeps(resolver, "epic-1", false, true)
	if err != nil {
		t.Fatalf("buildDeps cyclesMode=true: %v", err)
	}
	// r is cycle-free and must still be present in Items.
	if len(result.Items) != 1 || result.Items[0].ID != "r" {
		t.Fatalf("Items = %+v, want [r] (cycle members omitted)", result.Items)
	}
	if result.Items[0].Position != 1 {
		t.Errorf("r.Position = %d, want 1 (cycle-free nodes should be 1-indexed over themselves)", result.Items[0].Position)
	}

	if len(result.Cycles) != 1 {
		t.Fatalf("Cycles len = %d, want 1", len(result.Cycles))
	}
	gotNodes := map[string]string{}
	for _, n := range result.Cycles[0].Nodes {
		gotNodes[n.ID] = n.Title
	}
	if _, ok := gotNodes["p"]; !ok {
		t.Errorf("Cycles[0].Nodes = %+v, want to contain p", result.Cycles[0].Nodes)
	}
	if _, ok := gotNodes["q"]; !ok {
		t.Errorf("Cycles[0].Nodes = %+v, want to contain q", result.Cycles[0].Nodes)
	}
	if gotNodes["p"] != "P" || gotNodes["q"] != "Q" {
		t.Errorf("cycle node titles = %+v, want p→P, q→Q (renderers rely on these)", gotNodes)
	}
}

func TestBuildDeps_ParentNotFound(t *testing.T) {
	resolver, _ := setupDepsTest(t)
	_, err := buildDeps(resolver, "does-not-exist", false, false)
	if err == nil {
		t.Fatal("expected not-found error, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("expected error to mention the missing id, got: %q", err.Error())
	}
}

func TestBuildDeps_NoChildren(t *testing.T) {
	resolver, create := setupDepsTest(t)
	create("epic-1", "Empty Parent", "todo", "epic", "", "", "")

	result, err := buildDeps(resolver, "epic-1", false, false)
	if err != nil {
		t.Fatalf("buildDeps on empty parent: %v", err)
	}
	if result.Items == nil {
		t.Error("Items should be empty slice, not nil (for stable JSON shape)")
	}
	if len(result.Items) != 0 {
		t.Errorf("Items = %v, want empty", result.Items)
	}
	if result.ExternalDeps == nil {
		t.Error("ExternalDeps should be empty slice, not nil (for stable JSON shape)")
	}
	if len(result.ExternalDeps) != 0 {
		t.Errorf("ExternalDeps = %v, want empty", result.ExternalDeps)
	}
	if len(result.Cycles) != 0 {
		t.Errorf("Cycles = %v, want empty", result.Cycles)
	}
}

func TestBuildDeps_LinearChain(t *testing.T) {
	resolver, create := setupDepsTest(t)

	// parent epic with three children. b's body mentions #a; c's body
	// mentions #b. Expected topo order: a, b, c — even if the raw
	// sibling order would differ.
	create("epic-1", "Parent", "in-progress", "epic", "", "", "")
	create("a", "Alpha", "todo", "task", "epic-1", "a0", "no refs")
	create("b", "Beta", "todo", "task", "epic-1", "a1", "depends on #a for layout")
	create("c", "Gamma", "todo", "task", "epic-1", "a2", "blocked on #b finishing first")

	result, err := buildDeps(resolver, "epic-1", false, false)
	if err != nil {
		t.Fatalf("buildDeps: %v", err)
	}
	if result == nil || len(result.Items) != 3 {
		t.Fatalf("items = %+v, want 3 entries", result)
	}

	wantIDs := []string{"a", "b", "c"}
	wantDeps := [][]string{nil, {"a"}, {"b"}}
	for i, item := range result.Items {
		if item.Position != i+1 {
			t.Errorf("item[%d].Position = %d, want %d", i, item.Position, i+1)
		}
		if item.ID != wantIDs[i] {
			t.Errorf("item[%d].ID = %q, want %q", i, item.ID, wantIDs[i])
		}
		if len(item.DependsOn) != len(wantDeps[i]) {
			t.Errorf("item[%d].DependsOn = %v, want %v", i, item.DependsOn, wantDeps[i])
			continue
		}
		for j, dep := range item.DependsOn {
			if dep != wantDeps[i][j] {
				t.Errorf("item[%d].DependsOn[%d] = %q, want %q", i, j, dep, wantDeps[i][j])
			}
		}
	}
}

// resetDepsFlags clears the package-level flag vars used by depsCmd so
// tests don't pollute each other via rootCmd's singleton state. Mirrors
// resetRefsFlags — see its doc for rationale.
func resetDepsFlags() {
	depsJSON = false
	depsActive = false
	depsCycles = false
	depsGraph = ""
	depsCmd.Flags().Visit(func(f *pflag.Flag) {
		f.Changed = false
	})
}

// setupDepsCobraTest writes nib files to disk and returns the .nibs dir so
// `rootCmd.SetArgs(["--nibs-path", dir, "deps", ...])` drives the full
// Cobra + PersistentPreRunE pipeline, matching setupRefsCobraTest's style.
func setupDepsCobraTest(t *testing.T, files map[string]string) string {
	t.Helper()
	t.Cleanup(resetDepsFlags)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	resetDepsFlags()

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

// depsSubtreeFixture creates a small epic with three ordered children and
// one external nib for the CLI tests. File names follow the `{id}--{slug}.md`
// convention nibcore expects.
func depsSubtreeFixture() map[string]string {
	return map[string]string{
		"epic-1--parent.md": "---\ntitle: Parent\nstatus: in-progress\ntype: epic\n---\n\nEpic body.\n",
		"a--alpha.md":       "---\ntitle: Alpha\nstatus: todo\ntype: task\nparent: epic-1\norder: a0\n---\n\nNo refs.\n",
		"b--beta.md":        "---\ntitle: Beta\nstatus: todo\ntype: task\nparent: epic-1\norder: a1\n---\n\nDepends on #a.\n",
		"c--gamma.md":       "---\ntitle: Gamma\nstatus: todo\ntype: task\nparent: epic-1\norder: a2\n---\n\nDepends on #b.\n",
	}
}

func TestDepsCommand_JSONOutput(t *testing.T) {
	nibsDir := setupDepsCobraTest(t, depsSubtreeFixture())

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "deps", "epic-1", "--json"})
	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("deps --json: %v\nraw: %s", execErr, out)
	}

	var got DepsResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}
	if got.Parent.ID != "epic-1" {
		t.Errorf("parent.id = %q, want epic-1", got.Parent.ID)
	}
	if len(got.Items) != 3 {
		t.Fatalf("items len = %d, want 3", len(got.Items))
	}
	wantIDs := []string{"a", "b", "c"}
	for i, it := range got.Items {
		if it.ID != wantIDs[i] {
			t.Errorf("items[%d].id = %q, want %q", i, it.ID, wantIDs[i])
		}
		if it.DependsOn == nil {
			t.Errorf("items[%d].depends_on = nil, want non-null slice", i)
		}
	}

	// Stable shape: every empty slice must appear as [] in JSON. This
	// guards consumers against having to special-case `null`.
	if !strings.Contains(out, `"external_deps": []`) {
		t.Errorf("expected `\"external_deps\": []` in output, got:\n%s", out)
	}
	// items[0] has no deps — should serialize as [].
	if !strings.Contains(out, `"depends_on": []`) {
		t.Errorf("expected at least one `\"depends_on\": []` (for the root of the chain), got:\n%s", out)
	}
	// Cycles is omitempty under `--cycles` being off, so no cycles key
	// should appear when there are none.
	if strings.Contains(out, `"cycles"`) {
		t.Errorf("expected no 'cycles' key when cyclesMode off and no cycles, got:\n%s", out)
	}
}

func TestDepsCommand_GraphMermaid(t *testing.T) {
	nibsDir := setupDepsCobraTest(t, depsSubtreeFixture())

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "deps", "epic-1", "--graph", "mermaid"})
	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stdout)
	defer rootCmd.SetOut(nil)
	defer rootCmd.SetErr(nil)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("deps --graph mermaid: %v", err)
	}
	out := stdout.String()
	if !strings.HasPrefix(strings.TrimSpace(out), "graph TD") {
		t.Errorf("expected 'graph TD' header, got:\n%s", out)
	}
	// One node line per child, with the title as a quoted label.
	for _, id := range []string{"a", "b", "c"} {
		if !strings.Contains(out, id+"[") {
			t.Errorf("expected node line for %q (%q[...]), got:\n%s", id, id, out)
		}
	}
	// Edges: b depends on a → `a --> b`; c depends on b → `b --> c`.
	if !strings.Contains(out, "a --> b") {
		t.Errorf("expected edge 'a --> b', got:\n%s", out)
	}
	if !strings.Contains(out, "b --> c") {
		t.Errorf("expected edge 'b --> c', got:\n%s", out)
	}
}

func TestDepsCommand_GraphDOT(t *testing.T) {
	// Includes a quote in a title to pin the label-quoting behavior —
	// %q in Go's fmt emits `"say \"hi\""`, which is valid DOT.
	files := map[string]string{
		"epic-1--parent.md": "---\ntitle: Parent\nstatus: in-progress\ntype: epic\n---\n\nEpic body.\n",
		"a--alpha.md":       "---\ntitle: 'Alpha \"quoted\"'\nstatus: todo\ntype: task\nparent: epic-1\norder: a0\n---\n\nNo refs.\n",
		"b--beta.md":        "---\ntitle: Beta\nstatus: todo\ntype: task\nparent: epic-1\norder: a1\n---\n\nDepends on #a.\n",
	}
	nibsDir := setupDepsCobraTest(t, files)

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "deps", "epic-1", "--graph", "dot"})
	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stdout)
	defer rootCmd.SetOut(nil)
	defer rootCmd.SetErr(nil)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("deps --graph dot: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "digraph G {") {
		t.Errorf("expected 'digraph G {' header, got:\n%s", out)
	}
	if !strings.Contains(out, "}") {
		t.Errorf("expected closing '}' for digraph, got:\n%s", out)
	}
	// The edge for b depends on a.
	if !strings.Contains(out, `"a" -> "b";`) {
		t.Errorf("expected edge '\"a\" -> \"b\";' in DOT output, got:\n%s", out)
	}
	// Quoted title round-trip: Go's %q escapes the inner quotes so DOT
	// sees `"Alpha \"quoted\""` — a valid quoted label. We just check
	// the escaped form survives.
	if !strings.Contains(out, `Alpha \"quoted\"`) {
		t.Errorf("expected escaped quoted title in DOT output, got:\n%s", out)
	}
}

func TestDepsCommand_JsonGraphMutex(t *testing.T) {
	// --json and --graph are conceptually different output modes and must
	// not be combined silently. The RunE handler should fail-fast with a
	// validation error before loading the nib. Mirrors refs --both/--inbound.
	nibsDir := setupDepsCobraTest(t, depsSubtreeFixture())

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "deps", "epic-1", "--json", "--graph", "mermaid"})
	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stdout)
	defer rootCmd.SetOut(nil)
	defer rootCmd.SetErr(nil)

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected validation error for --json + --graph, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("expected 'mutually exclusive' in error, got: %v", err)
	}
}

func TestDepsCommand_GraphMermaid_QuoteEscape(t *testing.T) {
	// Mermaid does NOT accept backslash-escaped quotes inside labels —
	// it expects HTML entities (`#quot;`). Using %q would produce broken
	// output for any title containing a double quote. This test pins the
	// mermaidEscape helper so future refactors can't silently regress.
	files := map[string]string{
		"epic-1--parent.md": "---\ntitle: Parent\nstatus: in-progress\ntype: epic\n---\n\nEpic body.\n",
		"a--alpha.md":       "---\ntitle: 'Alpha \"quoted\"'\nstatus: todo\ntype: task\nparent: epic-1\norder: a0\n---\n\nNo refs.\n",
	}
	nibsDir := setupDepsCobraTest(t, files)

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "deps", "epic-1", "--graph", "mermaid"})
	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stdout)
	defer rootCmd.SetOut(nil)
	defer rootCmd.SetErr(nil)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("deps --graph mermaid: %v", err)
	}
	out := stdout.String()
	// The escaped form `#quot;` must appear; the backslash-escaped form `\"`
	// must NOT (it would indicate %q sneaked back in).
	if !strings.Contains(out, "#quot;") {
		t.Errorf("expected `#quot;` in Mermaid output for quoted title, got:\n%s", out)
	}
	if strings.Contains(out, `\"`) {
		t.Errorf("expected NO backslash-escaped quotes in Mermaid output (use HTML entity), got:\n%s", out)
	}
}

// depsCycleFixture creates a parent epic with a p↔q cycle and an independent
// sibling r. Shared by the cycle-mode renderer tests (findings #9 and #10).
func depsCycleFixture() map[string]string {
	return map[string]string{
		"epic-1--parent.md": "---\ntitle: Parent\nstatus: in-progress\ntype: epic\n---\n\nEpic body.\n",
		"p--pea.md":         "---\ntitle: Pea\nstatus: todo\ntype: task\nparent: epic-1\norder: a0\n---\n\nneeds #q\n",
		"q--queen.md":       "---\ntitle: Queen\nstatus: todo\ntype: task\nparent: epic-1\norder: a1\n---\n\nneeds #p\n",
		"r--rook.md":        "---\ntitle: Rook\nstatus: todo\ntype: task\nparent: epic-1\norder: a2\n---\n\n",
	}
}

// runDepsCommand executes the deps command with the given args and returns
// stdout. It keeps the per-test boilerplate for capturing output and
// restoring rootCmd's I/O in one place.
func runDepsCommand(t *testing.T, args []string) string {
	t.Helper()
	rootCmd.SetArgs(args)
	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stdout)
	t.Cleanup(func() { rootCmd.SetOut(nil); rootCmd.SetErr(nil) })
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("deps: %v\nraw: %s", err, stdout.String())
	}
	return stdout.String()
}

func TestDepsCommand_GraphMermaid_WithCycles(t *testing.T) {
	// Cycle-only nodes (p, q) must render with their titles as labels —
	// not with the ID as a fallback. The independent sibling r appears
	// in the regular Items flow. Output must be deterministic across runs.
	nibsDir := setupDepsCobraTest(t, depsCycleFixture())

	out1 := runDepsCommand(t, []string{"--nibs-path", nibsDir, "deps", "epic-1", "--cycles", "--graph", "mermaid"})

	if !strings.HasPrefix(strings.TrimSpace(out1), "graph TD") {
		t.Errorf("expected 'graph TD' header, got:\n%s", out1)
	}
	// Cycle-only node labels carry titles, not IDs.
	if !strings.Contains(out1, `p["Pea"]`) {
		t.Errorf(`expected 'p["Pea"]' (cycle node with title label), got:\n%s`, out1)
	}
	if !strings.Contains(out1, `q["Queen"]`) {
		t.Errorf(`expected 'q["Queen"]' (cycle node with title label), got:\n%s`, out1)
	}
	// Independent sibling shows up in Items.
	if !strings.Contains(out1, `r["Rook"]`) {
		t.Errorf(`expected 'r["Rook"]' (non-cycle sibling), got:\n%s`, out1)
	}

	// Determinism: a second invocation must produce byte-identical output.
	// Reset Cobra state between runs so flags don't pollute the rerun.
	resetDepsFlags()
	nibsDir2 := setupDepsCobraTest(t, depsCycleFixture())
	out2 := runDepsCommand(t, []string{"--nibs-path", nibsDir2, "deps", "epic-1", "--cycles", "--graph", "mermaid"})
	if out1 != out2 {
		t.Errorf("Mermaid cycle output not deterministic:\n---run 1---\n%s\n---run 2---\n%s", out1, out2)
	}
}

func TestDepsCommand_GraphDOT_WithCycles(t *testing.T) {
	// Mirror of the Mermaid test — cycle-only nodes must carry their
	// actual titles, not ID-as-label. Determinism is checked across runs.
	nibsDir := setupDepsCobraTest(t, depsCycleFixture())

	out1 := runDepsCommand(t, []string{"--nibs-path", nibsDir, "deps", "epic-1", "--cycles", "--graph", "dot"})

	if !strings.Contains(out1, "digraph G {") {
		t.Errorf("expected 'digraph G {' header, got:\n%s", out1)
	}
	// Cycle-only node labels carry titles.
	if !strings.Contains(out1, `"p" [label="Pea"]`) {
		t.Errorf(`expected '"p" [label="Pea"]' (cycle node with title), got:\n%s`, out1)
	}
	if !strings.Contains(out1, `"q" [label="Queen"]`) {
		t.Errorf(`expected '"q" [label="Queen"]' (cycle node with title), got:\n%s`, out1)
	}
	// Non-cycle sibling.
	if !strings.Contains(out1, `"r" [label="Rook"]`) {
		t.Errorf(`expected '"r" [label="Rook"]' (non-cycle sibling), got:\n%s`, out1)
	}

	// Determinism.
	resetDepsFlags()
	nibsDir2 := setupDepsCobraTest(t, depsCycleFixture())
	out2 := runDepsCommand(t, []string{"--nibs-path", nibsDir2, "deps", "epic-1", "--cycles", "--graph", "dot"})
	if out1 != out2 {
		t.Errorf("DOT cycle output not deterministic:\n---run 1---\n%s\n---run 2---\n%s", out1, out2)
	}
}

func TestDepsCommand_GraphInvalidValue(t *testing.T) {
	nibsDir := setupDepsCobraTest(t, depsSubtreeFixture())

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "deps", "epic-1", "--graph", "svg"})
	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stdout)
	defer rootCmd.SetOut(nil)
	defer rootCmd.SetErr(nil)

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected validation error for unknown --graph value, got nil")
	}
	if !strings.Contains(err.Error(), "mermaid") || !strings.Contains(err.Error(), "dot") {
		t.Errorf("expected error to name valid values, got: %v", err)
	}
}
