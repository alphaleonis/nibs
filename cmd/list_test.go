package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/spf13/pflag"
)

func TestBuildNibSort(t *testing.T) {
	tests := []struct {
		name      string
		sortFlag  string
		wantField model.NibSortField
		wantDesc  bool
	}{
		{"default", "", model.NibSortFieldOrder, false},
		{"created", "created", model.NibSortFieldCreatedAt, true},
		{"updated", "updated", model.NibSortFieldUpdatedAt, true},
		{"status", "status", model.NibSortFieldStatus, false},
		{"priority", "priority", model.NibSortFieldPriority, false},
		{"status-priority", "status-priority", model.NibSortFieldStatusPriority, false},
		{"id", "id", model.NibSortFieldID, false},
		{"unknown falls back to order", "garbage", model.NibSortFieldOrder, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildNibSort(tt.sortFlag)
			if got.Field != tt.wantField {
				t.Errorf("field = %s, want %s", got.Field, tt.wantField)
			}
			gotDesc := got.Direction != nil && *got.Direction == model.SortDirectionDesc
			if gotDesc != tt.wantDesc {
				t.Errorf("desc = %v, want %v", gotDesc, tt.wantDesc)
			}
		})
	}
}

// TestListReadyFlagMutualExclusion drives the real command so the assertion is
// on list.go's guard, not on a copy of it. --is-blocked=false is the case that
// matters: it is a *set* --is-blocked, so pairing it with --ready must be
// rejected even though the flag's value is false.
func TestListReadyFlagMutualExclusion(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantError bool
	}{
		{"neither flag", nil, false},
		{"only --ready", []string{"--ready"}, false},
		{"only --is-blocked=true", []string{"--is-blocked=true"}, false},
		{"only --is-blocked=false", []string{"--is-blocked=false"}, false},
		{"--ready --is-blocked=true", []string{"--ready", "--is-blocked=true"}, true},
		{"--ready --is-blocked=false", []string{"--ready", "--is-blocked=false"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsDir := setupListCobraTest(t, isBlockedFixture())
			out, err := runListCmd(t, nibsDir, append(append([]string{}, tt.args...), "-q")...)
			if !tt.wantError {
				if err != nil {
					t.Fatalf("list %v failed: %v\nout: %s", tt.args, err, out)
				}
				return
			}
			if err == nil {
				t.Fatalf("list %v: want a mutual-exclusion error, got nil\nout: %s", tt.args, out)
			}
			var ce *output.CodedError
			if !errors.As(err, &ce) || ce.Code != output.ErrValidation {
				t.Errorf("list %v: want a VALIDATION coded error, got: %v", tt.args, err)
			}
			if !strings.Contains(err.Error(), "mutually exclusive") {
				t.Errorf("list %v: error should say the flags are mutually exclusive; got: %v", tt.args, err)
			}
		})
	}
}

// isBlockedFixture splits four todo nibs cleanly into blocked and unblocked:
// b1 and b2 are each blocked by bk, which is itself open (todo) and so still
// blocking; bk and f1 have no blockers. Every nib is todo, so -s todo keeps
// the whole set in play and the two --is-blocked answers are exact complements.
func isBlockedFixture() map[string]string {
	return map[string]string{
		"bk--blocker.md":     "---\ntitle: Blocker\nstatus: todo\ntype: task\n---\n",
		"b1--blocked-one.md": "---\ntitle: BlockedOne\nstatus: todo\ntype: task\nblocked_by: [bk]\n---\n",
		"b2--blocked-two.md": "---\ntitle: BlockedTwo\nstatus: todo\ntype: task\nblocked_by: [bk]\n---\n",
		"f1--free.md":        "---\ntitle: Free\nstatus: todo\ntype: task\n---\n",
	}
}

// TestListCommand_IsBlockedFlag pins both answers of the --is-blocked
// predicate. The =false case is the regression guard: the filter layer reads a
// nil IsBlocked as "no blocked-filter", so a guard that tests the flag's value
// instead of whether it was set turns --is-blocked=false into a no-op that
// returns the entire set.
func TestListCommand_IsBlockedFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want map[string]bool
	}{
		{"--is-blocked=false returns exactly the unblocked nibs", []string{"--is-blocked=false"}, map[string]bool{"bk": true, "f1": true}},
		{"--is-blocked=true returns exactly the complement", []string{"--is-blocked=true"}, map[string]bool{"b1": true, "b2": true}},
		{"no --is-blocked returns the union", nil, map[string]bool{"bk": true, "b1": true, "b2": true, "f1": true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsDir := setupListCobraTest(t, isBlockedFixture())
			args := append([]string{"-s", "todo", "--json"}, tt.args...)
			out, err := runListCmd(t, nibsDir, args...)
			if err != nil {
				t.Fatalf("list %v failed: %v\nout: %s", args, err, out)
			}
			env := parseListEnvelope(t, out)
			got := envelopeIDs(env)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v (count=%d), want %v", got, env.Count, tt.want)
			}
			for id := range tt.want {
				if !got[id] {
					t.Errorf("missing %q; got %v, want %v", id, got, tt.want)
				}
			}
		})
	}
}

// resetListFlags clears the package-level flag vars used by listCmd AND
// Cobra's Changed-state tracking so tests don't pollute each other via
// rootCmd's singleton state. Clearing Changed state future-proofs the
// helper: listCmd's current --ready/--is-blocked mutex is implemented
// manually in list.go, but if MarkFlagsMutuallyExclusive is ever adopted
// it will read the Changed flag and would misbehave with stale state.
func resetListFlags() {
	listJSON = false
	listSearch = ""
	listStatus = nil
	listNoStatus = nil
	listType = nil
	listNoType = nil
	listPriority = nil
	listNoPriority = nil
	listEstimate = nil
	listNoEstimate = nil
	listTag = nil
	listNoTag = nil
	listHasParent = false
	listNoParent = false
	listParentID = ""
	listHasBlocking = false
	listNoBlocking = false
	listIsBlocked = false
	listMentions = ""
	listMentionedBy = ""
	listReady = false
	listAll = false
	listOpen = false
	listQuiet = false
	listSort = ""
	listView = ""
	listFields = ""
	listNoHeader = false
	listCount = false
	listLimit = 0
	listCmd.Flags().Visit(func(f *pflag.Flag) {
		f.Changed = false
	})
}

// TestResetListFlagsClearsAllState walks listCmd's Cobra FlagSet and verifies
// that every registered flag is at its documented default after resetListFlags
// runs. Unlike a hand-enumerated check, this catches additive drift: if a new
// flag is registered on listCmd but resetListFlags doesn't clear its backing
// var, the post-reset value will diverge from DefValue and this test fires.
// Also verifies Cobra's Changed state is cleared — see resetListFlags for the
// rationale.
func TestResetListFlagsClearsAllState(t *testing.T) {
	t.Cleanup(resetListFlags)

	// Dirty every flag via the real FlagSet so Cobra's `actual` map is
	// populated — Set() is the only public way to do this. Each value
	// chosen is a representative non-default (bool → "true", string →
	// a sentinel, stringArray → one element).
	dirty := map[string]string{
		"json":         "true",
		"search":       "dirty",
		"status":       "todo",
		"no-status":    "draft",
		"type":         "task",
		"no-type":      "bug",
		"priority":     "high",
		"no-priority":  "low",
		"estimate":     "m",
		"no-estimate":  "xl",
		"tag":          "idea",
		"no-tag":       "wontdo",
		"has-parent":   "true",
		"no-parent":    "true",
		"parent":       "dirty",
		"has-blocking": "true",
		"no-blocking":  "true",
		"is-blocked":   "true",
		"mentions":     "dirty",
		"mentioned-by": "dirty",
		"ready":        "true",
		"all":          "true",
		"open":         "true",
		"quiet":        "true",
		"sort":         "created",
		"view":         "card",
		"fields":       "id,title",
		"no-header":    "true",
		"count":        "true",
		"limit":        "5",
	}
	for name, val := range dirty {
		if err := listCmd.Flags().Set(name, val); err != nil {
			t.Fatalf("pre-populate --%s: %v", name, err)
		}
	}

	resetListFlags()

	// Walk LocalFlags() (not Flags()) — this test's responsibility is
	// resetListFlags hygiene only. Persistent-flag cleanup is exercised
	// independently by TestResetRootPersistentFlagsClearsAllState; mixing
	// the two would couple unrelated contracts and force this test to
	// also reset rootCmd's persistent flags.
	listCmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if f.Value.String() != f.DefValue {
			t.Errorf("flag %q = %q after reset, want default %q",
				f.Name, f.Value.String(), f.DefValue)
		}
		if f.Changed {
			t.Errorf("flag %q Changed = true after reset, want false", f.Name)
		}
	})
}

// setupListCobraTest writes nib files and returns the .nibs directory so
// `rootCmd.SetArgs(["--nibs-path", dir, "list", ...])` can drive the full
// Cobra pipeline.
func setupListCobraTest(t *testing.T, files map[string]string) string {
	t.Helper()
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetListFlags)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	// Belt-and-braces: reset rootCmd's writers in case a sibling test set
	// them via rootCmd.SetOut/SetErr and forgot to defer the reset.
	// Passing nil restores Cobra's default (os.Stdout / os.Stderr), so
	// captureStdout-based assertions in subsequent tests aren't silently
	// drained into a stale buffer.
	t.Cleanup(func() { rootCmd.SetOut(nil); rootCmd.SetErr(nil) })
	resetListFlags()

	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(nibsDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return nibsDir
}

// mentionsFixture returns a small nib-file map used by the list mention-flag
// tests. a1 mentions b2 and c3; d4 mentions a1. Statuses vary so --status
// composition can be exercised.
func mentionsFixture() map[string]string {
	return map[string]string{
		"a1--alpha.md": "---\ntitle: Alpha\nstatus: todo\ntype: task\n---\n\nSee #b2 and #c3.\n",
		"b2--beta.md":  "---\ntitle: Beta\nstatus: todo\ntype: task\n---\n\nNo refs.\n",
		"c3--gamma.md": "---\ntitle: Gamma\nstatus: completed\ntype: task\n---\n\nBackref to #a1.\n",
		"d4--delta.md": "---\ntitle: Delta\nstatus: todo\ntype: task\n---\n\nAlso mentions #a1.\n",
	}
}

// listEnvelope mirrors the {nibs,count,truncated} shape `nibs list --json`
// emits. Each projected nib carries at least its id (the ref default view), so
// the id-set assertions in the mention tests read it off .nibs[].id.
type listEnvelope struct {
	Nibs []struct {
		ID string `json:"id"`
	} `json:"nibs"`
	Count     int  `json:"count"`
	Truncated bool `json:"truncated"`
}

// parseListEnvelope unmarshals the list --json envelope, failing the test on
// error with the raw output for diagnosis.
func parseListEnvelope(t *testing.T, s string) listEnvelope {
	t.Helper()
	var env listEnvelope
	if err := json.Unmarshal([]byte(s), &env); err != nil {
		t.Fatalf("unmarshal list envelope: %v\nraw: %s", err, s)
	}
	return env
}

// envelopeIDs collects the id set from a parsed list envelope.
func envelopeIDs(env listEnvelope) map[string]bool {
	ids := map[string]bool{}
	for _, n := range env.Nibs {
		ids[n.ID] = true
	}
	return ids
}

func TestListCommand_MentionsFlag(t *testing.T) {
	// --mentions <id> → nibs whose bodies mention <id>.
	// Target nibs-a1 → mentioners are c3 (completed) and d4 (todo). --all keeps
	// the completed mentioner in the result (list is open-by-default).
	nibsDir := setupListCobraTest(t, mentionsFixture())

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "list", "--mentions", "a1", "--all", "--json"})

	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("list --mentions failed: %v", execErr)
	}

	env := parseListEnvelope(t, out)
	if env.Count != 2 {
		t.Fatalf("got count %d, want 2 (c3, d4)\nraw: %s", env.Count, out)
	}
	ids := envelopeIDs(env)
	if !ids["c3"] || !ids["d4"] {
		t.Errorf("got %v, want {c3, d4}", ids)
	}
}

func TestListCommand_MentionsFlag_ComposesWithStatus(t *testing.T) {
	// --mentions nibs-a1 --status todo → only d4 (c3 is completed).
	nibsDir := setupListCobraTest(t, mentionsFixture())

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "list",
		"--mentions", "a1", "--status", "todo", "--json"})

	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("list --mentions --status failed: %v", execErr)
	}

	env := parseListEnvelope(t, out)
	if env.Count != 1 || len(env.Nibs) != 1 || env.Nibs[0].ID != "d4" {
		t.Errorf("got count=%d nibs=%+v, want exactly [d4]", env.Count, env.Nibs)
	}
}

func TestListCommand_MentionedByFlag(t *testing.T) {
	// --mentioned-by nibs-a1 → nibs listed in a1's body: b2 and c3. --all keeps
	// c3 (completed) visible (list is open-by-default).
	nibsDir := setupListCobraTest(t, mentionsFixture())

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "list", "--mentioned-by", "a1", "--all", "--json"})

	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("list --mentioned-by failed: %v", execErr)
	}

	env := parseListEnvelope(t, out)
	if env.Count != 2 {
		t.Fatalf("got count %d, want 2 (b2, c3)\nraw: %s", env.Count, out)
	}
	ids := envelopeIDs(env)
	if !ids["b2"] || !ids["c3"] {
		t.Errorf("got %v, want {b2, c3}", ids)
	}
}

func TestListCommand_MentionsFlag_ShortIDNormalisation(t *testing.T) {
	// Passing a short-form id (without the prefix) should still resolve via
	// the GraphQL filter layer's NormalizeID path. We write an explicit
	// .nibs.yml with prefix `nibs-` and point --config at it so the loaded
	// config's prefix is honored regardless of test cwd.
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(tmpDir, ".nibs.yml")
	if err := os.WriteFile(cfgPath, []byte("nibs:\n  prefix: nibs-\n  id_length: 4\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for name, content := range mentionsFixture() {
		// Prefix filenames with `nibs-` so the ids parse as nibs-a1 etc.
		target := filepath.Join(nibsDir, "nibs-"+name)
		if err := os.WriteFile(target, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(resetListFlags)
	resetListFlags()
	t.Cleanup(func() { configPath = "" })

	// Pass the short form "a1" — filter layer should normalize to nibs-a1.
	// --all keeps the completed mentioner visible (list is open-by-default).
	rootCmd.SetArgs([]string{
		"--config", cfgPath,
		"--nibs-path", nibsDir,
		"list", "--mentions", "a1", "--all", "--json",
	})

	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("list --mentions (short id) failed: %v", execErr)
	}

	env := parseListEnvelope(t, out)
	if env.Count != 2 {
		t.Fatalf("got count %d, want 2 (nibs-c3, nibs-d4) — short-id normalisation failed\nraw: %s", env.Count, out)
	}
	ids := envelopeIDs(env)
	if !ids["nibs-c3"] || !ids["nibs-d4"] {
		t.Errorf("got %v, want {nibs-c3, nibs-d4}", ids)
	}
}

func TestListCommand_MentionsFlag_UnknownID(t *testing.T) {
	// Unknown target should yield an empty envelope, not an error. The nibs
	// array must be [] (never null) so agent consumers can index it
	// unconditionally — the empty-array convention shared by the get and rel
	// contracts.
	nibsDir := setupListCobraTest(t, mentionsFixture())

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "list", "--mentions", "nope", "--json"})

	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("list --mentions <unknown> failed: %v", execErr)
	}
	env := parseListEnvelope(t, out)
	if env.Count != 0 || len(env.Nibs) != 0 || env.Truncated {
		t.Errorf("got count=%d nibs=%+v truncated=%v, want an empty envelope", env.Count, env.Nibs, env.Truncated)
	}
	// The nibs field must serialize as [] (not null) for stable indexing.
	if !strings.Contains(out, "\"nibs\": []") {
		t.Errorf("empty list --json must render \"nibs\": [], got:\n%s", out)
	}
}

// projectionFixture is a small two-nib file set with distinct types/statuses
// for the projection/envelope tests. a1 = todo task, b2 = in-progress bug.
func projectionFixture() map[string]string {
	return map[string]string{
		"a1--alpha.md": "---\ntitle: Alpha\nstatus: todo\ntype: task\n---\n\nAlpha body.\n",
		"b2--beta.md":  "---\ntitle: Beta\nstatus: in-progress\ntype: bug\n---\n\nBeta body.\n",
	}
}

// runListCmd drives `nibs list <args...>` through the full Cobra pipeline
// against nibsDir and returns captured stdout plus the Execute error.
func runListCmd(t *testing.T, nibsDir string, args ...string) (string, error) {
	t.Helper()
	rootCmd.SetArgs(append([]string{"--nibs-path", nibsDir, "list"}, args...))
	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	return out, execErr
}

// deferredBlockerFixture pairs two todo nibs, each blocked by exactly one
// closed blocker: one completed (the dependency happened) and one deferred (the
// dependency was set aside and is coming back). Both blockers are closed, so
// only a predicate narrower than IsClosedStatus tells them apart.
func deferredBlockerFixture() map[string]string {
	return map[string]string{
		"bd--blocker-done.md":     "---\ntitle: BlockerDone\nstatus: completed\ntype: task\n---\n",
		"bp--blocker-deferred.md": "---\ntitle: BlockerDeferred\nstatus: deferred\ntype: task\n---\n",
		"dd--dep-of-done.md":      "---\ntitle: DepOfDone\nstatus: todo\ntype: task\nblocked_by: [bd]\n---\n",
		"dp--dep-of-deferred.md":  "---\ntitle: DepOfDeferred\nstatus: todo\ntype: task\nblocked_by: [bp]\n---\n",
	}
}

// TestListCommand_DeferredBlockerStillBlocks pins the rule that separates
// "closed" from "releases its dependents": deferring a blocker must NOT hand
// its dependents out as startable work. --ready is the agent work queue, so a
// nib blocked only by a deferred nib has to stay out of it and report
// ready:false, while one blocked only by completed work is released.
//
// Both blockers are closed. Routing the blocking graph through IsClosedStatus
// instead of StatusReleasesDependents makes dp ready and puts it in --ready.
func TestListCommand_DeferredBlockerStillBlocks(t *testing.T) {
	t.Run("--ready excludes a nib blocked by a deferred nib", func(t *testing.T) {
		nibsDir := setupListCobraTest(t, deferredBlockerFixture())
		out, err := runListCmd(t, nibsDir, "--ready", "-f", "id")
		if err != nil {
			t.Fatalf("list --ready failed: %v\nout: %s", err, out)
		}
		if strings.Contains(out, "dp") {
			t.Errorf("--ready returned dp, which is blocked by a deferred nib\nout: %s", out)
		}
		if !strings.Contains(out, "dd") {
			t.Errorf("--ready omitted dd, whose only blocker is completed\nout: %s", out)
		}
	})

	t.Run("ready projection reports false and still lists the blocker", func(t *testing.T) {
		nibsDir := setupListCobraTest(t, deferredBlockerFixture())
		out, err := runListCmd(t, nibsDir, "--json", "-f", "id,ready,blocked-by")
		if err != nil {
			t.Fatalf("list --json failed: %v\nout: %s", err, out)
		}
		var env struct {
			Nibs []struct {
				ID        string   `json:"id"`
				Ready     bool     `json:"ready"`
				BlockedBy []string `json:"blocked_by"`
			} `json:"nibs"`
		}
		if err := json.Unmarshal([]byte(out), &env); err != nil {
			t.Fatalf("unmarshal envelope: %v\nraw: %s", err, out)
		}
		byID := map[string]struct {
			ready     bool
			blockedBy []string
		}{}
		for _, n := range env.Nibs {
			byID[n.ID] = struct {
				ready     bool
				blockedBy []string
			}{n.Ready, n.BlockedBy}
		}
		dp, ok := byID["dp"]
		if !ok {
			t.Fatalf("dp missing from the open-default listing\nraw: %s", out)
		}
		if dp.ready {
			t.Errorf("dp ready = true, want false — its only blocker is deferred, not satisfied")
		}
		// blocked-by projects the declared list off the nib, unfiltered, so bp
		// is reported whatever the blocking rule says. That is what made the old
		// behavior self-contradictory: ready:true printed next to a live
		// blocker. Pinned here so the pair stays coherent.
		if len(dp.blockedBy) != 1 || dp.blockedBy[0] != "bp" {
			t.Errorf("dp blocked_by = %v, want [bp]", dp.blockedBy)
		}
		dd, ok := byID["dd"]
		if !ok {
			t.Fatalf("dd missing from the open-default listing\nraw: %s", out)
		}
		if !dd.ready {
			t.Errorf("dd ready = false, want true — its only blocker completed")
		}
	})
}

// TestListCommand_TSVDefault projects an explicit field set to TSV rows under
// the "# <n> nibs" header, with no body column. -f given alone (no --view)
// yields exactly the requested fields.
func TestListCommand_TSVDefault(t *testing.T) {
	nibsDir := setupListCobraTest(t, projectionFixture())

	out, err := runListCmd(t, nibsDir, "-f", "id,status,title")
	if err != nil {
		t.Fatalf("list -f failed: %v\nout: %s", err, out)
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3 (header + 2 rows)\nraw: %q", len(lines), out)
	}
	if lines[0] != "# 2 nibs" {
		t.Errorf("header = %q, want %q", lines[0], "# 2 nibs")
	}
	// Rows carry exactly the three requested columns (in menu order:
	// id, title, status), never a body.
	for _, line := range lines[1:] {
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			t.Errorf("row %q has %d tab-fields, want 3", line, len(fields))
		}
	}
	if strings.Contains(out, "body") {
		t.Errorf("TSV output leaked a body column:\n%s", out)
	}
	// Menu order places title before status, so each row is id\ttitle\tstatus.
	for _, expect := range []string{"a1\tAlpha\ttodo", "b2\tBeta\tin-progress"} {
		if !strings.Contains(out, expect) {
			t.Errorf("output missing row %q\nraw: %q", expect, out)
		}
	}
}

// TestListCommand_NoHeader drops the "# <n> nibs" comment line.
func TestListCommand_NoHeader(t *testing.T) {
	nibsDir := setupListCobraTest(t, projectionFixture())

	out, err := runListCmd(t, nibsDir, "-f", "id,title", "--no-header")
	if err != nil {
		t.Fatalf("list --no-header failed: %v\nout: %s", err, out)
	}
	if strings.HasPrefix(out, "#") || strings.Contains(out, "nibs\n") {
		t.Errorf("--no-header still emitted a header:\n%s", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2 rows (no header)\nraw: %q", len(lines), out)
	}
}

// TestListCommand_JSONEnvelope asserts the {nibs,count,truncated} shape.
func TestListCommand_JSONEnvelope(t *testing.T) {
	nibsDir := setupListCobraTest(t, projectionFixture())

	out, err := runListCmd(t, nibsDir, "--json")
	if err != nil {
		t.Fatalf("list --json failed: %v\nout: %s", err, out)
	}
	env := parseListEnvelope(t, out)
	if env.Count != 2 {
		t.Errorf("count = %d, want 2", env.Count)
	}
	if env.Truncated {
		t.Errorf("truncated = true, want false")
	}
	if len(env.Nibs) != 2 {
		t.Fatalf("got %d nibs, want 2\nraw: %s", len(env.Nibs), out)
	}
	// The three envelope keys must all be present (byte-shape shared with rel).
	for _, key := range []string{"\"nibs\"", "\"count\"", "\"truncated\""} {
		if !strings.Contains(out, key) {
			t.Errorf("envelope missing key %s\nraw: %s", key, out)
		}
	}
}

// TestListCommand_Count emits a bare integer, independent of --json and the
// projection selection.
func TestListCommand_Count(t *testing.T) {
	nibsDir := setupListCobraTest(t, projectionFixture())

	out, err := runListCmd(t, nibsDir, "-c")
	if err != nil {
		t.Fatalf("list -c failed: %v\nout: %s", err, out)
	}
	if strings.TrimSpace(out) != "2" {
		t.Errorf("list -c = %q, want bare integer %q", strings.TrimSpace(out), "2")
	}
}

// TestListCommand_CountRespectsFilter counts the filtered set, not all nibs.
func TestListCommand_CountRespectsFilter(t *testing.T) {
	nibsDir := setupListCobraTest(t, projectionFixture())

	out, err := runListCmd(t, nibsDir, "-t", "task", "-c")
	if err != nil {
		t.Fatalf("list -t task -c failed: %v\nout: %s", err, out)
	}
	if strings.TrimSpace(out) != "1" {
		t.Errorf("list -t task -c = %q, want %q", strings.TrimSpace(out), "1")
	}
}

// TestListCommand_TypeFilterProjected combines a type filter with an explicit
// projection: only the matching type, only the requested fields.
func TestListCommand_TypeFilterProjected(t *testing.T) {
	nibsDir := setupListCobraTest(t, projectionFixture())

	out, err := runListCmd(t, nibsDir, "-t", "bug", "-f", "id,title", "--json")
	if err != nil {
		t.Fatalf("list -t bug -f failed: %v\nout: %s", err, out)
	}
	env := parseListEnvelope(t, out)
	if env.Count != 1 || len(env.Nibs) != 1 || env.Nibs[0].ID != "b2" {
		t.Fatalf("got count=%d nibs=%+v, want exactly [b2]\nraw: %s", env.Count, env.Nibs, out)
	}
}

// TestListCommand_Quiet prints ids only, one per line.
func TestListCommand_Quiet(t *testing.T) {
	nibsDir := setupListCobraTest(t, projectionFixture())

	out, err := runListCmd(t, nibsDir, "-q")
	if err != nil {
		t.Fatalf("list -q failed: %v\nout: %s", err, out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2 ids\nraw: %q", len(lines), out)
	}
	got := map[string]bool{lines[0]: true, lines[1]: true}
	if !got["a1"] || !got["b2"] {
		t.Errorf("got ids %v, want {a1, b2}", got)
	}
	// Quiet is ids only — no header, no tab-separated columns.
	if strings.Contains(out, "#") || strings.Contains(out, "\t") {
		t.Errorf("quiet output must be bare ids, got:\n%s", out)
	}
}

// TestListCommand_Limit projects only the first N and marks the envelope
// truncated. count reflects the projected (post-limit) size; the bare -c count
// stays the true pre-limit total.
func TestListCommand_Limit(t *testing.T) {
	nibsDir := setupListCobraTest(t, projectionFixture())

	out, err := runListCmd(t, nibsDir, "--limit", "1", "--json")
	if err != nil {
		t.Fatalf("list --limit failed: %v\nout: %s", err, out)
	}
	env := parseListEnvelope(t, out)
	if len(env.Nibs) != 1 {
		t.Fatalf("got %d nibs, want 1 (limited)\nraw: %s", len(env.Nibs), out)
	}
	if env.Count != 1 {
		t.Errorf("envelope count = %d, want 1 (post-limit size)", env.Count)
	}
	if !env.Truncated {
		t.Errorf("truncated = false, want true (a row was dropped)")
	}

	// The bare -c count is the true pre-limit total, unaffected by --limit.
	cout, cerr := runListCmd(t, nibsDir, "--limit", "1", "-c")
	if cerr != nil {
		t.Fatalf("list --limit -c failed: %v", cerr)
	}
	if strings.TrimSpace(cout) != "2" {
		t.Errorf("list --limit 1 -c = %q, want true count %q", strings.TrimSpace(cout), "2")
	}
}

// TestListCommand_BadFields rejects an unknown -f token with a VALIDATION
// error naming the field menu. Non-JSON mode leaks no envelope to stdout.
func TestListCommand_BadFields(t *testing.T) {
	nibsDir := setupListCobraTest(t, projectionFixture())

	out, err := runListCmd(t, nibsDir, "-f", "bogus")
	if err == nil {
		t.Fatalf("list -f bogus should have failed; out: %q", out)
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error %q does not name the unknown field", err.Error())
	}
	// The field menu must appear so callers can recover.
	for _, f := range []string{"id", "title", "status"} {
		if !strings.Contains(err.Error(), f) {
			t.Errorf("error %q missing menu field %q", err.Error(), f)
		}
	}
	if strings.Contains(out, "\"code\"") || strings.Contains(out, "\"nibs\"") {
		t.Errorf("non-JSON mode leaked JSON to stdout: %q", out)
	}
}

// TestListCommand_BadFieldsJSON exercises the JSON-mode validation path: the
// {error:{code,message}} envelope on stdout plus a non-nil error carrying the
// VALIDATION_ERROR code.
func TestListCommand_BadFieldsJSON(t *testing.T) {
	nibsDir := setupListCobraTest(t, projectionFixture())

	out, err := runListCmd(t, nibsDir, "-f", "bogus", "--json")
	if err == nil {
		t.Fatalf("list -f bogus --json should have failed; out: %q", out)
	}
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if uerr := json.Unmarshal([]byte(out), &env); uerr != nil {
		t.Fatalf("stdout is not a JSON error envelope: %v\nraw: %s", uerr, out)
	}
	if env.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("envelope error.code=%q, want VALIDATION_ERROR", env.Error.Code)
	}
	if !strings.Contains(env.Error.Message, "bogus") {
		t.Errorf("envelope error.message %q does not name the bad field", env.Error.Message)
	}
}

// TestListCommand_BadView rejects an unknown --view with a VALIDATION error
// naming the valid views.
func TestListCommand_BadView(t *testing.T) {
	nibsDir := setupListCobraTest(t, projectionFixture())

	out, err := runListCmd(t, nibsDir, "--view", "bogus")
	if err == nil {
		t.Fatalf("list --view bogus should have failed; out: %q", out)
	}
	for _, v := range []string{"id", "ref", "card", "full"} {
		if !strings.Contains(err.Error(), v) {
			t.Errorf("error %q missing valid view %q", err.Error(), v)
		}
	}
}
