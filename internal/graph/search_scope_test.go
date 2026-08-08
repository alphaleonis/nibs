package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/executor"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
)

// searchScopeTerm is the term both fixtures below search for. It is longer than
// any id in them, so the id-match leg of Core.Search can never contribute a hit
// and every match is a full-text one — which is the leg the store-wide cap
// applies to.
const searchScopeTerm = "quarkfoo"

// TestRelationshipSearchIsNotBoundedByTheStoreWideLimit pins WHICH population a
// relationship-field search truncates.
//
// `children(filter: {search: q})` asks for "the children of X matching q". If the
// intersection is fed from a store-wide TOP-N answer, what comes back is instead
// "the children of X that are also among the store's global top-N hits for q" —
// a genuine child that ranks below the cutoff is dropped, silently, at exit 0.
// The relation is already a bounded working set, so nothing about it needs a
// store-wide cap to stay bounded.
//
// The fixture makes that cutoff bite: DefaultSearchLimit+200 short nibs all match
// the term, and the one child that matches buries it in a long body, so BM25's
// length normalization ranks it beneath every one of them. The premise assertion
// below proves the fixture still exercises the cap rather than passing for a
// trivial reason.
func TestRelationshipSearchIsNotBoundedByTheStoreWideLimit(t *testing.T) {
	resolver, core := setupTestResolver(t)

	// Noise: enough short, high-scoring matches to fill the store-wide cap and
	// then some, so the cap is reached before the child can be considered.
	for i := range nibcore.DefaultSearchLimit + 200 {
		id := fmt.Sprintf("nz%04d", i)
		mustCreate(t, core, &nib.Nib{
			ID:     id,
			Slug:   id,
			Title:  searchScopeTerm + " noise",
			Status: "todo",
		})
	}

	parent := &nib.Nib{ID: "par1", Slug: "parent", Title: "Parent", Status: "todo"}
	mustCreate(t, core, parent)
	// The term appears once in a long body: same match, lowest possible score.
	child := &nib.Nib{
		ID:     "kid1",
		Slug:   "child",
		Title:  "Child",
		Status: "todo",
		Parent: parent.ID,
		Body:   strings.Repeat("filler ", 4000) + searchScopeTerm,
	}
	mustCreate(t, core, child)

	// Premise: the store-wide answer really does drop this child. Without this,
	// a fixture that stopped reaching the cap would leave the assertion below
	// passing for a reason that has nothing to do with the bound.
	topHits, err := core.Search(searchScopeTerm)
	if err != nil {
		t.Fatalf("core.Search: %v", err)
	}
	for _, b := range topHits {
		if b.ID == child.ID {
			t.Fatalf("fixture no longer exercises the cap: the child is inside the store-wide top %d hits (%d returned), so this test would pass without the bound being fixed",
				nibcore.DefaultSearchLimit, len(topHits))
		}
	}

	got, err := resolver.Nib().Children(context.Background(), parent, &model.NibFilter{Search: strPtr(searchScopeTerm)}, nil)
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	assertNibIDs(t, got, []string{child.ID})
}

// countingSearchReader counts every index query a request costs, whichever
// search entry point makes it. Counting both entry points into ONE total is
// deliberate: the assertion is about how many times the index is consulted per
// request, not about which method was picked to consult it, so it survives a
// change of entry point instead of going quietly green.
type countingSearchReader struct {
	NibReader
	mu           sync.Mutex
	indexQueries int
}

func (r *countingSearchReader) Search(query string) ([]*nib.Nib, error) {
	r.mu.Lock()
	r.indexQueries++
	r.mu.Unlock()
	return r.NibReader.Search(query)
}

func (r *countingSearchReader) SearchAll(query string) ([]*nib.Nib, error) {
	r.mu.Lock()
	r.indexQueries++
	r.mu.Unlock()
	return r.NibReader.SearchAll(query)
}

func (r *countingSearchReader) queries() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.indexQueries
}

// TestNestedRelationshipSearchQueriesTheIndexOncePerRequest pins the cost of one
// term across a whole request.
//
// `nibs { children(filter: {search: q}) }` resolves children once per nib in the
// outer result, and each of those runs the same term against the index — N
// identical queries for one term in one request. One is not cheap: a write lock
// for lazy index init, a Bleve query, then a read lock over a full scan of the
// store for id matches. The per-operation cache already dedups the mention
// lookups for exactly this reason; search now rides the same mechanism.
//
// Driven through the real gqlgen executor rather than a resolver loop, because
// that is what makes the fan-out real: gqlgen resolves the children field of the
// outer list's elements CONCURRENTLY, so a cache that merely memoizes after the
// fact — without collapsing concurrent misses — would still issue several
// queries and this count would be non-deterministic rather than wrong-and-stable.
func TestNestedRelationshipSearchQueriesTheIndexOncePerRequest(t *testing.T) {
	resolver, core := setupTestResolver(t)

	const parents = 4
	for p := range parents {
		pid := fmt.Sprintf("par%d", p)
		mustCreate(t, core, &nib.Nib{ID: pid, Slug: pid, Title: "Parent", Status: "todo"})
		for c := range 2 {
			cid := fmt.Sprintf("kid%d%d", p, c)
			mustCreate(t, core, &nib.Nib{
				ID:     cid,
				Slug:   cid,
				Title:  searchScopeTerm + " child",
				Status: "todo",
				Parent: pid,
			})
		}
	}

	spy := &countingSearchReader{NibReader: resolver.Reader}
	resolver.Reader = spy

	es := NewExecutableSchema(Config{Resolvers: resolver})
	exec := executor.New(es)

	ctx := graphql.StartOperationTrace(context.Background())
	// Mirrors requestCacheAroundOperations in cmd/serve.go: one cache per operation.
	ctx = WithRequestCache(ctx, NewRequestCache())
	params := &graphql.RawParams{
		Query: `query { nibs(filter: {status: ["todo"]}) { id children(filter: {search: "` + searchScopeTerm + `"}) { id } } }`,
	}
	opCtx, errs := exec.CreateOperationContext(ctx, params)
	if len(errs) > 0 {
		t.Fatalf("CreateOperationContext: %v", errs)
	}
	ctx = graphql.WithOperationContext(ctx, opCtx)
	handler, ctx := exec.DispatchOperation(ctx, opCtx)
	resp := handler(ctx)
	if len(resp.Errors) > 0 {
		t.Fatalf("query returned errors: %v", resp.Errors)
	}

	var payload struct {
		Nibs []struct {
			ID       string `json:"id"`
			Children []struct {
				ID string `json:"id"`
			} `json:"children"`
		} `json:"nibs"`
	}
	if err := json.Unmarshal(resp.Data, &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	// Guard the query SHAPE: the count below means nothing unless the term was
	// actually evaluated against several parents' children.
	withMatches := 0
	for _, b := range payload.Nibs {
		if len(b.Children) > 0 {
			withMatches++
		}
	}
	if len(payload.Nibs) < parents {
		t.Fatalf("outer query returned %d nibs, want at least %d — the fan-out this test measures is gone", len(payload.Nibs), parents)
	}
	if withMatches != parents {
		t.Fatalf("%d nibs came back with matching children, want %d — the term is no longer being evaluated per parent", withMatches, parents)
	}

	if got := spy.queries(); got != 1 {
		t.Errorf("the request cost %d index queries, want 1 (one per parent is the N+1 this guards)", got)
	}
}

// TestMutationSeesItsOwnWritesThroughARelationshipSearch pins that the search
// memo cannot answer a mutation from before the mutation's own writes.
//
// GraphQL executes mutation root fields SERIALLY, so one document can read,
// write, and read again. A memo scoped to the whole operation freezes the first
// answer: field `a` fills it, field `b` creates a nib matching the same term,
// and field `c` is handed the pre-`b` answer — the same response reporting a
// child set that omits the child it just created, with no error and exit 0.
// Core reindexes synchronously on write, so an unmemoized read sees `b`
// immediately; the staleness would be entirely the memo's.
//
// Driven through the real gqlgen executor because the serial root-field
// execution is the whole premise — a resolver-by-resolver loop would prove
// nothing about ordering.
func TestMutationSeesItsOwnWritesThroughARelationshipSearch(t *testing.T) {
	resolver, core := setupTestResolver(t)

	// An epic, so the task created below is a legal child of it.
	mustCreate(t, core, &nib.Nib{ID: "par0", Slug: "parent", Title: "Parent", Type: "epic", Status: "todo"})

	es := NewExecutableSchema(Config{Resolvers: resolver})
	exec := executor.New(es)

	ctx := graphql.StartOperationTrace(context.Background())
	ctx = WithRequestCache(ctx, NewRequestCache())
	params := &graphql.RawParams{
		Query: `mutation {
			a: updateNib(id: "par0", input: {title: "Parent"}) { id children(filter: {search: "` + searchScopeTerm + `"}) { id } }
			b: createNib(input: {title: "` + searchScopeTerm + ` entry", parent: "par0"}) { id }
			c: updateNib(id: "par0", input: {title: "Parent"}) { id children(filter: {search: "` + searchScopeTerm + `"}) { id } }
		}`,
	}
	opCtx, errs := exec.CreateOperationContext(ctx, params)
	if len(errs) > 0 {
		t.Fatalf("CreateOperationContext: %v", errs)
	}
	ctx = graphql.WithOperationContext(ctx, opCtx)
	handler, ctx := exec.DispatchOperation(ctx, opCtx)
	resp := handler(ctx)
	if len(resp.Errors) > 0 {
		t.Fatalf("mutation returned errors: %v", resp.Errors)
	}

	type nibWithChildren struct {
		ID       string `json:"id"`
		Children []struct {
			ID string `json:"id"`
		} `json:"children"`
	}
	var payload struct {
		A nibWithChildren `json:"a"`
		B struct {
			ID string `json:"id"`
		} `json:"b"`
		C nibWithChildren `json:"c"`
	}
	if err := json.Unmarshal(resp.Data, &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	// Premise: the term matches nothing before `b` runs. Without this, `c`
	// finding the nib would prove nothing about ordering — the answer could
	// have been correct from the start.
	if len(payload.A.Children) != 0 {
		t.Fatalf("field a saw %d matching children before the create; the fixture no longer establishes the read-write-read ordering this test measures", len(payload.A.Children))
	}
	if payload.B.ID == "" {
		t.Fatal("field b returned no id; the create did not happen")
	}

	if len(payload.C.Children) != 1 || payload.C.Children[0].ID != payload.B.ID {
		t.Errorf("field c saw children %v, want exactly the nib field b created (%s) — a mutation must not be served a search answer memoized before its own writes",
			payload.C.Children, payload.B.ID)
	}
}
