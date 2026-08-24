package cmd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/99designs/gqlgen/graphql"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/membership"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/nibtypes"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/spf13/cobra"
)

// closeQueueNameLimit is graph.QueueNameLimit under this package's name: the
// gate's refusal and updateNib's backstop are one refusal seen from two
// surfaces, so they name the same amount of one set. See that constant for why
// a queue refusal caps its enumeration at all.
const closeQueueNameLimit = graph.QueueNameLimit

// closeQueueGate is decision 1.5: closing a milestone speaks its queue.
//
// A milestone closed for a reason that RELEASES its dependents — the reasons
// that say the wave is over, config.StatusReleasesDependents, today completed
// and scrapped — is refused while open work is still assigned to it, because
// closing it would leave that work planned for a wave that has finished.
// Deferred does not release: a parked milestone is coming back, so decision
// 1.5 lets it keep its queue and this gate stays out of its way.
//
// Which is also why a disposition flag is REFUSED under a holding reason rather
// than obeyed. The escapes exist to escape this gate's refusal; where the gate
// never refuses there is nothing for them to escape, and the two readings of the
// combination are not symmetric — obeying it destroys the queue keys the parked
// milestone was just promised (Orderer.Recalculate issues fresh ones on any
// later reassignment), while refusing it costs a rerun. The refusal names the
// explicit way to clear a parked milestone's queue anyway, so the intent is
// still expressible, just not by accident.
//
// "Open" is the ordinary role vocabulary every other surface reads
// (config.IsClosedStatus): a deferred MEMBER is closed and does not hold the
// milestone open. The two readings of "deferred" in decision 1.5 are about
// different nibs — the milestone's own close reason, and its members' statuses.
//
// The set is the milestone's QUEUE — its direct assignees — not the transitive
// closure beneath them. That is the set the two escapes can act on: an
// assignment is what --unassign-open clears and what --move-open-to rewrites,
// and work belonging to the milestone only through an assigned ancestor has no
// assignment of its own to dispose of. A refusal reading a wider set than its
// own remedies could clear would be unanswerable.
//
// Returns the disposition that ran (nil when none did) and nil when the close
// may proceed; the escapes have then already run. The disposition is returned
// as a record rather than as its rendered notice because it has to outlive this
// call — the subject's own write comes after it and can still fail.
func closeQueueGate(ctx context.Context, cmd *cobra.Command, app *App, resolver *graph.Resolver, subject *nib.Nib) (*closeDisposition, error) {
	// Cobra refuses the two together (MarkFlagsMutuallyExclusive in close.go's
	// init), so exactly one of these can be true here.
	move := cmd.Flags().Changed("move-open-to")
	unassign := closeUnassignOpen

	if subject.EffectiveType() != "milestone" {
		if move || unassign {
			return nil, cmdError(closeJSON, output.ErrValidation,
				"--move-open-to and --unassign-open dispose of a milestone's queue, and %s is a %s",
				stripControlChars(subject.ID), subject.EffectiveType())
		}
		return nil, nil
	}

	// One View per command, per internal/membership's live-pointer discipline:
	// the queue is read out of it as ids immediately, so no store pointer is
	// held across the writes below.
	view := membership.Compute(resolver.Reader.All())
	queued := len(view.DirectMembers(subject.ID))
	open := graph.OpenQueueEntries(view, subject.ID, app.Config())

	if move || unassign {
		if !app.Config().StatusReleasesDependents(closeAs) {
			return nil, closeHoldingReasonDispositionError(app.Config(), subject, move)
		}
		if err := closePreValidateSubject(resolver, subject); err != nil {
			return nil, err
		}
	}
	switch {
	case move:
		return closeMoveOpenWork(ctx, app, resolver, subject, open, queued)
	case unassign:
		return closeUnassignOpenWork(ctx, app, resolver, subject, open, queued)
	}

	if len(open) == 0 || !app.Config().StatusReleasesDependents(closeAs) {
		return nil, nil
	}
	return nil, cmdError(closeJSON, output.ErrValidation,
		"cannot close milestone %s as %s: %s still assigned to its queue (%s%s) — reassign them with --move-open-to <milestone>, drop the assignments with --unassign-open%s",
		stripControlChars(subject.ID), closeAs, countNibs(len(open)),
		strings.Join(namedIDs(open), ", "), moreThanNamed(len(open)),
		holdingReasonClause(app.Config()))
}

// closeDisposition records what a queue disposition actually did: the verb it
// ran, the flag that asked for it, where the work went, and every member it
// rewrote, in queue order.
//
// It is a record rather than a rendered notice because it has to survive the
// SUBJECT's own write, which runs after the disposition and can still fail. A
// caller told only about that failure would never learn that nibs it did not
// name had already been rewritten — and their queue keys are not recoverable
// afterwards, since Orderer.Recalculate assigns fresh ones on any later
// reassignment.
type closeDisposition struct {
	verb    string   // past tense, the voice the notice reads in: "moved" / "unassigned"
	flag    string   // the flag that asked for it, named by the follow-up advice
	target  string   // --move-open-to's milestone; empty for --unassign-open
	members []string // the ids it rewrote, in queue order
}

// notice renders the line a completed disposition prints beside the card (and
// carries in --json's warnings array), naming what moved and where.
func (d *closeDisposition) notice(subjectID string) string {
	if d == nil || len(d.members) == 0 {
		return ""
	}
	destination := ""
	if d.target != "" {
		destination = " to " + stripControlChars(d.target)
	}
	return fmt.Sprintf("%s %s from %s%s: %s%s",
		d.verb, countNibsOf(len(d.members), "open"), stripControlChars(subjectID), destination,
		strings.Join(namedIDs(d.members), ", "), moreThanNamed(len(d.members)))
}

// closeMemberRefusal is why one queue member refused its disposition write,
// classified by what — if anything — can get past it. The class is the whole
// content of the advice: three of the four exits the escapes appear to offer
// are dead ends for some refusals, and naming one that cannot work sends the
// caller round a loop that makes no progress and no writes.
type closeMemberRefusal int

const (
	// closeRefusalTransient: nothing about the member says the refusal repeats
	// — a concurrent edit landing mid-disposition, transient I/O. This is the
	// one class where rerunning is the repair, and it is the only class the
	// advice may describe that way.
	closeRefusalTransient closeMemberRefusal = iota

	// closeRefusalExclusivity: decision 1.2's ancestor/descendant conflict, and
	// the ONLY class --unassign-open routes around — validateAndSetMilestone's
	// clear branch returns before those checks run, so clearing succeeds
	// exactly where assigning fails. Reachable from the move path only.
	closeRefusalExclusivity

	// closeRefusalOwnFrontMatter: the member's OWN front matter is what the
	// write path refuses. Both escapes call UpdateNib, whose preValidateSubject
	// runs ValidateEnums, the axis rule and the area vocabulary BEFORE the
	// milestone branch splits, so the clear path meets the identical wall — and
	// every rerun recomputes the same set and stops at the same member. Only
	// repairing that nib clears it, and this command is not where that happens:
	// writing a nib the vocabulary refuses in order to get past its own guard
	// is not an escape, it is a corruption.
	//
	// The advice therefore quotes the refusal itself and points nowhere else.
	// `nibs check` reports the enum and axis causes but is deliberately SILENT
	// for an undeclared area — read-tolerance is by design there — so naming it
	// as the place to look would be true of some members and false of others,
	// with nothing in the message to say which.
	closeRefusalOwnFrontMatter

	// closeRefusalOwnFile: the member's own file could not be written. The same
	// dead end one layer down — both escapes write that same file through the
	// same call, so neither routes around it and no rerun does either.
	closeRefusalOwnFile
)

// closeClassifyMemberRefusal decides which of the four a member's refusal is.
//
// The member's OWN state is asked FIRST and directly, not inferred from the
// error: a member whose front matter the write path refuses is a dead end
// whatever error happened to surface, and that fact is checkable rather than
// guessable. Only then is the error itself consulted — for the one class that
// has an escape (typed, so the test is not a substring match on a message) and
// for the filesystem's permission refusal. Everything left over is transient,
// which is the only class whose advice mentions rerunning.
func closeClassifyMemberRefusal(resolver *graph.Resolver, id string, err error) closeMemberRefusal {
	if member, ok := resolver.Reader.GetSnapshot(id); ok {
		if closeMemberOwnGuards(resolver, member) != nil {
			return closeRefusalOwnFrontMatter
		}
	}
	var exclusivity *graph.MilestoneExclusivityError
	if errors.As(err, &exclusivity) {
		return closeRefusalExclusivity
	}
	if errors.Is(err, fs.ErrPermission) {
		return closeRefusalOwnFile
	}
	return closeRefusalTransient
}

// closeMemberOwnGuards runs the write-free guards a disposition applies to a
// MEMBER whichever escape asked for it, so a failure here is one no escape
// routes around.
//
// UpdateNib's preValidateSubject runs ValidateEnums, the axis rule and the area
// vocabulary before validateAndSetMilestone branches, so all three are shared,
// and in that order — no area value satisfies the axis rule, so answering an
// undeclared area on a milestone-typed member with the declared set would
// prescribe a remedy that member cannot follow.
//
// The axis rule is asked with the assignment CLEARED because that is the weaker
// of its two readings: a member the clear path still refuses is a genuine dead
// end, while one only the assign reading refuses (a milestone-typed nib carrying
// an assignment, which a hand-edit can produce) is precisely a case
// --unassign-open does fix, and calling that a dead end would withhold the
// remedy that works. The area rule has no such weaker reading — neither escape
// touches `area:`, so the member's stored value is the one both writes carry.
func closeMemberOwnGuards(resolver *graph.Resolver, member *nib.Nib) error {
	if err := resolver.Validator.ValidateEnums(member); err != nil {
		return err
	}
	if err := nibtypes.ValidateAxes(member.EffectiveType(), "", member.Area); err != nil {
		return err
	}
	return resolver.Validator.ValidateArea(member)
}

// refusalRemedy is the tail of the partial-failure advice: what the caller
// should actually do, given what refused.
//
// Only the transient class is told to rerun, and only the exclusivity class is
// offered the other escape. The two dead-end classes are told plainly that the
// member itself is the blocker and that no escape and no rerun changes that,
// because the previous wording — "fix what it was refused for", plus a rerun
// and an --unassign-open that hit the same wall — described a way out that did
// not exist. Every class still ends with the holding-reason exit, which is the
// one that needs no member repair at all.
func (d *closeDisposition) refusalRemedy(failedID string, class closeMemberRefusal, cfg *config.Config) string {
	id := stripControlChars(failedID)
	switch class {
	case closeRefusalOwnFrontMatter:
		return fmt.Sprintf("the blocker is %s's own front matter, which every escape and every retry meets identically — repair %s, then close again%s",
			id, id, holdingReasonExitClause(cfg))
	case closeRefusalOwnFile:
		return fmt.Sprintf("the blocker is %s's own file, which could not be written — every escape meets the same failure, so make that file writable, then close again%s",
			id, holdingReasonExitClause(cfg))
	case closeRefusalExclusivity:
		return fmt.Sprintf("rerun to dispose of the rest if that refusal was transient, but a refusal that repeats means %s cannot be moved to %s as it stands: drop the assignments with --unassign-open instead%s",
			id, stripControlChars(d.target), holdingReasonExitClause(cfg))
	default:
		return fmt.Sprintf("rerun to dispose of the rest if that refusal was transient, but a refusal that repeats means %s cannot be %s as it stands: repair it, then close again%s",
			id, d.verb, holdingReasonExitClause(cfg))
	}
}

// closeMoveOpenWork reassigns the open queue entries to another milestone.
// Each one goes through updateNib's assignment path, so it enters the target
// queue the way every other assignment does — at the ordering engine's default
// placement, which for a queue is last — and carries nothing of its old key
// (Orderer.Recalculate rewrites the key for the new group).
func closeMoveOpenWork(ctx context.Context, app *App, resolver *graph.Resolver, subject *nib.Nib, open []string, queued int) (*closeDisposition, error) {
	target, err := resolveMoveOpenTarget(ctx, app, resolver, subject)
	if err != nil {
		return nil, err
	}
	if len(open) == 0 {
		return nil, closeEmptyOpenSetError(subject, "move", "--move-open-to", queued)
	}

	if err := closePreValidateMembers(app.Config(), resolver, subject, open); err != nil {
		return nil, err
	}

	targetID := target.ID
	disposed := &closeDisposition{verb: "moved", flag: "--move-open-to", target: targetID, members: open}
	for i, id := range open {
		progress := fmt.Sprintf("moved %d of %d open nibs to %s", i, len(open), stripControlChars(targetID))
		etag, etagErr := closeMemberETag(resolver, id)
		if etagErr != nil {
			return nil, closePartialQueueWriteError(app.Config(), resolver, subject, disposed, id, i, etagErr, progress)
		}
		input := model.UpdateNibInput{Milestone: graphql.OmittableOf(&targetID), IfMatch: etag}
		if _, updateErr := resolver.Mutation().UpdateNib(ctx, id, input); updateErr != nil {
			return nil, closePartialQueueWriteError(app.Config(), resolver, subject, disposed, id, i, updateErr, progress)
		}
	}
	return disposed, nil
}

// closeUnassignOpenWork drops the assignment — and with it the queue key,
// which Orderer.Recalculate clears for a nib in no queue — from every open
// queue entry.
func closeUnassignOpenWork(ctx context.Context, app *App, resolver *graph.Resolver, subject *nib.Nib, open []string, queued int) (*closeDisposition, error) {
	if len(open) == 0 {
		return nil, closeEmptyOpenSetError(subject, "unassign", "--unassign-open", queued)
	}
	if err := closePreValidateMembers(app.Config(), resolver, subject, open); err != nil {
		return nil, err
	}

	disposed := &closeDisposition{verb: "unassigned", flag: "--unassign-open", members: open}
	for i, id := range open {
		progress := fmt.Sprintf("unassigned %d of %d open nibs", i, len(open))
		etag, etagErr := closeMemberETag(resolver, id)
		if etagErr != nil {
			return nil, closePartialQueueWriteError(app.Config(), resolver, subject, disposed, id, i, etagErr, progress)
		}
		input := model.UpdateNibInput{Milestone: graphql.OmittableOf[*string](nil), IfMatch: etag}
		if _, updateErr := resolver.Mutation().UpdateNib(ctx, id, input); updateErr != nil {
			return nil, closePartialQueueWriteError(app.Config(), resolver, subject, disposed, id, i, updateErr, progress)
		}
	}
	return disposed, nil
}

// closeMemberETag derives the if-match for one queue member's disposition
// write, from the member as this process LOADED it.
//
// A disposition rewrites nibs the caller never named, so the caller can supply
// an etag only for the SUBJECT — and under nibs.require_if_match a write
// without one is refused, which would make both escapes dead ends and leave a
// milestone holding open assigned work unclosable for any releasing reason.
// Deriving it here is what every other foreign write in the tree does:
// close's own Key-Decisions flow-up (close.go's recipient.ETag()),
// graph.updateTargetClone, graph.activateParentChain and Orderer.backfillKeys
// all take the token off the in-memory nib.
//
// What that consensus rules out is handing the write a token read straight off
// the file, unchecked, and the reason is not stylistic: the write carries the
// IN-MEMORY nib, while Core.Update validates the if-match against the very
// computeStoredETag call Reader.CurrentETag returns. The token would be
// compared against itself, so the precondition could never fail and any on-disk
// divergence would be silently clobbered instead of refused. `nibs close` runs
// no file watcher (only `nibs serve` starts one), so the store's picture is
// fixed at load and that window is the whole command rather than a
// sub-millisecond race. So the decision made here is the COMPARISON against the
// loaded nib; the token returned afterwards is only what carries the residual
// window to the write. Guarded by
// TestCloseMilestoneDispositionRefusesADivergedMember.
//
// It is read immediately before each member's own write rather than once up
// front: an earlier move can persist a queue key onto a later member
// (Orderer.backfillKeys repairs lazily), which would stale a token taken
// earlier in the batch.
func closeMemberETag(resolver *graph.Resolver, id string) (*string, error) {
	loaded, ok := resolver.Reader.GetSnapshot(id)
	if !ok {
		return nil, nibcore.ErrNotFound
	}
	onDisk, err := resolver.Reader.CurrentETag(id)
	if err != nil {
		return nil, err
	}
	if loaded.ETag() != onDisk {
		return nil, &nibcore.ETagMismatchError{Provided: loaded.ETag(), Current: onDisk}
	}
	// The file IS what this process loaded, so the on-disk token is the correct
	// precondition. The residual window it still covers is the one between this
	// read and Core.Update's own check.
	return &onDisk, nil
}

// resolveMoveOpenTarget resolves and validates --move-open-to. The target must
// exist and be milestone-typed (the same pair `nibs set --milestone` demands,
// refused the same way and with the same exit), must not be the milestone
// being closed, and must not itself be closed for a reason that releases its
// dependents — moving open work into a completed or scrapped milestone would
// recreate, one command later, exactly the state this gate exists to prevent.
// A DEFERRED target is accepted, because decision 1.5 gives a parked milestone
// its queue: it is coming back, and parking work with it is a real disposition.
func resolveMoveOpenTarget(ctx context.Context, app *App, resolver *graph.Resolver, subject *nib.Nib) (*nib.Nib, error) {
	raw := strings.TrimSpace(closeMoveOpenTo)
	if raw == "" {
		return nil, cmdError(closeJSON, output.ErrValidation,
			"--move-open-to requires the id of the milestone to move the open work to")
	}
	target, err := resolver.Query().Nib(ctx, raw)
	if err != nil || target == nil {
		return nil, cmdError(closeJSON, output.ErrValidation, "milestone nib not found: %s", stripControlChars(raw))
	}
	if typ := target.EffectiveType(); typ != "milestone" {
		return nil, cmdError(closeJSON, output.ErrValidation,
			"milestone target %s has type %s, not milestone", stripControlChars(target.ID), typ)
	}
	if target.ID == subject.ID {
		return nil, cmdError(closeJSON, output.ErrValidation,
			"--move-open-to %s is the milestone being closed: name another milestone, or use --unassign-open",
			stripControlChars(target.ID))
	}
	if app.Config().StatusReleasesDependents(target.Status) {
		return nil, cmdError(closeJSON, output.ErrValidation,
			"cannot move open work into %s: it is already closed as %s, so the moved work would be planned for a milestone that is over",
			stripControlChars(target.ID), target.Status)
	}
	return target, nil
}

// closePreValidateSubject runs EVERY write-free guard the subject's own write
// will apply, BEFORE a disposition writes to other nibs' files — for exactly
// the reason graph.preValidateSubject gives, and by calling that same check
// rather than restating it: a close that is going to be refused must not first
// drain a queue. A milestone carrying a retired enum value or an assignment
// axis it may not have is refused by the write path every time, so running
// only the --if-match half here left the queue disposed of, the milestone
// still open, and the rerun the partial-failure message prescribes facing an
// empty open set.
//
// The subject is validated as it will be WRITTEN: the pending status goes onto
// a Clone first, mirroring updateNib, which applies the enum fields before it
// pre-validates. Validating the nib as READ would refuse a close whose own
// write replaces the offending value.
//
// It narrows the window rather than closing it — the authoritative checks
// still run inside Writer.Update, under the write lock. What is left is the
// subject's write itself (a concurrent edit landing in between, or write I/O),
// which no ordering of read-only checks can pre-empt.
func closePreValidateSubject(resolver *graph.Resolver, subject *nib.Nib) error {
	pending := subject.Clone()
	pending.Status = closeAs

	var ifMatch *string
	if closeIfMatch != "" {
		ifMatch = &closeIfMatch
	}
	if err := resolver.PreValidateSubject(pending, ifMatch); err != nil {
		return setMutationError(closeJSON, err)
	}
	return nil
}

// closeEmptyOpenSetError refuses an escape that has nothing to act on. Letting
// it succeed would report a disposition that never happened — and the silent
// no-op is the one answer a caller cannot tell apart from a queue that really
// was drained.
//
// It says what the queue DOES hold and names the remedy, because the refusal
// itself is not the whole story: a caller reaches it not only by aiming an
// escape at an unplanned milestone but also by rerunning after a disposition
// succeeded and the subject's own write did not (see closeSubjectWriteError).
// Dropping the flag is what closes the milestone from either state.
func closeEmptyOpenSetError(subject *nib.Nib, verb, flag string, queued int) error {
	held := "its queue is empty"
	if queued > 0 {
		held = fmt.Sprintf("its queue holds %s, all closed", countNibsOf(queued, "assigned"))
	}
	return cmdError(closeJSON, output.ErrValidation,
		"nothing to %s: no open nib is assigned to milestone %s (%s) — drop %s to close it",
		verb, stripControlChars(subject.ID), held, flag)
}

// closePartialQueueWriteError reports a bulk queue write that failed part way.
//
// The policy is the house one (internal/graph/bulkreorder.go): validate what
// can be validated up front — here the target milestone, the subject's own
// write-free guards and every MEMBER's (closePreValidateMembers) — then write
// sequentially, and let the writes already made STAY. So what the caller is
// told is what actually happened: how many landed, which nib refused and why,
// and that the milestone was NOT closed.
//
// The loop still returns on the FIRST refusal rather than disposing of the rest
// and collecting failures, and after the pre-validation pass that costs nothing
// a caller wants. The refusals that reach here are the ones no pre-check can
// pre-empt, and none of them lets the close succeed — so continuing would only
// destroy more queue keys (Orderer.Recalculate issues fresh ones on any later
// reassignment, so they do not come back) for a command that is going to fail
// anyway, and it would take the holding-reason exit — the one that keeps the
// queue — away from a caller who has not chosen yet. The completeness argument
// for collecting is real, and closePreValidateMembers is where it is paid: it
// names every doomed member at once, before the first write.
//
// What the advice must not do is prescribe an exit that cannot work, which is
// what closeMemberRefusal decides. Only a transient refusal is told to rerun,
// and only the exclusivity class is offered the other escape.
func closePartialQueueWriteError(cfg *config.Config, resolver *graph.Resolver, subject *nib.Nib, d *closeDisposition, failedID string, done int, err error, progress string) error {
	return closeQueueError(fmt.Sprintf(
		"%s, then %s refused: %v — %s was NOT closed, and the %d write(s) already made are persisted; %s",
		progress, stripControlChars(failedID), err, stripControlChars(subject.ID), done,
		d.refusalRemedy(failedID, closeClassifyMemberRefusal(resolver, failedID, err), cfg)), err)
}

// closePreValidateMembers refuses a disposition whose member set contains a nib
// no escape and no rerun could ever write, BEFORE the first write lands.
//
// It is closePreValidateSubject's counterpart, for the same reason and one step
// wider: a disposition that is going to be refused must not first destroy queue
// keys. Running only the subject's guards left a store with one hand-edited
// member draining every member ahead of it on every attempt while never getting
// past that one — the members BEHIND it were never even reached, so each rerun
// reported a different, smaller failure and made no progress toward the close.
//
// It names ALL the offenders rather than the first, which is the whole reason
// the check is a separate pass: a loop that stops at the first one turns N bad
// members into N repair-and-rerun cycles, each of which costs the queue keys of
// everything ahead of them.
//
// The guards are only the write-free ones a member's own file answers for
// (closeMemberOwnGuards). Write I/O, a concurrent edit and the assignment
// exclusivity conflict all stay with the loop: the first two cannot be
// pre-empted by a read, and the third is not a dead end — --unassign-open
// clears it.
func closePreValidateMembers(cfg *config.Config, resolver *graph.Resolver, subject *nib.Nib, open []string) error {
	var blocked, reasons []string
	for _, id := range open {
		member, ok := resolver.Reader.GetSnapshot(id)
		if !ok {
			// A member deleted between the view and here is the loop's problem
			// (it surfaces as a not-found on its own write), not this check's.
			continue
		}
		if err := closeMemberOwnGuards(resolver, member); err != nil {
			blocked = append(blocked, id)
			if len(reasons) < closeQueueNameLimit {
				reasons = append(reasons, fmt.Sprintf("%s (%v)", stripControlChars(id), err))
			}
		}
	}
	if len(blocked) == 0 {
		return nil
	}
	pronoun, possessive := "it", "its"
	if len(blocked) > 1 {
		pronoun, possessive = "them", "their"
	}
	return cmdError(closeJSON, output.ErrValidation,
		"cannot dispose of milestone %s's queue: %s refused by the write path for %s own front matter — %s%s. No escape and no retry can write %s, so repair %s first, then close again%s",
		stripControlChars(subject.ID), countNibs(len(blocked)), possessive,
		strings.Join(reasons, "; "), moreThanNamed(len(blocked)),
		pronoun, pronoun,
		holdingReasonExitClause(cfg))
}

// closeHoldingReasonDispositionError refuses a disposition flag combined with a
// close reason that does not release the milestone's dependents.
//
// The two contradict: the escapes exist to escape the queue gate's refusal, and
// a holding reason is never refused — decision 1.5 hands a parked milestone its
// queue precisely so it can pick the wave back up. Obeying the combination
// would drain that queue irrecoverably, so it is refused the way close's other
// contradictory pairs are, and the message names the explicit spelling for a
// caller who really did mean to clear the assignments.
func closeHoldingReasonDispositionError(cfg *config.Config, subject *nib.Nib, move bool) error {
	flag := "--unassign-open"
	if move {
		flag = "--move-open-to"
	}
	return cmdError(closeJSON, output.ErrValidation,
		"%s cannot be combined with --as %s: a milestone closed as %s keeps its queue, so %s has nothing to dispose of — drop it, close %s with a reason that releases its dependents (%s), or drop the assignments deliberately with `nibs set <id> --clear milestone` per member",
		flag, closeAs, closeAs, flag, stripControlChars(subject.ID),
		strings.Join(cfg.ReleasingStatusNames(), " / "))
}

// closeSubjectWriteError reports the subject's own write failing AFTER its
// queue disposition had already completed.
//
// The disposition is the half the caller cannot see: nibs it never named have
// been rewritten and their queue keys destroyed, while the milestone it did
// name is still open. Reporting only the write error leaves that invisible —
// and sends the reader into the one retry that cannot work, since the identical
// command now meets an empty open set and is refused for it. So the message
// reports what was written and names the flag to DROP, which is what finishes
// the close from this state.
//
// With no disposition it is the plain mutation error, unchanged.
func closeSubjectWriteError(subjectID string, d *closeDisposition, err error) error {
	if d == nil || len(d.members) == 0 {
		return setMutationError(closeJSON, err)
	}
	return closeQueueError(fmt.Sprintf(
		"%s — then %s could not be written: %v. %s was NOT closed and those %d write(s) are persisted; rerun WITHOUT %s to finish the close (its queue is disposed of already, so rerunning WITH it is refused for an empty open set)",
		d.notice(subjectID), stripControlChars(subjectID), err,
		stripControlChars(subjectID), len(d.members), d.flag), err)
}

// closeQueueError classifies a disposition failure reported under a message
// this file composed. The message is ours; the CLASS stays the underlying
// error's — including, for a reconcilable ETag conflict, the server's
// currentEtag token — so an agent branching on $? or reading the --json
// envelope gets the same answer any other mutation would give it (see
// etagConflictError, whose enrichment this mirrors).
func closeQueueError(msg string, err error) error {
	var mismatch *nibcore.ETagMismatchError
	if errors.As(err, &mismatch) {
		if closeJSON {
			return output.ErrorConflict(msg, mismatch.Current)
		}
		return &output.CodedError{Code: output.ErrConflict, Msg: msg}
	}
	code, ok := mutationErrCode(err)
	if !ok {
		code = output.ErrValidation
	}
	return cmdError(closeJSON, code, "%s", msg)
}

// namedIDs renders the ids a refusal or a notice enumerates, capped at
// closeQueueNameLimit (moreThanNamed supplies the remainder).
func namedIDs(ids []string) []string {
	named := ids
	if len(named) > closeQueueNameLimit {
		named = named[:closeQueueNameLimit]
	}
	out := make([]string, len(named))
	for i, id := range named {
		out[i] = stripControlChars(id)
	}
	return out
}

// moreThanNamed is the ", and N more" tail namedIDs elides, or "".
func moreThanNamed(total int) string {
	if total <= closeQueueNameLimit {
		return ""
	}
	return fmt.Sprintf(", and %d more", total-closeQueueNameLimit)
}

// countNibs renders the subject of the refusal sentence ("1 open nib is" /
// "3 open nibs are") so the message never carries a parenthesized plural.
func countNibs(n int) string {
	if n == 1 {
		return "1 open nib is"
	}
	return fmt.Sprintf("%d open nibs are", n)
}

// countNibsOf is countNibs's object form, for the notice a disposition prints
// ("1 open nib" / "3 open nibs") and for the tally of what a queue with nothing
// open still holds ("2 assigned nibs"). The adjective is the caller's because
// the two count different sets; the pluralization is shared so neither grows a
// parenthesized plural of its own.
func countNibsOf(n int, adjective string) string {
	if n == 1 {
		return "1 " + adjective + " nib"
	}
	return fmt.Sprintf("%d %s nibs", n, adjective)
}

// holdingReasonClause offers the third way out: a close reason that does NOT
// release its dependents keeps the queue untouched (decision 1.5). Derived
// from the role vocabulary, so a project declaring no holding reason gets no
// clause rather than a suggestion it cannot follow.
//
// This spelling is for the GATE's refusal, which carries no disposition flag —
// there the reason is genuinely something to add to the command that just
// failed. holdingReasonExitClause is the spelling for the other side.
func holdingReasonClause(cfg *config.Config) string {
	return holdingReasonSuffix(cfg, ", or ")
}

// holdingReasonExitClause is the same exit offered to a message a disposition
// FLAG produced. Naming the reason alone there would read as something to
// append to the command that just failed — and appending it is refused, since
// a holding reason and a disposition flag contradict each other
// (closeHoldingReasonDispositionError). So this spells out that the flag comes
// off.
func holdingReasonExitClause(cfg *config.Config) string {
	return holdingReasonSuffix(cfg, ", or drop the flag and close with ")
}

// holdingReasonSuffix renders the shared tail — every declared holding reason,
// spelled as the --as it is passed with — behind the caller's lead-in.
func holdingReasonSuffix(cfg *config.Config, lead string) string {
	names := cfg.HoldingStatusNames()
	if len(names) == 0 {
		return ""
	}
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = "--as " + n
	}
	return lead + strings.Join(quoted, " / ") + " to keep the queue"
}
