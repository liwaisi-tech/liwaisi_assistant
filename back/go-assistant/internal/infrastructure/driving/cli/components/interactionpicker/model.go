// Package interactionpicker provides an interactive user-turn picker for /rewind.
package interactionpicker

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli/theme"
)

// Interaction represents a single user turn in the conversation.
type Interaction struct {
	Index     int
	Content   string
	Timestamp time.Time
}

// SelectedMsg is emitted when the user picks an interaction to rewind to.
type SelectedMsg struct {
	Index int
}

// CanceledMsg is emitted when the user cancels the picker.
type CanceledMsg struct{}

// Model is the BubbleTea model for the interaction picker.
type Model struct {
	interactions []Interaction
	cursor       int
	theme        theme.Theme
	width        int
	height       int
}

// New creates a new interaction picker Model.
func New(interactions []Interaction, th theme.Theme, width, height int) Model {
	return Model{
		interactions: interactions,
		theme:        th,
		width:        width,
		height:       height,
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.interactions)-1 {
				m.cursor++
			}
		case "enter":
			if len(m.interactions) > 0 {
				idx := m.interactions[m.cursor].Index
				return m, func() tea.Msg { return SelectedMsg{Index: idx} }
			}
		case "esc", "q":
			return m, func() tea.Msg { return CanceledMsg{} }
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}
	return m, nil
}

// View implements tea.Model.
func (m Model) View() string {
	if len(m.interactions) == 0 {
		return "No interactions to rewind to."
	}

	var b strings.Builder
	titleStyle := lipgloss.NewStyle().Bold(true).Padding(0, 1)
	b.WriteString(titleStyle.Render("Select a point to rewind to (↑/↓ Enter, Esc to cancel)"))
	b.WriteString("\n\n")

	for i, inter := range m.interactions {
		cursor := "  "
		if i == m.cursor {
			cursor = "▸ "
		}

		preview := truncate(inter.Content, 80)
		ts := inter.Timestamp.Format("15:04:05")
		line := fmt.Sprintf("%s[%d] %s  %s", cursor, inter.Index+1, ts, preview)

		if i == m.cursor && m.theme != nil {
			style := m.theme.CommandText().Bold(true)
			b.WriteString(style.Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}

	return b.String()
}

// SetSize updates the picker dimensions.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
