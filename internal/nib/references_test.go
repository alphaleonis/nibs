package nib

import (
	"reflect"
	"testing"
)

func TestExtractMentionTokens(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "empty body",
			body: "",
			want: nil,
		},
		{
			name: "no mentions",
			body: "This is a plain body with no references at all.",
			want: nil,
		},
		{
			name: "single short-form mention",
			body: "This depends on #gx0f.",
			want: []string{"gx0f"},
		},
		{
			name: "single full-form mention",
			body: "This depends on #nibs-gx0f.",
			want: []string{"nibs-gx0f"},
		},
		{
			name: "multiple distinct mentions",
			body: "See #gx0f and also #nibs-0rs2 for context.",
			want: []string{"gx0f", "nibs-0rs2"},
		},
		{
			name: "duplicate mentions are deduped",
			body: "See #gx0f and again #gx0f later.",
			want: []string{"gx0f"},
		},
		{
			name: "mention at start of body",
			body: "#gx0f is the starting point.",
			want: []string{"gx0f"},
		},
		{
			name: "mention after punctuation",
			body: "Refs:#gx0f,#nibs-abcd.",
			want: []string{"gx0f", "nibs-abcd"},
		},
		{
			name: "mention inside parentheses",
			body: "Details (see #gx0f) here.",
			want: []string{"gx0f"},
		},
		{
			name: "hash at line start followed by space is a Markdown header",
			body: "# Heading\n\nBody text.",
			want: nil,
		},
		{
			name: "hash at line start followed by hash is still a header",
			body: "## Section\n\nMore.",
			want: nil,
		},
		{
			name: "hash mid-line after space still matches (not a header)",
			body: "The ref is #gx0f here.",
			want: []string{"gx0f"},
		},
		{
			name: "mention inside fenced code block is skipped",
			body: "Normal #gx0f here.\n\n```\nsee #skipped in code\n```\n\nAfter.",
			want: []string{"gx0f"},
		},
		{
			name: "mention inside tilde-fenced code block is skipped",
			body: "Outside #gx0f.\n\n~~~\n#nothere\n~~~\n",
			want: []string{"gx0f"},
		},
		{
			name: "mention inside inline code is skipped",
			body: "Use `#gx0f` inline shows nothing, but #nibs-0rs2 does.",
			want: []string{"nibs-0rs2"},
		},
		{
			name: "multiple inline code spans",
			body: "`#a1b2` and `#c3d4` are code, but #e5f6 is a mention.",
			want: []string{"e5f6"},
		},
		{
			name: "fenced block spanning multiple lines",
			body: "#before\n```go\nfunc main() {\n  // #inside\n}\n```\n#after",
			want: []string{"before", "after"},
		},
		{
			name: "fenced block with language tag",
			body: "```go\n// #skip\n```\n#keep",
			want: []string{"keep"},
		},
		{
			name: "hash followed by non-id-character does not match",
			body: "Issue #! is not valid.",
			want: nil,
		},
		{
			name: "hash followed by digits only (short numeric)",
			body: "See #1234 here.",
			want: []string{"1234"},
		},
		{
			name: "token is trimmed at word boundary",
			body: "Ref #gx0f, then end.",
			want: []string{"gx0f"},
		},
		{
			name: "hash inside a word does not match",
			body: "email#gx0f@example.com - this is weird",
			want: nil,
		},
		{
			name: "multiple mentions separated by newlines",
			body: "First: #a1b2\nSecond: #c3d4\nThird: #e5f6",
			want: []string{"a1b2", "c3d4", "e5f6"},
		},
		{
			name: "fenced block at end of body unterminated",
			body: "Outside #gx0f.\n\n```\n#inside\n",
			want: []string{"gx0f"},
		},
		{
			name: "backtick inside word is not a code fence",
			body: "We use `backticks` for #gx0f refs.",
			want: []string{"gx0f"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractMentionTokens(tt.body)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractMentionTokens(%q) = %v, want %v", tt.body, got, tt.want)
			}
		})
	}
}
