package nibcore

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/fsutil"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/store"
	"github.com/alphaleonis/nibs/internal/testskip"
)

// areaVerbCore is setupAreaCore with members placed for the verbs to act on:
// one ON the node the tests rename and retire, one BELOW it, and one elsewhere.
func areaVerbCore(t *testing.T) (*Core, string) {
	t.Helper()
	core, nibsDir := setupAreaCore(t)
	for id, area := range map[string]string{
		"nibs-ae01": "web",
		"nibs-ae02": "web/ui",
		"nibs-ae03": "auth",
	} {
		if err := core.Create(&nib.Nib{ID: id, Title: "Placed " + id, Status: "todo", Area: area}); err != nil {
			t.Fatalf("creating %s: %v", id, err)
		}
	}
	return core, nibsDir
}

// storedAreasOf reads the vocabulary back OFF DISK, so an assertion judges what
// was persisted rather than what the in-memory copy holds.
func storedAreasOf(t *testing.T, nibsDir string) string {
	t.Helper()
	raw, err := os.ReadFile(store.NewLayout(nibsDir).AreasPath())
	if err != nil {
		t.Fatalf("reading the areas vocabulary: %v", err)
	}
	return string(raw)
}

// TestAreaVerbsReportWhatTheyDid pins the AreaEditResult each verb hands back.
// It is the whole channel a surface has for its success message, so a field left
// empty is a sentence that cannot be written rather than a wrong one.
func TestAreaVerbsReportWhatTheyDid(t *testing.T) {
	t.Run("rename", func(t *testing.T) {
		core, nibsDir := areaVerbCore(t)

		res, err := core.RenameArea("web", "platform")
		if err != nil {
			t.Fatalf("RenameArea: %v", err)
		}
		if res.NewPath != "platform" {
			t.Errorf("NewPath = %q, want %q", res.NewPath, "platform")
		}
		// Both members, in id order, and neither the nib on `auth` nor a
		// member counted twice.
		if want := []string{"nibs-ae01", "nibs-ae02"}; !slices.Equal(res.Members, want) {
			t.Errorf("Members = %v, want %v", res.Members, want)
		}
		if !slices.Equal(res.Written, res.Members) {
			t.Errorf("Written = %v, want every member %v", res.Written, res.Members)
		}
		// The vocabulary answered with is the one the edit WROTE, re-read under
		// the same lock — not the one it replaced.
		if !res.Areas.IsValid("platform/ui") || res.Areas.IsValid("web") {
			t.Errorf("Areas = %v, want the post-edit vocabulary", res.Areas.Paths())
		}
		if stored := storedAreasOf(t, nibsDir); strings.Contains(stored, "name: web") {
			t.Errorf("areas.yml still declares the old name:\n%s", stored)
		}
		// A member BELOW the renamed node keeps the remainder it carried.
		if got := core.Areas(); !got.IsValid("platform/ui") {
			t.Errorf("the store's vocabulary was not reloaded: %v", got.Paths())
		}
		if b, _ := core.Get("nibs-ae02"); b.Area != "platform/ui" {
			t.Errorf("nibs-ae02 area = %q, want platform/ui", b.Area)
		}
	})

	t.Run("retire", func(t *testing.T) {
		core, _ := areaVerbCore(t)

		res, err := core.RemoveArea("web", MoveAreaMembersTo("auth"))
		if err != nil {
			t.Fatalf("RemoveArea: %v", err)
		}
		if want := []string{"nibs-ae01", "nibs-ae02"}; !slices.Equal(res.Members, want) {
			t.Errorf("Members = %v, want %v", res.Members, want)
		}
		// `dashboard` and `ui` went with their parent, and the count is of
		// DECLARATIONS rather than of the nibs assigned to them.
		if res.DeclaredBelow != 2 {
			t.Errorf("DeclaredBelow = %d, want 2", res.DeclaredBelow)
		}
		if res.Areas.IsValid("web") || !res.Areas.IsValid("auth") {
			t.Errorf("Areas = %v, want web retired and auth kept", res.Areas.Paths())
		}
		for _, id := range res.Written {
			if b, _ := core.Get(id); b.Area != "auth" {
				t.Errorf("%s area = %q, want auth", id, b.Area)
			}
		}
	})

	t.Run("add", func(t *testing.T) {
		core, _ := areaVerbCore(t)

		res, err := core.AddArea("web/reports", "Charts", "teal")
		if err != nil {
			t.Fatalf("AddArea: %v", err)
		}
		if !res.Areas.IsValid("web/reports") {
			t.Errorf("Areas = %v, want it to declare web/reports", res.Areas.Paths())
		}
		// A declaration rewrites nothing: an area that is being declared has no
		// members to cascade to, and a nib already CARRYING the path is repaired
		// by the declaration itself rather than by a write.
		if len(res.Written) != 0 || len(res.Members) != 0 {
			t.Errorf("Written = %v, Members = %v, want a declaration to rewrite nothing", res.Written, res.Members)
		}
		if node := res.Areas.Get("web/reports"); node == nil || node.Description != "Charts" || node.Color != "teal" {
			t.Errorf("the declared node did not keep its description and color: %+v", node)
		}
	})
}

// TestAreaEditNamesTheHalfOfTheReReadThatFailed: the re-read under the lock is
// two passes over two different files — the vocabulary, then the nib walk — and
// they have different repairs. A surface that named the vocabulary for a walk
// failure would send the reader to the wrong file, so the phase tells them apart.
func TestAreaEditNamesTheHalfOfTheReReadThatFailed(t *testing.T) {
	t.Run("the vocabulary half", func(t *testing.T) {
		core, nibsDir := areaVerbCore(t)
		// Two siblings with one name: the loader refuses it, and Core.Load reads
		// the vocabulary before it walks the nibs.
		if err := os.WriteFile(store.NewLayout(nibsDir).AreasPath(),
			[]byte("areas:\n    - name: web\n    - name: web\n"), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := core.RenameArea("web", "platform")
		var ioErr *AreaEditIOError
		if !errors.As(err, &ioErr) {
			t.Fatalf("error = %v (%T), want an *AreaEditIOError", err, err)
		}
		if ioErr.Phase != AreaEditPhaseLoadVocabulary {
			t.Errorf("Phase = %d, want AreaEditPhaseLoadVocabulary", ioErr.Phase)
		}
	})

	t.Run("the nib walk", func(t *testing.T) {
		core, nibsDir := areaVerbCore(t)
		deep := filepath.Join(store.NewLayout(nibsDir).DataDir(), "deep")
		if err := os.MkdirAll(deep, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(deep, 0); err != nil {
			testskip.Unavailable(t, testskip.UnreadablePaths, "os.Chmod(deep, 0): %v", err)
		}
		// Restored so the temp directory can be removed at the end of the test.
		defer func() { _ = os.Chmod(deep, 0o755) }()
		if _, err := os.ReadDir(deep); err == nil {
			testskip.Unavailable(t, testskip.UnreadablePaths, "this process reads a mode-000 directory anyway (running as root?)")
		}

		_, err := core.RenameArea("web", "platform")
		var ioErr *AreaEditIOError
		if !errors.As(err, &ioErr) {
			t.Fatalf("error = %v (%T), want an *AreaEditIOError", err, err)
		}
		if ioErr.Phase != AreaEditPhaseLoadNibs {
			t.Errorf("Phase = %d, want AreaEditPhaseLoadNibs", ioErr.Phase)
		}
		if stored := storedAreasOf(t, nibsDir); !strings.Contains(stored, "name: web") {
			t.Errorf("a refused edit rewrote the vocabulary:\n%s", stored)
		}
	})
}

// TestAreaEditReportsAFailedReload: loadAreasLocked keeps the vocabulary it could
// still read when the file cannot be read back, and that is the one the edit
// replaced — so an edit that ignored the failure would answer with the pre-edit
// vocabulary and call itself a success, which renders in a client as the edit
// not having happened.
//
// Both writes have landed by then, which is what makes this the one IO phase
// with nothing to rerun.
func TestAreaEditReportsAFailedReload(t *testing.T) {
	core, nibsDir := areaVerbCore(t)
	restore := reloadAreasAfterEdit
	reloadAreasAfterEdit = func(*Core) error { return errors.New("areas.yml went missing") }
	t.Cleanup(func() { reloadAreasAfterEdit = restore })

	_, err := core.RenameArea("web", "platform")
	var ioErr *AreaEditIOError
	if !errors.As(err, &ioErr) {
		t.Fatalf("error = %v (%T), want an *AreaEditIOError", err, err)
	}
	if ioErr.Phase != AreaEditPhaseReload {
		t.Errorf("Phase = %d, want AreaEditPhaseReload", ioErr.Phase)
	}
	// The edit itself landed, which is what makes "nothing to rerun" the right
	// remedy: this is a stale reader, not a half-written store.
	if stored := storedAreasOf(t, nibsDir); strings.Contains(stored, "name: web") {
		t.Errorf("the edit did not reach disk, so the failure under test is not the reload:\n%s", stored)
	}
	if b, _ := core.Get("nibs-ae02"); b.Area != "platform/ui" {
		t.Errorf("nibs-ae02 area = %q, want the cascade to have landed too", b.Area)
	}
}

// TestAreaEditReportsAReplacedSymlink pins the note an edit owes when the
// areas.yml it replaced was a link: the atomic write leaves a regular file
// behind it, and the old target still declares the pre-edit vocabulary — so
// whatever manages that target restores the old paths while every nib the
// cascade rewrote carries the new one.
func TestAreaEditReportsAReplacedSymlink(t *testing.T) {
	core, nibsDir := areaVerbCore(t)
	areasPath := store.NewLayout(nibsDir).AreasPath()
	target := filepath.Join(t.TempDir(), "areas.yml")
	raw, err := os.ReadFile(areasPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, raw, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(areasPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, areasPath); err != nil {
		testskip.SymlinkUnavailable(t, err)
	}

	res, err := core.RenameArea("web", "platform")
	if err != nil {
		t.Fatalf("RenameArea: %v", err)
	}
	if res.StaleLinkTarget != target {
		t.Errorf("StaleLinkTarget = %q, want %q", res.StaleLinkTarget, target)
	}
	// And an ordinary edit reports nothing, so the field above is the replacement
	// and not a value every edit carries.
	plain, err := core.RenameArea("platform", "web")
	if err != nil {
		t.Fatalf("RenameArea back: %v", err)
	}
	if plain.StaleLinkTarget != "" {
		t.Errorf("StaleLinkTarget = %q over a regular file, want empty", plain.StaleLinkTarget)
	}
}

// TestAreaEditRefusalsCarryTheirFacts is the policy table: every decision the
// three verbs make, and the typed refusal each one raises.
//
// The refusals carry FIELDS and no prescription, so what is asserted here is the
// classification and the facts a surface words its sentence from — never a
// sentence, which is the CLI's and the resolver's to own.
func TestAreaEditRefusalsCarryTheirFacts(t *testing.T) {
	tests := []struct {
		name string
		call func(*Core) error
		want func(*testing.T, error)
	}{
		{
			name: "renaming a path the store does not declare",
			call: func(c *Core) error { _, err := c.RenameArea("nosuch", "platform"); return err },
			want: func(t *testing.T, err error) {
				var e *AreaUndeclaredError
				if !errors.As(err, &e) {
					t.Fatalf("error = %v (%T), want *AreaUndeclaredError", err, err)
				}
				if e.Role != AreaPathRenamed || e.Path != "nosuch" {
					t.Errorf("Role/Path = %d/%q, want AreaPathRenamed/nosuch", e.Role, e.Path)
				}
				// The declared set travels with the refusal, because naming it is
				// the repair and only the vocabulary read under the lock has it.
				if !e.Areas.IsValid("web/dashboard") {
					t.Errorf("Areas = %v, want the vocabulary read under the lock", e.Areas.Paths())
				}
			},
		},
		{
			name: "renaming to the name it already has",
			call: func(c *Core) error { _, err := c.RenameArea("web", "web"); return err },
			want: func(t *testing.T, err error) {
				var e *AreaNameUnchangedError
				if !errors.As(err, &e) {
					t.Fatalf("error = %v (%T), want *AreaNameUnchangedError", err, err)
				}
				if e.Path != "web" || e.Name != "web" {
					t.Errorf("Path/Name = %q/%q, want web/web", e.Path, e.Name)
				}
			},
		},
		{
			name: "renaming onto a name a sibling holds",
			call: func(c *Core) error { _, err := c.RenameArea("web/dashboard", "ui"); return err },
			want: func(t *testing.T, err error) {
				var e *AreaNameTakenError
				if !errors.As(err, &e) {
					t.Fatalf("error = %v (%T), want *AreaNameTakenError", err, err)
				}
				// The sibling's FULL path, which is what a message has to quote:
				// "ui" alone names a node the caller never mentioned.
				if e.Sibling != "web/ui" {
					t.Errorf("Sibling = %q, want web/ui", e.Sibling)
				}
			},
		},
		{
			name: "retiring an area work is assigned to",
			call: func(c *Core) error { _, err := c.RemoveArea("web", AreaDisposition{}); return err },
			want: func(t *testing.T, err error) {
				var e *AreaMembersPresentError
				if !errors.As(err, &e) {
					t.Fatalf("error = %v (%T), want *AreaMembersPresentError", err, err)
				}
				if want := []string{"nibs-ae01", "nibs-ae02"}; !slices.Equal(e.Members, want) {
					t.Errorf("Members = %v, want %v", e.Members, want)
				}
			},
		},
		{
			name: "disposing of members an area does not have",
			call: func(c *Core) error { _, err := c.RemoveArea("web/dashboard", UnassignAreaMembers()); return err },
			want: func(t *testing.T, err error) {
				var e *AreaDispositionEmptyError
				if !errors.As(err, &e) {
					t.Fatalf("error = %v (%T), want *AreaDispositionEmptyError", err, err)
				}
				// The disposition travels, because the message tells the caller
				// which argument to drop and the two spell it differently.
				if e.Disposition.Kind != AreaDispositionUnassign {
					t.Errorf("Disposition = %+v, want an unassign", e.Disposition)
				}
			},
		},
		{
			name: "moving members into the subtree being retired",
			call: func(c *Core) error { _, err := c.RemoveArea("web", MoveAreaMembersTo("web/ui")); return err },
			want: func(t *testing.T, err error) {
				var e *AreaMoveTargetWithinError
				if !errors.As(err, &e) {
					t.Fatalf("error = %v (%T), want *AreaMoveTargetWithinError", err, err)
				}
				if e.Target != "web/ui" || e.Path != "web" {
					t.Errorf("Target/Path = %q/%q, want web/ui and web", e.Target, e.Path)
				}
			},
		},
		{
			name: "moving members to an area the store does not declare",
			call: func(c *Core) error { _, err := c.RemoveArea("web", MoveAreaMembersTo("nosuch")); return err },
			want: func(t *testing.T, err error) {
				var e *AreaUndeclaredError
				if !errors.As(err, &e) {
					t.Fatalf("error = %v (%T), want *AreaUndeclaredError", err, err)
				}
				// The ROLE is what keeps a surface from wording this as "there is
				// no area to retire" over a path that is perfectly declared.
				if e.Role != AreaPathMoveTarget {
					t.Errorf("Role = %d, want AreaPathMoveTarget", e.Role)
				}
			},
		},
		{
			name: "declaring an area the store already declares",
			call: func(c *Core) error { _, err := c.AddArea("web/ui", "", ""); return err },
			want: func(t *testing.T, err error) {
				var e *AreaAlreadyDeclaredError
				if !errors.As(err, &e) {
					t.Fatalf("error = %v (%T), want *AreaAlreadyDeclaredError", err, err)
				}
				if e.Path != "web/ui" {
					t.Errorf("Path = %q, want web/ui", e.Path)
				}
			},
		},
		{
			name: "declaring an area under a parent the store does not declare",
			call: func(c *Core) error { _, err := c.AddArea("nosuch/child", "", ""); return err },
			want: func(t *testing.T, err error) {
				var e *AreaParentUndeclaredError
				if !errors.As(err, &e) {
					t.Fatalf("error = %v (%T), want *AreaParentUndeclaredError", err, err)
				}
				if e.Parent != "nosuch" || e.Path != "nosuch/child" {
					t.Errorf("Parent/Path = %q/%q, want nosuch and nosuch/child", e.Parent, e.Path)
				}
			},
		},
		{
			name: "a name the edited vocabulary could not hold",
			call: func(c *Core) error { _, err := c.RenameArea("web", "  "); return err },
			want: func(t *testing.T, err error) {
				// The planner's own refusal, passed through unwrapped: it is about
				// the file's CONTENT, which is the class every surface reports it
				// as, and wrapping it would hide the type they classify on.
				var e *config.AreaEditRefusal
				if !errors.As(err, &e) {
					t.Fatalf("error = %v (%T), want *config.AreaEditRefusal", err, err)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, nibsDir := areaVerbCore(t)
			before := storedAreasOf(t, nibsDir)
			areasBefore := map[string]string{}
			for _, b := range core.All() {
				areasBefore[b.ID] = b.Area
			}

			err := tt.call(core)
			if err == nil {
				t.Fatal("the verb was accepted")
			}
			tt.want(t, err)

			// Every refusal above lands before the first write, which is what
			// makes it a refusal rather than a partial edit.
			var ioErr *AreaEditIOError
			if errors.As(err, &ioErr) {
				t.Errorf("error = %v, want a refusal rather than the IO class", err)
			}
			if got := storedAreasOf(t, nibsDir); got != before {
				t.Errorf("a refused verb rewrote the vocabulary:\n%s", got)
			}
			for _, b := range core.All() {
				if areasBefore[b.ID] != b.Area {
					t.Errorf("a refused verb rewrote %s: %q -> %q", b.ID, areasBefore[b.ID], b.Area)
				}
			}
		})
	}
}

// TestAreaEditTellsARetiredPathApartFromOneThatNeverExisted: another nibs
// process finishing a retire or a rename while this edit waited for the store's
// write lock leaves a path that WAS declared when the argument was given and is
// not declared now. Reported as an ordinary undeclared path it reads as a typo
// the caller did not make, and it classifies as bad input rather than as a store
// that moved.
func TestAreaEditTellsARetiredPathApartFromOneThatNeverExisted(t *testing.T) {
	tests := []struct {
		name string
		call func(*Core) error
		role AreaPathRole
	}{
		{
			name: "the node being retired",
			call: func(c *Core) error { _, err := c.RemoveArea("auth", UnassignAreaMembers()); return err },
			role: AreaPathRetired,
		},
		{
			name: "the node being renamed",
			call: func(c *Core) error { _, err := c.RenameArea("auth", "identity"); return err },
			role: AreaPathRenamed,
		},
		{
			name: "a move target",
			call: func(c *Core) error { _, err := c.RemoveArea("web", MoveAreaMembersTo("auth")); return err },
			role: AreaPathMoveTarget,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, nibsDir := areaVerbCore(t)
			// The store this process loaded declares `auth`. Another writer
			// retires it while this edit is still holding that vocabulary — the
			// state a blocking wait on the store's write lock leaves behind.
			if err := os.WriteFile(store.NewLayout(nibsDir).AreasPath(),
				[]byte("areas:\n    - name: web\n      children:\n        - name: dashboard\n        - name: ui\n"), 0644); err != nil {
				t.Fatal(err)
			}

			err := tt.call(core)
			var e *AreaRetiredWhileWaitingError
			if !errors.As(err, &e) {
				t.Fatalf("error = %v (%T), want *AreaRetiredWhileWaitingError", err, err)
			}
			if e.Path != "auth" {
				t.Errorf("Path = %q, want auth", e.Path)
			}
			if e.Role != tt.role {
				t.Errorf("Role = %d, want %d", e.Role, tt.role)
			}
		})
	}
}

// TestAreaEditRefusesAVanishedVocabulary: an areas.yml that existed when this
// store was last read and does not exist now is a store to repair, not an
// argument to fix.
//
// It takes BOTH halves to say so. A store that NEVER had one reaches the same
// empty vocabulary and is a legitimate shape — refusing it would name a file to
// restore that never existed — so that case falls through to the ordinary
// undeclared-path refusal.
func TestAreaEditRefusesAVanishedVocabulary(t *testing.T) {
	core, nibsDir := areaVerbCore(t)
	areasPath := store.NewLayout(nibsDir).AreasPath()
	if err := os.Remove(areasPath); err != nil {
		t.Fatal(err)
	}

	_, err := core.RenameArea("web", "platform")
	var vanished *AreaVocabularyVanishedError
	if !errors.As(err, &vanished) {
		t.Fatalf("error = %v (%T), want *AreaVocabularyVanishedError", err, err)
	}
	if vanished.File != areasPath {
		t.Errorf("File = %q, want %q", vanished.File, areasPath)
	}

	// A store that never had one: the same empty vocabulary, and the ordinary
	// refusal, whose "declares no areas" is both the true cause and the remedy.
	fresh, _ := setupTestCore(t)
	_, err = fresh.RenameArea("web", "platform")
	var undeclared *AreaUndeclaredError
	if !errors.As(err, &undeclared) {
		t.Fatalf("error = %v (%T), want *AreaUndeclaredError over a store that never declared one", err, err)
	}
	if undeclared.Areas.Declared() {
		t.Errorf("Areas = %v, want a vocabulary declaring nothing", undeclared.Areas.Paths())
	}
}

// TestAreaEditPartialFailureIsRerunnable pins the claim both surfaces' messages
// make about a cascade that stopped part way: the writes already made stay, the
// vocabulary is untouched, and rerunning finishes the job because a nib already
// rewritten is no longer a member.
func TestAreaEditPartialFailureIsRerunnable(t *testing.T) {
	core, nibsDir := areaVerbCore(t)

	// Fail the SECOND member's rename, so the first is durably written and the
	// run aborts with the vocabulary still declaring the old path.
	restore := fsutil.RenameFn
	calls := 0
	fsutil.RenameFn = func(oldpath, newpath string) error {
		calls++
		if calls == 2 {
			return errors.New("simulated crash")
		}
		return os.Rename(oldpath, newpath)
	}
	t.Cleanup(func() { fsutil.RenameFn = restore })

	_, err := core.RenameArea("web", "platform")
	var ioErr *AreaEditIOError
	if !errors.As(err, &ioErr) {
		t.Fatalf("error = %v (%T), want an *AreaEditIOError", err, err)
	}
	if ioErr.Phase != AreaEditPhaseCascade {
		t.Fatalf("Phase = %d, want AreaEditPhaseCascade", ioErr.Phase)
	}
	// What the message reports: what it wrote, and what it set out to write.
	if want := []string{"nibs-ae01"}; !slices.Equal(ioErr.Written, want) {
		t.Errorf("Written = %v, want %v", ioErr.Written, want)
	}
	if want := []string{"nibs-ae01", "nibs-ae02"}; !slices.Equal(ioErr.Members, want) {
		t.Errorf("Members = %v, want %v — the set read BEFORE the cascade", ioErr.Members, want)
	}
	if stored := storedAreasOf(t, nibsDir); !strings.Contains(stored, "name: web") {
		t.Errorf("the vocabulary was rewritten by a failed cascade:\n%s", stored)
	}

	fsutil.RenameFn = restore
	res, err := core.RenameArea("web", "platform")
	if err != nil {
		t.Fatalf("the rerun the message prescribes failed: %v", err)
	}
	// The rerun's member set is exactly the unwritten remainder: the nib the
	// first run rewrote is no longer inside `web`.
	if want := []string{"nibs-ae02"}; !slices.Equal(res.Members, want) {
		t.Errorf("the rerun's Members = %v, want %v", res.Members, want)
	}
	for id, want := range map[string]string{"nibs-ae01": "platform", "nibs-ae02": "platform/ui", "nibs-ae03": "auth"} {
		if b, _ := core.Get(id); b.Area != want {
			t.Errorf("%s area = %q, want %q", id, b.Area, want)
		}
	}
}

// landNibOnDisk writes a nib file straight into the store's data directory, the
// way a `git pull` in .nibs does: nothing in this process is told, so the store
// learns of it only by reading the directory again.
func landNibOnDisk(t *testing.T, nibsDir, id, area string) {
	t.Helper()
	b := &nib.Nib{
		ID: id, Version: nib.CurrentVersion, Title: "Arrived " + id,
		Status: "todo", Type: "task", Area: area, Path: "data/" + id + ".md",
	}
	rendered, err := b.Render()
	if err != nil {
		t.Fatalf("rendering %s: %v", id, err)
	}
	if err := os.WriteFile(filepath.Join(nibsDir, filepath.FromSlash(b.Path)), rendered, 0o644); err != nil {
		t.Fatalf("landing %s: %v", id, err)
	}
}

// TestAreaEditRefusesToStrandANibThatArrived: a verb decides an area's
// membership from the nibs this process holds, and the store's write lock keeps
// out another nibs process but not a `git pull` in .nibs. A file landing after
// the plan's re-read is in no set the cascade walked, so a vocabulary write that
// went ahead would retire a declaration that file still carries — and every
// write to the nib is refused from then on, with both surfaces reporting
// success and nothing saying to rerun.
//
// The arrival is timed by fsutil.RenameFn, which fires as the cascade renames
// its first member into place: strictly after the plan's re-read and strictly
// before the vocabulary write, with nothing slept on or polled for.
func TestAreaEditRefusesToStrandANibThatArrived(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Core) (AreaEditResult, error)
		// area is what the arrived nib carries once the rerun has finished the
		// job the first run refused.
		area string
		// cascaded is the area the refused edit left nibs-ae02 — the member
		// BELOW the node the verb was given — carrying, and it is what decides
		// stranded: the vocabulary declares "auth" and accepts the cleared value,
		// and does not declare "platform/ui" until the rerun.
		cascaded string
		// stranded is whether the refusal leaves the members the cascade already
		// rewrote unwritable.
		stranded bool
	}{
		{
			name:     "rename",
			edit:     func(c *Core) (AreaEditResult, error) { return c.RenameArea("web", "platform") },
			area:     "platform",
			cascaded: "platform/ui",
			stranded: true,
		},
		{
			name: "retire, unassigning",
			edit: func(c *Core) (AreaEditResult, error) { return c.RemoveArea("web", UnassignAreaMembers()) },
			area: "",
		},
		{
			// The row the cleared value cannot carry: a moved member keeps a
			// non-empty area, so the write below answers from the vocabulary
			// rather than from the empty-path fast path both validators take.
			name:     "retire, moving members",
			edit:     func(c *Core) (AreaEditResult, error) { return c.RemoveArea("web", MoveAreaMembersTo("auth")) },
			area:     "auth",
			cascaded: "auth",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, nibsDir := areaVerbCore(t)

			restore := fsutil.RenameFn
			landed := false
			fsutil.RenameFn = func(oldpath, newpath string) error {
				if !landed && strings.HasSuffix(newpath, ".md") {
					landed = true
					landNibOnDisk(t, nibsDir, "nibs-ae04", "web")
				}
				return restore(oldpath, newpath)
			}
			t.Cleanup(func() { fsutil.RenameFn = restore })

			_, err := tt.edit(core)
			if !landed {
				t.Fatal("no nib arrived inside the window, so this proves nothing")
			}
			var arrived *AreaMembersArrivedError
			if !errors.As(err, &arrived) {
				t.Fatalf("error = %v (%T), want an *AreaMembersArrivedError", err, err)
			}
			if want := []string{"nibs-ae04"}; !slices.Equal(arrived.Members, want) {
				t.Errorf("Members = %v, want %v — the set the vocabulary write would have stranded", arrived.Members, want)
			}
			if want := []string{"nibs-ae01", "nibs-ae02"}; !slices.Equal(arrived.Written, want) {
				t.Errorf("Written = %v, want %v — the cascade's writes are durable and a surface must be able to say so", arrived.Written, want)
			}

			// The recoverable half: the declaration is still on disk, so the nib
			// that arrived under it is still writable.
			if stored := storedAreasOf(t, nibsDir); !strings.Contains(stored, "name: web") {
				t.Fatalf("the refused edit rewrote the vocabulary anyway:\n%s", stored)
			}
			b, err := core.Get("nibs-ae04")
			if err != nil {
				t.Fatalf("the refusing edit did not install the nib it read: %v", err)
			}
			b.Title = "Still writable"
			if err := core.Update(b, nil); err != nil {
				t.Errorf("Update of the arrived nib = %v, want nil: refusing is only the better answer if it leaves the area declared", err)
			}

			// The half it costs, which NewPath is what a surface says it with: a
			// rename's cascaded members are on a path nothing declares until the
			// rerun, so every write to them is refused meanwhile.
			if (arrived.NewPath != "") != tt.stranded {
				t.Errorf("NewPath = %q, want it set only where the cascade left its members undeclared", arrived.NewPath)
			}
			moved, err := core.Get("nibs-ae02")
			if err != nil {
				t.Fatalf("Get of a cascaded member: %v", err)
			}
			if moved.Area != tt.cascaded {
				t.Errorf("the cascade left nibs-ae02 on %q, want %q", moved.Area, tt.cascaded)
			}
			moved.Title = "Cascaded"
			if err := core.Update(moved, nil); (err != nil) != tt.stranded {
				t.Errorf("Update of a cascaded member = %v, want refused = %v", err, tt.stranded)
			}

			fsutil.RenameFn = restore
			if _, err := tt.edit(core); err != nil {
				t.Fatalf("the rerun failed: %v", err)
			}
			for id, want := range map[string]string{
				"nibs-ae01": tt.area, "nibs-ae02": tt.cascaded, "nibs-ae04": tt.area, "nibs-ae03": "auth"} {
				if got, _ := core.Get(id); got.Area != want {
					t.Errorf("%s area = %q after the rerun, want %q", id, got.Area, want)
				}
			}
		})
	}
}

// TestARetireWithNoMembersConfirmsBeforeItWrites is the same refusal for the
// shape that cascades nothing: `web` has no members when the plan reads it, so
// no nib is rewritten and no rename fires between that read and the vocabulary
// write. The arrival is landed through the confirming re-read itself, which is
// the only event left in the window — and an edit that did not look would not
// call it.
func TestARetireWithNoMembersConfirmsBeforeItWrites(t *testing.T) {
	core, nibsDir := setupAreaCore(t)

	restore := reloadNibsBeforeAreaWrite
	landed := false
	reloadNibsBeforeAreaWrite = func(c *Core) error {
		if !landed {
			landed = true
			landNibOnDisk(t, nibsDir, "nibs-ae04", "web")
		}
		return restore(c)
	}
	t.Cleanup(func() { reloadNibsBeforeAreaWrite = restore })

	_, err := core.RemoveArea("web", AreaDisposition{})
	if !landed {
		t.Fatal("the edit never re-read the store, so nothing arrived inside the window")
	}
	var arrived *AreaMembersArrivedError
	if !errors.As(err, &arrived) {
		t.Fatalf("error = %v (%T), want an *AreaMembersArrivedError", err, err)
	}
	if len(arrived.Written) != 0 {
		t.Errorf("Written = %v, want none: an empty area has no members to rewrite", arrived.Written)
	}
	if stored := storedAreasOf(t, nibsDir); !strings.Contains(stored, "name: web") {
		t.Errorf("the refused retire rewrote the vocabulary anyway:\n%s", stored)
	}

	// The rerun now sees the arrival as an ordinary member, which is the refusal
	// a caller can act on: a retire with no disposition for work that exists.
	_, err = core.RemoveArea("web", AreaDisposition{})
	var present *AreaMembersPresentError
	if !errors.As(err, &present) {
		t.Fatalf("the rerun's error = %v (%T), want an *AreaMembersPresentError", err, err)
	}
}

// TestAreaEditReportsAFailedConfirmation: the confirming re-read is a walk of
// every nib file in the store, so it can fail on the filesystem like the first
// one — but by then the cascade is durable and the vocabulary is not written, so
// it is neither of the phases that already have a sentence.
//
// The rows are the two shapes the surfaces have to word it in, and the fields
// are what they word it FROM: a rename past its cascade, and a retire of an area
// nothing is assigned to — which is the only retire admitted without a
// disposition, so it reaches this phase having rewritten nothing.
func TestAreaEditReportsAFailedConfirmation(t *testing.T) {
	tests := []struct {
		name    string
		edit    func(*Core) (AreaEditResult, error)
		newPath string
		written []string
		// declared is the vocabulary line the refused edit must have left alone.
		declared string
	}{
		{
			name:     "a rename",
			edit:     func(c *Core) (AreaEditResult, error) { return c.RenameArea("web", "platform") },
			newPath:  "platform",
			written:  []string{"nibs-ae01", "nibs-ae02"},
			declared: "name: web",
		},
		{
			name:     "a retire with no disposition",
			edit:     func(c *Core) (AreaEditResult, error) { return c.RemoveArea("web/dashboard", AreaDisposition{}) },
			declared: "name: dashboard",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, nibsDir := areaVerbCore(t)

			restore := reloadNibsBeforeAreaWrite
			reloadNibsBeforeAreaWrite = func(*Core) error { return errors.New("the store went unreadable") }
			t.Cleanup(func() { reloadNibsBeforeAreaWrite = restore })

			_, err := tt.edit(core)
			var ioErr *AreaEditIOError
			if !errors.As(err, &ioErr) {
				t.Fatalf("error = %v (%T), want an *AreaEditIOError", err, err)
			}
			if ioErr.Phase != AreaEditPhaseConfirm {
				t.Errorf("Phase = %d, want AreaEditPhaseConfirm", ioErr.Phase)
			}
			if ioErr.NewPath != tt.newPath {
				t.Errorf("NewPath = %q, want %q", ioErr.NewPath, tt.newPath)
			}
			if !slices.Equal(ioErr.Written, tt.written) {
				t.Errorf("Written = %v, want %v", ioErr.Written, tt.written)
			}
			if ioErr.Disposition.Kind != AreaDispositionNone {
				t.Errorf("Disposition.Kind = %v, want none — neither row named one", ioErr.Disposition.Kind)
			}
			if stored := storedAreasOf(t, nibsDir); !strings.Contains(stored, tt.declared) {
				t.Errorf("the vocabulary was rewritten by an edit that could not confirm it:\n%s", stored)
			}
		})
	}
}
