package graph

import (
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/membership"
	"github.com/alphaleonis/nibs/internal/nib"
)

// NextReason names a situation the walk could not answer from, or the reason
// it answered from outside a queue. The values are the wire vocabulary `nibs
// next --json` reports, so an agent branches on a token rather than on prose.
type NextReason string

const (
	// NextReasonNoMilestones: the store declares no milestone at all — the
	// day-one flat-list shape the model deliberately supports.
	NextReasonNoMilestones NextReason = "no_milestones"
	// NextReasonNoActiveMilestone: milestones exist, none is in progress, so
	// nothing derives as active (decision 1.4).
	NextReasonNoActiveMilestone NextReason = "no_active_milestone"
	// NextReasonEmptyQueue: the active milestone has no members.
	NextReasonEmptyQueue NextReason = "empty_queue"
	// NextReasonNothingStartable: the walk ran and every candidate it reached
	// was closed, blocked, or open-but-not-startable. NextTally says which.
	NextReasonNothingStartable NextReason = "nothing_startable"
)

// NextTally counts what a walk declined, so a "nothing to do" answer can say
// WHY rather than only that. Each node is counted at most once and only where
// the walk stopped at it: a closed node is counted where the walk declined to
// enter it (its subtree is not counted behind it), and an entered node is
// counted only when it turned out to be a leaf the caller cannot start.
type NextTally struct {
	// Closed: nodes the walk declined to enter because their status is closed.
	Closed int
	// Blocked: leaves carrying a startable status but held by an active
	// blocker — the same withholding `nibs list --ready` applies.
	Blocked int
	// Open: leaves that are open but not startable (a draft, or work already
	// in progress).
	Open int
	// Inverted: queue entries passed over as order-vs-dependency inversions
	// (decision 2.3). The pairs themselves are NextResult.Inversions.
	Inverted int
}

// Any reports whether the walk declined anything at all.
func (t NextTally) Any() bool {
	return t.Closed > 0 || t.Blocked > 0 || t.Open > 0 || t.Inverted > 0
}

// NextResult is the answer to "what do I do", with the provenance that makes
// it checkable. Every nib pointer is a LIVE store pointer (see NibReader.Get);
// a caller whose result outlives the store lock must snapshot them.
type NextResult struct {
	// Milestone is the derived active milestone, or nil when none derives.
	Milestone *nib.Nib
	// Position is the answer's 1-based place in the active milestone's queue,
	// or 0 when the answer did not come from a queue.
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
	// Tally counts what the walk declined on its way (see NextTally).
	Tally NextTally
	// Inversions are the queue inversions that caused an Inverted skip, as
	// QueueInversionsIn reports them.
	Inversions []QueueInversion
}

// activeMilestoneStatus is the status decision 1.4 derives "active" from. It
// is a literal rather than a config-derived group for the reason
// nibcontext.BuildSummaryWithView states about its own selectors: this picks
// out ONE status, and no group predicate singles a status out — "open" holds
// draft and todo too, and a planned-but-unstarted milestone is not active.
const activeMilestoneStatus = "in-progress"

// ActiveMilestone derives the active milestone (decision 1.4): the in-progress
// milestone that comes first in milestone order — the `order:` key milestones
// carry among themselves, NOT a queue key. It is derived per call and never
// stored, so a status change anywhere moves it with no migration and no cache
// to invalidate. Several in-progress milestones are legal; the earliest wins.
// Nil when no milestone is in progress.
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
// milestone's queue, with the provenance that reached it (decision 2.4).
//
// The walk takes the queue in milestone_order and, for each entry, descends
// its decomposition in `order` until it reaches a node with nothing open under
// it — a genuine leaf, or a container whose children are all closed, which
// decision 2.4 makes the action itself. The first such node that is STARTABLE
// is the answer; everything else is passed over and counted (NextTally), so a
// walk that comes up empty can say why.
//
// Startable is `--ready`'s own pair, not a second definition of it: a startable
// status (config.IsStartableStatus) AND no active blocker (BlockingChecker.
// IsBlocked, which is what the --ready filter narrows on). A deferred blocker
// therefore still withholds work here exactly as it does there.
//
// Two prunings, and only two. A CLOSED node is not entered — a completed,
// scrapped or deferred branch is finished or set aside, and offering work from
// under it would contradict the decomposition that says so. A queue entry
// caught in an inversion is passed over with its subtree (decision 2.3), by
// THE shared definition in QueueInversionsIn so `next` and the lint cannot
// drift: the entry sits ahead of a blocker that is itself later in the queue,
// so queue order alone would hand out work whose dependency has not been
// reached. An entry blocked by something EARLIER in the queue needs no
// pruning — the walk offers that blocker's work first and only reaches the
// blocked entry once the blocker has closed.
//
// With no active milestone — none declared, or none in progress — the same
// walk runs over the store's roots in tree order and the result says so
// through FallbackReason. That case is the model's day-one shape (a flat
// ordered list is a complete use of the tool), so refusing to answer there
// would make the verb useless for the simplest project. Once an active
// milestone DOES exist, `next` speaks only for it: a queue that is empty or
// yields nothing startable is reported as such rather than routed around,
// because the honest next action is to change the plan, not to quietly work
// outside it.
//
// Next never writes. It reads the queue the way QueueInversionsIn does
// — enumerate the group, sort by the key — rather than through Orderer.Members,
// which backfills a missing milestone_order onto members as a side effect of
// being read. A question must not edit files the caller never named (and must
// not fail differently on a read-only store), and an unkeyed member still
// sorts deterministically: nib.SortByKey puts keyed members first and orders
// the rest by title.
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
		// group with the milestone-typed nibs already dropped — a milestone is
		// a container, never the work.
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

// nextWalk carries the one-command state the walk threads through itself. The
// membership view is built once here and never cached beyond this call, per
// the package's live-pointer discipline.
type nextWalk struct {
	view     *membership.View
	reader   NibReader
	blocking BlockingChecker
	cfg      *config.Config
	// visited makes the descent total over illegal data: a cyclic parent chain
	// is an adjacency the recursion would otherwise follow forever.
	visited map[string]bool
	// aheadOf holds the active queue's inversions, keyed by the entry sitting
	// AHEAD of its blocker — the half of an inversion that makes queue order
	// unusable for that entry. The other half (a member it blocks sits ahead
	// of it) is the blocker's own problem, not a reason to skip it. Nil on the
	// fallback walk, which runs over roots and so sits in no queue.
	aheadOf map[string][]QueueInversion
	res     NextResult
}

// indexInversions reads the queue's inversions ONCE, by THE shared definition,
// and files them under the entry each one holds back. The scan, sort and
// position map behind them depend on the queue rather than on the entry, so
// asking per entry would repeat identical work for every one of them. The
// index is built once up front, so the walk pays for the queue once however
// far into it the answer lies.
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

// walk runs the entries in the order they were given, recording the answer on
// the result. isQueue selects the inversion pruning, which applies to queue
// entries alone — a nib deeper in a decomposition has no direct assignment, so
// it is in no queue and can be in no inversion.
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
// not closed; a repeat visit is this function's own guard.
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
			// work BELOW n and must not make n look unfinished. Every other
			// already-visited child stays — "nothing open below" is a property
			// of n's children, not of the order the walk happened to reach
			// them in, and the entry guard above still ends the recursion.
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

// onPath reports whether id is already a step of the descent that reached
// here — the only shape in which an open child is not work below its parent.
func onPath(path []*nib.Nib, id string) bool {
	for _, p := range path {
		if p.ID == id {
			return true
		}
	}
	return false
}

// startable is `nibs list --ready`'s predicate, composed from the same two
// halves it composes: a startable status, and no active blocker.
func (w *nextWalk) startable(n *nib.Nib) bool {
	return w.cfg.IsStartableStatus(n.Status) && !w.blocking.IsBlocked(n.ID)
}
