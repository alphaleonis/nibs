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

func statusIsOpen(s config.StatusConfig) bool   { return !s.Role.Closed() }
func statusIsClosed(s config.StatusConfig) bool { return s.Role.Closed() }
func statusIsHolding(s config.StatusConfig) bool {
	return s.Role.Closed() && !s.Role.ReleasesDependents()
}

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
		"context", "plan", "roadmap", // the composite views
		"--ready", // the startable filter, the only route to it on this sheet
		"@FILE",   // the '-'/@FILE prose-input rule
		"{nib}", "{nibs,count,truncated}",
		"never silently retry",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("cheat sheet missing %q", want)
		}
	}
}

// cheatEntryLabels returns the first token of every entry in the sheet's command
// sections (READ/WRITE) — the "verb column". A section opens with its label in
// column 0 and its first entry at column 7; later entries are indented to that
// same column 7, and a wrapped remainder is indented far past it. So an entry is
// exactly a line with seven leading spaces and a non-space at column 7, plus the
// section-label line itself.
func cheatEntryLabels(t *testing.T, sheet string, sections ...string) []string {
	t.Helper()
	var labels []string
	inSection := false
	for _, line := range strings.Split(sheet, "\n") {
		switch {
		case len(line) > 0 && line[0] != ' ':
			// A line starting in column 0 opens a section and ends the previous one.
			inSection = false
			for _, name := range sections {
				if after, ok := strings.CutPrefix(line, name); ok && strings.HasPrefix(after, " ") {
					inSection = true
					line = strings.TrimLeft(after, " ")
				}
			}
			if !inSection {
				continue
			}
		case inSection && strings.HasPrefix(line, "       ") && len(line) > 7 && line[7] != ' ':
			line = line[7:]
		default:
			continue
		}
		if label := strings.Fields(line); len(label) > 0 {
			labels = append(labels, label[0])
		}
	}
	if len(labels) == 0 {
		t.Fatalf("no entries parsed from sections %v; the sheet's layout changed:\n%s", sections, sheet)
	}
	return labels
}

// TestCheatSheetVerbColumnHoldsOnlyRealCommands is the guard for nibs-mslb: the
// READ and WRITE sections list one command per line with its name in the verb
// column, so EVERY name in that column must be runnable exactly as written.
//
// It regressed once by holding a category label instead. `recipes` sat in the
// column beside get/list/rel and read as a fourth command, but named a group
// whose members (context, plan, roadmap) are the actual commands — so `nibs
// recipes` exited 1. That is expensive precisely here: CLAUDE.md sends agents to
// `nibs cheat` FIRST to avoid guessing syntax, and the project rule is to STOP on
// any nibs error, so following the sheet correctly halts the run.
//
// Membership is checked against the command tree rather than a hand-listed set,
// which is what makes this catch a label nobody thought to exclude.
func TestCheatSheetVerbColumnHoldsOnlyRealCommands(t *testing.T) {
	registered := map[string]bool{}
	for _, c := range rootCmd.Commands() {
		registered[c.Name()] = true
		for _, alias := range c.Aliases {
			registered[alias] = true
		}
	}
	// Tripwire: if the command tree comes back empty the loop below passes
	// vacuously, reporting a clean sheet no matter what it holds.
	if len(registered) == 0 {
		t.Fatal("rootCmd has no subcommands; the membership check would be vacuous")
	}

	for _, label := range cheatEntryLabels(t, cheatSheet(config.Default()), "READ", "WRITE") {
		if !registered[label] {
			t.Errorf("cheat sheet verb column lists %q, which is not a runnable command; "+
				"the column must hold commands, not category labels", label)
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
		Role:        config.RoleParked,
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
	if !strings.Contains(entry, closeDefaultStatus()) {
		t.Errorf("cheat sheet close entry should name the default close reason %q, got: %q", closeDefaultStatus(), entry)
	}
}

// cheatEntry returns the sheet entry whose label line contains the given text,
// with the continuation lines it wraps onto. A wrapped remainder is indented
// past its label, so the entry ends at the first following line indented no
// further — which scopes an assertion to one entry, on a sheet that names the
// same verbs and relations in several other places.
func cheatEntry(t *testing.T, sheet, label string) string {
	t.Helper()
	lines := strings.Split(sheet, "\n")
	for i, line := range lines {
		if !strings.Contains(line, label) {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		entry := []string{line}
		for _, next := range lines[i+1:] {
			if strings.TrimSpace(next) == "" {
				break
			}
			if nextIndent := len(next) - len(strings.TrimLeft(next, " ")); nextIndent <= indent {
				break
			}
			entry = append(entry, next)
		}
		return strings.Join(entry, "\n")
	}
	t.Fatalf("cheat sheet has no entry containing %q:\n%s", label, sheet)
	return ""
}

// TestCheatSheetRelEntryMatchesRelArity ties the one-screen grammar to what the
// rel command actually accepts: --rel is bracketed exactly when omitting it is
// legal, and while it is legal the entry names the relation omitting it
// returns. An unbracketed --rel on a sheet that brackets 'list [filters]' reads
// as a requirement, which is how a caller runs the bare form unawares and takes
// the default's output for a relationship it named.
func TestCheatSheetRelEntryMatchesRelArity(t *testing.T) {
	entry := cheatEntry(t, cheatSheet(config.Default()), "rel <id>")
	required := relRequiresRelFlag(t)
	bracketed := strings.Contains(entry, "[--rel")

	switch {
	case required && bracketed:
		t.Errorf("cheat sheet brackets --rel as optional, but the rel command requires it:\n%s", entry)
	case !required && !bracketed:
		t.Errorf("cheat sheet presents --rel as required, but omitting it queries %s; bracket it:\n%s", relDefaultKind, entry)
	case !required && !statesRelDefault(entry):
		t.Errorf("cheat sheet never names %q as what an omitted --rel returns:\n%s", relDefaultKind, entry)
	}
}

// TestCheatSheetDropsBlockerNoteWhenNothingHolds asserts the blocker note is
// conditional on the holding set: with every closed status releasing its
// dependents the note has no members, so the sheet must drop it rather than
// print a dangling label.
func TestCheatSheetDropsBlockerNoteWhenNothingHolds(t *testing.T) {
	statuses := make([]config.StatusConfig, len(config.DefaultStatuses))
	copy(statuses, config.DefaultStatuses)
	for i := range statuses {
		if statuses[i].Role == config.RoleParked {
			statuses[i].Role = config.RoleDropped
		}
	}
	withStatuses(t, statuses)

	got := cheatSheet(config.Default())
	if strings.Contains(got, "still blocks") {
		t.Errorf("cheat sheet still carries the blocker note with no status holding its dependents:\n%s", got)
	}
}
