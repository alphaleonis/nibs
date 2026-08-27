package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/testdata/fixtures"
	"github.com/spf13/pflag"
)

// readyShapes are the `--ready` invocations this guard pins. The first carries
// the default rendering AND the order the rows come back in — the two things a
// queue-aware ready would disturb first. The second adds the projected `ready`
// field, so the filter half of the rule and the projection half are both held
// blind rather than only the one the flag narrows on.
var readyShapes = [][]string{
	{"list", "--ready"},
	{"list", "--ready", "-f", "id,status,ready"},
}

// TestReadyIsBlindToTheQueueAxis proves the scheduling axis is irrelevant to
// `nibs list --ready`. --ready answers FEASIBILITY — a startable status and no
// active blocker — and nothing about it may read `milestone:` or
// `milestone_order:`.
//
// It is an oracle, not a golden: a stored capture of the fixture's own answer
// would stay green while the axis leaked, because the fixture's assignments
// never move. So each case perturbs the queue axis on its own fixture copy and
// asserts the --ready answer does not move — byte for byte, ordering included.
//
// Each case first requires its perturbation to have reached the queue-axis
// derivation it speaks to (queueWitness, read by the case's own moved func).
// Without that, a perturbation that quietly stopped landing — a renamed key, a
// fixture nib that moved — would leave the comparison passing on two identical
// stores and the guard would be decoration. Two milestone cases need more than
// "something moved" to say that, because the perturbations differ from their
// siblings' by one edit whose effect only one consumer renders: see
// nextNamesM002 and milestoneLeftContext.
//
// The milestone cases only ever rewrite a milestone's own status, and never to
// a startable one: a startable milestone would enter --ready's answer on its
// own merits, so the answer would move for reasons of feasibility and the case
// would stop speaking about the queue. No fixture nib is blocked_by a
// milestone, so a milestone that closes releases nothing either — which is what
// lets milestone-closes cross the closed-status boundary `nibs context` keys on.
func TestReadyIsBlindToTheQueueAxis(t *testing.T) {
	t.Cleanup(resetReadyBlindFlags)

	cases := []struct {
		name string
		// requires states, for the failure message, what this case's
		// perturbation must have done to the queue axis; moved reads that
		// off the witness, against the unperturbed one.
		requires string
		moved    func(base, got queueSight) bool
		perturb  func(t *testing.T, dataDir string)
	}{
		{"assignment", "`nibs next` or `nibs roadmap`", nextOrRoadmapMoved, perturbQueueAssignment},
		{"order-keys", "`nibs next` or `nibs roadmap`", nextOrRoadmapMoved, perturbQueueOrderKeys},
		{"active-milestone-moves", "`nibs next` onto tnib-m002's queue", nextNamesM002, perturbActiveMilestone},
		{"no-active-milestone", "`nibs next`", nextMoved, perturbNoActiveMilestone},
		{"milestone-closes", "tnib-m001 out of `nibs context`'s milestone list", milestoneLeftContext, perturbActiveMilestoneCloses},
		{"whole-axis", "`nibs next` or `nibs roadmap`", nextOrRoadmapMoved, func(t *testing.T, dataDir string) {
			t.Helper()
			perturbQueueAssignment(t, dataDir)
			perturbQueueOrderKeys(t, dataDir)
			perturbActiveMilestone(t, dataDir)
		}},
	}

	baseRoot := fixtures.CopySampleProject(t)
	baseReady := readyAnswers(t, baseRoot)
	baseWitness := queueWitness(t, baseRoot)

	// No part of the witness witnesses anything unless it is stable across two
	// unperturbed copies: one that carried the store's temporary path would
	// differ on every case and wave every perturbation through, landed or not.
	if got := queueWitness(t, fixtures.CopySampleProject(t)); got != baseWitness {
		t.Fatalf("part of the queue-axis witness differs between two unperturbed fixture copies, so it cannot show a perturbation landed.\n=== one ===\n%s\n=== other ===\n%s",
			got.dump(), baseWitness.dump())
	}

	// The comparison is only worth making if the unperturbed answer actually
	// holds nibs the perturbations reach: tnib-e004 sits on a milestone queue
	// as shipped, and tnib-f019 is the unassigned root the assignment case
	// enqueues. With neither in the answer, the axis has nothing to leak into.
	// Reported rather than fatal, so the cases below still say what moved.
	for _, id := range []string{"tnib-e004", "tnib-f019"} {
		if !strings.Contains(baseReady[0], id) {
			t.Errorf("the unperturbed --ready answer does not list %s, so the comparisons below are weaker than they look:\n%s", id, baseReady[0])
		}
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := fixtures.CopySampleProject(t)
			tc.perturb(t, fixtures.DataPath(root))

			if got := queueWitness(t, root); !tc.moved(baseWitness, got) {
				t.Fatalf("the %s perturbation did not move %s, so it did not reach the derivation this case is about and the case proves nothing:\n%s",
					tc.name, tc.requires, got.dump())
			}

			for i, out := range readyAnswers(t, root) {
				// Byte-exact, deliberately: the leak this guard was proven
				// against reorders the rows and leaves the row SET identical,
				// so a set or sorted comparison here would pass on the very
				// mutation the guard exists to catch, taking all of it away.
				if out != baseReady[i] {
					t.Errorf("`nibs %s` moved when only the queue axis (%s) changed — --ready must answer feasibility alone.\n--- perturbed ---\n%s\n--- unperturbed ---\n%s",
						strings.Join(readyShapes[i], " "), tc.name, out, baseReady[i])
				}
			}
		})
	}
}

// readyAnswers runs every shape in readyShapes against the fixture at root,
// returning the outputs in the same order.
func readyAnswers(t *testing.T, root string) []string {
	t.Helper()
	outs := make([]string, 0, len(readyShapes))
	for _, shape := range readyShapes {
		outs = append(outs, runReadyBlindCmd(t, root, shape...))
	}
	return outs
}

// queueWitness renders the three queue-axis consumers a case shows its
// perturbation reached: `next`, which walks the active milestone's queue,
// `roadmap`, which renders each milestone's membership, and `context`, whose
// overview lists every milestone that is !IsClosedStatus. No one of them sees
// the whole axis — dropping the only in-progress milestone moves `next` while
// `roadmap` and `context` stay byte-identical, and a reorder behind the queue's
// head moves `roadmap` alone. `context` earns its place for a different reason
// than the other two: its milestone list can be read for a named fact instead
// of compared whole, which is the only way to witness the closed-status
// boundary (see milestoneLeftContext). Links are off so the `roadmap` rendering
// carries no store path, which would differ between fixture copies for reasons
// that have nothing to do with the queue.
func queueWitness(t *testing.T, root string) queueSight {
	t.Helper()
	return queueSight{
		next:    runReadyBlindCmd(t, root, "next"),
		roadmap: runReadyBlindCmd(t, root, "roadmap", "--no-links"),
		context: runReadyBlindCmd(t, root, "context"),
	}
}

// queueSight is what the three queue-axis consumers render. The parts stay
// apart so a case can require the one its perturbation actually speaks to, and
// so a loss in one part cannot be masked by a gain in another.
type queueSight struct {
	next    string
	roadmap string
	context string
}

// dump labels every part for a failure message, so a gate that reads only one
// of them still fails on evidence a reader can act on.
func (q queueSight) dump() string {
	return fmt.Sprintf("--- next ---\n%s\n--- roadmap ---\n%s\n--- context ---\n%s", q.next, q.roadmap, q.context)
}

// nextOrRoadmapMoved accepts a perturbation that reached either rendering of
// the axis itself, which is all the queue-shape cases need: they ask whether
// --ready moved, not which derivation answered. `context` is deliberately not a
// third alternative, and the reason is what it ADDS, not what it is sensitive
// to: measured on a bare `milestone_order` rewrite, `roadmap` moves while
// `context` stays identical, so `roadmap` already carries every axis edit
// `context` could witness. A third alternative that sees less of the axis can
// only widen the gate — both also move for a member's plain `status:` edit —
// so admitting it would trade strictness for nothing.
func nextOrRoadmapMoved(base, got queueSight) bool {
	return got.next != base.next || got.roadmap != base.roadmap
}

// nextMoved holds no-active-milestone to `nibs next` alone, the one consumer of
// graph.ActiveMilestone. `roadmap` re-renders for any edit on the axis, the
// derivation's own answer included or not — rewriting tnib-e005's
// milestone_order moves it while `next` stays byte-identical — so a milestone
// case that accepted either consumer could pass with the derivation it is about
// standing still.
func nextMoved(base, got queueSight) bool { return got.next != base.next }

// nextNamesM002 requires the derivation to have picked tnib-m002, not merely to
// have lost tnib-m001 — that loss alone is no-active-milestone's subject, and
// it is what the first of perturbActiveMilestone's two edits achieves on its
// own. `next` names tnib-m002 once it walks that milestone's queue, and never
// names it in the no-milestone fallback text, so this separates the two.
func nextNamesM002(base, got queueSight) bool {
	return got.next != base.next && strings.Contains(got.next, "tnib-m002")
}

// milestoneLeftContext holds milestone-closes to the derivation it is about:
// `nibs context` lists every milestone !IsClosedStatus, so the case has crossed
// the boundary only if tnib-m001 was listed and now is not. `next` cannot
// answer this — it keys on in-progress, so a merely un-started milestone moves
// it identically to a closed one — and a bare inequality on `context` cannot
// either, since that rendering also carries a per-milestone rollup which moves
// whenever a member's own status does, boundary or no boundary.
func milestoneLeftContext(base, got queueSight) bool {
	return strings.Contains(base.context, "tnib-m001") &&
		!strings.Contains(got.context, "tnib-m001")
}

// runReadyBlindCmd drives one command against the fixture store at root.
func runReadyBlindCmd(t *testing.T, root string, args ...string) string {
	t.Helper()
	resetReadyBlindFlags()
	out, err := runRootWith(t, append([]string{"--nibs-path", fixtures.NibsPath(root)}, args...)...)
	if err != nil {
		t.Fatalf("%v: %v", args, err)
	}
	return out
}

// resetReadyBlindFlags clears the package-level flag state the four commands
// this guard drives share, plus rootCmd's persistent flags — every invocation
// here passes --nibs-path, which names a temp store that is gone by the time
// any later test runs. rootCmd and its subcommands are singletons across the
// package's tests, so a flag left set — or merely left marked Changed — makes a
// later run see filters it was never given.
func resetReadyBlindFlags() {
	resetListFlags()
	resetNextFlags()
	resetContextFlags()
	resetRootPersistentFlags()
	roadmapJSON = false
	roadmapIncludeDone = false
	roadmapStatus = nil
	roadmapNoStatus = nil
	roadmapNoLinks = false
	roadmapLinkPrefix = ""
	roadmapCmd.Flags().Visit(func(f *pflag.Flag) {
		f.Changed = false
	})
}

// perturbQueueAssignment moves membership around the queue axis and touches
// nothing else: two members change queues, one leaves the axis entirely, and an
// unassigned root joins it.
func perturbQueueAssignment(t *testing.T, dataDir string) {
	t.Helper()
	setNibKey(t, dataDir, "tnib-e003", "milestone", "tnib-m002")
	setNibKey(t, dataDir, "tnib-e004", "milestone", "tnib-m002")
	dropNibKey(t, dataDir, "tnib-e001", "milestone")
	dropNibKey(t, dataDir, "tnib-e001", "milestone_order")
	setNibKey(t, dataDir, "tnib-f019", "milestone", "tnib-m001")
	setNibKey(t, dataDir, "tnib-f019", "milestone_order", "e")
}

// perturbQueueOrderKeys rewrites queue keys into a different order and drops
// two outright, so members with no key of their own exist too. One of the drops
// is tnib-e004, which is IN the --ready answer: a key rewritten on a nib the
// answer never lists could not reach it, and the case would guard nothing.
//
// The three perturbations overlap on tnib-e004 alone, and there on different
// keys, so the whole-axis case can apply all three in sequence without one
// undoing another's edit.
func perturbQueueOrderKeys(t *testing.T, dataDir string) {
	t.Helper()
	setNibKey(t, dataDir, "tnib-e002", "milestone_order", "zz")
	setNibKey(t, dataDir, "tnib-e005", "milestone_order", "m")
	dropNibKey(t, dataDir, "tnib-e006", "milestone_order")
	dropNibKey(t, dataDir, "tnib-e004", "milestone_order")
}

// perturbActiveMilestone hands the active-milestone derivation to the other
// milestone. Both statuses stay inside the open, non-startable pair
// (draft/in-progress) because the question here is which milestone the
// derivation picks, and swapping two open statuses moves that and nothing
// else — no milestone can enter --ready's answer at either one. Crossing into a
// closed status is a different question, asked by perturbActiveMilestoneCloses.
func perturbActiveMilestone(t *testing.T, dataDir string) {
	t.Helper()
	setNibKey(t, dataDir, "tnib-m001", "status", "draft")
	setNibKey(t, dataDir, "tnib-m002", "status", "in-progress")
}

// perturbNoActiveMilestone leaves the derivation with no answer at all: no
// milestone is in progress, which is the shape `next` reports as a fallback.
func perturbNoActiveMilestone(t *testing.T, dataDir string) {
	t.Helper()
	setNibKey(t, dataDir, "tnib-m001", "status", "draft")
}

// perturbActiveMilestoneCloses closes the active milestone, crossing the
// closed-status boundary the other milestone cases stay clear of. That boundary
// is a second, independent derivation of "which milestone is active":
// graph.ActiveMilestone keys on `status == "in-progress"`, while `nibs context`
// selects every milestone !IsClosedStatus. A --ready that suppressed work under
// a closed milestone leaks through every case that stays on the open side.
// Closing one is safe for the same reason the open cases are: neither
// in-progress nor completed is startable (config.StartableStatusNames is
// `todo` alone), so tnib-m001 is outside --ready's answer at both ends of this
// rewrite. And no fixture nib is blocked_by a milestone, so closing one
// releases nothing into that answer either.
func perturbActiveMilestoneCloses(t *testing.T, dataDir string) {
	t.Helper()
	setNibKey(t, dataDir, "tnib-m001", "status", "completed")
}

// setNibKey writes `key: value` into a nib's front matter, replacing the
// existing line or inserting one ahead of the closing delimiter.
func setNibKey(t *testing.T, dataDir, id, key, value string) {
	t.Helper()
	path := nibFixtureFile(t, dataDir, id)
	lines, end := frontMatterLines(t, path)
	for i := 1; i < end; i++ {
		if strings.HasPrefix(lines[i], key+":") {
			lines[i] = key + ": " + value
			writeNibLines(t, path, lines)
			return
		}
	}
	writeNibLines(t, path, slices.Insert(lines, end, key+": "+value))
}

// dropNibKey removes a key from a nib's front matter. A missing key is fatal
// rather than a no-op: a perturbation that silently stops landing is how an
// oracle turns into decoration.
func dropNibKey(t *testing.T, dataDir, id, key string) {
	t.Helper()
	path := nibFixtureFile(t, dataDir, id)
	lines, end := frontMatterLines(t, path)
	for i := 1; i < end; i++ {
		if strings.HasPrefix(lines[i], key+":") {
			writeNibLines(t, path, slices.Delete(lines, i, i+1))
			return
		}
	}
	t.Fatalf("nib %s carries no %q key to drop — the fixture moved out from under this guard", id, key)
}

// nibFixtureFile returns the one file in dataDir holding the given nib. Fixture
// files are named {id}.md or {id}--{slug}.md.
func nibFixtureFile(t *testing.T, dataDir, id string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dataDir, id+"--*.md"))
	if err != nil {
		t.Fatalf("globbing for nib %s: %v", id, err)
	}
	plain := filepath.Join(dataDir, id+".md")
	if _, err := os.Stat(plain); err == nil {
		matches = append(matches, plain)
	}
	if len(matches) != 1 {
		t.Fatalf("looking for nib %s in %s: found %d files, want exactly 1", id, dataDir, len(matches))
	}
	return matches[0]
}

// frontMatterLines reads a nib file into lines and returns the index of the
// closing front-matter delimiter. Splitting on "\n" is exact here because
// .gitattributes pins every .md file in the tree to LF.
func frontMatterLines(t *testing.T, path string) ([]string, int) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 || lines[0] != "---" {
		t.Fatalf("%s does not open with a front-matter delimiter", path)
	}
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			return lines, i
		}
	}
	t.Fatalf("%s has no closing front-matter delimiter", path)
	return nil, 0
}

// writeNibLines writes lines back over a fixture nib file.
func writeNibLines(t *testing.T, path string, lines []string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
