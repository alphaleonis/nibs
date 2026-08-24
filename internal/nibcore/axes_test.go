package nibcore

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
)

// TestCreateRefusesAxesOnMilestone pins the write-side half of the axis rule:
// a milestone is a waypoint, not work, so Create refuses one carrying a
// milestone assignment or an area.
func TestCreateRefusesAxesOnMilestone(t *testing.T) {
	// Declares `web/ui`, which the accepting row at the end carries: an area is
	// checked against the vocabulary on every write, so a store declaring none
	// would refuse that seed for a reason this test is not about.
	core, _ := setupAreaCore(t)

	tests := []struct {
		name        string
		id          string
		milestone   string
		area        string
		errContains string
	}{
		{name: "milestone assignment", id: "nibs-axm", milestone: "nibs-m1", errContains: "cannot be assigned to a milestone"},
		{name: "area", id: "nibs-axa", area: "web/ui", errContains: "cannot have an area"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &nib.Nib{
				ID:        tt.id,
				Title:     "Waypoint",
				Type:      "milestone",
				Status:    "todo",
				Milestone: tt.milestone,
				Area:      tt.area,
			}
			err := core.Create(b)
			if err == nil {
				t.Fatalf("Create accepted a milestone carrying milestone=%q area=%q", tt.milestone, tt.area)
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("error = %q, want substring %q", err.Error(), tt.errContains)
			}
		})
	}

	// The same fields on a work type pass.
	work := &nib.Nib{ID: "nibs-axok", Title: "Work", Type: "task", Status: "todo", Milestone: "nibs-m1", Area: "web/ui"}
	if err := core.Create(work); err != nil {
		t.Fatalf("Create refused a task carrying axis fields: %v", err)
	}
}

// TestUpdateRefusesAxesOnMilestone pins the update path: retyping a nib to
// milestone while it carries an axis field, or handing an axis field to an
// existing milestone, is refused before anything is written.
func TestUpdateRefusesAxesOnMilestone(t *testing.T) {
	core, _ := setupTestCore(t)

	if err := core.Create(&nib.Nib{ID: "nibs-ms1", Title: "Waypoint", Type: "milestone", Status: "todo"}); err != nil {
		t.Fatalf("create milestone: %v", err)
	}
	if err := core.Create(&nib.Nib{ID: "nibs-t1", Title: "Assigned task", Type: "task", Status: "todo", Milestone: "nibs-ms1"}); err != nil {
		t.Fatalf("create task: %v", err)
	}

	t.Run("milestone gaining an assignment", func(t *testing.T) {
		clone, err := core.GetForUpdate("nibs-ms1")
		if err != nil {
			t.Fatalf("GetForUpdate: %v", err)
		}
		clone.Milestone = "nibs-ms1"
		if err := core.Update(clone, nil); err == nil {
			t.Fatal("Update accepted a milestone gaining a milestone assignment")
		} else if !strings.Contains(err.Error(), "cannot be assigned to a milestone") {
			t.Errorf("error = %q, want the axis refusal", err.Error())
		}
	})

	t.Run("milestone gaining an area", func(t *testing.T) {
		clone, err := core.GetForUpdate("nibs-ms1")
		if err != nil {
			t.Fatalf("GetForUpdate: %v", err)
		}
		clone.Area = "web/ui"
		if err := core.Update(clone, nil); err == nil {
			t.Fatal("Update accepted a milestone gaining an area")
		} else if !strings.Contains(err.Error(), "cannot have an area") {
			t.Errorf("error = %q, want the axis refusal", err.Error())
		}
	})

	t.Run("assigned nib retyped to milestone", func(t *testing.T) {
		clone, err := core.GetForUpdate("nibs-t1")
		if err != nil {
			t.Fatalf("GetForUpdate: %v", err)
		}
		clone.Type = "milestone"
		if err := core.Update(clone, nil); err == nil {
			t.Fatal("Update accepted retyping an assigned nib to milestone")
		} else if !strings.Contains(err.Error(), "cannot be assigned to a milestone") {
			t.Errorf("error = %q, want the axis refusal", err.Error())
		}
	})
}

// TestLoadToleratesAxesOnMilestone pins read tolerance: a hand-edited file
// whose milestone carries an axis field still loads as written — the axis rule
// bites on the write paths only, like every other field-integrity rule.
func TestLoadToleratesAxesOnMilestone(t *testing.T) {
	core, nibsDir := setupTestCore(t)

	content := "---\n# nibs-hax1\nversion: 2\ntitle: Hand-edited waypoint\nstatus: todo\ntype: milestone\narea: web/ui\ncreated_at: 2026-01-02T03:04:05Z\nupdated_at: 2026-01-02T03:04:05Z\n---\n\nBody.\n"
	if err := os.WriteFile(dataPath(nibsDir, "nibs-hax1--hand.md"), []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	b, err := core.Get("nibs-hax1")
	if err != nil {
		t.Fatalf("a milestone carrying an area must still load: %v", err)
	}
	if b.Area != "web/ui" {
		t.Errorf("Area = %q, want the value as written", b.Area)
	}
}

// TestLoadWarnsAxesOnMilestone pins the visibility half of the tolerance
// above, mirroring the out-of-enum posture: the offender loads as written, but
// the load names it — the write paths refuse the shape strictly, so without a
// diagnostic the nib is un-updatable through nibs with nothing pointing at the
// file. Legal shapes (a clean milestone, axis keys on a work type) load
// without a warning.
func TestLoadWarnsAxesOnMilestone(t *testing.T) {
	core, nibsDir := setupTestCore(t)

	offender := "---\n# nibs-hax2\nversion: 2\ntitle: Located waypoint\nstatus: todo\ntype: milestone\narea: web/ui\ncreated_at: 2026-01-02T03:04:05Z\nupdated_at: 2026-01-02T03:04:05Z\n---\n\nBody.\n"
	if err := os.WriteFile(dataPath(nibsDir, "nibs-hax2--located.md"), []byte(offender), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	var warns bytes.Buffer
	core.SetWarnWriter(&warns)
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, want := range []string{"nibs-hax2", "cannot have an area", "nibs check"} {
		if !strings.Contains(warns.String(), want) {
			t.Errorf("load warning should contain %q, got:\n%s", want, warns.String())
		}
	}

	// Legal shapes stay silent: replace the offender with a clean milestone and
	// a work nib carrying both axis keys, and the same load warns nothing.
	if err := os.Remove(dataPath(nibsDir, "nibs-hax2--located.md")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	clean := "---\n# nibs-way1\nversion: 2\ntitle: Waypoint\nstatus: todo\ntype: milestone\ncreated_at: 2026-01-02T03:04:05Z\nupdated_at: 2026-01-02T03:04:05Z\n---\n\nBody.\n"
	work := "---\n# nibs-wrk1\nversion: 2\ntitle: Assigned task\nstatus: todo\ntype: task\nmilestone: nibs-way1\narea: web/ui\ncreated_at: 2026-01-02T03:04:05Z\nupdated_at: 2026-01-02T03:04:05Z\n---\n\nBody.\n"
	if err := os.WriteFile(dataPath(nibsDir, "nibs-way1--waypoint.md"), []byte(clean), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(dataPath(nibsDir, "nibs-wrk1--task.md"), []byte(work), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	warns.Reset()
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if warns.Len() != 0 {
		t.Errorf("legal shapes must load without a warning, got:\n%s", warns.String())
	}
}
