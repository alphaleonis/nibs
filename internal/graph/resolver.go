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

// What survives gqlgen codegen in schema.resolvers.go: gqlgen rewrites that file
// on every codegen and carries parts of the existing one into the new one.
// Surviving: resolver bodies, a resolver's doc comment (a comment directive in
// doc position included), and the import block. Dropped, silently: a
// free-standing comment attached to no declaration, and a non-resolver
// declaration, which is moved to a commented-out block at the end with its doc
// comment discarded.
//
// Put a durable note about that file on a resolver's doc comment, or here —
// gqlgen writes this file only when it is absent.

//go:generate go tool gqlgen generate

// Resolver is the root resolver for the GraphQL schema.
type Resolver struct {
	Reader     NibReader
	Writer     NibWriter
	Validator  NibValidator
	Blocking   BlockingChecker
	Subscriber NibSubscriber
	Orderer    *Orderer
	AreaWriter AreaWriter
	// Version is the running binary version; empty or "dev" disables the
	// updateStatus check.
	Version string
}

// checkMutualExclusion errors when the replace field and any delta field are
// both non-nil.
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

// isNilValue reports whether v is nil, including a typed nil pointer/slice/map
// boxed in an any.
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
// OWNED clone via Reader.GetForUpdate, applies mutate to it, and writes it. The
// SHARED c.nibs[id] pointer is never mutated, so a refused Writer.Update leaves
// the in-memory nib untouched instead of showing a phantom mutation.
//
// The fetch is FRESH per call, so a duplicate target id re-reads the nib the
// first Update installed rather than computing a stale if-match. The if-match is
// the target's PRE-mutation ETag, taken from that clone. mutate returns false for
// "nothing changed" and nothing is written. A missing target is an id-bearing
// not-found error, so guard existence first where it should be a no-op.
func (r *Resolver) updateTargetClone(id string, mutate func(*nib.Nib) bool) error {
	clone, err := r.Reader.GetForUpdate(id)
	if err != nil {
		return fmt.Errorf("target nib not found: %s: %w", id, err)
	}
	ifMatch := clone.ETag()
	if !mutate(clone) {
		return nil
	}
	return r.Writer.Update(clone, &ifMatch)
}

// snapshotResult returns a detached GetSnapshot clone of the nib a mutation just
// wrote, so gqlgen never marshals the live c.nibs pointer. Return a nib from a
// mutation resolver through this helper: the value the resolver would otherwise
// return aliases the store, and only a clone taken under the store lock is safe
// to hand out (see NibReader.GetSnapshot for the rule). A !ok means the nib
// vanished between the write and the snapshot (a concurrent delete): report it
// rather than return a nil nib for the non-null result.
func (r *Resolver) snapshotResult(id string) (*nib.Nib, error) {
	snap, ok := r.Reader.GetSnapshot(id)
	if !ok {
		return nil, fmt.Errorf("nib not found after write: %s: %w", id, nib.ErrNotFound)
	}
	return snap, nil
}

// snapshotResults is the slice form of snapshotResult for the bulk-reorder
// resolvers, preserving order. A !ok is NOT an error here: every input nib was
// just written by the reorder loop, so a miss means a concurrent delete in the
// lock-free window after its order-key write committed. Skip the vanished element
// — the persisted order among the survivors still holds.
func (r *Resolver) snapshotResults(nibs []*nib.Nib) ([]*nib.Nib, error) {
	out := make([]*nib.Nib, 0, len(nibs))
	for _, b := range nibs {
		snap, ok := r.Reader.GetSnapshot(b.ID)
		if !ok {
			continue
		}
		out = append(out, snap)
	}
	return out, nil
}

// validateAndSetParent validates and sets the parent relationship, recalculating
// the order key when the parent changes. b must be a nib the caller owns (a
// clone) — this mutates b.Parent and, via Orderer.Recalculate, b.Order in place.
//
// "Changes" is decided from the RESOLVED old parent (see resolvedParent), not the
// stored string. The raw reading counts a dangling link's repair as a change and
// sends an already-root nib to the end of the root order; Core.FixBrokenLinks
// repairs that same link without touching Order.
func (r *Resolver) validateAndSetParent(b *nib.Nib, parentID string) error {
	oldParent := resolvedParentID(b, r.Reader)

	if parentID == "" {
		b.Parent = ""
		if oldParent != "" {
			r.Orderer.Recalculate(ScopeParent, b)
		}
		return nil
	}

	normalizedParent, ok := r.Reader.NormalizeID(parentID)
	if !ok {
		return fmt.Errorf("parent nib not found: %s", parentID)
	}

	if err := r.Validator.ValidateParent(b, normalizedParent); err != nil {
		return err
	}

	if cycle := r.Validator.DetectCycle(b.ID, "parent", normalizedParent); cycle != nil {
		return fmt.Errorf("setting parent would create cycle: %v", cycle)
	}

	// Only a REAL change of parent is checked: the type-change branch re-validates
	// the existing parent through this same call, and a pre-existing conflict in
	// hand-edited data must not dead-end every type change on such a nib.
	// `nibs check` names that shape.
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

// validateAndSetMilestone validates and sets the milestone assignment, the
// assignment-side mirror of validateAndSetParent. b must be a nib the caller owns
// (a clone) — this mutates b.Milestone and, via Orderer.Recalculate,
// b.MilestoneOrder in place.
//
// "Assigned" throughout is the RESOLVED reading (membership.ResolvedMilestoneID):
// a dangling or non-milestone assignment schedules nothing, so it conflicts with
// nothing. On a change of queue the nib re-enters the new queue last through
// Orderer.Recalculate, and clearing drops the key along with the assignment; a
// key is never carried from one queue to another.
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
	// Decision 1.5's ASSIGNMENT door: the close gate refuses a close over a live
	// queue, and this refuses the same end-state reached from the other side, by
	// assigning AFTER the close. Scoped to an OPEN subject — retro-assigning
	// finished work to a finished wave plans nothing for a wave that ended. A
	// HOLDING reason keeps accepting work. Which reasons release is config's
	// answer, never a literal here.
	//
	// It must stay AFTER ValidateAxes: that rule is about the SUBJECT and no
	// property of the target can satisfy it, so answering first with the target's
	// status would hand back a remedy the subject cannot follow.
	if cfg := r.Reader.Config(); cfg != nil &&
		cfg.StatusReleasesDependents(target.Status) && !cfg.IsClosedStatus(b.Status) {
		return &MilestoneReleasedError{
			MilestoneID: normalized,
			Status:      target.Status,
			Holding:     cfg.HoldingStatusNames(),
		}
	}

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
// further work, so open work may not be assigned into it.
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
// ancestors are never both assigned. It is typed because cmd/close_queue.go
// recognizes this class by type to offer --unassign-open, which it offers for no
// other assignment refusal.
//
// Add no Unwrap: mutationErrCode's trailing nib.ErrNotFound test must not be able
// to claim it, and it has no class of its own there, so it stays
// validation-class.
type MilestoneExclusivityError struct {
	SubjectID           string
	MilestoneID         string
	Relation            string // how ConflictID relates to it: "ancestor" or "descendant"
	ConflictID          string // the already-assigned nib on that chain
	ConflictMilestoneID string // the milestone THAT nib is assigned to
}

func (e *MilestoneExclusivityError) Error() string {
	return fmt.Sprintf("cannot assign %s to milestone %s: its %s %s is already assigned to milestone %s (a nib and its ancestor are never both assigned)",
		e.SubjectID, e.MilestoneID, e.Relation, e.ConflictID, e.ConflictMilestoneID)
}

// checkReparentExclusivity refuses a move of b under newParentID that would
// violate assignment exclusivity: something in b's subtree (b itself first) is
// assigned AND something on the new chain (the new parent first, then its
// ancestors) is assigned.
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

// firstAssignedAncestor returns the nearest ancestor on b's resolved parent chain
// with a resolved milestone assignment, and that milestone's id — or nil when no
// ancestor is assigned.
func (r *Resolver) firstAssignedAncestor(b *nib.Nib) (*nib.Nib, string) {
	for _, ancestor := range liveParentChain(b, r.Reader, map[string]bool{b.ID: true}) {
		if ms := resolvedMilestoneID(ancestor, r.Reader); ms != "" {
			return ancestor, ms
		}
	}
	return nil, ""
}

// firstAssignedDescendant returns the first nib in the structural subtree under
// id with a resolved milestone assignment, and that milestone's id — or nil when
// none is assigned.
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

// scopeFromModel maps the wire enum onto the ordering engine's scope.
// model.OrderScope.UnmarshalGQL admits only PARENT and MILESTONE.
func scopeFromModel(scope model.OrderScope) Scope {
	if scope == model.OrderScopeMilestone {
		return ScopeMilestone
	}
	return ScopeParent
}

// preValidateSubject runs the subject's write-free guards, so a mutation that
// will fail has not already written to some OTHER nib's file.
//
// updateNib has two kinds of foreign write and this one call precedes both: the
// blocking handlers persist each target immediately, and a parent change
// recalculates the order key through Orderer.backfillKeys, which PERSISTS a key
// to any sibling that has none. The second is reachable from BOTH calls to
// validateAndSetParent, so updateNib applies all four enum fields before this
// check and defers the type-change branch until after it.
//
// The window is narrowed, not closed: a concurrent write to the subject landing
// between this check and Writer.Update still fails with the targets persisted.
func (r *mutationResolver) preValidateSubject(b *nib.Nib, ifMatch *string) error {
	if err := r.Validator.ValidateEnums(b); err != nil {
		return err
	}

	// The axis rule (a milestone takes neither assignment axis) is pure — no
	// config, no store state — so it is checked here directly.
	if err := nibtypes.ValidateAxes(b.EffectiveType(), b.Milestone, b.Area); err != nil {
		return err
	}

	// After the axis rule: a milestone takes no area at all, so answering an
	// undeclared area first would prescribe a remedy the subject cannot follow.
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
		// Propagate unwrapped, as Core.Update does: OnDiskUnparseableError is
		// non-reconcilable, and that classification must survive to the client.
		return err
	}
	if current != *ifMatch {
		return &nibcore.ETagMismatchError{Provided: *ifMatch, Current: current}
	}
	return nil
}

// PreValidateSubject exposes preValidateSubject to callers outside this package
// that make foreign writes of their OWN first — `nibs close`, whose queue
// dispositions rewrite a milestone's assignees before the milestone itself is
// written. Such a caller needs this same guard set, not a second one that can
// drift from it.
//
// b carries the PENDING values of the fields this check READS — the enum fields,
// and EffectiveType/Milestone/Area for the axis rule — applied to a Clone, never
// to the stored nib; validating them as read would refuse a mutation whose whole
// purpose is to replace the offending value. b is not the nib as it will be
// written (`nibs close` has not built its ## Summary entry yet), so a guard added
// to preValidateSubject must read only fields every caller can prepare here.
func (r *Resolver) PreValidateSubject(b *nib.Nib, ifMatch *string) error {
	return (&mutationResolver{r}).preValidateSubject(b, ifMatch)
}

// validateAndAddBlocking adds b.ID to each target's blockedBy list (single-side
// storage). Every target is validated before any is mutated.
func (r *Resolver) validateAndAddBlocking(b *nib.Nib, targetIDs []string) error {
	// Phase 1: validate every target, retaining only its normalized id.
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

		if cycle := r.Validator.DetectCycle(normalizedTargetID, "blocked_by", b.ID); cycle != nil {
			return fmt.Errorf("adding blocking relationship would create cycle: %v", cycle)
		}

		targets = append(targets, normalizedTargetID)
	}

	// Phase 2: apply all mutations. updateTargetClone re-fetches each target at the
	// point of mutation; never reuse a Phase-1 pointer.
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

// removeBlockingRelationships removes b.ID from each target's blockedBy list
// (single-side storage).
func (r *Resolver) removeBlockingRelationships(b *nib.Nib, targetIDs []string) error {
	for _, targetID := range targetIDs {
		normalizedTargetID, _ := r.Reader.NormalizeID(targetID)
		// Guard existence first: a missing target stays a no-op instead of surfacing
		// updateTargetClone's not-found error.
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

// validateAndAddBlockedBy adds each target to b's own blockedBy list (single-side
// storage). b must be a nib the caller owns (a clone) — this mutates it in place.
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

// removeBlockedByRelationships removes each target from b's own blockedBy list
// (single-side storage). b must be a nib the caller owns (a clone) — this mutates
// it in place.
func (r *Resolver) removeBlockedByRelationships(b *nib.Nib, targetIDs []string) {
	for _, targetID := range targetIDs {
		normalizedTargetID, _ := r.Reader.NormalizeID(targetID)
		b.RemoveBlockedBy(normalizedTargetID)
	}
}

// activateParentChain walks up the parent chain, setting any todo/draft parent to
// in-progress. Those two statuses are the whole activation set: the walk stops at
// a parent in any other status, so a closed ancestor — completed, scrapped or
// deferred — stays closed and a child going in-progress never reopens one.
//
// Best-effort: it warns on stderr and stops at the first refused write rather
// than skipping that ancestor to activate the ones above it, which would leave an
// active nib under a non-active one.
func (r *Resolver) activateParentChain(childID, parentID string) {
	for parentID != "" {
		parent, err := r.Reader.Get(parentID)
		if err != nil || parent == nil {
			return
		}
		if parent.Status != "todo" && parent.Status != "draft" {
			return
		}
		nextParentID := parent.Parent
		// Reader.Get above returns the SHARED in-memory pointer — read-only here,
		// for the status gate and the next parent. Take the if-match from its
		// current in-memory etag and mutate the OWNED clone from GetForUpdate. Do
		// NOT substitute CurrentETag: that is a lost-update regression, guarded by
		// TestActivateParentChainGenuineDivergenceIsRefused.
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

func (r *Resolver) isStartableStatus(status string) bool {
	return r.Reader.Config().IsStartableStatus(status)
}

// releasesDependents is narrower than config.IsClosedStatus: a deferred blocker
// is closed but still blocks.
func (r *Resolver) releasesDependents(status string) bool {
	return r.Reader.Config().StatusReleasesDependents(status)
}

// validateDocumentPaths rejects an absolute path or one that traverses upward.
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

// newPrefixedNibID is the id generator CreateNib's custom-prefix path draws from.
// A variable so the collision tests can seed deterministic draws.
var newPrefixedNibID = nib.NewID
