package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/ui"
	"github.com/spf13/cobra"
)

var (
	reorderAfter        string
	reorderBefore       string
	reorderFirst        bool
	reorderIfMatch      string
	reorderJSON         bool
	reorderChildrenOf   string
	reorderChildIfMatch []string
)

// parseChildIfMatch turns the repeatable `--child-if-match <id>=<etag>`
// values into a []*model.ChildEtag. Empty input yields nil. Each entry
// must contain a single `=` with non-empty id and etag; malformed values
// surface a parse error pointing at the offending input.
//
// Etags are FNV-64a hex (16 chars over [0-9a-f]) and therefore cannot
// contain '='. We split on the first '=' for that reason — if a future
// etag scheme allows '=' in the etag portion this parser must be revisited.
func parseChildIfMatch(values []string) ([]*model.ChildEtag, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*model.ChildEtag, 0, len(values))
	for _, v := range values {
		idx := strings.Index(v, "=")
		if idx < 0 {
			return nil, fmt.Errorf("--child-if-match %q: expected <id>=<etag>", v)
		}
		id := strings.TrimSpace(v[:idx])
		etag := strings.TrimSpace(v[idx+1:])
		if id == "" {
			return nil, fmt.Errorf("--child-if-match %q: empty id", v)
		}
		if etag == "" {
			return nil, fmt.Errorf("--child-if-match %q: empty etag", v)
		}
		out = append(out, &model.ChildEtag{ID: id, Etag: etag})
	}
	return out, nil
}

var reorderCmd = &cobra.Command{
	Use:   "reorder <id> [<id>...]",
	Short: "Reorder one or more nibs among their siblings",
	Long: `Reorder a single nib, a contiguous block of siblings, or the full set of children under a parent.

Single (existing): nibs reorder <id> --after|--before|--first <anchor>
Block move:        nibs reorder <id1> <id2> ... --after|--before <anchor>
Block move:        nibs reorder <id1> <id2> ... --first
Children-of:       nibs reorder --children-of <parent-id> <id1> <id2> ...

Use --children-of "" to reorder root-level (no-parent) siblings.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Dispatch by (args count, flag shape) into one of three regimes:
		//   - Mode A — --children-of <parent> set (incl. ""): args are full
		//     ordered child list; --after/--before/--first must NOT be set
		//     (Cobra-level mutex enforces this).
		//   - Mode B — len(args) >= 2 with --after/--before/--first: args
		//     are the contiguous sibling block to move.
		//   - Single (existing) — len(args) == 1, optional positioning flag;
		//     omitting all flags appends to end.
		// Order matters: Mode A is checked first via Changed("children-of").
		// Do not reorder these branches without re-verifying the boundary
		// behavior at len(args) == 1 vs >= 2.
		app := getApp(cmd)
		resolver := app.newResolver()
		ctx := context.Background()

		childrenOfSet := cmd.Flags().Changed("children-of")
		hasAfter := reorderAfter != ""
		hasBefore := reorderBefore != ""
		hasFirst := reorderFirst
		hasPosition := hasAfter || hasBefore || hasFirst

		childIfMatch, err := parseChildIfMatch(reorderChildIfMatch)
		if err != nil {
			return cmdError(reorderJSON, output.ErrValidation, "%v", err)
		}

		// Mode A: --children-of <parent> <id1> <id2> ...
		if childrenOfSet {
			// --if-match in Mode A is caught by the Cobra mutex below; this
			// path is unreachable when --if-match is set. --child-if-match
			// is the right flag for Mode A: it carries per-id etags.
			results, err := resolver.Mutation().ReorderChildren(ctx, reorderChildrenOf, args, childIfMatch)
			if err != nil {
				return cmdError(reorderJSON, output.ErrFileError, "failed to reorder children: %v", err)
			}
			if reorderJSON {
				return output.SuccessMultiple(filterResolvedBlockers(results, app.Core))
			}
			fmt.Println(ui.Success.Render(fmt.Sprintf("Reordered %d children", len(results))))
			return nil
		}

		// Mode B: <id1> <id2> ... --after|--before|--first
		if len(args) >= 2 {
			if !hasPosition {
				return cmdError(reorderJSON, output.ErrValidation,
					"multiple ids given without --children-of, --after, --before, or --first")
			}
			if reorderIfMatch != "" {
				// Bulk modes use --child-if-match (per-id etags), not --if-match.
				// --if-match has no canonical owner in a multi-nib reorder.
				return cmdError(reorderJSON, output.ErrValidation,
					"--if-match is only supported in single-nib reorder mode; use --child-if-match <id>=<etag> for bulk modes")
			}
			var afterID, beforeID *string
			var first *bool
			if hasAfter {
				afterID = &reorderAfter
			}
			if hasBefore {
				beforeID = &reorderBefore
			}
			if hasFirst {
				f := true
				first = &f
			}
			results, err := resolver.Mutation().ReorderSiblings(ctx, args, afterID, beforeID, first, childIfMatch)
			if err != nil {
				return cmdError(reorderJSON, output.ErrFileError, "failed to reorder siblings: %v", err)
			}
			if reorderJSON {
				return output.SuccessMultiple(filterResolvedBlockers(results, app.Core))
			}
			fmt.Println(ui.Success.Render(fmt.Sprintf("Reordered %d siblings", len(results))))
			return nil
		}

		// Single-nib (existing) — exactly 1 positional arg.
		if len(reorderChildIfMatch) > 0 {
			return cmdError(reorderJSON, output.ErrValidation,
				"--child-if-match is only valid in bulk modes; use --if-match for single-nib reorder")
		}
		if !hasPosition {
			return cmdError(reorderJSON, output.ErrValidation,
				"at least one positioning flag (--after, --before, --first) is required")
		}

		var afterID, beforeID *string
		var first *bool
		var ifMatch *string
		if hasAfter {
			afterID = &reorderAfter
		}
		if hasBefore {
			beforeID = &reorderBefore
		}
		if hasFirst {
			f := true
			first = &f
		}
		if reorderIfMatch != "" {
			ifMatch = &reorderIfMatch
		}

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
	reorderCmd.Flags().StringVar(&reorderIfMatch, "if-match", "", "ETag for optimistic concurrency (single-nib mode only)")
	reorderCmd.Flags().BoolVar(&reorderJSON, "json", false, "Output as JSON")
	reorderCmd.Flags().StringVar(&reorderChildrenOf, "children-of", "", `Replace ordering of all children under this parent (Mode A). Use "" for root.`)
	reorderCmd.Flags().StringArrayVar(&reorderChildIfMatch, "child-if-match", nil, `Per-child ETag in <id>=<etag> form (bulk modes; repeatable)`)
	reorderCmd.MarkFlagsMutuallyExclusive("after", "before", "first")
	reorderCmd.MarkFlagsMutuallyExclusive("children-of", "after")
	reorderCmd.MarkFlagsMutuallyExclusive("children-of", "before")
	reorderCmd.MarkFlagsMutuallyExclusive("children-of", "first")
	reorderCmd.MarkFlagsMutuallyExclusive("children-of", "if-match")
	reorderCmd.MarkFlagsMutuallyExclusive("if-match", "child-if-match")
	rootCmd.AddCommand(reorderCmd)
}
