package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
)

func setupPlanTest(t *testing.T) (*graph.Resolver, func(id, title, status, typ, parent, order, body string) *nib.Nib) {
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

	resolver := &graph.Resolver{Reader: core, Writer: core, Validator: core, Blocking: core, Orderer: graph.NewOrderer(core, core)}

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

	plan, err := buildPlan(resolver, "epic-1", false)
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

func TestBuildPlan_AcceptanceCriteria(t *testing.T) {
	resolver, create := setupPlanTest(t)

	create("epic-1", "My Epic", "in-progress", "epic", "", "", "")

	bodyWithAC := `Some intro text.

## Acceptance Criteria

- [ ] First criterion
- [x] Second criterion
- [ ] Third criterion

## Other Section

Other content.
`
	create("task-1", "Task With AC", "todo", "task", "epic-1", "a0", bodyWithAC)

	plan, err := buildPlan(resolver, "epic-1", false)
	if err != nil {
		t.Fatalf("buildPlan error: %v", err)
	}

	if len(plan.Items) != 1 {
		t.Fatalf("items count = %d, want 1", len(plan.Items))
	}

	item := plan.Items[0]
	if item.AcceptanceCriteria == "" {
		t.Fatal("AcceptanceCriteria should not be empty")
	}

	// Should contain the criteria lines but not the heading or other sections
	if !strings.Contains(item.AcceptanceCriteria, "First criterion") {
		t.Errorf("AC should contain 'First criterion', got: %q", item.AcceptanceCriteria)
	}
	if !strings.Contains(item.AcceptanceCriteria, "Second criterion") {
		t.Errorf("AC should contain 'Second criterion', got: %q", item.AcceptanceCriteria)
	}
	if !strings.Contains(item.AcceptanceCriteria, "Third criterion") {
		t.Errorf("AC should contain 'Third criterion', got: %q", item.AcceptanceCriteria)
	}
	if strings.Contains(item.AcceptanceCriteria, "## Acceptance Criteria") {
		t.Errorf("AC should not contain section heading, got: %q", item.AcceptanceCriteria)
	}
	if strings.Contains(item.AcceptanceCriteria, "Other content") {
		t.Errorf("AC should NOT contain content from other sections, got: %q", item.AcceptanceCriteria)
	}
}

func TestBuildPlan_JSONOutput(t *testing.T) {
	resolver, create := setupPlanTest(t)

	create("epic-1", "Release v1", "in-progress", "epic", "", "", "")
	create("task-1", "Do Thing", "todo", "task", "epic-1", "a0", "## Acceptance Criteria\n\n- [ ] Works\n")

	plan, err := buildPlan(resolver, "epic-1", false)
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
	if got.Items[0].AcceptanceCriteria == "" {
		t.Error("JSON items[0].acceptance_criteria should not be empty")
	}

	// Verify JSON field names (snake_case)
	raw := string(data)
	if !strings.Contains(raw, `"acceptance_criteria"`) {
		t.Errorf("JSON should use snake_case field 'acceptance_criteria', got: %s", raw)
	}
	if !strings.Contains(raw, `"position"`) {
		t.Errorf("JSON should contain 'position' field, got: %s", raw)
	}
}

func TestBuildPlan_EmptyChildren(t *testing.T) {
	resolver, create := setupPlanTest(t)

	create("epic-1", "Empty Epic", "todo", "epic", "", "", "")

	plan, err := buildPlan(resolver, "epic-1", false)
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

func TestBuildPlan_MissingAcceptanceCriteria(t *testing.T) {
	resolver, create := setupPlanTest(t)

	create("epic-1", "My Epic", "in-progress", "epic", "", "", "")

	// One child with AC, one without
	create("task-1", "Has AC", "todo", "task", "epic-1", "a0", "## Acceptance Criteria\n\n- [ ] Do it\n")
	create("task-2", "No AC", "todo", "task", "epic-1", "a1", "Just a plain description.\n")
	create("task-3", "Empty Body", "todo", "task", "epic-1", "a2", "")

	plan, err := buildPlan(resolver, "epic-1", false)
	if err != nil {
		t.Fatalf("buildPlan error: %v", err)
	}

	if len(plan.Items) != 3 {
		t.Fatalf("items count = %d, want 3", len(plan.Items))
	}

	// First item should have AC
	if plan.Items[0].AcceptanceCriteria == "" {
		t.Error("item[0] should have acceptance criteria")
	}

	// Second and third items should have empty AC (omitted in JSON)
	if plan.Items[1].AcceptanceCriteria != "" {
		t.Errorf("item[1].AcceptanceCriteria = %q, want empty", plan.Items[1].AcceptanceCriteria)
	}
	if plan.Items[2].AcceptanceCriteria != "" {
		t.Errorf("item[2].AcceptanceCriteria = %q, want empty", plan.Items[2].AcceptanceCriteria)
	}

	// Verify JSON omits empty acceptance_criteria
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	// Count occurrences of "acceptance_criteria" - should appear only once
	raw := string(data)
	count := strings.Count(raw, "acceptance_criteria")
	if count != 1 {
		t.Errorf("'acceptance_criteria' should appear once in JSON (for item with AC), appeared %d times", count)
	}
}

func TestBuildPlan_ParentNotFound(t *testing.T) {
	resolver, _ := setupPlanTest(t)

	_, err := buildPlan(resolver, "nonexistent", false)
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
		plan, err := buildPlan(resolver, "epic-1", false)
		if err != nil {
			t.Fatalf("buildPlan error: %v", err)
		}
		if len(plan.Items) != 5 {
			t.Errorf("items count = %d, want 5", len(plan.Items))
		}
	})

	t.Run("activeOnly=true excludes completed and scrapped", func(t *testing.T) {
		plan, err := buildPlan(resolver, "epic-1", true)
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
