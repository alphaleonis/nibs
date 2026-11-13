package nibcore

import (
	"errors"
	"sync"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
)

var errFillFailed = errors.New("fill failed")

// partialFillIndex is a real Bleve index whose IndexNibs stores the first
// `reach` nibs of the batch and then fails.
type partialFillIndex struct {
	SearchIndex
	reach   int
	reached map[string]bool
	closed  bool
}

func (p *partialFillIndex) IndexNibs(nibs []*nib.Nib) error {
	for i, b := range nibs {
		if i == p.reach {
			return errFillFailed
		}
		if err := p.IndexNib(b); err != nil {
			return err
		}
		p.reached[b.ID] = true
	}
	return nil
}

func (p *partialFillIndex) Close() error {
	p.closed = true
	return p.SearchIndex.Close()
}

// failingFillIndex is an index whose whole-store fill always fails.
type failingFillIndex struct{ NoOpSearchIndex }

func (failingFillIndex) IndexNibs([]*nib.Nib) error { return errFillFailed }

// TestSearch_FailedFillIsRetried fails the first fill after one nib, so the
// search after it must find a nib that fill never reached.
func TestSearch_FailedFillIsRetried(t *testing.T) {
	core, _ := setupTestCore(t)
	defer func() { _ = core.Close() }()

	words := map[string]string{"fil1": "apricot", "fil2": "blueberry", "fil3": "cranberry"}
	for id, word := range words {
		if err := core.Create(&nib.Nib{ID: id, Title: word}); err != nil {
			t.Fatalf("Create(%s) error = %v", id, err)
		}
	}

	var partial *partialFillIndex
	core.newSearchIndex = func() (SearchIndex, error) {
		fresh, err := newBleveIndex()
		if err != nil || partial != nil {
			return fresh, err
		}
		partial = &partialFillIndex{SearchIndex: fresh, reach: 1, reached: map[string]bool{}}
		return partial, nil
	}

	if _, err := core.Search("apricot"); !errors.Is(err, errFillFailed) {
		t.Fatalf("first Search() error = %v, want %v", err, errFillFailed)
	}
	if !partial.closed {
		t.Error("the index whose fill failed was not closed")
	}

	// Created while no index is installed, so only the retried fill indexes it.
	if err := core.Create(&nib.Nib{ID: "fil4", Title: "damson"}); err != nil {
		t.Fatalf("Create(fil4) error = %v", err)
	}
	words["fil4"] = "damson"

	for id, word := range words {
		if partial.reached[id] {
			continue
		}
		results, err := core.Search(word)
		if err != nil {
			t.Fatalf("Search(%q) error = %v", word, err)
		}
		if len(results) != 1 || results[0].ID != id {
			t.Errorf("Search(%q) = %v, want [%s]", word, nibIDs(results), id)
		}
	}
}

// TestSearch_PersistentFillFailureReachesEverySearch runs concurrent searches
// against a fill that always fails, then lets the fill succeed.
func TestSearch_PersistentFillFailureReachesEverySearch(t *testing.T) {
	core, _ := setupTestCore(t)
	defer func() { _ = core.Close() }()

	if err := core.Create(&nib.Nib{ID: "per1", Title: "elderberry"}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	core.newSearchIndex = func() (SearchIndex, error) { return failingFillIndex{}, nil }
	searchAll := func(check func(results []*nib.Nib, err error)) {
		var wg sync.WaitGroup
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				check(core.Search("elderberry"))
			}()
		}
		wg.Wait()
	}

	searchAll(func(_ []*nib.Nib, err error) {
		if !errors.Is(err, errFillFailed) {
			t.Errorf("Search() during a failing fill: error = %v, want %v", err, errFillFailed)
		}
	})

	core.newSearchIndex = newBleveIndex
	searchAll(func(results []*nib.Nib, err error) {
		if err != nil || len(results) != 1 || results[0].ID != "per1" {
			t.Errorf("Search() after the fill recovers = %v, %v, want [per1], nil", nibIDs(results), err)
		}
	})
}

func nibIDs(nibs []*nib.Nib) []string {
	ids := make([]string, 0, len(nibs))
	for _, b := range nibs {
		ids = append(ids, b.ID)
	}
	return ids
}
