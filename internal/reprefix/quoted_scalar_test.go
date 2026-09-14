package reprefix

import (
	"strings"
	"testing"
)

// spanBreakingScalar carries the two runes %q passes through: a backtick, which
// closes a code span the message opens, and U+3164, which renders as blank.
const spanBreakingScalar = "q`ㅤz"

func TestBuildPlanStripsFileDerivedIDsInRefusals(t *testing.T) {
	rows := []struct {
		name     string
		snapshot []NibSnapshot
	}{
		{"id without the old prefix", []NibSnapshot{{ID: spanBreakingScalar, Path: spanBreakingScalar + ".md"}}},
		{"basename not starting with the id", []NibSnapshot{{ID: "nibs-" + spanBreakingScalar, Path: "data/other.md"}}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			_, err := BuildPlan(row.snapshot, "nibs-", "new-", stubExists)
			if err == nil {
				t.Fatal("BuildPlan accepted an inconsistent snapshot")
			}
			msg := err.Error()
			if strings.Contains(msg, "q`") || strings.ContainsRune(msg, 'ㅤ') {
				t.Errorf("the refusal echoed the id raw:\n%s", msg)
			}
			if !strings.Contains(msg, "q  z") {
				t.Errorf("the refusal did not echo the id in its stripped form:\n%s", msg)
			}
		})
	}
}
