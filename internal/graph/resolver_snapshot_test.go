package graph

import (
	"context"
	"sync"
	"testing"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/executor"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
)

// TestRelationshipResolversReturnSnapshots pins the rule that the Nib
// relationship resolvers (Parent, Children, BlockedBy, Blocking) return
// snapshots (clones) of related nibs, never the live *nib.Nib pointers held in
// nibcore.Core's store. gqlgen reads scalar fields (e.g. path) off the returned
// pointers asynchronously and off the store lock, while Archive/Unarchive
// rewrite a stored nib's Path in place under the lock. Handing out a live
// pointer is a data race; a snapshot detaches the returned value from the store.
//
// Each subtest captures a resolver's returned nib and its Path, then archives
// the underlying stored nib (which rewrites its Path in place under c.mu). If
// the resolver returned the live pointer, the captured Path mutates and the
// assertion fails (proving the bug on unfixed code). With the clone fix the
// captured Path is frozen.
func TestRelationshipResolversReturnSnapshots(t *testing.T) {
	t.Run("Parent", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		nr := resolver.Nib()
		ctx := context.Background()

		parent := createTestNib(t, core, "parent1", "Parent", "todo")
		child := &nib.Nib{ID: "child1", Slug: "child", Title: "Child", Status: "todo", Parent: parent.ID}
		mustCreate(t, core, child)

		got, err := nr.Parent(ctx, child)
		if err != nil {
			t.Fatalf("Parent resolver: %v", err)
		}
		if got == nil {
			t.Fatal("Parent resolver returned nil")
		}
		before := got.Path

		if err := core.Archive(parent.ID); err != nil {
			t.Fatalf("Archive: %v", err)
		}

		if got.Path != before {
			t.Errorf("captured parent Path mutated after Archive: got %q, want frozen %q (resolver leaked the live store pointer)", got.Path, before)
		}
	})

	t.Run("Children", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		nr := resolver.Nib()
		ctx := context.Background()

		parent := createTestNib(t, core, "parent1", "Parent", "todo")
		child := &nib.Nib{ID: "child1", Slug: "child", Title: "Child", Status: "todo", Parent: parent.ID}
		mustCreate(t, core, child)

		got, err := nr.Children(ctx, parent, nil, nil)
		if err != nil {
			t.Fatalf("Children resolver: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("Children resolver returned %d nibs, want 1", len(got))
		}
		before := got[0].Path

		if err := core.Archive(child.ID); err != nil {
			t.Fatalf("Archive: %v", err)
		}

		if got[0].Path != before {
			t.Errorf("captured child Path mutated after Archive: got %q, want frozen %q (resolver leaked the live store pointer)", got[0].Path, before)
		}
	})

	t.Run("BlockedBy", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		nr := resolver.Nib()
		ctx := context.Background()

		blocker := createTestNib(t, core, "blocker1", "Blocker", "todo")
		blocked := &nib.Nib{ID: "blocked1", Slug: "blocked", Title: "Blocked", Status: "todo", BlockedBy: []string{blocker.ID}}
		mustCreate(t, core, blocked)

		got, err := nr.BlockedBy(ctx, blocked, nil)
		if err != nil {
			t.Fatalf("BlockedBy resolver: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("BlockedBy resolver returned %d nibs, want 1", len(got))
		}
		before := got[0].Path

		if err := core.Archive(blocker.ID); err != nil {
			t.Fatalf("Archive: %v", err)
		}

		if got[0].Path != before {
			t.Errorf("captured blocker Path mutated after Archive: got %q, want frozen %q (resolver leaked the live store pointer)", got[0].Path, before)
		}
	})

	t.Run("Blocking", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		nr := resolver.Nib()
		ctx := context.Background()

		blocker := createTestNib(t, core, "blocker1", "Blocker", "todo")
		blocked := &nib.Nib{ID: "blocked1", Slug: "blocked", Title: "Blocked", Status: "todo", BlockedBy: []string{blocker.ID}}
		mustCreate(t, core, blocked)

		// Blocking(blocker) returns the nibs that list blocker in their BlockedBy.
		got, err := nr.Blocking(ctx, blocker, nil)
		if err != nil {
			t.Fatalf("Blocking resolver: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("Blocking resolver returned %d nibs, want 1", len(got))
		}
		before := got[0].Path

		if err := core.Archive(blocked.ID); err != nil {
			t.Fatalf("Archive: %v", err)
		}

		if got[0].Path != before {
			t.Errorf("captured blocking nib Path mutated after Archive: got %q, want frozen %q (resolver leaked the live store pointer)", got[0].Path, before)
		}
	})
}

// TestRelationshipResolverRaceUnderExecutor drives the parent- and blockedBy-path
// reads through the real gqlgen executor concurrently with Archive/Unarchive on
// those related nibs. The child's `parent { path }` and `blockedBy { path }`
// fields are marshaled asynchronously off the values the resolvers return, while
// Archive/Unarchive rewrite the parent's and blocker's Path in place under c.mu.
//
// Because the resolvers now snapshot via Reader.GetSnapshot (clone-under-lock),
// the marshaled values are detached copies and this is `-race` clean. It fails
// under `-race` if a resolver ever reverts to handing out a live c.nibs pointer
// (the nibs-yr1j regression), so it is a real detector guard, not skipped.
func TestRelationshipResolverRaceUnderExecutor(t *testing.T) {
	resolver, core := setupTestResolver(t)

	parent := createTestNib(t, core, "parent1", "Parent", "todo")
	blocker := createTestNib(t, core, "blocker1", "Blocker", "todo")
	child := &nib.Nib{ID: "child1", Slug: "child", Title: "Child", Status: "todo", Parent: parent.ID, BlockedBy: []string{blocker.ID}}
	mustCreate(t, core, child)

	es := NewExecutableSchema(Config{Resolvers: resolver})
	exec := executor.New(es)

	runQuery := func() {
		ctx := graphql.StartOperationTrace(context.Background())
		params := &graphql.RawParams{
			Query:     `query { nib(id: "child1") { parent { path } blockedBy { path } } }`,
			Variables: map[string]any{},
		}
		opCtx, errs := exec.CreateOperationContext(ctx, params)
		if len(errs) > 0 {
			t.Errorf("CreateOperationContext: %v", errs)
			return
		}
		ctx = graphql.WithOperationContext(ctx, opCtx)
		handler, ctx := exec.DispatchOperation(ctx, opCtx)
		// Assert the query actually succeeds and marshals `path`. Without this,
		// a future regression that made the resolver error out (no `path` marshal)
		// would never open the race window, leaving the -race guard silently green.
		resp := handler(ctx)
		if len(resp.Errors) > 0 {
			t.Errorf("query returned errors (race window never opened): %v", resp.Errors)
		}
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); runQuery() }()
		go func() {
			defer wg.Done()
			_ = core.Archive(parent.ID)
			_ = core.Unarchive(parent.ID)
			_ = core.Archive(blocker.ID)
			_ = core.Unarchive(blocker.ID)
		}()
	}
	wg.Wait()
}

// TestQueryResolversReturnSnapshots extends the detachment rule to the
// query-level and mention resolvers left untouched by the relationship-resolver
// conversion: the top-level Nib and Nibs resolvers (including Nibs' search
// branch), and the nib-level Mentions and MentionedBy resolvers. Each returns
// nibs read out of nibcore.Core's store; gqlgen marshals their scalar fields
// (e.g. path) asynchronously and off the store lock, while Archive rewrites a
// stored nib's Path in place under c.mu. Handing out a live pointer is a data
// race; a snapshot detaches the returned value from the store.
//
// Each subtest captures a resolver's returned nib and its Path, then archives
// the underlying stored nib (which rewrites its Path in place under c.mu). If
// the resolver returned the live pointer, the captured Path mutates and the
// assertion fails (proving the bug on unfixed code). With the snapshot fix the
// captured Path is frozen.
func TestQueryResolversReturnSnapshots(t *testing.T) {
	t.Run("QueryNib", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		qr := resolver.Query()
		ctx := context.Background()

		target := createTestNib(t, core, "target1", "Target", "todo")

		got, err := qr.Nib(ctx, target.ID)
		if err != nil {
			t.Fatalf("Nib resolver: %v", err)
		}
		if got == nil {
			t.Fatal("Nib resolver returned nil")
		}
		before := got.Path

		if err := core.Archive(target.ID); err != nil {
			t.Fatalf("Archive: %v", err)
		}

		if got.Path != before {
			t.Errorf("captured nib Path mutated after Archive: got %q, want frozen %q (resolver leaked the live store pointer)", got.Path, before)
		}
	})

	t.Run("QueryNibs", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		qr := resolver.Query()
		ctx := context.Background()

		target := createTestNib(t, core, "target1", "Target", "todo")

		got, err := qr.Nibs(ctx, nil, nil)
		if err != nil {
			t.Fatalf("Nibs resolver: %v", err)
		}
		idx := nibIndexByID(got, target.ID)
		if idx < 0 {
			t.Fatalf("Nibs resolver did not return %q", target.ID)
		}
		before := got[idx].Path

		if err := core.Archive(target.ID); err != nil {
			t.Fatalf("Archive: %v", err)
		}

		if got[idx].Path != before {
			t.Errorf("captured nib Path mutated after Archive: got %q, want frozen %q (resolver leaked the live store pointer)", got[idx].Path, before)
		}
	})

	t.Run("QueryNibsSearch", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		qr := resolver.Query()
		ctx := context.Background()

		target := createTestNib(t, core, "target1", "Findable Target", "todo")

		search := "Findable"
		got, err := qr.Nibs(ctx, &model.NibFilter{Search: &search}, nil)
		if err != nil {
			t.Fatalf("Nibs (search) resolver: %v", err)
		}
		idx := nibIndexByID(got, target.ID)
		if idx < 0 {
			t.Fatalf("Nibs (search) resolver did not return %q", target.ID)
		}
		before := got[idx].Path

		if err := core.Archive(target.ID); err != nil {
			t.Fatalf("Archive: %v", err)
		}

		if got[idx].Path != before {
			t.Errorf("captured search-hit Path mutated after Archive: got %q, want frozen %q (resolver leaked the live store pointer)", got[idx].Path, before)
		}
	})

	t.Run("Mentions", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		nr := resolver.Nib()
		ctx := context.Background()

		target := createTestNib(t, core, "target1", "Target", "todo")
		source := &nib.Nib{ID: "source1", Slug: "source", Title: "Source", Status: "todo", Body: "mentions #target1"}
		mustCreate(t, core, source)

		got, err := nr.Mentions(ctx, source, nil)
		if err != nil {
			t.Fatalf("Mentions resolver: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("Mentions resolver returned %d nibs, want 1", len(got))
		}
		before := got[0].Path

		if err := core.Archive(target.ID); err != nil {
			t.Fatalf("Archive: %v", err)
		}

		if got[0].Path != before {
			t.Errorf("captured mention Path mutated after Archive: got %q, want frozen %q (resolver leaked the live store pointer)", got[0].Path, before)
		}
	})

	t.Run("MentionedBy", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		nr := resolver.Nib()
		ctx := context.Background()

		target := createTestNib(t, core, "target1", "Target", "todo")
		source := &nib.Nib{ID: "source1", Slug: "source", Title: "Source", Status: "todo", Body: "mentions #target1"}
		mustCreate(t, core, source)

		got, err := nr.MentionedBy(ctx, target, nil)
		if err != nil {
			t.Fatalf("MentionedBy resolver: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("MentionedBy resolver returned %d nibs, want 1", len(got))
		}
		before := got[0].Path

		if err := core.Archive(source.ID); err != nil {
			t.Fatalf("Archive: %v", err)
		}

		if got[0].Path != before {
			t.Errorf("captured mentionedBy Path mutated after Archive: got %q, want frozen %q (resolver leaked the live store pointer)", got[0].Path, before)
		}
	})
}

// TestMentionIDResolversDropDeletedNibs pins the id/object symmetry the mention
// resolvers must keep under a concurrent delete on the server path (a
// RequestCache attached to ctx). The object resolvers Mentions/MentionedBy skip
// a mention whose GetSnapshot returns !ok; the sibling id resolvers
// MentionIds/MentionedByIds must drop the same nib, or a single
// { mentionIds  mentions { id } } response self-contradicts (mentionIds lists a
// nib the object list omitted).
//
// The RequestCache memoizes the mention slice — holding the target's live
// pointer — on the first (warm) resolver call. A delete then removes the target
// from the store but leaves that pointer readable in the cached slice, so the
// second resolver call still sees it. Without the existence filter on the id
// resolvers this test's id list still contains the deleted nib (it fails); with
// the filter both lists agree.
func TestMentionIDResolversDropDeletedNibs(t *testing.T) {
	t.Run("MentionIds", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		nr := resolver.Nib()
		ctx := WithRequestCache(context.Background(), NewRequestCache())

		target := createTestNib(t, core, "target1", "Target", "todo")
		source := &nib.Nib{ID: "source1", Slug: "source", Title: "Source", Status: "todo", Body: "mentions #target1"}
		mustCreate(t, core, source)

		// Warm the per-request cache while target still exists.
		if _, err := nr.MentionIds(ctx, source); err != nil {
			t.Fatalf("MentionIds (warm cache): %v", err)
		}

		// Concurrent delete: target leaves the store, but the cached slice still
		// holds its (now-orphaned) pointer.
		if err := core.Delete(target.ID); err != nil {
			t.Fatalf("Delete: %v", err)
		}

		ids, err := nr.MentionIds(ctx, source)
		if err != nil {
			t.Fatalf("MentionIds: %v", err)
		}
		objs, err := nr.Mentions(ctx, source, nil)
		if err != nil {
			t.Fatalf("Mentions: %v", err)
		}

		if len(ids) != 0 {
			t.Errorf("MentionIds returned %v after target deleted; want [] to match the empty Mentions object list (existence filter must drop the deleted nib)", ids)
		}
		if len(objs) != len(ids) {
			t.Errorf("mentionIds (%d) and mentions (%d) disagree on element count under concurrent delete", len(ids), len(objs))
		}
	})

	t.Run("MentionedByIds", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		nr := resolver.Nib()
		ctx := WithRequestCache(context.Background(), NewRequestCache())

		target := createTestNib(t, core, "target1", "Target", "todo")
		source := &nib.Nib{ID: "source1", Slug: "source", Title: "Source", Status: "todo", Body: "mentions #target1"}
		mustCreate(t, core, source)

		// Warm the per-request cache while source still exists.
		if _, err := nr.MentionedByIds(ctx, target); err != nil {
			t.Fatalf("MentionedByIds (warm cache): %v", err)
		}

		if err := core.Delete(source.ID); err != nil {
			t.Fatalf("Delete: %v", err)
		}

		ids, err := nr.MentionedByIds(ctx, target)
		if err != nil {
			t.Fatalf("MentionedByIds: %v", err)
		}
		objs, err := nr.MentionedBy(ctx, target, nil)
		if err != nil {
			t.Fatalf("MentionedBy: %v", err)
		}

		if len(ids) != 0 {
			t.Errorf("MentionedByIds returned %v after source deleted; want [] to match the empty MentionedBy object list (existence filter must drop the deleted nib)", ids)
		}
		if len(objs) != len(ids) {
			t.Errorf("mentionedByIds (%d) and mentionedBy (%d) disagree on element count under concurrent delete", len(ids), len(objs))
		}
	})
}

// TestQueryAndMentionResolverRaceUnderExecutor drives the query-level and
// mention resolvers (nib, nibs, nibs+search, mentions, mentionedBy) through the
// real gqlgen executor concurrently with Archive/Unarchive on the nibs they
// return. Their `path` fields are marshaled asynchronously off the values the
// resolvers return, while Archive/Unarchive rewrite the stored nibs' Path in
// place under c.mu.
//
// Because the resolvers now snapshot via Reader.GetSnapshot (clone-under-lock),
// the marshaled values are detached copies and this is `-race` clean. It fails
// under `-race` if a resolver ever reverts to handing out a live c.nibs pointer,
// so it is a real detector guard, not skipped.
func TestQueryAndMentionResolverRaceUnderExecutor(t *testing.T) {
	resolver, core := setupTestResolver(t)

	target := createTestNib(t, core, "target1", "Findable Target", "todo")
	source := &nib.Nib{ID: "source1", Slug: "source", Title: "Source", Status: "todo", Body: "mentions #target1"}
	mustCreate(t, core, source)

	es := NewExecutableSchema(Config{Resolvers: resolver})
	exec := executor.New(es)

	runQuery := func() {
		ctx := graphql.StartOperationTrace(context.Background())
		params := &graphql.RawParams{
			Query: `query {
				a: nib(id: "source1") { path mentions { path } }
				b: nib(id: "target1") { path mentionedBy { path } }
				c: nibs { path }
				d: nibs(filter: { search: "Findable" }) { path }
			}`,
			Variables: map[string]any{},
		}
		opCtx, errs := exec.CreateOperationContext(ctx, params)
		if len(errs) > 0 {
			t.Errorf("CreateOperationContext: %v", errs)
			return
		}
		ctx = graphql.WithOperationContext(ctx, opCtx)
		handler, ctx := exec.DispatchOperation(ctx, opCtx)
		// Assert the query actually succeeds and marshals `path`. Without this,
		// a future regression that made a resolver error out (no `path` marshal)
		// would never open the race window, leaving the -race guard silently green.
		resp := handler(ctx)
		if len(resp.Errors) > 0 {
			t.Errorf("query returned errors (race window never opened): %v", resp.Errors)
		}
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); runQuery() }()
		go func() {
			defer wg.Done()
			_ = core.Archive(target.ID)
			_ = core.Unarchive(target.ID)
			_ = core.Archive(source.ID)
			_ = core.Unarchive(source.ID)
		}()
	}
	wg.Wait()
}

// nibIndexByID returns the index of the first nib with the given ID, or -1.
func nibIndexByID(nibs []*nib.Nib, id string) int {
	for i, b := range nibs {
		if b.ID == id {
			return i
		}
	}
	return -1
}
