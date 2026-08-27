package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/mdsection"
	"github.com/alphaleonis/nibs/internal/membership"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/projection"
	"github.com/spf13/cobra"
)

var (
	planJSON      bool
	planOpen      bool
	planWithOrder bool
)

// milestoneType is the nib type whose own membership is a scheduling queue
// rather than a structural decomposition. It is plan's own vocabulary: the
// bare "milestone" literals elsewhere in cmd (close, close_queue) are left as
// they are, because promoting one shared constant for the whole package is a
// decision about cmd's vocabulary rather than about plan's axis choice.
const milestoneType = "milestone"

// AcceptanceRollup is the computed acceptance-checklist rollup for one plan
// item: the checked/total count of the GitHub task-list checkboxes
// ("- [ ]"/"- [x]") in the member's Acceptance section. It is what makes
// `nibs plan` a rollup view — the agent reads progress on each member's
// acceptance criteria without opening every one to count boxes itself.
type AcceptanceRollup struct {
	Checked int `json:"checked"`
	Total   int `json:"total"`
}

// PlanItem represents a single member in a plan view: a structural child, or
// a queue entry for a milestone.
type PlanItem struct {
	Position int    `json:"position"`
	ID       string `json:"id"`
	Status   string `json:"status"`
	Type     string `json:"type,omitempty"`
	Title    string `json:"title"`
	// Order is the fractional-index sort key that placed this item in the set
	// plan reported, read from the front-matter field Plan.Axis names.
	// Unlike nib.Nib.Order (which uses json:"order,omitempty"), PlanItem.Order
	// is ALWAYS PRESENT in JSON output — agent consumers of `nibs plan --json`
	// read every item's key without having to remember a flag. It is EMPTY for
	// a member carrying no key on that axis: plan is a pure read and never
	// assigns one (see buildPlan), and an unkeyed member sorts deterministically
	// anyway — nib.SortByKey puts keyed members first and orders the rest by
	// title. So an empty value means "unpositioned, sorted by title", not
	// "unknown". The human renderer (renderPlanHuman) suppresses this column
	// unless --with-order is given.
	Order string `json:"order"`
	// Acceptance is the member's acceptance-checklist rollup, or nil when it
	// has no Acceptance section at all (so a missing section is
	// distinguishable from a present-but-empty one and omitted from JSON).
	Acceptance *AcceptanceRollup `json:"acceptance,omitempty"`
}

// PlanParent holds summary info about the container the plan is FOR — the nib
// named by the id the user passed. On the "order" axis it is the members'
// structural parent; on the "milestone_order" axis it is the milestone they
// are ASSIGNED to, which their own `parent:` need not name at all. Read
// Plan.Axis, not this field's name, to know which. The JSON key stays "parent"
// because it is the payload's wire contract.
type PlanParent struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Type   string `json:"type,omitempty"`
}

// Plan is the complete plan output.
type Plan struct {
	Parent PlanParent `json:"parent"`
	// Axis names the front-matter field every PlanItem.Order was read from —
	// "milestone_order" for a milestone's queue, "order" for any other
	// container's children. It is per-plan, not per-item, and it is the
	// discriminator a consumer needs to write back: repositioning within a
	// milestone_order set is `nibs mv --queue`, while `nibs mv` alone moves a
	// nib among its structural siblings. Without it the one `order` key means
	// two different fields (internal/projection declares them separately) and
	// a reorder built from a plan can silently land on the other axis.
	Axis  string     `json:"axis"`
	Items []PlanItem `json:"items"`
	// hiddenByOpen counts the members --open dropped. Unexported so it stays
	// out of the wire contract: it exists only so the human renderer can tell
	// an axis that holds nothing from one the filter emptied.
	hiddenByOpen int
}

// buildPlan fetches a nib and its ordered membership, returning a Plan. The
// nib itself comes from the GraphQL resolver, per the documented data flow
// (cmd -> graph -> nibcore -> nib); the member set comes from a
// membership.View, through whichever accessor planAxis paired with the axis.
//
// plan is a QUESTION, and asking it writes nothing. The member set is
// enumerated and sorted here rather than fetched through Orderer.Members,
// which backfills a missing ordering key onto a member as a side effect of
// being read: graph.Next states the rule (internal/graph/next.go) — a question
// must not edit files the caller never named, and must not fail differently on
// a read-only store.
//
// Nothing is lost by not backfilling. nib.SortByKey orders unkeyed members by
// title, which is the sequence a successful backfill freezes anyway, and a
// backfill buys no reliability: where the write cannot land it leaves the key
// empty regardless (see PlanItem.Order). What it costs is one rewritten file
// per unkeyed member — the ones --open goes on to hide included, since the set
// is read before it is filtered — each with a fresh updated_at, in a store the
// user commits from.
func buildPlan(ctx context.Context, resolver *graph.Resolver, containerID string, openOnly bool) (*Plan, error) {
	container, err := resolver.Query().Nib(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("failed to find nib: %w", err)
	}
	if container == nil {
		return nil, fmt.Errorf("nib not found: %s", containerID)
	}

	axis := planAxis(container)
	plan := &Plan{
		Parent: PlanParent{
			ID:     container.ID,
			Title:  container.Title,
			Status: container.Status,
			Type:   container.EffectiveType(),
		},
		Axis:  string(axis.Field),
		Items: []PlanItem{},
	}

	members := axis.Members(membership.Compute(resolver.Reader.All()), container.ID)
	nib.SortByKey(members, axis.Key)

	if openOnly {
		kept := filterOpen(members, resolver.Reader.Config())
		plan.hiddenByOpen = len(members) - len(kept)
		members = kept
	}

	for i, member := range members {
		item := PlanItem{
			Position:   i + 1,
			ID:         member.ID,
			Status:     member.Status,
			Type:       member.EffectiveType(),
			Title:      member.Title,
			Order:      axis.Key(member),
			Acceptance: acceptanceRollup(member.Body),
		}
		plan.Items = append(plan.Items, item)
	}

	return plan, nil
}

// planAxisSpec is one container's whole membership rule: which nibs belong,
// and which front-matter field positions them once they do.
type planAxisSpec struct {
	// Field is internal/projection's name for the positioning field, so the
	// JSON discriminator an agent reads is spelled the way every other field
	// surface spells it.
	Field projection.Field
	// Members enumerates the container's members out of a computed View.
	Members func(*membership.View, string) []*nib.Nib
	// Key reads Field off one member.
	Key func(*nib.Nib) string
}

// planAxis decides in ONE place which axis a plan is read on. Set and sort key
// are two halves of the same decision, and splitting them across two functions
// is how they drift apart.
//
// A milestone's members are its ASSIGNEES: the assignment axis is what
// schedules, and the structural parent edge on its own schedules nothing.
// membership.View.DirectMembers reads that axis for a milestone, and its
// exclusion of milestone-typed entries is right there — a milestone is a
// container of its own and is never a member of anything.
//
// Any other container's members are its structural children, read through
// membership.View.Children, which is membership's accessor for that axis
// ("every type, containers included"). The same type exclusion would be a
// silent narrowing here: `rel --rel children`, the web tree, the TUI,
// cmd/close.go and graph's reorder validation all read the unfiltered set, so
// hiding a milestone nested under an epic would under-report the container and
// break the round trip — `mv --children-of` fed the ids plan printed is
// refused with "missing child in reorder list".
//
// The key has to be the one that positions a member IN this set: an assignee's
// `order:` is its position among structural siblings, a different group
// entirely. membership hands out sets and leaves ordering to its consumers by
// design, so the pairing is made here, as cmd/roadmap.go pairs a sort with
// each of the same two axes.
func planAxis(container *nib.Nib) planAxisSpec {
	if container.EffectiveType() == milestoneType {
		return planAxisSpec{
			Field:   projection.FieldMilestoneOrder,
			Members: (*membership.View).DirectMembers,
			Key:     func(b *nib.Nib) string { return b.MilestoneOrder },
		}
	}
	return planAxisSpec{
		Field:   projection.FieldOrder,
		Members: (*membership.View).Children,
		Key:     func(b *nib.Nib) string { return b.Order },
	}
}

// acceptanceRollup counts the GitHub task-list checkboxes in a nib body's
// Acceptance section. It matches an "## Acceptance" heading first (the current
// body template) then "## Acceptance Criteria" (older bodies), and returns nil
// when neither section exists. Only "- [ ]" / "- [x]" list items are counted;
// checked is the "[x]"/"[X]" subset.
func acceptanceRollup(body string) *AcceptanceRollup {
	section, found := mdsection.Find(body, "Acceptance", mdsection.AnyLevel)
	if !found {
		section, found = mdsection.Find(body, "Acceptance Criteria", mdsection.AnyLevel)
	}
	if !found {
		return nil
	}
	r := &AcceptanceRollup{}
	for _, line := range strings.Split(section, "\n") {
		mark, ok := checkboxMark(line)
		if !ok {
			continue
		}
		r.Total++
		if mark == 'x' || mark == 'X' {
			r.Checked++
		}
	}
	return r
}

// checkboxMark reports the mark character of a GitHub task-list item
// ("- [ ] …", "- [x] …", "* [X] …") and whether the line is such an item.
func checkboxMark(line string) (byte, bool) {
	t := strings.TrimSpace(line)
	// Shortest valid item is "- [ ]" (5 chars).
	if len(t) < 5 {
		return 0, false
	}
	if (t[0] != '-' && t[0] != '*') || t[1] != ' ' || t[2] != '[' || t[4] != ']' {
		return 0, false
	}
	return t[3], true
}

// filterOpen keeps the nibs carrying an open status — the same open *group*
// that `-s open` expands to, which is what `--open` means on list and rel.
//
// It is membership in OpenStatusNames rather than !IsClosedStatus, and the two
// differ on exactly one input: a nib whose status is outside the vocabulary (a
// hand-edited file with no `status:` holds "") is in neither group, so it is
// dropped here and would be kept by an exclude-closed rule. Matching list is
// the point — one flag name meant two different memberships, and the narrow
// case where they diverge is precisely the malformed data nobody checks.
//
// Note this is NOT list's open-by-*default* rule, which is exclude-closed and
// therefore does keep a statusless nib. Passing --open is a narrowing on both
// commands; see cmd/statusfilter.go.
func filterOpen(nibs []*nib.Nib, cfg *config.Config) []*nib.Nib {
	open := make(map[string]bool, len(cfg.OpenStatusNames()))
	for _, name := range cfg.OpenStatusNames() {
		open[name] = true
	}
	var result []*nib.Nib
	for _, b := range nibs {
		if open[b.Status] {
			result = append(result, b)
		}
	}
	return result
}

var planCmd = &cobra.Command{
	Use:   "plan <id>",
	Short: "Display plan view for a nib and its ordered membership",
	Long: `Shows an ordered view of a nib's membership with position, status, title, and acceptance criteria.

A milestone's membership is the queue its assignees hold in milestone_order;
any other nib's is its children, in order.`,
	Args: codedExactArgs(&planJSON, 1),
	RunE: func(cmd *cobra.Command, args []string) error {
		app := getApp(cmd)
		resolver := app.newResolver()

		// Thread cmd.Context() into buildPlan so this command becomes
		// cancelable once root-level signal wiring lands (cmd.Execute
		// currently calls rootCmd.Execute(), not ExecuteContext, so
		// cmd.Context() is nil today — in both production and tests).
		// Guard against nil so the change is safe to land ahead of the
		// root-level wiring.
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}

		plan, err := buildPlan(ctx, resolver, args[0], planOpen)
		if err != nil {
			if planJSON {
				return output.Error(output.ErrNotFound, err.Error())
			}
			return err
		}

		if planJSON {
			return output.JSONRaw(plan)
		}

		// Human-readable output
		return renderPlanHuman(plan)
	},
}

func renderPlanHuman(plan *Plan) error {
	fmt.Printf("%s  %s\n\n", plan.Parent.ID, plan.Parent.Title)

	if len(plan.Items) == 0 {
		// Two different facts share this branch once --open is in play: the
		// axis holds nothing, or it holds only members the filter dropped.
		// Reporting the second as the first sends the reader off to populate a
		// queue that is already populated, so the hidden count is what
		// separates them. Name the axis either way — Parent.Type carries the
		// same EffectiveType() planAxis chose from, so the two cannot
		// disagree.
		noun := "children"
		if plan.Parent.Type == milestoneType {
			noun = "queue entries"
		}
		if plan.hiddenByOpen > 0 {
			fmt.Printf("No open %s (%d hidden by --open).\n", noun, plan.hiddenByOpen)
		} else {
			fmt.Printf("No %s.\n", noun)
		}
		return nil
	}

	for _, item := range plan.Items {
		// item.Order is empty for a member carrying no key on the axis read —
		// plan assigns none (see buildPlan) — so --with-order renders a bare
		// "order=" there. That is the honest report: the member is unpositioned
		// and sorted by title.
		line := fmt.Sprintf("  %d. [%s] %s (%s)", item.Position, item.Status, item.Title, item.ID)
		if item.Acceptance != nil && item.Acceptance.Total > 0 {
			line += fmt.Sprintf(" %d/%d", item.Acceptance.Checked, item.Acceptance.Total)
		}
		if planWithOrder {
			line += " order=" + item.Order
		}
		fmt.Println(line)
	}

	return nil
}

func init() {
	planCmd.Flags().BoolVar(&planJSON, "json", false, "Output as JSON")
	planCmd.Flags().BoolVar(&planOpen, "open", false, "Show only open items — shorthand for the open status group, the same set list/rel's --open selects")
	planCmd.Flags().BoolVar(&planWithOrder, "with-order", false, "Show each item's order key in the default (non-JSON) output (JSON always includes order)")
	rootCmd.AddCommand(planCmd)
}
