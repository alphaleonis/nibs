package cmd

import (
	"errors"

	"github.com/vektah/gqlparser/v2/gqlerror"

	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/output"
)

// reportErr returns the error a command with a --json flag should return. Both
// paths carry the structured code to reportExitError, so the exit status is
// code-driven in either mode.
func reportErr(jsonMode bool, code string, err error) error {
	if jsonMode {
		return output.Error(code, err.Error())
	}
	// Wrap the cause (Err) so callers' errors.Is/As chains survive; the
	// boundary recovers the code via errors.As and prints Msg to stderr.
	return &output.CodedError{Code: code, Msg: err.Error(), Err: err}
}

// filterTargetErrCode maps graph.ApplyFilter's target failures onto the CLI's
// structured error codes, reporting ok=false for anything else — the READ path's
// classifier, where a mutation not-found is mutationErrCode's.
//
// Not-found is recognized through nib.ErrNotFound rather than a concrete type,
// the same channel cmd/serve.go's error presenter keys on, so the CLI and the
// HTTP server classify one filter failure alike. Branch order is inert: no
// graph.FilterTarget* refusal type implements Unwrap, so none carries the
// sentinel and no branch can claim another's error.
//
// CANONICAL INVARIANT (the read-path filter-failure error classes). Decide a
// filter failure's class only here. The filter-error types in internal/graph
// name the class each falls into; they do not decide it.
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
// Classification runs over the Go error CHAIN, not extensions.code: the
// project's codes are stamped only by cmd/serve.go's presenter, and the
// in-process executor behind `nibs query` runs gqlgen's default one, so
// extensions is empty for every resolver-raised error here. The chain survives
// because *gqlerror.Error.Err implements Unwrap.
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
// Both conditions are about attribution being honest. Exactly one classified
// error, because the hint is single-valued while the failures are not — one
// top-level currentEtag cannot represent N per-mutation etags in a batch. And its
// class must be the response's, or a lone CONFLICT beside an unclassified failure
// would come back as the cause of an UNCATEGORIZED response, its etag offered as
// the fix for a document it does not touch. That also keeps an allowedParentTypes
// hint off a GENERALIZED response, which reports a class rather than a kind.
//
// VALIDATION_ERROR is the one class where a non-nil result does NOT establish
// that the response held a single failure: it is both a classified code and the
// default an unclassified error takes. For every other code a non-nil result
// means exactly one error, which is what the consumers in cmd/graphql.go gate on.
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
// is many-to-one, so comparing strings would report exit 1 for a response whose
// failures the direct commands all exit 2 for, destroying the parity `nibs query`
// exists to hold.
//
// The disagreement code is UNCATEGORIZED, not VALIDATION_ERROR, because exit 2 is
// a specific claim that the CALLER's input was at fault, and a stale if-match
// beside a missing id reports no input fault at all. Declining to classify costs
// an exit status an agent can branch on, and that cost is bounded: every non-zero
// code contracts for "STOP, diagnose, never silently retry", and each distinct
// message is still rendered.
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
