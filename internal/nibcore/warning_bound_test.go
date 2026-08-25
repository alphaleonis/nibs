package nibcore

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/fsnotify/fsnotify"

	"github.com/alphaleonis/nibs/internal/config"
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
	return core, reloadWarnings(t, core)
}

// reloadWarnings loads an already-constructed core again and returns everything
// THAT load wrote to its warn writer, with the previous load's output out of the
// way — which is what lets a test ask what a second read says rather than what
// two reads say together.
func reloadWarnings(t *testing.T, core *Core) string {
	t.Helper()
	var warnings bytes.Buffer
	core.SetWarnWriter(&warnings)
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return warnings.String()
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
			if !strings.Contains(warnings, fmt.Sprintf("%d more", offenders-maxWarningsPerBatch)) {
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
			_, warnings := loadWithWarnings(t, i, maxWarningsPerBatch)
			if got := countWarnings(warnings); got != maxWarningsPerBatch {
				t.Errorf("a store at exactly the budget emitted %d warnings, want %d", got, maxWarningsPerBatch)
			}
			if strings.Contains(warnings, "more") {
				t.Errorf("nothing was elided, but the load reported an elision:\n%s", warnings)
			}
		})
	}
}

// watcherBatch writes n files that fail to parse into a watched store and feeds
// them to handleChanges as ONE debounce batch, returning everything the batch
// wrote to the warn writer.
func watcherBatch(t *testing.T, n int) (*Core, string, string) {
	t.Helper()
	core, nibsDir := setupTestCore(t)
	setWatching(core)

	batch := make(map[string]fsnotify.Op, n)
	for i := 1; i <= n; i++ {
		path := dataPath(nibsDir, fmt.Sprintf("arr%03d--broken.md", i))
		writeNibFileAtomic(t, path, "---\nversion: 1\ntitle: A\ntitle: B\nstatus: todo\n---\n\nBody.\n")
		batch[path] = fsnotify.Create
	}

	var warnings bytes.Buffer
	core.SetWarnWriter(&warnings)
	core.handleChanges(batch)
	return core, nibsDir, warnings.String()
}

// TestWatcherBoundsOneBatchsWarningStream is the same bound on the other entry
// point. A `git pull` in the separate .nibs repository delivers one debounce
// window holding every changed file, and handleChanges warned once per file:
// 300 files put 600 lines and ~62 KB through a running `nibs serve`.
func TestWatcherBoundsOneBatchsWarningStream(t *testing.T) {
	const (
		offenders = 60
		manyMore  = 240
	)

	_, _, warnings := watcherBatch(t, offenders)
	emitted := countWarnings(warnings)
	if emitted == 0 {
		t.Fatal("the batch warned about nothing, so this test asserts no bound")
	}
	if _, _, grown := watcherBatch(t, manyMore); countWarnings(grown) != emitted {
		t.Errorf("the warning stream grows with the batch: %d warnings for %d arrivals, %d for %d",
			emitted, offenders, countWarnings(grown), manyMore)
	}
	if len(warnings) > 8192 {
		t.Errorf("one batch wrote %d bytes of warnings for %d arrivals:\n%.512s…", len(warnings), offenders, warnings)
	}
	if !strings.Contains(warnings, fmt.Sprintf("%d more", offenders-maxWarningsPerBatch)) {
		t.Errorf("the batch elided warnings without saying how many it dropped:\n%s", warnings)
	}
	if !strings.Contains(warnings, "nibs check") {
		t.Errorf("the batch elided warnings without naming the command that reports them in full:\n%s", warnings)
	}
}

// TestWatcherElisionSendsTheReaderSomewhereReal pins the remedy the elision
// prints, which is the whole reason a suppressed warning is acceptable.
//
// The suppressed files are never retained on the WATCHING core — nothing reads
// a long-lived process's diagnostics across a process boundary, so retaining
// them there would be state with no consumer. `nibs check` does not read them
// either: it builds its own Core and loads the store fresh, which is what makes
// the remedy hold. This asserts that second load actually names every file the
// batch warned about and then hid.
func TestWatcherElisionSendsTheReaderSomewhereReal(t *testing.T) {
	const offenders = 60
	_, nibsDir, warnings := watcherBatch(t, offenders)
	if !strings.Contains(warnings, "nibs check") {
		t.Fatal("the batch printed no remedy, so this test asserts nothing about it")
	}

	// What `nibs check` does: a separate Core over the same directory.
	fresh := New(nibsDir, config.Default())
	fresh.SetWarnWriter(nil)
	if err := fresh.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	unparseable, _ := fresh.LoadDiagnostics()
	if len(unparseable) != offenders {
		t.Fatalf("a fresh load names %d of the %d files the batch warned about, so the elision's remedy leads nowhere",
			len(unparseable), offenders)
	}
}

// TestReloadSaysNothingTheFirstLoadAlreadySaid is the bound's other dimension.
// The one above bounds how much ONE load says about a store; this bounds how
// often the SAME thing gets said to one reader.
//
// Three verbs — the two areas edits and `config set-prefix` — re-read the store
// under its write lock, because the snapshot they loaded at startup may name
// files a concurrent rename has moved. That reload used to re-emit every
// per-file diagnostic verbatim and spend a second copy of the per-load budget,
// so a store with 30 unparseable files put 42 lines on stderr where a `nibs
// list` over the same store put 21.
//
// The fourth read is what keeps the fix honest: a file that only went bad
// between the two reads is news, and must still reach the reader.
func TestReloadSaysNothingTheFirstLoadAlreadySaid(t *testing.T) {
	const offenders = 3

	for _, kind := range loadWarningKinds {
		t.Run(kind.name, func(t *testing.T) {
			core, nibsDir := setupTestCore(t)
			dataDir := storeData(t, nibsDir)
			for n := 1; n <= offenders; n++ {
				kind.write(t, dataDir, n)
			}

			if got := countWarnings(reloadWarnings(t, core)); got != offenders {
				t.Fatalf("the first load emitted %d warnings for %d offenders, so this test asserts nothing about the second",
					got, offenders)
			}

			repeat := reloadWarnings(t, core)
			if got := countWarnings(repeat); got != 0 {
				t.Errorf("re-reading an unchanged store repeated %d warning(s) the first load already printed:\n%s", got, repeat)
			}
			// The diagnostics are rebuilt from scratch on every load and are
			// what `nibs check` reads back, so silencing the repeat must not
			// silence them.
			if kind.diagnosed != nil {
				if got := kind.diagnosed(core); got != offenders {
					t.Errorf("after the reload the retained diagnostics carry %d of %d offenders, so the `nibs check` remedy leads nowhere",
						got, offenders)
				}
			}

			arrived := kind.write(t, dataDir, offenders+1)
			news := reloadWarnings(t, core)
			if got := countWarnings(news); got != 1 {
				t.Errorf("a file that went bad between the two reads produced %d warning(s), want exactly 1:\n%s", got, news)
			}
			if !strings.Contains(news, arrived) {
				t.Errorf("the reload never named %s, which only went bad after the first read:\n%s", arrived, news)
			}
		})
	}
}

// TestReloadDoesNotSpendASecondWarningBudget pins the half of the doubling that
// the count above cannot see: the budget is per load, so a reload over a store
// past it used to emit a fresh twenty AND a second elision line.
//
// A warning the first load SUPPRESSED is deliberately not promoted by the
// reload either. That load's elision line already spoke for it and named `nibs
// check`, and re-reading the same bytes is not what makes it worth printing.
func TestReloadDoesNotSpendASecondWarningBudget(t *testing.T) {
	const offenders = maxWarningsPerBatch + 10

	for _, kind := range loadWarningKinds {
		t.Run(kind.name, func(t *testing.T) {
			core, nibsDir := setupTestCore(t)
			dataDir := storeData(t, nibsDir)
			for n := 1; n <= offenders; n++ {
				kind.write(t, dataDir, n)
			}

			// The elision line goes through logWarn too, so a load that spends
			// the whole budget emits one more than the budget allows examples.
			first := reloadWarnings(t, core)
			if got := countWarnings(first); got != maxWarningsPerBatch+1 {
				t.Fatalf("the first load emitted %d warnings, want the budget's %d plus its elision line", got, maxWarningsPerBatch)
			}
			if !strings.Contains(first, "10 more") {
				t.Fatalf("the first load elided nothing, so this test asserts nothing about the second:\n%s", first)
			}

			repeat := reloadWarnings(t, core)
			if got := countWarnings(repeat); got != 0 {
				t.Errorf("the reload spent a second warning budget, emitting %d warning(s):\n%s", got, repeat)
			}
			if strings.Contains(repeat, "more") {
				t.Errorf("the reload printed a second elision line for warnings the first load already elided:\n%s", repeat)
			}
		})
	}
}
