package ui

import (
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
)

func TestBuildTree(t *testing.T) {
	// Create test nibs with parent relationships:
	// milestone1
	//   └── epic1
	//       └── task1
	// task2 (orphan)

	milestone1 := &nib.Nib{ID: "m1", Title: "Milestone 1", Type: "milestone"}
	epic1 := &nib.Nib{ID: "e1", Title: "Epic 1", Type: "epic", Parent: "m1"}
	task1 := &nib.Nib{ID: "t1", Title: "Task 1", Type: "task", Parent: "e1"}
	task2 := &nib.Nib{ID: "t2", Title: "Task 2", Type: "task"} // orphan

	allNibs := []*nib.Nib{milestone1, epic1, task1, task2}

	// Identity sort function (no sorting)
	noSort := func(b []*nib.Nib) {}

	t.Run("all nibs matched", func(t *testing.T) {
		tree := BuildTree(allNibs, allNibs, noSort)

		// Should have 2 root nodes: milestone1 and task2
		if len(tree) != 2 {
			t.Errorf("expected 2 root nodes, got %d", len(tree))
		}

		// Find milestone node
		var milestoneNode *TreeNode
		for _, n := range tree {
			if n.Nib.ID == "m1" {
				milestoneNode = n
				break
			}
		}
		if milestoneNode == nil {
			t.Fatal("milestone node not found")
			return
		}
		if !milestoneNode.Matched {
			t.Error("milestone should be marked as matched")
		}

		// Milestone should have epic as child
		if len(milestoneNode.Children) != 1 {
			t.Errorf("milestone should have 1 child, got %d", len(milestoneNode.Children))
		}
		epicNode := milestoneNode.Children[0]
		if epicNode.Nib.ID != "e1" {
			t.Errorf("expected epic child, got %s", epicNode.Nib.ID)
		}

		// Epic should have task as child
		if len(epicNode.Children) != 1 {
			t.Errorf("epic should have 1 child, got %d", len(epicNode.Children))
		}
		taskNode := epicNode.Children[0]
		if taskNode.Nib.ID != "t1" {
			t.Errorf("expected task child, got %s", taskNode.Nib.ID)
		}
	})

	t.Run("filter leaf only - ancestors included", func(t *testing.T) {
		// Only task1 matched, but ancestors should be included
		matchedNibs := []*nib.Nib{task1}
		tree := BuildTree(matchedNibs, allNibs, noSort)

		// Should have 1 root: milestone (as ancestor)
		if len(tree) != 1 {
			t.Errorf("expected 1 root node, got %d", len(tree))
		}

		milestoneNode := tree[0]
		if milestoneNode.Nib.ID != "m1" {
			t.Errorf("expected milestone as root, got %s", milestoneNode.Nib.ID)
		}
		if milestoneNode.Matched {
			t.Error("milestone should NOT be marked as matched (it's an ancestor)")
		}

		// Should have epic as child (also ancestor)
		if len(milestoneNode.Children) != 1 {
			t.Fatalf("milestone should have 1 child, got %d", len(milestoneNode.Children))
		}
		epicNode := milestoneNode.Children[0]
		if epicNode.Matched {
			t.Error("epic should NOT be marked as matched (it's an ancestor)")
		}

		// Task should be matched
		if len(epicNode.Children) != 1 {
			t.Fatalf("epic should have 1 child, got %d", len(epicNode.Children))
		}
		taskNode := epicNode.Children[0]
		if !taskNode.Matched {
			t.Error("task should be marked as matched")
		}
	})

	t.Run("filter middle - ancestors included", func(t *testing.T) {
		// Only epic1 matched
		matchedNibs := []*nib.Nib{epic1}
		tree := BuildTree(matchedNibs, allNibs, noSort)

		// Should have 1 root: milestone (ancestor)
		if len(tree) != 1 {
			t.Errorf("expected 1 root node, got %d", len(tree))
		}

		milestoneNode := tree[0]
		if milestoneNode.Matched {
			t.Error("milestone should NOT be marked as matched")
		}

		epicNode := milestoneNode.Children[0]
		if !epicNode.Matched {
			t.Error("epic should be marked as matched")
		}

		// Epic should have no children (task1 was not matched)
		if len(epicNode.Children) != 0 {
			t.Errorf("epic should have 0 children (task not matched), got %d", len(epicNode.Children))
		}
	})

	t.Run("orphan nib", func(t *testing.T) {
		matchedNibs := []*nib.Nib{task2}
		tree := BuildTree(matchedNibs, allNibs, noSort)

		if len(tree) != 1 {
			t.Errorf("expected 1 root node, got %d", len(tree))
		}
		if tree[0].Nib.ID != "t2" {
			t.Errorf("expected task2 as root, got %s", tree[0].Nib.ID)
		}
		if !tree[0].Matched {
			t.Error("task2 should be marked as matched")
		}
	})

	t.Run("broken parent link", func(t *testing.T) {
		// Nib with parent that doesn't exist
		brokenNib := &nib.Nib{ID: "broken", Title: "Broken", Parent: "nonexistent"}
		matchedNibs := []*nib.Nib{brokenNib}
		allNibsWithBroken := append(allNibs, brokenNib)

		tree := BuildTree(matchedNibs, allNibsWithBroken, noSort)

		// Should be treated as root (parent not found)
		if len(tree) != 1 {
			t.Errorf("expected 1 root node, got %d", len(tree))
		}
		if tree[0].Nib.ID != "broken" {
			t.Errorf("expected broken nib as root, got %s", tree[0].Nib.ID)
		}
	})
}

func TestTreeNodeToJSON(t *testing.T) {
	b := &nib.Nib{
		ID:       "test-id",
		Slug:     "test-slug",
		Path:     "test.md",
		Title:    "Test Title",
		Status:   "todo",
		Type:     "task",
		Priority: "high",
		Tags:     []string{"tag1", "tag2"},
		Body:     "Test body content",
	}

	node := &TreeNode{
		Nib:    b,
		Matched: true,
		Children: []*TreeNode{
			{
				Nib:    &nib.Nib{ID: "child-id", Title: "Child"},
				Matched: false,
			},
		},
	}

	t.Run("without full body", func(t *testing.T) {
		json := node.ToJSON(false)
		if json.ID != "test-id" {
			t.Errorf("expected id 'test-id', got %s", json.ID)
		}
		if json.Body != "" {
			t.Error("body should be empty when includeFull is false")
		}
		if !json.Matched {
			t.Error("matched should be true")
		}
		if len(json.Children) != 1 {
			t.Errorf("expected 1 child, got %d", len(json.Children))
		}
	})

	t.Run("with full body", func(t *testing.T) {
		json := node.ToJSON(true)
		if json.Body != "Test body content" {
			t.Errorf("expected body content, got %s", json.Body)
		}
	})
}

func TestFlattenTreeFiltered(t *testing.T) {
	// Build a tree:
	// m1 (milestone)
	//   ├── e1 (epic)
	//   │   ├── t1 (task)
	//   │   └── t2 (task)
	//   └── e2 (epic)
	// t3 (orphan task)

	tree := []*TreeNode{
		{
			Nib: &nib.Nib{ID: "m1", Title: "Milestone 1"}, Matched: true,
			Children: []*TreeNode{
				{
					Nib: &nib.Nib{ID: "e1", Title: "Epic 1"}, Matched: true,
					Children: []*TreeNode{
						{Nib: &nib.Nib{ID: "t1", Title: "Task 1"}, Matched: true},
						{Nib: &nib.Nib{ID: "t2", Title: "Task 2"}, Matched: true},
					},
				},
				{Nib: &nib.Nib{ID: "e2", Title: "Epic 2"}, Matched: true},
			},
		},
		{Nib: &nib.Nib{ID: "t3", Title: "Task 3"}, Matched: true},
	}

	t.Run("no collapsed nodes returns all items", func(t *testing.T) {
		items := FlattenTreeFiltered(tree, nil)
		expectedIDs := []string{"m1", "e1", "t1", "t2", "e2", "t3"}
		if len(items) != len(expectedIDs) {
			t.Fatalf("expected %d items, got %d", len(expectedIDs), len(items))
		}
		for i, id := range expectedIDs {
			if items[i].Nib.ID != id {
				t.Errorf("item %d: expected ID %s, got %s", i, id, items[i].Nib.ID)
			}
		}
	})

	t.Run("collapse parent hides children", func(t *testing.T) {
		collapsed := map[string]bool{"e1": true}
		items := FlattenTreeFiltered(tree, collapsed)
		// e1 is collapsed, so t1 and t2 are hidden
		expectedIDs := []string{"m1", "e1", "e2", "t3"}
		if len(items) != len(expectedIDs) {
			t.Fatalf("expected %d items, got %d", len(expectedIDs), len(items))
		}
		for i, id := range expectedIDs {
			if items[i].Nib.ID != id {
				t.Errorf("item %d: expected ID %s, got %s", i, id, items[i].Nib.ID)
			}
		}
	})

	t.Run("collapse grandparent hides all descendants", func(t *testing.T) {
		collapsed := map[string]bool{"m1": true}
		items := FlattenTreeFiltered(tree, collapsed)
		// m1 is collapsed, so e1, t1, t2, e2 are all hidden
		expectedIDs := []string{"m1", "t3"}
		if len(items) != len(expectedIDs) {
			t.Fatalf("expected %d items, got %d", len(expectedIDs), len(items))
		}
		for i, id := range expectedIDs {
			if items[i].Nib.ID != id {
				t.Errorf("item %d: expected ID %s, got %s", i, id, items[i].Nib.ID)
			}
		}
	})

	t.Run("collapse leaf has no effect", func(t *testing.T) {
		collapsed := map[string]bool{"t1": true}
		items := FlattenTreeFiltered(tree, collapsed)
		// t1 has no children, so collapsing it changes nothing
		expectedIDs := []string{"m1", "e1", "t1", "t2", "e2", "t3"}
		if len(items) != len(expectedIDs) {
			t.Fatalf("expected %d items, got %d", len(expectedIDs), len(items))
		}
	})

	t.Run("HasChildren and Collapsed flags", func(t *testing.T) {
		collapsed := map[string]bool{"e1": true}
		items := FlattenTreeFiltered(tree, collapsed)

		// m1 has children, expanded
		if !items[0].HasChildren {
			t.Error("m1 should have HasChildren=true")
		}
		if items[0].Collapsed {
			t.Error("m1 should have Collapsed=false")
		}

		// e1 has children, collapsed
		if !items[1].HasChildren {
			t.Error("e1 should have HasChildren=true")
		}
		if !items[1].Collapsed {
			t.Error("e1 should have Collapsed=true")
		}

		// e2 is a leaf (no children)
		if items[2].HasChildren {
			t.Error("e2 should have HasChildren=false")
		}

		// t3 is a leaf
		if items[3].HasChildren {
			t.Error("t3 should have HasChildren=false")
		}
	})

	t.Run("collapse indicators in prefix", func(t *testing.T) {
		collapsed := map[string]bool{"e1": true}
		items := FlattenTreeFiltered(tree, collapsed)

		// m1 (root, has children, expanded) - prefix should end with expanded indicator
		if !strings.HasSuffix(items[0].TreePrefix, CollapseIndicatorExpanded) {
			t.Errorf("m1 prefix should end with expanded indicator, got %q", items[0].TreePrefix)
		}

		// e1 (nested, has children, collapsed) - prefix should end with collapsed indicator
		if !strings.HasSuffix(items[1].TreePrefix, CollapseIndicatorCollapsed) {
			t.Errorf("e1 prefix should end with collapsed indicator, got %q", items[1].TreePrefix)
		}

		// e2 (nested, leaf) - prefix should end with leaf indicator
		if !strings.HasSuffix(items[2].TreePrefix, CollapseIndicatorLeaf) {
			t.Errorf("e2 prefix should end with leaf indicator, got %q", items[2].TreePrefix)
		}

		// t3 (root, leaf) - prefix should end with leaf indicator
		if !strings.HasSuffix(items[3].TreePrefix, CollapseIndicatorLeaf) {
			t.Errorf("t3 prefix should end with leaf indicator, got %q", items[3].TreePrefix)
		}
	})

	t.Run("tree prefixes correct after collapse", func(t *testing.T) {
		// Collapse e1, so only m1, e1(collapsed), e2, t3 are visible
		collapsed := map[string]bool{"e1": true}
		items := FlattenTreeFiltered(tree, collapsed)

		// e1 is first child (not last) under m1: should have ├─ connector
		if !strings.Contains(items[1].TreePrefix, "├─") {
			t.Errorf("e1 should have ├─ connector, got %q", items[1].TreePrefix)
		}

		// e2 is last child under m1: should have └─ connector
		if !strings.Contains(items[2].TreePrefix, "└─") {
			t.Errorf("e2 should have └─ connector, got %q", items[2].TreePrefix)
		}
	})
}

func TestHasAnyChildren(t *testing.T) {
	t.Run("tree with children", func(t *testing.T) {
		tree := []*TreeNode{
			{
				Nib: &nib.Nib{ID: "m1"},
				Children: []*TreeNode{
					{Nib: &nib.Nib{ID: "t1"}},
				},
			},
		}
		if !HasAnyChildren(tree) {
			t.Error("expected HasAnyChildren to return true")
		}
	})

	t.Run("flat list no children", func(t *testing.T) {
		tree := []*TreeNode{
			{Nib: &nib.Nib{ID: "t1"}},
			{Nib: &nib.Nib{ID: "t2"}},
		}
		if HasAnyChildren(tree) {
			t.Error("expected HasAnyChildren to return false")
		}
	})
}

func TestCollectParentIDs(t *testing.T) {
	tree := []*TreeNode{
		{
			Nib: &nib.Nib{ID: "m1"},
			Children: []*TreeNode{
				{
					Nib: &nib.Nib{ID: "e1"},
					Children: []*TreeNode{
						{Nib: &nib.Nib{ID: "t1"}},
					},
				},
			},
		},
		{Nib: &nib.Nib{ID: "t2"}},
	}

	ids := make(map[string]bool)
	CollectParentIDs(tree, ids)

	if !ids["m1"] {
		t.Error("m1 should be a parent")
	}
	if !ids["e1"] {
		t.Error("e1 should be a parent")
	}
	if ids["t1"] {
		t.Error("t1 should not be a parent")
	}
	if ids["t2"] {
		t.Error("t2 should not be a parent")
	}
}
