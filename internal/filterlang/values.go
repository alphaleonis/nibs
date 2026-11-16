package filterlang

import (
	"slices"

	"github.com/alphaleonis/nibs/internal/config"
)

// valueGroup is a name that stands for several values of one field, such as
// `status:open`.
type valueGroup struct {
	name    string
	members []string
}

// valueOrder holds each enumerated value set in the web's canonical order, the
// order a serialized token lists its values in. Statuses run open ones in
// workflow order, then closed ones in rank order, as the web's STATUSES does.
var valueOrder = func() map[ValueSet][]string {
	cfg := config.Default()
	return map[ValueSet][]string{
		TypeValues:     cfg.TypeNames(),
		PriorityValues: cfg.PriorityNames(),
		StatusValues:   slices.Concat(openStatuses(cfg), cfg.ClosedStatusNames()),
		EstimateValues: cfg.EstimateNames(),
	}
}()

// valueGroups holds the group names of each value set, in the order a
// serialized token tries to collapse them.
var valueGroups = func() map[ValueSet][]valueGroup {
	cfg := config.Default()
	return map[ValueSet][]valueGroup{
		StatusValues: {
			{name: config.StatusGroupOpen, members: openStatuses(cfg)},
			{name: config.StatusGroupClosed, members: cfg.ClosedStatusNames()},
		},
	}
}()

// openStatuses returns the statuses that are not closed, in workflow order.
func openStatuses(cfg *config.Config) []string {
	return slices.DeleteFunc(cfg.WorkflowStatusNames(), cfg.IsClosedStatus)
}

// isValidValue reports whether the lowercased value is legal for f: a member of
// its value set, one of its group names, or for tags a match of nib.TagPattern.
func isValidValue(f Field, value string) bool {
	if f.Values == TagValues {
		return tagPattern.MatchString(value)
	}
	return slices.Contains(valueOrder[f.Values], value) ||
		slices.ContainsFunc(valueGroups[f.Values], func(g valueGroup) bool { return g.name == value })
}

// expandValue returns the values a legal value stands for: a group's members,
// or the value itself.
func expandValue(f Field, value string) []string {
	for _, g := range valueGroups[f.Values] {
		if g.name == value {
			return g.members
		}
	}
	return []string{value}
}

// Values returns the values of an enumerated value set in canonical order, or
// nil for TagValues.
func Values(set ValueSet) []string {
	return slices.Clone(valueOrder[set])
}

// GroupMembers returns the values the group name stands for in set, or nil
// when set has no group of that name.
func GroupMembers(set ValueSet, name string) []string {
	for _, g := range valueGroups[set] {
		if g.name == name {
			return slices.Clone(g.members)
		}
	}
	return nil
}
