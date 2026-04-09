package fixtures

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopySampleProject(t *testing.T) {
	root := CopySampleProject(t)

	// Config file should exist
	configPath := filepath.Join(root, ".nibs.yml")
	if _, err := os.Stat(configPath); err != nil {
		t.Errorf("expected .nibs.yml at %s: %v", configPath, err)
	}

	// .nibs directory should exist with nib files
	nibsDir := NibsPath(root)
	entries, err := os.ReadDir(nibsDir)
	if err != nil {
		t.Fatalf("reading .nibs directory: %v", err)
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
	testFile := filepath.Join(nibsDir, "test-write.md")
	if err := os.WriteFile(testFile, []byte("test"), 0o644); err != nil {
		t.Fatalf("writing to temp copy: %v", err)
	}
	// Verify original doesn't have this file
	origPath := filepath.Join("sample-project", ".nibs", "test-write.md")
	if _, err := os.Stat(origPath); err == nil {
		t.Error("write to temp copy affected the original fixture")
	}
}
