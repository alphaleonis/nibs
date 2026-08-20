package cmd

import (
	"strings"
	"testing"
)

// TestHelpWorksOutsideAProject pins the command a user reaches for when there is
// no project yet.
//
// PersistentPreRunE resolved the store for every command not on its skip list, so
// `nibs help <cmd>` failed with "no .nibs directory found" anywhere outside a
// project — which is exactly where someone reads help to find out how to create
// one.
//
// `nibs --help` was never affected and is asserted alongside it: Cobra's ErrHelp
// path returns before the hooks run, so the FLAG and the SUBCOMMAND reach the same
// output by different routes and only one of them passed through the gate.
func TestHelpWorksOutsideAProject(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"help for a store command", []string{"help", "list"}, "list"},
		{"help for the root", []string{"help"}, "Usage"},
		{"the --help flag", []string{"--help"}, "Usage"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(resetRootPersistentFlags)
			resetRootPersistentFlags()
			// t.TempDir is under the OS temp root, not under this checkout, so
			// the upward walk cannot find the project's own store.
			t.Chdir(t.TempDir())
			t.Setenv("NIBS_PATH", "")

			out, err := runRootWith(t, tt.args...)
			if err != nil {
				t.Fatalf("%v outside a project: %v\nout: %s", tt.args, err, out)
			}
			if !strings.Contains(out, tt.want) {
				t.Errorf("%v printed no %q:\n%s", tt.args, tt.want, out)
			}
		})
	}
}

// TestCompletionNeedsNoStore pins the other half of the same fix through the
// predicate rather than end to end, and the reason is the harness, not the
// behavior: Cobra builds the completion subcommands lazily on the first Execute
// and closes them over the os.Stdout of that moment, so under captureStdout they
// write to whichever earlier test's pipe has since been closed. Running them
// through the root command therefore fails for a reason that has nothing to do
// with what is being tested.
//
// The predicate IS the fix. A shell sources completions on every new session,
// including in directories that are not projects, and each shell's subcommand had
// to pass the store gate to be generated.
//
// Matched by LINEAGE rather than by name on purpose: `nibs completion bash`
// executes the "bash" subcommand, so a name check would have to enumerate every
// shell and would miss the next one Cobra adds.
func TestCompletionNeedsNoStore(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			cmd, _, err := rootCmd.Find([]string{"completion", shell})
			if err != nil {
				t.Fatalf("completion %s is not a command: %v", shell, err)
			}
			if cmd.Name() != shell {
				t.Fatalf("Find resolved %q, not the %s subcommand", cmd.Name(), shell)
			}
			if !commandNeedsNoStore(cmd) {
				t.Errorf("`nibs completion %s` still resolves a store, so it fails outside a project", shell)
			}
		})
	}

	t.Run("a store command still needs one", func(t *testing.T) {
		cmd, _, err := rootCmd.Find([]string{"list"})
		if err != nil {
			t.Fatalf("finding list: %v", err)
		}
		if commandNeedsNoStore(cmd) {
			t.Error("`nibs list` no longer resolves a store; the predicate is too broad")
		}
	})
}
