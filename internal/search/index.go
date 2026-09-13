// Package search provides full-text search functionality for nibs using Bleve.
package search

import (
	"math"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search/query"
)

// Index wraps a Bleve in-memory index for searching nibs.
type Index struct {
	index bleve.Index
}

// nibDocument is the structure stored in the Bleve index.
type nibDocument struct {
	ID    string `json:"id"`
	Slug  string `json:"slug"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

// NewIndex creates a new in-memory Bleve index.
func NewIndex() (*Index, error) {
	indexMapping := buildIndexMapping()
	idx, err := bleve.NewMemOnly(indexMapping)
	if err != nil {
		return nil, err
	}

	return &Index{index: idx}, nil
}

// buildIndexMapping creates the Bleve index mapping for nib documents.
func buildIndexMapping() mapping.IndexMapping {
	textFieldMapping := bleve.NewTextFieldMapping()
	textFieldMapping.Analyzer = "standard"

	// id is stored as a single keyword token.
	keywordFieldMapping := bleve.NewKeywordFieldMapping()

	nibMapping := bleve.NewDocumentMapping()
	nibMapping.AddFieldMappingsAt("id", keywordFieldMapping)
	nibMapping.AddFieldMappingsAt("slug", textFieldMapping)
	nibMapping.AddFieldMappingsAt("title", textFieldMapping)
	nibMapping.AddFieldMappingsAt("body", textFieldMapping)

	indexMapping := bleve.NewIndexMapping()
	indexMapping.DefaultMapping = nibMapping
	indexMapping.DefaultAnalyzer = "standard"
	indexMapping.IndexDynamic = false
	indexMapping.StoreDynamic = false

	// BM25 saturates repeated terms and normalizes for document length.
	indexMapping.ScoringModel = "bm25"

	return indexMapping
}

// Close closes the index.
func (idx *Index) Close() error {
	return idx.index.Close()
}

// IndexNib adds or updates a nib in the search index.
func (idx *Index) IndexNib(b *nib.Nib) error {
	doc := nibDocument{
		ID:    b.ID,
		Slug:  b.Slug,
		Title: b.Title,
		Body:  b.Body,
	}
	return idx.index.Index(b.ID, doc)
}

// DeleteNib removes a nib from the search index.
func (idx *Index) DeleteNib(id string) error {
	return idx.index.Delete(id)
}

// Search returns the ids of nibs matching queryStr, in relevance order. limit caps
// the result; limit <= 0 returns every match, so pass 0 when intersecting with an
// already bounded set.
//
// queryStr uses Bleve's query-string syntax (terms, AND, wildcards, "phrases",
// field:term). Input that syntax rejects, such as `type:`, a lone `-` or an
// unbalanced quote, is matched as plain text instead; an index failure still
// returns an error.
func (idx *Index) Search(queryStr string, limit int) ([]string, error) {
	if limit <= 0 {
		// Bleve needs a concrete Size; its collector caps preallocation at 1000.
		limit = math.MaxInt32
	}

	ids, err := idx.runQuery(bleve.NewQueryStringQuery(queryStr), limit)
	if err != nil {
		return idx.runQuery(bleve.NewMatchQuery(queryStr), limit)
	}
	return ids, nil
}

// runQuery executes a single Bleve query and returns the matching nib IDs.
func (idx *Index) runQuery(q query.Query, limit int) ([]string, error) {
	searchRequest := bleve.NewSearchRequest(q)
	searchRequest.Size = limit
	searchRequest.Fields = []string{"id"}

	result, err := idx.index.Search(searchRequest)
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(result.Hits))
	for _, hit := range result.Hits {
		ids = append(ids, hit.ID)
	}

	return ids, nil
}

// IndexNibs indexes multiple nibs in a batch for efficiency.
func (idx *Index) IndexNibs(nibs []*nib.Nib) error {
	batch := idx.index.NewBatch()
	for _, b := range nibs {
		doc := nibDocument{
			ID:    b.ID,
			Slug:  b.Slug,
			Title: b.Title,
			Body:  b.Body,
		}
		if err := batch.Index(b.ID, doc); err != nil {
			return err
		}
	}
	return idx.index.Batch(batch)
}
