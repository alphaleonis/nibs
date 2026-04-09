package bodytemplate

import (
	"strings"
	"testing"
)

func TestBodyTemplate(t *testing.T) {
	tests := []struct {
		name            string
		typeName        string
		wantSections    []string
		wantPlaceholder bool // expect placeholder text under headings
		wantEmpty       bool
	}{
		{
			name:            "task has Description and Verification",
			typeName:        "task",
			wantSections:    []string{"## Description", "## Verification"},
			wantPlaceholder: true,
		},
		{
			name:            "bug has Steps to Reproduce, Expected vs Actual, Root Cause",
			typeName:        "bug",
			wantSections:    []string{"## Steps to Reproduce", "## Expected vs Actual", "## Root Cause"},
			wantPlaceholder: true,
		},
		{
			name:            "epic has Objective, Acceptance Criteria, Scope Boundaries",
			typeName:        "epic",
			wantSections:    []string{"## Objective", "## Acceptance Criteria", "## Scope Boundaries"},
			wantPlaceholder: true,
		},
		{
			name:            "milestone has Goal, Current Focus, Key Decisions",
			typeName:        "milestone",
			wantSections:    []string{"## Goal", "## Current Focus", "## Key Decisions"},
			wantPlaceholder: true,
		},
		{
			name:            "research has Question, Findings, Decision, Follow-ups",
			typeName:        "research",
			wantSections:    []string{"## Question", "## Findings", "## Decision", "## Follow-ups"},
			wantPlaceholder: true,
		},
		{
			name:      "feature returns empty (no template)",
			typeName:  "feature",
			wantEmpty: true,
		},
		{
			name:      "unknown type returns empty",
			typeName:  "nonexistent",
			wantEmpty: true,
		},
		{
			name:      "empty string returns empty",
			typeName:  "",
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BodyTemplate(tt.typeName)

			if tt.wantEmpty {
				if got != "" {
					t.Errorf("BodyTemplate(%q) = %q, want empty", tt.typeName, got)
				}
				return
			}

			for _, section := range tt.wantSections {
				if !strings.Contains(got, section) {
					t.Errorf("BodyTemplate(%q) missing section %q\ngot:\n%s", tt.typeName, section, got)
				}
			}

			// Verify no unexpected extra sections
			var gotHeadings int
			for _, line := range strings.Split(got, "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "## ") {
					gotHeadings++
				}
			}
			if gotHeadings != len(tt.wantSections) {
				t.Errorf("BodyTemplate(%q) has %d sections, want %d", tt.typeName, gotHeadings, len(tt.wantSections))
			}

			if tt.wantPlaceholder {
				// Verify each section heading has non-empty content beneath it
				for _, section := range tt.wantSections {
					idx := strings.Index(got, section)
					if idx == -1 {
						continue // already reported above
					}
					after := got[idx+len(section):]
					nextHeading := strings.Index(after, "\n## ")
					var sectionBody string
					if nextHeading == -1 {
						sectionBody = after
					} else {
						sectionBody = after[:nextHeading]
					}
					if strings.TrimSpace(sectionBody) == "" {
						t.Errorf("BodyTemplate(%q) section %q has no placeholder content\ngot:\n%s", tt.typeName, section, got)
					}
				}
			}
		})
	}
}
