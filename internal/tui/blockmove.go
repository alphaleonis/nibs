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
// UI can produce reaches them. The one shape that would — a nib promoted out of
// a parent cycle, whose stored parent link outlives the tree edge BuildTree
// severed — is caught by inParentCycle and refused with
// reorderReasonInParentCycle, which names that cause instead. The three are
// kept because they guard the assumption rather than restate it — reaching one
// means the tree and the sibling lookup have diverged, which is worth saying
// out loud instead of moving nothing.
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

	// A nib promoted out of a parent cycle reorders within no sibling list at
	// all. The scan runs ahead of both the parent-scope check and the sibling
	// lookup below, so it decides the message for a mixed selection too: with
	// such a nib marked alongside an ordinary root, the cycle is the cause and
	// "different parents" only restates the symptom the severed edge produced.
	for _, n := range effective {
		if inParentCycle(n, tree) {
			return nil, 0, 0, reorderReasonInParentCycle
		}
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
		// Defensive: a promoted cycle root is refused above with its own reason,
		// so reaching this point means the tree and the sibling lookup disagree
		// for some other cause. Refusing is correct either way — a reorder is
		// only meaningful within one parent's sibling list, and this selection is
		// not in one.
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

// treeResolvedParentID applies internal/graph's resolved-parent rule at the
// presentation layer, deciding the sibling set a nib actually reorders within.
// The TUI holds no NibReader at this point — only a tree already fetched from
// the backend — so it answers "does this parent link resolve" by tree
// membership. This is a re-derivation of graph.resolvedParent, which is the
// canonical rule; keep the two in agreement.
//
// Membership is equivalent to resolution only while the tree is built from the
// full, unfiltered nib set (see the loadNibs fetch that feeds ui.BuildTree).
// Under a filtered set a hidden parent would be indistinguishable from one
// that does not exist, and its children would be offered for reorder among
// unrelated roots.
//
// For a nib promoted out of a parent cycle this answers with a parent whose
// children no longer hold the nib, because BuildTree severed that edge. This
// function does not detect that case itself; each caller that turns the answer
// into a user-facing refusal consults inParentCycle so it can name the cycle
// rather than describe the sibling list — blockMovable ahead of resolving the
// parent scope, singleReorderCmd after its index scan comes up empty. A caller
// that only needs the sibling slice (findSiblings) does not ask at all.
func treeResolvedParentID(n *nib.Nib, tree []*ui.TreeNode) string {
	if n.Parent == "" || ui.FindNode(tree, n.Parent) == nil {
		return ""
	}
	return n.Parent
}

// inParentCycle reports whether n is the member of a parent cycle that
// ui.BuildTree promoted to a root. Such a nib keeps its stored parent link
// while the tree edge that link would draw is severed, so it renders at root
// level and belongs to no parent's sibling list — the one nib for which the
// tree offers a reorder scope that does not exist.
//
// The tell is that the stored parent lies inside n's own subtree: BuildTree
// nests the rest of the cycle underneath the promoted member, so n.Parent is n
// itself (a nib parented to itself) or one of its descendants. Every tree edge
// below n is a stored parent link, so following them from n.Parent back up
// reaches n — that is the cycle, read off the rendered tree rather than assumed
// from it.
//
// A cycle member that was NOT promoted keeps its edge and sits among its real
// parent's children, where a reorder is meaningful; it is deliberately not
// caught here.
//
// This reads the same under the filtered tree loadNibs builds, and not by
// accident: BuildTree's addAncestors closes the built set upward, so a cycle is
// never partially present — any member that enters drags the rest in along its
// parent chain, whether it matched the filter or came in as ancestor context.
// That is a narrower guarantee than the one treeResolvedParentID relies on
// above: outside a cycle, a filtered-out parent is still indistinguishable from
// one that does not exist.
//
// Two BuildTree properties are load-bearing here: exactly one member per cycle
// is promoted, and promotion severs the tree edge while leaving the stored
// Nib.Parent intact. Change either and this detection answers false, sending
// the refusal back to describing a sibling list instead of naming the cycle.
func inParentCycle(n *nib.Nib, tree []*ui.TreeNode) bool {
	if n == nil || n.Parent == "" {
		return false
	}
	node := ui.FindNode(tree, n.ID)
	if node == nil {
		return false
	}
	// Search n's subtree with n included, so a nib parented to itself — the
	// degenerate one-member cycle — is recognized too.
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
