package store_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alphaleonis/nibs/internal/store"
	"github.com/alphaleonis/nibs/testdata/fixtures"
)

// TestCopySampleProject guards the helper every write-side fixture test builds
// on, and it cannot sit beside that helper: `go` excludes any directory named
// testdata from wildcard package matching, so a test under testdata/ is never
// built by `./...` — the pattern the test gate, CI and the linter all run. It
// lives with the layout it asserts instead, which `./...` does reach.
// TestNoTestFilesUnderTestdata in internal/testskip fails on a test left in
// that trap.
//
// It is an EXTERNAL test package by necessity: fixtures derives every path it
// returns from this package, so an in-package test importing it closes an
// import cycle.
func TestCopySampleProject(t *testing.T) {
	root := fixtures.CopySampleProject(t)

	// The config travels with the data, inside the store — a copy without it
	// would be read under the wrong prefix and vocabulary.
	configPath := filepath.Join(fixtures.NibsPath(root), store.ConfigFileName)
	if _, err := os.Stat(configPath); err != nil {
		t.Errorf("expected %s in the copied store: %v", store.ConfigFileName, err)
	}

	dataDir := fixtures.DataPath(root)
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		t.Fatalf("reading the copied data directory: %v", err)
	}

	var mdCount int
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".md" {
			mdCount++
		}
	}
	if mdCount < 80 {
		t.Errorf("expected at least 80 nib files in %s, got %d", dataDir, mdCount)
	}

	// A copy a test may mutate freely is the helper's whole purpose, so a write
	// to it must not reach the committed fixture.
	//
	// The cleanup is registered BEFORE the write, because the failure this
	// asserts against is the one that leaves the file inside the committed
	// tree: git tracks that directory on purpose, and a stray nib there takes
	// the fixture from 89 to 90 for every other test in the suite. Removing it
	// costs the diagnostic nothing — the assertion has already run and named
	// the path by then.
	committed := filepath.Join(fixtures.DataPath(fixtures.SampleProjectDir(t)), "test-write.md")
	t.Cleanup(func() { _ = os.Remove(committed) })
	if err := os.WriteFile(filepath.Join(dataDir, "test-write.md"), []byte("test"), 0o644); err != nil {
		t.Fatalf("writing to the temporary copy: %v", err)
	}
	if _, err := os.Stat(committed); err == nil {
		t.Errorf("the write to the temporary copy reached the committed fixture at %s", committed)
	}
}
