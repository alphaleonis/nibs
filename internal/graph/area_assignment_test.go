package graph

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/99designs/gqlgen/graphql"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/store"
)

// setupTestResolverWithAreas is setupTestResolver over a config that DECLARES a
// vocabulary. config.Default() declares none, and a store with no areas refuses
// every assignment, so the accepting rows need their own fixture.
func setupTestResolverWithAreas(t *testing.T) (*Resolver, *nibcore.Core) {
	t.Helper()
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, store.DirName)
	if err := os.MkdirAll(store.NewLayout(nibsDir).DataDir(), 0755); err != nil {
		t.Fatalf("failed to create test .nibs dir: %v", err)
	}

	if err := os.WriteFile(store.NewLayout(nibsDir).AreasPath(), []byte(
		"areas:\n    - name: web\n      children:\n        - name: dashboard\n        - name: ui\n    - name: auth\n"), 0644); err != nil {
		t.Fatalf("writing the areas vocabulary: %v", err)
	}
	core := nibcore.New(nibsDir, config.Default())
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("failed to load core: %v", err)
	}
	return &Resolver{Reader: core, Writer: core, Validator: core, Blocking: core, Orderer: NewOrderer(core, core)}, core
}

func areaInput(path string) model.UpdateNibInput {
	return model.UpdateNibInput{Area: graphql.OmittableOf(&path)}
}

// TestUpdateNibAreaAssignment pins the write half of the ownership axis on the
// one input every client reaches — the CLI, the TUI and any MCP client ride
// these resolvers.
func TestUpdateNibAreaAssignment(t *testing.T) {
	ctx := context.Background()

	t.Run("assigns a declared path", func(t *testing.T) {
		resolver, core := setupTestResolverWithAreas(t)
		mustCreate(t, core, &nib.Nib{ID: "ar1", Title: "Work", Type: "task", Status: "todo"})
		got, err := resolver.Mutation().UpdateNib(ctx, "ar1", areaInput("web/dashboard"))
		if err != nil {
			t.Fatalf("UpdateNib(area): %v", err)
		}
		if got.Area != "web/dashboard" {
			t.Errorf("Area = %q, want web/dashboard", got.Area)
		}
	})

	t.Run("refuses an undeclared path naming the vocabulary", func(t *testing.T) {
		resolver, core := setupTestResolverWithAreas(t)
		mustCreate(t, core, &nib.Nib{ID: "ar2", Title: "Work", Type: "task", Status: "todo", Area: "auth"})
		_, err := resolver.Mutation().UpdateNib(ctx, "ar2", areaInput("nosuch"))
		if err == nil {
			t.Fatal("UpdateNib accepted an undeclared area")
		}
		for _, want := range []string{"nosuch", "web/dashboard"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error = %q, want substring %q", err.Error(), want)
			}
		}
		if stored, _ := core.GetSnapshot("ar2"); stored.Area != "auth" {
			t.Errorf("refused update changed the stored area to %q", stored.Area)
		}
	})

	t.Run("a store with no declared areas says so", func(t *testing.T) {
		resolver, core := setupTestResolver(t)
		mustCreate(t, core, &nib.Nib{ID: "ar3", Title: "Work", Type: "task", Status: "todo"})
		_, err := resolver.Mutation().UpdateNib(ctx, "ar3", areaInput("web"))
		if err == nil {
			t.Fatal("UpdateNib accepted an area against a store that declares none")
		}
		if !strings.Contains(err.Error(), "declares no areas") {
			t.Errorf("error = %q, want it to say the store declares no areas", err.Error())
		}
	})

	// The three-way wire reading, which is the milestone field's shape minus the
	// queue: omitted leaves the value alone, null and "" both clear it.
	t.Run("an explicit null clears", func(t *testing.T) {
		resolver, core := setupTestResolverWithAreas(t)
		mustCreate(t, core, &nib.Nib{ID: "ar4", Title: "Work", Type: "task", Status: "todo", Area: "auth"})
		got, err := resolver.Mutation().UpdateNib(ctx, "ar4", model.UpdateNibInput{Area: graphql.OmittableOf[*string](nil)})
		if err != nil {
			t.Fatalf("UpdateNib(area=null): %v", err)
		}
		if got.Area != "" {
			t.Errorf("Area = %q, want it cleared", got.Area)
		}
	})

	t.Run("an empty string clears", func(t *testing.T) {
		resolver, core := setupTestResolverWithAreas(t)
		mustCreate(t, core, &nib.Nib{ID: "ar5", Title: "Work", Type: "task", Status: "todo", Area: "auth"})
		got, err := resolver.Mutation().UpdateNib(ctx, "ar5", areaInput(""))
		if err != nil {
			t.Fatalf(`UpdateNib(area=""): %v`, err)
		}
		if got.Area != "" {
			t.Errorf("Area = %q, want it cleared", got.Area)
		}
	})

	t.Run("omitting the field leaves the assignment alone", func(t *testing.T) {
		resolver, core := setupTestResolverWithAreas(t)
		mustCreate(t, core, &nib.Nib{ID: "ar6", Title: "Work", Type: "task", Status: "todo", Area: "auth"})
		got, err := resolver.Mutation().UpdateNib(ctx, "ar6", model.UpdateNibInput{Title: stringPtr("Renamed")})
		if err != nil {
			t.Fatalf("UpdateNib(title): %v", err)
		}
		if got.Area != "auth" {
			t.Errorf("Area = %q, want auth", got.Area)
		}
	})

	// The axis rule answers first because no area value can satisfy it: a
	// milestone takes no area at all, so "must be one of …" would prescribe a
	// remedy the subject cannot follow.
	t.Run("a milestone subject gets the axis refusal, not the vocabulary one", func(t *testing.T) {
		resolver, core := setupTestResolverWithAreas(t)
		mustCreate(t, core, &nib.Nib{ID: "arms", Title: "Waypoint", Type: "milestone", Status: "todo"})
		_, err := resolver.Mutation().UpdateNib(ctx, "arms", areaInput("nosuch"))
		if err == nil {
			t.Fatal("UpdateNib accepted an area on a milestone")
		}
		if !strings.Contains(err.Error(), "cannot have an area") {
			t.Errorf("error = %q, want the axis refusal", err.Error())
		}
	})
}

// TestUpdateNibRepairsAnUndeclaredArea is the flip side of read-tolerance. A
// file carrying an undeclared area loads, and every write of that nib is then
// refused — so the clear has to reach the guard as the state the request LEAVES,
// not as the stale value it starts from, or the one input that repairs the nib
// would be refused by the rule it exists to satisfy.
func TestUpdateNibRepairsAnUndeclaredArea(t *testing.T) {
	ctx := context.Background()

	t.Run("clearing it", func(t *testing.T) {
		resolver, core := setupTestResolverWithAreas(t)
		mustCreate(t, core, &nib.Nib{ID: "arr1", Title: "Work", Type: "task", Status: "todo", Area: "auth"})
		stampUndeclaredArea(t, core, "arr1")
		got, err := resolver.Mutation().UpdateNib(ctx, "arr1", model.UpdateNibInput{Area: graphql.OmittableOf[*string](nil)})
		if err != nil {
			t.Fatalf("UpdateNib(area=null) on an undeclared area: %v", err)
		}
		if got.Area != "" {
			t.Errorf("Area = %q, want it cleared", got.Area)
		}
	})

	t.Run("reassigning it to a declared path", func(t *testing.T) {
		resolver, core := setupTestResolverWithAreas(t)
		mustCreate(t, core, &nib.Nib{ID: "arr2", Title: "Work", Type: "task", Status: "todo", Area: "auth"})
		stampUndeclaredArea(t, core, "arr2")
		got, err := resolver.Mutation().UpdateNib(ctx, "arr2", areaInput("web"))
		if err != nil {
			t.Fatalf("UpdateNib(area=web) on an undeclared area: %v", err)
		}
		if got.Area != "web" {
			t.Errorf("Area = %q, want web", got.Area)
		}
	})

	t.Run("an unrelated edit is refused until the area is repaired", func(t *testing.T) {
		resolver, core := setupTestResolverWithAreas(t)
		mustCreate(t, core, &nib.Nib{ID: "arr3", Title: "Work", Type: "task", Status: "todo", Area: "auth"})
		stampUndeclaredArea(t, core, "arr3")
		_, err := resolver.Mutation().UpdateNib(ctx, "arr3", model.UpdateNibInput{Title: stringPtr("Renamed")})
		if err == nil {
			t.Fatal("UpdateNib accepted an edit of a nib carrying an undeclared area")
		}
		if !strings.Contains(err.Error(), "retired/thing") {
			t.Errorf("error = %q, want it to name the stored value", err.Error())
		}
	})
}

// stampUndeclaredArea rewrites a stored nib's `area:` to a path the vocabulary
// does not declare, the way a hand edit or a retired declaration leaves one, and
// reloads so the store holds it. It goes through the file rather than Update
// precisely because Update is what now refuses the value.
func stampUndeclaredArea(t *testing.T, core *nibcore.Core, id string) {
	t.Helper()
	b, ok := core.GetSnapshot(id)
	if !ok {
		t.Fatalf("nib %s is not in the store", id)
	}
	path := filepath.Join(core.Root(), b.Path)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", b.Path, err)
	}
	rewritten := strings.Replace(string(raw), "area: auth", "area: retired/thing", 1)
	if rewritten == string(raw) {
		t.Fatalf("no `area: auth` key to rewrite in %s:\n%s", b.Path, raw)
	}
	if err := os.WriteFile(path, []byte(rewritten), 0644); err != nil {
		t.Fatalf("writing %s: %v", b.Path, err)
	}
	if err := core.Load(); err != nil {
		t.Fatalf("reload: %v", err)
	}
}

// TestCreateNibArea pins the create half: work can be placed at creation,
// subject to the same rule.
func TestCreateNibArea(t *testing.T) {
	ctx := context.Background()

	t.Run("assigns a declared path", func(t *testing.T) {
		resolver, _ := setupTestResolverWithAreas(t)
		area := "web/dashboard"
		got, err := resolver.Mutation().CreateNib(ctx, model.CreateNibInput{Title: "Placed", Area: &area})
		if err != nil {
			t.Fatalf("CreateNib(area): %v", err)
		}
		if got.Area != "web/dashboard" {
			t.Errorf("Area = %q, want web/dashboard", got.Area)
		}
	})

	t.Run("refuses an undeclared path", func(t *testing.T) {
		resolver, _ := setupTestResolverWithAreas(t)
		area := "nosuch"
		_, err := resolver.Mutation().CreateNib(ctx, model.CreateNibInput{Title: "Placed", Area: &area})
		if err == nil {
			t.Fatal("CreateNib accepted an undeclared area")
		}
		if !strings.Contains(err.Error(), "nosuch") {
			t.Errorf("error = %q, want it to name the refused value", err.Error())
		}
	})
}

// retireStoredArea gives a nib an `area:` its store does not declare, by hand-
// editing the file and reloading — which is the only way such a value gets into
// a store, since every write path refuses it. This is the shape a retired or
// renamed `areas:` entry leaves behind.
func retireStoredArea(t *testing.T, core *nibcore.Core, id, area string) {
	t.Helper()
	b, err := core.Get(id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	path := core.FullPath(b)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(raw), "status: ", "area: "+area+"\nstatus: ", 1)
	if updated == string(raw) {
		t.Fatalf("could not place an area key in %s:\n%s", path, raw)
	}
	if err := os.WriteFile(path, []byte(updated), 0644); err != nil {
		t.Fatal(err)
	}
	if err := core.Load(); err != nil {
		t.Fatalf("reload: %v", err)
	}
}

// TestBackfillOrderKeysStaysQuietOnAnUndeclaredArea guards the read-path flood
// that Core.Update's area check opened up.
//
// Orderer.backfillKeys writes from the hot Children/root READ path to install a
// missing order key, and an unwritable sibling keeps Order == "" — so the write
// is re-attempted on EVERY read. A nib carrying an `area:` the vocabulary no
// longer declares is refused by that write permanently and identically, which
// made the warning re-appear on every read of the parent, forever, for a
// condition deliberately tolerated everywhere else (`nibs check` reports
// nothing for it, so there is nothing to act on either).
//
// It is a stable class exactly like the etag divergence the suppression list
// was built for, so it belongs in the same guard. The second subtest is what
// keeps the first from passing vacuously: on the same read path, a sibling the
// write DOES accept gets its key.
func TestBackfillOrderKeysStaysQuietOnAnUndeclaredArea(t *testing.T) {
	t.Run("an undeclared area is a stable refusal, so the read path stays quiet", func(t *testing.T) {
		r, core := setupTestResolverWithAreas(t)
		createTestNib(t, core, "area-bf1", "Unkeyed root", "todo")
		retireStoredArea(t, core, "area-bf1", "retired/team")

		stderr := captureStderr(t, func() {
			// Model repeated tree renders/polls: each one re-attempts the
			// backfill, because the refused write never persists a key.
			for i := 0; i < 3; i++ {
				_ = r.Orderer.Members(ScopeParent, "")
			}
		})
		if strings.TrimSpace(stderr) != "" {
			t.Errorf("a permanently refused backfill must not warn once per read; got %q", stderr)
		}
		got, err := r.Reader.Get("area-bf1")
		if err != nil {
			t.Fatalf("get area-bf1: %v", err)
		}
		if got.Order != "" {
			t.Errorf("shared nib left with phantom Order %q after a refused backfill write", got.Order)
		}
		if got.Area != "retired/team" {
			t.Errorf("Area = %q, want the undeclared value to survive the read untouched", got.Area)
		}
	})

	t.Run("the same read path still installs a key when the write is accepted", func(t *testing.T) {
		r, core := setupTestResolverWithAreas(t)
		createTestNib(t, core, "area-bf2", "Unkeyed root", "todo")

		_ = r.Orderer.Members(ScopeParent, "")

		got, err := r.Reader.Get("area-bf2")
		if err != nil {
			t.Fatalf("get area-bf2: %v", err)
		}
		if got.Order == "" {
			t.Fatal("the backfill path was never reached, so the quiet subtest above proves nothing")
		}
	})
}
