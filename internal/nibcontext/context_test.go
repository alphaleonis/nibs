package nibcontext

import (
	"encoding/json"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
)

// cfgForTest is the config BuildSummary takes. It is the real default config,
// so these tests exercise the same closed set the CLI does instead of a private
// copy that could drift from it.
var cfgForTest = config.Default()

// makeNib is a test helper for building nibs with common fields.
func makeNib(id, typ, status, estimate, parent string) *nib.Nib {
	return &nib.Nib{
		ID:       id,
		Title:    id, // use ID as title for simplicity
		Type:     typ,
		Status:   status,
		Estimate: estimate,
		Parent:   parent,
	}
}

// makeQueued is makeNib on the assignment axis: the nib is enqueued in a
// milestone via its milestone: field rather than parented to it — the v2
// shape milestone membership derives from.
func makeQueued(id, typ, status, estimate, milestone string) *nib.Nib {
	n := makeNib(id, typ, status, estimate, "")
	n.Milestone = milestone
	return n
}

func TestBuildSummary(t *testing.T) {
	// Build a realistic nib tree:
	//   milestone (m1)
	//     epic-phase1 (e1) - completed
	//       task-a (t1) - completed, s
	//       task-b (t2) - completed, m
	//     epic-phase2 (e2) - in-progress  <-- active phase
	//       feature-c (f1) - completed, l
	//       task-d (t3) - in-progress, m  <-- active task
	//       bug-e (b1) - todo, s          <-- next task
	//       task-f (t4) - todo, xl        <-- next task
	//     epic-phase3 (e3) - todo
	//       task-g (t5) - todo, m
	//   unrelated (u1) - in-progress (not under milestone)

	m1 := makeNib("m1", "milestone", "in-progress", "", "")
	m1.Body = "## Goal\n\nShip the thing.\n\n## Key Decisions\n\n- Use file-based storage\n- GraphQL for all queries"

	allNibs := []*nib.Nib{
		m1,
		makeQueued("e1", "epic", "completed", "", "m1"),
		makeNib("t1", "task", "completed", "s", "e1"),
		makeNib("t2", "task", "completed", "m", "e1"),
		makeQueued("e2", "epic", "in-progress", "", "m1"),
		makeNib("f1", "feature", "completed", "l", "e2"),
		makeNib("t3", "task", "in-progress", "m", "e2"),
		makeNib("b1", "bug", "todo", "s", "e2"),
		makeNib("t4", "task", "todo", "xl", "e2"),
		makeQueued("e3", "epic", "todo", "", "m1"),
		makeNib("t5", "task", "todo", "m", "e3"),
		makeNib("u1", "task", "in-progress", "m", ""),
	}

	t.Run("scoped to milestone", func(t *testing.T) {
		sum := BuildSummary(allNibs, "m1", cfgForTest)

		// Milestone
		if sum.Root == nil || sum.Root.ID != "m1" {
			t.Fatal("expected root m1")
		}

		// Active phase: the in-progress epic
		if sum.ActivePhase == nil {
			t.Fatal("ActivePhase is nil, want e2")
		}
		if sum.ActivePhase.ID != "e2" {
			t.Errorf("ActivePhase.ID = %q, want %q", sum.ActivePhase.ID, "e2")
		}

		// Active tasks: in-progress leaf work
		if len(sum.ActiveTasks) != 1 || sum.ActiveTasks[0].ID != "t3" {
			t.Errorf("ActiveTasks = %v, want [t3]", nibRefIDs(sum.ActiveTasks))
		}

		// Next tasks: todo items under active phase, by order
		if len(sum.NextTasks) != 2 {
			t.Errorf("NextTasks count = %d, want 2", len(sum.NextTasks))
		} else {
			ids := nibRefIDs(sum.NextTasks)
			if ids[0] != "b1" || ids[1] != "t4" {
				t.Errorf("NextTasks = %v, want [b1, t4]", ids)
			}
		}

		// Decisions extracted from milestone body
		if len(sum.Decisions) != 2 {
			t.Fatalf("Decisions = %v, want 2", sum.Decisions)
		}
		if sum.Decisions[0] != "Use file-based storage" {
			t.Errorf("Decisions[0] = %q, want %q", sum.Decisions[0], "Use file-based storage")
		}
		if sum.Decisions[1] != "GraphQL for all queries" {
			t.Errorf("Decisions[1] = %q, want %q", sum.Decisions[1], "GraphQL for all queries")
		}

		// No warnings when milestone specified
		if len(sum.Warnings) != 0 {
			t.Errorf("Warnings = %v, want none", sum.Warnings)
		}
	})

	t.Run("no arg shows overview with containers", func(t *testing.T) {
		sum := BuildSummary(allNibs, "", cfgForTest)

		if sum.Root != nil {
			t.Errorf("expected nil root, got %v", sum.Root)
		}

		// No warnings in overview mode
		if len(sum.Warnings) != 0 {
			t.Errorf("Warnings = %v, want none", sum.Warnings)
		}

		// Active tasks should include ALL in-progress leaf work (t3 + u1)
		ids := nibRefIDs(sum.ActiveTasks)
		if len(ids) != 2 {
			t.Fatalf("ActiveTasks = %v, want [t3, u1]", ids)
		}
		idSet := map[string]bool{ids[0]: true, ids[1]: true}
		if !idSet["t3"] || !idSet["u1"] {
			t.Errorf("ActiveTasks = %v, want [t3, u1]", ids)
		}

		// Should have a container for the milestone m1 (the only active milestone in this test set)
		if len(sum.Containers) != 1 {
			t.Fatalf("Containers count = %d, want 1", len(sum.Containers))
		}
		if sum.Containers[0].ID != "m1" {
			t.Errorf("Containers[0].ID = %q, want %q", sum.Containers[0].ID, "m1")
		}
	})
}

func TestBuildSummary_ResearchIsLeafWork(t *testing.T) {
	// milestone (m1)
	//   epic (e1) - in-progress  <-- active phase
	//     research-r1 (in-progress) <-- should be active
	//     research-r2 (todo)        <-- should be next
	//     task-t1 (todo)            <-- should be next
	allNibs := []*nib.Nib{
		makeNib("m1", "milestone", "in-progress", "", ""),
		makeQueued("e1", "epic", "in-progress", "", "m1"),
		makeNib("r1", "research", "in-progress", "m", "e1"),
		makeNib("r2", "research", "todo", "s", "e1"),
		makeNib("t1", "task", "todo", "s", "e1"),
	}

	sum := BuildSummary(allNibs, "m1", cfgForTest)

	// Active tasks must include r1 (in-progress research)
	activeIDs := nibRefIDs(sum.ActiveTasks)
	foundR1 := false
	for _, id := range activeIDs {
		if id == "r1" {
			foundR1 = true
		}
	}
	if !foundR1 {
		t.Errorf("ActiveTasks = %v, want to include r1 (in-progress research)", activeIDs)
	}

	// Next tasks must include r2 (todo research) and t1 (todo task)
	nextIDs := nibRefIDs(sum.NextTasks)
	nextSet := map[string]bool{}
	for _, id := range nextIDs {
		nextSet[id] = true
	}
	if !nextSet["r2"] {
		t.Errorf("NextTasks = %v, want to include r2 (todo research)", nextIDs)
	}
	if !nextSet["t1"] {
		t.Errorf("NextTasks = %v, want to include t1", nextIDs)
	}

	// Overview mode still reports the container itself; only the weighted
	// progress it used to carry has moved out of this package.
	overview := BuildSummary(allNibs, "", cfgForTest)
	if len(overview.Containers) != 1 {
		t.Fatalf("Containers count = %d, want 1", len(overview.Containers))
	}
}

func TestExtractDecisions(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "no decisions section",
			body: "## Description\n\nSome text.",
			want: nil,
		},
		{
			name: "decisions section with bullet points",
			body: `## Goal

Build the thing.

## Key Decisions

- Use GraphQL for all queries
- Store estimates as t-shirt sizes
- Default unestimated to M weight

## Notes

Other stuff.`,
			want: []string{
				"Use GraphQL for all queries",
				"Store estimates as t-shirt sizes",
				"Default unestimated to M weight",
			},
		},
		{
			name: "decisions with mixed formatting",
			body: `## Key Decisions

* Decision one
- Decision two
  continued on next line

Some paragraph text (not a decision).

- Decision three`,
			want: []string{
				"Decision one",
				"Decision two",
				"Decision three",
			},
		},
		{
			name: "case insensitive header match",
			body: `## key decisions

- Works with lowercase too`,
			want: []string{"Works with lowercase too"},
		},
		{
			name: "heading with hash prefix is not false match",
			body: "## #key decisions\n\n- Not a real decision",
			want: nil,
		},
		{
			name: "header with parenthetical suffix",
			body: `## Key Decisions (Phase 2)

- Base-62 fractional indexing
- Order is sole sort key`,
			want: []string{
				"Base-62 fractional indexing",
				"Order is sole sort key",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractDecisions(tt.body)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d decisions %v, want %d %v", len(got), got, len(tt.want), tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("decision[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// makeNibWithOrder is a test helper for building nibs with an order key.
func makeNibWithOrder(id, typ, status, estimate, parent, order string) *nib.Nib {
	n := makeNib(id, typ, status, estimate, parent)
	n.Order = order
	return n
}

// makeQueuedWithOrder is makeQueued plus a MilestoneOrder key: an assignee's
// position lives in the milestone's queue, and the phase sorting under a
// milestone reads the queue key.
func makeQueuedWithOrder(id, typ, status, estimate, milestone, milestoneOrder string) *nib.Nib {
	n := makeQueued(id, typ, status, estimate, milestone)
	n.MilestoneOrder = milestoneOrder
	return n
}

func TestBuildSummary_ActivePhaseByQueueOrder(t *testing.T) {
	// Two in-progress epics assigned to the milestone, different MilestoneOrder
	// keys. ActivePhase should be the one first in the QUEUE, regardless of
	// input order — and regardless of any parent-scope Order key, which
	// positions an assignee among the structural roots, not in the queue.
	m := makeNib("ms", "milestone", "in-progress", "", "")

	// epic-b is first in the queue ("a1" < "a2") but appears later in the
	// slice, and epic-a's LOWER root-scope Order must not outrank it.
	epicA := makeQueuedWithOrder("epic-a", "epic", "in-progress", "", "ms", "a2")
	epicA.Order = "a0"
	allNibs := []*nib.Nib{
		m,
		epicA,
		makeQueuedWithOrder("epic-b", "epic", "in-progress", "", "ms", "a1"),
	}

	sum := BuildSummary(allNibs, "ms", cfgForTest)

	if sum.ActivePhase == nil {
		t.Fatal("ActivePhase is nil")
	}
	if sum.ActivePhase.ID != "epic-b" {
		t.Errorf("ActivePhase.ID = %q, want %q (lowest MilestoneOrder key)", sum.ActivePhase.ID, "epic-b")
	}
}

// TestSortDirectMembers pins the container seam directly: a milestone's
// members sort by the queue key (milestone_order), any other container's by
// the decomposition key (order) — each ignoring the other axis's key.
func TestSortDirectMembers(t *testing.T) {
	first := makeNib("first", "task", "todo", "", "")
	second := makeNib("second", "task", "todo", "", "")
	// The keys disagree across the axes: first leads the queue but trails the
	// decomposition, so each container type must produce the opposite order.
	first.MilestoneOrder, first.Order = "a1", "a2"
	second.MilestoneOrder, second.Order = "a2", "a1"

	milestone := makeNib("ms", "milestone", "in-progress", "", "")
	got := []*nib.Nib{second, first}
	sortDirectMembers(milestone, got)
	if got[0].ID != "first" || got[1].ID != "second" {
		t.Errorf("milestone members = [%s, %s], want queue order [first, second]", got[0].ID, got[1].ID)
	}

	epic := makeNib("ep", "epic", "in-progress", "", "")
	got = []*nib.Nib{first, second}
	sortDirectMembers(epic, got)
	if got[0].ID != "second" || got[1].ID != "first" {
		t.Errorf("epic members = [%s, %s], want decomposition order [second, first]", got[0].ID, got[1].ID)
	}
}

func TestBuildSummary_NextTasksSortedByOrder(t *testing.T) {
	m := makeNib("ms", "milestone", "in-progress", "", "")
	ep := makeQueuedWithOrder("ep", "epic", "in-progress", "", "ms", "a0")

	// Tasks under the active phase with Order keys in non-alphabetical input order
	allNibs := []*nib.Nib{
		m, ep,
		makeNibWithOrder("t-c", "task", "todo", "s", "ep", "a3"),
		makeNibWithOrder("t-a", "task", "todo", "s", "ep", "a1"),
		makeNibWithOrder("t-b", "task", "todo", "s", "ep", "a2"),
	}

	sum := BuildSummary(allNibs, "ms", cfgForTest)

	ids := nibRefIDs(sum.NextTasks)
	want := []string{"t-a", "t-b", "t-c"}
	if len(ids) != len(want) {
		t.Fatalf("NextTasks = %v, want %v", ids, want)
	}
	for i := range ids {
		if ids[i] != want[i] {
			t.Errorf("NextTasks[%d] = %q, want %q", i, ids[i], want[i])
		}
	}
}

func TestBuildSummary_ActiveTasksSortedByOrder(t *testing.T) {
	m := makeNib("ms", "milestone", "in-progress", "", "")
	ep := makeQueued("ep", "epic", "in-progress", "", "ms")

	// In-progress tasks under milestone, input order differs from Order
	allNibs := []*nib.Nib{
		m, ep,
		makeNibWithOrder("at-c", "task", "in-progress", "s", "ep", "a3"),
		makeNibWithOrder("at-a", "task", "in-progress", "s", "ep", "a1"),
		makeNibWithOrder("at-b", "bug", "in-progress", "s", "ep", "a2"),
	}

	sum := BuildSummary(allNibs, "ms", cfgForTest)

	ids := nibRefIDs(sum.ActiveTasks)
	want := []string{"at-a", "at-b", "at-c"}
	if len(ids) != len(want) {
		t.Fatalf("ActiveTasks = %v, want %v", ids, want)
	}
	for i := range ids {
		if ids[i] != want[i] {
			t.Errorf("ActiveTasks[%d] = %q, want %q", i, ids[i], want[i])
		}
	}
}

func TestBuildSummary_JSONContract(t *testing.T) {
	// Integration test: BuildSummary JSON must not leak full nib.Nib fields.
	// NibRef shape is tested separately in TestNibRefJSONFields; this test
	// focuses on the deny-list (no body, etag, path, etc.) and correct values.
	m := makeNib("ms", "milestone", "in-progress", "l", "")
	m.Title = "My Milestone"
	m.Body = "should not appear"
	ep := makeQueued("ep", "epic", "in-progress", "", "ms")
	task := makeNib("t1", "task", "in-progress", "l", "ep")
	task.Title = "Active Task"
	task.Body = "task body should not appear"
	next := makeNib("t2", "task", "todo", "s", "ep")
	next.Title = "Next Task"

	sum := BuildSummary([]*nib.Nib{m, ep, task, next}, "ms", cfgForTest)

	data, err := json.Marshal(sum)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	// Fields that must never appear in NibRef JSON output
	denyKeys := []string{"body", "etag", "path", "parent", "slug", "version",
		"created_at", "updated_at", "blocked_by", "documents", "tags"}

	// assertNibRefJSON checks a JSON object for denied keys and required values.
	assertNibRefJSON := func(t *testing.T, label string, jsonData json.RawMessage, wantID, wantTitle string) {
		t.Helper()
		var obj map[string]interface{}
		if err := json.Unmarshal(jsonData, &obj); err != nil {
			t.Fatalf("%s: unmarshal: %v", label, err)
		}
		for _, k := range denyKeys {
			if _, ok := obj[k]; ok {
				t.Errorf("%s: JSON should not have key %q (leaked from full Nib)", label, k)
			}
		}
		// Verify required fields are present with correct values
		if obj["id"] != wantID {
			t.Errorf("%s: id = %v, want %v", label, obj["id"], wantID)
		}
		if obj["title"] != wantTitle {
			t.Errorf("%s: title = %v, want %v", label, obj["title"], wantTitle)
		}
	}

	// Check root
	assertNibRefJSON(t, "root", raw["root"], "ms", "My Milestone")

	// Check active_tasks[0]
	var tasks []json.RawMessage
	if err := json.Unmarshal(raw["active_tasks"], &tasks); err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("active_tasks len = %d, want 1", len(tasks))
	}
	assertNibRefJSON(t, "active_tasks[0]", tasks[0], "t1", "Active Task")

	// Check next_tasks[0]
	var nextTasks []json.RawMessage
	if err := json.Unmarshal(raw["next_tasks"], &nextTasks); err != nil {
		t.Fatal(err)
	}
	if len(nextTasks) != 1 {
		t.Fatalf("next_tasks len = %d, want 1", len(nextTasks))
	}
	assertNibRefJSON(t, "next_tasks[0]", nextTasks[0], "t2", "Next Task")
}

func TestNibRefJSONFields(t *testing.T) {
	t.Run("all fields present", func(t *testing.T) {
		ref := NibRef{
			ID:       "test-1",
			Title:    "Test nib",
			Status:   "in-progress",
			Type:     "task",
			Estimate: "m",
		}

		data, err := json.Marshal(ref)
		if err != nil {
			t.Fatal(err)
		}

		var raw map[string]interface{}
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatal(err)
		}

		// Should have exactly these 5 keys with correct values
		want := map[string]interface{}{
			"id": "test-1", "title": "Test nib", "status": "in-progress",
			"type": "task", "estimate": "m",
		}
		for k := range raw {
			if _, ok := want[k]; !ok {
				t.Errorf("unexpected JSON key %q", k)
			}
		}
		for k, wantV := range want {
			if raw[k] != wantV {
				t.Errorf("%s = %v, want %v", k, raw[k], wantV)
			}
		}
	})

	t.Run("omitempty fields absent when empty", func(t *testing.T) {
		ref := NibRef{ID: "x", Title: "X", Status: "todo"}
		data, err := json.Marshal(ref)
		if err != nil {
			t.Fatal(err)
		}
		var raw map[string]interface{}
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatal(err)
		}
		if _, ok := raw["type"]; ok {
			t.Error("type should be omitted when empty")
		}
		if _, ok := raw["estimate"]; ok {
			t.Error("estimate should be omitted when empty")
		}
		// Required fields must still be present
		for _, k := range []string{"id", "title", "status"} {
			if _, ok := raw[k]; !ok {
				t.Errorf("required field %q missing", k)
			}
		}
	})
}

func TestBuildSummary_Overview(t *testing.T) {
	// No-arg BuildSummary should return an overview of active milestones.
	//   milestone-a (m1) - in-progress
	//     epic-p1 (e1) - in-progress
	//       task-1 (t1) - completed, s
	//       task-2 (t2) - in-progress, m
	//     epic-p2 (e2) - todo
	//       task-3 (t3) - todo, l
	//   milestone-b (m2) - todo
	//     task-4 (t4) - todo, m
	//   completed-milestone (m3) - completed (should be excluded)
	//     task-5 (t5) - completed, s
	//   orphan (u1) - in-progress, task (no parent, not a container)

	m1 := makeNib("m1", "milestone", "in-progress", "", "")
	m1.Title = "Ship v1"
	m2 := makeNib("m2", "milestone", "todo", "", "")
	m2.Title = "Ship v2"
	m3 := makeNib("m3", "milestone", "completed", "", "")
	m3.Title = "Old milestone"

	allNibs := []*nib.Nib{
		m1,
		makeQueuedWithOrder("e1", "epic", "in-progress", "", "m1", "a1"),
		makeNib("t1", "task", "completed", "s", "e1"),
		makeNib("t2", "task", "in-progress", "m", "e1"),
		makeQueuedWithOrder("e2", "epic", "todo", "", "m1", "a2"),
		makeNib("t3", "task", "todo", "l", "e2"),
		m2,
		makeQueued("t4", "task", "todo", "m", "m2"),
		m3,
		makeQueued("t5", "task", "completed", "s", "m3"),
		makeNib("u1", "task", "in-progress", "m", ""),
	}

	sum := BuildSummary(allNibs, "", cfgForTest)

	// Should have no root (overview mode)
	if sum.Root != nil {
		t.Errorf("Root = %v, want nil in overview mode", sum.Root)
	}

	// No warnings in overview mode
	if len(sum.Warnings) != 0 {
		t.Errorf("Warnings = %v, want none", sum.Warnings)
	}

	// Should have containers for active milestones (m1, m2), excluding completed m3
	if len(sum.Containers) != 2 {
		t.Fatalf("Containers count = %d, want 2", len(sum.Containers))
	}

	// First container: m1 (in-progress sorts before todo)
	c1 := sum.Containers[0]
	if c1.ID != "m1" {
		t.Errorf("Containers[0].ID = %q, want %q", c1.ID, "m1")
	}
	if c1.ActivePhase == nil || c1.ActivePhase.ID != "e1" {
		t.Errorf("Containers[0].ActivePhase = %v, want e1", c1.ActivePhase)
	}

	// Second container: m2
	c2 := sum.Containers[1]
	if c2.ID != "m2" {
		t.Errorf("Containers[1].ID = %q, want %q", c2.ID, "m2")
	}
	if c2.ActivePhase != nil {
		t.Errorf("Containers[1].ActivePhase = %v, want nil", c2.ActivePhase)
	}

	// Active tasks should include ALL in-progress leaf work
	activeIDs := nibRefIDs(sum.ActiveTasks)
	if len(activeIDs) != 2 {
		t.Fatalf("ActiveTasks = %v, want [t2, u1]", activeIDs)
	}
	idSet := map[string]bool{activeIDs[0]: true, activeIDs[1]: true}
	if !idSet["t2"] || !idSet["u1"] {
		t.Errorf("ActiveTasks = %v, want [t2, u1]", activeIDs)
	}
}

func TestContainerSummaryJSONFields(t *testing.T) {
	cs := ContainerSummary{
		NibRef: NibRef{
			ID:     "m1",
			Title:  "Ship v1",
			Status: "in-progress",
			Type:   "milestone",
		},
		ActivePhase: &NibRef{ID: "e1", Title: "Phase 1", Status: "in-progress", Type: "epic"},
	}

	data, err := json.Marshal(cs)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	// NibRef fields should be flattened (not nested under a "nib_ref" key)
	wantKeys := map[string]bool{
		"id": true, "title": true, "status": true, "type": true,
		"active_phase": true,
	}
	for k := range raw {
		if !wantKeys[k] {
			t.Errorf("unexpected JSON key %q", k)
		}
	}
	for k := range wantKeys {
		if _, ok := raw[k]; !ok {
			t.Errorf("missing expected JSON key %q", k)
		}
	}
}

func TestBuildSummary_OverviewJSON(t *testing.T) {
	// Verify the full Summary JSON in overview mode includes containers
	m1 := makeNib("m1", "milestone", "in-progress", "", "")
	t1 := makeQueued("t1", "task", "todo", "s", "m1")

	sum := BuildSummary([]*nib.Nib{m1, t1}, "", cfgForTest)

	data, err := json.Marshal(sum)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	// Should have containers key
	if _, ok := raw["containers"]; !ok {
		t.Fatal("missing 'containers' key in overview JSON")
	}

	// root should be absent (omitempty)
	if _, ok := raw["root"]; ok {
		t.Error("'root' should be absent in overview mode")
	}

	// Verify container has flattened NibRef fields
	var containers []map[string]json.RawMessage
	if err := json.Unmarshal(raw["containers"], &containers); err != nil {
		t.Fatal(err)
	}
	if len(containers) != 1 {
		t.Fatalf("containers count = %d, want 1", len(containers))
	}
	c := containers[0]
	if _, ok := c["id"]; !ok {
		t.Error("container missing 'id' (NibRef not flattened)")
	}
}

func TestBuildSummary_NonMilestoneRoot(t *testing.T) {
	// BuildSummary should work on any nib with children, not just milestones.
	//   epic (e1) - in-progress
	//     task-a (t1) - completed, s
	//     task-b (t2) - in-progress, m  <-- active task
	//     task-c (t3) - todo, l         <-- next task

	e1 := makeNib("e1", "epic", "in-progress", "", "")
	e1.Body = "## Key Decisions\n\n- Decisions work on any nib"

	allNibs := []*nib.Nib{
		e1,
		makeNib("t1", "task", "completed", "s", "e1"),
		makeNib("t2", "task", "in-progress", "m", "e1"),
		makeNib("t3", "task", "todo", "l", "e1"),
	}

	sum := BuildSummary(allNibs, "e1", cfgForTest)

	// Root should be the epic, not nil
	if sum.Root == nil {
		t.Fatal("Root is nil, want e1")
	}
	if sum.Root.ID != "e1" {
		t.Errorf("Root.ID = %q, want %q", sum.Root.ID, "e1")
	}

	// No active phase (epic has no epic children)
	if sum.ActivePhase != nil {
		t.Errorf("ActivePhase = %v, want nil (no epic children)", sum.ActivePhase)
	}

	// Active tasks: t2
	if len(sum.ActiveTasks) != 1 || sum.ActiveTasks[0].ID != "t2" {
		t.Errorf("ActiveTasks = %v, want [t2]", nibRefIDs(sum.ActiveTasks))
	}

	// Next tasks: all todo children (no active phase, so next = all todo under root)
	if len(sum.NextTasks) != 1 || sum.NextTasks[0].ID != "t3" {
		t.Errorf("NextTasks = %v, want [t3]", nibRefIDs(sum.NextTasks))
	}

	// Decisions extracted from epic body
	if len(sum.Decisions) != 1 || sum.Decisions[0] != "Decisions work on any nib" {
		t.Errorf("Decisions = %v, want [Decisions work on any nib]", sum.Decisions)
	}

	// No warnings
	if len(sum.Warnings) != 0 {
		t.Errorf("Warnings = %v, want none", sum.Warnings)
	}
}

// nibRefIDs extracts IDs from a slice of NibRefs for test assertions.
func nibRefIDs(refs []*NibRef) []string {
	ids := make([]string, len(refs))
	for i, r := range refs {
		ids[i] = r.ID
	}
	return ids
}
