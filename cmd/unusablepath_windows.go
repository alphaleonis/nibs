package cmd

import (
	"errors"
	"syscall"
)

// isUnusablePath reports whether err says the path can NEVER name a directory,
// as opposed to naming one that is merely missing or unreadable.
//
// The distinction decides which remedy preLayoutRemedy prints, and Windows is
// where it bites: syscall.Errno.Is maps only ERROR_FILE_NOT_FOUND,
// ERROR_PATH_NOT_FOUND, _ERROR_BAD_NETPATH and ENOENT onto fs.ErrNotExist, so a
// name the filesystem rejects on sight arrives as neither absence nor a
// recognized I/O fault and falls through to "cannot be determined" — whose advice
// is to mount the volume and fix its permissions. Neither can help: no volume
// holds a file with a pipe in its name.
//
// Reached by any `nibs.path` carrying a character Windows reserves, and by every
// drive-relative spelling, since `C:proj` is not absolute by filepath.IsAbs and so
// is joined onto the project as `<project>\C:proj`.
//
// ONE CODE, because one is what os.Stat actually produces. Measured rather than
// taken from winerror.h, which suggests several plausible neighbors that never
// arrive:
//
//	bad|name  bad<name  bad>name  bad"name  bad*name  bad?name   -> 123
//	a:b:c     <project>\C:        ESC or \x01 in the name        -> 123
//	a 300-character component (past the per-component limit)     -> 123
//	a path longer than MAX_PATH                                  ->   3  (ErrNotExist)
//	afile\sub, where afile is a regular file                     ->   3  (ErrNotExist)
//	bad:name                                                     ->   2  (ErrNotExist)
//
// So ERROR_FILENAME_EXCED_RANGE and ERROR_DIRECTORY are not reachable this way,
// and `bad:name` is not even invalid — it names an alternate data stream, which
// is why it reports ordinary absence. The last two rows are already handled by
// the fs.ErrNotExist branch, which is the correct answer for them.
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
	return errno == _ERROR_INVALID_NAME
}

// _ERROR_INVALID_NAME is not among the codes syscall names — its exported block
// stops at ERROR_INSUFFICIENT_BUFFER (122) — and syscall.ENOTDIR is an alias for
// ERROR_PATH_NOT_FOUND rather than anything to do with this. The value is from
// winerror.h and is stable ABI; TestWindowsUnusablePathIsNotErrNotExist checks it
// against a real failing stat rather than trusting the constant.
const _ERROR_INVALID_NAME syscall.Errno = 123
