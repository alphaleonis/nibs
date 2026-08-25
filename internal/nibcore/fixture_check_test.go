package nibcore

import (
	"encoding/json"
	"reflect"
	"slices"
	"sort"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/testdata/fixtures"
)

// deliberateFixtureBrokenDocuments is what `nibs check` is expected to report
// about the shipped sample fixture, and the whole of it. The four referenced
// files are deliberately absent — they are the fixture's only coverage of the
// broken-document-link finding, which is why the fixture is not "repaired" by
// adding them. testdata/fixtures/sample-project/README.md carries that
// decision for anyone who reaches the fixture before this test.
//
// Sorted by (NibID, Path), the order the assertion normalizes to.
var deliberateFixtureBrokenDocuments = []BrokenDocument{
	{NibID: "tnib-e001", Path: "docs/auth-architecture.md"},
	{NibID: "tnib-e005", Path: "docs/websocket-rfc.md"},
	{NibID: "tnib-f014", Path: "docs/websocket-rfc.md"},
	{NibID: "tnib-m001", Path: "docs/product-roadmap.md"},
}

// TestSampleProjectCheckFindingsArePinned pins what Core.CheckAllLinks reports
// about the shipped fixture: exactly the four deliberate broken document links,
// and no other LINK-HEALTH finding of any kind.
//
// The fixture is the demo store — `task demo`, the e2e lane and the screenshot
// captures all serve it — so a store the tool itself would flag is shipped as
// the worked example. Three bugs sat under features, which the hierarchy rules
// forbid, and nothing noticed because nothing compared the fixture against
// `nibs check`.
//
// Link health is most of `nibs check` but not all of it: runCheck totals
// len(configErrors) + LinkCheckResult.TotalIssues(), plus one for a pending
// migration (cmd/check.go). Only the middle term is read here, so two of that
// sum's three sources stay outside this pin.
//
//   - Config errors. Two of the three are color checks over hardcoded tables,
//     so no fixture can move them; the one a fixture owns is an invalid
//     `default_type`. Neither fixture-config guard covers it —
//     TestSampleProjectConfigMatchesGenerator compares the shipped config.yml
//     to the generator heredoc byte for byte, and
//     TestSampleProjectDeclaresEveryAssignedArea checks the areas vocabulary
//     against the assigned values (both in internal/config). An invalid
//     default_type written into the generator and the shipped file together
//     satisfies both.
//   - Migration state. No guard asserts it either. The fixture ships a
//     current-layout store, so `nibs check --json` against a fresh copy
//     returns a null migration.
//
// The document links are pinned by nib id AND path rather than by count, so a
// fifth reference, a dropped one, and a newly shipped docs/ file each fail
// here. Every other link-health category is pinned through TotalIssues, which
// counts categories this test does not name — including ones a later rule
// adds, a property TestLinkCheckResultTotalIssuesCountsEveryCategory below
// enforces rather than assumes.
//
// It lives in this package rather than beside the fixture because `go`
// excludes any directory named testdata from wildcard package matching, so a
// test under testdata/ is never built by `./...` — the pattern the test gate,
// CI and the linter all run.
func TestSampleProjectCheckFindingsArePinned(t *testing.T) {
	root := fixtures.CopySampleProject(t)
	nibsDir := fixtures.NibsPath(root)

	// The fixture's OWN config, not config.Default(): its declared areas are
	// what the fixture's `area:` values are checked against, and a config
	// declaring none exempts the store from that check wholesale
	// (CheckAllLinks gates the area pass on Config.AreasDeclared). That silences
	// one of the fourteen categories TotalIssues sums — the area one — not half
	// of them. The config also supplies the prefix that resolves short-form
	// parent ids, but nothing in this fixture is short-form, so that half is
	// defensive rather than load-bearing today.
	//
	// The areas assertion below is not decoration: config.Load returns a
	// default config with a NIL error when the file is absent, so a deleted
	// config.yml would otherwise leave this test green with the area check
	// silently switched off.
	cfg, err := config.LoadFromStore(nibsDir)
	if err != nil {
		t.Fatalf("loading the fixture config: %v", err)
	}
	if !cfg.AreasDeclared() {
		t.Fatal("the fixture config declares no areas; the area half of this pin would be vacuous")
	}

	core := New(nibsDir, cfg)
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	result := core.CheckAllLinks()

	got := slices.Clone(result.BrokenDocuments)
	sort.Slice(got, func(i, j int) bool {
		if got[i].NibID != got[j].NibID {
			return got[i].NibID < got[j].NibID
		}
		return got[i].Path < got[j].Path
	})
	if !slices.Equal(got, deliberateFixtureBrokenDocuments) {
		t.Errorf("the fixture's broken document links drifted\n got: %v\nwant: %v",
			got, deliberateFixtureBrokenDocuments)
	}

	// Named apart from the catch-all below because this is the one the fixture
	// actually shipped, and the remedy is a re-parent rather than a re-pin.
	for _, h := range result.InvalidHierarchies {
		t.Errorf("the shipped fixture violates the hierarchy rules: %s (%s): %s",
			h.NibID, h.Path, h.Reason)
	}

	if other := result.TotalIssues() - len(result.BrokenDocuments); other != 0 {
		dump, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			t.Fatalf("rendering the check result: %v", err)
		}
		t.Errorf("expected no findings beyond the %d deliberate broken document links, got %d more:\n%s",
			len(deliberateFixtureBrokenDocuments), other, dump)
	}
}

// TestLinkCheckResultTotalIssuesCountsEveryCategory is what makes the pin
// above's "including ones a later rule adds" clause true rather than hopeful.
// TotalIssues sums its terms by hand, so a fifteenth finding category added to
// LinkCheckResult without a matching term would report zero however many
// findings it holds — dropping out of `nibs check`'s issue total and exit
// status, and out of the catch-all above, with nothing to say so.
//
// It sits beside the claim it backs rather than with the other LinkCheckResult
// tests so the two are edited together.
func TestLinkCheckResultTotalIssuesCountsEveryCategory(t *testing.T) {
	rt := reflect.TypeOf(LinkCheckResult{})
	categories := 0
	for i := range rt.NumField() {
		f := rt.Field(i)
		if !f.IsExported() || f.Type.Kind() != reflect.Slice {
			continue
		}
		categories++
		t.Run(f.Name, func(t *testing.T) {
			var result LinkCheckResult
			reflect.ValueOf(&result).Elem().Field(i).Set(reflect.MakeSlice(f.Type, 1, 1))
			if got := result.TotalIssues(); got != 1 {
				t.Errorf("TotalIssues() reports %d for a LinkCheckResult holding one %s finding, want 1: "+
					"%s is a finding category TotalIssues does not sum, so every caller counting through it "+
					"is blind to it — `nibs check`'s issue total and exit status, HasIssues, and the "+
					"catch-all in TestSampleProjectCheckFindingsArePinned. Add a term for it to TotalIssues.",
					got, f.Name, f.Name)
			}
		})
	}
	if categories == 0 {
		t.Fatal("no exported slice field found on LinkCheckResult; this guard would pass vacuously")
	}
}
