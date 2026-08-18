//go:build !windows

package cmd

import (
	"errors"
	"syscall"
)

// isUnusablePath reports whether err says the path can NEVER name a directory,
// as opposed to naming one that is merely missing or unreadable.
//
// The distinction decides which remedy preLayoutRemedy prints. POSIX reaches it
// far less often than Windows does, because a filename here is very nearly
// arbitrary bytes — there is no equivalent of Windows' reserved characters, so
// most bad `nibs.path` values simply do not exist and are already handled as
// absence through fs.ErrNotExist.
//
// Two structural failures remain, and both mean the same thing as an unusable
// Windows name: no mount and no chmod will make the path nameable. Measured on
// Linux 6.x / Go 1.26 rather than taken from errno.h:
//
//	afile/sub, where afile is a regular file  -> 20  ENOTDIR       (not ErrNotExist)
//	a 300-character component                 -> 36  ENAMETOOLONG  (not ErrNotExist)
//	an ordinary missing directory             ->  2  ENOENT        (ErrNotExist)
//
// The last row is the one that must NOT match: it is already absence, handled by
// the branch this predicate sits beside.
//
// The two platforms reach the same verdict by different codes — a 300-character
// component is ERROR_INVALID_NAME on Windows and ENAMETOOLONG here, while
// afile/sub is ERROR_PATH_NOT_FOUND there and ENOTDIR here — so the CLASSIFICATION
// is portable even though no errno is.
//
// Deliberately NOT a catch-all for "stat failed". Permission denied, a
// disconnected network volume and a device error are all cases where the
// directory may be real and full of nibs, and answering "it is not there" would
// tell the reader to go and recreate a store that already exists.
func isUnusablePath(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	switch errno {
	case
		// A component that has to be a directory is a regular file, a symlink
		// loop's target, or similar.
		syscall.ENOTDIR,
		// The name, or a component of it, exceeds the system limit.
		syscall.ENAMETOOLONG:
		return true
	}
	return false
}
