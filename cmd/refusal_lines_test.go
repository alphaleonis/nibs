package cmd

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// enumeratedStore writes a store holding n files of one kind on top of a
// companion nib, which decides what the scan has to say about them: a v1 nib
// leaves the store current, a v0 one leaves a CONTENT step pending, and only
// then does an unclassifiable file block anything.
func enumeratedStore(t *testing.T, n int, companion string, content func(i int) (name, body string)) string {
	t.Helper()
	// The Cobra globals these tests drive are package-level, so a run that leaves
	// `list -q` or a migrate flag set changes what a later test's command prints.
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetListFlags)
	t.Cleanup(resetMigrateFlags)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	t.Cleanup(func() { rootCmd.SetOut(nil); rootCmd.SetErr(nil) })
	resetListFlags()
	resetMigrateFlags()

	files := map[string]string{"enum-ok1--fine.md": companion}
	for i := 1; i <= n; i++ {
		name, body := content(i)
		files[name] = body
	}
	return writeStore(t, filepath.Join(t.TempDir(), "proj"), "nibs:\n  prefix: enum-\n", files)
}

// A companion nib that leaves nothing pending, and one that leaves the v0
// content step pending.
const (
	enumCurrentNib = "---\nversion: 2\ntitle: Fine\nstatus: todo\ntype: task\n---\n\nBody.\n"
	enumV0Nib      = "---\ntitle: Legacy\nstatus: todo\ntype: task\n---\n\nBody.\n"
)

// newerFile is one file written by a nibs this build is too old to read.
func newerFile(i int) (string, string) {
	return fmt.Sprintf("enum-f%03d--future.md", i),
		fmt.Sprintf("---\n# enum-f%03d\nversion: 99\ntitle: Future\nstatus: todo\ntype: task\n---\n\nBody.\n", i)
}

// fenceLessFile is one .md the scan can prove is not a nib.
func fenceLessFile(i int) (string, string) {
	return fmt.Sprintf("note%03d.md", i), "# Just a document\n\nNo front matter here.\n"
}

// countEnumeratedLines returns how many lines of msg are enumeration entries —
// the indented lines a joined list contributes.
func countEnumeratedLines(msg string) int {
	n := 0
	for _, line := range strings.Split(msg, "\n") {
		if strings.HasPrefix(line, "  ") && strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

// TestARefusalDoesNotEnumerateEveryFileInTheStore is the line-count half of the
// echo boundary. maxEchoedFileTextRunes bounds one scalar; nothing bounded how
// MANY of them a refusal joins, so a store with 300 files this build cannot read
// made every command print 301 lines and 13 KB until the store was repaired —
// and this CLI's stated primary consumer is a coding agent, so that lands in a
// transcript rather than scrolling past.
//
// The load-bearing assertion is that the refusal does not GROW with the store: a
// message that happens to be short on one fixture proves nothing, while one whose
// size is identical for 60 and 240 offenders cannot be enumerating them.
func TestARefusalDoesNotEnumerateEveryFileInTheStore(t *testing.T) {
	const (
		offenders = 60
		manyMore  = 240
	)

	tests := []struct {
		name      string
		companion string
		content   func(int) (string, string)
		args      func(store string) []string
		// present is a spelling every rendering of this refusal must still carry:
		// bounding must not cost the reader the summary or the remedy.
		present []string
	}{
		{
			name:      "the newer-store refusal on an ordinary command",
			companion: enumCurrentNib,
			content:   newerFile,
			args:      func(s string) []string { return []string{"--nibs-path", s, "list", "-q"} },
			present:   []string{"newer nibs", "upgrade nibs"},
		},
		{
			name:      "the newer-store refusal from migrate",
			companion: enumCurrentNib,
			content:   newerFile,
			args:      func(s string) []string { return []string{"--nibs-path", s, "migrate"} },
			present:   []string{"newer nibs", "upgrade nibs"},
		},
		{
			name:      "migrate's refusal over files it cannot classify",
			companion: enumV0Nib,
			content:   fenceLessFile,
			args:      func(s string) []string { return []string{"--nibs-path", s, "migrate"} },
			present:   []string{"cannot be read as nibs", "nibs migrate"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refuse := func(n int) string {
				t.Helper()
				store := enumeratedStore(t, n, tt.companion, tt.content)
				_, err := runRootWith(t, tt.args(store)...)
				if err == nil {
					t.Fatalf("%s over %d offending files succeeded, want a refusal", tt.name, n)
				}
				return err.Error()
			}

			msg := refuse(offenders)
			lines := countEnumeratedLines(msg)
			if lines == 0 {
				t.Fatalf("the refusal enumerates nothing, so this test asserts no bound:\n%s", msg)
			}
			if grown := countEnumeratedLines(refuse(manyMore)); grown != lines {
				t.Errorf("the refusal grows with the store: %d enumerated lines for %d offending files, %d for %d",
					lines, offenders, grown, manyMore)
			}
			// A ceiling as well as a slope: several bounded lists still add up, and
			// the point is a refusal a reader can read. The gates that can coexist
			// here contribute one bounded list each.
			if len(msg) > 4096 {
				t.Errorf("the refusal is %d bytes for %d offending files:\n%.512s…", len(msg), offenders, msg)
			}
			if !strings.Contains(msg, fmt.Sprintf("%d more", offenders-maxEchoedListEntries)) {
				t.Errorf("the refusal elides entries without saying how many it dropped:\n%s", msg)
			}
			if !strings.Contains(msg, "nibs check") {
				t.Errorf("the refusal elides entries without naming the command that reports them in full:\n%s", msg)
			}
			for _, want := range tt.present {
				if !strings.Contains(msg, want) {
					t.Errorf("bounding cost the refusal %q:\n%s", want, msg)
				}
			}
		})
	}
}

// TestCheckStillCarriesEveryEnumeratedFile is the other side of the cap: the
// bounded refusals name `nibs check` as the full list, so check has to be one.
// It is the read-only diagnostic a user runs on purpose rather than something a
// pre-run gate prints on every command, which is why the length is information
// there and noise everywhere else.
func TestCheckStillCarriesEveryEnumeratedFile(t *testing.T) {
	const offenders = 60
	store := enumeratedStore(t, offenders, enumCurrentNib, newerFile)

	t.Cleanup(resetCheckFlags)
	resetCheckFlags()
	got := checkJSONResult(t, checkAppPastTheGate(t, store))
	if got.Migration == nil {
		t.Fatal("check said nothing about a store written by a newer nibs")
	}
	for i := 1; i <= offenders; i++ {
		name, _ := newerFile(i)
		if !strings.Contains(got.Migration.Message, name) {
			t.Fatalf("check --json dropped %s, so the refusal's `nibs check` remedy leads nowhere:\n%.800s…",
				name, got.Migration.Message)
		}
	}
	if strings.Contains(got.Migration.Message, "more; "+echoedListRemedyCheck) {
		t.Error("check elides the list and points at itself for the rest")
	}
}
