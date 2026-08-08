package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/charmbracelet/lipgloss"
)

// TreeNode represents a node in the nib tree hierarchy.
type TreeNode struct {
	Nib      *nib.Nib
	Children []*TreeNode
	Matched  bool // true if this nib matched the filter (vs. shown for context)
}

// TreeNodeJSON is the JSON-serializable version of TreeNode.
type TreeNodeJSON struct {
	ID       string          `json:"id"`
	Slug     string          `json:"slug,omitempty"`
	Path     string          `json:"path"`
	Title    string          `json:"title"`
	Status   string          `json:"status"`
	Type     string          `json:"type,omitempty"`
	Priority string          `json:"priority,omitempty"`
	Tags     []string        `json:"tags,omitempty"`
	Body     string          `json:"body,omitempty"`
	Matched  bool            `json:"matched"`
	Children []*TreeNodeJSON `json:"children,omitempty"`
}

// ToJSON converts a TreeNode to its JSON-serializable form.
func (n *TreeNode) ToJSON(includeFull bool) *TreeNodeJSON {
	json := &TreeNodeJSON{
		ID:       n.Nib.ID,
		Slug:     n.Nib.Slug,
		Path:     n.Nib.Path,
		Title:    n.Nib.Title,
		Status:   n.Nib.Status,
		Type:     n.Nib.EffectiveType(),
		Priority: n.Nib.EffectivePriority(),
		Tags:     n.Nib.Tags,
		Matched:  n.Matched,
	}
	if includeFull {
		json.Body = n.Nib.Body
	}
	if len(n.Children) > 0 {
		json.Children = make([]*TreeNodeJSON, len(n.Children))
		for i, child := range n.Children {
			json.Children[i] = child.ToJSON(includeFull)
		}
	}
	return json
}

// BuildTree builds a tree structure from filtered nibs, including ancestors for context.
// matchedNibs: nibs that matched the filter
// allNibs: all nibs (needed to find ancestors)
// sortFn: function to sort nibs at each level
//
// A parent cycle is the one place where the rendered tree deliberately departs
// from the stored hierarchy, and three properties of that departure are a
// contract other packages read the tree through:
//
//  1. Exactly one member of every parent cycle is promoted to a root
//     (promotedCycleRoots picks the lowest id).
//  2. Promotion severs the tree edge only. The promoted nib keeps its stored
//     Nib.Parent, and the rest of the cycle nests underneath it — so the stored
//     parent lies inside the promoted nib's own subtree.
//  3. addAncestors closes the built set upward, so a cycle is never partially
//     present: any member that enters drags the rest in along its parent chain,
//     whether it matched the filter or arrived as ancestor context.
//
// The known consumer is internal/tui's reorder scoping (inParentCycle in
// blockmove.go), which reads (2) as its tell that a nib belongs to no parent's
// sibling list and refuses the reorder by naming the cycle. Break any of the
// three and that detection quietly answers false, sending the refusal back to
// describing a sibling list — which is why TestBuildTreeCyclePromotionContract
// pins all three here, next to the code that produces them, rather than leaving
// the failure to surface a package away in the TUI's message assertions.
func BuildTree(matchedNibs []*nib.Nib, allNibs []*nib.Nib, sortFn func([]*nib.Nib)) []*TreeNode {
	// Build index of all nibs by ID
	nibByID := make(map[string]*nib.Nib)
	for _, b := range allNibs {
		nibByID[b.ID] = b
	}

	// Build set of matched nib IDs
	matchedSet := make(map[string]bool)
	for _, b := range matchedNibs {
		matchedSet[b.ID] = true
	}

	// Find all ancestors needed for context
	// Start with matched nibs, then walk up parent links
	neededNibs := make(map[string]*nib.Nib)
	for _, b := range matchedNibs {
		neededNibs[b.ID] = b
	}

	// Add ancestors of matched nibs
	for _, b := range matchedNibs {
		addAncestors(b, nibByID, neededNibs)
	}

	// One member of every parent cycle is promoted to a root; without that, no
	// member of a cycle qualifies as a root and the whole cycle is dropped.
	promoted := promotedCycleRoots(neededNibs)

	// Build children index (parent ID -> children)
	children := make(map[string][]*nib.Nib)
	for _, b := range neededNibs {
		// A promoted nib's edge to its parent is severed — that is what breaks
		// the cycle, so the recursive walk below terminates.
		if b.Parent != "" && !promoted[b.ID] {
			// Only add as child if parent is in our needed set
			if _, ok := neededNibs[b.Parent]; ok {
				children[b.Parent] = append(children[b.Parent], b)
			}
		}
	}

	// Sort children at each level
	for parentID := range children {
		sortFn(children[parentID])
	}

	// Find root nibs (no parent, parent not in needed set, or promoted out of a cycle)
	var roots []*nib.Nib
	for _, b := range neededNibs {
		if b.Parent == "" || promoted[b.ID] {
			roots = append(roots, b)
		} else {
			// Check if parent is in the tree
			if _, ok := neededNibs[b.Parent]; !ok {
				roots = append(roots, b)
			}
		}
	}
	sortFn(roots)

	// Build tree nodes recursively
	return buildNodes(roots, children, matchedSet)
}

// promotedCycleRoots picks one member of every parent cycle lying wholly inside
// nibs. Every member of such a cycle has its parent present, so none satisfies
// the ordinary root rule and the cycle would render nowhere at all; promoting
// one member and severing its parent edge turns the cycle into an ordinary
// chain, so a malformed hierarchy shows up as an oddity instead of a
// disappearance.
//
// The member with the lowest id wins. That keeps the choice independent of Go's
// randomized map iteration order, and matches web/src/lib/tree.ts, which applies
// the same rule — so both views promote the same member and nest a cycle
// identically. Sibling ORDER still follows each view's own sort, as it does for
// ordinary trees.
//
// Comparison is over bytes here and UTF-16 code units there. Those orders differ
// only for supplementary-plane characters, which ids drawn from idAlphabet never
// contain — but ParseFilename applies no charset gate, so an imported file can
// carry one. If that ever happens the two views root the cycle at different
// members; nothing else breaks.
//
// A nib has at most one parent, so cycles are disjoint and each is discovered
// exactly once. Every nib is walked once — unseen -> onPath -> settled — making
// the pass linear in the size of the set.
func promotedCycleRoots(nibs map[string]*nib.Nib) map[string]bool {
	const (
		unseen = iota
		onPath
		settled
	)
	state := make(map[string]int, len(nibs))
	promoted := make(map[string]bool)

	for id := range nibs {
		if state[id] != unseen {
			continue
		}
		// Follow this nib's parent chain until it leaves the set, ends, or
		// re-enters itself.
		var path []string
		for cur := id; ; {
			if state[cur] == onPath {
				// The chain closed on itself: the cycle is the path from this
				// nib onward. Anything before it merely leads into the cycle.
				start := 0
				for i, m := range path {
					if m == cur {
						start = i
						break
					}
				}
				lowest := path[start]
				for _, m := range path[start+1:] {
					if m < lowest {
						lowest = m
					}
				}
				promoted[lowest] = true
				break
			}
			if state[cur] == settled {
				// Already fully explored, along with any cycle beyond it.
				break
			}
			state[cur] = onPath
			path = append(path, cur)
			b := nibs[cur]
			if b.Parent == "" {
				break
			}
			if _, ok := nibs[b.Parent]; !ok {
				break
			}
			cur = b.Parent
		}
		for _, m := range path {
			state[m] = settled
		}
	}

	return promoted
}

// addAncestors recursively adds all ancestors of a nib to the needed set.
func addAncestors(b *nib.Nib, nibByID map[string]*nib.Nib, needed map[string]*nib.Nib) {
	if b.Parent == "" {
		return
	}
	parent, ok := nibByID[b.Parent]
	if !ok {
		return // parent doesn't exist (broken link)
	}
	if _, alreadyNeeded := needed[b.Parent]; alreadyNeeded {
		return // already processed
	}
	needed[b.Parent] = parent
	addAncestors(parent, nibByID, needed)
}

// buildNodes recursively builds TreeNodes from nibs.
func buildNodes(nibs []*nib.Nib, children map[string][]*nib.Nib, matchedSet map[string]bool) []*TreeNode {
	nodes := make([]*TreeNode, len(nibs))
	for i, b := range nibs {
		nodes[i] = &TreeNode{
			Nib:      b,
			Matched:  matchedSet[b.ID],
			Children: buildNodes(children[b.ID], children, matchedSet),
		}
	}
	return nodes
}

// Tree rendering connector width (cells per indentation level). The actual
// glyphs are returned by the glyph accessors in glyphs.go so they can switch
// to ASCII fallbacks when the terminal cannot display UTF-8.
const treeIndent = 3

// Tree connectors. Each accessor returns the appropriate variant (UTF-8 or
// ASCII) at call time based on the current detection state.
func treeBranch() string     { return glyphTreeBranch() }
func treeLastBranch() string { return glyphTreeLastBranch() }
func treePipe() string       { return glyphTreePipe() }
func treeSpace() string      { return glyphTreeSpace() }

// treeMetrics returns the maximum depth of the tree and the largest visible
// position value sourced from positions (0 when positions is nil or no node
// has an entry). Single traversal — replaces the previous separate
// calculateMaxDepth + maxVisiblePosition walks.
func treeMetrics(nodes []*TreeNode, positions map[string]int) (depth, maxPos int) {
	for _, node := range nodes {
		if positions != nil {
			if pos, ok := positions[node.Nib.ID]; ok && pos > maxPos {
				maxPos = pos
			}
		}
		childDepth, childMaxPos := treeMetrics(node.Children, positions)
		if 1+childDepth > depth {
			depth = 1 + childDepth
		}
		if childMaxPos > maxPos {
			maxPos = childMaxPos
		}
	}
	return depth, maxPos
}

// RenderTree renders the tree as an ASCII tree with styled columns.
// termWidth is used to calculate responsive column widths.
// positions maps nib.ID -> 1-based natural position among siblings (see
// nib.PositionMap). Pass nil to omit the position column. Nibs in the tree
// without an entry in the map render as a blank in the column.
func RenderTree(nodes []*TreeNode, cfg *config.Config, maxIDWidth int, hasTags bool, termWidth int, positions map[string]int) string {
	var sb strings.Builder

	// One walk gives both: tree depth (for ID column width) and the largest
	// visible position (for the # column width).
	maxDepth, maxPos := treeMetrics(nodes, positions)
	// ID column needs: indent (3 chars per level beyond depth 1) + connector (3 chars) + ID width
	// depth 0: 0 extra chars
	// depth 1: 3 chars (connector only)
	// depth 2: 6 chars (3 indent + 3 connector)
	// depth N: (N-1)*3 + 3 = N*3 chars
	treeColWidth := maxIDWidth
	if maxDepth > 0 {
		treeColWidth = maxIDWidth + maxDepth*treeIndent
	}

	// Position column: width is digits in the largest visible position.
	// 0 means "do not render column" — covers both nil positions and the
	// "no visible node has a position entry" case (treeMetrics returns 0).
	posColWidth := 0
	if maxPos > 0 {
		posColWidth = len(strconv.Itoa(maxPos))
	}
	// Total cells the position column occupies on each row, including the
	// trailing space separating it from the ID column.
	posColTotal := 0
	if posColWidth > 0 {
		posColTotal = posColWidth + 1
	}

	// Calculate responsive columns based on terminal width
	// Adjust for tree column width vs default ID column width
	adjustedWidth := termWidth - treeColWidth + ColWidthID - posColTotal
	cols := CalculateResponsiveColumns(adjustedWidth, hasTags)

	// Calculate title width from remaining space.
	// Approximate near the responsive thresholds: posColTotal is subtracted
	// directly here AND influenced cols.Tags via adjustedWidth above, so the
	// title may be 2-4 cells off when crossing the tags-shown/hidden boundary.
	// Clamped to 20 below so layout stays sane.
	titleWidth := termWidth - posColTotal - treeColWidth - ColWidthType - ColWidthStatus - 3
	if cols.ShowTags {
		titleWidth -= cols.Tags
	}
	if titleWidth < 20 {
		titleWidth = 20
	}

	// Header with manual padding (lipgloss Width doesn't handle styled strings well)
	headerCol := lipgloss.NewStyle().Foreground(ColorMuted)
	var posHeader string
	if posColWidth > 0 {
		// Right-align "#" within the position column so it sits over the digits.
		posHeader = strings.Repeat(" ", posColWidth-1) + headerCol.Render("#") + " "
	}
	idHeader := headerCol.Render("ID") + strings.Repeat(" ", treeColWidth-2)
	typeHeader := headerCol.Render("T") + strings.Repeat(" ", ColWidthType-1)
	statusHeader := headerCol.Render("S") + strings.Repeat(" ", ColWidthStatus-1)

	header := posHeader + idHeader + typeHeader + statusHeader + headerCol.Render("TITLE")
	if cols.ShowTags && titleWidth > 5 {
		header += strings.Repeat(" ", titleWidth-5+3) + headerCol.Render("TAGS") // +3 for priority/spacing
	}
	dividerWidth := termWidth - 1 // -1 to avoid wrapping on exact terminal width
	sb.WriteString(header)
	sb.WriteString("\n")
	sb.WriteString(Muted.Render(strings.Repeat(glyphHRule(), dividerWidth)))
	sb.WriteString("\n")

	// Build render config from responsive columns
	renderCfg := treeRenderConfig{
		treeColWidth: treeColWidth,
		titleWidth:   titleWidth,
		cols:         cols,
		positions:    positions,
		posColWidth:  posColWidth,
	}

	// Render nodes (depth 0 = root level, no ancestry yet)
	renderNodes(&sb, nodes, 0, nil, cfg, renderCfg)

	return sb.String()
}

// treeRenderConfig holds computed rendering configuration for tree output
type treeRenderConfig struct {
	treeColWidth int
	titleWidth   int
	cols         ResponsiveColumns
	positions    map[string]int // nib ID -> 1-based natural position; nil = no column
	posColWidth  int            // 0 = column hidden
}

// renderNodes recursively renders tree nodes with proper indentation.
// depth 0 = root level (no connector), depth 1+ = nested (has connector)
// ancestry tracks whether each parent level was a last child (true = last, no continuation line needed)
func renderNodes(sb *strings.Builder, nodes []*TreeNode, depth int, ancestry []bool, cfg *config.Config, renderCfg treeRenderConfig) {
	for i, node := range nodes {
		isLast := i == len(nodes)-1
		renderNode(sb, node, depth, isLast, ancestry, cfg, renderCfg)
		// Only add to ancestry when depth > 0 (roots have no connectors to continue)
		if len(node.Children) > 0 {
			var newAncestry []bool
			if depth > 0 {
				newAncestry = append(ancestry, isLast)
			}
			renderNodes(sb, node.Children, depth+1, newAncestry, cfg, renderCfg)
		}
	}
}

// renderNode renders a single tree node with tree connectors.
// depth 0 = root (no connector), depth 1+ = nested (has connector)
// ancestry tracks whether each parent level was a last child (true = last, no continuation line needed)
func renderNode(sb *strings.Builder, node *TreeNode, depth int, isLast bool, ancestry []bool, cfg *config.Config, renderCfg treeRenderConfig) {
	b := node.Nib

	// Position column (right-aligned numeric, blank when this nib has no entry)
	if renderCfg.posColWidth > 0 {
		var cell string
		if pos, ok := renderCfg.positions[b.ID]; ok {
			cell = fmt.Sprintf("%*d", renderCfg.posColWidth, pos)
		} else {
			cell = strings.Repeat(" ", renderCfg.posColWidth)
		}
		sb.WriteString(Muted.Render(cell))
		sb.WriteString(" ")
	}

	// Build tree prefix from ancestry
	var prefix string
	if depth > 0 {
		for _, wasLast := range ancestry {
			if wasLast {
				prefix += treeSpace()
			} else {
				prefix += treePipe()
			}
		}
		if isLast {
			prefix += treeLastBranch()
		} else {
			prefix += treeBranch()
		}
	}

	// Get colors from config. Use EffectiveType so a type-less nib renders its
	// "task" badge (and color) as it did when loadNib synthesized the default. Raw
	// Priority is safe here despite the missing default: GetNibColors -> GetPriority
	// yields a DIFFERENT PriorityColor for "" (none) vs "normal" ("white"), but that
	// color is only ever consumed by RenderPrioritySymbol, which returns "" (discarding
	// the color) whenever GetPrioritySymbol is empty — and the symbol is empty for both
	// "" and "normal", so the rendered result is identical.
	colors := cfg.GetNibColors(b.Status, b.EffectiveType(), b.Priority)

	// Use shared RenderNibRow function with responsive columns
	row := RenderNibRow(b.ID, b.Status, b.EffectiveType(), b.Title, NibRowConfig{
		StatusColor:   colors.StatusColor,
		TypeColor:     colors.TypeColor,
		PriorityColor: colors.PriorityColor,
		Priority:      b.Priority,
		IsClosed:      colors.IsClosed,
		MaxTitleWidth: renderCfg.titleWidth,
		ShowCursor:    false,
		Tags:          b.Tags,
		ShowTags:      renderCfg.cols.ShowTags,
		TagsColWidth:  renderCfg.cols.Tags,
		MaxTags:       renderCfg.cols.MaxTags,
		TreePrefix:    prefix,
		Dimmed:        !node.Matched,
		IDColWidth:    renderCfg.treeColWidth,
	})

	sb.WriteString(row)
	sb.WriteString("\n")
}

// FlatItem represents a flattened tree node with rendering context.
// Used by TUI to render tree structure in a flat list.
type FlatItem struct {
	Nib         *nib.Nib
	Depth       int    // 0 = root, 1+ = nested
	IsLast      bool   // last child at this level
	Matched     bool   // true if nib matched filter (vs. shown for context)
	TreePrefix  string // pre-computed tree prefix (e.g., "  └─")
	HasChildren bool   // true if this node has children in the tree
	Collapsed   bool   // true if this node is collapsed (children hidden)
}

// FlattenTree converts a tree into a flat slice with tree context preserved.
// Each item includes the pre-computed tree prefix for rendering.
func FlattenTree(nodes []*TreeNode) []FlatItem {
	var items []FlatItem
	flattenNodes(nodes, 0, nil, &items)
	return items
}

// flattenNodes recursively flattens tree nodes.
// ancestry tracks whether each parent level was a last child (true = last, no continuation line needed)
func flattenNodes(nodes []*TreeNode, depth int, ancestry []bool, items *[]FlatItem) {
	for i, node := range nodes {
		isLast := i == len(nodes)-1

		// Compute tree prefix
		var prefix string
		if depth > 0 {
			// Build prefix from ancestry - each level adds either │ or space
			for _, wasLast := range ancestry {
				if wasLast {
					prefix += treeSpace() // parent was last child, no continuation line
				} else {
					prefix += treePipe() // parent has more siblings, show continuation line
				}
			}
			// Add connector for this node
			if isLast {
				prefix += treeLastBranch()
			} else {
				prefix += treeBranch()
			}
		}

		*items = append(*items, FlatItem{
			Nib:         node.Nib,
			Depth:       depth,
			IsLast:      isLast,
			Matched:     node.Matched,
			TreePrefix:  prefix,
			HasChildren: len(node.Children) > 0,
		})

		// Recurse into children, passing updated ancestry
		// Only add to ancestry when depth > 0 (roots have no connectors to continue)
		if len(node.Children) > 0 {
			var newAncestry []bool
			if depth > 0 {
				newAncestry = append(ancestry, isLast)
			}
			flattenNodes(node.Children, depth+1, newAncestry, items)
		}
	}
}

// MaxTreeDepth returns the maximum depth of the flattened tree.
func MaxTreeDepth(items []FlatItem) int {
	maxDepth := 0
	for _, item := range items {
		if item.Depth > maxDepth {
			maxDepth = item.Depth
		}
	}
	return maxDepth
}

// Collapse/expand indicator accessors. The collapsed/expanded glyphs degrade
// to ASCII fallbacks on terminals that cannot display UTF-8 (see glyphs.go).
// The leaf indicator is purely whitespace and identical in both modes.
const CollapseIndicatorLeaf = "  " // leaf node (no children) - keeps columns aligned

func CollapseIndicatorCollapsed() string { return glyphCollapseCollapsed() }
func CollapseIndicatorExpanded() string  { return glyphCollapseExpanded() }

// FlattenTreeFiltered converts a tree into a flat slice, skipping children of collapsed nodes.
// collapsedIDs is the set of node IDs whose children should be hidden.
func FlattenTreeFiltered(nodes []*TreeNode, collapsedIDs map[string]bool) []FlatItem {
	var items []FlatItem
	flattenNodesFiltered(nodes, 0, nil, collapsedIDs, &items)
	return items
}

// flattenNodesFiltered recursively flattens tree nodes, skipping children of collapsed nodes.
func flattenNodesFiltered(nodes []*TreeNode, depth int, ancestry []bool, collapsedIDs map[string]bool, items *[]FlatItem) {
	for i, node := range nodes {
		isLast := i == len(nodes)-1
		hasChildren := len(node.Children) > 0
		isCollapsed := hasChildren && collapsedIDs[node.Nib.ID]

		// Compute tree prefix
		var prefix string
		if depth > 0 {
			for _, wasLast := range ancestry {
				if wasLast {
					prefix += treeSpace()
				} else {
					prefix += treePipe()
				}
			}
			if isLast {
				prefix += treeLastBranch()
			} else {
				prefix += treeBranch()
			}
		}

		// Append collapse indicator
		if isCollapsed {
			prefix += CollapseIndicatorCollapsed()
		} else if hasChildren {
			prefix += CollapseIndicatorExpanded()
		} else {
			prefix += CollapseIndicatorLeaf
		}

		*items = append(*items, FlatItem{
			Nib:         node.Nib,
			Depth:       depth,
			IsLast:      isLast,
			Matched:     node.Matched,
			TreePrefix:  prefix,
			HasChildren: hasChildren,
			Collapsed:   isCollapsed,
		})

		// Recurse into children only if not collapsed
		if hasChildren && !isCollapsed {
			var newAncestry []bool
			if depth > 0 {
				newAncestry = append(ancestry, isLast)
			}
			flattenNodesFiltered(node.Children, depth+1, newAncestry, collapsedIDs, items)
		}
	}
}

// HasAnyChildren returns true if any node in the tree has children.
func HasAnyChildren(nodes []*TreeNode) bool {
	for _, node := range nodes {
		if len(node.Children) > 0 {
			return true
		}
		if HasAnyChildren(node.Children) {
			return true
		}
	}
	return false
}

// CollectParentIDs collects IDs of all nodes that have children into the provided map.
func CollectParentIDs(nodes []*TreeNode, ids map[string]bool) {
	for _, node := range nodes {
		if len(node.Children) > 0 {
			ids[node.Nib.ID] = true
			CollectParentIDs(node.Children, ids)
		}
	}
}

// FindNode searches the tree for a node with the given nib ID.
func FindNode(nodes []*TreeNode, id string) *TreeNode {
	for _, node := range nodes {
		if node.Nib.ID == id {
			return node
		}
		if found := FindNode(node.Children, id); found != nil {
			return found
		}
	}
	return nil
}
