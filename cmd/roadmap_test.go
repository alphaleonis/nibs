package cmd

import (
	"testing"
	"time"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/config"
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
