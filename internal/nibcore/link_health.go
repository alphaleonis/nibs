package nibcore

import (
	"os"
	"path/filepath"

	"github.com/alphaleonis/nibs/internal/nib"
)

// BrokenLink represents a link to a non-existent nib.
type BrokenLink struct {
	NibID    string `json:"nib_id"`
	LinkType string `json:"link_type"`
	Target   string `json:"target"`
}

// SelfLink represents a nib linking to itself.
type SelfLink struct {
	NibID    string `json:"nib_id"`
	LinkType string `json:"link_type"`
}

// Cycle represents a circular dependency in links.
type Cycle struct {
	LinkType string   `json:"link_type"`
	Path     []string `json:"path"`
}

// BrokenDocument represents a document link to a non-existent file.
type BrokenDocument struct {
	NibID string `json:"nib_id"`
	Path  string `json:"path"`
}

// LinkCheckResult contains all link validation issues found.
type LinkCheckResult struct {
	BrokenLinks     []BrokenLink     `json:"broken_links"`
	SelfLinks       []SelfLink       `json:"self_links"`
	Cycles          []Cycle          `json:"cycles"`
	BrokenDocuments []BrokenDocument `json:"broken_documents"`
}

// HasIssues returns true if any link issues were found.
func (r *LinkCheckResult) HasIssues() bool {
	return len(r.BrokenLinks) > 0 || len(r.SelfLinks) > 0 || len(r.Cycles) > 0 || len(r.BrokenDocuments) > 0
}

// TotalIssues returns the total count of all issues.
func (r *LinkCheckResult) TotalIssues() int {
	return len(r.BrokenLinks) + len(r.SelfLinks) + len(r.Cycles) + len(r.BrokenDocuments)
}

// CheckAllLinksInMap validates all links across all nibs.
// When projectRoot is empty, document filesystem checks are skipped.
// This is a pure function that operates on a map of nibs without locking.
func CheckAllLinksInMap(nibs map[string]*nib.Nib, projectRoot string) *LinkCheckResult {
	result := &LinkCheckResult{
		BrokenLinks:     []BrokenLink{},
		SelfLinks:       []SelfLink{},
		Cycles:          []Cycle{},
		BrokenDocuments: []BrokenDocument{},
	}

	// Check for broken links and self-references
	for _, b := range nibs {
		// Check parent link
		if b.Parent != "" {
			if b.Parent == b.ID {
				result.SelfLinks = append(result.SelfLinks, SelfLink{
					NibID:    b.ID,
					LinkType: "parent",
				})
			} else if _, ok := nibs[b.Parent]; !ok {
				result.BrokenLinks = append(result.BrokenLinks, BrokenLink{
					NibID:    b.ID,
					LinkType: "parent",
					Target:   b.Parent,
				})
			}
		}

		// Check document paths exist on disk (skip when projectRoot is empty)
		if projectRoot != "" {
			for _, docPath := range b.Documents {
				absPath := filepath.Join(projectRoot, docPath)
				if _, err := os.Stat(absPath); os.IsNotExist(err) {
					result.BrokenDocuments = append(result.BrokenDocuments, BrokenDocument{
						NibID: b.ID,
						Path:  docPath,
					})
				}
			}
		}

		// Check blocked_by links (single-side: blocking not persisted)
		for _, blocker := range b.BlockedBy {
			if blocker == b.ID {
				result.SelfLinks = append(result.SelfLinks, SelfLink{
					NibID:    b.ID,
					LinkType: "blocked_by",
				})
			} else if _, ok := nibs[blocker]; !ok {
				result.BrokenLinks = append(result.BrokenLinks, BrokenLink{
					NibID:    b.ID,
					LinkType: "blocked_by",
					Target:   blocker,
				})
			}
		}
	}

	// Check for cycles in blocked_by and parent links
	// (blocking is derived from blocked_by, so only these two need cycle checks)
	for _, linkType := range []string{"blocked_by", "parent"} {
		cycles := FindCyclesInMap(nibs, linkType)
		result.Cycles = append(result.Cycles, cycles...)
	}

	return result
}

// CheckAllLinks validates all links across all nibs.
// Thread-safe wrapper around CheckAllLinksInMap.
func (c *Core) CheckAllLinks() *LinkCheckResult {
	c.mu.RLock()
	defer c.mu.RUnlock()
	projectRoot := filepath.Dir(c.root)
	return CheckAllLinksInMap(c.nibs, projectRoot)
}

// FindCyclesInMap detects all cycles for a specific link type using DFS.
// This is a pure function that operates on a map of nibs without locking.
func FindCyclesInMap(nibs map[string]*nib.Nib, linkType string) []Cycle {
	var cycles []Cycle
	visited := make(map[string]bool)
	inStack := make(map[string]bool)
	seenCycles := make(map[string]bool) // To avoid duplicate cycle reports

	var dfs func(id string, path []string)
	dfs = func(id string, path []string) {
		if inStack[id] {
			// Found a cycle - find where the cycle starts
			cycleStart := -1
			for i, p := range path {
				if p == id {
					cycleStart = i
					break
				}
			}
			if cycleStart >= 0 {
				cyclePath := append(path[cycleStart:], id)
				// Create a canonical key to avoid duplicate cycles
				key := canonicalCycleKey(cyclePath)
				if !seenCycles[key] {
					seenCycles[key] = true
					cycles = append(cycles, Cycle{
						LinkType: linkType,
						Path:     cyclePath,
					})
				}
			}
			return
		}

		if visited[id] {
			return
		}

		visited[id] = true
		inStack[id] = true

		b, ok := nibs[id]
		if ok {
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

			for _, target := range targets {
				// Skip self-references (they're tracked separately as SelfLinks)
				if target == id {
					continue
				}
				dfs(target, append(path, id))
			}
		}

		inStack[id] = false
	}

	for id := range nibs {
		if !visited[id] {
			dfs(id, nil)
		}
	}

	return cycles
}

// canonicalCycleKey creates a unique key for a cycle to detect duplicates.
// It normalizes the cycle by starting from the smallest ID.
func canonicalCycleKey(path []string) string {
	if len(path) <= 1 {
		return ""
	}

	// Remove the duplicate end element (cycle closes back)
	cycle := path[:len(path)-1]

	// Find the minimum element to use as start
	minIdx := 0
	for i, id := range cycle {
		if id < cycle[minIdx] {
			minIdx = i
		}
	}

	// Rotate to start from minimum
	key := ""
	for i := 0; i < len(cycle); i++ {
		idx := (minIdx + i) % len(cycle)
		if i > 0 {
			key += "->"
		}
		key += cycle[idx]
	}

	return key
}

// RemoveLinksTo removes all links pointing to the given target ID from all nibs.
// Returns the number of links removed.
func (c *Core) RemoveLinksTo(targetID string) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	removed := 0
	for _, b := range c.nibs {
		changed := false

		// Remove parent link
		if b.Parent == targetID {
			b.Parent = ""
			changed = true
			removed++
		}

		// Remove blocked_by links (single-side: no blocking field to remove)
		originalBlockedByLen := len(b.BlockedBy)
		b.RemoveBlockedBy(targetID)
		if len(b.BlockedBy) < originalBlockedByLen {
			changed = true
			removed += originalBlockedByLen - len(b.BlockedBy)
		}

		if changed {
			if err := c.saveToDisk(b); err != nil {
				return removed, err
			}
		}
	}

	return removed, nil
}

// FixBrokenLinks removes all broken links (links to non-existent nibs) and self-references.
// Returns the number of issues fixed.
func (c *Core) FixBrokenLinks() (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	fixed := 0
	for _, b := range c.nibs {
		changed := false

		// Fix parent link
		if b.Parent != "" {
			if b.Parent == b.ID {
				b.Parent = ""
				changed = true
				fixed++
			} else if _, ok := c.nibs[b.Parent]; !ok {
				b.Parent = ""
				changed = true
				fixed++
			}
		}

		// Fix blocked_by links (single-side: no blocking field to fix)
		originalBlockedByLen := len(b.BlockedBy)
		var newBlockedBy []string
		for _, blocker := range b.BlockedBy {
			if blocker == b.ID {
				continue
			}
			if _, ok := c.nibs[blocker]; !ok {
				continue
			}
			newBlockedBy = append(newBlockedBy, blocker)
		}
		if len(newBlockedBy) < originalBlockedByLen {
			b.BlockedBy = newBlockedBy
			changed = true
			fixed += originalBlockedByLen - len(newBlockedBy)
		}

		// Fix broken document links
		if len(b.Documents) > 0 {
			projectRoot := filepath.Dir(c.root)
			originalDocLen := len(b.Documents)
			var newDocs []string
			for _, docPath := range b.Documents {
				absPath := filepath.Join(projectRoot, docPath)
				if _, err := os.Stat(absPath); !os.IsNotExist(err) {
					newDocs = append(newDocs, docPath)
				}
			}
			if len(newDocs) < originalDocLen {
				b.Documents = newDocs
				changed = true
				fixed += originalDocLen - len(newDocs)
			}
		}

		if changed {
			if err := c.saveToDisk(b); err != nil {
				return fixed, err
			}
		}
	}

	return fixed, nil
}
