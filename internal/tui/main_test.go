package tui

import (
	"os"
	"testing"

	"github.com/alphaleonis/nibs/internal/testsupport/tempdirtest"
)

// TestMain keeps the lock files of the stores these tests open out of the
// system temp dir; see tempdirtest.
func TestMain(m *testing.M) {
	os.Exit(tempdirtest.Run(m))
}
