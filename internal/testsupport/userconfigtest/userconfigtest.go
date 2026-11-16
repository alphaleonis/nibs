// Package userconfigtest points the OS user config directory at a temp
// directory for the duration of a test, so a test never reads or writes the
// real user config.
//
// The redirect is a t.Setenv, so a caller cannot run in parallel.
package userconfigtest

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Isolate points os.UserConfigDir at a fresh temp directory until the test
// ends, and returns that directory.
func Isolate(t testing.TB) string {
	t.Helper()
	dir := t.TempDir()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("AppData", dir)
	case "darwin", "ios":
		t.Setenv("HOME", dir)
		dir = filepath.Join(dir, "Library", "Application Support")
	default:
		t.Setenv("XDG_CONFIG_HOME", dir)
	}
	if got, err := os.UserConfigDir(); err != nil || got != dir {
		t.Fatalf("os.UserConfigDir() = %q, %v, want %q", got, err, dir)
	}
	return dir
}

// WriteUserConfig isolates the user config directory as Isolate does, writes
// text there as the nibs user config, and returns the file's path.
func WriteUserConfig(t testing.TB, text string) string {
	t.Helper()
	path := filepath.Join(Isolate(t), "nibs", "nibs.yml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
