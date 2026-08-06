package nibcore

// The token-keyed reverse-mention index that backs Core.FindMentions /
// Core.FindMentionedBy lives in mention_index.go. The pure functions
// FindMentionsInMap / FindMentionedByInMap below remain as oracles — they
// operate on a map without any index, so they can be used to differentially
// verify the indexed Core methods in tests.

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
// nib.NewID always prepends the configured prefix, so nothing this program
// CREATES can put a bare token and its prefixed form in the map at once. The
// loader can: nib.ParseFilename derives a bare id from any filename that does
// not carry the prefix, so a hand-added or imported `e1.md` sitting next to
// `nibs-e1.md` makes the ordering below user-visible — `e1` names the bare nib,
// and the prefixed twin is reachable only by its full id.
//
// That makes what an id resolves to a property of the current key set. The store
// copes by re-resolving stored link ids on every removal (Core.Delete and the
// watcher's removal branch) and on every id arriving through the watcher — see
// canonicalize.go, in particular removalCanRebindLinksLocked for the removal
// that unmasks a prefixed twin.
//
// Core.Create is the gap: it inserts a key without re-resolving, and because it
// inserts BEFORE the watcher sees the file, the watcher's arrival sweep does not
// fire for it either. A dangling link whose prefixed form equals a newly created
// id therefore keeps its bare spelling while this function answers with the new
// nib. Reachable only when a generated id collides that way, so it is recorded
// rather than closed — do not read the coverage above as universal.
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

// FindMentions returns the nibs mentioned via `#<id>` in fromID's body.
// Results are deduplicated, exclude self-references and unresolved tokens,
// and preserve first-appearance order. Short IDs are normalized via the
// same exact-match-then-prefix-prepended rule as Core.Get / Core.NormalizeID.
//
// The outbound list of raw mention tokens is served from the reverse-mention
// index (populated at Load and maintained by Create/Update/Delete + the
// watcher), so no body re-parse happens here.
func (c *Core) FindMentions(fromID string) []*nib.Nib {
	c.mu.RLock()
	defer c.mu.RUnlock()
	fullID, ok := c.normalizeIDForLookupLocked(fromID)
	if !ok {
		return nil
	}
	tokens := c.mentionIdx.OutboundTokens(fullID)
	if len(tokens) == 0 {
		return nil
	}
	prefix := c.configPrefix()
	seen := make(map[string]struct{}, len(tokens))
	var out []*nib.Nib
	for _, tok := range tokens {
		resolvedID, ok := normalizeIDInMap(c.nibs, tok, prefix)
		if !ok || resolvedID == fullID {
			continue
		}
		if _, dup := seen[resolvedID]; dup {
			continue
		}
		seen[resolvedID] = struct{}{}
		out = append(out, c.nibs[resolvedID])
	}
	return out
}

// FindMentionedBy returns the nibs whose bodies contain a `#<id>` mention
// resolving to targetID. Results are deduplicated, exclude self-references,
// and are returned sorted by ID for deterministic ordering.
//
// Served from the reverse-mention index: for each token form that can
// resolve to the target (full ID; plus the short form if the full ID
// carries the configured prefix), we union the inbound source sets instead
// of re-parsing every body in the store.
func (c *Core) FindMentionedBy(targetID string) []*nib.Nib {
	c.mu.RLock()
	defer c.mu.RUnlock()
	fullID, ok := c.normalizeIDForLookupLocked(targetID)
	if !ok {
		return nil
	}
	// A source's body might carry either the full-form token ("nibs-abc")
	// or the short-form token ("abc") — both resolve to the same target,
	// so the inbound lookup must union both sets.
	tokens := []string{fullID}
	prefix := c.configPrefix()
	if prefix != "" && strings.HasPrefix(fullID, prefix) {
		if short := strings.TrimPrefix(fullID, prefix); short != "" && short != fullID {
			tokens = append(tokens, short)
		}
	}

	seen := make(map[string]struct{})
	for _, tok := range tokens {
		for _, srcID := range c.mentionIdx.InboundSources(tok) {
			if srcID == fullID {
				continue
			}
			seen[srcID] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil
	}

	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]*nib.Nib, 0, len(ids))
	for _, id := range ids {
		if b, ok := c.nibs[id]; ok {
			out = append(out, b)
		}
	}
	return out
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
