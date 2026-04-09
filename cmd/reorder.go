package cmd

import (
	"context"
	"fmt"

	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/ui"
	"github.com/spf13/cobra"
)

var (
	reorderAfter   string
	reorderBefore  string
	reorderFirst   bool
	reorderIfMatch string
	reorderJSON    bool
)

var reorderCmd = &cobra.Command{
	Use:   "reorder <id>",
	Short: "Reorder a nib among its siblings",
	Long:  `Reorder a nib by specifying its new position relative to its siblings.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		app := getApp(cmd)
		// Validate at least one positioning flag
		hasPosition := reorderAfter != "" || reorderBefore != "" || reorderFirst
		if !hasPosition {
			return cmdError(reorderJSON, output.ErrValidation, "at least one positioning flag (--after, --before, --first, --last) is required")
		}

		resolver := app.newResolver()
		ctx := context.Background()

		var afterID, beforeID *string
		var first *bool
		var ifMatch *string

		if reorderAfter != "" {
			afterID = &reorderAfter
		}
		if reorderBefore != "" {
			beforeID = &reorderBefore
		}
		if reorderFirst {
			f := true
			first = &f
		}
		if reorderIfMatch != "" {
			ifMatch = &reorderIfMatch
		}
		// --last is default behavior (no positioning flag = last), handled by resolver

		b, err := resolver.Mutation().ReorderNib(ctx, args[0], afterID, beforeID, first, nil, ifMatch)
		if err != nil {
			return cmdError(reorderJSON, output.ErrFileError, "failed to reorder nib: %v", err)
		}

		if reorderJSON {
			return output.Success(filterResolvedBlockersOne(b, app.Core), "Nib reordered")
		}

		fmt.Println(ui.Success.Render("Reordered ") + ui.ID.Render(b.ID))
		return nil
	},
}

func init() {
	reorderCmd.Flags().StringVar(&reorderAfter, "after", "", "Move after this sibling nib ID")
	reorderCmd.Flags().StringVar(&reorderBefore, "before", "", "Move before this sibling nib ID")
	reorderCmd.Flags().BoolVar(&reorderFirst, "first", false, "Move to first position")
	reorderCmd.Flags().StringVar(&reorderIfMatch, "if-match", "", "ETag for optimistic concurrency")
	reorderCmd.Flags().BoolVar(&reorderJSON, "json", false, "Output as JSON")
	reorderCmd.MarkFlagsMutuallyExclusive("after", "before", "first")
	rootCmd.AddCommand(reorderCmd)
}
