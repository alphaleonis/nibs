//go:build unix

package fsutil

import (
	"os"
	"syscall"
)

// readUmask reads the process umask without leaving it changed. There is no
// syscall that only reads it, so the value has to be set to learn it and then
// put back.
//
// Called once from a package-level initializer, which is what makes the
// round trip safe: between the two calls the process umask is 0, and a file
// created in that window by anyone would be born world-writable. Package
// initialization runs before main and before any goroutine this program starts,
// so there is no such concurrent creator. Do NOT move this to a lazy read.
func readUmask() os.FileMode {
	m := syscall.Umask(0)
	syscall.Umask(m)
	return os.FileMode(m) & os.ModePerm
}
