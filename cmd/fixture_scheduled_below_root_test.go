package cmd

import (
	"encoding/json"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/membership"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
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
