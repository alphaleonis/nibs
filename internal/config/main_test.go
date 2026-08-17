package config

import (
	"os"
	"testing"

	"github.com/alphaleonis/nibs/internal/testskip"
)

// TestMain reports the guards that did not run before this package's binary
// exits; see internal/testskip for why a skip is otherwise invisible.
func TestMain(m *testing.M) {
	code := m.Run()
	testskip.Report()
	os.Exit(code)
}
