package graph

import (
	"fmt"
	"strconv"
	"unicode/utf8"

	"github.com/alphaleonis/nibs/internal/nib"
)

// maxEchoedIDBytes caps how much of a caller-supplied id a refusal message
// repeats. It holds an ordinary id — config.Nibs.Prefix plus
// config.Nibs.IDLength characters — several times over; what it excludes is a
// body or description blob interpolated into an id slot.
const maxEchoedIDBytes = 64

// echoID renders one caller-supplied id for a refusal message — quoted whole
// when it fits maxEchoedIDBytes, abbreviated with the original length when it
// does not.
//
// Cap the RENDERING, not the ID field: one refused relationship-field filter
// mints an error per parent nib, each carrying its own copy of this message,
// while their ID fields share the one string the filter argument parsed.
//
// The cut is measured in BYTES and taken on a rune boundary; already-invalid
// input exhausts the backoff and is sliced anyway. Quoting escapes an
// unprintable byte to four characters, so the echoed fragment can reach four
// times maxEchoedIDBytes.
func echoID(id string) string {
	if len(id) <= maxEchoedIDBytes {
		return strconv.Quote(id)
	}
	// cut indexes the first EXCLUDED byte, so the slice ends on a boundary
	// exactly when that byte starts a rune.
	cut := maxEchoedIDBytes
	for range utf8.UTFMax - 1 {
		if utf8.RuneStart(id[cut]) {
			break
		}
		cut--
	}
	return fmt.Sprintf("%s... (truncated from %d bytes)", strconv.Quote(id[:cut]), len(id))
}

// FilterTargetNotFoundError reports that a filter field naming a single nib was
// given an id no nib answers to — `--parent nibs-typo`, `ancestorId: "gone"`.
type FilterTargetNotFoundError struct {
	// Field is the GraphQL filter field that carried the target, e.g.
	// "parentId" — the same spelling as in the schema.
	Field string
	// ID is the target exactly as supplied, before normalization, and is held
	// in full however long it is; only Error() abbreviates.
	ID string
}

func (e *FilterTargetNotFoundError) Error() string {
	return fmt.Sprintf("%s filter: no nib with id %s", e.Field, echoID(e.ID))
}

// Unwrap reports nib.ErrNotFound, which the GraphQL error presenter tags as
// extensions.code "NOT_FOUND" (cmd/serve.go) and the CLI maps to exit 3. No
// reader error is carried: the class is decided by NormalizeID missing, not by
// a failed fetch.
func (e *FilterTargetNotFoundError) Unwrap() error { return nib.ErrNotFound }

// FilterTargetEmptyError reports that a filter field naming a single nib was
// given the empty string — `nibs(filter:{parentId:""})`. No store state would
// make the same query succeed, so it is the validation class (exit 2).
//
// Do not read an empty id as "unset": the branch would skip itself and widen
// the query to the WHOLE STORE. Only the EXACT empty string is this class, as
// in cmd/list.go's own `== ""` tests — a whitespace-only value is an ordinary
// id and is reported as FilterTargetNotFoundError.
//
// Do not add Unwrap: errors.Is(err, nib.ErrNotFound) would then be true and
// collapse this into the not-found class at every classifier keyed on that
// channel, reporting exit 3.
type FilterTargetEmptyError struct {
	// Field is the GraphQL filter field that was given the empty value, e.g.
	// "parentId" — the same spelling as in the schema.
	Field string
}

func (e *FilterTargetEmptyError) Error() string {
	if e.Field == "parentId" {
		// cmd/list.go gives --parent "" the same redirection, so both surfaces
		// answer one user error with one hint. blockedById, the closest twin,
		// has no list flag to agree with, so a hint there would exist on one
		// surface only.
		return "parentId filter: empty id; it takes a nib id — use hasParent: false to select nibs that have no parent"
	}
	return fmt.Sprintf("%s filter: empty id; it takes a nib id", e.Field)
}

// FilterTargetContradictionError reports that an id-valued filter field was
// combined with the presence field covering the same relationship, set to false
// — `nibs(filter:{parentId:"nibs-9kvw", hasParent:false})`. It is the validation
// class (exit 2), the class cmd/list.go gives the flag spelling (`--parent X
// --no-parent`).
//
// Do not add Unwrap — see FilterTargetEmptyError.
type FilterTargetContradictionError struct {
	// Field is the id-valued GraphQL filter field, e.g. "parentId" — the same
	// spelling as in the schema.
	Field string
	// PresenceField is the tri-state field it contradicts, e.g. "hasParent".
	PresenceField string
	// ID is the target exactly as supplied, never the empty string —
	// refuseContradiction leaves an empty id to FilterTargetEmptyError.
	ID string
}

func (e *FilterTargetContradictionError) Error() string {
	return fmt.Sprintf("%s filter: contradicts %s: false — every nib matching %s %s satisfies %s: true, so nothing can match both",
		e.Field, e.PresenceField, e.Field, echoID(e.ID), e.PresenceField)
}

// FilterTargetTypeError reports that a filter field naming a nib of one
// particular type was given an id that resolves to a nib of another —
// `nibs(filter:{milestone:"nibs-e1"})` where nibs-e1 is an epic. It is the
// validation class (exit 2), the class the WRITE surface gives the same mistake:
// validateAndSetMilestone refuses `nibs set --milestone <epic>` with a message
// of the same shape.
//
// Do not add Unwrap — see FilterTargetEmptyError.
type FilterTargetTypeError struct {
	// Field is the GraphQL filter field that carried the target, e.g.
	// "milestone" — the same spelling as in the schema.
	Field string
	// ID is the normalized (full) target id — the spelling is fine, so the
	// resolved form is the useful one.
	ID string
	// Got is the target's effective type; Want is the type the field requires.
	Got  string
	Want string
}

func (e *FilterTargetTypeError) Error() string {
	return fmt.Sprintf("%s filter: target %s has type %s, not %s", e.Field, echoID(e.ID), e.Got, e.Want)
}

// FilterTargetUnreadableError reports that a filter target resolved and then
// could not be fetched — the reader answered NormalizeID for the id and refused
// Get for it moments later, which is the concurrent-delete window.
//
// It is a separate class from FilterTargetNotFoundError because the id was
// right: the store changed under a query that had already validated its input,
// and the same request repeated may succeed.
//
// Do not add Unwrap — see FilterTargetEmptyError; the reader failure carried
// here is normally nib.ErrNotFound (Core.Get returns nothing else). Keep the
// field named ReaderErr: Err or Cause read as "the thing you unwrap to".
type FilterTargetUnreadableError struct {
	// Field is the GraphQL filter field that carried the target, e.g.
	// "siblingId".
	Field string
	// ID is the normalized (full) target id: NormalizeID answered for it, so it
	// is a store id rather than caller text.
	ID string
	// ReaderErr is the reader failure, kept for diagnosis and rendered into the
	// message. It is not unwrapped.
	ReaderErr error
}

func (e *FilterTargetUnreadableError) Error() string {
	return fmt.Sprintf("%s filter: target %s became unreadable while filtering: %v", e.Field, echoID(e.ID), e.ReaderErr)
}

// FilterAreaError reports that the area filter was given a value the store's
// declared vocabulary does not hold — `nibs(filter:{area:"nosuch"})`, or the
// empty string. It is the validation class (exit 2); on the write surface
// Core.ValidateArea refuses the same value.
//
// Do not add Unwrap — see FilterTargetEmptyError.
//
// Its message is worded for a FILTER rather than reusing config.AreaError, which
// prescribes `nibs set` escapes for a nib whose stored value is refused. It
// restates no rule: membership is Areas.IsValid's, whether the axis is in use at
// all is Areas.Declared's, and the declared set is rendered by Areas.List.
type FilterAreaError struct {
	// Field is the GraphQL filter field that carried the value — "area", the
	// same spelling as in the schema.
	Field string
	// Path is the refused value, already through config.RenderAreaPath. It is
	// empty exactly when the caller supplied the empty string.
	Path string
	// Declared is the vocabulary as Areas.List renders it, empty when the store
	// declares none.
	Declared string
}

func (e *FilterAreaError) Error() string {
	switch {
	case e.Path == "":
		return fmt.Sprintf("%s filter: empty value; it takes a declared area path", e.Field)
	case e.Declared == "":
		return fmt.Sprintf("%s filter: %q is not a declared area: this store declares no areas — declare an `areas:` block in the store's areas.yml before filtering by one",
			e.Field, e.Path)
	default:
		return fmt.Sprintf("%s filter: %q is not a declared area; must be one of %s", e.Field, e.Path, e.Declared)
	}
}
