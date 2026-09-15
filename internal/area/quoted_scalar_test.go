package area

import (
	"strings"
	"testing"
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
		nodes []Node
	}{
		{"name with surrounding whitespace", []Node{{Name: " " + spanBreakingScalar}}},
		{"name holding the separator", []Node{{Name: spanBreakingScalar + "/b"}}},
		{"duplicate sibling", []Node{{Name: spanBreakingScalar}, {Name: spanBreakingScalar}}},
		{"unusable color", []Node{{Name: "web", Color: spanBreakingScalar}}},
		{"fault under a parent", []Node{{Name: spanBreakingScalar, Children: []Node{{Name: " x"}}}}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			err := (&Vocabulary{Nodes: row.nodes}).Validate()
			if err == nil {
				t.Fatal("Validate() = nil, want a refusal")
			}
			assertScalarStripped(t, "Vocabulary.Validate", err.Error())
		})
	}

	for _, color := range []string{"#q`", "#" + spanBreakingScalar, spanBreakingScalar} {
		err := ValidateColor(color)
		if err == nil {
			t.Fatalf("ValidateColor(%q) = nil, want a refusal", color)
		}
		msg := err.Error()
		if strings.Contains(msg, "q`") || strings.ContainsRune(msg, 'ㅤ') {
			t.Errorf("ValidateColor(%q) echoed the value raw:\n%s", color, msg)
		}
	}
}
