package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/ui"
)

// previewModel is a read-only detail preview for the two-column layout.
// It has no focus, no interaction - just renders nib details.
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
	// Header: ID and Title
	idStyle := lipgloss.NewStyle().Foreground(ui.ColorPrimary).Bold(true)
	titleStyle := lipgloss.NewStyle().Bold(true)

	header := idStyle.Render(m.nib.ID) + "\n" + titleStyle.Render(m.nib.Title)

	// Metadata: Status, Type, Priority
	metaStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)
	meta := metaStyle.Render("Status: " + m.nib.Status + "  Type: " + m.nib.EffectiveType())
	if m.nib.Priority != "" && m.nib.Priority != "normal" {
		meta += metaStyle.Render("  Priority: " + m.nib.Priority)
	}
	if m.nib.Estimate != "" {
		meta += metaStyle.Render("  Estimate: " + m.nib.Estimate)
	}

	// Tags
	var tagsLine string
	if len(m.nib.Tags) > 0 {
		tagsLine = ui.RenderTags(m.nib.Tags)
	}

	// Documents
	docsLine := ui.RenderDocuments(m.nib.Documents)

	// Body (truncated to fit)
	body := m.renderBody()

	// Compose
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

	// Truncate content to fit within available height
	// Border takes 2 lines (top + bottom), padding takes 0 vertical
	innerHeight := m.height - 2
	contentLines := strings.Split(content, "\n")
	if len(contentLines) > innerHeight {
		contentLines = contentLines[:innerHeight]
	}
	content = strings.Join(contentLines, "\n")

	// Border - use exact height to prevent overflow
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ui.ColorMuted).
		Padding(0, 1).
		Width(withBorder(m.width - 2)).
		Height(withBorder(innerHeight))

	result := borderStyle.Render(content)

	// Ensure output is exactly m.height lines
	// When truncating, preserve the bottom border (last line)
	resultLines := strings.Split(result, "\n")
	if len(resultLines) > m.height {
		// Keep first (m.height-1) lines + the last line (bottom border)
		bottomBorder := resultLines[len(resultLines)-1]
		resultLines = resultLines[:m.height-1]
		resultLines = append(resultLines, bottomBorder)
		result = strings.Join(resultLines, "\n")
	}

	return result
}

func (m previewModel) renderBody() string {
	if m.nib.Body == "" {
		return lipgloss.NewStyle().Foreground(ui.ColorMuted).Render("No description")
	}

	// Render markdown (reuse existing glamour renderer from detail.go)
	renderer := getGlamourRenderer()
	if renderer == nil {
		return m.nib.Body
	}

	rendered, err := renderer.Render(m.nib.Body)
	if err != nil {
		return m.nib.Body
	}

	// Truncate to available height
	lines := strings.Split(rendered, "\n")
	// Account for header (2 lines), blank line, meta (1 line), tags (0-1), docs (0-1), blank line, borders/padding
	// Base ~8 lines for header/meta, +1 each for optional tags and docs lines
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
