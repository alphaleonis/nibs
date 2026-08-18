//go:build !windows

package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// unusablePathFixture returns a `nibs.path` value this system refuses for a
// STRUCTURAL reason rather than because the target is missing.
//
// POSIX accepts very nearly any byte in a filename, so there is no equivalent of
// Windows' forbidden characters. The reachable shape is a path whose PARENT is
// not a directory: `afile/sub` where `afile` is a regular file fails ENOTDIR, and
// no amount of mounting or chmod makes it nameable.
func unusablePathFixture(t *testing.T, projectDir string) string {
	t.Helper()
	blocker := filepath.Join(projectDir, "afile")
	if err := os.WriteFile(blocker, []byte("not a directory\n"), 0o644); err != nil {
		t.Fatalf("writing the blocking file: %v", err)
	}
	return "afile/sub"
}

func statPathForTest(path string) (os.FileInfo, error) { return os.Stat(path) }

func isNotExistForTest(err error) bool { return os.IsNotExist(err) }
