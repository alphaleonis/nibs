package nibcore

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// The three per-file diagnostics one load can emit, each in the shape that
// provokes it. Every one is O(store): a bad merge or a hand-edit reaches all of
// data/ as easily as one file.
var loadWarningKinds = []struct {
	name string
	// write materializes offender i and returns the id it would have had, so a
	// test can ask whether the retained diagnostics still name it.
	write func(t *testing.T, dataDir string, i int) string
	// diagnosed reports whether the retained diagnostics still carry every
	// offender — the channel the elision points at.
	diagnosed func(core *Core) int
}{
	{
		name: "unparseable files",
		write: func(t *testing.T, dataDir string, i int) string {
			id := fmt.Sprintf("bad%03d", i)
			// A duplicate front-matter key: yaml.v3 hard-errors on it.
			writeNibFile(t, dataDir, id+"--broken.md",
				"---\nversion: 1\ntitle: A\ntitle: B\nstatus: todo\n---\n\nBody.\n")
			return id
		},
		diagnosed: func(core *Core) int {
			unparseable, _ := core.LoadDiagnostics()
			return len(unparseable)
		},
	},
	{
		name: "duplicate ids",
		write: func(t *testing.T, dataDir string, i int) string {
			id := fmt.Sprintf("dup%03d", i)
			body := "---\nversion: 1\ntitle: Twin\nstatus: todo\n---\n\nBody.\n"
			writeNibFile(t, dataDir, id+".md", body)
			writeNibFile(t, dataDir, id+"--slug.md", body)
			return id
		},
		diagnosed: func(core *Core) int {
			_, duplicates := core.LoadDiagnostics()
			return len(duplicates)
		},
	},
	{
		name: "out-of-enum values",
		write: func(t *testing.T, dataDir string, i int) string {
			id := fmt.Sprintf("enum%03d", i)
			writeNibFile(t, dataDir, id+"--odd.md",
				"---\nversion: 1\ntitle: Odd\nstatus: bogus\n---\n\nBody.\n")
			return id
		},
		// Out-of-enum values are not retained as a load diagnostic; `nibs check`
		// recomputes them from the loaded nibs, which all loaded.
		diagnosed: nil,
	},
}

// loadWithWarnings loads a fresh store holding n offenders of one kind and
// returns everything the load wrote to its warn writer.
func loadWithWarnings(t *testing.T, kind int, n int) (*Core, string) {
	t.Helper()
	core, nibsDir := setupTestCore(t)
	dataDir := storeData(t, nibsDir)
	for i := 1; i <= n; i++ {
		loadWarningKinds[kind].write(t, dataDir, i)
	}
	var warnings bytes.Buffer
	core.SetWarnWriter(&warnings)
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return core, warnings.String()
}

// countWarnings counts the emitted warnings rather than the lines they occupy:
// one warning can span several lines (a yaml error does), and the budget is
// spent per warning so a multi-line one is never cut in half.
func countWarnings(warnings string) int {
	return strings.Count(warnings, "warning: ")
}

// TestLoadBoundsItsPerFileWarningStream is the stream half of the echo bound
// nibs-tmi1 put on joined refusals. Core.Load emits one warning per offending
// file through logWarn, so 300 unparseable files put 600 lines and 77 KB on
// stderr — on EVERY command, until the files are repaired — and this CLI's
// stated primary consumer is a coding agent, so that lands in a transcript.
//
// The load-bearing assertion is that the stream does not GROW with the store: a
// count that happens to be small on one fixture proves nothing, while one that
// is identical for 60 and 240 offenders cannot be emitting one per file.
func TestLoadBoundsItsPerFileWarningStream(t *testing.T) {
	const (
		offenders = 60
		manyMore  = 240
	)

	for i, kind := range loadWarningKinds {
		t.Run(kind.name, func(t *testing.T) {
			core, warnings := loadWithWarnings(t, i, offenders)
			emitted := countWarnings(warnings)
			if emitted == 0 {
				t.Fatalf("the load warned about nothing, so this test asserts no bound")
			}
			if _, grown := loadWithWarnings(t, i, manyMore); countWarnings(grown) != emitted {
				t.Errorf("the warning stream grows with the store: %d warnings for %d offenders, %d for %d",
					emitted, offenders, countWarnings(grown), manyMore)
			}
			if len(warnings) > 8192 {
				t.Errorf("the load wrote %d bytes of warnings for %d offenders:\n%.512s…",
					len(warnings), offenders, warnings)
			}
			if !strings.Contains(warnings, fmt.Sprintf("%d more", offenders-maxLoadWarnings)) {
				t.Errorf("the load elided warnings without saying how many it dropped:\n%s", warnings)
			}
			if !strings.Contains(warnings, "nibs check") {
				t.Errorf("the load elided warnings without naming the command that reports them in full:\n%s", warnings)
			}
			if kind.diagnosed != nil {
				if got := kind.diagnosed(core); got != offenders {
					t.Errorf("the retained diagnostics carry %d of %d offenders, so the `nibs check` remedy leads nowhere",
						got, offenders)
				}
			}
		})
	}
}

// TestLoadSaysNothingExtraWhenNothingWasElided pins the quiet side: a store with
// fewer offenders than the budget warns exactly as it always did, with no tail
// claiming an elision that did not happen.
func TestLoadSaysNothingExtraWhenNothingWasElided(t *testing.T) {
	for i, kind := range loadWarningKinds {
		t.Run(kind.name, func(t *testing.T) {
			_, warnings := loadWithWarnings(t, i, maxLoadWarnings)
			if got := countWarnings(warnings); got != maxLoadWarnings {
				t.Errorf("a store at exactly the budget emitted %d warnings, want %d", got, maxLoadWarnings)
			}
			if strings.Contains(warnings, "more") {
				t.Errorf("nothing was elided, but the load reported an elision:\n%s", warnings)
			}
		})
	}
}
