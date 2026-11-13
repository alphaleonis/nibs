//go:build unix

package store

import "syscall"

// mkfifo creates a named pipe at path. It is build-tagged because
// syscall.Mkfifo exists only where the filesystem holds FIFOs at ordinary paths.
func mkfifo(path string) error {
	return syscall.Mkfifo(path, 0o600)
}
