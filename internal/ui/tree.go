package ui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
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

// BuildTree builds a tree of matchedNibs plus their ancestors from allNibs,
// sorting each level with sortFn.
//
// Where a parent cycle breaks the hierarchy, internal/tui's inParentCycle relies
// on three properties, pinned by TestBuildTreeCyclePromotionContract:
//
//  1. Exactly one member of each cycle is promoted to a root: the lowest id.
//  2. The promoted nib keeps its stored Nib.Parent; only the tree edge is
//     severed, and the rest of the cycle nests beneath it.
//  3. Ancestors are closed upward, so a cycle is never partially present.
func BuildTree(matchedNibs []*nib.Nib, allNibs []*nib.Nib, sortFn func([]*nib.Nib)) []*TreeNode {
	nibByID := make(map[string]*nib.Nib)
	for _, b := range allNibs {
		nibByID[b.ID] = b
	}

	matchedSet := make(map[string]bool)
	for _, b := range matchedNibs {
		matchedSet[b.ID] = true
	}

	neededNibs := make(map[string]*nib.Nib)
	for _, b := range matchedNibs {
		neededNibs[b.ID] = b
	}

	for _, b := range matchedNibs {
		addAncestors(b, nibByID, neededNibs)
	}

	// No member of a cycle meets the root rule below, so without a promoted
	// member the whole cycle is dropped.
	promoted := promotedCycleRoots(neededNibs)

	children := make(map[string][]*nib.Nib)
	for _, b := range neededNibs {
		// Severing a promoted nib's parent edge breaks its cycle, so buildNodes
		// terminates.
		if b.Parent != "" && !promoted[b.ID] {
			if _, ok := neededNibs[b.Parent]; ok {
				children[b.Parent] = append(children[b.Parent], b)
			}
		}
	}

	for parentID := range children {
		sortFn(children[parentID])
	}

	var roots []*nib.Nib
	for _, b := range neededNibs {
		if b.Parent == "" || promoted[b.ID] {
			roots = append(roots, b)
		} else {
			if _, ok := neededNibs[b.Parent]; !ok {
				roots = append(roots, b)
			}
		}
	}
	sortFn(roots)

	return buildNodes(roots, children, matchedSet)
}

// promotedCycleRoots picks the lowest-id member of every parent cycle lying
// wholly inside nibs, for BuildTree to promote to a root.
//
// web/src/lib/tree.ts applies the same rule; keep the two in agreement. Go
// compares bytes and TypeScript UTF-16 code units, so ids holding
// supplementary-plane characters can promote different members.
//
// Each nib is walked once: unseen -> onPath -> settled.
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
		// Follow the parent chain until it leaves the set, ends, or revisits
		// the path.
		var path []string
		for cur := id; ; {
			if state[cur] == onPath {
				// The cycle is the path from cur onward; entries before it lead
				// into the cycle.
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
				// Explored already, along with any cycle beyond it.
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

// treeIndent is the cell width of one tree connector.
const treeIndent = 3

func treeBranch() string     { return glyphTreeBranch() }
func treeLastBranch() string { return glyphTreeLastBranch() }
func treePipe() string       { return glyphTreePipe() }
func treeSpace() string      { return glyphTreeSpace() }

// treeMetrics returns the tree's depth in levels (1 for roots alone) and the
// largest value positions holds for any of its nodes (0 when none).
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

	maxDepth, maxPos := treeMetrics(nodes, positions)
	// One treeIndent per level. Roots draw no connector, so this is one indent
	// wider than the deepest prefix.
	treeColWidth := maxIDWidth
	if maxDepth > 0 {
		treeColWidth = maxIDWidth + maxDepth*treeIndent
	}

	// 0 hides the position column.
	posColWidth := 0
	if maxPos > 0 {
		posColWidth = len(strconv.Itoa(maxPos))
	}
	// The position column plus its trailing separator.
	posColTotal := 0
	if posColWidth > 0 {
		posColTotal = posColWidth + 1
	}

	adjustedWidth := termWidth - treeColWidth + ColWidthID - posColTotal
	cols := CalculateResponsiveColumns(adjustedWidth, hasTags)

	// Approximate near the tag thresholds, where posColTotal also moved
	// adjustedWidth; kept at least 20.
	titleWidth := termWidth - posColTotal - treeColWidth - ColWidthType - ColWidthStatus - 3
	if cols.ShowTags {
		titleWidth -= cols.Tags
	}
	if titleWidth < 20 {
		titleWidth = 20
	}

	headerCol := lipgloss.NewStyle().Foreground(ColorMuted)
	var posHeader string
	if posColWidth > 0 {
		// Right-align "#" over the digits.
		posHeader = strings.Repeat(" ", posColWidth-1) + headerCol.Render("#") + " "
	}
	idHeader := headerCol.Render("ID") + strings.Repeat(" ", treeColWidth-2)
	typeHeader := headerCol.Render("T") + strings.Repeat(" ", ColWidthType-1)
	statusHeader := headerCol.Render("S") + strings.Repeat(" ", ColWidthStatus-1)

	header := posHeader + idHeader + typeHeader + statusHeader + headerCol.Render("TITLE")
	if cols.ShowTags && titleWidth > 5 {
		header += strings.Repeat(" ", titleWidth-5+3) + headerCol.Render("TAGS")
	}
	dividerWidth := termWidth - 1 // -1 to avoid wrapping on exact terminal width
	sb.WriteString(header)
	sb.WriteString("\n")
	sb.WriteString(Muted.Render(strings.Repeat(glyphHRule(), dividerWidth)))
	sb.WriteString("\n")

	renderCfg := treeRenderConfig{
		treeColWidth: treeColWidth,
		titleWidth:   titleWidth,
		cols:         cols,
		positions:    positions,
		posColWidth:  posColWidth,
	}

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

	// Right-aligned position, blank when this nib has none.
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

	// Raw Priority is fine: "" and "normal" both render no priority symbol, so
	// the color that differs between them is never drawn.
	colors := cfg.GetNibColors(b.Status, b.EffectiveType(), b.Priority)

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
	TreePrefix  string // pre-computed tree prefix
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

		*items = append(*items, FlatItem{
			Nib:         node.Nib,
			Depth:       depth,
			IsLast:      isLast,
			Matched:     node.Matched,
			TreePrefix:  prefix,
			HasChildren: len(node.Children) > 0,
		})

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

// CollapseIndicatorLeaf pads a leaf to the width of the collapse indicators.
const CollapseIndicatorLeaf = "  "

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
