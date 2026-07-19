package config

import (
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
)

// TestNibPresentationDefaultsMatchConfig guards against drift between the
// presentation-default constants defined in the nib package (nib.DefaultType /
// nib.DefaultPriority — the fallbacks applied at the consumption boundary for a
// file that omits `type:`/`priority:`) and this package's type/priority enums.
//
// The nib package cannot import config (that would create a nib->config layering
// edge), so it hardcodes "task"/"normal". This test lives
// here — config CAN import nib — to pin those two definitions equal, so a future
// change to the enums (or the constants) cannot silently diverge them.
func TestNibPresentationDefaultsMatchConfig(t *testing.T) {
	cfg := Default()

	if nib.DefaultType != "task" {
		t.Errorf("nib.DefaultType = %q, want \"task\"", nib.DefaultType)
	}
	if cfg.GetType(nib.DefaultType) == nil {
		t.Errorf("nib.DefaultType %q is not a valid config type (not in DefaultTypes)", nib.DefaultType)
	}

	if nib.DefaultPriority != "normal" {
		t.Errorf("nib.DefaultPriority = %q, want \"normal\"", nib.DefaultPriority)
	}
	if cfg.GetPriority(nib.DefaultPriority) == nil {
		t.Errorf("nib.DefaultPriority %q is not a valid config priority (not in DefaultPriorities)", nib.DefaultPriority)
	}

	// The status-priority sort relies on PriorityRank treating an empty priority
	// as the presentation default; pin that so the sort's "empty == default"
	// contract stays wired to nib.DefaultPriority.
	if cfg.PriorityRank("") != cfg.PriorityRank(nib.DefaultPriority) {
		t.Errorf("PriorityRank(\"\") = %d != PriorityRank(%q) = %d; empty must rank as the default priority",
			cfg.PriorityRank(""), nib.DefaultPriority, cfg.PriorityRank(nib.DefaultPriority))
	}
}
