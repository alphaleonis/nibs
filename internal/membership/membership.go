// Package membership is the one answer to "what belongs to container X". The
// roadmap, the context summaries and the projection rollups used to derive
// container membership through rival ad-hoc parent walks — raw-keyed children
// maps, a two-level milestone walk, per-nib store scans — that disagreed on
// depth, on dangling links and on illegal nests. This package holds the single
// definition; consumers get SETS and keep every display policy (filtering,
// sorting, progress arithmetic, queue order) to themselves.
//
// Two axes feed the definition. The structural parent axis (`parent:`) is
// decomposition: epic → feature → task. The assignment axis (`milestone:`)
// is scheduling: it alone decides what belongs to a milestone. A container's
// membership is its assignees plus their structural subtrees; the parent edge
// on its own never schedules anything.
//
// Everything is pure and total: Compute takes any slice — including
// invariant-violating data — and produces a deterministic View. Links resolve
// against the slice itself, mirroring graph.resolvedParentID's rule: a link
// naming no nib in the slice is no link at all, so a dangling parent makes a
// root and a dangling assignment schedules nothing. Every accessor answers in
// input-slice order, never map-iteration order.
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

// ResolvedMilestoneID is THE definition of "directly assigned to a
// milestone": the target of b's `milestone:` field when that target exists
// and is milestone-typed, "" otherwise — the same dangling-link rule the
// resolved parent gives, so hand-edited garbage (a dangling id, an assignment
// naming a non-milestone) stays out of every view. The structural parent axis
// confers no membership at all. The ordering engine's milestone scope consumes
// this via a Lookup closure.
func ResolvedMilestoneID(b *nib.Nib, lookup Lookup) string {
	if b.Milestone == "" {
		return ""
	}
	target := lookup(b.Milestone)
	if target == nil || target.EffectiveType() != "milestone" {
		return ""
	}
	return target.ID
}

// View is a point-in-time membership index over one slice of nibs. See the
// package comment for the pointer discipline; see Compute for the build.
type View struct {
	byID map[string]*nib.Nib
	// children is the structural parent axis: resolved parent id → children in
	// input order. "" holds the roots (no parent, or a dangling link).
	children map[string][]*nib.Nib
	// assigned is the assignment axis: resolved milestone id
	// (ResolvedMilestoneID) → assignees in input order. "" holds the
	// unassigned.
	assigned   map[string][]*nib.Nib
	milestones []*nib.Nib
	all        []*nib.Nib
}

// Compute builds a View in O(N): an index pass, then a resolution pass that
// files every nib under its resolved parent and its resolved assignment. The
// transitive accessors walk the resulting adjacencies with visited sets, so
// cyclic parent links (illegal data) terminate instead of recursing forever.
func Compute(all []*nib.Nib) *View {
	v := &View{
		byID:     make(map[string]*nib.Nib, len(all)),
		children: make(map[string][]*nib.Nib),
		assigned: make(map[string][]*nib.Nib),
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
		msID := ResolvedMilestoneID(b, v.lookup)
		v.assigned[msID] = append(v.assigned[msID], b)
		if b.EffectiveType() == "milestone" {
			v.milestones = append(v.milestones, b)
		}
	}
	return v
}

// lookup is the View's own Lookup over its slice.
func (v *View) lookup(id string) *nib.Nib {
	return v.byID[id]
}

// Milestones returns the milestone-typed nibs in input order.
func (v *View) Milestones() []*nib.Nib {
	return copyNibs(v.milestones)
}

// Children is the structural parent axis: the nibs whose resolved parent is
// containerID, in input order — every type, containers included. "" names the
// root set. Deliberately NOT the assignment axis: the projected childCount
// answers "how many nibs name this one as parent" from here, so a milestone
// honestly reports no children while DirectMembers carries its assignees.
func (v *View) Children(containerID string) []*nib.Nib {
	return copyNibs(v.children[containerID])
}

// DirectMembers returns the work directly belonging to the container, in input
// order. For a milestone that is its ASSIGNEES — the nibs whose `milestone:`
// field resolves to it; for any other container it is the structural children
// (its decomposition). Milestone-typed nibs are excluded on both axes — a
// milestone is a container of its own and is never a member of anything (an
// illegal milestone nest keeps its subtree in its own queue, and a
// hand-authored assignment on a container schedules nothing).
func (v *View) DirectMembers(containerID string) []*nib.Nib {
	group := v.children[containerID]
	if c := v.byID[containerID]; c != nil && c.EffectiveType() == "milestone" {
		group = v.assigned[containerID]
	}
	var members []*nib.Nib
	for _, b := range group {
		if b.EffectiveType() == "milestone" {
			continue
		}
		members = append(members, b)
	}
	return members
}

// Members returns the container's transitive membership closure, single-
// counted, in breadth-first input order. The closure is FULL depth — for a
// milestone, the assignees plus their structural subtrees; for any other
// container, its structural subtree — and does not descend through a
// milestone-typed child (see DirectMembers).
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
// or "" for a nib in the backlog: its own resolved assignment when it has
// one, else the nearest resolved assignment up the structural parent chain.
// The walk stops at a milestone-typed ancestor — a milestone parent is
// decomposition data, not an assignment — and a milestone belongs to no
// milestone itself: it is a container of its own even when hand-edited data
// assigns or nests it. An unknown id is in the backlog.
func (v *View) MilestoneOf(id string) string {
	b := v.byID[id]
	if b == nil || b.EffectiveType() == "milestone" {
		return ""
	}
	visited := make(map[string]bool)
	for b != nil && !visited[b.ID] {
		visited[b.ID] = true
		if b.EffectiveType() == "milestone" {
			return ""
		}
		if ms := ResolvedMilestoneID(b, v.lookup); ms != "" {
			return ms
		}
		if b.Parent == "" {
			return ""
		}
		b = v.byID[b.Parent]
	}
	return ""
}

// EpicGroup is one epic with its direct member items.
type EpicGroup struct {
	Epic  *nib.Nib
	Items []*nib.Nib
}

// Backlog is the work outside every milestone: the epics that belong to no
// milestone (each with their items), and the root-level work items. Sets only,
// in input order.
//
// "Backlog" is the name every surface uses for this set — `nibs list
// --backlog`, the roadmap's section, the GraphQL filter's documentation — so
// the package that defines it says the same word.
type Backlog struct {
	Epics []EpicGroup
	Other []*nib.Nib
}

// Backlog returns the set. Roots are the RESOLVED reading — a nib
// whose parent link names no nib is a root here, exactly as every query
// surface reports it — and scheduling is MilestoneOf's: a root with a
// resolved assignment is scheduled work, not backlog, while a dangling
// assignment schedules nothing. The remainder is computed against every
// declared milestone regardless of status: work under a status-hidden
// milestone is scheduled work, not backlog — a consumer wanting the old leak
// back has to build it deliberately.
func (v *View) Backlog() Backlog {
	var rem Backlog
	for _, b := range v.all {
		switch b.EffectiveType() {
		case "milestone":
		case "epic":
			if v.MilestoneOf(b.ID) == "" {
				rem.Epics = append(rem.Epics, EpicGroup{Epic: b, Items: v.DirectMembers(b.ID)})
			}
		default:
			if v.isRoot(b) && v.MilestoneOf(b.ID) == "" {
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
