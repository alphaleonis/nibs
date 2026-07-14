package nib

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// mentionTokenShape validates the shape of every token returned by
// ExtractMentionTokens. Derived from MentionIDPattern so grammar changes
// flow automatically from the production source to this invariant.
// Compiled once at package scope so the invariant check is cheap enough
// to call from every table case and every fuzz input.
var mentionTokenShape = regexp.MustCompile(`^` + MentionIDPattern + `$`)

// assertValidOutput checks the six output invariants ExtractMentionTokens
// must satisfy for any input body:
//
//  1. No panic — implicit (a panic fails the test / fuzz case).
//  2. Dedup — no duplicate strings in the output.
//  3. Regex conformance — every token matches mentionTokenShape.
//  4. Reproducible on same input — calling twice returns an identical slice.
//     (Weaker than "deterministic" in the Knuth sense: the production code
//     does not iterate a map during output construction, so same-process
//     same-input repeat is a low-cost regression guard, not a proof of
//     cross-process stability.)
//  5. Bounded — len(out) <= len(body) (tokens cannot outnumber bytes).
//  6. Substring — every token appears as a substring of body. Catches
//     phantom-token bugs (buffer reuse, stale bytes) that would otherwise
//     slip past the shape/dedup/bounded invariants.
//
// Uses testing.TB so it works from both *testing.T (table tests) and
// *testing.F's inner func(t *testing.T, s string) (the fuzz target).
func assertValidOutput(t testing.TB, body string, out []string) {
	t.Helper()

	// Invariant 2: dedup.
	seen := make(map[string]struct{}, len(out))
	for _, tok := range out {
		if _, dup := seen[tok]; dup {
			t.Errorf("duplicate token %q in output for body %q", tok, body)
		}
		seen[tok] = struct{}{}
	}

	// Invariant 3: regex conformance.
	for _, tok := range out {
		if !mentionTokenShape.MatchString(tok) {
			t.Errorf("token %q does not match mentionTokenShape for body %q", tok, body)
		}
	}

	// Invariant 4: reproducible on same input.
	second := ExtractMentionTokens(body)
	if !reflect.DeepEqual(out, second) {
		t.Errorf("non-reproducible output for body %q: first=%v second=%v", body, out, second)
	}

	// Invariant 5: bounded.
	if len(out) > len(body) {
		t.Errorf("output length %d exceeds body length %d for body %q", len(out), len(body), body)
	}

	// Invariant 6: every token is a substring of body. Catches phantoms
	// that satisfy shape/dedup/bounded but never actually appeared.
	for _, tok := range out {
		if !strings.Contains(body, tok) {
			t.Errorf("token %q not a substring of body %q", tok, body)
		}
	}
}

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
			// Table cases are the invariants' first line of defense: every
			// expected output must also satisfy the six generic invariants.
			assertValidOutput(t, tt.body, got)
		})
	}
}

// TestExtractMentionTokens_UnclosedFenceSuppressesMention pins the
// CommonMark rule that an unclosed fenced code block extends to EOF —
// `#secret` inside it must never surface as a mention.
func TestExtractMentionTokens_UnclosedFenceSuppressesMention(t *testing.T) {
	body := "```\n#secret\n"
	got := ExtractMentionTokens(body)
	for _, tok := range got {
		if tok == "secret" {
			t.Errorf("unclosed fence leaked mention %q; output=%v", tok, got)
		}
	}
	assertValidOutput(t, body, got)
}

// TestExtractMentionTokens_VeryLongSingleToken pins that an extremely long
// lowercase-alphanumeric run following a `#` is captured as a single token
// of the expected length (no truncation, no split).
func TestExtractMentionTokens_VeryLongSingleToken(t *testing.T) {
	body := "#" + strings.Repeat("a", 10000)
	got := ExtractMentionTokens(body)
	if len(got) != 1 {
		t.Fatalf("want 1 token, got %d: %v", len(got), got)
	}
	if len(got[0]) != 10000 {
		t.Errorf("want token of length 10000, got %d", len(got[0]))
	}
	assertValidOutput(t, body, got)
}

// TestExtractMentionTokens_UnicodeHeavy pins the byte-level ASCII-only
// scope of isWordChar: non-ASCII runes (emoji, CJK, accented Latin, RTL)
// are not treated as word-like, so a `#` following them produces a
// mention. All four tokens must appear in document order.
func TestExtractMentionTokens_UnicodeHeavy(t *testing.T) {
	body := "🎉#tok1 日本語#tok2 é#tok3 العربية#tok4"
	got := ExtractMentionTokens(body)
	want := []string{"tok1", "tok2", "tok3", "tok4"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ExtractMentionTokens(%q) = %v, want %v", body, got, want)
	}
	assertValidOutput(t, body, got)
}

// buildPathologicalBody constructs a deterministic 1 MiB markdown body
// designed to stress ExtractMentionTokens: nested code spans, repeated
// valid mentions, alternating fences with trailing tokens, and plain
// prose filler. Returned body length is at least targetSize bytes.
func buildPathologicalBody(targetSize int) string {
	// One "unit" cycles through structures the parser has to discriminate:
	//   - a multi-backtick code span that contains a would-be mention,
	//   - a fenced block whose fence is immediately followed by a mention
	//     outside the fence,
	//   - a bare valid mention (contributes a token, repeatedly — good
	//     stress on dedup),
	//   - plain prose filler so goldmark has real paragraphs to chew on.
	unit := "Prose filler word word word. " +
		"``code with #inside span`` and see #alpha. " +
		"```\n#hidden\n```\n#beta and more words.\n" +
		"Lorem ipsum dolor sit amet, consectetur adipiscing elit.\n"

	var b strings.Builder
	b.Grow(targetSize + len(unit))
	for b.Len() < targetSize {
		b.WriteString(unit)
	}
	return b.String()
}

// BenchmarkExtractMentionTokens measures throughput against a 1 MiB
// well-structured markdown body — balanced multi-backtick code spans,
// balanced fenced blocks, and two distinct mentions (`alpha`, `beta`)
// outside skipped regions repeated for dedup-hit pressure (see
// buildPathologicalBody; two further mention-shaped tokens `inside` and
// `hidden` sit inside code spans and fences, so they never reach the
// dedup map). b.SetBytes reports MB/s in the usual benchmark output.
//
// Adversarial shapes — unbalanced fences, backtick storms, high-cardinality
// dedup — are intentionally NOT covered here; sibling benchmarks would be
// the right place to add them if a specific regression motivates the
// measurement. Today this benchmark's role is "is there quadratic blowup?"
// not "what's the worst case throughput?".
func BenchmarkExtractMentionTokens(b *testing.B) {
	body := buildPathologicalBody(1 << 20)
	b.SetBytes(int64(len(body)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ExtractMentionTokens(body)
	}
}

// FuzzExtractMentionTokens feeds adversarial bodies to the mention scanner
// and asserts the six invariants documented on assertValidOutput. Seeds
// cover the shapes we expect to fuzz heavily (long hash runs, unclosed
// fences, unpaired backticks, long single tokens, Unicode-heavy prose,
// binary garbage, CRLF line endings). Go's fuzzer mutates these seeds at
// runtime (`go test -fuzz=...`); this file's smoke-run (`go test -run=Fuzz`)
// only exercises the seed corpus.
func FuzzExtractMentionTokens(f *testing.F) {
	// Tracer seeds — a happy path and the empty body.
	f.Add("See #gx0f.")
	f.Add("")

	// Long run of bare sigils (no id characters follow any of them).
	// Seed sizes stay within Go's recommended corpus scale (~1 KiB); for
	// longer-body stress, see BenchmarkExtractMentionTokens (1 MiB body).
	f.Add(strings.Repeat("#", 1024))

	// Unclosed fenced code block — the fence extends to EOF (CommonMark),
	// so `#secret` inside must never surface as a mention.
	f.Add("```\n#secret\n")

	// Many unpaired backticks interleaved with mention-shaped tokens.
	// Pins "no panic" under pathological inline-code delimiter patterns.
	f.Add(strings.Repeat("`#tok ", 20))

	// Very long single token — pins no panic or quadratic blowup on
	// adversarial token length (kept at ~1 KiB per Go's seed guidance).
	f.Add("#" + strings.Repeat("a", 1024))

	// Unicode-heavy body: emoji, CJK, combining marks, RTL adjacent to #.
	f.Add("🎉#tok1 日本語#tok2 é#tok3 العربية#tok4")

	// Deterministic binary-garbage seed — hand-authored, so it round-trips
	// through fuzz corpus persistence reliably. Pure random bytes come
	// from go's fuzzer at runtime.
	f.Add("\x00\x01\xff\x7f#a\xc0\xc1")

	// CRLF line endings — goldmark's exact handling of headings and fences
	// under CRLF is a known-gap acknowledgement, not a bug to fix here.
	// Seeds pin "no panic" and that the generic invariants still hold.
	f.Add("## Heading\r\n#tok\r\n")
	f.Add("```\r\n#incode\r\n```\r\n")

	// Raw HTML — `#hidden` inside an HTML block must be skipped by the
	// *ast.HTMLBlock/*ast.RawHTML branch in references.go; `#real` in
	// surrounding prose must still surface. Pins the HTML-skip contract
	// that had no seed-level coverage before.
	f.Add("<div>#hidden</div>\n\nBody #real.")

	// Nested fences: tilde outer, backtick inner. The inner fence and its
	// contents are part of the outer code block (so `#a`, `#b` never leak);
	// `#c` after the outer close must surface. Pins outer-fence-close
	// behavior under nested-fence-type stress.
	f.Add("~~~\n```\n#a\n```\n#b\n~~~\n#c")

	f.Fuzz(func(t *testing.T, body string) {
		got := ExtractMentionTokens(body)
		assertValidOutput(t, body, got)
	})
}
