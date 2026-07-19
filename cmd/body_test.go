package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/output"
	"github.com/spf13/pflag"
)

// resetBodyFlags zeroes every global flag variable for the body command and
// clears Cobra's per-flag "Changed" tracking. The body tests share the rootCmd
// singleton, so they must NOT use t.Parallel().
func resetBodyFlags() {
	bodySet = ""
	bodyAppend = ""
	bodySection = ""
	bodyReplaceOld = ""
	bodyReplaceNew = ""
	bodyIfMatch = ""
	bodyJSON = false
	bodyCreate = false
	bodyCmd.Flags().Visit(func(f *pflag.Flag) {
		f.Changed = false
	})
	resetRootPersistentFlags()
}

func TestResetBodyFlagsClearsAllState(t *testing.T) {
	bodySet = "dirty"
	bodyAppend = "dirty"
	bodySection = "dirty"
	bodyReplaceOld = "dirty"
	bodyReplaceNew = "dirty"
	bodyIfMatch = "dirty"
	bodyJSON = true
	bodyCreate = true

	resetBodyFlags()

	if bodySet != "" || bodyAppend != "" || bodySection != "" ||
		bodyReplaceOld != "" || bodyReplaceNew != "" || bodyIfMatch != "" || bodyJSON || bodyCreate {
		t.Errorf("resetBodyFlags left non-zero state: set=%q append=%q section=%q old=%q new=%q ifMatch=%q json=%v create=%v",
			bodySet, bodyAppend, bodySection, bodyReplaceOld, bodyReplaceNew, bodyIfMatch, bodyJSON, bodyCreate)
	}
}

// bodyNibFile is the on-disk filename writeSetNib produces for a nib id.
func bodyNibFile(id string) string { return id + "--test.md" }

// TestBodyHelpDocumentsSectionCreate asserts the body command's Long help teaches
// the --section --set precondition (an existing heading) and the --create escape
// hatch, so the strict default is discoverable from --help alone.
func TestBodyHelpDocumentsSectionCreate(t *testing.T) {
	long := bodyCmd.Long
	if !strings.Contains(long, "--create") {
		t.Errorf("body Long help should mention --create, got:\n%s", long)
	}
	// Assert on a phrase unique to the new precondition sentence. A bare
	// "existing" check would pass on the pre-existing "Edits an existing nib's
	// body." even if the precondition sentence were deleted.
	if !strings.Contains(long, "targets an existing heading and errors if it is absent") {
		t.Errorf("body Long help should state the existing-heading precondition, got:\n%s", long)
	}
}

// TestSectionHeadingLevel pins the heading-level derivation boundaries: the
// spelled marker count wins, a bare heading defaults to level 2, and leading
// whitespace is trimmed before the markers are counted.
func TestSectionHeadingLevel(t *testing.T) {
	for _, tc := range []struct {
		flag string
		want int
	}{
		{"## H", 2},
		{"### H", 3},
		{"#### H", 4},
		{"H", 2},     // bare heading defaults to the "##" convention
		{" ## H", 2}, // leading whitespace trimmed first
		{"   ### H", 3},
	} {
		if got := sectionHeadingLevel(tc.flag); got != tc.want {
			t.Errorf("sectionHeadingLevel(%q) = %d, want %d", tc.flag, got, tc.want)
		}
	}
}

// TestSectionMatchLevel pins the MATCH-level derivation, which differs from the
// append level only for a bare heading: a spelled marker count wins, but a bare
// heading yields 0 (wildcard — match any level), NOT the append default of 2.
func TestSectionMatchLevel(t *testing.T) {
	for _, tc := range []struct {
		flag string
		want int
	}{
		{"## H", 2},
		{"### H", 3},
		{"#### H", 4},
		{"H", 0},     // bare heading is a wildcard (match any level)
		{" ## H", 2}, // leading whitespace trimmed first
		{"   ### H", 3},
	} {
		if got := sectionMatchLevel(tc.flag); got != tc.want {
			t.Errorf("sectionMatchLevel(%q) = %d, want %d", tc.flag, got, tc.want)
		}
	}
}

// TestBodyAppendFromStdin verifies `body <id> --append -` appends a block read
// from stdin, that special characters survive verbatim, and that the --json
// echo is the lean card (no body leak).
func TestBodyAppendFromStdin(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	nibsDir, id := writeSetNib(t, "bdy-1", "existing body\n")
	withStdin(t, "## Notes\n`code` with \"quotes\" and 'apostrophes'\n")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--append", "-", "--json"})
	var execErr error
	out := captureStdout(t, func() { execErr = rootCmd.Execute() })
	if execErr != nil {
		t.Fatalf("body --append - failed: %v", execErr)
	}

	content := readNibFile(t, nibsDir, bodyNibFile(id))
	if !strings.Contains(content, "existing body") {
		t.Errorf("original body lost, got:\n%s", content)
	}
	if !strings.Contains(content, "## Notes") {
		t.Errorf("appended section missing, got:\n%s", content)
	}
	if !strings.Contains(content, "`code` with \"quotes\" and 'apostrophes'") {
		t.Errorf("special characters not preserved verbatim, got:\n%s", content)
	}

	// The echo is the lean card: valid {nib} JSON with no body field.
	var envelope map[string]any
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("echo is not valid JSON: %v\n%s", err, out)
	}
	nibObj, ok := envelope["nib"].(map[string]any)
	if !ok {
		t.Fatalf("expected {nib:{...}} echo, got: %s", out)
	}
	if _, present := nibObj["body"]; present {
		t.Errorf("lean card should not include body, got: %s", out)
	}
}

// TestBodySetReplacesWholeBody verifies `body <id> --set -` replaces the whole
// body with the piped content.
func TestBodySetReplacesWholeBody(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	nibsDir, id := writeSetNib(t, "bdy-2", "old body here\n")
	withStdin(t, "brand new body\nwith two lines\n")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--set", "-"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("body --set - failed: %v", err)
	}

	content := readNibFile(t, nibsDir, bodyNibFile(id))
	if !strings.Contains(content, "brand new body") || !strings.Contains(content, "with two lines") {
		t.Errorf("body not replaced, got:\n%s", content)
	}
	if strings.Contains(content, "old body here") {
		t.Errorf("old body should be gone, got:\n%s", content)
	}
}

// TestBodySectionReplacesSectionContent verifies `--section "## H" --set @FILE`
// replaces only that heading's content, leaving other sections intact.
func TestBodySectionReplacesSectionContent(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	body := "## Intro\nkeep me\n\n## Notes\nold note\n\n## Tail\nkeep me too\n"
	nibsDir, id := writeSetNib(t, "bdy-3", body)
	secFile := filepath.Join(t.TempDir(), "sec.md")
	if err := os.WriteFile(secFile, []byte("fresh note content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--section", "## Notes", "--set", "@" + secFile})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("body --section --set @file failed: %v", err)
	}

	content := readNibFile(t, nibsDir, bodyNibFile(id))
	if !strings.Contains(content, "fresh note content") {
		t.Errorf("section content not replaced, got:\n%s", content)
	}
	if strings.Contains(content, "old note") {
		t.Errorf("old section content should be gone, got:\n%s", content)
	}
	// Neighbouring sections and their headings must survive.
	if !strings.Contains(content, "keep me") || !strings.Contains(content, "keep me too") {
		t.Errorf("neighbouring sections were disturbed, got:\n%s", content)
	}
	if !strings.Contains(content, "## Notes") {
		t.Errorf("target heading should remain, got:\n%s", content)
	}
}

// TestBodySectionExactBeatsEarlierParenthetical verifies the P2 fix through the
// CLI path: a bare `--section "Key Decisions" --set -` against a body where a
// parenthetical `## Key Decisions (Phase 1)` precedes the exact `## Key
// Decisions` replaces the EXACT section. Under the pre-fix single-pass matcher
// the earlier parenthetical heading would win, so the load-bearing checks are
// that the exact section got the new content while the `(Phase 1)` content
// survives untouched.
func TestBodySectionExactBeatsEarlierParenthetical(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	body := "## Key Decisions (Phase 1)\n- old phase-one\n\n## Key Decisions\n- keep\n"
	nibsDir, id := writeSetNib(t, "bdy-exact-1", body)
	withStdin(t, "new exact content\n")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--section", "Key Decisions", "--set", "-"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("body --section \"Key Decisions\" --set - failed: %v", err)
	}

	content := readNibFile(t, nibsDir, bodyNibFile(id))
	// The exact "## Key Decisions" section received the new content.
	if !strings.Contains(content, "new exact content") {
		t.Errorf("exact section not replaced, got:\n%s", content)
	}
	if strings.Contains(content, "- keep") {
		t.Errorf("exact section content should be gone, got:\n%s", content)
	}
	// The earlier parenthetical "(Phase 1)" section must be untouched.
	if !strings.Contains(content, "## Key Decisions (Phase 1)") {
		t.Errorf("parenthetical heading should survive, got:\n%s", content)
	}
	if !strings.Contains(content, "- old phase-one") {
		t.Errorf("parenthetical section content must be untouched, got:\n%s", content)
	}
}

// TestBodySectionCreateAddsMissingSection verifies `--section "## New" --set
// --create -` creates the absent heading (upsert) instead of erroring, appending
// it exactly once with the piped content.
func TestBodySectionCreateAddsMissingSection(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	nibsDir, id := writeSetNib(t, "bdy-create-1", "## Intro\nkeep me\n")
	withStdin(t, "brand new content\n")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--section", "## New", "--set", "-", "--create"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("body --section --set - --create failed: %v", err)
	}

	content := readNibFile(t, nibsDir, bodyNibFile(id))
	if got := strings.Count(content, "## New"); got != 1 {
		t.Errorf("expected the new heading exactly once, got %d, body:\n%s", got, content)
	}
	if !strings.Contains(content, "brand new content") {
		t.Errorf("new section content missing, got:\n%s", content)
	}
	if !strings.Contains(content, "## Intro") || !strings.Contains(content, "keep me") {
		t.Errorf("existing section disturbed, got:\n%s", content)
	}
}

// TestBodySectionCreateReplacesExistingSection verifies `--section --set
// --create` on a body that ALREADY has the heading replaces its content in place
// without duplicating the heading (upsert stays idempotent on the heading).
func TestBodySectionCreateReplacesExistingSection(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	body := "## Description\nSomething.\n\n## Reasons for Scrapping\nold reason\n"
	nibsDir, id := writeSetNib(t, "bdy-create-2", body)
	withStdin(t, "new reason: out of scope\n")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--section", "## Reasons for Scrapping", "--set", "-", "--create"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("body --section --set - --create on existing section failed: %v", err)
	}

	content := readNibFile(t, nibsDir, bodyNibFile(id))
	if got := strings.Count(content, "## Reasons for Scrapping"); got != 1 {
		t.Errorf("heading should appear exactly once, got %d, body:\n%s", got, content)
	}
	if !strings.Contains(content, "new reason: out of scope") {
		t.Errorf("new content missing, got:\n%s", content)
	}
	if strings.Contains(content, "old reason") {
		t.Errorf("old content should be gone, got:\n%s", content)
	}
	// Content BEFORE the target heading must survive the upsert-replace; a bug
	// truncating the preceding section would otherwise pass unnoticed.
	if !strings.Contains(content, "## Description") || !strings.Contains(content, "Something.") {
		t.Errorf("preceding section should survive the upsert-replace, got:\n%s", content)
	}
}

// TestBodySectionCreateSpelledLevelDoesNotClobberLowerLevel verifies the
// level-aware match: `--section "### Sub" --set - --create` against a body whose
// only "Sub" is a LEVEL-2 "## Sub" (with a nested "### Child") must NOT match and
// clobber that level-2 section. The spelled level-3 request finds no level-3 "Sub",
// so --create appends a fresh "### Sub" and leaves the level-2 section intact.
func TestBodySectionCreateSpelledLevelDoesNotClobberLowerLevel(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	body := "## Sub\nlevel-two text\n\n### Child\nnested child\n"
	nibsDir, id := writeSetNib(t, "bdy-level-1", body)
	withStdin(t, "fresh level-three content\n")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--section", "### Sub", "--set", "-", "--create"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("body --section \"### Sub\" --set - --create failed: %v", err)
	}

	content := readNibFile(t, nibsDir, bodyNibFile(id))
	// The level-2 "## Sub" section AND its nested "### Child" must survive: a
	// spelled "### Sub" must not match the level-2 heading and swallow the section.
	if !strings.Contains(content, "level-two text") {
		t.Errorf("level-2 ## Sub content was clobbered, got:\n%s", content)
	}
	if !strings.Contains(content, "nested child") {
		t.Errorf("nested ### Child content was clobbered, got:\n%s", content)
	}
	// The new "### Sub" heading is appended with the piped content.
	if !strings.Contains(content, "fresh level-three content") {
		t.Errorf("new ### Sub content missing, got:\n%s", content)
	}
	// Both headings are present as exact heading lines: the original level-2 and
	// the newly appended level-3 (substring counting would confuse "## Sub" with
	// the "## Sub" inside "### Sub").
	var hasL2, hasL3, childCount int
	for _, line := range strings.Split(content, "\n") {
		switch strings.TrimSpace(line) {
		case "## Sub":
			hasL2++
		case "### Sub":
			hasL3++
		case "### Child":
			childCount++
		}
	}
	if hasL2 != 1 {
		t.Errorf("expected the original level-2 '## Sub' heading exactly once, got %d, body:\n%s", hasL2, content)
	}
	if hasL3 != 1 {
		t.Errorf("expected a new level-3 '### Sub' heading exactly once, got %d, body:\n%s", hasL3, content)
	}
	if childCount != 1 {
		t.Errorf("expected the nested '### Child' heading to survive, got %d, body:\n%s", childCount, content)
	}
}

// TestBodySectionCreateBareMatchesAnyLevel verifies the wildcard is preserved
// through the CLI path: the only heading is a level-3 "### Sub", yet a bare
// `--section "Sub" --set - --create` matches and replaces it IN PLACE. The
// level-3 seed is load-bearing — the bare append default is level 2, so a
// regression that hard-coded the bare match level to 2 (instead of AnyLevel)
// would miss "### Sub", append a duplicate "## Sub", and leave "old three"
// behind. The load-bearing discriminators are the "old three" survival check
// and the zero-"## Sub" check — both fail under that counterfactual. The
// single-"### Sub" count corroborates in-place replacement but, on its own,
// stays 1 under the regression (the untouched level-3 seed), so it is not the
// leg that bites.
func TestBodySectionCreateBareMatchesAnyLevel(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	nibsDir, id := writeSetNib(t, "bdy-bare-l3", "### Sub\nold three\n")
	withStdin(t, "replaced content\n")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--section", "Sub", "--set", "-", "--create"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("body --section \"Sub\" --set - --create failed: %v", err)
	}

	content := readNibFile(t, nibsDir, bodyNibFile(id))
	if !strings.Contains(content, "replaced content") {
		t.Errorf("bare section did not replace existing content, got:\n%s", content)
	}
	if strings.Contains(content, "old three") {
		t.Errorf("old content should be replaced, got:\n%s", content)
	}
	// Exactly one "### Sub" heading and no "## Sub": a wildcard match replaces the
	// level-3 heading in place. A bare→fixed-level-2 regression would instead
	// append a new "## Sub" and leave "### Sub" untouched.
	var l3Count, l2Count int
	for _, line := range strings.Split(content, "\n") {
		switch strings.TrimSpace(line) {
		case "### Sub":
			l3Count++
		case "## Sub":
			l2Count++
		}
	}
	if l3Count != 1 {
		t.Errorf("bare wildcard should replace the level-3 heading in place (one '### Sub'), got %d, body:\n%s", l3Count, content)
	}
	if l2Count != 0 {
		t.Errorf("bare wildcard must not append a duplicate '## Sub', got %d, body:\n%s", l2Count, content)
	}
}

// TestBodySectionStrictSpelledLevelMismatchNotFound verifies the SAFE outcome of a
// level mismatch in the strict (no --create) path: `--section "### Sub" --set -`
// against a body whose only "Sub" is a level-2 "## Sub" yields a section-not-found
// validation error, NOT a silent clobber of the level-2 section. Because that
// same-name heading is an EXACT one at another level, the error also TEACHES the
// level mismatch rather than blindly recommending --create (which would append a
// heading a wildcard reader could shadow).
func TestBodySectionStrictSpelledLevelMismatchNotFound(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	nibsDir, id := writeSetNib(t, "bdy-level-3", "## Sub\nkeep me\n")
	withStdin(t, "whatever\n")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--section", "### Sub", "--set", "-", "--json"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected a level-mismatched strict --section --set to error, got nil")
	}
	var ce *output.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("error = %T, want *output.CodedError", err)
	}
	if output.ExitCode(ce.Code) != output.ExitValidation {
		t.Errorf("level-mismatch exit = %d, want %d (validation)", output.ExitCode(ce.Code), output.ExitValidation)
	}
	// The message must teach the level mismatch (an EXACT same-name heading exists
	// at another level), not merely recommend --create.
	if !strings.Contains(ce.Msg, "another level") {
		t.Errorf("strict level-mismatch message should reference the same-name heading at another level, got: %q", ce.Msg)
	}
	// The level-2 section must be untouched (no clobber).
	content := readNibFile(t, nibsDir, bodyNibFile(id))
	if !strings.Contains(content, "keep me") {
		t.Errorf("level-2 ## Sub content must survive the rejected request, got:\n%s", content)
	}
}

// TestBodySectionCreateLeadingWhitespaceStaysClean verifies that a --section
// value with leading whitespace (" ## H") is trimmed BEFORE the "#" markers are
// stripped, so it derives heading "H" at level 2 and creates a clean "## H"
// section — not a garbled "## ## H" heading with the markers retained.
func TestBodySectionCreateLeadingWhitespaceStaysClean(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	nibsDir, id := writeSetNib(t, "bdy-create-ws", "## Intro\nkeep me\n")
	withStdin(t, "brand new content\n")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--section", " ## H", "--set", "-", "--create"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("body --section \" ## H\" --set - --create failed: %v", err)
	}

	content := readNibFile(t, nibsDir, bodyNibFile(id))
	if strings.Contains(content, "## ## H") {
		t.Errorf("leading whitespace produced a garbled heading, got:\n%s", content)
	}
	if got := strings.Count(content, "## H"); got != 1 {
		t.Errorf("expected a clean \"## H\" heading exactly once, got %d, body:\n%s", got, content)
	}
	if !strings.Contains(content, "brand new content") {
		t.Errorf("new section content missing, got:\n%s", content)
	}
}

// TestBodySectionNotFound verifies replacing a heading that does not exist is a
// validation error (not a silent no-op).
func TestBodySectionNotFound(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	nibsDir, id := writeSetNib(t, "bdy-4", "## Intro\nhi\n")
	withStdin(t, "whatever\n")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--section", "## Absent", "--set", "-", "--json"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for a missing section, got nil")
	}
	var ce *output.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("error = %T, want *output.CodedError", err)
	}
	if output.ExitCode(ce.Code) != output.ExitValidation {
		t.Errorf("missing section exit = %d, want %d (validation)", output.ExitCode(ce.Code), output.ExitValidation)
	}
	// The message must teach both fixes: --append (its block carries the heading)
	// and --create (upsert in place).
	if !strings.Contains(ce.Msg, "--append") {
		t.Errorf("missing-section message should name --append, got: %q", ce.Msg)
	}
	if !strings.Contains(ce.Msg, "--create") {
		t.Errorf("missing-section message should name --create, got: %q", ce.Msg)
	}
}

// TestBodyCreateRequiresSectionSet verifies --create is rejected (validation,
// exit 2) unless paired with --section --set. --create is an upsert modifier for
// that one operation; on plain --set (no --section) it is meaningless.
func TestBodyCreateRequiresSectionSet(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	nibsDir, id := writeSetNib(t, "bdy-create-3", "## Intro\nhi\n")
	withStdin(t, "whatever\n")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--create", "--set", "-", "--json"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected --create without --section to error, got nil")
	}
	var ce *output.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("error = %T, want *output.CodedError", err)
	}
	if output.ExitCode(ce.Code) != output.ExitValidation {
		t.Errorf("--create misuse exit = %d, want %d (validation)", output.ExitCode(ce.Code), output.ExitValidation)
	}
}

// TestBodySetSwallowsFollowingFlagTwoArg verifies the swallowed-flag footgun in
// its 2-arg (trailing "-") shape: because --set is a string flag,
// "body <id> --section H --set --create -" binds "--create" as --set's value,
// leaving "-" as a second stray positional. That shape short-circuits at Args
// validation (2 args) before RunE ever runs, so the diagnostic must name the
// swallowed flag and steer to the correct order from the Args validator.
func TestBodySetSwallowsFollowingFlagTwoArg(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	// A placeholder id and empty dir are sufficient for the 2-arg shape: the
	// swallowed "--create" leaves "-" as a second stray positional, so the
	// counterfactual (guard removed) is codedExactArgs' arg-count error at
	// ValidateArgs — which fires before PersistentPreRunE builds the store,
	// independent of whether the id names a real nib. (The 1-arg tests DO need a
	// real nib, since their counterfactual reaches RunE.)
	dir := t.TempDir()

	rootCmd.SetArgs([]string{"--nibs-path", dir, "body", "placeholder-id", "--section", "## H", "--set", "--create", "-"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when --set swallowed --create, got nil")
	}
	var ce *output.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("error = %T, want *output.CodedError", err)
	}
	if output.ExitCode(ce.Code) != output.ExitValidation {
		t.Errorf("swallowed-flag exit = %d, want %d (validation)", output.ExitCode(ce.Code), output.ExitValidation)
	}
	if !strings.Contains(ce.Msg, "--create") {
		t.Errorf("message should name the swallowed flag --create, got: %q", ce.Msg)
	}
	if !strings.Contains(ce.Msg, "--set - --create") {
		t.Errorf("message should steer to `--set - --create`, got: %q", ce.Msg)
	}
}

// TestBodySetSwallowsFollowingFlagOneArg verifies the same footgun in its 1-arg
// shape (no trailing "-"): "body <id> --section H --set --create" leaves exactly
// one positional, so Args' count check passes. Without the guard, runBody would
// load the nib and then trip the generic ErrInlineProse in resolveBodyFlag
// ("--create" is not a valid channel). The Args-validator guard fires first for
// this shape too, naming the swallowed flag rather than emitting that unrelated
// inline-prose text. A REAL nib is required so the guard-removed counterfactual
// actually reaches resolveBodyFlag (an unknown id would fail earlier at the nib
// lookup instead), demonstrating the guard pre-empts the inline-prose path.
func TestBodySetSwallowsFollowingFlagOneArg(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	nibsDir, id := writeSetNib(t, "bdy-swallow-1arg", "## H\nhi\n")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--section", "## H", "--set", "--create"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when --set swallowed --create, got nil")
	}
	var ce *output.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("error = %T, want *output.CodedError", err)
	}
	if output.ExitCode(ce.Code) != output.ExitValidation {
		t.Errorf("swallowed-flag exit = %d, want %d (validation)", output.ExitCode(ce.Code), output.ExitValidation)
	}
	if !strings.Contains(ce.Msg, "--create") {
		t.Errorf("message should name the swallowed flag --create, got: %q", ce.Msg)
	}
	if !strings.Contains(ce.Msg, "--set - --create") {
		t.Errorf("message should steer to `--set - --create`, got: %q", ce.Msg)
	}
}

// TestBodyAppendSwallowsFollowingFlag verifies the --append arm of the same
// footgun: "body <id> --append --create -" binds "--create" as --append's value
// and steers to `--append - --create`.
func TestBodyAppendSwallowsFollowingFlag(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	// The guard short-circuits at ValidateArgs (see TwoArg), so no real nib is
	// needed — a placeholder id and empty dir exercise the same code path.
	dir := t.TempDir()

	rootCmd.SetArgs([]string{"--nibs-path", dir, "body", "placeholder-id", "--append", "--create", "-"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when --append swallowed --create, got nil")
	}
	var ce *output.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("error = %T, want *output.CodedError", err)
	}
	if output.ExitCode(ce.Code) != output.ExitValidation {
		t.Errorf("swallowed-flag exit = %d, want %d (validation)", output.ExitCode(ce.Code), output.ExitValidation)
	}
	if !strings.Contains(ce.Msg, "--create") {
		t.Errorf("message should name the swallowed flag --create, got: %q", ce.Msg)
	}
	if !strings.Contains(ce.Msg, "--append - --create") {
		t.Errorf("message should steer to `--append - --create`, got: %q", ce.Msg)
	}
}

// TestBodyAppendSwallowsFollowingFlagOneArg covers the --append arm of the
// footgun in its 1-arg shape (no trailing "-"): "body <id> --append --create"
// leaves exactly one positional, so Args passes the count check and RunE would
// otherwise trip a generic inline-prose error. The Args-validator guard fires
// here too, naming the swallowed flag and steering to `--append - --create`.
func TestBodyAppendSwallowsFollowingFlagOneArg(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	// A real nib is required: without the guard the counterfactual reaches RunE
	// and would trip the generic ErrInlineProse in resolveAppendFlag; an unknown
	// id would fail earlier at the nib lookup instead. See the --set 1-arg test.
	nibsDir, id := writeSetNib(t, "bdy-swallow-append-1arg", "## H\nhi\n")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--append", "--create"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when --append swallowed --create, got nil")
	}
	var ce *output.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("error = %T, want *output.CodedError", err)
	}
	if output.ExitCode(ce.Code) != output.ExitValidation {
		t.Errorf("swallowed-flag exit = %d, want %d (validation)", output.ExitCode(ce.Code), output.ExitValidation)
	}
	if !strings.Contains(ce.Msg, "--create") {
		t.Errorf("message should name the swallowed flag --create, got: %q", ce.Msg)
	}
	if !strings.Contains(ce.Msg, "--append - --create") {
		t.Errorf("message should steer to `--append - --create`, got: %q", ce.Msg)
	}
}

// TestBodySetSwallowsSingleDashFlag covers a SINGLE-dash swallow — the shape the
// widened swallowedFlagValue guard was added to catch. "body <id> --set -h"
// binds "-h" (cobra's auto --help shorthand, present on every command) as
// --set's value, so help never fires and the invocation reaches the Args
// validator with one positional. Without the guard, runBody would load the nib
// and then trip the generic ErrInlineProse in resolveBodyFlag ("-h" is not a
// valid channel); the guard fires first, naming the swallowed token and steering
// to `--set - -h`. A REAL nib is required so the guard-removed counterfactual
// reaches resolveBodyFlag rather than failing earlier at the nib lookup.
func TestBodySetSwallowsSingleDashFlag(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	nibsDir, id := writeSetNib(t, "bdy-swallow-shorthand", "## H\nhi\n")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--set", "-h"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when --set swallowed -h, got nil")
	}
	var ce *output.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("error = %T, want *output.CodedError", err)
	}
	if output.ExitCode(ce.Code) != output.ExitValidation {
		t.Errorf("swallowed-flag exit = %d, want %d (validation)", output.ExitCode(ce.Code), output.ExitValidation)
	}
	// The message must name the swallowed single-dash token and steer to the
	// corrected `--set - -h` form — NOT the pre-widening generic inline-prose text.
	if !strings.Contains(ce.Msg, "-h") {
		t.Errorf("message should name the swallowed token -h, got: %q", ce.Msg)
	}
	if !strings.Contains(ce.Msg, "--set - -h") {
		t.Errorf("message should steer to `--set - -h`, got: %q", ce.Msg)
	}
	if strings.Contains(ce.Msg, "inline text is not allowed") {
		t.Errorf("guard should replace the generic inline-prose fallback, got: %q", ce.Msg)
	}
}

// TestBodyValidChannelsNotSwallowed pins the load-bearing boundary of the
// widened swallowedFlagValue guard: the two legitimate input channels that a
// naive "any leading dash" check could misread must NOT false-positive. "-"
// (the stdin marker) and "@FILE" both drive a normal successful --set, proving
// the guard's `value != "-"` exclusion and the "@" prefix stay clear of it.
func TestBodyValidChannelsNotSwallowed(t *testing.T) {
	t.Run("stdin dash", func(t *testing.T) {
		t.Cleanup(resetBodyFlags)
		resetBodyFlags()

		nibsDir, id := writeSetNib(t, "bdy-chan-dash", "old body\n")
		withStdin(t, "piped body\n")

		rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--set", "-"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("body --set - should succeed, guard must not false-positive on \"-\": %v", err)
		}
		content := readNibFile(t, nibsDir, bodyNibFile(id))
		if !strings.Contains(content, "piped body") {
			t.Errorf("stdin body not written, got:\n%s", content)
		}
		// --set replaces the whole body; the seeded "old body" must be gone, or a
		// bug that appended instead of replacing would slip through this subtest.
		if strings.Contains(content, "old body") {
			t.Errorf("--set - should replace, not append; old body still present, got:\n%s", content)
		}
	})

	t.Run("file channel", func(t *testing.T) {
		t.Cleanup(resetBodyFlags)
		resetBodyFlags()

		nibsDir, id := writeSetNib(t, "bdy-chan-file", "old body\n")
		secFile := filepath.Join(t.TempDir(), "content.md")
		if err := os.WriteFile(secFile, []byte("file body\n"), 0644); err != nil {
			t.Fatal(err)
		}

		rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--set", "@" + secFile})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("body --set @FILE should succeed, guard must not false-positive on \"@FILE\": %v", err)
		}
		content := readNibFile(t, nibsDir, bodyNibFile(id))
		if !strings.Contains(content, "file body") {
			t.Errorf("file body not written, got:\n%s", content)
		}
		// --set replaces the whole body; the seeded "old body" must be gone, or a
		// bug that appended instead of replacing would slip through this subtest.
		if strings.Contains(content, "old body") {
			t.Errorf("--set @FILE should replace, not append; old body still present, got:\n%s", content)
		}
	})
}

// TestBodyCreateFalseStillErrorsOnMissingSection verifies that an explicit
// --create=false behaves exactly like omitting --create: a missing heading is a
// validation error (exit 2), NOT a silent upsert. Guards against passing the
// flag's presence (Changed) instead of its value into the upsert branch.
func TestBodyCreateFalseStillErrorsOnMissingSection(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	nibsDir, id := writeSetNib(t, "bdy-create-false", "## Intro\nhi\n")
	withStdin(t, "whatever\n")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--section", "## New", "--set", "-", "--create=false", "--json"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected --create=false on a missing section to error like omitting --create, got nil")
	}
	var ce *output.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("error = %T, want *output.CodedError", err)
	}
	if output.ExitCode(ce.Code) != output.ExitValidation {
		t.Errorf("--create=false missing-section exit = %d, want %d (validation)", output.ExitCode(ce.Code), output.ExitValidation)
	}
	// The section must NOT have been created (no silent upsert).
	content := readNibFile(t, nibsDir, bodyNibFile(id))
	if strings.Contains(content, "## New") {
		t.Errorf("--create=false must not create the section, got:\n%s", content)
	}
}

// TestBodyReplaceExactOnce verifies the surgical replace succeeds when the old
// text occurs exactly once.
func TestBodyReplaceExactOnce(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	nibsDir, id := writeSetNib(t, "bdy-5", "line one\nline two\n")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--replace-old", "line one", "--replace-new", "LINE ONE"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("body --replace-old/--replace-new should succeed, got: %v", err)
	}

	content := readNibFile(t, nibsDir, bodyNibFile(id))
	if !strings.Contains(content, "LINE ONE") || strings.Contains(content, "line one") {
		t.Errorf("surgical replace did not apply, got:\n%s", content)
	}
	if !strings.Contains(content, "line two") {
		t.Errorf("unrelated content should survive, got:\n%s", content)
	}
}

// TestBodyReplaceNotFound verifies a zero-match surgical replace yields
// TEXT_NOT_FOUND with occurrences 0 and exit 2, and leaves the nib untouched.
func TestBodyReplaceNotFound(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	nibsDir, id := writeSetNib(t, "bdy-6", "the original body\n")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--replace-old", "absent-text", "--replace-new", "x", "--json"})
	var execErr error
	out := captureStdout(t, func() { execErr = rootCmd.Execute() })
	if execErr == nil {
		t.Fatal("expected TEXT_NOT_FOUND, got nil")
	}
	var ce *output.CodedError
	if !errors.As(execErr, &ce) {
		t.Fatalf("error = %T, want *output.CodedError", execErr)
	}
	if ce.Code != output.ErrTextNotFound {
		t.Errorf("code = %q, want %q", ce.Code, output.ErrTextNotFound)
	}
	if output.ExitCode(ce.Code) != output.ExitValidation {
		t.Errorf("exit = %d, want %d (validation)", output.ExitCode(ce.Code), output.ExitValidation)
	}

	var envelope struct {
		Error struct {
			Code        string `json:"code"`
			Occurrences *int   `json:"occurrences"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("error output is not valid JSON: %v\n%s", err, out)
	}
	if envelope.Error.Code != output.ErrTextNotFound {
		t.Errorf("envelope code = %q, want %q", envelope.Error.Code, output.ErrTextNotFound)
	}
	if envelope.Error.Occurrences == nil || *envelope.Error.Occurrences != 0 {
		t.Errorf("envelope occurrences = %v, want 0", envelope.Error.Occurrences)
	}

	// The nib must be unchanged by the rejected replace.
	if content := readNibFile(t, nibsDir, bodyNibFile(id)); !strings.Contains(content, "the original body") {
		t.Errorf("rejected replace must not mutate the nib, got:\n%s", content)
	}
}

// TestBodyReplaceAmbiguous verifies a multi-match surgical replace yields
// TEXT_AMBIGUOUS with the occurrence count and exit 2.
func TestBodyReplaceAmbiguous(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	// "dup" appears exactly twice.
	nibsDir, id := writeSetNib(t, "bdy-7", "dup here\nand dup there\n")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--replace-old", "dup", "--replace-new", "x", "--json"})
	var execErr error
	out := captureStdout(t, func() { execErr = rootCmd.Execute() })
	if execErr == nil {
		t.Fatal("expected TEXT_AMBIGUOUS, got nil")
	}
	var ce *output.CodedError
	if !errors.As(execErr, &ce) {
		t.Fatalf("error = %T, want *output.CodedError", execErr)
	}
	if ce.Code != output.ErrTextAmbiguous {
		t.Errorf("code = %q, want %q", ce.Code, output.ErrTextAmbiguous)
	}
	if output.ExitCode(ce.Code) != output.ExitValidation {
		t.Errorf("exit = %d, want %d (validation)", output.ExitCode(ce.Code), output.ExitValidation)
	}

	var envelope struct {
		Error struct {
			Code        string `json:"code"`
			Occurrences *int   `json:"occurrences"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("error output is not valid JSON: %v\n%s", err, out)
	}
	if envelope.Error.Occurrences == nil || *envelope.Error.Occurrences != 2 {
		t.Errorf("envelope occurrences = %v, want 2", envelope.Error.Occurrences)
	}
}

// TestBodySectionRequiresSet verifies that --section without --set is a usage
// error naming the missing content channel.
func TestBodySectionRequiresSet(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	nibsDir, id := writeSetNib(t, "bdy-8", "## Notes\nhi\n")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--section", "## Notes", "--json"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected --section without --set to error, got nil")
	}
	var ce *output.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("error = %T, want *output.CodedError", err)
	}
	if output.ExitCode(ce.Code) != output.ExitValidation {
		t.Errorf("exit = %d, want %d (validation)", output.ExitCode(ce.Code), output.ExitValidation)
	}
}

// TestBodyNoOperation verifies that invoking body with no operation flag is a
// validation error.
func TestBodyNoOperation(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	nibsDir, id := writeSetNib(t, "bdy-9", "body\n")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--json"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for no body operation, got nil")
	}
	var ce *output.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("error = %T, want *output.CodedError", err)
	}
	if output.ExitCode(ce.Code) != output.ExitValidation {
		t.Errorf("exit = %d, want %d (validation)", output.ExitCode(ce.Code), output.ExitValidation)
	}
}

// TestBodyMutuallyExclusiveOps verifies two primary operations cannot be
// combined.
func TestBodyMutuallyExclusiveOps(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	nibsDir, id := writeSetNib(t, "bdy-10", "body\n")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--set", "-", "--append", "-"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected mutual-exclusion error for --set + --append, got nil")
	}
	if !strings.Contains(err.Error(), "cannot be set together") &&
		!strings.Contains(err.Error(), "if any flags in the group") {
		t.Errorf("expected mutual-exclusion error, got: %s", err)
	}
}

// TestBodyReplaceRequiresNew verifies --replace-old requires --replace-new.
func TestBodyReplaceRequiresNew(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	nibsDir, id := writeSetNib(t, "bdy-11", "body\n")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--replace-old", "body"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected --replace-old without --replace-new to error, got nil")
	}
	if !strings.Contains(err.Error(), "replace-new") &&
		!strings.Contains(err.Error(), "if any flags in the group") {
		t.Errorf("expected required-together error mentioning replace-new, got: %s", err)
	}
}

// TestBodyMissingFileIsIOError verifies a missing `@FILE` maps to the I/O exit
// code (5), not a validation error.
func TestBodyMissingFileIsIOError(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	nibsDir, id := writeSetNib(t, "bdy-12", "body\n")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--set", "@/no/such/file-xyz.md", "--json"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected missing @file to error, got nil")
	}
	var ce *output.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("error = %T, want *output.CodedError", err)
	}
	if output.ExitCode(ce.Code) != output.ExitIO {
		t.Errorf("missing @file exit = %d, want %d (IO)", output.ExitCode(ce.Code), output.ExitIO)
	}
}

// headingLevelCounts tallies exact heading lines by their marker level so tests
// can distinguish "## X" from "### X" without substring confusion.
func headingLevelCounts(content, text string) (l2, l3 int) {
	for _, line := range strings.Split(content, "\n") {
		switch strings.TrimSpace(line) {
		case "## " + text:
			l2++
		case "### " + text:
			l3++
		}
	}
	return l2, l3
}

// TestBodySectionCreateWarnsOnShadowingAppend verifies the shadowing-append
// safety net: `--section "### X" --set - --create` against a body whose only "X"
// is a LEVEL-2 "## X" appends a fresh level-3 "### X" (exit 0) AND emits a
// warn-only stderr notice, because wildcard readers (mdsection.Find at AnyLevel)
// surface only the first same-text heading, so the appended section is
// written-but-shadowed. The warning must reach stderr, not stdout.
func TestBodySectionCreateWarnsOnShadowingAppend(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	nibsDir, id := writeSetNib(t, "bdy-shadow-1", "## X\n- a\n")
	withStdin(t, "sub")

	var errBuf bytes.Buffer
	rootCmd.SetErr(&errBuf)
	t.Cleanup(func() { rootCmd.SetErr(nil); rootCmd.SetOut(nil) })

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--section", "### X", "--set", "-", "--create"})
	var execErr error
	out := captureStdout(t, func() { execErr = rootCmd.Execute() })
	if execErr != nil {
		t.Fatalf("shadowing --create append should succeed (exit 0), got: %v", execErr)
	}

	// The append still happens: the level-2 "## X" survives and a new level-3
	// "### X" is added with the piped content.
	content := readNibFile(t, nibsDir, bodyNibFile(id))
	if l2, l3 := headingLevelCounts(content, "X"); l2 != 1 || l3 != 1 {
		t.Errorf("expected one '## X' and one '### X', got l2=%d l3=%d, body:\n%s", l2, l3, content)
	}
	if !strings.Contains(content, "sub") {
		t.Errorf("appended level-3 content missing, got:\n%s", content)
	}
	// The pre-existing level-2 content must survive the append (not clobbered).
	if !strings.Contains(content, "- a") {
		t.Errorf("pre-existing '## X' content ('- a') must survive the append, got:\n%s", content)
	}

	// The warning goes to stderr (never stdout, which stays the clean card) and
	// names the shadow: the spelled section and the level mismatch.
	if strings.Contains(out, "warning:") {
		t.Errorf("warning leaked onto stdout; got:\n%s", out)
	}
	warn := errBuf.String()
	for _, want := range []string{"warning:", "### X", "another level"} {
		if !strings.Contains(warn, want) {
			t.Errorf("stderr warning missing %q; got:\n%s", want, warn)
		}
	}
}

// TestBodySectionCreateShadowingAppendJSONWarningsField pins the --json warnings
// contract (mirroring `nibs new`'s possible_duplicates sibling): a shadowing
// `--section "### X" --set - --create --json` echoes a single valid JSON doc
// decoding to {"nib":{...},"warnings":["…another level…"]}, the nib id matches,
// the card stays lean (no body), and nothing leaks to stderr in JSON mode.
func TestBodySectionCreateShadowingAppendJSONWarningsField(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	nibsDir, id := writeSetNib(t, "bdy-shadow-json", "## X\n- a\n")
	withStdin(t, "sub")

	var errBuf bytes.Buffer
	rootCmd.SetErr(&errBuf)
	t.Cleanup(func() { rootCmd.SetErr(nil); rootCmd.SetOut(nil) })

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--section", "### X", "--set", "-", "--create", "--json"})
	var execErr error
	out := captureStdout(t, func() { execErr = rootCmd.Execute() })
	if execErr != nil {
		t.Fatalf("shadowing --create --json append should succeed (exit 0), got: %v", execErr)
	}

	var envelope map[string]any
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("stdout is not a single valid JSON doc: %v\n%s", err, out)
	}
	nibObj, ok := envelope["nib"].(map[string]any)
	if !ok {
		t.Fatalf("expected {nib:{...}} echo, got: %s", out)
	}
	if nibObj["id"] != id {
		t.Errorf("echoed nib id = %v, want %q", nibObj["id"], id)
	}
	// Lean card: no body field on the echoed nib.
	if _, present := nibObj["body"]; present {
		t.Errorf("lean card should not include body, got: %s", out)
	}
	warnings, ok := envelope["warnings"].([]any)
	if !ok || len(warnings) != 1 {
		t.Fatalf("expected a warnings array of length 1, got: %s", out)
	}
	warnStr, _ := warnings[0].(string)
	for _, want := range []string{"### X", "another level"} {
		if !strings.Contains(warnStr, want) {
			t.Errorf("warnings[0] missing %q; got: %q", want, warnStr)
		}
	}
	// In --json mode the warning rides the JSON sibling, not stderr.
	if warn := errBuf.String(); strings.Contains(warn, "warning:") {
		t.Errorf("--json mode must not write the warning to stderr, got:\n%s", warn)
	}
}

// TestBodySectionCreateNonShadowingJSONOmitsWarnings verifies the companion
// contract (mirroring `nibs new`'s omitted-possible_duplicates test): a
// non-shadowing `--create --json` echo omits the "warnings" key entirely and
// decodes to just {nib}.
func TestBodySectionCreateNonShadowingJSONOmitsWarnings(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	nibsDir, id := writeSetNib(t, "bdy-nowarn-json", "## Intro\nkeep\n")
	withStdin(t, "fresh")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--section", "### New", "--set", "-", "--create", "--json"})
	out := captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("non-shadowing --create --json should succeed, got: %v", err)
		}
	})

	if strings.Contains(out, "warnings") {
		t.Errorf("non-shadowing --json should omit the warnings key; got:\n%s", out)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, out)
	}
	if _, present := envelope["warnings"]; present {
		t.Errorf("non-shadowing --json must not carry a warnings key; got:\n%s", out)
	}
}

// TestBodySectionCreateReplaceEmitsNoWarning verifies the in-place replace path
// stays quiet: `--section "### X" --set - --create` on a body that ALREADY has a
// level-3 "### X" replaces its content (no append, no duplicate heading), so the
// shadowing warning must NOT fire.
func TestBodySectionCreateReplaceEmitsNoWarning(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	nibsDir, id := writeSetNib(t, "bdy-shadow-2", "### X\nold\n")
	withStdin(t, "new sub")

	var errBuf bytes.Buffer
	rootCmd.SetErr(&errBuf)
	t.Cleanup(func() { rootCmd.SetErr(nil); rootCmd.SetOut(nil) })

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--section", "### X", "--set", "-", "--create"})
	_ = captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("in-place replace should succeed, got: %v", err)
		}
	})

	content := readNibFile(t, nibsDir, bodyNibFile(id))
	if _, l3 := headingLevelCounts(content, "X"); l3 != 1 {
		t.Errorf("expected the level-3 heading to stay exactly once, got l3=%d, body:\n%s", l3, content)
	}
	if !strings.Contains(content, "new sub") || strings.Contains(content, "old") {
		t.Errorf("expected in-place content replacement, got:\n%s", content)
	}
	if warn := errBuf.String(); strings.Contains(warn, "warning:") {
		t.Errorf("in-place replace must not warn, got stderr:\n%s", warn)
	}
}

// TestBodySectionCreateCleanAppendEmitsNoWarning verifies a --create append with
// NO pre-existing same-text heading at any level stays quiet: there is nothing to
// shadow, so the warning must NOT fire.
func TestBodySectionCreateCleanAppendEmitsNoWarning(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	nibsDir, id := writeSetNib(t, "bdy-shadow-3", "## Intro\nkeep\n")
	withStdin(t, "fresh")

	var errBuf bytes.Buffer
	rootCmd.SetErr(&errBuf)
	t.Cleanup(func() { rootCmd.SetErr(nil); rootCmd.SetOut(nil) })

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--section", "### New", "--set", "-", "--create"})
	_ = captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("clean append should succeed, got: %v", err)
		}
	})

	content := readNibFile(t, nibsDir, bodyNibFile(id))
	if _, l3 := headingLevelCounts(content, "New"); l3 != 1 {
		t.Errorf("expected a new '### New' exactly once, got l3=%d, body:\n%s", l3, content)
	}
	if !strings.Contains(content, "fresh") {
		t.Errorf("appended content missing, got:\n%s", content)
	}
	// The pre-existing section must survive the append (not clobbered).
	if !strings.Contains(content, "## Intro") || !strings.Contains(content, "keep") {
		t.Errorf("pre-existing '## Intro' section must survive the append, got:\n%s", content)
	}
	if warn := errBuf.String(); strings.Contains(warn, "warning:") {
		t.Errorf("clean append must not warn, got stderr:\n%s", warn)
	}
}

// TestBodySectionCreateBareRequestEmitsNoWarning verifies a bare (level-agnostic)
// `--section "X" --set - --create` against a body with a level-2 "## X" replaces
// that heading in place (bare = wildcard match) rather than appending, so no
// shadow is created and the warning must NOT fire.
func TestBodySectionCreateBareRequestEmitsNoWarning(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	nibsDir, id := writeSetNib(t, "bdy-shadow-4", "## X\n- a\n")
	withStdin(t, "replaced")

	var errBuf bytes.Buffer
	rootCmd.SetErr(&errBuf)
	t.Cleanup(func() { rootCmd.SetErr(nil); rootCmd.SetOut(nil) })

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--section", "X", "--set", "-", "--create"})
	_ = captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("bare --create should succeed, got: %v", err)
		}
	})

	content := readNibFile(t, nibsDir, bodyNibFile(id))
	if l2, l3 := headingLevelCounts(content, "X"); l2 != 1 || l3 != 0 {
		t.Errorf("bare wildcard should replace '## X' in place (one '## X', no '### X'), got l2=%d l3=%d, body:\n%s", l2, l3, content)
	}
	if !strings.Contains(content, "replaced") || strings.Contains(content, "- a") {
		t.Errorf("expected in-place replacement of the level-2 section, got:\n%s", content)
	}
	if warn := errBuf.String(); strings.Contains(warn, "warning:") {
		t.Errorf("bare replace must not warn, got stderr:\n%s", warn)
	}
}

// TestBodySectionCreateParentheticalOnlyEmitsNoWarning pins the exactness of the
// shadow trigger: a body whose ONLY same-name heading is a PARENTHETICAL
// "## Key Decisions (Phase 1)" is NOT shadowed by an appended exact
// "### Key Decisions" — the exact heading WINS the wildcard read (Find prefers
// exact over parenthetical), so `--section "### Key Decisions" --create` must
// append without warning. Keying the check on Find (which falls back to the
// parenthetical) instead of FindExact would wrongly warn here.
func TestBodySectionCreateParentheticalOnlyEmitsNoWarning(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	nibsDir, id := writeSetNib(t, "bdy-paren-1", "## Key Decisions (Phase 1)\n- old\n")
	withStdin(t, "sub")

	var errBuf bytes.Buffer
	rootCmd.SetErr(&errBuf)
	t.Cleanup(func() { rootCmd.SetErr(nil); rootCmd.SetOut(nil) })

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--section", "### Key Decisions", "--set", "-", "--create"})
	_ = captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("parenthetical-only --create append should succeed, got: %v", err)
		}
	})

	content := readNibFile(t, nibsDir, bodyNibFile(id))
	if !strings.Contains(content, "## Key Decisions (Phase 1)") || !strings.Contains(content, "- old") {
		t.Errorf("parenthetical section must survive the append, got:\n%s", content)
	}
	if !strings.Contains(content, "### Key Decisions") || !strings.Contains(content, "sub") {
		t.Errorf("exact level-3 section should be appended, got:\n%s", content)
	}
	if warn := errBuf.String(); strings.Contains(warn, "warning:") {
		t.Errorf("appended exact heading wins the wildcard read (not shadowed) — must NOT warn, got stderr:\n%s", warn)
	}
}

// TestBodySectionStrictParentheticalOnlyKeepsCreateSuggestion pins the strict
// counterpart: when the only same-name heading is a PARENTHETICAL one, the
// strict not-found error must keep the NORMAL --create guidance (an exact
// --create heading would win the read), NOT the "another level" teaching text
// reserved for an exact heading at a different level.
func TestBodySectionStrictParentheticalOnlyKeepsCreateSuggestion(t *testing.T) {
	t.Cleanup(resetBodyFlags)
	resetBodyFlags()

	nibsDir, id := writeSetNib(t, "bdy-paren-2", "## Key Decisions (Phase 1)\n- old\n")
	withStdin(t, "y")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "body", id, "--section", "### Key Decisions", "--set", "-", "--json"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected a strict not-found for a parenthetical-only heading, got nil")
	}
	var ce *output.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("error = %T, want *output.CodedError", err)
	}
	if strings.Contains(ce.Msg, "another level") {
		t.Errorf("parenthetical-only miss must NOT use the 'another level' teaching text, got: %q", ce.Msg)
	}
	if !strings.Contains(ce.Msg, "--create") {
		t.Errorf("parenthetical-only miss should keep the --create suggestion, got: %q", ce.Msg)
	}
}
