package nibcore

import (
	"slices"
	"strings"

	"github.com/alphaleonis/nibs/internal/nib"
)

// CANONICAL INVARIANT (link-id canonicalization at the disk-read boundary).
// This doc is its single authoritative statement; siblings across
// internal/nibcore and internal/membershipcontract defer here rather than
// re-derive it. The rule: every id stored in c.nibs is a FULL id.
//
// A nib file may name its parent, milestone or a blocker by SHORT id
// (`parent: par` rather than `parent: nibs-par`). The forward resolvers
// normalize such an id when they follow it; the reverse traversals
// (findIncomingLinksInMap, isBlockingInMap) and the cycle passes
// (FindCyclesInMap, findPathToTargetInMap) walk exact map keys and do not. The
// sweeps here resolve stored link ids to their full form, so those exact lookups
// find the link.
//
// Nothing here rewrites a file. The store holds the full form, so the next
// unrelated write to that nib persists it; computeStoredETag canonicalizes the
// same way, so a canonicalized nib and its short-form file do not false-conflict
// an if-match Update in the meantime. An UNRESOLVABLE id stays verbatim, and
// `nibs check` reports it broken against the spelling in the file.
//
// What a stored id resolves to is a property of the KEY SET, not of the id
// alone: a store holding both a bare token `e1` and its prefixed twin `nibs-e1`
// keeps a raw `parent: e1` verbatim, and removing `e1` makes that same spelling
// fall through to `nibs-e1`. Whoever changes the key set re-runs the sweep —
// Core.Delete gated on removalCanRebindLinksLocked, Core.Create
// unconditionally, the watcher via canonicalizeLinksAfterBatchLocked's scanAll.
// Skipping it leaves the stored spelling naming one nib while Get answers with
// another, invisibly.
//
// Core.Update changes no key set and runs no sweep — it installs the caller's
// nib as given, so resolving a link id before calling it is what keeps the rule
// true on that path.
//
// A sweep re-points links that already resolved, so it resolves from the FILE's
// spelling (nib.RawLinks) and never from the value the store now holds — see
// RawLinks for what keeps each pass idempotent.

// canonicalLinks holds the resolved link fields for one nib, so callers can skip
// the write (and, on published pointers, the clone) when nothing moved. changed
// compares against the nib's CURRENT values, not against the file spelling
// resolution reads from: those two differ permanently on every hand-edited
// short-form nib, so comparing against the file would report a change on every
// sweep forever and turn each into a spurious EventUpdated on the watcher path.
//
// A nil list field means unchanged. Resolution never empties a non-empty list,
// so that is unambiguous, and it keeps applyTo from overwriting loadNib's
// empty-slice defaults with a nil.
type canonicalLinks struct {
	parent    string
	milestone string
	blockedBy []string
	blocking  []string
	changed   bool
}

// applyTo writes the resolved fields onto b. Call it only when changed is true:
// a set reporting no change may be the zero value, which would clear Parent and
// Milestone.
func (s canonicalLinks) applyTo(b *nib.Nib) {
	b.Parent = s.parent
	b.Milestone = s.milestone
	if s.blockedBy != nil {
		b.BlockedBy = s.blockedBy
	}
	if s.blocking != nil {
		b.Blocking = s.blocking
	}
}

// canonicalizeLinksInMap resolves b's Parent, Milestone, BlockedBy and legacy
// Blocking ids to their full form against nibs, using the same exact-match-
// then-prefix-prepended rule as Core.Get (normalizeIDInMap). It resolves from
// b's FILE spelling (nib.RawLinks), never from the values b currently holds.
//
// A target that resolves to no nib is carried through unchanged, and an EMPTY
// file spelling where b holds a link is left alone: this pass never invents or
// erases a link.
//
// It does NOT mutate b, so a caller holding an already-published nib can apply
// the result copy-on-write (see NibReader.GetSnapshot in
// internal/graph/interfaces.go).
//
// Pure function over the given map, without locking: a caller passing Core.nibs
// must hold Core.mu for the duration.
func canonicalizeLinksInMap(nibs map[string]*nib.Nib, b *nib.Nib, configPrefix string) canonicalLinks {
	if configPrefix == "" {
		return canonicalLinks{}
	}

	resolve := func(target string) string {
		if full, ok := normalizeIDInMap(nibs, target, configPrefix); ok {
			return full
		}
		return target
	}

	raw := b.RawLinks()

	set := canonicalLinks{parent: b.Parent, milestone: b.Milestone}
	if raw.Parent != "" {
		if resolved := resolve(raw.Parent); resolved != b.Parent {
			set.parent = resolved
			set.changed = true
		}
	}
	if raw.Milestone != "" {
		if resolved := resolve(raw.Milestone); resolved != b.Milestone {
			set.milestone = resolved
			set.changed = true
		}
	}

	// Resolving can collapse two spellings of one target onto the same id
	// (`blocked_by: [blk, nibs-blk]`); the later duplicate is dropped. Returns nil
	// when the result matches what the nib already holds.
	canonicalList := func(rawIDs, current []string) []string {
		if len(rawIDs) == 0 {
			return nil
		}
		out := make([]string, 0, len(rawIDs))
		seen := make(map[string]bool, len(rawIDs))
		for _, id := range rawIDs {
			resolved := resolve(id)
			if seen[resolved] {
				continue
			}
			seen[resolved] = true
			out = append(out, resolved)
		}
		if slices.Equal(out, current) {
			return nil
		}
		return out
	}

	if set.blockedBy = canonicalList(raw.BlockedBy, b.BlockedBy); set.blockedBy != nil {
		set.changed = true
	}
	if set.blocking = canonicalList(raw.Blocking, b.Blocking); set.blocking != nil {
		set.changed = true
	}

	return set
}

// canonicalizeAllLinksUnpublishedLocked resolves every loaded nib's link ids to
// their full form. It runs as a second pass over the whole map because a target
// only resolves once every file has been read.
//
// UNPUBLISHED is the precondition: it mutates the stored nibs IN PLACE, which is
// safe only on the bulk load path, where loadFromDisk builds a fresh c.nibs of
// pointers no off-lock reader can hold. Anything working on published pointers
// must go through canonicalizeStoreLocked or canonicalizeLinksAfterBatchLocked.
// Must be called with c.mu held.
func (c *Core) canonicalizeAllLinksUnpublishedLocked() {
	configPrefix := c.configPrefix()
	if configPrefix == "" {
		return
	}
	for _, b := range c.nibs {
		if set := canonicalizeLinksInMap(c.nibs, b, configPrefix); set.changed {
			set.applyTo(b)
		}
	}
}

// canonicalizeOneLocked re-resolves one stored nib's link ids against the
// current store and installs the rewritten nib under its key, returning the
// fresh pointer. Returns nil when the nib is absent or nothing changed.
//
// Every rewrite is copy-on-write: Parent and Milestone (torn strings) and
// BlockedBy (a memory-unsafe torn slice header) are non-Path fields, so they
// must land on a FRESH pointer rather than on the published one — see the
// canonical live-pointer invariant at NibReader.GetSnapshot
// (internal/graph/interfaces.go). Must be called with c.mu held.
func (c *Core) canonicalizeOneLocked(id, configPrefix string) *nib.Nib {
	b, ok := c.nibs[id]
	if !ok {
		return nil
	}
	set := canonicalizeLinksInMap(c.nibs, b, configPrefix)
	if !set.changed {
		return nil
	}
	updated := b.Clone()
	set.applyTo(updated)
	c.nibs[id] = updated
	return updated
}

// linkRebind names one stored link a canonicalization sweep re-pointed. A sweep
// changes what an already-resolved link answers with no file changing and, off
// the watcher path, no event, so the mutator that triggered one uses these to
// tell the user which OTHER nibs it moved.
type linkRebind struct {
	nibID string
	field string
	from  string
	to    string
}

func (r linkRebind) String() string {
	return r.nibID + "." + r.field + ": " + r.from + " -> " + r.to
}

// describeRebinds reports the link fields that differ between a stored nib and
// its re-resolved replacement. A list is reported whole: resolution can collapse
// duplicates, so entries do not line up one to one with the originals.
func describeRebinds(before, after *nib.Nib) []linkRebind {
	var out []linkRebind
	if before.Parent != after.Parent {
		out = append(out, linkRebind{nibID: after.ID, field: "parent", from: before.Parent, to: after.Parent})
	}
	if before.Milestone != after.Milestone {
		out = append(out, linkRebind{nibID: after.ID, field: "milestone", from: before.Milestone, to: after.Milestone})
	}
	if !slices.Equal(before.BlockedBy, after.BlockedBy) {
		out = append(out, linkRebind{
			nibID: after.ID, field: "blocked_by",
			from: strings.Join(before.BlockedBy, ", "), to: strings.Join(after.BlockedBy, ", "),
		})
	}
	if !slices.Equal(before.Blocking, after.Blocking) {
		out = append(out, linkRebind{
			nibID: after.ID, field: "blocking",
			from: strings.Join(before.Blocking, ", "), to: strings.Join(after.Blocking, ", "),
		})
	}
	return out
}

// canonicalizeStoreLocked sweeps the whole store, re-resolving every nib's link
// ids copy-on-write, and returns what it re-pointed. It serves the mutators that
// change the key set outside a watcher batch: they publish no event, so the
// returned rebinds are their only way to announce a third nib's link moving.
//
// The loop reassigns existing keys only, never adding or removing one, so the
// key set the in-loop normalizeIDInMap lookups read is stable. O(N) over the
// store — callers gate it on a condition that can actually re-point a link. Must
// be called with c.mu held.
func (c *Core) canonicalizeStoreLocked() []linkRebind {
	configPrefix := c.configPrefix()
	if configPrefix == "" {
		return nil
	}
	var rebinds []linkRebind
	for id := range c.nibs {
		before := c.nibs[id]
		updated := c.canonicalizeOneLocked(id, configPrefix)
		if updated == nil {
			continue
		}
		rebinds = append(rebinds, describeRebinds(before, updated)...)
	}
	return rebinds
}

// removalCanRebindLinksLocked reports whether dropping removedID from the store
// can change what an ALREADY-RESOLVED link id points at, and so whether a
// canonicalization sweep must follow the removal.
//
// Only one removal shape can: normalizeIDInMap tries an exact map key before the
// prefix-prepended form, so a bare token that named the nib just removed now
// falls through to its prefixed twin. Every other removal leaves resolution
// alone — a link naming a gone id stops resolving, and an unresolvable id is
// left verbatim.
//
// A spurious true costs one sweep and rewrites nothing: the sweep re-resolves
// against the final map. Must be called with c.mu held.
func (c *Core) removalCanRebindLinksLocked(removedID string) bool {
	configPrefix := c.configPrefix()
	if configPrefix == "" || strings.HasPrefix(removedID, configPrefix) {
		return false
	}
	_, twinExists := c.nibs[configPrefix+removedID]
	return twinExists
}

// canonicalizeLinksAfterBatchLocked is the watcher's counterpart, run after a
// debounce batch has been applied to the store. It returns the events to
// publish: the ones it was given with a canonicalized payload swapped in, plus
// an EventUpdated for any OTHER nib the batch re-pointed.
//
// touched maps a nib id changed by this batch to the indices of the events
// carrying its payload, so a nib canonicalized as part of its own arrival keeps
// its created/updated event instead of collecting a second, contradictory one.
//
// scanAll widens the pass from the batch's own nibs to the whole store. The
// caller sets it when the batch CHANGED THE KEY SET in a way that can re-point a
// link on a nib the batch never touched — an id arriving that was not in the
// store before, or a bare-token id leaving while its prefixed twin remains (see
// removalCanRebindLinksLocked).
//
// Rewrites are copy-on-write — see canonicalizeOneLocked, which this delegates
// to, and which the scanAll branch ranges c.nibs across safely for the reason
// canonicalizeStoreLocked states. Must be called with c.mu held.
func (c *Core) canonicalizeLinksAfterBatchLocked(events []NibEvent, touched map[string][]int, scanAll bool) []NibEvent {
	configPrefix := c.configPrefix()
	if configPrefix == "" {
		return events
	}

	apply := func(id string) {
		updated := c.canonicalizeOneLocked(id, configPrefix)
		if updated == nil {
			return
		}

		if idx := touched[id]; len(idx) > 0 {
			for _, i := range idx {
				events[i].Nib = updated
			}
			return
		}
		events = append(events, NibEvent{
			Type:  EventUpdated,
			Nib:   updated,
			NibID: id,
		})
	}

	if scanAll {
		for id := range c.nibs {
			apply(id)
		}
	} else {
		for id := range touched {
			apply(id)
		}
	}

	return events
}
