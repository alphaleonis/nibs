package cmd

import (
	"fmt"
	"strings"

	"github.com/alphaleonis/nibs/internal/config"
)

// Status group names. A group is accepted anywhere a concrete status is
// accepted in -s/--status and --no-status on both `list` and `rel`, and
// expands to its concrete member statuses:
//   - open   → the non-archive statuses (config.OpenStatusNames)
//   - closed → the archive statuses (config.ArchiveStatusNames)
//   - parked → the non-actionable-but-not-abandoned statuses (config.ParkedStatusNames)
const (
	statusGroupOpen   = "open"
	statusGroupClosed = "closed"
	statusGroupParked = "parked"
)

// statusFilterInput captures the raw status-related flags shared by `list` and
// `rel`. It is the single input to resolveStatusFilter so the group-expansion,
// open-by-default, and precedence rules live in exactly one place.
type statusFilterInput struct {
	Status   []string // -s / --status tokens (concrete statuses or group names)
	NoStatus []string // --no-status tokens (concrete statuses or group names)
	All      bool     // --all: base is every status (no open-by-default exclusion)
	Open     bool     // --open / --active: shorthand for -s open
}

// resolveStatusFilter expands status groups, applies the open-by-default rule,
// and returns the concrete (include, exclude) status lists to populate a
// model.NibFilter's Status / ExcludeStatus fields. filterByField (include) then
// excludeByField (exclude) in graph.ApplyFilter give the "base then subtract"
// behavior for free.
//
// Semantics ("base then subtract"):
//   - No status token and no --all → default open: exclude the archive statuses
//     (completed, scrapped). include is nil so every non-excluded status passes.
//   - --all → base is every status: include and the open-default exclusion are
//     both empty.
//   - Any explicit -s X… (after group expansion, including --open/--active which
//     inject the open group) → include = union(X); this overrides the open
//     default so `-s closed`/`-s completed` show archived nibs.
//   - --no-status Y… (after group expansion) → added to exclude, subtracting Y
//     from the current base.
//
// An unknown token (neither a concrete status nor a group) is a validation
// error naming the offending token and the accepted values.
func resolveStatusFilter(cfg *config.Config, in statusFilterInput) (include, exclude []string, err error) {
	statusTokens := in.Status
	if in.Open {
		// --open / --active is shorthand for -s open; append rather than
		// replace so it unions with any explicit -s tokens.
		statusTokens = append(append([]string(nil), statusTokens...), statusGroupOpen)
	}

	include, err = expandStatusTokens(cfg, statusTokens)
	if err != nil {
		return nil, nil, err
	}
	exclude, err = expandStatusTokens(cfg, in.NoStatus)
	if err != nil {
		return nil, nil, err
	}

	// Open-by-default: apply only when the caller supplied no explicit include
	// set and did not ask for --all. An explicit -s (even -s open) overrides it.
	if len(include) == 0 && !in.All {
		exclude = appendMissingStatuses(exclude, cfg.ArchiveStatusNames())
	}
	return include, exclude, nil
}

// expandStatusTokens validates and expands a list of status/group tokens into a
// deduped list of concrete status names, preserving first-seen order.
func expandStatusTokens(cfg *config.Config, tokens []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, tok := range tokens {
		members, err := statusGroupMembers(cfg, tok)
		if err != nil {
			return nil, err
		}
		for _, m := range members {
			if seen[m] {
				continue
			}
			seen[m] = true
			out = append(out, m)
		}
	}
	return out, nil
}

// statusGroupMembers resolves a single -s/--no-status token to its concrete
// status members. A group name expands to its members; a concrete status
// resolves to itself; anything else is a validation error.
func statusGroupMembers(cfg *config.Config, token string) ([]string, error) {
	switch token {
	case statusGroupOpen:
		return cfg.OpenStatusNames(), nil
	case statusGroupClosed:
		return cfg.ArchiveStatusNames(), nil
	case statusGroupParked:
		return cfg.ParkedStatusNames(), nil
	}
	if cfg.IsValidStatus(token) {
		return []string{token}, nil
	}
	return nil, fmt.Errorf("invalid status %q: must be one of %s or a status group (%s)",
		token, cfg.StatusList(), strings.Join(statusGroupNames(), ", "))
}

// statusGroupNames returns the accepted status-group names, for error messages.
func statusGroupNames() []string {
	return []string{statusGroupOpen, statusGroupClosed, statusGroupParked}
}

// appendMissingStatuses appends every name in add that is not already in base,
// preserving order. Used to fold the open-default archive exclusion into an
// existing --no-status set without duplicating members.
func appendMissingStatuses(base, add []string) []string {
	present := make(map[string]bool, len(base))
	for _, s := range base {
		present[s] = true
	}
	for _, s := range add {
		if present[s] {
			continue
		}
		present[s] = true
		base = append(base, s)
	}
	return base
}
