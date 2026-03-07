// Package input provides a multiline text input with history support.
package input

import (
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli/theme"
)

const (
	defaultHistorySize = 100
	minHeight          = 1
	maxHeight          = 5
)

// SubmitMsg is sent when the user presses Enter to submit input.
type SubmitMsg struct {
	Value string
}

// CommandValidator checks whether a parsed command name is valid.
type CommandValidator func(name string) bool

// Model is the BubbleTea model for the text input area.
type Model struct {
	textarea         textarea.Model
	theme            theme.Theme
	history          []string
	historyIdx       int
	historySize      int
	width            int
	commandMode      bool
	commandValidator CommandValidator
}

// New creates a new input Model.
func New(th theme.Theme, historySize int) Model {
	if historySize <= 0 {
		historySize = defaultHistorySize
	}

	ta := textarea.New()
	ta.Placeholder = "Type your message..."
	ta.ShowLineNumbers = false
	ta.Prompt = "› "
	ta.SetHeight(minHeight)
	ta.CharLimit = 0

	return Model{
		textarea:    ta,
		theme:       th,
		history:     make([]string, 0, historySize),
		historyIdx:  -1,
		historySize: historySize,
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { //nolint:gocritic // BubbleTea requires value receiver pattern
	return m.textarea.Focus()
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) { //nolint:gocritic // BubbleTea requires value receiver pattern
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			value := m.textarea.Value()
			if value == "" {
				return m, nil
			}
			m.pushHistory(value)
			m.textarea.Reset()
			m.textarea.SetHeight(minHeight)
			return m, func() tea.Msg { return SubmitMsg{Value: value} }

		case "up":
			if m.textarea.Value() == "" || m.historyIdx >= 0 {
				if prev, ok := m.historyUp(); ok {
					m.textarea.Reset()
					m.textarea.InsertString(prev)
					return m, nil
				}
			}

		case "down":
			if m.historyIdx >= 0 {
				if next, ok := m.historyDown(); ok {
					m.textarea.Reset()
					m.textarea.InsertString(next)
					return m, nil
				}
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.textarea.SetWidth(msg.Width)
	}

	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	m.updateCommandMode()
	return m, cmd
}

// View implements tea.Model.
func (m Model) View() string { //nolint:gocritic // BubbleTea requires value receiver pattern
	style := lipgloss.NewStyle()
	if m.theme != nil {
		style = m.theme.InputArea()
	}
	return style.Render(m.textarea.View())
}

// CommandMode reports whether the input currently contains a slash command.
func (m Model) CommandMode() bool { //nolint:gocritic // BubbleTea requires value receiver pattern
	return m.commandMode
}

// SetWidth sets the textarea width.
func (m *Model) SetWidth(w int) {
	m.width = w
	m.textarea.SetWidth(w)
}

// Focus gives focus to the textarea.
func (m *Model) Focus() tea.Cmd {
	return m.textarea.Focus()
}

// Blur removes focus from the textarea.
func (m *Model) Blur() {
	m.textarea.Blur()
}

// Value returns the current textarea content.
func (m Model) Value() string { //nolint:gocritic // BubbleTea requires value receiver pattern
	return m.textarea.Value()
}

func (m *Model) pushHistory(value string) {
	m.history = append(m.history, value)
	if len(m.history) > m.historySize {
		m.history = m.history[1:]
	}
	m.historyIdx = -1
}

func (m *Model) historyUp() (string, bool) {
	if len(m.history) == 0 {
		return "", false
	}

	switch {
	case m.historyIdx < 0:
		m.historyIdx = len(m.history) - 1
	case m.historyIdx > 0:
		m.historyIdx--
	default:
		return "", false
	}
	return m.history[m.historyIdx], true
}

// SetCommandValidator sets the function used to check whether a parsed
// command name is a known registered command.
func (m *Model) SetCommandValidator(v CommandValidator) {
	m.commandValidator = v
}

func (m *Model) updateCommandMode() {
	isCmd := m.isValidCommand()
	if isCmd == m.commandMode {
		return
	}
	m.commandMode = isCmd
	styles := m.textarea.Styles()
	if isCmd && m.theme != nil {
		cmdStyle := m.theme.CommandText()
		styles.Focused.CursorLine = cmdStyle
		styles.Focused.Prompt = cmdStyle
	} else {
		styles.Focused.CursorLine = lipgloss.NewStyle()
		styles.Focused.Prompt = lipgloss.NewStyle()
	}
	m.textarea.SetStyles(styles)
}

func (m *Model) isValidCommand() bool {
	cmd, ok := valueobject.ParseSlashCommand(m.textarea.Value())
	if !ok {
		return false
	}
	if m.commandValidator != nil {
		return m.commandValidator(cmd.Name)
	}
	return true
}

func (m *Model) historyDown() (string, bool) {
	switch {
	case m.historyIdx < 0 || len(m.history) == 0:
		return "", false
	case m.historyIdx < len(m.history)-1:
		m.historyIdx++
		return m.history[m.historyIdx], true
	default:
		m.historyIdx = -1
		return "", true
	}
}
