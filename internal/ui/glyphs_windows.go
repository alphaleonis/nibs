//go:build windows

package ui

import (
	"os"

	"golang.org/x/sys/windows"
	"golang.org/x/term"
)

// detectASCIIRequired returns true when the Windows console output codepage
// is not UTF-8 (codepage 65001). On such consoles the multi-byte UTF-8 glyphs
// the CLI normally uses render as mojibake, so callers should fall back to
// ASCII.
//
// When stdout is not a console (redirected to a file, piped to another tool,
// captured by a wrapper), the codepage check is irrelevant: pipes, files, and
// CI logs are all UTF-8-aware. We assume UTF-8 in that case. If the codepage
// query itself fails on a real console, we also default to UTF-8 — preferring
// occasional mojibake on a misconfigured console over silently degrading
// every redirected output.
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
