package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alphaleonis/nibs/internal/store"
	"github.com/alphaleonis/nibs/internal/testskip"
)

func TestDefault(t *testing.T) {
	cfg := Default()

	if cfg.Nibs.IDLength != 4 {
		t.Errorf("IDLength = %d, want 4", cfg.Nibs.IDLength)
	}
	if cfg.Nibs.Prefix != "" {
		t.Errorf("Prefix = %q, want empty", cfg.Nibs.Prefix)
	}
	if cfg.Nibs.DefaultStatus != "todo" {
		t.Errorf("DefaultStatus = %q, want \"todo\"", cfg.Nibs.DefaultStatus)
	}
	if cfg.Nibs.DefaultType != "task" {
		t.Errorf("DefaultType = %q, want \"task\"", cfg.Nibs.DefaultType)
	}
	if cfg.Nibs.HideCompleted == nil || !*cfg.Nibs.HideCompleted {
		t.Error("HideCompleted should be ptr(true)")
	}
	if cfg.Nibs.WideMode == nil || !*cfg.Nibs.WideMode {
		t.Error("WideMode should be ptr(true)")
	}
	// Both types and statuses are hardcoded
	if len(DefaultTypes) != 6 {
		t.Errorf("len(DefaultTypes) = %d, want 6", len(DefaultTypes))
	}
	if len(DefaultStatuses) != 6 {
		t.Errorf("len(DefaultStatuses) = %d, want 6", len(DefaultStatuses))
	}
}

func TestDefaultWithPrefix(t *testing.T) {
	cfg := DefaultWithPrefix("myapp-")

	if cfg.Nibs.Prefix != "myapp-" {
		t.Errorf("Prefix = %q, want \"myapp-\"", cfg.Nibs.Prefix)
	}
	// Other defaults should still apply
	if cfg.Nibs.IDLength != 4 {
		t.Errorf("IDLength = %d, want 4", cfg.Nibs.IDLength)
	}
}

func TestIsValidStatus(t *testing.T) {
	cfg := Default()

	tests := []struct {
		status string
		want   bool
	}{
		{"draft", true},
		{"todo", true},
		{"in-progress", true},
		{"deferred", true},
		{"completed", true},
		{"scrapped", true},
		{"invalid", false},
		{"", false},
		{"TODO", false}, // case sensitive
		// Old status names should no longer be valid
		{"open", false},
		{"done", false},
		{"ready", false},
		{"not-ready", false},
		{"backlog", false}, // renamed to draft
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := cfg.IsValidStatus(tt.status)
			if got != tt.want {
				t.Errorf("IsValidStatus(%q) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestStatusList(t *testing.T) {
	cfg := Default()
	got := cfg.StatusList()
	want := "in-progress, todo, draft, deferred, completed, scrapped"

	if got != want {
		t.Errorf("StatusList() = %q, want %q", got, want)
	}
}

func TestStatusNames(t *testing.T) {
	cfg := Default()
	got := cfg.StatusNames()

	// The guard a maintainer adding a status hits first, so it names the sites
	// that need a decision. A status added without a Closed decision defaults to
	// open, which silently enlarges the bare `nibs list` and the group filters.
	if len(got) != 6 {
		t.Fatalf("len(StatusNames()) = %d, want 6 — a status was added or removed. Classify it "+
			"(Closed, ReleasesDependents), update the membership assertions in this file, and give it a "+
			"progress bucket in progress.ByCount and progress.ByEstimate, which name statuses "+
			"individually rather than reading a group predicate", len(got))
	}
	expected := []string{"in-progress", "todo", "draft", "deferred", "completed", "scrapped"}
	for i, name := range expected {
		if got[i] != name {
			t.Errorf("StatusNames()[%d] = %q, want %q", i, got[i], name)
		}
	}
}

func TestClosedStatusNames(t *testing.T) {
	cfg := Default()
	got := cfg.ClosedStatusNames()

	// Today the closed set is exactly {deferred, completed, scrapped} — three
	// reasons a nib left the board, in DefaultStatuses order.
	want := []string{"deferred", "completed", "scrapped"}
	if len(got) != len(want) {
		t.Fatalf("len(ClosedStatusNames()) = %d, want %d (%v)", len(got), len(want), got)
	}
	for i, name := range want {
		if got[i] != name {
			t.Errorf("ClosedStatusNames()[%d] = %q, want %q", i, got[i], name)
		}
	}

	// Every returned name must satisfy the canonical closed predicate.
	for _, name := range got {
		if !cfg.IsClosedStatus(name) {
			t.Errorf("ClosedStatusNames() returned %q which is not IsClosedStatus", name)
		}
	}

	// "deferred" is a close reason, so it must be in the closed group.
	if !slices.Contains(got, "deferred") {
		t.Errorf("ClosedStatusNames() = %v, must include \"deferred\"", got)
	}
}

func TestOpenStatusNames(t *testing.T) {
	cfg := Default()
	got := cfg.OpenStatusNames()

	// Today the open set is exactly the non-closed statuses — the three
	// workflow positions. "deferred" is closed and must not appear.
	want := []string{"in-progress", "todo", "draft"}
	if !slices.Equal(got, want) {
		t.Errorf("OpenStatusNames() = %v, want %v", got, want)
	}

	// Open and closed must partition the declared status vocabulary: every
	// status appears in exactly one group, and neither group returns a name
	// that is not declared. Asserted against DefaultStatuses rather than
	// against the other helper, so it stays a real check if either helper
	// stops deriving its set from the Closed flag — comparing the two to each
	// other would only restate that they are complementary filters over one
	// slice, which cannot fail.
	closed := cfg.ClosedStatusNames()
	for _, s := range DefaultStatuses {
		inOpen := slices.Contains(got, s.Name)
		inClosed := slices.Contains(closed, s.Name)
		if inOpen == inClosed {
			t.Errorf("status %q: open=%v closed=%v — every status must be in exactly one group",
				s.Name, inOpen, inClosed)
		}
	}
	if len(got)+len(closed) != len(DefaultStatuses) {
		t.Errorf("OpenStatusNames() (%v) + ClosedStatusNames() (%v) = %d names, want %d — "+
			"the two groups must cover the declared statuses exactly once between them",
			got, closed, len(got)+len(closed), len(DefaultStatuses))
	}
}

// TestStatusReleasesDependents pins the second per-status classification: which
// statuses satisfy a dependency. It is deliberately narrower than the closed
// set — deferred is closed but still blocks, since the set-aside work is coming
// back — so asserting it against IsClosedStatus is the whole point of the test.
func TestStatusReleasesDependents(t *testing.T) {
	cfg := Default()

	tests := []struct {
		status string
		want   bool
	}{
		{"completed", true},
		{"scrapped", true},
		{"deferred", false}, // closed, but the dependency is unsatisfied
		{"todo", false},
		{"in-progress", false},
		{"draft", false},
		{"", false}, // unknown statuses keep blocking
		{"bogus", false},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			if got := cfg.StatusReleasesDependents(tt.status); got != tt.want {
				t.Errorf("StatusReleasesDependents(%q) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}

	// The two classifications must not collapse back into one: releasing is a
	// STRICT subset of closed. If they ever coincide again, deferring a blocker
	// silently unblocks everything it was gating.
	closed := cfg.ClosedStatusNames()
	releasing := cfg.ReleasingStatusNames()
	for _, name := range releasing {
		if !slices.Contains(closed, name) {
			t.Errorf("%q releases dependents but is not closed", name)
		}
	}
	if len(releasing) >= len(closed) {
		t.Errorf("releasing (%v) must be a strict subset of closed (%v)", releasing, closed)
	}
}

func TestReleasingStatusNames(t *testing.T) {
	cfg := Default()
	got := cfg.ReleasingStatusNames()

	// DefaultStatuses order, and deliberately without "deferred".
	want := []string{"completed", "scrapped"}
	if !slices.Equal(got, want) {
		t.Errorf("ReleasingStatusNames() = %v, want %v", got, want)
	}

	// Every returned name must satisfy the canonical predicate.
	for _, name := range got {
		if !cfg.StatusReleasesDependents(name) {
			t.Errorf("ReleasingStatusNames() returned %q which does not release dependents", name)
		}
	}
}

// TestHoldingStatusNames pins the set difference the agent-facing docs state as
// the "closed but still blocks" rule: closed, but not releasing. It must be
// exactly the closed names that ReleasingStatusNames leaves out, so the rule
// can never name a status that in fact frees its dependents.
func TestHoldingStatusNames(t *testing.T) {
	cfg := Default()
	got := cfg.HoldingStatusNames()

	// DefaultStatuses order. Today only "deferred" is closed without releasing.
	want := []string{"deferred"}
	if !slices.Equal(got, want) {
		t.Errorf("HoldingStatusNames() = %v, want %v", got, want)
	}

	// Every returned name must be closed and must not release its dependents.
	for _, name := range got {
		if !cfg.IsClosedStatus(name) {
			t.Errorf("HoldingStatusNames() returned %q which is not closed", name)
		}
		if cfg.StatusReleasesDependents(name) {
			t.Errorf("HoldingStatusNames() returned %q which does release its dependents", name)
		}
	}

	// Holding and releasing must partition the closed group: a closed status
	// either settles its dependencies or keeps holding them, never both or
	// neither. Asserted against DefaultStatuses rather than against the other
	// helper, so it stays a real check if either stops reading the flags.
	releasing := cfg.ReleasingStatusNames()
	for _, s := range DefaultStatuses {
		if !s.Role.Closed() {
			continue
		}
		inHolding := slices.Contains(got, s.Name)
		inReleasing := slices.Contains(releasing, s.Name)
		if inHolding == inReleasing {
			t.Errorf("closed status %q: holding=%v releasing=%v — a closed status must be in exactly one of the two",
				s.Name, inHolding, inReleasing)
		}
	}
}

// TestIsStartableStatus pins the third per-status classification: which
// statuses work can be picked up from. It is strictly narrower than "not
// closed" — draft and in-progress are open and still not startable — so
// asserting it against IsClosedStatus is the whole point of the test.
func TestIsStartableStatus(t *testing.T) {
	cfg := Default()

	tests := []struct {
		status string
		want   bool
	}{
		{"todo", true},
		{"in-progress", false}, // already underway; not something to start
		{"draft", false},       // needs refinement first
		{"deferred", false},    // closed
		{"completed", false},
		{"scrapped", false},
		{"", false},      // a hand-edited nib with no status: stays out of the queue
		{"bogus", false}, // and so does an unrecognized one
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			if got := cfg.IsStartableStatus(tt.status); got != tt.want {
				t.Errorf("IsStartableStatus(%q) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}

	// The startable set must stay a strict subset of the open set. Reading
	// startability off the Closed flag instead would put draft and in-progress
	// work into the ready queue.
	//
	// Membership is asserted before the count, because the count alone is not a
	// subset test: a one-element startable set naming a status that is not open
	// at all still satisfies len(startable) < len(open).
	startable := cfg.StartableStatusNames()
	open := cfg.OpenStatusNames()
	for _, name := range startable {
		if !slices.Contains(open, name) {
			t.Errorf("startable status %q is not open (open = %v) — a closed status must never be startable", name, open)
		}
	}
	if len(startable) >= len(open) {
		t.Errorf("startable (%v) must be a strict subset of open (%v)", startable, open)
	}
}

// TestStartableStatusNames pins the ready queue's status half: today `todo` is
// the only status work can be picked up from. Every other status has a reason
// not to be here, so adding one is a deliberate change that must fail here
// first.
func TestStartableStatusNames(t *testing.T) {
	cfg := Default()
	got := cfg.StartableStatusNames()

	// DefaultStatuses order. Today `todo` alone.
	want := []string{"todo"}
	if !slices.Equal(got, want) {
		t.Errorf("StartableStatusNames() = %v, want %v", got, want)
	}

	// Every returned name must satisfy the canonical predicate, and must be a
	// status a nib can actually carry.
	for _, name := range got {
		if !cfg.IsStartableStatus(name) {
			t.Errorf("StartableStatusNames() returned %q which is not IsStartableStatus", name)
		}
		if !cfg.IsValidStatus(name) {
			t.Errorf("StartableStatusNames() returned %q which is not a declared status", name)
		}
	}

	// Asserted against DefaultStatuses rather than against the helper, so this
	// stays a real check if the helper stops reading the flag: exactly the
	// statuses declaring Startable are returned, and no others.
	for _, s := range DefaultStatuses {
		if inSet := slices.Contains(got, s.Name); inSet != s.Role.Startable() {
			t.Errorf("status %q: Role.Startable()=%v but StartableStatusNames() membership=%v",
				s.Name, s.Role.Startable(), inSet)
		}
	}
}

// TestStatusRoleGroupsAreNonEmpty requires each role-derived group to be
// non-empty. This is not tidiness: a derived set that empties out fails OPEN,
// not closed. Emptying Startable widened `nibs list --ready` from "only
// startable" to every unblocked nib (86 of 89 on the sample fixture, including
// completed and scrapped work), because an empty include-list filters nothing.
// (The illegal flag combinations the old TestStatusFlagCombinationsAreLegal
// ruled out are unrepresentable now — a Role is one legal combination.)
func TestStatusRoleGroupsAreNonEmpty(t *testing.T) {
	groups := []struct {
		name string
		why  string
		n    int
	}{
		{"open", "every nib would be closed, so nothing could be worked on", countStatuses(func(s StatusConfig) bool { return !s.Role.Closed() })},
		{"closed", "nothing could ever leave the board", countStatuses(func(s StatusConfig) bool { return s.Role.Closed() })},
		{"startable", "`nibs list --ready` could return nothing, and an empty include-list would make it return everything", countStatuses(func(s StatusConfig) bool { return s.Role.Startable() })},
		{"releasing", "closing a blocker would never free the work it gates", countStatuses(func(s StatusConfig) bool { return s.Role.ReleasesDependents() })},
		{"done", "no close reason would count as an accomplishment, and `nibs close` could not derive its default reason", countStatuses(func(s StatusConfig) bool { return s.Role == RoleDone })},
		{"dropped", "no close reason would take work out of scope, so progress could never shed abandoned work", countStatuses(func(s StatusConfig) bool { return s.Role == RoleDropped })},
	}
	for _, g := range groups {
		t.Run("at least one "+g.name+" status", func(t *testing.T) {
			if g.n == 0 {
				t.Errorf("no status is %s — %s", g.name, g.why)
			}
		})
	}
}

// TestWorkflowStatusOrderCoversEveryStatus is the membership half of the two
// status orders: workflowStatusOrder may list the same names in a different
// sequence than DefaultStatuses, but it may not hold a name that is not a
// status, and it may not forget one. Forgetting one is the failure that
// matters — orderStatusesBy appends the missing status so no picker hides it,
// which means the mistake is invisible at runtime and this test is the only
// thing that reports it.
func TestWorkflowStatusOrderCoversEveryStatus(t *testing.T) {
	declared := Default().StatusNames()

	for _, name := range workflowStatusOrder {
		if !slices.Contains(declared, name) {
			t.Errorf("workflowStatusOrder lists %q, which is not a declared status (%s)", name, Default().StatusList())
		}
	}
	for _, name := range declared {
		if !slices.Contains(workflowStatusOrder, name) {
			t.Errorf("status %q is missing from workflowStatusOrder — pickers will show it last instead of in the flow", name)
		}
	}
	if len(workflowStatusOrder) != len(declared) {
		t.Errorf("workflowStatusOrder has %d entries, DefaultStatuses has %d — a duplicate?", len(workflowStatusOrder), len(declared))
	}
}

// TestWorkflowStatuses pins the order pickers show, and that each entry carries
// the same StatusConfig the rest of the config serves for that name.
func TestWorkflowStatuses(t *testing.T) {
	cfg := Default()

	want := []string{"draft", "todo", "in-progress", "completed", "deferred", "scrapped"}
	if got := cfg.WorkflowStatusNames(); !slices.Equal(got, want) {
		t.Errorf("WorkflowStatusNames() = %v, want %v (transition order)", got, want)
	}

	for _, s := range cfg.WorkflowStatuses() {
		declared := cfg.GetStatus(s.Name)
		if declared == nil {
			t.Errorf("WorkflowStatuses() offers %q, which GetStatus does not know", s.Name)
			continue
		}
		if s != *declared {
			t.Errorf("WorkflowStatuses() entry %q = %+v, want %+v — the flags and color must come from DefaultStatuses", s.Name, s, *declared)
		}
	}
}

// TestOrderStatusesByKeepsEveryStatus proves the fail-safe in orderStatusesBy:
// an order that forgets a status still yields every status, with the forgotten
// one last. A picker built on a lossy reorder would silently stop offering a
// status work can legitimately be set to.
func TestOrderStatusesByKeepsEveryStatus(t *testing.T) {
	t.Run("forgotten status is appended", func(t *testing.T) {
		got := namesOf(orderStatusesBy([]string{"todo", "draft"}))
		if len(got) != len(DefaultStatuses) {
			t.Fatalf("orderStatusesBy dropped statuses: got %v, want all %d", got, len(DefaultStatuses))
		}
		if got[0] != "todo" || got[1] != "draft" {
			t.Errorf("named statuses came out as %v, want todo, draft first", got[:2])
		}
		for _, s := range DefaultStatuses {
			if !slices.Contains(got, s.Name) {
				t.Errorf("status %q is missing from %v", s.Name, got)
			}
		}
	})

	t.Run("unknown and duplicate names do not add entries", func(t *testing.T) {
		got := namesOf(orderStatusesBy([]string{"todo", "todo", "nonesuch"}))
		if len(got) != len(DefaultStatuses) {
			t.Errorf("orderStatusesBy = %v (%d entries), want %d — each status exactly once", got, len(got), len(DefaultStatuses))
		}
	})
}

func namesOf(statuses []StatusConfig) []string {
	names := make([]string, len(statuses))
	for i, s := range statuses {
		names[i] = s.Name
	}
	return names
}

func countStatuses(pred func(StatusConfig) bool) int {
	n := 0
	for _, s := range DefaultStatuses {
		if pred(s) {
			n++
		}
	}
	return n
}

func TestGetStatus(t *testing.T) {
	cfg := Default()

	t.Run("existing status", func(t *testing.T) {
		s := cfg.GetStatus("todo")
		if s == nil {
			t.Fatal("GetStatus(\"todo\") = nil, want non-nil")
			return
		}
		if s.Name != "todo" {
			t.Errorf("Name = %q, want \"todo\"", s.Name)
		}
		if s.Color != "green" {
			t.Errorf("Color = %q, want \"green\"", s.Color)
		}
	})

	t.Run("non-existing status", func(t *testing.T) {
		s := cfg.GetStatus("invalid")
		if s != nil {
			t.Errorf("GetStatus(\"invalid\") = %v, want nil", s)
		}
	})

	t.Run("old status names not valid", func(t *testing.T) {
		s := cfg.GetStatus("open")
		if s != nil {
			t.Errorf("GetStatus(\"open\") = %v, want nil (old status name)", s)
		}
		s = cfg.GetStatus("done")
		if s != nil {
			t.Errorf("GetStatus(\"done\") = %v, want nil (old status name)", s)
		}
		s = cfg.GetStatus("ready")
		if s != nil {
			t.Errorf("GetStatus(\"ready\") = %v, want nil (old status name)", s)
		}
	})
}

func TestGetDefaultStatus(t *testing.T) {
	cfg := Default()
	got := cfg.GetDefaultStatus()

	if got != "todo" {
		t.Errorf("GetDefaultStatus() = %q, want \"todo\"", got)
	}
}

func TestGetDefaultType(t *testing.T) {
	cfg := Default()
	got := cfg.GetDefaultType()

	if got != "task" {
		t.Errorf("GetDefaultType() = %q, want \"task\"", got)
	}
}

func TestIsClosedStatus(t *testing.T) {
	cfg := Default()

	tests := []struct {
		status string
		want   bool
	}{
		{"completed", true},
		{"scrapped", true},
		{"draft", false},
		{"todo", false},
		{"in-progress", false},
		{"deferred", true}, // set aside is a close reason
		{"invalid", false},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := cfg.IsClosedStatus(tt.status)
			if got != tt.want {
				t.Errorf("IsClosedStatus(%q) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

// TestClosedStatusSetUnchanged characterizes the closed/open split so a rename
// or refactor of the status vocabulary cannot quietly move a status across the
// boundary. Membership — not naming — is what every consumer depends on:
// "open" is the workflow position {draft, todo, in-progress} and "closed" is
// the close reason {deferred, completed, scrapped}. Moving a status across is a
// deliberate change that must fail here first.
func TestClosedStatusSetUnchanged(t *testing.T) {
	cfg := Default()

	tests := []struct {
		status   string
		wantOpen bool
	}{
		{"in-progress", true},
		{"todo", true},
		{"draft", true},
		{"deferred", false},
		{"completed", false},
		{"scrapped", false},
	}

	closed := cfg.ClosedStatusNames()
	open := cfg.OpenStatusNames()

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			group, want := closed, "closed"
			if tt.wantOpen {
				group, want = open, "open"
			}
			if !slices.Contains(group, tt.status) {
				t.Errorf("%s is not in the %s group (closed=%v open=%v)", tt.status, want, closed, open)
			}
			if got := cfg.IsClosedStatus(tt.status); got == tt.wantOpen {
				t.Errorf("IsClosedStatus(%q) = %v, want %v", tt.status, got, !tt.wantOpen)
			}
		})
	}

	// The closed group is exactly these three, in this order — nothing else may
	// have been folded into it.
	if want := []string{"deferred", "completed", "scrapped"}; !slices.Equal(closed, want) {
		t.Errorf("ClosedStatusNames() = %v, want %v", closed, want)
	}
}

func TestLoadNonExistent(t *testing.T) {
	// Load from non-existent directory should return defaults
	cfg, err := Load("/nonexistent/path/that/does/not/exist")
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	// Should have default values
	if cfg.Nibs.IDLength != 4 {
		t.Errorf("IDLength = %d, want 4", cfg.Nibs.IDLength)
	}
}

func TestLoadAndSave(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()

	// Create a config (statuses are no longer stored in config)
	cfg := &Config{
		Nibs: NibsConfig{
			Prefix:      "test-",
			IDLength:    6,
			DefaultType: "bug",
		},
	}
	cfg.SetStoreDir(tmpDir)

	// Save it
	if _, err := cfg.Save(tmpDir); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify file exists
	configPath := filepath.Join(tmpDir, store.ConfigFileName)
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("config file was not created")
	}

	// Load it back
	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Verify values
	if loaded.Nibs.Prefix != "test-" {
		t.Errorf("Prefix = %q, want \"test-\"", loaded.Nibs.Prefix)
	}
	if loaded.Nibs.IDLength != 6 {
		t.Errorf("IDLength = %d, want 6", loaded.Nibs.IDLength)
	}
	if loaded.Nibs.DefaultType != "bug" {
		t.Errorf("DefaultType = %q, want \"bug\"", loaded.Nibs.DefaultType)
	}
	// Statuses are hardcoded, not stored in config
	if len(loaded.StatusNames()) != 6 {
		t.Errorf("len(StatusNames()) = %d, want 6", len(loaded.StatusNames()))
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	// Create temp directory with minimal config
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, store.ConfigFileName)

	// Write minimal config (missing id_length and default_type)
	minimalConfig := `nibs:
  prefix: "my-"
`
	if err := os.WriteFile(configPath, []byte(minimalConfig), 0644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	// Load it
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Verify defaults were applied
	if cfg.Nibs.IDLength != 4 {
		t.Errorf("IDLength default not applied: got %d, want 4", cfg.Nibs.IDLength)
	}
	// Statuses are hardcoded, always 6
	if len(cfg.StatusNames()) != 6 {
		t.Errorf("Hardcoded statuses: got %d, want 6", len(cfg.StatusNames()))
	}
	// DefaultStatus is always "todo"
	if cfg.GetDefaultStatus() != "todo" {
		t.Errorf("DefaultStatus: got %q, want \"todo\"", cfg.GetDefaultStatus())
	}
	// An omitted default_type resolves to what nibs init would have written, not
	// to the first entry of a list ordered by hierarchy depth. This assertion
	// used to require "milestone" and so pinned the defect in place.
	if cfg.Nibs.DefaultType != defaultTypeName {
		t.Errorf("DefaultType default not applied: got %q, want %q", cfg.Nibs.DefaultType, defaultTypeName)
	}
}

func TestStatusesAreHardcoded(t *testing.T) {
	// Statuses are hardcoded and not configurable (like types)
	// Verify that any config only uses hardcoded statuses
	cfg := Default()

	// All hardcoded statuses should be valid
	hardcodedStatuses := []string{"draft", "todo", "in-progress", "deferred", "completed", "scrapped"}
	for _, status := range hardcodedStatuses {
		if !cfg.IsValidStatus(status) {
			t.Errorf("IsValidStatus(%q) = false, want true", status)
		}
	}

	// Closed statuses should be completed and scrapped
	if !cfg.IsClosedStatus("completed") {
		t.Error("IsClosedStatus(\"completed\") = false, want true")
	}
	if !cfg.IsClosedStatus("scrapped") {
		t.Error("IsClosedStatus(\"scrapped\") = false, want true")
	}
	if cfg.IsClosedStatus("todo") {
		t.Error("IsClosedStatus(\"todo\") = true, want false")
	}
}

func TestIsValidType(t *testing.T) {
	cfg := Default()

	tests := []struct {
		typeName string
		want     bool
	}{
		{"epic", true},
		{"milestone", true},
		{"feature", true},
		{"bug", true},
		{"task", true},
		{"invalid", false},
		{"", false},
		{"TASK", false}, // case sensitive
	}

	for _, tt := range tests {
		t.Run(tt.typeName, func(t *testing.T) {
			got := cfg.IsValidType(tt.typeName)
			if got != tt.want {
				t.Errorf("IsValidType(%q) = %v, want %v", tt.typeName, got, tt.want)
			}
		})
	}
}

func TestTypeList(t *testing.T) {
	cfg := Default()
	got := cfg.TypeList()
	want := "milestone, epic, bug, feature, task, research"

	if got != want {
		t.Errorf("TypeList() = %q, want %q", got, want)
	}
}

func TestGetType(t *testing.T) {
	cfg := Default()

	t.Run("existing type", func(t *testing.T) {
		typ := cfg.GetType("bug")
		if typ == nil {
			t.Fatal("GetType(\"bug\") = nil, want non-nil")
			return
		}
		if typ.Name != "bug" {
			t.Errorf("Name = %q, want \"bug\"", typ.Name)
		}
		if typ.Color != "red" {
			t.Errorf("Color = %q, want \"red\"", typ.Color)
		}
	})

	t.Run("non-existing type", func(t *testing.T) {
		// GetType returns nil for unknown types
		typ := cfg.GetType("invalid-type")
		if typ != nil {
			t.Errorf("GetType(\"invalid-type\") = %v, want nil", typ)
		}
	})

	t.Run("all hardcoded types exist", func(t *testing.T) {
		expectedTypes := []string{"milestone", "epic", "bug", "feature", "task", "research"}
		for _, typeName := range expectedTypes {
			typ := cfg.GetType(typeName)
			if typ == nil {
				t.Errorf("GetType(%q) = nil, want non-nil", typeName)
			}
		}
	})
}

func TestTypesAreHardcoded(t *testing.T) {
	// Types are hardcoded and not stored in config
	// Verify that saving and loading a config doesn't affect types

	tmpDir := t.TempDir()

	cfg := &Config{
		Nibs: NibsConfig{
			Prefix:      "test-",
			IDLength:    4,
			DefaultType: "task",
		},
	}
	cfg.SetStoreDir(tmpDir)

	// Save it
	if _, err := cfg.Save(tmpDir); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Load it back
	configPath := filepath.Join(tmpDir, store.ConfigFileName)
	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Types should always come from DefaultTypes, not config
	if len(loaded.TypeNames()) != 6 {
		t.Errorf("len(TypeNames()) = %d, want 6", len(loaded.TypeNames()))
	}

	// All default types should be accessible
	for _, typeName := range []string{"milestone", "epic", "bug", "feature", "task", "research"} {
		if !loaded.IsValidType(typeName) {
			t.Errorf("IsValidType(%q) = false, want true", typeName)
		}
	}

	// Statuses should also be hardcoded
	if len(loaded.StatusNames()) != 6 {
		t.Errorf("len(StatusNames()) = %d, want 6", len(loaded.StatusNames()))
	}
}

func TestTypeDescriptions(t *testing.T) {
	t.Run("hardcoded types have descriptions", func(t *testing.T) {
		cfg := Default()

		expectedDescriptions := map[string]string{
			"epic":      "A deliverable that tops the work tree; should have child nibs, not be worked on directly",
			"milestone": "A target release or checkpoint; group work that should ship together",
			"feature":   "A user-facing capability or enhancement",
			"bug":       "Something that is broken and needs fixing",
			"task":      "A concrete piece of work to complete (eg. a chore, or a sub-task for a feature)",
			"research":  "Exploratory work whose output is knowledge or decisions, not code",
		}

		for typeName, expectedDesc := range expectedDescriptions {
			typ := cfg.GetType(typeName)
			if typ == nil {
				t.Errorf("GetType(%q) = nil, want non-nil", typeName)
				continue
			}
			if typ.Description != expectedDesc {
				t.Errorf("Type %q description = %q, want %q", typeName, typ.Description, expectedDesc)
			}
		}
	})

	t.Run("types in config file are ignored", func(t *testing.T) {
		// Even if a config file has custom types, they should be ignored
		// and hardcoded types should be used instead
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, store.ConfigFileName)

		// Config with custom types (should be ignored)
		configYAML := `nibs:
  prefix: "test-"
  id_length: 4
  default_status: open
statuses:
  - name: open
    color: green
types:
  - name: custom-type
    color: pink
    description: "This should be ignored"
`
		if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
			t.Fatalf("WriteFile error = %v", err)
		}

		loaded, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		// Custom type should not be valid
		if loaded.IsValidType("custom-type") {
			t.Error("IsValidType(\"custom-type\") = true, want false (custom types should be ignored)")
		}

		// Hardcoded types should still work
		if !loaded.IsValidType("bug") {
			t.Error("IsValidType(\"bug\") = false, want true")
		}
	})
}

func TestStatusDescriptions(t *testing.T) {
	t.Run("hardcoded statuses have descriptions", func(t *testing.T) {
		cfg := Default()

		expectedDescriptions := map[string]string{
			"draft":       "Needs refinement before it can be worked on",
			"todo":        "Ready to be worked on",
			"in-progress": "Currently being worked on",
			"deferred":    "Set aside — a good idea at the wrong time; closed, but kept as a seed rather than a dead end",
			"completed":   "Finished successfully",
			"scrapped":    "Will not be done",
		}

		for statusName, expectedDesc := range expectedDescriptions {
			status := cfg.GetStatus(statusName)
			if status == nil {
				t.Errorf("GetStatus(%q) = nil, want non-nil", statusName)
				continue
			}
			if status.Description != expectedDesc {
				t.Errorf("Status %q description = %q, want %q", statusName, status.Description, expectedDesc)
			}
		}
	})

	t.Run("statuses in config file are ignored", func(t *testing.T) {
		// Even if a config file has custom statuses, they should be ignored
		// and hardcoded statuses should be used instead
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, store.ConfigFileName)

		// Config with custom statuses (should be ignored)
		configYAML := `nibs:
  prefix: "test-"
  id_length: 4
statuses:
  - name: custom-status
    color: pink
    description: "This should be ignored"
`
		if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
			t.Fatalf("WriteFile error = %v", err)
		}

		loaded, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		// Custom status should not be valid
		if loaded.IsValidStatus("custom-status") {
			t.Error("IsValidStatus(\"custom-status\") = true, want false (custom statuses should be ignored)")
		}

		// Hardcoded statuses should still work
		if !loaded.IsValidStatus("todo") {
			t.Error("IsValidStatus(\"todo\") = false, want true")
		}
	})
}

// TestLoadFromStore pins the read of an ALREADY-RESOLVED store's config: the
// config is taken from inside the store directory, and the loaded config
// remembers which store it came from. Resolving the store is a separate
// question, answered by internal/store's locators and covered there — this
// package no longer walks for one.
func TestLoadFromStore(t *testing.T) {
	t.Run("reads the config inside the store", func(t *testing.T) {
		tmpDir := t.TempDir()
		storeDir := filepath.Join(tmpDir, store.DirName)
		if err := os.MkdirAll(storeDir, 0755); err != nil {
			t.Fatalf("MkdirAll error = %v", err)
		}
		configYAML := `nibs:
  prefix: test-
  id_length: 6
`
		if err := os.WriteFile(filepath.Join(storeDir, store.ConfigFileName), []byte(configYAML), 0644); err != nil {
			t.Fatalf("WriteFile error = %v", err)
		}

		cfg, err := LoadFromStore(storeDir)
		if err != nil {
			t.Fatalf("LoadFromStore() error = %v", err)
		}
		if cfg.Nibs.Prefix != "test-" {
			t.Errorf("Prefix = %q, want \"test-\"", cfg.Nibs.Prefix)
		}
		if cfg.Nibs.IDLength != 6 {
			t.Errorf("IDLength = %d, want 6", cfg.Nibs.IDLength)
		}
		if cfg.StoreDir() != storeDir {
			t.Errorf("StoreDir() = %q, want %q", cfg.StoreDir(), storeDir)
		}
	})

	t.Run("a store without a config file loads as defaults", func(t *testing.T) {
		tmpDir := t.TempDir()
		storeDir := filepath.Join(tmpDir, store.DirName)
		if err := os.MkdirAll(storeDir, 0755); err != nil {
			t.Fatalf("MkdirAll error = %v", err)
		}

		cfg, err := LoadFromStore(storeDir)
		if err != nil {
			t.Fatalf("LoadFromStore() error = %v", err)
		}
		if cfg.Nibs.IDLength != 4 {
			t.Errorf("IDLength = %d, want the system default 4", cfg.Nibs.IDLength)
		}
		if cfg.StoreDir() != storeDir {
			t.Errorf("StoreDir() = %q, want %q", cfg.StoreDir(), storeDir)
		}
	})
}

// TestLoadRejectsRetiredNibsPath pins the refusal behavior 8 describes: the
// `nibs.path` key pointed the config at a data directory somewhere else, which
// the store layout retired. A config still carrying it describes a layout this
// build cannot honor, so reading it as decoration and silently operating on a
// different directory is the one outcome that must not happen.
//
// The `want` list is also this message's only guard from the CMD side. Two
// cmd/refusal_invariant_test.go rows drive this refusal through
// resolveCLIStore's `%w`-only wrappers, so every substantive part of what the
// user reads — the path, the backticked command — is written here, and that
// test parses cmd/root.go alone and cannot see it. The path in particular is
// guarded at neither end without this row: the composed message keeps a path
// from the wrapper's own format, so cmd's totality check stays satisfied while
// the reader loses the one thing telling them WHICH config to edit.
func TestLoadRejectsRetiredNibsPath(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, store.ConfigFileName)
	body := "nibs:\n  path: custom-nibs\n  prefix: test-\n"
	if err := os.WriteFile(configPath, []byte(body), 0644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("Load() error = nil, want a refusal naming the retired nibs.path key")
	}
	for _, want := range []string{"nibs.path", "nibs migrate", configPath} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Load() error = %q, want it to mention %q", err.Error(), want)
		}
	}
}

func TestSaveIncludesHideCompletedAndWideMode(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := DefaultWithPrefix("test-")
	cfg.SetStoreDir(tmpDir)
	if _, err := cfg.Save(tmpDir); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmpDir, store.ConfigFileName))
	if err != nil {
		t.Fatalf("ReadFile error = %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "hide_completed:") {
		t.Errorf("saved config missing hide_completed field:\n%s", content)
	}
	if !strings.Contains(content, "wide_mode:") {
		t.Errorf("saved config missing wide_mode field:\n%s", content)
	}
}

func TestSavePreservesExplicitFalseValues(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := DefaultWithPrefix("test-")
	cfg.Nibs.HideCompleted = boolPtr(false)
	cfg.Nibs.WideMode = boolPtr(false)
	cfg.SetStoreDir(tmpDir)
	if _, err := cfg.Save(tmpDir); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmpDir, store.ConfigFileName))
	if err != nil {
		t.Fatalf("ReadFile error = %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "hide_completed:") {
		t.Errorf("saved config dropped hide_completed when false:\n%s", content)
	}
	if !strings.Contains(content, "wide_mode:") {
		t.Errorf("saved config dropped wide_mode when false:\n%s", content)
	}
}

func TestIsValidPriority(t *testing.T) {
	cfg := Default()

	tests := []struct {
		priority string
		want     bool
	}{
		{"critical", true},
		{"high", true},
		{"normal", true},
		{"low", true},
		{"deferred", false}, // removed as a priority (now a status)
		{"", true},          // empty is valid (means no priority)
		{"invalid", false},
		{"CRITICAL", false}, // case sensitive
		{"medium", false},   // not a valid priority
	}

	for _, tt := range tests {
		t.Run(tt.priority, func(t *testing.T) {
			got := cfg.IsValidPriority(tt.priority)
			if got != tt.want {
				t.Errorf("IsValidPriority(%q) = %v, want %v", tt.priority, got, tt.want)
			}
		})
	}
}

func TestPriorityList(t *testing.T) {
	cfg := Default()
	got := cfg.PriorityList()
	want := "critical, high, normal, low"

	if got != want {
		t.Errorf("PriorityList() = %q, want %q", got, want)
	}
}

func TestPriorityNames(t *testing.T) {
	cfg := Default()
	got := cfg.PriorityNames()

	if len(got) != 4 {
		t.Fatalf("len(PriorityNames()) = %d, want 4", len(got))
	}
	expected := []string{"critical", "high", "normal", "low"}
	for i, name := range expected {
		if got[i] != name {
			t.Errorf("PriorityNames()[%d] = %q, want %q", i, got[i], name)
		}
	}
}

func TestGetPriority(t *testing.T) {
	cfg := Default()

	t.Run("existing priority", func(t *testing.T) {
		p := cfg.GetPriority("high")
		if p == nil {
			t.Fatal("GetPriority(\"high\") = nil, want non-nil")
			return
		}
		if p.Name != "high" {
			t.Errorf("Name = %q, want \"high\"", p.Name)
		}
		if p.Color != "yellow" {
			t.Errorf("Color = %q, want \"yellow\"", p.Color)
		}
	})

	t.Run("non-existing priority", func(t *testing.T) {
		p := cfg.GetPriority("invalid")
		if p != nil {
			t.Errorf("GetPriority(\"invalid\") = %v, want nil", p)
		}
	})

	t.Run("empty priority returns nil", func(t *testing.T) {
		p := cfg.GetPriority("")
		if p != nil {
			t.Errorf("GetPriority(\"\") = %v, want nil", p)
		}
	})
}

func TestPriorityDescriptions(t *testing.T) {
	cfg := Default()

	expectedDescriptions := map[string]string{
		"critical": "Urgent, blocking work. When possible, address immediately",
		"high":     "Important, should be done before normal work",
		"normal":   "Standard priority",
		"low":      "Less important, can be delayed",
	}

	for priorityName, expectedDesc := range expectedDescriptions {
		p := cfg.GetPriority(priorityName)
		if p == nil {
			t.Errorf("GetPriority(%q) = nil, want non-nil", priorityName)
			continue
		}
		if p.Description != expectedDesc {
			t.Errorf("Priority %q description = %q, want %q", priorityName, p.Description, expectedDesc)
		}
	}
}

func TestPriorityRank(t *testing.T) {
	cfg := Default()

	tests := []struct {
		name     string
		priority string
		want     int
	}{
		// Known priorities return their index
		{"critical", "critical", 0},
		{"high", "high", 1},
		{"normal", "normal", 2},
		{"low", "low", 3},
		// Empty string treated as normal
		{"empty is normal", "", 2},
		// Unknown priority sorts last ("deferred" is no longer a priority)
		{"deferred is unknown", "deferred", len(DefaultPriorities)},
		{"unknown sorts last", "bogus", len(DefaultPriorities)},
		{"case sensitive", "CRITICAL", len(DefaultPriorities)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cfg.PriorityRank(tt.priority)
			if got != tt.want {
				t.Errorf("PriorityRank(%q) = %d, want %d", tt.priority, got, tt.want)
			}
		})
	}
}

func TestDefaultPrioritiesCount(t *testing.T) {
	if len(DefaultPriorities) != 4 {
		t.Errorf("len(DefaultPriorities) = %d, want 4", len(DefaultPriorities))
	}
}

func TestIsValidEstimate(t *testing.T) {
	cfg := Default()

	tests := []struct {
		estimate string
		want     bool
	}{
		{"s", true},
		{"m", true},
		{"l", true},
		{"xl", true},
		{"", true},   // empty is valid (means unestimated)
		{"S", false}, // case sensitive
		{"M", false},
		{"small", false},
		{"medium", false},
		{"large", false},
		{"xxl", false},
		{"xs", false},
	}

	for _, tt := range tests {
		name := tt.estimate
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			got := cfg.IsValidEstimate(tt.estimate)
			if got != tt.want {
				t.Errorf("IsValidEstimate(%q) = %v, want %v", tt.estimate, got, tt.want)
			}
		})
	}
}

func TestEstimateNames(t *testing.T) {
	cfg := Default()
	got := cfg.EstimateNames()

	expected := []string{"s", "m", "l", "xl"}
	if len(got) != len(expected) {
		t.Fatalf("len(EstimateNames()) = %d, want %d", len(got), len(expected))
	}
	for i, name := range expected {
		if got[i] != name {
			t.Errorf("EstimateNames()[%d] = %q, want %q", i, got[i], name)
		}
	}
}

func TestEstimateList(t *testing.T) {
	cfg := Default()
	got := cfg.EstimateList()
	want := "s, m, l, xl"

	if got != want {
		t.Errorf("EstimateList() = %q, want %q", got, want)
	}
}

func TestGetEstimate(t *testing.T) {
	cfg := Default()

	t.Run("existing estimate", func(t *testing.T) {
		e := cfg.GetEstimate("m")
		if e == nil {
			t.Fatal("GetEstimate(\"m\") = nil, want non-nil")
			return
		}
		if e.Name != "m" {
			t.Errorf("Name = %q, want \"m\"", e.Name)
		}
	})

	t.Run("non-existing estimate", func(t *testing.T) {
		e := cfg.GetEstimate("invalid")
		if e != nil {
			t.Errorf("GetEstimate(\"invalid\") = %v, want nil", e)
		}
	})

	t.Run("empty estimate returns nil", func(t *testing.T) {
		e := cfg.GetEstimate("")
		if e != nil {
			t.Errorf("GetEstimate(\"\") = %v, want nil", e)
		}
	})
}

func TestServerConfigDefaults(t *testing.T) {
	cfg := Default()

	if cfg.ServerPort() != 3000 {
		t.Errorf("ServerPort() = %d, want 3000", cfg.ServerPort())
	}
	if !cfg.ServerOpenBrowser() {
		t.Error("ServerOpenBrowser() = false, want true")
	}
}

func TestServerConfigYAMLRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	port := 8080
	openBrowser := false

	cfg := Default()
	cfg.Nibs.Server.Port = &port
	cfg.Nibs.Server.OpenBrowser = &openBrowser
	cfg.SetStoreDir(tmpDir)

	if _, err := cfg.Save(tmpDir); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load(filepath.Join(tmpDir, store.ConfigFileName))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded.ServerPort() != 8080 {
		t.Errorf("ServerPort() = %d, want 8080", loaded.ServerPort())
	}
	if loaded.ServerOpenBrowser() {
		t.Error("ServerOpenBrowser() = true, want false")
	}
}

func TestServerConfigPartialYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, store.ConfigFileName)

	// Only port set, open_browser omitted
	yaml := `nibs:
  prefix: "test-"
  server:
    port: 4000
`
	if err := os.WriteFile(configPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.ServerPort() != 4000 {
		t.Errorf("ServerPort() = %d, want 4000", cfg.ServerPort())
	}
	// open_browser not set in YAML -> should default to true
	if !cfg.ServerOpenBrowser() {
		t.Error("ServerOpenBrowser() = false, want true (default)")
	}
}

// TestGetProjectName pins the project name to the directory CONTAINING the
// store. Every store directory is called `.nibs`, so reading the name off the
// store itself would call every project "nibs" — and that name is user-visible:
// it titles the TUI border and the web page (via the GraphQL Config resolver).
func TestGetProjectName(t *testing.T) {
	tests := []struct {
		name     string
		storeDir string
		want     string
	}{
		{
			name:     "normal directory",
			storeDir: filepath.Join("home", "user", "my-project", ".nibs"),
			want:     "my-project",
		},
		{
			name:     "nested path",
			storeDir: filepath.Join("var", "repos", "boardgametracker", ".nibs"),
			want:     "boardgametracker",
		},
		{
			name:     "empty storeDir",
			storeDir: "",
			want:     "Nibs",
		},
		{
			name:     "store at the filesystem root has no project directory",
			storeDir: filepath.Join(string(filepath.Separator), ".nibs"),
			want:     "Nibs",
		},
		{
			name:     "relative store with no parent",
			storeDir: ".nibs",
			want:     "Nibs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			cfg.SetStoreDir(tt.storeDir)
			got := cfg.GetProjectName()
			if got != tt.want {
				t.Errorf("GetProjectName() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSavePreservesTheConfigsPermissions pins that the two writers of
// <store>/config.yml hold the same contract. The migration engine relocates this
// file atomically with the source's mode preserved, precisely so a 0600 config does
// not become world-readable; Save writing 0644 unconditionally undid that on the
// next `nibs config set-*`.
func TestSavePreservesTheConfigsPermissions(t *testing.T) {
	testskip.NeedPosixFileModes(t, t.TempDir())
	storeDir := filepath.Join(t.TempDir(), store.DirName)
	if err := os.MkdirAll(storeDir, 0755); err != nil {
		t.Fatal(err)
	}
	path := store.NewLayout(storeDir).ConfigPath()
	if err := os.WriteFile(path, []byte("nibs:\n  prefix: p-\n"), 0600); err != nil {
		t.Fatal(err)
	}

	cfg := Default()
	cfg.SetStoreDir(storeDir)
	if _, err := cfg.Save(storeDir); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Errorf("mode = %v after Save, want 0600 — the write widened a private config", got)
	}

	// A config that never existed still gets the ordinary mode.
	fresh := filepath.Join(t.TempDir(), store.DirName)
	freshCfg := Default()
	if _, err := freshCfg.Save(fresh); err != nil {
		t.Fatalf("Save into a new store: %v", err)
	}
	info, err = os.Stat(store.NewLayout(fresh).ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0644 {
		t.Errorf("mode = %v for a new config, want 0644", got)
	}
}

// TestSaveReportsReplacingASymlinkedConfig pins that a divergence Save creates
// cannot be silent.
//
// The atomic write renames a temp file over the destination, and a rename REPLACES
// a symlink. So a config.yml symlinked into a dotfile manager becomes a regular
// file holding the new settings while the manager's copy keeps the old ones — the
// next apply restores a stale prefix, and short-id resolution stops finding nibs
// created since. Save must hand the caller the path that is now stale.
func TestSaveReportsReplacingASymlinkedConfig(t *testing.T) {
	tmp := t.TempDir()
	external := filepath.Join(tmp, "dotfiles", "nibs-config.yml")
	if err := os.MkdirAll(filepath.Dir(external), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(external, []byte("nibs:\n  prefix: old-\n"), 0600); err != nil {
		t.Fatal(err)
	}
	storeDir := filepath.Join(tmp, store.DirName)
	if err := os.MkdirAll(storeDir, 0755); err != nil {
		t.Fatal(err)
	}
	path := store.NewLayout(storeDir).ConfigPath()
	if err := os.Symlink(external, path); err != nil {
		testskip.SymlinkUnavailable(t, err)
	}

	cfg := Default()
	cfg.Nibs.Prefix = "new-"
	cfg.SetStoreDir(storeDir)
	stale, err := cfg.Save(storeDir)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if stale != external {
		t.Errorf("Save reported stale target %q, want %q — the divergence must not be silent", stale, external)
	}
	// And the divergence is real, which is what makes the report load-bearing.
	if info, lerr := os.Lstat(path); lerr != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("Lstat(%s) = %v, %v; this test asserts nothing unless the link was replaced", path, info, lerr)
	}
	target, err := os.ReadFile(external)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(target), "old-") {
		t.Fatalf("the symlink target was updated after all, so there is nothing to report:\n%s", target)
	}

	// A plain regular file reports nothing.
	plain := filepath.Join(tmp, "plain", store.DirName)
	plainCfg := Default()
	if stale, err := plainCfg.Save(plain); err != nil || stale != "" {
		t.Errorf("Save into a link-free store = (%q, %v), want no stale target and no error", stale, err)
	}
}

// TestConfigReadsRefuseAnIrregularFile pins that something other than a regular
// file at a config path produces a DETERMINATE ERROR from every config read,
// rather than blocking the command forever.
//
// The hazard is open(2) on a FIFO: it blocks until a writer arrives, so a
// project whose `.nibs.yml` was a named pipe made every command hang — never
// returning and never failing, with nothing downstream able to bound it because
// the process never reached downstream. The shared reader is what has to answer,
// which is why the routes below are every entry point into it, reaching the
// three different FILES a run reads: the pre-layout config beside the store, the
// bound store's own config, and the user config. A regularity check at any one
// of them leaves the others hanging.
//
// The error must also not read as ABSENCE. loadRaw and LoadUserConfigFrom both
// treat os.IsNotExist as "use the defaults", so an irregular file reported that
// way would quietly become an empty config — a project with no prefix, whose
// next nib is written under a different id.
//
// EVERY READ RUNS UNDER A DEADLINE, in a goroutine that never touches t. A guard
// against a hang that hangs when it regresses takes the whole package's run with
// it, and a suite that never finishes is a much worse signal than a red test. On
// a regression that goroutine stays parked in open(2) until the binary exits;
// that is an acceptable cost once the guard has already failed.
//
// The directory row is not a second spelling of the FIFO row: it is what keeps
// this guard meaningful where FIFOs cannot be created (see testskip.NamedPipes),
// because it reaches the same refusal through the same branch. Asserting the
// wording is what ties it to that branch rather than to whatever errno a read of
// a directory happens to produce.
func TestConfigReadsRefuseAnIrregularFile(t *testing.T) {
	kinds := []struct {
		name   string
		create func(t *testing.T, path string)
	}{
		{
			name: "a named pipe",
			create: func(t *testing.T, path string) {
				if err := mkfifo(path); err != nil {
					testskip.Unavailable(t, testskip.NamedPipes, "mkfifo(%s): %v", path, err)
				}
			},
		},
		{
			name: "a directory",
			create: func(t *testing.T, path string) {
				if err := os.Mkdir(path, 0o755); err != nil {
					t.Fatalf("mkdir %s: %v", path, err)
				}
			},
		},
	}

	routes := []struct {
		name string
		// file is where the irregular thing goes, relative to a fresh directory
		// that read then works against.
		file string
		read func(dir string) error
	}{
		{
			name: "ReadConfigFile",
			file: store.ConfigFileName,
			read: func(dir string) error {
				_, err := ReadConfigFile(filepath.Join(dir, store.ConfigFileName))
				return err
			},
		},
		{
			name: "RetiredNibsPath",
			file: store.LegacyProjectConfigFileName,
			read: func(dir string) error {
				_, err := RetiredNibsPath(filepath.Join(dir, store.LegacyProjectConfigFileName))
				return err
			},
		},
		{
			name: "LoadFromStore",
			file: store.ConfigFileName,
			read: func(dir string) error {
				_, err := LoadFromStore(dir)
				return err
			},
		},
		{
			name: "LoadUserConfigFrom",
			file: "nibs.yml",
			read: func(dir string) error {
				_, err := LoadUserConfigFrom(filepath.Join(dir, "nibs.yml"))
				return err
			},
		},
	}

	// Generous, because the only thing it has to separate is "returned" from
	// "blocked in open(2) forever" — every one of these reads is a stat and a
	// few hundred bytes otherwise.
	const deadline = 10 * time.Second

	for _, kind := range kinds {
		for _, route := range routes {
			t.Run(kind.name+"/"+route.name, func(t *testing.T) {
				dir := t.TempDir()
				kind.create(t, filepath.Join(dir, route.file))

				// Buffered, so a read that unblocks after the deadline has passed
				// completes its send and exits instead of leaking on the channel.
				done := make(chan error, 1)
				go func() { done <- route.read(dir) }()

				var err error
				select {
				case err = <-done:
				case <-time.After(deadline):
					t.Fatalf("%s did not return within %s with %s at the config path; the read is blocked, which is the hang this guard exists for",
						route.name, deadline, kind.name)
				}
				if err == nil {
					t.Fatalf("%s accepted %s as a config", route.name, kind.name)
				}
				if os.IsNotExist(err) {
					t.Fatalf("%s reported %s as an absent file (%v); absence means \"use the defaults\", so this becomes an empty config",
						route.name, kind.name, err)
				}
				if !strings.Contains(err.Error(), "not a regular file") {
					t.Errorf("%s refused %s with %v, which does not come from the regularity check; the guard is passing on some other error",
						route.name, kind.name, err)
				}
			})
		}
	}
}

// TestLoadedFromFileSeparatesAnAbsentConfigFromAnEmptyOne pins the distinction
// Load's return value cannot carry on its own.
//
// Every load path applies the system defaults on the way out, so a store with no
// config.yml and a store whose config.yml declares nothing come back as the same
// fully-defaulted Config. That is right for a reader that only wants values, and
// wrong for the one that compares what the store declares against what it loaded
// earlier — there, "declares nothing" is not evidence of a change on disk and
// "declares this" may be. See nibcore.Core.mintingVocabulary.
func TestLoadedFromFileSeparatesAnAbsentConfigFromAnEmptyOne(t *testing.T) {
	tests := []struct {
		name  string
		write string // "" means write no file at all
		want  bool
	}{
		{name: "a store with no config file", want: false},
		{name: "a config file declaring nothing", write: "\n", want: true},
		{name: "a config file declaring a prefix", write: "nibs:\n  prefix: emb-\n", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storeDir := filepath.Join(t.TempDir(), store.DirName)
			if err := os.MkdirAll(storeDir, 0o755); err != nil {
				t.Fatalf("creating the store: %v", err)
			}
			if tt.write != "" {
				if err := os.WriteFile(store.NewLayout(storeDir).ConfigPath(), []byte(tt.write), 0o644); err != nil {
					t.Fatalf("writing the store config: %v", err)
				}
			}

			cfg, err := LoadFromStore(storeDir)
			if err != nil {
				t.Fatalf("LoadFromStore: %v", err)
			}
			if got := cfg.LoadedFromFile(); got != tt.want {
				t.Errorf("LoadedFromFile() = %v, want %v", got, tt.want)
			}
			// The values are identical across the first two rows, which is the
			// reason the flag has to carry the answer.
			if got := cfg.Nibs.IDLength; got != 4 {
				t.Errorf("IDLength = %d, want the system default 4 either way", got)
			}
		})
	}
}

// TestLoadedFromFileIsFalseForAnInMemoryConfig pins the other constructor: a
// Config nobody read off disk declares nothing about any store.
func TestLoadedFromFileIsFalseForAnInMemoryConfig(t *testing.T) {
	if Default().LoadedFromFile() {
		t.Error("Default().LoadedFromFile() = true, want false")
	}
	if DefaultWithPrefix("emb-").LoadedFromFile() {
		t.Error("DefaultWithPrefix().LoadedFromFile() = true, want false")
	}
}
