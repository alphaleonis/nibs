//go:build !unix

package nibcore

import (
	"errors"
	"fmt"
)

// mkfifo reports that this platform cannot host the fixture. Windows named
// pipes live in the \\.\pipe\ namespace rather than in the directory tree, so
// there is no path inside a store that behaves like a FIFO.
func mkfifo(path string) error {
	return fmt.Errorf("creating a named pipe at %s: %w", path, errors.ErrUnsupported)
}
