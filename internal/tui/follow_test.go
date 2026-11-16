package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/viewshape"
)

// The relations cursor stops on hub's relations in this order: rp, rc1, rc2,
// rb, rd, rm, rmb, then the document.
const (
	hubToRC1 = "j"
	hubToRM  = "j j j j j"
)

// listApp is dockApp on dockNibs with the nib view hidden and the list cursor
// on id.
func listApp(t *testing.T, id string) *App {
	t.Helper()
	user := config.LoadUserLayerFrom(filepath.Join(t.TempDir(), "nibs.yml"))
	app := dockApp(t, dockStore(t), user, 160, 40)
	moveTo(t, app, id)
	return app
}

// expandedApp opens the expanded view of id from the list, focused on its
// relations.
func expandedApp(t *testing.T, id string) *App {
	t.Helper()
	app := listApp(t, id)
	pressKeys(app, "enter")
	if app.state != viewExpanded || app.expanded.nib.ID != id || app.expanded.focused() != regionRelations {
		t.Fatalf("premise failed: enter did not expand %s on its relations", id)
	}
	return app
}

// relationsApp is dockedApp with the focus in the docked view's relations.
func relationsApp(t *testing.T, id string) *App {
	t.Helper()
	app := dockedApp(t, id, 160, 40)
	pressKeys(app, "tab")
	if app.keyContext() != ctxNibRelations {
		t.Fatalf("premise failed: tab focused the %s", app.keyContext())
	}
	return app
}

// shownID is the id of the nib that the nib view on screen shows, or "".
func shownID(app *App) string {
	view := app.docked
	if app.state == viewExpanded {
		view = app.expanded
	}
	if view.nib == nil {
		return ""
	}
	return view.nib.ID
}

// requireAt fails unless the nib view shows shown and the list cursor is on
// cursor.
func requireAt(t *testing.T, app *App, what, shown, cursor string) {
	t.Helper()
	if got, at := shownID(app), app.list.focusedID(); got != shown || at != cursor {
		t.Fatalf("%s: the view shows %q with the list cursor on %q, want %q on %q", what, got, at, shown, cursor)
	}
}

// followModes are the two sizes of the nib view, each started on hub with the
// focus in its relations.
var followModes = []struct {
	name  string
	start func(t *testing.T, id string) *App
	state viewState
}{
	{"docked", relationsApp, viewList},
	{"expanded", expandedApp, viewExpanded},
}

func TestFollowingARelationMovesTheListCursorAndBackspaceReturns(t *testing.T) {
	for _, mode := range followModes {
		t.Run(mode.name, func(t *testing.T) {
			app := mode.start(t, "hub")
			pressKeys(app, hubToRC1+" enter")
			if app.state != mode.state {
				t.Fatalf("enter left state %d, want %d", app.state, mode.state)
			}
			requireAt(t, app, "after enter", "rc1", "rc1")
			pressKeys(app, "backspace")
			requireAt(t, app, "after backspace", "hub", "hub")
			if app.state != mode.state {
				t.Errorf("backspace left state %d, want %d", app.state, mode.state)
			}
			if got := app.keyContext(); got != ctxNibRelations {
				t.Errorf("backspace left the focus in the %s, want the relations", got)
			}
			if r := app.docked.selectedRelation(); app.state == viewList && (r == nil || r.ID != "rc1") {
				t.Errorf("backspace put the relations cursor on %v, want the followed rc1", r)
			}
		})
	}
}

// The follow and the step back apply before the next key, so keys typed
// faster than any command runs still land where they would one at a time.
func TestAFollowAndAStepBackApplyBeforeTheNextKey(t *testing.T) {
	for _, mode := range followModes {
		t.Run(mode.name, func(t *testing.T) {
			app := mode.start(t, "hub")
			typeAhead(app, hubToRC1+" enter")
			requireAt(t, app, "after a burst of enter", "rc1", "rc1")
			typeAhead(app, "backspace")
			requireAt(t, app, "after a burst of backspace", "hub", "hub")
			typeAhead(app, hubToRC1+" enter backspace")
			requireAt(t, app, "after a burst of enter backspace", "hub", "hub")
		})
	}
}

// Following a link into a folded subtree unfolds the rows above the target,
// and only those.
func TestFollowingIntoAFoldUnfoldsTheTargetsAncestors(t *testing.T) {
	app := dockedApp(t, "rd", 160, 40)
	app.list.setFold("rp", true)
	app.list.setFold("hub", true)
	app.list.reshape("rd")
	pressKeys(app, "tab enter") // rd's only relation: Blocked by hub
	requireAt(t, app, "after following rd to hub", "hub", "hub")
	if folds := app.list.collapsed(); folds["rp"] || !folds["hub"] {
		t.Errorf("folds after the follow: %v, want rp unfolded and hub still folded", folds)
	}
	pressKeys(app, hubToRC1+" enter")
	requireAt(t, app, "after following hub to rc1", "rc1", "rc1")
	if app.list.collapsed()["hub"] {
		t.Error("hub stayed folded over the followed rc1")
	}
}

// listFilters are the three ways a filter hides a relation of hub, each with
// the keys from hub's relations to that relation. Each also hides the
// relation's row, so the target is not in the list.
var listFilters = []struct {
	name   string
	apply  func(t *testing.T, app *App)
	follow string
	target string
}{
	{"text query", func(t *testing.T, app *App) {
		pressKeys(app, "esc / H u b enter")
	}, hubToRC1, "rc1"},
	{"tag filter", func(t *testing.T, app *App) {
		_, cmd := app.Update(tagSelectedMsg{tag: "tui"})
		processCmd(app, cmd)
	}, hubToRC1, "rc1"},
	{"hide closed", func(t *testing.T, app *App) {
		if !app.list.hideCompleted {
			pressKeys(app, "esc z h")
		}
	}, hubToRM, "rm"},
}

// filterState is everything that filters the list.
type filterState struct {
	query, tag string
	hideClosed bool
}

func filtersOf(app *App) filterState {
	return filterState{query: app.list.textQuery(), tag: app.list.tagFilter, hideClosed: app.list.hideCompleted}
}

// A link to a nib the filter hides shows that nib with a note, and leaves the
// filter and the list cursor alone.
func TestFollowingToAFilteredOutNibShowsItWithANote(t *testing.T) {
	for _, mode := range followModes {
		for _, f := range listFilters {
			t.Run(mode.name+"/"+f.name, func(t *testing.T) {
				app := listApp(t, "hub")
				if mode.state == viewList {
					pressKeys(app, "z p")
				}
				f.apply(t, app)
				if app.list.hasRow(f.target) || app.list.focusedID() != "hub" {
					t.Fatalf("premise failed: %s left %s in the list or moved the cursor to %s", f.name, f.target, app.list.focusedID())
				}
				if mode.state == viewList {
					pressKeys(app, "tab")
				} else {
					pressKeys(app, "enter")
				}
				before := filtersOf(app)
				if strings.Contains(plainView(app), notInListNote) {
					t.Fatalf("premise failed: hub, which is in the list, shows the note:\n%s", plainView(app))
				}

				pressKeys(app, f.follow+" enter")
				requireAt(t, app, "after the follow", f.target, "hub")
				if !strings.Contains(plainView(app), notInListNote) {
					t.Errorf("the view of %s lacks the note:\n%s", f.target, plainView(app))
				}
				if got := filtersOf(app); got != before {
					t.Errorf("the follow changed the filter from %+v to %+v", before, got)
				}

				_, cmd := app.Update(nibsChangedMsg{})
				processCmd(app, cmd)
				requireAt(t, app, "after a reload", f.target, "hub")
				if !strings.Contains(plainView(app), notInListNote) {
					t.Errorf("after a reload the view of %s lacks the note:\n%s", f.target, plainView(app))
				}

				pressKeys(app, "backspace")
				requireAt(t, app, "after backspace", "hub", "hub")
				if strings.Contains(plainView(app), notInListNote) {
					t.Errorf("back on hub the view still shows the note:\n%s", plainView(app))
				}
			})
		}
	}
}

// pinnedApp follows hub's relation to rc1 in the docked view under the tag
// filter, which leaves rc1 no row: the view shows rc1, the cursor stays on hub
// and the focus stays in the view.
func pinnedApp(t *testing.T) *App {
	t.Helper()
	app := dockedApp(t, "hub", 160, 40)
	listFilters[1].apply(t, app)
	pressKeys(app, "tab "+hubToRC1+" enter")
	requireAt(t, app, "premise", "rc1", "hub")
	if app.keyContext() != ctxNibRelations {
		t.Fatalf("premise failed: the focus is in the %s", app.keyContext())
	}
	return app
}

// Once the focus is back in the list, the docked view shows the cursor's nib
// again, so every list key acts on the nib the view shows.
func TestAPinEndsWhenTheFocusReturnsToTheList(t *testing.T) {
	for _, leave := range []string{"esc", "tab tab"} {
		t.Run(leave, func(t *testing.T) {
			app := pinnedApp(t)
			pressKeys(app, leave)
			if app.keyContext() != ctxList {
				t.Fatalf("premise failed: %s left the focus in the %s", leave, app.keyContext())
			}
			requireAt(t, app, "after "+leave, "hub", "hub")

			typeAhead(app, "s")
			if app.state != viewStatusPicker || !slices.Equal(app.statusPicker.nibIDs, []string{"hub"}) {
				t.Errorf("s opened state %d for %v, want the status picker for hub", app.state, app.statusPicker.nibIDs)
			}
			pressKeys(app, "esc enter")
			if app.state != viewExpanded || app.expanded.nib.ID != "hub" {
				t.Errorf("enter expanded %v, want hub", app.expanded.nib)
			}
		})
	}
	t.Run("esc from the expanded view", func(t *testing.T) {
		app := dockedApp(t, "hub", 160, 40)
		listFilters[1].apply(t, app)
		pressKeys(app, "enter "+hubToRC1+" enter")
		requireAt(t, app, "premise", "rc1", "hub")
		pressKeys(app, "esc")
		if app.state != viewList || app.keyContext() != ctxList {
			t.Fatalf("esc left state %d in the %s, want the list", app.state, app.keyContext())
		}
		requireAt(t, app, "after esc", "hub", "hub")
	})
}

// Clearing the filter or switching the view happens from the list, where the
// pin has already ended, so the docked view stays on the cursor's nib.
func TestAFilterOrViewChangeAfterAPinShowsTheCursorsNib(t *testing.T) {
	changes := map[string]func(app *App){
		"clear the filter": func(app *App) { pressKeys(app, "esc") },
		"next view": func(app *App) {
			processCmd(app, app.runAction(findAction(app.keys.actions, actViewNext)))
			app.Update(cursorChangedMsg{})
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			app := pinnedApp(t)
			pressKeys(app, "esc")
			change(app)
			requireAt(t, app, "after the change", "hub", "hub")
			if strings.Contains(plainView(app), notInListNote) {
				t.Errorf("the view of the cursor's nib shows the note:\n%s", plainView(app))
			}
		})
	}
}

// A pinned nib that gains a row, while the focus stays in the view, ends the
// pin and takes the list cursor to its row.
func TestAPinnedNibThatGainsARowTakesTheCursor(t *testing.T) {
	app := pinnedApp(t)
	app.list.setTagFilter("")
	_, cmd := app.Update(nibsChangedMsg{})
	processCmd(app, cmd)
	requireAt(t, app, "after the filter cleared under the view", "rc1", "rc1")
	if app.pinned != (pin{}) || app.keyContext() != ctxNibRelations {
		t.Errorf("pin %+v with the focus in the %s, want no pin and the relations", app.pinned, app.keyContext())
	}
	if strings.Contains(plainView(app), notInListNote) {
		t.Errorf("the view still shows the note:\n%s", plainView(app))
	}
}

// With the view hidden, a jump back to a nib the list has no row for has
// nowhere to show it, so it says so and keeps the jump for when the view is
// back.
func TestJumpBackToANibOffTheListNeedsAView(t *testing.T) {
	app := pinnedApp(t)
	pressKeys(app, "g p") // rc1 → hub
	requireAt(t, app, "premise", "hub", "hub")
	pressKeys(app, "esc z p")
	if app.dockPlacement() != "" {
		t.Fatal("premise failed: z p did not hide the view")
	}
	pressKeys(app, "backspace")
	if len(app.jumps) != 2 || app.list.focusedID() != "hub" {
		t.Errorf("backspace left %d jumps with the cursor on %s, want 2 on hub", len(app.jumps), app.list.focusedID())
	}
	if !strings.Contains(footerLine(app), "rc1 is not in this list") {
		t.Errorf("footer = %q, want why backspace did nothing", footerLine(app))
	}

	pressKeys(app, "z p backspace")
	requireAt(t, app, "after backspace with the view shown", "rc1", "hub")
	if app.keyContext() == ctxList {
		t.Error("the view shows a nib off the list without the focus")
	}
}

// longBodyNibs is a parent and its child, whose body runs past any screen and
// who also names two documents, so its relations cursor can sit off the
// parent.
func longBodyNibs() []*nib.Nib {
	var body strings.Builder
	for i := range 200 {
		fmt.Fprintf(&body, "Line %d of the long body.\n\n", i)
	}
	return []*nib.Nib{
		{ID: "lp", Title: "Long parent", Type: "feature", Status: "todo", Order: "1"},
		{ID: "lc", Title: "Long child", Type: "task", Status: "todo", Parent: "lp", Order: "a",
			Documents: []string{"docs/a.md", "docs/b.md"}, Body: body.String()},
	}
}

// A jump back returns the view to the scroll and the relation it had.
func TestJumpBackRestoresTheScrollAndTheRelation(t *testing.T) {
	for _, docked := range []bool{true, false} {
		name := "expanded"
		if docked {
			name = "docked"
		}
		t.Run(name, func(t *testing.T) {
			app := setupRealBackendApp(t, longBodyNibs())
			app.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
			if docked {
				pressKeys(app, "z p")
			}
			moveTo(t, app, "lc")
			if docked {
				pressKeys(app, "tab")
			} else {
				pressKeys(app, "enter")
			}
			pressKeys(app, "j j tab j j j g p") // the second document, then the body
			requireAt(t, app, "after g p", "lp", "lp")
			pressKeys(app, "backspace")
			requireAt(t, app, "after backspace", "lc", "lc")
			view := app.docked
			if !docked {
				view = app.expanded
			}
			if got := view.body.YOffset(); got != 3 {
				t.Errorf("the body is at %d, want 3", got)
			}
			if es := view.entries(); view.cursor >= len(es) || es[view.cursor].doc != "docs/b.md" {
				t.Errorf("the relations cursor is on %d of %+v, want docs/b.md", view.cursor, es)
			}
		})
	}
}

// enter in the docked view's body expands the nib the view shows, which can be
// one the list has no row for; esc then returns to the list on the cursor's
// nib.
func TestEnterInTheDockedBodyExpandsThePinnedNib(t *testing.T) {
	app := pinnedApp(t)
	pressKeys(app, "tab")
	if app.keyContext() != ctxNibBody || !strings.Contains(footerLine(app), "enter open") {
		t.Fatalf("premise failed: the focus is in the %s with footer %q", app.keyContext(), footerLine(app))
	}
	typeAhead(app, "enter")
	if app.state != viewExpanded || app.expanded.nib.ID != "rc1" {
		t.Fatalf("enter left state %d on %v, want the expanded view of rc1", app.state, app.expanded.nib)
	}
	if !strings.Contains(plainView(app), notInListNote) {
		t.Errorf("the expanded view of rc1 lacks the note:\n%s", plainView(app))
	}
	pressKeys(app, "tab")
	if ready, why := isReadyIn(app, *findAction(defaultActions, actOpen)); app.keyContext() != ctxNibBody || ready || why != "the nib view is already expanded" {
		t.Errorf("enter in the expanded %s: ready %v, reason %q", app.keyContext(), ready, why)
	}
	pressKeys(app, "esc")
	if app.state != viewList || app.keyContext() != ctxList {
		t.Fatalf("esc left state %d in the %s, want the list", app.state, app.keyContext())
	}
	requireAt(t, app, "after esc", "hub", "hub")
}

// A scroll offset counts lines of a body wrapped at one size, so a jump back
// into a view of another size starts at the top. The relation carries over.
func TestJumpBackIntoAViewOfAnotherSizeStartsAtTheTop(t *testing.T) {
	app := setupRealBackendApp(t, longBodyNibs())
	app.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	pressKeys(app, "z p")
	moveTo(t, app, "lc")
	pressKeys(app, "tab j j tab j j j g p") // docked, scrolled, then to lp
	requireAt(t, app, "after g p", "lp", "lp")
	pressKeys(app, "enter backspace") // expand lp from the body, then back
	if app.state != viewExpanded || app.expanded.nib.ID != "lc" {
		t.Fatalf("backspace left state %d on %v, want the expanded view of lc", app.state, app.expanded.nib)
	}
	if got := app.expanded.body.YOffset(); got != 0 {
		t.Errorf("the expanded body is at %d, want 0", got)
	}
	if es := app.expanded.entries(); app.expanded.cursor >= len(es) || es[app.expanded.cursor].doc != "docs/b.md" {
		t.Errorf("the relations cursor is on %d, want docs/b.md", app.expanded.cursor)
	}
}

// hideParentOfHub shows Flat filtered to hub's tag, which leaves hub's parent
// rp no row: Flat shows no ancestors for context.
func hideParentOfHub(t *testing.T, app *App) {
	t.Helper()
	listFilters[1].apply(t, app)
	app.list.showLevel(viewshape.LevelFlat)
	app.syncDocked()
	if app.list.hasRow("rp") || app.list.focusedID() != "hub" {
		t.Fatalf("premise failed: rp is in the list, or the cursor is on %s", app.list.focusedID())
	}
}

func TestGoToParentAndBack(t *testing.T) {
	t.Run("list", func(t *testing.T) {
		app := listApp(t, "rc1")
		pressKeys(app, "g p")
		if app.state != viewList || app.list.focusedID() != "hub" {
			t.Fatalf("g p left state %d on %s, want the list on hub", app.state, app.list.focusedID())
		}
		pressKeys(app, "backspace")
		if app.list.focusedID() != "rc1" {
			t.Errorf("backspace left the cursor on %s, want rc1", app.list.focusedID())
		}
	})
	t.Run("list with the view docked", func(t *testing.T) {
		app := dockedApp(t, "rc1", 160, 40)
		pressKeys(app, "g p")
		requireAt(t, app, "after g p", "hub", "hub")
		pressKeys(app, "backspace")
		requireAt(t, app, "after backspace", "rc1", "rc1")
	})
	for _, mode := range followModes {
		for _, region := range []string{"relations", "body"} {
			t.Run(mode.name+" "+region, func(t *testing.T) {
				app := mode.start(t, "rc1")
				if region == "body" {
					pressKeys(app, "tab")
					if app.keyContext() != ctxNibBody {
						t.Fatalf("premise failed: tab focused the %s", app.keyContext())
					}
				}
				typeAhead(app, "g p")
				requireAt(t, app, "after g p", "hub", "hub")
				typeAhead(app, "backspace")
				requireAt(t, app, "after backspace", "rc1", "rc1")
				if app.state != mode.state {
					t.Errorf("g p backspace left state %d, want %d", app.state, mode.state)
				}
			})
		}
	}
	t.Run("a parent the filter hides", func(t *testing.T) {
		app := dockedApp(t, "hub", 160, 40)
		hideParentOfHub(t, app)
		pressKeys(app, "g p")
		requireAt(t, app, "after g p", "rp", "hub")
		if !strings.Contains(plainView(app), notInListNote) {
			t.Errorf("the view of rp lacks the note:\n%s", plainView(app))
		}
		if app.keyContext() == ctxList {
			t.Error("g p left the focus in the list, which would end the pin on rp")
		}
	})
	t.Run("a parent the filter hides, with no view to show it", func(t *testing.T) {
		app := listApp(t, "hub")
		hideParentOfHub(t, app)
		pressKeys(app, "g p")
		if app.list.focusedID() != "hub" || len(app.jumps) != 0 {
			t.Errorf("g p moved the cursor to %s with %d jumps, want hub and none", app.list.focusedID(), len(app.jumps))
		}
		if !strings.Contains(footerLine(app), "rp is not in this list") {
			t.Errorf("footer = %q, want why g p did nothing", footerLine(app))
		}
	})
}

func TestGoToParentIsNotReadyWithoutAParent(t *testing.T) {
	act := findAction(defaultActions, actGoParent)
	starts := map[string]func(t *testing.T, id string) *App{
		"list": listApp,
		"docked": func(t *testing.T, id string) *App {
			app := dockedApp(t, id, 160, 40)
			pressKeys(app, "tab")
			return app
		},
		"expanded": func(t *testing.T, id string) *App {
			app := listApp(t, id)
			pressKeys(app, "enter")
			return app
		},
	}
	for name, start := range starts {
		t.Run(name, func(t *testing.T) {
			app := start(t, "rz")
			if ready, why := isReadyIn(app, *act); ready || why != "the nib has no parent" {
				t.Errorf("g p on rz in the %s: ready %v, reason %q", app.keyContext(), ready, why)
			}
			app = start(t, "rc1")
			if ready, _ := isReadyIn(app, *act); !ready {
				t.Errorf("g p on rc1 in the %s is not ready", app.keyContext())
			}
		})
	}
}

func TestJumpBackIsNotReadyWithAnEmptyHistory(t *testing.T) {
	act := findAction(defaultActions, actJumpBack)
	for _, mode := range followModes {
		t.Run(mode.name, func(t *testing.T) {
			app := mode.start(t, "hub")
			if ready, why := isReadyIn(app, *act); ready || why != "nothing to jump back to" {
				t.Errorf("backspace with no jumps: ready %v, reason %q", ready, why)
			}
			pressKeys(app, "backspace")
			requireAt(t, app, "after backspace with no jumps", "hub", "hub")
			if app.state != mode.state || app.keyContext() != ctxNibRelations {
				t.Errorf("backspace with no jumps left state %d in the %s", app.state, app.keyContext())
			}

			pressKeys(app, hubToRC1+" enter")
			if ready, _ := isReadyIn(app, *act); !ready {
				t.Error("backspace after a follow is not ready")
			}
			if !strings.Contains(footerLine(app), "backspace jump back") {
				t.Errorf("footer after a follow = %q, want the jump back hint", footerLine(app))
			}
			pressKeys(app, "backspace")
			if ready, _ := isReadyIn(app, *act); ready {
				t.Error("backspace is ready after the history ran out")
			}
		})
	}
}

// A jump to a nib deleted since is skipped, whether a reload has dropped it
// yet or not.
func TestJumpBackSkipsADeletedNib(t *testing.T) {
	for _, reload := range []bool{false, true} {
		name := "before the reload"
		if reload {
			name = "after the reload"
		}
		t.Run(name, func(t *testing.T) {
			app := relationsApp(t, "rd")
			pressKeys(app, "enter") // rd → hub
			moveTo(t, app, "rmb")
			if app.keyContext() != ctxNibRelations {
				t.Fatalf("premise failed: the focus is in the %s", app.keyContext())
			}
			pressKeys(app, "enter") // rmb → hub
			if len(app.jumps) != 2 {
				t.Fatalf("premise failed: %d jumps, want 2", len(app.jumps))
			}
			if err := app.backend.DeleteNib(context.Background(), "rmb"); err != nil {
				t.Fatal(err)
			}
			if reload {
				_, cmd := app.Update(nibsChangedMsg{})
				processCmd(app, cmd)
				if len(app.jumps) != 1 {
					t.Errorf("after the reload %d jumps remain, want 1", len(app.jumps))
				}
			}
			pressKeys(app, "backspace")
			requireAt(t, app, "after backspace", "rd", "rd")
			if len(app.jumps) != 0 {
				t.Errorf("%d jumps remain, want none", len(app.jumps))
			}
		})
	}
}

func TestTheHistoryKeepsTheLatestJumps(t *testing.T) {
	app := dockedApp(t, "hub", 160, 40)
	for i := range maxJumps + 5 {
		target := "rc1"
		if i%2 == 1 {
			target = "hub"
		}
		app.follow(target)
	}
	if len(app.jumps) != maxJumps {
		t.Fatalf("%d jumps, want %d", len(app.jumps), maxJumps)
	}
	// The first five jumps, from hub, rc1, hub, rc1 and hub, were dropped.
	if got := app.jumps[0].nib; got != "rc1" {
		t.Errorf("the oldest jump is from %s, want rc1", got)
	}
}

// esc leaves the expanded view whatever was followed, and the history stays
// for the list.
func TestEscLeavesTheExpandedViewAndKeepsTheHistory(t *testing.T) {
	app := expandedApp(t, "hub")
	pressKeys(app, hubToRC1+" enter esc")
	if app.state != viewList || len(app.jumps) != 1 {
		t.Fatalf("esc left state %d with %d jumps, want the list with 1", app.state, len(app.jumps))
	}
	pressKeys(app, "backspace")
	if app.list.focusedID() != "hub" {
		t.Errorf("backspace in the list left the cursor on %s, want hub", app.list.focusedID())
	}
}
