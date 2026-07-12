package projection

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
)

// sampleNibs returns three nibs with distinct scalar values for list tests.
func sampleNibs() []*nib.Nib {
	return []*nib.Nib{
		{ID: "a1", Title: "First", Status: "todo", Tags: []string{"x", "y"}},
		{ID: "b2", Title: "Second", Status: "in-progress"},
		{ID: "c3", Title: "Third", Status: "completed", Tags: []string{"z"}},
	}
}

// TestProjectList_JSONEnvelope pins the list envelope shape: a top-level
// {"nibs":[…],"count":n,"truncated":false} with each element the flat projected
// object in menu order, and no limit applied.
func TestProjectList_JSONEnvelope(t *testing.T) {
	sel, err := ParseFields("id,status")
	if err != nil {
		t.Fatalf("ParseFields: %v", err)
	}
	pl, err := ProjectList(sampleNibs(), sel, nil, 0)
	if err != nil {
		t.Fatalf("ProjectList: %v", err)
	}
	got, err := json.Marshal(pl)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := `{"nibs":[{"id":"a1","status":"todo"},{"id":"b2","status":"in-progress"},{"id":"c3","status":"completed"}],"count":3,"truncated":false}`
	if string(got) != want {
		t.Errorf("envelope JSON =\n  %s\nwant\n  %s", got, want)
	}
	if pl.Count() != 3 {
		t.Errorf("Count() = %d, want 3", pl.Count())
	}
	if pl.Truncated() {
		t.Error("Truncated() = true, want false")
	}
}

// TestProjectList_EmptyEnvelope asserts an empty input yields nibs:[] (not null),
// count:0, truncated:false — a consumer can index .nibs unconditionally.
func TestProjectList_EmptyEnvelope(t *testing.T) {
	sel, _ := ParseFields("id,status")
	pl, err := ProjectList(nil, sel, nil, 0)
	if err != nil {
		t.Fatalf("ProjectList: %v", err)
	}
	got, _ := json.Marshal(pl)
	want := `{"nibs":[],"count":0,"truncated":false}`
	if string(got) != want {
		t.Errorf("empty envelope = %s, want %s", got, want)
	}
}

// TestProjectList_LimitTruncates covers --limit: when the input exceeds the
// limit, only the first N are projected, count is N, and truncated is true.
func TestProjectList_LimitTruncates(t *testing.T) {
	sel, _ := ParseFields("id")
	pl, err := ProjectList(sampleNibs(), sel, nil, 2)
	if err != nil {
		t.Fatalf("ProjectList: %v", err)
	}
	got, _ := json.Marshal(pl)
	want := `{"nibs":[{"id":"a1"},{"id":"b2"}],"count":2,"truncated":true}`
	if string(got) != want {
		t.Errorf("truncated envelope = %s, want %s", got, want)
	}
	if !pl.Truncated() {
		t.Error("Truncated() = false, want true")
	}
	if pl.Count() != 2 {
		t.Errorf("Count() = %d, want 2", pl.Count())
	}
}

// TestProjectList_LimitNotExceeded asserts a limit at or above the input size
// keeps every element and leaves truncated false.
func TestProjectList_LimitNotExceeded(t *testing.T) {
	sel, _ := ParseFields("id")
	for _, limit := range []int{3, 5} {
		pl, err := ProjectList(sampleNibs(), sel, nil, limit)
		if err != nil {
			t.Fatalf("ProjectList(limit=%d): %v", limit, err)
		}
		if pl.Truncated() {
			t.Errorf("limit=%d: Truncated() = true, want false", limit)
		}
		if pl.Count() != 3 {
			t.Errorf("limit=%d: Count() = %d, want 3", limit, pl.Count())
		}
	}
}

// TestProjectList_DoesNotMutateInput guards that truncation reslices a local
// copy of the slice header and never mutates the caller's slice.
func TestProjectList_DoesNotMutateInput(t *testing.T) {
	sel, _ := ParseFields("id")
	in := sampleNibs()
	if _, err := ProjectList(in, sel, nil, 1); err != nil {
		t.Fatalf("ProjectList: %v", err)
	}
	if len(in) != 3 {
		t.Errorf("input slice length changed to %d, want 3", len(in))
	}
}

// TestProjectList_MenuOrder asserts elements render in canonical menu order
// regardless of the -f token order (status listed before id → id first).
func TestProjectList_MenuOrder(t *testing.T) {
	sel, _ := ParseFields("status,id,title")
	pl, err := ProjectList(sampleNibs()[:1], sel, nil, 0)
	if err != nil {
		t.Fatalf("ProjectList: %v", err)
	}
	got, _ := json.Marshal(pl)
	want := `{"nibs":[{"id":"a1","title":"First","status":"todo"}],"count":1,"truncated":false}`
	if string(got) != want {
		t.Errorf("menu-order envelope = %s, want %s", got, want)
	}
	// The TSV grid is menu-ordered too.
	rows := pl.Rows()
	if len(rows) != 1 || strings.Join(rows[0], "|") != "a1|First|todo" {
		t.Errorf("Rows() = %v, want [[a1 First todo]]", rows)
	}
}

// TestProjectList_RowsGrid covers the TSV grid Rows() produces: one []string
// per nib in menu order with multi-value tags comma-joined and missing fields
// rendered as "". This grid is what output.FormatListTSV assembles into text
// (that assembly is covered in the output package).
func TestProjectList_RowsGrid(t *testing.T) {
	sel, _ := ParseFields("id,status,tags")
	pl, err := ProjectList(sampleNibs(), sel, nil, 0)
	if err != nil {
		t.Fatalf("ProjectList: %v", err)
	}
	want := [][]string{
		{"a1", "todo", "x,y"},
		{"b2", "in-progress", ""},
		{"c3", "completed", "z"},
	}
	got := pl.Rows()
	if len(got) != len(want) {
		t.Fatalf("Rows() len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if strings.Join(got[i], "|") != strings.Join(want[i], "|") {
			t.Errorf("Rows()[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

// TestProjectList_EmptyGrid asserts an empty list produces a zero-length grid
// (which output.FormatListTSV renders as "# 0 nibs").
func TestProjectList_EmptyGrid(t *testing.T) {
	sel, _ := ParseFields("id,status")
	pl, err := ProjectList(nil, sel, nil, 0)
	if err != nil {
		t.Fatalf("ProjectList: %v", err)
	}
	if got := pl.Rows(); len(got) != 0 {
		t.Errorf("Rows() = %v, want empty grid", got)
	}
}

// TestProjectList_IdListTSVCommaJoined covers a bare relation (id-list) field
// rendering as a comma-joined cell in the TSV grid.
func TestProjectList_IdListTSVCommaJoined(t *testing.T) {
	n := &nib.Nib{ID: "a", BlockedBy: []string{"b1", "b2"}}
	sel, _ := ParseFields("id,blocked-by")
	pl, err := ProjectList([]*nib.Nib{n}, sel, nil, 0)
	if err != nil {
		t.Fatalf("ProjectList: %v", err)
	}
	rows := pl.Rows()
	if len(rows) != 1 || strings.Join(rows[0], "|") != "a|b1,b2" {
		t.Errorf("Rows() = %v, want [[a b1,b2]]", rows)
	}
}

// TestProjectList_ErrorPropagates asserts a per-element projection error (a
// computed field with a nil resolver) surfaces from ProjectList rather than
// being swallowed.
func TestProjectList_ErrorPropagates(t *testing.T) {
	sel, _ := ParseFields("children")
	if _, err := ProjectList(sampleNibs(), sel, nil, 0); err == nil {
		t.Fatal("ProjectList with a computed field and nil resolver should error")
	}
}

// TestProjectList_Resolver covers projecting a computed field through a resolver
// across multiple nibs.
func TestProjectList_Resolver(t *testing.T) {
	nibs := []*nib.Nib{{ID: "a"}, {ID: "b"}}
	r := &fakeResolver{childCount: map[string]int{"a": 2, "b": 0}}
	sel, _ := ParseFields("id,children")
	pl, err := ProjectList(nibs, sel, r, 0)
	if err != nil {
		t.Fatalf("ProjectList: %v", err)
	}
	got, _ := json.Marshal(pl)
	want := `{"nibs":[{"id":"a","children":2},{"id":"b","children":0}],"count":2,"truncated":false}`
	if string(got) != want {
		t.Errorf("resolver envelope = %s, want %s", got, want)
	}
}

// TestCount covers the -c path: a bare integer count of the input, independent
// of any limit or projection.
func TestCount(t *testing.T) {
	if got := Count(sampleNibs()); got != 3 {
		t.Errorf("Count(sample) = %d, want 3", got)
	}
	if got := Count(nil); got != 0 {
		t.Errorf("Count(nil) = %d, want 0", got)
	}
}
