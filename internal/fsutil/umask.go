package fsutil

import "os"

// processUmask is read ONCE, during package initialization — see readUmask for
// why the read cannot wait for first use. It is a snapshot: a process that
// changed its own umask later would not be reflected here. Nothing in this
// program does.
var processUmask = readUmask()

// Umask reports the umask this process started with, as permission bits. It is a
// seam as well as an accessor: processUmask is already fixed by the time any test
// runs, so a test cannot exercise masking through syscall.Umask.
var Umask = func() os.FileMode { return processUmask }

// ModeForNewFile applies the process umask to a base mode the way file creation
// would, for a caller that must set the mode with Chmod instead.
//
// The two differ: creating a file passes the mode through the kernel, which
// subtracts the umask, while Chmod sets it outright and the umask never enters.
// Every writer here Chmods a temp file and renames it (writeAndRename), so ask
// for the masking explicitly when you want what a plain create would produce.
//
// The umask only CLEARS bits, so the result is never wider than base. Pass the
// widest mode the file should ever have.
func ModeForNewFile(base os.FileMode) os.FileMode {
	return base &^ Umask()
}
