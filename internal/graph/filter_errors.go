package graph

import (
	"fmt"
	"strconv"
	"unicode/utf8"

	"github.com/alphaleonis/nibs/internal/nib"
)

// maxEchoedIDBytes caps how much of a caller-supplied id a refusal message
// repeats.
//
// 64 bytes is chosen to be far above every id this project mints and far below
// the sizes that make the echo cost anything. An id is config.Nibs.Prefix plus
// config.Nibs.IDLength characters, and IDLength defaults to 4 — "nibs-5k9b" is
// nine bytes — so the cap holds an ordinary id several times over and still
// leaves room for a long project prefix or for the recognizable head of a
// mistyped one. What it excludes is the case the cap exists for: a body or a
// description blob interpolated into an id slot, which is diagnostic in its
// first line and merely large after that.
const maxEchoedIDBytes = 64

// echoID renders one caller-supplied id for a refusal message — quoted whole
// when it fits maxEchoedIDBytes, and an abbreviated form carrying the original
// length when it does not.
//
// The abbreviation is what bounds the response, and it has to be the RENDERING
// rather than the ID field, because the rendering is what a refusal repeats. A
// relationship-field filter runs once per parent nib, so a single refused
// `children(filter:{parentId:...})` mints one error object per nib in the store
// and each carries its own copy of this message, while their ID fields all share
// the one string the filter argument parsed to. Measured on the 89-nib sample
// fixture, a 100 KB id returned an 8.9 MB response. The field itself stays whole
// so a caller can still correlate the error against the id it sent.
//
// The cut is measured in BYTES and taken on a RUNE boundary. Bytes because the
// response size is a byte count and a cap of N runes would let N four-byte runes
// reach 4N bytes. On a boundary because splitting a rune renders a character the
// caller really sent as a bogus \xNN escape, pointing the diagnosis at an
// encoding fault that does not exist. Backing off at most utf8.UTFMax-1 bytes
// reaches a rune start for any valid UTF-8; input that is already invalid there
// exhausts the backoff and gets sliced anyway, which strconv.Quote then escapes
// like any other invalid byte.
//
// Quoting happens HERE rather than at each caller's format verb, so both
// branches quote alike and the abbreviation marker sits outside the quotes where
// it cannot be mistaken for id content. Quoting is also why the bound on the
// MESSAGE is wider than the cap: an unprintable or invalid byte escapes to \xNN,
// four characters for one byte, so the echoed fragment can reach four times
// maxEchoedIDBytes.
//
// Two oversized ids render alike only if they share both the prefix and the
// exact length. The ID field carries the exact value for a Go caller holding the
// error; it is not serialized into a GraphQL response, so a remote caller
// correlates against the id it sent plus the length this message reports.
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
	// what was typed is what makes a typo visible. It is held in full however
	// long it is — only Error() abbreviates, and only past maxEchoedIDBytes —
	// so a caller matching it against the id it sent gets the value it sent.
	ID string
}

func (e *FilterTargetNotFoundError) Error() string {
	return fmt.Sprintf("%s filter: no nib with id %s", e.Field, echoID(e.ID))
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
	// It reaches resolveFilterTarget's callers only after NormalizeID answered
	// for it, so it is a real store id rather than caller text; Error() puts it
	// through the same maxEchoedIDBytes cap anyway, so the two id-echoing
	// refusals cannot drift apart on a property either might later need.
	ID string
	// ReaderErr is the reader failure, kept for diagnosis only. It is not
	// exposed via Unwrap; see the type comment for why that must stay true.
	ReaderErr error
}

func (e *FilterTargetUnreadableError) Error() string {
	return fmt.Sprintf("%s filter: target %s became unreadable while filtering: %v", e.Field, echoID(e.ID), e.ReaderErr)
}
