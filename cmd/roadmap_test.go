package cmd

import (
	"testing"
	"time"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/nib"
)

func TestBuildRoadmap(t *testing.T) {
	cfg := config.Default()

	now := time.Now()

	tests := []struct {
		name                  string
		nibs                 []*nib.Nib
		includeDone           bool
		wantMilestones        int
		wantUnscheduledEpics  int
		wantUnscheduledOther  int
	}{
		{
			name:           "empty nibs",
			nibs:          []*nib.Nib{},
			wantMilestones: 0,
		},
		{
			name: "milestone with epic and items",
			nibs: []*nib.Nib{
				{ID: "m1", Type: "milestone", Title: "v1.0", Status: "todo", CreatedAt: &now},
				{ID: "e1", Type: "epic", Title: "Auth", Status: "todo", Parent: "m1"},
				{ID: "t1", Type: "task", Title: "Login", Status: "todo", Parent: "e1"},
			},
			wantMilestones: 1,
		},
		{
			name: "milestone with direct children (no epic)",
			nibs: []*nib.Nib{
				{ID: "m1", Type: "milestone", Title: "v1.0", Status: "todo", CreatedAt: &now},
				{ID: "t1", Type: "task", Title: "Docs", Status: "todo", Parent: "m1"},
			},
			wantMilestones: 1,
		},
		{
			name: "unscheduled epic",
			nibs: []*nib.Nib{
				{ID: "e1", Type: "epic", Title: "Future", Status: "todo"},
				{ID: "t1", Type: "task", Title: "Nice to have", Status: "todo", Parent: "e1"},
			},
			wantMilestones:       0,
			wantUnscheduledEpics: 1,
		},
		{
			name: "done items excluded by default",
			nibs: []*nib.Nib{
				{ID: "m1", Type: "milestone", Title: "v1.0", Status: "todo", CreatedAt: &now},
				{ID: "t1", Type: "task", Title: "Done task", Status: "completed", Parent: "m1"},
			},
			includeDone:    false,
			wantMilestones: 0, // milestone has no visible children
		},
		{
			name: "done items included when requested",
			nibs: []*nib.Nib{
				{ID: "m1", Type: "milestone", Title: "v1.0", Status: "todo", CreatedAt: &now},
				{ID: "t1", Type: "task", Title: "Done task", Status: "completed", Parent: "m1"},
			},
			includeDone:    true,
			wantMilestones: 1,
		},
		{
			name: "orphan nib appears in unscheduled other",
			nibs: []*nib.Nib{
				{ID: "m1", Type: "milestone", Title: "v1.0", Status: "todo", CreatedAt: &now},
				{ID: "t1", Type: "task", Title: "Orphan", Status: "todo"}, // no parent link
			},
			wantMilestones:       0, // milestone has no children
			wantUnscheduledOther: 1, // orphan appears in unscheduled
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildRoadmap(tt.nibs, tt.includeDone, nil, nil, cfg)

			if got := len(result.Milestones); got != tt.wantMilestones {
				t.Errorf("got %d milestones, want %d", got, tt.wantMilestones)
			}

			gotUnscheduledEpics := 0
			gotUnscheduledOther := 0
			if result.Unscheduled != nil {
				gotUnscheduledEpics = len(result.Unscheduled.Epics)
				gotUnscheduledOther = len(result.Unscheduled.Other)
			}
			if gotUnscheduledEpics != tt.wantUnscheduledEpics {
				t.Errorf("got %d unscheduled epics, want %d", gotUnscheduledEpics, tt.wantUnscheduledEpics)
			}
			if gotUnscheduledOther != tt.wantUnscheduledOther {
				t.Errorf("got %d unscheduled other, want %d", gotUnscheduledOther, tt.wantUnscheduledOther)
			}
		})
	}
}

// TestBuildRoadmap_Progress pins the canonical % complete per node: the
// milestone rollup counts its direct epic children (scrapped excluded from the
// denominator), and an epic rollup counts its direct task children.
func TestBuildRoadmap_Progress(t *testing.T) {
	cfg := config.Default()
	now := time.Now()

	nibs := []*nib.Nib{
		{ID: "m1", Type: "milestone", Title: "v1.0", Status: "in-progress", CreatedAt: &now},
		{ID: "e1", Type: "epic", Title: "Done Epic", Status: "completed", Parent: "m1"},
		{ID: "e2", Type: "epic", Title: "Active Epic", Status: "in-progress", Parent: "m1"},
		{ID: "e3", Type: "epic", Title: "Scrapped Epic", Status: "scrapped", Parent: "m1"},
		{ID: "t1", Type: "task", Title: "T1", Status: "todo", Parent: "e1"},
		{ID: "t2", Type: "task", Title: "T2", Status: "completed", Parent: "e2"},
		{ID: "t3", Type: "task", Title: "T3", Status: "todo", Parent: "e2"},
	}

	result := buildRoadmap(nibs, true, nil, nil, cfg)
	if len(result.Milestones) != 1 {
		t.Fatalf("got %d milestones, want 1", len(result.Milestones))
	}
	ms := result.Milestones[0]

	// Milestone: 3 direct epic children, 1 completed, 1 scrapped (excluded) ->
	// total 2, done 1, 50%.
	if ms.Progress.Total != 2 || ms.Progress.Done != 1 || ms.Progress.Percent != 50 || ms.Progress.Scrapped != 1 {
		t.Errorf("milestone progress = %+v, want {Total:2, Done:1, Percent:50, Scrapped:1}", ms.Progress)
	}

	// Epic e2: 2 task children, 1 completed -> 50%.
	var e2 *epicGroup
	for i := range ms.Epics {
		if ms.Epics[i].Epic.ID == "e2" {
			e2 = &ms.Epics[i]
		}
	}
	if e2 == nil {
		t.Fatal("epic e2 not found in milestone group")
	}
	if e2.Progress.Total != 2 || e2.Progress.Done != 1 || e2.Progress.Percent != 50 {
		t.Errorf("epic e2 progress = %+v, want {Total:2, Done:1, Percent:50}", e2.Progress)
	}
}

// TestBuildRoadmap_ParkedChildStaysVisible pins the visibility rule for a
// container whose children are all closed. Finished work drops off the roadmap,
// but a deferred child is set aside rather than resolved: it is scope someone
// still has to deal with, and roadmap has no hidden-closed disclosure, so
// dropping it would take live work off the board silently. The discriminator is
// whether the child resolved (completed/scrapped) or is merely parked — never
// whether its parent is still open, because closing a parent does not resolve
// what is parked underneath it.
//
// Each case asserts on the rendered items AND on the progress rollup, because
// the two are one seam: the items a group renders are exactly the children its
// rollup counts in Total but not in Done. Asserting either alone let the two
// disagree in the same rendered line.
func TestBuildRoadmap_ParkedChildStaysVisible(t *testing.T) {
	cfg := config.Default()
	now := time.Now()

	base := func(lastChildStatus string) []*nib.Nib {
		return []*nib.Nib{
			{ID: "m1", Type: "milestone", Title: "v1.0", Status: "in-progress", CreatedAt: &now},
			{ID: "e1", Type: "epic", Title: "Epic", Status: "in-progress", Parent: "m1"},
			{ID: "t1", Type: "task", Title: "T1", Status: "completed", Parent: "e1"},
			{ID: "t2", Type: "task", Title: "T2", Status: "completed", Parent: "e1"},
			{ID: "t3", Type: "task", Title: "T3", Status: "completed", Parent: "e1"},
			{ID: "t4", Type: "task", Title: "T4", Status: lastChildStatus, Parent: "e1"},
		}
	}

	t.Run("epic renders its parked child and reports it as outstanding", func(t *testing.T) {
		result := buildRoadmap(base("deferred"), false, nil, nil, cfg)
		if len(result.Milestones) != 1 {
			t.Fatalf("got %d milestones, want 1 — the milestone lost its only epic", len(result.Milestones))
		}
		epics := result.Milestones[0].Epics
		if len(epics) != 1 || epics[0].Epic.ID != "e1" {
			t.Fatalf("got epics %+v, want the in-progress epic e1 to still be rendered", epics)
		}
		if len(epics[0].Items) != 1 || epics[0].Items[0].ID != "t4" {
			t.Fatalf("got items %+v, want the parked child t4 named", epics[0].Items)
		}
		// 3 completed + 1 deferred: the parked child is scope, so the epic is
		// not finished. 100% here would assert there is nothing left while the
		// line above it lists t4.
		if got := epics[0].Progress; got.Total != 4 || got.Done != 3 || got.Percent != 75 || got.Deferred != 1 {
			t.Errorf("epic progress = %+v, want {Total:4 Done:3 Percent:75 Deferred:1}", got)
		}
	})

	t.Run("epic whose children all resolved still drops", func(t *testing.T) {
		result := buildRoadmap(base("scrapped"), false, nil, nil, cfg)
		if len(result.Milestones) != 0 {
			t.Errorf("got %d milestones, want 0 — finished work drops off the roadmap", len(result.Milestones))
		}
	})

	t.Run("closing the parent does not park-and-hide the child", func(t *testing.T) {
		// The reachable cascade: `nibs close e1` on an epic that still has a
		// deferred child. Closing a parent over parked children is sanctioned
		// (no --force needed), so it must not take the whole milestone off the
		// board — the parked task is still unfinished scope.
		nibs := base("deferred")
		nibs[1].Status = "completed" // the epic itself is closed
		result := buildRoadmap(nibs, false, nil, nil, cfg)
		if len(result.Milestones) != 1 {
			t.Fatalf("got %d milestones, want 1 — closing the epic removed the whole milestone", len(result.Milestones))
		}
		epics := result.Milestones[0].Epics
		if len(epics) != 1 || len(epics[0].Items) != 1 || epics[0].Items[0].ID != "t4" {
			t.Fatalf("got epics %+v, want the closed epic still naming its parked child t4", epics)
		}
	})

	t.Run("scrapped parent over a parked child keeps it too", func(t *testing.T) {
		nibs := base("deferred")
		nibs[1].Status = "scrapped"
		result := buildRoadmap(nibs, false, nil, nil, cfg)
		if len(result.Milestones) != 1 {
			t.Fatalf("got %d milestones, want 1 — scrapping the epic does not resolve its parked child", len(result.Milestones))
		}
	})

	t.Run("milestone keeps a deferred direct child", func(t *testing.T) {
		nibs := []*nib.Nib{
			{ID: "m1", Type: "milestone", Title: "v1.0", Status: "in-progress", CreatedAt: &now},
			{ID: "t1", Type: "task", Title: "Parked", Status: "deferred", Parent: "m1"},
		}
		result := buildRoadmap(nibs, false, nil, nil, cfg)
		if len(result.Milestones) != 1 {
			t.Fatalf("got %d milestones, want 1 — the milestone still holds outstanding scope", len(result.Milestones))
		}
		other := result.Milestones[0].Other
		if len(other) != 1 || other[0].ID != "t1" {
			t.Errorf("got other %+v, want the parked task t1 named", other)
		}
		if got := result.Milestones[0].Progress; got.Total != 1 || got.Done != 0 || got.Percent != 0 {
			t.Errorf("milestone progress = %+v, want {Total:1 Done:0 Percent:0}", got)
		}
	})

	t.Run("unscheduled epic keeps its parked child", func(t *testing.T) {
		// The third call site: an epic with no milestone parent. Same rule.
		nibs := []*nib.Nib{
			{ID: "e1", Type: "epic", Title: "Loose Epic", Status: "in-progress"},
			{ID: "t1", Type: "task", Title: "T1", Status: "completed", Parent: "e1"},
			{ID: "t2", Type: "task", Title: "Parked", Status: "deferred", Parent: "e1"},
		}
		result := buildRoadmap(nibs, false, nil, nil, cfg)
		if result.Unscheduled == nil || len(result.Unscheduled.Epics) != 1 {
			t.Fatalf("got unscheduled %+v, want the epic kept for its parked child", result.Unscheduled)
		}
		epic := result.Unscheduled.Epics[0]
		if len(epic.Items) != 1 || epic.Items[0].ID != "t2" {
			t.Fatalf("got items %+v, want the parked child t2 named", epic.Items)
		}
		if got := epic.Progress; got.Total != 2 || got.Done != 1 || got.Percent != 50 {
			t.Errorf("epic progress = %+v, want {Total:2 Done:1 Percent:50}", got)
		}
	})

	t.Run("deferred container reports the open work it still holds", func(t *testing.T) {
		// A parked container is not empty scope: its open descendants are
		// rendered, so its rollup must not read 100%.
		nibs := []*nib.Nib{
			{ID: "m1", Type: "milestone", Title: "v1.0", Status: "in-progress", CreatedAt: &now},
			{ID: "e1", Type: "epic", Title: "Done Epic", Status: "completed", Parent: "m1"},
			{ID: "e2", Type: "epic", Title: "Parked Epic", Status: "deferred", Parent: "m1"},
			{ID: "t1", Type: "task", Title: "Open one", Status: "todo", Parent: "e2"},
			{ID: "t2", Type: "task", Title: "Open two", Status: "todo", Parent: "e2"},
		}
		result := buildRoadmap(nibs, false, nil, nil, cfg)
		if len(result.Milestones) != 1 {
			t.Fatalf("got %d milestones, want 1", len(result.Milestones))
		}
		ms := result.Milestones[0]
		// 1 completed epic + 1 deferred epic holding live work: 50%, not 100%.
		if got := ms.Progress; got.Total != 2 || got.Done != 1 || got.Percent != 50 || got.Deferred != 1 {
			t.Errorf("milestone progress = %+v, want {Total:2 Done:1 Percent:50 Deferred:1}", got)
		}
		if len(ms.Epics) != 1 || ms.Epics[0].Epic.ID != "e2" {
			t.Fatalf("got epics %+v, want the parked epic e2 rendered with its open tasks", ms.Epics)
		}
		if len(ms.Epics[0].Items) != 2 {
			t.Errorf("got %d items under the parked epic, want 2 open tasks", len(ms.Epics[0].Items))
		}
	})
}

// TestBuildRoadmap_VisibilityMatchesProgress pins the seam itself, over every
// declared status rather than the handful the scenarios above happen to use: a
// child is rendered by the default roadmap exactly when the progress rollup
// counts it as outstanding (in Total, not in Done). If one side gains or loses a
// status without the other, a group renders items while claiming 100%, or claims
// outstanding scope while rendering nothing.
func TestBuildRoadmap_VisibilityMatchesProgress(t *testing.T) {
	cfg := config.Default()
	now := time.Now()

	for _, status := range append(cfg.StatusNames(), "", "bogus") {
		t.Run("status="+status, func(t *testing.T) {
			nibs := []*nib.Nib{
				{ID: "m1", Type: "milestone", Title: "v1.0", Status: "in-progress", CreatedAt: &now},
				{ID: "e1", Type: "epic", Title: "Epic", Status: "in-progress", Parent: "m1"},
				{ID: "t1", Type: "task", Title: "T1", Status: status, Parent: "e1"},
			}
			result := buildRoadmap(nibs, false, nil, nil, cfg)

			rendered := 0
			if len(result.Milestones) == 1 && len(result.Milestones[0].Epics) == 1 {
				rendered = len(result.Milestones[0].Epics[0].Items)
			}
			rollup := graph.ComputeProgress([]string{status})
			outstanding := rollup.Total - rollup.Done

			if rendered != outstanding {
				t.Errorf("status %q: roadmap renders %d item(s) but the rollup reports %d outstanding (%+v) — "+
					"the visibility filter and the progress denominator disagree",
					status, rendered, outstanding, rollup)
			}
		})
	}
}

func TestFirstParagraph(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		want  string
	}{
		{
			name: "empty body",
			body: "",
			want: "",
		},
		{
			name: "single line",
			body: "This is a description.",
			want: "This is a description.",
		},
		{
			name: "multiple paragraphs",
			body: "First paragraph.\n\nSecond paragraph.",
			want: "First paragraph.",
		},
		{
			name: "multiline first paragraph",
			body: "Line one\nLine two\n\nSecond para.",
			want: "Line one Line two",
		},
		{
			name: "skips headers at start",
			body: "## Checklist\n- item one",
			want: "- item one",
		},
		{
			name: "truncates long text",
			body: "This is a very long paragraph that exceeds two hundred characters and needs to be truncated so it does not take up too much space in the roadmap output. Lorem ipsum dolor sit amet consectetur adipiscing elit.",
			want: "This is a very long paragraph that exceeds two hundred characters and needs to be truncated so it does not take up too much space in the roadmap output. Lorem ipsum dolor sit amet consectetur adipi...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := firstParagraph(tt.body)
			if got != tt.want {
				t.Errorf("firstParagraph() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderNibRef(t *testing.T) {
	tests := []struct {
		name       string
		nib       *nib.Nib
		asLink     bool
		linkPrefix string
		want       string
	}{
		{
			name:   "no link - just ID",
			nib:   &nib.Nib{ID: "abc", Path: "abc--milestone.md"},
			asLink: false,
			want:   "(abc)",
		},
		{
			name:       "link without prefix",
			nib:       &nib.Nib{ID: "abc", Path: "abc--milestone.md"},
			asLink:     true,
			linkPrefix: "",
			want:       "([abc](abc--milestone.md))",
		},
		{
			name:       "link with prefix",
			nib:       &nib.Nib{ID: "abc", Path: "abc--milestone.md"},
			asLink:     true,
			linkPrefix: "https://example.com/nibs/",
			want:       "([abc](https://example.com/nibs/abc--milestone.md))",
		},
		{
			name:       "link with prefix without trailing slash",
			nib:       &nib.Nib{ID: "abc", Path: "abc--milestone.md"},
			asLink:     true,
			linkPrefix: ".nibs",
			want:       "([abc](.nibs/abc--milestone.md))",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderNibRef(tt.nib, tt.asLink, tt.linkPrefix)
			if got != tt.want {
				t.Errorf("renderNibRef() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStatusFiltering(t *testing.T) {
	cfg := config.Default()

	now := time.Now()
	nibs := []*nib.Nib{
		{ID: "m1", Type: "milestone", Title: "Todo Milestone", Status: "todo", CreatedAt: &now},
		{ID: "m2", Type: "milestone", Title: "In Progress Milestone", Status: "in-progress", CreatedAt: &now},
		{ID: "t1", Type: "task", Title: "Task 1", Status: "todo", Parent: "m1"},
		{ID: "t2", Type: "task", Title: "Task 2", Status: "todo", Parent: "m2"},
	}

	t.Run("filter by status", func(t *testing.T) {
		result := buildRoadmap(nibs, false, []string{"todo"}, nil, cfg)
		if len(result.Milestones) != 1 {
			t.Errorf("expected 1 milestone, got %d", len(result.Milestones))
		}
		if result.Milestones[0].Milestone.Status != "todo" {
			t.Errorf("expected todo milestone, got %s", result.Milestones[0].Milestone.Status)
		}
	})

	t.Run("exclude by status", func(t *testing.T) {
		result := buildRoadmap(nibs, false, nil, []string{"in-progress"}, cfg)
		if len(result.Milestones) != 1 {
			t.Errorf("expected 1 milestone, got %d", len(result.Milestones))
		}
		if result.Milestones[0].Milestone.Status != "todo" {
			t.Errorf("expected todo milestone, got %s", result.Milestones[0].Milestone.Status)
		}
	})
}
