// Package testskip records the tests this suite skips because the machine
// running them cannot host their fixture, and reports the tally when the
// package's run ends.
//
// A SKIP IS INVISIBLE IN AN ORDINARY RUN, which is the whole reason this exists.
// `go test ./...` prints one `ok <pkg>` line for a package whose tests all pass
// or skip and DISCARDS that package's output entirely — only `-v`, or a failure,
// brings a skip reason into the log. So "every guard ran" and "a whole family of
// guards never executed" produce byte-identical output, and CI's windows leg is
// exactly that shape: `go test -count=1 ./...` with no `-v`, on a platform where
// creating a symlink commonly needs elevation or developer mode. Fifteen fixtures
// in this repo skip when os.Symlink fails, several of them the only coverage a
// store-resolution refusal has, and a green windows leg is no evidence that any
// of them ran.
//
// Two answers, because which one is honest depends on what the environment can
// actually do:
//
//   - REPORT. Every skip taken through this package is counted, and Report writes
//     the tally at the end of the package's run. Nothing at all is written when
//     nothing was skipped, so a normal run stays quiet. Because the go tool
//     swallows a passing package's stdout, NIBS_SKIP_REPORT names a file the
//     tally is appended to as well — that is what lets a CI job print the counts
//     as a step of its own instead of burying them in a `-v` transcript.
//   - REQUIRE. Setting a capability's env var (NIBS_REQUIRE_SYMLINKS=1) turns its
//     skips into failures. That is the setting for an environment where the
//     capability is known to be there: it converts "these guards may not have
//     run" into a build error the first time one stops running.
//
// The mechanism only measures what is enrolled in it, so a hand-rolled symlink
// skip would go back to being invisible. TestSymlinkSkipsAreEnrolled walks the
// module's test sources and fails on one.
package testskip

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

// TB is the slice of *testing.T this package uses.
//
// Deliberately NOT testing.TB, which carries an unexported method and therefore
// cannot be implemented outside the testing package. Both answers Unavailable
// gives are load-bearing — one skips, one fails — and a helper whose failing
// branch no test can observe is the same decoration this package exists to
// remove.
type TB interface {
	Helper()
	Skipf(format string, args ...any)
	Fatalf(format string, args ...any)
}

// Capability is something the machine must be able to DO before a fixture can be
// built at all — as opposed to something the code under test may or may not
// support, which is an ordinary assertion.
type Capability struct {
	// Name is what the report calls this capability.
	Name string
	// EnvVar, set to any non-empty value, makes a missing capability a FAILURE
	// rather than a skip.
	EnvVar string
}

var (
	// Symlinks covers both halves of what these fixtures need: os.Symlink
	// succeeding, and a filesystem that does not quietly resolve the link away.
	Symlinks = Capability{Name: "symlinks", EnvVar: "NIBS_REQUIRE_SYMLINKS"}

	// UnreadablePaths covers a file or directory this process cannot read.
	// chmod 0 denies nothing to root, and nothing at all on Windows, so a
	// fixture that needs an unreadable path is not buildable everywhere.
	UnreadablePaths = Capability{Name: "unreadable paths", EnvVar: "NIBS_REQUIRE_UNREADABLE_PATHS"}
)

var (
	mu      sync.Mutex
	skipped = map[Capability]int{}
)

// Unavailable records that t's fixture needs c, which this machine could not
// provide, and skips t — or FAILS t when c.EnvVar is set.
//
// The failure wording matters as much as the failure: an environment that
// declares a capability available and then cannot deliver it is a regression in
// the environment, not a limitation of the test, and the message has to say so
// or the next reader will "fix" it by dropping the variable.
func Unavailable(t TB, c Capability, format string, args ...any) {
	t.Helper()
	reason := fmt.Sprintf(format, args...)
	if os.Getenv(c.EnvVar) != "" {
		t.Fatalf("this guard needs %s, and %s is set — so this environment claims to have them, and skipping here would hide a guard that stopped running. Observed: %s",
			c.Name, c.EnvVar, reason)
		return
	}
	mu.Lock()
	skipped[c]++
	mu.Unlock()
	t.Skipf("%s unavailable, so this guard did not run: %s", c.Name, reason)
}

// SymlinkUnavailable is Unavailable for the shape every symlink fixture here
// takes: os.Symlink failed and there is no fixture without it.
func SymlinkUnavailable(t TB, err error) {
	t.Helper()
	Unavailable(t, Symlinks, "os.Symlink: %v", err)
}

// Report writes the tally of skips taken through this package, and writes
// nothing when there were none.
//
// Call it from a package's TestMain AFTER m.Run. It goes to stdout — which the
// go tool shows for a package run on its own or under `-v`, and discards for a
// passing package in a `./...` run — and, when NIBS_SKIP_REPORT names a file, is
// appended there too. The file is the half that survives `go test ./...`, and it
// is append-only precisely because every package in the run writes to it.
func Report() {
	reportTo(os.Stdout)
}

func reportTo(w io.Writer) {
	summary := tally()
	if summary == "" {
		return
	}
	// The report is advisory; a write that fails here must not take the run with
	// it, and there is nowhere better to report the failure to anyway.
	_, _ = fmt.Fprint(w, summary)

	path := os.Getenv("NIBS_SKIP_REPORT")
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "testskip: cannot write the skip report to %s: %v\n", path, err)
		return
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(summary); err != nil {
		fmt.Fprintf(os.Stderr, "testskip: cannot write the skip report to %s: %v\n", path, err)
	}
}

// tally renders the report, or the empty string when nothing was skipped.
func tally() string {
	mu.Lock()
	defer mu.Unlock()
	if len(skipped) == 0 {
		return ""
	}

	caps := make([]Capability, 0, len(skipped))
	total := 0
	for c, n := range skipped {
		caps = append(caps, c)
		total += n
	}
	sort.Slice(caps, func(i, j int) bool { return caps[i].Name < caps[j].Name })

	var b strings.Builder
	fmt.Fprintf(&b, "SKIPPED GUARDS in %s: %d did not run, because this machine could not build their fixture.\n", packageUnderTest(), total)
	for _, c := range caps {
		fmt.Fprintf(&b, "  %s: %d — set %s=1 where this capability is expected, and these become failures instead of skips.\n",
			c.Name, skipped[c], c.EnvVar)
	}
	return b.String()
}

// packageUnderTest names the package whose binary is running, read off that
// binary's own name so a TestMain does not have to restate it — a restated name
// is one more thing that can silently go stale after a package is moved.
func packageUnderTest() string {
	base := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	return strings.TrimSuffix(base, ".test")
}

// forget clears the tally so one test cannot see another's count.
func forget() {
	mu.Lock()
	defer mu.Unlock()
	skipped = map[Capability]int{}
}

// TB exists to be satisfied by *testing.T; this is what fails compilation rather
// than at the first call site if the two ever drift apart.
var _ TB = (*testing.T)(nil)
