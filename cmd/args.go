package cmd

import (
	"fmt"
	"strings"

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

// codedUnknownSubcommand rejects any positional argument as an unrecognized
// verb, while accepting none at all. Cobra's own "unknown command" error comes
// from legacyArgs, which raises it only for the ROOT command (`!cmd.HasParent()`),
// so a first argument matching no child of a GROUP resolves the group itself and
// its help prints at exit 0 — a retired or misspelled verb reads as success to
// anything checking the exit status.
//
// codedNoArgs is deliberately NOT the helper for that shape: it rejects the
// bare invocation too, and `nibs area` with no verb printing its help and
// exiting 0 is correct.
//
// The group must be RUNNABLE for this to fire at all. Command.execute returns
// flag.ErrHelp on `!c.Runnable()` before it reaches ValidateArgs, so attaching
// this to a group with neither Run nor RunE is a silent no-op — measured on
// areaCmd, which still exited 0 with the validator wired. Both groups therefore
// carry a RunE that prints their help.
func codedUnknownSubcommand(jsonMode *bool) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return nil
		}
		// Cobra builds its `help` command once, on the ROOT — ExecuteC returns
		// early for any command with a parent, so InitDefaultHelpCmd never runs
		// for a group and `nibs area help` arrives here as an unrecognized verb
		// rather than resolving. It is a help request, and this group's RunE
		// answers exactly that, so it is accepted: dead-ending the commonest way
		// to ask a group for its usage is not what this refusal is for.
		if args[0] == "help" {
			return nil
		}
		msg := fmt.Sprintf("unknown command %q for %q", args[0], invokedName(cmd))
		// Only when cobra has something to offer: a wholly unrelated word
		// matches neither by Levenshtein nor by prefix, and the message has to
		// read correctly with no suggestion at all.
		if suggestions := suggestionsFor(cmd, args[0]); len(suggestions) > 0 {
			quoted := make([]string, len(suggestions))
			for i, s := range suggestions {
				quoted[i] = fmt.Sprintf("%q", s)
			}
			msg += "; did you mean " + strings.Join(quoted, " or ") + "?"
		}
		return cmdError(jsonModeOf(jsonMode), output.ErrValidation, "%s", msg)
	}
}

// suggestionsFor names the subcommands close enough to typedName to offer, by
// the same measure cobra's own unknown-command message uses.
//
// The minimum distance has to be filled in here: cobra stores it on the command
// and defaults it lazily, inside the findSuggestions path this validator stands
// in for, so it is still 0 when SuggestionsFor reads it — which admits only an
// exact Levenshtein match and leaves prefix matching to carry every suggestion
// on its own. Assigning it mirrors what findSuggestions does, and is idempotent.
func suggestionsFor(cmd *cobra.Command, typedName string) []string {
	if cmd.DisableSuggestions {
		return nil
	}
	if cmd.SuggestionsMinimumDistance <= 0 {
		cmd.SuggestionsMinimumDistance = 2
	}
	return cmd.SuggestionsFor(typedName)
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
