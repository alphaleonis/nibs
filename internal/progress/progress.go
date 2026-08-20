// Package progress holds the arithmetic every surface reports completion with.
//
// It exists because there were two of them, in two packages, disagreeing about
// what "done" means: a child-count rollup serving the web projection, the
// roadmap and `nibs context`, and an estimate-weighted calculation computed on
// the CLI context path and then discarded by the caller. One module, so a third
// consumer adds a variant rather than a third arithmetic.
//
// The buckets key on status ROLES, never on literal status names, so a
// vocabulary change reaches every surface at once.
package progress

import (
	"math"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/estimate"
	"github.com/alphaleonis/nibs/internal/nib"
)

// Rollup is the value projected for the computed `progress` field, and
// the canonical child-completion rollup the recipe views (context, roadmap)
// reuse, so `nibs get <id> -f progress` and those views report the same number.
// Build it only via ComputeProgress — do not fork the rule.
//
// Canonical definition (single source of truth). Each child falls into exactly
// one of three buckets, keyed on its status's ROLE (config.StatusRole):
//
//   - Done    = children whose status carries the done role ("completed") — the
//     work actually happened. They also count toward Total.
//
//   - Dropped = children whose status carries the dropped role ("scrapped").
//     The work will not be done and is no longer scope, so it leaves the
//     denominator entirely rather than pinning the percentage below 100
//     forever.
//
//   - Pending = every other child, including the parked role ("deferred").
//     Counts toward Total, not toward Done. Parked work is set aside, not
//     resolved — it is coming back, so it is outstanding scope and the
//     percentage must say so.
//
//   - Total    = Done + Pending; only dropped children are excluded.
//
//   - Percent  = round(Done/Total*100); 0 when Total == 0.
//
//   - Scrapped = direct children in the dropped role, disclosed so the
//     children missing from Total are visible rather than silently dropped.
//
//   - Deferred = direct children in the parked role, disclosed so a
//     set-aside child inside Total can be told apart from work still in flight.
//
// The three closed statuses get three different treatments, and the roles are
// what tell them apart: no combination of the closed/releases-dependents
// predicates separates completed from scrapped (both are closed and both
// release their dependents) — the done/dropped role split exists exactly for
// this rule. See config.Role.
//
// A leaf nib (no children) reports zeros across the board: progress is a rollup
// over children, not a reflection of the nib's own status.
//
// This rule is one half of a seam with the roadmap's item filter
// (cmd/roadmap.go filterChildren): the direct children a default `nibs roadmap`
// lists under a container are exactly the ones this rollup counts in Total but
// not in Done, so a container cannot both list items and claim 100%. The rollup
// is over direct children only, so a milestone can sit at 100% while an epic
// below it still lists a deferred task under a closed parent.
type Rollup struct {
	Total    int `json:"total"`
	Done     int `json:"done"`
	Percent  int `json:"percent"`
	Scrapped int `json:"scrapped"`
	Deferred int `json:"deferred"`
}

// ByCount builds the canonical Rollup from a set of child
// status strings. It is the single place the done/total/percent rule lives; the
// projected `progress` field and every recipe view call it, so the rollup is
// identical everywhere. The buckets key on status ROLES, not names — see
// Rollup for the exact definition.
//
// An unrecognized status (including the empty status of a nib whose front
// matter omits it) lands in the default arm and counts as outstanding scope, so
// a typo holds the percentage down rather than inflating it.
func ByCount(childStatuses []string) Rollup {
	var r Rollup
	for _, s := range childStatuses {
		role, known := config.StatusRole(s)
		switch {
		case known && role == config.RoleDone:
			r.Total++
			r.Done++
		case known && role == config.RoleDropped:
			// Not scope any more — out of the denominator entirely.
			r.Scrapped++
		case known && role == config.RoleParked:
			// Set aside, not resolved: still scope, still not done.
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

// Weighted holds estimate-weighted progress metrics for a set of nibs.
//
// KEPT ALONGSIDE Rollup DELIBERATELY, though nothing computes it today: the two
// answer different questions — "how much of the work is done" versus "how many
// of the items are done" — and which one a surface wants is a decision the
// milestone-progress spec (nibs-yl9e) makes per surface, not one this module
// should make for it by deleting a variant.
type Weighted struct {
	CompletedWeight int     `json:"completed_weight"`
	TotalWeight     int     `json:"total_weight"`
	Percentage      float64 `json:"percentage"`
}

// ByEstimate computes estimate-weighted progress across a set of nibs.
//
// It weighs WHAT IT IS GIVEN. Selecting the set — leaf work, a container's
// members, a subtree — is the caller's, exactly as it is for ByCount, which
// takes prepared statuses. Its predecessor filtered to leaf types itself with a
// literal type list; two of its three callers were already passing filtered sets,
// and a module that knows which types count is a module that has to be edited
// when the hierarchy changes.
// Only leaf work types (task, bug, feature) count — epics and milestones are excluded.
// It applies the same three-way rule as graph.ComputeProgress, weighted by
// estimate and keyed on status ROLES: done-role work is the numerator,
// dropped-role work is no longer scope and leaves the denominator, and
// everything else counts toward the denominator without counting as done.
// Deferred nibs are in that last group — parked work is coming back, so it is
// outstanding scope. Draft nibs are there too: planned scope that hasn't been
// refined yet, as is any status outside the vocabulary, so a typo holds the
// percentage down rather than inflating it.
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
