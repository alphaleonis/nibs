package cmd

import (
	"bytes"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/spf13/pflag"
)

// resetRootPersistentFlags restores rootCmd's persistent flag state
// (--nibs-path, --config) to defaults so tests don't leak each other.
// Call via t.Cleanup() from any helper that runs rootCmd.Execute() with
// persistent flags set.
//
// This is an explicit two-flag reset — adding a third persistent flag to
// rootCmd requires touching the name list below. The companion regression
// test TestResetRootPersistentFlagsClearsAllState walks
// rootCmd.PersistentFlags().VisitAll to assert every persistent flag is
// at its default after the reset, so a fourth flag added without being
// included here will trip that test.
//
// What this resets that per-command reset*Flags() helpers miss:
//   - The package-level `nibsPath` and `configPath` string vars.
//   - The pflag Value backing each persistent flag (Cobra's flag parser
//     writes to Value separately from the bound var, so clearing only
//     the bound var leaves Value populated).
//   - The pflag Changed bit (matters for MarkFlagsMutuallyExclusive
//     and any future code that consults Visit).
func resetRootPersistentFlags() {
	nibsPath = ""
	configPath = ""
	for _, name := range []string{"nibs-path", "config"} {
		f := rootCmd.PersistentFlags().Lookup(name)
		if f == nil {
			continue
		}
		// Set(DefValue) is infallible for these StringVar flags — pflag's
		// stringValue.Set never returns a non-nil error. Swallowing the
		// error is provably safe given the explicit, known-string flag
		// list above; if a future persistent flag has a Set that can
		// fail, this swallow needs revisiting.
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	}
}

// TestResetRootPersistentFlagsClearsAllState pins the contract of
// resetRootPersistentFlags: after dirtying both --nibs-path and --config
// via rootCmd.PersistentFlags().Set (the path the real flag parser
// uses), every observable bit of state must be back to default.
func TestResetRootPersistentFlagsClearsAllState(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)

	// Dirty via the real FlagSet so Cobra's `actual` map is populated.
	if err := rootCmd.PersistentFlags().Set("nibs-path", "/tmp/leaked"); err != nil {
		t.Fatalf("pre-populate --nibs-path: %v", err)
	}
	if err := rootCmd.PersistentFlags().Set("config", "/tmp/leaked.yml"); err != nil {
		t.Fatalf("pre-populate --config: %v", err)
	}

	resetRootPersistentFlags()

	if nibsPath != "" {
		t.Errorf("nibsPath = %q after reset, want empty", nibsPath)
	}
	if configPath != "" {
		t.Errorf("configPath = %q after reset, want empty", configPath)
	}
	rootCmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		if f.Value.String() != f.DefValue {
			t.Errorf("persistent flag %q = %q after reset, want default %q",
				f.Name, f.Value.String(), f.DefValue)
		}
		if f.Changed {
			t.Errorf("persistent flag %q Changed = true after reset, want false", f.Name)
		}
	})
}

// stdoutMu serializes global os.Stdout mutations across tests that need to
// observe writes from the output package (which bypasses Cobra's writers).
// The mutex neutralizes the race if anyone later adds t.Parallel() to a
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
