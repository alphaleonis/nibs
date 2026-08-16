package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
)

func TestFilterClosedBlockersDoesNotMutateCore(t *testing.T) {
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

	// Create a blocker nib that is completed (closed)
	blocker := &nib.Nib{ID: "blocker-1", Slug: "blocker", Title: "Blocker", Status: "completed"}
	if err := core.Create(blocker); err != nil {
		t.Fatal(err)
	}

	// Create a nib blocked by the closed blocker
	blocked := &nib.Nib{
		ID:        "blocked-1",
		Slug:      "blocked",
		Title:     "Blocked",
		Status:    "todo",
		BlockedBy: []string{"blocker-1"},
	}
	if err := core.Create(blocked); err != nil {
		t.Fatal(err)
	}

	// Call filterReleasedBlockersOne — this should NOT mutate the in-memory nib
	result := filterReleasedBlockersOne(blocked, core)

	// The returned copy should have the closed blocker removed
	if len(result.BlockedBy) != 0 {
		t.Errorf("returned nib should have empty BlockedBy, got %v", result.BlockedBy)
	}

	// Re-fetch the nib from Core — its BlockedBy should still contain the blocker
	stored, err := core.Get("blocked-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.BlockedBy) != 1 || stored.BlockedBy[0] != "blocker-1" {
		t.Errorf("Core nib's BlockedBy was mutated: got %v, want [blocker-1]", stored.BlockedBy)
	}
}
