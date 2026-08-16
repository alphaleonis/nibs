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
	"github.com/alphaleonis/nibs/internal/store"
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

// TestResolveStoreDirRefusesTheLegacyProjectConfig pins the first of the two
// guards that keep a mis-aimed --config from turning a project directory into
// a store. `--config <project>/.nibs.yml` was the DOCUMENTED way to work
// against another project before the layout inversion, and its directory is
// the project, not the store — so accepting it would point every command
// (`nibs migrate` above all) at the project tree. The refusal names the
// replacement rather than merely rejecting.
func TestResolveStoreDirRefusesTheLegacyProjectConfig(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	resetRootPersistentFlags()
	t.Setenv("NIBS_PATH", "")

	projectDir := t.TempDir()
	storeDir := filepath.Join(projectDir, store.DirName)
	if err := os.MkdirAll(storeDir, 0755); err != nil {
		t.Fatalf("mkdir store: %v", err)
	}
	legacy := filepath.Join(projectDir, store.LegacyProjectConfigFileName)
	if err := os.WriteFile(legacy, []byte("nibs:\n  prefix: leg-\n"), 0644); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}
	configPath = legacy

	_, err := resolveStoreDir()
	if err == nil {
		t.Fatal("resolveStoreDir accepted --config at the pre-layout .nibs.yml; that path names the PROJECT, not the store")
	}
	for _, want := range []string{"--nibs-path", storeDir, store.ConfigFileName} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal = %q, want it to mention %q", err.Error(), want)
		}
	}
}

// TestResolveStoreDirRequiresStoreEvidence pins the second guard: a directory
// named EXPLICITLY — by --nibs-path, by NIBS_PATH, or through --config's
// containing directory — must carry positive evidence that it is a store.
// Without it, a path aimed one level too high (`--nibs-path <project>`)
// resolves to the project tree, and `nibs migrate` relocates and rewrites
// every front-mattered .md it finds there.
//
// The legacy shape (a top-level *.md beside a `.nibs.yml`) counts as evidence
// on purpose: a pre-layout store must stay resolvable, or `nibs migrate` could
// never reach the stores it exists to convert.
func TestResolveStoreDirRequiresStoreEvidence(t *testing.T) {
	tests := []struct {
		name   string
		build  func(t *testing.T, tmp string) string
		accept bool
	}{
		{
			name: "a .nibs store holding data/",
			build: func(t *testing.T, tmp string) string {
				dir := filepath.Join(tmp, "proj", store.DirName)
				mkdirAllT(t, filepath.Join(dir, store.DataDirName))
				return dir
			},
			accept: true,
		},
		{
			name: "an empty directory named .nibs",
			build: func(t *testing.T, tmp string) string {
				dir := filepath.Join(tmp, "proj", store.DirName)
				mkdirAllT(t, dir)
				return dir
			},
			accept: true,
		},
		{
			name: "a differently named store holding only config.yml",
			build: func(t *testing.T, tmp string) string {
				dir := filepath.Join(tmp, "proj", "nibdata")
				mkdirAllT(t, dir)
				writeFileT(t, filepath.Join(dir, store.ConfigFileName), "nibs:\n  prefix: nd-\n")
				return dir
			},
			accept: true,
		},
		{
			name: "a differently named store holding only archive/",
			build: func(t *testing.T, tmp string) string {
				dir := filepath.Join(tmp, "proj", "nibdata")
				mkdirAllT(t, filepath.Join(dir, store.ArchiveDirName))
				return dir
			},
			accept: true,
		},
		{
			name: "the legacy shape: a top-level nib file beside a .nibs.yml",
			build: func(t *testing.T, tmp string) string {
				dir := filepath.Join(tmp, "proj", "nibdata")
				mkdirAllT(t, dir)
				writeFileT(t, filepath.Join(dir, "leg-a1--one.md"), layoutNib)
				writeFileT(t, filepath.Join(tmp, "proj", store.LegacyProjectConfigFileName), "nibs:\n  prefix: leg-\n")
				return dir
			},
			accept: true,
		},
		{
			name: "a project directory with neither shape",
			build: func(t *testing.T, tmp string) string {
				dir := filepath.Join(tmp, "proj")
				mkdirAllT(t, filepath.Join(dir, "docs"))
				writeFileT(t, filepath.Join(dir, "docs", "post.md"), "---\ntitle: A post\n---\n\nBody.\n")
				return dir
			},
		},
		{
			name: "markdown at the top level but no .nibs.yml beside it",
			build: func(t *testing.T, tmp string) string {
				dir := filepath.Join(tmp, "proj", "notes")
				mkdirAllT(t, dir)
				writeFileT(t, filepath.Join(dir, "README.md"), "# Notes\n")
				return dir
			},
		},
		{
			name: "a .nibs.yml beside it but no markdown at the top level",
			build: func(t *testing.T, tmp string) string {
				dir := filepath.Join(tmp, "proj", "notes")
				mkdirAllT(t, dir)
				writeFileT(t, filepath.Join(tmp, "proj", store.LegacyProjectConfigFileName), "nibs:\n  prefix: leg-\n")
				return dir
			},
		},
	}

	// The guard must cover every route to an explicitly named directory, not
	// just the flag: NIBS_PATH and --config's containing directory reach the
	// same branch.
	routes := []struct {
		name  string
		apply func(t *testing.T, dir string)
	}{
		{"--nibs-path", func(t *testing.T, dir string) { nibsPath = dir }},
		{"NIBS_PATH", func(t *testing.T, dir string) { t.Setenv("NIBS_PATH", dir) }},
		{"--config", func(t *testing.T, dir string) {
			configPath = filepath.Join(dir, store.ConfigFileName)
		}},
	}

	for _, tt := range tests {
		for _, route := range routes {
			t.Run(tt.name+" via "+route.name, func(t *testing.T) {
				t.Cleanup(resetRootPersistentFlags)
				resetRootPersistentFlags()
				t.Setenv("NIBS_PATH", "")
				dir := tt.build(t, t.TempDir())
				route.apply(t, dir)

				got, err := resolveStoreDir()
				if tt.accept {
					if err != nil {
						t.Fatalf("resolveStoreDir() error = %v, want the store %s", err, dir)
					}
					if got != dir {
						t.Errorf("resolveStoreDir() = %q, want %q", got, dir)
					}
					return
				}
				if err == nil {
					t.Fatalf("resolveStoreDir() = %q with no error; %s carries no evidence of being a store", got, dir)
				}
				if !strings.Contains(err.Error(), "not a nibs store") {
					t.Errorf("refusal = %q, want it to say the directory is not a nibs store", err.Error())
				}
			})
		}
	}
}

// TestResolveStoreDirExplainsAPreLayoutProject pins the discovery message for
// the population the layout inversion is hardest on: a project whose data
// lived outside `.nibs` via the retired `nibs.path` key. It has no `.nibs`
// DIRECTORY, so the upward walk finds nothing — and the generic "run nibs
// init" answer is the one action that strands its data, creating an empty
// store with a derived prefix beside the real files.
func TestResolveStoreDirExplainsAPreLayoutProject(t *testing.T) {
	tests := []struct {
		name       string
		configBody string
		want       []string
		notWant    []string
	}{
		{
			name:       "nibs.path names the real data directory",
			configBody: "nibs:\n  prefix: leg-\n  path: nibdata\n",
			want:       []string{"nibs.path", "nibdata", ".nibs", "nibs migrate"},
			notWant:    []string{"run 'nibs init' to create one"},
		},
		{
			name:       "a pre-layout config with no nibs.path",
			configBody: "nibs:\n  prefix: leg-\n",
			want:       []string{".nibs.yml", "nibs migrate"},
			notWant:    []string{"run 'nibs init' to create one"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(resetRootPersistentFlags)
			resetRootPersistentFlags()
			t.Setenv("NIBS_PATH", "")

			tmp := t.TempDir()
			t.Setenv("NIBS_CONFIG_ROOT", tmp)
			projectDir := filepath.Join(tmp, "proj")
			mkdirAllT(t, filepath.Join(projectDir, "nibdata"))
			writeFileT(t, filepath.Join(projectDir, "nibdata", "leg-a1--one.md"), layoutNib)
			writeFileT(t, filepath.Join(projectDir, store.LegacyProjectConfigFileName), tt.configBody)
			t.Chdir(projectDir)

			_, err := resolveStoreDir()
			if err == nil {
				t.Fatal("resolveStoreDir found a store where there is no .nibs directory")
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("message = %q, want it to mention %q", err.Error(), want)
				}
			}
			for _, notWant := range tt.notWant {
				if strings.Contains(err.Error(), notWant) {
					t.Errorf("message = %q, must not suggest %q — that is what strands the data", err.Error(), notWant)
				}
			}
		})
	}
}

func mkdirAllT(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}

func writeFileT(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
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
