// Package safetext renders text nibs did not write, such as file contents, paths
// and the errors quoting them, so it cannot drive the terminal or the markdown it
// is shown in.
//
// Strip is for one scalar inside a message. Writer wraps a sink that carries only
// messages, and keeps their newlines and backticks. Do not wrap a sink that also
// carries styled (lipgloss) output; sanitize the field at the call site instead.
//
// Not covered: combining marks (printable, and unbounded when stacked), length,
// homoglyphs, and markdown emphasis (`*`, `_`, `#`).
package safetext

import (
	"io"
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
func Strip(s string) string {
	if !needsStripping(s) {
		return s
	}
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if scalarSafe(r) {
			out = append(out, r)
			continue
		}
		out = append(out, ' ')
	}
	return string(out)
}

func needsStripping(s string) bool {
	for _, r := range s {
		if !scalarSafe(r) {
			return true
		}
	}
	return false
}

// scalarSafe is keep without the newline and the backtick, which only a whole
// message may carry.
func scalarSafe(r rune) bool { return keep(r) && r != '\n' && r != '`' }

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
