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
// The absent LOADER warning is asserted too, and so is the absence of an
// InvalidEnum for the same nib, and together they pin the SEAM: an area check
// folded into ValidateEnums would reach loadFromDisk and CheckAllLinks through
// the two calls they already make, and the loader would then warn. The health
// report does name this nib — under its own category, see
// TestCheckAllLinksReportsUndeclaredArea — which is the one surface that is
// meant to.
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

// TestCheckAllLinksReportsUndeclaredArea is the other half of read-tolerance:
// the value loads as written, and this report is what makes it visible.
//
// It is its OWN category rather than an InvalidEnum — folding the area rule
// into ValidateEnums would reach loadFromDisk too and turn the tolerance above
// into a warning — so the finding carries the value and the declared set as
// data, not only a message.
func TestCheckAllLinksReportsUndeclaredArea(t *testing.T) {
	core, nibsDir := setupAreaCore(t)

	files := map[string]string{
		// The subject: a work nib whose stored area the vocabulary retired.
		"nibs-arf1--stranded.md": "---\n# nibs-arf1\nversion: 2\ntitle: Stranded\nstatus: todo\ntype: task\narea: retired/thing\n---\n\nBody.\n",
		// A declared assignment, which must stay unflagged.
		"nibs-arf2--located.md": "---\n# nibs-arf2\nversion: 2\ntitle: Located\nstatus: todo\ntype: task\narea: web/dashboard\n---\n\nBody.\n",
		// Unset, likewise.
		"nibs-arf3--plain.md": "---\n# nibs-arf3\nversion: 2\ntitle: Plain\nstatus: todo\ntype: task\n---\n\nBody.\n",
		// A milestone carrying an undeclared area: the axis rule already
		// refuses the KEY, so naming a declared value beside it would
		// prescribe a remedy this nib cannot follow.
		"nibs-arf4--waypoint.md": "---\n# nibs-arf4\nversion: 2\ntitle: Waypoint\nstatus: todo\ntype: milestone\narea: retired/thing\n---\n\nBody.\n",
	}
	for name, content := range files {
		if err := os.WriteFile(dataPath(nibsDir, name), []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	result := core.CheckAllLinks()
	if len(result.UndeclaredAreas) != 1 {
		t.Fatalf("undeclared areas = %+v, want exactly nibs-arf1", result.UndeclaredAreas)
	}
	got := result.UndeclaredAreas[0]
	if got.NibID != "nibs-arf1" {
		t.Errorf("nib_id = %q, want nibs-arf1", got.NibID)
	}
	if got.Path != "data/nibs-arf1--stranded.md" {
		t.Errorf("path = %q, want data/nibs-arf1--stranded.md", got.Path)
	}
	if got.Area != "retired/thing" {
		t.Errorf("area = %q, want retired/thing", got.Area)
	}
	for _, want := range []string{"web", "web/dashboard", "auth"} {
		if !strings.Contains(got.Declared, want) {
			t.Errorf("declared = %q, want it to name %q", got.Declared, want)
		}
	}
	if result.AreaIssues() != 1 {
		t.Errorf("AreaIssues() = %d, want 1", result.AreaIssues())
	}
	if !result.HasIssues() {
		t.Error("HasIssues() = false; an undeclared area is an issue")
	}
	// The milestone is reported once, by the axis rule, and its remedy is to
	// drop the key rather than to pick a declared value.
	axisIDs := make([]string, 0, len(result.InvalidAxes))
	for _, ia := range result.InvalidAxes {
		axisIDs = append(axisIDs, ia.NibID)
	}
	if len(axisIDs) != 1 || axisIDs[0] != "nibs-arf4" {
		t.Errorf("invalid axes = %v, want exactly nibs-arf4", axisIDs)
	}
}

// TestCheckIsSilentOnAreasWhenStoreDeclaresNone pins the deliberate exemption:
// with no vocabulary to check against, a stored `area:` produces no finding —
// even though every write to that nib is refused for it. The fixture is the
// only store shape the exemption changes anything for, and the trade it makes
// is set out at Core.CheckAllLinks.
func TestCheckIsSilentOnAreasWhenStoreDeclaresNone(t *testing.T) {
	core, nibsDir := setupTestCore(t) // config.Default() declares no areas

	file := "---\n# nibs-arn1\nversion: 2\ntitle: Located\nstatus: todo\ntype: task\narea: whatever/team\n---\n\nBody.\n"
	if err := os.WriteFile(dataPath(nibsDir, "nibs-arn1--located.md"), []byte(file), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	result := core.CheckAllLinks()
	if len(result.UndeclaredAreas) != 0 {
		t.Errorf("a store declaring no areas must produce no area findings, got %+v", result.UndeclaredAreas)
	}
	if result.AreaIssues() != 0 {
		t.Errorf("AreaIssues() = %d, want 0", result.AreaIssues())
	}
}
