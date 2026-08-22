package graph

import (
	"sort"

	"github.com/alphaleonis/nibs/internal/nib"
)

// QueueInversion is one order-vs-dependency inversion inside a milestone
// queue: Ahead sits earlier in the queue than Blocker, yet Blocker still
// blocks it. Both are live store pointers (see NibReader.Get); a caller whose
// result outlives the store lock must snapshot them.
type QueueInversion struct {
	// Milestone is the resolved id of the queue both nibs sit in.
	Milestone string
	Ahead     *nib.Nib
	Blocker   *nib.Nib
}

// QueueInversionsInvolving is THE definition of a queue inversion (decision
// 2.3), shared by the lint the creating mutations run and by the skip rule of
// `nibs next`. It reports every inversion the nib with id takes part in, on
// either side, or nil when it is in no queue or in none.
//
// A pair (A, B) is an inversion exactly when all four hold:
//
//   - B is in A's blocked_by set, resolved through the reader the way every
//     blocker read resolves (a dangling entry names nothing);
//   - B's status still blocks — config.StatusReleasesDependents is false for
//     it, so a completed or scrapped B is no inversion while a deferred one
//     still is, matching IsBlocked and --ready;
//   - A and B resolve to the SAME milestone (resolvedMilestoneID);
//   - A precedes B in that queue's order — the order `nibs list --milestone`
//     shows, nib.SortByMilestoneOrder over the queue's members.
//
// Inversions are legal: plans state importance, dependencies state
// feasibility. The lint names each pair once, at the write that creates it —
// the CLI snapshots this set for the subject before a queue-shaping write (an
// assignment, a queue move, a new blocked_by or blocking edge) and reports
// only what the write added, so a later move that leaves a pair in place does
// not repeat it; `next` skips them. The subject is reported as A (it sits
// ahead of its blocker) AND as B (a member that it blocks sits ahead of it) —
// the second direction is the one an assignment creates, since an assignment
// appends the subject LAST and so can only put it behind work it blocks, and
// the one a `--blocking` edge creates, since that edge makes the subject the
// blocker.
//
// A read-only mirror of the ordering engine's queue: it enumerates the queue
// the way Orderer's milestone scope does but never backfills a key, so a lint
// before or after a write — or a `next` before one — leaves no durable edit on
// a member the caller never named.
func QueueInversionsInvolving(reader NibReader, id string) []QueueInversion {
	subject, err := reader.Get(id)
	if err != nil {
		return nil
	}
	var out []QueueInversion
	for _, inv := range QueueInversionsIn(reader, resolvedMilestoneID(subject, reader)) {
		if inv.Ahead.ID == subject.ID || inv.Blocker.ID == subject.ID {
			out = append(out, inv)
		}
	}
	return out
}

// QueueInversionsIn reports every inversion in ONE milestone queue, by the
// rule QueueInversionsInvolving documents — it is the same definition read
// whole rather than through a subject, so the two cannot drift. Nil for the
// empty id (a nib in no queue is in no inversion).
//
// resolved is a RESOLVED milestone id — what resolvedMilestoneID returns —
// not a user-supplied one, which is why nothing is normalized here: this is
// below the boundary where an id is turned into a nib, and every caller has
// already crossed it. A short form reaching this far names no queue and
// yields nil.
//
// The scan, the sort and the position map behind an inversion depend on the
// QUEUE alone, so a caller asking about many entries of one queue — `nibs
// next` walking it in order — asks here once instead of re-deriving all three
// per entry.
//
// Pairs come back in queue order: by the position of the entry sitting ahead,
// then by the position of the blocker it sits ahead of. Filtering that to one
// subject yields exactly the order the per-subject view is pinned to, since
// every pair naming the subject as the blocker has an Ahead earlier than the
// subject itself.
func QueueInversionsIn(reader NibReader, resolved string) []QueueInversion {
	if resolved == "" {
		return nil
	}
	var members []*nib.Nib
	for _, b := range reader.All() {
		if resolvedMilestoneID(b, reader) == resolved {
			members = append(members, b)
		}
	}
	nib.SortByMilestoneOrder(members)
	position := make(map[string]int, len(members))
	member := make(map[string]*nib.Nib, len(members))
	for i, b := range members {
		position[b.ID] = i
		member[b.ID] = b
	}
	cfg := reader.Config()

	var out []QueueInversion
	for i, ahead := range members {
		// Read from the blocked_by side rather than by re-scanning the queue
		// for each entry: the set is small, and the queue is not.
		var blockers []*nib.Nib
		seen := make(map[string]bool, len(ahead.BlockedBy))
		for _, raw := range ahead.BlockedBy {
			full, ok := reader.NormalizeID(raw)
			if !ok || seen[full] {
				continue
			}
			seen[full] = true
			// In the same queue, later in it, and still blocking.
			blocker := member[full]
			if blocker == nil || position[full] <= i || cfg.StatusReleasesDependents(blocker.Status) {
				continue
			}
			blockers = append(blockers, blocker)
		}
		sort.Slice(blockers, func(x, y int) bool {
			return position[blockers[x].ID] < position[blockers[y].ID]
		})
		for _, blocker := range blockers {
			out = append(out, QueueInversion{Milestone: resolved, Ahead: ahead, Blocker: blocker})
		}
	}
	return out
}
