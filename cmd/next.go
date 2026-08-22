package cmd

import (
	"fmt"
	"strings"

	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcontext"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/ui"
	"github.com/spf13/cobra"
)

var nextJSON bool

// nextOutput is the agent-facing shape of `nibs next`. Action is always
// present — null when there is no answer — so a consumer branches on a field
// rather than on the presence of one, and every "no answer" carries a reason
// TOKEN beside its sentence so the branch never has to parse prose.
type nextOutput struct {
	Action *nibcontext.NibRef `json:"action"`
	// Milestone is the derived active milestone, present whenever one derives
	// — including when its queue had nothing to offer, since naming it is half
	// the answer.
	Milestone *nibcontext.NibRef `json:"milestone,omitempty"`
	// QueuePosition is Action's 1-based place in that milestone's queue.
	// Omitted when the answer did not come from a queue.
	QueuePosition int `json:"queue_position,omitempty"`
	// Path is the provenance: the queue entry (or root) first, then the
	// descent through containers, ending at Action.
	Path []*nibcontext.NibRef `json:"path,omitempty"`
	// Fallback is set when the answer came from the store's tree order rather
	// than from a plan — the honest label decision 1.4's derivation requires.
	Fallback *nextNote `json:"fallback,omitempty"`
	// NoAnswer is set when the walk produced nothing.
	NoAnswer *nextNote `json:"no_answer,omitempty"`
	// PassedOver counts what the walk declined on its way, so a "nothing to
	// do" answer is checkable. Omitted when it declined nothing.
	PassedOver *nextTally `json:"passed_over,omitempty"`
	// Inversions are the queue entries skipped because they sit ahead of a
	// blocker that is itself later in the queue (decision 2.3).
	Inversions []nextInversion `json:"inversions,omitempty"`
}

// nextNote pairs a stable reason token with the sentence a human reads.
type nextNote struct {
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

// nextTally is graph.NextTally on the wire.
type nextTally struct {
	Closed   int `json:"closed"`
	Blocked  int `json:"blocked"`
	Open     int `json:"open"`
	Inverted int `json:"inverted"`
}

// nextInversion is one skipped queue entry and the blocker it sits ahead of.
type nextInversion struct {
	Milestone string `json:"milestone"`
	Ahead     string `json:"ahead"`
	Blocker   string `json:"blocker"`
	Message   string `json:"message"`
}

var nextCmd = &cobra.Command{
	Use:   "next",
	Short: "Show the one thing to work on next, with the plan it came from",
	Long: `Answers "what do I do": the first startable work in the active milestone's queue.

The active milestone is DERIVED, never stored: the in-progress milestone that
comes first in milestone order. Its queue (the nibs assigned to it, in
milestone_order) is walked in order, descending each entry's decomposition
until it reaches something with nothing open left under it — a leaf, or a
container whose children are all closed, which is then itself the action. The
first such nib that is startable is the answer, and the provenance shows which
milestone it came from and the path that reached it.

Startable means exactly what 'nibs list --ready' means: a startable status and
no active blocker (a deferred blocker still blocks). Anything else is passed
over — including a queue entry that sits ahead of a blocker that is itself
later in the queue, which is skipped with its whole subtree: such order-vs-
dependency inversions are legal (plans state importance, dependencies state
feasibility) and are reported, not refused.

With no milestone in progress the same walk runs over the store's own tree
order and the answer is labeled a FALLBACK — a flat ordered list is a complete
use of nibs, so the question still has an answer. Once a milestone IS active,
next speaks only for it: an empty queue, or one with nothing startable, is
reported as such rather than routed around.

Having nothing to do is an answer, not a failure: next exits 0 either way.
Branch on --json's "action" being null, never on the exit status.`,
	Args: codedNoArgs(&nextJSON),
	RunE: func(cmd *cobra.Command, args []string) error {
		app := getApp(cmd)
		resolver := app.newResolver()

		res := graph.Next(resolver.Reader, resolver.Blocking)
		out := buildNextOutput(res)

		if nextJSON {
			return output.JSONRaw(out)
		}
		renderNextPretty(out)
		return nil
	},
}

// buildNextOutput adapts the walk's result — which holds live store pointers —
// into the lean, detached output shape both renderers read.
func buildNextOutput(res graph.NextResult) nextOutput {
	out := nextOutput{
		Action:        nextRef(res.Action),
		Milestone:     nextRef(res.Milestone),
		QueuePosition: res.Position,
		Path:          nextRefs(res.Path),
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
	if res.Tally.Any() {
		out.PassedOver = &nextTally{
			Closed:   res.Tally.Closed,
			Blocked:  res.Tally.Blocked,
			Open:     res.Tally.Open,
			Inverted: res.Tally.Inverted,
		}
	}
	for _, inv := range res.Inversions {
		out.Inversions = append(out.Inversions, nextInversion{
			Milestone: inv.Milestone,
			Ahead:     inv.Ahead.ID,
			Blocker:   inv.Blocker.ID,
			Message: fmt.Sprintf("skipped %s: it sits ahead of %s, which still blocks it (inversions are legal — reorder with `nibs mv %s --queue --after %s` if the order was unintended)",
				stripControlChars(inv.Ahead.ID), stripControlChars(inv.Blocker.ID),
				stripControlChars(inv.Ahead.ID), stripControlChars(inv.Blocker.ID)),
		})
	}
	return out
}

// fallbackMessage says why the walk left the plan and what would bring it
// back. Phrased for both outcomes: the fallback walk may itself find nothing.
func fallbackMessage(reason graph.NextReason) string {
	switch reason {
	case graph.NextReasonNoMilestones:
		return "this store declares no milestones, so next looked at the store's own tree order instead of a plan — " +
			"create one with `nibs new \"<title>\" -t milestone` and fill its queue with `nibs set <id> --milestone <milestone>`"
	case graph.NextReasonNoActiveMilestone:
		return "no milestone is in progress, so next looked at the store's own tree order instead of a plan — " +
			"start one with `nibs set <milestone> -s in-progress` (the active milestone is derived: in progress, earliest in milestone order)"
	}
	return string(reason)
}

// noAnswerMessage names the situation the walk ended in, and what it declined
// on the way, so "nothing to do" is checkable rather than merely asserted.
func noAnswerMessage(res graph.NextResult) string {
	if res.NoAnswerReason == graph.NextReasonEmptyQueue && res.Milestone != nil {
		return fmt.Sprintf("milestone %s is active but its queue is empty — assign work with `nibs set <id> --milestone %s`",
			stripControlChars(res.Milestone.ID), stripControlChars(res.Milestone.ID))
	}
	where := "in this store"
	remedy := ""
	if res.Milestone != nil {
		where = fmt.Sprintf("in milestone %s's queue", stripControlChars(res.Milestone.ID))
		remedy = fmt.Sprintf(" — `nibs list --milestone %s` shows the queue; unblock, close or assign work to change this",
			stripControlChars(res.Milestone.ID))
	}
	if !res.Tally.Any() {
		return "nothing startable " + where + ": there is no work to walk" + remedy
	}
	return "nothing startable " + where + ": " + tallyPhrase(res.Tally) + remedy
}

// tallyPhrase renders the non-zero counters as one clause, naming what each
// count means rather than only how many there were.
func tallyPhrase(t graph.NextTally) string {
	var parts []string
	if t.Closed > 0 {
		parts = append(parts, fmt.Sprintf("%d closed", t.Closed))
	}
	if t.Blocked > 0 {
		parts = append(parts, fmt.Sprintf("%d blocked", t.Blocked))
	}
	if t.Open > 0 {
		parts = append(parts, fmt.Sprintf("%d open but not startable", t.Open))
	}
	if t.Inverted > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped as an order-vs-dependency inversion", t.Inverted))
	}
	return strings.Join(parts, ", ")
}

// renderNextPretty writes the human form: the answer first, then the
// provenance that justifies it, with anything surprising called out above both.
func renderNextPretty(out nextOutput) {
	if out.Fallback != nil {
		ui.Println(ui.Warning.Render("⚠ fallback: " + out.Fallback.Message))
		ui.Println()
	}
	for _, inv := range out.Inversions {
		ui.Println(ui.Warning.Render("⚠ " + inv.Message))
		ui.Println()
	}

	ui.Println(ui.Header.Render("Next"))
	if out.Action == nil {
		if out.NoAnswer != nil {
			ui.Printf("  %s\n", out.NoAnswer.Message)
		} else {
			ui.Printf("  %s\n", ui.Muted.Render("nothing startable"))
		}
		ui.Println()
		return
	}
	ui.Printf("  %s  %s  %s\n\n",
		ui.ID.Render(out.Action.ID),
		ui.Title.Render(out.Action.Title),
		ui.Muted.Render("("+out.Action.Type+" · "+out.Action.Status+")"))

	// A one-step path with no milestone traces nothing the answer line has not
	// already said — the flat-store fallback landing on a root.
	if out.Milestone == nil && len(out.Path) <= 1 {
		return
	}
	ui.Println(ui.Header.Render("Reached through"))
	indent := "  "
	if out.Milestone != nil {
		renderNextStep(indent, out.Milestone, "active milestone")
		indent += "  "
	}
	for i, step := range out.Path {
		note := ""
		if i == 0 && out.QueuePosition > 0 {
			note = fmt.Sprintf("queue position %d", out.QueuePosition)
		}
		renderNextStep(indent, step, note)
		indent += "  "
	}
	ui.Println()
}

// renderNextStep writes one provenance line, appending the note only when
// there is one so an un-annotated step carries no trailing whitespace.
func renderNextStep(indent string, ref *nibcontext.NibRef, note string) {
	line := indent + ui.ID.Render(ref.ID) + "  " + ref.Title
	if note != "" {
		line += "  " + ui.Muted.Render(note)
	}
	ui.Println(line)
}

// nextRef projects one live store pointer onto the detached reference shape
// `nibs context` already publishes, so the two agent surfaces describe a nib
// the same way.
func nextRef(b *nib.Nib) *nibcontext.NibRef {
	if b == nil {
		return nil
	}
	return &nibcontext.NibRef{
		ID:       b.ID,
		Title:    b.Title,
		Status:   b.Status,
		Type:     b.EffectiveType(),
		Estimate: b.Estimate,
	}
}

func nextRefs(nibs []*nib.Nib) []*nibcontext.NibRef {
	if len(nibs) == 0 {
		return nil
	}
	refs := make([]*nibcontext.NibRef, len(nibs))
	for i, b := range nibs {
		refs[i] = nextRef(b)
	}
	return refs
}

func init() {
	nextCmd.Flags().BoolVar(&nextJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(nextCmd)
}
