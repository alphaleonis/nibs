package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/spf13/pflag"
)

// resetUpdateFlags resets ALL global flag variables for the update command.
// Cobra's StringArrayVar appends to slices across Execute() calls on the
// singleton rootCmd, so every global must be zeroed between tests.
// Also resets Cobra's "Changed" tracking on all flags to prevent stale state.
// Persistent-flag reset (--config, --nibs-path, plus their pflag Value/Changed
// bits) is delegated to the shared resetRootPersistentFlags helper.
// These tests must NOT use t.Parallel() — they share the rootCmd singleton.
func resetUpdateFlags() {
	updateStatus = ""
	updateType = ""
	updatePriority = ""
	updateEstimate = ""
	updateTitle = ""
	updateBody = ""
	updateBodyFile = ""
	updateBodyReplaceOld = nil
	updateBodyReplaceNew = nil
	updateBodyAppend = ""
	updateParent = ""
	updateRemoveParent = false
	updateBlocking = nil
	updateRemoveBlocking = nil
	updateBlockedBy = nil
	updateRemoveBlockedBy = nil
	updateTag = nil
	updateRemoveTag = nil
	updateDocument = nil
	updateRemoveDocument = nil
	updateAfter = ""
	updateBefore = ""
	updateFirst = false
	updateIfMatch = ""
	updateJSON = false
	// Reset Cobra's "Changed" tracking on all flags
	updateCmd.Flags().Visit(func(f *pflag.Flag) {
		f.Changed = false
	})
	resetRootPersistentFlags()
}

func TestResetUpdateFlagsClearsAllState(t *testing.T) {
	// Set every global to a non-zero value
	updateStatus = "in-progress"
	updateType = "bug"
	updatePriority = "critical"
	updateEstimate = "xl"
	updateTitle = "dirty"
	updateBody = "dirty"
	updateBodyFile = "dirty"
	updateBodyReplaceOld = []string{"a"}
	updateBodyReplaceNew = []string{"b"}
	updateBodyAppend = "dirty"
	updateParent = "dirty"
	updateRemoveParent = true
	updateBlocking = []string{"x"}
	updateRemoveBlocking = []string{"y"}
	updateBlockedBy = []string{"z"}
	updateRemoveBlockedBy = []string{"w"}
	updateTag = []string{"t"}
	updateRemoveTag = []string{"r"}
	updateDocument = []string{"d"}
	updateRemoveDocument = []string{"e"}
	updateAfter = "dirty"
	updateBefore = "dirty"
	updateFirst = true
	updateIfMatch = "dirty"
	updateJSON = true

	resetUpdateFlags()

	// Verify all are at zero values
	if updateStatus != "" {
		t.Errorf("updateStatus not reset: %q", updateStatus)
	}
	if updateType != "" {
		t.Errorf("updateType not reset: %q", updateType)
	}
	if updatePriority != "" {
		t.Errorf("updatePriority not reset: %q", updatePriority)
	}
	if updateEstimate != "" {
		t.Errorf("updateEstimate not reset: %q", updateEstimate)
	}
	if updateTitle != "" {
		t.Errorf("updateTitle not reset: %q", updateTitle)
	}
	if updateBody != "" {
		t.Errorf("updateBody not reset: %q", updateBody)
	}
	if updateBodyFile != "" {
		t.Errorf("updateBodyFile not reset: %q", updateBodyFile)
	}
	if updateBodyReplaceOld != nil {
		t.Errorf("updateBodyReplaceOld not reset: %v", updateBodyReplaceOld)
	}
	if updateBodyReplaceNew != nil {
		t.Errorf("updateBodyReplaceNew not reset: %v", updateBodyReplaceNew)
	}
	if updateBodyAppend != "" {
		t.Errorf("updateBodyAppend not reset: %q", updateBodyAppend)
	}
	if updateParent != "" {
		t.Errorf("updateParent not reset: %q", updateParent)
	}
	if updateRemoveParent {
		t.Error("updateRemoveParent not reset")
	}
	if updateBlocking != nil {
		t.Errorf("updateBlocking not reset: %v", updateBlocking)
	}
	if updateRemoveBlocking != nil {
		t.Errorf("updateRemoveBlocking not reset: %v", updateRemoveBlocking)
	}
	if updateBlockedBy != nil {
		t.Errorf("updateBlockedBy not reset: %v", updateBlockedBy)
	}
	if updateRemoveBlockedBy != nil {
		t.Errorf("updateRemoveBlockedBy not reset: %v", updateRemoveBlockedBy)
	}
	if updateTag != nil {
		t.Errorf("updateTag not reset: %v", updateTag)
	}
	if updateRemoveTag != nil {
		t.Errorf("updateRemoveTag not reset: %v", updateRemoveTag)
	}
	if updateDocument != nil {
		t.Errorf("updateDocument not reset: %v", updateDocument)
	}
	if updateRemoveDocument != nil {
		t.Errorf("updateRemoveDocument not reset: %v", updateRemoveDocument)
	}
	if updateAfter != "" {
		t.Errorf("updateAfter not reset: %q", updateAfter)
	}
	if updateBefore != "" {
		t.Errorf("updateBefore not reset: %q", updateBefore)
	}
	if updateFirst {
		t.Error("updateFirst not reset")
	}
	if updateIfMatch != "" {
		t.Errorf("updateIfMatch not reset: %q", updateIfMatch)
	}
	if updateJSON {
		t.Error("updateJSON not reset")
	}
}

func TestUpdateRejectsDuplicateBodyReplaceFlags(t *testing.T) {
	t.Cleanup(func() {
		resetUpdateFlags()
	})
	resetUpdateFlags()

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
		"update", "dup-1",
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

func TestUpdateSingleBodyReplaceWorks(t *testing.T) {
	t.Cleanup(func() {
		resetUpdateFlags()
	})
	resetUpdateFlags()

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
		"update", "dup-2",
		"--body-replace-old", "line one", "--body-replace-new", "LINE ONE",
	})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("single --body-replace-old should succeed, got error: %v", err)
	}
}

func TestUpdateAfterWithStatusRepositionsAndUpdates(t *testing.T) {
	t.Cleanup(func() { resetUpdateFlags() })
	resetUpdateFlags()

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

	// update ccc --after aaa --status in-progress
	// Should reposition "ccc" after "aaa" (between a0 and c0) AND update status
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"update", "ccc",
		"--after", "aaa",
		"--status", "in-progress",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("update --after --status should succeed, got: %v", err)
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

func TestUpdateBeforeRepositionsBeforeSibling(t *testing.T) {
	t.Cleanup(func() { resetUpdateFlags() })
	resetUpdateFlags()

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

	// update aaa --before ccc → aaa should be repositioned before ccc (between c0 and e0)
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"update", "aaa",
		"--before", "ccc",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("update --before should succeed, got: %v", err)
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

func TestUpdateFirstRepositionsToFirstPosition(t *testing.T) {
	t.Cleanup(func() { resetUpdateFlags() })
	resetUpdateFlags()

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

	// update ccc --first → ccc should be repositioned to first (before a0)
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"update", "ccc",
		"--first",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("update --first should succeed, got: %v", err)
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

func TestUpdateIfMatchWithFieldsAndPositioning(t *testing.T) {
	t.Cleanup(func() { resetUpdateFlags() })
	resetUpdateFlags()

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
		"update", "ccc",
		"--if-match", etag,
		"--after", "aaa",
		"--status", "in-progress",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("update --if-match with --status and --after should succeed, got: %v", err)
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

func TestUpdatePositionOnlyWithoutMetadataFlags(t *testing.T) {
	t.Cleanup(func() { resetUpdateFlags() })
	resetUpdateFlags()

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

	// Positioning-only update: no --status, --type, etc.
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"update", "bbb",
		"--after", "aaa",
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("positioning-only update should succeed without metadata flags, got: %v", err)
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

func TestUpdatePositionFlagsMutuallyExclusive(t *testing.T) {
	t.Cleanup(func() { resetUpdateFlags() })
	resetUpdateFlags()

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
		{"after+before", []string{"--nibs-path", nibsDir, "update", "mut-1", "--after", "x", "--before", "y"}},
		{"after+first", []string{"--nibs-path", nibsDir, "update", "mut-1", "--after", "x", "--first"}},
		{"before+first", []string{"--nibs-path", nibsDir, "update", "mut-1", "--before", "x", "--first"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resetUpdateFlags()
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
