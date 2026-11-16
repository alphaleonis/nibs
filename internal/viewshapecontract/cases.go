package viewshapecontract

import (
	"github.com/alphaleonis/nibs/internal/area"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/viewshape"
)

// Case is one shared view-shaping case: a nib set, a view over it, and the rows
// that view shows.
type Case struct {
	// Name identifies the case in both suites' output.
	Name string
	// Note says which rule the case pins. It is shipped in the generated module.
	Note string
	Nibs []*nib.Nib
	View viewshape.View
	Want []viewshape.Row
	// Reorders pins viewshape.StaysInGroup over pairs of the rows in Want.
	Reorders []Reorder
}

// Reorder is one question to viewshape.StaysInGroup: whether moving the row of
// nib From to the position of the row of nib To keeps it in its ordering group.
type Reorder struct {
	From, To string
	Stays    bool
}

// rowFlag sets one Boolean field of a Row that row builds.
type rowFlag int

const (
	hasChildren rowFlag = iota + 1
	dimmed
)

// row returns the Row of the nib id at depth, in the parent group of parent.
func row(id string, depth int, parent string, flags ...rowFlag) viewshape.Row {
	r := viewshape.Row{NibID: id, Depth: depth, Group: viewshape.Group{Axis: viewshape.GroupParent, ID: parent}}
	for _, f := range flags {
		switch f {
		case hasChildren:
			r.HasChildren = true
		case dimmed:
			r.Dimmed = true
		}
	}
	return r
}

// heads marks r as the row of the nib heading the section keyed by its id.
func heads(r viewshape.Row) viewshape.Row {
	r.Header = true
	r.Section = r.NibID
	return r
}

// in puts r in section.
func in(section string, r viewshape.Row) viewshape.Row {
	r.Section = section
	return r
}

// queued returns the row of the nib id at depth in the queue of milestone,
// which is also its section.
func queued(id string, depth int, milestone string, flags ...rowFlag) viewshape.Row {
	r := row(id, depth, "", flags...)
	r.Group = viewshape.Group{Axis: viewshape.GroupMilestone, ID: milestone}
	r.Section = milestone
	return r
}

// sectionRow returns the row of a section no nib heads.
func sectionRow(section string, depth int, flags ...rowFlag) viewshape.Row {
	r := row("", depth, "", flags...)
	r.Group = viewshape.Group{}
	r.Section = section
	r.Header = true
	return r
}

func view(level viewshape.Level, f viewshape.Filter, collapsed ...string) viewshape.View {
	v := viewshape.View{Level: level, Filter: f, Sort: viewshape.SortOrder}
	if len(collapsed) > 0 {
		v.Collapsed = make(map[string]bool, len(collapsed))
		for _, id := range collapsed {
			v.Collapsed[id] = true
		}
	}
	return v
}

// nestedSet is listed out of order key order, so a case passes only when the
// shaping sorts it. It holds a dangling parent link, a milestone, and a nib
// parented to that milestone.
func nestedSet() []*nib.Nib {
	return []*nib.Nib{
		{ID: "t2", Type: "task", Title: "Second step", Status: "todo", Parent: "f1", Order: "a1"},
		{ID: "t1", Type: "task", Title: "First step", Status: "todo", Parent: "f1", Order: "a0"},
		{ID: "f1", Type: "feature", Title: "Feature", Status: "todo", Parent: "e1", Order: "a0"},
		{ID: "e1", Type: "epic", Title: "Epic", Status: "todo", Order: "a1"},
		{ID: "r1", Type: "task", Title: "Root task", Status: "todo", Order: "a0"},
		{ID: "u1", Type: "task", Title: "Unordered root", Status: "todo"},
		{ID: "d1", Type: "task", Title: "Dangling parent", Status: "todo", Parent: "ghost", Order: "a2"},
		{ID: "m1", Type: "milestone", Title: "Milestone", Status: "todo", Order: "a3"},
		{ID: "h1", Type: "task", Title: "Under a milestone", Status: "todo", Parent: "m1", Order: "a4"},
	}
}

// grandchildSet holds one bug, b1, two levels below the root epic e1.
func grandchildSet() []*nib.Nib {
	return []*nib.Nib{
		{ID: "e1", Type: "epic", Title: "Checkout", Status: "todo", Order: "a0"},
		{ID: "f1", Type: "feature", Title: "Cart", Status: "todo", Parent: "e1", Order: "a0"},
		{ID: "b1", Type: "bug", Title: "Total is wrong", Status: "todo", Parent: "f1", Order: "a0"},
		{ID: "t1", Type: "task", Title: "Cart styling", Status: "todo", Parent: "f1", Order: "a1"},
		{ID: "f2", Type: "feature", Title: "Payment", Status: "todo", Parent: "e1", Order: "a1"},
		{ID: "t2", Type: "task", Title: "Card form", Status: "todo", Parent: "f2", Order: "a0"},
		{ID: "r1", Type: "task", Title: "Unrelated root", Status: "todo", Order: "a1"},
	}
}

// exclusionSet gives n5 a parent, n4, that the exclusions drop.
func exclusionSet() []*nib.Nib {
	return []*nib.Nib{
		{ID: "n1", Type: "task", Title: "Keep", Status: "todo", Tags: []string{"ui"}, Order: "a0"},
		{ID: "n2", Type: "task", Title: "Done", Status: "completed", Order: "a1"},
		{ID: "n3", Type: "task", Title: "Will not fix", Status: "in-progress", Tags: []string{"wontfix"}, Order: "a2"},
		{ID: "n4", Type: "feature", Title: "Parked feature", Status: "draft", Tags: []string{"ui", "wontfix"}, Order: "a3"},
		{ID: "n5", Type: "task", Title: "Deferred child", Status: "deferred", Parent: "n4", Order: "a0"},
	}
}

// cycleSet holds the parent cycle c1 -> c2 -> c1, with k1 hanging off c2.
func cycleSet() []*nib.Nib {
	return []*nib.Nib{
		{ID: "c2", Type: "task", Title: "Cycle member", Status: "todo", Parent: "c1", Order: "a1"},
		{ID: "c1", Type: "task", Title: "Cycle member", Status: "todo", Parent: "c2", Order: "a0"},
		{ID: "k1", Type: "bug", Title: "Hangs off the cycle", Status: "todo", Parent: "c2", Order: "a0"},
	}
}

// milestoneSet holds three milestones, m3 with nothing in it. m1's queue holds
// t2, e1 and t3 directly, out of queue order, and f1 and t1 through e1. c1 is
// queued on m2 below r1, which is in the Backlog with r2, and d1 names a
// milestone that does not exist.
func milestoneSet() []*nib.Nib {
	return []*nib.Nib{
		{ID: "m2", Type: "milestone", Title: "Beta", Status: "todo", Order: "a1"},
		{ID: "m1", Type: "milestone", Title: "Alpha", Status: "todo", Order: "a0"},
		{ID: "m3", Type: "milestone", Title: "Empty", Status: "todo", Order: "a2"},
		{ID: "e1", Type: "epic", Title: "Checkout", Status: "todo", Milestone: "m1", MilestoneOrder: "a1", Order: "a3"},
		{ID: "f1", Type: "feature", Title: "Cart", Status: "todo", Parent: "e1", Order: "a0"},
		{ID: "t1", Type: "task", Title: "Cart total", Status: "todo", Parent: "f1", Tags: []string{"money"}, Order: "a0"},
		{ID: "t2", Type: "task", Title: "Release notes", Status: "todo", Milestone: "m1", MilestoneOrder: "a0", Order: "a5"},
		{ID: "t3", Type: "task", Title: "Unqueued", Status: "todo", Milestone: "m1", Order: "a4"},
		{ID: "b1", Type: "bug", Title: "Crash", Status: "todo", Milestone: "m2", MilestoneOrder: "a0", Order: "a6"},
		{ID: "c1", Type: "task", Title: "Scheduled step", Status: "todo", Parent: "r1", Milestone: "m2", MilestoneOrder: "a1", Tags: []string{"sched"}, Order: "a2"},
		{ID: "r1", Type: "task", Title: "Someday", Status: "todo", Order: "a7"},
		{ID: "r2", Type: "task", Title: "Someday step", Status: "todo", Parent: "r1", Order: "a1"},
		{ID: "d1", Type: "task", Title: "Dangling milestone", Status: "todo", Milestone: "ghost", Order: "a8"},
	}
}

// areaVocabulary declares api with api/webhooks beneath it, auth, and web,
// listed out of name order.
func areaVocabulary() *area.Vocabulary {
	return &area.Vocabulary{Nodes: []area.Node{
		{Name: "web"},
		{Name: "auth", Description: "Sign-in", Color: "teal"},
		{Name: "api", Children: []area.Node{{Name: "webhooks"}}},
	}}
}

// areaSet places nibs in api, api/webhooks and auth, leaves web empty, and gives
// the No area section a5, whose parent is in auth, a6, whose area is not
// declared, and a7, which has none.
func areaSet() []*nib.Nib {
	return []*nib.Nib{
		{ID: "a1", Type: "task", Title: "Rate limits", Status: "todo", Area: "api", Order: "a0"},
		{ID: "a2", Type: "task", Title: "Retry hooks", Status: "todo", Area: "api/webhooks", Order: "a1"},
		{ID: "a3", Type: "feature", Title: "Login", Status: "todo", Area: "auth", Order: "a2"},
		{ID: "a4", Type: "task", Title: "Login form", Status: "todo", Parent: "a3", Area: "auth", Tags: []string{"form"}, Order: "a0"},
		{ID: "a5", Type: "task", Title: "Session cookie", Status: "todo", Parent: "a3", Order: "a1"},
		{ID: "a6", Type: "task", Title: "Legacy", Status: "todo", Area: "billing", Order: "a3"},
		{ID: "a7", Type: "task", Title: "Unsorted", Status: "todo", Order: "a4"},
	}
}

func areasView(f viewshape.Filter, collapsedSections ...string) viewshape.View {
	v := view(viewshape.LevelAreas, f)
	v.Areas = areaVocabulary()
	if len(collapsedSections) > 0 {
		v.CollapsedSections = make(map[string]bool, len(collapsedSections))
		for _, key := range collapsedSections {
			v.CollapsedSections[key] = true
		}
	}
	return v
}

var bugsOnly = viewshape.Filter{Type: []string{"bug"}}

const (
	backlog = viewshape.BacklogSection
	noArea  = viewshape.NoAreaSection
)

// Cases returns the shared view-shaping cases.
func Cases() []Case {
	return []Case{
		{
			Name: "tree nests children under their parents in order key order",
			Note: "milestones get no row, a nib parented to one is a root that keeps the milestone's parent group, a dangling parent link counts as none, and an unkeyed nib sorts after keyed ones",
			Nibs: nestedSet(),
			View: view(viewshape.LevelTree, viewshape.Filter{}),
			Want: []viewshape.Row{
				row("r1", 0, ""),
				row("e1", 0, "", hasChildren),
				row("f1", 1, "e1", hasChildren),
				row("t1", 2, "f1"),
				row("t2", 2, "f1"),
				row("d1", 0, ""),
				row("h1", 0, "m1"),
				row("u1", 0, ""),
			},
			Reorders: []Reorder{
				{From: "t2", To: "t1", Stays: true},
				{From: "d1", To: "r1", Stays: true},
				{From: "f1", To: "r1", Stays: false},
				{From: "h1", To: "r1", Stays: false},
			},
		},
		{
			Name: "flat lists every nib at depth 0 in order key order",
			Note: "flat keeps milestones, nests nothing, and orders equal keys by title",
			Nibs: nestedSet(),
			View: view(viewshape.LevelFlat, viewshape.Filter{}),
			Want: []viewshape.Row{
				row("f1", 0, "e1"),
				row("t1", 0, "f1"),
				row("r1", 0, ""),
				row("e1", 0, ""),
				row("t2", 0, "f1"),
				row("d1", 0, ""),
				row("m1", 0, ""),
				row("h1", 0, "m1"),
				row("u1", 0, ""),
			},
		},
		{
			Name: "a collapsed parent hides its descendants",
			Note: "the collapsed row keeps hasChildren, and collapsing a leaf changes nothing",
			Nibs: nestedSet(),
			View: view(viewshape.LevelTree, viewshape.Filter{}, "f1", "u1"),
			Want: []viewshape.Row{
				row("r1", 0, ""),
				row("e1", 0, "", hasChildren),
				row("f1", 1, "e1", hasChildren),
				row("d1", 0, ""),
				row("h1", 0, "m1"),
				row("u1", 0, ""),
			},
		},
		{
			Name: "tree keeps a matching grandchild's ancestors as dimmed context",
			Note: "the ancestor-context rule: a non-matching ancestor of a match shows dimmed, and hasChildren counts only the rows the filter keeps",
			Nibs: grandchildSet(),
			View: view(viewshape.LevelTree, bugsOnly),
			Want: []viewshape.Row{
				row("e1", 0, "", hasChildren, dimmed),
				row("f1", 1, "e1", hasChildren, dimmed),
				row("b1", 2, "f1"),
			},
		},
		{
			Name: "flat shows a matching grandchild without its ancestors",
			Note: "the ancestor-context rule does not apply to flat",
			Nibs: grandchildSet(),
			View: view(viewshape.LevelFlat, bugsOnly),
			Want: []viewshape.Row{
				row("b1", 0, "f1"),
			},
		},
		{
			Name: "a collapsed dimmed ancestor hides the match beneath it",
			Note: "collapse applies to context rows as to any other",
			Nibs: grandchildSet(),
			View: view(viewshape.LevelTree, bugsOnly, "f1"),
			Want: []viewshape.Row{
				row("e1", 0, "", hasChildren, dimmed),
				row("f1", 1, "e1", hasChildren, dimmed),
			},
		},
		{
			Name: "an excluded type stays as context above the matches in tree",
			Note: "-type: the excluded epic shows dimmed because its descendants match",
			Nibs: grandchildSet(),
			View: view(viewshape.LevelTree, viewshape.Filter{ExcludeType: []string{"epic"}}),
			Want: []viewshape.Row{
				row("e1", 0, "", hasChildren, dimmed),
				row("f1", 1, "e1", hasChildren),
				row("b1", 2, "f1"),
				row("t1", 2, "f1"),
				row("f2", 1, "e1", hasChildren),
				row("t2", 2, "f2"),
				row("r1", 0, ""),
			},
		},
		{
			Name: "excluded statuses and tags drop nibs in flat",
			Note: "-status and -tags: a nib carrying any excluded tag is dropped, and its matching child shows alone; n5 leads because flat orders equal keys by title",
			Nibs: exclusionSet(),
			View: view(viewshape.LevelFlat, viewshape.Filter{ExcludeStatus: []string{"completed"}, ExcludeTags: []string{"wontfix"}}),
			Want: []viewshape.Row{
				row("n5", 0, "n4"),
				row("n1", 0, ""),
			},
		},
		{
			Name: "excluded statuses and tags keep a dropped parent as context in tree",
			Note: "-status and -tags under the ancestor-context rule",
			Nibs: exclusionSet(),
			View: view(viewshape.LevelTree, viewshape.Filter{ExcludeStatus: []string{"completed"}, ExcludeTags: []string{"wontfix"}}),
			Want: []viewshape.Row{
				row("n1", 0, ""),
				row("n4", 0, "", hasChildren, dimmed),
				row("n5", 1, "n4"),
			},
		},
		{
			Name: "the closed status group selects every closed status",
			Note: "the filter holds the members of the closed group, deferred among them, as the CLI expands status:closed",
			Nibs: []*nib.Nib{
				{ID: "e1", Type: "epic", Title: "Open epic", Status: "in-progress", Order: "a0"},
				{ID: "t1", Type: "task", Title: "Completed", Status: "completed", Parent: "e1", Order: "a0"},
				{ID: "t2", Type: "task", Title: "Still open", Status: "todo", Parent: "e1", Order: "a1"},
				{ID: "t3", Type: "task", Title: "Scrapped", Status: "scrapped", Parent: "e1", Order: "a2"},
				{ID: "e2", Type: "epic", Title: "Deferred epic", Status: "deferred", Order: "a1"},
				{ID: "r1", Type: "task", Title: "Open root", Status: "todo", Order: "a2"},
			},
			View: view(viewshape.LevelTree, viewshape.Filter{Status: config.Default().ClosedStatusNames()}),
			Want: []viewshape.Row{
				row("e1", 0, "", hasChildren, dimmed),
				row("t1", 1, "e1"),
				row("t3", 1, "e1"),
				row("e2", 0, ""),
			},
		},
		{
			Name: "an estimate filter matches the listed estimates",
			Note: "f1 matches while its unestimated child does not, so f1 shows without children",
			Nibs: []*nib.Nib{
				{ID: "e1", Type: "epic", Title: "Large epic", Status: "todo", Estimate: "l", Order: "a0"},
				{ID: "t1", Type: "task", Title: "Small task", Status: "todo", Estimate: "s", Parent: "e1", Order: "a0"},
				{ID: "t2", Type: "task", Title: "Huge task", Status: "todo", Estimate: "xl", Parent: "e1", Order: "a1"},
				{ID: "f1", Type: "feature", Title: "Medium feature", Status: "todo", Estimate: "m", Order: "a1"},
				{ID: "t3", Type: "task", Title: "Unestimated", Status: "todo", Parent: "f1", Order: "a0"},
				{ID: "r1", Type: "task", Title: "Unestimated root", Status: "todo", Order: "a2"},
			},
			View: view(viewshape.LevelTree, viewshape.Filter{Estimate: []string{"s", "m"}}),
			Want: []viewshape.Row{
				row("e1", 0, "", hasChildren, dimmed),
				row("t1", 1, "e1"),
				row("f1", 0, ""),
			},
		},
		{
			Name: "type and priority match their effective values",
			Note: "a nib that omits type and priority matches task and normal",
			Nibs: []*nib.Nib{
				{ID: "n1", Title: "Omits both", Status: "todo", Order: "a0"},
				{ID: "n2", Type: "bug", Title: "A bug", Status: "todo", Order: "a1"},
				{ID: "n3", Type: "task", Title: "Urgent task", Priority: "high", Status: "todo", Order: "a2"},
				{ID: "n4", Type: "task", Title: "Explicit defaults", Priority: "normal", Status: "todo", Order: "a3"},
			},
			View: view(viewshape.LevelFlat, viewshape.Filter{Type: []string{"task"}, Priority: []string{"normal"}}),
			Want: []viewshape.Row{
				row("n1", 0, ""),
				row("n4", 0, ""),
			},
		},
		{
			Name: "a filter nothing matches shows no rows",
			Note: "an empty result is no rows, not an error",
			Nibs: grandchildSet(),
			View: view(viewshape.LevelTree, viewshape.Filter{Tags: []string{"no-such-tag"}}),
			Want: nil,
		},
		{
			Name: "the lowest id of a parent cycle is promoted to a root",
			Note: "each member keeps its stored parent's group",
			Nibs: cycleSet(),
			View: view(viewshape.LevelTree, viewshape.Filter{}),
			Want: []viewshape.Row{
				row("c1", 0, "c2", hasChildren),
				row("c2", 1, "c1", hasChildren),
				row("k1", 2, "c2"),
			},
			Reorders: []Reorder{
				{From: "k1", To: "c1", Stays: true},
				{From: "c1", To: "k1", Stays: false},
			},
		},
		{
			Name: "a match below a parent cycle keeps the whole cycle as context",
			Note: "the ancestor walk stops when it reaches a member again",
			Nibs: cycleSet(),
			View: view(viewshape.LevelTree, bugsOnly),
			Want: []viewshape.Row{
				row("c1", 0, "c2", hasChildren, dimmed),
				row("c2", 1, "c1", hasChildren, dimmed),
				row("k1", 2, "c2"),
			},
		},
		{
			Name: "milestones gives each milestone a section and the rest the Backlog",
			Note: "membership is derived up the parent chain; a queue's direct members are in queue order with unkeyed ones last; an empty milestone keeps its row; a dangling assignment and work with no milestone fall into the Backlog, last",
			Nibs: milestoneSet(),
			View: view(viewshape.LevelMilestones, viewshape.Filter{}),
			Want: []viewshape.Row{
				heads(row("m1", 0, "", hasChildren)),
				queued("t2", 1, "m1"),
				queued("e1", 1, "m1", hasChildren),
				in("m1", row("f1", 2, "e1", hasChildren)),
				in("m1", row("t1", 3, "f1")),
				queued("t3", 1, "m1"),
				heads(row("m2", 0, "", hasChildren)),
				queued("b1", 1, "m2"),
				queued("c1", 1, "m2"),
				heads(row("m3", 0, "")),
				sectionRow(backlog, 0, hasChildren),
				in(backlog, row("r1", 1, "", hasChildren)),
				in(backlog, row("r2", 2, "r1")),
				in(backlog, row("d1", 1, "")),
			},
			Reorders: []Reorder{
				{From: "t3", To: "t2", Stays: true},
				{From: "t2", To: "e1", Stays: true},
				{From: "c1", To: "b1", Stays: true},
				{From: "m1", To: "m2", Stays: true},
				{From: "d1", To: "r1", Stays: true},
				{From: "t2", To: "t2", Stays: false},
				{From: "f1", To: "t3", Stays: false},
				{From: "b1", To: "t2", Stays: false},
				{From: "r1", To: "m3", Stays: false},
			},
		},
		{
			Name: "milestones keeps a nested match's ancestors as dimmed context in its section",
			Note: "the ancestor-context rule in a milestone section: the heading milestone dims, and sections with nothing shown are dropped",
			Nibs: milestoneSet(),
			View: view(viewshape.LevelMilestones, viewshape.Filter{Tags: []string{"money"}}),
			Want: []viewshape.Row{
				heads(row("m1", 0, "", hasChildren, dimmed)),
				queued("e1", 1, "m1", hasChildren, dimmed),
				in("m1", row("f1", 2, "e1", hasChildren, dimmed)),
				in("m1", row("t1", 3, "f1")),
			},
		},
		{
			Name: "milestones keeps a match's ancestor in the section the ancestor is in",
			Note: "c1 is queued on m2 while its parent r1 is in the Backlog, so r1 shows there as context; the Backlog's own row never dims",
			Nibs: milestoneSet(),
			View: view(viewshape.LevelMilestones, viewshape.Filter{Tags: []string{"sched"}}),
			Want: []viewshape.Row{
				heads(row("m2", 0, "", hasChildren, dimmed)),
				queued("c1", 1, "m2"),
				sectionRow(backlog, 0, hasChildren),
				in(backlog, row("r1", 1, "", dimmed)),
			},
		},
		{
			Name: "a collapsed milestone or Backlog hides its section's rows",
			Note: "a milestone section collapses by the milestone's id and the Backlog by its section key; both keep hasChildren",
			Nibs: milestoneSet(),
			View: func() viewshape.View {
				v := view(viewshape.LevelMilestones, viewshape.Filter{}, "m1")
				v.CollapsedSections = map[string]bool{backlog: true}
				return v
			}(),
			Want: []viewshape.Row{
				heads(row("m1", 0, "", hasChildren)),
				heads(row("m2", 0, "", hasChildren)),
				queued("b1", 1, "m2"),
				queued("c1", 1, "m2"),
				heads(row("m3", 0, "")),
				sectionRow(backlog, 0, hasChildren),
			},
		},
		{
			Name: "a reorder before the moved nib's own descendant leaves its group",
			Note: "e1 and b1 share m1's queue, but b1 descends from e1 through m2, a milestone parented under e1",
			Nibs: []*nib.Nib{
				{ID: "m1", Type: "milestone", Title: "Release", Status: "todo", Order: "a0"},
				{ID: "m2", Type: "milestone", Title: "Nested", Status: "todo", Parent: "e1", Order: "a1"},
				{ID: "e1", Type: "epic", Title: "Epic", Status: "todo", Milestone: "m1", MilestoneOrder: "a0", Order: "a2"},
				{ID: "b1", Type: "bug", Title: "Bug", Status: "todo", Parent: "m2", Milestone: "m1", MilestoneOrder: "a1", Order: "a3"},
				{ID: "t1", Type: "task", Title: "Task", Status: "todo", Milestone: "m1", MilestoneOrder: "a2", Order: "a4"},
			},
			View: view(viewshape.LevelMilestones, viewshape.Filter{}),
			Want: []viewshape.Row{
				heads(row("m1", 0, "", hasChildren)),
				queued("e1", 1, "m1"),
				queued("b1", 1, "m1"),
				queued("t1", 1, "m1"),
				heads(row("m2", 0, "e1")),
			},
			Reorders: []Reorder{
				{From: "e1", To: "b1", Stays: false},
				{From: "b1", To: "e1", Stays: true},
				{From: "e1", To: "t1", Stays: true},
			},
		},
		{
			Name: "areas gives each declared area a section and the rest No area",
			Note: "sections follow the vocabulary's name order with a nested area inside its parent, ahead of the parent's nibs; an empty declared area keeps its row; an undeclared area falls into No area, last; a nib whose parent is in another section is a root in its own; area rows keep their parent groups",
			Nibs: areaSet(),
			View: areasView(viewshape.Filter{}),
			Want: []viewshape.Row{
				sectionRow("api", 0, hasChildren),
				sectionRow("api/webhooks", 1, hasChildren),
				in("api/webhooks", row("a2", 2, "")),
				in("api", row("a1", 1, "")),
				sectionRow("auth", 0, hasChildren),
				in("auth", row("a3", 1, "", hasChildren)),
				in("auth", row("a4", 2, "a3")),
				sectionRow("web", 0),
				sectionRow(noArea, 0, hasChildren),
				in(noArea, row("a5", 1, "a3")),
				in(noArea, row("a6", 1, "")),
				in(noArea, row("a7", 1, "")),
			},
			Reorders: []Reorder{
				{From: "a7", To: "a6", Stays: true},
				{From: "a1", To: "a2", Stays: false},
				{From: "a1", To: "a3", Stays: false},
				{From: "a5", To: "a6", Stays: false},
			},
		},
		{
			Name: "areas keeps every declared area while filtered",
			Note: "a declared area's row survives a filter that empties it, No area does not, and the ancestor-context rule applies within a section",
			Nibs: areaSet(),
			View: areasView(viewshape.Filter{Tags: []string{"form"}}),
			Want: []viewshape.Row{
				sectionRow("api", 0, hasChildren),
				sectionRow("api/webhooks", 1),
				sectionRow("auth", 0, hasChildren),
				in("auth", row("a3", 1, "", hasChildren, dimmed)),
				in("auth", row("a4", 2, "a3")),
				sectionRow("web", 0),
			},
		},
		{
			Name: "a collapsed area or No area hides its section's rows",
			Note: "area sections collapse by their section keys, and a collapsed area hides the areas declared inside it",
			Nibs: areaSet(),
			View: areasView(viewshape.Filter{}, "api", noArea),
			Want: []viewshape.Row{
				sectionRow("api", 0, hasChildren),
				sectionRow("auth", 0, hasChildren),
				in("auth", row("a3", 1, "", hasChildren)),
				in("auth", row("a4", 2, "a3")),
				sectionRow("web", 0),
				sectionRow(noArea, 0, hasChildren),
			},
		},
	}
}
