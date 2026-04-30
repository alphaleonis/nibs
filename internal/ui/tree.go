package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/config"
)

// TreeNode represents a node in the nib tree hierarchy.
type TreeNode struct {
	Nib     *nib.Nib
	Children []*TreeNode
	Matched  bool // true if this nib matched the filter (vs. shown for context)
}

// TreeNodeJSON is the JSON-serializable version of TreeNode.
type TreeNodeJSON struct {
	ID        string          `json:"id"`
	Slug      string          `json:"slug,omitempty"`
	Path      string          `json:"path"`
	Title     string          `json:"title"`
	Status    string          `json:"status"`
	Type      string          `json:"type,omitempty"`
	Priority  string          `json:"priority,omitempty"`
	Tags      []string        `json:"tags,omitempty"`
	Body      string          `json:"body,omitempty"`
	Matched   bool            `json:"matched"`
	Children  []*TreeNodeJSON `json:"children,omitempty"`
}

// ToJSON converts a TreeNode to its JSON-serializable form.
func (n *TreeNode) ToJSON(includeFull bool) *TreeNodeJSON {
	json := &TreeNodeJSON{
		ID:       n.Nib.ID,
		Slug:     n.Nib.Slug,
		Path:     n.Nib.Path,
		Title:    n.Nib.Title,
		Status:   n.Nib.Status,
		Type:     n.Nib.Type,
		Priority: n.Nib.Priority,
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

	// Build children index (parent ID -> children)
	children := make(map[string][]*nib.Nib)
	for _, b := range neededNibs {
		if b.Parent != "" {
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

	// Find root nibs (no parent or parent not in needed set)
	var roots []*nib.Nib
	for _, b := range neededNibs {
		if b.Parent == "" {
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
			Nib:     b,
			Matched:  matchedSet[b.ID],
			Children: buildNodes(children[b.ID], children, matchedSet),
		}
	}
	return nodes
}

// Tree rendering constants
const (
	treeBranch     = "├─ "
	treeLastBranch = "└─ "
	treePipe       = "│  " // vertical line for ongoing branches
	treeSpace      = "   " // empty space for completed branches
	treeIndent     = 3     // width of connector
)

// calculateMaxDepth returns the maximum depth of the tree.
func calculateMaxDepth(nodes []*TreeNode) int {
	maxDepth := 0
	for _, node := range nodes {
		depth := 1 + calculateMaxDepth(node.Children)
		if depth > maxDepth {
			maxDepth = depth
		}
	}
	return maxDepth
}

// RenderTree renders the tree as an ASCII tree with styled columns.
// termWidth is used to calculate responsive column widths.
// positions maps nib.ID -> 1-based natural position among siblings (see
// nib.PositionMap). Pass nil to omit the position column. Nibs in the tree
// without an entry in the map render as a blank in the column.
func RenderTree(nodes []*TreeNode, cfg *config.Config, maxIDWidth int, hasTags bool, termWidth int, positions map[string]int) string {
	var sb strings.Builder

	// Calculate max depth to determine ID column width
	maxDepth := calculateMaxDepth(nodes)
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
	// 0 means "do not render column" — both when positions is nil and when
	// no visible node has a position entry.
	posColWidth := 0
	if positions != nil {
		maxPos := maxVisiblePosition(nodes, positions)
		if maxPos > 0 {
			posColWidth = len(fmt.Sprintf("%d", maxPos))
		}
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
	sb.WriteString(Muted.Render(strings.Repeat("─", dividerWidth)))
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

// maxVisiblePosition returns the largest position value among nibs visible in
// the tree (recursing through children). Returns 0 if none have positions.
func maxVisiblePosition(nodes []*TreeNode, positions map[string]int) int {
	highest := 0
	for _, node := range nodes {
		if pos, ok := positions[node.Nib.ID]; ok && pos > highest {
			highest = pos
		}
		if childMax := maxVisiblePosition(node.Children, positions); childMax > highest {
			highest = childMax
		}
	}
	return highest
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
				prefix += treeSpace
			} else {
				prefix += treePipe
			}
		}
		if isLast {
			prefix += treeLastBranch
		} else {
			prefix += treeBranch
		}
	}

	// Get colors from config
	colors := cfg.GetNibColors(b.Status, b.Type, b.Priority)

	// Use shared RenderNibRow function with responsive columns
	row := RenderNibRow(b.ID, b.Status, b.Type, b.Title, NibRowConfig{
		StatusColor:   colors.StatusColor,
		TypeColor:     colors.TypeColor,
		PriorityColor: colors.PriorityColor,
		Priority:      b.Priority,
		IsArchive:     colors.IsArchive,
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
	Nib        *nib.Nib
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
					prefix += treeSpace // parent was last child, no continuation line
				} else {
					prefix += treePipe // parent has more siblings, show continuation line
				}
			}
			// Add connector for this node
			if isLast {
				prefix += treeLastBranch
			} else {
				prefix += treeBranch
			}
		}

		*items = append(*items, FlatItem{
			Nib:        node.Nib,
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

// Collapse/expand indicator constants
const (
	CollapseIndicatorCollapsed = "▸ " // collapsed node with children
	CollapseIndicatorExpanded  = "▾ " // expanded node with children
	CollapseIndicatorLeaf      = "  " // leaf node (no children) - keeps columns aligned
)

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
					prefix += treeSpace
				} else {
					prefix += treePipe
				}
			}
			if isLast {
				prefix += treeLastBranch
			} else {
				prefix += treeBranch
			}
		}

		// Append collapse indicator
		if isCollapsed {
			prefix += CollapseIndicatorCollapsed
		} else if hasChildren {
			prefix += CollapseIndicatorExpanded
		} else {
			prefix += CollapseIndicatorLeaf
		}

		*items = append(*items, FlatItem{
			Nib:        node.Nib,
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
