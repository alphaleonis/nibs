package graph

// MutationImplementors is the type-condition set graphql.CollectFields matches a
// root mutation selection's fragment spreads against.
//
// It re-exports the value generated.go's _Mutation dispatch passes, so an
// out-of-package caller that re-collects the same document (cmd/graphql.go,
// naming the root fields of a failed batch) resolves exactly the fields the
// executor dispatched against. gqlgen derives both that variable's NAME and its
// VALUE from the mutation root type's own name, so renaming that root breaks this
// alias at compile time where a hand-copied literal would go quiet.
//
// The slice is shared with generated.go, not copied — treat it as read-only.
var MutationImplementors = mutationImplementors
