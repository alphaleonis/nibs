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

// sampleAreasConfig declares a vocabulary with two roots whose names share a
// prefix (`web` and `webhooks`), which is what separates tree descent from a
// string-prefix test.
const sampleAreasConfig = `areas:
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
			dir := writeStoreAreas(t, tt.body)
			areas, err := LoadAreasFromStore(dir)
			if err == nil {
				t.Fatalf("LoadAreasFromStore succeeded, want an error; areas = %v", areas.Paths())
			}
			msg := err.Error()
			if !strings.Contains(msg, "areas.yml") {
				t.Errorf("error %q does not name the areas file", msg)
			}
			// The fault is asserted on the UNWRAPPED error. The wrapper
			// interpolates the file's path, and t.TempDir() derives that path from
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
			dir := writeStoreAreas(t, tt.body)
			cfg, err := LoadAreasFromStore(dir)
			if err != nil {
				t.Fatalf("LoadFromStore: %v", err)
			}
			if got := cfg.Paths(); len(got) != 0 {
				t.Errorf("AreaPaths() = %v, want none", got)
			}
			if cfg.IsValid("web") {
				t.Error("IsValidArea(\"web\") = true with no declared vocabulary")
			}
		})
	}
}

func TestAreaPathsEnumeratesInDeclarationOrder(t *testing.T) {
	dir := writeStoreAreas(t, sampleAreasConfig)
	cfg, err := LoadAreasFromStore(dir)
	if err != nil {
		t.Fatalf("LoadFromStore: %v", err)
	}

	want := []string{
		"web", "web/dashboard", "web/settings",
		"webhooks",
		"auth",
		"api", "api/v2",
	}
	if got := cfg.Paths(); !slices.Equal(got, want) {
		t.Errorf("AreaPaths() = %v, want %v", got, want)
	}
	if got, want := cfg.List(), strings.Join(want, ", "); got != want {
		t.Errorf("AreaList() = %q, want %q", got, want)
	}
}

func TestGetAreaResolvesDeclaredPaths(t *testing.T) {
	dir := writeStoreAreas(t, sampleAreasConfig)
	cfg, err := LoadAreasFromStore(dir)
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
			node := cfg.Get(tt.path)
			if (node != nil) != tt.wantFound {
				t.Fatalf("GetArea(%q) found = %v, want %v", tt.path, node != nil, tt.wantFound)
			}
			if got := cfg.IsValid(tt.path); got != tt.wantFound {
				t.Errorf("IsValidArea(%q) = %v, want %v", tt.path, got, tt.wantFound)
			}
			if tt.wantFound && node.Description != tt.wantDescription {
				t.Errorf("GetArea(%q).Description = %q, want %q", tt.path, node.Description, tt.wantDescription)
			}
		})
	}

	if got := cfg.Get("web").Color; got != "blue" {
		t.Errorf("GetArea(\"web\").Color = %q, want \"blue\"", got)
	}
}

func TestIsAreaWithin(t *testing.T) {
	dir := writeStoreAreas(t, sampleAreasConfig)
	cfg, err := LoadAreasFromStore(dir)
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
			if got := cfg.IsWithin(tt.path, tt.ancestor); got != tt.want {
				t.Errorf("IsAreaWithin(%q, %q) = %v, want %v", tt.path, tt.ancestor, got, tt.want)
			}
		})
	}
}

// TestAreasSaveRoundTrips pins the vocabulary through its own writer: what
// Save emits, LoadAreasFromStore must read back node for node.
func TestAreasSaveRoundTrips(t *testing.T) {
	areas := &Areas{Nodes: []AreaConfig{
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
	}}

	dir := t.TempDir()
	if err := areas.Save(dir); err != nil {
		t.Fatalf("Save: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "areas.yml"))
	if err != nil {
		t.Fatalf("reading saved areas: %v", err)
	}
	if !strings.Contains(string(raw), "areas:\n") {
		t.Errorf("saved areas file has no top-level areas block:\n%s", raw)
	}

	got, err := LoadAreasFromStore(dir)
	if err != nil {
		t.Fatalf("LoadAreasFromStore: %v", err)
	}
	wantPaths := []string{"web", "web/dashboard", "auth"}
	if paths := got.Paths(); !slices.Equal(paths, wantPaths) {
		t.Fatalf("Paths() = %v, want %v", paths, wantPaths)
	}
	dashboard := got.Get("web/dashboard")
	if dashboard.Description != "The landing dashboard" || dashboard.Order != "a0" {
		t.Errorf("web/dashboard = %+v, want the description and order that were saved", *dashboard)
	}
}

// A vocabulary that declares nothing REMOVES the file rather than writing an
// empty `areas:` key, so "declares nothing" has one shape on disk.
func TestAreasSaveRemovesTheFileWhenNothingIsDeclared(t *testing.T) {
	dir := writeStoreAreas(t, "areas:\n    - name: web\n")

	if err := (&Areas{}).Save(dir); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "areas.yml")); !os.IsNotExist(err) {
		t.Errorf("areas.yml still present after saving an empty vocabulary (stat err = %v)", err)
	}
}

// TestSetStoredPrefixLeavesTheVocabularyAlone pins the independence the split
// bought: the prefix editor rewrites config.yml, and areas.yml is not its file
// to touch.
func TestSetStoredPrefixLeavesTheVocabularyAlone(t *testing.T) {
	dir := writeStoreAreas(t, sampleAreasConfig)
	if err := os.WriteFile(filepath.Join(dir, "config.yml"), []byte("nibs:\n    prefix: t-\n"), 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(dir, "areas.yml"))
	if err != nil {
		t.Fatalf("reading areas: %v", err)
	}

	if _, err := SetStoredPrefix(dir, "new-"); err != nil {
		t.Fatalf("SetStoredPrefix: %v", err)
	}

	after, err := os.ReadFile(filepath.Join(dir, "areas.yml"))
	if err != nil {
		t.Fatalf("reading areas after the edit: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("SetStoredPrefix rewrote areas.yml:\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}

	cfg, err := LoadFromStore(dir)
	if err != nil {
		t.Fatalf("LoadFromStore: %v", err)
	}
	if cfg.Nibs.Prefix != "new-" {
		t.Errorf("Prefix = %q, want \"new-\"", cfg.Nibs.Prefix)
	}
}

func TestValidateAreasAcceptsWellFormedColors(t *testing.T) {
	for _, color := range []string{"", "blue", "lightgray", "#abc", "#AABBCC", "#aabbccdd", "#1234"} {
		areas := &Areas{Nodes: []AreaConfig{{Name: "web", Color: color}}}
		if err := areas.Validate(); err != nil {
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
	dir := writeStoreAreas(t, "areas:\n    - name: \"a\\eb\"\n    - name: \"c`d\"\n    - name: \"e\\nf\"\n")
	areas, err := LoadAreasFromStore(dir)
	if err != nil {
		t.Fatalf("LoadAreasFromStore: %v", err)
	}

	if got, want := areas.List(), "a b, c d, e f"; got != want {
		t.Errorf("AreaList() = %q, want %q", got, want)
	}
	// AreaPaths is the data accessor and must stay verbatim: resolution compares
	// a nib's `area:` value against it byte for byte.
	wantPaths := []string{"a\x1bb", "c`d", "e\nf"}
	if got := areas.Paths(); !slices.Equal(got, wantPaths) {
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
		vocab := &Areas{Nodes: areas}

		got := vocab.List()
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
		vocab := &Areas{Nodes: areas}

		got := vocab.List()
		if strings.Contains(got, "more") {
			t.Errorf("AreaList() = %q, want no elision claim when every path fits", got)
		}
		if last := fmt.Sprintf("a%03d", maxListedAreas-1); !strings.Contains(got, last) {
			t.Errorf("AreaList() = %q, want the last path %q listed", got, last)
		}
	})

	t.Run("one path's length exactly at the bound", func(t *testing.T) {
		name := strings.Repeat("x", maxListedAreaRunes)
		vocab := &Areas{Nodes: []AreaConfig{{Name: name}}}

		got := vocab.List()
		if got != name {
			t.Errorf("AreaList() = %q, want %q returned whole and unmarked", got, name)
		}
	})

	t.Run("one path's length", func(t *testing.T) {
		vocab := &Areas{Nodes: []AreaConfig{{Name: strings.Repeat("x", maxListedAreaRunes+50)}}}

		got := vocab.List()
		if n := utf8.RuneCountInString(got); n != maxListedAreaRunes+1 {
			t.Errorf("AreaList() is %d runes, want %d plus the truncation marker", n, maxListedAreaRunes)
		}
		if !strings.HasSuffix(got, "…") {
			t.Errorf("AreaList() = %q, want the truncation marked", got)
		}
	})
}

// TestValidateAreaAssignment pins the write-side membership rule: an unset
// `area:` is legal, a declared path is legal, and anything else is refused with
// the vocabulary in the message. The store that declares NOTHING is the case
// worth its own row — an empty allowed set reads as a bug in nibs rather than
// as an undeclared vocabulary, so the refusal has to say which it is.
func TestValidateAreaAssignment(t *testing.T) {
	dir := writeStoreAreas(t, sampleAreasConfig)
	declared, err := LoadAreasFromStore(dir)
	if err != nil {
		t.Fatalf("LoadFromStore: %v", err)
	}
	none := &Areas{}

	tests := []struct {
		name     string
		cfg      *Areas
		path     string
		wantErr  bool
		contains []string
		absent   []string
	}{
		{name: "unset is legal", cfg: declared, path: ""},
		{name: "unset is legal with no vocabulary", cfg: none, path: ""},
		{name: "a declared root", cfg: declared, path: "web"},
		{name: "a declared child", cfg: declared, path: "web/dashboard"},
		{
			name: "an undeclared path names the vocabulary", cfg: declared, path: "nosuch",
			wantErr:  true,
			contains: []string{`"nosuch"`, "web/dashboard", "webhooks"},
		},
		{
			// `web/legacy` descends a declared root, so a string-prefix test
			// would admit it; only the tree says it is not declared.
			name: "an undeclared child of a declared root", cfg: declared, path: "web/legacy",
			wantErr:  true,
			contains: []string{`"web/legacy"`},
		},
		{
			name: "a store with no declared areas says so", cfg: none, path: "web",
			wantErr:  true,
			contains: []string{`"web"`, "declares no areas"},
			// An empty allowed set must not be printed as though it were one.
			absent: []string{"must be one of"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.ValidateAssignment(tt.path)
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("ValidateAreaAssignment(%q) = %v, want nil", tt.path, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateAreaAssignment(%q) = nil, want an error", tt.path)
			}
			for _, want := range tt.contains {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want substring %q", err.Error(), want)
				}
			}
			for _, unwanted := range tt.absent {
				if strings.Contains(err.Error(), unwanted) {
					t.Errorf("error = %q, want it NOT to contain %q", err.Error(), unwanted)
				}
			}
		})
	}
}

// TestValidateStoredArea pins the second entry point: the SAME rule, for a write
// to a nib that already exists, where the value being judged need not have come
// from the request. It must agree with ValidateAreaAssignment on every accept
// and every refuse, and differ only in saying whose value it is and what to do
// about it — a caller who passed no area is otherwise told to correct an
// argument they never wrote.
func TestValidateStoredArea(t *testing.T) {
	dir := writeStoreAreas(t, sampleAreasConfig)
	declared, err := LoadAreasFromStore(dir)
	if err != nil {
		t.Fatalf("LoadFromStore: %v", err)
	}
	none := &Areas{}

	// The verdicts must not diverge, so they are asserted against the sibling
	// rather than restated: a rule that accepted here and refused there would
	// make the message depend on which write path reached it.
	for _, tc := range []struct {
		vocab string
		cfg   *Areas
		path  string
	}{
		{"a declared vocabulary", declared, ""}, {"no declared areas", none, ""},
		{"a declared vocabulary", declared, "web"}, {"a declared vocabulary", declared, "web/dashboard"},
		{"a declared vocabulary", declared, "nosuch"}, {"a declared vocabulary", declared, "web/legacy"},
		{"no declared areas", none, "web"},
	} {
		t.Run(fmt.Sprintf("agrees with the supplied-value rule on %q under %s", tc.path, tc.vocab), func(t *testing.T) {
			supplied := tc.cfg.ValidateAssignment(tc.path)
			stored := tc.cfg.ValidateStored("cfg-n001", tc.path)
			if (supplied == nil) != (stored == nil) {
				t.Fatalf("ValidateAreaAssignment(%q) = %v but ValidateStoredArea(%q) = %v", tc.path, supplied, tc.path, stored)
			}
		})
	}

	for _, tt := range []struct {
		name     string
		cfg      *Areas
		path     string
		contains []string
		absent   []string
	}{
		{
			name: "an undeclared path names the vocabulary and whose value it is",
			cfg:  declared, path: "nosuch",
			contains: []string{`"nosuch"`, "web/dashboard", "already carries",
				"`nibs set cfg-n001 --area <declared>`", "`nibs set cfg-n001 --clear area`"},
			// The subject is known here, so it is interpolated: a literal <id>
			// exits 3 for anyone who runs the line as printed.
			absent: []string{"nibs set <id>"},
		},
		{
			name: "a store with no declared areas names only the escape it can satisfy",
			cfg:  none, path: "web",
			contains: []string{`"web"`, "declares no areas", "already carries", "`nibs set cfg-n001 --clear area`"},
			// An empty allowed set must not be printed as though it were one,
			// and --area has no satisfiable argument in the state this branch
			// diagnoses — prescribing it would name an unfollowable command.
			absent: []string{"must be one of", "--area <declared>", "nibs set <id>"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.ValidateStored("cfg-n001", tt.path)
			if err == nil {
				t.Fatalf("ValidateStoredArea(%q) = nil, want an error", tt.path)
			}
			var areaErr *AreaError
			if !errors.As(err, &areaErr) {
				t.Fatalf("error = %T, want *AreaError — the ordering backfill classifies this refusal by type", err)
			}
			for _, want := range tt.contains {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want substring %q", err.Error(), want)
				}
			}
			for _, unwanted := range tt.absent {
				if strings.Contains(err.Error(), unwanted) {
					t.Errorf("error = %q, want it NOT to contain %q", err.Error(), unwanted)
				}
			}
		})
	}

	// The refused value is file-sourced on this path by definition, so it stays
	// on the same rendering boundary the supplied-value refusal applies.
	t.Run("the refused value is rendered, not echoed raw", func(t *testing.T) {
		areas := &Areas{Nodes: []AreaConfig{{Name: "web"}}}
		err := areas.ValidateStored("cfg-n001", "we`b")
		if err == nil {
			t.Fatal("ValidateStoredArea() = nil, want an error")
		}
		if strings.Contains(err.Error(), "we`b") {
			t.Errorf("error = %q, want the backtick substituted", err.Error())
		}
	})

	// The id is interpolated into a command the reader is invited to run, and
	// it comes from a filename — so it crosses the same boundary the value does.
	t.Run("the nib id is rendered, not echoed raw", func(t *testing.T) {
		areas := &Areas{Nodes: []AreaConfig{{Name: "web"}}}
		err := areas.ValidateStored("nib\x1b[31m1", "nosuch")
		if err == nil {
			t.Fatal("ValidateStoredArea() = nil, want an error")
		}
		if strings.Contains(err.Error(), "\x1b") {
			t.Errorf("error = %q, want the escape sequence neutralized", err.Error())
		}
	})
}

// TestValidateAreaAssignmentRendersTheRefusedValue keeps the refused value on
// the same rendering boundary AreaList applies to the declared set. The value
// reaching this rule is not always a flag: Core.Update re-checks the `area:` a
// nib already carries, so a hostile FILE reaches the message too.
func TestValidateAreaAssignmentRendersTheRefusedValue(t *testing.T) {
	areas := &Areas{Nodes: []AreaConfig{{Name: "web"}}}

	// The backtick is the rune the %q around the value does NOT answer: it is
	// printable, so strconv.Quote passes it through, and it closes the code span
	// an agent transcript renders the message inside. safetext.Strip is what
	// substitutes it (non-printables are already covered by %q — see
	// internal/safetext).
	t.Run("a backtick cannot close the message's code span", func(t *testing.T) {
		err := areas.ValidateAssignment("we`b")
		if err == nil {
			t.Fatal("ValidateAreaAssignment accepted an undeclared path")
		}
		if strings.Contains(err.Error(), "`") {
			t.Errorf("error = %q, want the backtick neutralized", err.Error())
		}
	})

	t.Run("an oversized value is bounded", func(t *testing.T) {
		err := areas.ValidateAssignment(strings.Repeat("x", maxListedAreaRunes*4))
		if err == nil {
			t.Fatal("ValidateAreaAssignment accepted an undeclared path")
		}
		if utf8.RuneCountInString(err.Error()) > maxListedAreaRunes*2 {
			t.Errorf("error is %d runes; want the echoed value bounded", utf8.RuneCountInString(err.Error()))
		}
	})
}
