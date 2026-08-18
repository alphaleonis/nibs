package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// unusablePathFixture returns a `nibs.path` value Windows refuses outright.
//
// The pipe is one of the characters reserved for the shell's redirection syntax
// and forbidden in a filename, so the stat fails ERROR_INVALID_NAME rather than
// reporting the name missing.
//
// NOT a colon, which was the first choice and was wrong: `bad:name` is valid
// syntax naming an alternate data stream, so it fails ERROR_FILE_NOT_FOUND and
// routes through fs.ErrNotExist — which would have made every assertion here
// pass while testing the untouched branch.
//
// Nothing is created: the point is a name the filesystem will not hold.
func unusablePathFixture(t *testing.T, projectDir string) string {
	t.Helper()
	_ = projectDir
	return `bad|name`
}

func statPathForTest(path string) (os.FileInfo, error) { return os.Stat(path) }

func isNotExistForTest(err error) bool { return os.IsNotExist(err) }

// TestWindowsUnusablePathIsNotErrNotExist records the measurement the fix rests
// on: Go maps only ERROR_FILE_NOT_FOUND, ERROR_PATH_NOT_FOUND, _ERROR_BAD_NETPATH
// and ENOENT onto fs.ErrNotExist (syscall.Errno.Is), so ERROR_INVALID_NAME
// reaches preLayoutRemedy as neither absence nor a recognized I/O fault.
func TestWindowsUnusablePathIsNotErrNotExist(t *testing.T) {
	dir := t.TempDir()
	probe := filepath.Join(dir, unusablePathFixture(t, dir))
	_, err := os.Stat(probe)
	if err == nil {
		t.Fatalf("Windows accepted %q as a path component; this fixture no longer models an unusable path", probe)
	}
	if os.IsNotExist(err) {
		t.Fatalf("ERROR_INVALID_NAME now maps to fs.ErrNotExist (%v), so the untouched branch already handles it and isUnusablePath is dead code", err)
	}
	if !isUnusablePath(err) {
		t.Fatalf("isUnusablePath does not recognize %v, which is the error this whole classification exists for", err)
	}
}
