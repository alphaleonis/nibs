package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/output"
)

func TestResolveStoreDir(t *testing.T) {
	tmpDir := t.TempDir()
	projectDir := filepath.Join(tmpDir, "project")
	validStoreDir := filepath.Join(projectDir, ".nibs")
	if err := os.MkdirAll(validStoreDir, 0755); err != nil {
		t.Fatalf("failed to create test .nibs dir: %v", err)
	}

	altStoreDir := filepath.Join(tmpDir, "alt", ".nibs")
	if err := os.MkdirAll(altStoreDir, 0755); err != nil {
		t.Fatalf("failed to create alt store dir: %v", err)
	}

	t.Cleanup(resetRootPersistentFlags)

	t.Run("flag takes highest precedence", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		t.Setenv("NIBS_PATH", altStoreDir)
		nibsPath = validStoreDir

		got, err := resolveStoreDir()
		if err != nil {
			t.Fatalf("resolveStoreDir() error = %v", err)
		}
		if got != validStoreDir {
			t.Errorf("expected flag path %q, got %q", validStoreDir, got)
		}
	})

	t.Run("env var used when flag is empty", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		t.Setenv("NIBS_PATH", altStoreDir)

		got, err := resolveStoreDir()
		if err != nil {
			t.Fatalf("resolveStoreDir() error = %v", err)
		}
		if got != altStoreDir {
			t.Errorf("expected env var path %q, got %q", altStoreDir, got)
		}
	})

	t.Run("--config names the store through its directory", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		t.Setenv("NIBS_PATH", "")
		configPath = filepath.Join(altStoreDir, "config.yml")

		got, err := resolveStoreDir()
		if err != nil {
			t.Fatalf("resolveStoreDir() error = %v", err)
		}
		if got != altStoreDir {
			t.Errorf("expected --config's directory %q, got %q", altStoreDir, got)
		}
	})

	t.Run("upward walk finds the store from a subdirectory", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		t.Setenv("NIBS_PATH", "")
		t.Setenv("NIBS_CONFIG_ROOT", tmpDir)
		deep := filepath.Join(projectDir, "a", "b")
		if err := os.MkdirAll(deep, 0755); err != nil {
			t.Fatalf("mkdir deep: %v", err)
		}
		t.Chdir(deep)

		got, err := resolveStoreDir()
		if err != nil {
			t.Fatalf("resolveStoreDir() error = %v", err)
		}
		if got != validStoreDir {
			t.Errorf("expected discovered store %q, got %q", validStoreDir, got)
		}
	})

	t.Run("invalid flag path returns error", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		nibsPath = "/nonexistent/path"

		_, err := resolveStoreDir()
		if err == nil {
			t.Fatal("expected error for invalid flag path, got nil")
		}
		if !strings.Contains(err.Error(), "does not exist or is not a directory") {
			t.Errorf("expected 'does not exist' error, got %q", err.Error())
		}
	})

	t.Run("invalid env var path returns error", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		t.Setenv("NIBS_PATH", "/nonexistent/env/path")

		_, err := resolveStoreDir()
		if err == nil {
			t.Fatal("expected error for invalid env var path, got nil")
		}
		if !strings.Contains(err.Error(), "does not exist or is not a directory") {
			t.Errorf("expected 'does not exist' error, got %q", err.Error())
		}
	})

	t.Run("no store anywhere returns init suggestion", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		t.Setenv("NIBS_PATH", "")
		bare := t.TempDir()
		t.Setenv("NIBS_CONFIG_ROOT", bare)
		t.Chdir(bare)

		_, err := resolveStoreDir()
		if err == nil {
			t.Fatal("expected error when no store exists, got nil")
		}
		if !strings.Contains(err.Error(), "nibs init") {
			t.Errorf("expected error to suggest 'nibs init', got %q", err.Error())
		}
	})

	t.Run("file path rejected as not a directory", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		filePath := filepath.Join(tmpDir, "not-a-dir")
		if err := os.WriteFile(filePath, []byte("hello"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
		nibsPath = filePath

		_, err := resolveStoreDir()
		if err == nil {
			t.Fatal("expected error for file path (not directory), got nil")
		}
		if !strings.Contains(err.Error(), "does not exist or is not a directory") {
			t.Errorf("expected 'does not exist' error, got %q", err.Error())
		}
	})
}

// TestReportExitError pins the CLI error boundary's contract. It is the ONE
// place that decides the process exit status, mapping each error's structured
// CODE to a stable exit code via output.ExitCode:
//   - nil err → exit 0, no stderr
//   - plain (uncoded) err → exit 1, stderr "Error: <msg>\n"
//   - plain filesystem/IO err → exit 5 (ExitIO), stderr "Error: <msg>\n"
//   - reported CodedError (JSON envelope / get text line already on stdout) →
//     exit mapped from code, NO stderr (a redundant "Error:" line would
//     corrupt `2>&1 | jq` callers)
//   - non-reported CodedError (shared text path) → exit mapped from code,
//     stderr "Error: <msg>\n" (the boundary owns the print)
//
// The `err` field is a thunk so the test can construct the error inside
// captureStdout — output.Error writes a JSON envelope on construction,
// and we don't want that envelope leaking into the test's stdout.
func TestReportExitError(t *testing.T) {
	tests := []struct {
		name       string
		err        func() error
		wantCode   int
		wantStderr string
	}{
		{
			name:       "nil error returns 0 with empty stderr",
			err:        func() error { return nil },
			wantCode:   output.ExitOK,
			wantStderr: "",
		},
		{
			name:       "plain error returns generic exit with Error: prefix",
			err:        func() error { return errors.New("boom") },
			wantCode:   output.ExitError,
			wantStderr: "Error: boom\n",
		},
		{
			name:       "plain filesystem error maps to ExitIO",
			err:        func() error { return &fs.PathError{Op: "open", Path: "x", Err: fs.ErrNotExist} },
			wantCode:   output.ExitIO,
			wantStderr: "Error: open x: file does not exist\n",
		},
		{
			name:       "reported validation error maps to ExitValidation, no stderr",
			err:        func() error { return output.Error(output.ErrValidation, "bad") },
			wantCode:   output.ExitValidation,
			wantStderr: "",
		},
		{
			name: "wrapped reported not-found error maps to ExitNotFound, no stderr",
			err: func() error {
				return fmt.Errorf("context: %w", output.Error(output.ErrNotFound, "missing"))
			},
			wantCode:   output.ExitNotFound,
			wantStderr: "",
		},
		{
			name:       "non-reported coded error maps code and prints to stderr",
			err:        func() error { return &output.CodedError{Code: output.ErrConflict, Msg: "clash"} },
			wantCode:   output.ExitConflict,
			wantStderr: "Error: clash\n",
		},
		{
			name:       "unknown code collapses to ExitError",
			err:        func() error { return &output.CodedError{Code: "WAT", Msg: "huh"} },
			wantCode:   output.ExitError,
			wantStderr: "Error: huh\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			// captureStdout absorbs the JSON envelope output.Error writes
			// on construction. The boundary itself only writes to the
			// stderr Writer passed in.
			_ = captureStdout(t, func() {
				code := reportExitError(&stderr, tt.err())
				if code != tt.wantCode {
					t.Errorf("exit code = %d, want %d", code, tt.wantCode)
				}
			})
			if got := stderr.String(); got != tt.wantStderr {
				t.Errorf("stderr = %q, want %q", got, tt.wantStderr)
			}
		})
	}
}
