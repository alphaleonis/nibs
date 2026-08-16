// Package fixtures provides test helpers for working with fixture datasets.
//
// Fixture data lives under testdata/fixtures/sample-project/.nibs/ — a
// complete store, holding config.yml alongside its data/ directory — and must
// never be modified in place. Use [CopySampleProject] to get a temporary copy
// that tests can safely mutate.
package fixtures

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/alphaleonis/nibs/internal/store"
)

// CopySampleProject copies the sample-project fixture to a temporary directory
// and returns the PROJECT root — the directory containing the .nibs store. The
// temporary directory is cleaned up automatically when the test finishes.
func CopySampleProject(t *testing.T) string {
	t.Helper()

	src := findFixtureDir(t)
	dst := t.TempDir()

	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copying sample-project fixture: %v", err)
	}
	return dst
}

// NibsPath returns the .nibs STORE directory for a fixture root returned by
// [CopySampleProject]. Pass it to --nibs-path or nibcore.New: the store carries
// its own config, so the fixture is always read under its own `tnib-` prefix.
func NibsPath(root string) string {
	return filepath.Join(root, store.DirName)
}

// DataPath returns the store's data/ directory for a fixture root — where the
// fixture's nib files live.
func DataPath(root string) string {
	return store.NewLayout(NibsPath(root)).DataDir()
}

// findFixtureDir locates the sample-project directory relative to this file.
func findFixtureDir(t *testing.T) string {
	t.Helper()

	// When running tests, the working directory is the package directory.
	// Walk up to find the testdata/fixtures/sample-project path.
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getting working directory: %v", err)
	}

	for {
		candidate := filepath.Join(dir, "testdata", "fixtures", "sample-project")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find testdata/fixtures/sample-project from any parent directory")
		}
		dir = parent
	}
}
