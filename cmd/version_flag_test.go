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
// The two must RENDER IDENTICALLY: a build-identity string that differs by how it
// was asked for is one somebody parses wrongly.
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
	// Cobra adds the shorthand itself when root has no -v of its own, so it is a
	// live surface whether or not anyone intended it; pinned so it cannot vanish
	// in a cobra bump unnoticed.
	short := run(t, "-v")

	if strings.TrimSpace(sub) == "" {
		t.Fatal("`nibs version` printed nothing")
	}
	if strings.TrimSpace(flag) != strings.TrimSpace(sub) {
		t.Errorf("the two spellings disagree:\n  --version: %q\n  version:   %q", flag, sub)
	}
	if strings.TrimSpace(short) != strings.TrimSpace(sub) {
		t.Errorf("-v disagrees with the subcommand:\n  -v:      %q\n  version: %q", short, sub)
	}
	// The subcommand's shape is what carries commit and build date; pinning it
	// here is what stops the shared formatter being "simplified" into just the
	// version number.
	if !strings.HasPrefix(sub, "nibs ") || !strings.Contains(sub, "built ") {
		t.Errorf("`nibs version` no longer carries the build identity: %q", sub)
	}
}

// TestVersionAnswersAStoreThatRefusesEverythingElse pins the case the nib is
// built on and the no-store case cannot reach.
//
// The argument for --version is that a probe cannot tell a non-zero "unknown
// flag" apart from an absent binary. A store awaiting migration refuses every
// other command, so it is exactly where a probe most needs an answer — and the
// only reason it gets one is that cobra handles the version flag inside
// execute() BEFORE the PersistentPreRunE chain. That ordering is cobra's to
// change, not ours, which is why it is pinned here rather than reasoned about.
func TestVersionAnswersAStoreThatRefusesEverythingElse(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(func() {
		if f := rootCmd.Flags().Lookup("version"); f != nil {
			_ = f.Value.Set("false")
			f.Changed = false
		}
		rootCmd.SetArgs(nil)
	})
	resetRootPersistentFlags()
	_, storeDir := writeLegacyStore(t, "nibs:\n  prefix: leg-\n", map[string]string{
		"leg-a1--one.md": layoutNib,
	})

	// The control: this store refuses ordinary commands.
	if _, err := runRootWith(t, "--nibs-path", storeDir, "list"); err == nil {
		t.Fatal("the fixture no longer refuses `nibs list`, so this proves nothing")
	}

	resetRootPersistentFlags()
	out, err := runRootWith(t, "--nibs-path", storeDir, "--version")
	if err != nil {
		t.Fatalf("--version failed in a store awaiting migration, which is where a probe most needs it: %v", err)
	}
	if !strings.Contains(out, "nibs ") {
		t.Errorf("--version printed no build identity: %q", out)
	}
}
