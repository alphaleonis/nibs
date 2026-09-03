package graph

import (
	"context"
	"sync"
)

// inversionKey identifies one queue inversion by the ids it is made of, so the
// set a mutation found before its write can be compared with the set it left
// without holding the live store pointers QueueInversion carries.
type inversionKey struct {
	milestone, ahead, blocker string
}

func keyOf(inv QueueInversion) inversionKey {
	return inversionKey{inv.Milestone, inv.Ahead.ID, inv.Blocker.ID}
}

// QueueInversionCollector gathers the inversions an operation's writes CREATE,
// for a caller that will render them.
//
// It exists because the lint has two renderings and must have one definition
// (decision 2.3): `nibs set`/`nibs mv` print a warning line on stderr, while a
// GraphQL response carries the pairs in `extensions.queueInversions`. Both read
// what the resolver put here, so the two entry points cannot disagree about
// what a write created.
//
// Scope is one operation, like RequestCache — and for a weaker reason: nothing
// here goes stale, but a collector outliving its operation would report the
// previous one's pairs alongside its own. It accumulates across the mutation
// fields of a single document on purpose, so a response carries every pair the
// document created rather than the last field's.
//
// The pairs hold live store pointers (see QueueInversion), so a caller whose
// result outlives the store lock must read the ids out rather than keep them.
type QueueInversionCollector struct {
	mu      sync.Mutex
	created []QueueInversion
}

func NewQueueInversionCollector() *QueueInversionCollector {
	return &QueueInversionCollector{}
}

func (c *QueueInversionCollector) add(inversions []QueueInversion) {
	if len(inversions) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.created = append(c.created, inversions...)
}

// Created returns the pairs collected so far, in the order the writes reported
// them. A copy, so a caller reading it cannot be surprised by a later write.
func (c *QueueInversionCollector) Created() []QueueInversion {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.created) == 0 {
		return nil
	}
	return append([]QueueInversion(nil), c.created...)
}

type queueInversionCtxKey struct{}

// WithQueueInversions returns a context carrying the collector the queue-shaping
// mutations report into.
func WithQueueInversions(ctx context.Context, c *QueueInversionCollector) context.Context {
	return context.WithValue(ctx, queueInversionCtxKey{}, c)
}

// QueueInversionsFrom retrieves a collector previously attached with
// WithQueueInversions, or nil when none is. Nil is a supported answer, not a
// bug — the same shape RequestCacheFrom has, and load-bearing for the same
// reason: pure unit tests and the direct resolver calls that never render a
// warning attach none, and the lint then costs them nothing (see
// TestQueueLintWithoutCollector).
func QueueInversionsFrom(ctx context.Context) *QueueInversionCollector {
	c, _ := ctx.Value(queueInversionCtxKey{}).(*QueueInversionCollector)
	return c
}

// beginQueueLint snapshots the inversions the subject is already part of, so
// endQueueLint can report only what the write adds. Nil — and no scan at all —
// when nobody is collecting: the scan walks the whole store, and a report no
// one reads is the one cost this lint must not impose on every write.
//
// Called BEFORE the write, and only from the paths that can create a pair; the
// judgment of which those are belongs to the caller, which is the only place
// that knows what the write is about to do.
func (r *Resolver) beginQueueLint(ctx context.Context, id string) map[inversionKey]bool {
	if QueueInversionsFrom(ctx) == nil {
		return nil
	}
	inversions := QueueInversionsInvolving(r.Reader, id)
	before := make(map[inversionKey]bool, len(inversions))
	for _, inv := range inversions {
		before[keyOf(inv)] = true
	}
	return before
}

// endQueueLint reports the pairs the write created — those the subject takes
// part in now and did not before. Called AFTER the write has landed, because an
// inversion is legal (plans state importance, dependencies state feasibility)
// and this is a lint rather than a refusal.
//
// A no-op when beginQueueLint returned nil, which is how a caller that skipped
// the snapshot skips the report without a second condition to keep in step.
func (r *Resolver) endQueueLint(ctx context.Context, id string, before map[inversionKey]bool) {
	if before == nil {
		return
	}
	c := QueueInversionsFrom(ctx)
	if c == nil {
		return
	}
	var created []QueueInversion
	for _, inv := range QueueInversionsInvolving(r.Reader, id) {
		if !before[keyOf(inv)] {
			created = append(created, inv)
		}
	}
	c.add(created)
}
