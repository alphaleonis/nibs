package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/toposort"
	"github.com/alphaleonis/nibs/internal/ui"
	"github.com/spf13/cobra"
)

// formatLinksRow renders one link row (id, status, title) for human output.
// Kept local to cmd/links.go because list and show use divergent row formats
// (columnar and block-oriented respectively); consolidating into a shared
// ui helper would require a format enum whose cost outweighs the handful
// of lines saved.
func formatLinksRow(r *nib.Nib, cfg *config.Config) string {
	statusCfg := cfg.GetStatus(r.Status)
	statusColor := "gray"
	if statusCfg != nil {
		statusColor = statusCfg.Color
	}
	isArchive := cfg.IsArchiveStatus(r.Status)
	return fmt.Sprintf("%s  %s  %s",
		ui.ID.Render(r.ID),
		ui.RenderStatusWithColor(r.Status, statusColor, isArchive),
		r.Title)
}

// Package-level flag vars for the links command.
var (
	linksRel        []string
	linksDepth      string // "" = default; "N" | "all"
	linksOrder      string // "" | "topo"
	linksFlat       bool
	linksJSON       bool
	linksStatus     []string
	linksNoStatus   []string
	linksType       []string
	linksNoType     []string
	linksPriority   []string
	linksNoPriority []string
	linksTag        []string
	linksEstimate   []string
	linksNoEstimate []string
	linksActive     bool
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
	errLinksNotFound           = errors.New("nib not found")
	errLinksInvalidRel         = errors.New("invalid rel")
	errLinksFilterInapplicable = errors.New("filter not applicable to rel")
	errLinksDepthInapplicable  = errors.New("--depth not applicable to rel")
	errLinksOrderInapplicable  = errors.New("--order not applicable to rel")
	errLinksInvalidDepth       = errors.New("invalid --depth value")
	errLinksCycle              = errors.New("dependency cycle")
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

// LinksRelBody is the body of a single relation in the envelope.
type LinksRelBody struct {
	Nibs []*nib.Nib `json:"nibs"`
}

// LinksResult is the full envelope. Relations preserve insertion order via
// custom marshaling; `depth` is emitted only when --depth was passed.
type LinksResult struct {
	ID    string
	Depth *int
	// relKeys preserves the order the relations were added.
	relKeys []string
	// relations maps rel name to body.
	relations map[string]LinksRelBody
}

// addRel appends a rel body, preserving first-seen insertion order.
func (r *LinksResult) addRel(key string, body LinksRelBody) {
	if _, ok := r.relations[key]; ok {
		return
	}
	if r.relations == nil {
		r.relations = map[string]LinksRelBody{}
	}
	r.relations[key] = body
	r.relKeys = append(r.relKeys, key)
}

// MarshalJSON emits the envelope with a stable key order inside `relations`
// matching the order keys were added. This matters because Go's encoding/json
// sorts map keys alphabetically by default, which would break the declared
// "caller-supplied rel order" invariant.
func (r LinksResult) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	// id first
	buf.WriteString(`"id":`)
	idBytes, err := json.Marshal(r.ID)
	if err != nil {
		return nil, err
	}
	buf.Write(idBytes)
	// optional depth
	if r.Depth != nil {
		buf.WriteString(`,"depth":`)
		dBytes, err := json.Marshal(*r.Depth)
		if err != nil {
			return nil, err
		}
		buf.Write(dBytes)
	}
	// relations — ordered
	buf.WriteString(`,"relations":{`)
	for i, key := range r.relKeys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kBytes, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		buf.Write(kBytes)
		buf.WriteByte(':')
		body := r.relations[key]
		if body.Nibs == nil {
			body.Nibs = []*nib.Nib{}
		}
		bBytes, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		buf.Write(bBytes)
	}
	buf.WriteString(`}}`)
	return buf.Bytes(), nil
}

// LinksFlatResult is the envelope used under --flat: a single deduped
// list of nibs across all requested rels.
type LinksFlatResult struct {
	ID    string     `json:"id"`
	Depth *int       `json:"depth,omitempty"`
	Nibs  []*nib.Nib `json:"nibs"`
}

// parseRels parses the repeated --rel values, splitting comma-separated
// entries inside a single value. Returns the validated, expanded list
// (meta rels expanded to their direct constituents) paired with the
// original caller-supplied list (for envelope key order).
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
					errLinksInvalidRel, part, strings.Join(allRelNames(), ", "))
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
// replaced by their direct constituents. Order is preserved.
// Returns:
//   - fetched: the list of atomic rels to fetch (for dispatch)
//   - outKeys: the list of keys to emit in the envelope, in order
//     (meta rels emit their constituent keys; atomic rels emit themselves)
//   - applyActive: true when neighbours-active is present — the caller
//     must OR --active into the filter for the expanded rels.
func expandRels(in []relKind) (fetched []relKind, outKeys []relKind, applyActive bool) {
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
				outKeys = append(outKeys, sub)
			}
			continue
		}
		if seen[r] {
			continue
		}
		seen[r] = true
		fetched = append(fetched, r)
		outKeys = append(outKeys, r)
	}
	return fetched, outKeys, applyActive
}

// linksFilterFlags is the single source of truth for which CLI filter flags
// are set and how they translate to a NibFilter / error message / presence
// check. When adding a new filter flag, add it to:
//   - the struct fields below
//   - readLinksFilterFlags (CLI flag vars → struct)
//   - activeFilterNames (struct → error message labels)
//   - isEmpty (struct → presence check)
//   - buildNibFilter (struct → model.NibFilter)
//
// All five live in this file, adjacent to each other, so a reviewer can see
// at a glance that the list is complete.
type linksFilterFlags struct {
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

// readLinksFilterFlags reads the package-level CLI flag vars into a struct
// the rest of the pipeline can consume. This is the ONLY place that reads
// the flag vars for filter-flag purposes.
func readLinksFilterFlags() linksFilterFlags {
	return linksFilterFlags{
		Status:     linksStatus,
		NoStatus:   linksNoStatus,
		Type:       linksType,
		NoType:     linksNoType,
		Priority:   linksPriority,
		NoPriority: linksNoPriority,
		Tag:        linksTag,
		Estimate:   linksEstimate,
		NoEstimate: linksNoEstimate,
		Active:     linksActive,
	}
}

// isEmpty reports whether no filter flag was set (used to short-circuit
// buildNibFilter so the resolver can use its uncached fast path).
func (f linksFilterFlags) isEmpty() bool {
	return !f.Active &&
		len(f.Status) == 0 && len(f.NoStatus) == 0 &&
		len(f.Type) == 0 && len(f.NoType) == 0 &&
		len(f.Priority) == 0 && len(f.NoPriority) == 0 &&
		len(f.Tag) == 0 && len(f.Estimate) == 0 &&
		len(f.NoEstimate) == 0
}

// activeFilterNames returns the --flag names currently set, for use in
// error messages when a filter is inapplicable to the chosen rel.
func (f linksFilterFlags) activeFilterNames() []string {
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
func (f linksFilterFlags) buildNibFilter(forceActive bool) (*model.NibFilter, error) {
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
// with errLinksInvalidDepth. strconv.Atoi (not fmt.Sscanf) is used because
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
		return 0, false, fmt.Errorf("%w: %q (must be positive integer or 'all')", errLinksInvalidDepth, raw)
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

// topoSortNibs reorders `candidates` using mention edges among the set.
// Mention edges pointing outside the candidate set are dropped. Self-mentions
// are ignored. Returns an error when a cycle is present (naming the members).
func topoSortNibs(ctx context.Context, resolver *graph.Resolver, candidates []*nib.Nib) ([]*nib.Nib, error) {
	byID := make(map[string]*nib.Nib, len(candidates))
	ids := make([]string, 0, len(candidates))
	for _, c := range candidates {
		byID[c.ID] = c
		ids = append(ids, c.ID)
	}
	var edges [][2]string
	seenEdge := map[[2]string]struct{}{}
	for _, c := range candidates {
		// The nil filter here is load-bearing: edges must be computed
		// against the unfiltered mention graph so that a filtered-out
		// intermediate node doesn't silently collapse the topo graph
		// into a false edge. After resolving mentions unfiltered, the
		// byID gate below drops any target not in the already-filtered
		// candidate set — so an edge c→(filtered_out)→a is correctly
		// dropped rather than flattened into c→a. See
		// TestLinksCommand_Children_OrderTopo_SkipsFilteredSibling.
		mentions, err := resolver.Nib().Mentions(ctx, c, nil)
		if err != nil {
			return nil, fmt.Errorf("resolving mentions for %s: %w", c.ID, err)
		}
		for _, m := range mentions {
			if m.ID == c.ID {
				continue
			}
			if _, in := byID[m.ID]; !in {
				continue
			}
			key := [2]string{m.ID, c.ID}
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
		return nil, fmt.Errorf("%w detected: %s", errLinksCycle, strings.Join(names, ", "))
	}
	out := make([]*nib.Nib, 0, len(ordered))
	for _, id := range ordered {
		out = append(out, byID[id])
	}
	return out, nil
}

var linksCmd = &cobra.Command{
	Use:   "links <id>",
	Short: "Query relationships (mentions, parent/children, blocking, etc.) for a nib",
	Long: `Query any relationship direction for a nib under one stable JSON envelope.

--rel selects one or more relations (repeatable; comma-separated values OK):
  mentions-out, mentions-in, parent, children, siblings, blocking, blocked-by
  ancestors, descendants, blockers-transitive, blocks-transitive,
  mentions-out-transitive, mentions-in-transitive
  neighbours (= all 7 direct rels), neighbours-active (= neighbours with --active)

--depth N|all applies only to transitive rels (default 1).
--order topo applies only to children / descendants / blocking-family rels.
--flat collapses multi-rel output to a deduped {id, nibs: [...]} envelope.

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
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rels, err := parseRels(linksRel)
		if err != nil {
			return reportErr(linksJSON, output.ErrValidation, err)
		}

		fetched, outKeys, forceActive := expandRels(rels)

		depthVal, depthSet, err := parseDepth(linksDepth)
		if err != nil {
			return reportErr(linksJSON, output.ErrValidation, err)
		}

		// Validate --depth applies to every explicit rel that was not a meta
		// expansion. Meta rels (neighbours/neighbours-active) are not allowed
		// to receive --depth either — they expand to direct rels, and direct
		// rels do not support depth.
		if depthSet {
			for _, r := range rels {
				spec := relTable[r]
				if !spec.AllowsDepth {
					return reportErr(linksJSON, output.ErrValidation,
						fmt.Errorf("%w: %s", errLinksDepthInapplicable, r))
				}
			}
		}

		// Validate --order applies to every explicit rel.
		if linksOrder != "" {
			if linksOrder != "topo" {
				return reportErr(linksJSON, output.ErrValidation,
					fmt.Errorf("--order must be 'topo', got %q", linksOrder))
			}
			for _, r := range rels {
				spec := relTable[r]
				// Meta rels expand; if any constituent supports order that's
				// OK — but the nib plan said only specific rels support topo.
				// Reject when the explicit rel doesn't advertise AllowsOrder
				// (meta rels are rejected because their expanded atomic rels
				// mix order-supporting and non-supporting).
				if !spec.AllowsOrder {
					return reportErr(linksJSON, output.ErrValidation,
						fmt.Errorf("%w: --order topo not supported for rel %s", errLinksOrderInapplicable, r))
				}
			}
		}

		// Validate filters-on-singular. If any explicit rel is singular
		// (parent) and a filter is set, error out.
		filterFlags := readLinksFilterFlags()
		if !filterFlags.isEmpty() {
			for _, r := range rels {
				spec := relTable[r]
				if spec.IsSingular {
					filters := strings.Join(filterFlags.activeFilterNames(), ", ")
					return reportErr(linksJSON, output.ErrValidation,
						fmt.Errorf("%w: filter %s does not apply to rel %s (singular/chain relation)", errLinksFilterInapplicable, filters, r))
				}
			}
		}

		filter, err := filterFlags.buildNibFilter(forceActive)
		if err != nil {
			return reportErr(linksJSON, output.ErrValidation, err)
		}

		app := getApp(cmd)
		resolver := app.newResolver()
		// Use Cobra's cancellable context so SIGINT propagates through a
		// potentially long-running traversal (e.g. --rel descendants --depth all).
		// Guard against nil which can occur when cmd.Context() is not set (e.g.
		// tests that don't call ExecuteContext).
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}

		b, err := resolver.Query().Nib(ctx, args[0])
		if err != nil {
			return reportErr(linksJSON, output.ErrNotFound,
				fmt.Errorf("%w: %s: %w", errLinksNotFound, args[0], err))
		}
		if b == nil {
			return reportErr(linksJSON, output.ErrNotFound,
				fmt.Errorf("%w: %s", errLinksNotFound, args[0]))
		}

		result := LinksResult{ID: b.ID, relations: map[string]LinksRelBody{}}
		if depthSet {
			d := depthVal
			result.Depth = &d
		}

		for i, fetchKind := range fetched {
			// Build per-rel filter: singular rels (already validated to have
			// no filter via filterFlags.isEmpty()) get nil; everything else
			// uses the composite filter.
			perRelFilter := filter
			if relTable[fetchKind].IsSingular {
				perRelFilter = nil
			}

			got, ferr := fetchRel(ctx, resolver, b, fetchKind, perRelFilter, depthVal)
			if ferr != nil {
				code := output.ErrFileError
				if errors.Is(ferr, errLinksCycle) {
					code = output.ErrValidation
				}
				return reportErr(linksJSON, code,
					fmt.Errorf("fetching %s: %w", fetchKind, ferr))
			}
			if got == nil {
				got = []*nib.Nib{}
			}
			if linksOrder == "topo" && relTable[fetchKind].AllowsOrder {
				ordered, oerr := topoSortNibs(ctx, resolver, got)
				if oerr != nil {
					code := output.ErrFileError
					if errors.Is(oerr, errLinksCycle) {
						code = output.ErrValidation
					}
					return reportErr(linksJSON, code, oerr)
				}
				got = ordered
			}
			result.addRel(string(outKeys[i]), LinksRelBody{Nibs: got})
		}

		if linksJSON {
			if linksFlat {
				flat := linksToFlat(result)
				return output.JSONRaw(flat)
			}
			return output.JSONRaw(result)
		}

		return renderLinksHuman(cmd, result)
	},
}

// linksToFlat collapses a multi-rel LinksResult into a flat deduped list.
// Preserves first-encountered order across rels.
func linksToFlat(r LinksResult) LinksFlatResult {
	seen := map[string]bool{}
	var nibs []*nib.Nib
	for _, key := range r.relKeys {
		for _, n := range r.relations[key].Nibs {
			if seen[n.ID] {
				continue
			}
			seen[n.ID] = true
			nibs = append(nibs, n)
		}
	}
	if nibs == nil {
		nibs = []*nib.Nib{}
	}
	return LinksFlatResult{ID: r.ID, Depth: r.Depth, Nibs: nibs}
}

// renderLinksHuman prints a sectioned per-rel text block. Under --flat a
// single deduped list is rendered instead.
func renderLinksHuman(cmd *cobra.Command, r LinksResult) error {
	out := cmd.OutOrStdout()
	app := getApp(cmd)
	cfg := app.Config()

	if linksFlat {
		flat := linksToFlat(r)
		if len(flat.Nibs) == 0 {
			_, _ = fmt.Fprintln(out, ui.Muted.Render("No linked nibs."))
			return nil
		}
		for _, n := range flat.Nibs {
			_, _ = fmt.Fprintln(out, formatLinksRow(n, cfg))
		}
		return nil
	}

	if len(r.relKeys) == 0 {
		_, _ = fmt.Fprintln(out, ui.Muted.Render("No relations requested."))
		return nil
	}
	for i, key := range r.relKeys {
		if i > 0 {
			_, _ = fmt.Fprintln(out)
		}
		body := r.relations[key]
		_, _ = fmt.Fprintln(out, ui.Title.Render(key+":"))
		if len(body.Nibs) == 0 {
			_, _ = fmt.Fprintln(out, ui.Muted.Render("  (none)"))
			continue
		}
		for _, n := range body.Nibs {
			_, _ = fmt.Fprintln(out, "  "+formatLinksRow(n, cfg))
		}
	}
	return nil
}

func init() {
	linksCmd.Flags().StringArrayVar(&linksRel, "rel", nil, "Relationship to query (repeatable; comma-separated OK)")
	linksCmd.Flags().StringVar(&linksDepth, "depth", "", "Depth for transitive rels: N (positive integer) or 'all' (default 1)")
	linksCmd.Flags().StringVar(&linksOrder, "order", "", "Order the results (supports: topo)")
	linksCmd.Flags().BoolVar(&linksFlat, "flat", false, "Collapse multi-rel output to a single deduped list")
	linksCmd.Flags().BoolVar(&linksJSON, "json", false, "Output as JSON")
	linksCmd.Flags().StringArrayVarP(&linksStatus, "status", "s", nil, "Filter by status (repeatable)")
	linksCmd.Flags().StringArrayVar(&linksNoStatus, "no-status", nil, "Exclude by status (repeatable)")
	linksCmd.Flags().StringArrayVarP(&linksType, "type", "t", nil, "Filter by type (repeatable)")
	linksCmd.Flags().StringArrayVar(&linksNoType, "no-type", nil, "Exclude by type (repeatable)")
	linksCmd.Flags().StringArrayVarP(&linksPriority, "priority", "p", nil, "Filter by priority (repeatable)")
	linksCmd.Flags().StringArrayVar(&linksNoPriority, "no-priority", nil, "Exclude by priority (repeatable)")
	linksCmd.Flags().StringArrayVar(&linksTag, "tag", nil, "Filter by tag (repeatable)")
	linksCmd.Flags().StringArrayVarP(&linksEstimate, "estimate", "e", nil, "Filter by estimate (repeatable)")
	linksCmd.Flags().StringArrayVar(&linksNoEstimate, "no-estimate", nil, "Exclude by estimate (repeatable)")
	linksCmd.Flags().BoolVar(&linksActive, "active", false, "Exclude completed/scrapped nibs")
	rootCmd.AddCommand(linksCmd)
}
