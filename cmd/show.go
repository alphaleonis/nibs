package cmd

import (
	"context"
	"fmt"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/projection"
	"github.com/spf13/cobra"
)

var (
	getJSON   bool
	getView   string
	getFields string
)

var getCmd = &cobra.Command{
	Use:     "get <id> [id...]",
	Aliases: []string{"show"},
	Short:   "Get one or more nibs",
	Long: `Get one or more nibs, projected through the field-set engine.

With no flags, prints each nib as its on-disk document (YAML front matter +
body) — the leanest full representation, matching the writeback format.

  --view id|ref|card|full   Select a coarse field set (leanest to fullest).
  -f, --fields <spec>       Select exact fields, additive over --view. Scalars,
                            computed fields (children, progress, ready), and one
                            level of nested relation projection are supported,
                            e.g. -f "id,blocked-by(id,status)".

'body' and 'etag' are opt-in: absent from the ref/card tiers unless projected
explicitly (-f body / -f etag) or via --view full.

  --json                    Emit the single-read output contract: {"nib": {…}}
                            for one id, {"nibs": [ … ]} for several. No success
                            or data wrapper. Defaults to the 'card' field set
                            when neither --view nor -f is given.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runGet,
}

// runGet looks up every requested nib, compiles the projection selection from
// --view + -f, then renders as a document (default), projected text, or the
// {nib}/{nibs} JSON contract.
func runGet(cmd *cobra.Command, args []string) error {
	app := getApp(cmd)
	gqlResolver := app.newResolver()

	// Look up all requested nibs first — a missing id is a hard NOT_FOUND
	// before any projection work.
	nibs := make([]*nib.Nib, 0, len(args))
	for _, id := range args {
		b, err := gqlResolver.Query().Nib(context.Background(), id)
		if err != nil {
			return reportErr(getJSON, output.ErrNotFound,
				fmt.Errorf("failed to find nib: %w", err))
		}
		if b == nil {
			return reportErr(getJSON, output.ErrNotFound,
				fmt.Errorf("nib not found: %s", id))
		}
		nibs = append(nibs, b)
	}

	// Compile the selection: --view first, then -f merged additively on top. A
	// bad view/field/nesting is surfaced as a validation error naming the menu.
	sel, err := projection.Compile(getView, getFields)
	if err != nil {
		return reportErr(getJSON, output.ErrValidation, err)
	}

	resolver := gqlResolver.ProjectionResolver(context.Background())

	if getJSON {
		return renderGetJSON(nibs, sel, resolver)
	}
	return renderGetText(nibs, sel, resolver)
}

// renderGetJSON emits the single-read output contract. When neither --view nor
// -f was given the selection defaults to the 'card' field set (no body/etag).
// One id yields {"nib": {…}}; several yield {"nibs": [ … ]} — both carry the
// flat projected object(s) with no success/data wrapper.
func renderGetJSON(nibs []*nib.Nib, sel projection.Selection, r projection.Resolver) error {
	if sel.IsEmpty() {
		// Card is a compile-time-valid view, so this never errors.
		card, _ := projection.ViewFields(string(projection.ViewCard))
		sel = card
	}

	projected := make([]*projection.Projected, len(nibs))
	for i, b := range nibs {
		p, err := projection.Project(b, sel, r)
		if err != nil {
			return reportErr(true, output.ErrValidation, err)
		}
		projected[i] = p
	}

	if len(projected) == 1 {
		return output.JSONRaw(map[string]any{"nib": projected[0]})
	}
	return output.JSONRaw(map[string]any{"nibs": projected})
}

// renderGetText prints the nib document when no projection was requested, or the
// projected fields as "key: value" lines (in canonical menu order) otherwise.
// Multiple nibs are separated by a delimiter appropriate to the mode.
func renderGetText(nibs []*nib.Nib, sel projection.Selection, r projection.Resolver) error {
	if sel.IsEmpty() {
		return printNibDocuments(nibs)
	}
	for i, b := range nibs {
		if i > 0 {
			fmt.Println()
		}
		p, err := projection.Project(b, sel, r)
		if err != nil {
			return reportErr(false, output.ErrValidation, err)
		}
		printProjectedText(p)
	}
	return nil
}

// printNibDocuments renders each nib's on-disk document (front matter + body),
// byte-faithful and never truncated, separating multiple nibs with a horizontal
// rule.
func printNibDocuments(nibs []*nib.Nib) error {
	for i, b := range nibs {
		if i > 0 {
			fmt.Print("\n---\n\n")
		}
		content, err := b.Render()
		if err != nil {
			return fmt.Errorf("failed to render nib: %w", err)
		}
		fmt.Print(string(content))
	}
	return nil
}

// printProjectedText renders one projection as "key: value" lines in menu
// order. Leaf values use projection.TextValue; nested relations and computed
// structs fall back to their JSON encoding (lossless) via the same helper.
func printProjectedText(p *projection.Projected) {
	for _, f := range p.Fields() {
		fmt.Printf("%s: %s\n", f.Key, projection.TextValue(f.Value))
	}
}

func init() {
	getCmd.Flags().BoolVar(&getJSON, "json", false,
		"Emit the single-read {nib}/{nibs} JSON contract")
	getCmd.Flags().StringVar(&getView, "view", "",
		"View tier: id, ref, card, or full")
	getCmd.Flags().StringVarP(&getFields, "fields", "f", "",
		"Field selection (additive over --view), e.g. \"status,priority\" or \"id,blocked-by(id,status)\"")
	rootCmd.AddCommand(getCmd)
}
