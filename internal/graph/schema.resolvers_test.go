package graph

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/99designs/gqlgen/graphql"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph/model"
)

func setupTestResolver(t *testing.T) (*Resolver, *nibcore.Core) {
	t.Helper()
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatalf("failed to create test .nibs dir: %v", err)
	}

	cfg := config.Default()
	core := nibcore.New(nibsDir, cfg)
	if err := core.Load(); err != nil {
		t.Fatalf("failed to load core: %v", err)
	}

	return &Resolver{Reader: core, Writer: core, Validator: core, Blocking: core, Orderer: NewOrderer(core, core)}, core
}

func mustCreate(t *testing.T, core *nibcore.Core, b *nib.Nib) {
	t.Helper()
	if err := core.Create(b); err != nil {
		t.Fatalf("failed to create nib %s: %v", b.ID, err)
	}
}

func createTestNib(t *testing.T, core *nibcore.Core, id, title, status string) *nib.Nib {
	t.Helper()
	b := &nib.Nib{
		ID:     id,
		Slug:   nib.Slugify(title),
		Title:  title,
		Status: status,
	}
	if err := core.Create(b); err != nil {
		t.Fatalf("failed to create test nib: %v", err)
	}
	return b
}

// TestNibFieldResolversApplyPresentationDefaults pins the behavior-preservation
// contract for nibs-7d3o: the stored Nib keeps Type/Priority EMPTY when the file
// omits them (so the etag witnesses the on-disk bytes), but the non-nullable
// GraphQL fields must still resolve the "task"/"normal" presentation defaults —
// exactly what a client saw before loadNib stopped synthesizing them. Uses a
// fresh Core.Load to exercise the reload path where the false-conflict bug lived.
func TestNibFieldResolversApplyPresentationDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(nibsDir, name), []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	// Omits `priority:` (what CreateNib writes without a priority); `type:` present.
	write("nopri1--x.md", "---\nversion: 1\ntitle: No Priority\nstatus: todo\ntype: bug\ncreated_at: 2026-01-02T03:04:05Z\nupdated_at: 2026-01-02T03:04:05Z\n---\n\nBody.\n")
	// Omits `type:` (hand-authored); explicit priority preserved.
	write("notype1--y.md", "---\nversion: 1\ntitle: No Type\nstatus: todo\npriority: high\ncreated_at: 2026-01-02T03:04:05Z\nupdated_at: 2026-01-02T03:04:05Z\n---\n\nBody.\n")

	core := nibcore.New(nibsDir, config.Default())
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	resolver := &Resolver{Reader: core, Writer: core, Validator: core, Blocking: core, Orderer: NewOrderer(core, core)}
	ctx := context.Background()
	nr := resolver.Nib()

	t.Run("priority-less nib: stored empty, resolver returns normal", func(t *testing.T) {
		b, err := resolver.Query().Nib(ctx, "nopri1")
		if err != nil {
			t.Fatalf("Nib(): %v", err)
		}
		if b.Priority != "" {
			t.Errorf("stored Priority = %q, want empty (must not be synthesized onto the struct)", b.Priority)
		}
		if got, err := nr.Priority(ctx, b); err != nil || got != "normal" {
			t.Errorf("Priority resolver = %q, %v; want \"normal\", nil", got, err)
		}
		if got, err := nr.Type(ctx, b); err != nil || got != "bug" {
			t.Errorf("Type resolver = %q, %v; want \"bug\" (explicit), nil", got, err)
		}
	})

	t.Run("type-less nib: stored empty, resolver returns task", func(t *testing.T) {
		b, err := resolver.Query().Nib(ctx, "notype1")
		if err != nil {
			t.Fatalf("Nib(): %v", err)
		}
		if b.Type != "" {
			t.Errorf("stored Type = %q, want empty (must not be synthesized onto the struct)", b.Type)
		}
		if got, err := nr.Type(ctx, b); err != nil || got != "task" {
			t.Errorf("Type resolver = %q, %v; want \"task\", nil", got, err)
		}
		if got, err := nr.Priority(ctx, b); err != nil || got != "high" {
			t.Errorf("Priority resolver = %q, %v; want \"high\" (explicit), nil", got, err)
		}
	})
}

func TestQueryNib(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	// Create test nib
	createTestNib(t, core, "test-1", "Test Nib", "todo")

	// Test exact match
	t.Run("exact match", func(t *testing.T) {
		qr := resolver.Query()
		got, err := qr.Nib(ctx, "test-1")
		if err != nil {
			t.Fatalf("Nib() error = %v", err)
		}
		if got == nil {
			t.Fatal("Nib() returned nil")
			return
		}
		if got.ID != "test-1" {
			t.Errorf("Nib().ID = %q, want %q", got.ID, "test-1")
		}
	})

	// Test partial ID not found (no prefix matching)
	t.Run("partial ID not found", func(t *testing.T) {
		qr := resolver.Query()
		got, err := qr.Nib(ctx, "test")
		if err != nil {
			t.Fatalf("Nib() error = %v", err)
		}
		if got != nil {
			t.Errorf("Nib() = %v, want nil (partial IDs should not match)", got)
		}
	})

	// Test not found
	t.Run("not found", func(t *testing.T) {
		qr := resolver.Query()
		got, err := qr.Nib(ctx, "nonexistent")
		if err != nil {
			t.Fatalf("Nib() error = %v", err)
		}
		if got != nil {
			t.Errorf("Nib() = %v, want nil", got)
		}
	})
}

func TestQueryNibs(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	// Create test nibs
	createTestNib(t, core, "nib-1", "First Nib", "todo")
	createTestNib(t, core, "nib-2", "Second Nib", "in-progress")
	createTestNib(t, core, "nib-3", "Third Nib", "completed")

	t.Run("no filter", func(t *testing.T) {
		qr := resolver.Query()
		got, err := qr.Nibs(ctx, nil, nil)
		if err != nil {
			t.Fatalf("Nibs() error = %v", err)
		}
		if len(got) != 3 {
			t.Errorf("Nibs() count = %d, want 3", len(got))
		}
	})

	t.Run("filter by status", func(t *testing.T) {
		qr := resolver.Query()
		filter := &model.NibFilter{
			Status: []string{"todo"},
		}
		got, err := qr.Nibs(ctx, filter, nil)
		if err != nil {
			t.Fatalf("Nibs() error = %v", err)
		}
		if len(got) != 1 {
			t.Errorf("Nibs() count = %d, want 1", len(got))
		}
		if got[0].ID != "nib-1" {
			t.Errorf("Nibs()[0].ID = %q, want %q", got[0].ID, "nib-1")
		}
	})

	t.Run("filter by multiple statuses", func(t *testing.T) {
		qr := resolver.Query()
		filter := &model.NibFilter{
			Status: []string{"todo", "in-progress"},
		}
		got, err := qr.Nibs(ctx, filter, nil)
		if err != nil {
			t.Fatalf("Nibs() error = %v", err)
		}
		if len(got) != 2 {
			t.Errorf("Nibs() count = %d, want 2", len(got))
		}
	})

	t.Run("exclude status", func(t *testing.T) {
		qr := resolver.Query()
		filter := &model.NibFilter{
			ExcludeStatus: []string{"completed"},
		}
		got, err := qr.Nibs(ctx, filter, nil)
		if err != nil {
			t.Fatalf("Nibs() error = %v", err)
		}
		if len(got) != 2 {
			t.Errorf("Nibs() count = %d, want 2", len(got))
		}
	})
}

func TestQueryNibsWithTags(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	// Create test nibs with tags
	b1 := &nib.Nib{ID: "tag-1", Title: "Tagged 1", Status: "todo", Tags: []string{"frontend", "urgent"}}
	b2 := &nib.Nib{ID: "tag-2", Title: "Tagged 2", Status: "todo", Tags: []string{"backend"}}
	b3 := &nib.Nib{ID: "tag-3", Title: "No Tags", Status: "todo"}
	if err := core.Create(b1); err != nil {
		t.Fatal(err)
	}
	if err := core.Create(b2); err != nil {
		t.Fatal(err)
	}
	if err := core.Create(b3); err != nil {
		t.Fatal(err)
	}

	t.Run("filter by tag", func(t *testing.T) {
		qr := resolver.Query()
		filter := &model.NibFilter{
			Tags: []string{"frontend"},
		}
		got, err := qr.Nibs(ctx, filter, nil)
		if err != nil {
			t.Fatalf("Nibs() error = %v", err)
		}
		if len(got) != 1 {
			t.Errorf("Nibs() count = %d, want 1", len(got))
		}
	})

	t.Run("filter by multiple tags (OR)", func(t *testing.T) {
		qr := resolver.Query()
		filter := &model.NibFilter{
			Tags: []string{"frontend", "backend"},
		}
		got, err := qr.Nibs(ctx, filter, nil)
		if err != nil {
			t.Fatalf("Nibs() error = %v", err)
		}
		if len(got) != 2 {
			t.Errorf("Nibs() count = %d, want 2", len(got))
		}
	})

	t.Run("exclude by tag", func(t *testing.T) {
		qr := resolver.Query()
		filter := &model.NibFilter{
			ExcludeTags: []string{"urgent"},
		}
		got, err := qr.Nibs(ctx, filter, nil)
		if err != nil {
			t.Fatalf("Nibs() error = %v", err)
		}
		if len(got) != 2 {
			t.Errorf("Nibs() count = %d, want 2", len(got))
		}
	})
}

func TestQueryNibsWithPriority(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	// Create test nibs with various priorities
	// Empty priority should be treated as "normal"
	b1 := &nib.Nib{ID: "pri-1", Title: "Critical", Status: "todo", Priority: "critical"}
	b2 := &nib.Nib{ID: "pri-2", Title: "High", Status: "todo", Priority: "high"}
	b3 := &nib.Nib{ID: "pri-3", Title: "Normal Explicit", Status: "todo", Priority: "normal"}
	b4 := &nib.Nib{ID: "pri-4", Title: "Normal Implicit", Status: "todo", Priority: ""} // empty = normal
	b5 := &nib.Nib{ID: "pri-5", Title: "Low", Status: "todo", Priority: "low"}
	for _, b := range []*nib.Nib{b1, b2, b3, b4, b5} {
		if err := core.Create(b); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("filter by normal includes empty priority", func(t *testing.T) {
		qr := resolver.Query()
		filter := &model.NibFilter{
			Priority: []string{"normal"},
		}
		got, err := qr.Nibs(ctx, filter, nil)
		if err != nil {
			t.Fatalf("Nibs() error = %v", err)
		}
		// Should include both explicit "normal" and implicit (empty) priority
		if len(got) != 2 {
			t.Errorf("Nibs() count = %d, want 2", len(got))
		}
		ids := make(map[string]bool)
		for _, b := range got {
			ids[b.ID] = true
		}
		if !ids["pri-3"] || !ids["pri-4"] {
			t.Errorf("Nibs() should include pri-3 and pri-4, got %v", ids)
		}
	})

	t.Run("filter by critical", func(t *testing.T) {
		qr := resolver.Query()
		filter := &model.NibFilter{
			Priority: []string{"critical"},
		}
		got, err := qr.Nibs(ctx, filter, nil)
		if err != nil {
			t.Fatalf("Nibs() error = %v", err)
		}
		if len(got) != 1 {
			t.Errorf("Nibs() count = %d, want 1", len(got))
		}
		if got[0].ID != "pri-1" {
			t.Errorf("Nibs()[0].ID = %q, want %q", got[0].ID, "pri-1")
		}
	})

	t.Run("filter by multiple priorities", func(t *testing.T) {
		qr := resolver.Query()
		filter := &model.NibFilter{
			Priority: []string{"critical", "high"},
		}
		got, err := qr.Nibs(ctx, filter, nil)
		if err != nil {
			t.Fatalf("Nibs() error = %v", err)
		}
		if len(got) != 2 {
			t.Errorf("Nibs() count = %d, want 2", len(got))
		}
	})

	t.Run("exclude normal excludes empty priority", func(t *testing.T) {
		qr := resolver.Query()
		filter := &model.NibFilter{
			ExcludePriority: []string{"normal"},
		}
		got, err := qr.Nibs(ctx, filter, nil)
		if err != nil {
			t.Fatalf("Nibs() error = %v", err)
		}
		// Should exclude both explicit "normal" and implicit (empty) priority
		if len(got) != 3 {
			t.Errorf("Nibs() count = %d, want 3", len(got))
		}
		for _, b := range got {
			if b.ID == "pri-3" || b.ID == "pri-4" {
				t.Errorf("Nibs() should not include %s", b.ID)
			}
		}
	})
}

func TestNibRelationships(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	// Create nibs with relationships
	parent := &nib.Nib{ID: "parent-1", Title: "Parent", Status: "todo"}
	child1 := &nib.Nib{
		ID:        "child-1",
		Title:     "Child 1",
		Status:    "todo",
		Parent:    "parent-1",
		BlockedBy: []string{"blocker-1"},
	}
	child2 := &nib.Nib{
		ID:     "child-2",
		Title:  "Child 2",
		Status: "todo",
		Parent: "parent-1",
	}
	blocker := &nib.Nib{
		ID:     "blocker-1",
		Title:  "Blocker",
		Status: "todo",
	}

	for _, b := range []*nib.Nib{parent, child1, child2, blocker} {
		if err := core.Create(b); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("parent resolver", func(t *testing.T) {
		br := resolver.Nib()
		got, err := br.Parent(ctx, child1)
		if err != nil {
			t.Fatalf("Parent() error = %v", err)
		}
		if got == nil {
			t.Fatal("Parent() returned nil")
			return
		}
		if got.ID != "parent-1" {
			t.Errorf("Parent().ID = %q, want %q", got.ID, "parent-1")
		}
	})

	t.Run("children resolver", func(t *testing.T) {
		br := resolver.Nib()
		got, err := br.Children(ctx, parent, nil, nil)
		if err != nil {
			t.Fatalf("Children() error = %v", err)
		}
		if len(got) != 2 {
			t.Errorf("Children() count = %d, want 2", len(got))
		}
	})

	t.Run("blockedBy resolver", func(t *testing.T) {
		br := resolver.Nib()
		got, err := br.BlockedBy(ctx, child1, nil)
		if err != nil {
			t.Fatalf("BlockedBy() error = %v", err)
		}
		if len(got) != 1 {
			t.Errorf("BlockedBy() count = %d, want 1", len(got))
		}
		if got[0].ID != "blocker-1" {
			t.Errorf("BlockedBy()[0].ID = %q, want %q", got[0].ID, "blocker-1")
		}
	})

	t.Run("blocks resolver", func(t *testing.T) {
		br := resolver.Nib()
		got, err := br.Blocking(ctx, blocker, nil)
		if err != nil {
			t.Fatalf("Blocks() error = %v", err)
		}
		if len(got) != 1 {
			t.Errorf("Blocks() count = %d, want 1", len(got))
		}
		if got[0].ID != "child-1" {
			t.Errorf("Blocks()[0].ID = %q, want %q", got[0].ID, "child-1")
		}
	})
}

func TestBlockedByFieldResolvers(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	// Setup: blocker has no Blocking field; blocked nib uses BlockedBy field instead
	blocker := &nib.Nib{
		ID:     "bb-blocker",
		Title:  "Blocker",
		Status: "todo",
	}
	blocked := &nib.Nib{
		ID:        "bb-blocked",
		Title:     "Blocked",
		Status:    "todo",
		BlockedBy: []string{"bb-blocker"},
	}
	mustCreate(t, core, blocker)
	mustCreate(t, core, blocked)

	t.Run("blockedBy resolver includes own BlockedBy field", func(t *testing.T) {
		br := resolver.Nib()
		got, err := br.BlockedBy(ctx, blocked, nil)
		if err != nil {
			t.Fatalf("BlockedBy() error = %v", err)
		}
		if len(got) != 1 {
			t.Errorf("BlockedBy() count = %d, want 1", len(got))
		}
		if len(got) > 0 && got[0].ID != "bb-blocker" {
			t.Errorf("BlockedBy()[0].ID = %q, want %q", got[0].ID, "bb-blocker")
		}
	})

	t.Run("blocking resolver includes incoming blocked_by links", func(t *testing.T) {
		br := resolver.Nib()
		got, err := br.Blocking(ctx, blocker, nil)
		if err != nil {
			t.Fatalf("Blocking() error = %v", err)
		}
		if len(got) != 1 {
			t.Errorf("Blocking() count = %d, want 1", len(got))
		}
		if len(got) > 0 && got[0].ID != "bb-blocked" {
			t.Errorf("Blocking()[0].ID = %q, want %q", got[0].ID, "bb-blocked")
		}
	})

	t.Run("deduplicates when both sides express same relationship", func(t *testing.T) {
		// Both blocker.Blocking and blocked.BlockedBy point to same relationship
		bothSides := &nib.Nib{
			ID:       "bb-both-blocker",
			Title:    "Both Sides Blocker",
			Status:   "todo",
			Blocking: []string{"bb-both-blocked"},
		}
		bothBlocked := &nib.Nib{
			ID:        "bb-both-blocked",
			Title:     "Both Sides Blocked",
			Status:    "todo",
			BlockedBy: []string{"bb-both-blocker"},
		}
		mustCreate(t, core, bothSides)
		mustCreate(t, core, bothBlocked)

		br := resolver.Nib()

		// BlockedBy should return only 1, not 2
		got, err := br.BlockedBy(ctx, bothBlocked, nil)
		if err != nil {
			t.Fatalf("BlockedBy() error = %v", err)
		}
		if len(got) != 1 {
			t.Errorf("BlockedBy() count = %d, want 1 (deduplicated)", len(got))
		}

		// Blocking should return only 1, not 2
		got, err = br.Blocking(ctx, bothSides, nil)
		if err != nil {
			t.Fatalf("Blocking() error = %v", err)
		}
		if len(got) != 1 {
			t.Errorf("Blocking() count = %d, want 1 (deduplicated)", len(got))
		}
	})
}

func TestBrokenLinksFiltered(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	// Create nib with broken link
	b := &nib.Nib{
		ID:     "orphan-1",
		Title:  "Orphan",
		Status: "todo",
		Parent: "nonexistent",
	}
	mustCreate(t, core, b)

	t.Run("broken parent link returns nil", func(t *testing.T) {
		br := resolver.Nib()
		got, err := br.Parent(ctx, b)
		if err != nil {
			t.Fatalf("Parent() error = %v", err)
		}
		if got != nil {
			t.Errorf("Parent() = %v, want nil for broken link", got)
		}
	})
}

func TestQueryNibsWithParentAndBlocks(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	// Create nibs with various relationship configurations
	noRels := &nib.Nib{ID: "no-rels", Title: "No Relationships", Status: "todo"}
	hasParent := &nib.Nib{
		ID:     "has-parent",
		Title:  "Has Parent",
		Status: "todo",
		Parent: "no-rels",
	}
	hasBlocks := &nib.Nib{
		ID:     "has-blocks",
		Title:  "Has Blocks",
		Status: "todo",
	}

	mustCreate(t, core, noRels)
	// has-parent is blocked by has-blocks (single-side: stored on has-parent)
	hasParent.BlockedBy = []string{"has-blocks"}
	mustCreate(t, core, hasParent)
	mustCreate(t, core, hasBlocks)

	t.Run("filter hasParent", func(t *testing.T) {
		qr := resolver.Query()
		hasParentBool := true
		filter := &model.NibFilter{
			HasParent: &hasParentBool,
		}
		got, err := qr.Nibs(ctx, filter, nil)
		if err != nil {
			t.Fatalf("Nibs() error = %v", err)
		}
		if len(got) != 1 {
			t.Errorf("Nibs() count = %d, want 1", len(got))
		}
		if got[0].ID != "has-parent" {
			t.Errorf("Nibs()[0].ID = %q, want %q", got[0].ID, "has-parent")
		}
	})

	t.Run("filter noParent", func(t *testing.T) {
		qr := resolver.Query()
		noParentBool := true
		filter := &model.NibFilter{
			NoParent: &noParentBool,
		}
		got, err := qr.Nibs(ctx, filter, nil)
		if err != nil {
			t.Fatalf("Nibs() error = %v", err)
		}
		if len(got) != 2 {
			t.Errorf("Nibs() count = %d, want 2", len(got))
		}
	})

	t.Run("filter hasBlocks", func(t *testing.T) {
		qr := resolver.Query()
		hasBlocksBool := true
		filter := &model.NibFilter{
			HasBlocking: &hasBlocksBool,
		}
		got, err := qr.Nibs(ctx, filter, nil)
		if err != nil {
			t.Fatalf("Nibs() error = %v", err)
		}
		if len(got) != 1 {
			t.Errorf("Nibs() count = %d, want 1", len(got))
		}
		if got[0].ID != "has-blocks" {
			t.Errorf("Nibs()[0].ID = %q, want %q", got[0].ID, "has-blocks")
		}
	})

	t.Run("filter isBlocked true", func(t *testing.T) {
		qr := resolver.Query()
		isBlockedBool := true
		filter := &model.NibFilter{
			IsBlocked: &isBlockedBool,
		}
		got, err := qr.Nibs(ctx, filter, nil)
		if err != nil {
			t.Fatalf("Nibs() error = %v", err)
		}
		if len(got) != 1 {
			t.Errorf("Nibs() count = %d, want 1", len(got))
		}
		if got[0].ID != "has-parent" {
			t.Errorf("Nibs()[0].ID = %q, want %q", got[0].ID, "has-parent")
		}
	})

	t.Run("filter isBlocked false", func(t *testing.T) {
		qr := resolver.Query()
		isBlockedBool := false
		filter := &model.NibFilter{
			IsBlocked: &isBlockedBool,
		}
		got, err := qr.Nibs(ctx, filter, nil)
		if err != nil {
			t.Fatalf("Nibs() error = %v", err)
		}
		// Should return all nibs except "has-parent" (which is blocked by "has-blocks")
		if len(got) != 2 {
			t.Errorf("Nibs() count = %d, want 2", len(got))
		}
		// Verify "has-parent" is not in results
		for _, b := range got {
			if b.ID == "has-parent" {
				t.Errorf("Nibs() should not contain blocked nib 'has-parent'")
			}
		}
	})

	t.Run("filter by parentId", func(t *testing.T) {
		qr := resolver.Query()
		parentID := "no-rels"
		filter := &model.NibFilter{
			ParentID: &parentID,
		}
		got, err := qr.Nibs(ctx, filter, nil)
		if err != nil {
			t.Fatalf("Nibs() error = %v", err)
		}
		if len(got) != 1 {
			t.Errorf("Nibs() count = %d, want 1", len(got))
		}
		if got[0].ID != "has-parent" {
			t.Errorf("Nibs()[0].ID = %q, want %q", got[0].ID, "has-parent")
		}
	})
}

func TestIsBlockedFilterWithResolvedBlockers(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	// Create nibs to test blocking with various blocker statuses (single-side: blockedBy on targets)
	activeBlocker := &nib.Nib{ID: "active-blocker", Title: "Active Blocker", Status: "todo"}
	completedBlocker := &nib.Nib{ID: "completed-blocker", Title: "Completed Blocker", Status: "completed"}
	scrappedBlocker := &nib.Nib{ID: "scrapped-blocker", Title: "Scrapped Blocker", Status: "scrapped"}
	blockedByActive := &nib.Nib{
		ID: "blocked-by-active", Title: "Blocked by Active", Status: "todo",
		BlockedBy: []string{"active-blocker"},
	}
	blockedByCompleted := &nib.Nib{
		ID: "blocked-by-completed", Title: "Blocked by Completed", Status: "todo",
		BlockedBy: []string{"completed-blocker"},
	}
	blockedByScrapped := &nib.Nib{
		ID: "blocked-by-scrapped", Title: "Blocked by Scrapped", Status: "todo",
		BlockedBy: []string{"scrapped-blocker"},
	}
	notBlocked := &nib.Nib{ID: "not-blocked", Title: "Not Blocked", Status: "todo"}
	mixedBlocker := &nib.Nib{ID: "mixed-blocker", Title: "Mixed Blocker (active)", Status: "in-progress"}
	mixedBlockerCompleted := &nib.Nib{ID: "mixed-blocker-completed", Title: "Mixed Blocker (completed)", Status: "completed"}
	mixedBlocked := &nib.Nib{
		ID: "mixed-blocked", Title: "Mixed Blocked", Status: "todo",
		BlockedBy: []string{"mixed-blocker", "mixed-blocker-completed"},
	}

	nibs := []*nib.Nib{
		activeBlocker, completedBlocker, scrappedBlocker,
		blockedByActive, blockedByCompleted, blockedByScrapped,
		notBlocked, mixedBlocker, mixedBlockerCompleted, mixedBlocked,
	}
	for _, b := range nibs {
		if err := core.Create(b); err != nil {
			t.Fatalf("Create error: %v", err)
		}
	}

	t.Run("isBlocked true returns only nibs with active blockers", func(t *testing.T) {
		qr := resolver.Query()
		isBlocked := true
		filter := &model.NibFilter{
			IsBlocked: &isBlocked,
		}
		got, err := qr.Nibs(ctx, filter, nil)
		if err != nil {
			t.Fatalf("Nibs() error = %v", err)
		}

		// Should only return nibs blocked by active blockers
		ids := make(map[string]bool)
		for _, b := range got {
			ids[b.ID] = true
		}

		if !ids["blocked-by-active"] {
			t.Error("expected blocked-by-active in results (has active blocker)")
		}
		if !ids["mixed-blocked"] {
			t.Error("expected mixed-blocked in results (has one active blocker)")
		}
		if ids["blocked-by-completed"] {
			t.Error("blocked-by-completed should NOT be in results (blocker is completed)")
		}
		if ids["blocked-by-scrapped"] {
			t.Error("blocked-by-scrapped should NOT be in results (blocker is scrapped)")
		}
		if ids["not-blocked"] {
			t.Error("not-blocked should NOT be in results (no blockers)")
		}
	})

	t.Run("isBlocked false excludes nibs with active blockers", func(t *testing.T) {
		qr := resolver.Query()
		isBlocked := false
		filter := &model.NibFilter{
			IsBlocked: &isBlocked,
		}
		got, err := qr.Nibs(ctx, filter, nil)
		if err != nil {
			t.Fatalf("Nibs() error = %v", err)
		}

		ids := make(map[string]bool)
		for _, b := range got {
			ids[b.ID] = true
		}

		// Should include nibs with no active blockers
		if !ids["blocked-by-completed"] {
			t.Error("expected blocked-by-completed in results (blocker is completed)")
		}
		if !ids["blocked-by-scrapped"] {
			t.Error("expected blocked-by-scrapped in results (blocker is scrapped)")
		}
		if !ids["not-blocked"] {
			t.Error("expected not-blocked in results (no blockers)")
		}
		// Should exclude nibs with active blockers
		if ids["blocked-by-active"] {
			t.Error("blocked-by-active should NOT be in results (has active blocker)")
		}
		if ids["mixed-blocked"] {
			t.Error("mixed-blocked should NOT be in results (has active blocker)")
		}
	})
}

func TestMutationCreateNib(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	t.Run("create with required fields only", func(t *testing.T) {
		mr := resolver.Mutation()
		input := model.CreateNibInput{
			Title: "New Nib",
		}
		got, err := mr.CreateNib(ctx, input)
		if err != nil {
			t.Fatalf("CreateNib() error = %v", err)
		}
		if got == nil {
			t.Fatal("CreateNib() returned nil")
			return
		}
		if got.Title != "New Nib" {
			t.Errorf("CreateNib().Title = %q, want %q", got.Title, "New Nib")
		}
		// Type defaults to "task"
		if got.Type != "task" {
			t.Errorf("CreateNib().Type = %q, want %q (default)", got.Type, "task")
		}
		if got.ID == "" {
			t.Error("CreateNib().ID is empty")
		}
	})

	t.Run("create with all fields", func(t *testing.T) {
		// Create parent and target nibs first
		parentNib := &nib.Nib{
			ID:     "some-parent",
			Title:  "Parent Nib",
			Status: "todo",
			Type:   "epic",
		}
		targetNib := &nib.Nib{
			ID:     "some-target",
			Title:  "Target Nib",
			Status: "todo",
			Type:   "task",
		}
		mustCreate(t, core, parentNib)
		mustCreate(t, core, targetNib)

		mr := resolver.Mutation()
		nibType := "feature"
		status := "in-progress"
		priority := "high"
		body := "Test body content"
		parent := "some-parent"
		input := model.CreateNibInput{
			Title:    "Full Nib",
			Type:     &nibType,
			Status:   &status,
			Priority: &priority,
			Body:     &body,
			Tags:     []string{"tag1", "tag2"},
			Parent:   &parent,
			Blocking: []string{"some-target"},
		}
		got, err := mr.CreateNib(ctx, input)
		if err != nil {
			t.Fatalf("CreateNib() error = %v", err)
		}
		if got.Type != "feature" {
			t.Errorf("CreateNib().Type = %q, want %q", got.Type, "feature")
		}
		if got.Status != "in-progress" {
			t.Errorf("CreateNib().Status = %q, want %q", got.Status, "in-progress")
		}
		if got.Priority != "high" {
			t.Errorf("CreateNib().Priority = %q, want %q", got.Priority, "high")
		}
		if got.Body != "Test body content" {
			t.Errorf("CreateNib().Body = %q, want %q", got.Body, "Test body content")
		}
		if len(got.Tags) != 2 {
			t.Errorf("CreateNib().Tags count = %d, want 2", len(got.Tags))
		}
		if got.Parent != "some-parent" {
			t.Errorf("CreateNib().Parent = %q, want %q", got.Parent, "some-parent")
		}
		// Single-side: Blocking field not on created nib; target's blockedBy should be updated
		if len(got.Blocking) != 0 {
			t.Errorf("CreateNib().Blocking should be empty (single-side storage), got %v", got.Blocking)
		}
		targetAfter, _ := core.Get("some-target")
		if !targetAfter.IsBlockedBy(got.ID) {
			t.Errorf("target.BlockedBy should contain created nib %s, got %v", got.ID, targetAfter.BlockedBy)
		}
	})
}

func TestMutationCreateNibWithCustomPrefix(t *testing.T) {
	resolver, _ := setupTestResolver(t)
	ctx := context.Background()

	t.Run("create with custom prefix", func(t *testing.T) {
		mr := resolver.Mutation()
		customPrefix := "SYNC-TASK-"
		input := model.CreateNibInput{
			Title:  "Custom Prefix Nib",
			Prefix: &customPrefix,
		}
		got, err := mr.CreateNib(ctx, input)
		if err != nil {
			t.Fatalf("CreateNib() error = %v", err)
		}
		if got == nil {
			t.Fatal("CreateNib() returned nil")
			return
		}
		// ID should start with the custom prefix
		if !strings.HasPrefix(got.ID, "SYNC-TASK-") {
			t.Errorf("CreateNib().ID = %q, want prefix %q", got.ID, "SYNC-TASK-")
		}
		// ID should be prefix + 4 chars (default length)
		if len(got.ID) != len("SYNC-TASK-")+4 {
			t.Errorf("CreateNib().ID length = %d, want %d", len(got.ID), len("SYNC-TASK-")+4)
		}
	})

	t.Run("create without prefix uses config default", func(t *testing.T) {
		mr := resolver.Mutation()
		input := model.CreateNibInput{
			Title: "No Custom Prefix Nib",
		}
		got, err := mr.CreateNib(ctx, input)
		if err != nil {
			t.Fatalf("CreateNib() error = %v", err)
		}
		// Without custom prefix, should use config default (empty in test setup)
		// ID should just be 4 chars
		if len(got.ID) != 4 {
			t.Errorf("CreateNib().ID length = %d, want 4", len(got.ID))
		}
	})

	t.Run("create with empty prefix string uses config default", func(t *testing.T) {
		mr := resolver.Mutation()
		emptyPrefix := ""
		input := model.CreateNibInput{
			Title:  "Empty Prefix Nib",
			Prefix: &emptyPrefix,
		}
		got, err := mr.CreateNib(ctx, input)
		if err != nil {
			t.Fatalf("CreateNib() error = %v", err)
		}
		// Empty string prefix should fall back to config default
		if len(got.ID) != 4 {
			t.Errorf("CreateNib().ID length = %d, want 4", len(got.ID))
		}
	})
}

func TestMutationUpdateNib(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	// Create a test nib
	b := &nib.Nib{
		ID:       "update-test",
		Title:    "Original Title",
		Status:   "todo",
		Type:     "task",
		Priority: "normal",
		Body:     "Original body",
		Tags:     []string{"original"},
	}
	mustCreate(t, core, b)

	t.Run("update single field", func(t *testing.T) {
		mr := resolver.Mutation()
		newStatus := "in-progress"
		input := model.UpdateNibInput{
			Status: &newStatus,
		}
		got, err := mr.UpdateNib(ctx, "update-test", input)
		if err != nil {
			t.Fatalf("UpdateNib() error = %v", err)
		}
		if got.Status != "in-progress" {
			t.Errorf("UpdateNib().Status = %q, want %q", got.Status, "in-progress")
		}
		// Other fields unchanged
		if got.Title != "Original Title" {
			t.Errorf("UpdateNib().Title = %q, want %q", got.Title, "Original Title")
		}
	})

	t.Run("update multiple fields", func(t *testing.T) {
		mr := resolver.Mutation()
		newTitle := "Updated Title"
		newPriority := "high"
		newBody := "Updated body"
		input := model.UpdateNibInput{
			Title:    &newTitle,
			Priority: graphql.OmittableOf(&newPriority),
			Body:     &newBody,
		}
		got, err := mr.UpdateNib(ctx, "update-test", input)
		if err != nil {
			t.Fatalf("UpdateNib() error = %v", err)
		}
		if got.Title != "Updated Title" {
			t.Errorf("UpdateNib().Title = %q, want %q", got.Title, "Updated Title")
		}
		if got.Priority != "high" {
			t.Errorf("UpdateNib().Priority = %q, want %q", got.Priority, "high")
		}
		if got.Body != "Updated body" {
			t.Errorf("UpdateNib().Body = %q, want %q", got.Body, "Updated body")
		}
	})

	t.Run("replace tags", func(t *testing.T) {
		mr := resolver.Mutation()
		input := model.UpdateNibInput{
			Tags: []string{"new-tag-1", "new-tag-2"},
		}
		got, err := mr.UpdateNib(ctx, "update-test", input)
		if err != nil {
			t.Fatalf("UpdateNib() error = %v", err)
		}
		if len(got.Tags) != 2 {
			t.Errorf("UpdateNib().Tags count = %d, want 2", len(got.Tags))
		}
	})

	t.Run("update nonexistent nib", func(t *testing.T) {
		mr := resolver.Mutation()
		newTitle := "Whatever"
		input := model.UpdateNibInput{
			Title: &newTitle,
		}
		_, err := mr.UpdateNib(ctx, "nonexistent", input)
		if err == nil {
			t.Error("UpdateNib() expected error for nonexistent nib")
		}
	})
}

func TestMutationSetParent(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	// Create test nibs
	parent := &nib.Nib{ID: "parent-1", Title: "Parent", Status: "todo", Type: "epic"}
	child := &nib.Nib{ID: "child-1", Title: "Child", Status: "todo", Type: "task"}
	mustCreate(t, core, parent)
	mustCreate(t, core, child)

	t.Run("set parent", func(t *testing.T) {
		mr := resolver.Mutation()
		parentID := "parent-1"
		got, err := mr.SetParent(ctx, "child-1", &parentID, nil)
		if err != nil {
			t.Fatalf("SetParent() error = %v", err)
		}
		if got.Parent != "parent-1" {
			t.Errorf("SetParent().Parent = %q, want %q", got.Parent, "parent-1")
		}
	})

	t.Run("clear parent", func(t *testing.T) {
		mr := resolver.Mutation()
		got, err := mr.SetParent(ctx, "child-1", nil, nil)
		if err != nil {
			t.Fatalf("SetParent() error = %v", err)
		}
		if got.Parent != "" {
			t.Errorf("SetParent().Parent = %q, want empty", got.Parent)
		}
	})

	t.Run("set parent on nonexistent nib", func(t *testing.T) {
		mr := resolver.Mutation()
		parentID := "parent-1"
		_, err := mr.SetParent(ctx, "nonexistent", &parentID, nil)
		if err == nil {
			t.Error("SetParent() expected error for nonexistent nib")
		}
	})
}

func TestMutationAddRemoveBlocking(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	// Create test nibs
	blocker := &nib.Nib{ID: "blocker-1", Title: "Blocker", Status: "todo", Type: "task"}
	target := &nib.Nib{ID: "target-1", Title: "Target", Status: "todo", Type: "task"}
	mustCreate(t, core, blocker)
	mustCreate(t, core, target)

	t.Run("add block", func(t *testing.T) {
		mr := resolver.Mutation()
		_, err := mr.AddBlocking(ctx, "blocker-1", "target-1")
		if err != nil {
			t.Fatalf("AddBlocking() error = %v", err)
		}
		// Single-side: target should have blocker in its blockedBy
		targetAfter, _ := core.Get("target-1")
		if !targetAfter.IsBlockedBy("blocker-1") {
			t.Errorf("target.BlockedBy should contain blocker-1, got %v", targetAfter.BlockedBy)
		}
	})

	t.Run("remove block", func(t *testing.T) {
		mr := resolver.Mutation()
		_, err := mr.RemoveBlocking(ctx, "blocker-1", "target-1")
		if err != nil {
			t.Fatalf("RemoveBlocking() error = %v", err)
		}
		// Single-side: target's blockedBy should be cleared
		targetAfter, _ := core.Get("target-1")
		if targetAfter.IsBlockedBy("blocker-1") {
			t.Errorf("target.BlockedBy should not contain blocker-1 after removal, got %v", targetAfter.BlockedBy)
		}
	})

	t.Run("add block to nonexistent nib", func(t *testing.T) {
		mr := resolver.Mutation()
		_, err := mr.AddBlocking(ctx, "nonexistent", "target-1")
		if err == nil {
			t.Error("AddBlocking() expected error for nonexistent nib")
		}
	})
}

func TestRemoveBlockingSingleSide(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	t.Run("RemoveBlocking removes from target blockedBy", func(t *testing.T) {
		a := &nib.Nib{ID: "bidir-a1", Title: "A", Status: "todo", Type: "task"}
		b := &nib.Nib{ID: "bidir-b1", Title: "B", Status: "todo", Type: "task", BlockedBy: []string{"bidir-a1"}}
		mustCreate(t, core, a)
		mustCreate(t, core, b)

		mr := resolver.Mutation()
		_, err := mr.RemoveBlocking(ctx, "bidir-a1", "bidir-b1")
		if err != nil {
			t.Fatalf("RemoveBlocking() error = %v", err)
		}
		bAfter, _ := core.Get("bidir-b1")
		if len(bAfter.BlockedBy) != 0 {
			t.Errorf("B.BlockedBy = %v, want []", bAfter.BlockedBy)
		}
	})

	t.Run("RemoveBlockedBy removes from own blockedBy", func(t *testing.T) {
		a := &nib.Nib{ID: "bidir-a2", Title: "A", Status: "todo", Type: "task"}
		b := &nib.Nib{ID: "bidir-b2", Title: "B", Status: "todo", Type: "task", BlockedBy: []string{"bidir-a2"}}
		mustCreate(t, core, a)
		mustCreate(t, core, b)

		mr := resolver.Mutation()
		got, err := mr.RemoveBlockedBy(ctx, "bidir-b2", "bidir-a2", nil)
		if err != nil {
			t.Fatalf("RemoveBlockedBy() error = %v", err)
		}
		if len(got.BlockedBy) != 0 {
			t.Errorf("B.BlockedBy = %v, want []", got.BlockedBy)
		}
	})

	t.Run("UpdateNib removeBlocking removes from target blockedBy", func(t *testing.T) {
		a := &nib.Nib{ID: "bidir-a3", Title: "A", Status: "todo", Type: "task"}
		b := &nib.Nib{ID: "bidir-b3", Title: "B", Status: "todo", Type: "task", BlockedBy: []string{"bidir-a3"}}
		mustCreate(t, core, a)
		mustCreate(t, core, b)

		mr := resolver.Mutation()
		_, err := mr.UpdateNib(ctx, "bidir-a3", model.UpdateNibInput{
			RemoveBlocking: []string{"bidir-b3"},
		})
		if err != nil {
			t.Fatalf("UpdateNib() error = %v", err)
		}
		bAfter, _ := core.Get("bidir-b3")
		if len(bAfter.BlockedBy) != 0 {
			t.Errorf("B.BlockedBy = %v, want []", bAfter.BlockedBy)
		}
	})

	t.Run("UpdateNib removeBlockedBy removes from own blockedBy", func(t *testing.T) {
		a := &nib.Nib{ID: "bidir-a4", Title: "A", Status: "todo", Type: "task"}
		b := &nib.Nib{ID: "bidir-b4", Title: "B", Status: "todo", Type: "task", BlockedBy: []string{"bidir-a4"}}
		mustCreate(t, core, a)
		mustCreate(t, core, b)

		mr := resolver.Mutation()
		got, err := mr.UpdateNib(ctx, "bidir-b4", model.UpdateNibInput{
			RemoveBlockedBy: []string{"bidir-a4"},
		})
		if err != nil {
			t.Fatalf("UpdateNib() error = %v", err)
		}
		if len(got.BlockedBy) != 0 {
			t.Errorf("B.BlockedBy = %v, want []", got.BlockedBy)
		}
	})
}

func TestMutationDeleteNib(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	t.Run("delete existing nib", func(t *testing.T) {
		// Create a nib to delete
		b := &nib.Nib{ID: "delete-me", Title: "Delete Me", Status: "todo", Type: "task"}
		mustCreate(t, core, b)

		mr := resolver.Mutation()
		got, err := mr.DeleteNib(ctx, "delete-me")
		if err != nil {
			t.Fatalf("DeleteNib() error = %v", err)
		}
		if !got {
			t.Error("DeleteNib() = false, want true")
		}

		// Verify it's gone
		qr := resolver.Query()
		nib, _ := qr.Nib(ctx, "delete-me")
		if nib != nil {
			t.Error("Nib still exists after delete")
		}
	})

	t.Run("delete removes incoming links", func(t *testing.T) {
		// Create target nib that is blocked by linker (single-side: blockedBy on target)
		linker := &nib.Nib{ID: "linker-nib", Title: "Linker", Status: "todo", Type: "task"}
		mustCreate(t, core, linker)

		target := &nib.Nib{
			ID:        "target-nib",
			Title:     "Target",
			Status:    "todo",
			Type:      "task",
			BlockedBy: []string{"linker-nib"},
		}
		mustCreate(t, core, target)

		// Also create a nib that has target in its blockedBy (to test removal of that link)
		dependent := &nib.Nib{
			ID:        "dependent-nib",
			Title:     "Dependent",
			Status:    "todo",
			Type:      "task",
			BlockedBy: []string{"target-nib"},
		}
		mustCreate(t, core, dependent)

		// Delete target - should remove the blockedBy link from dependent
		mr := resolver.Mutation()
		_, err := mr.DeleteNib(ctx, "target-nib")
		if err != nil {
			t.Fatalf("DeleteNib() error = %v", err)
		}

		// Verify dependent no longer has the blocked_by link
		qr := resolver.Query()
		updated, _ := qr.Nib(ctx, "dependent-nib")
		if updated == nil {
			t.Fatal("Dependent nib was deleted unexpectedly")
			return
		}
		if len(updated.BlockedBy) != 0 {
			t.Errorf("Dependent still has %d blocked_by, want 0", len(updated.BlockedBy))
		}
	})

	t.Run("delete nonexistent nib", func(t *testing.T) {
		mr := resolver.Mutation()
		_, err := mr.DeleteNib(ctx, "nonexistent")
		if err == nil {
			t.Error("DeleteNib() expected error for nonexistent nib")
		}
	})
}

func TestRelationshipFieldsWithFilter(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	// Create a parent (milestone) with multiple children (tasks) of different statuses
	parent := &nib.Nib{
		ID:     "parent-filter-test",
		Title:  "Parent Milestone",
		Type:   "milestone",
		Status: "in-progress",
	}
	child1 := &nib.Nib{
		ID:        "child-todo",
		Title:     "Todo Task",
		Type:      "task",
		Status:    "todo",
		Parent:    "parent-filter-test",
		BlockedBy: []string{"blocker-bug", "blocker-task"},
	}
	child2 := &nib.Nib{
		ID:     "child-completed",
		Title:  "Completed Task",
		Type:   "task",
		Status: "completed",
		Parent: "parent-filter-test",
	}
	child3 := &nib.Nib{
		ID:       "child-inprogress",
		Title:    "In Progress Task",
		Type:     "task",
		Status:   "in-progress",
		Parent:   "parent-filter-test",
		Priority: "high",
	}

	// Blockers (blocking is computed from child-todo's blockedBy)
	blocker1 := &nib.Nib{
		ID:     "blocker-bug",
		Title:  "Blocking Bug",
		Type:   "bug",
		Status: "todo",
	}
	blocker2 := &nib.Nib{
		ID:     "blocker-task",
		Title:  "Blocking Task",
		Type:   "task",
		Status: "completed",
	}

	for _, b := range []*nib.Nib{parent, child1, child2, child3, blocker1, blocker2} {
		if err := core.Create(b); err != nil {
			t.Fatalf("Failed to create nib %s: %v", b.ID, err)
		}
	}

	br := resolver.Nib()

	t.Run("children with status filter", func(t *testing.T) {
		filter := &model.NibFilter{
			Status: []string{"todo"},
		}
		got, err := br.Children(ctx, parent, filter, nil)
		if err != nil {
			t.Fatalf("Children() error = %v", err)
		}
		if len(got) != 1 {
			t.Errorf("Children(filter status=todo) count = %d, want 1", len(got))
		}
		if len(got) > 0 && got[0].ID != "child-todo" {
			t.Errorf("Children(filter status=todo)[0].ID = %q, want %q", got[0].ID, "child-todo")
		}
	})

	t.Run("children with excludeStatus filter", func(t *testing.T) {
		filter := &model.NibFilter{
			ExcludeStatus: []string{"completed"},
		}
		got, err := br.Children(ctx, parent, filter, nil)
		if err != nil {
			t.Fatalf("Children() error = %v", err)
		}
		if len(got) != 2 {
			t.Errorf("Children(filter excludeStatus=completed) count = %d, want 2", len(got))
		}
	})

	t.Run("children with priority filter", func(t *testing.T) {
		filter := &model.NibFilter{
			Priority: []string{"high"},
		}
		got, err := br.Children(ctx, parent, filter, nil)
		if err != nil {
			t.Fatalf("Children() error = %v", err)
		}
		if len(got) != 1 {
			t.Errorf("Children(filter priority=high) count = %d, want 1", len(got))
		}
		if len(got) > 0 && got[0].ID != "child-inprogress" {
			t.Errorf("Children(filter priority=high)[0].ID = %q, want %q", got[0].ID, "child-inprogress")
		}
	})

	t.Run("children with nil filter returns all", func(t *testing.T) {
		got, err := br.Children(ctx, parent, nil, nil)
		if err != nil {
			t.Fatalf("Children() error = %v", err)
		}
		if len(got) != 3 {
			t.Errorf("Children(nil filter) count = %d, want 3", len(got))
		}
	})

	t.Run("blockedBy with type filter", func(t *testing.T) {
		filter := &model.NibFilter{
			Type: []string{"bug"},
		}
		got, err := br.BlockedBy(ctx, child1, filter)
		if err != nil {
			t.Fatalf("BlockedBy() error = %v", err)
		}
		if len(got) != 1 {
			t.Errorf("BlockedBy(filter type=bug) count = %d, want 1", len(got))
		}
		if len(got) > 0 && got[0].ID != "blocker-bug" {
			t.Errorf("BlockedBy(filter type=bug)[0].ID = %q, want %q", got[0].ID, "blocker-bug")
		}
	})

	t.Run("blockedBy with excludeStatus filter", func(t *testing.T) {
		filter := &model.NibFilter{
			ExcludeStatus: []string{"completed"},
		}
		got, err := br.BlockedBy(ctx, child1, filter)
		if err != nil {
			t.Fatalf("BlockedBy() error = %v", err)
		}
		if len(got) != 1 {
			t.Errorf("BlockedBy(filter excludeStatus=completed) count = %d, want 1", len(got))
		}
		if len(got) > 0 && got[0].ID != "blocker-bug" {
			t.Errorf("BlockedBy(filter excludeStatus=completed)[0].ID = %q, want %q", got[0].ID, "blocker-bug")
		}
	})

	t.Run("blocking with status filter", func(t *testing.T) {
		filter := &model.NibFilter{
			Status: []string{"todo"},
		}
		got, err := br.Blocking(ctx, blocker1, filter)
		if err != nil {
			t.Fatalf("Blocking() error = %v", err)
		}
		if len(got) != 1 {
			t.Errorf("Blocking(filter status=todo) count = %d, want 1", len(got))
		}
	})

	t.Run("blocking filter excludes all", func(t *testing.T) {
		filter := &model.NibFilter{
			Status: []string{"completed"},
		}
		got, err := br.Blocking(ctx, blocker1, filter)
		if err != nil {
			t.Fatalf("Blocking() error = %v", err)
		}
		if len(got) != 0 {
			t.Errorf("Blocking(filter status=completed) count = %d, want 0", len(got))
		}
	})
}

// setupTestResolverWithPrefix creates a test resolver with a configured prefix.
func setupTestResolverWithPrefix(t *testing.T, prefix string) (*Resolver, *nibcore.Core) {
	t.Helper()
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatalf("failed to create test .nibs dir: %v", err)
	}

	cfg := config.DefaultWithPrefix(prefix)
	core := nibcore.New(nibsDir, cfg)
	if err := core.Load(); err != nil {
		t.Fatalf("failed to load core: %v", err)
	}

	return &Resolver{Reader: core, Writer: core, Validator: core, Blocking: core, Orderer: NewOrderer(core, core)}, core
}

// setupTestResolverWithRequireIfMatch creates a test resolver with require_if_match enabled.
func setupTestResolverWithRequireIfMatch(t *testing.T) (*Resolver, *nibcore.Core) {
	t.Helper()
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatalf("failed to create test .nibs dir: %v", err)
	}

	cfg := config.Default()
	cfg.Nibs.RequireIfMatch = true
	core := nibcore.New(nibsDir, cfg)
	if err := core.Load(); err != nil {
		t.Fatalf("failed to load core: %v", err)
	}

	return &Resolver{Reader: core, Writer: core, Validator: core, Blocking: core, Orderer: NewOrderer(core, core)}, core
}

func TestETagValidation(t *testing.T) {
	t.Run("update with correct etag succeeds", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		ctx := context.Background()

		b := &nib.Nib{ID: "etag-test-1", Title: "Test", Status: "todo"}
		mustCreate(t, core, b)

		// Get current etag
		currentETag := b.ETag()

		mr := resolver.Mutation()
		newTitle := "Updated"
		input := model.UpdateNibInput{
			Title:   &newTitle,
			IfMatch: &currentETag,
		}
		got, err := mr.UpdateNib(ctx, "etag-test-1", input)
		if err != nil {
			t.Fatalf("UpdateNib() with correct etag error = %v", err)
		}
		if got.Title != "Updated" {
			t.Errorf("UpdateNib().Title = %q, want %q", got.Title, "Updated")
		}
	})

	t.Run("update with incorrect etag fails", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		ctx := context.Background()

		b := &nib.Nib{ID: "etag-test-2", Title: "Test", Status: "todo"}
		mustCreate(t, core, b)

		mr := resolver.Mutation()
		newTitle := "Updated"
		wrongETag := "wrongetagvalue1"
		input := model.UpdateNibInput{
			Title:   &newTitle,
			IfMatch: &wrongETag,
		}
		_, err := mr.UpdateNib(ctx, "etag-test-2", input)
		if err == nil {
			t.Error("UpdateNib() with wrong etag should fail")
		}
		if !strings.Contains(err.Error(), "etag mismatch") {
			t.Errorf("Error should mention etag mismatch, got: %v", err)
		}
	})

	t.Run("update without etag succeeds when not required", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		ctx := context.Background()

		b := &nib.Nib{ID: "etag-test-3", Title: "Test", Status: "todo"}
		mustCreate(t, core, b)

		mr := resolver.Mutation()
		newTitle := "Updated"
		input := model.UpdateNibInput{
			Title: &newTitle,
		}
		got, err := mr.UpdateNib(ctx, "etag-test-3", input)
		if err != nil {
			t.Fatalf("UpdateNib() without etag error = %v", err)
		}
		if got.Title != "Updated" {
			t.Errorf("UpdateNib().Title = %q, want %q", got.Title, "Updated")
		}
	})
}

func TestRequireIfMatchConfig(t *testing.T) {
	t.Run("update without etag fails when require_if_match is true", func(t *testing.T) {
		resolver, core := setupTestResolverWithRequireIfMatch(t)
		ctx := context.Background()

		b := &nib.Nib{ID: "require-etag-1", Title: "Test", Status: "todo"}
		mustCreate(t, core, b)

		mr := resolver.Mutation()
		newTitle := "Updated"
		input := model.UpdateNibInput{
			Title: &newTitle,
		}
		_, err := mr.UpdateNib(ctx, "require-etag-1", input)
		if err == nil {
			t.Error("UpdateNib() without etag should fail when require_if_match is true")
		}
		if !strings.Contains(err.Error(), "if-match etag is required") {
			t.Errorf("Error should mention etag is required, got: %v", err)
		}
	})

	t.Run("update with correct etag succeeds when require_if_match is true", func(t *testing.T) {
		resolver, core := setupTestResolverWithRequireIfMatch(t)
		ctx := context.Background()

		b := &nib.Nib{ID: "require-etag-2", Title: "Test", Status: "todo"}
		mustCreate(t, core, b)

		currentETag := b.ETag()

		mr := resolver.Mutation()
		newTitle := "Updated"
		input := model.UpdateNibInput{
			Title:   &newTitle,
			IfMatch: &currentETag,
		}
		got, err := mr.UpdateNib(ctx, "require-etag-2", input)
		if err != nil {
			t.Fatalf("UpdateNib() with correct etag error = %v", err)
		}
		if got.Title != "Updated" {
			t.Errorf("UpdateNib().Title = %q, want %q", got.Title, "Updated")
		}
	})

	t.Run("setParent without etag fails when require_if_match is true", func(t *testing.T) {
		resolver, core := setupTestResolverWithRequireIfMatch(t)
		ctx := context.Background()

		parent := &nib.Nib{ID: "req-parent", Title: "Parent", Status: "todo", Type: "epic"}
		child := &nib.Nib{ID: "req-child", Title: "Child", Status: "todo", Type: "task"}
		mustCreate(t, core, parent)
		mustCreate(t, core, child)

		mr := resolver.Mutation()
		parentID := "req-parent"
		_, err := mr.SetParent(ctx, "req-child", &parentID, nil)
		if err == nil {
			t.Error("SetParent() without etag should fail when require_if_match is true")
		}
	})

	t.Run("addBlocking succeeds when require_if_match is true", func(t *testing.T) {
		resolver, core := setupTestResolverWithRequireIfMatch(t)
		ctx := context.Background()

		blocker := &nib.Nib{ID: "req-blocker", Title: "Blocker", Status: "todo", Type: "task"}
		target := &nib.Nib{ID: "req-target", Title: "Target", Status: "todo", Type: "task"}
		mustCreate(t, core, blocker)
		mustCreate(t, core, target)

		mr := resolver.Mutation()
		_, err := mr.AddBlocking(ctx, "req-blocker", "req-target")
		if err != nil {
			t.Fatalf("AddBlocking() should succeed with require_if_match (system-initiated update): %v", err)
		}
		targetAfter, _ := core.Get("req-target")
		if !targetAfter.IsBlockedBy("req-blocker") {
			t.Errorf("target.BlockedBy should contain blocker, got %v", targetAfter.BlockedBy)
		}
	})

	t.Run("removeBlocking succeeds when require_if_match is true", func(t *testing.T) {
		resolver, core := setupTestResolverWithRequireIfMatch(t)
		ctx := context.Background()

		blocker := &nib.Nib{ID: "req-blocker2", Title: "Blocker", Status: "todo", Type: "task"}
		target := &nib.Nib{ID: "req-target2", Title: "Target", Status: "todo", Type: "task", BlockedBy: []string{"req-blocker2"}}
		mustCreate(t, core, blocker)
		mustCreate(t, core, target)

		mr := resolver.Mutation()
		_, err := mr.RemoveBlocking(ctx, "req-blocker2", "req-target2")
		if err != nil {
			t.Fatalf("RemoveBlocking() should succeed with require_if_match (system-initiated update): %v", err)
		}
		targetAfter, _ := core.Get("req-target2")
		if targetAfter.IsBlockedBy("req-blocker2") {
			t.Errorf("target.BlockedBy should not contain blocker after removal, got %v", targetAfter.BlockedBy)
		}
	})

}

func TestShortIDNormalization(t *testing.T) {
	// Use a prefix so we can test short ID resolution
	resolver, core := setupTestResolverWithPrefix(t, "nibs-")
	ctx := context.Background()

	// Create test nibs with full IDs (prefix + short ID)
	parent := &nib.Nib{ID: "nibs-parent1", Title: "Parent", Status: "todo", Type: "epic"}
	child := &nib.Nib{ID: "nibs-child1", Title: "Child", Status: "todo", Type: "task"}
	target := &nib.Nib{ID: "nibs-target1", Title: "Target", Status: "todo", Type: "task"}
	mustCreate(t, core, parent)
	mustCreate(t, core, child)
	mustCreate(t, core, target)

	t.Run("SetParent normalizes short ID", func(t *testing.T) {
		mr := resolver.Mutation()
		// Use short ID (without prefix)
		shortParentID := "parent1"
		got, err := mr.SetParent(ctx, "nibs-child1", &shortParentID, nil)
		if err != nil {
			t.Fatalf("SetParent() error = %v", err)
		}
		// Should store the full ID, not the short one
		if got.Parent != "nibs-parent1" {
			t.Errorf("SetParent().Parent = %q, want %q", got.Parent, "nibs-parent1")
		}
	})

	t.Run("AddBlocking normalizes short ID", func(t *testing.T) {
		mr := resolver.Mutation()
		// Use short ID (without prefix)
		_, err := mr.AddBlocking(ctx, "nibs-child1", "target1")
		if err != nil {
			t.Fatalf("AddBlocking() error = %v", err)
		}
		// Should store the full ID on target's blockedBy, not the short one
		targetAfter, _ := core.Get("nibs-target1")
		if !targetAfter.IsBlockedBy("nibs-child1") {
			t.Errorf("target.BlockedBy should contain nibs-child1, got %v", targetAfter.BlockedBy)
		}
	})

	t.Run("RemoveBlocking normalizes short ID", func(t *testing.T) {
		mr := resolver.Mutation()
		// Remove using short ID
		_, err := mr.RemoveBlocking(ctx, "nibs-child1", "target1")
		if err != nil {
			t.Fatalf("RemoveBlocking() error = %v", err)
		}
		targetAfter, _ := core.Get("nibs-target1")
		if targetAfter.IsBlockedBy("nibs-child1") {
			t.Errorf("target.BlockedBy should not contain nibs-child1, got %v", targetAfter.BlockedBy)
		}
	})

	t.Run("CreateNib normalizes parent short ID", func(t *testing.T) {
		mr := resolver.Mutation()
		nibType := "task"
		shortParentID := "parent1"
		input := model.CreateNibInput{
			Title:  "New Child",
			Type:   &nibType,
			Parent: &shortParentID,
		}
		got, err := mr.CreateNib(ctx, input)
		if err != nil {
			t.Fatalf("CreateNib() error = %v", err)
		}
		if got.Parent != "nibs-parent1" {
			t.Errorf("CreateNib().Parent = %q, want %q", got.Parent, "nibs-parent1")
		}
	})

	t.Run("CreateNib normalizes blocking short IDs", func(t *testing.T) {
		mr := resolver.Mutation()
		nibType := "task"
		input := model.CreateNibInput{
			Title:    "Blocker Nib",
			Type:     &nibType,
			Blocking: []string{"target1"},
		}
		got, err := mr.CreateNib(ctx, input)
		if err != nil {
			t.Fatalf("CreateNib() error = %v", err)
		}
		// Single-side: blocking stored on target's blockedBy with normalized ID
		targetAfter, _ := core.Get("nibs-target1")
		if !targetAfter.IsBlockedBy(got.ID) {
			t.Errorf("target.BlockedBy should contain %s, got %v", got.ID, targetAfter.BlockedBy)
		}
	})
}

func TestUpdateNibWithBodyMod(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	t.Run("bodyMod with single replacement only", func(t *testing.T) {
		b := &nib.Nib{
			ID:     "bodymod-test-1",
			Title:  "Test",
			Status: "todo",
			Body:   "## Tasks\n- [ ] Task 1\n- [ ] Task 2",
		}
		mustCreate(t, core, b)

		input := model.UpdateNibInput{
			BodyMod: &model.BodyModification{
				Replace: []*model.ReplaceOperation{
					{Old: "- [ ] Task 1", New: "- [x] Task 1"},
				},
			},
		}

		got, err := resolver.Mutation().UpdateNib(ctx, "bodymod-test-1", input)
		if err != nil {
			t.Fatalf("UpdateNib() error = %v", err)
		}
		want := "## Tasks\n- [x] Task 1\n- [ ] Task 2"
		if got.Body != want {
			t.Errorf("UpdateNib().Body = %q, want %q", got.Body, want)
		}
	})

	t.Run("bodyMod with append only", func(t *testing.T) {
		b := &nib.Nib{
			ID:     "bodymod-test-2",
			Title:  "Test",
			Status: "todo",
			Body:   "Existing content",
		}
		mustCreate(t, core, b)

		appendText := "## Notes\n\nNew section"
		input := model.UpdateNibInput{
			BodyMod: &model.BodyModification{
				Append: &appendText,
			},
		}

		got, err := resolver.Mutation().UpdateNib(ctx, "bodymod-test-2", input)
		if err != nil {
			t.Fatalf("UpdateNib() error = %v", err)
		}
		want := "Existing content\n\n## Notes\n\nNew section"
		if got.Body != want {
			t.Errorf("UpdateNib().Body = %q, want %q", got.Body, want)
		}
	})

	t.Run("bodyMod with replacement and append combined", func(t *testing.T) {
		b := &nib.Nib{
			ID:     "bodymod-test-3",
			Title:  "Test",
			Status: "todo",
			Body:   "## Tasks\n- [ ] Deploy",
		}
		mustCreate(t, core, b)

		appendText := "## Summary\n\nCompleted"
		input := model.UpdateNibInput{
			BodyMod: &model.BodyModification{
				Replace: []*model.ReplaceOperation{
					{Old: "- [ ] Deploy", New: "- [x] Deploy"},
				},
				Append: &appendText,
			},
		}

		got, err := resolver.Mutation().UpdateNib(ctx, "bodymod-test-3", input)
		if err != nil {
			t.Fatalf("UpdateNib() error = %v", err)
		}
		want := "## Tasks\n- [x] Deploy\n\n## Summary\n\nCompleted"
		if got.Body != want {
			t.Errorf("UpdateNib().Body = %q, want %q", got.Body, want)
		}
	})

	t.Run("bodyMod with multiple replacements sequential", func(t *testing.T) {
		b := &nib.Nib{
			ID:     "bodymod-test-4",
			Title:  "Test",
			Status: "todo",
			Body:   "- [ ] Task 1\n- [ ] Task 2\n- [ ] Task 3",
		}
		mustCreate(t, core, b)

		input := model.UpdateNibInput{
			BodyMod: &model.BodyModification{
				Replace: []*model.ReplaceOperation{
					{Old: "- [ ] Task 1", New: "- [x] Task 1"},
					{Old: "- [ ] Task 2", New: "- [x] Task 2"},
					{Old: "- [ ] Task 3", New: "- [x] Task 3"},
				},
			},
		}

		got, err := resolver.Mutation().UpdateNib(ctx, "bodymod-test-4", input)
		if err != nil {
			t.Fatalf("UpdateNib() error = %v", err)
		}
		want := "- [x] Task 1\n- [x] Task 2\n- [x] Task 3"
		if got.Body != want {
			t.Errorf("UpdateNib().Body = %q, want %q", got.Body, want)
		}
	})

	t.Run("bodyMod with metadata update", func(t *testing.T) {
		b := &nib.Nib{
			ID:     "bodymod-test-5",
			Title:  "Test",
			Status: "todo",
			Body:   "- [ ] Task",
		}
		mustCreate(t, core, b)

		status := "completed"
		appendText := "## Done"
		input := model.UpdateNibInput{
			Status: &status,
			BodyMod: &model.BodyModification{
				Replace: []*model.ReplaceOperation{
					{Old: "- [ ] Task", New: "- [x] Task"},
				},
				Append: &appendText,
			},
		}

		got, err := resolver.Mutation().UpdateNib(ctx, "bodymod-test-5", input)
		if err != nil {
			t.Fatalf("UpdateNib() error = %v", err)
		}
		if got.Status != "completed" {
			t.Errorf("UpdateNib().Status = %q, want %q", got.Status, "completed")
		}
		want := "- [x] Task\n\n## Done"
		if got.Body != want {
			t.Errorf("UpdateNib().Body = %q, want %q", got.Body, want)
		}
	})

	t.Run("error when both body and bodyMod provided", func(t *testing.T) {
		b := &nib.Nib{
			ID:     "bodymod-test-6",
			Title:  "Test",
			Status: "todo",
			Body:   "Original",
		}
		mustCreate(t, core, b)

		bodyText := "New body"
		appendText := "Append"
		input := model.UpdateNibInput{
			Body: &bodyText,
			BodyMod: &model.BodyModification{
				Append: &appendText,
			},
		}

		_, err := resolver.Mutation().UpdateNib(ctx, "bodymod-test-6", input)
		if err == nil {
			t.Error("UpdateNib() expected error when both body and bodyMod provided")
		}
		if !strings.Contains(err.Error(), "cannot specify both body and bodyMod") {
			t.Errorf("Error should mention mutual exclusivity, got: %v", err)
		}
	})

	t.Run("error when replacement text not found", func(t *testing.T) {
		b := &nib.Nib{
			ID:     "bodymod-test-7",
			Title:  "Test",
			Status: "todo",
			Body:   "Hello world",
		}
		mustCreate(t, core, b)

		input := model.UpdateNibInput{
			BodyMod: &model.BodyModification{
				Replace: []*model.ReplaceOperation{
					{Old: "nonexistent", New: "fail"},
				},
			},
		}

		_, err := resolver.Mutation().UpdateNib(ctx, "bodymod-test-7", input)
		if err == nil {
			t.Error("UpdateNib() expected error when replacement text not found")
		}
		if !strings.Contains(err.Error(), "text not found") {
			t.Errorf("Error should mention text not found, got: %v", err)
		}
	})

	t.Run("error when replacement text found multiple times", func(t *testing.T) {
		b := &nib.Nib{
			ID:     "bodymod-test-8",
			Title:  "Test",
			Status: "todo",
			Body:   "foo foo foo",
		}
		mustCreate(t, core, b)

		input := model.UpdateNibInput{
			BodyMod: &model.BodyModification{
				Replace: []*model.ReplaceOperation{
					{Old: "foo", New: "bar"},
				},
			},
		}

		_, err := resolver.Mutation().UpdateNib(ctx, "bodymod-test-8", input)
		if err == nil {
			t.Error("UpdateNib() expected error when replacement text found multiple times")
		}
		if !strings.Contains(err.Error(), "found 3 times") {
			t.Errorf("Error should mention multiple matches, got: %v", err)
		}
	})

	t.Run("transactional: later replacement fails, nothing saved", func(t *testing.T) {
		b := &nib.Nib{
			ID:     "bodymod-test-9",
			Title:  "Test",
			Status: "todo",
			Body:   "Task 1\nTask 2",
		}
		mustCreate(t, core, b)
		originalBody := b.Body

		input := model.UpdateNibInput{
			BodyMod: &model.BodyModification{
				Replace: []*model.ReplaceOperation{
					{Old: "Task 1", New: "Done 1"},    // This should succeed
					{Old: "nonexistent", New: "fail"}, // This should fail
				},
			},
		}

		_, err := resolver.Mutation().UpdateNib(ctx, "bodymod-test-9", input)
		if err == nil {
			t.Error("UpdateNib() expected error")
		}

		// Verify nib wasn't modified
		updated, _ := core.Get("bodymod-test-9")
		if updated.Body != originalBody {
			t.Errorf("Nib body was modified despite error. Got %q, want %q", updated.Body, originalBody)
		}
	})

	t.Run("empty append is no-op", func(t *testing.T) {
		b := &nib.Nib{
			ID:     "bodymod-test-10",
			Title:  "Test",
			Status: "todo",
			Body:   "Original content",
		}
		mustCreate(t, core, b)

		emptyAppend := ""
		input := model.UpdateNibInput{
			BodyMod: &model.BodyModification{
				Append: &emptyAppend,
			},
		}

		got, err := resolver.Mutation().UpdateNib(ctx, "bodymod-test-10", input)
		if err != nil {
			t.Fatalf("UpdateNib() error = %v", err)
		}
		if got.Body != "Original content" {
			t.Errorf("UpdateNib().Body = %q, want %q (no-op for empty append)", got.Body, "Original content")
		}
	})

	t.Run("transactional: later replacement fails, nothing saved", func(t *testing.T) {
		b := &nib.Nib{
			ID:     "bodymod-test-9",
			Title:  "Test",
			Status: "todo",
			Body:   "Task 1\nTask 2",
		}
		mustCreate(t, core, b)
		originalBody := b.Body

		input := model.UpdateNibInput{
			BodyMod: &model.BodyModification{
				Replace: []*model.ReplaceOperation{
					{Old: "Task 1", New: "Done 1"},    // This should succeed
					{Old: "nonexistent", New: "fail"}, // This should fail
				},
			},
		}

		_, err := resolver.Mutation().UpdateNib(ctx, "bodymod-test-9", input)
		if err == nil {
			t.Error("UpdateNib() expected error")
		}

		// Verify nib wasn't modified
		updated, _ := core.Get("bodymod-test-9")
		if updated.Body != originalBody {
			t.Errorf("Nib body was modified despite error. Got %q, want %q", updated.Body, originalBody)
		}
	})
}

func TestUpdateNibWithRelationships(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	t.Run("atomic update with parent and blocking", func(t *testing.T) {
		epic := &nib.Nib{ID: "epic-1", Title: "Epic", Type: "epic", Status: "todo"}
		task := &nib.Nib{ID: "task-1", Title: "Task", Type: "task", Status: "todo"}
		blocker := &nib.Nib{ID: "blocker-1", Title: "Blocker", Type: "task", Status: "todo"}
		mustCreate(t, core, epic)
		mustCreate(t, core, task)
		mustCreate(t, core, blocker)

		input := model.UpdateNibInput{
			Status:      stringPtr("in-progress"),
			Parent:      graphql.OmittableOf(stringPtr("epic-1")),
			AddBlocking: []string{"blocker-1"},
		}

		got, err := resolver.Mutation().UpdateNib(ctx, "task-1", input)
		if err != nil {
			t.Fatalf("UpdateNib() error = %v", err)
		}

		if got.Status != "in-progress" {
			t.Errorf("UpdateNib().Status = %q, want %q", got.Status, "in-progress")
		}
		if got.Parent != "epic-1" {
			t.Errorf("UpdateNib().Parent = %q, want %q", got.Parent, "epic-1")
		}
		// Single-side: blocker-1's blockedBy should contain task-1
		blockerAfter, _ := core.Get("blocker-1")
		if !blockerAfter.IsBlockedBy("task-1") {
			t.Errorf("blocker-1.BlockedBy should contain task-1, got %v", blockerAfter.BlockedBy)
		}
	})

	t.Run("atomic update with bodyMod and relationships", func(t *testing.T) {
		epic := &nib.Nib{ID: "epic-2", Title: "Epic", Type: "epic", Status: "todo"}
		task := &nib.Nib{ID: "task-2", Title: "Task", Type: "task", Status: "todo", Body: "- [ ] Step 1"}
		blocker := &nib.Nib{ID: "blocker-2", Title: "Blocker", Type: "task", Status: "todo"}
		mustCreate(t, core, epic)
		mustCreate(t, core, task)
		mustCreate(t, core, blocker)

		input := model.UpdateNibInput{
			Status: stringPtr("completed"),
			Parent: graphql.OmittableOf(stringPtr("epic-2")),
			BodyMod: &model.BodyModification{
				Replace: []*model.ReplaceOperation{
					{Old: "- [ ] Step 1", New: "- [x] Step 1"},
				},
				Append: stringPtr("## Done"),
			},
			AddBlocking: []string{"blocker-2"},
		}

		got, err := resolver.Mutation().UpdateNib(ctx, "task-2", input)
		if err != nil {
			t.Fatalf("UpdateNib() error = %v", err)
		}

		if got.Status != "completed" {
			t.Errorf("Status = %q, want completed", got.Status)
		}
		if got.Parent != "epic-2" {
			t.Errorf("Parent = %q, want epic-2", got.Parent)
		}
		if !strings.Contains(got.Body, "- [x] Step 1") {
			t.Errorf("Body missing completed task")
		}
		if !strings.Contains(got.Body, "## Done") {
			t.Errorf("Body missing appended content")
		}
		// Single-side: blocker-2's blockedBy should contain task-2
		blockerAfter, _ := core.Get("blocker-2")
		if !blockerAfter.IsBlockedBy("task-2") {
			t.Errorf("blocker-2.BlockedBy should contain task-2, got %v", blockerAfter.BlockedBy)
		}
	})

	t.Run("parent validation fails for invalid type hierarchy", func(t *testing.T) {
		task1 := &nib.Nib{ID: "task-invalid-1", Title: "Task 1", Type: "task", Status: "todo"}
		task2 := &nib.Nib{ID: "task-invalid-2", Title: "Task 2", Type: "task", Status: "todo"}
		mustCreate(t, core, task1)
		mustCreate(t, core, task2)

		input := model.UpdateNibInput{
			Parent: graphql.OmittableOf(stringPtr("task-invalid-2")),
		}

		_, err := resolver.Mutation().UpdateNib(ctx, "task-invalid-1", input)
		if err == nil {
			t.Error("UpdateNib() should fail for invalid parent type")
		}
	})

	t.Run("blocking self-reference validation", func(t *testing.T) {
		task := &nib.Nib{ID: "task-self", Title: "Task", Type: "task", Status: "todo"}
		mustCreate(t, core, task)

		input := model.UpdateNibInput{
			AddBlocking: []string{"task-self"},
		}

		_, err := resolver.Mutation().UpdateNib(ctx, "task-self", input)
		if err == nil {
			t.Error("UpdateNib() should fail when nib blocks itself")
		}
		if !strings.Contains(err.Error(), "block itself") {
			t.Errorf("Error should mention self-blocking, got: %v", err)
		}
	})

	t.Run("blocking cycle detection", func(t *testing.T) {
		// Single-side: task-block-1 is blocked by task-block-2 (task-block-2 blocks task-block-1)
		task1 := &nib.Nib{ID: "task-block-1", Title: "Task 1", Type: "task", Status: "todo", BlockedBy: []string{"task-block-2"}}
		task2 := &nib.Nib{ID: "task-block-2", Title: "Task 2", Type: "task", Status: "todo"}
		mustCreate(t, core, task1)
		mustCreate(t, core, task2)

		// Try to make task-1 block task-2 (would create cycle: task-2 blocks task-1, task-1 blocks task-2)
		input := model.UpdateNibInput{
			AddBlocking: []string{"task-block-2"},
		}

		_, err := resolver.Mutation().UpdateNib(ctx, "task-block-1", input)
		if err == nil {
			t.Error("UpdateNib() should fail when creating blocking cycle")
		}
		if !strings.Contains(err.Error(), "cycle") {
			t.Errorf("Error should mention cycle, got: %v", err)
		}
	})

	t.Run("blocking target not found", func(t *testing.T) {
		task := &nib.Nib{ID: "task-notfound", Title: "Task", Type: "task", Status: "todo"}
		mustCreate(t, core, task)

		input := model.UpdateNibInput{
			AddBlocking: []string{"nonexistent"},
		}

		_, err := resolver.Mutation().UpdateNib(ctx, "task-notfound", input)
		if err == nil {
			t.Error("UpdateNib() should fail when blocking target doesn't exist")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("Error should mention not found, got: %v", err)
		}
	})

	t.Run("remove blocking relationships", func(t *testing.T) {
		task := &nib.Nib{ID: "task-remove-1", Title: "Task", Type: "task", Status: "todo"}
		other1 := &nib.Nib{ID: "other-1", Title: "Other 1", Type: "task", Status: "todo", BlockedBy: []string{"task-remove-1"}}
		other2 := &nib.Nib{ID: "other-2", Title: "Other 2", Type: "task", Status: "todo", BlockedBy: []string{"task-remove-1"}}
		mustCreate(t, core, task)
		mustCreate(t, core, other1)
		mustCreate(t, core, other2)

		input := model.UpdateNibInput{
			RemoveBlocking: []string{"other-1"},
		}

		_, err := resolver.Mutation().UpdateNib(ctx, "task-remove-1", input)
		if err != nil {
			t.Fatalf("UpdateNib() error = %v", err)
		}

		// other-1 should no longer be blocked, other-2 should still be blocked
		other1After, _ := core.Get("other-1")
		if other1After.IsBlockedBy("task-remove-1") {
			t.Errorf("other-1.BlockedBy should not contain task-remove-1 after removal, got %v", other1After.BlockedBy)
		}
	})

	t.Run("blockedBy self-reference validation", func(t *testing.T) {
		task := &nib.Nib{ID: "task-blockedby-self", Title: "Task", Type: "task", Status: "todo"}
		mustCreate(t, core, task)

		input := model.UpdateNibInput{
			AddBlockedBy: []string{"task-blockedby-self"},
		}

		_, err := resolver.Mutation().UpdateNib(ctx, "task-blockedby-self", input)
		if err == nil {
			t.Error("UpdateNib() should fail when nib is blocked by itself")
		}
		if !strings.Contains(err.Error(), "blocked by itself") {
			t.Errorf("Error should mention self-blocking, got: %v", err)
		}
	})

	t.Run("blockedBy target not found", func(t *testing.T) {
		task := &nib.Nib{ID: "task-blockedby-notfound", Title: "Task", Type: "task", Status: "todo"}
		mustCreate(t, core, task)

		input := model.UpdateNibInput{
			AddBlockedBy: []string{"nonexistent"},
		}

		_, err := resolver.Mutation().UpdateNib(ctx, "task-blockedby-notfound", input)
		if err == nil {
			t.Error("UpdateNib() should fail when blocker doesn't exist")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("Error should mention not found, got: %v", err)
		}
	})

	t.Run("combined add and remove operations", func(t *testing.T) {
		task := &nib.Nib{ID: "task-combined", Title: "Task", Type: "task", Status: "todo"}
		old1 := &nib.Nib{ID: "old-1", Title: "Old", Type: "task", Status: "todo", BlockedBy: []string{"task-combined"}}
		new1 := &nib.Nib{ID: "new-1", Title: "New", Type: "task", Status: "todo"}
		mustCreate(t, core, task)
		mustCreate(t, core, old1)
		mustCreate(t, core, new1)

		input := model.UpdateNibInput{
			RemoveBlocking: []string{"old-1"},
			AddBlocking:    []string{"new-1"},
		}

		_, err := resolver.Mutation().UpdateNib(ctx, "task-combined", input)
		if err != nil {
			t.Fatalf("UpdateNib() error = %v", err)
		}

		// old-1 should no longer be blocked, new-1 should be blocked
		old1After, _ := core.Get("old-1")
		if old1After.IsBlockedBy("task-combined") {
			t.Errorf("old-1 should not be blocked by task-combined after removal")
		}
		new1After, _ := core.Get("new-1")
		if !new1After.IsBlockedBy("task-combined") {
			t.Errorf("new-1 should be blocked by task-combined, got %v", new1After.BlockedBy)
		}
	})

	t.Run("blockedBy cycle detection", func(t *testing.T) {
		task1 := &nib.Nib{ID: "task-blockedby-cycle-1", Title: "Task 1", Type: "task", Status: "todo"}
		task2 := &nib.Nib{ID: "task-blockedby-cycle-2", Title: "Task 2", Type: "task", Status: "todo", BlockedBy: []string{"task-blockedby-cycle-1"}}
		mustCreate(t, core, task1)
		mustCreate(t, core, task2)

		// Try to make task-1 blocked by task-2 (would create cycle)
		input := model.UpdateNibInput{
			AddBlockedBy: []string{"task-blockedby-cycle-2"},
		}

		_, err := resolver.Mutation().UpdateNib(ctx, "task-blockedby-cycle-1", input)
		if err == nil {
			t.Error("UpdateNib() should fail when creating blockedBy cycle")
		}
		if !strings.Contains(err.Error(), "cycle") {
			t.Errorf("Error should mention cycle, got: %v", err)
		}
	})

	t.Run("remove parent", func(t *testing.T) {
		epic := &nib.Nib{ID: "epic-parent-remove", Title: "Epic", Type: "epic", Status: "todo"}
		task := &nib.Nib{ID: "task-parent-remove", Title: "Task", Type: "task", Status: "todo", Parent: "epic-parent-remove"}
		mustCreate(t, core, epic)
		mustCreate(t, core, task)

		// Remove parent by setting to empty string
		emptyParent := ""
		input := model.UpdateNibInput{
			Parent: graphql.OmittableOf(&emptyParent),
		}

		got, err := resolver.Mutation().UpdateNib(ctx, "task-parent-remove", input)
		if err != nil {
			t.Fatalf("UpdateNib() error = %v", err)
		}

		if got.Parent != "" {
			t.Errorf("Parent = %q, want empty string", got.Parent)
		}
	})

	t.Run("remove blockedBy relationships", func(t *testing.T) {
		task := &nib.Nib{ID: "task-remove-blockedby", Title: "Task", Type: "task", Status: "todo", BlockedBy: []string{"blocker-1", "blocker-2"}}
		blocker1 := &nib.Nib{ID: "blocker-1", Title: "Blocker 1", Type: "task", Status: "todo"}
		blocker2 := &nib.Nib{ID: "blocker-2", Title: "Blocker 2", Type: "task", Status: "todo"}
		mustCreate(t, core, task)
		mustCreate(t, core, blocker1)
		mustCreate(t, core, blocker2)

		input := model.UpdateNibInput{
			RemoveBlockedBy: []string{"blocker-1"},
		}

		got, err := resolver.Mutation().UpdateNib(ctx, "task-remove-blockedby", input)
		if err != nil {
			t.Fatalf("UpdateNib() error = %v", err)
		}

		if len(got.BlockedBy) != 1 || got.BlockedBy[0] != "blocker-2" {
			t.Errorf("BlockedBy = %v, want [blocker-2]", got.BlockedBy)
		}
	})

	t.Run("multiple blocking additions", func(t *testing.T) {
		task := &nib.Nib{ID: "task-multi-blocking", Title: "Task", Type: "task", Status: "todo"}
		target1 := &nib.Nib{ID: "target-1", Title: "Target 1", Type: "task", Status: "todo"}
		target2 := &nib.Nib{ID: "target-2", Title: "Target 2", Type: "task", Status: "todo"}
		mustCreate(t, core, task)
		mustCreate(t, core, target1)
		mustCreate(t, core, target2)

		input := model.UpdateNibInput{
			AddBlocking: []string{"target-1", "target-2"},
		}

		_, err := resolver.Mutation().UpdateNib(ctx, "task-multi-blocking", input)
		if err != nil {
			t.Fatalf("UpdateNib() error = %v", err)
		}

		// Both targets should have task in their blockedBy
		t1After, _ := core.Get("target-1")
		t2After, _ := core.Get("target-2")
		if !t1After.IsBlockedBy("task-multi-blocking") {
			t.Errorf("target-1 should be blocked by task-multi-blocking")
		}
		if !t2After.IsBlockedBy("task-multi-blocking") {
			t.Errorf("target-2 should be blocked by task-multi-blocking")
		}
	})

	t.Run("all relationship types combined", func(t *testing.T) {
		epic := &nib.Nib{ID: "epic-all", Title: "Epic", Type: "epic", Status: "todo"}
		task := &nib.Nib{ID: "task-all", Title: "Task", Type: "task", Status: "todo"}
		blocker := &nib.Nib{ID: "new-blocker", Title: "Blocker", Type: "task", Status: "todo"}
		blocked := &nib.Nib{ID: "new-blocked", Title: "Blocked", Type: "task", Status: "todo"}
		oldBlocking := &nib.Nib{ID: "old-blocking", Title: "Old Blocking", Type: "task", Status: "todo", BlockedBy: []string{"task-all"}}
		mustCreate(t, core, epic)
		mustCreate(t, core, task)
		mustCreate(t, core, blocker)
		mustCreate(t, core, blocked)
		mustCreate(t, core, oldBlocking)

		input := model.UpdateNibInput{
			Status:         stringPtr("in-progress"),
			Parent:         graphql.OmittableOf(stringPtr("epic-all")),
			AddBlocking:    []string{"new-blocked"},
			RemoveBlocking: []string{"old-blocking"},
			AddBlockedBy:   []string{"new-blocker"},
		}

		got, err := resolver.Mutation().UpdateNib(ctx, "task-all", input)
		if err != nil {
			t.Fatalf("UpdateNib() error = %v", err)
		}

		if got.Status != "in-progress" {
			t.Errorf("Status = %q, want in-progress", got.Status)
		}
		if got.Parent != "epic-all" {
			t.Errorf("Parent = %q, want epic-all", got.Parent)
		}
		// new-blocked should have task-all in its blockedBy
		newBlockedAfter, _ := core.Get("new-blocked")
		if !newBlockedAfter.IsBlockedBy("task-all") {
			t.Errorf("new-blocked should be blocked by task-all, got %v", newBlockedAfter.BlockedBy)
		}
		// old-blocking should no longer be blocked by task-all
		oldBlockingAfter, _ := core.Get("old-blocking")
		if oldBlockingAfter.IsBlockedBy("task-all") {
			t.Errorf("old-blocking should not be blocked by task-all after removal")
		}
		if len(got.BlockedBy) != 1 || got.BlockedBy[0] != "new-blocker" {
			t.Errorf("BlockedBy = %v, want [new-blocker]", got.BlockedBy)
		}
	})

	t.Run("add tags", func(t *testing.T) {
		task := &nib.Nib{ID: "task-tags-1", Title: "Task", Type: "task", Status: "todo", Tags: []string{"existing"}}
		mustCreate(t, core, task)

		input := model.UpdateNibInput{
			AddTags: []string{"new1", "new2"},
		}

		got, err := resolver.Mutation().UpdateNib(ctx, "task-tags-1", input)
		if err != nil {
			t.Fatalf("UpdateNib() error = %v", err)
		}

		if len(got.Tags) != 3 {
			t.Errorf("Tags count = %d, want 3", len(got.Tags))
		}
		tagSet := make(map[string]bool)
		for _, tag := range got.Tags {
			tagSet[tag] = true
		}
		if !tagSet["existing"] || !tagSet["new1"] || !tagSet["new2"] {
			t.Errorf("Tags = %v, want [existing new1 new2]", got.Tags)
		}
	})

	t.Run("remove tags", func(t *testing.T) {
		task := &nib.Nib{ID: "task-tags-2", Title: "Task", Type: "task", Status: "todo", Tags: []string{"tag1", "tag2", "tag3"}}
		mustCreate(t, core, task)

		input := model.UpdateNibInput{
			RemoveTags: []string{"tag2"},
		}

		got, err := resolver.Mutation().UpdateNib(ctx, "task-tags-2", input)
		if err != nil {
			t.Fatalf("UpdateNib() error = %v", err)
		}

		if len(got.Tags) != 2 {
			t.Errorf("Tags count = %d, want 2", len(got.Tags))
		}
		for _, tag := range got.Tags {
			if tag == "tag2" {
				t.Error("Tag 'tag2' should have been removed")
			}
		}
	})

	t.Run("add and remove tags in one operation", func(t *testing.T) {
		task := &nib.Nib{ID: "task-tags-3", Title: "Task", Type: "task", Status: "todo", Tags: []string{"old1", "old2", "keep"}}
		mustCreate(t, core, task)

		input := model.UpdateNibInput{
			AddTags:    []string{"new1", "new2"},
			RemoveTags: []string{"old1", "old2"},
		}

		got, err := resolver.Mutation().UpdateNib(ctx, "task-tags-3", input)
		if err != nil {
			t.Fatalf("UpdateNib() error = %v", err)
		}

		if len(got.Tags) != 3 {
			t.Errorf("Tags count = %d, want 3", len(got.Tags))
		}
		tagSet := make(map[string]bool)
		for _, tag := range got.Tags {
			tagSet[tag] = true
		}
		if !tagSet["keep"] || !tagSet["new1"] || !tagSet["new2"] {
			t.Errorf("Tags = %v, want [keep new1 new2]", got.Tags)
		}
		if tagSet["old1"] || tagSet["old2"] {
			t.Errorf("Tags = %v, should not contain old1 or old2", got.Tags)
		}
	})

	t.Run("tags and addTags are mutually exclusive", func(t *testing.T) {
		task := &nib.Nib{ID: "task-tags-4", Title: "Task", Type: "task", Status: "todo"}
		mustCreate(t, core, task)

		input := model.UpdateNibInput{
			Tags:    []string{"tag1"},
			AddTags: []string{"tag2"},
		}

		_, err := resolver.Mutation().UpdateNib(ctx, "task-tags-4", input)
		if err == nil {
			t.Error("UpdateNib() should fail when both tags and addTags are specified")
		}
		if !strings.Contains(err.Error(), "cannot specify both") {
			t.Errorf("Error should mention conflict, got: %v", err)
		}
	})
}

// Helper function for tests
func stringPtr(s string) *string {
	return &s
}

func TestBlockedByCycleDetection(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	t.Run("blocked_by self-reference fails", func(t *testing.T) {
		b := &nib.Nib{ID: "self-ref", Title: "Self Reference", Status: "todo"}
		mustCreate(t, core, b)

		mr := resolver.Mutation()
		_, err := mr.AddBlockedBy(ctx, "self-ref", "self-ref", nil)
		if err == nil {
			t.Error("AddBlockedBy() should fail for self-reference")
		}
		if !strings.Contains(err.Error(), "blocked by itself") {
			t.Errorf("Error should mention self-reference, got: %v", err)
		}
	})

	t.Run("blocked_by cycle via blocked_by only is detected", func(t *testing.T) {
		// This tests the scenario where cycles are created using only blocked_by
		a := &nib.Nib{ID: "cycle-a", Title: "Nib A", Status: "todo"}
		b := &nib.Nib{ID: "cycle-b", Title: "Nib B", Status: "todo"}
		mustCreate(t, core, a)
		mustCreate(t, core, b)

		mr := resolver.Mutation()

		// A is blocked by B (B → A)
		_, err := mr.AddBlockedBy(ctx, "cycle-a", "cycle-b", nil)
		if err != nil {
			t.Fatalf("AddBlockedBy(A, B) error = %v", err)
		}

		// B is blocked by A (A → B) - should create cycle A → B → A
		_, err = mr.AddBlockedBy(ctx, "cycle-b", "cycle-a", nil)
		if err == nil {
			t.Error("AddBlockedBy(B, A) should fail - would create cycle")
		}
		if !strings.Contains(err.Error(), "cycle") {
			t.Errorf("Error should mention cycle, got: %v", err)
		}
	})

	t.Run("blocked_by cycle via blocking is detected", func(t *testing.T) {
		// A blocks B, then B is blocked_by A creates a conflict
		a := &nib.Nib{ID: "cross-a", Title: "Nib A", Status: "todo"}
		b := &nib.Nib{ID: "cross-b", Title: "Nib B", Status: "todo"}
		mustCreate(t, core, a)
		mustCreate(t, core, b)

		mr := resolver.Mutation()

		// A blocks B (A → B)
		_, err := mr.AddBlocking(ctx, "cross-a", "cross-b")
		if err != nil {
			t.Fatalf("AddBlocking(A, B) error = %v", err)
		}

		// A is blocked by B (B → A) - should create cycle
		_, err = mr.AddBlockedBy(ctx, "cross-a", "cross-b", nil)
		if err == nil {
			t.Error("AddBlockedBy(A, B) should fail - would create cycle")
		}
		if !strings.Contains(err.Error(), "cycle") {
			t.Errorf("Error should mention cycle, got: %v", err)
		}
	})

	t.Run("blocking cycle via blocked_by is detected", func(t *testing.T) {
		// A is blocked_by B, then A blocking B creates a conflict
		a := &nib.Nib{ID: "cross2-a", Title: "Nib A", Status: "todo"}
		b := &nib.Nib{ID: "cross2-b", Title: "Nib B", Status: "todo"}
		mustCreate(t, core, a)
		mustCreate(t, core, b)

		mr := resolver.Mutation()

		// A is blocked by B (B → A)
		_, err := mr.AddBlockedBy(ctx, "cross2-a", "cross2-b", nil)
		if err != nil {
			t.Fatalf("AddBlockedBy(A, B) error = %v", err)
		}

		// A blocks B (A → B) - should create cycle
		_, err = mr.AddBlocking(ctx, "cross2-a", "cross2-b")
		if err == nil {
			t.Error("AddBlocking(A, B) should fail - would create cycle")
		}
		if !strings.Contains(err.Error(), "cycle") {
			t.Errorf("Error should mention cycle, got: %v", err)
		}
	})

	t.Run("blocker nib not found fails", func(t *testing.T) {
		a := &nib.Nib{ID: "exists-a", Title: "Nib A", Status: "todo"}
		mustCreate(t, core, a)

		mr := resolver.Mutation()
		_, err := mr.AddBlockedBy(ctx, "exists-a", "nonexistent", nil)
		if err == nil {
			t.Error("AddBlockedBy() should fail when blocker doesn't exist")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("Error should mention not found, got: %v", err)
		}
	})
}

func TestCreateNibBlockedByValidation(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	t.Run("create with blocked_by referencing nonexistent nib fails", func(t *testing.T) {
		mr := resolver.Mutation()
		input := model.CreateNibInput{
			Title:     "New Nib",
			BlockedBy: []string{"nonexistent"},
		}
		_, err := mr.CreateNib(ctx, input)
		if err == nil {
			t.Error("CreateNib() should fail when blocked_by references nonexistent nib")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("Error should mention not found, got: %v", err)
		}
	})

	t.Run("create with blocking referencing nonexistent nib fails", func(t *testing.T) {
		mr := resolver.Mutation()
		input := model.CreateNibInput{
			Title:    "New Nib",
			Blocking: []string{"nonexistent"},
		}
		_, err := mr.CreateNib(ctx, input)
		if err == nil {
			t.Error("CreateNib() should fail when blocking references nonexistent nib")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("Error should mention not found, got: %v", err)
		}
	})

	t.Run("create with same nib in both blocking and blocked_by fails", func(t *testing.T) {
		target := &nib.Nib{ID: "target-nib", Title: "Target", Status: "todo"}
		mustCreate(t, core, target)

		mr := resolver.Mutation()
		input := model.CreateNibInput{
			Title:     "Cyclic Nib",
			Blocking:  []string{"target-nib"},
			BlockedBy: []string{"target-nib"},
		}
		_, err := mr.CreateNib(ctx, input)
		if err == nil {
			t.Error("CreateNib() should fail when same nib is in both blocking and blocked_by")
		}
		if !strings.Contains(err.Error(), "cycle") {
			t.Errorf("Error should mention cycle, got: %v", err)
		}
	})

	t.Run("create with valid blocked_by succeeds", func(t *testing.T) {
		blocker := &nib.Nib{ID: "valid-blocker", Title: "Blocker", Status: "todo"}
		mustCreate(t, core, blocker)

		mr := resolver.Mutation()
		input := model.CreateNibInput{
			Title:     "Blocked Nib",
			BlockedBy: []string{"valid-blocker"},
		}
		got, err := mr.CreateNib(ctx, input)
		if err != nil {
			t.Fatalf("CreateNib() error = %v", err)
		}
		if len(got.BlockedBy) != 1 {
			t.Errorf("CreateNib().BlockedBy count = %d, want 1", len(got.BlockedBy))
		}
		if got.BlockedBy[0] != "valid-blocker" {
			t.Errorf("CreateNib().BlockedBy[0] = %q, want %q", got.BlockedBy[0], "valid-blocker")
		}
	})
}

func TestMutationAddRemoveBlockedBy(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	// Create test nibs
	blocked := &nib.Nib{ID: "blocked-1", Title: "Blocked", Status: "todo"}
	blocker := &nib.Nib{ID: "blocker-1", Title: "Blocker", Status: "todo"}
	mustCreate(t, core, blocked)
	mustCreate(t, core, blocker)

	t.Run("add blocked_by", func(t *testing.T) {
		mr := resolver.Mutation()
		got, err := mr.AddBlockedBy(ctx, "blocked-1", "blocker-1", nil)
		if err != nil {
			t.Fatalf("AddBlockedBy() error = %v", err)
		}
		if len(got.BlockedBy) != 1 {
			t.Errorf("AddBlockedBy().BlockedBy count = %d, want 1", len(got.BlockedBy))
		}
		if got.BlockedBy[0] != "blocker-1" {
			t.Errorf("AddBlockedBy().BlockedBy[0] = %q, want %q", got.BlockedBy[0], "blocker-1")
		}
	})

	t.Run("remove blocked_by", func(t *testing.T) {
		mr := resolver.Mutation()
		got, err := mr.RemoveBlockedBy(ctx, "blocked-1", "blocker-1", nil)
		if err != nil {
			t.Fatalf("RemoveBlockedBy() error = %v", err)
		}
		if len(got.BlockedBy) != 0 {
			t.Errorf("RemoveBlockedBy().BlockedBy count = %d, want 0", len(got.BlockedBy))
		}
	})

	t.Run("add blocked_by to nonexistent nib fails", func(t *testing.T) {
		mr := resolver.Mutation()
		_, err := mr.AddBlockedBy(ctx, "nonexistent", "blocker-1", nil)
		if err == nil {
			t.Error("AddBlockedBy() expected error for nonexistent nib")
		}
	})
}

func TestUpdateNibWithETag(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	t.Run("update with correct etag succeeds", func(t *testing.T) {
		b := &nib.Nib{
			ID:     "etag-update-1",
			Title:  "Test",
			Status: "todo",
		}
		mustCreate(t, core, b)

		currentETag := b.ETag()
		newTitle := "Updated"
		input := model.UpdateNibInput{
			Title:   &newTitle,
			IfMatch: &currentETag,
		}

		got, err := resolver.Mutation().UpdateNib(ctx, "etag-update-1", input)
		if err != nil {
			t.Fatalf("UpdateNib() with correct etag failed: %v", err)
		}
		if got.Title != "Updated" {
			t.Errorf("UpdateNib().Title = %q, want %q", got.Title, "Updated")
		}
	})

	t.Run("update with wrong etag fails", func(t *testing.T) {
		b := &nib.Nib{
			ID:     "etag-update-2",
			Title:  "Test",
			Status: "todo",
		}
		mustCreate(t, core, b)

		wrongETag := "wrongetag123"
		newTitle := "Should Fail"
		input := model.UpdateNibInput{
			Title:   &newTitle,
			IfMatch: &wrongETag,
		}

		_, err := resolver.Mutation().UpdateNib(ctx, "etag-update-2", input)
		if err == nil {
			t.Error("UpdateNib() with wrong etag should fail")
		}

		var mismatchErr *nibcore.ETagMismatchError
		if !errors.As(err, &mismatchErr) {
			t.Errorf("Expected ETagMismatchError, got %T: %v", err, err)
		}
	})
}

func TestSetParentWithETag(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	// Create parent
	parent := &nib.Nib{
		ID:     "parent-etag",
		Title:  "Parent",
		Status: "todo",
		Type:   "epic",
	}
	mustCreate(t, core, parent)

	t.Run("setParent with correct etag succeeds", func(t *testing.T) {
		child := &nib.Nib{
			ID:     "child-etag-1",
			Title:  "Child",
			Status: "todo",
			Type:   "task",
		}
		mustCreate(t, core, child)

		currentETag := child.ETag()
		parentID := "parent-etag"

		got, err := resolver.Mutation().SetParent(ctx, "child-etag-1", &parentID, &currentETag)
		if err != nil {
			t.Fatalf("SetParent() with correct etag failed: %v", err)
		}
		if got.Parent != "parent-etag" {
			t.Errorf("SetParent().Parent = %q, want %q", got.Parent, "parent-etag")
		}
	})

	t.Run("setParent with wrong etag fails", func(t *testing.T) {
		child := &nib.Nib{
			ID:     "child-etag-2",
			Title:  "Child",
			Status: "todo",
			Type:   "task",
		}
		mustCreate(t, core, child)

		wrongETag := "wrongetag123"
		parentID := "parent-etag"

		_, err := resolver.Mutation().SetParent(ctx, "child-etag-2", &parentID, &wrongETag)
		if err == nil {
			t.Error("SetParent() with wrong etag should fail")
		}

		var mismatchErr *nibcore.ETagMismatchError
		if !errors.As(err, &mismatchErr) {
			t.Errorf("Expected ETagMismatchError, got %T: %v", err, err)
		}
	})
}

func TestSingleSideBlocking(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	t.Run("addBlocking stores on target's blockedBy", func(t *testing.T) {
		blocker := &nib.Nib{ID: "ss-blocker-1", Title: "Blocker", Status: "todo", Type: "task"}
		target := &nib.Nib{ID: "ss-target-1", Title: "Target", Status: "todo", Type: "task"}
		mustCreate(t, core, blocker)
		mustCreate(t, core, target)

		mr := resolver.Mutation()
		_, err := mr.AddBlocking(ctx, "ss-blocker-1", "ss-target-1")
		if err != nil {
			t.Fatalf("AddBlocking() error = %v", err)
		}

		// Target's blockedBy should now include the blocker
		targetAfter, _ := core.Get("ss-target-1")
		if !targetAfter.IsBlockedBy("ss-blocker-1") {
			t.Errorf("target.BlockedBy should contain blocker, got %v", targetAfter.BlockedBy)
		}

		// Blocker's Blocking field should NOT be modified (single-side storage)
		blockerAfter, _ := core.Get("ss-blocker-1")
		if len(blockerAfter.Blocking) != 0 {
			t.Errorf("blocker.Blocking should be empty (not persisted), got %v", blockerAfter.Blocking)
		}
	})

	t.Run("removeBlocking removes from target's blockedBy", func(t *testing.T) {
		blocker := &nib.Nib{ID: "ss-blocker-2", Title: "Blocker", Status: "todo", Type: "task"}
		target := &nib.Nib{ID: "ss-target-2", Title: "Target", Status: "todo", Type: "task", BlockedBy: []string{"ss-blocker-2"}}
		mustCreate(t, core, blocker)
		mustCreate(t, core, target)

		mr := resolver.Mutation()
		_, err := mr.RemoveBlocking(ctx, "ss-blocker-2", "ss-target-2")
		if err != nil {
			t.Fatalf("RemoveBlocking() error = %v", err)
		}

		// Target's blockedBy should no longer include the blocker
		targetAfter, _ := core.Get("ss-target-2")
		if targetAfter.IsBlockedBy("ss-blocker-2") {
			t.Errorf("target.BlockedBy should not contain blocker after removal, got %v", targetAfter.BlockedBy)
		}
	})

	t.Run("blockingIds computed from blockedBy scan", func(t *testing.T) {
		blocker := &nib.Nib{ID: "ss-blocker-3", Title: "Blocker", Status: "todo", Type: "task"}
		target := &nib.Nib{ID: "ss-target-3", Title: "Target", Status: "todo", Type: "task", BlockedBy: []string{"ss-blocker-3"}}
		mustCreate(t, core, blocker)
		mustCreate(t, core, target)

		br := resolver.Nib()
		ids, err := br.BlockingIds(ctx, blocker)
		if err != nil {
			t.Fatalf("BlockingIds() error = %v", err)
		}
		if len(ids) != 1 || ids[0] != "ss-target-3" {
			t.Errorf("BlockingIds() = %v, want [ss-target-3]", ids)
		}
	})

	t.Run("blocking resolver computed from blockedBy scan", func(t *testing.T) {
		blocker := &nib.Nib{ID: "ss-blocker-4", Title: "Blocker", Status: "todo", Type: "task"}
		target := &nib.Nib{ID: "ss-target-4", Title: "Target", Status: "todo", Type: "task", BlockedBy: []string{"ss-blocker-4"}}
		mustCreate(t, core, blocker)
		mustCreate(t, core, target)

		br := resolver.Nib()
		got, err := br.Blocking(ctx, blocker, nil)
		if err != nil {
			t.Fatalf("Blocking() error = %v", err)
		}
		if len(got) != 1 || got[0].ID != "ss-target-4" {
			ids := make([]string, len(got))
			for i, n := range got {
				ids[i] = n.ID
			}
			t.Errorf("Blocking() IDs = %v, want [ss-target-4]", ids)
		}
	})

	t.Run("CreateNib blocking creates blockedBy on targets", func(t *testing.T) {
		target := &nib.Nib{ID: "ss-create-target", Title: "Target", Status: "todo", Type: "task"}
		mustCreate(t, core, target)

		mr := resolver.Mutation()
		created, err := mr.CreateNib(ctx, model.CreateNibInput{
			Title:    "New Blocker",
			Blocking: []string{"ss-create-target"},
		})
		if err != nil {
			t.Fatalf("CreateNib() error = %v", err)
		}

		// The created nib should NOT have Blocking field set
		if len(created.Blocking) != 0 {
			t.Errorf("created nib should not have Blocking field, got %v", created.Blocking)
		}

		// Target should have the new nib in its blockedBy
		targetAfter, _ := core.Get("ss-create-target")
		if !targetAfter.IsBlockedBy(created.ID) {
			t.Errorf("target.BlockedBy should contain %s, got %v", created.ID, targetAfter.BlockedBy)
		}
	})

	t.Run("UpdateNib addBlocking modifies target's blockedBy", func(t *testing.T) {
		blocker := &nib.Nib{ID: "ss-upd-blocker", Title: "Blocker", Status: "todo", Type: "task"}
		target := &nib.Nib{ID: "ss-upd-target", Title: "Target", Status: "todo", Type: "task"}
		mustCreate(t, core, blocker)
		mustCreate(t, core, target)

		mr := resolver.Mutation()
		_, err := mr.UpdateNib(ctx, "ss-upd-blocker", model.UpdateNibInput{
			AddBlocking: []string{"ss-upd-target"},
		})
		if err != nil {
			t.Fatalf("UpdateNib() error = %v", err)
		}

		// Target's blockedBy should include blocker
		targetAfter, _ := core.Get("ss-upd-target")
		if !targetAfter.IsBlockedBy("ss-upd-blocker") {
			t.Errorf("target.BlockedBy should contain blocker, got %v", targetAfter.BlockedBy)
		}

		// Blocker should NOT have Blocking field set
		blockerAfter, _ := core.Get("ss-upd-blocker")
		if len(blockerAfter.Blocking) != 0 {
			t.Errorf("blocker.Blocking should be empty, got %v", blockerAfter.Blocking)
		}
	})

	t.Run("UpdateNib removeBlocking modifies target's blockedBy", func(t *testing.T) {
		blocker := &nib.Nib{ID: "ss-upd-rm-blocker", Title: "Blocker", Status: "todo", Type: "task"}
		target := &nib.Nib{ID: "ss-upd-rm-target", Title: "Target", Status: "todo", Type: "task", BlockedBy: []string{"ss-upd-rm-blocker"}}
		mustCreate(t, core, blocker)
		mustCreate(t, core, target)

		mr := resolver.Mutation()
		_, err := mr.UpdateNib(ctx, "ss-upd-rm-blocker", model.UpdateNibInput{
			RemoveBlocking: []string{"ss-upd-rm-target"},
		})
		if err != nil {
			t.Fatalf("UpdateNib() error = %v", err)
		}

		// Target's blockedBy should no longer include blocker
		targetAfter, _ := core.Get("ss-upd-rm-target")
		if targetAfter.IsBlockedBy("ss-upd-rm-blocker") {
			t.Errorf("target.BlockedBy should not contain blocker, got %v", targetAfter.BlockedBy)
		}
	})
}

func TestDocumentsGraphQL(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	t.Run("create with documents", func(t *testing.T) {
		mr := resolver.Mutation()
		got, err := mr.CreateNib(ctx, model.CreateNibInput{
			Title:     "Doc Nib",
			Documents: []string{"docs/prd.md", "research/notes.md"},
		})
		if err != nil {
			t.Fatalf("CreateNib() error = %v", err)
		}
		if len(got.Documents) != 2 {
			t.Errorf("Documents count = %d, want 2", len(got.Documents))
		}
	})

	t.Run("update addDocuments", func(t *testing.T) {
		b := &nib.Nib{ID: "doc-upd-1", Title: "Test", Status: "todo", Documents: []string{"existing.md"}}
		mustCreate(t, core, b)

		mr := resolver.Mutation()
		got, err := mr.UpdateNib(ctx, "doc-upd-1", model.UpdateNibInput{
			AddDocuments: []string{"new.md"},
		})
		if err != nil {
			t.Fatalf("UpdateNib() error = %v", err)
		}
		if len(got.Documents) != 2 {
			t.Errorf("Documents count = %d, want 2", len(got.Documents))
		}
	})

	t.Run("update removeDocuments", func(t *testing.T) {
		b := &nib.Nib{ID: "doc-upd-2", Title: "Test", Status: "todo", Documents: []string{"keep.md", "remove.md"}}
		mustCreate(t, core, b)

		mr := resolver.Mutation()
		got, err := mr.UpdateNib(ctx, "doc-upd-2", model.UpdateNibInput{
			RemoveDocuments: []string{"remove.md"},
		})
		if err != nil {
			t.Fatalf("UpdateNib() error = %v", err)
		}
		if len(got.Documents) != 1 {
			t.Errorf("Documents count = %d, want 1", len(got.Documents))
		}
		if len(got.Documents) > 0 && got.Documents[0] != "keep.md" {
			t.Errorf("Documents = %v, want [keep.md]", got.Documents)
		}
	})

	t.Run("update replace documents", func(t *testing.T) {
		b := &nib.Nib{ID: "doc-upd-3", Title: "Test", Status: "todo", Documents: []string{"old.md"}}
		mustCreate(t, core, b)

		mr := resolver.Mutation()
		got, err := mr.UpdateNib(ctx, "doc-upd-3", model.UpdateNibInput{
			Documents: []string{"new1.md", "new2.md"},
		})
		if err != nil {
			t.Fatalf("UpdateNib() error = %v", err)
		}
		if len(got.Documents) != 2 {
			t.Errorf("Documents count = %d, want 2", len(got.Documents))
		}
	})

	t.Run("documents and addDocuments mutually exclusive", func(t *testing.T) {
		b := &nib.Nib{ID: "doc-upd-4", Title: "Test", Status: "todo"}
		mustCreate(t, core, b)

		mr := resolver.Mutation()
		_, err := mr.UpdateNib(ctx, "doc-upd-4", model.UpdateNibInput{
			Documents:    []string{"a.md"},
			AddDocuments: []string{"b.md"},
		})
		if err == nil {
			t.Error("UpdateNib() should fail when both documents and addDocuments specified")
		}
	})
}

func TestValidateDocumentPaths(t *testing.T) {
	tests := []struct {
		name    string
		paths   []string
		wantErr bool
	}{
		{"nil paths", nil, false},
		{"empty slice", []string{}, false},
		{"relative path", []string{"docs/design.md"}, false},
		{"nested relative path", []string{"src/internal/file.go"}, false},
		{"current dir relative", []string{"./README.md"}, false},
		{"absolute unix path", []string{"/etc/passwd"}, true},
		{"parent traversal", []string{"../secret.txt"}, true},
		{"deep parent traversal", []string{"../../etc/passwd"}, true},
		{"embedded traversal", []string{"foo/../../etc/passwd"}, true},
		{"mixed valid and invalid", []string{"docs/ok.md", "../bad.md"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDocumentPaths(tt.paths)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateDocumentPaths(%v) error = %v, wantErr %v", tt.paths, err, tt.wantErr)
			}
		})
	}
}

func TestChildrenSortedByOrder(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	// Create a parent
	parent := &nib.Nib{ID: "parent-1", Title: "Parent", Status: "todo", Type: "epic", Version: 1}
	if err := core.Create(parent); err != nil {
		t.Fatal(err)
	}

	// Create children with explicit order keys (out of lexicographic order)
	children := []struct {
		id    string
		title string
		order string
	}{
		{"child-c", "Third", "c0"},
		{"child-a", "First", "a0"},
		{"child-b", "Second", "b0"},
	}
	for _, c := range children {
		child := &nib.Nib{
			ID:      c.id,
			Title:   c.title,
			Status:  "todo",
			Type:    "task",
			Parent:  "parent-1",
			Order:   c.order,
			Version: 1,
		}
		if err := core.Create(child); err != nil {
			t.Fatal(err)
		}
	}

	got, err := resolver.Nib().Children(ctx, parent, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 3 {
		t.Fatalf("expected 3 children, got %d", len(got))
	}

	expectedOrder := []string{"child-a", "child-b", "child-c"}
	for i, expected := range expectedOrder {
		if got[i].ID != expected {
			t.Errorf("children[%d].ID = %q, want %q", i, got[i].ID, expected)
		}
	}
}

func TestChildrenBackfillOrder(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	// Create a parent (IDs without hyphens to avoid ParseFilename legacy id-slug split on reload)
	parent := &nib.Nib{ID: "parent1", Title: "Parent", Status: "todo", Type: "epic", Version: 1}
	if err := core.Create(parent); err != nil {
		t.Fatal(err)
	}

	// Create children WITHOUT order keys
	for _, c := range []struct{ id, title string }{
		{"czebra", "Zebra"}, {"capple", "Apple"}, {"cmango", "Mango"},
	} {
		child := &nib.Nib{
			ID:      c.id,
			Title:   c.title,
			Status:  "todo",
			Type:    "task",
			Parent:  "parent1",
			Version: 1,
		}
		if err := core.Create(child); err != nil {
			t.Fatal(err)
		}
	}

	// First query should backfill order keys
	got, err := resolver.Nib().Children(ctx, parent, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 children, got %d", len(got))
	}

	// Backfill should preserve title-based sort order (Apple, Mango, Zebra)
	expectedOrder := []string{"capple", "cmango", "czebra"}
	for i, expected := range expectedOrder {
		if got[i].ID != expected {
			t.Errorf("children[%d].ID = %q, want %q", i, got[i].ID, expected)
		}
	}

	// After backfill, all children should have order keys
	for _, child := range got {
		if child.Order == "" {
			t.Errorf("child %q should have order key after backfill", child.ID)
		}
	}

	// Order keys should be strictly increasing
	for i := 1; i < len(got); i++ {
		if got[i].Order <= got[i-1].Order {
			t.Errorf("children[%d].Order %q should be > children[%d].Order %q",
				i, got[i].Order, i-1, got[i-1].Order)
		}
	}

	// Backfilled order keys must be persisted to disk (survives reload)
	savedOrders := make(map[string]string)
	for _, child := range got {
		savedOrders[child.ID] = child.Order
	}

	if err := core.Load(); err != nil {
		t.Fatal(err)
	}
	for id, expectedOrder := range savedOrders {
		reloaded, err := core.Get(id)
		if err != nil {
			t.Fatalf("failed to get %s after reload: %v", id, err)
		}
		if reloaded.Order != expectedOrder {
			t.Errorf("%s: order after reload = %q, want %q", id, reloaded.Order, expectedOrder)
		}
	}
}

func TestCreateNibPositioning(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	// Create parent epic
	parent := &nib.Nib{ID: "epic1", Title: "Epic", Status: "todo", Type: "epic", Version: 1}
	if err := core.Create(parent); err != nil {
		t.Fatal(err)
	}

	// Create two ordered children
	child1 := &nib.Nib{ID: "task1", Title: "First", Status: "todo", Type: "task", Parent: "epic1", Order: "a0", Version: 1}
	child2 := &nib.Nib{ID: "task2", Title: "Second", Status: "todo", Type: "task", Parent: "epic1", Order: "b0", Version: 1}
	for _, c := range []*nib.Nib{child1, child2} {
		if err := core.Create(c); err != nil {
			t.Fatal(err)
		}
	}

	taskType := "task"
	parentID := "epic1"

	t.Run("afterId inserts after target sibling", func(t *testing.T) {
		afterID := "task1"
		created, err := resolver.Mutation().CreateNib(ctx, model.CreateNibInput{
			Title:   "After First",
			Type:    &taskType,
			Parent:  &parentID,
			AfterID: &afterID,
		})
		if err != nil {
			t.Fatalf("CreateNib error: %v", err)
		}
		if created.Order <= child1.Order {
			t.Errorf("order %q should be > child1 order %q", created.Order, child1.Order)
		}
		if created.Order >= child2.Order {
			t.Errorf("order %q should be < child2 order %q", created.Order, child2.Order)
		}
	})

	t.Run("beforeId inserts before target sibling", func(t *testing.T) {
		beforeID := "task2"
		created, err := resolver.Mutation().CreateNib(ctx, model.CreateNibInput{
			Title:    "Before Second",
			Type:     &taskType,
			Parent:   &parentID,
			BeforeID: &beforeID,
		})
		if err != nil {
			t.Fatalf("CreateNib error: %v", err)
		}
		if created.Order >= child2.Order {
			t.Errorf("order %q should be < child2 order %q", created.Order, child2.Order)
		}
	})

	t.Run("first inserts before all siblings", func(t *testing.T) {
		firstFlag := true
		created, err := resolver.Mutation().CreateNib(ctx, model.CreateNibInput{
			Title:  "Very First",
			Type:   &taskType,
			Parent: &parentID,
			First:  &firstFlag,
		})
		if err != nil {
			t.Fatalf("CreateNib error: %v", err)
		}
		// Must be before the first original child
		if created.Order >= child1.Order {
			t.Errorf("order %q should be < child1 order %q", created.Order, child1.Order)
		}
	})

	t.Run("default inserts last among same priority", func(t *testing.T) {
		// child1 and child2 have default priority (normal after nibcore default)
		// A new normal-priority task should go after them
		created, err := resolver.Mutation().CreateNib(ctx, model.CreateNibInput{
			Title:  "Default Position",
			Type:   &taskType,
			Parent: &parentID,
		})
		if err != nil {
			t.Fatalf("CreateNib error: %v", err)
		}
		if created.Order <= child2.Order {
			t.Errorf("order %q should be > last sibling order %q", created.Order, child2.Order)
		}
	})

	t.Run("mutual exclusivity error", func(t *testing.T) {
		afterID := "task1"
		beforeID := "task2"
		_, err := resolver.Mutation().CreateNib(ctx, model.CreateNibInput{
			Title:    "Bad",
			Type:     &taskType,
			Parent:   &parentID,
			AfterID:  &afterID,
			BeforeID: &beforeID,
		})
		if err == nil {
			t.Fatal("expected error for mutual exclusivity")
		}
		if !strings.Contains(err.Error(), "at most one") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("first flag on root inserts before existing roots", func(t *testing.T) {
		// `epic1` from the fixture above is an existing root nib. A new root
		// created with --first should land before it. This is the inverse of
		// the previous bug-pinning subtest which asserted that positioning
		// without a parent was rejected — see nibs-d44y.
		existingRootID := "epic1"
		if _, err := resolver.Query().Nib(ctx, existingRootID); err != nil {
			t.Fatalf("missing fixture root %q: %v", existingRootID, err)
		}
		firstFlag := true
		created, err := resolver.Mutation().CreateNib(ctx, model.CreateNibInput{
			Title: "Root First",
			Type:  &taskType,
			First: &firstFlag,
		})
		if err != nil {
			t.Fatalf("CreateNib with --first on root failed: %v", err)
		}
		if created.Parent != "" {
			t.Errorf("created root should have no parent, got %q", created.Parent)
		}
		// Re-read the existing root AFTER the create: positioning backfilled it an
		// order key, and under clone-before-mutate (nibs-twvo) that key lives on
		// the nib's fresh store entry — a pointer captured before the call keeps its
		// old (empty) order instead of being aliased-mutated in place. Query the
		// current entry to observe the persisted order.
		existing, err := resolver.Query().Nib(ctx, existingRootID)
		if err != nil || existing == nil {
			t.Fatalf("missing fixture root %q after create: %v", existingRootID, err)
		}
		if created.Order >= existing.Order {
			t.Errorf("order %q should be < existing root order %q", created.Order, existing.Order)
		}
	})

	t.Run("afterId with nonexistent sibling is error", func(t *testing.T) {
		afterID := "nonexistent"
		_, err := resolver.Mutation().CreateNib(ctx, model.CreateNibInput{
			Title:   "Bad Target",
			Type:    &taskType,
			Parent:  &parentID,
			AfterID: &afterID,
		})
		if err == nil {
			t.Fatal("expected error for nonexistent sibling")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func setupReorderFixture(t *testing.T) (*Resolver, *nibcore.Core) {
	t.Helper()
	resolver, core := setupTestResolver(t)
	parent := &nib.Nib{ID: "epic1", Title: "Epic", Status: "todo", Type: "epic", Version: 1}
	if err := core.Create(parent); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ id, title, order string }{
		{"t1", "First", "a0"}, {"t2", "Second", "b0"}, {"t3", "Third", "c0"},
	} {
		child := &nib.Nib{ID: c.id, Title: c.title, Status: "todo", Type: "task", Parent: "epic1", Order: c.order, Version: 1}
		if err := core.Create(child); err != nil {
			t.Fatal(err)
		}
	}
	return resolver, core
}

func TestReorderNib(t *testing.T) {
	ctx := context.Background()

	t.Run("reorder after sibling", func(t *testing.T) {
		resolver, _ := setupReorderFixture(t)
		afterID := "t2"
		got, err := resolver.Mutation().ReorderNib(ctx, "t1", &afterID, nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("ReorderNib error: %v", err)
		}
		if got.Order <= "b0" {
			t.Errorf("order %q should be > b0", got.Order)
		}
		if got.Order >= "c0" {
			t.Errorf("order %q should be < c0", got.Order)
		}
	})

	t.Run("reorder before sibling", func(t *testing.T) {
		resolver, _ := setupReorderFixture(t)
		beforeID := "t2"
		got, err := resolver.Mutation().ReorderNib(ctx, "t3", nil, &beforeID, nil, nil, nil)
		if err != nil {
			t.Fatalf("ReorderNib error: %v", err)
		}
		if got.Order <= "a0" {
			t.Errorf("order %q should be > a0 (after t1)", got.Order)
		}
		if got.Order >= "b0" {
			t.Errorf("order %q should be < b0 (before t2)", got.Order)
		}
	})

	t.Run("reorder to first", func(t *testing.T) {
		resolver, _ := setupReorderFixture(t)
		firstFlag := true
		got, err := resolver.Mutation().ReorderNib(ctx, "t3", nil, nil, &firstFlag, nil, nil)
		if err != nil {
			t.Fatalf("ReorderNib error: %v", err)
		}
		if got.Order >= "a0" {
			t.Errorf("order %q should be < a0 (before first sibling)", got.Order)
		}
	})

	t.Run("error when no positioning flag", func(t *testing.T) {
		resolver, _ := setupReorderFixture(t)
		_, err := resolver.Mutation().ReorderNib(ctx, "t1", nil, nil, nil, nil, nil)
		if err == nil {
			t.Fatal("expected error when no positioning flag")
		}
		if !strings.Contains(err.Error(), "positioning flag") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("reorder root nib succeeds", func(t *testing.T) {
		resolver, _ := setupReorderFixture(t)
		firstFlag := true
		got, err := resolver.Mutation().ReorderNib(ctx, "epic1", nil, nil, &firstFlag, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Order == "" {
			t.Error("expected order key to be set")
		}
		if err := nib.ValidateOrderKey(got.Order); err != nil {
			t.Errorf("order key is not valid base-62: %v", err)
		}
	})

	t.Run("cross-parent reorder with parentId", func(t *testing.T) {
		resolver, core := setupReorderFixture(t)
		// Create a second epic with children
		epic2 := &nib.Nib{ID: "epic2", Title: "Epic 2", Status: "todo", Type: "epic", Version: 1}
		if err := core.Create(epic2); err != nil {
			t.Fatal(err)
		}
		for _, c := range []struct{ id, title, order string }{
			{"x1", "X First", "a0"}, {"x2", "X Second", "b0"},
		} {
			child := &nib.Nib{ID: c.id, Title: c.title, Status: "todo", Type: "task", Parent: "epic2", Order: c.order, Version: 1}
			if err := core.Create(child); err != nil {
				t.Fatal(err)
			}
		}

		// Move t1 (child of epic1) to before x2 (child of epic2) using parentId
		beforeID := "x2"
		newParent := "epic2"
		got, err := resolver.Mutation().ReorderNib(ctx, "t1", nil, &beforeID, nil, &newParent, nil)
		if err != nil {
			t.Fatalf("cross-parent ReorderNib error: %v", err)
		}

		// Verify parent changed
		if got.Parent != "epic2" {
			t.Errorf("expected parent epic2, got %q", got.Parent)
		}

		// Verify order is between x1 and x2
		if got.Order <= "a0" {
			t.Errorf("order %q should be > a0 (after x1)", got.Order)
		}
		if got.Order >= "b0" {
			t.Errorf("order %q should be < b0 (before x2)", got.Order)
		}
	})
}

func TestCreateNibWithEstimate(t *testing.T) {
	resolver, _ := setupTestResolver(t)
	ctx := context.Background()

	t.Run("creates with estimate", func(t *testing.T) {
		est := "l"
		b, err := resolver.Mutation().CreateNib(ctx, model.CreateNibInput{
			Title:    "Large Task",
			Estimate: &est,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if b.Estimate != "l" {
			t.Errorf("Estimate = %q, want %q", b.Estimate, "l")
		}
	})

	t.Run("creates without estimate", func(t *testing.T) {
		b, err := resolver.Mutation().CreateNib(ctx, model.CreateNibInput{
			Title: "No Estimate Task",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if b.Estimate != "" {
			t.Errorf("Estimate = %q, want empty", b.Estimate)
		}
	})
}

func TestUpdateNibEstimate(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	t.Run("set estimate", func(t *testing.T) {
		b := createTestNib(t, core, "est-1", "Task", "todo")
		est := "m"
		updated, err := resolver.Mutation().UpdateNib(ctx, b.ID, model.UpdateNibInput{
			Estimate: graphql.OmittableOf(&est),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if updated.Estimate != "m" {
			t.Errorf("Estimate = %q, want %q", updated.Estimate, "m")
		}

		// Verify persistence
		got, err := resolver.Query().Nib(ctx, b.ID)
		if err != nil {
			t.Fatalf("failed to query nib: %v", err)
		}
		if got.Estimate != "m" {
			t.Errorf("Persisted Estimate = %q, want %q", got.Estimate, "m")
		}
	})

	t.Run("clear estimate", func(t *testing.T) {
		b := createTestNib(t, core, "est-2", "Estimated Task", "todo")
		etag := b.ETag() // compute before mutation
		b.Estimate = "xl"
		if err := core.Update(b, &etag); err != nil {
			t.Fatalf("failed to update test nib estimate: %v", err)
		}

		empty := ""
		updated, err := resolver.Mutation().UpdateNib(ctx, b.ID, model.UpdateNibInput{
			Estimate: graphql.OmittableOf(&empty),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if updated.Estimate != "" {
			t.Errorf("Estimate = %q, want empty", updated.Estimate)
		}
	})
}

func TestQueryNibsEstimateFilter(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	// Create nibs with various estimates
	n1 := createTestNib(t, core, "ef-1", "Small", "todo")
	etag1 := n1.ETag() // compute before mutation
	n1.Estimate = "s"
	if err := core.Update(n1, &etag1); err != nil {
		t.Fatalf("failed to update test nib estimate: %v", err)
	}

	n2 := createTestNib(t, core, "ef-2", "Large", "todo")
	etag2 := n2.ETag() // compute before mutation
	n2.Estimate = "l"
	if err := core.Update(n2, &etag2); err != nil {
		t.Fatalf("failed to update test nib estimate: %v", err)
	}

	createTestNib(t, core, "ef-3", "Unestimated", "todo")

	t.Run("filter by estimate", func(t *testing.T) {
		got, err := resolver.Query().Nibs(ctx, &model.NibFilter{
			Estimate: []string{"s"},
		}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0].ID != "ef-1" {
			ids := make([]string, len(got))
			for i, n := range got {
				ids[i] = n.ID
			}
			t.Errorf("got %v, want [ef-1]", ids)
		}
	})

	t.Run("exclude by estimate", func(t *testing.T) {
		got, err := resolver.Query().Nibs(ctx, &model.NibFilter{
			ExcludeEstimate: []string{"s", "l"},
		}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0].ID != "ef-3" {
			ids := make([]string, len(got))
			for i, n := range got {
				ids[i] = n.ID
			}
			t.Errorf("got %v, want [ef-3]", ids)
		}
	})
}

func TestUpdateNibTypeChangeValidatesParent(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	t.Run("change task to milestone rejects because milestones cannot have parents", func(t *testing.T) {
		feat := &nib.Nib{ID: "feat-tc-ms", Title: "Feature", Type: "feature", Status: "todo"}
		task := &nib.Nib{ID: "task-tc-ms", Title: "Task", Type: "task", Status: "todo", Parent: "feat-tc-ms"}
		mustCreate(t, core, feat)
		mustCreate(t, core, task)

		newType := "milestone"
		input := model.UpdateNibInput{Type: &newType}
		_, err := resolver.Mutation().UpdateNib(ctx, "task-tc-ms", input)
		if err == nil {
			t.Fatal("expected error when changing task to milestone with existing parent, got nil")
		}
		if !strings.Contains(err.Error(), "parent") {
			t.Errorf("expected hierarchy validation error, got: %v", err)
		}
	})

	t.Run("change task to epic rejects because epic cannot have feature parent", func(t *testing.T) {
		feat := &nib.Nib{ID: "feat-tc-ep", Title: "Feature", Type: "feature", Status: "todo"}
		task := &nib.Nib{ID: "task-tc-ep", Title: "Task", Type: "task", Status: "todo", Parent: "feat-tc-ep"}
		mustCreate(t, core, feat)
		mustCreate(t, core, task)

		newType := "epic"
		input := model.UpdateNibInput{Type: &newType}
		_, err := resolver.Mutation().UpdateNib(ctx, "task-tc-ep", input)
		if err == nil {
			t.Fatal("expected error when changing task to epic under a feature parent, got nil")
		}
		if !strings.Contains(err.Error(), "parent") {
			t.Errorf("expected hierarchy validation error, got: %v", err)
		}
	})

	t.Run("change task to epic under milestone parent succeeds", func(t *testing.T) {
		milestone := &nib.Nib{ID: "ms-tc", Title: "Milestone", Type: "milestone", Status: "todo"}
		taskUnderMs := &nib.Nib{ID: "task-tc2", Title: "Task2", Type: "task", Status: "todo", Parent: "ms-tc"}
		mustCreate(t, core, milestone)
		mustCreate(t, core, taskUnderMs)

		newType := "epic"
		input := model.UpdateNibInput{Type: &newType}
		got, err := resolver.Mutation().UpdateNib(ctx, "task-tc2", input)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if got.Type != "epic" {
			t.Errorf("type = %q, want %q", got.Type, "epic")
		}
	})

	t.Run("change type with no parent always succeeds", func(t *testing.T) {
		rootTask := &nib.Nib{ID: "root-tc", Title: "Root Task", Type: "task", Status: "todo"}
		mustCreate(t, core, rootTask)

		newType := "milestone"
		input := model.UpdateNibInput{Type: &newType}
		got, err := resolver.Mutation().UpdateNib(ctx, "root-tc", input)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if got.Type != "milestone" {
			t.Errorf("type = %q, want %q", got.Type, "milestone")
		}
	})

	t.Run("change epic to task rejects because existing feature children cannot have task parent", func(t *testing.T) {
		epic := &nib.Nib{ID: "epic-tc2", Title: "Epic", Type: "epic", Status: "todo"}
		feat := &nib.Nib{ID: "feat-tc2", Title: "Feature", Type: "feature", Status: "todo", Parent: "epic-tc2"}
		mustCreate(t, core, epic)
		mustCreate(t, core, feat)

		newType := "task"
		input := model.UpdateNibInput{Type: &newType}
		_, err := resolver.Mutation().UpdateNib(ctx, "epic-tc2", input)
		if err == nil {
			t.Fatal("expected error when changing epic to task with feature children, got nil")
		}
		if !strings.Contains(err.Error(), "invalidate child") {
			t.Errorf("expected child validation error, got: %v", err)
		}
	})

	t.Run("change epic to milestone succeeds because all child types are valid under milestone", func(t *testing.T) {
		epic := &nib.Nib{ID: "epic-tc3", Title: "Epic", Type: "epic", Status: "todo"}
		task := &nib.Nib{ID: "task-tc3", Title: "Task", Type: "task", Status: "todo", Parent: "epic-tc3"}
		mustCreate(t, core, epic)
		mustCreate(t, core, task)

		newType := "milestone"
		input := model.UpdateNibInput{Type: &newType}
		got, err := resolver.Mutation().UpdateNib(ctx, "epic-tc3", input)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if got.Type != "milestone" {
			t.Errorf("type = %q, want %q", got.Type, "milestone")
		}
	})

	t.Run("rejected type change does not corrupt in-memory state", func(t *testing.T) {
		epic := &nib.Nib{ID: "epic-tc4", Title: "Epic", Type: "epic", Status: "todo"}
		feat := &nib.Nib{ID: "feat-tc4", Title: "Feature", Type: "feature", Status: "todo", Parent: "epic-tc4"}
		mustCreate(t, core, epic)
		mustCreate(t, core, feat)

		// Try to change epic to task — should fail (feature children can't have task parent)
		newType := "task"
		input := model.UpdateNibInput{Type: &newType}
		_, err := resolver.Mutation().UpdateNib(ctx, "epic-tc4", input)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		// Verify the in-memory nib still has the original type
		stored, err := resolver.Query().Nib(ctx, "epic-tc4")
		if err != nil {
			t.Fatalf("failed to re-read nib: %v", err)
		}
		if stored.Type != "epic" {
			t.Errorf("in-memory type corrupted: got %q, want %q", stored.Type, "epic")
		}
	})

	t.Run("simultaneous type and parent change validates correctly", func(t *testing.T) {
		ms := &nib.Nib{ID: "ms-tc-sim", Title: "Milestone", Type: "milestone", Status: "todo"}
		feat := &nib.Nib{ID: "feat-tc-sim", Title: "Feature", Type: "feature", Status: "todo"}
		task := &nib.Nib{ID: "task-tc-sim", Title: "Task", Type: "task", Status: "todo", Parent: "feat-tc-sim"}
		mustCreate(t, core, ms)
		mustCreate(t, core, feat)
		mustCreate(t, core, task)

		// Change task to epic AND move parent from feature to milestone — should succeed
		newType := "epic"
		newParent := "ms-tc-sim"
		input := model.UpdateNibInput{Type: &newType, Parent: graphql.OmittableOf(&newParent)}
		got, err := resolver.Mutation().UpdateNib(ctx, "task-tc-sim", input)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if got.Type != "epic" {
			t.Errorf("type = %q, want %q", got.Type, "epic")
		}
		if got.Parent != "ms-tc-sim" {
			t.Errorf("parent = %q, want %q", got.Parent, "ms-tc-sim")
		}
	})

	t.Run("failed parent change with type change does not corrupt in-memory type", func(t *testing.T) {
		task := &nib.Nib{ID: "task-tc-fp", Title: "Task", Type: "task", Status: "todo"}
		mustCreate(t, core, task)

		// Change task to epic AND set parent to nonexistent nib — parent validation should fail
		newType := "epic"
		newParent := "nonexistent-nib"
		input := model.UpdateNibInput{Type: &newType, Parent: graphql.OmittableOf(&newParent)}
		_, err := resolver.Mutation().UpdateNib(ctx, "task-tc-fp", input)
		if err == nil {
			t.Fatal("expected error for nonexistent parent, got nil")
		}

		// Verify the in-memory nib still has the original type
		stored, err := resolver.Query().Nib(ctx, "task-tc-fp")
		if err != nil {
			t.Fatalf("failed to re-read nib: %v", err)
		}
		if stored.Type != "task" {
			t.Errorf("in-memory type corrupted: got %q, want %q", stored.Type, "task")
		}
	})
}

func TestReparentRecalculatesOrderKey(t *testing.T) {
	resolver, core := setupTestResolver(t)
	ctx := context.Background()

	// Create two epics (parent groups)
	epicA := &nib.Nib{ID: "epic-a", Title: "Epic A", Status: "todo", Type: "epic", Order: "a0"}
	epicB := &nib.Nib{ID: "epic-b", Title: "Epic B", Status: "todo", Type: "epic", Order: "a0"}
	if err := core.Create(epicA); err != nil {
		t.Fatalf("failed to create epicA: %v", err)
	}
	if err := core.Create(epicB); err != nil {
		t.Fatalf("failed to create epicB: %v", err)
	}

	// Create children under epic-a with known order keys
	childA1 := &nib.Nib{ID: "child-a1", Title: "Child A1", Status: "todo", Type: "task", Parent: "epic-a", Order: "a0"}
	childA2 := &nib.Nib{ID: "child-a2", Title: "Child A2", Status: "todo", Type: "task", Parent: "epic-a", Order: "b0"}
	if err := core.Create(childA1); err != nil {
		t.Fatalf("failed to create childA1: %v", err)
	}
	if err := core.Create(childA2); err != nil {
		t.Fatalf("failed to create childA2: %v", err)
	}

	// Create a child under epic-b that happens to have the same order key as child-a1
	childB1 := &nib.Nib{ID: "child-b1", Title: "Child B1", Status: "todo", Type: "task", Parent: "epic-b", Order: "a0"}
	if err := core.Create(childB1); err != nil {
		t.Fatalf("failed to create childB1: %v", err)
	}

	t.Run("SetParent recalculates order to avoid collision", func(t *testing.T) {
		mr := resolver.Mutation()
		// Move child-b1 (order "a0") to epic-a where child-a1 already has order "a0"
		parentID := "epic-a"
		got, err := mr.SetParent(ctx, "child-b1", &parentID, nil)
		if err != nil {
			t.Fatalf("SetParent() error = %v", err)
		}

		// The order key must be unique among siblings
		for _, sibID := range []string{"child-a1", "child-a2"} {
			sib, err := core.Get(sibID)
			if err != nil {
				t.Fatalf("core.Get(%q) error = %v", sibID, err)
			}
			if got.Order == sib.Order {
				t.Errorf("SetParent() gave order %q which collides with sibling %s (order %q)",
					got.Order, sibID, sib.Order)
			}
		}
		if got.Order == "" {
			t.Error("SetParent() resulted in empty order key")
		}
	})

	t.Run("UpdateNib parent change recalculates order to avoid collision", func(t *testing.T) {
		// Reset: move child-b1 back to epic-b
		parentB := "epic-b"
		if _, err := resolver.Mutation().SetParent(ctx, "child-b1", &parentB, nil); err != nil {
			t.Fatalf("reset SetParent() error = %v", err)
		}

		// Now move via UpdateNib
		mr := resolver.Mutation()
		parentID := "epic-a"
		input := model.UpdateNibInput{Parent: graphql.OmittableOf(&parentID)}
		got, err := mr.UpdateNib(ctx, "child-b1", input)
		if err != nil {
			t.Fatalf("UpdateNib() error = %v", err)
		}

		// The order key must be unique among siblings
		for _, sibID := range []string{"child-a1", "child-a2"} {
			sib, err := core.Get(sibID)
			if err != nil {
				t.Fatalf("core.Get(%q) error = %v", sibID, err)
			}
			if got.Order == sib.Order {
				t.Errorf("UpdateNib() gave order %q which collides with sibling %s (order %q)",
					got.Order, sibID, sib.Order)
			}
		}
		if got.Order == "" {
			t.Error("UpdateNib() resulted in empty order key")
		}
	})

	t.Run("reparent to empty group gets initial order", func(t *testing.T) {
		// Create a new empty epic
		epicC := &nib.Nib{ID: "epic-c", Title: "Epic C", Status: "todo", Type: "epic", Order: "c0"}
		if err := core.Create(epicC); err != nil {
			t.Fatalf("failed to create epicC: %v", err)
		}

		mr := resolver.Mutation()
		parentID := "epic-c"
		got, err := mr.SetParent(ctx, "child-a2", &parentID, nil)
		if err != nil {
			t.Fatalf("SetParent() error = %v", err)
		}
		if got.Order == "" {
			t.Error("SetParent() to empty group resulted in empty order key")
		}
		if got.Order != "a0" {
			t.Errorf("SetParent() to empty group: got order %q, want initial order %q", got.Order, "a0")
		}
	})

	t.Run("clear parent recalculates order for root level", func(t *testing.T) {
		mr := resolver.Mutation()
		got, err := mr.SetParent(ctx, "child-a1", nil, nil)
		if err != nil {
			t.Fatalf("SetParent() error = %v", err)
		}

		// Should have an order key that doesn't collide with existing root nibs
		for _, rootID := range []string{"epic-a", "epic-b", "epic-c"} {
			root, err := core.Get(rootID)
			if err != nil {
				t.Fatalf("core.Get(%q) error = %v", rootID, err)
			}
			if got.Order == root.Order {
				t.Errorf("SetParent(nil) gave order %q which collides with root nib %s (order %q)",
					got.Order, rootID, root.Order)
			}
		}
		if got.Order == "" {
			t.Error("SetParent(nil) resulted in empty order key")
		}
	})
}

func setupTestResolverWithAutoActivation(t *testing.T) (*Resolver, *nibcore.Core) {
	t.Helper()
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatalf("failed to create test .nibs dir: %v", err)
	}

	cfg := config.Default()
	cfg.Nibs.AutoActivation = true
	core := nibcore.New(nibsDir, cfg)
	if err := core.Load(); err != nil {
		t.Fatalf("failed to load core: %v", err)
	}

	return &Resolver{Reader: core, Writer: core, Validator: core, Blocking: core, Orderer: NewOrderer(core, core)}, core
}

func TestAutoActivationPropagatesInProgress(t *testing.T) {
	resolver, core := setupTestResolverWithAutoActivation(t)
	ctx := context.Background()

	// Create parent epic (todo) and child task (todo)
	parent := createTestNib(t, core, "epic-1", "Parent Epic", "todo")
	parent.Type = "epic"
	if err := core.Update(parent, nil); err != nil {
		t.Fatalf("setup: %v", err)
	}

	child := createTestNib(t, core, "task-1", "Child Task", "todo")
	child.Type = "task"
	child.Parent = "epic-1"
	if err := core.Update(child, nil); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Set child to in-progress
	inProgress := "in-progress"
	input := model.UpdateNibInput{Status: &inProgress}
	_, err := resolver.Mutation().UpdateNib(ctx, "task-1", input)
	if err != nil {
		t.Fatalf("UpdateNib failed: %v", err)
	}

	// Parent should now be in-progress too
	updatedParent, err := resolver.Query().Nib(ctx, "epic-1")
	if err != nil {
		t.Fatalf("Query parent failed: %v", err)
	}
	if updatedParent.Status != "in-progress" {
		t.Errorf("expected parent status 'in-progress', got %q", updatedParent.Status)
	}
}

// TestAutoActivationSkipsDeferredParent locks in that a deferred parent stays
// parked: activateParentChain only promotes todo/draft parents, so a child
// going in-progress must not un-park a deferred ancestor.
func TestAutoActivationSkipsDeferredParent(t *testing.T) {
	resolver, core := setupTestResolverWithAutoActivation(t)
	ctx := context.Background()

	// Parent epic is deferred (parked); child task is todo.
	parent := createTestNib(t, core, "epic-def", "Deferred Epic", "deferred")
	parent.Type = "epic"
	if err := core.Update(parent, nil); err != nil {
		t.Fatalf("setup: %v", err)
	}

	child := createTestNib(t, core, "task-def", "Child Task", "todo")
	child.Type = "task"
	child.Parent = "epic-def"
	if err := core.Update(child, nil); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Set child to in-progress.
	inProgress := "in-progress"
	input := model.UpdateNibInput{Status: &inProgress}
	if _, err := resolver.Mutation().UpdateNib(ctx, "task-def", input); err != nil {
		t.Fatalf("UpdateNib failed: %v", err)
	}

	// Parent must remain deferred (parked), not auto-activated.
	updatedParent, err := resolver.Query().Nib(ctx, "epic-def")
	if err != nil {
		t.Fatalf("Query parent failed: %v", err)
	}
	if updatedParent.Status != "deferred" {
		t.Errorf("expected parent status 'deferred' (parked), got %q", updatedParent.Status)
	}
}

func TestAutoActivationDisabledByDefault(t *testing.T) {
	resolver, core := setupTestResolver(t) // default config, no auto_activation
	ctx := context.Background()

	parent := createTestNib(t, core, "epic-2", "Parent Epic", "todo")
	parent.Type = "epic"
	if err := core.Update(parent, nil); err != nil {
		t.Fatalf("setup: %v", err)
	}

	child := createTestNib(t, core, "task-2", "Child Task", "todo")
	child.Type = "task"
	child.Parent = "epic-2"
	if err := core.Update(child, nil); err != nil {
		t.Fatalf("setup: %v", err)
	}

	inProgress := "in-progress"
	input := model.UpdateNibInput{Status: &inProgress}
	_, err := resolver.Mutation().UpdateNib(ctx, "task-2", input)
	if err != nil {
		t.Fatalf("UpdateNib failed: %v", err)
	}

	// Parent should remain todo (auto_activation not enabled)
	updatedParent, err := resolver.Query().Nib(ctx, "epic-2")
	if err != nil {
		t.Fatalf("Query parent failed: %v", err)
	}
	if updatedParent.Status != "todo" {
		t.Errorf("expected parent status 'todo' (auto_activation disabled), got %q", updatedParent.Status)
	}
}

func TestAutoActivationRecursesUpChain(t *testing.T) {
	resolver, core := setupTestResolverWithAutoActivation(t)
	ctx := context.Background()

	// milestone (draft) → epic (todo) → task (todo)
	ms := createTestNib(t, core, "ms-1", "Milestone", "draft")
	ms.Type = "milestone"
	if err := core.Update(ms, nil); err != nil {
		t.Fatalf("setup: %v", err)
	}

	epic := createTestNib(t, core, "epic-3", "Epic", "todo")
	epic.Type = "epic"
	epic.Parent = "ms-1"
	if err := core.Update(epic, nil); err != nil {
		t.Fatalf("setup: %v", err)
	}

	task := createTestNib(t, core, "task-3", "Task", "todo")
	task.Type = "task"
	task.Parent = "epic-3"
	if err := core.Update(task, nil); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Set task to in-progress
	inProgress := "in-progress"
	input := model.UpdateNibInput{Status: &inProgress}
	_, err := resolver.Mutation().UpdateNib(ctx, "task-3", input)
	if err != nil {
		t.Fatalf("UpdateNib failed: %v", err)
	}

	// Both epic and milestone should now be in-progress
	updatedEpic, err := resolver.Query().Nib(ctx, "epic-3")
	if err != nil {
		t.Fatalf("Query epic failed: %v", err)
	}
	if updatedEpic.Status != "in-progress" {
		t.Errorf("expected epic status 'in-progress', got %q", updatedEpic.Status)
	}
	updatedMs, err := resolver.Query().Nib(ctx, "ms-1")
	if err != nil {
		t.Fatalf("Query milestone failed: %v", err)
	}
	if updatedMs.Status != "in-progress" {
		t.Errorf("expected milestone status 'in-progress', got %q", updatedMs.Status)
	}
}

func TestAutoActivationStopsAtAlreadyActiveParent(t *testing.T) {
	resolver, core := setupTestResolverWithAutoActivation(t)
	ctx := context.Background()

	// grandparent (draft) → milestone (in-progress) → epic (todo) → task (todo)
	// The grandparent proves recursion actually stops at the active milestone.
	gp := createTestNib(t, core, "gp-1", "Grandparent", "draft")
	gp.Type = "milestone"
	if err := core.Update(gp, nil); err != nil {
		t.Fatalf("setup: %v", err)
	}

	ms := createTestNib(t, core, "ms-2", "Milestone", "in-progress")
	ms.Type = "milestone"
	ms.Parent = "gp-1"
	if err := core.Update(ms, nil); err != nil {
		t.Fatalf("setup: %v", err)
	}

	epic := createTestNib(t, core, "epic-4", "Epic", "todo")
	epic.Type = "epic"
	epic.Parent = "ms-2"
	if err := core.Update(epic, nil); err != nil {
		t.Fatalf("setup: %v", err)
	}

	task := createTestNib(t, core, "task-4", "Task", "todo")
	task.Type = "task"
	task.Parent = "epic-4"
	if err := core.Update(task, nil); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Set task to in-progress
	inProgress := "in-progress"
	input := model.UpdateNibInput{Status: &inProgress}
	_, err := resolver.Mutation().UpdateNib(ctx, "task-4", input)
	if err != nil {
		t.Fatalf("UpdateNib failed: %v", err)
	}

	// Epic should be activated
	updatedEpic, err := resolver.Query().Nib(ctx, "epic-4")
	if err != nil {
		t.Fatalf("Query epic failed: %v", err)
	}
	if updatedEpic.Status != "in-progress" {
		t.Errorf("expected epic status 'in-progress', got %q", updatedEpic.Status)
	}
	// Milestone was already in-progress — recursion should stop here
	updatedMs, err := resolver.Query().Nib(ctx, "ms-2")
	if err != nil {
		t.Fatalf("Query milestone failed: %v", err)
	}
	if updatedMs.Status != "in-progress" {
		t.Errorf("expected milestone to remain 'in-progress', got %q", updatedMs.Status)
	}
	// Grandparent should NOT have been activated (proves recursion stopped)
	updatedGp, err := resolver.Query().Nib(ctx, "gp-1")
	if err != nil {
		t.Fatalf("Query grandparent failed: %v", err)
	}
	if updatedGp.Status != "draft" {
		t.Errorf("expected grandparent to remain 'draft' (recursion should stop at active milestone), got %q", updatedGp.Status)
	}
}

func TestAutoActivationWorksWithRequireIfMatch(t *testing.T) {
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatalf("failed to create test .nibs dir: %v", err)
	}

	cfg := config.Default()
	cfg.Nibs.AutoActivation = true
	cfg.Nibs.RequireIfMatch = true
	core := nibcore.New(nibsDir, cfg)
	if err := core.Load(); err != nil {
		t.Fatalf("failed to load core: %v", err)
	}

	resolver := &Resolver{Reader: core, Writer: core, Validator: core, Blocking: core, Orderer: NewOrderer(core, core)}
	ctx := context.Background()

	parent := createTestNib(t, core, "epic-5", "Parent Epic", "todo")
	parentETag := parent.ETag() // before mutation
	parent.Type = "epic"
	if err := core.Update(parent, &parentETag); err != nil {
		t.Fatalf("setup: update parent type: %v", err)
	}

	child := createTestNib(t, core, "task-5", "Child Task", "todo")
	childETag := child.ETag() // before mutation
	child.Type = "task"
	child.Parent = "epic-5"
	if err := core.Update(child, &childETag); err != nil {
		t.Fatalf("setup: update child parent: %v", err)
	}

	// Set child to in-progress (must pass etag since require_if_match is on)
	childETag = child.ETag()
	inProgress := "in-progress"
	input := model.UpdateNibInput{Status: &inProgress, IfMatch: &childETag}
	_, err := resolver.Mutation().UpdateNib(ctx, "task-5", input)
	if err != nil {
		t.Fatalf("UpdateNib failed: %v", err)
	}

	// Verify parent was activated on disk (not just in memory).
	// Read the file directly to confirm the disk state matches.
	parentPath := core.FullPath(parent)
	data, err := os.ReadFile(parentPath)
	if err != nil {
		t.Fatalf("read parent file: %v", err)
	}
	if !strings.Contains(string(data), "status: in-progress") {
		t.Errorf("expected parent status 'in-progress' on disk with require_if_match, got file:\n%s", string(data))
	}
}

// --- Phase 1: Bug nibs-j7ez — NormalizeID ok return value must be checked ---
// These tests verify that NormalizeID failures produce clear "nib not found" errors
// at each call site, rather than passing through bad IDs to downstream operations.

func TestNormalizeIDValidation(t *testing.T) {
	ctx := context.Background()

	t.Run("CreateNib with non-existent parent short ID returns nib not found", func(t *testing.T) {
		// Uses stub that returns ok=false for unknown IDs.
		// The error must come from NormalizeID check, not from ValidateParent.
		reader := &stubReader{nibs: map[string]*nib.Nib{}}
		validator := &stubValidator{}
		writer := &stubWriter{}
		resolver := &Resolver{
			Reader:    reader,
			Writer:    writer,
			Validator: validator,
			Blocking:  &stubBlockingChecker{},
			Orderer:   NewOrderer(reader, writer),
		}
		parent := "nonexistent-parent"
		_, err := resolver.Mutation().CreateNib(ctx, model.CreateNibInput{
			Title:  "Test",
			Parent: &parent,
		})
		if err == nil {
			t.Fatal("expected error for non-existent parent, got nil")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("error should mention 'not found', got: %v", err)
		}
		// ValidateParent should NOT have been called — the NormalizeID check
		// should short-circuit before reaching it
		// (validator.validateParentErr is nil, so if it was called, no error
		// would come from it — the error must come from the NormalizeID check)
	})

	t.Run("CreateNib with non-existent blocking target returns nib not found", func(t *testing.T) {
		reader := &stubReader{nibs: map[string]*nib.Nib{}}
		writer := &stubWriter{}
		resolver := &Resolver{
			Reader:    reader,
			Writer:    writer,
			Validator: &stubValidator{},
			Blocking:  &stubBlockingChecker{},
			Orderer:   NewOrderer(reader, writer),
		}
		_, err := resolver.Mutation().CreateNib(ctx, model.CreateNibInput{
			Title:    "Test",
			Blocking: []string{"nonexistent-target"},
		})
		if err == nil {
			t.Fatal("expected error for non-existent blocking target, got nil")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("error should mention 'not found', got: %v", err)
		}
	})

	t.Run("CreateNib with non-existent blockedBy returns nib not found", func(t *testing.T) {
		reader := &stubReader{nibs: map[string]*nib.Nib{}}
		writer := &stubWriter{}
		resolver := &Resolver{
			Reader:    reader,
			Writer:    writer,
			Validator: &stubValidator{},
			Blocking:  &stubBlockingChecker{},
			Orderer:   NewOrderer(reader, writer),
		}
		_, err := resolver.Mutation().CreateNib(ctx, model.CreateNibInput{
			Title:     "Test",
			BlockedBy: []string{"nonexistent-blocker"},
		})
		if err == nil {
			t.Fatal("expected error for non-existent blocked-by target, got nil")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("error should mention 'not found', got: %v", err)
		}
	})

	t.Run("validateAndSetParent with non-existent parent returns nib not found", func(t *testing.T) {
		reader := &stubReader{nibs: map[string]*nib.Nib{}}
		writer := &stubWriter{}
		resolver := &Resolver{
			Reader:    reader,
			Writer:    writer,
			Validator: &stubValidator{},
			Blocking:  &stubBlockingChecker{},
			Orderer:   NewOrderer(reader, writer),
		}
		b := &nib.Nib{ID: "test-nib", Title: "Test", Status: "todo", Type: "task"}
		err := resolver.validateAndSetParent(b, "nonexistent-parent")
		if err == nil {
			t.Fatal("expected error for non-existent parent, got nil")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("error should mention 'not found', got: %v", err)
		}
	})

	t.Run("validateAndAddBlocking with non-existent target returns nib not found", func(t *testing.T) {
		existingNib := &nib.Nib{ID: "src-nib", Title: "Source", Status: "todo", Type: "task"}
		reader := &stubReader{nibs: map[string]*nib.Nib{"src-nib": existingNib}}
		writer := &stubWriter{}
		resolver := &Resolver{
			Reader:    reader,
			Writer:    writer,
			Validator: &stubValidator{},
			Blocking:  &stubBlockingChecker{},
			Orderer:   NewOrderer(reader, writer),
		}
		err := resolver.validateAndAddBlocking(existingNib, []string{"nonexistent-target"})
		if err == nil {
			t.Fatal("expected error for non-existent blocking target, got nil")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("error should mention 'not found', got: %v", err)
		}
	})

	t.Run("validateAndAddBlockedBy with non-existent target returns nib not found", func(t *testing.T) {
		existingNib := &nib.Nib{ID: "src-nib2", Title: "Source", Status: "todo", Type: "task"}
		reader := &stubReader{nibs: map[string]*nib.Nib{"src-nib2": existingNib}}
		writer := &stubWriter{}
		resolver := &Resolver{
			Reader:    reader,
			Writer:    writer,
			Validator: &stubValidator{},
			Blocking:  &stubBlockingChecker{},
			Orderer:   NewOrderer(reader, writer),
		}
		err := resolver.validateAndAddBlockedBy(existingNib, []string{"nonexistent-blocker"})
		if err == nil {
			t.Fatal("expected error for non-existent blocked-by target, got nil")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("error should mention 'not found', got: %v", err)
		}
	})

	t.Run("AddBlocking resolver with non-existent target returns nib not found", func(t *testing.T) {
		existingNib := &nib.Nib{ID: "add-blocking-src", Title: "Source", Status: "todo", Type: "task"}
		reader := &stubReader{nibs: map[string]*nib.Nib{"add-blocking-src": existingNib}}
		writer := &stubWriter{}
		resolver := &Resolver{
			Reader:    reader,
			Writer:    writer,
			Validator: &stubValidator{},
			Blocking:  &stubBlockingChecker{},
			Orderer:   NewOrderer(reader, writer),
		}
		_, err := resolver.Mutation().AddBlocking(ctx, "add-blocking-src", "nonexistent-target")
		if err == nil {
			t.Fatal("expected error for non-existent target, got nil")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("error should mention 'not found', got: %v", err)
		}
	})

	t.Run("AddBlockedBy resolver with non-existent target returns nib not found", func(t *testing.T) {
		existingNib := &nib.Nib{ID: "add-blockedby-src", Title: "Source", Status: "todo", Type: "task"}
		reader := &stubReader{nibs: map[string]*nib.Nib{"add-blockedby-src": existingNib}}
		writer := &stubWriter{}
		resolver := &Resolver{
			Reader:    reader,
			Writer:    writer,
			Validator: &stubValidator{},
			Blocking:  &stubBlockingChecker{},
			Orderer:   NewOrderer(reader, writer),
		}
		_, err := resolver.Mutation().AddBlockedBy(ctx, "add-blockedby-src", "nonexistent-blocker", nil)
		if err == nil {
			t.Fatal("expected error for non-existent blocker, got nil")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("error should mention 'not found', got: %v", err)
		}
	})

	t.Run("positionAfter with non-existent target returns nib not found", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		epicNib := &nib.Nib{ID: "pos-epic", Title: "Epic", Status: "todo", Type: "epic"}
		mustCreate(t, core, epicNib)
		childNib := &nib.Nib{ID: "pos-child", Title: "Child", Status: "todo", Type: "task", Parent: "pos-epic"}
		mustCreate(t, core, childNib)

		afterID := "nonexistent-sibling"
		_, err := resolver.Mutation().CreateNib(ctx, model.CreateNibInput{
			Title:   "New Child",
			Parent:  strPtr("pos-epic"),
			AfterID: &afterID,
		})
		if err == nil {
			t.Fatal("expected error for non-existent afterId target, got nil")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("error should mention 'not found', got: %v", err)
		}
	})

	t.Run("positionBefore with non-existent target returns nib not found", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		epicNib2 := &nib.Nib{ID: "pos-epic-2", Title: "Epic 2", Status: "todo", Type: "epic"}
		mustCreate(t, core, epicNib2)
		childNib2 := &nib.Nib{ID: "pos-child-2", Title: "Child 2", Status: "todo", Type: "task", Parent: "pos-epic-2"}
		mustCreate(t, core, childNib2)

		beforeID := "nonexistent-sibling"
		_, err := resolver.Mutation().CreateNib(ctx, model.CreateNibInput{
			Title:    "New Child",
			Parent:   strPtr("pos-epic-2"),
			BeforeID: &beforeID,
		})
		if err == nil {
			t.Fatal("expected error for non-existent beforeId target, got nil")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("error should mention 'not found', got: %v", err)
		}
	})
}

func strPtr(s string) *string { return &s }

// --- Phase 2: Bug nibs-r9e1 — Clone-before-mutate in UpdateNib ---

func TestUpdateNibCloneBeforeMutate(t *testing.T) {
	ctx := context.Background()

	t.Run("field change + failed blocking validation leaves in-memory nib unchanged", func(t *testing.T) {
		resolver, core := setupTestResolver(t)

		// Create the nib to be updated
		original := &nib.Nib{
			ID:     "update-rollback-1",
			Title:  "Original Title",
			Status: "todo",
			Type:   "task",
			Body:   "Original body",
		}
		mustCreate(t, core, original)

		// Attempt to update title AND add a non-existent blocking target
		newTitle := "Updated Title"
		_, err := resolver.Mutation().UpdateNib(ctx, "update-rollback-1", model.UpdateNibInput{
			Title:       &newTitle,
			AddBlocking: []string{"nonexistent-target"},
		})
		if err == nil {
			t.Fatal("expected error for non-existent blocking target, got nil")
		}

		// Verify in-memory nib was NOT mutated
		stored, _ := core.Get("update-rollback-1")
		if stored.Title != "Original Title" {
			t.Errorf("in-memory nib title was mutated to %q, want %q", stored.Title, "Original Title")
		}
	})

	t.Run("type change + failed child validation leaves in-memory nib unchanged", func(t *testing.T) {
		resolver, core := setupTestResolver(t)

		// Create parent (epic) with a child (task)
		parent := &nib.Nib{ID: "type-parent", Title: "Parent", Status: "todo", Type: "epic"}
		mustCreate(t, core, parent)
		child := &nib.Nib{ID: "type-child", Title: "Child", Status: "todo", Type: "task", Parent: "type-parent"}
		mustCreate(t, core, child)

		// Change parent type to "task" — should fail because task can't have children
		newType := "task"
		_, err := resolver.Mutation().UpdateNib(ctx, "type-parent", model.UpdateNibInput{
			Type: &newType,
		})
		if err == nil {
			t.Fatal("expected error for invalid type change, got nil")
		}

		// Verify in-memory nib type was NOT mutated
		stored, _ := core.Get("type-parent")
		if stored.Type != "epic" {
			t.Errorf("in-memory nib type was mutated to %q, want %q", stored.Type, "epic")
		}
	})

	t.Run("body change + failed Writer.Update leaves in-memory nib unchanged", func(t *testing.T) {
		// Use a failingWriter that rejects Update calls
		existingNib := &nib.Nib{
			ID:     "body-rollback",
			Title:  "Original",
			Status: "todo",
			Type:   "task",
			Body:   "Original body",
		}
		reader := &stubReader{nibs: map[string]*nib.Nib{"body-rollback": existingNib}}
		writer := &failingWriter{failUpdate: true}
		resolver := &Resolver{
			Reader:    reader,
			Writer:    writer,
			Validator: &stubValidator{},
			Blocking:  &stubBlockingChecker{},
			Orderer:   NewOrderer(reader, writer),
		}

		newBody := "Updated body"
		_, err := resolver.Mutation().UpdateNib(ctx, "body-rollback", model.UpdateNibInput{
			Body: &newBody,
		})
		if err == nil {
			t.Fatal("expected error from Writer.Update, got nil")
		}

		// Verify in-memory nib was NOT mutated
		if existingNib.Body != "Original body" {
			t.Errorf("in-memory nib body was mutated to %q, want %q", existingNib.Body, "Original body")
		}
	})
}

// failingWriter is a stubWriter that can fail on specific operations.
type failingWriter struct {
	stubWriter
	failUpdate bool
	failCreate bool
}

func (f *failingWriter) Update(b *nib.Nib, ifMatch *string) error {
	if f.failUpdate {
		return fmt.Errorf("simulated update failure")
	}
	return f.stubWriter.Update(b, ifMatch)
}

func (f *failingWriter) Create(b *nib.Nib) error {
	if f.failCreate {
		return fmt.Errorf("simulated create failure")
	}
	return f.stubWriter.Create(b)
}

// --- Phase 3: Bug nibs-kzlw — Atomic multi-step mutations ---

func TestAtomicCreateNib(t *testing.T) {
	ctx := context.Background()

	t.Run("CreateNib with 2 blocking targets where target 2 does not exist fails before creating nib", func(t *testing.T) {
		// target1 exists, target2 does not
		target1 := &nib.Nib{ID: "target-1", Title: "Target 1", Status: "todo", Type: "task"}
		reader := &stubReader{
			nibs: map[string]*nib.Nib{"target-1": target1},
		}
		writer := &stubWriter{}
		resolver := &Resolver{
			Reader:    reader,
			Writer:    writer,
			Validator: &stubValidator{},
			Blocking:  &stubBlockingChecker{},
			Orderer:   NewOrderer(reader, writer),
		}

		_, err := resolver.Mutation().CreateNib(ctx, model.CreateNibInput{
			Title:    "New Nib",
			Blocking: []string{"target-1", "nonexistent-target"},
		})
		if err == nil {
			t.Fatal("expected error for non-existent blocking target, got nil")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("error should mention 'not found', got: %v", err)
		}
		// The nib should NOT have been created
		if len(writer.created) != 0 {
			t.Errorf("nib was created despite validation failure: %d created", len(writer.created))
		}
	})

	t.Run("CreateNib with valid blocking targets succeeds and all targets get blockedBy updated", func(t *testing.T) {
		resolver, core := setupTestResolver(t)

		target1 := &nib.Nib{ID: "atomic-target-1", Title: "Target 1", Status: "todo", Type: "task"}
		target2 := &nib.Nib{ID: "atomic-target-2", Title: "Target 2", Status: "todo", Type: "task"}
		mustCreate(t, core, target1)
		mustCreate(t, core, target2)

		got, err := resolver.Mutation().CreateNib(ctx, model.CreateNibInput{
			Title:    "Blocker",
			Blocking: []string{"atomic-target-1", "atomic-target-2"},
		})
		if err != nil {
			t.Fatalf("CreateNib() error = %v", err)
		}

		// Both targets should have the new nib in blockedBy
		t1, _ := core.Get("atomic-target-1")
		if !t1.IsBlockedBy(got.ID) {
			t.Errorf("target-1 should be blocked by %s, got blockedBy=%v", got.ID, t1.BlockedBy)
		}
		t2, _ := core.Get("atomic-target-2")
		if !t2.IsBlockedBy(got.ID) {
			t.Errorf("target-2 should be blocked by %s, got blockedBy=%v", got.ID, t2.BlockedBy)
		}
	})
}

func TestAtomicValidateAndAddBlocking(t *testing.T) {
	t.Run("pre-validates all targets before mutating any", func(t *testing.T) {
		// target1 exists, target2 does not
		// If validation is sequential (validate+mutate one at a time),
		// target1 would get mutated before target2 fails.
		// With two-phase, no targets should be mutated.
		target1 := &nib.Nib{ID: "two-phase-t1", Title: "T1", Status: "todo", Type: "task"}
		reader := &stubReader{
			nibs: map[string]*nib.Nib{
				"two-phase-t1": target1,
				"source-nib":   {ID: "source-nib", Title: "Source", Status: "todo", Type: "task"},
			},
		}
		writer := &stubWriter{}
		resolver := &Resolver{
			Reader:    reader,
			Writer:    writer,
			Validator: &stubValidator{},
			Blocking:  &stubBlockingChecker{},
			Orderer:   NewOrderer(reader, writer),
		}

		b, _ := reader.Get("source-nib")
		err := resolver.validateAndAddBlocking(b, []string{"two-phase-t1", "nonexistent-target"})
		if err == nil {
			t.Fatal("expected error for non-existent target, got nil")
		}

		// target1 should NOT have been mutated (no update calls at all)
		if len(writer.updated) != 0 {
			t.Errorf("targets were mutated despite validation failure: %d updates", len(writer.updated))
		}
		// target1's blockedBy should still be empty
		if target1.IsBlockedBy("source-nib") {
			t.Errorf("target1 was mutated despite validation failure: blockedBy=%v", target1.BlockedBy)
		}
	})
}

