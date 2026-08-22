package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/mdsection"
	"github.com/alphaleonis/nibs/internal/output"
)

// closeOwnedFlags is the surface resetCloseFlags answers for: the flags
// registered on closeCmd itself. It is NOT closeCmd.Flags() — after the first
// rootCmd.Execute() Cobra merges the root's persistent flags (--nibs-path,
// --config) into that set as the very same *pflag.Flag pointers, and those
// belong to resetRootPersistentFlags. Clearing them from a helper named for
// `close` would reach into global state it does not set, and asserting on them
// would report a persistent-flag leak from any earlier test in this package as
// a `close` bug.
func closeOwnedFlags() *pflag.FlagSet {
	return closeCmd.LocalNonPersistentFlags()
}

func resetCloseFlags() {
	closeSummary = ""
	// --as is the one close flag whose zero value is not its default: Cobra
	// leaves the bound variable at whatever the last invocation parsed, so
	// clearing it to "" would make the next bare `close` fail its own --as
	// validation rather than produce the default close reason.
	closeAs = closeDefaultStatus()
	closeForce = false
	closeIfMatch = ""
	closeJSON = false
	closeMoveOpenTo = ""
	closeUnassignOpen = false
	// Cobra registers --help on the command itself and binds no variable of
	// ours to it, so it is restored through its Value. A leaked one makes the
	// next `close` print help instead of running.
	if help := closeOwnedFlags().Lookup("help"); help != nil {
		_ = help.Value.Set(help.DefValue)
	}
	// The queue gate asks Cobra whether --move-open-to was GIVEN rather than
	// reading the bound string, so "given as empty" cannot pass as "omitted".
	// That makes the Changed bit state too, and it leaks between subtests
	// exactly as a bound value does. VisitAll, not Visit: the derived set has
	// no `actual` map of its own, and the flags it carries are the command's
	// real pointers, so clearing Changed here clears it on the command.
	closeOwnedFlags().VisitAll(func(f *pflag.Flag) {
		f.Changed = false
	})
}

// TestResetCloseFlagsClearsAllState dirties every flag `close` owns — through
// the FlagSet the real parser writes to — and verifies each is back at its
// documented default afterwards. The dirty set is DERIVED from the command
// rather than listed here, which is what makes the guard self-maintaining: a
// flag added to close and left out of resetCloseFlags fails this test instead
// of leaking state into another subtest.
func TestResetCloseFlagsClearsAllState(t *testing.T) {
	t.Cleanup(resetCloseFlags)

	// Cobra adds --help LAZILY, from Command.execute() and from the generated
	// help subcommand — never from LocalNonPersistentFlags(). So the derived set
	// carries it only if something executed `close` earlier in the process, and
	// under `-run '^TestResetCloseFlagsClearsAllState$'` nothing has: the one
	// flag resetCloseFlags special-cases would be the one flag this guard never
	// exercised. Forcing the registration (exported, idempotent) makes the set
	// the same whatever the run order or -run filter.
	closeCmd.InitDefaultHelpFlag()

	var names []string
	closeOwnedFlags().VisitAll(func(f *pflag.Flag) { names = append(names, f.Name) })
	if len(names) == 0 {
		t.Fatal("close owns no flags, so this guard asserts nothing")
	}
	if !slices.Contains(names, "help") {
		t.Fatal("--help is not in the derived set, so resetCloseFlags's restoration of it is unguarded here")
	}
	for _, name := range names {
		f := closeCmd.Flags().Lookup(name)
		if err := closeCmd.Flags().Set(name, dirtyFlagValue(t, f)); err != nil {
			t.Fatalf("pre-populate --%s: %v", name, err)
		}
	}

	resetCloseFlags()

	closeOwnedFlags().VisitAll(func(f *pflag.Flag) {
		if f.Value.String() != f.DefValue {
			t.Errorf("flag %q = %q after reset, want default %q",
				f.Name, f.Value.String(), f.DefValue)
		}
		if f.Changed {
			t.Errorf("flag %q Changed = true after reset, want false", f.Name)
		}
	})
}

// dirtyFlagValue returns a value that differs from f's default, so the guard
// above can dirty a flag it knows nothing about beyond its type. A type it
// cannot dirty fails loudly: silently dirtying nothing would let the very flag
// the guard exists to catch pass at its default.
func dirtyFlagValue(t *testing.T, f *pflag.Flag) string {
	t.Helper()
	switch f.Value.Type() {
	case "bool":
		if f.DefValue == "true" {
			return "false"
		}
		return "true"
	case "string":
		if f.DefValue == "leaked" {
			return "leaked-twice"
		}
		return "leaked"
	default:
		t.Fatalf("flag --%s has type %q: teach dirtyFlagValue how to dirty it", f.Name, f.Value.Type())
		return ""
	}
}

// TestResetCloseFlagsLeavesRootPersistentFlagsAlone: after the first Execute,
// --nibs-path and --config are in closeCmd.Flags() as the root's own flag
// pointers. resetRootPersistentFlags owns them; a close-scoped reset that
// cleared their Changed bit would silently take over half of another helper's
// job and hide the leak that helper exists to catch.
func TestResetCloseFlagsLeavesRootPersistentFlagsAlone(t *testing.T) {
	t.Cleanup(resetCloseFlags)
	t.Cleanup(resetRootPersistentFlags)

	// Force Cobra's merge: before the first Execute the root's persistent flags
	// are not in closeCmd.Flags() at all, so there would be nothing to protect.
	nibsDir := setupCloseTest(t, map[string]string{
		"abc-1--my-task.md": "---\nversion: 2\ntitle: My Task\nstatus: in-progress\ntype: task\n---\n\nBody.\n",
	})
	withStdin(t, "Done.\n")
	if _, err := runRootWith(t, "--nibs-path", nibsDir, "close", "abc-1", "--summary", "-"); err != nil {
		t.Fatalf("close: %v", err)
	}

	root := rootCmd.PersistentFlags().Lookup("nibs-path")
	if root == nil {
		t.Fatal("rootCmd has no --nibs-path")
	}
	if closeCmd.Flags().Lookup("nibs-path") != root {
		t.Fatal("Cobra did not merge --nibs-path into closeCmd.Flags(); this guard would prove nothing")
	}

	if err := rootCmd.PersistentFlags().Set("nibs-path", "/tmp/leaked"); err != nil {
		t.Fatalf("pre-populate --nibs-path: %v", err)
	}
	resetCloseFlags()

	if !root.Changed || root.Value.String() != "/tmp/leaked" {
		t.Errorf("resetCloseFlags touched --nibs-path: Changed = %v, value = %q",
			root.Changed, root.Value.String())
	}
}

// withCloseNow pins the clock `close` stamps its ## Summary entries with, and
// returns a setter so one test can advance the date between two closes — which
// is what makes "both entries survived" readable as two distinct records rather
// than one line repeated. The original clock is restored when the test ends.
func withCloseNow(t *testing.T, at time.Time) func(time.Time) {
	t.Helper()
	original := closeNow
	t.Cleanup(func() { closeNow = original })
	now := at
	closeNow = func() time.Time { return now }
	return func(next time.Time) { now = next }
}

func setupCloseTest(t *testing.T, files map[string]string) string {
	t.Helper()
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(func() { resetCloseFlags() })
	resetCloseFlags()

	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(storeDataDir(nibsDir), 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(dataPath(nibsDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return nibsDir
}

// readNibFile reads an ACTIVE nib file out of the store — i.e. from data/,
// where nib files live.
func readNibFile(t *testing.T, nibsDir, filename string) string {
	t.Helper()
	data, err := os.ReadFile(dataPath(nibsDir, filename))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCloseBasic(t *testing.T) {
	nibsDir := setupCloseTest(t, map[string]string{
		"abc-1--my-task.md": "---\nversion: 2\ntitle: My Task\nstatus: in-progress\ntype: task\n---\n\nSome body content.\n",
	})

	withStdin(t, "All done\n")
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "abc-1", "--summary", "-",
	})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("expected close to succeed, got: %v", err)
	}

	content := readNibFile(t, nibsDir, "abc-1--my-task.md")
	if !strings.Contains(content, "status: completed") {
		t.Errorf("expected status to be completed, got:\n%s", content)
	}
}

func TestCloseSummaryAppended(t *testing.T) {
	nibsDir := setupCloseTest(t, map[string]string{
		"sum-1--my-task.md": "---\nversion: 2\ntitle: My Task\nstatus: in-progress\ntype: task\n---\n\nExisting body.\n",
	})

	withStdin(t, "Implemented the feature and added tests.\n")
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "sum-1", "--summary", "-",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	content := readNibFile(t, nibsDir, "sum-1--my-task.md")
	if !strings.Contains(content, "## Summary") {
		t.Errorf("expected ## Summary heading in body, got:\n%s", content)
	}
	if !strings.Contains(content, "Implemented the feature and added tests.") {
		t.Errorf("expected summary text in body, got:\n%s", content)
	}
	// Original body should still be there
	if !strings.Contains(content, "Existing body.") {
		t.Errorf("expected original body to be preserved, got:\n%s", content)
	}
}

func TestCloseFailsWithIncompleteChildren(t *testing.T) {
	nibsDir := setupCloseTest(t, map[string]string{
		"par-1--parent.md":    "---\nversion: 2\ntitle: Parent Epic\nstatus: in-progress\ntype: epic\n---\n\nEpic body.\n",
		"ch-1--child-done.md": "---\nversion: 2\ntitle: Child Done\nstatus: completed\ntype: task\nparent: par-1\n---\n\nDone.\n",
		"ch-2--child-wip.md":  "---\nversion: 2\ntitle: Child WIP\nstatus: in-progress\ntype: task\nparent: par-1\n---\n\nStill working.\n",
	})

	withStdin(t, "Done\n")
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "par-1", "--summary", "-",
	})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when children are incomplete, got nil")
	}
	if !strings.Contains(err.Error(), "incomplete") {
		t.Errorf("error should mention incomplete children, got: %s", err)
	}
	if !strings.Contains(err.Error(), "ch-2") {
		t.Errorf("error should mention the incomplete child ID, got: %s", err)
	}
}

func TestCloseForceWithIncompleteChildren(t *testing.T) {
	nibsDir := setupCloseTest(t, map[string]string{
		"frc-1--parent.md": "---\nversion: 2\ntitle: Parent\nstatus: in-progress\ntype: epic\n---\n\nBody.\n",
		"frc-2--child.md":  "---\nversion: 2\ntitle: Child\nstatus: todo\ntype: task\nparent: frc-1\n---\n\nTodo.\n",
	})

	withStdin(t, "Forced close\n")
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "frc-1", "--summary", "-", "--force",
	})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("expected --force to bypass children check, got: %v", err)
	}

	content := readNibFile(t, nibsDir, "frc-1--parent.md")
	if !strings.Contains(content, "status: completed") {
		t.Errorf("expected status completed, got:\n%s", content)
	}
}

func TestCloseUpdatesParentCurrentFocus(t *testing.T) {
	nibsDir := setupCloseTest(t, map[string]string{
		"ms-1--milestone.md": "---\nversion: 2\ntitle: Milestone\nstatus: in-progress\ntype: milestone\ncreated_at: 2026-01-02T03:04:05Z\nupdated_at: 2026-01-02T03:04:05Z\n---\n\n## Current Focus\n\nWorking on phase 1.\n",
		"ph-1--phase.md":     "---\nversion: 2\ntitle: Phase 1\nstatus: in-progress\ntype: epic\nparent: ms-1\n---\n\nPhase 1 body.\n",
	})

	withStdin(t, "Phase 1 completed successfully.\n")
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "ph-1", "--summary", "-",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	milestone := readNibFile(t, nibsDir, "ms-1--milestone.md")
	if !strings.Contains(milestone, "Phase 1 completed") {
		t.Errorf("expected parent Current Focus to be updated with summary, got:\n%s", milestone)
	}
}

func TestCloseUpdatesParentKeyDecisions(t *testing.T) {
	nibsDir := setupCloseTest(t, map[string]string{
		"ms-2--milestone.md": "---\nversion: 2\ntitle: Milestone\nstatus: in-progress\ntype: milestone\ncreated_at: 2026-01-02T03:04:05Z\nupdated_at: 2026-01-02T03:04:05Z\n---\n\n## Key Decisions\n\n- Previous decision\n",
		"ph-2--phase.md":     "---\nversion: 2\ntitle: Phase 2\nstatus: in-progress\ntype: epic\nparent: ms-2\n---\n\n## Key Decisions\n\n- Used GraphQL instead of REST\n- Chose table-driven tests\n",
	})

	withStdin(t, "Phase 2 done.\n")
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "ph-2", "--summary", "-",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	milestone := readNibFile(t, nibsDir, "ms-2--milestone.md")
	if !strings.Contains(milestone, "Used GraphQL instead of REST") {
		t.Errorf("expected parent Key Decisions to include child's decisions, got:\n%s", milestone)
	}
	// Original decisions should be preserved
	if !strings.Contains(milestone, "Previous decision") {
		t.Errorf("expected parent's original Key Decisions to be preserved, got:\n%s", milestone)
	}
}

func TestCloseNoParent(t *testing.T) {
	nibsDir := setupCloseTest(t, map[string]string{
		"nop-1--solo.md": "---\nversion: 2\ntitle: Solo Task\nstatus: in-progress\ntype: task\n---\n\nJust a task.\n",
	})

	withStdin(t, "Done without parent\n")
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "nop-1", "--summary", "-",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("close should work without parent, got: %v", err)
	}

	content := readNibFile(t, nibsDir, "nop-1--solo.md")
	if !strings.Contains(content, "status: completed") {
		t.Errorf("expected completed status, got:\n%s", content)
	}
}

func TestCloseParentMissingSections(t *testing.T) {
	nibsDir := setupCloseTest(t, map[string]string{
		"ms-3--milestone.md": "---\nversion: 2\ntitle: Milestone\nstatus: in-progress\ntype: milestone\ncreated_at: 2026-01-02T03:04:05Z\nupdated_at: 2026-01-02T03:04:05Z\n---\n\nJust a goal.\n",
		"ph-3--phase.md":     "---\nversion: 2\ntitle: Phase 3\nstatus: in-progress\ntype: epic\nparent: ms-3\n---\n\n## Key Decisions\n\n- Chose mdsection for parsing\n",
	})

	withStdin(t, "Phase 3 completed.\n")
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "ph-3", "--summary", "-",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	milestone := readNibFile(t, nibsDir, "ms-3--milestone.md")
	if !strings.Contains(milestone, "## Current Focus") {
		t.Errorf("expected Current Focus section to be appended, got:\n%s", milestone)
	}
	if !strings.Contains(milestone, "## Key Decisions") {
		t.Errorf("expected Key Decisions section to be appended, got:\n%s", milestone)
	}
	if !strings.Contains(milestone, "Chose mdsection for parsing") {
		t.Errorf("expected child's key decisions to appear in parent, got:\n%s", milestone)
	}
}

// TestCloseOnAnAlreadyClosedNibChangesTheReason pins the guard drop: a nib that
// is already closed can be closed again, and the second close writes the reason
// asked for. It is only safe because the ## Summary write accrues — with a
// replacing write this command would silently destroy the first rationale — so
// the entries kept afterwards are asserted here too, not left to a sibling test.
func TestCloseOnAnAlreadyClosedNibChangesTheReason(t *testing.T) {
	nibsDir := setupCloseTest(t, map[string]string{
		"done-1--finished.md": "---\nversion: 2\ntitle: Finished\nstatus: completed\ntype: task\n---\n\n## Summary\n\n**Completed 2026-07-20** — shipped it.\n",
	})
	withCloseNow(t, time.Date(2026, 7, 27, 9, 0, 0, 0, time.UTC))

	withStdin(t, "Turned out to be wrong.\n")
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "done-1", "--as", "scrapped", "--summary", "-",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("closing an already-closed nib should succeed, got: %v", err)
	}

	content := readNibFile(t, nibsDir, "done-1--finished.md")
	if !strings.Contains(content, "status: scrapped") {
		t.Errorf("expected the second close to write status scrapped, got:\n%s", content)
	}
	if !strings.Contains(content, "**Completed 2026-07-20** — shipped it.") {
		t.Errorf("the second close destroyed the first rationale, got:\n%s", content)
	}
	if !strings.Contains(content, "**Scrapped 2026-07-27** — Turned out to be wrong.") {
		t.Errorf("expected the new entry in ## Summary, got:\n%s", content)
	}
}

func TestCloseJSONOutput(t *testing.T) {
	nibsDir := setupCloseTest(t, map[string]string{
		"json-1--task.md": "---\nversion: 2\ntitle: JSON Task\nstatus: in-progress\ntype: task\n---\n\nBody.\n",
	})

	withStdin(t, "Done\n")
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "json-1", "--summary", "-", "--json",
	})

	// JSON output goes via output.Success which writes to stdout.
	// If no error, that's sufficient — the output.Success path was hit.
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("close --json failed: %v", err)
	}

	content := readNibFile(t, nibsDir, "json-1--task.md")
	if !strings.Contains(content, "status: completed") {
		t.Errorf("expected completed status, got:\n%s", content)
	}
}

func TestCloseSummaryRequired(t *testing.T) {
	nibsDir := setupCloseTest(t, map[string]string{
		"req-1--task.md": "---\nversion: 2\ntitle: Task\nstatus: todo\ntype: task\n---\n\nBody.\n",
	})

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "req-1",
	})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when --summary is missing")
	}
	if !strings.Contains(err.Error(), "--summary") {
		t.Errorf("error should mention --summary, got: %s", err)
	}
}

func TestCloseSucceedsWithAllChildrenCompleted(t *testing.T) {
	nibsDir := setupCloseTest(t, map[string]string{
		"par-2--parent.md": "---\nversion: 2\ntitle: Parent Epic\nstatus: in-progress\ntype: epic\n---\n\nEpic body.\n",
		"ch-3--child-a.md": "---\nversion: 2\ntitle: Child A\nstatus: completed\ntype: task\nparent: par-2\n---\n\nDone.\n",
		"ch-4--child-b.md": "---\nversion: 2\ntitle: Child B\nstatus: scrapped\ntype: task\nparent: par-2\n---\n\nScrapped.\n",
	})

	withStdin(t, "All children resolved\n")
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "par-2", "--summary", "-",
	})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("expected close to succeed when all children are resolved, got: %v", err)
	}

	content := readNibFile(t, nibsDir, "par-2--parent.md")
	if !strings.Contains(content, "status: completed") {
		t.Errorf("expected status completed, got:\n%s", content)
	}
	if !strings.Contains(content, "## Summary") {
		t.Errorf("expected summary section appended, got:\n%s", content)
	}
}

// TestCloseAsSetsTheNamedClosedStatus asserts --as writes the status it names,
// for every status config declares closed. The cases are derived from
// ClosedStatusNames, so a status added to the vocabulary is covered here without
// an edit; the membership check keeps the loop from silently going empty.
func TestCloseAsSetsTheNamedClosedStatus(t *testing.T) {
	closed := config.Default().ClosedStatusNames()
	for _, want := range []string{"scrapped", "deferred"} {
		if !slices.Contains(closed, want) {
			t.Fatalf("test setup: %q is not among the closed statuses %v, so this test no longer covers it", want, closed)
		}
	}

	for _, status := range closed {
		t.Run(status, func(t *testing.T) {
			nibsDir := setupCloseTest(t, map[string]string{
				"as-1--my-task.md": "---\nversion: 2\ntitle: My Task\nstatus: in-progress\ntype: task\n---\n\nBody.\n",
			})

			withStdin(t, "Closing as "+status+".\n")
			rootCmd.SetArgs([]string{
				"--nibs-path", nibsDir,
				"close", "as-1", "--as", status, "--summary", "-",
			})

			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("close --as %s failed: %v", status, err)
			}

			content := readNibFile(t, nibsDir, "as-1--my-task.md")
			if !strings.Contains(content, "status: "+status) {
				t.Errorf("expected status %s, got:\n%s", status, content)
			}
			// The summary is the record the close reason exists to carry, so a
			// reason written without one would defeat the point of the flag.
			if !strings.Contains(content, "Closing as "+status+".") {
				t.Errorf("expected the summary in the body, got:\n%s", content)
			}
		})
	}
}

// TestCloseRejectsAnOpenStatusAs asserts --as refuses every open status, naming
// the closed ones it would have accepted. Without this, `--as todo` would write
// an open status through the closing ritual — the exact move the `set` refusal
// exists to prevent, arriving by the other door.
func TestCloseRejectsAnOpenStatusAs(t *testing.T) {
	cfg := config.Default()
	open := cfg.OpenStatusNames()
	if len(open) == 0 {
		t.Fatal("test setup: no open statuses declared, so this test asserts nothing")
	}

	for _, status := range open {
		t.Run(status, func(t *testing.T) {
			nibsDir := setupCloseTest(t, map[string]string{
				"bad-1--my-task.md": "---\nversion: 2\ntitle: My Task\nstatus: in-progress\ntype: task\n---\n\nBody.\n",
			})

			// Supply the summary so --as is the only thing wrong with this
			// invocation. Without it the command fails on the missing summary
			// whatever --as holds, and the test would pass with no --as check at
			// all.
			withStdin(t, "Should never be written.\n")
			rootCmd.SetArgs([]string{
				"--nibs-path", nibsDir,
				"close", "bad-1", "--as", status, "--summary", "-",
			})

			err := rootCmd.Execute()
			if err == nil {
				t.Fatalf("close --as %s should be rejected, got nil", status)
			}
			var ce *output.CodedError
			if !errors.As(err, &ce) {
				t.Fatalf("error = %T, want *output.CodedError", err)
			}
			if output.ExitCode(ce.Code) != output.ExitValidation {
				t.Errorf("close --as %s exit = %d, want %d (validation)", status, output.ExitCode(ce.Code), output.ExitValidation)
			}
			// The message has to name the choices, or an agent's only recovery is
			// to guess a second time.
			for _, name := range cfg.ClosedStatusNames() {
				if !strings.Contains(err.Error(), name) {
					t.Errorf("close --as %s error should name the valid choice %q, got: %s", status, name, err)
				}
			}

			content := readNibFile(t, nibsDir, "bad-1--my-task.md")
			if !strings.Contains(content, "status: in-progress") {
				t.Errorf("rejected close --as %s still wrote the file:\n%s", status, content)
			}
		})
	}
}

// TestCloseAsFollowsTheClosedFlag proves --as reads StatusConfig.Closed rather
// than a list of names kept in close.go: a status declared closed for the
// duration of this test is accepted with no edit to the command. The paired
// half — an open status being rejected — is TestCloseRejectsAnOpenStatusAs,
// which together with this rules out "accepts anything in the vocabulary".
func TestCloseAsFollowsTheClosedFlag(t *testing.T) {
	withExtraStatus(t, config.StatusConfig{
		Name:        "abandoned",
		Color:       "gray",
		Role:        config.RoleParked,
		Description: "Guard status: closed, declared only for this test",
	})

	nibsDir := setupCloseTest(t, map[string]string{
		"drv-1--my-task.md": "---\nversion: 2\ntitle: My Task\nstatus: in-progress\ntype: task\n---\n\nBody.\n",
	})

	withStdin(t, "Walked away from it.\n")
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "drv-1", "--as", "abandoned", "--summary", "-",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("close --as abandoned should succeed once abandoned is declared closed, got: %v", err)
	}

	content := readNibFile(t, nibsDir, "drv-1--my-task.md")
	if !strings.Contains(content, "status: abandoned") {
		t.Errorf("expected status abandoned, got:\n%s", content)
	}
}

// TestCloseSummaryEntryIsStampedWithReasonAndDate covers the first close on a
// nib that has no ## Summary at all: the section is created and the summary
// arrives as a dated, reason-stamped entry rather than as bare prose. Every
// accrual guard below builds on this shape, so it is pinned here against a
// literal — assembling the expectation with closeSummaryEntry would let the two
// drift together and agree about the wrong thing.
func TestCloseSummaryEntryIsStampedWithReasonAndDate(t *testing.T) {
	nibsDir := setupCloseTest(t, map[string]string{
		"stamp-1--task.md": "---\nversion: 2\ntitle: Task\nstatus: in-progress\ntype: task\n---\n\nExisting body.\n",
	})
	withCloseNow(t, time.Date(2026, 7, 27, 14, 30, 0, 0, time.UTC))

	withStdin(t, "waiting on the upstream provider release\n")
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "stamp-1", "--as", "deferred", "--summary", "-",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	content := readNibFile(t, nibsDir, "stamp-1--task.md")
	if !strings.Contains(content, "## Summary") {
		t.Errorf("expected a ## Summary section to be created, got:\n%s", content)
	}
	const want = "**Deferred 2026-07-27** — waiting on the upstream provider release"
	if !strings.Contains(content, want) {
		t.Errorf("expected the entry %q, got:\n%s", want, content)
	}
	if !strings.Contains(content, "Existing body.") {
		t.Errorf("expected the original body to be preserved, got:\n%s", content)
	}
}

// TestCloseSummaryAccruesAcrossReasons is the guard the accrual exists for: a
// nib closed under one reason and then re-closed under another keeps BOTH
// records. A replacing write passes every other close test in this file and
// fails only here, because only here is there an earlier rationale to destroy.
func TestCloseSummaryAccruesAcrossReasons(t *testing.T) {
	nibsDir := setupCloseTest(t, map[string]string{
		"acc-1--task.md": "---\nversion: 2\ntitle: Task\nstatus: in-progress\ntype: task\n---\n\nBody.\n",
	})
	setNow := withCloseNow(t, time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC))

	withStdin(t, "waiting on the upstream provider release\n")
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "acc-1", "--as", "deferred", "--summary", "-",
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("first close failed: %v", err)
	}

	setNow(time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC))
	resetCloseFlags()
	withStdin(t, "superseded by nibs-abcd\n")
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "acc-1", "--as", "scrapped", "--summary", "-",
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("second close failed: %v", err)
	}

	content := readNibFile(t, nibsDir, "acc-1--task.md")
	const (
		first  = "**Deferred 2026-07-27** — waiting on the upstream provider release"
		second = "**Scrapped 2026-08-02** — superseded by nibs-abcd"
	)
	firstAt, secondAt := strings.Index(content, first), strings.Index(content, second)
	if firstAt < 0 {
		t.Errorf("the second close destroyed the first entry %q, got:\n%s", first, content)
	}
	if secondAt < 0 {
		t.Errorf("expected the second entry %q, got:\n%s", second, content)
	}
	// Newest last: the section reads as a history, so an entry appended at the
	// top would put the reasons in the reverse of the order they happened.
	if firstAt >= 0 && secondAt >= 0 && firstAt > secondAt {
		t.Errorf("entries are out of order — the later close should come after the earlier one, got:\n%s", content)
	}
	// One ## Summary heading, not a second one appended beside the first.
	if n := strings.Count(content, "## Summary"); n != 1 {
		t.Errorf("expected exactly 1 ## Summary heading, got %d:\n%s", n, content)
	}
}

// TestCloseSummaryAccruesUnderTheSameReason pins re-closing under the reason the
// nib already carries: that is a legitimate "the rationale changed" action, not
// a no-op to be swallowed, so it appends a second entry like any other close.
func TestCloseSummaryAccruesUnderTheSameReason(t *testing.T) {
	nibsDir := setupCloseTest(t, map[string]string{
		"same-1--task.md": "---\nversion: 2\ntitle: Task\nstatus: in-progress\ntype: task\n---\n\nBody.\n",
	})
	setNow := withCloseNow(t, time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC))

	withStdin(t, "blocked on the vendor\n")
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "same-1", "--as", "deferred", "--summary", "-",
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("first close failed: %v", err)
	}

	setNow(time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC))
	resetCloseFlags()
	withStdin(t, "vendor replied, revisit next quarter\n")
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "same-1", "--as", "deferred", "--summary", "-",
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("re-closing under the same reason should be allowed, got: %v", err)
	}

	content := readNibFile(t, nibsDir, "same-1--task.md")
	for _, want := range []string{
		"**Deferred 2026-07-27** — blocked on the vendor",
		"**Deferred 2026-08-02** — vendor replied, revisit next quarter",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("expected entry %q, got:\n%s", want, content)
		}
	}
}

// TestCloseReasonStampFollowsTheStatusVocabulary proves the stamp is derived
// from the status the close writes rather than from a second list of reason
// words kept in close.go: a status declared closed for the duration of this test
// stamps its own name, with no edit to the command. TestCloseAsFollowsTheClosedFlag
// proves the same status is accepted; this one proves it is also spelled right
// in the record.
func TestCloseReasonStampFollowsTheStatusVocabulary(t *testing.T) {
	withExtraStatus(t, config.StatusConfig{
		Name:        "abandoned",
		Color:       "gray",
		Role:        config.RoleParked,
		Description: "Guard status: closed, declared only for this test",
	})

	nibsDir := setupCloseTest(t, map[string]string{
		"stmp-1--task.md": "---\nversion: 2\ntitle: Task\nstatus: in-progress\ntype: task\n---\n\nBody.\n",
	})
	withCloseNow(t, time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC))

	withStdin(t, "walked away from it\n")
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "stmp-1", "--as", "abandoned", "--summary", "-",
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("close --as abandoned failed: %v", err)
	}

	content := readNibFile(t, nibsDir, "stmp-1--task.md")
	const want = "**Abandoned 2026-08-02** — walked away from it"
	if !strings.Contains(content, want) {
		t.Errorf("expected the stamp to be the status itself, capitalized (%q), got:\n%s", want, content)
	}
}

// TestCloseParentPropagationDependsOnTheReason pins which half of the parent
// write is reason-dependent. Key Decisions merge upward for EVERY close reason —
// why work was set aside is exactly what a later reader looks for in the parent
// — while Current Focus, which answers "what is the latest progress here", is
// rewritten only by a completion. Rewriting it for a nib that was deferred or
// scrapped would erase the last real progress and make the parent read as though
// nothing were happening.
//
// The cases come from ClosedStatusNames, so a newly declared close reason is
// covered here without an edit; the membership check keeps the loop from
// silently going empty, and the completion case keeps the guard from passing on
// a build that simply never writes Current Focus at all.
func TestCloseParentPropagationDependsOnTheReason(t *testing.T) {
	closed := config.Default().ClosedStatusNames()
	for _, want := range []string{closeCompletionStatus(), "deferred", "scrapped"} {
		if !slices.Contains(closed, want) {
			t.Fatalf("test setup: %q is not among the closed statuses %v, so this test no longer covers it", want, closed)
		}
	}

	const originalFocus = "Working on phase 1."
	for _, status := range closed {
		t.Run(status, func(t *testing.T) {
			nibsDir := setupCloseTest(t, map[string]string{
				"pp-ms--milestone.md": "---\nversion: 2\ntitle: Milestone\nstatus: in-progress\ntype: milestone\ncreated_at: 2026-01-02T03:04:05Z\nupdated_at: 2026-01-02T03:04:05Z\n---\n\n## Current Focus\n\n" + originalFocus + "\n\n## Key Decisions\n\n- Previous decision\n",
				"pp-ch--child.md":     "---\nversion: 2\ntitle: Child\nstatus: in-progress\ntype: epic\nparent: pp-ms\n---\n\n## Key Decisions\n\n- Chose mdsection for parsing\n",
			})

			withStdin(t, "the child is off the board\n")
			rootCmd.SetArgs([]string{
				"--nibs-path", nibsDir,
				"close", "pp-ch", "--as", status, "--summary", "-",
			})
			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("close --as %s failed: %v", status, err)
			}

			milestone := readNibFile(t, nibsDir, "pp-ms--milestone.md")

			// Key Decisions merge upward whatever the reason, and the parent's own
			// decisions survive the merge.
			for _, want := range []string{"Chose mdsection for parsing", "Previous decision"} {
				if !strings.Contains(milestone, want) {
					t.Errorf("close --as %s should have merged Key Decisions upward; %q missing from:\n%s", status, want, milestone)
				}
			}

			focus, found := mdsection.Find(milestone, "Current Focus", mdsection.AnyLevel)
			if !found {
				t.Fatalf("close --as %s removed the parent's Current Focus section:\n%s", status, milestone)
			}
			if status == closeCompletionStatus() {
				if !strings.Contains(focus, "Completed pp-ch: the child is off the board") {
					t.Errorf("close --as %s should have rewritten the parent's Current Focus, got: %q", status, focus)
				}
				return
			}
			if strings.TrimSpace(focus) != originalFocus {
				t.Errorf("close --as %s must leave the parent's Current Focus alone (setting work aside is not progress), got: %q", status, focus)
			}
			// Naming the child in the focus is the specific damage: it would read
			// as progress that never happened.
			if strings.Contains(focus, "pp-ch") {
				t.Errorf("close --as %s wrote the child into the parent's Current Focus: %q", status, focus)
			}
		})
	}
}

// TestCloseMergesEachChildDecisionIntoTheParentOnce is the parent-side cost of
// letting a closed nib be closed again: the Key Decisions merge runs on every
// close, so a merge that re-copied the whole child section would leave the
// parent holding one duplicate of it per close. Three closes here, because two
// would also pass a merge that happened to skip the second write.
func TestCloseMergesEachChildDecisionIntoTheParentOnce(t *testing.T) {
	nibsDir := setupCloseTest(t, map[string]string{
		"idm-ms--milestone.md": "---\nversion: 2\ntitle: Milestone\nstatus: in-progress\ntype: milestone\ncreated_at: 2026-01-02T03:04:05Z\nupdated_at: 2026-01-02T03:04:05Z\n---\n\n## Key Decisions\n\n- Previous decision\n",
		"idm-ch--child.md":     "---\nversion: 2\ntitle: Child\nstatus: in-progress\ntype: epic\nparent: idm-ms\n---\n\n## Key Decisions\n\n- Chose mdsection for parsing\n",
	})
	setNow := withCloseNow(t, time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC))

	for i, reason := range []string{"deferred", "scrapped", "scrapped"} {
		setNow(time.Date(2026, 7, 27+i, 0, 0, 0, 0, time.UTC))
		resetCloseFlags()
		withStdin(t, "revised to "+reason+"\n")
		rootCmd.SetArgs([]string{
			"--nibs-path", nibsDir,
			"close", "idm-ch", "--as", reason, "--summary", "-",
		})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("close #%d (--as %s) failed: %v", i+1, reason, err)
		}
	}

	milestone := readNibFile(t, nibsDir, "idm-ms--milestone.md")
	if n := strings.Count(milestone, "- Chose mdsection for parsing"); n != 1 {
		t.Errorf("expected the child's decision in the parent exactly once, got %d:\n%s", n, milestone)
	}
	if n := strings.Count(milestone, "- Previous decision"); n != 1 {
		t.Errorf("expected the parent's own decision exactly once, got %d:\n%s", n, milestone)
	}
}

// TestCloseMergesADecisionTheChildGainedBetweenCloses is the other half of that
// merge: skipping what the parent already carries must not degrade into skipping
// everything after the first close. A child that records a new decision while it
// sits closed still sends that line up when it is closed again — which is why the
// comparison is per line rather than over the section as a whole.
func TestCloseMergesADecisionTheChildGainedBetweenCloses(t *testing.T) {
	nibsDir := setupCloseTest(t, map[string]string{
		"gan-ms--milestone.md": "---\nversion: 2\ntitle: Milestone\nstatus: in-progress\ntype: milestone\ncreated_at: 2026-01-02T03:04:05Z\nupdated_at: 2026-01-02T03:04:05Z\n---\n\n## Key Decisions\n\n- Previous decision\n",
		"gan-ch--child.md":     "---\nversion: 2\ntitle: Child\nstatus: in-progress\ntype: epic\nparent: gan-ms\n---\n\n## Key Decisions\n\n- Chose mdsection for parsing\n",
	})
	setNow := withCloseNow(t, time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC))

	withStdin(t, "waiting on the vendor\n")
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "gan-ch", "--as", "deferred", "--summary", "-",
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("first close failed: %v", err)
	}

	// The child learns something between the two closes.
	const (
		firstDecision = "- Chose mdsection for parsing"
		gained        = "- Dropped the regex parser"
	)
	child := readNibFile(t, nibsDir, "gan-ch--child.md")
	revised := strings.Replace(child, firstDecision+"\n", firstDecision+"\n"+gained+"\n", 1)
	if revised == child {
		t.Fatalf("test setup: %q not found in the child, so it gained no decision:\n%s", firstDecision, child)
	}
	if err := os.WriteFile(dataPath(nibsDir, "gan-ch--child.md"), []byte(revised), 0644); err != nil {
		t.Fatal(err)
	}

	setNow(time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC))
	resetCloseFlags()
	withStdin(t, "superseded by another nib\n")
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "gan-ch", "--as", "scrapped", "--summary", "-",
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("second close failed: %v", err)
	}

	milestone := readNibFile(t, nibsDir, "gan-ms--milestone.md")
	if !strings.Contains(milestone, gained) {
		t.Errorf("the decision the child gained between the two closes never reached the parent:\n%s", milestone)
	}
	if n := strings.Count(milestone, firstDecision); n != 1 {
		t.Errorf("expected the decision merged by the first close to stay at one copy, got %d:\n%s", n, milestone)
	}
}

// TestCloseSummaryEntryDatesInUTC pins the zone of the entry's date stamp
// against the zone updated_at is written in. Both describe the same close, so a
// stamp taken in the machine's local zone would date it a day off from the front
// matter for anyone whose offset crosses midnight. The cases are one instant seen
// from three zones; only the UTC day may reach the entry. The command-level tests
// cannot catch this — withCloseNow pins UTC times, where the two zones agree.
func TestCloseSummaryEntryDatesInUTC(t *testing.T) {
	tests := []struct {
		name string
		when time.Time
		want string
	}{
		{
			name: "utc",
			when: time.Date(2026, 7, 29, 1, 0, 0, 0, time.UTC),
			want: "**Completed 2026-07-29** — shipped it",
		},
		{
			// 18:00 on the 28th, 7 hours behind UTC: the local clock still reads
			// the 28th while UTC has turned the page to the 29th.
			name: "behind utc",
			when: time.Date(2026, 7, 28, 18, 0, 0, 0, time.FixedZone("UTC-7", -7*60*60)),
			want: "**Completed 2026-07-29** — shipped it",
		},
		{
			// 08:00 on the 29th, 9 hours ahead: the local clock has turned the
			// page while UTC is still on the 28th.
			name: "ahead of utc",
			when: time.Date(2026, 7, 29, 8, 0, 0, 0, time.FixedZone("UTC+9", 9*60*60)),
			want: "**Completed 2026-07-28** — shipped it",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := closeSummaryEntry("completed", tt.when, "shipped it"); got != tt.want {
				t.Errorf("closeSummaryEntry(%s) = %q, want %q", tt.when.Format(time.RFC3339), got, tt.want)
			}
		})
	}
}

// TestCloseIntoAnEmptySummaryStub covers a ## Summary heading that exists with
// nothing under it — a hand-written stub, or a template that laid the section
// out in advance. The entry must land there like any other first entry, with no
// stray blank line standing in for the record that was never written.
func TestCloseIntoAnEmptySummaryStub(t *testing.T) {
	nibsDir := setupCloseTest(t, map[string]string{
		"stub-1--task.md": "---\nversion: 2\ntitle: Task\nstatus: in-progress\ntype: task\n---\n\nBody.\n\n## Summary\n",
	})
	withCloseNow(t, time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC))

	withStdin(t, "first record\n")
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "stub-1", "--summary", "-",
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	content := readNibFile(t, nibsDir, "stub-1--task.md")
	const want = "## Summary\n\n**Completed 2026-07-27** — first record\n"
	if !strings.Contains(content, want) {
		t.Errorf("expected %q, got:\n%s", want, content)
	}
}
