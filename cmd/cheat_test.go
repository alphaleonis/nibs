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
