//go:build windows

package ui

import (
	"os"

	"golang.org/x/sys/windows"
	"golang.org/x/term"
)

// detectASCIIRequired reports whether stdout is a console whose output codepage
// is not UTF-8 (65001). Redirected output and a failed codepage query both get
// UTF-8.
func detectASCIIRequired() bool {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return false
	}
	cp, err := windows.GetConsoleOutputCP()
	if err != nil {
		return false
	}
	return cp != 65001
}
