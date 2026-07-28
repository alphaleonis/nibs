package cmd

import (
	"context"
	"fmt"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/projection"
	"github.com/spf13/cobra"
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
	listAll         bool
	listOpen        bool
	listQuiet       bool
	listSort        string
	listView        string
	listFields      string
	listNoHeader    bool
	listCount       bool
	listLimit       int
)

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List nibs (projected, TSV by default)",
	Long: `List nibs matching the filter flags, projected through the field-set engine.

The filtered set is projected and rendered as tab-separated rows under a
"# <n> nibs" comment header (drop it with --no-header). Every output form
shares one field-selection model with 'nibs get'.

Status filtering (open by default):
  With no status flag, only open nibs are listed (the closed statuses are
  hidden). -s/--status and --no-status accept the status groups open and
  closed anywhere a concrete status is accepted. Any explicit -s overrides
  the open default (so -s closed shows closed nibs). --open is shorthand for
  -s open; --all disables the open default entirely.

  --view id|ref|card|full   Select a coarse field set (leanest to fullest).
                            Defaults to 'ref' when neither --view nor -f is
                            given.
  -f, --fields <spec>       Select exact fields, additive over --view. Scalars,
                            computed fields (children, progress, ready), and one
                            level of nested relation projection are supported,
                            e.g. -f "id,blocked-by(id,status)". Given alone
                            (no --view), the projection is exactly those fields.

Output modes:
  (default)                 TSV rows + the "# <n> nibs" header.
  --no-header               TSV rows only (no header line).
  --json                    The {"nibs":[…],"count":N,"truncated":<bool>}
                            envelope — the same shape rel and the recipe views
                            emit. Carries "hidden_closed":N when the open default
                            suppressed that many closed nibs.
  -q, --quiet               Ids only, one per line (the 'id' view, unwrapped).
                            Honors the open default (closed statuses hidden);
                            add --all to include them. Never annotated.
  -c, --count               The true count of the filtered set as a bare
                            integer (pre-limit; ignores --view/-f/--json).
                            Honors the open default, so it counts open nibs only;
                            use --all for the total across every status.
  --limit N                 Project only the first N rows and set
                            "truncated":true in the envelope. N<=0 is unlimited.

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
  body:auth      Search only in body field

  Single-token queries that look like a nib ID or ID fragment (e.g. 5a8k,
  nibs-5a) also match directly by ID: a substring of the short ID (min 2
  characters), a prefix of the full ID (starting with the configured
  prefix), or an exact full ID, case-insensitive. ID matches are
  interleaved with full-text hits by the list's sort order.`,
	Args: codedNoArgs(&listJSON), // all filtering is via flags; takes no positional args
	RunE: func(cmd *cobra.Command, args []string) error {
		app := getApp(cmd)

		// Resolve the status filter through the shared helper: expand status
		// groups (open/closed), apply the open-by-default rule, and honor
		// the -s/--no-status/--all/--open precedence. --ready is a stricter,
		// self-contained status filter (below), so suppress the open default when
		// it is set — the group expansion of any explicit -s/--no-status still
		// applies.
		includeStatus, excludeStatus, openDefaultApplied, err := resolveStatusFilter(app.Config(), statusFilterInput{
			Status:   listStatus,
			NoStatus: listNoStatus,
			All:      listAll || listReady,
			Open:     listOpen,
		})
		if err != nil {
			return reportErr(listJSON, output.ErrValidation, err)
		}

		// Build the GraphQL filter from the CLI flags. Filtering and sorting are
		// resolved here; the output layer projects the results separately through
		// the field-set engine.
		filter := &model.NibFilter{
			Status:          includeStatus,
			ExcludeStatus:   excludeStatus,
			Type:            listType,
			ExcludeType:     listNoType,
			Priority:        listPriority,
			ExcludePriority: listNoPriority,
			Estimate:        listEstimate,
			ExcludeEstimate: listNoEstimate,
			Tags:            listTag,
			ExcludeTags:     listNoTag,
		}

		if listSearch != "" {
			filter.Search = &listSearch
		}
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
		// filter layer normalizes via NibReader.NormalizeID in ApplyFilter
		// (internal/graph/filters.go:resolveFilterID). Do not normalize at
		// the CLI layer.
		if listMentions != "" {
			filter.MentionsID = &listMentions
		}
		if listMentionedBy != "" {
			filter.MentionedByID = &listMentionedBy
		}

		// --ready and --is-blocked are mutually exclusive.
		if listReady && listIsBlocked {
			return reportErr(listJSON, output.ErrValidation,
				fmt.Errorf("--ready and --is-blocked are mutually exclusive"))
		}
		if listIsBlocked {
			filter.IsBlocked = &listIsBlocked
		}
		// --ready: nibs available to start — not blocked, and carrying none of
		// the five statuses in the literal below. In-progress work is already
		// underway, draft needs refinement, and deferred/completed/scrapped are
		// off the board.
		//
		// Those five are a literal, not the derived closed set, and --ready
		// suppresses the open default by setting All above — so
		// resolveStatusFilter does not apply ClosedStatusNames on this path
		// (cmd/statusfilter.go). A status added to DefaultStatuses lands in
		// neither, so nothing excludes it here: an unblocked nib carrying a
		// newly added *closed* status comes back from --ready as ready work.
		// Adding one therefore means editing this literal and the three
		// surfaces that mirror it in the same order — the --ready flag usage
		// below, cmd/prompt.tmpl and cmd/prompt-full.tmpl. Deriving the
		// exclusion from the Closed flags instead is nibs-xfh5.
		if listReady {
			isBlocked := false
			filter.IsBlocked = &isBlocked
			filter.ExcludeStatus = append(filter.ExcludeStatus, "in-progress", "completed", "scrapped", "draft", "deferred")
		}

		// Compile the projection selection: --view first, then -f merged
		// additively on top. A bad view/field/nesting is a VALIDATION error
		// naming the menu, surfaced before any nib work — so even the
		// count/quiet shortcuts reject a bad -f rather than silently ignoring
		// it.
		sel, err := projection.Compile(listView, listFields)
		if err != nil {
			return reportErr(listJSON, output.ErrValidation, err)
		}
		// An empty selection (neither --view nor -f) defaults to the ref tier.
		// Applying it as the empty-selection fallback — rather than a literal
		// flag default — keeps `-f id,title` meaning exactly {id,title} instead
		// of ref∪{id,title}. ref is a compile-time-valid view, so this never
		// errors.
		if sel.IsEmpty() {
			sel, _ = projection.ViewFields(string(projection.ViewRef))
		}

		// Execute the query (filter + sort).
		nibSort := buildNibSort(listSort)
		resolver := app.newResolver()
		nibs, err := resolver.Query().Nibs(context.Background(), filter, nibSort)
		if err != nil {
			return fmt.Errorf("querying nibs: %w", err)
		}

		// -c/--count: the true count of the filtered set (pre-limit) as a bare
		// integer. Independent of --json and the projection selection.
		if listCount {
			fmt.Println(projection.Count(nibs))
			return nil
		}

		// -q/--quiet: ids only, one per line (equivalent to the id view,
		// unwrapped). Independent of --limit and the projection selection.
		if listQuiet {
			for _, b := range nibs {
				fmt.Println(b.ID)
			}
			return nil
		}

		// hidden_closed: when the open default silently dropped closed
		// rows, disclose how many matched every OTHER filter so the caller can see
		// the set is partial. Only meaningful when the open default is active (an
		// explicit -s/--open/--all/--ready never hides silently). Computed
		// pre-limit (like -c): re-run the same query with the closed-status exclusion
		// removed and subtract the displayed count. Skipped after the -c/-q early
		// returns, so the terse outputs stay bare.
		hiddenClosed := 0
		if openDefaultApplied {
			hiddenClosed, err = countHiddenClosed(context.Background(), app.Config(), resolver, filter,
				statusFilterInput{Status: listStatus, NoStatus: listNoStatus}, nibSort, len(nibs))
			if err != nil {
				return reportErr(listJSON, output.ErrValidation, err)
			}
		}

		// Project the filtered nibs through the selection, applying --limit.
		projResolver := resolver.ProjectionResolver(context.Background())
		pl, err := projection.ProjectList(nibs, sel, projResolver, listLimit)
		if err != nil {
			return reportErr(listJSON, output.ErrValidation, err)
		}
		pl.SetHiddenClosed(hiddenClosed)

		// --json: the {nibs,count,truncated,hidden_closed} envelope (byte-identical
		// to what rel and the recipe views emit).
		if listJSON {
			return output.JSONRaw(pl)
		}

		// Default: TSV rows under a "# <n> nibs" header (--no-header drops it),
		// annotated with the hidden-closed count when the open default suppressed
		// rows.
		fmt.Print(output.FormatListTSV(pl.Rows(), !listNoHeader, hiddenClosed, closedStatusLabel(app.Config())))
		return nil
	},
}

// countHiddenClosed returns how many nibs the open-by-default closed-status
// exclusion removed from the displayed set. It re-runs the query with that
// exclusion dropped — every other filter identical, only the status resolution
// widened to --all semantics (keeping any --no-status) — and subtracts the
// displayed count. Both counts are pre-limit, so the result is the size of the
// full matching closed set independent of --limit. Only call this
// when the open default was applied; otherwise nothing was hidden.
func countHiddenClosed(ctx context.Context, cfg *config.Config, resolver *graph.Resolver, displayed *model.NibFilter, status statusFilterInput, sort *model.NibSort, displayedCount int) (int, error) {
	widenedInclude, widenedExclude, _, err := resolveStatusFilter(cfg, statusFilterInput{
		Status:   status.Status,
		NoStatus: status.NoStatus,
		All:      true, // drop the open-default closed-status exclusion; keep --no-status
	})
	if err != nil {
		return 0, err
	}
	// Copy the displayed filter and override only its status fields so every
	// other constraint (type, priority, tags, parent, search, mentions, …) is
	// identical — the difference in matches is exactly the hidden closed set.
	widened := *displayed
	widened.Status = widenedInclude
	widened.ExcludeStatus = widenedExclude
	all, err := resolver.Query().Nibs(ctx, &widened, sort)
	if err != nil {
		return 0, fmt.Errorf("querying nibs for hidden-count: %w", err)
	}
	hidden := len(all) - displayedCount
	if hidden < 0 {
		hidden = 0
	}
	return hidden, nil
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

func init() {
	listCmd.Flags().BoolVar(&listJSON, "json", false, "Emit the {nibs,count,truncated} JSON envelope")
	listCmd.Flags().StringVarP(&listSearch, "search", "S", "", "Full-text search in title and body (nib IDs and ID fragments match directly)")
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
	listCmd.Flags().BoolVar(&listReady, "ready", false, "Filter nibs available to start (not blocked, excludes in-progress/completed/scrapped/draft/deferred)")
	listCmd.Flags().BoolVar(&listAll, "all", false, "Include every status (disable the open-by-default filter)")
	listCmd.Flags().BoolVar(&listOpen, "open", false, "Show only open nibs — shorthand for -s open (the default when no status filter is given)")
	listCmd.Flags().BoolVarP(&listQuiet, "quiet", "q", false, "Only output IDs, one per line (honors the open default; add --all to include closed nibs)")
	listCmd.Flags().StringVar(&listSort, "sort", "", "Sort by: created, updated, status, priority, status-priority, id (default: order key)")
	listCmd.Flags().StringVar(&listView, "view", "", "View tier: id, ref, card, or full (default: ref)")
	listCmd.Flags().StringVarP(&listFields, "fields", "f", "", "Field selection (additive over --view), e.g. \"status,priority\" or \"id,blocked-by(id,status)\"")
	listCmd.Flags().BoolVar(&listNoHeader, "no-header", false, "Drop the \"# <n> nibs\" header from TSV output")
	listCmd.Flags().BoolVarP(&listCount, "count", "c", false, "Output the count of matching nibs as a bare integer (honors the open default; use --all for the total across every status)")
	listCmd.Flags().IntVar(&listLimit, "limit", 0, "Project only the first N rows (0 = unlimited); sets truncated:true when it drops rows")
	rootCmd.AddCommand(listCmd)
}
