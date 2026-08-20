package graph

import (
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/testsupport/vocabtest"
)

// TestComputeProgressKeysOnRoles proves the rollup follows a status's ROLE
// rather than its name. On the declared vocabulary the two keyings agree on
// every input, so the probe reassigns "deferred" — parked, outstanding scope —
// to each settled role and asserts the rollup moves with it: as dropped the
// deferred work must leave the scope entirely, as done it must count.
func TestComputeProgressKeysOnRoles(t *testing.T) {
	statuses := []string{"completed", "deferred"}

	t.Run("a dropped-role status leaves the denominator", func(t *testing.T) {
		vocabtest.WithStatusRole(t, "deferred", config.RoleDropped)
		got := ComputeProgress(statuses)
		if got.Done != 1 || got.Total != 1 || got.Scrapped != 1 || got.Deferred != 0 {
			t.Errorf("ComputeProgress = %+v, want done 1/1 with 1 out of scope and 0 deferred — deferred carries the dropped role", got)
		}
	})

	t.Run("a done-role status counts as done", func(t *testing.T) {
		vocabtest.WithStatusRole(t, "deferred", config.RoleDone)
		got := ComputeProgress(statuses)
		if got.Done != 2 || got.Total != 2 || got.Deferred != 0 {
			t.Errorf("ComputeProgress = %+v, want done 2/2 with 0 deferred — deferred carries the done role", got)
		}
	})
}
