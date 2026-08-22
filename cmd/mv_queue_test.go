package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/output"
)

// dataFileDigests returns a content digest for every file under the store's
// data directory, keyed by filename, so a test can prove which files a
// command rewrote.
func dataFileDigests(t *testing.T, nibsPath string) map[string]string {
	t.Helper()
	dir := storeDataDir(nibsPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		out[e.Name()] = hex.EncodeToString(sum[:])
	}
	return out
}

// TestMvQueueOnKeyedQueueMovesExactlyOneFile pins decision 2.1/2.2 on the
// CLI: a queue reorder through `nibs mv --queue` repositions the subject in
// its milestone queue and rewrites the subject's file and no other — the
// one-file property that makes parallel queue edits merge cleanly. The
// property holds once every member carries a queue key, which the fixture's
// tnib-m001 queue (keys a..d) does; a member without one is keyed once by the
// first move that reads the queue, the engine's lazy backfill, and that write
// is outside what this test claims.
func TestMvQueueOnKeyedQueueMovesExactlyOneFile(t *testing.T) {
	nibsPath := setupQueueCLITest(t)
	before := dataFileDigests(t, nibsPath)

	if _, err := runRootWith(t, "--nibs-path", nibsPath, "mv", "tnib-e003", "--queue", "--first"); err != nil {
		t.Fatalf("mv --queue --first: %v", err)
	}

	after := dataFileDigests(t, nibsPath)
	var changed []string
	for name, digest := range after {
		if before[name] != digest {
			changed = append(changed, name)
		}
	}
	if len(changed) != 1 || !strings.HasPrefix(changed[0], "tnib-e003--") {
		t.Errorf("files changed = %v, want exactly the subject's file", changed)
	}
	if len(after) != len(before) {
		t.Errorf("file count changed %d -> %d; a queue move must not create or remove files", len(before), len(after))
	}
	if got, want := queueIDs(t, nibsPath, "tnib-m001"), "tnib-e003 tnib-e001 tnib-e002 tnib-e004"; strings.Join(got, " ") != want {
		t.Errorf("queue = %v, want %s", got, want)
	}
	// The sibling order key is untouched: e003 still sits among the roots in
	// its old place.
	ms, key, _ := axisFields(t, nibsPath, "tnib-e003")
	if ms != "tnib-m001" || key >= "a" {
		t.Errorf("milestone=%q milestone_order=%q, want tnib-m001 and a key before a", ms, key)
	}
}

// TestMvQueueAnchors pins the after/before grammar within the queue, with the
// anchor given in short form.
func TestMvQueueAnchors(t *testing.T) {
	nibsPath := setupQueueCLITest(t)
	if _, err := runRootWith(t, "--nibs-path", nibsPath, "mv", "tnib-e004", "--queue", "--after", "e001"); err != nil {
		t.Fatalf("mv --queue --after: %v", err)
	}
	if got, want := queueIDs(t, nibsPath, "tnib-m001"), "tnib-e001 tnib-e004 tnib-e002 tnib-e003"; strings.Join(got, " ") != want {
		t.Errorf("queue after --after = %v, want %s", got, want)
	}
	resetMvFlags()
	if _, err := runRootWith(t, "--nibs-path", nibsPath, "mv", "tnib-e001", "--queue", "--before", "tnib-e003"); err != nil {
		t.Fatalf("mv --queue --before: %v", err)
	}
	if got, want := queueIDs(t, nibsPath, "tnib-m001"), "tnib-e004 tnib-e002 tnib-e001 tnib-e003"; strings.Join(got, " ") != want {
		t.Errorf("queue after --before = %v, want %s", got, want)
	}
}

// TestMvQueueRefusals pins the refusals around the queue scope: the ordering
// engine's two membership errors surface as validation refusals (exit 2),
// --queue needs a position, is single-nib, and cannot be combined with the
// container flags.
func TestMvQueueRefusals(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantExit int
		wantMsg  string
	}{
		{"unassigned subject", []string{"mv", "tnib-t042", "--queue", "--first"}, output.ExitValidation, "assigned to no milestone, so it has no queue position"},
		{"anchor in another queue", []string{"mv", "tnib-e005", "--queue", "--after", "tnib-e001"}, output.ExitValidation, "not in the same milestone queue"},
		{"unknown anchor", []string{"mv", "tnib-e001", "--queue", "--after", "nope"}, output.ExitValidation, "queue nib not found"},
		{"no position", []string{"mv", "tnib-e001", "--queue"}, output.ExitValidation, "--queue needs a position"},
		{"multiple ids", []string{"mv", "tnib-e001", "tnib-e002", "--queue", "--first"}, output.ExitValidation, "--queue moves a single nib"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsPath := setupQueueCLITest(t)
			before := dataFileDigests(t, nibsPath)
			_, err := runRootWith(t, append([]string{"--nibs-path", nibsPath}, tt.args...)...)
			if err == nil {
				t.Fatal("mv succeeded, want refusal")
			}
			if code := reportExitError(io.Discard, err); code != tt.wantExit {
				t.Errorf("exit = %d, want %d", code, tt.wantExit)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want substring %q", err.Error(), tt.wantMsg)
			}
			after := dataFileDigests(t, nibsPath)
			for name, digest := range after {
				if before[name] != digest {
					t.Errorf("a refused move rewrote %s", name)
				}
			}
		})
	}

	for _, tt := range []struct {
		name string
		args []string
	}{
		{"with --parent", []string{"mv", "tnib-e001", "--queue", "--first", "--parent", "tnib-e002"}},
		{"with --children-of", []string{"mv", "--queue", "--children-of", "tnib-e001", "tnib-f001"}},
	} {
		t.Run("exclusive "+tt.name, func(t *testing.T) {
			nibsPath := setupQueueCLITest(t)
			_, err := runRootWith(t, append([]string{"--nibs-path", nibsPath}, tt.args...)...)
			if err == nil || !strings.Contains(err.Error(), "queue") {
				t.Fatalf("error = %v, want the flag-group refusal naming --queue", err)
			}
		})
	}
}

// TestMvQueueInversionLint pins decision 2.3 at the queue move: moving a nib
// ahead of a blocker that still blocks it succeeds (exit 0) and prints one
// stderr warning naming the pair; moving it behind its blocker warns nothing.
func TestMvQueueInversionLint(t *testing.T) {
	files := map[string]string{
		"qm1--waypoint.md": "---\nversion: 2\ntitle: Waypoint\nstatus: todo\ntype: milestone\n---\n",
		"qb--blocker.md":   "---\nversion: 2\ntitle: Blocker\nstatus: todo\ntype: task\nmilestone: qm1\nmilestone_order: a0\n---\n",
		"qa--blocked.md":   "---\nversion: 2\ntitle: Blocked\nstatus: todo\ntype: task\nblocked_by:\n    - qb\nmilestone: qm1\nmilestone_order: b0\n---\n",
	}

	t.Run("moving ahead of an open blocker warns once", func(t *testing.T) {
		nibsPath := writeStoreFiles(t, files)
		t.Cleanup(resetQueueCLIFlags)
		resetQueueCLIFlags()
		var stderr strings.Builder
		rootCmd.SetErr(&stderr)
		t.Cleanup(func() { rootCmd.SetErr(nil) })

		_, err := runRootWith(t, "--nibs-path", nibsPath, "mv", "qa", "--queue", "--before", "qb")
		if err != nil {
			t.Fatalf("mv --queue --before: %v", err)
		}
		if got, want := queueIDs(t, nibsPath, "qm1"), "qa qb"; strings.Join(got, " ") != want {
			t.Errorf("queue = %v, want %s — the lint must not block the write", got, want)
		}
		for _, want := range []string{"warning:", "milestone qm1", "qa is ahead of qb, which still blocks it"} {
			if !strings.Contains(stderr.String(), want) {
				t.Errorf("stderr = %q, want substring %q", stderr.String(), want)
			}
		}
		if strings.Count(stderr.String(), "warning:") != 1 {
			t.Errorf("want exactly one warning line, got:\n%s", stderr.String())
		}
	})

	t.Run("moving behind the blocker warns nothing", func(t *testing.T) {
		nibsPath := writeStoreFiles(t, files)
		t.Cleanup(resetQueueCLIFlags)
		resetQueueCLIFlags()
		var stderr strings.Builder
		rootCmd.SetErr(&stderr)
		t.Cleanup(func() { rootCmd.SetErr(nil) })
		if _, err := runRootWith(t, "--nibs-path", nibsPath, "mv", "qb", "--queue", "--first"); err != nil {
			t.Fatalf("mv --queue --first: %v", err)
		}
		if strings.Contains(stderr.String(), "warning:") {
			t.Errorf("unexpected warning:\n%s", stderr.String())
		}
	})

	t.Run("a move that creates no pair is silent about a pre-existing one", func(t *testing.T) {
		// qa already sits ahead of qb, which blocks it — an inversion some
		// earlier write created. Neither moving qa to the front it already
		// holds nor moving an uninvolved member ahead of both creates a pair,
		// so neither may re-report the one that is there.
		inverted := map[string]string{
			"qm1--waypoint.md": files["qm1--waypoint.md"],
			"qa--blocked.md":   "---\nversion: 2\ntitle: Blocked\nstatus: todo\ntype: task\nblocked_by:\n    - qb\nmilestone: qm1\nmilestone_order: a0\n---\n",
			"qb--blocker.md":   "---\nversion: 2\ntitle: Blocker\nstatus: todo\ntype: task\nmilestone: qm1\nmilestone_order: b0\n---\n",
			"qc--bystander.md": "---\nversion: 2\ntitle: Bystander\nstatus: todo\ntype: task\nmilestone: qm1\nmilestone_order: c0\n---\n",
		}
		for _, move := range [][]string{
			{"mv", "qa", "--queue", "--first"},
			{"mv", "qc", "--queue", "--first"},
		} {
			nibsPath := writeStoreFiles(t, inverted)
			t.Cleanup(resetQueueCLIFlags)
			resetQueueCLIFlags()
			var stderr strings.Builder
			rootCmd.SetErr(&stderr)
			t.Cleanup(func() { rootCmd.SetErr(nil) })
			if _, err := runRootWith(t, append([]string{"--nibs-path", nibsPath}, move...)...); err != nil {
				t.Fatalf("%v: %v", move, err)
			}
			if strings.Contains(stderr.String(), "warning:") {
				t.Errorf("%v re-reported a pair it did not create:\n%s", move, stderr.String())
			}
		}
	})
}
