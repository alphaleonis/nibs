//go:build unix

package fsutil

import (
	"os"
	"syscall"
)

// readUmask reads the process umask without leaving it changed. No syscall only
// reads it, so the value has to be set to learn it and then put back.
//
// Between those two calls the umask is 0, and any file created in that window
// would be born world-writable. Package initialization runs before main and
// before any goroutine this program starts, so no such creator exists there.
// Do NOT move this to a lazy read.
func readUmask() os.FileMode {
	m := syscall.Umask(0)
	syscall.Umask(m)
	return os.FileMode(m) & os.ModePerm
}
