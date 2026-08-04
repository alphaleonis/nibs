package graph

import (
	"fmt"

	"github.com/alphaleonis/nibs/internal/nib"
)

// FilterTargetNotFoundError reports that a filter field naming a single nib was
// given an id no nib answers to — `--parent nibs-typo`, `ancestorId: "gone"`.
// The id came from the caller, so this is a question that cannot be answered
// rather than a question whose answer is "nothing".
//
// Telling the two apart is the whole point of the type. An empty list is a
// factual claim about the store ("nothing is under that nib"), and an agent
// that receives one for a mistyped id has no way to find out it asked the wrong
// question.
//
// It carries nib.ErrNotFound through Unwrap, which is the project's existing
// not-found channel: the GraphQL error presenter tags errors.Is(err,
// nib.ErrNotFound) with extensions.code = "NOT_FOUND" (cmd/serve.go), and the
// CLI boundary maps that code to exit 3 (internal/output). Nothing new needs
// to learn about this type to classify it correctly.
type FilterTargetNotFoundError struct {
	// Field is the GraphQL filter field that carried the target, e.g.
	// "parentId" — the same spelling in the schema, so a message naming it
	// points at something the caller can look up.
	Field string
	// ID is the target exactly as supplied, before normalization: echoing back
	// what was typed is what makes a typo visible.
	ID string
}

func (e *FilterTargetNotFoundError) Error() string {
	return fmt.Sprintf("%s filter: no nib with id %q", e.Field, e.ID)
}

// Unwrap reports nib.ErrNotFound so this error travels the project's existing
// not-found path. It deliberately does not carry a reader error as its cause:
// the class is decided by NormalizeID missing, not by a failed fetch.
func (e *FilterTargetNotFoundError) Unwrap() error { return nib.ErrNotFound }

// FilterTargetUnreadableError reports that a filter target resolved and then
// could not be fetched — the reader answered NormalizeID for the id and refused
// Get for it moments later. The concurrent-delete window between the two is how
// this arises in practice.
//
// It is a separate class from FilterTargetNotFoundError because the two have
// opposite audiences. An unknown target is the caller's mistake and is fixed by
// correcting the id; an unreadable one says the store changed underneath a query
// that had already validated its input, and the same request repeated may well
// succeed. Reporting a transient delete as a not-found would tell an agent its
// id was wrong when it was not.
//
// It deliberately implements NO Unwrap method, and that absence is the whole
// safety property. The reader failure it carries is normally nib.ErrNotFound
// (Core.Get returns nothing else), so unwrapping to it would make
// errors.Is(err, nib.ErrNotFound) true — collapsing this class back into the
// not-found one at every classifier in the project, none of which would be
// wrong to trust that channel. ReaderErr stays inspectable as a field and is
// rendered into the message, so nothing is lost for diagnosis.
//
// The field is deliberately not named Err or Cause: both read as "the thing you
// unwrap to" and would invite the one-line Unwrap that breaks the type.
type FilterTargetUnreadableError struct {
	// Field is the GraphQL filter field that carried the target, e.g.
	// "siblingId".
	Field string
	// ID is the normalized (full) target id — unlike the not-found case there
	// is nothing wrong with the spelling, so the resolved form is the useful one.
	ID string
	// ReaderErr is the reader failure, kept for diagnosis only. It is not
	// exposed via Unwrap; see the type comment for why that must stay true.
	ReaderErr error
}

func (e *FilterTargetUnreadableError) Error() string {
	return fmt.Sprintf("%s filter: target %q became unreadable while filtering: %v", e.Field, e.ID, e.ReaderErr)
}
