package cmd

import (
	_ "embed"
	"io"
	"os"
	"text/template"

	"github.com/spf13/cobra"
	"github.com/alphaleonis/nibs/internal/config"
)

//go:embed prompt.tmpl
var agentPromptSlim string

//go:embed prompt-full.tmpl
var agentPromptFull string

// promptData holds all data needed to render the prompt template.
type promptData struct {
	GraphQLSchema string
	Types         []config.TypeConfig
	Statuses      []config.StatusConfig
	Priorities    []config.PriorityConfig
}

var primeFullFlag bool

var primeCmd = &cobra.Command{
	Use:   "prime",
	Short: "Output instructions for AI coding agents",
	Long: `Outputs a prompt that primes AI coding agents on how to use the nibs CLI to manage project issues.

By default, emits a slim prompt with the mandatory workflow rules and a directive to load
the full reference on demand. Pass --full to emit the complete CLI guide (commands, flags,
body section conventions, GraphQL examples).`,
	Args: cobra.NoArgs,
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
			_, err := os.Stdout.WriteString(agentPromptSlim)
			return err
		}

		return renderFullPrompt(os.Stdout)
	},
}

// renderFullPrompt executes the full-guide template with the live config enums
// (types/statuses/priorities) so the rendered guide can never drift from the
// values the CLI accepts. Extracted from the command's RunE so tests can render
// it without going through cwd-based config discovery.
func renderFullPrompt(w io.Writer) error {
	tmpl, err := template.New("prompt-full").Parse(agentPromptFull)
	if err != nil {
		return err
	}

	data := promptData{
		GraphQLSchema: GetGraphQLSchema(),
		Types:         config.DefaultTypes,
		Statuses:      config.DefaultStatuses,
		Priorities:    config.DefaultPriorities,
	}

	return tmpl.Execute(w, data)
}

func init() {
	primeCmd.Flags().BoolVar(&primeFullFlag, "full", false, "Emit the full CLI reference instead of the slim default")
	rootCmd.AddCommand(primeCmd)
}
