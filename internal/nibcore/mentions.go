package nibcore

import (
	"strings"

	"github.com/alphaleonis/nibs/internal/nib"
)

// resolveMentionToken resolves a token (short or full ID form) against the nib
// map. Returns the full ID and true if the token resolves to a known nib.
// Tries exact match first, then prefix-prepended match if the configured
// prefix is non-empty and not already present.
func resolveMentionToken(nibs map[string]*nib.Nib, token, configPrefix string) (string, bool) {
	if _, ok := nibs[token]; ok {
		return token, true
	}
	if configPrefix != "" && !strings.HasPrefix(token, configPrefix) {
		full := configPrefix + token
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
// Pure function operating on the given map without locking.
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
		fullID, ok := resolveMentionToken(nibs, tok, configPrefix)
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
// if it mentions the target multiple times) and exclude self-references.
//
// Pure function operating on the given map without locking.
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
			fullID, ok := resolveMentionToken(nibs, tok, configPrefix)
			if !ok {
				continue
			}
			if fullID == targetID {
				out = append(out, b)
				break
			}
		}
	}
	return out
}

// FindMentions is the thread-safe wrapper around FindMentionsInMap using the
// Core's nib map and configured prefix.
func (c *Core) FindMentions(fromID string) []*nib.Nib {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return FindMentionsInMap(c.nibs, fromID, c.configPrefix())
}

// FindMentionedBy is the thread-safe wrapper around FindMentionedByInMap.
func (c *Core) FindMentionedBy(targetID string) []*nib.Nib {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return FindMentionedByInMap(c.nibs, targetID, c.configPrefix())
}

func (c *Core) configPrefix() string {
	if c.config == nil {
		return ""
	}
	return c.config.Nibs.Prefix
}
