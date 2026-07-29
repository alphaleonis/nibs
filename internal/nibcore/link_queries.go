package nibcore

import (
	"github.com/alphaleonis/nibs/internal/nib"
)

// DetectCycleInMap checks if adding a link from fromID to toID would create a cycle.
// Checks for blocked_by and parent link types.
// Returns the cycle path if a cycle would be created, nil otherwise.
// This is a pure function that operates on a map of nibs without locking.
func DetectCycleInMap(nibs map[string]*nib.Nib, fromID, linkType, toID string) []string {
	// Only check hierarchical link types
	if linkType != "blocked_by" && linkType != "parent" {
		return nil
	}

	// Adding edge: fromID -> toID
	// Check if there's already a path from toID back to fromID
	visited := make(map[string]bool)
	path := []string{fromID, toID}

	return findPathToTargetInMap(nibs, toID, fromID, linkType, visited, path)
}

// DetectCycle checks if adding a link from fromID to toID would create a cycle.
// Thread-safe wrapper around DetectCycleInMap.
func (c *Core) DetectCycle(fromID, linkType, toID string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return DetectCycleInMap(c.nibs, fromID, linkType, toID)
}

// findPathToTargetInMap uses DFS to find if there's a path from current to target.
// Returns the path if found, nil otherwise.
func findPathToTargetInMap(nibs map[string]*nib.Nib, current, target, linkType string, visited map[string]bool, path []string) []string {
	if current == target {
		return path
	}

	if visited[current] {
		return nil
	}
	visited[current] = true

	b, ok := nibs[current]
	if !ok {
		return nil
	}

	// Get targets based on link type
	var targets []string
	switch linkType {
	case "parent":
		if b.Parent != "" {
			targets = []string{b.Parent}
		}
	case "blocked_by":
		targets = b.BlockedBy
	}

	for _, t := range targets {
		newPath := append(path, t)
		if result := findPathToTargetInMap(nibs, t, target, linkType, visited, newPath); result != nil {
			return result
		}
	}

	return nil
}

// findIncomingLinksInMap returns all nibs that link TO the given nib ID.
// Single-side storage: blocking relationships are only stored as blockedBy on the blocked nib.
// This is a pure function that operates on a map of nibs without locking.
func findIncomingLinksInMap(nibs map[string]*nib.Nib, targetID string) []nib.IncomingLink {
	var result []nib.IncomingLink
	for _, b := range nibs {
		// Check parent link
		if b.Parent == targetID {
			result = append(result, nib.IncomingLink{
				FromNib:  b,
				LinkType: "parent",
			})
		}
		// Check blocked_by links (if A has blocked_by B, then B links to A)
		for _, blocker := range b.BlockedBy {
			if blocker == targetID {
				result = append(result, nib.IncomingLink{
					FromNib:  b,
					LinkType: "blocked_by",
				})
			}
		}
	}
	return result
}

// FindIncomingLinks returns all nibs that link TO the given nib ID.
// Thread-safe wrapper around findIncomingLinksInMap.
func (c *Core) FindIncomingLinks(targetID string) []nib.IncomingLink {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return findIncomingLinksInMap(c.nibs, targetID)
}

// releasesDependentsPredicate returns the "does this blocker still count" test
// the pure map functions need. Deliberately not the closed-status test: a
// deferred nib is closed but still blocks, because the set-aside work is coming
// back and its dependency is unsatisfied. The definition lives in config
// (Config.StatusReleasesDependents) and is threaded in rather than duplicated
// here, so nibcore has no status list of its own.
//
// c.config may be nil — cmd/init.go builds such a Core, as do several tests in
// this package. Binding and calling StatusReleasesDependents on a nil *Config
// is safe because it answers from the package-level config.DefaultStatuses and
// never dereferences its receiver; TestCoreReleasesDependentsPredicate/
// config-less pins that. If status data ever moves onto the Config value, this
// becomes a nil dereference and New should normalize a nil config instead.
func (c *Core) releasesDependentsPredicate() func(string) bool {
	return c.config.StatusReleasesDependents
}

// isBlockedInMap returns true if the nib with the given ID is blocked by any
// nib whose status has not released its dependents. releasesDependents is that
// predicate, and configPrefix the id-resolution prefix, both supplied by the
// caller because this is a pure function that operates on a map of nibs without
// locking and so cannot reach the project config itself.
func isBlockedInMap(nibs map[string]*nib.Nib, nibID, configPrefix string, releasesDependents func(string) bool) bool {
	return len(findActiveBlockersInMap(nibs, nibID, configPrefix, releasesDependents)) > 0
}

// IsBlocked returns true if the nib with the given ID has any active blockers —
// blockers whose status has not released them.
// Thread-safe wrapper around isBlockedInMap.
func (c *Core) IsBlocked(nibID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return isBlockedInMap(c.nibs, nibID, c.configPrefix(), c.releasesDependentsPredicate())
}

// isBlockingInMap returns true if the nib with the given ID is actively blocking
// any nib. The nib itself must not have released its dependents to be
// considered actively blocking, and a dependent that has been released no
// longer counts as blocked — so both sides use the same predicate and the two
// directions of an edge always agree. Computed from other nibs' blockedBy
// fields. releasesDependents is supplied by the caller because this is a pure
// function that operates on a map of nibs without locking and so cannot reach
// the project config itself.
func isBlockingInMap(nibs map[string]*nib.Nib, nibID string, releasesDependents func(string) bool) bool {
	b, ok := nibs[nibID]
	if !ok || releasesDependents(b.Status) {
		return false
	}

	// Check other nibs that list this nib in their blocked_by
	for _, other := range nibs {
		if releasesDependents(other.Status) {
			continue
		}
		for _, blockerID := range other.BlockedBy {
			if blockerID == nibID {
				return true
			}
		}
	}

	return false
}

// IsBlocking returns true if the nib with the given ID is actively blocking
// any other nib.
// Thread-safe wrapper around isBlockingInMap.
func (c *Core) IsBlocking(nibID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return isBlockingInMap(c.nibs, nibID, c.releasesDependentsPredicate())
}

// findActiveBlockersInMap returns all nibs that are actively blocking the given
// nib. A blocker is "active" unless its status released its dependents, per the
// caller-supplied predicate — this is a pure function that operates on a map of
// nibs without locking and so cannot reach the project config itself. Note this
// is narrower than "not closed": a deferred blocker is closed and still active.
// Single-side storage: only reads from the nib's blockedBy field.
//
// Each blockedBy entry is resolved through normalizeIDInMap, so a hand-edited
// nib naming its blocker by short id finds it — the same exact-then-prefixed
// rule Core.Get applies, which is how the projected `ready` field reaches its
// blockers. configPrefix is threaded in for the same reason releasesDependents
// is: the function cannot reach the config. Resolution stays two map lookups at
// worst plus the one that fetches the blocker, and never scans.
//
// nibID itself is looked up exactly, not normalized: it names the subject, and
// every production call arrives through Core.IsBlocked carrying an id read off
// a stored nib (the graph blocked-filter predicate and the TUI row builders).
// The exported Core.FindActiveBlockers reaches here too and normalizes no more
// than IsBlocked does; it simply has no production caller today. Either entry
// point answers "not blocked" for a short subject id, so a caller holding one
// should resolve it with Core.NormalizeID first.
func findActiveBlockersInMap(nibs map[string]*nib.Nib, nibID, configPrefix string, releasesDependents func(string) bool) []*nib.Nib {
	b, ok := nibs[nibID]
	if !ok {
		return nil
	}

	var blockers []*nib.Nib
	for _, blockerID := range b.BlockedBy {
		fullID, ok := normalizeIDInMap(nibs, blockerID, configPrefix)
		if !ok {
			continue
		}
		if blocker := nibs[fullID]; !releasesDependents(blocker.Status) {
			blockers = append(blockers, blocker)
		}
	}

	return blockers
}

// FindActiveBlockers returns all nibs that are actively blocking the given nib.
// Thread-safe wrapper around findActiveBlockersInMap.
func (c *Core) FindActiveBlockers(nibID string) []*nib.Nib {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return findActiveBlockersInMap(c.nibs, nibID, c.configPrefix(), c.releasesDependentsPredicate())
}
