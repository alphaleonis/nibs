package nibcore

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/store"
)

// setupAreasCore builds a store with a declared prefix — a file named
// "tst-a001.md" reads its id back only under one, and a real store always
// declares one.
func setupAreasCore(t *testing.T) (*Core, string) {
	t.Helper()
	nibsDir := filepath.Join(t.TempDir(), store.DirName)
	if err := os.MkdirAll(store.NewLayout(nibsDir).DataDir(), 0o755); err != nil {
		t.Fatalf("creating the test store: %v", err)
	}
	core := New(nibsDir, config.DefaultWithPrefix("tst-"))
	core.SetWarnWriter(nil)
	return core, nibsDir
}

// writeStoreAreas writes the store's areas.yml the way an external editor does.
func writeStoreAreas(t *testing.T, nibsDir, body string) {
	t.Helper()
	if err := os.WriteFile(store.NewLayout(nibsDir).AreasPath(), []byte(body), 0o644); err != nil {
		t.Fatalf("writing areas.yml: %v", err)
	}
}

func TestLoadReadsTheAreasVocabulary(t *testing.T) {
	core, nibsDir := setupAreasCore(t)
	writeStoreAreas(t, nibsDir, "areas:\n    - name: web\n      children:\n        - name: dashboard\n")

	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !core.Areas().IsValid("web/dashboard") {
		t.Errorf("Areas().IsValid(web/dashboard) = false after Load, want true")
	}
}

func TestLoadRefusesAMalformedAreasVocabulary(t *testing.T) {
	core, nibsDir := setupAreasCore(t)
	writeStoreAreas(t, nibsDir, "areas:\n    - name: web\n    - name: web\n")

	if err := core.Load(); err == nil {
		t.Fatal("Load accepted a malformed vocabulary, want a refusal")
	}
}

// The bug this nib is about: a `nibs area rename` from another process rewrites
// the member nibs AND the vocabulary, and a live server used to take only the
// first half — so every later write to a renamed nib was refused against the
// vocabulary it read at startup.
func TestWatcherReloadsTheAreasVocabulary(t *testing.T) {
	core, nibsDir := setupAreasCore(t)
	writeStoreAreas(t, nibsDir, "areas:\n    - name: web\n")
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	created := &nib.Nib{ID: "tst-a001", Title: "In the web area", Status: "todo", Type: "task", Area: "web"}
	if err := core.Create(created); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := core.StartWatching(); err != nil {
		t.Fatalf("StartWatching: %v", err)
	}
	defer func() { _ = core.StopWatching() }()

	// The rename, as `nibs area rename web frontend` performs it: the members
	// are rewritten first, then the vocabulary.
	updated := created.Clone()
	updated.Area = "frontend"
	if err := writeNibFileFor(t, core, updated); err != nil {
		t.Fatalf("rewriting the member: %v", err)
	}
	writeStoreAreas(t, nibsDir, "areas:\n    - name: frontend\n")

	waitFor(t, "the reloaded vocabulary to declare frontend", func() bool {
		return core.Areas().IsValid("frontend")
	})

	if core.Areas().IsValid("web") {
		t.Error("Areas() still declares the retired path web")
	}
	// The symptom the reload exists to remove: a write to the renamed nib.
	subject, err := core.Get("tst-a001")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	subject.Title = "Renamed area, still writable"
	if err := core.Update(subject, nil); err != nil {
		t.Errorf("Update after the vocabulary reload = %v, want nil", err)
	}
}

// A vocabulary the loader cannot honor must not replace a good one: swapping in
// an empty tree would make every `area:` in the store undeclared and refuse
// every write, which is worse than the staleness it would be fixing.
//
// The refusal WARNING is what makes this observable. A later good write would
// restore the vocabulary and hide a clobber in between, so the test waits for
// the warning — which only the refusal path emits — and asserts on the
// vocabulary at that moment, with no second write to launder the result.
func TestWatcherKeepsTheLastGoodVocabularyOnAMalformedWrite(t *testing.T) {
	core, nibsDir := setupAreasCore(t)
	writeStoreAreas(t, nibsDir, "areas:\n    - name: web\n")
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	warnings := &syncBuffer{}
	core.SetWarnWriter(warnings)
	if err := core.StartWatching(); err != nil {
		t.Fatalf("StartWatching: %v", err)
	}
	defer func() { _ = core.StopWatching() }()

	writeStoreAreas(t, nibsDir, "areas:\n    - name: web\n    - name: web\n")

	waitFor(t, "the refusal of the malformed vocabulary", func() bool {
		return strings.Contains(warnings.String(), "keeping the areas vocabulary already loaded")
	})
	if !core.Areas().IsValid("web") {
		t.Error("web went missing, so the malformed write replaced the good vocabulary")
	}
}

// syncBuffer is a writer safe to share with the watcher goroutine.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// writeNibFileFor rewrites a nib's file directly, the way an external process
// does — the core learns about it through the watcher, not through Update.
func writeNibFileFor(t *testing.T, core *Core, b *nib.Nib) error {
	t.Helper()
	rendered, err := b.Render()
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(core.Root(), filepath.FromSlash(b.Path)), rendered, 0o644)
}
