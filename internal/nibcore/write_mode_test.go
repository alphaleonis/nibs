package nibcore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alphaleonis/nibs/internal/fsutil"
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

// A nib that has never existed has no mode to preserve, so it takes the base
// 0644 with the process umask subtracted — the mode a plain create would have
// produced, except clamped: a umask can only clear bits, so a permissive one
// cannot widen a nib past 0644 into group- or world-writable. Tightening is
// honored, loosening is not offered.
func TestCreateAppliesTheUmaskToTheDefaultMode(t *testing.T) {
	for _, tc := range []struct {
		name  string
		umask os.FileMode
		want  os.FileMode
	}{
		{"typical 022 leaves the default alone", 0o022, 0o644},
		{"private 077 tightens to owner-only", 0o077, 0o600},
		{"group-only 027", 0o027, 0o640},
		{"permissive 000 is CLAMPED, not widened to 0666", 0o000, 0o644},
	} {
		t.Run(tc.name, func(t *testing.T) {
			core, nibsDir := setupTestCore(t)
			testskip.NeedPosixFileModes(t, storeData(t, nibsDir))

			restore := fsutil.Umask
			fsutil.Umask = func() os.FileMode { return tc.umask }
			t.Cleanup(func() { fsutil.Umask = restore })

			b := createTestNib(t, core, "test-umask", "A new nib", "todo")

			info, err := os.Stat(filepath.Join(core.Root(), b.Path))
			if err != nil {
				t.Fatalf("stat after create: %v", err)
			}
			if got := info.Mode().Perm(); got != tc.want {
				t.Errorf("new nib under umask %04o = %04o, want %04o", tc.umask, got, tc.want)
			}
		})
	}
}

// The umask applies to a file being CREATED and to nothing else. An existing
// nib's mode is the user's answer already, so a umask must not re-narrow it on
// every subsequent write — that would walk a 0644 nib down to 0600 behind the
// user's back the first time they edited it from a tightened shell.
func TestUmaskDoesNotNarrowAnExistingFile(t *testing.T) {
	core, nibsDir := setupTestCore(t)
	testskip.NeedPosixFileModes(t, storeData(t, nibsDir))

	b := createTestNib(t, core, "test-003", "Already exists", "todo")
	path := filepath.Join(core.Root(), b.Path)

	const want os.FileMode = 0o664
	if err := os.Chmod(path, want); err != nil {
		t.Fatalf("chmod %v: %v", want, err)
	}

	restore := fsutil.Umask
	fsutil.Umask = func() os.FileMode { return 0o077 }
	t.Cleanup(func() { fsutil.Umask = restore })

	b.Priority = "high"
	if err := core.Update(b, nil); err != nil {
		t.Fatalf("Update: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after update: %v", err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("existing nib after an edit under umask 077 = %04o, want %04o (preserved, not re-masked)", got, want)
	}
}
