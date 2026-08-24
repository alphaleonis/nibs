package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
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
		"bk--blocker.md":     "---\nversion: 2\ntitle: Blocker\nstatus: todo\ntype: task\n---\n",
		"b1--blocked-one.md": "---\nversion: 2\ntitle: BlockedOne\nstatus: todo\ntype: task\nblocked_by: [bk]\n---\n",
		"b2--blocked-two.md": "---\nversion: 2\ntitle: BlockedTwo\nstatus: todo\ntype: task\nblocked_by: [bk]\n---\n",
		"f1--free.md":        "---\nversion: 2\ntitle: Free\nstatus: todo\ntype: task\n---\n",
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

// presenceFixture gives both the parent and the blocking predicate a non-empty
// answer on each side. pa is a root with two children (ca, cb); rb is a root
// that blocks cb and is therefore the only blocking nib. Every nib is todo, so
// list's open-by-default filter keeps the whole set in play and each pair of
// answers is an exact complement.
func presenceFixture() map[string]string {
	return map[string]string{
		"pa--parent-a.md": "---\nversion: 2\ntitle: ParentA\nstatus: todo\ntype: task\n---\n",
		"ca--child-a.md":  "---\nversion: 2\ntitle: ChildA\nstatus: todo\ntype: task\nparent: pa\n---\n",
		"cb--child-b.md":  "---\nversion: 2\ntitle: ChildB\nstatus: todo\ntype: task\nparent: pa\nblocked_by: [rb]\n---\n",
		"rb--root-b.md":   "---\nversion: 2\ntitle: RootB\nstatus: todo\ntype: task\n---\n",
	}
}

// TestListCommand_PresenceFlagsAreOneTriStateField pins that each flag pair is
// two spellings of one tri-state filter field. --has-parent=false has to select
// the parentless nibs and agree with --no-parent exactly; --no-parent=false has
// to agree with --has-parent. A guard that reads the flag's value instead of
// whether it was set leaves the field nil on the =false rows, which the filter
// layer reads as "no filter" and hands back the entire set.
func TestListCommand_PresenceFlagsAreOneTriStateField(t *testing.T) {
	var (
		withParent    = map[string]bool{"ca": true, "cb": true}
		withoutParent = map[string]bool{"pa": true, "rb": true}
		blocking      = map[string]bool{"rb": true}
		notBlocking   = map[string]bool{"pa": true, "ca": true, "cb": true}
	)

	tests := []struct {
		name string
		args []string
		want map[string]bool
	}{
		{"--has-parent", []string{"--has-parent"}, withParent},
		{"--has-parent=true", []string{"--has-parent=true"}, withParent},
		{"--has-parent=false", []string{"--has-parent=false"}, withoutParent},
		{"--no-parent", []string{"--no-parent"}, withoutParent},
		{"--no-parent=true", []string{"--no-parent=true"}, withoutParent},
		{"--no-parent=false", []string{"--no-parent=false"}, withParent},
		{"--has-blocking", []string{"--has-blocking"}, blocking},
		{"--has-blocking=false", []string{"--has-blocking=false"}, notBlocking},
		{"--no-blocking", []string{"--no-blocking"}, notBlocking},
		{"--no-blocking=false", []string{"--no-blocking=false"}, blocking},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsDir := setupListCobraTest(t, presenceFixture())
			args := append([]string{"--json"}, tt.args...)
			out, err := runListCmd(t, nibsDir, args...)
			if err != nil {
				t.Fatalf("list %v failed: %v\nout: %s", args, err, out)
			}
			got := envelopeIDs(parseListEnvelope(t, out))
			if len(got) != len(tt.want) {
				t.Fatalf("got %v (count=%d), want %v", got, len(got), tt.want)
			}
			for id := range tt.want {
				if !got[id] {
					t.Errorf("missing %q; got %v, want %v", id, got, tt.want)
				}
			}
		})
	}
}

// TestListCommand_PresenceFlagMutualExclusion drives the real command so the
// assertion lands on list.go's guard rather than a copy of it. Both spellings
// write the same field, so giving both spells one filter concept twice in a
// single invocation — redundant and near-certainly a mistake. The rejection is
// uniform rather than conditional on the values disagreeing, which is why the
// agreeing-values rows are rejected too even though they are unambiguous.
func TestListCommand_PresenceFlagMutualExclusion(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantError bool
		wantFlags []string
	}{
		{"only --has-parent", []string{"--has-parent"}, false, nil},
		{"only --no-parent=false", []string{"--no-parent=false"}, false, nil},
		{"only --has-blocking=false", []string{"--has-blocking=false"}, false, nil},
		{"only --no-blocking", []string{"--no-blocking"}, false, nil},
		{"--has-parent --no-parent", []string{"--has-parent", "--no-parent"}, true, []string{"--has-parent", "--no-parent"}},
		// Agreeing values: --has-parent=false and --no-parent both mean
		// "parentless", so this pair has exactly one possible meaning — and is
		// still rejected, because the rejection is uniform.
		{"--has-parent=false --no-parent", []string{"--has-parent=false", "--no-parent"}, true, []string{"--has-parent", "--no-parent"}},
		{"--has-blocking --no-blocking", []string{"--has-blocking", "--no-blocking"}, true, []string{"--has-blocking", "--no-blocking"}},
		{"--has-blocking=false --no-blocking", []string{"--has-blocking=false", "--no-blocking"}, true, []string{"--has-blocking", "--no-blocking"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsDir := setupListCobraTest(t, presenceFixture())
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
			for _, flag := range tt.wantFlags {
				if !strings.Contains(err.Error(), flag) {
					t.Errorf("list %v: error should name %s; got: %v", tt.args, flag, err)
				}
			}
			if !strings.Contains(err.Error(), "mutually exclusive") {
				t.Errorf("list %v: error should say the flags are mutually exclusive; got: %v", tt.args, err)
			}
		})
	}
}

// TestListCommand_ParentIDVersusPresenceFlags drives the real command so the
// assertions land on list.go's guards rather than a copy of them.
//
// --parent <id> and the has-parent field are separate constraints — "parent is
// <id>" versus "has a parent at all" — so the rejection is not uniform the way
// the two spellings of one field are. Only the combination nothing can satisfy
// is rejected: --parent <id> alongside a has-parent that resolved to false,
// however it was spelled. --parent <id> --has-parent is redundant but
// satisfiable, and keeps returning what --parent <id> returns on its own.
//
// The empty --parent row is a separate guard: `--parent ""` used to fall
// through the `!= ""` check that decides whether to set the filter, leaving the
// flag with no effect and no diagnostic.
// presenceFixtureAll is every nib in presenceFixture — what `list` returns when
// no filter applies. All four are todo, so the open-by-default status filter
// keeps the whole set and this is also the unfiltered answer.
func presenceFixtureAll() map[string]bool {
	return map[string]bool{"pa": true, "ca": true, "cb": true, "rb": true}
}

func TestListCommand_ParentIDVersusPresenceFlags(t *testing.T) {
	// pa's children in presenceFixture. This is the baseline `--parent pa`
	// returns on its own, and what the redundant-but-satisfiable rows have to
	// return too.
	childrenOfPA := map[string]bool{"ca": true, "cb": true}

	tests := []struct {
		name string
		args []string
		// wantIDs is the id set the command must return when it is accepted.
		wantIDs map[string]bool
		// wantError marks the rows that must be rejected; wantText lists
		// substrings the rejection message has to contain.
		wantError bool
		wantText  []string
	}{
		{"--parent alone", []string{"--parent", "pa"}, childrenOfPA, false, nil},
		{"--parent with --has-parent", []string{"--parent", "pa", "--has-parent"}, childrenOfPA, false, nil},
		{"--parent with --has-parent=true", []string{"--parent", "pa", "--has-parent=true"}, childrenOfPA, false, nil},
		{"--parent with --no-parent=false", []string{"--parent", "pa", "--no-parent=false"}, childrenOfPA, false, nil},
		{"--parent with --no-parent", []string{"--parent", "pa", "--no-parent"}, nil, true, []string{"--parent", "--no-parent"}},
		{"--parent with --no-parent=true", []string{"--parent", "pa", "--no-parent=true"}, nil, true, []string{"--parent", "--no-parent"}},
		{"--parent with --has-parent=false", []string{"--parent", "pa", "--has-parent=false"}, nil, true, []string{"--parent", "--has-parent"}},
		// The empty --parent message points at --no-parent, the flag that does
		// select root-level nibs.
		{"empty --parent", []string{"--parent", ""}, nil, true, []string{"--parent", "--no-parent"}},
		// The other id-valued filters reject an empty value for the same reason:
		// there is no nib whose id is "", so the usual source is an unset shell
		// variable, and returning every nib would be a lie about the result.
		{"empty --mentions", []string{"--mentions", ""}, nil, true, []string{"--mentions", "nib id"}},
		{"empty --mentioned-by", []string{"--mentioned-by", ""}, nil, true, []string{"--mentioned-by", "nib id"}},
		// -S is deliberately NOT in that group: an empty search means "no keyword
		// filter", which is a real thing to want, and the web reads it the same
		// way. It must keep returning the unfiltered set rather than erroring.
		{"empty -S is accepted as no search", []string{"-S", ""}, presenceFixtureAll(), false, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsDir := setupListCobraTest(t, presenceFixture())
			args := append([]string{"--json"}, tt.args...)
			out, err := runListCmd(t, nibsDir, args...)

			if !tt.wantError {
				if err != nil {
					t.Fatalf("list %v failed: %v\nout: %s", args, err, out)
				}
				got := envelopeIDs(parseListEnvelope(t, out))
				if len(got) != len(tt.wantIDs) {
					t.Fatalf("list %v: got %v (count=%d), want %v", args, got, len(got), tt.wantIDs)
				}
				for id := range tt.wantIDs {
					if !got[id] {
						t.Errorf("list %v: missing %q; got %v, want %v", args, id, got, tt.wantIDs)
					}
				}
				return
			}

			if err == nil {
				t.Fatalf("list %v: want a validation error, got nil\nout: %s", args, out)
			}
			var ce *output.CodedError
			if !errors.As(err, &ce) || ce.Code != output.ErrValidation {
				t.Errorf("list %v: want a VALIDATION coded error, got: %v", args, err)
			}
			for _, want := range tt.wantText {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("list %v: error should name %s; got: %v", args, want, err)
				}
			}
		})
	}
}

// resetListFlags clears the package-level flag vars used by listCmd AND
// Cobra's Changed-state tracking so tests don't pollute each other via
// rootCmd's singleton state. Clearing Changed state is load-bearing today,
// not future-proofing: list.go decides whether a filter applies from
// cmd.Flags().Changed — once for --is-blocked and once per spelling inside
// resolvePresenceFlag — so a flag left Changed makes a later test see flags
// it never passed. Leaving it stale is what turns an earlier
// --has-parent/--no-parent case into a mutual-exclusion error in the list
// tests that run after it.
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
	listMilestone = ""
	listBacklog = false
	listArea = ""
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
		"milestone":    "dirty",
		"backlog":      "true",
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

// setupListCobraTestWithPrefix is setupListCobraTest plus a config carrying an
// explicit prefix, written INSIDE the store. Tests that need the short
// (prefix-less) spelling of an id use it so the prefix under test is the
// fixture's own rather than whatever project config the test cwd happens to sit
// under. Returns the store directory and the config path to pass as --config.
func setupListCobraTestWithPrefix(t *testing.T, prefix string, files map[string]string) (nibsDir, cfgPath string) {
	t.Helper()
	nibsDir = setupListCobraTest(t, files)
	cfgPath = filepath.Join(nibsDir, "config.yml")
	body := fmt.Sprintf("nibs:\n  prefix: %s\n  id_length: 4\n", prefix)
	if err := os.WriteFile(cfgPath, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return nibsDir, cfgPath
}

// mentionsFixture returns a small nib-file map used by the list mention-flag
// tests. a1 mentions b2 and c3; d4 mentions a1. Statuses vary so --status
// composition can be exercised.
func mentionsFixture() map[string]string {
	return map[string]string{
		"a1--alpha.md": "---\nversion: 2\ntitle: Alpha\nstatus: todo\ntype: task\n---\n\nSee #b2 and #c3.\n",
		"b2--beta.md":  "---\nversion: 2\ntitle: Beta\nstatus: todo\ntype: task\n---\n\nNo refs.\n",
		"c3--gamma.md": "---\nversion: 2\ntitle: Gamma\nstatus: completed\ntype: task\n---\n\nBackref to #a1.\n",
		"d4--delta.md": "---\nversion: 2\ntitle: Delta\nstatus: todo\ntype: task\n---\n\nAlso mentions #a1.\n",
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
	// the GraphQL filter layer's NormalizeID path. The store carries its own
	// config with prefix `nibs-`, so that prefix is honored regardless of the
	// test cwd.
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(storeDataDir(nibsDir), 0755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(nibsDir, "config.yml")
	if err := os.WriteFile(cfgPath, []byte("nibs:\n  prefix: nibs-\n  id_length: 4\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for name, content := range mentionsFixture() {
		// Prefix filenames with `nibs-` so the ids parse as nibs-a1 etc.
		target := dataPath(nibsDir, "nibs-"+name)
		if err := os.WriteFile(target, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(resetListFlags)
	resetListFlags()
	t.Cleanup(func() { configPath = "" })

	// Pass the short form "a1" — filter layer should normalize to nibs-a1.
	// --all keeps the completed mentioner visible (list is open-by-default).
	// --config alone names the store (the config lives inside it); the two
	// flags together are refused.
	rootCmd.SetArgs([]string{
		"--config", cfgPath,
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

// TestListCommand_UnknownFilterTargetIsNotFound is the CLI end of the
// filter-target contract. Every id-valued list flag names one nib, so an id no
// nib answers to is a question the command cannot answer — it must say so and
// exit 3, not print an empty listing and exit 0.
//
// Exit 0 with zero rows would be worse than a plain error here: an agent
// scripting `--parent "$ID"` reads an empty exit-0 listing as "that nib has no
// children" and moves on, so a stale or mistyped id never surfaces.
//
// The assertion is on the exit status via the real boundary (reportExitError),
// not merely on err != nil: the whole value of the change is that a caller can
// branch on $? without parsing text.
func TestListCommand_UnknownFilterTargetIsNotFound(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"--parent", []string{"--parent", "nope"}},
		{"--mentions", []string{"--mentions", "nope"}},
		{"--mentioned-by", []string{"--mentioned-by", "nope"}},
	}

	for _, tt := range tests {
		t.Run(tt.name+" --json", func(t *testing.T) {
			nibsDir := setupListCobraTest(t, mentionsFixture())
			out, err := runListCmd(t, nibsDir, append(append([]string{}, tt.args...), "--json")...)
			if err == nil {
				t.Fatalf("list %v --json returned no error; out: %q", tt.args, out)
			}
			if code := reportExitError(io.Discard, err); code != output.ExitNotFound {
				t.Errorf("exit code = %d, want %d (NOT_FOUND)", code, output.ExitNotFound)
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
			if env.Error.Code != output.ErrNotFound {
				t.Errorf("envelope error.code=%q, want %q", env.Error.Code, output.ErrNotFound)
			}
			if !strings.Contains(env.Error.Message, "nope") {
				t.Errorf("envelope error.message %q does not echo the id that was not found", env.Error.Message)
			}
			// A rejected query must not also emit a listing: an agent parsing
			// stdout would otherwise see a valid-looking empty result.
			if strings.Contains(out, `"nibs"`) {
				t.Errorf("error envelope must not carry a nibs listing:\n%s", out)
			}
		})

		t.Run(tt.name+" text", func(t *testing.T) {
			nibsDir := setupListCobraTest(t, mentionsFixture())
			out, err := runListCmd(t, nibsDir, tt.args...)
			if err == nil {
				t.Fatalf("list %v returned no error; out: %q", tt.args, out)
			}
			if code := reportExitError(io.Discard, err); code != output.ExitNotFound {
				t.Errorf("exit code = %d, want %d (NOT_FOUND)", code, output.ExitNotFound)
			}
			if strings.Contains(out, "nibs") {
				t.Errorf("text mode must not print the TSV header for a rejected query:\n%s", out)
			}
		})
	}
}

// projectionFixture is a small two-nib file set with distinct types/statuses
// for the projection/envelope tests. a1 = todo task, b2 = in-progress bug.
func projectionFixture() map[string]string {
	return map[string]string{
		"a1--alpha.md": "---\nversion: 2\ntitle: Alpha\nstatus: todo\ntype: task\n---\n\nAlpha body.\n",
		"b2--beta.md":  "---\nversion: 2\ntitle: Beta\nstatus: in-progress\ntype: bug\n---\n\nBeta body.\n",
	}
}

// runListCmdWithConfig is runListCmd with an explicit --config, for tests whose
// fixture supplies its own project config (see setupListCobraTestWithPrefix).
// --config alone resolves the store as the config file's containing directory,
// and combining it with --nibs-path is refused.
func runListCmdWithConfig(t *testing.T, cfgPath, nibsDir string, args ...string) (string, error) {
	t.Helper()
	_ = nibsDir
	rootCmd.SetArgs(append([]string{"--config", cfgPath, "list"}, args...))
	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	return out, execErr
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
		"bd--blocker-done.md":     "---\nversion: 2\ntitle: BlockerDone\nstatus: completed\ntype: task\n---\n",
		"bp--blocker-deferred.md": "---\nversion: 2\ntitle: BlockerDeferred\nstatus: deferred\ntype: task\n---\n",
		"dd--dep-of-done.md":      "---\nversion: 2\ntitle: DepOfDone\nstatus: todo\ntype: task\nblocked_by: [bd]\n---\n",
		"dp--dep-of-deferred.md":  "---\nversion: 2\ntitle: DepOfDeferred\nstatus: todo\ntype: task\nblocked_by: [bp]\n---\n",
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

// readyAgreementCases is the expectation table for "can I start this?": every
// declared status plus the out-of-vocabulary one, with the single answer both
// surfaces owe a nib carrying it. The values are literal rather than read back
// from config.Startable, so flipping that flag has to be restated here
// deliberately instead of quietly carrying both surfaces with it.
//
// The "" row is not decoration. On the declared statuses the old spelling of
// --ready (exclude in-progress/draft/deferred/completed/scrapped) picks out the
// same nibs the startable set does — that equivalence is why the two were worth
// unifying at all — so nothing inside the vocabulary can tell the two spellings
// apart. Only a status neither one names can: an exclusion list cannot mention
// "", so it hands the nib back as ready work, while a startable include list
// leaves it out.
var readyAgreementCases = []struct {
	status string
	want   bool
}{
	{"todo", true},
	{"in-progress", false}, // already underway; not something to start
	{"draft", false},       // needs refinement first
	{"deferred", false},    // closed
	{"completed", false},
	{"scrapped", false},
	{"", false}, // front matter with no status: — outside the vocabulary
}

// TestReadyAgreementCasesCoverEveryStatus fails when a status is added to the
// vocabulary without an entry in the table above, so the agreement guard cannot
// quietly stop being exhaustive — and fails on a case naming something that is
// neither a declared status nor the deliberate "" probe, so the table cannot
// drift into testing statuses a nib could never carry.
func TestReadyAgreementCasesCoverEveryStatus(t *testing.T) {
	cfg := config.Default()
	covered := map[string]bool{}
	for _, tc := range readyAgreementCases {
		covered[tc.status] = true
	}
	for _, name := range cfg.StatusNames() {
		if !covered[name] {
			t.Errorf("status %q has no case in readyAgreementCases; add one", name)
		}
	}
	for status := range covered {
		if status == "" {
			continue // the deliberate out-of-vocabulary probe
		}
		if !cfg.IsValidStatus(status) {
			t.Errorf("readyAgreementCases names %q, which is not a declared status", status)
		}
	}
	if !covered[""] {
		t.Error(`readyAgreementCases lost its "" case — without it the table cannot tell a startable include list from a non-startable exclusion list`)
	}
}

// readyAgreementFrontMatter renders one fixture nib's front matter. A case with
// no status omits the key entirely rather than writing `status: ""`, because
// that is how a hand-edited nib actually carries no status.
func readyAgreementFrontMatter(title, status, extra string) string {
	line := ""
	if status != "" {
		line = fmt.Sprintf("status: %s\n", status)
	}
	return fmt.Sprintf("---\nversion: 2\ntitle: %s\n%stype: task\n%s---\n", title, line, extra)
}

// TestReadyProjectionAndFilterAgree is the agreement guard between the two
// surfaces that answer "can I start this?": the projected `ready` field and the
// `nibs list --ready` filter. They used to give different answers — the
// projection asked only whether a nib was unfinished, so it reported drafts and
// work already in progress as ready while the filter withheld them.
//
// Both surfaces are driven for real, through separate `nibs list` invocations
// against the same store, and each is compared to the literal table above
// rather than to the other. Comparing them only to each other would pass if
// both regressed together; comparing each to the table means reverting either
// one on its own fails here.
//
// Each status gets an unblocked nib and two blocked twins, so the status half
// and the blocker half are exercised for every status: the twins' blocker is
// open, so it holds whatever the twins' own status is. The two twins differ
// only in how they spell that one blocker — `nibs-bkr` in full and `bkr` in
// short form — because the blocker half is where the two surfaces resolve ids,
// and a table that only ever spelled the blocker in full could not tell an
// exact map lookup from one that normalizes.
//
// The prefix is supplied by a fixture .nibs.yml passed as --config rather than
// inherited from whatever project config the test cwd sits under, so "bkr" is
// a genuinely short id here no matter where the suite runs.
func TestReadyProjectionAndFilterAgree(t *testing.T) {
	// Carries the configured prefix, so the same blocker has both a full and a
	// short spelling.
	const blockerID = "nibs-bkr"
	fixture := map[string]string{
		blockerID + "--blocker.md": "---\nversion: 2\ntitle: Blocker\nstatus: todo\ntype: task\n---\n",
	}
	unblockedID := make([]string, len(readyAgreementCases))
	blockedID := make([]string, len(readyAgreementCases))
	shortBlockedID := make([]string, len(readyAgreementCases))
	for i, tc := range readyAgreementCases {
		unblockedID[i] = fmt.Sprintf("u%d", i)
		blockedID[i] = fmt.Sprintf("b%d", i)
		shortBlockedID[i] = fmt.Sprintf("s%d", i)
		fixture[unblockedID[i]+"--unblocked.md"] = readyAgreementFrontMatter("Unblocked", tc.status, "")
		fixture[blockedID[i]+"--blocked.md"] = readyAgreementFrontMatter("Blocked", tc.status, "blocked_by: ["+blockerID+"]\n")
		fixture[shortBlockedID[i]+"--short-blocked.md"] = readyAgreementFrontMatter("ShortBlocked", tc.status, "blocked_by: [bkr]\n")
	}
	nibsDir, cfgPath := setupListCobraTestWithPrefix(t, "nibs-", fixture)

	// Surface 1: the projected `ready` field over every nib, whatever its
	// status (--all, so the open default hides none of them).
	projOut, err := runListCmdWithConfig(t, cfgPath, nibsDir, "--all", "--json", "-f", "id,ready")
	if err != nil {
		t.Fatalf("list --all --json failed: %v\nout: %s", err, projOut)
	}
	var projEnv struct {
		Nibs []struct {
			ID    string `json:"id"`
			Ready bool   `json:"ready"`
		} `json:"nibs"`
	}
	if err := json.Unmarshal([]byte(projOut), &projEnv); err != nil {
		t.Fatalf("unmarshal projection envelope: %v\nraw: %s", err, projOut)
	}
	if len(projEnv.Nibs) != len(fixture) {
		t.Fatalf("projection returned %d nibs, want all %d — the two surfaces must see the same store",
			len(projEnv.Nibs), len(fixture))
	}
	projReady := make(map[string]bool, len(projEnv.Nibs))
	for _, n := range projEnv.Nibs {
		projReady[n.ID] = n.Ready
	}

	// Surface 2: the --ready filter, as a second run against the same store.
	// The flag state Cobra accumulated above has to be cleared first, or this
	// run would inherit --all/--json/-f and stop being a --ready run.
	resetListFlags()
	filterOut, err := runListCmdWithConfig(t, cfgPath, nibsDir, "--ready", "-q")
	if err != nil {
		t.Fatalf("list --ready failed: %v\nout: %s", err, filterOut)
	}
	inFilter := map[string]bool{}
	for _, id := range strings.Fields(filterOut) {
		inFilter[id] = true
	}

	for i, tc := range readyAgreementCases {
		name := tc.status
		if name == "" {
			name = "no-status"
		}
		t.Run(name, func(t *testing.T) {
			for _, probe := range []struct {
				id   string
				want bool
				why  string
			}{
				{unblockedID[i], tc.want, "unblocked"},
				// An active blocker withholds the nib whatever its status, so
				// the twins are never ready — including for todo, where the
				// status half alone would say yes.
				{blockedID[i], false, "blocked by an open nib"},
				// Same blocker, named by its short id. Both surfaces have to
				// resolve it, or the nib is withheld by one and handed out by
				// the other.
				{shortBlockedID[i], false, "blocked by an open nib named by short id"},
			} {
				got, listed := projReady[probe.id], inFilter[probe.id]
				if _, ok := projReady[probe.id]; !ok {
					t.Fatalf("%s (%s, %s) missing from the projection listing", probe.id, tc.status, probe.why)
				}
				if got != listed {
					t.Errorf("%s (%s, %s): projection ready=%v but --ready listed=%v — the two answers disagree",
						probe.id, tc.status, probe.why, got, listed)
				}
				if got != probe.want {
					t.Errorf("%s (%s, %s): projection ready=%v, want %v", probe.id, tc.status, probe.why, got, probe.want)
				}
				if listed != probe.want {
					t.Errorf("%s (%s, %s): --ready listed=%v, want %v", probe.id, tc.status, probe.why, listed, probe.want)
				}
			}
		})
	}
}

// TestListCommand_ReadyStatusFiltering pins how --ready composes with the
// status flags. --ready narrows the status filter to the startable statuses,
// and the two cases below are the two ways it has to do that: with no explicit
// -s it supplies the base itself (which is also what keeps a nib carrying an
// undeclared status out, since no exclusion can name one), and against an
// explicit -s it subtracts the non-startable statuses so the include list
// cannot widen it. The last row covers the degenerate vocabulary where neither
// way can work, because the branches part company there: the explicit -s branch
// yields nothing on its own, while the bare-flag branch would fail open.
func TestListCommand_ReadyStatusFiltering(t *testing.T) {
	// nostatus's front matter omits `status:` entirely, so it carries "" — a
	// status no group and no exclusion list names.
	fixture := map[string]string{
		"td--todo.md":      "---\nversion: 2\ntitle: Todo\nstatus: todo\ntype: task\n---\n",
		"dr--draft.md":     "---\nversion: 2\ntitle: Draft\nstatus: draft\ntype: task\n---\n",
		"ip--in-prog.md":   "---\nversion: 2\ntitle: InProgress\nstatus: in-progress\ntype: task\n---\n",
		"cm--completed.md": "---\nversion: 2\ntitle: Completed\nstatus: completed\ntype: task\n---\n",
		"ns--no-status.md": "---\nversion: 2\ntitle: NoStatus\ntype: task\n---\n",
	}

	tests := []struct {
		name string
		// setup runs before the command, for rows that need a different status
		// vocabulary than the declared one.
		setup func(*testing.T)
		args  []string
		want  []string
		// wantErr is a substring of the validation error the row must produce;
		// empty means the row must succeed.
		wantErr string
	}{
		{name: "bare --ready keeps only the startable status", args: []string{"--ready"}, want: []string{"td"}},
		{name: "--all does not widen --ready", args: []string{"--ready", "--all"}, want: []string{"td"}},
		{name: "--open does not widen --ready", args: []string{"--ready", "--open"}, want: []string{"td"}},
		{name: "an explicit -s loses its open non-startable members", args: []string{"--ready", "-s", "todo", "-s", "draft"}, want: []string{"td"}},
		{name: "-s with no startable member yields nothing", args: []string{"--ready", "-s", "draft"}, want: nil},
		// The sibling row above covers an open non-startable status. A closed
		// one takes a different route to the same place: --ready sets All, so
		// resolveStatusFilter adds no closed-status exclusion here and the
		// explicit include list is the only reason `cm` is in the base at all.
		// Only --ready's own subtraction removes it. (The previous row here ran
		// `--ready -s todo` and asserted [td], which the include list alone
		// already produces — `-s todo` without --ready returns the same.)
		{name: "an explicit -s naming a closed status does not let it in", args: []string{"--ready", "-s", "todo", "-s", "completed"}, want: []string{"td"}},
		// With nothing startable the flag cannot select anything, and an empty
		// include list would be a no-op filter rather than an empty result — so
		// the bare flag has to fail loudly instead of returning every unblocked
		// nib of any status.
		{
			name: "no status declaring startable is a validation error",
			setup: func(t *testing.T) {
				statuses := make([]config.StatusConfig, len(config.DefaultStatuses))
				copy(statuses, config.DefaultStatuses)
				for i := range statuses {
					if statuses[i].Role == config.RoleStartable {
						statuses[i].Role = config.RoleOpen
					}
				}
				withStatuses(t, statuses)
			},
			args:    []string{"--ready"},
			wantErr: "no status declares startable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup(t)
			}
			nibsDir := setupListCobraTest(t, fixture)
			out, err := runListCmd(t, nibsDir, append(append([]string{}, tt.args...), "-q")...)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("list %v succeeded and returned %v, want an error containing %q",
						tt.args, strings.Fields(out), tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("list %v error = %v, want it to contain %q", tt.args, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("list %v failed: %v\nout: %s", tt.args, err, out)
			}
			got := strings.Fields(out)
			if len(got) != len(tt.want) {
				t.Fatalf("list %v returned %v, want %v", tt.args, got, tt.want)
			}
			for i, id := range tt.want {
				if got[i] != id {
					t.Errorf("list %v returned %v, want %v", tt.args, got, tt.want)
					break
				}
			}
		})
	}
}

// TestListCommand_ReadyRequiresDeclaredStartability pins the durable half of
// deriving --ready from the flag: a status added to the vocabulary does not
// join the ready queue by default — it has to declare Startable, and until it
// does an unblocked nib carrying it stays out of both surfaces. The old
// exclusion literal failed exactly here: a status it did not name was never
// excluded, so a newly added one arrived as ready work.
func TestListCommand_ReadyRequiresDeclaredStartability(t *testing.T) {
	withExtraStatus(t, config.StatusConfig{
		Name:        "parked",
		Color:       "gray",
		Description: "Guard status: declared, and not startable",
	})
	if config.Default().IsStartableStatus("parked") {
		t.Fatal("test setup: the added status declares Startable, so it proves nothing")
	}

	fixture := map[string]string{
		"td--todo.md":   "---\nversion: 2\ntitle: Todo\nstatus: todo\ntype: task\n---\n",
		"pk--parked.md": "---\nversion: 2\ntitle: Parked\nstatus: parked\ntype: task\n---\n",
	}
	nibsDir := setupListCobraTest(t, fixture)

	// Each row asserts a positive control before the negative one, so a --ready
	// that returned nothing at all could not pass this test by vacuously
	// omitting pk. The bare flag must still hand back td; the `-s parked` row
	// has no startable member to return, so its control is that the result is
	// empty rather than merely pk-free.
	for _, tc := range []struct {
		args []string
		want []string // the exact result, so "returned nothing" fails here
	}{
		{[]string{"--ready", "-q"}, []string{"td"}},
		// Asking for it by name does not let it in — and since parked is the
		// only status named, nothing is left to return.
		{[]string{"--ready", "-s", "parked", "-q"}, nil},
	} {
		out, err := runListCmd(t, nibsDir, tc.args...)
		if err != nil {
			t.Fatalf("list %v failed: %v\nout: %s", tc.args, err, out)
		}
		got := strings.Fields(out)
		if slices.Contains(got, "pk") {
			t.Errorf("list %v returned %v — the added status never declared Startable", tc.args, got)
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("list %v returned %v, want %v", tc.args, got, tc.want)
		}
		resetListFlags()
	}

	// The projection has to withhold it too, or the two surfaces part company
	// the moment the vocabulary grows.
	out, err := runListCmd(t, nibsDir, "--all", "--json", "-f", "id,ready")
	if err != nil {
		t.Fatalf("list --all --json failed: %v\nout: %s", err, out)
	}
	var env struct {
		Nibs []struct {
			ID    string `json:"id"`
			Ready bool   `json:"ready"`
		} `json:"nibs"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v\nraw: %s", err, out)
	}
	seen := false
	for _, n := range env.Nibs {
		if n.ID != "pk" {
			continue
		}
		seen = true
		if n.Ready {
			t.Error("pk projects ready=true — the added status never declared Startable")
		}
	}
	if !seen {
		t.Fatalf("pk missing from the --all listing, so the assertion above ran on nothing\nraw: %s", out)
	}
}

// TestReadyFlagUsageStatesTheStatusesReadyActuallyReturns binds the --ready
// flag's help text to what the flag hands back. That string reaches agents:
// `nibs catalog examples` and `nibs catalog recipes` quote it verbatim
// (cmd/catalog.go flagUsage), and the catalog guards pin that propagation — but
// none of them looked at its content, so swapping StartableStatusNames() for
// OpenStatusNames() inside readyFlagUsage advertised in-progress/todo/draft
// while the flag still filtered on todo, with the whole suite green.
//
// The expectation is built from the statuses `nibs list --ready` actually
// returns over a fixture holding one unblocked nib per declared status, and
// deliberately NOT from a second config-derived list — that is the trap here. A
// sentence rendered from the wrong derived set self-updates into a confident lie
// while a guard tied to the same derived set stays green.
//
// readyFlagUsage runs in func init(), before any test body, so withStatuses
// cannot reach it: this guard works against the declared vocabulary only, and
// reads the string back through the same flagUsage accessor catalog uses rather
// than calling readyFlagUsage directly, so a flag that stopped using the helper
// still fails here.
//
// The trailing ")" is load-bearing — it closes the list, so a usage string
// naming todo/draft cannot satisfy an expectation built for todo alone.
func TestReadyFlagUsageStatesTheStatusesReadyActuallyReturns(t *testing.T) {
	declared := config.Default().StatusNames()

	fixture := map[string]string{}
	idOf := map[string]string{}
	for i, status := range declared {
		id := fmt.Sprintf("s%d", i)
		fixture[id+"--nib.md"] = fmt.Sprintf("---\nversion: 2\ntitle: S\nstatus: %s\ntype: task\n---\n", status)
		idOf[id] = status
	}
	nibsDir := setupListCobraTest(t, fixture)

	out, err := runListCmd(t, nibsDir, "--ready", "-q")
	if err != nil {
		t.Fatalf("list --ready failed: %v\nout: %s", err, out)
	}
	returned := map[string]bool{}
	for _, id := range strings.Fields(out) {
		status, ok := idOf[id]
		if !ok {
			t.Fatalf("--ready returned unknown id %q\nout: %s", id, out)
		}
		returned[status] = true
	}

	// Ordered by the declared vocabulary, which is the order readyFlagUsage
	// joins in and is independent of the flag being asserted.
	var actual []string
	for _, status := range declared {
		if returned[status] {
			actual = append(actual, status)
		}
	}
	if len(actual) == 0 {
		t.Fatal("--ready returned nothing over a fixture with one unblocked nib per status, so this guard compares nothing")
	}

	usage := flagUsage("list", "ready")
	if want := "startable status: " + strings.Join(actual, "/") + ")"; !strings.Contains(usage, want) {
		t.Errorf("--ready usage = %q does not state the statuses the flag returns; want it to contain %q", usage, want)
	}
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
