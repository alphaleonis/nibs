package nibcore

import (
	"os"
	"testing"

	"github.com/alphaleonis/nibs/internal/testskip"
	"github.com/alphaleonis/nibs/internal/testsupport/tempdirtest"
)

// TestMain runs the tests in a temp dir of their own (see tempdirtest), then
// reports the guards that did not run before this package's binary exits; see
// internal/testskip for why a skip is otherwise invisible.
func TestMain(m *testing.M) {
	code := tempdirtest.Run(m)
	testskip.Report()
	os.Exit(code)
}
