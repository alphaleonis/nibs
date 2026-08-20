package cmd

import (
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/updatecheck"
	"github.com/spf13/cobra"
	"slices"
)

func TestUpdateNotifyEligible(t *testing.T) {
	cases := []struct {
		name    string
		cmdName string
		jsonSet bool
		isTTY   bool
		want    bool
	}{
		{"interactive list on a tty", "list", false, true, true},
		// Real command names throughout: maybeNotifyUpdate passes cmd.Name(),
		// which never returns an alias, so a row naming one would describe an
		// input this function cannot receive. `get` is the command `nibs show`
		// reaches.
		{"get on a tty", "get", false, true, true},
		{"not a tty (piped)", "list", false, false, false},
		{"json output suppresses", "list", true, true, false},
		{"json and non-tty", "list", true, false, false},
		{"the web server is skipped", "web", false, true, false},
		{"the query command is skipped", "query", false, true, false},
		{"tui is skipped (in-app indicator)", "tui", false, true, false},
		{"completion machinery is skipped", "__complete", false, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := updateNotifyEligible(tc.cmdName, tc.jsonSet, tc.isTTY); got != tc.want {
				t.Errorf("updateNotifyEligible(%q, json=%v, tty=%v)=%v, want %v",
					tc.cmdName, tc.jsonSet, tc.isTTY, got, tc.want)
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
	// Cobra creates its completion tree during Execute, not at init.
	rootCmd.InitDefaultCompletionCmd()

	real := map[string]bool{
		// Cobra owns these two and builds them inside ExecuteC through an
		// unexported initializer, so they are never in rootCmd.Commands() here.
		cobra.ShellCompRequestCmd:       true,
		cobra.ShellCompNoDescRequestCmd: true,
	}
	var aliases []string
	for _, c := range rootCmd.Commands() {
		real[c.Name()] = true
		aliases = append(aliases, c.Aliases...)
	}
	isAlias := func(key string) bool { return slices.Contains(aliases, key) }

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
			if updateNotifyEligible(cmd.Name(), false, true) {
				t.Errorf("`nibs %s` would print an update notice into the server's output", spelling)
			}
		})
	}
}
