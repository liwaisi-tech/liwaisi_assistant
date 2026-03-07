// Package statusbar provides a fixed bottom status bar.
package statusbar

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli/theme"
)

// Model is the BubbleTea model for the status bar.
type Model struct {
	theme     theme.Theme
	width     int
	model     string
	sessionID string
	tokens    int
	latencyMS int64
}

// New creates a new statusbar Model.
func New(th theme.Theme) Model {
	return Model{
		theme:     th,
		model:     "none",
		sessionID: "—",
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if msg, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = msg.Width
	}
	return m, nil
}

// View implements tea.Model.
func (m Model) View() string {
	style := lipgloss.NewStyle()
	if m.theme != nil {
		style = m.theme.StatusBar()
	}

	left := fmt.Sprintf(" %s | %s", m.model, m.sessionID)

	var right string
	if m.tokens > 0 {
		right = fmt.Sprintf("tokens: %d | %dms ", m.tokens, m.latencyMS)
	} else {
		right = " ready "
	}

	hPadding := style.GetHorizontalPadding()
	contentWidth := m.width - hPadding
	gap := contentWidth - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}
	bar := left + strings.Repeat(" ", gap) + right

	return style.Width(m.width).Render(bar)
}

// SetModel updates the displayed model name.
func (m *Model) SetModel(name string) {
	m.model = name
}

// SetSessionID updates the displayed session identifier.
func (m *Model) SetSessionID(id string) {
	m.sessionID = id
}

// SetTokens updates the displayed token count.
func (m *Model) SetTokens(n int) {
	m.tokens = n
}

// SetLatency updates the displayed latency in milliseconds.
func (m *Model) SetLatency(ms int64) {
	m.latencyMS = ms
}

// SetWidth sets the status bar width.
func (m *Model) SetWidth(w int) {
	m.width = w
}
