package cmd

import (
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/mdsection"
	"github.com/alphaleonis/nibs/internal/testsupport/vocabtest"
)

// TestCloseCompletionFollowsTheDoneRole proves the completion reason — the one
// close reason that rewrites the parent's Current Focus — is derived from the
// done ROLE rather than from a name kept in close.go. The probe reassigns
// "deferred" to the done role; deferred precedes completed in DefaultStatuses
// order, so it becomes the first done-role status, and closing --as deferred
// must count as the accomplishment and rewrite the parent's focus.
func TestCloseCompletionFollowsTheDoneRole(t *testing.T) {
	vocabtest.WithStatusRole(t, "deferred", config.RoleDone)

	nibsDir := setupCloseTest(t, map[string]string{
		"dr-ms--milestone.md": "---\nversion: 1\ntitle: Milestone\nstatus: in-progress\ntype: milestone\ncreated_at: 2026-01-02T03:04:05Z\nupdated_at: 2026-01-02T03:04:05Z\n---\n\n## Current Focus\n\nWorking on phase 1.\n",
		"dr-ch--child.md":     "---\nversion: 1\ntitle: Child\nstatus: in-progress\ntype: task\nparent: dr-ms\n---\n\nBody.\n",
	})

	withStdin(t, "the work got done under another name\n")
	rootCmd.SetArgs([]string{
		"--nibs-path", nibsDir,
		"close", "dr-ch", "--as", "deferred", "--summary", "-",
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("close --as deferred failed: %v", err)
	}

	milestone := readNibFile(t, nibsDir, "dr-ms--milestone.md")
	focus, found := mdsection.Find(milestone, "Current Focus", mdsection.AnyLevel)
	if !found {
		t.Fatalf("parent lost its Current Focus section:\n%s", milestone)
	}
	if !strings.Contains(focus, "Completed dr-ch") {
		t.Errorf("deferred carries the done role, so closing --as deferred must rewrite the parent's Current Focus; got: %q", focus)
	}
}
