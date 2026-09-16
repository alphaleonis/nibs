package nibcore

import (
	"hash/fnv"

	"github.com/alphaleonis/nibs/internal/nib"
)

// searchDocDigest fingerprints the four fields the search index stores (see
// search.nibDocument). A load re-indexes a nib only when this changes, so a
// field ADDED to that document must be added here too, or the index keeps the
// old value of it until something else re-indexes that nib.
//
// A 64-bit digest, not the fields themselves: the alternative retains every
// body a second time. A collision leaves one nib's indexed text stale until the
// next write to it.
func searchDocDigest(b *nib.Nib) uint64 {
	h := fnv.New64a()
	for _, field := range []string{b.ID, b.Slug, b.Title, b.Body} {
		_, _ = h.Write([]byte(field))
		// A separator, so moving text across a field boundary changes the digest.
		_, _ = h.Write([]byte{0})
	}
	return h.Sum64()
}

// syncSearchIndexLocked brings the search index in line with c.nibs, indexing
// only what changed since this Core last indexed it and dropping what is gone.
// Must be called with c.mu held.
//
// It is keyed on what THIS Core put in the index rather than on the previous
// c.nibs, because the two differ: SetSearchIndex can install an empty index
// after a load, and ensureSearchIndexLocked builds one lazily on first search.
// Diffing against the old map would leave everything unchanged and the new index
// empty.
//
// Best-effort, like the whole-store re-index it replaces: a failure warns rather
// than failing the load. An index write that fails records no digest, so the next
// load tries again; a delete that fails leaves a stale entry, which Search
// filters out against c.nibs.
func (c *Core) syncSearchIndexLocked() {
	if c.searchIndex == nil {
		return
	}

	changed := make([]*nib.Nib, 0, len(c.nibs))
	next := make(map[string]uint64, len(c.nibs))
	for id, b := range c.nibs {
		digest := searchDocDigest(b)
		next[id] = digest
		if indexed, ok := c.indexedDigests[id]; !ok || indexed != digest {
			changed = append(changed, b)
		}
	}

	if len(changed) > 0 {
		if err := c.searchIndex.IndexNibs(changed); err != nil {
			// Keep the digests as they were: none of this batch reached the index.
			c.logWarn("failed to re-populate search index after reload: %v", err)
			return
		}
	}

	for id := range c.indexedDigests {
		if _, present := c.nibs[id]; present {
			continue
		}
		if err := c.searchIndex.DeleteNib(id); err != nil {
			c.logWarn("failed to remove nib %s from search index: %v", id, err)
		}
	}

	c.indexedDigests = next
}

// recordIndexedLocked notes that the index now holds this nib's current text.
// Must be called with c.mu held.
func (c *Core) recordIndexedLocked(b *nib.Nib) {
	if c.indexedDigests == nil {
		c.indexedDigests = make(map[string]uint64)
	}
	c.indexedDigests[b.ID] = searchDocDigest(b)
}

// forgetIndexedLocked drops what this Core believes the index holds for one nib,
// so the next load indexes it again. Must be called with c.mu held.
func (c *Core) forgetIndexedLocked(id string) {
	delete(c.indexedDigests, id)
}

// recordAllIndexedLocked notes that the index holds the current text of every
// nib in the store, after a whole-store index. Must be called with c.mu held.
func (c *Core) recordAllIndexedLocked() {
	digests := make(map[string]uint64, len(c.nibs))
	for id, b := range c.nibs {
		digests[id] = searchDocDigest(b)
	}
	c.indexedDigests = digests
}
