package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/vektah/gqlparser/v2/gqlerror"

	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/nibtypes"
	"github.com/alphaleonis/nibs/internal/output"
)

// TestReportErr_TextMode verifies that in non-JSON mode reportErr returns a
// non-reported CodedError carrying the code and message (so the CLI boundary
// maps the exit status and prints "Error: <msg>" to stderr) and writes NO
// JSON envelope to stdout.
func TestReportErr_TextMode(t *testing.T) {
	origErr := errors.New("boom")
	var got error
	out := captureStdout(t, func() {
		got = reportErr(false, output.ErrValidation, origErr)
	})
	var ce *output.CodedError
	if !errors.As(got, &ce) {
		t.Fatalf("reportErr(false, ...) = %v, want *output.CodedError", got)
	}
	if ce.Reported {
		t.Errorf("text-mode CodedError.Reported = true, want false (boundary owns the stderr print)")
	}
	if ce.Code != output.ErrValidation {
		t.Errorf("CodedError.Code = %q, want %q", ce.Code, output.ErrValidation)
	}
	if ce.Msg != "boom" {
		t.Errorf("CodedError.Msg = %q, want %q", ce.Msg, "boom")
	}
	if errors.Is(got, output.ErrAlreadyReported) {
		t.Error("text-mode err must NOT satisfy ErrAlreadyReported (nothing written to stdout)")
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("expected NO stdout output in text mode, got: %q", out)
	}
}

// TestReportErr_JSONMode verifies that in JSON mode reportErr writes the
// {error:{code,message}} contract to stdout and returns a reported
// CodedError. This is the single-source-of-truth for the dual-path error
// convention used by every --json-aware command in cmd/.
func TestReportErr_JSONMode(t *testing.T) {
	origErr := errors.New("bad things happened")
	var got error
	out := captureStdout(t, func() {
		got = reportErr(true, output.ErrValidation, origErr)
	})
	if got == nil {
		t.Fatal("reportErr(true, ...) returned nil; expected non-nil coded error")
	}
	if !errors.Is(got, output.ErrAlreadyReported) {
		t.Errorf("JSON-mode err must satisfy ErrAlreadyReported; got %v", got)
	}
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v\nraw: %s", err, out)
	}
	if env.Error.Code != output.ErrValidation {
		t.Errorf("envelope error.code = %q, want %q", env.Error.Code, output.ErrValidation)
	}
	if env.Error.Message != "bad things happened" {
		t.Errorf("envelope error.message = %q, want %q", env.Error.Message, "bad things happened")
	}
}

// TestReportErr_JSONMode_DifferentCodes verifies that reportErr propagates
// the caller-provided code unchanged. Every callsite in refs/show picks a
// code from output.Err* based on the error category — the helper must not
// coerce them.
func TestReportErr_JSONMode_DifferentCodes(t *testing.T) {
	tests := []struct {
		name string
		code string
	}{
		{"validation", output.ErrValidation},
		{"not-found", output.ErrNotFound},
		{"conflict", output.ErrConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := captureStdout(t, func() {
				_ = reportErr(true, tt.code, errors.New("x"))
			})
			var env struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(out), &env); err != nil {
				t.Fatalf("unmarshal: %v\nraw: %s", err, out)
			}
			if env.Error.Code != tt.code {
				t.Errorf("envelope error.code = %q, want %q", env.Error.Code, tt.code)
			}
		})
	}
}

// notFoundErr, unreadableErr, emptyFilterErr, conflictErr and unparseableErr
// construct five of the failures graphQLErrCode classifies — an
// *nibcore.ETagRequiredError and a bare nib.ErrNotFound from a mutation are the
// others. Each is wrapped the way gqlgen wraps a resolver error (gqlerror.Wrap
// sets Err, and *gqlerror.Error implements Unwrap) so the classifier is
// exercised through the same chain the executor produces.
// TestGraphQLErrCodeCodesAggregateWithinTheirExitClass has the full corpus.
func notFoundErr() *gqlerror.Error {
	return gqlerror.Wrap(&graph.FilterTargetNotFoundError{Field: "parentId", ID: "zz"})
}

func unreadableErr() *gqlerror.Error {
	return gqlerror.Wrap(&graph.FilterTargetUnreadableError{
		Field: "siblingId", ID: "gone", ReaderErr: nib.ErrNotFound,
	})
}

func emptyFilterErr() *gqlerror.Error {
	return gqlerror.Wrap(&graph.FilterTargetEmptyError{Field: "parentId"})
}

func contradictionErr() *gqlerror.Error {
	return gqlerror.Wrap(&graph.FilterTargetContradictionError{
		Field: "parentId", PresenceField: "hasParent", ID: "zz",
	})
}

func wrongTypeErr() *gqlerror.Error {
	return gqlerror.Wrap(&graph.FilterTargetTypeError{
		Field: "milestone", ID: "zz", Got: "epic", Want: "milestone",
	})
}

func conflictErr() *gqlerror.Error {
	return gqlerror.Wrap(&nibcore.ETagMismatchError{Provided: "stale", Current: "fresh"})
}

// otherConflictErr is a SECOND, distinct etag mismatch — a different pair of
// tokens, so its message differs and message-dedup cannot collapse the two.
// A response carrying both is the case a single top-level currentEtag cannot
// represent.
func otherConflictErr() *gqlerror.Error {
	return gqlerror.Wrap(&nibcore.ETagMismatchError{Provided: "old", Current: "newer"})
}

func unparseableErr() *gqlerror.Error {
	return gqlerror.Wrap(&nibcore.OnDiskUnparseableError{
		ID: "a", Path: "a.md", Reason: "unparseable", Err: errors.New("bad yaml"),
	})
}

func hierarchyErr() *gqlerror.Error {
	return gqlerror.Wrap(&nibtypes.HierarchyError{
		ChildType: "epic", ParentType: "task", Allowed: []string{"milestone"},
	})
}

// otherHierarchyErr is a SECOND, distinct illegal parent link — a different
// child type, so its allowed set and its message both differ. A response
// carrying both is the case a single top-level allowedParentTypes cannot
// represent.
func otherHierarchyErr() *gqlerror.Error {
	return gqlerror.Wrap(&nibtypes.HierarchyError{
		ChildType: "feature", ParentType: "task", Allowed: []string{"milestone", "epic"},
	})
}

// textNotFoundErr and textAmbiguousErr are the two surgical-replace refusals,
// each wrapped in the "replacement N failed" sentence nib.ApplyBodyMod puts
// around it with %w — the shape the resolver actually returns, so the classifier
// is exercised against a ReplaceMatchError it has to reach through a wrapper.
func textNotFoundErr() *gqlerror.Error {
	return gqlerror.Wrap(fmt.Errorf("replacement 0 failed: %w", &nib.ReplaceMatchError{Count: 0}))
}

func textAmbiguousErr() *gqlerror.Error {
	return gqlerror.Wrap(fmt.Errorf("replacement 0 failed: %w", &nib.ReplaceMatchError{Count: 2}))
}

// TestGraphQLResponseCode pins the rule that decides a whole response's exit
// class: errors agreeing on a code yield that code, errors that merely agree on
// an exit STATUS yield that class's general member, and anything else yields
// UNCATEGORIZED (exit 1). The mixed cases are the point — a response is one exit
// status, and claiming NOT_FOUND for a response that also reports an IO failure
// would tell an agent it typed a bad id when the store is what broke. Claiming
// VALIDATION_ERROR instead would be just as wrong in the other direction: exit 2
// asserts the caller's input was at fault, which a mixed not-found/conflict pair
// does not support.
//
// The middle tier is what keeps HIERARCHY classifiable. It and VALIDATION_ERROR
// are different claims about the same caller-input fault class, so a batch
// holding one of each still supports exit 2 — while neither specific claim
// survives being asserted about the other's failure.
//
// Both orderings of a mixed pair are covered: a rule that only inspected the
// first error, or only the last, would pass one of them.
func TestGraphQLResponseCode(t *testing.T) {
	tests := []struct {
		name string
		errs gqlerror.List
		want string
	}{
		{"empty list", gqlerror.List{}, output.ErrValidation},
		{"single not-found", gqlerror.List{notFoundErr()}, output.ErrNotFound},
		{"single unreadable target", gqlerror.List{unreadableErr()}, output.ErrFileError},
		// The next three rows pin this function's AGGREGATION behavior for the
		// empty-filter input shape — alone, beside an agreeing failure, beside a
		// disagreeing one. They cannot demonstrate that the class is classified
		// at all: filterTargetErrCode returns the same ErrValidation that an
		// UNCLASSIFIED error defaults to here, so deleting the branch leaves all
		// three green. Deleting it fails
		// TestFilterTargetErrCodeClassifiesEveryRefusalClass — the totality
		// guard, which drives every refusal class it finds in internal/graph
		// through filterTargetErrCode — along with
		// TestGraphQLErrCodeCodesAggregateWithinTheirExitClass and
		// TestRelFetchErrCodeClassifiesFilterRefusals. Reach for those when
		// changing the classification, and for these rows when changing how a
		// response's single code is decided.
		{"single empty filter target", gqlerror.List{emptyFilterErr()}, output.ErrValidation},
		{
			// Agreement, not generalization: both sides carry the same code, so
			// the first tier answers. Minting a distinct code for the empty target
			// would still land here through the second tier — both are exit 2 —
			// but would change the row above, where a lone refusal reports its own
			// code.
			"empty filter target beside an unrecognized failure agree",
			gqlerror.List{emptyFilterErr(), gqlerror.Errorf("resolver blew up")},
			output.ErrValidation,
		},
		{
			"empty filter target then not-found disagree",
			gqlerror.List{emptyFilterErr(), notFoundErr()},
			output.ErrUncategorized,
		},
		{"single etag mismatch", gqlerror.List{conflictErr()}, output.ErrConflict},
		{
			"single etag required",
			gqlerror.List{gqlerror.Wrap(&nibcore.ETagRequiredError{})},
			output.ErrConflict,
		},
		{"single uncertifiable file", gqlerror.List{unparseableErr()}, output.ErrFileError},
		{
			"single unrecognized failure",
			gqlerror.List{gqlerror.Errorf("resolver blew up")},
			output.ErrValidation,
		},
		{
			// gqlgen's own protocol errors carry extensions.code but no Go
			// cause. They must stay validation-class: GRAPHQL_PARSE_FAILED is
			// not an output.Err* constant, so mapping the extension through
			// would collapse them to the uncategorized exit 1.
			"gqlgen parse failure alone",
			gqlerror.List{&gqlerror.Error{
				Message:    "Expected Name, found <EOF>",
				Extensions: map[string]any{"code": "GRAPHQL_PARSE_FAILED"},
			}},
			output.ErrValidation,
		},
		{"two agreeing not-founds", gqlerror.List{notFoundErr(), notFoundErr()}, output.ErrNotFound},
		{
			"two agreeing file errors from different types",
			gqlerror.List{unreadableErr(), unparseableErr()},
			output.ErrFileError,
		},
		{
			"not-found then file error disagree",
			gqlerror.List{notFoundErr(), unreadableErr()},
			output.ErrUncategorized,
		},
		{
			"file error then not-found disagree",
			gqlerror.List{unreadableErr(), notFoundErr()},
			output.ErrUncategorized,
		},
		{
			"conflict then not-found disagree",
			gqlerror.List{conflictErr(), notFoundErr()},
			output.ErrUncategorized,
		},
		{
			"recognized then unrecognized disagree",
			gqlerror.List{notFoundErr(), gqlerror.Errorf("resolver blew up")},
			output.ErrUncategorized,
		},
		{
			"unrecognized then recognized disagree",
			gqlerror.List{gqlerror.Errorf("resolver blew up"), notFoundErr()},
			output.ErrUncategorized,
		},
		{
			// The disagreement is only visible at the last element, so a scan
			// that stopped early would report NOT_FOUND.
			"disagreement in the tail",
			gqlerror.List{notFoundErr(), notFoundErr(), conflictErr()},
			output.ErrUncategorized,
		},
		{
			"two agreeing unrecognized failures",
			gqlerror.List{gqlerror.Errorf("one"), gqlerror.Errorf("two")},
			output.ErrValidation,
		},
		// The HIERARCHY rows below walk the boundary the middle tier draws: the
		// specific code survives only where every error agrees on it. Keeping it
		// is what makes an allowedParentTypes hint reachable at all — though
		// reaching it also needs a single failure to attribute, which is
		// soleClassifiedErr's condition, not this function's.
		{"single illegal parent type", gqlerror.List{hierarchyErr()}, output.ErrHierarchy},
		{
			// Two of a kind agree, so the code survives even though their allowed
			// sets differ; withholding the single-valued hint is soleClassifiedErr's
			// job, not this function's.
			"two illegal parent types agree",
			gqlerror.List{hierarchyErr(), otherHierarchyErr()},
			output.ErrHierarchy,
		},
		{
			// Different codes, one exit class: the response generalizes rather than
			// claiming an illegal parent type about the unrecognized failure — and
			// rather than collapsing to exit 1, which is the regression comparing
			// code strings would produce here.
			"illegal parent type beside an unrecognized failure generalizes",
			gqlerror.List{hierarchyErr(), gqlerror.Errorf("resolver blew up")},
			output.ErrValidation,
		},
		{
			// The same pair in the other order. Generalization must not depend on
			// which member the scan met first.
			"unrecognized failure beside an illegal parent type generalizes",
			gqlerror.List{gqlerror.Errorf("resolver blew up"), hierarchyErr()},
			output.ErrValidation,
		},
		{
			// Both are CLASSIFIED and both exit 2, which is the case the old
			// code-string comparison could not express at all.
			"illegal parent type beside an empty filter target generalizes",
			gqlerror.List{hierarchyErr(), emptyFilterErr()},
			output.ErrValidation,
		},
		{
			"illegal parent type then not-found disagree",
			gqlerror.List{hierarchyErr(), notFoundErr()},
			output.ErrUncategorized,
		},
		{
			"illegal parent type then conflict disagree",
			gqlerror.List{hierarchyErr(), conflictErr()},
			output.ErrUncategorized,
		},
		{
			// A third member of the same class after generalization has already
			// happened must not un-generalize the answer back to a specific code.
			"generalization survives an agreeing tail",
			gqlerror.List{hierarchyErr(), emptyFilterErr(), hierarchyErr()},
			output.ErrValidation,
		},
		{
			// …and must not survive a disagreeing one: the scan has to keep
			// comparing exits after it generalizes, not stop at the first mixture.
			"generalization then a disagreeing tail",
			gqlerror.List{hierarchyErr(), emptyFilterErr(), notFoundErr()},
			output.ErrUncategorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := graphQLResponseCode(tt.errs); got != tt.want {
				t.Errorf("graphQLResponseCode() = %q (exit %d), want %q (exit %d)",
					got, output.ExitCode(got), tt.want, output.ExitCode(tt.want))
			}
		})
	}
}

// TestGraphQLErrCodeCodesAggregateWithinTheirExitClass pins graphQLResponseCode's
// stated precondition, and pins it by running the aggregation rather than a
// proxy for it.
//
// It replaces a guard that required no two classified codes to share an exit
// status. That requirement was satisfiable only while the classifier happened to
// produce four codes on four exits: HIERARCHY shares exit 2 with the
// VALIDATION_ERROR an unclassified error defaults to, so keeping the requirement
// would have meant declining to classify the one refusal that carries a repair
// hint. What replaces it is the behavior the old rule was a proxy FOR — for every
// pair the corpus can build, a shared exit status must survive aggregation, a
// split one must not, and the reported code must be the shared code or a claim no
// stronger than the class. The old regression (a same-exit pair reporting
// UNCATEGORIZED, exit 1) fails the second property below; nothing about the
// widened classifier hides it.
//
// classified names one representative of every failure graphQLErrCode is
// expected to recognize; unclassified names the ones that must keep the caller's
// VALIDATION_ERROR. The claim is bounded by that corpus: a new branch is caught
// from whichever side it lands on as long as some row's type reaches it — its
// code is checked against output.ExitCode's vocabulary, its class against the
// row's want, and its aggregation against every other row. A branch matching a
// type named by neither list trips nothing, because no row calls the classifier
// with one. Adding a branch means adding a row.
func TestGraphQLErrCodeCodesAggregateWithinTheirExitClass(t *testing.T) {
	classified := []struct {
		name string
		err  *gqlerror.Error
		want string
	}{
		{"filter target not found", notFoundErr(), output.ErrNotFound},
		{"filter target unreadable", unreadableErr(), output.ErrFileError},
		{
			// Deliberately the SAME code the unclassified default uses, so a
			// response holding both reports it rather than generalizing.
			// Classifying it at all is what the direct commands need:
			// relFetchErrCode's fallback is FILE_ERROR (exit 5), so an
			// unclassified empty id would report a malformed argument as a
			// broken tracker.
			"filter target empty",
			emptyFilterErr(),
			output.ErrValidation,
		},
		{
			// Also the unclassified default's own code, and deliberately so:
			// minting one for the contradictory pairs would buy a second name
			// for the same class and nothing an agent could act on.
			"contradictory filter pair",
			contradictionErr(),
			output.ErrValidation,
		},
		{
			// The third refusal on the unclassified default's own code. It is
			// a real id of the wrong type, so neither the not-found nor the
			// file class fits, and nothing beyond the field name would repair
			// it — the same reasoning the two rows above record.
			"filter target wrong type",
			wrongTypeErr(),
			output.ErrValidation,
		},
		{"mutation etag mismatch", conflictErr(), output.ErrConflict},
		{
			"mutation etag required",
			gqlerror.Wrap(&nibcore.ETagRequiredError{}),
			output.ErrConflict,
		},
		{"on-disk nib unparseable", unparseableErr(), output.ErrFileError},
		{"a stale path under a mutation", gqlerror.Wrap(fmt.Errorf("up91: writing file: %w", fs.ErrNotExist)), output.ErrFileError},
		{
			// The store's prefix moved while a create waited for its write lock,
			// so the process's whole id vocabulary is retired. Same class as the
			// stale path above and for the same reason: the filesystem moved
			// under a loaded process, and the repair is to re-read the store.
			"a store re-prefixed under the lock",
			gqlerror.Wrap(&nibcore.StoreRePrefixedError{Loaded: "old-", Declared: "new-"}),
			output.ErrFileError,
		},
		{
			// Shares exit 2 with the VALIDATION_ERROR default, which is legal
			// only because aggregation compares exits. What the separate code
			// buys is the allowedParentTypes hint the envelope carries with it.
			"an illegal parent type",
			hierarchyErr(),
			output.ErrHierarchy,
		},
		{
			// A mutation aimed at an unknown subject id arrives as a bare
			// sentinel, not as a filter-target type — mutationErrCode's
			// errors.Is tail is what classifies it.
			"mutation target not found",
			gqlerror.Wrap(nib.ErrNotFound),
			output.ErrNotFound,
		},
		{
			// The two surgical-replace refusals share exit 2 with each other
			// AND with the VALIDATION_ERROR default — three codes on one exit,
			// which only an exit-comparing aggregation can express. What the
			// separate codes buy is the occurrences count the envelope carries
			// with them, and the ability to tell "your text was not there" from
			// "your text was ambiguous".
			"a surgical replace that matched nothing",
			textNotFoundErr(),
			output.ErrTextNotFound,
		},
		{
			"a surgical replace that matched more than once",
			textAmbiguousErr(),
			output.ErrTextAmbiguous,
		},
	}
	unclassified := []struct {
		name string
		err  *gqlerror.Error
	}{
		{"a resolver failure with no nib-level class", gqlerror.Errorf("resolver blew up")},
		{
			// gqlgen's own protocol failures carry extensions.code but no Go
			// cause; the classifier reads the chain, so they stay unclassified.
			"a gqlgen protocol failure",
			&gqlerror.Error{
				Message:    "Expected Name, found <EOF>",
				Extensions: map[string]any{"code": "GRAPHQL_PARSE_FAILED"},
			},
		},
	}

	// corpus pairs every representative with the code graphQLResponseCode sees
	// for it: the classified code, or the VALIDATION_ERROR an unclassified error
	// defaults to. The default is part of the comparison the rule runs, so it has
	// to be part of the claim.
	type member struct {
		name string
		err  *gqlerror.Error
		code string
	}
	var corpus []member

	for _, tt := range classified {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := graphQLErrCode(tt.err)
			if !ok {
				t.Fatalf("graphQLErrCode() reported unclassified, want %q", tt.want)
			}
			if got != tt.want {
				t.Fatalf("graphQLErrCode() = %q, want %q", got, tt.want)
			}
			// A code output.ExitCode does not know falls through to the
			// uncategorized exit, where it exits 1 alone and disagrees with
			// everything beside it — strictly less than staying unclassified.
			if output.ExitCode(got) == output.ExitError && got != output.ErrUncategorized {
				t.Fatalf("graphQLErrCode() = %q, which output.ExitCode does not recognize "+
					"(exit %d); classifying a failure into it reports less than leaving it "+
					"unclassified — see graphQLResponseCode's PRECONDITION",
					got, output.ExitError)
			}
			if got == output.ErrUncategorized {
				t.Fatalf("graphQLErrCode() = %q; a classified failure has a class by "+
					"definition, so it must not report the code reserved for having none",
					got)
			}
			// A lone failure keeps its own code, which is what makes a repair
			// hint reachable: formatGraphQLErrors attributes a cause only when
			// its class IS the response's.
			if got := graphQLResponseCode(gqlerror.List{tt.err}); got != tt.want {
				t.Errorf("graphQLResponseCode(one %s) = %q, want %q", tt.name, got, tt.want)
			}
		})
		corpus = append(corpus, member{tt.name, tt.err, tt.want})
	}

	for _, tt := range unclassified {
		t.Run(tt.name, func(t *testing.T) {
			if code, ok := graphQLErrCode(tt.err); ok {
				t.Fatalf("graphQLErrCode() classified this as %q (exit %d); it must keep the "+
					"VALIDATION_ERROR default, or graphQLResponseCode's PRECONDITION needs "+
					"rechecking against the new code",
					code, output.ExitCode(code))
			}
		})
		corpus = append(corpus, member{tt.name, tt.err, output.ErrValidation})
	}

	// Every unordered pair the corpus can build, aggregated. The properties are
	// stated in terms of the pair's own codes and exits, not by recomputing the
	// rule, so a rewrite of graphQLResponseCode is judged by what it must deliver:
	//
	//   - Split exit statuses → UNCATEGORIZED. No single claim covers the pair.
	//   - Shared exit status → the response exits there too. This is the parity
	//     `nibs query` exists to hold, and the one a code-string comparison breaks
	//     the moment two classified codes share an exit.
	//   - Equal codes → that code, so a specific refusal is still reported as
	//     one.
	//   - Different codes on one exit → a code that is its own class's general
	//     member, so the response asserts nothing beyond the class. (Which code
	//     that is per class is output.GeneralCode's contract, pinned in
	//     internal/output.)
	//   - Either order → the same answer. Root query fields complete in
	//     nondeterministic order, so an order-sensitive rule would be flaky rather
	//     than merely wrong.
	for i, a := range corpus {
		for _, b := range corpus[i:] {
			t.Run(a.name+" + "+b.name, func(t *testing.T) {
				got := graphQLResponseCode(gqlerror.List{a.err, b.err})
				if reversed := graphQLResponseCode(gqlerror.List{b.err, a.err}); reversed != got {
					t.Errorf("order decides the response: %q one way, %q the other", got, reversed)
				}
				exitA, exitB := output.ExitCode(a.code), output.ExitCode(b.code)
				if exitA != exitB {
					if got != output.ErrUncategorized {
						t.Errorf("graphQLResponseCode() = %q for %q (exit %d) beside %q (exit %d), "+
							"want %q — no single class covers the pair",
							got, a.code, exitA, b.code, exitB, output.ErrUncategorized)
					}
					return
				}
				if output.ExitCode(got) != exitA {
					t.Errorf("graphQLResponseCode() = %q (exit %d) for two failures the direct "+
						"commands both exit %d for; the exit status every error in the response "+
						"supports must survive aggregation",
						got, output.ExitCode(got), exitA)
				}
				if a.code == b.code {
					if got != a.code {
						t.Errorf("graphQLResponseCode() = %q, want %q — agreeing failures keep "+
							"their code, which is what keeps a repair hint attributable",
							got, a.code)
					}
					return
				}
				if general := output.GeneralCode(got); general != got {
					t.Errorf("graphQLResponseCode() = %q for %q beside %q, which is a "+
						"specialization of %q — a mixed class must not assert one member's "+
						"refusal about the other's failure",
						got, a.code, b.code, general)
				}
			})
		}
	}
}

// TestMutationErrCodeBoundaries pins mutationErrCode at every boundary its
// branches touch, including the values that sit BETWEEN two of them. It is the
// direct-command end of the classification: `nibs mv` reaches it through
// setMutationError → mutationError, and `nibs query` reaches the same function
// through graphQLErrCode, so one table covers both surfaces.
//
// The rows carrying a not-found cause under a concrete type are the point, and
// the two error types split on Unwrap. OnDiskUnparseableError implements it, so
// a not-found Err would satisfy the sentinel test — the sentinel test running
// LAST is what keeps that row FILE_ERROR. FilterTargetUnreadableError implements
// none, so its ReaderErr never reaches the sentinel and the row stays
// unclassified here; filterTargetErrCode owns it, and the test below asserts the
// FILE_ERROR it lands on through the union.
//
// The non-wrapping "parent nib not found" row is the secondary-id shape: the
// graph layer formats those with %s, so they carry no sentinel and stay
// validation-class on both surfaces.
func TestMutationErrCodeBoundaries(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string // "" means the branch must report ok=false
	}{
		{"bare not-found sentinel", nib.ErrNotFound, output.ErrNotFound},
		{
			"wrapped not-found sentinel",
			fmt.Errorf("target nib not found: %s: %w", "zz", nib.ErrNotFound),
			output.ErrNotFound,
		},
		{
			"etag mismatch",
			&nibcore.ETagMismatchError{Provided: "stale", Current: "fresh"},
			output.ErrConflict,
		},
		{"etag required", &nibcore.ETagRequiredError{}, output.ErrConflict},
		{"supplied id already exists", &nibcore.IDExistsError{ID: "a"}, output.ErrConflict},
		{
			"unparseable carrying an OS error",
			&nibcore.OnDiskUnparseableError{
				ID: "a", Path: "a.md", Reason: "unreadable", Err: os.ErrPermission,
			},
			output.ErrFileError,
		},
		{
			// The create half of the set-prefix race, classified with the
			// stale-path refusal it mirrors rather than riding `nibs new`'s own
			// FILE_ERROR fallback — which would leave `nibs query` reporting the
			// caller's arguments as the fault (exit 2).
			"a store re-prefixed under the write lock",
			&nibcore.StoreRePrefixedError{Loaded: "old-", Declared: "new-"},
			output.ErrFileError,
		},
		{
			"unparseable carrying a not-found cause",
			&nibcore.OnDiskUnparseableError{
				ID: "a", Path: "a.md", Reason: "unreadable", Err: nib.ErrNotFound,
			},
			output.ErrFileError,
		},
		{
			// The concrete filter-target not-found Unwraps to the sentinel, so
			// the mutation half of the union claims it and graphQLErrCode never
			// reaches filterTargetErrCode with one.
			"filter target not found",
			&graph.FilterTargetNotFoundError{Field: "parentId", ID: "zz"},
			output.ErrNotFound,
		},
		{
			"an illegal parent link",
			&nibtypes.HierarchyError{ChildType: "epic", ParentType: "task", Allowed: []string{"milestone"}},
			output.ErrHierarchy,
		},
		{
			// The graph layer wraps the child-orphaning half of the rule with %w,
			// so this row is what says the branch reads the chain rather than the
			// concrete top-level type.
			"a type change orphaning a child, reported with %w",
			fmt.Errorf("type change would invalidate child %s: %w", "ch",
				&nibtypes.HierarchyError{ChildType: "feature", ParentType: "task", Allowed: []string{"milestone", "epic"}}),
			output.ErrHierarchy,
		},
		{
			// The axis rule is newly reachable from the CREATE path (--area), and
			// create is the one surface whose fallback is FILE_ERROR — so without
			// this branch a bad argument pair exits 5 and sends an agent to look
			// at the filesystem.
			"an assignment axis the type refuses",
			&nibtypes.AxisError{NibType: "milestone", Axis: nibtypes.AxisArea},
			output.ErrValidation,
		},
		{
			"an axis refusal reported with %w",
			fmt.Errorf("failed to create nib: %w",
				&nibtypes.AxisError{NibType: "milestone", Axis: nibtypes.AxisMilestone}),
			output.ErrValidation,
		},
		{
			// An id whose file name reads back as a different id. It reaches this
			// function only wrapped — `nibs new` prefixes "failed to create nib:"
			// — and create is the surface whose fallback is FILE_ERROR, so
			// without the branch a bad --prefix exits 5 and sends the caller to
			// look at the filesystem. Its path-shape sibling already exits 2.
			"an id whose file name does not read back as itself",
			fmt.Errorf("failed to create nib: %w",
				nib.ValidateIDRoundTrip("c.d-h1wy", "", "tnib-")),
			output.ErrValidation,
		},
		{
			"unreadable filter target holding a not-found reader error",
			&graph.FilterTargetUnreadableError{
				Field: "siblingId", ID: "gone", ReaderErr: nib.ErrNotFound,
			},
			"",
		},
		{
			// Like the unreadable class it implements no Unwrap, so no sentinel
			// reaches this function's tail and filterTargetErrCode owns it.
			"empty filter target",
			&graph.FilterTargetEmptyError{Field: "parentId"},
			"",
		},
		{
			"a secondary id reported without %w",
			fmt.Errorf("parent nib not found: %s", "zz"),
			"",
		},
		{
			// The shape Core.Update mints when the file it means to replace is
			// gone: the non-creating writer refuses, wrapping fs.ErrNotExist. The
			// repair is to re-read the store, not to fix an argument, so the
			// VALIDATION_ERROR fallback would name the wrong thing.
			"a stale path the store's files moved out from under",
			fmt.Errorf("%s: %w", "up91",
				fmt.Errorf("writing file: %w",
					fmt.Errorf("updating %s: %w", "/s/.nibs/data/up91--x.md", fs.ErrNotExist))),
			output.ErrFileError,
		},
		{
			// The two sentinels are unrelated types, and this row is what says
			// so: fs.ErrNotExist must not start claiming an id-miss, which is
			// exit 3 and a different repair.
			"an id-miss carrying a filesystem miss too",
			fmt.Errorf("%w: %w", nib.ErrNotFound, fs.ErrNotExist),
			output.ErrNotFound,
		},
		{"an unrelated failure", errors.New("resolver blew up"), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := mutationErrCode(tt.err)
			if tt.want == "" {
				if ok {
					t.Fatalf("mutationErrCode() = %q, want unclassified", got)
				}
				return
			}
			if !ok {
				t.Fatalf("mutationErrCode() reported unclassified, want %q", tt.want)
			}
			if got != tt.want {
				t.Errorf("mutationErrCode() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestGraphQLErrCodeClassifiesUnreadableFilterTargetAsFileError is the other
// half of the boundary above: an unreadable filter target is unclassified by
// mutationErrCode, so graphQLErrCode must still reach filterTargetErrCode and
// report FILE_ERROR. Reporting a target that resolved and then could not be read
// as a not-found would tell an agent it typed the wrong id when it did not.
func TestGraphQLErrCodeClassifiesUnreadableFilterTargetAsFileError(t *testing.T) {
	got, ok := graphQLErrCode(unreadableErr())
	if !ok {
		t.Fatal("graphQLErrCode() reported unclassified, want FILE_ERROR")
	}
	if got != output.ErrFileError {
		t.Errorf("graphQLErrCode() = %q, want %q", got, output.ErrFileError)
	}
}

// TestFilterRefusalExitCodes names the exit class every filter-refusal type
// carries. It is the table a caller reads to answer "what does $? mean when a
// filter argument is refused". A new class must be added here BY HAND; nothing
// enforces it.
//
// graph.FilterAreaError is the row that motivated writing it out: it is a
// filter refusal like the five below, it is dispatched by filterTargetErrCode
// beside them, and until this test nothing asserted its exit code at all. The
// guard that was meant to notice reads type NAMES matching FilterTarget*Error,
// and this one is named FilterArea — so it shipped past a green test.
func TestFilterRefusalExitCodes(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"FilterTargetNotFoundError", &graph.FilterTargetNotFoundError{Field: "parentId", ID: "zz"}, output.ErrNotFound},
		{"FilterTargetUnreadableError", &graph.FilterTargetUnreadableError{Field: "siblingId", ID: "gone", ReaderErr: nib.ErrNotFound}, output.ErrFileError},
		{"FilterTargetEmptyError", &graph.FilterTargetEmptyError{Field: "parentId"}, output.ErrValidation},
		{"FilterTargetContradictionError", &graph.FilterTargetContradictionError{Field: "parentId", PresenceField: "hasParent", ID: "zz"}, output.ErrValidation},
		{"FilterTargetTypeError", &graph.FilterTargetTypeError{Field: "milestone", ID: "zz", Got: "epic", Want: "milestone"}, output.ErrValidation},
		{"FilterAreaError", &graph.FilterAreaError{Field: "area", Path: "nope", Declared: "core, web"}, output.ErrValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := filterTargetErrCode(tt.err)
			if !ok {
				t.Fatalf("filterTargetErrCode leaves %s unclassified, so its call sites answer one user error with three different exit codes", tt.name)
			}
			if got != tt.want {
				t.Errorf("filterTargetErrCode(%s) = %q (exit %d), want %q (exit %d)",
					tt.name, got, output.ExitCode(got), tt.want, output.ExitCode(tt.want))
			}
		})
	}
}

// TestFilterTargetErrCodeClassifiesEveryRefusalClass is the totality guard for
// the refusal taxonomy — for error CLASSES what idValuedFilterFields is for
// filter FIELDS.
//
// filterTargetErrCode dispatches on concrete type for the classes that carry no
// sentinel and on the nib.ErrNotFound sentinel for those that do, and reports
// ok=false for everything else. Its call sites then default differently:
// cmd/rel.go falls back to FILE_ERROR (exit 5, "the tracker broke"), cmd/list.go
// to VALIDATION_ERROR (exit 2) in one place and to a bare fmt.Errorf (exit 1) in
// another. A NEW refusal class following this taxonomy's own convention —
// implement NO Unwrap, which FilterTargetUnreadableError's doc calls the whole
// safety property — would match no branch, so each of those call sites would
// answer one user error with its own default class, with nothing failing to
// compile and nothing failing to pass.
//
// The class names are read out of internal/graph's SOURCE rather than listed
// here, because a list is precisely what cannot notice a class it was never
// told about. A new FilterTarget*Error declared in that package fails this test
// until it has a representative here and filterTargetErrCode returns the code
// that representative names. The walk reads that package's own files and does
// not recurse, so a refusal class declared in a subpackage or in another
// package is outside this guard's scope.
func TestFilterTargetErrCodeClassifiesEveryRefusalClass(t *testing.T) {
	// One representative per class, keyed by the type name the source walk
	// reports, carrying the code the taxonomy owes it.
	representatives := map[string]struct {
		err  error
		want string
	}{
		"FilterTargetNotFoundError": {
			&graph.FilterTargetNotFoundError{Field: "parentId", ID: "zz"},
			output.ErrNotFound,
		},
		"FilterTargetUnreadableError": {
			&graph.FilterTargetUnreadableError{Field: "siblingId", ID: "gone", ReaderErr: nib.ErrNotFound},
			output.ErrFileError,
		},
		"FilterTargetEmptyError": {
			&graph.FilterTargetEmptyError{Field: "parentId"},
			output.ErrValidation,
		},
		"FilterTargetContradictionError": {
			&graph.FilterTargetContradictionError{Field: "parentId", PresenceField: "hasParent", ID: "zz"},
			output.ErrValidation,
		},
		"FilterTargetTypeError": {
			&graph.FilterTargetTypeError{Field: "milestone", ID: "zz", Got: "epic", Want: "milestone"},
			output.ErrValidation,
		},
	}

	declared := filterRefusalTypeNames(t)
	for _, name := range declared {
		t.Run(name, func(t *testing.T) {
			rep, ok := representatives[name]
			if !ok {
				t.Fatalf("%s is a filter-refusal class with no representative here, so nothing checks that filterTargetErrCode classifies it — add one, and a branch if it needs one", name)
			}
			got, ok := filterTargetErrCode(rep.err)
			if !ok {
				t.Fatalf("filterTargetErrCode leaves %s unclassified, so its call sites answer one user error with three different exit codes", name)
			}
			if got != rep.want {
				t.Errorf("filterTargetErrCode(%s) = %q (exit %d), want %q (exit %d)",
					name, got, output.ExitCode(got), rep.want, output.ExitCode(rep.want))
			}
		})
	}

	// The other direction: a representative the walk never reported means the
	// walk stopped seeing the source, and every subtest above would then be
	// vacuously absent rather than failing.
	for name := range representatives {
		if !slices.Contains(declared, name) {
			t.Errorf("%s has a representative here but the source walk did not find it, so the walk is not reading what it is meant to", name)
		}
	}
}

// filterRefusalTypeNames reads internal/graph's non-test sources and returns
// every type declared there whose name matches FilterTarget*Error.
//
// Reading the source is what makes the guard above total. Go offers no runtime
// enumeration of a package's types, so the alternative is a hand-kept list —
// and a hand-kept list cannot report the class nobody remembered to add to it,
// which is the entire failure this guards against.
func filterRefusalTypeNames(t *testing.T) []string {
	t.Helper()

	dir := filepath.Join("..", "internal", "graph")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	fset := token.NewFileSet()
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if strings.HasPrefix(ts.Name.Name, "FilterTarget") && strings.HasSuffix(ts.Name.Name, "Error") {
					names = append(names, ts.Name.Name)
				}
			}
		}
	}

	// Without this a wrong directory, a renamed package or a changed naming
	// convention would empty the walk and leave the guard reporting success
	// over nothing.
	if len(names) == 0 {
		t.Fatalf("no FilterTarget*Error type found under %s, so this guard checks nothing", dir)
	}
	slices.Sort(names)
	return names
}
