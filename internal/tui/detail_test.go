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
