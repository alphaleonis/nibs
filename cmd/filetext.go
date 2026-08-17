package cmd

import (
	"strings"
	"unicode/utf8"
)

// maxEchoedFileTextRunes bounds how much of a file-sourced scalar a message
// repeats. A message's job is to name the file and the key so the reader can
// look; repeating an arbitrarily long value adds nothing and gives a hostile
// file a canvas.
const maxEchoedFileTextRunes = 200

// stripControlChars replaces every C0 and C1 control character in s with a
// single space. It is the rendering boundary for text nibs did not write: file
// contents, filesystem paths and the parse errors that quote them all reach
// stdout and stderr, and a YAML double-quoted scalar can carry ESC (`\e`), so
// echoing the raw bytes lets a file paint arbitrary text over the terminal.
// The observed payload redrew the line as a fabricated "All checks passed"
// inside an error message.
//
// This CLI's primary consumer is a coding agent, which makes the transcript the
// aggravating sink rather than the terminal: `nibs list` in a freshly cloned,
// store-less repository is exactly the low-suspicion command an onboarding agent
// runs, and an ancestor `.nibs.yml` reached by the upward walk lands unfiltered
// in that agent's context.
//
// Whitespace collapsing is left to the caller — flattenReason already does it
// with strings.Fields, and a path is better left as-is.
func stripControlChars(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == utf8.RuneError, r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f:
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// sanitizeFileText renders one scalar read from a file for a terminal message:
// control characters neutralized (see stripControlChars), whitespace collapsed
// onto one line, and the result truncated. Use it for a VALUE quoted back at the
// user; use stripControlChars alone where the text is a path or a multi-line
// report whose length carries information.
//
// It must NOT reach a path that appears as an argument in a remedy the user is
// meant to copy and run: collapsing whitespace and appending "…" past 200 runes
// corrupts exactly the string that has to survive intact. Those go through
// shellArg (stripControlChars plus quoting) instead.
func sanitizeFileText(s string) string {
	flat := strings.Join(strings.Fields(stripControlChars(s)), " ")
	if utf8.RuneCountInString(flat) <= maxEchoedFileTextRunes {
		return flat
	}
	runes := []rune(flat)
	return string(runes[:maxEchoedFileTextRunes]) + "…"
}
