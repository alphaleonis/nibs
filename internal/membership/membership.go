// Package membership answers "what belongs to container X". The structural
// parent axis (`parent:`) is decomposition; the assignment axis (`milestone:`)
// is scheduling. A milestone holds its assignees plus their structural
// subtrees, any other container its structural subtree. Consumers keep display
// policy (filtering, sorting, progress, queue order) to themselves.
//
// Decide membership through this package, not from the raw `parent:` string: a
// link naming no nib is non-empty and confers no membership.
package membership

import "github.com/alphaleonis/nibs/internal/nib"

// Lookup resolves a nib id, returning nil for an id that names no nib.
type Lookup func(id string) *nib.Nib

// ResolvedMilestoneID returns the milestone b is directly assigned to: the
// target of b's `milestone:` field when that target exists and is
// milestone-typed and b is not itself a milestone, "" otherwise.
func ResolvedMilestoneID(b *nib.Nib, lookup Lookup) string {
	if b.Milestone == "" || b.EffectiveType() == "milestone" {
		return ""
	}
	target := lookup(b.Milestone)
	if target == nil || target.EffectiveType() != "milestone" {
		return ""
	}
	return target.ID
}

// View is a membership index over one slice of nibs. It pins the *nib.Nib
// pointers it was built over: build one per command or GraphQL operation.
type View struct {
	byID       map[string]*nib.Nib
	children   map[string][]*nib.Nib // resolved parent id → children; "" holds the roots
	assigned   map[string][]*nib.Nib // ResolvedMilestoneID → assignees; "" holds the unassigned
	milestones []*nib.Nib
	all        []*nib.Nib
}

// Compute indexes all, which may hold invariant-violating data, and retains the
// slice itself. Links resolve against all: a dangling parent makes a root, and a
// dangling assignment schedules nothing.
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

func (v *View) lookup(id string) *nib.Nib {
	return v.byID[id]
}

// Milestones returns the milestone-typed nibs in input order.
func (v *View) Milestones() []*nib.Nib {
	return copyNibs(v.milestones)
}

// Children returns the nibs whose resolved parent is containerID, in input
// order and of every type; "" names the roots. It ignores `milestone:`, so a
// milestone's assignees come from DirectMembers.
func (v *View) Children(containerID string) []*nib.Nib {
	return copyNibs(v.children[containerID])
}

// DirectMembers returns what directly belongs to the container, in input order:
// a milestone's assignees, or any other container's structural children.
// Milestone-typed nibs are never members.
func (v *View) DirectMembers(containerID string) []*nib.Nib {
	group := v.children[containerID]
	if c := v.byID[containerID]; c != nil && c.EffectiveType() == "milestone" {
		group = v.assigned[containerID]
	}
	// No milestone is assigned to a milestone, but a milestone with a `parent:`
	// sits in that parent's children.
	var members []*nib.Nib
	for _, b := range group {
		if b.EffectiveType() == "milestone" {
			continue
		}
		members = append(members, b)
	}
	return members
}

// Members returns the container's full-depth membership, each nib once,
// breadth-first: DirectMembers applied transitively, so it never descends into a
// milestone.
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

// MilestoneOf returns the id of the milestone the nib belongs to: its own
// resolved assignment, else the nearest one up the structural parent chain,
// which stops at a milestone-typed ancestor. "" for a milestone, an unknown id,
// or unscheduled work.
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

// EpicGroup is one epic and its DirectMembers.
type EpicGroup struct {
	Epic  *nib.Nib
	Items []*nib.Nib
}

// Backlog is the work outside every milestone: the epics MilestoneOf places in
// none, and the unscheduled root nibs of other non-milestone types, in input
// order. An epic's Items are its DirectMembers, scheduled or not.
type Backlog struct {
	Epics []EpicGroup
	Other []*nib.Nib
}

// Backlog counts work under a milestone of any status as scheduled.
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

func (v *View) isRoot(b *nib.Nib) bool {
	if b.Parent == "" {
		return true
	}
	return v.byID[b.Parent] == nil
}

// copyNibs returns a fresh slice over the same pointers, so callers may sort it.
func copyNibs(nibs []*nib.Nib) []*nib.Nib {
	if nibs == nil {
		return nil
	}
	out := make([]*nib.Nib, len(nibs))
	copy(out, nibs)
	return out
}
