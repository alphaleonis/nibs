package graph

import (
	"context"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
)

// stubReaderWithLinks extends stubReader with configurable incoming links.
type stubReaderWithLinks struct {
	stubReader
	links map[string][]nib.IncomingLink
}

func (s *stubReaderWithLinks) FindIncomingLinks(targetID string) []nib.IncomingLink {
	return s.links[targetID]
}

func TestBlockedByIdsFiltersResolved(t *testing.T) {
	active := &nib.Nib{ID: "blocker-active", Status: "todo"}
	completed := &nib.Nib{ID: "blocker-done", Status: "completed"}
	scrapped := &nib.Nib{ID: "blocker-scrapped", Status: "scrapped"}
	target := &nib.Nib{
		ID:        "target",
		Status:    "todo",
		BlockedBy: []string{"blocker-active", "blocker-done", "blocker-scrapped"},
	}

	reader := &stubReader{
		nibs: map[string]*nib.Nib{
			"blocker-active":   active,
			"blocker-done":     completed,
			"blocker-scrapped": scrapped,
			"target":           target,
		},
	}

	writer := &stubWriter{}
	resolver := &Resolver{
		Reader:   reader,
		Blocking: &stubBlockingChecker{},
		Orderer:  NewOrderer(reader, writer),
	}
	nibRes := &nibResolver{resolver}

	ids, err := nibRes.BlockedByIds(context.Background(), target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("got %d IDs, want 1: %v", len(ids), ids)
	}
	if ids[0] != "blocker-active" {
		t.Errorf("got %q, want %q", ids[0], "blocker-active")
	}
}

func TestBlockedByFiltersResolved(t *testing.T) {
	active := &nib.Nib{ID: "blocker-active", Status: "in-progress"}
	completed := &nib.Nib{ID: "blocker-done", Status: "completed"}
	target := &nib.Nib{
		ID:        "target",
		Status:    "todo",
		BlockedBy: []string{"blocker-active", "blocker-done"},
	}

	reader := &stubReader{
		nibs: map[string]*nib.Nib{
			"blocker-active": active,
			"blocker-done":   completed,
			"target":         target,
		},
	}

	writer := &stubWriter{}
	resolver := &Resolver{
		Reader:   reader,
		Blocking: &stubBlockingChecker{},
		Orderer:  NewOrderer(reader, writer),
	}
	nibRes := &nibResolver{resolver}

	result, err := nibRes.BlockedBy(context.Background(), target, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("got %d blockers, want 1", len(result))
	}
	if result[0].ID != "blocker-active" {
		t.Errorf("got %q, want %q", result[0].ID, "blocker-active")
	}
}

func TestBlockedByIdsStillReturnedWhenTargetResolved(t *testing.T) {
	// A completed nib should still report its active blockers —
	// the filtering is on the blocker's status, not the target's.
	activBlocker := &nib.Nib{ID: "blocker-active", Status: "in-progress"}
	completedTarget := &nib.Nib{
		ID:        "target",
		Status:    "completed",
		BlockedBy: []string{"blocker-active"},
	}

	reader := &stubReader{
		nibs: map[string]*nib.Nib{
			"blocker-active": activBlocker,
			"target":         completedTarget,
		},
	}

	writer := &stubWriter{}
	resolver := &Resolver{
		Reader:   reader,
		Blocking: &stubBlockingChecker{},
		Orderer:  NewOrderer(reader, writer),
	}
	nibRes := &nibResolver{resolver}

	ids, err := nibRes.BlockedByIds(context.Background(), completedTarget)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("got %d IDs, want 1 (completed target should still show active blockers): %v", len(ids), ids)
	}
	if ids[0] != "blocker-active" {
		t.Errorf("got %q, want %q", ids[0], "blocker-active")
	}
}

func TestBlockingIdsFiltersResolved(t *testing.T) {
	blocker := &nib.Nib{ID: "blocker", Status: "todo"}
	activeBlockee := &nib.Nib{ID: "blockee-active", Status: "todo", BlockedBy: []string{"blocker"}}
	doneBlockee := &nib.Nib{ID: "blockee-done", Status: "completed", BlockedBy: []string{"blocker"}}

	reader := &stubReaderWithLinks{
		stubReader: stubReader{
			nibs: map[string]*nib.Nib{
				"blocker":         blocker,
				"blockee-active":  activeBlockee,
				"blockee-done":    doneBlockee,
			},
		},
		links: map[string][]nib.IncomingLink{
			"blocker": {
				{FromNib: activeBlockee, LinkType: "blocked_by"},
				{FromNib: doneBlockee, LinkType: "blocked_by"},
			},
		},
	}

	writer := &stubWriter{}
	resolver := &Resolver{
		Reader:   reader,
		Blocking: &stubBlockingChecker{},
		Orderer:  NewOrderer(reader, writer),
	}
	nibRes := &nibResolver{resolver}

	ids, err := nibRes.BlockingIds(context.Background(), blocker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("got %d IDs, want 1: %v", len(ids), ids)
	}
	if ids[0] != "blockee-active" {
		t.Errorf("got %q, want %q", ids[0], "blockee-active")
	}
}

func TestBlockingIdsEmptyWhenSelfResolved(t *testing.T) {
	completedBlocker := &nib.Nib{ID: "blocker", Status: "completed"}
	blockee := &nib.Nib{ID: "blockee", Status: "todo", BlockedBy: []string{"blocker"}}

	reader := &stubReaderWithLinks{
		stubReader: stubReader{
			nibs: map[string]*nib.Nib{
				"blocker": completedBlocker,
				"blockee": blockee,
			},
		},
		links: map[string][]nib.IncomingLink{
			"blocker": {
				{FromNib: blockee, LinkType: "blocked_by"},
			},
		},
	}

	writer := &stubWriter{}
	resolver := &Resolver{
		Reader:   reader,
		Blocking: &stubBlockingChecker{},
		Orderer:  NewOrderer(reader, writer),
	}
	nibRes := &nibResolver{resolver}

	ids, err := nibRes.BlockingIds(context.Background(), completedBlocker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("got %v, want empty (resolved nib should not block anything)", ids)
	}
}

func TestBlockingFiltersResolved(t *testing.T) {
	blocker := &nib.Nib{ID: "blocker", Status: "todo"}
	activeBlockee := &nib.Nib{ID: "blockee-active", Status: "todo", BlockedBy: []string{"blocker"}}
	doneBlockee := &nib.Nib{ID: "blockee-done", Status: "scrapped", BlockedBy: []string{"blocker"}}

	reader := &stubReaderWithLinks{
		stubReader: stubReader{
			nibs: map[string]*nib.Nib{
				"blocker":        blocker,
				"blockee-active": activeBlockee,
				"blockee-done":   doneBlockee,
			},
		},
		links: map[string][]nib.IncomingLink{
			"blocker": {
				{FromNib: activeBlockee, LinkType: "blocked_by"},
				{FromNib: doneBlockee, LinkType: "blocked_by"},
			},
		},
	}

	writer := &stubWriter{}
	resolver := &Resolver{
		Reader:   reader,
		Blocking: &stubBlockingChecker{},
		Orderer:  NewOrderer(reader, writer),
	}
	nibRes := &nibResolver{resolver}

	result, err := nibRes.Blocking(context.Background(), blocker, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("got %d blocked nibs, want 1", len(result))
	}
	if result[0].ID != "blockee-active" {
		t.Errorf("got %q, want %q", result[0].ID, "blockee-active")
	}
}

func TestBlockingEmptyWhenSelfResolved(t *testing.T) {
	completedBlocker := &nib.Nib{ID: "blocker", Status: "completed"}
	blockee := &nib.Nib{ID: "blockee", Status: "todo", BlockedBy: []string{"blocker"}}

	reader := &stubReaderWithLinks{
		stubReader: stubReader{
			nibs: map[string]*nib.Nib{
				"blocker": completedBlocker,
				"blockee": blockee,
			},
		},
		links: map[string][]nib.IncomingLink{
			"blocker": {
				{FromNib: blockee, LinkType: "blocked_by"},
			},
		},
	}

	writer := &stubWriter{}
	resolver := &Resolver{
		Reader:   reader,
		Blocking: &stubBlockingChecker{},
		Orderer:  NewOrderer(reader, writer),
	}
	nibRes := &nibResolver{resolver}

	result, err := nibRes.Blocking(context.Background(), completedBlocker, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("got %v, want empty (resolved nib should not block anything)", result)
	}
}
