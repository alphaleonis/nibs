package ui

import (
	"os"

	"charm.land/lipgloss/v2"
)

// Print, Printf and Println write to stdout through Lip Gloss, which downsamples
// styled text to whatever the destination can actually show — truecolor, 256
// colors, or no color at all for a pipe, a file, or NO_COLOR.
//
// Style.Render emits full-fidelity ANSI unconditionally, so printing its result
// with the fmt equivalents leaks raw escape sequences into redirected output.
// Every command that renders a style must print through these.
//
// The destination is read from os.Stdout on each call rather than captured once,
// because tests swap os.Stdout to capture command output and a writer bound at
// package init would write past them to the real terminal.

func Print(a ...any) {
	_, _ = lipgloss.Fprint(os.Stdout, a...)
}

func Printf(format string, a ...any) {
	_, _ = lipgloss.Fprintf(os.Stdout, format, a...)
}

func Println(a ...any) {
	_, _ = lipgloss.Fprintln(os.Stdout, a...)
}
