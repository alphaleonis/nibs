package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
)

func TestResolveNibsPath(t *testing.T) {
	// Create a valid nibs directory for tests that need one
	tmpDir := t.TempDir()
	validNibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(validNibsDir, 0755); err != nil {
		t.Fatalf("failed to create test .nibs dir: %v", err)
	}

	altNibsDir := filepath.Join(tmpDir, "alt-nibs")
	if err := os.MkdirAll(altNibsDir, 0755); err != nil {
		t.Fatalf("failed to create alt nibs dir: %v", err)
	}

	// Config that points to the valid nibs dir
	cfg := config.Default()
	cfg.SetConfigDir(tmpDir)

	t.Run("flag takes highest precedence", func(t *testing.T) {
		t.Setenv("NIBS_PATH", altNibsDir)

		got, err := resolveNibsPath(validNibsDir, cfg)
		if err != nil {
			t.Fatalf("resolveNibsPath() error = %v", err)
		}
		if got != validNibsDir {
			t.Errorf("expected flag path %q, got %q", validNibsDir, got)
		}
	})

	t.Run("flag overrides env var", func(t *testing.T) {
		t.Setenv("NIBS_PATH", "/nonexistent/should/not/be/used")

		got, err := resolveNibsPath(validNibsDir, cfg)
		if err != nil {
			t.Fatalf("resolveNibsPath() error = %v", err)
		}
		if got != validNibsDir {
			t.Errorf("expected flag path %q, got %q", validNibsDir, got)
		}
	})

	t.Run("env var used when flag is empty", func(t *testing.T) {
		t.Setenv("NIBS_PATH", altNibsDir)

		got, err := resolveNibsPath("", cfg)
		if err != nil {
			t.Fatalf("resolveNibsPath() error = %v", err)
		}
		if got != altNibsDir {
			t.Errorf("expected env var path %q, got %q", altNibsDir, got)
		}
	})

	t.Run("config used when flag and env var are empty", func(t *testing.T) {
		t.Setenv("NIBS_PATH", "")

		got, err := resolveNibsPath("", cfg)
		if err != nil {
			t.Fatalf("resolveNibsPath() error = %v", err)
		}
		expected := cfg.ResolveNibsPath()
		if got != expected {
			t.Errorf("expected config path %q, got %q", expected, got)
		}
	})

	t.Run("invalid flag path returns error", func(t *testing.T) {
		_, err := resolveNibsPath("/nonexistent/path", cfg)
		if err == nil {
			t.Fatal("expected error for invalid flag path, got nil")
		}
		if !strings.Contains(err.Error(), "does not exist or is not a directory") {
			t.Errorf("expected 'does not exist' error, got %q", err.Error())
		}
	})

	t.Run("invalid env var path returns error", func(t *testing.T) {
		t.Setenv("NIBS_PATH", "/nonexistent/env/path")

		_, err := resolveNibsPath("", cfg)
		if err == nil {
			t.Fatal("expected error for invalid env var path, got nil")
		}
		if !strings.Contains(err.Error(), "does not exist or is not a directory") {
			t.Errorf("expected 'does not exist' error, got %q", err.Error())
		}
	})

	t.Run("invalid config path returns init suggestion", func(t *testing.T) {
		t.Setenv("NIBS_PATH", "")

		// Config pointing to a nonexistent directory
		badCfg := config.Default()
		badCfg.SetConfigDir("/nonexistent/config/dir")

		_, err := resolveNibsPath("", badCfg)
		if err == nil {
			t.Fatal("expected error for invalid config path, got nil")
		}
		if !strings.Contains(err.Error(), "nibs init") {
			t.Errorf("expected error to suggest 'nibs init', got %q", err.Error())
		}
	})

	t.Run("file path rejected as not a directory", func(t *testing.T) {
		// Create a regular file (not a directory)
		filePath := filepath.Join(tmpDir, "not-a-dir")
		if err := os.WriteFile(filePath, []byte("hello"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		_, err := resolveNibsPath(filePath, cfg)
		if err == nil {
			t.Fatal("expected error for file path (not directory), got nil")
		}
		if !strings.Contains(err.Error(), "does not exist or is not a directory") {
			t.Errorf("expected 'does not exist' error, got %q", err.Error())
		}
	})
}
