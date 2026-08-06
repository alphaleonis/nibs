package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/output"
	"github.com/spf13/cobra"
)

// TestArgCountErrorRoutesThroughJSONEnvelope is the end-to-end guard for
// nibs-2isj: an arg-count (usage) violation on a --json-bearing command must
// surface as the {"error":{code,message}} envelope on stdout AND exit 2
// (output.ExitValidation), exactly like a value-validation error.
//
// This bites against the pre-sweep code: with stock cobra.NoArgs /
// MaximumNArgs the violation prints a plain-text line and the returned error is
// uncoded, so reportExitError maps it to exit 1 (ExitError) and no JSON
// envelope reaches stdout — both the json.Unmarshal and the exit-code
// assertions fail.
func TestArgCountErrorRoutesThroughJSONEnvelope(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		reset     func()
		wantInMsg string // if set, the envelope message must contain this substring
	}{
		// new: MaximumNArgs(1) — two positional titles is one too many.
		{"new rejects a second title", []string{"new", "a", "b", "--json"}, resetNewFlags, ""},
		// list: NoArgs — a stray positional is rejected.
		{"list rejects a positional", []string{"list", "x", "--json"}, resetListFlags, ""},
		// show is an alias of get (MinimumNArgs(1)); a too-few (0-arg) call must
		// name the alias the user typed ("nibs show"), not the canonical "nibs get"
		// — the invokedName behavior this sweep added. Exercised through the real
		// rootCmd.Execute() path so cobra populates CalledAs().
		{"show alias reports the alias, not the canonical name", []string{"show", "--json"}, resetGetFlags, "nibs show"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(resetRootPersistentFlags)
			t.Cleanup(func() { rootCmd.SetArgs(nil) })
			t.Cleanup(tt.reset)
			tt.reset()

			rootCmd.SetArgs(tt.args)
			var execErr error
			out := captureStdout(t, func() { execErr = rootCmd.Execute() })
			if execErr == nil {
				t.Fatal("expected an arg-count error, got nil")
			}

			// stdout must carry the JSON error envelope (not plain text / empty).
			var env struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(out), &env); err != nil {
				t.Fatalf("arg-count error did not emit the {error} envelope on stdout: %v\nstdout=%q", err, out)
			}
			if env.Error.Code != output.ErrValidation {
				t.Errorf("envelope code = %q, want %q", env.Error.Code, output.ErrValidation)
			}
			if env.Error.Message == "" {
				t.Error("envelope message is empty")
			}
			if tt.wantInMsg != "" && !strings.Contains(env.Error.Message, tt.wantInMsg) {
				t.Errorf("envelope message = %q, want it to contain %q (invokedName should name the invoked alias)", env.Error.Message, tt.wantInMsg)
			}

			// The returned error carries the code so the boundary exits 2.
			var ce *output.CodedError
			if !errors.As(execErr, &ce) {
				t.Fatalf("error = %T, want *output.CodedError", execErr)
			}
			var stderr bytes.Buffer
			if code := reportExitError(&stderr, execErr); code != output.ExitValidation {
				t.Errorf("exit code = %d, want %d (validation)", code, output.ExitValidation)
			}
		})
	}
}

// TestCodedArgsValidatorsAreWired walks every command whose stock cobra Args
// validator was replaced with a coded equivalent (nibs-2isj sweep), plus query's
// bespoke coded validator (archive's is covered separately by archive_test.go).
// For each:
//   - a violating arg count returns a coded validation error (exit 2), mirroring
//     the archive_test.go pattern;
//   - a valid arg count returns nil (no false rejection); and
//   - the validator is wired to its OWN --json var — a --json-bearing command
//     flips its own flag true and must then return the REPORTED form (the
//     {error} envelope on stdout, ce.Reported set), which only happens when
//     jsonModeOf reads this command's own flag. A validator pointed at a
//     different (still-false) json var yields the non-reported text form with
//     empty stdout, so both the Reported and envelope assertions fail — catching
//     the copy-paste swap the sweep is most exposed to.
//
// It bites against the pre-sweep code: a command still on cobra.NoArgs /
// ExactArgs / MinimumNArgs / MaximumNArgs returns a plain (uncoded) error on a
// violation, so errors.As(..., *output.CodedError) fails.
func TestCodedArgsValidatorsAreWired(t *testing.T) {
	tests := []struct {
		name    string
		cmd     *cobra.Command
		bad     []string // a violating arg count
		good    []string // a valid arg count (must be accepted)
		jsonVar *bool    // the command's own --json flag var, or nil if it has none
	}{
		// 9 NoArgs (4 --json-bearing, 5 without)
		{"version", versionCmd, []string{"x"}, nil, nil},
		{"serve", serveCmd, []string{"x"}, nil, nil},
		{"cheat", cheatCmd, []string{"x"}, nil, nil},
		{"check", checkCmd, []string{"x"}, nil, &checkJSON},
		{"list", listCmd, []string{"x"}, nil, &listJSON},
		{"init", initCmd, []string{"x"}, nil, &initJSON},
		{"tui", tuiCmd, []string{"x"}, nil, nil},
		{"prime", primeCmd, []string{"x"}, nil, nil},
		{"roadmap", roadmapCmd, []string{"x"}, nil, &roadmapJSON},
		// 6 ExactArgs(1)
		{"config set-prefix (too few)", configSetPrefixCmd, nil, []string{"pfx"}, &setPrefixJSON},
		{"config set-prefix (too many)", configSetPrefixCmd, []string{"a", "b"}, []string{"pfx"}, &setPrefixJSON},
		{"plan", planCmd, nil, []string{"x"}, &planJSON},
		{"body", bodyCmd, nil, []string{"x"}, &bodyJSON},
		{"close", closeCmd, nil, []string{"x"}, &closeJSON},
		{"rel", relCmd, nil, []string{"x"}, &relJSON},
		{"set", setCmd, nil, []string{"x"}, &setJSON},
		// 4 MinimumNArgs(1)
		{"mv", mvCmd, nil, []string{"x"}, &mvJSON},
		{"rm", rmCmd, nil, []string{"x"}, &rmJSON},
		{"delete", deleteCmd, nil, []string{"x"}, &deleteJSON},
		{"show", getCmd, nil, []string{"x"}, &getJSON},
		// 3 MaximumNArgs(1)
		{"catalog", catalogCmd, []string{"a", "b"}, []string{"x"}, &catalogJSON},
		{"new", newCmd, []string{"a", "b"}, []string{"x"}, &newJSON},
		{"context", contextCmd, []string{"a", "b"}, []string{"x"}, &contextJSON},
		// bespoke coded validator (at most 1 arg, --schema bypass): query.
		{"query", queryCmd, []string{"a", "b"}, []string{"x"}, &queryJSON},
	}
	// Make the swap-detection premise self-contained: force every command's
	// --json var false up front, regardless of other tests' cleanup discipline,
	// so a validator wired to the WRONG var reliably reads false (non-reported)
	// and trips the checks below.
	for _, tt := range tests {
		if tt.jsonVar != nil {
			*tt.jsonVar = false
		}
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.cmd.Args == nil {
				t.Fatalf("%s has no Args validator", tt.name)
			}
			// Flip THIS command's own --json flag true so the reported envelope
			// is expected to reach stdout. A validator wired to a different json
			// var would leave that var false and print nothing — the checks below
			// then catch the swap. Reset after the case.
			if tt.jsonVar != nil {
				*tt.jsonVar = true
				t.Cleanup(func() { *tt.jsonVar = false })
			}

			// Violating arity → coded validation error, captured so the envelope
			// print is observed (and doesn't leak to the test's stdout).
			var badErr error
			out := captureStdout(t, func() { badErr = tt.cmd.Args(tt.cmd, tt.bad) })
			if badErr == nil {
				t.Fatalf("%s accepted a violating arg count %v", tt.name, tt.bad)
			}
			var ce *output.CodedError
			if !errors.As(badErr, &ce) {
				t.Fatalf("%s: error = %T, want *output.CodedError (stock cobra validator?)", tt.name, badErr)
			}
			if output.ExitCode(ce.Code) != output.ExitValidation {
				t.Errorf("%s: exit = %d, want %d (validation)", tt.name, output.ExitCode(ce.Code), output.ExitValidation)
			}

			if tt.jsonVar != nil {
				// --json mode: must be the reported form with the {error}
				// envelope on stdout. A validator reading the wrong (false) json
				// var yields a non-reported error and empty stdout — both fail.
				if !ce.Reported {
					t.Errorf("%s: coded error not Reported under its own --json=true; validator likely reads the wrong --json var", tt.name)
				}
				var env struct {
					Error struct {
						Code string `json:"code"`
					} `json:"error"`
				}
				if err := json.Unmarshal([]byte(out), &env); err != nil {
					t.Fatalf("%s: --json arity error did not emit the {error} envelope on stdout: %v\nstdout=%q", tt.name, err, out)
				}
				if env.Error.Code != output.ErrValidation {
					t.Errorf("%s: envelope code = %q, want %q", tt.name, env.Error.Code, output.ErrValidation)
				}
			} else {
				// No --json flag: text-mode form (non-reported), nothing on stdout.
				if ce.Reported {
					t.Errorf("%s: coded error should not be Reported for a command with no --json flag", tt.name)
				}
				if out != "" {
					t.Errorf("%s: non-json validator wrote to stdout: %q", tt.name, out)
				}
			}

			// Valid arity → no error (no false rejection).
			if err := tt.cmd.Args(tt.cmd, tt.good); err != nil {
				t.Errorf("%s: valid arity %v rejected: %v", tt.name, tt.good, err)
			}
		})
	}
}

// TestEveryLeafCommandUsesCodedArityValidator is the structural drift guard: it
// walks the whole command tree and, for each leaf command, asserts an
// over-arity call returns EITHER nil (the command accepts arbitrary arity) OR a
// coded *output.CodedError — never a plain cobra errorString. A stock
// cobra.NoArgs/ExactArgs/MaximumNArgs validator returns a bare fmt.Errorf on an
// over-arity call, so a command that regresses to (or is added with) a stock
// validator trips this test — the exact drift that let `query` slip past the
// hardcoded table above. cobra's own help/completion/__complete* commands
// legitimately use stock validators and are excluded.
//
// Note: the over-arity slice catches NoArgs/ExactArgs(n)/MaximumNArgs(n) drift;
// it cannot catch MinimumNArgs drift (extra args satisfy a minimum), which the
// per-command table above covers explicitly.
func TestEveryLeafCommandUsesCodedArityValidator(t *testing.T) {
	isBuiltin := func(c *cobra.Command) bool {
		return c.Hidden || c.Name() == "help" || c.Name() == "completion"
	}
	over := []string{"x", "y", "z"}
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		for _, child := range cmd.Commands() {
			if isBuiltin(child) {
				continue
			}
			if child.HasSubCommands() {
				walk(child)
				continue
			}
			var err error
			_ = captureStdout(t, func() { err = child.ValidateArgs(over) })
			if err == nil {
				continue // accepts arbitrary arity — fine
			}
			var ce *output.CodedError
			if !errors.As(err, &ce) {
				t.Errorf("%s: over-arity Args error = %T (%v), want *output.CodedError; a stock cobra validator leaked a plain error — wire it to a coded* helper or a cmdError-routing validator (see cmd/args.go)",
					child.CommandPath(), err, err)
			}
		}
	}
	walk(rootCmd)
}

// TestCodedArgsHelpersJSONModePointer pins the nil-safe pointer contract of the
// coded validators: a nil pointer yields a non-reported (plain-text) coded
// error; a pointer to true yields the reported {error} envelope. Both carry the
// validation code so the boundary exits 2 either way.
func TestCodedArgsHelpersJSONModePointer(t *testing.T) {
	cmd := &cobra.Command{Use: "demo"}

	// nil pointer → text mode: non-reported coded error, nothing on stdout.
	var nilErr error
	out := captureStdout(t, func() { nilErr = codedNoArgs(nil)(cmd, []string{"x"}) })
	if out != "" {
		t.Errorf("nil json pointer should not print to stdout, got %q", out)
	}
	var nilCE *output.CodedError
	if !errors.As(nilErr, &nilCE) || nilCE.Code != output.ErrValidation {
		t.Fatalf("nil pointer: error = %v, want coded VALIDATION_ERROR", nilErr)
	}
	if nilCE.Reported {
		t.Error("nil (text) pointer should yield a non-reported coded error")
	}

	// pointer to true → json mode: reported envelope on stdout.
	jsonMode := true
	var jsonErr error
	out = captureStdout(t, func() { jsonErr = codedExactArgs(&jsonMode, 1)(cmd, nil) })
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("json pointer should print the {error} envelope: %v\nstdout=%q", err, out)
	}
	if env.Error.Code != output.ErrValidation {
		t.Errorf("envelope code = %q, want %q", env.Error.Code, output.ErrValidation)
	}
	var jsonCE *output.CodedError
	if !errors.As(jsonErr, &jsonCE) || !jsonCE.Reported {
		t.Fatalf("json pointer should yield a reported coded error, got %v", jsonErr)
	}
}
