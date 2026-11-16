package filterlang

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph/model"
)

func TestGeneratedFilterTokensAreFresh(t *testing.T) {
	committed, err := os.ReadFile(filepath.Join(moduleRoot(t), filepath.FromSlash(OutputPath)))
	if err != nil {
		t.Fatalf("reading the committed %s: %v — run `task codegen` and commit the result", OutputPath, err)
	}
	if string(committed) != Render() {
		t.Fatalf("%s is stale: the token tables changed without regenerating it — run `task codegen` and commit the result", OutputPath)
	}
}

// TestTokenKeysNameNibFilterFields requires every key a token writes to be a
// NibFilter field of the Go type that token writes: a value list, an id, or a
// tri-state flag. Two tokens write the same key only as the two spellings of
// one existence field.
func TestTokenKeysNameNibFilterFields(t *testing.T) {
	kinds := nibFilterKinds()
	stringList := reflect.TypeFor[[]string]()
	stringPtr := reflect.TypeFor[*string]()
	boolPtr := reflect.TypeFor[*bool]()

	written := map[string]string{} // NibFilter key → the token that writes it
	claim := func(key, token string, want reflect.Type) {
		t.Helper()
		got, ok := kinds[key]
		if !ok {
			t.Errorf("%s writes %q, which is not a NibFilter field", token, key)
			return
		}
		if got != want {
			t.Errorf("%s writes %q as %v, but NibFilter declares it %v", token, key, want, got)
		}
		if prior, dup := written[key]; dup {
			t.Errorf("%s and %s both write %q", prior, token, key)
		}
		written[key] = token
	}

	for _, f := range fields {
		claim(f.FilterKey, f.Name+":", stringList)
		claim(f.ExcludeKey, "-"+f.Name+":", stringList)
	}
	spellings := map[string][]bool{} // existence field → the values its spellings write
	for _, r := range relTokens {
		switch r.Kind {
		case RelID:
			claim(r.Field, r.Name+":", stringPtr)
		case RelBool:
			if slices.Contains(spellings[r.Field], r.Value) {
				t.Errorf("%s is a second spelling of %s=%t", r.Token, r.Field, r.Value)
			}
			if len(spellings[r.Field]) == 0 {
				claim(r.Field, r.Token, boolPtr)
			}
			spellings[r.Field] = append(spellings[r.Field], r.Value)
		default:
			t.Errorf("%+v has kind %q, which Render does not spell", r, r.Kind)
		}
	}
	for _, a := range areaTokens {
		claim(a.Field, a.Name+":", stringPtr)
	}
}

// TestTokenNamesAreDistinct requires each name to reach one token. The web's
// parser tries metadata fields first, then relationship tokens, then area
// tokens, so a shared name would shadow a later table's token.
func TestTokenNamesAreDistinct(t *testing.T) {
	metadataName := regexp.MustCompile(`^[a-z]+$`) // the web's FIELD_TOKEN reads letters only
	hyphenatedName := regexp.MustCompile(`^[a-z]+(?:-[a-z]+)*$`)

	owner := map[string]string{}
	name := func(n, table string, pattern *regexp.Regexp) {
		t.Helper()
		if !pattern.MatchString(n) {
			t.Errorf("%s name %q does not match %s", table, n, pattern)
		}
		if prior, dup := owner[n]; dup && prior != table {
			t.Errorf("%q names both a %s token and a %s token", n, prior, table)
		}
		owner[n] = table
	}

	for _, f := range fields {
		if owner[f.Name] == "field" {
			t.Errorf("two fields are named %q", f.Name)
		}
		name(f.Name, "field", metadataName)
	}
	existence := map[string]bool{}
	for _, r := range relTokens {
		switch r.Kind {
		case RelID:
			if owner[r.Name] == "relationship-id" {
				t.Errorf("relationship-id name %q appears twice", r.Name)
			}
			name(r.Name, "relationship-id", hyphenatedName)
		case RelBool:
			word, value, ok := strings.Cut(r.Token, ":")
			if !ok || strings.Contains(value, ":") || value == "" {
				t.Errorf("existence token %q is not word:value with one colon", r.Token)
				continue
			}
			if existence[r.Token] {
				t.Errorf("existence token %q appears twice", r.Token)
			}
			existence[r.Token] = true
			name(word, "existence", metadataName)
			if !hyphenatedName.MatchString(value) {
				t.Errorf("existence token %q has value %q, which does not match %s", r.Token, value, hyphenatedName)
			}
		}
	}
	for _, a := range areaTokens {
		if owner[a.Name] == "area" {
			t.Errorf("area name %q appears twice", a.Name)
		}
		name(a.Name, "area", hyphenatedName)
	}
}

// TestFieldValueSetsAreKnown requires each field's values to come from a
// declared value set, and each enumerated value set to name a non-empty
// vocabulary in the default config.
func TestFieldValueSetsAreKnown(t *testing.T) {
	cfg := config.Default()
	vocabulary := map[ValueSet]int{
		TypeValues:     len(cfg.TypeNames()),
		PriorityValues: len(cfg.PriorityNames()),
		StatusValues:   len(cfg.StatusNames()),
		EstimateValues: len(cfg.EstimateNames()),
	}
	for _, f := range fields {
		if !slices.Contains(valueSets, f.Values) {
			t.Errorf("field %q takes values from %q, which is not in valueSets", f.Name, f.Values)
			continue
		}
		if f.Values == TagValues {
			continue
		}
		if n, ok := vocabulary[f.Values]; !ok || n == 0 {
			t.Errorf("field %q takes values from %q, which names no default vocabulary", f.Name, f.Values)
		}
	}
}

func TestSearchTypesAreDeclaredTypes(t *testing.T) {
	declared := map[string]bool{}
	for _, ty := range config.Default().TypeNames() {
		declared[ty] = true
	}
	for _, r := range relTokens {
		if r.Kind != RelID && r.SearchTypes != nil {
			t.Errorf("existence token %q carries search types", r.Token)
		}
		for _, ty := range r.SearchTypes {
			if !declared[ty] {
				t.Errorf("%s: search type %q is not a declared nib type", r.Name, ty)
			}
		}
	}
}

func TestEveryTokenHasHelpText(t *testing.T) {
	for _, r := range relTokens {
		if strings.TrimSpace(r.Description) == "" {
			t.Errorf("%+v has no description", r)
		}
	}
	for _, a := range areaTokens {
		if strings.TrimSpace(a.Description) == "" {
			t.Errorf("%+v has no description", a)
		}
	}
	for _, row := range slices.Concat(helpOperators, helpExamples) {
		if strings.TrimSpace(row.Token) == "" || strings.TrimSpace(row.Description) == "" {
			t.Errorf("help row %+v is missing its token or its description", row)
		}
	}
}

// nibFilterKinds maps each NibFilter field's GraphQL name to its Go type.
func nibFilterKinds() map[string]reflect.Type {
	typ := reflect.TypeFor[model.NibFilter]()
	kinds := make(map[string]reflect.Type, typ.NumField())
	for i := range typ.NumField() {
		f := typ.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		kinds[name] = f.Type
	}
	return kinds
}

// moduleRoot walks up from the package directory to the directory holding
// go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod in any parent of the test's working directory")
		}
		dir = parent
	}
}

// The exported value lists are what a token parses to and serializes from.
func TestValuesAndGroupMembersAgreeWithParse(t *testing.T) {
	all, _ := Parse("status:open,closed")
	if got := Values(StatusValues); !slices.Equal(got, all.Status) {
		t.Errorf("Values(StatusValues) = %v, want %v", got, all.Status)
	}
	open, _ := Parse("status:open")
	if got := GroupMembers(StatusValues, config.StatusGroupOpen); !slices.Equal(got, open.Status) {
		t.Errorf("GroupMembers(open) = %v, want %v", got, open.Status)
	}
	if got := Serialize(model.NibFilter{Status: GroupMembers(StatusValues, config.StatusGroupOpen)}, nil); got != "status:open" {
		t.Errorf("the open members serialize as %q, want status:open", got)
	}
	if Values(TagValues) != nil || GroupMembers(TypeValues, "open") != nil {
		t.Error("a set without values or groups returns some")
	}
}
