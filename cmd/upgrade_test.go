package cmd

import (
	"path/filepath"
	"testing"
)

func fakeEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDetectPackageManager(t *testing.T) {
	cases := []struct {
		name    string
		exePath string
		env     map[string]string
		want    string // manager name, "" for unmanaged
	}{
		{"nix store", "/nix/store/abc123-nibs-0.5.0/bin/nibs", nil, "Nix"},
		{"homebrew opt", "/opt/homebrew/bin/nibs", nil, "Homebrew"},
		{"homebrew cellar", "/usr/local/Cellar/nibs/0.5.0/bin/nibs", nil, "Homebrew"},
		{"linuxbrew", "/home/linuxbrew/.linuxbrew/bin/nibs", nil, "Homebrew"},
		{"scoop windows", `C:\Users\me\scoop\apps\nibs\current\nibs.exe`, nil, "Scoop"},
		{"chocolatey windows", `C:\ProgramData\chocolatey\bin\nibs.exe`, nil, "Chocolatey"},
		{"winget windows", `C:\Users\me\AppData\Local\Microsoft\WinGet\Packages\alphaleonis.nibs\nibs.exe`, nil, "WinGet"},
		{"gobin", "/opt/gobin/nibs", map[string]string{"GOBIN": "/opt/gobin"}, "go install"},
		{"gopath bin", "/ws/gp/bin/nibs", map[string]string{"GOPATH": "/ws/gp"}, "go install"},
		{"home go bin", "/home/u/go/bin/nibs", map[string]string{"HOME": "/home/u"}, "go install"},
		// Build GOPATH with the OS list separator (":" on Unix, ";" on Windows)
		// so filepath.SplitList in inGoBin splits it on every platform.
		{"gopath with multiple entries", "/second/gp/bin/nibs", map[string]string{"GOPATH": "/first/gp" + string(filepath.ListSeparator) + "/second/gp"}, "go install"},

		// Must NOT be flagged — these are the default install.sh targets and
		// ordinary system locations that `nibs upgrade` should self-replace.
		{"default local bin", "/home/user/.local/bin/nibs", map[string]string{"HOME": "/home/user"}, ""},
		{"usr local bin", "/usr/local/bin/nibs", nil, ""},
		{"home not in go bin", "/home/user/bin/nibs", map[string]string{"HOME": "/home/user"}, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := tc.env
			if env == nil {
				env = map[string]string{}
			}
			mgr, managed := detectPackageManager(tc.exePath, fakeEnv(env))
			if tc.want == "" {
				if managed {
					t.Errorf("detectPackageManager(%q) = managed by %q, want unmanaged", tc.exePath, mgr.name)
				}
				return
			}
			if !managed {
				t.Fatalf("detectPackageManager(%q) = unmanaged, want %q", tc.exePath, tc.want)
			}
			if mgr.name != tc.want {
				t.Errorf("detectPackageManager(%q) = %q, want %q", tc.exePath, mgr.name, tc.want)
			}
			if mgr.hint == "" {
				t.Errorf("manager %q has an empty upgrade hint", mgr.name)
			}
		})
	}
}

func TestIsUpToDate(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"v0.6.0", "v0.6.0", true},  // equal
		{"v0.7.0", "v0.6.0", true},  // running ahead
		{"v0.5.0", "v0.6.0", false}, // update available
		{"v0.5.0", "garbage", false},
		{"garbage", "v0.6.0", false},
	}
	for _, tc := range cases {
		if got := isUpToDate(tc.current, tc.latest); got != tc.want {
			t.Errorf("isUpToDate(%q,%q)=%v, want %v", tc.current, tc.latest, got, tc.want)
		}
	}
}

func TestEnsureVersionV(t *testing.T) {
	cases := map[string]string{
		"0.6.0":  "v0.6.0",
		"v0.6.0": "v0.6.0",
		"":       "",
	}
	for in, want := range cases {
		if got := ensureVersionV(in); got != want {
			t.Errorf("ensureVersionV(%q)=%q, want %q", in, got, want)
		}
	}
}
