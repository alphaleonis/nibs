package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	rmArchive bool
	rmDelete  bool
	rmForce   bool
	rmJSON    bool
)

var rmCmd = &cobra.Command{
	Use:   "rm <id> [id...]",
	Short: "Remove nib(s): archive (default) or hard-delete",
	Long: `Removes one or more nibs.

By default a nib is ARCHIVED — moved to .nibs/archive/, which is reversible and
keeps it visible in queries (relationships are preserved). Pass --delete to hard-
DELETE it from disk; that is irreversible and strips incoming references from
other nibs.

Confirmation: an interactive terminal prompts before removing. Agents and scripts
must pass -f/--force (or --json, which implies force). With no terminal to prompt
on and no --force, rm refuses with a validation error rather than silently doing
nothing.`,
	Args: codedMinimumNArgs(&rmJSON, 1),
	RunE: runRm,
}

func runRm(cmd *cobra.Command, args []string) error {
	app := getApp(cmd)
	ctx := context.Background()
	resolver := app.newResolver()

	// Collect all targets and their incoming links upfront (validate before
	// removing anything).
	var targets []nibWithLinks
	for _, id := range args {
		b, err := resolver.Query().Nib(ctx, id)
		if err != nil {
			return cmdError(rmJSON, output.ErrNotFound, "failed to find nib: %v", err)
		}
		if b == nil {
			return cmdError(rmJSON, output.ErrNotFound, "nib not found: %s", id)
		}
		targets = append(targets, nibWithLinks{
			nib:   b,
			links: app.Core.FindIncomingLinks(b.ID),
		})
	}

	// Confirmation contract. --json implies force (machine callers). Otherwise an
	// interactive terminal prompts; a NON-interactive caller must pass -f, else we
	// refuse loudly (VALIDATION) rather than silently canceling with a success
	// exit — a success exit on a no-op would leave an agent believing the
	// removal happened.
	if !rmForce && !rmJSON {
		if !isInteractiveStdin() {
			return cmdError(rmJSON, output.ErrValidation,
				"refusing to remove without confirmation: pass -f/--force (no terminal available to prompt)")
		}
		if !confirmRemove(targets, rmDelete) {
			fmt.Println("Cancelled")
			return nil
		}
	}

	if rmDelete {
		return runRmDelete(ctx, app, resolver, targets)
	}
	return runRmArchive(app, targets)
}

// runRmArchive archives each target (moving it to .nibs/archive/). Relationships
// are preserved, so no references are removed.
func runRmArchive(app *App, targets []nibWithLinks) error {
	var archived []*nib.Nib
	for _, t := range targets {
		if err := app.Core.Archive(t.nib.ID); err != nil {
			return cmdError(rmJSON, output.ErrFileError, "failed to archive nib %s: %v", t.nib.ID, err)
		}
		archived = append(archived, t.nib)
	}

	if rmJSON {
		filtered := filterReleasedBlockers(archived, app.Core)
		if len(filtered) == 1 {
			return output.Success(filtered[0], "Nib archived")
		}
		return output.JSON(output.Response{
			Success: true,
			Nibs:    filtered,
			Count:   len(archived),
			Message: fmt.Sprintf("%d nibs archived", len(archived)),
		})
	}

	for _, b := range archived {
		fmt.Printf("Archived %s (%s)\n", b.ID, b.Title)
	}
	return nil
}

// runRmDelete hard-deletes each target from disk, removing incoming references
// from other nibs (handled by the DeleteNib mutation).
func runRmDelete(ctx context.Context, app *App, resolver *graph.Resolver, targets []nibWithLinks) error {
	var deleted []*nib.Nib
	var totalLinksRemoved int
	for _, t := range targets {
		if _, err := resolver.Mutation().DeleteNib(ctx, t.nib.ID); err != nil {
			return cmdError(rmJSON, output.ErrFileError, "failed to delete nib %s: %v", t.nib.ID, err)
		}
		deleted = append(deleted, t.nib)
		totalLinksRemoved += len(t.links)
	}

	if rmJSON {
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
		fmt.Printf("Deleted %s (%s)\n", b.ID, b.Title)
	}
	return nil
}

// isInteractiveStdin reports whether stdin is a terminal. When it is not (a pipe,
// a redirect, an agent), rm cannot prompt for confirmation and must be given -f.
func isInteractiveStdin() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// confirmRemove prompts to confirm removal. Hard delete reuses the delete
// warning (which surfaces incoming references that will be stripped); archive
// preserves relationships, so it prompts with a lighter message.
func confirmRemove(targets []nibWithLinks, hardDelete bool) bool {
	if hardDelete {
		return confirmDeleteMultiple(targets)
	}

	if len(targets) == 1 {
		fmt.Printf("Archive '%s' (%s)? [y/N] ", targets[0].nib.Title, targets[0].nib.ID)
	} else {
		fmt.Printf("About to archive %d nib(s):\n", len(targets))
		for _, t := range targets {
			fmt.Printf("  - %s (%s)\n", t.nib.ID, t.nib.Title)
		}
		fmt.Print("\nProceed with archive? [y/N] ")
	}

	reader := bufio.NewReader(os.Stdin)
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
}

func init() {
	rmCmd.Flags().BoolVar(&rmArchive, "archive", false, "Archive the nib(s) (default)")
	rmCmd.Flags().BoolVar(&rmDelete, "delete", false, "Hard-delete the nib(s) from disk (irreversible)")
	rmCmd.Flags().BoolVarP(&rmForce, "force", "f", false, "Skip confirmation and warnings")
	rmCmd.Flags().BoolVar(&rmJSON, "json", false, "Output as JSON (implies --force)")
	rmCmd.MarkFlagsMutuallyExclusive("archive", "delete")
	rootCmd.AddCommand(rmCmd)
}
