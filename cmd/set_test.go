package cmd

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/spf13/pflag"
)

// resetSetFlags resets ALL global flag variables for the set command.
// Cobra's StringArrayVar appends to slices across Execute() calls on the
// singleton rootCmd, so every global must be zeroed between tests.
// Also resets Cobra's "Changed" tracking on all flags to prevent stale state.
// Persistent-flag reset (--config, --nibs-path, plus their pflag Value/Changed
// bits) is delegated to the shared resetRootPersistentFlags helper.
// These tests must NOT use t.Parallel() — they share the rootCmd singleton.
func resetSetFlags() {
	setStatus = ""
	setType = ""
	setPriority = ""
	setEstimate = ""
	setTitle = ""
	setClear = nil
	setParent = ""
	setBlocking = nil
	setRemoveBlocking = nil
	setBlockedBy = nil
	setRemoveBlockedBy = nil
	setTag = nil
	setRemoveTag = nil
	setDocument = nil
	setRemoveDocument = nil
	setIfMatch = ""
	setJSON = false
	// Reset Cobra's "Changed" tracking on all flags
	setCmd.Flags().Visit(func(f *pflag.Flag) {
		f.Changed = false
	})
	resetRootPersistentFlags()
}

func TestResetSetFlagsClearsAllState(t *testing.T) {
	// Set every global to a non-zero value
	setStatus = "in-progress"
	setType = "bug"
	setPriority = "critical"
	setEstimate = "xl"
	setTitle = "dirty"
	setClear = []string{"priority"}
	setParent = "dirty"
	setBlocking = []string{"x"}
	setRemoveBlocking = []string{"y"}
	setBlockedBy = []string{"z"}
	setRemoveBlockedBy = []string{"w"}
	setTag = []string{"t"}
	setRemoveTag = []string{"r"}
	setDocument = []string{"d"}
	setRemoveDocument = []string{"e"}
	setIfMatch = "dirty"
	setJSON = true

	resetSetFlags()

	// Verify all are at zero values
	if setStatus != "" {
		t.Errorf("setStatus not reset: %q", setStatus)
	}
	if setType != "" {
		t.Errorf("setType not reset: %q", setType)
	}
	if setPriority != "" {
		t.Errorf("setPriority not reset: %q", setPriority)
	}
	if setEstimate != "" {
		t.Errorf("setEstimate not reset: %q", setEstimate)
	}
	if setTitle != "" {
		t.Errorf("setTitle not reset: %q", setTitle)
	}
	if setClear != nil {
		t.Errorf("setClear not reset: %v", setClear)
	}
	if setParent != "" {
		t.Errorf("setParent not reset: %q", setParent)
	}
	if setBlocking != nil {
		t.Errorf("setBlocking not reset: %v", setBlocking)
	}
	if setRemoveBlocking != nil {
		t.Errorf("setRemoveBlocking not reset: %v", setRemoveBlocking)
	}
	if setBlockedBy != nil {
		t.Errorf("setBlockedBy not reset: %v", setBlockedBy)
	}
	if setRemoveBlockedBy != nil {
		t.Errorf("setRemoveBlockedBy not reset: %v", setRemoveBlockedBy)
	}
	if setTag != nil {
		t.Errorf("setTag not reset: %v", setTag)
	}
	if setRemoveTag != nil {
		t.Errorf("setRemoveTag not reset: %v", setRemoveTag)
	}
	if setDocument != nil {
		t.Errorf("setDocument not reset: %v", setDocument)
	}
	if setRemoveDocument != nil {
		t.Errorf("setRemoveDocument not reset: %v", setRemoveDocument)
	}
	if setIfMatch != "" {
		t.Errorf("setIfMatch not reset: %q", setIfMatch)
	}
	if setJSON {
		t.Error("setJSON not reset")
	}
}

// writeSetNib creates a nibs dir with a single nib and returns (nibsDir, id).
func writeSetNib(t *testing.T, id, body string) (string, string) {
	t.Helper()
	nibsDir := filepath.Join(t.TempDir(), ".nibs")
	if err := os.MkdirAll(storeDataDir(nibsDir), 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\nversion: 2\ntitle: Test\nstatus: todo\ntype: task\n---\n" + body
	if err := os.WriteFile(dataPath(nibsDir, id+"--test.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return nibsDir, id
}

// writeIllegalReparentFixture creates a nibs dir holding one feature and one
// task — the smallest store in which a reparent is refused by the hierarchy
// rule with a non-empty repair hint, since a feature's only legal parent is an
// epic. It returns (nibsDir, featureID, taskID).
//
// The surfaces that must agree about that one refusal — `set`, `mv` and
// `query` — all build from this helper, so a difference between them can only
// come from the code under test and never from the fixture.
func writeIllegalReparentFixture(t *testing.T) (string, string, string) {
	t.Helper()
	nibsDir := filepath.Join(t.TempDir(), ".nibs")
	if err := os.MkdirAll(storeDataDir(nibsDir), 0755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"ft--feature.md": "---\nversion: 2\ntitle: Feature\nstatus: todo\ntype: feature\norder: a0\n---\n",
		"tk--task.md":    "---\nversion: 2\ntitle: Task\nstatus: todo\ntype: task\norder: b0\n---\n",
	}
	for name, content := range files {
		if err := os.WriteFile(dataPath(nibsDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return nibsDir, "ft", "tk"
}

// hierarchyEnvelope is the {code,message,allowedParentTypes} shape a HIERARCHY
// refusal emits, used to compare the envelopes `set`, `mv` and `query` build for
// one illegal reparent.
type hierarchyEnvelope struct {
	Error struct {
		Code               string   `json:"code"`
		Message            string   `json:"message"`
		AllowedParentTypes []string `json:"allowedParentTypes"`
	} `json:"error"`
}

// TestSetIllegalReparentIsHierarchy pins that `nibs set --parent` reports an
// illegal reparent as HIERARCHY carrying the parent types that WOULD be legal.
//
// The exit status is not the whole contract: HIERARCHY and VALIDATION_ERROR both
// exit 2, so a caller branching on $? alone cannot tell them apart. What differs
// is allowedParentTypes — the field that turns a refusal into a next action by
// naming the parent type that would be accepted. `nibs mv` has always provided
// it; `set` writes the same field through the same resolver, so reaching for the
// other verb must not yield a weaker answer.
func TestSetIllegalReparentIsHierarchy(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		t.Cleanup(resetSetFlags)
		resetSetFlags()
		nibsDir, featureID, taskID := writeIllegalReparentFixture(t)

		rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "set", featureID, "--parent", taskID, "--json"})
		var execErr error
		out := captureStdout(t, func() { execErr = rootCmd.Execute() })
		if execErr == nil {
			t.Fatalf("set --parent on an illegal reparent returned no error; out: %q", out)
		}
		if code := reportExitError(io.Discard, execErr); code != output.ExitValidation {
			t.Errorf("exit = %d, want %d (validation)", code, output.ExitValidation)
		}

		var env hierarchyEnvelope
		if err := json.Unmarshal([]byte(out), &env); err != nil {
			t.Fatalf("stdout is not a JSON error envelope: %v\nraw: %s", err, out)
		}
		if env.Error.Code != output.ErrHierarchy {
			t.Errorf("envelope code = %q, want %q", env.Error.Code, output.ErrHierarchy)
		}
		want := []string{"epic"}
		if !slices.Equal(env.Error.AllowedParentTypes, want) {
			t.Errorf("allowedParentTypes = %v, want %v", env.Error.AllowedParentTypes, want)
		}
		if !strings.Contains(env.Error.Message, "epic") {
			t.Errorf("message %q should name the allowed parent type", env.Error.Message)
		}
	})

	t.Run("text", func(t *testing.T) {
		t.Cleanup(resetSetFlags)
		resetSetFlags()
		nibsDir, featureID, taskID := writeIllegalReparentFixture(t)

		rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "set", featureID, "--parent", taskID})
		var execErr error
		out := captureStdout(t, func() { execErr = rootCmd.Execute() })
		if execErr == nil {
			t.Fatalf("set --parent on an illegal reparent returned no error; out: %q", out)
		}
		var ce *output.CodedError
		if !errors.As(execErr, &ce) {
			t.Fatalf("error = %T, want *output.CodedError", execErr)
		}
		if ce.Code != output.ErrHierarchy {
			t.Errorf("code = %q, want %q", ce.Code, output.ErrHierarchy)
		}
		if code := reportExitError(io.Discard, execErr); code != output.ExitValidation {
			t.Errorf("exit = %d, want %d (validation)", code, output.ExitValidation)
		}
	})
}

// TestSetTypeChangeOrphaningAChildIsHierarchy covers the other way `nibs set`
// can violate the hierarchy rule: changing a nib's TYPE so that an existing
// child's parent link becomes illegal. The graph layer wraps that refusal with
// the child's id (`fmt.Errorf("… child %s: %w")`), which is the case that
// decides how the shared envelope renders its message — carrying the wrapper's
// text keeps the id of the child that blocks the change, while the
// HierarchyError alone knows only the two types.
func TestSetTypeChangeOrphaningAChildIsHierarchy(t *testing.T) {
	t.Cleanup(resetSetFlags)
	resetSetFlags()

	nibsDir := filepath.Join(t.TempDir(), ".nibs")
	if err := os.MkdirAll(storeDataDir(nibsDir), 0755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"ep--epic.md":    "---\nversion: 2\ntitle: Epic\nstatus: todo\ntype: epic\norder: a0\n---\n",
		"ft--feature.md": "---\nversion: 2\ntitle: Feature\nstatus: todo\ntype: feature\nparent: ep\norder: a0\n---\n",
	}
	for name, content := range files {
		if err := os.WriteFile(dataPath(nibsDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// A feature may sit under an epic — not under a task.
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "set", "ep", "--type", "task", "--json"})
	var execErr error
	out := captureStdout(t, func() { execErr = rootCmd.Execute() })
	if execErr == nil {
		t.Fatalf("set --type on a change that orphans a child returned no error; out: %q", out)
	}
	if code := reportExitError(io.Discard, execErr); code != output.ExitValidation {
		t.Errorf("exit = %d, want %d (validation)", code, output.ExitValidation)
	}

	var env hierarchyEnvelope
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("stdout is not a JSON error envelope: %v\nraw: %s", err, out)
	}
	if env.Error.Code != output.ErrHierarchy {
		t.Errorf("envelope code = %q, want %q", env.Error.Code, output.ErrHierarchy)
	}
	want := []string{"epic"}
	if !slices.Equal(env.Error.AllowedParentTypes, want) {
		t.Errorf("allowedParentTypes = %v, want %v (the CHILD's legal parents)",
			env.Error.AllowedParentTypes, want)
	}
	if !strings.Contains(env.Error.Message, "ft") {
		t.Errorf("message %q should name the child that blocks the change", env.Error.Message)
	}
}

// TestSetStatusEchoesLeanCard verifies `set --status --json` echoes the lean
// {nib} card (get contract), NOT the full body/etag.
func TestSetStatusEchoesLeanCard(t *testing.T) {
	t.Cleanup(resetSetFlags)
	resetSetFlags()

	nibsDir, id := writeSetNib(t, "card-1", "## Notes\nsecret body text\n")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "set", id, "--status", "in-progress", "--json"})
	var execErr error
	out := captureStdout(t, func() { execErr = rootCmd.Execute() })
	if execErr != nil {
		t.Fatalf("set --status --json should succeed, got: %v", execErr)
	}

	var envelope map[string]any
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	nibObj, ok := envelope["nib"].(map[string]any)
	if !ok {
		t.Fatalf("expected {nib:{...}} echo, got: %s", out)
	}
	if nibObj["status"] != "in-progress" {
		t.Errorf("echoed status = %v, want in-progress", nibObj["status"])
	}
	// The lean card must not carry the body.
	if _, present := nibObj["body"]; present {
		t.Errorf("lean card should not include body, got: %s", out)
	}
	if strings.Contains(out, "secret body text") {
		t.Errorf("body content leaked into card echo: %s", out)
	}
}

// TestSetClearPriority verifies `--clear priority` removes the priority.
func TestSetClearPriority(t *testing.T) {
	t.Cleanup(resetSetFlags)
	resetSetFlags()

	nibsDir := filepath.Join(t.TempDir(), ".nibs")
	if err := os.MkdirAll(storeDataDir(nibsDir), 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\nversion: 2\ntitle: Test\nstatus: todo\ntype: task\npriority: critical\n---\n"
	if err := os.WriteFile(dataPath(nibsDir, "clr-1--test.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "set", "clr-1", "--clear", "priority"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("set --clear priority should succeed, got: %v", err)
	}

	data, _ := os.ReadFile(dataPath(nibsDir, "clr-1--test.md"))
	if strings.Contains(string(data), "priority: critical") {
		t.Errorf("priority should be cleared, got:\n%s", data)
	}
	if strings.Contains(string(data), "priority:") {
		t.Errorf("priority key should be gone, got:\n%s", data)
	}
}

// TestSetClearEstimate verifies `--clear estimate` removes the estimate.
func TestSetClearEstimate(t *testing.T) {
	t.Cleanup(resetSetFlags)
	resetSetFlags()

	nibsDir := filepath.Join(t.TempDir(), ".nibs")
	if err := os.MkdirAll(storeDataDir(nibsDir), 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\nversion: 2\ntitle: Test\nstatus: todo\ntype: task\nestimate: xl\n---\n"
	if err := os.WriteFile(dataPath(nibsDir, "clr-2--test.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "set", "clr-2", "--clear", "estimate"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("set --clear estimate should succeed, got: %v", err)
	}

	data, _ := os.ReadFile(dataPath(nibsDir, "clr-2--test.md"))
	if strings.Contains(string(data), "estimate: xl") {
		t.Errorf("estimate should be cleared, got:\n%s", data)
	}
}

// TestSetClearParent verifies `--clear parent` detaches the child from its parent.
func TestSetClearParent(t *testing.T) {
	t.Cleanup(resetSetFlags)
	resetSetFlags()

	nibsDir := filepath.Join(t.TempDir(), ".nibs")
	if err := os.MkdirAll(storeDataDir(nibsDir), 0755); err != nil {
		t.Fatal(err)
	}
	parent := "---\nversion: 2\ntitle: Parent\nstatus: todo\ntype: epic\n---\n"
	child := "---\nversion: 2\ntitle: Child\nstatus: todo\ntype: task\nparent: par-1\n---\n"
	if err := os.WriteFile(dataPath(nibsDir, "par-1--parent.md"), []byte(parent), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataPath(nibsDir, "chi-1--child.md"), []byte(child), 0644); err != nil {
		t.Fatal(err)
	}

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "set", "chi-1", "--clear", "parent"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("set --clear parent should succeed, got: %v", err)
	}

	data, _ := os.ReadFile(dataPath(nibsDir, "chi-1--child.md"))
	if strings.Contains(string(data), "parent: par-1") {
		t.Errorf("parent should be cleared, got:\n%s", data)
	}
}

// TestSetRejectsSetAndClearSameField verifies setting and clearing the same
// field in one invocation is a usage error.
func TestSetRejectsSetAndClearSameField(t *testing.T) {
	t.Cleanup(resetSetFlags)
	resetSetFlags()

	nibsDir := filepath.Join(t.TempDir(), ".nibs")
	if err := os.MkdirAll(storeDataDir(nibsDir), 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\nversion: 2\ntitle: Test\nstatus: todo\ntype: task\n---\n"
	if err := os.WriteFile(dataPath(nibsDir, "clr-3--test.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "set", "clr-3", "--priority", "high", "--clear", "priority", "--json"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for set-and-clear of same field, got nil")
	}
	var ce *output.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("error = %T, want *output.CodedError", err)
	}
	if output.ExitCode(ce.Code) != output.ExitValidation {
		t.Errorf("set-and-clear exit = %d, want %d (validation)", output.ExitCode(ce.Code), output.ExitValidation)
	}
}

// TestSetRejectsInvalidClearField verifies an unknown --clear field name is a
// usage error naming the allowed set.
func TestSetRejectsInvalidClearField(t *testing.T) {
	t.Cleanup(resetSetFlags)
	resetSetFlags()

	nibsDir, id := writeSetNib(t, "clr-4", "body\n")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "set", id, "--clear", "title", "--json"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid --clear field, got nil")
	}
	var ce *output.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("error = %T, want *output.CodedError", err)
	}
	if output.ExitCode(ce.Code) != output.ExitValidation {
		t.Errorf("invalid --clear exit = %d, want %d (validation)", output.ExitCode(ce.Code), output.ExitValidation)
	}
}

// TestUpdateAliasStillWorks verifies `update` remains a working alias of `set`.
func TestUpdateAliasStillWorks(t *testing.T) {
	t.Cleanup(resetSetFlags)
	resetSetFlags()

	nibsDir, id := writeSetNib(t, "alias-1", "body\n")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "update", id, "--status", "todo"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("update alias should still work, got: %v", err)
	}

	data, _ := os.ReadFile(dataPath(nibsDir, id+"--test.md"))
	if !strings.Contains(string(data), "status: todo") {
		t.Errorf("status should be todo via update alias, got:\n%s", data)
	}
}

// TestSetStaleIfMatchConflictCarriesCurrentEtag verifies a stale --if-match
// yields a CONFLICT (exit 4) whose --json envelope carries the server's current
// etag so an agent can retry with it.
func TestSetStaleIfMatchConflictCarriesCurrentEtag(t *testing.T) {
	t.Cleanup(resetSetFlags)
	resetSetFlags()

	nibsDir := filepath.Join(t.TempDir(), ".nibs")
	if err := os.MkdirAll(storeDataDir(nibsDir), 0755); err != nil {
		t.Fatal(err)
	}
	// version: 1 and the `# id` comment match Render() output so the etag we
	// compute from the file agrees with what the core computes.
	content := "---\n# cnf-1\nversion: 2\ntitle: Test\nstatus: todo\ntype: task\norder: a0\n---\n\n"
	if err := os.WriteFile(dataPath(nibsDir, "cnf-1--test.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Compute the etag before any mutation — this becomes the stale token.
	f, err := os.Open(dataPath(nibsDir, "cnf-1--test.md"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := nib.Parse(f)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	parsed.ID = "cnf-1"
	staleEtag := parsed.ETag()

	// Mutate once (no if-match) so the on-disk etag advances past staleEtag.
	resetSetFlags()
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "set", "cnf-1", "--status", "in-progress"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("priming mutation should succeed, got: %v", err)
	}

	// Now set with the stale etag — expect a CONFLICT carrying currentEtag.
	resetSetFlags()
	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "set", "cnf-1", "--status", "todo", "--if-match", staleEtag, "--json"})
	var execErr error
	out := captureStdout(t, func() { execErr = rootCmd.Execute() })

	if execErr == nil {
		t.Fatal("expected CONFLICT for stale --if-match, got nil")
	}
	var ce *output.CodedError
	if !errors.As(execErr, &ce) {
		t.Fatalf("error = %T, want *output.CodedError", execErr)
	}
	if output.ExitCode(ce.Code) != output.ExitConflict {
		t.Errorf("stale --if-match exit = %d, want %d (conflict)", output.ExitCode(ce.Code), output.ExitConflict)
	}

	var envelope struct {
		Error struct {
			Code        string `json:"code"`
			Message     string `json:"message"`
			CurrentEtag string `json:"currentEtag"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("conflict output is not valid JSON: %v\n%s", err, out)
	}
	if envelope.Error.Code != output.ErrConflict {
		t.Errorf("error code = %q, want %q", envelope.Error.Code, output.ErrConflict)
	}
	if envelope.Error.CurrentEtag == "" {
		t.Errorf("conflict envelope missing currentEtag: %s", out)
	}
	if envelope.Error.CurrentEtag == staleEtag {
		t.Errorf("currentEtag should differ from the stale token, both were %q", staleEtag)
	}

	// The nib must be unchanged by the rejected write (still in-progress).
	data, _ := os.ReadFile(dataPath(nibsDir, "cnf-1--test.md"))
	if !strings.Contains(string(data), "status: in-progress") {
		t.Errorf("rejected set must not have mutated the nib, got:\n%s", data)
	}
}

// writeSetNibWithStatus creates a nibs dir with a single nib carrying the given
// status and returns (nibsDir, id). writeSetNib always writes `todo`, which
// cannot express the closed starting point the reopen boundary needs.
func writeSetNibWithStatus(t *testing.T, id, status string) (string, string) {
	t.Helper()
	nibsDir := filepath.Join(t.TempDir(), ".nibs")
	if err := os.MkdirAll(storeDataDir(nibsDir), 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\nversion: 2\ntitle: Test\nstatus: " + status + "\ntype: task\n---\nbody\n"
	if err := os.WriteFile(dataPath(nibsDir, id+"--test.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return nibsDir, id
}

// TestSetRefusesEveryClosedStatus asserts `set -s <a closed status>` fails for
// every closed status, that the message names the `close` line to run instead,
// and that nothing was written. The cases come from ClosedStatusNames rather
// than a literal list, so the guard covers whatever config declares closed; the
// membership check below keeps that from silently becoming an empty loop.
func TestSetRefusesEveryClosedStatus(t *testing.T) {
	closed := config.Default().ClosedStatusNames()
	for _, want := range []string{"deferred", "completed", "scrapped"} {
		if !slices.Contains(closed, want) {
			t.Fatalf("test setup: %q is not among the closed statuses %v, so this test no longer covers it", want, closed)
		}
	}

	for _, status := range closed {
		t.Run(status, func(t *testing.T) {
			t.Cleanup(resetSetFlags)
			resetSetFlags()

			nibsDir, id := writeSetNibWithStatus(t, "cls-"+status, "in-progress")

			rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "set", id, "-s", status})
			err := rootCmd.Execute()
			if err == nil {
				t.Fatalf("set -s %s should be refused, got nil", status)
			}
			var ce *output.CodedError
			if !errors.As(err, &ce) {
				t.Fatalf("error = %T, want *output.CodedError", err)
			}
			if output.ExitCode(ce.Code) != output.ExitValidation {
				t.Errorf("set -s %s exit = %d, want %d (validation)", status, output.ExitCode(ce.Code), output.ExitValidation)
			}
			// The whole point of the refusal is to route the caller to `close`,
			// so the message must carry a line they can run — with this nib's id
			// and this status, not a generic mention of the verb.
			if wantCmd := "nibs close " + id + " --as " + status; !strings.Contains(err.Error(), wantCmd) {
				t.Errorf("set -s %s error should name %q, got: %s", status, wantCmd, err)
			}

			data, readErr := os.ReadFile(dataPath(nibsDir, id+"--test.md"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !strings.Contains(string(data), "status: in-progress") {
				t.Errorf("refused set -s %s still wrote the file:\n%s", status, data)
			}
		})
	}
}

// TestSetAllowsOpenStatusOnClosedNib pins the no-reopen-command boundary: the
// refusal reads the INCOMING status, never the nib's current one. There is no
// `reopen` verb, so `set -s todo` is how a closed nib returns to the board and
// must keep working — a guard that looked at the nib's own status instead would
// strand every closed nib.
func TestSetAllowsOpenStatusOnClosedNib(t *testing.T) {
	for _, from := range config.Default().ClosedStatusNames() {
		t.Run(from, func(t *testing.T) {
			t.Cleanup(resetSetFlags)
			resetSetFlags()

			nibsDir, id := writeSetNibWithStatus(t, "rop-"+from, from)

			rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "set", id, "-s", "todo"})
			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("set -s todo on a %s nib should be allowed, got: %v", from, err)
			}

			data, readErr := os.ReadFile(dataPath(nibsDir, id+"--test.md"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !strings.Contains(string(data), "status: todo") {
				t.Errorf("reopening a %s nib did not write todo:\n%s", from, data)
			}
		})
	}
}

// TestSetRefusalFollowsTheClosedFlag proves the refusal is derived from
// StatusConfig.Closed rather than from a list of status names kept in set.go.
// It swaps the vocabulary both ways: a newly declared closed status is refused
// with no edit to the command, and a status that stops being closed becomes
// settable again. Either half alone would still pass against a hardcoded list —
// the first if the list were "anything unrecognized", the second if it were
// empty.
func TestSetRefusalFollowsTheClosedFlag(t *testing.T) {
	t.Run("an added closed status is refused", func(t *testing.T) {
		withExtraStatus(t, config.StatusConfig{
			Name:        "abandoned",
			Color:       "gray",
			Role:        config.RoleParked,
			Description: "Guard status: closed, declared only for this test",
		})
		t.Cleanup(resetSetFlags)
		resetSetFlags()

		nibsDir, id := writeSetNibWithStatus(t, "drv-1", "in-progress")

		rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "set", id, "-s", "abandoned"})
		err := rootCmd.Execute()
		if err == nil {
			t.Fatal("set -s abandoned should be refused once abandoned is declared closed, got nil")
		}
		if wantCmd := "nibs close " + id + " --as abandoned"; !strings.Contains(err.Error(), wantCmd) {
			t.Errorf("error should name %q, got: %s", wantCmd, err)
		}
	})

	t.Run("a status that stops being closed is settable", func(t *testing.T) {
		statuses := make([]config.StatusConfig, len(config.DefaultStatuses))
		copy(statuses, config.DefaultStatuses)
		flipped := false
		for i := range statuses {
			if statuses[i].Name == "deferred" {
				statuses[i].Role = config.RoleOpen
				flipped = true
			}
		}
		if !flipped {
			t.Fatal("test setup: no `deferred` status to flip open, so this proves no derivation")
		}
		withStatuses(t, statuses)

		t.Cleanup(resetSetFlags)
		resetSetFlags()

		nibsDir, id := writeSetNibWithStatus(t, "drv-2", "in-progress")

		rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "set", id, "-s", "deferred"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("set -s deferred should be allowed while deferred is declared open, got: %v", err)
		}

		data, readErr := os.ReadFile(dataPath(nibsDir, id+"--test.md"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !strings.Contains(string(data), "status: deferred") {
			t.Errorf("expected status deferred, got:\n%s", data)
		}
	})
}

// TestSetInvalidStatusNamesOnlyOpenStatuses asserts the invalid-status message
// advertises exactly what `set -s` accepts. Naming a closed status there would
// offer a value the very next check refuses — one rejected command answered
// with a second — which is why the -s usage string lists the open ones only.
// Both expectations come from config, so the guard follows a vocabulary change
// instead of pinning today's words.
func TestSetInvalidStatusNamesOnlyOpenStatuses(t *testing.T) {
	t.Cleanup(resetSetFlags)
	resetSetFlags()

	cfg := config.Default()
	open, closed := cfg.OpenStatusNames(), cfg.ClosedStatusNames()
	if len(open) == 0 || len(closed) == 0 {
		t.Fatalf("declared groups are open=%v closed=%v; with either empty this test asserts nothing", open, closed)
	}

	nibsDir, id := writeSetNibWithStatus(t, "inv-1", "todo")

	rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "set", id, "-s", "bogus"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("set -s bogus should be rejected, got nil")
	}
	msg := err.Error()

	for _, name := range open {
		if !strings.Contains(msg, name) {
			t.Errorf("invalid-status message should name the accepted status %q, got: %s", name, msg)
		}
	}
	for _, name := range closed {
		if strings.Contains(msg, name) {
			t.Errorf("invalid-status message names %q, a status `set` refuses — got: %s", name, msg)
		}
	}
	// Naming no closed status is only half of it: an agent that wants one still
	// needs the verb that reaches it, or its second guess is as blind as the first.
	if !strings.Contains(msg, "nibs close") {
		t.Errorf("invalid-status message should point at `nibs close` for the closed statuses, got: %s", msg)
	}
}

// quotedNibsCommand matches a whole `nibs …` command quoted in an error
// message. Bare verb mentions (`set`, `close`) are deliberately not matched:
// they name a command, they are not one to run.
var quotedNibsCommand = regexp.MustCompile("`(nibs [^`]+)`")

// runQuotedCommand executes a command an error message quoted, against the
// test's own data directory. No quoted argument contains a space, so splitting
// on whitespace reproduces the argv a caller would have typed.
func runQuotedCommand(t *testing.T, nibsDir, quoted string) error {
	t.Helper()
	fields := strings.Fields(quoted)
	if len(fields) < 2 || fields[0] != "nibs" {
		t.Fatalf("not a nibs command: %q", quoted)
	}
	resetSetFlags()
	resetCloseFlags()
	rootCmd.SetArgs(append([]string{"--nibs-path", nibsDir}, fields[1:]...))
	return rootCmd.Execute()
}

// TestSetRefusalOnAClosedNibNamesAWorkingRoute pins the case that used to need
// special handling: a nib that is ALREADY closed, whose reason an agent is
// revising. `close` now accepts such a nib and appends another ## Summary entry,
// so the one command the refusal quotes is the whole route — where it once had
// to spell out a reopen-then-close detour, because suggesting `close` alone
// would have answered one error with a second, against a CLI contract that tells
// agents to stop on the first.
//
// The guard runs the command the message quotes and requires the nib to end up
// in the status that was asked for: a message whose route does not work fails
// here. Quoting more than one command fails here too — the detour coming back
// would mean `close` had started refusing again.
func TestSetRefusalOnAClosedNibNamesAWorkingRoute(t *testing.T) {
	closed := config.Default().ClosedStatusNames()
	if len(closed) < 2 {
		t.Fatalf("closed statuses are %v; with fewer than two there is no second reason to change to", closed)
	}

	for i, from := range closed {
		// Ask for a different reason than the nib carries — the case an agent
		// hits when it revises why the work ended.
		to := closed[(i+1)%len(closed)]
		t.Run(from+"-to-"+to, func(t *testing.T) {
			t.Cleanup(resetSetFlags)
			resetSetFlags()

			nibsDir, id := writeSetNibWithStatus(t, "rt-"+from, from)

			rootCmd.SetArgs([]string{"--nibs-path", nibsDir, "set", id, "-s", to})
			err := rootCmd.Execute()
			if err == nil {
				t.Fatalf("set -s %s should be refused, got nil", to)
			}

			quoted := quotedNibsCommand.FindAllStringSubmatch(err.Error(), -1)
			if len(quoted) != 1 {
				t.Fatalf("refusal should quote exactly one command (`close` takes an already-closed nib); got %d in: %s", len(quoted), err)
			}

			withStdin(t, "Reason revised.\n")
			if runErr := runQuotedCommand(t, nibsDir, quoted[0][1]); runErr != nil {
				t.Fatalf("the refusal quoted `%s`, which failed: %v", quoted[0][1], runErr)
			}

			data, readErr := os.ReadFile(dataPath(nibsDir, id+"--test.md"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !strings.Contains(string(data), "status: "+to) {
				t.Errorf("running the quoted route did not reach %s, got:\n%s", to, data)
			}
			if !strings.Contains(string(data), "Reason revised.") {
				t.Errorf("the quoted route should have recorded the summary, got:\n%s", data)
			}
		})
	}
}
