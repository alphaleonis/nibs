package nibcore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/fsutil"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/store"
)

// writeNibFile writes a raw nib markdown file to disk for testing migration. It
// writes atomically because one of its callers drives a live watcher, which must
// never observe a half-written file (see writeNibFileAtomic).
func writeNibFile(t *testing.T, dir, filename, content string) {
	t.Helper()
	writeNibFileAtomic(t, filepath.Join(dir, filename), content)
}

// TestLoadNeverWrites pins the inverse invariant the explicit-migration design
// bought: Load() reads. Loading a legacy-shaped store — v0 files with
// `blocking:` edges, a `priority: deferred` file, plus a canonical v1 control —
// leaves every file byte-identical. Silent load-time migration is gone; the
// only path that rewrites store files is `nibs migrate`.
func TestLoadNeverWrites(t *testing.T) {
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, store.DirName)
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatal(err)
	}

	files := map[string]string{
		"v0a--blocker.md": "---\ntitle: Blocker\nstatus: todo\nblocking:\n    - v0b\n---\n\nBody A.\n",
		"v0b--target.md":  "---\ntitle: Target\nstatus: todo\n---\n\nBody B.\n",
		"def1--legacy.md": "---\nversion: 1\ntitle: Legacy Deferred\nstatus: todo\npriority: deferred\n---\n\nBody D.\n",
		"cur1--modern.md": "---\nversion: 1\ntitle: Modern\nstatus: todo\ntype: task\n---\n\nBody C.\n",
	}
	for name, content := range files {
		writeNibFile(t, storeData(t, nibsDir), name, content)
	}

	cfg := config.Default()
	core := New(nibsDir, cfg)
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	for name, before := range files {
		after, err := os.ReadFile(dataPath(nibsDir, name))
		if err != nil {
			t.Fatalf("re-reading %s: %v", name, err)
		}
		if string(after) != before {
			t.Errorf("Load() rewrote %s; Load must never write.\nbefore:\n%s\nafter:\n%s", name, before, after)
		}
	}

	// The legacy shapes load AS THEY ARE: no in-memory migration either. The
	// store is only ever in this state under `nibs migrate` itself (every other
	// command refuses pre-load), so what Load reports must be what disk holds.
	v0a, err := core.Get("v0a")
	if err != nil {
		t.Fatalf("Get(v0a): %v", err)
	}
	if v0a.Version != 0 || len(v0a.Blocking) != 1 {
		t.Errorf("v0 nib loaded as version=%d blocking=%v, want the on-disk v0 shape", v0a.Version, v0a.Blocking)
	}
	def1, err := core.Get("def1")
	if err != nil {
		t.Fatalf("Get(def1): %v", err)
	}
	if def1.Priority != "deferred" {
		t.Errorf("def1.Priority = %q, want the on-disk %q (parse-time normalization is retired)", def1.Priority, "deferred")
	}
}

func TestCheckBrokenDocuments(t *testing.T) {
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, store.DirName)
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
		Slug:      "doc-check",
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

// loadMigrationCore writes the given files into a fresh .nibs dir, loads a
// Core over it (which, per TestLoadNeverWrites, changes nothing), acquires the
// store-wide lock the migration methods require as proof-of-lock, and returns
// all three. Migration tests then call MigrateV0ToV1 explicitly — the way the
// migrate command drives it: lock held for the whole test (released via
// cleanup), token passed to every migration call against this store.
func loadMigrationCore(t *testing.T, files map[string]string) (*Core, string, *StoreLock) {
	t.Helper()
	nibsDir := filepath.Join(t.TempDir(), store.DirName)
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		writeNibFile(t, storeData(t, nibsDir), name, content)
	}
	core := New(nibsDir, config.Default())
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	lock, err := AcquireStoreLock(nibsDir)
	if err != nil {
		t.Fatalf("AcquireStoreLock: %v", err)
	}
	t.Cleanup(func() { _ = lock.Release() })
	return core, nibsDir, lock
}

func TestMigrateV0ToV1(t *testing.T) {
	t.Run("converts blocking to blockedBy on targets and persists", func(t *testing.T) {
		core, nibsDir, lock := loadMigrationCore(t, map[string]string{
			"aaa1--blocker.md": "---\ntitle: Blocker\nstatus: todo\nblocking:\n    - bbb2\n---\n",
			"bbb2--blocked.md": "---\ntitle: Blocked\nstatus: todo\n---\n",
		})

		n, err := core.MigrateV0ToV1(lock)
		if err != nil {
			t.Fatalf("MigrateV0ToV1() error: %v", err)
		}
		if n != 2 {
			t.Errorf("migrated count = %d, want 2", n)
		}

		// In memory: A is v1 with blocking cleared; B is v1 with blockedBy=[aaa1].
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

		// Persisted: a fresh Load sees the converted store.
		core2 := New(nibsDir, config.Default())
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
		// A blocks B, and B already has blockedBy=[A] (dual-side legacy):
		// the transfer must deduplicate.
		core, _, lock := loadMigrationCore(t, map[string]string{
			"aaa1.md": "---\ntitle: Blocker\nstatus: todo\nblocking:\n    - bbb2\n---\n",
			"bbb2.md": "---\ntitle: Blocked\nstatus: todo\nblocked_by:\n    - aaa1\n---\n",
		})

		if _, err := core.MigrateV0ToV1(lock); err != nil {
			t.Fatalf("MigrateV0ToV1() error: %v", err)
		}

		b, _ := core.Get("bbb2")
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

	t.Run("drops a blocking reference to a nonexistent nib with a warning", func(t *testing.T) {
		core, _, lock := loadMigrationCore(t, map[string]string{
			"aaa1.md": "---\ntitle: Blocker\nstatus: todo\nblocking:\n    - nonexistent\n---\n",
		})
		var warnings strings.Builder
		core.SetWarnWriter(&warnings)

		if _, err := core.MigrateV0ToV1(lock); err != nil {
			t.Fatalf("MigrateV0ToV1() error: %v", err)
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
		core, _, lock := loadMigrationCore(t, map[string]string{
			"aaa1.md": "---\ntitle: Simple Nib\nstatus: todo\n---\n",
		})

		if _, err := core.MigrateV0ToV1(lock); err != nil {
			t.Fatalf("MigrateV0ToV1() error: %v", err)
		}
		a, _ := core.Get("aaa1")
		if a.Version != 1 {
			t.Errorf("Version = %d, want 1", a.Version)
		}
	})

	t.Run("idempotent: an all-v1 store is untouched", func(t *testing.T) {
		files := map[string]string{
			"aaa1.md": "---\nversion: 1\ntitle: Already Migrated\nstatus: todo\nblocked_by:\n    - bbb2\n---\n",
			"bbb2.md": "---\nversion: 1\ntitle: Other Nib\nstatus: todo\n---\n",
		}
		core, nibsDir, lock := loadMigrationCore(t, files)

		n, err := core.MigrateV0ToV1(lock)
		if err != nil {
			t.Fatalf("MigrateV0ToV1() error: %v", err)
		}
		if n != 0 {
			t.Errorf("migrated count = %d, want 0 on an all-v1 store", n)
		}
		for name, before := range files {
			after, err := os.ReadFile(dataPath(nibsDir, name))
			if err != nil {
				t.Fatalf("re-reading %s: %v", name, err)
			}
			if string(after) != before {
				t.Errorf("MigrateV0ToV1 rewrote already-migrated file %s:\n%s", name, after)
			}
		}
		a, _ := core.Get("aaa1")
		if len(a.BlockedBy) != 1 || a.BlockedBy[0] != "bbb2" {
			t.Errorf("BlockedBy = %v, want [bbb2]", a.BlockedBy)
		}
	})

	t.Run("fail-loud: a persistence failure aborts with an error", func(t *testing.T) {
		core, _, lock := loadMigrationCore(t, map[string]string{
			"leg1--legacy.md": "---\ntitle: Legacy V0\nstatus: todo\n---\n",
		})

		// Force the persistence failure via the atomic-write rename seam
		// (saveToDisk writes a temp file and renames it over the target), which
		// simulates an un-persistable .nibs — full disk, unwritable dir, torn
		// rename — deterministically and independent of uid/OS.
		orig := fsutil.RenameFn
		fsutil.RenameFn = func(_, _ string) error { return errors.New("simulated persistence failure") }
		t.Cleanup(func() { fsutil.RenameFn = orig })

		if _, err := core.MigrateV0ToV1(lock); err == nil {
			t.Fatal("MigrateV0ToV1() = nil error on an unwritable store, want fail-loud error")
		} else if !strings.Contains(err.Error(), "leg1") {
			t.Errorf("error should name the nib that failed to persist, got: %v", err)
		}
	})

	t.Run("mixed v0 and v1 directory", func(t *testing.T) {
		core, _, lock := loadMigrationCore(t, map[string]string{
			"v0nib.md": "---\ntitle: V0 Nib\nstatus: todo\nblocking:\n    - v1nib\n---\n",
			"v1nib.md": "---\nversion: 1\ntitle: V1 Nib\nstatus: todo\n---\n",
		})

		n, err := core.MigrateV0ToV1(lock)
		if err != nil {
			t.Fatalf("MigrateV0ToV1() error: %v", err)
		}
		if n != 1 {
			t.Errorf("migrated count = %d, want 1 (only the v0 nib)", n)
		}

		v0, _ := core.Get("v0nib")
		v1, _ := core.Get("v1nib")
		if v0.Version != 1 {
			t.Errorf("v0nib.Version = %d, want 1", v0.Version)
		}
		if v1.Version != 1 {
			t.Errorf("v1nib.Version = %d, want 1", v1.Version)
		}
		if !v1.IsBlockedBy("v0nib") {
			t.Errorf("v1nib.BlockedBy should contain v0nib, got %v", v1.BlockedBy)
		}
	})
}

// TestMigrateV0ToV1CrashResumeKeepsEdges pins the persist-ordering contract:
// a write failure at ANY point mid-run must leave the store in a state a
// re-run converges from with EVERY legacy blocking edge present. The dangerous
// ordering is a source id sorting before its target's: a single-phase
// sorted-id persist writes the source (blocking cleared, version stamped —
// the resume signal) before the target holds the transferred edge, so a crash
// between the two loses the edge while the store reports fully migrated.
//
// Each scenario is probed at every write index: fail the Nth write, then
// re-run over a fresh Core (the crashed process is gone; the files alone say
// what is left) and require full convergence. The loop stops at the first N
// the run never reaches, so every crash point of the implementation is
// exercised no matter how many writes it performs.
func TestMigrateV0ToV1CrashResumeKeepsEdges(t *testing.T) {
	scenarios := []struct {
		name  string
		files map[string]string
		// edges[target] lists the blockers the target must carry after
		// convergence.
		edges map[string][]string
	}{
		{
			name: "source sorts before target",
			files: map[string]string{
				"aaa1--source.md": "---\ntitle: Source\nstatus: todo\nblocking:\n    - zzz1\n---\n",
				"zzz1--target.md": "---\nversion: 1\ntitle: Target\nstatus: todo\n---\n",
			},
			edges: map[string][]string{"zzz1": {"aaa1"}},
		},
		{
			name: "v0 chain: targets are sources too",
			files: map[string]string{
				"aaa1--head.md": "---\ntitle: Head\nstatus: todo\nblocking:\n    - bbb2\n---\n",
				"bbb2--mid.md":  "---\ntitle: Mid\nstatus: todo\nblocking:\n    - ccc3\n---\n",
				"ccc3--tail.md": "---\ntitle: Tail\nstatus: todo\n---\n",
			},
			edges: map[string][]string{"bbb2": {"aaa1"}, "ccc3": {"bbb2"}},
		},
		{
			name: "v0 blocking cycle",
			files: map[string]string{
				"aaa1--one.md": "---\ntitle: One\nstatus: todo\nblocking:\n    - bbb2\n---\n",
				"bbb2--two.md": "---\ntitle: Two\nstatus: todo\nblocking:\n    - aaa1\n---\n",
			},
			edges: map[string][]string{"aaa1": {"bbb2"}, "bbb2": {"aaa1"}},
		},
	}

	verifyConverged := func(t *testing.T, nibsDir string, edges map[string][]string) {
		t.Helper()
		final := New(nibsDir, config.Default())
		final.SetWarnWriter(nil)
		if err := final.Load(); err != nil {
			t.Fatalf("verification Load: %v", err)
		}
		for _, b := range final.All() {
			if b.Version != 1 {
				t.Errorf("nib %s converged at version %d, want 1", b.ID, b.Version)
			}
			if len(b.Blocking) != 0 {
				t.Errorf("nib %s still carries blocking %v after convergence", b.ID, b.Blocking)
			}
		}
		for target, blockers := range edges {
			b, err := final.Get(target)
			if err != nil {
				t.Fatalf("Get(%s): %v", target, err)
			}
			for _, blocker := range blockers {
				if !b.IsBlockedBy(blocker) {
					t.Errorf("edge %s -> %s lost: %s.BlockedBy = %v", blocker, target, target, b.BlockedBy)
				}
			}
		}
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			const maxProbes = 20 // safety bound; every scenario completes in far fewer writes
			completed := false
			for failAt := 1; failAt <= maxProbes; failAt++ {
				core, nibsDir, lock := loadMigrationCore(t, sc.files)

				orig := fsutil.RenameFn
				writes := 0
				fsutil.RenameFn = func(oldpath, newpath string) error {
					writes++
					if writes == failAt {
						return errors.New("injected crash")
					}
					return orig(oldpath, newpath)
				}
				_, err := core.MigrateV0ToV1(lock)
				fsutil.RenameFn = orig

				if err == nil {
					// The run finished before reaching write #failAt: every
					// crash point has been probed. Verify the clean run too.
					verifyConverged(t, nibsDir, sc.edges)
					completed = true
					break
				}

				// Crashed mid-run: a fresh process re-runs; the files alone
				// must say what is left, and convergence must restore every
				// edge.
				resumed := New(nibsDir, config.Default())
				resumed.SetWarnWriter(nil)
				if err := resumed.Load(); err != nil {
					t.Fatalf("failAt=%d: resume Load: %v", failAt, err)
				}
				if _, err := resumed.MigrateV0ToV1(lock); err != nil {
					t.Fatalf("failAt=%d: resumed MigrateV0ToV1: %v", failAt, err)
				}
				t.Run(fmt.Sprintf("crash at write %d", failAt), func(t *testing.T) {
					verifyConverged(t, nibsDir, sc.edges)
				})
			}
			if !completed {
				t.Fatalf("migration still failing after %d write probes; runaway write count?", maxProbes)
			}
		})
	}
}

// TestMigrationMethodsRequireLockToken pins the proof-of-lock contract: the
// token must prove CURRENT possession of THIS store's lock, so each way a
// token can fail to prove that is refused before any state is touched — nil
// (never acquired), acquired for a different store (holds the wrong lock),
// and already released (holds nothing anymore). A future direct caller
// (serve auto-migrate, the TUI) is exactly who could release early or cross
// stores, silently.
func TestMigrationMethodsRequireLockToken(t *testing.T) {
	files := map[string]string{
		"v0a--one.md": "---\ntitle: One\nstatus: todo\n---\n",
	}
	core, nibsDir, lock := loadMigrationCore(t, files)

	if _, err := core.MigrateV0ToV1(nil); err == nil || !strings.Contains(err.Error(), "AcquireStoreLock") {
		t.Errorf("MigrateV0ToV1(nil) = %v, want a refusal naming AcquireStoreLock", err)
	}
	if _, err := core.NormalizeLegacyPriorities(nil); err == nil || !strings.Contains(err.Error(), "AcquireStoreLock") {
		t.Errorf("NormalizeLegacyPriorities(nil) = %v, want a refusal naming AcquireStoreLock", err)
	}

	// A token acquired for a DIFFERENT store holds the wrong lock. Acquiring
	// it while this store's own lock is held is safe — the two flocks are
	// different files.
	otherRoot := filepath.Join(t.TempDir(), store.DirName)
	if err := os.MkdirAll(otherRoot, 0755); err != nil {
		t.Fatal(err)
	}
	otherLock, err := AcquireStoreLock(otherRoot)
	if err != nil {
		t.Fatalf("AcquireStoreLock(other store): %v", err)
	}
	t.Cleanup(func() { _ = otherLock.Release() })
	if _, err := core.MigrateV0ToV1(otherLock); err == nil || !strings.Contains(err.Error(), "different store") {
		t.Errorf("MigrateV0ToV1(other store's lock) = %v, want a different-store refusal", err)
	}
	if _, err := core.NormalizeLegacyPriorities(otherLock); err == nil || !strings.Contains(err.Error(), "different store") {
		t.Errorf("NormalizeLegacyPriorities(other store's lock) = %v, want a different-store refusal", err)
	}

	// A released token no longer proves possession.
	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, err := core.MigrateV0ToV1(lock); err == nil || !strings.Contains(err.Error(), "released") {
		t.Errorf("MigrateV0ToV1(released lock) = %v, want a released-token refusal", err)
	}
	if _, err := core.NormalizeLegacyPriorities(lock); err == nil || !strings.Contains(err.Error(), "released") {
		t.Errorf("NormalizeLegacyPriorities(released lock) = %v, want a released-token refusal", err)
	}

	// Every refusal above fired BEFORE touching state: the store is
	// byte-identical.
	for name, before := range files {
		after, err := os.ReadFile(dataPath(nibsDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != before {
			t.Errorf("lock-token refusal modified %s:\n%s", name, after)
		}
	}
}

func TestNormalizeLegacyPriorities(t *testing.T) {
	t.Run("rewrites deferred to low on disk without touching the version", func(t *testing.T) {
		control := "---\nversion: 1\ntitle: Control\nstatus: todo\npriority: normal\n---\n"
		core, nibsDir, lock := loadMigrationCore(t, map[string]string{
			"def1--legacy.md": "---\nversion: 1\ntitle: Set Aside\nstatus: todo\npriority: deferred\n---\n",
			"v0d2--old.md":    "---\ntitle: Old Deferred\nstatus: todo\npriority: deferred\n---\n",
			"ctl3--normal.md": control,
		})

		n, err := core.NormalizeLegacyPriorities(lock)
		if err != nil {
			t.Fatalf("NormalizeLegacyPriorities() error: %v", err)
		}
		if n != 2 {
			t.Errorf("rewritten count = %d, want 2", n)
		}

		for _, name := range []string{"def1--legacy.md", "v0d2--old.md"} {
			disk, err := os.ReadFile(dataPath(nibsDir, name))
			if err != nil {
				t.Fatalf("re-reading %s: %v", name, err)
			}
			s := string(disk)
			if strings.Contains(s, "deferred") {
				t.Errorf("%s still contains 'deferred':\n%s", name, s)
			}
			if !strings.Contains(s, "priority: low") {
				t.Errorf("%s missing 'priority: low':\n%s", name, s)
			}
		}

		// The already-v1 file keeps its version; the v0 file must NOT be
		// stamped `version: 1`: the stamp is MigrateV0ToV1's completion
		// record, and writing it here would mark the file's `blocking:` edges
		// migrated without transferring them — permanently, since v0 detection
		// keys on the version. The rewrite renders `version: 0`, which stays
		// detectably v0.
		v1Disk, err := os.ReadFile(dataPath(nibsDir, "def1--legacy.md"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(v1Disk), "version: 1") {
			t.Errorf("already-v1 file lost its version key:\n%s", v1Disk)
		}
		v0Disk, err := os.ReadFile(dataPath(nibsDir, "v0d2--old.md"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(v0Disk), "version: 1") {
			t.Errorf("still-v0 file was version-stamped by the priority step:\n%s", v0Disk)
		}
		reloaded := New(nibsDir, config.Default())
		reloaded.SetWarnWriter(nil)
		if err := reloaded.Load(); err != nil {
			t.Fatalf("reload after normalize: %v", err)
		}
		if b, err := reloaded.Get("v0d2"); err != nil || b.Version != 0 {
			t.Errorf("rewritten v0 file must remain version 0 for the v0 step to find, got Version=%d (err %v)", b.Version, err)
		}

		// Control untouched, and a second run is a no-op.
		ctl, err := os.ReadFile(dataPath(nibsDir, "ctl3--normal.md"))
		if err != nil {
			t.Fatalf("re-reading control: %v", err)
		}
		if string(ctl) != control {
			t.Errorf("control file was rewritten:\n%s", ctl)
		}
		if n, err := core.NormalizeLegacyPriorities(lock); err != nil || n != 0 {
			t.Errorf("second run = (%d, %v), want (0, nil)", n, err)
		}
	})

	t.Run("fail-loud: a persistence failure aborts with an error", func(t *testing.T) {
		core, _, lock := loadMigrationCore(t, map[string]string{
			"def1--legacy.md": "---\nversion: 1\ntitle: Set Aside\nstatus: todo\npriority: deferred\n---\n",
		})
		orig := fsutil.RenameFn
		fsutil.RenameFn = func(_, _ string) error { return errors.New("simulated persistence failure") }
		t.Cleanup(func() { fsutil.RenameFn = orig })

		if _, err := core.NormalizeLegacyPriorities(lock); err == nil {
			t.Fatal("NormalizeLegacyPriorities() = nil error on an unwritable store, want fail-loud error")
		} else if !strings.Contains(err.Error(), "def1") {
			t.Errorf("error should name the nib that failed to persist, got: %v", err)
		}
	})
}

func TestMigrateV1ToV2(t *testing.T) {
	t.Run("moves a milestone parent onto the assignment axis and persists", func(t *testing.T) {
		core, nibsDir, lock := loadMigrationCore(t, map[string]string{
			"ms01--rel.md":  "---\nversion: 1\ntitle: Release\nstatus: todo\ntype: milestone\n---\n",
			"epi1--auth.md": "---\nversion: 1\ntitle: Auth\nstatus: todo\ntype: epic\nparent: ms01\norder: a5\n---\n",
			"tsk1--sub.md":  "---\nversion: 1\ntitle: Sub\nstatus: todo\ntype: task\nparent: epi1\norder: a0\n---\n",
		})

		n, err := core.MigrateV1ToV2(lock)
		if err != nil {
			t.Fatalf("MigrateV1ToV2() error: %v", err)
		}
		if n != 3 {
			t.Errorf("migrated count = %d, want 3 (every v1 nib is stamped)", n)
		}

		// In memory: the milestone child moved axes; the epic child did not.
		epi, _ := core.Get("epi1")
		if epi.Milestone != "ms01" || epi.MilestoneOrder != "a5" {
			t.Errorf("epi1 = milestone %q order %q, want the assignment ms01/a5", epi.Milestone, epi.MilestoneOrder)
		}
		if epi.Parent != "" || epi.Order != "" {
			t.Errorf("epi1 kept parent %q order %q; the milestone parent must be cleared", epi.Parent, epi.Order)
		}
		tsk, _ := core.Get("tsk1")
		if tsk.Parent != "epi1" || tsk.Order != "a0" || tsk.Milestone != "" {
			t.Errorf("tsk1 = parent %q order %q milestone %q; a non-milestone parent is untouched", tsk.Parent, tsk.Order, tsk.Milestone)
		}
		for _, id := range []string{"ms01", "epi1", "tsk1"} {
			b, _ := core.Get(id)
			if b.Version != 2 {
				t.Errorf("%s.Version = %d, want 2", id, b.Version)
			}
		}

		// Persisted: a fresh Load sees the converted store.
		core2 := New(nibsDir, config.Default())
		core2.SetWarnWriter(nil)
		if err := core2.Load(); err != nil {
			t.Fatalf("second Load() error: %v", err)
		}
		epi2, _ := core2.Get("epi1")
		if epi2.Milestone != "ms01" || epi2.MilestoneOrder != "a5" || epi2.Parent != "" || epi2.Version != 2 {
			t.Errorf("epi1 not persisted: milestone %q order %q parent %q version %d", epi2.Milestone, epi2.MilestoneOrder, epi2.Parent, epi2.Version)
		}
	})

	t.Run("an existing assignment wins the collision, with a warning", func(t *testing.T) {
		core, _, lock := loadMigrationCore(t, map[string]string{
			"ms01--one.md": "---\nversion: 1\ntitle: One\nstatus: todo\ntype: milestone\n---\n",
			"ms02--two.md": "---\nversion: 1\ntitle: Two\nstatus: todo\ntype: milestone\n---\n",
			"tsk1--t.md":   "---\nversion: 1\ntitle: T\nstatus: todo\ntype: task\nparent: ms01\norder: a5\nmilestone: ms02\nmilestone_order: b3\n---\n",
		})
		var warnings strings.Builder
		core.SetWarnWriter(&warnings)

		if _, err := core.MigrateV1ToV2(lock); err != nil {
			t.Fatalf("MigrateV1ToV2() error: %v", err)
		}

		tsk, _ := core.Get("tsk1")
		if tsk.Milestone != "ms02" || tsk.MilestoneOrder != "b3" {
			t.Errorf("tsk1 = milestone %q order %q, want the pre-existing ms02/b3 kept", tsk.Milestone, tsk.MilestoneOrder)
		}
		if tsk.Parent != "" || tsk.Order != "" {
			t.Errorf("tsk1 kept parent %q order %q; the milestone parent is cleared even on a collision", tsk.Parent, tsk.Order)
		}
		if !strings.Contains(warnings.String(), "tsk1") || !strings.Contains(warnings.String(), "ms02") {
			t.Errorf("collision should be warned about, got %q", warnings.String())
		}
	})

	t.Run("dangling and non-milestone parents stay on the parent axis", func(t *testing.T) {
		core, _, lock := loadMigrationCore(t, map[string]string{
			"epi1--e.md": "---\nversion: 1\ntitle: E\nstatus: todo\ntype: epic\n---\n",
			"tsk1--a.md": "---\nversion: 1\ntitle: A\nstatus: todo\ntype: task\nparent: epi1\norder: a0\n---\n",
			"tsk2--b.md": "---\nversion: 1\ntitle: B\nstatus: todo\ntype: task\nparent: ghost\n---\n",
		})

		if _, err := core.MigrateV1ToV2(lock); err != nil {
			t.Fatalf("MigrateV1ToV2() error: %v", err)
		}
		a, _ := core.Get("tsk1")
		if a.Parent != "epi1" || a.Order != "a0" || a.Milestone != "" || a.Version != 2 {
			t.Errorf("tsk1 = parent %q order %q milestone %q version %d, want epic parent untouched at version 2", a.Parent, a.Order, a.Milestone, a.Version)
		}
		b, _ := core.Get("tsk2")
		if b.Parent != "ghost" || b.Version != 2 {
			t.Errorf("tsk2 = parent %q version %d, want the dangling parent untouched at version 2", b.Parent, b.Version)
		}
	})

	t.Run("an illegally nested milestone is not assigned to its parent", func(t *testing.T) {
		core, _, lock := loadMigrationCore(t, map[string]string{
			"ms01--outer.md": "---\nversion: 1\ntitle: Outer\nstatus: todo\ntype: milestone\n---\n",
			"ms02--inner.md": "---\nversion: 1\ntitle: Inner\nstatus: todo\ntype: milestone\nparent: ms01\n---\n",
		})

		if _, err := core.MigrateV1ToV2(lock); err != nil {
			t.Fatalf("MigrateV1ToV2() error: %v", err)
		}
		inner, _ := core.Get("ms02")
		// A milestone is never a member of another; v1 membership never
		// enqueued the nest, so the migration must not invent the assignment.
		// The illegal parent stays as it is — untouched, not repaired.
		if inner.Milestone != "" || inner.Parent != "ms01" || inner.Version != 2 {
			t.Errorf("ms02 = milestone %q parent %q version %d, want no assignment, parent kept, version 2", inner.Milestone, inner.Parent, inner.Version)
		}
	})

	t.Run("a still-v0 file is left for the v0 step", func(t *testing.T) {
		files := map[string]string{
			"v0a--old.md":  "---\ntitle: Old\nstatus: todo\nblocking:\n    - cur1\n---\n",
			"cur1--new.md": "---\nversion: 1\ntitle: New\nstatus: todo\n---\n",
		}
		core, nibsDir, lock := loadMigrationCore(t, files)

		n, err := core.MigrateV1ToV2(lock)
		if err != nil {
			t.Fatalf("MigrateV1ToV2() error: %v", err)
		}
		if n != 1 {
			t.Errorf("migrated count = %d, want 1 (only the v1 nib)", n)
		}
		// The version stamp is MigrateV0ToV1's completion record: stamping the
		// v0 file from here would mark its `blocking:` edges migrated without
		// transferring them, permanently. Byte-identical, not merely still-v0.
		after, err := os.ReadFile(dataPath(nibsDir, "v0a--old.md"))
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != files["v0a--old.md"] {
			t.Errorf("v0 file was touched by the v1→v2 step:\n%s", after)
		}
	})

	t.Run("idempotent: an all-v2 store is untouched", func(t *testing.T) {
		files := map[string]string{
			"ms01--m.md":  "---\nversion: 2\ntitle: M\nstatus: todo\ntype: milestone\n---\n",
			"tsk1--t.md":  "---\nversion: 2\ntitle: T\nstatus: todo\ntype: task\nmilestone: ms01\nmilestone_order: a0\n---\n",
			"tsk2--t2.md": "---\nversion: 2\ntitle: T2\nstatus: todo\ntype: task\nparent: ms01\n---\n",
		}
		core, nibsDir, lock := loadMigrationCore(t, files)

		n, err := core.MigrateV1ToV2(lock)
		if err != nil {
			t.Fatalf("MigrateV1ToV2() error: %v", err)
		}
		if n != 0 {
			t.Errorf("migrated count = %d, want 0 on an all-v2 store", n)
		}
		for name, before := range files {
			after, err := os.ReadFile(dataPath(nibsDir, name))
			if err != nil {
				t.Fatalf("re-reading %s: %v", name, err)
			}
			if string(after) != before {
				t.Errorf("MigrateV1ToV2 rewrote already-migrated file %s:\n%s", name, after)
			}
		}
	})

	t.Run("fail-loud: a persistence failure aborts with an error", func(t *testing.T) {
		core, _, lock := loadMigrationCore(t, map[string]string{
			"cur1--one.md": "---\nversion: 1\ntitle: One\nstatus: todo\n---\n",
		})
		orig := fsutil.RenameFn
		fsutil.RenameFn = func(_, _ string) error { return errors.New("simulated persistence failure") }
		t.Cleanup(func() { fsutil.RenameFn = orig })

		if _, err := core.MigrateV1ToV2(lock); err == nil {
			t.Fatal("MigrateV1ToV2() = nil error on an unwritable store, want fail-loud error")
		} else if !strings.Contains(err.Error(), "cur1") {
			t.Errorf("error should name the nib that failed to persist, got: %v", err)
		}
	})

	t.Run("requires the lock token", func(t *testing.T) {
		core, _, _ := loadMigrationCore(t, map[string]string{
			"cur1--one.md": "---\nversion: 1\ntitle: One\nstatus: todo\n---\n",
		})
		if _, err := core.MigrateV1ToV2(nil); err == nil || !strings.Contains(err.Error(), "AcquireStoreLock") {
			t.Errorf("MigrateV1ToV2(nil) = %v, want a refusal naming AcquireStoreLock", err)
		}
	})
}
