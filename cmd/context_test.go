package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/spf13/pflag"
)

// resetContextFlags clears the package-level flag vars used by contextCmd so
// tests don't pollute each other via rootCmd's singleton state, and clears
// Cobra's "Changed" tracking.
func resetContextFlags() {
	contextJSON = false
	contextCmd.Flags().Visit(func(f *pflag.Flag) {
		f.Changed = false
	})
}

// setupContextCobraTest writes a store config (with prefix nibs-) plus nib files
// and returns the config path and .nibs directory so a test can drive the full
// Cobra pipeline via `--config <cfg> --nibs-path <dir> context ...`.
func setupContextCobraTest(t *testing.T, files map[string]string) (cfgPath, nibsDir string) {
	t.Helper()
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetContextFlags)
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	t.Cleanup(func() { configPath = "" })
	resetContextFlags()

	tmpDir := t.TempDir()
	nibsDir = filepath.Join(tmpDir, ".nibs")
	if err := os.MkdirAll(storeDataDir(nibsDir), 0755); err != nil {
		t.Fatal(err)
	}
	cfgPath = filepath.Join(nibsDir, "config.yml")
	if err := os.WriteFile(cfgPath, []byte("nibs:\n  prefix: nibs-\n  id_length: 4\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(dataPath(nibsDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return cfgPath, nibsDir
}

// contextFixture is a small milestone subtree used by the context tests:
//   - nibs-din3 milestone (in-progress) is the root container
//   - nibs-aaaa task child (completed, estimate M)
//   - nibs-bbbb task child (in-progress, estimate M)
func contextFixture() map[string]string {
	return map[string]string{
		"nibs-din3--milestone.md": "---\nversion: 2\ntitle: Alpha Milestone\nstatus: in-progress\ntype: milestone\n---\n\nRoot container.\n",
		"nibs-aaaa--done.md":      "---\nversion: 2\ntitle: Done Task\nstatus: completed\ntype: task\nestimate: M\nparent: nibs-din3\n---\n\nDone.\n",
		"nibs-bbbb--active.md":    "---\nversion: 2\ntitle: Active Task\nstatus: in-progress\ntype: task\nestimate: M\nparent: nibs-din3\n---\n\nActive.\n",
	}
}

// runContextJSON drives `context --json <idArg>` through the full Cobra
// pipeline and returns the decoded context output.
func runContextJSON(t *testing.T, cfgPath, nibsDir, idArg string) contextOutput {
	t.Helper()
	resetContextFlags()
	// --config alone names the store; the two flags together are refused.
	_ = nibsDir
	rootCmd.SetArgs([]string{
		"--config", cfgPath,
		"context", "--json", idArg,
	})
	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("context --json %q failed: %v", idArg, execErr)
	}
	var got contextOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal context output for %q: %v\nraw: %s", idArg, err, out)
	}
	return got
}

// TestContextCommand_ShortIDResolves pins short-id resolution:
// `context <short-id>` must resolve to the same nib as `context <full-id>`,
// with no "nib not found" warning and identical summary content.
func TestContextCommand_ShortIDResolves(t *testing.T) {
	cfgPath, nibsDir := setupContextCobraTest(t, contextFixture())

	full := runContextJSON(t, cfgPath, nibsDir, "nibs-din3")
	short := runContextJSON(t, cfgPath, nibsDir, "din3")

	// The full id already works today; guard the expectations it sets up.
	if full.Root == nil {
		t.Fatalf("full-id context has nil Root; fixture/setup broken: %+v", full)
	}
	if full.Root.ID != "nibs-din3" {
		t.Fatalf("full-id Root.ID = %q, want nibs-din3", full.Root.ID)
	}
	if len(full.Warnings) != 0 {
		t.Fatalf("full-id context produced warnings: %v", full.Warnings)
	}

	// The bug: the short id yields "nib not found" + empty data.
	if len(short.Warnings) != 0 {
		t.Errorf("short-id context produced warnings %v, want none", short.Warnings)
	}
	if short.Root == nil {
		t.Fatalf("short-id context has nil Root — short id did not resolve")
	}
	if short.Root.ID != "nibs-din3" {
		t.Errorf("short-id Root.ID = %q, want nibs-din3", short.Root.ID)
	}

	// The two summaries must be identical: resolving a short id is exactly
	// resolving its full id.
	if !reflect.DeepEqual(short, full) {
		t.Errorf("short-id summary differs from full-id summary\nshort: %+v\nfull:  %+v", short, full)
	}
}

// TestContextCommand_UnknownIDWarns pins that an unknown id still produces the
// "nib not found" warning and an empty summary (behavior preserved by the fix).
func TestContextCommand_UnknownIDWarns(t *testing.T) {
	cfgPath, nibsDir := setupContextCobraTest(t, contextFixture())

	sum := runContextJSON(t, cfgPath, nibsDir, "zzzz")

	if sum.Root != nil {
		t.Errorf("unknown-id context Root = %+v, want nil", sum.Root)
	}
	if len(sum.Warnings) == 0 {
		t.Errorf("unknown-id context produced no warnings, want a 'nib not found' warning")
	}
}
