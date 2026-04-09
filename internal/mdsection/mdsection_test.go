package mdsection

import "testing"

func TestFind(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		heading   string
		wantText  string
		wantFound bool
	}{
		{
			name: "basic section between two headings",
			body: "## Goal\n\nBuild the thing.\n\n## Key Decisions\n\n- Use GraphQL\n- File-based storage\n\n## Notes\n\nOther stuff.",
			heading:   "Key Decisions",
			wantText:  "\n- Use GraphQL\n- File-based storage\n",
			wantFound: true,
		},
		{
			name: "case insensitive match",
			body: "## key decisions\n\n- Works with lowercase\n",
			heading:   "Key Decisions",
			wantText:  "\n- Works with lowercase\n",
			wantFound: true,
		},
		{
			name: "heading at end of document",
			body: "## Goal\n\nShip it.\n\n## Notes\n\nTrailing content with no next heading.",
			heading:   "Notes",
			wantText:  "\nTrailing content with no next heading.\n",
			wantFound: true,
		},
		{
			name:      "empty body",
			body:      "",
			heading:   "Anything",
			wantText:  "",
			wantFound: false,
		},
		{
			name:      "section not found",
			body:      "## Goal\n\nSome text.",
			heading:   "Key Decisions",
			wantText:  "",
			wantFound: false,
		},
		{
			name:      "sub-headings are part of the section",
			body:      "## Design\n\nOverview.\n\n### Details\n\nSub-section content.\n\n## Next",
			heading:   "Design",
			wantText:  "\nOverview.\n\n### Details\n\nSub-section content.\n",
			wantFound: true,
		},
		{
			name:      "prefix match with parenthetical",
			body:      "## Key Decisions (Phase 2)\n\n- Decision one\n",
			heading:   "Key Decisions",
			wantText:  "\n- Decision one\n",
			wantFound: true,
		},
		{
			name:      "does not prefix match non-parenthetical suffix",
			body:      "## Notes on Design\n\nDesign notes.\n\n## Notes\n\nActual notes.\n",
			heading:   "Notes",
			wantText:  "\nActual notes.\n",
			wantFound: true,
		},
		{
			name:      "finds h1 heading including sub-headings",
			body:      "# Overview\n\nTop-level content.\n\n## Details\n\nSub content.",
			heading:   "Overview",
			wantText:  "\nTop-level content.\n\n## Details\n\nSub content.\n",
			wantFound: true,
		},
		{
			name:      "finds h3 heading stopped by h2",
			body:      "## Parent\n\n### Child\n\nChild content.\n\n## Next",
			heading:   "Child",
			wantText:  "\nChild content.\n",
			wantFound: true,
		},
		{
			name:      "duplicate headings returns first match",
			body:      "## Notes\n\nFirst.\n\n## Notes\n\nSecond.",
			heading:   "Notes",
			wantText:  "\nFirst.\n",
			wantFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := Find(tt.body, tt.heading)
			if found != tt.wantFound {
				t.Errorf("found = %v, want %v", found, tt.wantFound)
			}
			if got != tt.wantText {
				t.Errorf("content = %q, want %q", got, tt.wantText)
			}
		})
	}
}

func TestReplace(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		heading    string
		newContent string
		want       string
	}{
		{
			name:       "replace section between headings",
			body:       "## Goal\n\nShip it.\n\n## Current Focus\n\nOld focus.\n\n## Notes\n\nStuff.",
			heading:    "Current Focus",
			newContent: "\nNew focus here.\n",
			want:       "## Goal\n\nShip it.\n\n## Current Focus\n\nNew focus here.\n\n## Notes\n\nStuff.",
		},
		{
			name:       "section not found returns body unchanged",
			body:       "## Goal\n\nShip it.",
			heading:    "Missing",
			newContent: "\nWon't appear.\n",
			want:       "## Goal\n\nShip it.",
		},
		{
			name:       "replace section at end of document",
			body:       "## Goal\n\nShip it.\n\n## Notes\n\nOld notes.",
			heading:    "Notes",
			newContent: "\nNew notes here.",
			want:       "## Goal\n\nShip it.\n\n## Notes\n\nNew notes here.",
		},
		{
			name:       "replace preserves sub-headings in other sections",
			body:       "## A\n\nContent A.\n\n### Sub-A\n\nSub content.\n\n## B\n\nContent B.\n\n## C\n\nContent C.",
			heading:    "B",
			newContent: "\nReplaced B.\n",
			want:       "## A\n\nContent A.\n\n### Sub-A\n\nSub content.\n\n## B\n\nReplaced B.\n\n## C\n\nContent C.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Replace(tt.body, tt.heading, tt.newContent)
			if got != tt.want {
				t.Errorf("Replace() =\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

func TestSet(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		level   int
		heading string
		content string
		want    string
	}{
		{
			name:    "replaces existing section",
			body:    "## Goal\n\nShip it.\n\n## Notes\n\nOld notes.\n\n## End",
			level:   2,
			heading: "Notes",
			content: "\nNew notes.\n",
			want:    "## Goal\n\nShip it.\n\n## Notes\n\nNew notes.\n\n## End",
		},
		{
			name:    "appends new section when not found",
			body:    "## Goal\n\nShip it.",
			level:   2,
			heading: "Key Decisions",
			content: "\n- Decision one\n",
			want:    "## Goal\n\nShip it.\n\n## Key Decisions\n\n- Decision one\n",
		},
		{
			name:    "appends to empty body",
			body:    "",
			level:   2,
			heading: "Notes",
			content: "\nSome notes.\n",
			want:    "\n## Notes\n\nSome notes.\n",
		},
		{
			name:    "appends h3 section",
			body:    "## Parent\n\nSome content.",
			level:   3,
			heading: "Details",
			content: "\nDetail text.\n",
			want:    "## Parent\n\nSome content.\n\n### Details\n\nDetail text.\n",
		},
		{
			name:    "appends h1 section",
			body:    "## Existing\n\nContent.",
			level:   1,
			heading: "Top Level",
			content: "\nTop-level text.\n",
			want:    "## Existing\n\nContent.\n\n# Top Level\n\nTop-level text.\n",
		},
		{
			name:    "level zero clamps to h1",
			body:    "## Existing\n\nContent.",
			level:   0,
			heading: "Added",
			content: "\nNew content.\n",
			want:    "## Existing\n\nContent.\n\n# Added\n\nNew content.\n",
		},
		{
			name:    "negative level clamps to h1",
			body:    "## Existing\n\nContent.",
			level:   -1,
			heading: "Added",
			content: "\nNew content.\n",
			want:    "## Existing\n\nContent.\n\n# Added\n\nNew content.\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Set(tt.body, tt.level, tt.heading, tt.content)
			if got != tt.want {
				t.Errorf("Set() =\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}
