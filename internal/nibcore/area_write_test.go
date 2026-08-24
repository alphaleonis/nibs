package nibcore

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/fsutil"
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

// areaCoreWith creates nibs carrying the given areas and returns the core.
func areaCoreWith(t *testing.T, areas map[string]string) (*Core, string) {
	t.Helper()
	core, nibsDir := setupAreaCore(t)
	ids := make([]string, 0, len(areas))
	for id := range areas {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		b := &nib.Nib{ID: id, Title: "Work " + id, Type: "task", Status: "todo", Area: areas[id]}
		if err := core.Create(b); err != nil {
			t.Fatalf("Create(%s): %v", id, err)
		}
	}
	return core, nibsDir
}

// storedAreaOf reads a nib's `area:` back OFF DISK, so the assertions judge what
// was persisted rather than what the in-memory map happens to hold.
func storedAreaOf(t *testing.T, core *Core, nibsDir, id string) string {
	t.Helper()
	b, err := core.Get(id)
	if err != nil {
		t.Fatalf("%s is not in the store: %v", id, err)
	}
	raw, err := os.ReadFile(filepath.Join(nibsDir, filepath.FromSlash(b.Path)))
	if err != nil {
		t.Fatalf("reading %s: %v", b.Path, err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if area, ok := strings.CutPrefix(line, "area: "); ok {
			return area
		}
	}
	return ""
}

// renameRewrite is the mapping `nibs area rename` supplies: everything at or
// below from moves, keeping whatever it carried below from.
func renameRewrite(cfg *config.Config, from, to string) func(string) (string, bool) {
	return func(area string) (string, bool) {
		if !cfg.IsAreaWithin(area, from) {
			return "", false
		}
		return to + strings.TrimPrefix(area, from), true
	}
}

// TestRewriteAreaAssignmentsMovesTheWholeSubtree pins the cascade a rename
// needs: every nib assigned AT or BELOW the renamed node follows it, and a nib
// outside the subtree is not touched — including one whose area merely spells a
// string prefix of it.
func TestRewriteAreaAssignmentsMovesTheWholeSubtree(t *testing.T) {
	core, nibsDir := areaCoreWith(t, map[string]string{
		"nibs-aw01": "web",
		"nibs-aw02": "web/dashboard",
		"nibs-aw03": "web/ui",
		"nibs-aw04": "auth",
		"nibs-aw05": "",
	})

	written, err := rewriteAreasLocked(t, core, renameRewrite(core.Config(), "web", "frontend"))
	if err != nil {
		t.Fatalf("RewriteAreaAssignments: %v", err)
	}
	if want := []string{"nibs-aw01", "nibs-aw02", "nibs-aw03"}; !slices.Equal(written, want) {
		t.Errorf("written = %v, want %v (in id order)", written, want)
	}
	for id, want := range map[string]string{
		"nibs-aw01": "frontend",
		"nibs-aw02": "frontend/dashboard",
		"nibs-aw03": "frontend/ui",
		"nibs-aw04": "auth",
		"nibs-aw05": "",
	} {
		if got := storedAreaOf(t, core, nibsDir, id); got != want {
			t.Errorf("%s stored area = %q, want %q", id, got, want)
		}
		b, _ := core.Get(id)
		if b.Area != want {
			t.Errorf("%s in-memory area = %q, want %q", id, b.Area, want)
		}
	}
}

// TestRewriteAreaAssignmentsCollapsesOntoOneTarget is the other mapping a caller
// supplies — `nibs area rm --move-to`, where every member lands ON the target
// rather than keeping a remainder that the target does not declare.
func TestRewriteAreaAssignmentsCollapsesOntoOneTarget(t *testing.T) {
	core, nibsDir := areaCoreWith(t, map[string]string{
		"nibs-aw11": "web",
		"nibs-aw12": "web/dashboard",
		"nibs-aw13": "auth",
	})
	cfg := core.Config()

	if _, err := rewriteAreasLocked(t, core, func(area string) (string, bool) {
		if !cfg.IsAreaWithin(area, "web") {
			return "", false
		}
		return "auth", true
	}); err != nil {
		t.Fatalf("RewriteAreaAssignments: %v", err)
	}
	for _, id := range []string{"nibs-aw11", "nibs-aw12", "nibs-aw13"} {
		if got := storedAreaOf(t, core, nibsDir, id); got != "auth" {
			t.Errorf("%s stored area = %q, want auth", id, got)
		}
	}
}

// TestRewriteAreaAssignmentsClears covers the `--unassign` mapping, whose new
// value is the empty string — a value Render omits entirely rather than writing
// as an empty key.
func TestRewriteAreaAssignmentsClears(t *testing.T) {
	core, nibsDir := areaCoreWith(t, map[string]string{"nibs-aw21": "web/dashboard"})
	cfg := core.Config()

	if _, err := rewriteAreasLocked(t, core, func(area string) (string, bool) {
		return "", cfg.IsAreaWithin(area, "web")
	}); err != nil {
		t.Fatalf("RewriteAreaAssignments: %v", err)
	}
	if got := storedAreaOf(t, core, nibsDir, "nibs-aw21"); got != "" {
		t.Errorf("stored area = %q, want it cleared", got)
	}
	b, _ := core.Get("nibs-aw21")
	if b.Area != "" {
		t.Errorf("in-memory area = %q, want it cleared", b.Area)
	}
}

// TestRewriteAreaAssignmentsLeavesTheVocabularyAlone is hazard #2 as a test.
//
// Core.ValidateArea reads c.config.Areas OFF-LOCK, and rests that on nothing
// mutating it after construction. A cascade that assigned the new vocabulary
// into the live config — the way `nibs config set-prefix` assigns the prefix —
// would make that read a race. The verbs write the vocabulary to the FILE and
// never into this struct, and this pins it: after a cascade the loaded config
// still declares exactly what it declared before.
func TestRewriteAreaAssignmentsLeavesTheVocabularyAlone(t *testing.T) {
	core, _ := areaCoreWith(t, map[string]string{"nibs-aw31": "web/dashboard"})
	before := append([]string(nil), core.Config().AreaPaths()...)

	if _, err := rewriteAreasLocked(t, core, renameRewrite(core.Config(), "web", "frontend")); err != nil {
		t.Fatalf("RewriteAreaAssignments: %v", err)
	}
	if after := core.Config().AreaPaths(); !slices.Equal(before, after) {
		t.Errorf("the cascade mutated the live vocabulary: %v -> %v", before, after)
	}
}

// TestRewriteAreaAssignmentsRacesNothingAgainstValidateArea is the other half of
// hazard #2, and the half only the detector can answer: ValidateArea reads
// c.config off-lock, so a cascade that touched the config struct would be a data
// race rather than merely a stale read. This package runs under -race in the
// default gate, so the assertion is the detector itself.
func TestRewriteAreaAssignmentsRacesNothingAgainstValidateArea(t *testing.T) {
	core, _ := areaCoreWith(t, map[string]string{
		"nibs-aw41": "web",
		"nibs-aw42": "web/dashboard",
		"nibs-aw43": "web/ui",
	})
	probe := &nib.Nib{ID: "nibs-aw41", Area: "web/dashboard"}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			_ = core.ValidateArea(probe)
		}
	}()
	if _, err := rewriteAreasLocked(t, core, renameRewrite(core.Config(), "web", "frontend")); err != nil {
		t.Fatalf("RewriteAreaAssignments: %v", err)
	}
	<-done
}

// TestRewriteAreaAssignmentsPartialFailureIsRerunnable pins the claim the CLI's
// partial-failure message makes: the writes already made stay, and rerunning the
// same command finishes the job.
//
// It holds because a rewritten nib is no longer a MEMBER — its new value is not
// within the old ancestor — so the recomputed set on the second run holds only
// what the first run never reached. The writes are ordered by id so the failure
// lands on a known nib rather than wherever the map happened to iterate.
func TestRewriteAreaAssignmentsPartialFailureIsRerunnable(t *testing.T) {
	core, nibsDir := areaCoreWith(t, map[string]string{
		"nibs-aw51": "web",
		"nibs-aw52": "web/dashboard",
		"nibs-aw53": "web/ui",
	})

	orig := fsutil.RenameFn
	fail := true
	fsutil.RenameFn = func(oldpath, newpath string) error {
		if fail && strings.Contains(newpath, "nibs-aw52") {
			return errors.New("simulated persistence failure")
		}
		return orig(oldpath, newpath)
	}
	t.Cleanup(func() { fsutil.RenameFn = orig })

	written, err := rewriteAreasLocked(t, core, renameRewrite(core.Config(), "web", "frontend"))
	if err == nil {
		t.Fatal("expected the seeded write failure to surface, got nil")
	}
	if !strings.Contains(err.Error(), "nibs-aw52") {
		t.Errorf("error = %v, want it to name the nib that refused", err)
	}
	if want := []string{"nibs-aw51"}; !slices.Equal(written, want) {
		t.Fatalf("written = %v, want %v — the loop must stop at the first refusal", written, want)
	}
	if got := storedAreaOf(t, core, nibsDir, "nibs-aw51"); got != "frontend" {
		t.Errorf("the write made before the failure was not persisted: %q", got)
	}
	if got := storedAreaOf(t, core, nibsDir, "nibs-aw53"); got != "web/ui" {
		t.Errorf("a nib behind the failure was written anyway: %q", got)
	}

	fail = false
	written, err = rewriteAreasLocked(t, core, renameRewrite(core.Config(), "web", "frontend"))
	if err != nil {
		t.Fatalf("the rerun must finish the job: %v", err)
	}
	if want := []string{"nibs-aw52", "nibs-aw53"}; !slices.Equal(written, want) {
		t.Errorf("the rerun wrote %v, want %v — it must skip what the first run already moved", written, want)
	}
	for id, want := range map[string]string{
		"nibs-aw51": "frontend",
		"nibs-aw52": "frontend/dashboard",
		"nibs-aw53": "frontend/ui",
	} {
		if got := storedAreaOf(t, core, nibsDir, id); got != want {
			t.Errorf("%s = %q, want %q after the rerun", id, got, want)
		}
	}
}

// TestRewriteAreaAssignmentsFlushesEveryDirectoryItTouched pins the batch
// contract: one flush per DISTINCT directory, and owed on the error path too,
// since an aborted cascade has already committed its earlier renames. An
// archived member is what makes the set span more than one directory.
func TestRewriteAreaAssignmentsFlushesEveryDirectoryItTouched(t *testing.T) {
	core, nibsDir := areaCoreWith(t, map[string]string{
		"nibs-aw61": "web",
		"nibs-aw62": "web/dashboard",
		"nibs-aw63": "web/ui",
	})
	if err := core.Archive("nibs-aw61"); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	var flushed []string
	origSync := fsutil.SyncDirFn
	fsutil.SyncDirFn = func(dir string) { flushed = append(flushed, dir) }
	t.Cleanup(func() { fsutil.SyncDirFn = origSync })

	origRename := fsutil.RenameFn
	fsutil.RenameFn = func(oldpath, newpath string) error {
		if strings.Contains(newpath, "nibs-aw63") {
			return errors.New("simulated persistence failure")
		}
		return origRename(oldpath, newpath)
	}
	t.Cleanup(func() { fsutil.RenameFn = origRename })

	if _, err := rewriteAreasLocked(t, core, renameRewrite(core.Config(), "web", "frontend")); err == nil {
		t.Fatal("expected the seeded write failure to surface, got nil")
	}

	layout := store.NewLayout(nibsDir)
	for _, dir := range []string{layout.DataDir(), layout.ArchiveDir()} {
		if !slices.Contains(flushed, dir) {
			t.Errorf("an aborted cascade did not flush %s; flushed = %v", dir, flushed)
		}
	}
	for _, dir := range []string{layout.DataDir(), layout.ArchiveDir()} {
		if n := countOf(flushed, dir); n != 1 {
			t.Errorf("%s was flushed %d times, want exactly 1 — the batch pays per directory", dir, n)
		}
	}
}

func countOf(items []string, want string) int {
	n := 0
	for _, item := range items {
		if item == want {
			n++
		}
	}
	return n
}

// rewriteAreasLocked runs one cascade under the store lock its caller owes it,
// held for that call alone. Held any longer it would deadlock the next call: the
// flock is per descriptor, so a second acquisition in this process waits on the
// first.
func rewriteAreasLocked(t *testing.T, core *Core, rewrite func(string) (string, bool)) ([]string, error) {
	t.Helper()
	lock, err := AcquireStoreLock(core.Root())
	if err != nil {
		t.Fatalf("AcquireStoreLock: %v", err)
	}
	defer func() { _ = lock.Release() }()
	return core.RewriteAreaAssignments(lock, rewrite)
}

// TestRewriteAreaAssignmentsRequiresTheStoreLock pins the precondition the
// signature states. Each of the three refusals makes the claim the token stands
// for true, and any of them passing would run a cascade whose config write — the
// other half of the same edit, made by the caller after this returns — is not
// serialized against a concurrent area edit at all.
func TestRewriteAreaAssignmentsRequiresTheStoreLock(t *testing.T) {
	core, nibsDir := areaCoreWith(t, map[string]string{"nibs-aw71": "web/dashboard"})

	other, _ := areaCoreWith(t, map[string]string{"nibs-aw72": "web"})
	foreign, err := AcquireStoreLock(other.Root())
	if err != nil {
		t.Fatalf("AcquireStoreLock: %v", err)
	}
	defer func() { _ = foreign.Release() }()

	released, err := AcquireStoreLock(core.Root())
	if err != nil {
		t.Fatalf("AcquireStoreLock: %v", err)
	}
	if err := released.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	tests := []struct {
		name string
		lock *StoreLock
		want string
	}{
		{name: "no token at all", lock: nil, want: "requires the store-wide lock"},
		{name: "a token already released", lock: released, want: "already-released"},
		{name: "a token for another store", lock: foreign, want: "different store"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			written, err := core.RewriteAreaAssignments(tt.lock, renameRewrite(core.Config(), "web", "frontend"))
			if err == nil {
				t.Fatal("the cascade ran without proof of the store lock")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want substring %q", err, tt.want)
			}
			if len(written) != 0 {
				t.Errorf("written = %v, want nothing written", written)
			}
			if got := storedAreaOf(t, core, nibsDir, "nibs-aw71"); got != "web/dashboard" {
				t.Errorf("a refused cascade wrote a nib anyway: %q", got)
			}
		})
	}
}
