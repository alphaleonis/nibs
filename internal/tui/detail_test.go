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
	for _, height := range []int{12, 16, 20, 24, 30} {
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
// Heights run one at a time because the thresholds here are boundaries and the
// coarse geometry sweep steps from eight straight to twelve.
func TestTheLinksBoxFitsAShortTerminal(t *testing.T) {
	// Widths where a link row is wider than the box it is drawn into are in the
	// sweep on purpose: a row left to wrap makes the box taller than the height
	// it was sized to, which is the same floor by another route.
	for _, width := range []int{30, 46, 76, 80, 160} {
		for height := 8; height <= 16; height++ {
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

// detailOnLinkedNib drives the real key path into the detail view of a nib with
// n links, at a given terminal size.
func detailOnLinkedNib(t *testing.T, n, width, height int) *App {
	t.Helper()
	app := setupRealBackendApp(t, linkedNibs(n))
	app.Update(tea.WindowSizeMsg{Width: width, Height: height})
	if !focusOn(app, "hub") {
		t.Fatal("premise failed: could not focus the linked nib")
	}
	sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.state != viewDetail {
		t.Fatalf("premise failed: expected the detail view, got state %d", app.state)
	}
	if got := len(app.detail.links); got != n {
		t.Fatalf("premise failed: %d links resolved, want %d", got, n)
	}
	return app
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
		for _, height := range []int{12, 24, 40} {
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
