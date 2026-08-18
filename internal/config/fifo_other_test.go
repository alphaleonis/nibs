//go:build !unix

package config

import (
	"errors"
	"fmt"
)

// mkfifo reports that this platform cannot host the fixture. Windows named
// pipes live in the \\.\pipe\ namespace rather than in the directory tree, so
// there is no path a config could name that behaves like a FIFO.
func mkfifo(path string) error {
	return fmt.Errorf("creating a named pipe at %s: %w", path, errors.ErrUnsupported)
}
