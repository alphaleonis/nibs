// Package scripts holds tests for the repository's shell scripts. It carries no
// non-test source: the code under test is scripts/go-test-capped.sh, and this
// package exists so `task test` exercises it alongside everything else.
package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeExec writes an executable helper script.
func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// stubEnv builds a PATH whose `go` and `systemd-run` are stubs: systemd-run
// always fails (standing in for a machine with no usable user systemd manager),
// and `go` records its arguments to a marker file instead of running. The marker
// path is returned; its absence proves `go test` was never reached.
func stubEnv(t *testing.T) (binDir, marker string) {
	t.Helper()
	binDir = t.TempDir()
	marker = filepath.Join(t.TempDir(), "go-was-run")

	writeExec(t, filepath.Join(binDir, "systemd-run"), "#!/bin/sh\nexit 1\n")
	writeExec(t, filepath.Join(binDir, "go"),
		"#!/bin/sh\nprintf '%s ' \"$@\" > \""+marker+"\"\n")

	return binDir, marker
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

func TestGoTestCappedRefusesUncappedOnWSL(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the script is bash; Windows runs it through Git Bash, not exercised here")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}

	const wslKernel = "6.18.33.2-microsoft-standard-WSL2"
	const nativeKernel = "6.11.4-201.fc40.x86_64"

	tests := []struct {
		name string
		// osrelease is the fake kernel release string; "" means the file is
		// absent altogether, which must be read as "not WSL".
		osrelease  string
		fileAbsent bool
		uncapped   bool

		wantRefusal bool
	}{
		{
			name:        "WSL without a cap refuses",
			osrelease:   wslKernel,
			wantRefusal: true,
		},
		{
			name:        "WSL with the opt-in runs",
			osrelease:   wslKernel,
			uncapped:    true,
			wantRefusal: false,
		},
		{
			name:        "native Linux runs without ceremony",
			osrelease:   nativeKernel,
			wantRefusal: false,
		},
		{
			name:        "an unreadable osrelease is not WSL",
			fileAbsent:  true,
			wantRefusal: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binDir, marker := stubEnv(t)

			osrelease := filepath.Join(t.TempDir(), "does-not-exist")
			if !tt.fileAbsent {
				osrelease = osreleaseFile(t, tt.osrelease)
			}

			cmd := exec.Command("bash", "go-test-capped.sh", "./internal/nib/")
			cmd.Env = append(os.Environ(),
				"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"NIBS_OSRELEASE_FILE="+osrelease,
			)
			if tt.uncapped {
				cmd.Env = append(cmd.Env, "GO_TEST_UNCAPPED=1")
			}

			var stderr strings.Builder
			cmd.Stderr = &stderr
			runErr := cmd.Run()

			_, markerErr := os.Stat(marker)
			goRan := markerErr == nil

			if tt.wantRefusal {
				if runErr == nil {
					t.Errorf("expected a non-zero exit, got success")
				}
				// The refusal is worthless if it still ran the thing it refused.
				if goRan {
					t.Errorf("refused but still executed 'go test'")
				}
				if !strings.Contains(stderr.String(), "GO_TEST_UNCAPPED=1") {
					t.Errorf("refusal does not name the opt-in; stderr:\n%s", stderr.String())
				}
				return
			}

			if runErr != nil {
				t.Errorf("expected success, got %v; stderr:\n%s", runErr, stderr.String())
			}
			if !goRan {
				t.Fatalf("expected 'go test' to run; stderr:\n%s", stderr.String())
			}
			args, err := os.ReadFile(marker)
			if err != nil {
				t.Fatalf("read marker: %v", err)
			}
			if got := string(args); !strings.HasPrefix(got, "test ./internal/nib/") {
				t.Errorf("arguments not forwarded verbatim: got %q", got)
			}
		})
	}
}
