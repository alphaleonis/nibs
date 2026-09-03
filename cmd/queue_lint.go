package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/99designs/gqlgen/graphql"
	"github.com/alphaleonis/nibs/internal/graph"
)

// queueInversionWarning renders the pairs a mutation created (decision 2.3) as
// a single warning line, or "" when it created none.
//
// The DEFINITION of an inversion and the before/after diff that makes this fire
// once — at the creating write — both live behind the resolver now
// (internal/graph/queue_lint.go), so `nibs serve`, `nibs query` and a direct
// resolver call answer the same question this does. What stays here is the
// sentence: the CLI renders a warning on stderr, a GraphQL response carries the
// same pairs in `extensions.queueInversions`.
//
// It is a lint, not a refusal: an inversion is legal — plans state importance,
// dependencies state feasibility — so the write has already landed by the time
// this runs, and the warning only names the pairs so the author can decide
// whether the order was intended. Every pair goes on the one line, so a move
// that inverts against several blockers is reported once, not once per blocker.
// Ids come from filenames and front-matter links, so they cross the rendering
// boundary like every other id on stderr.
func queueInversionWarning(created []graph.QueueInversion) string {
	if len(created) == 0 {
		return ""
	}
	pairs := make([]string, len(created))
	for i, inv := range created {
		pairs[i] = fmt.Sprintf("%s is ahead of %s, which still blocks it",
			stripControlChars(inv.Ahead.ID), stripControlChars(inv.Blocker.ID))
	}
	return fmt.Sprintf("warning: queue order and dependencies disagree in milestone %s: %s (inversions are legal — plans state importance, dependencies state feasibility; reorder with `nibs mv <id> --queue --after|--before <anchor>` if the order was unintended)",
		stripControlChars(created[0].Milestone), strings.Join(pairs, "; "))
}

// queueInversionAroundOperations attaches a collector to each GraphQL operation
// the SERVER runs and lifts whatever that operation's writes created into the
// response's `extensions.queueInversions`.
//
// Once per OPERATION, and never from a resolver, because
// graphql.RegisterExtension panics twice over: on a second registration of the
// same key, which a document carrying two queue-shaping mutation fields would
// reach, and on a missing response context, which every direct resolver call
// lacks — the CLI drives resolvers directly, not through an executor.
// Collecting into the context and registering once here avoids both.
//
// The in-process executor behind `nibs query` does not use this: it reduces the
// response to its data, so it attaches its own collector and renders the
// warning instead (see executeQuery).
func queueInversionAroundOperations(ctx context.Context, next graphql.OperationHandler) graphql.ResponseHandler {
	collector := graph.NewQueueInversionCollector()
	inner := next(graph.WithQueueInversions(ctx, collector))
	return func(ctx context.Context) *graphql.Response {
		resp := inner(ctx)
		if resp == nil {
			return nil
		}
		created := collector.Created()
		if len(created) == 0 {
			return resp
		}
		if resp.Extensions == nil {
			resp.Extensions = map[string]any{}
		}
		resp.Extensions["queueInversions"] = queueInversionExtension(created)
		return resp
	}
}

// queueInversionExtension is the wire shape of a reported pair: the three ids
// alone. QueueInversion holds live store pointers, which a response outlives —
// and unlike the warning line these ids are not being rendered to a terminal,
// so nothing here strips control characters.
func queueInversionExtension(created []graph.QueueInversion) []map[string]string {
	out := make([]map[string]string, len(created))
	for i, inv := range created {
		out[i] = map[string]string{
			"milestone": inv.Milestone,
			"ahead":     inv.Ahead.ID,
			"blocker":   inv.Blocker.ID,
		}
	}
	return out
}
