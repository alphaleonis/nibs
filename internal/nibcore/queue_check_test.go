package nibcore

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/store"
)

// TestClosedMilestoneQueuesInMap pins the closed-milestone-queue finding: a
// milestone carrying a status that releases its dependents while open work is
// still assigned to it is reported, naming the open entries in queue order.
// Findings arrive sorted by milestone id.
func TestClosedMilestoneQueuesInMap(t *testing.T) {
	cfg := config.Default()
	nibs := map[string]*nib.Nib{
		// Completed with two open entries — reported, and the already-closed
		// member is left out of the set.
		"chk-ms1": {ID: "chk-ms1", Status: "completed", Type: "milestone", Path: "data/chk-ms1--one.md"},
		"chk-t1":  {ID: "chk-t1", Status: "in-progress", Type: "task", Path: "data/chk-t1.md", Milestone: "chk-ms1", MilestoneOrder: "b0"},
		"chk-t2":  {ID: "chk-t2", Status: "todo", Type: "task", Path: "data/chk-t2.md", Milestone: "ms1", MilestoneOrder: "a0"},
		"chk-t3":  {ID: "chk-t3", Status: "completed", Type: "task", Path: "data/chk-t3.md", Milestone: "chk-ms1", MilestoneOrder: "c0"},
		// Scrapped is releasing too, so one open entry is enough.
		"chk-ms2": {ID: "chk-ms2", Status: "scrapped", Type: "milestone", Path: "data/chk-ms2--two.md"},
		"chk-t4":  {ID: "chk-t4", Status: "draft", Type: "task", Path: "data/chk-t4.md", Milestone: "chk-ms2"},
	}

	got := closedMilestoneQueuesInMap(nibs, "chk-", cfg.IsClosedStatus, cfg.StatusReleasesDependents)
	want := []ClosedMilestoneQueue{
		{NibID: "chk-ms1", Path: "data/chk-ms1--one.md", Status: "completed", Open: []string{"chk-t2", "chk-t1"}},
		{NibID: "chk-ms2", Path: "data/chk-ms2--two.md", Status: "scrapped", Open: []string{"chk-t4"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("closedMilestoneQueuesInMap = %+v, want %+v", got, want)
	}
}

// TestClosedMilestoneQueuesInMapSilentCases pins what the finding must NOT
// report: the states decision 1.5 permits, and the ones no remedy could act on.
func TestClosedMilestoneQueuesInMapSilentCases(t *testing.T) {
	cfg := config.Default()
	tests := []struct {
		name  string
		nibs  map[string]*nib.Nib
		quiet string
	}{
		{
			name: "a deferred milestone keeps its queue on purpose",
			nibs: map[string]*nib.Nib{
				"chk-ms": {ID: "chk-ms", Status: "deferred", Type: "milestone", Path: "data/chk-ms.md"},
				"chk-t":  {ID: "chk-t", Status: "todo", Type: "task", Path: "data/chk-t.md", Milestone: "chk-ms"},
			},
		},
		{
			name: "an open milestone is no offense whatever its queue holds",
			nibs: map[string]*nib.Nib{
				"chk-ms": {ID: "chk-ms", Status: "in-progress", Type: "milestone", Path: "data/chk-ms.md"},
				"chk-t":  {ID: "chk-t", Status: "todo", Type: "task", Path: "data/chk-t.md", Milestone: "chk-ms"},
			},
		},
		{
			name: "a deferred MEMBER is closed and does not hold the milestone open",
			nibs: map[string]*nib.Nib{
				"chk-ms": {ID: "chk-ms", Status: "completed", Type: "milestone", Path: "data/chk-ms.md"},
				"chk-t":  {ID: "chk-t", Status: "deferred", Type: "task", Path: "data/chk-t.md", Milestone: "chk-ms"},
			},
		},
		{
			name: "an empty queue",
			nibs: map[string]*nib.Nib{
				"chk-ms": {ID: "chk-ms", Status: "completed", Type: "milestone", Path: "data/chk-ms.md"},
			},
		},
		{
			name: "open work reaching the milestone only through an assigned ancestor",
			nibs: map[string]*nib.Nib{
				"chk-ms": {ID: "chk-ms", Status: "completed", Type: "milestone", Path: "data/chk-ms.md"},
				"chk-ep": {ID: "chk-ep", Status: "completed", Type: "epic", Path: "data/chk-ep.md", Milestone: "chk-ms"},
				"chk-t":  {ID: "chk-t", Status: "todo", Type: "task", Path: "data/chk-t.md", Parent: "chk-ep"},
			},
		},
		{
			name: "an assignment naming a non-milestone schedules nothing",
			nibs: map[string]*nib.Nib{
				"chk-ep": {ID: "chk-ep", Status: "completed", Type: "epic", Path: "data/chk-ep.md"},
				"chk-t":  {ID: "chk-t", Status: "todo", Type: "task", Path: "data/chk-t.md", Milestone: "chk-ep"},
			},
		},
		{
			name: "a dangling assignment schedules nothing",
			nibs: map[string]*nib.Nib{
				"chk-ms": {ID: "chk-ms", Status: "completed", Type: "milestone", Path: "data/chk-ms.md"},
				"chk-t":  {ID: "chk-t", Status: "todo", Type: "task", Path: "data/chk-t.md", Milestone: "chk-gone"},
			},
		},
		{
			name: "a milestone hand-assigned to a closed milestone is not a queue entry",
			nibs: map[string]*nib.Nib{
				"chk-ms":  {ID: "chk-ms", Status: "completed", Type: "milestone", Path: "data/chk-ms.md"},
				"chk-ms2": {ID: "chk-ms2", Status: "todo", Type: "milestone", Path: "data/chk-ms2.md", Milestone: "chk-ms"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := closedMilestoneQueuesInMap(tt.nibs, "chk-", cfg.IsClosedStatus, cfg.StatusReleasesDependents); len(got) != 0 {
				t.Errorf("closedMilestoneQueuesInMap = %+v, want none", got)
			}
		})
	}
}

// TestCheckAllLinksReportsClosedMilestoneQueues pins the wiring: the two role
// predicates reach the derivation through Core.CheckAllLinks, the finding lands
// in the result, and it counts toward the non-zero exit.
func TestCheckAllLinksReportsClosedMilestoneQueues(t *testing.T) {
	tmp := t.TempDir()
	nibsDir := filepath.Join(tmp, store.DirName)
	dataDir := store.NewLayout(nibsDir).DataDir()
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	core := New(nibsDir, config.DefaultWithPrefix("chk-"))
	if err := core.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, b := range []*nib.Nib{
		{ID: "chk-ms1", Title: "Wave", Type: "milestone", Status: "in-progress"},
		{ID: "chk-t1", Title: "Open work", Type: "task", Status: "todo", Milestone: "chk-ms1"},
	} {
		if err := core.Create(b); err != nil {
			t.Fatalf("create %s: %v", b.ID, err)
		}
	}
	if result := core.CheckAllLinks(); result.QueueIssues() != 0 {
		t.Fatalf("QueueIssues() = %d before the hand edit, want 0", result.QueueIssues())
	}

	// The hand edit no write surface would accept.
	ms, err := core.Get("chk-ms1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	edited := ms.Clone()
	edited.Status = "completed"
	content, err := edited.Render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if err := os.WriteFile(filepath.Join(core.Root(), edited.Path), content, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := core.Load(); err != nil {
		t.Fatalf("reload: %v", err)
	}

	result := core.CheckAllLinks()
	want := []ClosedMilestoneQueue{{NibID: "chk-ms1", Path: edited.Path, Status: "completed", Open: []string{"chk-t1"}}}
	if !reflect.DeepEqual(result.ClosedMilestoneQueues, want) {
		t.Fatalf("ClosedMilestoneQueues = %+v, want %+v", result.ClosedMilestoneQueues, want)
	}
	if result.QueueIssues() != 1 {
		t.Errorf("QueueIssues() = %d, want 1", result.QueueIssues())
	}
	if result.TotalIssues() != 1 {
		t.Errorf("TotalIssues() = %d, want 1 (the queue finding must count)", result.TotalIssues())
	}
	if !result.HasIssues() {
		t.Error("HasIssues() = false, want true")
	}
}
