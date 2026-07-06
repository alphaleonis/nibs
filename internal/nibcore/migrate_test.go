package nibcore

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
)

// writeNibFile writes a raw nib markdown file to disk for testing migration.
func writeNibFile(t *testing.T, dir, filename, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write nib file %s: %v", filename, err)
	}
}

func TestCheckBrokenDocuments(t *testing.T) {
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, NibsDir)
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a valid document
	if err := os.WriteFile(filepath.Join(tmpDir, "existing.md"), []byte("# Exists"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	core := New(nibsDir, cfg)
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Create nib with one valid and one broken document link
	b := &nib.Nib{
		ID:        "doc-check-1",
		Title:     "Doc Check",
		Status:    "todo",
		Version:   1,
		Documents: []string{"existing.md", "nonexistent.md"},
	}
	if err := core.Create(b); err != nil {
		t.Fatal(err)
	}

	t.Run("detects broken document links", func(t *testing.T) {
		result := core.CheckAllLinks()
		if len(result.BrokenDocuments) != 1 {
			t.Fatalf("BrokenDocuments count = %d, want 1", len(result.BrokenDocuments))
		}
		if result.BrokenDocuments[0].NibID != "doc-check-1" {
			t.Errorf("BrokenDocuments[0].NibID = %q, want doc-check-1", result.BrokenDocuments[0].NibID)
		}
		if result.BrokenDocuments[0].Path != "nonexistent.md" {
			t.Errorf("BrokenDocuments[0].Path = %q, want nonexistent.md", result.BrokenDocuments[0].Path)
		}
	})

	t.Run("fix removes broken document links", func(t *testing.T) {
		fixed, err := core.FixBrokenLinks()
		if err != nil {
			t.Fatalf("FixBrokenLinks() error: %v", err)
		}
		if fixed != 1 {
			t.Errorf("fixed = %d, want 1", fixed)
		}
		updated, _ := core.Get("doc-check-1")
		if len(updated.Documents) != 1 {
			t.Errorf("Documents count = %d, want 1", len(updated.Documents))
		}
		if len(updated.Documents) > 0 && updated.Documents[0] != "existing.md" {
			t.Errorf("Documents = %v, want [existing.md]", updated.Documents)
		}
	})
}

// assertDeferredConverged checks that the nib loaded under id has been
// normalized to priority "low" both in memory and on disk (file at
// nibsDir/filename), and that a read-etag → if-match Update round-trips without
// an ETagMismatchError. The on-disk and etag checks are the real
// regression-catchers: the in-memory check alone passes even with the
// persistence reverted, since nib.Parse normalizes in memory regardless.
func assertDeferredConverged(t *testing.T, core *Core, id, nibsDir, filename string) {
	t.Helper()

	b, err := core.Get(id)
	if err != nil {
		t.Fatalf("Get(%q) error: %v", id, err)
	}
	if b.Priority != "low" {
		t.Errorf("in-memory Priority = %q, want %q", b.Priority, "low")
	}

	// On disk: the normalization must be persisted (not deferred to the next
	// write), so disk == memory immediately.
	diskBytes, err := os.ReadFile(filepath.Join(nibsDir, filename))
	if err != nil {
		t.Fatalf("reading migrated file: %v", err)
	}
	disk := string(diskBytes)
	if strings.Contains(disk, "deferred") {
		t.Errorf("on-disk file still contains 'deferred':\n%s", disk)
	}
	if !strings.Contains(disk, "priority: low") {
		t.Errorf("on-disk file missing 'priority: low':\n%s", disk)
	}

	// Read etag → if-match Update round-trip must succeed (no ETagMismatchError).
	// The read path exposes the in-memory nib's ETag(); the write path validates
	// against the on-disk etag. Persisting the normalization makes them agree.
	readETag := b.ETag()
	updated := b.Clone()
	updated.Title = b.Title + " (edited)"
	if err := core.Update(updated, &readETag); err != nil {
		var mismatch *ETagMismatchError
		if errors.As(err, &mismatch) {
			t.Fatalf("if-match Update returned ETagMismatchError (etag divergence not fixed): provided=%s current=%s",
				mismatch.Provided, mismatch.Current)
		}
		t.Fatalf("if-match Update failed: %v", err)
	}
}

func TestDeferredPriorityReconcile(t *testing.T) {
	t.Run("persists deferred->low at load and converges if-match etag", func(t *testing.T) {
		tmpDir := t.TempDir()
		nibsDir := filepath.Join(tmpDir, NibsDir)
		if err := os.MkdirAll(nibsDir, 0755); err != nil {
			t.Fatal(err)
		}

		// A canonically-formatted v1 nib carrying the removed `priority: deferred`.
		const filename = "def1--legacy.md"
		writeNibFile(t, nibsDir, filename, `---
version: 1
title: Legacy Deferred
status: todo
priority: deferred
---
`)

		// A control sibling with a valid priority: the load-time persistence must
		// rewrite ONLY the migrated nib. Capture its exact bytes now — if the
		// PriorityMigrated() guard were dropped/inverted (rewrite every nib on
		// load), loading would re-render this file (adding the `# sib2` id line),
		// changing its bytes and failing the assertion below.
		const siblingFile = "sib2--control.md"
		writeNibFile(t, nibsDir, siblingFile, `---
version: 1
title: Control Sibling
status: todo
priority: normal
---
`)
		siblingBefore, err := os.ReadFile(filepath.Join(nibsDir, siblingFile))
		if err != nil {
			t.Fatalf("reading sibling file: %v", err)
		}

		cfg := config.Default()
		core := New(nibsDir, cfg)
		core.SetWarnWriter(nil)
		if err := core.Load(); err != nil {
			t.Fatalf("Load() error: %v", err)
		}

		assertDeferredConverged(t, core, "def1", nibsDir, filename)

		// The control sibling must be byte-for-byte untouched on disk.
		siblingAfter, err := os.ReadFile(filepath.Join(nibsDir, siblingFile))
		if err != nil {
			t.Fatalf("re-reading sibling file: %v", err)
		}
		if !bytes.Equal(siblingBefore, siblingAfter) {
			t.Errorf("control sibling was rewritten at load (only migrated nibs should be persisted)\nbefore:\n%s\nafter:\n%s",
				siblingBefore, siblingAfter)
		}
		if sib, err := core.Get("sib2"); err != nil {
			t.Fatalf("Get(sib2) error: %v", err)
		} else if sib.Priority != "normal" {
			t.Errorf("control sibling Priority = %q, want %q", sib.Priority, "normal")
		}
	})

	t.Run("normalizes deferred->low in memory on the watcher path without persisting", func(t *testing.T) {
		core, nibsDir := setupTestCore(t)

		if err := core.StartWatching(); err != nil {
			t.Fatalf("StartWatching() error: %v", err)
		}
		defer func() { _ = core.Unwatch() }()

		ch, unsub := core.Subscribe()
		defer unsub()

		// Give the watcher time to start.
		time.Sleep(50 * time.Millisecond)

		// A legacy `priority: deferred` file that first appears AFTER the initial
		// Load (e.g. a git pull in the separate .nibs repo). This never goes
		// through loadFromDisk, only through handleChanges — which reconciles in
		// memory only and must NOT write to disk (an unguarded self-write would
		// clobber external writes, dirty the separate .nibs git tree, and emit a
		// spurious content-free event).
		const filename = "wdf1--watched.md"
		const raw = `---
version: 1
title: Watched Deferred
status: todo
priority: deferred
---
`
		writeNibFile(t, nibsDir, filename, raw)

		// Wait for the watcher to ingest the file (exactly one batch: a Created
		// event carrying the in-memory-normalized nib).
		var batch []NibEvent
		select {
		case batch = <-ch:
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for watcher event")
		}
		if len(batch) != 1 {
			t.Fatalf("first event batch has %d events, want 1: %+v", len(batch), batch)
		}
		if batch[0].Type != EventCreated {
			t.Errorf("event type = %v, want EventCreated", batch[0].Type)
		}
		if batch[0].Nib == nil || batch[0].Nib.Priority != "low" {
			t.Errorf("event nib priority = %+v, want in-memory normalization to low", batch[0].Nib)
		}

		// No SECOND batch: a self-write (persisting the migration) would fire a
		// spurious content-free Updated event. Assert none arrives within a
		// debounce window plus margin.
		select {
		case extra := <-ch:
			t.Fatalf("unexpected second event batch (watcher self-write?): %+v", extra)
		case <-time.After(300 * time.Millisecond):
		}

		// In memory: normalized to low.
		b, err := core.Get("wdf1")
		if err != nil {
			t.Fatalf("Get(wdf1) error: %v", err)
		}
		if b.Priority != "low" {
			t.Errorf("in-memory Priority = %q, want %q", b.Priority, "low")
		}

		// On disk: UNCHANGED. The watcher path does not persist, so the raw
		// `deferred` bytes remain until the next explicit Update/Load.
		diskBytes, err := os.ReadFile(filepath.Join(nibsDir, filename))
		if err != nil {
			t.Fatalf("reading watched file: %v", err)
		}
		if string(diskBytes) != raw {
			t.Errorf("on-disk file was rewritten by the watcher path; want unchanged bytes.\n got:\n%s\nwant:\n%s", diskBytes, raw)
		}
	})
}

func TestMigrateV0ToV1(t *testing.T) {
	t.Run("converts blocking to blockedBy on targets", func(t *testing.T) {
		tmpDir := t.TempDir()
		nibsDir := filepath.Join(tmpDir, NibsDir)
		if err := os.MkdirAll(nibsDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Write v0 nib A that blocks B
		writeNibFile(t, nibsDir, "aaa1--blocker.md", `---
title: Blocker
status: todo
blocking:
    - bbb2
---
`)
		// Write v0 nib B (no blocking)
		writeNibFile(t, nibsDir, "bbb2--blocked.md", `---
title: Blocked
status: todo
---
`)

		cfg := config.Default()
		core := New(nibsDir, cfg)
		core.SetWarnWriter(nil)
		if err := core.Load(); err != nil {
			t.Fatalf("Load() error: %v", err)
		}

		// After migration:
		// - A should have version 1, no blocking field
		// - B should have version 1, blockedBy=[aaa1]
		a, _ := core.Get("aaa1")
		b, _ := core.Get("bbb2")

		if a.Version != 1 {
			t.Errorf("A.Version = %d, want 1", a.Version)
		}
		if len(a.Blocking) != 0 {
			t.Errorf("A.Blocking should be cleared after migration, got %v", a.Blocking)
		}
		if b.Version != 1 {
			t.Errorf("B.Version = %d, want 1", b.Version)
		}
		if !b.IsBlockedBy("aaa1") {
			t.Errorf("B.BlockedBy should contain aaa1, got %v", b.BlockedBy)
		}

		// Verify persisted to disk
		core2 := New(nibsDir, cfg)
		core2.SetWarnWriter(nil)
		if err := core2.Load(); err != nil {
			t.Fatalf("second Load() error: %v", err)
		}
		b2, _ := core2.Get("bbb2")
		if !b2.IsBlockedBy("aaa1") {
			t.Errorf("B.BlockedBy not persisted, got %v", b2.BlockedBy)
		}
		if b2.Version != 1 {
			t.Errorf("B.Version not persisted, got %d", b2.Version)
		}
	})

	t.Run("handles both blocking and blockedBy present", func(t *testing.T) {
		tmpDir := t.TempDir()
		nibsDir := filepath.Join(tmpDir, NibsDir)
		if err := os.MkdirAll(nibsDir, 0755); err != nil {
			t.Fatal(err)
		}

		// A blocks B, and B already has blockedBy=[A] (dual-side legacy)
		writeNibFile(t, nibsDir, "aaa1.md", `---
title: Blocker
status: todo
blocking:
    - bbb2
---
`)
		writeNibFile(t, nibsDir, "bbb2.md", `---
title: Blocked
status: todo
blocked_by:
    - aaa1
---
`)

		cfg := config.Default()
		core := New(nibsDir, cfg)
		core.SetWarnWriter(nil)
		if err := core.Load(); err != nil {
			t.Fatalf("Load() error: %v", err)
		}

		b, _ := core.Get("bbb2")
		// Should deduplicate: aaa1 appears only once
		count := 0
		for _, id := range b.BlockedBy {
			if id == "aaa1" {
				count++
			}
		}
		if count != 1 {
			t.Errorf("B.BlockedBy should have aaa1 exactly once, got %v", b.BlockedBy)
		}
	})

	t.Run("handles blocking references nonexistent nib", func(t *testing.T) {
		tmpDir := t.TempDir()
		nibsDir := filepath.Join(tmpDir, NibsDir)
		if err := os.MkdirAll(nibsDir, 0755); err != nil {
			t.Fatal(err)
		}

		var warnings strings.Builder
		writeNibFile(t, nibsDir, "aaa1.md", `---
title: Blocker
status: todo
blocking:
    - nonexistent
---
`)

		cfg := config.Default()
		core := New(nibsDir, cfg)
		core.SetWarnWriter(&warnings)
		if err := core.Load(); err != nil {
			t.Fatalf("Load() error: %v", err)
		}

		a, _ := core.Get("aaa1")
		if a.Version != 1 {
			t.Errorf("A.Version = %d, want 1", a.Version)
		}
		if len(a.Blocking) != 0 {
			t.Errorf("A.Blocking should be cleared, got %v", a.Blocking)
		}
		if !strings.Contains(warnings.String(), "nonexistent") {
			t.Errorf("expected warning about nonexistent nib, got %q", warnings.String())
		}
	})

	t.Run("bumps version on nibs with no blocking", func(t *testing.T) {
		tmpDir := t.TempDir()
		nibsDir := filepath.Join(tmpDir, NibsDir)
		if err := os.MkdirAll(nibsDir, 0755); err != nil {
			t.Fatal(err)
		}

		writeNibFile(t, nibsDir, "aaa1.md", `---
title: Simple Nib
status: todo
---
`)

		cfg := config.Default()
		core := New(nibsDir, cfg)
		core.SetWarnWriter(nil)
		if err := core.Load(); err != nil {
			t.Fatalf("Load() error: %v", err)
		}

		a, _ := core.Get("aaa1")
		if a.Version != 1 {
			t.Errorf("Version = %d, want 1", a.Version)
		}
	})

	t.Run("idempotent: v1 nibs not re-migrated", func(t *testing.T) {
		tmpDir := t.TempDir()
		nibsDir := filepath.Join(tmpDir, NibsDir)
		if err := os.MkdirAll(nibsDir, 0755); err != nil {
			t.Fatal(err)
		}

		writeNibFile(t, nibsDir, "aaa1.md", `---
version: 1
title: Already Migrated
status: todo
blocked_by:
    - bbb2
---
`)
		writeNibFile(t, nibsDir, "bbb2.md", `---
version: 1
title: Other Nib
status: todo
---
`)

		cfg := config.Default()
		core := New(nibsDir, cfg)
		core.SetWarnWriter(nil)
		if err := core.Load(); err != nil {
			t.Fatalf("Load() error: %v", err)
		}

		a, _ := core.Get("aaa1")
		if a.Version != 1 {
			t.Errorf("Version = %d, want 1", a.Version)
		}
		if len(a.BlockedBy) != 1 || a.BlockedBy[0] != "bbb2" {
			t.Errorf("BlockedBy = %v, want [bbb2]", a.BlockedBy)
		}
	})

	t.Run("best-effort persistence: Load succeeds when the migration cannot be written", func(t *testing.T) {
		// Forcing the write failure: the loaded file is chmod'd read-only so
		// saveToDisk's write (O_WRONLY|O_TRUNC) fails while loadNib's read
		// (O_RDONLY) still succeeds. This is deterministic and root-independent
		// on the CI matrix: Linux non-root honours the 0444 mode, and Windows
		// honours the read-only attribute (os.Geteuid() returns -1 there, so the
		// root guard below never fires on Windows).
		if os.Geteuid() == 0 {
			t.Skip("running as root bypasses file-mode write protection, so the saveToDisk failure can't be forced")
		}

		tmpDir := t.TempDir()
		nibsDir := filepath.Join(tmpDir, NibsDir)
		if err := os.MkdirAll(nibsDir, 0755); err != nil {
			t.Fatal(err)
		}

		// A legacy v0 nib (no `version:` field). Loading migrates it to v1 in
		// memory and marks it dirty for persistence.
		const filename = "leg1--legacy.md"
		const raw = `---
title: Legacy V0
status: todo
---
`
		writeNibFile(t, nibsDir, filename, raw)
		path := filepath.Join(nibsDir, filename)

		if err := os.Chmod(path, 0444); err != nil {
			t.Fatal(err)
		}
		// Restore write permission so TempDir cleanup (and Windows deletion of a
		// read-only file) succeeds.
		t.Cleanup(func() { _ = os.Chmod(path, 0644) })

		var warnings strings.Builder
		cfg := config.Default()
		core := New(nibsDir, cfg)
		core.SetWarnWriter(&warnings)

		// Load must SUCCEED even though the migration cannot be persisted: a
		// legacy nib on a read-only/full/permission-restricted .nibs must not
		// brick every command.
		if err := core.Load(); err != nil {
			t.Fatalf("Load() returned error on unwritable .nibs; load-time migration persistence must be best-effort: %v", err)
		}

		// In memory: migrated to v1 regardless of the persistence failure.
		b, err := core.Get("leg1")
		if err != nil {
			t.Fatalf("Get(leg1) error: %v", err)
		}
		if b.Version != 1 {
			t.Errorf("in-memory Version = %d, want 1", b.Version)
		}

		// On disk: UNCHANGED. The write failed, so the original v0 bytes remain;
		// on-disk convergence waits for the next successful write.
		diskBytes, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading nib file: %v", err)
		}
		if string(diskBytes) != raw {
			t.Errorf("on-disk file changed despite an unwritable target:\n got:\n%s\nwant:\n%s", diskBytes, raw)
		}

		// A warning about the failed persistence should have been logged.
		if !strings.Contains(warnings.String(), "leg1") {
			t.Errorf("expected a warning mentioning the un-persisted nib, got %q", warnings.String())
		}
	})

	t.Run("defers migration when a blocking target's file was skipped (no edge loss)", func(t *testing.T) {
		// nibs-r3y1 review #2: a v0 nib A with blocking:[B] must NOT lose that edge
		// when B's file is unparseable at load time. Pre-fix, loadFromDisk skipped B
		// and migrateV0ToV1 still cleared+persisted A with `blocking:` erased —
		// irrecoverable. A must instead stay v0 with Blocking intact (memory AND
		// disk) so a later clean Load completes the migration.
		tmpDir := t.TempDir()
		nibsDir := filepath.Join(tmpDir, NibsDir)
		if err := os.MkdirAll(nibsDir, 0755); err != nil {
			t.Fatal(err)
		}

		const aRaw = `---
title: Blocker A
status: todo
blocking:
    - bbb2
---

Body A.
`
		writeNibFile(t, nibsDir, "aaa1--blocker.md", aRaw)
		// B is present on disk but UNPARSEABLE (duplicate modeled key), so
		// loadFromDisk skips it. Its ID (bbb2) is still derivable from the filename.
		writeNibFile(t, nibsDir, "bbb2--blocked.md", `---
title: First
title: Second
status: todo
---

Body B.
`)

		cfg := config.Default()
		core := New(nibsDir, cfg)
		core.SetWarnWriter(nil)
		if err := core.Load(); err != nil {
			t.Fatalf("Load() error: %v", err)
		}

		// A must remain UNMIGRATED: still v0 with its blocking edge intact in memory.
		a, err := core.Get("aaa1")
		if err != nil {
			t.Fatalf("Get(aaa1) error: %v", err)
		}
		if a.Version != 0 {
			t.Errorf("A.Version = %d, want 0 (migration must be deferred while target skipped)", a.Version)
		}
		if len(a.Blocking) != 1 || a.Blocking[0] != "bbb2" {
			t.Errorf("A.Blocking = %v, want [bbb2] (edge must not be dropped)", a.Blocking)
		}

		// On disk: A's file must be byte-for-byte untouched (the `blocking:` line
		// must NOT have been erased by a clear+persist).
		aDisk, err := os.ReadFile(filepath.Join(nibsDir, "aaa1--blocker.md"))
		if err != nil {
			t.Fatalf("reading A file: %v", err)
		}
		if string(aDisk) != aRaw {
			t.Errorf("A's file was rewritten (edge at risk); want unchanged bytes.\n got:\n%s\nwant:\n%s", aDisk, aRaw)
		}

		// Repair B, then a fresh Load must complete the deferred migration: the edge
		// lands on B (B.blockedBy contains aaa1) and A becomes v1.
		writeNibFile(t, nibsDir, "bbb2--blocked.md", `---
title: Blocked B
status: todo
---

Body B.
`)
		core2 := New(nibsDir, cfg)
		core2.SetWarnWriter(nil)
		if err := core2.Load(); err != nil {
			t.Fatalf("second Load() error: %v", err)
		}
		a2, err := core2.Get("aaa1")
		if err != nil {
			t.Fatalf("Get(aaa1) after repair: %v", err)
		}
		if a2.Version != 1 {
			t.Errorf("after repair A.Version = %d, want 1 (deferred migration must complete)", a2.Version)
		}
		if len(a2.Blocking) != 0 {
			t.Errorf("after repair A.Blocking = %v, want cleared", a2.Blocking)
		}
		b2, err := core2.Get("bbb2")
		if err != nil {
			t.Fatalf("Get(bbb2) after repair: %v", err)
		}
		if !b2.IsBlockedBy("aaa1") {
			t.Errorf("after repair B.BlockedBy = %v, want to contain aaa1 (edge landed)", b2.BlockedBy)
		}
	})

	t.Run("chain A->B->C: deferred middle nib still receives sibling blockedBy transfer (lossless convergence)", func(t *testing.T) {
		// nibs-r3y1 review #1 (FINAL pass): in a v0 chain A blocking:[B],
		// B blocking:[C] where only C's file is skipped, B is deferred for its OWN
		// edge (B→C) yet A's migration still transfers A→B onto B and re-persists
		// B. This contradicts a naive "deferred nib's file is untouched" reading,
		// but it is CORRECT and lossless: the A→B edge survives on disk, B's own
		// blocking:[C] stays intact, and a later clean Load converges. This test
		// pins that actual behavior so a future refactor of the dirty-tracking loop
		// (e.g. "exclude deferred targets from dirty") that would silently drop the
		// A→B edge fails CI.
		tmpDir := t.TempDir()
		nibsDir := filepath.Join(tmpDir, NibsDir)
		if err := os.MkdirAll(nibsDir, 0755); err != nil {
			t.Fatal(err)
		}

		// A --blocking--> B (bbb2)
		writeNibFile(t, nibsDir, "aaa1--chain-a.md", `---
title: Chain A
status: todo
blocking:
    - bbb2
---

Body A.
`)
		// B --blocking--> C (ccc3)
		const bRaw = `---
title: Chain B
status: todo
blocking:
    - ccc3
---

Body B.
`
		writeNibFile(t, nibsDir, "bbb2--chain-b.md", bRaw)
		// C is present on disk but UNPARSEABLE (duplicate modeled key), so
		// loadFromDisk skips it. Its ID (ccc3) is still derivable from the filename,
		// so B's blocking:[ccc3] target is in the skipped set → B is deferred.
		writeNibFile(t, nibsDir, "ccc3--chain-c.md", `---
title: First
title: Second
status: todo
---

Body C.
`)

		cfg := config.Default()
		core := New(nibsDir, cfg)
		core.SetWarnWriter(nil)
		if err := core.Load(); err != nil {
			t.Fatalf("Load() error: %v", err)
		}

		// A migrated: v1, blocking cleared, and its edge landed on B.
		a, err := core.Get("aaa1")
		if err != nil {
			t.Fatalf("Get(aaa1) error: %v", err)
		}
		if a.Version != 1 {
			t.Errorf("A.Version = %d, want 1 (A's target B loaded fine, so A migrates)", a.Version)
		}
		if len(a.Blocking) != 0 {
			t.Errorf("A.Blocking = %v, want cleared", a.Blocking)
		}

		// B deferred for its OWN edge: still v0 with blocking:[C] intact...
		b, err := core.Get("bbb2")
		if err != nil {
			t.Fatalf("Get(bbb2) error: %v", err)
		}
		if b.Version != 0 {
			t.Errorf("B.Version = %d, want 0 (B's target C skipped, so B's own migration is deferred)", b.Version)
		}
		if len(b.Blocking) != 1 || b.Blocking[0] != "ccc3" {
			t.Errorf("B.Blocking = %v, want [ccc3] (deferred edge must stay intact)", b.Blocking)
		}
		// ...yet B carries the A->B transfer from A's completed migration.
		if !b.IsBlockedBy("aaa1") {
			t.Errorf("B.BlockedBy = %v, want to contain aaa1 (A's edge transfer)", b.BlockedBy)
		}

		// On disk, B was re-persisted by A's migration: version:0, blocking:[ccc3]
		// intact, PLUS the new blocked_by:[aaa1]. This is the "extra persist" the
		// method doc describes — lossless, and the thing that preserves A->B.
		bDiskBytes, err := os.ReadFile(filepath.Join(nibsDir, "bbb2--chain-b.md"))
		if err != nil {
			t.Fatalf("reading B file: %v", err)
		}
		bDisk := string(bDiskBytes)
		if !strings.Contains(bDisk, "blocked_by:") || !strings.Contains(bDisk, "aaa1") {
			t.Errorf("B's on-disk file missing the transferred blocked_by:[aaa1] (A->B edge would be lost on restart):\n%s", bDisk)
		}
		if !strings.Contains(bDisk, "blocking:") || !strings.Contains(bDisk, "ccc3") {
			t.Errorf("B's on-disk file lost its own blocking:[ccc3] edge:\n%s", bDisk)
		}
		if !strings.Contains(bDisk, "version: 0") {
			t.Errorf("B's on-disk file should remain version: 0 (own migration deferred):\n%s", bDisk)
		}

		// Repair C, then a fresh Load must complete B's deferred migration WITHOUT
		// losing A->B: B->v1 with blocking cleared, C.blockedBy gains B, and
		// B.blockedBy still contains A throughout.
		writeNibFile(t, nibsDir, "ccc3--chain-c.md", `---
title: Chain C
status: todo
---

Body C.
`)
		core2 := New(nibsDir, cfg)
		core2.SetWarnWriter(nil)
		if err := core2.Load(); err != nil {
			t.Fatalf("second Load() error: %v", err)
		}

		a2, err := core2.Get("aaa1")
		if err != nil {
			t.Fatalf("Get(aaa1) after repair: %v", err)
		}
		if a2.Version != 1 {
			t.Errorf("after repair A.Version = %d, want 1 (unchanged)", a2.Version)
		}

		b2, err := core2.Get("bbb2")
		if err != nil {
			t.Fatalf("Get(bbb2) after repair: %v", err)
		}
		if b2.Version != 1 {
			t.Errorf("after repair B.Version = %d, want 1 (deferred migration completes)", b2.Version)
		}
		if len(b2.Blocking) != 0 {
			t.Errorf("after repair B.Blocking = %v, want cleared", b2.Blocking)
		}
		if !b2.IsBlockedBy("aaa1") {
			t.Errorf("after repair B.BlockedBy = %v, want to still contain aaa1 (A->B preserved throughout)", b2.BlockedBy)
		}

		c2, err := core2.Get("ccc3")
		if err != nil {
			t.Fatalf("Get(ccc3) after repair: %v", err)
		}
		if !c2.IsBlockedBy("bbb2") {
			t.Errorf("after repair C.BlockedBy = %v, want to contain bbb2 (B->C edge landed)", c2.BlockedBy)
		}
	})

	t.Run("mixed v0 and v1 directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		nibsDir := filepath.Join(tmpDir, NibsDir)
		if err := os.MkdirAll(nibsDir, 0755); err != nil {
			t.Fatal(err)
		}

		// v0 nib that blocks a v1 nib
		writeNibFile(t, nibsDir, "v0nib.md", `---
title: V0 Nib
status: todo
blocking:
    - v1nib
---
`)
		// v1 nib (already migrated)
		writeNibFile(t, nibsDir, "v1nib.md", `---
version: 1
title: V1 Nib
status: todo
---
`)

		cfg := config.Default()
		core := New(nibsDir, cfg)
		core.SetWarnWriter(nil)
		if err := core.Load(); err != nil {
			t.Fatalf("Load() error: %v", err)
		}

		// All nibs should be v1 after load
		v0, _ := core.Get("v0nib")
		v1, _ := core.Get("v1nib")

		if v0.Version != 1 {
			t.Errorf("v0nib.Version = %d, want 1", v0.Version)
		}
		if v1.Version != 1 {
			t.Errorf("v1nib.Version = %d, want 1", v1.Version)
		}
		// v1nib should have v0nib in its blockedBy (from migration)
		if !v1.IsBlockedBy("v0nib") {
			t.Errorf("v1nib.BlockedBy should contain v0nib, got %v", v1.BlockedBy)
		}
	})
}
