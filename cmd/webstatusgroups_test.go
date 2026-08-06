package cmd

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/testsupport/webconstants"
)

// TestWebStatusGroupsMatchCLI pins the web query language's status groups —
// `status:open` / `status:closed` in the filter box — against the CLI's
// `-s open` / `-s closed`.
//
// The two are meant to be one VALUE vocabulary: the word after `-s` on the CLI
// is the word after `status:` in the box. (The token syntax is not shared; the
// box's `-` prefix is negation.) Nothing enforces that at build time, because
// TypeScript cannot import the Go constants and GraphQL does not serve them, so
// this guard is what makes the duplication fail loudly instead of drifting.
// TestWebConstantsMatchConfig already covers the status names themselves; this
// covers the group names layered on top of them, which nothing else checks.
//
// Membership is re-derived here the same way constants.ts derives it (open =
// STATUSES minus CLOSED_STATUSES) so the Go side carries no third literal.
// Note the limit: the derivation rule is duplicated, not read — if
// OPEN_STATUSES is ever computed differently, this guard keeps asserting the
// old rule and passes. Re-pin it here if that expression changes. (A derivation
// change does not ship silently regardless: web/src/lib/filter.test.ts pins
// OPEN_STATUSES verbatim and runs under `task test`.)
func TestWebStatusGroupsMatchCLI(t *testing.T) {
	src := readWebConstantsFile(t)
	cfg := config.Default()
	groups := parseWebStatusGroups(t, src)

	t.Run("group names match", func(t *testing.T) {
		var got []string
		for name := range groups {
			got = append(got, name)
		}
		want := statusGroupNames()
		sort.Strings(got)
		sortedWant := append([]string(nil), want...)
		sort.Strings(sortedWant)
		if len(got) != len(sortedWant) {
			t.Fatalf("STATUS_GROUPS in %s declares %v, the CLI accepts %v", webconstants.Path, got, want)
		}
		for i := range got {
			if got[i] != sortedWant[i] {
				t.Fatalf("STATUS_GROUPS in %s declares %v, the CLI accepts %v", webconstants.Path, got, want)
			}
		}
	})

	t.Run("group membership matches", func(t *testing.T) {
		for _, name := range statusGroupNames() {
			members, ok := groups[name]
			if !ok {
				// Redundant during a full run — the names subtest fails first —
				// but this subtest must stand on its own under `-run` filtering,
				// where the sibling body never executes at all.
				t.Errorf("the CLI accepts `-s %s` but STATUS_GROUPS in %s declares no %q group",
					name, webconstants.Path, name)
				continue
			}
			want, err := statusGroupMembers(cfg, name)
			if err != nil {
				t.Fatalf("statusGroupMembers(%q): %v", name, err)
			}
			assertSameStatusMembers(t, name, members, want)
		}
	})
}

func readWebConstantsFile(t *testing.T) string {
	t.Helper()
	src, err := webconstants.Source()
	if err != nil {
		// A missing file means the guard silently stops guarding, so fail rather
		// than skip — the whole point is that nobody notices this drifting.
		t.Fatal(err)
	}
	return src
}

// statusGroupEntry matches one complete `["name", IDENTIFIER]` Map entry,
// tolerating the `as readonly string[]` cast constants.ts uses to widen a
// `readonly` tuple. Anchored at both ends: an entry is either fully understood
// or rejected, never partially read.
var statusGroupEntry = regexp.MustCompile(`^\[\s*"([^"]+)"\s*,\s*([A-Za-z_][A-Za-z0-9_]*)\s*(?:as\b.*)?\]$`)

// parseWebStatusGroups extracts the `export const STATUS_GROUPS = new Map([...])`
// entries from the TypeScript source and resolves each one's member list,
// returning group name → member statuses.
func parseWebStatusGroups(t *testing.T, src string) map[string][]string {
	t.Helper()
	decl := regexp.MustCompile(`export const STATUS_GROUPS[^=]*=\s*new Map\(\[(.*?)\]\)`)
	m := decl.FindStringSubmatch(strings.ReplaceAll(src, "\n", " "))
	if m == nil {
		t.Fatalf("no `export const STATUS_GROUPS = new Map([...])` in %s — if it was renamed or restructured, this guard needs updating, not deleting",
			webconstants.Path)
	}
	entries, err := splitMapEntries(m[1])
	if err != nil {
		t.Fatalf("STATUS_GROUPS in %s: %v — this guard has to understand the WHOLE declaration, so teach it the new shape rather than letting it read the part it recognizes",
			webconstants.Path, err)
	}
	if len(entries) == 0 {
		t.Fatalf("STATUS_GROUPS in %s parsed as empty; the guard would pass against anything", webconstants.Path)
	}
	groups := make(map[string][]string, len(entries))
	for _, e := range entries {
		sm := statusGroupEntry.FindStringSubmatch(e)
		if sm == nil {
			t.Fatalf("STATUS_GROUPS entry `%s` in %s is not `[\"name\", IDENTIFIER]` — this guard has to understand the WHOLE declaration, so teach it the new shape rather than skipping the entry",
				e, webconstants.Path)
		}
		groups[sm[1]] = resolveWebStatusList(t, src, sm[2])
	}
	return groups
}

// splitMapEntries splits the body of `new Map([...])` into its top-level
// `[...]` entries, rejecting anything it cannot account for.
//
// The point is TOTALITY, and it is what stops this guard from quietly ceasing
// to guard. Scanning for entries with a repeated regex match and keeping
// whatever it recognized lets an entry written in a shape the regex cannot read
// vanish: the surviving entries still agree with the CLI, both subtests go
// green, and the box accepts a group name the CLI rejects. So every byte of the
// declaration must be accounted for — either inside an entry, or one of the
// commas and whitespace between them.
func splitMapEntries(body string) ([]string, error) {
	var entries []string
	depth, start := 0, 0
	var quote byte
	for i := 0; i < len(body); i++ {
		c := body[i]
		if quote != 0 {
			switch c {
			case '\\':
				i++ // an escaped byte can never close the string
			case quote:
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'', '`':
			if depth == 0 {
				return nil, fmt.Errorf("string literal at offset %d sits outside any entry", i)
			}
			quote = c
		case '[':
			if depth == 0 {
				start = i
			}
			depth++
		case ']':
			depth--
			if depth < 0 {
				return nil, fmt.Errorf("unbalanced `]` at offset %d", i)
			}
			if depth == 0 {
				entries = append(entries, body[start:i+1])
			}
		default:
			if depth == 0 && c != ',' && c != ' ' && c != '\t' && c != '\r' && c != '\n' {
				return nil, fmt.Errorf("unexpected %q at offset %d, outside any entry", string(c), i)
			}
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated string literal")
	}
	if depth != 0 {
		return nil, fmt.Errorf("unbalanced `[`")
	}
	return entries, nil
}

// resolveWebStatusList resolves the TypeScript identifier a STATUS_GROUPS entry
// points at to its member statuses. STATUSES and CLOSED_STATUSES are array
// literals; OPEN_STATUSES is derived in the source as the complement of the
// closed set, so it is derived the same way here.
func resolveWebStatusList(t *testing.T, src, ident string) []string {
	t.Helper()
	switch ident {
	case "STATUSES", "CLOSED_STATUSES":
		return parseWebStringArray(t, src, ident)
	case "OPEN_STATUSES":
		closed := map[string]bool{}
		for _, s := range parseWebStringArray(t, src, "CLOSED_STATUSES") {
			closed[s] = true
		}
		var open []string
		for _, s := range parseWebStringArray(t, src, "STATUSES") {
			if !closed[s] {
				open = append(open, s)
			}
		}
		return open
	}
	t.Fatalf("STATUS_GROUPS in %s points at unknown identifier %q — teach this guard how to resolve it rather than dropping the check",
		webconstants.Path, ident)
	return nil
}

func parseWebStringArray(t *testing.T, src, name string) []string {
	t.Helper()
	out, err := webconstants.ParseStringArray(src, name)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// assertSameStatusMembers compares two status lists as sets. Order is
// deliberately not checked — the web and Go orders differ on purpose, as
// TestWebConstantsMatchConfig explains.
func assertSameStatusMembers(t *testing.T, group string, got, want []string) {
	t.Helper()
	missing, extra := webconstants.Diff(got, want)
	for _, w := range missing {
		t.Errorf("the CLI's %q status group contains %q but the web's does not — `status:%s` in the filter box and `-s %s` on the CLI now select different nibs (web has %v)",
			group, w, group, group, got)
	}
	for _, g := range extra {
		t.Errorf("the web's %q status group contains %q but the CLI's does not — `status:%s` in the filter box and `-s %s` on the CLI now select different nibs (CLI has %v)",
			group, g, group, group, want)
	}
}
