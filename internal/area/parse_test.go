package area

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestParseReadsTheAreasBlock(t *testing.T) {
	vocab := parseVocab(t, "areas:\n    - name: web\n      children:\n        - name: dashboard\n    - name: auth\n")

	want := []string{"auth", "web", "web/dashboard"}
	if got := vocab.Paths(); !slices.Equal(got, want) {
		t.Errorf("Paths() = %v, want %v", got, want)
	}
	if vocab.IsEmpty() {
		t.Error("IsEmpty() = true, want false")
	}
}

// TestParseErrorNamesTheFileItWasGiven pins the two renderings a refusal has:
// without a File it names the store's areas.yml generically, and with one it
// names that path, which is what the loader sets.
func TestParseErrorNamesTheFileItWasGiven(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		malformed bool
		sentence  string
	}{
		{name: "content that does not decode", body: "areas:\n  - name: [unclosed\n", sentence: "is not readable as an areas vocabulary"},
		{name: "content that declares an unusable vocabulary", body: "areas:\n    - name: auth\n    - name: auth\n", malformed: true, sentence: "declares a malformed area vocabulary"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.body))
			var parseErr *ParseError
			if !errors.As(err, &parseErr) {
				t.Fatalf("error = %v (%T), want *ParseError", err, err)
			}
			if parseErr.Malformed != tt.malformed {
				t.Errorf("Malformed = %v, want %v", parseErr.Malformed, tt.malformed)
			}
			if got, want := err.Error(), "this store's areas.yml "+tt.sentence; !strings.HasPrefix(got, want) {
				t.Errorf("Error() = %q, want it to begin %q", got, want)
			}
			parseErr.File = "/store/areas.yml"
			if got, want := err.Error(), "/store/areas.yml "+tt.sentence; !strings.HasPrefix(got, want) {
				t.Errorf("Error() with File = %q, want it to begin %q", got, want)
			}
		})
	}
}

// A reordered areas.yml declares the same vocabulary, so a reload of it must not
// count as a change — the watcher would otherwise wake every browser over a
// rendering that comes out identical.
func TestVocabularyEqualIgnoresFileOrder(t *testing.T) {
	declared := parseVocab(t, "areas:\n    - name: web\n      children:\n        - name: settings\n        - name: dashboard\n    - name: api\n")

	if reordered := parseVocab(t, "areas:\n    - name: api\n    - name: web\n      children:\n        - name: dashboard\n        - name: settings\n"); !declared.Equal(reordered) {
		t.Errorf("Equal = false for vocabularies that differ only in file order: %v vs %v", declared.Paths(), reordered.Paths())
	}
	if changed := parseVocab(t, "areas:\n    - name: api\n    - name: web\n      children:\n        - name: dashboard\n"); declared.Equal(changed) {
		t.Errorf("Equal = true for a vocabulary that dropped web/settings: %v", changed.Paths())
	}
}

// A field Equal does not compare makes an edited areas.yml read as unchanged, so
// no configChanged fires and a running server keeps the old vocabulary. Vary each
// Node field alone; a field kind this cannot vary fails until it is taught.
func TestVocabularyEqualSeesEveryNodeField(t *testing.T) {
	base := Node{Name: "web", Description: "front end", Color: "red", Children: []Node{{Name: "dashboard"}}}
	childrenType := reflect.TypeOf([]Node(nil))

	typ := reflect.TypeOf(base)
	for i := range typ.NumField() {
		field := typ.Field(i)
		changed := base
		changed.Children = slices.Clone(base.Children)

		value := reflect.ValueOf(&changed).Elem().Field(i)
		switch {
		case field.Type.Kind() == reflect.String:
			value.SetString(value.String() + "-changed")
		case field.Type == childrenType:
			value.Set(reflect.Append(value, reflect.ValueOf(Node{Name: "settings"})))
		default:
			t.Fatalf("Node.%s is a %s, which this test cannot vary; teach it the field", field.Name, field.Type)
		}

		if (&Vocabulary{Nodes: []Node{base}}).Equal(&Vocabulary{Nodes: []Node{changed}}) {
			t.Errorf("Equal = true for vocabularies that differ only in Node.%s", field.Name)
		}
	}
}

// TestVocabularyMarshalRoundTrips pins the yaml tags against Parse: what a
// Vocabulary marshals to, Parse must read back node for node. Tests across the
// repository write fixtures this way.
func TestVocabularyMarshalRoundTrips(t *testing.T) {
	vocab := &Vocabulary{Nodes: []Node{
		{
			Name:        "web",
			Description: "The browser client",
			Color:       "blue",
			Children:    []Node{{Name: "dashboard", Description: "The landing dashboard"}},
		},
		{Name: "auth", Description: "Sign-in and sessions"},
	}}

	raw, err := yaml.Marshal(vocab)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(raw), "areas:\n") {
		t.Errorf("marshaled vocabulary has no top-level areas block:\n%s", raw)
	}
	if got := parseVocab(t, string(raw)); !got.Equal(vocab) {
		t.Errorf("Parse(Marshal(v)) = %v, want %v", got.Paths(), vocab.Paths())
	}
}

// A nil vocabulary answers as the empty one, and every reader must answer from
// it rather than panic.
func TestNilVocabularyAnswersEveryQuery(t *testing.T) {
	var vocab *Vocabulary

	if !vocab.IsEmpty() {
		t.Error("IsEmpty() = false on nil, want true")
	}
	if got := vocab.Paths(); len(got) != 0 {
		t.Errorf("Paths() = %v on nil, want empty", got)
	}
	if vocab.Exists("web") {
		t.Error("Exists(web) = true on nil, want false")
	}
	if vocab.Get("web") != nil {
		t.Error("Get(web) != nil on nil")
	}
	if vocab.IsWithin("web/dashboard", "web") {
		t.Error("IsWithin = true on nil, want false")
	}
	if err := vocab.Validate(); err != nil {
		t.Errorf("Validate() = %v on nil, want nil", err)
	}
	if err := vocab.ValidateAssignment(""); err != nil {
		t.Errorf("ValidateAssignment(\"\") = %v on nil, want nil", err)
	}
	if err := vocab.ValidateAssignment("web"); err == nil {
		t.Error("ValidateAssignment(web) = nil on nil, want a refusal")
	}
	if !vocab.Equal(&Vocabulary{}) {
		t.Error("Equal(empty) = false on nil, want true")
	}
}
