package nibcore

import (
	"fmt"
	"sort"

	"github.com/alphaleonis/nibs/internal/fsutil"
	"github.com/alphaleonis/nibs/internal/nib"
)

// MigrateV0ToV1 converts every v0 nib in the store to v1: each v0 nib's legacy
// dual-side `blocking:` edges are transferred onto the targets' blocked_by,
// its own Blocking field is cleared, and its Version is stamped 1 — then every
// changed nib is persisted. Returns the number of v0 nibs converted.
//
// This is the explicit-migration relocation of what the load path used to do
// silently: it runs only under `nibs migrate`, on a store that already loaded
// cleanly (the command gates on LoadDiagnostics first), and it is FAIL-LOUD —
// the first persistence failure aborts with an error instead of the old
// best-effort log-and-continue.
//
// Persistence is TWO-PHASE, and the split is what makes a crashed run
// resumable. The version stamp is the step's per-file completion record (v0
// detection keys on it), while the transferred edge lives in a DIFFERENT file
// — so the invariant is: no source may stamp v1 before every edge it is
// transferring has been persisted on its target. Phase 1 writes the targets'
// additive blocked_by transfers (targets keep whatever version/blocking they
// had — a v0 target stays detectably v0); phase 2 rewrites the sources
// (blocking cleared, version stamped). A crash anywhere leaves every
// not-yet-stamped source still v0, and the re-run redoes its transfers
// (AddBlockedBy dedups) before stamping — including chains and cycles of
// v0→v0 edges, where a target is a source too. A single sorted-id pass had a
// real crash window here: a source sorting before its target persisted the
// stamp before the edge, and the re-run then reported the store fully
// migrated with the edge gone.
//
// Blocking targets are looked up by exact id: Load's canonicalization pass has
// already resolved short-form spellings, so a target that still resolves to
// nothing genuinely does not exist — the edge is dropped with a warning, as a
// data repair the migration cannot invent an answer for.
//
// Concurrency: lock is PROOF-OF-LOCK — the *StoreLock the caller received
// from AcquireStoreLock, held for the whole run. The method validates the
// proof (this store's lock, not yet released — see requireStoreLock) but
// consumes nothing else from it; it exists so this precondition lives in the
// signature instead of a comment, because the method cannot defend itself —
// the flock is per-descriptor, so re-acquiring it in-process deadlocks,
// which is also why this method must NOT take the per-operation write lock. Mutations are copy-on-write (clone, persist, reinstall)
// per the canonical live-pointer invariant (see NibReader.GetSnapshot in
// internal/graph/interfaces.go), even though a migration Core is a scoped
// throwaway that publishes no pointers.
// requireStoreLock validates a migration method's proof-of-lock token: it
// must be non-nil, not yet released, and acquired for THIS core's store. Each
// check makes the claim the token stands for true — nil never held the lock,
// a released token no longer holds it, and a token for another store holds
// the wrong one. Any of the three passing would run the method's whole-store
// read-modify-write with no cross-process exclusion, silently.
func (c *Core) requireStoreLock(method string, lock *StoreLock) error {
	if lock == nil {
		return fmt.Errorf("%s requires the store-wide lock: pass the *StoreLock from AcquireStoreLock", method)
	}
	if lock.released {
		return fmt.Errorf("%s was passed an already-released *StoreLock; re-acquire it via AcquireStoreLock", method)
	}
	if lock.lockPath != c.lockPath {
		return fmt.Errorf("%s was passed a *StoreLock for a different store; acquire it via AcquireStoreLock(%q)", method, c.root)
	}
	return nil
}

func (c *Core) MigrateV0ToV1(lock *StoreLock) (int, error) {
	if err := c.requireStoreLock("MigrateV0ToV1", lock); err != nil {
		return 0, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Deterministic id order so warnings and any failure point are stable from
	// run to run (map iteration order is not).
	v0IDs := make([]string, 0)
	for id, b := range c.nibs {
		if b.Version < 1 {
			v0IDs = append(v0IDs, id)
		}
	}
	sort.Strings(v0IDs)

	// Copy-on-write staging: every nib that changes is mutated on a clone; the
	// stored pointers are untouched until the clone has been persisted.
	cloneInto := func(dirty map[string]*nib.Nib, id string) *nib.Nib {
		if cl, ok := dirty[id]; ok {
			return cl
		}
		cl := c.nibs[id].Clone()
		dirty[id] = cl
		return cl
	}

	// Phase 1: transfer every edge onto its target and persist ONLY those
	// additive changes. Sources are read but not modified, so a crash in this
	// phase leaves every source detectably v0.
	targets := make(map[string]*nib.Nib)
	for _, id := range v0IDs {
		for _, targetID := range c.nibs[id].Blocking {
			if _, ok := c.nibs[targetID]; !ok {
				c.logWarn("migration: nib %s has blocking reference to nonexistent nib %s, dropping the edge", id, targetID)
				continue
			}
			cloneInto(targets, targetID).AddBlockedBy(id)
		}
	}
	if err := c.persistClonesLocked(targets, "v0→v1 edge transfer"); err != nil {
		return 0, err
	}

	// Phase 2: rewrite the sources — clear the transferred blocking and stamp
	// the version. Clones are taken AFTER phase 1's reinstall so a source that
	// also received edges keeps them.
	sources := make(map[string]*nib.Nib)
	for _, id := range v0IDs {
		cl := cloneInto(sources, id)
		cl.Blocking = nil
		cl.Version = 1 // the v0→v1 step's fixed output version (not CurrentVersion)
	}
	if err := c.persistClonesLocked(sources, "v0→v1"); err != nil {
		return 0, err
	}

	return len(v0IDs), nil
}

// NormalizeLegacyPriorities rewrites every nib carrying the legacy
// `priority: deferred` value to `low` on disk and returns the number of nibs
// rewritten. "deferred" was removed as a priority (it is now a status); it
// maps to "low" because it ranked BELOW "low" in the old enum, so "low"
// preserves its relative rank — do not "tidy" this to "normal", which would
// silently re-rank legacy nibs upward.
//
// It deliberately does NOT stamp version: 1 on a still-v0 file it touches.
// The version stamp is MigrateV0ToV1's completion record: stamping from here
// would mark a v0 file's `blocking:` edges migrated without transferring them,
// and — because v0 detection keys on the version — nothing would ever return
// to finish the job (the edge silently vanishes from every view). Leaving the
// file at v0 is safe: Render emits the version verbatim, so the rewritten file
// carries `version: 0` and stays detectably v0 (absent or 0 both mean legacy),
// letting the v0 step complete it on the same or any later run. See
// migrationSteps in cmd/migrate.go for the chain-wide statement of this
// invariant.
//
// This is the priority-deferred migration step's engine, sharing
// MigrateV0ToV1's contract: fail-loud persistence (first error aborts),
// idempotent per nib (a rewritten file no longer matches), copy-on-write
// staging, and NO per-operation write lock — lock is the caller's
// proof-of-lock token from AcquireStoreLock (see MigrateV0ToV1's concurrency
// note for why the precondition is a parameter).
func (c *Core) NormalizeLegacyPriorities(lock *StoreLock) (int, error) {
	if err := c.requireStoreLock("NormalizeLegacyPriorities", lock); err != nil {
		return 0, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	dirty := make(map[string]*nib.Nib)
	for id, b := range c.nibs {
		if b.Priority != "deferred" {
			continue
		}
		cl := b.Clone()
		cl.Priority = "low"
		dirty[id] = cl
	}

	if err := c.persistClonesLocked(dirty, "priority"); err != nil {
		return 0, err
	}

	return len(dirty), nil
}

// MigrateV1ToV2 converts every v1 nib in the store to v2: a nib whose resolved
// direct parent is milestone-typed moves onto the assignment axis — Milestone
// set to that parent's id, MilestoneOrder to the nib's Order (the milestone's
// child set WAS its queue, so the sibling position carries over) — with the
// milestone parent and its order cleared, and every converted nib's Version
// stamped 2. Returns the number of v1 nibs converted.
//
// A nib already carrying an assignment keeps it (and its MilestoneOrder): the
// hand-authored field is the newer axis speaking, and inventing a different
// answer would silently reschedule the nib. The milestone parent is still
// cleared — the parent axis is decomposition only from v2 on — and the
// collision is warned about, like the dropped-edge warning in MigrateV0ToV1.
//
// A milestone-typed nib under a milestone parent (an illegal nest) gets no
// assignment: v1 membership never enqueued the nest, so writing `milestone:`
// here would invent membership. The illegal parent stays as it is — this
// migration cannot repair it, and hierarchy is validated only on write
// paths today (a hierarchy scan in `nibs check` is tracked separately).
//
// Parents are resolved by exact id against the loaded store, exactly as the
// membership reads resolve them: Load's canonicalization has already resolved
// short-form spellings, so a parent that resolves to nothing is no parent and
// the nib stays on the parent axis as written.
//
// A still-v0 nib is deliberately left byte-identical: the version stamp is
// MigrateV0ToV1's completion record (see NormalizeLegacyPriorities for the
// chain-wide statement), and the chain runs the v0 step first, so by the time
// this step applies no v0 nib remains.
//
// This is the v2-axes migration step's engine, sharing MigrateV0ToV1's
// contract: fail-loud persistence (first error aborts), idempotent per nib (a
// v2 nib no longer matches), copy-on-write staging, and NO per-operation write
// lock — lock is the caller's proof-of-lock token from AcquireStoreLock (see
// MigrateV0ToV1's concurrency note for why the precondition is a parameter).
func (c *Core) MigrateV1ToV2(lock *StoreLock) (int, error) {
	if err := c.requireStoreLock("MigrateV1ToV2", lock); err != nil {
		return 0, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Deterministic id order so warnings and any failure point are stable from
	// run to run (map iteration order is not).
	v1IDs := make([]string, 0)
	for id, b := range c.nibs {
		if b.Version == 1 {
			v1IDs = append(v1IDs, id)
		}
	}
	sort.Strings(v1IDs)

	dirty := make(map[string]*nib.Nib, len(v1IDs))
	for _, id := range v1IDs {
		b := c.nibs[id]
		cl := b.Clone()
		if p, ok := c.nibs[b.Parent]; ok && b.Parent != "" &&
			p.EffectiveType() == "milestone" && b.EffectiveType() != "milestone" {
			if cl.Milestone != "" {
				c.logWarn("migration: nib %s already carries milestone %s; keeping it over milestone parent %s", id, cl.Milestone, p.ID)
			} else {
				cl.Milestone = p.ID
				cl.MilestoneOrder = b.Order
			}
			// Order positioned the nib among the cleared parent's children — a
			// group it just left — so it is cleared with the parent rather than
			// left to place the nib among the roots at a meaningless position.
			cl.Parent = ""
			cl.Order = ""
		}
		cl.Version = 2 // the v1→v2 step's fixed output version (not CurrentVersion)
		dirty[id] = cl
	}
	if err := c.persistClonesLocked(dirty, "v1→v2"); err != nil {
		return 0, err
	}

	return len(dirty), nil
}

// persistClonesLocked writes each staged clone to disk in deterministic id
// order and reinstalls it under c.nibs — the copy-on-write commit shared by
// the migration methods. Fail-loud: the first write error aborts, naming the
// nib; already-persisted clones stay installed (correct: memory matches what
// disk now holds), and a re-run resumes from the files still unconverted.
// Must be called with c.mu held.
func (c *Core) persistClonesLocked(dirty map[string]*nib.Nib, what string) error {
	ids := make([]string, 0, len(dirty))
	for id := range dirty {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	// One directory fsync per directory the batch touched, not one per nib.
	// Deferred so an aborted batch still flushes what it did write: the first
	// error returns with the earlier files already renamed into place.
	var pending fsutil.DirSyncBatch
	defer pending.Flush()

	for _, id := range ids {
		cl := dirty[id]
		dir, err := c.saveToDiskDeferDirSync(cl)
		pending.Add(dir)
		if err != nil {
			return fmt.Errorf("persisting %s migration for %s: %w", what, id, err)
		}
		c.nibs[id] = cl
	}
	return nil
}
