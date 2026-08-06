package graph

// MutationImplementors is the type-condition set graphql.CollectFields matches a
// root mutation selection's fragment spreads against.
//
// It re-exports the value generated.go's _Mutation dispatch passes, rather than
// letting an out-of-package caller hand-copy the literal. A caller that has to
// re-collect the same document — cmd/graphql.go, naming the root fields of a
// failed batch — must resolve it to exactly the fields the executor dispatched
// against. gqlgen derives both the generated variable's NAME and its VALUE from
// the mutation root type's own name (codegen/object.gotpl emits
// `var <lcFirst name>Implementors = []string{"<name>", ...interfaces}`), so a
// schema that renames that root (`schema { mutation: X }`) regenerates the
// variable under a new name and this alias stops compiling. A hand-copied
// literal would instead keep matching a type condition the schema no longer has,
// with no compiler or test signal.
//
// The slice is shared with generated.go, not copied — treat it as read-only.
var MutationImplementors = mutationImplementors
