package nibcore

import (
	"sync"
	"testing"
)

// TestGetForUpdateRaceAgainstArchive pins that GetForUpdate takes its
// working-copy clone under the store lock. GetForUpdate resolves the shared
// stored pointer and Clones it; Clone's `clone := *b` shallow-copies every
// value field, including Path. Archive/Unarchive rewrite that same stored
// pointer's Path in place under c.mu. If the Clone runs off-lock, its Path read
// races the in-place Path write.
//
// Two goroutines per pair hammer the SAME nib: one loops GetForUpdate (the
// off-lock read on the buggy code), the other loops Archive/Unarchive (the
// in-place Path write under c.mu). With GetForUpdate cloning under c.mu.RLock
// the read is serialized against the writer and this is `-race` clean; it fails
// under `-race` if GetForUpdate ever reverts to cloning off-lock, so it is a
// real detector guard, not skipped.
func TestGetForUpdateRaceAgainstArchive(t *testing.T) {
	core, _ := setupTestCore(t)

	target := createTestNib(t, core, "target1", "Target", "completed")

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				// Clones the stored pointer; on the buggy code the Path read
				// here happens off-lock.
				got, err := core.GetForUpdate(target.ID)
				if err != nil {
					t.Errorf("GetForUpdate: %v", err)
					return
				}
				if got == nil || got.ID != target.ID {
					t.Errorf("GetForUpdate returned %v, want a clone of %s", got, target.ID)
					return
				}
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				// Rewrites the stored pointer's Path in place under c.mu.
				if err := core.Archive(target.ID); err != nil {
					t.Errorf("Archive: %v", err)
					return
				}
				if err := core.Unarchive(target.ID); err != nil {
					t.Errorf("Unarchive: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
}
