package graph

import (
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/membership"
	"github.com/alphaleonis/nibs/internal/nib"
)

// NextReason names a situation the walk could not answer from, or the reason
// it answered from outside a queue. The values are wire vocabulary: `nibs next
// --json` and `nibs context` report them verbatim, so renaming one breaks
// agents that branch on the token.
type NextReason string

const (
	NextReasonNoMilestones NextReason = "no_milestones"
	// Milestones exist, none is in progress (decision 1.4).
	NextReasonNoActiveMilestone NextReason = "no_active_milestone"
	NextReasonEmptyQueue        NextReason = "empty_queue"
	// Every candidate the walk reached was closed, blocked, or not startable.
	// NextTally says which.
	NextReasonNothingStartable NextReason = "nothing_startable"
)

// NextTally counts what a walk declined, so a "nothing to do" answer can say
// WHY rather than only that. Counts are of declines, not of distinct nodes:
// one nib can be counted twice, and under two of the counters.
type NextTally struct {
	// Declines to enter a node whose status is closed.
	Closed int
	// Nodes with nothing open below them, carrying a startable status but held
	// by an active blocker — the same withholding `nibs list --ready` applies.
	Blocked int
	// Nodes with nothing open below them that are open but not startable (a
	// draft, or work already in progress).
	Open int
	// Queue entries passed over as order-vs-dependency inversions (decision
	// 2.3). The pairs themselves are NextResult.Inversions.
	Inverted int
}

// Any reports whether the walk declined anything at all.
func (t NextTally) Any() bool {
	return t.Closed > 0 || t.Blocked > 0 || t.Open > 0 || t.Inverted > 0
}

// NextResult is the answer to "what do I do", with the provenance that reached
// it. Every nib pointer is a LIVE store pointer (see NibReader.GetSnapshot) —
// snapshot a result that outlives the store lock.
type NextResult struct {
	// Milestone is the derived active milestone, or nil when none derives.
	Milestone *nib.Nib
	// Position is the 1-based place in the active milestone's queue of the
	// ENTRY the answer was reached through, not of Action — Action is often
	// deeper in that entry's decomposition and in no queue. 0 on the fallback
	// walk and when there is no Action.
	Position int
	// Action is the nib to work on, or nil when the walk found none.
	Action *nib.Nib
	// Path is how Action was reached: the queue entry (or, on the fallback
	// walk, the root) first, then the descent through containers, ending at
	// Action itself. Nil when there is no Action.
	Path []*nib.Nib
	// FallbackReason is why the answer came from the store-wide walk rather
	// than from a queue; "" when a queue answered (or refused to).
	FallbackReason NextReason
	// NoAnswerReason is why the walk that ran produced no Action; "" when it
	// produced one.
	NoAnswerReason NextReason
	Tally          NextTally
	// Inversions are the queue inversions that caused an Inverted skip.
	Inversions []QueueInversion
}

// activeMilestoneStatus is the status decision 1.4 derives "active" from. A
// literal, not a config-derived group: this picks out ONE status, and no group
// predicate singles one out — "open" holds draft and todo too, and a
// planned-but-unstarted milestone is not active.
const activeMilestoneStatus = "in-progress"

// ActiveMilestone derives the active milestone (decision 1.4): the in-progress
// milestone that comes first in milestone order — the `order:` key milestones
// carry among themselves, NOT a queue key. Several in-progress milestones are
// legal; the earliest wins. Nil when no milestone is in progress.
func ActiveMilestone(view *membership.View) *nib.Nib {
	var active []*nib.Nib
	for _, m := range view.Milestones() {
		if m.Status == activeMilestoneStatus {
			active = append(active, m)
		}
	}
	if len(active) == 0 {
		return nil
	}
	nib.SortByOrder(active)
	return active[0]
}

// Next answers "what do I do": the first startable leaf in the active
// milestone's queue, with the provenance that reached it.
//
// The walk takes the queue in milestone_order and, for each entry, descends its
// decomposition in `order` until it reaches a node with nothing open under it
// (decision 2.4). The first such node that is STARTABLE is the answer.
//
// Startable is `--ready`'s own pair, not a second definition of it: a startable
// status (config.IsStartableStatus) AND no active blocker
// (BlockingChecker.IsBlocked). A deferred blocker therefore withholds work here
// exactly as it does there.
//
// Two prunings, and only two: a CLOSED node is not entered, and a queue entry
// caught in an inversion is passed over with its subtree (decision 2.3), by the
// shared definition in QueueInversionsIn.
//
// With no active milestone — none declared, or none in progress — the same walk
// runs over the store's roots in tree order and FallbackReason says so. Once one
// exists, `next` speaks only for it: an empty queue, or one yielding nothing
// startable, is reported as such rather than routed around.
//
// Next never writes. Read the queue the way this does — enumerate the group,
// sort by the key — not through Orderer.Members, which backfills a missing
// milestone_order onto members as a side effect of being read.
func Next(reader NibReader, blocking BlockingChecker) NextResult {
	w := &nextWalk{
		view:     membership.Compute(reader.All()),
		reader:   reader,
		blocking: blocking,
		cfg:      reader.Config(),
		visited:  make(map[string]bool),
	}

	active := ActiveMilestone(w.view)
	if active == nil {
		w.res.FallbackReason = NextReasonNoMilestones
		if len(w.view.Milestones()) > 0 {
			w.res.FallbackReason = NextReasonNoActiveMilestone
		}
		// The store's roots, in tree order. DirectMembers("") is the root
		// group with the milestone-typed nibs already dropped.
		roots := w.view.DirectMembers("")
		nib.SortByOrder(roots)
		w.walk(roots, false)
		return w.res
	}

	w.res.Milestone = active
	queue := w.view.DirectMembers(active.ID)
	if len(queue) == 0 {
		w.res.NoAnswerReason = NextReasonEmptyQueue
		return w.res
	}
	nib.SortByMilestoneOrder(queue)
	w.indexInversions(active.ID)
	w.walk(queue, true)
	return w.res
}

// nextWalk carries one command's walk state. The membership view is built per
// call and never cached beyond it, per the package's live-pointer discipline.
type nextWalk struct {
	view     *membership.View
	reader   NibReader
	blocking BlockingChecker
	cfg      *config.Config
	// visited bounds the descent over a cyclic parent chain, which the
	// recursion would otherwise follow forever.
	visited map[string]bool
	// aheadOf holds the active queue's inversions, keyed by the entry sitting
	// AHEAD of its blocker — the half that makes queue order unusable for that
	// entry. Nil on the fallback walk, which sits in no queue.
	aheadOf map[string][]QueueInversion
	res     NextResult
}

// indexInversions files the queue's inversions under the entry each one holds
// back.
func (w *nextWalk) indexInversions(milestoneID string) {
	inversions := QueueInversionsIn(w.reader, milestoneID)
	if len(inversions) == 0 {
		return
	}
	w.aheadOf = make(map[string][]QueueInversion, len(inversions))
	for _, inv := range inversions {
		w.aheadOf[inv.Ahead.ID] = append(w.aheadOf[inv.Ahead.ID], inv)
	}
}

// walk runs the entries in the order given, recording the answer on the
// result. isQueue selects the inversion pruning and the Position record, both
// of which apply to queue entries alone.
func (w *nextWalk) walk(entries []*nib.Nib, isQueue bool) {
	for i, e := range entries {
		if w.cfg.IsClosedStatus(e.Status) {
			w.res.Tally.Closed++
			continue
		}
		if isQueue {
			if inversions := w.aheadOf[e.ID]; len(inversions) > 0 {
				w.res.Inversions = append(w.res.Inversions, inversions...)
				w.res.Tally.Inverted++
				continue
			}
		}
		if path := w.descend(e, nil); path != nil {
			w.res.Path = path
			w.res.Action = path[len(path)-1]
			if isQueue {
				w.res.Position = i + 1
			}
			return
		}
	}
	w.res.NoAnswerReason = NextReasonNothingStartable
}

// descend returns the provenance path to the first startable node at or under
// n, or nil when there is none. The caller has already established that n is
// not closed.
func (w *nextWalk) descend(n *nib.Nib, path []*nib.Nib) []*nib.Nib {
	if w.visited[n.ID] {
		return nil
	}
	w.visited[n.ID] = true

	// A fresh slice per step: siblings recurse over the same path prefix, and
	// appending in place would let one sibling's descent overwrite another's.
	path = append(append(make([]*nib.Nib, 0, len(path)+1), path...), n)

	children := w.view.DirectMembers(n.ID)
	nib.SortByOrder(children)
	candidates := make([]*nib.Nib, 0, len(children))
	for _, c := range children {
		if w.cfg.IsClosedStatus(c.Status) {
			w.res.Tally.Closed++
			continue
		}
		if onPath(path, c.ID) {
			// A cyclic parent chain: c is its own ancestor here, so it is not
			// work below n. Not w.visited — an already-visited child that is
			// not on this path is still a child of n, and the entry guard
			// above ends the recursion.
			continue
		}
		candidates = append(candidates, c)
	}

	if len(candidates) == 0 {
		// Nothing open below: a genuine leaf, or the all-children-closed
		// container decision 2.4 makes the action itself.
		if w.startable(n) {
			return path
		}
		if w.cfg.IsStartableStatus(n.Status) {
			// Startable status and still not startable means a blocker holds it.
			w.res.Tally.Blocked++
		} else {
			w.res.Tally.Open++
		}
		return nil
	}

	for _, c := range candidates {
		if p := w.descend(c, path); p != nil {
			return p
		}
	}
	return nil
}

// onPath reports whether id is already a step of the descent that reached here.
func onPath(path []*nib.Nib, id string) bool {
	for _, p := range path {
		if p.ID == id {
			return true
		}
	}
	return false
}

// startable is `nibs list --ready`'s predicate.
func (w *nextWalk) startable(n *nib.Nib) bool {
	return w.cfg.IsStartableStatus(n.Status) && !w.blocking.IsBlocked(n.ID)
}
