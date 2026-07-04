package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/alphaleonis/nibs/internal/nibcontext"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/ui"
	"github.com/spf13/cobra"
)

var contextJSON bool

var contextCmd = &cobra.Command{
	Use:   "context [nib-id]",
	Short: "Show project context summary",
	Long: `Displays a project status summary for a nib and its descendants:
progress, active phase, active tasks, next tasks, and key decisions.

Without an argument, shows an overview of all active milestones
with progress bars, active phases, and current tasks.

With a nib ID, shows a detailed summary for that nib's subtree
(works for milestones, epics, features — any nib with children).`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		app := getApp(cmd)
		resolver := app.newResolver()

		// Fetch all nibs
		allNibs, err := resolver.Query().Nibs(context.Background(), nil, nil)
		if err != nil {
			return cmdError(contextJSON, output.ErrFileError, "failed to query nibs: %v", err)
		}

		var rootID string
		if len(args) > 0 {
			rootID = args[0]
			// Resolve short/bare ids (e.g. "din3") to their full form
			// ("nibs-din3") before building the summary, using the same
			// single-id resolution path show/links use (Core.Get via the
			// Nib resolver). BuildSummary keys on full ids only, so an
			// unresolved short id would spuriously report "nib not found".
			// On an unknown id the resolver returns nil; we leave rootID as
			// the raw arg so BuildSummary still emits the not-found warning.
			if resolved, rerr := resolver.Query().Nib(context.Background(), rootID); rerr != nil {
				return cmdError(contextJSON, output.ErrFileError, "failed to resolve nib %q: %v", rootID, rerr)
			} else if resolved != nil {
				rootID = resolved.ID
			}
		}

		sum := nibcontext.BuildSummary(allNibs, rootID)

		if contextJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(sum)
		}

		return renderContextPretty(sum)
	},
}

func renderContextPretty(sum nibcontext.Summary) error {
	// Warnings
	for _, w := range sum.Warnings {
		fmt.Println(ui.Warning.Render("⚠ " + w))
	}

	// Overview mode: show containers
	if len(sum.Containers) > 0 {
		fmt.Println(ui.Header.Render("Milestones"))
		for _, c := range sum.Containers {
			renderContainerLine(c)
		}
		fmt.Println()
	}

	// Root nib (detail mode)
	if sum.Root != nil {
		fmt.Println(ui.Header.Render("Root"))
		fmt.Printf("  %s  %s\n", ui.ID.Render(sum.Root.ID), ui.Title.Render(sum.Root.Title))
		fmt.Println()
	}

	// Active Phase
	if sum.ActivePhase != nil {
		fmt.Println(ui.Header.Render("Active Phase"))
		fmt.Printf("  %s  %s\n", ui.ID.Render(sum.ActivePhase.ID), sum.ActivePhase.Title)
		fmt.Println()
	}

	// Progress
	fmt.Println(ui.Header.Render("Progress"))
	renderProgressBar(sum.Progress)
	fmt.Println()

	// Active Tasks
	if len(sum.ActiveTasks) > 0 {
		fmt.Println(ui.Header.Render("Active Tasks"))
		for _, n := range sum.ActiveTasks {
			fmt.Printf("  %s  %s\n", ui.ID.Render(n.ID), n.Title)
		}
		fmt.Println()
	}

	// Next Tasks
	if len(sum.NextTasks) > 0 {
		fmt.Println(ui.Header.Render("Next Tasks"))
		for _, n := range sum.NextTasks {
			fmt.Printf("  %s  %s\n", ui.ID.Render(n.ID), n.Title)
		}
		fmt.Println()
	}

	// Key Decisions
	if len(sum.Decisions) > 0 {
		fmt.Println(ui.Header.Render("Key Decisions"))
		for _, d := range sum.Decisions {
			fmt.Printf("  • %s\n", d)
		}
		fmt.Println()
	}

	return nil
}

func renderContainerLine(c *nibcontext.ContainerSummary) {
	// Progress bar (compact)
	const barWidth = 20
	filled := 0
	if c.Progress.TotalWeight > 0 {
		filled = int(math.Round(float64(barWidth) * c.Progress.Percentage / 100))
	}
	empty := barWidth - filled

	bar := ui.Success.Render(strings.Repeat("█", filled)) +
		ui.Muted.Render(strings.Repeat("░", empty))

	pct := fmt.Sprintf("%3.0f%%", c.Progress.Percentage)

	line := fmt.Sprintf("  %s  %s %s  %s",
		ui.ID.Render(c.ID),
		bar, ui.Bold.Render(pct),
		ui.Title.Render(c.Title))

	if c.ActivePhase != nil {
		line += fmt.Sprintf("  %s %s",
			ui.Muted.Render("phase:"),
			c.ActivePhase.Title)
	}

	fmt.Println(line)
}

func renderProgressBar(p nibcontext.Progress) {
	const barWidth = 30
	filled := 0
	if p.TotalWeight > 0 {
		filled = int(math.Round(float64(barWidth) * p.Percentage / 100))
	}
	empty := barWidth - filled

	bar := ui.Success.Render(strings.Repeat("█", filled)) +
		ui.Muted.Render(strings.Repeat("░", empty))

	pct := fmt.Sprintf("%.0f%%", p.Percentage)
	weight := fmt.Sprintf("(%d/%d pts)", p.CompletedWeight, p.TotalWeight)

	fmt.Printf("  %s %s %s\n", bar, ui.Bold.Render(pct), ui.Muted.Render(weight))
}

func init() {
	contextCmd.Flags().BoolVar(&contextJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(contextCmd)
}
