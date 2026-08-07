package nibcore

import "github.com/alphaleonis/nibs/internal/nib"

// migrateV0ToV1 migrates all v0 nibs to v1 format.
// v0→v1: converts dual-side blocking to single-side (blockedBy only).
// For each v0 nib with blocking entries, adds this nib's ID to each target's blockedBy.
// Then clears the blocking field, bumps version to 1, and best-effort saves to
// disk (a persistence failure is logged and skipped, not returned — see the save
// loop). Must be called with c.mu held and after all nibs are loaded.
//
// skipped holds the IDs of files present on disk but skipped this load
// (unparseable/unreadable — see loadFromDisk). A v0 nib whose `blocking:` target
// was skipped has its OWN migration DEFERRED: its own iteration does not clear
// Blocking, does not bump Version, and does not persist it. Performing its own
// migration would erase the edge (`omitempty` drops the `blocking:` line) while
// the transfer to the skipped target never happened — irrecoverable data loss.
// Leaving it unmigrated lets a later clean Load (target repaired) complete the
// migration without dropping the edge.
//
// A deferred nib's file is NOT guaranteed untouched, however. In a chain
// A --blocking--> B --blocking--> C where only C's file is skipped, B is deferred
// for its own edge (B→C), yet A's migration still runs (B loaded fine, so A is
// not deferred) and transfers A→B onto B via AddBlockedBy, marking B dirty and
// re-persisting it — as version:0 with `blocking:[C]` intact PLUS a new
// `blocked_by:[A]`. This is intentional and lossless: B's own Blocking/Version
// are only ever mutated inside B's own (deferred) iteration, AddBlockedBy is
// purely additive, and B's own `blocking:[C]` edge converges on a later clean
// Load. Excluding a deferred target from the dirty set would instead DROP the
// A→B edge (A goes v1 with its `blocking:` cleared while B never receives
// `blocked_by:[A]` on disk) — real data loss — so this extra persist is exactly
// what preserves the edge. See migrate_test.go's 3-node-chain regression test.
func (c *Core) migrateV0ToV1(skipped map[string]bool) error {
	// Track which nibs need saving (either migrated or had blockedBy modified)
	dirty := make(map[string]bool)
	var migratedCount int

	// First pass: convert blocking entries to blockedBy on targets
	for _, b := range c.nibs {
		if b.Version > 0 {
			continue // already migrated
		}

		// Defer this nib's OWN migration if any of its blocking targets had its
		// file skipped this load: migrating now would drop the A→target edge (see
		// the method doc). Leave A v0 with Blocking intact so a later clean Load
		// migrates it atomically. (A deferred nib may still be re-persisted this
		// load if another nib's migration additively transfers a blocked_by onto
		// it — lossless; see the method doc.)
		if blockingTargetSkipped(b, skipped) {
			c.logWarn("migration: deferring v0→v1 for nib %s: a blocking target's file was skipped this load (unparseable/unreadable); leaving it unmigrated to preserve the edge", b.ID)
			continue
		}

		migratedCount++
		for _, targetID := range b.Blocking {
			target, ok := c.nibs[targetID]
			if !ok {
				c.logWarn("migration: nib %s has blocking reference to nonexistent nib %s, skipping", b.ID, targetID)
				continue
			}
			target.AddBlockedBy(b.ID)
			dirty[targetID] = true
		}

		b.Blocking = nil
		b.Version = 1
		dirty[b.ID] = true
	}

	// Save all dirty nibs (best-effort). A load-time persistence failure
	// (read-only mount, disk full, restricted permissions) must NOT abort Load:
	// the migration is already applied in memory, so nibs are correct for reads
	// and mutations, and on-disk convergence waits for the next successful write.
	// This mirrors loadNibReconciledLocked's "don't fail load" posture for the
	// priority migration. (Update/Delete still surface write errors to callers.)
	for id := range dirty {
		if b, ok := c.nibs[id]; ok {
			if err := c.saveToDisk(b); err != nil {
				c.logWarn("could not persist v0→v1 migration for %s: %v", id, err)
				// saveToDisk refreshes the raw-link mirror only on a SUCCESSFUL
				// write, so that a failed write leaves the mirror describing the
				// bytes still on disk. That is right everywhere except here: this
				// is the one path that deliberately keeps an in-memory link change
				// after a failed write (see the posture above). Leaving the mirror
				// on the pre-migration spelling means the next canonicalization
				// sweep — Core.Create fires one on every create — resolves from it
				// and RESTORES the legacy `blocking:` list onto a version-1 nib,
				// which the next successful Update then persists. Capture here so
				// the mirror describes what memory is authoritative for.
				b.CaptureRawLinks()
			}
		}
	}

	if migratedCount > 0 {
		c.logWarn("migrated %d nib(s) from v0 to v1", migratedCount)
	}

	return nil
}

// blockingTargetSkipped reports whether any of b's v0 blocking targets had its
// file skipped this load. Blocking references share the id space of c.nibs (the
// same keys migrateV0ToV1 looks up), and skipped ids are the filename-derived ids
// of unparseable/unreadable files, so the two are directly comparable.
func blockingTargetSkipped(b *nib.Nib, skipped map[string]bool) bool {
	if len(skipped) == 0 {
		return false
	}
	for _, targetID := range b.Blocking {
		if skipped[targetID] {
			return true
		}
	}
	return false
}
