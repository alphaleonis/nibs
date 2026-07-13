package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/99designs/gqlgen/graphql"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nibtypes"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/projection"
	"github.com/alphaleonis/nibs/internal/ui"
	"github.com/spf13/cobra"
)

var (
	mvAfter        string
	mvBefore       string
	mvFirst        bool
	mvParent       string
	mvIfMatch      string
	mvJSON         bool
	mvChildrenOf   string
	mvChildIfMatch []string
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

var mvCmd = &cobra.Command{
	Use:     "mv <id> [<id>...]",
	Aliases: []string{"reorder"},
	Short:   "Move a nib: reposition among siblings or reparent",
	Long: `Move one nib (reposition or reparent) or reorder a block of siblings.

Single nib:   nibs mv <id> --after|--before|--first <anchor>   # reposition
              nibs mv <id> --parent <new-parent>                # reparent (append to end)
              nibs mv <id> --parent <new-parent> --first        # reparent to first
Block move:   nibs mv <id1> <id2> ... --after|--before <anchor>
Block move:   nibs mv <id1> <id2> ... --first
Children-of:  nibs mv --children-of <parent-id> <id1> <id2> ...

Use --children-of "" to reorder root-level (no-parent) siblings.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Dispatch by (args count, flag shape) into one of three regimes:
		//   - Mode A — --children-of <parent> set (incl. ""): args are full
		//     ordered child list; --after/--before/--first must NOT be set
		//     (Cobra-level mutex enforces this).
		//   - Mode B — len(args) >= 2 with --after/--before/--first: args
		//     are the contiguous sibling block to move.
		//   - Single — len(args) == 1: reposition (--after/--before/--first)
		//     and/or reparent (--parent), with a lean card echo.
		// Order matters: Mode A is checked first via Changed("children-of").
		// Do not reorder these branches without re-verifying the boundary
		// behavior at len(args) == 1 vs >= 2.
		app := getApp(cmd)
		resolver := app.newResolver()
		ctx := context.Background()

		childrenOfSet := cmd.Flags().Changed("children-of")
		hasAfter := mvAfter != ""
		hasBefore := mvBefore != ""
		hasFirst := mvFirst
		hasPosition := hasAfter || hasBefore || hasFirst
		hasParent := cmd.Flags().Changed("parent")

		// Reparent is a single-nib operation only: it has no meaning for a bulk
		// block move or a full children-of reordering (which id would adopt the
		// new parent?). Reject the combination structurally.
		if hasParent && (childrenOfSet || len(args) >= 2) {
			return cmdError(mvJSON, output.ErrValidation,
				"--parent moves a single nib; it cannot be combined with --children-of or multiple ids")
		}

		childIfMatch, err := parseChildIfMatch(mvChildIfMatch)
		if err != nil {
			return cmdError(mvJSON, output.ErrValidation, "%v", err)
		}

		// Mode A: --children-of <parent> <id1> <id2> ...
		if childrenOfSet {
			// --if-match in Mode A is caught by the Cobra mutex below; this
			// path is unreachable when --if-match is set. --child-if-match
			// is the right flag for Mode A: it carries per-id etags.
			results, err := resolver.Mutation().ReorderChildren(ctx, mvChildrenOf, args, childIfMatch)
			if err != nil {
				return cmdError(mvJSON, output.ErrFileError, "failed to reorder children: %v", err)
			}
			if mvJSON {
				return output.SuccessMultiple(filterResolvedBlockers(results, app.Core))
			}
			fmt.Println(ui.Success.Render(fmt.Sprintf("Reordered %d children", len(results))))
			return nil
		}

		// Mode B: <id1> <id2> ... --after|--before|--first
		if len(args) >= 2 {
			if !hasPosition {
				return cmdError(mvJSON, output.ErrValidation,
					"multiple ids given without --children-of, --after, --before, or --first")
			}
			if mvIfMatch != "" {
				// Bulk modes use --child-if-match (per-id etags), not --if-match.
				// --if-match has no canonical owner in a multi-nib reorder.
				return cmdError(mvJSON, output.ErrValidation,
					"--if-match is only supported in single-nib mode; use --child-if-match <id>=<etag> for bulk modes")
			}
			var afterID, beforeID *string
			var first *bool
			if hasAfter {
				afterID = &mvAfter
			}
			if hasBefore {
				beforeID = &mvBefore
			}
			if hasFirst {
				f := true
				first = &f
			}
			results, err := resolver.Mutation().ReorderSiblings(ctx, args, afterID, beforeID, first, childIfMatch)
			if err != nil {
				return cmdError(mvJSON, output.ErrFileError, "failed to reorder siblings: %v", err)
			}
			if mvJSON {
				return output.SuccessMultiple(filterResolvedBlockers(results, app.Core))
			}
			fmt.Println(ui.Success.Render(fmt.Sprintf("Reordered %d siblings", len(results))))
			return nil
		}

		// Single-nib — exactly 1 positional arg.
		if len(mvChildIfMatch) > 0 {
			return cmdError(mvJSON, output.ErrValidation,
				"--child-if-match is only valid in bulk modes; use --if-match for a single nib")
		}
		if !hasPosition && !hasParent {
			return cmdError(mvJSON, output.ErrValidation,
				"specify a move: --after, --before, --first (reposition) or --parent (reparent)")
		}

		var ifMatch *string
		if mvIfMatch != "" {
			ifMatch = &mvIfMatch
		}

		card, _ := projection.ViewFields(string(projection.ViewCard))

		// Reparent-only: move under the new parent, appended to the end of its
		// children (updateNib's parent semantics). A position flag is not required
		// here — omitting it is the natural "move X under Y" default.
		if !hasPosition {
			input := model.UpdateNibInput{Parent: graphql.OmittableOf(&mvParent)}
			if ifMatch != nil {
				input.IfMatch = ifMatch
			}
			moved, err := resolver.Mutation().UpdateNib(ctx, args[0], input)
			if err != nil {
				return mvMutationError(mvJSON, err)
			}
			return echoCard(mvJSON, moved, resolver.ProjectionResolver(ctx), card)
		}

		// Reposition (optionally reparenting first, atomically via reorderNib).
		var afterID, beforeID *string
		var first *bool
		if hasAfter {
			afterID = &mvAfter
		}
		if hasBefore {
			beforeID = &mvBefore
		}
		if hasFirst {
			f := true
			first = &f
		}
		var parentID *string
		if hasParent {
			parentID = &mvParent
		}

		moved, err := resolver.Mutation().ReorderNib(ctx, args[0], afterID, beforeID, first, parentID, ifMatch)
		if err != nil {
			return mvMutationError(mvJSON, err)
		}
		return echoCard(mvJSON, moved, resolver.ProjectionResolver(ctx), card)
	},
}

// mvMutationError maps a move mutation error to a structured CLI error. An
// illegal reparent (e.g. a task under a milestone) surfaces as a HIERARCHY error
// carrying the allowed parent types — mirroring `nibs new`. Everything else
// (reconcilable ETag conflict → CONFLICT with the server etag, corrupt file →
// FILE_ERROR, generic validation) delegates to set's shared mapping.
func mvMutationError(jsonOutput bool, err error) error {
	var he *nibtypes.HierarchyError
	if errors.As(err, &he) {
		if jsonOutput {
			return output.ErrorHierarchy(he.Error(), he.Allowed)
		}
		return cmdError(false, output.ErrHierarchy, "%s", he.Error())
	}
	return setMutationError(jsonOutput, err)
}

func init() {
	mvCmd.Flags().StringVar(&mvAfter, "after", "", "Move after this sibling nib ID")
	mvCmd.Flags().StringVar(&mvBefore, "before", "", "Move before this sibling nib ID")
	mvCmd.Flags().BoolVar(&mvFirst, "first", false, "Move to first position")
	mvCmd.Flags().StringVar(&mvParent, "parent", "", "Reparent under this nib ID (single-nib only; use \"\" for root)")
	mvCmd.Flags().StringVar(&mvIfMatch, "if-match", "", "ETag for optimistic concurrency (single-nib mode only)")
	mvCmd.Flags().BoolVar(&mvJSON, "json", false, "Output as JSON")
	mvCmd.Flags().StringVar(&mvChildrenOf, "children-of", "", `Replace ordering of all children under this parent (bulk). Use "" for root.`)
	mvCmd.Flags().StringArrayVar(&mvChildIfMatch, "child-if-match", nil, `Per-child ETag in <id>=<etag> form (bulk modes; repeatable)`)
	mvCmd.MarkFlagsMutuallyExclusive("after", "before", "first")
	mvCmd.MarkFlagsMutuallyExclusive("children-of", "after")
	mvCmd.MarkFlagsMutuallyExclusive("children-of", "before")
	mvCmd.MarkFlagsMutuallyExclusive("children-of", "first")
	mvCmd.MarkFlagsMutuallyExclusive("children-of", "parent")
	mvCmd.MarkFlagsMutuallyExclusive("children-of", "if-match")
	mvCmd.MarkFlagsMutuallyExclusive("if-match", "child-if-match")
	rootCmd.AddCommand(mvCmd)
}
