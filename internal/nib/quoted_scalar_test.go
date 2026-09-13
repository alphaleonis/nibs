package nib

import (
	"strings"
	"testing"
)

// spanBreakingScalar carries the two runes %q passes through: a backtick, which
// closes a code span the message opens, and U+3164, which renders as blank.
const spanBreakingScalar = "q`ㅤz"

func TestFileSourcedScalarsAreStrippedInMessages(t *testing.T) {
	rows := []struct {
		name string
		err  func() error
	}{
		{"order key read from front matter", func() error {
			_, err := Parse(strings.NewReader("---\nversion: 2\ntitle: t\nstatus: todo\norder: \"" + spanBreakingScalar + "\"\n---\n"))
			return err
		}},
		{"id holding a separator", func() error { return ValidateIDForFilename(spanBreakingScalar + "/x") }},
		{"id that does not round-trip", func() error { return ValidateIDRoundTrip(spanBreakingScalar+".d-h1wy", "", "") }},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			err := row.err()
			if err == nil {
				t.Fatal("got nil, want a refusal")
			}
			msg := err.Error()
			if strings.Contains(msg, "q`") || strings.ContainsRune(msg, 'ㅤ') {
				t.Errorf("the message echoed the scalar raw:\n%s", msg)
			}
			if !strings.Contains(msg, "q  z") {
				t.Errorf("the message did not echo the scalar in its stripped form:\n%s", msg)
			}
		})
	}
}
