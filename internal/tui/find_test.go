package tui

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/charmbracelet/x/ansi"
)

// findNibs is a tree in which "needle" is in two titles and one tag:
//
//	n-1 Alpha epic
//	  n-2 Needle feature
//	    n-3 Plain leaf      (tag needle-tag)
//	  n-4 Beta task
//	n-5 Gamma root
//	  n-6 Other needle
func findNibs() []*nib.Nib {
	nibs := []*nib.Nib{
		makeNib("n-1", "Alpha epic", "epic", "todo", ""),
		makeNib("n-2", "Needle feature", "feature", "todo", "n-1"),
		makeNib("n-3", "Plain leaf", "task", "todo", "n-2"),
		makeNib("n-4", "Beta task", "task", "todo", "n-1"),
		makeNib("n-5", "Gamma root", "epic", "todo", ""),
		makeNib("n-6", "Other needle", "task", "todo", "n-5"),
	}
	for i, n := range nibs {
		n.Order = strconv.Itoa(i + 1)
	}
	nibs[2].Tags = []string{"needle-tag"}
	return nibs
}

// findIn opens the find box, types text and leaves the box with enter.
func findIn(app *App, text string) {
	pressKeys(app, "f")
	typeText(app, text)
	pressKeys(app, "enter")
}

func TestFindJumpsToTheFirstMatchAsItIsTyped(t *testing.T) {
	app, _ := setupTestApp(t, findNibs())
	pressKeys(app, "f")
	if app.keyContext() != ctxFindBox {
		t.Fatalf("f opened context %s, want the find box", app.keyContext())
	}
	typeText(app, "need")
	if got := selectedID(app); got != "n-2" {
		t.Errorf("the cursor is on %q after typing, want n-2", got)
	}
	if app.list.query != app.list.defaultQuery || len(shownRows(app)) != len(findNibs()) {
		t.Errorf("find filtered the list: query %q, %d rows", app.list.query, len(shownRows(app)))
	}
	if header := strings.Split(plainView(app), "\n")[1]; !strings.Contains(header, "find: need") {
		t.Errorf("the header does not show the find box: %q", header)
	}
}

// The box opens empty, so what is typed replaces the last text, and enter on
// the empty box keeps the last text for f3.
func TestReopeningFindReplacesTheText(t *testing.T) {
	app, _ := setupTestApp(t, findNibs())
	findIn(app, "needle")
	findIn(app, "n-4")
	if got := selectedID(app); got != "n-4" {
		t.Fatalf("the second find put the cursor on %q, want n-4", got)
	}
	pressKeys(app, "f enter home")
	sendKeyNonBlocking(app, keyPress("f3"))
	if got := selectedID(app); got != "n-4" {
		t.Errorf("f3 after an empty find moved to %q, want n-4", got)
	}
}

func TestCtrlFOpensTheFindBox(t *testing.T) {
	app, _ := setupTestApp(t, findNibs())
	sendKeyNonBlocking(app, keyPress("ctrl+f"))
	if app.keyContext() != ctxFindBox {
		t.Errorf("ctrl+f opened context %s, want the find box", app.keyContext())
	}
}

// Matching ignores letter case and reads the ID and the tags beside the title.
func TestFindMatchesTitleIDAndTagsInAnyLetterCase(t *testing.T) {
	for _, tt := range []struct{ text, want string }{
		{"NEEDLE F", "n-2"},
		{"N-4", "n-4"},
		{"NEEDLE-TAG", "n-3"},
	} {
		t.Run(tt.text, func(t *testing.T) {
			app, _ := setupTestApp(t, findNibs())
			findIn(app, tt.text)
			if got := selectedID(app); got != tt.want {
				t.Errorf("find %q put the cursor on %q, want %s", tt.text, got, tt.want)
			}
		})
	}
}

func TestFindUnfoldsTheAncestorsOfAFoldedHit(t *testing.T) {
	app, _ := setupTestApp(t, findNibs())
	app.list.collapsed()["n-1"] = true
	app.list.collapsed()["n-2"] = true
	app.list.reshape("n-1")
	if slices.ContainsFunc(shownRows(app), func(r shownRow) bool { return r.id == "n-3" }) {
		t.Fatal("premise failed: n-3 is on screen before the find")
	}

	findIn(app, "plain")

	if got := selectedID(app); got != "n-3" {
		t.Fatalf("the cursor is on %q, want the folded hit n-3", got)
	}
	if app.list.collapsed()["n-1"] || app.list.collapsed()["n-2"] {
		t.Errorf("the hit's ancestors are still folded: %v", app.list.collapsed())
	}
}

func TestFindNeverReachesARowTheFilterHides(t *testing.T) {
	app, _ := setupTestApp(t, findNibs())
	typeQuery(app, "-type:task")
	if slices.ContainsFunc(shownRows(app), func(r shownRow) bool { return r.id == "n-6" }) {
		t.Fatal("premise failed: the filter shows n-6")
	}

	findIn(app, "needle")
	if got := selectedID(app); got != "n-2" {
		t.Fatalf("the cursor is on %q, want n-2", got)
	}
	for range 3 {
		pressKeys(app, "f3")
		if got := selectedID(app); got != "n-2" {
			t.Fatalf("f3 moved the cursor to %q, which is not the one match the filter shows", got)
		}
		if got := app.list.statusMessage; got != findWrappedToFirst {
			t.Fatalf("f3 from the one match shows %q, want the wrap note", got)
		}
	}
	if got := app.list.query; got != "-type:task" {
		t.Errorf("find changed the query to %q", got)
	}
}

// A dimmed ancestor is on screen, so find reaches it like a match.
func TestFindReachesARowShownOnlyAsContext(t *testing.T) {
	app, _ := setupTestApp(t, findNibs())
	typeQuery(app, "tags:needle-tag")
	pressKeys(app, "end")
	if !slices.Contains(shownRows(app), shownRow{"n-1", true}) {
		t.Fatalf("premise failed: n-1 is not a dimmed row: %v", shownRows(app))
	}
	findIn(app, "alpha")
	if got := selectedID(app); got != "n-1" {
		t.Errorf("the cursor is on %q, want the dimmed n-1", got)
	}
}

func TestFindWrapsWithANoteInBothDirections(t *testing.T) {
	app, _ := setupTestApp(t, findNibs())
	findIn(app, "needle")

	pressKeys(app, "f3")
	if got := selectedID(app); got != "n-3" {
		t.Fatalf("f3 moved to %q, want n-3", got)
	}
	if app.list.statusMessage != "" {
		t.Errorf("a step without a wrap showed %q", app.list.statusMessage)
	}
	pressKeys(app, "f3")
	if got := selectedID(app); got != "n-6" {
		t.Fatalf("f3 moved to %q, want n-6", got)
	}

	pressKeys(app, "f3")
	if got := selectedID(app); got != "n-2" {
		t.Errorf("f3 past the last match moved to %q, want the first, n-2", got)
	}
	if got := app.list.statusMessage; got != findWrappedToFirst {
		t.Errorf("the note after the forward wrap is %q, want %q", got, findWrappedToFirst)
	}

	sendKeyNonBlocking(app, keyPress("shift+f3"))
	if got := selectedID(app); got != "n-6" {
		t.Errorf("shift+f3 before the first match moved to %q, want the last, n-6", got)
	}
	if got := app.list.statusMessage; got != findWrappedToLast {
		t.Errorf("the note after the backward wrap is %q, want %q", got, findWrappedToLast)
	}
	if !strings.Contains(plainView(app), findWrappedToLast) {
		t.Error("the footer does not show the wrap note")
	}
}

func TestFindWithNoMatchLeavesTheCursorWithANote(t *testing.T) {
	app, _ := setupTestApp(t, findNibs())
	pressKeys(app, "down")
	pressKeys(app, "f")
	typeText(app, "zzz")
	if got := selectedID(app); got != "n-2" {
		t.Errorf("the cursor moved to %q, want it on n-2", got)
	}
	if got := app.list.statusMessage; !strings.Contains(got, "zzz") {
		t.Errorf("the note is %q, want one that names zzz", got)
	}

	pressKeys(app, "enter f3")
	if got := selectedID(app); got != "n-2" {
		t.Errorf("f3 with no match moved the cursor to %q", got)
	}
	if got := app.list.statusMessage; !strings.Contains(got, "zzz") {
		t.Errorf("the note after f3 is %q, want one that names zzz", got)
	}
}

// Enter keeps the text for f3, and esc clears it. Both keep the cursor on the
// last hit.
func TestLeavingTheFindBoxKeepsTheCursor(t *testing.T) {
	for _, tt := range []struct {
		key      string
		keepText bool
	}{{"enter", true}, {"esc", false}} {
		t.Run(tt.key, func(t *testing.T) {
			app, _ := setupTestApp(t, findNibs())
			pressKeys(app, "f")
			typeText(app, "needle")
			sendKeyNonBlocking(app, keyPress("f3"))
			if got := selectedID(app); got != "n-3" {
				t.Fatalf("premise failed: f3 in the box moved to %q, want n-3", got)
			}

			pressKeys(app, tt.key)

			if app.keyContext() != ctxList {
				t.Fatalf("%s left context %s, want the list", tt.key, app.keyContext())
			}
			if got := selectedID(app); got != "n-3" {
				t.Errorf("%s moved the cursor to %q, want it kept on n-3", tt.key, got)
			}
			pressKeys(app, "f3")
			want := "n-3"
			if tt.keepText {
				want = "n-6"
			}
			if got := selectedID(app); got != want {
				t.Errorf("f3 after %s moved to %q, want %s", tt.key, got, want)
			}
		})
	}
}

// The text found stays after the box closes, so f3 reads the rows as they
// are after the store or the filter changes.
func TestFindReadsTheRowsAfterAReloadAndAFilterChange(t *testing.T) {
	app, stub := setupTestApp(t, findNibs())
	findIn(app, "needle")
	pressKeys(app, "f3 f3")
	if got := selectedID(app); got != "n-6" {
		t.Fatalf("premise failed: the cursor is on %q, want n-6", got)
	}

	added := makeNib("n-7", "Third needle", "task", "todo", "")
	added.Order = "7"
	stub.AllNibs = append(stub.AllNibs, added)
	stub.Nibs[added.ID] = added
	processCmd(app, app.list.loadNibs)
	pressKeys(app, "f3")
	if got := selectedID(app); got != "n-7" {
		t.Errorf("f3 after the reload moved to %q, want the new match n-7", got)
	}

	typeQuery(app, "-type:task")
	pressKeys(app, "f3")
	if got := selectedID(app); got != "n-2" {
		t.Errorf("f3 after the filter change moved to %q, want n-2", got)
	}
}

// reversedRuns returns the stretches of s's text drawn reversed, and s's text.
func reversedRuns(s string) (runs []string, text string) {
	var b, run strings.Builder
	reversed := false
	flush := func() {
		if run.Len() > 0 {
			runs = append(runs, run.String())
			run.Reset()
		}
	}
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], "\x1b[") {
			end := strings.IndexByte(s[i:], 'm')
			params := strings.Split(s[i+2:i+end], ";")
			for j := 0; j < len(params); j++ {
				switch params[j] {
				case "", "0", "27":
					reversed = false
				case "7":
					reversed = true
				case "38", "48", "58":
					if j+1 < len(params) && params[j+1] == "5" {
						j += 2
					} else {
						j += 4
					}
				}
			}
			if !reversed {
				flush()
			}
			i += end + 1
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		b.WriteString(s[i : i+size])
		if reversed {
			run.WriteString(s[i : i+size])
		}
		i += size
	}
	flush()
	return runs, b.String()
}

func TestMarkFoundReversesEachMatchAndKeepsTheText(t *testing.T) {
	plain := lipgloss.NewStyle()
	for _, tt := range []struct {
		name, shown, full, find string
		want                    []string
	}{
		{"one match", "Other needle", "Other needle", "NEEDLE", []string{"needle"}},
		{"two matches", "ab ab", "ab ab", "ab", []string{"ab", "ab"}},
		{"multibyte", "Grüße Grüße", "Grüße Grüße", "RÜSS", nil},
		{"multibyte case", "Grüße aus Köln", "Grüße aus Köln", "GRÜ", []string{"Grü"}},
		{"wide runes", "日本語の本", "日本語の本", "本", []string{"本", "本"}},
		{"cut by truncation", "Hello nee…", "Hello needle world", "needle", []string{"nee"}},
		{"cut before the match", "Hello…", "Hello needle", "needle", nil},
		{"no find", "Hello", "Hello", "", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := markFound(tt.shown, tt.full, findNeedle(tt.find), plain)
			runs, text := reversedRuns(got)
			if text != tt.shown {
				t.Errorf("the drawn text is %q, want %q unchanged", text, tt.shown)
			}
			if !slices.Equal(runs, tt.want) {
				t.Errorf("reversed %q, want %q", runs, tt.want)
			}
		})
	}
}

// styledRowLine returns the screen line, with its color, of the row that shows title.
func styledRowLine(t *testing.T, app *App, title string) string {
	t.Helper()
	for line := range strings.SplitSeq(app.View().Content, "\n") {
		if strings.Contains(ansi.Strip(line), title) {
			return line
		}
	}
	t.Fatalf("no row shows %q:\n%s", title, plainView(app))
	return ""
}

func TestFoundTextIsReversedInTheRows(t *testing.T) {
	nibs := findNibs()
	nibs[5].Title = "Other \x1b[31mneedle"
	app, _ := setupTestApp(t, nibs)
	before := plainView(app)

	findIn(app, "needle")
	pressKeys(app, "home")

	if after := plainView(app); after != before {
		t.Errorf("find changed the text on screen:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	for _, tt := range []struct {
		row  string
		want []string
	}{
		{"Needle feature", []string{"Needle"}},
		{"Plain leaf", []string{"needle"}},
		{"Other  [31mneedle", []string{"needle"}},
	} {
		if runs, _ := reversedRuns(styledRowLine(t, app, tt.row)); !slices.Equal(runs, tt.want) {
			t.Errorf("the row of %q reverses %q, want %q", tt.row, runs, tt.want)
		}
	}

	findIn(app, "n-4")
	if runs, _ := reversedRuns(styledRowLine(t, app, "Beta task")); !slices.Equal(runs, []string{"n-4"}) {
		t.Errorf("the row of n-4 reverses %q, want its ID", runs)
	}
}

func TestAPasteIntoTheFindBoxFinds(t *testing.T) {
	app, _ := setupTestApp(t, findNibs())
	pressKeys(app, "f")
	pasteText(app, "n-4")
	if got := app.list.findBox.Value(); got != "n-4" {
		t.Errorf("the find box holds %q after the paste, want n-4", got)
	}
	if got := selectedID(app); got != "n-4" {
		t.Errorf("the paste put the cursor on %q, want n-4", got)
	}
}

// A match that covers part of a grapheme cluster reverses the whole cluster,
// so no escape lands inside one and the drawn width stays the text's width.
func TestMarkFoundReversesWholeGraphemeClusters(t *testing.T) {
	const family = "👨\u200d👩\u200d👧"
	plain := lipgloss.NewStyle()
	for _, tt := range []struct {
		name, shown, find string
		want              []string
	}{
		{"one person of a family", "Family " + family + " x", "👩", []string{family}},
		{"one regional indicator", "Flag 🇸🇪 here", "🇪", []string{"🇸🇪"}},
		{"a consonant in a conjunct", "नमस्ते world", "स", []string{"स्ते"}},
		{"a letter with a combining mark", "Café au lait", "e", []string{"é"}},
		{"a whole cluster", "x 🇸🇪 y", "🇸🇪", []string{"🇸🇪"}},
		{"from mid-cluster past its end", "a" + family + "b", "👧b", []string{family + "b"}},
		{"two matches in one cluster", family, "\u200d", []string{family}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := markFound(tt.shown, tt.shown, findNeedle(tt.find), plain)
			runs, text := reversedRuns(got)
			if text != tt.shown {
				t.Errorf("the drawn text is %q, want %q unchanged", text, tt.shown)
			}
			if !slices.Equal(runs, tt.want) {
				t.Errorf("reversed %q, want %q", runs, tt.want)
			}
			if got, want := ansi.StringWidth(got), ansi.StringWidth(tt.shown); got != want {
				t.Errorf("the drawn width is %d, want %d", got, want)
			}
		})
	}
}

func TestFindInsideAClusterKeepsTheRowWidthAndItsID(t *testing.T) {
	nibs := findNibs()
	nibs[5].Title = "Family 👨\u200d👩\u200d👧 x"
	app, _ := setupTestApp(t, nibs)
	before := styledRowLine(t, app, "Family")

	findIn(app, "👩")

	after := styledRowLine(t, app, "Family")
	if got, want := ansi.StringWidth(after), ansi.StringWidth(before); got != want {
		t.Errorf("the row is %d cells wide, want %d as before the find", got, want)
	}
	if !strings.HasSuffix(strings.TrimRight(ansi.Strip(after), " "), "n-6") {
		t.Errorf("the row lost its ID column: %q", ansi.Strip(after))
	}
}

func TestAPasteThatFindsWithoutWrappingClearsTheWrapNote(t *testing.T) {
	app, _ := setupTestApp(t, findNibs())
	pressKeys(app, "end f")
	pasteText(app, "alpha")
	if got := app.list.statusMessage; got != findWrappedToFirst {
		t.Fatalf("premise failed: the first paste shows %q, want the wrap note", got)
	}
	pasteText(app, " epic")
	if got := selectedID(app); got != "n-1" {
		t.Fatalf("premise failed: the paste put the cursor on %q, want n-1", got)
	}
	if got := app.list.statusMessage; got != "" {
		t.Errorf("a paste that did not wrap left the note %q", got)
	}
}
