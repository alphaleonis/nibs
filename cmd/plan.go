package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/mdsection"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/spf13/cobra"
)

var (
	planJSON      bool
	planActive    bool
	planWithOrder bool
)

// AcceptanceRollup is the computed acceptance-checklist rollup for one plan
// item: the checked/total count of the GitHub task-list checkboxes
// ("- [ ]"/"- [x]") in the child's Acceptance section. It is what makes
// `nibs plan` a rollup view — the agent reads progress on each child's
// acceptance criteria without opening every child to count boxes itself.
type AcceptanceRollup struct {
	Checked int `json:"checked"`
	Total   int `json:"total"`
}

// PlanItem represents a single child in a plan view.
type PlanItem struct {
	Position int    `json:"position"`
	ID       string `json:"id"`
	Status   string `json:"status"`
	Type     string `json:"type,omitempty"`
	Title    string `json:"title"`
	// Order is the fractional-index sort key for this item among its siblings.
	// Unlike nib.Nib.Order (which uses json:"order,omitempty"), PlanItem.Order
	// is ALWAYS included in JSON output — agent consumers of `nibs plan --json`
	// rely on every item carrying an order key without having to remember a
	// flag. In practice the value is always non-empty because the resolver's
	// Orderer backfills missing keys before this struct is built. The human
	// renderer (renderPlanHuman) suppresses this column unless --with-order
	// is given.
	Order string `json:"order"`
	// Acceptance is the child's acceptance-checklist rollup, or nil when the
	// child has no Acceptance section at all (so a missing section is
	// distinguishable from a present-but-empty one and omitted from JSON).
	Acceptance *AcceptanceRollup `json:"acceptance,omitempty"`
}

// PlanParent holds summary info about the parent nib.
type PlanParent struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Type   string `json:"type,omitempty"`
}

// Plan is the complete plan output.
type Plan struct {
	Parent PlanParent `json:"parent"`
	Items  []PlanItem `json:"items"`
}

// buildPlan fetches a parent nib and its ordered children, returning a Plan.
// Uses the GraphQL resolver for parent lookup and child ordering, consistent
// with the documented data flow (cmd -> graph -> nibcore -> nib).
func buildPlan(ctx context.Context, resolver *graph.Resolver, parentID string, activeOnly bool) (*Plan, error) {
	parent, err := resolver.Query().Nib(ctx, parentID)
	if err != nil {
		return nil, fmt.Errorf("failed to find nib: %w", err)
	}
	if parent == nil {
		return nil, fmt.Errorf("nib not found: %s", parentID)
	}

	plan := &Plan{
		Parent: PlanParent{
			ID:     parent.ID,
			Title:  parent.Title,
			Status: parent.Status,
			Type:   parent.EffectiveType(),
		},
		Items: []PlanItem{},
	}

	// Get children sorted by order via the resolver, which handles
	// backfilling order keys for legacy nibs without them.
	children := resolver.Orderer.GetSortedSiblings(parent.ID)

	// Filter if active only
	if activeOnly {
		children = filterActive(children, resolver.Reader.Config())
	}

	// Build plan items
	for i, child := range children {
		item := PlanItem{
			Position:   i + 1,
			ID:         child.ID,
			Status:     child.Status,
			Type:       child.EffectiveType(),
			Title:      child.Title,
			Order:      child.Order,
			Acceptance: acceptanceRollup(child.Body),
		}
		plan.Items = append(plan.Items, item)
	}

	return plan, nil
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

// filterActive removes closed (completed and scrapped) nibs.
func filterActive(nibs []*nib.Nib, cfg *config.Config) []*nib.Nib {
	var result []*nib.Nib
	for _, b := range nibs {
		if !cfg.IsClosedStatus(b.Status) {
			result = append(result, b)
		}
	}
	return result
}

var planCmd = &cobra.Command{
	Use:   "plan <parent-id>",
	Short: "Display plan view for a parent nib and its children",
	Long:  `Shows an ordered view of a parent nib's children with position, status, title, and acceptance criteria.`,
	Args:  codedExactArgs(&planJSON, 1),
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

		plan, err := buildPlan(ctx, resolver, args[0], planActive)
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
		fmt.Println("No children.")
		return nil
	}

	for _, item := range plan.Items {
		// item.Order is always non-empty here: GetSortedSiblings backfills missing
		// keys via Orderer.backfillOrderKeys before items are built.
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
	planCmd.Flags().BoolVar(&planActive, "active", false, "Show only active items (exclude completed/scrapped)")
	planCmd.Flags().BoolVar(&planWithOrder, "with-order", false, "Show each item's order key in the default (non-JSON) output (JSON always includes order)")
	rootCmd.AddCommand(planCmd)
}
