package cmd

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/output"
)

// firstLine returns the first line of s (the "# <n> nibs" header for TSV output).
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// jsonHiddenClosed reports the envelope's hidden_closed value and whether the
// key was present at all (omitempty drops it when 0), decoded from the raw JSON
// so the omitted-vs-zero distinction is observable.
func jsonHiddenClosed(t *testing.T, raw string) (int, bool) {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("unmarshal envelope: %v\nraw: %s", err, raw)
	}
	v, ok := m["hidden_closed"]
	if !ok {
		return 0, false
	}
	var n int
	if err := json.Unmarshal(v, &n); err != nil {
		t.Fatalf("hidden_closed not an int: %v\nraw: %s", err, raw)
	}
	return n, true
}

// twoTypeStatusFixture discriminates the hidden-closed count against a non-status
// filter: bugs and features, each with open and closed members. A hidden-closed
// count computed for -t bug must count only the closed bug, not the closed
// features.
func twoTypeStatusFixture() map[string]string {
	return map[string]string{
		"bo--bug-open.md":   "---\ntitle: BugOpen\nstatus: todo\ntype: bug\n---\n",
		"bd--bug-done.md":   "---\ntitle: BugDone\nstatus: completed\ntype: bug\n---\n",
		"fo--feat-open.md":  "---\ntitle: FeatOpen\nstatus: todo\ntype: feature\n---\n",
		"fd--feat-done.md":  "---\ntitle: FeatDone\nstatus: completed\ntype: feature\n---\n",
		"fs--feat-scrap.md": "---\ntitle: FeatScrap\nstatus: scrapped\ntype: feature\n---\n",
	}
}

// allOpenFixture has no completed/scrapped nibs, so the open default hides
// nothing and must NOT annotate.
func allOpenFixture() map[string]string {
	return map[string]string{
		"o1--one.md": "---\ntitle: One\nstatus: todo\ntype: task\n---\n",
		"o2--two.md": "---\ntitle: Two\nstatus: in-progress\ntype: task\n---\n",
	}
}

// TestListCommand_HiddenClosed_Header covers the TSV/view header disclosure: the
// open default annotates "# N nibs (M hidden: completed/scrapped — --all to
// include)" when it suppressed rows, and stays bare when the user asked
// explicitly (-s/--all/--ready) or nothing was hidden.
func TestListCommand_HiddenClosed_Header(t *testing.T) {
	tests := []struct {
		name        string
		fixture     map[string]string
		args        []string
		wantHeader  string
		wantNoteAbs bool // when true, the header must carry no "hidden:" annotation
	}{
		{
			name:       "default annotates suppressed closed rows",
			fixture:    mixedStatusFixture(),
			args:       nil,
			wantHeader: "# 4 nibs (2 hidden: completed/scrapped — --all to include)",
		},
		{
			name:        "--all shows every status, no annotation",
			fixture:     mixedStatusFixture(),
			args:        []string{"--all"},
			wantHeader:  "# 6 nibs",
			wantNoteAbs: true,
		},
		{
			name:        "-s completed is explicit, no annotation",
			fixture:     mixedStatusFixture(),
			args:        []string{"-s", "completed"},
			wantHeader:  "# 1 nibs",
			wantNoteAbs: true,
		},
		{
			name:        "-s open is explicit, no annotation",
			fixture:     mixedStatusFixture(),
			args:        []string{"-s", "open"},
			wantHeader:  "# 4 nibs",
			wantNoteAbs: true,
		},
		{
			name:        "--ready is explicit, no annotation",
			fixture:     mixedStatusFixture(),
			args:        []string{"--ready"},
			wantNoteAbs: true,
		},
		{
			name:        "nothing hidden, no annotation",
			fixture:     allOpenFixture(),
			args:        nil,
			wantHeader:  "# 2 nibs",
			wantNoteAbs: true,
		},
		{
			name:       "hidden count respects a non-status filter",
			fixture:    twoTypeStatusFixture(),
			args:       []string{"-t", "bug"},
			wantHeader: "# 1 nibs (1 hidden: completed/scrapped — --all to include)",
		},
		{
			name:       "hidden count respects --no-status (only completed remains hidden)",
			fixture:    mixedStatusFixture(),
			args:       []string{"--no-status", "scrapped"},
			wantHeader: "# 4 nibs (1 hidden: completed/scrapped — --all to include)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsDir := setupListCobraTest(t, tt.fixture)
			out, err := runListCmd(t, nibsDir, tt.args...)
			if err != nil {
				t.Fatalf("list %v failed: %v\nout: %s", tt.args, err, out)
			}
			header := firstLine(out)
			if tt.wantNoteAbs {
				if strings.Contains(header, "hidden:") {
					t.Errorf("header should carry no annotation, got %q", header)
				}
			}
			if tt.wantHeader != "" && header != tt.wantHeader {
				t.Errorf("header = %q, want %q", header, tt.wantHeader)
			}
		})
	}
}

// TestListCommand_HiddenClosed_JSON covers the --json envelope: hidden_closed is
// present with the pre-limit count under the open default and omitted otherwise.
func TestListCommand_HiddenClosed_JSON(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantHC   int  // expected hidden_closed
		wantPres bool // whether the key should be present
	}{
		{"default discloses hidden count", []string{"--json"}, 2, true},
		{"--all omits the key", []string{"--all", "--json"}, 0, false},
		{"-s closed omits the key", []string{"-s", "closed", "--json"}, 0, false},
		{"--ready omits the key", []string{"--ready", "--json"}, 0, false},
		{"hidden_closed is pre-limit, not the limited page", []string{"--limit", "1", "--json"}, 2, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsDir := setupListCobraTest(t, mixedStatusFixture())
			out, err := runListCmd(t, nibsDir, tt.args...)
			if err != nil {
				t.Fatalf("list %v failed: %v\nout: %s", tt.args, err, out)
			}
			hc, present := jsonHiddenClosed(t, out)
			if present != tt.wantPres {
				t.Errorf("hidden_closed present = %v, want %v\nraw: %s", present, tt.wantPres, out)
			}
			if present && hc != tt.wantHC {
				t.Errorf("hidden_closed = %d, want %d\nraw: %s", hc, tt.wantHC, out)
			}
		})
	}
}

// TestListCommand_TerseOutputs_StayBare pins that -c/-q honor the open default
// but are never annotated: -c is a bare integer, -q is bare ids.
func TestListCommand_TerseOutputs_StayBare(t *testing.T) {
	t.Run("count is a bare open-default integer", func(t *testing.T) {
		nibsDir := setupListCobraTest(t, mixedStatusFixture())
		out, err := runListCmd(t, nibsDir, "-c")
		if err != nil {
			t.Fatalf("list -c failed: %v", err)
		}
		if strings.TrimSpace(out) != "4" {
			t.Errorf("list -c = %q, want \"4\" (open default)", strings.TrimSpace(out))
		}
		if strings.Contains(out, "hidden") || strings.Contains(out, "#") {
			t.Errorf("list -c must stay bare, got %q", out)
		}
	})

	t.Run("count --all gives the total", func(t *testing.T) {
		nibsDir := setupListCobraTest(t, mixedStatusFixture())
		out, err := runListCmd(t, nibsDir, "-c", "--all")
		if err != nil {
			t.Fatalf("list -c --all failed: %v", err)
		}
		if strings.TrimSpace(out) != "6" {
			t.Errorf("list -c --all = %q, want \"6\"", strings.TrimSpace(out))
		}
	})

	t.Run("quiet is bare ids honoring the open default", func(t *testing.T) {
		nibsDir := setupListCobraTest(t, mixedStatusFixture())
		out, err := runListCmd(t, nibsDir, "-q")
		if err != nil {
			t.Fatalf("list -q failed: %v", err)
		}
		if strings.Contains(out, "hidden") || strings.Contains(out, "#") {
			t.Errorf("list -q must stay bare, got %q", out)
		}
		ids := strings.Fields(strings.TrimSpace(out))
		if len(ids) != 4 {
			t.Errorf("list -q emitted %d ids, want 4 (open default)\nraw: %s", len(ids), out)
		}
	})
}

// TestRelCommand_HiddenClosed covers rel's disclosure: the default children
// traversal annotates the header and populates hidden_closed; --all and -s
// closed suppress both.
func TestRelCommand_HiddenClosed(t *testing.T) {
	t.Run("default annotates and discloses in JSON", func(t *testing.T) {
		nibsDir := setupRelCobraTest(t, relStatusFixture)
		out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "par", "--rel", "children", "--json")
		hc, present := jsonHiddenClosed(t, out)
		if !present || hc != 2 {
			t.Errorf("hidden_closed = (%d, present=%v), want (2, true)\nraw: %s", hc, present, out)
		}
	})

	t.Run("default TSV header is annotated", func(t *testing.T) {
		nibsDir := setupRelCobraTest(t, relStatusFixture)
		out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "par", "--rel", "children", "-f", "id,status")
		header := firstLine(out)
		want := "# 2 nibs (2 hidden: completed/scrapped — --all to include)"
		if header != want {
			t.Errorf("rel header = %q, want %q", header, want)
		}
	})

	t.Run("--all suppresses the annotation and the key", func(t *testing.T) {
		nibsDir := setupRelCobraTest(t, relStatusFixture)
		out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "par", "--rel", "children", "--all", "--json")
		if _, present := jsonHiddenClosed(t, out); present {
			t.Errorf("--all must omit hidden_closed, got: %s", out)
		}
	})

	t.Run("-s closed is explicit, no key", func(t *testing.T) {
		nibsDir := setupRelCobraTest(t, relStatusFixture)
		out := runRelJSON(t, "--nibs-path", nibsDir, "rel", "par", "--rel", "children", "-s", "closed", "--json")
		if _, present := jsonHiddenClosed(t, out); present {
			t.Errorf("-s closed must omit hidden_closed, got: %s", out)
		}
	})
}

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
