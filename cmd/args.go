package cmd

import (
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/spf13/cobra"
)

// Coded positional-argument validators for Cobra commands.
//
// All of them route an arity violation through cmdError/output.ErrValidation so
// an arg-count error gets the SAME treatment as a value-validation error: exit
// status 2 (output.ExitValidation) via the reportExitError boundary and, for a
// command that carries a --json flag, the {"error":{code,message}} envelope on
// stdout. Stock cobra.NoArgs/ExactArgs/MinimumNArgs/MaximumNArgs would instead
// print a plain-text line and exit 1, bypassing the envelope and the coded exit
// status entirely.
//
// Convention: never wire a command's Args field to a stock
// cobra.NoArgs/ExactArgs/MinimumNArgs/MaximumNArgs — use these coded
// equivalents so every positional-arg error is uniform CLI-wide: exit 2 and,
// for a --json command, the {error} envelope. The three commands with bespoke
// arg logic (archiveCmd, queryCmd, bodyCmd) do NOT use the stock validators
// either — each keeps an inline validator that still routes its arity violation
// through cmdError: archiveCmd adds an id-specific hint naming `nibs rm`,
// queryCmd preserves its --schema bypass and 0-or-1-arg stdin logic, and bodyCmd
// (bodyArgs) pre-checks the --set/--append flag-swallow footgun before falling
// through to codedExactArgs.
//
// jsonMode is a nil-safe pointer to the command's --json flag var. Cobra parses
// flags before it validates args, so dereferencing it inside the returned
// validator reads the value the caller actually passed. Pass the flag var's
// address for a --json-bearing command; pass nil for a command with no --json
// flag — it still gets a coded exit-2 plain-text error, uniform with the rest.

// jsonModeOf dereferences a nil-safe --json flag pointer.
func jsonModeOf(jsonMode *bool) bool {
	return jsonMode != nil && *jsonMode
}

// invokedName returns the command path as the user actually typed it: the
// parent's canonical path plus the alias used to invoke the command
// (cmd.CalledAs), so an aliased call reports the alias rather than the canonical
// Use name — e.g. `nibs show` (alias of get) reports "nibs show", not
// "nibs get". Cobra populates CalledAs during command resolution, before the
// Args validator runs, so it is set on the real invocation path. It falls back
// to cmd.Name() when CalledAs is empty (an Args validator invoked directly, as
// in tests) and omits the parent prefix for a parentless command (guarding the
// bare-command demo cases).
func invokedName(cmd *cobra.Command) string {
	name := cmd.CalledAs()
	if name == "" {
		name = cmd.Name()
	}
	if parent := cmd.Parent(); parent != nil {
		return parent.CommandPath() + " " + name
	}
	return name
}

// codedNoArgs rejects any positional argument. Coded replacement for
// cobra.NoArgs.
func codedNoArgs(jsonMode *bool) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return cmdError(jsonModeOf(jsonMode), output.ErrValidation,
				"%s does not take positional arguments, got %d", invokedName(cmd), len(args))
		}
		return nil
	}
}

// codedExactArgs requires exactly n positional arguments. Coded replacement for
// cobra.ExactArgs.
func codedExactArgs(jsonMode *bool, n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != n {
			return cmdError(jsonModeOf(jsonMode), output.ErrValidation,
				"%s requires exactly %d argument(s), got %d", invokedName(cmd), n, len(args))
		}
		return nil
	}
}

// codedMinimumNArgs requires at least n positional arguments. Coded replacement
// for cobra.MinimumNArgs.
func codedMinimumNArgs(jsonMode *bool, n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < n {
			return cmdError(jsonModeOf(jsonMode), output.ErrValidation,
				"%s requires at least %d argument(s), got %d", invokedName(cmd), n, len(args))
		}
		return nil
	}
}

// codedMaximumNArgs accepts at most n positional arguments. Coded replacement
// for cobra.MaximumNArgs.
func codedMaximumNArgs(jsonMode *bool, n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) > n {
			return cmdError(jsonModeOf(jsonMode), output.ErrValidation,
				"%s accepts at most %d argument(s), got %d", invokedName(cmd), n, len(args))
		}
		return nil
	}
}
