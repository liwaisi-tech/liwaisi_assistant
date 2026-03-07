// Package statusline provides a real-time activity indicator for the liwaisi CLI.
package statusline

import (
	"fmt"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli/theme"
)

// Activity describes the current operation being displayed.
type Activity struct {
	Kind      valueobject.ToolEventKind
	AgentName string
	Label     string
	Iteration int
	Total     int
	Elapsed   time.Duration
}

// Model is the BubbleTea model for the StatusLine component.
// It replaces the simple spinner with structured activity rendering
// that shows sub-agent identity, tool names, progress, and elapsed time.
type Model struct {
	spinner  spinner.Model
	theme    theme.Theme
	palette  Palette
	activity Activity
	visible  bool
}

// New creates a new StatusLine Model.
func New(th theme.Theme) Model {
	s := spinner.New(spinner.WithSpinner(spinner.Dot))
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFD700"))

	return Model{
		spinner: s,
		theme:   th,
		palette: NewPalette(),
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { //nolint:gocritic // BubbleTea requires value receiver pattern
	return m.spinner.Tick
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) { //nolint:gocritic // BubbleTea requires value receiver pattern
	if !m.visible {
		return m, nil
	}

	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

// View implements tea.Model.
func (m Model) View() string { //nolint:gocritic // BubbleTea requires value receiver pattern
	if !m.visible {
		return ""
	}

	muted := lipgloss.NewStyle()
	if m.theme != nil {
		muted = m.theme.MutedText()
	}

	label := m.formatActivity(muted)
	return fmt.Sprintf("%s %s", m.spinner.View(), label)
}

// Show makes the StatusLine visible and returns a spinner tick command.
func (m *Model) Show() tea.Cmd {
	m.activity = Activity{}
	m.visible = true
	return m.spinner.Tick
}

// Hide makes the StatusLine invisible.
func (m *Model) Hide() {
	m.visible = false
}

// Visible returns whether the StatusLine is currently displayed.
func (m Model) Visible() bool { //nolint:gocritic // BubbleTea requires value receiver pattern
	return m.visible
}

// SetActivity updates the displayed activity.
func (m *Model) SetActivity(a Activity) {
	m.activity = a
}

// ClearActivity resets the activity to its zero value.
func (m *Model) ClearActivity() {
	m.activity = Activity{}
}

// formatActivity renders the activity label with appropriate coloring.
func (m Model) formatActivity(muted lipgloss.Style) string { //nolint:gocritic // consistent with BubbleTea patterns
	a := m.activity

	switch a.Kind {
	case valueobject.ToolEventThinking:
		return muted.Render(fmt.Sprintf("Thinking... [%d/%d] (%s)",
			a.Iteration, a.Total, a.Elapsed))

	case valueobject.ToolEventCalling:
		return muted.Render(fmt.Sprintf("Running %s [%d/%d] (%s)",
			a.Label, a.Iteration, a.Total, a.Elapsed))

	case valueobject.ToolEventToolLoaded:
		return muted.Render(fmt.Sprintf("tools loaded: %s", a.Label))

	case valueobject.ToolEventSkillActivated:
		return muted.Render(fmt.Sprintf("skill activated: %s", a.Label))

	case valueobject.ToolEventSubAgentStarted:
		agentStyle := m.palette.ColorFor(a.AgentName)
		return fmt.Sprintf("%s %s",
			agentStyle.Render("@"+a.AgentName),
			muted.Render("started"))

	case valueobject.ToolEventSubAgentThinking:
		agentStyle := m.palette.ColorFor(a.AgentName)
		return fmt.Sprintf("%s %s",
			agentStyle.Render("@"+a.AgentName),
			muted.Render(fmt.Sprintf("thinking [%d/%d] (%s)", a.Iteration, a.Total, a.Elapsed)))

	case valueobject.ToolEventSubAgentToolCall:
		agentStyle := m.palette.ColorFor(a.AgentName)
		return fmt.Sprintf("%s %s",
			agentStyle.Render("@"+a.AgentName),
			muted.Render(fmt.Sprintf("%s [%d/%d] (%s)", a.Label, a.Iteration, a.Total, a.Elapsed)))

	case valueobject.ToolEventSubAgentCompleted:
		agentStyle := m.palette.ColorFor(a.AgentName)
		status := "completed"
		if a.Label != "" {
			status = a.Label
		}
		return fmt.Sprintf("%s %s",
			agentStyle.Render("@"+a.AgentName),
			muted.Render(fmt.Sprintf("%s (%s)", status, a.Elapsed)))

	default:
		return muted.Render(fmt.Sprintf("Working... [%d/%d] (%s)",
			a.Iteration, a.Total, a.Elapsed))
	}
}
