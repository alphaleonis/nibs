package nibcore

import (
	"sync"
	"testing"
)

// TestPublishedNibReadRaceAgainstArchive pins that a published nib can be read
// after the store lock is released while Archive and Unarchive move it. Both
// install a fresh pointer for the new Path (see NibReader.GetSnapshot in
// internal/graph/interfaces.go), so nothing writes the pointer a reader holds.
// It fails under -race if either one writes Path in place.
func TestPublishedNibReadRaceAgainstArchive(t *testing.T) {
	core, _ := setupTestCore(t)

	target := createTestNib(t, core, "target1", "Target", "completed")

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				live, err := core.Get(target.ID)
				if err != nil {
					t.Errorf("Get: %v", err)
					return
				}
				// Every field of the live pointer is read here, outside c.mu.
				_ = live.Clone()

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
