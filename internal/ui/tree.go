package ui

import (
	"strings"

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

// CollapseIndicatorLeaf pads a leaf to the width of the collapse indicators.
const CollapseIndicatorLeaf = "  "

func CollapseIndicatorCollapsed(ascii bool) string { return glyphCollapseCollapsed(ascii) }
func CollapseIndicatorExpanded(ascii bool) string  { return glyphCollapseExpanded(ascii) }

// TreePrefixes returns the tree connectors and the fold mark each of rows is
// drawn with, in display order. A row is folded when it has children and
// folded reports it. ascii draws them in ASCII.
//
// rows are the rows a view shows, so a row's siblings are the rows at its depth
// up to the next shallower row.
func TreePrefixes(rows []viewshape.Row, folded func(viewshape.Row) bool, ascii bool) []string {
	// last[i] reports whether no sibling follows rows[i]. Scanning upward,
	// laterAt[d] holds whether a row at depth d follows before a shallower one.
	last := make([]bool, len(rows))
	var laterAt []bool
	for i := len(rows) - 1; i >= 0; i-- {
		d := rows[i].Depth
		for len(laterAt) <= d {
			laterAt = append(laterAt, false)
		}
		last[i] = !laterAt[d]
		laterAt[d] = true
		clear(laterAt[d+1:])
	}

	prefixes := make([]string, len(rows))
	// lastAt[d] is last for the nearest row above at depth d.
	var lastAt []bool
	for i, r := range rows {
		lastAt = append(lastAt[:min(r.Depth, len(lastAt))], last[i])
		var b strings.Builder
		if r.Depth > 0 {
			for _, ancestorLast := range lastAt[1:r.Depth] {
				if ancestorLast {
					b.WriteString(glyphTreeSpace(ascii))
				} else {
					b.WriteString(glyphTreePipe(ascii))
				}
			}
			if last[i] {
				b.WriteString(glyphTreeLastBranch(ascii))
			} else {
				b.WriteString(glyphTreeBranch(ascii))
			}
		}
		switch {
		case r.HasChildren && folded(r):
			b.WriteString(CollapseIndicatorCollapsed(ascii))
		case r.HasChildren:
			b.WriteString(CollapseIndicatorExpanded(ascii))
		default:
			b.WriteString(CollapseIndicatorLeaf)
		}
		prefixes[i] = b.String()
	}
	return prefixes
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
