package nibcore

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/alphaleonis/nibs/internal/store"
)

// TestWalkStoreDirsSharesTheDotRule pins the watcher's directory set against the
// same definition of "inside the store" that Load and the migrate scans use.
//
// They disagreed. WalkStoreFiles prunes dot directories — a store's own `.git`
// is not store content — while StartWatching enumerated with a bare
// filepath.WalkDir and registered fsnotify watches on every directory it found.
// So a nib-shaped `.md` changing inside a dot directory during a live serve was
// loaded by handleChanges, from a directory no scan would ever have read.
func TestWalkStoreDirsSharesTheDotRule(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{
		filepath.Join(root, store.DataDirName),
		filepath.Join(root, store.DataDirName, "sub"),
		filepath.Join(root, store.ArchiveDirName),
		filepath.Join(root, ".git"),
		filepath.Join(root, ".git", "refs"),
		filepath.Join(root, store.DataDirName, ".obsidian"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	var got []string
	if err := WalkStoreDirs(root, func(dir string) error {
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			return err
		}
		got = append(got, filepath.ToSlash(rel))
		return nil
	}); err != nil {
		t.Fatalf("WalkStoreDirs: %v", err)
	}
	slices.Sort(got)

	want := []string{".", store.ArchiveDirName, store.DataDirName, store.DataDirName + "/sub"}
	if !slices.Equal(got, want) {
		t.Errorf("WalkStoreDirs = %v, want %v", got, want)
	}
}
