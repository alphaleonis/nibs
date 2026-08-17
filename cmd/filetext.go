package cmd

import (
	"strings"
	"unicode/utf8"

	"github.com/alphaleonis/nibs/internal/safetext"
)

// maxEchoedFileTextRunes bounds how much of a file-sourced scalar a message
// repeats. A message's job is to name the file and the key so the reader can
// look; repeating an arbitrarily long value adds nothing and gives a hostile
// file a canvas.
const maxEchoedFileTextRunes = 200

// stripControlChars renders one scalar read from a file so it cannot paint text
// over the terminal — see internal/safetext for the rule and why it is the whole
// non-printable category rather than a control-character list.
//
// This CLI's primary consumer is a coding agent, which makes the transcript the
// aggravating sink rather than the terminal: `nibs list` in a freshly cloned,
// store-less repository is exactly the low-suspicion command an onboarding agent
// runs, and an ancestor `.nibs.yml` reached by the upward walk lands unfiltered
// in that agent's context.
//
// SCOPE, so nobody reads more into this than it does. Two sinks apply the boundary
// structurally and cannot be bypassed by forgetting to call this: nibcore's warn
// writer and the CLI's error boundary (reportExitError). STDOUT is not one of
// them — it carries lipgloss-styled output, and stripping escapes there would
// erase the styling — so every file-sourced field printed through ui.Printf must
// pass through this function or flattenReason at the call site.
// TestFileSourcedTextNeverReachesAnEchoSurfaceRaw enumerates those surfaces.
//
// Whitespace collapsing is left to the caller — flattenReason already does it
// with strings.Fields, and a path is better left as-is.
func stripControlChars(s string) string { return safetext.Strip(s) }

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
