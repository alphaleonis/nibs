package tui

import (
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/ui"
)

// effectiveSelection returns the SPACE-marked items whose ancestors are NOT
// also marked. Descendants of a marked ancestor ride along inside their
// parent's subtree and never move independently for block-move purposes.
//
// The returned slice is ordered by tree traversal (parent-before-child,
// sibling order).
func effectiveSelection(marked map[string]bool, tree []*ui.TreeNode) []*nib.Nib {
	var out []*nib.Nib
	walkEffective(tree, marked, false, &out)
	return out
}

// walkEffective walks the tree in-order. If ancestorMarked is true, skip
// recording nodes in this subtree (they ride along with the marked ancestor).
// When we hit a marked node, include it and stop descending into its children
// for the purpose of this collection (they are definitionally subsumed).
func walkEffective(nodes []*ui.TreeNode, marked map[string]bool, ancestorMarked bool, out *[]*nib.Nib) {
	for _, node := range nodes {
		isMarked := marked[node.Nib.ID]
		if isMarked && !ancestorMarked {
			*out = append(*out, node.Nib)
		}
		// If this node (or any ancestor) is marked, descendants ride along —
		// don't include them independently.
		walkEffective(node.Children, marked, ancestorMarked || isMarked, out)
	}
}

// blockMovable checks whether the effective selection can move as a contiguous
// block. Returns the parent's ordered sibling slice and the inclusive
// [startIdx, endIdx] range of the block within it. ok=false when the selection
// spans multiple parents, has gaps between selected items, or is empty.
func blockMovable(effective []*nib.Nib, tree []*ui.TreeNode) (siblings []*nib.Nib, startIdx, endIdx int, ok bool) {
	if len(effective) == 0 {
		return nil, 0, 0, false
	}

	parentID := effective[0].Parent
	// All effective items must share the same Parent (same parent scope).
	for _, n := range effective[1:] {
		if n.Parent != parentID {
			return nil, 0, 0, false
		}
	}

	siblings = siblingsFromTree(tree, parentID)
	if len(siblings) == 0 {
		return nil, 0, 0, false
	}

	// Locate each effective item's index in the sibling slice.
	indices := make([]int, 0, len(effective))
	effectiveIDs := make(map[string]bool, len(effective))
	for _, n := range effective {
		effectiveIDs[n.ID] = true
	}
	for i, s := range siblings {
		if effectiveIDs[s.ID] {
			indices = append(indices, i)
		}
	}
	if len(indices) != len(effective) {
		// Some effective item wasn't found among siblings — scope mismatch.
		return nil, 0, 0, false
	}

	// Indices come out sorted because we iterate siblings in order. Check
	// contiguity: last - first + 1 == count.
	startIdx = indices[0]
	endIdx = indices[len(indices)-1]
	if endIdx-startIdx+1 != len(indices) {
		return nil, 0, 0, false
	}

	return siblings, startIdx, endIdx, true
}

// siblingsFromTree returns the ordered siblings under the given parent ID.
// When parentID is empty, returns the tree's top-level nodes. Returns nil when
// the parent can't be located.
func siblingsFromTree(tree []*ui.TreeNode, parentID string) []*nib.Nib {
	if parentID == "" {
		out := make([]*nib.Nib, len(tree))
		for i, node := range tree {
			out[i] = node.Nib
		}
		return out
	}
	node := ui.FindNode(tree, parentID)
	if node == nil {
		return nil
	}
	out := make([]*nib.Nib, len(node.Children))
	for i, child := range node.Children {
		out[i] = child.Nib
	}
	return out
}
