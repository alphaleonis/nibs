package nibcore

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/area"
	"github.com/alphaleonis/nibs/internal/store"
	"github.com/alphaleonis/nibs/internal/testskip"
	"github.com/alphaleonis/nibs/internal/yamlfile"
)

// The area planners work on bytes, so the half of an edit that touches the
// areas.yml itself — reading it under the lock, writing it back, and naming it in
// a refusal — is Core's, and is pinned here.

// TestAreaEditPreservesTheVocabularyFilesMode holds the vocabulary write to the
// contract every config write has: a file kept private stays private.
func TestAreaEditPreservesTheVocabularyFilesMode(t *testing.T) {
	core, nibsDir := areaVerbCore(t)
	testskip.NeedPosixFileModes(t, nibsDir)
	path := store.NewLayout(nibsDir).AreasPath()
	// chmod rather than a mode handed to the write, which umask would narrow.
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := core.AddArea(context.Background(), "platform", "", ""); err != nil {
		t.Fatalf("AddArea: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}
}

// TestAreaAddBootstrapsAStoreWithNoVocabularyFile: a store that has never
// declared an area has no areas.yml, and Core has to hand the planner that
// absence rather than refusing on the read.
func TestAreaAddBootstrapsAStoreWithNoVocabularyFile(t *testing.T) {
	core, nibsDir := setupCoreWithDeclaredAreas(t)
	path := store.NewLayout(nibsDir).AreasPath()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	// Reloaded so the store's own view is "never had one" rather than a file
	// that vanished, which is a different refusal.
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	res, err := core.AddArea(context.Background(), "platform", "Build and release", "")
	if err != nil {
		t.Fatalf("AddArea: %v", err)
	}
	if !res.Areas.Exists("platform") {
		t.Errorf("Areas = %v, want platform declared", res.Areas.Paths())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the add wrote no areas.yml: %v", err)
	}
	if !strings.Contains(string(raw), "name: platform") {
		t.Errorf("areas.yml = %q, want it to declare platform", raw)
	}
}

// TestAreaEditRefusalNamesTheVocabularyFile: the planner never sees a path, so
// Core is what puts the file into a content refusal for a surface whose reader
// may be told where it is — and Error still names none.
func TestAreaEditRefusalNamesTheVocabularyFile(t *testing.T) {
	core, nibsDir := areaVerbCore(t)
	path := store.NewLayout(nibsDir).AreasPath()
	// The loader accepts a second document and the editor refuses it, so the
	// edit gets past the re-read and reaches the planner.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, []byte("---\nother: 1\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = core.RenameArea(context.Background(), "web", "platform")
	var refusal *area.EditRefusal
	if !errors.As(err, &refusal) {
		t.Fatalf("error = %v (%T), want an *area.EditRefusal", err, err)
	}
	if refusal.File != path {
		t.Errorf("File = %q, want %q", refusal.File, path)
	}
	if !strings.Contains(refusal.Naming(refusal.File), path) {
		t.Errorf("Naming = %q, want it to name %s", refusal.Naming(refusal.File), path)
	}
	if strings.Contains(refusal.Error(), nibsDir) {
		t.Errorf("Error = %q names the store directory", refusal.Error())
	}
}

// TestAreaEditRefusesAVocabularyFileItCannotRead covers the shapes of areas.yml
// the bounded read refuses: past the size cap, or not a regular file at all. The
// edit reports the re-read of the vocabulary as the phase that failed and writes
// nothing; the FIFO row is the one a missing regularity check would HANG on
// rather than fail.
func TestAreaEditRefusesAVocabularyFileItCannotRead(t *testing.T) {
	tests := []struct {
		name   string
		damage func(t *testing.T, path string)
		want   string
	}{
		{
			name: "a file past the size cap",
			damage: func(t *testing.T, path string) {
				if err := os.WriteFile(path, []byte("areas: []\n# "+strings.Repeat("x", yamlfile.MaxBytes)+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			want: "configuration limit",
		},
		{
			name: "a directory",
			damage: func(t *testing.T, path string) {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0o755); err != nil {
					t.Fatal(err)
				}
			},
			want: "not a regular file",
		},
		{
			name: "a named pipe",
			damage: func(t *testing.T, path string) {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := mkfifo(path); err != nil {
					testskip.Unavailable(t, testskip.NamedPipes, "mkfifo(%s): %v", path, err)
				}
			},
			want: "not a regular file",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, nibsDir := areaVerbCore(t)
			tt.damage(t, store.NewLayout(nibsDir).AreasPath())

			_, err := core.AddArea(context.Background(), "platform", "", "")
			var ioErr *AreaEditIOError
			if !errors.As(err, &ioErr) {
				t.Fatalf("error = %v (%T), want an *AreaEditIOError", err, err)
			}
			if ioErr.Phase != AreaEditPhaseLoadVocabulary {
				t.Errorf("Phase = %d, want AreaEditPhaseLoadVocabulary", ioErr.Phase)
			}
			if !strings.Contains(ioErr.Cause.Error(), tt.want) {
				t.Errorf("Cause = %v, want it to carry %q", ioErr.Cause, tt.want)
			}
			if core.Areas().Exists("platform") {
				t.Error("a refused add installed the area anyway")
			}
		})
	}
}
