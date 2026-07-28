package cmd

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
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

// TestSlimPromptNamesScrappingSectionVerb asserts the scrapping-section hint
// names the verb an agent actually runs (--create upsert, or append) rather than
// the old verb-less "Use `## Reasons for Scrapping`" phrasing, which left agents
// to guess how to write the section.
func TestSlimPromptNamesScrappingSectionVerb(t *testing.T) {
	slim := renderedSlimPrompt(t)
	if strings.Contains(slim, "Use `## Reasons for Scrapping` when scrapping") {
		t.Error("slim prompt still uses the verb-less scrapping hint")
	}
	// Scope the verb check to the scrapping hint. Checking the whole prompt would
	// pass spuriously: "append" matches the unrelated earlier "appends a summary".
	idx := strings.Index(slim, "Reasons for Scrapping")
	if idx < 0 {
		t.Fatal("slim prompt should still reference the Reasons for Scrapping section")
	}
	hint := slim[idx:]
	if nl := strings.IndexByte(hint, '\n'); nl >= 0 {
		hint = hint[:nl] // bound to the scrapping line so a later prompt edit can't re-decay this guard
	}
	if !strings.Contains(hint, "--create") && !strings.Contains(hint, "--append") {
		t.Error("slim prompt scrapping hint should name a verb (--create or --append)")
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
		Closed:      true,
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
		fixture[id+"--nib.md"] = fmt.Sprintf("---\ntitle: S\nstatus: %s\ntype: task\n---\n", status)
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
		"hld--holder.md": fmt.Sprintf("---\ntitle: Holder\nstatus: %s\ntype: task\n---\n", holding[0]),
		"dep--blocked.md": fmt.Sprintf("---\ntitle: Dep\nstatus: %s\ntype: task\nblocked_by: [hld]\n---\n",
			startable[0]),
		"free--free.md": fmt.Sprintf("---\ntitle: Free\nstatus: %s\ntype: task\n---\n", startable[0]),
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
				if s.Closed {
					s.ReleasesDependents = true
				}
			},
			(*config.Config).HoldingStatusNames,
			"blocker still blocks",
		},
		{
			"nothing releases",
			func(s *config.StatusConfig) {
				if s.Closed {
					s.ReleasesDependents = false
				}
			},
			(*config.Config).ReleasingStatusNames,
			"blocker still blocks",
		},
		{
			"nothing is startable",
			func(s *config.StatusConfig) { s.Startable = false },
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
