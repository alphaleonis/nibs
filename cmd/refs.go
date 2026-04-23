package cmd

import (
	"context"
	"fmt"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/ui"
	"github.com/spf13/cobra"
)

var (
	refsInbound bool
	refsJSON    bool
)

var refsCmd = &cobra.Command{
	Use:   "refs <id>",
	Short: "Show body references (#<id> mentions) for a nib",
	Long: `Shows the #<id> mentions embedded in a nib's body.

By default, lists outbound mentions (nibs that <id>'s body references).
Use --inbound to list the reverse: nibs whose bodies mention <id>.

Body references use the #<id> syntax — either the short form (#gx0f)
or the full form (#nibs-gx0f). Bare IDs without the # sigil are not
recognised as mentions. References inside fenced code blocks or inline
code spans are ignored.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		app := getApp(cmd)
		resolver := app.newResolver()
		ctx := context.Background()

		b, err := resolver.Query().Nib(ctx, args[0])
		if err != nil {
			if refsJSON {
				return output.Error(output.ErrNotFound, err.Error())
			}
			return fmt.Errorf("failed to find nib: %w", err)
		}
		if b == nil {
			if refsJSON {
				return output.Error(output.ErrNotFound, fmt.Sprintf("nib not found: %s", args[0]))
			}
			return fmt.Errorf("nib not found: %s", args[0])
		}

		var results []*nib.Nib
		if refsInbound {
			results = app.Core.FindMentionedBy(b.ID)
		} else {
			results = app.Core.FindMentions(b.ID)
		}

		if refsJSON {
			if results == nil {
				results = []*nib.Nib{}
			}
			return output.SuccessMultiple(results)
		}

		if len(results) == 0 {
			direction := "outbound"
			if refsInbound {
				direction = "inbound"
			}
			fmt.Println(ui.Muted.Render(fmt.Sprintf("No %s mentions for %s.", direction, b.ID)))
			return nil
		}

		cfg := app.Config()
		for _, r := range results {
			statusCfg := cfg.GetStatus(r.Status)
			statusColor := "gray"
			if statusCfg != nil {
				statusColor = statusCfg.Color
			}
			isArchive := cfg.IsArchiveStatus(r.Status)
			fmt.Printf("%s  %s  %s\n",
				ui.ID.Render(r.ID),
				ui.RenderStatusWithColor(r.Status, statusColor, isArchive),
				r.Title,
			)
		}
		return nil
	},
}

func init() {
	refsCmd.Flags().BoolVar(&refsInbound, "inbound", false, "Show inbound mentions (nibs whose bodies reference this one)")
	refsCmd.Flags().BoolVar(&refsJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(refsCmd)
}
