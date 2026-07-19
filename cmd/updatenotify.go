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
// "update available" line: long-running servers and machine/JSON-oriented
// output (serve, graphql, query), shell-completion machinery, and tui — which
// surfaces update availability inside the UI itself rather than on stderr.
var updateNotifySkip = map[string]bool{
	"serve":            true,
	"graphql":          true,
	"query":            true,
	"tui":              true,
	"completion":       true,
	"__complete":       true,
	"__completeNoDesc": true,
}

// updateNotifyEligible reports whether the notifier may print for a command.
// It stays quiet unless stdout is an interactive terminal, the command is not
// emitting JSON, and the command is not on the skip list — so scripts, pipes,
// and JSON consumers never see the line.
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
	eligible := updateNotifyEligible(cmd.Name(), jsonFlagSet(cmd), term.IsTerminal(int(os.Stdout.Fd())))
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
