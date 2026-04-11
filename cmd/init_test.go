package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
)

// resetInitFlags restores all package-level flag globals touched by the init
// command so subsequent tests start from a clean state. Cobra binds --json,
// --prefix, --config, and --nibs-path to package-level vars and does not
// reset them between Execute() calls.
func resetInitFlags() {
	initJSON = false
	initPrefix = ""
	configPath = ""
	nibsPath = ""
}

// setupInitTest prepares a temp project with a controlled basename and
// schedules a flag reset on cleanup. It creates
//
//	<tmpDir>/<dirName>/
//
// but does NOT create the nibs subdirectory — `nibs init` does that itself.
// Returns (projectDir, nibsPath) where nibsPath is the path the test should
// pass via --nibs-path so the command computes dirName = filepath.Base(projectDir).
func setupInitTest(t *testing.T, dirName string) (projectDir, nibsPath string) {
	t.Helper()
	t.Cleanup(resetInitFlags)
	resetInitFlags()

	tmpDir := t.TempDir()
	projectDir = filepath.Join(tmpDir, dirName)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project dir: %v", err)
	}
	nibsPath = filepath.Join(projectDir, ".nibs")
	return projectDir, nibsPath
}

// runInitCmd invokes `nibs --nibs-path <nibsPath> init <args...>` via the
// package-level rootCmd and returns rootCmd.Execute()'s error.
func runInitCmd(t *testing.T, nibsPath string, args ...string) error {
	t.Helper()
	full := append([]string{"--nibs-path", nibsPath, "init"}, args...)
	rootCmd.SetArgs(full)
	return rootCmd.Execute()
}

// loadInitCfg loads <projectDir>/.nibs.yml and fails the test if missing.
func loadInitCfg(t *testing.T, projectDir string) *config.Config {
	t.Helper()
	cfgPath := filepath.Join(projectDir, ".nibs.yml")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config at %s: %v", cfgPath, err)
	}
	return cfg
}

// TestInit_ExplicitPrefix_TracerBullet exercises the happy path: a
// directory with an awkward camelCase name + an explicit --prefix flag
// produces a valid prefix in .nibs.yml regardless of the dirname.
func TestInit_ExplicitPrefix_TracerBullet(t *testing.T) {
	projectDir, nibsPath := setupInitTest(t, "boardGameTracker")

	if err := runInitCmd(t, nibsPath, "--prefix", "bgt-", "--json"); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// .nibs.yml must exist and have the explicit prefix.
	cfg := loadInitCfg(t, projectDir)
	if cfg.Nibs.Prefix != "bgt-" {
		t.Errorf("cfg prefix = %q, want %q", cfg.Nibs.Prefix, "bgt-")
	}

	// .nibs/ subdir must exist.
	if _, err := os.Stat(nibsPath); err != nil {
		t.Errorf("expected .nibs/ dir to exist: %v", err)
	}
}

// TestInit_ExplicitPrefix_AutoAppendsDash verifies that a user who passes
// `--prefix bgt` (no trailing dash) gets `bgt-` in .nibs.yml — same
// auto-append behavior as config set-prefix.
func TestInit_ExplicitPrefix_AutoAppendsDash(t *testing.T) {
	projectDir, nibsPath := setupInitTest(t, "boardGameTracker")

	if err := runInitCmd(t, nibsPath, "--prefix", "bgt", "--json"); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	cfg := loadInitCfg(t, projectDir)
	if cfg.Nibs.Prefix != "bgt-" {
		t.Errorf("cfg prefix = %q, want %q", cfg.Nibs.Prefix, "bgt-")
	}
}

// TestInit_ExplicitPrefix_UppercaseRejected verifies that explicit input is
// NOT silently lowercased — an uppercase prefix is an error, and the error
// message must NOT mention --prefix (the user already used it, so the hint
// would be tautological).
func TestInit_ExplicitPrefix_UppercaseRejected(t *testing.T) {
	cases := []struct {
		name           string
		input          string
		wantInErrorMsg string // substring that must appear (the auto-appended form)
	}{
		{"bare uppercase", "BGT", "BGT-"},
		{"uppercase with dash", "BGT-", "BGT-"},
		{"mixed case with dash", "Foo-", "Foo-"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projectDir, nibsPath := setupInitTest(t, "someProject")

			err := runInitCmd(t, nibsPath, "--prefix", tc.input, "--json")
			assertErrorContainsAll(t, err,
				[]string{tc.wantInErrorMsg},
				[]string{"--prefix"},
			)
			assertNoConfigFile(t, projectDir)
		})
	}
}

// TestInit_DerivedPrefix_LowercaseDirname is a regression guard: the
// existing behavior — a valid lowercase dirname becomes `<dirname>-` —
// must continue to work unchanged.
func TestInit_DerivedPrefix_LowercaseDirname(t *testing.T) {
	projectDir, nibsPath := setupInitTest(t, "myproj")

	if err := runInitCmd(t, nibsPath, "--json"); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	cfg := loadInitCfg(t, projectDir)
	if cfg.Nibs.Prefix != "myproj-" {
		t.Errorf("cfg prefix = %q, want %q", cfg.Nibs.Prefix, "myproj-")
	}
}

// TestInit_DerivedPrefix_DefaultBranch_Getwd exercises the init code path
// that runs with no --nibs-path, where the command uses os.Getwd() to
// determine the project directory. Every other test uses --nibs-path, so
// without this the default (common real-user) branch has zero coverage of
// the new --prefix feature.
func TestInit_DerivedPrefix_DefaultBranch_Getwd(t *testing.T) {
	projectDir, _ := setupInitTest(t, "myproj")
	t.Chdir(projectDir)
	// setupInitTest has already called resetInitFlags once; call it again
	// here to defend against any leakage from earlier tests in this run
	// where Cobra's flag parser may have left stale values in the
	// package-level nibsPath/configPath globals. Without empty nibsPath
	// the command would route into the first branch at init.go:25 and
	// never hit the Getwd() fallback this test is meant to exercise.
	resetInitFlags()
	rootCmd.SetArgs([]string{"init", "--json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	cfg := loadInitCfg(t, projectDir)
	if cfg.Nibs.Prefix != "myproj-" {
		t.Errorf("cfg prefix = %q, want %q", cfg.Nibs.Prefix, "myproj-")
	}
}

// TestInit_DerivedPrefix_CamelCaseLowercased verifies that a camelCase
// dirname is lowercased in the derived path (user didn't choose the name,
// so we can normalize).
func TestInit_DerivedPrefix_CamelCaseLowercased(t *testing.T) {
	projectDir, nibsPath := setupInitTest(t, "boardGame")

	if err := runInitCmd(t, nibsPath, "--json"); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	cfg := loadInitCfg(t, projectDir)
	if cfg.Nibs.Prefix != "boardgame-" {
		t.Errorf("cfg prefix = %q, want %q", cfg.Nibs.Prefix, "boardgame-")
	}
}

// TestInit_DerivedPrefix_InvalidWithHint verifies that when the derived
// prefix fails validation, the error message contains the failing derived
// value, the originating dirname, AND the --prefix escape-hatch hint.
func TestInit_DerivedPrefix_InvalidWithHint(t *testing.T) {
	cases := []struct {
		name           string
		dirName        string
		wantInErrorMsg []string // substrings that must appear
	}{
		{
			// Lowercased to "boardgametracker-" (17 chars) — fails length limit.
			name:           "too long",
			dirName:        "boardGameTracker",
			wantInErrorMsg: []string{"boardgametracker-", "boardGameTracker", "--prefix"},
		},
		{
			// Lowercased to "my project-" — contains space, fails charset.
			name:           "contains space",
			dirName:        "my project",
			wantInErrorMsg: []string{"my project-", "my project", "--prefix"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projectDir, nibsPath := setupInitTest(t, tc.dirName)

			err := runInitCmd(t, nibsPath, "--json")
			assertErrorContainsAll(t, err, tc.wantInErrorMsg, nil)
			assertNoConfigFile(t, projectDir)
		})
	}
}

// TestInit_ExplicitPrefix_InvalidCharset verifies that an invalid-character
// explicit prefix is rejected after auto-append, the error mentions the
// auto-appended form, and the error message does NOT contain the --prefix
// hint (redundant for explicit input). The "lowercase underscore only" case
// isolates the charset rule from the uppercase rule — using "BAD_" alone
// would leave the pure-charset failure untested (it'd trip the uppercase
// check too), so a "bad_" subcase is added to lock in charset-only rejection.
func TestInit_ExplicitPrefix_InvalidCharset(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"uppercase and underscore", "BAD_"},
		{"lowercase underscore only", "bad_"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projectDir, nibsPath := setupInitTest(t, "myproject")

			err := runInitCmd(t, nibsPath, "--prefix", tc.input, "--json")
			assertErrorContainsAll(t, err,
				[]string{tc.input + "-"},
				[]string{"--prefix"},
			)
			assertNoConfigFile(t, projectDir)
		})
	}
}

// assertNoConfigFile helps a handful of failure-mode tests verify that a
// rejected init did NOT leave a partial .nibs.yml behind.
func assertNoConfigFile(t *testing.T, projectDir string) {
	t.Helper()
	cfgPath := filepath.Join(projectDir, ".nibs.yml")
	_, err := os.Stat(cfgPath)
	if err == nil {
		t.Errorf("expected %s to NOT exist after rejected init", cfgPath)
	} else if !os.IsNotExist(err) {
		t.Errorf("unexpected stat error on %s: %v", cfgPath, err)
	}
}

// assertErrorContainsAll checks that every expected substring appears in the
// error message, and optionally that excluded substrings do not. Used so each
// test's assertion list reads as a single block.
func assertErrorContainsAll(t *testing.T, err error, contains []string, notContains []string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	for _, s := range contains {
		if !strings.Contains(msg, s) {
			t.Errorf("error message %q should contain %q", msg, s)
		}
	}
	for _, s := range notContains {
		if strings.Contains(msg, s) {
			t.Errorf("error message %q should NOT contain %q", msg, s)
		}
	}
}
