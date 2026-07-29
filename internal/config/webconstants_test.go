package config_test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
)

// webConstantsPath is the TypeScript file that restates the status vocabulary
// for the web UI.
const webConstantsPath = "../../web/src/lib/constants.ts"

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
	b, err := os.ReadFile(filepath.FromSlash(webConstantsPath))
	if err != nil {
		// A missing file means the guard silently stops guarding, so fail rather
		// than skip — the whole point is that nobody notices this drifting.
		t.Fatalf("reading %s: %v", webConstantsPath, err)
	}
	return string(b)
}

// parseStringArray extracts the string literals from
// `export const <name> = [...]` in the TypeScript source.
func parseStringArray(t *testing.T, src, name string) []string {
	t.Helper()
	decl := regexp.MustCompile(`export const ` + regexp.QuoteMeta(name) + `\s*(?::[^=]*)?=\s*\[([^\]]*)\]`)
	m := decl.FindStringSubmatch(src)
	if m == nil {
		t.Fatalf("no `export const %s = [...]` array literal in %s — if it was renamed or made derived, this guard needs updating, not deleting",
			name, webConstantsPath)
	}
	items := regexp.MustCompile(`"([^"]*)"`).FindAllStringSubmatch(m[1], -1)
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it[1])
	}
	if len(out) == 0 {
		t.Fatalf("%s in %s parsed as empty; the guard would pass against anything", name, webConstantsPath)
	}
	return out
}

// assertSameMembers compares two lists as sets. Order is deliberately not
// checked; see the note on TestWebConstantsMatchConfig.
func assertSameMembers(t *testing.T, name string, got, want []string) {
	t.Helper()
	inGot := map[string]bool{}
	for _, g := range got {
		inGot[g] = true
	}
	inWant := map[string]bool{}
	for _, w := range want {
		inWant[w] = true
	}
	for _, w := range want {
		if !inGot[w] {
			t.Errorf("the Go config declares %q but %s in %s does not — the web and Go status vocabularies have drifted (web has %v)",
				w, name, webConstantsPath, got)
		}
	}
	for _, g := range got {
		if !inWant[g] {
			t.Errorf("%s in %s names %q but the Go config does not — the web and Go status vocabularies have drifted (Go has %v)",
				name, webConstantsPath, g, want)
		}
	}
}
