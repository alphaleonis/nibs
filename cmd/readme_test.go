package cmd

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
)

// README's Data Model section states the status vocabulary with no render step
// behind it: it is what someone reads to learn the model without running a
// command, so it keeps its enumeration instead of pointing at `nibs catalog`.
// These tests are what binds it to config — the job the templates do by
// rendering. They fail when README and config.DefaultStatuses disagree; fix
// README, not the test, unless the sentence itself was rewritten.
var (
	// readmeStatusGroups captures the members README lists for each group.
	readmeStatusGroups = regexp.MustCompile(`open \(([^)]+)\) or closed \(([^)]+)\)`)
	// readmeStatusBlocker captures the statuses README names as closed but still
	// blocking their dependents. The capture spans a run of backticked names so
	// the sentence can grow past one status without the guard going blind.
	readmeStatusBlocker = regexp.MustCompile("A ((?:`[^`]+`(?:, | or |/)?)+) nib is closed but still blocks")
	// statusNameSep splits a status enumeration written for humans. README is
	// prose, so the separator is a wording choice, not a fact about config —
	// accept the three the sentences use rather than failing on a reword.
	statusNameSep = regexp.MustCompile(`\s*(?:,|/| or )\s*`)
)

// readmeText reads README.md from the repository root. Go runs a package's
// tests with that package's directory as the working directory, so README sits
// one level up.
func readmeText(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	return string(data)
}

// splitStatusNames splits a status enumeration written in prose into its
// names, dropping the separators and the Markdown code spans around them.
func splitStatusNames(list string) []string {
	names := statusNameSep.Split(list, -1)
	for i, name := range names {
		names[i] = strings.Trim(strings.TrimSpace(name), "`")
	}
	return names
}

// TestReadmeStatusGroupsMatchConfig asserts README's open/closed groups name
// exactly the statuses config declares, in the same order. OpenStatusNames and
// ClosedStatusNames walk DefaultStatuses in declaration order, and README lists
// both groups that way, so there is no divergence to sort away: comparing order
// costs nothing here, and it means a reorder of DefaultStatuses has to reach
// README too.
func TestReadmeStatusGroupsMatchConfig(t *testing.T) {
	cfg := config.Default()
	open, closed := cfg.OpenStatusNames(), cfg.ClosedStatusNames()
	m := readmeStatusGroups.FindStringSubmatch(readmeText(t))

	// The line names members for both groups, so no README text satisfies it
	// once a group is empty — the capture needs at least one character. Assert
	// README names nobody instead of demanding a line it cannot write, so
	// editing this test never becomes the only way to green the suite.
	if len(open) == 0 || len(closed) == 0 {
		if m != nil {
			t.Errorf("README names open %v and closed %v; config declares open %v and closed %v — one group has no members, so the line should be dropped rather than listing any",
				splitStatusNames(m[1]), splitStatusNames(m[2]), open, closed)
		}
		return
	}

	if m == nil {
		t.Fatal(`README.md has no "open (…) or closed (…)" status line, so the vocabulary is unguarded — restore the line or update this test to match its new shape`)
	}

	for _, tc := range []struct {
		group string
		named string
		want  []string
	}{
		{"open", m[1], open},
		{"closed", m[2], closed},
	} {
		if got := splitStatusNames(tc.named); !slices.Equal(got, tc.want) {
			t.Errorf("README names the %s statuses as %v; config declares %v (order included)", tc.group, got, tc.want)
		}
	}
}

// TestReadmeBlockerRuleMatchesConfig asserts README's "closed but still blocks"
// sentence names exactly the statuses that are closed without releasing their
// dependents. It is the most surprising fact in the section, and the one that
// has drifted before. Membership is compared but not order: this is one prose
// sentence rather than a list mirroring the declaration, so the order its
// names are read out in is a wording choice.
//
// The rule is conditional on there being such a status at all: if every closed
// status releases its dependents the sentence describes nothing, and README is
// right to drop it. Demanding it unconditionally would leave editing this test
// as the only way to green — the escape hatch the guard exists to close.
func TestReadmeBlockerRuleMatchesConfig(t *testing.T) {
	m := readmeStatusBlocker.FindStringSubmatch(readmeText(t))
	holding := config.Default().HoldingStatusNames()

	if len(holding) == 0 {
		if m != nil {
			t.Errorf("README still names %v as closed-but-still-blocking; config declares none — every closed status releases its dependents, so the sentence should be dropped", splitStatusNames(m[1]))
		}
		return
	}

	if m == nil {
		t.Fatalf(`README.md has no "A `+"`x`"+` nib is closed but still blocks" sentence, so the rule is unguarded; config says %v hold their dependents while closed — restore the sentence or update this test to match its new shape`, holding)
	}

	got := splitStatusNames(m[1])
	want := slices.Clone(holding)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("README names %v as closed-but-still-blocking; config declares %v", got, want)
	}
}
