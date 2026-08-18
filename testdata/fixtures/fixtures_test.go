package fixtures

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopySampleProject(t *testing.T) {
	root := CopySampleProject(t)

	// The config lives inside the store
	configPath := filepath.Join(NibsPath(root), "config.yml")
	if _, err := os.Stat(configPath); err != nil {
		t.Errorf("expected config.yml at %s: %v", configPath, err)
	}

	// data/ should exist with nib files
	dataDir := DataPath(root)
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		t.Fatalf("reading data directory: %v", err)
	}

	var mdCount int
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".md" {
			mdCount++
		}
	}

	if mdCount < 80 {
		t.Errorf("expected at least 80 nib files, got %d", mdCount)
	}

	// Should be a separate copy (modifying shouldn't affect original)
	testFile := filepath.Join(dataDir, "test-write.md")
	if err := os.WriteFile(testFile, []byte("test"), 0o644); err != nil {
		t.Fatalf("writing to temp copy: %v", err)
	}
	// Verify original doesn't have this file
	origPath := filepath.Join("sample-project", ".nibs", "data", "test-write.md")
	if _, err := os.Stat(origPath); err == nil {
		t.Error("write to temp copy affected the original fixture")
	}
}
