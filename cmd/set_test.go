package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/spf13/pflag"
)

// resetSetFlags resets ALL global flag variables for the set command.
// Cobra's StringArrayVar appends to slices across Execute() calls on the
// singleton rootCmd, so every global must be zeroed between tests.
// Also resets Cobra's "Changed" tracking on all flags to prevent stale state.
// Persistent-flag reset (--config, --nibs-path, plus their pflag Value/Changed
// bits) is delegated to the shared resetRootPersistentFlags helper.
// These tests must NOT use t.Parallel() — they share the rootCmd singleton.
func resetSetFlags() {
	setStatus = ""
	setType = ""
	setPriority = ""
	setEstimate = ""
	setTitle = ""
	setClear = nil
	setBody = ""
	setBodyFile = ""
	setBodyReplaceOld = nil
	setBodyReplaceNew = nil
	setBodyAppend = ""
	setParent = ""
	setBlocking = nil
	setRemoveBlocking = nil
	setBlockedBy = nil
	setRemoveBlockedBy = nil
	setTag = nil
	setRemoveTag = nil
	setDocument = nil
	setRemoveDocument = nil
	setAfter = ""
	setBefore = ""
	setFirst = false
	setIfMatch = ""
	setJSON = false
	// Reset Cobra's "Changed" tracking on all flags
	setCmd.Flags().Visit(func(f *pflag.Flag) {
		f.Changed = false
	})
	resetRootPersistentFlags()
}

func TestResetSetFlagsClearsAllState(t *testing.T) {
	// Set every global to a non-zero value
	setStatus = "in-progress"
	setType = "bug"
	setPriority = "critical"
	setEstimate = "xl"
	setTitle = "dirty"
	setClear = []string{"priority"}
	setBody = "dirty"
	setBodyFile = "dirty"
	setBodyReplaceOld = []string{"a"}
	setBodyReplaceNew = []string{"b"}
	setBodyAppend = "dirty"
	setParent = "dirty"
	setBlocking = []string{"x"}
	setRemoveBlocking = []string{"y"}
	setBlockedBy = []string{"z"}
	setRemoveBlockedBy = []string{"w"}
	setTag = []string{"t"}
	setRemoveTag = []string{"r"}
	setDocument = []string{"d"}
	setRemoveDocument = []string{"e"}
	setAfter = "dirty"
	setBefore = "dirty"
	setFirst = true
	setIfMatch = "dirty"
	setJSON = true

	resetSetFlags()

	// Verify all are at zero values
	if setStatus != "" {
		t.Errorf("setStatus not reset: %q", setStatus)
	}
	if setType != "" {
		t.Errorf("setType not reset: %q", setType)
	}
	if setPriority != "" {
		t.Errorf("setPriority not reset: %q", setPriority)
	}
	if setEstimate != "" {
		t.Errorf("setEstimate not reset: %q", setEstimate)
	}
	if setTitle != "" {
		t.Errorf("setTitle not reset: %q", setTitle)
	}
	if setClear != nil {
		t.Errorf("setClear not reset: %v", setClear)
	}
	if setBody != "" {
		t.Errorf("setBody not reset: %q", setBody)
	}
	if setBodyFile != "" {
		t.Errorf("setBodyFile not reset: %q", setBodyFile)
	}
	if setBodyReplaceOld != nil {
		t.Errorf("setBodyReplaceOld not reset: %v", setBodyReplaceOld)
	}
	if setBodyReplaceNew != nil {
		t.Errorf("setBodyReplaceNew not reset: %v", setBodyReplaceNew)
	}
	if setBodyAppend != "" {
		t.Errorf("setBodyAppend not reset: %q", setBodyAppend)
	}
	if setParent != "" {
		t.Errorf("setParent not reset: %q", setParent)
	}
	if setBlocking != nil {
		t.Errorf("setBlocking not reset: %v", setBlocking)
	}
	if setRemoveBlocking != nil {
		t.Errorf("setRemoveBlocking not reset: %v", setRemoveBlocking)
	}
	if setBlockedBy != nil {
		t.Errorf("setBlockedBy not reset: %v", setBlockedBy)
	}
	if setRemoveBlockedBy != nil {
		t.Errorf("setRemoveBlockedBy not reset: %v", setRemoveBlockedBy)
	}
	if setTag != nil {
		t.Errorf("setTag not reset: %v", setTag)
	}
	if setRemoveTag != nil {
		t.Errorf("setRemoveTag not reset: %v", setRemoveTag)
	}
	if setDocument != nil {
		t.Errorf("setDocument not reset: %v", setDocument)
	}
	if setRemoveDocument != nil {
		t.Errorf("setRemoveDocument not reset: %v", setRemoveDocument)
	}
	if setAfter != "" {
		t.Errorf("setAfter not reset: %q", setAfter)
	}
	if setBefore != "" {
		t.Errorf("setBefore not reset: %q", setBefore)
	}
	if setFirst {
		t.Error("setFirst not reset")
	}
	if setIfMatch != "" {
		t.Errorf("setIfMatch not reset: %q", setIfMatch)
	}
	if setJSON {
		t.Error("setJSON not reset")
	}
}

func TestSetRejectsDuplicateBodyReplaceFlags(t *testing.T) {
	t.Cleanup(func() {
		resetSetFlags()
	})
	resetSetFlags()

	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatal(err)
	}
	nibContent := "---\ntitle: Test\nstatus: todo\ntype: task\n---\nline one\nline two\n"
	if err := os.WriteFile(filepath.Join(nibsDir, "dup-1--test.md"), []byte(nibContent), 0644); err != nil {
		t.Fatal(err)
	}

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"set", "dup-1",
		"--body-replace-old", "line one", "--body-replace-new", "LINE ONE",
		"--body-replace-old", "line two", "--body-replace-new", "LINE TWO",
	})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when --body-replace-old is specified multiple times, got nil")
	}
	if !strings.Contains(err.Error(), "cannot be specified multiple times") {
		t.Errorf("error should mention 'cannot be specified multiple times', got: %s", err)
	}
}

func TestSetSingleBodyReplaceWorks(t *testing.T) {
	t.Cleanup(func() {
		resetSetFlags()
	})
	resetSetFlags()

	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatal(err)
	}
	nibContent := "---\ntitle: Test\nstatus: todo\ntype: task\n---\nline one\nline two\n"
	if err := os.WriteFile(filepath.Join(nibsDir, "dup-2--test.md"), []byte(nibContent), 0644); err != nil {
		t.Fatal(err)
	}

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"set", "dup-2",
		"--body-replace-old", "line one", "--body-replace-new", "LINE ONE",
	})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("single --body-replace-old should succeed, got error: %v", err)
	}
}

// writeSetNib creates a nibs dir with a single nib and returns (nibsDir, id).
func writeSetNib(t *testing.T, id, body string) (string, string) {
	t.Helper()
	nibsDir := filepath.Join(t.TempDir(), ".nibs")
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\ntitle: Test\nstatus: todo\ntype: task\n---\n" + body
	if err := os.WriteFile(filepath.Join(nibsDir, id+"--test.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return nibsDir, id
}

// TestSetBodyFromFile verifies `set --body @FILE` replaces the body.
func TestSetBodyFromFile(t *testing.T) {
	t.Cleanup(resetSetFlags)
	resetSetFlags()

	nibsDir, id := writeSetNib(t, "upd-1", "old body\n")
	bodyFile := filepath.Join(t.TempDir(), "new.md")
	if err := os.WriteFile(bodyFile, []byte("brand new body\n"), 0644); err != nil {
		t.Fatal(err)
	}

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "set", id, "--body", "@" + bodyFile})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("set --body @file failed: %v", err)
	}

	content, _ := os.ReadFile(filepath.Join(nibsDir, id+"--test.md"))
	if !strings.Contains(string(content), "brand new body") {
		t.Errorf("body not replaced from file, got:\n%s", content)
	}
	if strings.Contains(string(content), "old body") {
		t.Errorf("old body should be gone, got:\n%s", content)
	}
}

// TestSetBodyAppendFromFile verifies `--body-append @FILE` appends the file
// content (with the trailing newline trimmed).
func TestSetBodyAppendFromFile(t *testing.T) {
	t.Cleanup(resetSetFlags)
	resetSetFlags()

	nibsDir, id := writeSetNib(t, "upd-2", "existing body\n")
	appendFile := filepath.Join(t.TempDir(), "add.md")
	if err := os.WriteFile(appendFile, []byte("## Added\nmore text\n\n"), 0644); err != nil {
		t.Fatal(err)
	}

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "set", id, "--body-append", "@" + appendFile})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("set --body-append @file failed: %v", err)
	}

	content, _ := os.ReadFile(filepath.Join(nibsDir, id+"--test.md"))
	if !strings.Contains(string(content), "existing body") || !strings.Contains(string(content), "more text") {
		t.Errorf("append did not combine bodies, got:\n%s", content)
	}
}

// TestSetRejectsInlineBody verifies the inline `--body "<string>"` form is
// gone on set: a bare value is a validation error.
func TestSetRejectsInlineBody(t *testing.T) {
	t.Cleanup(resetSetFlags)
	resetSetFlags()

	nibsDir, id := writeSetNib(t, "upd-3", "body\n")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "set", id, "--body", "inline replacement", "--json"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected inline --body to be rejected, got nil")
	}
	var ce *output.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("error = %T, want *output.CodedError", err)
	}
	if output.ExitCode(ce.Code) != output.ExitValidation {
		t.Errorf("inline --body exit = %d, want %d (validation)", output.ExitCode(ce.Code), output.ExitValidation)
	}
}

// TestSetMissingBodyFileIsIOError verifies a missing `@FILE` on set maps
// to the I/O exit code (5), not a validation error.
func TestSetMissingBodyFileIsIOError(t *testing.T) {
	t.Cleanup(resetSetFlags)
	resetSetFlags()

	nibsDir, id := writeSetNib(t, "upd-4", "body\n")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "set", id, "--body", "@/no/such/file-xyz.md", "--json"})
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

// TestSetStatusEchoesLeanCard verifies `set --status --json` echoes the lean
// {nib} card (get contract), NOT the full body/etag.
func TestSetStatusEchoesLeanCard(t *testing.T) {
	t.Cleanup(resetSetFlags)
	resetSetFlags()

	nibsDir, id := writeSetNib(t, "card-1", "## Notes\nsecret body text\n")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "set", id, "--status", "in-progress", "--json"})
	var execErr error
	out := captureStdout(t, func() { execErr = rootCmd.Execute() })
	if execErr != nil {
		t.Fatalf("set --status --json should succeed, got: %v", execErr)
	}

	var envelope map[string]any
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	nibObj, ok := envelope["nib"].(map[string]any)
	if !ok {
		t.Fatalf("expected {nib:{...}} echo, got: %s", out)
	}
	if nibObj["status"] != "in-progress" {
		t.Errorf("echoed status = %v, want in-progress", nibObj["status"])
	}
	// The lean card must not carry the body.
	if _, present := nibObj["body"]; present {
		t.Errorf("lean card should not include body, got: %s", out)
	}
	if strings.Contains(out, "secret body text") {
		t.Errorf("body content leaked into card echo: %s", out)
	}
}

// TestSetClearPriority verifies `--clear priority` removes the priority.
func TestSetClearPriority(t *testing.T) {
	t.Cleanup(resetSetFlags)
	resetSetFlags()

	nibsDir := filepath.Join(t.TempDir(), ".nibs")
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\ntitle: Test\nstatus: todo\ntype: task\npriority: critical\n---\n"
	if err := os.WriteFile(filepath.Join(nibsDir, "clr-1--test.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "set", "clr-1", "--clear", "priority"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("set --clear priority should succeed, got: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(nibsDir, "clr-1--test.md"))
	if strings.Contains(string(data), "priority: critical") {
		t.Errorf("priority should be cleared, got:\n%s", data)
	}
	if strings.Contains(string(data), "priority:") {
		t.Errorf("priority key should be gone, got:\n%s", data)
	}
}

// TestSetClearEstimate verifies `--clear estimate` removes the estimate.
func TestSetClearEstimate(t *testing.T) {
	t.Cleanup(resetSetFlags)
	resetSetFlags()

	nibsDir := filepath.Join(t.TempDir(), ".nibs")
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\ntitle: Test\nstatus: todo\ntype: task\nestimate: xl\n---\n"
	if err := os.WriteFile(filepath.Join(nibsDir, "clr-2--test.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "set", "clr-2", "--clear", "estimate"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("set --clear estimate should succeed, got: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(nibsDir, "clr-2--test.md"))
	if strings.Contains(string(data), "estimate: xl") {
		t.Errorf("estimate should be cleared, got:\n%s", data)
	}
}

// TestSetClearParent verifies `--clear parent` detaches the child from its parent.
func TestSetClearParent(t *testing.T) {
	t.Cleanup(resetSetFlags)
	resetSetFlags()

	nibsDir := filepath.Join(t.TempDir(), ".nibs")
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatal(err)
	}
	parent := "---\ntitle: Parent\nstatus: todo\ntype: epic\n---\n"
	child := "---\ntitle: Child\nstatus: todo\ntype: task\nparent: par-1\n---\n"
	if err := os.WriteFile(filepath.Join(nibsDir, "par-1--parent.md"), []byte(parent), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nibsDir, "chi-1--child.md"), []byte(child), 0644); err != nil {
		t.Fatal(err)
	}

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "set", "chi-1", "--clear", "parent"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("set --clear parent should succeed, got: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(nibsDir, "chi-1--child.md"))
	if strings.Contains(string(data), "parent: par-1") {
		t.Errorf("parent should be cleared, got:\n%s", data)
	}
}

// TestSetRejectsSetAndClearSameField verifies setting and clearing the same
// field in one invocation is a usage error.
func TestSetRejectsSetAndClearSameField(t *testing.T) {
	t.Cleanup(resetSetFlags)
	resetSetFlags()

	nibsDir := filepath.Join(t.TempDir(), ".nibs")
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\ntitle: Test\nstatus: todo\ntype: task\n---\n"
	if err := os.WriteFile(filepath.Join(nibsDir, "clr-3--test.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "set", "clr-3", "--priority", "high", "--clear", "priority", "--json"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for set-and-clear of same field, got nil")
	}
	var ce *output.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("error = %T, want *output.CodedError", err)
	}
	if output.ExitCode(ce.Code) != output.ExitValidation {
		t.Errorf("set-and-clear exit = %d, want %d (validation)", output.ExitCode(ce.Code), output.ExitValidation)
	}
}

// TestSetRejectsInvalidClearField verifies an unknown --clear field name is a
// usage error naming the allowed set.
func TestSetRejectsInvalidClearField(t *testing.T) {
	t.Cleanup(resetSetFlags)
	resetSetFlags()

	nibsDir, id := writeSetNib(t, "clr-4", "body\n")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "set", id, "--clear", "title", "--json"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid --clear field, got nil")
	}
	var ce *output.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("error = %T, want *output.CodedError", err)
	}
	if output.ExitCode(ce.Code) != output.ExitValidation {
		t.Errorf("invalid --clear exit = %d, want %d (validation)", output.ExitCode(ce.Code), output.ExitValidation)
	}
}

// TestUpdateAliasStillWorks verifies `update` remains a working alias of `set`.
func TestUpdateAliasStillWorks(t *testing.T) {
	t.Cleanup(resetSetFlags)
	resetSetFlags()

	nibsDir, id := writeSetNib(t, "alias-1", "body\n")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "update", id, "--status", "todo"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("update alias should still work, got: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(nibsDir, id+"--test.md"))
	if !strings.Contains(string(data), "status: todo") {
		t.Errorf("status should be todo via update alias, got:\n%s", data)
	}
}

// TestSetStaleIfMatchConflictCarriesCurrentEtag verifies a stale --if-match
// yields a CONFLICT (exit 4) whose --json envelope carries the server's current
// etag so an agent can retry with it.
func TestSetStaleIfMatchConflictCarriesCurrentEtag(t *testing.T) {
	t.Cleanup(resetSetFlags)
	resetSetFlags()

	nibsDir := filepath.Join(t.TempDir(), ".nibs")
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatal(err)
	}
	// version: 1 and the `# id` comment match Render() output so the etag we
	// compute from the file agrees with what the core computes.
	content := "---\n# cnf-1\nversion: 1\ntitle: Test\nstatus: todo\ntype: task\norder: a0\n---\n\n"
	if err := os.WriteFile(filepath.Join(nibsDir, "cnf-1--test.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Compute the etag before any mutation — this becomes the stale token.
	f, err := os.Open(filepath.Join(nibsDir, "cnf-1--test.md"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := nib.Parse(f)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	parsed.ID = "cnf-1"
	staleEtag := parsed.ETag()

	// Mutate once (no if-match) so the on-disk etag advances past staleEtag.
	resetSetFlags()
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "set", "cnf-1", "--status", "in-progress"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("priming mutation should succeed, got: %v", err)
	}

	// Now set with the stale etag — expect a CONFLICT carrying currentEtag.
	resetSetFlags()
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "set", "cnf-1", "--status", "todo", "--if-match", staleEtag, "--json"})
	var execErr error
	out := captureStdout(t, func() { execErr = rootCmd.Execute() })

	if execErr == nil {
		t.Fatal("expected CONFLICT for stale --if-match, got nil")
	}
	var ce *output.CodedError
	if !errors.As(execErr, &ce) {
		t.Fatalf("error = %T, want *output.CodedError", execErr)
	}
	if output.ExitCode(ce.Code) != output.ExitConflict {
		t.Errorf("stale --if-match exit = %d, want %d (conflict)", output.ExitCode(ce.Code), output.ExitConflict)
	}

	var envelope struct {
		Error struct {
			Code        string `json:"code"`
			Message     string `json:"message"`
			CurrentEtag string `json:"currentEtag"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("conflict output is not valid JSON: %v\n%s", err, out)
	}
	if envelope.Error.Code != output.ErrConflict {
		t.Errorf("error code = %q, want %q", envelope.Error.Code, output.ErrConflict)
	}
	if envelope.Error.CurrentEtag == "" {
		t.Errorf("conflict envelope missing currentEtag: %s", out)
	}
	if envelope.Error.CurrentEtag == staleEtag {
		t.Errorf("currentEtag should differ from the stale token, both were %q", staleEtag)
	}

	// The nib must be unchanged by the rejected write (still in-progress).
	data, _ := os.ReadFile(filepath.Join(nibsDir, "cnf-1--test.md"))
	if !strings.Contains(string(data), "status: in-progress") {
		t.Errorf("rejected set must not have mutated the nib, got:\n%s", data)
	}
}

func TestSetAfterWithStatusRepositionsAndUpdates(t *testing.T) {
	t.Cleanup(func() { resetSetFlags() })
	resetSetFlags()

	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create 3 sibling nibs with known order keys: a0, c0, e0
	nibs := []struct {
		file, content string
	}{
		{"aaa--first.md", "---\ntitle: First\nstatus: todo\ntype: task\norder: a0\n---\n"},
		{"bbb--second.md", "---\ntitle: Second\nstatus: todo\ntype: task\norder: c0\n---\n"},
		{"ccc--third.md", "---\ntitle: Third\nstatus: todo\ntype: task\norder: e0\n---\n"},
	}
	for _, n := range nibs {
		if err := os.WriteFile(filepath.Join(nibsDir, n.file), []byte(n.content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// set ccc --after aaa --status in-progress
	// Should reposition "ccc" after "aaa" (between a0 and c0) AND update status
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"set", "ccc",
		"--after", "aaa",
		"--status", "in-progress",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("set --after --status should succeed, got: %v", err)
	}

	// Read back the nib file and check order changed and status updated
	data, err := os.ReadFile(filepath.Join(nibsDir, "ccc--third.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	// Order should be between a0 and c0 (not a0, not c0, not e0)
	if strings.Contains(content, "order: e0") {
		t.Error("order should have changed from e0")
	}
	if !strings.Contains(content, "order: ") {
		t.Error("nib should have an order key after repositioning")
	}

	// Verify order key is lexicographically between a0 and c0
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "order: ") {
			orderKey := strings.TrimPrefix(line, "order: ")
			if orderKey <= "a0" || orderKey >= "c0" {
				t.Errorf("order key %q should be between 'a0' and 'c0'", orderKey)
			}
			break
		}
	}

	// Status should be updated
	if !strings.Contains(content, "status: in-progress") {
		t.Errorf("status should be in-progress, got:\n%s", content)
	}
}

func TestSetBeforeRepositionsBeforeSibling(t *testing.T) {
	t.Cleanup(func() { resetSetFlags() })
	resetSetFlags()

	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create 3 sibling nibs with known order keys: a0, c0, e0
	nibs := []struct {
		file, content string
	}{
		{"aaa--first.md", "---\ntitle: First\nstatus: todo\ntype: task\norder: a0\n---\n"},
		{"bbb--second.md", "---\ntitle: Second\nstatus: todo\ntype: task\norder: c0\n---\n"},
		{"ccc--third.md", "---\ntitle: Third\nstatus: todo\ntype: task\norder: e0\n---\n"},
	}
	for _, n := range nibs {
		if err := os.WriteFile(filepath.Join(nibsDir, n.file), []byte(n.content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// set aaa --before ccc → aaa should be repositioned before ccc (between c0 and e0)
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"set", "aaa",
		"--before", "ccc",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("set --before should succeed, got: %v", err)
	}

	// Read back the nib file and verify order changed
	data, err := os.ReadFile(filepath.Join(nibsDir, "aaa--first.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	// Order should have changed from a0 to something between c0 and e0
	if strings.Contains(content, "order: a0") {
		t.Error("order should have changed from a0")
	}
	if !strings.Contains(content, "order: ") {
		t.Error("nib should have an order key after repositioning")
	}

	// Verify order key is lexicographically between c0 and e0
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "order: ") {
			orderKey := strings.TrimPrefix(line, "order: ")
			if orderKey <= "c0" || orderKey >= "e0" {
				t.Errorf("order key %q should be between 'c0' and 'e0'", orderKey)
			}
			break
		}
	}
}

func TestSetFirstRepositionsToFirstPosition(t *testing.T) {
	t.Cleanup(func() { resetSetFlags() })
	resetSetFlags()

	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create 3 sibling nibs with known order keys: a0, c0, e0
	nibs := []struct {
		file, content string
	}{
		{"aaa--first.md", "---\ntitle: First\nstatus: todo\ntype: task\norder: a0\n---\n"},
		{"bbb--second.md", "---\ntitle: Second\nstatus: todo\ntype: task\norder: c0\n---\n"},
		{"ccc--third.md", "---\ntitle: Third\nstatus: todo\ntype: task\norder: e0\n---\n"},
	}
	for _, n := range nibs {
		if err := os.WriteFile(filepath.Join(nibsDir, n.file), []byte(n.content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// set ccc --first → ccc should be repositioned to first (before a0)
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"set", "ccc",
		"--first",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("set --first should succeed, got: %v", err)
	}

	// Read back the nib file and verify order is before a0
	data, err := os.ReadFile(filepath.Join(nibsDir, "ccc--third.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	// Order should have changed from e0 to something before a0
	if strings.Contains(content, "order: e0") {
		t.Error("order should have changed from e0")
	}
	if !strings.Contains(content, "order: ") {
		t.Error("nib should have an order key after repositioning")
	}

	// The new order key should sort before a0
	// Extract order value and verify it's lexicographically less than "a0"
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "order: ") {
			orderKey := strings.TrimPrefix(line, "order: ")
			if orderKey >= "a0" {
				t.Errorf("order key %q should sort before 'a0'", orderKey)
			}
			break
		}
	}
}

func TestSetIfMatchWithFieldsAndPositioning(t *testing.T) {
	t.Cleanup(func() { resetSetFlags() })
	resetSetFlags()

	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create 3 sibling nibs with known order keys: a0, c0, e0
	// Use version: 1 and # id comment to match Render() output format exactly,
	// so the etag computed from the file matches what the core will have.
	nibs := []struct {
		file, content string
	}{
		{"aaa--first.md", "---\n# aaa\nversion: 1\ntitle: First\nstatus: todo\ntype: task\norder: a0\n---\n\n"},
		{"bbb--second.md", "---\n# bbb\nversion: 1\ntitle: Second\nstatus: todo\ntype: task\norder: c0\n---\n\n"},
		{"ccc--third.md", "---\n# ccc\nversion: 1\ntitle: Third\nstatus: todo\ntype: task\norder: e0\n---\n\n"},
	}
	for _, n := range nibs {
		if err := os.WriteFile(filepath.Join(nibsDir, n.file), []byte(n.content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Compute the etag of ccc before modification
	f, err := os.Open(filepath.Join(nibsDir, "ccc--third.md"))
	if err != nil {
		t.Fatal(err)
	}
	cccNib, err := nib.Parse(f)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	cccNib.ID = "ccc"
	etag := cccNib.ETag()

	// Combine --if-match with both field update (--status) and positioning (--after)
	// This should succeed: after UpdateNib changes the etag, the code should
	// refresh ifMatch before calling ReorderNib.
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"set", "ccc",
		"--if-match", etag,
		"--after", "aaa",
		"--status", "in-progress",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("set --if-match with --status and --after should succeed, got: %v", err)
	}

	// Verify both changes were applied
	data, err := os.ReadFile(filepath.Join(nibsDir, "ccc--third.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, "status: in-progress") {
		t.Errorf("status should be in-progress, got:\n%s", content)
	}
	if strings.Contains(content, "order: e0") {
		t.Error("order should have changed from e0")
	}
}

func TestSetPositionOnlyWithoutMetadataFlags(t *testing.T) {
	t.Cleanup(func() { resetSetFlags() })
	resetSetFlags()

	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatal(err)
	}

	nibs := []struct {
		file, content string
	}{
		{"aaa--first.md", "---\ntitle: First\nstatus: todo\ntype: task\norder: a0\n---\n"},
		{"bbb--second.md", "---\ntitle: Second\nstatus: todo\ntype: task\norder: c0\n---\n"},
	}
	for _, n := range nibs {
		if err := os.WriteFile(filepath.Join(nibsDir, n.file), []byte(n.content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Positioning-only set: no --status, --type, etc.
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"set", "bbb",
		"--after", "aaa",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("positioning-only set should succeed without metadata flags, got: %v", err)
	}

	// Verify the nib was repositioned (order changed from c0)
	data, err := os.ReadFile(filepath.Join(nibsDir, "bbb--second.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	// Status should remain unchanged
	if !strings.Contains(content, "status: todo") {
		t.Errorf("status should remain todo, got:\n%s", content)
	}

	// Order should have changed from c0 to something after a0
	if strings.Contains(content, "order: c0") {
		t.Error("order should have changed from c0")
	}
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "order: ") {
			orderKey := strings.TrimPrefix(line, "order: ")
			if orderKey <= "a0" || orderKey >= "c0" {
				t.Errorf("order key %q should be between 'a0' and 'c0'", orderKey)
			}
			break
		}
	}
}

func TestSetPositionFlagsMutuallyExclusive(t *testing.T) {
	t.Cleanup(func() { resetSetFlags() })
	resetSetFlags()

	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatal(err)
	}
	nibContent := "---\ntitle: Test\nstatus: todo\ntype: task\norder: a0\n---\n"
	if err := os.WriteFile(filepath.Join(nibsDir, "mut-1--test.md"), []byte(nibContent), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		args []string
	}{
		{"after+before", []string{"--nibs-path", nibsDir, "set", "mut-1", "--after", "x", "--before", "y"}},
		{"after+first", []string{"--nibs-path", nibsDir, "set", "mut-1", "--after", "x", "--first"}},
		{"before+first", []string{"--nibs-path", nibsDir, "set", "mut-1", "--before", "x", "--first"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resetSetFlags()
			rootCmd.SetArgs(tc.args)
			err := rootCmd.Execute()
			if err == nil {
				t.Fatal("expected error for mutually exclusive positioning flags")
			}
			if !strings.Contains(err.Error(), "cannot be set together") &&
				!strings.Contains(err.Error(), "if any flags in the group") {
				t.Errorf("expected mutual exclusion error, got: %s", err)
			}
		})
	}
}
