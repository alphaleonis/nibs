package cmd

import (
	"io"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/output"
)

// TestListMilestoneQueue pins `nibs list --milestone`: the queue in
// milestone_order by default, short ids accepted, and an explicit --sort
// winning over the queue-order default.
func TestListMilestoneQueue(t *testing.T) {
	nibsPath := setupQueueCLITest(t)

	if got, want := queueIDs(t, nibsPath, "tnib-m001"), "tnib-e001 tnib-e002 tnib-e003 tnib-e004"; strings.Join(got, " ") != want {
		t.Errorf("queue = %v, want %s", got, want)
	}
	if got, want := queueIDs(t, nibsPath, "m002"), "tnib-e005 tnib-e006"; strings.Join(got, " ") != want {
		t.Errorf("queue (short id) = %v, want %s", got, want)
	}

	// Move e003 to the front so queue order and id order disagree, then show
	// the default follows the queue while --sort id still wins.
	resetMvFlags()
	if _, err := runRootWith(t, "--nibs-path", nibsPath, "mv", "tnib-e003", "--queue", "--first"); err != nil {
		t.Fatalf("mv --queue --first: %v", err)
	}
	if got, want := queueIDs(t, nibsPath, "tnib-m001"), "tnib-e003 tnib-e001 tnib-e002 tnib-e004"; strings.Join(got, " ") != want {
		t.Errorf("queue after move = %v, want %s", got, want)
	}
	resetListFlags()
	out, err := runRootWith(t, "--nibs-path", nibsPath, "list", "--milestone", "tnib-m001", "--sort", "id", "-q")
	if err != nil {
		t.Fatalf("list --sort id: %v", err)
	}
	if got, want := strings.Join(strings.Fields(out), " "), "tnib-e001 tnib-e002 tnib-e003 tnib-e004"; got != want {
		t.Errorf("--sort id = %s, want %s (an explicit sort wins over queue order)", got, want)
	}
}

// TestListBacklog pins `nibs list --backlog`: the derived complement of every
// queue — an assigned nib is out, and so is its structural subtree (planned
// work, not backlog), while an unassigned root and the milestones themselves
// (in no queue) are in.
func TestListBacklog(t *testing.T) {
	nibsPath := setupQueueCLITest(t)
	out, err := runRootWith(t, "--nibs-path", nibsPath, "list", "--backlog", "--all", "-q")
	if err != nil {
		t.Fatalf("list --backlog: %v", err)
	}
	ids := " " + strings.Join(strings.Fields(out), " ") + " "
	for _, absent := range []string{"tnib-e001", "tnib-f001", "tnib-t001", "tnib-e005"} {
		if strings.Contains(ids, " "+absent+" ") {
			t.Errorf("--backlog lists %s, which is assigned or under an assigned epic", absent)
		}
	}
	for _, present := range []string{"tnib-t041", "tnib-m001"} {
		if !strings.Contains(ids, " "+present+" ") {
			t.Errorf("--backlog omits %s, which belongs to no milestone", present)
		}
	}

	resetListFlags()
	out, err = runRootWith(t, "--nibs-path", nibsPath, "list", "--backlog", "--all", "--no-type", "milestone", "-q")
	if err != nil {
		t.Fatalf("list --backlog --no-type milestone: %v", err)
	}
	if strings.Contains(" "+strings.Join(strings.Fields(out), " ")+" ", " tnib-m001 ") {
		t.Error("--no-type milestone should drop the milestones from the backlog listing")
	}
}

// TestListMilestoneFlagRefusals pins the flag-level refusals: an empty id, the
// queue/backlog contradiction (validation, exit 2) and an id naming no nib
// (not found, exit 3) — the same classes --parent reports — plus an id naming
// a nib that is not a milestone (validation, exit 2, naming its type — the
// class and message `set --milestone` gives the same id), so a mistyped id
// that lands on an epic is not answered with an empty queue.
func TestListMilestoneFlagRefusals(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantExit int
		wantMsg  string
	}{
		{"empty id", []string{"list", "--milestone", ""}, output.ExitValidation, "--milestone was given an empty value"},
		{"milestone with backlog", []string{"list", "--milestone", "tnib-m001", "--backlog"}, output.ExitValidation, "--milestone and --backlog are mutually exclusive"},
		{"unknown id", []string{"list", "--milestone", "nope"}, output.ExitNotFound, "nope"},
		{"non-milestone id names its type", []string{"list", "--milestone", "tnib-e001"}, output.ExitValidation, `"tnib-e001" has type epic, not milestone`},
		{"non-milestone id under --all", []string{"list", "--milestone", "tnib-t041", "--all"}, output.ExitValidation, "has type task, not milestone"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsPath := setupQueueCLITest(t)
			_, err := runRootWith(t, append([]string{"--nibs-path", nibsPath}, tt.args...)...)
			if err == nil {
				t.Fatal("list succeeded, want refusal")
			}
			if code := reportExitError(io.Discard, err); code != tt.wantExit {
				t.Errorf("exit = %d, want %d", code, tt.wantExit)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want substring %q", err.Error(), tt.wantMsg)
			}
		})
	}
}
