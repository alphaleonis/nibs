package cmd

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/store"
)

// renderedFullPrompt renders the full guide once for the assertions below.
func renderedFullPrompt(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	if err := renderFullPrompt(&b); err != nil {
		t.Fatalf("renderFullPrompt: %v", err)
	}
	return b.String()
}

// renderedSlimPrompt renders the slim prompt. It is a template too — the status
// groups its workflow rules name come from config — so the assertions below run
// against the rendered text an agent actually receives, not the raw source.
func renderedSlimPrompt(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	if err := renderSlimPrompt(&b); err != nil {
		t.Fatalf("renderSlimPrompt: %v", err)
	}
	return b.String()
}

// staleTokens are the pre-rename surface tokens that must never reappear in the
// full guide. Each is the exact grep the feature's acceptance uses.
var staleTokens = regexp.MustCompile(`body-replace|--columns|nibs (show|links|create|update) |close --summary "`)

// TestFullPromptHasNoStaleTokens guards against the guide regressing to the old
// verb surface (show/links/create/update, --columns, body-replace flags, inline
// close summaries).
func TestFullPromptHasNoStaleTokens(t *testing.T) {
	out := renderedFullPrompt(t)
	if loc := staleTokens.FindString(out); loc != "" {
		t.Fatalf("full guide contains stale token %q", loc)
	}
}

// TestFullPromptTeachesNewSurface asserts the guide covers every current verb,
// both JSON contracts, the '-'/@FILE input rule, and the exit-code model.
func TestFullPromptTeachesNewSurface(t *testing.T) {
	out := renderedFullPrompt(t)
	for _, want := range []string{
		"nibs get", "nibs list", "nibs rel", "nibs new", "nibs set",
		"nibs body", "nibs mv", "nibs rm", "nibs close", "nibs catalog", "nibs cheat",
		`{"nib"`, `"truncated"`, // the two read contracts
		"@FILE",          // the prose-input rule
		"| 4 | conflict", // the exit-code table
	} {
		if !strings.Contains(out, want) {
			t.Errorf("full guide missing %q", want)
		}
	}
}

// relSynopsisLine matches the guide's verb-surface entry for rel — the one
// written with the <relation> placeholder, as opposed to the worked examples
// that name a concrete relation.
var relSynopsisLine = regexp.MustCompile(`(?m)^nibs rel <id>.*<relation>.*$`)

// TestFullPromptRelSynopsisMatchesRelArity holds the full guide to the same
// rule as the cheat sheet: the verb surface brackets --rel exactly when
// omitting it is legal, and the guide says which relation an omitted --rel
// queries. The guide sits beside `nibs list [filters]` in the same code block,
// so an unbracketed --rel there reads as a requirement.
func TestFullPromptRelSynopsisMatchesRelArity(t *testing.T) {
	out := renderedFullPrompt(t)
	synopsis := relSynopsisLine.FindString(out)
	if synopsis == "" {
		t.Fatalf("full guide has no 'nibs rel <id> … <relation>' synopsis line:\n%s", out)
	}
	required := relRequiresRelFlag(t)
	bracketed := strings.Contains(synopsis, "[--rel")

	switch {
	case required && bracketed:
		t.Errorf("full guide brackets --rel as optional, but the rel command requires it: %q", synopsis)
	case !required && !bracketed:
		t.Errorf("full guide presents --rel as required, but omitting it queries %s; bracket it: %q", relDefaultKind, synopsis)
	case !required && !statesRelDefault(out):
		t.Errorf("full guide never names %q as what an omitted --rel returns", relDefaultKind)
	}
}

// TestFullPromptEnumsRender asserts the config-driven enum loops populated (the
// template rendered with real type/status/priority values, not an empty range).
func TestFullPromptEnumsRender(t *testing.T) {
	out := renderedFullPrompt(t)
	for _, want := range []string{"**milestone**", "**in-progress**", "**critical**"} {
		if !strings.Contains(out, want) {
			t.Errorf("full guide missing rendered enum %q", want)
		}
	}
}

// TestFullPromptIsMateriallyShorter pins a hard ceiling on the full guide: it
// must stay under 300 lines, so the projection/catalog model keeps carrying the
// reference weight instead of inlined prose.
func TestFullPromptIsMateriallyShorter(t *testing.T) {
	out := renderedFullPrompt(t)
	lines := strings.Count(out, "\n")
	if lines >= 300 {
		t.Fatalf("full guide is %d lines; expected < 300", lines)
	}
}

// TestSlimPromptTeachesTheClosingRitual asserts the slim prompt teaches
// scrapping as a close reason rather than as a body section an agent writes by
// hand. The hand-written `## Reasons for Scrapping` convention it used to
// prescribe is superseded: `close --as scrapped` is now the only route to that
// status, and it records the reason in the summary it already requires.
func TestSlimPromptTeachesTheClosingRitual(t *testing.T) {
	slim := renderedSlimPrompt(t)
	if strings.Contains(slim, "Reasons for Scrapping") {
		t.Error("slim prompt still prescribes the hand-written Reasons for Scrapping section")
	}
	for _, want := range []string{
		"nibs close <id> --as scrapped --summary -", // the ritual, spelled out
		"--as",                    // the flag itself, for the other reasons
		"refuses a closed status", // why `set -s scrapped` will fail
	} {
		if !strings.Contains(slim, want) {
			t.Errorf("slim prompt missing %q", want)
		}
	}
}

// TestFullPromptExplainsTheClosedStatusRefusal asserts the full guide states the
// rule an agent hits the moment it tries `set -s completed` — that `set` refuses
// a closed status — and names `close --as` as what to run instead. Without both
// halves the guide leaves the agent with a rejected command and no next step.
func TestFullPromptExplainsTheClosedStatusRefusal(t *testing.T) {
	out := renderedFullPrompt(t)

	// Scope the explanation to the Closing section. Checking the whole guide
	// passes on the one-line mention in the workflow rules near the top, which
	// states the rule without explaining it — the section is where an agent
	// looks after being refused.
	start := strings.Index(out, "\n## Closing\n")
	if start < 0 {
		t.Fatal("full guide has no '## Closing' section")
	}
	rest := out[start+len("\n## Closing\n"):]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		rest = rest[:end]
	}

	for _, want := range []string{
		"is refused", // the rule
		"--as",       // the replacement flag
		"-s todo",    // the no-reopen-verb boundary: open statuses still settable
	} {
		if !strings.Contains(rest, want) {
			t.Errorf("full guide Closing section missing %q, got:\n%s", want, rest)
		}
	}
	// The guide must not still offer `set` as the simple-task shortcut to a
	// closed status — that line is now a command the CLI rejects.
	for _, gone := range []string{"-s completed", "-s scrapped", "-s deferred"} {
		if strings.Contains(out, gone) {
			t.Errorf("full guide still tells agents to run `set %s`, which is refused", gone)
		}
	}
}

// TestSlimPromptPointsAtGrammar asserts the slim prompt routes agents to the
// grammar entry points (cheat + catalog) rather than inlining the reference.
func TestSlimPromptPointsAtGrammar(t *testing.T) {
	slim := renderedSlimPrompt(t)
	for _, want := range []string{"nibs cheat", "nibs catalog"} {
		if !strings.Contains(slim, want) {
			t.Errorf("slim prompt missing pointer %q", want)
		}
	}
}

// TestCodeList pins codeList's output against literals. It is the only
// assertion in the package that does: the prompt guards below build their
// expected strings by calling codeList, so a change to how it renders a name
// reproduces on both sides of their comparison and they stay green. Without
// this, dropping the code spans from every status named in both guides is
// invisible to the whole suite.
func TestCodeList(t *testing.T) {
	for _, tc := range []struct {
		name  string
		names []string
		sep   string
		want  string
	}{
		{"slash separator", []string{"deferred", "completed"}, "/", "`deferred`/`completed`"},
		{"or separator", []string{"deferred", "completed"}, " or ", "`deferred` or `completed`"},
		{"single name keeps its span", []string{"deferred"}, "/", "`deferred`"},
		{"empty set renders nothing", nil, "/", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := codeList(tc.names, tc.sep); got != tc.want {
				t.Errorf("codeList(%q, %q) = %q, want %q", tc.names, tc.sep, got, tc.want)
			}
		})
	}
}

// TestPromptsStateConfiguredStatusGroups pins the rendered shape of the status
// vocabulary in both prompts: the full guide's group lines must name exactly
// the members config declares, in config's order, and the slim prompt's blocker
// rule must name the holding set.
//
// The `--ready` sentence is deliberately NOT asserted here. Tying it to a
// config-derived set would only prove the prose matches some list of statuses,
// not the one the flag hands back — a sentence bound to the wrong derived set
// self-updates into a confident lie while its guard stays green. It is bound to
// the flag's observed output instead, in
// TestPromptsStateTheStatusesReadyActuallyReturns.
func TestPromptsStateConfiguredStatusGroups(t *testing.T) {
	cfg := config.Default()
	full := renderedFullPrompt(t)
	for _, want := range []string{
		"**open** = " + strings.Join(cfg.OpenStatusNames(), ", "),
		"**closed** = " + strings.Join(cfg.ClosedStatusNames(), ", "),
	} {
		if !strings.Contains(full, want) {
			t.Errorf("full guide missing status group line %q", want)
		}
	}
	slim := renderedSlimPrompt(t)
	if want := "a " + codeList(cfg.HoldingStatusNames(), "/") + " blocker still blocks"; !strings.Contains(slim, want) {
		t.Errorf("slim prompt missing blocker rule %q", want)
	}
}

// TestPromptsDeriveStatusVocabularyFromConfig proves the group membership and
// the blocker rule are read out of config rather than restated: a status added
// to DefaultStatuses must reach both rendered guides — in the right group, and
// in the blocker rule when it is closed without releasing its dependents — with
// no edit to either template.
func TestPromptsDeriveStatusVocabularyFromConfig(t *testing.T) {
	withExtraStatus(t, config.StatusConfig{
		Name:        "parked",
		Color:       "gray",
		Role:        config.RoleParked,
		Description: "Guard status: closed, and still blocking",
	})

	cfg := config.Default()
	closedList := strings.Join(cfg.ClosedStatusNames(), ", ")
	if !strings.Contains(closedList, "parked") {
		t.Fatalf("test setup: closed statuses %q should include the added status", closedList)
	}
	holding := codeList(cfg.HoldingStatusNames(), "/")

	full := renderedFullPrompt(t)
	if want := "**closed** = " + closedList; !strings.Contains(full, want) {
		t.Errorf("full guide closed group did not pick up the added status; want %q", want)
	}
	if want := "**A " + holding + " blocker still blocks"; !strings.Contains(full, want) {
		t.Errorf("full guide blocker rule did not pick up the added status; want %q", want)
	}

	slim := renderedSlimPrompt(t)
	if want := "a " + holding + " blocker still blocks"; !strings.Contains(slim, want) {
		t.Errorf("slim prompt blocker rule did not pick up the added status; want %q", want)
	}
}

// TestPromptsStateTheStatusesReadyActuallyReturns binds both guides' `--ready`
// sentence to what the flag does, not to a config-derived list. The two are not
// the same guarantee: a sentence rendered from some other derived set stays
// green against config while telling agents the flag returns statuses it
// withholds, which is how the claim went wrong before.
//
// So the expected sentence is built from the statuses `nibs list --ready`
// actually hands back over a fixture holding one unblocked nib per declared
// status. Both sentences end in a phrase that closes the list ("nothing else is
// startable"), which is load-bearing here: without it a guide naming
// `todo`/`draft` would still contain the substring for `todo` alone and pass.
func TestPromptsStateTheStatusesReadyActuallyReturns(t *testing.T) {
	cfg := config.Default()
	declared := cfg.StatusNames()

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

	// Ordered by the declared vocabulary, which is how both guides join the
	// set — and which is independent of the flag being asserted.
	var actual []string
	for _, status := range declared {
		if returned[status] {
			actual = append(actual, status)
		}
	}
	if len(actual) == 0 {
		t.Fatal("--ready returned nothing over a fixture with one unblocked nib per status, so this guard compares nothing")
	}

	if want := "whose status is " + codeList(actual, "/") + " — nothing else is startable."; !strings.Contains(renderedSlimPrompt(t), want) {
		t.Errorf("slim prompt does not state the statuses --ready returns; want %q", want)
	}
	if want := "status " + strings.Join(actual, "/") + " — nothing else is startable)"; !strings.Contains(renderedFullPrompt(t), want) {
		t.Errorf("full guide does not state the statuses --ready returns; want %q", want)
	}
}

// TestFullPromptHoldingBlockerRuleMatchesReady checks the one other `--ready`
// claim in the guides: that a nib whose only blocker is a holding one stays out
// of the flag's output. Also run against the flag rather than reasoned about,
// because the sentence names a consequence, not a set.
func TestFullPromptHoldingBlockerRuleMatchesReady(t *testing.T) {
	cfg := config.Default()
	holding := cfg.HoldingStatusNames()
	if len(holding) == 0 {
		t.Skip("no holding statuses declared, so the guides drop the rule entirely")
	}
	startable := cfg.StartableStatusNames()
	if len(startable) == 0 {
		t.Skip("nothing is startable, so no nib could be in --ready with or without the blocker")
	}

	// dep is startable and unblocked but for one blocker in a holding status;
	// free is the same nib without the blocker, so the difference between them
	// is the blocker and nothing else.
	fixture := map[string]string{
		"hld--holder.md": fmt.Sprintf("---\nversion: 2\ntitle: Holder\nstatus: %s\ntype: task\n---\n", holding[0]),
		"dep--blocked.md": fmt.Sprintf("---\nversion: 2\ntitle: Dep\nstatus: %s\ntype: task\nblocked_by: [hld]\n---\n",
			startable[0]),
		"free--free.md": fmt.Sprintf("---\nversion: 2\ntitle: Free\nstatus: %s\ntype: task\n---\n", startable[0]),
	}
	nibsDir := setupListCobraTest(t, fixture)
	out, err := runListCmd(t, nibsDir, "--ready", "-q")
	if err != nil {
		t.Fatalf("list --ready failed: %v\nout: %s", err, out)
	}
	listed := map[string]bool{}
	for _, id := range strings.Fields(out) {
		listed[id] = true
	}
	if !listed["free"] {
		t.Fatalf("--ready omitted free, so the comparison below shows nothing about the blocker\nout: %s", out)
	}
	if listed["dep"] {
		t.Errorf("--ready returned dep, whose only blocker is %s — the guides claim it stays out\nout: %s", holding[0], out)
	}

	if want := "stays out of `nibs list --ready`"; !strings.Contains(renderedFullPrompt(t), want) {
		t.Errorf("full guide no longer states %q, so the behavior above is unclaimed", want)
	}
}

// emptyInterpolation matches one shape an empty derived set leaves behind: an
// article or "as" with nothing left to introduce — "a  blocker still blocks",
// "closing it as  does not". That is its whole coverage, and it is narrower
// than the set of places a derived set reaches.
//
// It does NOT see a set whose sentence continues after it. The group line
// `- **open** = {{join .OpenStatuses ", "}} — where the work sits` renders
// `- **open** =  — where the work sits` with the set empty, and this pattern
// does not match that. Four sites in prompt-full.tmpl are that shape and none
// sits inside an {{if}}: the `nibs list` line in the finding-work fence, the
// two **open**/**closed** group lines, and the open-by-default paragraph. All
// four name .OpenStatuses or .ClosedStatuses, and a config that empties either
// group is already rejected by TestStatusGroupNames (a group of fewer than two
// members is a second spelling of a concrete status), whereas the holding and
// releasing sets are emptied by flipping ReleasesDependents — a plausible
// product decision rather than a broken vocabulary. That is why the guarded
// sites are the ones that are guarded.
//
// So this is a backstop for the shape above, not a substitute for guarding: a
// new sentence naming a derived set still needs its own {{if}} on THAT set and
// its own case in TestPromptsGuardEmptyDerivedSets.
var emptyInterpolation = regexp.MustCompile(`\b(?i:an?|the|as) {2}`)

// renderedPrompts renders both guides, for assertions that must hold of each.
func renderedPrompts(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{
		"full guide":  renderedFullPrompt(t),
		"slim prompt": renderedSlimPrompt(t),
	}
}

// excerpt quotes a match with enough of its sentence around it to show what
// went missing.
func excerpt(s string, loc []int) string {
	start, end := loc[0]-50, loc[1]+50
	if start < 0 {
		start = 0
	}
	if end > len(s) {
		end = len(s)
	}
	return s[start:end]
}

// TestPromptsGuardEmptyDerivedSets asserts neither guide ever interpolates an
// empty derived set into prose. Every sentence naming a set must sit inside a
// guard on THAT set: a sentence contrasting two sets needs both, which a guard
// on one of them does not give it. So each way a set can empty is exercised —
// nothing holding, where the blocker rule loses its subject; nothing releasing,
// where the contrast it draws loses its counterpart; nothing startable, where
// the --ready rule has no statuses left to name — and the affected sentences
// must be dropped whole rather than rendered with a hole in them.
func TestPromptsGuardEmptyDerivedSets(t *testing.T) {
	// With the statuses as declared no set is empty, so a match here means the
	// pattern is too broad rather than that a guard is missing.
	for name, out := range renderedPrompts(t) {
		if loc := emptyInterpolation.FindStringIndex(out); loc != nil {
			t.Fatalf("pattern matches the %s as declared, so it cannot guard anything: %q", name, excerpt(out, loc))
		}
	}

	for _, tc := range []struct {
		name    string
		empty   func(*config.StatusConfig) // applied to every declared status
		emptied func(*config.Config) []string
		// The rule that has no subject left once the set is empty, and so must
		// be dropped whole rather than rendered with a hole in it.
		dropped string
	}{
		{
			"nothing holds",
			func(s *config.StatusConfig) {
				if s.Role == config.RoleParked {
					s.Role = config.RoleDropped
				}
			},
			(*config.Config).HoldingStatusNames,
			"blocker still blocks",
		},
		{
			"nothing releases",
			func(s *config.StatusConfig) {
				if s.Role.Closed() {
					s.Role = config.RoleParked
				}
			},
			(*config.Config).ReleasingStatusNames,
			"blocker still blocks",
		},
		{
			"nothing is startable",
			func(s *config.StatusConfig) {
				if s.Role == config.RoleStartable {
					s.Role = config.RoleOpen
				}
			},
			(*config.Config).StartableStatusNames,
			"nothing else is startable",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			statuses := make([]config.StatusConfig, len(config.DefaultStatuses))
			copy(statuses, config.DefaultStatuses)
			for i := range statuses {
				tc.empty(&statuses[i])
			}
			withStatuses(t, statuses)

			if got := tc.emptied(config.Default()); len(got) != 0 {
				t.Fatalf("test setup: emptied set = %v, want it empty", got)
			}

			for name, out := range renderedPrompts(t) {
				// The closed-but-blocks rule contrasts holding against
				// releasing, so it teaches nothing when either side is empty;
				// the --ready rule names the startable set outright.
				if strings.Contains(out, tc.dropped) {
					t.Errorf("%s still states %q with the set it names empty", name, tc.dropped)
				}
				if loc := emptyInterpolation.FindStringIndex(out); loc != nil {
					t.Errorf("%s rendered an empty status set into prose: %q", name, excerpt(out, loc))
				}
			}
		})
	}
}

// primeDiscoveryFixture is the shape the discovery bug lived in: a pre-layout
// project (a `.nibs.yml` naming its data directory through the retired
// `nibs.path` key) nested under an unrelated ancestor store. It returns the
// nested project's directory.
func primeDiscoveryFixture(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("NIBS_CONFIG_ROOT", tmp)
	t.Setenv("NIBS_PATH", "")
	parent := filepath.Join(tmp, "parent")
	ancestor := filepath.Join(parent, store.DirName)
	mkdirAllT(t, filepath.Join(ancestor, store.DataDirName))
	writeFileT(t, filepath.Join(ancestor, store.ConfigFileName), "nibs:\n  prefix: par-\n  id_length: 4\n")
	sub := filepath.Join(parent, "sub")
	data := filepath.Join(sub, "nibdata")
	mkdirAllT(t, data)
	writeFileT(t, filepath.Join(data, "sub-b2--two.md"), layoutNib)
	writeFileT(t, filepath.Join(sub, store.LegacyProjectConfigFileName),
		"nibs:\n  prefix: sub-\n  id_length: 4\n  path: nibdata\n")
	return sub
}

// TestPrimeRefusesInANestedPreLayoutProject pins which question `nibs prime`
// asks on the way to emitting the onboarding prompt. Searching only for a
// `.nibs` directory walks straight past the nearer pre-layout project and finds
// the ancestor store, so the prompt was emitted — telling an agent to track all
// work here with a CLI whose every other command refuses in this directory.
// A pre-layout project is a nibs project, so the answer is the same refusal
// every other command gives, naming the migration that makes it usable.
func TestPrimeRefusesInANestedPreLayoutProject(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	resetRootPersistentFlags()

	sub := primeDiscoveryFixture(t)
	t.Chdir(sub)

	out, err := runRootWith(t, "prime")
	if err == nil {
		t.Fatalf("prime emitted a prompt in a pre-layout project instead of refusing; output:\n%s", out)
	}
	if strings.Contains(out, "Nibs — agentic-first issue tracker") {
		t.Errorf("prime refused but still wrote the prompt to stdout:\n%s", out)
	}
	msg := err.Error()
	for _, want := range []string{sub, "pre-layout", "nibs migrate"} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal = %q, want it to name %q", msg, want)
		}
	}
	// The ancestor store is named as context, not bound to: the reader may have
	// watched prime answer from it until now.
	if ancestor := filepath.Join(filepath.Dir(sub), store.DirName); !strings.Contains(msg, ancestor) {
		t.Errorf("refusal = %q, want it to name the shadowed ancestor store %q", msg, ancestor)
	}
}

// TestPrimeStaysSilentWithNoNibsProject is the other half of the gate, and the
// reason the refusal above cannot simply be "refuse whenever no store is
// bound": `nibs prime` is run unconditionally from an agent's startup, so a
// repository that does not use nibs must get silence and a zero exit.
func TestPrimeStaysSilentWithNoNibsProject(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	resetRootPersistentFlags()

	tmp := t.TempDir()
	t.Setenv("NIBS_CONFIG_ROOT", tmp)
	t.Setenv("NIBS_PATH", "")
	plain := filepath.Join(tmp, "plain", "src")
	mkdirAllT(t, plain)
	t.Chdir(plain)

	out, err := runRootWith(t, "prime")
	if err != nil {
		t.Fatalf("prime in a project with no nibs marker = %v, want a silent success", err)
	}
	if out != "" {
		t.Errorf("prime wrote %q where there is no nibs project, want nothing", out)
	}
}

// TestPrimeEmitsInACurrentLayoutProject is the happy path the gate must keep:
// a store found by the upward walk still gets the prompt.
func TestPrimeEmitsInACurrentLayoutProject(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	resetRootPersistentFlags()

	tmp := t.TempDir()
	t.Setenv("NIBS_CONFIG_ROOT", tmp)
	t.Setenv("NIBS_PATH", "")
	projectDir := filepath.Join(tmp, "proj")
	writeStore(t, projectDir, "nibs:\n  prefix: proj-\n  id_length: 4\n", map[string]string{
		"proj-a1b2--one.md": layoutNib,
	})
	nested := filepath.Join(projectDir, "src", "deep")
	mkdirAllT(t, nested)
	t.Chdir(nested)

	out, err := runRootWith(t, "prime")
	if err != nil {
		t.Fatalf("prime in a current-layout project = %v, want the prompt", err)
	}
	if !strings.Contains(out, "Nibs — agentic-first issue tracker") {
		t.Errorf("prime emitted no prompt in a current-layout project; output:\n%s", out)
	}
}
