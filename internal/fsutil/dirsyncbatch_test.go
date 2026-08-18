package fsutil

import (
	"path/filepath"
	"testing"
)

// recordSyncs swaps the directory-sync seam for one that records every directory
// flushed, and restores it afterwards. Nothing else can observe a directory
// sync — SyncDir returns nothing and swallows its errors by contract.
func recordSyncs(t *testing.T) *[]string {
	t.Helper()
	var synced []string
	orig := SyncDirFn
	SyncDirFn = func(dir string) { synced = append(synced, dir) }
	t.Cleanup(func() { SyncDirFn = orig })
	return &synced
}

func assertSynced(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("directories synced = %q (%d calls), want %q", got, len(got), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("directories synced = %q, want %q", got, want)
		}
	}
}

// TestDirSyncBatchFlushesEachDirectoryOnceInOrder pins the two properties the
// bulk callers depend on: a directory added repeatedly is flushed once (the
// saving the batch exists for), and the flush order is deterministic so a test
// can assert the set rather than a permutation of it.
func TestDirSyncBatchFlushesEachDirectoryOnceInOrder(t *testing.T) {
	var batch DirSyncBatch
	data := filepath.Join("store", "data")
	sub := filepath.Join("store", "data", "sub")
	archive := filepath.Join("store", "archive")

	// Added out of order and with data/ repeated, as a loop spanning the store's
	// content directories would produce.
	for _, dir := range []string{data, archive, data, sub, data} {
		batch.Add(dir)
	}

	synced := recordSyncs(t)
	batch.Flush()
	assertSynced(t, *synced, archive, data, sub)
}

// TestDirSyncBatchIgnoresTheEmptyDirectory covers the contract that lets a bulk
// loop hand Add the result of a write it has not yet checked:
// AtomicWriteFileDeferDirSync returns no directory when the rename never ran, so
// an empty string means "nothing landed, nothing to flush" rather than a
// directory to sync.
func TestDirSyncBatchIgnoresTheEmptyDirectory(t *testing.T) {
	var batch DirSyncBatch
	batch.Add("")

	synced := recordSyncs(t)
	batch.Flush()
	assertSynced(t, *synced)
}

// TestDirSyncBatchFlushDischargesWhatItFlushed pins the idempotency decision: a
// Flush hands back nothing still owed, so a second one with no Add in between
// does no work. Every caller today flushes once from a defer, which cannot tell
// the difference — a caller that also flushed mid-loop would otherwise re-pay
// for every directory it had already made durable.
func TestDirSyncBatchFlushDischargesWhatItFlushed(t *testing.T) {
	var batch DirSyncBatch
	data := filepath.Join("store", "data")
	archive := filepath.Join("store", "archive")
	batch.Add(data)

	synced := recordSyncs(t)
	batch.Flush()
	batch.Flush()
	assertSynced(t, *synced, data)

	// A directory added after a Flush is still owed one, so discharging the set
	// must not turn the batch into a spent object.
	batch.Add(archive)
	batch.Flush()
	assertSynced(t, *synced, data, archive)
}
