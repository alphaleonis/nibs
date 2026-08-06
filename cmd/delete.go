package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/spf13/cobra"
)

var (
	forceDelete bool
	deleteJSON  bool
)

// nibWithLinks holds a nib and its incoming links for batch processing
type nibWithLinks struct {
	nib   *nib.Nib
	links []nib.IncomingLink
}

var deleteCmd = &cobra.Command{
	Use:   "delete <id> [id...]",
	Short: "Delete one or more nibs (alias of `rm --delete`)",
	Long: `Deletes one or more nibs after confirmation (use -f to skip confirmation).

If other nibs reference the target nib(s) (as parent or via blocking), you will be
warned and those references will be removed after confirmation. Use -f to skip all warnings.`,
	Args: codedMinimumNArgs(&deleteJSON, 1),
	RunE: func(cmd *cobra.Command, args []string) error {
		app := getApp(cmd)
		ctx := context.Background()
		resolver := app.newResolver()

		// Collect all nibs and their incoming links upfront (validate before deleting)
		var targets []nibWithLinks
		for _, id := range args {
			b, err := resolver.Query().Nib(ctx, id)
			if err != nil {
				return cmdError(deleteJSON, output.ErrNotFound, "failed to find nib: %v", err)
			}
			if b == nil {
				return cmdError(deleteJSON, output.ErrNotFound, "nib not found: %s", id)
			}
			targets = append(targets, nibWithLinks{
				nib:   b,
				links: app.Core.FindIncomingLinks(b.ID),
			})
		}

		// Prompt for confirmation (JSON implies force)
		if !forceDelete && !deleteJSON {
			if !confirmDeleteMultiple(targets) {
				fmt.Println("Cancelled")
				return nil
			}
		}

		// Delete all nibs via GraphQL mutation
		var deleted []*nib.Nib
		var totalLinksRemoved int
		for _, target := range targets {
			_, err := resolver.Mutation().DeleteNib(ctx, target.nib.ID)
			if err != nil {
				return cmdError(deleteJSON, output.ErrFileError, "failed to delete nib %s: %v", target.nib.ID, err)
			}
			deleted = append(deleted, target.nib)
			totalLinksRemoved += len(target.links)
		}

		// Output results
		if deleteJSON {
			filtered := filterReleasedBlockers(deleted, app.Core)
			if len(filtered) == 1 {
				return output.Success(filtered[0], "Nib deleted")
			}
			return output.JSON(output.Response{
				Success: true,
				Nibs:    filtered,
				Count:   len(deleted),
				Message: fmt.Sprintf("%d nibs deleted", len(deleted)),
			})
		}

		if totalLinksRemoved > 0 {
			fmt.Printf("Removed %d reference(s)\n", totalLinksRemoved)
		}
		for _, b := range deleted {
			fmt.Printf("Deleted %s\n", b.Path)
		}
		return nil
	},
}

// confirmDeleteMultiple prompts the user to confirm deletion of one or more nibs.
func confirmDeleteMultiple(targets []nibWithLinks) bool {
	nibsWithLinks := 0
	totalLinks := 0
	for _, t := range targets {
		if len(t.links) > 0 {
			nibsWithLinks++
			totalLinks += len(t.links)
		}
	}

	// Single nib: use simpler format
	if len(targets) == 1 {
		t := targets[0]
		if len(t.links) > 0 {
			fmt.Printf("Warning: %d nib(s) link to '%s':\n", len(t.links), t.nib.Title)
			for _, link := range t.links {
				fmt.Printf("  - %s (%s) via %s\n", link.FromNib.ID, link.FromNib.Title, link.LinkType)
			}
			fmt.Print("Delete anyway and remove references? [y/N] ")
		} else {
			fmt.Printf("Delete '%s' (%s)? [y/N] ", t.nib.Title, t.nib.Path)
		}
	} else {
		// Multiple nibs: show batch summary
		fmt.Printf("About to delete %d nib(s):\n", len(targets))
		for _, t := range targets {
			if len(t.links) > 0 {
				fmt.Printf("  - %s (%s) ← %d incoming link(s)\n", t.nib.ID, t.nib.Title, len(t.links))
			} else {
				fmt.Printf("  - %s (%s)\n", t.nib.ID, t.nib.Title)
			}
		}
		if nibsWithLinks > 0 {
			fmt.Printf("\nWarning: %d nib(s) have incoming references (%d total) that will be removed.\n", nibsWithLinks, totalLinks)
		}
		fmt.Print("\nProceed with deletion? [y/N] ")
	}

	reader := bufio.NewReader(os.Stdin)
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
}

func init() {
	deleteCmd.Flags().BoolVarP(&forceDelete, "force", "f", false, "Skip confirmation and warnings")
	deleteCmd.Flags().BoolVar(&deleteJSON, "json", false, "Output as JSON (implies --force)")
	rootCmd.AddCommand(deleteCmd)
}
