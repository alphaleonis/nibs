package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/membership"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/progress"
	"github.com/alphaleonis/nibs/testdata/fixtures"
)

// The one assignment the sample fixture carries below a root, named once so
// the two guards below read the same row.
const (
	fixtureScheduledChildID    = "tnib-t041"
	fixtureUnscheduledParentID = "tnib-f020"
	fixtureChildMilestoneID    = "tnib-m001"
)

// loadSampleProjectNibs loads the shipped fixture through the ordinary store
// path and returns every nib in it.
func loadSampleProjectNibs(t *testing.T) []*nib.Nib {
	t.Helper()

	nibsDir := fixtures.NibsPath(fixtures.CopySampleProject(t))
	cfg, err := config.LoadFromStore(nibsDir)
	if err != nil {
		t.Fatalf("loading the fixture config: %v", err)
	}
	core := nibcore.New(nibsDir, cfg)
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	return core.All()
}

// TestSampleProjectSchedulesWorkBelowARoot asserts the fixture holds the shape
// the milestone view's "orphaned row" depends on: tnib-t041 HAS a parent and
// carries a milestone of its own.
//
// Without such a row every assignment in the fixture sits on a root epic, so
// every rendered member has its carrier visible above it and the case where a
// scheduled nib's parent is somewhere else entirely cannot be reached from the
// demo store, the e2e lane or the screenshot captures.
//
// The row is asserted by id rather than by scanning for any nib of that shape:
// cmd/set_milestone_test.go, cmd/close_milestone_test.go and
// cmd/list_milestone_test.go all name tnib-t041, so moving the assignment to
// another nib has to fail here rather than there.
func TestSampleProjectSchedulesWorkBelowARoot(t *testing.T) {
	all := loadSampleProjectNibs(t)
	byID := make(map[string]*nib.Nib, len(all))
	for _, b := range all {
		byID[b.ID] = b
	}
	lookup := func(id string) *nib.Nib { return byID[id] }

	child := byID[fixtureScheduledChildID]
	if child == nil {
		t.Fatalf("the sample fixture no longer holds %s, the nib it schedules below a root",
			fixtureScheduledChildID)
	}
	if child.Parent != fixtureUnscheduledParentID {
		t.Errorf("%s's parent = %q, want %s: the scheduled row has to sit under a container "+
			"its milestone section does not draw",
			fixtureScheduledChildID, child.Parent, fixtureUnscheduledParentID)
	}
	if ms := membership.ResolvedMilestoneID(child, lookup); ms != fixtureChildMilestoneID {
		t.Errorf("%s's milestone = %q, want %s: unassigned, every assignment in the fixture "+
			"sits on a root epic again",
			fixtureScheduledChildID, ms, fixtureChildMilestoneID)
	}
}

// TestSampleProjectRoadmapDrawsAScheduledChildApartFromItsParent takes the same
// shape end to end, through the CLI against the shipped fixture: the scheduled
// task stands in its milestone's section, and its unscheduled parent is nowhere
// in that section — it is down in the backlog, which is what makes the absence
// a position rather than a missing row.
func TestSampleProjectRoadmapDrawsAScheduledChildApartFromItsParent(t *testing.T) {
	t.Cleanup(resetMembershipPinFlags)
	resetMembershipPinFlags()

	nibsDir := fixtures.NibsPath(fixtures.CopySampleProject(t))
	out, err := runRootWith(t, "--nibs-path", nibsDir, "roadmap", "--json")
	if err != nil {
		t.Fatalf("roadmap --json: %v", err)
	}
	var data roadmapData
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		t.Fatalf("decoding roadmap --json: %v", err)
	}

	// Every id a queue renders, container rows and their decompositions alike.
	ids := func(queue []queueEntry) []string {
		var out []string
		for _, e := range queue {
			out = append(out, e.Nib.ID)
			for _, item := range e.Items {
				out = append(out, item.ID)
			}
		}
		return out
	}
	has := func(list []string, id string) bool {
		for _, got := range list {
			if got == id {
				return true
			}
		}
		return false
	}

	var section []string
	for _, g := range data.Milestones {
		if g.Milestone.ID == fixtureChildMilestoneID {
			section = ids(g.Queue)
		}
	}
	if section == nil {
		t.Fatalf("the roadmap has no %s section at all", fixtureChildMilestoneID)
	}
	if !has(section, fixtureScheduledChildID) {
		t.Errorf("%s is not in %s's section (%v), though it is assigned to it",
			fixtureScheduledChildID, fixtureChildMilestoneID, section)
	}
	if has(section, fixtureUnscheduledParentID) {
		t.Errorf("%s's unscheduled parent %s is drawn inside %s's section (%v)",
			fixtureScheduledChildID, fixtureUnscheduledParentID, fixtureChildMilestoneID, section)
	}

	if data.Backlog == nil {
		t.Fatal("the roadmap has no backlog")
	}
	backlog := ids(data.Backlog.Queue)
	if !has(backlog, fixtureUnscheduledParentID) {
		t.Errorf("%s is in neither %s's section nor the backlog (%v): the guard above would "+
			"pass on a roadmap that simply lost the row",
			fixtureUnscheduledParentID, fixtureChildMilestoneID, backlog)
	}
	if has(backlog, fixtureScheduledChildID) {
		t.Errorf("%s is drawn both in %s's section and in the backlog under its parent",
			fixtureScheduledChildID, fixtureChildMilestoneID)
	}
}

// TestSampleProjectProgressAgreesAcrossRoadmapAndList reads tnib-f020's
// progress from `roadmap` and from `list -f progress` — two commands, two
// renderings, one number — and holds both to the whole decomposition.
//
// tnib-f020 has two children and one of them, tnib-t041, is scheduled into
// tnib-m001. Progress measures decomposition rather than schedule, so both
// surfaces count 2: a feature does not appear to shrink as its children are
// scheduled into milestones. Each surface is read from its own command run, so
// a change that moves one of them alone fails here rather than agreeing with
// itself.
func TestSampleProjectProgressAgreesAcrossRoadmapAndList(t *testing.T) {
	t.Cleanup(resetMembershipPinFlags)

	nibsDir := fixtures.NibsPath(fixtures.CopySampleProject(t))

	resetMembershipPinFlags()
	roadmapOut, err := runRootWith(t, "--nibs-path", nibsDir, "roadmap", "--json")
	if err != nil {
		t.Fatalf("roadmap --json: %v", err)
	}
	var data roadmapData
	if err := json.Unmarshal([]byte(roadmapOut), &data); err != nil {
		t.Fatalf("decoding roadmap --json: %v", err)
	}
	var fromRoadmap *progress.Rollup
	for _, e := range data.Backlog.Queue {
		if e.Nib.ID == fixtureUnscheduledParentID {
			fromRoadmap = e.Progress
		}
	}
	if fromRoadmap == nil {
		t.Fatalf("roadmap carries no progress for %s", fixtureUnscheduledParentID)
	}

	resetMembershipPinFlags()
	listOut, err := runRootWith(t, "--nibs-path", nibsDir, "list", "--all", "-f", "id,progress")
	if err != nil {
		t.Fatalf("list -f progress: %v", err)
	}
	var fromList *progress.Rollup
	for _, line := range strings.Split(listOut, "\n") {
		id, payload, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok || id != fixtureUnscheduledParentID {
			continue
		}
		var rollup progress.Rollup
		if err := json.Unmarshal([]byte(strings.TrimSpace(payload)), &rollup); err != nil {
			t.Fatalf("decoding %s's progress from list: %v", fixtureUnscheduledParentID, err)
		}
		fromList = &rollup
	}
	if fromList == nil {
		t.Fatalf("list -f progress has no row for %s:\n%s", fixtureUnscheduledParentID, listOut)
	}

	const wantTotal = 2
	if fromRoadmap.Total != wantTotal {
		t.Errorf("roadmap reports total %d for %s, want %d — %s is scheduled into %s, but it is still part of the decomposition",
			fromRoadmap.Total, fixtureUnscheduledParentID, wantTotal, fixtureScheduledChildID, fixtureChildMilestoneID)
	}
	if fromList.Total != wantTotal {
		t.Errorf("list -f progress reports total %d for %s, want %d",
			fromList.Total, fixtureUnscheduledParentID, wantTotal)
	}
	if *fromRoadmap != *fromList {
		t.Errorf("roadmap says %+v and list -f progress says %+v for the same nib %s",
			*fromRoadmap, *fromList, fixtureUnscheduledParentID)
	}
}
