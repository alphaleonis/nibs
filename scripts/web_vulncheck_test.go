package scripts

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// npmAuditJSON builds an `npm audit --json` report carrying one advisory per
// GHSA id.
//
// The shape matters more than the volume here. npm reports one entry per
// affected package, and `via` mixes two kinds of value: an advisory object on
// the package that carries the defect, and a bare string naming a parent on
// every package that merely depends on it. A filter that does not separate them
// either crashes on the strings or invents advisories from package names, so
// every fixture includes a string-only entry alongside the real ones.
func npmAuditJSON(ids ...string) string {
	entries := make([]string, 0, len(ids)+1)

	for i, id := range ids {
		entries = append(entries, fmt.Sprintf(
			`"pkg-%[1]s":{"name":"pkg-%[1]s","severity":"high","isDirect":true,`+
				`"via":[{"source":%[2]d,"name":"pkg-%[1]s","title":"noise the filter must ignore",`+
				`"url":"https://github.com/advisories/%[1]s","severity":"high","range":"<9.9.9"}],`+
				`"effects":["dependent-of-%[1]s"],"range":"<9.9.9","nodes":[],"fixAvailable":true}`,
			id, 1090000+i))
	}

	// A package flagged only because it depends on one of the above. Its `via`
	// is a bare string, which is not an advisory and must not become one.
	if len(ids) > 0 {
		entries = append(entries, fmt.Sprintf(
			`"dependent-of-%[1]s":{"name":"dependent-of-%[1]s","severity":"high","isDirect":false,`+
				`"via":["pkg-%[1]s"],"effects":[],"range":"*","nodes":[],"fixAvailable":true}`,
			ids[0]))
	}

	return fmt.Sprintf(
		`{"auditReportVersion":2,"vulnerabilities":{%s},`+
			`"metadata":{"vulnerabilities":{"info":0,"low":0,"moderate":0,"high":%d,"critical":0,"total":%d},`+
			`"dependencies":{"total":1}}}`,
		strings.Join(entries, ","), len(ids), len(ids))
}

// npmStubEnv returns a PATH whose `npm` prints the given audit report, standing
// in for a real registry query. jq is used for real — stubbing it would test the
// stub rather than the filter.
//
// An empty body stands for npm failing to produce a report at all: it emits the
// error payload npm actually returns on an unreachable registry (a `message`,
// no `metadata`) and exits non-zero, which is the case the gate must not
// mistake for a clean tree.
func npmStubEnv(t *testing.T, report string) string {
	t.Helper()
	binDir := t.TempDir()

	body := "#!/bin/sh\n" +
		`printf '%s\n' '{"message":"request to https://registry.npmjs.org/-/npm/v1/security/advisories/bulk failed, reason: getaddrinfo ENOTFOUND","error":{"summary":"","detail":""}}'` + "\n" +
		"exit 1\n"
	if report != "" {
		payload := filepath.Join(binDir, "payload.json")
		if err := os.WriteFile(payload, []byte(report), 0o644); err != nil {
			t.Fatalf("write payload: %v", err)
		}
		// npm audit exits 1 when it reports findings, so the stub does too: the
		// gate must decide from the payload, never from the status.
		body = "#!/bin/sh\ncat " + payload + "\nexit 1\n"
	}
	writeExec(t, filepath.Join(binDir, "npm"), body)

	return binDir + string(os.PathListSeparator) + os.Getenv("PATH")
}

func TestWebVulncheckGate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the script is bash; Windows runs it through Git Bash, not exercised here")
	}
	for _, bin := range []string{"bash", "jq"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not on PATH", bin)
		}
	}

	_, thisFile, _, _ := runtime.Caller(0)
	script := filepath.Join(filepath.Dir(thisFile), "web-vulncheck.sh")

	tests := []struct {
		name      string
		report    string // npm audit output; empty means npm fails to produce one
		allow     string
		wantExit  int
		wantInOut string
	}{
		{
			name:      "allowlisted advisory passes",
			report:    npmAuditJSON("GHSA-vh95-rmgr-6w4m"),
			allow:     "# rationale\nGHSA-vh95-rmgr-6w4m\n",
			wantExit:  0,
			wantInOut: "no new advisories",
		},
		{
			// The core guard: an advisory nobody has reviewed must fail.
			name:      "unallowlisted advisory fails",
			report:    npmAuditJSON("GHSA-vh95-rmgr-6w4m", "GHSA-xvch-5gv4-984h"),
			allow:     "GHSA-vh95-rmgr-6w4m\n",
			wantExit:  1,
			wantInOut: "GHSA-xvch-5gv4-984h",
		},
		{
			// The guard that keeps the gate honest over time: a suppression whose
			// reason is gone must not silently persist.
			name:      "stale allowlist entry fails",
			report:    npmAuditJSON("GHSA-vh95-rmgr-6w4m"),
			allow:     "GHSA-vh95-rmgr-6w4m\nGHSA-0000-0000-0000\n",
			wantExit:  1,
			wantInOut: "STALE",
		},
		{
			name:      "clean audit with empty allowlist passes",
			report:    npmAuditJSON(),
			allow:     "# nothing suppressed\n",
			wantExit:  0,
			wantInOut: "no new advisories",
		},
		{
			name:      "comments and blank lines are not treated as ids",
			report:    npmAuditJSON("GHSA-vh95-rmgr-6w4m"),
			allow:     "\n# GHSA-0000-0000-0000 mentioned only in prose\n\nGHSA-vh95-rmgr-6w4m # trailing comment\n",
			wantExit:  0,
			wantInOut: "no new advisories",
		},
		{
			// "could not check" must never look like "nothing to report". This is
			// the failure mode npm's exit status cannot express: it returns 1 both
			// here and on a real finding.
			name:      "npm failing to produce a report fails the gate",
			report:    "",
			allow:     "",
			wantExit:  1,
			wantInOut: "did not produce a report",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command("bash", script)
			cmd.Env = append(os.Environ(),
				"PATH="+npmStubEnv(t, tt.report),
				"WEB_VULNCHECK_ALLOWFILE="+writeAllow(t, tt.allow),
				"WEB_VULNCHECK_DIR="+t.TempDir(),
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

// A package whose `via` holds only strings names a parent, not an advisory. If
// the filter let those through, the gate would fail on invented ids that no
// allowlist entry could ever match and no advisory page would explain.
func TestWebVulncheckIgnoresTransitiveViaStrings(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the script is bash; Windows runs it through Git Bash, not exercised here")
	}
	for _, bin := range []string{"bash", "jq"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not on PATH", bin)
		}
	}

	_, thisFile, _, _ := runtime.Caller(0)
	script := filepath.Join(filepath.Dir(thisFile), "web-vulncheck.sh")

	report := `{"auditReportVersion":2,"vulnerabilities":{` +
		`"minimatch":{"name":"minimatch","severity":"high","via":["brace-expansion"],` +
		`"effects":[],"range":"*","nodes":[],"fixAvailable":true}` +
		`},"metadata":{"vulnerabilities":{"info":0,"low":0,"moderate":0,"high":1,"critical":0,"total":1},"dependencies":{"total":1}}}`

	cmd := exec.Command("bash", script)
	cmd.Env = append(os.Environ(),
		"PATH="+npmStubEnv(t, report),
		"WEB_VULNCHECK_ALLOWFILE="+writeAllow(t, "# nothing suppressed\n"),
		"WEB_VULNCHECK_DIR="+t.TempDir(),
	)
	out, err := cmd.CombinedOutput()

	if err != nil {
		t.Errorf("gate failed on a report whose only via entries are parent names: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "no new advisories") {
		t.Errorf("output missing %q\noutput:\n%s", "no new advisories", out)
	}
}
