package cmd

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/projection"
	"github.com/alphaleonis/nibs/internal/toposort"
	"github.com/spf13/cobra"
)

// Package-level flag vars for the rel command.
var (
	relKinds      []string
	relDepth      string // "" = default; "N" | "all"
	relOrder      string // "" | "topo"
	relFlat       bool   // deprecated no-op: the envelope is always a flat list
	relJSON       bool
	relView       string
	relFields     string
	relNoHeader   bool
	relLimit      int
	relStatus     []string
	relNoStatus   []string
	relType       []string
	relNoType     []string
	relPriority   []string
	relNoPriority []string
	relTag        []string
	relEstimate   []string
	relNoEstimate []string
	relActive     bool
)

// relKind is the closed set of relationship names accepted by --rel.
type relKind string

const (
	relMentionsOut           relKind = "mentions-out"
	relMentionsIn            relKind = "mentions-in"
	relParent                relKind = "parent"
	relChildren              relKind = "children"
	relSiblings              relKind = "siblings"
	relBlocking              relKind = "blocking"
	relBlockedBy             relKind = "blocked-by"
	relAncestors             relKind = "ancestors"
	relDescendants           relKind = "descendants"
	relBlockersTransitive    relKind = "blockers-transitive"
	relBlocksTransitive      relKind = "blocks-transitive"
	relMentionsOutTransitive relKind = "mentions-out-transitive"
	relMentionsInTransitive  relKind = "mentions-in-transitive"
	relNeighbours            relKind = "neighbours"
	relNeighboursActive      relKind = "neighbours-active"
)

// directRels lists the 7 atomic direct relationships in the canonical
// order used by --rel neighbours expansion.
var directRels = []relKind{
	relMentionsOut,
	relMentionsIn,
	relParent,
	relChildren,
	relSiblings,
	relBlocking,
	relBlockedBy,
}

// Sentinel errors classified by the RunE handler. Use errors.Is to match.
var (
	errRelNotFound           = errors.New("nib not found")
	errRelInvalidRel         = errors.New("invalid rel")
	errRelFilterInapplicable = errors.New("filter not applicable to rel")
	errRelDepthInapplicable  = errors.New("--depth not applicable to rel")
	errRelOrderInapplicable  = errors.New("--order not applicable to rel")
	errRelInvalidDepth       = errors.New("invalid --depth value")
	errRelCycle              = errors.New("dependency cycle")
)

// relSpec describes per-rel capabilities used for flag validation and
// expansion. Meta rels have non-nil ExpandsTo and delegate to their
// atomic constituents.
type relSpec struct {
	IsSingular      bool      // parent / ancestors chain — filters are not applicable
	AllowsDepth     bool      // transitive rels only
	AllowsOrder     bool      // topo-sortable rels
	ExpandsTo       []relKind // non-nil for meta rels (neighbours, neighbours-active)
	NeighbourActive bool      // neighbours-active adds --active on top of neighbours expansion
}

// relTable describes the capabilities of every rel.
var relTable = map[relKind]relSpec{
	relMentionsOut:           {},
	relMentionsIn:            {},
	relParent:                {IsSingular: true},
	relChildren:              {AllowsOrder: true},
	relSiblings:              {},
	relBlocking:              {AllowsOrder: true},
	relBlockedBy:             {AllowsOrder: true},
	relAncestors:             {IsSingular: true, AllowsDepth: true},
	relDescendants:           {AllowsDepth: true, AllowsOrder: true},
	relBlockersTransitive:    {AllowsDepth: true, AllowsOrder: true},
	relBlocksTransitive:      {AllowsDepth: true, AllowsOrder: true},
	relMentionsOutTransitive: {AllowsDepth: true},
	relMentionsInTransitive:  {AllowsDepth: true},
	relNeighbours:            {ExpandsTo: directRels},
	relNeighboursActive:      {ExpandsTo: directRels, NeighbourActive: true},
}

// parseRels parses the repeated --rel values, splitting comma-separated
// entries inside a single value. Returns the validated list (meta rels are
// left intact; expandRels resolves them to their atomic constituents).
//
// When --rel is empty, default to `neighbours`.
func parseRels(raw []string) ([]relKind, error) {
	if len(raw) == 0 {
		raw = []string{string(relNeighbours)}
	}
	var out []relKind
	seen := map[relKind]bool{}
	for _, entry := range raw {
		for _, part := range strings.Split(entry, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			r := relKind(part)
			if _, ok := relTable[r]; !ok {
				return nil, fmt.Errorf("%w: %q — accepted: %s",
					errRelInvalidRel, part, strings.Join(allRelNames(), ", "))
			}
			if seen[r] {
				continue
			}
			seen[r] = true
			out = append(out, r)
		}
	}
	return out, nil
}

// allRelNames returns the sorted list of accepted rel names for error messages.
func allRelNames() []string {
	names := make([]string, 0, len(relTable))
	for r := range relTable {
		names = append(names, string(r))
	}
	sort.Strings(names)
	return names
}

// expandRels turns the caller-supplied rel list into the final list of
// atomic rels to fetch. Meta rels (neighbours/neighbours-active) are
// replaced by their direct constituents. Order is preserved and the result
// is deduped across rels.
// Returns:
//   - fetched: the ordered, deduped list of atomic rels to traverse
//   - applyActive: true when neighbours-active is present — the caller
//     must OR --active into the filter for the expanded rels.
func expandRels(in []relKind) (fetched []relKind, applyActive bool) {
	seen := map[relKind]bool{}
	for _, r := range in {
		spec := relTable[r]
		if spec.NeighbourActive {
			applyActive = true
		}
		if len(spec.ExpandsTo) > 0 {
			for _, sub := range spec.ExpandsTo {
				if seen[sub] {
					continue
				}
				seen[sub] = true
				fetched = append(fetched, sub)
			}
			continue
		}
		if seen[r] {
			continue
		}
		seen[r] = true
		fetched = append(fetched, r)
	}
	return fetched, applyActive
}

// relFilterFlags is the single source of truth for which CLI filter flags
// are set and how they translate to a NibFilter / error message / presence
// check. When adding a new filter flag, add it to:
//   - the struct fields below
//   - readRelFilterFlags (CLI flag vars → struct)
//   - activeFilterNames (struct → error message labels)
//   - isEmpty (struct → presence check)
//   - buildNibFilter (struct → model.NibFilter)
//
// All five live in this file, adjacent to each other, so a reviewer can see
// at a glance that the list is complete.
type relFilterFlags struct {
	Status     []string
	NoStatus   []string
	Type       []string
	NoType     []string
	Priority   []string
	NoPriority []string
	Tag        []string
	Estimate   []string
	NoEstimate []string
	Active     bool
}

// readRelFilterFlags reads the package-level CLI flag vars into a struct
// the rest of the pipeline can consume. This is the ONLY place that reads
// the flag vars for filter-flag purposes.
func readRelFilterFlags() relFilterFlags {
	return relFilterFlags{
		Status:     relStatus,
		NoStatus:   relNoStatus,
		Type:       relType,
		NoType:     relNoType,
		Priority:   relPriority,
		NoPriority: relNoPriority,
		Tag:        relTag,
		Estimate:   relEstimate,
		NoEstimate: relNoEstimate,
		Active:     relActive,
	}
}

// isEmpty reports whether no filter flag was set (used to short-circuit
// buildNibFilter so the resolver can use its uncached fast path).
func (f relFilterFlags) isEmpty() bool {
	return !f.Active &&
		len(f.Status) == 0 && len(f.NoStatus) == 0 &&
		len(f.Type) == 0 && len(f.NoType) == 0 &&
		len(f.Priority) == 0 && len(f.NoPriority) == 0 &&
		len(f.Tag) == 0 && len(f.Estimate) == 0 &&
		len(f.NoEstimate) == 0
}

// activeFilterNames returns the --flag names currently set, for use in
// error messages when a filter is inapplicable to the chosen rel.
func (f relFilterFlags) activeFilterNames() []string {
	var names []string
	if f.Active {
		names = append(names, "--active")
	}
	if len(f.Status) > 0 {
		names = append(names, "--status")
	}
	if len(f.NoStatus) > 0 {
		names = append(names, "--no-status")
	}
	if len(f.Type) > 0 {
		names = append(names, "--type")
	}
	if len(f.NoType) > 0 {
		names = append(names, "--no-type")
	}
	if len(f.Priority) > 0 {
		names = append(names, "--priority")
	}
	if len(f.NoPriority) > 0 {
		names = append(names, "--no-priority")
	}
	if len(f.Tag) > 0 {
		names = append(names, "--tag")
	}
	if len(f.Estimate) > 0 {
		names = append(names, "--estimate")
	}
	if len(f.NoEstimate) > 0 {
		names = append(names, "--no-estimate")
	}
	return names
}

// buildNibFilter translates the struct into a *model.NibFilter. When no flag
// is set (isEmpty()) returns nil so the resolver short-circuits. When
// forceActive is true, excludes completed/scrapped on top of any explicit
// status flags (used by neighbours-active).
func (f relFilterFlags) buildNibFilter(forceActive bool) (*model.NibFilter, error) {
	active := f.Active || forceActive
	if active {
		for _, s := range f.Status {
			if s == "completed" || s == "scrapped" {
				return nil, fmt.Errorf("--active excludes completed and scrapped; combining with --status %s always yields empty results", s)
			}
		}
	}
	if f.isEmpty() && !forceActive {
		return nil, nil
	}
	excludeStatus := append([]string(nil), f.NoStatus...)
	if active {
		excludeStatus = append(excludeStatus, "completed", "scrapped")
	}
	return &model.NibFilter{
		Status:          f.Status,
		ExcludeStatus:   excludeStatus,
		Type:            f.Type,
		ExcludeType:     f.NoType,
		Priority:        f.Priority,
		ExcludePriority: f.NoPriority,
		Tags:            f.Tag,
		Estimate:        f.Estimate,
		ExcludeEstimate: f.NoEstimate,
	}, nil
}

// parseDepth returns the resolved depth (0 means "caller did not pass --depth").
// Special value "all" resolves to -1 (traverse until exhaustion). Any other
// input that is not a positive integer in its entirety — including trailing
// garbage like "3abc", fractions like "1.5", or negative values — is rejected
// with errRelInvalidDepth. strconv.Atoi (not fmt.Sscanf) is used because
// the %d verb stops at the first non-digit and reports success.
func parseDepth(raw string) (int, bool, error) {
	if raw == "" {
		return 1, false, nil // default, not set
	}
	if raw == "all" {
		return -1, true, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, false, fmt.Errorf("%w: %q (must be positive integer or 'all')", errRelInvalidDepth, raw)
	}
	return n, true, nil
}

// fetchRel dispatches a single atomic rel to its resolver / traversal.
// depth is meaningful only for transitive rels; direct rels ignore it.
func fetchRel(ctx context.Context, resolver *graph.Resolver, b *nib.Nib, r relKind, filter *model.NibFilter, depth int) ([]*nib.Nib, error) {
	switch r {
	case relMentionsOut:
		return resolver.Nib().Mentions(ctx, b, filter)
	case relMentionsIn:
		return resolver.Nib().MentionedBy(ctx, b, filter)
	case relParent:
		p, err := resolver.Nib().Parent(ctx, b)
		if err != nil {
			return nil, err
		}
		if p == nil {
			return []*nib.Nib{}, nil
		}
		return []*nib.Nib{p}, nil
	case relChildren:
		return resolver.Nib().Children(ctx, b, filter, nil)
	case relSiblings:
		return fetchSiblings(ctx, resolver, b, filter)
	case relBlocking:
		return resolver.Nib().Blocking(ctx, b, filter)
	case relBlockedBy:
		return resolver.Nib().BlockedBy(ctx, b, filter)
	case relAncestors:
		return bfsChainAncestors(ctx, resolver, b, depth)
	case relDescendants:
		return bfsDescendants(ctx, resolver, b, filter, depth)
	case relBlockersTransitive:
		return bfsTransitive(ctx, resolver, b, filter, depth, relBlockedBy)
	case relBlocksTransitive:
		return bfsTransitive(ctx, resolver, b, filter, depth, relBlocking)
	case relMentionsOutTransitive:
		return bfsTransitive(ctx, resolver, b, filter, depth, relMentionsOut)
	case relMentionsInTransitive:
		return bfsTransitive(ctx, resolver, b, filter, depth, relMentionsIn)
	}
	return nil, fmt.Errorf("unhandled rel %q", r)
}

// fetchSiblings returns siblings of b (same parent, self excluded). If b has
// no parent, returns the set of other root nibs (Parent == "").
func fetchSiblings(ctx context.Context, resolver *graph.Resolver, b *nib.Nib, filter *model.NibFilter) ([]*nib.Nib, error) {
	parent, err := resolver.Nib().Parent(ctx, b)
	if err != nil {
		return nil, err
	}
	var candidates []*nib.Nib
	if parent != nil {
		sibs, err := resolver.Nib().Children(ctx, parent, filter, nil)
		if err != nil {
			return nil, err
		}
		candidates = sibs
	} else {
		// Root siblings: all nibs with no parent, minus self.
		all, err := resolver.Query().Nibs(ctx, filter, nil)
		if err != nil {
			return nil, err
		}
		for _, n := range all {
			if n.Parent == "" {
				candidates = append(candidates, n)
			}
		}
	}
	out := make([]*nib.Nib, 0, len(candidates))
	for _, n := range candidates {
		if n.ID == b.ID {
			continue
		}
		out = append(out, n)
	}
	return out, nil
}

// bfsChainAncestors walks parent links up to `depth` steps and returns
// the chain in encounter order (closest ancestor first). depth < 0 means
// "until root".
func bfsChainAncestors(ctx context.Context, resolver *graph.Resolver, b *nib.Nib, depth int) ([]*nib.Nib, error) {
	var out []*nib.Nib
	seen := map[string]bool{b.ID: true}
	cur := b
	for steps := 0; depth < 0 || steps < depth; steps++ {
		p, err := resolver.Nib().Parent(ctx, cur)
		if err != nil {
			return nil, err
		}
		if p == nil {
			break
		}
		if seen[p.ID] {
			break // cycle-safe: stop if loop
		}
		seen[p.ID] = true
		out = append(out, p)
		cur = p
	}
	return out, nil
}

// bfsDescendants performs BFS over children edges from b up to depth.
// The filter applies to each child-fetch call.
func bfsDescendants(ctx context.Context, resolver *graph.Resolver, b *nib.Nib, filter *model.NibFilter, depth int) ([]*nib.Nib, error) {
	return bfsTraverse(ctx, resolver, b, filter, depth, relChildren)
}

// bfsTransitive performs generic BFS traversal from b following `edge`.
func bfsTransitive(ctx context.Context, resolver *graph.Resolver, b *nib.Nib, filter *model.NibFilter, depth int, edge relKind) ([]*nib.Nib, error) {
	return bfsTraverse(ctx, resolver, b, filter, depth, edge)
}

// bfsTraverse is the shared BFS engine. It walks `edge` from `b` up to
// `depth` levels (depth < 0 = unlimited) with a visited set for cycle
// safety. Filter is applied at each direct-fetch call.
func bfsTraverse(ctx context.Context, resolver *graph.Resolver, start *nib.Nib, filter *model.NibFilter, depth int, edge relKind) ([]*nib.Nib, error) {
	var out []*nib.Nib
	seen := map[string]bool{start.ID: true}
	frontier := []*nib.Nib{start}
	level := 0
	for len(frontier) > 0 {
		if depth >= 0 && level >= depth {
			break
		}
		var next []*nib.Nib
		for _, cur := range frontier {
			children, err := fetchRel(ctx, resolver, cur, edge, filter, 0)
			if err != nil {
				return nil, err
			}
			for _, c := range children {
				if seen[c.ID] {
					continue
				}
				seen[c.ID] = true
				out = append(out, c)
				next = append(next, c)
			}
		}
		frontier = next
		level++
	}
	return out, nil
}

// topoSortNibs reorders `candidates` using `blocked_by` edges among the set.
// Edges pointing outside the candidate set are dropped. Self-edges are
// ignored. Returns an error when a cycle is present (naming the members).
//
// Only `blocked_by` declarations contribute edges. `#<id>` body mentions are
// informational and never affect topo order.
func topoSortNibs(candidates []*nib.Nib) ([]*nib.Nib, error) {
	byID := make(map[string]*nib.Nib, len(candidates))
	ids := make([]string, 0, len(candidates))
	for _, c := range candidates {
		byID[c.ID] = c
		ids = append(ids, c.ID)
	}
	var edges [][2]string
	seenEdge := map[[2]string]struct{}{}
	for _, c := range candidates {
		for _, blockerID := range c.BlockedBy {
			if blockerID == c.ID {
				continue // defensive: self-block is meaningless
			}
			if _, in := byID[blockerID]; !in {
				continue // blocker outside candidate set — drop edge
			}
			key := [2]string{blockerID, c.ID}
			if _, dup := seenEdge[key]; dup {
				continue
			}
			seenEdge[key] = struct{}{}
			edges = append(edges, key)
		}
	}
	ordered, cycles := toposort.Sort(ids, edges)
	if len(cycles) > 0 {
		names := []string{}
		for _, cyc := range cycles {
			c := append([]string(nil), cyc...)
			sort.Strings(c)
			names = append(names, "["+strings.Join(c, ", ")+"]")
		}
		return nil, fmt.Errorf("%w detected: %s", errRelCycle, strings.Join(names, ", "))
	}
	out := make([]*nib.Nib, 0, len(ordered))
	for _, id := range ordered {
		out = append(out, byID[id])
	}
	return out, nil
}

var relCmd = &cobra.Command{
	Use:     "rel <id>",
	Aliases: []string{"links"},
	Short:   "Query relationships (mentions, parent/children, blocking, etc.) for a nib",
	Long: `Traverse any relationship direction from a nib and project the related nibs
through the shared field-set engine — the related set is rendered as the same
{nibs,count,truncated} envelope 'nibs list' emits.

--rel selects one or more relations (repeatable; comma-separated values OK):
  mentions-out, mentions-in, parent, children, siblings, blocking, blocked-by
  ancestors, descendants, blockers-transitive, blocks-transitive,
  mentions-out-transitive, mentions-in-transitive
  neighbours (= all 7 direct rels), neighbours-active (= neighbours with --active)

When several rels are requested the related nibs are unioned into one deduped
list (first-encountered order across rels), then projected.

--depth N|all applies only to transitive rels (default 1).
--order topo applies only to children / descendants / blocking-family rels.

Projection (identical to 'nibs list'):
  --view id|ref|card|full   Select a coarse field set for the related nibs.
                            Defaults to 'ref' when neither --view nor -f is given.
  -f, --fields <spec>       Select exact fields, additive over --view, applied to
                            the related nibs. One level of nested relation
                            projection is supported, e.g. -f "id,blocked-by(id,status)".

Output modes:
  (default)                 TSV rows + the "# <n> nibs" header.
  --no-header               TSV rows only (no header line).
  --json                    The {"nibs":[…],"count":N,"truncated":<bool>} envelope
                            — byte-identical to 'nibs list --json'.
  --limit N                 Project only the first N related nibs and set
                            "truncated":true in the envelope. N<=0 is unlimited.

Filters (--status, --type, --priority, --estimate, --tag, their --no-... pairs,
--active) apply only to rels where they make sense — passing an inapplicable
filter is a validation error (parent is singular; ancestors is a chain).

For transitive rels (descendants, blockers-transitive, blocks-transitive,
mentions-*-transitive), filters apply at each traversal step: a node that
fails the filter stops traversal through it, and nodes beyond it are not
visited (downward-closed pruning). Consequences: a matching leaf whose
intermediate ancestor fails the filter is NOT included in the result.

--rel neighbours and --rel neighbours-active accept filter flags. Filters
apply to the non-singular constituents (mentions-out/in, children, siblings,
blocking, blocked-by) and are silently dropped for the singular constituent
(parent) — parent is aggregated by expansion, not requested directly, so the
filter-on-singular validation error does not fire here.`,
	Args: codedExactArgs(&relJSON, 1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Compile the projection selection up-front (mirrors 'nibs list'): a
		// bad view/field/nesting is a VALIDATION error naming the menu, surfaced
		// before any traversal work so a bad -f is rejected rather than wasted.
		sel, err := projection.Compile(relView, relFields)
		if err != nil {
			return reportErr(relJSON, output.ErrValidation, err)
		}
		// An empty selection (neither --view nor -f) defaults to the ref tier —
		// applied as the empty-selection fallback rather than a flag default so
		// `-f id,title` means exactly {id,title} instead of ref∪{id,title}.
		if sel.IsEmpty() {
			sel, _ = projection.ViewFields(string(projection.ViewRef))
		}

		rels, err := parseRels(relKinds)
		if err != nil {
			return reportErr(relJSON, output.ErrValidation, err)
		}

		fetched, forceActive := expandRels(rels)

		depthVal, depthSet, err := parseDepth(relDepth)
		if err != nil {
			return reportErr(relJSON, output.ErrValidation, err)
		}

		// Validate --depth applies to every explicit rel that was not a meta
		// expansion. Meta rels (neighbours/neighbours-active) are not allowed
		// to receive --depth either — they expand to direct rels, and direct
		// rels do not support depth.
		if depthSet {
			for _, r := range rels {
				spec := relTable[r]
				if !spec.AllowsDepth {
					return reportErr(relJSON, output.ErrValidation,
						fmt.Errorf("%w: %s", errRelDepthInapplicable, r))
				}
			}
		}

		// Validate --order applies to every explicit rel.
		if relOrder != "" {
			if relOrder != "topo" {
				return reportErr(relJSON, output.ErrValidation,
					fmt.Errorf("--order must be 'topo', got %q", relOrder))
			}
			for _, r := range rels {
				spec := relTable[r]
				// Reject when the explicit rel doesn't advertise AllowsOrder
				// (meta rels are rejected because their expanded atomic rels
				// mix order-supporting and non-supporting).
				if !spec.AllowsOrder {
					return reportErr(relJSON, output.ErrValidation,
						fmt.Errorf("%w: --order topo not supported for rel %s", errRelOrderInapplicable, r))
				}
			}
		}

		// Validate filters-on-singular. If any explicit rel is singular
		// (parent) and a filter is set, error out.
		filterFlags := readRelFilterFlags()
		if !filterFlags.isEmpty() {
			for _, r := range rels {
				spec := relTable[r]
				if spec.IsSingular {
					filters := strings.Join(filterFlags.activeFilterNames(), ", ")
					return reportErr(relJSON, output.ErrValidation,
						fmt.Errorf("%w: filter %s does not apply to rel %s (singular/chain relation)", errRelFilterInapplicable, filters, r))
				}
			}
		}

		filter, err := filterFlags.buildNibFilter(forceActive)
		if err != nil {
			return reportErr(relJSON, output.ErrValidation, err)
		}

		app := getApp(cmd)
		resolver := app.newResolver()
		// Use Cobra's cancelable context so SIGINT propagates through a
		// potentially long-running traversal (e.g. --rel descendants --depth all).
		// Guard against nil which can occur when cmd.Context() is not set (e.g.
		// tests that don't call ExecuteContext).
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}

		b, err := resolver.Query().Nib(ctx, args[0])
		if err != nil {
			return reportErr(relJSON, output.ErrNotFound,
				fmt.Errorf("%w: %s: %w", errRelNotFound, args[0], err))
		}
		if b == nil {
			return reportErr(relJSON, output.ErrNotFound,
				fmt.Errorf("%w: %s", errRelNotFound, args[0]))
		}

		// Traverse every atomic rel, unioning the related nibs into one deduped
		// list in first-encountered order. --order topo is applied per rel
		// (before the union) so a rel's internal dependency order is preserved.
		seenNib := map[string]bool{}
		var results []*nib.Nib
		for _, fetchKind := range fetched {
			// Singular rels (parent) were validated to carry no filter via
			// filterFlags.isEmpty(); pass nil so the resolver stays on its fast
			// path. Everything else uses the composite filter.
			perRelFilter := filter
			if relTable[fetchKind].IsSingular {
				perRelFilter = nil
			}

			got, ferr := fetchRel(ctx, resolver, b, fetchKind, perRelFilter, depthVal)
			if ferr != nil {
				code := output.ErrFileError
				if errors.Is(ferr, errRelCycle) {
					code = output.ErrValidation
				}
				return reportErr(relJSON, code,
					fmt.Errorf("fetching %s: %w", fetchKind, ferr))
			}
			if relOrder == "topo" && relTable[fetchKind].AllowsOrder {
				ordered, oerr := topoSortNibs(got)
				if oerr != nil {
					code := output.ErrFileError
					if errors.Is(oerr, errRelCycle) {
						code = output.ErrValidation
					}
					return reportErr(relJSON, code, oerr)
				}
				got = ordered
			}
			for _, n := range got {
				if seenNib[n.ID] {
					continue
				}
				seenNib[n.ID] = true
				results = append(results, n)
			}
		}

		// Project the related nibs through the selection into the shared
		// {nibs,count,truncated} envelope — byte-identical to 'nibs list'.
		projResolver := resolver.ProjectionResolver(context.Background())
		pl, err := projection.ProjectList(results, sel, projResolver, relLimit)
		if err != nil {
			return reportErr(relJSON, output.ErrValidation, err)
		}

		if relJSON {
			return output.JSONRaw(pl)
		}

		// Default: TSV rows under a "# <n> nibs" header (--no-header drops it).
		fmt.Print(output.FormatListTSV(pl.Rows(), !relNoHeader))
		return nil
	},
}

func init() {
	relCmd.Flags().StringArrayVar(&relKinds, "rel", nil, "Relationship to query (repeatable; comma-separated OK)")
	relCmd.Flags().StringVar(&relDepth, "depth", "", "Depth for transitive rels: N (positive integer) or 'all' (default 1)")
	relCmd.Flags().StringVar(&relOrder, "order", "", "Order the results (supports: topo)")
	relCmd.Flags().BoolVar(&relFlat, "flat", false, "Deprecated no-op: the related set is always a single deduped list")
	_ = relCmd.Flags().MarkHidden("flat")
	relCmd.Flags().BoolVar(&relJSON, "json", false, "Emit the {nibs,count,truncated} JSON envelope")
	relCmd.Flags().StringVar(&relView, "view", "", "View tier for the related nibs: id, ref, card, or full (default: ref)")
	relCmd.Flags().StringVarP(&relFields, "fields", "f", "", "Field selection for the related nibs (additive over --view), e.g. \"status,priority\" or \"id,blocked-by(id,status)\"")
	relCmd.Flags().BoolVar(&relNoHeader, "no-header", false, "Drop the \"# <n> nibs\" header from TSV output")
	relCmd.Flags().IntVar(&relLimit, "limit", 0, "Project only the first N related nibs (0 = unlimited); sets truncated:true when it drops rows")
	relCmd.Flags().StringArrayVarP(&relStatus, "status", "s", nil, "Filter by status (repeatable)")
	relCmd.Flags().StringArrayVar(&relNoStatus, "no-status", nil, "Exclude by status (repeatable)")
	relCmd.Flags().StringArrayVarP(&relType, "type", "t", nil, "Filter by type (repeatable)")
	relCmd.Flags().StringArrayVar(&relNoType, "no-type", nil, "Exclude by type (repeatable)")
	relCmd.Flags().StringArrayVarP(&relPriority, "priority", "p", nil, "Filter by priority (repeatable)")
	relCmd.Flags().StringArrayVar(&relNoPriority, "no-priority", nil, "Exclude by priority (repeatable)")
	relCmd.Flags().StringArrayVar(&relTag, "tag", nil, "Filter by tag (repeatable)")
	relCmd.Flags().StringArrayVarP(&relEstimate, "estimate", "e", nil, "Filter by estimate (repeatable)")
	relCmd.Flags().StringArrayVar(&relNoEstimate, "no-estimate", nil, "Exclude by estimate (repeatable)")
	relCmd.Flags().BoolVar(&relActive, "active", false, "Exclude completed/scrapped nibs")
	rootCmd.AddCommand(relCmd)
}
