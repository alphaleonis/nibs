// Package progress computes completion for every surface. Its buckets key on
// status roles, never on status names.
package progress

import (
	"math"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/estimate"
	"github.com/alphaleonis/nibs/internal/nib"
)

// Rollup is the child-completion rollup behind the projected `progress` field
// and the context and roadmap views; build it with ByCount. By status role
// (config.StatusRole): done children count in Done and Total, dropped children
// only in Scrapped, and every other child in Total, parked ones also in Deferred.
// Percent is round(Done/Total*100), or 0 when Total is 0. Split on roles: the
// closed and releases-dependents predicates cannot tell done from dropped.
//
// cmd/roadmap.go's filterChildren lists exactly the children counted in Total
// but not in Done; keep the two rules in step.
type Rollup struct {
	Total    int `json:"total"`
	Done     int `json:"done"`
	Percent  int `json:"percent"`
	Scrapped int `json:"scrapped"`
	Deferred int `json:"deferred"`
}

// ByCount builds a Rollup from child status strings. An unknown or empty status
// counts as outstanding.
func ByCount(childStatuses []string) Rollup {
	var r Rollup
	for _, s := range childStatuses {
		role, known := config.StatusRole(s)
		switch {
		case known && role == config.RoleDone:
			r.Total++
			r.Done++
		case known && role == config.RoleDropped:
			r.Scrapped++
		case known && role == config.RoleParked:
			r.Deferred++
			r.Total++
		default:
			r.Total++
		}
	}
	if r.Total > 0 {
		r.Percent = int(math.Round(float64(r.Done) / float64(r.Total) * 100))
	}
	return r
}

// Weighted is estimate-weighted progress. Nothing uses it yet; keep it as the
// work-weighted alternative to Rollup.
type Weighted struct {
	CompletedWeight int     `json:"completed_weight"`
	TotalWeight     int     `json:"total_weight"`
	Percentage      float64 `json:"percentage"`
}

// ByEstimate weighs nibs by estimate with ByCount's buckets: done work is the
// numerator and dropped work leaves the total. It counts every nib it is given,
// containers included, so pass a prepared set such as leaf work only.
func ByEstimate(nibs []*nib.Nib) Weighted {
	var completed, total int
	for _, n := range nibs {
		role, known := config.StatusRole(n.Status)
		if known && role == config.RoleDropped {
			continue
		}
		w := estimate.Weight(n.Estimate)
		total += w
		if known && role == config.RoleDone {
			completed += w
		}
	}
	var pct float64
	if total > 0 {
		pct = float64(completed) / float64(total) * 100
	}
	return Weighted{
		CompletedWeight: completed,
		TotalWeight:     total,
		Percentage:      pct,
	}
}
