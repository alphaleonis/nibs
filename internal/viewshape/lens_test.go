package viewshape

import (
	"slices"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
)

// TestStructuralLensCarriesSubtrees covers the lens mode no shipped lens uses
// yet: an epic heads a section with its whole subtree, a milestone is hidden so
// its epic splices up, and any other root takes its subtree into the leftover.
func TestStructuralLensCarriesSubtrees(t *testing.T) {
	const level Level = "test-epics"
	const noEpic = "/__no_epic__"
	lenses[level] = &lens{
		leftover: noEpic,
		place: func(b *nib.Nib, _ *shapeInput) placement {
			switch b.EffectiveType() {
			case "epic":
				return placement{kind: placeHeader, section: b.ID}
			case "milestone":
				return placement{kind: placeHidden}
			default:
				return placement{kind: placeMember, section: noEpic}
			}
		},
		nestStructurally: true,
		memberGroup:      func(string) Group { return Group{} },
		queueKey:         func(string) func(*nib.Nib) string { return nil },
	}
	t.Cleanup(func() { delete(lenses, level) })

	nibs := []*nib.Nib{
		{ID: "ms", Type: "milestone", Title: "Hidden", Order: "a0"},
		{ID: "e1", Type: "epic", Title: "Under a milestone", Parent: "ms", Order: "a1"},
		{ID: "t1", Type: "task", Title: "Epic child", Parent: "e1", Order: "a2"},
		{ID: "f1", Type: "feature", Title: "Loose", Order: "a3"},
		{ID: "t2", Type: "task", Title: "Loose child", Parent: "f1", Order: "a4"},
		{ID: "e2", Type: "epic", Title: "Root epic", Order: "a5"},
	}
	got, err := Shape(nibs, View{Level: level, Sort: SortOrder})
	if err != nil {
		t.Fatal(err)
	}
	parent := func(id string) Group { return Group{Axis: GroupParent, ID: id} }
	want := []Row{
		{NibID: "e1", Section: "e1", Header: true, HasChildren: true, Group: parent("ms")},
		{NibID: "t1", Depth: 1, Section: "e1", Group: parent("e1")},
		{NibID: "e2", Section: "e2", Header: true, Group: parent("")},
		{Section: noEpic, Header: true, HasChildren: true},
		{NibID: "f1", Depth: 1, Section: noEpic, HasChildren: true, Group: parent("")},
		{NibID: "t2", Depth: 2, Section: noEpic, Group: parent("f1")},
	}
	if !slices.Equal(got, want) {
		t.Errorf("rows differ\n got: %+v\nwant: %+v", got, want)
	}
}

// TestHeadedDeclaredSection covers a nib heading a declared section: a declared
// leaf renders with its header nested where it is declared, and a declared
// section that declares sections inside it refuses a header.
func TestHeadedDeclaredSection(t *testing.T) {
	const level Level = "test-declared"
	headerOf := map[string]string{}
	lenses[level] = &lens{
		leftover: "/__none__",
		declared: func(*shapeInput) []declaredSection {
			return []declaredSection{{key: "x", children: []declaredSection{{key: "y"}}}}
		},
		place: func(b *nib.Nib, _ *shapeInput) placement {
			if section, ok := headerOf[b.ID]; ok {
				return placement{kind: placeHeader, section: section}
			}
			return placement{kind: placeMember, section: "y"}
		},
		memberGroup: func(string) Group { return Group{} },
		queueKey:    func(string) func(*nib.Nib) string { return nil },
	}
	t.Cleanup(func() { delete(lenses, level) })
	nibs := []*nib.Nib{
		{ID: "n1", Title: "Header", Order: "a0"},
		{ID: "n2", Title: "Member", Order: "a1"},
	}

	headerOf["n1"] = "y"
	got, err := Shape(nibs, View{Level: level, Sort: SortOrder})
	if err != nil {
		t.Fatal(err)
	}
	root := Group{Axis: GroupParent}
	want := []Row{
		{Section: "x", Header: true, HasChildren: true},
		{NibID: "n1", Depth: 1, Section: "y", Header: true, HasChildren: true, Group: root},
		{NibID: "n2", Depth: 2, Section: "y", Group: root},
	}
	if !slices.Equal(got, want) {
		t.Errorf("rows differ\n got: %+v\nwant: %+v", got, want)
	}
	if section := memberOf(want, 1); section != "x" {
		t.Errorf("memberOf(n1) = %q, want the enclosing section x", section)
	}

	headerOf["n1"] = "x"
	if _, err := Shape(nibs, View{Level: level, Sort: SortOrder}); err == nil {
		t.Error("Shape accepted a header for a section that declares sections inside it")
	}
}
