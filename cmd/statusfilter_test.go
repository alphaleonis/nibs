package cmd

import (
	"sort"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
)

// sameStatusSet reports whether got and want contain the same status names,
// order-independent. The helper builds include/exclude lists whose order is an
// implementation detail; only membership is part of the contract.
func sameStatusSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	g := append([]string(nil), got...)
	w := append([]string(nil), want...)
	sort.Strings(g)
	sort.Strings(w)
	for i := range g {
		if g[i] != w[i] {
			return false
		}
	}
	return true
}

func TestResolveStatusFilter(t *testing.T) {
	cfg := config.Default()
	openSet := []string{"in-progress", "todo", "draft", "deferred"}
	archive := []string{"completed", "scrapped"}

	tests := []struct {
		name        string
		in          statusFilterInput
		wantInclude []string
		wantExclude []string
	}{
		{
			name:        "default open: no flags excludes archive statuses",
			in:          statusFilterInput{},
			wantInclude: nil,
			wantExclude: archive,
		},
		{
			name:        "all: no include and no default exclusion",
			in:          statusFilterInput{All: true},
			wantInclude: nil,
			wantExclude: nil,
		},
		{
			name:        "explicit status overrides open default",
			in:          statusFilterInput{Status: []string{"todo"}},
			wantInclude: []string{"todo"},
			wantExclude: nil,
		},
		{
			name:        "explicit completed shows completed (overrides default)",
			in:          statusFilterInput{Status: []string{"completed"}},
			wantInclude: []string{"completed"},
			wantExclude: nil,
		},
		{
			name:        "group open expands to the open set",
			in:          statusFilterInput{Status: []string{"open"}},
			wantInclude: openSet,
			wantExclude: nil,
		},
		{
			name:        "group closed expands to the archive set",
			in:          statusFilterInput{Status: []string{"closed"}},
			wantInclude: archive,
			wantExclude: nil,
		},
		{
			name:        "group parked expands to deferred",
			in:          statusFilterInput{Status: []string{"parked"}},
			wantInclude: []string{"deferred"},
			wantExclude: nil,
		},
		{
			name:        "open flag equals -s open",
			in:          statusFilterInput{Open: true},
			wantInclude: openSet,
			wantExclude: nil,
		},
		{
			name:        "open flag composes with explicit status (union)",
			in:          statusFilterInput{Status: []string{"completed"}, Open: true},
			wantInclude: append(append([]string(nil), openSet...), "completed"),
			wantExclude: nil,
		},
		{
			name:        "open with no-status parked subtracts deferred",
			in:          statusFilterInput{Status: []string{"open"}, NoStatus: []string{"parked"}},
			wantInclude: openSet,
			wantExclude: []string{"deferred"},
		},
		{
			name:        "default with no-status draft subtracts draft on top of archive",
			in:          statusFilterInput{NoStatus: []string{"draft"}},
			wantInclude: nil,
			wantExclude: []string{"completed", "scrapped", "draft"},
		},
		{
			name:        "no-status closed under default dedups archive exclusion",
			in:          statusFilterInput{NoStatus: []string{"closed"}},
			wantInclude: nil,
			wantExclude: archive,
		},
		{
			name:        "all with no-status closed excludes archive only",
			in:          statusFilterInput{All: true, NoStatus: []string{"closed"}},
			wantInclude: nil,
			wantExclude: archive,
		},
		{
			name:        "status group union dedups overlapping members",
			in:          statusFilterInput{Status: []string{"open", "parked"}},
			wantInclude: openSet,
			wantExclude: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inc, exc, err := resolveStatusFilter(cfg, tt.in)
			if err != nil {
				t.Fatalf("resolveStatusFilter returned error: %v", err)
			}
			if !sameStatusSet(inc, tt.wantInclude) {
				t.Errorf("include = %v, want %v", inc, tt.wantInclude)
			}
			if !sameStatusSet(exc, tt.wantExclude) {
				t.Errorf("exclude = %v, want %v", exc, tt.wantExclude)
			}
		})
	}
}

func TestResolveStatusFilter_InvalidToken(t *testing.T) {
	cfg := config.Default()
	tests := []struct {
		name string
		in   statusFilterInput
	}{
		{"bad status include", statusFilterInput{Status: []string{"bogus"}}},
		{"bad status exclude", statusFilterInput{NoStatus: []string{"bogus"}}},
		{"bad among valid", statusFilterInput{Status: []string{"todo", "nope"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := resolveStatusFilter(cfg, tt.in)
			if err == nil {
				t.Fatal("expected an error for an unknown status/group token, got nil")
			}
			// The error must name the offending token and list the accepted
			// group names so a caller can recover.
			for _, want := range []string{"open", "closed", "parked"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q should list accepted group %q", err.Error(), want)
				}
			}
		})
	}
}
