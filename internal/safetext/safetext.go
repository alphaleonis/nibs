// Package safetext is the rendering boundary for text nibs did not write.
//
// File contents, filesystem paths and the parse errors that quote them all reach
// stdout and stderr. A YAML double-quoted scalar can carry ESC (`\e`) and a
// filename on Linux is arbitrary bytes, so echoing them raw lets a file paint
// text over the terminal — the observed payload redrew the line as a fabricated
// "All checks passed" inside an error message. Unicode does the same thing without
// any control character at all: the bidi overrides and the zero-width and
// soft-hyphen formatting codes make a rendered string differ from the string that
// is actually there, which is the whole point of the deception.
//
// The package is stdlib-only so every layer can reach it — internal/nibcore warns
// about unparseable files on every command, and the CLI's error boundary prints
// refusals that quote them.
//
// Two shapes, for two situations:
//
//   - Strip, for one scalar interpolated into a message. Every non-printable rune
//     becomes a space, newlines included: a scalar spans one line.
//   - Writer, for a SINK that carries only such messages. Newlines survive
//     (nibs' own refusals are multi-line and their layout is information) and
//     everything else non-printable becomes a space. A sink cannot be bypassed by
//     forgetting to call a helper, which is why the two highest-traffic
//     file-sourced echoes are wrapped rather than sanitized per call site.
//
// A sink that also carries STYLED output cannot be wrapped: lipgloss emits real
// escape sequences for color, and stripping them would erase the styling. Those
// surfaces neutralize the file-sourced field at the call site instead.
package safetext

import (
	"io"
	"unicode"
	"unicode/utf8"
)

// Strip replaces every rune that is not printable with a single space, so a
// scalar read from a file renders as itself and nothing else.
//
// "Printable" is unicode.IsPrint — the same rule strconv.Quote applies, which is
// why the %q sites in this codebase are already safe. It covers the C0 and C1
// control characters, DEL, the bidi overrides and isolates (U+202A–U+202E,
// U+2066–U+2069), the zero-width joiners and spaces (U+200B–U+200D, U+2060,
// U+FEFF), the soft hyphen (U+00AD) and the line/paragraph separators.
//
// utf8.RuneError is replaced too, which conflates an invalid byte sequence with a
// legitimate U+FFFD in a filename: both render as a space. That cost is accepted
// because an invalid sequence is the more likely of the two here and a filename
// that renders one space short is still recognizable.
//
// Whitespace collapsing and length bounding are deliberately NOT done here — a
// caller quoting a value wants both, and a caller printing a path wants neither.
func Strip(s string) string {
	// Fast path: most text needs no work, and this runs on every command.
	if !needsStripping(s) {
		return s
	}
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if keep(r) && r != '\n' {
			out = append(out, r)
			continue
		}
		out = append(out, ' ')
	}
	return string(out)
}

func needsStripping(s string) bool {
	for _, r := range s {
		if !keep(r) || r == '\n' {
			return true
		}
	}
	return false
}

// keep reports whether r may reach a terminal as itself. A newline is kept here
// and dropped by Strip, because only a Writer wraps a whole multi-line message.
func keep(r rune) bool {
	if r == '\n' {
		return true
	}
	return r != utf8.RuneError && unicode.IsPrint(r)
}

// Writer wraps a sink so no file-sourced text can reach it unfiltered by
// omission. Every rune Strip would replace is replaced, except the newline, which
// survives: nibs' own refusals enumerate files one per line and collapsing them
// would destroy the report rather than protect it.
//
// It is NOT safe to wrap a writer that carries styled output; see the package
// comment.
type Writer struct {
	w io.Writer
	// tail holds an incomplete UTF-8 sequence split across two Write calls, so a
	// multi-byte rune is never misread as invalid bytes at a buffer boundary.
	tail []byte
}

// NewWriter returns w with the rendering boundary applied to everything written
// through it.
func NewWriter(w io.Writer) *Writer { return &Writer{w: w} }

// Write neutralizes p and writes the result. It reports len(p) on success even
// though the byte count written may differ: callers count what they handed over,
// and io.Writer requires n < len(p) to mean an error occurred.
func (s *Writer) Write(p []byte) (int, error) {
	buf := p
	if len(s.tail) > 0 {
		buf = append(s.tail, p...)
		s.tail = nil
	}
	out := make([]byte, 0, len(buf))
	for len(buf) > 0 {
		r, size := utf8.DecodeRune(buf)
		if !utf8.FullRune(buf) {
			// A rune split across writes: hold the bytes rather than rendering
			// them as invalid. Copied, because p belongs to the caller.
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
