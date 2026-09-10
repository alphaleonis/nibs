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

// reloadUnderLock installs the store's vocabulary the way every caller of
// loadAreasLocked does — with c.mu held.
func reloadUnderLock(core *Core) error {
	core.mu.Lock()
	defer core.mu.Unlock()
	return core.loadAreasLocked()
}

// TestReloadAreasReportsAFileItCannotRead is the writer's half of the same
// refusal TestWatcherKeepsTheLastGoodVocabularyOnAMalformedWrite pins for the
// watcher: the vocabulary already loaded is kept either way, and the difference
// is who is told. An area edit that has just replaced the file has a caller
// waiting, and answering it from the vocabulary this call could not replace is
// how an edit reports success while showing the state before it.
func TestReloadAreasReportsAFileItCannotRead(t *testing.T) {
	core, nibsDir := setupAreasCore(t)
	writeStoreAreas(t, nibsDir, "areas:\n    - name: web\n")
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	writeStoreAreas(t, nibsDir, "areas:\n    - name: web\n    - name: web\n")
	if err := reloadUnderLock(core); err == nil {
		t.Fatal("the reload accepted a vocabulary the loader refuses")
	}
	if !core.Areas().IsValid("web") {
		t.Error("the refused reload replaced the vocabulary already loaded")
	}

	// And a readable file still reloads, so the error above is the refusal and
	// not this method reporting one for every call.
	writeStoreAreas(t, nibsDir, "areas:\n    - name: platform\n")
	if err := reloadUnderLock(core); err != nil {
		t.Fatalf("reloading over a good file: %v", err)
	}
	if !core.Areas().IsValid("platform") {
		t.Error("the vocabulary was not reloaded")
	}
}

// TestTheWatchersReloadDoesNotInstallOverAHeldStoreLock pins the half of the
// vocabulary install the race detector cannot speak for. c.areas is an atomic
// pointer, so a reload that reads areas.yml, is descheduled while an area edit
// writes and installs a newer vocabulary, and then stores what it read is a LOST
// UPDATE and not a data race — memory ends up a vocabulary behind disk, the
// edit's own file event has already been spent, and every write to the nibs that
// edit's cascade moved is refused from then on.
//
// What makes that impossible is the install being one step against the other
// installer, so that is what is asserted: while an edit's critical section is
// open, the vocabulary in memory does not move, however far behind disk it is.
func TestTheWatchersReloadDoesNotInstallOverAHeldStoreLock(t *testing.T) {
	core, nibsDir := setupAreasCore(t)
	writeStoreAreas(t, nibsDir, "areas:\n    - name: web\n")
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Stand in for an area edit mid-flight: editArea holds c.mu from before it
	// re-reads the store until after it has installed the vocabulary it wrote.
	core.mu.Lock()
	writeStoreAreas(t, nibsDir, "areas:\n    - name: platform\n")

	done := make(chan struct{})
	go func() {
		defer close(done)
		core.watchReloadAreas()
	}()

	// One file read, so a reload that takes no lock finishes far inside this;
	// one that takes c.mu cannot finish inside any window at all.
	select {
	case <-done:
		t.Error("the watcher's reload ran to completion while the store's lock was held")
	case <-time.After(200 * time.Millisecond):
	}
	if core.Areas().IsValid("platform") {
		t.Fatal("the watcher's reload installed a vocabulary while an edit held the store: an edit installing its own vocabulary next is then reverted by whichever of the two stores last")
	}

	core.mu.Unlock()
	<-done
	if !core.Areas().IsValid("platform") {
		t.Error("the watcher's reload installed nothing once the store's lock was free")
	}
}

// TestARefusedAreaEditTicksTheVocabularyItInstalled: every area verb re-reads the
// store under its write lock, so a verb that goes on to be REFUSED has still
// installed whatever the file then declared. A silent install is not repaired by
// the watcher's later reload — that one compares against what is already
// installed, finds it equal and ticks nobody either — so the vocabulary moves in
// memory while every open client keeps offering the retired one.
func TestARefusedAreaEditTicksTheVocabularyItInstalled(t *testing.T) {
	core, nibsDir := setupAreasCore(t)
	writeStoreAreas(t, nibsDir, "areas:\n    - name: web\n")
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	ticks, unsubscribe := core.SubscribeAreas()
	defer unsubscribe()

	// Another process retires `web` and declares `ops` in its place, which is
	// what makes the retire below refuse: the path it names is gone.
	writeStoreAreas(t, nibsDir, "areas:\n    - name: ops\n")

	if _, err := core.RemoveArea("web", AreaDisposition{}); err == nil {
		t.Fatal("retiring an area this store no longer declares was accepted")
	}
	if !core.Areas().IsValid("ops") {
		t.Fatal("the refused verb did not install the vocabulary it re-read")
	}

	select {
	case <-ticks:
	default:
		t.Fatal("the refused verb installed a vocabulary no subscriber was told about, and the watcher's next reload finds it already equal and ticks nobody either")
	}
}
