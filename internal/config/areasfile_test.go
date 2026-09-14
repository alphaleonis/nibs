package config

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// writeStoreConfig writes body as a store's config.yml and returns the store
// directory, so a test can reach the file through either Load or LoadFromStore.
func writeStoreConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yml"), []byte(body), 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	return dir
}

// writeStoreAreas writes body as a store's areas.yml and returns the store
// directory.
func writeStoreAreas(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "areas.yml"), []byte(body), 0o644); err != nil {
		t.Fatalf("writing areas: %v", err)
	}
	return dir
}

func TestLoadAreasFromStoreReadsTheAreasFile(t *testing.T) {
	dir := writeStoreAreas(t, "areas:\n    - name: web\n      children:\n        - name: dashboard\n    - name: auth\n")

	areas, err := LoadAreasFromStore(dir)
	if err != nil {
		t.Fatalf("LoadAreasFromStore: %v", err)
	}

	want := []string{"auth", "web", "web/dashboard"}
	if got := areas.Paths(); !slices.Equal(got, want) {
		t.Errorf("Paths() = %v, want %v", got, want)
	}
	if areas.IsEmpty() {
		t.Error("IsEmpty() = true, want false")
	}
}

// A reordered areas.yml declares the same vocabulary, so a reload of it must not
// count as a change — the watcher would otherwise wake every browser over a
// rendering that comes out identical.
func TestAreasEqualIgnoresFileOrder(t *testing.T) {
	load := func(body string) *Areas {
		t.Helper()
		areas, err := LoadAreasFromStore(writeStoreAreas(t, body))
		if err != nil {
			t.Fatalf("LoadAreasFromStore: %v", err)
		}
		return areas
	}
	declared := load("areas:\n    - name: web\n      children:\n        - name: settings\n        - name: dashboard\n    - name: api\n")

	if reordered := load("areas:\n    - name: api\n    - name: web\n      children:\n        - name: dashboard\n        - name: settings\n"); !declared.Equal(reordered) {
		t.Errorf("Equal = false for vocabularies that differ only in file order: %v vs %v", declared.Paths(), reordered.Paths())
	}
	if changed := load("areas:\n    - name: api\n    - name: web\n      children:\n        - name: dashboard\n"); declared.Equal(changed) {
		t.Errorf("Equal = true for a vocabulary that dropped web/settings: %v", changed.Paths())
	}
}

// A field Equal does not compare makes an edited areas.yml read as unchanged, so
// no configChanged fires and a running server keeps the old vocabulary. Vary each
// AreaConfig field alone; a field kind this cannot vary fails until it is taught.
func TestAreasEqualSeesEveryAreaConfigField(t *testing.T) {
	base := AreaConfig{Name: "web", Description: "front end", Color: "red", Children: []AreaConfig{{Name: "dashboard"}}}
	childrenType := reflect.TypeOf([]AreaConfig(nil))

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
			value.Set(reflect.Append(value, reflect.ValueOf(AreaConfig{Name: "settings"})))
		default:
			t.Fatalf("AreaConfig.%s is a %s, which this test cannot vary; teach it the field", field.Name, field.Type)
		}

		if (&Areas{Nodes: []AreaConfig{base}}).Equal(&Areas{Nodes: []AreaConfig{changed}}) {
			t.Errorf("Equal = true for vocabularies that differ only in AreaConfig.%s", field.Name)
		}
	}
}

func TestLoadAreasFromStoreWithNoFileDeclaresNothing(t *testing.T) {
	areas, err := LoadAreasFromStore(t.TempDir())
	if err != nil {
		t.Fatalf("LoadAreasFromStore: %v", err)
	}
	if !areas.IsEmpty() {
		t.Error("IsEmpty() = false for a store with no areas.yml, want true")
	}
	if got := areas.Paths(); len(got) != 0 {
		t.Errorf("Paths() = %v, want empty", got)
	}
}

func TestLoadAreasFromStoreRefusesAMalformedVocabulary(t *testing.T) {
	dir := writeStoreAreas(t, "areas:\n    - name: auth\n    - name: auth\n")

	_, err := LoadAreasFromStore(dir)
	if err == nil {
		t.Fatal("LoadAreasFromStore accepted duplicate siblings, want a refusal")
	}
	for _, want := range []string{"areas.yml", "duplicate", `"auth"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// A nil vocabulary is what a Core with no store file holds, and every reader
// must answer from it rather than panic.
func TestNilAreasAnswersEveryQuery(t *testing.T) {
	var areas *Areas

	if !areas.IsEmpty() {
		t.Error("IsEmpty() = false on nil, want true")
	}
	if got := areas.Paths(); len(got) != 0 {
		t.Errorf("Paths() = %v on nil, want empty", got)
	}
	if areas.Exists("web") {
		t.Error("Exists(web) = true on nil, want false")
	}
	if areas.Get("web") != nil {
		t.Error("Get(web) != nil on nil")
	}
	if areas.IsWithin("web/dashboard", "web") {
		t.Error("IsWithin = true on nil, want false")
	}
	if err := areas.Validate(); err != nil {
		t.Errorf("Validate() = %v on nil, want nil", err)
	}
	if err := areas.ValidateAssignment(""); err != nil {
		t.Errorf("ValidateAssignment(\"\") = %v on nil, want nil", err)
	}
	if err := areas.ValidateAssignment("web"); err == nil {
		t.Error("ValidateAssignment(web) = nil on nil, want a refusal")
	}
}

// A config.yml still carrying the block is refused rather than ignored: an
// ignored block would keep reading like a declaration while authorizing
// nothing, which is exactly the silent state the split exists to prevent.
func TestLoadRefusesAConfigThatStillDeclaresAreas(t *testing.T) {
	dir := writeStoreConfig(t, "nibs:\n    prefix: t-\nareas:\n    - name: web\n")

	_, err := LoadFromStore(dir)
	if err == nil {
		t.Fatal("LoadFromStore accepted an `areas:` block in config.yml, want a refusal")
	}
	for _, want := range []string{"areas:", "areas.yml", "config.yml"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// A config.yml with no areas block loads, and the two files are read
// independently: the vocabulary comes from its own file either way.
func TestConfigAndAreasLoadIndependently(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yml"), []byte("nibs:\n    prefix: t-\n"), 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "areas.yml"), []byte("areas:\n    - name: web\n"), 0o644); err != nil {
		t.Fatalf("writing areas: %v", err)
	}

	cfg, err := LoadFromStore(dir)
	if err != nil {
		t.Fatalf("LoadFromStore: %v", err)
	}
	if cfg.Nibs.Prefix != "t-" {
		t.Errorf("prefix = %q, want t-", cfg.Nibs.Prefix)
	}

	areas, err := LoadAreasFromStore(dir)
	if err != nil {
		t.Fatalf("LoadAreasFromStore: %v", err)
	}
	if !areas.Exists("web") {
		t.Error("Exists(web) = false, want true")
	}
}
