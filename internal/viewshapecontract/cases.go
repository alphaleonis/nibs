package viewshapecontract

import (
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

var bugsOnly = viewshape.Filter{Type: []string{"bug"}}

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
	}
}
