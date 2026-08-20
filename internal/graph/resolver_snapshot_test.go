package graph

import (
	"context"
	"errors"
	"fmt"
	"slices"
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

// TestMutationResolversReturnSnapshots extends the detachment rule to every
// mutation resolver that returns nib data. Each returns a nib that gqlgen
// marshals asynchronously and off the store lock, while Archive rewrites a
// stored nib's Path in place under c.mu. Handing out the live c.nibs pointer
// (the one Writer.Create/Writer.Update installed, or the one Reader.Get handed
// back) is a data race; a GetSnapshot clone detaches the returned value.
//
// Each subtest drives a mutation resolver, captures the returned nib's Path,
// then archives the underlying stored nib (which rewrites its Path in place
// under c.mu). If the resolver returned the live pointer, the captured Path
// mutates and the assertion fails (proving the bug on unfixed code). With the
// snapshot fix the captured Path is frozen.
func TestMutationResolversReturnSnapshots(t *testing.T) {
	t.Run("CreateNib", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		mr := resolver.Mutation()
		ctx := context.Background()

		got, err := mr.CreateNib(ctx, model.CreateNibInput{Title: "Fresh"})
		if err != nil {
			t.Fatalf("CreateNib resolver: %v", err)
		}
		if got == nil {
			t.Fatal("CreateNib resolver returned nil")
		}
		before := got.Path

		if err := core.Archive(got.ID); err != nil {
			t.Fatalf("Archive: %v", err)
		}

		if got.Path != before {
			t.Errorf("captured nib Path mutated after Archive: got %q, want frozen %q (resolver leaked the live store pointer)", got.Path, before)
		}
	})

	t.Run("UpdateNib", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		mr := resolver.Mutation()
		ctx := context.Background()

		target := createTestNib(t, core, "target1", "Target", "todo")

		newTitle := "Updated Title"
		got, err := mr.UpdateNib(ctx, target.ID, model.UpdateNibInput{Title: &newTitle})
		if err != nil {
			t.Fatalf("UpdateNib resolver: %v", err)
		}
		if got == nil {
			t.Fatal("UpdateNib resolver returned nil")
		}
		before := got.Path

		if err := core.Archive(target.ID); err != nil {
			t.Fatalf("Archive: %v", err)
		}

		if got.Path != before {
			t.Errorf("captured nib Path mutated after Archive: got %q, want frozen %q (resolver leaked the live store pointer)", got.Path, before)
		}
	})

	t.Run("SetParent", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		mr := resolver.Mutation()
		ctx := context.Background()

		parent := &nib.Nib{ID: "parent1", Slug: "parent", Title: "Parent", Status: "todo", Type: "feature"}
		mustCreate(t, core, parent)
		child := createTestNib(t, core, "child1", "Child", "todo")

		got, err := mr.SetParent(ctx, child.ID, &parent.ID, nil)
		if err != nil {
			t.Fatalf("SetParent resolver: %v", err)
		}
		if got == nil {
			t.Fatal("SetParent resolver returned nil")
		}
		before := got.Path

		if err := core.Archive(child.ID); err != nil {
			t.Fatalf("Archive: %v", err)
		}

		if got.Path != before {
			t.Errorf("captured nib Path mutated after Archive: got %q, want frozen %q (resolver leaked the live store pointer)", got.Path, before)
		}
	})

	t.Run("AddBlocking", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		mr := resolver.Mutation()
		ctx := context.Background()

		source := createTestNib(t, core, "source1", "Source", "todo")
		target := createTestNib(t, core, "target1", "Target", "todo")

		got, err := mr.AddBlocking(ctx, source.ID, target.ID)
		if err != nil {
			t.Fatalf("AddBlocking resolver: %v", err)
		}
		if got == nil {
			t.Fatal("AddBlocking resolver returned nil")
		}
		before := got.Path

		if err := core.Archive(source.ID); err != nil {
			t.Fatalf("Archive: %v", err)
		}

		if got.Path != before {
			t.Errorf("captured nib Path mutated after Archive: got %q, want frozen %q (resolver leaked the live store pointer)", got.Path, before)
		}
	})

	t.Run("RemoveBlocking", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		mr := resolver.Mutation()
		ctx := context.Background()

		source := createTestNib(t, core, "source1", "Source", "todo")
		target := &nib.Nib{ID: "target1", Slug: "target", Title: "Target", Status: "todo", BlockedBy: []string{"source1"}}
		mustCreate(t, core, target)

		got, err := mr.RemoveBlocking(ctx, source.ID, target.ID)
		if err != nil {
			t.Fatalf("RemoveBlocking resolver: %v", err)
		}
		if got == nil {
			t.Fatal("RemoveBlocking resolver returned nil")
		}
		before := got.Path

		if err := core.Archive(source.ID); err != nil {
			t.Fatalf("Archive: %v", err)
		}

		if got.Path != before {
			t.Errorf("captured nib Path mutated after Archive: got %q, want frozen %q (resolver leaked the live store pointer)", got.Path, before)
		}
	})

	t.Run("AddBlockedBy", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		mr := resolver.Mutation()
		ctx := context.Background()

		blocked := createTestNib(t, core, "blocked1", "Blocked", "todo")
		blocker := createTestNib(t, core, "blocker1", "Blocker", "todo")

		got, err := mr.AddBlockedBy(ctx, blocked.ID, blocker.ID, nil)
		if err != nil {
			t.Fatalf("AddBlockedBy resolver: %v", err)
		}
		if got == nil {
			t.Fatal("AddBlockedBy resolver returned nil")
		}
		before := got.Path

		if err := core.Archive(blocked.ID); err != nil {
			t.Fatalf("Archive: %v", err)
		}

		if got.Path != before {
			t.Errorf("captured nib Path mutated after Archive: got %q, want frozen %q (resolver leaked the live store pointer)", got.Path, before)
		}
	})

	t.Run("RemoveBlockedBy", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		mr := resolver.Mutation()
		ctx := context.Background()

		blocker := createTestNib(t, core, "blocker1", "Blocker", "todo")
		blocked := &nib.Nib{ID: "blocked1", Slug: "blocked", Title: "Blocked", Status: "todo", BlockedBy: []string{"blocker1"}}
		mustCreate(t, core, blocked)

		got, err := mr.RemoveBlockedBy(ctx, blocked.ID, blocker.ID, nil)
		if err != nil {
			t.Fatalf("RemoveBlockedBy resolver: %v", err)
		}
		if got == nil {
			t.Fatal("RemoveBlockedBy resolver returned nil")
		}
		before := got.Path

		if err := core.Archive(blocked.ID); err != nil {
			t.Fatalf("Archive: %v", err)
		}

		if got.Path != before {
			t.Errorf("captured nib Path mutated after Archive: got %q, want frozen %q (resolver leaked the live store pointer)", got.Path, before)
		}
	})

	t.Run("ReorderNib", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		mr := resolver.Mutation()
		ctx := context.Background()

		a := createTestNib(t, core, "a1", "A", "todo")
		b := createTestNib(t, core, "b1", "B", "todo")

		got, err := mr.ReorderNib(ctx, a.ID, &b.ID, nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("ReorderNib resolver: %v", err)
		}
		if got == nil {
			t.Fatal("ReorderNib resolver returned nil")
		}
		before := got.Path

		if err := core.Archive(a.ID); err != nil {
			t.Fatalf("Archive: %v", err)
		}

		if got.Path != before {
			t.Errorf("captured nib Path mutated after Archive: got %q, want frozen %q (resolver leaked the live store pointer)", got.Path, before)
		}
	})

	t.Run("ReorderChildren", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		mr := resolver.Mutation()
		ctx := context.Background()

		parent := createTestNib(t, core, "parent1", "Parent", "todo")
		c1 := &nib.Nib{ID: "c1", Slug: "c1", Title: "C1", Status: "todo", Parent: parent.ID}
		c2 := &nib.Nib{ID: "c2", Slug: "c2", Title: "C2", Status: "todo", Parent: parent.ID}
		mustCreate(t, core, c1)
		mustCreate(t, core, c2)

		got, err := mr.ReorderChildren(ctx, parent.ID, []string{c2.ID, c1.ID}, nil)
		if err != nil {
			t.Fatalf("ReorderChildren resolver: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("ReorderChildren resolver returned %d nibs, want 2", len(got))
		}
		before := got[0].Path

		if err := core.Archive(got[0].ID); err != nil {
			t.Fatalf("Archive: %v", err)
		}

		if got[0].Path != before {
			t.Errorf("captured nib Path mutated after Archive: got %q, want frozen %q (resolver leaked the live store pointer)", got[0].Path, before)
		}
	})

	t.Run("ReorderSiblings", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		mr := resolver.Mutation()
		ctx := context.Background()

		parent := createTestNib(t, core, "parent1", "Parent", "todo")
		c1 := &nib.Nib{ID: "c1", Slug: "c1", Title: "C1", Status: "todo", Parent: parent.ID}
		c2 := &nib.Nib{ID: "c2", Slug: "c2", Title: "C2", Status: "todo", Parent: parent.ID}
		mustCreate(t, core, c1)
		mustCreate(t, core, c2)

		first := true
		got, err := mr.ReorderSiblings(ctx, []string{c1.ID}, nil, nil, &first, nil)
		if err != nil {
			t.Fatalf("ReorderSiblings resolver: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("ReorderSiblings resolver returned %d nibs, want 1", len(got))
		}
		before := got[0].Path

		if err := core.Archive(got[0].ID); err != nil {
			t.Fatalf("Archive: %v", err)
		}

		if got[0].Path != before {
			t.Errorf("captured nib Path mutated after Archive: got %q, want frozen %q (resolver leaked the live store pointer)", got[0].Path, before)
		}
	})
}

// TestSnapshotHelpersWrapErrNotFound pins that the SINGULAR snapshotResult wraps
// nib.ErrNotFound when GetSnapshot misses (the nib was deleted in the window
// between the write committing and the re-lookup). Over `nibs serve`,
// etagErrorPresenter (cmd/serve.go) tags the error NOT_FOUND only when
// errors.Is(err, nib.ErrNotFound) holds; a bare error would surface untagged and
// the web client would show a raw toast instead of its gone/deleted notice.
//
// The PLURAL snapshotResults deliberately does NOT error on a miss: a bulk
// reorder just wrote every listed nib, so a miss means a concurrent delete
// landed after the order-key write committed, and the persisted order among the
// surviving siblings is still valid. It returns the survivors (see
// TestSnapshotResultsSkipsVanishedElements), so here — where the only listed nib
// is absent — it yields an empty, error-free result rather than a wrapped
// ErrNotFound.
//
// The helpers are driven directly against a stubReader with an empty store, so
// GetSnapshot always returns !ok — a deterministic stand-in for the
// concurrent-delete race without depending on the scheduler.
func TestSnapshotHelpersWrapErrNotFound(t *testing.T) {
	r := &Resolver{Reader: &stubReader{nibs: map[string]*nib.Nib{}}}

	t.Run("snapshotResult", func(t *testing.T) {
		got, err := r.snapshotResult("missing")
		if got != nil {
			t.Errorf("expected nil nib, got %v", got)
		}
		if err == nil {
			t.Fatal("expected error on GetSnapshot miss, got nil")
		}
		if !errors.Is(err, nib.ErrNotFound) {
			t.Errorf("error does not wrap nib.ErrNotFound: %v", err)
		}
	})

	t.Run("snapshotResults returns survivors, not an error", func(t *testing.T) {
		got, err := r.snapshotResults([]*nib.Nib{{ID: "missing"}})
		if err != nil {
			t.Fatalf("expected no error on GetSnapshot miss (survivors-only contract), got %v", err)
		}
		if len(got) != 0 {
			t.Errorf("expected empty survivor slice for an all-absent input, got %v", got)
		}
	})
}

// TestSnapshotResultsSkipsVanishedElements pins the survivors-only contract of
// snapshotResults (the bulk-reorder facet): every element present in the store
// is returned as a detached snapshot in the requested order, and an element
// absent from GetSnapshot — a sibling deleted in the lock-free window between the
// reorder's order-key write committing and this post-write snapshot — is skipped
// rather than failing the whole already-persisted batch. The order among the
// survivors is preserved.
//
// This is the strong deterministic guard: it drives snapshotResults directly
// against a stubReader whose store holds only the "surviving" ids, so a listed
// id absent from the map is an exact stand-in for the concurrent-delete race
// without depending on the scheduler.
func TestSnapshotResultsSkipsVanishedElements(t *testing.T) {
	// present is the set of ids the store still holds. snapshotResults returns a
	// GetSnapshot clone for each of these and skips the rest.
	present := map[string]*nib.Nib{
		"a": {ID: "a", Title: "A"},
		"b": {ID: "b", Title: "B"},
		"c": {ID: "c", Title: "C"},
		"d": {ID: "d", Title: "D"},
	}

	tests := []struct {
		name  string
		input []string // ordered ids handed to snapshotResults
		want  []string // ordered surviving ids
	}{
		{"all present, order preserved", []string{"a", "b", "c", "d"}, []string{"a", "b", "c", "d"}},
		{"middle vanished", []string{"a", "gone", "c"}, []string{"a", "c"}},
		{"first vanished", []string{"gone", "b", "c"}, []string{"b", "c"}},
		{"last vanished", []string{"a", "b", "gone"}, []string{"a", "b"}},
		{"multiple vanished", []string{"a", "x", "c", "y"}, []string{"a", "c"}},
		{"all vanished", []string{"x", "y"}, []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Resolver{Reader: &stubReader{nibs: present}}

			in := make([]*nib.Nib, len(tt.input))
			for i, id := range tt.input {
				in[i] = &nib.Nib{ID: id}
			}

			got, err := r.snapshotResults(in)
			if err != nil {
				t.Fatalf("snapshotResults returned error, want survivors: %v", err)
			}

			gotIDs := make([]string, len(got))
			for i, b := range got {
				gotIDs[i] = b.ID
			}
			if !slices.Equal(gotIDs, tt.want) {
				t.Errorf("survivor ids = %v, want %v", gotIDs, tt.want)
			}
		})
	}
}

// vanishingSnapshotReader wraps a NibReader and reports one id as vanished from
// GetSnapshot ONLY, leaving every other accessor (Get, GetForUpdate, and the ids
// the Orderer's Members sees) intact. It is a deterministic stand-in for a
// nib deleted in the lock-free window between a bulk reorder's order-key write
// committing and its post-write snapshot: the reorder validates and persists the
// full block, then snapshotResults snapshots into the gap the delete left.
type vanishingSnapshotReader struct {
	NibReader
	vanishID string
}

func (v *vanishingSnapshotReader) GetSnapshot(id string) (*nib.Nib, bool) {
	if id == v.vanishID {
		return nil, false
	}
	return v.NibReader.GetSnapshot(id)
}

// TestReorderChildrenSurvivesConcurrentDelete drives the real ReorderChildren
// resolver end-to-end (validate -> assign order keys -> persist -> snapshot) with
// a middle child that vanishes from GetSnapshot exactly as the post-write
// snapshot runs — the concurrent-delete race described in nibs-3lul. Wrapping
// only the resolver's Reader (the Orderer still resolves siblings against the
// unwrapped core) keeps this deterministic without a scheduler hook: the reorder
// still validates and writes all three children, then snapshots into the gap.
//
// The resolver must return the surviving children in order with NO error — the
// persisted order is honest. On the pre-fix code snapshotResults erred on the
// miss, surfacing the whole already-persisted batch to the client as a total
// failure, so this test fails there.
func TestReorderChildrenSurvivesConcurrentDelete(t *testing.T) {
	resolver, core := setupTestResolver(t)

	parent := createTestNib(t, core, "parent1", "Parent", "todo")
	childIDs := []string{"child1", "child2", "child3"}
	for _, id := range childIDs {
		mustCreate(t, core, &nib.Nib{ID: id, Slug: id, Title: "Child", Status: "todo", Parent: parent.ID})
	}

	// child2 vanishes from GetSnapshot only — as if a concurrent delete landed
	// after its order-key write committed.
	resolver.Reader = &vanishingSnapshotReader{NibReader: core, vanishID: "child2"}
	mr := &mutationResolver{resolver}

	got, err := mr.ReorderChildren(context.Background(), parent.ID, childIDs, nil)
	if err != nil {
		t.Fatalf("ReorderChildren surfaced a total-failure error for one concurrently-deleted child: %v", err)
	}

	gotIDs := make([]string, len(got))
	for i, b := range got {
		gotIDs[i] = b.ID
	}
	if want := []string{"child1", "child3"}; !slices.Equal(gotIDs, want) {
		t.Errorf("survivors = %v, want %v (order preserved, deleted child skipped)", gotIDs, want)
	}
}

// TestMutationResolverRaceUnderExecutor drives the removeBlocking mutation
// through the real gqlgen executor concurrently with Archive/Unarchive on the nib
// it returns (the source). The mutation's returned `path` field is marshaled
// asynchronously off the value the resolver returns, while Archive/Unarchive
// rewrite the stored nib's Path in place under c.mu.
//
// removeBlocking against a NON-EXISTENT target is chosen deliberately as the
// demonstrator, to isolate exactly the return-value race this fix closes:
//   - It sources its returned nib from Reader.Get (the shared pointer, WITHOUT an
//     off-lock clone) rather than Reader.GetForUpdate, so the source's Path is
//     never cloned off-lock during the mutation. The GetForUpdate-based mutations
//     (update, setParent, reorder, ...) sourced their working nib from
//     GetForUpdate, whose own input-side clone once raced Archive's in-place Path
//     write independently of the returned value — a distinct nibcore concern,
//     since closed by cloning under c.mu — so they could not cleanly isolate the
//     return-value race this test guards.
//   - A non-existent target makes the resolver skip its target-side write entirely
//     (the existence guard is false), so there is no optimistic-concurrency etag
//     collision under the 8-way concurrency below, and the ONLY off-lock read of
//     the source's Path is the value handed to gqlgen. It is a documented no-op
//     that still returns and marshals the source nib.
//
// Because the resolver now snapshots via Reader.GetSnapshot (clone-under-lock),
// the marshaled value is a detached copy and this is `-race` clean. It fails
// under `-race` if the mutation resolver ever reverts to handing out the live
// c.nibs pointer, so it is a real detector guard, not skipped.
func TestMutationResolverRaceUnderExecutor(t *testing.T) {
	resolver, core := setupTestResolver(t)

	createTestNib(t, core, "source1", "Source", "todo")

	es := NewExecutableSchema(Config{Resolvers: resolver})
	exec := executor.New(es)

	runMutation := func() {
		ctx := graphql.StartOperationTrace(context.Background())
		params := &graphql.RawParams{
			Query:     `mutation { removeBlocking(id: "source1", targetId: "nonexistent") { path } }`,
			Variables: map[string]any{},
		}
		opCtx, errs := exec.CreateOperationContext(ctx, params)
		if len(errs) > 0 {
			t.Errorf("CreateOperationContext: %v", errs)
			return
		}
		ctx = graphql.WithOperationContext(ctx, opCtx)
		handler, ctx := exec.DispatchOperation(ctx, opCtx)
		// Assert the mutation actually succeeds and marshals `path`. Without this,
		// a future regression that made the resolver error out (no `path` marshal)
		// would never open the race window, leaving the -race guard silently green.
		resp := handler(ctx)
		if len(resp.Errors) > 0 {
			t.Errorf("mutation returned errors (race window never opened): %v", resp.Errors)
		}
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); runMutation() }()
		go func() {
			defer wg.Done()
			_ = core.Archive("source1")
			_ = core.Unarchive("source1")
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

// TestNibsFilterRaceAgainstRemoveLinksTo reproduces the write-side data race in
// nibs-pyei. The Nibs query pipeline reads b.Parent off the LIVE c.nibs pointers
// off-lock — ApplyFilter's HasParent predicate, which reaches the field through
// the opening emptiness test of resolvedParent (internal/graph/filters.go), the
// function resolvedParentID delegates to — while Core.RemoveLinksTo mutates
// b.Parent under c.mu (internal/nibcore/link_health.go).
// That unsynchronized read of b.Parent is what makes this a detector, so a
// change removing it from the predicate's path retires the probe with it.
// On the old in-place code those two accesses touch the same b.Parent word with
// no synchronization between them, so `-race` fires (read at filters.go vs write
// at link_health.go). With RemoveLinksTo copy-on-write — it installs a fresh
// cloned pointer instead of mutating the stored one — the off-lock reader only
// ever holds the frozen old pointer, so this is `-race` clean. It stays a live
// detector: revert RemoveLinksTo to in-place mutation and `-race` fires again.
//
// The writer restores each child's parent between removals (via Update, which
// already installs a fresh pointer) so the mutating access keeps firing across
// the whole run instead of a single one-shot burst that the reader might miss.
func TestNibsFilterRaceAgainstRemoveLinksTo(t *testing.T) {
	resolver, core := setupTestResolver(t)

	parent := createTestNib(t, core, "parent1", "Parent", "todo")
	var childIDs []string
	for i := 0; i < 30; i++ {
		id := fmt.Sprintf("child%d", i)
		mustCreate(t, core, &nib.Nib{ID: id, Slug: id, Title: "Child", Status: "todo", Parent: parent.ID})
		childIDs = append(childIDs, id)
	}

	hasParent := false
	filter := &model.NibFilter{HasParent: &hasParent}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				// Reads b.Parent off the live store pointers, off-lock.
				//
				// The error return is discarded rather than asserted: the filter
				// carries no *ID field, so it is structurally nil on every
				// iteration, and an assertion that cannot fire would only slow
				// the loop the detector depends on running hot.
				_, _ = ApplyFilter(context.Background(), core.All(), filter, resolver.Reader, resolver.Blocking)
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 50; j++ {
			if _, err := core.RemoveLinksTo(parent.ID); err != nil {
				t.Errorf("RemoveLinksTo: %v", err)
				return
			}
			for _, id := range childIDs {
				b, err := core.GetForUpdate(id)
				if err != nil {
					t.Errorf("GetForUpdate restore: %v", err)
					return
				}
				b.Parent = parent.ID
				if err := core.Update(b, nil); err != nil {
					t.Errorf("Update restore: %v", err)
					return
				}
			}
		}
	}()
	wg.Wait()
}

// TestRemoveLinksToFreezesReadPointers is the deterministic companion to the
// -race demonstrator above: it pins the copy-on-write contract without depending
// on the scheduler. It captures the LIVE stored pointer of a linked nib (from
// All, the same pointers the Nibs pipeline reads off-lock), runs RemoveLinksTo
// on its parent and its blocker, then asserts the captured pointer's Parent and
// BlockedBy are FROZEN. On the old in-place code RemoveLinksTo mutated that very
// pointer (Parent -> "", BlockedBy -> a shorter slice) and these assertions
// fail; with copy-on-write the mutation lands on a fresh clone reinstalled in the
// store and the captured pointer is untouched.
func TestRemoveLinksToFreezesReadPointers(t *testing.T) {
	_, core := setupTestResolver(t)

	parent := createTestNib(t, core, "parent1", "Parent", "todo")
	blocker := createTestNib(t, core, "blocker1", "Blocker", "todo")
	child := &nib.Nib{ID: "child1", Slug: "child", Title: "Child", Status: "todo", Parent: parent.ID, BlockedBy: []string{blocker.ID}}
	mustCreate(t, core, child)

	all := core.All()
	idx := nibIndexByID(all, child.ID)
	if idx < 0 {
		t.Fatalf("All did not return %q", child.ID)
	}
	captured := all[idx]
	beforeParent := captured.Parent
	beforeBlockedBy := append([]string(nil), captured.BlockedBy...)

	if _, err := core.RemoveLinksTo(parent.ID); err != nil {
		t.Fatalf("RemoveLinksTo(parent): %v", err)
	}
	if _, err := core.RemoveLinksTo(blocker.ID); err != nil {
		t.Fatalf("RemoveLinksTo(blocker): %v", err)
	}

	if captured.Parent != beforeParent {
		t.Errorf("captured Parent mutated in place: got %q, want frozen %q (RemoveLinksTo must copy-on-write)", captured.Parent, beforeParent)
	}
	if !slices.Equal(captured.BlockedBy, beforeBlockedBy) {
		t.Errorf("captured BlockedBy mutated in place: got %v, want frozen %v (RemoveLinksTo must copy-on-write)", captured.BlockedBy, beforeBlockedBy)
	}

	// The store itself must still reflect the removals, on fresh pointers.
	reloaded, err := core.Get(child.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if reloaded.Parent != "" || len(reloaded.BlockedBy) != 0 {
		t.Errorf("stored nib not updated: parent=%q blocked_by=%v", reloaded.Parent, reloaded.BlockedBy)
	}
}

// TestFixBrokenLinksFreezesReadPointers is the FixBrokenLinks analogue of the
// guard above. The nib has a broken parent, a mix of valid/broken blocked_by,
// and a broken document link. Capturing its live pointer, running FixBrokenLinks,
// then asserting Parent/BlockedBy/Documents on the captured pointer are frozen
// proves FixBrokenLinks copies-on-write rather than mutating the stored pointer
// the Nibs pipeline reads off-lock. Fails on the old in-place code, passes after.
func TestFixBrokenLinksFreezesReadPointers(t *testing.T) {
	_, core := setupTestResolver(t)

	valid := createTestNib(t, core, "valid1", "Valid", "todo")
	broken := &nib.Nib{
		ID: "broken1", Slug: "broken", Title: "Broken", Status: "todo",
		Parent:    "ghost",                       // broken parent (no such nib)
		BlockedBy: []string{valid.ID, "ghost2"},  // one valid, one broken
		Documents: []string{"does/not/exist.md"}, // broken document link
	}
	mustCreate(t, core, broken)

	all := core.All()
	idx := nibIndexByID(all, broken.ID)
	if idx < 0 {
		t.Fatalf("All did not return %q", broken.ID)
	}
	captured := all[idx]
	beforeParent := captured.Parent
	beforeBlockedBy := append([]string(nil), captured.BlockedBy...)
	beforeDocs := append([]string(nil), captured.Documents...)

	if _, err := core.FixBrokenLinks(); err != nil {
		t.Fatalf("FixBrokenLinks: %v", err)
	}

	if captured.Parent != beforeParent {
		t.Errorf("captured Parent mutated in place: got %q, want frozen %q (FixBrokenLinks must copy-on-write)", captured.Parent, beforeParent)
	}
	if !slices.Equal(captured.BlockedBy, beforeBlockedBy) {
		t.Errorf("captured BlockedBy mutated in place: got %v, want frozen %v (FixBrokenLinks must copy-on-write)", captured.BlockedBy, beforeBlockedBy)
	}
	if !slices.Equal(captured.Documents, beforeDocs) {
		t.Errorf("captured Documents mutated in place: got %v, want frozen %v (FixBrokenLinks must copy-on-write)", captured.Documents, beforeDocs)
	}

	// The store itself must still reflect the fixes, on a fresh pointer.
	reloaded, err := core.Get(broken.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if reloaded.Parent != "" {
		t.Errorf("broken parent not fixed: %q", reloaded.Parent)
	}
	if len(reloaded.BlockedBy) != 1 || reloaded.BlockedBy[0] != valid.ID {
		t.Errorf("blocked_by not fixed: got %v, want [%s]", reloaded.BlockedBy, valid.ID)
	}
	if len(reloaded.Documents) != 0 {
		t.Errorf("broken document not fixed: %v", reloaded.Documents)
	}
}
