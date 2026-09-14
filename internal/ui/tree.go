package ui

import (
	"github.com/alphaleonis/nibs/internal/nib"
)

// TreeNode represents a node in the nib tree hierarchy.
type TreeNode struct {
	Nib      *nib.Nib
	Children []*TreeNode
	Matched  bool // true if this nib matched the filter (vs. shown for context)
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
