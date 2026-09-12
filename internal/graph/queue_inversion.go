package graph

import (
	"sort"

	"github.com/alphaleonis/nibs/internal/nib"
)

// QueueInversion is one order-vs-dependency inversion inside a milestone queue:
// Ahead sits earlier in the queue than Blocker, yet Blocker still blocks it.
// Both are live store pointers (see NibReader.Get) — snapshot them if the result
// outlives the store lock.
type QueueInversion struct {
	// Milestone is the resolved id of the queue both nibs sit in.
	Milestone string
	Ahead     *nib.Nib
	Blocker   *nib.Nib
}

// QueueInversionsInvolving reports every inversion the nib with id takes part
// in, on either side, or nil when it is in no queue or in none. The subject
// appears as Ahead (it sits ahead of its blocker) and as Blocker (a member it
// blocks sits ahead of it).
//
// A pair (A, B) is an inversion exactly when all four hold:
//
//   - B is in A's blocked_by set, resolved through the reader (a dangling
//     entry names nothing);
//   - B's status still blocks — config.StatusReleasesDependents is false for
//     it, so a completed or scrapped B is no inversion while a deferred one
//     still is, matching IsBlocked and --ready;
//   - A and B resolve to the SAME milestone (resolvedMilestoneID);
//   - A precedes B in that queue's order (nib.SortByMilestoneOrder over the
//     queue's members).
//
// Read-only: it enumerates the queue the way Orderer's milestone scope does,
// but never backfills a key, so a lint leaves no durable edit.
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

// QueueInversionsIn reports every inversion in ONE milestone queue, by the rule
// QueueInversionsInvolving documents. Nil for the empty id.
//
// resolved is a RESOLVED milestone id — what resolvedMilestoneID returns.
// Nothing is normalized here, so a short form names no queue and yields nil.
//
// The scan, the sort and the position map depend on the QUEUE alone: a caller
// walking many entries of one queue asks here once rather than per entry.
//
// Pairs come back in queue order: by the position of the entry sitting ahead,
// then by the position of the blocker it sits ahead of.
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
