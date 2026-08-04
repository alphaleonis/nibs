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

Id-valued filters (--parent, --mentions, --mentioned-by):
  An id naming no nib is refused with a "not found" error (exit 3) rather than
  listing zero rows, so a mistyped or stale id — --parent "$ID" with $ID unset
  or wrong — stays distinguishable from a nib that genuinely has no children.
  An empty value is rejected outright (use --no-parent to select the parentless
  nibs).

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
		// the -s/--no-status/--all/--open precedence. --ready narrows the status
		// filter further (below), so suppress the open default when it is set —
		// the group expansion of any explicit -s/--no-status still applies, and
		// --ready narrows whatever this leaves behind.
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

		// -S "" is deliberately NOT rejected: an empty search string has a real
		// meaning — "no keyword filter" — so `nibs list -S "$q"` with an empty q
		// is a reasonable thing to write. The web reads it the same way
		// (Toolbar.svelte sends `search: value || undefined`), so rejecting it
		// here would make the CLI stricter than the UI for no gain.
		if listSearch != "" {
			filter.Search = &listSearch
		}
		// The id-valued string filters reject an explicit empty value instead of
		// ignoring it. Each is applied by a `!= ""` test, so an empty value would
		// otherwise be dropped without a word — and the usual way one arrives is
		// an unset shell variable (`--parent "$ID"`), where silently returning
		// every nib is a lie about the result rather than a wider answer.
		//
		// Unlike -S above, an empty id has no benign reading: there is no nib
		// whose id is "". That is the line between the two groups, not whether a
		// sibling flag exists.
		//
		// --parent is not given the "move to root" meaning it carries on
		// `nibs mv` and `nibs set` (both write an empty parent, detaching the
		// nib), because --no-parent already selects the parentless nibs here.
		if cmd.Flags().Changed("parent") && listParentID == "" {
			return reportErr(listJSON, output.ErrValidation,
				fmt.Errorf(`--parent was given an empty value; use --no-parent to select nibs that have no parent`))
		}
		if cmd.Flags().Changed("mentions") && listMentions == "" {
			return reportErr(listJSON, output.ErrValidation,
				fmt.Errorf(`--mentions was given an empty value; it takes a nib id`))
		}
		if cmd.Flags().Changed("mentioned-by") && listMentionedBy == "" {
			return reportErr(listJSON, output.ErrValidation,
				fmt.Errorf(`--mentioned-by was given an empty value; it takes a nib id`))
		}
		if listParentID != "" {
			filter.ParentID = &listParentID
		}

		// --has-parent/--no-parent and --has-blocking/--no-blocking are each
		// two spellings of one tri-state filter field, folded onto that field
		// by resolvePresenceFlag. Whether the field is set at all keys on
		// whether a spelling was given; what gets written is that spelling's
		// value, negated for the --no- form. So --has-parent=false and
		// --no-parent both write HasParent=&false, while --has-parent=true and
		// --no-parent=false both write &true. Giving both spellings of a pair
		// is rejected there rather than resolved here.
		if filter.HasParent, err = resolvePresenceFlag(cmd, "has-parent", "no-parent"); err != nil {
			return reportErr(listJSON, output.ErrValidation, err)
		}
		if filter.HasBlocking, err = resolvePresenceFlag(cmd, "has-blocking", "no-blocking"); err != nil {
			return reportErr(listJSON, output.ErrValidation, err)
		}

		// --parent <id> asks for a specific parent, so it cannot be met at the
		// same time as a has-parent that resolved to false: no nib has parent
		// <id> and no parent. ApplyFilter intersects the two and hands back the
		// empty set, so without this the contradiction reads as "no matches".
		//
		// The guard keys on the resolved value, which is why it catches
		// --no-parent and --has-parent=false alike, while the message names the
		// flag the caller actually gave so it is something they can act on.
		// --has-parent is reported with its resolved =false, because bare
		// --has-parent is not rejected: only a has-parent of false contradicts
		// --parent <id>. --parent <id> --has-parent is redundant instead, and
		// is left to return what --parent <id> returns.
		if filter.ParentID != nil && filter.HasParent != nil && !*filter.HasParent {
			presence := "--has-parent=false"
			if cmd.Flags().Changed("no-parent") {
				presence = "--no-parent"
			}
			return reportErr(listJSON, output.ErrValidation,
				fmt.Errorf("--parent and %s are mutually exclusive (no nib both has parent %q and has no parent)", presence, listParentID))
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
		//
		// --is-blocked is an unpaired boolean predicate: both of its values
		// carry a filter, so what matters is whether it was given at all, not
		// what it was set to. Testing the value instead would make
		// --is-blocked=false a silent no-op (filter.IsBlocked left nil, which
		// the filter layer reads as "no blocked-filter") and would let
		// --ready --is-blocked=false through the mutex unchallenged.
		//
		// Unlike the paired presence flags above, --is-blocked has no sibling
		// spelling, so there is nothing to fold and no pair to reject — only
		// the mutex against --ready.
		isBlockedSet := cmd.Flags().Changed("is-blocked")
		if listReady && isBlockedSet {
			return reportErr(listJSON, output.ErrValidation,
				fmt.Errorf("--ready and --is-blocked are mutually exclusive"))
		}
		if isBlockedSet {
			filter.IsBlocked = &listIsBlocked
		}
		// --ready: nibs available to start — no active blockers, and a startable
		// status.
		//
		// The status half is shared: this filter and the projected `ready` field
		// both read config.Startable, so they narrow by status from one
		// definition rather than two — TestReadyProjectionAndFilterAgree holds
		// them to it. The blocker half reaches the same answer through two
		// implementations of one rule: the field resolves each `blocked_by`
		// entry through Reader.Get, this filter through Core.IsBlocked →
		// findActiveBlockersInMap → normalizeIDInMap, and both take the exact id
		// first and then the configured prefix prepended. So a nib whose
		// `blocked_by` names its blocker by short id is withheld from both, and
		// the same test drives a blocker under both spellings to keep the two
		// copies of the rule together.
		//
		// An empty startable set is rejected below rather than filtered on: an
		// empty include list makes filterByField a no-op
		// (internal/graph/filters.go), so the bare-flag branch would fail open
		// and hand back every unblocked nib of any status — completed and
		// statusless ones included — as ready work. The explicit -s branch would
		// meanwhile correctly yield nothing, so the error also keeps the two
		// branches from disagreeing at exactly that boundary. Erroring on a
		// filter that can admit nothing is what resolveStatusFilter already does
		// (cmd/statusfilter.go).
		//
		// Which status field carries the narrowing depends on whether an
		// explicit -s already populated one, because neither field alone can
		// express it in both cases:
		//   - No explicit -s (the common case, --all included): the startable
		//     names become the whole base. An exclusion cannot stand in here,
		//     because it can only name declared statuses — a nib whose front
		//     matter omits `status:` carries "", which no exclusion mentions,
		//     so only an include list shuts it out.
		//   - An explicit -s already set the base: subtract every declared
		//     non-startable status from it. Overwriting the base instead would
		//     drop the -s without a word, and unioning the startable names into
		//     it would widen `-s draft --ready` to every unblocked draft.
		// Either way — given the guard above — every nib --ready returns carries
		// a startable status.
		//
		// --ready suppresses the open default by setting All above, so
		// resolveStatusFilter adds no closed-status exclusion of its own on
		// this path (cmd/statusfilter.go) and nothing is hidden silently.
		if listReady {
			startable := app.Config().StartableStatusNames()
			if len(startable) == 0 {
				return reportErr(listJSON, output.ErrValidation,
					fmt.Errorf("no status declares startable, so --ready cannot return anything"))
			}
			isBlocked := false
			filter.IsBlocked = &isBlocked
			if len(filter.Status) == 0 {
				filter.Status = startable
			} else {
				filter.ExcludeStatus = appendMissingStatuses(filter.ExcludeStatus, nonStartableStatusNames(app.Config()))
			}
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
			// An id-valued filter flag naming no nib is reported as NOT_FOUND
			// rather than as an empty listing, so `--parent "$ID"` with a stale
			// or mistyped id is distinguishable from a parent that has no
			// children. See filterTargetErrCode for the two classes.
			if code, ok := filterTargetErrCode(err); ok {
				return reportErr(listJSON, code, err)
			}
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
				// Same classification as the displayed query above: the widened
				// re-run carries the identical id-valued filters, so a target
				// deleted between the two runs must not be reported as a
				// validation failure.
				if code, ok := filterTargetErrCode(err); ok {
					return reportErr(listJSON, code, err)
				}
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

// resolvePresenceFlag folds a pair of opposite boolean flag spellings onto the
// one tri-state filter field they share. hasName is the positive spelling and
// noName its negation, so --<noName>=v contributes !v to the field and bare
// --<noName> contributes false.
//
// Whether the field is set at all keys on cmd.Flags().Changed, never on the
// flag values: a nil return means neither spelling was given, which the filter
// layer reads as "do not filter on this". Keying on the values instead would
// collapse "not given" and "given as false" into the same nil, making
// --<hasName>=false a silent no-op that returns the unfiltered set.
//
// Giving both spellings is rejected uniformly, including when their values
// agree. Agreeing values are unambiguous — --<hasName>=false and --<noName>
// select the identical set — but one filter concept spelled twice in a single
// invocation is redundant and near-certainly a mistake, so the command rejects
// the pair rather than special-casing the agreeing case.
//
// Values are read back through cmd.Flags().GetBool rather than passed in
// alongside the names, so a name and the value it resolves to cannot drift
// apart. Both lookups run before the Changed checks so an unregistered or
// non-bool name is an error even on the paths that would not have used its
// value — cmd.Flags().Changed reports false for a name it does not know, so
// gating the lookups on it would let a misspelled name resolve to a silent
// "not given" and drop the filter.
func resolvePresenceFlag(cmd *cobra.Command, hasName, noName string) (*bool, error) {
	hasValue, err := cmd.Flags().GetBool(hasName)
	if err != nil {
		return nil, err
	}
	noValue, err := cmd.Flags().GetBool(noName)
	if err != nil {
		return nil, err
	}

	hasSet := cmd.Flags().Changed(hasName)
	noSet := cmd.Flags().Changed(noName)
	switch {
	case hasSet && noSet:
		return nil, fmt.Errorf("--%s and --%s are mutually exclusive (they set the same filter)", hasName, noName)
	case hasSet:
		return &hasValue, nil
	case noSet:
		negated := !noValue
		return &negated, nil
	default:
		return nil, nil
	}
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
	listCmd.Flags().StringVar(&listParentID, "parent", "", "Filter by parent ID (an id naming no nib is an error, not an empty listing)")
	listCmd.Flags().BoolVar(&listHasBlocking, "has-blocking", false, "Filter nibs that are blocking others")
	listCmd.Flags().BoolVar(&listNoBlocking, "no-blocking", false, "Filter nibs that aren't blocking others")
	listCmd.Flags().BoolVar(&listIsBlocked, "is-blocked", false, "Filter nibs that are blocked by others")
	listCmd.Flags().StringVar(&listMentions, "mentions", "", "Filter nibs whose bodies mention this ID (short or full; an id naming no nib is an error, not an empty listing)")
	listCmd.Flags().StringVar(&listMentionedBy, "mentioned-by", "", "Filter nibs mentioned in the given ID's body (short or full; an id naming no nib is an error, not an empty listing)")
	listCmd.Flags().BoolVar(&listReady, "ready", false, readyFlagUsage(config.Default()))
	listCmd.Flags().BoolVar(&listAll, "all", false, "Include every status (disable the open-by-default filter)")
	listCmd.Flags().BoolVar(&listOpen, "open", false, "Show only open nibs — shorthand for -s open; slightly narrower than the open-by-default rule, which excludes the closed statuses and so keeps a nib with no status")
	listCmd.Flags().BoolVarP(&listQuiet, "quiet", "q", false, "Only output IDs, one per line (honors the open default; add --all to include closed nibs)")
	listCmd.Flags().StringVar(&listSort, "sort", "", "Sort by: created, updated, status, priority, status-priority, id (default: order key)")
	listCmd.Flags().StringVar(&listView, "view", "", "View tier: id, ref, card, or full (default: ref)")
	listCmd.Flags().StringVarP(&listFields, "fields", "f", "", "Field selection (additive over --view), e.g. \"status,priority\" or \"id,blocked-by(id,status)\"")
	listCmd.Flags().BoolVar(&listNoHeader, "no-header", false, "Drop the \"# <n> nibs\" header from TSV output")
	listCmd.Flags().BoolVarP(&listCount, "count", "c", false, "Output the count of matching nibs as a bare integer (honors the open default; use --all for the total across every status)")
	listCmd.Flags().IntVar(&listLimit, "limit", 0, "Project only the first N rows (0 = unlimited); sets truncated:true when it drops rows")
	rootCmd.AddCommand(listCmd)
}
