package filterlang

import (
	"reflect"
	"slices"
	"testing"
)

func TestCompleteOffersSearchedNibsForAnIDValue(t *testing.T) {
	var gotFragment string
	var gotTypes []string
	calls := 0
	v := Vocabulary{SearchNibs: func(fragment string, types []string) []NibSuggestion {
		calls++
		gotFragment, gotTypes = fragment, types
		return []NibSuggestion{{ID: "tnib-12", Title: "Ship it", Type: "milestone", Status: "todo"}}
	}}

	text := "type:bug milestone:t1x status:todo"
	got := Complete(text, len("type:bug milestone:t1"), v)

	if calls != 1 || gotFragment != "t1x" || !slices.Equal(gotTypes, []string{"milestone"}) {
		t.Fatalf("SearchNibs called %d times with (%q, %q), want once with (\"t1x\", [milestone])", calls, gotFragment, gotTypes)
	}
	want := []Completion{{
		Kind: CompletionNib, Item: "tnib-12",
		Nib:   NibSuggestion{ID: "tnib-12", Title: "Ship it", Type: "milestone", Status: "todo"},
		Start: 19, End: 22, Insert: "tnib-12", Cursor: 26,
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Complete = %+v, want %+v", got, want)
	}
	edited, cursor := got[0].Apply(text)
	if edited != "type:bug milestone:tnib-12 status:todo" || cursor != 26 {
		t.Errorf("Apply = (%q, %d)", edited, cursor)
	}
}

// A relationship token with no type constraint passes nil types, and an empty
// value searches nothing, though the cursor still sits in it.
func TestCompleteSearchesOnlyANonEmptyIDValue(t *testing.T) {
	var searched []string
	var types [][]string
	v := Vocabulary{SearchNibs: func(fragment string, ts []string) []NibSuggestion {
		searched = append(searched, fragment)
		types = append(types, ts)
		return nil
	}}
	CompleteExplicit("parent:", 7, v)
	Complete("parent:x", 8, v)
	if !slices.Equal(searched, []string{"x"}) || types[0] != nil {
		t.Errorf("searched %q with types %v, want [x] with nil types", searched, types)
	}
	if got := Complete("parent:x", 8, Vocabulary{}); got != nil {
		t.Errorf("Complete with no SearchNibs = %+v, want nil", got)
	}
}

func TestRelValueAtClonesSearchTypes(t *testing.T) {
	rel, ok := RelValueAt("milestone:x", 11)
	if !ok {
		t.Fatal("no relationship value")
	}
	rel.Token.SearchTypes[0] = "bug"
	if again, _ := RelValueAt("milestone:x", 11); again.Token.SearchTypes[0] != "milestone" {
		t.Errorf("a caller's write reached the token table: %q", again.Token.SearchTypes)
	}
}

func TestCompleteClampsTheCursor(t *testing.T) {
	if got := Complete("ty", 99, Vocabulary{}); len(got) != 1 || got[0].Item != "type" {
		t.Errorf("a cursor past the end = %+v, want the completion at the end", got)
	}
	if got := CompleteExplicit("ty", -3, Vocabulary{}); len(got) != len(allFieldNames) {
		t.Errorf("a negative cursor offered %d names, want every field name", len(got))
	}
	if _, ok := RelValueAt("parent:x", 99); !ok {
		t.Error("a cursor past the end of an id value is not in it")
	}
}

// Offsets count bytes: the no-break space before the token is two.
func TestCompletionOffsetsCountBytes(t *testing.T) {
	text := "\u00a0st"
	got := Complete(text, len(text), Vocabulary{})
	if len(got) != 1 || got[0].Start != 2 || got[0].End != 4 || got[0].Cursor != 9 {
		t.Fatalf("Complete(%q) = %+v, want status replacing bytes 2 to 4 with the cursor at 9", text, got)
	}
}
