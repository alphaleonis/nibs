package testskip

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recorder stands in for *testing.T so BOTH answers Unavailable gives can be
// observed. The failing one is the answer no ordinary run reaches — it needs the
// capability's env var set — and an unobserved failure branch is the same
// decoration this package was built to remove.
type recorder struct {
	skipped string
	failed  string
}

func (r *recorder) Helper() {}
func (r *recorder) Skipf(format string, args ...any) {
	r.skipped = fmt.Sprintf(format, args...)
}
func (r *recorder) Fatalf(format string, args ...any) {
	r.failed = fmt.Sprintf(format, args...)
}

func TestUnavailableSkipsAndCounts(t *testing.T) {
	forget()
	t.Cleanup(forget)
	t.Setenv(Symlinks.EnvVar, "")

	var r recorder
	SymlinkUnavailable(&r, errors.New("operation not permitted"))

	if r.failed != "" {
		t.Fatalf("Unavailable failed the test where the capability is not required: %s", r.failed)
	}
	if !strings.Contains(r.skipped, "operation not permitted") {
		t.Errorf("skip message = %q, want the observed reason in it", r.skipped)
	}
	if got := tally(); !strings.Contains(got, "symlinks: 1") {
		t.Errorf("tally = %q, want it to count the skip; an uncounted skip is exactly the invisibility this package exists to remove", got)
	}
}

// TestUnavailableFailsWhenTheCapabilityIsRequired pins the strict mode. Setting
// the variable is a claim about the environment, so a skip under it is a guard
// that stopped running rather than one that never could.
func TestUnavailableFailsWhenTheCapabilityIsRequired(t *testing.T) {
	forget()
	t.Cleanup(forget)
	t.Setenv(Symlinks.EnvVar, "1")

	var r recorder
	SymlinkUnavailable(&r, errors.New("operation not permitted"))

	if r.skipped != "" {
		t.Errorf("Unavailable skipped while %s is set: %s", Symlinks.EnvVar, r.skipped)
	}
	if !strings.Contains(r.failed, Symlinks.EnvVar) {
		t.Errorf("failure = %q, want it to name %s — otherwise the next reader unsets the wrong thing", r.failed, Symlinks.EnvVar)
	}
	// A failure is not a skip, so it must not inflate the tally either.
	if got := tally(); got != "" {
		t.Errorf("tally = %q after a required capability failed, want none", got)
	}
}

// TestReportIsSilentWhenNothingWasSkipped is why a normal Linux run stays quiet:
// the report has to cost nothing when there is nothing to report, or it becomes
// noise everyone learns to scroll past.
func TestReportIsSilentWhenNothingWasSkipped(t *testing.T) {
	forget()
	t.Cleanup(forget)

	path := filepath.Join(t.TempDir(), "report.txt")
	t.Setenv("NIBS_SKIP_REPORT", path)

	var out strings.Builder
	reportTo(&out)

	if out.String() != "" {
		t.Errorf("reportTo wrote %q with nothing skipped, want nothing", out.String())
	}
	if _, err := os.Stat(path); err == nil {
		t.Errorf("reportTo created %s with nothing skipped, want no file", path)
	}
}

// TestReportReachesTheFileThatSurvivesAPackageListRun pins the half that makes
// the count readable at all in CI: `go test ./...` discards a passing package's
// stdout, so the file is the only channel a green run can carry this on.
func TestReportReachesTheFileThatSurvivesAPackageListRun(t *testing.T) {
	forget()
	t.Cleanup(forget)

	path := filepath.Join(t.TempDir(), "report.txt")
	t.Setenv("NIBS_SKIP_REPORT", path)
	t.Setenv(Symlinks.EnvVar, "")

	var r recorder
	SymlinkUnavailable(&r, errors.New("operation not permitted"))

	var out strings.Builder
	reportTo(&out)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the skip report: %v", err)
	}
	for _, want := range []string{"SKIPPED GUARDS", "symlinks: 1", Symlinks.EnvVar} {
		if !strings.Contains(string(data), want) {
			t.Errorf("skip report = %q, want %q in it", data, want)
		}
	}
	if string(data) != out.String() {
		t.Errorf("the file and the stdout report differ:\nfile:   %q\nstdout: %q", data, out.String())
	}

	// APPEND, not truncate: every package in a `./...` run writes to the same
	// file, so a second write must not erase the first.
	reportTo(&out)
	again, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("re-reading the skip report: %v", err)
	}
	if len(again) <= len(data) {
		t.Errorf("the second report did not append (%d bytes, was %d); one package's tally would overwrite another's", len(again), len(data))
	}
}
