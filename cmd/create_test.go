package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func resetCreateFlags() {
	createStatus = ""
	createType = ""
	createPriority = ""
	createBody = ""
	createBodyFile = ""
	createTag = nil
	createParent = ""
	createBlocking = nil
	createBlockedBy = nil
	createDocument = nil
	createEstimate = ""
	createPrefix = ""
	createAfter = ""
	createBefore = ""
	createFirst = false
	createJSON = false
}

func TestResetCreateFlagsClearsAllState(t *testing.T) {
	createStatus = "dirty"
	createType = "dirty"
	createPriority = "dirty"
	createBody = "dirty"
	createBodyFile = "dirty"
	createTag = []string{"t"}
	createParent = "dirty"
	createBlocking = []string{"x"}
	createBlockedBy = []string{"y"}
	createDocument = []string{"d"}
	createEstimate = "dirty"
	createPrefix = "dirty"
	createAfter = "dirty"
	createBefore = "dirty"
	createFirst = true
	createJSON = true

	resetCreateFlags()

	if createStatus != "" {
		t.Errorf("createStatus not reset: %q", createStatus)
	}
	if createType != "" {
		t.Errorf("createType not reset: %q", createType)
	}
	if createPriority != "" {
		t.Errorf("createPriority not reset: %q", createPriority)
	}
	if createBody != "" {
		t.Errorf("createBody not reset: %q", createBody)
	}
	if createBodyFile != "" {
		t.Errorf("createBodyFile not reset: %q", createBodyFile)
	}
	if createTag != nil {
		t.Errorf("createTag not reset: %v", createTag)
	}
	if createParent != "" {
		t.Errorf("createParent not reset: %q", createParent)
	}
	if createBlocking != nil {
		t.Errorf("createBlocking not reset: %v", createBlocking)
	}
	if createBlockedBy != nil {
		t.Errorf("createBlockedBy not reset: %v", createBlockedBy)
	}
	if createDocument != nil {
		t.Errorf("createDocument not reset: %v", createDocument)
	}
	if createEstimate != "" {
		t.Errorf("createEstimate not reset: %q", createEstimate)
	}
	if createPrefix != "" {
		t.Errorf("createPrefix not reset: %q", createPrefix)
	}
	if createAfter != "" {
		t.Errorf("createAfter not reset: %q", createAfter)
	}
	if createBefore != "" {
		t.Errorf("createBefore not reset: %q", createBefore)
	}
	if createFirst {
		t.Error("createFirst not reset")
	}
	if createJSON {
		t.Error("createJSON not reset")
	}
}

func setupCreateTest(t *testing.T) string {
	t.Helper()
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(func() { resetCreateFlags() })
	resetCreateFlags()

	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
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

func TestCreateEditorOpensWithTemplate(t *testing.T) {
	nibsDir := setupCreateTest(t)
	scriptPath := writeEditorScript(t, filepath.Dir(nibsDir))

	// Set EDITOR to our test script
	t.Setenv("EDITOR", scriptPath)
	t.Setenv("VISUAL", "")

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"create", "Editor Test", "-t", "task", "--json",
	})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Find the created nib file
	entries, err := os.ReadDir(nibsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("no nib file created")
	}

	content, err := os.ReadFile(filepath.Join(nibsDir, entries[0].Name()))
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

func TestCreateFallsBackToTemplateWithoutEditor(t *testing.T) {
	nibsDir := setupCreateTest(t)

	// Ensure no editor is set
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"create", "No Editor Test", "-t", "task", "--json",
	})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	entries, _ := os.ReadDir(nibsDir)
	content, _ := os.ReadFile(filepath.Join(nibsDir, entries[0].Name()))
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

func TestCreateBodyFlagSkipsEditor(t *testing.T) {
	nibsDir := setupCreateTest(t)
	scriptPath := writeEditorScript(t, filepath.Dir(nibsDir))

	t.Setenv("EDITOR", scriptPath)
	t.Setenv("VISUAL", "")

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"create", "Body Flag Test", "-t", "task", "-d", "Custom body content", "--json",
	})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	entries, _ := os.ReadDir(nibsDir)
	content, _ := os.ReadFile(filepath.Join(nibsDir, entries[0].Name()))
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

func TestCreateVisualTakesPrecedence(t *testing.T) {
	nibsDir := setupCreateTest(t)
	scriptPath := writeEditorScript(t, filepath.Dir(nibsDir))

	// Set VISUAL to working script, EDITOR to a nonexistent command
	t.Setenv("VISUAL", scriptPath)
	t.Setenv("EDITOR", "nonexistent-editor-that-should-not-be-called")

	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"create", "Visual Test", "-t", "task", "--json",
	})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("create failed (VISUAL should have been used, not EDITOR): %v", err)
	}

	entries, _ := os.ReadDir(nibsDir)
	content, _ := os.ReadFile(filepath.Join(nibsDir, entries[0].Name()))
	body := string(content)

	if !strings.Contains(body, "EDITED BY SCRIPT") {
		t.Errorf("expected VISUAL editor to be used, got:\n%s", body)
	}
}
