package filterlang

import (
	"slices"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/area"
)

func TestDiagnosticsLocateTheRejectedText(t *testing.T) {
	areas := &area.Vocabulary{Nodes: []area.Node{{Name: "web"}}}
	tests := []struct {
		text string
		want []Diagnostic
	}{
		{"status:todo,Banana", []Diagnostic{{Token: "status:banana", Start: 12, End: 18, Reason: UnknownValue}}},
		{"-type:x,,y", []Diagnostic{
			{Token: "-type:x", Start: 6, End: 7, Reason: UnknownValue},
			{Token: "-type:y", Start: 9, End: 10, Reason: UnknownValue},
		}},
		{"a  -Ancestor:X", []Diagnostic{{Token: "-ancestor:x", Start: 3, End: 14, Reason: NegatedToken}}},
		{"-area:web", []Diagnostic{{Token: "-area:web", Start: 0, End: 9, Reason: NegatedToken}}},
		{"x area-exact:Web", []Diagnostic{{Token: "area-exact:Web", Start: 13, End: 16, Reason: UndeclaredArea}}},
		// Offsets count bytes: the no-break space before the token is two.
		{"\u00a0tags:a\u03a3", []Diagnostic{{Token: "tags:a\u03c2", Start: 7, End: 10, Reason: UnknownValue}}},
		// Each occurrence has its own diagnostic, though InvalidTokens lists the token once.
		{"type:x type:x", []Diagnostic{
			{Token: "type:x", Start: 5, End: 6, Reason: UnknownValue},
			{Token: "type:x", Start: 12, End: 13, Reason: UnknownValue},
		}},
	}
	for _, tt := range tests {
		_, got := ParseWithAreas(tt.text, areas)
		if !slices.Equal(got, tt.want) {
			t.Errorf("ParseWithAreas(%q) diagnostics = %+v, want %+v", tt.text, got, tt.want)
		}
	}
}

func TestInvalidTokensListsEachTokenOnce(t *testing.T) {
	_, diags := Parse("type:x -parent:a type:x")
	if got, want := InvalidTokens(diags), []string{"type:x", "-parent:a"}; !slices.Equal(got, want) {
		t.Errorf("InvalidTokens = %q, want %q", got, want)
	}
}

// TestRunsTileTheText pins what highlighting relies on: the runs cover the text
// in order with no gap, and whitespace and token runs alternate.
func TestRunsTileTheText(t *testing.T) {
	for _, text := range []string{"", " ", "a", "  type:bug \t login\u3000x\u0085y ", "\ufeff"} {
		var b strings.Builder
		for i, r := range runs(text) {
			if i > 0 && r.space == runs(text)[i-1].space {
				t.Errorf("runs(%q): runs %d and %d are both space=%t", text, i-1, i, r.space)
			}
			if r.start != b.Len() || r.end <= r.start {
				t.Errorf("runs(%q): run %d is [%d,%d) after %d bytes", text, i, r.start, r.end, b.Len())
			}
			b.WriteString(text[r.start:r.end])
		}
		if b.String() != text {
			t.Errorf("runs(%q) rebuild %q", text, b.String())
		}
	}
}

func TestSerializeOmitsEmptyValues(t *testing.T) {
	f, _ := Parse("type:bug")
	empty := ""
	f.ParentID, f.Area, f.Search = &empty, &empty, &empty
	f.Status = []string{}
	if got := Serialize(f, nil); got != "type:bug" {
		t.Errorf("Serialize = %q, want %q", got, "type:bug")
	}
}

// TestSerializeSortsTagsByUTF16CodeUnits uses tags whose UTF-8 byte order puts
// U+FF21 before U+1F600, while their UTF-16 order puts the surrogate pair first.
func TestSerializeSortsTagsByUTF16CodeUnits(t *testing.T) {
	f, _ := Parse("")
	f.Tags = []string{"\uff21", "\U0001f600", "a"}
	if got, want := Serialize(f, nil), "tags:a,\U0001f600,\uff21"; got != want {
		t.Errorf("Serialize = %q, want %q", got, want)
	}
}
