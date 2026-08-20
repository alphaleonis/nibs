package cmd

import (
	_ "embed"
	"io"
	"os"
	"strings"
	"text/template"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/store"
	"github.com/spf13/cobra"
)

//go:embed prompt.tmpl
var agentPromptSlim string

//go:embed prompt-full.tmpl
var agentPromptFull string

// promptData holds all data needed to render the prompt templates.
type promptData struct {
	GraphQLSchema string
	Types         []config.TypeConfig
	Statuses      []config.StatusConfig
	Priorities    []config.PriorityConfig

	// The status groups the guides teach, precomputed here rather than derived
	// in the markup: both templates state the holding/releasing split and the
	// --ready rule, and the full guide states all the sets, so computing them
	// once avoids duplicating the same filter-and-join across two template
	// files, and keeps {{if .HoldingStatuses}} a plain emptiness test rather
	// than a re-derivation. Holding is the closed statuses that do NOT release
	// their dependents, which lets the guides state the "closed but still
	// blocks" rule without naming a status. Startable is the status half of
	// `nibs list --ready`, so both guides describe that flag from the same set
	// the flag filters by.
	OpenStatuses      []string
	ClosedStatuses    []string
	ReleasingStatuses []string
	HoldingStatuses   []string
	StartableStatuses []string

	// DefaultCloseStatus is what a bare `nibs close` produces — read from the
	// --as flag's default rather than written out in the guides, so the two
	// cannot disagree about which reason omitting --as records.
	DefaultCloseStatus string

	// CompletionCloseStatus is the close reason that rewrites the parent's
	// `## Current Focus`; the others merge Key Decisions upward and leave the
	// focus alone. Threaded from the same const the propagation branches on, so
	// the guide cannot name a different reason than the one that gets the
	// behavior. It is a separate field from DefaultCloseStatus even though both
	// hold `completed` today: "the reason a bare close records" and "the reason
	// that counts as progress on the parent" are different questions.
	CompletionCloseStatus string
}

// promptFuncs are the template helpers the guides need to state a derived set:
// text/template has no join, and statuses named in prose are rendered as
// Markdown code spans.
var promptFuncs = template.FuncMap{
	"join":  strings.Join,
	"codes": codeList,
}

// codeList joins names as Markdown code spans. Separators are chosen per call
// site so a set of one or more members stays grammatical: " or " for a set the
// sentence treats as alternatives, "/" where it names one thing. The empty set
// renders as the empty string, which leaves the surrounding sentence without a
// subject — every call site must sit inside a guard on the set it names.
func codeList(names []string, sep string) string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = "`" + name + "`"
	}
	return strings.Join(quoted, sep)
}

var primeFullFlag bool

var primeCmd = &cobra.Command{
	Use:   "prime",
	Short: "Output instructions for AI coding agents",
	Long: `Outputs a prompt that primes AI coding agents on how to use the nibs CLI to manage project issues.

By default, emits a slim prompt with the mandatory workflow rules and a directive to load
the full reference on demand. Pass --full to emit the complete CLI guide (commands, flags,
body section conventions, GraphQL examples).`,
	Args: codedNoArgs(nil),
	RunE: func(cmd *cobra.Command, args []string) error {
		// An explicitly named store answers "is there a project here" on its
		// own; only the cwd-driven case has to walk for one.
		//
		// BOUND, stated because the branch below reads like a general guard and
		// is not one: this covers the DISCOVERY route only. `nibs prime
		// --nibs-path <dir>` still emits the prompt without asking whether that
		// directory is a store, and NIBS_PATH is not consulted here at all. Both
		// predate the store-evidence rule and neither is a mutation — prime
		// writes nothing — so widening them is a change to prime's contract
		// ("safe to run unconditionally from an agent's startup") rather than a
		// fix to this one.
		if nibsPath == "" && configPath == "" {
			cwd, err := os.Getwd()
			if err != nil {
				return nil // Silently exit on error
			}
			// The nearest marker of EITHER kind, for the same reason store
			// resolution binds to it: a `.nibs`-only walk runs past a nearer
			// pre-layout project and answers from an ancestor store, so the
			// prompt was emitted for a project the reader is not standing in
			// while every command it teaches refused in that directory.
			marker, err := store.FindNearestMarker(cwd)
			if err != nil {
				return nil // Silently exit on error
			}
			switch marker.Kind {
			case store.MarkerStore:
				// A `.nibs` SYMLINK is a store only on the evidence its
				// destination carries, and prime has to agree with the commands
				// it is teaching: emitting the onboarding prompt for a store
				// every one of them refuses is the same wrong answer the
				// pre-layout branch below exists to avoid. For a real `.nibs`
				// this costs nothing — bindNamedStore answers on the name.
				if _, err := bindNamedStore(marker.Path); err != nil {
					return err
				}
			case store.MarkerLegacyProject:
				// A pre-layout project IS a nibs project, so silence is the
				// wrong answer: this command's consumer is an agent, and
				// emitting nothing reads as "this project does not use nibs".
				// It gets the refusal every other command gives, which names
				// the migration that makes the project usable.
				return preLayoutProjectError(cwd, marker.Path)
			case store.MarkerNone:
				// No nibs project at all — say nothing, so `nibs prime` stays
				// safe to run unconditionally from an agent's startup in a
				// repository that does not use nibs.
				return nil
			}
		}

		if !primeFullFlag {
			return renderSlimPrompt(os.Stdout)
		}

		return renderFullPrompt(os.Stdout)
	},
}

// renderFullPrompt executes the full-guide template with the live config enums
// (types/statuses/priorities) so the rendered guide can never drift from the
// values the CLI accepts. Extracted from the command's RunE so tests can render
// it without going through cwd-based config discovery.
func renderFullPrompt(w io.Writer) error {
	return renderPrompt(w, "prompt-full", agentPromptFull)
}

// renderSlimPrompt executes the slim prompt. It is a template for the same
// reason the full guide is: the workflow rules it states name the status groups,
// and those must come from config rather than from prose.
func renderSlimPrompt(w io.Writer) error {
	return renderPrompt(w, "prompt", agentPromptSlim)
}

// renderPrompt executes one prompt template against the live config vocabulary.
func renderPrompt(w io.Writer, name, text string) error {
	tmpl, err := template.New(name).Funcs(promptFuncs).Parse(text)
	if err != nil {
		return err
	}

	cfg := config.Default()
	data := promptData{
		GraphQLSchema:         GetGraphQLSchema(),
		Types:                 config.DefaultTypes,
		Statuses:              config.DefaultStatuses,
		Priorities:            config.DefaultPriorities,
		OpenStatuses:          cfg.OpenStatusNames(),
		ClosedStatuses:        cfg.ClosedStatusNames(),
		ReleasingStatuses:     cfg.ReleasingStatusNames(),
		HoldingStatuses:       cfg.HoldingStatusNames(),
		StartableStatuses:     cfg.StartableStatusNames(),
		DefaultCloseStatus:    closeDefaultStatus(),
		CompletionCloseStatus: closeCompletionStatus(),
	}

	return tmpl.Execute(w, data)
}

func init() {
	primeCmd.Flags().BoolVar(&primeFullFlag, "full", false, "Emit the full CLI reference instead of the slim default")
	rootCmd.AddCommand(primeCmd)
}
