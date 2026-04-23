package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/ui"
	"github.com/spf13/cobra"
)

var (
	refsInbound    bool
	refsBoth       bool
	refsJSON       bool
	refsStatus     []string
	refsNoStatus   []string
	refsType       []string
	refsNoType     []string
	refsPriority   []string
	refsActive     bool
)

// refsBothResult is the JSON shape emitted by `nibs refs --both --json`.
// Outbound and inbound lists are both always present (may be empty) so the
// shape is stable for consumers.
type refsBothResult struct {
	Success  bool       `json:"success"`
	Outbound []*nib.Nib `json:"outbound"`
	Inbound  []*nib.Nib `json:"inbound"`
}

var refsCmd = &cobra.Command{
	Use:   "refs <id>",
	Short: "Show body references (#<id> mentions) for a nib",
	Long: `Shows the #<id> mentions embedded in a nib's body.

By default, lists outbound mentions (nibs that <id>'s body references).
Use --inbound to list the reverse: nibs whose bodies mention <id>.
Use --both to fetch outbound AND inbound in one call; --both cannot be
combined with --inbound.

Filter flags apply to the mention list (to both directions under --both):
  --status / --no-status      (repeatable) filter by status
  --type   / --no-type        (repeatable) filter by type
  --priority                  (repeatable) filter by priority
  --active                    shorthand: exclude completed/scrapped

Body references use the #<id> syntax — either the short form (#gx0f)
or the full form (#nibs-gx0f). Bare IDs without the # sigil are not
recognised as mentions.

Rules the parser applies:
- Only lowercase ASCII letters/digits plus internal hyphens are accepted
  in the id body; uppercase IDs (#ABCD) are ignored.
- ` + "`#`" + ` adjacent to a word-like char (email#foo, name_#bar) is ignored.
- Content inside fenced or indented code blocks, inline code spans,
  links, images, and raw HTML is skipped entirely.
- Self-references and tokens that do not resolve to a known nib drop
  silently — if ` + "`nibs refs`" + ` shows fewer results than expected, check
  the body for typos or unresolved IDs.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if refsBoth && refsInbound {
			// --both always fetches both directions in one envelope;
			// --inbound fetches only inbound. Combining them is ambiguous.
			err := fmt.Errorf("--both and --inbound are mutually exclusive")
			if refsJSON {
				return output.Error(output.ErrValidation, err.Error())
			}
			return err
		}

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

		filter := buildRefsFilter()

		if refsBoth {
			outbound, err := resolver.Nib().Mentions(ctx, b, filter)
			if err != nil {
				return fmt.Errorf("resolving outbound mentions: %w", err)
			}
			inbound, err := resolver.Nib().MentionedBy(ctx, b, filter)
			if err != nil {
				return fmt.Errorf("resolving inbound mentions: %w", err)
			}
			if refsJSON {
				if outbound == nil {
					outbound = []*nib.Nib{}
				}
				if inbound == nil {
					inbound = []*nib.Nib{}
				}
				return output.JSONRaw(refsBothResult{Success: true, Outbound: outbound, Inbound: inbound})
			}
			cfg := app.Config()
			out := cmd.OutOrStdout()
			renderRefsSection(out, "Outbound", outbound, cfg)
			_, _ = fmt.Fprintln(out)
			renderRefsSection(out, "Inbound", inbound, cfg)
			return nil
		}

		var results []*nib.Nib
		if refsInbound {
			results, err = resolver.Nib().MentionedBy(ctx, b, filter)
		} else {
			results, err = resolver.Nib().Mentions(ctx, b, filter)
		}
		if err != nil {
			return fmt.Errorf("resolving mentions: %w", err)
		}

		if refsJSON {
			if results == nil {
				results = []*nib.Nib{}
			}
			return output.SuccessMultiple(results)
		}

		out := cmd.OutOrStdout()
		if len(results) == 0 {
			direction := "outbound"
			if refsInbound {
				direction = "inbound"
			}
			_, _ = fmt.Fprintln(out, ui.Muted.Render(fmt.Sprintf("No %s mentions for %s.", direction, b.ID)))
			return nil
		}

		cfg := app.Config()
		for _, r := range results {
			_, _ = fmt.Fprintln(out, formatRefsRow(r, cfg))
		}
		return nil
	},
}

// buildRefsFilter translates the refs CLI filter flags into a NibFilter that
// can be passed to the mentions / mentionedBy resolvers. Returns nil when
// no flags are set so the resolver short-circuits the filter step.
func buildRefsFilter() *model.NibFilter {
	if len(refsStatus) == 0 && len(refsNoStatus) == 0 &&
		len(refsType) == 0 && len(refsNoType) == 0 &&
		len(refsPriority) == 0 && !refsActive {
		return nil
	}
	excludeStatus := append([]string(nil), refsNoStatus...)
	if refsActive {
		// Match the semantic used by `plan --active` / `list --ready`:
		// drop completed/scrapped. Archived nibs are stored separately
		// and are already excluded from the main nib list.
		excludeStatus = append(excludeStatus, "completed", "scrapped")
	}
	return &model.NibFilter{
		Status:        refsStatus,
		ExcludeStatus: excludeStatus,
		Type:          refsType,
		ExcludeType:   refsNoType,
		Priority:      refsPriority,
	}
}

// renderRefsSection prints a labelled refs section with the given rows;
// when rows is empty it emits a muted "No <label> mentions." line so
// callers can always see both sections under --both.
func renderRefsSection(out io.Writer, label string, rows []*nib.Nib, cfg *config.Config) {
	_, _ = fmt.Fprintln(out, ui.Title.Render(label+":"))
	if len(rows) == 0 {
		_, _ = fmt.Fprintln(out, ui.Muted.Render(fmt.Sprintf("  No %s mentions.", strings.ToLower(label))))
		return
	}
	for _, r := range rows {
		_, _ = fmt.Fprintln(out, "  "+formatRefsRow(r, cfg))
	}
}

// formatRefsRow renders one mention row (id, status, title).
func formatRefsRow(r *nib.Nib, cfg *config.Config) string {
	statusCfg := cfg.GetStatus(r.Status)
	statusColor := "gray"
	if statusCfg != nil {
		statusColor = statusCfg.Color
	}
	isArchive := cfg.IsArchiveStatus(r.Status)
	return fmt.Sprintf("%s  %s  %s",
		ui.ID.Render(r.ID),
		ui.RenderStatusWithColor(r.Status, statusColor, isArchive),
		r.Title)
}

func init() {
	refsCmd.Flags().BoolVar(&refsInbound, "inbound", false, "Show inbound mentions (nibs whose bodies reference this one)")
	refsCmd.Flags().BoolVar(&refsBoth, "both", false, "Show outbound AND inbound mentions in one call")
	refsCmd.Flags().BoolVar(&refsJSON, "json", false, "Output as JSON")
	refsCmd.Flags().StringArrayVarP(&refsStatus, "status", "s", nil, "Filter mentions by status (repeatable)")
	refsCmd.Flags().StringArrayVar(&refsNoStatus, "no-status", nil, "Exclude mentions by status (repeatable)")
	refsCmd.Flags().StringArrayVarP(&refsType, "type", "t", nil, "Filter mentions by type (repeatable)")
	refsCmd.Flags().StringArrayVar(&refsNoType, "no-type", nil, "Exclude mentions by type (repeatable)")
	refsCmd.Flags().StringArrayVarP(&refsPriority, "priority", "p", nil, "Filter mentions by priority (repeatable)")
	refsCmd.Flags().BoolVar(&refsActive, "active", false, "Exclude completed/scrapped mentions (shorthand for --no-status completed --no-status scrapped)")
	rootCmd.AddCommand(refsCmd)
}
