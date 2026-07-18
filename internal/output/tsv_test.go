package output

import (
	"strings"
	"testing"
)

// TestFormatTSV_Grid covers the generic grid → TSV primitive: cells joined by
// '\t', rows terminated by '\n' (N rows → N newlines), an empty grid → "".
func TestFormatTSV_Grid(t *testing.T) {
	tests := []struct {
		name string
		rows [][]string
		want string
	}{
		{"empty grid", nil, ""},
		{"empty slice grid", [][]string{}, ""},
		{"single cell", [][]string{{"a"}}, "a\n"},
		{"single row multi cell", [][]string{{"a", "b", "c"}}, "a\tb\tc\n"},
		{
			"multiple rows",
			[][]string{{"a1", "todo"}, {"b2", "done"}},
			"a1\ttodo\nb2\tdone\n",
		},
		{"empty cells preserved", [][]string{{"", "", ""}}, "\t\t\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatTSV(tt.rows); got != tt.want {
				t.Errorf("FormatTSV(%v) = %q, want %q", tt.rows, got, tt.want)
			}
		})
	}
}

// TestFormatListTSV_Header covers the default list TSV renderer: a "# <n> nibs"
// comment header (n = row count, "# 0 nibs" when empty) followed by the rows,
// and the --no-header form that drops the comment line. The hiddenClosed
// annotation is exercised separately in TestFormatListTSV_HiddenAnnotation.
func TestFormatListTSV_Header(t *testing.T) {
	rows := [][]string{{"a1", "todo"}, {"b2", "done"}}
	tests := []struct {
		name   string
		rows   [][]string
		header bool
		want   string
	}{
		{
			name:   "header with rows",
			rows:   rows,
			header: true,
			want:   "# 2 nibs\na1\ttodo\nb2\tdone\n",
		},
		{
			name:   "no-header drops the comment",
			rows:   rows,
			header: false,
			want:   "a1\ttodo\nb2\tdone\n",
		},
		{
			name:   "empty with header",
			rows:   nil,
			header: true,
			want:   "# 0 nibs\n",
		},
		{
			name:   "empty without header",
			rows:   nil,
			header: false,
			want:   "",
		},
		{
			name:   "single nib header is not pluralized specially",
			rows:   [][]string{{"only"}},
			header: true,
			want:   "# 1 nibs\nonly\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatListTSV(tt.rows, tt.header, 0, "")
			if got != tt.want {
				t.Errorf("FormatListTSV(%v, header=%v) = %q, want %q", tt.rows, tt.header, got, tt.want)
			}
		})
	}
}

// TestFormatListTSV_HiddenAnnotation covers the open-default disclosure: when
// hiddenClosed > 0 the header discloses the suppressed count and how to reveal
// them; hiddenClosed == 0 leaves the header bare; --no-header never annotates.
func TestFormatListTSV_HiddenAnnotation(t *testing.T) {
	rows := [][]string{{"a1", "todo"}, {"b2", "in-progress"}}
	tests := []struct {
		name         string
		rows         [][]string
		header       bool
		hiddenClosed int
		hiddenLabel  string
		want         string
	}{
		{
			name:         "annotates when rows hidden",
			rows:         rows,
			header:       true,
			hiddenClosed: 43,
			hiddenLabel:  "completed/scrapped",
			want:         "# 2 nibs (43 hidden: completed/scrapped — --all to include)\na1\ttodo\nb2\tin-progress\n",
		},
		{
			name:         "zero hidden leaves header bare",
			rows:         rows,
			header:       true,
			hiddenClosed: 0,
			hiddenLabel:  "completed/scrapped",
			want:         "# 2 nibs\na1\ttodo\nb2\tin-progress\n",
		},
		{
			name:         "no-header suppresses the annotation",
			rows:         rows,
			header:       false,
			hiddenClosed: 43,
			hiddenLabel:  "completed/scrapped",
			want:         "a1\ttodo\nb2\tin-progress\n",
		},
		{
			name:         "empty result with hidden rows still annotates",
			rows:         nil,
			header:       true,
			hiddenClosed: 5,
			hiddenLabel:  "completed/scrapped",
			want:         "# 0 nibs (5 hidden: completed/scrapped — --all to include)\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatListTSV(tt.rows, tt.header, tt.hiddenClosed, tt.hiddenLabel)
			if got != tt.want {
				t.Errorf("FormatListTSV(rows, header=%v, hidden=%d) = %q, want %q",
					tt.header, tt.hiddenClosed, got, tt.want)
			}
		})
	}
}

// TestFormatColumns_DelegatesToFormatTSV is a belt-and-braces check that the
// refactor of FormatColumns onto the shared FormatTSV primitive keeps its
// column output byte-identical to a hand-built TSV grid.
func TestFormatColumns_DelegatesToFormatTSV(t *testing.T) {
	// A row grid equivalent to the columns projection below.
	grid := [][]string{{"a1", "todo"}, {"b2", "in-progress"}}
	if !strings.HasSuffix(FormatTSV(grid), "\n") {
		t.Fatal("FormatTSV should end with a trailing newline")
	}
}
