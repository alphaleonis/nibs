package filterlangcontract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/alphaleonis/nibs/internal/filterlang"
)

// TestCasesPassInGo is the Go half of the parity table. The web half is
// filterParseContract.test.ts.
func TestCasesPassInGo(t *testing.T) {
	for _, c := range Cases() {
		t.Run(c.Name, func(t *testing.T) {
			got, diags := filterlang.ParseWithAreas(c.Text, c.Areas)
			if !reflect.DeepEqual(got, c.Want) {
				t.Errorf("filter differs\n got: %s\nwant: %s", asJSON(t, got), asJSON(t, c.Want))
			}
			invalid := filterlang.InvalidTokens(diags)
			if !slices.Equal(invalid, c.Invalid) {
				t.Errorf("invalid tokens = %q, want %q", invalid, c.Invalid)
			}
			if text := filterlang.Serialize(got, invalid); text != c.Canonical {
				t.Errorf("Serialize = %q, want %q", text, c.Canonical)
			}
		})
	}
}

// TestCasesCoverTheSyntaxHelp requires a case for each query the filter box's
// syntax help shows, so the help never shows a query the two parsers read
// differently.
func TestCasesCoverTheSyntaxHelp(t *testing.T) {
	texts := map[string]bool{}
	names := map[string]bool{}
	for _, c := range Cases() {
		texts[c.Text] = true
		if names[c.Name] {
			t.Errorf("two cases are named %q", c.Name)
		}
		names[c.Name] = true
	}
	for _, row := range slices.Concat(filterlang.HelpExamples(), filterlang.HelpOperators()) {
		if !texts[row.Token] {
			t.Errorf("no case parses the help row %q", row.Token)
		}
	}
}

// TestGeneratedFilterParseContractIsFresh holds the committed module
// byte-identical to Render, so the web runs the cases this package defines.
func TestGeneratedFilterParseContractIsFresh(t *testing.T) {
	committed, err := os.ReadFile(filepath.Join(moduleRoot(t), filepath.FromSlash(OutputPath)))
	if err != nil {
		t.Fatalf("reading the committed %s: %v — run `task codegen` and commit the result", OutputPath, err)
	}
	if string(committed) != Render() {
		t.Fatalf("%s is stale: the cases or the renderer changed without regenerating it — run `task codegen` and commit the result", OutputPath)
	}
}

func asJSON(t *testing.T, v any) string {
	t.Helper()
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
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
			t.Fatal("no go.mod in any parent of the working directory")
		}
		dir = parent
	}
}
