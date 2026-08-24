package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/output"
)

// runMv drives the full Cobra pipeline and returns the command's error, if any.
func runMv(t *testing.T, nibsDir string, args ...string) error {
	t.Helper()
	rootCmd.SetArgs(append([]string{"--nibs-path", nibsDir, "mv"}, args...))
	var execErr error
	captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	return execErr
}

// mvValidationMessage asserts the error is a VALIDATION refusal and returns its text.
func mvValidationMessage(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("expected a refusal, got nil")
	}
	var coded *output.CodedError
	if !errors.As(err, &coded) {
		t.Fatalf("error is %T (%v), want *output.CodedError", err, err)
	}
	if got := output.ExitCode(coded.Code); got != output.ExitValidation {
		t.Errorf("exit = %d, want %d (validation)", got, output.ExitValidation)
	}
	return err.Error()
}

// blockFixture is five ordered siblings under one epic — enough for a contiguous
// block move to be distinguishable from a single move.
func blockFixture() map[string]string {
	files := map[string]string{
		"epic1.md": "---\nversion: 2\ntitle: Epic\nstatus: todo\ntype: epic\norder: a0\n---\n",
	}
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		files[id+".md"] = "---\nversion: 2\ntitle: " + strings.ToUpper(id) +
			"\nstatus: todo\ntype: task\nparent: epic1\norder: " + id + "0\n---\n"
	}
	return files
}

// TestMvRefusesAnUnannouncedBlockMove is nibs-qfv4.
//
// The block move used to be the one multi-nib regime entered IMPLICITLY, by argument
// count alone, while its two siblings announce themselves: --children-of selects the
// full-reorder mode and --queue the queue scope. That asymmetry is what let a
// mistyped single move become a block move — `--first` is a bool and takes no anchor,
// so `nibs mv <id> --first <anchor>` parses as two positional ids and reorders a nib
// the caller never named. Flag position is not recoverable after parsing
// (`mv a b --first` and `mv a --first b` are byte-identical to the command), so the
// only place to catch it is the arity itself.
func TestMvRefusesAnUnannouncedBlockMove(t *testing.T) {
	nibsDir := setupMvCobraTest(t, blockFixture())

	msg := mvValidationMessage(t, runMv(t, nibsDir, "c", "e", "--after", "a"))
	if !strings.Contains(msg, "--block") {
		t.Errorf("refusal must name --block, the flag that expresses the intent: %s", msg)
	}
	if !strings.Contains(msg, "--children-of") {
		t.Errorf("refusal should also name the other multi-nib mode: %s", msg)
	}
	// The refusal must precede every write.
	for i, b := range listChildrenOrder(t, nibsDir, "epic1") {
		if want := []string{"a", "b", "c", "d", "e"}[i]; b.ID != want {
			t.Errorf("a refused move still reordered: got[%d] = %q, want %q", i, b.ID, want)
		}
	}
}

// TestMvRefusalDiagnosesTheFirstTypo pins the targeted half of that refusal. Two ids
// with --first set is overwhelmingly the mistyped single move rather than a real
// block move, so the message names the cause instead of only the remedy — the old
// failure was expensive precisely because it pointed nowhere near the mistake.
func TestMvRefusalDiagnosesTheFirstTypo(t *testing.T) {
	nibsDir := setupMvCobraTest(t, blockFixture())

	msg := mvValidationMessage(t, runMv(t, nibsDir, "c", "--first", "e"))
	if !strings.Contains(msg, "takes no anchor") {
		t.Errorf("refusal should explain that --first takes no anchor, so a trailing id "+
			"becomes a second nib: %s", msg)
	}
}

// TestMvBlockMovesWhenAnnounced is the other side: the capability is unchanged, it
// just has to say so. Same fixture and expectation as the implicit form used to have.
func TestMvBlockMovesWhenAnnounced(t *testing.T) {
	nibsDir := setupMvCobraTest(t, blockFixture())

	if err := runMv(t, nibsDir, "c", "e", "--block", "--after", "a"); err != nil {
		t.Fatalf("announced block move failed: %v", err)
	}
	want := []string{"a", "c", "e", "b", "d"}
	got := listChildrenOrder(t, nibsDir, "epic1")
	if len(got) != len(want) {
		t.Fatalf("got %d children, want %d", len(got), len(want))
	}
	for i, b := range got {
		if b.ID != want[i] {
			t.Errorf("got[%d].ID = %q, want %q", i, b.ID, want[i])
		}
	}
}

// TestMvBlockRefusesASingleNib keeps one operation from having two spellings: a
// "block" of one is just a single move, and allowing it would give the same action
// two grammars with different output shapes (a count versus a nib echo).
func TestMvBlockRefusesASingleNib(t *testing.T) {
	nibsDir := setupMvCobraTest(t, blockFixture())

	msg := mvValidationMessage(t, runMv(t, nibsDir, "--block", "c", "--first"))
	if !strings.Contains(msg, "--block") {
		t.Errorf("refusal should name the flag to drop: %s", msg)
	}
}

// TestMvSingleNibStillMovesWithoutTheFlag guards the boundary the new arity check
// sits on: one id must keep working untouched, with no --block anywhere.
func TestMvSingleNibStillMovesWithoutTheFlag(t *testing.T) {
	nibsDir := setupMvCobraTest(t, blockFixture())

	if err := runMv(t, nibsDir, "c", "--first"); err != nil {
		t.Fatalf("single-nib move failed: %v", err)
	}
	if got := listChildrenOrder(t, nibsDir, "epic1"); got[0].ID != "c" {
		t.Errorf("first child = %q, want c", got[0].ID)
	}
}
