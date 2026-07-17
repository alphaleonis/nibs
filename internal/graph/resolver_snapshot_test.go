package graph

import (
	"context"
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
// off-lock (ApplyFilter's NoParent predicate, internal/graph/filters.go), while
// Core.RemoveLinksTo mutates b.Parent under c.mu (internal/nibcore/link_health.go).
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

	noParent := true
	filter := &model.NibFilter{NoParent: &noParent}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				// Reads b.Parent off the live store pointers, off-lock.
				_ = ApplyFilter(context.Background(), core.All(), filter, resolver.Reader, resolver.Blocking)
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
