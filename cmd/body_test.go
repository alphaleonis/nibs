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

	resetBodyFlags()

	if bodySet != "" || bodyAppend != "" || bodySection != "" ||
		bodyReplaceOld != "" || bodyReplaceNew != "" || bodyIfMatch != "" || bodyJSON {
		t.Errorf("resetBodyFlags left non-zero state: set=%q append=%q section=%q old=%q new=%q ifMatch=%q json=%v",
			bodySet, bodyAppend, bodySection, bodyReplaceOld, bodyReplaceNew, bodyIfMatch, bodyJSON)
	}
}

// bodyNibFile is the on-disk filename writeSetNib produces for a nib id.
func bodyNibFile(id string) string { return id + "--test.md" }

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
