package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// govulncheckJSON builds a govulncheck `-format json` stream reporting one
// finding per id. The real output interleaves config, SBOM, progress and osv
// objects around the findings; a couple are included so the jq filter is
// exercised against noise rather than a clean list.
func govulncheckJSON(ids ...string) string {
	var b strings.Builder
	b.WriteString(`{"config":{"protocol_version":"v1.0.0"}}`)
	b.WriteString(`{"progress":{"message":"Scanning your code..."}}`)
	for _, id := range ids {
		b.WriteString(`{"osv":{"id":"` + id + `","summary":"noise the filter must ignore"}}`)
		b.WriteString(`{"finding":{"osv":"` + id + `","trace":[{"module":"example.com/mod","function":"F"}]}}`)
	}
	return b.String()
}

// vulnStubEnv returns a PATH whose `mise` prints the given govulncheck JSON (or
// fails, when json is empty), standing in for a real scan. jq is used for real —
// stubbing it would test the stub rather than the filter.
func vulnStubEnv(t *testing.T, json string) string {
	t.Helper()
	binDir := t.TempDir()

	body := "#!/bin/sh\nexit 1\n"
	if json != "" {
		payload := filepath.Join(binDir, "payload.json")
		if err := os.WriteFile(payload, []byte(json), 0o644); err != nil {
			t.Fatalf("write payload: %v", err)
		}
		body = "#!/bin/sh\ncat " + payload + "\n"
	}
	writeExec(t, filepath.Join(binDir, "mise"), body)

	return binDir + string(os.PathListSeparator) + os.Getenv("PATH")
}

func writeAllow(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "allow.txt")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write allowlist: %v", err)
	}
	return p
}

func TestVulncheckGate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the script is bash; Windows runs it through Git Bash, not exercised here")
	}
	for _, bin := range []string{"bash", "jq"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not on PATH", bin)
		}
	}

	_, thisFile, _, _ := runtime.Caller(0)
	script := filepath.Join(filepath.Dir(thisFile), "vulncheck.sh")

	tests := []struct {
		name      string
		json      string // govulncheck output; empty means govulncheck fails to run
		allow     string
		wantExit  int
		wantInOut string
	}{
		{
			name:      "allowlisted finding passes",
			json:      govulncheckJSON("GO-2026-5932"),
			allow:     "# rationale\nGO-2026-5932\n",
			wantExit:  0,
			wantInOut: "no new vulnerabilities",
		},
		{
			// The core guard: a vulnerability nobody has reviewed must fail.
			name:      "unallowlisted finding fails",
			json:      govulncheckJSON("GO-2026-5932", "GO-2026-9999"),
			allow:     "GO-2026-5932\n",
			wantExit:  1,
			wantInOut: "GO-2026-9999",
		},
		{
			// The guard that keeps the gate honest over time: a suppression whose
			// reason is gone must not silently persist.
			name:      "stale allowlist entry fails",
			json:      govulncheckJSON("GO-2026-5932"),
			allow:     "GO-2026-5932\nGO-2020-0001\n",
			wantExit:  1,
			wantInOut: "STALE",
		},
		{
			name:      "clean scan with empty allowlist passes",
			json:      govulncheckJSON(),
			allow:     "# nothing suppressed\n",
			wantExit:  0,
			wantInOut: "no new vulnerabilities",
		},
		{
			name:      "comments and blank lines are not treated as ids",
			json:      govulncheckJSON("GO-2026-5932"),
			allow:     "\n# GO-2020-0002 mentioned only in prose\n\nGO-2026-5932 # trailing comment\n",
			wantExit:  0,
			wantInOut: "no new vulnerabilities",
		},
		{
			// "could not check" must never look like "nothing to report".
			name:      "govulncheck failing to run fails the gate",
			json:      "",
			allow:     "GO-2026-5932\n",
			wantExit:  1,
			wantInOut: "failed to run",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command("bash", script)
			cmd.Env = append(os.Environ(),
				"PATH="+vulnStubEnv(t, tt.json),
				"VULNCHECK_ALLOWFILE="+writeAllow(t, tt.allow),
			)
			out, err := cmd.CombinedOutput()

			exit := 0
			if ee, ok := err.(*exec.ExitError); ok {
				exit = ee.ExitCode()
			} else if err != nil {
				t.Fatalf("running gate: %v\n%s", err, out)
			}

			if exit != tt.wantExit {
				t.Errorf("exit = %d, want %d\noutput:\n%s", exit, tt.wantExit, out)
			}
			if !strings.Contains(string(out), tt.wantInOut) {
				t.Errorf("output missing %q\noutput:\n%s", tt.wantInOut, out)
			}
		})
	}
}
