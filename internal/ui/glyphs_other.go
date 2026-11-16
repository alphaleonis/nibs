//go:build !windows

package ui

import "os"

func terminalUTF8() bool {
	return localeUTF8(os.Getenv)
}
