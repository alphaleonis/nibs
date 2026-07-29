package cmd

import (
	"slices"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
)

// declaredStatusNames re-derives a status group straight from the declaration
// in config.DefaultStatuses, rather than through the accessor the cheat sheet
// calls. Expectations built this way disagree with the sheet when an accessor
// picks the wrong members or the wrong order; expectations built by calling the
// accessor cannot, because the same answer lands on both sides of the
// comparison.
func declaredStatusNames(keep func(config.StatusConfig) bool) []string {
	var names []string
	for _, s := range config.DefaultStatuses {
		if keep(s) {
			names = append(names, s.Name)
		}
	}
	return names
}

func statusIsOpen(s config.StatusConfig) bool    { return !s.Closed }
func statusIsClosed(s config.StatusConfig) bool  { return s.Closed }
func statusIsHolding(s config.StatusConfig) bool { return s.Closed && !s.ReleasesDependents }

// statusLineGroups returns the "open=… · closed=…" segment of the cheat sheet's
// STATUS line: everything between the "STATUS " prefix and the parenthetical
// after it. Callers compare that segment for equality, because asking whether
// the sheet CONTAINS "open=<members>" passes whenever the rendered list merely
// starts with those members — an accessor that appends the closed statuses to
// the open group survives a containment check unnoticed.
func statusLineGroups(t *testing.T, sheet string) string {
	t.Helper()
	for _, line := range strings.Split(sheet, "\n") {
		rest, ok := strings.CutPrefix(line, "STATUS ")
		if !ok {
			continue
		}
		if i := strings.Index(rest, "   ("); i >= 0 {
			rest = rest[:i]
		}
		return rest
	}
	t.Fatalf("cheat sheet has no STATUS line:\n%s", sheet)
	return ""
}

// wantStatusLineGroups builds the STATUS line's group segment from the declared
// status names, in the shape cheatSheet renders it.
func wantStatusLineGroups(open, closed []string) string {
	return statusGroupOpen + "=" + strings.Join(open, "/") + " · " +
		statusGroupClosed + "=" + strings.Join(closed, "/")
}

// TestCheatSheetFitsOneScreen pins the cheat sheet's defining property: it must
// stay a single screen (<=40 lines) so an agent can absorb the whole grammar in
// one read.
func TestCheatSheetFitsOneScreen(t *testing.T) {
	got := cheatSheet(config.Default())
	lines := strings.Count(strings.TrimRight(got, "\n"), "\n") + 1
	if lines > 40 {
		t.Fatalf("cheat sheet is %d lines; must be <= 40", lines)
	}
}

// TestCheatSheetCoversTheSurface asserts the cheat sheet names every agent-facing
// verb plus the load-bearing cribs (the '-'/@FILE input rule, exit codes, and the
// "stop on error" rule). If a verb is renamed or dropped, this fails loudly.
func TestCheatSheetCoversTheSurface(t *testing.T) {
	got := cheatSheet(config.Default())
	for _, want := range []string{
		"get", "list", "rel", "new", "set", "body", "mv", "rm", "close",
		"catalog", "cheat", "prime",
		"@FILE", // the '-'/@FILE prose-input rule
		"{nib}", "{nibs,count,truncated}",
		"never silently retry",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("cheat sheet missing %q", want)
		}
	}
}

// TestCheatSheetShowsSectionCreate asserts the body line hints that --section
// --set replaces an EXISTING heading and how to create one (--create), so an
// agent does not learn only the strict-default half of the operation.
func TestCheatSheetShowsSectionCreate(t *testing.T) {
	got := cheatSheet(config.Default())
	// Scope the assertion to the body line so an unrelated future --create mention
	// elsewhere in the sheet can't mask a regressed body line.
	var bodyLine string
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "body <id>") {
			bodyLine = line
			break
		}
	}
	if bodyLine == "" {
		t.Fatalf("cheat sheet has no 'body <id>' line, got:\n%s", got)
	}
	if !strings.Contains(bodyLine, "--create") {
		t.Errorf("cheat sheet body line should mention --create, got line: %q", bodyLine)
	}
}

// TestCheatSheetShowsOpenDefaultAndGroups asserts the cheat sheet conveys the
// status-group vocabulary (open/closed as -s/--no-status values), the
// open-by-default behavior of list/rel, the "open work under X" recipe, and the
// terse-output (-c/-q) caveat — the discoverability the open default depends on.
func TestCheatSheetShowsOpenDefaultAndGroups(t *testing.T) {
	got := cheatSheet(config.Default())
	open := declaredStatusNames(statusIsOpen)
	closed := declaredStatusNames(statusIsClosed)
	holding := declaredStatusNames(statusIsHolding)

	// An empty group renders as a bare "open=", which the equality check below
	// would still match against an equally empty expectation. TestStatusGroupNames
	// rejects a group of fewer than two members, so this is a tripwire on that
	// invariant rather than a reachable state.
	if len(open) == 0 || len(closed) == 0 {
		t.Fatalf("declared status groups are open=%v closed=%v; an empty group makes the group assertion vacuous", open, closed)
	}

	// The status-group vocabulary: both group names with exactly their members.
	if gotGroups, wantGroups := statusLineGroups(t, got), wantStatusLineGroups(open, closed); gotGroups != wantGroups {
		t.Errorf("cheat sheet STATUS groups = %q, want %q", gotGroups, wantGroups)
	}

	want := []string{
		"OPEN only by default",         // list/rel are open by default
		"--all",                        // --all shows everything
		"descendants -t bug",           // the "open work under X" recipe
		"no post-filter",               // the recipe needs no post-filtering
		"-c/-q honor the open default", // the terse-output caveat
	}
	// The one non-obvious consequence: closed does not always mean released. The
	// trailing ")" closes the parenthetical the note ends, so an extra holding
	// status cannot hide behind a prefix match. The note only exists when some
	// status holds — the empty case is TestCheatSheetDropsBlockerNoteWhenNothingHolds.
	if len(holding) > 0 {
		want = append(want, "closed but still blocks: "+strings.Join(holding, ", ")+")")
	}
	for _, want := range want {
		if !strings.Contains(got, want) {
			t.Errorf("cheat sheet missing %q", want)
		}
	}
	// The retired vocabulary must not reappear on the one screen an agent reads.
	for _, gone := range []string{retiredStatusGroup, "--active"} {
		if strings.Contains(got, gone) {
			t.Errorf("cheat sheet still mentions retired %q", gone)
		}
	}
}

// TestCheatSheetEnumsAreGenerated asserts the type/status/priority/estimate lines
// are sourced from config (not hand-transcribed), so they cannot drift from what
// the CLI accepts.
func TestCheatSheetEnumsAreGenerated(t *testing.T) {
	cfg := config.Default()
	got := cheatSheet(cfg)
	for _, group := range [][]string{
		cfg.TypeNames(), cfg.StatusNames(), cfg.PriorityNames(), cfg.EstimateNames(),
	} {
		for _, v := range group {
			if !strings.Contains(got, v) {
				t.Errorf("cheat sheet missing enum value %q", v)
			}
		}
	}
}

// TestCheatSheetDerivesStatusGroups proves the STATUS line reads its group
// membership and its blocker note out of config rather than restating them: a
// status added to DefaultStatuses lands in the right group and, being closed
// without releasing its dependents, in the blocker note too — with no edit to
// the cheat sheet's format string.
func TestCheatSheetDerivesStatusGroups(t *testing.T) {
	withExtraStatus(t, config.StatusConfig{
		Name:        "parked",
		Color:       "gray",
		Closed:      true,
		Description: "Guard status: closed, and still blocking",
	})

	got := cheatSheet(config.Default())
	open := declaredStatusNames(statusIsOpen)
	closed := declaredStatusNames(statusIsClosed)
	holding := declaredStatusNames(statusIsHolding)
	if !slices.Contains(closed, "parked") || !slices.Contains(holding, "parked") {
		t.Fatalf("test setup: added status missing from declared closed=%v holding=%v, so this proves no derivation", closed, holding)
	}

	if gotGroups, wantGroups := statusLineGroups(t, got), wantStatusLineGroups(open, closed); gotGroups != wantGroups {
		t.Errorf("cheat sheet STATUS groups did not pick up the added status: got %q, want %q", gotGroups, wantGroups)
	}
	if want := "closed but still blocks: " + strings.Join(holding, ", ") + ")"; !strings.Contains(got, want) {
		t.Errorf("cheat sheet blocker note did not pick up the added status; want %q in:\n%s", want, got)
	}
}

// TestCheatSheetCloseLineNamesTheCloseDefault asserts the close entry states
// the reason a bare `nibs close` records by interpolating closeDefaultStatus,
// not by spelling it out. `nibs prime` renders the same const and the CLI reads
// it as the --as default, so a restated word here would make the one-screen
// grammar the surface left lying after the default moved.
func TestCheatSheetCloseLineNamesTheCloseDefault(t *testing.T) {
	got := cheatSheet(config.Default())

	// Scope to the close entry — it wraps onto a continuation line, and the
	// sheet names statuses in several other places where a match would prove
	// nothing about this one. The entry runs to the next section label.
	start := strings.Index(got, "close <id>")
	if start < 0 {
		t.Fatalf("cheat sheet has no 'close <id>' entry, got:\n%s", got)
	}
	entry := got[start:]
	if end := strings.Index(entry, "\nMETA"); end >= 0 {
		entry = entry[:end]
	}

	if !strings.Contains(entry, "--as") {
		t.Errorf("cheat sheet close entry should name --as, got: %q", entry)
	}
	if !strings.Contains(entry, closeDefaultStatus) {
		t.Errorf("cheat sheet close entry should name the default close reason %q, got: %q", closeDefaultStatus, entry)
	}
}

// TestCheatSheetDropsBlockerNoteWhenNothingHolds asserts the blocker note is
// conditional on the ReleasesDependents flag: with every closed status
// releasing its dependents the note has no members, so the sheet must drop it
// rather than print a dangling label.
func TestCheatSheetDropsBlockerNoteWhenNothingHolds(t *testing.T) {
	statuses := make([]config.StatusConfig, len(config.DefaultStatuses))
	copy(statuses, config.DefaultStatuses)
	for i := range statuses {
		if statuses[i].Closed {
			statuses[i].ReleasesDependents = true
		}
	}
	withStatuses(t, statuses)

	got := cheatSheet(config.Default())
	if strings.Contains(got, "still blocks") {
		t.Errorf("cheat sheet still carries the blocker note with no status holding its dependents:\n%s", got)
	}
}
