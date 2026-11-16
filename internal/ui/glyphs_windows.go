//go:build windows

package ui

import (
	"os"

	"golang.org/x/sys/windows"
	"golang.org/x/term"
)

// terminalUTF8 reports whether stdout is not a console, or is one whose output
// codepage is UTF-8 (65001). A failed codepage query also counts as UTF-8.
func terminalUTF8() bool {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return true
	}
	cp, err := windows.GetConsoleOutputCP()
	if err != nil {
		return true
	}
	return cp == 65001
}
