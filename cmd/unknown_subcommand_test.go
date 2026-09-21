package cmd

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/testdata/fixtures"
)

// The `nibs area` and `nibs config` groups take no verb of their own, and until
// codedUnknownSubcommand was wired they answered an unrecognized one with their
// help text and exit 0 — so `nibs area rename …`, a verb retired in faeb903,
// read as success to every caller that checks the status. These guards hold the
// refusal, its wording, and the two things the refusal must not take with it:
// the bare invocation's help, and every real verb below the group.

// unknownVerbStore returns a throwaway copy of the sample fixture's store, with
// the whole command tree reset around the test: these rows run several commands
// against the one rootCmd singleton.
func unknownVerbStore(t *testing.T) string {
	t.Helper()
	resetCommandTreeFlags(rootCmd)
	t.Cleanup(func() {
		resetCommandTreeFlags(rootCmd)
		rootCmd.SetArgs(nil)
	})
	return filepath.Join(fixtures.CopySampleProject(t), ".nibs")
}

// runGroupCmd runs one invocation against nibsPath with every flag back at its
// default, so a row can run a refusal and then a real verb.
func runGroupCmd(t *testing.T, nibsPath string, args ...string) (string, error) {
	t.Helper()
	resetCommandTreeFlags(rootCmd)
	return runRootWith(t, append([]string{"--nibs-path", nibsPath}, args...)...)
}

// codedErrorFrom asserts err is a *output.CodedError and returns it.
func codedErrorFrom(t *testing.T, err error) *output.CodedError {
	t.Helper()
	if err == nil {
		t.Fatal("expected a refusal, got nil (the group printed help and exited 0)")
	}
	var ce *output.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("error = %T (%v), want *output.CodedError", err, err)
	}
	return ce
}

// TestUnknownSubcommandExitsValidation is the exit-status half: an unrecognized
// verb on either group is a malformed invocation (exit 2), not a success and
// not a missing object (exit 3).
func TestUnknownSubcommandExitsValidation(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string // the whole message
	}{
		{"area rejects an invented verb", []string{"area", "bogus"},
			`unknown command "bogus" for "nibs area"`},
		{"area rejects the retired rename verb and its operands", []string{"area", "rename", "web", "frontend"},
			`unknown command "rename" for "nibs area"`},
		{"config rejects an invented verb", []string{"config", "bogus"},
			`unknown command "bogus" for "nibs config"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsPath := unknownVerbStore(t)
			_, err := runGroupCmd(t, nibsPath, tt.args...)
			ce := codedErrorFrom(t, err)
			if ce.Code != output.ErrValidation {
				t.Errorf("code = %q, want %q", ce.Code, output.ErrValidation)
			}
			if got := output.ExitCode(ce.Code); got != output.ExitValidation {
				t.Errorf("exit = %d, want %d", got, output.ExitValidation)
			}
			if ce.Msg != tt.want {
				t.Errorf("message = %q, want %q", ce.Msg, tt.want)
			}
		})
	}
}

// TestUnknownSubcommandMessage pins the wording, because the suggestion is the
// thing a reader acts on.
//
// The rows are what cobra's matcher actually returns, measured rather than
// assumed: it offers a name within Levenshtein distance 2 OR carrying the typed
// text as a prefix, so several names can qualify at once ("lst" is one edit from
// "list" and two from "set") and an unrelated word qualifies for none. The
// retired "rename" is that last case, which is why the message has to read
// correctly with no suggestion at all.
func TestUnknownSubcommandMessage(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"one suggestion", []string{"area", "sett"},
			`unknown command "sett" for "nibs area"; did you mean "set"?`},
		{"one suggestion across a hyphen", []string{"config", "setprefix"},
			`unknown command "setprefix" for "nibs config"; did you mean "set-prefix"?`},
		{"several suggestions are joined", []string{"area", "lst"},
			`unknown command "lst" for "nibs area"; did you mean "list" or "set"?`},
		{"no suggestion leaves a complete sentence", []string{"area", "rename"},
			`unknown command "rename" for "nibs area"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsPath := unknownVerbStore(t)
			_, err := runGroupCmd(t, nibsPath, tt.args...)
			ce := codedErrorFrom(t, err)
			if ce.Msg != tt.want {
				t.Errorf("message = %q, want %q", ce.Msg, tt.want)
			}
		})
	}
}

// TestBareGroupPrintsHelp holds the behavior the refusal must not cost: the
// group with no verb prints its help and exits 0.
//
// --nibs-path names an empty directory on purpose. Printing help needs no store,
// and making the group runnable is what lets its Args validator fire at all —
// which also puts it through root's PersistentPreRunE, where a store WOULD be
// resolved unless commandNeedsNoStore exempts it. A directory that resolves to
// no store makes that exemption the difference between exit 0 and a discovery
// error, so this row fails if the exemption goes away.
//
// The `help` rows ask the same question the other way round. Cobra builds its
// help command on the ROOT alone, so `nibs area help` never resolves to a
// command: it reaches the group's Args validator as a positional, and that
// validator accepting it is the only thing between a help request and a refusal
// no suggestion can soften.
func TestBareGroupPrintsHelp(t *testing.T) {
	for _, group := range []string{"area", "config"} {
		for _, args := range [][]string{{group}, {group, "help"}} {
			t.Run(strings.Join(args, " "), func(t *testing.T) {
				resetCommandTreeFlags(rootCmd)
				t.Cleanup(func() {
					resetCommandTreeFlags(rootCmd)
					rootCmd.SetArgs(nil)
				})
				typed := "nibs " + strings.Join(args, " ")
				out, err := runRootWith(t, append([]string{"--nibs-path", t.TempDir()}, args...)...)
				if err != nil {
					t.Fatalf("`%s` = %v, want nil (help, exit 0)", typed, err)
				}
				if !strings.Contains(out, "Available Commands:") {
					t.Errorf("`%s` printed no command list:\n%s", typed, out)
				}
				if !strings.Contains(out, "Use \"nibs "+group+" [command] --help\"") {
					t.Errorf("`%s` printed no help footer:\n%s", typed, out)
				}
			})
		}
	}
}

// TestGroupVerbsStillRun holds the other half: a validator on the group runs
// only when the group itself is the resolved command, so every verb below it is
// untouched. Run for real against a throwaway store rather than asserted from
// the resolution rule.
func TestGroupVerbsStillRun(t *testing.T) {
	nibsPath := unknownVerbStore(t)
	steps := []struct {
		name string
		args []string
	}{
		{"area list", []string{"area", "list"}},
		{"area add", []string{"area", "add", "qa", "--description", "Test and release engineering"}},
		{"area set", []string{"area", "set", "qa", "--name", "quality"}},
		{"area rm", []string{"area", "rm", "quality"}},
		{"config set-prefix", []string{"config", "set-prefix", "zz-", "--dry-run"}},
	}
	for _, step := range steps {
		out, err := runGroupCmd(t, nibsPath, step.args...)
		if err != nil {
			t.Fatalf("`nibs %s` = %v, want nil\n%s", strings.Join(step.args, " "), err, out)
		}
		if strings.Contains(out, "unknown command") {
			t.Fatalf("`nibs %s` was answered as an unknown command:\n%s", strings.Join(step.args, " "), out)
		}
	}
}
