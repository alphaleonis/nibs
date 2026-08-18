package scripts

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The phrase the guard looks for, kept out of this file's literals so that
// editing these tests is not itself a command the guard would object to when a
// tool call quotes a line of them back.
const goTest = "go" + " test"

// guardShim locates the PreToolUse guard's entry point, skipping when the
// environment cannot run it. The shim warns and allows without a Python
// interpreter, so a machine with none has nothing to assert about.
func guardShim(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the entry point is POSIX sh; Windows runs it through Git Bash, not exercised here")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}
	// Probe by running, as the shim does: on Windows `python3` is an App
	// Execution Alias that exists on PATH and refuses to run.
	usable := false
	for _, py := range []string{"python3", "python"} {
		if exec.Command(py, "-c", "").Run() == nil {
			usable = true
			break
		}
	}
	if !usable {
		t.Skip("no working Python interpreter; the shim warns and allows without one")
	}

	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "guard-go-test.sh")
}

// guardVerdict feeds one PreToolUse payload to the guard and reports whether it
// blocked, along with what it wrote to stderr.
//
// Exit 1 means the shim could not run the guard at all — a Python syntax error
// reaches the test that way — so it is a failure rather than a quiet "allowed".
func guardVerdict(t *testing.T, shim string, payload any) (bool, string) {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}

	cmd := exec.Command("sh", shim)
	cmd.Stdin = strings.NewReader(string(encoded))
	var stderr strings.Builder
	cmd.Stderr = &stderr
	cmd.Env = append(os.Environ(), "NIBS_ALLOW_BARE_GO_TEST=")
	err = cmd.Run()

	exit := 0
	if ee, ok := err.(*exec.ExitError); ok {
		exit = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running guard: %v\n%s", err, stderr.String())
	}
	if exit != 0 && exit != 2 {
		t.Fatalf("guard exited %d, want 0 (allow) or 2 (block)\n%s", exit, stderr.String())
	}
	return exit == 2, stderr.String()
}

// bashCommand wraps a command string in the PreToolUse payload shape.
func bashCommand(command string) map[string]any {
	return map[string]any{
		"tool_name":  "Bash",
		"tool_input": map[string]string{"command": command},
	}
}

// TestGuardSeparatesInvocationFromMention drives the guard over both directions
// at once: every shape that runs the tests must be refused, and every shape that
// only writes about them must pass. The two halves are one table because the
// value of either depends on the other holding — a guard that stops firing and a
// guard that fires on a commit message are both broken.
func TestGuardSeparatesInvocationFromMention(t *testing.T) {
	shim := guardShim(t)

	tests := []struct {
		name    string
		command string
		block   bool
	}{
		// Invocations.
		{"plain", goTest + " ./...", true},
		{"after &&", "cd internal && " + goTest + " ./...", true},
		{"after ;", "make build; " + goTest + " .", true},
		{"after ||", "make build || " + goTest + " ./...", true},
		{"after a pipe", "echo ./... | " + goTest, true},
		{"on its own line", "cd internal\n" + goTest + " ./...", true},
		{"extra whitespace", "go  test ./...", true},
		{"in a command substitution", "echo $(" + goTest + " ./...)", true},
		{"in backticks", "echo `" + goTest + " ./...`", true},
		{"in a subshell", "(cd internal; " + goTest + " ./...)", true},
		{"in a brace group", "{ " + goTest + " ./...; }", true},
		{"behind env assignments", "GOFLAGS=-count=1 " + goTest + " ./...", true},
		{"behind a timeout wrapper", "timeout 30 " + goTest + " ./internal/nibcore", true},
		{"behind sudo", "sudo " + goTest + " ./...", true},
		{"behind a loop keyword", "for d in a b; do " + goTest + " $d; done", true},
		{"behind xargs", "echo ./... | xargs " + goTest, true},
		// A redirection may precede the command word instead of following it,
		// and `> file cmd args` runs cmd args all the same.
		{"behind a leading redirect", "> /tmp/out " + goTest + " ./...", true},
		{"behind a redirect with no space", ">/tmp/out " + goTest + " ./...", true},
		{"behind an appending redirect after a separator",
			"cd internal && >> /tmp/out " + goTest + " ./...", true},
		{"behind a numbered redirect", "2> /tmp/err " + goTest + " ./internal/nib", true},
		{"behind a merged redirect", "&> /tmp/out " + goTest + " ./...", true},
		{"behind a clobbering redirect", ">| /tmp/out " + goTest + " ./...", true},
		{"behind an input redirect", "< /dev/null " + goTest + " ./...", true},
		{"behind a duplicated descriptor", "2>&1 " + goTest + " ./...", true},
		{"behind a redirect and a wrapper", "> /tmp/out timeout 30 " + goTest + " ./...", true},
		// A quoted argument is data for most commands, but not for one that
		// hands it to an interpreter.
		{"quoted argument of sh -c", `sh -c "` + goTest + ` ./..."`, true},
		{"quoted argument of bash -c", "bash -c '" + goTest + " ./...'", true},
		{"quoted argument of eval", "eval '" + goTest + " ./...'", true},
		{"separator inside sh -c", `sh -c "cd internal && ` + goTest + ` ./..."`, true},
		// The guard's doc leans on this case when it explains why a container
		// runner's *trailing* command form is left open: the string form of the
		// same thing is not.
		{"quoted argument of a container runner", "docker run img sh -c '" + goTest + " ./...'", true},
		// Likewise a heredoc body, when something in the pipeline runs it.
		{"heredoc read by bash", "bash <<'EOF'\n" + goTest + " ./...\nEOF", true},
		{"heredoc piped to sh", "cat <<'EOF' | sh\n" + goTest + " ./...\nEOF", true},
		// An unterminated quote leaves the scan guessing where the span ends,
		// and bash's answer differs ($'...' reads \' as an escaped quote, this
		// scanner as the closing one). The scan is dropped rather than trusted,
		// so what follows the guess is still matched.
		{"after an ANSI-C quoted word", `echo $'it\'s fine'; ` + goTest + " ./...", true},
		// A substitution runs wherever it appears, quoted or not.
		{"substitution inside a message", `git commit -m "output: $(` + goTest + ` ./...)"`, true},
		{"substitution inside an expanding heredoc", "git commit -F - <<EOF\nout: $(" + goTest + " ./...)\nEOF", true},

		// Mentions. These are the cases this guard used to refuse.
		{"heredoc commit message", "git commit -F - <<'EOF'\nfix: something\n\n" +
			goTest + " discards a passing package's stdout, so -v could not carry this.\nEOF", false},
		{"heredoc commit message, expanding delimiter", "git commit -F - <<EOF\n" +
			goTest + " discards stdout.\nEOF", false},
		{"heredoc body holding quotes", "git commit -F - <<'EOF'\nit's what \"the runner\" does:\n" +
			goTest + " discards stdout.\nEOF", false},
		{"message flag", `git commit -m "` + goTest + ` discards stdout"`, false},
		{"multi-line message flag", `git commit -m "fix: something` + "\n\n" + goTest + ` discards stdout"`, false},
		{"message flag quoting a separator", `git commit -m "do not run cd internal && ` + goTest + ` yourself"`, false},
		{"long message flag", "git commit --message='" + goTest + " is what this guard refuses'", false},
		{"nib body argument", `nibs update nibs-x -b "Never run ` + goTest + ` by hand"`, false},
		{"doc comment written to a file", "cat > internal/doc.go <<'EOF'\n// " + goTest +
			" discards a passing package's stdout.\nEOF", false},
		{"prose echoed", `echo "` + goTest + ` is banned in this project"`, false},
		{"prose in single quotes", "printf '%s\\n' '" + goTest + " is banned'", false},
		{"searching for the phrase", "grep -rn '" + goTest + "' scripts/", false},
		// Executed quoted text is a script in its own right, so the commands
		// inside it get read on their own terms rather than inheriting `sh`.
		{"message inside a shell script string",
			`sh -c "cd internal; git commit -m '` + goTest + ` discards stdout'"`, false},

		// The sanctioned paths, which must stay usable.
		{"capped runner", "bash scripts/go-test-capped.sh ./...", false},
		{"capped runner after a separator", "cd /repo && bash scripts/go-test-capped.sh ./internal/nib -run TestX", false},
		{"capped runner run directly", "scripts/go-test-capped.sh ./internal/nib -run TestX", false},
		{"the full gate", "task test", false},
		{"another go subcommand", "go build ./... && go vet ./...", false},
		{"another go subcommand behind a redirect", "> /tmp/out go build ./...", false},
		{"capped runner behind a redirect", "> /tmp/out bash scripts/go-test-capped.sh ./...", false},
		{"the full gate with its output redirected", "task test > /tmp/out 2>&1", false},
		// A redirect only stands in front of a command word at a command
		// position. Here `echo` already holds that place, so the words after
		// the file are its arguments and nothing runs the tests.
		{"a redirect that is not at a command position", "echo hi > /tmp/out " + goTest, false},
		// An escaped separator is regex alternation in a search pattern, not a
		// shell separator.
		{"escaped separator in a pattern", `rg '\bgo\|test\b' .`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocked, stderr := guardVerdict(t, shim, bashCommand(tt.command))
			if blocked != tt.block {
				verb := map[bool]string{true: "blocked", false: "allowed"}
				t.Errorf("guard %s %q, want %s\nstderr:\n%s",
					verb[blocked], tt.command, verb[tt.block], stderr)
			}
			if blocked && !strings.Contains(stderr, "go-test-capped.sh") {
				t.Errorf("refusal does not name the capped runner:\n%s", stderr)
			}
		})
	}
}

// TestGuardHonorsTheDeliberateOverride keeps the documented escape hatch
// working, so nobody has to disable the hook to get past it.
func TestGuardHonorsTheDeliberateOverride(t *testing.T) {
	shim := guardShim(t)

	blocked, stderr := guardVerdict(t, shim, bashCommand("NIBS_ALLOW_BARE_GO_TEST=1 "+goTest+" ./..."))
	if blocked {
		t.Errorf("the override did not get through:\n%s", stderr)
	}
}

// TestGuardStaysOutOfTheWayOtherwise covers the payloads the hook receives that
// are none of its business. Blocking any of these would stop every tool call.
func TestGuardStaysOutOfTheWayOtherwise(t *testing.T) {
	shim := guardShim(t)

	tests := []struct {
		name    string
		payload any
	}{
		{"another tool", map[string]any{
			"tool_name":  "Edit",
			"tool_input": map[string]string{"command": goTest + " ./..."},
		}},
		{"no command", map[string]any{
			"tool_name":  "Bash",
			"tool_input": map[string]string{},
		}},
		{"empty command", bashCommand("")},
		{"no tool input", map[string]any{"tool_name": "Bash"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if blocked, stderr := guardVerdict(t, shim, tt.payload); blocked {
				t.Errorf("guard blocked a payload it should ignore:\n%s", stderr)
			}
		})
	}
}

// TestGuardIgnoresAMalformedPayload pins the fail-open on unparsable stdin: the
// hook stands in front of every Bash call, so a payload shape it cannot read
// must not take the session down with it.
func TestGuardIgnoresAMalformedPayload(t *testing.T) {
	shim := guardShim(t)

	cmd := exec.Command("sh", shim)
	cmd.Stdin = strings.NewReader("{not json")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("guard failed on a malformed payload: %v\n%s", err, out)
	}
}
