package nibcore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
)

func setupTestCore(t *testing.T) (*Core, string) {
	t.Helper()
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, NibsDir)
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatalf("failed to create test .nibs dir: %v", err)
	}

	cfg := config.Default()
	core := New(nibsDir, cfg)
	core.SetWarnWriter(nil) // suppress warnings in tests
	if err := core.Load(); err != nil {
		t.Fatalf("failed to load core: %v", err)
	}

	return core, nibsDir
}

func setupTestCoreWithRequireIfMatch(t *testing.T) (*Core, string) {
	t.Helper()
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, NibsDir)
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatalf("failed to create test .nibs dir: %v", err)
	}

	cfg := config.Default()
	cfg.Nibs.RequireIfMatch = true
	core := New(nibsDir, cfg)
	core.SetWarnWriter(nil) // suppress warnings in tests
	if err := core.Load(); err != nil {
		t.Fatalf("failed to load core: %v", err)
	}

	return core, nibsDir
}

// writeNibFileAtomic writes a nib file the way production does — a temp file in
// the same directory, then a rename over the target — so a reader can never
// observe the file mid-write. Any test that writes a .md file while a watcher is
// live must use this rather than os.WriteFile.
//
// os.WriteFile truncates before it writes, leaving a window in which the file is
// empty. fsnotify reports the truncate, so a debounce batch firing inside that
// window loads a nib parsed from bytes that do not carry the title yet. The
// watcher recovers on the next event, but a test asserting on the first batch it
// receives sees the empty read and fails (nibs-6wdq).
func writeNibFileAtomic(t *testing.T, path, content string) {
	t.Helper()
	if err := atomicWriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write nib file %s: %v", path, err)
	}
}

func createTestNib(t *testing.T, core *Core, id, title, status string) *nib.Nib {
	t.Helper()
	b := &nib.Nib{
		ID:     id,
		Slug:   nib.Slugify(title),
		Title:  title,
		Status: status,
	}
	if err := core.Create(b); err != nil {
		t.Fatalf("failed to create test nib: %v", err)
	}
	return b
}

func TestNew(t *testing.T) {
	cfg := config.Default()
	core := New("/some/path", cfg)

	if core.Root() != "/some/path" {
		t.Errorf("Root() = %q, want %q", core.Root(), "/some/path")
	}
	if core.Config() != cfg {
		t.Error("Config() returned different config")
	}
}

func TestInit(t *testing.T) {
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, NibsDir)

	core := New(nibsDir, nil)
	err := core.Init()
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	info, err := os.Stat(nibsDir)
	if err != nil {
		t.Fatalf(".nibs directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Error(".nibs is not a directory")
	}
}

func TestInitIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, NibsDir)

	core := New(nibsDir, nil)

	// Call Init twice - should not error
	if err := core.Init(); err != nil {
		t.Fatalf("first Init() error = %v", err)
	}
	if err := core.Init(); err != nil {
		t.Fatalf("second Init() error = %v", err)
	}
}

func TestCreate(t *testing.T) {
	core, nibsDir := setupTestCore(t)

	b := &nib.Nib{
		ID:     "abc1",
		Slug:   "test-nib",
		Title:  "Test Nib",
		Status: "todo",
		Body:   "Some content here.",
	}

	err := core.Create(b)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Check file exists
	expectedPath := filepath.Join(nibsDir, "abc1--test-nib.md")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("nib file not created at %s", expectedPath)
	}

	// Check timestamps were set
	if b.CreatedAt == nil {
		t.Error("CreatedAt not set")
	}
	if b.UpdatedAt == nil {
		t.Error("UpdatedAt not set")
	}

	// Check Path was set
	if b.Path != "abc1--test-nib.md" {
		t.Errorf("Path = %q, want %q", b.Path, "abc1--test-nib.md")
	}

	// Check in-memory state
	all := core.All()
	if len(all) != 1 {
		t.Errorf("All() returned %d nibs, want 1", len(all))
	}
}

func TestCreateGeneratesID(t *testing.T) {
	core, _ := setupTestCore(t)

	b := &nib.Nib{
		Title:  "Auto ID Nib",
		Status: "todo",
	}

	err := core.Create(b)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if b.ID == "" {
		t.Error("ID was not generated")
	}
	if len(b.ID) != 4 { // Default ID length
		t.Errorf("ID length = %d, want 4", len(b.ID))
	}
}

func TestAll(t *testing.T) {
	core, _ := setupTestCore(t)

	createTestNib(t, core, "aaa1", "First Nib", "todo")
	createTestNib(t, core, "bbb2", "Second Nib", "in-progress")
	createTestNib(t, core, "ccc3", "Third Nib", "completed")

	nibs := core.All()
	if len(nibs) != 3 {
		t.Errorf("All() returned %d nibs, want 3", len(nibs))
	}
}

func TestAllEmpty(t *testing.T) {
	core, _ := setupTestCore(t)

	nibs := core.All()
	if len(nibs) != 0 {
		t.Errorf("All() returned %d nibs, want 0", len(nibs))
	}
}

func TestGet(t *testing.T) {
	core, _ := setupTestCore(t)

	createTestNib(t, core, "abc1", "First", "todo")
	createTestNib(t, core, "def2", "Second", "todo")

	t.Run("exact match", func(t *testing.T) {
		b, err := core.Get("abc1")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if b.ID != "abc1" {
			t.Errorf("ID = %q, want %q", b.ID, "abc1")
		}
	})

	t.Run("partial ID not found", func(t *testing.T) {
		_, err := core.Get("abc")
		if err != ErrNotFound {
			t.Errorf("Get() error = %v, want ErrNotFound", err)
		}
	})
}

func TestGetNotFound(t *testing.T) {
	core, _ := setupTestCore(t)

	createTestNib(t, core, "abc1", "Test", "todo")

	_, err := core.Get("xyz")
	if err != ErrNotFound {
		t.Errorf("Get() error = %v, want ErrNotFound", err)
	}
}

// TestGetForUpdate pins the accessor contract: GetForUpdate hands back an
// OWNED, independent copy the caller may mutate freely, and mutating it never
// leaks into the shared store nib that Get returns. This is the safe-by-
// construction guarantee the mutation sites rely on — mutate then a
// failed Update must not corrupt in-memory state.
func TestGetForUpdate(t *testing.T) {
	core, _ := setupTestCore(t)

	original := createTestNib(t, core, "abc1", "First", "todo")
	original.Tags = []string{"keep"}
	if err := core.Update(original, nil); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	t.Run("returns an independent copy", func(t *testing.T) {
		owned, err := core.GetForUpdate("abc1")
		if err != nil {
			t.Fatalf("GetForUpdate() error = %v", err)
		}

		shared, err := core.Get("abc1")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if owned == shared {
			t.Fatal("GetForUpdate returned the SHARED pointer, want an independent copy")
		}

		// Mutate the owned copy — including a slice field to prove the deep copy.
		owned.Status = "in-progress"
		owned.Title = "Mutated"
		owned.Tags = append(owned.Tags, "leaked")

		// The shared store nib must be untouched by the mutation above.
		got, err := core.Get("abc1")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if got.Status != "todo" {
			t.Errorf("shared Status = %q, want %q (mutation of owned copy leaked into the store)", got.Status, "todo")
		}
		if got.Title != "First" {
			t.Errorf("shared Title = %q, want %q", got.Title, "First")
		}
		if len(got.Tags) != 1 || got.Tags[0] != "keep" {
			t.Errorf("shared Tags = %v, want [keep] (slice mutation leaked)", got.Tags)
		}
	})

	t.Run("missing id returns ErrNotFound", func(t *testing.T) {
		if _, err := core.GetForUpdate("xyz"); err != ErrNotFound {
			t.Errorf("GetForUpdate() error = %v, want ErrNotFound", err)
		}
	})
}

func TestGetShortID(t *testing.T) {
	// Create a core with a configured prefix
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, NibsDir)
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatalf("failed to create test .nibs dir: %v", err)
	}

	cfg := config.DefaultWithPrefix("nibs-")
	core := New(nibsDir, cfg)
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("failed to load core: %v", err)
	}

	// Create nibs with the prefix
	createTestNib(t, core, "nibs-abc1", "First", "todo")
	createTestNib(t, core, "nibs-def2", "Second", "todo")

	t.Run("short ID exact match", func(t *testing.T) {
		b, err := core.Get("abc1")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if b.ID != "nibs-abc1" {
			t.Errorf("ID = %q, want %q", b.ID, "nibs-abc1")
		}
	})

	t.Run("full ID exact match", func(t *testing.T) {
		b, err := core.Get("nibs-abc1")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if b.ID != "nibs-abc1" {
			t.Errorf("ID = %q, want %q", b.ID, "nibs-abc1")
		}
	})

	t.Run("partial short ID not found", func(t *testing.T) {
		_, err := core.Get("abc")
		if err != ErrNotFound {
			t.Errorf("Get() error = %v, want ErrNotFound", err)
		}
	})

	t.Run("partial full ID not found", func(t *testing.T) {
		_, err := core.Get("nibs-ab")
		if err != ErrNotFound {
			t.Errorf("Get() error = %v, want ErrNotFound", err)
		}
	})

	t.Run("nonexistent ID not found", func(t *testing.T) {
		_, err := core.Get("xyz")
		if err != ErrNotFound {
			t.Errorf("Get() error = %v, want ErrNotFound", err)
		}
	})
}

func TestUpdate(t *testing.T) {
	core, _ := setupTestCore(t)

	b := createTestNib(t, core, "upd1", "Original Title", "todo")
	originalCreatedAt := *b.CreatedAt

	// Update the nib
	b.Title = "Updated Title"
	b.Status = "in-progress"

	err := core.Update(b, nil)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	// CreatedAt should be preserved
	if !b.CreatedAt.Equal(originalCreatedAt) {
		t.Errorf("CreatedAt changed: got %v, want %v", b.CreatedAt, originalCreatedAt)
	}

	// UpdatedAt should be refreshed (might be same second, so just check it's set)
	if b.UpdatedAt == nil {
		t.Error("UpdatedAt not set")
	}

	// Verify in-memory state
	loaded, err := core.Get("upd1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if loaded.Title != "Updated Title" {
		t.Errorf("Title = %q, want %q", loaded.Title, "Updated Title")
	}
	if loaded.Status != "in-progress" {
		t.Errorf("Status = %q, want %q", loaded.Status, "in-progress")
	}
}

func TestUpdateNotFound(t *testing.T) {
	core, _ := setupTestCore(t)

	b := &nib.Nib{
		ID:     "nonexistent",
		Title:  "Ghost Nib",
		Status: "todo",
	}

	err := core.Update(b, nil)
	if err != ErrNotFound {
		t.Errorf("Update() error = %v, want ErrNotFound", err)
	}
}

func TestDelete(t *testing.T) {
	core, nibsDir := setupTestCore(t)

	b := createTestNib(t, core, "del1", "To Delete", "todo")
	filePath := filepath.Join(nibsDir, b.Path)

	// Verify file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatal("nib file should exist before delete")
	}

	// Delete
	err := core.Delete("del1")
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// Verify file is gone
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Error("nib file should not exist after delete")
	}

	// Verify in-memory state
	_, err = core.Get("del1")
	if err != ErrNotFound {
		t.Error("nib should not be in memory after delete")
	}
}

func TestDeleteNotFound(t *testing.T) {
	core, _ := setupTestCore(t)

	err := core.Delete("nonexistent")
	if err != ErrNotFound {
		t.Errorf("Delete() error = %v, want ErrNotFound", err)
	}
}

func TestDeleteShortID(t *testing.T) {
	// Create a core with a configured prefix
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, NibsDir)
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatalf("failed to create test .nibs dir: %v", err)
	}

	cfg := config.DefaultWithPrefix("nibs-")
	core := New(nibsDir, cfg)
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("failed to load core: %v", err)
	}

	createTestNib(t, core, "nibs-xyz1", "Test", "todo")

	// Delete by short ID (without prefix)
	err := core.Delete("xyz1")
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// Verify it's gone
	_, err = core.Get("nibs-xyz1")
	if err != ErrNotFound {
		t.Error("nib should be deleted")
	}
}

func TestDeletePartialIDNotFound(t *testing.T) {
	core, _ := setupTestCore(t)

	createTestNib(t, core, "unique123", "Test", "todo")

	// Partial ID should not match
	err := core.Delete("unique")
	if err != ErrNotFound {
		t.Errorf("Delete() error = %v, want ErrNotFound", err)
	}

	// Verify nib still exists
	_, err = core.Get("unique123")
	if err != nil {
		t.Errorf("nib should still exist, got error: %v", err)
	}
}

func TestFullPath(t *testing.T) {
	root := filepath.Join("path", "to", ".nibs")
	core := New(root, nil)

	b := &nib.Nib{
		ID:   "abc1",
		Path: "abc1--test.md",
	}

	got := core.FullPath(b)
	want := filepath.Join("path", "to", ".nibs", "abc1--test.md")

	if got != want {
		t.Errorf("FullPath() = %q, want %q", got, want)
	}
}

func TestLoad(t *testing.T) {
	core, nibsDir := setupTestCore(t)

	// Create a nib file manually
	content := `---
title: Manual Nib
status: open
---

Manual content.
`
	if err := os.WriteFile(filepath.Join(nibsDir, "man1--manual.md"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Reload
	if err := core.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	b, err := core.Get("man1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if b.Title != "Manual Nib" {
		t.Errorf("Title = %q, want %q", b.Title, "Manual Nib")
	}
}

func TestLoadIgnoresNonMdFiles(t *testing.T) {
	core, nibsDir := setupTestCore(t)

	createTestNib(t, core, "abc1", "Real Nib", "todo")

	// Create non-.md files that should be ignored
	if err := os.WriteFile(filepath.Join(nibsDir, "config.yaml"), []byte("config"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nibsDir, "README.txt"), []byte("readme"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(nibsDir, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}

	// Reload
	if err := core.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	nibs := core.All()
	if len(nibs) != 1 {
		t.Errorf("All() returned %d nibs, want 1 (should ignore non-.md files)", len(nibs))
	}
}

func TestBlockedByPreserved(t *testing.T) {
	core, _ := setupTestCore(t)

	// Create nib A (the blocker)
	nibA := &nib.Nib{
		ID:     "aaa1",
		Slug:   "blocker",
		Title:  "Blocker Nib",
		Status: "todo",
	}
	if err := core.Create(nibA); err != nil {
		t.Fatalf("Create nibA error = %v", err)
	}

	// Create nib B that is blocked by A (single-side: stored on the blocked nib)
	nibB := &nib.Nib{
		ID:        "bbb2",
		Slug:      "blocked",
		Title:     "Blocked Nib",
		Status:    "todo",
		BlockedBy: []string{"aaa1"},
	}
	if err := core.Create(nibB); err != nil {
		t.Fatalf("Create nibB error = %v", err)
	}

	// Reload from disk
	if err := core.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Nib B should retain its blocked_by link after round-trip
	loadedB, err := core.Get("bbb2")
	if err != nil {
		t.Fatalf("Get bbb2 error = %v", err)
	}
	if !loadedB.IsBlockedBy("aaa1") {
		t.Errorf("Nib B BlockedBy = %v, want [aaa1]", loadedB.BlockedBy)
	}
}

func TestConcurrentAccess(t *testing.T) {
	core, _ := setupTestCore(t)

	// Create some initial nibs
	for i := 0; i < 10; i++ {
		createTestNib(t, core, nib.NewID("", 4), "Initial Nib", "todo")
	}

	// Run concurrent operations
	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// Readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = core.All()
			}
		}()
	}

	// Writers (create)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				b := &nib.Nib{
					Title:  "Concurrent Nib",
					Status: "todo",
				}
				if err := core.Create(b); err != nil {
					errors <- err
				}
			}
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent operation error: %v", err)
	}
}

func TestWatch(t *testing.T) {
	core, nibsDir := setupTestCore(t)

	createTestNib(t, core, "wat1", "Initial Nib", "todo")

	// Start watching
	if err := core.StartWatching(); err != nil {
		t.Fatalf("StartWatching() error = %v", err)
	}
	defer func() { _ = core.StopWatching() }()

	ch, unsub := core.Subscribe()
	defer unsub()

	// Give watcher time to start
	time.Sleep(50 * time.Millisecond)

	// Create a new nib file manually (simulating external change)
	content := `---
title: External Nib
status: open
---
`
	writeNibFileAtomic(t, filepath.Join(nibsDir, "ext1--external.md"), content)

	// Wait for the watcher to report the change
	select {
	case <-ch:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for change event")
	}

	// Verify the new nib is in memory
	if _, err := core.Get("ext1"); err != nil {
		t.Errorf("external nib not loaded: %v", err)
	}
}

func TestWatchDeletedNib(t *testing.T) {
	core, nibsDir := setupTestCore(t)

	b := createTestNib(t, core, "del1", "To Delete", "todo")

	// Start watching
	if err := core.StartWatching(); err != nil {
		t.Fatalf("StartWatching() error = %v", err)
	}
	defer func() { _ = core.StopWatching() }()

	ch, unsub := core.Subscribe()
	defer unsub()

	// Give watcher time to start
	time.Sleep(50 * time.Millisecond)

	// Delete the file manually
	if err := os.Remove(filepath.Join(nibsDir, b.Path)); err != nil {
		t.Fatalf("failed to delete file: %v", err)
	}

	// Wait for change notification
	select {
	case <-ch:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for delete event")
	}

	// Verify the nib is gone from memory
	if _, err := core.Get("del1"); err != ErrNotFound {
		t.Errorf("deleted nib still in memory: %v", err)
	}
}

func TestStopWatchingIdempotent(t *testing.T) {
	core, _ := setupTestCore(t)

	// StopWatching without watching should not error
	if err := core.StopWatching(); err != nil {
		t.Errorf("StopWatching() without StartWatching() error = %v", err)
	}

	// Start watching
	if err := core.StartWatching(); err != nil {
		t.Fatalf("StartWatching() error = %v", err)
	}

	// StopWatching twice should not error
	if err := core.StopWatching(); err != nil {
		t.Errorf("first StopWatching() error = %v", err)
	}
	if err := core.StopWatching(); err != nil {
		t.Errorf("second StopWatching() error = %v", err)
	}
}

func TestClose(t *testing.T) {
	core, _ := setupTestCore(t)

	// Start watching
	if err := core.StartWatching(); err != nil {
		t.Fatalf("StartWatching() error = %v", err)
	}

	// Close should stop the watcher
	if err := core.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestSubscribe(t *testing.T) {
	core, nibsDir := setupTestCore(t)

	// Start watching
	if err := core.StartWatching(); err != nil {
		t.Fatalf("StartWatching() error = %v", err)
	}
	defer func() { _ = core.StopWatching() }()

	// Subscribe to events
	ch, unsub := core.Subscribe()
	defer unsub()

	// Give watcher time to start
	time.Sleep(50 * time.Millisecond)

	// Create a nib file (should trigger EventCreated)
	content := `---
title: New Nib
status: todo
---
`
	writeNibFileAtomic(t, filepath.Join(nibsDir, "new1--new.md"), content)

	// Wait for events
	select {
	case events := <-ch:
		if len(events) == 0 {
			t.Error("expected at least one event")
		}
		found := false
		for _, e := range events {
			if e.Type == EventCreated && e.NibID == "new1" {
				found = true
				if e.Nib == nil {
					t.Error("EventCreated should include Nib")
				}
			}
		}
		if !found {
			t.Errorf("expected EventCreated for new1, got: %+v", events)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("timeout waiting for events")
	}
}

func TestSubscribeMultiple(t *testing.T) {
	core, nibsDir := setupTestCore(t)

	if err := core.StartWatching(); err != nil {
		t.Fatalf("StartWatching() error = %v", err)
	}
	defer func() { _ = core.StopWatching() }()

	// Create two subscribers
	ch1, unsub1 := core.Subscribe()
	defer unsub1()
	ch2, unsub2 := core.Subscribe()
	defer unsub2()

	// Give watcher time to start
	time.Sleep(50 * time.Millisecond)

	// Create a nib file
	content := `---
title: Multi Test
status: todo
---
`
	writeNibFileAtomic(t, filepath.Join(nibsDir, "mult--multi.md"), content)

	// Both subscribers should receive events
	received1, received2 := false, false
	timeout := time.After(500 * time.Millisecond)

	for !received1 || !received2 {
		select {
		case <-ch1:
			received1 = true
		case <-ch2:
			received2 = true
		case <-timeout:
			t.Fatalf("timeout: received1=%v, received2=%v", received1, received2)
		}
	}
}

func TestUnsubscribe(t *testing.T) {
	core, _ := setupTestCore(t)

	if err := core.StartWatching(); err != nil {
		t.Fatalf("StartWatching() error = %v", err)
	}
	defer func() { _ = core.StopWatching() }()

	ch, unsub := core.Subscribe()
	unsub()

	// Channel should be closed
	_, ok := <-ch
	if ok {
		t.Error("expected channel to be closed after unsubscribe")
	}
}

func TestEventTypes(t *testing.T) {
	core, nibsDir := setupTestCore(t)

	// Create an initial nib
	createTestNib(t, core, "evt1", "Event Test", "todo")

	if err := core.StartWatching(); err != nil {
		t.Fatalf("StartWatching() error = %v", err)
	}
	defer func() { _ = core.StopWatching() }()

	ch, unsub := core.Subscribe()
	defer unsub()

	// Give watcher time to start
	time.Sleep(50 * time.Millisecond)

	t.Run("update event", func(t *testing.T) {
		// Modify the existing nib file
		content := `---
title: Updated Title
status: in-progress
---
`
		writeNibFileAtomic(t, filepath.Join(nibsDir, "evt1--event-test.md"), content)

		select {
		case events := <-ch:
			found := false
			for _, e := range events {
				if e.Type == EventUpdated && e.NibID == "evt1" {
					found = true
					if e.Nib == nil {
						t.Error("EventUpdated should include Nib")
					}
					if e.Nib.Title != "Updated Title" {
						t.Errorf("expected updated title, got %q", e.Nib.Title)
					}
				}
			}
			if !found {
				t.Errorf("expected EventUpdated for evt1, got: %+v", events)
			}
		case <-time.After(500 * time.Millisecond):
			t.Error("timeout waiting for update event")
		}
	})

	t.Run("delete event", func(t *testing.T) {
		// Delete the nib file
		if err := os.Remove(filepath.Join(nibsDir, "evt1--event-test.md")); err != nil {
			t.Fatalf("failed to delete file: %v", err)
		}

		select {
		case events := <-ch:
			found := false
			for _, e := range events {
				if e.Type == EventDeleted && e.NibID == "evt1" {
					found = true
					if e.Nib != nil {
						t.Error("EventDeleted should have nil Nib")
					}
				}
			}
			if !found {
				t.Errorf("expected EventDeleted for evt1, got: %+v", events)
			}
		case <-time.After(500 * time.Millisecond):
			t.Error("timeout waiting for delete event")
		}
	})
}

// collectNibEvents drains ch for the whole window and returns every event naming
// nibID. An archive move can surface as more than one batch — the rename of the
// old path and the create at the archive path are separate fsnotify events, and
// the archive directory is only watched when it existed at StartWatching — so
// the assertions look at the settled set rather than the first batch.
func collectNibEvents(t *testing.T, ch <-chan []NibEvent, nibID string, window time.Duration) []NibEvent {
	t.Helper()

	var got []NibEvent
	deadline := time.After(window)
	for {
		select {
		case batch, ok := <-ch:
			if !ok {
				return got
			}
			for _, e := range batch {
				if e.NibID == nibID {
					got = append(got, e)
				}
			}
		case <-deadline:
			return got
		}
	}
}

// TestWatcherArchiveVsDelete pins the removal branch's classification. A file
// leaving its path only means the nib was deleted when the nib is really gone:
// Archive moves the file into archive/ and rewrites the stored Path, so the nib
// still exists there and is still savable. Reporting that as a deletion both
// lies to subscribers and evicts a live nib from the store.
func TestWatcherArchiveVsDelete(t *testing.T) {
	const nibID = "evt1"

	tests := []struct {
		name string
		// preWatch runs before StartWatching, so whatever it creates is walked
		// into the watch set.
		preWatch func(t *testing.T, core *Core, nibsDir, filename string)
		// act triggers the removal under test while the watcher is running.
		act  func(t *testing.T, core *Core, nibsDir, filename string)
		want EventType
		// notWant must never appear for the nib: the misclassification each case
		// exists to catch.
		notWant     EventType
		wantNibSet  bool
		wantInStore bool
	}{
		{
			name: "archiving into a fresh archive dir reports archived",
			act: func(t *testing.T, core *Core, nibsDir, filename string) {
				if err := core.Archive(nibID); err != nil {
					t.Fatalf("Archive() error = %v", err)
				}
			},
			want:        EventArchived,
			notWant:     EventDeleted,
			wantNibSet:  true,
			wantInStore: true,
		},
		{
			// The archive directory already exists, so the walk watches it and the
			// create at the archive path lands in the same batch as the rename —
			// the ordering fsnotify can produce either way round.
			name: "archiving into a pre-existing watched archive dir reports archived",
			preWatch: func(t *testing.T, core *Core, nibsDir, filename string) {
				if err := os.MkdirAll(filepath.Join(nibsDir, ArchiveDir), 0755); err != nil {
					t.Fatalf("failed to pre-create archive dir: %v", err)
				}
			},
			act: func(t *testing.T, core *Core, nibsDir, filename string) {
				if err := core.Archive(nibID); err != nil {
					t.Fatalf("Archive() error = %v", err)
				}
			},
			want:        EventArchived,
			notWant:     EventDeleted,
			wantNibSet:  true,
			wantInStore: true,
		},
		{
			name: "removing the file reports deleted",
			act: func(t *testing.T, core *Core, nibsDir, filename string) {
				if err := os.Remove(filepath.Join(nibsDir, filename)); err != nil {
					t.Fatalf("failed to remove nib file: %v", err)
				}
			},
			want:        EventDeleted,
			notWant:     EventArchived,
			wantNibSet:  false,
			wantInStore: false,
		},
		{
			// The stored Path says archive/, which is what marks an archived nib —
			// but its file is gone, so this is a real deletion, not a move.
			name: "removing an already-archived file reports deleted",
			preWatch: func(t *testing.T, core *Core, nibsDir, filename string) {
				if err := core.Archive(nibID); err != nil {
					t.Fatalf("Archive() error = %v", err)
				}
			},
			act: func(t *testing.T, core *Core, nibsDir, filename string) {
				if err := os.Remove(filepath.Join(nibsDir, ArchiveDir, filename)); err != nil {
					t.Fatalf("failed to remove archived nib file: %v", err)
				}
			},
			want:        EventDeleted,
			notWant:     EventArchived,
			wantNibSet:  false,
			wantInStore: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, nibsDir := setupTestCore(t)
			b := createTestNib(t, core, nibID, "Event Test", "todo")
			// Capture the filename now: Archive mutates the stored nib's Path in
			// place, and this is the same pointer.
			filename := filepath.Base(b.Path)

			if tt.preWatch != nil {
				tt.preWatch(t, core, nibsDir, filename)
			}

			if err := core.StartWatching(); err != nil {
				t.Fatalf("StartWatching() error = %v", err)
			}
			defer func() { _ = core.StopWatching() }()

			ch, unsub := core.Subscribe()
			defer unsub()

			// Let the watch goroutine reach its select before mutating the tree.
			time.Sleep(50 * time.Millisecond)

			tt.act(t, core, nibsDir, filename)

			got := collectNibEvents(t, ch, nibID, 500*time.Millisecond)

			var found *NibEvent
			for i, e := range got {
				if e.Type == tt.notWant {
					t.Errorf("got a %s event for %s; want none (events: %+v)", e.Type, nibID, got)
				}
				if e.Type == tt.want && found == nil {
					found = &got[i]
				}
			}
			if found == nil {
				t.Fatalf("expected a %s event for %s, got: %+v", tt.want, nibID, got)
			}
			if tt.wantNibSet && found.Nib == nil {
				t.Errorf("%s event should carry the nib", tt.want)
			}
			if !tt.wantNibSet && found.Nib != nil {
				t.Errorf("%s event should have a nil nib, got %+v", tt.want, found.Nib)
			}

			_, err := core.Get(nibID)
			if inStore := err == nil; inStore != tt.wantInStore {
				t.Errorf("Get(%q) found = %v, want %v", nibID, inStore, tt.wantInStore)
			}
		})
	}
}

func TestSubscribersClosedOnStopWatching(t *testing.T) {
	core, _ := setupTestCore(t)

	if err := core.StartWatching(); err != nil {
		t.Fatalf("StartWatching() error = %v", err)
	}

	ch, _ := core.Subscribe() // Note: not calling unsub
	if !core.hasPayloadSubscribers() {
		t.Fatal("hasPayloadSubscribers() = false right after Subscribe, want true")
	}

	// StopWatching should close subscriber channels
	if err := core.StopWatching(); err != nil {
		t.Fatalf("StopWatching() error = %v", err)
	}

	// Channel should be closed
	_, ok := <-ch
	if ok {
		t.Error("expected channel to be closed after StopWatching")
	}

	// StopWatching force-closes the payload subscriber without running its
	// unsubscribe (which is what decrements the count), so unwatchLocked's
	// payloadSubCount.Store(0) is the only thing that reclaims the counter here.
	// Drop that reset and the count stays stuck-positive, so handleChanges would
	// keep paying the per-nib clone with zero live payload subscribers — defeating
	// the optimization. This assertion bites exactly that regression.
	if core.hasPayloadSubscribers() {
		t.Error("hasPayloadSubscribers() = true after StopWatching, want false")
	}
}

// awaitEvents drains debounced batches until every wanted nib id has been seen
// carrying its wanted event type, or the ceiling expires.
//
// Draining rather than reading ONE batch is the whole point (nibs-cke5). The
// watcher's debounce timer RESETS on each event, so a burst of writes coalesces
// into a single batch only while every gap stays under debounceDelay. That holds
// easily under inotify, where a back-to-back burst arrives in microseconds — but
// Windows' ReadDirectoryChangesW delivers more coarsely, and a gap wider than the
// window splits the burst in two. A test that consumed one batch then asserted on
// both events was therefore correct only by luck, and failed on Windows CI.
//
// The ceiling is generous because it is a backstop against a genuine hang, not a
// timing assertion: the old fixed 500ms deadline was tight enough to look like the
// cause, and it was not — the CI failure landed on the missing-event assertion,
// never on the timeout branch.
//
// State is safe to read once this returns: handleChanges commits to the store
// under the lock BEFORE fanning out, an ordering that watcher.go marks as
// load-bearing.
//
// Distinct from collectNibEvents below, which always waits out its whole window
// and gathers every event for ONE nib id — the right tool when the question is
// "what did the settled set contain", as the archive-vs-delete tests ask. Reach
// for awaitEvents when the question is "have these arrived yet", across several
// ids: it returns as soon as they have, and cannot be used twice on one channel
// the way two collectNibEvents calls would race for the same batches.
func awaitEvents(t *testing.T, ch <-chan []NibEvent, want map[string]EventType) {
	t.Helper()

	pending := make(map[string]EventType, len(want))
	for id, typ := range want {
		pending[id] = typ
	}
	if len(pending) == 0 {
		t.Fatal("awaitEvents called with nothing to wait for; the assertion would be vacuous")
	}

	deadline := time.After(5 * time.Second)
	for len(pending) > 0 {
		select {
		case events := <-ch:
			for _, e := range events {
				if typ, ok := pending[e.NibID]; ok && e.Type == typ {
					delete(pending, e.NibID)
				}
			}
		case <-deadline:
			t.Fatalf("timed out waiting for events; still missing %v", pending)
		}
	}
}

func TestMultipleChangesInDebounceWindow(t *testing.T) {
	core, nibsDir := setupTestCore(t)

	// Create an initial nib to update
	createTestNib(t, core, "upd1", "To Update", "todo")

	if err := core.StartWatching(); err != nil {
		t.Fatalf("StartWatching() error = %v", err)
	}
	defer func() { _ = core.StopWatching() }()

	ch, unsub := core.Subscribe()
	defer unsub()

	time.Sleep(50 * time.Millisecond)

	// Make multiple changes rapidly (within debounce window)
	// 1. Create a new nib
	content1 := `---
title: New Nib
status: todo
---
`
	writeNibFileAtomic(t, filepath.Join(nibsDir, "new1--new.md"), content1)

	// 2. Update existing nib
	content2 := `---
title: Updated Nib
status: in-progress
---
`
	writeNibFileAtomic(t, filepath.Join(nibsDir, "upd1--to-update.md"), content2)

	// 3. Create another nib then delete it (net effect: nothing)
	writeNibFileAtomic(t, filepath.Join(nibsDir, "tmp1--temp.md"), content1)
	_ = os.Remove(filepath.Join(nibsDir, "tmp1--temp.md"))

	// tmp1 might or might not appear depending on timing, so it is not awaited.
	awaitEvents(t, ch, map[string]EventType{
		"new1": EventCreated,
		"upd1": EventUpdated,
	})

	// Verify state is correct
	_, err := core.Get("new1")
	if err != nil {
		t.Errorf("new1 should exist: %v", err)
	}

	upd, err := core.Get("upd1")
	if err != nil {
		t.Fatalf("upd1 should exist: %v", err)
	}
	if upd.Title != "Updated Nib" {
		t.Errorf("upd1 title = %q, want %q", upd.Title, "Updated Nib")
	}

	// tmp1 should not exist
	_, err = core.Get("tmp1")
	if err != ErrNotFound {
		t.Error("tmp1 should not exist (was created then deleted)")
	}
}

// TestChangesSpanningDebounceWindows pins the failure mode behind nibs-cke5 and
// makes it deterministic on every platform, rather than leaving it to whether an
// OS happens to deliver events coarsely enough to expose it.
//
// The sibling test above writes its changes back-to-back, so they land in ONE
// debounced batch under inotify. Windows delivered the same burst with a wider
// gap, splitting it across two batches, and the single-batch read there missed
// the second half — failing roughly one run in some tens on Windows CI while
// passing forever on Linux. Sleeping past debounceDelay reproduces that split
// deliberately, so the drain in awaitEvents is exercised on the machine of
// whoever runs the suite, not only on the CI leg that happened to be unlucky.
//
// The sleep is deliberately a multiple of the real debounceDelay const rather
// than a hardcoded duration, so retuning the window cannot silently turn this
// back into a same-batch test that proves nothing.
func TestChangesSpanningDebounceWindows(t *testing.T) {
	core, nibsDir := setupTestCore(t)

	createTestNib(t, core, "upd1", "To Update", "todo")

	if err := core.StartWatching(); err != nil {
		t.Fatalf("StartWatching() error = %v", err)
	}
	defer func() { _ = core.StopWatching() }()

	ch, unsub := core.Subscribe()
	defer unsub()

	time.Sleep(50 * time.Millisecond)

	writeNibFileAtomic(t, filepath.Join(nibsDir, "new1--new.md"), `---
title: New Nib
status: todo
---
`)

	// Wide enough that the first batch has certainly fired before the next write.
	time.Sleep(3 * debounceDelay)

	writeNibFileAtomic(t, filepath.Join(nibsDir, "upd1--to-update.md"), `---
title: Updated Nib
status: in-progress
---
`)

	awaitEvents(t, ch, map[string]EventType{
		"new1": EventCreated,
		"upd1": EventUpdated,
	})

	if _, err := core.Get("new1"); err != nil {
		t.Errorf("new1 should exist: %v", err)
	}
	upd, err := core.Get("upd1")
	if err != nil {
		t.Fatalf("upd1 should exist: %v", err)
	}
	if upd.Title != "Updated Nib" {
		t.Errorf("upd1 title = %q, want %q", upd.Title, "Updated Nib")
	}
}

func TestInvalidFileIgnored(t *testing.T) {
	core, nibsDir := setupTestCore(t)

	// Create a valid nib first
	createTestNib(t, core, "val1", "Valid Nib", "todo")

	if err := core.StartWatching(); err != nil {
		t.Fatalf("StartWatching() error = %v", err)
	}
	defer func() { _ = core.StopWatching() }()

	ch, unsub := core.Subscribe()
	defer unsub()

	time.Sleep(50 * time.Millisecond)

	// Create an invalid nib file (malformed YAML frontmatter)
	invalidContent := `---
title: [unclosed bracket
status: {broken yaml
---
`
	writeNibFileAtomic(t, filepath.Join(nibsDir, "bad1--invalid.md"), invalidContent)

	// Also create a valid nib to verify processing continues
	validContent := `---
title: Another Valid
status: todo
---
`
	writeNibFileAtomic(t, filepath.Join(nibsDir, "val2--another.md"), validContent)

	// Wait for events
	select {
	case events := <-ch:
		// Should have event for val2 (created), bad1 should be skipped
		foundVal2 := false
		for _, e := range events {
			if e.NibID == "val2" && e.Type == EventCreated {
				foundVal2 = true
			}
			if e.NibID == "bad1" {
				t.Error("bad1 should not produce an event (invalid file)")
			}
		}
		if !foundVal2 {
			t.Error("expected EventCreated for val2")
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("timeout waiting for events")
	}

	// Valid nibs should still be accessible
	if _, err := core.Get("val1"); err != nil {
		t.Errorf("val1 should still exist: %v", err)
	}
	if _, err := core.Get("val2"); err != nil {
		t.Errorf("val2 should exist: %v", err)
	}
}

func TestRapidUpdatesToSameFile(t *testing.T) {
	core, nibsDir := setupTestCore(t)

	createTestNib(t, core, "rap1", "Rapid Updates", "todo")

	if err := core.StartWatching(); err != nil {
		t.Fatalf("StartWatching() error = %v", err)
	}
	defer func() { _ = core.StopWatching() }()

	ch, unsub := core.Subscribe()
	defer unsub()

	time.Sleep(50 * time.Millisecond)

	// Write to the same file multiple times rapidly
	const writes = 5
	const finalTitle = "Update 5"
	path := filepath.Join(nibsDir, "rap1--rapid-updates.md")
	for i := 1; i <= writes; i++ {
		content := fmt.Sprintf(`---
title: Update %d
status: todo
---
`, i)
		writeNibFileAtomic(t, path, content)
		time.Sleep(10 * time.Millisecond) // Small delay but within debounce
	}

	// Collect batches until the settled value arrives, rather than asserting on
	// whichever batch happens to land first. Debouncing coalesces the writes, but
	// nothing pins WHICH window each write falls into: a runner that stalls this
	// goroutine for longer than debounceDelay mid-loop publishes an intermediate
	// value in one batch and the final one in a later batch. Both are correct
	// watcher behavior, so the settled state is what this can assert on.
	rap1Events := 0
	deadline := time.After(5 * time.Second)
	for {
		var events []NibEvent
		select {
		case events = <-ch:
		case <-deadline:
			t.Fatalf("timeout waiting for a batch carrying title %q (saw %d events for rap1)",
				finalTitle, rap1Events)
		}

		// One event per file per batch is the coalescing property, and it holds
		// however the writes fall across windows: pendingChanges is keyed by path,
		// so repeated writes to one file collapse into a single entry.
		inBatch := 0
		var lastEvent NibEvent
		for _, e := range events {
			if e.NibID == "rap1" {
				inBatch++
				lastEvent = e
			}
		}
		if inBatch == 0 {
			continue
		}
		if inBatch != 1 {
			t.Fatalf("batch carries %d events for rap1, want 1 — writes to one file must coalesce", inBatch)
		}
		rap1Events++

		if lastEvent.Type != EventUpdated {
			t.Errorf("expected EventUpdated, got %v", lastEvent.Type)
		}
		if lastEvent.Nib == nil {
			t.Fatal("EventUpdated must carry a Nib")
		}
		if lastEvent.Nib.Title == finalTitle {
			break
		}
	}

	// Debouncing must actually have coalesced something. Five writes spanning
	// ~40ms against a 100ms window cannot each earn their own batch unless the
	// watcher idled a full window four times over — so one event per write means
	// debouncing has stopped working, which the settle loop alone would not catch.
	if rap1Events >= writes {
		t.Errorf("got %d events for %d rapid writes — writes were not debounced", rap1Events, writes)
	}

	// The store settles on the final value too, not just the event stream.
	stored, err := core.Get("rap1")
	if err != nil {
		t.Fatalf("Get(rap1) error = %v", err)
	}
	if stored.Title != finalTitle {
		t.Errorf("stored title = %q, want %q", stored.Title, finalTitle)
	}
}

// Archive functionality tests

func TestArchive(t *testing.T) {
	core, nibsDir := setupTestCore(t)

	b := createTestNib(t, core, "arc1", "To Archive", "completed")
	originalFilename := filepath.Base(b.Path)

	// Archive the nib
	err := core.Archive("arc1")
	if err != nil {
		t.Fatalf("Archive() error = %v", err)
	}

	// Verify file moved to archive directory
	archivePath := filepath.Join(nibsDir, ArchiveDir, originalFilename)
	if _, err := os.Stat(archivePath); os.IsNotExist(err) {
		t.Error("nib file should exist in archive directory")
	}

	// Verify file no longer in main directory
	mainPath := filepath.Join(nibsDir, "arc1--to-archive.md")
	if _, err := os.Stat(mainPath); !os.IsNotExist(err) {
		t.Error("nib file should not exist in main directory")
	}

	// Verify nib is still accessible in memory
	archived, err := core.Get("arc1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	// Verify path is updated (Path uses forward slashes for portability)
	wantPath := ArchiveDir + "/" + "arc1--to-archive.md"
	if archived.Path != wantPath {
		t.Errorf("Path = %q, want %q", archived.Path, wantPath)
	}
}

func TestArchiveIdempotent(t *testing.T) {
	core, _ := setupTestCore(t)

	createTestNib(t, core, "arc1", "To Archive", "completed")

	// Archive twice should not error
	if err := core.Archive("arc1"); err != nil {
		t.Fatalf("first Archive() error = %v", err)
	}
	if err := core.Archive("arc1"); err != nil {
		t.Fatalf("second Archive() error = %v", err)
	}
}

func TestArchiveNotFound(t *testing.T) {
	core, _ := setupTestCore(t)

	err := core.Archive("nonexistent")
	if err != ErrNotFound {
		t.Errorf("Archive() error = %v, want ErrNotFound", err)
	}
}

func TestUnarchive(t *testing.T) {
	core, nibsDir := setupTestCore(t)

	b := createTestNib(t, core, "una1", "To Unarchive", "completed")
	originalFilename := filepath.Base(b.Path)

	// Archive first
	if err := core.Archive("una1"); err != nil {
		t.Fatalf("Archive() error = %v", err)
	}

	// Unarchive
	err := core.Unarchive("una1")
	if err != nil {
		t.Fatalf("Unarchive() error = %v", err)
	}

	// Verify file moved back to main directory
	mainPath := filepath.Join(nibsDir, originalFilename)
	if _, err := os.Stat(mainPath); os.IsNotExist(err) {
		t.Error("nib file should exist in main directory")
	}

	// Verify file no longer in archive
	archivePath := filepath.Join(nibsDir, ArchiveDir, originalFilename)
	if _, err := os.Stat(archivePath); !os.IsNotExist(err) {
		t.Error("nib file should not exist in archive directory")
	}

	// Verify path is updated
	unarchived, err := core.Get("una1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if unarchived.Path != "una1--to-unarchive.md" {
		t.Errorf("Path = %q, want %q", unarchived.Path, "una1--to-unarchive.md")
	}
}

func TestUnarchiveIdempotent(t *testing.T) {
	core, _ := setupTestCore(t)

	createTestNib(t, core, "una1", "To Unarchive", "completed")

	// Unarchive non-archived nib should not error
	if err := core.Unarchive("una1"); err != nil {
		t.Fatalf("Unarchive() on non-archived nib error = %v", err)
	}
}

func TestIsArchived(t *testing.T) {
	core, _ := setupTestCore(t)

	createTestNib(t, core, "isa1", "Test Archived", "completed")

	t.Run("not archived", func(t *testing.T) {
		if core.IsArchived("isa1") {
			t.Error("IsArchived() should return false for non-archived nib")
		}
	})

	// Archive the nib
	if err := core.Archive("isa1"); err != nil {
		t.Fatalf("Archive() error = %v", err)
	}

	t.Run("archived", func(t *testing.T) {
		if !core.IsArchived("isa1") {
			t.Error("IsArchived() should return true for archived nib")
		}
	})

	t.Run("nonexistent", func(t *testing.T) {
		if core.IsArchived("nonexistent") {
			t.Error("IsArchived() should return false for nonexistent nib")
		}
	})
}

func TestArchivedNibsAlwaysLoaded(t *testing.T) {
	core, nibsDir := setupTestCore(t)

	// Create nibs and archive one
	createTestNib(t, core, "act1", "Active Nib", "todo")
	createTestNib(t, core, "arc1", "Archived Nib", "completed")
	if err := core.Archive("arc1"); err != nil {
		t.Fatalf("Archive() error = %v", err)
	}

	// Create a new core and load - archived nibs should always be included
	core2 := New(nibsDir, config.Default())
	core2.SetWarnWriter(nil)
	if err := core2.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	t.Run("all nibs loaded including archived", func(t *testing.T) {
		nibs := core2.All()
		if len(nibs) != 2 {
			t.Errorf("All() returned %d nibs, want 2 (active + archived)", len(nibs))
		}
	})

	t.Run("active nib accessible", func(t *testing.T) {
		if _, err := core2.Get("act1"); err != nil {
			t.Errorf("active nib should be found: %v", err)
		}
	})

	t.Run("archived nib accessible", func(t *testing.T) {
		if _, err := core2.Get("arc1"); err != nil {
			t.Errorf("archived nib should be found: %v", err)
		}
	})

	t.Run("archived nib has correct path", func(t *testing.T) {
		b, _ := core2.Get("arc1")
		if !core2.IsArchived("arc1") {
			t.Error("archived nib should be identified as archived")
		}
		if b.Path != "archive/arc1--archived-nib.md" {
			t.Errorf("archived nib path = %q, want %q", b.Path, "archive/arc1--archived-nib.md")
		}
	})
}

func TestLoadFromSubdirectories(t *testing.T) {
	// Create a core with nibs in various subdirectories
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, NibsDir)
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatalf("failed to create test .nibs dir: %v", err)
	}

	// Create subdirectories
	milestone1Dir := filepath.Join(nibsDir, "milestone-1")
	milestone2Dir := filepath.Join(nibsDir, "milestone-2")
	nestedDir := filepath.Join(nibsDir, "epics", "auth")
	if err := os.MkdirAll(milestone1Dir, 0755); err != nil {
		t.Fatalf("failed to create milestone-1 dir: %v", err)
	}
	if err := os.MkdirAll(milestone2Dir, 0755); err != nil {
		t.Fatalf("failed to create milestone-2 dir: %v", err)
	}
	if err := os.MkdirAll(nestedDir, 0755); err != nil {
		t.Fatalf("failed to create nested dir: %v", err)
	}

	// Create nibs in different locations
	writeTestNibFile(t, filepath.Join(nibsDir, "root1--root-nib.md"), "root1", "Root Nib", "todo")
	writeTestNibFile(t, filepath.Join(milestone1Dir, "m1b1--milestone-one-nib.md"), "m1b1", "Milestone One Nib", "todo")
	writeTestNibFile(t, filepath.Join(milestone2Dir, "m2b1--milestone-two-nib.md"), "m2b1", "Milestone Two Nib", "in-progress")
	writeTestNibFile(t, filepath.Join(nestedDir, "auth1--auth-nib.md"), "auth1", "Auth Nib", "todo")

	// Load and verify all nibs are found
	core := New(nibsDir, config.Default())
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	nibs := core.All()
	if len(nibs) != 4 {
		t.Errorf("All() returned %d nibs, want 4", len(nibs))
	}

	// Verify each nib is accessible and has correct path
	testCases := []struct {
		id           string
		expectedPath string
	}{
		{"root1", "root1--root-nib.md"},
		{"m1b1", "milestone-1/m1b1--milestone-one-nib.md"},
		{"m2b1", "milestone-2/m2b1--milestone-two-nib.md"},
		{"auth1", "epics/auth/auth1--auth-nib.md"},
	}

	for _, tc := range testCases {
		t.Run(tc.id, func(t *testing.T) {
			b, err := core.Get(tc.id)
			if err != nil {
				t.Fatalf("Get(%q) error = %v", tc.id, err)
			}
			if b.Path != tc.expectedPath {
				t.Errorf("Path = %q, want %q", b.Path, tc.expectedPath)
			}
		})
	}
}

// writeTestNibFile creates a nib file directly on disk (for testing load scenarios)
func writeTestNibFile(t *testing.T, path, id, title, status string) {
	t.Helper()
	content := fmt.Sprintf(`---
title: %s
status: %s
type: task
---

Test nib content.
`, title, status)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test nib file: %v", err)
	}
}

func TestGetFromArchive(t *testing.T) {
	core, nibsDir := setupTestCore(t)

	createTestNib(t, core, "gfa1", "Get From Archive", "completed")
	if err := core.Archive("gfa1"); err != nil {
		t.Fatalf("Archive() error = %v", err)
	}

	// Create a new core - archived nibs are loaded but GetFromArchive reads directly from disk
	core2 := New(nibsDir, config.Default())
	core2.SetWarnWriter(nil)
	if err := core2.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	t.Run("nib in archive", func(t *testing.T) {
		b, err := core2.GetFromArchive("gfa1")
		if err != nil {
			t.Fatalf("GetFromArchive() error = %v", err)
		}
		if b == nil {
			t.Fatal("GetFromArchive() returned nil")
			return
		}
		if b.ID != "gfa1" {
			t.Errorf("ID = %q, want %q", b.ID, "gfa1")
		}
	})

	t.Run("nib not in archive", func(t *testing.T) {
		b, err := core2.GetFromArchive("nonexistent")
		if err != nil {
			t.Fatalf("GetFromArchive() error = %v", err)
		}
		if b != nil {
			t.Error("GetFromArchive() should return nil for nonexistent nib")
		}
	})

	t.Run("no archive directory", func(t *testing.T) {
		// Create a fresh core with no archive
		tmpDir := t.TempDir()
		freshNibsDir := filepath.Join(tmpDir, NibsDir)
		if err := os.MkdirAll(freshNibsDir, 0755); err != nil {
			t.Fatalf("failed to create .nibs dir: %v", err)
		}
		core3 := New(freshNibsDir, config.Default())
		core3.SetWarnWriter(nil)
		if err := core3.Load(); err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		b, err := core3.GetFromArchive("anything")
		if err != nil {
			t.Fatalf("GetFromArchive() error = %v", err)
		}
		if b != nil {
			t.Error("GetFromArchive() should return nil when archive doesn't exist")
		}
	})
}

func TestLoadAndUnarchive(t *testing.T) {
	core, nibsDir := setupTestCore(t)

	createTestNib(t, core, "lau1", "Load And Unarchive", "completed")
	if err := core.Archive("lau1"); err != nil {
		t.Fatalf("Archive() error = %v", err)
	}

	// Create a new core - archived nibs are now always loaded
	core2 := New(nibsDir, config.Default())
	core2.SetWarnWriter(nil)
	if err := core2.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Nib should be accessible (archived nibs are always loaded)
	b, err := core2.Get("lau1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !core2.IsArchived("lau1") {
		t.Error("nib should be identified as archived before LoadAndUnarchive")
	}

	// Load and unarchive should move the file
	unarchived, err := core2.LoadAndUnarchive("lau1")
	if err != nil {
		t.Fatalf("LoadAndUnarchive() error = %v", err)
	}
	if unarchived == nil {
		t.Fatal("LoadAndUnarchive() returned nil")
		return
	}
	if unarchived.ID != b.ID {
		t.Errorf("LoadAndUnarchive returned different nib: got %q, want %q", unarchived.ID, b.ID)
	}

	// Nib should no longer be archived
	if core2.IsArchived("lau1") {
		t.Error("nib should not be archived after LoadAndUnarchive")
	}

	// File should be in main directory, not archive
	mainPath := filepath.Join(nibsDir, "lau1--load-and-unarchive.md")
	if _, err := os.Stat(mainPath); os.IsNotExist(err) {
		t.Error("nib file should exist in main directory after LoadAndUnarchive")
	}

	// File should NOT be in archive directory
	archivePath := filepath.Join(nibsDir, "archive", "lau1--load-and-unarchive.md")
	if _, err := os.Stat(archivePath); !os.IsNotExist(err) {
		t.Error("nib file should not exist in archive directory after LoadAndUnarchive")
	}
}

func TestLoadAndUnarchiveNotFound(t *testing.T) {
	core, _ := setupTestCore(t)

	_, err := core.LoadAndUnarchive("nonexistent")
	if err != ErrNotFound {
		t.Errorf("LoadAndUnarchive() error = %v, want ErrNotFound", err)
	}
}

func TestArchiveShortID(t *testing.T) {
	// Create a core with a configured prefix
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, NibsDir)
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatalf("failed to create test .nibs dir: %v", err)
	}

	cfg := config.DefaultWithPrefix("nibs-")
	core := New(nibsDir, cfg)
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("failed to load core: %v", err)
	}

	createTestNib(t, core, "nibs-xyz1", "Test", "completed")

	// Archive by short ID (without prefix)
	err := core.Archive("xyz1")
	if err != nil {
		t.Fatalf("Archive() error = %v", err)
	}

	// Verify it's archived
	if !core.IsArchived("nibs-xyz1") {
		t.Error("nib should be archived")
	}
}

func TestNormalizeID(t *testing.T) {
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, NibsDir)
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultWithPrefix("nibs-")
	core := New(nibsDir, cfg)
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("failed to load core: %v", err)
	}

	createTestNib(t, core, "nibs-abc1", "Test Nib", "todo")

	t.Run("exact match returns same ID", func(t *testing.T) {
		normalized, found := core.NormalizeID("nibs-abc1")
		if !found {
			t.Error("NormalizeID() should find exact match")
		}
		if normalized != "nibs-abc1" {
			t.Errorf("NormalizeID() = %q, want %q", normalized, "nibs-abc1")
		}
	})

	t.Run("short ID normalizes to full ID", func(t *testing.T) {
		normalized, found := core.NormalizeID("abc1")
		if !found {
			t.Error("NormalizeID() should find short ID match")
		}
		if normalized != "nibs-abc1" {
			t.Errorf("NormalizeID() = %q, want %q", normalized, "nibs-abc1")
		}
	})

	t.Run("nonexistent ID returns original", func(t *testing.T) {
		normalized, found := core.NormalizeID("nonexistent")
		if found {
			t.Error("NormalizeID() should not find nonexistent ID")
		}
		if normalized != "nonexistent" {
			t.Errorf("NormalizeID() = %q, want %q", normalized, "nonexistent")
		}
	})
}

func TestUpdateWithETag(t *testing.T) {
	core, _ := setupTestCore(t)

	t.Run("update with correct etag succeeds", func(t *testing.T) {
		b := &nib.Nib{
			ID:     "etag-test-1",
			Title:  "ETag Test",
			Status: "todo",
			Body:   "Original",
		}
		if err := core.Create(b); err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		currentETag := b.ETag()
		b.Title = "Updated"
		err := core.Update(b, &currentETag)
		if err != nil {
			t.Errorf("Update() with correct etag failed: %v", err)
		}
	})

	t.Run("update with wrong etag fails", func(t *testing.T) {
		b := &nib.Nib{
			ID:     "etag-test-2",
			Title:  "ETag Test",
			Status: "todo",
		}
		if err := core.Create(b); err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		wrongETag := "wrongetag123"
		b.Title = "Should Fail"
		err := core.Update(b, &wrongETag)

		var mismatchErr *ETagMismatchError
		if !errors.As(err, &mismatchErr) {
			t.Errorf("Update() with wrong etag should return ETagMismatchError, got %T: %v", err, err)
		}
	})

	t.Run("update without etag succeeds when not required", func(t *testing.T) {
		b := &nib.Nib{
			ID:     "etag-test-3",
			Title:  "ETag Test",
			Status: "todo",
		}
		if err := core.Create(b); err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		b.Title = "No ETag"
		err := core.Update(b, nil)
		if err != nil {
			t.Errorf("Update() without etag failed: %v", err)
		}
	})
}

func TestUpdateWithETagRequired(t *testing.T) {
	core, _ := setupTestCoreWithRequireIfMatch(t)

	t.Run("update without etag fails when required", func(t *testing.T) {
		b := &nib.Nib{
			ID:     "etag-req-test-1",
			Title:  "ETag Required Test",
			Status: "todo",
		}
		if err := core.Create(b); err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		b.Title = "Should Fail"
		err := core.Update(b, nil)

		var requiredErr *ETagRequiredError
		if !errors.As(err, &requiredErr) {
			t.Errorf("Update() without etag should return ETagRequiredError when required, got %T: %v", err, err)
		}
	})

	t.Run("update with empty etag fails when required", func(t *testing.T) {
		b := &nib.Nib{
			ID:     "etag-req-test-2",
			Title:  "ETag Required Test",
			Status: "todo",
		}
		if err := core.Create(b); err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		emptyETag := ""
		b.Title = "Should Fail"
		err := core.Update(b, &emptyETag)

		var requiredErr *ETagRequiredError
		if !errors.As(err, &requiredErr) {
			t.Errorf("Update() with empty etag should return ETagRequiredError when required, got %T: %v", err, err)
		}
	})

	t.Run("update with correct etag succeeds even when required", func(t *testing.T) {
		b := &nib.Nib{
			ID:     "etag-req-test-3",
			Title:  "ETag Required Test",
			Status: "todo",
		}
		if err := core.Create(b); err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		currentETag := b.ETag()
		b.Title = "Success"
		err := core.Update(b, &currentETag)
		if err != nil {
			t.Errorf("Update() with correct etag failed: %v", err)
		}
	})
}
func TestUpdateWithETagDebug(t *testing.T) {
	core, _ := setupTestCore(t)

	b := &nib.Nib{
		ID:     "etag-debug",
		Title:  "ETag Test",
		Status: "todo",
		Body:   "Original",
	}
	if err := core.Create(b); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	etagAfterCreate := b.ETag()
	t.Logf("ETag after create: %s", etagAfterCreate)

	// Get from core to see what's stored
	stored, _ := core.Get("etag-debug")
	storedEtag := stored.ETag()
	t.Logf("ETag of stored nib: %s", storedEtag)

	// Modify our local copy
	b.Title = "Updated"
	modifiedEtag := b.ETag()
	t.Logf("ETag of modified local nib: %s", modifiedEtag)

	// What will Update see?
	err := core.Update(b, &etagAfterCreate)
	if err != nil {
		t.Logf("Update failed: %v", err)
	}
}

// TestCreateValidatesEnums pins the core write-path chokepoint:
// Create must reject non-empty type/status/priority/estimate values that are not
// valid under the config, while still accepting the empty "unset -> default"
// sentinel and valid values. When no config is set, validation must no-op.
func TestCreateValidatesEnums(t *testing.T) {
	t.Run("rejects invalid enum values", func(t *testing.T) {
		cases := []struct {
			name string
			b    *nib.Nib
		}{
			{"invalid type", &nib.Nib{ID: "ct1", Title: "T", Status: "todo", Type: "epicc"}},
			{"invalid status", &nib.Nib{ID: "ct2", Title: "T", Status: "banana"}},
			{"invalid priority", &nib.Nib{ID: "ct3", Title: "T", Status: "todo", Priority: "urgent"}},
			{"invalid estimate", &nib.Nib{ID: "ct4", Title: "T", Status: "todo", Estimate: "2h"}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				core, _ := setupTestCore(t)
				if err := core.Create(tc.b); err == nil {
					t.Fatalf("Create(%+v) = nil error, want validation error", tc.b)
				}
				if _, err := core.Get(tc.b.ID); !errors.Is(err, ErrNotFound) {
					t.Errorf("nib persisted despite validation failure (Get err = %v)", err)
				}
			})
		}
	})

	t.Run("accepts empty enum sentinels", func(t *testing.T) {
		core, _ := setupTestCore(t)
		if err := core.Create(&nib.Nib{ID: "cempty", Title: "Empty"}); err != nil {
			t.Fatalf("Create() with empty enums error = %v, want nil", err)
		}
	})

	t.Run("accepts valid enum values", func(t *testing.T) {
		core, _ := setupTestCore(t)
		b := &nib.Nib{ID: "cvalid", Title: "Valid", Status: "in-progress", Type: "bug", Priority: "high", Estimate: "l"}
		if err := core.Create(b); err != nil {
			t.Fatalf("Create() with valid enums error = %v, want nil", err)
		}
	})

	t.Run("no-op when config is nil", func(t *testing.T) {
		tmpDir := t.TempDir()
		nibsDir := filepath.Join(tmpDir, NibsDir)
		if err := os.MkdirAll(nibsDir, 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		core := New(nibsDir, nil)
		core.SetWarnWriter(nil)
		b := &nib.Nib{ID: "cnil", Title: "No Config", Status: "banana", Type: "epicc", Priority: "urgent", Estimate: "2h"}
		if err := core.Create(b); err != nil {
			t.Fatalf("Create() with nil config error = %v, want nil (no-op)", err)
		}
	})
}

// TestUpdateValidatesEnums pins the same chokepoint on the Update path: an
// invalid non-empty enum must be rejected and leave the stored nib untouched;
// empty sentinels and valid values are accepted; nil config no-ops.
func TestUpdateValidatesEnums(t *testing.T) {
	newValidNib := func(t *testing.T) *Core {
		t.Helper()
		core, _ := setupTestCore(t)
		if err := core.Create(&nib.Nib{
			ID: "uenum", Title: "Enum", Status: "todo", Type: "task", Priority: "normal", Estimate: "m",
		}); err != nil {
			t.Fatalf("seed Create() error = %v", err)
		}
		return core
	}

	t.Run("rejects invalid enum values", func(t *testing.T) {
		mutators := []struct {
			name  string
			apply func(*nib.Nib)
		}{
			{"invalid type", func(b *nib.Nib) { b.Type = "epicc" }},
			{"invalid status", func(b *nib.Nib) { b.Status = "banana" }},
			{"invalid priority", func(b *nib.Nib) { b.Priority = "urgent" }},
			{"invalid estimate", func(b *nib.Nib) { b.Estimate = "2h" }},
		}
		for _, m := range mutators {
			t.Run(m.name, func(t *testing.T) {
				core := newValidNib(t)
				b, err := core.GetForUpdate("uenum")
				if err != nil {
					t.Fatalf("GetForUpdate() error = %v", err)
				}
				m.apply(b)
				if err := core.Update(b, nil); err == nil {
					t.Fatalf("Update() = nil error, want validation error")
				}
				stored, _ := core.Get("uenum")
				if stored.Type != "task" || stored.Status != "todo" || stored.Priority != "normal" || stored.Estimate != "m" {
					t.Errorf("stored nib mutated on failed update: type=%q status=%q priority=%q estimate=%q",
						stored.Type, stored.Status, stored.Priority, stored.Estimate)
				}
			})
		}
	})

	t.Run("accepts empty enum sentinels", func(t *testing.T) {
		core := newValidNib(t)
		b, _ := core.GetForUpdate("uenum")
		b.Priority = ""
		b.Estimate = ""
		if err := core.Update(b, nil); err != nil {
			t.Fatalf("Update() clearing priority/estimate error = %v, want nil", err)
		}
	})

	t.Run("accepts valid enum values", func(t *testing.T) {
		core := newValidNib(t)
		b, _ := core.GetForUpdate("uenum")
		b.Type = "bug"
		b.Status = "in-progress"
		b.Priority = "high"
		b.Estimate = "xl"
		if err := core.Update(b, nil); err != nil {
			t.Fatalf("Update() with valid enums error = %v, want nil", err)
		}
	})

	t.Run("no-op when config is nil", func(t *testing.T) {
		tmpDir := t.TempDir()
		nibsDir := filepath.Join(tmpDir, NibsDir)
		if err := os.MkdirAll(nibsDir, 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		core := New(nibsDir, nil)
		core.SetWarnWriter(nil)
		if err := core.Create(&nib.Nib{ID: "un", Title: "No Config", Status: "todo"}); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		b, _ := core.GetForUpdate("un")
		b.Status = "banana"
		b.Type = "epicc"
		if err := core.Update(b, nil); err != nil {
			t.Fatalf("Update() with nil config error = %v, want nil (no-op)", err)
		}
	})
}

// TestLoadSluglessPrefixedFileKeepsFullID pins nibs-mccz: a slugless nib file
// whose id carries the configured prefix (nibs-x9z2.md) must load under its FULL
// prefixed id, not the pre-dash fragment. Every configured prefix ends in a dash,
// which the legacy single-dash filename parse mis-split — assigning id "nibs" to
// the file and stranding the real nib so no lookup by "nibs-x9z2" could find it.
func TestLoadSluglessPrefixedFileKeepsFullID(t *testing.T) {
	const fullID = "nibs-x9z2"

	// Persist a real, slugless prefixed nib through one core so the on-disk file is
	// {id}.md (nibs-x9z2.md) rather than hand-rolled YAML.
	core, nibsDir := mustLoadPrefixedCore(t)
	if err := core.Create(&nib.Nib{ID: fullID, Slug: "", Title: "Slugless", Status: "todo"}); err != nil {
		t.Fatalf("create slugless nib: %v", err)
	}
	// Precondition: the file on disk really is the slugless {id}.md form.
	sluglessAbs := filepath.Join(nibsDir, nib.BuildFilename(fullID, ""))
	if _, err := os.Stat(sluglessAbs); err != nil {
		t.Fatalf("precondition: expected slugless file %s on disk: %v", sluglessAbs, err)
	}

	// A fresh core loading the same directory from disk must recover the FULL
	// prefixed id (a second core so the load is a real cold read, not the cache).
	core2 := New(nibsDir, config.DefaultWithPrefix("nibs-"))
	core2.SetWarnWriter(nil)
	if err := core2.Load(); err != nil {
		t.Fatalf("reader load: %v", err)
	}
	b, err := core2.Get(fullID)
	if err != nil {
		t.Fatalf("nib lost after reload — Get(%q) = %v (slugless prefixed id misparsed on load)", fullID, err)
	}
	if b.ID != fullID {
		t.Errorf("loaded ID = %q, want %q", b.ID, fullID)
	}
}
