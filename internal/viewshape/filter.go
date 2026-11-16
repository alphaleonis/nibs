package viewshape

import (
	"slices"

	"github.com/alphaleonis/nibs/internal/nib"
)

// Filter holds the metadata fields of a nib filter. Each field is a list of
// values, and an empty list leaves its field unfiltered.
//
// Status values are concrete statuses, compared literally.
type Filter struct {
	Type            []string
	ExcludeType     []string
	Priority        []string
	ExcludePriority []string
	Status          []string
	ExcludeStatus   []string
	Estimate        []string
	ExcludeEstimate []string
	// Tags matches a nib carrying any of the tags.
	Tags []string
	// ExcludeTags drops a nib carrying any of the tags.
	ExcludeTags []string
}

// IsEmpty reports whether f sets no field, so it matches every nib.
func (f Filter) IsEmpty() bool {
	return len(f.Type) == 0 && len(f.ExcludeType) == 0 &&
		len(f.Priority) == 0 && len(f.ExcludePriority) == 0 &&
		len(f.Status) == 0 && len(f.ExcludeStatus) == 0 &&
		len(f.Estimate) == 0 && len(f.ExcludeEstimate) == 0 &&
		len(f.Tags) == 0 && len(f.ExcludeTags) == 0
}

// Matches reports whether n has a listed value for every field of f that sets
// one, and an excluded value for none.
//
// Type and priority compare the effective values, so a nib that omits either
// matches as nib.DefaultType or nib.DefaultPriority. The server's nib filter
// (graph.ApplyFilter) applies these fields through Matches.
func Matches(n *nib.Nib, f Filter) bool {
	return admits(f.Type, f.ExcludeType, n.EffectiveType()) &&
		admits(f.Priority, f.ExcludePriority, n.EffectivePriority()) &&
		admits(f.Status, f.ExcludeStatus, n.Status) &&
		admits(f.Estimate, f.ExcludeEstimate, n.Estimate) &&
		(len(f.Tags) == 0 || sharesAny(n.Tags, f.Tags)) &&
		!sharesAny(n.Tags, f.ExcludeTags)
}

// admits reports whether value is in include, or include is empty, and is not
// in exclude.
func admits(include, exclude []string, value string) bool {
	return (len(include) == 0 || slices.Contains(include, value)) && !slices.Contains(exclude, value)
}

func sharesAny(values, set []string) bool {
	return slices.ContainsFunc(values, func(v string) bool { return slices.Contains(set, v) })
}
