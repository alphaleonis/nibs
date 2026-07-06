package graph

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
)

// divergeNibOnDisk rewrites nib id's on-disk file with genuinely different but
// still-parseable content (new title + body; every other field preserved) so its
// canonical stored etag no longer matches the in-memory nib's etag. Any
// subsequent if-match Update on that nib then fails with ETagMismatchError,
// exercising the failed-write path. It clones the shared in-memory nib before
// mutating, so the in-memory store itself is left untouched.
func divergeNibOnDisk(t *testing.T, core *nibcore.Core, id string) {
	t.Helper()
	shared, err := core.Get(id)
	if err != nil {
		t.Fatalf("diverge: get %s: %v", id, err)
	}
	diverged := shared.Clone()
	diverged.Title = "DIVERGED-" + id
	diverged.Body = "Divergent on-disk content for " + id + ".\n"
	content, err := diverged.Render()
	if err != nil {
		t.Fatalf("diverge: render %s: %v", id, err)
	}
	path := filepath.Join(core.Root(), shared.Path)
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("diverge: write %s: %v", path, err)
	}
}

// inMemETag returns a pointer to the in-memory etag of nib id, for use as an
// if-match value. Combined with divergeNibOnDisk it forces an ETagMismatchError:
// the caller passes the pre-divergence etag while the on-disk content no longer
// matches it.
func inMemETag(t *testing.T, core *nibcore.Core, id string) *string {
	t.Helper()
	b, err := core.Get(id)
	if err != nil {
		t.Fatalf("etag: get %s: %v", id, err)
	}
	e := b.ETag()
	return &e
}

// TestFailedUpdateLeavesSharedNibUntouched is the clone-before-mutate regression
// guard for nibs-twvo (same corruption class as nibs-e9oz). Reader.Get returns
// the SHARED c.nibs[id] pointer; a resolver that mutates it before Writer.Update
// leaves the in-memory store falsely showing the attempted change when the write
// is rejected (genuine on-disk etag divergence / a concurrent write to the target
// between its Get and its Update).
//
// Each case forces the write to fail by genuinely diverging the mutated nib's
// on-disk file, then asserts the SHARED in-memory nib (fetched fresh via
// Reader.Get) still shows its PRE-mutation state — i.e. the failed write left no
// phantom mutation. Every case is red against the pre-fix mutate-shared-pointer
// code and green once each site mutates a clone instead.
func TestFailedUpdateLeavesSharedNibUntouched(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name string
		run  func(t *testing.T, r *Resolver, core *nibcore.Core)
	}{
		{
			// SetParent (schema.resolvers.go): mutates the subject `b` (Reader.Get
			// pointer) via validateAndSetParent, then Update(b, ifMatch).
			name: "SetParent",
			run: func(t *testing.T, r *Resolver, core *nibcore.Core) {
				parent := createTestNib(t, core, "epic-sp", "Parent", "todo")
				parent.Type = "epic"
				if err := core.Update(parent, nil); err != nil {
					t.Fatalf("setup parent type: %v", err)
				}
				createTestNib(t, core, "task-sp", "Child", "todo") // no parent

				ifMatch := inMemETag(t, core, "task-sp")
				divergeNibOnDisk(t, core, "task-sp")

				pid := "epic-sp"
				if _, err := r.Mutation().SetParent(ctx, "task-sp", &pid, ifMatch); err == nil {
					t.Fatal("expected SetParent to fail on etag mismatch, got nil error")
				}

				got, err := r.Reader.Get("task-sp")
				if err != nil {
					t.Fatalf("get task-sp: %v", err)
				}
				if got.Parent != "" {
					t.Errorf("shared nib mutated after failed SetParent: Parent=%q, want empty", got.Parent)
				}
			},
		},
		{
			// AddBlockedBy (schema.resolvers.go): mutates the subject `b` via
			// b.AddBlockedBy, then Update(b, ifMatch).
			name: "AddBlockedBy",
			run: func(t *testing.T, r *Resolver, core *nibcore.Core) {
				createTestNib(t, core, "sub-abb", "Subject", "todo")
				createTestNib(t, core, "blk-abb", "Blocker", "todo")

				ifMatch := inMemETag(t, core, "sub-abb")
				divergeNibOnDisk(t, core, "sub-abb")

				if _, err := r.Mutation().AddBlockedBy(ctx, "sub-abb", "blk-abb", ifMatch); err == nil {
					t.Fatal("expected AddBlockedBy to fail on etag mismatch, got nil error")
				}

				got, err := r.Reader.Get("sub-abb")
				if err != nil {
					t.Fatalf("get sub-abb: %v", err)
				}
				if len(got.BlockedBy) != 0 {
					t.Errorf("shared subject mutated after failed AddBlockedBy: BlockedBy=%v, want empty", got.BlockedBy)
				}
			},
		},
		{
			// RemoveBlockedBy (schema.resolvers.go): mutates the subject `b` via
			// b.RemoveBlockedBy, then Update(b, ifMatch).
			name: "RemoveBlockedBy",
			run: func(t *testing.T, r *Resolver, core *nibcore.Core) {
				sub := createTestNib(t, core, "sub-rbb", "Subject", "todo")
				createTestNib(t, core, "blk-rbb", "Blocker", "todo")
				sub.AddBlockedBy("blk-rbb")
				if err := core.Update(sub, nil); err != nil {
					t.Fatalf("setup blockedBy: %v", err)
				}

				ifMatch := inMemETag(t, core, "sub-rbb")
				divergeNibOnDisk(t, core, "sub-rbb")

				if _, err := r.Mutation().RemoveBlockedBy(ctx, "sub-rbb", "blk-rbb", ifMatch); err == nil {
					t.Fatal("expected RemoveBlockedBy to fail on etag mismatch, got nil error")
				}

				got, err := r.Reader.Get("sub-rbb")
				if err != nil {
					t.Fatalf("get sub-rbb: %v", err)
				}
				if !got.IsBlockedBy("blk-rbb") {
					t.Errorf("shared subject mutated after failed RemoveBlockedBy: BlockedBy=%v, want [blk-rbb]", got.BlockedBy)
				}
			},
		},
		{
			// ReorderNib (schema.resolvers.go): mutates the subject `b` order key
			// via positionAfter, then Update(b, ifMatch).
			name: "ReorderNib",
			run: func(t *testing.T, r *Resolver, core *nibcore.Core) {
				a := createTestNib(t, core, "task-ra", "A", "todo")
				a.Order = "m"
				if err := core.Update(a, nil); err != nil {
					t.Fatalf("setup order a: %v", err)
				}
				b := createTestNib(t, core, "task-rb", "B", "todo")
				b.Order = "s"
				if err := core.Update(b, nil); err != nil {
					t.Fatalf("setup order b: %v", err)
				}
				beforeOrder := a.Order

				ifMatch := inMemETag(t, core, "task-ra")
				divergeNibOnDisk(t, core, "task-ra")

				afterID := "task-rb"
				if _, err := r.Mutation().ReorderNib(ctx, "task-ra", &afterID, nil, nil, nil, ifMatch); err == nil {
					t.Fatal("expected ReorderNib to fail on etag mismatch, got nil error")
				}

				got, err := r.Reader.Get("task-ra")
				if err != nil {
					t.Fatalf("get task-ra: %v", err)
				}
				if got.Order != beforeOrder {
					t.Errorf("shared nib mutated after failed ReorderNib: Order=%q, want %q", got.Order, beforeOrder)
				}
			},
		},
		{
			// AddBlocking (schema.resolvers.go): mutates the TARGET (a separate
			// Reader.Get pointer) via target.AddBlockedBy, then Update(target).
			name: "AddBlocking",
			run: func(t *testing.T, r *Resolver, core *nibcore.Core) {
				createTestNib(t, core, "src-ab", "Source", "todo")
				createTestNib(t, core, "tgt-ab", "Target", "todo")

				divergeNibOnDisk(t, core, "tgt-ab")

				if _, err := r.Mutation().AddBlocking(ctx, "src-ab", "tgt-ab"); err == nil {
					t.Fatal("expected AddBlocking to fail on etag mismatch, got nil error")
				}

				got, err := r.Reader.Get("tgt-ab")
				if err != nil {
					t.Fatalf("get tgt-ab: %v", err)
				}
				if len(got.BlockedBy) != 0 {
					t.Errorf("shared target mutated after failed AddBlocking: BlockedBy=%v, want empty", got.BlockedBy)
				}
			},
		},
		{
			// RemoveBlocking (schema.resolvers.go): mutates the TARGET via
			// target.RemoveBlockedBy, then Update(target).
			name: "RemoveBlocking",
			run: func(t *testing.T, r *Resolver, core *nibcore.Core) {
				createTestNib(t, core, "src-rb", "Source", "todo")
				tgt := createTestNib(t, core, "tgt-rb", "Target", "todo")
				tgt.AddBlockedBy("src-rb")
				if err := core.Update(tgt, nil); err != nil {
					t.Fatalf("setup blockedBy: %v", err)
				}

				divergeNibOnDisk(t, core, "tgt-rb")

				if _, err := r.Mutation().RemoveBlocking(ctx, "src-rb", "tgt-rb"); err == nil {
					t.Fatal("expected RemoveBlocking to fail on etag mismatch, got nil error")
				}

				got, err := r.Reader.Get("tgt-rb")
				if err != nil {
					t.Fatalf("get tgt-rb: %v", err)
				}
				if !got.IsBlockedBy("src-rb") {
					t.Errorf("shared target mutated after failed RemoveBlocking: BlockedBy=%v, want [src-rb]", got.BlockedBy)
				}
			},
		},
		{
			// validateAndAddBlocking (resolver.go, via UpdateNib addBlocking):
			// mutates the TARGET via vt.target.AddBlockedBy, then Update(target).
			name: "UpdateNib_addBlocking",
			run: func(t *testing.T, r *Resolver, core *nibcore.Core) {
				createTestNib(t, core, "src-vab", "Source", "todo")
				createTestNib(t, core, "tgt-vab", "Target", "todo")

				divergeNibOnDisk(t, core, "tgt-vab")

				input := model.UpdateNibInput{AddBlocking: []string{"tgt-vab"}}
				if _, err := r.Mutation().UpdateNib(ctx, "src-vab", input); err == nil {
					t.Fatal("expected UpdateNib(addBlocking) to fail on etag mismatch, got nil error")
				}

				got, err := r.Reader.Get("tgt-vab")
				if err != nil {
					t.Fatalf("get tgt-vab: %v", err)
				}
				if len(got.BlockedBy) != 0 {
					t.Errorf("shared target mutated after failed addBlocking: BlockedBy=%v, want empty", got.BlockedBy)
				}
			},
		},
		{
			// removeBlockingRelationships (resolver.go, via UpdateNib
			// removeBlocking): mutates the TARGET via target.RemoveBlockedBy, then
			// Update(target).
			name: "UpdateNib_removeBlocking",
			run: func(t *testing.T, r *Resolver, core *nibcore.Core) {
				createTestNib(t, core, "src-rbr", "Source", "todo")
				tgt := createTestNib(t, core, "tgt-rbr", "Target", "todo")
				tgt.AddBlockedBy("src-rbr")
				if err := core.Update(tgt, nil); err != nil {
					t.Fatalf("setup blockedBy: %v", err)
				}

				divergeNibOnDisk(t, core, "tgt-rbr")

				input := model.UpdateNibInput{RemoveBlocking: []string{"tgt-rbr"}}
				if _, err := r.Mutation().UpdateNib(ctx, "src-rbr", input); err == nil {
					t.Fatal("expected UpdateNib(removeBlocking) to fail on etag mismatch, got nil error")
				}

				got, err := r.Reader.Get("tgt-rbr")
				if err != nil {
					t.Fatalf("get tgt-rbr: %v", err)
				}
				if !got.IsBlockedBy("src-rbr") {
					t.Errorf("shared target mutated after failed removeBlocking: BlockedBy=%v, want [src-rbr]", got.BlockedBy)
				}
			},
		},
		{
			// CreateNib (schema.resolvers.go) blocking loop: mutates each blocking
			// TARGET via target.AddBlockedBy, then Update(target). Same class as the
			// eight enumerated sites, fixed via the same helper.
			name: "CreateNib_blocking",
			run: func(t *testing.T, r *Resolver, core *nibcore.Core) {
				createTestNib(t, core, "tgt-cn", "Target", "todo")

				divergeNibOnDisk(t, core, "tgt-cn")

				input := model.CreateNibInput{Title: "New Blocker", Blocking: []string{"tgt-cn"}}
				if _, err := r.Mutation().CreateNib(ctx, input); err == nil {
					t.Fatal("expected CreateNib(blocking) to fail on etag mismatch, got nil error")
				}

				got, err := r.Reader.Get("tgt-cn")
				if err != nil {
					t.Fatalf("get tgt-cn: %v", err)
				}
				if len(got.BlockedBy) != 0 {
					t.Errorf("shared target mutated after failed CreateNib blocking: BlockedBy=%v, want empty", got.BlockedBy)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, core := setupTestResolver(t)
			tc.run(t, r, core)
		})
	}
}

// TestUpdateNibDuplicateBlockingTargetIsIdempotent is the regression guard for
// nibs-twvo (the Phase-2 stale-pointer bug the sweep introduced).
// validateAndAddBlocking cached each target's *nib.Nib in Phase 1 and reused it
// in Phase 2; because a successful Writer.Update installs the CLONE as the new
// c.nibs[id], the cached pointer is orphaned after the first write. With a
// DUPLICATE target ID (--blocking is a repeatable flag with no dedup), iteration
// 2 held the stale pre-mutation pointer, computed a stale if-match, and the write
// was refused with a spurious ETagMismatchError — aborting UpdateNib AFTER target
// 1 was persisted and silently dropping any bundled subject field change (e.g. a
// Title edit): a partial write plus a lost update.
//
// The fix re-fetches each target FRESH in Phase 2. This test bundles a duplicate
// blocking target with a Title change and asserts the whole update succeeds, the
// Title change IS applied (subject persisted), and the target is blocked by the
// source exactly once. RED against the stale-pointer code, GREEN after the
// re-fetch fix.
func TestUpdateNibDuplicateBlockingTargetIsIdempotent(t *testing.T) {
	ctx := context.Background()
	r, core := setupTestResolver(t)

	createTestNib(t, core, "src-dup", "Source", "todo")
	createTestNib(t, core, "tgt-dup", "Target", "todo")

	newTitle := "Renamed Source"
	input := model.UpdateNibInput{
		Title:       &newTitle,
		AddBlocking: []string{"tgt-dup", "tgt-dup"}, // duplicate target, as an un-deduped CLI --blocking Y --blocking Y
	}
	updated, err := r.Mutation().UpdateNib(ctx, "src-dup", input)
	if err != nil {
		t.Fatalf("UpdateNib with duplicate blocking target failed: %v", err)
	}

	// The bundled subject Title change must have been applied, not dropped.
	if updated.Title != newTitle {
		t.Errorf("returned subject Title not applied: got %q, want %q", updated.Title, newTitle)
	}
	gotSrc, err := r.Reader.Get("src-dup")
	if err != nil {
		t.Fatalf("get src-dup: %v", err)
	}
	if gotSrc.Title != newTitle {
		t.Errorf("persisted subject Title not applied: got %q, want %q", gotSrc.Title, newTitle)
	}

	// The target must be blocked by the source exactly once.
	gotTgt, err := r.Reader.Get("tgt-dup")
	if err != nil {
		t.Fatalf("get tgt-dup: %v", err)
	}
	count := 0
	for _, id := range gotTgt.BlockedBy {
		if id == "src-dup" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("target blockedBy = %v, want exactly one 'src-dup'", gotTgt.BlockedBy)
	}
}

// TestBackfillOrderKeysRefusedWriteLeavesNoPhantomOrder guards the nibs-twvo
// corruption class in a sibling site the sweep extends to.
// Orderer.backfillOrderKeys iterates the SHARED c.nibs[id] pointers returned by
// reader.All()/FindIncomingLinks and assigned b.Order = newKey in place before a
// best-effort Writer.Update. A refused write (genuine on-disk divergence)
// therefore left the shared in-memory sibling showing a phantom Order that was
// never persisted. The fix mutates a CLONE so the shared pointer is untouched.
//
// backfillOrderKeys also runs on the hot Children/root READ path, and a
// persistently etag-diverged sibling keeps Order=="" so the backfill Update is
// re-attempted on EVERY read. An unconditional stderr warning there floods the
// log under a long-running `nibs serve`, so a stable ETagMismatchError (a benign,
// already-accepted best-effort fallback) must NOT warn.
//
// One unordered root nib is created and its on-disk file diverged so the backfill
// Update is rejected (etag mismatch); backfill is triggered via getRootSiblings,
// called REPEATEDLY to model the read-path re-attempt. The shared in-memory nib
// must still show no order key, and NO warning may be emitted across the repeated
// reads. RED against the mutate-shared-pointer code (phantom order) and against
// the unconditional-warning code (stderr flood); GREEN after both fixes.
func TestBackfillOrderKeysRefusedWriteLeavesNoPhantomOrder(t *testing.T) {
	r, core := setupTestResolver(t)

	// A root nib with no order key → getRootSiblings will try to backfill it.
	createTestNib(t, core, "root-bf", "Root", "todo")
	if b, err := core.Get("root-bf"); err != nil {
		t.Fatalf("get root-bf: %v", err)
	} else if b.Order != "" {
		t.Fatalf("precondition: expected empty order, got %q", b.Order)
	}

	// Diverge its on-disk file so the backfill Update is rejected on etag mismatch.
	divergeNibOnDisk(t, core, "root-bf")

	stderr := captureStderr(t, func() {
		// Model repeated tree renders/polls: each read re-attempts the backfill
		// because the diverged sibling never gets an order key persisted.
		for i := 0; i < 3; i++ {
			_ = r.Orderer.getRootSiblings()
		}
	})

	// The shared in-memory nib must not show a phantom order after the refused write.
	got, err := r.Reader.Get("root-bf")
	if err != nil {
		t.Fatalf("get root-bf: %v", err)
	}
	if got.Order != "" {
		t.Errorf("shared nib left with phantom Order %q after refused backfill write; want empty", got.Order)
	}

	// A benign, persistent etag divergence must NOT warn — otherwise it floods
	// stderr on every read of the affected parent.
	if strings.TrimSpace(stderr) != "" {
		t.Errorf("expected NO stderr warning for a benign etag divergence on the hot read path; got %q", stderr)
	}
}

// errWriter is a NibWriter whose Update always fails with a fixed error, used to
// exercise backfillOrderKeys' unexpected-error branch (a non-etag, non-parse
// write failure, e.g. disk I/O) which must still emit exactly one warning.
type errWriter struct {
	stubWriter
	updateErr error
}

func (w *errWriter) Update(b *nib.Nib, ifMatch *string) error {
	return w.updateErr
}

// TestBackfillOrderKeysUnexpectedWriteErrorWarns is the companion to the
// no-flood guard above: a genuinely UNEXPECTED backfill write failure (not an
// etag mismatch or an unparseable/unreadable file) must still surface a stderr
// warning so a real problem stays diagnosable. Uses a writer that returns a
// plain error to drive backfillOrderKeys' non-etag branch.
func TestBackfillOrderKeysUnexpectedWriteErrorWarns(t *testing.T) {
	unordered := &nib.Nib{ID: "root-err", Title: "Root", Status: "todo"} // Order == ""
	reader := &stubReader{
		nibs:    map[string]*nib.Nib{"root-err": unordered},
		allNibs: []*nib.Nib{unordered},
	}
	writer := &errWriter{updateErr: errors.New("simulated disk I/O failure")}
	o := NewOrderer(reader, writer)

	stderr := captureStderr(t, func() {
		_ = o.getRootSiblings()
	})

	if !strings.Contains(stderr, "root-err") {
		t.Errorf("expected a warning mentioning root-err on stderr for an unexpected write error; got %q", stderr)
	}
	if !strings.Contains(stderr, "simulated disk I/O failure") {
		t.Errorf("expected the underlying error to be surfaced on stderr; got %q", stderr)
	}
	// Exactly one warning line for the single failing sibling.
	if n := strings.Count(strings.TrimSpace(stderr), "\n"); n != 0 {
		t.Errorf("expected exactly one warning line, got %d extra newlines in %q", n, stderr)
	}
}

// corruptNibOnDisk overwrites nib id's on-disk file with content nib.Parse
// cannot decode (git conflict markers in the YAML front matter), leaving the
// in-memory nib untouched. A subsequent computeStoredETag on that nib then fails
// CLOSED with *OnDiskUnparseableError, exercising the uncertifiable-file path.
func corruptNibOnDisk(t *testing.T, core *nibcore.Core, id string) {
	t.Helper()
	shared, err := core.Get(id)
	if err != nil {
		t.Fatalf("corrupt: get %s: %v", id, err)
	}
	const corrupt = `---
version: 1
title: Being Edited
status: todo
<<<<<<< HEAD
priority: high
=======
priority: low
>>>>>>> other
---

Body under edit.
`
	path := filepath.Join(core.Root(), shared.Path)
	if err := os.WriteFile(path, []byte(corrupt), 0644); err != nil {
		t.Fatalf("corrupt: write %s: %v", path, err)
	}
}

// TestBackfillOrderKeysCorruptSiblingDoesNotFloodStderr is the residual-flood
// guard for the nibs-twvo review (finding #1). backfillOrderKeys runs on the hot
// Children/root READ path and re-attempts the best-effort Writer.Update on every
// read for a sibling that never gets an order key. When that sibling's on-disk
// file is UNCERTIFIABLE (unparseable/unreadable), computeStoredETag returns
// *OnDiskUnparseableError; the orderer classifies and SUPPRESSES its own warning.
//
// The real flood used to originate one frame deeper: computeStoredETag ALSO
// logWarn'd unconditionally on that branch, so a persistently-corrupt sibling
// still emitted one nibcore warning per read even though the orderer stayed
// silent. The fix makes computeStoredETag RETURN the error rather than log it, so
// the benign read path emits nothing from EITHER layer.
//
// This test captures BOTH warning sinks — the orderer writes to os.Stderr, while
// nibcore writes to its own warnWriter (which setupTestResolver leaves at the
// real os.Stderr captured at New() time, so captureStderr's pipe swap would NOT
// see it; we redirect it explicitly to a buffer). Repeated getRootSiblings reads
// over a store with one corrupt sibling must produce ZERO warning lines across
// both sinks. Detection is preserved: a direct if-match Update of the same corrupt
// file still fails CLOSED with *OnDiskUnparseableError.
//
// RED against the unconditional-nibcore-logWarn code (the warnWriter buffer
// accumulates one line per read); GREEN once computeStoredETag returns instead of
// logging.
func TestBackfillOrderKeysCorruptSiblingDoesNotFloodStderr(t *testing.T) {
	r, core := setupTestResolver(t)

	// Redirect nibcore's own warnings to a buffer. setupTestResolver leaves the
	// warnWriter at os.Stderr (captured at New() time), which captureStderr's
	// package-var swap cannot intercept — so we must capture it directly here.
	var nibcoreWarn bytes.Buffer
	core.SetWarnWriter(&nibcoreWarn)

	// A root nib with no order key → getRootSiblings will try to backfill it.
	createTestNib(t, core, "root-corrupt", "Root", "todo")

	// Corrupt its on-disk file so the backfill Update fails CLOSED with an
	// uncertifiable-file error (OnDiskUnparseableError), the branch that used to
	// double-log.
	corruptNibOnDisk(t, core, "root-corrupt")

	const reads = 4
	ordererStderr := captureStderr(t, func() {
		// Model repeated tree renders/polls: each read re-attempts the backfill
		// because the corrupt sibling never gets an order key persisted.
		for i := 0; i < reads; i++ {
			_ = r.Orderer.getRootSiblings()
		}
	})

	// No orderer warning (it classifies OnDiskUnparseableError and stays quiet).
	if strings.TrimSpace(ordererStderr) != "" {
		t.Errorf("orderer emitted a stderr warning for an uncertifiable sibling on the hot read path; want none, got %q", ordererStderr)
	}
	// No nibcore flood: the fail-closed branch must RETURN the error, not log it,
	// so the buffer stays empty across all reads.
	if got := strings.TrimSpace(nibcoreWarn.String()); got != "" {
		t.Errorf("nibcore flooded its warn writer for a persistently-corrupt sibling across %d reads; want zero lines, got %q", reads, got)
	}

	// Detection is preserved: a direct if-match Update of the corrupt file still
	// fails CLOSED with the non-reconcilable OnDiskUnparseableError.
	shared, err := core.Get("root-corrupt")
	if err != nil {
		t.Fatalf("get root-corrupt: %v", err)
	}
	ifMatch := shared.ETag()
	err = core.Update(shared.Clone(), &ifMatch)
	var unparseable *nibcore.OnDiskUnparseableError
	if !errors.As(err, &unparseable) {
		t.Fatalf("direct if-match Update of the corrupt file: got %T: %v, want *nibcore.OnDiskUnparseableError (detection must be preserved)", err, err)
	}
}

// TestUpdateTargetCloneSkipsWriteWhenMutateReturnsFalse covers updateTargetClone's
// mutate-returns-false skip-the-write branch (the no-op Remove path). Removing a
// relationship that isn't present must attempt NO write at all. That is proven
// here by diverging the target's on-disk file first: were updateTargetClone to
// call Writer.Update, its pre-mutation if-match would fail with an etag mismatch.
// Instead the call must succeed (no error) and leave the shared target untouched.
func TestUpdateTargetCloneSkipsWriteWhenMutateReturnsFalse(t *testing.T) {
	ctx := context.Background()
	r, core := setupTestResolver(t)

	createTestNib(t, core, "src-skip", "Source", "todo")
	createTestNib(t, core, "tgt-skip", "Target", "todo") // NOT blocked by src-skip

	before, err := r.Reader.Get("tgt-skip")
	if err != nil {
		t.Fatalf("get tgt-skip: %v", err)
	}
	beforeETag := before.ETag()

	// Diverge the target's on-disk file so any attempted if-match Update would
	// fail with an etag mismatch — turning a skipped write into an observable
	// success and an attempted write into an error.
	divergeNibOnDisk(t, core, "tgt-skip")

	// RemoveBlocking a relationship that does not exist → RemoveBlockedBy returns
	// false → updateTargetClone skips the write and returns nil.
	if _, err := r.Mutation().RemoveBlocking(ctx, "src-skip", "tgt-skip"); err != nil {
		t.Fatalf("RemoveBlocking of a non-present relationship must skip the write and succeed, got: %v", err)
	}

	// The shared target nib must be untouched (no write happened).
	got, err := r.Reader.Get("tgt-skip")
	if err != nil {
		t.Fatalf("get tgt-skip: %v", err)
	}
	if got.ETag() != beforeETag {
		t.Errorf("shared target etag changed after a no-op RemoveBlocking: got %q, want %q", got.ETag(), beforeETag)
	}
	if len(got.BlockedBy) != 0 {
		t.Errorf("shared target BlockedBy = %v, want empty (no phantom mutation)", got.BlockedBy)
	}
}
