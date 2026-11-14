package nib

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// Covers the link label reads in ExtractMentionSpans: a long paragraph of link
// labels, titles or reference definitions parses quickly, and the parse
// matches goldmark's default parser for paragraphs within the definition bound.

// Each body is about 480 KB, which took seconds to parse with goldmark's default
// parser.
func TestExtractMentionSpansLinkLabelParagraphsAreFast(t *testing.T) {
	const size = 480000
	for name, body := range map[string]string{
		"shortcut labels":     repeatTo("[a]\n", size),
		"full labels":         repeatTo("[a][b]\n", size),
		"collapsed labels":    repeatTo("[a][]\n", size),
		"image labels":        repeatTo("![a]\n", size),
		"bracket mentions":    repeatTo("[[a]]\n", size),
		"quoted labels":       repeatTo("> [a]\n", size),
		"wrapped titles":      repeatTo("[a](x \"t\n", size),
		"wrapped labels":      repeatTo("[a][\nb]\n", size),
		"definitions":         repeatTo("[a]: x\n", size),
		"titled definitions":  repeatTo("[a]: x \"t\"\n", size),
		"long title":          "[a](x \"" + repeatTo("a\n", size) + "\")\n",
		"long label":          "[a][" + repeatTo("a\n", size) + "]\n",
		"long definition":     "[a]: x \"" + repeatTo("a\n", size) + "\"\n",
		"long definition key": "[" + repeatTo("a\n", size) + "]: x\n",
	} {
		t.Run(name, func(t *testing.T) {
			start := time.Now()
			ExtractMentionSpans(body)
			if elapsed := time.Since(start); elapsed > time.Second {
				t.Fatalf("parse took %v; want well under 1s", elapsed)
			}
		})
	}
}

// The paragraph is one line longer than the bound, so its definition is text
// and the destination is a mention.
func TestExtractMentionSpansReadsDefinitionsOfLongParagraphsAsText(t *testing.T) {
	body := "[a]: #tnib-a\n" + strings.Repeat("x\n", maxDefinitionParagraphLines) + "\n[x][a] [[tnib-b]]\n"
	got := ExtractMentionSpans(body)
	want := []MentionSpan{
		spanAt(body, "#tnib-a", "tnib-a", MentionHash),
		spanAt(body, "[[tnib-b]]", "tnib-b", MentionBracket),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("spans = %+v, want %+v", got, want)
	}
}

func TestExtractMentionSpansKeepsDefinitionsWithinTheLineBound(t *testing.T) {
	for name, defs := range map[string]string{
		"at the bound":     "[a]: #tnib-a\n" + strings.Repeat("x\n", maxDefinitionParagraphLines-1),
		"many definitions": strings.Repeat("[a]: #tnib-a\n", maxDefinitionParagraphLines),
		"wrapped title":    "[a]: #tnib-a \"" + strings.Repeat("t\n", maxDefinitionParagraphLines-2) + "\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			body := defs + "\n[[tnib-b]] [#tnib-c][a]\n"
			want := []MentionSpan{spanAt(body, "[[tnib-b]]", "tnib-b", MentionBracket)}
			if got := ExtractMentionSpans(body); !reflect.DeepEqual(got, want) {
				t.Errorf("spans = %+v, want %+v", got, want)
			}
		})
	}
}

// Each body is within the definition bound, so both parsers render it to the
// same HTML.
func TestMentionParserMatchesGoldmark(t *testing.T) {
	bodies := map[string]string{
		"reference kinds": "[a]\n[b][c]\n[d][]\n![e]\n[f](x \"t\")\n\n[c]: /c\n[d]: /d 'dt'\n[e]: /e\n[f]: /f\n",
		"quoted":          "> [a](x \"t\n> u\") [b][\n> c]\n>\n> [c]: /c \"multi\n> line\"\n",
		"tabbed quote":    ">\t[a](x \"t\n>\tu\") [b][\n>\tc]\n\n[c]: /c\n",
		"list":            "- [a][\n  b] and [b]\n  lazy [c\nd][]\n\n[b]: /b\n[c d]: /cd\n",
		"escapes":         "[a\\]b][x] [y\\\\][]\n\n[x]: /x\n[y\\\\]: /y\n",
		"setext":          "[b]: /b\n[a]\n[b]\n===\n",
		"crlf":            "[a]\r\n[b][]\r\n[c](x \"t\r\nu\")\r\n\r\n[a]: /a\r\n[b]: /b\r\n",
		"overlong label":  "[" + strings.Repeat("a\n", 600) + "][]\n\n[" + strings.Repeat("a\n", 600) + "]: /a\n",
		"nested":          "[a [b] c][d] [![e][d]](x)\n\n[d]: /d\n",
		"definition wrap": "[a]:\n/a\n'title\nmore'\n\n[a]\n",
		"mentions":        "[[tnib-a]] [x](#tnib-b \"#tnib-c\")\n[[tnib-d]][x]\n\n[x]: #tnib-e\n",
		"long paragraph":  strings.Repeat("[a] [[b]] [c][]\n[d](x \"t\n", 400) + "\n[c]: /c\n[d]: /d\n",
	}
	files, err := filepath.Glob(filepath.Join("..", "..", "testdata", "fixtures", "sample-project", ".nibs", "data", "*.md"))
	if err != nil || len(files) == 0 {
		t.Fatalf("fixture nibs: %v, %d files", err, len(files))
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		bodies[filepath.Base(f)] = string(b)
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			want := renderWith(t, goldmark.DefaultParser(), body)
			if got := renderWith(t, newMentionParser(), body); got != want {
				t.Errorf("html = %q\nwant %q", got, want)
			}
		})
	}
}

func renderWith(t *testing.T, p parser.Parser, body string) string {
	t.Helper()
	source := []byte(body)
	var buf bytes.Buffer
	if err := goldmark.DefaultRenderer().Render(&buf, source, p.Parse(text.NewReader(source))); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func repeatTo(unit string, size int) string {
	return strings.Repeat(unit, size/len(unit))
}
