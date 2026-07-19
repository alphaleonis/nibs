package cmd

import (
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/updatecheck"
	"github.com/spf13/cobra"
)

func TestUpdateNotifyEligible(t *testing.T) {
	cases := []struct {
		name    string
		cmdName string
		jsonSet bool
		isTTY   bool
		want    bool
	}{
		{"interactive list on a tty", "list", false, true, true},
		{"show on a tty", "show", false, true, true},
		{"not a tty (piped)", "list", false, false, false},
		{"json output suppresses", "list", true, true, false},
		{"json and non-tty", "list", true, false, false},
		{"serve is skipped", "serve", false, true, false},
		{"graphql is skipped", "graphql", false, true, false},
		{"query is skipped", "query", false, true, false},
		{"tui is skipped (in-app indicator)", "tui", false, true, false},
		{"completion machinery is skipped", "__complete", false, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := updateNotifyEligible(tc.cmdName, tc.jsonSet, tc.isTTY); got != tc.want {
				t.Errorf("updateNotifyEligible(%q, json=%v, tty=%v)=%v, want %v",
					tc.cmdName, tc.jsonSet, tc.isTTY, got, tc.want)
			}
		})
	}
}

func TestWriteUpdateNotice(t *testing.T) {
	avail := updatecheck.Result{Current: "v0.5.0", Latest: "v0.6.0", UpdateAvailable: true}

	t.Run("prints the notice when eligible and available", func(t *testing.T) {
		var b strings.Builder
		writeUpdateNotice(&b, true, avail, true)
		got := b.String()
		if !strings.Contains(got, "v0.6.0") || !strings.Contains(got, "nibs upgrade") {
			t.Errorf("notice missing version or upgrade hint: %q", got)
		}
	})

	t.Run("silent when not eligible", func(t *testing.T) {
		var b strings.Builder
		writeUpdateNotice(&b, false, avail, true)
		if b.Len() != 0 {
			t.Errorf("expected no output when ineligible, got %q", b.String())
		}
	})

	t.Run("silent when the check had no opinion", func(t *testing.T) {
		var b strings.Builder
		writeUpdateNotice(&b, true, updatecheck.Result{}, false)
		if b.Len() != 0 {
			t.Errorf("expected no output when ok=false, got %q", b.String())
		}
	})

	t.Run("silent when no update is available", func(t *testing.T) {
		var b strings.Builder
		writeUpdateNotice(&b, true, updatecheck.Result{Current: "v0.6.0", Latest: "v0.6.0"}, true)
		if b.Len() != 0 {
			t.Errorf("expected no output when up to date, got %q", b.String())
		}
	})
}

func TestJSONFlagSet(t *testing.T) {
	t.Run("flag present and true", func(t *testing.T) {
		cmd := &cobra.Command{Use: "x"}
		var v bool
		cmd.Flags().BoolVar(&v, "json", false, "")
		if err := cmd.Flags().Set("json", "true"); err != nil {
			t.Fatal(err)
		}
		if !jsonFlagSet(cmd) {
			t.Error("expected jsonFlagSet=true when --json=true")
		}
	})

	t.Run("flag present but false", func(t *testing.T) {
		cmd := &cobra.Command{Use: "x"}
		var v bool
		cmd.Flags().BoolVar(&v, "json", false, "")
		if jsonFlagSet(cmd) {
			t.Error("expected jsonFlagSet=false when --json defaults false")
		}
	})

	t.Run("no json flag", func(t *testing.T) {
		cmd := &cobra.Command{Use: "x"}
		if jsonFlagSet(cmd) {
			t.Error("expected jsonFlagSet=false when there is no --json flag")
		}
	})
}
