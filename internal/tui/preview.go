package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/ui"
)

// previewModel is the read-only nib preview in the two-column layout.
type previewModel struct {
	nib    *nib.Nib
	width  int
	height int
}

func newPreviewModel(b *nib.Nib, width, height int) previewModel {
	return previewModel{
		nib:    b,
		width:  width,
		height: height,
	}
}

func (m previewModel) View() string {
	if m.nib == nil {
		return m.renderEmpty()
	}
	return m.renderNib()
}

func (m previewModel) renderEmpty() string {
	style := lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		Align(lipgloss.Center, lipgloss.Center).
		Foreground(ui.ColorMuted)

	return style.Render("No nib selected")
}

func (m previewModel) renderNib() string {
	idStyle := lipgloss.NewStyle().Foreground(ui.ColorPrimary).Bold(true)
	titleStyle := lipgloss.NewStyle().Bold(true)

	header := idStyle.Render(m.nib.ID) + "\n" + titleStyle.Render(m.nib.Title)

	metaStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)
	meta := metaStyle.Render("Status: " + m.nib.Status + "  Type: " + m.nib.EffectiveType())
	if m.nib.Priority != "" && m.nib.Priority != "normal" {
		meta += metaStyle.Render("  Priority: " + m.nib.Priority)
	}
	if m.nib.Estimate != "" {
		meta += metaStyle.Render("  Estimate: " + m.nib.Estimate)
	}

	var tagsLine string
	if len(m.nib.Tags) > 0 {
		tagsLine = ui.RenderTags(m.nib.Tags)
	}

	docsLine := ui.RenderDocuments(m.nib.Documents)

	body := m.renderBody()

	var parts []string
	parts = append(parts, header)
	parts = append(parts, "")
	parts = append(parts, meta)
	if tagsLine != "" {
		parts = append(parts, tagsLine)
	}
	if docsLine != "" {
		parts = append(parts, docsLine)
	}
	parts = append(parts, "")
	parts = append(parts, body)

	content := lipgloss.JoinVertical(lipgloss.Left, parts...)

	// The border takes two rows.
	innerHeight := m.height - 2
	contentLines := strings.Split(content, "\n")
	if len(contentLines) > innerHeight {
		contentLines = contentLines[:innerHeight]
	}
	content = strings.Join(contentLines, "\n")

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ui.ColorMuted).
		Padding(0, 1).
		Width(withBorder(m.width - 2)).
		Height(withBorder(innerHeight))

	result := borderStyle.Render(content)

	// Cut to m.height lines, keeping the bottom border.
	resultLines := strings.Split(result, "\n")
	if len(resultLines) > m.height {
		bottomBorder := resultLines[len(resultLines)-1]
		resultLines = resultLines[:m.height-1]
		resultLines = append(resultLines, bottomBorder)
		result = strings.Join(resultLines, "\n")
	}

	return result
}

func (m previewModel) renderBody() string {
	// TrimSpace: a body of only blank lines survives parsing and renders to
	// nothing.
	if strings.TrimSpace(m.nib.Body) == "" {
		return lipgloss.NewStyle().Foreground(ui.ColorMuted).Render("No description")
	}

	// The border and its one-cell padding take four columns.
	renderer := getGlamourRenderer(m.width - 4)
	if renderer == nil {
		return m.nib.Body
	}

	rendered, err := renderer.Render(m.nib.Body)
	if err != nil {
		return m.nib.Body
	}

	lines := strings.Split(rendered, "\n")
	// About 8 rows go to the header, meta line, blank lines and border, plus one
	// each for the optional tags and documents lines.
	headerLines := 8
	if len(m.nib.Tags) > 0 {
		headerLines++
	}
	if len(m.nib.Documents) > 0 {
		headerLines++
	}
	availableLines := m.height - headerLines
	if availableLines < 1 {
		availableLines = 1
	}

	if len(lines) > availableLines {
		lines = lines[:availableLines]
		lines = append(lines, lipgloss.NewStyle().Foreground(ui.ColorMuted).Render("..."))
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}
