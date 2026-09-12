package nibcore

import (
	"fmt"
	"sort"

	"github.com/alphaleonis/nibs/internal/fsutil"
)

// rewriteAreaAssignmentsLocked rewrites the `area:` of every nib for which
// rewrite returns a replacement, and returns the ids it wrote, in id order.
//
// It is the cascade half of a rename and a retire.
//
// THE WRITE IS THE NON-CREATING ONE (updateOnDiskDeferDirSync), and this is the
// statement RemoveLinksTo's identical sweep defers to. Every target is a nib
// already on disk, so a path this sweep cannot find is stale and the write
// fails. A creating write would answer it by writing the nib back under its
// pre-rename name, leaving the store one file heavier under a prefix its config
// no longer declares.
//
// CONCURRENCY: the caller holds BOTH c.mu and the store's cross-process write
// lock, in that order, for the whole verb — editArea is its only production
// caller and is where that is arranged. Neither is acquired here: c.mu because
// the cascade is only one step of a critical section spanning the plan, the
// cascade, the areas.yml write and the reload; the flock because it is
// per-descriptor (see flock.go).
//
// The span matters, not just the exclusion. The config write that follows the
// cascade is a read-modify-write of the whole `areas:` block; split the two
// across separate critical sections and a concurrent edit of another node is
// lost — last writer wins, both cascades persist, both callers report success,
// and the members stay write-refused with nothing printing a reason to rerun.
//
// IT DOES NOT GO THROUGH Update, and cannot: Update re-checks the `area:` the
// nib will carry against the declared vocabulary, and no vocabulary declares
// both ends of a rename (see Core.ValidateArea). So this path validates nothing
// about areas; the caller makes that judgment, holding the declared tree on both
// sides of the edit.
//
// ORDER IS BY ID, and it is a contract: the caller's partial-failure message
// names the nib that refused, and a loop stopping wherever a Go map happened to
// iterate would name a different one on every run.
//
// The rewrite function is called UNDER c.mu with one nib's stored area, and
// returns (new value, true) to claim it — the empty string being the legal value
// that clears an assignment. It must not call back into Core, which would
// deadlock, and it is asked only about nibs that carry an area at all.
//
// Copy-on-write, per the canonical live-pointer invariant (see
// NibReader.GetSnapshot in internal/graph/interfaces.go): Area is a non-Path
// field, so a changed nib is cloned, written and reinstalled under its key
// rather than edited in place.
func (c *Core) rewriteAreaAssignmentsLocked(rewrite func(area string) (string, bool)) ([]string, error) {
	type target struct {
		id   string
		area string
	}
	var targets []target
	for id, b := range c.nibs {
		if b.Area == "" {
			continue
		}
		next, claimed := rewrite(b.Area)
		if !claimed || next == b.Area {
			continue
		}
		targets = append(targets, target{id: id, area: next})
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].id < targets[j].id })

	// One directory fsync per directory the cascade touched, not one per nib.
	// Deferred so an aborted cascade still flushes what it did write: the first
	// error returns with the earlier files already renamed into place.
	var pending fsutil.DirSyncBatch
	defer pending.Flush()

	written := make([]string, 0, len(targets))
	for _, t := range targets {
		clone := c.nibs[t.id].Clone()
		clone.Area = t.area

		dir, err := c.updateOnDiskDeferDirSync(clone)
		pending.Add(dir)
		if err != nil {
			return written, fmt.Errorf("%s: %w", t.id, err)
		}
		c.nibs[t.id] = clone
		written = append(written, t.id)
	}
	return written, nil
}
