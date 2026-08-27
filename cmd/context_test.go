package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/nibcontext"
	"github.com/alphaleonis/nibs/testdata/fixtures"
	"github.com/spf13/pflag"
)

// resetContextFlags clears the package-level flag vars used by contextCmd so
// tests don't pollute each other via rootCmd's singleton state, and clears
// Cobra's "Changed" tracking.
func resetContextFlags() {
	contextJSON = false
	contextCmd.Flags().Visit(func(f *pflag.Flag) {
		f.Changed = false
	})
}

// setupContextCobraTest writes a store config (with prefix nibs-) plus nib files
// and returns the config path and .nibs directory so a test can drive the full
// Cobra pipeline via `--config <cfg> --nibs-path <dir> context ...`.
func setupContextCobraTest(t *testing.T, files map[string]string) (cfgPath, nibsDir string) {
	t.Helper()
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetContextFlags)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	t.Cleanup(func() { configPath = "" })
	resetContextFlags()

	tmpDir := t.TempDir()
	nibsDir = filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(storeDataDir(nibsDir), 0755); err != nil {
		t.Fatal(err)
	}
	cfgPath = filepath.Join(nibsDir, "config.yml")
	if err := os.WriteFile(cfgPath, []byte("nibs:\n  prefix: nibs-\n  id_length: 4\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(dataPath(nibsDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return cfgPath, nibsDir
}

// contextFixture is a small milestone subtree used by the context tests:
//   - nibs-din3 milestone (in-progress) is the root container
//   - nibs-aaaa task child (completed, estimate M)
//   - nibs-bbbb task child (in-progress, estimate M)
func contextFixture() map[string]string {
	return map[string]string{
		"nibs-din3--milestone.md": "---\nversion: 2\ntitle: Alpha Milestone\nstatus: in-progress\ntype: milestone\n---\n\nRoot container.\n",
		"nibs-aaaa--done.md":      "---\nversion: 2\ntitle: Done Task\nstatus: completed\ntype: task\nestimate: M\nparent: nibs-din3\n---\n\nDone.\n",
		"nibs-bbbb--active.md":    "---\nversion: 2\ntitle: Active Task\nstatus: in-progress\ntype: task\nestimate: M\nparent: nibs-din3\n---\n\nActive.\n",
	}
}

// runContextJSON drives `context --json <idArg>` through the full Cobra
// pipeline and returns the decoded context output.
func runContextJSON(t *testing.T, cfgPath, nibsDir, idArg string) contextOutput {
	t.Helper()
	resetContextFlags()
	// --config alone names the store; the two flags together are refused.
	_ = nibsDir
	rootCmd.SetArgs([]string{
		"--config", cfgPath,
		"context", "--json", idArg,
	})
	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("context --json %q failed: %v", idArg, execErr)
	}
	var got contextOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal context output for %q: %v\nraw: %s", idArg, err, out)
	}
	return got
}

// TestContextCommand_ShortIDResolves pins short-id resolution:
// `context <short-id>` must resolve to the same nib as `context <full-id>`,
// with no "nib not found" warning and identical summary content.
func TestContextCommand_ShortIDResolves(t *testing.T) {
	cfgPath, nibsDir := setupContextCobraTest(t, contextFixture())

	full := runContextJSON(t, cfgPath, nibsDir, "nibs-din3")
	short := runContextJSON(t, cfgPath, nibsDir, "din3")

	// The full id already works today; guard the expectations it sets up.
	if full.Root == nil {
		t.Fatalf("full-id context has nil Root; fixture/setup broken: %+v", full)
	}
	if full.Root.ID != "nibs-din3" {
		t.Fatalf("full-id Root.ID = %q, want nibs-din3", full.Root.ID)
	}
	if len(full.Warnings) != 0 {
		t.Fatalf("full-id context produced warnings: %v", full.Warnings)
	}

	// The bug: the short id yields "nib not found" + empty data.
	if len(short.Warnings) != 0 {
		t.Errorf("short-id context produced warnings %v, want none", short.Warnings)
	}
	if short.Root == nil {
		t.Fatalf("short-id context has nil Root — short id did not resolve")
	}
	if short.Root.ID != "nibs-din3" {
		t.Errorf("short-id Root.ID = %q, want nibs-din3", short.Root.ID)
	}

	// The two summaries must be identical: resolving a short id is exactly
	// resolving its full id.
	if !reflect.DeepEqual(short, full) {
		t.Errorf("short-id summary differs from full-id summary\nshort: %+v\nfull:  %+v", short, full)
	}
}

// TestContextCommand_UnknownIDWarns pins that an unknown id still produces the
// "nib not found" warning and an empty summary (behavior preserved by the fix).
func TestContextCommand_UnknownIDWarns(t *testing.T) {
	cfgPath, nibsDir := setupContextCobraTest(t, contextFixture())

	sum := runContextJSON(t, cfgPath, nibsDir, "zzzz")

	if sum.Root != nil {
		t.Errorf("unknown-id context Root = %+v, want nil", sum.Root)
	}
	if len(sum.Warnings) == 0 {
		t.Errorf("unknown-id context produced no warnings, want a 'nib not found' warning")
	}
}

// setupContextCLITest registers the flag resets the overview tests need and
// hands back the sample fixture's store, whose only in-progress milestone is
// tnib-m001 (tnib-m002 is a draft).
func setupContextCLITest(t *testing.T) string {
	t.Helper()
	resetContextCLIFlags()
	t.Cleanup(resetContextCLIFlags)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	return filepath.Join(fixtures.CopySampleProject(t), ".nibs")
}

// resetContextCLIFlags clears the flag state these tests share. It covers
// `next`'s flags too: the cross-surface guard drives both commands, and its
// own reset would otherwise leak --json into whatever runs after it.
func resetContextCLIFlags() {
	resetContextFlags()
	resetNextFlags()
	resetRootPersistentFlags()
}

// runContextIn runs `nibs context` against a store named by path and returns
// its stdout.
func runContextIn(t *testing.T, nibsPath string, args ...string) string {
	t.Helper()
	resetContextCLIFlags()
	out, err := runRootWith(t, append([]string{"--nibs-path", nibsPath, "context"}, args...)...)
	if err != nil {
		t.Fatalf("context %v: %v", args, err)
	}
	return out
}

// runContextInJSON runs `nibs context --json` against a store and decodes it.
func runContextInJSON(t *testing.T, nibsPath string, args ...string) contextOutput {
	t.Helper()
	raw := runContextIn(t, nibsPath, append([]string{"--json"}, args...)...)
	var got contextOutput
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode context JSON: %v\n%s", err, raw)
	}
	return got
}

// milestoneLine returns the overview line naming id, so an assertion can hold
// one milestone's row to something the neighboring row must not carry.
func milestoneLine(t *testing.T, out, id string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, id) {
			return line
		}
	}
	t.Fatalf("overview names no %s:\n%s", id, out)
	return ""
}

// hasLine reports whether some line of out is exactly want once trimmed, so an
// assertion can hold a whole line rather than a substring a neighboring line
// could satisfy too.
func hasLine(out, want string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

// TestContextOverviewNamesTheActiveMilestoneAndWhatNextAnswers pins the join
// the overview makes for its caller: which milestone is THE active one
// (decision 1.4 — in progress, earliest in milestone order) and what `nibs
// next` would answer from it. On the fixture that is tnib-m001 and tnib-t006,
// the first startable leaf under the queue head.
//
// The queue position is asserted together with the entry it belongs to, and
// they are DIFFERENT nibs here: position 1 is tnib-e001's place in tnib-m001's
// queue, and tnib-t006 — two levels below it — sits in no queue at all
// (`nibs mv tnib-t006 --queue --first` refuses for exactly that reason: it is
// "assigned to no milestone, so it has no queue position"). A number printed
// beside the action with no entry named would read as the action's own.
func TestContextOverviewNamesTheActiveMilestoneAndWhatNextAnswers(t *testing.T) {
	nibsPath := setupContextCLITest(t)

	got := runContextInJSON(t, nibsPath)

	if got.ActiveMilestone == nil || got.ActiveMilestone.ID != "tnib-m001" {
		t.Fatalf("active_milestone = %+v, want tnib-m001 (the only in-progress milestone)", got.ActiveMilestone)
	}
	if got.Next == nil {
		t.Fatal("next is absent; the overview must carry what `nibs next` answers")
	}
	if got.Next.Action == nil || got.Next.Action.ID != "tnib-t006" {
		t.Fatalf("next.action = %+v, want tnib-t006", got.Next.Action)
	}
	if got.Next.QueueEntry == nil || got.Next.QueueEntry.ID != "tnib-e001" {
		t.Errorf("next.queue_entry = %+v, want tnib-e001 — the queue member the walk descended from", got.Next.QueueEntry)
	}
	if got.Next.QueuePosition != 1 {
		t.Errorf("next.queue_position = %d, want 1 (tnib-e001's place, not tnib-t006's)", got.Next.QueuePosition)
	}
	if got.Next.Fallback != nil {
		t.Errorf("next.fallback = %+v, want none — an active milestone answered", got.Next.Fallback)
	}
	if got.Next.NoAnswer != nil {
		t.Errorf("next.no_answer = %+v, want none", got.Next.NoAnswer)
	}

	// The human form marks the active milestone, and only it.
	out := runContextIn(t, nibsPath)
	active := milestoneLine(t, out, "tnib-m001")
	if !strings.Contains(active, "active") {
		t.Errorf("the tnib-m001 row does not mark it active:\n%s", active)
	}
	if other := milestoneLine(t, out, "tnib-m002"); strings.Contains(other, "active") {
		t.Errorf("the tnib-m002 row is marked active, but it is a draft:\n%s", other)
	}
	for _, want := range []string{"Next", "tnib-t006", "via tnib-e001 · queue position 1"} {
		if !strings.Contains(out, want) {
			t.Errorf("context overview missing %q, got:\n%s", want, out)
		}
	}
}

// TestContextOverviewSaysWhenNoMilestoneIsActive pins the honest answer for a
// store where milestones exist but none is in progress: nothing derives as
// active, so nothing is named — and `next`'s answer stays labeled a fallback
// rather than being presented as the plan's.
func TestContextOverviewSaysWhenNoMilestoneIsActive(t *testing.T) {
	nibsPath := writeStoreFiles(t, map[string]string{
		"nm1--one.md":   "---\nversion: 2\ntitle: Wave One\nstatus: todo\ntype: milestone\norder: a\n---\n",
		"nm2--two.md":   "---\nversion: 2\ntitle: Wave Two\nstatus: draft\ntype: milestone\norder: b\n---\n",
		"nt1--loose.md": "---\nversion: 2\ntitle: Loose task\nstatus: todo\ntype: task\norder: a\n---\n",
	})
	t.Cleanup(resetContextCLIFlags)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })

	got := runContextInJSON(t, nibsPath)

	if len(got.Milestones) != 2 {
		t.Fatalf("milestones = %d, want 2 — both are open and must still be listed", len(got.Milestones))
	}
	if got.ActiveMilestone != nil {
		t.Errorf("active_milestone = %+v, want none — no milestone is in progress", got.ActiveMilestone)
	}
	if got.Next == nil || got.Next.Fallback == nil {
		t.Fatalf("next = %+v, want a fallback-labeled answer", got.Next)
	}
	if got.Next.Fallback.Reason != string(graph.NextReasonNoActiveMilestone) {
		t.Errorf("next.fallback.reason = %q, want %q", got.Next.Fallback.Reason, graph.NextReasonNoActiveMilestone)
	}
	if got.Next.Action == nil || got.Next.Action.ID != "nt1" {
		t.Fatalf("next.action = %+v, want nt1 — the fallback walk still answers", got.Next.Action)
	}
	// A fallback answer came from the store's tree order, so there is no queue
	// entry to anchor a position to and neither field may appear.
	if got.Next.QueueEntry != nil || got.Next.QueuePosition != 0 {
		t.Errorf("next carries queue_entry %+v / queue_position %d, want neither — the answer came from no queue",
			got.Next.QueueEntry, got.Next.QueuePosition)
	}

	out := runContextIn(t, nibsPath)
	if strings.Contains(out, "queue position") {
		t.Errorf("the overview prints a queue position for an answer that came from no queue:\n%s", out)
	}
	if !strings.Contains(out, "none active") {
		t.Errorf("the overview does not say no milestone is active:\n%s", out)
	}
	if !strings.Contains(out, "fallback") {
		t.Errorf("the overview does not label the answer a fallback:\n%s", out)
	}
	for _, id := range []string{"nm1", "nm2"} {
		if line := milestoneLine(t, out, id); strings.Contains(line, "active") {
			t.Errorf("the %s row is marked active, but nothing is in progress:\n%s", id, line)
		}
	}
}

// TestContextOverviewAnswersWithNoMilestonesDeclared pins the day-one shape:
// a store that declares no milestone at all still gets an overview, and the
// answer it gets is `next`'s fallback over the store's own tree order. The
// milestone list is empty, so nothing is marked and nothing is named active.
func TestContextOverviewAnswersWithNoMilestonesDeclared(t *testing.T) {
	nibsPath := writeStoreFiles(t, map[string]string{
		"nt1--loose.md": "---\nversion: 2\ntitle: Loose task\nstatus: todo\ntype: task\norder: a\n---\n",
	})
	t.Cleanup(resetContextCLIFlags)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })

	got := runContextInJSON(t, nibsPath)

	if len(got.Milestones) != 0 {
		t.Fatalf("milestones = %+v, want none", got.Milestones)
	}
	if got.ActiveMilestone != nil {
		t.Errorf("active_milestone = %+v, want none", got.ActiveMilestone)
	}
	if got.Next == nil || got.Next.Fallback == nil {
		t.Fatalf("next = %+v, want a fallback-labeled answer", got.Next)
	}
	if got.Next.Fallback.Reason != string(graph.NextReasonNoMilestones) {
		t.Errorf("next.fallback.reason = %q, want %q", got.Next.Fallback.Reason, graph.NextReasonNoMilestones)
	}
	if got.Next.Action == nil || got.Next.Action.ID != "nt1" {
		t.Fatalf("next.action = %+v, want nt1", got.Next.Action)
	}

	out := runContextIn(t, nibsPath)
	for _, want := range []string{"Milestones", "Next", "fallback", "nt1"} {
		if !strings.Contains(out, want) {
			t.Errorf("overview missing %q, got:\n%s", want, out)
		}
	}
	// An empty list gets a plain "none" rather than the "none active" line a
	// populated one gets: there is no entry here to be mistaken for the active
	// milestone, only a header that would otherwise be followed by nothing.
	if !hasLine(out, "none") {
		t.Errorf("the empty milestone list carries no \"none\" line:\n%s", out)
	}
}

// TestContextOverviewAgreesWithNext holds the two ADAPTATION layers to one
// answer: each command runs graph.Next and hands the result to its own builder
// (buildContextNext, buildNextOutput), and every field the overview
// republishes is compared here against the one `nibs next` publishes. A bug in
// the walk itself is outside what this can see — both sides would report it
// identically — so what it defends is the join, not the derivation behind it.
//
// The three stores are what make it defend anything: each populates a
// different one of the fields below, so no comparison in the table is nil
// against nil in every case. assertPopulated fails the case whose store
// stopped producing the shape it was chosen for, since that would silently
// turn the comparisons back into a trivial pass.
func TestContextOverviewAgreesWithNext(t *testing.T) {
	tests := []struct {
		name  string
		store func(t *testing.T) string
		// assertPopulated names what this store exists to exercise.
		assertPopulated func(t *testing.T, n *contextNext)
	}{
		{
			name:  "a queue answers",
			store: setupContextCLITest,
			assertPopulated: func(t *testing.T, n *contextNext) {
				if n.Action == nil || n.QueueEntry == nil || n.QueuePosition == 0 {
					t.Fatalf("the fixture must answer from a queue, got %+v", n)
				}
			},
		},
		{
			name: "a fallback answers",
			store: func(t *testing.T) string {
				return writeStoreFiles(t, map[string]string{
					"nm1--one.md":   "---\nversion: 2\ntitle: Wave One\nstatus: todo\ntype: milestone\norder: a\n---\n",
					"nt1--loose.md": "---\nversion: 2\ntitle: Loose task\nstatus: todo\ntype: task\norder: a\n---\n",
				})
			},
			assertPopulated: func(t *testing.T, n *contextNext) {
				if n.Fallback == nil {
					t.Fatalf("a store with no milestone in progress must label its answer a fallback, got %+v", n)
				}
			},
		},
		{
			name: "an empty queue answers with no answer",
			store: func(t *testing.T) string {
				return writeStoreFiles(t, map[string]string{
					"nm1--one.md": "---\nversion: 2\ntitle: Wave One\nstatus: in-progress\ntype: milestone\norder: a\n---\n",
				})
			},
			assertPopulated: func(t *testing.T, n *contextNext) {
				if n.NoAnswer == nil || n.Action != nil {
					t.Fatalf("an active milestone with an empty queue must report a no-answer, got %+v", n)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsPath := tt.store(t)
			t.Cleanup(resetContextCLIFlags)
			t.Cleanup(func() { rootCmd.SetArgs(nil) })

			ctx := runContextInJSON(t, nibsPath)
			nxt := runNextJSON(t, nibsPath)

			if ctx.Next == nil {
				t.Fatal("context carries no next answer")
			}
			tt.assertPopulated(t, ctx.Next)

			if !reflect.DeepEqual(ctx.Next.Action, nxt.Action) {
				t.Errorf("context next.action = %+v, next action = %+v", ctx.Next.Action, nxt.Action)
			}
			if !reflect.DeepEqual(ctx.ActiveMilestone, nxt.Milestone) {
				t.Errorf("context active_milestone = %+v, next milestone = %+v — one question, two answers",
					ctx.ActiveMilestone, nxt.Milestone)
			}
			// The overview's queue entry is `nibs next`'s first provenance
			// step, which is where that command attaches the same number.
			var wantEntry *nibcontext.NibRef
			if nxt.QueuePosition > 0 && len(nxt.Path) > 0 {
				wantEntry = nxt.Path[0]
			}
			if !reflect.DeepEqual(ctx.Next.QueueEntry, wantEntry) {
				t.Errorf("context queue_entry = %+v, next path[0] = %+v", ctx.Next.QueueEntry, wantEntry)
			}
			if ctx.Next.QueuePosition != nxt.QueuePosition {
				t.Errorf("context queue_position = %d, next queue_position = %d", ctx.Next.QueuePosition, nxt.QueuePosition)
			}
			if !reflect.DeepEqual(ctx.Next.Fallback, nxt.Fallback) {
				t.Errorf("context fallback = %+v, next fallback = %+v", ctx.Next.Fallback, nxt.Fallback)
			}
			if !reflect.DeepEqual(ctx.Next.NoAnswer, nxt.NoAnswer) {
				t.Errorf("context no_answer = %+v, next no_answer = %+v", ctx.Next.NoAnswer, nxt.NoAnswer)
			}
		})
	}
}

// TestContextOverviewReportsNoAnswerAsAnAnswer pins the "nothing to do" branch:
// it is an answer carrying a reason TOKEN and a sentence, not an omission, and
// the human form prints that sentence. renderContextNext dereferences NoAnswer
// whenever Action is nil — graph.Next sets a reason on every such path — so a
// path that ever stopped setting one fails here instead of printing prose of
// the renderer's own.
func TestContextOverviewReportsNoAnswerAsAnAnswer(t *testing.T) {
	const milestone = "---\nversion: 2\ntitle: Wave One\nstatus: in-progress\ntype: milestone\norder: a\n---\n"
	tests := []struct {
		name       string
		files      map[string]string
		wantReason graph.NextReason
		wantInMsg  string
	}{
		{
			name:       "the active milestone's queue is empty",
			files:      map[string]string{"nm1--wave.md": milestone},
			wantReason: graph.NextReasonEmptyQueue,
			wantInMsg:  "its queue is empty",
		},
		{
			name: "the queue's only entry is open but not startable",
			files: map[string]string{
				"nm1--wave.md":  milestone,
				"ne1--draft.md": "---\nversion: 2\ntitle: Drafted entry\nstatus: draft\ntype: epic\nmilestone: nm1\nmilestone_order: a\n---\n",
			},
			wantReason: graph.NextReasonNothingStartable,
			wantInMsg:  "nothing startable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsPath := writeStoreFiles(t, tt.files)
			t.Cleanup(resetContextCLIFlags)
			t.Cleanup(func() { rootCmd.SetArgs(nil) })

			raw := runContextIn(t, nibsPath, "--json")
			// The key must be present and null: decoding maps an absent
			// "action" and an explicit null onto the same nil pointer, so only
			// the raw bytes can show the branch an agent is told to take.
			if !strings.Contains(raw, `"action": null`) {
				t.Errorf("context --json omits the null action an agent branches on:\n%s", raw)
			}
			var got contextOutput
			if err := json.Unmarshal([]byte(raw), &got); err != nil {
				t.Fatalf("decode context JSON: %v\n%s", err, raw)
			}
			if got.Next == nil || got.Next.NoAnswer == nil {
				t.Fatalf("next = %+v, want a no_answer", got.Next)
			}
			if got.Next.NoAnswer.Reason != string(tt.wantReason) {
				t.Errorf("next.no_answer.reason = %q, want %q", got.Next.NoAnswer.Reason, tt.wantReason)
			}
			if !strings.Contains(got.Next.NoAnswer.Message, tt.wantInMsg) {
				t.Errorf("next.no_answer.message = %q, want it to contain %q", got.Next.NoAnswer.Message, tt.wantInMsg)
			}

			out := runContextIn(t, nibsPath)
			if !strings.Contains(out, got.Next.NoAnswer.Message) {
				t.Errorf("the overview does not print the no-answer sentence %q, got:\n%s", got.Next.NoAnswer.Message, out)
			}
		})
	}
}

// TestContextOverviewCarriesTheQueueInversionWarning keeps the overview from
// being lossy where it matters most: `nibs next` warns when a queue entry sits
// ahead of a blocker that is itself later in the queue, and an agent told the
// overview is its one "where are we, what do I do" call would otherwise read
// the position jump as the head being finished. The two surfaces must warn in
// the same words.
func TestContextOverviewCarriesTheQueueInversionWarning(t *testing.T) {
	nibsPath := writeStoreFiles(t, map[string]string{
		"nm1--wave.md": "---\nversion: 2\ntitle: Wave One\nstatus: in-progress\ntype: milestone\norder: a\n---\n",
		"na1--a.md":    "---\nversion: 2\ntitle: Entry A\nstatus: todo\ntype: task\nmilestone: nm1\nmilestone_order: a\nblocked_by:\n  - nc1\n---\n",
		"nb1--b.md":    "---\nversion: 2\ntitle: Entry B\nstatus: todo\ntype: task\nmilestone: nm1\nmilestone_order: b\n---\n",
		"nc1--c.md":    "---\nversion: 2\ntitle: Entry C\nstatus: todo\ntype: task\nmilestone: nm1\nmilestone_order: c\n---\n",
	})
	t.Cleanup(resetContextCLIFlags)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })

	got := runContextInJSON(t, nibsPath)
	if got.Next == nil || got.Next.Action == nil || got.Next.Action.ID != "nb1" {
		t.Fatalf("next.action = %+v, want nb1 — na1 is skipped as an inversion", got.Next)
	}
	nxt := runNextJSON(t, nibsPath)
	if len(nxt.Inversions) != 1 {
		t.Fatalf("next reported %d inversions, want 1", len(nxt.Inversions))
	}
	want := nxt.Inversions[0].Message
	if !slices.Contains(got.Warnings, want) {
		t.Errorf("context warnings = %q, want the inversion `nibs next` reports: %q", got.Warnings, want)
	}

	out := runContextIn(t, nibsPath)
	if !strings.Contains(out, "skipped na1") {
		t.Errorf("the overview does not warn that na1 was skipped:\n%s", out)
	}
	// nb1 is the queue entry AND the action, so there is no descent to name:
	// the position stands alone rather than being attributed "via" the nib it
	// is already printed beside.
	if !strings.Contains(out, "queue position 2") || strings.Contains(out, "via nb1") {
		t.Errorf("the answer line should carry a bare \"queue position 2\", got:\n%s", out)
	}
}

// TestContextOverviewWarnsOncePerSkippedEntry bounds what the overview spends
// on inversions. graph.Next records one pair per (entry, blocker), so a queue
// head that gates the entries behind it produces a pair for each one; the
// overview prints its answer BELOW the warnings, so a per-pair rendering
// scales the caveat with the fan-out and buries the answer. One line per
// skipped ENTRY is the bound, and the walk's own Inverted tally is what it has
// to equal — the table asserts that equality in every shape, which is the
// assertion a return to per-pair emission fails.
func TestContextOverviewWarnsOncePerSkippedEntry(t *testing.T) {
	const milestone = "---\nversion: 2\ntitle: Wave One\nstatus: in-progress\ntype: milestone\norder: a\n---\n"
	entry := func(title, order string, blockers ...string) string {
		s := "---\nversion: 2\ntitle: " + title + "\nstatus: todo\ntype: task\nmilestone: nm1\nmilestone_order: " + order + "\n"
		if len(blockers) > 0 {
			s += "blocked_by:\n"
			for _, b := range blockers {
				s += "  - " + b + "\n"
			}
		}
		return s + "---\n"
	}

	tests := []struct {
		name         string
		files        map[string]string
		wantPairs    int
		wantWarnings int
		wantContains []string
		wantAbsent   []string
	}{
		{
			// One blocker is `nibs next`'s own sentence, reused verbatim; the
			// case is re-pinned here so the aggregate cannot quietly take it
			// over and reword the common skip.
			name: "one blocker is one pair and one warning",
			files: map[string]string{
				"nm1--wave.md": milestone,
				"na1--a.md":    entry("Entry A", "a", "nc1"),
				"nb1--b.md":    entry("Entry B", "b"),
				"nc1--c.md":    entry("Entry C", "c"),
			},
			wantPairs:    1,
			wantWarnings: 1,
			wantContains: []string{"skipped na1: it sits ahead of nc1, which still blocks it"},
			wantAbsent:   []string{"later blockers"},
		},
		{
			// The remedy names the LAST blocker in queue order, since that is
			// the single move that clears every pair the entry has.
			name: "two blockers on one entry collapse to one warning",
			files: map[string]string{
				"nm1--wave.md": milestone,
				"na1--a.md":    entry("Entry A", "a", "nc1", "nd1"),
				"nb1--b.md":    entry("Entry B", "b"),
				"nc1--c.md":    entry("Entry C", "c"),
				"nd1--d.md":    entry("Entry D", "d"),
			},
			wantPairs:    2,
			wantWarnings: 1,
			wantContains: []string{
				"skipped na1: it sits ahead of 2 later blockers, the last of them nd1",
				"`nibs mv na1 --queue --after nd1`",
			},
			wantAbsent: []string{"--after nc1"},
		},
		{
			name: "three skipped entries with four pairs between them",
			files: map[string]string{
				"nm1--wave.md": milestone,
				"na1--a.md":    entry("Entry A1", "a", "nc1"),
				"na2--b.md":    entry("Entry A2", "b", "nc2", "nc3"),
				"na3--c.md":    entry("Entry A3", "c", "nc4"),
				"nz1--z.md":    entry("Startable", "d"),
				"nc1--c1.md":   entry("Blocker C1", "e"),
				"nc2--c2.md":   entry("Blocker C2", "f"),
				"nc3--c3.md":   entry("Blocker C3", "g"),
				"nc4--c4.md":   entry("Blocker C4", "h"),
			},
			wantPairs:    4,
			wantWarnings: 3,
			wantContains: []string{
				"skipped na1: it sits ahead of nc1",
				"skipped na2: it sits ahead of 2 later blockers, the last of them nc3",
				"skipped na3: it sits ahead of nc4",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsPath := writeStoreFiles(t, tt.files)
			t.Cleanup(resetContextCLIFlags)
			t.Cleanup(func() { rootCmd.SetArgs(nil) })

			nxt := runNextJSON(t, nibsPath)
			if len(nxt.Inversions) != tt.wantPairs {
				t.Fatalf("next reported %d (entry, blocker) pairs, want %d", len(nxt.Inversions), tt.wantPairs)
			}
			if nxt.PassedOver == nil || nxt.PassedOver.Inverted != tt.wantWarnings {
				t.Fatalf("next passed_over = %+v, want inverted = %d", nxt.PassedOver, tt.wantWarnings)
			}

			got := runContextInJSON(t, nibsPath)
			if len(got.Warnings) != nxt.PassedOver.Inverted {
				t.Errorf("context emitted %d warnings for %d skipped entries (%d pairs) — the line count must not scale with the blocker fan-out:\n%s",
					len(got.Warnings), nxt.PassedOver.Inverted, len(nxt.Inversions), strings.Join(got.Warnings, "\n"))
			}

			joined := strings.Join(got.Warnings, "\n")
			for _, want := range tt.wantContains {
				if !strings.Contains(joined, want) {
					t.Errorf("context warnings do not contain %q:\n%s", want, joined)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(joined, absent) {
					t.Errorf("context warnings unexpectedly contain %q:\n%s", absent, joined)
				}
			}

			assertWarningBlockIsClosed(t, runContextIn(t, nibsPath), tt.wantWarnings)
		})
	}
}

// TestContextRunsNoWarningBlockIntoTheBody guards the separator from both
// sides: a warning block ends with a blank line before the body, and a clean
// overview opens on its first header rather than on a stray blank line.
func TestContextRunsNoWarningBlockIntoTheBody(t *testing.T) {
	nibsPath := setupContextCLITest(t)

	out := runContextIn(t, nibsPath)
	if strings.Contains(out, "⚠") {
		t.Fatalf("the sample fixture is expected to warn about nothing:\n%s", out)
	}
	assertWarningBlockIsClosed(t, out, 0)
}

// assertWarningBlockIsClosed holds the human overview to `nibs next`'s
// convention: warnings first, then ONE blank line, then the body. With no
// warnings the body starts immediately — no leading blank line to suggest a
// block that was not printed.
func assertWarningBlockIsClosed(t *testing.T, out string, wantWarnings int) {
	t.Helper()
	lines := strings.Split(out, "\n")
	last := -1
	warnings := 0
	for i, line := range lines {
		if strings.Contains(line, "⚠") {
			warnings++
			last = i
		}
	}
	if warnings != wantWarnings {
		t.Fatalf("the human overview printed %d warning lines, want %d:\n%s", warnings, wantWarnings, out)
	}
	if wantWarnings == 0 {
		if !strings.Contains(lines[0], "Milestones") {
			t.Errorf("a warning-free overview should open on its first header, got %q:\n%s", lines[0], out)
		}
		return
	}
	if last+2 >= len(lines) {
		t.Fatalf("nothing follows the warning block:\n%s", out)
	}
	if strings.TrimSpace(lines[last+1]) != "" {
		t.Errorf("the warning block runs straight into %q — it needs a blank line after it:\n%s", lines[last+1], out)
	}
	if !strings.Contains(lines[last+2], "Milestones") {
		t.Errorf("the body after the warning block starts with %q, want the Milestones header:\n%s", lines[last+2], out)
	}
}

// TestContextDetailCarriesNeitherActiveMilestoneNorNext keeps the contract
// lean: the two new fields answer "where are we, what do I do", which is the
// overview's question. A rooted call is a subtree summary and stays one.
func TestContextDetailCarriesNeitherActiveMilestoneNorNext(t *testing.T) {
	nibsPath := setupContextCLITest(t)

	got := runContextInJSON(t, nibsPath, "tnib-m001")

	if got.Root == nil || got.Root.ID != "tnib-m001" {
		t.Fatalf("root = %+v, want tnib-m001", got.Root)
	}
	if got.ActiveMilestone != nil {
		t.Errorf("active_milestone = %+v, want none in detail mode", got.ActiveMilestone)
	}
	if got.Next != nil {
		t.Errorf("next = %+v, want none in detail mode", got.Next)
	}
}

// TestContextLeavesTheStoreUnchanged holds `nibs context` to asking a question
// and nothing else. graph.Next reads the active milestone's queue from a
// membership view rather than through Orderer.Members, which backfills a
// missing milestone_order onto members as a side effect of being read, and the
// queue below carries an unkeyed member so that a regression routing this path
// back through the Orderer lands a write. That mutation does fail this test,
// on a rewritten ne2--unkeyed.md — the guard bites for the thing it names.
//
// The comparison covers every file at the top level of the store's data
// directory, not the unkeyed member's alone: a backfill misdirected onto the
// keyed member or onto the milestone would otherwise pass unseen. It does not
// descend into subdirectories or read archive/, which this fixture does not
// use — a case that needs either has to widen storeSnapshot first.
func TestContextLeavesTheStoreUnchanged(t *testing.T) {
	nibsPath := writeStoreFiles(t, map[string]string{
		"nm1--wave.md":    "---\nversion: 2\ntitle: Wave One\nstatus: in-progress\ntype: milestone\norder: a\n---\n",
		"ne1--keyed.md":   "---\nversion: 2\ntitle: Keyed entry\nstatus: draft\ntype: epic\nmilestone: nm1\nmilestone_order: a0\n---\n",
		"ne2--unkeyed.md": "---\nversion: 2\ntitle: Unkeyed entry\nstatus: todo\ntype: epic\nmilestone: nm1\n---\n",
	})
	t.Cleanup(resetContextCLIFlags)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })

	before := storeSnapshot(t, nibsPath)
	got := runContextInJSON(t, nibsPath)
	if got.Next == nil || got.Next.Action == nil || got.Next.Action.ID != "ne2" {
		t.Fatalf("next.action = %+v, want ne2 (the unkeyed member is still walked, last)", got.Next)
	}
	assertStoreUnchanged(t, before, storeSnapshot(t, nibsPath))
}
