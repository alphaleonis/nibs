package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
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

// notFoundErr, unreadableErr, conflictErr and unparseableErr construct four of
// the failures graphQLErrCode classifies — an *nibcore.ETagRequiredError and a
// bare nib.ErrNotFound from a mutation are the others. Each is wrapped the way
// gqlgen wraps a resolver error (gqlerror.Wrap sets Err, and *gqlerror.Error
// implements Unwrap) so the classifier is exercised through the same chain the
// executor produces. TestGraphQLErrCodeCodesHaveDistinctExitStatuses has the
// full corpus.
func notFoundErr() *gqlerror.Error {
	return gqlerror.Wrap(&graph.FilterTargetNotFoundError{Field: "parentId", ID: "zz"})
}

func unreadableErr() *gqlerror.Error {
	return gqlerror.Wrap(&graph.FilterTargetUnreadableError{
		Field: "siblingId", ID: "gone", ReaderErr: nib.ErrNotFound,
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

// TestGraphQLResponseCode pins the rule that decides a whole response's exit
// class: all errors agreeing on a code yields that code, any disagreement
// yields UNCATEGORIZED (exit 1). The mixed cases are the point — a response is
// one exit status, and claiming NOT_FOUND for a response that also reports an IO
// failure would tell an agent it typed a bad id when the store is what broke.
// Claiming VALIDATION_ERROR instead would be just as wrong in the other
// direction: exit 2 asserts the caller's input was at fault, which a mixed
// not-found/conflict pair does not support.
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

// TestGraphQLErrCodeCodesHaveDistinctExitStatuses pins graphQLResponseCode's
// stated precondition: no two codes graphQLErrCode can return may share an exit
// status, and none may share one with the VALIDATION_ERROR an unclassified
// error defaults to.
//
// graphQLResponseCode compares CODE STRINGS but decides an EXIT STATUS, and
// output.ExitCode is many-to-one — ErrHierarchy, ErrInvalidStatus,
// ErrTextNotFound and ErrTextAmbiguous all collapse onto ErrValidation's exit 2.
// The moment a classifier branch returns one of those, a batch whose failures
// the direct commands all exit 2 for starts exiting 1 as UNCATEGORIZED, and
// nothing else in the suite notices: every existing assertion is written in
// codes, so a code-level disagreement that is an exit-level agreement reads as
// correct everywhere.
//
// classified names one representative of every failure graphQLErrCode is
// expected to recognize; unclassified names the ones that must keep the
// caller's VALIDATION_ERROR. The claim is bounded by that corpus: a new branch
// is caught from whichever side it lands on as long as some row's type reaches
// it — a new code sharing an exit status fails the distinctness check, and a
// corpus member changing class fails its own row. A branch matching a type
// named by neither list trips nothing, because no row calls the classifier with
// one and the exits map never sees the duplicate. Adding a branch means adding
// a row.
func TestGraphQLErrCodeCodesHaveDistinctExitStatuses(t *testing.T) {
	classified := []struct {
		name string
		err  error
		want string
	}{
		{"filter target not found", notFoundErr(), output.ErrNotFound},
		{"filter target unreadable", unreadableErr(), output.ErrFileError},
		{"mutation etag mismatch", conflictErr(), output.ErrConflict},
		{
			"mutation etag required",
			gqlerror.Wrap(&nibcore.ETagRequiredError{}),
			output.ErrConflict,
		},
		{"on-disk nib unparseable", unparseableErr(), output.ErrFileError},
		{
			// A mutation aimed at an unknown subject id arrives as a bare
			// sentinel, not as a filter-target type — mutationErrCode's
			// errors.Is tail is what classifies it.
			"mutation target not found",
			gqlerror.Wrap(nib.ErrNotFound),
			output.ErrNotFound,
		},
	}
	unclassified := []struct {
		name string
		err  error
	}{
		{"a resolver failure with no nib-level class", gqlerror.Errorf("resolver blew up")},
		{
			// The already-planned classifier extension: today an illegal parent
			// type reaches `nibs query` unclassified and rides the
			// VALIDATION_ERROR default, which is exit 2 — the same exit
			// nibs mv reports for it. Classifying it as HIERARCHY would keep
			// that exit for a lone failure and break it for a batch.
			"an illegal parent type",
			gqlerror.Wrap(&nibtypes.HierarchyError{ChildType: "milestone", ParentType: "task"}),
		},
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

	// The unclassified default is part of the comparison graphQLResponseCode
	// runs, so it has to be part of the distinctness claim too.
	exits := map[int]string{output.ExitCode(output.ErrValidation): output.ErrValidation}

	for _, tt := range classified {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := graphQLErrCode(tt.err)
			if !ok {
				t.Fatalf("graphQLErrCode() reported unclassified, want %q", tt.want)
			}
			if got != tt.want {
				t.Fatalf("graphQLErrCode() = %q, want %q", got, tt.want)
			}
		})
		code, ok := graphQLErrCode(tt.err)
		if !ok {
			continue
		}
		exit := output.ExitCode(code)
		if prior, dup := exits[exit]; dup && prior != code {
			t.Errorf("graphQLErrCode can return both %q and %q, which share exit %d — "+
				"graphQLResponseCode compares codes, so a response carrying both reports "+
				"UNCATEGORIZED (exit 1) for failures the direct commands agree exit %d; "+
				"see its PRECONDITION",
				prior, code, exit, exit)
		}
		exits[exit] = code
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
	}
}

// TestMutationErrCodeBoundaries pins mutationErrCode at every boundary its
// branches touch, including the values that sit BETWEEN two of them. It is the
// direct-command end of the classification: `nibs mv` reaches it through
// mvMutationError → setMutationError → mutationError, and `nibs query` reaches
// the same function through graphQLErrCode, so one table covers both surfaces.
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
		{
			"unparseable carrying an OS error",
			&nibcore.OnDiskUnparseableError{
				ID: "a", Path: "a.md", Reason: "unreadable", Err: os.ErrPermission,
			},
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
			"unreadable filter target holding a not-found reader error",
			&graph.FilterTargetUnreadableError{
				Field: "siblingId", ID: "gone", ReaderErr: nib.ErrNotFound,
			},
			"",
		},
		{
			"a secondary id reported without %w",
			fmt.Errorf("parent nib not found: %s", "zz"),
			"",
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
