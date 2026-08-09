package graph

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/vektah/gqlparser/v2/ast"
)

// countingReader wraps a NibReader to count FindMentions / FindMentionedBy /
// SearchAll invocations. Used to verify per-request memoization behavior.
//
// searchDelay, when set, holds each SearchAll call open for that long, which is
// what gives the concurrent callers in TestCachedSearchAll_CollapsesConcurrentMisses
// a window to pile into.
type countingReader struct {
	*stubReader
	mentionsCalls    int32
	mentionedByCalls int32
	searchAllCalls   int32
	searchDelay      time.Duration
}

func (c *countingReader) FindMentions(fromID string) []*nib.Nib {
	atomic.AddInt32(&c.mentionsCalls, 1)
	return c.stubReader.FindMentions(fromID)
}

func (c *countingReader) FindMentionedBy(targetID string) []*nib.Nib {
	atomic.AddInt32(&c.mentionedByCalls, 1)
	return c.stubReader.FindMentionedBy(targetID)
}

// SearchAll counts and delegates. It does NOT go through the embedded stub's
// own SearchAll, because that one is not safe to call concurrently (it bumps an
// unsynchronized counter) and these tests drive it from several goroutines.
//
// Each call returns a FRESH slice over the fixture's nibs. Handing back the
// same map-stored slice every time would make any "the caller got the memoized
// object, not a re-fetch" assertion hold even with memoization removed entirely
// — a test that cannot fail.
func (c *countingReader) SearchAll(query string) ([]*nib.Nib, error) {
	atomic.AddInt32(&c.searchAllCalls, 1)
	if c.searchDelay > 0 {
		time.Sleep(c.searchDelay)
	}
	if c.searchErr != nil {
		return nil, c.searchErr
	}
	return append([]*nib.Nib(nil), c.searchOut[query]...), nil
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
		searchOut: map[string][]*nib.Nib{
			"term":  {a, b},
			"other": {a},
		},
	}
	return &countingReader{stubReader: stub}
}

// cachedSearchAllIDs memoizes per term within one operation: the reader answers
// each distinct term once, however many relationship fields ask for it.
func TestCachedSearchAllIDs_MemoizesWithinOperation(t *testing.T) {
	reader := newCountingReader(t)
	ctx := WithRequestCache(context.Background(), NewRequestCache())

	first, err := cachedSearchAllIDs(ctx, reader, "term")
	if err != nil {
		t.Fatalf("cachedSearchAllIDs: %v", err)
	}
	second, err := cachedSearchAllIDs(ctx, reader, "term")
	if err != nil {
		t.Fatalf("cachedSearchAllIDs: %v", err)
	}
	if _, err := cachedSearchAllIDs(ctx, reader, "other"); err != nil {
		t.Fatalf("cachedSearchAllIDs: %v", err)
	}

	if calls := atomic.LoadInt32(&reader.searchAllCalls); calls != 2 {
		t.Errorf("reader.SearchAll called %d times, want 2 (one per distinct term)", calls)
	}
	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("results = %d and %d ids, want 2 each", len(first), len(second))
	}
	if _, ok := first["a"]; !ok {
		t.Errorf("result = %v, want the membership set {a, b}", first)
	}
	// The memoized set is handed back as the same map, so repeat callers share
	// one object rather than each building an equal one. The reader returns a
	// fresh slice per call, so this can only hold if the entry was memoized.
	if reflect.ValueOf(first).Pointer() != reflect.ValueOf(second).Pointer() {
		t.Error("second call returned a different map; the entry was refetched rather than memoized")
	}
}

// A term's answer is cached per operation, not globally: two operations each pay.
func TestCachedSearchAllIDs_PerOperationIsolation(t *testing.T) {
	reader := newCountingReader(t)

	ctx1 := WithRequestCache(context.Background(), NewRequestCache())
	ctx2 := WithRequestCache(context.Background(), NewRequestCache())

	if _, err := cachedSearchAllIDs(ctx1, reader, "term"); err != nil {
		t.Fatalf("cachedSearchAllIDs: %v", err)
	}
	if _, err := cachedSearchAllIDs(ctx2, reader, "term"); err != nil {
		t.Fatalf("cachedSearchAllIDs: %v", err)
	}

	if calls := atomic.LoadInt32(&reader.searchAllCalls); calls != 2 {
		t.Errorf("reader.SearchAll called %d times, want 2 (one per operation)", calls)
	}
}

// With no cache on the context — a command driving a resolver directly, such as
// cmd/rel.go's BFS or ProjectionResolver, and unit tests — the helper falls
// straight through to the reader. (CLI GraphQL invocations are NOT in this set:
// newQueryContext attaches a cache of their own.)
func TestCachedSearchAllIDs_NilCacheFallsThrough(t *testing.T) {
	reader := newCountingReader(t)
	ctx := context.Background() // no cache attached

	if _, err := cachedSearchAllIDs(ctx, reader, "term"); err != nil {
		t.Fatalf("cachedSearchAllIDs: %v", err)
	}
	if _, err := cachedSearchAllIDs(ctx, reader, "term"); err != nil {
		t.Fatalf("cachedSearchAllIDs: %v", err)
	}

	if calls := atomic.LoadInt32(&reader.searchAllCalls); calls != 2 {
		t.Errorf("reader.SearchAll calls = %d, want 2 (no cache, no dedup)", calls)
	}
}

// A MUTATION operation bypasses the memo even with a cache attached, because a
// mutation's own root fields can write to the store between two reads of the
// same term. Pins memoFor's operation-type check at the helper level; the
// end-to-end consequence is pinned by
// TestMutationSeesItsOwnWritesThroughARelationshipSearch.
func TestCachedSearchAllIDs_MutationBypassesTheMemo(t *testing.T) {
	reader := newCountingReader(t)
	ctx := WithRequestCache(mutationOperationContext(t), NewRequestCache())

	if _, err := cachedSearchAllIDs(ctx, reader, "term"); err != nil {
		t.Fatalf("cachedSearchAllIDs: %v", err)
	}
	if _, err := cachedSearchAllIDs(ctx, reader, "term"); err != nil {
		t.Fatalf("cachedSearchAllIDs: %v", err)
	}

	if calls := atomic.LoadInt32(&reader.searchAllCalls); calls != 2 {
		t.Errorf("reader.SearchAll calls = %d, want 2 — a mutation must re-read, since its own earlier root fields may have written", calls)
	}

	// The same context under a query operation memoizes, so the bypass keys on
	// the operation type and not on something incidental to the fixture.
	queryReader := newCountingReader(t)
	queryCtx := WithRequestCache(queryOperationContext(t), NewRequestCache())
	if _, err := cachedSearchAllIDs(queryCtx, queryReader, "term"); err != nil {
		t.Fatalf("cachedSearchAllIDs: %v", err)
	}
	if _, err := cachedSearchAllIDs(queryCtx, queryReader, "term"); err != nil {
		t.Fatalf("cachedSearchAllIDs: %v", err)
	}
	if calls := atomic.LoadInt32(&queryReader.searchAllCalls); calls != 1 {
		t.Errorf("query operation: reader.SearchAll calls = %d, want 1", calls)
	}
}

// The mention memos take the same bypass, for the same reason: a mutation can
// rewrite a body — and therefore the mention graph — between two reads.
func TestCachedMentions_MutationBypassesTheMemo(t *testing.T) {
	reader := newCountingReader(t)
	ctx := WithRequestCache(mutationOperationContext(t), NewRequestCache())

	_ = cachedMentions(ctx, reader, "a")
	_ = cachedMentions(ctx, reader, "a")
	_ = cachedMentionedBy(ctx, reader, "b")
	_ = cachedMentionedBy(ctx, reader, "b")

	if calls := atomic.LoadInt32(&reader.mentionsCalls); calls != 2 {
		t.Errorf("reader.FindMentions calls = %d, want 2 (mutation bypasses the memo)", calls)
	}
	if calls := atomic.LoadInt32(&reader.mentionedByCalls); calls != 2 {
		t.Errorf("reader.FindMentionedBy calls = %d, want 2 (mutation bypasses the memo)", calls)
	}
}

// mutationOperationContext returns a context carrying a gqlgen OperationContext
// for a mutation document, which is what memoFor keys on.
func mutationOperationContext(t *testing.T) context.Context {
	t.Helper()
	return operationContextOfType(t, ast.Mutation)
}

// queryOperationContext is mutationOperationContext's control case.
func queryOperationContext(t *testing.T) context.Context {
	t.Helper()
	return operationContextOfType(t, ast.Query)
}

func operationContextOfType(t *testing.T, op ast.Operation) context.Context {
	t.Helper()
	return graphql.WithOperationContext(context.Background(), &graphql.OperationContext{
		Operation: &ast.OperationDefinition{Operation: op},
	})
}

// TestCachedSearchAllIDs_CollapsesConcurrentMisses is the property a plain
// double-check memo does not have.
//
// gqlgen resolves the children field of an outer list's elements concurrently,
// so the callers that share a term arrive TOGETHER and all miss. Memoizing only
// after the fact would let each of them run its own query — the fan-out the
// cache exists to remove, surviving in the one shape that actually occurs.
//
// The barrier is two-sided on purpose. `ready` holds the release until every
// caller goroutine has actually started and reached the barrier (its next
// statement is start.Wait()), so they go together; without it, a goroutine the
// runtime had not scheduled yet when `start` opened could arrive after the
// winner had already filled the entry, and the test would rest on searchDelay
// outrunning scheduler latency rather than on the collapse it claims to measure.
func TestCachedSearchAllIDs_CollapsesConcurrentMisses(t *testing.T) {
	reader := newCountingReader(t)
	reader.searchDelay = 20 * time.Millisecond
	ctx := WithRequestCache(context.Background(), NewRequestCache())

	const callers = 8
	var ready sync.WaitGroup
	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	ready.Add(callers)
	results := make([]map[string]struct{}, callers)
	for i := range callers {
		done.Add(1)
		go func() {
			defer done.Done()
			ready.Done()
			start.Wait()
			got, err := cachedSearchAllIDs(ctx, reader, "term")
			if err != nil {
				t.Errorf("cachedSearchAllIDs: %v", err)
				return
			}
			results[i] = got
		}()
	}
	ready.Wait()
	start.Done()
	done.Wait()

	if calls := atomic.LoadInt32(&reader.searchAllCalls); calls != 1 {
		t.Errorf("reader.SearchAll called %d times, want 1 — concurrent misses on one term must collapse", calls)
	}
	for i, got := range results {
		if len(got) != 2 {
			t.Errorf("caller %d got %d ids, want 2", i, len(got))
		}
	}
}

// An index that cannot answer reports the failure to every caller, and is asked
// once rather than once per parent. The error travels out unwrapped, the same
// value filterBySearch propagates.
func TestCachedSearchAllIDs_MemoizesTheFailure(t *testing.T) {
	reader := newCountingReader(t)
	indexDown := errors.New("search index unavailable")
	reader.searchErr = indexDown
	ctx := WithRequestCache(context.Background(), NewRequestCache())

	for i := range 3 {
		got, err := cachedSearchAllIDs(ctx, reader, "term")
		if !errors.Is(err, indexDown) {
			t.Fatalf("call %d: error = %v, want the reader's own failure", i, err)
		}
		if got != nil {
			t.Errorf("call %d: result = %v, want nil alongside the error", i, got)
		}
	}
	if calls := atomic.LoadInt32(&reader.searchAllCalls); calls != 1 {
		t.Errorf("reader.SearchAll called %d times, want 1 (the failure is memoized too)", calls)
	}
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
// reader call on every invocation. That is the path for callers that drive
// resolvers without an executor — cmd/rel.go's BFS, ProjectionResolver — and
// for unit tests. CLI GraphQL invocations attach a cache (newQueryContext) and
// are not in this set.
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

// TestRequestCacheOperationMiddleware_DedupsWithinOneOperation is the
// end-to-end pin that the operation-middleware pattern (attach a fresh
// RequestCache to every executed operation via WithRequestCache) threads all
// the way through to cachedMentions. Within one operation, two cachedMentions
// calls for the same sourceID must hit the underlying reader exactly once; the
// second call is served from the cache.
//
// Lives in the graph package because cachedMentions is unexported — the
// equivalent cmd-package tests (TestRequestCacheAroundOperations_FreshCachePerOperation
// and TestWebSocketOperationsDoNotShareARequestCache in cmd/serve_test.go) pin
// the scope of the real middleware, not the dedup behavior.
func TestRequestCacheOperationMiddleware_DedupsWithinOneOperation(t *testing.T) {
	reader := newCountingReader(t)

	// Middleware under test: the exact same pattern cmd/serve.go uses, in the
	// real gqlgen middleware types.
	//
	// KEEP IN SYNC with cmd/serve.go:requestCacheAroundOperations. cachedMentions
	// is unexported, so this test cannot drive the real middleware through the
	// cmd package. If the production middleware gains behavior (additional ctx
	// values, pooling, size limits, etc.) this clone must be updated to match or
	// the dedup guarantee silently drifts.
	var middleware graphql.OperationMiddleware = func(ctx context.Context, next graphql.OperationHandler) graphql.ResponseHandler {
		return next(WithRequestCache(ctx, NewRequestCache()))
	}

	// Stands in for an operation that resolves mentions twice on one nib.
	inner := func(ctx context.Context) graphql.ResponseHandler {
		_ = cachedMentions(ctx, reader, "a")
		_ = cachedMentions(ctx, reader, "a")
		return graphql.OneShot(&graphql.Response{})
	}

	if got := middleware(context.Background(), inner); got == nil {
		t.Fatal("middleware returned a nil response handler")
	}
	if calls := atomic.LoadInt32(&reader.mentionsCalls); calls != 1 {
		t.Errorf("reader.FindMentions called %d times within one operation, want 1 (middleware-installed cache should dedup)", calls)
	}
}
