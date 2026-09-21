package cmd

import (
	"slices"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/updatecheck"
	"github.com/spf13/cobra"
)

func TestUpdateNotifyEligible(t *testing.T) {
	cases := []struct {
		name    string
		cmdName string
		jsonSet bool
		isTTY   bool
		isGroup bool
		want    bool
	}{
		{"interactive list on a tty", "list", false, true, false, true},
		// Real command names throughout: maybeNotifyUpdate passes cmd.Name(),
		// which never returns an alias, so a row naming one would describe an
		// input this function cannot receive. `get` is the command `nibs show`
		// reaches.
		{"get on a tty", "get", false, true, false, true},
		{"not a tty (piped)", "list", false, false, false, false},
		{"json output suppresses", "list", true, true, false, false},
		{"json and non-tty", "list", true, false, false, false},
		{"the web server is skipped", "web", false, true, false, false},
		{"the query command is skipped", "query", false, true, false, false},
		{"tui is skipped (in-app indicator)", "tui", false, true, false, false},
		{"completion machinery is skipped", "__complete", false, true, false, false},
		// A group reached this function only after it was made runnable, so its
		// name is on no skip list and nothing else would stay its hand.
		{"a group typed bare is skipped", "area", false, true, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := updateNotifyEligible(tc.cmdName, tc.jsonSet, tc.isTTY, tc.isGroup); got != tc.want {
				t.Errorf("updateNotifyEligible(%q, json=%v, tty=%v, group=%v)=%v, want %v",
					tc.cmdName, tc.jsonSet, tc.isTTY, tc.isGroup, got, tc.want)
			}
		})
	}
}

func TestWriteUpdateNotice(t *testing.T) {
	avail := updatecheck.Result{Current: "v0.5.0", Latest: "v0.6.0", UpdateAvailable: true}

	t.Run("prints the notice when eligible and available", func(t *testing.T) {
		var b strings.Builder
		writeUpdateNotice(&b, true, avail, true)
		got := b.String()
		if !strings.Contains(got, "v0.6.0") || !strings.Contains(got, "nibs upgrade") {
			t.Errorf("notice missing version or upgrade hint: %q", got)
		}
	})

	t.Run("silent when not eligible", func(t *testing.T) {
		var b strings.Builder
		writeUpdateNotice(&b, false, avail, true)
		if b.Len() != 0 {
			t.Errorf("expected no output when ineligible, got %q", b.String())
		}
	})

	t.Run("silent when the check had no opinion", func(t *testing.T) {
		var b strings.Builder
		writeUpdateNotice(&b, true, updatecheck.Result{}, false)
		if b.Len() != 0 {
			t.Errorf("expected no output when ok=false, got %q", b.String())
		}
	})

	t.Run("silent when no update is available", func(t *testing.T) {
		var b strings.Builder
		writeUpdateNotice(&b, true, updatecheck.Result{Current: "v0.6.0", Latest: "v0.6.0"}, true)
		if b.Len() != 0 {
			t.Errorf("expected no output when up to date, got %q", b.String())
		}
	})
}

func TestJSONFlagSet(t *testing.T) {
	t.Run("flag present and true", func(t *testing.T) {
		cmd := &cobra.Command{Use: "x"}
		var v bool
		cmd.Flags().BoolVar(&v, "json", false, "")
		if err := cmd.Flags().Set("json", "true"); err != nil {
			t.Fatal(err)
		}
		if !jsonFlagSet(cmd) {
			t.Error("expected jsonFlagSet=true when --json=true")
		}
	})

	t.Run("flag present but false", func(t *testing.T) {
		cmd := &cobra.Command{Use: "x"}
		var v bool
		cmd.Flags().BoolVar(&v, "json", false, "")
		if jsonFlagSet(cmd) {
			t.Error("expected jsonFlagSet=false when --json defaults false")
		}
	})

	t.Run("no json flag", func(t *testing.T) {
		cmd := &cobra.Command{Use: "x"}
		if jsonFlagSet(cmd) {
			t.Error("expected jsonFlagSet=false when there is no --json flag")
		}
	})
}

// TestUpdateNotifySkipKeysRealCommandNames pins every key in the skip map against
// the tree it is supposed to name.
//
// The map keyed the server as "serve", which is an ALIAS: the command's Use is
// "web" (cmd/serve.go), and maybeNotifyUpdate passes cmd.Name(), which returns
// the real name whichever spelling the user typed. So the entry never fired, and
// nothing said so — a key that matches nothing looks exactly like a key that
// matches something, which is why this is a test rather than a careful reading.
//
// The same shape sits one line below: the GraphQL command's Use is "query" with
// "graphql" as its alias.
func TestUpdateNotifySkipKeysRealCommandNames(t *testing.T) {
	// Cobra registers the help and completion commands during Execute, not at
	// init, so a test that only reads rootCmd.Commands() would otherwise depend
	// on some earlier test in the package having run one. Both are idempotent.
	rootCmd.InitDefaultHelpCmd()
	rootCmd.InitDefaultCompletionCmd()

	real := map[string]bool{
		// The one name cobra owns that is genuinely absent from Commands():
		// initCompleteCmd builds it inside ExecuteC through an unexported
		// initializer. Its no-descriptions spelling is NOT exempt — that is an
		// alias of this same command, so keying it would be the very defect
		// this test exists to catch.
		cobra.ShellCompRequestCmd: true,
	}
	var aliases []string
	for _, c := range rootCmd.Commands() {
		real[c.Name()] = true
		aliases = append(aliases, c.Aliases...)
	}
	isAlias := func(key string) bool { return slices.Contains(aliases, key) }

	// Without this the test passes vacuously on an empty map: every key being
	// valid is trivially true when there are none, and only `web` is pinned by a
	// behavior test.
	for _, want := range []string{"web", "query", "tui", "upgrade", "help", "completion", cobra.ShellCompRequestCmd} {
		if !updateNotifySkip[want] {
			t.Errorf("updateNotifySkip lost its %q entry", want)
		}
	}

	for key := range updateNotifySkip {
		if real[key] {
			continue
		}
		if isAlias(key) {
			t.Errorf("updateNotifySkip keys %q, which is an ALIAS — cmd.Name() never returns it, so the entry never fires", key)
			continue
		}
		t.Errorf("updateNotifySkip keys %q, which names no command in the tree", key)
	}
}

// TestUpdateNotifySkipCoversSubtrees pins what an entry means: the command it
// names AND everything under it. `nibs completion bash` executes the "bash"
// subcommand, so before updateNotifyKey the "completion" entry covered only the
// bare `nibs completion` and every shell fell through it.
func TestUpdateNotifySkipCoversSubtrees(t *testing.T) {
	rootCmd.InitDefaultCompletionCmd()

	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run("completion "+shell, func(t *testing.T) {
			cmd, _, err := rootCmd.Find([]string{"completion", shell})
			if err != nil {
				t.Fatalf("completion %s is not a command: %v", shell, err)
			}
			if updateNotifyEligible(updateNotifyKey(cmd), false, true, cmd.HasSubCommands()) {
				t.Errorf("`nibs completion %s` would print an update notice into the script it emits", shell)
			}
		})
	}

	t.Run("an ordinary command is untouched", func(t *testing.T) {
		cmd, _, err := rootCmd.Find([]string{"list"})
		if err != nil {
			t.Fatalf("finding list: %v", err)
		}
		if !updateNotifyEligible(updateNotifyKey(cmd), false, true, cmd.HasSubCommands()) {
			t.Error("`nibs list` stopped being eligible; the subtree walk is too broad")
		}
	})
}

// TestUpdateNotifyStaysQuietForGroups holds the gate against the groups it was
// added for, and against the verbs it must not take with them.
//
// `nibs area` and `nibs config` carry a RunE so their Args validator can refuse
// an unrecognized verb, and a runnable command reaches root's PersistentPostRun
// — so the notifier, which had never run for either, now does. Their only own
// output is help, which is the thing the "help" skip entry exists to keep clean.
func TestUpdateNotifyStaysQuietForGroups(t *testing.T) {
	quiet := []string{"area", "config"}
	loud := [][]string{{"area", "list"}, {"config", "set-prefix"}}

	for _, group := range quiet {
		t.Run("bare "+group, func(t *testing.T) {
			cmd, _, err := rootCmd.Find([]string{group})
			if err != nil {
				t.Fatalf("`nibs %s` is not a command: %v", group, err)
			}
			// isTTY and no --json: every other reason to stay quiet is off.
			if updateNotifyEligible(updateNotifyKey(cmd), false, true, cmd.HasSubCommands()) {
				t.Errorf("bare `nibs %s` would append an update notice to its help text", group)
			}
		})
	}

	for _, verb := range loud {
		t.Run(strings.Join(verb, " "), func(t *testing.T) {
			cmd, _, err := rootCmd.Find(verb)
			if err != nil {
				t.Fatalf("`nibs %s` is not a command: %v", strings.Join(verb, " "), err)
			}
			if !updateNotifyEligible(updateNotifyKey(cmd), false, true, cmd.HasSubCommands()) {
				t.Errorf("`nibs %s` stopped being eligible; the group gate reaches too far",
					strings.Join(verb, " "))
			}
		})
	}
}

// TestUpdateNotifySkipsTheServerUnderEitherSpelling is the behavior the map
// exists for: the server's output must stay clean, and a user may reach it by
// either name.
func TestUpdateNotifySkipsTheServerUnderEitherSpelling(t *testing.T) {
	for _, spelling := range []string{"web", "serve"} {
		t.Run(spelling, func(t *testing.T) {
			cmd, _, err := rootCmd.Find([]string{spelling})
			if err != nil {
				t.Fatalf("`nibs %s` is not a command: %v", spelling, err)
			}
			// isTTY and no --json: every other reason to stay quiet is off, so
			// the skip map is the only thing that can answer.
			if updateNotifyEligible(cmd.Name(), false, true, cmd.HasSubCommands()) {
				t.Errorf("`nibs %s` would print an update notice into the server's output", spelling)
			}
		})
	}
}
