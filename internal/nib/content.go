package nib

import (
	"fmt"
	"strings"
)

// BodyReplacement describes a single find-and-replace operation on body text.
type BodyReplacement struct {
	Old string
	New string
}

// ReplaceMatchError is returned by ReplaceOnce when the old text does not occur
// exactly once in the body. Count is the number of occurrences found: 0 means
// "not found", any value >1 means "ambiguous". Callers inspect Count via
// errors.As to distinguish the two cases and to report the occurrence count
// structurally (e.g. the CLI's TEXT_NOT_FOUND / TEXT_AMBIGUOUS envelopes).
type ReplaceMatchError struct {
	Count int
}

func (e *ReplaceMatchError) Error() string {
	if e.Count == 0 {
		return "text not found in body"
	}
	return fmt.Sprintf("text found %d times in body (must be unique)", e.Count)
}

// ApplyBodyMod applies a sequence of replacements and an optional append to a body string.
// Returns the modified body. If any replacement fails, returns the original body unchanged.
func ApplyBodyMod(body string, replacements []BodyReplacement, appendText string) (string, error) {
	working := body
	for i, r := range replacements {
		newBody, err := ReplaceOnce(working, r.Old, r.New)
		if err != nil {
			return body, fmt.Errorf("replacement %d failed: %w", i, err)
		}
		working = newBody
	}
	if appendText != "" {
		working = AppendWithSeparator(working, appendText)
	}
	return working, nil
}

// ReplaceOnce replaces exactly one occurrence of old with new in text.
// Returns an error if old is empty, not found, or found multiple times.
// The new string can be empty to delete the matched text.
func ReplaceOnce(text, old, new string) (string, error) {
	if old == "" {
		return "", fmt.Errorf("old text cannot be empty")
	}
	// Exactly-once semantics: 0 occurrences (not found) and >1 occurrences
	// (ambiguous) are both surfaced as a typed *ReplaceMatchError carrying the
	// count, so callers can branch structurally instead of parsing the message.
	if count := strings.Count(text, old); count != 1 {
		return "", &ReplaceMatchError{Count: count}
	}
	return strings.Replace(text, old, new, 1), nil
}

// AppendWithSeparator appends addition to text with a blank line separator.
// If text is empty, returns addition without separator.
// If addition is empty, returns text unchanged (no-op).
func AppendWithSeparator(text, addition string) string {
	if addition == "" {
		return text
	}
	if text == "" {
		return addition
	}
	// Ensure single newline separator
	text = strings.TrimRight(text, "\n")
	return text + "\n\n" + addition
}
