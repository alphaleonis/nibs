package nibcore

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alphaleonis/nibs/internal/area"
	"github.com/alphaleonis/nibs/internal/store"
	"github.com/alphaleonis/nibs/internal/testskip"
	"github.com/alphaleonis/nibs/internal/yamlfile"
)

// The areas.yml read is nibcore's: internal/area parses bytes and never sees the
// file. These pin what the file's presence, shape and size mean at that read.

// loadIOWithin runs Load under a deadline, so a read that blocks in open(2)
// fails the test instead of hanging the package's run.
func loadIOWithin(t *testing.T, core *Core) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- core.Load() }()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("Load did not return; the areas.yml read is blocked")
		return nil
	}
}

func TestLoadRecordsWhetherAnAreasFileWasRead(t *testing.T) {
	t.Run("no file is the empty vocabulary, not an error", func(t *testing.T) {
		core, _ := setupAreasCore(t)
		if err := core.Load(); err != nil {
			t.Fatalf("Load over a store with no areas.yml: %v", err)
		}
		if core.Areas() == nil || !core.Areas().IsEmpty() {
			t.Errorf("Areas() = %v, want a non-nil empty vocabulary", core.Areas())
		}
		if core.areasState().fromFile {
			t.Error("fromFile = true with no areas.yml, which would read a later absence as a vanished file")
		}
	})

	t.Run("a file declaring nothing was still read", func(t *testing.T) {
		core, nibsDir := setupAreasCore(t)
		writeStoreAreas(t, nibsDir, "areas: []\n")
		if err := core.Load(); err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !core.Areas().IsEmpty() {
			t.Errorf("Areas() = %v, want empty", core.Areas().Paths())
		}
		if !core.areasState().fromFile {
			t.Error("fromFile = false after reading an areas.yml")
		}
	})
}

// TestLoadNamesTheAreasFileItRefuses pins that the refusal names the file: the
// parser works on bytes, so the loader is the only place the path can come from,
// and cmd's served-path scrubber keys on that path being in the text.
func TestLoadNamesTheAreasFileItRefuses(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		sentence string
	}{
		{name: "content that does not decode", body: "areas:\n  - name: [unclosed\n", sentence: " is not readable as an areas vocabulary: "},
		{name: "a malformed vocabulary", body: "areas:\n    - name: web\n    - name: web\n", sentence: " declares a malformed area vocabulary: "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, nibsDir := setupAreasCore(t)
			writeStoreAreas(t, nibsDir, tt.body)

			err := core.Load()
			if err == nil {
				t.Fatal("Load accepted the vocabulary, want a refusal")
			}
			path := store.NewLayout(nibsDir).AreasPath()
			if !strings.Contains(err.Error(), path+tt.sentence) {
				t.Errorf("error = %q, want it to contain %q", err, path+tt.sentence)
			}
			var parseErr *area.ParseError
			if !errors.As(err, &parseErr) {
				t.Errorf("error = %T, want it to unwrap to *area.ParseError", err)
			}
		})
	}
}

// TestLoadRefusesAnAreasFileItMustNotRead pins that an areas.yml the reader
// refuses is a refusal of the load, never the empty vocabulary an ABSENT file
// gets: that would undeclare every `area:` the store carries.
func TestLoadRefusesAnAreasFileItMustNotRead(t *testing.T) {
	tests := []struct {
		name   string
		create func(t *testing.T, path string)
		want   string
	}{
		{
			name: "a directory",
			create: func(t *testing.T, path string) {
				if err := os.Mkdir(path, 0o755); err != nil {
					t.Fatal(err)
				}
			},
			want: "not a regular file",
		},
		{
			name: "a named pipe",
			create: func(t *testing.T, path string) {
				if err := mkfifo(path); err != nil {
					testskip.Unavailable(t, testskip.NamedPipes, "mkfifo(%s): %v", path, err)
				}
			},
			want: "not a regular file",
		},
		{
			name: "a file past the size cap",
			create: func(t *testing.T, path string) {
				body := "areas:\n    - name: web\n# " + strings.Repeat("x", yamlfile.MaxBytes) + "\n"
				if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			want: "configuration limit",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, nibsDir := setupAreasCore(t)
			tt.create(t, store.NewLayout(nibsDir).AreasPath())

			err := loadIOWithin(t, core)
			if err == nil {
				t.Fatalf("Load accepted %s at areas.yml", tt.name)
			}
			if errors.Is(err, os.ErrNotExist) {
				t.Fatalf("Load reported %s as an absent areas.yml (%v)", tt.name, err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to contain %q", err, tt.want)
			}
			var loadErr *AreasLoadError
			if !errors.As(err, &loadErr) {
				t.Errorf("error = %T, want *AreasLoadError so an edit names the vocabulary phase", err)
			}
		})
	}
}
