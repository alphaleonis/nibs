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
//   - An id no nib answers to is NOT_FOUND (exit 3). It is recognized through
//     nib.ErrNotFound rather than the concrete type, which is the same channel
//     the GraphQL error presenter keys on (cmd/serve.go), so the CLI and the
//     HTTP server classify one filter failure the same way.
//   - A target that resolved and then could not be read is FILE_ERROR (exit 5)
//     — the io/internal class. Reporting a concurrent delete as a not-found
//     would tell an agent it typed the wrong id when it did not.
//   - An id-valued field given the EMPTY STRING is VALIDATION_ERROR (exit 2):
//     the caller's input is malformed, nothing was mistyped, and no store state
//     would make the same query succeed. That is the exit cmd/list.go already
//     gives `--parent ""` on the flag surface, so the graph layer and the flag
//     layer agree about one user error instead of splitting it across two exits.
//   - An id-valued field combined with its presence twin set to false is
//     VALIDATION_ERROR (exit 2) for the same reason, and to agree with the same
//     surface: cmd/list.go gives `--parent X --no-parent` exit 2 too.
//
// Reusing VALIDATION_ERROR rather than minting a code for those last two is
// deliberate: a distinct code is worth minting only when it carries something an
// agent can act on that the exit status does not. HIERARCHY earns its own — the
// envelope carries the parent types that would be accepted with it — while a
// refused filter argument has no repair beyond the field name, which the message
// already gives. Minting one would also change what a LONE such refusal reports
// (see graphQLResponseCode's first tier), for no gain.
//
// The branches are independent, not ordered: none of
// graph.FilterTargetUnreadableError, graph.FilterTargetEmptyError and
// graph.FilterTargetContradictionError carries nib.ErrNotFound (see their doc
// comments — the absent Unwrap is the whole safety property in each), so no test
// can claim another's error.
//
// This is the READ path's classifier: cmd/list.go and cmd/rel.go call it
// directly with a graph.ApplyFilter failure. It is NOT what classifies a
// mutation not-found — mutationErrCode carries its own sentinel test and
// graphQLErrCode consults that one first, so no not-found cause reaches the
// branch below by way of `nibs query`.
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
// The classification runs over the Go error chain, NOT over extensions.code. The
// project's own codes (NOT_FOUND, ETAG_MISMATCH, FILTER_CONTRADICTION) are
// stamped only by the error presenter installed on the HTTP handler
// (cmd/serve.go); the in-process executor behind `nibs query` runs gqlgen's
// default presenter, which stamps nothing, so extensions is empty for every
// resolver-raised error here. It is NOT empty in
// general: gqlgen's executor calls errcode.Set itself for its own parse and
// validation failures, so extensions.code carries GRAPHQL_PARSE_FAILED /
// GRAPHQL_VALIDATION_FAILED on this path too — codes that are not output.Err*
// constants (see the last paragraph). The chain, by contrast, survives: gqlgen
// presents gqlerror.WrapPath(graphql.GetPath(ctx), err), which stores the
// resolver's own error in *gqlerror.Error.Err, and that type implements Unwrap —
// so errors.As and errors.Is see straight through to the concrete failure.
//
// This is the UNION of the two classifiers, and the union — not either half —
// is what a direct command has to apply for its class to match. Delegating to
// both here constrains nothing on the other surface: filterTargetErrCode is
// called by the READ commands alone (cmd/list.go, cmd/rel.go), so no direct
// MUTATION command reaches it, and each mutating command still maps its own
// errors. Parity is therefore a per-command obligation held up by tests naming
// both exits, not a property of this call. `nibs mv` is the worked example —
// alone among the four commands that classify through mutationErrCode it
// pre-checks no id, so an unknown id used to reach only that function's
// concrete-type checks and exit 2 while this surface exited 3. mutationErrCode's
// sentinel branch is what closed that, and cmd/mv_test.go's
// TestMvUnknownIdIsNotFound is what keeps it closed.
//
// The two calls do not overlap on any reachable value, so their order here is
// inert. mutationErrCode's sentinel test claims every not-found cause, including
// *graph.FilterTargetNotFoundError's (it Unwraps to nib.ErrNotFound), which
// leaves filterTargetErrCode to classify the refusal types that implement no
// Unwrap and so carry no sentinel. Swapping the two calls breaks no test today;
// what holds the classes apart is mutationErrCode's own branch order,
// documented there.
//
// Everything unrecognized reports ok=false and keeps the caller's
// VALIDATION_ERROR. gqlgen's own parse and validation failures land there and
// belong there: they are mistakes in the caller's document (exit 2), not
// nib-level refusals, and their GRAPHQL_PARSE_FAILED / GRAPHQL_VALIDATION_FAILED
// codes are not output.Err* constants — mapping them through would collapse them
// to the uncategorized exit 1.
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
// Two conditions, and both are about attribution being honest:
//
//   - Exactly one classified error. Zero leave nothing to attribute. Two or more
//     would force a pick, and the hint is single-valued while the failures are
//     not: one top-level currentEtag cannot represent N per-mutation etags in a
//     batch, and naming either one would hand back a token that reconciles only
//     part of it.
//   - Its class is the response's. The two rules differ — graphQLResponseCode
//     requires every error to agree, this one requires exactly one to be
//     classified — so without the code check a lone CONFLICT sitting beside an
//     unclassified failure would be handed back as the cause of an
//     UNCATEGORIZED response. A caller reading a repair hint off it would take a
//     currentEtag as the fix for a document whose other failure it does not
//     touch. Passing code in makes a cause that contradicts its own response
//     unrepresentable rather than merely unreached.
//
// The code check also disposes of the GENERALIZED response, where
// graphQLResponseCode reports a class rather than a kind: a lone HIERARCHY
// beside an unrelated failure makes the response VALIDATION_ERROR, which is not
// the cause's own code, so nothing is attributed and no allowedParentTypes hint
// escapes onto a response that is not claiming one.
//
// VALIDATION_ERROR is the one class where the two conditions together do NOT
// establish that the response holds a single failure, and three routes now reach
// it: it is a classified code (filterTargetErrCode returns it for an empty
// filter target), it is the code an UNCLASSIFIED error defaults to, and it is
// what a mixture within exit 2 generalizes to. So an empty filter target beside
// an unrelated resolver failure agrees on VALIDATION_ERROR and this function
// hands back the classified one. For that class Err attributes only the failure
// it names — it does not imply the response carries no other.
//
// For every OTHER code the stronger reading holds, and generalizing did not
// weaken it. A second error here would be unclassified (only one is classified,
// by the first condition), so it counts as VALIDATION_ERROR, and a response
// mixing that with a non-VALIDATION_ERROR code reports either UNCATEGORIZED or
// the class's general member — never the classified code itself. So a non-
// VALIDATION_ERROR code plus a non-nil Err means the response held exactly one
// error. Both consumers today (cmd/graphql.go) gate on such a code:
// output.ErrConflict and output.ErrHierarchy.
//
// The scan runs over the FULL list for the same reason graphQLResponseCode's
// does: dedup keys on message text, which nothing ties to the cause, so a
// deduplicated scan could see one classified error where the response holds two.
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
// The comparison runs over the exit STATUS rather than the code STRING because
// the exit status is what this function ultimately decides, and output.ExitCode
// is many-to-one: ErrValidation, ErrInvalidStatus, ErrHierarchy, ErrTextNotFound
// and ErrTextAmbiguous all collapse to exit 2. Comparing strings would report
// UNCATEGORIZED — exit 1 — for a response whose failures the direct commands all
// exit 2 for, destroying the parity `nibs query` exists to hold.
//
// The disagreement code is UNCATEGORIZED, not VALIDATION_ERROR, because exit 2
// is not this CLI's generic class — it is a specific claim that the CALLER's
// input was at fault ("validation error (bad input, hierarchy violation,
// text-not-found/ambiguous)" in cmd/prompt-full.tmpl). A stale if-match beside a
// missing id reports no input fault at all. Exit 1 is the one documented as the
// uncategorized failure, which is exactly what a mixed response is.
//
// Rejected alternatives, all of which turn a response that supports no single
// claim into a confident one:
//
//   - Most-severe-wins. It needs a severity ranking over the code set that
//     nothing else in the CLI defines, and the winning class would still be
//     asserted about errors that do not support it.
//   - First-error-wins. Root MUTATION fields execute in document order — the
//     _Mutation loop in internal/graph/generated.go assigns out.Values[i]
//     synchronously — so the reported class would depend on how the caller
//     ordered the aliases, a property of the document rather than of the
//     failure. Root QUERY fields are worse: _Query registers each one through
//     out.Concurrently, and graphql.FieldSet.Dispatch (gqlgen
//     graphql/fieldset.go) runs all but the first on their own goroutine, while
//     graphql.AddError (graphql/context_response.go) appends under a mutex in
//     completion order — so their error order is not stable across runs at all.
//     executeQuery has no query/mutation branch, so both kinds reach here.
//     gqlgen's own errcode.GetErrorKind is not a precedent for such a rule:
//     graphql/errcode/codes.go registers exactly two kinds and reports
//     KindProtocol if ANY error in the list carries a protocol code, so its
//     answer does not depend on order.
//   - Keeping one of the SPECIFIC codes for a same-exit mixture (HIERARCHY for a
//     hierarchy-plus-validation batch). It asserts an illegal parent type about a
//     failure that is not one, and choosing which of the two to keep is
//     first-error-wins again, with the order problem above.
//   - UNCATEGORIZED for a same-exit mixture — which is what comparing code
//     strings amounts to once two classified codes share an exit. It discards an
//     exit status every failure in the response supports; measured, it turns the
//     exit-2 batch above into exit 1.
//   - Generalizing unconditionally, dropping the first tier. A lone HIERARCHY
//     would then report VALIDATION_ERROR, and with it the allowedParentTypes
//     repair hint: soleClassifiedErr attributes a cause only when its class IS
//     the response's, so generalizing the code withholds the hint. That hint is
//     the whole reason this failure is classified at all.
//
// Declining to classify costs an exit status an agent can branch on, and that
// cost is bounded: the contract for every non-zero code is "STOP, diagnose,
// never silently retry" (cmd/prompt.tmpl), and each distinct message is still
// rendered, so what actually failed is still on the wire.
//
// An unrecognized error counts as VALIDATION_ERROR for the comparison, so a
// recognized error paired with an unrecognized one either generalizes (when it
// shares exit 2) or disagrees. The empty list is VALIDATION_ERROR by the same
// defaulting; formatGraphQLErrors never calls it with one.
//
// The scan runs over the FULL list, not the message-deduplicated one that
// formatGraphQLErrors renders. Dedup is a message concern — it exists to keep N
// identical sentences out of an agent's context — and it keys on text, which
// nothing ties to the code. Scanning the full list cannot miss a code that
// dedup happened to drop behind an identical message.
//
// PRECONDITION: every code graphQLErrCode returns must name a class — one
// output.ExitCode recognizes, and not ErrUncategorized itself. A code landing on
// the uncategorized exit would exit 1 on its own and disagree with everything
// beside it, so classifying a failure into one reports strictly less than
// leaving it unclassified. Sharing an exit status with another code is fine and is what the
// second tier is for; what must hold there is that generalizing lands on a claim
// the response supports, which is output.GeneralCode's contract.
// TestGraphQLErrCodeCodesAggregateWithinTheirExitClass pins both, by running
// this function over every pair its corpus can build. A branch classifying a
// type absent from both that test's classified and unclassified lists is not
// covered — add a row for it.
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
