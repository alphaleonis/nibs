package safetext

import (
	"bytes"
	"strings"
	"testing"
)

func TestStripReplacesEveryNonPrintableRune(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"an ESC sequence", "a\x1b[2Kb", "a [2Kb"},
		{"NUL, DEL and a C1 control", "a\x00b\x7fc\u009bd", "a b c d"},
		{"a bidi override", "a\u202eb", "a b"},
		{"a zero-width space", "a\u200bb", "a b"},
		{"a BOM", "a\ufeffb", "a b"},
		{"a soft hyphen", "a\u00adb", "a b"},
		{"a newline, because a scalar is one line", "a\nb", "a b"},
		{"a tab", "a\tb", "a b"},
		{"an invalid UTF-8 byte", "a\xffb", "a b"},
		{"ordinary text is untouched", "data/tnib-0001--höger.md", "data/tnib-0001--höger.md"},
		{"non-Latin text is untouched", "エピック", "エピック"},
		{"an emoji is untouched", "done \U0001f389", "done \U0001f389"},
		{"an empty string", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Strip(tt.in); got != tt.want {
				t.Errorf("Strip(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestWriterKeepsNewlinesAndDropsEverythingElse pins the one difference between
// the writer and Strip. A sink carries whole messages, and nibs' own multi-file
// refusals put one file per line — collapsing those would destroy the report the
// boundary is supposed to protect.
func TestWriterKeepsNewlinesAndDropsEverythingElse(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	in := "refusing to migrate around 2 file(s):\n  data/a\x1b[2K.md: bad\n  data/b\u202e.md: bad\n"
	n, err := w.Write([]byte(in))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(in) {
		t.Errorf("Write returned n = %d, want %d — a short count means an error to the caller", n, len(in))
	}
	got := buf.String()
	if strings.Count(got, "\n") != 3 {
		t.Errorf("output = %q, want the three newlines preserved", got)
	}
	for _, bad := range []string{"\x1b", "\u202e"} {
		if strings.Contains(got, bad) {
			t.Errorf("output = %q still carries %q", got, bad)
		}
	}
}

// TestWriterCarriesARuneSplitAcrossWrites pins that a multi-byte rune arriving in
// two Write calls is not mistaken for invalid bytes and blanked out.
func TestWriterCarriesARuneSplitAcrossWrites(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	full := []byte("höger")
	// Split inside the two-byte ö.
	if _, err := w.Write(full[:2]); err != nil {
		t.Fatalf("Write first half: %v", err)
	}
	if _, err := w.Write(full[2:]); err != nil {
		t.Fatalf("Write second half: %v", err)
	}
	if got := buf.String(); got != "höger" {
		t.Errorf("output = %q, want %q", got, "höger")
	}
}
