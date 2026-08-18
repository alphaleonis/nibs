package safetext

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
	"unicode"
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
		{"a backtick, because the code spans belong to the message", "a`b", "a b"},
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

// TestWriterKeepsNewlinesAndDropsEverythingElse pins one of the two differences
// between the writer and Strip (the backtick is the other, below). A sink carries
// whole messages, and nibs' own multi-file refusals put one file per line —
// collapsing those would destroy the report the boundary is supposed to protect.
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

// TestWriterKeepsTheBackticksAMessageWrites pins the other half of the asymmetry
// Strip's backtick rule creates. A whole MESSAGE delimits the commands it
// prescribes with backticks, so a writer that replaced them would render every
// remedy this CLI prints as undelimited prose — protecting nothing and destroying
// the one part of a refusal a reader is meant to copy. The scalars inside such a
// message have already crossed Strip at the call site.
func TestWriterKeepsTheBackticksAMessageWrites(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	in := "no store here; run `nibs init` to create one\n"
	if _, err := w.Write([]byte(in)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := buf.String(); got != in {
		t.Errorf("output = %q, want the message unchanged (%q) — a writer that strips backticks unwrites every prescribed command", got, in)
	}
	if Strip(in) == in {
		t.Error("Strip left this message unchanged, so the two rules no longer differ and this test compares nothing")
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

// TestWriterFlushEmitsAHeldTail pins that bytes Write accepted cannot vanish. A
// trailing incomplete rune is held so it can be joined with the next Write, and
// with no Flush it was simply dropped at the end of the writer's life — while
// Write had already reported the full length as accepted.
func TestWriterFlushEmitsAHeldTail(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	// "hello " plus the first two bytes of a three-byte rune.
	partial := []byte("hello \xe2\x82")
	n, err := w.Write(partial)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(partial) {
		t.Fatalf("Write returned n = %d, want %d", n, len(partial))
	}
	if got := buf.String(); got != "hello " {
		t.Fatalf("output before Flush = %q, want the held tail withheld", got)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if got := buf.String(); got != "hello  " {
		t.Errorf("output after Flush = %q, want the held bytes rendered as one space", got)
	}
	// Flush is idempotent: a second call must not emit a second space.
	if err := w.Flush(); err != nil {
		t.Fatalf("second Flush: %v", err)
	}
	if got := buf.String(); got != "hello  " {
		t.Errorf("output after a second Flush = %q, want it unchanged", got)
	}
}

// TestWriterIsSafeForConcurrentUse pins the property the wrapped sink already had.
// os.Stderr's Write is safe for concurrent use, and every caller that installed
// this Writer in its place inherited whatever guarantee it gives — an
// unsynchronized tail would take that away silently. Run under -race.
func TestWriterIsSafeForConcurrentUse(t *testing.T) {
	w := NewWriter(io.Discard)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				// Ends mid-rune, so every goroutine touches the tail.
				_, _ = w.Write([]byte("warning: h\xc3"))
				_ = w.Flush()
			}
		}()
	}
	wg.Wait()
}

// TestStripBlanksCodePointsThatRenderAsWhitespace pins the exceptions
// unicode.IsPrint cannot express: these are letters and symbols by category, so
// IsPrint accepts them, yet they render as blank — which lets a path or an id be
// padded with runes a reader cannot see.
func TestStripBlanksCodePointsThatRenderAsWhitespace(t *testing.T) {
	for _, r := range []rune{'ㅤ', '⠀', 'ᅟ', 'ᅠ', 'ﾠ'} {
		if !unicode.IsPrint(r) {
			t.Fatalf("U+%04X is not IsPrint, so this test asserts nothing about the exception list", r)
		}
		in := "a" + string(r) + "b"
		if got := Strip(in); got != "a b" {
			t.Errorf("Strip(%q) = %q, want %q — U+%04X renders blank and survived", in, got, "a b", r)
		}
	}
	// Ordinary Hangul must still come through: the exceptions are the fillers, not
	// the script.
	if got := Strip("한글"); got != "한글" {
		t.Errorf("Strip(%q) = %q, want it untouched", "한글", got)
	}
}
