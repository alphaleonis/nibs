package tui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
)

func TestDetailHeaderWithDocuments(t *testing.T) {
	b := &nib.Nib{
		ID:        "nibs-test",
		Title:     "Nib with Docs",
		Status:    "todo",
		Type:      "task",
		Documents: []string{"docs/design.md", "README.md"},
	}

	m := detailModel{
		nib:    b,
		config: config.Default(),
		width:  80,
	}

	header := m.renderHeader()

	if !strings.Contains(header, "docs/design.md") {
		t.Error("detail header should display document path 'docs/design.md'")
	}
	if !strings.Contains(header, "README.md") {
		t.Error("detail header should display document path 'README.md'")
	}
}

func TestCalculateHeaderHeightWithDocuments(t *testing.T) {
	withDocs := detailModel{
		nib: &nib.Nib{
			ID: "nibs-test", Title: "Test", Status: "todo", Type: "task",
			Documents: []string{"docs/design.md"},
		},
		config: config.Default(),
		width:  80,
		height: 40,
	}
	withoutDocs := detailModel{
		nib: &nib.Nib{
			ID: "nibs-test", Title: "Test", Status: "todo", Type: "task",
		},
		config: config.Default(),
		width:  80,
		height: 40,
	}

	heightWith := withDocs.calculateHeaderHeight()
	heightWithout := withoutDocs.calculateHeaderHeight()

	if heightWith != heightWithout+1 {
		t.Errorf("header with documents should be 1 line taller: got %d, want %d", heightWith, heightWithout+1)
	}
}

func TestDetailHeaderWithoutDocuments(t *testing.T) {
	b := &nib.Nib{
		ID:     "nibs-test",
		Title:  "Nib without Docs",
		Status: "todo",
		Type:   "task",
	}

	m := detailModel{
		nib:    b,
		config: config.Default(),
		width:  80,
	}

	header := m.renderHeader()

	if strings.Contains(header, "Docs:") {
		t.Error("detail header should not show 'Docs:' when documents is empty")
	}
}

// Lip Gloss v2 changed what Width and Height mean on a bordered style: v1
// counted content only and let the border add two cells on top, v2 counts the
// border inside the number. Every bordered pane in the TUI is sized from a
// content width, so without compensation each one renders two columns narrower
// than the layout it was designed for. This pins the detail panes to the full
// width the layout allots them.
func TestDetailPanesFillTheirAllottedWidth(t *testing.T) {
	b := &nib.Nib{
		ID:     "nibs-test",
		Title:  "Sizing",
		Status: "todo",
		Type:   "task",
		Body:   "## Description\n\nSome body text.\n",
	}
	m := newDetailModel(b, &StubBackend{Nibs: map[string]*nib.Nib{"nibs-test": b}}, config.Default(), 120, 40)

	// The detail panes are laid out with a one-column gutter on each side.
	const want = 120 - 2

	for _, tc := range []struct {
		name    string
		content string
	}{
		{"header", m.renderHeader()},
		{"body", m.View()},
	} {
		for _, line := range strings.Split(tc.content, "\n") {
			if !strings.ContainsAny(line, "╭╰") {
				continue
			}
			if got := lipgloss.Width(line); got != want {
				t.Errorf("%s border line width = %d, want %d\n%q", tc.name, got, want, line)
			}
			break
		}
	}
}

// Glamour v2 emits the document's left margin as plain spaces before any escape
// sequence; v1 emitted style escapes first. strings.TrimSpace therefore used to
// stop at the escape and now eats the first line's indent, leaving the opening
// heading flush against the pane border while every later line stays indented.
func TestRenderBodyKeepsFirstLineIndent(t *testing.T) {
	b := &nib.Nib{
		ID: "nibs-test", Title: "Indent", Status: "todo", Type: "task",
		Body: "## Description\n\nSome body text.\n",
	}
	m := newDetailModel(b, &StubBackend{Nibs: map[string]*nib.Nib{"nibs-test": b}}, config.Default(), 120, 40)

	out := m.renderBody(100)
	lines := strings.Split(out, "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatalf("renderBody produced no first line: %q", out)
	}
	if !strings.HasPrefix(lines[0], " ") {
		t.Errorf("first rendered line lost its left margin: %q", lines[0])
	}
	if strings.HasPrefix(out, "\n") {
		t.Errorf("renderBody should still strip glamour's leading blank line: %q", out)
	}
}

// detailScrollPctRe reads the percentage off a painted frame. The footer's help
// row is the frame's last line and the percentage is the first thing on it, so
// the last match is the one the reader sees.
var detailScrollPctRe = regexp.MustCompile(`(\d+)%`)

func detailScrollPct(t *testing.T, painted string) int {
	t.Helper()
	all := detailScrollPctRe.FindAllStringSubmatch(painted, -1)
	if len(all) == 0 {
		t.Fatalf("the frame carries no scroll percentage:\n%s", painted)
	}
	pct, err := strconv.Atoi(all[len(all)-1][1])
	if err != nil {
		t.Fatalf("reading the scroll percentage: %v", err)
	}
	return pct
}

// The percentage the footer paints is what tells the reader whether there is
// body below the fold, and it has to be measured against the body the frame
// actually paints. It was measured against the viewport height Update derived
// instead — an estimate that reserves two rows more of header than renders — so
// a body entirely on screen reported 0%: the model's viewport was one row
// shorter than the painted one, which is exactly the "one line still hidden"
// arithmetic.
//
// Both directions are the property. A percentage that is always 100 would be
// just as wrong, so a body with a line genuinely off-screen has to say so.
func TestTheDetailFooterMeasuresTheBodyItPaints(t *testing.T) {
	const width = 100
	for _, height := range sweepHeights {
		for paras := 1; paras <= 14; paras++ {
			t.Run(fmt.Sprintf("%dx%d/%dp", width, height, paras), func(t *testing.T) {
				lines := make([]string, paras)
				for i := range lines {
					lines[i] = fmt.Sprintf("L%02d", i)
				}
				app, _ := setupTestApp(t, []*nib.Nib{{
					ID: "nib-1", Title: "Scrolled", Type: "task", Status: "todo", Order: "1",
					Body: strings.Join(lines, "\n\n"),
				}})
				app.Update(tea.WindowSizeMsg{Width: width, Height: height})
				sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
				if app.state != viewDetail {
					t.Fatalf("premise failed: expected the detail view, got state %d", app.state)
				}

				painted := frameText(app.View().Content, width, height)
				if !strings.Contains(painted, "L00") {
					// Below the height where a body box is drawn at all there
					// is no body on screen for a percentage to describe.
					t.Skip("no body row is painted at this geometry")
				}
				pct := detailScrollPct(t, painted)
				if strings.Contains(painted, lines[paras-1]) {
					if pct != 100 {
						t.Errorf("every body line is on screen but the footer reads %d%% — it says there is more below the fold when there is not:\n%s",
							pct, painted)
					}
					return
				}
				if pct == 100 {
					t.Errorf("the footer reads 100%% while %q is off screen:\n%s", lines[paras-1], painted)
				}
			})
		}
	}
}

// The links box has a floor no terminal height shrinks: one entry is already
// the fewest it can show, and around it go the two border rows. Its title row
// and the padding row beside it put that floor at five, which with the header
// and the footer is more rows than an eight-row terminal has — so the frame ran
// past the last row and the footer went over the edge with it.
//
// What gives is the title, not the border and not the box: the border's color
// is the only thing on screen that says whether the links pane has focus, and
// the footer advertises tab as the way to reach the box, so a frame that fits
// by dropping either is the defect again in a different shape. "Linked Nibs" is
// redundant beside a list of nibs.
//
// Heights run one at a time because the thresholds here are boundaries: the
// box's floor of five stops fitting within a row or two of where the header and
// the footer stop leaving room for it.
func TestTheLinksBoxFitsAShortTerminal(t *testing.T) {
	// Widths where a link row is wider than the box it is drawn into are in the
	// sweep on purpose: a row left to wrap makes the box taller than the height
	// it was sized to, which is the same floor by another route.
	for _, width := range []int{30, 46, 76, 80, 160} {
		for height := minSupportedHeight; height <= 16; height++ {
			t.Run(fmt.Sprintf("%dx%d", width, height), func(t *testing.T) {
				app := enterHelpView(t, detailSweepView(t), width, height)
				if got := len(app.detail.links); got != 1 {
					t.Fatalf("premise failed: the nib carries %d links, want 1", got)
				}

				content := app.View().Content
				assertFrameFitsTerminal(t, content, width, height)

				if got := blockLines(app.detail.linksBox()); got != 3 {
					t.Errorf("the links box is %d rows around one link, want 3 — its border and the entry, with no row spent on a label", got)
				}
				if painted := frameText(content, width, height); !strings.Contains(painted, "b1") {
					t.Errorf("the linked nib is not on screen at %d rows, so tab reaches a box the frame never drew:\n%s", height, painted)
				}
			})
		}
	}
}

// A body that is only blank lines renders to nothing, and since Parse now hands
// the body back verbatim it is a stable value rather than one that converges to
// empty on the next write. So the placeholder has to key off what the body
// RENDERS to, not off the empty string.
func TestAWhitespaceOnlyBodyStillSaysNoDescription(t *testing.T) {
	for _, body := range []string{"", "\n", "\n\n", "\n\n\n", "   ", "\t\n  \n"} {
		t.Run(fmt.Sprintf("%q", body), func(t *testing.T) {
			d := detailModel{width: 80, height: 24, nib: &nib.Nib{ID: "x", Title: "T", Body: body}}
			if got := d.renderBody(80); !strings.Contains(got, "No description") {
				t.Errorf("a body of %q did not get the placeholder; rendered %q", body, got)
			}
			p := previewModel{width: 80, height: 24, nib: &nib.Nib{ID: "x", Title: "T", Body: body}}
			if got := p.renderBody(); !strings.Contains(got, "No description") {
				t.Errorf("preview: a body of %q did not get the placeholder; rendered %q", body, got)
			}
		})
	}
}

// linkedNibs is a nib with n links pointing at it, so the detail view opens on
// a box whose height the caller chooses: the threshold where the frame stops
// having room for it moves with the link count.
func linkedNibs(n int) []*nib.Nib {
	nibs := []*nib.Nib{{ID: "hub", Title: "Hub", Type: "task", Status: "todo", Order: "1"}}
	for i := 0; i < n; i++ {
		nibs = append(nibs, &nib.Nib{
			ID:        fmt.Sprintf("lk%d", i),
			Title:     fmt.Sprintf("Blocker %d", i),
			Type:      "task",
			Status:    "todo",
			Order:     fmt.Sprintf("%d", i+2),
			BlockedBy: []string{"hub"},
		})
	}
	return nibs
}

// linkedNibsWithBody is linkedNibs whose hub carries a body taller than any
// viewport these tests give it, so a key that reaches the viewport moves the
// frame and one that does not shows up as a frame that did not change.
func linkedNibsWithBody(n int) []*nib.Nib {
	nibs := linkedNibs(n)
	lines := make([]string, 40)
	for i := range lines {
		lines[i] = fmt.Sprintf("L%02d", i)
	}
	nibs[0].Body = strings.Join(lines, "\n\n") // the hub, which linkedNibs puts first
	return nibs
}

// detailOnLinkedNib drives the real key path into the detail view of a nib with
// n links, at a given terminal size.
func detailOnLinkedNib(t *testing.T, n, width, height int) *App {
	t.Helper()
	return detailOnHub(t, linkedNibs(n), n, width, height)
}

// detailOnHub is detailOnLinkedNib for a hub the caller built, so a test that
// needs something under the links box can supply it.
func detailOnHub(t *testing.T, nibs []*nib.Nib, wantLinks, width, height int) *App {
	t.Helper()
	app := setupRealBackendApp(t, nibs)
	app.Update(tea.WindowSizeMsg{Width: width, Height: height})
	if !focusOn(app, "hub") {
		t.Fatal("premise failed: could not focus the linked nib")
	}
	sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.state != viewDetail {
		t.Fatalf("premise failed: expected the detail view, got state %d", app.state)
	}
	if got := len(app.detail.links); got != wantLinks {
		t.Fatalf("premise failed: %d links resolved, want %d", got, wantLinks)
	}
	return app
}

// The pane the detail view routes keys to has to be one the frame drew. It
// opens focused on the links list, and the frame drops that box whole where it
// does not fit — so at every geometry where the box goes and the body stays, j
// and k moved a list nobody could see while the visible body sat still, and
// enter jumped to a link the reader never saw. Nothing on screen says so
// either: the footer names tab only for a box it drew, and the body box's
// border is drawn muted for as long as the links list holds focus.
//
// Both directions are the property. Focus that always left the links list would
// pass the dropped half trivially, so where the box IS drawn enter still has to
// follow the link it sits on.
//
// Each geometry is reached twice, because the two halves of the answer are in
// different places: a view opened onto a frame too short for the box never sees
// a message, and a view shrunk onto one is holding focus the frame has just
// taken away.
func TestTheDetailViewFocusesAPaneTheFrameDrew(t *testing.T) {
	var linksDrawn, linksDroppedBodyDrawn int

	arrivals := []struct {
		name string
		open func(t *testing.T, links, width, height int) *App
	}{
		{"opened", func(t *testing.T, links, width, height int) *App {
			return detailOnHub(t, linkedNibsWithBody(links), links, width, height)
		}},
		{"resized", func(t *testing.T, links, width, height int) *App {
			// Tall enough that every link count here draws its box, so the
			// shrink is what takes it away.
			const tall = 24
			app := detailOnHub(t, linkedNibsWithBody(links), links, width, tall)
			if !app.detail.linksActive {
				t.Fatalf("premise failed: the view did not open on the links box at %d rows, so shrinking it takes nothing away", tall)
			}
			app.Update(tea.WindowSizeMsg{Width: width, Height: height})
			return app
		}},
	}

	for _, arrival := range arrivals {
		for _, links := range []int{2, 4, 6} {
			for _, width := range []int{30, 40, 80, 120, 200} {
				// The box is dropped and the body kept at eight rows with two
				// links and at nine with more; both are drawn from twelve up.
				// The sweep runs past that changeover rather than stopping on
				// it, so the drawn side is exercised at more than one height.
				for height := minSupportedHeight; height <= 16; height++ {
					t.Run(fmt.Sprintf("%s/%dlinks/%dx%d", arrival.name, links, width, height), func(t *testing.T) {
						app := arrival.open(t, links, width, height)
						before := frameText(app.View().Content, width, height)

						if strings.Contains(before, "lk0") {
							linksDrawn++
							sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
							if got := app.detail.nib.ID; got == "hub" {
								t.Errorf("the links box is on screen, yet enter did not follow the link it sits on:\n%s", before)
							}
							return
						}
						if !strings.Contains(before, "L00") {
							return
						}
						linksDroppedBodyDrawn++

						sendKey(app, tea.KeyPressMsg{Code: 'j', Text: "j"})
						if afterJ := frameText(app.View().Content, width, height); afterJ == before {
							t.Errorf("the links box is not on screen and the body is, yet j left the frame where it was — the keys are reaching a list no row carries:\n%s", before)
						}
						sendKey(app, tea.KeyPressMsg{Code: 'k', Text: "k"})
						if afterK := frameText(app.View().Content, width, height); afterK != before {
							t.Errorf("k did not put the visible body back where j found it:\n%s\n%s", before, afterK)
						}
						sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
						if got := app.detail.nib.ID; got != "hub" {
							t.Errorf("enter followed a link the frame never drew — the view is on %s now:\n%s", got, before)
						}
					})
				}
			}
		}
	}

	for _, c := range []struct {
		name string
		n    int
	}{
		{"geometries with the links box drawn", linksDrawn},
		{"geometries with the links box dropped and the body drawn", linksDroppedBodyDrawn},
	} {
		if c.n == 0 {
			t.Errorf("premise failed: no %s, so the sweep only saw one side of the property", c.name)
		}
	}
}

// The links list takes a keystroke only while it is on screen to take it. The
// frame drops the box whole where it does not fit, and the filter input's own
// row is enough to drop a box that fit without it — so the keypress that starts
// the filter can be the one that removes the thing being filtered, leaving an
// input no row carries and the footer still naming keys that reach nothing.
//
// The property is asserted in both directions, since each alone has a trivial
// passing shape: a filter that is never entered, or a box that is never
// dropped. Geometries are swept rather than picked because the threshold moves
// with the link count — eight rows with one link, ten with three.
func TestTheLinksFilterTakesKeysOnlyWhileItIsOnScreen(t *testing.T) {
	const width = 80
	for _, links := range []int{1, 3, 5} {
		for height := 6; height <= 16; height++ {
			t.Run(fmt.Sprintf("%dlinks/%d", links, height), func(t *testing.T) {
				app := detailOnLinkedNib(t, links, width, height)

				// Non-blocking throughout: the filter input returns a cursor
				// blink the blocking driver recurses on without end.
				sendKeyNonBlocking(app, tea.KeyPressMsg{Code: '/', Text: "/"})

				content := app.View().Content
				assertFrameFitsTerminal(t, content, width, height)
				painted := frameText(content, width, height)
				onScreen := strings.Contains(painted, "Filter")
				if onScreen && !strings.Contains(painted, "lk0") {
					t.Errorf("the filter input is drawn over links the frame does not show:\n%s", painted)
				}

				// s is on the footer in front of the reader either way.
				sendKeyNonBlocking(app, tea.KeyPressMsg{Code: 's', Text: "s"})
				if onScreen {
					if got := app.detail.linkList.FilterValue(); got != "s" {
						t.Errorf("the filter input is on screen but the key went elsewhere; filter reads %q", got)
					}
					return
				}
				if app.state != viewStatusPicker {
					t.Errorf("no filter input is on screen, yet s did not open the status picker the footer advertises (state %d) — the links list took the key with nothing on screen to show for it:\n%s",
						app.state, painted)
				}
			})
		}
	}
}

// sendKeyNonBlocking sends a key the way sendKey does, but skips a command that
// does not return — the filter input's cursor blink, which processCmd recurses
// on for as long as the input exists.
func sendKeyNonBlocking(app *App, key tea.KeyPressMsg) {
	_, cmd := app.Update(key)
	processCmdNonBlocking(app, cmd)
}

// The list the reader navigates and the copy the box paints are two models, and
// they have to agree about how many entries a page holds. bubbles charges a
// title row for a shown filter whether or not the row it renders carries
// anything, so a stored list left with the filter shown pages one entry short
// of what is drawn — and NextPage then steps over a boundary nothing on screen
// marks, past links the reader can see.
func TestTheStoredLinkListPagesTheRowsTheBoxPaints(t *testing.T) {
	const width = 80
	for _, links := range []int{1, 3, 5, 9} {
		for _, height := range heightSweep(16, 24, 40) {
			t.Run(fmt.Sprintf("%dlinks/%d", links, height), func(t *testing.T) {
				app := detailOnLinkedNib(t, links, width, height)
				rows := app.detail.linkRows()

				painted := frameText(app.View().Content, width, height)
				onScreen := 0
				for i := 0; i < links; i++ {
					if strings.Contains(painted, fmt.Sprintf("lk%d", i)) {
						onScreen++
					}
				}
				// Below the height the box's floor needs, the frame drops it
				// whole — linksSection is View's own answer to that, and
				// linkRows is a function of the link count and the terminal
				// alone, so it keeps reporting rows for a box nobody painted.
				// A page size for a list no row carries has nothing to
				// disagree with, so what is asserted at those geometries is
				// that the frame really carries no link. Which keys such a
				// list may still take is
				// TestTheDetailViewFocusesAPaneTheFrameDrew's property.
				if app.detail.linksSection() == "" {
					if onScreen != 0 {
						t.Fatalf("View draws no links box, yet %d link rows are on screen:\n%s", onScreen, painted)
					}
					return
				}
				if onScreen != rows {
					t.Fatalf("premise failed: the box paints %d links, want %d:\n%s", onScreen, rows, painted)
				}

				if got := app.detail.linkList.Paginator.PerPage; got != rows {
					t.Errorf("the stored list pages %d entries at a time while the box paints %d", got, rows)
				}
				if rows >= links {
					if got := app.detail.linkList.Paginator.TotalPages; got != 1 {
						t.Errorf("all %d links are on screen, yet the stored list holds %d pages", links, got)
					}
				}
			})
		}
	}
}

// At eight rows a refusal and the links box cannot both be drawn whatever the
// box costs: the header is four rows, the footer wraps the message across two
// above its help row, and the box's own floor is three. Ten rows for eight.
//
// The box is what gives, for the same reason the body already does — a refusal
// the reader cannot see is the defect the footer was taught to wrap for, and
// the help row is how the reader acts on it.
func TestTheLinksBoxGivesItsRowsToARefusal(t *testing.T) {
	const (
		width  = 80
		height = 8
	)
	view := detailSweepView(t)
	app := enterHelpView(t, view, width, height)
	refuseStatusChange(t, app, view)

	content := app.View().Content
	assertFrameFitsTerminal(t, content, width, height)

	painted := frameText(content, width, height)
	if !strings.Contains(painted, "Status change failed") {
		t.Errorf("the frame does not carry the refusal:\n%s", painted)
	}
	if !strings.Contains(painted, "e edit") {
		t.Errorf("the frame does not carry the help row the refusal is acted on from:\n%s", painted)
	}
}

// detailLinksBoxOnScreen reports whether the links box reached the screen. b1
// is the only link ms1 carries and its id appears nowhere else in the frame.
//
// It reads a frame already clipped to the terminal, so it answers only at or
// above detailFooterMinWidth, where the id is still inside the right edge.
func detailLinksBoxOnScreen(painted string) bool {
	return strings.Contains(painted, "b1")
}

// detailBodyBoxOnScreen reports whether the body box reached the screen. ms1
// carries no body, so the placeholder is the whole of what the box holds — and
// nothing else in the frame paints it. Clipped like the links sentinel, and
// held to the same floor.
func detailBodyBoxOnScreen(painted string) bool {
	return strings.Contains(painted, "No description")
}

// detailBoxesDrawn is which of the two droppable boxes View will paint, taken
// from the model the way View takes them.
//
// The sweep asserts against the sentinels instead, which is the answer the
// reader gets and an independent one; this is only how it checks that the frame
// is still legible enough to read them off.
func detailBoxesDrawn(t *testing.T, app *App) (links, body bool) {
	t.Helper()
	if app.state != viewDetail {
		t.Fatalf("premise failed: expected the detail view, got state %d", app.state)
	}
	d := app.detail
	avail := d.contentAvail()
	section := d.linksSection()
	return section != "", d.renderBodyBox(avail-blockLines(section)) != ""
}

// detailFooterMinWidth is the narrowest terminal the sweep's oracles can answer
// at. It is a property of them, not of the view, which draws a footer at any
// width.
//
// Everything they read is clipped: renderFooter ends with clipToWidth, and
// frameText cuts each line the way the alt screen does. The two footer pieces
// under test survive furthest, being the leftmost items of the help row —
// "100%  tab switch" is sixteen cells — while the sentinels go first, since the
// link row spends its left columns on a label before it reaches the id.
// Measured against the frame's own answer: below 24 columns
// detailLinksBoxOnScreen reports a drawn box as absent, and below 20
// detailBodyBoxOnScreen does.
//
// Sweep narrower than this and the oracles produce the exact failure the
// regression would, which is why the widths below start clear of it and the
// sweep checks the floor rather than assuming it.
const detailFooterMinWidth = 24

// detailFooterGeometries are the sizes the footer's two claims turn on: heights
// where the links box is dropped, heights where the body is dropped, and
// heights where both are drawn, at widths the painted frame can still be read
// back at. Each carries a refusal, since the rows a wrapped message takes are
// what push the frame past the thresholds.
func detailFooterGeometries() []struct{ width, height int } {
	var geos []struct{ width, height int }
	for _, width := range []int{30, 38, 46, 80, 120, 200} {
		// Up to twenty rather than sixteen: this sweep carries a refusal, whose
		// wrapped rows move the thresholds it is here for further up the range.
		for height := minSupportedHeight; height <= 20; height++ {
			geos = append(geos, struct{ width, height int }{width, height})
		}
	}
	return geos
}

// The footer describes the frame in front of the reader, not the model behind
// it. View drops the links box and then the body when the rows run out, while
// the footer asked whether links EXIST and printed a percentage unconditionally
// — so it named tab as the way to a box that is not on screen, and reported a
// position inside a body nobody painted, measured against a viewport height
// left over from the last render that had one.
//
// Both directions are the property. A footer that never names tab, and one that
// never prints a percentage, would each pass half of this trivially, so the
// sweep is required to see both pieces drawn and withheld.
func TestTheDetailFooterDescribesTheFrameItPaints(t *testing.T) {
	var linksDrawn, linksDropped, bodyDrawn, bodyDropped int

	for _, geo := range detailFooterGeometries() {
		t.Run(fmt.Sprintf("%dx%d", geo.width, geo.height), func(t *testing.T) {
			if geo.width < detailFooterMinWidth {
				t.Fatalf("premise failed: %d columns is below the width these oracles can answer at (%d)", geo.width, detailFooterMinWidth)
			}
			view := detailSweepView(t)
			app := enterHelpView(t, view, geo.width, geo.height)
			refuseStatusChange(t, app, view)

			content := app.View().Content
			assertFrameFitsTerminal(t, content, geo.width, geo.height)
			painted := frameText(content, geo.width, geo.height)

			// The help row as the reader has it, clipped to the terminal like
			// everything else in the frame. The two pieces under test are its
			// leftmost items, so above the floor a missing one means the footer
			// withheld it rather than the right edge taking it.
			row := detailHelpRow(t, app)

			// The sentinels are read off the same clipped frame, so they are
			// checked against what View will paint before they are believed: a
			// sentinel cut off the right edge reports a box that IS drawn as
			// absent, and that fails as the regression this test is here for.
			wantLinks, wantBody := detailBoxesDrawn(t, app)

			links := detailLinksBoxOnScreen(painted)
			if links != wantLinks {
				t.Fatalf("premise failed: the links sentinel says on screen = %v where View paints the box = %v — the oracle cannot answer at %d columns:\n%s",
					links, wantLinks, geo.width, painted)
			}
			if links {
				linksDrawn++
			} else {
				linksDropped++
			}
			if got := strings.Contains(row, "tab switch"); got != links {
				if links {
					t.Errorf("the links box is on screen but the footer does not name tab:\n%s\n%s", row, painted)
				} else {
					t.Errorf("the footer names tab as the way to a links box the frame did not draw:\n%s\n%s", row, painted)
				}
			}

			body := detailBodyBoxOnScreen(painted)
			if body != wantBody {
				t.Fatalf("premise failed: the body sentinel says on screen = %v where View paints the box = %v — the oracle cannot answer at %d columns:\n%s",
					body, wantBody, geo.width, painted)
			}
			if body {
				bodyDrawn++
			} else {
				bodyDropped++
			}
			if got := detailScrollPctRe.MatchString(row); got != body {
				if body {
					t.Errorf("the body is on screen but the footer reports no scroll position:\n%s\n%s", row, painted)
				} else {
					t.Errorf("the footer reports a scroll position for a body the frame did not draw:\n%s\n%s", row, painted)
				}
			}
		})
	}

	for _, c := range []struct {
		name string
		n    int
	}{
		{"geometries with the links box drawn", linksDrawn},
		{"geometries with the links box dropped", linksDropped},
		{"geometries with the body drawn", bodyDrawn},
		{"geometries with the body dropped", bodyDropped},
	} {
		if c.n == 0 {
			t.Errorf("premise failed: no %s, so the sweep only saw one side of the property", c.name)
		}
	}
}

// detailHelpRow is the frame's last row: the footer's help row as the terminal
// shows it.
//
// There is no unclipped one to read — renderFooter ends with clipToWidth, so
// the row is cut to the width before it is ever painted, and reading it back
// cannot tell a hint the footer withheld from one the right edge took. What
// keeps the two apart is detailFooterMinWidth, below which the sweeps do not go.
func detailHelpRow(t *testing.T, app *App) string {
	t.Helper()
	if app.state != viewDetail {
		t.Fatalf("premise failed: expected the detail view, got state %d", app.state)
	}
	lines := strings.Split(ansiSGR.ReplaceAllString(app.View().Content, ""), "\n")
	return lines[len(lines)-1]
}

// The four geometries the review measured, kept by name so a regression names
// the frame it broke. At thirteen rows the body is gone and a percentage was
// still printed; at sixteen it is drawn and the percentage has to be there.
func TestTheDetailFooterAtTheMeasuredGeometries(t *testing.T) {
	for _, geo := range []struct {
		width, height int
		wantBody      bool
	}{
		{38, 13, false},
		{80, 13, false},
		{30, 16, true},
		{38, 16, true},
	} {
		t.Run(fmt.Sprintf("%dx%d", geo.width, geo.height), func(t *testing.T) {
			view := detailSweepView(t)
			app := enterHelpView(t, view, geo.width, geo.height)
			refuseStatusChange(t, app, view)

			painted := frameText(app.View().Content, geo.width, geo.height)
			if got := detailBodyBoxOnScreen(painted); got != geo.wantBody {
				t.Fatalf("premise failed: body on screen = %v, want %v:\n%s", got, geo.wantBody, painted)
			}
			row := detailHelpRow(t, app)
			if got := detailScrollPctRe.MatchString(row); got != geo.wantBody {
				t.Errorf("scroll position in the footer = %v with the body on screen = %v:\n%s\n%s",
					got, geo.wantBody, row, painted)
			}
		})
	}
}

// The footer's height cannot depend on what View painted above it, and this is
// the load-bearing half of how the two are allowed to know about each other.
// contentAvail measures the region to decide whether the links box and the body
// fit, so a footer whose height moved with that decision would be measuring
// itself. Both pieces sit in the help row, which clipToWidth cuts rather than
// wraps, so it is one row whatever it carries; the status message above it is
// sized from the message, the width and the terminal height alone.
//
// Give either piece a row of its own and the sizing pass and the painting pass
// stop agreeing — the frame overruns the terminal, or gives back rows nothing
// claims. This is what fails first when that happens.
func TestTheDetailFooterRegionHeightIgnoresWhatWasPainted(t *testing.T) {
	for _, refused := range []bool{false, true} {
		for _, geo := range detailFooterGeometries() {
			name := fmt.Sprintf("%dx%d", geo.width, geo.height)
			if refused {
				name += "/refused"
			}
			t.Run(name, func(t *testing.T) {
				view := detailSweepView(t)
				app := enterHelpView(t, view, geo.width, geo.height)
				if refused {
					refuseStatusChange(t, app, view)
				}
				for _, expanded := range []bool{false, true} {
					d := app.detail
					d.helpExpanded = expanded
					base := lipgloss.Height(d.footerRegion(paintedFrame{}))
					for _, p := range []paintedFrame{
						{links: true},
						{body: true},
						{links: true, body: true},
					} {
						if got := lipgloss.Height(d.footerRegion(p)); got != base {
							t.Errorf("expanded=%v: the footer region is %d rows for %+v and %d rows for the zero value — contentAvail measures one and View paints the other",
								expanded, got, p, base)
						}
					}
				}
			})
		}
	}
}
