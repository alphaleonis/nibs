package nibcontext

import (
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/testsupport/vocabtest"
)

// TestCalcProgressKeysOnRoles proves the arithmetic follows a status's ROLE
// rather than its name. On the declared vocabulary the two keyings agree on
// every input, so the probe reassigns "deferred" — parked, in the denominator
// — to each settled role and asserts the arithmetic moves with it: as dropped
// the deferred work must leave the scope, as done it must count toward it.
func TestCalcProgressKeysOnRoles(t *testing.T) {
	nibs := []*nib.Nib{
		{ID: "a", Type: "task", Status: "completed", Estimate: "s"}, // 1
		{ID: "b", Type: "task", Status: "deferred", Estimate: "m"},  // 3
	}

	t.Run("a dropped-role status leaves the denominator", func(t *testing.T) {
		vocabtest.WithStatusRole(t, "deferred", config.RoleDropped)
		got := CalcProgress(nibs)
		if got.CompletedWeight != 1 || got.TotalWeight != 1 {
			t.Errorf("CalcProgress = %d/%d, want 1/1 — deferred carries the dropped role, so its weight must leave the scope", got.CompletedWeight, got.TotalWeight)
		}
	})

	t.Run("a done-role status counts as completed", func(t *testing.T) {
		vocabtest.WithStatusRole(t, "deferred", config.RoleDone)
		got := CalcProgress(nibs)
		if got.CompletedWeight != 4 || got.TotalWeight != 4 {
			t.Errorf("CalcProgress = %d/%d, want 4/4 — deferred carries the done role, so its weight must count as completed", got.CompletedWeight, got.TotalWeight)
		}
	})
}
