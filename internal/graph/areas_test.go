package graph

import (
	"context"
	"slices"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/testdata/fixtures"
)

// areaWire is one entry of the flat list reduced to the two fields the ordering
// contract is expressed in.
type areaWire struct {
	path  string
	depth int
}

// areaCfg builds a store config declaring the given vocabulary and nothing else
// unusual.
func areaCfg(areas ...config.AreaConfig) *config.Config {
	cfg := config.DefaultWithPrefix("nibs-")
	cfg.Areas = areas
	return cfg
}

// sampleProjectConfig loads the committed fixture's own config, so the tests
// that read it fail when the fixture's declared vocabulary changes shape rather
// than silently asserting about a copy that has drifted.
func sampleProjectConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.LoadFromStore(fixtures.NibsPath(fixtures.SampleProjectDir(t)))
	if err != nil {
		t.Fatalf("loading the sample-project config: %v", err)
	}
	return cfg
}

func resolveAreaList(t *testing.T, cfg *config.Config) []*model.Area {
	t.Helper()
	r := &queryResolver{&Resolver{Reader: &stubReader{cfg: cfg}}}
	got, err := r.Config(context.Background())
	if err != nil {
		t.Fatalf("config resolver: %v", err)
	}
	return got.Areas
}

func wireOf(areas []*model.Area) []areaWire {
	out := make([]areaWire, 0, len(areas))
	for _, a := range areas {
		out = append(out, areaWire{path: a.Path, depth: a.Depth})
	}
	return out
}

func pathsOf(areas []*model.Area) []string {
	out := make([]string, 0, len(areas))
	for _, a := range areas {
		out = append(out, a.Path)
	}
	return out
}

// TestConfigResolverFlattensAreasInDeclarationOrder pins the shape of the wire
// list: declaration order, a parent immediately before the subtree it heads,
// depth counted from a root.
func TestConfigResolverFlattensAreasInDeclarationOrder(t *testing.T) {
	tests := []struct {
		name  string
		areas []config.AreaConfig
		want  []areaWire
	}{
		{
			name:  "no areas declared",
			areas: nil,
			want:  []areaWire{},
		},
		{
			name: "roots keep the file's order, not an alphabetical one",
			areas: []config.AreaConfig{
				{Name: "infra"},
				{Name: "auth"},
				{Name: "api"},
			},
			want: []areaWire{{"infra", 0}, {"auth", 0}, {"api", 0}},
		},
		{
			name: "a parent comes immediately before its subtree",
			areas: []config.AreaConfig{
				{Name: "web", Children: []config.AreaConfig{{Name: "dashboard"}, {Name: "settings"}}},
				{Name: "infra"},
			},
			want: []areaWire{
				{"web", 0},
				{"web/dashboard", 1},
				{"web/settings", 1},
				{"infra", 0},
			},
		},
		{
			name: "a deeper subtree is emitted before the parent's next sibling",
			areas: []config.AreaConfig{
				{Name: "web", Children: []config.AreaConfig{
					{Name: "settings", Children: []config.AreaConfig{
						{Name: "billing", Children: []config.AreaConfig{{Name: "invoices"}}},
					}},
					{Name: "dashboard"},
				}},
				{Name: "auth"},
			},
			want: []areaWire{
				{"web", 0},
				{"web/settings", 1},
				{"web/settings/billing", 2},
				{"web/settings/billing/invoices", 3},
				{"web/dashboard", 1},
				{"auth", 0},
			},
		},
		{
			// `web` and `webhooks` share a name prefix, which is what separates
			// the tree from the strings: a root named for a prefix of another
			// root stays a root, at depth 0 and outside the other's run.
			name: "a name-prefix sibling stays its own root",
			areas: []config.AreaConfig{
				{Name: "web", Children: []config.AreaConfig{{Name: "dashboard"}}},
				{Name: "webhooks"},
			},
			want: []areaWire{{"web", 0}, {"web/dashboard", 1}, {"webhooks", 0}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wireOf(resolveAreaList(t, areaCfg(tt.areas...)))
			if !slices.Equal(got, tt.want) {
				t.Errorf("areas = %v, want %v", got, tt.want)
			}
		})
	}
}

// The committed fixture is the vocabulary the web's own tests and the demo
// server read, so the resolver's answer for it is pinned here rather than left
// to whichever test happens to load it.
func TestConfigResolverFlattensTheSampleProjectVocabulary(t *testing.T) {
	got := wireOf(resolveAreaList(t, sampleProjectConfig(t)))
	want := []areaWire{
		{"auth", 0},
		{"api", 0},
		{"api/webhooks", 1},
		{"web", 0},
		{"web/dashboard", 1},
		{"infra", 0},
		{"docs", 0},
	}
	if !slices.Equal(got, want) {
		t.Errorf("areas = %v, want %v", got, want)
	}
}

// A store declaring no areas answers with an empty list. Declaring none is a
// normal, permanent state (config.AreasDeclared), not a failure, so the
// resolver must not error and must not omit the field.
//
// It says nothing about null-vs-[] on the wire, because that is not this
// function's to get wrong: `[Area!]!` is marshaled by gqlgen, whose list
// marshaller takes len() of whatever it is handed, so a nil slice and an empty
// one both serialize as []. Measured, with flattenAreas returning nil: the
// served response was {"data":{"config":{"areas":[]}}}, byte-identical to
// baseline.
func TestConfigResolverEmitsNoAreasAsAnEmptyList(t *testing.T) {
	got := resolveAreaList(t, areaCfg())
	if len(got) != 0 {
		t.Fatalf("areas = %v, want empty", wireOf(got))
	}
}

// TestAreasOrderingCarriesSubtreeMembership is the guard the whole wire shape
// rests on.
//
// `Area` carries no `children` field, so the only thing on the wire that says
// which nodes sit below a node is the ORDER: a node's subtree is the maximal
// run of following entries with a greater depth. A list emitted in any other
// order still type-checks and still carries every declared node — it just
// describes a different tree, silently.
//
// The expected sets are computed with config.IsAreaWithin, the same
// downward-closed rule the server's `area:` filter applies, rather than written
// out: that is what makes this an agreement between the two sides instead of a
// second transcription of one.
func TestAreasOrderingCarriesSubtreeMembership(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
	}{
		{
			name: "the sample-project vocabulary",
			cfg:  sampleProjectConfig(t),
		},
		{
			// `webhooks` must not fall inside `web`'s run. Nothing else in a
			// vocabulary tells a string-prefix test apart from genuine closure.
			name: "a name-prefix sibling root",
			cfg: areaCfg(
				config.AreaConfig{Name: "web", Children: []config.AreaConfig{
					{Name: "dashboard"},
					{Name: "settings", Children: []config.AreaConfig{{Name: "billing"}}},
				}},
				config.AreaConfig{Name: "webhooks"},
				config.AreaConfig{Name: "auth"},
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveAreaList(t, tt.cfg)

			declared := tt.cfg.AreaPaths()
			// Reported rather than fatal, so a run that gets the paths wrong
			// still shows which subtrees the order then names.
			if !slices.Equal(pathsOf(got), declared) {
				t.Errorf("paths = %v, want config.AreaPaths' order %v", pathsOf(got), declared)
			}

			for i, node := range got {
				byOrder := subtreeByOrder(got, i)
				byRule := make([]string, 0, len(declared))
				for _, path := range declared {
					if tt.cfg.IsAreaWithin(path, node.Path) {
						byRule = append(byRule, path)
					}
				}
				if !slices.Equal(byOrder, byRule) {
					t.Errorf("subtree of %q read from the order = %v, but IsAreaWithin gives %v",
						node.Path, byOrder, byRule)
				}
			}
		})
	}
}

// subtreeByOrder reads a node's subtree the way a client must: the node itself,
// then the maximal run of following entries with a greater depth.
func subtreeByOrder(list []*model.Area, i int) []string {
	out := []string{list[i].Path}
	for _, node := range list[i+1:] {
		if node.Depth <= list[i].Depth {
			break
		}
		out = append(out, node.Path)
	}
	return out
}

// Names, descriptions and colors cross verbatim. config.RenderAreaPath is a
// TERMINAL rendering boundary for CLI text; a JSON value decoded into a DOM text
// node is a different one, and stripping here would also change the `path` a
// client sends back as an `area:` filter argument, which the server matches
// against the declared vocabulary exactly.
//
// The fixture description carries markup, a quote and a BEL, so a description
// that had gone through safetext.Strip on the way out is visible here: Strip
// turns every non-printable rune into a space.
func TestConfigResolverEmitsAreaFieldsVerbatim(t *testing.T) {
	cfg := areaCfg(config.AreaConfig{
		Name:        "web",
		Description: "The <b>browser</b> client\a \u2014 \"quoted\"",
		Color:       "#0a7cff",
		Children:    []config.AreaConfig{{Name: "dashboard"}},
	})

	got := resolveAreaList(t, cfg)
	if len(got) != 2 {
		t.Fatalf("areas = %v, want 2 entries", wireOf(got))
	}
	root := got[0]
	if root.Name != "web" {
		t.Errorf("name = %q, want %q", root.Name, "web")
	}
	if root.Description != cfg.Areas[0].Description {
		t.Errorf("description = %q, want %q", root.Description, cfg.Areas[0].Description)
	}
	if root.Color != "#0a7cff" {
		t.Errorf("color = %q, want %q", root.Color, "#0a7cff")
	}
	// A child inherits nothing: an omitted description and color cross as empty
	// strings, which is what the non-nullable fields mean by "unset".
	if got[1].Description != "" || got[1].Color != "" {
		t.Errorf("child description/color = %q/%q, want both empty", got[1].Description, got[1].Color)
	}
}
