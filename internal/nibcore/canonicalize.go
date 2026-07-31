package nibcore

import "github.com/alphaleonis/nibs/internal/nib"

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

// canonicalLinks holds the resolved link fields for one nib. changed reports
// whether any of them differs from what the nib currently holds, so callers can
// skip the write (and, on published pointers, the clone) entirely.
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

	set := canonicalLinks{parent: b.Parent}
	if b.Parent != "" {
		if resolved := resolve(b.Parent); resolved != b.Parent {
			set.parent = resolved
			set.changed = true
		}
	}

	// Resolving can collapse two spellings of one target onto the same id
	// (`blocked_by: [blk, nibs-blk]`). Drop the later duplicate: keeping it would
	// render a duplicated entry on the next save and double-count the edge in
	// every reverse traversal. Returns nil when nothing changed.
	canonicalList := func(ids []string) []string {
		out := make([]string, 0, len(ids))
		seen := make(map[string]bool, len(ids))
		changed := false
		for _, id := range ids {
			resolved := resolve(id)
			if seen[resolved] {
				changed = true
				continue
			}
			seen[resolved] = true
			if resolved != id {
				changed = true
			}
			out = append(out, resolved)
		}
		if !changed {
			return nil
		}
		return out
	}

	if set.blockedBy = canonicalList(b.BlockedBy); set.blockedBy != nil {
		set.changed = true
	}
	if set.blocking = canonicalList(b.Blocking); set.blocking != nil {
		set.changed = true
	}

	return set
}

// canonicalizeAllLinksLocked resolves every loaded nib's link ids to their full
// form. It runs as a second pass over the whole map because a target only
// resolves once every file has been read — a per-file step during the walk
// would leave a link written before its target was visited unresolved.
//
// Mutates the stored nibs in place, which is safe ONLY on the bulk load path:
// loadFromDisk builds a fresh c.nibs of pointers that have never been
// published, so no off-lock reader can hold one. The watcher's incremental path
// works on published pointers and must go through
// canonicalizeLinksAfterBatchLocked instead. Must be called with c.mu held.
func (c *Core) canonicalizeAllLinksLocked() {
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

// canonicalizeLinksAfterBatchLocked is the watcher's counterpart, run after a
// debounce batch has been applied to the store. It returns the events to
// publish, which may be the ones it was given with a canonicalized payload
// swapped in, plus an EventUpdated for any OTHER nib the batch made resolvable.
//
// touched maps a nib id changed by this batch to the indices of the events
// carrying its payload, so a nib canonicalized as part of its own arrival keeps
// its created/updated event (with the canonicalized payload swapped in) instead
// of collecting a second, contradictory one.
//
// scanAll widens the pass from the batch's own nibs to the whole store. The
// caller sets it when the batch introduced an id that was not in the store
// before: a link written BEFORE the nib it names exists is unresolvable at load
// and correctly left verbatim, and only the target's arrival can resolve it.
// Without that sweep the forward resolver would answer for such a link while
// every reverse traversal stayed blind until the next full reload. A batch that
// only edits or removes existing nibs cannot make anything newly resolvable, so
// it pays the cheap touched-only pass.
//
// Every rewrite is copy-on-write: Parent (a torn string) and BlockedBy (a
// memory-unsafe torn slice header) are non-Path fields, so they must land on a
// FRESH pointer rather than on the published one — see the canonical
// live-pointer invariant at NibReader.GetSnapshot (internal/graph/interfaces.go).
// Reassigning an existing key's value while ranging over the map is safe in Go,
// and no key is added or removed here, so the concurrent normalizeIDInMap
// lookups see a stable key set. Must be called with c.mu held.
func (c *Core) canonicalizeLinksAfterBatchLocked(events []NibEvent, touched map[string][]int, scanAll bool) []NibEvent {
	configPrefix := c.configPrefix()
	if configPrefix == "" {
		return events
	}

	apply := func(id string) {
		b, ok := c.nibs[id]
		if !ok {
			return
		}
		set := canonicalizeLinksInMap(c.nibs, b, configPrefix)
		if !set.changed {
			return
		}
		updated := b.Clone()
		set.applyTo(updated)
		c.nibs[id] = updated

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
