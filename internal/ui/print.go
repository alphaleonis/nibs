package ui

import (
	"os"

	"charm.land/lipgloss/v2"
)

// Print, Printf and Println write to os.Stdout through Lip Gloss, which
// downsamples styled text to what the destination supports: no color for a
// pipe, a file or NO_COLOR. Style.Render output printed through fmt keeps its
// escape sequences, so print styled text through these.
//
// os.Stdout is read on each call, not captured once: tests swap it to capture
// output.

func Print(a ...any) {
	_, _ = lipgloss.Fprint(os.Stdout, a...)
}

func Printf(format string, a ...any) {
	_, _ = lipgloss.Fprintf(os.Stdout, format, a...)
}

func Println(a ...any) {
	_, _ = lipgloss.Fprintln(os.Stdout, a...)
}
