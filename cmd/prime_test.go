package cmd

import (
	"regexp"
	"strings"
	"testing"
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
	if strings.Contains(agentPromptSlim, "Use `## Reasons for Scrapping` when scrapping") {
		t.Error("slim prompt still uses the verb-less scrapping hint")
	}
	// Scope the verb check to the scrapping hint. Checking the whole prompt would
	// pass spuriously: "append" matches the unrelated earlier "appends a summary".
	idx := strings.Index(agentPromptSlim, "Reasons for Scrapping")
	if idx < 0 {
		t.Fatal("slim prompt should still reference the Reasons for Scrapping section")
	}
	hint := agentPromptSlim[idx:]
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
	for _, want := range []string{"nibs cheat", "nibs catalog"} {
		if !strings.Contains(agentPromptSlim, want) {
			t.Errorf("slim prompt missing pointer %q", want)
		}
	}
}
