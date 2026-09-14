package graph

import (
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
)

// spanBreakingScalar carries the two runes %q passes through: a backtick, which
// closes a code span the message opens, and U+3164, which renders as blank.
const spanBreakingScalar = "q`ㅤz"

func TestDescribeParentStripsTheStoredParent(t *testing.T) {
	for _, resolved := range []string{spanBreakingScalar, "tnib-0001"} {
		msg := describeParent(&nib.Nib{Parent: spanBreakingScalar}, resolved)
		if strings.Contains(msg, "q`") || strings.ContainsRune(msg, 'ㅤ') {
			t.Errorf("describeParent echoed the stored parent raw: %s", msg)
		}
		if !strings.Contains(msg, "q  z") {
			t.Errorf("describeParent did not echo the stored parent in its stripped form: %s", msg)
		}
	}
}
