package nibcore

import (
	"slices"
	"strings"

	"github.com/alphaleonis/nibs/internal/nib"
)

// Link-id canonicalization: every id stored in c.nibs is a FULL id.
//
// A nib file may name its parent or a blocker by SHORT id (`parent: par`
// rather than `parent: nibs-par`) — hand-editing a file is the only way that
// spelling enters the store, since every write path resolves through
// NormalizeID first. The forward resolvers normalize such an id when they
// follow it, but the reverse traversals (findIncomingLinksInMap,
// isBlockingInMap) and the cycle passes (FindCyclesInMap,
// findPathToTargetInMap) walk exact map keys, so a short-form link used to
// resolve from the nib holding it and be invisible from the other end — and a
// short-form parent cycle went undetected while the forward resolver looped
// through it.
//
// Rather than teach each of those traversals to normalize (a lookup per edge on
// paths that run per nib in list projections, and a discipline every future
// traversal has to remember), resolution happens ONCE at the disk-read
// boundary. Downstream every id is full, so an exact map lookup is correct
// everywhere.
//
// Two consequences are deliberate, not incidental:
//
//   - In-memory only. Nothing here rewrites a file; canonicalization never
//     touches bytes the user did not edit and never fights the file watcher.
//     But the store now holds the full form, so the NEXT unrelated write to
//     that nib (a set/body/close) persists it — a hand-written short id
//     silently becomes canonical on its next save. computeStoredETag
//     canonicalizes the same way, so the divergence between the canonicalized
//     nib and its short-form file does not false-conflict an if-match Update in
//     the meantime.
//   - An UNRESOLVABLE id stays verbatim. `parent: e001` naming no nib cannot be
//     canonicalized, so it is left exactly as written and `nibs check` still
//     reports it broken against the spelling in the file.
//
// What a stored id resolves to is a property of the KEY SET, not of the id
// alone, so canonicalization is not a one-shot load-time step: every change to
// the set of stored ids can re-point a link that was already resolved. Both
// directions matter. An id ARRIVING can resolve a link that was unresolvable
// before it. An id LEAVING can re-point one that resolved exactly — a store
// holding both a bare token `e1` and its prefixed twin `nibs-e1` keeps a raw
// `parent: e1` verbatim (it resolves exactly), and removing `e1` makes that same
// spelling fall through to `nibs-e1`. Whoever changes the key set therefore
// re-runs the sweep — Core.Delete gated on removalCanRebindLinksLocked,
// Core.Create unconditionally, the watcher via canonicalizeLinksAfterBatchLocked's
// scanAll; skipping it leaves the stored spelling naming one nib while Get
// answers with another, invisibly.
//
// Because the sweep re-points a link that was already resolved, it must resolve
// from the FILE's spelling and not from the value the store now holds. The
// stored value is the previous sweep's output, so feeding it back in makes the
// pass one-way and the two directions asymmetric: `parent: e1` re-pointed to
// `nibs-e1` by a delete resolves to itself forever, and restoring `e1.md` — a
// `git checkout` in the separately-versioned .nibs repo, or a re-create — leaves
// the live store answering `nibs-e1` while the untouched file, and therefore
// every fresh load, says `e1`. Resolving from the file spelling instead
// (nib.RawLinks, mirrored on every read AND every write) makes each pass a pure
// function of the file: idempotent, reversible, and identical in both
// directions without the sweep having to know which one occurred.

// canonicalLinks holds the resolved link fields for one nib. changed reports
// whether any of them differs from what the nib currently holds, so callers can
// skip the write (and, on published pointers, the clone) entirely.
//
// changed is deliberately measured against the nib's CURRENT values, not against
// the file spelling resolution reads from. Those two differ permanently on every
// hand-edited short-form nib — the file says `par`, the store says `nibs-par` —
// so comparing against the file would report a change on every sweep forever,
// installing a fresh clone each time and turning each into a spurious
// EventUpdated on the watcher path.
//
// A list field is nil when it did not change, which is distinguishable from a
// changed value because resolution never empties a non-empty list: it rewrites
// spellings and can collapse duplicates, so at least one entry always survives.
// That keeps applyTo from overwriting loadNib's empty-slice defaults (which
// GraphQL's non-null list fields rely on) with a nil.
type canonicalLinks struct {
	parent    string
	blockedBy []string
	blocking  []string
	changed   bool
}

// applyTo writes the resolved fields onto b. Only meaningful when changed is
// true; applying an unchanged set is a no-op.
func (s canonicalLinks) applyTo(b *nib.Nib) {
	b.Parent = s.parent
	if s.blockedBy != nil {
		b.BlockedBy = s.blockedBy
	}
	if s.blocking != nil {
		b.Blocking = s.blocking
	}
}

// canonicalizeLinksInMap resolves b's Parent, BlockedBy and legacy Blocking ids
// to their full form against nibs, using the same exact-match-then-prefix-
// prepended rule as Core.Get (normalizeIDInMap). Targets that resolve to no nib
// are carried through unchanged.
//
// Resolution reads b's FILE spelling (nib.RawLinks), never the values b
// currently holds — those are the previous resolution's own output, and feeding
// them back in makes the pass one-way: `nibs-par` resolves to itself, so a link
// re-pointed while its target was missing can never follow the file back when
// the target returns. See RawLinks for the full argument.
//
// A file spelling that is EMPTY where b holds a link is left alone rather than
// cleared: this pass rewrites how a link is spelled and never invents or erases
// one, so a nib whose in-memory links have run ahead of its file keeps them.
//
// It does NOT mutate b — the result is returned so the caller can decide where
// it lands. That matters for already-published nibs, which must be updated
// copy-on-write (see NibReader.GetSnapshot in internal/graph/interfaces.go).
//
// Pure function operating on the given map without locking. Callers passing a
// Core.nibs map must hold Core.mu for the duration of the call.
//
// With no configured prefix, resolution degrades to an exact map lookup, so
// nothing can resolve to a DIFFERENT spelling and the whole pass is a no-op —
// taken as an early return so the common prefix-less project pays nothing.
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

	set := canonicalLinks{parent: b.Parent}
	if raw.Parent != "" {
		if resolved := resolve(raw.Parent); resolved != b.Parent {
			set.parent = resolved
			set.changed = true
		}
	}

	// Resolving can collapse two spellings of one target onto the same id
	// (`blocked_by: [blk, nibs-blk]`). Drop the later duplicate: keeping it would
	// render a duplicated entry on the next save and double-count the edge in
	// every reverse traversal. Returns nil when the resolved list matches what the
	// nib already holds.
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
// only resolves once every file has been read — a per-file step during the walk
// would leave a link written before its target was visited unresolved.
//
// UNPUBLISHED is the precondition, spelled in the name because otherwise this
// reads like a fungible alternative to canonicalizeStoreLocked (same call shape,
// no arguments): this mutates the stored nibs
// IN PLACE, which is safe only on the bulk load path, where loadFromDisk builds a
// fresh c.nibs of pointers no off-lock reader can hold. Anything working on
// published pointers must go through canonicalizeStoreLocked or
// canonicalizeLinksAfterBatchLocked, which rewrite copy-on-write. Must be called
// with c.mu held.
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
// fresh pointer. Returns nil when the nib is absent or nothing changed, so a
// caller can treat "rewritten" and "left alone" as one branch.
//
// Every rewrite is copy-on-write: Parent (a torn string) and BlockedBy (a
// memory-unsafe torn slice header) are non-Path fields, so they must land on a
// FRESH pointer rather than on the published one — see the canonical live-pointer
// invariant at NibReader.GetSnapshot (internal/graph/interfaces.go).
//
// This is the runtime counterpart used by the mutator and watcher sweeps, which
// re-point links on nibs the user is already looking at; the load path rewrites
// in place instead (see canonicalizeAllLinksUnpublishedLocked). Must be called
// with c.mu held.
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
// its re-resolved replacement. A list is reported whole rather than per entry
// because resolution can collapse duplicates, so entries do not line up one to
// one with the originals.
func describeRebinds(before, after *nib.Nib) []linkRebind {
	var out []linkRebind
	if before.Parent != after.Parent {
		out = append(out, linkRebind{nibID: after.ID, field: "parent", from: before.Parent, to: after.Parent})
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
// ids copy-on-write, and returns what it re-pointed. It is the event-free
// counterpart to canonicalizeLinksAfterBatchLocked, for the mutators that change
// the key set outside a watcher batch and publish nothing — so the return value
// is the only way their callers can announce a third nib's link moving.
//
// Reassigning an existing key's value while ranging over the map is safe in Go,
// and no key is added or removed here, so the in-loop normalizeIDInMap lookups
// see a stable key set. (c.mu is held exclusively, so there are no off-lock
// readers inside that function to reason about.) O(N) over the store — callers
// gate it on a condition that can actually re-point a link. Must be called with
// c.mu held.
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
// can change what an ALREADY-RESOLVED link id points at, and therefore whether a
// canonicalization sweep has to follow the removal.
//
// Only one removal shape can: normalizeIDInMap tries an exact map key before the
// prefix-prepended form, so a bare token that named the nib just removed now
// falls through to its prefixed twin. Every other removal leaves resolution
// alone — a link naming a gone id simply stops resolving, and an unresolvable id
// is left verbatim by design.
//
// Note the gate reads the store, so a caller that removes several ids in one pass
// may see it fire on an intermediate state. That is safe in the direction that
// matters: the sweep re-resolves against the final map, so a spurious true costs
// one pass and rewrites nothing. Must be called with c.mu held.
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
// publish, which may be the ones it was given with a canonicalized payload
// swapped in, plus an EventUpdated for any OTHER nib the batch re-pointed.
//
// touched maps a nib id changed by this batch to the indices of the events
// carrying its payload, so a nib canonicalized as part of its own arrival keeps
// its created/updated event (with the canonicalized payload swapped in) instead
// of collecting a second, contradictory one.
//
// scanAll widens the pass from the batch's own nibs to the whole store. The
// caller sets it when the batch CHANGED THE KEY SET in a way that can re-point a
// link on a nib the batch never touched — an id arriving that was not in the
// store before, or a bare-token id leaving while its prefixed twin remains (see
// removalCanRebindLinksLocked). Without that sweep the forward resolver would
// answer for such a link while the stored spelling and every reverse traversal
// disagreed until the next full reload. A batch that only edits existing nibs
// cannot re-point anything, so it pays the cheap touched-only pass.
//
// Rewrites are copy-on-write — see canonicalizeOneLocked, which this delegates
// to. The scanAll branch ranges c.nibs while that delegate reassigns its values,
// which is safe for the same reason canonicalizeStoreLocked states: only existing
// keys are reassigned, none is added or removed, so the key set the in-loop
// lookups read is stable. Must be called with c.mu held.
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
