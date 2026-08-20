// Package membership is the one answer to "what belongs to container X". The
// roadmap, the context summaries and the projection rollups used to derive
// container membership through rival ad-hoc parent walks — raw-keyed children
// maps, a two-level milestone walk, per-nib store scans — that disagreed on
// depth, on dangling links and on illegal nests. This package holds the single
// definition; consumers get SETS and keep every display policy (filtering,
// sorting, progress arithmetic, queue order) to themselves.
//
// Everything is pure and total: Compute takes any slice — including
// invariant-violating data — and produces a deterministic View. Parent links
// resolve against the slice itself, mirroring graph.resolvedParentID's rule: a
// link naming no nib in the slice is no parent at all, so the nib is a root.
// Every accessor answers in input-slice order, never map-iteration order.
//
// Live-pointer discipline, stated once for the package: a View pins the
// *nib.Nib pointers it was built over. It is a point-in-time value — build it
// once per command or per GraphQL operation, and never cache it across
// operations, because the store installs fresh pointers on every write.
//
// The package sits below nibcontext, graph and cmd, and imports only
// internal/nib — a consumer wanting a seam declares its own.
package membership

import "github.com/alphaleonis/nibs/internal/nib"

// Lookup resolves a nib id, returning nil for an id that names no nib. It is
// how ResolvedMilestoneID reads the store without this package importing one —
// callers pass a NibReader.Get closure or a snapshot map.
type Lookup func(id string) *nib.Nib

// ResolvedMilestoneID is THE step-1 definition of "directly assigned to a
// milestone": b's resolved parent when that parent is milestone-typed, ""
// otherwise — including when the parent link dangles, mirroring the
// resolved-parent rule. The ordering engine's milestone scope consumes this
// via a Lookup closure; the three-axis v2 release swaps this one body to read
// the `milestone:` field, and every caller survives unchanged.
func ResolvedMilestoneID(b *nib.Nib, lookup Lookup) string {
	if b.Parent == "" {
		return ""
	}
	parent := lookup(b.Parent)
	if parent == nil || parent.EffectiveType() != "milestone" {
		return ""
	}
	return parent.ID
}

// View is a point-in-time membership index over one slice of nibs. See the
// package comment for the pointer discipline; see Compute for the build.
type View struct {
	byID map[string]*nib.Nib
	// children is the structural parent axis: resolved parent id → children in
	// input order. "" holds the roots (no parent, or a dangling link).
	children   map[string][]*nib.Nib
	milestones []*nib.Nib
	all        []*nib.Nib
}

// Compute builds a View in O(N): an index pass, then a resolution pass that
// files every nib under its resolved parent. The transitive accessors walk
// the resulting adjacency with visited sets, so cyclic parent links (illegal
// data) terminate instead of recursing forever.
func Compute(all []*nib.Nib) *View {
	v := &View{
		byID:     make(map[string]*nib.Nib, len(all)),
		children: make(map[string][]*nib.Nib),
		all:      all,
	}
	for _, b := range all {
		v.byID[b.ID] = b
	}
	for _, b := range all {
		parentID := ""
		if b.Parent != "" {
			if p := v.byID[b.Parent]; p != nil {
				parentID = p.ID
			}
		}
		v.children[parentID] = append(v.children[parentID], b)
		if b.EffectiveType() == "milestone" {
			v.milestones = append(v.milestones, b)
		}
	}
	return v
}

// Milestones returns the milestone-typed nibs in input order.
func (v *View) Milestones() []*nib.Nib {
	return copyNibs(v.milestones)
}

// Children is the structural parent axis: the nibs whose resolved parent is
// containerID, in input order — every type, containers included. "" names the
// root set. This axis stays what it is through the step-2 cutover (the
// projected childCount answers from it), while DirectMembers/Members move to
// the assignment axis.
func (v *View) Children(containerID string) []*nib.Nib {
	return copyNibs(v.children[containerID])
}

// DirectMembers returns the work directly belonging to the container, in input
// order: its resolved-parent children, minus milestone-typed nibs — a
// milestone is a container of its own and is never a member of anything (an
// illegal milestone nest keeps its subtree in its own queue).
func (v *View) DirectMembers(containerID string) []*nib.Nib {
	var members []*nib.Nib
	for _, b := range v.children[containerID] {
		if b.EffectiveType() == "milestone" {
			continue
		}
		members = append(members, b)
	}
	return members
}

// Members returns the container's transitive membership closure, single-
// counted, in breadth-first input order. The closure is FULL depth — a task
// under a feature under an epic under a milestone is a member — and does not
// descend through a milestone-typed child (see DirectMembers).
func (v *View) Members(containerID string) []*nib.Nib {
	var result []*nib.Nib
	visited := make(map[string]bool)
	queue := v.DirectMembers(containerID)
	for len(queue) > 0 {
		b := queue[0]
		queue = queue[1:]
		if visited[b.ID] {
			continue
		}
		visited[b.ID] = true
		result = append(result, b)
		queue = append(queue, v.DirectMembers(b.ID)...)
	}
	return result
}

// MilestoneOf returns the id of the milestone the nib transitively belongs to,
// or "" for unscheduled work — walking up the resolved parent chain to the
// nearest milestone-typed ancestor. A milestone belongs to no milestone, and
// an unknown id is unscheduled.
func (v *View) MilestoneOf(id string) string {
	visited := make(map[string]bool)
	b := v.byID[id]
	for b != nil && !visited[b.ID] {
		visited[b.ID] = true
		if b.Parent == "" {
			return ""
		}
		p := v.byID[b.Parent]
		if p == nil {
			return ""
		}
		if p.EffectiveType() == "milestone" {
			return p.ID
		}
		b = p
	}
	return ""
}

// EpicGroup is one epic with its direct member items.
type EpicGroup struct {
	Epic  *nib.Nib
	Items []*nib.Nib
}

// Tree is a milestone's membership in the roadmap's shape: the epic-typed
// direct members each with their items, and the remaining direct members.
// Sets only — filtering, sorting and progress are the consumer's policy.
type Tree struct {
	Epics []EpicGroup
	Other []*nib.Nib
}

// Grouped returns the milestone's Tree.
func (v *View) Grouped(milestoneID string) Tree {
	var tree Tree
	for _, b := range v.DirectMembers(milestoneID) {
		if b.EffectiveType() == "epic" {
			tree.Epics = append(tree.Epics, EpicGroup{Epic: b, Items: v.DirectMembers(b.ID)})
			continue
		}
		tree.Other = append(tree.Other, b)
	}
	return tree
}

// Remainder is the backlog outside every milestone: the epics that belong to
// no milestone (each with their items), and the root-level work items. Sets
// only, in input order.
type Remainder struct {
	Epics []EpicGroup
	Other []*nib.Nib
}

// Unscheduled returns the Remainder. Roots are the RESOLVED reading — a nib
// whose parent link names no nib is a root here, exactly as every query
// surface reports it, where the old raw `Parent != ""` orphan scan hid it.
// The remainder is computed against every declared milestone regardless of
// status: work under a status-hidden milestone is scheduled work, not backlog
// — a consumer wanting the old leak back has to build it deliberately.
func (v *View) Unscheduled() Remainder {
	var rem Remainder
	for _, b := range v.all {
		switch b.EffectiveType() {
		case "milestone":
		case "epic":
			if v.MilestoneOf(b.ID) == "" {
				rem.Epics = append(rem.Epics, EpicGroup{Epic: b, Items: v.DirectMembers(b.ID)})
			}
		default:
			if v.isRoot(b) {
				rem.Other = append(rem.Other, b)
			}
		}
	}
	return rem
}

// isRoot reports whether b's resolved parent is the root group.
func (v *View) isRoot(b *nib.Nib) bool {
	if b.Parent == "" {
		return true
	}
	return v.byID[b.Parent] == nil
}

// copyNibs returns a fresh slice over the same pointers, so a consumer sorting
// its result in place cannot reorder the View's own adjacency.
func copyNibs(nibs []*nib.Nib) []*nib.Nib {
	if nibs == nil {
		return nil
	}
	out := make([]*nib.Nib, len(nibs))
	copy(out, nibs)
	return out
}
