package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

// writeStoreConfig writes body as a store's config.yml and returns the store
// directory, so a test can reach the file through either Load or LoadFromStore.
func writeStoreConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yml"), []byte(body), 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	return dir
}

// sampleAreasConfig declares a vocabulary with two roots whose names share a
// prefix (`web` and `webhooks`), which is what separates tree descent from a
// string-prefix test.
const sampleAreasConfig = `nibs:
    prefix: t-
areas:
    - name: web
      description: The browser client
      color: blue
      children:
        - name: dashboard
          description: The landing dashboard
        - name: settings
          description: Project settings screens
    - name: webhooks
      description: Outbound webhook delivery
    - name: auth
      description: Sign-in and sessions
    - name: api
      children:
        - name: v2
`

func TestLoadRejectsMalformedAreas(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "duplicate top-level siblings",
			body: "areas:\n    - name: auth\n    - name: auth\n",
			want: []string{"duplicate", `"auth"`},
		},
		{
			name: "duplicate nested siblings",
			body: "areas:\n    - name: web\n      children:\n        - name: dashboard\n        - name: dashboard\n",
			want: []string{"duplicate", `"web/dashboard"`},
		},
		{
			name: "empty name",
			body: "areas:\n    - name: \"\"\n",
			want: []string{"area #1", "at the top level", "has no name"},
		},
		{
			name: "whitespace-only name",
			body: "areas:\n    - name: \"   \"\n",
			want: []string{"area #1", "at the top level", "has no name"},
		},
		{
			name: "missing name key",
			body: "areas:\n    - description: nameless\n",
			want: []string{"area #1", "at the top level", "has no name"},
		},
		{
			name: "name padded with whitespace",
			body: "areas:\n    - name: \" web \"\n",
			want: []string{`" web "`, "leading or trailing whitespace"},
		},
		{
			name: "nested name padded with whitespace",
			body: "areas:\n    - name: web\n      children:\n        - name: \"dashboard \"\n",
			want: []string{`"dashboard "`, `under "web"`, "leading or trailing whitespace"},
		},
		{
			name: "name carries the path separator",
			body: "areas:\n    - name: web/dashboard\n",
			want: []string{`"web/dashboard"`, `"/"`},
		},
		{
			name: "nested name carries the path separator",
			body: "areas:\n    - name: web\n      children:\n        - name: dash/board\n",
			want: []string{`"dash/board"`, `"/"`},
		},
		{
			name: "unusable hex color",
			body: "areas:\n    - name: web\n      color: \"#12x\"\n",
			want: []string{"color", `"#12x"`, `"web"`},
		},
		{
			name: "unusable named color",
			body: "areas:\n    - name: web\n      color: \"hot pink\"\n",
			want: []string{"color", `"hot pink"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeStoreConfig(t, tt.body)
			cfg, err := LoadFromStore(dir)
			if err == nil {
				t.Fatalf("LoadFromStore succeeded, want an error; areas = %v", cfg.AreaPaths())
			}
			msg := err.Error()
			if !strings.Contains(msg, "config.yml") {
				t.Errorf("error %q does not name the config file", msg)
			}
			// The fault is asserted on the UNWRAPPED error. The wrapper
			// interpolates the config path, and t.TempDir() derives that path from
			// the subtest name — so a want entry that also occurs in the name (the
			// word "name", say) would be satisfied by the path rather than by the
			// message, and the row would pass however the message read.
			detail := errors.Unwrap(err)
			if detail == nil {
				t.Fatalf("error %q does not wrap the validation fault", msg)
			}
			for _, want := range tt.want {
				if !strings.Contains(detail.Error(), want) {
					t.Errorf("validation error %q does not mention %s", detail, want)
				}
			}
		})
	}
}

func TestLoadAcceptsAbsentOrEmptyAreas(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "no areas key", body: "nibs:\n    prefix: t-\n"},
		{name: "empty sequence", body: "nibs:\n    prefix: t-\nareas: []\n"},
		{name: "null value", body: "nibs:\n    prefix: t-\nareas:\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeStoreConfig(t, tt.body)
			cfg, err := LoadFromStore(dir)
			if err != nil {
				t.Fatalf("LoadFromStore: %v", err)
			}
			if got := cfg.AreaPaths(); len(got) != 0 {
				t.Errorf("AreaPaths() = %v, want none", got)
			}
			if cfg.IsValidArea("web") {
				t.Error("IsValidArea(\"web\") = true with no declared vocabulary")
			}
		})
	}
}

func TestAreaPathsEnumeratesInDeclarationOrder(t *testing.T) {
	dir := writeStoreConfig(t, sampleAreasConfig)
	cfg, err := LoadFromStore(dir)
	if err != nil {
		t.Fatalf("LoadFromStore: %v", err)
	}

	want := []string{
		"web", "web/dashboard", "web/settings",
		"webhooks",
		"auth",
		"api", "api/v2",
	}
	if got := cfg.AreaPaths(); !slices.Equal(got, want) {
		t.Errorf("AreaPaths() = %v, want %v", got, want)
	}
	if got, want := cfg.AreaList(), strings.Join(want, ", "); got != want {
		t.Errorf("AreaList() = %q, want %q", got, want)
	}
}

func TestGetAreaResolvesDeclaredPaths(t *testing.T) {
	dir := writeStoreConfig(t, sampleAreasConfig)
	cfg, err := LoadFromStore(dir)
	if err != nil {
		t.Fatalf("LoadFromStore: %v", err)
	}

	tests := []struct {
		path            string
		wantFound       bool
		wantDescription string
	}{
		{path: "web", wantFound: true, wantDescription: "The browser client"},
		{path: "web/dashboard", wantFound: true, wantDescription: "The landing dashboard"},
		{path: "api/v2", wantFound: true},
		{path: "webhooks", wantFound: true, wantDescription: "Outbound webhook delivery"},
		{path: "dashboard"},
		{path: "web/missing"},
		{path: "web/dashboard/deeper"},
		{path: ""},
		{path: "web/"},
		{path: "/web"},
	}

	for _, tt := range tests {
		t.Run("path="+tt.path, func(t *testing.T) {
			node := cfg.GetArea(tt.path)
			if (node != nil) != tt.wantFound {
				t.Fatalf("GetArea(%q) found = %v, want %v", tt.path, node != nil, tt.wantFound)
			}
			if got := cfg.IsValidArea(tt.path); got != tt.wantFound {
				t.Errorf("IsValidArea(%q) = %v, want %v", tt.path, got, tt.wantFound)
			}
			if tt.wantFound && node.Description != tt.wantDescription {
				t.Errorf("GetArea(%q).Description = %q, want %q", tt.path, node.Description, tt.wantDescription)
			}
		})
	}

	if got := cfg.GetArea("web").Color; got != "blue" {
		t.Errorf("GetArea(\"web\").Color = %q, want \"blue\"", got)
	}
}

func TestIsAreaWithin(t *testing.T) {
	dir := writeStoreConfig(t, sampleAreasConfig)
	cfg, err := LoadFromStore(dir)
	if err != nil {
		t.Fatalf("LoadFromStore: %v", err)
	}

	tests := []struct {
		name     string
		path     string
		ancestor string
		want     bool
	}{
		{name: "a node is within itself", path: "web", ancestor: "web", want: true},
		{name: "a child is within its parent", path: "web/dashboard", ancestor: "web", want: true},
		{name: "a sibling sharing a name prefix is not within", path: "webhooks", ancestor: "web"},
		{name: "a parent is not within its child", path: "web", ancestor: "web/dashboard"},
		{name: "an unrelated root", path: "auth", ancestor: "web"},
		{name: "a grandchild path that is not declared", path: "web/dashboard/charts", ancestor: "web"},
		{name: "an undeclared path below a declared root", path: "web/legacy", ancestor: "web"},
		{name: "an undeclared ancestor", path: "web/dashboard", ancestor: "browser"},
		{name: "a nested node within its own root", path: "api/v2", ancestor: "api", want: true},
		{name: "an empty path", path: "", ancestor: "web"},
		{name: "an empty ancestor", path: "web", ancestor: ""},
		{name: "both empty", path: "", ancestor: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cfg.IsAreaWithin(tt.path, tt.ancestor); got != tt.want {
				t.Errorf("IsAreaWithin(%q, %q) = %v, want %v", tt.path, tt.ancestor, got, tt.want)
			}
		})
	}
}

// TestSaveRoundTripsAreas pins the second top-level block through Save. Config
// serialized exactly one key until areas arrived, so nothing had ever proved
// that a whole-config write emits a sibling of `nibs:` and reads it back.
func TestSaveRoundTripsAreas(t *testing.T) {
	cfg := Default()
	cfg.Nibs.Prefix = "t-"
	cfg.Areas = []AreaConfig{
		{
			Name:        "web",
			Description: "The browser client",
			Color:       "blue",
			Order:       "a",
			Children: []AreaConfig{
				{Name: "dashboard", Description: "The landing dashboard", Order: "a0"},
			},
		},
		{Name: "auth", Description: "Sign-in and sessions"},
	}

	dir := t.TempDir()
	if _, err := cfg.Save(dir); err != nil {
		t.Fatalf("Save: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "config.yml"))
	if err != nil {
		t.Fatalf("reading saved config: %v", err)
	}
	if !strings.Contains(string(raw), "\nareas:\n") {
		t.Errorf("saved config has no top-level areas block:\n%s", raw)
	}

	got, err := LoadFromStore(dir)
	if err != nil {
		t.Fatalf("LoadFromStore: %v", err)
	}
	if got.Nibs.Prefix != "t-" {
		t.Errorf("Prefix = %q, want \"t-\"", got.Nibs.Prefix)
	}
	wantPaths := []string{"web", "web/dashboard", "auth"}
	if paths := got.AreaPaths(); !slices.Equal(paths, wantPaths) {
		t.Fatalf("AreaPaths() = %v, want %v", paths, wantPaths)
	}
	dashboard := got.GetArea("web/dashboard")
	if dashboard.Description != "The landing dashboard" || dashboard.Order != "a0" {
		t.Errorf("web/dashboard = %+v, want the description and order that were saved", *dashboard)
	}
	if web := got.GetArea("web"); web.Color != "blue" || web.Order != "a" {
		t.Errorf("web = %+v, want color \"blue\" and order \"a\"", *web)
	}
}

// TestSaveOmitsAreasWhenNoneAreDeclared keeps `nibs init` writing a config with
// no areas block: a project that has not declared a vocabulary is a normal
// project, and an empty `areas:` key would read as a deliberate one.
func TestSaveOmitsAreasWhenNoneAreDeclared(t *testing.T) {
	dir := t.TempDir()
	if _, err := DefaultWithPrefix("t-").Save(dir); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "config.yml"))
	if err != nil {
		t.Fatalf("reading saved config: %v", err)
	}
	if strings.Contains(string(raw), "areas") {
		t.Errorf("saved config mentions areas:\n%s", raw)
	}
}

// TestSetStoredPrefixPreservesAreas guards the in-place key edit against the
// new block: it rewrites through a yaml.Node tree precisely so unmodeled and
// untouched keys survive, and areas is the largest thing it has to carry.
func TestSetStoredPrefixPreservesAreas(t *testing.T) {
	dir := writeStoreConfig(t, sampleAreasConfig)
	if _, err := SetStoredPrefix(dir, "new-"); err != nil {
		t.Fatalf("SetStoredPrefix: %v", err)
	}

	cfg, err := LoadFromStore(dir)
	if err != nil {
		t.Fatalf("LoadFromStore: %v", err)
	}
	if cfg.Nibs.Prefix != "new-" {
		t.Errorf("Prefix = %q, want \"new-\"", cfg.Nibs.Prefix)
	}
	want := []string{"web", "web/dashboard", "web/settings", "webhooks", "auth", "api", "api/v2"}
	if got := cfg.AreaPaths(); !slices.Equal(got, want) {
		t.Errorf("AreaPaths() = %v, want %v", got, want)
	}
	if got := cfg.GetArea("web/dashboard"); got == nil || got.Description != "The landing dashboard" {
		t.Errorf("web/dashboard lost its description after SetStoredPrefix: %+v", got)
	}
}

func TestValidateAreasAcceptsWellFormedColors(t *testing.T) {
	for _, color := range []string{"", "blue", "lightgray", "#abc", "#AABBCC", "#aabbccdd", "#1234"} {
		cfg := &Config{Areas: []AreaConfig{{Name: "web", Color: color}}}
		if err := cfg.ValidateAreas(); err != nil {
			t.Errorf("ValidateAreas with color %q: %v", color, err)
		}
	}
}

// TestAreaListRendersFileSourcedNames keeps AreaList a rendering boundary rather
// than a raw echo. Areas are the one vocabulary in this package a PROJECT
// authors, so a declared name is file-sourced text of arbitrary length and
// content — an escape sequence, a backtick that closes the code span a message
// put around the list, a newline that turns one line into several.
func TestAreaListRendersFileSourcedNames(t *testing.T) {
	dir := writeStoreConfig(t, "areas:\n    - name: \"a\\eb\"\n    - name: \"c`d\"\n    - name: \"e\\nf\"\n")
	cfg, err := LoadFromStore(dir)
	if err != nil {
		t.Fatalf("LoadFromStore: %v", err)
	}

	if got, want := cfg.AreaList(), "a b, c d, e f"; got != want {
		t.Errorf("AreaList() = %q, want %q", got, want)
	}
	// AreaPaths is the data accessor and must stay verbatim: resolution compares
	// a nib's `area:` value against it byte for byte.
	wantPaths := []string{"a\x1bb", "c`d", "e\nf"}
	if got := cfg.AreaPaths(); !slices.Equal(got, wantPaths) {
		t.Errorf("AreaPaths() = %q, want %q", got, wantPaths)
	}
}

// TestAreaListBoundsWhatItRepeats pins the two bounds on the same message: how
// many paths it enumerates and how much of one it repeats. Without them a config
// declaring a thousand areas, or one area named a megabyte of text, is repeated
// in full by every message that names the vocabulary.
func TestAreaListBoundsWhatItRepeats(t *testing.T) {
	t.Run("entry count", func(t *testing.T) {
		areas := make([]AreaConfig, maxListedAreas+5)
		for i := range areas {
			areas[i] = AreaConfig{Name: fmt.Sprintf("a%03d", i)}
		}
		cfg := &Config{Areas: areas}

		got := cfg.AreaList()
		if !strings.HasSuffix(got, "…and 5 more") {
			t.Errorf("AreaList() = %q, want it to end by stating the elided count", got)
		}
		if !strings.Contains(got, fmt.Sprintf("a%03d", maxListedAreas-1)) {
			t.Errorf("AreaList() = %q, want the first %d paths listed", got, maxListedAreas)
		}
		if first := fmt.Sprintf("a%03d", maxListedAreas); strings.Contains(got, first) {
			t.Errorf("AreaList() = %q, want %q elided", got, first)
		}
	})

	// Exactly at each bound is where an off-by-one lives: a store declaring
	// maxListedAreas areas must list them all and claim no remainder, and a name
	// of exactly maxListedAreaRunes must survive unmarked.
	t.Run("entry count exactly at the bound", func(t *testing.T) {
		areas := make([]AreaConfig, maxListedAreas)
		for i := range areas {
			areas[i] = AreaConfig{Name: fmt.Sprintf("a%03d", i)}
		}
		cfg := &Config{Areas: areas}

		got := cfg.AreaList()
		if strings.Contains(got, "more") {
			t.Errorf("AreaList() = %q, want no elision claim when every path fits", got)
		}
		if last := fmt.Sprintf("a%03d", maxListedAreas-1); !strings.Contains(got, last) {
			t.Errorf("AreaList() = %q, want the last path %q listed", got, last)
		}
	})

	t.Run("one path's length exactly at the bound", func(t *testing.T) {
		name := strings.Repeat("x", maxListedAreaRunes)
		cfg := &Config{Areas: []AreaConfig{{Name: name}}}

		got := cfg.AreaList()
		if got != name {
			t.Errorf("AreaList() = %q, want %q returned whole and unmarked", got, name)
		}
	})

	t.Run("one path's length", func(t *testing.T) {
		cfg := &Config{Areas: []AreaConfig{{Name: strings.Repeat("x", maxListedAreaRunes+50)}}}

		got := cfg.AreaList()
		if n := utf8.RuneCountInString(got); n != maxListedAreaRunes+1 {
			t.Errorf("AreaList() is %d runes, want %d plus the truncation marker", n, maxListedAreaRunes)
		}
		if !strings.HasSuffix(got, "…") {
			t.Errorf("AreaList() = %q, want the truncation marked", got)
		}
	})
}
