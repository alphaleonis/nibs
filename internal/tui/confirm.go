package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/alphaleonis/nibs/internal/ui"
)

// confirmDialog holds the state for an archive/delete confirmation modal.
type confirmDialog struct {
	title   string   // e.g., "Archive nib?" or "Permanently delete nib?"
	message string   // descriptive message shown in the dialog
	action  string   // "archive" or "delete"
	nibIDs  []string // IDs to operate on
}

// openConfirmMsg requests opening the confirm dialog
type openConfirmMsg struct {
	dialog confirmDialog
}

// confirmActionMsg is sent when the user confirms the action
type confirmActionMsg struct {
	nibIDs []string
	action string
}

// cancelConfirmMsg is sent when the user cancels the dialog
type cancelConfirmMsg struct{}

func (d confirmDialog) Update(msg tea.Msg) (confirmDialog, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "y", "Y":
			return d, func() tea.Msg {
				return confirmActionMsg{nibIDs: d.nibIDs, action: d.action}
			}
		case "n", "N", "esc":
			return d, func() tea.Msg {
				return cancelConfirmMsg{}
			}
		}
	}
	return d, nil
}

func (d confirmDialog) View(width, height int) string {
	modalWidth := max(40, min(60, width*50/100))

	// Title
	header := lipgloss.NewStyle().Bold(true).Render(d.title)

	// Help footer
	help := helpKeyStyle.Render("y") + " " + helpStyle.Render("confirm") + "  " +
		helpKeyStyle.Render("n/esc") + " " + helpStyle.Render("cancel")

	// Border style
	borderColor := ui.ColorPrimary
	if d.action == "delete" {
		borderColor = lipgloss.Color("#ff5555")
	}
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1).
		Width(modalWidth)

	content := fmt.Sprintf("%s\n\n%s\n\n%s", header, d.message, help)
	modal := border.Render(content)

	return modal
}

// ModalView renders the confirm dialog overlaid on the background
func (d confirmDialog) ModalView(bgView string, width, height int) string {
	modal := d.View(width, height)
	return overlayModal(bgView, modal, width, height)
}

// buildConfirmDialog creates a confirmDialog for archiving or deleting nibs.
// nibTitle is the display title for a single nib. descendantCount is the number
// of descendants (children, grandchildren, etc.) — 0 means just the one nib.
func buildConfirmDialog(action string, nibTitle string, nibIDs []string, descendantCount int) confirmDialog {
	total := len(nibIDs)
	var title, message string

	if action == "archive" {
		if total == 1 && descendantCount == 0 {
			title = "Archive nib?"
			message = fmt.Sprintf("Archive \"%s\"?", truncateTitle(nibTitle, 40))
		} else {
			title = "Archive nibs?"
			message = fmt.Sprintf("%d nibs will be archived (including children).", total)
		}
	} else {
		// delete
		if total == 1 && descendantCount == 0 {
			title = "Permanently delete nib?"
			message = fmt.Sprintf("Permanently delete \"%s\"? This cannot be undone.", truncateTitle(nibTitle, 40))
		} else {
			title = "Permanently delete nibs?"
			message = fmt.Sprintf("%d nibs will be permanently deleted (including children). This cannot be undone.", total)
		}
	}

	return confirmDialog{
		title:   title,
		message: message,
		action:  action,
		nibIDs:  nibIDs,
	}
}

func truncateTitle(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}

// gatherNibAndDescendants returns the nib ID plus all its descendant IDs.
func gatherNibAndDescendants(backend Backend, nibID string) []string {
	allNibs, err := backend.ListNibs(context.Background(), nil)
	if err != nil {
		return []string{nibID}
	}

	descendants := collectDescendants(nibID, allNibs)
	ids := make([]string, 0, 1+len(descendants))
	ids = append(ids, nibID)
	for id := range descendants {
		ids = append(ids, id)
	}
	return ids
}
