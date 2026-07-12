package cmd

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcontext"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/ui"
	"github.com/spf13/cobra"
)

var contextJSON bool

// contextOutput is the lean, agent-facing shape emitted by `nibs context`.
// The `progress` values are the canonical child-completion rollup
// (graph.ProgressRollup) — byte-identical to `nibs get <id> -f progress` — so
// an agent never has to re-sum children. Overview mode (no id) populates
// Milestones; detail mode (an id) populates Root and the subtree fields.
type contextOutput struct {
	// Detail mode (a specific nib's subtree summary).
	Root        *nibcontext.NibRef    `json:"root,omitempty"`
	Progress    *graph.ProgressRollup `json:"progress,omitempty"`
	ActivePhase *nibcontext.NibRef    `json:"active_phase,omitempty"`
	ActiveTasks []*nibcontext.NibRef  `json:"active_tasks,omitempty"`
	NextTasks   []*nibcontext.NibRef  `json:"next_tasks,omitempty"`
	Decisions   []string              `json:"decisions,omitempty"`

	// Overview mode (all active milestones).
	Milestones []milestoneContext `json:"milestones,omitempty"`

	Warnings []string `json:"warnings,omitempty"`
}

// milestoneContext is one active milestone in the overview. The embedded NibRef
// promotes id/title/status/type to the top level; Progress is the canonical
// child-completion rollup over the milestone's direct children.
type milestoneContext struct {
	*nibcontext.NibRef
	Progress    graph.ProgressRollup `json:"progress"`
	ActivePhase *nibcontext.NibRef   `json:"active_phase,omitempty"`
}

var contextCmd = &cobra.Command{
	Use:   "context [nib-id]",
	Short: "Show project context summary",
	Long: `Displays a project status summary for a nib and its descendants.

Without an argument, shows an overview of all active milestones, each with a
progress rollup (done = completed children, total excludes scrapped) and its
active phase.

With a nib ID, shows a subtree summary for that nib: the same progress rollup
over its direct children, plus active tasks, next tasks, and key decisions
(works for milestones, epics, features — any nib with children).`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		app := getApp(cmd)
		resolver := app.newResolver()

		allNibs, err := resolver.Query().Nibs(context.Background(), nil, nil)
		if err != nil {
			return cmdError(contextJSON, output.ErrFileError, "failed to query nibs: %v", err)
		}

		var rootID string
		if len(args) > 0 {
			rootID = args[0]
			// Resolve short/bare ids (e.g. "din3") to their full form
			// ("nibs-din3") before building the summary, using the same
			// single-id resolution path show/links use (Core.Get via the Nib
			// resolver). BuildSummary keys on full ids only, so an unresolved
			// short id would spuriously report "nib not found". On an unknown id
			// the resolver returns nil; we leave rootID as the raw arg so
			// BuildSummary still emits the not-found warning.
			if resolved, rerr := resolver.Query().Nib(context.Background(), rootID); rerr != nil {
				return cmdError(contextJSON, output.ErrFileError, "failed to resolve nib %q: %v", rootID, rerr)
			} else if resolved != nil {
				rootID = resolved.ID
			}
		}

		sum := nibcontext.BuildSummary(allNibs, rootID)
		out := buildContextOutput(sum, rootID, directChildStatuses(allNibs))

		if contextJSON {
			return output.JSONRaw(out)
		}
		return renderContextPretty(out)
	},
}

// buildContextOutput adapts the structural summary from nibcontext into the
// lean context output, replacing nibcontext's weighted progress with the
// canonical child-completion rollup so it matches `nibs get -f progress`.
func buildContextOutput(sum nibcontext.Summary, rootID string, childStatuses map[string][]string) contextOutput {
	out := contextOutput{Warnings: sum.Warnings}

	// Overview mode: one entry per active milestone with its rollup.
	if rootID == "" {
		for _, c := range sum.Containers {
			out.Milestones = append(out.Milestones, milestoneContext{
				NibRef:      &c.NibRef,
				Progress:    graph.ComputeProgress(childStatuses[c.ID]),
				ActivePhase: c.ActivePhase,
			})
		}
		return out
	}

	// Detail mode: subtree summary for the resolved nib.
	out.Root = sum.Root
	out.ActivePhase = sum.ActivePhase
	out.ActiveTasks = sum.ActiveTasks
	out.NextTasks = sum.NextTasks
	out.Decisions = sum.Decisions
	if sum.Root != nil {
		rollup := graph.ComputeProgress(childStatuses[rootID])
		out.Progress = &rollup
	}
	return out
}

// directChildStatuses maps each parent id to the statuses of its direct
// children — the input graph.ComputeProgress needs to build the rollup for any
// node without re-scanning the whole set.
func directChildStatuses(allNibs []*nib.Nib) map[string][]string {
	m := make(map[string][]string)
	for _, n := range allNibs {
		if n.Parent != "" {
			m[n.Parent] = append(m[n.Parent], n.Status)
		}
	}
	return m
}

func renderContextPretty(out contextOutput) error {
	for _, w := range out.Warnings {
		fmt.Println(ui.Warning.Render("⚠ " + w))
	}

	// Overview mode: active milestones with rollups.
	if len(out.Milestones) > 0 {
		fmt.Println(ui.Header.Render("Milestones"))
		for _, m := range out.Milestones {
			renderMilestoneLine(m)
		}
		fmt.Println()
		return nil
	}

	// Detail mode.
	if out.Root != nil {
		fmt.Println(ui.Header.Render("Root"))
		fmt.Printf("  %s  %s\n\n", ui.ID.Render(out.Root.ID), ui.Title.Render(out.Root.Title))
	}

	if out.ActivePhase != nil {
		fmt.Println(ui.Header.Render("Active Phase"))
		fmt.Printf("  %s  %s\n\n", ui.ID.Render(out.ActivePhase.ID), out.ActivePhase.Title)
	}

	if out.Progress != nil {
		fmt.Println(ui.Header.Render("Progress"))
		renderProgressBar(*out.Progress)
		fmt.Println()
	}

	if len(out.ActiveTasks) > 0 {
		fmt.Println(ui.Header.Render("Active Tasks"))
		for _, n := range out.ActiveTasks {
			fmt.Printf("  %s  %s\n", ui.ID.Render(n.ID), n.Title)
		}
		fmt.Println()
	}

	if len(out.NextTasks) > 0 {
		fmt.Println(ui.Header.Render("Next Tasks"))
		for _, n := range out.NextTasks {
			fmt.Printf("  %s  %s\n", ui.ID.Render(n.ID), n.Title)
		}
		fmt.Println()
	}

	if len(out.Decisions) > 0 {
		fmt.Println(ui.Header.Render("Key Decisions"))
		for _, d := range out.Decisions {
			fmt.Printf("  • %s\n", d)
		}
		fmt.Println()
	}

	return nil
}

// renderMilestoneLine renders one overview milestone: a compact bar + percent +
// done/total from the canonical rollup, plus the active phase if any.
func renderMilestoneLine(m milestoneContext) {
	const barWidth = 20
	line := fmt.Sprintf("  %s  %s %s  %s",
		ui.ID.Render(m.ID),
		progressBar(m.Progress, barWidth),
		ui.Bold.Render(fmt.Sprintf("%3d%%", m.Progress.Percent)),
		ui.Title.Render(m.Title))
	line += "  " + ui.Muted.Render(fmt.Sprintf("(%d/%d)", m.Progress.Done, m.Progress.Total))
	if m.ActivePhase != nil {
		line += fmt.Sprintf("  %s %s", ui.Muted.Render("phase:"), m.ActivePhase.Title)
	}
	fmt.Println(line)
}

// renderProgressBar renders the detail-mode progress bar for a rollup.
func renderProgressBar(p graph.ProgressRollup) {
	const barWidth = 30
	fmt.Printf("  %s %s %s\n",
		progressBar(p, barWidth),
		ui.Bold.Render(fmt.Sprintf("%d%%", p.Percent)),
		ui.Muted.Render(fmt.Sprintf("(%d/%d)", p.Done, p.Total)))
}

// progressBar builds a filled/empty bar of the given width from a rollup's
// percent. An empty rollup (total 0) renders as all-empty.
func progressBar(p graph.ProgressRollup, width int) string {
	filled := 0
	if p.Total > 0 {
		filled = int(math.Round(float64(width) * float64(p.Percent) / 100))
	}
	if filled > width {
		filled = width
	}
	return ui.Success.Render(strings.Repeat("█", filled)) +
		ui.Muted.Render(strings.Repeat("░", width-filled))
}

func init() {
	contextCmd.Flags().BoolVar(&contextJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(contextCmd)
}
