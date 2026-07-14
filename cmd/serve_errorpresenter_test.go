package cmd

import (
	"context"
	"errors"
	"fmt"
	"testing"

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
