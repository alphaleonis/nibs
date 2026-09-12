//go:build !unix

package fsutil

import "os"

// readUmask reports no umask on platforms that have none. Windows is the case
// that matters: it carries no umask, and os.Chmod there only toggles the
// read-only attribute rather than setting a POSIX triad. Returning 0 leaves every
// caller's base mode untouched.
func readUmask() os.FileMode { return 0 }
