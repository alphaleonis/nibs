package graph

import (
	"context"
	"sync"

	"github.com/alphaleonis/nibs/internal/nib"
)

// RequestCache memoizes per-request mention lookups so that a single GraphQL
// operation querying the same nib's mentions / mentionedBy across multiple
// selections (e.g. both `mentions { id }` and `mentionIds`, or the same
// relationship reached from several parent selections) does not re-run the
// reader lookup.
//
// Scope: one cache per GraphQL HTTP request. The cache is attached to the
// request context by requestCacheMiddleware in cmd/serve.go and read by the
// cached* resolver helpers below. CLI callers use the in-process executor
// and do not attach a cache — their contexts produce nil from
// RequestCacheFrom, in which case cached* helpers fall straight through to
// the reader (see TestCachedMentions_NilCacheFallsThrough).
//
// Keys are full (normalized) nib IDs. Callers should resolve short-form IDs
// via NibReader.NormalizeID before looking up; the cache itself does not
// normalize.
type RequestCache struct {
	mu          sync.Mutex
	mentions    map[string][]*nib.Nib
	mentionedBy map[string][]*nib.Nib
}

// NewRequestCache returns a ready-to-use cache.
func NewRequestCache() *RequestCache {
	return &RequestCache{
		mentions:    make(map[string][]*nib.Nib),
		mentionedBy: make(map[string][]*nib.Nib),
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

// cachedMentions returns the nibs mentioned by sourceID. When a RequestCache
// is attached to ctx, the result is memoized on first read. Callers must
// have already normalized sourceID to its full form.
func cachedMentions(ctx context.Context, reader NibReader, sourceID string) []*nib.Nib {
	cache := RequestCacheFrom(ctx)
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

// cachedMentionedBy returns the nibs that mention targetID. Semantics match
// cachedMentions; see its comment for rationale.
func cachedMentionedBy(ctx context.Context, reader NibReader, targetID string) []*nib.Nib {
	cache := RequestCacheFrom(ctx)
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
