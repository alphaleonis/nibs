package output

import (
	"strings"
	"testing"
	"time"

	"github.com/alphaleonis/nibs/internal/nib"
)

func TestParseColumns(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		want    []Column
		wantErr bool
		// errSubstr, when non-empty, must be contained in the error string.
		errSubstr string
	}{
		{
			name: "single column",
			spec: "id",
			want: []Column{"id"},
		},
		{
			name: "two columns",
			spec: "id,title",
			want: []Column{"id", "title"},
		},
		{
			name: "trims whitespace around names",
			spec: " id , title ",
			want: []Column{"id", "title"},
		},
		{
			name:      "rejects unknown column listing the available set",
			spec:      "id,bogus",
			wantErr:   true,
			errSubstr: "id",
		},
		{
			name:      "rejects empty entry from doubled comma",
			spec:      "id,,title",
			wantErr:   true,
			errSubstr: "empty",
		},
		{
			name:      "rejects fully empty spec",
			spec:      "",
			wantErr:   true,
			errSubstr: "empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseColumns(tt.spec)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseColumns(%q) = %v, nil, want error", tt.spec, got)
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseColumns(%q) error: %v", tt.spec, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("ParseColumns(%q) = %v, want %v", tt.spec, got, tt.want)
			}
			for i, c := range got {
				if c != tt.want[i] {
					t.Errorf("ParseColumns(%q)[%d] = %q, want %q", tt.spec, i, c, tt.want[i])
				}
			}
		})
	}
}

// TestFormatColumns_SingleNib_TabSeparated covers the basic shape:
// id, status, title joined by tabs with a trailing newline.
func TestFormatColumns_SingleNib_TabSeparated(t *testing.T) {
	b := &nib.Nib{ID: "abc", Status: "todo", Title: "The title"}
	got := FormatColumns([]*nib.Nib{b}, []Column{"id", "status", "title"})
	want := "abc\ttodo\tThe title\n"
	if got != want {
		t.Errorf("FormatColumns = %q, want %q", got, want)
	}
}

// TestFormatColumns_MultipleNibs_PreservesOrder_TrailingNewline asserts
// row order matches input and that the output ends with exactly one
// trailing newline (not zero, not two). Each row ends with '\n'; the
// final row also gets one — so N nibs produce N newlines total.
func TestFormatColumns_MultipleNibs_PreservesOrder_TrailingNewline(t *testing.T) {
	nibs := []*nib.Nib{
		{ID: "a1", Status: "todo", Title: "First"},
		{ID: "b2", Status: "in-progress", Title: "Second"},
		{ID: "c3", Status: "completed", Title: "Third"},
	}
	got := FormatColumns(nibs, []Column{"id", "status", "title"})
	want := "a1\ttodo\tFirst\nb2\tin-progress\tSecond\nc3\tcompleted\tThird\n"
	if got != want {
		t.Errorf("FormatColumns = %q\n          want %q", got, want)
	}
	// Belt-and-braces: exactly one trailing newline.
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("output missing trailing newline: %q", got)
	}
	if strings.HasSuffix(got, "\n\n") {
		t.Errorf("output has extra trailing newline: %q", got)
	}
}

// TestFormatColumns_TagsColumn_JoinedByComma covers the multi-value tag
// column. Tags are joined with ',' (commas don't collide with the tab
// separator). Empty tags render as the empty string.
func TestFormatColumns_TagsColumn_JoinedByComma(t *testing.T) {
	tests := []struct {
		name string
		tags []string
		want string
	}{
		{"multiple tags", []string{"alpha", "beta", "gamma"}, "id1\talpha,beta,gamma\n"},
		{"single tag", []string{"alpha"}, "id1\talpha\n"},
		{"no tags", nil, "id1\t\n"},
		{"empty slice", []string{}, "id1\t\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &nib.Nib{ID: "id1", Tags: tt.tags}
			got := FormatColumns([]*nib.Nib{b}, []Column{"id", "tags"})
			if got != tt.want {
				t.Errorf("FormatColumns tags=%v = %q, want %q", tt.tags, got, tt.want)
			}
		})
	}
}

// TestFormatColumns_EmptyFields_RenderAsEmptyString locks in that missing
// fields render as the empty string (not "<nil>", not "0001-01-01...", not
// "0"). Important for agent consumers that split lines on tab.
func TestFormatColumns_EmptyFields_RenderAsEmptyString(t *testing.T) {
	b := &nib.Nib{ID: "only-id"}
	got := FormatColumns(
		[]*nib.Nib{b},
		[]Column{"id", "slug", "title", "status", "type", "priority", "estimate", "order", "parent", "tags"},
	)
	want := "only-id\t\t\t\t\t\t\t\t\t\n"
	if got != want {
		t.Errorf("FormatColumns of empty nib = %q\n                       want %q", got, want)
	}
}

// TestFormatColumns_TimeColumns_RFC3339_OrEmpty locks in:
//   - non-nil time.Time pointer → RFC3339 string
//   - nil pointer → ""
func TestFormatColumns_TimeColumns_RFC3339_OrEmpty(t *testing.T) {
	tm := time.Date(2026, 5, 6, 14, 30, 0, 0, time.UTC)
	withTime := &nib.Nib{ID: "with", CreatedAt: &tm, UpdatedAt: &tm}
	noTime := &nib.Nib{ID: "without"}

	got := FormatColumns(
		[]*nib.Nib{withTime, noTime},
		[]Column{"id", "created_at", "updated_at"},
	)
	want := "with\t2026-05-06T14:30:00Z\t2026-05-06T14:30:00Z\nwithout\t\t\n"
	if got != want {
		t.Errorf("FormatColumns time = %q\n               want %q", got, want)
	}
}

// TestParseColumns_UnknownErrorListsAvailable ensures the error message lists
// the available column set so callers can recover.
func TestParseColumns_UnknownErrorListsAvailable(t *testing.T) {
	_, err := ParseColumns("bogus")
	if err == nil {
		t.Fatal("expected error for unknown column")
	}
	for _, c := range AvailableColumns {
		if !strings.Contains(err.Error(), string(c)) {
			t.Errorf("error message does not list available column %q: %s", c, err.Error())
		}
	}
}

// TestParseColumns_RejectsDuplicates locks in the duplicate-rejection
// contract: agent consumers that split lines on tab must be able to map a
// column name to a single index. "id,id,title" would otherwise silently
// produce id\tid\ttitle, putting title at index 2 instead of 1.
func TestParseColumns_RejectsDuplicates(t *testing.T) {
	tests := []struct {
		name string
		spec string
	}{
		{"adjacent dup", "id,id,title"},
		{"separated dup", "id,title,id"},
		{"dup with whitespace", " id , id "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseColumns(tt.spec)
			if err == nil {
				t.Fatalf("ParseColumns(%q) should have failed", tt.spec)
			}
			if !strings.Contains(err.Error(), "duplicate") {
				t.Errorf("error %q does not mention duplicate", err.Error())
			}
			// Mirror TestParseColumns_UnknownErrorListsAvailable: the
			// duplicate error must list the available column set so callers
			// can self-correct (consistent with the empty/empty-entry/unknown
			// sibling errors).
			for _, c := range AvailableColumns {
				if !strings.Contains(err.Error(), string(c)) {
					t.Errorf("error message does not list available column %q: %s", c, err.Error())
				}
			}
		})
	}
}

// TestRenderField_AllAvailableColumns_ProduceNonEmpty is the exhaustiveness
// guard: for a fully-populated nib (every renderable field set to a distinct
// non-zero value), every column in AvailableColumns must render to a
// non-empty string. A future commit that adds an entry to AvailableColumns
// (or a typed Column constant) but forgets to extend the renderField switch
// will make this test fire — the bare-string-empty default would otherwise
// silently produce a column of empties.
func TestRenderField_AllAvailableColumns_ProduceNonEmpty(t *testing.T) {
	tm := time.Date(2026, 5, 6, 14, 30, 0, 0, time.UTC)
	full := &nib.Nib{
		ID:        "id-x",
		Slug:      "slug-x",
		Title:     "Title X",
		Status:    "todo",
		Type:      "task",
		Priority:  "normal",
		Estimate:  "m",
		Order:     "a0",
		Parent:    "parent-x",
		Tags:      []string{"alpha", "beta"},
		CreatedAt: &tm,
		UpdatedAt: &tm,
	}
	for _, c := range AvailableColumns {
		got := renderField(full, c)
		if got == "" {
			t.Errorf("renderField(full, %q) = \"\"; AvailableColumns has %q but renderField has no case (or its case returned empty for a fully-populated nib)", c, c)
		}
	}
}
