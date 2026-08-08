package graph

import (
	"context"
	"sync"

	"github.com/99designs/gqlgen/graphql"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/vektah/gqlparser/v2/ast"
)

// RequestCache memoizes per-operation reader lookups so that a single GraphQL
// operation asking the same question from multiple selections (e.g. both
// `mentions { id }` and `mentionIds`, the same relationship reached from
// several parent selections, or one search term evaluated on every element of
// an outer list) does not re-run the reader lookup.
//
// Scope: one cache per GraphQL OPERATION, and never across two of them. Every
// entry holds live store state, so a cache outliving its operation serves
// answers from before a write that has already happened. The entry points that
// attach one are:
//
//   - requestCacheAroundOperations (cmd/serve.go) — a gqlgen AroundOperations
//     middleware, so HTTP POST, HTTP GET and each message on a long-lived
//     WebSocket connection all get their own. Attaching per HTTP request would
//     NOT be equivalent: the WebSocket transport derives every operation's
//     context from the upgrade request, so one cache would serve the whole
//     connection.
//   - newQueryContext (cmd/graphql.go) — the in-process CLI executor, which
//     runs one operation per invocation.
//
// Callers that drive resolvers directly rather than through an executor attach
// no cache: cmd/rel.go's BFS calls ApplyFilter with the cobra context, and
// ProjectionResolver is handed context.Background(). Pure unit tests are the
// third such caller. All of them produce nil from RequestCacheFrom, and the
// cached* helpers below fall straight through to the reader in that case (see
// TestCachedMentions_NilCacheFallsThrough) — that path is load-bearing, not
// vestigial.
//
// Mention keys are full (normalized) nib IDs. Callers should resolve short-form
// IDs via NibReader.NormalizeID before looking up; the cache itself does not
// normalize. Search keys are the raw query string — see cachedSearchAllIDs for
// why that is the whole key.
type RequestCache struct {
	mu          sync.Mutex
	mentions    map[string][]*nib.Nib
	mentionedBy map[string][]*nib.Nib
	searchAll   map[string]*searchEntry
}

// searchEntry holds one memoized SearchAll answer, reduced to the membership
// set every caller actually wants. Storing the set rather than the []*nib.Nib
// does two things: it moves the O(M) map build out of the per-parent path and
// into the once-per-term path, and it keeps the entry from pinning live store
// pointers for the length of the operation.
//
// Unlike the mention maps it is filled through a sync.Once rather than a plain
// double-check, so concurrent misses on the same term COLLAPSE into a single
// reader call instead of each running its own and racing to store the winner.
// The mention lookups tolerate that duplicate work; a search does not — gqlgen
// resolves an outer list's children fields concurrently, so tolerating it would
// leave the very fan-out this cache exists to remove partly in place, and its
// cost non-deterministic. Once.Do publishes ids and err together with the same
// happens-before edge, so both are safe to read after it returns.
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

// requestCacheCtxKey is the unexported type used to key RequestCache values
// on a context. Following the convention in net/http, using a private type
// prevents collisions with any other package's context keys.
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
// bypass memoization and read the store directly.
//
// It bypasses for MUTATION operations, and that is a correctness rule rather
// than a tuning choice. GraphQL executes mutation root fields serially and
// gqlgen honors it, so one document can write between two reads of the same
// question: field a resolves `children(filter: {search: q})`, field b creates a
// matching nib, field c resolves the same selection. A memo filled by a would
// answer c from before b's write, and the response would report, at exit 0, a
// child set missing the child the same response just created. Core reindexes
// synchronously on write, so an unmemoized read sees b immediately — the
// staleness is entirely the memo's, and dropping the memo removes it for the
// search entries and for the mention entries alike.
//
// The trade is the fan-out protection inside a mutation document, which is the
// cheap half: mutation documents name a few root fields, they do not select a
// relationship field across a large outer list the way a query does.
//
// Deciding here rather than at attach time is deliberate — it is one choke
// point that every entry point and every future one passes through, so a new
// caller cannot reintroduce the staleness by attaching a cache of its own.
func memoFor(ctx context.Context) *RequestCache {
	if isMutationOperation(ctx) {
		return nil
	}
	return RequestCacheFrom(ctx)
}

// isMutationOperation reports whether ctx is executing a mutation. Contexts
// with no operation context at all — direct resolver callers and unit tests —
// are not mutations for this purpose: they perform no GraphQL-level write
// sequencing, so nothing can go stale mid-operation.
func isMutationOperation(ctx context.Context) bool {
	if ctx == nil || !graphql.HasOperationContext(ctx) {
		return false
	}
	op := graphql.GetOperationContext(ctx).Operation
	return op != nil && op.Operation == ast.Mutation
}

// cachedMentions returns the nibs mentioned by sourceID. When a RequestCache
// is attached to ctx, the result is memoized on first read. Callers must
// have already normalized sourceID to its full form.
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
	// Double-check pattern: if another goroutine populated the key while we
	// were fetching, prefer the existing entry so callers who already
	// observed it see a stable pointer.
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
// straight through to the reader, exactly as the mention helpers do.
//
// It returns the SET rather than the ranked slice because membership is all any
// caller wants: filterBySearch intersects a relation against it and keeps the
// relation's own order. Building the set once per term, inside the memo, is
// what makes the memo actually remove the fan-out — memoizing only the slice
// still leaves an O(M) map build on every parent, over an M that SearchAll
// deliberately leaves uncapped.
//
// THE QUERY STRING IS THE WHOLE KEY, and that is only true because every caller
// asks for the same thing: the UNCAPPED answer. NibReader has two search entry
// points that differ solely in their bound, so a second caller routing the
// capped Search through this cache would collide with an uncapped entry under
// an identical key and be handed a set the store-wide cap should have trimmed.
// If that ever becomes desirable, the bound belongs in the key. Nothing else
// varies the result: SearchAll is a function of the term and the store, the
// reader is fixed for the operation, and memoFor withholds the cache from the
// one operation shape that can write to the store while it runs.
//
// The error is memoized alongside the result, so a failing index is queried
// once per operation rather than once per parent. It reaches every caller
// unwrapped; the first relationship field to receive it fails the whole
// response anyway (see filterBySearch).
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

	// Outside the cache lock: a slow query for one term must not block a
	// different one. Once.Do serializes only the callers sharing this term, and
	// gives every one of them a happens-before edge to the fields it fills.
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
