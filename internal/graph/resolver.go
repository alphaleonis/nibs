package graph

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/alphaleonis/nibs/internal/nib"
)

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
// Caller must pass a nib it owns (a clone), not a shared Reader.Get pointer —
// this mutates b (b.Parent and, via RecalculateOrder, b.Order) in place.
func (r *Resolver) validateAndSetParent(b *nib.Nib, parentID string) error {
	oldParent := b.Parent

	if parentID == "" {
		b.Parent = ""
		if oldParent != "" {
			r.Orderer.RecalculateOrder(b)
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

	b.Parent = normalizedParent
	if normalizedParent != oldParent {
		r.Orderer.RecalculateOrder(b)
	}
	return nil
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
// parents to in-progress. Stops when it reaches a parent that is already
// in-progress, deferred, completed, or scrapped (or has no parent). A deferred
// parent is parked, so it is left untouched — a child going in-progress does
// not un-park it.
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
			return // already active or resolved, stop
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

// isResolvedStatus delegates to nib.IsResolvedStatus — the canonical definition.
func isResolvedStatus(status string) bool {
	return nib.IsResolvedStatus(status)
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
