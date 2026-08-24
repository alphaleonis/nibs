package nibcore

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/store"
)

// setupAreaCore is setupTestCore over a config that DECLARES a vocabulary —
// config.Default() declares none, and a store with no areas refuses every
// assignment, so the accepting rows need their own fixture.
func setupAreaCore(t *testing.T) (*Core, string) {
	t.Helper()
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, store.DirName)
	if err := os.MkdirAll(store.NewLayout(nibsDir).DataDir(), 0755); err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}

	cfg := config.Default()
	cfg.Areas = []config.AreaConfig{
		{Name: "web", Children: []config.AreaConfig{{Name: "dashboard"}, {Name: "ui"}}},
		{Name: "auth"},
	}
	core := New(nibsDir, cfg)
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("failed to load core: %v", err)
	}
	return core, nibsDir
}

// TestCreateChecksAreaAgainstVocabulary pins the create half of the write rule.
func TestCreateChecksAreaAgainstVocabulary(t *testing.T) {
	tests := []struct {
		name        string
		area        string
		errContains []string
	}{
		{name: "unset", area: ""},
		{name: "a declared root", area: "auth"},
		{name: "a declared child", area: "web/dashboard"},
		{name: "an undeclared path", area: "nosuch", errContains: []string{"nosuch", "web/dashboard"}},
		{name: "an undeclared child of a declared root", area: "web/legacy", errContains: []string{"web/legacy"}},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, _ := setupAreaCore(t)
			b := &nib.Nib{ID: "nibs-ar" + string(rune('a'+i)), Title: "Work", Type: "task", Status: "todo", Area: tt.area}
			err := core.Create(b)
			if len(tt.errContains) == 0 {
				if err != nil {
					t.Fatalf("Create(area=%q) = %v, want nil", tt.area, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Create accepted an undeclared area %q", tt.area)
			}
			for _, want := range tt.errContains {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want substring %q", err.Error(), want)
				}
			}
		})
	}
}

// TestCreateRefusesAreaWhenNoneAreDeclared pins the store that declares nothing:
// every assignment is refused, and the refusal says that is why rather than
// printing an empty allowed set.
func TestCreateRefusesAreaWhenNoneAreDeclared(t *testing.T) {
	core, _ := setupTestCore(t) // config.Default() declares no areas

	err := core.Create(&nib.Nib{ID: "nibs-arn1", Title: "Work", Type: "task", Status: "todo", Area: "web"})
	if err == nil {
		t.Fatal("Create accepted an area against a store that declares none")
	}
	if !strings.Contains(err.Error(), "declares no areas") {
		t.Errorf("error = %q, want it to say the store declares no areas", err.Error())
	}
	if strings.Contains(err.Error(), "must be one of") {
		t.Errorf("error = %q, want no empty allowed set", err.Error())
	}
}

// TestCreateAreaRefusalNamesTheArgumentOnly and its Update counterpart pin the
// one thing the two write halves say differently.
//
// A write to an EXISTING nib re-checks the `area:` that nib will carry, which
// for most writes is the one it already carries — so retiring an `areas:` entry
// refuses writes that named no area at all, and the refusal has to say whose
// value it is and how to get out. A create has no such value: its argument is
// the only candidate, so the same clause there would point at nothing.
func TestCreateAreaRefusalNamesTheArgumentOnly(t *testing.T) {
	core, _ := setupAreaCore(t)

	err := core.Create(&nib.Nib{ID: "nibs-arc9", Title: "Work", Type: "task", Status: "todo", Area: "nosuch"})
	if err == nil {
		t.Fatal("Create accepted an undeclared area")
	}
	if strings.Contains(err.Error(), "already carries") {
		t.Errorf("error = %q; a create has no stored area the argument could be confused with", err.Error())
	}
}

func TestUpdateAreaRefusalNamesTheNibsOwnValue(t *testing.T) {
	core, _ := setupAreaCore(t)
	if err := core.Create(&nib.Nib{ID: "nibs-aru9", Title: "Work", Type: "task", Status: "todo", Area: "auth"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	clone, err := core.GetForUpdate("nibs-aru9")
	if err != nil {
		t.Fatalf("GetForUpdate: %v", err)
	}
	// The shape a retired or renamed `areas:` entry leaves behind: the write
	// names no area, and is refused for the one the nib already holds.
	clone.Area = "retired/thing"
	clone.Title = "Renamed"
	err = core.Update(clone, nil)
	if err == nil {
		t.Fatal("Update accepted an undeclared area")
	}
	for _, want := range []string{"retired/thing", "already carries", "`nibs set nibs-aru9 --clear area`"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want substring %q", err.Error(), want)
		}
	}
	// The escapes are named as commands a reader can run, so the subject has to
	// be the nib's own id and not a placeholder that exits 3.
	if strings.Contains(err.Error(), "nibs set <id>") {
		t.Errorf("error = %q, want the subject interpolated rather than left as <id>", err.Error())
	}
	// Typed so Orderer.backfillKeys can recognize a permanently stable refusal
	// on the read path without matching on the message.
	var areaErr *config.AreaError
	if !errors.As(err, &areaErr) {
		t.Errorf("error = %T, want *config.AreaError", err)
	}
}

// TestUpdateChecksAreaAgainstVocabulary pins the update half, including the two
// transitions that have to keep working: assigning a declared area, and clearing
// one back to unset.
func TestUpdateChecksAreaAgainstVocabulary(t *testing.T) {
	core, _ := setupAreaCore(t)
	if err := core.Create(&nib.Nib{ID: "nibs-aru1", Title: "Work", Type: "task", Status: "todo"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	t.Run("an undeclared area is refused", func(t *testing.T) {
		clone, err := core.GetForUpdate("nibs-aru1")
		if err != nil {
			t.Fatalf("GetForUpdate: %v", err)
		}
		clone.Area = "nosuch"
		if err := core.Update(clone, nil); err == nil {
			t.Fatal("Update accepted an undeclared area")
		} else if !strings.Contains(err.Error(), "nosuch") {
			t.Errorf("error = %q, want it to name the refused value", err.Error())
		}
		if stored, _ := core.Get("nibs-aru1"); stored.Area != "" {
			t.Errorf("refused update persisted area %q", stored.Area)
		}
	})

	t.Run("a declared area is assigned", func(t *testing.T) {
		clone, err := core.GetForUpdate("nibs-aru1")
		if err != nil {
			t.Fatalf("GetForUpdate: %v", err)
		}
		clone.Area = "web/dashboard"
		if err := core.Update(clone, nil); err != nil {
			t.Fatalf("Update: %v", err)
		}
		if stored, _ := core.Get("nibs-aru1"); stored.Area != "web/dashboard" {
			t.Errorf("area = %q, want web/dashboard", stored.Area)
		}
	})

	t.Run("clearing back to unset", func(t *testing.T) {
		clone, err := core.GetForUpdate("nibs-aru1")
		if err != nil {
			t.Fatalf("GetForUpdate: %v", err)
		}
		clone.Area = ""
		if err := core.Update(clone, nil); err != nil {
			t.Fatalf("Update: %v", err)
		}
		if stored, _ := core.Get("nibs-aru1"); stored.Area != "" {
			t.Errorf("area = %q, want it cleared", stored.Area)
		}
	})
}

// TestLoadToleratesUndeclaredArea is the read side, and it is deliberate rather
// than incidental: a file already carrying an undeclared area must load, list
// and render exactly as written. The write path is the only refusal.
//
// The absent warning and the absent `nibs check` finding are asserted too, and
// that is what pins the SEAM: an area check folded into ValidateEnums would
// reach the loader and the health report through the two calls they already
// make, turning read-tolerance into a report this feature does not own.
func TestLoadToleratesUndeclaredArea(t *testing.T) {
	core, nibsDir := setupAreaCore(t)

	file := "---\n# nibs-arld\nversion: 2\ntitle: Legacy area\nstatus: todo\ntype: task\narea: retired/thing\ncreated_at: 2026-01-02T03:04:05Z\nupdated_at: 2026-01-02T03:04:05Z\n---\n\nBody.\n"
	if err := os.WriteFile(dataPath(nibsDir, "nibs-arld--legacy.md"), []byte(file), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var warns bytes.Buffer
	core.SetWarnWriter(&warns)
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	b, err := core.Get("nibs-arld")
	if err != nil {
		t.Fatalf("Get after load: %v", err)
	}
	if b.Area != "retired/thing" {
		t.Errorf("area = %q, want it loaded as written", b.Area)
	}
	if warns.Len() != 0 {
		t.Errorf("an undeclared area must load silently for now, got:\n%s", warns.String())
	}
	for _, ie := range core.CheckAllLinks().InvalidEnums {
		if ie.NibID == "nibs-arld" {
			t.Errorf("check reported the undeclared area as an out-of-enum value: %s", ie.Reason)
		}
	}
}
