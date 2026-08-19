package cmd

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/alphaleonis/nibs/internal/safetext"
)

// maxEchoedFileTextRunes bounds how much of a file-sourced scalar a message
// repeats. A message's job is to name the file and the key so the reader can
// look; repeating an arbitrarily long value adds nothing and gives a hostile
// file a canvas.
const maxEchoedFileTextRunes = 200

// maxEchoedListEntries bounds how many entries a message ENUMERATES, the count
// half of the same boundary maxEchoedFileTextRunes governs one scalar of. A
// refusal that names one file per offender is O(store): 300 files carrying a
// format version this build cannot read made every command print 301 lines and
// 13 KB, on every command, until the store was repaired.
//
// Twenty is enough to recognize the shape of the problem — one directory, one
// bad merge, the whole store — which is what the reader acts on; the full list
// belongs to the command the elision names (see echoedListRemedyCheck), not to a
// message printed by a pre-run gate nobody asked to run.
const maxEchoedListEntries = 20

// echoedListRemedyCheck is the elision tail's remedy for a list `nibs check`
// reports in full. Pass it only where check really does report the SAME files:
// the tail is a prescription, and one pointing at a command that does not carry
// the list would send the reader in a circle.
const echoedListRemedyCheck = "run `nibs check` for the full list"

// echoedList joins entries onto the indented lines a refusal enumerates them on,
// bounded to maxEchoedListEntries with the elided count stated. An empty remedy
// states the count alone, for a list nothing else reports.
//
// The bound belongs HERE rather than where each list is collected: the lists feed
// `nibs check` too, which is the unbounded channel this tail points at, so
// truncating at the source would leave the full list nowhere (see
// newerStoreError.full).
func echoedList(entries []string, remedy string) string {
	if len(entries) <= maxEchoedListEntries {
		return strings.Join(entries, "\n  ")
	}
	// Copy rather than append onto the caller's slice: entries is a live field on
	// the error these messages are rendered from, and appending in place would
	// overwrite the 21st entry it still has to be able to render in full.
	shown := make([]string, maxEchoedListEntries, maxEchoedListEntries+1)
	copy(shown, entries)
	tail := fmt.Sprintf("…and %d more", len(entries)-maxEchoedListEntries)
	if remedy != "" {
		tail += "; " + remedy
	}
	return strings.Join(append(shown, tail), "\n  ")
}

// stripControlChars renders one scalar read from a file so it cannot paint text
// over the terminal, nor close a markdown code span the message put around it —
// see internal/safetext for both rules, why the first is the whole non-printable
// category rather than a control-character list, and why the backtick is the one
// printable rune the second answers.
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
//
// That per-call-site half is NOT enforced. TestFileSourcedTextNeverReachesAnEchoSurfaceRaw
// pins the sites it lists, and nothing detects a new ui.Printf of a file-sourced
// value — a `go/analysis` rule over a tainted string type returned by nib parsing
// would, and does not exist. Treat the test as a regression guard over known sites,
// not as proof of coverage: two omissions (the link diagnostics in cmd/check.go and
// `config set-prefix --dry-run`'s filenames) survived under a comment claiming the
// list was complete. `nibs list` / `nibs show` render nib TITLES and bodies through
// lipgloss on stdout and are deliberately outside this boundary — that is the
// app's own output, not a diagnostic echo.
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
// It must NOT reach a PATH: collapsing whitespace renames a directory whose name
// contains a space. A path echoed as prose goes through sanitizeFilePath, which
// bounds it without collapsing it, and a path that appears as an argument in a
// remedy the user is meant to copy and run goes through shellArg — where appending
// "…" past 200 runes would corrupt exactly the string that has to survive intact.
func sanitizeFileText(s string) string {
	return truncateEchoedText(strings.Join(strings.Fields(stripControlChars(s)), " "))
}

// sanitizeFilePath renders a path DERIVED from a file-declared value: control
// characters neutralized (see stripControlChars) and the result truncated, but
// whitespace left alone, because a real path may contain spaces and collapsing
// them would name a directory that is not there.
//
// The bound is what separates it from stripControlChars. A `nibs.path` value is
// read from a config file that may be up to config.MaxConfigBytes, and the
// resolved path a refusal echoes is that value joined onto the project directory —
// so the message repeated a megabyte of attacker-chosen text per interpolation,
// with only the QUOTED value bounded beside it.
//
// Use stripControlChars where the length carries information (a multi-file report)
// and shellArg where the path is a command argument the reader has to run —
// truncating either corrupts the one thing it is there for. Nothing is truncated
// out of reach: the message names the config the value came from, so the full
// spelling is one file away.
func sanitizeFilePath(p string) string { return truncateEchoedText(stripControlChars(p)) }

// truncateEchoedText bounds one echoed string, marking a truncation so the reader
// can tell a shortened rendering from a complete one.
func truncateEchoedText(s string) string {
	if utf8.RuneCountInString(s) <= maxEchoedFileTextRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxEchoedFileTextRunes]) + "…"
}
