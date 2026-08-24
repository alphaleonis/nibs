package graph

import (
	"context"
	"fmt"
	"strings"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/membership"
	"github.com/alphaleonis/nibs/internal/nib"
)

// QueueNameLimit caps how many ids a queue refusal enumerates before it
// switches to a count.
//
// `close`'s sibling guard (incomplete children) names them all, but a milestone
// queue is a project-sized set rather than one container's children, and a
// refusal that scrolls off the screen stops naming anything usefully. The count
// that follows keeps the message honest about the size.
//
// It lives here rather than in cmd because BOTH refusals below it are the same
// refusal seen from two surfaces (see MilestoneQueueOpenError), and a limit that
// differed between them would make the CLI and the wire disagree about how much
// of one set they are willing to name.
const QueueNameLimit = 5

// OpenQueueEntries is THE definition of a milestone's OPEN queue (decision
// 1.5): the ids of its direct assignees whose status is not closed, in queue
// order, so a set that moves elsewhere arrives there in the order it left.
//
// One definition, two callers, deliberately: `nibs close`'s gate
// (cmd/close_queue.go) and updateNib's backstop (refuseClosingFullQueue). A
// second copy of the predicate is exactly how the verb-local rule and the model
// invariant drift back apart.
//
// "Open" is the ordinary role vocabulary every other surface reads
// (config.IsClosedStatus): a deferred MEMBER is closed and does not hold the
// milestone open. The two readings of "deferred" in decision 1.5 are about
// different nibs — the milestone's own close reason, and its members' statuses.
//
// The set is the milestone's QUEUE — its DIRECT assignees — not the transitive
// closure beneath them. That is the set the CLI's two escapes can act on: an
// assignment is what --unassign-open clears and what --move-open-to rewrites,
// and work belonging to the milestone only through an assigned ancestor has no
// assignment of its own to dispose of. A refusal reading a wider set than its
// own remedies could clear would be unanswerable — on the wire just as much as
// on the command line, where the remedy is the same two mutations spelled by
// hand.
//
// view pins live store pointers (see internal/membership's discipline); only
// ids are read out of it here, so nothing survives the call.
func OpenQueueEntries(view *membership.View, milestoneID string, cfg *config.Config) []string {
	var open []*nib.Nib
	for _, m := range view.DirectMembers(milestoneID) {
		if !cfg.IsClosedStatus(m.Status) {
			open = append(open, m)
		}
	}
	nib.SortByMilestoneOrder(open)
	ids := make([]string, len(open))
	for i, b := range open {
		ids[i] = b.ID
	}
	return ids
}

// MilestoneRetypeError refuses to strip milestone-hood from a nib that still
// holds assignments. Every member's `milestone:` would keep naming it, conferring
// no membership and reading back a target of the wrong type — the state
// nibcore's InvalidMilestoneTarget finding reports. Unlike the close gate this
// counts EVERY assignee, open or closed: a closed member's assignment is still a
// link this change would invalidate.
type MilestoneRetypeError struct {
	MilestoneID string
	NewType     string
	Held        []string
}

func (e *MilestoneRetypeError) Error() string {
	named := e.Held
	more := ""
	if len(named) > QueueNameLimit {
		named = named[:QueueNameLimit]
		more = fmt.Sprintf(", and %d more", len(e.Held)-QueueNameLimit)
	}
	subject := fmt.Sprintf("%d nibs are", len(e.Held))
	if len(e.Held) == 1 {
		subject = "1 nib is"
	}
	return fmt.Sprintf("cannot change milestone %s to %s: %s still assigned to it (%s%s), and the assignment would name a nib that is no longer a milestone — clear the assignments first, or leave the type alone",
		e.MilestoneID, e.NewType, subject, strings.Join(named, ", "), more)
}

// memberIDs reads the ids out of a member set, so no store pointer outlives the
// view it came from (internal/membership's discipline).
func memberIDs(members []*nib.Nib) []string {
	ids := make([]string, len(members))
	for i, m := range members {
		ids[i] = m.ID
	}
	return ids
}

// MilestoneQueueOpenError is decision 1.5's refusal at the model boundary: a
// milestone may not take a status that RELEASES its dependents while open work
// is still assigned to its queue, because that would leave the work planned for
// a wave that has finished.
//
// It is the BACKSTOP, not the primary refusal. `nibs close` gates first and
// keeps its own message, which can name the flags that answer it
// (--move-open-to / --unassign-open); this one is reached by every OTHER client
// — the web status dropdown, the TUI status picker, `nibs graphql` — none of
// which has those flags. So the message names the CAPABILITY instead: reassign
// the open work or clear its assignments, which on the wire is one
// updateNib(milestone:) per member and is what the flags batch.
//
// Holding reasons come from the role vocabulary rather than from a status name
// spelled here, so a project declaring none gets no clause rather than advice
// it cannot follow. They are resolved at construction so Error() stays a pure
// rendering of the value.
//
// It deliberately carries no Unwrap, matching MilestoneExclusivityError: there
// is no cause underneath, and mutationErrCode's trailing nib.ErrNotFound test
// must not be able to claim it. With no class of its own there it stays
// validation-class — exit 2, the same class `nibs close`'s own refusal reports.
type MilestoneQueueOpenError struct {
	MilestoneID string   // the milestone being closed
	Status      string   // the releasing status it was being closed as
	Open        []string // its open queue entries, in queue order
	Holding     []string // the declared holding statuses, if any
}

func (e *MilestoneQueueOpenError) Error() string {
	named := e.Open
	more := ""
	if len(named) > QueueNameLimit {
		named = named[:QueueNameLimit]
		more = fmt.Sprintf(", and %d more", len(e.Open)-QueueNameLimit)
	}
	subject := fmt.Sprintf("%d open nibs are", len(e.Open))
	if len(e.Open) == 1 {
		subject = "1 open nib is"
	}
	holding := ""
	if len(e.Holding) > 0 {
		holding = fmt.Sprintf(", or close it as %s to keep the queue", strings.Join(e.Holding, " / "))
	}
	return fmt.Sprintf("cannot close milestone %s as %s: %s still assigned to its queue (%s%s) — reassign the open work to another milestone or clear its assignments first%s",
		e.MilestoneID, e.Status, subject, strings.Join(named, ", "), more, holding)
}

// refuseClosingFullQueue is decision 1.5 read off the state the request LEAVES:
// b is updateNib's owned clone with this request's status and type already
// applied, so the guard judges what will be on disk rather than what is.
//
// The conjunction is ordered by cost, and that ordering is load-bearing rather
// than cosmetic. The two field tests are free; the queue read behind them is a
// full-store scan (membership.Compute over Reader.All()), so an ordinary
// updateNib — every title edit, every status change on anything that is not a
// milestone — pays nothing for this guard. The view comes from
// cachedMembershipView, which memoFor deliberately does NOT memoize for a
// mutation: one document can write between two reads, and a queue answered from
// before that write is exactly the staleness this guard must not have.
//
// Nothing here is spelled as a status name. Which reasons release their
// dependents is config.StatusReleasesDependents' answer, so a holding reason —
// today deferred — passes: a parked milestone is coming back and decision 1.5
// lets it keep its queue.
func (r *mutationResolver) refuseClosingFullQueue(ctx context.Context, b *nib.Nib) error {
	cfg := r.Reader.Config()
	if b.EffectiveType() != "milestone" || !cfg.StatusReleasesDependents(b.Status) {
		return nil
	}

	// Two different types are in play, and the queue read below answers to the
	// STORED one. membership.View.DirectMembers picks its axis from the stored
	// nib: a milestone-typed container hands back its assignees, anything else
	// hands back its structural CHILDREN. The pending clone decides whether this
	// guard runs; the stored nib decides what it would be shown.
	//
	// For a nib only BECOMING a milestone in this same request those diverge, and
	// it has no queue either way — nothing can have been assigned to it while it
	// was not a milestone. Asking anyway hands back its children and refuses while
	// naming work that carries no assignment at all, so the remedy the message
	// names ("clear its assignments") cannot apply to any of it. The type change
	// is still refused, by the check that actually understands it: a milestone can
	// be nobody's parent.
	//
	// A subject that vanished between GetForUpdate and here is not this guard's to
	// report — the write it is about to attempt says so with the right error.
	stored, err := r.Reader.Get(b.ID)
	if err != nil || stored.EffectiveType() != "milestone" {
		return nil
	}

	open := OpenQueueEntries(cachedMembershipView(ctx, r.Reader), b.ID, cfg)
	if len(open) == 0 {
		return nil
	}
	return &MilestoneQueueOpenError{
		MilestoneID: b.ID,
		Status:      b.Status,
		Open:        open,
		Holding:     cfg.HoldingStatusNames(),
	}
}
