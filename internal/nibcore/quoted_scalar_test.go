package nibcore

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/store"
	"github.com/alphaleonis/nibs/internal/testskip"
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

// The warn writer is a safetext.Writer, which keeps backticks, so the id has to
// be stripped where the warning is built.
func TestDuplicateIDLoadWarningStripsTheID(t *testing.T) {
	core, _ := setupCoreWithStoredConfig(t, "nibs:\n  prefix: tnib-\n")
	dataDir := store.NewLayout(core.Root()).DataDir()
	const content = "---\nversion: 2\ntitle: One\nstatus: todo\n---\n\nBody.\n"
	for _, name := range []string{spanBreakingScalar + "--a.md", spanBreakingScalar + "--b.md"} {
		if err := os.WriteFile(filepath.Join(dataDir, name), []byte(content), 0o644); err != nil {
			testskip.Unavailable(t, testskip.HostileFilenames, "os.WriteFile(%q): %v", name, err)
			return
		}
	}
	var warn bytes.Buffer
	core.SetWarnWriter(&warn)
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(warn.String(), "duplicate nib id") {
		t.Fatalf("no duplicate-id warning, so this test asserts nothing:\n%s", warn.String())
	}
	// The file paths beside the id are unquoted and keep their backtick through
	// the Writer, so only the quoted id is asserted on.
	got := warn.String()
	if strings.Contains(got, "\"q`") || !strings.Contains(got, "\"q  z\"") {
		t.Errorf("duplicate-id warning did not quote the id in its stripped form:\n%s", got)
	}
}
