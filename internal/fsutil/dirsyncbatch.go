package fsutil

import "sort"

// DirSyncBatch collects the distinct directories a bulk loop wrote into, so the
// loop pays one directory fsync per DIRECTORY rather than one per file. It is the
// other half of AtomicWriteFileDeferDirSync, which hands its flush obligation back
// to the caller.
//
// It holds a SET because one loop can span several directories: a nib's Path
// carries the content directory it lives in, so archived nibs sit under archive/
// while active ones sit under data/, which itself tolerates subdirectories.
// Syncing one hardcoded directory would drop the durability of every write
// outside it, and SyncDir returns nothing, so that loss is invisible.
//
// The zero value is ready to use — declare one and defer its Flush. Pass it by
// POINTER: a copy taken before the first Add gets its own backing map, and Adds
// through one never reach the other. noCopy makes that a vet error rather than
// only a documented hazard.
type DirSyncBatch struct {
	_    noCopy
	dirs map[string]struct{}
}

// noCopy is the standard vet sentinel: it has the shape of a sync.Locker, which
// is what copylocks looks for, and does nothing at run time.
type noCopy struct{}

func (*noCopy) Lock()   {}
func (*noCopy) Unlock() {}

// Add records a directory to flush. It ignores the empty string, so the result of
// a failed write can go straight in without a guard at the call site.
func (b *DirSyncBatch) Add(dir string) {
	if dir == "" {
		return
	}
	if b.dirs == nil {
		b.dirs = make(map[string]struct{})
	}
	b.dirs[dir] = struct{}{}
}

// Flush fsyncs each collected directory once, in sorted order so a test can
// assert the set, and discharges them: a second Flush with nothing added between
// does no work.
//
// RUN IT FROM A DEFER. A loop that returns on its first error has already
// committed every write before the failure, so a flush placed after the loop is
// skipped exactly when a partial batch is on disk.
//
// Best-effort, like every directory sync here: see AtomicWriteFile's "does not
// promise" list.
func (b *DirSyncBatch) Flush() {
	dirs := make([]string, 0, len(b.dirs))
	for dir := range b.dirs {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		SyncDir(dir)
	}
	clear(b.dirs)
}
