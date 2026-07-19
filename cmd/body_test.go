package cmd

import (
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
