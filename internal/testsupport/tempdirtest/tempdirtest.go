// Package tempdirtest gives a test binary a temp directory of its own, removed
// when the binary's tests finish. A test panic or timeout ends the process first
// and leaves it behind. A package whose tests open stores needs it, because nibs
// keeps one lock file per store under the temp dir and never deletes it.
package tempdirtest

import (
	"fmt"
	"os"
	"runtime"
	"testing"
)

// Run points os.TempDir at a fresh directory, runs m, and removes the
// directory. It returns the exit code for os.Exit: m's own, or 1 when the
// directory could not be created or removed. Child processes the tests start
// inherit the redirect.
func Run(m *testing.M) int {
	// The prefix is short because some tests assert on paths cut to a length bound.
	dir, err := os.MkdirTemp("", "nt")
	if err != nil {
		fmt.Fprintf(os.Stderr, "tempdirtest: %v\n", err)
		return 1
	}
	for _, name := range tempDirVars() {
		if err := os.Setenv(name, dir); err != nil {
			fmt.Fprintf(os.Stderr, "tempdirtest: %v\n", err)
			_ = os.RemoveAll(dir)
			return 1
		}
	}

	code := m.Run()

	if err := os.RemoveAll(dir); err != nil {
		fmt.Fprintf(os.Stderr, "tempdirtest: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	return code
}

// tempDirVars names the variables os.TempDir reads first on this OS.
func tempDirVars() []string {
	if runtime.GOOS == "windows" {
		return []string{"TMP", "TEMP"}
	}
	return []string{"TMPDIR"}
}
