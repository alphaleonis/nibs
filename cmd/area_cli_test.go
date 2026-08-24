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

	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/testdata/fixtures"
)

// setupAreaCLITest copies the sample fixture — whose config declares auth, api,
// api/webhooks, web, web/dashboard and infra — and registers the flag resets the
// commands under test need. Returns the store path.
func setupAreaCLITest(t *testing.T) string {
	t.Helper()
	resetSetFlags()
	resetNewFlags()
	resetGetFlags()
	resetListFlags()
	resetQueryFlags()
	resetRootPersistentFlags()
	t.Cleanup(func() {
		resetSetFlags()
		resetNewFlags()
		resetGetFlags()
		resetListFlags()
		resetQueryFlags()
		resetRootPersistentFlags()
		rootCmd.SetArgs(nil)
	})
	return filepath.Join(fixtures.CopySampleProject(t), ".nibs")
}

func areaErrCode(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	var ce *output.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("error = %T, want *output.CodedError: %v", err, err)
	}
	return ce.Code
}

// areaOf reads a nib's stored area back through the projection, so the
// assertions read what a user would see.
func areaOf(t *testing.T, nibsPath, id string) string {
	t.Helper()
	_, _, area := axisFields(t, nibsPath, id)
	return area
}

// TestSetAreaAssignsAndClears pins `nibs set --area` end to end against a store
// that declares a vocabulary: the assignment lands, --clear area takes it away,
// and an empty value clears it the way --milestone "" does.
func TestSetAreaAssignsAndClears(t *testing.T) {
	nibsPath := setupAreaCLITest(t)

	if _, err := runRootWith(t, "--nibs-path", nibsPath, "set", "tnib-t031", "--area", "web/dashboard"); err != nil {
		t.Fatalf("set --area: %v", err)
	}
	if got := areaOf(t, nibsPath, "tnib-t031"); got != "web/dashboard" {
		t.Fatalf("area = %q, want web/dashboard", got)
	}

	resetSetFlags()
	if _, err := runRootWith(t, "--nibs-path", nibsPath, "set", "tnib-t031", "--clear", "area"); err != nil {
		t.Fatalf("set --clear area: %v", err)
	}
	if got := areaOf(t, nibsPath, "tnib-t031"); got != "" {
		t.Fatalf("area = %q, want it cleared", got)
	}

	resetSetFlags()
	if _, err := runRootWith(t, "--nibs-path", nibsPath, "set", "tnib-t031", "--area", "auth"); err != nil {
		t.Fatalf("set --area auth: %v", err)
	}
	resetSetFlags()
	if _, err := runRootWith(t, "--nibs-path", nibsPath, "set", "tnib-t031", "--area", ""); err != nil {
		t.Fatalf(`set --area "": %v`, err)
	}
	if got := areaOf(t, nibsPath, "tnib-t031"); got != "" {
		t.Fatalf(`area = %q, want --area "" to clear it`, got)
	}
}

// TestSetAreaRefusesUndeclaredValue pins the refusal a caller has to be able to
// act on: the exit class every other invalid vocabulary value uses, the offending
// value, and the declared set.
func TestSetAreaRefusesUndeclaredValue(t *testing.T) {
	nibsPath := setupAreaCLITest(t)

	_, err := runRootWith(t, "--nibs-path", nibsPath, "set", "tnib-t031", "--area", "nosuch")
	if code := areaErrCode(t, err); code != output.ErrValidation {
		t.Errorf("code = %q, want %q", code, output.ErrValidation)
	}
	if output.ExitCode(output.ErrValidation) != 2 {
		t.Errorf("VALIDATION_ERROR exit = %d, want 2", output.ExitCode(output.ErrValidation))
	}
	for _, want := range []string{"nosuch", "web/dashboard", "api/webhooks"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want substring %q", err.Error(), want)
		}
	}
	if got := areaOf(t, nibsPath, "tnib-t031"); got != "" {
		t.Errorf("refused set persisted area %q", got)
	}
}

// TestSetAreaRejectsSetAndClearTogether keeps --area in the clearable family:
// naming both is a last-writer-wins ambiguity every other clearable field
// already refuses.
func TestSetAreaRejectsSetAndClearTogether(t *testing.T) {
	nibsPath := setupAreaCLITest(t)

	_, err := runRootWith(t, "--nibs-path", nibsPath, "set", "tnib-t031", "--area", "auth", "--clear", "area")
	if code := areaErrCode(t, err); code != output.ErrValidation {
		t.Errorf("code = %q, want %q", code, output.ErrValidation)
	}
	if !strings.Contains(err.Error(), "cannot both set and --clear area") {
		t.Errorf("error = %q, want the set-and-clear refusal", err.Error())
	}
}

// TestSetAreaAloneIsAChange keeps `--area` out of the no-op refusal: it is the
// only flag some invocations carry.
func TestSetAreaAloneIsAChange(t *testing.T) {
	nibsPath := setupAreaCLITest(t)

	out, err := runRootWith(t, "--nibs-path", nibsPath, "set", "tnib-t031", "--area", "infra")
	if err != nil {
		t.Fatalf("set --area alone: %v\nout: %s", err, out)
	}
	if got := areaOf(t, nibsPath, "tnib-t031"); got != "infra" {
		t.Errorf("area = %q, want infra", got)
	}
}

// TestNewAreaPlacesWorkAtCreation pins `nibs new --area`, including the refusal,
// which must be the same validation class the sibling vocabulary flags report
// rather than the create path's file-error fallback.
func TestNewAreaPlacesWorkAtCreation(t *testing.T) {
	nibsPath := setupAreaCLITest(t)

	out, err := runRootWith(t, "--nibs-path", nibsPath, "new", "Placed work", "--area", "api/webhooks", "--json")
	if err != nil {
		t.Fatalf("new --area: %v\nout: %s", err, out)
	}
	var created struct {
		Nib struct {
			ID string `json:"id"`
		} `json:"nib"`
	}
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatalf("unmarshal new output: %v\nraw: %s", err, out)
	}
	// The card view carries neither axis field, so the area is read back through
	// the projection the way `nibs get -f area` reports it.
	if got := areaOf(t, nibsPath, created.Nib.ID); got != "api/webhooks" {
		t.Errorf("created nib area = %q, want api/webhooks", got)
	}

	resetNewFlags()
	resetRootPersistentFlags()
	_, err = runRootWith(t, "--nibs-path", nibsPath, "new", "Misplaced work", "--area", "nosuch")
	if code := areaErrCode(t, err); code != output.ErrValidation {
		t.Errorf("code = %q, want %q", code, output.ErrValidation)
	}
	for _, want := range []string{"nosuch", "web/dashboard"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want substring %q", err.Error(), want)
		}
	}
}

// TestNewAreaAnswersTheAxisRuleFirst pins the guard ORDERING on the create path
// against the one every other site agrees on: enums, then the axis rule, then
// the area vocabulary. A milestone takes no area at all, so answering an
// undeclared area on one with "must be one of …" prescribes a remedy the subject
// cannot follow — and callers stop at the first error.
//
// The refusal must also carry the validation class whatever half fires, because
// create's error fallback is FILE_ERROR (exit 5) and a bad argument pair is not
// a filesystem problem.
func TestNewAreaAnswersTheAxisRuleFirst(t *testing.T) {
	cases := []struct {
		name   string
		area   string
		want   string
		unwant string
	}{
		{
			name:   "undeclared area on a milestone",
			area:   "nosuch",
			want:   "a milestone cannot have an area",
			unwant: "must be one of",
		},
		{
			name:   "declared area on a milestone",
			area:   "auth",
			want:   "a milestone cannot have an area",
			unwant: "must be one of",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nibsPath := setupAreaCLITest(t)

			_, err := runRootWith(t, "--nibs-path", nibsPath, "new", "Waypoint", "--type", "milestone", "--area", tc.area)
			if code := areaErrCode(t, err); code != output.ErrValidation {
				t.Errorf("code = %q (exit %d), want %q (exit %d): %v",
					code, output.ExitCode(code), output.ErrValidation, output.ExitCode(output.ErrValidation), err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want the axis rule to answer first (%q)", err.Error(), tc.want)
			}
			if strings.Contains(err.Error(), tc.unwant) {
				t.Errorf("error = %q, must not prescribe %q — no area value satisfies the axis rule", err.Error(), tc.unwant)
			}
		})
	}
}

// TestAreaReadToleranceSurvives is the manual acceptance criterion as a test: a
// nib whose file already carries an undeclared area still loads and lists. The
// write path is the only refusal this feature adds.
func TestAreaReadToleranceSurvives(t *testing.T) {
	nibsPath := setupAreaCLITest(t)

	if _, err := runRootWith(t, "--nibs-path", nibsPath, "set", "tnib-t031", "--area", "auth"); err != nil {
		t.Fatalf("set --area auth: %v", err)
	}
	rewriteStoredArea(t, nibsPath, "tnib-t031", "auth", "retired/thing")

	resetListFlags()
	resetRootPersistentFlags()
	out, err := runRootWith(t, "--nibs-path", nibsPath, "list", "--all", "-q")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "tnib-t031") {
		t.Errorf("a nib carrying an undeclared area dropped out of list:\n%s", out)
	}
	if got := areaOf(t, nibsPath, "tnib-t031"); got != "retired/thing" {
		t.Errorf("area = %q, want it read back as written", got)
	}
}

// TestUndeclaredStoredAreaSaysWhoseValueItIs: retiring or renaming an `areas:`
// entry turns every nib that carried it into a write dead end, because a write
// re-checks the `area:` the nib already holds. The refusal a caller then meets
// has to say that the offending value is the NIB's and not an argument, and name
// the way out — a caller who passed no --area at all is otherwise told to
// correct something they never wrote, about a nib the message does not connect
// the value to.
//
// The create half of that split is asserted where it is reachable — the CLI
// pre-check in new.go answers a supplied --area before Core.Create ever sees it
// (TestCreateAreaRefusalNamesTheArgumentOnly, internal/nibcore).
func TestUndeclaredStoredAreaSaysWhoseValueItIs(t *testing.T) {
	nibsPath := setupAreaCLITest(t)

	if _, err := runRootWith(t, "--nibs-path", nibsPath, "set", "tnib-t031", "--area", "auth"); err != nil {
		t.Fatalf("set --area auth: %v", err)
	}
	rewriteStoredArea(t, nibsPath, "tnib-t031", "auth", "retired/thing")

	resetSetFlags()
	resetRootPersistentFlags()
	_, err := runRootWith(t, "--nibs-path", nibsPath, "set", "tnib-t031", "--title", "Renamed")
	if err == nil {
		t.Fatal("a stored area the config no longer declares must refuse the write, got nil")
	}
	for _, want := range []string{
		"retired/thing",   // the offending value
		"already carries", // whose value it is
		"--clear area",    // the way out
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want substring %q", err.Error(), want)
		}
	}

	// The way out the message names works, on the nib in that state.
	resetSetFlags()
	resetRootPersistentFlags()
	if _, err := runRootWith(t, "--nibs-path", nibsPath, "set", "tnib-t031", "--clear", "area"); err != nil {
		t.Errorf("the remedy the refusal names must work: %v", err)
	}
}

// rewriteStoredArea edits a nib's `area:` on disk, the way a hand edit or a
// retired declaration leaves one, without going through a write path.
func rewriteStoredArea(t *testing.T, nibsPath, id, from, to string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(nibsPath, "data", id+"*.md"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("locating %s: %v (matches %v)", id, err, matches)
	}
	raw, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("reading %s: %v", matches[0], err)
	}
	rewritten := strings.Replace(string(raw), "area: "+from, "area: "+to, 1)
	if rewritten == string(raw) {
		t.Fatalf("no `area: %s` key in %s:\n%s", from, matches[0], raw)
	}
	if err := os.WriteFile(matches[0], []byte(rewritten), 0644); err != nil {
		t.Fatalf("writing %s: %v", matches[0], err)
	}
}

// listAreaIDs runs `nibs list --area <path> --all -q` and returns the ids it
// listed, sorted. --all is deliberate: the area axis says nothing about status,
// and the open default would otherwise decide half of what these rows assert.
func listAreaIDs(t *testing.T, nibsPath, area string) []string {
	t.Helper()
	resetListFlags()
	resetRootPersistentFlags()
	out, err := runRootWith(t, "--nibs-path", nibsPath, "list", "--area", area, "--all", "-q")
	if err != nil {
		t.Fatalf("list --area %s: %v", area, err)
	}
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			ids = append(ids, line)
		}
	}
	sort.Strings(ids)
	return ids
}

// TestListAreaIsDownwardClosedOverTheDeclaredTree is the flag surface of the
// closure rule, on the shipped vocabulary: a filter selects the area named and
// every declared area below it, and a leaf never reaches back up to its parent.
//
// The tree-versus-string half of the rule cannot be shown here — the sample
// project declares `webhooks` nested under `api`, so no two of its paths spell a
// prefix of one another as separate roots. It is pinned where a vocabulary can
// be authored for it, in TestAreaFilterIsDownwardClosedOverTheDeclaredTree
// (internal/graph), over the same ApplyFilter this flag reaches.
func TestListAreaIsDownwardClosedOverTheDeclaredTree(t *testing.T) {
	nibsPath := setupAreaCLITest(t)

	tests := []struct {
		name string
		area string
		want []string
	}{
		{
			name: "a root takes its declared child too",
			area: "web",
			want: []string{"tnib-b005", "tnib-f008"},
		},
		{
			name: "a leaf takes itself alone",
			area: "web/dashboard",
			want: []string{"tnib-f008"},
		},
		{
			name: "the other nested root behaves the same way",
			area: "api",
			want: []string{"tnib-b011", "tnib-f011"},
		},
		{
			name: "a childless root takes only its own",
			area: "infra",
			want: []string{"tnib-t039"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := listAreaIDs(t, nibsPath, tt.area)
			want := append([]string(nil), tt.want...)
			sort.Strings(want)
			if !slices.Equal(got, want) {
				t.Errorf("list --area %s = %v, want %v", tt.area, got, want)
			}
		})
	}
}

// TestListAreaRefusesUndeclaredValue pins the refusal the work item asks for by
// name: a path the store does not declare is an error, not an empty listing.
//
// The empty listing is what makes it worth refusing. The fixture holds seven
// assigned nibs, so zero rows is a shape the caller sees constantly and would
// read as "nothing is in this area" — for a value that names no area at all.
func TestListAreaRefusesUndeclaredValue(t *testing.T) {
	nibsPath := setupAreaCLITest(t)

	resetListFlags()
	out, err := runRootWith(t, "--nibs-path", nibsPath, "list", "--area", "nosuch")
	if err == nil {
		t.Fatalf("an undeclared area listed instead of being refused:\n%s", out)
	}
	if code := areaErrCode(t, err); code != output.ErrValidation {
		t.Errorf("code = %q (exit %d), want %q (exit 2)", code, output.ExitCode(code), output.ErrValidation)
	}
	for _, want := range []string{"nosuch", "must be one of", "web/dashboard", "api/webhooks"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want substring %q", err.Error(), want)
		}
	}
}

// TestListAreaRefusesAnEmptyValue keeps `--area "$AREA"` with AREA unset from
// widening the listing to the whole store — the usual way an empty value
// arrives, and the one where a silent widening is a lie about the result rather
// than a broader answer.
func TestListAreaRefusesAnEmptyValue(t *testing.T) {
	nibsPath := setupAreaCLITest(t)

	resetListFlags()
	out, err := runRootWith(t, "--nibs-path", nibsPath, "list", "--area", "")
	if err == nil {
		t.Fatalf(`--area "" listed instead of being refused:\n%s`, out)
	}
	if code := areaErrCode(t, err); code != output.ErrValidation {
		t.Errorf("code = %q, want %q", code, output.ErrValidation)
	}
	if !strings.Contains(err.Error(), "empty value") {
		t.Errorf("error = %q, want it to report the value as empty", err.Error())
	}
}

// TestListAreaInAStoreDeclaringNoAreasSaysWhy answers the question the axis
// itself raises. "must be one of " followed by nothing reads as a bug in nibs;
// the real answer is that this project has never declared a vocabulary, which is
// a config edit rather than a different flag value.
func TestListAreaInAStoreDeclaringNoAreasSaysWhy(t *testing.T) {
	setupAreaCLITest(t)
	nibsPath := remedyStoreWithoutAreas(nil)(t)

	resetListFlags()
	_, err := runRootWith(t, "--nibs-path", nibsPath, "list", "--area", "web")
	if err == nil {
		t.Fatal("a store declaring no areas must refuse every --area, got nil")
	}
	if code := areaErrCode(t, err); code != output.ErrValidation {
		t.Errorf("code = %q, want %q", code, output.ErrValidation)
	}
	if !strings.Contains(err.Error(), "declares no areas") {
		t.Errorf("error = %q, want it to say the store declares none", err.Error())
	}
	if strings.Contains(err.Error(), "must be one of") {
		t.Errorf("error = %q, must not name an empty allowed set", err.Error())
	}
}

// TestQueryAreaFilterAgreesWithTheFlag is the [manual] acceptance criterion as a
// test: `nibs(filter: {area: …})` is downward-closed the same way `--area` is,
// and refuses the same value with the same exit class.
//
// Both surfaces reach one ApplyFilter, so what this really guards is that
// neither grew a reading of its own on the way there — the flag by pre-filtering
// or expanding the path itself, the graph surface by a schema field wired to
// something else.
func TestQueryAreaFilterAgreesWithTheFlag(t *testing.T) {
	nibsPath := setupAreaCLITest(t)

	for _, area := range []string{"web", "web/dashboard", "api", "infra"} {
		t.Run(area, func(t *testing.T) {
			resetQueryFlags()
			out, err := runRootWith(t, "--nibs-path", nibsPath, "query", "--json",
				`{ nibs(filter:{area:"`+area+`"}) { id } }`)
			if err != nil {
				t.Fatalf("query: %v", err)
			}
			var payload struct {
				Nibs []struct {
					ID string `json:"id"`
				} `json:"nibs"`
			}
			if err := json.Unmarshal([]byte(out), &payload); err != nil {
				t.Fatalf("unmarshal query output: %v\nraw: %s", err, out)
			}
			var ids []string
			for _, b := range payload.Nibs {
				ids = append(ids, b.ID)
			}
			sort.Strings(ids)
			if want := listAreaIDs(t, nibsPath, area); !slices.Equal(ids, want) {
				t.Errorf("graphql area %q = %v, flag = %v", area, ids, want)
			}
		})
	}

	resetQueryFlags()
	_, err := runRootWith(t, "--nibs-path", nibsPath, "query",
		`{ nibs(filter:{area:"nosuch"}) { id } }`)
	if err == nil {
		t.Fatal("an undeclared area must be refused on the graph surface too, got nil")
	}
	if code := areaErrCode(t, err); code != output.ErrValidation {
		t.Errorf("code = %q (exit %d), want %q (exit 2)", code, output.ExitCode(code), output.ErrValidation)
	}
}
