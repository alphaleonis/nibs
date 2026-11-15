package tui

import (
	"os"
	"testing"
)

// TestMain makes every App a test builds see a UTF-8 terminal, whatever the
// host's locale. A test that needs ASCII forces it with forceUTF8.
func TestMain(m *testing.M) {
	terminalUTF8 = func() bool { return true }
	os.Exit(m.Run())
}
