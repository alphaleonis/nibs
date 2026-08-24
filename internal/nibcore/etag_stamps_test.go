package nibcore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// stampedNibFile renders a hand-authored nib file whose timestamp block is
// supplied verbatim, so one fixture covers a file carrying both stamps, either
// one alone, or neither.
func stampedNibFile(title, stamps string) string {
	return "---\nversion: 2\ntitle: " + title + "\nstatus: todo\ntype: task\n" + stamps + "---\n\nBody.\n"
}

// TestETagStampedFileComparesExactly bounds the stamp reconciliation to the case
// it exists for: a stamp the FILE DOES carry is content like any other field, and
// an external edit to it is divergence the store must refuse to overwrite. The
// reconciliation may fill a stamp in, never overwrite one.
//
// Both stamp pairings are covered, because they take different routes through
// the reconciliation. A nib whose stamps DIFFER is turned away by
// loaderMaySynthesizeStamps before the fill is reached at all; only one whose
// stamps COINCIDE gets that far, so it is the only fixture that can witness the
// fill declining to overwrite a value the file already carries.
func TestETagStampedFileComparesExactly(t *testing.T) {
	const file = "stamp1--both.md"
	const differ = "created_at: 2019-01-01T00:00:00Z\nupdated_at: 2026-01-02T03:04:05Z\n"
	const coincide = "created_at: 2026-01-02T03:04:05Z\nupdated_at: 2026-01-02T03:04:05Z\n"

	tests := []struct {
		name   string
		loaded string
		edited string
	}{
		{"created_at edited, stamps differ", differ,
			"created_at: 2020-06-06T00:00:00Z\nupdated_at: 2026-01-02T03:04:05Z\n"},
		{"updated_at edited, stamps differ", differ,
			"created_at: 2019-01-01T00:00:00Z\nupdated_at: 2026-02-02T00:00:00Z\n"},
		{"created_at edited, stamps coincide", coincide,
			"created_at: 2020-06-06T00:00:00Z\nupdated_at: 2026-01-02T03:04:05Z\n"},
		{"updated_at edited, stamps coincide", coincide,
			"created_at: 2026-01-02T03:04:05Z\nupdated_at: 2026-02-02T00:00:00Z\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsDir := setupNibsDir(t)
			data := storeData(t, nibsDir)
			writeNibFile(t, data, file, stampedNibFile("Both", tt.loaded))
			core := setupLoadedCore(t, nibsDir)

			b, err := core.Get("stamp1")
			if err != nil {
				t.Fatalf("Get() error: %v", err)
			}
			ifMatch := b.ETag()
			stored, err := core.CurrentETag("stamp1")
			if err != nil {
				t.Fatalf("CurrentETag() error: %v", err)
			}
			if stored != ifMatch {
				t.Fatalf("an unedited fully-stamped file must compare equal: CurrentETag=%s, in-memory ETag=%s", stored, ifMatch)
			}

			writeNibFile(t, data, file, stampedNibFile("Both", tt.edited))

			if stored, err = core.CurrentETag("stamp1"); err != nil {
				t.Fatalf("CurrentETag() after the external edit: %v", err)
			}
			if stored == ifMatch {
				t.Fatalf("an externally edited stamp is content divergence, but CurrentETag still reports %s", stored)
			}

			updated := b.Clone()
			updated.Title = "Both (edited)"
			var mismatch *ETagMismatchError
			if err := core.Update(updated, &ifMatch); !errors.As(err, &mismatch) {
				t.Fatalf("an if-match Update over an externally edited stamp must conflict, got: %v", err)
			}
		})
	}
}

// TestETagExternalEditAfterLoadStillConflicts is the guard the reconciliation
// must not weaken. It closes a false conflict, so the adversarial question is
// whether it also swallowed the TRUE one: an edit that lands on the file after
// this process loaded it must still be refused, on exactly the stores the fix is
// for.
//
// The second case is the one the reconciliation has to reason about rather than
// wave through. A stamp DELETED from the file renders identically to a stamp the
// loader synthesized — from the bare parse the two are the same absence. What
// separates them is the store's own pair: every branch of loadNib's fallback
// assigns one stamp FROM the other (or both from the file's mtime), so a nib
// holding two DIFFERENT stamps synthesized nothing and its file has to match
// exactly. Accepting it instead would let Core.Update write the stale clone back
// — it re-stamps updated_at but never assigns created_at — silently restoring the
// deleted key and losing the external edit with no conflict raised.
func TestETagExternalEditAfterLoadStillConflicts(t *testing.T) {
	const file = "ext1--target.md"

	tests := []struct {
		name     string
		loaded   string
		external string
	}{
		{
			name:     "body edited on a stamp-less file",
			loaded:   stampedNibFile("Target", ""),
			external: "---\nversion: 2\ntitle: Target\nstatus: todo\ntype: task\n---\n\nEdited elsewhere.\n",
		},
		{
			name:     "created_at deleted from a nib whose stamps differ",
			loaded:   stampedNibFile("Target", "created_at: 2019-01-01T00:00:00Z\nupdated_at: 2026-01-02T03:04:05Z\n"),
			external: stampedNibFile("Target", "updated_at: 2026-01-02T03:04:05Z\n"),
		},
		{
			name:     "updated_at deleted from a nib whose stamps differ",
			loaded:   stampedNibFile("Target", "created_at: 2019-01-01T00:00:00Z\nupdated_at: 2026-01-02T03:04:05Z\n"),
			external: stampedNibFile("Target", "created_at: 2019-01-01T00:00:00Z\n"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsDir := setupNibsDir(t)
			data := storeData(t, nibsDir)
			writeNibFile(t, data, file, tt.loaded)
			core := setupLoadedCore(t, nibsDir)

			b, err := core.Get("ext1")
			if err != nil {
				t.Fatalf("Get() error: %v", err)
			}
			ifMatch := b.ETag()

			writeNibFile(t, data, file, tt.external)

			stored, err := core.CurrentETag("ext1")
			if err != nil {
				t.Fatalf("CurrentETag() after the external edit: %v", err)
			}
			if stored == ifMatch {
				t.Errorf("the external edit is invisible: CurrentETag still reports %s", stored)
			}

			updated := b.Clone()
			updated.Status = "in-progress"
			var mismatch *ETagMismatchError
			if err := core.Update(updated, &ifMatch); !errors.As(err, &mismatch) {
				t.Fatalf("an if-match Update over an external edit must conflict, got: %v", err)
			}
			after, err := os.ReadFile(filepath.Join(data, file))
			if err != nil {
				t.Fatalf("ReadFile(%s) error: %v", file, err)
			}
			if string(after) != tt.external {
				t.Errorf("the refused Update wrote the file anyway:\n%s", after)
			}
		})
	}
}
