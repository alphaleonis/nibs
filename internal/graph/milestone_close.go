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
// switches to a count. It is exported because `nibs close`'s own gate
// (cmd/close_queue.go) words that refusal for the CLI and must cap it the same
// way.
const QueueNameLimit = 5

// OpenQueueEntries is THE definition of a milestone's OPEN queue (decision 1.5):
// the ids of its direct assignees whose status is not closed, in queue order.
// Call it rather than re-deriving the predicate.
//
// "Open" is the ordinary role vocabulary (config.IsClosedStatus): a deferred
// MEMBER is closed and does not hold the milestone open. The set is the DIRECT
// assignees, not the transitive closure beneath them — the remedies act on an
// assignment (--unassign-open, --move-open-to), and work belonging through an
// ancestor has none.
//
// view pins live store pointers (see internal/membership's discipline); only ids
// are read out of it here.
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
// holds assignments: every member's `milestone:` would keep naming it, conferring
// no membership. Unlike the close gate this counts EVERY assignee, open or
// closed.
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

// memberIDs reads the ids out of a member set in queue order, so no store pointer
// outlives the view it came from (internal/membership's discipline).
//
// DirectMembers answers in reader.All() order, which is Go map-iteration order,
// so without the sort a refusal past QueueNameLimit names a different subset each
// time it is raised.
func memberIDs(members []*nib.Nib) []string {
	nib.SortByMilestoneOrder(members)
	ids := make([]string, len(members))
	for i, m := range members {
		ids[i] = m.ID
	}
	return ids
}

// MilestoneQueueOpenError is decision 1.5's refusal at the model boundary: a
// milestone may not take a status that RELEASES its dependents while open work
// is still assigned to its queue, which would unblock them while the work the
// milestone gathered is unfinished.
//
// It is the backstop `nibs close` gates ahead of, reached by every client that
// has no flags to offer — so the message names the capability (reassign the open
// work or clear its assignments), not a flag.
//
// Add no Unwrap: there is no cause underneath, and mutationErrCode's trailing
// nib.ErrNotFound test must not be able to reach through one. With no branch of
// its own there this stays validation-class, like `nibs close`'s own refusal.
type MilestoneQueueOpenError struct {
	MilestoneID string   // the milestone being closed
	Status      string   // the releasing status it was being closed as
	Open        []string // its open queue entries, in queue order
	Holding     []string // declared holding statuses, resolved at construction; may be empty
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
// The conjunction is ordered by cost: the two field tests are free, the queue
// read behind them is a full-store scan (membership.Compute over Reader.All()),
// so an ordinary updateNib pays nothing. cachedMembershipView does not memoize
// for a mutation — one document can write between two reads, and a queue
// answered from before that write is the staleness this guard must not have.
//
// Spell no status name here: config.StatusReleasesDependents decides, so a
// holding reason (today deferred) keeps its queue.
func (r *mutationResolver) refuseClosingFullQueue(ctx context.Context, b *nib.Nib) error {
	cfg := r.Reader.Config()
	if b.EffectiveType() != "milestone" || !cfg.StatusReleasesDependents(b.Status) {
		return nil
	}

	// The queue read below answers to the STORED type: DirectMembers hands back
	// assignees for a milestone-typed nib and structural CHILDREN for anything
	// else. The pending clone decides whether this guard runs; the stored nib
	// decides what it would be shown.
	//
	// A nib only BECOMING a milestone in this request has no queue either way,
	// and asking anyway would refuse while naming children that carry no
	// assignment. That type change is refused by the check that understands it:
	// a milestone can be nobody's parent.
	//
	// A subject that vanished between GetForUpdate and here is not this guard's
	// to report; the write it is about to attempt says so.
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
