package progress

import (
	"math"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/testsupport/vocabtest"
)

// TestByEstimate pins the weighted arithmetic even though nothing calls it yet.
// Preserving a variant for a future consumer (nibs-yl9e) is only preservation if
// the variant still works when that consumer arrives: unexecuted code rots
// silently, and this one was well covered before it moved here.
//
// The two cases about which TYPES count are deliberately not carried over — set
// selection is the caller's now, so those assertions belong to whoever prepares
// the set.
func TestByEstimate(t *testing.T) {
	tests := []struct {
		name           string
		nibs           []*nib.Nib
		wantCompleted  int
		wantTotal      int
		wantPercentage float64
	}{
		{
			name:           "empty list",
			nibs:           nil,
			wantCompleted:  0,
			wantTotal:      0,
			wantPercentage: 0,
		},
		{
			name: "all completed",
			nibs: []*nib.Nib{
				{ID: "a", Type: "task", Status: "completed", Estimate: "s"}, // 1
				{ID: "b", Type: "task", Status: "completed", Estimate: "m"}, // 3
			},
			wantCompleted:  4,
			wantTotal:      4,
			wantPercentage: 100,
		},
		{
			name: "mixed statuses",
			nibs: []*nib.Nib{
				{ID: "a", Type: "task", Status: "completed", Estimate: "xl"},     // 8
				{ID: "b", Type: "feature", Status: "in-progress", Estimate: "l"}, // 5
				{ID: "c", Type: "bug", Status: "todo", Estimate: "s"},            // 1
			},
			wantCompleted:  8,
			wantTotal:      14,
			wantPercentage: 57.14285714285714,
		},
		{
			name: "unestimated defaults to M weight",
			nibs: []*nib.Nib{
				{ID: "a", Type: "task", Status: "completed"}, // default 3
				{ID: "b", Type: "task", Status: "todo"},      // default 3
			},
			wantCompleted:  3,
			wantTotal:      6,
			wantPercentage: 50,
		},
		{
			name: "scrapped excluded from total",
			nibs: []*nib.Nib{
				{ID: "a", Type: "task", Status: "completed", Estimate: "m"}, // 3
				{ID: "b", Type: "task", Status: "scrapped", Estimate: "xl"}, // excluded
				{ID: "c", Type: "task", Status: "todo", Estimate: "m"},      // 3
			},
			wantCompleted:  3,
			wantTotal:      6,
			wantPercentage: 50,
		},
		{
			// deferred is closed, but the work is coming back — set-aside scope
			// weighs on the denominator exactly like open work, so the estimate
			// of a set-aside nib is still counted as outstanding.
			name: "deferred counts toward total, not toward completed",
			nibs: []*nib.Nib{
				{ID: "a", Type: "task", Status: "completed", Estimate: "m"}, // 3
				{ID: "b", Type: "task", Status: "deferred", Estimate: "xl"}, // 8, undone
				{ID: "c", Type: "task", Status: "todo", Estimate: "m"},      // 3
			},
			wantCompleted:  3,
			wantTotal:      14,
			wantPercentage: 3.0 / 14.0 * 100,
		},
		{
			name: "a deferred nib holds the set below 100%",
			nibs: []*nib.Nib{
				{ID: "a", Type: "task", Status: "completed", Estimate: "m"}, // 3
				{ID: "b", Type: "task", Status: "deferred", Estimate: "m"},  // 3, undone
			},
			wantCompleted:  3,
			wantTotal:      6,
			wantPercentage: 50,
		},
		{
			// Only scrapped work leaves the denominator, so a set whose
			// remaining nibs are all scrapped does reach 100%.
			name: "scrapped remainder reaches 100%",
			nibs: []*nib.Nib{
				{ID: "a", Type: "task", Status: "completed", Estimate: "m"}, // 3
				{ID: "b", Type: "task", Status: "scrapped", Estimate: "m"},  // excluded
			},
			wantCompleted:  3,
			wantTotal:      3,
			wantPercentage: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ByEstimate(tt.nibs)
			if got.CompletedWeight != tt.wantCompleted {
				t.Errorf("CompletedWeight = %d, want %d", got.CompletedWeight, tt.wantCompleted)
			}
			if got.TotalWeight != tt.wantTotal {
				t.Errorf("TotalWeight = %d, want %d", got.TotalWeight, tt.wantTotal)
			}
			if math.Abs(got.Percentage-tt.wantPercentage) > 1e-9 {
				t.Errorf("Percentage = %f, want %f", got.Percentage, tt.wantPercentage)
			}
		})
	}
}

// TestByEstimateKeysOnRoles proves the arithmetic follows a status's ROLE
// rather than its name. On the declared vocabulary the two keyings agree on
// every input, so the probe reassigns "deferred" — parked, in the denominator
// — to each settled role and asserts the arithmetic moves with it: as dropped
// the deferred work must leave the scope, as done it must count toward it.
func TestByEstimateKeysOnRoles(t *testing.T) {
	nibs := []*nib.Nib{
		{ID: "a", Type: "task", Status: "completed", Estimate: "s"}, // 1
		{ID: "b", Type: "task", Status: "deferred", Estimate: "m"},  // 3
	}

	t.Run("a dropped-role status leaves the denominator", func(t *testing.T) {
		vocabtest.WithStatusRole(t, "deferred", config.RoleDropped)
		got := ByEstimate(nibs)
		if got.CompletedWeight != 1 || got.TotalWeight != 1 {
			t.Errorf("ByEstimate = %d/%d, want 1/1 — deferred carries the dropped role, so its weight must leave the scope", got.CompletedWeight, got.TotalWeight)
		}
	})

	t.Run("a done-role status counts as completed", func(t *testing.T) {
		vocabtest.WithStatusRole(t, "deferred", config.RoleDone)
		got := ByEstimate(nibs)
		if got.CompletedWeight != 4 || got.TotalWeight != 4 {
			t.Errorf("ByEstimate = %d/%d, want 4/4 — deferred carries the done role, so its weight must count as completed", got.CompletedWeight, got.TotalWeight)
		}
	})
}
