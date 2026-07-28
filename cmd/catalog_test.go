package cmd

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/pflag"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nibtypes"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/internal/projection"
)

// resetCatalogFlags clears the package-level flag var used by catalogCmd so
// tests don't pollute each other via rootCmd's singleton state.
func resetCatalogFlags() {
	catalogJSON = false
	catalogCmd.Flags().Visit(func(f *pflag.Flag) { f.Changed = false })
}

// execCatalog drives `catalog <args...>` through the full Cobra pipeline and
// returns captured stdout plus the RunE error. catalog is in the App-skip list,
// so no --nibs-path/--config is needed.
func execCatalog(t *testing.T, args ...string) (string, error) {
	t.Helper()
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	t.Cleanup(resetCatalogFlags)
	resetCatalogFlags()
	rootCmd.SetArgs(append([]string{"catalog"}, args...))
	var execErr error
	out := captureStdout(t, func() { execErr = rootCmd.Execute() })
	return out, execErr
}

// TestCatalogTopicsRunClean drives every advertised topic (text and JSON) and
// asserts each succeeds with non-empty output. It pins that the topic index
// (catalogTopics) never advertises a topic the dispatch cannot render.
func TestCatalogTopicsRunClean(t *testing.T) {
	for _, topic := range catalogTopicNames() {
		for _, jsonMode := range []bool{false, true} {
			name := topic
			if jsonMode {
				name += "/json"
			}
			t.Run(name, func(t *testing.T) {
				var args []string
				if jsonMode {
					args = append(args, "--json")
				}
				args = append(args, topic)
				out, err := execCatalog(t, args...)
				if err != nil {
					t.Fatalf("catalog %v: unexpected error: %v", args, err)
				}
				if strings.TrimSpace(out) == "" {
					t.Fatalf("catalog %v: empty output", args)
				}
			})
		}
	}
}

// TestCatalogIndex verifies the no-topic form lists every topic, in both modes.
func TestCatalogIndex(t *testing.T) {
	// Text mode: each topic name appears.
	out, err := execCatalog(t)
	if err != nil {
		t.Fatalf("catalog (index): %v", err)
	}
	for _, name := range catalogTopicNames() {
		if !strings.Contains(out, name) {
			t.Errorf("index text output missing topic %q", name)
		}
	}

	// JSON mode: topics decode and match the index in order.
	jsonOut, err := execCatalog(t, "--json")
	if err != nil {
		t.Fatalf("catalog --json (index): %v", err)
	}
	var got struct {
		Topics []catalogTopic `json:"topics"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &got); err != nil {
		t.Fatalf("decode index JSON: %v\n%s", err, jsonOut)
	}
	if !reflect.DeepEqual(got.Topics, catalogTopics) {
		t.Errorf("index topics = %+v, want %+v", got.Topics, catalogTopics)
	}
}

// TestCatalogFieldsMatchProjection pins that catalog fields is generated from
// projection.FieldCatalog — the same registry that drives `-f`/--fields — so it
// cannot drift from what the CLI actually projects.
func TestCatalogFieldsMatchProjection(t *testing.T) {
	out, err := execCatalog(t, "--json", "fields")
	if err != nil {
		t.Fatalf("catalog --json fields: %v", err)
	}
	var got struct {
		Fields []struct {
			Name    string `json:"name"`
			Kind    string `json:"kind"`
			JSONKey string `json:"json_key"`
		} `json:"fields"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode fields JSON: %v\n%s", err, out)
	}

	want := projection.FieldCatalog()
	if len(got.Fields) != len(want) {
		t.Fatalf("got %d fields, want %d", len(got.Fields), len(want))
	}
	for i, w := range want {
		g := got.Fields[i]
		if g.Name != string(w.Name) || g.Kind != w.Kind || g.JSONKey != w.JSONKey {
			t.Errorf("field[%d] = {%s,%s,%s}, want {%s,%s,%s}",
				i, g.Name, g.Kind, g.JSONKey, w.Name, w.Kind, w.JSONKey)
		}
	}
}

// TestCatalogFiltersMatchConfig pins that the filter enum values are generated
// from config (the single source of truth), not hand-transcribed.
func TestCatalogFiltersMatchConfig(t *testing.T) {
	out, err := execCatalog(t, "--json", "filters")
	if err != nil {
		t.Fatalf("catalog --json filters: %v", err)
	}
	var got struct {
		Filters []struct {
			Field  string   `json:"field"`
			Values []string `json:"values"`
		} `json:"filters"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode filters JSON: %v\n%s", err, out)
	}

	cfg := config.Default()
	want := map[string][]string{
		"status":   cfg.StatusNames(),
		"type":     cfg.TypeNames(),
		"priority": cfg.PriorityNames(),
		"estimate": cfg.EstimateNames(),
	}
	if len(got.Filters) != len(want) {
		t.Fatalf("got %d filters, want %d", len(got.Filters), len(want))
	}
	for _, f := range got.Filters {
		w, ok := want[f.Field]
		if !ok {
			t.Errorf("unexpected filter field %q", f.Field)
			continue
		}
		if !reflect.DeepEqual(f.Values, w) {
			t.Errorf("filter %q values = %v, want %v", f.Field, f.Values, w)
		}
	}
}

// TestCatalogFiltersShowStatusGroups pins that catalog filters documents the
// status groups (open/closed) with members derived from config, and discloses
// the open-by-default behavior — the vocabulary the open default depends on for
// discoverability. The retired third group must not come back: an extra group
// here would fail the length check below.
func TestCatalogFiltersShowStatusGroups(t *testing.T) {
	// JSON mode: status_groups decode with config-derived members, and
	// open_by_default is advertised.
	jsonOut, err := execCatalog(t, "--json", "filters")
	if err != nil {
		t.Fatalf("catalog --json filters: %v", err)
	}
	var got struct {
		StatusGroups []struct {
			Group   string   `json:"group"`
			Members []string `json:"members"`
		} `json:"status_groups"`
		OpenByDefault bool `json:"open_by_default"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &got); err != nil {
		t.Fatalf("decode filters JSON: %v\n%s", err, jsonOut)
	}
	if !got.OpenByDefault {
		t.Errorf("filters JSON open_by_default = false, want true")
	}
	cfg := config.Default()
	want := map[string][]string{
		"open":   cfg.OpenStatusNames(),
		"closed": cfg.ClosedStatusNames(),
	}
	if len(got.StatusGroups) != len(want) {
		t.Fatalf("got %d status groups, want %d", len(got.StatusGroups), len(want))
	}
	for _, g := range got.StatusGroups {
		w, ok := want[g.Group]
		if !ok {
			t.Errorf("unexpected status group %q", g.Group)
			continue
		}
		if !reflect.DeepEqual(g.Members, w) {
			t.Errorf("status group %q members = %v, want %v", g.Group, g.Members, w)
		}
	}

	// Text mode: the group names and the open-by-default disclosure appear.
	textOut, err := execCatalog(t, "filters")
	if err != nil {
		t.Fatalf("catalog filters: %v", err)
	}
	for _, want := range []string{
		"Status groups", "open", "closed",
		"open nibs by default", "--all",
		// The "closed but still blocks" subtlety is the one thing about the
		// closed group an agent cannot infer from the member list.
		"still blocks its dependents",
	} {
		if !strings.Contains(textOut, want) {
			t.Errorf("catalog filters text missing %q", want)
		}
	}
	// The retired vocabulary must not reappear in the text rendering either.
	for _, gone := range []string{retiredStatusGroup, "--active"} {
		if strings.Contains(textOut, gone) {
			t.Errorf("catalog filters text still mentions retired %q", gone)
		}
	}
}

// TestCatalogExamplesShowOpenWorkRecipes pins that catalog examples surfaces the
// open-work recipes (open work under a parent, only-closed via -s closed,
// everything via --all) so an agent finds them instead of hand-rolling a
// '--json | python' post-filter.
func TestCatalogExamplesShowOpenWorkRecipes(t *testing.T) {
	// JSON mode: recipes decode alongside the {nib} and list payloads.
	jsonOut, err := execCatalog(t, "--json", "examples")
	if err != nil {
		t.Fatalf("catalog --json examples: %v", err)
	}
	var got struct {
		Recipes []recipeInfo `json:"recipes"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &got); err != nil {
		t.Fatalf("decode examples JSON: %v\n%s", err, jsonOut)
	}
	if !reflect.DeepEqual(got.Recipes, catalogOpenWorkRecipes()) {
		t.Errorf("examples recipes = %+v, want %+v", got.Recipes, catalogOpenWorkRecipes())
	}

	// Text mode: the key open-work commands and the anti-post-filter note appear.
	textOut, err := execCatalog(t, "examples")
	if err != nil {
		t.Fatalf("catalog examples: %v", err)
	}
	for _, want := range []string{
		"Open-work recipes",
		"open by default",
		"nibs rel <id> --rel descendants",
		"nibs list -s closed",
		"nibs list --all",
	} {
		if !strings.Contains(textOut, want) {
			t.Errorf("catalog examples text missing %q", want)
		}
	}
}

// TestCatalogHierarchyMatchesNibtypes pins that catalog hierarchy is generated
// from nibtypes.ValidParentTypes / ValidChildTypes for every configured type.
func TestCatalogHierarchyMatchesNibtypes(t *testing.T) {
	out, err := execCatalog(t, "--json", "hierarchy")
	if err != nil {
		t.Fatalf("catalog --json hierarchy: %v", err)
	}
	var got struct {
		Types []struct {
			Type          string   `json:"type"`
			ValidParents  []string `json:"valid_parents"`
			ValidChildren []string `json:"valid_children"`
		} `json:"types"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode hierarchy JSON: %v\n%s", err, out)
	}

	types := config.Default().TypeNames()
	if len(got.Types) != len(types) {
		t.Fatalf("got %d types, want %d", len(got.Types), len(types))
	}
	for i, name := range types {
		g := got.Types[i]
		if g.Type != name {
			t.Errorf("type[%d] = %q, want %q", i, g.Type, name)
		}
		wantParents := normalizeEmpty(nibtypes.ValidParentTypes(name))
		if !reflect.DeepEqual(g.ValidParents, wantParents) {
			t.Errorf("type %q valid_parents = %v, want %v", name, g.ValidParents, wantParents)
		}
		wantChildren := normalizeEmpty(nibtypes.ValidChildTypes(name))
		if !reflect.DeepEqual(g.ValidChildren, wantChildren) {
			t.Errorf("type %q valid_children = %v, want %v", name, g.ValidChildren, wantChildren)
		}
	}
}

// TestCatalogHierarchyMatchesHierarchyError proves the acceptance requirement
// that catalog hierarchy is consistent with a real HIERARCHY error: the allowed
// parent set the *nibtypes.HierarchyError carries (the exact data
// output.ErrorHierarchy serializes as allowedParentTypes) equals the catalog's
// valid_parents for the same child type.
func TestCatalogHierarchyMatchesHierarchyError(t *testing.T) {
	out, err := execCatalog(t, "--json", "hierarchy")
	if err != nil {
		t.Fatalf("catalog --json hierarchy: %v", err)
	}
	var got struct {
		Types []struct {
			Type         string   `json:"type"`
			ValidParents []string `json:"valid_parents"`
		} `json:"types"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode hierarchy JSON: %v", err)
	}

	for _, ty := range got.Types {
		illegal := illegalParentFor(ty.Type, ty.ValidParents)
		verr := nibtypes.ValidateParentType(ty.Type, illegal)
		var he *nibtypes.HierarchyError
		if !errors.As(verr, &he) {
			t.Fatalf("ValidateParentType(%q,%q) did not return a HierarchyError: %v", ty.Type, illegal, verr)
		}
		wantAllowed := normalizeEmpty(he.Allowed)
		if !reflect.DeepEqual(ty.ValidParents, wantAllowed) {
			t.Errorf("type %q: catalog valid_parents = %v, HIERARCHY error allowed = %v",
				ty.Type, ty.ValidParents, wantAllowed)
		}
	}
}

// illegalParentFor returns a type name that is NOT a valid parent for child, so
// ValidateParentType is guaranteed to reject it.
func illegalParentFor(child string, validParents []string) string {
	valid := make(map[string]bool, len(validParents))
	for _, p := range validParents {
		valid[p] = true
	}
	for _, candidate := range config.Default().TypeNames() {
		if !valid[candidate] {
			return candidate
		}
	}
	return "no-such-type"
}

// TestCatalogExamplesShape verifies the examples payload is a real, decodable
// {nib} single-read object and a {nibs,count,truncated} envelope with count 2.
func TestCatalogExamplesShape(t *testing.T) {
	out, err := execCatalog(t, "--json", "examples")
	if err != nil {
		t.Fatalf("catalog --json examples: %v", err)
	}
	var got struct {
		Nib struct {
			Nib map[string]any `json:"nib"`
		} `json:"nib"`
		List struct {
			Nibs      []map[string]any `json:"nibs"`
			Count     int              `json:"count"`
			Truncated bool             `json:"truncated"`
		} `json:"list"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode examples JSON: %v\n%s", err, out)
	}

	// The single {nib} must carry the card-view keys.
	for _, key := range []string{"id", "title", "status", "type", "priority", "blocked_by"} {
		if _, ok := got.Nib.Nib[key]; !ok {
			t.Errorf("example {nib} missing key %q", key)
		}
	}
	if got.List.Count != 2 {
		t.Errorf("example list count = %d, want 2", got.List.Count)
	}
	if len(got.List.Nibs) != 2 {
		t.Errorf("example list nibs len = %d, want 2", len(got.List.Nibs))
	}
	if got.List.Truncated {
		t.Errorf("example list truncated = true, want false")
	}
}

// TestCatalogRecipesFromLiveCommands pins that recipe purposes are pulled from
// the live cobra Short strings and the list --ready flag usage, not transcribed.
func TestCatalogRecipesFromLiveCommands(t *testing.T) {
	out, err := execCatalog(t, "--json", "recipes")
	if err != nil {
		t.Fatalf("catalog --json recipes: %v", err)
	}
	var got struct {
		Recipes []recipeInfo `json:"recipes"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode recipes JSON: %v\n%s", err, out)
	}

	want := map[string]string{
		"nibs context":      commandShort("context"),
		"nibs plan <id>":    commandShort("plan"),
		"nibs roadmap":      commandShort("roadmap"),
		"nibs list --ready": flagUsage("list", "ready"),
	}
	if len(got.Recipes) != len(want) {
		t.Fatalf("got %d recipes, want %d", len(got.Recipes), len(want))
	}
	for _, r := range got.Recipes {
		w, ok := want[r.Command]
		if !ok {
			t.Errorf("unexpected recipe command %q", r.Command)
			continue
		}
		if r.Purpose != w {
			t.Errorf("recipe %q purpose = %q, want %q", r.Command, r.Purpose, w)
		}
		if strings.HasPrefix(r.Purpose, "(command not found") || strings.HasPrefix(r.Purpose, "(flag not found") {
			t.Errorf("recipe %q resolved to placeholder %q — a command/flag was renamed", r.Command, r.Purpose)
		}
	}
}

// TestCatalogReadyPurposeAgreesAcrossTopics pins that `catalog examples` and
// `catalog recipes` describe `nibs list --ready` with the same words. Both
// topics list the command, so an agent that reads one and an agent that reads
// the other must learn the same exclusion set. That set is a literal in
// cmd/list.go with no derivation behind it, so a second hand-written
// description of the flag has nothing holding it in step.
func TestCatalogReadyPurposeAgreesAcrossTopics(t *testing.T) {
	purposes := map[string]string{}
	for _, topic := range []string{"examples", "recipes"} {
		out, err := execCatalog(t, "--json", topic)
		if err != nil {
			t.Fatalf("catalog --json %s: %v", topic, err)
		}
		var got struct {
			Recipes []recipeInfo `json:"recipes"`
		}
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("decode %s JSON: %v\n%s", topic, err, out)
		}
		for _, r := range got.Recipes {
			if r.Command == "nibs list --ready" {
				purposes[topic] = r.Purpose
			}
		}
		if _, ok := purposes[topic]; !ok {
			t.Fatalf("catalog %s lists no 'nibs list --ready' recipe, so this guard compares nothing", topic)
		}
	}
	if purposes["examples"] != purposes["recipes"] {
		t.Errorf("catalog examples describes --ready as %q but catalog recipes says %q — the same flag, two answers",
			purposes["examples"], purposes["recipes"])
	}
}

// TestCatalogSchemaReusesGraphQL pins that catalog schema surfaces the exact
// SDL 'nibs graphql --schema' prints (GetGraphQLSchema).
func TestCatalogSchemaReusesGraphQL(t *testing.T) {
	out, err := execCatalog(t, "schema")
	if err != nil {
		t.Fatalf("catalog schema: %v", err)
	}
	if out != GetGraphQLSchema() {
		t.Errorf("catalog schema output does not match GetGraphQLSchema()")
	}
}

// TestCatalogUnknownTopic verifies an unknown topic is a VALIDATION error
// (exit 2) that names the valid topics, in both text and JSON modes.
func TestCatalogUnknownTopic(t *testing.T) {
	for _, jsonMode := range []bool{false, true} {
		mode := "text"
		if jsonMode {
			mode = "json"
		}
		t.Run(mode, func(t *testing.T) {
			var args []string
			if jsonMode {
				args = append(args, "--json")
			}
			args = append(args, "bogus")
			out, err := execCatalog(t, args...)
			if err == nil {
				t.Fatalf("catalog %v: expected error, got nil", args)
			}
			var ce *output.CodedError
			if !errors.As(err, &ce) {
				t.Fatalf("error is not a CodedError: %v", err)
			}
			if ce.Code != output.ErrValidation {
				t.Errorf("code = %q, want %q", ce.Code, output.ErrValidation)
			}
			if output.ExitCode(ce.Code) != output.ExitValidation {
				t.Errorf("exit code = %d, want %d", output.ExitCode(ce.Code), output.ExitValidation)
			}
			// The message must name every valid topic so the surface is
			// self-documenting.
			for _, name := range catalogTopicNames() {
				if !strings.Contains(ce.Msg, name) {
					t.Errorf("error message %q does not name topic %q", ce.Msg, name)
				}
			}
			// JSON mode reports to stdout; text mode leaves stdout clean.
			if jsonMode && !strings.Contains(out, output.ErrValidation) {
				t.Errorf("json error envelope missing on stdout: %q", out)
			}
		})
	}
}
