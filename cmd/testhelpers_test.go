package cmd

import (
	"bytes"
	"io"
	"os"
	"sync"
	"testing"
	"time"
)

// stdoutMu serializes global os.Stdout mutations across tests that need to
// observe writes from the output package (which bypasses Cobra's writers).
// The mutex neutralises the race if anyone later adds t.Parallel() to a
// test in this package — stdout itself is process-global, so concurrent
// swaps would silently steal each other's output without this guard.
var stdoutMu sync.Mutex

// captureStdout captures writes made directly to os.Stdout while fn runs
// (used for output.Success*/Error which bypass Cobra's configured writers).
// NOT safe under t.Parallel() — the package-level mutex guards against
// concurrent swaps within the same process, but stdout itself is still
// global.
//
// The os.Stdout restore and writer close are deferred so a panic inside
// fn() cannot leave stdout redirected for subsequent tests. The final
// read is wrapped in a timeout so a hung pipe fails fast per-test rather
// than ticking over to the suite-level timeout with no diagnostic.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	stdoutMu.Lock()
	defer stdoutMu.Unlock()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	// Double-close guard: the deferred close runs after a panic inside
	// fn() (where the explicit close below never reached). On the normal
	// path the flag is flipped and the defer becomes a no-op. Without
	// the guard a stray second Close() would hit an already-closed fd
	// and, under concurrent OS fd reuse, could race with whichever file
	// has inherited the fd slot.
	//
	// Goroutine-leak invariant: the draining goroutine above exits when
	// io.Copy returns, which happens on pipe EOF — and the pipe reaches
	// EOF as soon as its write end is closed. The guard ensures exactly
	// one close happens on every exit path (normal, panic, timeout), so
	// the goroutine cannot leak. If you refactor this (e.g. remove the
	// deferred close, or swap the pipe for a bounded buffer), preserve
	// the "exactly one close on every exit path" contract.
	closed := false
	defer func() {
		if !closed {
			_ = w.Close()
		}
	}()
	fn()
	_ = w.Close()
	closed = true

	select {
	case s := <-done:
		return s
	case <-time.After(5 * time.Second):
		t.Fatal("captureStdout timed out waiting for goroutine (pipe deadlocked?)")
		return ""
	}
}
