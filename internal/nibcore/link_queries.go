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

// FindIncomingLinksInMap returns all nibs that link TO the given nib ID.
// Single-side storage: blocking relationships are only stored as blockedBy on the blocked nib.
// This is a pure function that operates on a map of nibs without locking.
func FindIncomingLinksInMap(nibs map[string]*nib.Nib, targetID string) []nib.IncomingLink {
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
// Thread-safe wrapper around FindIncomingLinksInMap.
func (c *Core) FindIncomingLinks(targetID string) []nib.IncomingLink {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return FindIncomingLinksInMap(c.nibs, targetID)
}

// isResolvedStatus delegates to nib.IsResolvedStatus — the canonical definition.
func isResolvedStatus(status string) bool {
	return nib.IsResolvedStatus(status)
}

// IsBlockedInMap returns true if the nib with the given ID is blocked by any
// active (non-completed, non-scrapped) nibs.
// This is a pure function that operates on a map of nibs without locking.
func IsBlockedInMap(nibs map[string]*nib.Nib, nibID string) bool {
	return len(FindActiveBlockersInMap(nibs, nibID)) > 0
}

// IsBlocked returns true if the nib with the given ID is blocked by any
// active (non-completed, non-scrapped) nibs.
// Thread-safe wrapper around IsBlockedInMap.
func (c *Core) IsBlocked(nibID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return IsBlockedInMap(c.nibs, nibID)
}

// IsBlockingInMap returns true if the nib with the given ID is actively blocking
// any non-resolved nibs. The nib itself must also be non-resolved to be
// considered actively blocking. Computed from other nibs' blockedBy fields.
// This is a pure function that operates on a map of nibs without locking.
func IsBlockingInMap(nibs map[string]*nib.Nib, nibID string) bool {
	b, ok := nibs[nibID]
	if !ok || isResolvedStatus(b.Status) {
		return false
	}

	// Check other nibs that list this nib in their blocked_by
	for _, other := range nibs {
		if isResolvedStatus(other.Status) {
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
// any non-resolved nibs.
// Thread-safe wrapper around IsBlockingInMap.
func (c *Core) IsBlocking(nibID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return IsBlockingInMap(c.nibs, nibID)
}

// FindActiveBlockersInMap returns all nibs that are actively blocking the given nib.
// A blocker is "active" if its status is NOT "completed" or "scrapped".
// Single-side storage: only reads from the nib's blockedBy field.
// This is a pure function that operates on a map of nibs without locking.
func FindActiveBlockersInMap(nibs map[string]*nib.Nib, nibID string) []*nib.Nib {
	b, ok := nibs[nibID]
	if !ok {
		return nil
	}

	var blockers []*nib.Nib
	for _, blockerID := range b.BlockedBy {
		if blocker, ok := nibs[blockerID]; ok {
			if !isResolvedStatus(blocker.Status) {
				blockers = append(blockers, blocker)
			}
		}
	}

	return blockers
}

// FindActiveBlockers returns all nibs that are actively blocking the given nib.
// Thread-safe wrapper around FindActiveBlockersInMap.
func (c *Core) FindActiveBlockers(nibID string) []*nib.Nib {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return FindActiveBlockersInMap(c.nibs, nibID)
}
