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
			name: "right-side terminator is any non-id char (comma)",
			body: "Ref #gx0f, then end.",
			want: []string{"gx0f"},
		},
		{
			name: "right-side terminator: period",
			body: "See #gx0f.",
			want: []string{"gx0f"},
		},
		{
			name: "right-side terminator: closing paren",
			body: "(see #gx0f)",
			want: []string{"gx0f"},
		},
		{
			name: "right-side terminator: space",
			body: "We noted #gx0f today.",
			want: []string{"gx0f"},
		},
		{
			name: "right-side terminator: newline",
			body: "Ref #gx0f\nnext line",
			want: []string{"gx0f"},
		},
		{
			name: "hash inside a word does not match",
			body: "email#gx0f@example.com - this is weird",
			want: nil,
		},
		{
			// Pins the byte-level ASCII-only scope of isWordChar: the `#`
			// sigils here are directly preceded by the trailing continuation
			// byte of a non-ASCII rune (é, è), which is not treated as
			// word-like, so the mentions are accepted. Do not widen the
			// guard to unicode.IsLetter without updating this test.
			name: "non-ascii rune directly before hash is not word-like",
			body: "café#gx0f and déjà#abc",
			want: []string{"gx0f", "abc"},
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
			name: "inline code span inside prose is stripped; mentions outside preserved",
			body: "We use `backticks` for #gx0f refs.",
			want: []string{"gx0f"},
		},
		{
			name: "unpaired backtick in prose does not swallow the mention",
			body: "We use backtick ` for #gx0f refs.",
			want: []string{"gx0f"},
		},
		// Finding #22 — load-bearing invariants that used to be unpinned.
		{
			name: "uppercase ID does not match",
			body: "See #ABCD here",
			want: nil,
		},
		{
			name: "trailing hyphen is stripped from the captured token",
			body: "ref #abc-",
			want: []string{"abc"},
		},
		{
			name: "CRLF line endings preserved across line breaks",
			body: "See #gx0f,\r\nand #nibs-abc.",
			want: []string{"gx0f", "nibs-abc"},
		},
		// Finding #1 — multi-backtick code spans must not leak mentions.
		{
			name: "multi-backtick code span strips its contents",
			body: "We use ``#gx0f`` in a sentence",
			want: nil,
		},
		// Finding #2 — ATX heading text is walked, mentions inside it count.
		{
			name: "mentions inside heading text are extracted",
			body: "## Related: #abc1 and #def2",
			want: []string{"abc1", "def2"},
		},
		{
			name: "heading mention followed by body mentions deduplicates",
			body: "# Release notes for #nibs-def\n\nBody body #nibs-def and #other",
			want: []string{"nibs-def", "other"},
		},
		// Finding #10 — pin `##gx0f` behavior. Goldmark parses this as a
		// level-2 heading whose text is "gx0f body text", so `gx0f` surfaces
		// as a mention. The leading `##` is the heading marker itself and
		// never reaches the mention scanner.
		{
			name: "##gx0f without trailing space: heading text is scanned for mentions",
			body: "##gx0f body text",
			want: []string{"gx0f"},
		},
		// Finding #11 — indented (4-space/tab) code blocks are skipped by
		// goldmark, so mentions inside them do not leak.
		{
			name: "four-space indented code block is skipped",
			body: "Intro.\n\n    code line #gx0f\n\nOutro #abc",
			want: []string{"abc"},
		},
		// Finding #12 — pin goldmark behavior for tab-indented fences.
		// A tab-indented triple-backtick line is parsed as an indented code
		// block (so the fence opener itself is in code), and the line after
		// the code block terminates the block; `#inside` lands in a
		// following paragraph and *is* scanned. Pin this behavior.
		{
			name: "tab-indented fence marker is not a fence (pinned goldmark behavior)",
			body: "outside #gx0f\n\n\t```\n#inside\n\t```\n",
			want: []string{"gx0f", "inside"},
		},
		{
			name: "tab-indented heading line is parsed as indented code",
			body: "\t# Title\n\nbody with #abc",
			want: []string{"abc"},
		},
		// Finding #13 — reference-link definitions are not walked as text.
		{
			name: "reference-link definition URL does not produce a mention",
			body: "[foo]: #anchor\n\nBody text with #real.",
			want: []string{"real"},
		},
		// Inline link text and image alt text are inside skipped node types.
		{
			name: "inline link text without mention produces nothing",
			body: "[click here](https://example.com)",
			want: nil,
		},
		{
			name: "inline link text with mention inside is skipped",
			body: "[click #gx0f](https://example.com)",
			want: nil,
		},
		{
			name: "inline link URL fragment is skipped",
			body: "[click](#realref)",
			want: nil,
		},
		{
			name: "markdown image alt text is skipped",
			body: "![alt #gx0f](img.png)",
			want: nil,
		},
		// Finding #1 (iteration 2) — word-boundary must survive AST
		// segment splits. goldmark splits inline text at emphasis and other
		// delimiters, so a `#` at position 0 of a follow-up segment must
		// still see the previous segment's last char as the word boundary.
		{
			name: "word_char + underscore + hash is not a mention (name_#bar)",
			body: "name_#bar",
			want: nil,
		},
		{
			name: "emphasis underscore into hash (foo_#xyz)",
			body: "foo_#xyz",
			want: nil,
		},
		{
			name: "emphasis wrapping word, then #: word-boundary preserved",
			body: "_abc_#xyz",
			want: nil,
		},
		{
			name: "bold then #: `*` is not word-like so mention matches",
			body: "**word**#xyz",
			want: []string{"xyz"},
		},
		{
			name: "code span breaks word-boundary (` followed by #)",
			body: "foo`bar`#xyz",
			want: []string{"xyz"},
		},
		{
			name: "mention at start of paragraph still matches",
			body: "#abc is a real mention",
			want: []string{"abc"},
		},
		{
			name: "mention at start of heading text still matches",
			body: "## #abc in heading",
			want: []string{"abc"},
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
