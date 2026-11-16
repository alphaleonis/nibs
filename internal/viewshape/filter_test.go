package viewshape

import (
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
	if _, err := Shape(nil, View{Level: "milestones", Sort: SortOrder}); err == nil {
		t.Error("Shape accepted an unimplemented level")
	}
	if _, err := Shape(nil, View{Level: LevelTree, Sort: "title"}); err == nil {
		t.Error("Shape accepted an unimplemented sort")
	}
}
