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

func TestAreasYMLScalarsAreStrippedInMessages(t *testing.T) {
	rows := []struct {
		name  string
		areas []AreaConfig
	}{
		{"name with surrounding whitespace", []AreaConfig{{Name: " " + spanBreakingScalar}}},
		{"name holding the separator", []AreaConfig{{Name: spanBreakingScalar + "/b"}}},
		{"duplicate sibling", []AreaConfig{{Name: spanBreakingScalar}, {Name: spanBreakingScalar}}},
		{"unusable color", []AreaConfig{{Name: "web", Color: spanBreakingScalar}}},
		{"fault under a parent", []AreaConfig{{Name: spanBreakingScalar, Children: []AreaConfig{{Name: " x"}}}}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			err := (&Areas{Nodes: row.areas}).Validate()
			if err == nil {
				t.Fatal("Validate() = nil, want a refusal")
			}
			assertScalarStripped(t, "Areas.Validate", err.Error())
		})
	}

	for _, color := range []string{"#q`", "#" + spanBreakingScalar, spanBreakingScalar} {
		err := ValidateAreaColor(color)
		if err == nil {
			t.Fatalf("ValidateAreaColor(%q) = nil, want a refusal", color)
		}
		msg := err.Error()
		if strings.Contains(msg, "q`") || strings.ContainsRune(msg, 'ㅤ') {
			t.Errorf("ValidateAreaColor(%q) echoed the value raw:\n%s", color, msg)
		}
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
