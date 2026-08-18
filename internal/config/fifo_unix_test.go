//go:build unix

package config

import "syscall"

// mkfifo creates a named pipe at path, for the fixture that proves a config
// read refuses one instead of blocking in open(2) forever.
//
// Build-tagged because syscall.Mkfifo exists only where the filesystem holds
// FIFOs at ordinary paths at all; see testskip.NamedPipes for what its absence
// costs the guard.
func mkfifo(path string) error {
	return syscall.Mkfifo(path, 0o600)
}
