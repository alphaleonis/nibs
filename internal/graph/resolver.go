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

// validateAndSetParent validates and sets the parent relationship.
// When the parent changes, the order key is recalculated to avoid collisions
// with existing siblings in the new parent group.
func (r *Resolver) validateAndSetParent(b *nib.Nib, parentID string) error {
	oldParent := b.Parent

	if parentID == "" {
		b.Parent = ""
		if oldParent != "" {
			r.Orderer.RecalculateOrder(b)
		}
		return nil
	}

	// Normalise short ID to full ID
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
	// Phase 1: validate all targets
	type validatedTarget struct {
		id     string
		target *nib.Nib
	}
	targets := make([]validatedTarget, 0, len(targetIDs))

	for _, targetID := range targetIDs {
		normalizedTargetID, ok := r.Reader.NormalizeID(targetID)
		if !ok {
			return fmt.Errorf("blocking target nib not found: %s", targetID)
		}

		if normalizedTargetID == b.ID {
			return fmt.Errorf("nib cannot block itself")
		}

		target, err := r.Reader.Get(normalizedTargetID)
		if err != nil {
			return fmt.Errorf("blocking target nib not found: %s", targetID)
		}

		// Check for cycles via blocked_by links
		if cycle := r.Validator.DetectCycle(normalizedTargetID, "blocked_by", b.ID); cycle != nil {
			return fmt.Errorf("adding blocking relationship would create cycle: %v", cycle)
		}

		targets = append(targets, validatedTarget{id: normalizedTargetID, target: target})
	}

	// Phase 2: apply all mutations (all targets validated successfully)
	for _, vt := range targets {
		targetETag := vt.target.ETag()
		vt.target.AddBlockedBy(b.ID)
		if err := r.Writer.Update(vt.target, &targetETag); err != nil {
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
		if target, err := r.Reader.Get(normalizedTargetID); err == nil {
			targetETag := target.ETag()
			if target.RemoveBlockedBy(b.ID) {
				if err := r.Writer.Update(target, &targetETag); err != nil {
					return fmt.Errorf("failed to remove blocking from %s: %w", normalizedTargetID, err)
				}
			}
		}
	}
	return nil
}

// validateAndAddBlockedBy validates and adds blocked-by relationships.
// Single-side storage: modifies b's blockedBy list directly.
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
// Best-effort: warns on stderr and stops on any error. Mutates a clone before
// each Update (clone-before-mutate, as UpdateNib does with b := original.Clone())
// so a refused write never corrupts the shared in-memory nib.
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
		// Reader.Get returns the SHARED in-memory pointer (nibcore.Core.Get hands
		// back c.nibs[id] directly, not a defensive copy). Compute the if-match
		// from the parent's current etag, then mutate a CLONE — never the shared
		// pointer — so a failed Update (genuine on-disk divergence ->
		// ETagMismatchError) leaves the in-memory nib untouched, rather than
		// corrupting the store to show in-progress while disk was never written.
		//
		// Caveat: parent.ETag() can still false-conflict for a reloaded nib whose
		// on-disk file omits created_at/updated_at (loadNib synthesizes those from
		// the file's mtime while the stored etag bare-parses), spuriously dropping
		// activation for such hand-authored files. The priority/type axis of this
		// false-conflict is fixed (nibs-7d3o: loadNib keeps a default-omitting nib's
		// Type/Priority empty, so a missing priority:/type: line no longer diverges).
		// Do NOT substitute CurrentETag here — that reintroduces the
		// reverted lost-update/data-loss regression (see nibs-znt8/nibs-7d3o,
		// guarded by TestActivateParentChainGenuineDivergenceIsRefused).
		parentETag := parent.ETag()
		updated := parent.Clone()
		updated.Status = "in-progress"
		if err := r.Writer.Update(updated, &parentETag); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to activate parent %s (from %s): %v\n", parentID, childID, err)
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
