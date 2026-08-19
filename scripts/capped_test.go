// Package scripts holds tests for the repository's shell scripts. It carries no
// non-test source: the code under test is scripts/run-capped.sh and its
// go-test-capped.sh front end, and this package exists so `task test` exercises
// them alongside everything else.
package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const (
	wslKernel    = "6.18.33.2-microsoft-standard-WSL2"
	nativeKernel = "6.11.4-201.fc40.x86_64"
)

// writeExec writes an executable helper script.
func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// recorder writes an executable named name into dir that appends its arguments
// to logPath as one line per invocation and exits 0. Used both as the payload
// command (its presence in the log proves the command ran) and as a stub
// systemd-run (the log then records the scope properties it was asked for).
func recorder(t *testing.T, dir, name, logPath string) {
	t.Helper()
	writeExec(t, filepath.Join(dir, name),
		"#!/bin/sh\nprintf '%s ' \"$@\" >> '"+logPath+"'\nprintf '\\n' >> '"+logPath+"'\n")
}

// logLines returns the recorder log's non-empty lines, or nil if it was never
// written — an absent log means the recorded program never ran.
func logLines(t *testing.T, logPath string) []string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("read %s: %v", logPath, err)
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, strings.TrimSpace(line))
		}
	}
	return out
}

// osreleaseFile writes a fake /proc/sys/kernel/osrelease with the given content.
func osreleaseFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "osrelease")
	if err := os.WriteFile(path, []byte(content+"\n"), 0o644); err != nil {
		t.Fatalf("write osrelease: %v", err)
	}
	return path
}

// skipUnlessBash skips on platforms where these bash scripts are not exercised.
func skipUnlessBash(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the scripts are bash; Windows runs them through Git Bash, not exercised here")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
}

// TestRefusalIsScopedToThePolicyFlag pins which lanes refuse to run uncapped.
// The go lane opts in (--refuse-on-wsl) because an uncapped runaway `go test`
// has twice taken down the whole WSL VM; the web lane does not, because it has
// no such history and refusing there would break `task test` on an ordinary WSL
// box with no user systemd manager.
func TestRefusalIsScopedToThePolicyFlag(t *testing.T) {
	skipUnlessBash(t)

	tests := []struct {
		name string
		// script defaults to run-capped.sh; naming a front end instead asserts
		// that the front end still opts into the policy it is supposed to carry.
		script string
		// refuseOnWSL passes the policy flag that opts a lane into refusing.
		// Ignored when script is a front end that sets its own policy.
		refuseOnWSL bool
		// osrelease is the fake kernel release string; fileAbsent means the file
		// is missing altogether, which must be read as "not WSL".
		osrelease  string
		fileAbsent bool
		uncapped   bool

		wantRefusal bool
	}{
		{
			name:        "WSL without a cap refuses under the policy flag",
			refuseOnWSL: true,
			osrelease:   wslKernel,
			wantRefusal: true,
		},
		{
			name:        "the opt-in overrides the policy flag",
			refuseOnWSL: true,
			osrelease:   wslKernel,
			uncapped:    true,
			wantRefusal: false,
		},
		{
			name:        "native Linux runs without ceremony",
			refuseOnWSL: true,
			osrelease:   nativeKernel,
			wantRefusal: false,
		},
		{
			name:        "an unreadable osrelease is not WSL",
			refuseOnWSL: true,
			fileAbsent:  true,
			wantRefusal: false,
		},
		{
			name:        "without the policy flag WSL runs uncapped",
			osrelease:   wslKernel,
			wantRefusal: false,
		},
		{
			// Without this, dropping --refuse-on-wsl from the go front end
			// silently reopens the bypass that nibs-mlss closed, and every
			// other test here still passes.
			name:        "go-test-capped.sh opts into the refusal",
			script:      "go-test-capped.sh",
			osrelease:   wslKernel,
			wantRefusal: true,
		},
		{
			name:        "go-test-capped.sh honors the opt-in",
			script:      "go-test-capped.sh",
			osrelease:   wslKernel,
			uncapped:    true,
			wantRefusal: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binDir := t.TempDir()
			log := filepath.Join(t.TempDir(), "payload.log")

			// No usable cap: the probe fails, so every branch below is the
			// "systemd-run unavailable" one.
			writeExec(t, filepath.Join(binDir, "systemd-run"), "#!/bin/sh\nexit 1\n")

			osrelease := filepath.Join(t.TempDir(), "does-not-exist")
			if !tt.fileAbsent {
				osrelease = osreleaseFile(t, tt.osrelease)
			}

			// The front ends supply their own command and policy; run-capped.sh
			// is handed a recorded payload directly.
			var args []string
			switch tt.script {
			case "":
				recorder(t, binDir, "payload", log)
				args = []string{"run-capped.sh"}
				if tt.refuseOnWSL {
					args = append(args, "--refuse-on-wsl")
				}
				args = append(args, "payload", "an-argument")
			default:
				recorder(t, binDir, "go", log)
				args = []string{tt.script, "an-argument"}
			}

			cmd := exec.Command("bash", args...)
			cmd.Env = append(os.Environ(),
				"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"NIBS_OSRELEASE_FILE="+osrelease,
			)
			if tt.uncapped {
				cmd.Env = append(cmd.Env, "NIBS_UNCAPPED=1")
			}

			var stderr strings.Builder
			cmd.Stderr = &stderr
			runErr := cmd.Run()
			ran := logLines(t, log) != nil

			if tt.wantRefusal {
				if runErr == nil {
					t.Errorf("expected a non-zero exit, got success")
				}
				// The refusal is worthless if it still ran what it refused.
				if ran {
					t.Errorf("refused but still executed the command")
				}
				if !strings.Contains(stderr.String(), "NIBS_UNCAPPED=1") {
					t.Errorf("refusal does not name the opt-in; stderr:\n%s", stderr.String())
				}
				return
			}

			if runErr != nil {
				t.Errorf("expected success, got %v; stderr:\n%s", runErr, stderr.String())
			}
			if !ran {
				t.Fatalf("expected the command to run; stderr:\n%s", stderr.String())
			}
			if got := logLines(t, log)[0]; !strings.HasSuffix(got, "an-argument") {
				t.Errorf("arguments not forwarded verbatim: got %q", got)
			}
		})
	}
}

// TestCeilingIsAppliedToTheScope is the guard that matters for nibs-0kip: the
// point of the wrapper is that the workload is charged to a scope of its own
// with a ceiling, rather than to the caller's cgroup. If the properties stop
// reaching systemd-run, the command still runs and every other test still
// passes, so nothing but this assertion notices.
func TestCeilingIsAppliedToTheScope(t *testing.T) {
	skipUnlessBash(t)

	tests := []struct {
		name     string
		script   string
		extraEnv []string
		args     []string

		wantCeiling string
		wantCommand string
	}{
		{
			name:        "default ceiling",
			script:      "run-capped.sh",
			args:        []string{"payload", "one", "two"},
			wantCeiling: "-p MemoryMax=4G",
			wantCommand: "payload one two",
		},
		{
			name:        "NIBS_CAP_MEM_MAX overrides the ceiling",
			script:      "run-capped.sh",
			extraEnv:    []string{"NIBS_CAP_MEM_MAX=2G"},
			args:        []string{"payload"},
			wantCeiling: "-p MemoryMax=2G",
			wantCommand: "payload",
		},
		{
			name:        "the policy flag is consumed, not forwarded to the command",
			script:      "run-capped.sh",
			args:        []string{"--refuse-on-wsl", "payload", "one"},
			wantCeiling: "-p MemoryMax=4G",
			wantCommand: "payload one",
		},
		{
			name:        "go-test-capped.sh caps `go test` through the same mechanism",
			script:      "go-test-capped.sh",
			args:        []string{"./internal/nib/"},
			wantCeiling: "-p MemoryMax=4G",
			wantCommand: "go test ./internal/nib/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binDir := t.TempDir()
			log := filepath.Join(t.TempDir(), "systemd-run.log")

			// A systemd-run that succeeds: the availability probe passes and the
			// real invocation is recorded rather than executed.
			recorder(t, binDir, "systemd-run", log)

			cmd := exec.Command("bash", append([]string{tt.script}, tt.args...)...)
			cmd.Env = append(os.Environ(),
				"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
			)
			cmd.Env = append(cmd.Env, tt.extraEnv...)

			var stderr strings.Builder
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("expected success, got %v; stderr:\n%s", err, stderr.String())
			}

			lines := logLines(t, log)
			if len(lines) < 2 {
				t.Fatalf("expected an availability probe and a real invocation, got %v", lines)
			}
			// The probe is first; the invocation that carries the workload is last.
			got := lines[len(lines)-1]

			if !strings.Contains(got, tt.wantCeiling) {
				t.Errorf("scope missing %q; got %q", tt.wantCeiling, got)
			}
			// Swap must be off: with swap available the ceiling stops bounding
			// the workload's footprint and only slows the machine down.
			if !strings.Contains(got, "-p MemorySwapMax=0") {
				t.Errorf("scope does not disable swap; got %q", got)
			}
			if !strings.HasSuffix(got, tt.wantCommand) {
				t.Errorf("command not forwarded verbatim: want suffix %q, got %q", tt.wantCommand, got)
			}
		})
	}
}

// TestCrossedOperatingSystemsIsRefused pins the guard that stops a lane Task
// started on Windows from silently executing on Linux.
//
// `bash` is not one program on Windows: PowerShell resolves it to the WSL
// launcher in System32, Git Bash resolves its own first. Handed off from
// PowerShell the lane runs on Linux, and the Go suite then answers for Linux
// while looking like a Windows run — with an EMPTY skip tally, because on Linux
// nothing skips, which reads exactly like "every guard ran" (nibs-ehym).
//
// The marker is an ARGUMENT because neither environment route survives the
// crossing: a Windows variable is invisible inside WSL unless named in WSLENV,
// and Task's `env:` cannot set WSLENV when the parent shell already has one —
// which Windows Terminal does by default.
func TestCrossedOperatingSystemsIsRefused(t *testing.T) {
	skipUnlessBash(t)

	tests := []struct {
		name string
		// script defaults to run-capped.sh; naming the front end asserts it
		// forwards the marker rather than swallowing it.
		script string
		// hostOS is the --host-os value; empty means the flag is not passed at
		// all, which must stay runnable.
		hostOS string
		// ostype is what the script observes for the platform it is ON.
		ostype string

		wantRefusal bool
	}{
		{
			name:        "Task ran on Windows and the lane landed on Linux",
			hostOS:      "windows",
			ostype:      "linux-gnu",
			wantRefusal: true,
		},
		{
			// Git Bash is a Windows lane running on Windows: it reports cygwin
			// or msys, never linux-gnu, so the guard must stay out of the way.
			name:   "Git Bash is a Windows lane that did not cross",
			hostOS: "windows",
			ostype: "cygwin",
		},
		{
			name:   "a deliberate Linux run agrees with itself",
			hostOS: "linux",
			ostype: "linux-gnu",
		},
		{
			name:   "no marker at all still runs",
			ostype: "linux-gnu",
		},
		{
			name:        "the go front end forwards the marker",
			script:      "go-test-capped.sh",
			hostOS:      "windows",
			ostype:      "linux-gnu",
			wantRefusal: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binDir := t.TempDir()
			log := filepath.Join(t.TempDir(), "payload.log")

			// No cap available, and a native kernel — so neither the cap branch
			// nor the WSL refusal can account for a refusal seen here.
			writeExec(t, filepath.Join(binDir, "systemd-run"), "#!/bin/sh\nexit 1\n")
			osrelease := osreleaseFile(t, nativeKernel)

			var args []string
			switch tt.script {
			case "":
				recorder(t, binDir, "payload", log)
				args = []string{"run-capped.sh"}
				if tt.hostOS != "" {
					args = append(args, "--host-os", tt.hostOS)
				}
				args = append(args, "payload", "an-argument")
			default:
				recorder(t, binDir, "go", log)
				args = []string{tt.script}
				if tt.hostOS != "" {
					args = append(args, "--host-os", tt.hostOS)
				}
				args = append(args, "an-argument")
			}

			cmd := exec.Command("bash", args...)
			cmd.Env = append(os.Environ(),
				"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"NIBS_OSRELEASE_FILE="+osrelease,
				// bash does not overwrite an inherited OSTYPE, which is what
				// lets one machine exercise both platforms.
				"OSTYPE="+tt.ostype,
			)

			var stderr strings.Builder
			cmd.Stderr = &stderr
			runErr := cmd.Run()
			ran := logLines(t, log) != nil

			if tt.wantRefusal {
				if runErr == nil {
					t.Errorf("expected a non-zero exit, got success")
				}
				// A refusal that still ran the lane would have tested Linux
				// anyway, which is the whole failure being prevented.
				if ran {
					t.Errorf("refused but still executed the lane")
				}
				if !strings.Contains(stderr.String(), "crossed operating systems") {
					t.Errorf("refusal does not say what went wrong; stderr:\n%s", stderr.String())
				}
				return
			}

			if runErr != nil {
				t.Errorf("expected success, got %v; stderr:\n%s", runErr, stderr.String())
			}
			if !ran {
				t.Fatalf("expected the lane to run; stderr:\n%s", stderr.String())
			}
		})
	}
}

// TestRunCappedRejectsAnEmptyCommand keeps a missing command from being read as
// "cap nothing and succeed".
func TestRunCappedRejectsAnEmptyCommand(t *testing.T) {
	skipUnlessBash(t)

	cmd := exec.Command("bash", "run-capped.sh", "--refuse-on-wsl")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil {
		t.Errorf("expected a non-zero exit for a missing command, got success")
	}
	if !strings.Contains(stderr.String(), "usage:") {
		t.Errorf("no usage message; stderr:\n%s", stderr.String())
	}
}
