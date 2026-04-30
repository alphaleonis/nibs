package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	listJSON        bool
	listSearch      string
	listStatus      []string
	listNoStatus    []string
	listType        []string
	listNoType      []string
	listPriority    []string
	listNoPriority  []string
	listEstimate    []string
	listNoEstimate  []string
	listTag         []string
	listNoTag       []string
	listHasParent   bool
	listNoParent    bool
	listParentID    string
	listHasBlocking bool
	listNoBlocking  bool
	listIsBlocked   bool
	listMentions    string
	listMentionedBy string
	listReady       bool
	listQuiet       bool
	listSort        string
	listFull        bool
)

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all nibs",
	Long: `Lists all nibs in the .nibs directory.

Position column (#):
  The leftmost # column shows each nib's natural-order position among its
  siblings (per-parent, 1-based). It is independent of --sort, so under
  --sort priority the numbers will appear non-monotonic — that's by design,
  letting you reference "move from 2 to 5" regardless of the current sort.

Search Syntax (--search/-S):
  The search flag supports Bleve query string syntax:

  login          Exact term match
  login~         Fuzzy match (1 edit distance, finds "loggin", "logins")
  login~2        Fuzzy match (2 edit distance)
  log*           Wildcard prefix match
  "user login"   Exact phrase match
  user AND login Both terms required
  user OR login  Either term matches
  slug:auth      Search only in slug field
  title:login    Search only in title field
  body:auth      Search only in body field`,
	RunE: func(cmd *cobra.Command, args []string) error {
		app := getApp(cmd)
		// Build GraphQL filter from CLI flags
		filter := &model.NibFilter{
			Status:          listStatus,
			ExcludeStatus:   listNoStatus,
			Type:            listType,
			ExcludeType:     listNoType,
			Priority:        listPriority,
			ExcludePriority: listNoPriority,
			Estimate:        listEstimate,
			ExcludeEstimate: listNoEstimate,
			Tags:            listTag,
			ExcludeTags:     listNoTag,
		}

		// Add search filter if provided
		if listSearch != "" {
			filter.Search = &listSearch
		}

		// Add parent/blocks filters
		if listHasParent {
			filter.HasParent = &listHasParent
		}
		if listNoParent {
			filter.NoParent = &listNoParent
		}
		if listParentID != "" {
			filter.ParentID = &listParentID
		}
		if listHasBlocking {
			filter.HasBlocking = &listHasBlocking
		}
		if listNoBlocking {
			filter.NoBlocking = &listNoBlocking
		}
		// MentionsID / MentionedByID accept short or full IDs; the GraphQL
		// filter layer normalises via NibReader.NormalizeID in ApplyFilter
		// (internal/graph/filters.go:resolveFilterID). Do not normalise at
		// the CLI layer.
		if listMentions != "" {
			filter.MentionsID = &listMentions
		}
		if listMentionedBy != "" {
			filter.MentionedByID = &listMentionedBy
		}
		// --ready and --is-blocked are mutually exclusive
		if listReady && listIsBlocked {
			return fmt.Errorf("--ready and --is-blocked are mutually exclusive")
		}

		if listIsBlocked {
			filter.IsBlocked = &listIsBlocked
		}

		// --ready: nibs available to start (not blocked, excludes in-progress/completed/scrapped/draft)
		if listReady {
			isBlocked := false
			filter.IsBlocked = &isBlocked
			filter.ExcludeStatus = append(filter.ExcludeStatus, "in-progress", "completed", "scrapped", "draft")
		}

		// Execute query via GraphQL resolver with sort
		nibSort := buildNibSort(listSort)
		resolver := app.newResolver()
		nibs, err := resolver.Query().Nibs(context.Background(), filter, nibSort)
		if err != nil {
			return fmt.Errorf("querying nibs: %w", err)
		}

		// JSON output (flat list) — work on copies to avoid mutating Core state
		if listJSON {
			filtered := filterResolvedBlockers(nibs, app.Core)
			if !listFull {
				for i, b := range filtered {
					clone := *b
					clone.Body = ""
					filtered[i] = &clone
				}
			}
			// Always emit `[]` for empty list fields so agent consumers can
			// rely on `jq '.[]'` without special-casing null.
			if filtered == nil {
				filtered = []*nib.Nib{}
			}
			return output.SuccessMultiple(filtered)
		}

		// Quiet mode: just IDs (flat)
		if listQuiet {
			for _, b := range nibs {
				fmt.Println(b.ID)
			}
			return nil
		}

		// Default: tree view
		// We need all nibs to find ancestors for context
		allNibs, err := resolver.Query().Nibs(context.Background(), nil, nil)
		if err != nil {
			return fmt.Errorf("querying all nibs for tree: %w", err)
		}

		// Create sort function for tree building
		sortFn := func(b []*nib.Nib) {
			graph.ApplySorting(b, nibSort, app.Config())
		}

		// Build tree
		tree := ui.BuildTree(nibs, allNibs, sortFn)

		if len(tree) == 0 {
			fmt.Println(ui.Muted.Render("No nibs found. Create one with: nibs new <title>"))
			return nil
		}

		// Calculate max ID width from all nibs in tree
		maxIDWidth := 2
		for _, b := range allNibs {
			if len(b.ID) > maxIDWidth {
				maxIDWidth = len(b.ID)
			}
		}
		maxIDWidth += 2

		// Check if any nibs have tags
		hasTags := false
		for _, b := range nibs {
			if len(b.Tags) > 0 {
				hasTags = true
				break
			}
		}

		// Detect terminal width (default to 80 if not a terminal)
		termWidth := 80
		if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
			termWidth = w
		}

		// Per-parent natural-order position map (independent of --sort).
		positions := nib.PositionMap(allNibs)

		fmt.Print(ui.RenderTree(tree, app.Config(), maxIDWidth, hasTags, termWidth, positions))
		return nil
	},
}

// buildNibSort maps CLI --sort flag values to a GraphQL NibSort.
// Time sorts (created, updated) default to DESC (newest first).
func buildNibSort(sortFlag string) *model.NibSort {
	desc := model.SortDirectionDesc

	switch sortFlag {
	case "created":
		return &model.NibSort{Field: model.NibSortFieldCreatedAt, Direction: &desc}
	case "updated":
		return &model.NibSort{Field: model.NibSortFieldUpdatedAt, Direction: &desc}
	case "status":
		return &model.NibSort{Field: model.NibSortFieldStatus}
	case "priority":
		return &model.NibSort{Field: model.NibSortFieldPriority}
	case "status-priority":
		return &model.NibSort{Field: model.NibSortFieldStatusPriority}
	case "id":
		return &model.NibSort{Field: model.NibSortFieldID}
	default:
		return &model.NibSort{Field: model.NibSortFieldOrder}
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func init() {
	listCmd.Flags().BoolVar(&listJSON, "json", false, "Output as JSON")
	listCmd.Flags().StringVarP(&listSearch, "search", "S", "", "Full-text search in title and body")
	listCmd.Flags().StringArrayVarP(&listStatus, "status", "s", nil, "Filter by status (can be repeated)")
	listCmd.Flags().StringArrayVar(&listNoStatus, "no-status", nil, "Exclude by status (can be repeated)")
	listCmd.Flags().StringArrayVarP(&listType, "type", "t", nil, "Filter by type (can be repeated)")
	listCmd.Flags().StringArrayVar(&listNoType, "no-type", nil, "Exclude by type (can be repeated)")
	listCmd.Flags().StringArrayVarP(&listPriority, "priority", "p", nil, "Filter by priority (can be repeated)")
	listCmd.Flags().StringArrayVar(&listNoPriority, "no-priority", nil, "Exclude by priority (can be repeated)")
	listCmd.Flags().StringArrayVarP(&listEstimate, "estimate", "e", nil, "Filter by estimate (can be repeated)")
	listCmd.Flags().StringArrayVar(&listNoEstimate, "no-estimate", nil, "Exclude by estimate (can be repeated)")
	listCmd.Flags().StringArrayVar(&listTag, "tag", nil, "Filter by tag (can be repeated, OR logic)")
	listCmd.Flags().StringArrayVar(&listNoTag, "no-tag", nil, "Exclude nibs with tag (can be repeated)")
	listCmd.Flags().BoolVar(&listHasParent, "has-parent", false, "Filter nibs with a parent")
	listCmd.Flags().BoolVar(&listNoParent, "no-parent", false, "Filter nibs without a parent")
	listCmd.Flags().StringVar(&listParentID, "parent", "", "Filter by parent ID")
	listCmd.Flags().BoolVar(&listHasBlocking, "has-blocking", false, "Filter nibs that are blocking others")
	listCmd.Flags().BoolVar(&listNoBlocking, "no-blocking", false, "Filter nibs that aren't blocking others")
	listCmd.Flags().BoolVar(&listIsBlocked, "is-blocked", false, "Filter nibs that are blocked by others")
	listCmd.Flags().StringVar(&listMentions, "mentions", "", "Filter nibs whose bodies mention this ID (short or full)")
	listCmd.Flags().StringVar(&listMentionedBy, "mentioned-by", "", "Filter nibs mentioned in the given ID's body (short or full)")
	listCmd.Flags().BoolVar(&listReady, "ready", false, "Filter nibs available to start (not blocked, excludes in-progress/completed/scrapped/draft)")
	listCmd.Flags().BoolVarP(&listQuiet, "quiet", "q", false, "Only output IDs (one per line)")
	listCmd.Flags().StringVar(&listSort, "sort", "", "Sort by: created, updated, status, priority, status-priority, id (default: order key)")
	listCmd.Flags().BoolVar(&listFull, "full", false, "Include nib body in JSON output")
	rootCmd.AddCommand(listCmd)
}
