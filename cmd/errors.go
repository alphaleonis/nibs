package cmd

import (
	"errors"

	"github.com/vektah/gqlparser/v2/gqlerror"

	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/output"
)

// reportErr returns either a text-path error or a JSON-envelope error based
// on the jsonMode flag. Keeps the CLI's dual-path error convention in one
// place — every command that has a --json flag should use this rather than
// inlining the check.
//
// Both paths carry the structured code to the CLI boundary (reportExitError)
// so the exit status is code-driven in both modes:
//
//   - jsonMode true: output.Error writes the {error:{code,message}} envelope
//     to stdout and returns a reported CodedError (the boundary suppresses
//     its stderr print).
//   - jsonMode false: return a non-reported CodedError carrying the code and
//     message. The boundary owns the stderr "Error: <msg>" print; only the
//     exit status is now mapped from the code.
func reportErr(jsonMode bool, code string, err error) error {
	if jsonMode {
		return output.Error(code, err.Error())
	}
	// Wrap the cause (Err) so callers' errors.Is/As chains survive; the
	// boundary recovers the code via errors.As and prints Msg to stderr.
	return &output.CodedError{Code: code, Msg: err.Error(), Err: err}
}

// filterTargetErrCode maps the filter-target failures graph.ApplyFilter
// distinguishes onto the CLI's structured error codes, so a query that could
// not be answered exits differently from one that was answered with nothing.
// It reports ok=false for every other error, leaving the caller's own fallback
// in charge.
//
//   - An id no nib answers to is NOT_FOUND (exit 3), recognized through
//     nib.ErrNotFound and not the concrete type — the same channel the GraphQL
//     error presenter keys on (cmd/serve.go), so the CLI and the HTTP server
//     classify one filter failure the same way.
//   - A target that resolved and then could not be read is FILE_ERROR (exit 5),
//     the io/internal class: reporting a concurrent delete as a not-found would
//     tell an agent it typed the wrong id when it did not.
//   - An id-valued field given the EMPTY STRING is VALIDATION_ERROR (exit 2):
//     the input is malformed, nothing was mistyped, and no store state would
//     make the same query succeed. cmd/list.go gives `--parent ""` that exit on
//     the flag surface, so the graph layer and the flag layer agree.
//   - An id-valued field combined with its presence twin set to false is
//     VALIDATION_ERROR (exit 2) for the same reason; cmd/list.go gives
//     `--parent X --no-parent` exit 2 too.
//   - An id-valued field requiring a nib of one type and given one of another
//     (milestone naming an epic) is VALIDATION_ERROR (exit 2): the id is real,
//     so it is not a not-found, and `nibs set --milestone` gives the same
//     mistake the same class.
//   - The area filter given a path the store's vocabulary does not declare is
//     VALIDATION_ERROR (exit 2), arrived at from the other direction: an area is
//     a declared path rather than a nib, so there is no lookup to miss and
//     nothing is a not-found — and `nibs set --area` already refuses the same
//     value with that class.
//
// Those last four reuse VALIDATION_ERROR because a distinct code is worth
// minting only when it carries something an agent can act on that the exit
// status does not. HIERARCHY earns one: its envelope carries the parent types
// that would be accepted. A refused filter argument has no repair beyond the
// field name the message already gives.
//
// The branches are independent, not ordered: none of
// graph.FilterTargetUnreadableError, graph.FilterTargetEmptyError,
// graph.FilterTargetContradictionError, graph.FilterTargetTypeError and
// graph.FilterAreaError carries nib.ErrNotFound (see their doc comments — the
// absent Unwrap is the whole safety property in each), so no branch can claim
// another's error.
//
// This is the READ path's classifier: cmd/list.go and cmd/rel.go call it
// directly with a graph.ApplyFilter failure. A mutation not-found is
// mutationErrCode's, and graphQLErrCode consults that one first, so no
// not-found cause reaches the branches below by way of `nibs query`.
//
// CANONICAL INVARIANT (the read-path filter-failure error classes). This doc is
// its single authoritative statement; the filter-error types in internal/graph
// defer here rather than re-derive it.
func filterTargetErrCode(err error) (string, bool) {
	var unreadable *graph.FilterTargetUnreadableError
	if errors.As(err, &unreadable) {
		return output.ErrFileError, true
	}
	var empty *graph.FilterTargetEmptyError
	if errors.As(err, &empty) {
		return output.ErrValidation, true
	}
	var contradiction *graph.FilterTargetContradictionError
	if errors.As(err, &contradiction) {
		return output.ErrValidation, true
	}
	var wrongType *graph.FilterTargetTypeError
	if errors.As(err, &wrongType) {
		return output.ErrValidation, true
	}
	var badArea *graph.FilterAreaError
	if errors.As(err, &badArea) {
		return output.ErrValidation, true
	}
	if errors.Is(err, nib.ErrNotFound) {
		return output.ErrNotFound, true
	}
	return "", false
}

// graphQLErrCode maps ONE GraphQL error onto the CLI's structured error codes so
// `nibs query` reports the same class as the direct command that raises the same
// failure. `nibs list --parent nope` exits 3 and a stale `nibs set --if-match`
// exits 4; the general-purpose query surface — the one agents script against —
// must not flatten either to a bare validation error.
//
// The classification runs over the Go error CHAIN, not over extensions.code. The
// project's own codes (NOT_FOUND, ETAG_MISMATCH, FILTER_CONTRADICTION) are
// stamped only by the error presenter installed on the HTTP handler
// (cmd/serve.go), and the in-process executor behind `nibs query` runs gqlgen's
// default presenter, so extensions is empty for every resolver-raised error here.
// It is not empty in general — gqlgen calls errcode.Set for its own parse and
// validation failures. The chain always survives: gqlgen presents
// gqlerror.WrapPath(graphql.GetPath(ctx), err), whose *gqlerror.Error.Err
// implements Unwrap, so errors.As sees through to the concrete failure.
//
// Calling both classifiers here constrains neither surface: filterTargetErrCode
// is called by the READ commands alone (cmd/list.go, cmd/rel.go), and each
// mutating command still maps its own errors. Parity is therefore a per-command
// obligation held up by tests naming both exits, with cmd/mv_test.go's
// TestMvUnknownIdIsNotFound as the worked example.
//
// Their order is inert: the two do not overlap on any reachable value.
// mutationErrCode's sentinel test claims every not-found cause, including
// *graph.FilterTargetNotFoundError's (it Unwraps to nib.ErrNotFound), which
// leaves filterTargetErrCode the refusal types that implement no Unwrap and so
// carry no sentinel. What holds the classes apart is mutationErrCode's own
// branch order, documented there.
//
// Everything unrecognized reports ok=false and keeps the caller's
// VALIDATION_ERROR. gqlgen's own parse and validation failures belong there:
// they are mistakes in the caller's document (exit 2), and their
// GRAPHQL_PARSE_FAILED / GRAPHQL_VALIDATION_FAILED codes are not output.Err*
// constants, so mapping them through would collapse them to exit 1.
func graphQLErrCode(err error) (string, bool) {
	if code, ok := mutationErrCode(err); ok {
		return code, true
	}
	return filterTargetErrCode(err)
}

// soleClassifiedErr returns the one error in a GraphQL response that
// graphQLErrCode recognized, provided its class is code — the class the response
// as a whole reports. It returns nil otherwise. It is what lets a caller
// attribute the response to a concrete failure and read a repair hint off it —
// the server's current etag on a CONFLICT, say.
//
// Two conditions, both about attribution being honest:
//
//   - Exactly one classified error. Zero leave nothing to attribute; two or more
//     would force a pick, and the hint is single-valued while the failures are
//     not — one top-level currentEtag cannot represent N per-mutation etags in a
//     batch.
//   - Its class is the response's. graphQLResponseCode requires every error to
//     agree; this requires exactly one to be classified. Without the code check
//     a lone CONFLICT beside an unclassified failure would come back as the
//     cause of an UNCATEGORIZED response, and its currentEtag as the fix for a
//     document whose other failure it does not touch. Passing code in makes a
//     cause that contradicts its own response unrepresentable.
//
// The code check also covers the GENERALIZED response, where graphQLResponseCode
// reports a class rather than a kind: a lone HIERARCHY beside an unrelated
// failure makes the response VALIDATION_ERROR, so nothing is attributed and no
// allowedParentTypes hint escapes onto a response that is not claiming one.
//
// VALIDATION_ERROR is the one class where a non-nil result does NOT establish
// that the response held a single failure: it is both a classified code and the
// default an unclassified error takes, so a classified empty filter target can
// sit beside an unrelated resolver failure and still agree. For every other code
// a non-nil result means the response held exactly one error, which is what the
// consumers in cmd/graphql.go gate on — output.ErrConflict, output.ErrHierarchy,
// output.ErrTextNotFound and output.ErrTextAmbiguous.
//
// The scan runs over the FULL list for the same reason graphQLResponseCode's
// does.
func soleClassifiedErr(errs gqlerror.List, code string) error {
	var sole error
	var soleCode string
	for _, e := range errs {
		c, ok := graphQLErrCode(e)
		if !ok {
			continue
		}
		if sole != nil {
			return nil
		}
		sole, soleCode = e, c
	}
	// Also covers the no-classified-error case: soleCode is "" and no response
	// code is ever empty.
	if soleCode != code {
		return nil
	}
	return sole
}

// graphQLResponseCode decides the single structured code for a whole GraphQL
// error response, in three tiers:
//
//   - Every error agrees on a code → that code. One refusal, or N of the same
//     kind, is reported as exactly what it is, which is what keeps a lone
//     failure's repair hint attributable (see soleClassifiedErr).
//   - The codes differ but share an EXIT STATUS → the general member of that
//     class (output.GeneralCode). A HIERARCHY beside a plain VALIDATION_ERROR is
//     the case: both are caller-input faults exiting 2, so the class is
//     well-supported while neither specific claim is.
//   - Otherwise → UNCATEGORIZED (exit 1).
//
// Agreement is the rule because an exit status is a claim about the response as
// a whole, and a mixed response supports no single one. A NOT_FOUND alongside a
// FILE_ERROR exiting 3 would tell an agent it typed a bad id while the message
// beside it reports an IO failure it must not retry past.
//
// The comparison runs over the exit STATUS, not the code string: output.ExitCode
// is many-to-one — ErrValidation, ErrInvalidStatus, ErrHierarchy, ErrTextNotFound
// and ErrTextAmbiguous all collapse to exit 2 — so comparing strings would report
// exit 1 for a response whose failures the direct commands all exit 2 for,
// destroying the parity `nibs query` exists to hold.
//
// The disagreement code is UNCATEGORIZED, not VALIDATION_ERROR, because exit 2 is
// a specific claim that the CALLER's input was at fault ("validation error (bad
// input, hierarchy violation, text-not-found/ambiguous)" in
// cmd/prompt-full.tmpl). A stale if-match beside a missing id reports no input
// fault at all. Declining to classify costs an exit status an agent can branch
// on, and that cost is bounded: every non-zero code contracts for "STOP,
// diagnose, never silently retry" (cmd/prompt.tmpl), and each distinct message
// is still rendered.
//
// ERROR ORDER IS NOT USABLE HERE. _Mutation assigns out.Values[i] in document
// order, but _Query registers each root field through out.Concurrently and
// graphql.AddError appends in completion order, so a query's error order is not
// stable across runs. executeQuery has no query/mutation branch, so both kinds
// reach this function.
//
// An unrecognized error counts as VALIDATION_ERROR for the comparison, so a
// recognized error paired with an unrecognized one either generalizes (when it
// shares exit 2) or disagrees. The empty list is VALIDATION_ERROR by the same
// defaulting; formatGraphQLErrors never calls it with one.
//
// The scan runs over the FULL list, not the message-deduplicated one that
// formatGraphQLErrors renders: dedup keys on message text, which nothing ties to
// the code, so a deduplicated scan could miss a code behind an identical message.
//
// PRECONDITION: every code graphQLErrCode returns must name a class — one
// output.ExitCode recognizes, and not ErrUncategorized itself. A code landing on
// the uncategorized exit would exit 1 on its own and disagree with everything
// beside it, so classifying a failure into one reports strictly less than
// leaving it unclassified. Sharing an exit status with another code is what the
// second tier is for; what must hold there is that generalizing lands on a claim
// the response supports, which is output.GeneralCode's contract.
// TestGraphQLErrCodeCodesAggregateWithinTheirExitClass pins both by running this
// function over every pair its corpus can build. A branch classifying a type
// absent from both that test's classified and unclassified lists is not covered
// — add a row for it.
func graphQLResponseCode(errs gqlerror.List) string {
	code := output.ErrValidation
	sameCode := true
	for i, e := range errs {
		c, ok := graphQLErrCode(e)
		if !ok {
			c = output.ErrValidation
		}
		if i == 0 {
			code = c
			continue
		}
		if c == code {
			continue
		}
		if output.ExitCode(c) != output.ExitCode(code) {
			return output.ErrUncategorized
		}
		sameCode = false
	}
	if sameCode {
		return code
	}
	// code is the FIRST error's, but every later one reached here sharing its
	// exit status, so the class — and with it the general member — is the same
	// whichever member code happens to be.
	return output.GeneralCode(code)
}
