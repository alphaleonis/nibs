package cmd

import (
	"fmt"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/spf13/cobra"
)

var archiveJSON bool

var archiveCmd = &cobra.Command{
	Use:   "archive",
	Short: "Move completed/scrapped nibs to the archive",
	Long: `Moves all nibs with status "completed" or "scrapped" to the archive directory (.nibs/archive/).
Archived nibs are preserved for project memory and remain visible in all queries.
The archive keeps the main .nibs directory tidy while preserving project history.

Relationships (parent, blocking) are preserved in archived nibs.`,
	// archive takes NO positional arguments — it archives every eligible nib.
	// A custom validator (rather than the shared codedNoArgs helper) is used so
	// the error names the id-taking alternative: an argument here was a caller
	// reaching for `nibs rm <id>` (which archives by default). Without this, an
	// id was silently discarded and every completed/scrapped nib archived. Flags
	// are parsed before Args validation, so archiveJSON is already set here and
	// the coded cmdError routes through the --json envelope (exit 2). This
	// bespoke message is the only thing that sets archive apart: the coded
	// exit-2/envelope behavior it shares with every other command (the coded*
	// validators in cmd/args.go), rather than being the exception it once was.
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return cmdError(archiveJSON, output.ErrValidation,
				"nibs archive takes no arguments (it archives all completed/scrapped nibs).\n"+
					"To archive specific nibs, use: nibs rm <id> [id...]")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		app := getApp(cmd)
		allNibs := app.Core.All()

		// Find nibs with any archive status
		var archiveNibs []*nib.Nib
		archiveSet := make(map[string]bool)
		for _, b := range allNibs {
			if app.Config().IsArchiveStatus(b.Status) {
				archiveNibs = append(archiveNibs, b)
				archiveSet[b.ID] = true
			}
		}

		if len(archiveNibs) == 0 {
			if archiveJSON {
				return output.SuccessMessage("No nibs to archive")
			}
			fmt.Println("No nibs with archive status to archive.")
			return nil
		}

		// Sort nibs for consistent display
		nib.SortByStatusPriorityAndType(archiveNibs, app.Config().StatusNames(), app.Config().TypeNames(), app.Config())

		// Archive all nibs with archive status
		var archived []string
		for _, b := range archiveNibs {
			if err := app.Core.Archive(b.ID); err != nil {
				if archiveJSON {
					return output.Error(output.ErrFileError, fmt.Sprintf("failed to archive nib %s: %s", b.ID, err.Error()))
				}
				return fmt.Errorf("failed to archive nib %s: %w", b.ID, err)
			}
			archived = append(archived, b.ID)
		}

		if archiveJSON {
			return output.SuccessMessage(fmt.Sprintf("Archived %d nib(s) to .nibs/archive/", len(archived)))
		}

		fmt.Printf("Archived %d nib(s) to .nibs/archive/\n", len(archived))
		return nil
	},
}

func init() {
	archiveCmd.Flags().BoolVar(&archiveJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(archiveCmd)
}
