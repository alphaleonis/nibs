package nibcore

import "github.com/alphaleonis/nibs/internal/nib"

// DefaultSearchLimit caps each leg of Core.Search independently: the
// full-text index query and the direct ID match are each limited to this
// many results, so a combined result may hold up to twice this many nibs.
const DefaultSearchLimit = 1000

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
	// Search returns IDs of nibs matching the query, up to limit results.
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
func (NoOpSearchIndex) DeleteNib(string) error                { return nil }
func (NoOpSearchIndex) Search(string, int) ([]string, error)  { return nil, nil }
func (NoOpSearchIndex) Close() error                          { return nil }
