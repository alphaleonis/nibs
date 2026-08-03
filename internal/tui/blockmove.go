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

// statusKind selects how the footer colors a status message. It rides
// alongside the message rather than being inferred from its text, so a
// rewording cannot silently turn a refusal green.
type statusKind int

const (
	statusOK statusKind = iota
	statusWarn
)

// Reasons a reorder is refused. They are shown verbatim in the list footer, so
// they describe the selection in the user's terms rather than the code's.
//
// The last three are defensive: every nib a reorder can reach is drawn from the
// tree and therefore appears under its own resolved parent, so no selection the
// UI can produce reaches them. They are kept because they guard the assumption
// rather than restate it — reaching one means the tree and the sibling lookup
// have diverged, which is worth saying out loud instead of moving nothing.
const (
	reorderReasonNothingSelected  = "Nothing to move"
	reorderReasonDifferentParents = "Can't move: selected nibs have different parents"
	reorderReasonNotContiguous    = "Can't move: select nibs that are next to each other"
	reorderReasonAtTop            = "Already at the top"
	reorderReasonAtBottom         = "Already at the bottom"

	reorderReasonNoSiblings       = "Can't move: that nib has no siblings to move among"
	reorderReasonNotAmongSiblings = "Can't move: the selection does not match its parent's list"
	reorderReasonNotInList        = "Can't move: that nib is not in its parent's list"
)

// blockMovable checks whether the effective selection can move as a contiguous
// block. Returns the parent's ordered sibling slice and the inclusive
// [startIdx, endIdx] range of the block within it. reason is empty when the
// block can move; otherwise it explains the refusal.
func blockMovable(effective []*nib.Nib, tree []*ui.TreeNode) (siblings []*nib.Nib, startIdx, endIdx int, reason string) {
	if len(effective) == 0 {
		return nil, 0, 0, reorderReasonNothingSelected
	}

	parentID := treeResolvedParentID(effective[0], tree)
	// All effective items must share the same parent scope.
	for _, n := range effective[1:] {
		if treeResolvedParentID(n, tree) != parentID {
			return nil, 0, 0, reorderReasonDifferentParents
		}
	}

	siblings = siblingsFromTree(tree, parentID)
	if len(siblings) == 0 {
		return nil, 0, 0, reorderReasonNoSiblings
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
		// Defensive: every effective item is drawn from the tree and shares the
		// resolved parent, so it should appear among that parent's children.
		return nil, 0, 0, reorderReasonNotAmongSiblings
	}

	// Indices come out sorted because we iterate siblings in order. Check
	// contiguity: last - first + 1 == count.
	startIdx = indices[0]
	endIdx = indices[len(indices)-1]
	if endIdx-startIdx+1 != len(indices) {
		return nil, 0, 0, reorderReasonNotContiguous
	}

	return siblings, startIdx, endIdx, ""
}

// treeResolvedParentID applies internal/graph's resolvedParentID rule at the
// presentation layer, deciding the sibling set a nib actually reorders within.
// The TUI holds no NibReader at this point — only a tree already fetched from
// the backend — so it answers "does this parent link resolve" by tree
// membership. graph.resolvedParentID is the canonical rule and names the
// surfaces that depend on it; keep the two in agreement.
//
// Membership is equivalent to resolution only while the tree is built from the
// full, unfiltered nib set (see the loadNibs fetch that feeds ui.BuildTree).
// Under a filtered set a hidden parent would be indistinguishable from one
// that does not exist, and its children would be offered for reorder among
// unrelated roots.
func treeResolvedParentID(n *nib.Nib, tree []*ui.TreeNode) string {
	if n.Parent == "" || ui.FindNode(tree, n.Parent) == nil {
		return ""
	}
	return n.Parent
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
