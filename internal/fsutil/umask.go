package fsutil

import "os"

// processUmask is read ONCE, during package initialization. See readUmask for
// why the read cannot be deferred to first use.
//
// A umask read at startup is a snapshot: a process that changed its own umask
// later would not be reflected here. Nothing in this program does, and the
// alternative — re-reading per write — reintroduces the window readUmask exists
// to avoid, on every single file.
var processUmask = readUmask()

// Umask reports the umask this process started with, as permission bits.
//
// It is a seam as well as an accessor: a test cannot exercise masking by calling
// syscall.Umask, because the value above is already fixed by the time any test
// runs. Production always uses the startup value.
var Umask = func() os.FileMode { return processUmask }

// ModeForNewFile applies the process umask to a base mode the way file creation
// would, for a caller that must set the mode with Chmod instead.
//
// The two are not interchangeable by default. Creating a file passes the mode
// through the kernel, which subtracts the umask; Chmod sets the mode outright
// and the umask never enters. Every writer here commits through a temp file
// that is Chmodded and renamed (see writeAndRename), so a caller wanting the
// mode a plain create would have produced has to ask for it.
//
// The umask can only CLEAR bits, never set them, so the result is never wider
// than base. Pass the widest mode the file should ever have.
func ModeForNewFile(base os.FileMode) os.FileMode {
	return base &^ Umask()
}
