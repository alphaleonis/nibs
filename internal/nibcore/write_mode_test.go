package nibcore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alphaleonis/nibs/internal/testskip"
)

// A nib file's permissions belong to the user, not to the tool. Every write
// replaces the file through a temp-file-plus-rename, which carries nothing of
// the old file with it, so a mode the user tightened survives only if the
// writer reads it back and passes it through. Hardcoding one instead widens a
// private nib on the next unrelated edit, silently and with nothing to notice
// it by short of re-running stat.
func TestWritePreservesExistingFileMode(t *testing.T) {
	core, nibsDir := setupTestCore(t)
	testskip.NeedPosixFileModes(t, storeData(t, nibsDir))

	b := createTestNib(t, core, "test-001", "A private nib", "todo")
	path := filepath.Join(core.Root(), b.Path)

	// Deliberately not 0644: the bug returns the hardcoded default, so a mode
	// equal to it could not tell a preserved mode from a discarded one.
	const want os.FileMode = 0o600
	if err := os.Chmod(path, want); err != nil {
		t.Fatalf("chmod %v: %v", want, err)
	}

	b.Priority = "high"
	if err := core.Update(b, nil); err != nil {
		t.Fatalf("Update: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after update: %v", err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("mode after an unrelated edit = %v, want %v (the user's mode was discarded)", got, want)
	}
}

// The create path has no mode to preserve, so it keeps the 0644 this project
// already settled on for a file that has never existed (config.Save makes the
// same choice for config.yml). Pinned so the preservation change above cannot
// quietly become "whatever the temp file happened to be", which os.CreateTemp
// makes 0600.
func TestCreateUsesTheDefaultMode(t *testing.T) {
	core, nibsDir := setupTestCore(t)
	testskip.NeedPosixFileModes(t, storeData(t, nibsDir))

	b := createTestNib(t, core, "test-002", "A new nib", "todo")

	info, err := os.Stat(filepath.Join(core.Root(), b.Path))
	if err != nil {
		t.Fatalf("stat after create: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o644); got != want {
		t.Errorf("mode of a newly created nib = %v, want %v", got, want)
	}
}
