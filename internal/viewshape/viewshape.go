// Package viewshape arranges nibs into the rows a list view shows: which nibs
// appear, in what order and at what depth, and which of them are dimmed context.
// It holds no TUI or GraphQL types.
//
// The web shapes its table in web/src/lib/tableData.ts and lays out its grouped
// views with the lenses in web/src/lib/viewSpine.ts. The cases in
// internal/viewshapecontract run against both, so a rule that a case exercises
// fails that case when it changes on one side only.
package viewshape

import (
	"fmt"
	"slices"

	"github.com/alphaleonis/nibs/internal/area"
	"github.com/alphaleonis/nibs/internal/membership"
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
	// LevelMilestones gives each milestone a section holding its queue, and puts
	// the rest in the Backlog section.
	LevelMilestones Level = "milestones"
	// LevelAreas gives each declared area a section, and puts the rest in the No
	// area section.
	LevelAreas Level = "areas"
)

// Sort is the order Shape puts sibling rows in.
type Sort string

// SortOrder orders siblings by their order key, as nib.SortByOrder does. It is
// the order the web's table requests from the server and then keeps.
const SortOrder Sort = "order"

// GroupAxis names the ordering key a reorder moves a row along, as the server's
// `OrderScope` names it in lower case.
type GroupAxis string

const (
	// GroupParent is the `order` key among the children of one resolved parent.
	GroupParent GroupAxis = "parent"
	// GroupMilestone is the `milestoneOrder` key among the nibs directly in one
	// milestone's queue.
	GroupMilestone GroupAxis = "milestone"
)

// Group is the ordering group a reorder of a row stays within. The zero Group
// means that no reorder can address the row.
type Group struct {
	Axis GroupAxis
	// ID names the group within its axis. For GroupParent it is the resolved
	// parent's id, or "" for the root group. For GroupMilestone it is the
	// milestone's id.
	ID string
}

// Row is one row of a shaped view.
type Row struct {
	// NibID is the nib the row shows, or "" for a section row that no nib heads.
	NibID string
	Depth int
	// Section is the key of the section the row heads or belongs to, or "" in a
	// view without sections. A section a nib heads is keyed by that nib's id, a
	// declared area by its path, and a leftover by BacklogSection or
	// NoAreaSection.
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
	// Match is a test a nib must also pass to match, beside Filter, or nil to
	// add none.
	Match func(*nib.Nib) bool
	// Collapsed holds the ids of the nibs whose rows hide their descendants.
	Collapsed map[string]bool
	// CollapsedSections holds the keys of the sections no nib heads whose rows
	// hide their contents.
	CollapsedSections map[string]bool
	Sort              Sort
	// Areas is the declared vocabulary LevelAreas draws its sections from.
	Areas *area.Vocabulary
}

// Matches reports whether n matches v: it passes v.Filter and v.Match. A row
// whose nib does not match is shown only as context, or not at all.
func (v View) Matches(n *nib.Nib) bool {
	return Matches(n, v.Filter) && (v.Match == nil || v.Match(n))
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
		if v.Matches(b) {
			matched[b.ID] = true
		}
	}

	var roots []*node
	// Flat keeps matches only, because an ancestor at depth 0 shows no path to
	// its match.
	visible := matched
	switch v.Level {
	case LevelFlat:
		for _, b := range ordered {
			roots = append(roots, &node{nib: b})
		}
	case LevelTree:
		var inTree []*nib.Nib
		for _, b := range ordered {
			// A nib parented to a milestone becomes a root, because the milestone
			// has no row to nest it under.
			if b.EffectiveType() != "milestone" {
				inTree = append(inTree, b)
			}
		}
		roots = buildForest(inTree)
		visible = withAncestors(matched, byID)
	default:
		l, ok := lenses[v.Level]
		if !ok {
			return nil, fmt.Errorf("viewshape: level %q is not supported", v.Level)
		}
		var err error
		roots, err = shapeGrouped(ordered, l, &shapeInput{membership: membership.Compute(ordered), areas: v.Areas})
		if err != nil {
			return nil, err
		}
		visible = withAncestors(matched, byID)
	}

	shown := make(map[*node]bool)
	markShown(roots, visible, shown)
	f := flattener{byID: byID, matched: matched, shown: shown, view: v}
	f.flatten(roots, 0, "", Group{})
	return f.rows, nil
}

// node is one row of a view before filtering and collapse.
type node struct {
	// nib is nil for the row of a section no nib heads.
	nib *nib.Nib
	// heads is the key of the section this row heads, or "".
	heads string
	// persists keeps a section's row when nothing in it is shown.
	persists bool
	// memberGroup is the ordering group of the rows directly beneath this one,
	// or the zero Group to leave each in its parent group.
	memberGroup Group
	children    []*node
}

// buildForest nests nibs under their parents within nibs, keeping their order.
// The lowest id of each parent cycle becomes a root (see PromotedCycleRoots).
func buildForest(nibs []*nib.Nib) []*node {
	set := make(map[string]*nib.Nib, len(nibs))
	nodes := make(map[string]*node, len(nibs))
	for _, b := range nibs {
		set[b.ID] = b
		nodes[b.ID] = &node{nib: b}
	}
	promoted := PromotedCycleRoots(set)
	var roots []*node
	for _, b := range nibs {
		if parent, ok := nodes[b.Parent]; ok && !promoted[b.ID] {
			parent.children = append(parent.children, nodes[b.ID])
		} else {
			roots = append(roots, nodes[b.ID])
		}
	}
	return roots
}

// markShown records in shown whether each node is shown: its nib is visible,
// its section persists, or something beneath it is shown. It returns whether
// any of nodes is shown.
func markShown(nodes []*node, visible map[string]bool, shown map[*node]bool) bool {
	anyShown := false
	for _, n := range nodes {
		below := markShown(n.children, visible, shown)
		self := below || n.persists || (n.nib != nil && visible[n.nib.ID])
		shown[n] = self
		anyShown = anyShown || self
	}
	return anyShown
}

type flattener struct {
	byID    map[string]*nib.Nib
	matched map[string]bool
	shown   map[*node]bool
	view    View
	rows    []Row
}

// flatten appends the shown rows of nodes. enclosing is the section they are in
// and group the ordering group their section declares for them.
func (f *flattener) flatten(nodes []*node, depth int, enclosing string, group Group) {
	for _, n := range nodes {
		if !f.shown[n] {
			continue
		}
		r := Row{
			Depth:       depth,
			Section:     enclosing,
			HasChildren: slices.ContainsFunc(n.children, func(c *node) bool { return f.shown[c] }),
		}
		if n.heads != "" {
			r.Section = n.heads
			r.Header = true
		}
		collapsed := f.view.CollapsedSections[n.heads]
		if n.nib != nil {
			r.NibID = n.nib.ID
			r.Dimmed = !f.matched[n.nib.ID]
			r.Group = group
			if r.Group == (Group{}) {
				r.Group = parentGroup(n.nib, f.byID)
			}
			collapsed = f.view.Collapsed[n.nib.ID]
		}
		f.rows = append(f.rows, r)
		if !collapsed {
			f.flatten(n.children, depth+1, r.Section, n.memberGroup)
		}
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

// StaysInGroup reports whether moving rows[moved] to the position of
// rows[target] keeps the moved nib in its ordering group and in the section it
// is drawn in. rows are the rows Shape returned for nibs. The shared cases
// compare it with the web's planDrop for a drop before the target.
//
// A target that descends from the moved nib through a chain of shown rows does
// not qualify, because the move would put a nib among its own descendants.
func StaysInGroup(nibs []*nib.Nib, rows []Row, moved, target int) bool {
	m, t := rows[moved], rows[target]
	if m.NibID == "" || t.NibID == "" || m.NibID == t.NibID ||
		m.Group == (Group{}) || m.Group != t.Group ||
		memberOf(rows, moved) != memberOf(rows, target) {
		return false
	}
	return !descends(nibs, rows, t.NibID, m.NibID)
}

// descends reports whether nib id descends from ancestor through a chain of
// nibs that each have a row in rows.
func descends(nibs []*nib.Nib, rows []Row, id, ancestor string) bool {
	byID := make(map[string]*nib.Nib, len(nibs))
	for _, b := range nibs {
		byID[b.ID] = b
	}
	shown := make(map[string]bool, len(rows))
	for _, r := range rows {
		if r.NibID != "" {
			shown[r.NibID] = true
		}
	}
	seen := make(map[string]bool)
	for cur := id; shown[cur] && !seen[cur]; {
		seen[cur] = true
		parent, ok := byID[byID[cur].Parent]
		if !ok {
			return false
		}
		if parent.ID == ancestor {
			return true
		}
		cur = parent.ID
	}
	return false
}

// memberOf returns the section rows[i] is drawn in as a member, or "". A header
// row is a member of the section its nearest shallower row heads or is in.
func memberOf(rows []Row, i int) string {
	if !rows[i].Header {
		return rows[i].Section
	}
	for j := i - 1; j >= 0; j-- {
		if rows[j].Depth < rows[i].Depth {
			return rows[j].Section
		}
	}
	return ""
}
