package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/spf13/pflag"
)

// setupPlanTestStore builds the same empty store setupPlanTest does but hands
// back the Core and the store directory, so a test can create a nib carrying
// fields the positional create helper does not take (milestone,
// milestone_order) and can read the bytes a nib was persisted as.
func setupPlanTestStore(t *testing.T) (*graph.Resolver, *nibcore.Core, string) {
	t.Helper()
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(storeDataDir(nibsDir), 0755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	core := nibcore.New(nibsDir, cfg)
	if err := core.Load(); err != nil {
		t.Fatal(err)
	}
	return &graph.Resolver{Reader: core, Writer: core, Validator: core, Blocking: core, Orderer: graph.NewOrderer(core, core)}, core, nibsDir
}

// createInStore persists b, filling in the slug the way the create helper does.
func createInStore(t *testing.T, core *nibcore.Core, b *nib.Nib) *nib.Nib {
	t.Helper()
	if b.Slug == "" {
		b.Slug = nib.Slugify(b.Title)
	}
	if err := core.Create(b); err != nil {
		t.Fatalf("failed to create nib %s: %v", b.ID, err)
	}
	return b
}

func setupPlanTest(t *testing.T) (*graph.Resolver, func(id, title, status, typ, parent, order, body string) *nib.Nib) {
	t.Helper()
	resolver, core, _ := setupPlanTestStore(t)

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
			t.Fatalf("failed to create nib %s: %v", id, err)
		}
		return b
	}

	return resolver, create
}

func TestBuildPlan_BasicDisplay(t *testing.T) {
	resolver, create := setupPlanTest(t)

	// Create parent epic
	create("epic-1", "My Epic", "in-progress", "epic", "", "", "Epic body")

	// Create children in specific order
	create("task-1", "First Task", "completed", "task", "epic-1", "a0", "")
	create("task-2", "Second Task", "in-progress", "task", "epic-1", "a1", "")
	create("task-3", "Third Task", "todo", "task", "epic-1", "a2", "")

	plan, err := buildPlan(context.Background(), resolver, "epic-1", false)
	if err != nil {
		t.Fatalf("buildPlan error: %v", err)
	}

	// Verify parent info
	if plan.Parent.ID != "epic-1" {
		t.Errorf("parent ID = %q, want %q", plan.Parent.ID, "epic-1")
	}
	if plan.Parent.Title != "My Epic" {
		t.Errorf("parent title = %q, want %q", plan.Parent.Title, "My Epic")
	}
	if plan.Parent.Status != "in-progress" {
		t.Errorf("parent status = %q, want %q", plan.Parent.Status, "in-progress")
	}
	if plan.Parent.Type != "epic" {
		t.Errorf("parent type = %q, want %q", plan.Parent.Type, "epic")
	}

	// Verify children count and order
	if len(plan.Items) != 3 {
		t.Fatalf("items count = %d, want 3", len(plan.Items))
	}

	// Verify positions are 1-indexed and sequential
	for i, item := range plan.Items {
		if item.Position != i+1 {
			t.Errorf("item[%d].Position = %d, want %d", i, item.Position, i+1)
		}
	}

	// Verify ordering matches order keys
	wantTitles := []string{"First Task", "Second Task", "Third Task"}
	for i, item := range plan.Items {
		if item.Title != wantTitles[i] {
			t.Errorf("item[%d].Title = %q, want %q", i, item.Title, wantTitles[i])
		}
	}

	// Verify status and type are populated
	wantStatuses := []string{"completed", "in-progress", "todo"}
	for i, item := range plan.Items {
		if item.Status != wantStatuses[i] {
			t.Errorf("item[%d].Status = %q, want %q", i, item.Status, wantStatuses[i])
		}
		if item.Type != "task" {
			t.Errorf("item[%d].Type = %q, want %q", i, item.Type, "task")
		}
	}
}

func TestBuildPlan_AcceptanceRollup(t *testing.T) {
	resolver, create := setupPlanTest(t)

	create("epic-1", "My Epic", "in-progress", "epic", "", "", "")

	// A "## Acceptance Criteria" section with 3 boxes, 1 checked. The boxes in
	// other sections must NOT count toward the acceptance rollup.
	bodyWithAC := `Some intro text.

## Acceptance Criteria

- [ ] First criterion
- [x] Second criterion
- [ ] Third criterion

## Other Section

- [x] Not an acceptance box
`
	create("task-1", "Task With AC", "todo", "task", "epic-1", "a0", bodyWithAC)

	plan, err := buildPlan(context.Background(), resolver, "epic-1", false)
	if err != nil {
		t.Fatalf("buildPlan error: %v", err)
	}

	if len(plan.Items) != 1 {
		t.Fatalf("items count = %d, want 1", len(plan.Items))
	}

	got := plan.Items[0].Acceptance
	if got == nil {
		t.Fatal("Acceptance rollup should not be nil")
	}
	if got.Total != 3 {
		t.Errorf("Acceptance.Total = %d, want 3 (only boxes in the Acceptance section)", got.Total)
	}
	if got.Checked != 1 {
		t.Errorf("Acceptance.Checked = %d, want 1", got.Checked)
	}
}

// TestBuildPlan_AcceptanceHeadingVariants pins that both the current
// "### Acceptance" body-template heading and the older "## Acceptance Criteria"
// heading are recognized when counting checkboxes.
func TestBuildPlan_AcceptanceHeadingVariants(t *testing.T) {
	resolver, create := setupPlanTest(t)

	create("epic-1", "My Epic", "in-progress", "epic", "", "", "")
	create("task-1", "Level-3 Acceptance", "todo", "task", "epic-1", "a0",
		"### Acceptance\n\n- [x] one\n- [ ] two\n")
	create("task-2", "Criteria Heading", "todo", "task", "epic-1", "a1",
		"## Acceptance Criteria\n\n- [x] done\n")

	plan, err := buildPlan(context.Background(), resolver, "epic-1", false)
	if err != nil {
		t.Fatalf("buildPlan error: %v", err)
	}

	if got := plan.Items[0].Acceptance; got == nil || got.Checked != 1 || got.Total != 2 {
		t.Errorf("item[0].Acceptance = %+v, want {Checked:1, Total:2}", got)
	}
	if got := plan.Items[1].Acceptance; got == nil || got.Checked != 1 || got.Total != 1 {
		t.Errorf("item[1].Acceptance = %+v, want {Checked:1, Total:1}", got)
	}
}

func TestBuildPlan_JSONOutput(t *testing.T) {
	resolver, create := setupPlanTest(t)

	create("epic-1", "Release v1", "in-progress", "epic", "", "", "")
	create("task-1", "Do Thing", "todo", "task", "epic-1", "a0", "## Acceptance Criteria\n\n- [ ] Works\n")

	plan, err := buildPlan(context.Background(), resolver, "epic-1", false)
	if err != nil {
		t.Fatalf("buildPlan error: %v", err)
	}

	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	// Verify it round-trips through JSON correctly
	var got Plan
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}

	// Verify parent fields are present in JSON
	if got.Parent.ID != "epic-1" {
		t.Errorf("JSON parent.id = %q, want %q", got.Parent.ID, "epic-1")
	}
	if got.Parent.Title != "Release v1" {
		t.Errorf("JSON parent.title = %q, want %q", got.Parent.Title, "Release v1")
	}

	// Verify items
	if len(got.Items) != 1 {
		t.Fatalf("JSON items count = %d, want 1", len(got.Items))
	}
	if got.Items[0].Position != 1 {
		t.Errorf("JSON items[0].position = %d, want 1", got.Items[0].Position)
	}
	if got.Items[0].ID != "task-1" {
		t.Errorf("JSON items[0].id = %q, want %q", got.Items[0].ID, "task-1")
	}
	if got.Items[0].Acceptance == nil || got.Items[0].Acceptance.Total != 1 {
		t.Errorf("JSON items[0].acceptance = %+v, want {Checked:0, Total:1}", got.Items[0].Acceptance)
	}

	// Verify the acceptance rollup serializes as a nested object.
	raw := string(data)
	if !strings.Contains(raw, `"acceptance":{"checked":0,"total":1}`) {
		t.Errorf("JSON should carry acceptance rollup object, got: %s", raw)
	}
	if !strings.Contains(raw, `"position"`) {
		t.Errorf("JSON should contain 'position' field, got: %s", raw)
	}
}

func TestBuildPlan_EmptyChildren(t *testing.T) {
	resolver, create := setupPlanTest(t)

	create("epic-1", "Empty Epic", "todo", "epic", "", "", "")

	plan, err := buildPlan(context.Background(), resolver, "epic-1", false)
	if err != nil {
		t.Fatalf("buildPlan error: %v", err)
	}

	if plan.Parent.ID != "epic-1" {
		t.Errorf("parent ID = %q, want %q", plan.Parent.ID, "epic-1")
	}

	// Items should be empty slice (not nil) for clean JSON output
	if plan.Items == nil {
		t.Error("Items should be empty slice, not nil")
	}
	if len(plan.Items) != 0 {
		t.Errorf("Items count = %d, want 0", len(plan.Items))
	}
}

func TestBuildPlan_MissingAcceptanceSection(t *testing.T) {
	resolver, create := setupPlanTest(t)

	create("epic-1", "My Epic", "in-progress", "epic", "", "", "")

	// One child with an Acceptance section, two without.
	create("task-1", "Has AC", "todo", "task", "epic-1", "a0", "## Acceptance Criteria\n\n- [ ] Do it\n")
	create("task-2", "No AC", "todo", "task", "epic-1", "a1", "Just a plain description.\n")
	create("task-3", "Empty Body", "todo", "task", "epic-1", "a2", "")

	plan, err := buildPlan(context.Background(), resolver, "epic-1", false)
	if err != nil {
		t.Fatalf("buildPlan error: %v", err)
	}

	if len(plan.Items) != 3 {
		t.Fatalf("items count = %d, want 3", len(plan.Items))
	}

	// First item should carry an acceptance rollup.
	if plan.Items[0].Acceptance == nil {
		t.Error("item[0] should have an acceptance rollup")
	}

	// Items without an Acceptance section report nil (omitted in JSON).
	if plan.Items[1].Acceptance != nil {
		t.Errorf("item[1].Acceptance = %+v, want nil", plan.Items[1].Acceptance)
	}
	if plan.Items[2].Acceptance != nil {
		t.Errorf("item[2].Acceptance = %+v, want nil", plan.Items[2].Acceptance)
	}

	// Verify JSON omits the acceptance key for the two items without a section.
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}
	raw := string(data)
	if count := strings.Count(raw, `"acceptance"`); count != 1 {
		t.Errorf("'acceptance' should appear once in JSON (for item with a section), appeared %d times", count)
	}
}

func TestBuildPlan_OrderAlwaysIncludedInJSON(t *testing.T) {
	resolver, create := setupPlanTest(t)

	create("epic-1", "My Epic", "in-progress", "epic", "", "", "")
	create("task-1", "First", "todo", "task", "epic-1", "a0", "")
	create("task-2", "Second", "todo", "task", "epic-1", "a1", "")
	create("task-3", "Third", "todo", "task", "epic-1", "a2", "")

	plan, err := buildPlan(context.Background(), resolver, "epic-1", false)
	if err != nil {
		t.Fatalf("buildPlan error: %v", err)
	}

	// Each PlanItem.Order should match the source nib's order key.
	wantOrders := []string{"a0", "a1", "a2"}
	if len(plan.Items) != len(wantOrders) {
		t.Fatalf("items count = %d, want %d", len(plan.Items), len(wantOrders))
	}
	for i, item := range plan.Items {
		if item.Order != wantOrders[i] {
			t.Errorf("plan.Items[%d].Order = %q, want %q", i, item.Order, wantOrders[i])
		}
	}

	// JSON output must always include the order field, regardless of any
	// flag — that's the contract for agent consumers.
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	raw := string(data)
	if !strings.Contains(raw, `"order"`) {
		t.Errorf("JSON should contain 'order' field, got: %s", raw)
	}
	// Field appears once per item (3 items here).
	if got := strings.Count(raw, `"order":`); got != 3 {
		t.Errorf("JSON 'order' field should appear %d times (one per item), got %d: %s", 3, got, raw)
	}
	for _, want := range wantOrders {
		if !strings.Contains(raw, `"order":"`+want+`"`) {
			t.Errorf("JSON should contain order=%q, got: %s", want, raw)
		}
	}
}

func TestBuildPlan_ParentNotFound(t *testing.T) {
	resolver, _ := setupPlanTest(t)

	_, err := buildPlan(context.Background(), resolver, "nonexistent", false)
	if err == nil {
		t.Fatal("expected error for nonexistent parent, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should contain 'not found', got: %q", err.Error())
	}
}

func TestBuildPlan_ActiveFlag(t *testing.T) {
	resolver, create := setupPlanTest(t)

	create("epic-1", "My Epic", "in-progress", "epic", "", "", "")
	create("task-1", "Done Task", "completed", "task", "epic-1", "a0", "")
	create("task-2", "Active Task", "in-progress", "task", "epic-1", "a1", "")
	create("task-3", "Scrapped Task", "scrapped", "task", "epic-1", "a2", "")
	create("task-4", "Todo Task", "todo", "task", "epic-1", "a3", "")
	create("task-5", "Draft Task", "draft", "task", "epic-1", "a4", "")

	t.Run("activeOnly=false shows all", func(t *testing.T) {
		plan, err := buildPlan(context.Background(), resolver, "epic-1", false)
		if err != nil {
			t.Fatalf("buildPlan error: %v", err)
		}
		if len(plan.Items) != 5 {
			t.Errorf("items count = %d, want 5", len(plan.Items))
		}
	})

	t.Run("activeOnly=true excludes completed and scrapped", func(t *testing.T) {
		plan, err := buildPlan(context.Background(), resolver, "epic-1", true)
		if err != nil {
			t.Fatalf("buildPlan error: %v", err)
		}
		if len(plan.Items) != 3 {
			t.Fatalf("items count = %d, want 3", len(plan.Items))
		}

		// Verify remaining items are the active ones
		wantTitles := []string{"Active Task", "Todo Task", "Draft Task"}
		for i, item := range plan.Items {
			if item.Title != wantTitles[i] {
				t.Errorf("item[%d].Title = %q, want %q", i, item.Title, wantTitles[i])
			}
		}

		// Verify positions are renumbered (1, 2, 3 not 2, 4, 5)
		for i, item := range plan.Items {
			if item.Position != i+1 {
				t.Errorf("item[%d].Position = %d, want %d", i, item.Position, i+1)
			}
		}
	})
}

// resetPlanFlags clears the package-level flag vars used by planCmd AND
// Cobra's Changed-state tracking so tests don't pollute each other via
// rootCmd's singleton state.
func resetPlanFlags() {
	planJSON = false
	planOpen = false
	planWithOrder = false
	planCmd.Flags().Visit(func(f *pflag.Flag) {
		f.Changed = false
	})
}

// setupPlanCobraTest writes nib files and returns the .nibs directory so
// `rootCmd.SetArgs(["--nibs-path", dir, "plan", ...])` can drive the full
// Cobra pipeline. Mirrors the contract of setupListCobraTest.
func setupPlanCobraTest(t *testing.T, files map[string]string) string {
	t.Helper()
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetPlanFlags)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	t.Cleanup(func() { rootCmd.SetOut(nil); rootCmd.SetErr(nil) })
	resetPlanFlags()

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

// planFixtureFiles returns a small set of nib files: one parent epic and
// three children with known order keys (a0, a1, a2). Reused across the
// human-output Cobra tests. Filenames use the modern `id--slug.md` format
// so ParseFilename treats the prefix segment (e.g. `e1`, `t1`) as the ID.
func planFixtureFiles() map[string]string {
	return map[string]string{
		"e1--my-epic.md":     "---\nversion: 2\ntitle: My Epic\nstatus: in-progress\ntype: epic\n---\n",
		"t1--first-task.md":  "---\nversion: 2\ntitle: First Task\nstatus: todo\ntype: task\nparent: e1\norder: a0\n---\n",
		"t2--second-task.md": "---\nversion: 2\ntitle: Second Task\nstatus: in-progress\ntype: task\nparent: e1\norder: a1\n---\n",
		"t3--third-task.md":  "---\nversion: 2\ntitle: Third Task\nstatus: completed\ntype: task\nparent: e1\norder: a2\n---\n",
	}
}

func TestPlanCommand_WithOrder_HumanOutputShowsOrderKeys(t *testing.T) {
	nibsDir := setupPlanCobraTest(t, planFixtureFiles())

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "plan", "e1", "--with-order"})

	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("rootCmd.Execute: %v", execErr)
	}

	// Each row should carry the `order=<key>` suffix in addition to the
	// existing `(id)` suffix.
	wantLines := []string{
		"1. [todo] First Task (t1) order=a0",
		"2. [in-progress] Second Task (t2) order=a1",
		"3. [completed] Third Task (t3) order=a2",
	}
	for _, want := range wantLines {
		if !strings.Contains(out, want) {
			t.Errorf("human output missing line %q\nfull output:\n%s", want, out)
		}
	}
}

func TestPlanCommand_JSON_FlagDoesNotChangeShape(t *testing.T) {
	// Contract pin: --with-order MUST NOT alter --json output. JSON always
	// includes the order field; the flag only governs the human renderer.
	nibsDir := setupPlanCobraTest(t, planFixtureFiles())

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "plan", "e1", "--json"})
	var err1 error
	out1 := captureStdout(t, func() { err1 = rootCmd.Execute() })
	if err1 != nil {
		t.Fatalf("first run: %v", err1)
	}

	// Reset between runs (Cobra holds parsed-flag state on rootCmd).
	resetPlanFlags()
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "plan", "e1", "--json", "--with-order"})
	var err2 error
	out2 := captureStdout(t, func() { err2 = rootCmd.Execute() })
	if err2 != nil {
		t.Fatalf("second run: %v", err2)
	}

	if out1 != out2 {
		t.Errorf("--with-order must not change --json output\n--json:\n%s\n--json --with-order:\n%s", out1, out2)
	}
}

// planOpenFixtureFiles returns a parent epic and one child per side of the
// --open filter: a todo child (open), a completed and a deferred child (both
// closed), and a child with no `status:` key at all — the case where plan's
// exclude-closed rule and list/rel's -s open include-list disagree.
func planOpenFixtureFiles() map[string]string {
	return map[string]string{
		"e1--my-epic.md":        "---\nversion: 2\ntitle: My Epic\nstatus: in-progress\ntype: epic\n---\n",
		"t1--todo-task.md":      "---\nversion: 2\ntitle: Todo Task\nstatus: todo\ntype: task\nparent: e1\norder: a0\n---\n",
		"t2--done-task.md":      "---\nversion: 2\ntitle: Done Task\nstatus: completed\ntype: task\nparent: e1\norder: a1\n---\n",
		"t3--deferred-task.md":  "---\nversion: 2\ntitle: Deferred Task\nstatus: deferred\ntype: task\nparent: e1\norder: a2\n---\n",
		"t4--no-status-task.md": "---\nversion: 2\ntitle: No Status Task\ntype: task\nparent: e1\norder: a3\n---\n",
	}
}

// TestPlanCommand_Open_ExcludesClosedChildren drives --open through Cobra
// rather than calling buildPlan with a bare bool, so it covers the wiring from
// the registered flag to buildPlan's openOnly parameter — an unbound flag
// leaves every child in the output and fails the closed-child assertions.
//
// It also pins the statusless child, which is where plan --open parts company
// with list/rel's --open: filterOpen removes only known-closed statuses, so ""
// survives here while -s open's include-list drops it. plan's --open usage
// string states that, and this is what holds it.
func TestPlanCommand_Open_ExcludesClosedChildren(t *testing.T) {
	nibsDir := setupPlanCobraTest(t, planOpenFixtureFiles())

	// Baseline: without --open every child is listed, so the "absent" checks
	// below are about the filter and not about a missing fixture nib.
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "plan", "e1"})
	var baseErr error
	baseOut := captureStdout(t, func() { baseErr = rootCmd.Execute() })
	if baseErr != nil {
		t.Fatalf("plan e1: %v", baseErr)
	}
	for _, want := range []string{"Todo Task", "Done Task", "Deferred Task", "No Status Task"} {
		if !strings.Contains(baseOut, want) {
			t.Fatalf("baseline output missing %q; fixture is wrong\nfull output:\n%s", want, baseOut)
		}
	}

	// Reset between runs (Cobra holds parsed-flag state on rootCmd).
	resetPlanFlags()
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "plan", "e1", "--open"})
	var openErr error
	openOut := captureStdout(t, func() { openErr = rootCmd.Execute() })
	if openErr != nil {
		t.Fatalf("plan e1 --open: %v", openErr)
	}

	// Positions are renumbered over the surviving children.
	wantLines := []string{
		"1. [todo] Todo Task (t1)",
	}
	for _, want := range wantLines {
		if !strings.Contains(openOut, want) {
			t.Errorf("plan --open output missing line %q\nfull output:\n%s", want, openOut)
		}
	}
	// "No Status Task" is excluded, and that is the behavior change: --open now
	// selects the open status *group* on plan, the same set list's --open
	// selects, rather than "everything not closed". A nib with no `status:`
	// holds "" and is in neither group, so it is not open. Before this it was
	// kept here and dropped by list, which made one flag name mean two
	// memberships — see TestOpenFlagAgreesBetweenListAndPlan.
	for _, unwanted := range []string{"Done Task", "Deferred Task", "No Status Task"} {
		if strings.Contains(openOut, unwanted) {
			t.Errorf("plan --open should exclude closed child %q\nfull output:\n%s", unwanted, openOut)
		}
	}
}

func TestPlanCommand_NoFlag_HumanOutputUnchanged(t *testing.T) {
	// Regression guard: without --with-order, human rows must NOT include
	// the `order=<key>` suffix. The default human format is
	// `N. [status] title (id)` with nothing after the closing paren.
	nibsDir := setupPlanCobraTest(t, planFixtureFiles())

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "plan", "e1"})

	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("rootCmd.Execute: %v", execErr)
	}

	// Sanity check: existing format still appears.
	wantLines := []string{
		"1. [todo] First Task (t1)",
		"2. [in-progress] Second Task (t2)",
		"3. [completed] Third Task (t3)",
	}
	for _, want := range wantLines {
		if !strings.Contains(out, want) {
			t.Errorf("human output missing line %q\nfull output:\n%s", want, out)
		}
	}

	// And the `order=` token must be absent entirely.
	if strings.Contains(out, "order=") {
		t.Errorf("human output should NOT contain 'order=' without --with-order, got:\n%s", out)
	}
}

// TestOpenFlagAgreesBetweenListAndPlan pins the two commands to one meaning of
// --open. They diverged on exactly one input — a nib whose front matter carries
// no `status:`, which holds "" and is therefore in neither the open nor the
// closed group: list's --open (an include-list) dropped it while plan's
// exclude-closed rule kept it.
//
// The statusless row is the whole test. On well-formed data the include-list and
// exclude-closed readings select identical sets, so nothing inside the declared
// vocabulary can tell the two implementations apart.
func TestOpenFlagAgreesBetweenListAndPlan(t *testing.T) {
	cfg := config.Default()

	statusless := &nib.Nib{ID: "zz00", Title: "Statusless", Type: "task"}
	open := &nib.Nib{ID: "op01", Title: "Open", Type: "task", Status: "todo"}
	closed := &nib.Nib{ID: "cl01", Title: "Closed", Type: "task", Status: "completed"}
	all := []*nib.Nib{statusless, open, closed}

	// What plan --open keeps.
	gotPlan := map[string]bool{}
	for _, b := range filterOpen(all, cfg) {
		gotPlan[b.ID] = true
	}

	// What list --open keeps: the open group, exactly as resolveStatusFilter
	// expands it for -s open.
	include, _, _, err := resolveStatusFilter(cfg, statusFilterInput{Open: true})
	if err != nil {
		t.Fatalf("resolveStatusFilter: %v", err)
	}
	inList := map[string]bool{}
	for _, name := range include {
		inList[name] = true
	}
	gotList := map[string]bool{}
	for _, b := range all {
		if inList[b.Status] {
			gotList[b.ID] = true
		}
	}

	if !reflect.DeepEqual(gotPlan, gotList) {
		t.Errorf("plan --open kept %v, list --open kept %v — one flag name, two memberships", gotPlan, gotList)
	}
	if gotPlan["zz00"] {
		t.Errorf("plan --open kept the statusless nib; the open group does not contain %q", "")
	}
	if !gotPlan["op01"] {
		t.Error("plan --open dropped an open nib — the filter is not merely agreeing by returning nothing")
	}
	if gotPlan["cl01"] {
		t.Error("plan --open kept a closed nib")
	}
}

// TestBuildPlan_MilestoneReportsItsQueue pins the axis a milestone's plan is
// read on: its queue in milestone_order, not the structural parent axis, which
// holds nothing for a well-formed milestone (nibtypes declares no valid child
// type for one). The queue keys reverse creation order, id order and title
// order alike, so a pass cannot come from any of the three — title order
// included, which is where nib.SortByKey falls back for unkeyed members.
// task-x is assigned to no milestone and belongs to no queue at all.
func TestBuildPlan_MilestoneReportsItsQueue(t *testing.T) {
	resolver, core, _ := setupPlanTestStore(t)

	createInStore(t, core, &nib.Nib{ID: "ms-1", Title: "v1 Launch", Status: "in-progress", Type: "milestone"})
	createInStore(t, core, &nib.Nib{ID: "epic-a", Title: "Alpha", Status: "todo", Type: "epic", Milestone: "ms-1", MilestoneOrder: "c"})
	createInStore(t, core, &nib.Nib{ID: "epic-b", Title: "Bravo", Status: "todo", Type: "epic", Milestone: "ms-1", MilestoneOrder: "b"})
	createInStore(t, core, &nib.Nib{ID: "epic-c", Title: "Charlie", Status: "todo", Type: "epic", Milestone: "ms-1", MilestoneOrder: "a"})
	createInStore(t, core, &nib.Nib{ID: "task-x", Title: "Unscheduled", Status: "todo", Type: "task"})

	plan, err := buildPlan(context.Background(), resolver, "ms-1", false)
	if err != nil {
		t.Fatalf("buildPlan error: %v", err)
	}

	if plan.Parent.Type != "milestone" {
		t.Fatalf("parent type = %q, want %q", plan.Parent.Type, "milestone")
	}

	wantIDs := []string{"epic-c", "epic-b", "epic-a"}
	wantOrders := []string{"a", "b", "c"}
	if len(plan.Items) != len(wantIDs) {
		t.Fatalf("items = %d, want %d (%v)", len(plan.Items), len(wantIDs), planItemIDs(plan))
	}
	for i, item := range plan.Items {
		if item.ID != wantIDs[i] {
			t.Errorf("items[%d].ID = %q, want %q", i, item.ID, wantIDs[i])
		}
		// The Order column must carry the key that actually placed the item in
		// this queue — milestone_order, not the structural `order:`.
		if item.Order != wantOrders[i] {
			t.Errorf("items[%d].Order = %q, want %q (the milestone_order key)", i, item.Order, wantOrders[i])
		}
		if item.Position != i+1 {
			t.Errorf("items[%d].Position = %d, want %d", i, item.Position, i+1)
		}
	}
}

// TestBuildPlan_MilestoneQueueOverridesStructuralChildren covers a milestone
// carrying BOTH a hand-authored structural child and a queue. The assignment
// axis is what schedules (membership.DirectMembers), so the queue is what plan
// reports and the structural child is absent unless it is also assigned.
func TestBuildPlan_MilestoneQueueOverridesStructuralChildren(t *testing.T) {
	resolver, core, _ := setupPlanTestStore(t)

	createInStore(t, core, &nib.Nib{ID: "ms-1", Title: "v1 Launch", Status: "in-progress", Type: "milestone"})
	// Hand-authored `parent: ms-1` with no assignment: decomposition data, not
	// a queue entry.
	createInStore(t, core, &nib.Nib{ID: "task-p", Title: "Nested By Parent", Status: "todo", Type: "task", Parent: "ms-1", Order: "a"})
	createInStore(t, core, &nib.Nib{ID: "epic-q", Title: "Queued", Status: "todo", Type: "epic", Milestone: "ms-1", MilestoneOrder: "a"})

	plan, err := buildPlan(context.Background(), resolver, "ms-1", false)
	if err != nil {
		t.Fatalf("buildPlan error: %v", err)
	}

	if got := planItemIDs(plan); !reflect.DeepEqual(got, []string{"epic-q"}) {
		t.Errorf("milestone plan = %v, want [epic-q] — the queue, not the structural children", got)
	}
}

// TestBuildPlan_StructuralAxisReportsMilestoneTypedChildren pins the parent
// axis as the UNFILTERED structural set, matching every other reader of it.
// membership.View.DirectMembers drops milestone-typed members on both axes,
// which is right for a queue and wrong here: `rel --rel children`, the web
// tree, the TUI, cmd/close.go and graph's reorder validation all read the
// unfiltered set, so a plan that hid such a child would both under-report and
// break the round trip — `mv --children-of` fed the ids plan printed is
// refused with "missing child in reorder list".
//
// The shape reaches released data through migration rather than hand-editing
// alone: nibcore's v2-axes step rewrites a `parent:` onto the assignment axis
// only when the parent is milestone-typed AND the child is not, so a milestone
// carrying `parent: <epic>` matches neither half and survives as valid v2.
func TestBuildPlan_StructuralAxisReportsMilestoneTypedChildren(t *testing.T) {
	resolver, core, _ := setupPlanTestStore(t)

	createInStore(t, core, &nib.Nib{ID: "epic-1", Title: "My Epic", Status: "in-progress", Type: "epic"})
	createInStore(t, core, &nib.Nib{ID: "ms-nested", Title: "Nested Milestone", Status: "todo", Type: "milestone", Parent: "epic-1", Order: "a"})
	createInStore(t, core, &nib.Nib{ID: "task-p", Title: "Plain Task", Status: "todo", Type: "task", Parent: "epic-1", Order: "b"})

	plan, err := buildPlan(context.Background(), resolver, "epic-1", false)
	if err != nil {
		t.Fatalf("buildPlan error: %v", err)
	}

	want := []string{"ms-nested", "task-p"}
	if got := planItemIDs(plan); !reflect.DeepEqual(got, want) {
		t.Fatalf("epic plan = %v, want %v — the structural axis carries every type", got, want)
	}
	if got := plan.Items[0].Type; got != "milestone" {
		t.Errorf("items[0].Type = %q, want %q — the item must name what it is", got, "milestone")
	}

	// The round trip the finding names: graph's reorder validation demands the
	// same set, so a plan the user pipes into `mv --children-of` must not be
	// missing any of it.
	var reorderIDs []string
	for _, b := range resolver.Orderer.Members(graph.ScopeParent, "epic-1") {
		reorderIDs = append(reorderIDs, b.ID)
	}
	if got := planItemIDs(plan); !reflect.DeepEqual(got, reorderIDs) {
		t.Errorf("plan reported %v but a reorder of epic-1 demands %v — `mv --children-of` fed plan's ids would be refused", got, reorderIDs)
	}
}

// TestBuildPlan_ReadsWithoutWriting pins plan as a pure question on BOTH
// axes: an unkeyed member is reported with an empty order key and its file is
// left byte-identical, rather than being backfilled to disk as a side effect
// of the read. That is graph.Next's rule (internal/graph/next.go) — a question
// must not edit files the caller never named, and must not fail differently on
// a read-only store — and the reason plan enumerates the member set from a
// membership.View rather than through Orderer.Members, which backfills.
//
// The comparison is over every file in the store's data directory, not the
// unkeyed member's alone: a backfill misdirected onto the keyed member or onto
// the container would otherwise pass unseen. It does not descend into
// subdirectories or read archive/, which these fixtures do not use — a case
// that needs either has to widen storeSnapshot first.
//
// Each case arranges its two members so the expected sequence [keyed, unkeyed]
// reverses their title order, their id order and their creation order alike,
// leaving nib.SortByKey's keyed-before-unkeyed rule as the only one that
// yields it. Input order is not among those rules — Core.All() ranges a map —
// so that sequence check is a supporting assertion; the byte comparison is
// what this test exists for.
func TestBuildPlan_ReadsWithoutWriting(t *testing.T) {
	tests := []struct {
		name      string
		container *nib.Nib
		keyed     *nib.Nib
		unkeyed   *nib.Nib
	}{
		{
			name:      "milestone queue",
			container: &nib.Nib{ID: "ms-1", Title: "v1 Launch", Status: "in-progress", Type: "milestone"},
			keyed:     &nib.Nib{ID: "epic-z", Title: "Zulu", Status: "todo", Type: "epic", Milestone: "ms-1", MilestoneOrder: "a"},
			unkeyed:   &nib.Nib{ID: "epic-a", Title: "Alpha", Status: "todo", Type: "epic", Milestone: "ms-1"},
		},
		{
			name:      "structural children",
			container: &nib.Nib{ID: "epic-1", Title: "My Epic", Status: "in-progress", Type: "epic"},
			keyed:     &nib.Nib{ID: "task-z", Title: "Zulu", Status: "todo", Type: "task", Parent: "epic-1", Order: "a"},
			unkeyed:   &nib.Nib{ID: "task-a", Title: "Alpha", Status: "todo", Type: "task", Parent: "epic-1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver, core, nibsDir := setupPlanTestStore(t)
			createInStore(t, core, tt.container)
			unkeyed := createInStore(t, core, tt.unkeyed)
			createInStore(t, core, tt.keyed)

			before := storeSnapshot(t, nibsDir)

			plan, err := buildPlan(context.Background(), resolver, tt.container.ID, false)
			if err != nil {
				t.Fatalf("buildPlan error: %v", err)
			}

			assertStoreUnchanged(t, before, storeSnapshot(t, nibsDir))

			// The store must not carry a phantom key either: a member the read
			// keyed in memory only would resurface on the next write.
			stored, err := core.Get(unkeyed.ID)
			if err != nil {
				t.Fatalf("core.Get: %v", err)
			}
			if stored.Order != "" || stored.MilestoneOrder != "" {
				t.Errorf("stored keys = order:%q milestone_order:%q, want both empty — plan must not key a member it merely read", stored.Order, stored.MilestoneOrder)
			}

			// The unkeyed member is still reported, last (nib.SortByKey puts
			// keyed members first) and with an empty order key.
			if got := planItemIDs(plan); !reflect.DeepEqual(got, []string{tt.keyed.ID, tt.unkeyed.ID}) {
				t.Fatalf("plan items = %v, want [%s %s]", got, tt.keyed.ID, tt.unkeyed.ID)
			}
			if got := plan.Items[1].Order; got != "" {
				t.Errorf("items[1].Order = %q, want \"\" — an unkeyed member carries no key on the axis plan read", got)
			}
		})
	}
}

// TestBuildPlan_AxisNamesTheFieldItRead pins the JSON discriminator: `order`
// carries a different front-matter field per container type, so the payload
// says which one. The value is the field's name in the project's own
// vocabulary (internal/projection), which is what lets an agent choose between
// `nibs mv --queue` and the default parent scope without encoding the
// milestone rule itself.
func TestBuildPlan_AxisNamesTheFieldItRead(t *testing.T) {
	resolver, core, _ := setupPlanTestStore(t)

	createInStore(t, core, &nib.Nib{ID: "ms-1", Title: "v1 Launch", Status: "in-progress", Type: "milestone"})
	createInStore(t, core, &nib.Nib{ID: "epic-1", Title: "Alpha", Status: "todo", Type: "epic", Milestone: "ms-1", MilestoneOrder: "a", Order: "z"})

	tests := []struct {
		containerID string
		wantAxis    string
	}{
		{containerID: "ms-1", wantAxis: "milestone_order"},
		{containerID: "epic-1", wantAxis: "order"},
	}

	for _, tt := range tests {
		t.Run(tt.containerID, func(t *testing.T) {
			plan, err := buildPlan(context.Background(), resolver, tt.containerID, false)
			if err != nil {
				t.Fatalf("buildPlan error: %v", err)
			}
			if plan.Axis != tt.wantAxis {
				t.Errorf("plan.Axis = %q, want %q", plan.Axis, tt.wantAxis)
			}
			data, err := json.Marshal(plan)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			if want := `"axis":"` + tt.wantAxis + `"`; !strings.Contains(string(data), want) {
				t.Errorf("JSON missing %s, got: %s", want, data)
			}
		})
	}
}

// assertStoreUnchanged reports every file whose bytes moved between two
// storeSnapshot results, naming the file rather than only reporting that
// something moved.
func assertStoreUnchanged(t *testing.T, before, after map[string]string) {
	t.Helper()
	for name, want := range before {
		got, ok := after[name]
		if !ok {
			t.Errorf("a read deleted %s", name)
			continue
		}
		if got != want {
			t.Errorf("a read rewrote %s — a question must not edit a file the caller never named\nbefore:\n%s\nafter:\n%s", name, want, got)
		}
	}
	for name := range after {
		if _, ok := before[name]; !ok {
			t.Errorf("a read created %s", name)
		}
	}
}

// planItemIDs projects a plan to its item ids for whole-slice comparisons.
func planItemIDs(plan *Plan) []string {
	ids := make([]string, 0, len(plan.Items))
	for _, item := range plan.Items {
		ids = append(ids, item.ID)
	}
	return ids
}

// planMilestoneFixtureFiles returns a milestone and its queue, where every
// member's structural `order:` DISAGREES with its `milestone_order:`. A plan
// read on the wrong axis therefore produces a different sequence and a
// different set of order= keys, rather than agreeing by coincidence.
//
// The one closed entry sits in the MIDDLE of the queue, so the survivors'
// positions under --open (1 and 2) differ from the ones they hold unfiltered
// (1 and 3) — renumbering after the filter is the only way to get them right,
// and a position taken from a pre-filter index is visible instead of masked.
//
// t1 is a structural child of the milestone and is in no queue.
func planMilestoneFixtureFiles() map[string]string {
	return map[string]string{
		"m1--v1-launch.md": "---\nversion: 2\ntitle: v1 Launch\nstatus: in-progress\ntype: milestone\norder: a\n---\n",
		"e1--first-up.md":  "---\nversion: 2\ntitle: First Up\nstatus: todo\ntype: epic\nmilestone: m1\nmilestone_order: a\norder: z\n---\n",
		"e2--shipped.md":   "---\nversion: 2\ntitle: Shipped\nstatus: completed\ntype: epic\nmilestone: m1\nmilestone_order: b\norder: y\n---\n",
		"e3--last-up.md":   "---\nversion: 2\ntitle: Last Up\nstatus: todo\ntype: epic\nmilestone: m1\nmilestone_order: c\norder: x\n---\n",
		"t1--nested.md":    "---\nversion: 2\ntitle: Nested By Parent\nstatus: todo\ntype: task\nparent: m1\norder: a\n---\n",
	}
}

// TestPlanCommand_Milestone drives the milestone axis through Cobra, so it
// covers the wiring from the command down to the rendered rows rather than
// buildPlan alone.
func TestPlanCommand_Milestone(t *testing.T) {
	t.Run("with-order shows the queue keys", func(t *testing.T) {
		nibsDir := setupPlanCobraTest(t, planMilestoneFixtureFiles())
		rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "plan", "m1", "--with-order"})
		var execErr error
		out := captureStdout(t, func() { execErr = rootCmd.Execute() })
		if execErr != nil {
			t.Fatalf("rootCmd.Execute: %v", execErr)
		}

		wantLines := []string{
			"1. [todo] First Up (e1) order=a",
			"2. [completed] Shipped (e2) order=b",
			"3. [todo] Last Up (e3) order=c",
		}
		for _, want := range wantLines {
			if !strings.Contains(out, want) {
				t.Errorf("human output missing line %q\nfull output:\n%s", want, out)
			}
		}
		if strings.Contains(out, "Nested By Parent") {
			t.Errorf("a structural child of a milestone is not a queue entry\nfull output:\n%s", out)
		}
	})

	t.Run("empty queue names the axis it read", func(t *testing.T) {
		// Only the milestone and a structural child of it: the queue is empty
		// while the parent axis is not, so the message reports the axis plan
		// actually read rather than "No children."
		nibsDir := setupPlanCobraTest(t, map[string]string{
			"m1--v1-launch.md": "---\nversion: 2\ntitle: v1 Launch\nstatus: in-progress\ntype: milestone\norder: a\n---\n",
			"t1--nested.md":    "---\nversion: 2\ntitle: Nested By Parent\nstatus: todo\ntype: task\nparent: m1\norder: a\n---\n",
		})
		rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "plan", "m1"})
		var execErr error
		out := captureStdout(t, func() { execErr = rootCmd.Execute() })
		if execErr != nil {
			t.Fatalf("rootCmd.Execute: %v", execErr)
		}
		if !strings.Contains(out, "No queue entries.") {
			t.Errorf("empty milestone plan should report an empty queue\nfull output:\n%s", out)
		}
		if strings.Contains(out, "No children.") {
			t.Errorf("a milestone's plan is its queue, so it must not report on children\nfull output:\n%s", out)
		}
	})

	// The closed entry is the queue's MIDDLE one, so e3 must move from position
	// 3 to position 2. A Position taken from the pre-filter index would leave
	// it at 3 and fail here.
	t.Run("open filters and renumbers the queue", func(t *testing.T) {
		nibsDir := setupPlanCobraTest(t, planMilestoneFixtureFiles())
		rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "plan", "m1", "--open"})
		var execErr error
		out := captureStdout(t, func() { execErr = rootCmd.Execute() })
		if execErr != nil {
			t.Fatalf("rootCmd.Execute: %v", execErr)
		}

		for _, want := range []string{"1. [todo] First Up (e1)", "2. [todo] Last Up (e3)"} {
			if !strings.Contains(out, want) {
				t.Errorf("plan --open output missing line %q — positions must be recomputed after the filter\nfull output:\n%s", want, out)
			}
		}
		if strings.Contains(out, "Shipped") {
			t.Errorf("plan --open should drop the completed queue entry\nfull output:\n%s", out)
		}
	})

	// The empty branch has two causes once --open is in play, and reporting the
	// filtered one as an empty axis sends the reader off to populate a queue
	// that is already populated.
	t.Run("open reports what it hid rather than an empty queue", func(t *testing.T) {
		nibsDir := setupPlanCobraTest(t, map[string]string{
			"m1--v1-launch.md": "---\nversion: 2\ntitle: v1 Launch\nstatus: in-progress\ntype: milestone\n---\n",
			"e1--done.md":      "---\nversion: 2\ntitle: Done Epic\nstatus: completed\ntype: epic\nmilestone: m1\nmilestone_order: a\n---\n",
			"e2--also-done.md": "---\nversion: 2\ntitle: Also Done\nstatus: scrapped\ntype: epic\nmilestone: m1\nmilestone_order: b\n---\n",
		})
		rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "plan", "m1", "--open"})
		var execErr error
		out := captureStdout(t, func() { execErr = rootCmd.Execute() })
		if execErr != nil {
			t.Fatalf("rootCmd.Execute: %v", execErr)
		}
		if want := "No open queue entries (2 hidden by --open)."; !strings.Contains(out, want) {
			t.Errorf("plan --open on an all-closed queue should report %q\nfull output:\n%s", want, out)
		}
		if strings.Contains(out, "No queue entries.") {
			t.Errorf("the queue has two entries — plan must not assert it is empty\nfull output:\n%s", out)
		}
	})
}
