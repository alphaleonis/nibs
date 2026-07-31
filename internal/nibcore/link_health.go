package nibcore

import (
	"os"
	"path/filepath"
	"slices"

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
//
// A parent or blockedBy target is resolved through normalizeIDInMap — the
// exact id, then the configured prefix prepended — so it is broken only when
// no nib answers to it under either spelling. That is the same rule Core.Get
// and findActiveBlockersInMap apply, which matters because Core.FixBrokenLinks
// repeats these checks and writes: a bare map lookup here called a resolvable
// short-form target broken, and `nibs check --fix` then deleted it from the
// file. configPrefix is threaded in because a pure map function cannot reach
// the project config itself.
//
// Resolution also decides self versus broken: a target that resolves back to
// the nib holding it is a self link however it was spelled.
//
// The cycle pass below, the reverse traversals (findIncomingLinksInMap,
// isBlockingInMap) and the setParent cycle guard all walk exact map keys, and
// are correct because every id in the store is already full: the loader
// resolves short-form link ids once, at the disk-read boundary (see
// canonicalize.go). Resolving again here is what keeps this check honest for
// the ids canonicalization deliberately leaves verbatim — an id naming no nib
// is broken however it is spelled, and the report names the spelling the file
// holds, which is what `--fix` would drop.
func CheckAllLinksInMap(nibs map[string]*nib.Nib, projectRoot, configPrefix string) *LinkCheckResult {
	result := &LinkCheckResult{
		BrokenLinks:     []BrokenLink{},
		SelfLinks:       []SelfLink{},
		Cycles:          []Cycle{},
		BrokenDocuments: []BrokenDocument{},
	}

	// Check for broken links and self-references
	for _, b := range nibs {
		// Check parent link. Target reports the spelling as stored, which is
		// what `--fix` would drop.
		if b.Parent != "" {
			fullID, ok := normalizeIDInMap(nibs, b.Parent, configPrefix)
			switch {
			case !ok:
				result.BrokenLinks = append(result.BrokenLinks, BrokenLink{
					NibID:    b.ID,
					LinkType: "parent",
					Target:   b.Parent,
				})
			case fullID == b.ID:
				result.SelfLinks = append(result.SelfLinks, SelfLink{
					NibID:    b.ID,
					LinkType: "parent",
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
			fullID, ok := normalizeIDInMap(nibs, blocker, configPrefix)
			switch {
			case !ok:
				result.BrokenLinks = append(result.BrokenLinks, BrokenLink{
					NibID:    b.ID,
					LinkType: "blocked_by",
					Target:   blocker,
				})
			case fullID == b.ID:
				result.SelfLinks = append(result.SelfLinks, SelfLink{
					NibID:    b.ID,
					LinkType: "blocked_by",
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
	return CheckAllLinksInMap(c.nibs, projectRoot, c.configPrefix())
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
//
// Copy-on-write: for every nib that actually changes we clone it, mutate the
// clone, persist the clone, and reinstall it under c.nibs[id] — the stored
// pointer is never edited in place. This upholds the canonical live-pointer /
// copy-on-write invariant (see NibReader.GetSnapshot in
// internal/graph/interfaces.go): the changed fields here (Parent, a torn string;
// BlockedBy, a memory-unsafe torn slice header) are non-Path, so they must land
// on a fresh pointer, leaving any off-lock reader still holding the old one a
// stable, unmutated value. Ranging over c.nibs while reassigning an existing
// key's value is safe in Go.
func (c *Core) RemoveLinksTo(targetID string) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	removed := 0
	for id, b := range c.nibs {
		// Detect changes by READING the stored pointer only — never mutate it,
		// so an unchanged nib skips the Clone() below.
		removeParent := b.Parent == targetID
		removeBlocker := slices.Contains(b.BlockedBy, targetID)

		if !removeParent && !removeBlocker {
			continue
		}

		clone := b.Clone()
		if removeParent {
			clone.Parent = ""
			removed++
		}
		if removeBlocker {
			// RemoveBlockedBy strips every occurrence of targetID; count the
			// drops via the length delta (matching FixBrokenLinks).
			before := len(clone.BlockedBy)
			clone.RemoveBlockedBy(targetID)
			removed += before - len(clone.BlockedBy)
		}

		if err := c.saveToDisk(clone); err != nil {
			return removed, err
		}
		c.nibs[id] = clone
	}

	return removed, nil
}

// FixBrokenLinks removes all broken links (links to non-existent nibs) and self-references.
// Returns the number of issues fixed.
//
// It restates the parent, blockedBy and document checks CheckAllLinksInMap
// makes, resolving each link target through normalizeIDInMap the same way, so
// `nibs check --fix` removes exactly the broken links, self links and broken
// documents `nibs check` reported. Cycles are the one reported category left
// untouched here; the command prints them as not auto-fixable instead.
//
// A link that resolves is left exactly as stored: nothing here rewrites a
// short id into its full form.
//
// Copy-on-write for the same reason as RemoveLinksTo: mutate a clone and
// reinstall it rather than editing the stored pointer in place, so no off-lock
// reader ever sees a stored pointer's non-Path fields torn mid-write. See the
// canonical live-pointer / copy-on-write invariant at NibReader.GetSnapshot
// (internal/graph/interfaces.go). Documents is made copy-on-write here too, for
// the same discipline, so any future off-lock reader of it is safe as well.
func (c *Core) FixBrokenLinks() (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	projectRoot := filepath.Dir(c.root)
	configPrefix := c.configPrefix()

	fixed := 0
	for id, b := range c.nibs {
		// Detect changes by READING the stored pointer only — never mutate it.

		// Parent is dropped when it resolves back to this nib (self) or
		// resolves to nothing (broken).
		fixParent := false
		if b.Parent != "" {
			fullID, ok := normalizeIDInMap(c.nibs, b.Parent, configPrefix)
			fixParent = !ok || fullID == b.ID
		}

		// Surviving blocked_by set (drop self-refs and links to missing nibs).
		// Survivors keep the spelling they were stored with.
		var newBlockedBy []string
		for _, blocker := range b.BlockedBy {
			fullID, ok := normalizeIDInMap(c.nibs, blocker, configPrefix)
			if !ok || fullID == b.ID {
				continue
			}
			newBlockedBy = append(newBlockedBy, blocker)
		}
		blockedRemoved := len(b.BlockedBy) - len(newBlockedBy)

		// Surviving document set (drop paths that no longer exist on disk).
		var newDocs []string
		for _, docPath := range b.Documents {
			absPath := filepath.Join(projectRoot, docPath)
			if _, err := os.Stat(absPath); !os.IsNotExist(err) {
				newDocs = append(newDocs, docPath)
			}
		}
		docsRemoved := len(b.Documents) - len(newDocs)

		if !fixParent && blockedRemoved == 0 && docsRemoved == 0 {
			continue
		}

		clone := b.Clone()
		if fixParent {
			clone.Parent = ""
			fixed++
		}
		if blockedRemoved > 0 {
			clone.BlockedBy = newBlockedBy
			fixed += blockedRemoved
		}
		if docsRemoved > 0 {
			clone.Documents = newDocs
			fixed += docsRemoved
		}

		if err := c.saveToDisk(clone); err != nil {
			return fixed, err
		}
		c.nibs[id] = clone
	}

	return fixed, nil
}
