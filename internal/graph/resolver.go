package graph

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/nibtypes"
)

// schema.resolvers.go is generated but not disposable: gqlgen rewrites it on
// every codegen and carries parts of the existing file into the new one. Which
// parts is the whole subtlety. Resolver bodies survive as raw source; a
// resolver's doc-comment prose survives (UpdateNib and DeleteNib carry
// hand-written paragraphs that outlive codegen); the import block survives
// (internal/nibtypes is imported solely for a hand-written resolver body and
// appears nowhere in generated.go); and a comment directive in doc position
// survives too, since gqlgen iterates the comment list rather than flattening it
// through go/ast's CommentGroup.Text().
//
// Two things do not survive, both quietly: a free-standing comment (attached to
// no declaration) is dropped outright, and a non-resolver declaration is moved
// into a commented-out block at the end with its own doc comment discarded. So a
// durable note about that file needs a surviving home — a resolver's doc
// comment, or this file, which gqlgen writes once when it is absent and never
// regenerates.

//go:generate go tool gqlgen generate

// Resolver is the root resolver for the GraphQL schema.
// It holds role interfaces for data access, validation, and blocking queries.
type Resolver struct {
	Reader     NibReader
	Writer     NibWriter
	Validator  NibValidator
	Blocking   BlockingChecker
	Subscriber NibSubscriber
	Orderer    *Orderer
	// Version is the running binary version, used by the updateStatus query.
	// Empty (or "dev") disables the check.
	Version string
}

// checkMutualExclusion returns an error if both the replace field and any
// delta field are non-nil. fieldName is used in the error message.
func checkMutualExclusion(fieldName string, replace any, deltas ...any) error {
	if isNilValue(replace) {
		return nil
	}
	for _, d := range deltas {
		if !isNilValue(d) {
			return fmt.Errorf("cannot specify both %s", fieldName)
		}
	}
	return nil
}

// isNilValue checks whether v is nil, handling both untyped nil and typed nil
// pointers/slices/maps that get boxed in an any interface.
func isNilValue(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Map:
		return rv.IsNil()
	}
	return false
}

// updateTargetClone is the one blessed target-side write path: it fetches an
// OWNED clone of the target via Reader.GetForUpdate, applies mutate to that
// clone, and writes it — the SHARED c.nibs[id] pointer is never mutated. A
// rejected Writer.Update (genuine on-disk etag divergence, or a concurrent write
// to the target between its fetch and its Update) therefore leaves the shared
// in-memory nib untouched instead of showing a phantom mutation.
//
// Sourcing the clone from GetForUpdate makes the fetch FRESH per call: with a
// duplicate target id, a second invocation re-reads the now-updated c.nibs[id]
// (installed by the first Update) rather than reusing a stale pre-mutation
// pointer.
//
// The if-match is the target's PRE-mutation ETag, computed from the fresh clone
// before mutate runs — equivalent to the shared nib's current etag since the
// clone is a faithful copy — matching every blocking-side call site's existing
// optimistic-concurrency convention (these sites key on the target's own current
// etag, not a caller-supplied if-match). mutate returns false to signal "nothing
// changed" (e.g. RemoveBlockedBy matched no id), in which case no write is
// attempted and nil is returned. A missing target surfaces an id-bearing
// not-found error, so callers that tolerate missing targets must guard existence
// before calling.
func (r *Resolver) updateTargetClone(id string, mutate func(*nib.Nib) bool) error {
	clone, err := r.Reader.GetForUpdate(id)
	if err != nil {
		// GetForUpdate only fails not-found; name the id so the (concurrent-delete)
		// error is diagnosable rather than a bare ErrNotFound.
		return fmt.Errorf("target nib not found: %s: %w", id, err)
	}
	ifMatch := clone.ETag()
	if !mutate(clone) {
		return nil
	}
	return r.Writer.Update(clone, &ifMatch)
}

// snapshotResult returns a detached GetSnapshot clone of the nib a mutation just
// wrote, so gqlgen never marshals the live c.nibs pointer. Every nib-returning
// mutation resolver ends by handing its result to this helper: Writer.Create/
// Writer.Update install the working nib AS the shared store entry (c.nibs[id] =
// b), and Reader.Get hands back that same live pointer — either way the value the
// resolver would otherwise return aliases the store. Per the canonical
// live-pointer / copy-on-write invariant (NibReader.GetSnapshot), a stored
// pointer's Path is still rewritten in place under c.mu while gqlgen marshals the
// returned nib's fields asynchronously off the lock, so a GetSnapshot clone taken
// under the store lock is the only value safe to hand out. A !ok means the nib
// vanished between the write and the snapshot (e.g. a concurrent delete): report
// it as an error rather than returning a nil nib for the non-null result.
func (r *Resolver) snapshotResult(id string) (*nib.Nib, error) {
	snap, ok := r.Reader.GetSnapshot(id)
	if !ok {
		return nil, fmt.Errorf("nib not found after write: %s: %w", id, nib.ErrNotFound)
	}
	return snap, nil
}

// snapshotResults is the slice form of snapshotResult for the bulk-reorder
// resolvers, preserving order. Each element is detached via GetSnapshot. Unlike
// the singular snapshotResult, a !ok is NOT an error here: every input nib was
// just written by the reorder loop, so a miss means the nib vanished via a
// concurrent delete in the lock-free window between its order-key write
// committing and this post-write snapshot. Skip the vanished element and return
// the surviving snapshots in order — the persisted order among the survivors is
// still valid, so the shortened ordered set is the honest result. Failing the
// whole already-persisted batch instead would misreport a durable write as a
// total failure and dead-end the client's same-input retry on
// validateBulkChildren (the deleted child no longer existing).
func (r *Resolver) snapshotResults(nibs []*nib.Nib) ([]*nib.Nib, error) {
	out := make([]*nib.Nib, 0, len(nibs))
	for _, b := range nibs {
		snap, ok := r.Reader.GetSnapshot(b.ID)
		if !ok {
			// Deleted concurrently after its order-key write committed; skip it.
			continue
		}
		out = append(out, snap)
	}
	return out, nil
}

// validateAndSetParent validates and sets the parent relationship.
// When the parent changes, the order key is recalculated to avoid collisions
// with existing siblings in the new parent group.
//
// "Changes" is decided from the RESOLVED old parent, not the stored string —
// see resolvedParent for the rule. Both readings agree on a link that names a
// nib; they part ways on one that does not, and there the raw reading counts
// dangling -> cleared as a change and recalculates. That relocates a nib which
// was ALREADY a root by every surface bound to the rule, to the end of the root
// order, for a repair that changes nothing semantically. Core.FixBrokenLinks
// repairs the identical link without touching Order, so the raw reading also
// puts the two repair paths (`nibs set --clear parent` and `nibs check --fix`)
// at odds over where the nib lands. Resolving settles both.
//
// Caller must pass a nib it owns (a clone), not a shared Reader.Get pointer —
// this mutates b (b.Parent and, via Orderer.Recalculate, b.Order) in place.
func (r *Resolver) validateAndSetParent(b *nib.Nib, parentID string) error {
	oldParent := resolvedParentID(b, r.Reader)

	if parentID == "" {
		b.Parent = ""
		if oldParent != "" {
			r.Orderer.Recalculate(ScopeParent, b)
		}
		return nil
	}

	// Normalize short ID to full ID
	normalizedParent, ok := r.Reader.NormalizeID(parentID)
	if !ok {
		return fmt.Errorf("parent nib not found: %s", parentID)
	}

	// Validate parent type hierarchy
	if err := r.Validator.ValidateParent(b, normalizedParent); err != nil {
		return err
	}

	// Check for cycles
	if cycle := r.Validator.DetectCycle(b.ID, "parent", normalizedParent); cycle != nil {
		return fmt.Errorf("setting parent would create cycle: %v", cycle)
	}

	// Assignment exclusivity is checked only for a REAL change of parent. The
	// type-change branch re-validates the existing parent through this same
	// call, and a pre-existing conflict in hand-edited data must not turn every
	// type change on such a nib into a dead end — `nibs check` names that
	// shape; the write path refuses only the moves that would create it.
	if normalizedParent != oldParent {
		if err := r.checkReparentExclusivity(b, normalizedParent); err != nil {
			return err
		}
	}

	b.Parent = normalizedParent
	if normalizedParent != oldParent {
		r.Orderer.Recalculate(ScopeParent, b)
	}
	return nil
}

// validateAndSetMilestone validates and sets the milestone assignment — the
// scheduling axis — and is the assignment-side mirror of validateAndSetParent.
//
// The target must exist and be milestone-typed (a short id normalizes like
// every other link); the subject must be able to carry an assignment at all
// (nibtypes.ValidateAxes: a milestone is a waypoint, not work); and decision
// 1.2's exclusivity holds — a nib and one of its ancestors are never both
// assigned — so an assigned ancestor or an assigned descendant refuses the
// write, naming the conflicting nib. "Assigned" throughout is the RESOLVED
// reading (membership.ResolvedMilestoneID): a dangling or non-milestone
// assignment schedules nothing, so it does not conflict either.
//
// On a change of queue the nib re-enters the new queue at the scope's default
// placement (last) through Orderer.Recalculate, the same hook a parent change
// uses; the key is never carried from one queue to another. Clearing drops
// the key with the assignment — Recalculate in the memberless group clears
// it — so a stale queue key cannot outlive its queue.
//
// Caller must pass a nib it owns (a clone), not a shared Reader.Get pointer —
// this mutates b (b.Milestone and, via Orderer.Recalculate, b.MilestoneOrder)
// in place.
func (r *Resolver) validateAndSetMilestone(b *nib.Nib, milestoneID string) error {
	oldMilestone := resolvedMilestoneID(b, r.Reader)

	if milestoneID == "" {
		b.Milestone = ""
		r.Orderer.Recalculate(ScopeMilestone, b)
		return nil
	}

	normalized, ok := r.Reader.NormalizeID(milestoneID)
	if !ok {
		return fmt.Errorf("milestone nib not found: %s", milestoneID)
	}
	target, err := r.Reader.Get(normalized)
	if err != nil {
		return fmt.Errorf("milestone nib not found: %s", milestoneID)
	}
	if typ := target.EffectiveType(); typ != "milestone" {
		return fmt.Errorf("milestone target %s has type %s, not milestone", normalized, typ)
	}
	if err := nibtypes.ValidateAxes(b.EffectiveType(), normalized, b.Area); err != nil {
		return err
	}
	// Decision 1.5's ASSIGNMENT door. Closing a milestone over a live queue is
	// refused on every client, but the same end-state — open work planned for a
	// wave that has finished — was reachable from the other side, by assigning
	// AFTER the close. Scoped to an OPEN subject on purpose: retro-assigning
	// finished work to a finished wave is how a record gets written after the
	// fact, and leaves nothing planned for a wave that ended. A HOLDING reason
	// keeps accepting work, because 1.5 gives a parked milestone its queue and it
	// is coming back. Which reasons release is config's answer, never a literal.
	//
	// It runs AFTER ValidateAxes deliberately. That rule is about the SUBJECT and
	// no property of the target can satisfy it — a milestone may never carry an
	// assignment — so answering first with the target's status would hand back a
	// remedy ("assign to an open milestone") that the subject cannot follow, and
	// callers here are told to stop at the first error.
	if cfg := r.Reader.Config(); cfg != nil &&
		cfg.StatusReleasesDependents(target.Status) && !cfg.IsClosedStatus(b.Status) {
		return &MilestoneReleasedError{
			MilestoneID: normalized,
			Status:      target.Status,
			Holding:     cfg.HoldingStatusNames(),
		}
	}

	// Exclusivity along the parent chain, both directions from the subject.
	if ancestor, ms := r.firstAssignedAncestor(b); ancestor != nil {
		return &MilestoneExclusivityError{SubjectID: b.ID, MilestoneID: normalized,
			Relation: "ancestor", ConflictID: ancestor.ID, ConflictMilestoneID: ms}
	}
	if descendant, ms := r.firstAssignedDescendant(b.ID); descendant != nil {
		return &MilestoneExclusivityError{SubjectID: b.ID, MilestoneID: normalized,
			Relation: "descendant", ConflictID: descendant.ID, ConflictMilestoneID: ms}
	}

	b.Milestone = normalized
	if normalized != oldMilestone {
		r.Orderer.Recalculate(ScopeMilestone, b)
	}
	return nil
}

// MilestoneReleasedError is decision 1.5's refusal seen from the assignment
// side: a milestone closed for a reason that RELEASES its dependents plans no
// further work, so open work may not be assigned into it. The close gate refuses
// the same end-state from the other direction; this is the door it cannot see.
//
// It carries the declared holding reasons rather than a status name spelled here,
// so a project declaring none gets no clause instead of advice it cannot follow.
type MilestoneReleasedError struct {
	MilestoneID string
	Status      string
	Holding     []string
}

func (e *MilestoneReleasedError) Error() string {
	holding := ""
	if len(e.Holding) > 0 {
		holding = fmt.Sprintf(", or one closed as %s, which keeps its queue", strings.Join(e.Holding, " / "))
	}
	return fmt.Sprintf("cannot assign open work to milestone %s: it is %s, and a milestone closed for a reason that releases its dependents plans no further work — assign to an open milestone%s",
		e.MilestoneID, e.Status, holding)
}

// MilestoneExclusivityError is decision 1.2's refusal: a nib and one of its
// ancestors are never both assigned.
//
// It is typed rather than a bare fmt.Errorf because it is the one assignment
// refusal a CALLER can route around. validateAndSetMilestone's clear branch
// returns before these two checks run, so dropping the assignment succeeds
// exactly where assigning it fails — and `nibs close` offers --unassign-open as
// the remedy for this class and for no other (cmd/close_queue.go's refusal
// diagnosis). Recognizing the class by message text would make that advice a
// guess, which is what it used to be.
//
// It deliberately carries no Unwrap: there is no cause underneath, and
// mutationErrCode's trailing nib.ErrNotFound test must not be able to claim it.
// It has no class of its own there either, so it stays validation-class, as the
// fmt.Errorf it replaces did.
type MilestoneExclusivityError struct {
	SubjectID           string // the nib being assigned
	MilestoneID         string // the milestone it was being assigned to
	Relation            string // how ConflictID relates to it: "ancestor" or "descendant"
	ConflictID          string // the already-assigned nib on that chain
	ConflictMilestoneID string // the milestone THAT nib is assigned to
}

func (e *MilestoneExclusivityError) Error() string {
	return fmt.Sprintf("cannot assign %s to milestone %s: its %s %s is already assigned to milestone %s (a nib and its ancestor are never both assigned)",
		e.SubjectID, e.MilestoneID, e.Relation, e.ConflictID, e.ConflictMilestoneID)
}

// checkReparentExclusivity refuses a move of b under newParentID that would
// violate assignment exclusivity: something in b's subtree (b itself first)
// is assigned AND something on the new chain (the new parent first, then its
// ancestors) is assigned. Called only for a real change of parent — see
// validateAndSetParent.
func (r *Resolver) checkReparentExclusivity(b *nib.Nib, newParentID string) error {
	var below *nib.Nib
	var belowMS string
	if ms := resolvedMilestoneID(b, r.Reader); ms != "" {
		below, belowMS = b, ms
	} else {
		below, belowMS = r.firstAssignedDescendant(b.ID)
	}
	if below == nil {
		return nil
	}
	parent, err := r.Reader.Get(newParentID)
	if err != nil {
		return nil
	}
	var above *nib.Nib
	var aboveMS string
	if ms := resolvedMilestoneID(parent, r.Reader); ms != "" {
		above, aboveMS = parent, ms
	} else {
		above, aboveMS = r.firstAssignedAncestor(parent)
	}
	if above == nil {
		return nil
	}
	return fmt.Errorf("cannot move %s under %s: %s is assigned to milestone %s and %s is assigned to milestone %s (a nib and its ancestor are never both assigned)",
		b.ID, newParentID, below.ID, belowMS, above.ID, aboveMS)
}

// firstAssignedAncestor walks b's resolved parent chain and returns the
// nearest ancestor with a resolved milestone assignment, and that milestone's
// id — or nil when no ancestor is assigned.
func (r *Resolver) firstAssignedAncestor(b *nib.Nib) (*nib.Nib, string) {
	for _, ancestor := range liveParentChain(b, r.Reader, map[string]bool{b.ID: true}) {
		if ms := resolvedMilestoneID(ancestor, r.Reader); ms != "" {
			return ancestor, ms
		}
	}
	return nil, ""
}

// firstAssignedDescendant walks the structural subtree under id (breadth
// first, cycle-safe) and returns the first nib with a resolved milestone
// assignment, and that milestone's id — or nil when none is assigned. Children
// are read the way the ordering engine's parent scope reads them, through the
// reader's incoming parent links.
func (r *Resolver) firstAssignedDescendant(id string) (*nib.Nib, string) {
	visited := map[string]bool{id: true}
	queue := []string{id}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, link := range r.Reader.FindIncomingLinks(cur) {
			if link.LinkType != "parent" || visited[link.FromNib.ID] {
				continue
			}
			visited[link.FromNib.ID] = true
			if ms := resolvedMilestoneID(link.FromNib, r.Reader); ms != "" {
				return link.FromNib, ms
			}
			queue = append(queue, link.FromNib.ID)
		}
	}
	return nil, ""
}

// scopeFromModel maps the wire enum onto the ordering engine's scope. The
// switch is exhaustive over model.AllOrderScope; gqlgen refuses any other
// value at the boundary, so the fallthrough is unreachable.
func scopeFromModel(scope model.OrderScope) Scope {
	if scope == model.OrderScopeMilestone {
		return ScopeMilestone
	}
	return ScopeParent
}

// preValidateSubject runs the subject's write-free guards — enum validity, a
// missing ifMatch under require_if_match, and an ifMatch that disagrees with the
// on-disk content — so a caller that will be told the mutation failed has not
// already had it write to some OTHER nib's file.
//
// updateNib has two kinds of foreign write, and this one call precedes both. The
// blocking handlers persist each target immediately (single-side storage puts
// the edge in the target's blocked_by). And a parent change recalculates the
// subject's order key, which reads the sibling set — a read that repairs lazily:
// Orderer.backfillKeys PERSISTS an order key to any sibling that has none,
// so an ordinary read path leaves a durable edit on a nib the mutation never
// named. That second one is reachable from BOTH calls to validateAndSetParent —
// the type-change branch as well as the parent block — which is why updateNib
// applies all four enum fields before this check and defers the type-change
// branch until after it. Every one of these runs before Writer.Update applies
// these same guards to the subject.
//
// This mirrors validateIfMatchETags in bulkreorder.go, which pre-checks each
// listed nib's etag against on-disk content before the batch writes anything.
// Reader.CurrentETag and NibWriter.Update both derive their etag through
// nibcore.Core.computeStoredETag, so the pre-check compares the same notion of
// etag the write will and cannot pass here only to fail there for a mismatched
// derivation.
//
// It narrows the failure surface rather than closing it, exactly as the reorder
// family documents for its own pre-validation. The guards that need the write
// lock stay inside Writer.Update — flock acquisition, a concurrent delete, and
// the subject's own write I/O — and a concurrent write to the subject landing
// between this check and Writer.Update still surfaces as an ETagMismatchError
// with the targets already persisted. Closing that window would mean staging the
// target writes until after the subject commits, which diverges from the
// established pattern and changes what a failed target write means once the
// subject is already durable.
func (r *mutationResolver) preValidateSubject(b *nib.Nib, ifMatch *string) error {
	if err := r.Validator.ValidateEnums(b); err != nil {
		return err
	}

	// The axis rule (a milestone takes neither assignment axis) is pure — no
	// config, no store state — so it is checked directly rather than through the
	// Validator. Writer.Update repeats it under the write lock; this call, like
	// ValidateEnums above, only refuses the doomed subject early.
	if err := nibtypes.ValidateAxes(b.EffectiveType(), b.Milestone, b.Area); err != nil {
		return err
	}

	// The area vocabulary runs after the axis rule because no area value can
	// satisfy that rule: a milestone takes no area at all, so answering one
	// carrying an undeclared area with the declared set would prescribe a remedy
	// the subject cannot follow, and callers here stop at the first error. It
	// reads per-config state, unlike everything above it — the same footing
	// requireIfMatch below already stands on.
	if err := r.Validator.ValidateArea(b); err != nil {
		return err
	}

	if ifMatch == nil || *ifMatch == "" {
		if r.requireIfMatch() {
			return &nibcore.ETagRequiredError{}
		}
		return nil
	}

	current, err := r.Reader.CurrentETag(b.ID)
	if err != nil {
		// An uncertifiable on-disk file (unparseable/unreadable) surfaces the
		// distinct, NON-RECONCILABLE OnDiskUnparseableError, which carries no etag
		// token a reconcile-retry could echo back. Propagate it unwrapped, as
		// Core.Update does, so the classification survives to the client.
		return err
	}
	if current != *ifMatch {
		return &nibcore.ETagMismatchError{Provided: *ifMatch, Current: current}
	}
	return nil
}

// PreValidateSubject exposes preValidateSubject to callers outside this package
// that make foreign writes of their OWN before the subject's mutation runs —
// `nibs close`, whose queue dispositions rewrite a milestone's assignees before
// the milestone itself is written. Such a caller has the same obligation
// updateNib has (not to leave a durable edit on a nib the command never named
// for a subject that was doomed anyway) and therefore needs the same guard set,
// not a second one that can drift away from it: a guard added to
// preValidateSubject must reach every foreign-write path at once.
//
// b carries the PENDING values of the fields this check READS — the enum
// fields, and EffectiveType/Milestone/Area for the axis rule — applied to a
// Clone, never to the stored nib. Validating them as read would refuse a
// mutation whose whole purpose is to replace the offending value: updateNib
// applies the enum fields before calling this, and `nibs close` applies the
// status it is about to write.
//
// It is deliberately NOT "the nib as it will be written", which no caller can
// supply: `nibs close` also writes a ## Summary body entry, and it cannot build
// that until after the queue dispositions this check exists to run BEFORE. That
// costs nothing while the guard set stays inside the fields above — none of
// them is Body — but it bounds the invariant above: a guard added here that
// reads a field a caller cannot prepare would silently judge that caller's
// write on stale input, so it has to arrive with the means to prepare it.
func (r *Resolver) PreValidateSubject(b *nib.Nib, ifMatch *string) error {
	return (&mutationResolver{r}).preValidateSubject(b, ifMatch)
}

// validateAndAddBlocking validates and adds blocking relationships.
// Single-side storage: adds b.ID to each target's blockedBy list.
// Two-phase approach: validate ALL targets first, then apply ALL mutations.
// This ensures no targets are mutated if any validation fails.
func (r *Resolver) validateAndAddBlocking(b *nib.Nib, targetIDs []string) error {
	// Phase 1: validate all targets, collecting their normalized IDs (not their
	// pointers — see Phase 2). updateTargetClone re-fetches each fresh by ID in
	// Phase 2, so only the resolved ID is retained here.
	targets := make([]string, 0, len(targetIDs))

	for _, targetID := range targetIDs {
		normalizedTargetID, ok := r.Reader.NormalizeID(targetID)
		if !ok {
			return fmt.Errorf("blocking target nib not found: %s", targetID)
		}

		if normalizedTargetID == b.ID {
			return fmt.Errorf("nib cannot block itself")
		}

		if _, err := r.Reader.Get(normalizedTargetID); err != nil {
			return fmt.Errorf("blocking target nib not found: %s", targetID)
		}

		// Check for cycles via blocked_by links
		if cycle := r.Validator.DetectCycle(normalizedTargetID, "blocked_by", b.ID); cycle != nil {
			return fmt.Errorf("adding blocking relationship would create cycle: %v", cycle)
		}

		targets = append(targets, normalizedTargetID)
	}

	// Phase 2: apply all mutations (all targets validated successfully).
	// updateTargetClone re-fetches each target FRESH via Reader.GetForUpdate at
	// the point of mutation — never reusing a Phase-1 pointer. A successful
	// Writer.Update installs the CLONE as the new c.nibs[id], orphaning any
	// earlier pointer; with a duplicate target ID the second iteration would
	// otherwise hold a stale pre-mutation pointer, compute a stale if-match, and
	// spuriously fail with an ETagMismatchError after target 1 was already
	// persisted. The fetched clone is what mutate touches, so a
	// genuinely refused write leaves the shared in-memory nib untouched.
	for _, targetID := range targets {
		if err := r.updateTargetClone(targetID, func(c *nib.Nib) bool {
			c.AddBlockedBy(b.ID)
			return true
		}); err != nil {
			return err
		}
	}
	return nil
}

// removeBlockingRelationships removes blocking relationships.
// Single-side storage: removes b.ID from each target's blockedBy list.
func (r *Resolver) removeBlockingRelationships(b *nib.Nib, targetIDs []string) error {
	for _, targetID := range targetIDs {
		normalizedTargetID, _ := r.Reader.NormalizeID(targetID)
		// Guard existence first so a missing target stays a no-op — updateTargetClone
		// would otherwise surface GetForUpdate's not-found error. The write itself
		// goes through an owned clone, never the shared pointer.
		if _, err := r.Reader.Get(normalizedTargetID); err == nil {
			if err := r.updateTargetClone(normalizedTargetID, func(c *nib.Nib) bool {
				return c.RemoveBlockedBy(b.ID)
			}); err != nil {
				return fmt.Errorf("failed to remove blocking from %s: %w", normalizedTargetID, err)
			}
		}
	}
	return nil
}

// validateAndAddBlockedBy validates and adds blocked-by relationships.
// Single-side storage: modifies b's blockedBy list directly.
//
// Caller must pass a nib it owns (a clone), not a shared Reader.Get pointer —
// this mutates b in place.
func (r *Resolver) validateAndAddBlockedBy(b *nib.Nib, targetIDs []string) error {
	for _, targetID := range targetIDs {
		normalizedTargetID, ok := r.Reader.NormalizeID(targetID)
		if !ok {
			return fmt.Errorf("blocker nib not found: %s", targetID)
		}

		if normalizedTargetID == b.ID {
			return fmt.Errorf("nib cannot be blocked by itself")
		}

		if _, err := r.Reader.Get(normalizedTargetID); err != nil {
			return fmt.Errorf("blocker nib not found: %s", targetID)
		}

		if cycle := r.Validator.DetectCycle(b.ID, "blocked_by", normalizedTargetID); cycle != nil {
			return fmt.Errorf("adding blocked-by relationship would create cycle: %v", cycle)
		}

		b.AddBlockedBy(normalizedTargetID)
	}
	return nil
}

// removeBlockedByRelationships removes blocked-by relationships.
// Single-side storage: modifies b's blockedBy list directly.
//
// Caller must pass a nib it owns (a clone), not a shared Reader.Get pointer —
// this mutates b in place.
func (r *Resolver) removeBlockedByRelationships(b *nib.Nib, targetIDs []string) {
	for _, targetID := range targetIDs {
		normalizedTargetID, _ := r.Reader.NormalizeID(targetID)
		b.RemoveBlockedBy(normalizedTargetID)
	}
}

// activateParentChain walks up the parent chain, setting any todo/draft
// parents to in-progress. Those two statuses are the whole activation set: the
// walk stops at a parent in any other status (or one with no parent), so an
// in-progress ancestor is already active and a closed one — completed,
// scrapped or deferred — stays closed. A child going in-progress never reopens
// a closed parent.
// Best-effort: warns on stderr and stops on any error. Mutates an owned clone
// (from GetForUpdate) before each Update — as UpdateNib does — so a refused write
// never corrupts the shared in-memory nib.
//
// Stop-on-first-error is a deliberate atomicity choice, NOT laziness.
// The walk does not skip a refused ancestor to activate the ones above it. The
// invariant being maintained is "ancestors of an in-progress nib are active";
// activating a grandparent while this parent is left todo/draft would violate that
// invariant more visibly (an active nib sitting under a non-active one) than simply
// stopping. A refused write is almost always a genuine on-disk divergence (stale
// etag) or a transient write error, so leaving the remaining chain untouched keeps
// the store self-consistent, and the next child-start re-triggers the walk from the
// bottom — so a partial stop self-heals rather than corrupting. The warning names
// the exact ancestor the walk stopped at so the omission is diagnosable.
func (r *Resolver) activateParentChain(childID, parentID string) {
	for parentID != "" {
		parent, err := r.Reader.Get(parentID)
		if err != nil || parent == nil {
			return
		}
		if parent.Status != "todo" && parent.Status != "draft" {
			return // already active or closed, stop
		}
		nextParentID := parent.Parent
		// Reader.Get above returns the SHARED in-memory pointer (nibcore.Core.Get
		// hands back c.nibs[id] directly, not a defensive copy) — read-only, used
		// only for the status gate and next-parent. Compute the if-match from the
		// parent's current etag, then mutate an OWNED clone from GetForUpdate —
		// never the shared pointer — so a failed Update (genuine on-disk divergence
		// -> ETagMismatchError) leaves the in-memory nib untouched, rather than
		// corrupting the store to show in-progress while disk was never written.
		//
		// Caveat: parent.ETag() can still false-conflict for a reloaded nib whose
		// on-disk file omits created_at/updated_at (loadNib synthesizes those from
		// the file's mtime while the stored etag bare-parses), spuriously dropping
		// activation for such hand-authored files. The priority/type axis of this
		// false-conflict does not arise: loadNib keeps a default-omitting nib's
		// Type/Priority empty, so a missing priority:/type: line does not diverge.
		// Do NOT substitute CurrentETag here — that causes a lost-update/data-loss
		// regression (guarded by TestActivateParentChainGenuineDivergenceIsRefused).
		parentETag := parent.ETag()
		updated, err := r.Reader.GetForUpdate(parentID)
		if err != nil {
			return
		}
		updated.Status = "in-progress"
		if err := r.Writer.Update(updated, &parentETag); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not activate ancestor %s (from %s): %v — chain activation stops at this ancestor; it and any higher todo/draft ancestors stay unactivated until the next child-start re-triggers the walk\n", parentID, childID, err)
			return
		}
		parentID = nextParentID
	}
}

// isStartableStatus delegates to config.IsStartableStatus — the canonical
// status half of "can I start this?" — reached through the reader's config so
// this package keeps no status list of its own.
func (r *Resolver) isStartableStatus(status string) bool {
	return r.Reader.Config().IsStartableStatus(status)
}

// releasesDependents delegates to config.StatusReleasesDependents — the
// canonical answer to "does a blocker in this status still count" — reached
// through the reader's config so this package keeps no status list of its own.
// Narrower than config.IsClosedStatus: a deferred blocker is closed but still
// blocks.
func (r *Resolver) releasesDependents(status string) bool {
	return r.Reader.Config().StatusReleasesDependents(status)
}

// validateDocumentPaths checks that document paths are safe (no absolute paths or path traversal).
func validateDocumentPaths(paths []string) error {
	for _, p := range paths {
		if filepath.IsAbs(p) || strings.HasPrefix(p, "/") {
			return fmt.Errorf("document path must be relative: %s", p)
		}
		cleaned := filepath.ToSlash(filepath.Clean(p))
		if strings.HasPrefix(cleaned, "..") {
			return fmt.Errorf("document path must not contain path traversal: %s", p)
		}
	}
	return nil
}

// newPrefixedNibID is the id generator CreateNib's custom-prefix path draws
// from. A variable so the collision tests can seed deterministic draws;
// nothing in production reassigns it.
var newPrefixedNibID = nib.NewID
