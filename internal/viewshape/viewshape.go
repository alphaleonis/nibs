// Package viewshape arranges nibs into the rows a list view shows: which nibs
// appear, in what order and at what depth, and which of them are dimmed context.
// It holds no TUI or GraphQL types.
//
// The web shapes its table in web/src/lib/tableData.ts. The cases in
// internal/viewshapecontract run against both, so a rule that a case exercises
// fails that case when it changes on one side only.
package viewshape

import (
	"fmt"
	"slices"

	"github.com/alphaleonis/nibs/internal/nib"
)

// Level is a view's arrangement, named as the web's `ViewLevel` names it.
type Level string

const (
	// LevelTree nests nibs under their parents. Milestones have no row, because
	// they sit outside the parent graph.
	LevelTree Level = "none"
	// LevelFlat lists every nib at depth 0.
	LevelFlat Level = "flat"
)

// Sort is the order Shape puts sibling rows in.
type Sort string

// SortOrder orders siblings by their order key, as nib.SortByOrder does. It is
// the order the web's table requests from the server and then keeps.
const SortOrder Sort = "order"

// GroupAxis names the ordering key a reorder moves a row along, as the server's
// `OrderScope` names it in lower case.
type GroupAxis string

// GroupParent is the `order` key among the children of one resolved parent.
const GroupParent GroupAxis = "parent"

// Group is the ordering group a reorder of a row stays within. The zero Group
// means that no reorder can address the row.
type Group struct {
	Axis GroupAxis
	// ID names the group within its axis. For GroupParent it is the resolved
	// parent's id, or "" for the root group.
	ID string
}

// Row is one row of a shaped view.
type Row struct {
	// NibID is the nib the row shows, or "" for a section row that no nib heads.
	NibID string
	Depth int
	// Section is the key of the section the row heads or belongs to, or "" in a
	// view without sections.
	Section string
	// Header reports whether the row heads Section.
	Header bool
	// Dimmed reports whether the row is shown only as an ancestor of a match.
	Dimmed bool
	// HasChildren reports whether any row is shown beneath this one when it is
	// expanded. Collapsing a row does not change it.
	HasChildren bool
	Group       Group
}

// View is what Shape needs to know about a view besides its nibs.
type View struct {
	Level  Level
	Filter Filter
	// Collapsed holds the ids of the rows whose descendants are hidden.
	Collapsed map[string]bool
	Sort      Sort
}

// Shape returns the rows v shows for nibs, in display order.
//
// A nib's parent link counts only when it names a nib in nibs by its exact id.
// Shape returns an error for a Level or Sort it does not implement.
func Shape(nibs []*nib.Nib, v View) ([]Row, error) {
	if v.Sort != SortOrder {
		return nil, fmt.Errorf("viewshape: sort %q is not supported", v.Sort)
	}
	ordered := slices.Clone(nibs)
	nib.SortByOrder(ordered)

	byID := make(map[string]*nib.Nib, len(ordered))
	matched := make(map[string]bool, len(ordered))
	for _, b := range ordered {
		byID[b.ID] = b
		if Matches(b, v.Filter) {
			matched[b.ID] = true
		}
	}

	switch v.Level {
	case LevelTree:
		return shapeTree(ordered, byID, matched, v.Collapsed), nil
	case LevelFlat:
		return shapeFlat(ordered, byID, matched), nil
	default:
		return nil, fmt.Errorf("viewshape: level %q is not supported", v.Level)
	}
}

// parentGroup returns the parent group of b. It follows the stored parent link
// even where the view does not nest b under that parent.
func parentGroup(b *nib.Nib, byID map[string]*nib.Nib) Group {
	parent := ""
	if _, ok := byID[b.Parent]; ok {
		parent = b.Parent
	}
	return Group{Axis: GroupParent, ID: parent}
}

func shapeFlat(ordered []*nib.Nib, byID map[string]*nib.Nib, matched map[string]bool) []Row {
	var rows []Row
	for _, b := range ordered {
		// Flat keeps matches only, because an ancestor at depth 0 shows no path
		// to its match.
		if matched[b.ID] {
			rows = append(rows, Row{NibID: b.ID, Group: parentGroup(b, byID)})
		}
	}
	return rows
}

func shapeTree(ordered []*nib.Nib, byID map[string]*nib.Nib, matched map[string]bool, collapsed map[string]bool) []Row {
	visible := withAncestors(matched, byID)

	inTree := make(map[string]*nib.Nib, len(ordered))
	var treeNibs []*nib.Nib
	for _, b := range ordered {
		if b.EffectiveType() != "milestone" {
			inTree[b.ID] = b
			treeNibs = append(treeNibs, b)
		}
	}

	promoted := PromotedCycleRoots(inTree)
	children := make(map[string][]*nib.Nib)
	var roots []*nib.Nib
	for _, b := range treeNibs {
		// A nib parented to a milestone is a root, because the milestone has no
		// row to nest it under.
		if _, ok := inTree[b.Parent]; ok && !promoted[b.ID] {
			children[b.Parent] = append(children[b.Parent], b)
		} else {
			roots = append(roots, b)
		}
	}

	var rows []Row
	var walk func(level []*nib.Nib, depth int)
	walk = func(level []*nib.Nib, depth int) {
		for _, b := range level {
			if !visible[b.ID] {
				continue
			}
			rows = append(rows, Row{
				NibID:       b.ID,
				Depth:       depth,
				Dimmed:      !matched[b.ID],
				HasChildren: slices.ContainsFunc(children[b.ID], func(c *nib.Nib) bool { return visible[c.ID] }),
				Group:       parentGroup(b, byID),
			})
			if !collapsed[b.ID] {
				walk(children[b.ID], depth+1)
			}
		}
	}
	walk(roots, 0)
	return rows
}

// withAncestors returns the matched ids plus every ancestor of a match, so a
// nested view keeps the path to each match.
func withAncestors(matched map[string]bool, byID map[string]*nib.Nib) map[string]bool {
	visible := make(map[string]bool, len(matched))
	for id := range matched {
		visible[id] = true
		seen := map[string]bool{id: true}
		for cur := byID[id]; ; {
			parent, ok := byID[cur.Parent]
			if !ok || seen[parent.ID] {
				break
			}
			seen[parent.ID] = true
			visible[parent.ID] = true
			cur = parent
		}
	}
	return visible
}
