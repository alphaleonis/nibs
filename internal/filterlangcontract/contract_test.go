package filterlangcontract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
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

// TestSpanCasesPassInGo is the Go half of the highlighting table. The web half
// is filterInputContract.test.ts.
func TestSpanCasesPassInGo(t *testing.T) {
	for _, c := range SpanCases() {
		t.Run(c.Name, func(t *testing.T) {
			text := c.Text()
			got := []WireSpan{}
			for _, s := range filterlang.SpansWithAreas(text, c.Areas) {
				got = append(got, WireSpan{UTF16Offset(text, s.Start), UTF16Offset(text, s.End), string(s.Kind)})
			}
			if want := c.WantSpans(); !slices.Equal(got, want) {
				t.Errorf("spans of %q\n got: %v\nwant: %v", text, got, want)
			}
		})
	}
}

// TestCompletionCasesPassInGo is the Go half of the completion table. It runs
// filterlang.Complete with no SearchNibs, so a cursor in an id value offers
// nothing, as getCompletion offers nothing there.
func TestCompletionCasesPassInGo(t *testing.T) {
	for _, c := range CompletionCases() {
		t.Run(c.Name, func(t *testing.T) {
			text := strings.Replace(c.Text, Caret, "", 1)
			cursor := strings.Index(c.Text, Caret)

			var gotRel *WireRel
			if rel, ok := filterlang.RelValueAt(text, cursor); ok {
				gotRel = &WireRel{
					Field: rel.Token.Field, Name: rel.Token.Name, Fragment: rel.Fragment,
					Start: UTF16Offset(text, rel.Start), End: UTF16Offset(text, rel.End),
					SearchTypes: rel.Token.SearchTypes,
				}
			}
			if want := c.WantRel(); !reflect.DeepEqual(gotRel, want) {
				t.Errorf("relationship value\n got: %+v\nwant: %+v", gotRel, want)
			}

			vocab := filterlang.Vocabulary{Tags: c.Tags, Areas: c.Areas}
			complete := filterlang.Complete
			if c.Explicit {
				complete = filterlang.CompleteExplicit
			}
			var got *WireCompletion
			for _, comp := range complete(text, cursor, vocab) {
				if got == nil {
					got = &WireCompletion{Kind: string(comp.Kind)}
				} else if string(comp.Kind) != got.Kind {
					t.Errorf("completion %q is a %s among %s completions", comp.Item, comp.Kind, got.Kind)
				}
				edited, caret := comp.Apply(text)
				got.Items = append(got.Items, comp.Item)
				got.Applied = append(got.Applied, WireApply{edited, UTF16Offset(edited, caret)})
			}
			if want := c.WantCompletion(); !reflect.DeepEqual(got, want) {
				t.Errorf("completion\n got: %s\nwant: %s", asJSON(t, got), asJSON(t, want))
			}
		})
	}
}

// TestInputCasesAreWellFormed requires distinct names, one Caret in each
// completion text and result, and a relationship range that brackets the
// case's own text.
func TestInputCasesAreWellFormed(t *testing.T) {
	names := map[string]bool{}
	for _, c := range SpanCases() {
		if names[c.Name] {
			t.Errorf("two span cases are named %q", c.Name)
		}
		names[c.Name] = true
		if strings.Contains(c.Text(), Caret) {
			t.Errorf("span case %q holds the caret marker", c.Name)
		}
		for _, p := range c.Spans {
			if p.Text == "" {
				t.Errorf("span case %q has an empty %s span", c.Name, p.Kind)
			}
		}
	}
	names = map[string]bool{}
	for _, c := range CompletionCases() {
		if names[c.Name] {
			t.Errorf("two completion cases are named %q", c.Name)
		}
		names[c.Name] = true
		if strings.Count(c.Text, Caret) != 1 {
			t.Errorf("completion case %q must hold one caret marker", c.Name)
		}
		for _, o := range c.Offers {
			if strings.Count(o.Result, Caret) != 1 {
				t.Errorf("completion case %q: the result for %q must hold one caret marker", c.Name, o.Item)
			}
		}
		if (c.Kind == "") != (len(c.Offers) == 0) {
			t.Errorf("completion case %q must set Kind exactly when it has offers", c.Name)
		}
		if c.Rel != nil {
			text, _ := c.Input()
			bare := strings.Replace(strings.Replace(c.Rel.Range, "[", "", 1), "]", "", 1)
			if bare != text || strings.Count(c.Rel.Range, "[") != 1 || strings.Count(c.Rel.Range, "]") != 1 {
				t.Errorf("completion case %q: the range %q must bracket the text %q once", c.Name, c.Rel.Range, text)
			}
		}
	}
}

// TestGeneratedFilterInputContractIsFresh holds the committed module
// byte-identical to RenderInput, so the web runs the cases this package
// defines.
func TestGeneratedFilterInputContractIsFresh(t *testing.T) {
	committed, err := os.ReadFile(filepath.Join(moduleRoot(t), filepath.FromSlash(InputOutputPath)))
	if err != nil {
		t.Fatalf("reading the committed %s: %v — run `task codegen` and commit the result", InputOutputPath, err)
	}
	if string(committed) != RenderInput() {
		t.Fatalf("%s is stale: the cases or the renderer changed without regenerating it — run `task codegen` and commit the result", InputOutputPath)
	}
}
