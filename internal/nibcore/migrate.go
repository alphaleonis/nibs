package nibcore

// migrateV0ToV1 migrates all v0 nibs to v1 format.
// v0→v1: converts dual-side blocking to single-side (blockedBy only).
// For each v0 nib with blocking entries, adds this nib's ID to each target's blockedBy.
// Then clears the blocking field, bumps version to 1, and saves to disk.
// Must be called with c.mu held and after all nibs are loaded.
func (c *Core) migrateV0ToV1() error {
	// Track which nibs need saving (either migrated or had blockedBy modified)
	dirty := make(map[string]bool)
	var migratedCount int

	// First pass: convert blocking entries to blockedBy on targets
	for _, b := range c.nibs {
		if b.Version > 0 {
			continue // already migrated
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

	// Save all dirty nibs
	for id := range dirty {
		if b, ok := c.nibs[id]; ok {
			if err := c.saveToDisk(b); err != nil {
				return err
			}
		}
	}

	if migratedCount > 0 {
		c.logWarn("migrated %d nib(s) from v0 to v1", migratedCount)
	}

	return nil
}
