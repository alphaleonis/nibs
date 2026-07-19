package cmd

import (
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
)

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
// status-group vocabulary (open/closed/parked as -s/--no-status values), the
// open-by-default behavior of list/rel, the "open work under X" recipe, and the
// terse-output (-c/-q) caveat — the discoverability the open default depends on.
func TestCheatSheetShowsOpenDefaultAndGroups(t *testing.T) {
	got := cheatSheet(config.Default())
	for _, want := range []string{
		"open|closed|parked",           // the status-group vocabulary
		"OPEN only by default",         // list/rel are open by default
		"--all",                        // --all shows everything
		"descendants -t bug",           // the "open work under X" recipe
		"no post-filter",               // the recipe needs no post-filtering
		"-c/-q honor the open default", // the terse-output caveat
	} {
		if !strings.Contains(got, want) {
			t.Errorf("cheat sheet missing %q", want)
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
