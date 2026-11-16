package nibcore

import (
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
)

// spanBreakingScalar carries the two runes %q passes through: a backtick, which
// closes a code span the message opens, and U+3164, which renders as blank.
const spanBreakingScalar = "q`ㅤz"

func assertScalarStripped(t *testing.T, surface, msg string) {
	t.Helper()
	if strings.Contains(msg, "q`") || strings.ContainsRune(msg, 'ㅤ') {
		t.Errorf("%s echoed the scalar raw:\n%s", surface, msg)
	}
	if !strings.Contains(msg, "q  z") {
		t.Errorf("%s did not echo the scalar in its stripped form:\n%s", surface, msg)
	}
}

func TestFileSourcedScalarsAreStrippedInMessages(t *testing.T) {
	core, _ := setupCoreWithStoredConfig(t, "nibs:\n  prefix: tnib-\n")

	for name, b := range map[string]*nib.Nib{
		"type":     {Type: spanBreakingScalar},
		"status":   {Status: spanBreakingScalar},
		"priority": {Priority: spanBreakingScalar},
		"estimate": {Estimate: spanBreakingScalar},
	} {
		err := core.ValidateEnums(b)
		if err == nil {
			t.Fatalf("ValidateEnums accepted a %s of %q", name, spanBreakingScalar)
		}
		assertScalarStripped(t, "ValidateEnums "+name, err.Error())
	}

	assertScalarStripped(t, "StoreRePrefixedError",
		(&StoreRePrefixedError{Loaded: spanBreakingScalar, Declared: spanBreakingScalar}).Error())
	assertScalarStripped(t, "IDExistsError", (&IDExistsError{ID: spanBreakingScalar}).Error())
}
