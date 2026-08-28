package cmd

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibtypes"
	"github.com/alphaleonis/nibs/internal/progress"
)

// roadmapQueueIDs projects a queue — a milestone's or the backlog's — to the
// ids standing at each position: the one list the section is supposed to be.
// An entry that expands contributes its own id and not its items', which stay
// nested under it; read those from the entry's Items.
func roadmapQueueIDs(queue []queueEntry) []string {
	ids := make([]string, 0, len(queue))
	for _, e := range queue {
		ids = append(ids, e.Nib.ID)
	}
	return ids
}

// roadmapQueueEpics returns the entries that expand into a decomposition, for
// the assertions that are about visibility or progress rather than position.
func roadmapQueueEpics(queue []queueEntry) []queueEntry {
	var epics []queueEntry
	for _, e := range queue {
		if len(e.Items) > 0 {
			epics = append(epics, e)
		}
	}
	return epics
}

// roadmapQueueItems returns the members standing alone at their position.
func roadmapQueueItems(queue []queueEntry) []*nib.Nib {
	var items []*nib.Nib
	for _, e := range queue {
		if len(e.Items) == 0 {
			items = append(items, e.Nib)
		}
	}
	return items
}

func TestBuildRoadmap(t *testing.T) {
	cfg := config.Default()

	now := time.Now()

	tests := []struct {
		name             string
		nibs             []*nib.Nib
		includeDone      bool
		wantMilestones   int
		wantBacklogEpics int
		wantBacklogOther int
	}{
		{
			name:           "empty nibs",
			nibs:           []*nib.Nib{},
			wantMilestones: 0,
		},
		{
			name: "milestone with epic and items",
			nibs: []*nib.Nib{
				{ID: "m1", Type: "milestone", Title: "v1.0", Status: "todo", CreatedAt: &now},
				{ID: "e1", Type: "epic", Title: "Auth", Status: "todo", Milestone: "m1"},
				{ID: "t1", Type: "task", Title: "Login", Status: "todo", Parent: "e1"},
			},
			wantMilestones: 1,
		},
		{
			name: "milestone with direct children (no epic)",
			nibs: []*nib.Nib{
				{ID: "m1", Type: "milestone", Title: "v1.0", Status: "todo", CreatedAt: &now},
				{ID: "t1", Type: "task", Title: "Docs", Status: "todo", Milestone: "m1"},
			},
			wantMilestones: 1,
		},
		{
			name: "backlog epic",
			nibs: []*nib.Nib{
				{ID: "e1", Type: "epic", Title: "Future", Status: "todo"},
				{ID: "t1", Type: "task", Title: "Nice to have", Status: "todo", Parent: "e1"},
			},
			wantMilestones:   0,
			wantBacklogEpics: 1,
		},
		{
			name: "done items excluded by default",
			nibs: []*nib.Nib{
				{ID: "m1", Type: "milestone", Title: "v1.0", Status: "todo", CreatedAt: &now},
				{ID: "t1", Type: "task", Title: "Done task", Status: "completed", Milestone: "m1"},
			},
			includeDone:    false,
			wantMilestones: 0, // milestone has no visible children
		},
		{
			name: "done items included when requested",
			nibs: []*nib.Nib{
				{ID: "m1", Type: "milestone", Title: "v1.0", Status: "todo", CreatedAt: &now},
				{ID: "t1", Type: "task", Title: "Done task", Status: "completed", Milestone: "m1"},
			},
			includeDone:    true,
			wantMilestones: 1,
		},
		{
			name: "orphan nib appears in the backlog",
			nibs: []*nib.Nib{
				{ID: "m1", Type: "milestone", Title: "v1.0", Status: "todo", CreatedAt: &now},
				{ID: "t1", Type: "task", Title: "Orphan", Status: "todo"}, // no parent link
			},
			wantMilestones:   0, // milestone has no children
			wantBacklogOther: 1, // orphan appears in the backlog
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildRoadmap(tt.nibs, tt.includeDone, nil, nil, cfg)

			if got := len(result.Milestones); got != tt.wantMilestones {
				t.Errorf("got %d milestones, want %d", got, tt.wantMilestones)
			}

			gotBacklogEpics := 0
			gotBacklogOther := 0
			if result.Backlog != nil {
				gotBacklogEpics = len(roadmapQueueEpics(result.Backlog.Queue))
				gotBacklogOther = len(roadmapQueueItems(result.Backlog.Queue))
			}
			if gotBacklogEpics != tt.wantBacklogEpics {
				t.Errorf("got %d backlog epics, want %d", gotBacklogEpics, tt.wantBacklogEpics)
			}
			if gotBacklogOther != tt.wantBacklogOther {
				t.Errorf("got %d backlog items, want %d", gotBacklogOther, tt.wantBacklogOther)
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
		{ID: "e1", Type: "epic", Title: "Done Epic", Status: "completed", Milestone: "m1"},
		{ID: "e2", Type: "epic", Title: "Active Epic", Status: "in-progress", Milestone: "m1"},
		{ID: "e3", Type: "epic", Title: "Scrapped Epic", Status: "scrapped", Milestone: "m1"},
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
	var e2 *queueEntry
	epics := roadmapQueueEpics(ms.Queue)
	for i := range epics {
		if epics[i].Nib.ID == "e2" {
			e2 = &epics[i]
		}
	}
	if e2 == nil {
		t.Fatal("epic e2 not found in milestone group")
	}
	if e2.Progress.Total != 2 || e2.Progress.Done != 1 || e2.Progress.Percent != 50 {
		t.Errorf("epic e2 progress = %+v, want {Total:2, Done:1, Percent:50}", e2.Progress)
	}
}

// TestBuildRoadmap_DeferredChildStaysVisible pins the visibility rule for a
// container whose children are all closed. Finished work drops off the roadmap,
// but a deferred child is set aside rather than resolved: it is scope someone
// still has to deal with, and roadmap has no hidden-closed disclosure, so
// dropping it would take live work off the board silently. The discriminator is
// whether the child resolved (completed/scrapped) or is merely deferred — never
// whether its parent is still open, because closing a parent does not resolve
// what is set aside underneath it.
//
// Each case asserts on the rendered items AND on the progress rollup, because
// the two are one seam: the items a group renders are exactly the children its
// rollup counts in Total but not in Done. Asserting either alone let the two
// disagree in the same rendered line.
func TestBuildRoadmap_DeferredChildStaysVisible(t *testing.T) {
	cfg := config.Default()
	now := time.Now()

	base := func(lastChildStatus string) []*nib.Nib {
		return []*nib.Nib{
			{ID: "m1", Type: "milestone", Title: "v1.0", Status: "in-progress", CreatedAt: &now},
			{ID: "e1", Type: "epic", Title: "Epic", Status: "in-progress", Milestone: "m1"},
			{ID: "t1", Type: "task", Title: "T1", Status: "completed", Parent: "e1"},
			{ID: "t2", Type: "task", Title: "T2", Status: "completed", Parent: "e1"},
			{ID: "t3", Type: "task", Title: "T3", Status: "completed", Parent: "e1"},
			{ID: "t4", Type: "task", Title: "T4", Status: lastChildStatus, Parent: "e1"},
		}
	}

	t.Run("epic renders its deferred child and reports it as outstanding", func(t *testing.T) {
		result := buildRoadmap(base("deferred"), false, nil, nil, cfg)
		if len(result.Milestones) != 1 {
			t.Fatalf("got %d milestones, want 1 — the milestone lost its only epic", len(result.Milestones))
		}
		epics := roadmapQueueEpics(result.Milestones[0].Queue)
		if len(epics) != 1 || epics[0].Nib.ID != "e1" {
			t.Fatalf("got epics %+v, want the in-progress epic e1 to still be rendered", epics)
		}
		if len(epics[0].Items) != 1 || epics[0].Items[0].ID != "t4" {
			t.Fatalf("got items %+v, want the deferred child t4 named", epics[0].Items)
		}
		// 3 completed + 1 deferred: the deferred child is scope, so the epic is
		// not finished. 100% here would assert there is nothing left while the
		// line above it lists t4.
		if got := epics[0].Progress; got.Total != 4 || got.Done != 3 || got.Percent != 75 || got.Deferred != 1 {
			t.Errorf("epic progress = %+v, want {Total:4 Done:3 Percent:75 Deferred:1}", got)
		}
	})

	t.Run("an OPEN container whose children all resolved stands alone", func(t *testing.T) {
		// It has no scope left under it, but it is open work in its own right:
		// the milestone's rollup counts it in Total and not in Done, and at a
		// startable status `nibs next` hands it back as THE action — decision
		// 2.4, "all-children-closed makes the container itself the action".
		// Measured on a store of this shape with e1 at todo: next answers e1
		// while the roadmap rendered an empty document, which made the roadmap
		// the one surface that could not see the thing next told you to do.
		result := buildRoadmap(base("scrapped"), false, nil, nil, cfg)
		if len(result.Milestones) != 1 {
			t.Fatalf("got %d milestones, want 1 — the in-progress epic is still open work", len(result.Milestones))
		}
		queue := result.Milestones[0].Queue
		if len(queue) != 1 || queue[0].Nib.ID != "e1" {
			t.Fatalf("got queue %+v, want the epic e1 standing alone", queue)
		}
		if len(queue[0].Items) != 0 {
			t.Errorf("got items %+v, want none — every child is resolved", queue[0].Items)
		}
	})

	t.Run("a CLOSED container whose children all resolved drops", func(t *testing.T) {
		// The half of the old rule that was right: nothing here is outstanding,
		// so the milestone has no content and leaves the board.
		nibs := base("scrapped")
		nibs[1].Status = "completed"
		result := buildRoadmap(nibs, false, nil, nil, cfg)
		if len(result.Milestones) != 0 {
			t.Errorf("got %d milestones, want 0 — finished work drops off the roadmap", len(result.Milestones))
		}
	})

	t.Run("closing the parent does not hide the deferred child", func(t *testing.T) {
		// The reachable cascade: `nibs close e1` on an epic that still has a
		// deferred child. Closing a parent over deferred children is sanctioned
		// (no --force needed), so it must not take the whole milestone off the
		// board — the deferred task is still unfinished scope.
		nibs := base("deferred")
		nibs[1].Status = "completed" // the epic itself is closed
		result := buildRoadmap(nibs, false, nil, nil, cfg)
		if len(result.Milestones) != 1 {
			t.Fatalf("got %d milestones, want 1 — closing the epic removed the whole milestone", len(result.Milestones))
		}
		epics := roadmapQueueEpics(result.Milestones[0].Queue)
		if len(epics) != 1 || len(epics[0].Items) != 1 || epics[0].Items[0].ID != "t4" {
			t.Fatalf("got epics %+v, want the closed epic still naming its deferred child t4", epics)
		}
	})

	t.Run("scrapped parent over a deferred child keeps it too", func(t *testing.T) {
		nibs := base("deferred")
		nibs[1].Status = "scrapped"
		result := buildRoadmap(nibs, false, nil, nil, cfg)
		if len(result.Milestones) != 1 {
			t.Fatalf("got %d milestones, want 1 — scrapping the epic does not resolve its deferred child", len(result.Milestones))
		}
	})

	t.Run("milestone keeps a deferred direct child", func(t *testing.T) {
		nibs := []*nib.Nib{
			{ID: "m1", Type: "milestone", Title: "v1.0", Status: "in-progress", CreatedAt: &now},
			{ID: "t1", Type: "task", Title: "Set Aside", Status: "deferred", Milestone: "m1"},
		}
		result := buildRoadmap(nibs, false, nil, nil, cfg)
		if len(result.Milestones) != 1 {
			t.Fatalf("got %d milestones, want 1 — the milestone still holds outstanding scope", len(result.Milestones))
		}
		other := roadmapQueueItems(result.Milestones[0].Queue)
		if len(other) != 1 || other[0].ID != "t1" {
			t.Errorf("got other %+v, want the deferred task t1 named", other)
		}
		if got := result.Milestones[0].Progress; got.Total != 1 || got.Done != 0 || got.Percent != 0 {
			t.Errorf("milestone progress = %+v, want {Total:1 Done:0 Percent:0}", got)
		}
	})

	t.Run("a backlog epic keeps its deferred child", func(t *testing.T) {
		// The third call site: an epic with no milestone parent. Same rule.
		nibs := []*nib.Nib{
			{ID: "e1", Type: "epic", Title: "Loose Epic", Status: "in-progress"},
			{ID: "t1", Type: "task", Title: "T1", Status: "completed", Parent: "e1"},
			{ID: "t2", Type: "task", Title: "Set Aside", Status: "deferred", Parent: "e1"},
		}
		result := buildRoadmap(nibs, false, nil, nil, cfg)
		if result.Backlog == nil {
			t.Fatal("got no backlog group, want the epic kept for its deferred child")
		}
		epics := roadmapQueueEpics(result.Backlog.Queue)
		if len(epics) != 1 {
			t.Fatalf("got backlog %+v, want the epic kept for its deferred child", result.Backlog.Queue)
		}
		if len(epics[0].Items) != 1 || epics[0].Items[0].ID != "t2" {
			t.Fatalf("got items %+v, want the deferred child t2 named", epics[0].Items)
		}
		if got := *epics[0].Progress; got.Total != 2 || got.Done != 1 || got.Percent != 50 {
			t.Errorf("epic progress = %+v, want {Total:2 Done:1 Percent:50}", got)
		}
	})

	t.Run("deferred container reports the open work it still holds", func(t *testing.T) {
		// A deferred container is not empty scope: its open descendants are
		// rendered, so its rollup must not read 100%.
		nibs := []*nib.Nib{
			{ID: "m1", Type: "milestone", Title: "v1.0", Status: "in-progress", CreatedAt: &now},
			{ID: "e1", Type: "epic", Title: "Done Epic", Status: "completed", Milestone: "m1"},
			{ID: "e2", Type: "epic", Title: "Deferred Epic", Status: "deferred", Milestone: "m1"},
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
		epics := roadmapQueueEpics(ms.Queue)
		if len(epics) != 1 || epics[0].Nib.ID != "e2" {
			t.Fatalf("got epics %+v, want the deferred epic e2 rendered with its open tasks", epics)
		}
		if len(epics[0].Items) != 2 {
			t.Errorf("got %d items under the deferred epic, want 2 open tasks", len(epics[0].Items))
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
				{ID: "e1", Type: "epic", Title: "Epic", Status: "in-progress", Milestone: "m1"},
				{ID: "t1", Type: "task", Title: "T1", Status: status, Parent: "e1"},
			}
			result := buildRoadmap(nibs, false, nil, nil, cfg)

			rendered := 0
			if len(result.Milestones) == 1 {
				if epics := roadmapQueueEpics(result.Milestones[0].Queue); len(epics) == 1 {
					rendered = len(epics[0].Items)
				}
			}
			rollup := progress.ByCount([]string{status})
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
		name string
		body string
		want string
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
		nib        *nib.Nib
		asLink     bool
		linkPrefix string
		want       string
	}{
		{
			name:   "no link - just ID",
			nib:    &nib.Nib{ID: "abc", Path: "abc--milestone.md"},
			asLink: false,
			want:   "(abc)",
		},
		{
			name:       "link without prefix",
			nib:        &nib.Nib{ID: "abc", Path: "abc--milestone.md"},
			asLink:     true,
			linkPrefix: "",
			want:       "([abc](abc--milestone.md))",
		},
		{
			name:       "link with prefix",
			nib:        &nib.Nib{ID: "abc", Path: "abc--milestone.md"},
			asLink:     true,
			linkPrefix: "https://example.com/nibs/",
			want:       "([abc](https://example.com/nibs/abc--milestone.md))",
		},
		{
			name:       "link with prefix without trailing slash",
			nib:        &nib.Nib{ID: "abc", Path: "abc--milestone.md"},
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
		{ID: "t1", Type: "task", Title: "Task 1", Status: "todo", Milestone: "m1"},
		{ID: "t2", Type: "task", Title: "Task 2", Status: "todo", Milestone: "m2"},
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

// TestBuildRoadmap_HiddenMilestoneMembersAreNotBacklog pins the roadmap's side
// of membership ledger delta (d): work under a milestone the status filter
// hides is SCHEDULED work the filter chose not to show — it must not leak into
// the backlog. The old two-level walk computed "under a milestone"
// from the filtered list, so hiding a milestone dumped its epics into the
// backlog.
func TestBuildRoadmap_HiddenMilestoneMembersAreNotBacklog(t *testing.T) {
	cfg := config.Default()
	nibs := []*nib.Nib{
		{ID: "m1", Title: "Shipped", Type: "milestone", Status: "completed"},
		{ID: "e1", Title: "Epic under shipped", Type: "epic", Status: "in-progress", Milestone: "m1"},
		{ID: "t1", Title: "Task", Type: "task", Status: "todo", Parent: "e1"},
	}

	data := buildRoadmap(nibs, false, nil, []string{"completed"}, cfg)

	if len(data.Milestones) != 0 {
		t.Fatalf("the completed milestone is filtered out, got %d milestone groups", len(data.Milestones))
	}
	if data.Backlog != nil {
		t.Fatalf("members of the hidden milestone leaked into the backlog: %+v", data.Backlog)
	}
}

// TestBuildRoadmap_MilestoneQueueIsOneOrderedList pins the milestone section as
// a QUEUE: its direct members in milestone_order, epic-typed or not, standing
// at the positions their keys give them. The sample fixture cannot reach this
// shape — every milestone in it holds only epic assignees — so the coverage
// here is synthetic by necessity.
func TestBuildRoadmap_MilestoneQueueIsOneOrderedList(t *testing.T) {
	cfg := config.Default()
	now := time.Now()

	milestone := &nib.Nib{ID: "m1", Type: "milestone", Title: "v1.0", Status: "in-progress", CreatedAt: &now}

	tests := []struct {
		name    string
		members []*nib.Nib
		want    []string
	}{
		{
			name: "an epic stands between two loose items",
			members: []*nib.Nib{
				{ID: "e1", Type: "epic", Title: "Epic", Status: "todo", Milestone: "m1", MilestoneOrder: "b"},
				{ID: "b1", Type: "bug", Title: "Loose bug", Status: "todo", Milestone: "m1", MilestoneOrder: "a"},
				{ID: "f1", Type: "feature", Title: "Loose feature", Status: "todo", Milestone: "m1", MilestoneOrder: "c"},
			},
			want: []string{"b1", "e1", "f1"},
		},
		{
			name: "loose work interleaves between two epics",
			members: []*nib.Nib{
				{ID: "b1", Type: "bug", Title: "Loose bug", Status: "todo", Milestone: "m1", MilestoneOrder: "d"},
				{ID: "e2", Type: "epic", Title: "Second epic", Status: "todo", Milestone: "m1", MilestoneOrder: "c"},
				{ID: "t1", Type: "task", Title: "Loose task", Status: "todo", Milestone: "m1", MilestoneOrder: "b"},
				{ID: "e1", Type: "epic", Title: "First epic", Status: "todo", Milestone: "m1", MilestoneOrder: "a"},
			},
			want: []string{"e1", "t1", "e2", "b1"},
		},
		{
			name: "type never outranks the key the user arranged",
			members: []*nib.Nib{
				// Declared bug-feature-task, which is both the input order and
				// type-then-status order, while the queue keys ask for the
				// reverse: no sort at all and a type sort both fail this case.
				{ID: "b1", Type: "bug", Title: "Bug", Status: "todo", Milestone: "m1", MilestoneOrder: "c"},
				{ID: "f1", Type: "feature", Title: "Feature", Status: "todo", Milestone: "m1", MilestoneOrder: "b"},
				{ID: "t1", Type: "task", Title: "Task", Status: "todo", Milestone: "m1", MilestoneOrder: "a"},
			},
			want: []string{"t1", "f1", "b1"},
		},
		{
			name: "unkeyed members fall behind the keyed ones, by title",
			members: []*nib.Nib{
				{ID: "b1", Type: "bug", Title: "Zebra", Status: "todo", Milestone: "m1"},
				{ID: "e1", Type: "epic", Title: "Apple", Status: "todo", Milestone: "m1"},
				{ID: "t1", Type: "task", Title: "Keyed", Status: "todo", Milestone: "m1", MilestoneOrder: "a"},
			},
			want: []string{"t1", "e1", "b1"},
		},
		{
			name: "an epic with no outstanding scope leaves the queue without moving the rest",
			members: []*nib.Nib{
				{ID: "b1", Type: "bug", Title: "Loose bug", Status: "todo", Milestone: "m1", MilestoneOrder: "a"},
				{ID: "e1", Type: "epic", Title: "Finished epic", Status: "completed", Milestone: "m1", MilestoneOrder: "b"},
				{ID: "f1", Type: "feature", Title: "Loose feature", Status: "todo", Milestone: "m1", MilestoneOrder: "c"},
			},
			want: []string{"b1", "f1"},
		},
		{
			name: "a finished loose member leaves the queue too",
			members: []*nib.Nib{
				{ID: "b1", Type: "bug", Title: "Loose bug", Status: "todo", Milestone: "m1", MilestoneOrder: "a"},
				{ID: "t1", Type: "task", Title: "Shipped", Status: "completed", Milestone: "m1", MilestoneOrder: "b"},
				{ID: "f1", Type: "feature", Title: "Loose feature", Status: "todo", Milestone: "m1", MilestoneOrder: "c"},
			},
			want: []string{"b1", "f1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibs := []*nib.Nib{milestone}
			nibs = append(nibs, tt.members...)
			// Every epic in the table needs one open child, or it would drop for
			// holding no outstanding scope rather than for the reason under test.
			for _, m := range tt.members {
				if m.EffectiveType() == "epic" && m.Status != "completed" {
					nibs = append(nibs, &nib.Nib{ID: m.ID + "-child", Type: "task", Title: "Child of " + m.ID, Status: "todo", Parent: m.ID})
				}
			}

			result := buildRoadmap(nibs, false, nil, nil, cfg)
			if len(result.Milestones) != 1 {
				t.Fatalf("got %d milestone groups, want 1", len(result.Milestones))
			}
			got := roadmapQueueIDs(result.Milestones[0].Queue)
			if !slices.Equal(got, tt.want) {
				t.Errorf("queue = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestBuildRoadmap_EpicItemsAreTreeOrder pins the other axis: an epic standing
// in a milestone's queue expands into its own decomposition, and that is
// ordered by the parent-scope `order` key, not by the queue key and not by
// type. The two axes coexist in one section and neither borrows the other.
func TestBuildRoadmap_EpicItemsAreTreeOrder(t *testing.T) {
	cfg := config.Default()
	now := time.Now()

	nibs := []*nib.Nib{
		{ID: "m1", Type: "milestone", Title: "v1.0", Status: "in-progress", CreatedAt: &now},
		{ID: "e1", Type: "epic", Title: "Epic", Status: "in-progress", Milestone: "m1", MilestoneOrder: "a"},
		// Declared bug-feature-task, which is both the input order and
		// type-then-status order, while the order keys ask for the reverse.
		// The titles run opposite to the keys as well, so CompareByKey's
		// title tiebreak cannot stand in for reading the key.
		{ID: "b1", Type: "bug", Title: "Alpha", Status: "todo", Parent: "e1", Order: "c"},
		{ID: "f1", Type: "feature", Title: "Mike", Status: "todo", Parent: "e1", Order: "b"},
		{ID: "t1", Type: "task", Title: "Zulu", Status: "todo", Parent: "e1", Order: "a"},
	}

	result := buildRoadmap(nibs, false, nil, nil, cfg)
	epics := roadmapQueueEpics(result.Milestones[0].Queue)
	if len(epics) != 1 {
		t.Fatalf("got %d epic groups, want 1", len(epics))
	}
	var got []string
	for _, item := range epics[0].Items {
		got = append(got, item.ID)
	}
	if want := []string{"t1", "f1", "b1"}; !slices.Equal(got, want) {
		t.Errorf("epic items = %v, want %v (tree order)", got, want)
	}
}

// TestBuildRoadmap_BacklogIsTreeOrder pins decision 2.5: the backlog is the
// tree, filtered — so its epics and its root-level items sit in `order`, the
// key the tree itself is arranged by, standing in ONE list together because
// the tree makes them siblings in the same root scope.
func TestBuildRoadmap_BacklogIsTreeOrder(t *testing.T) {
	cfg := config.Default()

	t.Run("root-level items", func(t *testing.T) {
		nibs := []*nib.Nib{
			// Declared bug-feature-task, which is both the input order and
			// type-then-status order, while the order keys ask for the reverse.
			{ID: "b1", Type: "bug", Title: "Bug", Status: "todo", Order: "c"},
			{ID: "f1", Type: "feature", Title: "Feature", Status: "todo", Order: "b"},
			{ID: "t1", Type: "task", Title: "Task", Status: "todo", Order: "a"},
		}
		result := buildRoadmap(nibs, false, nil, nil, cfg)
		if result.Backlog == nil {
			t.Fatal("got no backlog group")
		}
		got := roadmapQueueIDs(result.Backlog.Queue)
		if want := []string{"t1", "f1", "b1"}; !slices.Equal(got, want) {
			t.Errorf("backlog items = %v, want %v (tree order)", got, want)
		}
	})

	t.Run("epic groups", func(t *testing.T) {
		nibs := []*nib.Nib{
			// Declared Apple-before-Zebra, which is both the input order and
			// title order, while the order keys ask for the reverse.
			{ID: "e2", Type: "epic", Title: "Apple", Status: "todo", Order: "b"},
			{ID: "e1", Type: "epic", Title: "Zebra", Status: "todo", Order: "a"},
			{ID: "t2", Type: "task", Title: "Under apple", Status: "todo", Parent: "e2"},
			{ID: "t1", Type: "task", Title: "Under zebra", Status: "todo", Parent: "e1"},
		}
		result := buildRoadmap(nibs, false, nil, nil, cfg)
		if result.Backlog == nil {
			t.Fatal("got no backlog group")
		}
		got := roadmapQueueIDs(result.Backlog.Queue)
		if want := []string{"e1", "e2"}; !slices.Equal(got, want) {
			t.Errorf("backlog epics = %v, want %v (tree order)", got, want)
		}
	})

	t.Run("an epic stands between two root items", func(t *testing.T) {
		// Backlog epics and backlog root items are siblings in ONE root order
		// scope, so the epic keeps the position the tree gives it instead of
		// being hoisted ahead of both tasks. Titles run opposite to the keys,
		// so the title tiebreak cannot reproduce the wanted sequence.
		nibs := []*nib.Nib{
			{ID: "t1", Type: "task", Title: "Zulu", Status: "todo", Order: "a"},
			{ID: "e1", Type: "epic", Title: "Mike", Status: "todo", Order: "b"},
			{ID: "t2", Type: "task", Title: "Alpha", Status: "todo", Order: "c"},
			{ID: "c1", Type: "task", Title: "Under the epic", Status: "todo", Parent: "e1"},
		}
		result := buildRoadmap(nibs, false, nil, nil, cfg)
		if result.Backlog == nil {
			t.Fatal("got no backlog group")
		}
		got := roadmapQueueIDs(result.Backlog.Queue)
		if want := []string{"t1", "e1", "t2"}; !slices.Equal(got, want) {
			t.Errorf("backlog = %v, want %v (tree order)", got, want)
		}

		// And the same interleaving survives into the rendered document — two
		// sequential blocks would read as epic-then-everything-else whatever
		// the data said.
		marks := roadmapMarks(renderRoadmapMarkdown(result, false, ""))
		want := []string{"(t1)", "(e1)", "  (c1)", "(t2)"}
		if !slices.Equal(marks, want) {
			t.Errorf("rendered backlog = %#v, want %#v", marks, want)
		}
	})

	t.Run("items under a backlog epic", func(t *testing.T) {
		nibs := []*nib.Nib{
			{ID: "e1", Type: "epic", Title: "Epic", Status: "todo"},
			// Declared bug-first, which is both the input order and
			// type-then-status order, while the order keys ask for the
			// reverse — and the titles run opposite to the keys too, so the
			// title tiebreak cannot stand in for reading the key.
			{ID: "b1", Type: "bug", Title: "Apple", Status: "todo", Parent: "e1", Order: "b"},
			{ID: "t1", Type: "task", Title: "Zebra", Status: "todo", Parent: "e1", Order: "a"},
		}
		result := buildRoadmap(nibs, false, nil, nil, cfg)
		if result.Backlog == nil {
			t.Fatal("got no backlog group")
		}
		epics := roadmapQueueEpics(result.Backlog.Queue)
		if len(epics) != 1 {
			t.Fatalf("got backlog %+v, want one epic group", result.Backlog.Queue)
		}
		var got []string
		for _, item := range epics[0].Items {
			got = append(got, item.ID)
		}
		if want := []string{"t1", "b1"}; !slices.Equal(got, want) {
			t.Errorf("backlog epic items = %v, want %v (tree order)", got, want)
		}
	})
}

// TestBuildRoadmap_QueueOrderDoesNotMoveProgress pins the seam the queue rework
// must not disturb: the milestone rollup counts the milestone's assigned set,
// whole, whatever the queue looks like and whatever the display filters hide.
func TestBuildRoadmap_QueueOrderDoesNotMoveProgress(t *testing.T) {
	cfg := config.Default()
	now := time.Now()

	nibs := []*nib.Nib{
		{ID: "m1", Type: "milestone", Title: "v1.0", Status: "in-progress", CreatedAt: &now},
		{ID: "e1", Type: "epic", Title: "Epic", Status: "in-progress", Milestone: "m1", MilestoneOrder: "b"},
		{ID: "t9", Type: "task", Title: "Under the epic", Status: "todo", Parent: "e1"},
		{ID: "b1", Type: "bug", Title: "Loose bug", Status: "todo", Milestone: "m1", MilestoneOrder: "a"},
		{ID: "t1", Type: "task", Title: "Shipped", Status: "completed", Milestone: "m1", MilestoneOrder: "c"},
		{ID: "t2", Type: "task", Title: "Dropped", Status: "scrapped", Milestone: "m1", MilestoneOrder: "d"},
	}

	// 4 assignees: 1 completed, 1 scrapped (out of the denominator), 2 open.
	want := progress.Rollup{Total: 3, Done: 1, Percent: 33, Scrapped: 1}

	for _, includeDone := range []bool{false, true} {
		t.Run(fmt.Sprintf("includeDone=%v", includeDone), func(t *testing.T) {
			result := buildRoadmap(nibs, includeDone, nil, nil, cfg)
			if len(result.Milestones) != 1 {
				t.Fatalf("got %d milestone groups, want 1", len(result.Milestones))
			}
			if got := result.Milestones[0].Progress; got != want {
				t.Errorf("milestone progress = %+v, want %+v — the rollup follows the assigned set, not the rendered queue", got, want)
			}
		})
	}
}

// TestRenderRoadmapMarkdown_NoHeadingRepeatsInsideASection renders the shape
// that used to mint duplicate anchors — a queue alternating containers and
// loose runs — and holds every heading in a section to appearing once.
//
// A repeated heading is not merely untidy: the roadmap is meant to be
// committed and linked into, and two identically-named sections in one
// document give a reader no way to say which one they meant. The queue's
// members carry no heading of their own now, so the only headings a section
// holds are its own `##` line; the assertion is written over ALL headings
// rather than that fact, so it keeps its meaning if a future block earns one.
func TestRenderRoadmapMarkdown_NoHeadingRepeatsInsideASection(t *testing.T) {
	cfg := config.Default()
	now := time.Now()

	nibs := []*nib.Nib{
		{ID: "m1", Type: "milestone", Title: "v1.0", Status: "in-progress", CreatedAt: &now},
		{ID: "e1", Type: "epic", Title: "First epic", Status: "todo", Milestone: "m1", MilestoneOrder: "a"},
		{ID: "t1", Type: "task", Title: "Under first", Status: "todo", Parent: "e1"},
		{ID: "l1", Type: "task", Title: "First loose", Status: "todo", Milestone: "m1", MilestoneOrder: "b"},
		{ID: "e2", Type: "epic", Title: "Second epic", Status: "todo", Milestone: "m1", MilestoneOrder: "c"},
		{ID: "t2", Type: "task", Title: "Under second", Status: "todo", Parent: "e2"},
		{ID: "l2", Type: "task", Title: "Second loose", Status: "todo", Milestone: "m1", MilestoneOrder: "d"},
		{ID: "l3", Type: "task", Title: "Third loose", Status: "todo", Milestone: "m1", MilestoneOrder: "e"},
	}

	md := renderRoadmapMarkdown(buildRoadmap(nibs, false, nil, nil, cfg), false, "")

	// The queue must actually be interleaved, or the document under test is
	// not the one that produced duplicates.
	if marks := roadmapMarks(md); !slices.Equal(marks, []string{"(e1)", "  (t1)", "(l1)", "(e2)", "  (t2)", "(l2)", "(l3)"}) {
		t.Fatalf("the fixture did not render an interleaved queue: %#v", marks)
	}

	seen := map[string]int{}
	for _, line := range strings.Split(md, "\n") {
		if !strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "## ") {
			seen = map[string]int{} // a new section starts its own namespace
		}
		seen[line]++
		if seen[line] > 1 {
			t.Errorf("heading %q appears %d times in one section — a reader linking to it cannot say which:\n%s", line, seen[line], md)
		}
	}
}

// TestRoadmapNamesTheBacklogTheSameWayEverywhere pins the one thing this set
// kept getting wrong: it had four names at once — `## Unplanned` in Markdown,
// `unscheduled` in JSON, `Unscheduled()`/`Remainder` in internal/membership,
// and `--backlog` on list, cheat and catalog.
//
// The flag is the authority rather than a literal spelled here, so the
// assertion cannot drift with the thing it guards: if `--backlog` is ever
// renamed, this fails until the roadmap follows.
func TestRoadmapNamesTheBacklogTheSameWayEverywhere(t *testing.T) {
	cfg := config.Default()
	now := time.Now()
	nibs := []*nib.Nib{
		{ID: "m1", Type: "milestone", Title: "v1.0", Status: "in-progress", CreatedAt: &now},
		{ID: "e1", Type: "epic", Title: "Assigned", Status: "todo", Milestone: "m1", MilestoneOrder: "a"},
		{ID: "t1", Type: "task", Title: "Under the epic", Status: "todo", Parent: "e1"},
		{ID: "t2", Type: "task", Title: "In no milestone", Status: "todo", Order: "a"},
	}

	flag := listCmd.Flags().Lookup("backlog")
	if flag == nil {
		t.Fatal("list has no --backlog flag; this guard compares the roadmap against it")
	}
	want := flag.Name

	data := buildRoadmap(nibs, false, nil, nil, cfg)
	if data.Backlog == nil {
		t.Fatal("the fixture produced no backlog, so neither surface names anything")
	}

	// The Markdown heading.
	md := renderRoadmapMarkdown(data, false, "")
	var heading string
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(line, "## ") && !strings.HasPrefix(line, "## Milestone:") {
			heading = strings.ToLower(strings.TrimPrefix(line, "## "))
		}
	}
	if heading != want {
		t.Errorf("roadmap Markdown heads the backlog %q, but list calls it --%s:\n%s", heading, want, md)
	}

	// The JSON key, read off the encoded payload rather than the struct tag, so
	// the wire format is what is checked.
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload[want]; !ok {
		keys := make([]string, 0, len(payload))
		for k := range payload {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		t.Errorf("roadmap --json has no %q key; it carries %v, and list calls this set --%s", want, keys, want)
	}
}

// TestBuildRoadmap_EveryContainerTypeExpands is the regression guard for a
// milestone member whose children appeared in no view at all.
//
// The roadmap used to expand `EffectiveType() == "epic"` and nothing else,
// while nibs declares THREE container types. A feature or bug assigned to a
// milestone therefore rendered as one bare bullet and its children fell out of
// everything: not the queue, since a milestone's members are its assignees
// alone; not the backlog, which reaches only roots and unassigned epics; and
// not the rollup, which counts the container as one unit.
//
// The table is driven off nibtypes rather than a list spelled here, so a new
// container type joins this guard the day it is declared — which is the whole
// failure mode, a type table that one consumer did not keep in step.
func TestBuildRoadmap_EveryContainerTypeExpands(t *testing.T) {
	cfg := config.Default()
	now := time.Now()

	var containers []string
	for _, typ := range cfg.TypeNames() {
		if typ == "milestone" {
			continue // a container of its own, never a queue member
		}
		if len(nibtypes.ValidChildTypes(typ)) > 0 {
			containers = append(containers, typ)
		}
	}
	if len(containers) < 2 {
		t.Fatalf("found %v container types; with fewer than two this guard cannot show the rule is type-independent", containers)
	}
	t.Logf("container types under guard: %v", containers)

	for _, typ := range containers {
		t.Run(typ+" in a milestone queue", func(t *testing.T) {
			childType := nibtypes.ValidChildTypes(typ)[0]
			nibs := []*nib.Nib{
				{ID: "m1", Type: "milestone", Title: "v1.0", Status: "in-progress", CreatedAt: &now},
				{ID: "c1", Type: typ, Title: "Container", Status: "todo", Milestone: "m1", MilestoneOrder: "a"},
				{ID: "k1", Type: childType, Title: "First child", Status: "todo", Parent: "c1", Order: "a"},
				{ID: "k2", Type: childType, Title: "Second child", Status: "todo", Parent: "c1", Order: "b"},
			}
			result := buildRoadmap(nibs, false, nil, nil, cfg)
			if len(result.Milestones) != 1 {
				t.Fatalf("got %d milestones, want 1", len(result.Milestones))
			}
			queue := result.Milestones[0].Queue
			if got := roadmapQueueIDs(queue); !slices.Equal(got, []string{"c1"}) {
				t.Fatalf("queue = %v, want the container standing at one position", got)
			}
			if got := nibIDs(queue[0].Items); !slices.Equal(got, []string{"k1", "k2"}) {
				t.Errorf("a %s member carried items %v, want [k1 k2] — its children appear in no view at all when it does not expand", typ, got)
			}
		})

		t.Run(typ+" in the backlog", func(t *testing.T) {
			childType := nibtypes.ValidChildTypes(typ)[0]
			nibs := []*nib.Nib{
				{ID: "c1", Type: typ, Title: "Container", Status: "todo", Order: "a"},
				{ID: "k1", Type: childType, Title: "First child", Status: "todo", Parent: "c1", Order: "a"},
			}
			result := buildRoadmap(nibs, false, nil, nil, cfg)
			if result.Backlog == nil {
				t.Fatal("got no backlog group")
			}
			queue := result.Backlog.Queue
			if got := roadmapQueueIDs(queue); !slices.Equal(got, []string{"c1"}) {
				t.Fatalf("backlog = %v, want the container standing at one position", got)
			}
			if got := nibIDs(queue[0].Items); !slices.Equal(got, []string{"k1"}) {
				t.Errorf("a %s root carried items %v, want [k1] — the backlog reaches roots, so a child that is not one has nowhere else to appear", typ, got)
			}
		})
	}
}

// roadmapMarks projects rendered roadmap Markdown to the skeleton the ordering
// assertions are about: every bullet reduced to its indent plus the nib
// reference that closes it.
//
// The indent is load-bearing, not cosmetic. A milestone's section is ONE list,
// so nesting is the only thing that distinguishes a container's decomposition
// from the queue entries standing around it — a projection that dropped the
// indent would read a flattened decomposition as a correct queue.
//
// The bullet projection is scoped to the `--no-links` rendering, where
// renderNibRef emits a bare `(id)` and LastIndex therefore finds the id. With
// links enabled the reference is `([id](path))` and LastIndex would silently
// return the path segment instead, so callers must render with links off.
func roadmapMarks(md string) []string {
	var marks []string
	for _, line := range strings.Split(md, "\n") {
		body := strings.TrimLeft(line, " ")
		if !strings.HasPrefix(body, "- ") {
			continue
		}
		indent := strings.Repeat(" ", len(line)-len(body))
		marks = append(marks, indent+body[strings.LastIndex(body, "("):])
	}
	return marks
}

// TestRenderRoadmapMarkdown_QueueReadsAsOneList pins the human rendering of an
// interleaved queue: a milestone's section is one Markdown list, each member
// standing at its queue position, and a container's decomposition NESTED
// beneath it.
//
// Nesting is what makes the list unambiguous. Rendered flat, a container's
// child and the queue entry after it are the same shape at the same level, and
// no separator between them can be both unique per run and meaningful — which
// is why the heading this replaced could repeat inside one section.
func TestRenderRoadmapMarkdown_QueueReadsAsOneList(t *testing.T) {
	cfg := config.Default()
	now := time.Now()

	milestone := &nib.Nib{ID: "m1", Type: "milestone", Title: "v1.0", Status: "in-progress", CreatedAt: &now}

	tests := []struct {
		name    string
		members []*nib.Nib
		want    []string
	}{
		{
			name: "a loose member standing after a container closes the queue",
			members: []*nib.Nib{
				// Declared against the queue keys, so input order cannot pass
				// for the arrangement.
				{ID: "f1", Type: "feature", Title: "Closes the queue", Status: "todo", Milestone: "m1", MilestoneOrder: "c"},
				{ID: "e1", Type: "epic", Title: "Middle epic", Status: "todo", Milestone: "m1", MilestoneOrder: "b"},
				{ID: "t9", Type: "task", Title: "Under the epic", Status: "todo", Parent: "e1"},
				{ID: "b1", Type: "bug", Title: "Opens the queue", Status: "todo", Milestone: "m1", MilestoneOrder: "a"},
			},
			want: []string{"(b1)", "(e1)", "  (t9)", "(f1)"},
		},
		{
			name: "a container nests its items and the loose members do not",
			members: []*nib.Nib{
				{ID: "e1", Type: "epic", Title: "Only epic", Status: "todo", Milestone: "m1", MilestoneOrder: "a"},
				{ID: "t9", Type: "task", Title: "Under the epic", Status: "todo", Parent: "e1"},
				{ID: "l1", Type: "task", Title: "First loose", Status: "todo", Milestone: "m1", MilestoneOrder: "b"},
				{ID: "l2", Type: "task", Title: "Second loose", Status: "todo", Milestone: "m1", MilestoneOrder: "c"},
			},
			want: []string{"(e1)", "  (t9)", "(l1)", "(l2)"},
		},
		{
			name: "a queue holding no container is a flat list",
			members: []*nib.Nib{
				{ID: "l1", Type: "task", Title: "First loose", Status: "todo", Milestone: "m1", MilestoneOrder: "a"},
				{ID: "l2", Type: "task", Title: "Second loose", Status: "todo", Milestone: "m1", MilestoneOrder: "b"},
			},
			want: []string{"(l1)", "(l2)"},
		},
		{
			name: "an epic dropped for empty scope leaves no trace",
			members: []*nib.Nib{
				// e1 holds no outstanding scope, so it never reaches the
				// queue at all and leaves no gap where it would have stood.
				{ID: "e1", Type: "epic", Title: "Finished epic", Status: "completed", Milestone: "m1", MilestoneOrder: "a"},
				{ID: "l1", Type: "task", Title: "First loose", Status: "todo", Milestone: "m1", MilestoneOrder: "b"},
				{ID: "l2", Type: "task", Title: "Second loose", Status: "todo", Milestone: "m1", MilestoneOrder: "c"},
			},
			want: []string{"(l1)", "(l2)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibs := append([]*nib.Nib{milestone}, tt.members...)
			md := renderRoadmapMarkdown(buildRoadmap(nibs, false, nil, nil, cfg), false, "")
			if marks := roadmapMarks(md); !slices.Equal(marks, tt.want) {
				t.Errorf("rendered queue = %#v, want %#v", marks, tt.want)
			}
		})
	}
}
