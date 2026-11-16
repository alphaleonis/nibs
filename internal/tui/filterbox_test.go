package tui

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/filterlang"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/viewshape"
	"github.com/charmbracelet/x/ansi"
)

// typeQuery opens the filter box, replaces its text with query and applies
// it.
func typeQuery(app *App, query string) {
	pressKeys(app, "/ ctrl+u")
	typeText(app, query)
	pressKeys(app, "enter")
}

// matchedIDs returns the ids of the nibs the list's filter selects, in folded
// rows too, leaving out ancestors shown only as context.
func matchedIDs(app *App) []string {
	var ids []string
	for _, r := range app.list.unfolded {
		if r.NibID != "" && !r.Dimmed {
			ids = append(ids, r.NibID)
		}
	}
	slices.Sort(ids)
	return ids
}

// webMatches returns the ids of the nibs the web's table counts as matches for
// query on the store behind app, sorted. As TreeTable does, it sends the query
// without its metadata fields to the GraphQL nibs field, then keeps the nibs
// that match the metadata fields, which filter.ts matches in the client.
func webMatches(t *testing.T, app *App, query string) []string {
	t.Helper()
	rb := app.backend.(displayBackend).inner.(*RealBackend)
	f, diags := filterlang.ParseWithAreas(query, rb.core.Areas())
	if len(diags) > 0 {
		t.Fatalf("premise failed: %q has invalid tokens %v", query, filterlang.InvalidTokens(diags))
	}
	// The web's CLIENT_FIELDS, in web/src/lib/filter.ts.
	client := viewshape.Filter{
		Type: f.Type, ExcludeType: f.ExcludeType, Priority: f.Priority, ExcludePriority: f.ExcludePriority,
		Status: f.Status, ExcludeStatus: f.ExcludeStatus, Estimate: f.Estimate, ExcludeEstimate: f.ExcludeEstimate,
		Tags: f.Tags, ExcludeTags: f.ExcludeTags,
	}
	server := f
	server.Type, server.ExcludeType, server.Priority, server.ExcludePriority = nil, nil, nil, nil
	server.Status, server.ExcludeStatus, server.Estimate, server.ExcludeEstimate = nil, nil, nil, nil
	server.Tags, server.ExcludeTags = nil, nil
	asc := model.SortDirectionAsc
	nibs, err := rb.resolver.Query().Nibs(t.Context(), &server, &model.NibSort{Field: model.NibSortFieldOrder, Direction: &asc})
	if err != nil {
		t.Fatalf("nibs(%q): %v", query, err)
	}
	var ids []string
	for _, n := range nibs {
		if viewshape.Matches(n, client) {
			ids = append(ids, n.ID)
		}
	}
	slices.Sort(ids)
	return ids
}

// Flat draws a row for every match, milestones included, so its rows are the
// selection itself. With search words the web also counts the ancestors that
// the nibs field adds to the hits.
func TestTheFilterSelectsWhatTheWebSelects(t *testing.T) {
	for _, query := range []string{
		"type:bug status:open -tags:wip",
		"type:bug status:open -tags:security",
		"parent:tnib-e002",
		"ancestor:tnib-e001 -status:closed",
		"is:blocked",
		"has:parent -type:task",
		"area:api",
		"area:web priority:high,normal",
		"session",
		"session -status:closed",
		"session type:feature,epic",
		"token -type:bug",
	} {
		t.Run(query, func(t *testing.T) {
			app := sampleApp(t, 140)
			pressKeys(app, "v f")
			typeQuery(app, query)
			want := webMatches(t, app, query)
			if len(want) == 0 {
				t.Fatalf("premise failed: the web selects no nib for %q", query)
			}
			if got := matchedIDs(app); !slices.Equal(got, want) {
				t.Errorf("the list selects %v, want the web's %v", got, want)
			}
			if got := headerCount(t, app); got != strconv.Itoa(len(want)) {
				t.Errorf("the header counts %s nibs, want %d", got, len(want))
			}
		})
	}
}

// A relationship token keeps Tree's context: the matches' ancestors are drawn
// dimmed and are not counted.
func TestARelationTokenDimsTheAncestorsInTree(t *testing.T) {
	app := sampleApp(t, 140)
	typeQuery(app, "parent:tnib-f004")
	want := webMatches(t, app, "parent:tnib-f004")
	if got := matchedIDs(app); !slices.Equal(got, want) {
		t.Fatalf("the list selects %v, want %v", got, want)
	}
	dimmed := map[string]bool{}
	for _, r := range app.list.unfolded {
		if r.Dimmed {
			dimmed[r.NibID] = true
		}
	}
	if !dimmed["tnib-f004"] || !dimmed[app.list.byID["tnib-f004"].Parent] {
		t.Errorf("dimmed rows %v, want tnib-f004 and its parent as context", dimmed)
	}
}

// An invalid token sets nothing, so it does not empty the list, and the header
// draws it in the error color.
func TestAnInvalidTokenIsMarkedAndFiltersNothing(t *testing.T) {
	app, _ := setupTestApp(t, listKeysNibs())
	typeQuery(app, "")
	all := len(app.list.list.Items())
	typeQuery(app, "type:nope")

	if app.list.query != "type:nope" || len(app.list.list.Items()) != all {
		t.Errorf("under %q the list has %d rows, want all %d", app.list.query, len(app.list.list.Items()), all)
	}
	header := strings.Split(app.View().Content, "\n")[1]
	invalid := spanStyles[filterlang.SpanInvalid]
	if !strings.Contains(header, invalid.Render("n")+invalid.Render("o")) {
		t.Errorf("the header does not draw nope as invalid: %q", header)
	}
	if strings.Contains(header, invalid.Render("t")) {
		t.Errorf("the header draws the field name as invalid: %q", header)
	}
}

// Completion offers the field name, then its values, and tab, the arrows and
// enter accept them in the box.
func TestCompletionInsertsAFieldAndAValue(t *testing.T) {
	app, _ := setupTestApp(t, listKeysNibs())
	pressKeys(app, "/ ctrl+u t y")
	if app.keyContext() != ctxFilterMenu || !strings.Contains(plainView(app), "type:") {
		t.Fatalf("premise failed: after ty the context is the %s and the screen shows:\n%s", app.keyContext(), plainView(app))
	}
	pressKeys(app, "tab")
	if got := app.list.box.Value(); got != "type:" {
		t.Fatalf("tab made %q, want type:", got)
	}
	values := app.list.completions
	if len(values) < 2 || values[0].Kind != filterlang.CompletionValue {
		t.Fatalf("after type: the menu offers %+v, want the type values", values)
	}

	pressKeys(app, "down down enter")

	if got, want := app.list.box.Value(), "type:"+values[1].Item; got != want || !app.list.editing {
		t.Errorf("down down enter made %q with the box open = %v, want %q in the open box", got, app.list.editing, want)
	}
}

// esc first closes the menu, then empties the query and closes the box, as the
// web's clear button does. In the list esc returns to the default query.
func TestEscClosesTheMenuThenClearsTheQuery(t *testing.T) {
	app, _ := setupTestApp(t, listKeysNibs())
	pressKeys(app, "/ t")
	if len(app.list.completions) == 0 {
		t.Fatal("premise failed: t offers no completions")
	}
	pressKeys(app, "esc")
	if !app.list.editing || len(app.list.completions) != 0 || app.list.box.Value() != closedToken+" t" {
		t.Errorf("esc in the menu left the box open = %v, completions %v, text %q; want the box open with its text and no menu",
			app.list.editing, app.list.completions, app.list.box.Value())
	}
	pressKeys(app, "esc")
	if app.list.editing || app.list.query != "" {
		t.Errorf("esc in the box left it open = %v with query %q, want closed and empty", app.list.editing, app.list.query)
	}
	if !slices.Contains(rowIDs(app), "nib-3") {
		t.Errorf("the empty query still hides the closed nib-3: %v", rowIDs(app))
	}
	pressKeys(app, "esc")
	if app.list.query != closedToken {
		t.Errorf("esc in the list left the query %q, want the default %q", app.list.query, closedToken)
	}
}

// A reload asks the server again, so a change on disk that moves a nib in or
// out of a relationship token's answer reaches the list.
func TestAReloadEvaluatesTheQueryAgain(t *testing.T) {
	app, stub := setupTestApp(t, listKeysNibs())
	answer := map[string]bool{"nib-1": true}
	stub.FilterIDsFn = func(*model.NibFilter) (map[string]bool, error) { return answer, nil }
	typeQuery(app, "is:blocked")
	if got := matchedIDs(app); !slices.Equal(got, []string{"nib-1"}) {
		t.Fatalf("premise failed: the list selects %v, want [nib-1]", got)
	}
	answer = map[string]bool{"nib-4": true}

	reload(app)

	if got := matchedIDs(app); !slices.Equal(got, []string{"nib-4"}) {
		t.Errorf("after the reload the list selects %v, want [nib-4]", got)
	}
}

// A query the server refuses selects nothing and says why in place of the
// rows.
func TestARefusedQueryShowsTheRefusal(t *testing.T) {
	app, stub := setupTestApp(t, listKeysNibs())
	stub.FilterIDsFn = func(*model.NibFilter) (map[string]bool, error) {
		return nil, errors.New("parentId filter: no nib with id nope")
	}
	typeQuery(app, "parent:nope")
	if len(app.list.list.Items()) != 0 {
		t.Errorf("the list shows %v under a refused query", rowIDs(app))
	}
	if rows := strings.Split(plainView(app), "\n")[2]; !strings.Contains(rows, "no nib with id nope") {
		t.Errorf("the first row line is %q, want the refusal", rows)
	}
}

// The nibs a relationship token offers have titles from the store, which the
// menu strips.
func TestTheNibCompletionsDrawNoControlSequence(t *testing.T) {
	app, _ := setupTestApp(t, hostileNibs())
	pressKeys(app, "/ ctrl+u")
	typeText(app, "parent:e")
	if len(app.list.completions) == 0 || app.list.completions[0].Kind != filterlang.CompletionNib {
		t.Fatalf("premise failed: parent:e offers %+v, want nibs", app.list.completions)
	}
	requireNoInjectedEscape(t, "the nib completions", app.View().Content, "Parent")
}

// A query too long for the header scrolls to keep the cursor in view.
func TestRenderQueryKeepsTheCursorInView(t *testing.T) {
	text := "type:bug status:open -tags:wip"
	spans := filterlang.Spans(text)
	if got := ansi.Strip(renderQuery(text, spans, len(text), 10)); got != "-tags:wip " {
		t.Errorf("the end of the query drew %q, want %q", got, "-tags:wip ")
	}
	if got := ansi.Strip(renderQuery(text, spans, -1, 8)); got != "type:bug" {
		t.Errorf("the query without a cursor drew %q, want %q", got, "type:bug")
	}
}

func TestSplitFilterLeavesNoRestForMetadataAlone(t *testing.T) {
	f, _ := filterlang.Parse("type:bug -status:closed tags:a")
	if _, rest := splitFilter(f); rest != nil {
		t.Errorf("the metadata fields left %+v for the server", *rest)
	}
	f, _ = filterlang.Parse("type:bug is:blocked")
	meta, rest := splitFilter(f)
	if rest == nil || rest.IsBlocked == nil || rest.Type != nil || !slices.Equal(meta.Type, []string{"bug"}) {
		t.Errorf("split into %+v and %+v, want type in the metadata and is:blocked in the rest", meta, rest)
	}
}

// Opening and closing the box without changing what the query selects keeps
// the cursor, and only a changed query moves it to the first match.
func TestTheBoxMovesTheCursorOnlyForAChangedQuery(t *testing.T) {
	for _, tt := range []struct {
		name, keys, typed, want string
	}{
		{"open", "/", "", "nib-4"},
		{"open and enter", "/ enter", "", "nib-4"},
		{"open and apply with down", "/ down", "", "nib-4"},
		{"whitespace only", "/ space space enter", "", "nib-4"},
		{"a changed query", "/", "type:epic", "nib-1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			app, _ := setupTestApp(t, listKeysNibs())
			if !focusOn(app, "nib-4") {
				t.Fatal("premise failed: could not focus nib-4")
			}
			pressKeys(app, tt.keys)
			typeText(app, tt.typed)
			if got := selectedID(app); got != tt.want {
				t.Errorf("after %q and %q the cursor is on %q, want %q", tt.keys, tt.typed, got, tt.want)
			}
		})
	}
}

// z h pressed twice returns the query it started from, whatever the query
// excluded or required of the status before, also when the filter box was
// opened and closed without a change in between.
func TestZHTwiceRestoresTheQuery(t *testing.T) {
	for _, start := range []string{
		"",
		closedToken,
		"-status:completed",
		"-status:completed,scrapped",
		"status:todo",
		"foo -status:completed",
		"type:bug -status:closed bar",
	} {
		for _, between := range []string{"", "/ enter", "/ down", "/ space space enter"} {
			t.Run(start+"/"+between, func(t *testing.T) {
				app, _ := setupTestApp(t, listKeysNibs())
				typeQuery(app, start)
				before := app.list.query
				pressKeys(app, "z h")
				once := app.list.query
				pressKeys(app, between+" z h")
				if once == before || app.list.query != before {
					t.Errorf("z h made %q of %q, and %q then z h %q; want a change and then %q", once, before, between, app.list.query, before)
				}
			})
		}
	}
}

// A z h after another change to the query toggles that query, even when the
// change returns the query that the first z h made.
func TestZHAfterAnotherChangeTogglesAfresh(t *testing.T) {
	app, _ := setupTestApp(t, listKeysNibs())
	typeQuery(app, "-status:completed")
	pressKeys(app, "z h")
	if app.list.query != closedToken {
		t.Fatalf("premise failed: z h made %q, want %q", app.list.query, closedToken)
	}
	typeQuery(app, "type:bug")
	pressKeys(app, "esc")
	if app.list.query != closedToken {
		t.Fatalf("premise failed: esc reset the query to %q, want %q", app.list.query, closedToken)
	}
	pressKeys(app, "z h")
	if app.list.query != "" {
		t.Errorf("z h after the reset made %q, want the closed statuses shown", app.list.query)
	}
}
