package nibcore

// Mention lookups currently recompute from live bodies on every call, so no
// explicit invalidation hook is needed. If a cache/index is added, wire it
// into Core.Create/Update/Delete and watcher.handleChanges. See
// search_index.go for the indexed pattern, link_queries.go for the O(N)
// field-based pattern. Tracking nib: nibs-rckp.

import (
	"sort"
	"strings"

	"github.com/alphaleonis/nibs/internal/nib"
)

// normalizeIDInMap is the single source of truth for the exact-match →
// prefix-prepended ID resolution rule shared by Core.NormalizeID,
// Core.normalizeIDForLookupLocked, and the mention-resolution call sites in
// FindMentionsInMap / FindMentionedByInMap.
//
// Returns the full ID and true if the id resolves via either an exact map
// key or by prepending configPrefix; otherwise ("", false).
//
// Pure function operating on the given map without locking. Callers passing
// a Core.nibs map must hold Core.mu.RLock for the duration of the call.
//
// nib.NewID always prepends the configured prefix today, so a bare token
// and its prefixed form cannot both exist in the map — if that invariant
// is ever broken (e.g. importing externally generated nibs), the ordering
// here (exact-first, prefix-prepended-second) becomes user-visible and
// needs revisiting.
func normalizeIDInMap(nibs map[string]*nib.Nib, id, configPrefix string) (string, bool) {
	if _, ok := nibs[id]; ok {
		return id, true
	}
	if configPrefix != "" && !strings.HasPrefix(id, configPrefix) {
		full := configPrefix + id
		if _, ok := nibs[full]; ok {
			return full, true
		}
	}
	return "", false
}

// FindMentionsInMap returns the nibs whose IDs are referenced via `#<id>`
// mentions in fromID's body. Results are deduplicated, exclude self-references,
// and exclude unresolved tokens. Nibs are returned in order of first appearance
// in the body.
//
// Pure function operating on the given map without locking. Callers passing a
// Core.nibs map must hold Core.mu.RLock for the duration of the call and any
// subsequent use of returned *nib.Nib pointers.
func FindMentionsInMap(nibs map[string]*nib.Nib, fromID, configPrefix string) []*nib.Nib {
	from, ok := nibs[fromID]
	if !ok {
		return nil
	}
	tokens := nib.ExtractMentionTokens(from.Body)
	if len(tokens) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(tokens))
	var out []*nib.Nib
	for _, tok := range tokens {
		fullID, ok := normalizeIDInMap(nibs, tok, configPrefix)
		if !ok || fullID == fromID {
			continue
		}
		if _, dup := seen[fullID]; dup {
			continue
		}
		seen[fullID] = struct{}{}
		out = append(out, nibs[fullID])
	}
	return out
}

// FindMentionedByInMap returns the nibs whose bodies contain a `#<id>` mention
// resolving to targetID. Results are deduplicated (a nib is returned once even
// if it mentions the target multiple times) and exclude self-references. The
// returned slice is sorted by ID for deterministic ordering across calls.
//
// Pure function operating on the given map without locking. Callers passing a
// Core.nibs map must hold Core.mu.RLock for the duration of the call and any
// subsequent use of returned *nib.Nib pointers.
func FindMentionedByInMap(nibs map[string]*nib.Nib, targetID, configPrefix string) []*nib.Nib {
	if _, ok := nibs[targetID]; !ok {
		return nil
	}

	var out []*nib.Nib
	for _, b := range nibs {
		if b.ID == targetID {
			continue
		}
		tokens := nib.ExtractMentionTokens(b.Body)
		for _, tok := range tokens {
			fullID, ok := normalizeIDInMap(nibs, tok, configPrefix)
			if !ok {
				continue
			}
			if fullID == targetID {
				out = append(out, b)
				break
			}
		}
	}
	// Map iteration order is randomized — sort by ID so callers see a
	// stable, deterministic result across repeated invocations.
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// FindMentions is the thread-safe wrapper around FindMentionsInMap using the
// Core's nib map and configured prefix. Short IDs are normalized via the
// same exact-match-then-prefix-prepended rule as Core.Get / Core.NormalizeID,
// so callers can pass either form consistently.
func (c *Core) FindMentions(fromID string) []*nib.Nib {
	c.mu.RLock()
	defer c.mu.RUnlock()
	fullID, ok := c.normalizeIDForLookupLocked(fromID)
	if !ok {
		return nil
	}
	return FindMentionsInMap(c.nibs, fullID, c.configPrefix())
}

// FindMentionedBy is the thread-safe wrapper around FindMentionedByInMap.
// Short IDs are normalized, matching Core.Get / Core.NormalizeID.
func (c *Core) FindMentionedBy(targetID string) []*nib.Nib {
	c.mu.RLock()
	defer c.mu.RUnlock()
	fullID, ok := c.normalizeIDForLookupLocked(targetID)
	if !ok {
		return nil
	}
	return FindMentionedByInMap(c.nibs, fullID, c.configPrefix())
}

// normalizeIDForLookupLocked mirrors Core.NormalizeID but assumes the caller
// already holds c.mu. Returns (fullID, true) if the ID resolves via exact
// match or prefix prepending, otherwise ("", false). Delegates to the
// shared normalizeIDInMap helper so resolution logic stays in one place.
func (c *Core) normalizeIDForLookupLocked(id string) (string, bool) {
	return normalizeIDInMap(c.nibs, id, c.configPrefix())
}

func (c *Core) configPrefix() string {
	if c.config == nil {
		return ""
	}
	return c.config.Nibs.Prefix
}
