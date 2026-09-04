package cmd

import (
	"context"
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
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/output"
	"github.com/alphaleonis/nibs/testdata/fixtures"
	"github.com/spf13/cobra"
)

// setupAreaCLITest copies the sample fixture — whose config declares auth, api,
// api/webhooks, web, web/dashboard, infra and docs — and registers the flag
// resets the commands under test need. Returns the store path.
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

// staleAreaApp loads the store into an App and hands it back WITHOUT reloading
// it again — the shape a CLI process is in from the moment its startup load
// finishes until it writes. A test then moves the store underneath it, which is
// what another nibs process holding the store lock does.
func staleAreaApp(t *testing.T, nibsPath string) *App {
	t.Helper()
	cfg, err := config.Load(filepath.Join(nibsPath, "config.yml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	core := nibcore.New(nibsPath, cfg)
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("load store: %v", err)
	}
	t.Cleanup(func() { _ = core.Close() })
	return &App{Core: core, MigrationGatePassed: true}
}

// countStoreFiles counts the nib files the store holds across data/ and
// archive/, which is the number an area edit must leave unchanged.
func countStoreFiles(t *testing.T, nibsPath string) (total int, oldPrefixed []string) {
	t.Helper()
	for _, sub := range []string{"data", "archive"} {
		entries, err := os.ReadDir(filepath.Join(nibsPath, sub))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatalf("read %s: %v", sub, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			total++
			if strings.HasPrefix(e.Name(), "tnib-") {
				oldPrefixed = append(oldPrefixed, e.Name())
			}
		}
	}
	sort.Strings(oldPrefixed)
	return total, oldPrefixed
}

// TestAreaCascadeWritesThePathsTheStoreHasNowIsTheAreaSideOfTheSameDefect.
//
// An `area rename` (or `area rm`) that parks on the store lock behind a
// `config set-prefix` resumes holding the paths it loaded BEFORE that rename.
// Writing a clone to one of them used to CREATE the file — resurrecting the nib
// at its pre-rename path, so the store ended up with one more file than it
// started with, under a prefix its config no longer declares.
//
// Both verbs re-derive the store under the lock, so the cascade lands on the
// files that are actually there.
func TestAreaCascadeWritesThePathsTheStoreHasNow(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, app *App) error
		// wantArea is the area the members must carry afterwards.
		wantArea string
	}{
		{
			name: "rename",
			run: func(t *testing.T, app *App) error {
				areaRenameCmd.SetContext(withApp(context.Background(), app))
				return runAreaRename(areaRenameCmd, []string{"auth", "identity"})
			},
			wantArea: "identity",
		},
		{
			name: "rm --unassign",
			run: func(t *testing.T, app *App) error {
				areaRmUnassign = true
				areaRmCmd.SetContext(withApp(context.Background(), app))
				return runAreaRm(areaRmCmd, []string{"auth"})
			},
			wantArea: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsPath := setupAreaCLITest(t)
			t.Cleanup(func() {
				areaRenameJSON, areaRmJSON, areaRmUnassign, areaRmMoveTo = false, false, false, ""
				setPrefixDryRun, setPrefixForce, setPrefixJSON = false, false, false
				areaRenameCmd.SetContext(context.Background())
				areaRmCmd.SetContext(context.Background())
			})

			// The members this cascade must reach, named by the id they carry
			// BEFORE the prefix changes underneath the command.
			before, _ := countStoreFiles(t, nibsPath)
			app := staleAreaApp(t, nibsPath)
			var members []string
			for _, b := range app.Core.All() {
				if app.Config().IsAreaWithin(b.Area, "auth") {
					members = append(members, strings.TrimPrefix(b.ID, "tnib-"))
				}
			}
			sort.Strings(members)
			if len(members) == 0 {
				t.Fatal("fixture declares area auth with no members; the cascade would prove nothing")
			}

			// Another process renames every file and rewrites the config while
			// this App holds its pre-rename snapshot.
			if _, err := runRootWith(t, "--nibs-path", nibsPath, "config", "set-prefix", "zz", "--force"); err != nil {
				t.Fatalf("set-prefix: %v", err)
			}

			if err := tt.run(t, app); err != nil {
				t.Fatalf("area cascade: %v", err)
			}

			total, oldPrefixed := countStoreFiles(t, nibsPath)
			if total != before {
				t.Errorf("store holds %d nib files, want %d — the cascade duplicated a nib", total, before)
			}
			if len(oldPrefixed) != 0 {
				t.Errorf("files resurrected under the retired prefix: %v", oldPrefixed)
			}
			for _, short := range members {
				if got := areaOf(t, nibsPath, "zz-"+short); got != tt.wantArea {
					t.Errorf("zz-%s area = %q, want %q", short, got, tt.wantArea)
				}
			}
		})
	}
}

// runStaleAreaVerb runs one area verb against an App holding the snapshot it
// loaded BEFORE the store moved — the shape a CLI process is in while it waits
// on the store's write lock.
//
// The flags go through the command's own FlagSet rather than through the
// package variables they bind, because `--move-to` is read off the Changed bit
// and a bare assignment leaves that false.
func runStaleAreaVerb(t *testing.T, app *App, cmd *cobra.Command, run func(*cobra.Command, []string) error,
	flags map[string]string, args ...string,
) error {
	t.Helper()
	resetCommandTreeFlags(rootCmd)
	t.Cleanup(func() {
		resetCommandTreeFlags(rootCmd)
		cmd.SetContext(context.Background())
	})
	for name, value := range flags {
		if err := cmd.Flags().Set(name, value); err != nil {
			t.Fatalf("set --%s=%s: %v", name, value, err)
		}
	}
	cmd.SetContext(withApp(context.Background(), app))
	return run(cmd, args)
}

// TestAreaRetireRefusesAMoveToTargetRetiredUnderTheLock is nibs-fohy's own
// scenario. `nibs area rm auth --move-to infra` resolved its target from the
// vocabulary this process loaded at startup, so a `nibs area rm infra
// --unassign` that finished while this one waited for the store's write lock
// left every reassigned member carrying a path the store no longer declares —
// permanently write-refused, with both processes exiting 0.
func TestAreaRetireRefusesAMoveToTargetRetiredUnderTheLock(t *testing.T) {
	nibsPath := setupAreaCLITest(t)
	app := staleAreaApp(t, nibsPath)

	// Another nibs process retires the very area this one was told to move its
	// members into.
	resetCommandTreeFlags(rootCmd)
	if _, err := runRootWith(t, "--nibs-path", nibsPath, "area", "rm", "infra", "--unassign"); err != nil {
		t.Fatalf("the racing retire: %v", err)
	}

	err := runStaleAreaVerb(t, app, areaRmCmd, runAreaRm, map[string]string{"move-to": "infra"}, "auth")
	if err == nil {
		t.Fatal("moving members into an area retired under the lock must be refused, got nil")
	}
	if code := areaErrCode(t, err); code != output.ErrFileError {
		t.Errorf("code = %q (exit %d), want %q (exit %d) — the store moved under the command, which is not bad input",
			code, output.ExitCode(code), output.ErrFileError, output.ExitCode(output.ErrFileError))
	}
	for _, want := range []string{"infra", "when this command started"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want substring %q", err.Error(), want)
		}
	}
	// The refusal lands before anything is written: the members still carry
	// auth, and auth is still declared.
	for _, id := range []string{"tnib-b002", "tnib-f002"} {
		if got := areaOf(t, nibsPath, id); got != "auth" {
			t.Errorf("%s area = %q, want auth — the members were moved onto a retired area", id, got)
		}
	}
	if got := areaVocabulary(t, nibsPath); !slices.Contains(got, "auth") {
		t.Errorf("the refused retire removed the declaration: %v", got)
	}
}

// TestAreaEditsCascadeThroughAreasDeclaredUnderTheLock: the cascade's membership
// question is asked of the vocabulary the store declares NOW, not of the one
// this process loaded.
//
// A concurrent `area rename api/webhooks hooks` moves a member onto a path the
// waiting process never loaded. Deciding membership from that snapshot skips it,
// so the verb retires or renames its parent out from under a nib it never
// rewrote — the same permanent write refusal, reached from the other direction.
func TestAreaEditsCascadeThroughAreasDeclaredUnderTheLock(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, app *App) error
		// wantAreas is what each member of api must carry afterwards, and
		// wantVocabulary what the file declares — so a cascade that reached every
		// member cannot pass on a run whose declaration edit never landed.
		wantAreas      map[string]string
		wantVocabulary []string
	}{
		{
			name: "rename",
			run: func(t *testing.T, app *App) error {
				return runStaleAreaVerb(t, app, areaRenameCmd, runAreaRename, nil, "api", "platform")
			},
			wantAreas:      map[string]string{"tnib-b011": "platform", "tnib-f011": "platform/hooks"},
			wantVocabulary: []string{"auth", "platform", "platform/hooks", "web", "web/dashboard", "infra", "docs"},
		},
		{
			name: "rm --unassign",
			run: func(t *testing.T, app *App) error {
				return runStaleAreaVerb(t, app, areaRmCmd, runAreaRm, map[string]string{"unassign": "true"}, "api")
			},
			wantAreas:      map[string]string{"tnib-b011": "", "tnib-f011": ""},
			wantVocabulary: []string{"auth", "web", "web/dashboard", "infra", "docs"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsPath := setupAreaCLITest(t)
			app := staleAreaApp(t, nibsPath)

			resetCommandTreeFlags(rootCmd)
			if _, err := runRootWith(t, "--nibs-path", nibsPath, "area", "rename", "api/webhooks", "hooks"); err != nil {
				t.Fatalf("the racing rename: %v", err)
			}
			if got := areaOf(t, nibsPath, "tnib-f011"); got != "api/hooks" {
				t.Fatalf("the racing rename left tnib-f011 at %q, want api/hooks", got)
			}

			if err := tt.run(t, app); err != nil {
				t.Fatalf("area edit: %v", err)
			}
			for id, want := range tt.wantAreas {
				if got := areaOf(t, nibsPath, id); got != want {
					t.Errorf("%s area = %q, want %q — the cascade decided membership from the vocabulary this process loaded", id, got, want)
				}
			}
			if got := areaVocabulary(t, nibsPath); !slices.Equal(got, tt.wantVocabulary) {
				t.Errorf("vocabulary = %v, want %v", got, tt.wantVocabulary)
			}
		})
	}
}

// A refusal the two arguments alone decide must not queue behind the store's
// write lock. AcquireStoreLock is a blocking flock with no timeout and prints
// nothing while it waits, so `nibs area rename web ""` behind a long-running
// writer sat silent for the whole of that writer's run before printing an error
// about the empty string it was handed.
//
// The lock is held for the length of each case, so a check that moved back under
// it does not fail an assertion — it never returns, and the deadline below is
// what reports that.
func TestAreaRenameRefusesABadNameWithoutTakingTheStoreLock(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "an empty new name", args: []string{"web", ""}, want: "needs a name"},
		{name: "a new name that is only whitespace padding", args: []string{"web", " frontend"}, want: "whitespace"},
		{name: "a path where a name belongs", args: []string{"web/dashboard", "web/panel"}, want: "not a name"},
		{name: "the name the node already has", args: []string{"api/webhooks", "webhooks"}, want: "already named"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsPath := setupAreaCLITest(t)
			app := staleAreaApp(t, nibsPath)

			lock, err := nibcore.AcquireStoreLock(nibsPath)
			if err != nil {
				t.Fatalf("holding the store's write lock: %v", err)
			}
			defer func() { _ = lock.Release() }()

			resetCommandTreeFlags(rootCmd)
			t.Cleanup(func() {
				resetCommandTreeFlags(rootCmd)
				areaRenameCmd.SetContext(context.Background())
			})
			areaRenameCmd.SetContext(withApp(context.Background(), app))

			done := make(chan error, 1)
			go func() { done <- runAreaRename(areaRenameCmd, tt.args) }()

			select {
			case err := <-done:
				if err == nil {
					t.Fatalf("expected a refusal over %v, got nil", tt.args)
				}
				if !strings.Contains(err.Error(), tt.want) {
					t.Errorf("error = %q, want substring %q", err.Error(), tt.want)
				}
			case <-time.After(10 * time.Second):
				t.Fatalf("`area rename %v` is still waiting for the store's write lock — the two arguments alone refuse it, and nothing is printed while it waits", tt.args)
			}
		})
	}
}

// A store whose config.yml vanished while a verb waited for the write lock is
// not a store whose areas were retired. config.loadRaw answers a missing file
// with an empty config and a NIL error, so absence arrives at the verb looking
// exactly like a concurrent `nibs area rm` — and the refusal then names a
// process that never ran and prescribes `nibs area list`, which goes on to print
// "this store declares no areas". `.nibs` is its own git repository here, so a
// `git -C .nibs checkout` rewriting that file under a running command is the
// ordinary way in.
func TestAreaEditRefusesAVanishedConfigRatherThanBlamingAnotherProcess(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, app *App) error
	}{
		{
			name: "rename",
			run: func(t *testing.T, app *App) error {
				return runStaleAreaVerb(t, app, areaRenameCmd, runAreaRename, nil, "auth", "identity")
			},
		},
		{
			name: "rm",
			run: func(t *testing.T, app *App) error {
				return runStaleAreaVerb(t, app, areaRmCmd, runAreaRm, nil, "auth")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsPath := setupAreaCLITest(t)
			app := staleAreaApp(t, nibsPath)

			if err := os.Remove(filepath.Join(nibsPath, "config.yml")); err != nil {
				t.Fatalf("removing the store config: %v", err)
			}

			err := tt.run(t, app)
			if err == nil {
				t.Fatal("expected a refusal over a store with no config, got nil")
			}
			if code := areaErrCode(t, err); code != output.ErrFileError {
				t.Errorf("code = %q (exit %d), want %q (exit %d)",
					code, output.ExitCode(code), output.ErrFileError, output.ExitCode(output.ErrFileError))
			}
			for _, want := range []string{"config.yml", "does not exist"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want substring %q", err.Error(), want)
				}
			}
			// The diagnosis this replaces: a cause that did not happen, and a
			// remedy that answers "this store declares no areas".
			for _, unwanted := range []string{"another nibs process", "nibs area list"} {
				if strings.Contains(err.Error(), unwanted) {
					t.Errorf("error = %q — an absent config is reported as a concurrent retire, and %q sends the reader nowhere", err.Error(), unwanted)
				}
			}
		})
	}
}

// The other reading of an absent config.yml: a store that never had one. That
// is a legitimate shape here — the evidence rule accepts a real directory named
// `.nibs`, `nibs list` serves it and `nibs area list` prints "this store
// declares no areas" — so the vanished-config refusal must not reach it. It
// names a file to restore that never existed, and reclassifies an undamaged
// store from VALIDATION to FILE_ERROR, which an agent branching on $? reads as
// the filesystem being broken.
func TestAreaEditOnAStoreThatNeverHadAConfigRefusesAsUndeclaredAreas(t *testing.T) {
	tests := []struct {
		name string
		verb string
		run  func(t *testing.T, app *App) error
	}{
		{
			name: "rename",
			verb: "rename",
			run: func(t *testing.T, app *App) error {
				return runStaleAreaVerb(t, app, areaRenameCmd, runAreaRename, nil, "auth", "identity")
			},
		},
		{
			name: "rm",
			verb: "retire",
			run: func(t *testing.T, app *App) error {
				return runStaleAreaVerb(t, app, areaRmCmd, runAreaRm, nil, "auth")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsPath := setupAreaCLITest(t)
			// Removed BEFORE the App is built, so this process never loaded one
			// — the difference between this store and the vanished-config one.
			if err := os.Remove(filepath.Join(nibsPath, "config.yml")); err != nil {
				t.Fatalf("removing the store config: %v", err)
			}
			app := staleAreaApp(t, nibsPath)
			if app.Config().LoadedFromFile() {
				t.Fatal("premise failed: the App loaded a config file from a store that has none")
			}

			err := tt.run(t, app)
			if err == nil {
				t.Fatal("expected a refusal over a store that declares no areas, got nil")
			}
			if code := areaErrCode(t, err); code != output.ErrValidation {
				t.Errorf("code = %q (exit %d), want %q (exit %d) — the store is undamaged, so this is bad input and not a broken filesystem",
					code, output.ExitCode(code), output.ErrValidation, output.ExitCode(output.ErrValidation))
			}
			for _, want := range []string{"declares no areas", "there is none to " + tt.verb, "`areas:` block"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want substring %q", err.Error(), want)
				}
			}
			// The vanished-config wording, which names a file to put back that
			// this store never had.
			if strings.Contains(err.Error(), "does not exist") {
				t.Errorf("error = %q — it prescribes restoring a config.yml this store never had", err.Error())
			}
		})
	}
}

// A path the store does not declare is the first thing the caller has to hear,
// whatever else is wrong with the arguments. The four argument-shape checks run
// before the store's write lock is taken — deliberately, so a typo does not sit
// silent behind a competing writer — but asked first they answer over a node
// that is not there: `nibs area rename api/hooks hooks` asserted the nonexistent
// area "is already named hooks", and the separator branch prescribed
// `nibs area rename api/hooks other`, a command the tool then refuses.
func TestAreaRenameReportsAnUndeclaredPathBeforeTheNameShape(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		unwanted string
	}{
		{name: "the name the path's last segment already has", args: []string{"api/hooks", "hooks"}, unwanted: "already named"},
		{name: "a path where a name belongs", args: []string{"api/hooks", "api/other"}, unwanted: "is not a name"},
		{name: "an empty new name", args: []string{"api/hooks", ""}, unwanted: "needs a name"},
		{name: "a new name that is only whitespace padding", args: []string{"api/hooks", " hooks"}, unwanted: "whitespace"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsPath := setupAreaCLITest(t)
			app := staleAreaApp(t, nibsPath)

			err := runStaleAreaVerb(t, app, areaRenameCmd, runAreaRename, nil, tt.args...)
			if err == nil {
				t.Fatalf("expected a refusal over %v, got nil", tt.args)
			}
			if code := areaErrCode(t, err); code != output.ErrValidation {
				t.Errorf("code = %q, want %q", code, output.ErrValidation)
			}
			for _, want := range []string{`no area "api/hooks"`, "the declared areas are"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want substring %q — the typo in the path is what the caller has to see", err.Error(), want)
				}
			}
			if strings.Contains(err.Error(), tt.unwanted) {
				t.Errorf("error = %q — it answers about the name over an area this store does not declare", err.Error())
			}
			// The separator branch's prescription is the sharpest form: a
			// runnable command the tool refuses the moment it is run.
			if strings.Contains(err.Error(), "nibs area rename api/hooks") {
				t.Errorf("error = %q — it prescribes a command that refuses with this same message", err.Error())
			}
		})
	}
}

// TestAreaEditRefusesAPathRetiredUnderTheLock: the node a verb was told to
// rename or retire can itself be gone by the time the lock is granted.
//
// config.PlanRenameStoredArea and PlanRemoveStoredArea already refuse it — they
// read the file — but as "this store's config.yml declares no area", which reads
// as a typo the caller did not make and is classified as bad input. Deciding
// from the re-read vocabulary lets the refusal say what actually happened, and
// classifies it with every other refusal over a store the filesystem moved.
func TestAreaEditRefusesAPathRetiredUnderTheLock(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, app *App) error
	}{
		{
			name: "rename",
			run: func(t *testing.T, app *App) error {
				return runStaleAreaVerb(t, app, areaRenameCmd, runAreaRename, nil, "auth", "identity")
			},
		},
		{
			name: "rm",
			run: func(t *testing.T, app *App) error {
				return runStaleAreaVerb(t, app, areaRmCmd, runAreaRm, nil, "auth")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nibsPath := setupAreaCLITest(t)
			app := staleAreaApp(t, nibsPath)

			resetCommandTreeFlags(rootCmd)
			if _, err := runRootWith(t, "--nibs-path", nibsPath, "area", "rm", "auth", "--unassign"); err != nil {
				t.Fatalf("the racing retire: %v", err)
			}
			vocabBefore := areaVocabulary(t, nibsPath)

			err := tt.run(t, app)
			if err == nil {
				t.Fatal("expected a refusal over an area retired under the lock, got nil")
			}
			if code := areaErrCode(t, err); code != output.ErrFileError {
				t.Errorf("code = %q (exit %d), want %q (exit %d)",
					code, output.ExitCode(code), output.ErrFileError, output.ExitCode(output.ErrFileError))
			}
			for _, want := range []string{"auth", "when this command started"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want substring %q", err.Error(), want)
				}
			}
			// The refusal says "nothing was written", so nothing may have been.
			if got := areaVocabulary(t, nibsPath); !slices.Equal(got, vocabBefore) {
				t.Errorf("vocabulary = %v, want %v — the refused edit rewrote the declaration", got, vocabBefore)
			}
			for _, id := range []string{"tnib-b002", "tnib-f002"} {
				if got := areaOf(t, nibsPath, id); got != "" {
					t.Errorf("%s area = %q, want it left unassigned by the racing retire", id, got)
				}
			}
		})
	}
}
