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

// FilterTargetEmptyError reports that a filter field naming a single nib was
// given the empty string — `nibs(filter:{parentId:""})`. It is the caller's
// input that is malformed, so this is the validation class (exit 2), not a
// not-found.
//
// The distinction from FilterTargetNotFoundError is that nothing was mistyped.
// An unknown id is a plausible id that happens to name no nib, and the repair is
// to correct it; the empty string names no nib and never could, so no store
// state would make the same query succeed. The way it arrives says the same
// thing: an empty id is what a client sends when a variable did not interpolate
// (`--parent "$ID"` with ID unset), which is a bug in the caller rather than a
// question about the store.
//
// Refusing it at all is the point. Nothing else in a *ID branch rejects an empty
// id — read as "unset" it would drop the branch and widen the query to the WHOLE
// STORE, a confident factual answer to a question that was never asked.
// cmd/list.go already refuses the same input on the flag surface (`--parent ""`),
// and the two surfaces must not disagree about one user error.
//
// Only the EXACT empty string is this class. A whitespace-only value is an id
// like any other: it travels into NormalizeID, resolves to nothing, and is
// reported as FilterTargetNotFoundError. That keeps resolveFilterID free of a
// trimming policy the not-found error's echoed id would otherwise contradict,
// and matches cmd/list.go's own exact `== ""` tests.
//
// Like FilterTargetUnreadableError it implements NO Unwrap, and for the same
// reason: unwrapping to nib.ErrNotFound would make errors.Is(err,
// nib.ErrNotFound) true and collapse this back into the not-found class at every
// classifier keyed on that channel — reporting exit 3 ("that id names no nib")
// for input that names nothing at all. cmd/errors.go's filterTargetErrCode
// recognizes the concrete type instead and maps it to VALIDATION_ERROR.
type FilterTargetEmptyError struct {
	// Field is the GraphQL filter field that was given the empty value, e.g.
	// "parentId" — the same spelling in the schema. There is no ID field: the
	// value is empty by definition of the type.
	Field string
}

func (e *FilterTargetEmptyError) Error() string {
	if e.Field == "parentId" {
		// Named here because cmd/list.go gives --parent "" the very same
		// redirection, so the flag surface and the graph surface answer one
		// user error with one hint. parentId is not the only field with a
		// presence-filter twin, and it is not even the closest one: hasBlockedBy
		// and blockedById read the same raw blocked_by field, which hasParent
		// and parentId do not (hasParent asks whether the link RESOLVES — see
		// the parentId branch in filters.go for that divergence and why it is
		// deliberate). blockedById has no flag to agree with, though, so a hint
		// there would exist on one surface only.
		return "parentId filter: empty id; it takes a nib id — use hasParent: false to select nibs that have no parent"
	}
	return fmt.Sprintf("%s filter: empty id; it takes a nib id", e.Field)
}

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
