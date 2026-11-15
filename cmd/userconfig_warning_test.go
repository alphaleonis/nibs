package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/testsupport/userconfigtest"
	"github.com/spf13/cobra"
)

// runRootCapturingStderr runs the root command with args and returns what it
// wrote to stdout and to the command's stderr.
func runRootCapturingStderr(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	resetRootPersistentFlags()
	t.Cleanup(resetRootPersistentFlags)
	var errBuf bytes.Buffer
	rootCmd.SetErr(&errBuf)
	t.Cleanup(func() { rootCmd.SetErr(nil); rootCmd.SetArgs(nil) })
	rootCmd.SetArgs(args)
	stdout = captureStdout(t, func() { err = rootCmd.Execute() })
	return stdout, errBuf.String(), err
}

const notLoaded = "did not load, so its settings are ignored"

func TestAUserConfigThatDidNotLoadWarnsOnceOnStderr(t *testing.T) {
	tests := []struct {
		name, text, cause string
	}{
		{"syntax error", "nibs:\n  id_length: [\n", "line 2"},
		// yaml.v3 quotes a value of the wrong type, so the file's bytes reach the cause.
		{"terminal controls in the cause", "nibs: \"\\e[2J\\e]0;owned\\a\"\n", "cannot unmarshal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userPath := userconfigtest.WriteUserConfig(t, tt.text)
			nibsDir := setupListCobraTest(t, map[string]string{
				"aa--one.md": "---\nversion: 2\ntitle: One\nstatus: todo\ntype: task\n---\n",
			})

			stdout, stderr, err := runRootCapturingStderr(t, "--nibs-path", nibsDir, "list", "--json")
			if err != nil {
				t.Fatalf("list --json failed: %v", err)
			}
			if !json.Valid([]byte(stdout)) || strings.Contains(stdout, notLoaded) {
				t.Errorf("stdout is not clean JSON:\n%s", stdout)
			}
			want := "warning: the user config " + userPath + " " + notLoaded + ": "
			if !strings.HasPrefix(stderr, want) || strings.Count(stderr, "\n") != 1 || !strings.Contains(stderr, tt.cause) {
				t.Errorf("stderr = %q, want one line starting %q and naming %q", stderr, want, tt.cause)
			}
			if strings.ContainsAny(stderr, "\x1b\a") {
				t.Errorf("stderr = %q, want no terminal controls from the file", stderr)
			}
		})
	}
}

func TestCommandsThatLoadNoProjectConfigStillReportTheUserConfig(t *testing.T) {
	userconfigtest.WriteUserConfig(t, "nibs: [\n")

	t.Run("init", func(t *testing.T) {
		_, nibsPath := setupInitTest(t, "proj")
		_, stderr, err := runRootCapturingStderr(t, "--nibs-path", nibsPath, "init", "--prefix", "p")
		if err != nil {
			t.Fatalf("init failed: %v", err)
		}
		if strings.Count(stderr, notLoaded) != 1 {
			t.Errorf("stderr = %q, want one warning", stderr)
		}
	})

	t.Run("migrate", func(t *testing.T) {
		nibsDir := setupMigrateStore(t, map[string]string{
			"aa--one.md": "---\nversion: 2\ntitle: One\nstatus: todo\ntype: task\n---\n",
		})
		_, stderr, err := runRootCapturingStderr(t, "--nibs-path", nibsDir, "migrate")
		if err != nil {
			t.Fatalf("migrate failed: %v", err)
		}
		if strings.Count(stderr, notLoaded) != 1 {
			t.Errorf("stderr = %q, want one warning", stderr)
		}
	})

	t.Run("version", func(t *testing.T) {
		_, stderr, err := runRootCapturingStderr(t, "version")
		if err != nil {
			t.Fatalf("version failed: %v", err)
		}
		if strings.Contains(stderr, notLoaded) {
			t.Errorf("stderr = %q, want no warning from a command that loads no config", stderr)
		}
	})
}

// The TUI shows the failure in its status line instead, and a completion
// request runs on every TAB press.
func TestTheTUIAndCompletionDoNotWarnOnStderr(t *testing.T) {
	if reportsUserConfigFailureOnStderr(tuiCmd) {
		t.Error("tui reports the user config on stderr")
	}
	complete := &cobra.Command{Use: cobra.ShellCompRequestCmd}
	rootCmd.AddCommand(complete)
	t.Cleanup(func() { rootCmd.RemoveCommand(complete) })
	if reportsUserConfigFailureOnStderr(complete) {
		t.Error("a completion request reports the user config on stderr")
	}
	if !reportsUserConfigFailureOnStderr(listCmd) {
		t.Error("premise failed: list does not report the user config on stderr")
	}
}
