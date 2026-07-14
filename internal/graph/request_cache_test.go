package graph

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
)

// countingReader wraps a NibReader to count FindMentions / FindMentionedBy
// invocations. Used to verify per-request memoization behavior.
type countingReader struct {
	*stubReader
	mentionsCalls        int32
	mentionedByCalls     int32
}

func (c *countingReader) FindMentions(fromID string) []*nib.Nib {
	atomic.AddInt32(&c.mentionsCalls, 1)
	return c.stubReader.FindMentions(fromID)
}

func (c *countingReader) FindMentionedBy(targetID string) []*nib.Nib {
	atomic.AddInt32(&c.mentionedByCalls, 1)
	return c.stubReader.FindMentionedBy(targetID)
}

func newCountingReader(t *testing.T) *countingReader {
	t.Helper()
	a := &nib.Nib{ID: "a", Status: "todo"}
	b := &nib.Nib{ID: "b", Status: "todo"}
	stub := &stubReader{
		nibs:    map[string]*nib.Nib{"a": a, "b": b},
		allNibs: []*nib.Nib{a, b},
		cfg:     config.Default(),
		mentionsOut: map[string][]*nib.Nib{
			"a": {b},
		},
		mentionsIn: map[string][]*nib.Nib{
			"b": {a},
		},
	}
	return &countingReader{stubReader: stub}
}

// Behavior 12: within one request (one RequestCache) two cachedMentions
// calls for the same ID share a result — the reader is hit exactly once.
func TestCachedMentions_MemoizesWithinRequest(t *testing.T) {
	reader := newCountingReader(t)
	ctx := WithRequestCache(context.Background(), NewRequestCache())

	first := cachedMentions(ctx, reader, "a")
	second := cachedMentions(ctx, reader, "a")

	if calls := atomic.LoadInt32(&reader.mentionsCalls); calls != 1 {
		t.Errorf("reader.FindMentions called %d times, want 1", calls)
	}
	if len(first) != 1 || first[0].ID != "b" {
		t.Fatalf("first = %v, want [b]", first)
	}
	// The memoized call must return the identical slice — first element
	// pointer equality is sufficient proof no re-fetch happened.
	if len(second) == 0 || second[0] != first[0] {
		t.Errorf("second call returned a fresh slice; want the cached pointer")
	}
}

func TestCachedMentionedBy_MemoizesWithinRequest(t *testing.T) {
	reader := newCountingReader(t)
	ctx := WithRequestCache(context.Background(), NewRequestCache())

	first := cachedMentionedBy(ctx, reader, "b")
	second := cachedMentionedBy(ctx, reader, "b")

	if calls := atomic.LoadInt32(&reader.mentionedByCalls); calls != 1 {
		t.Errorf("reader.FindMentionedBy called %d times, want 1", calls)
	}
	if len(first) != 1 || first[0].ID != "a" {
		t.Fatalf("first = %v, want [a]", first)
	}
	if len(second) == 0 || second[0] != first[0] {
		t.Errorf("second call returned a fresh slice; want the cached pointer")
	}
}

// Behavior 13: two distinct RequestCache instances each miss independently;
// the reader is called once per cache.
func TestCachedMentions_PerRequestIsolation(t *testing.T) {
	reader := newCountingReader(t)

	ctx1 := WithRequestCache(context.Background(), NewRequestCache())
	ctx2 := WithRequestCache(context.Background(), NewRequestCache())

	_ = cachedMentions(ctx1, reader, "a")
	_ = cachedMentions(ctx2, reader, "a")

	if calls := atomic.LoadInt32(&reader.mentionsCalls); calls != 2 {
		t.Errorf("reader.FindMentions called %d times, want 2 (one per request)", calls)
	}
}

// Behavior 14: nil cache (no RequestCache in ctx) falls through to a direct
// reader call on every invocation — CLI/test compatibility.
func TestCachedMentions_NilCacheFallsThrough(t *testing.T) {
	reader := newCountingReader(t)
	ctx := context.Background() // no cache attached

	_ = cachedMentions(ctx, reader, "a")
	_ = cachedMentions(ctx, reader, "a")
	_ = cachedMentionedBy(ctx, reader, "b")

	if calls := atomic.LoadInt32(&reader.mentionsCalls); calls != 2 {
		t.Errorf("reader.FindMentions calls = %d, want 2 (no cache, no dedup)", calls)
	}
	if calls := atomic.LoadInt32(&reader.mentionedByCalls); calls != 1 {
		t.Errorf("reader.FindMentionedBy calls = %d, want 1", calls)
	}
}

// RequestCacheFrom returns nil for a context with no cache attached — the
// sentinel the cached* helpers switch on.
func TestRequestCacheFrom_AbsentReturnsNil(t *testing.T) {
	if got := RequestCacheFrom(context.Background()); got != nil {
		t.Errorf("RequestCacheFrom(bare ctx) = %v, want nil", got)
	}
	cache := NewRequestCache()
	ctx := WithRequestCache(context.Background(), cache)
	if got := RequestCacheFrom(ctx); got != cache {
		t.Errorf("RequestCacheFrom(ctx) = %v, want attached cache %v", got, cache)
	}

	t.Run("nil context returns nil without panic", func(t *testing.T) {
		// Defensive guard: context.Value panics on a nil receiver, so the
		// production helper's `if ctx == nil { return nil }` is
		// load-bearing. Pin it so a refactor that drops the guard fails
		// here instead of in a resolver under load.
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("RequestCacheFrom(nil) panicked: %v", r)
			}
		}()
		//nolint:staticcheck // SA1012: passing nil is the point of this test — we're pinning the guard behavior.
		if got := RequestCacheFrom(nil); got != nil {
			t.Errorf("RequestCacheFrom(nil) = %v, want nil", got)
		}
	})
}

// TestRequestCacheMiddleware_DedupsWithinOneRequest is the end-to-end pin
// that the request-cache middleware pattern (attach a fresh RequestCache on
// every incoming HTTP request via WithRequestCache) threads all the way
// through to cachedMentions. Within one request, two cachedMentions calls
// for the same sourceID must hit the underlying reader exactly once; the
// second call is served from the cache.
//
// Lives in the graph package because cachedMentions is unexported — the
// equivalent cmd-package test (TestRequestCacheMiddleware_FreshCachePerRequest
// in cmd/serve_test.go) pins only that the middleware installs a fresh
// cache per request, not the dedup behavior.
func TestRequestCacheMiddleware_DedupsWithinOneRequest(t *testing.T) {
	reader := newCountingReader(t)

	// Middleware under test: the exact same pattern cmd/serve.go uses.
	//
	// KEEP IN SYNC with cmd/serve.go:requestCacheMiddleware. cachedMentions is
	// unexported, so this test cannot drive the real middleware through the cmd
	// package. If the production middleware gains behavior (additional ctx
	// values, pooling, size limits, etc.) this clone must be updated to match
	// or the HTTP-boundary dedup guarantee silently drifts.
	middleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := WithRequestCache(r.Context(), NewRequestCache())
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}

	// Handler makes two cachedMentions calls with the same sourceID, simulating
	// a single GraphQL operation that resolves mentions twice on one nib.
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = cachedMentions(r.Context(), reader, "a")
		_ = cachedMentions(r.Context(), reader, "a")
		w.WriteHeader(http.StatusOK)
	})

	h := middleware(inner)

	req := httptest.NewRequest(http.MethodGet, "/graphql", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d", rec.Code)
	}
	if calls := atomic.LoadInt32(&reader.mentionsCalls); calls != 1 {
		t.Errorf("reader.FindMentions called %d times within one request, want 1 (middleware-installed cache should dedup)", calls)
	}
}
