package tui

import (
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/ui"
)

// effectiveSelection returns the marked nibs that have no marked ancestor, in
// tree order. A marked nib's descendants move with it.
func effectiveSelection(marked map[string]bool, tree []*ui.TreeNode) []*nib.Nib {
	var out []*nib.Nib
	walkEffective(tree, marked, false, &out)
	return out
}

// walkEffective appends to out every marked node that has no marked ancestor.
func walkEffective(nodes []*ui.TreeNode, marked map[string]bool, ancestorMarked bool, out *[]*nib.Nib) {
	for _, node := range nodes {
		isMarked := marked[node.Nib.ID]
		if isMarked && !ancestorMarked {
			*out = append(*out, node.Nib)
		}
		walkEffective(node.Children, marked, ancestorMarked || isMarked, out)
	}
}

// statusKind selects how the footer colors a status message.
type statusKind int

const (
	statusOK statusKind = iota
	statusWarn
)

// Reasons a reorder is refused, shown verbatim in the list footer.
//
// The last three are defensive: reaching one means the tree and the sibling
// lookup disagree. A nib promoted out of a parent cycle is refused with
// reorderReasonInParentCycle instead.
const (
	reorderReasonNothingSelected  = "Nothing to move"
	reorderReasonDifferentParents = "Can't move: selected nibs have different parents"
	reorderReasonNotContiguous    = "Can't move: select nibs that are next to each other"
	reorderReasonAtTop            = "Already at the top"
	reorderReasonAtBottom         = "Already at the bottom"
	reorderReasonInParentCycle    = "Can't move: that nib is in a parent cycle — run nibs check"

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

	// Ahead of the parent-scope check, so a mixed selection names the cycle
	// rather than "different parents".
	for _, n := range effective {
		if inParentCycle(n, tree) {
			return nil, 0, 0, reorderReasonInParentCycle
		}
	}

	parentID := treeResolvedParentID(effective[0], tree)
	for _, n := range effective[1:] {
		if treeResolvedParentID(n, tree) != parentID {
			return nil, 0, 0, reorderReasonDifferentParents
		}
	}

	siblings = siblingsFromTree(tree, parentID)
	if len(siblings) == 0 {
		return nil, 0, 0, reorderReasonNoSiblings
	}

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
		return nil, 0, 0, reorderReasonNotAmongSiblings
	}

	// indices is ascending: siblings are scanned in order.
	startIdx = indices[0]
	endIdx = indices[len(indices)-1]
	if endIdx-startIdx+1 != len(indices) {
		return nil, 0, 0, reorderReasonNotContiguous
	}

	return siblings, startIdx, endIdx, ""
}

// treeResolvedParentID returns n's parent ID when that parent is in the tree,
// otherwise "" for the root scope. It mirrors graph.resolvedParent using tree
// membership; keep the two in agreement.
//
// Membership matches resolution because ui.BuildTree adds every ancestor from
// the unfiltered nib set loadNibs passes it.
//
// For a nib promoted out of a parent cycle this returns a parent whose children
// no longer hold the nib. Callers that report a refusal consult inParentCycle to
// name the cycle.
func treeResolvedParentID(n *nib.Nib, tree []*ui.TreeNode) string {
	if n.Parent == "" || ui.FindNode(tree, n.Parent) == nil {
		return ""
	}
	return n.Parent
}

// inParentCycle reports whether n is the member of a parent cycle that
// ui.BuildTree promoted to a root. Such a nib keeps its stored parent but belongs
// to no parent's sibling list.
//
// The tell is that n.Parent lies inside n's own subtree. A cycle member that was
// not promoted keeps its tree edge and is not reported. The tree properties this
// relies on are stated on ui.BuildTree.
func inParentCycle(n *nib.Nib, tree []*ui.TreeNode) bool {
	if n == nil || n.Parent == "" {
		return false
	}
	node := ui.FindNode(tree, n.ID)
	if node == nil {
		return false
	}
	// The subtree includes n, so a nib parented to itself is caught.
	return ui.FindNode([]*ui.TreeNode{node}, n.Parent) != nil
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
