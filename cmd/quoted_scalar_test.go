package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/reprefix"
	"github.com/alphaleonis/nibs/internal/store"
	"github.com/alphaleonis/nibs/internal/testskip"
)

// spanBreakingScalar carries the two runes %q passes through and
// assertNoDeception does not look for: a backtick, which closes a code span the
// message opens, and U+3164, which renders as blank.
const spanBreakingScalar = "q`ㅤz"

func assertScalarStripped(t *testing.T, surface, text string) {
	t.Helper()
	if strings.Contains(text, "q`") || strings.ContainsRune(text, 'ㅤ') {
		t.Errorf("%s echoed the scalar raw:\n%s", surface, text)
	}
	if !strings.Contains(text, "q  z") {
		t.Errorf("%s did not echo the scalar in its stripped form:\n%s", surface, text)
	}
}

func TestQuotedFileScalarsAreStripped(t *testing.T) {
	t.Run("check duplicate-id diagnostics", func(t *testing.T) {
		result := &nibcore.LinkCheckResult{
			DuplicateIDs: []nibcore.DuplicateID{{NibID: spanBreakingScalar, Loaded: "data/a.md", Shadowed: "data/b.md"}},
		}
		plain := captureStdout(t, func() { renderLoadDiagnostics(result, nil) })
		prev := checkFix
		checkFix = true
		t.Cleanup(func() { checkFix = prev })
		fix := captureStdout(t, func() { renderLoadDiagnostics(result, nil) })
		assertScalarStripped(t, "check (plain)", plain)
		assertScalarStripped(t, "check --fix", fix)
	})

	t.Run("set-prefix dry-run plan", func(t *testing.T) {
		out := captureStdout(t, func() {
			if err := printPlan(&reprefix.RenamePlan{OldPrefix: spanBreakingScalar, NewPrefix: "new-"}, false); err != nil {
				t.Fatalf("printPlan: %v", err)
			}
		})
		assertScalarStripped(t, "printPlan", out)
	})

	t.Run("set-prefix from a stored prefix", func(t *testing.T) {
		storeDir := writeStoreFiles(t, nil)
		writeFileT(t, filepath.Join(storeDir, store.ConfigFileName),
			"nibs:\n  prefix: \""+spanBreakingScalar+"-\"\n  id_length: 4\n")
		t.Cleanup(resetRootPersistentFlags)
		t.Cleanup(func() { setPrefixDryRun, setPrefixForce, setPrefixJSON = false, false, false })
		out, err := runRootWith(t, "--nibs-path", storeDir, "config", "set-prefix", "newp-", "--force")
		if err != nil {
			t.Fatalf("config set-prefix: %v\nout: %s", err, out)
		}
		assertScalarStripped(t, "set-prefix", out)
	})

	t.Run("migrate refusal over duplicate ids", func(t *testing.T) {
		storeDir := writeStoreFiles(t, nil)
		const content = "---\nversion: 2\ntitle: One\nstatus: todo\n---\n\nBody.\n"
		for _, name := range []string{spanBreakingScalar + "--a.md", spanBreakingScalar + "--b.md"} {
			if err := os.WriteFile(dataPath(storeDir, name), []byte(content), 0o644); err != nil {
				testskip.Unavailable(t, testskip.HostileFilenames, "os.WriteFile(%q): %v", name, err)
				return
			}
		}
		_, err := loadStoreForMigration(newMigrateEnv(storeDir))
		if err == nil {
			t.Fatal("loadStoreForMigration accepted a store with a duplicate id")
		}
		assertScalarStripped(t, "loadStoreForMigration", err.Error())
	})
}
