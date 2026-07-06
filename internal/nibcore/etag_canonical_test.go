package nibcore

import (
	"encoding/hex"
	"errors"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
)

// setupLoadedCore builds a Core over an existing nibsDir and performs the initial
// bulk Load (no file watcher). Mirrors the setup used by the migration tests.
func setupLoadedCore(t *testing.T, nibsDir string) *Core {
	t.Helper()
	cfg := config.Default()
	core := New(nibsDir, cfg)
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	return core
}

func setupNibsDir(t *testing.T) string {
	t.Helper()
	nibsDir := filepath.Join(t.TempDir(), NibsDir)
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatalf("failed to create test .nibs dir: %v", err)
	}
	return nibsDir
}

const (
	canonEtagFile = "etagcanon1--drift.md"

	// canonEtagFile written in canonical form. `type` is written explicitly, as
	// every nib produced by the app carries it (the CreateNib resolver always
	// sets type), so loadNib applies no in-memory synthesis on top.
	canonEtagCanonical = `---
version: 1
title: Canonical Etag
status: todo
type: task
priority: normal
created_at: 2026-01-02T03:04:05Z
updated_at: 2026-01-02T03:04:05Z
---

Body paragraph.
`

	// Same logical fields as canonEtagCanonical, but with reordered YAML keys,
	// extra intra-line whitespace, and no leading id comment: benign formatting
	// drift that parses to an identical nib but has different raw bytes.
	canonEtagReordered = `---
status: todo
title: Canonical Etag
updated_at: 2026-01-02T03:04:05Z
priority:    normal
type:   task
version: 1
created_at: 2026-01-02T03:04:05Z
---

Body paragraph.
`
)

// TestComputeStoredETagCanonical verifies that the stored etag hashes the
// CANONICAL render of the on-disk file (parse -> ETag()), not raw disk bytes,
// so an ETag()-derived if-match survives benign formatting drift yet still
// detects genuine content divergence.
func TestComputeStoredETagCanonical(t *testing.T) {
	// Case 1: benign byte drift on disk does not change the stored etag.
	t.Run("benign byte drift keeps stored etag equal to in-memory ETag", func(t *testing.T) {
		nibsDir := setupNibsDir(t)
		writeNibFile(t, nibsDir, canonEtagFile, canonEtagCanonical)
		core := setupLoadedCore(t, nibsDir)

		b, err := core.Get("etagcanon1")
		if err != nil {
			t.Fatalf("Get() error: %v", err)
		}
		want := b.ETag()

		// Overwrite disk with cosmetically-drifted bytes that parse identically.
		writeNibFile(t, nibsDir, canonEtagFile, canonEtagReordered)

		got, err := core.CurrentETag("etagcanon1")
		if err != nil {
			t.Fatalf("CurrentETag() error: %v", err)
		}
		if got != want {
			t.Errorf("stored etag diverged on benign drift: got %s, want %s (in-memory ETag)", got, want)
		}
	})

	// Case 2: ETag()-based Update succeeds across benign drift, then fails on
	// genuine divergence.
	t.Run("ETag()-based Update succeeds across drift and fails on divergence", func(t *testing.T) {
		nibsDir := setupNibsDir(t)
		writeNibFile(t, nibsDir, canonEtagFile, canonEtagCanonical)
		core := setupLoadedCore(t, nibsDir)

		b, err := core.Get("etagcanon1")
		if err != nil {
			t.Fatalf("Get() error: %v", err)
		}
		ifMatch := b.ETag()

		// Benign drift on disk must not block an ETag()-derived if-match Update.
		writeNibFile(t, nibsDir, canonEtagFile, canonEtagReordered)

		updated := b.Clone()
		updated.Title = "Canonical Etag (edited)"
		if err := core.Update(updated, &ifMatch); err != nil {
			var mismatch *ETagMismatchError
			if errors.As(err, &mismatch) {
				t.Fatalf("if-match Update mismatched across benign drift: provided=%s current=%s", mismatch.Provided, mismatch.Current)
			}
			t.Fatalf("if-match Update failed: %v", err)
		}

		// Now simulate a genuine concurrent external edit on disk (different
		// title AND body), then attempt an Update with the now-stale if-match.
		writeNibFile(t, nibsDir, canonEtagFile, `---
version: 1
title: Externally Changed
status: todo
priority: normal
created_at: 2026-01-02T03:04:05Z
updated_at: 2026-01-02T03:04:05Z
---

Externally rewritten body.
`)
		stale := ifMatch // the original ETag() from before the divergence
		again := updated.Clone()
		again.Title = "My Concurrent Edit"
		err = core.Update(again, &stale)
		var mismatch *ETagMismatchError
		if !errors.As(err, &mismatch) {
			t.Fatalf("expected *ETagMismatchError on genuine divergence, got %T: %v", err, err)
		}
	})

	// Case 3: legacy `priority: deferred` on disk (memory normalizes to `low`)
	// no longer causes a false etag mismatch.
	t.Run("priority deferred on disk matches low in memory", func(t *testing.T) {
		nibsDir := setupNibsDir(t)
		// Start from a canonical `low` nib so the in-memory value is low.
		const file = "defcanon1--legacy.md"
		writeNibFile(t, nibsDir, file, `---
version: 1
title: Legacy Deferred
status: todo
type: task
priority: low
created_at: 2026-01-02T03:04:05Z
updated_at: 2026-01-02T03:04:05Z
---
`)
		core := setupLoadedCore(t, nibsDir)

		b, err := core.Get("defcanon1")
		if err != nil {
			t.Fatalf("Get() error: %v", err)
		}
		if b.Priority != "low" {
			t.Fatalf("in-memory Priority = %q, want low", b.Priority)
		}
		want := b.ETag()

		// Overwrite disk with the legacy `deferred` value (bypassing the
		// load-time write-back, so disk genuinely carries `deferred`).
		writeNibFile(t, nibsDir, file, `---
version: 1
title: Legacy Deferred
status: todo
type: task
priority: deferred
created_at: 2026-01-02T03:04:05Z
updated_at: 2026-01-02T03:04:05Z
---
`)
		diskBytes, err := os.ReadFile(filepath.Join(nibsDir, file))
		if err != nil {
			t.Fatalf("reading file: %v", err)
		}
		if !strings.Contains(string(diskBytes), "deferred") {
			t.Fatalf("precondition: on-disk file should contain 'deferred'")
		}

		got, err := core.CurrentETag("defcanon1")
		if err != nil {
			t.Fatalf("CurrentETag() error: %v", err)
		}
		if got != want {
			t.Errorf("stored etag for legacy `deferred` disk file = %s, want %s (in-memory `low` ETag)", got, want)
		}
	})

	// Case 4: genuine lost-update protection — a real divergence is detected AND
	// the divergent on-disk content is NOT silently overwritten by the stale
	// in-memory clone.
	t.Run("genuine divergence aborts and preserves disk content", func(t *testing.T) {
		nibsDir := setupNibsDir(t)
		writeNibFile(t, nibsDir, canonEtagFile, canonEtagCanonical)
		core := setupLoadedCore(t, nibsDir)

		b, err := core.Get("etagcanon1")
		if err != nil {
			t.Fatalf("Get() error: %v", err)
		}
		stale := b.ETag()

		// A concurrent external writer replaces the file's content entirely.
		const external = `---
version: 1
title: External Winner
status: in-progress
priority: high
created_at: 2026-01-02T03:04:05Z
updated_at: 2026-03-04T05:06:07Z
---

Content written by another process.
`
		writeNibFile(t, nibsDir, canonEtagFile, external)

		clone := b.Clone()
		clone.Title = "Stale Loser"
		clone.Body = "This must not overwrite the external content."
		err = core.Update(clone, &stale)
		var mismatch *ETagMismatchError
		if !errors.As(err, &mismatch) {
			t.Fatalf("expected *ETagMismatchError, got %T: %v", err, err)
		}

		// The on-disk file must still hold the external writer's content.
		after, err := os.ReadFile(filepath.Join(nibsDir, canonEtagFile))
		if err != nil {
			t.Fatalf("reading file after aborted update: %v", err)
		}
		if string(after) != external {
			t.Errorf("aborted update overwrote divergent disk content:\n got:\n%s\nwant:\n%s", after, external)
		}
	})

	// Case 5: bulk-reorder pre-validation property — CurrentETag equals the
	// in-memory ETag for a freshly loaded nib, survives benign drift, and detects
	// genuine divergence.
	t.Run("CurrentETag tracks canonical content for bulk-reorder pre-validation", func(t *testing.T) {
		nibsDir := setupNibsDir(t)
		writeNibFile(t, nibsDir, canonEtagFile, canonEtagCanonical)
		core := setupLoadedCore(t, nibsDir)

		b, err := core.Get("etagcanon1")
		if err != nil {
			t.Fatalf("Get() error: %v", err)
		}

		got, err := core.CurrentETag("etagcanon1")
		if err != nil {
			t.Fatalf("CurrentETag() error: %v", err)
		}
		if got != b.ETag() {
			t.Errorf("CurrentETag = %s, want in-memory ETag %s", got, b.ETag())
		}

		// Benign drift: still matches.
		writeNibFile(t, nibsDir, canonEtagFile, canonEtagReordered)
		got, err = core.CurrentETag("etagcanon1")
		if err != nil {
			t.Fatalf("CurrentETag() error: %v", err)
		}
		if got != b.ETag() {
			t.Errorf("CurrentETag after benign drift = %s, want %s", got, b.ETag())
		}

		// Genuine divergence: no longer matches.
		writeNibFile(t, nibsDir, canonEtagFile, `---
version: 1
title: Reordered Externally
status: todo
priority: normal
created_at: 2026-01-02T03:04:05Z
updated_at: 2026-01-02T03:04:05Z
---

Different body entirely.
`)
		got, err = core.CurrentETag("etagcanon1")
		if err != nil {
			t.Fatalf("CurrentETag() error: %v", err)
		}
		if got == b.ETag() {
			t.Errorf("CurrentETag should differ after genuine divergence, got %s == in-memory %s", got, b.ETag())
		}
	})
}

// TestComputeStoredETagUnparseableFailsClosed covers findings #1 and #5: when
// the on-disk file EXISTS but cannot be parsed (torn/partial write, git
// merge-conflict markers, YAML typo), an if-match Update must fail CLOSED with a
// distinct, NON-RECONCILABLE error (OnDiskUnparseableError) — carrying no
// reusable etag token — so a stale-but-parseable in-memory nib cannot overwrite
// the corrupt file, AND a client that blindly retries "with the server's current
// etag" still cannot clobber it (the single-shot #5 vulnerability).
func TestComputeStoredETagUnparseableFailsClosed(t *testing.T) {
	// A file that exists but nib.Parse cannot decode: git conflict markers
	// injected into the YAML front matter produce invalid YAML.
	const corrupt = `---
version: 1
title: Being Edited
status: todo
<<<<<<< HEAD
priority: high
=======
priority: low
>>>>>>> other
type: task
created_at: 2026-01-02T03:04:05Z
updated_at: 2026-01-02T03:04:05Z
---

Body under edit.
`

	t.Run("CurrentETag returns a non-reconcilable error for an unparseable file", func(t *testing.T) {
		nibsDir := setupNibsDir(t)
		writeNibFile(t, nibsDir, canonEtagFile, canonEtagCanonical)
		core := setupLoadedCore(t, nibsDir)

		if _, err := core.Get("etagcanon1"); err != nil {
			t.Fatalf("Get() error: %v", err)
		}

		// Corrupt the on-disk file after load.
		writeNibFile(t, nibsDir, canonEtagFile, corrupt)

		got, err := core.CurrentETag("etagcanon1")
		var unparseable *OnDiskUnparseableError
		if !errors.As(err, &unparseable) {
			t.Fatalf("CurrentETag for an unparseable file: got (%q, %T:%v), want *OnDiskUnparseableError", got, err, err)
		}
		if got != "" {
			t.Errorf("CurrentETag returned a non-empty etag token %q alongside the uncertifiable error; it must carry none", got)
		}
		if unparseable.Reason != "unparseable" {
			t.Errorf("Reason = %q, want %q", unparseable.Reason, "unparseable")
		}
	})

	t.Run("stale if-match Update is refused with OnDiskUnparseableError; retry cannot clobber", func(t *testing.T) {
		nibsDir := setupNibsDir(t)
		writeNibFile(t, nibsDir, canonEtagFile, canonEtagCanonical)
		core := setupLoadedCore(t, nibsDir)

		b, err := core.Get("etagcanon1")
		if err != nil {
			t.Fatalf("Get() error: %v", err)
		}
		ifMatch := b.ETag() // matches the in-memory value a normal client holds

		// An external writer leaves the file in an unparseable state.
		writeNibFile(t, nibsDir, canonEtagFile, corrupt)

		clone := b.Clone()
		clone.Title = "Stale Overwrite Attempt"
		clone.Body = "This must NOT clobber the corrupt-on-disk content."

		// Attempt 1: must be refused with the non-reconcilable error, NOT a
		// reconcilable ETagMismatchError.
		err = core.Update(clone, &ifMatch)
		var unparseable *OnDiskUnparseableError
		if !errors.As(err, &unparseable) {
			t.Fatalf("attempt 1: got %T: %v, want *OnDiskUnparseableError", err, err)
		}
		var mismatch *ETagMismatchError
		if errors.As(err, &mismatch) {
			t.Fatalf("attempt 1 returned a reconcilable ETagMismatchError (%v); an unparseable file must be non-reconcilable", mismatch)
		}

		// Attempt 2 hits the SAME branch as attempt 1 (the guard returns the
		// OnDiskUnparseableError before any etag comparison), so it adds no branch
		// coverage — it just documents that a "reconcile" token fabricated from the
		// corrupt bytes still cannot reach the comparison, hence cannot clobber.
		disk, err := os.ReadFile(filepath.Join(nibsDir, canonEtagFile))
		if err != nil {
			t.Fatalf("reading file: %v", err)
		}
		reconstructed := rawBytesETagForTest(disk)
		err = core.Update(clone, &reconstructed)
		if !errors.As(err, &unparseable) {
			t.Fatalf("attempt 2 (reconcile-retry): got %T: %v, want *OnDiskUnparseableError (clobber must be impossible)", err, err)
		}

		// The corrupt bytes must survive both attempts.
		after, err := os.ReadFile(filepath.Join(nibsDir, canonEtagFile))
		if err != nil {
			t.Fatalf("reading file after refused updates: %v", err)
		}
		if string(after) != corrupt {
			t.Errorf("a refused update overwrote the unparseable disk content:\n got:\n%s\nwant:\n%s", after, corrupt)
		}
	})
}

// rawBytesETagForTest mirrors the FNV-64a/hex hashing an attacker/naive client
// might apply to raw disk bytes to fabricate an if-match token. Used only to
// prove such a token can never satisfy the OnDiskUnparseableError guard.
func rawBytesETagForTest(raw []byte) string {
	h := fnv.New64a()
	h.Write(raw)
	return hex.EncodeToString(h.Sum(nil))
}

// TestComputeStoredETagReadErrorFailsClosed covers findings #1/#5 for the read
// path: when the on-disk file EXISTS but cannot be READ (permission-denied,
// transient/torn I/O — a non-IsNotExist error), an if-match Update must fail
// CLOSED with the non-reconcilable OnDiskUnparseableError (no reusable etag
// token), never the in-memory etag, so the file cannot be overwritten and a
// reconcile-retry cannot satisfy the guard.
func TestComputeStoredETagReadErrorFailsClosed(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("chmod 0 does not deny reads for root; run as non-root to exercise the read-error branch")
	}

	t.Run("CurrentETag returns a non-reconcilable error for an unreadable file", func(t *testing.T) {
		nibsDir := setupNibsDir(t)
		writeNibFile(t, nibsDir, canonEtagFile, canonEtagCanonical)
		core := setupLoadedCore(t, nibsDir)

		if _, err := core.Get("etagcanon1"); err != nil {
			t.Fatalf("Get() error: %v", err)
		}

		path := filepath.Join(nibsDir, canonEtagFile)
		if err := os.Chmod(path, 0); err != nil {
			t.Fatalf("chmod 0: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

		got, err := core.CurrentETag("etagcanon1")
		var unreadable *OnDiskUnparseableError
		if !errors.As(err, &unreadable) {
			t.Fatalf("CurrentETag for an unreadable file: got (%q, %T:%v), want *OnDiskUnparseableError", got, err, err)
		}
		if got != "" {
			t.Errorf("CurrentETag returned a non-empty etag token %q alongside the uncertifiable error; it must carry none", got)
		}
		if unreadable.Reason != "unreadable" {
			t.Errorf("Reason = %q, want %q", unreadable.Reason, "unreadable")
		}
	})

	t.Run("if-match Update on an unreadable file is refused (non-reconcilable) and disk survives", func(t *testing.T) {
		nibsDir := setupNibsDir(t)
		writeNibFile(t, nibsDir, canonEtagFile, canonEtagCanonical)
		core := setupLoadedCore(t, nibsDir)

		b, err := core.Get("etagcanon1")
		if err != nil {
			t.Fatalf("Get() error: %v", err)
		}
		ifMatch := b.ETag() // the etag a normal client holds from Get

		path := filepath.Join(nibsDir, canonEtagFile)
		if err := os.Chmod(path, 0); err != nil {
			t.Fatalf("chmod 0: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

		clone := b.Clone()
		clone.Title = "Overwrite Attempt"
		err = core.Update(clone, &ifMatch)
		var unparseable *OnDiskUnparseableError
		if !errors.As(err, &unparseable) {
			t.Fatalf("expected *OnDiskUnparseableError updating over an unreadable file, got %T: %v", err, err)
		}
		var mismatch *ETagMismatchError
		if errors.As(err, &mismatch) {
			t.Fatalf("an unreadable file must be non-reconcilable, got a reconcilable ETagMismatchError: %v", mismatch)
		}

		// Restore read access and confirm the original content survived untouched.
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatalf("chmod restore: %v", err)
		}
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading file after refused update: %v", err)
		}
		if string(after) != canonEtagCanonical {
			t.Errorf("refused update overwrote the on-disk content:\n got:\n%s\nwant:\n%s", after, canonEtagCanonical)
		}
	})
}

// TestComputeStoredETagFailsOpenWhenNotFlushed pins the two fail-OPEN branches of
// computeStoredETag's matrix (finding #1): a nib with no on-disk file yet (empty
// Path, or the file removed → os.IsNotExist) falls back to the in-memory etag so
// a normal if-match still matches, matching Update's not-flushed semantics.
func TestComputeStoredETagFailsOpenWhenNotFlushed(t *testing.T) {
	t.Run("empty Path falls back to the in-memory etag", func(t *testing.T) {
		nibsDir := setupNibsDir(t)
		core := New(nibsDir, config.Default())
		n := &nib.Nib{ID: "notflushed1", Title: "Not Flushed", Status: "todo"}
		got, err := core.computeStoredETag(n)
		if err != nil {
			t.Fatalf("computeStoredETag for an empty-Path nib: unexpected error %v", err)
		}
		if got != n.ETag() {
			t.Errorf("computeStoredETag for an empty-Path nib = %q, want in-memory etag %q", got, n.ETag())
		}
	})

	t.Run("removed file (os.IsNotExist) falls back to the in-memory etag", func(t *testing.T) {
		nibsDir := setupNibsDir(t)
		writeNibFile(t, nibsDir, canonEtagFile, canonEtagCanonical)
		core := setupLoadedCore(t, nibsDir)

		b, err := core.Get("etagcanon1")
		if err != nil {
			t.Fatalf("Get() error: %v", err)
		}
		if err := os.Remove(filepath.Join(nibsDir, canonEtagFile)); err != nil {
			t.Fatalf("removing file: %v", err)
		}
		got, err := core.CurrentETag("etagcanon1")
		if err != nil {
			t.Fatalf("CurrentETag() error: %v", err)
		}
		if got != b.ETag() {
			t.Errorf("CurrentETag after external delete = %q, want in-memory etag %q (fail open)", got, b.ETag())
		}
	})
}

// TestComputeStoredETagOutOfProjectionContent covers finding #2: content on
// disk that lives outside Render()'s modeled field set (unknown/extra YAML keys
// and the legacy v0 `blocking:` field) must still be reflected in the stored
// etag, so an external edit confined to that content is detected and NOT
// silently stripped by a stale if-match Update.
func TestComputeStoredETagOutOfProjectionContent(t *testing.T) {
	t.Run("unknown front-matter key is etag-visible and survives a refused update", func(t *testing.T) {
		nibsDir := setupNibsDir(t)
		const file = "extrakey1--unknown.md"
		const withAlice = `---
version: 1
title: Extra Key
status: todo
type: task
priority: normal
assignee: alice
created_at: 2026-01-02T03:04:05Z
updated_at: 2026-01-02T03:04:05Z
---

Body.
`
		writeNibFile(t, nibsDir, file, withAlice)
		core := setupLoadedCore(t, nibsDir)

		b, err := core.Get("extrakey1")
		if err != nil {
			t.Fatalf("Get() error: %v", err)
		}
		ifMatch := b.ETag()
		before, err := core.CurrentETag("extrakey1")
		if err != nil {
			t.Fatalf("CurrentETag() error: %v", err)
		}
		if before != ifMatch {
			t.Fatalf("stored etag %s != in-memory etag %s for a nib with an unknown key (round-trip must be lossless)", before, ifMatch)
		}

		// External edit changing ONLY the unknown key.
		withBob := strings.Replace(withAlice, "assignee: alice", "assignee: bob", 1)
		writeNibFile(t, nibsDir, file, withBob)

		after, err := core.CurrentETag("extrakey1")
		if err != nil {
			t.Fatalf("CurrentETag() error: %v", err)
		}
		if after == before {
			t.Errorf("CurrentETag blind to an unknown-key edit: got %s == %s", after, before)
		}

		// A stale if-match Update must be refused, and the unknown key must survive.
		clone := b.Clone()
		clone.Title = "Stale Overwrite"
		err = core.Update(clone, &ifMatch)
		var mismatch *ETagMismatchError
		if !errors.As(err, &mismatch) {
			t.Fatalf("expected *ETagMismatchError on unknown-key divergence, got %T: %v", err, err)
		}
		disk, err := os.ReadFile(filepath.Join(nibsDir, file))
		if err != nil {
			t.Fatalf("reading file: %v", err)
		}
		if !strings.Contains(string(disk), "assignee: bob") {
			t.Errorf("unknown key stripped from disk by a refused update:\n%s", disk)
		}
	})

	t.Run("legacy v0 blocking field is etag-visible and survives a refused update", func(t *testing.T) {
		nibsDir := setupNibsDir(t)
		// Seed with a canonical v1 file so Load produces a stable in-memory nib.
		// The two on-disk states below are v0 files that differ ONLY in their
		// `blocking:` target, so the etag comparison isolates the out-of-projection
		// content (writing them after Load avoids migrateV0ToV1 rewriting the file
		// and changing `version`, which would confound the comparison).
		const file = "v0block1--legacy.md"
		writeNibFile(t, nibsDir, file, `---
version: 1
title: Legacy Blocking
status: todo
type: task
priority: normal
created_at: 2026-01-02T03:04:05Z
updated_at: 2026-01-02T03:04:05Z
---

Body.
`)
		core := setupLoadedCore(t, nibsDir)

		b, err := core.Get("v0block1")
		if err != nil {
			t.Fatalf("Get() error: %v", err)
		}
		ifMatch := b.ETag()

		writeV0 := func(blockingID string) string {
			return `---
title: Legacy Blocking
status: todo
type: task
priority: normal
blocking:
  - ` + blockingID + `
created_at: 2026-01-02T03:04:05Z
updated_at: 2026-01-02T03:04:05Z
---

Body.
`
		}

		// Two v0 on-disk states differing ONLY in the blocking target.
		writeNibFile(t, nibsDir, file, writeV0("ghost-a"))
		before, err := core.CurrentETag("v0block1")
		if err != nil {
			t.Fatalf("CurrentETag() error: %v", err)
		}
		writeNibFile(t, nibsDir, file, writeV0("ghost-b"))
		after, err := core.CurrentETag("v0block1")
		if err != nil {
			t.Fatalf("CurrentETag() error: %v", err)
		}
		if after == before {
			t.Errorf("CurrentETag blind to a v0 `blocking:` edit: got %s == %s", after, before)
		}

		// A stale if-match Update must be refused, and the blocking content survives.
		clone := b.Clone()
		clone.Title = "Stale Overwrite"
		err = core.Update(clone, &ifMatch)
		var mismatch *ETagMismatchError
		if !errors.As(err, &mismatch) {
			t.Fatalf("expected *ETagMismatchError on v0 blocking divergence, got %T: %v", err, err)
		}
		disk, err := os.ReadFile(filepath.Join(nibsDir, file))
		if err != nil {
			t.Fatalf("reading file: %v", err)
		}
		if !strings.Contains(string(disk), "ghost-b") {
			t.Errorf("v0 blocking content stripped from disk by a refused update:\n%s", disk)
		}
	})
}

// TestUpdateUnknownKeyByteVerbatimRoundTrip verifies that YAML-1.1-ambiguous
// unknown keys (`offset: -0.0` signed-zero, `reviewed: y` bool-like) survive a
// real if-match Update byte-for-byte on disk.
//
// NOTE ON SCOPE: this test cannot exercise a self-conflict mechanism — a
// pre-Update `stored == ifMatch` check would be tautological, since core.Get
// (in-memory nib from Load) and core.CurrentETag (re-read + re-parse of disk)
// both parse the SAME unchanged on-disk bytes with the SAME parser, so they
// necessarily agree regardless of the parser version. Its value is the
// byte-verbatim disk assertions after a genuine Update: they prove the raw
// unknown-key text is neither coerced nor stripped when the nib is rewritten.
func TestUpdateUnknownKeyByteVerbatimRoundTrip(t *testing.T) {
	nibsDir := setupNibsDir(t)
	const file = "ambig1--ambiguous.md"
	const content = `---
version: 1
title: Ambiguous Scalars
status: todo
type: task
priority: normal
offset: -0.0
reviewed: y
created_at: 2026-01-02T03:04:05Z
updated_at: 2026-01-02T03:04:05Z
---

Body.
`
	writeNibFile(t, nibsDir, file, content)
	core := setupLoadedCore(t, nibsDir)

	b, err := core.Get("ambig1")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	ifMatch := b.ETag()

	// A normal if-match Update (no concurrent writer) must succeed, not conflict.
	clone := b.Clone()
	clone.Body = "Edited body."
	if err := core.Update(clone, &ifMatch); err != nil {
		t.Fatalf("Update with own ETag if-match failed (self-inflicted conflict?): %T: %v", err, err)
	}

	// The unknown keys must survive the write verbatim.
	disk, err := os.ReadFile(filepath.Join(nibsDir, file))
	if err != nil {
		t.Fatalf("reading file: %v", err)
	}
	if !strings.Contains(string(disk), "offset: -0.0") {
		t.Errorf("signed-zero unknown key not preserved verbatim on disk:\n%s", disk)
	}
	if !strings.Contains(string(disk), "reviewed: y") {
		t.Errorf("bool-like unknown key not preserved verbatim on disk:\n%s", disk)
	}
}
