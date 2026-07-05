package graph

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
)

// captureStderr redirects os.Stderr for the duration of fn and returns anything
// written to it. activateParentChain warns on stderr (best-effort), so tests use
// this to assert a warning is (or is not) emitted.
//
// This helper mutates process-global os.Stderr and is NOT safe under
// t.Parallel() — a concurrent swap would silently steal another test's stderr.
// No test in this package is parallel, which is what keeps it safe.
//
// The os.Stderr restore and writer close are deferred so a runtime.Goexit
// (t.Fatal inside fn) or a panic cannot leave os.Stderr redirected at a
// dangling pipe or leak the drain goroutine. Mirrors captureStdout in
// cmd/testhelpers_test.go — keep the "exactly one close on every exit path"
// contract if you refactor.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	// Double-close guard: the deferred close runs when fn does a
	// runtime.Goexit (t.Fatal) or panics, where the explicit close below
	// never reached. On the normal path the flag is flipped and the defer
	// becomes a no-op. Exactly one close happens on every exit path, so the
	// drain goroutine (which exits on pipe EOF once the write end closes)
	// cannot leak.
	closed := false
	defer func() {
		if !closed {
			_ = w.Close()
		}
	}()
	fn()
	_ = w.Close()
	closed = true

	select {
	case s := <-done:
		return s
	case <-time.After(5 * time.Second):
		t.Fatal("captureStderr timed out waiting for goroutine (pipe deadlocked?)")
		return ""
	}
}

// swapFrontMatterLines rewrites a nib's on-disk file by swapping the whole lines
// for keyA and keyB in the YAML front matter. Because YAML maps are order-
// independent and Render() re-emits keys in a fixed order, this produces a file
// whose raw bytes differ but whose parsed→canonical form (and thus the stored
// etag computed by computeStoredETag) is unchanged — i.e. benign formatting drift.
func swapFrontMatterLines(t *testing.T, path, keyA, keyB string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	lines := strings.Split(string(raw), "\n")
	// Bound the search to the front-matter block only — between the first two
	// `---` fences — so a matching line in the body can never be swapped by
	// accident.
	fmStart, fmEnd := -1, -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "---" {
			if fmStart == -1 {
				fmStart = i
			} else {
				fmEnd = i
				break
			}
		}
	}
	if fmStart == -1 || fmEnd == -1 {
		t.Fatalf("could not locate front-matter fences (--- ... ---) in %s", path)
	}
	ai, bi := -1, -1
	for i := fmStart + 1; i < fmEnd; i++ {
		if strings.HasPrefix(lines[i], keyA+":") {
			ai = i
		}
		if strings.HasPrefix(lines[i], keyB+":") {
			bi = i
		}
	}
	if ai == -1 || bi == -1 {
		t.Fatalf("could not find both front-matter keys %q (%d) and %q (%d) in %s", keyA, ai, keyB, bi, path)
	}
	lines[ai], lines[bi] = lines[bi], lines[ai]
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestActivateParentChainBenignDriftActivates covers the original nibs-e9oz bug,
// which nibs-znt8 fixed by canonicalizing the stored etag: a todo parent whose
// on-disk file is canonically equivalent to (but not byte-identical with) its
// in-memory form must still auto-activate when a child starts, with no false
// etag mismatch and no stderr warning.
//
// Property covered: benign-drift etag tolerance (successful activation path).
// This does NOT guard the clone-hygiene fix — it passes with or without the
// clone because the activation succeeds. The dedicated shared-pointer
// regression guard is TestActivateParentChainFailedWriteLeavesSharedNibUntouched.
func TestActivateParentChainBenignDriftActivates(t *testing.T) {
	resolver, core := setupTestResolverWithAutoActivation(t)
	ctx := context.Background()

	parent := createTestNib(t, core, "epic-ben", "Benign Parent", "todo")
	parent.Type = "epic"
	if err := core.Update(parent, nil); err != nil {
		t.Fatalf("setup: update parent type: %v", err)
	}

	child := createTestNib(t, core, "task-ben", "Child Task", "todo")
	child.Type = "task"
	child.Parent = "epic-ben"
	if err := core.Update(child, nil); err != nil {
		t.Fatalf("setup: update child parent: %v", err)
	}

	// Introduce benign formatting drift on the parent's on-disk file: reorder two
	// front-matter keys. Raw bytes now differ, but the canonical stored etag must
	// still equal the in-memory parent's etag (the property znt8 established).
	parentPath := filepath.Join(core.Root(), parent.Path)
	swapFrontMatterLines(t, parentPath, "status", "type")

	storedETag, err := core.CurrentETag("epic-ben")
	if err != nil {
		t.Fatalf("CurrentETag: %v", err)
	}
	if storedETag != parent.ETag() {
		t.Fatalf("benign drift should be canonically equivalent: stored etag %q != in-memory etag %q", storedETag, parent.ETag())
	}

	// Start the child; activateParentChain must activate the parent.
	inProgress := "in-progress"
	input := model.UpdateNibInput{Status: &inProgress}
	stderr := captureStderr(t, func() {
		if _, err := resolver.Mutation().UpdateNib(ctx, "task-ben", input); err != nil {
			t.Fatalf("UpdateNib failed: %v", err)
		}
	})

	if strings.Contains(stderr, "failed to activate parent") {
		t.Errorf("expected no activation warning on benign drift, got stderr: %q", stderr)
	}

	updatedParent, err := resolver.Query().Nib(ctx, "epic-ben")
	if err != nil {
		t.Fatalf("Query parent: %v", err)
	}
	if updatedParent.Status != "in-progress" {
		t.Errorf("expected parent 'in-progress' after benign-drift activation, got %q", updatedParent.Status)
	}
}

const (
	divergedTitle = "DIVERGED-ON-DISK-TITLE"
	divergedBody  = "This parent content was written directly to disk, out of band.\n"
)

// divergeParentThenStartChild sets up a todo epic parent and todo task child,
// then genuinely diverges the parent's ON-DISK file (different title/body, still
// parseable and still status todo) so its stored etag no longer matches the
// in-memory parent's etag. It then starts the child so activateParentChain runs,
// returning the parent's on-disk path and any captured stderr.
func divergeParentThenStartChild(t *testing.T, resolver *Resolver, core *nibcore.Core) (parentPath, stderr string) {
	t.Helper()
	ctx := context.Background()

	parent := createTestNib(t, core, "epic-div", "Parent Epic", "todo")
	parent.Type = "epic"
	if err := core.Update(parent, nil); err != nil {
		t.Fatalf("setup: update parent type: %v", err)
	}

	child := createTestNib(t, core, "task-div", "Child Task", "todo")
	child.Type = "task"
	child.Parent = "epic-div"
	if err := core.Update(child, nil); err != nil {
		t.Fatalf("setup: update child parent: %v", err)
	}

	// Write a genuinely divergent parent file straight to disk (bypassing Core),
	// so the on-disk content no longer matches the in-memory parent.
	diverged := &nib.Nib{
		ID:     parent.ID,
		Slug:   parent.Slug,
		Title:  divergedTitle,
		Status: "todo",
		Type:   "epic",
		Body:   divergedBody,
	}
	content, err := diverged.Render()
	if err != nil {
		t.Fatalf("render diverged nib: %v", err)
	}
	parentPath = filepath.Join(core.Root(), parent.Path)
	if err := os.WriteFile(parentPath, content, 0644); err != nil {
		t.Fatalf("write diverged parent file: %v", err)
	}

	inProgress := "in-progress"
	input := model.UpdateNibInput{Status: &inProgress}
	stderr = captureStderr(t, func() {
		if _, err := resolver.Mutation().UpdateNib(ctx, "task-div", input); err != nil {
			t.Fatalf("UpdateNib failed: %v", err)
		}
	})
	return parentPath, stderr
}

// TestActivateParentChainGenuineDivergenceIsRefused locks in real lost-update
// protection: when the parent's on-disk file has genuinely diverged, activation
// must be refused (best-effort warn) and — critically — the divergent on-disk
// content must SURVIVE, never be clobbered by the stale in-memory parent. This
// guards the property a prior attempt (CurrentETag as if-match) broke.
//
// Property covered: on-disk mismatch-refusal integrity (the divergent file and
// its todo status survive). It ALSO carries the clone-hygiene guard via the
// shared-pointer assertion at the end (added so the divergence scenario is
// self-guarding); the dedicated regression guard remains
// TestActivateParentChainFailedWriteLeavesSharedNibUntouched.
func TestActivateParentChainGenuineDivergenceIsRefused(t *testing.T) {
	resolver, core := setupTestResolverWithAutoActivation(t)

	parentPath, stderr := divergeParentThenStartChild(t, resolver, core)

	if !strings.Contains(stderr, "failed to activate parent") {
		t.Errorf("expected an activation warning on genuine divergence, got stderr: %q", stderr)
	}

	// The divergent on-disk content must survive: not overwritten by the stale
	// in-memory parent.
	raw, err := os.ReadFile(parentPath)
	if err != nil {
		t.Fatalf("read parent file: %v", err)
	}
	if !strings.Contains(string(raw), divergedTitle) || !strings.Contains(string(raw), strings.TrimSpace(divergedBody)) {
		t.Errorf("divergent on-disk content was clobbered; file now:\n%s", raw)
	}

	// The persisted parent must still be todo (activation did not take effect).
	onDisk, err := nib.Parse(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse parent file: %v", err)
	}
	if onDisk.Status != "todo" {
		t.Errorf("expected on-disk parent to remain 'todo', got %q", onDisk.Status)
	}

	// Clone-hygiene guard carried directly by the divergence scenario: the
	// SHARED in-memory parent (Reader.Get hands back c.nibs[id], not a copy)
	// must still be todo after the refused activation. Without the clone in
	// activateParentChain this fails — the shared pointer would show
	// in-progress even though the write was rejected. Duplicates the dedicated
	// TestActivateParentChainFailedWriteLeavesSharedNibUntouched on purpose so
	// this scenario is self-guarding.
	shared, err := resolver.Reader.Get("epic-div")
	if err != nil {
		t.Fatalf("Reader.Get: %v", err)
	}
	if shared.Status != "todo" {
		t.Errorf("shared in-memory parent was mutated despite refused activation: status=%q, want 'todo'", shared.Status)
	}
}

// TestActivateParentChainFailedWriteLeavesSharedNibUntouched is the clone-hygiene
// regression: after a failed activation (genuine divergence), the SHARED
// in-memory parent returned by Reader.Get must still be todo. Without cloning,
// activateParentChain mutates the shared pointer to in-progress before the write,
// so a failed write leaves the in-memory store falsely showing in-progress.
func TestActivateParentChainFailedWriteLeavesSharedNibUntouched(t *testing.T) {
	resolver, core := setupTestResolverWithAutoActivation(t)

	divergeParentThenStartChild(t, resolver, core)

	got, err := resolver.Reader.Get("epic-div")
	if err != nil {
		t.Fatalf("Reader.Get: %v", err)
	}
	if got.Status != "todo" {
		t.Errorf("shared in-memory parent was mutated despite failed write: status=%q, want 'todo'", got.Status)
	}
}

// TestActivateParentChainMultiLevelActivatesAllAncestors guards the loop: a chain
// of todo ancestors that are all canonically consistent must all activate, with
// no stderr warnings.
//
// Property covered: multi-level activation (the walk climbs the whole chain).
// This does NOT guard the clone-hygiene fix — every activation succeeds, so it
// passes with or without the clone. The dedicated shared-pointer regression
// guard is TestActivateParentChainFailedWriteLeavesSharedNibUntouched.
func TestActivateParentChainMultiLevelActivatesAllAncestors(t *testing.T) {
	resolver, core := setupTestResolverWithAutoActivation(t)
	ctx := context.Background()

	ms := createTestNib(t, core, "ms-lvl", "Milestone", "draft")
	ms.Type = "milestone"
	if err := core.Update(ms, nil); err != nil {
		t.Fatalf("setup milestone: %v", err)
	}

	epic := createTestNib(t, core, "epic-lvl", "Epic", "todo")
	epic.Type = "epic"
	epic.Parent = "ms-lvl"
	if err := core.Update(epic, nil); err != nil {
		t.Fatalf("setup epic: %v", err)
	}

	task := createTestNib(t, core, "task-lvl", "Task", "todo")
	task.Type = "task"
	task.Parent = "epic-lvl"
	if err := core.Update(task, nil); err != nil {
		t.Fatalf("setup task: %v", err)
	}

	inProgress := "in-progress"
	input := model.UpdateNibInput{Status: &inProgress}
	stderr := captureStderr(t, func() {
		if _, err := resolver.Mutation().UpdateNib(ctx, "task-lvl", input); err != nil {
			t.Fatalf("UpdateNib failed: %v", err)
		}
	})

	if strings.Contains(stderr, "failed to activate parent") {
		t.Errorf("expected no activation warnings, got stderr: %q", stderr)
	}

	for _, id := range []string{"epic-lvl", "ms-lvl"} {
		got, err := resolver.Query().Nib(ctx, id)
		if err != nil {
			t.Fatalf("Query %s: %v", id, err)
		}
		if got.Status != "in-progress" {
			t.Errorf("expected %s 'in-progress', got %q", id, got.Status)
		}
	}
}
