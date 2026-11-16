package ui

import (
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/viewshape"
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
	promoted := viewshape.PromotedCycleRoots(neededNibs)

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

// CollapseIndicatorLeaf pads a leaf to the width of the collapse indicators.
const CollapseIndicatorLeaf = "  "

func CollapseIndicatorCollapsed(ascii bool) string { return glyphCollapseCollapsed(ascii) }
func CollapseIndicatorExpanded(ascii bool) string  { return glyphCollapseExpanded(ascii) }

// FlattenTreeFiltered converts a tree into a flat slice, skipping children of collapsed nodes.
// collapsedIDs is the set of node IDs whose children should be hidden. ascii
// draws the tree prefixes in ASCII.
func FlattenTreeFiltered(nodes []*TreeNode, collapsedIDs map[string]bool, ascii bool) []FlatItem {
	var items []FlatItem
	flattenNodesFiltered(nodes, 0, nil, collapsedIDs, ascii, &items)
	return items
}

// flattenNodesFiltered recursively flattens tree nodes, skipping children of collapsed nodes.
func flattenNodesFiltered(nodes []*TreeNode, depth int, ancestry []bool, collapsedIDs map[string]bool, ascii bool, items *[]FlatItem) {
	for i, node := range nodes {
		isLast := i == len(nodes)-1
		hasChildren := len(node.Children) > 0
		isCollapsed := hasChildren && collapsedIDs[node.Nib.ID]

		var prefix string
		if depth > 0 {
			for _, wasLast := range ancestry {
				if wasLast {
					prefix += glyphTreeSpace(ascii)
				} else {
					prefix += glyphTreePipe(ascii)
				}
			}
			if isLast {
				prefix += glyphTreeLastBranch(ascii)
			} else {
				prefix += glyphTreeBranch(ascii)
			}
		}

		if isCollapsed {
			prefix += CollapseIndicatorCollapsed(ascii)
		} else if hasChildren {
			prefix += CollapseIndicatorExpanded(ascii)
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
			flattenNodesFiltered(node.Children, depth+1, newAncestry, collapsedIDs, ascii, items)
		}
	}
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
