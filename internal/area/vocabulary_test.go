package area

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/alphaleonis/nibs/internal/safetext"
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

// parseVocab parses body as an areas.yml, failing the test on a refusal.
func parseVocab(t *testing.T, body string) *Vocabulary {
	t.Helper()
	vocab, err := Parse([]byte(body))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return vocab
}

func TestParseRejectsMalformedAreas(t *testing.T) {
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
			vocab, err := Parse([]byte(tt.body))
			if err == nil {
				t.Fatalf("Parse succeeded, want an error; areas = %v", vocab.Paths())
			}
			var parseErr *ParseError
			if !errors.As(err, &parseErr) || !parseErr.Malformed {
				t.Fatalf("error = %v (%T), want a *ParseError marked Malformed", err, err)
			}
			// The fault is asserted on the UNWRAPPED error, so a want entry that
			// also occurs in the wrapper's own sentence cannot satisfy the row.
			detail := errors.Unwrap(err)
			if detail == nil {
				t.Fatalf("error %q does not wrap the validation fault", err)
			}
			for _, want := range tt.want {
				if !strings.Contains(detail.Error(), want) {
					t.Errorf("validation error %q does not mention %s", detail, want)
				}
			}
		})
	}
}

func TestParseAcceptsAbsentOrEmptyAreas(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "empty input", body: ""},
		{name: "no areas key", body: "nibs:\n    prefix: t-\n"},
		{name: "empty sequence", body: "nibs:\n    prefix: t-\nareas: []\n"},
		{name: "null value", body: "nibs:\n    prefix: t-\nareas:\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vocab := parseVocab(t, tt.body)
			if got := vocab.Paths(); len(got) != 0 {
				t.Errorf("Paths() = %v, want none", got)
			}
			if vocab.Exists("web") {
				t.Error("Exists(\"web\") = true with no declared vocabulary")
			}
		})
	}
}

// TestAreaPathsEnumerateSiblingsByName pins the order every surface renders a
// vocabulary in: siblings by name, each parent immediately before the subtree it
// heads. The file declares its roots and one set of children out of that order,
// so an enumeration in file order fails here.
func TestAreaPathsEnumerateSiblingsByName(t *testing.T) {
	vocab := parseVocab(t, sampleAreasConfig)

	want := []string{
		"api", "api/v2",
		"auth",
		"web", "web/dashboard", "web/settings",
		"webhooks",
	}
	if got := vocab.Paths(); !slices.Equal(got, want) {
		t.Errorf("Paths() = %v, want %v", got, want)
	}
	if got, want := vocab.List(), strings.Join(want, ", "); got != want {
		t.Errorf("List() = %q, want %q", got, want)
	}

	t.Run("children and letter case", func(t *testing.T) {
		vocab := parseVocab(t, "areas:\n    - name: web\n      children:\n        - name: settings\n        - name: Dashboard\n    - name: Infra\n    - name: api\n")
		want := []string{"api", "Infra", "web", "web/Dashboard", "web/settings"}
		if got := vocab.Paths(); !slices.Equal(got, want) {
			t.Errorf("Paths() = %v, want %v", got, want)
		}
	})
}

func TestGetAreaResolvesDeclaredPaths(t *testing.T) {
	vocab := parseVocab(t, sampleAreasConfig)

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
			node := vocab.Get(tt.path)
			if (node != nil) != tt.wantFound {
				t.Fatalf("Get(%q) found = %v, want %v", tt.path, node != nil, tt.wantFound)
			}
			if got := vocab.Exists(tt.path); got != tt.wantFound {
				t.Errorf("Exists(%q) = %v, want %v", tt.path, got, tt.wantFound)
			}
			if tt.wantFound && node.Description != tt.wantDescription {
				t.Errorf("Get(%q).Description = %q, want %q", tt.path, node.Description, tt.wantDescription)
			}
		})
	}

	if got := vocab.Get("web").Color; got != "blue" {
		t.Errorf("Get(\"web\").Color = %q, want \"blue\"", got)
	}
}

func TestVocabularyIsWithin(t *testing.T) {
	vocab := parseVocab(t, sampleAreasConfig)

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
			if got := vocab.IsWithin(tt.path, tt.ancestor); got != tt.want {
				t.Errorf("IsWithin(%q, %q) = %v, want %v", tt.path, tt.ancestor, got, tt.want)
			}
		})
	}
}

func TestJoinAndSplitPathAreInverses(t *testing.T) {
	tests := []struct {
		parent, name, path string
	}{
		{parent: "", name: "web", path: "web"},
		{parent: "web", name: "dashboard", path: "web/dashboard"},
		{parent: "web/dashboard", name: "charts", path: "web/dashboard/charts"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := JoinPath(tt.parent, tt.name); got != tt.path {
				t.Errorf("JoinPath(%q, %q) = %q, want %q", tt.parent, tt.name, got, tt.path)
			}
			parent, name := SplitPath(tt.path)
			if parent != tt.parent || name != tt.name {
				t.Errorf("SplitPath(%q) = (%q, %q), want (%q, %q)", tt.path, parent, name, tt.parent, tt.name)
			}
		})
	}
}

func TestValidateAcceptsWellFormedColors(t *testing.T) {
	for _, color := range []string{"", "blue", "lightgray", "#abc", "#AABBCC", "#aabbccdd", "#1234"} {
		vocab := &Vocabulary{Nodes: []Node{{Name: "web", Color: color}}}
		if err := vocab.Validate(); err != nil {
			t.Errorf("Validate with color %q: %v", color, err)
		}
	}
}

// TestAreaListRendersFileSourcedNames keeps List a rendering boundary rather
// than a raw echo. Areas are the one vocabulary a PROJECT authors, so a declared
// name is file-sourced text of arbitrary length and content — an escape
// sequence, a backtick that closes the code span a message put around the list,
// a newline that turns one line into several.
func TestAreaListRendersFileSourcedNames(t *testing.T) {
	vocab := parseVocab(t, "areas:\n    - name: \"a\\eb\"\n    - name: \"c`d\"\n    - name: \"e\\nf\"\n")

	if got, want := vocab.List(), "a b, c d, e f"; got != want {
		t.Errorf("List() = %q, want %q", got, want)
	}
	// Paths is the data accessor and must stay verbatim: resolution compares
	// a nib's `area:` value against it byte for byte.
	wantPaths := []string{"a\x1bb", "c`d", "e\nf"}
	if got := vocab.Paths(); !slices.Equal(got, wantPaths) {
		t.Errorf("Paths() = %q, want %q", got, wantPaths)
	}
}

// TestAreaListBoundsWhatItRepeats pins the two bounds on the same message: how
// many paths it enumerates and how much of one it repeats. Without them a
// vocabulary declaring a thousand areas, or one area named a megabyte of text,
// is repeated in full by every message that names the vocabulary.
func TestAreaListBoundsWhatItRepeats(t *testing.T) {
	t.Run("entry count", func(t *testing.T) {
		nodes := make([]Node, maxListedAreas+5)
		for i := range nodes {
			nodes[i] = Node{Name: fmt.Sprintf("a%03d", i)}
		}
		vocab := &Vocabulary{Nodes: nodes}

		got := vocab.List()
		if !strings.HasSuffix(got, "…and 5 more") {
			t.Errorf("List() = %q, want it to end by stating the elided count", got)
		}
		if !strings.Contains(got, fmt.Sprintf("a%03d", maxListedAreas-1)) {
			t.Errorf("List() = %q, want the first %d paths listed", got, maxListedAreas)
		}
		if first := fmt.Sprintf("a%03d", maxListedAreas); strings.Contains(got, first) {
			t.Errorf("List() = %q, want %q elided", got, first)
		}
	})

	// Exactly at each bound is where an off-by-one lives: a store declaring
	// maxListedAreas areas must list them all and claim no remainder, and a name
	// of exactly safetext.MaxBoundedRunes must survive unmarked.
	t.Run("entry count exactly at the bound", func(t *testing.T) {
		nodes := make([]Node, maxListedAreas)
		for i := range nodes {
			nodes[i] = Node{Name: fmt.Sprintf("a%03d", i)}
		}
		vocab := &Vocabulary{Nodes: nodes}

		got := vocab.List()
		if strings.Contains(got, "more") {
			t.Errorf("List() = %q, want no elision claim when every path fits", got)
		}
		if last := fmt.Sprintf("a%03d", maxListedAreas-1); !strings.Contains(got, last) {
			t.Errorf("List() = %q, want the last path %q listed", got, last)
		}
	})

	t.Run("one path's length exactly at the bound", func(t *testing.T) {
		name := strings.Repeat("x", safetext.MaxBoundedRunes)
		vocab := &Vocabulary{Nodes: []Node{{Name: name}}}

		got := vocab.List()
		if got != name {
			t.Errorf("List() = %q, want %q returned whole and unmarked", got, name)
		}
	})

	t.Run("one path's length", func(t *testing.T) {
		vocab := &Vocabulary{Nodes: []Node{{Name: strings.Repeat("x", safetext.MaxBoundedRunes+50)}}}

		got := vocab.List()
		if n := utf8.RuneCountInString(got); n != safetext.MaxBoundedRunes+1 {
			t.Errorf("List() is %d runes, want %d plus the truncation marker", n, safetext.MaxBoundedRunes)
		}
		if !strings.HasSuffix(got, "…") {
			t.Errorf("List() = %q, want the truncation marked", got)
		}
	})
}

// TestValidateAssignment pins the write-side membership rule: an unset `area:`
// is legal, a declared path is legal, and anything else is refused with the
// vocabulary in the message. The store that declares NOTHING is the case worth
// its own row — an empty allowed set reads as a bug in nibs rather than as an
// undeclared vocabulary, so the refusal has to say which it is.
func TestValidateAssignment(t *testing.T) {
	declared := parseVocab(t, sampleAreasConfig)
	none := &Vocabulary{}

	tests := []struct {
		name     string
		vocab    *Vocabulary
		path     string
		wantErr  bool
		contains []string
		absent   []string
	}{
		{name: "unset is legal", vocab: declared, path: ""},
		{name: "unset is legal with no vocabulary", vocab: none, path: ""},
		{name: "a declared root", vocab: declared, path: "web"},
		{name: "a declared child", vocab: declared, path: "web/dashboard"},
		{
			name: "an undeclared path names the vocabulary", vocab: declared, path: "nosuch",
			wantErr:  true,
			contains: []string{`"nosuch"`, "web/dashboard", "webhooks"},
		},
		{
			// `web/legacy` descends a declared root, so a string-prefix test
			// would admit it; only the tree says it is not declared.
			name: "an undeclared child of a declared root", vocab: declared, path: "web/legacy",
			wantErr:  true,
			contains: []string{`"web/legacy"`},
		},
		{
			name: "a store with no declared areas says so", vocab: none, path: "web",
			wantErr:  true,
			contains: []string{`"web"`, "declares no areas"},
			// An empty allowed set must not be printed as though it were one.
			absent: []string{"must be one of"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.vocab.ValidateAssignment(tt.path)
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("ValidateAssignment(%q) = %v, want nil", tt.path, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateAssignment(%q) = nil, want an error", tt.path)
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

// TestValidateStored pins the second entry point: the SAME rule, for a write to
// a nib that already exists, where the value being judged need not have come
// from the request. It must agree with ValidateAssignment on every accept and
// every refuse, and differ only in saying whose value it is and what to do about
// it — a caller who passed no area is otherwise told to correct an argument they
// never wrote.
func TestValidateStored(t *testing.T) {
	declared := parseVocab(t, sampleAreasConfig)
	none := &Vocabulary{}

	// The verdicts must not diverge, so they are asserted against the sibling
	// rather than restated: a rule that accepted here and refused there would
	// make the message depend on which write path reached it.
	for _, tc := range []struct {
		name  string
		vocab *Vocabulary
		path  string
	}{
		{"a declared vocabulary", declared, ""}, {"no declared areas", none, ""},
		{"a declared vocabulary", declared, "web"}, {"a declared vocabulary", declared, "web/dashboard"},
		{"a declared vocabulary", declared, "nosuch"}, {"a declared vocabulary", declared, "web/legacy"},
		{"no declared areas", none, "web"},
	} {
		t.Run(fmt.Sprintf("agrees with the supplied-value rule on %q under %s", tc.path, tc.name), func(t *testing.T) {
			supplied := tc.vocab.ValidateAssignment(tc.path)
			stored := tc.vocab.ValidateStored("cfg-n001", tc.path)
			if (supplied == nil) != (stored == nil) {
				t.Fatalf("ValidateAssignment(%q) = %v but ValidateStored(%q) = %v", tc.path, supplied, tc.path, stored)
			}
		})
	}

	for _, tt := range []struct {
		name     string
		vocab    *Vocabulary
		path     string
		contains []string
		absent   []string
	}{
		{
			name:  "an undeclared path names the vocabulary and whose value it is",
			vocab: declared, path: "nosuch",
			contains: []string{`"nosuch"`, "web/dashboard", "already carries",
				"`nibs set cfg-n001 --area <declared>`", "`nibs set cfg-n001 --clear area`"},
			// The subject is known here, so it is interpolated: a literal <id>
			// exits 3 for anyone who runs the line as printed.
			absent: []string{"nibs set <id>"},
		},
		{
			name:  "a store with no declared areas names only the escape it can satisfy",
			vocab: none, path: "web",
			contains: []string{`"web"`, "declares no areas", "already carries", "`nibs set cfg-n001 --clear area`"},
			// An empty allowed set must not be printed as though it were one,
			// and --area has no satisfiable argument in the state this branch
			// diagnoses — prescribing it would name an unfollowable command.
			absent: []string{"must be one of", "--area <declared>", "nibs set <id>"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.vocab.ValidateStored("cfg-n001", tt.path)
			if err == nil {
				t.Fatalf("ValidateStored(%q) = nil, want an error", tt.path)
			}
			var areaErr *Error
			if !errors.As(err, &areaErr) {
				t.Fatalf("error = %T, want *Error — the ordering backfill classifies this refusal by type", err)
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
		vocab := &Vocabulary{Nodes: []Node{{Name: "web"}}}
		err := vocab.ValidateStored("cfg-n001", "we`b")
		if err == nil {
			t.Fatal("ValidateStored() = nil, want an error")
		}
		if strings.Contains(err.Error(), "we`b") {
			t.Errorf("error = %q, want the backtick substituted", err.Error())
		}
	})

	// The id is interpolated into a command the reader is invited to run, and
	// it comes from a filename — so it crosses the same boundary the value does.
	t.Run("the nib id is rendered, not echoed raw", func(t *testing.T) {
		vocab := &Vocabulary{Nodes: []Node{{Name: "web"}}}
		err := vocab.ValidateStored("nib\x1b[31m1", "nosuch")
		if err == nil {
			t.Fatal("ValidateStored() = nil, want an error")
		}
		if strings.Contains(err.Error(), "\x1b") {
			t.Errorf("error = %q, want the escape sequence neutralized", err.Error())
		}
	})
}

// TestValidateAssignmentRendersTheRefusedValue keeps the refused value on the
// same rendering boundary List applies to the declared set. The value reaching
// this rule is not always a flag: Core.Update re-checks the `area:` a nib
// already carries, so a hostile FILE reaches the message too.
func TestValidateAssignmentRendersTheRefusedValue(t *testing.T) {
	vocab := &Vocabulary{Nodes: []Node{{Name: "web"}}}

	// The backtick is the rune the %q around the value does NOT answer: it is
	// printable, so strconv.Quote passes it through, and it closes the code span
	// an agent transcript renders the message inside. safetext.Strip is what
	// substitutes it.
	t.Run("a backtick cannot close the message's code span", func(t *testing.T) {
		err := vocab.ValidateAssignment("we`b")
		if err == nil {
			t.Fatal("ValidateAssignment accepted an undeclared path")
		}
		if strings.Contains(err.Error(), "`") {
			t.Errorf("error = %q, want the backtick neutralized", err.Error())
		}
	})

	t.Run("an oversized value is bounded", func(t *testing.T) {
		err := vocab.ValidateAssignment(strings.Repeat("x", safetext.MaxBoundedRunes*4))
		if err == nil {
			t.Fatal("ValidateAssignment accepted an undeclared path")
		}
		if utf8.RuneCountInString(err.Error()) > safetext.MaxBoundedRunes*2 {
			t.Errorf("error is %d runes; want the echoed value bounded", utf8.RuneCountInString(err.Error()))
		}
	})
}
