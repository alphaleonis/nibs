//go:build !unix

package store

import (
	"errors"
	"fmt"
)

// mkfifo reports that this platform cannot host the fixture: Windows named
// pipes live in the \\.\pipe\ namespace, not in the directory tree.
func mkfifo(path string) error {
	return fmt.Errorf("creating a named pipe at %s: %w", path, errors.ErrUnsupported)
}
