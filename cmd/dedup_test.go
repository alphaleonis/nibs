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

// TestCloseReasonSnippet covers both sources: the ## Summary entries `close`
// writes today, and the "## Reasons for Scrapping" convention that predated
// them. Nibs closed the old way still have to explain themselves, because this
// project does no data migration.
func TestCloseReasonSnippet(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{{
		name: "legacy section, first bullet without its marker",
		body: "## Description\nSomething.\n\n## Reasons for Scrapping\n- Too risky to implement now\n- Superseded by another approach\n",
		want: "Too risky to implement now",
	}, {
		name: "no reason anywhere",
		body: "## Description\nno reasons section here\n",
		want: "",
	}, {
		name: "a single close entry",
		body: "## Summary\n\n**Scrapped 2026-08-02** — superseded by the new pipeline\n",
		want: "superseded by the new pipeline",
	}, {
		name: "the LAST entry wins, because a reason can be revised",
		body: "## Summary\n\n**Deferred 2026-07-27** — waiting on the upstream release\n\n**Scrapped 2026-08-02** — superseded by the new pipeline\n",
		want: "superseded by the new pipeline",
	}, {
		name: "a deferred nib explains itself too",
		body: "## Summary\n\n**Deferred 2026-07-27** — waiting on the upstream release\n",
		want: "waiting on the upstream release",
	}, {
		// The Summary is checked first, so a nib carrying both must not have its
		// current reason shadowed by the stale legacy section.
		name: "close entries win over a stale legacy section",
		body: "## Summary\n\n**Scrapped 2026-08-02** — the real, current reason\n\n## Reasons for Scrapping\n- the stale one\n",
		want: "the real, current reason",
	}, {
		// Bold text at the start of a hand-written summary is not a close entry;
		// requiring the date stamp is what tells them apart.
		name: "hand-written summary is not mistaken for an entry",
		body: "## Summary\n\n**Note** — this was written by hand, not by close\n",
		want: "",
	}, {
		name: "hand-written summary falls back to the legacy section",
		body: "## Summary\n\nplain prose\n\n## Reasons for Scrapping\n- the legacy reason\n",
		want: "the legacy reason",
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := closeReasonSnippet(tc.body); got != tc.want {
				t.Errorf("closeReasonSnippet = %q, want %q", got, tc.want)
			}
		})
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

	t.Run("a match with no recorded reason carries none", func(t *testing.T) {
		// n-done's body holds neither close entries nor a legacy section, so the
		// field stays empty. Emptiness tracks what the nib recorded, not which
		// closed status it carries.
		got := findPossibleDuplicates(candidates, cfg, "new", "Add dark mode", "add-dark-mode")
		want := []possibleDuplicate{
			{ID: "n-done", Status: "completed", Title: "Add dark mode"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("findPossibleDuplicates = %#v, want %#v", got, want)
		}
	})

	t.Run("deferred match surfaces and explains itself", func(t *testing.T) {
		// Deferred is a closed status, so a set-aside nib is exactly the kind of
		// duplicate this warning exists for: the idea was kept, not rejected.
		// That is also why the reason is not scrapped-only — "we set this aside
		// until X" is the most useful thing to tell someone about to redo it.
		deferred := mkNib("n-defer", "slack-integration", "Slack integration", "deferred",
			"## Summary\n\n**Deferred 2026-07-27** — waiting on the upstream release\n")
		got := findPossibleDuplicates([]*nib.Nib{deferred}, cfg, "new", "Slack integration", "slack-integration")
		want := []possibleDuplicate{
			{ID: "n-defer", Status: "deferred", Title: "Slack integration", Reason: "waiting on the upstream release"},
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
