package graph

import "github.com/alphaleonis/nibs/internal/nib"

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
	msID := resolvedMilestoneID(subject, reader)
	if msID == "" {
		return nil
	}

	var members []*nib.Nib
	for _, b := range reader.All() {
		if resolvedMilestoneID(b, reader) == msID {
			members = append(members, b)
		}
	}
	nib.SortByMilestoneOrder(members)
	position := make(map[string]int, len(members))
	for i, b := range members {
		position[b.ID] = i
	}
	subjectPos, ok := position[subject.ID]
	if !ok {
		return nil
	}

	stillBlocks := func(b *nib.Nib) bool {
		return !reader.Config().StatusReleasesDependents(b.Status)
	}
	// blockedBy reports whether blocker is in b's blocked_by set, resolving
	// each stored entry the way every blocker read does.
	blockedBy := func(b *nib.Nib, blockerID string) bool {
		for _, raw := range b.BlockedBy {
			if full, ok := reader.NormalizeID(raw); ok && full == blockerID {
				return true
			}
		}
		return false
	}

	var out []QueueInversion
	for _, m := range members {
		if m.ID == subject.ID {
			continue
		}
		switch {
		case position[m.ID] > subjectPos && blockedBy(subject, m.ID) && stillBlocks(m):
			out = append(out, QueueInversion{Milestone: msID, Ahead: subject, Blocker: m})
		case position[m.ID] < subjectPos && blockedBy(m, subject.ID) && stillBlocks(subject):
			out = append(out, QueueInversion{Milestone: msID, Ahead: m, Blocker: subject})
		}
	}
	return out
}
