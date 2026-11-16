package tempdirtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// systemTempDir is os.TempDir before Run redirects it.
var systemTempDir string

func TestMain(m *testing.M) {
	systemTempDir = os.TempDir()
	os.Exit(Run(m))
}

func TestRunRedirectsTheTempDir(t *testing.T) {
	got := os.TempDir()
	if filepath.Dir(got) != filepath.Clean(systemTempDir) || !strings.HasPrefix(filepath.Base(got), "nt") {
		t.Fatalf("os.TempDir() = %s, want an nt directory in %s", got, systemTempDir)
	}
	if info, err := os.Stat(got); err != nil || !info.IsDir() {
		t.Fatalf("the redirected temp dir is not a directory: %v", err)
	}
}
