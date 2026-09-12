package graph

import (
	"github.com/alphaleonis/nibs/internal/nib"
)

// existingMentionIDs extracts the ID field from a []*nib.Nib, dropping any
// element whose nib no longer exists in the store. Shared by the
// MentionIds/MentionedByIds resolvers so the two sites can't drift.
//
// The existence filter mirrors what the mentions/mentionedBy OBJECT resolvers do,
// so a nib deleted BEFORE the request appears in neither list. The two can still
// momentarily disagree when a delete lands between their separate probes: they are
// independent field resolvers, and the RequestCache memoizes the mention slice,
// not the existence result.
//
// The returned slice is never nil, so an empty mention list marshals as `[]`, not
// `null` — the empty-array contract show --json and links --json use.
func existingMentionIDs(reader NibReader, nibs []*nib.Nib) []string {
	ids := make([]string, 0, len(nibs))
	for _, m := range nibs {
		if _, ok := reader.NormalizeID(m.ID); ok {
			ids = append(ids, m.ID)
		}
	}
	return ids
}
