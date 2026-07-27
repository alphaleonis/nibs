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

// closedPredicate returns the closed-status test the pure map functions need.
// The definition lives in config (Config.IsClosedStatus) and is threaded in
// rather than duplicated here, so nibcore has no status list of its own.
//
// c.config may be nil — cmd/init.go builds such a Core, as do several tests in
// this package. Binding and calling IsClosedStatus on a nil *Config is safe
// because it answers from the package-level config.DefaultStatuses and never
// dereferences its receiver; TestCoreClosedPredicate/config-less pins that. If
// status data ever moves onto the Config value, this becomes a nil dereference
// and New should normalize a nil config instead.
func (c *Core) closedPredicate() func(string) bool {
	return c.config.IsClosedStatus
}

// isBlockedInMap returns true if the nib with the given ID is blocked by any
// open (non-closed) nibs. isClosed is the closed-status predicate, supplied by
// the caller because this is a pure function that operates on a map of nibs
// without locking and so cannot reach the project config itself.
func isBlockedInMap(nibs map[string]*nib.Nib, nibID string, isClosed func(string) bool) bool {
	return len(findActiveBlockersInMap(nibs, nibID, isClosed)) > 0
}

// IsBlocked returns true if the nib with the given ID is blocked by any
// open (non-closed) nibs.
// Thread-safe wrapper around isBlockedInMap.
func (c *Core) IsBlocked(nibID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return isBlockedInMap(c.nibs, nibID, c.closedPredicate())
}

// isBlockingInMap returns true if the nib with the given ID is actively blocking
// any open nibs. The nib itself must also be open to be considered actively
// blocking. Computed from other nibs' blockedBy fields. isClosed is the
// closed-status predicate, supplied by the caller because this is a pure
// function that operates on a map of nibs without locking and so cannot reach
// the project config itself.
func isBlockingInMap(nibs map[string]*nib.Nib, nibID string, isClosed func(string) bool) bool {
	b, ok := nibs[nibID]
	if !ok || isClosed(b.Status) {
		return false
	}

	// Check other nibs that list this nib in their blocked_by
	for _, other := range nibs {
		if isClosed(other.Status) {
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
// any open nibs.
// Thread-safe wrapper around isBlockingInMap.
func (c *Core) IsBlocking(nibID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return isBlockingInMap(c.nibs, nibID, c.closedPredicate())
}

// findActiveBlockersInMap returns all nibs that are actively blocking the given
// nib. A blocker is "active" if its status is open (not closed), per the
// caller-supplied isClosed predicate — this is a pure function that operates on
// a map of nibs without locking and so cannot reach the project config itself.
// Single-side storage: only reads from the nib's blockedBy field.
func findActiveBlockersInMap(nibs map[string]*nib.Nib, nibID string, isClosed func(string) bool) []*nib.Nib {
	b, ok := nibs[nibID]
	if !ok {
		return nil
	}

	var blockers []*nib.Nib
	for _, blockerID := range b.BlockedBy {
		if blocker, ok := nibs[blockerID]; ok {
			if !isClosed(blocker.Status) {
				blockers = append(blockers, blocker)
			}
		}
	}

	return blockers
}

// FindActiveBlockers returns all nibs that are actively blocking the given nib.
// Thread-safe wrapper around findActiveBlockersInMap.
func (c *Core) FindActiveBlockers(nibID string) []*nib.Nib {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return findActiveBlockersInMap(c.nibs, nibID, c.closedPredicate())
}
