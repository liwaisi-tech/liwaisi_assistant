// Package sessionpicker provides an interactive session browser for /resume.
package sessionpicker

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/session"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli/theme"
)

// SessionSelectedMsg is emitted when the user picks a session.
type SessionSelectedMsg struct {
	SessionID string
}

// CanceledMsg is emitted when the user cancels the picker.
type CanceledMsg struct{}

// Model is the BubbleTea model for the session picker.
type Model struct {
	sessions []session.Summary
	cursor   int
	theme    theme.Theme
	width    int
	height   int
}

// New creates a new session picker Model.
func New(sessions []session.Summary, th theme.Theme, width, height int) Model {
	return Model{
		sessions: sessions,
		theme:    th,
		width:    width,
		height:   height,
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
			if m.cursor < len(m.sessions)-1 {
				m.cursor++
			}
		case "enter":
			if len(m.sessions) > 0 {
				return m, func() tea.Msg {
					return SessionSelectedMsg{SessionID: m.sessions[m.cursor].SessionID}
				}
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
	if len(m.sessions) == 0 {
		return "No sessions found."
	}

	var b strings.Builder
	titleStyle := lipgloss.NewStyle().Bold(true).Padding(0, 1)
	b.WriteString(titleStyle.Render("Select a session to resume (↑/↓ Enter, Esc to cancel)"))
	b.WriteString("\n\n")

	for i, s := range m.sessions {
		cursor := "  "
		if i == m.cursor {
			cursor = "▸ "
		}

		age := formatAge(s.LastActivity)
		preview := s.FirstUserMsg
		if preview == "" {
			preview = "(empty session)"
		}

		line := fmt.Sprintf("%s%-20s  %s  %d msgs  %s",
			cursor, s.SessionID, age, s.MessageCount, preview)

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

func formatAge(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
