package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/fsutil"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/testdata/fixtures"
)

// setupAreaVerbTest copies the sample fixture — whose config declares auth, api,
// api/webhooks, web, web/dashboard, infra and docs, with seven nibs assigned
// across all of those but docs — and returns the store path. The whole command
// tree is reset rather than a named list of verbs: these rows run several
// commands per test.
func setupAreaVerbTest(t *testing.T) string {
	t.Helper()
	resetCommandTreeFlags(rootCmd)
	t.Cleanup(func() {
		resetCommandTreeFlags(rootCmd)
		rootCmd.SetArgs(nil)
	})
	return filepath.Join(fixtures.CopySampleProject(t), ".nibs")
}

// runArea runs one `nibs area …` invocation against nibsPath with every flag
// returned to its default first, so a row can run a refusal and then the remedy
// it names.
func runArea(t *testing.T, nibsPath string, args ...string) (string, error) {
	t.Helper()
	resetCommandTreeFlags(rootCmd)
	return runRootWith(t, append([]string{"--nibs-path", nibsPath, "area"}, args...)...)
}

// areaVocabulary reloads the store's declared vocabulary from its config.yml,
// so the assertions read the FILE rather than anything the command held.
func areaVocabulary(t *testing.T, nibsPath string) []string {
	t.Helper()
	cfg, err := config.LoadFromStore(nibsPath)
	if err != nil {
		t.Fatalf("the edited config no longer loads: %v", err)
	}
	return cfg.AreaPaths()
}

// storedAreas returns every nib id in the store paired with the `area:` its file
// carries, read straight off disk.
func storedAreas(t *testing.T, nibsPath string) map[string]string {
	t.Helper()
	areas := map[string]string{}
	for _, dir := range []string{"data", "archive"} {
		matches, err := filepath.Glob(filepath.Join(nibsPath, dir, "*.md"))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range matches {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			// data/{id}--{slug}.md, or data/{id}.md when a nib has no slug.
			id, _, _ := strings.Cut(strings.TrimSuffix(filepath.Base(path), ".md"), "--")
			for _, line := range strings.Split(string(raw), "\n") {
				if area, ok := strings.CutPrefix(line, "area: "); ok {
					areas[id] = area
					break
				}
			}
		}
	}
	return areas
}

// TestAreaListPrintsTheDeclaredTree is the listing verb's whole job: an agent
// has to be able to read the vocabulary to place work in it, so every declared
// node appears, nested, with the description that says what belongs there — and
// with the FULL path, which is the value `--area` takes.
func TestAreaListPrintsTheDeclaredTree(t *testing.T) {
	nibsPath := setupAreaVerbTest(t)

	out, err := runArea(t, nibsPath, "list")
	if err != nil {
		t.Fatalf("area list: %v\nout: %s", err, out)
	}
	for _, want := range []string{
		"auth", "api", "api/webhooks", "web", "web/dashboard", "infra", "docs",
		"Sign-in, sessions, tokens and account security",
		"Outbound webhook delivery and subscriptions",
		"The project dashboard and its charts",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("area list did not print %q:\n%s", want, out)
		}
	}

	// Declaration order, and a child indented under its parent.
	var order []string
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		path, _, _ := strings.Cut(trimmed, "  ")
		order = append(order, path)
		if strings.Contains(path, "/") && !strings.HasPrefix(line, " ") {
			t.Errorf("the nested area %q is not indented under its parent:\n%s", path, out)
		}
	}
	want := []string{"auth", "api", "api/webhooks", "web", "web/dashboard", "infra", "docs"}
	if !slices.Equal(order, want) {
		t.Errorf("area list printed %v, want the declaration order %v", order, want)
	}
}

// TestAreaListJSONCarriesTheTree pins the machine-readable shape, since the
// audience for this verb is an agent placing work.
func TestAreaListJSONCarriesTheTree(t *testing.T) {
	nibsPath := setupAreaVerbTest(t)

	out, err := runArea(t, nibsPath, "list", "--json")
	if err != nil {
		t.Fatalf("area list --json: %v\nout: %s", err, out)
	}
	var payload struct {
		Areas []struct {
			Path        string `json:"path"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Children    []struct {
				Path string `json:"path"`
			} `json:"children"`
		} `json:"areas"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}
	if len(payload.Areas) != 5 {
		t.Fatalf("got %d roots, want 5: %s", len(payload.Areas), out)
	}
	if payload.Areas[2].Path != "web" || len(payload.Areas[2].Children) != 1 ||
		payload.Areas[2].Children[0].Path != "web/dashboard" {
		t.Errorf("the nesting did not survive: %s", out)
	}
	if payload.Areas[0].Description == "" {
		t.Errorf("descriptions are missing: %s", out)
	}
}

// TestAreaListInAStoreDeclaringNoneSaysSo answers the question an empty listing
// otherwise leaves open — whether the vocabulary is empty or the verb is broken
// — and says where a vocabulary is declared. It is not a refusal: a project that
// has declared no areas is a normal project.
func TestAreaListInAStoreDeclaringNoneSaysSo(t *testing.T) {
	setupAreaVerbTest(t)
	nibsPath := remedyStoreWithoutAreas(nil)(t)

	out, err := runArea(t, nibsPath, "list")
	if err != nil {
		t.Fatalf("area list must not refuse an undeclared vocabulary: %v", err)
	}
	if !strings.Contains(out, "declares no areas") || !strings.Contains(out, "config.yml") {
		t.Errorf("area list said nothing useful about an empty vocabulary:\n%s", out)
	}
}

// TestAreaRenameCascadesTheWholeSubtree is the acceptance criterion: the node
// moves in config.yml and every nib assigned to it OR to any declared descendant
// follows, because renaming a parent moves the whole subtree's paths.
func TestAreaRenameCascadesTheWholeSubtree(t *testing.T) {
	nibsPath := setupAreaVerbTest(t)
	before := storedAreas(t, nibsPath)

	out, err := runArea(t, nibsPath, "rename", "web", "frontend")
	if err != nil {
		t.Fatalf("area rename: %v\nout: %s", err, out)
	}

	if got, want := areaVocabulary(t, nibsPath),
		[]string{"auth", "api", "api/webhooks", "frontend", "frontend/dashboard", "infra", "docs"}; !slices.Equal(got, want) {
		t.Errorf("vocabulary = %v, want %v", got, want)
	}
	after := storedAreas(t, nibsPath)
	for id, was := range before {
		switch was {
		case "web":
			if after[id] != "frontend" {
				t.Errorf("%s = %q, want frontend", id, after[id])
			}
		case "web/dashboard":
			if after[id] != "frontend/dashboard" {
				t.Errorf("%s = %q, want frontend/dashboard", id, after[id])
			}
		default:
			if after[id] != was {
				t.Errorf("%s moved from %q to %q; only the renamed subtree may change", id, was, after[id])
			}
		}
	}
	for id, area := range after {
		if area == "web" || area == "web/dashboard" {
			t.Errorf("%s still carries the old path %q", id, area)
		}
	}
	if !strings.Contains(out, "frontend") {
		t.Errorf("the summary does not say what happened:\n%s", out)
	}
}

// TestAreaRenameTouchesNothingElseInTheConfig is hazard #1 at the command level:
// a rename must edit the `name:` scalar and nothing else. Config.Save would
// write the MERGED read model — user config and system defaults layered onto the
// project's own values — and drop every key this build does not model, and the
// diff is the only assertion that catches all of that at once.
func TestAreaRenameTouchesNothingElseInTheConfig(t *testing.T) {
	nibsPath := setupAreaVerbTest(t)
	cfgPath := filepath.Join(nibsPath, "config.yml")
	// The shipped fixture's config is close enough to what Config.Save marshals
	// that a diff over it alone would not tell the two apart. A comment and a key
	// this build does not model are what a real project's committed config
	// carries and what saving the read model destroys, so the row plants both.
	seeded, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	seeded = append([]byte("# The vocabulary this project places work in.\n"), seeded...)
	seeded = append(seeded, []byte("future_key:\n    a_newer_nibs_wrote_this: true\n")...)
	if err := os.WriteFile(cfgPath, seeded, 0644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	if out, err := runArea(t, nibsPath, "rename", "web", "frontend"); err != nil {
		t.Fatalf("area rename: %v\nout: %s", err, out)
	}
	after, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	changed := changedLines(string(before), string(after))
	want := [][2]string{{"    - name: web", "    - name: frontend"}}
	if !slices.Equal(changed, want) {
		t.Errorf("the rename changed %v, want only %v", changed, want)
	}
}

// changedLines pairs the lines that differ between two files, positionally. It
// reports a length change as a single sentinel pair so a rewrite that adds or
// drops lines fails loudly rather than aligning past the difference.
func changedLines(before, after string) [][2]string {
	b := strings.Split(before, "\n")
	a := strings.Split(after, "\n")
	if len(b) != len(a) {
		return [][2]string{{"<" + itoa(len(b)) + " lines>", "<" + itoa(len(a)) + " lines>"}}
	}
	var changed [][2]string
	for i := range b {
		if b[i] != a[i] {
			changed = append(changed, [2]string{b[i], a[i]})
		}
	}
	return changed
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// TestAreaRenameRefusals covers every way the rename is refused, and asserts
// that a refused rename left both the vocabulary and the assignments alone.
func TestAreaRenameRefusals(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "a path the store does not declare",
			args: []string{"rename", "nosuch", "x"},
			want: []string{"nosuch", "declares no area"},
		},
		{
			name: "a name a sibling already holds",
			args: []string{"rename", "web", "auth"},
			want: []string{"already declares", "auth"},
		},
		{
			name: "a name a nested sibling already holds",
			args: []string{"rename", "api/webhooks", "webhooks"},
			want: []string{"already named"},
		},
		{
			name: "a path where a name belongs",
			args: []string{"rename", "web/dashboard", "web/panel"},
			want: []string{"not a name", "nibs area rename web/dashboard panel"},
		},
		{
			name: "a path under a different parent",
			args: []string{"rename", "web/dashboard", "api/panel"},
			want: []string{"not a name", "never moves it"},
		},
		{
			name: "an empty new name",
			args: []string{"rename", "web", ""},
			want: []string{"needs a name"},
		},
		{
			name: "a new name that is only whitespace padding",
			args: []string{"rename", "web", " frontend"},
			want: []string{"whitespace"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsPath := setupAreaVerbTest(t)
			vocabBefore := areaVocabulary(t, nibsPath)
			areasBefore := storedAreas(t, nibsPath)

			out, err := runArea(t, nibsPath, tt.args...)
			if err == nil {
				t.Fatalf("expected a refusal, got:\n%s", out)
			}
			if code := areaErrCode(t, err); code != output.ErrValidation {
				t.Errorf("code = %q (exit %d), want %q (exit 2)", code, output.ExitCode(code), output.ErrValidation)
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want substring %q", err.Error(), want)
				}
			}
			if got := areaVocabulary(t, nibsPath); !slices.Equal(got, vocabBefore) {
				t.Errorf("a refused rename edited the vocabulary: %v", got)
			}
			if got := storedAreas(t, nibsPath); !mapsEqual(got, areasBefore) {
				t.Errorf("a refused rename rewrote assignments: %v", got)
			}
		})
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// TestAreaRetireRefusesWhilePopulated is the acceptance criterion: retiring an
// area with work in it refuses, names the nibs, and names both dispositions.
func TestAreaRetireRefusesWhilePopulated(t *testing.T) {
	nibsPath := setupAreaVerbTest(t)

	out, err := runArea(t, nibsPath, "rm", "auth")
	if err == nil {
		t.Fatalf("expected a refusal, got:\n%s", out)
	}
	if code := areaErrCode(t, err); code != output.ErrValidation {
		t.Errorf("code = %q, want %q", code, output.ErrValidation)
	}
	for _, want := range []string{"auth", "tnib-b002", "tnib-f002", "--move-to", "--unassign"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want substring %q", err.Error(), want)
		}
	}
	if got := areaVocabulary(t, nibsPath); !slices.Contains(got, "auth") {
		t.Errorf("a refused retire removed the declaration: %v", got)
	}
}

// TestAreaRetireCountsTheWholeSubtree: the refusal is over everything assigned
// AT OR BELOW the node, since retiring it takes its declared children too.
func TestAreaRetireCountsTheWholeSubtree(t *testing.T) {
	nibsPath := setupAreaVerbTest(t)

	_, err := runArea(t, nibsPath, "rm", "web")
	if err == nil {
		t.Fatal("expected a refusal")
	}
	for _, want := range []string{"tnib-b005", "tnib-f008"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to name the descendant's member %q", err.Error(), want)
		}
	}
}

// TestAreaRetireUnassigns is the acceptance criterion for the clearing
// disposition: the members lose their assignment and the declaration goes.
func TestAreaRetireUnassigns(t *testing.T) {
	nibsPath := setupAreaVerbTest(t)

	out, err := runArea(t, nibsPath, "rm", "auth", "--unassign")
	if err != nil {
		t.Fatalf("area rm --unassign: %v\nout: %s", err, out)
	}
	if got := areaVocabulary(t, nibsPath); slices.Contains(got, "auth") {
		t.Errorf("auth is still declared: %v", got)
	}
	for id, area := range storedAreas(t, nibsPath) {
		if area == "auth" {
			t.Errorf("%s still carries area auth", id)
		}
	}
	for _, id := range []string{"tnib-b002", "tnib-f002"} {
		if got := areaOf(t, nibsPath, id); got != "" {
			t.Errorf("%s area = %q, want it cleared", id, got)
		}
	}
}

// TestAreaRetireMovesMembers is the other disposition: every member lands ON the
// named area, not under a path it does not declare.
func TestAreaRetireMovesMembers(t *testing.T) {
	nibsPath := setupAreaVerbTest(t)

	out, err := runArea(t, nibsPath, "rm", "web", "--move-to", "api")
	if err != nil {
		t.Fatalf("area rm --move-to: %v\nout: %s", err, out)
	}
	if got, want := areaVocabulary(t, nibsPath),
		[]string{"auth", "api", "api/webhooks", "infra", "docs"}; !slices.Equal(got, want) {
		t.Errorf("vocabulary = %v, want %v", got, want)
	}
	// tnib-f008 was assigned to web/dashboard: it lands on api, not api/dashboard.
	for _, id := range []string{"tnib-b005", "tnib-f008"} {
		if got := areaOf(t, nibsPath, id); got != "api" {
			t.Errorf("%s area = %q, want api", id, got)
		}
	}
}

// TestAreaRetireEmptyNeedsNoDisposition is the manual acceptance criterion:
// nothing is assigned, so nothing has to be disposed of and it just works.
func TestAreaRetireEmptyNeedsNoDisposition(t *testing.T) {
	nibsPath := setupAreaVerbTest(t)
	// infra holds exactly one nib in the fixture; clear it to empty the area.
	resetCommandTreeFlags(rootCmd)
	if _, err := runRootWith(t, "--nibs-path", nibsPath, "set", "tnib-t039", "--clear", "area"); err != nil {
		t.Fatalf("clearing the one member: %v", err)
	}

	out, err := runArea(t, nibsPath, "rm", "infra")
	if err != nil {
		t.Fatalf("retiring an empty area must need no disposition: %v\nout: %s", err, out)
	}
	if got := areaVocabulary(t, nibsPath); slices.Contains(got, "infra") {
		t.Errorf("infra is still declared: %v", got)
	}
}

// TestAreaRetireRefusesADispositionWithNothingToDispose keeps a silent no-op out
// of the report: obeying the flag here would say members were disposed of when
// none were, and that is the one answer a caller cannot tell from a real one.
func TestAreaRetireRefusesADispositionWithNothingToDispose(t *testing.T) {
	nibsPath := setupAreaVerbTest(t)
	resetCommandTreeFlags(rootCmd)
	if _, err := runRootWith(t, "--nibs-path", nibsPath, "set", "tnib-t039", "--clear", "area"); err != nil {
		t.Fatalf("clearing the one member: %v", err)
	}

	_, err := runArea(t, nibsPath, "rm", "infra", "--unassign")
	if err == nil {
		t.Fatal("expected a refusal for a disposition with nothing to dispose of")
	}
	for _, want := range []string{"nothing to", "nibs area rm infra"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want substring %q", err.Error(), want)
		}
	}
	if got := areaVocabulary(t, nibsPath); !slices.Contains(got, "infra") {
		t.Errorf("the refused retire removed the declaration anyway: %v", got)
	}
}

// TestAreaRetireRefusesATargetItIsAboutToRetire: moving members into the subtree
// being retired would leave them assigned to a path that is about to stop
// existing, which is the state this whole verb exists to prevent.
func TestAreaRetireRefusesATargetItIsAboutToRetire(t *testing.T) {
	for _, target := range []string{"web", "web/dashboard"} {
		t.Run(target, func(t *testing.T) {
			nibsPath := setupAreaVerbTest(t)
			_, err := runArea(t, nibsPath, "rm", "web", "--move-to", target)
			if err == nil {
				t.Fatal("expected a refusal")
			}
			if !strings.Contains(err.Error(), "retiring") {
				t.Errorf("error = %q, want it to say the target is inside the retiring subtree", err.Error())
			}
			if got := areaVocabulary(t, nibsPath); !slices.Contains(got, "web") {
				t.Errorf("the refused retire removed the declaration: %v", got)
			}
		})
	}
}

// TestAreaRetireRefusesAnUndeclaredTarget: an area nobody declared is not
// somewhere work can be moved to.
func TestAreaRetireRefusesAnUndeclaredTarget(t *testing.T) {
	nibsPath := setupAreaVerbTest(t)

	_, err := runArea(t, nibsPath, "rm", "auth", "--move-to", "nosuch")
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if !strings.Contains(err.Error(), "nosuch") || !strings.Contains(err.Error(), "declares no area") {
		t.Errorf("error = %q, want it to name the undeclared target", err.Error())
	}
	for _, id := range []string{"tnib-b002", "tnib-f002"} {
		if got := areaOf(t, nibsPath, id); got != "auth" {
			t.Errorf("%s was rewritten by a refused retire: %q", id, got)
		}
	}
}

// TestAreaRetireRefusesBothDispositionsAtOnce: naming both is a
// last-writer-wins ambiguity, refused the way close refuses its pair.
func TestAreaRetireRefusesBothDispositionsAtOnce(t *testing.T) {
	nibsPath := setupAreaVerbTest(t)

	_, err := runArea(t, nibsPath, "rm", "auth", "--move-to", "api", "--unassign")
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if !strings.Contains(err.Error(), "move-to") || !strings.Contains(err.Error(), "unassign") {
		t.Errorf("error = %q, want it to name both flags", err.Error())
	}
}

// TestAreaRetireRefusesAnUndeclaredPath keeps `rm` from silently succeeding on a
// path nobody declared.
func TestAreaRetireRefusesAnUndeclaredPath(t *testing.T) {
	nibsPath := setupAreaVerbTest(t)

	_, err := runArea(t, nibsPath, "rm", "nosuch")
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if !strings.Contains(err.Error(), "declares no area") {
		t.Errorf("error = %q, want the undeclared-path refusal", err.Error())
	}
}

// TestAreaVerbsRefuseAStoreDeclaringNoAreas: both mutations name a node, and a
// store with no vocabulary has none to name — so the answer is the config edit,
// not a different argument.
func TestAreaVerbsRefuseAStoreDeclaringNoAreas(t *testing.T) {
	setupAreaVerbTest(t)
	nibsPath := remedyStoreWithoutAreas(nil)(t)

	for _, args := range [][]string{{"rename", "web", "frontend"}, {"rm", "web"}} {
		t.Run(args[0], func(t *testing.T) {
			_, err := runArea(t, nibsPath, args...)
			if err == nil {
				t.Fatal("expected a refusal")
			}
			if !strings.Contains(err.Error(), "declares no areas") {
				t.Errorf("error = %q, want it to say the store declares none", err.Error())
			}
			if strings.Contains(err.Error(), "must be one of") {
				t.Errorf("error = %q, must not name an empty allowed set", err.Error())
			}
		})
	}
}

// TestAreaRenameLeavesAStrandedAssignmentAlone: a nib carrying a value the
// vocabulary does not declare is not a member of anything, so no cascade sweeps
// it up — the closure is over the DECLARED tree, not over the strings.
func TestAreaRenameLeavesAStrandedAssignmentAlone(t *testing.T) {
	nibsPath := setupAreaVerbTest(t)
	rewriteStoredArea(t, nibsPath, "tnib-b005", "web", "web/retired")

	if out, err := runArea(t, nibsPath, "rename", "web", "frontend"); err != nil {
		t.Fatalf("area rename: %v\nout: %s", err, out)
	}
	if got := storedAreas(t, nibsPath)["tnib-b005"]; got != "web/retired" {
		t.Errorf("the stranded value was rewritten to %q; only declared members cascade", got)
	}
}

// TestAreaVerbsAreDeterministicallyOrdered pins the id order the partial-failure
// message rests on: the cascade names the nib that refused, and a set walked in
// map order would name a different one on every run.
func TestAreaVerbsAreDeterministicallyOrdered(t *testing.T) {
	nibsPath := setupAreaVerbTest(t)

	out, err := runArea(t, nibsPath, "rm", "auth", "--unassign")
	if err != nil {
		t.Fatalf("area rm --unassign: %v", err)
	}
	ids := []string{"tnib-b002", "tnib-f002"}
	sort.Strings(ids)
	first := strings.Index(out, ids[0])
	second := strings.Index(out, ids[1])
	if first < 0 || second < 0 {
		t.Fatalf("the summary does not name the members it rewrote:\n%s", out)
	}
	if first > second {
		t.Errorf("the summary lists members out of id order:\n%s", out)
	}
}

// failRenameOnto seeds the atomic-write seam so any rename whose TARGET path
// contains marker fails, which is how a bulk write is made to abort part way
// without an unwritable filesystem. It returns a function that lifts the seed.
func failRenameOnto(t *testing.T, marker string) func() {
	t.Helper()
	orig := fsutil.RenameFn
	armed := true
	fsutil.RenameFn = func(oldpath, newpath string) error {
		if armed && strings.Contains(newpath, marker) {
			return errors.New("simulated persistence failure")
		}
		return orig(oldpath, newpath)
	}
	t.Cleanup(func() { fsutil.RenameFn = orig })
	return func() { armed = false }
}

// TestAreaRenamePartialCascadeIsRerunnable executes the claim the partial-failure
// message makes, rather than trusting it: the writes already made stay, the
// declaration is untouched, and rerunning the SAME command finishes the job.
//
// It holds because the cascade writes the nibs BEFORE the declaration. A nib
// already rewritten carries a path that is not within the old node, so it is no
// longer a member and the rerun picks up exactly where this stopped.
func TestAreaRenamePartialCascadeIsRerunnable(t *testing.T) {
	nibsPath := setupAreaVerbTest(t)
	// web's members in id order are tnib-b005 then tnib-f008; failing on the
	// second leaves a genuinely partial run.
	disarm := failRenameOnto(t, "tnib-f008")

	_, err := runArea(t, nibsPath, "rename", "web", "frontend")
	if err == nil {
		t.Fatal("expected the seeded write failure to surface")
	}
	for _, want := range []string{"tnib-f008", "rerun the same command", "persisted"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want substring %q", err.Error(), want)
		}
	}
	if got := areaVocabulary(t, nibsPath); !slices.Contains(got, "web") {
		t.Errorf("the declaration was changed despite the failed cascade: %v", got)
	}
	areas := storedAreas(t, nibsPath)
	if areas["tnib-b005"] != "frontend" {
		t.Errorf("the write made before the failure was not persisted: %q", areas["tnib-b005"])
	}
	if areas["tnib-f008"] != "web/dashboard" {
		t.Errorf("tnib-f008 = %q, want it untouched by the failed write", areas["tnib-f008"])
	}

	disarm()
	if out, err := runArea(t, nibsPath, "rename", "web", "frontend"); err != nil {
		t.Fatalf("the rerun the message prescribes must finish the job: %v\nout: %s", err, out)
	}
	if got, want := areaVocabulary(t, nibsPath),
		[]string{"auth", "api", "api/webhooks", "frontend", "frontend/dashboard", "infra", "docs"}; !slices.Equal(got, want) {
		t.Errorf("vocabulary = %v, want %v", got, want)
	}
	areas = storedAreas(t, nibsPath)
	if areas["tnib-b005"] != "frontend" || areas["tnib-f008"] != "frontend/dashboard" {
		t.Errorf("after the rerun: tnib-b005=%q tnib-f008=%q", areas["tnib-b005"], areas["tnib-f008"])
	}
}

// TestAreaRenameConfigWriteFailureIsRerunnable is the other half of the same
// claim, at the other end of the run: every member is rewritten and the config
// write is what fails. Rerunning the same command still finishes, because the
// cascade then finds no members and only the declaration is left to rename.
func TestAreaRenameConfigWriteFailureIsRerunnable(t *testing.T) {
	nibsPath := setupAreaVerbTest(t)
	disarm := failRenameOnto(t, "config.yml")

	_, err := runArea(t, nibsPath, "rename", "web", "frontend")
	if err == nil {
		t.Fatal("expected the seeded config write failure to surface")
	}
	for _, want := range []string{"config.yml", "rerun the same command", "persisted"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want substring %q", err.Error(), want)
		}
	}
	areas := storedAreas(t, nibsPath)
	if areas["tnib-b005"] != "frontend" || areas["tnib-f008"] != "frontend/dashboard" {
		t.Fatalf("the cascade did not complete: tnib-b005=%q tnib-f008=%q", areas["tnib-b005"], areas["tnib-f008"])
	}

	disarm()
	if out, err := runArea(t, nibsPath, "rename", "web", "frontend"); err != nil {
		t.Fatalf("the rerun the message prescribes must finish the job: %v\nout: %s", err, out)
	}
	if got, want := areaVocabulary(t, nibsPath),
		[]string{"auth", "api", "api/webhooks", "frontend", "frontend/dashboard", "infra", "docs"}; !slices.Equal(got, want) {
		t.Errorf("vocabulary = %v, want %v", got, want)
	}
}

// TestAreaRetireConfigWriteFailureTellsTheCallerToDropTheFlag executes both
// halves of what that message says. A retire whose disposition completed and
// whose config write did not is the one state where rerunning the SAME command
// cannot work — the member set is empty now, and a disposition with nothing to
// dispose of is refused — so the message prescribes dropping the flag, and that
// is what has to finish the job.
func TestAreaRetireConfigWriteFailureTellsTheCallerToDropTheFlag(t *testing.T) {
	nibsPath := setupAreaVerbTest(t)
	disarm := failRenameOnto(t, "config.yml")

	_, err := runArea(t, nibsPath, "rm", "auth", "--unassign")
	if err == nil {
		t.Fatal("expected the seeded config write failure to surface")
	}
	if !strings.Contains(err.Error(), "rerun WITHOUT --unassign") {
		t.Errorf("error = %q, want it to prescribe dropping the flag", err.Error())
	}
	for _, id := range []string{"tnib-b002", "tnib-f002"} {
		if got := storedAreas(t, nibsPath)[id]; got != "" {
			t.Fatalf("the disposition did not complete: %s = %q", id, got)
		}
	}

	disarm()
	if _, err := runArea(t, nibsPath, "rm", "auth", "--unassign"); err == nil {
		t.Error("rerunning WITH the flag must be refused for an empty member set, which is why the message says WITHOUT")
	}
	if out, err := runArea(t, nibsPath, "rm", "auth"); err != nil {
		t.Fatalf("the rerun the message prescribes must finish the job: %v\nout: %s", err, out)
	}
	if got := areaVocabulary(t, nibsPath); slices.Contains(got, "auth") {
		t.Errorf("auth is still declared: %v", got)
	}
}

// TestAreaEditsHoldTheStoreLockAcrossTheConfigWrite is finding #1 as a test.
//
// The cascade and the `areas:` rewrite are two halves of one edit, and the
// config half is a read-modify-write of the whole file. If the lock is dropped
// between them — or never taken for the config write — two concurrent area
// edits interleave and the loser's declaration is gone while its cascade is on
// disk, both processes exiting 0. The probe therefore asks the question at the
// only moment that settles it: while the config file is being renamed into
// place, can anything else take the store's write lock?
func TestAreaEditsHoldTheStoreLockAcrossTheConfigWrite(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "rename", args: []string{"rename", "web", "frontend"}},
		{name: "retire", args: []string{"rm", "auth", "--unassign"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsPath := setupAreaVerbTest(t)
			var probed, acquired bool

			orig := fsutil.RenameFn
			fsutil.RenameFn = func(oldpath, newpath string) error {
				if strings.Contains(newpath, "config.yml") && !probed {
					probed = true
					acquired = storeLockIsFree(t, nibsPath)
				}
				return orig(oldpath, newpath)
			}
			t.Cleanup(func() { fsutil.RenameFn = orig })

			if out, err := runArea(t, nibsPath, tt.args...); err != nil {
				t.Fatalf("area %v: %v\nout: %s", tt.args, err, out)
			}
			if !probed {
				t.Fatal("the config write never happened, so the probe proves nothing")
			}
			if acquired {
				t.Error("the store's write lock was free while config.yml was being written — " +
					"a concurrent area edit can read the pre-edit config here and write it back over this one")
			}
		})
	}
}

// storeLockIsFree reports whether the store's cross-process write lock can be
// taken right now. It answers from another goroutine because the acquisition
// BLOCKS while the lock is held, and the caller is the goroutine holding it.
//
// The window is one-sided on purpose: an unheld flock is granted immediately —
// measured worst case 16µs over 2000 uncontended tries on this machine — so the
// wait below is four orders of magnitude of headroom, and is only ever spent in
// full when the lock really is held.
func storeLockIsFree(t *testing.T, nibsPath string) bool {
	t.Helper()
	got := make(chan *nibcore.StoreLock, 1)
	go func() {
		lock, err := nibcore.AcquireStoreLock(nibsPath)
		if err != nil {
			got <- nil
			return
		}
		got <- lock
	}()
	select {
	case lock := <-got:
		if lock == nil {
			return false
		}
		_ = lock.Release()
		return true
	case <-time.After(200 * time.Millisecond):
		// Still blocked, which is the answer. The goroutine acquires the lock
		// once the command releases it and leaks until the test binary exits;
		// draining it here would deadlock, since the command cannot finish while
		// this call has not returned.
		go func() {
			if lock := <-got; lock != nil {
				_ = lock.Release()
			}
		}()
		return false
	}
}

// TestAreaVerbsRefuseAVocabularyTheyCannotAddress is finding #2 at the command
// level. A node declared through a YAML alias or a merge key resolves for the
// loaded model and is invisible to the file editor, so the config write can
// never succeed — and it must therefore be refused BEFORE the cascade, with the
// store left exactly as it was. Discovered afterwards it was permanent: the
// members carried an undeclared path, every write to them was refused, and the
// printed "rerun the same command" failed identically forever.
func TestAreaVerbsRefuseAVocabularyTheyCannotAddress(t *testing.T) {
	const aliased = `nibs:
    prefix: tnib-
    id_length: 4
shared:
    dashboard: &dashboard
        name: dashboard
        description: The project dashboard and its charts
areas:
    - name: auth
      description: Sign-in
    - name: web
      description: The browser client
      children:
        - *dashboard
`
	const merged = `nibs:
    prefix: tnib-
    id_length: 4
shared:
    dashboard: &dashboard
        name: dashboard
        description: The project dashboard and its charts
areas:
    - name: auth
      description: Sign-in
    - name: web
      description: The browser client
      children:
        - <<: *dashboard
`
	const twoDocs = `nibs:
    prefix: tnib-
    id_length: 4
areas:
    - name: auth
      description: Sign-in
    - name: web
      description: The browser client
      children:
        - name: dashboard
          description: The project dashboard and its charts
---
# a second document some other tool appends
extra: true
`
	tests := []struct {
		name    string
		config  string
		args    []string
		wantMsg string
	}{
		{"rename a node declared through an alias", aliased, []string{"rename", "web/dashboard", "panel"}, "alias"},
		{"retire a node declared through an alias", aliased, []string{"rm", "web/dashboard", "--unassign"}, "alias"},
		{"rename a node named by a merge key", merged, []string{"rename", "web/dashboard", "panel"}, "merge key"},
		{"rename in a multi-document config", twoDocs, []string{"rename", "web", "frontend"}, "more than one YAML document"},
		{"retire in a multi-document config", twoDocs, []string{"rm", "web", "--unassign"}, "more than one YAML document"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsPath := setupAreaVerbTest(t)
			cfgPath := filepath.Join(nibsPath, "config.yml")
			if err := os.WriteFile(cfgPath, []byte(tt.config), 0644); err != nil {
				t.Fatal(err)
			}
			areasBefore := storedAreas(t, nibsPath)

			out, err := runArea(t, nibsPath, tt.args...)
			if err == nil {
				t.Fatalf("expected a refusal, got:\n%s", out)
			}
			if code := areaErrCode(t, err); code != output.ErrValidation {
				t.Errorf("code = %q (exit %d), want %q (exit 2)", code, output.ExitCode(code), output.ErrValidation)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want it to name the shape (%q)", err.Error(), tt.wantMsg)
			}
			// Nothing at all may have moved: the whole point of refusing up
			// front is that there is no half-applied state to repair.
			after, readErr := os.ReadFile(cfgPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(after) != tt.config {
				t.Errorf("the refused edit rewrote config.yml:\n%s", after)
			}
			if got := storedAreas(t, nibsPath); !mapsEqual(got, areasBefore) {
				t.Errorf("the refused edit rewrote assignments: %v, want %v", got, areasBefore)
			}
		})
	}
}

// TestAreaRetireWriteFailureWithNoDispositionSaysWhatHappened is finding #5.
// Retiring an EMPTY area needs no disposition, so a config write that fails
// there must not report one as having run, nor tell the caller to drop a flag
// they never passed.
func TestAreaRetireWriteFailureWithNoDispositionSaysWhatHappened(t *testing.T) {
	nibsPath := setupAreaVerbTest(t)
	// infra's one member, cleared, so the retire needs no disposition at all.
	if _, err := runRootWith(t, "--nibs-path", nibsPath, "set", "tnib-t039", "--clear", "area"); err != nil {
		t.Fatalf("clearing infra's member: %v", err)
	}
	disarm := failRenameOnto(t, "config.yml")
	defer disarm()

	_, err := runArea(t, nibsPath, "rm", "infra")
	if err == nil {
		t.Fatal("expected the seeded config write failure to surface")
	}
	for _, unwanted := range []string{"--unassign", "--move-to", "unassigned", "reassigned"} {
		if strings.Contains(err.Error(), unwanted) {
			t.Errorf("error = %q, want no mention of %q — no disposition was named and none ran", err.Error(), unwanted)
		}
	}
	if !strings.Contains(err.Error(), "nibs area rm infra") {
		t.Errorf("error = %q, want it to prescribe the rerun that finishes the job", err.Error())
	}
	// And the sentence it does print has to be true: nothing was rewritten.
	if got := storedAreas(t, nibsPath)["tnib-t039"]; got != "" {
		t.Errorf("tnib-t039 = %q, want it still unassigned", got)
	}
}

// TestAreaRmHelpAgreesWithTheRerunItPrescribes keeps finding #4 from coming
// back: the long help used to promise that rerunning the SAME command finishes
// a partial run, which TestAreaRetireConfigWriteFailureTellsTheCallerToDropTheFlag
// asserts is false for a run whose disposition completed. Help is what a caller
// reads first, so it has to carry the same qualification the error does.
func TestAreaRmHelpAgreesWithTheRerunItPrescribes(t *testing.T) {
	long := areaRmCmd.Long
	if !strings.Contains(long, "WITHOUT the\ndisposition flag") {
		t.Errorf("`area rm` long help does not name the without-the-flag rerun the error message prescribes:\n%s", long)
	}
	if strings.Contains(long, "rerunning the\nsame command finishes a run that failed part way") {
		t.Errorf("`area rm` long help still makes the unqualified rerun claim its own test refutes:\n%s", long)
	}
}

// TestAreaEditNamesALiveServe is finding #6. The verbs write the vocabulary to
// the file and never into the loaded config, which keeps Core.ValidateArea's
// off-lock read sound — but a serve that is already running keeps the vocabulary
// it read at startup while its watcher picks the rewritten nibs up, so every
// write it makes to one of them is refused until it restarts. The success line
// has to say so, and must not say so when nothing is holding the store.
func TestAreaEditNamesALiveServe(t *testing.T) {
	const note = "restart it"

	t.Run("with no other process holding the store", func(t *testing.T) {
		nibsPath := setupAreaVerbTest(t)
		out, err := runArea(t, nibsPath, "rename", "web", "frontend")
		if err != nil {
			t.Fatalf("area rename: %v", err)
		}
		if strings.Contains(out, note) {
			t.Errorf("output claims a process is holding the store when none is:\n%s", out)
		}
	})

	t.Run("with a serve holding the store", func(t *testing.T) {
		nibsPath := setupAreaVerbTest(t)
		// The shared side is what `nibs serve` holds for its whole lifetime.
		serving, err := nibcore.AcquireServeLock(nibsPath)
		if err != nil {
			t.Fatalf("AcquireServeLock: %v", err)
		}
		defer func() { _ = serving.Release() }()

		out, err := runArea(t, nibsPath, "rename", "web", "frontend")
		if err != nil {
			t.Fatalf("area rename: %v", err)
		}
		if !strings.Contains(out, note) {
			t.Errorf("output does not name the live serve whose writes this edit just started refusing:\n%s", out)
		}
	})
}
