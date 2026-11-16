package filterlang

import (
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/area"
)

func TestSpansTileTheText(t *testing.T) {
	areas := &area.Vocabulary{Nodes: []area.Node{{Name: "web"}}}
	texts := []string{
		"", " ", "type:bug,,banana -status:open, x",
		"\u00a0tags:a\u03a3 \U0001F600parent:x,y:z -is:blocked",
		"area:web area:gone -area:web area: has:parent no:x type:,",
		"a\u0085b\u2028c\ufeff",
	}
	for _, text := range texts {
		var b strings.Builder
		end := 0
		for _, s := range SpansWithAreas(text, areas) {
			if s.Start != end || s.End <= s.Start {
				t.Errorf("SpansWithAreas(%q): span %+v does not continue at %d", text, s, end)
			}
			b.WriteString(text[s.Start:s.End])
			end = s.End
		}
		if b.String() != text {
			t.Errorf("SpansWithAreas(%q) joins to %q", text, b.String())
		}
	}
}

// Offsets count bytes: the no-break space before the token is two.
func TestSpanOffsetsCountBytes(t *testing.T) {
	got := Spans("\u00a0is:blocked")
	want := []Span{{0, 2, SpanWhitespace}, {2, 4, SpanField}, {4, 5, SpanOperator}, {5, 12, SpanValue}}
	if len(got) != len(want) {
		t.Fatalf("Spans = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("span %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
