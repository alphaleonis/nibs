package nibcore

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// countWatchLoops reports how many goroutines are currently executing
// watchLoop, read out of a full goroutine dump by frame name.
//
// runtime.NumGoroutine is unusable here: it also counts fsnotify's internal
// reader goroutines and runtime timer goroutines, which come and go on their
// own schedule, so its value swings independently of the leak under test.
//
// The trailing paren in the frame name is load-bearing. Go names a function
// literal after its parent — watchLoop's deferred Close and its time.AfterFunc
// debounce callback appear as watchLoop.func1 and watchLoop.func2 — so an
// unanchored match also counts those. The debounce callback runs on its own
// goroutine, so a census sampled while it is in flight would read one loop too
// many. Only the real call frame carries the argument list.
func countWatchLoops() int {
	// Dumps here run to a few KB; start small and grow rather than allocating a
	// megabyte on every poll.
	buf := make([]byte, 64<<10)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return bytes.Count(buf[:n], []byte("nibcore.(*Core).watchLoop("))
		}
		buf = make([]byte, 2*len(buf))
	}
}

// waitForWatchLoops polls the goroutine census until it reads want, returning
// the last count seen. A retiring watchLoop needs a moment to notice its done
// channel closed, so a single sample would be racy; an orphaned one never
// settles at all, because nothing will ever close the channel it has latched
// onto.
func waitForWatchLoops(want int, timeout time.Duration) (int, bool) {
	deadline := time.Now().Add(timeout)
	for {
		n := countWatchLoops()
		if n == want {
			return n, true
		}
		if !time.Now().Before(deadline) {
			return n, false
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// restartTwice drives Unwatch -> StartWatching twice with nothing in between.
//
// The second restart is what does the work. The first one spawns a loop that
// has almost certainly not reached its select yet, and the second reassigns
// c.done while it is still short of it. A loop that binds its exit channel at
// the `go` statement is unaffected; a loop that reads c.done at select entry
// reads the successor's channel instead of its own and never exits.
//
// A single restart per cycle only exercises this on the very first cycle:
// afterwards the caller's census poll hands every fresh loop the microseconds
// it needs to park in select, and a parked loop has already evaluated its channel
// operand, so closing it wakes the select and the loop returns without ever
// re-reading the field. That is why a quiet restart proves nothing, and why
// this helper never waits between the pair.
func restartTwice(t *testing.T, core *Core, cycle int) {
	t.Helper()
	for i := range 2 {
		if err := core.Unwatch(); err != nil {
			t.Fatalf("cycle %d: Unwatch() #%d error = %v", cycle, i+1, err)
		}
		if err := core.StartWatching(); err != nil {
			t.Fatalf("cycle %d: StartWatching() #%d error = %v", cycle, i+1, err)
		}
	}
}

// TestRestartWatchingDoesNotOrphanWatchLoop covers Unwatch -> StartWatching.
// watchLoop must exit on the done channel it was started with, not on whatever
// c.done happens to hold when it next reaches the select — otherwise a restart
// leaves the old loop running against a fresh, open channel, leaking the
// goroutine and the fsnotify watcher and file descriptors its deferred Close
// would have released, and double-servicing the directory.
func TestRestartWatchingDoesNotOrphanWatchLoop(t *testing.T) {
	const restartCycles = 50

	core, _ := setupTestCore(t)

	if err := core.StartWatching(); err != nil {
		t.Fatalf("StartWatching() error = %v", err)
	}
	defer func() { _ = core.Unwatch() }()

	for cycle := range restartCycles {
		restartTwice(t, core, cycle)

		if n, ok := waitForWatchLoops(1, 2*time.Second); !ok {
			t.Fatalf("cycle %d: %d watchLoop goroutines live after restart, want 1 — "+
				"the pre-restart loop was orphaned onto the new done channel", cycle, n)
		}
	}
}

// collectBatchArrivals reads ch for the whole window and returns the arrival
// time of every batch it receives. It keeps batch boundaries — unlike
// collectNibEvents, which flattens them — because the duplicate-loop symptom is
// *when* batches land relative to each other, not what they carry. fanOut never
// delivers an empty batch, and this test's only filesystem activity is its one
// write, so every batch received here is that write surfacing.
func collectBatchArrivals(ch <-chan []NibEvent, window time.Duration) []time.Time {
	var times []time.Time
	deadline := time.After(window)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return times
			}
			times = append(times, time.Now())
		case <-deadline:
			return times
		}
	}
}

// TestRestartWatchingDoesNotDuplicateEvents is the user-visible half of the
// same leak: an orphaned loop keeps servicing the directory, so a single write
// is debounced, handled and fanned out once per live loop. The census above
// asserts the cause; this asserts the symptom. The two are separable, because
// each loop owns its own pendingChanges map and debounce timer.
//
// The subscription must be taken after the restart: unwatchLocked closes and
// drops every subscriber channel, and StartWatching does not restore them.
func TestRestartWatchingDoesNotDuplicateEvents(t *testing.T) {
	// Each round is an independent chance to orphan a loop, and one orphan is
	// enough to double-deliver. A round that fails to orphan simply sees one
	// batch, the same as a healthy watch.
	const rounds = 3

	core, nibsDir := setupTestCore(t)

	if err := core.StartWatching(); err != nil {
		t.Fatalf("StartWatching() error = %v", err)
	}
	defer func() { _ = core.Unwatch() }()

	for round := range rounds {
		restartTwice(t, core, round)

		events, unsubscribe := core.Subscribe()

		path := filepath.Join(nibsDir, fmt.Sprintf("dup%d--duplicate.md", round))
		body := fmt.Sprintf("---\ntitle: Duplicate %d\nstatus: todo\n---\n", round)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("round %d: writing nib to drive the watch: %v", round, err)
		}

		// Presence, on a generous timeout, recording when the first batch lands. A
		// round that never sees the write has tested nothing and must say so rather
		// than pass quietly.
		var arrivals []time.Time
		presence := time.After(2 * time.Second)
	awaitFirst:
		for {
			select {
			case _, ok := <-events:
				if !ok {
					t.Fatalf("round %d: subscription closed while watching", round)
				}
				arrivals = append(arrivals, time.Now())
				break awaitFirst
			case <-presence:
				t.Fatalf("round %d: no event batch delivered for a written nib — "+
					"the watch never observed the write, so this round proves nothing", round)
			}
		}

		// Then collect any further batches over a window well past the debounce,
		// without unsubscribing early: a late duplicate must not be dropped the way
		// a hard absence window plus an immediate unsubscribe would drop it.
		arrivals = append(arrivals, collectBatchArrivals(events, 4*debounceDelay)...)

		// One servicing loop cannot deliver two batches for a single write closer
		// together than a full debounce window: its timer only re-arms on a fresh
		// event and then waits the whole delay again, so its successive batches are
		// always more than debounceDelay apart. That is exactly the shape of the
		// Windows false-failure — an fsnotify Create/Write that splits past the
		// 100 ms debounce surfaces as two *spread* batches from the one loop, and
		// must not fail this test. An orphaned loop is different in kind: it and its
		// successor are armed by the same events at the same instant and fire
		// together, so their batches land within a few milliseconds of each other.
		// A gap below half the debounce window therefore isolates a real duplicate —
		// a second loop servicing the directory — from a benign split.
		for i := 1; i < len(arrivals); i++ {
			if gap := arrivals[i].Sub(arrivals[i-1]); gap < debounceDelay/2 {
				t.Fatalf("round %d: two batches for one write landed %v apart (< %v) — "+
					"an orphaned watchLoop is servicing the directory alongside its successor",
					round, gap, debounceDelay/2)
			}
		}

		unsubscribe()
	}
}
