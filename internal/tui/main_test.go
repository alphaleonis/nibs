package tui

import (
	"flag"
	"os"
	"runtime"
	"testing"

	"github.com/alphaleonis/nibs/internal/testskip"
)

// TestMain makes every App a test builds see a UTF-8 terminal, whatever the
// host's locale. A test that needs ASCII forces it with forceUTF8.
//
// It also points the user state directory at a temp directory, so a session
// that a test starts without isolateStateDir never reads or writes the real
// state files. Before it exits it reports the guards that did not run; see
// internal/testskip.
func TestMain(m *testing.M) {
	// A bad flag exits here, before there is a temp directory to leave behind.
	flag.Parse()
	terminalUTF8 = func() bool { return true }
	stateDir, err := os.MkdirTemp("", "nibs-tui-state-")
	if err != nil {
		panic(err)
	}
	name, _ := stateDirVar(runtime.GOOS)
	if err := os.Setenv(name, stateDir); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(stateDir)
	testskip.Report()
	os.Exit(code)
}
