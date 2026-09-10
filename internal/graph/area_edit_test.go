package graph

import (
	"context"
	"errors"
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

// TestRenameAreaCascadesToMembers is the guard on step 4 of the sequence: the
// member rewrite runs BETWEEN the plan and the config write, through
// AreaEditor.RewriteAreaAssignments — the one primitive that takes the store
// lock as proof rather than acquiring it, which is what keeps a resolver already
// holding the per-descriptor flock from deadlocking on itself.
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
	var ioErr *AreaEditIOError
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
	var ioErr *AreaEditIOError
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
			var ioErr *AreaEditIOError
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
	var ioErr *AreaEditIOError
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
			name:    "moveTo names the area being retired",
			members: map[string]string{"dr4": "web"},
			input:   model.RemoveAreaInput{Path: "web", MoveTo: strptr("web")},
			want:    []string{"web"},
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
			var ioErr *AreaEditIOError
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

// areaEditorLoadFailure is the AreaEditor role over a real store with the
// re-read under the lock made to fail. Everything else — the lock, the
// vocabulary, the cascade — is the store's own, so only the branch under test
// differs from an ordinary run.
type areaEditorLoadFailure struct {
	*nibcore.Core
	err error
}

func (e areaEditorLoadFailure) Load() error { return e.err }

// TestAreaEditNamesTheHalfOfTheReloadThatFailed: beginAreaEdit re-reads the
// whole store, and Core.Load is two passes over two different files — the
// vocabulary, then the nib walk. A message naming the vocabulary for a walk
// failure sends the reader to the wrong file.
func TestAreaEditNamesTheHalfOfTheReloadThatFailed(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		want     string
		unwanted string
	}{
		{
			name:     "the vocabulary half",
			err:      &nibcore.AreasLoadError{Cause: errors.New("boom")},
			want:     "areas vocabulary",
			unwanted: "this store's nibs",
		},
		{
			name:     "the nib walk",
			err:      errors.New("boom"),
			want:     "this store's nibs",
			unwanted: "areas vocabulary",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver, core := setupTestResolverWithAreas(t)
			resolver.AreaEditor = areaEditorLoadFailure{Core: core, err: tt.err}
			before := storeSnapshot(t, core.Root())

			_, err := resolver.Mutation().RenameArea(context.Background(), model.RenameAreaInput{Path: "web", NewName: "platform"})
			if err == nil {
				t.Fatal("RenameArea reported success over a store it could not re-read")
			}
			var ioErr *AreaEditIOError
			if !errors.As(err, &ioErr) {
				t.Errorf("error = %v (%T), want the IO class", err, err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want substring %q", err.Error(), tt.want)
			}
			if strings.Contains(err.Error(), tt.unwanted) {
				t.Errorf("error = %q, want it not to blame %q", err.Error(), tt.unwanted)
			}
			assertStoreUnchanged(t, before, core.Root())
		})
	}
}

// areaEditorReloadFailure is the AreaEditor role over a real store with the
// re-read of the file the edit just WROTE made to fail. Every other step —
// including both writes — is the store's own, so the edit really does land
// before the failure under test.
type areaEditorReloadFailure struct {
	*nibcore.Core
	err error
}

func (e areaEditorReloadFailure) ReloadAreas() error { return e.err }

// TestAreaMutationsReportAFailedReload: Core keeps the vocabulary it could read
// when a reload fails, and that is the one the edit replaced — so a mutation
// that ignored the failure would answer with the pre-edit vocabulary and report
// success, which renders in a client as the edit not having happened.
func TestAreaMutationsReportAFailedReload(t *testing.T) {
	unassign := true
	tests := []struct {
		name string
		call func(*Resolver) (*model.Config, error)
	}{
		{
			name: "rename",
			call: func(r *Resolver) (*model.Config, error) {
				return r.Mutation().RenameArea(context.Background(), model.RenameAreaInput{Path: "web", NewName: "platform"})
			},
		},
		{
			name: "retire",
			call: func(r *Resolver) (*model.Config, error) {
				return r.Mutation().RemoveArea(context.Background(), model.RemoveAreaInput{Path: "web", Unassign: &unassign})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver, core := setupTestResolverWithAreas(t)
			mustCreate(t, core, &nib.Nib{ID: "rl1", Title: "Member", Type: "task", Status: "todo", Area: "web"})
			resolver.AreaEditor = areaEditorReloadFailure{Core: core, err: errors.New("areas.yml went missing")}

			cfg, err := tt.call(resolver)
			if err == nil {
				t.Fatalf("the mutation reported success and answered with %v", pathsOf(cfg.Areas))
			}
			var ioErr *AreaEditIOError
			if !errors.As(err, &ioErr) {
				t.Errorf("error = %v (%T), want the IO class", err, err)
			}
			if !strings.Contains(err.Error(), "nothing to rerun") {
				t.Errorf("error = %q, want it to say the edit needs no rerun", err.Error())
			}

			// The edit itself landed, which is what makes "nothing to rerun" the
			// right remedy: this is a stale reader, not a half-written store.
			if stored := storedAreasFile(t, core.Root()); strings.Contains(stored, "name: web") {
				t.Errorf("the edit did not reach disk, so the failure under test is not the reload:\n%s", stored)
			}
		})
	}
}
