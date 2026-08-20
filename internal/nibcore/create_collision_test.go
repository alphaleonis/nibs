package nibcore

import (
	"errors"
	"os"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
)

// swapIDGenerator replaces the id generator Create draws from until the test
// ends, returning the given ids in order and failing the test if Create draws
// more than provided.
func swapIDGenerator(t *testing.T, ids ...string) {
	t.Helper()
	original := newNibID
	t.Cleanup(func() { newNibID = original })
	i := 0
	newNibID = func(prefix string, length int) string {
		if i >= len(ids) {
			t.Fatalf("id generator drawn %d times, only %d ids seeded", i+1, len(ids))
		}
		id := ids[i]
		i++
		return id
	}
}

// TestCreateRetriesGeneratedIDCollision pins the fix for nibs-kafe: a
// generated id that collides with a live nib is redrawn, never written over
// the existing nib. Before the guard, `nibs new` shadowed a completed nib
// ("last file loaded wins") the day the 4-char id space produced its first
// birthday collision.
func TestCreateRetriesGeneratedIDCollision(t *testing.T) {
	core, _ := setupTestCore(t)
	taken := &nib.Nib{ID: "tkn1", Slug: "taken", Title: "Original", Status: "todo"}
	if err := core.Create(taken); err != nil {
		t.Fatal(err)
	}

	swapIDGenerator(t, "tkn1", "frs1")
	b := &nib.Nib{Title: "Newcomer", Status: "todo"}
	if err := core.Create(b); err != nil {
		t.Fatalf("Create() error = %v — a colliding draw must be retried, not fatal", err)
	}
	if b.ID != "frs1" {
		t.Errorf("Create assigned id %q, want the redrawn frs1", b.ID)
	}
	got, err := core.Get("tkn1")
	if err != nil || got.Title != "Original" {
		t.Errorf("the original nib was disturbed: Get(tkn1) = %v, %v", got, err)
	}
}

// TestCreateFailsWhenTheIDSpaceStaysTaken bounds the retry: a generator that
// never yields a free id produces an error, not a hang and not a shadowing
// write.
func TestCreateFailsWhenTheIDSpaceStaysTaken(t *testing.T) {
	core, _ := setupTestCore(t)
	if err := core.Create(&nib.Nib{ID: "tkn1", Slug: "taken", Title: "Original", Status: "todo"}); err != nil {
		t.Fatal(err)
	}

	original := newNibID
	t.Cleanup(func() { newNibID = original })
	newNibID = func(prefix string, length int) string { return "tkn1" }

	err := core.Create(&nib.Nib{Title: "Newcomer", Status: "todo"})
	if err == nil {
		t.Fatal("Create() succeeded with every draw taken; want an error")
	}
	if got, gerr := core.Get("tkn1"); gerr != nil || got.Title != "Original" {
		t.Errorf("the original nib was disturbed: %v, %v", got, gerr)
	}
}

// TestCreateRefusesSuppliedDuplicateID pins the backstop for callers that
// arrive with an id of their own: an id already in the store — active or
// archived — is refused with the typed conflict error, and neither the
// original's file nor its in-memory entry is touched.
func TestCreateRefusesSuppliedDuplicateID(t *testing.T) {
	core, nibsDir := setupTestCore(t)
	if err := core.Create(&nib.Nib{ID: "tkn1", Slug: "taken", Title: "Original", Status: "todo"}); err != nil {
		t.Fatal(err)
	}

	err := core.Create(&nib.Nib{ID: "tkn1", Slug: "imposter", Title: "Imposter", Status: "todo"})
	if err == nil {
		t.Fatal("Create() with a duplicate supplied id succeeded; want a refusal")
	}
	var exists *IDExistsError
	if !errors.As(err, &exists) || exists.ID != "tkn1" {
		t.Errorf("error = %v, want *IDExistsError for tkn1", err)
	}

	if got, gerr := core.Get("tkn1"); gerr != nil || got.Title != "Original" {
		t.Errorf("the original in-memory nib was replaced: %v, %v", got, gerr)
	}
	if _, serr := os.Stat(dataPath(nibsDir, "tkn1--imposter.md")); !os.IsNotExist(serr) {
		t.Error("the refusal still wrote the imposter's file")
	}
}
