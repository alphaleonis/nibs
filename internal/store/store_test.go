package store

import (
	"os"
	"path/filepath"
	"testing"
)

// mkStore creates a `.nibs` directory under dir and returns its path.
func mkStore(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, DirName)
	if err := os.MkdirAll(p, 0755); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	return p
}

// mkSub creates a child directory and returns its path.
func mkSub(t *testing.T, parent, name string) string {
	t.Helper()
	p := filepath.Join(parent, name)
	if err := os.MkdirAll(p, 0755); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	return p
}

func TestFindStore(t *testing.T) {
	t.Run("finds the store in the start directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("NIBS_CONFIG_ROOT", tmpDir)
		want := mkStore(t, tmpDir)

		found, err := FindStore(tmpDir)
		if err != nil {
			t.Fatalf("FindStore() error = %v", err)
		}
		if found != want {
			t.Errorf("FindStore() = %q, want %q", found, want)
		}
	})

	t.Run("finds the store in an ancestor directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("NIBS_CONFIG_ROOT", tmpDir)
		want := mkStore(t, tmpDir)
		subDir := mkSub(t, tmpDir, filepath.Join("sub", "dir"))

		found, err := FindStore(subDir)
		if err != nil {
			t.Fatalf("FindStore() error = %v", err)
		}
		if found != want {
			t.Errorf("FindStore() = %q, want %q", found, want)
		}
	})

	t.Run("returns empty when no store exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("NIBS_CONFIG_ROOT", tmpDir)

		found, err := FindStore(tmpDir)
		if err != nil {
			t.Fatalf("FindStore() error = %v", err)
		}
		if found != "" {
			t.Errorf("FindStore() = %q, want empty string", found)
		}
	})

	// A `.nibs` FILE is not a store. The locator stats for a directory
	// deliberately, so a stray file of that name cannot halt the walk and hide
	// the real store above it.
	t.Run("a .nibs file does not stop the walk", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("NIBS_CONFIG_ROOT", tmpDir)
		want := mkStore(t, tmpDir)
		decoy := mkSub(t, tmpDir, "decoy")
		if err := os.WriteFile(filepath.Join(decoy, DirName), []byte("not a store"), 0644); err != nil {
			t.Fatalf("WriteFile error = %v", err)
		}

		found, err := FindStore(decoy)
		if err != nil {
			t.Fatalf("FindStore() error = %v", err)
		}
		if found != want {
			t.Errorf("FindStore() = %q, want %q (a .nibs FILE is not a store)", found, want)
		}
	})
}

// TestFindStore_RespectsCeiling pins the NIBS_CONFIG_ROOT sandbox: the walk
// checks the ceiling directory itself but never ascends above it, so a stray
// ancestor store cannot leak into a test that expects none.
func TestFindStore_RespectsCeiling(t *testing.T) {
	t.Run("store above the ceiling is not found", func(t *testing.T) {
		root := t.TempDir()
		ceiling := mkSub(t, root, "ceiling")
		start := mkSub(t, ceiling, "start")
		mkStore(t, root) // above the ceiling

		t.Setenv("NIBS_CONFIG_ROOT", ceiling)
		found, err := FindStore(start)
		if err != nil {
			t.Fatalf("FindStore() error = %v", err)
		}
		if found != "" {
			t.Errorf("FindStore() = %q, want empty (store above ceiling must not be found)", found)
		}
	})

	t.Run("store at the ceiling is found", func(t *testing.T) {
		root := t.TempDir()
		ceiling := mkSub(t, root, "ceiling")
		start := mkSub(t, ceiling, "start")
		want := mkStore(t, ceiling)

		t.Setenv("NIBS_CONFIG_ROOT", ceiling)
		found, err := FindStore(start)
		if err != nil {
			t.Fatalf("FindStore() error = %v", err)
		}
		if found != want {
			t.Errorf("FindStore() = %q, want %q (store at ceiling must be found)", found, want)
		}
	})

	t.Run("store below the ceiling is found", func(t *testing.T) {
		root := t.TempDir()
		ceiling := mkSub(t, root, "ceiling")
		start := mkSub(t, ceiling, "start")
		want := mkStore(t, start)

		t.Setenv("NIBS_CONFIG_ROOT", ceiling)
		found, err := FindStore(start)
		if err != nil {
			t.Fatalf("FindStore() error = %v", err)
		}
		if found != want {
			t.Errorf("FindStore() = %q, want %q (store below ceiling must be found)", found, want)
		}
	})
}

func TestLayoutPaths(t *testing.T) {
	root := filepath.Join("home", "user", "project", ".nibs")
	l := NewLayout(root)

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"Root", l.Root(), root},
		{"ConfigPath", l.ConfigPath(), filepath.Join(root, "config.yml")},
		{"DataDir", l.DataDir(), filepath.Join(root, "data")},
		{"ArchiveDir", l.ArchiveDir(), filepath.Join(root, "archive")},
		{"ProjectDir", l.ProjectDir(), filepath.Join("home", "user", "project")},
		{"DataRel", l.DataRel("x.md"), "data/x.md"},
		{"ArchiveRel", l.ArchiveRel("x.md"), "archive/x.md"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}

// TestLayoutClassifiesRelPaths pins which store-relative paths count as
// archived and which as active. A bare basename belongs to NEITHER: after the
// layout inversion the store root holds no nib files, so a root-level path is
// a legacy leftover rather than an active nib.
func TestLayoutClassifiesRelPaths(t *testing.T) {
	l := NewLayout(filepath.Join("p", ".nibs"))

	tests := []struct {
		rel        string
		isArchived bool
		isData     bool
	}{
		{"data/x.md", false, true},
		{"data/sub/x.md", false, true},
		{"archive/x.md", true, false},
		{"archive/sub/x.md", true, false},
		{"x.md", false, false},
		{"data", false, false},
		{"archive", false, false},
		{"datastore/x.md", false, false},
		{"archived/x.md", false, false},
		{filepath.Join("data", "x.md"), false, true},
		{filepath.Join("archive", "x.md"), true, false},
	}
	for _, tt := range tests {
		t.Run(tt.rel, func(t *testing.T) {
			if got := l.IsArchivedRel(tt.rel); got != tt.isArchived {
				t.Errorf("IsArchivedRel(%q) = %v, want %v", tt.rel, got, tt.isArchived)
			}
			if got := l.IsDataRel(tt.rel); got != tt.isData {
				t.Errorf("IsDataRel(%q) = %v, want %v", tt.rel, got, tt.isData)
			}
		})
	}
}

// TestWatchableDirs pins that the store ROOT is always watched — including
// when data/ does not exist yet — so a data/ directory created under a running
// process arrives as a create event in a directory that IS being watched.
func TestWatchableDirs(t *testing.T) {
	t.Run("root only when no subdirectories exist", func(t *testing.T) {
		root := t.TempDir()
		got := NewLayout(root).WatchableDirs()
		if len(got) != 1 || got[0] != root {
			t.Errorf("WatchableDirs() = %v, want [%q]", got, root)
		}
	})

	t.Run("root plus the subdirectories that exist", func(t *testing.T) {
		root := t.TempDir()
		l := NewLayout(root)
		if err := os.MkdirAll(l.DataDir(), 0755); err != nil {
			t.Fatalf("MkdirAll error = %v", err)
		}
		if err := os.MkdirAll(l.ArchiveDir(), 0755); err != nil {
			t.Fatalf("MkdirAll error = %v", err)
		}

		got := l.WatchableDirs()
		want := []string{root, l.DataDir(), l.ArchiveDir()}
		if len(got) != len(want) {
			t.Fatalf("WatchableDirs() = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("WatchableDirs()[%d] = %q, want %q", i, got[i], want[i])
			}
		}
	})
}
