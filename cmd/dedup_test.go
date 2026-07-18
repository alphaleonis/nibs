package cmd

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
)

func TestNormalizeTitle(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain lowercases", "Fix The Login Flow", "fix the login flow"},
		{"collapses whitespace", "  Fix   the\tlogin   flow ", "fix the login flow"},
		{"strips punctuation to spaces", "Fix the login-flow!", "fix the login flow"},
		{"parenthetical", "Add dark-mode (toggle)", "add dark mode toggle"},
		{"empty", "", ""},
		{"only punctuation", "-- !! --", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeTitle(tc.in); got != tc.want {
				t.Errorf("normalizeTitle(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestTitlesMatch(t *testing.T) {
	cases := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{"exact", "Fix login", "Fix login", true},
		{"case and punctuation only differ", "Fix, the login!", "fix the login", true},
		{"token subset a in b", "Add dark mode", "Add dark mode toggle", true},
		{"token subset b in a", "Add dark mode toggle", "add dark mode", true},
		{"reordered tokens (subset both ways)", "login fix", "fix login", true},
		{"partial overlap not subset", "fix login bug", "fix logout bug", false},
		{"disjoint", "totally different", "unrelated words", false},
		{"empty left", "", "fix login", false},
		{"empty right", "fix login", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := titlesMatch(tc.a, tc.b); got != tc.want {
				t.Errorf("titlesMatch(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestScrapReasonSnippet(t *testing.T) {
	body := "## Description\nSomething.\n\n## Reasons for Scrapping\n- Too risky to implement now\n- Superseded by another approach\n"
	got := scrapReasonSnippet(body)
	if got != "Too risky to implement now" {
		t.Errorf("scrapReasonSnippet = %q, want first bullet without marker", got)
	}

	if s := scrapReasonSnippet("## Description\nno reasons section here\n"); s != "" {
		t.Errorf("scrapReasonSnippet with no section = %q, want empty", s)
	}
}

// mkNib builds a minimal nib for the finder tests.
func mkNib(id, slug, title, status, body string) *nib.Nib {
	return &nib.Nib{ID: id, Slug: slug, Title: title, Status: status, Body: body}
}

func TestFindPossibleDuplicates(t *testing.T) {
	cfg := config.Default()

	scrapped := mkNib("n-scrap", "fix-login", "Fix login", "scrapped",
		"## Reasons for Scrapping\nDecided against it: out of scope\n")
	completed := mkNib("n-done", "add-dark-mode", "Add dark mode", "completed", "## Summary\nDone.\n")
	openNib := mkNib("n-open", "fix-login", "Fix login", "todo", "")
	unrelated := mkNib("n-other", "unrelated", "Something unrelated", "scrapped", "")
	self := mkNib("n-self", "fix-login", "Fix login", "scrapped", "")

	candidates := []*nib.Nib{scrapped, completed, openNib, unrelated, self}

	t.Run("title match surfaces closed nibs and excludes open + self", func(t *testing.T) {
		got := findPossibleDuplicates(candidates, cfg, "n-self", "Fix login", "fix-login")
		// scrapped (title+slug), self excluded by id, open excluded by status.
		// unrelated does not match by title/slug.
		want := []possibleDuplicate{
			{ID: "n-scrap", Status: "scrapped", Title: "Fix login", Reason: "Decided against it: out of scope"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("findPossibleDuplicates = %#v, want %#v", got, want)
		}
	})

	t.Run("completed match has no reason", func(t *testing.T) {
		got := findPossibleDuplicates(candidates, cfg, "new", "Add dark mode", "add-dark-mode")
		want := []possibleDuplicate{
			{ID: "n-done", Status: "completed", Title: "Add dark mode"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("findPossibleDuplicates = %#v, want %#v", got, want)
		}
	})

	t.Run("no match yields nil", func(t *testing.T) {
		got := findPossibleDuplicates(candidates, cfg, "new", "A brand new distinct heading", "brand-new")
		if len(got) != 0 {
			t.Errorf("findPossibleDuplicates = %#v, want empty", got)
		}
	})

	t.Run("slug equality matches even when titles differ", func(t *testing.T) {
		// New title differs from the scrapped nib's title, but the slug is equal.
		got := findPossibleDuplicates([]*nib.Nib{scrapped}, cfg, "new", "Completely different words here", "fix-login")
		if len(got) != 1 || got[0].ID != "n-scrap" {
			t.Errorf("findPossibleDuplicates by slug = %#v, want the scrapped nib", got)
		}
	})

	t.Run("empty slug does not match empty slug", func(t *testing.T) {
		a := mkNib("n-a", "", "Title one", "scrapped", "")
		// Different title, both empty slug -> no match.
		got := findPossibleDuplicates([]*nib.Nib{a}, cfg, "new", "Title two", "")
		if len(got) != 0 {
			t.Errorf("empty-slug should not self-match: %#v", got)
		}
	})
}

func TestPrintDuplicateWarning(t *testing.T) {
	var buf bytes.Buffer
	dups := []possibleDuplicate{
		{ID: "n-scrap", Status: "scrapped", Title: "Fix login", Reason: "out of scope"},
		{ID: "n-done", Status: "completed", Title: "Add dark mode"},
	}
	printDuplicateWarning(&buf, dups)
	out := buf.String()
	for _, want := range []string{"n-scrap", "scrapped", "Fix login", "out of scope", "n-done", "completed", "Add dark mode"} {
		if !bytes.Contains([]byte(out), []byte(want)) {
			t.Errorf("warning missing %q; got:\n%s", want, out)
		}
	}
}
