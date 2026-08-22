package cmd

import (
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/testdata/fixtures"
)

// setupQueueCLITest copies the sample fixture — whose queues are
// tnib-e001..e004 -> tnib-m001 (a..d) and tnib-e005/e006 -> tnib-m002 (a, b) —
// and registers the flag resets every command these tests drive needs, so one
// subtest's flags cannot leak into the next. Returns the store path.
func setupQueueCLITest(t *testing.T) string {
	t.Helper()
	resetQueueCLIFlags()
	t.Cleanup(resetQueueCLIFlags)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	return filepath.Join(fixtures.CopySampleProject(t), ".nibs")
}

func resetQueueCLIFlags() {
	resetSetFlags()
	resetListFlags()
	resetMvFlags()
	resetGetFlags()
	resetRootPersistentFlags()
}

// queueIDs runs `nibs list --milestone <ms> -q` and returns the queue in its
// reported order.
func queueIDs(t *testing.T, nibsPath, milestone string) []string {
	t.Helper()
	resetListFlags()
	resetRootPersistentFlags()
	out, err := runRootWith(t, "--nibs-path", nibsPath, "list", "--milestone", milestone, "-q")
	if err != nil {
		t.Fatalf("list --milestone %s: %v", milestone, err)
	}
	return strings.Fields(out)
}

// axisFields runs `nibs get <id> -f id,milestone,milestone_order,area --json`
// and returns the projected axis values.
func axisFields(t *testing.T, nibsPath, id string) (milestone, milestoneOrder, area string) {
	t.Helper()
	resetGetFlags()
	resetRootPersistentFlags()
	out, err := runRootWith(t, "--nibs-path", nibsPath, "get", id, "-f", "id,milestone,milestone_order,area", "--json")
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	var env struct {
		Nib struct {
			Milestone      string `json:"milestone"`
			MilestoneOrder string `json:"milestone_order"`
			Area           string `json:"area"`
		} `json:"nib"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("unmarshal get output: %v\nraw: %s", err, out)
	}
	return env.Nib.Milestone, env.Nib.MilestoneOrder, env.Nib.Area
}

// TestSetMilestoneAssignsAndQueues pins the happy path end to end: `nibs set
// --milestone` assigns (short ids normalize), the ordering engine appends the
// nib to the queue, the projection exposes the axis fields, and `nibs list
// --milestone` shows the queue in queue order.
func TestSetMilestoneAssignsAndQueues(t *testing.T) {
	nibsPath := setupQueueCLITest(t)

	if _, err := runRootWith(t, "--nibs-path", nibsPath, "set", "tnib-t041", "--milestone", "m001"); err != nil {
		t.Fatalf("set --milestone: %v", err)
	}
	ms, key, _ := axisFields(t, nibsPath, "tnib-t041")
	if ms != "tnib-m001" {
		t.Errorf("milestone = %q, want the normalized tnib-m001", ms)
	}
	if key <= "d" {
		t.Errorf("milestone_order = %q, want a key after d (appended last)", key)
	}
	if got, want := queueIDs(t, nibsPath, "tnib-m001"), []string{"tnib-e001", "tnib-e002", "tnib-e003", "tnib-e004", "tnib-t041"}; strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("queue = %v, want %v", got, want)
	}

	// The area projects too (tnib-f002 carries area: auth in the fixture).
	if _, _, area := axisFields(t, nibsPath, "tnib-f002"); area != "auth" {
		t.Errorf("area = %q, want auth", area)
	}
}

// TestSetMilestoneRefusals pins the write-strict refusals and their exit
// class: every one is a validation refusal (exit 2, like an unknown --parent
// target), names why, and leaves the nib unassigned.
func TestSetMilestoneRefusals(t *testing.T) {
	tests := []struct {
		name    string
		subject string
		target  string
		wantMsg []string
	}{
		{"unknown target", "tnib-t041", "nope", []string{"milestone nib not found", "nope"}},
		{"non-milestone target names its type", "tnib-t041", "tnib-e001", []string{"tnib-e001", "epic", "not milestone"}},
		{"milestone-typed subject", "tnib-m002", "tnib-m001", []string{"a milestone cannot be assigned to a milestone"}},
		{"ancestor already assigned", "tnib-f001", "tnib-m002", []string{"tnib-f001", "ancestor tnib-e001", "tnib-m001"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsPath := setupQueueCLITest(t)
			_, err := runRootWith(t, "--nibs-path", nibsPath, "set", tt.subject, "--milestone", tt.target)
			if err == nil {
				t.Fatal("set --milestone succeeded, want refusal")
			}
			if code := reportExitError(io.Discard, err); code != output.ExitValidation {
				t.Errorf("exit = %d, want %d (validation)", code, output.ExitValidation)
			}
			for _, want := range tt.wantMsg {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want substring %q", err.Error(), want)
				}
			}
		})
	}

	t.Run("descendant already assigned", func(t *testing.T) {
		nibsPath := setupQueueCLITest(t)
		// tnib-t041 sits under tnib-f020 with no assigned ancestor; once it is
		// queued, assigning its parent would put both on one chain.
		if _, err := runRootWith(t, "--nibs-path", nibsPath, "set", "tnib-t041", "--milestone", "tnib-m001"); err != nil {
			t.Fatalf("assigning the child: %v", err)
		}
		resetSetFlags()
		_, err := runRootWith(t, "--nibs-path", nibsPath, "set", "tnib-f020", "--milestone", "tnib-m002")
		if err == nil {
			t.Fatal("assigning the parent of an assigned nib succeeded, want refusal")
		}
		if code := reportExitError(io.Discard, err); code != output.ExitValidation {
			t.Errorf("exit = %d, want %d (validation)", code, output.ExitValidation)
		}
		for _, want := range []string{"tnib-f020", "descendant tnib-t041", "tnib-m001"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error = %q, want substring %q", err.Error(), want)
			}
		}
		if ms, _, _ := axisFields(t, nibsPath, "tnib-f020"); ms != "" {
			t.Errorf("refused assignment leaked: milestone = %q", ms)
		}
	})
}

// TestSetReparentWithMilestoneChange pins a combined `--parent` and milestone
// change in ONE set: it is judged on the state it leaves, so it succeeds
// exactly when the same two changes as separate commands would. tnib-f001
// sits under tnib-e001, which is assigned to tnib-m001.
func TestSetReparentWithMilestoneChange(t *testing.T) {
	t.Run("--parent under an assigned chain with --clear milestone", func(t *testing.T) {
		nibsPath := setupQueueCLITest(t)
		if _, err := runRootWith(t, "--nibs-path", nibsPath, "set", "tnib-t041", "--milestone", "m002"); err != nil {
			t.Fatalf("assigning the task: %v", err)
		}
		resetSetFlags()
		if _, err := runRootWith(t, "--nibs-path", nibsPath, "set", "tnib-t041", "--parent", "tnib-f001", "--clear", "milestone"); err != nil {
			t.Fatalf("--parent with --clear milestone was refused: %v (the two as separate commands both succeed)", err)
		}
		ms, key, _ := axisFields(t, nibsPath, "tnib-t041")
		if ms != "" || key != "" {
			t.Errorf("milestone=%q milestone_order=%q, want both cleared", ms, key)
		}
		resetGetFlags()
		resetRootPersistentFlags()
		out, err := runRootWith(t, "--nibs-path", nibsPath, "get", "tnib-t041", "-f", "parent", "--json")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if !strings.Contains(out, "tnib-f001") {
			t.Errorf("parent not moved: %s", out)
		}
	})

	// Both block orderings refuse this one; only the fixed order blames the
	// right half. tnib-t041 is unassigned, so the move on its own is legal —
	// a refusal phrased as "cannot move" sends the caller to retry the step
	// that was never the problem. The assignment, judged against the chain the
	// nib will sit on, is.
	t.Run("--parent under an assigned chain with --milestone is refused", func(t *testing.T) {
		nibsPath := setupQueueCLITest(t)
		_, err := runRootWith(t, "--nibs-path", nibsPath, "set", "tnib-t041", "--parent", "tnib-f001", "--milestone", "m002")
		if err == nil {
			t.Fatal("assigning while moving under an assigned chain succeeded, want refusal")
		}
		if code := reportExitError(io.Discard, err); code != output.ExitValidation {
			t.Errorf("exit = %d, want %d (validation)", code, output.ExitValidation)
		}
		for _, want := range []string{"cannot assign tnib-t041 to milestone tnib-m002", "its ancestor tnib-e001 is already assigned to milestone tnib-m001"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error = %q, want it to blame the assignment with %q", err.Error(), want)
			}
		}
	})
}

// TestSetTypeMilestoneWithClear pins a combined `--type milestone` and
// milestone clear in ONE set. The axis rule (a milestone takes no assignment)
// judges the state the command LEAVES, so converting an assigned nib into a
// milestone succeeds when the same call drops the assignment — exactly as the
// two commands issued separately do. A conversion that leaves an assignment
// standing is still refused.
func TestSetTypeMilestoneWithClear(t *testing.T) {
	files := map[string]string{
		"qm1--waypoint.md": "---\nversion: 2\ntitle: Waypoint\nstatus: todo\ntype: milestone\n---\n",
		"qm2--other.md":    "---\nversion: 2\ntitle: Other waypoint\nstatus: todo\ntype: milestone\n---\n",
		"qs--subject.md":   "---\nversion: 2\ntitle: Subject\nstatus: todo\ntype: task\nmilestone: qm1\nmilestone_order: a0\n---\n",
	}

	// Both spellings of the clear: --clear milestone (explicit null) and the
	// empty --milestone (which clears like --parent "").
	for _, clear := range []struct {
		name string
		args []string
	}{
		{"--clear milestone", []string{"--clear", "milestone"}},
		{`--milestone ""`, []string{"--milestone", ""}},
	} {
		t.Run("--type milestone with "+clear.name, func(t *testing.T) {
			nibsPath := writeStoreFiles(t, files)
			t.Cleanup(resetQueueCLIFlags)
			resetQueueCLIFlags()
			args := append([]string{"--nibs-path", nibsPath, "set", "qs", "--type", "milestone"}, clear.args...)
			if _, err := runRootWith(t, args...); err != nil {
				t.Fatalf("--type milestone with %s was refused: %v (the two as separate commands both succeed)", clear.name, err)
			}
			ms, key, _ := axisFields(t, nibsPath, "qs")
			if ms != "" || key != "" {
				t.Errorf("milestone=%q milestone_order=%q, want both cleared", ms, key)
			}
			resetGetFlags()
			resetRootPersistentFlags()
			out, err := runRootWith(t, "--nibs-path", nibsPath, "get", "qs", "-f", "type", "--json")
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if !strings.Contains(out, "milestone") {
				t.Errorf("type not converted: %s", out)
			}
		})
	}

	for _, tt := range []struct {
		name string
		args []string
	}{
		{"--type milestone alone on an assigned nib", nil},
		{"--type milestone beside an assignment to another milestone", []string{"--milestone", "qm2"}},
	} {
		t.Run(tt.name+" is refused", func(t *testing.T) {
			nibsPath := writeStoreFiles(t, files)
			t.Cleanup(resetQueueCLIFlags)
			resetQueueCLIFlags()
			args := append([]string{"--nibs-path", nibsPath, "set", "qs", "--type", "milestone"}, tt.args...)
			_, err := runRootWith(t, args...)
			if err == nil {
				t.Fatal("the conversion succeeded, want the axis refusal")
			}
			if code := reportExitError(io.Discard, err); code != output.ExitValidation {
				t.Errorf("exit = %d, want %d (validation)", code, output.ExitValidation)
			}
			if !strings.Contains(err.Error(), "a milestone cannot be assigned to a milestone") {
				t.Errorf("error = %q, want the axis refusal", err.Error())
			}
			if ms, _, _ := axisFields(t, nibsPath, "qs"); ms != "qm1" {
				t.Errorf("refusal leaked: milestone = %q, want qm1 untouched", ms)
			}
		})
	}
}

// TestSetClearMilestone pins the clear: `--clear milestone` drops both the
// assignment and the queue key, and setting and clearing in one call is
// refused like every other clearable field.
func TestSetClearMilestone(t *testing.T) {
	t.Run("clears assignment and queue key", func(t *testing.T) {
		nibsPath := setupQueueCLITest(t)
		if _, err := runRootWith(t, "--nibs-path", nibsPath, "set", "tnib-e002", "--clear", "milestone"); err != nil {
			t.Fatalf("set --clear milestone: %v", err)
		}
		ms, key, _ := axisFields(t, nibsPath, "tnib-e002")
		if ms != "" || key != "" {
			t.Errorf("after clear: milestone=%q milestone_order=%q, want both empty", ms, key)
		}
		if got := queueIDs(t, nibsPath, "tnib-m001"); strings.Contains(strings.Join(got, " "), "tnib-e002") {
			t.Errorf("queue still lists the cleared nib: %v", got)
		}
	})

	t.Run("set and clear together is refused", func(t *testing.T) {
		nibsPath := setupQueueCLITest(t)
		_, err := runRootWith(t, "--nibs-path", nibsPath, "set", "tnib-e002", "--milestone", "tnib-m002", "--clear", "milestone")
		if err == nil || !strings.Contains(err.Error(), "cannot both set and --clear milestone") {
			t.Fatalf("error = %v, want the set-and-clear refusal", err)
		}
	})

	t.Run("--clear names milestone as a clearable field", func(t *testing.T) {
		nibsPath := setupQueueCLITest(t)
		_, err := runRootWith(t, "--nibs-path", nibsPath, "set", "tnib-e002", "--clear", "nonsense")
		if err == nil || !strings.Contains(err.Error(), "milestone") {
			t.Fatalf("error = %v, want the clearable menu to include milestone", err)
		}
	})
}

// TestSetMilestoneInversionLint pins decision 2.3 at the assignment: an
// assignment appends the subject LAST, so the inversion it can create is a
// queue member ahead of it that it still blocks. The write succeeds (exit 0),
// one stderr warning names the pair in text mode, and --json carries it as a
// "warnings" array beside the {nib} contract. A blocker-free assignment warns
// nothing.
func TestSetMilestoneInversionLint(t *testing.T) {
	files := map[string]string{
		"qm1--waypoint.md": "---\nversion: 2\ntitle: Waypoint\nstatus: todo\ntype: milestone\n---\n",
		"qx--ahead.md":     "---\nversion: 2\ntitle: Ahead\nstatus: todo\ntype: task\nblocked_by:\n    - qs\nmilestone: qm1\nmilestone_order: a0\n---\n",
		"qs--subject.md":   "---\nversion: 2\ntitle: Subject\nstatus: todo\ntype: task\n---\n",
		"qt--plain.md":     "---\nversion: 2\ntitle: Plain\nstatus: todo\ntype: task\n---\n",
	}

	t.Run("text: warning on stderr, write lands", func(t *testing.T) {
		nibsPath := writeStoreFiles(t, files)
		t.Cleanup(resetQueueCLIFlags)
		resetQueueCLIFlags()
		var stderr strings.Builder
		rootCmd.SetErr(&stderr)
		t.Cleanup(func() { rootCmd.SetErr(nil) })

		_, err := runRootWith(t, "--nibs-path", nibsPath, "set", "qs", "--milestone", "qm1")
		if err != nil {
			t.Fatalf("set --milestone: %v", err)
		}
		for _, want := range []string{"warning:", "milestone qm1", "qx is ahead of qs, which still blocks it"} {
			if !strings.Contains(stderr.String(), want) {
				t.Errorf("stderr = %q, want substring %q", stderr.String(), want)
			}
		}
		if strings.Count(stderr.String(), "warning:") != 1 {
			t.Errorf("want exactly one warning line, got:\n%s", stderr.String())
		}
		if ms, _, _ := axisFields(t, nibsPath, "qs"); ms != "qm1" {
			t.Errorf("milestone = %q, want qm1 — the lint must not block the write", ms)
		}
	})

	t.Run("json: warnings array beside the nib", func(t *testing.T) {
		nibsPath := writeStoreFiles(t, files)
		t.Cleanup(resetQueueCLIFlags)
		resetQueueCLIFlags()
		out, err := runRootWith(t, "--nibs-path", nibsPath, "set", "qs", "--milestone", "qm1", "--json")
		if err != nil {
			t.Fatalf("set --milestone --json: %v", err)
		}
		var env struct {
			Nib      map[string]any `json:"nib"`
			Warnings []string       `json:"warnings"`
		}
		if err := json.Unmarshal([]byte(out), &env); err != nil {
			t.Fatalf("unmarshal: %v\nraw: %s", err, out)
		}
		if env.Nib["id"] != "qs" {
			t.Errorf("nib.id = %v, want qs", env.Nib["id"])
		}
		if len(env.Warnings) != 1 || !strings.Contains(env.Warnings[0], "qx is ahead of qs") {
			t.Errorf("warnings = %v, want one naming the pair", env.Warnings)
		}
	})

	t.Run("no inversion, no warning", func(t *testing.T) {
		nibsPath := writeStoreFiles(t, files)
		t.Cleanup(resetQueueCLIFlags)
		resetQueueCLIFlags()
		var stderr strings.Builder
		rootCmd.SetErr(&stderr)
		t.Cleanup(func() { rootCmd.SetErr(nil) })
		if _, err := runRootWith(t, "--nibs-path", nibsPath, "set", "qt", "--milestone", "qm1"); err != nil {
			t.Fatalf("set --milestone: %v", err)
		}
		if strings.Contains(stderr.String(), "warning:") {
			t.Errorf("unexpected warning:\n%s", stderr.String())
		}
	})

	t.Run("a pre-existing pair is not reported again", func(t *testing.T) {
		nibsPath := writeStoreFiles(t, files)
		t.Cleanup(resetQueueCLIFlags)
		resetQueueCLIFlags()
		if _, err := runRootWith(t, "--nibs-path", nibsPath, "set", "qs", "--milestone", "qm1"); err != nil {
			t.Fatalf("set --milestone: %v", err)
		}
		// The pair (qx, qs) was created — and reported — above; reassigning
		// qs to the same queue changes nothing and must stay silent.
		resetQueueCLIFlags()
		var stderr strings.Builder
		rootCmd.SetErr(&stderr)
		t.Cleanup(func() { rootCmd.SetErr(nil) })
		if _, err := runRootWith(t, "--nibs-path", nibsPath, "set", "qs", "--milestone", "qm1"); err != nil {
			t.Fatalf("set --milestone (same queue): %v", err)
		}
		if strings.Contains(stderr.String(), "warning:") {
			t.Errorf("a reassignment to the same queue re-reported a pair it did not create:\n%s", stderr.String())
		}
	})
}

// TestSetDependencyInversionLint pins decision 2.3 at the other creating
// write: a new dependency edge between two members of one queue, with the
// blocker later in the queue, creates an inversion whichever end spells it
// (`--blocked-by` on the blocked nib, `--blocking` on the blocker), and is
// reported once, at that write. An edge whose blocker is already ahead
// creates none and warns nothing. On the sample fixture tnib-m001's queue is
// e001 e002 e003 e004 and tnib-m002's is e005 e006.
func TestSetDependencyInversionLint(t *testing.T) {
	run := func(t *testing.T, args ...string) string {
		t.Helper()
		nibsPath := setupQueueCLITest(t)
		var stderr strings.Builder
		rootCmd.SetErr(&stderr)
		t.Cleanup(func() { rootCmd.SetErr(nil) })
		if _, err := runRootWith(t, append([]string{"--nibs-path", nibsPath}, args...)...); err != nil {
			t.Fatalf("set %v: %v", args, err)
		}
		return stderr.String()
	}

	t.Run("--blocked-by naming a member behind the subject warns once", func(t *testing.T) {
		stderr := run(t, "set", "tnib-e001", "--blocked-by", "tnib-e004")
		for _, want := range []string{"warning:", "milestone tnib-m001", "tnib-e001 is ahead of tnib-e004, which still blocks it"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("stderr = %q, want substring %q", stderr, want)
			}
		}
		if strings.Count(stderr, "warning:") != 1 {
			t.Errorf("want exactly one warning line, got:\n%s", stderr)
		}
	})

	t.Run("--blocking naming a member ahead of the subject warns the same pair", func(t *testing.T) {
		stderr := run(t, "set", "tnib-e004", "--blocking", "tnib-e001")
		if !strings.Contains(stderr, "tnib-e001 is ahead of tnib-e004, which still blocks it") {
			t.Errorf("stderr = %q, want the pair named from the blocker's side", stderr)
		}
		if strings.Count(stderr, "warning:") != 1 {
			t.Errorf("want exactly one warning line, got:\n%s", stderr)
		}
	})

	t.Run("an edge whose blocker is already ahead warns nothing", func(t *testing.T) {
		if stderr := run(t, "set", "tnib-e006", "--blocked-by", "tnib-e005"); strings.Contains(stderr, "warning:") {
			t.Errorf("unexpected warning:\n%s", stderr)
		}
	})
}
