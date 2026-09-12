package nibcore

import (
	"github.com/alphaleonis/nibs/internal/nib"
)

// DetectCycleInMap checks if adding a link from fromID to toID would create a cycle.
// Returns the cycle path if a cycle would be created, nil otherwise.
// This is a pure function that operates on a map of nibs without locking.
func DetectCycleInMap(nibs map[string]*nib.Nib, fromID, linkType, toID string) []string {
	if linkType != "blocked_by" && linkType != "parent" {
		return nil
	}

	// Adding fromID -> toID closes a cycle if a path from toID back to fromID
	// already exists.
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
		if b.Parent == targetID {
			result = append(result, nib.IncomingLink{
				FromNib:  b,
				LinkType: "parent",
			})
		}
		// A has blocked_by B, so B links to A.
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
// the pure map functions need. Not the closed-status test: a deferred nib is
// closed and still blocks.
//
// c.config may be nil — cmd/init.go builds such a Core, as do several tests in
// this package. Binding and calling StatusReleasesDependents on a nil *Config
// is safe because it answers from the package-level config.DefaultStatuses and
// never dereferences its receiver. If status data moves onto the Config value,
// this becomes a nil dereference and New must normalize a nil config instead.
func (c *Core) releasesDependentsPredicate() func(string) bool {
	return c.config.StatusReleasesDependents
}

// closedStatusPredicate returns the "is this nib finished" test. A nil c.config
// is safe for the reason releasesDependentsPredicate gives.
//
// The two predicates are not interchangeable — deferred is closed and still
// blocks — so take both when both are needed.
func (c *Core) closedStatusPredicate() func(string) bool {
	return c.config.IsClosedStatus
}

// isBlockedInMap returns true if the nib with the given ID is blocked by any
// nib whose status has not released its dependents. This is a pure function that
// operates on a map of nibs without locking, so releasesDependents and
// configPrefix come from the caller.
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
// any nib: neither it nor the dependent may be in a status that releases
// dependents. Computed from other nibs' blockedBy fields, whose entries are
// matched to nibID EXACTLY.
//
// It is not the mirror of isBlockedInMap, which never consults the subject's own
// status and resolves short-form blocker ids through normalizeIDInMap. A
// released dependent, or a short-form link no canonicalization sweep has
// reached, makes the two directions of one edge disagree.
//
// This is a pure function that operates on a map of nibs without locking, so
// releasesDependents comes from the caller.
func isBlockingInMap(nibs map[string]*nib.Nib, nibID string, releasesDependents func(string) bool) bool {
	b, ok := nibs[nibID]
	if !ok || releasesDependents(b.Status) {
		return false
	}

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

// findActiveBlockersInMap returns the nibs actively blocking the given nib:
// those its blockedBy names whose status has not released its dependents, per
// the caller-supplied predicate. Single-side storage — only the nib's blockedBy
// field is read. This is a pure function that operates on a map of nibs without
// locking, so releasesDependents and configPrefix come from the caller.
//
// Each blockedBy entry is resolved through normalizeIDInMap (the exact-then-
// prefixed rule Core.Get applies), so a short-form entry in a map no sweep has
// canonicalized still finds its blocker. nibID itself is looked up exactly,
// never normalized: this and Core.IsBlocked both answer "not blocked" for a
// short subject id, so resolve one through Core.NormalizeID first.
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
