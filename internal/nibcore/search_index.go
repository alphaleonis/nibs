package nibcore

import "github.com/alphaleonis/nibs/internal/nib"

// DefaultSearchLimit caps each leg of Core.Search independently: the
// full-text index query and the direct ID match are each limited to this
// many results, so a combined result may hold up to twice this many nibs.
//
// It bounds Core.Search only. Core.SearchAll answers the same query with no cap
// at all, for the callers that intersect the answer with an already-bounded
// working set — see its doc for why a store-wide cap is the wrong bound there.
const DefaultSearchLimit = 1000

// Unlimited is the SearchIndex.Search limit that means "no cap — every match".
//
// It exists because the sentinel is an INVERSION of the usual convention: the
// obvious reading of a zero limit is "unset, substitute a default", and an
// implementation that reads it that way compiles, passes every existing test,
// and silently reinstates the cap Core.SearchAll exists to avoid. Naming the
// value puts that meaning at each call site instead of only in prose, so
// `idx.Search(query, Unlimited)` cannot be misread the way `idx.Search(query, 0)`
// can. Negative limits mean the same thing; this is the spelling to use.
const Unlimited = 0

// SearchIndex abstracts full-text search so that nibcore.Core can work with
// pluggable implementations (real Bleve index, no-op for tests, etc.).
// Implementations control only the full-text leg of Core.Search: Core unions
// direct ID matches on top of whatever the index returns.
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
	// can produce, NOT by substituting a default and not by measuring the
	// backing store in a separate operation: substituting a default makes
	// Core.SearchAll silently regain the cap it exists to avoid, and a separate
	// measuring operation lets a concurrent write invalidate the size before the
	// search runs, truncating the answer just as silently.
	Search(query string, limit int) ([]string, error)
	// Close releases resources held by the index.
	Close() error
}

// NoOpSearchIndex is a search index that does nothing. Useful for tests
// that exercise Core logic but don't need search functionality. Injecting
// it silences only the full-text leg of Core.Search: direct ID matching
// runs against the in-memory nib map regardless of the index.
type NoOpSearchIndex struct{}

func (NoOpSearchIndex) IndexNib(*nib.Nib) error              { return nil }
func (NoOpSearchIndex) IndexNibs([]*nib.Nib) error           { return nil }
func (NoOpSearchIndex) DeleteNib(string) error               { return nil }
func (NoOpSearchIndex) Search(string, int) ([]string, error) { return nil, nil }
func (NoOpSearchIndex) Close() error                         { return nil }
