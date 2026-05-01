package cmd

import (
	"strings"
	"testing"
)

func TestFindClosestFlag(t *testing.T) {
	tests := []struct {
		name       string
		unknown    string
		candidates []string
		maxDist    int
		want       string
		wantOK     bool
	}{
		{
			name:       "tracer: tags suggests tag",
			unknown:    "tags",
			candidates: []string{"tag", "title"},
			maxDist:    2,
			want:       "tag",
			wantOK:     true,
		},
		{
			name:       "too far: xyzqwe has no close match",
			unknown:    "xyzqwe",
			candidates: []string{"tag", "title"},
			maxDist:    2,
			want:       "",
			wantOK:     false,
		},
		{
			name:       "no candidates",
			unknown:    "tag",
			candidates: nil,
			maxDist:    2,
			want:       "",
			wantOK:     false,
		},
		{
			name:       "tie at min distance: skip",
			unknown:    "tab",
			candidates: []string{"tag", "tar"},
			maxDist:    2,
			want:       "",
			wantOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := findClosestFlag(tt.unknown, tt.candidates, tt.maxDist)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("findClosestFlag(%q, %v, %d) = (%q, %v), want (%q, %v)",
					tt.unknown, tt.candidates, tt.maxDist, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

// TestFlagSuggestion_Integration drives the real rootCmd and asserts the
// FlagErrorFunc surfaces a "Did you mean --foo?" hint for unknown long flags.
// Flag parsing fails before PersistentPreRunE, so no .nibs directory or
// fixtures are required.
func TestFlagSuggestion_Integration(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantErr     bool
		wantContain string // substring that must be present
		wantAbsent  string // substring that must NOT be present (empty = no check)
	}{
		{
			name:        "subcommand: --tags suggests --tag",
			args:        []string{"create", "--tags", "foo"},
			wantErr:     true,
			wantContain: "Did you mean --tag?",
		},
		{
			name:       "subcommand: --xyzqwe is too far, no suggestion",
			args:       []string{"create", "--xyzqwe", "foo"},
			wantErr:    true,
			wantAbsent: "Did you mean",
		},
		{
			name:        "persistent: --confg suggests --config",
			args:        []string{"--confg", "./x.yml", "list"},
			wantErr:     true,
			wantContain: "Did you mean --config?",
		},
		{
			// `update.go` sorts alphabetically AFTER `root.go`, so its
			// `rootCmd.AddCommand(updateCmd)` runs in init() *after*
			// installFlagSuggestions. updateCmd never gets flagErrorFunc
			// set explicitly — it relies on Cobra's parent-walk in
			// FlagErrorFunc() to find rootCmd's. This case pins that
			// contract: if the parent-walk ever breaks, this test fails.
			name:        "late-registered subcommand: --titel suggests --title",
			args:        []string{"update", "--titel", "abc-1"},
			wantErr:     true,
			wantContain: "Did you mean --title?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origSilenceUsage := rootCmd.SilenceUsage
			origSilenceErrors := rootCmd.SilenceErrors
			t.Cleanup(resetRootPersistentFlags)
			t.Cleanup(func() {
				rootCmd.SetArgs(nil)
				rootCmd.SilenceUsage = origSilenceUsage
				rootCmd.SilenceErrors = origSilenceErrors
			})
			rootCmd.SetArgs(tt.args)
			// Silence Cobra's own usage/error printing so it doesn't pollute
			// the test process's stdout/stderr.
			rootCmd.SilenceUsage = true
			rootCmd.SilenceErrors = true
			err := rootCmd.Execute()
			if tt.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			msg := ""
			if err != nil {
				msg = err.Error()
			}
			if tt.wantContain != "" && !strings.Contains(msg, tt.wantContain) {
				t.Errorf("error %q does not contain %q", msg, tt.wantContain)
			}
			if tt.wantAbsent != "" && strings.Contains(msg, tt.wantAbsent) {
				t.Errorf("error %q unexpectedly contains %q", msg, tt.wantAbsent)
			}
		})
	}
}
