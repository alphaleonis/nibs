package graph

import (
	"github.com/alphaleonis/nibs/internal/nib"
)

// existingMentionIDs extracts the ID field from a []*nib.Nib slice, dropping any
// element whose nib no longer exists in the store, and returns a non-nil,
// possibly-empty []string. Shared by the MentionIds/MentionedByIds GraphQL
// resolvers so the two sites can't drift.
//
// The existence filter mirrors what the mentions/mentionedBy OBJECT resolvers
// already do (they skip a mention whose GetSnapshot returns !ok): a nib deleted
// BEFORE the request no longer appears in either the id list or the object list.
// The two fields can still momentarily disagree if a delete lands between their
// two separate existence probes — `mentionIds` and `mentions` are independent
// field resolvers, each probing under its own RLock at its own execution time,
// and the RequestCache memoizes the mention slice, not the existence result.
// Full cross-field consistency would require deriving both fields from a single
// per-request existence snapshot. Existence is probed with NormalizeID (an
// RLock'd map lookup, no clone) rather than GetSnapshot, since only existence —
// not the nib's fields — is needed here.
//
// Always returns a non-nil slice so agent consumers (and downstream JSON
// marshalling) get `[]` instead of `null` for empty mention lists — matches
// the empty-array contract used by show --json and links --json.
func existingMentionIDs(reader NibReader, nibs []*nib.Nib) []string {
	ids := make([]string, 0, len(nibs))
	for _, m := range nibs {
		if _, ok := reader.NormalizeID(m.ID); ok {
			ids = append(ids, m.ID)
		}
	}
	return ids
}
