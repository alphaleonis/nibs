package graph

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/store"
)

// storedAreasFile reads the store's areas.yml back off disk, so an assertion
// judges what was persisted rather than what the in-memory vocabulary holds.
func storedAreasFile(t *testing.T, root string) string {
	t.Helper()
	raw, err := os.ReadFile(store.NewLayout(root).AreasPath())
	if err != nil {
		t.Fatalf("reading the areas vocabulary: %v", err)
	}
	return string(raw)
}

// TestRenameAreaRenamesADeclaredArea is the tracer bullet for the area write
// path over GraphQL: lock, re-read, plan, write, and answer with the whole
// vocabulary as it now stands.
func TestRenameAreaRenamesADeclaredArea(t *testing.T) {
	resolver, core := setupTestResolverWithAreas(t)

	cfg, err := resolver.Mutation().RenameArea(context.Background(), model.RenameAreaInput{Path: "web", NewName: "platform"})
	if err != nil {
		t.Fatalf("RenameArea: %v", err)
	}

	got := pathsOf(cfg.Areas)
	want := []string{"platform", "platform/dashboard", "platform/ui", "auth"}
	if !slices.Equal(got, want) {
		t.Errorf("areas = %v, want %v", got, want)
	}

	stored := storedAreasFile(t, core.Root())
	if !strings.Contains(stored, "name: platform") {
		t.Errorf("areas.yml does not declare the new name:\n%s", stored)
	}
	if strings.Contains(stored, "name: web") {
		t.Errorf("areas.yml still declares the old name:\n%s", stored)
	}
}

// storedAreaOfNib reads a nib's `area:` back OFF DISK, so an assertion judges
// what the cascade persisted rather than what the in-memory map happens to hold.
func storedAreaOfNib(t *testing.T, core *nibcore.Core, id string) string {
	t.Helper()
	b, err := core.Get(id)
	if err != nil {
		t.Fatalf("%s is not in the store: %v", id, err)
	}
	raw, err := os.ReadFile(filepath.Join(core.Root(), filepath.FromSlash(b.Path)))
	if err != nil {
		t.Fatalf("reading %s: %v", b.Path, err)
	}
	for line := range strings.SplitSeq(string(raw), "\n") {
		if area, ok := strings.CutPrefix(line, "area: "); ok {
			return area
		}
	}
	return ""
}

// TestRenameAreaCascadesToMembers is the guard on the member rewrite: it runs
// BETWEEN the plan and the config write, inside the one critical section the
// store's own verb holds both of its locks across.
//
// A member assigned BELOW the renamed node keeps the remainder it carried, so
// the two rows are not the same assertion twice: `web/ui` has to arrive at
// `platform/ui` and not at `platform`.
func TestRenameAreaCascadesToMembers(t *testing.T) {
	resolver, core := setupTestResolverWithAreas(t)
	mustCreate(t, core, &nib.Nib{ID: "cas1", Title: "On the node", Type: "task", Status: "todo", Area: "web"})
	mustCreate(t, core, &nib.Nib{ID: "cas2", Title: "Below it", Type: "task", Status: "todo", Area: "web/ui"})
	mustCreate(t, core, &nib.Nib{ID: "cas3", Title: "Elsewhere", Type: "task", Status: "todo", Area: "auth"})

	if _, err := resolver.Mutation().RenameArea(context.Background(), model.RenameAreaInput{Path: "web", NewName: "platform"}); err != nil {
		t.Fatalf("RenameArea: %v", err)
	}

	for _, tc := range []struct{ id, want string }{
		{"cas1", "platform"},
		{"cas2", "platform/ui"},
		{"cas3", "auth"},
	} {
		if got := storedAreaOfNib(t, core, tc.id); got != tc.want {
			t.Errorf("%s stored area = %q, want %q", tc.id, got, tc.want)
		}
	}
}

// storeSnapshot fingerprints every file under the store, so a refusal can be
// asserted to have written NOTHING rather than merely to have returned an error.
// Comparing whole contents rather than mtimes is what makes it insensitive to
// filesystem timestamp granularity.
func storeSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		snap[filepath.ToSlash(rel)] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatalf("snapshotting the store: %v", err)
	}
	return snap
}

func assertStoreUnchanged(t *testing.T, before map[string]string, root string) {
	t.Helper()
	after := storeSnapshot(t, root)
	if len(before) != len(after) {
		t.Errorf("the store gained or lost files: %d before, %d after", len(before), len(after))
	}
	for name, want := range before {
		got, ok := after[name]
		switch {
		case !ok:
			t.Errorf("%s is gone after a refused edit", name)
		case got != want:
			t.Errorf("%s was rewritten by a refused edit:\n--- before ---\n%s\n--- after ---\n%s", name, want, got)
		}
	}
}

// TestRenameAreaRefusesAnUndeclaredPath pins the refusal the planners cannot
// word: it names the declared set, which is the repair.
func TestRenameAreaRefusesAnUndeclaredPath(t *testing.T) {
	resolver, core := setupTestResolverWithAreas(t)
	mustCreate(t, core, &nib.Nib{ID: "und1", Title: "Member", Type: "task", Status: "todo", Area: "web"})
	before := storeSnapshot(t, core.Root())

	_, err := resolver.Mutation().RenameArea(context.Background(), model.RenameAreaInput{Path: "nosuch", NewName: "platform"})
	if err == nil {
		t.Fatal("RenameArea accepted a path the store does not declare")
	}
	var ioErr *nibcore.AreaEditIOError
	if errors.As(err, &ioErr) {
		t.Errorf("an undeclared path is a validation-class refusal, got the IO class: %v", err)
	}
	for _, want := range []string{"nosuch", "web/dashboard"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want substring %q", err.Error(), want)
		}
	}
	assertStoreUnchanged(t, before, core.Root())
}

// TestRemoveAreaRefusesAnUndeclaredPath is TestRenameAreaRefusesAnUndeclaredPath
// for the other verb. The two mutations ask requireDeclaredArea the same
// question and neither planner can word the answer: the refusal names the
// declared set, which is the repair.
func TestRemoveAreaRefusesAnUndeclaredPath(t *testing.T) {
	resolver, core := setupTestResolverWithAreas(t)
	mustCreate(t, core, &nib.Nib{ID: "rund1", Title: "Member", Type: "task", Status: "todo", Area: "web"})
	before := storeSnapshot(t, core.Root())

	_, err := resolver.Mutation().RemoveArea(context.Background(), model.RemoveAreaInput{Path: "nosuch"})
	if err == nil {
		t.Fatal("RemoveArea accepted a path the store does not declare")
	}
	var ioErr *nibcore.AreaEditIOError
	if errors.As(err, &ioErr) {
		t.Errorf("an undeclared path is a validation-class refusal, got the IO class: %v", err)
	}
	for _, want := range []string{"nosuch", "web/dashboard"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want substring %q", err.Error(), want)
		}
	}
	// Asked BEFORE the member set is read, which is what keeps the answer from
	// being "nothing is assigned at or below it" about an area that is not there.
	if strings.Contains(err.Error(), "nothing is assigned") {
		t.Errorf("error = %q, want it to refuse the path rather than speak about its members", err.Error())
	}
	assertStoreUnchanged(t, before, core.Root())
}

// TestRenameAreaRefusesASiblingNameBeforeTouchingAMember is the plan/write
// split's whole purpose, asserted where it is observable: the collision is a
// refusal the planner makes, and planning runs BEFORE the cascade, so the store
// is still untouched when it lands. Reversed, the members would already carry a
// path the vocabulary does not declare and every later write to them would be
// refused for it.
func TestRenameAreaRefusesASiblingNameBeforeTouchingAMember(t *testing.T) {
	resolver, core := setupTestResolverWithAreas(t)
	mustCreate(t, core, &nib.Nib{ID: "sib1", Title: "Member", Type: "task", Status: "todo", Area: "web/dashboard"})
	mustCreate(t, core, &nib.Nib{ID: "sib2", Title: "Also", Type: "task", Status: "todo", Area: "web/dashboard"})
	before := storeSnapshot(t, core.Root())

	_, err := resolver.Mutation().RenameArea(context.Background(), model.RenameAreaInput{Path: "web/dashboard", NewName: "ui"})
	if err == nil {
		t.Fatal("RenameArea accepted a name a sibling already holds")
	}
	var ioErr *nibcore.AreaEditIOError
	if errors.As(err, &ioErr) {
		t.Errorf("a sibling collision is a validation-class refusal, got the IO class: %v", err)
	}
	assertStoreUnchanged(t, before, core.Root())
}

// TestRenameAreaArgumentRefusals covers the ones the arguments settle on their
// own. A rename to the name the node already carries is a valid no-op WRITE as
// far as the planner is concerned, so this resolver is what refuses it; a name
// carrying the path separator is the planner's, since the edited vocabulary
// would not load.
//
// The last row is the one this surface creates: the wire carries a name of any
// length the request body holds, and a long enough one writes an areas.yml past
// MaxConfigBytes — which Core.Load refuses before it walks the nibs, leaving a
// store no command can open, this mutation included.
func TestRenameAreaArgumentRefusals(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		newName string
	}{
		{name: "the name it already has", path: "web", newName: "web"},
		{name: "a name carrying the separator", path: "web", newName: "platform/ui"},
		{name: "a nested name carrying the separator", path: "web/ui", newName: "web/dashboard"},
		{name: "a name no store could read back", path: "web", newName: strings.Repeat("x", 201)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver, core := setupTestResolverWithAreas(t)
			mustCreate(t, core, &nib.Nib{ID: "arg1", Title: "Member", Type: "task", Status: "todo", Area: "web"})
			before := storeSnapshot(t, core.Root())

			_, err := resolver.Mutation().RenameArea(context.Background(), model.RenameAreaInput{Path: tt.path, NewName: tt.newName})
			if err == nil {
				t.Fatalf("RenameArea(%q -> %q) was accepted", tt.path, tt.newName)
			}
			var ioErr *nibcore.AreaEditIOError
			if errors.As(err, &ioErr) {
				t.Errorf("want a validation-class refusal, got the IO class: %v", err)
			}
			assertStoreUnchanged(t, before, core.Root())
		})
	}
}

// TestRemoveAreaRetiresTheSubtree pins that a retire takes the whole subtree
// with it: leaving `web`'s children behind would declare paths with no parent.
func TestRemoveAreaRetiresTheSubtree(t *testing.T) {
	resolver, core := setupTestResolverWithAreas(t)

	cfg, err := resolver.Mutation().RemoveArea(context.Background(), model.RemoveAreaInput{Path: "web"})
	if err != nil {
		t.Fatalf("RemoveArea: %v", err)
	}

	if got, want := pathsOf(cfg.Areas), []string{"auth"}; !slices.Equal(got, want) {
		t.Errorf("areas = %v, want %v", got, want)
	}
	stored := storedAreasFile(t, core.Root())
	for _, gone := range []string{"name: web", "name: dashboard", "name: ui"} {
		if strings.Contains(stored, gone) {
			t.Errorf("areas.yml still carries %q:\n%s", gone, stored)
		}
	}
	if !strings.Contains(stored, "name: auth") {
		t.Errorf("areas.yml lost an area the retire did not name:\n%s", stored)
	}
}

// TestRemoveAreaRefusesWhileMembersRemain is the refusal that keeps a retire
// from stranding work: a nib left carrying a path the vocabulary no longer
// declares is write-refused from then on, with nothing printing a reason.
func TestRemoveAreaRefusesWhileMembersRemain(t *testing.T) {
	resolver, core := setupTestResolverWithAreas(t)
	mustCreate(t, core, &nib.Nib{ID: "rm1", Title: "On the node", Type: "task", Status: "todo", Area: "web"})
	mustCreate(t, core, &nib.Nib{ID: "rm2", Title: "Below it", Type: "task", Status: "todo", Area: "web/ui"})
	mustCreate(t, core, &nib.Nib{ID: "rm3", Title: "Elsewhere", Type: "task", Status: "todo", Area: "auth"})
	before := storeSnapshot(t, core.Root())

	_, err := resolver.Mutation().RemoveArea(context.Background(), model.RemoveAreaInput{Path: "web"})
	if err == nil {
		t.Fatal("RemoveArea retired an area with members and no disposition")
	}
	var ioErr *nibcore.AreaEditIOError
	if errors.As(err, &ioErr) {
		t.Errorf("a member refusal is validation-class, got the IO class: %v", err)
	}
	for _, want := range []string{"rm1", "rm2"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to name member %q", err.Error(), want)
		}
	}
	if strings.Contains(err.Error(), "rm3") {
		t.Errorf("error = %q names a nib that is not a member", err.Error())
	}
	assertStoreUnchanged(t, before, core.Root())
}

// TestRemoveAreaMoveToLandsMembersOnTheTarget pins the disposition's one
// non-obvious rule: a member assigned BELOW the retiring node does not keep the
// remainder it carried. The target declares no such child, so preserving it
// would walk that member to another undeclared path — the very state the member
// refusal exists to prevent.
func TestRemoveAreaMoveToLandsMembersOnTheTarget(t *testing.T) {
	resolver, core := setupTestResolverWithAreas(t)
	mustCreate(t, core, &nib.Nib{ID: "mv1", Title: "On the node", Type: "task", Status: "todo", Area: "web"})
	mustCreate(t, core, &nib.Nib{ID: "mv2", Title: "Below it", Type: "task", Status: "todo", Area: "web/ui"})
	mustCreate(t, core, &nib.Nib{ID: "mv3", Title: "Elsewhere", Type: "task", Status: "todo", Area: "auth"})

	target := "auth"
	cfg, err := resolver.Mutation().RemoveArea(context.Background(), model.RemoveAreaInput{Path: "web", MoveTo: &target})
	if err != nil {
		t.Fatalf("RemoveArea(moveTo): %v", err)
	}

	if got, want := pathsOf(cfg.Areas), []string{"auth"}; !slices.Equal(got, want) {
		t.Errorf("areas = %v, want %v", got, want)
	}
	for _, id := range []string{"mv1", "mv2", "mv3"} {
		if got := storedAreaOfNib(t, core, id); got != "auth" {
			t.Errorf("%s stored area = %q, want auth", id, got)
		}
	}
}

// TestRemoveAreaUnassignClearsMembers pins the other disposition: the empty
// string is the legal cleared value for `area:`.
func TestRemoveAreaUnassignClearsMembers(t *testing.T) {
	resolver, core := setupTestResolverWithAreas(t)
	mustCreate(t, core, &nib.Nib{ID: "un1", Title: "On the node", Type: "task", Status: "todo", Area: "web"})
	mustCreate(t, core, &nib.Nib{ID: "un2", Title: "Below it", Type: "task", Status: "todo", Area: "web/ui"})
	mustCreate(t, core, &nib.Nib{ID: "un3", Title: "Elsewhere", Type: "task", Status: "todo", Area: "auth"})

	unassign := true
	cfg, err := resolver.Mutation().RemoveArea(context.Background(), model.RemoveAreaInput{Path: "web", Unassign: &unassign})
	if err != nil {
		t.Fatalf("RemoveArea(unassign): %v", err)
	}

	if got, want := pathsOf(cfg.Areas), []string{"auth"}; !slices.Equal(got, want) {
		t.Errorf("areas = %v, want %v", got, want)
	}
	for _, id := range []string{"un1", "un2"} {
		if got := storedAreaOfNib(t, core, id); got != "" {
			t.Errorf("%s stored area = %q, want it cleared", id, got)
		}
	}
	if got := storedAreaOfNib(t, core, "un3"); got != "auth" {
		t.Errorf("un3 stored area = %q, want auth untouched", got)
	}
}

// TestRemoveAreaDispositionRefusals covers the answers a retire will not give.
// Each leaves the store byte-identical: a disposition is refused before the
// cascade, so no member is half-disposed-of when one lands.
func TestRemoveAreaDispositionRefusals(t *testing.T) {
	strptr := func(s string) *string { return &s }
	boolptr := func(b bool) *bool { return &b }

	tests := []struct {
		name    string
		members map[string]string
		input   model.RemoveAreaInput
		want    []string
	}{
		{
			name:    "both dispositions at once",
			members: map[string]string{"dr1": "web"},
			input:   model.RemoveAreaInput{Path: "web", MoveTo: strptr("auth"), Unassign: boolptr(true)},
			want:    []string{"moveTo", "unassign"},
		},
		{
			name:  "moveTo for an area nothing is assigned to",
			input: model.RemoveAreaInput{Path: "web", MoveTo: strptr("auth")},
			want:  []string{"nothing to reassign", "moveTo"},
		},
		{
			name:  "unassign for an area nothing is assigned to",
			input: model.RemoveAreaInput{Path: "web", Unassign: boolptr(true)},
			want:  []string{"nothing to unassign", "unassign"},
		},
		{
			name:    "moveTo names an undeclared area",
			members: map[string]string{"dr2": "web"},
			input:   model.RemoveAreaInput{Path: "web", MoveTo: strptr("nosuch")},
			want:    []string{"nosuch", "web/dashboard"},
		},
		{
			name:    "moveTo names an area inside the subtree being retired",
			members: map[string]string{"dr3": "web"},
			input:   model.RemoveAreaInput{Path: "web", MoveTo: strptr("web/ui")},
			want:    []string{"web/ui", "unassign"},
		},
		{
			// Both fields are "web", so "web" alone would pass on almost any
			// error this input could produce: the phrase is what says WHICH
			// refusal landed.
			name:    "moveTo names the area being retired",
			members: map[string]string{"dr4": "web"},
			input:   model.RemoveAreaInput{Path: "web", MoveTo: strptr("web")},
			want:    []string{"web", "name an area outside it"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver, core := setupTestResolverWithAreas(t)
			for id, area := range tt.members {
				mustCreate(t, core, &nib.Nib{ID: id, Title: "Member", Type: "task", Status: "todo", Area: area})
			}
			before := storeSnapshot(t, core.Root())

			_, err := resolver.Mutation().RemoveArea(context.Background(), tt.input)
			if err == nil {
				t.Fatalf("RemoveArea(%+v) was accepted", tt.input)
			}
			var ioErr *nibcore.AreaEditIOError
			if errors.As(err, &ioErr) {
				t.Errorf("want a validation-class refusal, got the IO class: %v", err)
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want substring %q", err.Error(), want)
				}
			}
			assertStoreUnchanged(t, before, core.Root())
		})
	}
}

// TestRemoveAreaUnassignFalseIsNoDisposition pins the reading of the third wire
// state: false is a client's checkbox left off, not a contradiction with
// moveTo — and on its own it is not a disposition at all, so an area with
// members is still refused.
func TestRemoveAreaUnassignFalseIsNoDisposition(t *testing.T) {
	unassign := false

	t.Run("beside moveTo it is not a contradiction", func(t *testing.T) {
		resolver, core := setupTestResolverWithAreas(t)
		mustCreate(t, core, &nib.Nib{ID: "uf1", Title: "Member", Type: "task", Status: "todo", Area: "web"})

		target := "auth"
		if _, err := resolver.Mutation().RemoveArea(context.Background(),
			model.RemoveAreaInput{Path: "web", MoveTo: &target, Unassign: &unassign}); err != nil {
			t.Fatalf("RemoveArea(moveTo, unassign: false): %v", err)
		}
		if got := storedAreaOfNib(t, core, "uf1"); got != "auth" {
			t.Errorf("uf1 stored area = %q, want auth", got)
		}
	})

	t.Run("alone it disposes of nothing", func(t *testing.T) {
		resolver, core := setupTestResolverWithAreas(t)
		mustCreate(t, core, &nib.Nib{ID: "uf2", Title: "Member", Type: "task", Status: "todo", Area: "web"})
		before := storeSnapshot(t, core.Root())

		_, err := resolver.Mutation().RemoveArea(context.Background(), model.RemoveAreaInput{Path: "web", Unassign: &unassign})
		if err == nil {
			t.Fatal("RemoveArea(unassign: false) retired an area with members")
		}
		if !strings.Contains(err.Error(), "uf2") {
			t.Errorf("error = %q, want it to name the member", err.Error())
		}
		assertStoreUnchanged(t, before, core.Root())
	})
}

// TestAreaMutationTicksConfigChanged pins the claim nibs-7ywa's Question 2
// settled on paper: a server writing the file it watches needs no suppression
// and no special case, because the write's own reload is what wakes the
// subscribers — the watcher's later pass then finds the vocabulary unchanged and
// ticks nobody.
//
// Without the reload the edit would still reach a browser, but only after the
// watcher's debounce, and the Config the mutation itself answered with would
// carry the vocabulary it replaced.
func TestAreaMutationTicksConfigChanged(t *testing.T) {
	resolver, _ := setupTestResolverWithAreas(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := resolver.Subscription().ConfigChanged(ctx)
	if err != nil {
		t.Fatalf("ConfigChanged: %v", err)
	}

	go func() {
		_, _ = resolver.Mutation().RenameArea(ctx, model.RenameAreaInput{Path: "web", NewName: "platform"})
	}()

	select {
	case got := <-ch:
		if got == nil {
			t.Fatal("the subscription closed instead of delivering the edited vocabulary")
		}
		want := []string{"platform", "platform/dashboard", "platform/ui", "auth"}
		if !slices.Equal(pathsOf(got.Areas), want) {
			t.Errorf("areas = %v, want %v", pathsOf(got.Areas), want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("an area mutation delivered no configChanged event")
	}
}

// TestAreaEditRunsAlongsideANibUpdate is the lock-order regression guard.
//
// The store has two locks and ONE order they may be taken in: c.mu, then the
// cross-process file lock (nibcore.Core.acquireWriteLock states it, and every
// Core mutator obeys it). An area edit that took the file lock FIRST — as this
// resolver did while it drove the verb step by step — and then reached back into
// the store for the vocabulary, the member set and the cascade inverted that
// order, and a concurrent updateNib holding c.mu while parked on the file lock
// is the other half of a textbook ABBA. Nothing recovers from it: c.mu is never
// released, so every read resolver in the process wedges too, and the file
// lock's descriptor is never closed, so every other nibs process on the machine
// blocks as well.
//
// THE GATE IS WHAT MAKES THE INTERLEAVING HAPPEN rather than hoping for it. A
// third descriptor on the same lock file holds it, so both operations have to
// queue for it and the order they queue in is this test's to choose: the area
// edit first, then the update. Released, an inverted edit takes the lock it was
// queued for and then asks for the mutex the update is holding, while the update
// asks for the lock the edit now has. Without the gate the two simply do not
// overlap — 1,600 updates against 8 renames finish in 50ms and never meet, which
// is exactly how a deadlock guard passes while its subject is broken.
//
// The timeout is the assertion, not a nicety: a deadlocked pair cannot be
// interrupted, so the guard has to report from a THIRD goroutine or it would
// hang the suite it is part of.
func TestAreaEditRunsAlongsideANibUpdate(t *testing.T) {
	resolver, core := setupTestResolverWithAreas(t)
	mustCreate(t, core, &nib.Nib{ID: "dl1", Title: "Member", Type: "task", Status: "todo", Area: "web"})
	mustCreate(t, core, &nib.Nib{ID: "dl2", Title: "Bystander", Type: "task", Status: "todo"})

	for round := range 4 {
		// The vocabulary is renamed back and forth so every round is a real
		// rename with a real member cascade rather than a refused no-op.
		from, to := "web", "platform"
		if round%2 == 1 {
			from, to = to, from
		}

		gate, err := nibcore.AcquireStoreLock(core.Root())
		if err != nil {
			t.Fatalf("round %d: holding the store lock: %v", round, err)
		}

		renamed := make(chan error, 1)
		go func() {
			_, err := resolver.Mutation().RenameArea(context.Background(),
				model.RenameAreaInput{Path: from, NewName: to})
			renamed <- err
		}()
		// Queued first, so the released gate hands the file lock to the area
		// edit — the arrival that makes an inverted order deadlock rather than
		// merely serialize.
		time.Sleep(100 * time.Millisecond)

		updated := make(chan error, 1)
		go func() {
			title := fmt.Sprintf("spin %d", round)
			_, err := resolver.Mutation().UpdateNib(context.Background(), "dl2",
				model.UpdateNibInput{Title: &title})
			updated <- err
		}()
		time.Sleep(100 * time.Millisecond)

		if err := gate.Release(); err != nil {
			t.Fatalf("round %d: releasing the gate: %v", round, err)
		}

		for pending := 2; pending > 0; pending-- {
			select {
			case err := <-renamed:
				if err != nil {
					t.Fatalf("round %d: RenameArea: %v", round, err)
				}
			case err := <-updated:
				if err != nil {
					t.Fatalf("round %d: UpdateNib: %v", round, err)
				}
			case <-time.After(20 * time.Second):
				t.Fatalf("round %d: the area edit and the nib update deadlocked", round)
			}
		}
	}
}

// stubAreaWriter stands in for the store when what is under test is the SENTENCE
// rather than the edit. Every failure these mutations word is a value the store
// hands back, so a stub that hands back one value exercises exactly one branch
// and nothing else — where driving a real store into each phase would need a
// different fault injected per row.
type stubAreaWriter struct {
	res      nibcore.AreaEditResult
	err      error
	calls    int
	warnings []string
}

func (s *stubAreaWriter) RenameArea(string, string) (nibcore.AreaEditResult, error) {
	s.calls++
	return s.res, s.err
}

func (s *stubAreaWriter) RemoveArea(string, nibcore.AreaDisposition) (nibcore.AreaEditResult, error) {
	s.calls++
	return s.res, s.err
}

func (s *stubAreaWriter) Warn(format string, args ...any) {
	s.warnings = append(s.warnings, fmt.Sprintf(format, args...))
}

// TestAreaMutationsWordEveryIOPhase pins what this surface says for each phase
// an area edit can fail in, and that the value it says it in is still the type
// cmd/set.go's mutationErrCode classifies on.
//
// The wording and the type travel together on purpose: nibcore.AreaEditIOError
// implements no Unwrap, so a surface cannot put its sentence in a wrapper and
// keep the error classifiable — it re-stamps a copy instead.
func TestAreaMutationsWordEveryIOPhase(t *testing.T) {
	cause := errors.New("disk on fire")
	tests := []struct {
		name     string
		err      *nibcore.AreaEditIOError
		retire   bool
		want     []string
		unwanted []string
	}{
		{
			name: "the write lock",
			err:  &nibcore.AreaEditIOError{Phase: nibcore.AreaEditPhaseLock, Cause: cause},
			want: []string{"write lock could not be taken", "disk on fire"},
		},
		{
			name:     "the vocabulary half of the re-read",
			err:      &nibcore.AreaEditIOError{Phase: nibcore.AreaEditPhaseLoadVocabulary, Cause: cause},
			want:     []string{"nothing was written", "areas vocabulary"},
			unwanted: []string{"this store's nibs"},
		},
		{
			name:     "the nib half of the re-read",
			err:      &nibcore.AreaEditIOError{Phase: nibcore.AreaEditPhaseLoadNibs, Cause: cause},
			want:     []string{"nothing was written", "this store's nibs"},
			unwanted: []string{"areas vocabulary"},
		},
		{
			name: "a rename's cascade",
			err: &nibcore.AreaEditIOError{
				Phase: nibcore.AreaEditPhaseCascade, Path: "web", NewPath: "platform",
				Written: []string{"a"}, Members: []string{"a", "b"}, Cause: cause,
			},
			want: []string{"rewrote 1 of the 2 nibs", `area "web"`, "rerun the same mutation"},
		},
		{
			name: "a rename's vocabulary write",
			err: &nibcore.AreaEditIOError{
				Phase: nibcore.AreaEditPhaseWrite, Path: "web", NewPath: "platform",
				Written: []string{"a", "b"}, Members: []string{"a", "b"}, Cause: cause,
			},
			want: []string{"rewrote 2 nibs", `to "platform"`, "rerun the same mutation"},
		},
		{
			name: "a retire's cascade",
			err: &nibcore.AreaEditIOError{
				Phase: nibcore.AreaEditPhaseCascade, Path: "web",
				Disposition: nibcore.UnassignAreaMembers(),
				Written:     []string{"a"}, Members: []string{"a", "b"}, Cause: cause,
			},
			retire: true,
			want:   []string{"unassigned 1 of the 2 nibs", "rerun the same mutation"},
		},
		{
			name: "a retire's vocabulary write, with a disposition",
			err: &nibcore.AreaEditIOError{
				Phase: nibcore.AreaEditPhaseWrite, Path: "web",
				Disposition: nibcore.MoveAreaMembersTo("auth"),
				Written:     []string{"a", "b"}, Members: []string{"a", "b"}, Cause: cause,
			},
			retire: true,
			want:   []string{"reassigned 2 nibs", "rerun WITHOUT moveTo"},
		},
		{
			// The no-disposition branch is reachable only for an area nothing was
			// assigned to, so it has nothing to report as done and no argument to
			// drop — and saying otherwise would claim an unassignment that never
			// ran.
			name: "a retire's vocabulary write, with none",
			err: &nibcore.AreaEditIOError{
				Phase: nibcore.AreaEditPhaseWrite, Path: "web", Cause: cause,
			},
			retire:   true,
			want:     []string{"nothing is assigned at or below it", "the store is as it was"},
			unwanted: []string{"unassign", "moveTo", "persisted"},
		},
		{
			// The one phase whose sentence must NOT say "nothing was written":
			// the cascade is already durable by then, and a rename's members are
			// sitting on a path the vocabulary does not declare yet.
			name: "the confirming re-read before the vocabulary write",
			err: &nibcore.AreaEditIOError{
				Phase: nibcore.AreaEditPhaseConfirm, Path: "web", NewPath: "platform",
				Written: []string{"a", "b"}, Members: []string{"a", "b"}, Cause: cause,
			},
			want: []string{"the vocabulary was left as it was", "2 nibs it had already rewritten are persisted", "rerun the same mutation",
				"every write to them is refused"},
			unwanted: []string{"nothing was written"},
		},
		{
			// A retire that named none rewrote nothing, so the shared arm words
			// it: there is no disposition to report as done, no field to drop,
			// and nothing sitting on an undeclared path.
			name: "the confirming re-read, with no disposition",
			err: &nibcore.AreaEditIOError{
				Phase: nibcore.AreaEditPhaseConfirm, Path: "web", Cause: cause,
			},
			retire:   true,
			want:     []string{"the vocabulary was left as it was", "rerun the same mutation once that is fixed"},
			unwanted: []string{"rerun WITHOUT", "unassign", "moveTo", "persisted", "every write to them is refused"},
		},
		{
			// A retire that carried a disposition is past its cascade here too,
			// so it needs the rerun the write phase prescribes: with every
			// member disposed of, rerunning with the field is refused.
			name: "the confirming re-read, after a disposition",
			err: &nibcore.AreaEditIOError{
				Phase: nibcore.AreaEditPhaseConfirm, Path: "web",
				Disposition: nibcore.UnassignAreaMembers(),
				Written:     []string{"a", "b"}, Members: []string{"a", "b"}, Cause: cause,
			},
			retire:   true,
			want:     []string{"unassigned 2 nibs", "rerun WITHOUT unassign"},
			unwanted: []string{"rerun the same mutation", "now that nothing is assigned"},
		},
		{
			name: "the confirming re-read, after a reassignment",
			err: &nibcore.AreaEditIOError{
				Phase: nibcore.AreaEditPhaseConfirm, Path: "web",
				Disposition: nibcore.MoveAreaMembersTo("auth"),
				Written:     []string{"a", "b"}, Members: []string{"a", "b"}, Cause: cause,
			},
			retire:   true,
			want:     []string{"reassigned 2 nibs", "rerun WITHOUT moveTo"},
			unwanted: []string{"rerun the same mutation", "now that nothing is assigned"},
		},
		{
			name: "the re-read of the file it just wrote",
			err: &nibcore.AreaEditIOError{
				Phase: nibcore.AreaEditPhaseReload, Path: "web", Cause: cause,
			},
			want: []string{"both halves of this edit landed on disk", "nothing to rerun"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver, _ := setupTestResolverWithAreas(t)
			resolver.AreaWriter = &stubAreaWriter{err: tt.err}

			var err error
			if tt.retire {
				_, err = resolver.Mutation().RemoveArea(context.Background(), model.RemoveAreaInput{Path: "web"})
			} else {
				_, err = resolver.Mutation().RenameArea(context.Background(),
					model.RenameAreaInput{Path: "web", NewName: "platform"})
			}
			if err == nil {
				t.Fatal("the mutation reported success over a failed edit")
			}
			var ioErr *nibcore.AreaEditIOError
			if !errors.As(err, &ioErr) {
				t.Fatalf("error = %v (%T), want the IO class so `nibs query` exits 5", err, err)
			}
			if ioErr.Phase != tt.err.Phase {
				t.Errorf("Phase = %d, want %d — the fields a later reader inspects must survive the wording", ioErr.Phase, tt.err.Phase)
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want substring %q", err.Error(), want)
				}
			}
			for _, unwanted := range tt.unwanted {
				if strings.Contains(err.Error(), unwanted) {
					t.Errorf("error = %q, want it not to carry %q", err.Error(), unwanted)
				}
			}
		})
	}
}

// TestAreaMutationsWordANibThatArrivedUnderTheArea pins the sentence for the
// refusal that is not a failure: the store gained work under the area while the
// edit ran, so the vocabulary was left declaring it.
//
// Both verbs reach it through the shared ladder, and both must keep the concrete
// type — cmd/set.go's mutationErrCode classifies `nibs query` on it, and the
// caller's argument was fine, so a validation fallback would blame the wrong
// party.
func TestAreaMutationsWordANibThatArrivedUnderTheArea(t *testing.T) {
	calls := []struct {
		name string
		// newPath is what nibcore sets on a rename's refusal and leaves empty on
		// a retire's, and it is what selects the clause below.
		newPath string
		call    func(*Resolver) error
	}{
		{"rename", "platform", func(r *Resolver) error {
			_, err := r.Mutation().RenameArea(context.Background(),
				model.RenameAreaInput{Path: "web", NewName: "platform"})
			return err
		}},
		{"retire", "", func(r *Resolver) error {
			_, err := r.Mutation().RemoveArea(context.Background(), model.RemoveAreaInput{Path: "web"})
			return err
		}},
	}
	// A retire of an area nothing was assigned to cascades nothing, so the clause
	// naming what is persisted has to be ABSENT rather than zero — and absent
	// without leaving the gap it sat in.
	cascades := []struct {
		name     string
		written  []string
		want     []string
		unwanted []string
	}{
		{
			name:    "after a cascade",
			written: []string{"nibs-a2"},
			want:    []string{"the nib it had already rewritten is persisted, so rerun"},
		},
		{
			name:     "with nothing cascaded",
			want:     []string{"usual one; rerun the same mutation"},
			unwanted: []string{"rewritten", "  "},
		},
	}
	for _, tt := range calls {
		for _, c := range cascades {
			t.Run(tt.name+", "+c.name, func(t *testing.T) {
				resolver, _ := setupTestResolverWithAreas(t)
				resolver.AreaWriter = &stubAreaWriter{err: &nibcore.AreaMembersArrivedError{
					Path: "web", Members: []string{"nibs-a1"}, Written: c.written, NewPath: tt.newPath,
				}}

				err := tt.call(resolver)
				if err == nil {
					t.Fatal("the mutation reported success over an edit that would have stranded a nib")
				}
				var got *nibcore.AreaMembersArrivedError
				if !errors.As(err, &got) {
					t.Fatalf("error = %v (%T), want the arrival class so `nibs query` exits 5", err, err)
				}
				want := append([]string{"the vocabulary was left as it was", `area "web"`, "nibs-a1"}, c.want...)
				for _, w := range want {
					if !strings.Contains(err.Error(), w) {
						t.Errorf("error = %q, want substring %q", err.Error(), w)
					}
				}
				for _, u := range c.unwanted {
					if strings.Contains(err.Error(), u) {
						t.Errorf("error = %q, want it not to carry %q", err.Error(), u)
					}
				}
				// "persisted" reads as reassurance for a rename, whose cascaded
				// members are on a path the vocabulary does not declare until the
				// rerun. Nothing else strands one, so nothing else says it.
				stranded := tt.newPath != "" && len(c.written) > 0
				if got := strings.Contains(err.Error(), "every write to them is refused"); got != stranded {
					t.Errorf("error = %q, carries the stranded-member clause = %v, want %v", err.Error(), got, stranded)
				}
			})
		}
	}
}

// TestAreaMutationsAnswerArgumentsWithoutTheStore is finding #5's first half: a
// question the arguments alone answer must not sit behind the store's write
// lock, which is a blocking flock with no timeout that says nothing while it
// waits. The stub's call count is the assertion — the store is never reached.
func TestAreaMutationsAnswerArgumentsWithoutTheStore(t *testing.T) {
	unassign := true
	tests := []struct {
		name string
		call func(*Resolver) error
		want string
	}{
		{
			name: "an empty new name",
			call: func(r *Resolver) error {
				_, err := r.Mutation().RenameArea(context.Background(), model.RenameAreaInput{Path: "web", NewName: ""})
				return err
			},
			want: "none was given",
		},
		{
			name: "a padded new name",
			call: func(r *Resolver) error {
				_, err := r.Mutation().RenameArea(context.Background(), model.RenameAreaInput{Path: "web", NewName: " ui"})
				return err
			},
			want: "whitespace",
		},
		{
			name: "a new name carrying the separator",
			call: func(r *Resolver) error {
				_, err := r.Mutation().RenameArea(context.Background(), model.RenameAreaInput{Path: "web", NewName: "a/b"})
				return err
			},
			want: "is not a name",
		},
		{
			name: "a name no store could read back",
			call: func(r *Resolver) error {
				_, err := r.Mutation().RenameArea(context.Background(),
					model.RenameAreaInput{Path: "web", NewName: strings.Repeat("x", 201)})
				return err
			},
			want: "bounded at 200",
		},
		{
			name: "both dispositions at once",
			call: func(r *Resolver) error {
				target := "auth"
				_, err := r.Mutation().RemoveArea(context.Background(),
					model.RemoveAreaInput{Path: "web", MoveTo: &target, Unassign: &unassign})
				return err
			},
			want: "two different dispositions",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver, _ := setupTestResolverWithAreas(t)
			stub := &stubAreaWriter{}
			resolver.AreaWriter = stub

			err := tt.call(resolver)
			if err == nil {
				t.Fatal("the mutation was accepted")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want substring %q", err.Error(), tt.want)
			}
			if stub.calls != 0 {
				t.Errorf("the store was asked %d times for a question the arguments answer", stub.calls)
			}
			var ioErr *nibcore.AreaEditIOError
			if errors.As(err, &ioErr) {
				t.Errorf("error = %v, want a validation-class refusal", err)
			}
		})
	}
}

// TestAreaMutationReportsAReplacedSymlink: the answer is a Config, so the note
// an edit owes when it replaced a symlinked areas.yml has nowhere to go on the
// wire — it goes to the store's warning sink, where a running `nibs serve`
// operator reads. Dropped, the mutation reports success over an edit that
// whatever manages the link target is about to undo.
func TestAreaMutationReportsAReplacedSymlink(t *testing.T) {
	resolver, core := setupTestResolverWithAreas(t)
	stub := &stubAreaWriter{res: nibcore.AreaEditResult{
		Areas:           core.Areas(),
		StaleLinkTarget: "/srv/vocab/areas.yml",
	}}
	resolver.AreaWriter = stub

	if _, err := resolver.Mutation().RenameArea(context.Background(),
		model.RenameAreaInput{Path: "web", NewName: "platform"}); err != nil {
		t.Fatalf("RenameArea: %v", err)
	}
	if len(stub.warnings) != 1 {
		t.Fatalf("warnings = %v, want the stale-link note", stub.warnings)
	}
	for _, want := range []string{"/srv/vocab/areas.yml", "symlink", "old vocabulary"} {
		if !strings.Contains(stub.warnings[0], want) {
			t.Errorf("warning = %q, want substring %q", stub.warnings[0], want)
		}
	}

	// And an ordinary edit warns about nothing, so the note above is the
	// replacement being reported and not a line every edit prints.
	stub.res.StaleLinkTarget = ""
	stub.warnings = nil
	if _, err := resolver.Mutation().RenameArea(context.Background(),
		model.RenameAreaInput{Path: "web", NewName: "platform"}); err != nil {
		t.Fatalf("RenameArea: %v", err)
	}
	if len(stub.warnings) != 0 {
		t.Errorf("warnings = %v, want none", stub.warnings)
	}
}

// TestAreaMutationsNameNoPath is the mechanism behind the claim at the head of
// area_edit.go: these messages reach an unauthenticated HTTP client, and an
// absolute store path in one discloses the operating-system username and the
// project layout.
//
// It drives REAL refusals through the real store — every refusal class the two
// mutations can raise from a store that is intact — because the leak it closes
// was per-branch: the planner's inner reason was already path-free and its
// wrapper added the path, so a reader auditing one branch concluded the surface
// was clean.
//
// What it does NOT cover is an IO failure's Cause: an operating-system error
// embeds the path it failed on, and redacting that is a separate boundary from
// this one. Those messages are reachable only from a store that is already
// broken.
func TestAreaMutationsNameNoPath(t *testing.T) {
	unassign := true
	tests := []struct {
		name  string
		setup func(t *testing.T, core *nibcore.Core)
		call  func(*Resolver) error
	}{
		{
			name: "a path the store does not declare",
			call: func(r *Resolver) error {
				_, err := r.Mutation().RenameArea(context.Background(), model.RenameAreaInput{Path: "nosuch", NewName: "x"})
				return err
			},
		},
		{
			name: "a name a sibling already holds",
			call: func(r *Resolver) error {
				_, err := r.Mutation().RenameArea(context.Background(), model.RenameAreaInput{Path: "web/dashboard", NewName: "ui"})
				return err
			},
		},
		{
			name: "a retire with members and no disposition",
			setup: func(t *testing.T, core *nibcore.Core) {
				mustCreate(t, core, &nib.Nib{ID: "np1", Title: "Member", Type: "task", Status: "todo", Area: "web"})
			},
			call: func(r *Resolver) error {
				_, err := r.Mutation().RemoveArea(context.Background(), model.RemoveAreaInput{Path: "web"})
				return err
			},
		},
		{
			name: "a disposition with nothing to dispose of",
			call: func(r *Resolver) error {
				_, err := r.Mutation().RemoveArea(context.Background(), model.RemoveAreaInput{Path: "web", Unassign: &unassign})
				return err
			},
		},
		{
			// The planner's refusal, which used to bake the absolute areas.yml
			// path into its own text.
			name: "a vocabulary these edits cannot address",
			setup: func(t *testing.T, core *nibcore.Core) {
				if err := os.WriteFile(store.NewLayout(core.Root()).AreasPath(), []byte(
					"defaults: &d\n    description: shared\nareas:\n    - name: web\n      <<: *d\n"), 0644); err != nil {
					t.Fatal(err)
				}
			},
			call: func(r *Resolver) error {
				_, err := r.Mutation().RenameArea(context.Background(), model.RenameAreaInput{Path: "web", NewName: "platform"})
				return err
			},
		},
		{
			name: "a store declaring no areas at all",
			setup: func(t *testing.T, core *nibcore.Core) {
				if err := os.WriteFile(store.NewLayout(core.Root()).AreasPath(), []byte("areas: []\n"), 0644); err != nil {
					t.Fatal(err)
				}
			},
			call: func(r *Resolver) error {
				_, err := r.Mutation().RemoveArea(context.Background(), model.RemoveAreaInput{Path: "web"})
				return err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver, core := setupTestResolverWithAreas(t)
			if tt.setup != nil {
				tt.setup(t, core)
			}

			err := tt.call(resolver)
			if err == nil {
				t.Fatal("the mutation was accepted, so no refusal is under test")
			}
			if strings.Contains(err.Error(), core.Root()) {
				t.Errorf("error = %q names the store root %s, which reaches an HTTP client verbatim", err.Error(), core.Root())
			}
			// The temp directory the store sits in, in case a message names the
			// project rather than the store.
			if parent := filepath.Dir(core.Root()); strings.Contains(err.Error(), parent) {
				t.Errorf("error = %q names the project directory %s", err.Error(), parent)
			}
		})
	}
}
