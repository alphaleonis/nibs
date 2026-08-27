package cmd

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/membership"
	"github.com/alphaleonis/nibs/internal/nibcontext"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/progress"
	"github.com/alphaleonis/nibs/internal/ui"
	"github.com/spf13/cobra"
)

var contextJSON bool

// contextOutput is the lean, agent-facing shape emitted by `nibs context`.
// The `progress` values are the canonical child-completion rollup
// (progress.Rollup) — byte-identical to `nibs get <id> -f progress` — so
// an agent never has to re-sum children. Overview mode (no id) populates
// Milestones; detail mode (an id) populates Root and the subtree fields.
type contextOutput struct {
	// Detail mode (a specific nib's subtree summary).
	Root        *nibcontext.NibRef   `json:"root,omitempty"`
	Progress    *progress.Rollup     `json:"progress,omitempty"`
	ActivePhase *nibcontext.NibRef   `json:"active_phase,omitempty"`
	ActiveTasks []*nibcontext.NibRef `json:"active_tasks,omitempty"`
	NextTasks   []*nibcontext.NibRef `json:"next_tasks,omitempty"`
	Decisions   []string             `json:"decisions,omitempty"`

	// Overview mode (all active milestones).
	//
	// ActiveMilestone is the one milestone decision 1.4 derives as active:
	// in progress, earliest in milestone order. It is derived per call and
	// never stored, and it is absent when no milestone is in progress — the
	// Milestones list holds every non-closed milestone whether or not one of
	// them is active, so its first entry means nothing on its own.
	ActiveMilestone *nibcontext.NibRef `json:"active_milestone,omitempty"`
	Milestones      []milestoneContext `json:"milestones,omitempty"`
	Next            *contextNext       `json:"next,omitempty"`

	// Warnings carries the summary's own warnings and, in overview mode, the
	// queue inversions `nibs next` reports — the one thing that command warns
	// about for which Next has no field of its own here.
	Warnings []string `json:"warnings,omitempty"`
}

// contextNext is what `nibs next` answers, carried in the overview so that
// "where are we" and "what do I do" are one call rather than two joined by
// hand. It REFERENCES that answer rather than restating it: the action, the
// queue entry it was reached through with that entry's position, and the
// labels that qualify it.
//
// The milestone the answer came from is the overview's own ActiveMilestone,
// and the two agree by a DATA invariant rather than a structural one.
// graph.Next takes no membership view and computes its own, so the rule runs
// twice — once here over this command's Nibs(nil, nil), once inside Next over
// Reader.All(). Both are the store's whole unfiltered set, which is why the
// answers match; nothing in the types forces them to stay that way, and
// TestContextOverviewAgreesWithNext compares the two published answers and is
// what would catch them diverging.
//
// The provenance path and the passed-over tally stay with `nibs next`; its
// queue inversions are carried in the overview's Warnings.
type contextNext struct {
	// Action is the nib to work on, null when the walk found none, so a
	// consumer branches on the field rather than on its presence.
	Action *nibcontext.NibRef `json:"action"`
	// QueueEntry is the active milestone's queue member the walk descended
	// from to reach Action — Action itself when the entry was startable, and
	// an ancestor of it otherwise. Absent when the answer did not come from a
	// queue.
	QueueEntry *nibcontext.NibRef `json:"queue_entry,omitempty"`
	// QueuePosition is QueueEntry's 1-based place in that queue — the ENTRY's
	// position, not Action's. Omitted when the answer did not come from a
	// queue.
	QueuePosition int `json:"queue_position,omitempty"`
	// Fallback is set when the answer came from the store's own tree order
	// rather than from a plan.
	Fallback *nextNote `json:"fallback,omitempty"`
	// NoAnswer is set when the walk produced nothing.
	NoAnswer *nextNote `json:"no_answer,omitempty"`
}

// milestoneContext is one active milestone in the overview. The embedded NibRef
// promotes id/title/status/type to the top level; Progress is the canonical
// child-completion rollup over the milestone's direct children.
type milestoneContext struct {
	*nibcontext.NibRef
	Progress    progress.Rollup    `json:"progress"`
	ActivePhase *nibcontext.NibRef `json:"active_phase,omitempty"`
}

var contextCmd = &cobra.Command{
	Use:   "context [nib-id]",
	Short: "Show project context summary",
	Long: `Displays a project status summary for a nib and its descendants.

Without an argument, shows an overview of all active milestones, each with a
progress rollup (done = completed children, total excludes scrapped) and its
active phase. The overview also names the derived active milestone — the
in-progress one that comes first in milestone order — and what 'nibs next'
answers from it, so "where are we, what do I do" is a single call. With no
milestone in progress nothing is active, and the answer is labeled a fallback
exactly as 'nibs next' labels it.

With a nib ID, shows a subtree summary for that nib: the same progress rollup
over its direct children, plus active tasks, next tasks, and key decisions
(works for milestones, epics, features — any nib with children).`,
	Args: codedMaximumNArgs(&contextJSON, 1),
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

		view := membership.Compute(allNibs)
		sum := nibcontext.BuildSummaryWithView(allNibs, view, rootID, app.Config())
		out := buildContextOutput(sum, rootID, view)
		if rootID == "" {
			// The overview is the "where are we" call, so it answers "what do
			// I do" too. Both facts come from the functions `nibs next` itself
			// calls — graph.ActiveMilestone and graph.Next — rather than from
			// a rule restated here, which is what keeps the two surfaces from
			// answering one question two ways.
			res := graph.Next(resolver.Reader, resolver.Blocking)
			out.ActiveMilestone = nextRef(graph.ActiveMilestone(view))
			out.Next = buildContextNext(res)
			out.Warnings = append(out.Warnings, inversionWarnings(res)...)
		}

		if contextJSON {
			return output.JSONRaw(out)
		}
		return renderContextPretty(out)
	},
}

// buildContextOutput adapts the structural summary from nibcontext into the
// lean context output, replacing nibcontext's weighted progress with the
// canonical child-completion rollup so it matches `nibs get -f progress`.
func buildContextOutput(sum nibcontext.Summary, rootID string, view *membership.View) contextOutput {
	out := contextOutput{Warnings: sum.Warnings}

	// Overview mode: one entry per active milestone with its rollup.
	if rootID == "" {
		for _, c := range sum.Containers {
			out.Milestones = append(out.Milestones, milestoneContext{
				NibRef:      &c.NibRef,
				Progress:    progress.ByCount(memberStatuses(view, c.ID)),
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
		rollup := progress.ByCount(memberStatuses(view, rootID))
		out.Progress = &rollup
	}
	return out
}

// memberStatuses projects a container's direct members to their statuses —
// the input progress.ByCount needs to build the rollup for a node.
func memberStatuses(view *membership.View, containerID string) []string {
	members := view.DirectMembers(containerID)
	statuses := make([]string, len(members))
	for i, m := range members {
		statuses[i] = m.Status
	}
	return statuses
}

// buildContextNext adapts the walk's result — which holds live store pointers
// — into the overview's reference to it, reusing `nibs next`'s own messages so
// the two surfaces explain a fallback or an empty answer in the same words.
func buildContextNext(res graph.NextResult) *contextNext {
	out := &contextNext{
		Action:        nextRef(res.Action),
		QueuePosition: res.Position,
	}
	// A non-zero position is set only by the queue walk, which records the
	// entry it took as Path[0] in the same step — so the number always has the
	// nib it describes beside it, and cannot be read as the action's own.
	if res.Position > 0 {
		out.QueueEntry = nextRef(res.Path[0])
	}
	if res.FallbackReason != "" {
		out.Fallback = &nextNote{
			Reason:  string(res.FallbackReason),
			Message: fallbackMessage(res.FallbackReason),
		}
	}
	if res.NoAnswerReason != "" {
		out.NoAnswer = &nextNote{
			Reason:  string(res.NoAnswerReason),
			Message: noAnswerMessage(res),
		}
	}
	return out
}

// inversionWarnings renders the queue inversions the walk passed over ON ITS
// WAY TO THIS ANSWER — exactly the set `nibs next` reports and no wider, since
// the walk returns at the first startable entry and never examines the queue
// beyond it. Carrying that set is what keeps the overview usable as an agent's
// one call: a queue handing out work ahead of its own dependency is visible
// here as well as in `nibs next`, in the same words.
//
// One warning per skipped ENTRY, not per (entry, blocker) pair. graph.Next
// records a pair for every blocker an entry sits ahead of, so a head that gates
// the rest of its queue emits one pair per gated entry — a fan-out that would
// bury the overview's own answer, which prints below these lines. Per entry is
// also the count the walk's own Inverted tally reports.
//
// A single-blocker skip keeps `nibs next`'s sentence verbatim, so the common
// case cannot be worded two ways. An aggregate has no counterpart there — that
// command prints the pairs — so it is built here from the same lead and the
// same remedy, naming the LAST blocker in queue order: QueueInversionsIn
// returns an entry's pairs ordered by the blocker's position, so moving the
// entry after that one clears every pair it has.
func inversionWarnings(res graph.NextResult) []string {
	var order []string
	byEntry := make(map[string][]nextInversion)
	for _, inv := range buildNextOutput(res).Inversions {
		if _, seen := byEntry[inv.Ahead]; !seen {
			order = append(order, inv.Ahead)
		}
		byEntry[inv.Ahead] = append(byEntry[inv.Ahead], inv)
	}
	var warnings []string
	for _, ahead := range order {
		warnings = append(warnings, inversionWarning(byEntry[ahead]))
	}
	return warnings
}

// inversionWarning words one skipped entry's inversions as a single sentence,
// bounded in length however many blockers the entry sits ahead of: the count,
// the one id the remedy needs, and where the pairs themselves are listed.
func inversionWarning(pairs []nextInversion) string {
	if len(pairs) == 1 {
		return pairs[0].Message
	}
	last := pairs[len(pairs)-1]
	return fmt.Sprintf("skipped %s: it sits ahead of %d later blockers, the last of them %s, which still block it "+
		"(inversions are legal — reorder with `nibs mv %s --queue --after %s` if the order was unintended; `nibs next` names every pair)",
		stripControlChars(last.Ahead), len(pairs), stripControlChars(last.Blocker),
		stripControlChars(last.Ahead), stripControlChars(last.Blocker))
}

func renderContextPretty(out contextOutput) error {
	// Warnings lead, as `nibs next` prints its own, and the block is closed
	// with a blank line — without it the body's first header runs straight on
	// from the last warning and the answer below is harder to find than the
	// caveat above it.
	for _, w := range out.Warnings {
		ui.Println(ui.Warning.Render("⚠ " + w))
	}
	if len(out.Warnings) > 0 {
		ui.Println()
	}

	// Overview mode. Next is populated for the overview alone, so it is what
	// tells the two modes apart: a store that declares no milestone has an
	// empty list and still has an answer to show.
	if out.Next != nil {
		renderContextOverview(out)
		return nil
	}

	// Detail mode.
	if out.Root != nil {
		ui.Println(ui.Header.Render("Root"))
		ui.Printf("  %s  %s\n\n", ui.ID.Render(out.Root.ID), ui.Title.Render(out.Root.Title))
	}

	if out.ActivePhase != nil {
		ui.Println(ui.Header.Render("Active Phase"))
		ui.Printf("  %s  %s\n\n", ui.ID.Render(out.ActivePhase.ID), out.ActivePhase.Title)
	}

	if out.Progress != nil {
		ui.Println(ui.Header.Render("Progress"))
		renderProgressBar(*out.Progress)
		ui.Println()
	}

	if len(out.ActiveTasks) > 0 {
		ui.Println(ui.Header.Render("Active Tasks"))
		for _, n := range out.ActiveTasks {
			ui.Printf("  %s  %s\n", ui.ID.Render(n.ID), n.Title)
		}
		ui.Println()
	}

	if len(out.NextTasks) > 0 {
		ui.Println(ui.Header.Render("Next Tasks"))
		for _, n := range out.NextTasks {
			ui.Printf("  %s  %s\n", ui.ID.Render(n.ID), n.Title)
		}
		ui.Println()
	}

	if len(out.Decisions) > 0 {
		ui.Println(ui.Header.Render("Key Decisions"))
		for _, d := range out.Decisions {
			ui.Printf("  • %s\n", d)
		}
		ui.Println()
	}

	return nil
}

// renderContextOverview writes the overview: every non-closed milestone with
// the one that is active marked, then what `nibs next` answers. The "none
// active" line is not decoration — without it a list of milestones with no
// marker in it reads as though its first entry were the one being worked on.
// An EMPTY list needs no such line: there is no entry to be mistaken for the
// active one, and the fallback below says what the store holds instead.
func renderContextOverview(out contextOutput) {
	ui.Println(ui.Header.Render("Milestones"))
	for _, m := range out.Milestones {
		renderMilestoneLine(m, out.ActiveMilestone)
	}
	switch {
	case len(out.Milestones) == 0:
		ui.Println("  " + ui.Muted.Render("none"))
	case out.ActiveMilestone == nil:
		ui.Println("  " + ui.Muted.Render("none active — no milestone is in progress"))
	}
	ui.Println()
	renderContextNext(out.Next)
}

// renderContextNext writes the answer to "what do I do" with the labels that
// qualify it — the same sentences `nibs next` prints for a fallback or a
// no-answer. It is deliberately shorter than that command's own rendering,
// which additionally traces the whole path that reached the action.
func renderContextNext(n *contextNext) {
	ui.Println(ui.Header.Render("Next"))
	if n.Fallback != nil {
		ui.Println("  " + ui.Warning.Render("⚠ fallback: "+n.Fallback.Message))
	}
	if n.Action == nil {
		// A no-answer is an answer, and it carries its own explanation:
		// graph.Next names a NoAnswerReason on every path that leaves Action
		// nil, so there is no wordless case to substitute prose for.
		ui.Printf("  %s\n\n", n.NoAnswer.Message)
		return
	}
	line := fmt.Sprintf("  %s  %s  %s",
		ui.ID.Render(n.Action.ID),
		ui.Title.Render(n.Action.Title),
		ui.Muted.Render("("+n.Action.Type+" · "+n.Action.Status+")"))
	if n.QueuePosition > 0 {
		line += "  " + ui.Muted.Render(queuePositionNote(n))
	}
	ui.Println(line)
	ui.Println()
}

// queuePositionNote says whose queue position the number is. The action is
// usually a descendant of the entry that holds the position, so printing the
// number alone beside the action reads as the action's own — which it is not.
func queuePositionNote(n *contextNext) string {
	if n.QueueEntry != nil && n.QueueEntry.ID != n.Action.ID {
		return fmt.Sprintf("via %s · queue position %d", n.QueueEntry.ID, n.QueuePosition)
	}
	return fmt.Sprintf("queue position %d", n.QueuePosition)
}

// renderMilestoneLine renders one overview milestone: a compact bar + percent +
// done/total from the canonical rollup, the active marker, plus the active
// phase if any.
func renderMilestoneLine(m milestoneContext, active *nibcontext.NibRef) {
	const barWidth = 20
	marker := "  "
	isActive := active != nil && active.ID == m.ID
	if isActive {
		marker = "▸ "
	}
	line := fmt.Sprintf("  %s%s  %s %s  %s",
		marker,
		ui.ID.Render(m.ID),
		progressBar(m.Progress, barWidth),
		ui.Bold.Render(fmt.Sprintf("%3d%%", m.Progress.Percent)),
		ui.Title.Render(m.Title))
	line += "  " + ui.Muted.Render(fmt.Sprintf("(%d/%d)", m.Progress.Done, m.Progress.Total))
	if isActive {
		line += "  " + ui.Muted.Render("active")
	}
	if m.ActivePhase != nil {
		line += fmt.Sprintf("  %s %s", ui.Muted.Render("phase:"), m.ActivePhase.Title)
	}
	ui.Println(line)
}

// renderProgressBar renders the detail-mode progress bar for a rollup.
func renderProgressBar(p progress.Rollup) {
	const barWidth = 30
	ui.Printf("  %s %s %s\n",
		progressBar(p, barWidth),
		ui.Bold.Render(fmt.Sprintf("%d%%", p.Percent)),
		ui.Muted.Render(fmt.Sprintf("(%d/%d)", p.Done, p.Total)))
}

// progressBar builds a filled/empty bar of the given width from a rollup's
// percent. An empty rollup (total 0) renders as all-empty.
func progressBar(p progress.Rollup, width int) string {
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
