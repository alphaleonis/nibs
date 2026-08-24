package cmd

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/output"
)

// handwrittenStampShapes are the timestamp blocks a hand-written file can carry
// and still leave loadNib something to synthesize — the whole set the fix has to
// cover, so every case here walks all three.
var handwrittenStampShapes = []struct {
	name   string
	stamps string
}{
	{"neither stamp", ""},
	{"created_at only", "created_at: 2026-01-02T03:04:05Z\n"},
	{"updated_at only", "updated_at: 2026-01-02T03:04:05Z\n"},
}

// handwrittenPairStore returns a two-nib store in the shape a person types by
// hand: the keys nibs demands, and none of the timestamps nib.Render emits.
// stamps is spliced into both files so one fixture covers each shape loadNib
// synthesizes a missing stamp for.
func handwrittenPairStore(stamps string) map[string]string {
	file := func(title, body string) string {
		return "---\nversion: 2\ntitle: " + title + "\nstatus: todo\ntype: task\n" + stamps + "---\n\n" + body + "\n"
	}
	return map[string]string{
		"qa--alpha.md": file("Alpha", "Body A."),
		"qb--beta.md":  file("Beta", "Body B."),
	}
}

// checkIssues loads the store at nibsPath and runs the `nibs check` report over
// it, returning the issue count and the rendered report. It drives runCheck
// rather than rootCmd.Execute because checkCmd's RunE exits the process on a
// non-empty report.
func checkIssues(t *testing.T, nibsPath string) (int, string) {
	t.Helper()
	t.Cleanup(resetCheckFlags)
	resetCheckFlags()

	core := nibcore.New(nibsPath, config.Default())
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("loading %s for check: %v", nibsPath, err)
	}
	var total int
	var runErr error
	out := captureStdout(t, func() { total, runErr = runCheck(&App{Core: core}) })
	if runErr != nil {
		t.Fatalf("runCheck error = %v", runErr)
	}
	return total, out
}

// TestSetBlockingOnAHandwrittenStore is the regression guard for nibs-kkgu: on a
// store whose files were written by hand and never rewritten by nibs, a write
// that touches a TARGET nib was refused as a conflict. `--blocking` is the
// shape that exposes it — the subject's own write carries the caller's if-match,
// while the reverse `blocked_by:` edge onto the target derives its precondition
// from the target as this process loaded it, and the loader synthesizes the
// stamps the file omits.
//
// One case per shape loadNib synthesizes for, because the synthesis is a
// per-field fallback: a file can omit either stamp on its own.
func TestSetBlockingOnAHandwrittenStore(t *testing.T) {
	for _, shape := range handwrittenStampShapes {
		t.Run(shape.name, func(t *testing.T) {
			resetSetFlags()
			t.Cleanup(resetSetFlags)
			t.Cleanup(func() { rootCmd.SetArgs(nil) })

			nibsPath := writeStoreFiles(t, handwrittenPairStore(shape.stamps))
			if _, err := runRootWith(t, "--nibs-path", nibsPath, "set", "qb", "--blocking", "qa"); err != nil {
				t.Fatalf("set --blocking against a hand-written target: %v", err)
			}

			if got := readNibFile(t, nibsPath, "qa--alpha.md"); !strings.Contains(got, "blocked_by:") {
				t.Errorf("the reverse edge never reached the target:\n%s", got)
			}
			if total, out := checkIssues(t, nibsPath); total != 0 {
				t.Errorf("the store does not round-trip clean: %d issue(s)\n%s", total, out)
			}
		})
	}
}

// TestGetETagIsUsableAsIfMatchOnAHandwrittenStore pins the read side of the same
// defect. `nibs get -f etag` is where a caller obliged to supply --if-match gets
// one, and on a hand-written store the value it printed was not the value the
// store compared against — leaving a user of such a store no way to satisfy
// nibs.require_if_match at all.
func TestGetETagIsUsableAsIfMatchOnAHandwrittenStore(t *testing.T) {
	for _, shape := range handwrittenStampShapes {
		t.Run(shape.name, func(t *testing.T) {
			resetSetFlags()
			t.Cleanup(resetSetFlags)
			t.Cleanup(func() { rootCmd.SetArgs(nil) })

			nibsPath := writeStoreFiles(t, handwrittenPairStore(shape.stamps))
			etag := etagOf(t, nibsPath, "qa")

			resetSetFlags()
			if _, err := runRootWith(t, "--nibs-path", nibsPath,
				"set", "qa", "--status", "in-progress", "--if-match", etag); err != nil {
				t.Fatalf("the etag `get` printed (%s) was refused as --if-match: %v", etag, err)
			}
		})
	}
}

// TestSetIfMatchStillConflictsOnAHandwrittenStore is the CLI half of the guard
// the reconciliation must not weaken: closing the false conflict must leave the
// true one intact, on exactly the stores the fix is for. An edit that lands
// between the `get` and the `set` is still refused, with the conflict exit.
func TestSetIfMatchStillConflictsOnAHandwrittenStore(t *testing.T) {
	for _, shape := range handwrittenStampShapes {
		t.Run(shape.name, func(t *testing.T) {
			resetSetFlags()
			t.Cleanup(resetSetFlags)
			t.Cleanup(func() { rootCmd.SetArgs(nil) })

			files := handwrittenPairStore(shape.stamps)
			nibsPath := writeStoreFiles(t, files)
			etag := etagOf(t, nibsPath, "qa")

			edited := strings.Replace(files["qa--alpha.md"], "Body A.", "Edited elsewhere.", 1)
			if err := os.WriteFile(dataPath(nibsPath, "qa--alpha.md"), []byte(edited), 0644); err != nil {
				t.Fatal(err)
			}

			resetSetFlags()
			_, err := runRootWith(t, "--nibs-path", nibsPath,
				"set", "qa", "--status", "in-progress", "--if-match", etag)
			if err == nil {
				t.Fatal("a stale --if-match over an external edit must be refused, got nil")
			}
			if code := reportExitError(io.Discard, err); code != output.ExitConflict {
				t.Errorf("exit = %d, want %d (conflict): %v", code, output.ExitConflict, err)
			}
			if got := readNibFile(t, nibsPath, "qa--alpha.md"); got != edited {
				t.Errorf("the refused write landed anyway:\n%s", got)
			}
		})
	}
}
