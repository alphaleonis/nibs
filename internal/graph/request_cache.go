package graph

import (
	"context"
	"sync"

	"github.com/99designs/gqlgen/graphql"
	"github.com/alphaleonis/nibs/internal/membership"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/vektah/gqlparser/v2/ast"
)

// RequestCache memoizes per-operation reader lookups, so one GraphQL operation
// asking the same question from several selections — both `mentions { id }` and
// `mentionIds`, or one search term evaluated on every element of an outer list —
// does not re-run the reader lookup.
//
// Scope: one cache per GraphQL OPERATION, never across two. Every entry holds
// live store state, so a cache outliving its operation serves answers from
// before a write that has already happened. It is attached in
// requestCacheAroundOperations (cmd/serve.go) and newQueryContext
// (cmd/graphql.go).
//
// Mention keys are full (normalized) nib IDs — resolve short-form IDs via
// NibReader.NormalizeID first, the cache does not normalize. Search keys are the
// raw query string; see cachedSearchAllIDs for why that is the whole key.
type RequestCache struct {
	mu          sync.Mutex
	mentions    map[string][]*nib.Nib
	mentionedBy map[string][]*nib.Nib
	searchAll   map[string]*searchEntry

	// The operation's membership View — see cachedMembershipView.
	membershipOnce sync.Once
	membershipView *membership.View
}

// searchEntry holds one memoized SearchAll answer, reduced to the membership
// set every caller wants: the O(M) map build moves out of the per-parent path,
// and the entry pins no live store pointers for the operation's length.
//
// Filled through a sync.Once rather than a plain double-check, so concurrent
// misses on the same term COLLAPSE into a single reader call. Once.Do publishes
// ids and err together with the same happens-before edge, so both are safe to
// read after it returns.
type searchEntry struct {
	once sync.Once
	ids  map[string]struct{}
	err  error
}

// NewRequestCache returns a ready-to-use cache.
func NewRequestCache() *RequestCache {
	return &RequestCache{
		mentions:    make(map[string][]*nib.Nib),
		mentionedBy: make(map[string][]*nib.Nib),
		searchAll:   make(map[string]*searchEntry),
	}
}

// requestCacheCtxKey keys RequestCache values on a context. Private type, per
// the net/http convention, so it cannot collide with another package's keys.
type requestCacheCtxKey struct{}

// WithRequestCache returns a new context carrying the given cache.
func WithRequestCache(ctx context.Context, cache *RequestCache) context.Context {
	return context.WithValue(ctx, requestCacheCtxKey{}, cache)
}

// RequestCacheFrom retrieves a RequestCache previously attached via
// WithRequestCache. Returns nil when no cache is present — the sentinel
// that cached* helpers use to fall straight through to the reader.
func RequestCacheFrom(ctx context.Context) *RequestCache {
	if ctx == nil {
		return nil
	}
	if v, ok := ctx.Value(requestCacheCtxKey{}).(*RequestCache); ok {
		return v
	}
	return nil
}

// memoFor returns the cache the cached* helpers should memoize into, or nil to
// bypass the memo and read the store directly. Route a new cached* helper
// through it rather than through RequestCacheFrom.
//
// The memo is only for SINGLE-RESPONSE operations: it is safe exactly when no
// store write can land between two reads served by one memo. Two operation
// shapes break that, and both are withheld:
//
//   - MUTATION. GraphQL executes mutation root fields serially and gqlgen
//     honors it, so one document can write between two reads of the same
//     question, and a memo filled by the first answers the second from before
//     that write.
//
//   - SUBSCRIPTION. gqlgen dispatches the operation once per subscribe message
//     and resolves every pushed event under that same cache-carrying context,
//     each event preceded by a store write. A memo filled at the first event
//     would answer the socket's whole life from pre-write state, and pin that
//     event's store pointers just as long.
//
// Core reindexes synchronously on write, so an unmemoized read sees the write
// immediately.
func memoFor(ctx context.Context) *RequestCache {
	switch operationType(ctx) {
	case ast.Mutation, ast.Subscription:
		return nil
	}
	return RequestCacheFrom(ctx)
}

// operationType reports which kind of GraphQL operation ctx is executing, or ""
// for a context with no operation context at all — a direct resolver caller or
// a unit test, both of which memoize.
func operationType(ctx context.Context) ast.Operation {
	if ctx == nil || !graphql.HasOperationContext(ctx) {
		return ""
	}
	op := graphql.GetOperationContext(ctx).Operation
	if op == nil {
		return ""
	}
	return op.Operation
}

// cachedMentions returns the nibs mentioned by sourceID, memoized per
// operation. Normalize sourceID to its full form before calling.
func cachedMentions(ctx context.Context, reader NibReader, sourceID string) []*nib.Nib {
	cache := memoFor(ctx)
	if cache == nil {
		return reader.FindMentions(sourceID)
	}
	cache.mu.Lock()
	if v, ok := cache.mentions[sourceID]; ok {
		cache.mu.Unlock()
		return v
	}
	cache.mu.Unlock()

	// Run the lookup outside the lock so concurrent cache users for
	// *different* keys aren't serialized on our reader call.
	result := reader.FindMentions(sourceID)

	cache.mu.Lock()
	// Another goroutine may have populated the key while we fetched; prefer the
	// existing entry so callers already holding it see a stable pointer.
	if v, ok := cache.mentions[sourceID]; ok {
		cache.mu.Unlock()
		return v
	}
	cache.mentions[sourceID] = result
	cache.mu.Unlock()
	return result
}

// cachedSearchAllIDs returns the IDs of every nib matching query as a
// membership set, memoized per operation. With no cache on ctx it falls
// straight through to the reader.
//
// It returns the SET rather than the ranked slice because membership is all any
// caller wants: filterBySearch intersects a relation against it and keeps the
// relation's own order.
//
// THE QUERY STRING IS THE WHOLE KEY, which holds only because every caller asks
// for the UNCAPPED answer. NibReader's two search entry points differ solely in
// their bound, so routing the capped Search through this cache would collide
// with an uncapped entry under an identical key. Put the bound in the key first.
//
// The error is memoized alongside the result, so a failing index is queried once
// per operation rather than once per parent.
func cachedSearchAllIDs(ctx context.Context, reader NibReader, query string) (map[string]struct{}, error) {
	cache := memoFor(ctx)
	if cache == nil {
		return searchAllIDs(reader, query)
	}

	cache.mu.Lock()
	entry, ok := cache.searchAll[query]
	if !ok {
		entry = &searchEntry{}
		cache.searchAll[query] = entry
	}
	cache.mu.Unlock()

	// Outside the cache lock, so a slow query for one term does not block a
	// different one. Once.Do serializes only the callers sharing this term and
	// gives each a happens-before edge to the fields it fills.
	entry.once.Do(func() {
		entry.ids, entry.err = searchAllIDs(reader, query)
	})
	return entry.ids, entry.err
}

// searchAllIDs runs the uncapped search and reduces it to a membership set.
func searchAllIDs(reader NibReader, query string) (map[string]struct{}, error) {
	matches, err := reader.SearchAll(query)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]struct{}, len(matches))
	for _, b := range matches {
		ids[b.ID] = struct{}{}
	}
	return ids, nil
}

// cachedMembershipView returns the operation's membership View, computed on
// first use and shared by every ApplyFilter call within the operation. With no
// cache on ctx it falls straight through to a fresh Compute. Per-operation
// reuse is the scope the membership package's live-pointer discipline names
// ("build it once per command or per GraphQL operation").
//
// The entry is unkeyed — the View is a function of the store alone — and filled
// through a sync.Once for the reason searchEntry is.
func cachedMembershipView(ctx context.Context, reader NibReader) *membership.View {
	cache := memoFor(ctx)
	if cache == nil {
		return membership.Compute(reader.All())
	}
	cache.membershipOnce.Do(func() {
		cache.membershipView = membership.Compute(reader.All())
	})
	return cache.membershipView
}

// cachedMentionedBy returns the nibs that mention targetID. Semantics match
// cachedMentions; see its comment for rationale.
func cachedMentionedBy(ctx context.Context, reader NibReader, targetID string) []*nib.Nib {
	cache := memoFor(ctx)
	if cache == nil {
		return reader.FindMentionedBy(targetID)
	}
	cache.mu.Lock()
	if v, ok := cache.mentionedBy[targetID]; ok {
		cache.mu.Unlock()
		return v
	}
	cache.mu.Unlock()

	result := reader.FindMentionedBy(targetID)

	cache.mu.Lock()
	if v, ok := cache.mentionedBy[targetID]; ok {
		cache.mu.Unlock()
		return v
	}
	cache.mentionedBy[targetID] = result
	cache.mu.Unlock()
	return result
}
