// Package chatview provides a scrollable chat message viewport.
package chatview

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli/render"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli/theme"
)

// Role identifies the sender of a message.
type Role int

const (
	// User is the human user.
	User Role = iota
	// Assistant is the AI assistant.
	Assistant
	// System is used for local feedback messages (e.g. command confirmations).
	System
)

// Message represents a single chat message.
type Message struct {
	Role        Role
	Content     string
	Timestamp   time.Time
	preRendered bool
}

// Model is the BubbleTea model for the chat viewport.
type Model struct {
	viewport viewport.Model
	messages []Message
	theme    theme.Theme
	renderer *render.Renderer
	width    int
	height   int
}

// New creates a new chatview Model.
func New(th theme.Theme, r *render.Renderer) Model {
	vp := viewport.New()
	vp.MouseWheelEnabled = true
	vp.KeyMap = viewport.KeyMap{}
	return Model{
		viewport: vp,
		messages: nil,
		theme:    th,
		renderer: r,
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { //nolint:gocritic // BubbleTea requires value receiver pattern
	return nil
}

// Update implements tea.Model.
// Note: WindowSizeMsg is NOT handled here because the parent App manages this
// component's dimensions via SetSize with layout-constrained values. Letting
// the raw WindowSizeMsg through would override the constrained height.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) { //nolint:gocritic // BubbleTea requires value receiver pattern
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// View implements tea.Model.
func (m Model) View() string { //nolint:gocritic // BubbleTea requires value receiver pattern
	return m.viewport.View()
}

// AddMessage appends a message and refreshes the viewport content.
func (m *Model) AddMessage(msg Message) {
	m.messages = append(m.messages, msg)
	m.refreshContent()
	m.viewport.GotoBottom()
}

// SetSize sets the viewport dimensions.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.viewport.SetWidth(width)
	m.viewport.SetHeight(height)
	m.refreshContent()
}

// UpdateLastAssistant replaces the content of the last assistant message,
// used during streaming.
func (m *Model) UpdateLastAssistant(content string) {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].Role != Assistant {
			continue
		}
		m.messages[i].Content = content
		m.messages[i].preRendered = false
		m.refreshContent()
		m.viewport.GotoBottom()
		return
	}
}

// UpdateLastAssistantRendered replaces the content of the last assistant
// message with already-rendered output, skipping glamour re-rendering.
func (m *Model) UpdateLastAssistantRendered(content string) {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].Role != Assistant {
			continue
		}
		m.messages[i].Content = content
		m.messages[i].preRendered = true
		m.refreshContent()
		m.viewport.GotoBottom()
		return
	}
}

// ClearMessages removes all messages and refreshes the viewport.
func (m *Model) ClearMessages() {
	m.messages = nil
	m.refreshContent()
}

// Messages returns a copy of the current messages.
func (m Model) Messages() []Message { //nolint:gocritic // BubbleTea requires value receiver pattern
	msgs := make([]Message, len(m.messages))
	copy(msgs, m.messages)
	return msgs
}

// MessageCount returns the number of messages stored.
func (m Model) MessageCount() int { //nolint:gocritic // BubbleTea requires value receiver pattern
	return len(m.messages)
}

func (m *Model) refreshContent() {
	var b strings.Builder
	for _, msg := range m.messages {
		b.WriteString(m.renderMessage(msg))
		b.WriteString("\n")
	}
	m.viewport.SetContent(b.String())
}

func (m *Model) renderMessage(msg Message) string {
	var style lipgloss.Style
	var prefix string

	switch msg.Role {
	case User:
		style = m.theme.UserMessage()
		prefix = "You"
	case Assistant:
		style = m.theme.AssistantMessage()
		prefix = "Assistant"
	case System:
		style = m.theme.MutedText()
		prefix = "System"
	}

	header := style.Render(fmt.Sprintf("● %s", prefix))

	var content string
	switch {
	case msg.preRendered:
		content = msg.Content
	case msg.Role == Assistant && m.renderer != nil:
		rendered, err := m.renderer.Render(msg.Content)
		if err == nil {
			content = rendered
		} else {
			content = msg.Content
		}
	default:
		content = "  " + msg.Content
	}

	return header + "\n" + content
}
