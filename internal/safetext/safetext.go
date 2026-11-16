// Package safetext renders text nibs did not write, such as file contents, paths
// and the errors quoting them, so it cannot drive the terminal or the markdown it
// is shown in.
//
// Strip is for one scalar inside a message, and StripBounded for one whose length
// nothing else bounds. StripLine and StripText are for text drawn as itself,
// outside any message. Writer wraps a sink that carries only messages, and keeps
// their newlines and backticks. Do not wrap a sink that also carries styled
// (lipgloss) output; sanitize the field at the call site instead.
//
// Not covered: combining marks (printable, and unbounded when stacked), length
// outside StripBounded, homoglyphs, and markdown emphasis (`*`, `_`, `#`).
package safetext

import (
	"io"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// Strip replaces with a space every rune a scalar may not carry: anything
// unicode.IsPrint rejects (controls, bidi and zero-width formatting, separators),
// invalid UTF-8 and U+FFFD, the blank-rendering runes, newlines, and the backtick,
// which would close a code span the message put around the scalar. It neither
// collapses whitespace nor bounds length. strconv.Quote (%q) keeps backticks and
// blank-rendering runes, so it is not a substitute.
func Strip(s string) string { return replaceUnsafe(s, scalarSafe) }

// StripLine is Strip for a scalar drawn as itself rather than quoted in a
// message, such as a nib's title in a TUI row. It keeps the backtick, which only
// a message's code span needs replaced, and the zero-width joiners (U+200C and
// U+200D).
func StripLine(s string) string { return replaceUnsafe(s, lineSafe) }

// StripText is StripLine for multi-line text drawn as itself, such as a nib's
// body. It also keeps newlines and tabs.
func StripText(s string) string { return replaceUnsafe(s, textSafe) }

// replaceUnsafe replaces with a space every rune of s that safe rejects, and
// returns s itself when safe accepts them all.
func replaceUnsafe(s string, safe func(rune) bool) string {
	if !strings.ContainsFunc(s, func(r rune) bool { return !safe(r) }) {
		return s
	}
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if safe(r) {
			out = append(out, r)
			continue
		}
		out = append(out, ' ')
	}
	return string(out)
}

// MaxBoundedRunes is how many runes StripBounded keeps.
const MaxBoundedRunes = 200

// StripBounded is Strip for a scalar whose length the source does not bound,
// such as a name read from a config file: it also cuts the result to
// MaxBoundedRunes, marking a cut with "…".
func StripBounded(s string) string {
	s = Strip(s)
	if utf8.RuneCountInString(s) <= MaxBoundedRunes {
		return s
	}
	return string([]rune(s)[:MaxBoundedRunes]) + "…"
}

// scalarSafe is keep without the newline and the backtick, which only a whole
// message may carry.
func scalarSafe(r rune) bool { return keep(r) && r != '\n' && r != '`' }

func lineSafe(r rune) bool { return (keep(r) || joiner(r)) && r != '\n' }

func textSafe(r rune) bool { return keep(r) || joiner(r) || r == '\t' }

// joiner reports whether r is ZWNJ or ZWJ, which emoji sequences and scripts
// such as Persian need inside words.
func joiner(r rune) bool { return r == '\u200c' || r == '\u200d' }

// keep reports whether r may reach a Writer's sink as itself.
func keep(r rune) bool {
	if r == '\n' {
		return true
	}
	if blankRendering[r] {
		return false
	}
	return r != utf8.RuneError && unicode.IsPrint(r)
}

// blankRendering are runes unicode.IsPrint accepts that render as blank space.
var blankRendering = map[rune]bool{
	'ᅟ': true, // HANGUL CHOSEONG FILLER
	'ᅠ': true, // HANGUL JUNGSEONG FILLER
	'ㅤ': true, // HANGUL FILLER
	'ﾠ': true, // HALFWIDTH HANGUL FILLER
	'⠀': true, // BRAILLE PATTERN BLANK
}

// Writer replaces what Strip would in everything written through it, except
// newlines and backticks. Still pass each file-sourced scalar through Strip. Write
// is safe for concurrent use.
type Writer struct {
	mu sync.Mutex
	w  io.Writer
	// tail holds a UTF-8 sequence split across Write calls.
	tail []byte
}

// NewWriter returns w with the rendering boundary applied to everything written
// through it.
func NewWriter(w io.Writer) *Writer { return &Writer{w: w} }

// Write sanitizes p, writes the result, and reports len(p) on success. A trailing
// incomplete rune is held for the next Write or Flush.
func (s *Writer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	buf := p
	if len(s.tail) > 0 {
		buf = append(s.tail, p...)
		s.tail = nil
	}
	out := make([]byte, 0, len(buf))
	for len(buf) > 0 {
		r, size := utf8.DecodeRune(buf)
		if !utf8.FullRune(buf) {
			// Copy the held bytes: p belongs to the caller.
			s.tail = append([]byte(nil), buf...)
			break
		}
		if keep(r) {
			out = append(out, buf[:size]...)
		} else {
			out = append(out, ' ')
		}
		buf = buf[size:]
	}
	if _, err := s.w.Write(out); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Flush writes any incomplete rune Write is holding as a single space. Call it
// when done writing.
func (s *Writer) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.tail) == 0 {
		return nil
	}
	s.tail = nil
	_, err := s.w.Write([]byte{' '})
	return err
}
