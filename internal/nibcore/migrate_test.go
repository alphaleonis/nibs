package nibcore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
)

// writeNibFile writes a raw nib markdown file to disk for testing migration.
func writeNibFile(t *testing.T, dir, filename, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write nib file %s: %v", filename, err)
	}
}

func TestCheckBrokenDocuments(t *testing.T) {
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, NibsDir)
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a valid document
	if err := os.WriteFile(filepath.Join(tmpDir, "existing.md"), []byte("# Exists"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	core := New(nibsDir, cfg)
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Create nib with one valid and one broken document link
	b := &nib.Nib{
		ID:        "doc-check-1",
		Title:     "Doc Check",
		Status:    "todo",
		Version:   1,
		Documents: []string{"existing.md", "nonexistent.md"},
	}
	if err := core.Create(b); err != nil {
		t.Fatal(err)
	}

	t.Run("detects broken document links", func(t *testing.T) {
		result := core.CheckAllLinks()
		if len(result.BrokenDocuments) != 1 {
			t.Fatalf("BrokenDocuments count = %d, want 1", len(result.BrokenDocuments))
		}
		if result.BrokenDocuments[0].NibID != "doc-check-1" {
			t.Errorf("BrokenDocuments[0].NibID = %q, want doc-check-1", result.BrokenDocuments[0].NibID)
		}
		if result.BrokenDocuments[0].Path != "nonexistent.md" {
			t.Errorf("BrokenDocuments[0].Path = %q, want nonexistent.md", result.BrokenDocuments[0].Path)
		}
	})

	t.Run("fix removes broken document links", func(t *testing.T) {
		fixed, err := core.FixBrokenLinks()
		if err != nil {
			t.Fatalf("FixBrokenLinks() error: %v", err)
		}
		if fixed != 1 {
			t.Errorf("fixed = %d, want 1", fixed)
		}
		updated, _ := core.Get("doc-check-1")
		if len(updated.Documents) != 1 {
			t.Errorf("Documents count = %d, want 1", len(updated.Documents))
		}
		if len(updated.Documents) > 0 && updated.Documents[0] != "existing.md" {
			t.Errorf("Documents = %v, want [existing.md]", updated.Documents)
		}
	})
}

func TestMigrateV0ToV1(t *testing.T) {
	t.Run("converts blocking to blockedBy on targets", func(t *testing.T) {
		tmpDir := t.TempDir()
		nibsDir := filepath.Join(tmpDir, NibsDir)
		if err := os.MkdirAll(nibsDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Write v0 nib A that blocks B
		writeNibFile(t, nibsDir, "aaa1--blocker.md", `---
title: Blocker
status: todo
blocking:
    - bbb2
---
`)
		// Write v0 nib B (no blocking)
		writeNibFile(t, nibsDir, "bbb2--blocked.md", `---
title: Blocked
status: todo
---
`)

		cfg := config.Default()
		core := New(nibsDir, cfg)
		core.SetWarnWriter(nil)
		if err := core.Load(); err != nil {
			t.Fatalf("Load() error: %v", err)
		}

		// After migration:
		// - A should have version 1, no blocking field
		// - B should have version 1, blockedBy=[aaa1]
		a, _ := core.Get("aaa1")
		b, _ := core.Get("bbb2")

		if a.Version != 1 {
			t.Errorf("A.Version = %d, want 1", a.Version)
		}
		if len(a.Blocking) != 0 {
			t.Errorf("A.Blocking should be cleared after migration, got %v", a.Blocking)
		}
		if b.Version != 1 {
			t.Errorf("B.Version = %d, want 1", b.Version)
		}
		if !b.IsBlockedBy("aaa1") {
			t.Errorf("B.BlockedBy should contain aaa1, got %v", b.BlockedBy)
		}

		// Verify persisted to disk
		core2 := New(nibsDir, cfg)
		core2.SetWarnWriter(nil)
		if err := core2.Load(); err != nil {
			t.Fatalf("second Load() error: %v", err)
		}
		b2, _ := core2.Get("bbb2")
		if !b2.IsBlockedBy("aaa1") {
			t.Errorf("B.BlockedBy not persisted, got %v", b2.BlockedBy)
		}
		if b2.Version != 1 {
			t.Errorf("B.Version not persisted, got %d", b2.Version)
		}
	})

	t.Run("handles both blocking and blockedBy present", func(t *testing.T) {
		tmpDir := t.TempDir()
		nibsDir := filepath.Join(tmpDir, NibsDir)
		if err := os.MkdirAll(nibsDir, 0755); err != nil {
			t.Fatal(err)
		}

		// A blocks B, and B already has blockedBy=[A] (dual-side legacy)
		writeNibFile(t, nibsDir, "aaa1.md", `---
title: Blocker
status: todo
blocking:
    - bbb2
---
`)
		writeNibFile(t, nibsDir, "bbb2.md", `---
title: Blocked
status: todo
blocked_by:
    - aaa1
---
`)

		cfg := config.Default()
		core := New(nibsDir, cfg)
		core.SetWarnWriter(nil)
		if err := core.Load(); err != nil {
			t.Fatalf("Load() error: %v", err)
		}

		b, _ := core.Get("bbb2")
		// Should deduplicate: aaa1 appears only once
		count := 0
		for _, id := range b.BlockedBy {
			if id == "aaa1" {
				count++
			}
		}
		if count != 1 {
			t.Errorf("B.BlockedBy should have aaa1 exactly once, got %v", b.BlockedBy)
		}
	})

	t.Run("handles blocking references nonexistent nib", func(t *testing.T) {
		tmpDir := t.TempDir()
		nibsDir := filepath.Join(tmpDir, NibsDir)
		if err := os.MkdirAll(nibsDir, 0755); err != nil {
			t.Fatal(err)
		}

		var warnings strings.Builder
		writeNibFile(t, nibsDir, "aaa1.md", `---
title: Blocker
status: todo
blocking:
    - nonexistent
---
`)

		cfg := config.Default()
		core := New(nibsDir, cfg)
		core.SetWarnWriter(&warnings)
		if err := core.Load(); err != nil {
			t.Fatalf("Load() error: %v", err)
		}

		a, _ := core.Get("aaa1")
		if a.Version != 1 {
			t.Errorf("A.Version = %d, want 1", a.Version)
		}
		if len(a.Blocking) != 0 {
			t.Errorf("A.Blocking should be cleared, got %v", a.Blocking)
		}
		if !strings.Contains(warnings.String(), "nonexistent") {
			t.Errorf("expected warning about nonexistent nib, got %q", warnings.String())
		}
	})

	t.Run("bumps version on nibs with no blocking", func(t *testing.T) {
		tmpDir := t.TempDir()
		nibsDir := filepath.Join(tmpDir, NibsDir)
		if err := os.MkdirAll(nibsDir, 0755); err != nil {
			t.Fatal(err)
		}

		writeNibFile(t, nibsDir, "aaa1.md", `---
title: Simple Nib
status: todo
---
`)

		cfg := config.Default()
		core := New(nibsDir, cfg)
		core.SetWarnWriter(nil)
		if err := core.Load(); err != nil {
			t.Fatalf("Load() error: %v", err)
		}

		a, _ := core.Get("aaa1")
		if a.Version != 1 {
			t.Errorf("Version = %d, want 1", a.Version)
		}
	})

	t.Run("idempotent: v1 nibs not re-migrated", func(t *testing.T) {
		tmpDir := t.TempDir()
		nibsDir := filepath.Join(tmpDir, NibsDir)
		if err := os.MkdirAll(nibsDir, 0755); err != nil {
			t.Fatal(err)
		}

		writeNibFile(t, nibsDir, "aaa1.md", `---
version: 1
title: Already Migrated
status: todo
blocked_by:
    - bbb2
---
`)
		writeNibFile(t, nibsDir, "bbb2.md", `---
version: 1
title: Other Nib
status: todo
---
`)

		cfg := config.Default()
		core := New(nibsDir, cfg)
		core.SetWarnWriter(nil)
		if err := core.Load(); err != nil {
			t.Fatalf("Load() error: %v", err)
		}

		a, _ := core.Get("aaa1")
		if a.Version != 1 {
			t.Errorf("Version = %d, want 1", a.Version)
		}
		if len(a.BlockedBy) != 1 || a.BlockedBy[0] != "bbb2" {
			t.Errorf("BlockedBy = %v, want [bbb2]", a.BlockedBy)
		}
	})

	t.Run("mixed v0 and v1 directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		nibsDir := filepath.Join(tmpDir, NibsDir)
		if err := os.MkdirAll(nibsDir, 0755); err != nil {
			t.Fatal(err)
		}

		// v0 nib that blocks a v1 nib
		writeNibFile(t, nibsDir, "v0nib.md", `---
title: V0 Nib
status: todo
blocking:
    - v1nib
---
`)
		// v1 nib (already migrated)
		writeNibFile(t, nibsDir, "v1nib.md", `---
version: 1
title: V1 Nib
status: todo
---
`)

		cfg := config.Default()
		core := New(nibsDir, cfg)
		core.SetWarnWriter(nil)
		if err := core.Load(); err != nil {
			t.Fatalf("Load() error: %v", err)
		}

		// All nibs should be v1 after load
		v0, _ := core.Get("v0nib")
		v1, _ := core.Get("v1nib")

		if v0.Version != 1 {
			t.Errorf("v0nib.Version = %d, want 1", v0.Version)
		}
		if v1.Version != 1 {
			t.Errorf("v1nib.Version = %d, want 1", v1.Version)
		}
		// v1nib should have v0nib in its blockedBy (from migration)
		if !v1.IsBlockedBy("v0nib") {
			t.Errorf("v1nib.BlockedBy should contain v0nib, got %v", v1.BlockedBy)
		}
	})
}
