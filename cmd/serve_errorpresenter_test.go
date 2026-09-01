package cmd

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
)

// TestETagErrorPresenter_TagsOnlyTypedEtagError pins the structured error-code
// contract the web client relies on: the gqlgen error presenter attaches a
// stable extensions.code = "ETAG_MISMATCH" to ONLY the typed
// *nibcore.ETagMismatchError, and leaves every other error (validation, generic,
// wrapped-but-unrelated) untouched. The web classifier (isEtagConflict) keys
// conflict routing on that code first, with the human-readable "etag mismatch"
// string retained purely as a fallback (see web/src/lib/nibForm.svelte.ts).
func TestETagErrorPresenter_TagsOnlyTypedEtagError(t *testing.T) {
	ctx := context.Background()

	t.Run("tags the typed ETagMismatchError", func(t *testing.T) {
		err := &nibcore.ETagMismatchError{Provided: "abc123", Current: "def456"}

		gqlErr := etagErrorPresenter(ctx, err)

		if gqlErr.Extensions["code"] != "ETAG_MISMATCH" {
			t.Errorf("extensions.code = %v, want %q", gqlErr.Extensions["code"], "ETAG_MISMATCH")
		}
		// The human-readable message must be preserved verbatim (the substring
		// fallback still keys off it).
		if gqlErr.Message != err.Error() {
			t.Errorf("message = %q, want %q", gqlErr.Message, err.Error())
		}
	})

	t.Run("tags a wrapped ETagMismatchError (errors.As chain)", func(t *testing.T) {
		wrapped := fmt.Errorf("update failed: %w", &nibcore.ETagMismatchError{Provided: "a", Current: "b"})

		gqlErr := etagErrorPresenter(ctx, wrapped)

		if gqlErr.Extensions["code"] != "ETAG_MISMATCH" {
			t.Errorf("extensions.code = %v, want %q (wrapped typed error must still match errors.As)",
				gqlErr.Extensions["code"], "ETAG_MISMATCH")
		}
	})

	t.Run("does NOT tag the enum-validation error", func(t *testing.T) {
		// Mirrors the plain fmt.Errorf validation errors from nibcore.Core.ValidateEnums.
		err := fmt.Errorf("invalid status %q: must be one of draft, todo", "bogus")

		gqlErr := etagErrorPresenter(ctx, err)

		if _, ok := gqlErr.Extensions["code"]; ok {
			t.Errorf("validation error was tagged with code %v; only the typed etag error may be tagged",
				gqlErr.Extensions["code"])
		}
	})

	t.Run("does NOT tag a generic error", func(t *testing.T) {
		gqlErr := etagErrorPresenter(ctx, errors.New("disk full"))

		if _, ok := gqlErr.Extensions["code"]; ok {
			t.Errorf("generic error was tagged with code %v; only the typed etag error may be tagged",
				gqlErr.Extensions["code"])
		}
	})

	t.Run("does NOT tag the ETagRequiredError (distinct typed error)", func(t *testing.T) {
		gqlErr := etagErrorPresenter(ctx, &nibcore.ETagRequiredError{})

		if _, ok := gqlErr.Extensions["code"]; ok {
			t.Errorf("ETagRequiredError was tagged with code %v; only ETagMismatchError is a reconcilable conflict",
				gqlErr.Extensions["code"])
		}
	})
}

// TestETagErrorPresenter_TagsNotFound pins the second half of the structured
// error-code contract: a mutation error carrying nib.ErrNotFound is tagged with a
// stable extensions.code = "NOT_FOUND". The web client keys on this to route a
// failed save against a DELETED nib into its gone/deleted notice (see
// web/src/lib/nibForm.svelte.ts, isNotFound) instead of showing the raw
// "target nib not found" toast. A delete is NOT an etag conflict —
// GetForUpdate returns ErrNotFound before any if-match check — so this code is
// distinct from ETAG_MISMATCH. The tag keys on the error type, not on WHICH nib
// is gone: both the bare ErrNotFound (the edited nib deleted) and the wrapped
// "target nib not found" (a deleted blocking/parent TARGET) are tagged. On the
// web save path only the former is reachable, because the edit-save input sends no
// parent/blocking fields (see etagErrorPresenter's docstring in serve.go).
func TestETagErrorPresenter_TagsNotFound(t *testing.T) {
	ctx := context.Background()

	t.Run("tags the bare nib.ErrNotFound", func(t *testing.T) {
		gqlErr := etagErrorPresenter(ctx, nib.ErrNotFound)

		if gqlErr.Extensions["code"] != "NOT_FOUND" {
			t.Errorf("extensions.code = %v, want %q", gqlErr.Extensions["code"], "NOT_FOUND")
		}
	})

	t.Run("tags a wrapped ErrNotFound (errors.Is chain)", func(t *testing.T) {
		// Mirrors the resolver wrap: fmt.Errorf("target nib not found: %s: %w", id, err).
		wrapped := fmt.Errorf("target nib not found: %s: %w", "n1", nib.ErrNotFound)

		gqlErr := etagErrorPresenter(ctx, wrapped)

		if gqlErr.Extensions["code"] != "NOT_FOUND" {
			t.Errorf("extensions.code = %v, want %q (wrapped ErrNotFound must still match errors.Is)",
				gqlErr.Extensions["code"], "NOT_FOUND")
		}
		// The human-readable message is preserved verbatim.
		if gqlErr.Message != wrapped.Error() {
			t.Errorf("message = %q, want %q", gqlErr.Message, wrapped.Error())
		}
	})

	t.Run("does NOT tag a generic 'not found'-worded error", func(t *testing.T) {
		// Only the typed ErrNotFound (via errors.Is) may be tagged — a coincidental
		// wording must never be mistaken for a real deletion, which routes the whole
		// view to gone/deleted.
		gqlErr := etagErrorPresenter(ctx, errors.New("parent nib not found: p1"))

		if _, ok := gqlErr.Extensions["code"]; ok {
			t.Errorf("generic 'not found' error was tagged with code %v; only errors.Is(err, nib.ErrNotFound) may be tagged",
				gqlErr.Extensions["code"])
		}
	})

	// The three filter-target classes reach the presenter over the READ path, not
	// the mutation path the cases above cover: the web filter box sends
	// user-typed ids straight into NibFilter, so a half-typed id refuses the
	// whole query. NOT_FOUND is what lets the list treat that as an explainable
	// empty state instead of flashing a failure while the user is still typing
	// (see web/src/lib/components/TreeTable.svelte).
	t.Run("tags an unknown filter target", func(t *testing.T) {
		err := &graph.FilterTargetNotFoundError{Field: "parentId", ID: "zz"}

		gqlErr := etagErrorPresenter(ctx, err)

		if gqlErr.Extensions["code"] != "NOT_FOUND" {
			t.Errorf("extensions.code = %v, want %q", gqlErr.Extensions["code"], "NOT_FOUND")
		}
		if gqlErr.Message != err.Error() {
			t.Errorf("message = %q, want %q", gqlErr.Message, err.Error())
		}
	})

	t.Run("does NOT tag a filter target that vanished mid-filter", func(t *testing.T) {
		// A concurrent delete is not the caller's typo. Tagging it NOT_FOUND
		// would tell the web to explain a wrong id that was never wrong, so it
		// stays uncoded and lands in the generic error path with every other
		// internal failure.
		err := &graph.FilterTargetUnreadableError{Field: "siblingId", ID: "nibs-a", ReaderErr: nib.ErrNotFound}

		gqlErr := etagErrorPresenter(ctx, err)

		if code, ok := gqlErr.Extensions["code"]; ok {
			t.Errorf("a mid-filter vanish was tagged with code %v; it must not classify as the caller's not-found", code)
		}
	})

	t.Run("does NOT tag a filter field given an empty id", func(t *testing.T) {
		// The other read-path filter refusal, and the one that must NOT reach
		// the web as NOT_FOUND. TreeTable.svelte routes any read-path NOT_FOUND
		// to a calm inline empty state, which is right for a half-typed id and
		// wrong for a malformed query: an empty id is what a client sends when
		// a variable did not interpolate, so explaining it away as "nothing
		// matched" would hide the client bug behind the same confident empty
		// answer the refusal exists to replace. Untagged, it lands in the
		// generic error path and shows as the failure it is.
		err := &graph.FilterTargetEmptyError{Field: "parentId"}

		gqlErr := etagErrorPresenter(ctx, err)

		if code, ok := gqlErr.Extensions["code"]; ok {
			t.Errorf("an empty filter id was tagged with code %v; it must not be presented as a not-found or any other routable class", code)
		}
		if gqlErr.Message != err.Error() {
			t.Errorf("message = %q, want %q", gqlErr.Message, err.Error())
		}
	})

	t.Run("tags a contradictory filter pair with its own code, not NOT_FOUND", func(t *testing.T) {
		// The third read-path refusal, and the one that needs a code of its own.
		// NOT_FOUND is wrong: it routes to the "no such nib" empty state, whose
		// wording blames an id when both ids may be perfectly good. Uncoded is
		// wrong too: the web list then renders the backend's raw sentence in a
		// destructive box for a query the user wrote and can edit — and for the
		// parent pair that replaces an inline empty state whose button cleared it
		// in one click. A distinct code is what lets TreeTable.svelte explain the
		// pair in the query box's own vocabulary and keep that button.
		err := &graph.FilterTargetContradictionError{Field: "parentId", PresenceField: "hasParent", ID: "nibs-a"}

		gqlErr := etagErrorPresenter(ctx, err)

		if gqlErr.Extensions["code"] != "FILTER_CONTRADICTION" {
			t.Errorf("extensions.code = %v, want %q", gqlErr.Extensions["code"], "FILTER_CONTRADICTION")
		}
		if gqlErr.Message != err.Error() {
			t.Errorf("message = %q, want %q", gqlErr.Message, err.Error())
		}
	})

	t.Run("does NOT tag the typed ETagMismatchError as NOT_FOUND (stays ETAG_MISMATCH)", func(t *testing.T) {
		// Symmetric to TagsOnlyTypedEtagError's negative case: the two typed errors
		// are mutually exclusive. A reconcilable etag conflict must route into the
		// inline resolver, never the (stronger, less-recoverable) gone/deleted path.
		err := &nibcore.ETagMismatchError{Provided: "abc123", Current: "def456"}

		gqlErr := etagErrorPresenter(ctx, err)

		if gqlErr.Extensions["code"] != "ETAG_MISMATCH" {
			t.Errorf("extensions.code = %v, want %q; an etag conflict must not be tagged NOT_FOUND",
				gqlErr.Extensions["code"], "ETAG_MISMATCH")
		}
	})
}

// TestETagErrorPresenter_TagsBulkReorderPreValidationConflict runs the REAL
// bulk-reorder pre-validation refusal through the presenter, rather than a
// hand-built ETagMismatchError. Both of a bulk reorder's etag refusals — the
// pre-validation one raised before any write, and the racing one raised by the
// per-nib write — are reconcilable conflicts, so both must reach the web client
// as extensions.code = "ETAG_MISMATCH"; the pre-validation one is also the
// COMMON case, firing whenever the caller's ifMatch is already stale on entry.
// Constructing it through the resolver is what makes this bite: the guard is
// that the graph layer emits a TYPED error, and only an end-to-end error can
// witness that.
func TestETagErrorPresenter_TagsBulkReorderPreValidationConflict(t *testing.T) {
	ctx := context.Background()

	// Two root-level siblings; the reorder lists both, so it is complete.
	resolver, _ := setupParentLinkTest(t, map[string]string{
		"nibs-a": "order: a0\n",
		"nibs-b": "order: b0\n",
	})

	const stale = "deadbeefdeadbeef"
	ifMatch := []*model.ChildEtag{{ID: "nibs-a", Etag: stale}}
	_, err := resolver.Mutation().ReorderChildren(ctx, "", []string{"nibs-b", "nibs-a"}, ifMatch)
	if err == nil {
		t.Fatal("expected a pre-validation refusal for the stale etag")
	}

	gqlErr := etagErrorPresenter(ctx, err)

	if gqlErr.Extensions["code"] != "ETAG_MISMATCH" {
		t.Errorf("extensions.code = %v, want %q (a bulk-reorder pre-validation conflict is reconcilable and must be routable structurally); error was %T: %v",
			gqlErr.Extensions["code"], "ETAG_MISMATCH", err, err)
	}
	// The message keeps naming the offending nib, and keeps the "etag mismatch"
	// wording the web classifier retains as its substring fallback.
	if !strings.Contains(gqlErr.Message, "nibs-a") {
		t.Errorf("message = %q, want it to name the nib whose etag was stale", gqlErr.Message)
	}
	if !strings.Contains(gqlErr.Message, "etag mismatch") {
		t.Errorf("message = %q, want it to keep the %q wording the web fallback keys on", gqlErr.Message, "etag mismatch")
	}
}

// TestWireErrorCodeConstantsAreNamedInTheSchema requires the SDL to spell every
// wire code the error presenter can mint. The SDL is the shipped contract — its
// descriptions propagate verbatim into internal/graph/model/models_gen.go, into
// web/src/lib/gql/graphql.ts (what a web developer reads on hover) and into
// `nibs catalog schema` (what an agent is told) — so a code minted here but
// never named there is one no client can discover by reading the contract, and
// no behavioral test can see it: a missing description produces no wrong answer
// at runtime, only a client that cannot be written.
//
// The claim runs presenter -> SDL and only that direction. The reverse (every
// code the SDL names must be mintable) is deliberately not asserted: the SDL's
// SCREAMING_SNAKE token space is mostly enum members (UPDATED_AT, DESC) and
// prose emphasis (EMPTY, BOTH), with no notation separating a wire code from a
// capitalized word.
//
// The rows are a hand-written list, so any code reaching the wire that this list
// is not updated for is what this does not see — a review question.
func TestWireErrorCodeConstantsAreNamedInTheSchema(t *testing.T) {
	schemaPath := filepath.Join("..", "internal", "graph", "schema.graphqls")
	sdl, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read %s: %v", schemaPath, err)
	}

	for _, code := range []string{wireCodeETagMismatch, wireCodeFilterContradiction, wireCodeNotFound} {
		// A whole-word match, not a parse: the claim is only that the contract
		// SPELLS the code, which is what a client greps for and what survives any
		// rewording of the sentence around it. RE2 counts _ as a word character,
		// so \b refuses a longer token that merely contains the code
		// (NOT_FOUND_ANYWHERE) while matching both spellings in live use — prose
		// ("refused with a NOT_FOUND error") and quoted (code = "NOT_FOUND").
		if !regexp.MustCompile(`\b` + regexp.QuoteMeta(code) + `\b`).Match(sdl) {
			t.Errorf("the error presenter can mint extensions.code = %q, but %s never spells it, "+
				"so a client reading the shipped contract has nothing to branch on for this refusal — "+
				"name the code in the description of every field whose refusal carries it",
				code, schemaPath)
		}
	}
}

// TestEveryMintableWireErrorCodeIsNamedInTheSchema pins the half of the code
// contract the tests above cannot reach: that every extensions.code this
// presenter can mint is also SPELLED in internal/graph/schema.graphqls. The SDL
// is the shipped contract — its descriptions propagate verbatim into
// internal/graph/model/models_gen.go, into web/src/lib/gql/graphql.ts (what a web
// developer reads on hover) and into `nibs catalog schema` (what an agent is
// told) — so a code the presenter mints but the SDL never names is a code no
// client can discover by reading the contract. ETAG_MISMATCH was exactly that:
// minted here twice, absent from the SDL entirely, while
// web/src/lib/nibForm.svelte.ts already routed conflict recovery on it and
// documented it as the primary conflict signal.
//
// The claim runs presenter -> SDL and ONLY that direction. The reverse (every
// code the SDL names must be mintable) is deliberately not asserted: the SDL's
// SCREAMING_SNAKE token space is mostly enum members (UPDATED_AT, DESC) and prose
// emphasis (EMPTY, BOTH), with no notation separating a wire code from a CLI code
// from a capitalized word, so that direction needs a notation convention first —
// without one it needs an allowlist, and an allowlist rots.
//
// The rows are read out of the presenter's own call sites rather than listed
// here, for the reason filterRefusalTypeNames gives in errors_test.go: a list
// cannot report the code nobody remembered to add to it. That is what makes this
// fail CLOSED — a new setErrorCode call with an undocumented code is caught by
// construction, not by anyone remembering this test exists.
func TestEveryMintableWireErrorCodeIsNamedInTheSchema(t *testing.T) {
	schemaPath := filepath.Join("..", "internal", "graph", "schema.graphqls")
	sdl, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read %s: %v", schemaPath, err)
	}

	for _, code := range mintableWireErrorCodes(t) {
		t.Run(code, func(t *testing.T) {
			// A whole-word match, not a parse: the claim is only that the contract
			// SPELLS the code, which is what a client greps for and what survives
			// any rewording of the sentence around it. RE2 counts _ as a word
			// character, so \b refuses a longer token that merely contains the
			// code (NOT_FOUND_ANYWHERE, X_NOT_FOUND) while still matching both
			// spellings in live use — prose ("refused with a NOT_FOUND error")
			// and quoted (extensions.code = "NOT_FOUND").
			if !regexp.MustCompile(`\b` + regexp.QuoteMeta(code) + `\b`).Match(sdl) {
				t.Errorf("the error presenter can mint extensions.code = %q, but %s never spells it, "+
					"so a client reading the shipped contract has nothing to branch on for this refusal — "+
					"name the code in the description of every field whose refusal carries it",
					code, schemaPath)
			}
		})
	}
}

// mintableWireErrorCodes reads package cmd's non-test sources and returns every
// extensions.code passed to setErrorCode, sorted and deduplicated.
//
// It resolves the two forms the ARGUMENT can take: a string literal, and an
// identifier declared as a string constant in this package. An argument it
// cannot evaluate statically — a selector such as output.ErrNotFound, a
// variable, a concatenation — FAILS the walk rather than being dropped from it,
// because a code this cannot read is a code the guard above would silently stop
// checking.
//
// The CALLEE is a separate matter, and a weaker one. The code walk reads
// call.Fun.(*ast.Ident) only, so a call reached any other way is not seen as a
// call site at all — it is dropped rather than failing. Two escapes from that
// are cheap to recognize and are flagged below instead:
//   - A qualified call, graph.setErrorCode(...), which is what a move of the
//     helper to another package looks like from here.
//   - Any mention of setErrorCode that is not a bare call and not its own
//     declaration — assigned to a variable (set := setErrorCode; set(...)),
//     passed as an argument, deferred. Inside package cmd the function value
//     cannot be obtained without naming it, so flagging the name in a non-callee
//     position covers the whole family.
//
// What remains genuinely uncovered, so the guard's reach is not overstated:
//   - Calls outside package cmd. The walk is os.ReadDir("."), so a presenter
//     that moves elsewhere while any call stays behind keeps this green over a
//     shrunken set; only a TOTAL move trips the emptiness fatal below.
//   - A direct gqlErr.Extensions["code"] = … assignment, which bypasses the
//     helper entirely and so is invisible to a walk keyed on its name.
func mintableWireErrorCodes(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}

	fset := token.NewFileSet()
	var files []*ast.File
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files = append(files, file)
	}

	// Package-level string constants first, so an identifier argument below can
	// be resolved to the code it stands for.
	consts := map[string]string{}
	for _, file := range files {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					if lit, ok := stringLiteral(vs.Values[i]); ok {
						consts[name.Name] = lit
					}
				}
			}
		}
	}

	// Every escape from the bare-call shape the code walk below reads. Inside
	// package cmd the function value cannot be obtained without naming it, so a
	// mention of setErrorCode that is neither its declaration nor the callee of a
	// bare call is a call site this guard is about to stop seeing.
	for _, file := range files {
		// Positions the walks already account for: a callee (bare or qualified —
		// the qualified one is reported by the selector arm below) and the
		// helper's own declaration.
		accounted := map[token.Pos]bool{}
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				switch fun := n.Fun.(type) {
				case *ast.Ident:
					accounted[fun.Pos()] = true
				case *ast.SelectorExpr:
					accounted[fun.Sel.Pos()] = true
				}
			case *ast.FuncDecl:
				accounted[n.Name.Pos()] = true
			}
			return true
		})
		ast.Inspect(file, func(n ast.Node) bool {
			ident, ok := n.(*ast.Ident)
			if !ok || ident.Name != "setErrorCode" || accounted[ident.Pos()] {
				return true
			}
			t.Errorf("%s: setErrorCode is referenced as a VALUE rather than called directly, "+
				"so any code it mints through that value goes unchecked against the schema — "+
				"call it as a bare identifier in package cmd, or widen this walk",
				fset.Position(ident.Pos()))
			return true
		})
	}

	var codes []string
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			fn, ok := call.Fun.(*ast.Ident)
			if !ok || fn.Name != "setErrorCode" || len(call.Args) != 2 {
				// A qualified call — graph.setErrorCode(...) after a move, or a
				// method value — reads as a call site to a human but not to the
				// *ast.Ident match above, so say so instead of dropping it.
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "setErrorCode" {
					t.Errorf("%s: setErrorCode is reached through a selector, but this guard only reads "+
						"bare calls, so the code it mints goes unchecked against the schema — call it "+
						"as a bare identifier in package cmd, or widen this walk",
						fset.Position(call.Pos()))
				}
				return true
			}
			arg := call.Args[1]
			if lit, ok := stringLiteral(arg); ok {
				codes = append(codes, lit)
				return true
			}
			if ident, ok := arg.(*ast.Ident); ok {
				if lit, ok := consts[ident.Name]; ok {
					codes = append(codes, lit)
					return true
				}
			}
			t.Errorf("%s: setErrorCode is passed a code this guard cannot evaluate statically, "+
				"so the code it mints goes unchecked against the schema — pass a string literal or a "+
				"string constant declared in package cmd", fset.Position(arg.Pos()))
			return true
		})
	}

	// Without this a renamed helper, a changed signature or a wrong directory
	// would empty the walk and leave the guard above reporting success over
	// nothing.
	if len(codes) == 0 {
		t.Fatalf("no setErrorCode call found in package cmd, so this guard checks nothing")
	}
	slices.Sort(codes)
	return slices.Compact(codes)
}

// stringLiteral reports the value of an untyped string literal expression.
func stringLiteral(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return value, true
}
