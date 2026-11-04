package nibcore

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/alphaleonis/nibs/internal/area"
	"github.com/alphaleonis/nibs/internal/nib"
)

// readerDeadline bounds each read made while a mutation is parked. A read served
// from the published snapshot takes microseconds, so the margin is for a loaded
// machine under -race.
const readerDeadline = 200 * time.Millisecond

// waitForWriterHoldingMu returns once some goroutine holds c.mu, or fails the
// test after a second.
func waitForWriterHoldingMu(t *testing.T, c *Core) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for c.mu.TryLock() {
		c.mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatal("the mutator never took c.mu")
		}
		time.Sleep(time.Millisecond)
	}
}

// returnsWithin runs read on its own goroutine and reports whether it finished
// before the deadline. A read that misses it stays blocked until the test
// releases the file lock.
func returnsWithin(d time.Duration, read func()) bool {
	done := make(chan struct{})
	go func() {
		read()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

// A second descriptor holds the store's file lock, so an Update parks on it
// while holding c.mu. The search index is built first, because its lazy init is
// serialized with writers. See readState.
func TestReadersDoNotWaitForAParkedMutation(t *testing.T) {
	core, _ := setupTestCore(t)
	defer func() { _ = core.Close() }()
	createTestNib(t, core, "snap1", "Original title", "todo")
	if _, err := core.Search("Original"); err != nil {
		t.Fatalf("warming the search index: %v", err)
	}

	release, err := acquireFileLock(core.lockPath)
	if err != nil {
		t.Fatalf("holding the file lock: %v", err)
	}
	released := false
	defer func() {
		if !released {
			_ = release()
		}
	}()

	updateDone := make(chan error, 1)
	go func() {
		edit, err := core.GetForUpdate("snap1")
		if err != nil {
			updateDone <- err
			return
		}
		edit.Title = "Renamed title"
		updateDone <- core.Update(edit, nil)
	}()
	waitForWriterHoldingMu(t, core)

	var got *nib.Nib
	if !returnsWithin(readerDeadline, func() { got, _ = core.Get("snap1") }) {
		t.Fatal("Get waited for the parked mutation")
	}
	if got == nil || got.Title != "Original title" {
		t.Fatalf("Get during the mutation = %+v, want the published nib titled %q", got, "Original title")
	}
	var all []*nib.Nib
	if !returnsWithin(readerDeadline, func() { all = core.All() }) {
		t.Fatal("All waited for the parked mutation")
	}
	if len(all) != 1 {
		t.Fatalf("All during the mutation returned %d nibs, want 1", len(all))
	}
	var hits []*nib.Nib
	if !returnsWithin(readerDeadline, func() { hits, _ = core.Search("Original") }) {
		t.Fatal("Search waited for the parked mutation")
	}
	if len(hits) != 1 {
		t.Fatalf("Search during the mutation returned %d hits, want 1", len(hits))
	}

	select {
	case err := <-updateDone:
		t.Fatalf("Update finished while the file lock was held (err=%v)", err)
	default:
	}

	released = true
	if err := release(); err != nil {
		t.Fatalf("releasing the file lock: %v", err)
	}
	select {
	case err := <-updateDone:
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Update did not finish after the file lock was released")
	}

	if after, err := core.Get("snap1"); err != nil || after.Title != "Renamed title" {
		t.Fatalf("Get after the mutation = %+v, %v; want the title %q", after, err, "Renamed title")
	}
	hits, err = core.Search("Renamed")
	if err != nil || len(hits) != 1 {
		t.Fatalf("Search after the mutation = %d hits, %v; want 1", len(hits), err)
	}
}

// TestGetForUpdateWaitsForAParkedMutation parks writer A on the file lock while
// it holds c.mu, then lets writer B read, edit and Update the same nib with no
// ifMatch. B must clone the nib A leaves, or its write reverts A's change.
func TestGetForUpdateWaitsForAParkedMutation(t *testing.T) {
	core, nibsDir := setupTestCore(t)
	defer func() { _ = core.Close() }()
	createTestNib(t, core, "lost1", "Original title", "todo")

	release, err := acquireFileLock(core.lockPath)
	if err != nil {
		t.Fatalf("holding the file lock: %v", err)
	}
	released := false
	defer func() {
		if !released {
			_ = release()
		}
	}()

	edit := func(change func(*nib.Nib), cloned chan<- struct{}) <-chan error {
		done := make(chan error, 1)
		go func() {
			b, err := core.GetForUpdate("lost1")
			if cloned != nil {
				close(cloned)
			}
			if err != nil {
				done <- err
				return
			}
			change(b)
			done <- core.Update(b, nil)
		}()
		return done
	}
	aDone := edit(func(b *nib.Nib) { b.Title = "Renamed title" }, nil)
	waitForWriterHoldingMu(t, core)
	bCloned := make(chan struct{})
	bDone := edit(func(b *nib.Nib) { b.Status = "in-progress" }, bCloned)

	// Without the wait, B clones at once. With it, B stays blocked, and the
	// timeout only decides when to release A.
	select {
	case <-bCloned:
	case <-time.After(100 * time.Millisecond):
	}
	released = true
	if err := release(); err != nil {
		t.Fatalf("releasing the file lock: %v", err)
	}
	for name, done := range map[string]<-chan error{"A": aDone, "B": bDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("writer %s: %v", name, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("writer %s did not finish", name)
		}
	}

	got, err := core.Get("lost1")
	if err != nil || got.Title != "Renamed title" || got.Status != "in-progress" {
		t.Fatalf("Get = %+v, %v; want both edits", got, err)
	}
	reread := New(nibsDir, core.Config())
	reread.SetWarnWriter(nil)
	if err := reread.Load(); err != nil {
		t.Fatalf("reloading the store: %v", err)
	}
	if got, err := reread.Get("lost1"); err != nil || got.Title != "Renamed title" || got.Status != "in-progress" {
		t.Fatalf("on disk = %+v, %v; want both edits", got, err)
	}
}

// TestCurrentETagWaitsForAParkedDelete parks a Delete on the file lock while it
// holds c.mu. CurrentETag answers an if-match pre-check, so it must report the
// nib gone rather than the etag of the state the Delete is removing.
func TestCurrentETagWaitsForAParkedDelete(t *testing.T) {
	core, _ := setupTestCore(t)
	defer func() { _ = core.Close() }()
	createTestNib(t, core, "gone1", "Doomed", "todo")

	release, err := acquireFileLock(core.lockPath)
	if err != nil {
		t.Fatalf("holding the file lock: %v", err)
	}
	released := false
	defer func() {
		if !released {
			_ = release()
		}
	}()

	deleted := make(chan error, 1)
	go func() { deleted <- core.Delete("gone1") }()
	waitForWriterHoldingMu(t, core)

	type answer struct {
		etag string
		err  error
	}
	answered := make(chan answer, 1)
	go func() {
		etag, err := core.CurrentETag("gone1")
		answered <- answer{etag, err}
	}()

	// Without the wait, CurrentETag answers at once. With it, it stays blocked,
	// and the timeout only decides when to release the Delete.
	var got answer
	select {
	case got = <-answered:
	case <-time.After(100 * time.Millisecond):
		released = true
		if err := release(); err != nil {
			t.Fatalf("releasing the file lock: %v", err)
		}
		got = <-answered
	}
	if !errors.Is(got.err, ErrNotFound) {
		t.Errorf("CurrentETag during the Delete = %q, %v; want ErrNotFound", got.etag, got.err)
	}
	if !released {
		released = true
		_ = release()
	}
	if err := <-deleted; err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

// The confirm seam runs after a rename's cascade has rewritten its member and
// before areas.yml is written, so a reader there must still see the member and
// the vocabulary as they were before the edit. See readState.
func TestReadersSeeNoHalfOfAnAreaRename(t *testing.T) {
	core, _ := areaCoreWith(t, map[string]string{"nibs-aw01": "web"})

	type sight struct {
		memberArea          string
		declaresOld, newOne bool
	}
	look := func() sight {
		b, err := core.Get("nibs-aw01")
		if err != nil {
			t.Errorf("Get: %v", err)
			return sight{}
		}
		vocab := core.Areas()
		return sight{memberArea: b.Area, declaresOld: vocab.Exists("web"), newOne: vocab.Exists("platform")}
	}

	restore := confirmAreaMembersOnDisk
	t.Cleanup(func() { confirmAreaMembersOnDisk = restore })
	var during sight
	looked := false
	confirmAreaMembersOnDisk = func(c *Core, areas *area.Vocabulary, path string) ([]string, error) {
		looked = returnsWithin(readerDeadline, func() { during = look() })
		return restore(c, areas, path)
	}

	if _, err := core.UpdateArea(context.Background(), "web", area.NodeUpdate{NewName: ptrTo("platform")}); err != nil {
		t.Fatalf("UpdateArea: %v", err)
	}
	if !looked {
		t.Fatal("a read inside the edit waited for it")
	}
	if want := (sight{memberArea: "web", declaresOld: true}); during != want {
		t.Errorf("inside the edit, readers saw %+v, want the pre-edit %+v", during, want)
	}
	if got, want := look(), (sight{memberArea: "platform", newOne: true}); got != want {
		t.Errorf("after the edit, readers saw %+v, want %+v", got, want)
	}
}

// TestMentionReadsRaceAgainstBodyUpdates has writers rewrite mention bodies
// while readers query the mention index. Under -race, a writer that writes an
// inbound set it shares with a published index can then be reported.
func TestMentionReadsRaceAgainstBodyUpdates(t *testing.T) {
	core, _ := mustLoadPrefixedCore(t)
	defer func() { _ = core.Close() }()
	ids := []string{"nibs-mra1", "nibs-mra2", "nibs-mra3"}
	for _, id := range ids {
		if err := core.Create(&nib.Nib{ID: id, Title: id, Status: "todo", Body: "see #mra1 and #nibs-mra2"}); err != nil {
			t.Fatalf("Create(%s): %v", id, err)
		}
	}

	const rounds = 40
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range rounds {
				edit, err := core.GetForUpdate(id)
				if err != nil {
					t.Errorf("GetForUpdate(%s): %v", id, err)
					return
				}
				edit.Body = fmt.Sprintf("round %d: #mra%d and #nibs-mra%d", i, i%3+1, (i+1)%3+1)
				if err := core.Update(edit, nil); err != nil {
					t.Errorf("Update(%s): %v", id, err)
					return
				}
			}
		}()
	}
	stop := make(chan struct{})
	var readers sync.WaitGroup
	readers.Add(1)
	go func() {
		defer readers.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			for _, id := range ids {
				_ = core.FindMentions(id)
				_ = core.FindMentionedBy(id)
			}
			_ = core.CheckAllLinks()
		}
	}()
	wg.Wait()
	close(stop)
	readers.Wait()
}
