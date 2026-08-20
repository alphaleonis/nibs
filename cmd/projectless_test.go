package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
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
			// --help is a LOCAL flag on root, so resetRootPersistentFlags (and
			// its drift guard) never sees it. Left set, it makes execute()
			// short-circuit to flag.ErrHelp for any later test whose target
			// command IS root — a failure with nothing in it to point here.
			t.Cleanup(func() {
				if f := rootCmd.Flags().Lookup("help"); f != nil {
					_ = f.Value.Set("false")
					f.Changed = false
				}
				rootCmd.SetArgs(nil)
			})
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

// TestCompletionSurvivesOutsideAProject pins the other half of the same fix
// through the PersistentPreRunE hook rather than end to end, and the reason is
// the harness, not the behavior: Cobra builds the completion subcommands lazily
// and closes them over the os.Stdout of that moment, so under captureStdout the
// second one writes to an earlier test's closed pipe. The failure is entirely in
// WRITING the script; driving the hook covers the store gate, which is the part
// this fix changes, without going near stdout.
//
// Two shapes are covered because they are two different rules. `nibs completion
// <shell>` runs once at install and needs no store; `__complete` runs on every
// TAB press and takes one when there is one, degrading quietly when there is not
// — a shell that gets an error back completes FILENAMES instead of subcommands.
func TestCompletionSurvivesOutsideAProject(t *testing.T) {
	// Cobra adds the completion tree during Execute, not at init, so a test that
	// only asks rootCmd.Find for it depends on some earlier test having run a
	// command. This call makes the test independent of what ran before it; it is
	// idempotent.
	rootCmd.InitDefaultCompletionCmd()

	run := func(t *testing.T, path []string) error {
		t.Helper()
		t.Cleanup(resetRootPersistentFlags)
		t.Cleanup(func() { rootCmd.SetArgs(nil) })
		resetRootPersistentFlags()
		t.Chdir(t.TempDir())
		t.Setenv("NIBS_PATH", "")

		target, _, err := rootCmd.Find(path)
		if err != nil {
			t.Fatalf("%v is not a command: %v", path, err)
		}
		return rootCmd.PersistentPreRunE(target, nil)
	}

	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run("completion "+shell, func(t *testing.T) {
			if err := run(t, []string{"completion", shell}); err != nil {
				t.Errorf("`nibs completion %s` outside a project: %v", shell, err)
			}
		})
	}

	t.Run("a TAB press", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		resetRootPersistentFlags()
		t.Chdir(t.TempDir())
		t.Setenv("NIBS_PATH", "")

		// Cobra builds the real __complete command inside ExecuteC through an
		// unexported initializer, so it cannot be materialized here the way
		// InitDefaultCompletionCmd materializes the `completion` tree. What the
		// hook actually keys on is the name, so a stand-in carrying it exercises
		// the same branch — and it is NOT added to rootCmd, which would leave a
		// hidden command behind for every later test in the package.
		probe := &cobra.Command{Use: cobra.ShellCompRequestCmd}
		if err := rootCmd.PersistentPreRunE(probe, nil); err != nil {
			t.Errorf("a completion request outside a project failed, so the shell completes filenames: %v", err)
		}
	})

	t.Run("a store command still needs a store", func(t *testing.T) {
		if err := run(t, []string{"list"}); err == nil {
			t.Error("`nibs list` no longer requires a store; the exemption is too broad")
		}
	})
}
