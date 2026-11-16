package tui

import (
	"slices"

	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/viewshape"
)

// statusKind selects how the footer colors a status message.
type statusKind int

const (
	statusOK statusKind = iota
	statusWarn
)

// Reasons a reorder is refused, shown verbatim in the list footer.
const (
	reorderReasonNothingSelected   = "Nothing to move"
	reorderReasonDifferentParents  = "Can't move: selected nibs have different parents"
	reorderReasonDifferentGroups   = "Can't move: selected nibs are ordered in different groups"
	reorderReasonDifferentSections = "Can't move: selected nibs are in different sections"
	reorderReasonNotContiguous     = "Can't move: select nibs that are next to each other"
	reorderReasonAtTop             = "Already at the top"
	reorderReasonAtBottom          = "Already at the bottom"
	reorderReasonOtherGroup        = "Can't move past a nib that is ordered in another group"
	reorderReasonOwnDescendant     = "Can't place a nib above its own descendant"
	reorderReasonInParentCycle     = "Can't move: that nib is in a parent cycle — run nibs check"
	reorderReasonFlat              = "Can't move in Flat: switch to Tree or Milestones to reorder"
	reorderReasonAreas             = "Can't move in Areas: switch to Tree or Milestones to reorder"
	reorderReasonSearch            = "Can't move while the filter searches text: remove its words to reorder"
)

// moveDir is where a reorder moves rows within their ordering group.
type moveDir int

const (
	moveUp moveDir = iota
	moveDown
	moveTop
	moveBottom
)

// reorderWrite places the nib id right after afterID or right before beforeID.
type reorderWrite struct {
	id       string
	afterID  *string
	beforeID *string
}

func placeAfter(id, anchor string) reorderWrite  { return reorderWrite{id: id, afterID: &anchor} }
func placeBefore(id, anchor string) reorderWrite { return reorderWrite{id: id, beforeID: &anchor} }

// reorderPlan is the writes one reorder makes, in order, all in one scope.
type reorderPlan struct {
	writes []reorderWrite
	scope  model.OrderScope
}

// effectiveRows returns the indexes of the rows that show a marked nib and are
// nested under no such row, in row order. A row's descendants move with it.
func effectiveRows(rows []viewshape.Row, marked map[string]bool) []int {
	var out []int
	markedDepth := -1 // the depth of the marked row the current rows are nested under, or -1
	for i, r := range rows {
		if markedDepth >= 0 && r.Depth <= markedDepth {
			markedDepth = -1
		}
		if markedDepth < 0 && r.NibID != "" && marked[r.NibID] {
			out = append(out, i)
			markedDepth = r.Depth
		}
	}
	return out
}

// siblingRun returns the indexes of the rows at rows[i]'s depth that are
// nested under the same row as rows[i], in row order.
func siblingRun(rows []viewshape.Row, i int) []int {
	depth := rows[i].Depth
	start := i
	for start > 0 && rows[start-1].Depth >= depth {
		start--
	}
	var run []int
	for j := start; j < len(rows) && rows[j].Depth >= depth; j++ {
		if rows[j].Depth == depth {
			run = append(run, j)
		}
	}
	return run
}

// inParentCycle reports whether rows[i] shows a nib whose parent group's nib is
// itself or is nested beneath it, which happens only to the nib a parent cycle
// was broken at.
func inParentCycle(rows []viewshape.Row, i int) bool {
	g := rows[i].Group
	if g.Axis != viewshape.GroupParent || g.ID == "" {
		return false
	}
	if g.ID == rows[i].NibID {
		return true
	}
	for j := i + 1; j < len(rows) && rows[j].Depth > rows[i].Depth; j++ {
		if rows[j].NibID == g.ID {
			return true
		}
	}
	return false
}

// planReorder plans a move of the rows subjects in direction dir. rows are the
// rows viewshape.Shape returned for nibs with every row unfolded, so a folded
// row's descendants move with it and a folded sibling counts. unfiltered are
// the rows the same view shows with no filter. reason is empty when the move
// can be made, and otherwise says why it cannot.
//
// The subjects move as one block past the rows drawn beside them at their depth
// under the same row. Every row they pass must be in their ordering group, so
// the move writes within that group and passes no row of another one. A move to
// the top or bottom also passes the group's nibs the filter hides.
func planReorder(nibs []*nib.Nib, rows, unfiltered []viewshape.Row, subjects []int, dir moveDir) (plan reorderPlan, reason string) {
	if len(subjects) == 0 {
		return reorderPlan{}, reorderReasonNothingSelected
	}
	for _, s := range subjects {
		if inParentCycle(rows, s) {
			return reorderPlan{}, reorderReasonInParentCycle
		}
	}
	group := rows[subjects[0]].Group
	for _, s := range subjects[1:] {
		if rows[s].Group != group {
			if allParentAxis(rows, subjects) {
				return reorderPlan{}, reorderReasonDifferentParents
			}
			return reorderPlan{}, reorderReasonDifferentGroups
		}
	}

	run := siblingRun(rows, subjects[0])
	first := slices.Index(run, subjects[0])
	for k, s := range subjects {
		at := slices.Index(run, s)
		if at < 0 {
			return reorderPlan{}, reorderReasonDifferentSections
		}
		if at != first+k {
			return reorderPlan{}, reorderReasonNotContiguous
		}
	}
	last := first + len(subjects) - 1

	// A row that shows no nib heads a section, which ends the rows a move can
	// pass.
	above := run[:first]
	for k := len(above) - 1; k >= 0; k-- {
		if rows[above[k]].NibID == "" {
			above = above[k+1:]
			break
		}
	}
	below := run[last+1:]
	for k, j := range below {
		if rows[j].NibID == "" {
			below = below[:k]
			break
		}
	}

	var passed []int
	switch dir {
	case moveUp:
		if len(above) == 0 {
			return reorderPlan{}, reorderReasonAtTop
		}
		passed = above[len(above)-1:]
	case moveDown:
		if len(below) == 0 {
			return reorderPlan{}, reorderReasonAtBottom
		}
		passed = below[:1]
	case moveTop:
		passed = above
	case moveBottom:
		passed = below
	}
	for _, p := range passed {
		if rows[p].Group != group {
			return reorderPlan{}, reorderReasonOtherGroup
		}
		// Rows of one group in one run share a section, so StaysInGroup refuses
		// only a nib placed above its own descendant.
		for _, s := range subjects {
			rising, sinking := s, p
			if p > s {
				rising, sinking = p, s
			}
			if !viewshape.StaysInGroup(nibs, rows, rising, sinking) {
				return reorderPlan{}, reorderReasonOwnDescendant
			}
		}
	}

	ids := make([]string, len(subjects))
	for k, s := range subjects {
		ids[k] = rows[s].NibID
	}
	plan.scope = model.OrderScopeParent
	if group.Axis == viewshape.GroupMilestone {
		plan.scope = model.OrderScopeMilestone
	}
	switch dir {
	case moveUp:
		anchor := rows[passed[0]].NibID
		if len(ids) == 1 {
			plan.writes = []reorderWrite{placeBefore(ids[0], anchor)}
		} else {
			// A block moves one place by moving the one nib it passes to its
			// other end.
			plan.writes = []reorderWrite{placeAfter(anchor, ids[len(ids)-1])}
		}
	case moveDown:
		anchor := rows[passed[0]].NibID
		if len(ids) == 1 {
			plan.writes = []reorderWrite{placeAfter(ids[0], anchor)}
		} else {
			plan.writes = []reorderWrite{placeBefore(anchor, ids[0])}
		}
	case moveTop, moveBottom:
		// The unfiltered rows hold the group's hidden nibs, which the move passes
		// too, and the parent chains that StaysInGroup follows through them.
		members := groupMembers(unfiltered, ids[0], group)
		if dir == moveBottom {
			slices.Reverse(members)
		}
		lastSubject := -1
		for k, j := range members {
			if slices.Contains(ids, unfiltered[j].NibID) {
				lastSubject = k
			}
		}
		var passedAll, subjectRows []int
		for k, j := range members {
			if slices.Contains(ids, unfiltered[j].NibID) {
				subjectRows = append(subjectRows, j)
			} else if k < lastSubject {
				passedAll = append(passedAll, j)
			}
		}
		if len(passedAll) == 0 {
			if dir == moveTop {
				return reorderPlan{}, reorderReasonAtTop
			}
			return reorderPlan{}, reorderReasonAtBottom
		}
		for _, p := range passedAll {
			for _, s := range subjectRows {
				rising, sinking := s, p
				if dir == moveBottom {
					rising, sinking = p, s
				}
				if !viewshape.StaysInGroup(nibs, unfiltered, rising, sinking) {
					return reorderPlan{}, reorderReasonOwnDescendant
				}
			}
		}
		anchor := unfiltered[passedAll[0]].NibID
		if dir == moveTop {
			plan.writes = append(plan.writes, placeBefore(ids[0], anchor))
			for k := 1; k < len(ids); k++ {
				plan.writes = append(plan.writes, placeAfter(ids[k], ids[k-1]))
			}
		} else {
			plan.writes = append(plan.writes, placeAfter(ids[len(ids)-1], anchor))
			for k := len(ids) - 2; k >= 0; k-- {
				plan.writes = append(plan.writes, placeBefore(ids[k], ids[k+1]))
			}
		}
	}
	return plan, ""
}

// groupMembers returns the indexes of the rows in group that share a run with
// the row of id, in row order, or nil when no row shows id.
func groupMembers(rows []viewshape.Row, id string, group viewshape.Group) []int {
	i := slices.IndexFunc(rows, func(r viewshape.Row) bool { return r.NibID == id })
	if i < 0 {
		return nil
	}
	var members []int
	for _, j := range siblingRun(rows, i) {
		if rows[j].NibID != "" && rows[j].Group == group {
			members = append(members, j)
		}
	}
	return members
}

// allParentAxis reports whether every row of subjects is ordered among its
// siblings.
func allParentAxis(rows []viewshape.Row, subjects []int) bool {
	for _, s := range subjects {
		if rows[s].Group.Axis != viewshape.GroupParent {
			return false
		}
	}
	return true
}
