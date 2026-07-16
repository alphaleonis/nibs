package graph

import (
	"context"
	"sync"
	"testing"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/executor"
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
		_ = handler(ctx)
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
