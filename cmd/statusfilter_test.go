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
	openSet := []string{"in-progress", "todo", "draft"}
	closedSet := []string{"deferred", "completed", "scrapped"}

	tests := []struct {
		name           string
		in             statusFilterInput
		wantInclude    []string
		wantExclude    []string
		wantOpenApplie bool
	}{
		{
			name:           "default open: no flags excludes the closed statuses",
			in:             statusFilterInput{},
			wantInclude:    nil,
			wantExclude:    closedSet,
			wantOpenApplie: true,
		},
		{
			name:           "all: no include and no default exclusion",
			in:             statusFilterInput{All: true},
			wantInclude:    nil,
			wantExclude:    nil,
			wantOpenApplie: false,
		},
		{
			name:           "explicit status overrides open default",
			in:             statusFilterInput{Status: []string{"todo"}},
			wantInclude:    []string{"todo"},
			wantExclude:    nil,
			wantOpenApplie: false,
		},
		{
			name:           "explicit completed shows completed (overrides default)",
			in:             statusFilterInput{Status: []string{"completed"}},
			wantInclude:    []string{"completed"},
			wantExclude:    nil,
			wantOpenApplie: false,
		},
		{
			name:           "group open expands to the open set",
			in:             statusFilterInput{Status: []string{"open"}},
			wantInclude:    openSet,
			wantExclude:    nil,
			wantOpenApplie: false,
		},
		{
			name:           "group closed expands to the closed set",
			in:             statusFilterInput{Status: []string{"closed"}},
			wantInclude:    closedSet,
			wantExclude:    nil,
			wantOpenApplie: false,
		},
		{
			name:           "group parked expands to deferred",
			in:             statusFilterInput{Status: []string{"parked"}},
			wantInclude:    []string{"deferred"},
			wantExclude:    nil,
			wantOpenApplie: false,
		},
		{
			name:           "open flag equals -s open",
			in:             statusFilterInput{Open: true},
			wantInclude:    openSet,
			wantExclude:    nil,
			wantOpenApplie: false,
		},
		{
			name:           "open flag composes with explicit status (union)",
			in:             statusFilterInput{Status: []string{"completed"}, Open: true},
			wantInclude:    append(append([]string(nil), openSet...), "completed"),
			wantExclude:    nil,
			wantOpenApplie: false,
		},
		{
			// parked is a subset of closed, so subtracting it from the closed
			// group actually removes a member (it is a no-op against open).
			name:           "closed with no-status parked subtracts deferred",
			in:             statusFilterInput{Status: []string{"closed"}, NoStatus: []string{"parked"}},
			wantInclude:    closedSet,
			wantExclude:    []string{"deferred"},
			wantOpenApplie: false,
		},
		{
			name:           "default with no-status draft subtracts draft on top of the closed set",
			in:             statusFilterInput{NoStatus: []string{"draft"}},
			wantInclude:    nil,
			wantExclude:    append(append([]string(nil), closedSet...), "draft"),
			wantOpenApplie: true,
		},
		{
			name:           "no-status closed under default dedups the closed exclusion",
			in:             statusFilterInput{NoStatus: []string{"closed"}},
			wantInclude:    nil,
			wantExclude:    closedSet,
			wantOpenApplie: true,
		},
		{
			name:           "all with no-status closed excludes the closed set only",
			in:             statusFilterInput{All: true, NoStatus: []string{"closed"}},
			wantInclude:    nil,
			wantExclude:    closedSet,
			wantOpenApplie: false,
		},
		{
			// closed ∪ parked overlaps on deferred; the union must list it once.
			name:           "status group union dedups overlapping members",
			in:             statusFilterInput{Status: []string{"closed", "parked"}},
			wantInclude:    closedSet,
			wantExclude:    nil,
			wantOpenApplie: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inc, exc, openApplied, err := resolveStatusFilter(cfg, tt.in)
			if err != nil {
				t.Fatalf("resolveStatusFilter returned error: %v", err)
			}
			if !sameStatusSet(inc, tt.wantInclude) {
				t.Errorf("include = %v, want %v", inc, tt.wantInclude)
			}
			if !sameStatusSet(exc, tt.wantExclude) {
				t.Errorf("exclude = %v, want %v", exc, tt.wantExclude)
			}
			if openApplied != tt.wantOpenApplie {
				t.Errorf("openDefaultApplied = %v, want %v", openApplied, tt.wantOpenApplie)
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
			_, _, _, err := resolveStatusFilter(cfg, tt.in)
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

// TestResolveStatusFilter_ContradictoryFilter pins that a status filter admitting
// no status at all fails loudly instead of silently returning zero rows — an
// empty result reads as "no such nibs exist" to an agent.
func TestResolveStatusFilter_ContradictoryFilter(t *testing.T) {
	cfg := config.Default()
	contradictory := []struct {
		name string
		in   statusFilterInput
	}{
		{"no-status open under open default", statusFilterInput{NoStatus: []string{"open"}}},
		{"-s open --no-status open", statusFilterInput{Status: []string{"open"}, NoStatus: []string{"open"}}},
		{"-s completed --no-status completed", statusFilterInput{Status: []string{"completed"}, NoStatus: []string{"completed"}}},
		{"no-status open and closed", statusFilterInput{NoStatus: []string{"open", "closed"}}},
	}
	for _, tt := range contradictory {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := resolveStatusFilter(cfg, tt.in)
			if err == nil {
				t.Fatal("expected a contradiction error, got nil (would silently return zero rows)")
			}
			if !strings.Contains(err.Error(), "--all") {
				t.Errorf("error %q should suggest --all as a recovery", err.Error())
			}
		})
	}

	// The escape hatch: --all --no-status open is NOT contradictory — it means
	// "everything except the open set" = the closed set — and must not error.
	if _, _, _, err := resolveStatusFilter(cfg, statusFilterInput{All: true, NoStatus: []string{"open"}}); err != nil {
		t.Errorf("--all --no-status open should yield the closed set, not error: %v", err)
	}
}
