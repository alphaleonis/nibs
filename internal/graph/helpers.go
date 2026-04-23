package graph

import (
	"github.com/alphaleonis/nibs/internal/nib"
)

// MentionIDList extracts the ID field from a []*nib.Nib slice and returns a
// non-nil, possibly-empty []string. Shared by the MentionIds/MentionedByIds
// GraphQL resolvers and cmd/show's --json envelope builder so the two sites
// can't drift (e.g. if one starts filtering archived mentions and the other
// doesn't).
//
// Always returns a non-nil slice so agent consumers (and downstream JSON
// marshalling) get `[]` instead of `null` for empty mention lists — matches
// the empty-array contract used by show --json and refs --both --json.
func MentionIDList(nibs []*nib.Nib) []string {
	ids := make([]string, 0, len(nibs))
	for _, m := range nibs {
		ids = append(ids, m.ID)
	}
	return ids
}
