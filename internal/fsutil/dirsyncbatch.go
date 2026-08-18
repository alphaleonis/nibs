package fsutil

import "sort"

// DirSyncBatch collects the distinct directories a bulk loop wrote into, so the
// loop pays one directory fsync per DIRECTORY rather than one per file — the
// same flush, N times fewer. It is the other half of
// AtomicWriteFileDeferDirSync: that writer hands its flush obligation back to
// the caller, and this is what a caller discharging many of them holds.
//
// A SET rather than a single remembered directory, because one loop can span
// several. A nib's Path carries the content directory it lives in, so archived
// nibs sit under archive/ while active ones sit under data/, and data/ itself
// tolerates subdirectories that a nib's Path preserves. Syncing one hardcoded
// directory would silently drop the durability of every write outside it, and
// silently is the operative word: SyncDir returns nothing, so the loss is
// invisible to everything but the SyncDirFn seam.
//
// The zero value is ready to use, so a caller declares one and defers its
// Flush. Pass it by POINTER — a copy taken before the first Add gets its own
// backing map, and the Adds recorded through one would not reach the other.
// noCopy makes that a lint error rather than only a documented hazard: go vet's
// copylocks check, which this project already runs, reports any copy of a type
// carrying a Lock method.
type DirSyncBatch struct {
	_    noCopy
	dirs map[string]struct{}
}

// noCopy is the standard vet sentinel: it has the shape of a sync.Locker, which
// is what copylocks looks for, and does nothing at run time.
type noCopy struct{}

func (*noCopy) Lock()   {}
func (*noCopy) Unlock() {}

// Add records a directory to flush. The empty string is ignored, so a caller can
// hand it the result of a failed write — AtomicWriteFileDeferDirSync returns no
// directory when the rename never ran — without a guard at every call site.
func (b *DirSyncBatch) Add(dir string) {
	if dir == "" {
		return
	}
	if b.dirs == nil {
		b.dirs = make(map[string]struct{})
	}
	b.dirs[dir] = struct{}{}
}

// Flush fsyncs each collected directory once, in a deterministic order so a
// test can assert the set, and discharges them: a second Flush with nothing
// added in between does no work.
//
// RUN IT FROM A DEFER. A loop that returns on its first error has already
// committed every write before the failure, so those directory entries still
// need flushing — a flush placed after the loop is skipped exactly when a
// partial batch is on disk, which is the case it exists for.
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
