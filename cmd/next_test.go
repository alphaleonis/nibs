package cmd

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/testdata/fixtures"
	"github.com/spf13/pflag"
)

func resetNextFlags() {
	nextJSON = false
	nextCmd.Flags().Visit(func(f *pflag.Flag) {
		f.Changed = false
	})
}

// setupNextCLITest registers the flag resets `nibs next` tests need and hands
// back the sample fixture's store, whose only in-progress milestone is
// tnib-m001 (queue tnib-e001..e004; tnib-m002 is a draft).
func setupNextCLITest(t *testing.T) string {
	t.Helper()
	resetNextCLIFlags()
	t.Cleanup(resetNextCLIFlags)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	return filepath.Join(fixtures.CopySampleProject(t), ".nibs")
}

func resetNextCLIFlags() {
	resetNextFlags()
	// list is driven too, by the --ready agreement guard.
	resetListFlags()
	resetRootPersistentFlags()
}

// runNext runs `nibs next` against a store and returns its stdout.
func runNext(t *testing.T, nibsPath string, args ...string) string {
	t.Helper()
	resetNextCLIFlags()
	out, err := runRootWith(t, append([]string{"--nibs-path", nibsPath, "next"}, args...)...)
	if err != nil {
		t.Fatalf("next %v: %v", args, err)
	}
	return out
}

// runNextJSON runs `nibs next --json` and decodes the envelope.
func runNextJSON(t *testing.T, nibsPath string) nextOutput {
	t.Helper()
	var got nextOutput
	raw := runNext(t, nibsPath, "--json")
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode next JSON: %v\n%s", err, raw)
	}
	return got
}

// TestNextOnTheFixture pins the end-to-end answer and its provenance: the
// active milestone derives to tnib-m001, its queue head tnib-e001 is descended
// (past a completed feature and an in-progress task) to the first startable
// leaf, and every hop is named.
func TestNextOnTheFixture(t *testing.T) {
	nibsPath := setupNextCLITest(t)
	out := runNext(t, nibsPath)

	for _, want := range []string{
		"Next",
		"tnib-b001", // the answer: the first startable leaf under the queue head
		"Reached through",
		"tnib-m001", "active milestone",
		"tnib-e001", "queue position 1",
		"tnib-f002",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("next output missing %q, got:\n%s", want, out)
		}
	}

	got := runNextJSON(t, nibsPath)
	if got.Action == nil || got.Action.ID != "tnib-b001" {
		t.Fatalf("action = %v, want tnib-b001", got.Action)
	}
	if got.Milestone == nil || got.Milestone.ID != "tnib-m001" {
		t.Fatalf("milestone = %v, want tnib-m001 (the only in-progress milestone)", got.Milestone)
	}
	if got.QueuePosition != 1 {
		t.Errorf("queue_position = %d, want 1", got.QueuePosition)
	}
	var path []string
	for _, p := range got.Path {
		path = append(path, p.ID)
	}
	if want := "tnib-e001 tnib-f002 tnib-b001"; strings.Join(path, " ") != want {
		t.Errorf("path = %v, want %s (queue entry, then the descent)", path, want)
	}
	if got.Fallback != nil {
		t.Errorf("fallback = %+v, want none — an active milestone answered", got.Fallback)
	}
	if got.NoAnswer != nil {
		t.Errorf("no_answer = %+v, want none", got.NoAnswer)
	}
}

// TestNextAgreesWithReady is the cross-surface guard: `next` composes
// startability from the same two halves `list --ready` does, so whatever it
// offers must be in --ready's answer. Without it the two could drift into
// disagreeing about what "startable" means, which is the one thing an agent
// reading both would never expect.
func TestNextAgreesWithReady(t *testing.T) {
	nibsPath := setupNextCLITest(t)
	got := runNextJSON(t, nibsPath)
	if got.Action == nil {
		t.Fatal("next found nothing on the fixture; the agreement check needs an answer")
	}

	resetNextCLIFlags()
	ready, err := runRootWith(t, "--nibs-path", nibsPath, "list", "--ready", "-q")
	if err != nil {
		t.Fatalf("list --ready: %v", err)
	}
	ids := " " + strings.Join(strings.Fields(ready), " ") + " "
	if !strings.Contains(ids, " "+got.Action.ID+" ") {
		t.Errorf("next offered %s, which `list --ready` does not report as ready:\n%s", got.Action.ID, ready)
	}
}

// TestNextSkipsAQueueHeadWithNothingStartable pins the acceptance case: a
// queue entry with nothing startable under it is passed over and the walk
// carries on, rather than the whole answer collapsing to "nothing".
func TestNextSkipsAQueueHeadWithNothingStartable(t *testing.T) {
	nibsPath := writeStoreFiles(t, map[string]string{
		"nm1--wave.md":     "---\nversion: 2\ntitle: Wave One\nstatus: in-progress\ntype: milestone\norder: a\n---\n",
		"ne1--drafting.md": "---\nversion: 2\ntitle: Still Drafting\nstatus: draft\ntype: epic\nmilestone: nm1\nmilestone_order: a0\n---\n",
		"nt1--draft.md":    "---\nversion: 2\ntitle: Draft task\nstatus: draft\ntype: task\nparent: ne1\norder: a0\n---\n",
		"ne2--ready.md":    "---\nversion: 2\ntitle: Ready Epic\nstatus: todo\ntype: epic\nmilestone: nm1\nmilestone_order: b0\n---\n",
		"nt2--answer.md":   "---\nversion: 2\ntitle: The answer\nstatus: todo\ntype: task\nparent: ne2\norder: a0\n---\n",
	})
	t.Cleanup(resetNextCLIFlags)

	got := runNextJSON(t, nibsPath)
	if got.Action == nil || got.Action.ID != "nt2" {
		t.Fatalf("action = %v, want nt2 — the head entry offers nothing startable", got.Action)
	}
	if got.QueuePosition != 2 {
		t.Errorf("queue_position = %d, want 2", got.QueuePosition)
	}
	if got.PassedOver == nil || got.PassedOver.Open != 1 {
		t.Errorf("passed_over = %+v, want one open-but-not-startable nib counted", got.PassedOver)
	}
}

// TestNextFallsBackWithoutAnActiveMilestone pins the labeled fallback: with
// milestones declared but none in progress, the walk runs over the store's own
// tree order and says so — the day-one flat-list shape still gets an answer,
// but never one dressed up as a plan.
func TestNextFallsBackWithoutAnActiveMilestone(t *testing.T) {
	nibsPath := writeStoreFiles(t, map[string]string{
		"nm1--wave.md":   "---\nversion: 2\ntitle: Wave One\nstatus: todo\ntype: milestone\norder: a\n---\n",
		"nt1--first.md":  "---\nversion: 2\ntitle: First root\nstatus: todo\ntype: task\norder: a0\n---\n",
		"nt2--second.md": "---\nversion: 2\ntitle: Second root\nstatus: todo\ntype: task\norder: b0\n---\n",
	})
	t.Cleanup(resetNextCLIFlags)

	out := runNext(t, nibsPath)
	if !strings.Contains(out, "fallback") {
		t.Errorf("output does not label the answer a fallback:\n%s", out)
	}

	got := runNextJSON(t, nibsPath)
	if got.Action == nil || got.Action.ID != "nt1" {
		t.Fatalf("action = %v, want nt1 (first startable in tree order)", got.Action)
	}
	if got.Fallback == nil || got.Fallback.Reason != "no_active_milestone" {
		t.Fatalf("fallback = %+v, want reason no_active_milestone", got.Fallback)
	}
	if !strings.Contains(got.Fallback.Message, "in-progress") {
		t.Errorf("fallback message does not say what would change it: %q", got.Fallback.Message)
	}
	if got.Milestone != nil {
		t.Errorf("milestone = %+v, want none — nothing derived as active", got.Milestone)
	}
	if got.QueuePosition != 0 {
		t.Errorf("queue_position = %d, want 0 — the answer came from no queue", got.QueuePosition)
	}
}

// TestNextFallsBackWithNoMilestonesAtAll pins the other fallback trigger and
// its distinct reason token.
func TestNextFallsBackWithNoMilestonesAtAll(t *testing.T) {
	nibsPath := writeStoreFiles(t, map[string]string{
		"nt1--first.md": "---\nversion: 2\ntitle: First root\nstatus: todo\ntype: task\norder: a0\n---\n",
	})
	t.Cleanup(resetNextCLIFlags)

	got := runNextJSON(t, nibsPath)
	if got.Fallback == nil || got.Fallback.Reason != "no_milestones" {
		t.Fatalf("fallback = %+v, want reason no_milestones", got.Fallback)
	}
	if got.Action == nil || got.Action.ID != "nt1" {
		t.Fatalf("action = %v, want nt1", got.Action)
	}
}

// TestNextDoesNotRouteAroundAnActiveMilestone pins the deliberate asymmetry:
// once a milestone IS active, `next` speaks only for it. Unplanned startable
// work sitting outside the queue is NOT offered — the honest next action is to
// change the plan.
func TestNextDoesNotRouteAroundAnActiveMilestone(t *testing.T) {
	t.Run("empty queue", func(t *testing.T) {
		nibsPath := writeStoreFiles(t, map[string]string{
			"nm1--wave.md":      "---\nversion: 2\ntitle: Wave One\nstatus: in-progress\ntype: milestone\norder: a\n---\n",
			"nt1--unplanned.md": "---\nversion: 2\ntitle: Unplanned\nstatus: todo\ntype: task\norder: a0\n---\n",
		})
		t.Cleanup(resetNextCLIFlags)

		got := runNextJSON(t, nibsPath)
		if got.Action != nil {
			t.Fatalf("action = %s, want none — the active milestone's queue is empty", got.Action.ID)
		}
		if got.NoAnswer == nil || got.NoAnswer.Reason != "empty_queue" {
			t.Fatalf("no_answer = %+v, want reason empty_queue", got.NoAnswer)
		}
		if got.Fallback != nil {
			t.Errorf("fallback = %+v, want none — a plan exists", got.Fallback)
		}
		if got.Milestone == nil || got.Milestone.ID != "nm1" {
			t.Errorf("milestone = %+v, want nm1 named even with nothing to offer", got.Milestone)
		}
		if !strings.Contains(got.NoAnswer.Message, "--milestone nm1") {
			t.Errorf("no_answer message does not say what would change it: %q", got.NoAnswer.Message)
		}
	})

	t.Run("nothing startable names why", func(t *testing.T) {
		nibsPath := writeStoreFiles(t, map[string]string{
			"nm1--wave.md":      "---\nversion: 2\ntitle: Wave One\nstatus: in-progress\ntype: milestone\norder: a\n---\n",
			"ne1--epic.md":      "---\nversion: 2\ntitle: The Epic\nstatus: in-progress\ntype: epic\nmilestone: nm1\nmilestone_order: a0\n---\n",
			"nt1--done.md":      "---\nversion: 2\ntitle: Done\nstatus: completed\ntype: task\nparent: ne1\norder: a0\n---\n",
			"nt2--blocked.md":   "---\nversion: 2\ntitle: Blocked\nstatus: todo\ntype: task\nparent: ne1\norder: b0\nblocked_by:\n    - ndep\n---\n",
			"nt3--draft.md":     "---\nversion: 2\ntitle: Draft\nstatus: draft\ntype: task\nparent: ne1\norder: c0\n---\n",
			"ndep--outside.md":  "---\nversion: 2\ntitle: Dependency\nstatus: todo\ntype: task\norder: z0\n---\n",
			"nt9--unplanned.md": "---\nversion: 2\ntitle: Unplanned\nstatus: todo\ntype: task\norder: a0\n---\n",
		})
		t.Cleanup(resetNextCLIFlags)

		out := runNext(t, nibsPath)
		if !strings.Contains(out, "nothing startable") {
			t.Errorf("output does not say nothing is startable:\n%s", out)
		}

		got := runNextJSON(t, nibsPath)
		if got.Action != nil {
			t.Fatalf("action = %s, want none", got.Action.ID)
		}
		if got.NoAnswer == nil || got.NoAnswer.Reason != "nothing_startable" {
			t.Fatalf("no_answer = %+v, want reason nothing_startable", got.NoAnswer)
		}
		if got.Fallback != nil {
			t.Errorf("fallback = %+v, want none — unplanned work must not be offered around a plan", got.Fallback)
		}
		for _, want := range []string{"1 closed", "1 blocked", "1 open but not startable"} {
			if !strings.Contains(got.NoAnswer.Message, want) {
				t.Errorf("no_answer message %q does not name %q", got.NoAnswer.Message, want)
			}
		}
		want := nextTally{Closed: 1, Blocked: 1, Open: 1}
		if got.PassedOver == nil || *got.PassedOver != want {
			t.Errorf("passed_over = %+v, want %+v", got.PassedOver, want)
		}
	})
}

// TestNextReportsASkippedInversion pins decision 2.3 at the CLI: the entry that
// sits ahead of its still-blocking queue mate is passed over with its subtree,
// the walk answers from the next entry, and the skip is named rather than
// silent.
func TestNextReportsASkippedInversion(t *testing.T) {
	nibsPath := writeStoreFiles(t, map[string]string{
		"nm1--wave.md":     "---\nversion: 2\ntitle: Wave One\nstatus: in-progress\ntype: milestone\norder: a\n---\n",
		"ne1--ahead.md":    "---\nversion: 2\ntitle: Ahead\nstatus: todo\ntype: epic\nmilestone: nm1\nmilestone_order: a0\nblocked_by:\n    - ne2\n---\n",
		"nt1--under.md":    "---\nversion: 2\ntitle: Under the inverted entry\nstatus: todo\ntype: task\nparent: ne1\norder: a0\n---\n",
		"ne2--blocker.md":  "---\nversion: 2\ntitle: Blocker\nstatus: todo\ntype: epic\nmilestone: nm1\nmilestone_order: b0\n---\n",
		"nt2--eventual.md": "---\nversion: 2\ntitle: The answer\nstatus: todo\ntype: task\nparent: ne2\norder: a0\n---\n",
	})
	t.Cleanup(resetNextCLIFlags)

	out := runNext(t, nibsPath)
	if !strings.Contains(out, "ne1") || !strings.Contains(out, "still blocks it") {
		t.Errorf("output does not name the skipped inversion:\n%s", out)
	}

	got := runNextJSON(t, nibsPath)
	if got.Action == nil || got.Action.ID != "nt2" {
		t.Fatalf("action = %v, want nt2 — nt1 sits under an inverted queue entry", got.Action)
	}
	if len(got.Inversions) != 1 {
		t.Fatalf("inversions = %+v, want exactly one", got.Inversions)
	}
	if got.Inversions[0].Ahead != "ne1" || got.Inversions[0].Blocker != "ne2" || got.Inversions[0].Milestone != "nm1" {
		t.Errorf("inversion = %+v, want ahead ne1, blocker ne2 in nm1", got.Inversions[0])
	}
	if got.PassedOver == nil || got.PassedOver.Inverted != 1 {
		t.Errorf("passed_over = %+v, want one inverted entry", got.PassedOver)
	}
}

// TestNextLeavesTheStoreUnchanged is the guard for the read-only decision:
// `next` reads a queue without going through Orderer.Members, which backfills
// a missing milestone_order onto members as a side effect of being read. The
// queue below holds an unkeyed member precisely to trip that write, and the
// whole store must come back byte-identical — a question may not edit files
// the caller never named, and must not need a writable store to be asked.
func TestNextLeavesTheStoreUnchanged(t *testing.T) {
	// The timestamps are load-bearing: loadNib synthesizes created_at/updated_at
	// from mtime for a file that omits them, and the synthesized in-memory etag
	// then never matches the stored one — so a backfill write would be refused
	// by the etag check and this guard would pass for the wrong reason.
	const stamps = "created_at: 2026-01-01T00:00:00Z\nupdated_at: 2026-01-01T00:00:00Z\n"
	nibsPath := writeStoreFiles(t, map[string]string{
		"nm1--wave.md":    "---\nversion: 2\ntitle: Wave One\nstatus: in-progress\ntype: milestone\n" + stamps + "order: a\n---\n",
		"ne1--keyed.md":   "---\nversion: 2\ntitle: Keyed entry\nstatus: draft\ntype: epic\n" + stamps + "milestone: nm1\nmilestone_order: a0\n---\n",
		"ne2--unkeyed.md": "---\nversion: 2\ntitle: Unkeyed entry\nstatus: todo\ntype: epic\n" + stamps + "milestone: nm1\n---\n",
	})
	t.Cleanup(resetNextCLIFlags)

	before := storeSnapshot(t, nibsPath)
	got := runNextJSON(t, nibsPath)
	if got.Action == nil || got.Action.ID != "ne2" {
		t.Fatalf("action = %v, want ne2 (the unkeyed member is still walked, last)", got.Action)
	}
	after := storeSnapshot(t, nibsPath)
	for name, want := range before {
		if after[name] != want {
			t.Errorf("`nibs next` rewrote %s:\n before: %q\n  after: %q", name, want, after[name])
		}
	}
	if len(after) != len(before) {
		t.Errorf("store holds %d files after next, want %d", len(after), len(before))
	}
}

// storeSnapshot reads every file under the store's data directory, keyed by
// name, so a read command can be held to changing none of them.
func storeSnapshot(t *testing.T, nibsPath string) map[string]string {
	t.Helper()
	dir := storeDataDir(nibsPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read store data dir: %v", err)
	}
	snap := make(map[string]string, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		snap[e.Name()] = string(b)
	}
	if len(snap) == 0 {
		t.Fatal("store snapshot is empty; the comparison would be vacuous")
	}
	return snap
}

// TestNextHasNothingToDoIsNotAFailure pins the exit contract: no answer is an
// answer. An agent branches on a null "action", never on $?.
//
// Both halves of that sentence are asserted, and the second needs raw JSON:
// decoding into nextOutput maps an ABSENT "action" and an explicit null onto
// the same nil pointer, so a struct assertion cannot tell the branch an agent
// is told to take from one that never appears. The key must be there.
func TestNextHasNothingToDoIsNotAFailure(t *testing.T) {
	nibsPath := writeStoreFiles(t, map[string]string{
		"nm1--wave.md": "---\nversion: 2\ntitle: Wave One\nstatus: in-progress\ntype: milestone\norder: a\n---\n",
	})
	t.Cleanup(resetNextCLIFlags)

	resetNextCLIFlags()
	if _, err := runRootWith(t, "--nibs-path", nibsPath, "next"); err != nil {
		t.Fatalf("next with nothing to do exited non-zero: %v", err)
	}
	resetNextCLIFlags()
	raw, err := runRootWith(t, "--nibs-path", nibsPath, "next", "--json")
	if err != nil {
		t.Fatalf("next --json with nothing to do exited non-zero: %v", err)
	}

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		t.Fatalf("decode next envelope: %v\n%s", err, raw)
	}
	action, ok := envelope["action"]
	if !ok {
		t.Fatalf("the envelope has no \"action\" key at all, so the documented branch has nothing to read:\n%s", raw)
	}
	if got := strings.TrimSpace(string(action)); got != "null" {
		t.Errorf("action = %s, want null with nothing to do", got)
	}
}

// TestNextTakesNoPositionalArguments pins the coded refusal so a mistyped
// `nibs next <id>` reports validation (exit 2) instead of quietly answering a
// different question.
func TestNextTakesNoPositionalArguments(t *testing.T) {
	nibsPath := setupNextCLITest(t)
	_, err := runRootWith(t, "--nibs-path", nibsPath, "next", "tnib-e001")
	if err == nil {
		t.Fatal("next accepted a positional argument, want refusal")
	}
	if code := reportExitError(io.Discard, err); code != output.ExitValidation {
		t.Errorf("exit = %d, want %d", code, output.ExitValidation)
	}
}

// TestResetNextFlagsClearsAllState mirrors the other reset helpers: walk
// nextCmd's FlagSet and verify every flag is back at its default, so a new
// flag that is not enrolled here fails loudly rather than leaking between
// subtests.
func TestResetNextFlagsClearsAllState(t *testing.T) {
	t.Cleanup(resetNextCLIFlags)
	resetRootPersistentFlags()

	if err := nextCmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf("pre-populate --json: %v", err)
	}

	resetNextFlags()
	nextCmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Value.String() != f.DefValue {
			t.Errorf("flag %s = %q after reset, want default %q", f.Name, f.Value.String(), f.DefValue)
		}
		if f.Changed {
			t.Errorf("flag %s still marked Changed after reset", f.Name)
		}
	})
}
