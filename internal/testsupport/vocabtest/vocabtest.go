// Package vocabtest mutates the compiled-in status vocabulary for the duration
// of a test. On the declared vocabulary a status's role and its name always
// agree, so a consumer keying on the name is indistinguishable from one keying
// on the role — reassigning a role is the only observation that can tell the
// two apart, and the prove-the-guard-bites probes on the progress arithmetic
// and the close defaults are built on it.
//
// The mutation is a package-level swap of config.DefaultStatuses, so a caller
// must not run in parallel with tests that read the vocabulary.
package vocabtest

import (
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
)

// WithStatusRole reassigns one declared status's role until the test ends.
func WithStatusRole(t testing.TB, statusName string, role config.Role) {
	t.Helper()
	original := config.DefaultStatuses
	swapped := make([]config.StatusConfig, len(original))
	copy(swapped, original)
	found := false
	for i := range swapped {
		if swapped[i].Name == statusName {
			swapped[i].Role = role
			found = true
		}
	}
	if !found {
		t.Fatalf("vocabtest: no declared status named %q", statusName)
	}
	config.DefaultStatuses = swapped
	t.Cleanup(func() { config.DefaultStatuses = original })
}
