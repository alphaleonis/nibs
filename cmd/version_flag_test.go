package cmd

import (
	"strings"
	"testing"
)

// TestVersionFlagMatchesTheSubcommand pins the two spellings against each other.
//
// `--version` is the near-universal convention, so it is what a person tries
// first and what tooling reaches for without checking. Rejecting it with
// "unknown flag" exits NON-ZERO, which a probe cannot tell apart from nibs being
// absent — the failure mode is "nibs is broken", not "try another spelling".
//
// The two must render identically, from one formatter: a build-identity string
// that differs by how it was asked for is a string someone will parse wrongly.
func TestVersionFlagMatchesTheSubcommand(t *testing.T) {
	// --version is a LOCAL flag on root, like --help, so resetRootPersistentFlags
	// cannot reach it. Left set, it makes every later execution whose target is
	// root short-circuit to printing the version (see the same hazard for --help
	// in cmd/projectless_test.go).
	resetVersionFlag := func() {
		if f := rootCmd.Flags().Lookup("version"); f != nil {
			_ = f.Value.Set("false")
			f.Changed = false
		}
		rootCmd.SetArgs(nil)
	}

	run := func(t *testing.T, args ...string) string {
		t.Helper()
		t.Cleanup(resetRootPersistentFlags)
		t.Cleanup(resetVersionFlag)
		resetRootPersistentFlags()
		// No store anywhere above: version identifies the BINARY, so it must not
		// depend on where it is run from.
		t.Chdir(t.TempDir())
		t.Setenv("NIBS_PATH", "")

		out, err := runRootWith(t, args...)
		if err != nil {
			t.Fatalf("nibs %v: %v\nout: %s", args, err, out)
		}
		return out
	}

	sub := run(t, "version")
	flag := run(t, "--version")

	if strings.TrimSpace(sub) == "" {
		t.Fatal("`nibs version` printed nothing")
	}
	if strings.TrimSpace(flag) != strings.TrimSpace(sub) {
		t.Errorf("the two spellings disagree:\n  --version: %q\n  version:   %q", flag, sub)
	}
	// The subcommand's shape is what carries commit and build date; pinning it
	// here is what stops the shared formatter being "simplified" into just the
	// version number.
	if !strings.HasPrefix(sub, "nibs ") || !strings.Contains(sub, "built ") {
		t.Errorf("`nibs version` no longer carries the build identity: %q", sub)
	}
}
