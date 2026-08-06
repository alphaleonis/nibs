package config_test

import (
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/testsupport/webconstants"
)

// TestWebConstantsMatchConfig pins web/src/lib/constants.ts against the Go
// configuration.
//
// The web hardcodes the vocabulary because GraphQL does not serve it, so the
// two definitions are kept in step by hand. This epic found three such
// "synchronized" definitions that agreed only by luck, which is why this guard
// exists: it is not a fix for the duplication, it is what makes the duplication
// fail loudly instead of drifting unnoticed. Serving the vocabulary from the
// schema is the real fix.
//
// It asserts MEMBERSHIP, not order. The two orders differ deliberately and must
// be allowed to: Go lists most-active-first (in-progress, todo, draft) for its
// help text, while the web lists workflow progression (draft, todo, in-progress)
// because that is the order the facet checkboxes read best in. Pinning order
// here would fail on a correct codebase and invite someone to "fix" one of the
// two orderings into being wrong for its own surface.
//
// The TypeScript scraping itself lives in internal/testsupport/webconstants,
// shared with TestWebStatusGroupsMatchCLI, which layers the query language's
// status groups on top of these names.
func TestWebConstantsMatchConfig(t *testing.T) {
	src := readWebConstants(t)
	cfg := config.Default()

	t.Run("status names match", func(t *testing.T) {
		got := parseStringArray(t, src, "STATUSES")
		want := cfg.StatusNames()
		assertSameMembers(t, "STATUSES", got, want)
	})

	t.Run("closed set matches", func(t *testing.T) {
		got := parseStringArray(t, src, "CLOSED_STATUSES")
		want := cfg.ClosedStatusNames()
		assertSameMembers(t, "CLOSED_STATUSES", got, want)
	})

	t.Run("every closed name is a declared status", func(t *testing.T) {
		// Guards the case the two lists above miss: a typo in CLOSED_STATUSES
		// that happens to keep the length right would make the web hide nothing
		// while still matching config's count.
		for _, name := range parseStringArray(t, src, "CLOSED_STATUSES") {
			if !cfg.IsValidStatus(name) {
				t.Errorf("CLOSED_STATUSES names %q, which is not a status in the Go config (%s)",
					name, cfg.StatusList())
			}
		}
	})
}

func readWebConstants(t *testing.T) string {
	t.Helper()
	src, err := webconstants.Source()
	if err != nil {
		// A missing file means the guard silently stops guarding, so fail rather
		// than skip — the whole point is that nobody notices this drifting.
		t.Fatal(err)
	}
	return src
}

func parseStringArray(t *testing.T, src, name string) []string {
	t.Helper()
	out, err := webconstants.ParseStringArray(src, name)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// assertSameMembers compares two lists as sets. Order is deliberately not
// checked; see the note on TestWebConstantsMatchConfig.
func assertSameMembers(t *testing.T, name string, got, want []string) {
	t.Helper()
	missing, extra := webconstants.Diff(got, want)
	for _, w := range missing {
		t.Errorf("the Go config declares %q but %s in %s does not — the web and Go status vocabularies have drifted (web has %v)",
			w, name, webconstants.Path, got)
	}
	for _, g := range extra {
		t.Errorf("%s in %s names %q but the Go config does not — the web and Go status vocabularies have drifted (Go has %v)",
			name, webconstants.Path, g, want)
	}
}
