package cmd

import (
	"context"
	"net/http"

	"github.com/99designs/gqlgen/graphql"
	"github.com/vektah/gqlparser/v2/ast"
)

// maxRecursiveSelectionDepth bounds how many fields of a self-recursive type one
// document may nest inside one another. `Nib` is recursive on six fields
// (parent, children, blocking, blockedBy, mentions, mentionedBy), so alternating
// them — children { parent { children { … } } } — asks for a tree that multiplies
// by the fan-out at every cycle while the document itself stays a few dozen
// bytes. Bounding the document's SIZE does not bound that; bounding how far it
// may re-enter the recursion does.
//
// The basis for 3: the deepest document the shipped web client sends is
// NibDetail, at 2 — `nib` and then one relationship field, never nested — so 3
// leaves one level of headroom for a view that gains a hop. The tests measure
// the client's own source against this limit and fail if any document reaches
// it, so the headroom is checked rather than assumed.
const maxRecursiveSelectionDepth = 3

// maxRecursiveSelectionTotal bounds how many recursive-type fields one document
// may select ALTOGETHER. The depth bound above constrains one path through the
// document and says nothing about how many paths it carries; aliases do not
// merge, so `{ nibs { a0: children { parent { id } } a1: … } }` repeats a branch
// sitting exactly at the depth bound as many times as the request size allows,
// and every repeat is resolved independently.
//
// The basis for 100: the heaviest document the shipped web client sends selects
// 7 recursive fields (NibDetail — `nib` plus its six relationship fields), and
// the tests refuse any shipped document above half this limit, so the room left
// for the client to grow exceeds everything it asks for today.
const maxRecursiveSelectionTotal = 100

// recursiveTypeNames returns the names of the schema's composite types that can
// reach themselves — through their own fields, or through an abstract type a
// selection on them can narrow back to them. Those are the types whose fields
// can be nested without limit, so those are the fields the bounds count.
// Deriving the set from the schema rather than naming Nib keeps a later
// recursive type covered without a second edit.
//
// Introspection meta-types are excluded (__Type reaches itself through ofType):
// they are answered from the schema held in memory, not from the store, and
// newGraphQLHandler installs no extension.Introspection, so gqlgen leaves them
// disabled anyway.
func recursiveTypeNames(schema *ast.Schema) map[string]bool {
	recursive := make(map[string]bool)
	for name, def := range schema.Types {
		switch def.Kind {
		case ast.Object, ast.Interface, ast.Union:
		default:
			continue
		}
		if len(name) >= 2 && name[:2] == "__" {
			continue
		}
		if typeReachesItself(schema, name) {
			recursive[name] = true
		}
	}
	return recursive
}

// typeReachesItself reports whether start appears among the types reachable from
// start. The walk begins at what start LEADS TO, not at start, so a type is
// counted only for an actual path back to itself.
func typeReachesItself(schema *ast.Schema, start string) bool {
	seen := make(map[string]bool)
	queue := reachableTypeNames(schema, schema.Types[start])
	for len(queue) > 0 {
		name := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if name == start {
			return true
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		queue = append(queue, reachableTypeNames(schema, schema.Types[name])...)
	}
	return false
}

// reachableTypeNames returns the type names one step out from def: the named
// type of each of its fields (ast.Type.Name unwraps list and non-null wrappers),
// plus, for an abstract type, the types a selection on it can narrow to — a
// union's members, an interface's implementors.
//
// That second half is what lets the walk see a cycle a document can write but no
// single type declares: an object whose only path back to itself runs through an
// interface it implements (A.f: I, with A implementing I) has no field of its
// own type, and a union has no fields at all. The narrowing is only ever added
// for an abstract type — a concrete type is among its own possible types, so
// adding it there would report every object in the schema as recursive.
func reachableTypeNames(schema *ast.Schema, def *ast.Definition) []string {
	if def == nil {
		return nil
	}
	names := make([]string, 0, len(def.Fields))
	for _, f := range def.Fields {
		names = append(names, f.Type.Name())
	}
	if def.Kind == ast.Union || def.Kind == ast.Interface {
		for _, possible := range schema.PossibleTypes[def.Name] {
			names = append(names, possible.Name)
		}
	}
	return names
}

// recursionCost is what a selection set asks of the recursive part of the
// schema: depth is the greatest number of recursive-type fields on any one path
// through it, total is how many it selects across all paths. The two bound
// different things and neither implies the other — depth constrains the shape of
// one branch, total constrains how many branches the document may carry.
type recursionCost struct {
	depth int
	total int
}

// recursionWalk measures selection sets against one schema's recursive types,
// remembering each fragment of one document by name.
type recursionWalk struct {
	recursive map[string]bool
	fragments map[string]recursionCost
}

// measureRecursion reports the recursion cost of one document's selection set,
// expanding fragments so nesting hidden behind a spread counts the same as
// nesting written inline.
func measureRecursion(sel ast.SelectionSet, recursive map[string]bool) recursionCost {
	walk := recursionWalk{recursive: recursive, fragments: make(map[string]recursionCost)}
	return walk.measure(sel)
}

func (w recursionWalk) measure(sel ast.SelectionSet) recursionCost {
	var cost recursionCost
	for _, s := range sel {
		var sub recursionCost
		switch selection := s.(type) {
		case *ast.Field:
			sub = w.measure(selection.SelectionSet)
			if selection.Definition != nil && w.recursive[selection.Definition.Type.Name()] {
				sub.depth++
				sub.total = saturatingAdd(sub.total, 1)
			}
		case *ast.InlineFragment:
			sub = w.measure(selection.SelectionSet)
		case *ast.FragmentSpread:
			if selection.Definition != nil {
				sub = w.fragment(selection.Definition)
			}
		}
		if sub.depth > cost.depth {
			cost.depth = sub.depth
		}
		cost.total = saturatingAdd(cost.total, sub.total)
	}
	return cost
}

// fragment measures a fragment once per document and remembers it by name. A
// fragment's cost is the same wherever it is spread, and gqlparser's
// NoFragmentCyclesRule — in the default rule set gqlgen's executor validates
// with, so it has already run by the time an operation middleware sees the
// document — means following a spread terminates.
//
// The memo is what keeps this measurement linear in the document. Expanding a
// spread at every occurrence instead costs 2^n on a document whose n fragments
// each spread the previous one twice: legal, cycle-free, and about 40 bytes per
// fragment, so the guard would become a cheaper version of the denial of service
// it exists to refuse.
func (w recursionWalk) fragment(def *ast.FragmentDefinition) recursionCost {
	if cost, ok := w.fragments[def.Name]; ok {
		return cost
	}
	cost := w.measure(def.SelectionSet)
	w.fragments[def.Name] = cost
	return cost
}

// saturatingAdd adds two totals, stopping one past the limit. A document can
// multiply its own count — n fragments each spreading the previous one twice
// take the innermost fragment's count to 2^n — and the sum is only ever
// compared against the limit, so it is capped rather than left to overflow.
func saturatingAdd(a, b int) int {
	if sum := a + b; sum < maxRecursiveSelectionTotal+1 {
		return sum
	}
	return maxRecursiveSelectionTotal + 1
}

// recursionBoundAroundOperations refuses an operation that nests recursive-type
// fields past maxRecursiveSelectionDepth, or selects more than
// maxRecursiveSelectionTotal of them altogether, before any resolver runs.
//
// It is an OPERATION middleware rather than a field middleware because the bound
// is a property of the document's shape: it can be decided once, from the parsed
// and validated operation, and a refusal then costs one error instead of one per
// field the executor would otherwise reach. That also makes it right for
// subscriptions, whose shape is fixed when the client subscribes while their
// responses run for as long as the socket lives.
func recursionBoundAroundOperations(recursive map[string]bool) graphql.OperationMiddleware {
	return func(ctx context.Context, next graphql.OperationHandler) graphql.ResponseHandler {
		opCtx := graphql.GetOperationContext(ctx)
		cost := measureRecursion(opCtx.Operation.SelectionSet, recursive)
		if cost.depth > maxRecursiveSelectionDepth {
			return graphql.OneShot(graphql.ErrorResponse(ctx,
				"query nests recursive relationship fields %d levels deep, above the limit of %d; "+
					"select the id-valued fields (parentId, blockingIds, blockedByIds, mentionIds, mentionedByIds) instead of nesting",
				cost.depth, maxRecursiveSelectionDepth))
		}
		// "at least": the count saturates, so a document that multiplies its own
		// selections reports the ceiling rather than its true, larger total.
		if cost.total > maxRecursiveSelectionTotal {
			return graphql.OneShot(graphql.ErrorResponse(ctx,
				"query selects at least %d fields of a recursive type, above the limit of %d; "+
					"split it into smaller requests",
				cost.total, maxRecursiveSelectionTotal))
		}
		return next(ctx)
	}
}

// requestBodyLimitMiddleware caps how much of a request body the server will
// read. Without it a client can make the server buffer an arbitrarily large
// GraphQL document before parsing rejects it.
func requestBodyLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		next.ServeHTTP(w, r)
	})
}
