package cmd

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
)

// TestETagErrorPresenter_TagsOnlyTypedEtagError pins the structured error-code
// contract the web client relies on: the gqlgen error presenter attaches a
// stable extensions.code = "ETAG_MISMATCH" to ONLY the typed
// *nibcore.ETagMismatchError, and leaves every other error (validation, generic,
// wrapped-but-unrelated) untouched. The web classifier (isEtagConflict) keys
// conflict routing on that code first, with the human-readable "etag mismatch"
// string retained purely as a fallback (see web/src/lib/nibForm.svelte.ts).
func TestETagErrorPresenter_TagsOnlyTypedEtagError(t *testing.T) {
	ctx := context.Background()

	t.Run("tags the typed ETagMismatchError", func(t *testing.T) {
		err := &nibcore.ETagMismatchError{Provided: "abc123", Current: "def456"}

		gqlErr := etagErrorPresenter(ctx, err)

		if gqlErr.Extensions["code"] != "ETAG_MISMATCH" {
			t.Errorf("extensions.code = %v, want %q", gqlErr.Extensions["code"], "ETAG_MISMATCH")
		}
		// The human-readable message must be preserved verbatim (the substring
		// fallback still keys off it).
		if gqlErr.Message != err.Error() {
			t.Errorf("message = %q, want %q", gqlErr.Message, err.Error())
		}
	})

	t.Run("tags a wrapped ETagMismatchError (errors.As chain)", func(t *testing.T) {
		wrapped := fmt.Errorf("update failed: %w", &nibcore.ETagMismatchError{Provided: "a", Current: "b"})

		gqlErr := etagErrorPresenter(ctx, wrapped)

		if gqlErr.Extensions["code"] != "ETAG_MISMATCH" {
			t.Errorf("extensions.code = %v, want %q (wrapped typed error must still match errors.As)",
				gqlErr.Extensions["code"], "ETAG_MISMATCH")
		}
	})

	t.Run("does NOT tag the enum-validation error", func(t *testing.T) {
		// Mirrors the plain fmt.Errorf validation errors from nibcore.validateEnums.
		err := fmt.Errorf("invalid status %q: must be one of draft, todo", "bogus")

		gqlErr := etagErrorPresenter(ctx, err)

		if _, ok := gqlErr.Extensions["code"]; ok {
			t.Errorf("validation error was tagged with code %v; only the typed etag error may be tagged",
				gqlErr.Extensions["code"])
		}
	})

	t.Run("does NOT tag a generic error", func(t *testing.T) {
		gqlErr := etagErrorPresenter(ctx, errors.New("disk full"))

		if _, ok := gqlErr.Extensions["code"]; ok {
			t.Errorf("generic error was tagged with code %v; only the typed etag error may be tagged",
				gqlErr.Extensions["code"])
		}
	})

	t.Run("does NOT tag the ETagRequiredError (distinct typed error)", func(t *testing.T) {
		gqlErr := etagErrorPresenter(ctx, &nibcore.ETagRequiredError{})

		if _, ok := gqlErr.Extensions["code"]; ok {
			t.Errorf("ETagRequiredError was tagged with code %v; only ETagMismatchError is a reconcilable conflict",
				gqlErr.Extensions["code"])
		}
	})
}

// TestETagErrorPresenter_TagsNotFound pins the second half of the structured
// error-code contract: a mutation error carrying nib.ErrNotFound is tagged with a
// stable extensions.code = "NOT_FOUND". The web client keys on this to route a
// failed save against a DELETED nib into its gone/deleted notice (see
// web/src/lib/nibForm.svelte.ts, isNotFound) instead of showing the raw
// "target nib not found" toast. A delete is NOT an etag conflict —
// GetForUpdate returns ErrNotFound before any if-match check — so this code is
// distinct from ETAG_MISMATCH. The tag keys on the error type, not on WHICH nib
// is gone: both the bare ErrNotFound (the edited nib deleted) and the wrapped
// "target nib not found" (a deleted blocking/parent TARGET) are tagged. On the
// web save path only the former is reachable, because the edit-save input sends no
// parent/blocking fields (see etagErrorPresenter's docstring in serve.go).
func TestETagErrorPresenter_TagsNotFound(t *testing.T) {
	ctx := context.Background()

	t.Run("tags the bare nib.ErrNotFound", func(t *testing.T) {
		gqlErr := etagErrorPresenter(ctx, nib.ErrNotFound)

		if gqlErr.Extensions["code"] != "NOT_FOUND" {
			t.Errorf("extensions.code = %v, want %q", gqlErr.Extensions["code"], "NOT_FOUND")
		}
	})

	t.Run("tags a wrapped ErrNotFound (errors.Is chain)", func(t *testing.T) {
		// Mirrors the resolver wrap: fmt.Errorf("target nib not found: %s: %w", id, err).
		wrapped := fmt.Errorf("target nib not found: %s: %w", "n1", nib.ErrNotFound)

		gqlErr := etagErrorPresenter(ctx, wrapped)

		if gqlErr.Extensions["code"] != "NOT_FOUND" {
			t.Errorf("extensions.code = %v, want %q (wrapped ErrNotFound must still match errors.Is)",
				gqlErr.Extensions["code"], "NOT_FOUND")
		}
		// The human-readable message is preserved verbatim.
		if gqlErr.Message != wrapped.Error() {
			t.Errorf("message = %q, want %q", gqlErr.Message, wrapped.Error())
		}
	})

	t.Run("does NOT tag a generic 'not found'-worded error", func(t *testing.T) {
		// Only the typed ErrNotFound (via errors.Is) may be tagged — a coincidental
		// wording must never be mistaken for a real deletion, which routes the whole
		// view to gone/deleted.
		gqlErr := etagErrorPresenter(ctx, errors.New("parent nib not found: p1"))

		if _, ok := gqlErr.Extensions["code"]; ok {
			t.Errorf("generic 'not found' error was tagged with code %v; only errors.Is(err, nib.ErrNotFound) may be tagged",
				gqlErr.Extensions["code"])
		}
	})

	// The three filter-target classes reach the presenter over the READ path, not
	// the mutation path the cases above cover: the web filter box sends
	// user-typed ids straight into NibFilter, so a half-typed id refuses the
	// whole query. NOT_FOUND is what lets the list treat that as an explainable
	// empty state instead of flashing a failure while the user is still typing
	// (see web/src/lib/components/TreeTable.svelte).
	t.Run("tags an unknown filter target", func(t *testing.T) {
		err := &graph.FilterTargetNotFoundError{Field: "parentId", ID: "zz"}

		gqlErr := etagErrorPresenter(ctx, err)

		if gqlErr.Extensions["code"] != "NOT_FOUND" {
			t.Errorf("extensions.code = %v, want %q", gqlErr.Extensions["code"], "NOT_FOUND")
		}
		if gqlErr.Message != err.Error() {
			t.Errorf("message = %q, want %q", gqlErr.Message, err.Error())
		}
	})

	t.Run("does NOT tag a filter target that vanished mid-filter", func(t *testing.T) {
		// A concurrent delete is not the caller's typo. Tagging it NOT_FOUND
		// would tell the web to explain a wrong id that was never wrong, so it
		// stays uncoded and lands in the generic error path with every other
		// internal failure.
		err := &graph.FilterTargetUnreadableError{Field: "siblingId", ID: "nibs-a", ReaderErr: nib.ErrNotFound}

		gqlErr := etagErrorPresenter(ctx, err)

		if code, ok := gqlErr.Extensions["code"]; ok {
			t.Errorf("a mid-filter vanish was tagged with code %v; it must not classify as the caller's not-found", code)
		}
	})

	t.Run("does NOT tag a filter field given an empty id", func(t *testing.T) {
		// The other read-path filter refusal, and the one that must NOT reach
		// the web as NOT_FOUND. TreeTable.svelte routes any read-path NOT_FOUND
		// to a calm inline empty state, which is right for a half-typed id and
		// wrong for a malformed query: an empty id is what a client sends when
		// a variable did not interpolate, so explaining it away as "nothing
		// matched" would hide the client bug behind the same confident empty
		// answer the refusal exists to replace. Untagged, it lands in the
		// generic error path and shows as the failure it is.
		err := &graph.FilterTargetEmptyError{Field: "parentId"}

		gqlErr := etagErrorPresenter(ctx, err)

		if code, ok := gqlErr.Extensions["code"]; ok {
			t.Errorf("an empty filter id was tagged with code %v; it must not be presented as a not-found or any other routable class", code)
		}
		if gqlErr.Message != err.Error() {
			t.Errorf("message = %q, want %q", gqlErr.Message, err.Error())
		}
	})

	t.Run("tags a contradictory filter pair with its own code, not NOT_FOUND", func(t *testing.T) {
		// The third read-path refusal, and the one that needs a code of its own.
		// NOT_FOUND is wrong: it routes to the "no such nib" empty state, whose
		// wording blames an id when both ids may be perfectly good. Uncoded is
		// wrong too: the web list then renders the backend's raw sentence in a
		// destructive box for a query the user wrote and can edit — and for the
		// parent pair that replaces an inline empty state whose button cleared it
		// in one click. A distinct code is what lets TreeTable.svelte explain the
		// pair in the query box's own vocabulary and keep that button.
		err := &graph.FilterTargetContradictionError{Field: "parentId", PresenceField: "hasParent", ID: "nibs-a"}

		gqlErr := etagErrorPresenter(ctx, err)

		if gqlErr.Extensions["code"] != "FILTER_CONTRADICTION" {
			t.Errorf("extensions.code = %v, want %q", gqlErr.Extensions["code"], "FILTER_CONTRADICTION")
		}
		if gqlErr.Message != err.Error() {
			t.Errorf("message = %q, want %q", gqlErr.Message, err.Error())
		}
	})

	t.Run("does NOT tag the typed ETagMismatchError as NOT_FOUND (stays ETAG_MISMATCH)", func(t *testing.T) {
		// Symmetric to TagsOnlyTypedEtagError's negative case: the two typed errors
		// are mutually exclusive. A reconcilable etag conflict must route into the
		// inline resolver, never the (stronger, less-recoverable) gone/deleted path.
		err := &nibcore.ETagMismatchError{Provided: "abc123", Current: "def456"}

		gqlErr := etagErrorPresenter(ctx, err)

		if gqlErr.Extensions["code"] != "ETAG_MISMATCH" {
			t.Errorf("extensions.code = %v, want %q; an etag conflict must not be tagged NOT_FOUND",
				gqlErr.Extensions["code"], "ETAG_MISMATCH")
		}
	})
}
