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
	"github.com/alphaleonis/nibs/internal/projection"
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
	Args:  codedExactArgs(&closeJSON, 1),
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

		// Resolve the summary from the input channel ("-" for stdin, "@FILE" for a
		// file) — prose never rides on argv, mirroring `nibs new --body` and
		// `nibs body --set`. A trailing newline is trimmed so the appended
		// ## Summary section does not accrue a blank line.
		summary, err := resolveAppendFlag(closeSummary)
		if err != nil {
			return inputError(closeJSON, err)
		}
		if summary == "" {
			return cmdError(closeJSON, output.ErrValidation, "--summary is required (use '-' for stdin or '@FILE')")
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

		// Append summary section to body. Set is the wildcard-match variant (matches
		// a "Summary" heading at any level); appendLevel 2 creates it at "## " when absent.
		newBody, _ := mdsection.Set(b.Body, 2, "Summary", "\n"+summary+"\n")

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
			// A reconcilable ETag conflict carries the server's current etag so an
			// agent can retry with it (the "409 → retry with the server etag"
			// reconcile), mirroring `nibs set`.
			return setMutationError(closeJSON, err)
		}

		// Update parent milestone if present
		if b.Parent != "" {
			parent, parentErr := resolver.Query().Nib(ctx, b.Parent)
			if parentErr == nil && parent != nil {
				parentBody := parent.Body

				// Replace (not append) Current Focus: after closing a child, the milestone's
				// focus should reflect the latest completed work, not accumulate history.
				focusContent := fmt.Sprintf("\nCompleted %s: %s\n", b.ID, summary)
				parentBody, _ = mdsection.Set(parentBody, 2, "Current Focus", focusContent)

				// Merge Key Decisions from closed nib into parent. All matches are
				// wildcard (AnyLevel) to preserve the historic level-agnostic behavior.
				if childDecisions, found := mdsection.Find(originalBody, "Key Decisions", mdsection.AnyLevel); found {
					existingDecisions, hasExisting := mdsection.Find(parentBody, "Key Decisions", mdsection.AnyLevel)
					if hasExisting {
						merged := strings.TrimRight(existingDecisions, "\n") + "\n" + childDecisions
						parentBody = mdsection.Replace(parentBody, "Key Decisions", merged, mdsection.AnyLevel)
					} else {
						parentBody, _ = mdsection.Set(parentBody, 2, "Key Decisions", childDecisions)
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

		// Lean card echo — the same projection path `nibs get` uses (no body/etag
		// unless explicitly asked).
		card, _ := projection.ViewFields(string(projection.ViewCard))
		return echoCard(closeJSON, b, resolver.ProjectionResolver(ctx), card)
	},
}

func init() {
	closeCmd.Flags().StringVar(&closeSummary, "summary", "", "Summary input channel: '-' for stdin or '@FILE' for a file (no inline text)")
	closeCmd.Flags().BoolVar(&closeForce, "force", false, "Close even if children are incomplete")
	closeCmd.Flags().StringVar(&closeIfMatch, "if-match", "", "Only close if etag matches (optimistic locking)")
	closeCmd.Flags().BoolVar(&closeJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(closeCmd)
}
