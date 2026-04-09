package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/ui"
	"github.com/spf13/cobra"
)

var (
	showJSON     bool
	showRaw      bool
	showBodyOnly bool
	showETagOnly bool
)

var showCmd = &cobra.Command{
	Use:   "show <id> [id...]",
	Short: "Show a nib's contents",
	Long:  `Displays the full contents of one or more nibs, including front matter and body.`,
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		app := getApp(cmd)
		resolver := app.newResolver()

		// Collect all nibs
		var nibs []*nib.Nib
		for _, id := range args {
			b, err := resolver.Query().Nib(context.Background(), id)
			if err != nil {
				if showJSON {
					return output.Error(output.ErrNotFound, err.Error())
				}
				return fmt.Errorf("failed to find nib: %w", err)
			}
			if b == nil {
				if showJSON {
					return output.Error(output.ErrNotFound, fmt.Sprintf("nib not found: %s", id))
				}
				return fmt.Errorf("nib not found: %s", id)
			}
			nibs = append(nibs, b)
		}

		// JSON output
		if showJSON {
			filtered := filterResolvedBlockers(nibs, app.Core)
			if len(filtered) == 1 {
				return output.SuccessSingle(filtered[0])
			}
			return output.SuccessMultiple(filtered)
		}

		// Raw markdown output (frontmatter + body)
		if showRaw {
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

		// Body only (no header, no styling)
		if showBodyOnly {
			for i, b := range nibs {
				if i > 0 {
					fmt.Print("\n---\n\n")
				}
				fmt.Print(b.Body)
			}
			return nil
		}

		// ETag only (for easy extraction in scripts)
		if showETagOnly {
			for i, b := range nibs {
				if i > 0 {
					fmt.Println()
				}
				fmt.Print(b.ETag())
			}
			return nil
		}

		// Default: styled human-friendly output
		for i, b := range nibs {
			if i > 0 {
				fmt.Println()
				fmt.Println(ui.Muted.Render(strings.Repeat("═", 60)))
				fmt.Println()
			}
			showStyledNib(b, computeBlockingIDs(b, app.Core), app.Config())
		}

		return nil
	},
}

// computeBlockingIDs returns the IDs of active nibs that this nib is blocking,
// computed from other nibs' blockedBy fields via FindIncomingLinks.
// Filters out resolved (completed/scrapped) nibs — a resolved nib is not
// considered to be blocking anything, and resolved blockees are not shown.
func computeBlockingIDs(b *nib.Nib, reader graph.NibReader) []string {
	if nib.IsResolvedStatus(b.Status) {
		return nil
	}
	incoming := reader.FindIncomingLinks(b.ID)
	var ids []string
	for _, link := range incoming {
		if link.LinkType == "blocked_by" && !nib.IsResolvedStatus(link.FromNib.Status) {
			ids = append(ids, link.FromNib.ID)
		}
	}
	return ids
}

// showStyledNib displays a single nib with styled output.
func showStyledNib(b *nib.Nib, blockingIDs []string, cfg *config.Config) {
	statusCfg := cfg.GetStatus(b.Status)
	statusColor := "gray"
	if statusCfg != nil {
		statusColor = statusCfg.Color
	}
	isArchive := cfg.IsArchiveStatus(b.Status)

	var header strings.Builder
	header.WriteString(ui.ID.Render(b.ID))
	header.WriteString(" ")
	header.WriteString(ui.RenderStatusWithColor(b.Status, statusColor, isArchive))

	// Display type
	if b.Type != "" {
		typeCfg := cfg.GetType(b.Type)
		typeColor := "gray"
		if typeCfg != nil {
			typeColor = typeCfg.Color
		}
		header.WriteString(" ")
		header.WriteString(ui.RenderTypeWithColor(b.Type, typeColor))
	}

	if b.Priority != "" {
		priorityCfg := cfg.GetPriority(b.Priority)
		priorityColor := "gray"
		if priorityCfg != nil {
			priorityColor = priorityCfg.Color
		}
		header.WriteString(" ")
		header.WriteString(ui.RenderPriorityWithColor(b.Priority, priorityColor))
	}
	if b.Estimate != "" {
		estimateCfg := cfg.GetEstimate(b.Estimate)
		estimateColor := "gray"
		if estimateCfg != nil {
			estimateColor = estimateCfg.Color
		}
		header.WriteString(" ")
		header.WriteString(ui.RenderEstimateWithColor(b.Estimate, estimateColor))
	}
	if len(b.Tags) > 0 {
		header.WriteString("  ")
		header.WriteString(ui.Muted.Render(strings.Join(b.Tags, ", ")))
	}
	header.WriteString("\n")
	header.WriteString(ui.Title.Render(b.Title))

	// Display relationships
	if b.Parent != "" || len(blockingIDs) > 0 {
		header.WriteString("\n")
		header.WriteString(ui.Muted.Render(strings.Repeat("─", 50)))
		header.WriteString("\n")
		header.WriteString(formatRelationships(b, blockingIDs))
	}

	header.WriteString("\n")
	header.WriteString(ui.Muted.Render(strings.Repeat("─", 50)))

	headerBox := lipgloss.NewStyle().
		MarginBottom(1).
		Render(header.String())

	fmt.Println(headerBox)

	// Render the body with Glamour
	if b.Body != "" {
		renderer, err := glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
			glamour.WithWordWrap(80),
		)
		if err != nil {
			fmt.Printf("failed to create renderer: %v\n", err)
			return
		}

		rendered, err := renderer.Render(b.Body)
		if err != nil {
			fmt.Printf("failed to render markdown: %v\n", err)
			return
		}

		fmt.Print(rendered)
	}
}

// formatRelationships formats parent and blocking relationships for display.
// blockingIDs are computed from other nibs' blockedBy fields.
func formatRelationships(b *nib.Nib, blockingIDs []string) string {
	var parts []string

	// Display parent
	if b.Parent != "" {
		parts = append(parts, fmt.Sprintf("%s %s",
			ui.Muted.Render("parent:"),
			ui.ID.Render(b.Parent)))
	}

	// Display blocking (computed from incoming links)
	for _, target := range blockingIDs {
		parts = append(parts, fmt.Sprintf("%s %s",
			ui.Muted.Render("blocking:"),
			ui.ID.Render(target)))
	}
	return strings.Join(parts, "\n")
}

func init() {
	showCmd.Flags().BoolVar(&showJSON, "json", false, "Output as JSON")
	showCmd.Flags().BoolVar(&showRaw, "raw", false, "Output raw markdown without styling")
	showCmd.Flags().BoolVar(&showBodyOnly, "body-only", false, "Output only the body content")
	showCmd.Flags().BoolVar(&showETagOnly, "etag-only", false, "Output only the etag")
	showCmd.MarkFlagsMutuallyExclusive("json", "raw", "body-only", "etag-only")
	rootCmd.AddCommand(showCmd)
}
