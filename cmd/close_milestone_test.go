package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/testskip"
)

// releasingCloseReasons is the set decision 1.5 calls a completion: the close
// reasons that RELEASE the milestone's dependents (today completed and
// scrapped). Derived, never listed, so a vocabulary change moves the tests
// with it rather than leaving them asserting a retired name.
func releasingCloseReasons(t *testing.T) []string {
	t.Helper()
	names := config.Default().ReleasingStatusNames()
	if len(names) == 0 {
		t.Fatal("test setup: no releasing close reason declared, so these tests assert nothing")
	}
	return names
}

// holdingCloseReason is the other half: a closed status that does NOT release
// its dependents (today deferred), which decision 1.5 lets keep its queue.
func holdingCloseReason(t *testing.T) string {
	t.Helper()
	names := config.Default().HoldingStatusNames()
	if len(names) == 0 {
		t.Fatal("test setup: no holding close reason declared, so these tests assert nothing")
	}
	return names[0]
}

// setupMilestoneCloseTest builds a store from the given files and registers
// the resets every command these tests drive needs.
func setupMilestoneCloseTest(t *testing.T, files map[string]string) string {
	t.Helper()
	nibsDir := setupCloseTest(t, files)
	t.Cleanup(resetGetFlags)
	t.Cleanup(resetListFlags)
	return nibsDir
}

// setupMilestoneCloseFixture is the same for the sample fixture, whose queues
// are tnib-e001..e004 -> tnib-m001 (keys a..d, m001 in-progress) and
// tnib-e005/e006 -> tnib-m002 (keys a, b, m002 draft). Every one of those six
// members is open.
func setupMilestoneCloseFixture(t *testing.T) string {
	t.Helper()
	nibsDir := setupQueueCLITest(t)
	resetCloseFlags()
	t.Cleanup(resetCloseFlags)
	return nibsDir
}

func closeErrCode(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	var ce *output.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("error = %T, want *output.CodedError: %v", err, err)
	}
	return ce.Code
}

// milestoneOf reads a nib's stored assignment and queue key back off disk
// through the projection, so the assertions read what a user would see.
func milestoneOf(t *testing.T, nibsPath, id string) (milestone, key string) {
	t.Helper()
	ms, order, _ := axisFields(t, nibsPath, id)
	return ms, order
}

// etagOf reads a nib's etag back through the projection, which is where a
// caller obliged to supply --if-match gets one.
func etagOf(t *testing.T, nibsPath, id string) string {
	t.Helper()
	resetGetFlags()
	resetRootPersistentFlags()
	out, err := runRootWith(t, "--nibs-path", nibsPath, "get", id, "-f", "etag")
	if err != nil {
		t.Fatalf("get %s -f etag: %v", id, err)
	}
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(out), "etag:"))
}

func statusOf(t *testing.T, nibsPath, id string) string {
	t.Helper()
	resetGetFlags()
	resetRootPersistentFlags()
	out, err := runRootWith(t, "--nibs-path", nibsPath, "get", id, "-f", "status")
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(out), "status:"))
}

// TestCloseMilestoneRefusesWithOpenAssignedWork is decision 1.5's refusal: a
// close reason that releases the milestone's dependents cannot land while its
// queue still holds open work. The message has to say how much and how to
// proceed, or an agent's only recovery is to guess.
func TestCloseMilestoneRefusesWithOpenAssignedWork(t *testing.T) {
	for _, reason := range releasingCloseReasons(t) {
		t.Run(reason, func(t *testing.T) {
			nibsPath := setupMilestoneCloseFixture(t)

			withStdin(t, "Shipping the wave.\n")
			_, err := runRootWith(t, "--nibs-path", nibsPath,
				"close", "tnib-m001", "--as", reason, "--summary", "-")
			if code := closeErrCode(t, err); output.ExitCode(code) != output.ExitValidation {
				t.Errorf("exit = %d, want %d (validation)", output.ExitCode(code), output.ExitValidation)
			}

			msg := err.Error()
			// How much: the count, and the ids it could name.
			if !strings.Contains(msg, "4 open nibs are") {
				t.Errorf("refusal should count the open queue, got: %s", msg)
			}
			for _, id := range []string{"tnib-e001", "tnib-e002", "tnib-e003", "tnib-e004"} {
				if !strings.Contains(msg, id) {
					t.Errorf("refusal should name %s, got: %s", id, msg)
				}
			}
			// How to proceed: both escapes, and the holding reason that keeps
			// the queue.
			for _, want := range []string{"--move-open-to", "--unassign-open", "--as " + holdingCloseReason(t)} {
				if !strings.Contains(msg, want) {
					t.Errorf("refusal should offer %q, got: %s", want, msg)
				}
			}

			if got := statusOf(t, nibsPath, "tnib-m001"); got != "in-progress" {
				t.Errorf("refused close still wrote the milestone: status = %q", got)
			}
			if ms, key := milestoneOf(t, nibsPath, "tnib-e001"); ms != "tnib-m001" || key != "a" {
				t.Errorf("refused close disturbed the queue: tnib-e001 = (%q, %q), want (tnib-m001, a)", ms, key)
			}
		})
	}
}

// TestCloseMilestoneAsHoldingReasonKeepsTheQueue is the other half of 1.5: a
// reason that does not release its dependents parks the milestone, and parked
// work is coming back — so the queue is left exactly as it was.
func TestCloseMilestoneAsHoldingReasonKeepsTheQueue(t *testing.T) {
	nibsPath := setupMilestoneCloseFixture(t)
	holding := holdingCloseReason(t)

	withStdin(t, "Parked until the platform work lands.\n")
	if _, err := runRootWith(t, "--nibs-path", nibsPath,
		"close", "tnib-m001", "--as", holding, "--summary", "-"); err != nil {
		t.Fatalf("close --as %s should be allowed with an open queue: %v", holding, err)
	}

	if got := statusOf(t, nibsPath, "tnib-m001"); got != holding {
		t.Errorf("milestone status = %q, want %q", got, holding)
	}
	for id, key := range map[string]string{"tnib-e001": "a", "tnib-e002": "b", "tnib-e003": "c", "tnib-e004": "d"} {
		if ms, got := milestoneOf(t, nibsPath, id); ms != "tnib-m001" || got != key {
			t.Errorf("%s = (%q, %q) after a holding close, want (tnib-m001, %q)", id, ms, got, key)
		}
	}
}

// TestCloseMilestoneMovesOpenWorkToAnotherQueue pins --move-open-to: the open
// set leaves through the ordering engine, lands at the END of the target queue
// in the order it left, and keeps nothing of its old key.
func TestCloseMilestoneMovesOpenWorkToAnotherQueue(t *testing.T) {
	nibsPath := setupMilestoneCloseFixture(t)

	withStdin(t, "Wave one is done; the rest rolls forward.\n")
	if _, err := runRootWith(t, "--nibs-path", nibsPath,
		"close", "tnib-m001", "--move-open-to", "tnib-m002", "--summary", "-"); err != nil {
		t.Fatalf("close --move-open-to: %v", err)
	}

	if got := statusOf(t, nibsPath, "tnib-m001"); got != closeDefaultStatus() {
		t.Errorf("milestone status = %q, want %q", got, closeDefaultStatus())
	}

	// Appended last, after the target's own two members, in source order.
	want := []string{"tnib-e005", "tnib-e006", "tnib-e001", "tnib-e002", "tnib-e003", "tnib-e004"}
	if got := queueIDs(t, nibsPath, "tnib-m002"); !slices.Equal(got, want) {
		t.Errorf("tnib-m002 queue = %v, want %v", got, want)
	}
	if got := queueIDs(t, nibsPath, "tnib-m001"); len(got) != 0 {
		t.Errorf("tnib-m001 queue = %v, want empty", got)
	}
	// Nothing of the old queue key survives: every moved nib sorts after the
	// target's last pre-existing key.
	for _, id := range []string{"tnib-e001", "tnib-e002", "tnib-e003", "tnib-e004"} {
		ms, key := milestoneOf(t, nibsPath, id)
		if ms != "tnib-m002" {
			t.Errorf("%s milestone = %q, want tnib-m002", id, ms)
		}
		if key <= "b" {
			t.Errorf("%s queue key = %q, want a key after the target's last (b)", id, key)
		}
	}
}

// TestCloseMilestoneUnassignsOpenWork pins --unassign-open: the assignment and
// the queue key both go, since a nib in no queue has no position in one.
func TestCloseMilestoneUnassignsOpenWork(t *testing.T) {
	nibsPath := setupMilestoneCloseFixture(t)

	withStdin(t, "Wave one is done; the rest goes back to the backlog.\n")
	if _, err := runRootWith(t, "--nibs-path", nibsPath,
		"close", "tnib-m001", "--unassign-open", "--summary", "-"); err != nil {
		t.Fatalf("close --unassign-open: %v", err)
	}

	if got := statusOf(t, nibsPath, "tnib-m001"); got != closeDefaultStatus() {
		t.Errorf("milestone status = %q, want %q", got, closeDefaultStatus())
	}
	for _, id := range []string{"tnib-e001", "tnib-e002", "tnib-e003", "tnib-e004"} {
		ms, key := milestoneOf(t, nibsPath, id)
		if ms != "" || key != "" {
			t.Errorf("%s = (%q, %q) after --unassign-open, want both empty", id, ms, key)
		}
	}
	if got := queueIDs(t, nibsPath, "tnib-m001"); len(got) != 0 {
		t.Errorf("tnib-m001 queue = %v, want empty", got)
	}
}

// TestCloseMilestoneKeepsClosedMembersAssigned: the escapes act on the OPEN
// set only. A member that already closed stays in the milestone's queue,
// because that is the record of what the milestone delivered.
func TestCloseMilestoneKeepsClosedMembersAssigned(t *testing.T) {
	nibsPath := setupMilestoneCloseTest(t, milestoneStoreFiles(map[string]string{
		"done": "completed",
		"open": "todo",
	}))

	withStdin(t, "Wave over.\n")
	if _, err := runRootWith(t, "--nibs-path", nibsPath,
		"close", "ms-1", "--unassign-open", "--summary", "-"); err != nil {
		t.Fatalf("close --unassign-open: %v", err)
	}

	if ms, _ := milestoneOf(t, nibsPath, "mem-done"); ms != "ms-1" {
		t.Errorf("closed member lost its assignment: milestone = %q, want ms-1", ms)
	}
	if ms, key := milestoneOf(t, nibsPath, "mem-open"); ms != "" || key != "" {
		t.Errorf("open member = (%q, %q), want both empty", ms, key)
	}
}

// TestCloseMilestoneReadsTheRoleVocabularyForOpen: "open" here is what it is
// everywhere else — config.IsClosedStatus. A holding member (deferred) is
// CLOSED, so it does not hold the milestone open, even though it is coming
// back.
func TestCloseMilestoneReadsTheRoleVocabularyForOpen(t *testing.T) {
	holding := holdingCloseReason(t)
	nibsPath := setupMilestoneCloseTest(t, milestoneStoreFiles(map[string]string{
		"parked": holding,
	}))

	withStdin(t, "Wave over.\n")
	if _, err := runRootWith(t, "--nibs-path", nibsPath, "close", "ms-1", "--summary", "-"); err != nil {
		t.Fatalf("a queue holding only %s members should close: %v", holding, err)
	}
	if got := statusOf(t, nibsPath, "ms-1"); got != closeDefaultStatus() {
		t.Errorf("milestone status = %q, want %q", got, closeDefaultStatus())
	}
	if ms, _ := milestoneOf(t, nibsPath, "mem-parked"); ms != "ms-1" {
		t.Errorf("%s member lost its assignment: milestone = %q, want ms-1", holding, ms)
	}
}

// TestCloseMilestoneEscapesRefuseAnEmptyOpenSet: an escape with nothing to act
// on is a request that cannot be honored, and reporting success would be
// indistinguishable from a queue that really was drained.
func TestCloseMilestoneEscapesRefuseAnEmptyOpenSet(t *testing.T) {
	cases := map[string][]string{
		"move":     {"--move-open-to", "ms-2"},
		"unassign": {"--unassign-open"},
	}
	for name, flags := range cases {
		t.Run(name, func(t *testing.T) {
			nibsPath := setupMilestoneCloseTest(t, milestoneStoreFiles(map[string]string{
				"done": "completed",
			}))

			withStdin(t, "Wave over.\n")
			args := append([]string{"--nibs-path", nibsPath, "close", "ms-1", "--summary", "-"}, flags...)
			_, err := runRootWith(t, args...)
			if code := closeErrCode(t, err); output.ExitCode(code) != output.ExitValidation {
				t.Errorf("exit = %d, want %d (validation)", output.ExitCode(code), output.ExitValidation)
			}
			if !strings.Contains(err.Error(), "no open nib is assigned") {
				t.Errorf("refusal should say the set is empty, got: %s", err)
			}
			if got := statusOf(t, nibsPath, "ms-1"); got != "in-progress" {
				t.Errorf("refused escape still closed the milestone: status = %q", got)
			}
		})
	}
}

// TestCloseMilestoneMoveTargetIsValidated covers every way --move-open-to can
// name the wrong thing. The closed-target rule is the interesting one: a
// RELEASING target is refused (moving open work there would recreate the very
// state this gate prevents), while a HOLDING one is accepted, because decision
// 1.5 gives a parked milestone its queue.
func TestCloseMilestoneMoveTargetIsValidated(t *testing.T) {
	holding := holdingCloseReason(t)
	cases := []struct {
		name    string
		target  string
		wantErr string
	}{
		{name: "unknown", target: "ms-ghost", wantErr: "milestone nib not found"},
		{name: "not a milestone", target: "mem-open", wantErr: "not milestone"},
		{name: "the milestone being closed", target: "ms-1", wantErr: "is the milestone being closed"},
		{name: "a released target", target: "ms-done", wantErr: "already closed as"},
		{name: "a parked target", target: "ms-parked", wantErr: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := milestoneStoreFiles(map[string]string{"open": "todo"})
			files["ms-done--finished.md"] = "---\nversion: 2\ntitle: Finished\nstatus: completed\ntype: milestone\norder: c\n---\n\nBody.\n"
			files["ms-parked--parked.md"] = "---\nversion: 2\ntitle: Parked\nstatus: " + holding + "\ntype: milestone\norder: d\n---\n\nBody.\n"
			nibsPath := setupMilestoneCloseTest(t, files)

			withStdin(t, "Wave over.\n")
			_, err := runRootWith(t, "--nibs-path", nibsPath,
				"close", "ms-1", "--move-open-to", tc.target, "--summary", "-")

			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("--move-open-to %s should be accepted: %v", tc.target, err)
				}
				if ms, _ := milestoneOf(t, nibsPath, "mem-open"); ms != tc.target {
					t.Errorf("mem-open milestone = %q, want %q", ms, tc.target)
				}
				return
			}
			if code := closeErrCode(t, err); output.ExitCode(code) != output.ExitValidation {
				t.Errorf("exit = %d, want %d (validation)", output.ExitCode(code), output.ExitValidation)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tc.wantErr)
			}
			if ms, key := milestoneOf(t, nibsPath, "mem-open"); ms != "ms-1" || key == "" {
				t.Errorf("refused move still touched the queue: mem-open = (%q, %q)", ms, key)
			}
			if got := statusOf(t, nibsPath, "ms-1"); got != "in-progress" {
				t.Errorf("refused move still closed the milestone: status = %q", got)
			}
		})
	}
}

// TestCloseQueueEscapesApplyToMilestonesOnly: the two flags dispose of a
// QUEUE, and only a milestone has one. On anything else they would be a silent
// no-op, which is the one answer that cannot be told apart from success.
func TestCloseQueueEscapesApplyToMilestonesOnly(t *testing.T) {
	cases := map[string][]string{
		"move":     {"--move-open-to", "ms-1"},
		"unassign": {"--unassign-open"},
	}
	for name, flags := range cases {
		t.Run(name, func(t *testing.T) {
			nibsPath := setupMilestoneCloseTest(t, milestoneStoreFiles(map[string]string{"open": "todo"}))

			withStdin(t, "Done.\n")
			args := append([]string{"--nibs-path", nibsPath, "close", "mem-open", "--summary", "-"}, flags...)
			_, err := runRootWith(t, args...)
			if code := closeErrCode(t, err); output.ExitCode(code) != output.ExitValidation {
				t.Errorf("exit = %d, want %d (validation)", output.ExitCode(code), output.ExitValidation)
			}
			if !strings.Contains(err.Error(), "is a task") {
				t.Errorf("refusal should name the subject's type, got: %s", err)
			}
			if got := statusOf(t, nibsPath, "mem-open"); got != "todo" {
				t.Errorf("refused close still wrote the nib: status = %q", got)
			}
		})
	}
}

// TestCloseMilestoneEscapesAreMutuallyExclusive: two dispositions for one set
// is a contradiction, refused before anything is written.
func TestCloseMilestoneEscapesAreMutuallyExclusive(t *testing.T) {
	nibsPath := setupMilestoneCloseTest(t, milestoneStoreFiles(map[string]string{"open": "todo"}))

	withStdin(t, "Wave over.\n")
	_, err := runRootWith(t, "--nibs-path", nibsPath,
		"close", "ms-1", "--move-open-to", "ms-2", "--unassign-open", "--summary", "-")
	if err == nil {
		t.Fatal("--move-open-to with --unassign-open should be refused, got nil")
	}
	if got := statusOf(t, nibsPath, "ms-1"); got != "in-progress" {
		t.Errorf("refused close still wrote the milestone: status = %q", got)
	}
	if ms, _ := milestoneOf(t, nibsPath, "mem-open"); ms != "ms-1" {
		t.Errorf("refused close still moved the queue: mem-open milestone = %q", ms)
	}
}

// TestCloseMilestonePartialMoveLeavesTheMilestoneOpen pins the partial-failure
// policy. The batch writes one nib at a time (the bulkreorder precedent:
// pre-validate what can be pre-validated, then write sequentially, and let the
// landed writes stay). A refusal part way therefore has to say how far it got,
// which nib refused, and that the milestone was NOT closed.
//
// The refusal is a real one: assignment exclusivity (decision 1.2) forbids a
// nib and its ancestor both being assigned, and a hand-edited store can hold
// that shape already — `nibs check` names it rather than the write path
// refusing to read it.
//
// It is also DETERMINISTIC, and the message has to survive that. The
// exclusivity checks run only for a non-empty milestone and --move-open-to only
// ever reassigns, so the rerun the advice offers recomputes the same open set,
// stops at the same member and writes nothing — forever. The run below walks
// that: the rerun is a byte-for-byte repeat, and the remedy the message names
// is the one that actually clears the queue.
func TestCloseMilestonePartialMoveLeavesTheMilestoneOpen(t *testing.T) {
	nibsPath := setupMilestoneCloseTest(t, map[string]string{
		"ms-1--wave-one.md": "---\nversion: 2\ntitle: Wave One\nstatus: in-progress\ntype: milestone\norder: a\n---\n\nBody.\n",
		"ms-2--wave-two.md": "---\nversion: 2\ntitle: Wave Two\nstatus: todo\ntype: milestone\norder: b\n---\n\nBody.\n",
		// First in the queue, unencumbered: it moves.
		"mem-a--clean.md": "---\nversion: 2\ntitle: Clean\nstatus: todo\ntype: task\nmilestone: ms-1\nmilestone_order: a\n---\n\nBody.\n",
		// Second: an epic whose child carries its own assignment, which is the
		// conflict the assignment write path refuses.
		"mem-b--conflicted.md": "---\nversion: 2\ntitle: Conflicted\nstatus: todo\ntype: epic\nmilestone: ms-1\nmilestone_order: b\n---\n\nBody.\n",
		"mem-c--child.md":      "---\nversion: 2\ntitle: Child\nstatus: todo\ntype: task\nparent: mem-b\nmilestone: ms-1\nmilestone_order: c\n---\n\nBody.\n",
	})

	withStdin(t, "Wave over.\n")
	_, err := runRootWith(t, "--nibs-path", nibsPath,
		"close", "ms-1", "--move-open-to", "ms-2", "--summary", "-")
	if err == nil {
		t.Fatal("a mid-batch refusal should fail the command, got nil")
	}
	msg := err.Error()
	for _, want := range []string{
		"moved 1 of 3", "mem-b", "was NOT closed", "rerun",
		// The rerun is not the repair for a refusal that repeats, and the
		// message must not pretend otherwise: it names the disposition that
		// does work here, which skips the exclusivity check entirely.
		"--unassign-open",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("partial-failure message should contain %q, got: %s", want, msg)
		}
	}

	if got := statusOf(t, nibsPath, "ms-1"); got != "in-progress" {
		t.Errorf("milestone status = %q, want it left open by the partial failure", got)
	}
	if ms, _ := milestoneOf(t, nibsPath, "mem-a"); ms != "ms-2" {
		t.Errorf("the write that landed should be persisted: mem-a milestone = %q, want ms-2", ms)
	}
	if ms, _ := milestoneOf(t, nibsPath, "mem-b"); ms != "ms-1" {
		t.Errorf("mem-b milestone = %q, want ms-1 (its write was refused)", ms)
	}

	// The rerun the message offers first: it makes zero writes and stops at the
	// same member, which is why the advice cannot end there.
	resetCloseFlags()
	resetRootPersistentFlags()
	withStdin(t, "Wave over.\n")
	_, rerunErr := runRootWith(t, "--nibs-path", nibsPath,
		"close", "ms-1", "--move-open-to", "ms-2", "--summary", "-")
	if rerunErr == nil {
		t.Fatal("the rerun meets the same deterministic refusal, got nil")
	}
	if !strings.Contains(rerunErr.Error(), "moved 0 of 2") {
		t.Errorf("the rerun should stop at mem-b having written nothing, got: %s", rerunErr)
	}

	// The remedy the message names, on the state the message describes.
	resetCloseFlags()
	resetRootPersistentFlags()
	withStdin(t, "Wave over.\n")
	if _, unassignErr := runRootWith(t, "--nibs-path", nibsPath,
		"close", "ms-1", "--unassign-open", "--summary", "-"); unassignErr != nil {
		t.Fatalf("the remedy the message prescribes has to work: %v", unassignErr)
	}
	if got := statusOf(t, nibsPath, "ms-1"); got != closeDefaultStatus() {
		t.Errorf("milestone status = %q, want %q after the prescribed remedy", got, closeDefaultStatus())
	}
	for _, id := range []string{"mem-b", "mem-c"} {
		if ms, key := milestoneOf(t, nibsPath, id); ms != "" || key != "" {
			t.Errorf("%s = (%q, %q) after --unassign-open, want both empty", id, ms, key)
		}
	}
}

// TestCloseKeyDecisionsFlowToTheAssignedMilestone is decision 1.6's second
// half: a nib with NO parent flows its Key Decisions to the milestone it is
// assigned to. Current Focus is deliberately not written there — see the
// gating comment in close.go.
func TestCloseKeyDecisionsFlowToTheAssignedMilestone(t *testing.T) {
	nibsDir := setupMilestoneCloseTest(t, map[string]string{
		"ms-1--wave-one.md": "---\nversion: 2\ntitle: Wave One\nstatus: in-progress\ntype: milestone\norder: a\ncreated_at: 2026-01-02T03:04:05Z\nupdated_at: 2026-01-02T03:04:05Z\n---\n\nThe wave.\n",
		"mem-a--rooted.md": "---\nversion: 2\ntitle: Rooted\nstatus: in-progress\ntype: task\nmilestone: ms-1\nmilestone_order: a\n---\n\n" +
			"## Key Decisions\n\nChose fractional keys over integer positions.\n",
	})

	withStdin(t, "Landed the queue keys.\n")
	if _, err := runRootWith(t, "--nibs-path", nibsDir, "close", "mem-a", "--summary", "-"); err != nil {
		t.Fatalf("close: %v", err)
	}

	milestone := readNibFile(t, nibsDir, "ms-1--wave-one.md")
	if !strings.Contains(milestone, "Chose fractional keys over integer positions.") {
		t.Errorf("the milestone should carry the closed nib's Key Decisions, got:\n%s", milestone)
	}
	if strings.Contains(milestone, "Current Focus") {
		t.Errorf("the milestone path must not write Current Focus, got:\n%s", milestone)
	}
}

// TestCloseKeyDecisionsPreferTheParentOverTheMilestone: parent wins when both
// exist. A nib is PART OF its parent and only PLANNED FOR its milestone, and
// the decomposition is where a reader looks for why a piece of it closed.
func TestCloseKeyDecisionsPreferTheParentOverTheMilestone(t *testing.T) {
	nibsDir := setupMilestoneCloseTest(t, map[string]string{
		"ms-1--wave-one.md":   "---\nversion: 2\ntitle: Wave One\nstatus: in-progress\ntype: milestone\norder: a\n---\n\nThe wave.\n",
		"epic-1--the-epic.md": "---\nversion: 2\ntitle: The Epic\nstatus: in-progress\ntype: epic\nmilestone: ms-1\nmilestone_order: a\ncreated_at: 2026-01-02T03:04:05Z\nupdated_at: 2026-01-02T03:04:05Z\n---\n\nThe epic.\n",
		"mem-a--child.md": "---\nversion: 2\ntitle: Child\nstatus: in-progress\ntype: task\nparent: epic-1\n---\n\n" +
			"## Key Decisions\n\nUsed a fractional key.\n",
	})

	withStdin(t, "Done.\n")
	if _, err := runRootWith(t, "--nibs-path", nibsDir, "close", "mem-a", "--summary", "-"); err != nil {
		t.Fatalf("close: %v", err)
	}

	epic := readNibFile(t, nibsDir, "epic-1--the-epic.md")
	if !strings.Contains(epic, "Used a fractional key.") {
		t.Errorf("the parent should carry the Key Decisions, got:\n%s", epic)
	}
	if !strings.Contains(epic, "Current Focus") {
		t.Errorf("the parent path still rewrites Current Focus, got:\n%s", epic)
	}
	milestone := readNibFile(t, nibsDir, "ms-1--wave-one.md")
	if strings.Contains(milestone, "Used a fractional key.") {
		t.Errorf("the milestone should not also receive them, got:\n%s", milestone)
	}
}

// TestCloseKeyDecisionsIgnoreAnUnresolvableAssignment: the assignment is read
// RESOLVED, the way every membership surface reads it, so an id naming no nib
// — or naming something that is not a milestone — flows the record nowhere
// rather than to a target the queue itself does not recognize.
func TestCloseKeyDecisionsIgnoreAnUnresolvableAssignment(t *testing.T) {
	nibsDir := setupMilestoneCloseTest(t, map[string]string{
		"epic-1--not-a-milestone.md": "---\nversion: 2\ntitle: Not A Milestone\nstatus: in-progress\ntype: epic\ncreated_at: 2026-01-02T03:04:05Z\nupdated_at: 2026-01-02T03:04:05Z\n---\n\nThe epic.\n",
		"mem-a--rooted.md": "---\nversion: 2\ntitle: Rooted\nstatus: in-progress\ntype: task\nmilestone: epic-1\n---\n\n" +
			"## Key Decisions\n\nShould stay put.\n",
	})

	withStdin(t, "Done.\n")
	if _, err := runRootWith(t, "--nibs-path", nibsDir, "close", "mem-a", "--summary", "-"); err != nil {
		t.Fatalf("close: %v", err)
	}

	epic := readNibFile(t, nibsDir, "epic-1--not-a-milestone.md")
	if strings.Contains(epic, "Should stay put.") {
		t.Errorf("a non-milestone assignment target must not receive the record, got:\n%s", epic)
	}
}

// milestoneStoreFiles builds a two-milestone store (ms-1 in-progress, ms-2
// todo) plus one member per entry of the given suffix->status map, each
// assigned to ms-1 and keyed in the map's sorted order.
func milestoneStoreFiles(members map[string]string) map[string]string {
	files := map[string]string{
		"ms-1--wave-one.md": "---\nversion: 2\ntitle: Wave One\nstatus: in-progress\ntype: milestone\norder: a\n---\n\nBody.\n",
		"ms-2--wave-two.md": "---\nversion: 2\ntitle: Wave Two\nstatus: todo\ntype: milestone\norder: b\n---\n\nBody.\n",
	}
	keys := make([]string, 0, len(members))
	for suffix := range members {
		keys = append(keys, suffix)
	}
	slices.Sort(keys)
	for i, suffix := range keys {
		files["mem-"+suffix+"--member.md"] = "---\nversion: 2\ntitle: Member " + suffix +
			"\nstatus: " + members[suffix] + "\ntype: task\nmilestone: ms-1\nmilestone_order: " +
			string(rune('a'+i)) + "\n---\n\nBody.\n"
	}
	return files
}

// stampedStoreFiles gives every file created_at/updated_at, which is what makes
// a nib's in-memory etag equal its on-disk one — and so what lets a test hand
// --if-match a value it read back through `get`. A file authored without them
// is stamped in memory at load, and the two diverge until it is next written.
func stampedStoreFiles(files map[string]string) map[string]string {
	const stamp = "created_at: 2026-01-02T03:04:05Z\nupdated_at: 2026-01-02T03:04:05Z\n"
	for name, body := range files {
		files[name] = strings.Replace(body, "\n---\n\n", "\n"+stamp+"---\n\n", 1)
	}
	return files
}

// TestCloseMilestoneStaleIfMatchRefusesBeforeTheQueueMoves: --if-match still
// protects the close SUBJECT only, but a disposition writes OTHER nibs' files
// before the subject is touched — so a close that is going to be refused for a
// stale etag has to be refused before the queue is drained.
func TestCloseMilestoneStaleIfMatchRefusesBeforeTheQueueMoves(t *testing.T) {
	nibsPath := setupMilestoneCloseTest(t, milestoneStoreFiles(map[string]string{"open": "todo"}))

	withStdin(t, "Wave over.\n")
	_, err := runRootWith(t, "--nibs-path", nibsPath,
		"close", "ms-1", "--unassign-open", "--if-match", "stale-etag", "--summary", "-")
	if code := closeErrCode(t, err); output.ExitCode(code) != output.ExitConflict {
		t.Errorf("exit = %d, want %d (conflict)", output.ExitCode(code), output.ExitConflict)
	}
	if ms, key := milestoneOf(t, nibsPath, "mem-open"); ms != "ms-1" || key == "" {
		t.Errorf("a refused close must not have drained the queue: mem-open = (%q, %q)", ms, key)
	}
	if got := statusOf(t, nibsPath, "ms-1"); got != "in-progress" {
		t.Errorf("refused close still wrote the milestone: status = %q", got)
	}
}

// TestCloseMilestoneInvalidSubjectRefusesBeforeTheQueueMoves: --if-match is not
// the only write-free guard the subject has to pass. A milestone whose OWN
// front matter the write path refuses — a retired enum value, or an assignment
// axis a milestone may not carry — is never going to be closed, so the queue
// must not be disposed of on the way to that refusal. Both escapes destroy the
// queue key (Orderer.Recalculate clears it on unassign and rewrites it on a
// move), so nothing recovers the ordering afterwards, and the rerun the
// partial-failure message prescribes finds an empty open set and is refused.
//
// Both shapes are ones the store deliberately LOADS — it warns and keeps
// reading rather than dropping the nib — so they are reachable from a
// hand-edited file, not just from a corrupt store.
func TestCloseMilestoneInvalidSubjectRefusesBeforeTheQueueMoves(t *testing.T) {
	subjects := map[string]string{
		// nibtypes.ValidateAxes: a milestone takes neither assignment axis.
		"area": "---\nversion: 2\ntitle: Wave One\nstatus: in-progress\ntype: milestone\norder: a\narea: platform\n---\n\nBody.\n",
		// Validator.ValidateEnums: a priority the vocabulary retired.
		"legacy priority": "---\nversion: 2\ntitle: Wave One\nstatus: in-progress\ntype: milestone\norder: a\npriority: urgent\n---\n\nBody.\n",
	}
	escapes := map[string][]string{
		"unassign-open": {"--unassign-open"},
		"move-open-to":  {"--move-open-to", "ms-2"},
	}
	for subject, frontMatter := range subjects {
		for escape, flags := range escapes {
			t.Run(subject+"/"+escape, func(t *testing.T) {
				files := milestoneStoreFiles(map[string]string{"open": "todo"})
				files["ms-1--wave-one.md"] = frontMatter
				nibsPath := setupMilestoneCloseTest(t, files)

				withStdin(t, "Wave over.\n")
				args := append([]string{"--nibs-path", nibsPath, "close", "ms-1", "--summary", "-"}, flags...)
				_, err := runRootWith(t, args...)
				if err == nil {
					t.Fatal("a milestone the write path refuses must not close, got nil")
				}
				if ms, key := milestoneOf(t, nibsPath, "mem-open"); ms != "ms-1" || key == "" {
					t.Errorf("a refused close must not have disposed of the queue: mem-open = (%q, %q), want (ms-1, non-empty)", ms, key)
				}
				if got := statusOf(t, nibsPath, "ms-1"); got != "in-progress" {
					t.Errorf("refused close still wrote the milestone: status = %q", got)
				}
			})
		}
	}
}

// TestCloseMilestoneEscapesWorkUnderRequireIfMatch: a disposition writes nibs
// the caller never named, so the caller has no way to supply THEIR etags —
// only the subject's. Under nibs.require_if_match every write without one is
// refused, which would make both escapes dead ends and leave a milestone
// holding open assigned work with no way to close as a releasing reason at
// all (the refusal even advising a rerun that can never help). close derives
// each member's etag internally, from the member AS LOADED — the same source
// the Key-Decisions flow-up reads for its recipient — so the token is a real
// precondition and not a value read back off the file it is about to overwrite
// (see TestCloseMilestoneDispositionRefusesADivergedMember).
func TestCloseMilestoneEscapesWorkUnderRequireIfMatch(t *testing.T) {
	cases := map[string][]string{
		"move":     {"--move-open-to", "ms-2"},
		"unassign": {"--unassign-open"},
	}
	for name, flags := range cases {
		t.Run(name, func(t *testing.T) {
			nibsPath := setupMilestoneCloseTest(t,
				stampedStoreFiles(milestoneStoreFiles(map[string]string{"open": "todo"})))
			if err := os.WriteFile(filepath.Join(nibsPath, "config.yml"),
				[]byte("nibs:\n  require_if_match: true\n"), 0644); err != nil {
				t.Fatal(err)
			}

			withStdin(t, "Wave over.\n")
			args := append([]string{"--nibs-path", nibsPath, "close", "ms-1",
				"--if-match", etagOf(t, nibsPath, "ms-1"), "--summary", "-"}, flags...)
			if _, err := runRootWith(t, args...); err != nil {
				t.Fatalf("close %v under require_if_match: %v", flags, err)
			}

			if got := statusOf(t, nibsPath, "ms-1"); got != closeDefaultStatus() {
				t.Errorf("milestone status = %q, want %q", got, closeDefaultStatus())
			}
			wantMilestone := ""
			if name == "move" {
				wantMilestone = "ms-2"
			}
			if ms, _ := milestoneOf(t, nibsPath, "mem-open"); ms != wantMilestone {
				t.Errorf("mem-open milestone = %q, want %q", ms, wantMilestone)
			}
		})
	}
}

// TestCloseMilestoneReportsTheDisposition: a command that rewrites N files the
// caller did not name has to say so. In --json it rides the same "warnings"
// array every other card-plus-note surface uses.
func TestCloseMilestoneReportsTheDisposition(t *testing.T) {
	nibsPath := setupMilestoneCloseTest(t, milestoneStoreFiles(map[string]string{"open": "todo"}))

	withStdin(t, "Wave over.\n")
	out, err := runRootWith(t, "--nibs-path", nibsPath,
		"close", "ms-1", "--move-open-to", "ms-2", "--summary", "-", "--json")
	if err != nil {
		t.Fatalf("close --move-open-to --json: %v", err)
	}
	var env struct {
		Warnings []string `json:"warnings"`
	}
	if jsonErr := json.Unmarshal([]byte(out), &env); jsonErr != nil {
		t.Fatalf("unmarshal close output: %v\nraw: %s", jsonErr, out)
	}
	if len(env.Warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one disposition notice", env.Warnings)
	}
	for _, want := range []string{"moved 1 open nib", "ms-1", "ms-2", "mem-open"} {
		if !strings.Contains(env.Warnings[0], want) {
			t.Errorf("notice %q should contain %q", env.Warnings[0], want)
		}
	}
}

// TestCloseMilestoneMoveTargetCannotBeEmpty: --move-open-to given without a
// value is a request that names no destination. Reading the bound string alone
// would let it pass as "flag omitted" and close the milestone with its queue
// silently intact — the guard asks Cobra whether the flag was GIVEN instead.
func TestCloseMilestoneMoveTargetCannotBeEmpty(t *testing.T) {
	nibsPath := setupMilestoneCloseTest(t, milestoneStoreFiles(map[string]string{"open": "todo"}))

	withStdin(t, "Wave over.\n")
	_, err := runRootWith(t, "--nibs-path", nibsPath,
		"close", "ms-1", "--move-open-to", "", "--summary", "-")
	if code := closeErrCode(t, err); output.ExitCode(code) != output.ExitValidation {
		t.Errorf("exit = %d, want %d (validation)", output.ExitCode(code), output.ExitValidation)
	}
	if !strings.Contains(err.Error(), "--move-open-to requires") {
		t.Errorf("refusal should say the flag needs a milestone id, got: %s", err)
	}
	if got := statusOf(t, nibsPath, "ms-1"); got != "in-progress" {
		t.Errorf("refused close still wrote the milestone: status = %q", got)
	}
}

// TestCloseMilestoneDispositionRefusesADivergedMember is the lost-update guard
// for the queue dispositions, and it is the reason each member's if-match is
// derived from the nib as LOADED rather than from its current on-disk bytes.
//
// `nibs close` is a single-shot CLI with no file watcher (StartWatching is
// wired only from `nibs serve`), so the store's in-memory picture is fixed at
// load and the divergence window spans the WHOLE command — from process start
// to each member's turn in the disposition loop. Any concurrent actor writing a
// queue member in that window (a second nibs process, a hand-edit, a git
// operation in .nibs/) must have its content refused, not silently overwritten
// by the picture this process loaded.
//
// Deriving the token from Reader.CurrentETag instead makes the precondition a
// tautology — both sides of the comparison resolve to the same
// computeStoredETag call — so the guard passes while the write clobbers the
// divergent file. This test fails in exactly that shape: the edit disappears.
func TestCloseMilestoneDispositionRefusesADivergedMember(t *testing.T) {
	const memberFile = "mem-open--member.md"
	const diverged = "---\nversion: 2\ntitle: Member open\nstatus: todo\ntype: task\n" +
		"milestone: ms-1\nmilestone_order: a\n" +
		"created_at: 2026-01-02T03:04:05Z\nupdated_at: 2026-01-02T03:04:05Z\n---\n\n" +
		"Rewritten by another process while this close was running.\n"

	escapes := map[string]func(t *testing.T, app *App, resolver *graph.Resolver, subject *nib.Nib) error{
		"unassign-open": func(t *testing.T, app *App, resolver *graph.Resolver, subject *nib.Nib) error {
			t.Helper()
			_, err := closeUnassignOpenWork(context.Background(), app, resolver, subject, []string{"mem-open"}, 1)
			return err
		},
		"move-open-to": func(t *testing.T, app *App, resolver *graph.Resolver, subject *nib.Nib) error {
			t.Helper()
			closeMoveOpenTo = "ms-2"
			_, err := closeMoveOpenWork(context.Background(), app, resolver, subject, []string{"mem-open"}, 1)
			return err
		},
	}

	for name, run := range escapes {
		t.Run(name, func(t *testing.T) {
			nibsPath := setupMilestoneCloseTest(t,
				stampedStoreFiles(milestoneStoreFiles(map[string]string{"open": "todo"})))

			// Load the store the way the command does, THEN diverge the member's
			// file — the same order a concurrent writer produces.
			core := nibcore.New(nibsPath, config.Default())
			if err := core.Load(); err != nil {
				t.Fatalf("load store: %v", err)
			}
			app := &App{Core: core}
			resolver := app.newResolver()
			subject, ok := resolver.Reader.GetSnapshot("ms-1")
			if !ok {
				t.Fatal("test setup: ms-1 is not in the store")
			}
			if err := os.WriteFile(dataPath(nibsPath, memberFile), []byte(diverged), 0644); err != nil {
				t.Fatal(err)
			}

			err := run(t, app, resolver, subject)
			if err == nil {
				t.Fatal("a member whose file diverged on disk must not be rewritten, got nil")
			}
			if code := closeErrCode(t, err); output.ExitCode(code) != output.ExitConflict {
				t.Errorf("exit = %d, want %d (conflict): %v", output.ExitCode(code), output.ExitConflict, err)
			}
			if got := readNibFile(t, nibsPath, memberFile); got != diverged {
				t.Errorf("the divergent content was clobbered; file is now:\n%s", got)
			}
		})
	}
}

// unwritableDataDirT makes the store's data/ directory unwritable, so a write
// landing directly in it fails while a write into a SUBDIRECTORY of it still
// succeeds — the split a disposition-then-subject failure needs. It skips where
// the mode does not bite (root, Windows).
func unwritableDataDirT(t *testing.T, nibsPath string) {
	t.Helper()
	dataDir := storeDataDir(nibsPath)
	if err := os.Chmod(dataDir, 0o555); err != nil {
		testskip.Unavailable(t, testskip.UnwritablePaths, "os.Chmod(data, 0555): %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dataDir, 0o755) })
	if probe, err := os.CreateTemp(dataDir, "probe"); err == nil {
		_ = probe.Close()
		_ = os.Remove(probe.Name())
		testskip.Unavailable(t, testskip.UnwritablePaths,
			"this process writes into a mode-555 directory anyway (running as root?)")
	}
}

// TestCloseMilestoneReportsADispositionTheSubjectWriteThenLoses pins the one
// exit where a disposition is already durable and the command still fails: the
// escapes rewrite every open member, and then the milestone's OWN write does
// not land.
//
// Nothing about that state is inferable from the write error alone. N nibs the
// caller never named have lost their assignment and their queue key — and the
// key is gone for good, since Orderer.Recalculate issues a fresh one on any
// later reassignment — while the milestone named on the command line is still
// open. Worse, the obvious retry is the one command that cannot work: the queue
// is drained, so rerunning WITH the escape meets an empty open set and is
// refused for it. So the failure has to report what was written and name the
// flag to DROP.
//
// closePreValidateSubject cannot pre-empt this: it runs every write-FREE guard,
// and this is write I/O.
func TestCloseMilestoneReportsADispositionTheSubjectWriteThenLoses(t *testing.T) {
	nibsPath := setupMilestoneCloseTest(t, map[string]string{
		"ms-1--wave-one.md": "---\nversion: 2\ntitle: Wave One\nstatus: in-progress\ntype: milestone\norder: a\n---\n\nBody.\n",
	})
	// The member lives one level down, so its own write still succeeds once
	// data/ itself is closed to new files.
	if err := os.MkdirAll(dataPath(nibsPath, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataPath(nibsPath, "sub", "mem-open--member.md"),
		[]byte("---\nversion: 2\ntitle: Member open\nstatus: todo\ntype: task\nmilestone: ms-1\nmilestone_order: a\n---\n\nBody.\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	unwritableDataDirT(t, nibsPath)

	withStdin(t, "Wave over.\n")
	_, err := runRootWith(t, "--nibs-path", nibsPath,
		"close", "ms-1", "--unassign-open", "--summary", "-")
	if err == nil {
		t.Fatal("the subject's write cannot land in an unwritable directory, got nil")
	}

	msg := err.Error()
	for _, want := range []string{
		"unassigned 1 open nib", // what the disposition did
		"mem-open",              // to which nib
		"was NOT closed",        // and what did not happen
		"rerun WITHOUT --unassign-open",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("the failure should report the disposition; %q missing from: %s", want, msg)
		}
	}

	if ms, key := milestoneOf(t, nibsPath, "mem-open"); ms != "" || key != "" {
		t.Errorf("mem-open = (%q, %q), want both empty — the disposition write is persisted", ms, key)
	}
	if got := statusOf(t, nibsPath, "ms-1"); got != "in-progress" {
		t.Errorf("milestone status = %q, want it left open by the failed write", got)
	}
}

// TestCloseMilestoneReplacesAnInvalidStatusRatherThanRefusingIt is the positive
// half of the pre-validation, and the axis the substitution actually operates
// on. closePreValidateSubject runs the subject's write-free guards BEFORE a
// disposition writes anyone else's file — but it runs them against the nib as
// it will be WRITTEN (a Clone carrying the pending status), not as it was read.
// Validating the nib as READ would refuse the close for the very value the
// close replaces, and a milestone whose `status:` the vocabulary no longer
// declares would become unclosable by the one command that would fix it.
//
// The store LOADS such a file — it warns and keeps reading — so this is
// reachable from a hand-edited nib, not only from a corrupt store. Its sibling
// TestCloseMilestoneInvalidSubjectRefusesBeforeTheQueueMoves covers the fields
// the close does NOT write, which stay refusals.
func TestCloseMilestoneReplacesAnInvalidStatusRatherThanRefusingIt(t *testing.T) {
	escapes := map[string][]string{
		"unassign-open": {"--unassign-open"},
		"move-open-to":  {"--move-open-to", "ms-2"},
	}
	for escape, flags := range escapes {
		t.Run(escape, func(t *testing.T) {
			files := milestoneStoreFiles(map[string]string{"open": "todo"})
			files["ms-1--wave-one.md"] = "---\nversion: 2\ntitle: Wave One\nstatus: retired-status\ntype: milestone\norder: a\n---\n\nBody.\n"
			nibsPath := setupMilestoneCloseTest(t, files)

			withStdin(t, "Wave over.\n")
			args := append([]string{"--nibs-path", nibsPath, "close", "ms-1", "--summary", "-"}, flags...)
			if _, err := runRootWith(t, args...); err != nil {
				t.Fatalf("--as replaces the offending status, so the close must not be refused for it: %v", err)
			}

			if got := statusOf(t, nibsPath, "ms-1"); got != closeDefaultStatus() {
				t.Errorf("milestone status = %q, want %q", got, closeDefaultStatus())
			}
			wantMilestone := ""
			if escape == "move-open-to" {
				wantMilestone = "ms-2"
			}
			if ms, _ := milestoneOf(t, nibsPath, "mem-open"); ms != wantMilestone {
				t.Errorf("mem-open milestone = %q, want %q — the disposition still ran", ms, wantMilestone)
			}
		})
	}
}

// TestCloseMilestoneMemberOwnFrontMatterDeadEndsBothEscapes: a member whose OWN
// front matter the write path refuses is a dead end for EVERY escape, not just
// the one that was tried. Both --move-open-to and --unassign-open go through
// UpdateNib, whose preValidateSubject runs ValidateEnums and the axis rule
// BEFORE validateAndSetMilestone branches, so the clear path meets the identical
// wall the assign path does — and a rerun recomputes the same set and stops at
// the same member forever.
//
// So the refusal has to say that, and has to arrive before any write: naming a
// remedy that cannot work (--unassign-open, or "rerun if transient") sends the
// caller into a loop, and disposing of the members ahead of the bad one first
// would destroy queue keys for a command that was never going to close the
// milestone.
//
// The shape is reachable from a hand-edited file: the store LOADS a nib whose
// status the vocabulary no longer declares — it warns and keeps reading — which
// is the same reasoning TestCloseMilestoneReplacesAnInvalidStatusRatherThanRefusingIt
// records for the subject, and it is store-wide.
func TestCloseMilestoneMemberOwnFrontMatterDeadEndsBothEscapes(t *testing.T) {
	escapes := map[string][]string{
		"unassign-open": {"--unassign-open"},
		"move-open-to":  {"--move-open-to", "ms-2"},
	}
	for escape, flags := range escapes {
		t.Run(escape, func(t *testing.T) {
			files := milestoneStoreFiles(map[string]string{"a": "todo", "b": "todo"})
			files["mem-b--member.md"] = strings.Replace(
				files["mem-b--member.md"], "status: todo", "status: retired-status", 1)
			nibsPath := setupMilestoneCloseTest(t, files)

			withStdin(t, "Wave over.\n")
			args := append([]string{"--nibs-path", nibsPath, "close", "ms-1", "--summary", "-"}, flags...)
			_, err := runRootWith(t, args...)
			if err == nil {
				t.Fatal("a member no escape can write must refuse the disposition, got nil")
			}

			msg := err.Error()
			// `nibs check` is deliberately NOT named: the message is shared
			// across causes and check is silent for one of them (see
			// closeRefusalOwnFrontMatter), so the pointer would be true here
			// and false one row down.
			for _, want := range []string{"mem-b", "front matter", "No escape and no retry"} {
				if !strings.Contains(msg, want) {
					t.Errorf("the diagnosis should contain %q, got: %s", want, msg)
				}
			}
			// The two exits that cannot work must not be offered.
			for _, unwanted := range []string{"--unassign-open", "transient"} {
				if strings.Contains(msg, unwanted) {
					t.Errorf("the diagnosis must not offer %q, which cannot clear this: %s", unwanted, msg)
				}
			}

			// Nothing was written: the member AHEAD of the bad one still holds its
			// assignment and its queue key.
			if ms, key := milestoneOf(t, nibsPath, "mem-a"); ms != "ms-1" || key == "" {
				t.Errorf("a doomed disposition must make no writes: mem-a = (%q, %q), want (ms-1, non-empty)", ms, key)
			}
			if ms, _ := milestoneOf(t, nibsPath, "mem-b"); ms != "ms-1" {
				t.Errorf("mem-b milestone = %q, want ms-1", ms)
			}
			if got := statusOf(t, nibsPath, "ms-1"); got != "in-progress" {
				t.Errorf("a refused close must leave the milestone open: status = %q", got)
			}
		})
	}
}

// TestCloseMilestoneUnwritableMemberDeadEndsBothEscapes is the other half of the
// dead end, in the filesystem rather than the front matter. Both escapes write
// the member's own file through the same call, so a member whose file cannot be
// written refuses identically under either — and again no rerun helps.
//
// This one cannot be pre-validated away: it is write I/O, so it surfaces from
// inside the disposition loop. What the message must not do is prescribe the
// rerun or the other escape.
func TestCloseMilestoneUnwritableMemberDeadEndsBothEscapes(t *testing.T) {
	escapes := map[string][]string{
		"unassign-open": {"--unassign-open"},
		"move-open-to":  {"--move-open-to", "ms-2"},
	}
	for escape, flags := range escapes {
		t.Run(escape, func(t *testing.T) {
			// Only the MEMBER sits in the directory that is closed to writes; both
			// milestones live one level down, so nothing else fails for the mode.
			nibsPath := setupMilestoneCloseTest(t, map[string]string{
				"mem-open--member.md": "---\nversion: 2\ntitle: Member open\nstatus: todo\ntype: task\nmilestone: ms-1\nmilestone_order: a\n---\n\nBody.\n",
			})
			if err := os.MkdirAll(dataPath(nibsPath, "sub"), 0o755); err != nil {
				t.Fatal(err)
			}
			for name, content := range map[string]string{
				"ms-1--wave-one.md": "---\nversion: 2\ntitle: Wave One\nstatus: in-progress\ntype: milestone\norder: a\n---\n\nBody.\n",
				"ms-2--wave-two.md": "---\nversion: 2\ntitle: Wave Two\nstatus: todo\ntype: milestone\norder: b\n---\n\nBody.\n",
			} {
				if err := os.WriteFile(dataPath(nibsPath, "sub", name), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			unwritableDataDirT(t, nibsPath)

			withStdin(t, "Wave over.\n")
			args := append([]string{"--nibs-path", nibsPath, "close", "ms-1", "--summary", "-"}, flags...)
			_, err := runRootWith(t, args...)
			if err == nil {
				t.Fatal("a member whose file cannot be written must refuse the disposition, got nil")
			}

			msg := err.Error()
			for _, want := range []string{"mem-open", "own file", "was NOT closed"} {
				if !strings.Contains(msg, want) {
					t.Errorf("the diagnosis should contain %q, got: %s", want, msg)
				}
			}
			for _, unwanted := range []string{"--unassign-open", "transient"} {
				if strings.Contains(msg, unwanted) {
					t.Errorf("the diagnosis must not offer %q, which cannot clear this: %s", unwanted, msg)
				}
			}
			if got := statusOf(t, nibsPath, "ms-1"); got != "in-progress" {
				t.Errorf("a refused close must leave the milestone open: status = %q", got)
			}
		})
	}
}

// TestCloseMilestoneHoldingReasonRefusesADispositionFlag: the escapes exist to
// escape the queue gate's REFUSAL, and a holding close reason is never refused —
// decision 1.5 gives a parked milestone its queue. Combining the two is a
// contradiction, and the destructive reading is the one that would silently win:
// the disposition drops every member's queue key, and Orderer.Recalculate issues
// a fresh one on any later reassignment, so the order the parked milestone was
// promised to keep is gone for good.
//
// Refused the way this codebase refuses its other contradictory pairs, with the
// alternative named for a caller who really did mean to clear the queue.
func TestCloseMilestoneHoldingReasonRefusesADispositionFlag(t *testing.T) {
	escapes := map[string][]string{
		"unassign-open": {"--unassign-open"},
		"move-open-to":  {"--move-open-to", "ms-2"},
	}
	holding := holdingCloseReason(t)
	for escape, flags := range escapes {
		t.Run(escape, func(t *testing.T) {
			nibsPath := setupMilestoneCloseTest(t, milestoneStoreFiles(map[string]string{"open": "todo"}))

			withStdin(t, "Parking the wave.\n")
			args := append([]string{"--nibs-path", nibsPath, "close", "ms-1", "--as", holding, "--summary", "-"}, flags...)
			_, err := runRootWith(t, args...)
			if err == nil {
				t.Fatalf("--as %s with a disposition flag is a contradiction and must be refused, got nil", holding)
			}
			if code := closeErrCode(t, err); output.ExitCode(code) != output.ExitValidation {
				t.Errorf("exit = %d, want %d (validation)", output.ExitCode(code), output.ExitValidation)
			}

			msg := err.Error()
			for _, want := range []string{"--as " + holding, "keeps its queue", "--clear milestone"} {
				if !strings.Contains(msg, want) {
					t.Errorf("the refusal should contain %q, got: %s", want, msg)
				}
			}

			if ms, key := milestoneOf(t, nibsPath, "mem-open"); ms != "ms-1" || key == "" {
				t.Errorf("the queue a parked milestone keeps was drained anyway: mem-open = (%q, %q)", ms, key)
			}
			if got := statusOf(t, nibsPath, "ms-1"); got != "in-progress" {
				t.Errorf("a refused close still wrote the milestone: status = %q", got)
			}
		})
	}
}

// TestCloseMilestoneDisposesMembersMissingEitherStamp pins each of loadNib's
// three timestamp-synthesis shapes through the disposition. A hand-authored nib
// omitting created_at, updated_at or both is loaded with the missing one
// synthesized, so its in-memory nib renders stamps the file does not carry —
// and unless nibcore.reconcileLoaderDerived takes those back out of the
// comparison, such a member false-conflicts and the escape is unusable in a
// hand-authored store.
//
// One member per shape, because the reconciliation fills the two stamps
// independently: disable either fill and exactly the members whose files lack
// that stamp start refusing.
func TestCloseMilestoneDisposesMembersMissingEitherStamp(t *testing.T) {
	const stamp = "2026-01-02T03:04:05Z"
	member := func(title, order, stamps string) string {
		return "---\nversion: 2\ntitle: " + title + "\nstatus: todo\ntype: task\n" +
			"milestone: ms-1\nmilestone_order: " + order + "\n" + stamps + "---\n\nBody.\n"
	}
	nibsPath := setupMilestoneCloseTest(t, map[string]string{
		"ms-1--wave-one.md": "---\nversion: 2\ntitle: Wave One\nstatus: in-progress\ntype: milestone\norder: a\n" +
			"created_at: " + stamp + "\nupdated_at: " + stamp + "\n---\n\nBody.\n",
		// Neither stamp: created_at comes from the file's mtime, updated_at from it.
		"mem-none--member.md": member("Member none", "a", ""),
		// created_at only missing: synthesized from updated_at.
		"mem-created--member.md": member("Member created", "b", "updated_at: "+stamp+"\n"),
		// updated_at only missing: defaulted to created_at.
		"mem-updated--member.md": member("Member updated", "c", "created_at: "+stamp+"\n"),
	})

	withStdin(t, "Wave over.\n")
	if _, err := runRootWith(t, "--nibs-path", nibsPath,
		"close", "ms-1", "--unassign-open", "--summary", "-"); err != nil {
		t.Fatalf("every hand-authored stamp shape has to be disposable: %v", err)
	}
	for _, id := range []string{"mem-none", "mem-created", "mem-updated"} {
		if ms, key := milestoneOf(t, nibsPath, id); ms != "" || key != "" {
			t.Errorf("%s = (%q, %q) after --unassign-open, want both empty", id, ms, key)
		}
	}
}

// TestCloseMilestoneRefusesAStampDeletionOnAFullyStampedMember bounds, through
// the close path, the reconciliation the test above needs. A stamp DELETED from
// a member's file renders exactly like a stamp the loader synthesized, so what
// separates them is the store's own pair: loadNib's fallback always leaves the
// two CARRYING THE SAME VALUE, and a member loaded with two DIFFERENT stamps
// therefore synthesized nothing and has to match exactly
// (nibcore.loaderMaySynthesizeStamps).
//
// Unbounded, the deletion is accepted and Core.Update writes the stale in-memory
// clone: it re-stamps updated_at but never assigns created_at, so the deleted key
// is silently restored and the concurrent edit is lost with no conflict raised.
func TestCloseMilestoneRefusesAStampDeletionOnAFullyStampedMember(t *testing.T) {
	const memberFile = "mem-open--member.md"
	const loaded = "---\nversion: 2\ntitle: Member open\nstatus: todo\ntype: task\n" +
		"milestone: ms-1\nmilestone_order: a\n" +
		"created_at: 2019-01-01T00:00:00Z\nupdated_at: 2026-01-02T03:04:05Z\n---\n\nBody.\n"
	const deleted = "---\nversion: 2\ntitle: Member open\nstatus: todo\ntype: task\n" +
		"milestone: ms-1\nmilestone_order: a\n" +
		"updated_at: 2026-01-02T03:04:05Z\n---\n\nBody.\n"

	files := stampedStoreFiles(milestoneStoreFiles(map[string]string{"open": "todo"}))
	files[memberFile] = loaded
	nibsPath := setupMilestoneCloseTest(t, files)

	core := nibcore.New(nibsPath, config.Default())
	if err := core.Load(); err != nil {
		t.Fatalf("load store: %v", err)
	}
	app := &App{Core: core}
	resolver := app.newResolver()
	subject, ok := resolver.Reader.GetSnapshot("ms-1")
	if !ok {
		t.Fatal("test setup: ms-1 is not in the store")
	}
	if err := os.WriteFile(dataPath(nibsPath, memberFile), []byte(deleted), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := closeUnassignOpenWork(context.Background(), app, resolver, subject, []string{"mem-open"}, 1)
	if err == nil {
		t.Fatal("a concurrent stamp deletion on a fully-stamped member must be refused, got nil")
	}
	if code := closeErrCode(t, err); output.ExitCode(code) != output.ExitConflict {
		t.Errorf("exit = %d, want %d (conflict): %v", output.ExitCode(code), output.ExitConflict, err)
	}
	if got := readNibFile(t, nibsPath, memberFile); got != deleted {
		t.Errorf("the deleted created_at was silently restored; file is now:\n%s", got)
	}
}

// TestCloseMilestoneMemberUndeclaredAreaDeadEndsBothEscapes is the area half of
// the own-front-matter dead end. Retiring or renaming an `areas:` entry leaves
// every nib that carried it loading fine and refused by every write path, so a
// queue member in that shape is exactly the case closePreValidateMembers exists
// for: no escape and no rerun can dispose of it, and disposing of the members
// ahead of it first destroys queue keys Orderer.Recalculate never gives back.
//
// The guard set closeMemberOwnGuards runs therefore has to be the same one
// preValidateSubject runs, area rule included — the sibling status case is
// TestCloseMilestoneMemberOwnFrontMatterDeadEndsBothEscapes.
func TestCloseMilestoneMemberUndeclaredAreaDeadEndsBothEscapes(t *testing.T) {
	escapes := map[string][]string{
		"unassign-open": {"--unassign-open"},
		"move-open-to":  {"--move-open-to", "tnib-m002"},
	}
	for escape, flags := range escapes {
		t.Run(escape, func(t *testing.T) {
			nibsPath := setupMilestoneCloseFixture(t)

			// tnib-m001's open queue is tnib-e001..e004 in that order; the
			// offender sits SECOND so a disposition that starts writing leaves
			// tnib-e001 visibly rewritten.
			queue := queueIDs(t, nibsPath, "tnib-m001")
			if len(queue) < 2 || queue[1] != "tnib-e002" {
				t.Fatalf("test setup: queue = %v, want tnib-e002 second", queue)
			}
			resetQueueCLIFlags()
			if _, err := runRootWith(t, "--nibs-path", nibsPath, "set", "tnib-e002", "--area", "auth"); err != nil {
				t.Fatalf("set --area auth: %v", err)
			}
			rewriteStoredArea(t, nibsPath, "tnib-e002", "auth", "retired/thing")

			before := queueFileContents(t, nibsPath, queue)

			resetQueueCLIFlags()
			resetCloseFlags()
			withStdin(t, "Wave over.\n")
			args := append([]string{"--nibs-path", nibsPath, "close", "tnib-m001", "--summary", "-"}, flags...)
			_, err := runRootWith(t, args...)
			if err == nil {
				t.Fatal("a member carrying an undeclared area must refuse the disposition, got nil")
			}

			msg := err.Error()
			// The repair is quoted inline as a command the reader can run —
			// `nibs check` reports nothing for an undeclared area, so pointing
			// there would name a report with nothing in it.
			for _, want := range []string{"tnib-e002", "front matter", "retired/thing",
				"`nibs set tnib-e002 --clear area`"} {
				if !strings.Contains(msg, want) {
					t.Errorf("the diagnosis should contain %q, got: %s", want, msg)
				}
			}
			// The two exits that cannot work must not be offered, and neither
			// must a diagnostic that says nothing about this cause.
			for _, unwanted := range []string{"--unassign-open", "transient", "nibs check"} {
				if strings.Contains(msg, unwanted) {
					t.Errorf("the diagnosis must not offer %q, which cannot clear this: %s", unwanted, msg)
				}
			}

			// Nothing was written: every member file, the ones AHEAD of the
			// offender included, is byte-identical to what it was.
			for _, id := range queue {
				if got := before[id]; got != readQueueFile(t, nibsPath, id) {
					t.Errorf("a doomed disposition rewrote %s; before:\n%s\nafter:\n%s", id, got, readQueueFile(t, nibsPath, id))
				}
			}
			if got := statusOf(t, nibsPath, "tnib-m001"); got != "in-progress" {
				t.Errorf("a refused close must leave the milestone open: status = %q", got)
			}
		})
	}
}

// readQueueFile reads one nib's file out of data/ by id, for the byte-identity
// assertions a "no writes happened" claim needs.
func readQueueFile(t *testing.T, nibsPath, id string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(nibsPath, "data", id+"*.md"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("locating %s: %v (matches %v)", id, err, matches)
	}
	raw, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("reading %s: %v", matches[0], err)
	}
	return string(raw)
}

func queueFileContents(t *testing.T, nibsPath string, ids []string) map[string]string {
	t.Helper()
	contents := make(map[string]string, len(ids))
	for _, id := range ids {
		contents[id] = readQueueFile(t, nibsPath, id)
	}
	return contents
}
