package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/output"
)

// writeBodyFile writes content to a temp file and returns the "@path" token
// used by body/prose flags (which no longer accept inline text).
func writeBodyFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing body file: %v", err)
	}
	return "@" + path
}

func resetNewFlags() {
	newStatus = ""
	newType = ""
	newPriority = ""
	newBody = ""
	newBodyFile = ""
	newTag = nil
	newParent = ""
	newArea = ""
	newBlocking = nil
	newBlockedBy = nil
	newDocument = nil
	newEstimate = ""
	newPrefix = ""
	newAfter = ""
	newBefore = ""
	newFirst = false
	newJSON = false
	newNoEdit = false
	newNoDedupCheck = false
}

func TestResetNewFlagsClearsAllState(t *testing.T) {
	newStatus = "dirty"
	newType = "dirty"
	newPriority = "dirty"
	newBody = "dirty"
	newBodyFile = "dirty"
	newTag = []string{"t"}
	newParent = "dirty"
	newArea = "dirty"
	newBlocking = []string{"x"}
	newBlockedBy = []string{"y"}
	newDocument = []string{"d"}
	newEstimate = "dirty"
	newPrefix = "dirty"
	newAfter = "dirty"
	newBefore = "dirty"
	newFirst = true
	newJSON = true
	newNoEdit = true
	newNoDedupCheck = true

	resetNewFlags()

	if newStatus != "" {
		t.Errorf("newStatus not reset: %q", newStatus)
	}
	if newType != "" {
		t.Errorf("newType not reset: %q", newType)
	}
	if newPriority != "" {
		t.Errorf("newPriority not reset: %q", newPriority)
	}
	if newBody != "" {
		t.Errorf("newBody not reset: %q", newBody)
	}
	if newBodyFile != "" {
		t.Errorf("newBodyFile not reset: %q", newBodyFile)
	}
	if newTag != nil {
		t.Errorf("newTag not reset: %v", newTag)
	}
	if newParent != "" {
		t.Errorf("newParent not reset: %q", newParent)
	}
	if newArea != "" {
		t.Errorf("newArea not reset: %q", newArea)
	}
	if newBlocking != nil {
		t.Errorf("newBlocking not reset: %v", newBlocking)
	}
	if newBlockedBy != nil {
		t.Errorf("newBlockedBy not reset: %v", newBlockedBy)
	}
	if newDocument != nil {
		t.Errorf("newDocument not reset: %v", newDocument)
	}
	if newEstimate != "" {
		t.Errorf("newEstimate not reset: %q", newEstimate)
	}
	if newPrefix != "" {
		t.Errorf("newPrefix not reset: %q", newPrefix)
	}
	if newAfter != "" {
		t.Errorf("newAfter not reset: %q", newAfter)
	}
	if newBefore != "" {
		t.Errorf("newBefore not reset: %q", newBefore)
	}
	if newFirst {
		t.Error("newFirst not reset")
	}
	if newJSON {
		t.Error("newJSON not reset")
	}
	if newNoEdit {
		t.Error("newNoEdit not reset")
	}
	if newNoDedupCheck {
		t.Error("newNoDedupCheck not reset")
	}
}

func setupNewTest(t *testing.T) string {
	t.Helper()
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(func() { resetNewFlags() })
	resetNewFlags()

	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(storeDataDir(nibsDir), 0755); err != nil {
		t.Fatal(err)
	}
	return nibsDir
}

// writeEditorScript creates a platform-appropriate script that appends a line
// to the file it edits. Returns the path to the script.
func writeEditorScript(t *testing.T, dir string) string {
	t.Helper()
	var scriptPath, script string
	if runtime.GOOS == "windows" {
		script = "@echo off\r\necho EDITED BY SCRIPT>> %1\r\n"
		scriptPath = filepath.Join(dir, "test-editor.cmd")
	} else {
		script = "#!/bin/sh\necho 'EDITED BY SCRIPT' >> \"$1\"\n"
		scriptPath = filepath.Join(dir, "test-editor.sh")
	}
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return scriptPath
}

// forceInteractive overrides the isInteractiveTerminal gate for a test and
// restores it afterward. Tests run serially (they mutate global rootCmd
// state), so a package-level override with cleanup is safe.
func forceInteractive(t *testing.T, interactive bool) {
	t.Helper()
	orig := isInteractiveTerminal
	isInteractiveTerminal = func() bool { return interactive }
	t.Cleanup(func() { isInteractiveTerminal = orig })
}

func TestNewEditorOpensWithTemplate(t *testing.T) {
	nibsDir := setupNewTest(t)
	forceInteractive(t, true)
	scriptPath := writeEditorScript(t, filepath.Dir(nibsDir))

	// Set EDITOR to our test script
	t.Setenv("EDITOR", scriptPath)
	t.Setenv("VISUAL", "")

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"new", "Editor Test", "-t", "task",
	})

	out := captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("new failed: %v", err)
		}
	})
	_ = out

	// Find the created nib file
	entries, err := os.ReadDir(storeDataDir(nibsDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("no nib file created")
	}

	content, err := os.ReadFile(dataPath(nibsDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	body := string(content)

	// Should contain the template (task has "## Description")
	if !strings.Contains(body, "## Description") {
		t.Errorf("expected task template in body, got:\n%s", body)
	}
	// Should contain the text appended by our editor script
	if !strings.Contains(body, "EDITED BY SCRIPT") {
		t.Errorf("expected editor modifications in body, got:\n%s", body)
	}
}

func TestNewFallsBackToTemplateWithoutEditor(t *testing.T) {
	nibsDir := setupNewTest(t)

	// Ensure no editor is set
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"new", "No Editor Test", "-t", "task", "--json",
	})

	out := captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("new failed: %v", err)
		}
	})
	_ = out

	entries, _ := os.ReadDir(storeDataDir(nibsDir))
	content, _ := os.ReadFile(dataPath(nibsDir, entries[0].Name()))
	body := string(content)

	// Should contain the template
	if !strings.Contains(body, "## Description") {
		t.Errorf("expected task template in body, got:\n%s", body)
	}
	// Should NOT contain editor modifications (no editor ran)
	if strings.Contains(body, "EDITED BY SCRIPT") {
		t.Errorf("editor should not have been invoked without EDITOR set, got:\n%s", body)
	}
}

func TestNewBodyFlagSkipsEditor(t *testing.T) {
	nibsDir := setupNewTest(t)
	scriptPath := writeEditorScript(t, filepath.Dir(nibsDir))

	t.Setenv("EDITOR", scriptPath)
	t.Setenv("VISUAL", "")

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"new", "Body Flag Test", "-t", "task", "-d", writeBodyFile(t, "Custom body content"), "--json",
	})

	out := captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("new failed: %v", err)
		}
	})
	_ = out

	entries, _ := os.ReadDir(storeDataDir(nibsDir))
	content, _ := os.ReadFile(dataPath(nibsDir, entries[0].Name()))
	body := string(content)

	// Should use the provided body, not the template
	if !strings.Contains(body, "Custom body content") {
		t.Errorf("expected custom body, got:\n%s", body)
	}
	// Editor should NOT have been invoked
	if strings.Contains(body, "EDITED BY SCRIPT") {
		t.Errorf("editor should not have been invoked when --body is provided, got:\n%s", body)
	}
	// Template should NOT be present
	if strings.Contains(body, "## Description") {
		t.Errorf("template should not be used when --body is provided, got:\n%s", body)
	}
}

// withStdin temporarily replaces os.Stdin with a pipe carrying content, so
// tests can exercise the "-" input channel end-to-end.
func withStdin(t *testing.T, content string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	go func() {
		_, _ = w.WriteString(content)
		_ = w.Close()
	}()
	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = orig
		_ = r.Close()
	})
}

// TestNewBodyFromStdin verifies that `--body -` reads the full body from
// stdin verbatim — backticks, quotes and newlines survive intact (the whole
// point of the input channel: no fragile shell escaping).
func TestNewBodyFromStdin(t *testing.T) {
	nibsDir := setupNewTest(t)
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")

	body := "## Section\n`code` with \"quotes\" and 'apostrophes'\nmultiple\nlines\n"
	withStdin(t, body)

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"new", "Stdin Body", "-t", "task", "-d", "-", "--json",
	})
	out := captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("new failed: %v", err)
		}
	})
	_ = out

	entries, _ := os.ReadDir(storeDataDir(nibsDir))
	content, _ := os.ReadFile(dataPath(nibsDir, entries[0].Name()))
	if !strings.Contains(string(content), "`code` with \"quotes\" and 'apostrophes'") {
		t.Errorf("piped body not preserved verbatim, got:\n%s", content)
	}
	if !strings.Contains(string(content), "multiple\nlines") {
		t.Errorf("piped multi-line body not preserved, got:\n%s", content)
	}
}

// TestNewBodyFromFile verifies that `--body @FILE` reads the file content.
func TestNewBodyFromFile(t *testing.T) {
	nibsDir := setupNewTest(t)
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"new", "File Body", "-t", "task", "-d", writeBodyFile(t, "content from a file\n"), "--json",
	})
	out := captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("new failed: %v", err)
		}
	})
	_ = out

	entries, _ := os.ReadDir(storeDataDir(nibsDir))
	content, _ := os.ReadFile(dataPath(nibsDir, entries[0].Name()))
	if !strings.Contains(string(content), "content from a file") {
		t.Errorf("file body not used, got:\n%s", content)
	}
}

// TestNewRejectsInlineBody verifies the inline `--body "<string>"` form is
// gone: a bare value is rejected with a validation error and nothing is created.
func TestNewRejectsInlineBody(t *testing.T) {
	nibsDir := setupNewTest(t)
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"new", "Inline Body", "-t", "task", "-d", "some inline body text", "--json",
	})
	var err error
	_ = captureStdout(t, func() { err = rootCmd.Execute() })
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
	entries, _ := os.ReadDir(storeDataDir(nibsDir))
	if len(entries) != 0 {
		t.Errorf("expected no nib created on rejection, found %d entries", len(entries))
	}
}

// TestNewMissingBodyFileIsIOError verifies a missing `@FILE` maps to the
// I/O exit code (5), not a validation error.
func TestNewMissingBodyFileIsIOError(t *testing.T) {
	nibsDir := setupNewTest(t)
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"new", "Missing File", "-t", "task", "-d", "@/no/such/file-xyz.md", "--json",
	})
	var err error
	_ = captureStdout(t, func() { err = rootCmd.Execute() })
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

// readNibByID scans a nibs directory and returns the nib whose filename parses
// to exactly the given ID. Used by the positioning tests below.
func readNibByID(t *testing.T, nibsDir, idPrefix string) *nib.Nib {
	t.Helper()
	entries, err := os.ReadDir(storeDataDir(nibsDir))
	if err != nil {
		t.Fatalf("reading nibs dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		id, _ := nib.ParseFilename(e.Name(), "")
		if id != idPrefix {
			continue
		}
		f, err := os.Open(dataPath(nibsDir, e.Name()))
		if err != nil {
			t.Fatalf("opening %s: %v", e.Name(), err)
		}
		b, err := nib.Parse(f)
		_ = f.Close()
		if err != nil {
			t.Fatalf("parsing %s: %v", e.Name(), err)
		}
		b.ID = id
		return b
	}
	t.Fatalf("no nib file found with id %q in %s", idPrefix, nibsDir)
	return nil
}

// firstCreatedID returns the ID of the (single) nib file in the directory.
// Useful when the just-created nib's ID was generated and is not known up-front.
func firstCreatedID(t *testing.T, nibsDir string) string {
	t.Helper()
	entries, err := os.ReadDir(storeDataDir(nibsDir))
	if err != nil {
		t.Fatalf("reading nibs dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		id, _ := nib.ParseFilename(e.Name(), "")
		if id != "" {
			return id
		}
	}
	t.Fatal("no nib files found")
	return ""
}

// countNibFiles returns how many nib files (non-directory, parseable id) live
// directly in nibsDir.
func countNibFiles(t *testing.T, nibsDir string) int {
	t.Helper()
	entries, err := os.ReadDir(storeDataDir(nibsDir))
	if err != nil {
		t.Fatalf("reading nibs dir: %v", err)
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if id, _ := nib.ParseFilename(e.Name(), ""); id != "" {
			n++
		}
	}
	return n
}

// TestNewAfterRootSibling verifies that `nibs new --after <root-id>`
// successfully positions the new nib between the anchor and the next root,
// not merely appended at the end. Three roots are needed to distinguish
// "insert between" from "always append last". Regression test.
func TestNewAfterRootSibling(t *testing.T) {
	nibsDir := setupNewTest(t)
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")

	// Create root A.
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"new", "Root A", "-t", "bug", "-d", writeBodyFile(t, "body-a"), "--json",
	})
	_ = captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("new A failed: %v", err)
		}
	})
	idA := firstCreatedID(t, nibsDir)
	resetNewFlags()

	// Create root B.
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"new", "Root B", "-t", "bug", "-d", writeBodyFile(t, "body-b"), "--json",
	})
	_ = captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("new B failed: %v", err)
		}
	})
	// Find B by elimination (the new id that isn't A).
	idB := otherID(t, nibsDir, map[string]bool{idA: true})
	resetNewFlags()

	// Create root C with --after A. C should land between A and B,
	// NOT after B.
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"new", "Root C", "-t", "bug", "-d", writeBodyFile(t, "body-c"),
		"--after", idA, "--json",
	})
	_ = captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("new C with --after %q failed: %v", idA, err)
		}
	})
	idC := otherID(t, nibsDir, map[string]bool{idA: true, idB: true})

	a := readNibByID(t, nibsDir, idA)
	b := readNibByID(t, nibsDir, idB)
	c := readNibByID(t, nibsDir, idC)

	if c.Parent != "" {
		t.Errorf("new nib should be root, got parent %q", c.Parent)
	}
	if a.Order == "" || b.Order == "" {
		t.Fatalf("anchors missing order keys (a=%q b=%q)", a.Order, b.Order)
	}
	// C must sort after A.
	if c.Order <= a.Order {
		t.Errorf("new order %q should sort after A %q", c.Order, a.Order)
	}
	// C must sort BEFORE B — this is what distinguishes "insert between"
	// from a regression to "always append last".
	if c.Order >= b.Order {
		t.Errorf("new order %q should sort before B %q (would indicate regression to always-append)", c.Order, b.Order)
	}
}

// otherID returns the ID of the nib in the directory whose ID is not in the
// given excluded set. Fails the test if zero or more than one such nib exists.
func otherID(t *testing.T, nibsDir string, excluded map[string]bool) string {
	t.Helper()
	entries, err := os.ReadDir(storeDataDir(nibsDir))
	if err != nil {
		t.Fatalf("reading nibs dir: %v", err)
	}
	var found string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		id, _ := nib.ParseFilename(e.Name(), "")
		if id == "" || excluded[id] {
			continue
		}
		if found != "" {
			t.Fatalf("expected exactly one new nib in %s, found multiple (%q and %q)", nibsDir, found, id)
		}
		found = id
	}
	if found == "" {
		t.Fatalf("no new nib found in %s (excluded=%v)", nibsDir, excluded)
	}
	return found
}

func TestNewVisualTakesPrecedence(t *testing.T) {
	nibsDir := setupNewTest(t)
	forceInteractive(t, true)
	scriptPath := writeEditorScript(t, filepath.Dir(nibsDir))

	// Set VISUAL to working script, EDITOR to a nonexistent command
	t.Setenv("VISUAL", scriptPath)
	t.Setenv("EDITOR", "nonexistent-editor-that-should-not-be-called")

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"new", "Visual Test", "-t", "task",
	})

	out := captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("new failed (VISUAL should have been used, not EDITOR): %v", err)
		}
	})
	_ = out

	entries, _ := os.ReadDir(storeDataDir(nibsDir))
	content, _ := os.ReadFile(dataPath(nibsDir, entries[0].Name()))
	body := string(content)

	if !strings.Contains(body, "EDITED BY SCRIPT") {
		t.Errorf("expected VISUAL editor to be used, got:\n%s", body)
	}
}

// TestNewJSONNeverOpensEditor isolates the --json guard: with interactive forced
// true and EDITOR set, only the newJSON check keeps the editor from running.
// It also pins that --json emits clean, parseable JSON in a machine context.
func TestNewJSONNeverOpensEditor(t *testing.T) {
	nibsDir := setupNewTest(t)
	forceInteractive(t, true)
	scriptPath := writeEditorScript(t, filepath.Dir(nibsDir))

	t.Setenv("EDITOR", scriptPath)
	t.Setenv("VISUAL", "")

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"new", "JSON No Editor", "-t", "task", "--json",
	})

	out := captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("new --json failed: %v", err)
		}
	})

	entries, _ := os.ReadDir(storeDataDir(nibsDir))
	content, _ := os.ReadFile(dataPath(nibsDir, entries[0].Name()))
	body := string(content)

	if !strings.Contains(body, "## Description") {
		t.Errorf("expected task template in body, got:\n%s", body)
	}
	if strings.Contains(body, "EDITED BY SCRIPT") {
		t.Errorf("editor must not run in --json mode, got:\n%s", body)
	}

	// The captured stdout must be clean, parseable JSON carrying a nib object.
	var got struct {
		Nib json.RawMessage `json:"nib"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("--json output is not valid JSON: %v\n%s", err, out)
	}
	if len(got.Nib) == 0 {
		t.Errorf("expected a {\"nib\": {...}} object in --json output, got:\n%s", out)
	}
}

// TestNewNonTTYFallsBackToTemplate isolates the TTY guard: no --json, EDITOR set,
// so only isInteractiveTerminal()==false keeps the editor from running. This is the
// agent/subagent/pipe case that previously errored on /dev/tty.
func TestNewNonTTYFallsBackToTemplate(t *testing.T) {
	nibsDir := setupNewTest(t)
	forceInteractive(t, false)
	scriptPath := writeEditorScript(t, filepath.Dir(nibsDir))

	t.Setenv("EDITOR", scriptPath)
	t.Setenv("VISUAL", "")

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"new", "Non TTY", "-t", "task",
	})

	_ = captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("new failed: %v", err)
		}
	})

	entries, _ := os.ReadDir(storeDataDir(nibsDir))
	content, _ := os.ReadFile(dataPath(nibsDir, entries[0].Name()))
	body := string(content)

	if !strings.Contains(body, "## Description") {
		t.Errorf("expected task template in body, got:\n%s", body)
	}
	if strings.Contains(body, "EDITED BY SCRIPT") {
		t.Errorf("editor must not run without a controlling terminal, got:\n%s", body)
	}
}

// TestNewNoEditFlagSkipsEditor isolates the --no-edit guard: interactive forced
// true and EDITOR set, so only newNoEdit keeps the editor from running.
func TestNewNoEditFlagSkipsEditor(t *testing.T) {
	nibsDir := setupNewTest(t)
	forceInteractive(t, true)
	scriptPath := writeEditorScript(t, filepath.Dir(nibsDir))

	t.Setenv("EDITOR", scriptPath)
	t.Setenv("VISUAL", "")

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"new", "No Edit Flag", "-t", "task", "--no-edit",
	})

	_ = captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("new --no-edit failed: %v", err)
		}
	})

	entries, _ := os.ReadDir(storeDataDir(nibsDir))
	content, _ := os.ReadFile(dataPath(nibsDir, entries[0].Name()))
	body := string(content)

	if !strings.Contains(body, "## Description") {
		t.Errorf("expected task template in body, got:\n%s", body)
	}
	if strings.Contains(body, "EDITED BY SCRIPT") {
		t.Errorf("editor must not run with --no-edit, got:\n%s", body)
	}
}

// TestNewCommandNameAndAliases pins the command surface: the primary command is
// `new`, with `create` and `c` reachable as aliases.
func TestNewCommandNameAndAliases(t *testing.T) {
	if got := newCmd.Name(); got != "new" {
		t.Errorf("command name = %q, want %q", got, "new")
	}
	want := map[string]bool{"create": true, "c": true}
	for _, a := range newCmd.Aliases {
		delete(want, a)
	}
	if len(want) != 0 {
		t.Errorf("missing aliases %v; have %v", want, newCmd.Aliases)
	}
}

// TestCreateAliasStillCreates verifies the `create` alias creates a nib.
func TestCreateAliasStillCreates(t *testing.T) {
	nibsDir := setupNewTest(t)
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"create", "Via Alias", "-t", "task", "-d", writeBodyFile(t, "aliased body"),
	})
	_ = captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("create alias failed: %v", err)
		}
	})
	if countNibFiles(t, nibsDir) != 1 {
		t.Errorf("expected exactly one nib created via the create alias, found %d", countNibFiles(t, nibsDir))
	}
}

// TestNewRejectsExtraArgs verifies the title is a single positional: a second
// arg is rejected and no nib is created. Before MaximumNArgs(1), extra args
// were silently folded into the title via strings.Join, so `new First Second`
// created a nib titled "First Second" and returned nil — this guard bites
// against that.
func TestNewRejectsExtraArgs(t *testing.T) {
	nibsDir := setupNewTest(t)
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"new", "First", "Second", "-t", "task",
	})
	var err error
	_ = captureStdout(t, func() { err = rootCmd.Execute() })
	if err == nil {
		t.Fatal("expected `new` to reject a second positional arg, got nil")
	}
	// Pin the failure to the arity guard specifically — not some unrelated error
	// (a config load, an editor spawn) that would also leave err != nil and no
	// nib created, letting a broken codedMaximumNArgs(1) slip through green.
	if !strings.Contains(err.Error(), "argument(s)") {
		t.Errorf("expected an arg-count error, got: %v", err)
	}
	// The coded validator routes arg-count errors through the same VALIDATION
	// path as value errors (exit 2), not cobra's plain-text exit 1.
	var ce *output.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("error = %T, want *output.CodedError", err)
	}
	if output.ExitCode(ce.Code) != output.ExitValidation {
		t.Errorf("exit = %d, want %d (validation)", output.ExitCode(ce.Code), output.ExitValidation)
	}
	if n := countNibFiles(t, nibsDir); n != 0 {
		t.Errorf("expected no nib created when extra args are rejected, found %d", n)
	}
}

// TestNewEchoesCardText verifies the text echo is a lean card (key: value
// lines from the card projection) — never the full body.
func TestNewEchoesCardText(t *testing.T) {
	nibsDir := setupNewTest(t)
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"new", "Card Echo", "-t", "task", "-p", "high",
		"-d", writeBodyFile(t, "## Body\nSECRET-BODY-MARKER\n"),
	})
	out := captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("new failed: %v", err)
		}
	})

	for _, want := range []string{"id: ", "title: Card Echo", "status: ", "type: task", "priority: high"} {
		if !strings.Contains(out, want) {
			t.Errorf("card echo missing %q; got:\n%s", want, out)
		}
	}
	// The full body must NOT be echoed — the card is lean.
	if strings.Contains(out, "SECRET-BODY-MARKER") {
		t.Errorf("card echo leaked the body; got:\n%s", out)
	}
	if strings.Contains(out, "body:") {
		t.Errorf("card echo should not include a body field; got:\n%s", out)
	}
}

// TestNewEchoesCardJSON verifies --json emits the {nib} single-read contract
// with the card field set — no success wrapper, no body/etag.
func TestNewEchoesCardJSON(t *testing.T) {
	nibsDir := setupNewTest(t)
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"new", "JSON Card", "-t", "task",
		"-d", writeBodyFile(t, "## Body\nSECRET-BODY-MARKER\n"), "--json",
	})
	out := captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("new --json failed: %v", err)
		}
	})

	var got struct {
		Nib map[string]any `json:"nib"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if got.Nib == nil {
		t.Fatalf("expected {\"nib\": {...}} contract, got:\n%s", out)
	}
	if got.Nib["title"] != "JSON Card" {
		t.Errorf("nib.title = %v, want %q", got.Nib["title"], "JSON Card")
	}
	if _, ok := got.Nib["id"]; !ok {
		t.Errorf("card nib missing id; got:\n%s", out)
	}
	// body and etag are opt-in — a card echo must omit them.
	if _, ok := got.Nib["body"]; ok {
		t.Errorf("card echo JSON leaked body; got:\n%s", out)
	}
	if _, ok := got.Nib["etag"]; ok {
		t.Errorf("card echo JSON leaked etag; got:\n%s", out)
	}
	// No success/data wrapper — the {nib} single-read contract.
	if strings.Contains(out, "\"success\"") {
		t.Errorf("card echo JSON should not carry a success wrapper; got:\n%s", out)
	}
	if strings.Contains(out, "SECRET-BODY-MARKER") {
		t.Errorf("card echo JSON leaked body content; got:\n%s", out)
	}
}

// newRootNib creates a parentless nib of the given type (always legal) and
// returns its id, swallowing the card echo. A -d body is supplied so no editor
// is invoked for types with a body template.
func newRootNib(t *testing.T, nibsDir, title, nibType string, jsonMode bool) string {
	t.Helper()
	args := []string{
		"--nibs-path", nibsDir, "new", title, "-t", nibType,
		"-d", writeBodyFile(t, "seed body\n"),
	}
	if jsonMode {
		args = append(args, "--json")
	}
	rootCmd.SetArgs(args)
	_ = captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("create root %s failed: %v", nibType, err)
		}
	})
	id := firstCreatedID(t, nibsDir)
	resetNewFlags()
	return id
}

// TestNewHierarchyErrorText verifies an illegal parent type (a feature under a
// task — a task is not a legal parent for a feature) surfaces a HIERARCHY-coded
// error (exit 2) whose message names the allowed parent types, and creates no
// extra nib.
func TestNewHierarchyErrorText(t *testing.T) {
	nibsDir := setupNewTest(t)
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")

	taskID := newRootNib(t, nibsDir, "Root Task", "task", false)

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"new", "Bad Feature", "-t", "feature", "--parent", taskID,
	})
	var err error
	_ = captureStdout(t, func() { err = rootCmd.Execute() })
	if err == nil {
		t.Fatal("expected HIERARCHY error for a feature under a task, got nil")
	}
	var ce *output.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("error = %T, want *output.CodedError", err)
	}
	if ce.Code != output.ErrHierarchy {
		t.Errorf("code = %q, want %q", ce.Code, output.ErrHierarchy)
	}
	if output.ExitCode(ce.Code) != output.ExitValidation {
		t.Errorf("exit = %d, want %d (validation)", output.ExitCode(ce.Code), output.ExitValidation)
	}
	// The message names the allowed parent types for a feature.
	if !strings.Contains(ce.Error(), "parent of type epic") {
		t.Errorf("message %q should name the allowed parent types", ce.Error())
	}
	if n := countNibFiles(t, nibsDir); n != 1 {
		t.Errorf("expected only the root task to exist, found %d nibs", n)
	}
}

// TestNewHierarchyErrorJSON verifies --json surfaces the HIERARCHY error as the
// {error:{code,message,allowedParentTypes}} envelope, exit 2.
func TestNewHierarchyErrorJSON(t *testing.T) {
	nibsDir := setupNewTest(t)
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")

	taskID := newRootNib(t, nibsDir, "Root Task", "task", true)

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"new", "Bad Feature", "-t", "feature", "--parent", taskID, "--json",
	})
	var err error
	out := captureStdout(t, func() { err = rootCmd.Execute() })
	if err == nil {
		t.Fatal("expected HIERARCHY error, got nil")
	}

	var env struct {
		Error struct {
			Code               string   `json:"code"`
			Message            string   `json:"message"`
			AllowedParentTypes []string `json:"allowedParentTypes"`
		} `json:"error"`
	}
	if e := json.Unmarshal([]byte(out), &env); e != nil {
		t.Fatalf("error output is not valid JSON: %v\n%s", e, out)
	}
	if env.Error.Code != output.ErrHierarchy {
		t.Errorf("code = %q, want %q", env.Error.Code, output.ErrHierarchy)
	}
	if env.Error.Message == "" {
		t.Error("error envelope has empty message")
	}
	// A feature's only legal parent is an epic.
	want := []string{"epic"}
	if !reflect.DeepEqual(env.Error.AllowedParentTypes, want) {
		t.Errorf("allowedParentTypes = %v, want %v", env.Error.AllowedParentTypes, want)
	}
	// The returned error still carries the code for the exit boundary.
	var ce *output.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("error = %T, want *output.CodedError", err)
	}
	if output.ExitCode(ce.Code) != output.ExitValidation {
		t.Errorf("exit = %d, want %d (validation)", output.ExitCode(ce.Code), output.ExitValidation)
	}
}

// makeClosedNib creates a nib with the given closed status and body, returning
// its id. Used to seed the dedup-check integration tests. The dedup check runs
// on this creation too, but with no prior closed nibs it produces no matches.
func makeClosedNib(t *testing.T, nibsDir, title, status, body string) string {
	t.Helper()
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"new", title, "-t", "bug", "-s", status,
		"-d", writeBodyFile(t, body), "--json",
	})
	_ = captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("seeding %s nib %q failed: %v", status, title, err)
		}
	})
	id := firstCreatedID(t, nibsDir)
	resetNewFlags()
	return id
}

// newDupJSON runs `nibs new <title> --json` and returns the created id, the
// parsed possible_duplicates array (nil if absent), and asserts exit 0.
func newDupJSON(t *testing.T, nibsDir, title string, extraArgs ...string) (string, []possibleDuplicate) {
	t.Helper()
	args := []string{
		"--nibs-path", nibsDir,
		"new", title, "-t", "bug",
		"-d", writeBodyFile(t, "seed\n"), "--json",
	}
	args = append(args, extraArgs...)
	rootCmd.SetArgs(args)

	var execErr error
	out := captureStdout(t, func() { execErr = rootCmd.Execute() })
	if execErr != nil {
		t.Fatalf("new %q --json returned non-nil error (want exit 0): %v", title, execErr)
	}

	var got struct {
		Nib                map[string]any      `json:"nib"`
		PossibleDuplicates []possibleDuplicate `json:"possible_duplicates"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("new %q --json output not parseable: %v\n%s", title, err, out)
	}
	if got.Nib == nil {
		t.Fatalf("new %q --json missing {nib}; got:\n%s", title, out)
	}
	id, _ := got.Nib["id"].(string)
	return id, got.PossibleDuplicates
}

// TestNewWarnsOnScrappedDuplicateText verifies the human/text path: a new nib
// whose normalized title equals a scrapped nib triggers a warn-only stderr
// notice naming the scrapped nib's id, status, and reason snippet — while the
// create still succeeds (exit 0) and stdout carries the clean card.
func TestNewWarnsOnScrappedDuplicateText(t *testing.T) {
	nibsDir := setupNewTest(t)
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")

	scrapID := makeClosedNib(t, nibsDir, "Fix the login flow", "scrapped",
		"## Reasons for Scrapping\n- Too risky to implement right now\n")

	var errBuf bytes.Buffer
	rootCmd.SetErr(&errBuf)
	t.Cleanup(func() { rootCmd.SetErr(nil); rootCmd.SetOut(nil) })

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"new", "Fix the login flow", "-t", "bug",
		"-d", writeBodyFile(t, "seed\n"),
	})
	var execErr error
	out := captureStdout(t, func() { execErr = rootCmd.Execute() })
	if execErr != nil {
		t.Fatalf("new returned non-nil error (want exit 0): %v", execErr)
	}

	// Stdout must stay the clean card (no warning leaked onto it).
	if !strings.Contains(out, "title: Fix the login flow") {
		t.Errorf("stdout missing the card; got:\n%s", out)
	}
	if strings.Contains(out, "warning:") {
		t.Errorf("warning leaked onto stdout; got:\n%s", out)
	}

	// Stderr carries the warn-only notice with id, status, and reason.
	warn := errBuf.String()
	for _, want := range []string{scrapID, "scrapped", "Fix the login flow", "Too risky to implement right now"} {
		if !strings.Contains(warn, want) {
			t.Errorf("stderr warning missing %q; got:\n%s", want, warn)
		}
	}
}

// TestNewWarnsOnCompletedDuplicateJSON verifies a completed match is surfaced in
// --json via the possible_duplicates field (no reason for a completed match),
// the {nib} contract is preserved, and the exit code is 0.
func TestNewWarnsOnCompletedDuplicateJSON(t *testing.T) {
	nibsDir := setupNewTest(t)
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")

	doneID := makeClosedNib(t, nibsDir, "Add dark mode", "completed", "## Summary\nShipped.\n")

	_, dups := newDupJSON(t, nibsDir, "Add dark mode")
	if len(dups) != 1 {
		t.Fatalf("possible_duplicates = %#v, want exactly one completed match", dups)
	}
	if dups[0].ID != doneID {
		t.Errorf("match id = %q, want %q", dups[0].ID, doneID)
	}
	if dups[0].Status != "completed" {
		t.Errorf("match status = %q, want completed", dups[0].Status)
	}
	if dups[0].Reason != "" {
		t.Errorf("completed match should have no reason, got %q", dups[0].Reason)
	}
}

// TestNewDoesNotWarnOnOpenDuplicate pins the closed-only scope: an OPEN nib with
// an identical title is NOT surfaced. This is the guard that would fail if the
// finder scanned all statuses instead of just completed/scrapped.
func TestNewDoesNotWarnOnOpenDuplicate(t *testing.T) {
	nibsDir := setupNewTest(t)
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")

	// Seed an OPEN nib (todo) with a title the new nib will exactly match.
	makeClosedNib(t, nibsDir, "Refactor the parser", "todo", "## Description\nwork\n")

	_, dups := newDupJSON(t, nibsDir, "Refactor the parser")
	if len(dups) != 0 {
		t.Errorf("open same-title nib should not be surfaced; got %#v", dups)
	}
}

// TestNewNoMatchOmitsDuplicatesField verifies a no-match create emits no
// possible_duplicates key at all and exits 0.
func TestNewNoMatchOmitsDuplicatesField(t *testing.T) {
	nibsDir := setupNewTest(t)
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")

	makeClosedNib(t, nibsDir, "Fix the login flow", "scrapped",
		"## Reasons for Scrapping\n- nope\n")

	_, dups := newDupJSON(t, nibsDir, "A completely unrelated distinct heading")
	if len(dups) != 0 {
		t.Errorf("expected no matches for a distinct title; got %#v", dups)
	}

	// And the raw JSON must not carry the key at all (omitempty).
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"new", "Another unrelated distinct heading", "-t", "bug",
		"-d", writeBodyFile(t, "seed\n"), "--json",
	})
	out := captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("new failed: %v", err)
		}
	})
	if strings.Contains(out, "possible_duplicates") {
		t.Errorf("no-match --json should omit possible_duplicates key; got:\n%s", out)
	}
}

// TestNewNoDedupCheckSuppresses verifies --no-dedup-check skips the check even
// when a scrapped match exists: no possible_duplicates field is emitted.
func TestNewNoDedupCheckSuppresses(t *testing.T) {
	nibsDir := setupNewTest(t)
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")

	makeClosedNib(t, nibsDir, "Fix the login flow", "scrapped",
		"## Reasons for Scrapping\n- Too risky\n")

	_, dups := newDupJSON(t, nibsDir, "Fix the login flow", "--no-dedup-check")
	if len(dups) != 0 {
		t.Errorf("--no-dedup-check must suppress matches; got %#v", dups)
	}
}

// TestNewScrappedDuplicateJSONCarriesReason verifies the --json path surfaces a
// scrapped match WITH its reason snippet, exit 0.
func TestNewScrappedDuplicateJSONCarriesReason(t *testing.T) {
	nibsDir := setupNewTest(t)
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")

	scrapID := makeClosedNib(t, nibsDir, "Support offline mode", "scrapped",
		"## Reasons for Scrapping\n- Out of scope for this milestone\n")

	_, dups := newDupJSON(t, nibsDir, "Support offline mode")
	if len(dups) != 1 {
		t.Fatalf("possible_duplicates = %#v, want one scrapped match", dups)
	}
	if dups[0].ID != scrapID || dups[0].Status != "scrapped" {
		t.Errorf("match = %#v, want scrapped %q", dups[0], scrapID)
	}
	if dups[0].Reason != "Out of scope for this milestone" {
		t.Errorf("reason = %q, want the scrapping reason snippet", dups[0].Reason)
	}
}
