package cmd

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/nibcore"
)

// checkJSONResult drives runCheck in --json mode over an App and decodes the
// envelope.
func checkJSONResult(t *testing.T, app *App) checkResult {
	t.Helper()
	checkJSON = true
	t.Cleanup(func() { checkJSON = false })
	var runErr error
	out := captureStdout(t, func() { _, runErr = runCheck(app) })
	if runErr != nil {
		t.Fatalf("runCheck error = %v", runErr)
	}
	var got checkResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decoding the check envelope: %v\nraw: %s", err, out)
	}
	return got
}

// TestCheckJSONMigrationFieldIsStructured pins the shape of the envelope's
// `migration` key.
//
// It used to be one free-text sentence carrying THREE states — healthy, pending,
// probe refused — where "" meant healthy and the other two were separable only by
// matching on prose, while its doc described just one of them. The two non-healthy
// states have opposite remedies: `nibs migrate` resolves a pending step and
// refuses a newer store with materially the same message, so an agent that reads
// "run nibs migrate" off the second one loops forever. Every sibling field in this
// envelope is structured.
func TestCheckJSONMigrationFieldIsStructured(t *testing.T) {
	t.Run("a pending migration names its steps and flags the partial load", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		t.Cleanup(resetCheckFlags)
		resetCheckFlags()
		_, storeDir := writeLegacyStore(t, "nibs:\n  prefix: leg-\n", map[string]string{
			"leg-a1--one.md": layoutNib,
		})

		got := checkJSONResult(t, checkAppPastTheGate(t, storeDir))
		if got.Migration == nil {
			t.Fatal("check reported no migration state for a store no other command will touch")
		}
		if got.Migration.Kind != migrationKindPending {
			t.Errorf("kind = %q, want %q", got.Migration.Kind, migrationKindPending)
		}
		if len(got.Migration.Steps) == 0 || got.Migration.Steps[0] != "layout" {
			t.Errorf("steps = %v, want the layout step first", got.Migration.Steps)
		}
		if !got.Migration.PartialLoad {
			t.Error("partial_load = false, but a shape step means Core.Load saw only part of the store")
		}
		if !strings.Contains(got.Migration.Message, "nibs migrate") {
			t.Errorf("message = %q, want it to name the command that fixes this", got.Migration.Message)
		}
		if got.Success {
			t.Error("success = true on a store needing migration")
		}
	})

	t.Run("a store this build cannot read is blocked, not pending", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		t.Cleanup(resetCheckFlags)
		resetCheckFlags()
		// A file written by a newer nibs: `nibs migrate` refuses it too, so
		// pointing a consumer at migrate would be a no-op loop.
		storeDir := writeStore(t, filepath.Join(t.TempDir(), "proj"), "nibs:\n  prefix: chk-\n", map[string]string{
			"chk-a1--future.md": "---\nversion: 99\ntitle: Future\nstatus: todo\ntype: task\n---\n\nBody.\n",
		})

		got := checkJSONResult(t, checkAppPastTheGate(t, storeDir))
		if got.Migration == nil {
			t.Fatal("check said nothing about a store written by a newer nibs")
		}
		if got.Migration.Kind != migrationKindBlocked {
			t.Errorf("kind = %q, want %q — running `nibs migrate` cannot resolve this", got.Migration.Kind, migrationKindBlocked)
		}
		if len(got.Migration.Steps) != 0 {
			t.Errorf("steps = %v, want none: no step was decided", got.Migration.Steps)
		}
		if !strings.Contains(got.Migration.Message, "upgrade nibs") {
			t.Errorf("message = %q, want it to name the only remedy", got.Migration.Message)
		}
	})

	t.Run("a current store omits the key entirely", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		t.Cleanup(resetCheckFlags)
		resetCheckFlags()
		storeDir := writeStore(t, filepath.Join(t.TempDir(), "proj"), "nibs:\n  prefix: chk-\n", map[string]string{
			"chk-a1--one.md": layoutNib,
		})

		got := checkJSONResult(t, checkAppPastTheGate(t, storeDir))
		if got.Migration != nil {
			t.Errorf("migration = %+v on a current store, want the key omitted", got.Migration)
		}
		if !got.Success {
			t.Error("success = false on a healthy, current store")
		}
	})
}

// TestCheckSkipsTheMigrationProbeOnceTheGateHasAnswered pins the second scan away.
//
// The pre-run gate performs a full store scan and only lets a command through when
// nothing is pending, so a gated command that then probes again is recomputing a
// known answer over every file in the store. `check --fix` did exactly that, moments
// apart. Plain check is EXEMPT from the gate, so it must still probe — the flag says
// "already established here", never "nothing is pending".
func TestCheckSkipsTheMigrationProbeOnceTheGateHasAnswered(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetCheckFlags)
	resetCheckFlags()

	_, storeDir := writeLegacyStore(t, "nibs:\n  prefix: leg-\n", map[string]string{
		"leg-a1--one.md": layoutNib,
	})
	core := nibcore.New(storeDir, nil)
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Ungated (plain check): the probe runs and finds the pending step.
	if got := storeMigration(&App{Core: core}); got == nil {
		t.Error("plain check must still probe — it is exempt from the gate, so nothing else has answered")
	}
	// Gated: the gate already answered, so no second scan and no second answer.
	if got := storeMigration(&App{Core: core, MigrationGatePassed: true}); got != nil {
		t.Errorf("storeMigration = %+v, want nil: the gate already established this store is current", got)
	}
}

// TestPersistentPreRunERecordsWhetherTheGateRan pins the wiring behind the skip
// above: the flag must be set for a GATED command and left clear for plain check,
// or check's exemption stops diagnosing the very states it exists for.
func TestPersistentPreRunERecordsWhetherTheGateRan(t *testing.T) {
	storeDir := writeStore(t, filepath.Join(t.TempDir(), "proj"), "nibs:\n  prefix: chk-\n", map[string]string{
		"chk-a1--one.md": layoutNib,
	})

	tests := []struct {
		name string
		fix  bool
		want bool
	}{
		{name: "plain check is exempt, so nothing was established", fix: false, want: false},
		{name: "check --fix is gated, so getting here proves the store is current", fix: true, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(resetRootPersistentFlags)
			t.Cleanup(resetCheckFlags)
			resetCheckFlags()
			checkFix = tt.fix
			nibsPath = storeDir
			checkCmd.SetContext(context.Background())
			t.Cleanup(func() { checkCmd.SetContext(context.Background()) })

			if err := rootCmd.PersistentPreRunE(checkCmd, nil); err != nil {
				t.Fatalf("the pre-run hook refused a current store: %v", err)
			}
			if got := getApp(checkCmd).MigrationGatePassed; got != tt.want {
				t.Errorf("MigrationGatePassed = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestCheckSuppressesTheAllLoadedCheckmarkOnAnUnmigratedStore pins the one place
// the report contradicted itself: on a pre-layout store Core.Load finds nothing
// where it looks, so "0 of 0 files failed" printed a green "All nib files loaded"
// directly beneath the red "Store needs migration".
func TestCheckSuppressesTheAllLoadedCheckmarkOnAnUnmigratedStore(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetCheckFlags)
	resetCheckFlags()
	_, storeDir := writeLegacyStore(t, "nibs:\n  prefix: leg-\n", map[string]string{
		"leg-a1--one.md": layoutNib,
	})

	app := checkAppPastTheGate(t, storeDir)
	out := captureStdout(t, func() {
		if _, err := runCheck(app); err != nil {
			t.Fatalf("runCheck error = %v", err)
		}
	})
	if strings.Contains(out, "All nib files loaded") {
		t.Errorf("the report claims every nib file loaded while telling the user the store is unmigrated:\n%s", out)
	}
	if !strings.Contains(out, "Store needs migration") {
		t.Errorf("the migration line is missing, so this test is no longer testing anything:\n%s", out)
	}

	// The checkmark must still appear on a store that genuinely loaded cleanly,
	// or the suppression has swallowed the signal instead of the contradiction.
	resetRootPersistentFlags()
	resetCheckFlags()
	current := writeStore(t, filepath.Join(t.TempDir(), "proj"), "nibs:\n  prefix: chk-\n", map[string]string{
		"chk-a1--one.md": layoutNib,
	})
	currentOut := captureStdout(t, func() {
		if _, err := runCheck(checkAppPastTheGate(t, current)); err != nil {
			t.Fatalf("runCheck error = %v", err)
		}
	})
	if !strings.Contains(currentOut, "All nib files loaded") {
		t.Errorf("a healthy store lost its load checkmark:\n%s", currentOut)
	}
}
