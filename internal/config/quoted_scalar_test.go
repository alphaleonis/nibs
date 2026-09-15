package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/store"
)

// spanBreakingScalar carries the two runes %q passes through: a backtick, which
// closes a code span the message opens, and U+3164, which renders as blank.
const spanBreakingScalar = "q`ㅤz"

// assertScalarStripped fails unless msg echoes spanBreakingScalar in the form
// safetext.Strip renders it, which rules out a message that dropped the value.
func assertScalarStripped(t *testing.T, surface, msg string) {
	t.Helper()
	if strings.Contains(msg, "q`") || strings.ContainsRune(msg, 'ㅤ') {
		t.Errorf("%s echoed the scalar raw:\n%s", surface, msg)
	}
	if !strings.Contains(msg, "q  z") {
		t.Errorf("%s did not echo the scalar in its stripped form:\n%s", surface, msg)
	}
}

func TestLoadStripsTheRetiredNibsPathValue(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), store.ConfigFileName)
	body := "nibs:\n  path: \"" + spanBreakingScalar + "\"\n  prefix: test-\n"
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(configPath)
	if err == nil {
		t.Fatal("Load() = nil, want the retired nibs.path refusal")
	}
	assertScalarStripped(t, "Load", err.Error())
}
