package cmd

import (
	"errors"
	"testing"

	"github.com/alphaleonis/nibs/internal/output"
)

// mixedStatusFixture covers every status so the open-by-default and status-group
// behavior can be exercised. Four open (in-progress/todo/draft/deferred) and two
// archived (completed/scrapped).
func mixedStatusFixture() map[string]string {
	return map[string]string{
		"w1--wip.md":    "---\ntitle: Wip\nstatus: in-progress\ntype: task\n---\n",
		"t1--todo.md":   "---\ntitle: Todo\nstatus: todo\ntype: task\n---\n",
		"d1--draft.md":  "---\ntitle: Draft\nstatus: draft\ntype: task\n---\n",
		"p1--parked.md": "---\ntitle: Parked\nstatus: deferred\ntype: task\n---\n",
		"c1--done.md":   "---\ntitle: Done\nstatus: completed\ntype: task\n---\n",
		"s1--scrap.md":  "---\ntitle: Scrap\nstatus: scrapped\ntype: task\n---\n",
	}
}

func TestListCommand_OpenByDefault(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantIDs []string
	}{
		{
			name:    "no status flag hides completed and scrapped",
			args:    []string{"--json"},
			wantIDs: []string{"w1", "t1", "d1", "p1"},
		},
		{
			name:    "--all includes every status",
			args:    []string{"--all", "--json"},
			wantIDs: []string{"w1", "t1", "d1", "p1", "c1", "s1"},
		},
		{
			name:    "-s closed shows the archive group only",
			args:    []string{"-s", "closed", "--json"},
			wantIDs: []string{"c1", "s1"},
		},
		{
			name:    "-s open shows the open group",
			args:    []string{"-s", "open", "--json"},
			wantIDs: []string{"w1", "t1", "d1", "p1"},
		},
		{
			name:    "--open is shorthand for -s open",
			args:    []string{"--open", "--json"},
			wantIDs: []string{"w1", "t1", "d1", "p1"},
		},
		{
			name:    "--active is a synonym for --open",
			args:    []string{"--active", "--json"},
			wantIDs: []string{"w1", "t1", "d1", "p1"},
		},
		{
			name:    "-s parked shows deferred only",
			args:    []string{"-s", "parked", "--json"},
			wantIDs: []string{"p1"},
		},
		{
			name:    "-s completed overrides open default",
			args:    []string{"-s", "completed", "--json"},
			wantIDs: []string{"c1"},
		},
		{
			name:    "--all --no-status parked subtracts deferred from the full set",
			args:    []string{"--all", "--no-status", "parked", "--json"},
			wantIDs: []string{"w1", "t1", "d1", "c1", "s1"},
		},
		{
			name:    "-s open --no-status parked subtracts within the open group",
			args:    []string{"-s", "open", "--no-status", "parked", "--json"},
			wantIDs: []string{"w1", "t1", "d1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Fresh setup per subtest resets the package-level flag vars so the
			// repeatable -s/--no-status arrays don't accumulate across cases.
			nibsDir := setupListCobraTest(t, mixedStatusFixture())
			out, err := runListCmd(t, nibsDir, tt.args...)
			if err != nil {
				t.Fatalf("list %v failed: %v\nout: %s", tt.args, err, out)
			}
			got := envelopeIDs(parseListEnvelope(t, out))
			assertIDSet(t, got, tt.wantIDs)
		})
	}
}

// TestListCommand_ActiveNoLongerUnknownFlag pins the asymmetry fix: --active
// used to be a "unknown flag" error on list (it lived only on rel/plan).
func TestListCommand_ActiveNoLongerUnknownFlag(t *testing.T) {
	nibsDir := setupListCobraTest(t, mixedStatusFixture())
	out, err := runListCmd(t, nibsDir, "--active", "--json")
	if err != nil {
		t.Fatalf("list --active should be accepted, got error: %v\nout: %s", err, out)
	}
}

func TestListCommand_BadStatus_Validation(t *testing.T) {
	nibsDir := setupListCobraTest(t, mixedStatusFixture())
	out, err := runListCmd(t, nibsDir, "-s", "bogus")
	if err == nil {
		t.Fatalf("list -s bogus should fail; out: %q", out)
	}
	var ce *output.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("expected a coded error, got %v", err)
	}
	if ce.Code != output.ErrValidation {
		t.Errorf("error code = %q, want %q", ce.Code, output.ErrValidation)
	}
	if output.ExitCode(ce.Code) != 2 {
		t.Errorf("exit code = %d, want 2", output.ExitCode(ce.Code))
	}
}

func TestListCommand_BadNoStatus_Validation(t *testing.T) {
	nibsDir := setupListCobraTest(t, mixedStatusFixture())
	_, err := runListCmd(t, nibsDir, "--no-status", "bogus")
	if err == nil {
		t.Fatal("list --no-status bogus should fail")
	}
	var ce *output.CodedError
	if !errors.As(err, &ce) || ce.Code != output.ErrValidation {
		t.Errorf("expected VALIDATION coded error, got %v", err)
	}
}

// relStatusFixture: parent epic with open + completed + scrapped children.
var relStatusFixture = map[string]string{
	"par--parent.md":  "---\ntitle: Parent\nstatus: in-progress\ntype: epic\n---\n",
	"co--open.md":     "---\ntitle: COpen\nstatus: todo\ntype: task\nparent: par\norder: a0\n---\n",
	"cd--done.md":     "---\ntitle: CDone\nstatus: completed\ntype: task\nparent: par\norder: a1\n---\n",
	"cs--scrap.md":    "---\ntitle: CScrap\nstatus: scrapped\ntype: task\nparent: par\norder: a2\n---\n",
	"cf--deferred.md": "---\ntitle: CDeferred\nstatus: deferred\ntype: task\nparent: par\norder: a3\n---\n",
}

func TestRelCommand_OpenByDefault(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantIDs []string
	}{
		{
			name:    "children hides completed and scrapped by default",
			args:    []string{"rel", "par", "--rel", "children", "--json"},
			wantIDs: []string{"co", "cf"},
		},
		{
			name:    "--all shows every child",
			args:    []string{"rel", "par", "--rel", "children", "--all", "--json"},
			wantIDs: []string{"co", "cd", "cs", "cf"},
		},
		{
			name:    "-s closed shows archived children",
			args:    []string{"rel", "par", "--rel", "children", "-s", "closed", "--json"},
			wantIDs: []string{"cd", "cs"},
		},
		{
			name:    "--open shows open children",
			args:    []string{"rel", "par", "--rel", "children", "--open", "--json"},
			wantIDs: []string{"co", "cf"},
		},
		{
			name:    "--active is a synonym for --open on rel",
			args:    []string{"rel", "par", "--rel", "children", "--active", "--json"},
			wantIDs: []string{"co", "cf"},
		},
		{
			name:    "-s parked shows deferred child",
			args:    []string{"rel", "par", "--rel", "children", "-s", "parked", "--json"},
			wantIDs: []string{"cf"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsDir := setupRelCobraTest(t, relStatusFixture)
			args := append([]string{"--nibs-path", nibsDir}, tt.args...)
			out := runRelJSON(t, args...)
			got := relEnvIDs(decodeRelEnvelope(t, out))
			assertIDSet(t, got, tt.wantIDs)
		})
	}
}

// TestRelCommand_ActivePlusStatusIsUnion pins that --active (== -s open) unions
// with an explicit -s rather than erroring: under the old semantics --active
// with -s completed was rejected as "always empty".
func TestRelCommand_ActivePlusExplicitStatus_Union(t *testing.T) {
	nibsDir := setupRelCobraTest(t, relStatusFixture)
	out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "par", "--rel", "children", "--active", "-s", "completed", "--json")
	got := relEnvIDs(decodeRelEnvelope(t, out))
	// open children (co, cf) unioned with the explicitly requested completed (cd).
	assertIDSet(t, got, []string{"co", "cf", "cd"})
}

func TestRelCommand_BadStatus_Validation(t *testing.T) {
	nibsDir := setupRelCobraTest(t, relStatusFixture)
	_, err := runRelExpectError(t, "--nibs-path", nibsDir, "rel", "par", "--rel", "children", "-s", "bogus")
	if err == nil {
		t.Fatal("rel -s bogus should fail")
	}
	var ce *output.CodedError
	if !errors.As(err, &ce) || ce.Code != output.ErrValidation {
		t.Errorf("expected VALIDATION coded error, got %v", err)
	}
}

// assertIDSet fails unless got is exactly the set wantIDs.
func assertIDSet(t *testing.T, got map[string]bool, wantIDs []string) {
	t.Helper()
	if len(got) != len(wantIDs) {
		t.Errorf("id set = %v, want %v (size %d vs %d)", idKeys(got), wantIDs, len(got), len(wantIDs))
		return
	}
	for _, id := range wantIDs {
		if !got[id] {
			t.Errorf("missing %q; got %v, want %v", id, idKeys(got), wantIDs)
		}
	}
}

func idKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
