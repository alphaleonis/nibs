// Package search provides full-text search functionality for nibs using Bleve.
package search

import (
	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/alphaleonis/nibs/internal/nib"
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
	// Create a text field mapping with the standard analyzer
	textFieldMapping := bleve.NewTextFieldMapping()
	textFieldMapping.Analyzer = "standard"

	// Create a keyword field mapping for ID (stored but not analyzed)
	keywordFieldMapping := bleve.NewKeywordFieldMapping()

	// Create the document mapping
	nibMapping := bleve.NewDocumentMapping()
	nibMapping.AddFieldMappingsAt("id", keywordFieldMapping)
	nibMapping.AddFieldMappingsAt("slug", textFieldMapping)
	nibMapping.AddFieldMappingsAt("title", textFieldMapping)
	nibMapping.AddFieldMappingsAt("body", textFieldMapping)

	// Create the index mapping with BM25 scoring for better relevance ranking
	indexMapping := bleve.NewIndexMapping()
	indexMapping.DefaultMapping = nibMapping
	indexMapping.DefaultAnalyzer = "standard"
	indexMapping.IndexDynamic = false
	indexMapping.StoreDynamic = false

	// Use BM25 scoring algorithm (available in Bleve v2.5.0+)
	// BM25 provides better relevance ranking than TF-IDF, especially for:
	// - Handling term frequency saturation (repeated terms don't over-boost)
	// - Normalizing for document length (short docs aren't unfairly penalized)
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

const defaultSearchLimit = 1000

// Search executes a search query and returns matching nib IDs.
// The limit parameter controls the maximum number of results (0 uses a default of 1000).
func (idx *Index) Search(queryStr string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = defaultSearchLimit
	}

	// Use query string syntax which supports:
	// - Simple terms: "authentication"
	// - Boolean operators: "user AND password"
	// - Wildcards: "auth*"
	// - Phrases: "\"user login\""
	// - Field-specific: "title:login"
	query := bleve.NewQueryStringQuery(queryStr)

	searchRequest := bleve.NewSearchRequest(query)
	searchRequest.Size = limit
	searchRequest.Fields = []string{"id"} // Only return ID field

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
