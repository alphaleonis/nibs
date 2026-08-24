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

// TestETagStampLessFileDoesNotFalseConflict is the POSITIVE direction, and it
// belongs in this package rather than only behind the CLI.
//
// Its siblings above are bounding tests — they pin what the reconciliation must
// NOT do. Without this one the whole stamp fill can be deleted and
// ./internal/nibcore/ stays green, with only ./cmd/ going red: a maintainer
// refactoring reconcileLoaderDerived and running the package targeted at it, the
// focused workflow this project prescribes, would get a false green on the very
// regression the reconciliation exists to prevent.
//
// Each shape below is one route through loadNib's fallback: no stamps at all
// (both derived from the file's mtime), created_at alone (updated_at defaulted
// from it), and updated_at alone (created_at taken from it).
func TestETagStampLessFileDoesNotFalseConflict(t *testing.T) {
	const file = "stamp2--partial.md"

	for _, tt := range []struct {
		name   string
		stamps string
	}{
		{"neither stamp", ""},
		{"created_at only", "created_at: 2026-01-02T03:04:05Z\n"},
		{"updated_at only", "updated_at: 2026-01-02T03:04:05Z\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			nibsDir := setupNibsDir(t)
			writeNibFile(t, storeData(t, nibsDir), file, stampedNibFile("Partial", tt.stamps))
			core := setupLoadedCore(t, nibsDir)

			b, err := core.Get("stamp2")
			if err != nil {
				t.Fatalf("Get() error: %v", err)
			}
			ifMatch := b.ETag()
			stored, err := core.CurrentETag("stamp2")
			if err != nil {
				t.Fatalf("CurrentETag() error: %v", err)
			}
			// The two renders of an UNMODIFIED file must agree. They are what the
			// caller supplies and what the store compares it against, so a
			// divergence here is a conflict no retry can clear.
			if ifMatch != stored {
				t.Fatalf("the in-memory etag (%s) and the stored etag (%s) disagree on an unmodified file; "+
					"an if-match write on it can never succeed", ifMatch, stored)
			}

			// And the token the caller holds is actually accepted.
			clone := b.Clone()
			clone.Title = "Renamed"
			if err := core.Update(clone, &ifMatch); err != nil {
				t.Errorf("Update with the etag Get handed back: %v", err)
			}
		})
	}
}
