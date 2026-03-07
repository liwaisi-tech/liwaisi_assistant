// Package cli provides the root BubbleTea model for the liwaisi interactive TUI.
package cli

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/memory"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/session"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/input"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli/components/chatview"
	inputcomp "github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli/components/input"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli/components/interactionpicker"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli/components/sessionpicker"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli/components/statusbar"
	statuslinecomp "github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli/components/statusline"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli/render"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli/theme"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/version"
)

type appState int

const (
	stateReady appState = iota
	stateWaiting
	stateStreaming
	stateResumePicker
	stateRewindPicker
)

const (
	headerHeight     = 1
	statusBarHeight  = 1
	statusLineHeight = 1
	inputHeight      = 3
)

// App is the root BubbleTea model that composes all sub-components.
type App struct {
	chatview          chatview.Model
	input             inputcomp.Model
	statusline        statuslinecomp.Model
	statusbar         statusbar.Model
	theme             theme.Theme
	renderer          *render.Renderer
	streamRenderer    *render.StreamRenderer
	agentSvc          input.AgentService
	memory            *memory.ConversationMemory
	sessionStore      *session.Store
	commandRegistry   *CommandRegistry
	sessionPicker     sessionpicker.Model
	interactionPicker interactionpicker.Model
	sessionID         string
	cancelStream      context.CancelFunc
	state             appState
	width             int
	height            int
	quitting          bool
}

// AppConfig holds configuration for creating a new App.
type AppConfig struct {
	Theme           theme.Theme
	HistorySize     int
	AgentSvc        input.AgentService
	Memory          *memory.ConversationMemory
	SessionStore    *session.Store
	ModelName       string
	ResumeSessionID string
}

// NewApp creates a new App with the given configuration and agent service.
func NewApp(cfg *AppConfig) (*App, error) {
	r, err := render.NewRenderer(cfg.Theme.GlamourStyle(), 80)
	if err != nil {
		return nil, fmt.Errorf("creating renderer: %w", err)
	}

	var sessionID string
	var resumeMessages []entity.Message

	if cfg.ResumeSessionID != "" && cfg.SessionStore != nil {
		sid := cfg.ResumeSessionID
		if sid == "latest" {
			summaries, err := cfg.SessionStore.ListSessions()
			if err == nil && len(summaries) > 0 {
				sid = summaries[0].SessionID
			}
		}
		if sid != "" && sid != "latest" {
			msgs, err := cfg.SessionStore.LoadSession(sid)
			if err == nil {
				sessionID = sid
				resumeMessages = msgs
			} else {
				slog.Warn("loading session for resume", "session_id", sid, "error", err)
			}
		}
	}
	if sessionID == "" {
		sessionID = fmt.Sprintf("chat-%d", time.Now().UnixNano())
	}

	sb := statusbar.New(cfg.Theme)
	if cfg.ModelName != "" {
		sb.SetModel(cfg.ModelName)
	}
	sb.SetSessionID(sessionID)

	app := &App{
		chatview:        chatview.New(cfg.Theme, r),
		input:           inputcomp.New(cfg.Theme, cfg.HistorySize),
		statusline:      statuslinecomp.New(cfg.Theme),
		statusbar:       sb,
		theme:           cfg.Theme,
		renderer:        r,
		streamRenderer:  render.NewStreamRenderer(r),
		agentSvc:        cfg.AgentSvc,
		memory:          cfg.Memory,
		sessionStore:    cfg.SessionStore,
		commandRegistry: NewCommandRegistry(),
		sessionID:       sessionID,
		state:           stateReady,
	}

	if app.sessionStore != nil {
		if len(resumeMessages) == 0 {
			if err := app.sessionStore.CreateSession(sessionID, cfg.ModelName); err != nil {
				slog.Warn("creating session file", "error", err)
			}
		}

		if app.memory != nil {
			app.memory.SetOnAppend(func(sid string, msg *entity.Message) {
				if err := app.sessionStore.AppendMessage(sid, msg); err != nil {
					slog.Warn("persisting message to session file", "error", err)
				}
			})
		}
	}

	if len(resumeMessages) > 0 {
		app.restoreSession(resumeMessages)
	}

	app.registerBuiltinCommands()
	app.input.SetCommandValidator(app.commandRegistry.Has)

	return app, nil
}

// Init implements tea.Model.
func (a *App) Init() tea.Cmd {
	return tea.Batch(
		a.input.Focus(),
		a.statusline.Init(),
	)
}

// Update implements tea.Model.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return a.handleResize(msg)

	case tea.KeyPressMsg:
		return a.handleKey(msg)

	case inputcomp.SubmitMsg:
		return a.handleSubmit(msg)

	case agentStreamStartMsg:
		return a.handleStreamStart(msg)

	case agentToolEventMsg:
		return a.handleToolEvent(&msg)

	case agentTokenMsg:
		return a.handleToken(msg)

	case agentStreamDoneMsg:
		return a.handleStreamDone()

	case agentErrorMsg:
		return a.handleAgentError(msg)

	case sessionpicker.SessionSelectedMsg:
		return a.handleSessionSelected(msg)

	case sessionpicker.CanceledMsg:
		return a.handlePickerCancelled()

	case interactionpicker.SelectedMsg:
		return a.handleInteractionSelected(msg)

	case interactionpicker.CanceledMsg:
		return a.handlePickerCancelled()

	case render.StreamRenderedMsg:
		a.chatview.UpdateLastAssistantRendered(msg.Rendered)
		return a, nil
	}

	return a.updateComponents(msg)
}

// View implements tea.Model.
func (a *App) View() tea.View {
	if a.quitting {
		return tea.NewView("Goodbye!\n")
	}

	sections := []string{
		a.renderHeader(),
	}

	switch a.state {
	case stateResumePicker:
		sections = append(sections, a.sessionPicker.View())
	case stateRewindPicker:
		sections = append(sections, a.interactionPicker.View())
	default:
		sections = append(sections, a.chatview.View())
		if a.statusline.Visible() {
			sections = append(sections, a.statusline.View())
		}
		sections = append(sections, a.input.View())
	}

	sections = append(sections, a.statusbar.View())

	v := tea.NewView(strings.Join(sections, "\n"))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (a *App) handleResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	a.width = msg.Width
	a.height = msg.Height

	if err := a.renderer.SetWidth(msg.Width); err != nil {
		slog.Error("updating renderer width", "error", err)
	}

	chatHeight := a.height - headerHeight - statusBarHeight - inputHeight
	if a.statusline.Visible() {
		chatHeight -= statusLineHeight
	}
	if chatHeight < 1 {
		chatHeight = 1
	}

	a.chatview.SetSize(msg.Width, chatHeight)
	a.input.SetWidth(msg.Width)
	a.statusbar.SetWidth(msg.Width)

	return a.updateComponents(msg)
}

func (a *App) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if a.state == stateResumePicker {
		var cmd tea.Cmd
		a.sessionPicker, cmd = a.sessionPicker.Update(msg)
		return a, cmd
	}
	if a.state == stateRewindPicker {
		var cmd tea.Cmd
		a.interactionPicker, cmd = a.interactionPicker.Update(msg)
		return a, cmd
	}

	switch msg.String() {
	case "ctrl+c":
		if a.state == stateStreaming {
			if a.cancelStream != nil {
				a.cancelStream()
			}
			return a, nil
		}
		if a.state == stateWaiting {
			if a.cancelStream != nil {
				a.cancelStream()
			}
			a.statusline.Hide()
			a.state = stateReady
			return a, a.input.Focus()
		}
		a.quitting = true
		return a, tea.Quit

	case "ctrl+d", "esc":
		a.quitting = true
		return a, tea.Quit
	}

	return a.updateComponents(msg)
}

func (a *App) handleSubmit(msg inputcomp.SubmitMsg) (tea.Model, tea.Cmd) {
	if parsed, ok := valueobject.ParseSlashCommand(msg.Value); ok {
		return a.handleCommand(parsed)
	}

	slog.Debug("user message submitted",
		"session_id", a.sessionID,
		"message_length", len(msg.Value),
	)

	a.chatview.AddMessage(chatview.Message{
		Role:      chatview.User,
		Content:   msg.Value,
		Timestamp: time.Now(),
	})

	a.state = stateWaiting
	a.input.Blur()
	cmd := a.statusline.Show()

	ctx, cancel := context.WithCancel(context.Background())
	a.cancelStream = cancel

	agentCmd := a.sendToAgent(ctx, msg.Value)
	return a, tea.Batch(cmd, agentCmd)
}

func (a *App) handleCommand(cmd valueobject.SlashCommand) (tea.Model, tea.Cmd) {
	slog.Debug("slash command received",
		"command", cmd.Name,
		"args", cmd.Args,
		"session_id", a.sessionID,
	)

	teaCmd, found := a.commandRegistry.Execute(cmd.Name, cmd.Args)
	if !found {
		a.chatview.AddMessage(chatview.Message{
			Role:      chatview.System,
			Content:   fmt.Sprintf("Unknown command: /%s. Type /help for available commands.", cmd.Name),
			Timestamp: time.Now(),
		})
		return a, nil
	}

	return a, teaCmd
}

func (a *App) handleSessionSelected(msg sessionpicker.SessionSelectedMsg) (tea.Model, tea.Cmd) {
	slog.Debug("session selected for resume", "session_id", msg.SessionID)

	if a.sessionStore == nil {
		a.state = stateReady
		return a, a.input.Focus()
	}

	msgs, err := a.sessionStore.LoadSession(msg.SessionID)
	if err != nil {
		slog.Error("loading selected session", "error", err, "session_id", msg.SessionID)
		a.state = stateReady
		a.chatview.AddMessage(chatview.Message{
			Role:      chatview.System,
			Content:   fmt.Sprintf("Failed to load session: %v", err),
			Timestamp: time.Now(),
		})
		return a, a.input.Focus()
	}

	a.sessionID = msg.SessionID
	a.statusbar.SetSessionID(msg.SessionID)
	a.chatview.ClearMessages()
	a.restoreSession(msgs)
	a.state = stateReady
	return a, a.input.Focus()
}

func (a *App) handleInteractionSelected(msg interactionpicker.SelectedMsg) (tea.Model, tea.Cmd) {
	slog.Debug("interaction selected for rewind", "index", msg.Index, "session_id", a.sessionID)

	if a.memory != nil {
		a.memory.Rewind(a.sessionID, msg.Index)
	}

	if a.sessionStore != nil {
		if err := a.sessionStore.AppendRewind(a.sessionID, msg.Index); err != nil {
			slog.Warn("persisting rewind event", "error", err)
		}
	}

	a.chatview.ClearMessages()
	if a.memory != nil {
		a.renderMessagesToChat(a.memory.History(a.sessionID))
	}

	a.chatview.AddMessage(chatview.Message{
		Role:      chatview.System,
		Content:   fmt.Sprintf("Conversation rewound to turn %d.", msg.Index),
		Timestamp: time.Now(),
	})

	a.state = stateReady
	return a, a.input.Focus()
}

func (a *App) handlePickerCancelled() (tea.Model, tea.Cmd) {
	a.state = stateReady
	a.chatview.AddMessage(chatview.Message{
		Role:      chatview.System,
		Content:   "Selection canceled.",
		Timestamp: time.Now(),
	})
	return a, a.input.Focus()
}

func (a *App) restoreSession(msgs []entity.Message) {
	if a.memory != nil {
		a.memory.GetOrCreate(a.sessionID, "")
		a.memory.ReplaceMessages(a.sessionID, msgs)
	}

	a.renderMessagesToChat(msgs)

	a.chatview.AddMessage(chatview.Message{
		Role:      chatview.System,
		Content:   fmt.Sprintf("Session resumed: %s", a.sessionID),
		Timestamp: time.Now(),
	})
}

func (a *App) renderMessagesToChat(msgs []entity.Message) {
	for _, msg := range msgs {
		var role chatview.Role
		switch msg.Role {
		case valueobject.RoleUser:
			role = chatview.User
		case valueobject.RoleAssistant:
			if msg.Content == "" {
				continue
			}
			role = chatview.Assistant
		default:
			continue
		}
		a.chatview.AddMessage(chatview.Message{
			Role:      role,
			Content:   msg.Content,
			Timestamp: msg.Timestamp,
		})
	}
}

func (a *App) registerBuiltinCommands() {
	a.commandRegistry.Register(CommandEntry{
		Name:        "help",
		Description: "Show available slash commands",
		Handler: func(_ string) tea.Cmd {
			var b strings.Builder
			b.WriteString("Available commands:\n")
			for _, entry := range a.commandRegistry.Entries() {
				b.WriteString(fmt.Sprintf("  /%s — %s\n", entry.Name, entry.Description))
			}
			a.chatview.AddMessage(chatview.Message{
				Role:      chatview.System,
				Content:   b.String(),
				Timestamp: time.Now(),
			})
			return nil
		},
	})

	a.commandRegistry.Register(CommandEntry{
		Name:        "rewind",
		Description: "Rewind the conversation to a previous user turn",
		Handler: func(_ string) tea.Cmd {
			if a.memory == nil {
				a.chatview.AddMessage(chatview.Message{
					Role:      chatview.System,
					Content:   "Memory is not available for rewind.",
					Timestamp: time.Now(),
				})
				return nil
			}

			msgs := a.memory.History(a.sessionID)
			var interactions []interactionpicker.Interaction
			for i, m := range msgs {
				if m.Role == valueobject.RoleUser {
					interactions = append(interactions, interactionpicker.Interaction{
						Index:     i,
						Content:   m.Content,
						Timestamp: m.Timestamp,
					})
				}
			}

			if len(interactions) == 0 {
				a.chatview.AddMessage(chatview.Message{
					Role:      chatview.System,
					Content:   "No user interactions to rewind to.",
					Timestamp: time.Now(),
				})
				return nil
			}

			a.interactionPicker = interactionpicker.New(interactions, a.theme, a.width, a.height)
			a.state = stateRewindPicker
			a.input.Blur()
			return nil
		},
	})

	a.commandRegistry.Register(CommandEntry{
		Name:        "resume",
		Description: "Resume a previous conversation session",
		Handler: func(_ string) tea.Cmd {
			if a.sessionStore == nil {
				a.chatview.AddMessage(chatview.Message{
					Role:      chatview.System,
					Content:   "Session persistence is not configured.",
					Timestamp: time.Now(),
				})
				return nil
			}

			summaries, err := a.sessionStore.ListSessions()
			if err != nil {
				a.chatview.AddMessage(chatview.Message{
					Role:      chatview.System,
					Content:   fmt.Sprintf("Error listing sessions: %v", err),
					Timestamp: time.Now(),
				})
				return nil
			}

			if len(summaries) == 0 {
				a.chatview.AddMessage(chatview.Message{
					Role:      chatview.System,
					Content:   "No previous sessions found.",
					Timestamp: time.Now(),
				})
				return nil
			}

			a.sessionPicker = sessionpicker.New(summaries, a.theme, a.width, a.height)
			a.state = stateResumePicker
			a.input.Blur()
			return nil
		},
	})

	a.commandRegistry.Register(CommandEntry{
		Name:        "clear",
		Description: "Clear the current conversation",
		Handler: func(_ string) tea.Cmd {
			if a.memory != nil {
				a.memory.Clear(a.sessionID)
			}
			a.chatview.ClearMessages()
			if a.sessionStore != nil {
				if err := a.sessionStore.AppendClear(a.sessionID); err != nil {
					slog.Warn("persisting clear event", "error", err)
				}
			}
			a.chatview.AddMessage(chatview.Message{
				Role:      chatview.System,
				Content:   "Conversation cleared.",
				Timestamp: time.Now(),
			})
			return nil
		},
	})
}

func (a *App) handleStreamStart(msg agentStreamStartMsg) (tea.Model, tea.Cmd) {
	slog.Debug("agent stream started", "session_id", a.sessionID)

	cmd := a.readStream(msg.stream)
	return a, cmd
}

func (a *App) handleToolEvent(msg *agentToolEventMsg) (tea.Model, tea.Cmd) {
	elapsed := msg.event.Elapsed.Truncate(time.Second)

	activity := statuslinecomp.Activity{
		Kind:      msg.event.Kind,
		Iteration: msg.event.Iteration,
		Total:     msg.event.Total,
		Elapsed:   elapsed,
	}

	switch msg.event.Kind {
	case valueobject.ToolEventThinking:
		activity.Label = "Thinking..."
	case valueobject.ToolEventCalling:
		activity.Label = msg.event.ToolName
	case valueobject.ToolEventToolLoaded:
		activity.Label = fmt.Sprintf("%s (%s)", msg.event.ToolName, msg.event.Detail)
		a.chatview.AddMessage(chatview.Message{
			Role:      chatview.System,
			Content:   fmt.Sprintf("Loaded tools: %s (%s)", msg.event.ToolName, msg.event.Detail),
			Timestamp: time.Now(),
		})
	case valueobject.ToolEventSkillActivated:
		activity.Label = msg.event.ToolName
		a.chatview.AddMessage(chatview.Message{
			Role:      chatview.System,
			Content:   fmt.Sprintf("Skill activated: %s", msg.event.ToolName),
			Timestamp: time.Now(),
		})
	case valueobject.ToolEventSubAgentStarted,
		valueobject.ToolEventSubAgentThinking,
		valueobject.ToolEventSubAgentToolCall,
		valueobject.ToolEventSubAgentCompleted:
		activity.AgentName = msg.event.ToolName
		activity.Label = msg.event.Detail
	}

	a.statusline.SetActivity(activity)

	cmd := a.readStream(msg.stream)
	return a, cmd
}

func (a *App) handleToken(msg agentTokenMsg) (tea.Model, tea.Cmd) {
	if a.state == stateWaiting {
		a.state = stateStreaming
		a.statusline.Hide()
		a.streamRenderer.Reset()

		a.chatview.AddMessage(chatview.Message{
			Role:      chatview.Assistant,
			Content:   "",
			Timestamp: time.Now(),
		})
	}

	renderCmd := a.streamRenderer.WriteToken(msg.content)

	// Use pre-rendered path (skip Glamour) for raw content during streaming.
	// StreamRenderedMsg will replace this with properly rendered content at
	// paragraph boundaries via shouldRender.
	a.chatview.UpdateLastAssistantRendered("  " + a.streamRenderer.Content())

	var cmds []tea.Cmd
	if renderCmd != nil {
		cmds = append(cmds, renderCmd)
	}
	cmds = append(cmds, a.readStream(msg.stream))

	return a, tea.Batch(cmds...)
}

func (a *App) handleStreamDone() (tea.Model, tea.Cmd) {
	slog.Debug("agent stream completed", "session_id", a.sessionID)

	if a.state == stateWaiting {
		a.statusline.Hide()
		a.chatview.AddMessage(chatview.Message{
			Role:      chatview.Assistant,
			Content:   "*No response received.*",
			Timestamp: time.Now(),
		})
	}

	a.cancelStream = nil
	a.state = stateReady

	var cmds []tea.Cmd
	if flushCmd := a.streamRenderer.Flush(); flushCmd != nil {
		cmds = append(cmds, flushCmd)
	}
	cmds = append(cmds, a.input.Focus())

	return a, tea.Batch(cmds...)
}

func (a *App) handleAgentError(msg agentErrorMsg) (tea.Model, tea.Cmd) {
	slog.Error("agent error", "error", msg.err, "session_id", a.sessionID)

	if a.cancelStream != nil {
		a.cancelStream()
		a.cancelStream = nil
	}
	a.statusline.Hide()
	a.state = stateReady

	errContent := fmt.Sprintf("*Error: %v*", msg.err)
	a.chatview.AddMessage(chatview.Message{
		Role:      chatview.Assistant,
		Content:   errContent,
		Timestamp: time.Now(),
	})

	return a, a.input.Focus()
}

func (a *App) updateComponents(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	newChat, cmd := a.chatview.Update(msg)
	a.chatview = newChat
	cmds = append(cmds, cmd)

	newInput, cmd := a.input.Update(msg)
	a.input = newInput
	cmds = append(cmds, cmd)

	newStatusLine, cmd := a.statusline.Update(msg)
	a.statusline = newStatusLine
	cmds = append(cmds, cmd)

	newStatus, cmd := a.statusbar.Update(msg)
	a.statusbar = newStatus
	cmds = append(cmds, cmd)

	return a, tea.Batch(cmds...)
}

func (a *App) renderHeader() string {
	style := a.theme.HeaderBar().Width(a.width)
	return style.Render(fmt.Sprintf(" liwaisi — AI Agent Assistant  %s", version.Short()))
}

type agentStreamStartMsg struct {
	stream <-chan valueobject.StreamChunk
}

type agentToolEventMsg struct {
	event  valueobject.ToolEvent
	stream <-chan valueobject.StreamChunk
}

type agentTokenMsg struct {
	content string
	stream  <-chan valueobject.StreamChunk
}

type agentStreamDoneMsg struct{}

type agentErrorMsg struct {
	err error
}

func (a *App) sendToAgent(ctx context.Context, userMsg string) tea.Cmd {
	return func() tea.Msg {
		ch, err := a.agentSvc.Chat(ctx, a.sessionID, userMsg)
		if err != nil {
			return agentErrorMsg{err: err}
		}
		return agentStreamStartMsg{stream: ch}
	}
}

func (a *App) readStream(ch <-chan valueobject.StreamChunk) tea.Cmd {
	return func() tea.Msg {
		chunk, ok := <-ch
		if !ok {
			return agentStreamDoneMsg{}
		}
		if chunk.Err != nil {
			return agentErrorMsg{err: chunk.Err}
		}
		if chunk.Done {
			return agentStreamDoneMsg{}
		}
		if chunk.ToolEvent != nil {
			return agentToolEventMsg{event: *chunk.ToolEvent, stream: ch}
		}
		return agentTokenMsg{content: chunk.Content, stream: ch}
	}
}
