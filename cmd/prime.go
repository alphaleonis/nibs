package cmd

import (
	_ "embed"
	"io"
	"os"
	"strings"
	"text/template"

	"github.com/spf13/cobra"
	"github.com/alphaleonis/nibs/internal/config"
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
	// full guide states all four sets, so computing them once avoids
	// duplicating the same filter-and-join across two template files, and keeps
	// {{if .HoldingStatuses}} a plain emptiness test rather than a
	// re-derivation. Holding is the closed statuses that do NOT release their
	// dependents, which lets the guides state the "closed but still blocks"
	// rule without naming a status.
	OpenStatuses      []string
	ClosedStatuses    []string
	ReleasingStatuses []string
	HoldingStatuses   []string
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
		// If no explicit path given, check if a nibs project exists by searching
		// upward for a .nibs.yml config file
		if nibsPath == "" && configPath == "" {
			cwd, err := os.Getwd()
			if err != nil {
				return nil // Silently exit on error
			}
			configFile, err := config.FindConfig(cwd)
			if err != nil || configFile == "" {
				// No config file found - silently exit
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
		GraphQLSchema:     GetGraphQLSchema(),
		Types:             config.DefaultTypes,
		Statuses:          config.DefaultStatuses,
		Priorities:        config.DefaultPriorities,
		OpenStatuses:      cfg.OpenStatusNames(),
		ClosedStatuses:    cfg.ClosedStatusNames(),
		ReleasingStatuses: cfg.ReleasingStatusNames(),
		HoldingStatuses:   cfg.HoldingStatusNames(),
	}

	return tmpl.Execute(w, data)
}

func init() {
	primeCmd.Flags().BoolVar(&primeFullFlag, "full", false, "Emit the full CLI reference instead of the slim default")
	rootCmd.AddCommand(primeCmd)
}
