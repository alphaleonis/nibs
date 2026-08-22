package nibcore

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/membership"
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
		"chk-t2":  {ID: "chk-t2", Status: "todo", Type: "task", Path: "data/chk-t2.md", Milestone: "chk-ms1", MilestoneOrder: "a0"},
		"chk-t3":  {ID: "chk-t3", Status: "completed", Type: "task", Path: "data/chk-t3.md", Milestone: "chk-ms1", MilestoneOrder: "c0"},
		// Scrapped is releasing too, so one open entry is enough.
		"chk-ms2": {ID: "chk-ms2", Status: "scrapped", Type: "milestone", Path: "data/chk-ms2--two.md"},
		"chk-t4":  {ID: "chk-t4", Status: "draft", Type: "task", Path: "data/chk-t4.md", Milestone: "chk-ms2"},
	}

	got := closedMilestoneQueuesInMap(nibs, cfg.IsClosedStatus, cfg.StatusReleasesDependents)
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
			// A prefix-less `milestone: ms1` is inert everywhere: membership
			// resolves the assignment axis by exact id, so no view, no orderer and
			// neither write refusal sees this nib in chk-ms's queue. Reporting it
			// would send a reader to repair something no write surface objects to.
			// (That such an assignment is silently inert rather than named is a
			// separate gap, and not this finding's to answer.)
			name: "a shorthand assignment is not a queue entry, because no refusal sees one",
			nibs: map[string]*nib.Nib{
				"chk-ms": {ID: "chk-ms", Status: "completed", Type: "milestone", Path: "data/chk-ms.md"},
				"chk-t":  {ID: "chk-t", Status: "todo", Type: "task", Path: "data/chk-t.md", Milestone: "ms"},
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
			if got := closedMilestoneQueuesInMap(tt.nibs, cfg.IsClosedStatus, cfg.StatusReleasesDependents); len(got) != 0 {
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

// TestClosedMilestoneQueueAgreesWithMembership ties the read-side finding to the
// write-side refusal by construction rather than by restating its clauses.
//
// The finding's whole justification is that it names exactly the set the refusal
// names (see closedMilestoneQueuesInMap's doc: "A report naming a wider or
// narrower set than the refusal would send a reader to repair something no write
// surface objects to"). Both `nibs close`'s gate and updateNib's backstop read
// that set through graph.OpenQueueEntries -> membership.View.DirectMembers, whose
// resolution rule is membership.ResolvedMilestoneID — an EXACT id, no prefix
// expansion. Resolving the finding's side by any other rule silently re-opens
// that gap, so this compares the two answers over one map instead of trusting
// the prose.
//
// The shorthand assignment below is the case that separates them: a hand-edited
// `milestone: ms1` in a store prefixed `chk-`. It is the only population the
// finding exists for, since every write path stores the normalized full id.
func TestClosedMilestoneQueueAgreesWithMembership(t *testing.T) {
	cfg := config.Default()
	all := []*nib.Nib{
		{ID: "chk-ms1", Status: "completed", Type: "milestone", Path: "data/chk-ms1.md"},
		{ID: "chk-full", Status: "todo", Type: "task", Path: "data/chk-full.md", Milestone: "chk-ms1", MilestoneOrder: "a0"},
		{ID: "chk-short", Status: "todo", Type: "task", Path: "data/chk-short.md", Milestone: "ms1", MilestoneOrder: "b0"},
	}
	nibs := make(map[string]*nib.Nib, len(all))
	for _, b := range all {
		nibs[b.ID] = b
	}

	view := membership.Compute(all)
	var refusalSees []string
	for _, m := range view.DirectMembers("chk-ms1") {
		if !cfg.IsClosedStatus(m.Status) {
			refusalSees = append(refusalSees, m.ID)
		}
	}

	findings := closedMilestoneQueuesInMap(nibs, cfg.IsClosedStatus, cfg.StatusReleasesDependents)
	var checkSees []string
	for _, f := range findings {
		if f.NibID == "chk-ms1" {
			checkSees = f.Open
		}
	}

	slices.Sort(refusalSees)
	slices.Sort(checkSees)
	if !reflect.DeepEqual(checkSees, refusalSees) {
		t.Errorf("check reports queue %v but the refusal sees %v; the finding must name the set the write path objects to, no wider and no narrower",
			checkSees, refusalSees)
	}
}
