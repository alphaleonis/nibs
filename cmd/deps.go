package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/toposort"
	"github.com/spf13/cobra"
)

var (
	depsJSON   bool
	depsActive bool
	depsCycles bool
	depsGraph  string
)

// Sentinel errors for classification by the RunE handler. Wrapping these with
// fmt.Errorf("%w: ...") keeps a human-readable message while letting callers
// use errors.Is to classify reliably — no more substring-matching on text
// that can drift out from under the code.
var (
	errDepsNotFound = errors.New("nib not found")
	errDepsCycle    = errors.New("dependency cycle")
)

// DepsItem is one child in a deps view. DependsOn lists the IDs of other
// children in the same subtree whose bodies this child mentions (so this
// child transitively depends on them via the parent's topological order).
type DepsItem struct {
	Position  int      `json:"position"`
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Status    string   `json:"status"`
	Type      string   `json:"type,omitempty"`
	DependsOn []string `json:"depends_on"`
}

// DepsExternal captures a mention from a child inside the subtree to a nib
// outside the subtree — recorded for visibility but not part of the sort.
type DepsExternal struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// DepsCycleNode identifies one node in a cycle — the ID (which participates
// in the cycle) plus the title so renderers can produce a meaningful label
// without round-tripping through the core store.
type DepsCycleNode struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// DepsCycle captures the set of children that participate in a cycle. The
// exact order within `Nodes` is implementation-defined (see buildDeps);
// consumers should treat the cycle as an unordered set.
type DepsCycle struct {
	Nodes []DepsCycleNode `json:"nodes"`
}

// DepsResult is the complete deps output — parent summary, topologically
// ordered children, external dependencies, and optional cycles.
type DepsResult struct {
	Parent       PlanParent     `json:"parent"`
	Items        []DepsItem     `json:"items"`
	ExternalDeps []DepsExternal `json:"external_deps"`
	Cycles       []DepsCycle    `json:"cycles,omitempty"`
}

// buildDeps loads parent + children, computes mention edges internal to the
// subtree, captures external edges, and topologically sorts. When cyclesMode
// is false and cycles exist, returns an error naming the participating nodes.
// When cyclesMode is true, cycles populate result.Cycles and nodes in cycles
// are omitted from result.Items.
func buildDeps(resolver *graph.Resolver, parentID string, activeOnly, cyclesMode bool) (*DepsResult, error) {
	ctx := context.Background()
	parent, err := resolver.Query().Nib(ctx, parentID)
	if err != nil {
		// Wrap both the original resolver error and errDepsNotFound so
		// callers classifying via errors.Is see this as a not-found (the
		// question we were asking) while still retaining the underlying
		// cause for diagnostics.
		return nil, fmt.Errorf("%w: %s: %w", errDepsNotFound, parentID, err)
	}
	if parent == nil {
		return nil, fmt.Errorf("%w: %s", errDepsNotFound, parentID)
	}

	// allSiblings tracks the full unfiltered sibling set so mentions of a
	// filtered-out sibling (e.g. a completed one under --active) can be
	// silently dropped instead of leaking into ExternalDeps. External is
	// reserved for references to nibs genuinely outside the subtree.
	allSiblings := resolver.Orderer.GetSortedSiblings(parent.ID)
	allSiblingIDs := make(map[string]struct{}, len(allSiblings))
	for _, s := range allSiblings {
		allSiblingIDs[s.ID] = struct{}{}
	}

	children := allSiblings
	if activeOnly {
		children = filterActive(children)
	}

	// subtreeByID indexes children by ID for O(1) "is this nib in the
	// subtree?" checks while building edges. Self-references are excluded
	// later by an explicit ID-equality guard.
	subtreeByID := make(map[string]*nib.Nib, len(children))
	subtreeIDs := make([]string, 0, len(children))
	for _, c := range children {
		subtreeByID[c.ID] = c
		subtreeIDs = append(subtreeIDs, c.ID)
	}

	// Collect mention edges. Internal edges (both endpoints in subtree)
	// feed the topo sort and DependsOn list. External edges (child →
	// nib outside subtree) are surfaced separately so the caller sees
	// the full dependency picture without polluting the sort.
	var edges [][2]string
	var externalDeps []DepsExternal
	dependsOn := make(map[string][]string, len(children))
	seenInternalEdge := make(map[[2]string]struct{})
	seenExternalEdge := make(map[[2]string]struct{})
	for _, child := range children {
		mentions, err := resolver.Nib().Mentions(ctx, child, nil)
		if err != nil {
			return nil, fmt.Errorf("resolving mentions for %s: %w", child.ID, err)
		}
		for _, m := range mentions {
			if m.ID == child.ID {
				// Self-reference — ignored at both the edge and the
				// DependsOn level. Can't cause a cycle.
				continue
			}
			if _, inSubtree := subtreeByID[m.ID]; inSubtree {
				key := [2]string{m.ID, child.ID}
				if _, dup := seenInternalEdge[key]; dup {
					continue
				}
				seenInternalEdge[key] = struct{}{}
				// Edge direction: m must come before child, so the
				// topo edge is [m.ID, child.ID]. child DependsOn m.
				edges = append(edges, key)
				dependsOn[child.ID] = append(dependsOn[child.ID], m.ID)
			} else if _, isFilteredSibling := allSiblingIDs[m.ID]; isFilteredSibling {
				// A sibling that was filtered out (e.g. completed
				// under --active). Drop silently: it's not a cycle,
				// not a dependency we can track, and not genuinely
				// external.
				continue
			} else {
				key := [2]string{child.ID, m.ID}
				if _, dup := seenExternalEdge[key]; dup {
					continue
				}
				seenExternalEdge[key] = struct{}{}
				externalDeps = append(externalDeps, DepsExternal{From: child.ID, To: m.ID})
			}
		}
	}

	ordered, cycles := toposort.Sort(subtreeIDs, edges)

	if len(cycles) > 0 && !cyclesMode {
		// Name participating nodes in a stable order so the error message
		// is reproducible across runs.
		names := []string{}
		for _, cyc := range cycles {
			c := append([]string(nil), cyc...)
			sort.Strings(c)
			names = append(names, "["+strings.Join(c, ", ")+"]")
		}
		return nil, fmt.Errorf("%w detected: %s", errDepsCycle, strings.Join(names, ", "))
	}

	result := &DepsResult{
		Parent: PlanParent{
			ID:     parent.ID,
			Title:  parent.Title,
			Status: parent.Status,
			Type:   parent.Type,
		},
		Items:        []DepsItem{},
		ExternalDeps: []DepsExternal{},
	}

	for i, id := range ordered {
		child := subtreeByID[id]
		deps := dependsOn[id]
		if deps == nil {
			deps = []string{}
		}
		result.Items = append(result.Items, DepsItem{
			Position:  i + 1,
			ID:        child.ID,
			Title:     child.Title,
			Status:    child.Status,
			Type:      child.Type,
			DependsOn: deps,
		})
	}

	if len(externalDeps) > 0 {
		result.ExternalDeps = externalDeps
	}

	if cyclesMode && len(cycles) > 0 {
		for _, cyc := range cycles {
			c := append([]string(nil), cyc...)
			sort.Strings(c)
			nodes := make([]DepsCycleNode, 0, len(c))
			for _, id := range c {
				title := id
				if child, ok := subtreeByID[id]; ok && child != nil {
					title = child.Title
				}
				nodes = append(nodes, DepsCycleNode{ID: id, Title: title})
			}
			result.Cycles = append(result.Cycles, DepsCycle{Nodes: nodes})
		}
	}

	return result, nil
}

var depsCmd = &cobra.Command{
	Use:   "deps <parent-id>",
	Short: "Show children of a parent nib topologically sorted by body-reference dependencies",
	Long: `Produces a dependency-aware ordering of a parent nib's children, derived
from #<id> mentions in each child's body. Use this to decide what order to
tackle an epic's children when some depend on others finishing first.

Flags:
  --active              exclude completed/scrapped children (matches plan --active)
  --cycles              report cycles in a dedicated section instead of erroring
  --graph mermaid|dot   emit a graph in the given format (default: text)
  --json                structured JSON output

Edges are derived from #<id> mentions: if child B's body mentions #a (where
A is also a child of the same parent), then A must come before B. Mentions of
nibs outside the subtree are recorded in 'external_deps' but do not affect
the order. Self-mentions are ignored. Cycles default to a hard error so
agents don't silently plan against an unsatisfiable order — use --cycles to
list the cyclic groups for manual resolution.

--json and --graph are mutually exclusive — JSON is the structured output and
--graph is a rendering mode, so combining them would be ambiguous. Pick one.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Validate flag combinations BEFORE loading the nib so malformed
		// invocations fail fast without paying for a filesystem resolve.
		// Mirrors the refs command's flag-only-validation-first convention.
		if depsJSON && depsGraph != "" {
			return reportErr(depsJSON, output.ErrValidation,
				fmt.Errorf("--json and --graph are mutually exclusive"))
		}
		if depsGraph != "" && depsGraph != "mermaid" && depsGraph != "dot" {
			return reportErr(depsJSON, output.ErrValidation,
				fmt.Errorf("--graph must be 'mermaid' or 'dot', got %q", depsGraph))
		}

		app := getApp(cmd)
		resolver := app.newResolver()

		result, err := buildDeps(resolver, args[0], depsActive, depsCycles)
		if err != nil {
			code := output.ErrFileError
			switch {
			case errors.Is(err, errDepsNotFound):
				code = output.ErrNotFound
			case errors.Is(err, errDepsCycle):
				code = output.ErrValidation
			}
			return reportErr(depsJSON, code, err)
		}

		if depsJSON {
			return output.JSONRaw(result)
		}
		if depsGraph == "mermaid" {
			return renderDepsMermaid(cmd.OutOrStdout(), result)
		}
		if depsGraph == "dot" {
			return renderDepsDOT(cmd.OutOrStdout(), result)
		}
		return renderDepsHuman(cmd.OutOrStdout(), result)
	},
}

// renderDepsHuman prints a compact position / id / status / title list plus
// per-item dependencies and (if any) a trailing cycles block. Mirrors
// renderPlanHuman's style so the two commands feel like siblings.
func renderDepsHuman(out io.Writer, r *DepsResult) error {
	_, _ = fmt.Fprintf(out, "%s  %s\n\n", r.Parent.ID, r.Parent.Title)
	if len(r.Items) == 0 && len(r.Cycles) == 0 {
		_, _ = fmt.Fprintln(out, "No children.")
		return nil
	}
	for _, item := range r.Items {
		line := fmt.Sprintf("  %d. [%s] %s (%s)", item.Position, item.Status, item.Title, item.ID)
		if len(item.DependsOn) > 0 {
			line += "  ← depends on " + strings.Join(item.DependsOn, ", ")
		}
		_, _ = fmt.Fprintln(out, line)
	}
	if len(r.ExternalDeps) > 0 {
		_, _ = fmt.Fprintln(out, "\nExternal dependencies:")
		for _, ed := range r.ExternalDeps {
			_, _ = fmt.Fprintf(out, "  %s → %s\n", ed.From, ed.To)
		}
	}
	if len(r.Cycles) > 0 {
		_, _ = fmt.Fprintln(out, "\nCycles:")
		for _, cyc := range r.Cycles {
			ids := make([]string, len(cyc.Nodes))
			for i, n := range cyc.Nodes {
				ids[i] = n.ID
			}
			_, _ = fmt.Fprintf(out, "  [%s]\n", strings.Join(ids, ", "))
		}
	}
	return nil
}

// mermaidEscape replaces characters that have structural meaning inside a
// Mermaid node label with HTML entities. Mermaid does NOT accept backslash
// escaping — using %q would produce broken output for any title containing
// a double quote or square bracket. The set below covers the characters
// Mermaid treats specially inside a "..." label.
func mermaidEscape(s string) string {
	replacer := strings.NewReplacer(
		`"`, "#quot;",
		"<", "#lt;",
		">", "#gt;",
		"[", "#91;",
		"]", "#93;",
	)
	return replacer.Replace(s)
}

// renderDepsMermaid emits a Mermaid `graph TD` block: one node line per
// child (id with quoted title as label) and one arrow per internal edge
// (depends_on). External deps are rendered as dashed arrows so consumers
// can visually distinguish "within subtree" from "outside subtree". Cycle
// members are still included as nodes — the diagram reflects the mention
// graph faithfully; callers opt into cycle handling via --cycles.
func renderDepsMermaid(out io.Writer, r *DepsResult) error {
	_, _ = fmt.Fprintln(out, "graph TD")
	// Union of cycle members and Items so cyclic nodes (which are omitted
	// from Items under --cycles) still render with their actual titles.
	titles := make(map[string]string, len(r.Items))
	for _, item := range r.Items {
		titles[item.ID] = item.Title
	}
	for _, cyc := range r.Cycles {
		for _, n := range cyc.Nodes {
			if _, ok := titles[n.ID]; !ok {
				titles[n.ID] = n.Title
			}
		}
	}
	// Deterministic node-line order: follow Items order, then append any
	// extra cycle-only nodes sorted by ID.
	for _, item := range r.Items {
		_, _ = fmt.Fprintf(out, "  %s[\"%s\"]\n", item.ID, mermaidEscape(item.Title))
	}
	extras := []string{}
	for id := range titles {
		inItems := false
		for _, item := range r.Items {
			if item.ID == id {
				inItems = true
				break
			}
		}
		if !inItems {
			extras = append(extras, id)
		}
	}
	sort.Strings(extras)
	for _, id := range extras {
		_, _ = fmt.Fprintf(out, "  %s[\"%s\"]\n", id, mermaidEscape(titles[id]))
	}
	// Internal edges: from dependency → child ("A --> B" means B depends on A).
	for _, item := range r.Items {
		for _, dep := range item.DependsOn {
			_, _ = fmt.Fprintf(out, "  %s --> %s\n", dep, item.ID)
		}
	}
	for _, ed := range r.ExternalDeps {
		_, _ = fmt.Fprintf(out, "  %s -.-> %s\n", ed.From, ed.To)
	}
	return nil
}

// renderDepsDOT emits a Graphviz digraph with properly quoted labels so
// titles containing double quotes survive the round-trip. External deps
// are styled as dashed edges to match the Mermaid renderer.
func renderDepsDOT(out io.Writer, r *DepsResult) error {
	_, _ = fmt.Fprintln(out, "digraph G {")
	// Emit node declarations with quoted labels. Also include cycle members
	// that may be omitted from Items — their titles travel in DepsCycleNode
	// so we no longer have to fall back to the ID as a label.
	knownTitles := make(map[string]string, len(r.Items))
	for _, item := range r.Items {
		knownTitles[item.ID] = item.Title
		_, _ = fmt.Fprintf(out, "  %q [label=%q];\n", item.ID, item.Title)
	}
	extras := []string{}
	for _, cyc := range r.Cycles {
		for _, n := range cyc.Nodes {
			if _, known := knownTitles[n.ID]; !known {
				extras = append(extras, n.ID)
				knownTitles[n.ID] = n.Title
			}
		}
	}
	sort.Strings(extras)
	for _, id := range extras {
		_, _ = fmt.Fprintf(out, "  %q [label=%q];\n", id, knownTitles[id])
	}
	for _, item := range r.Items {
		for _, dep := range item.DependsOn {
			_, _ = fmt.Fprintf(out, "  %q -> %q;\n", dep, item.ID)
		}
	}
	for _, ed := range r.ExternalDeps {
		_, _ = fmt.Fprintf(out, "  %q -> %q [style=dashed];\n", ed.From, ed.To)
	}
	_, _ = fmt.Fprintln(out, "}")
	return nil
}

func init() {
	depsCmd.Flags().BoolVar(&depsJSON, "json", false, "Output as JSON")
	depsCmd.Flags().BoolVar(&depsActive, "active", false, "Exclude completed/scrapped children before computing the sort")
	depsCmd.Flags().BoolVar(&depsCycles, "cycles", false, "Report cycles separately instead of erroring")
	depsCmd.Flags().StringVar(&depsGraph, "graph", "", "Emit graph format: mermaid or dot")
	rootCmd.AddCommand(depsCmd)
}

