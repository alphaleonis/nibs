package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/alphaleonis/nibs/internal/updatecheck"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// updateNotifySkip lists commands that must never emit the trailing
// "update available" line: the long-running web server, machine-oriented output
// (the GraphQL query command), the shell-completion machinery, help, upgrade
// (which reports availability itself), and tui — which surfaces update
// availability inside the UI rather than on stderr.
//
// KEYED ON cmd.Name(), WHICH IS NEVER AN ALIAS — see updateNotifyKey, which is
// what makes an entry cover a whole subtree. Two of these commands are usually
// typed under a different name (`nibs serve` for `web`, `nibs graphql` for
// `query`), and keying the alias silently disables the entry, which is how the
// server spent its life outside this list. TestUpdateNotifySkipKeysRealCommandNames
// fails on a key that names no command, or one that names an alias.
var updateNotifySkip = map[string]bool{
	"web":        true, // also reached as `nibs serve`
	"query":      true, // also reached as `nibs graphql`
	"tui":        true,
	"upgrade":    true, // reports availability itself
	"help":       true,
	"completion": true, // covers `completion <shell>` through updateNotifyKey
	"__complete": true, // also reached as `__completeNoDesc`, its alias
}

// updateNotifyEligible reports whether the notifier may print for a command.
// It stays quiet unless stdout is an interactive terminal, the command is not
// emitting JSON, and the command is not on the skip list — so scripts, pipes,
// and JSON consumers never see the line.
// updateNotifyKey is the skip-map key for cmd: its own name, or the nearest
// ancestor's when that ancestor is on the list.
//
// An entry names a SUBTREE, not one command. `nibs completion bash` executes the
// "bash" subcommand, so a lookup on the executed command's own name missed every
// shell and the "completion" entry covered only the bare `nibs completion`. The
// same hazard commandNeedsNoStore handles the same way, one file over.
func updateNotifyKey(cmd *cobra.Command) string {
	for c := cmd; c != nil; c = c.Parent() {
		if updateNotifySkip[c.Name()] {
			return c.Name()
		}
	}
	return cmd.Name()
}

func updateNotifyEligible(cmdName string, jsonSet, isTTY bool) bool {
	if !isTTY || jsonSet {
		return false
	}
	return !updateNotifySkip[cmdName]
}

// jsonFlagSet reports whether the command was invoked with --json=true.
func jsonFlagSet(cmd *cobra.Command) bool {
	f := cmd.Flags().Lookup("json")
	return f != nil && f.Value.String() == "true"
}

// maybeNotifyUpdate prints a single trailing stderr line when a newer nibs
// release is available. It is wired into the root PersistentPostRun so it only
// runs after a command has succeeded, and it is entirely best-effort: any
// gating, cache miss, or network failure results in silence, never an error or
// a delay beyond the check's own short timeout.
func maybeNotifyUpdate(cmd *cobra.Command) {
	eligible := updateNotifyEligible(updateNotifyKey(cmd), jsonFlagSet(cmd), term.IsTerminal(int(os.Stdout.Fd())))
	if !eligible {
		return
	}
	res, ok := updatecheck.NewChecker(version).Check(cmd.Context())
	writeUpdateNotice(os.Stderr, eligible, res, ok)
}

// writeUpdateNotice writes the single trailing update line to w, but only when
// the command was eligible, the check had an opinion, and an update is
// actually available. Isolated from I/O and network so the exact wording and
// the final gate are unit-testable.
func writeUpdateNotice(w io.Writer, eligible bool, res updatecheck.Result, ok bool) {
	if !eligible || !ok || !res.UpdateAvailable {
		return
	}
	_, _ = fmt.Fprintf(w, "\n(nibs %s is available — run `nibs upgrade`)\n", res.Latest)
}
