package tui

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/ui"
)

// nibCreatedMsg is sent when a new nib is created
type nibCreatedMsg struct {
	title   string
	nibType string
}

// closeCreateModalMsg is sent when the create modal is canceled
type closeCreateModalMsg struct{}

// createModalModel is the model for the create nib modal
type createModalModel struct {
	textInput textinput.Model
	nibType   string // chosen type from create flow type picker
	typeColor string // color for the type from config
	width     int
	height    int
}

func newCreateModalModel(nibType string, cfg *config.Config, width, height int) createModalModel {
	ti := textinput.New()
	ti.Placeholder = "Enter nib title..."
	ti.CharLimit = 200
	ti.SetWidth(50)
	ti.Focus()
	// The modal is drawn on the dark palette the whole TUI assumes, so start
	// from the dark defaults rather than probing the terminal background.
	tiStyles := textinput.DefaultDarkStyles()
	tiStyles.Focused.Prompt = lipgloss.NewStyle().Foreground(ui.ColorPrimary)
	tiStyles.Focused.Text = lipgloss.NewStyle()
	tiStyles.Focused.Placeholder = lipgloss.NewStyle().Foreground(ui.ColorMuted)
	ti.SetStyles(tiStyles)
	ti.Prompt = ""

	var typeColor string
	if tc := cfg.GetType(nibType); tc != nil {
		typeColor = tc.Color
	}

	return createModalModel{
		textInput: ti,
		nibType:   nibType,
		typeColor: typeColor,
		width:     width,
		height:    height,
	}
}

func (m createModalModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m createModalModel) Update(msg tea.Msg) (createModalModel, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyPressMsg:
		switch msg.Code {
		case tea.KeyEnter:
			title := m.textInput.Value()
			if title != "" {
				nibType := m.nibType
				return m, func() tea.Msg {
					return nibCreatedMsg{title: title, nibType: nibType}
				}
			}
			// Empty title - just close
			return m, func() tea.Msg {
				return closeCreateModalMsg{}
			}

		case tea.KeyEsc:
			return m, func() tea.Msg {
				return closeCreateModalMsg{}
			}
		}
	}

	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m createModalModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	modalWidth := max(40, min(60, m.width*50/100))

	// Header
	header := lipgloss.NewStyle().Bold(true).Render("Create New ") + ui.RenderTypeText(m.nibType, m.typeColor)

	// Input field
	inputBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ui.ColorMuted).
		Padding(0, 1).
		Width(withBorder(modalWidth - 6)).
		Render(m.textInput.View())

	// Help text
	help := helpKeyStyle.Render("enter") + " " + helpStyle.Render("create") + "  " +
		helpKeyStyle.Render("esc") + " " + helpStyle.Render("cancel")

	// Assemble content
	content := header + "\n\n" + inputBox + "\n\n" + help

	// Border style
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ui.ColorPrimary).
		Padding(1, 2).
		Width(withBorder(modalWidth))

	return border.Render(content)
}

// ModalView returns the modal rendered as a centered overlay
func (m createModalModel) ModalView(bgView string, fullWidth, fullHeight int) string {
	modal := m.View()
	return overlayModal(bgView, modal, fullWidth, fullHeight)
}
