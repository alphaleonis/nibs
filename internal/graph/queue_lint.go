package graph

import (
	"context"
	"sync"
)

// inversionKey identifies one queue inversion by the ids it is made of, so a
// before/after comparison holds no live store pointer.
type inversionKey struct {
	milestone, ahead, blocker string
}

func keyOf(inv QueueInversion) inversionKey {
	return inversionKey{inv.Milestone, inv.Ahead.ID, inv.Blocker.ID}
}

// QueueInversionCollector gathers the inversions an operation's writes CREATE,
// for a caller that will render them. The CLI's warning line and the served
// response's `extensions.queueInversions` both render what the resolver put
// here, so the lint has one definition and two renderings.
//
// Scope is one operation, like RequestCache. It accumulates across the mutation
// fields of a single document on purpose, so a response carries every pair the
// document created rather than the last field's.
//
// The pairs hold live store pointers (see QueueInversion) and go stale when the
// store installs a new object — read the ids out rather than keeping them.
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
// them — a copy, so a later write cannot alter it.
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
// WithQueueInversions, or nil when none is. Nil is a supported answer: unit
// tests and direct resolver calls attach none, and the lint then costs them
// nothing.
func QueueInversionsFrom(ctx context.Context) *QueueInversionCollector {
	c, _ := ctx.Value(queueInversionCtxKey{}).(*QueueInversionCollector)
	return c
}

// beginQueueLint snapshots the inversions the subject is already part of, so
// endQueueLint can report only what the write adds. Nil — and no scan at all —
// when nobody is collecting: the scan walks the whole store.
//
// Call it BEFORE the write, and only from a path that can create a pair; which
// paths those are is the caller's judgment.
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
// part in now and did not before. Call it AFTER the write lands: an inversion
// is legal, so this is a lint rather than a refusal.
//
// A no-op when beginQueueLint returned nil, so a caller that skipped the
// snapshot skips the report with no second condition to keep in step.
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
