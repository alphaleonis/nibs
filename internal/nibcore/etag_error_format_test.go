package nibcore

import "testing"

// TestETagMismatchErrorFormat pins the exact wire format of ETagMismatchError.
//
// The web client classifies a failed save as a reconcilable conflict (routing it
// into the graceful "Load theirs / Overwrite" resolver instead of a bare error)
// by substring-matching the token "etag mismatch" against this message — see
// web/src/lib/nibForm.svelte.ts (isEtagConflict). There is no structured error
// code on the GraphQL error today, so this human-readable string IS the contract.
//
// If you reword this message, the web classifier silently stops recognizing the
// conflict. Keep the two in lockstep (and update
// web/src/lib/nibForm.svelte.test.ts, which pins the same string).
func TestETagMismatchErrorFormat(t *testing.T) {
	err := &ETagMismatchError{Provided: "abc123", Current: "def456"}

	const want = "etag mismatch: provided abc123, current is def456"
	if got := err.Error(); got != want {
		t.Errorf("ETagMismatchError.Error() = %q, want %q\n"+
			"the web client (isEtagConflict) depends on the leading \"etag mismatch\" token", got, want)
	}
}
