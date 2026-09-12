package nibcore

import "github.com/alphaleonis/nibs/internal/nib"

// DefaultSearchLimit caps each leg of Core.Search independently: the full-text
// index query and the direct ID match are each limited to this many results, so
// a combined result may hold up to twice this many nibs. It bounds Core.Search
// only — Core.SearchAll answers the same query with no cap.
const DefaultSearchLimit = 1000

// Unlimited is the SearchIndex.Search limit that means "no cap — every match".
//
// The sentinel INVERTS the usual convention, where a zero limit means "unset,
// substitute a default". Spell it Unlimited at each call site rather than 0.
// Negative limits mean the same thing.
const Unlimited = 0

// SearchIndex is the full-text leg of Core.Search: Core unions direct ID
// matches on top of whatever the index returns.
//
// Implementations must be safe for concurrent use after construction.
// Core calls IndexNib/IndexNibs/DeleteNib under its own mutex, but calls
// Search outside the mutex after capturing the index reference.
type SearchIndex interface {
	// IndexNib adds or updates a single nib in the search index (upsert).
	IndexNib(b *nib.Nib) error
	// IndexNibs adds or updates multiple nibs (upsert). Additive: existing
	// entries not in the slice are retained, not removed.
	IndexNibs(nibs []*nib.Nib) error
	// DeleteNib removes a nib by ID. Must be idempotent (no error if absent).
	DeleteNib(id string) error
	// Search returns IDs of nibs matching the query, in relevance order, up to
	// limit results. A limit <= 0 (spelled Unlimited) means NO cap — every
	// match, however many that is. An implementation whose backend needs a
	// concrete size must express "no cap" as a size larger than any answer it
	// can produce: not by substituting a default, which makes Core.SearchAll
	// regain the cap it exists to avoid, and not by measuring the backing store
	// in a separate operation, which a concurrent write can invalidate before
	// the search runs.
	Search(query string, limit int) ([]string, error)
	// Close releases resources held by the index.
	Close() error
}

// NoOpSearchIndex is a search index that does nothing, for tests that exercise
// Core logic without search. It silences only the full-text leg of Core.Search:
// direct ID matching runs against the in-memory nib map regardless.
type NoOpSearchIndex struct{}

func (NoOpSearchIndex) IndexNib(*nib.Nib) error              { return nil }
func (NoOpSearchIndex) IndexNibs([]*nib.Nib) error           { return nil }
func (NoOpSearchIndex) DeleteNib(string) error               { return nil }
func (NoOpSearchIndex) Search(string, int) ([]string, error) { return nil, nil }
func (NoOpSearchIndex) Close() error                         { return nil }
