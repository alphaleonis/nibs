package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/mdsection"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/ui"
	"github.com/spf13/cobra"
)

var (
	closeSummary string
	closeForce   bool
	closeIfMatch string
	closeJSON    bool
)

var closeCmd = &cobra.Command{
	Use:   "close <id>",
	Short: "Close a nib by marking it completed with a summary",
	Long:  `Closes a nib by marking it completed with a summary. If the nib has a parent, updates the parent's Current Focus and merges Key Decisions. The --if-match flag protects the target nib only; the parent update uses its own etag internally.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		app := getApp(cmd)
		ctx := context.Background()
		resolver := app.newResolver()

		// Find the nib
		b, err := resolver.Query().Nib(ctx, args[0])
		if err != nil || b == nil {
			return cmdError(closeJSON, output.ErrNotFound, "nib not found: %s", args[0])
		}

		// Reject already-resolved nibs
		if nib.IsResolvedStatus(b.Status) {
			return cmdError(closeJSON, output.ErrValidation, "nib %s is already %s", b.ID, b.Status)
		}

		// Require --summary
		if closeSummary == "" {
			return cmdError(closeJSON, output.ErrValidation, "--summary is required")
		}

		// Check children status
		children := resolver.Orderer.GetSortedSiblings(b.ID)
		if len(children) > 0 && !closeForce {
			var incomplete []string
			for _, child := range children {
				if !nib.IsResolvedStatus(child.Status) {
					incomplete = append(incomplete, child.ID)
				}
			}
			if len(incomplete) > 0 {
				return cmdError(closeJSON, output.ErrValidation,
					"incomplete children: %s (use --force to close anyway)", strings.Join(incomplete, ", "))
			}
		}

		// Save original body before mutation (needed for Key Decisions extraction)
		originalBody := b.Body

		// Append summary section to body
		newBody := mdsection.Set(b.Body, 2, "Summary", "\n"+closeSummary+"\n")

		// Build update input
		completed := "completed"
		input := model.UpdateNibInput{
			Status: &completed,
			Body:   &newBody,
		}
		if closeIfMatch != "" {
			input.IfMatch = &closeIfMatch
		}

		b, err = resolver.Mutation().UpdateNib(ctx, b.ID, input)
		if err != nil {
			return mutationError(closeJSON, err)
		}

		// Update parent milestone if present
		if b.Parent != "" {
			parent, parentErr := resolver.Query().Nib(ctx, b.Parent)
			if parentErr == nil && parent != nil {
				parentBody := parent.Body

				// Replace (not append) Current Focus: after closing a child, the milestone's
				// focus should reflect the latest completed work, not accumulate history.
				focusContent := fmt.Sprintf("\nCompleted %s: %s\n", b.ID, closeSummary)
				parentBody = mdsection.Set(parentBody, 2, "Current Focus", focusContent)

				// Merge Key Decisions from closed nib into parent
				if childDecisions, found := mdsection.Find(originalBody, "Key Decisions"); found {
					existingDecisions, hasExisting := mdsection.Find(parentBody, "Key Decisions")
					if hasExisting {
						merged := strings.TrimRight(existingDecisions, "\n") + "\n" + childDecisions
						parentBody = mdsection.Replace(parentBody, "Key Decisions", merged)
					} else {
						parentBody = mdsection.Set(parentBody, 2, "Key Decisions", childDecisions)
					}
				}

				if parentBody != parent.Body {
					parentETag := parent.ETag()
					parentInput := model.UpdateNibInput{Body: &parentBody, IfMatch: &parentETag}
					if _, updateErr := resolver.Mutation().UpdateNib(ctx, parent.ID, parentInput); updateErr != nil {
						fmt.Fprintf(os.Stderr, "warning: closed %s but failed to update parent %s: %v\n", b.ID, parent.ID, updateErr)
					}
				}
			}
		}

		if closeJSON {
			return output.Success(filterResolvedBlockersOne(b, app.Core), "Nib closed")
		}
		fmt.Println(ui.Success.Render("Closed ") + ui.ID.Render(b.ID) + " " + ui.Muted.Render(b.Path))
		return nil
	},
}

func init() {
	closeCmd.Flags().StringVar(&closeSummary, "summary", "", "Summary of what was accomplished")
	closeCmd.Flags().BoolVar(&closeForce, "force", false, "Close even if children are incomplete")
	closeCmd.Flags().StringVar(&closeIfMatch, "if-match", "", "Only close if etag matches (optimistic locking)")
	closeCmd.Flags().BoolVar(&closeJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(closeCmd)
}
