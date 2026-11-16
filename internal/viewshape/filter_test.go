package viewshape

import (
	"slices"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
)

func TestMatches(t *testing.T) {
	defaulted := &nib.Nib{ID: "d", Status: "todo"}
	tagged := &nib.Nib{ID: "a", Type: "bug", Priority: "high", Status: "completed", Estimate: "s", Tags: []string{"frontend", "urgent"}}
	untagged := &nib.Nib{ID: "u", Type: "epic", Status: "todo", Estimate: ""}

	tests := []struct {
		name string
		n    *nib.Nib
		f    Filter
		want bool
	}{
		{"an empty filter matches", tagged, Filter{}, true},
		{"type is effective", defaulted, Filter{Type: []string{nib.DefaultType}}, true},
		{"excluded effective type", defaulted, Filter{ExcludeType: []string{nib.DefaultType}}, false},
		{"priority is effective", defaulted, Filter{Priority: []string{nib.DefaultPriority}}, true},
		{"excluded effective priority", defaulted, Filter{ExcludePriority: []string{nib.DefaultPriority}}, false},
		{"status outside the list", tagged, Filter{Status: []string{"todo"}}, false},
		{"status in the list", tagged, Filter{Status: []string{"todo", "completed"}}, true},
		{"excluded status", tagged, Filter{ExcludeStatus: []string{"completed"}}, false},
		{"estimate in the list", tagged, Filter{Estimate: []string{"s", "l"}}, true},
		{"excluded estimate", tagged, Filter{ExcludeEstimate: []string{"s"}}, false},
		{"an empty estimate is a value", untagged, Filter{Estimate: []string{""}}, true},
		{"any listed tag", tagged, Filter{Tags: []string{"backend", "urgent"}}, true},
		{"no listed tag", tagged, Filter{Tags: []string{"backend"}}, false},
		{"a nib without tags against a tag list", untagged, Filter{Tags: []string{"frontend"}}, false},
		{"any excluded tag", tagged, Filter{ExcludeTags: []string{"urgent"}}, false},
		{"a nib without tags against an exclusion", untagged, Filter{ExcludeTags: []string{"urgent"}}, true},
		{"every field must admit", tagged, Filter{Type: []string{"bug"}, Status: []string{"todo"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Matches(tt.n, tt.f); got != tt.want {
				t.Errorf("Matches = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShapeRefusesWhatItDoesNotImplement(t *testing.T) {
	if _, err := Shape(nil, View{Level: "epics", Sort: SortOrder}); err == nil {
		t.Error("Shape accepted an unimplemented level")
	}
	if _, err := Shape(nil, View{Level: LevelTree, Sort: "title"}); err == nil {
		t.Error("Shape accepted an unimplemented sort")
	}
}

// TestMatchJoinsTheFilter covers View.Match: a nib must pass it and the Filter
// to match, and the ancestor-context rule treats its misses as any other.
func TestMatchJoinsTheFilter(t *testing.T) {
	nibs := []*nib.Nib{
		{ID: "e1", Type: "epic", Status: "todo", Title: "Epic", Order: "a0"},
		{ID: "t1", Type: "task", Status: "todo", Title: "Wanted", Parent: "e1", Order: "a1"},
		{ID: "t2", Type: "task", Status: "completed", Title: "Wanted but closed", Parent: "e1", Order: "a2"},
		{ID: "t3", Type: "task", Status: "todo", Title: "Other", Order: "a3"},
	}
	match := func(n *nib.Nib) bool { return n.Title[0] == 'W' }
	filter := Filter{ExcludeStatus: []string{"completed"}}
	parent := func(id string) Group { return Group{Axis: GroupParent, ID: id} }
	tests := []struct {
		level Level
		want  []Row
	}{
		{LevelTree, []Row{
			{NibID: "e1", Dimmed: true, HasChildren: true, Group: parent("")},
			{NibID: "t1", Depth: 1, Group: parent("e1")},
		}},
		{LevelFlat, []Row{{NibID: "t1", Group: parent("e1")}}},
	}
	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			got, err := Shape(nibs, View{Level: tt.level, Filter: filter, Match: match, Sort: SortOrder})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("rows differ\n got: %+v\nwant: %+v", got, tt.want)
			}
		})
	}
}
