package cli

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
	inputcomp "github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli/components/input"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli/render"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli/theme"
)

type mockAgentService struct{}

func (m *mockAgentService) Chat(_ context.Context, _ string, msg string) (<-chan valueobject.StreamChunk, error) {
	ch := make(chan valueobject.StreamChunk, 3)
	ch <- valueobject.StreamChunk{Content: fmt.Sprintf("Reply to: %s", msg)}
	ch <- valueobject.StreamChunk{Done: true}
	close(ch)
	return ch, nil
}

func (m *mockAgentService) Ask(_ context.Context, query string) (string, error) {
	return fmt.Sprintf("Answer to: %s", query), nil
}

func newTestApp(t *testing.T) *App {
	t.Helper()
	th, err := theme.Get("dark")
	if err != nil {
		t.Fatalf("theme.Get: %v", err)
	}
	app, err := NewApp(&AppConfig{
		Theme:       th,
		HistorySize: 10,
		AgentSvc:    &mockAgentService{},
		ModelName:   "test-model",
	})
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return app
}

func TestNewApp(t *testing.T) {
	app := newTestApp(t)
	if app.state != stateReady {
		t.Errorf("initial state = %d, want %d (stateReady)", app.state, stateReady)
	}
}

func TestApp_Init(t *testing.T) {
	app := newTestApp(t)
	cmd := app.Init()
	if cmd == nil {
		t.Error("Init() should return a command")
	}
}

func TestApp_View_NotEmpty(t *testing.T) {
	app := newTestApp(t)
	view := app.View()
	content := view.Content
	if content == "" {
		t.Error("View() should not be empty")
	}
	if !strings.Contains(content, "liwaisi") {
		t.Errorf("View() should contain header, got:\n%s", content)
	}
}

func TestApp_CtrlC_Quits(t *testing.T) {
	app := newTestApp(t)
	_, cmd := app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !app.quitting {
		t.Error("Ctrl+C should set quitting state")
	}
	if cmd == nil {
		t.Error("Ctrl+C should return a quit command")
	}
}

func TestApp_CtrlD_Quits(t *testing.T) {
	app := newTestApp(t)
	_, cmd := app.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	if !app.quitting {
		t.Error("Ctrl+D should set quitting state")
	}
	if cmd == nil {
		t.Error("Ctrl+D should return a quit command")
	}
}

func TestApp_Esc_Quits(t *testing.T) {
	app := newTestApp(t)
	_, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !app.quitting {
		t.Error("Esc should set quitting state")
	}
	if cmd == nil {
		t.Error("Esc should return a quit command")
	}
}

func TestApp_Submit_TransitionsToWaiting(t *testing.T) {
	app := newTestApp(t)
	_, _ = app.Update(inputcomp.SubmitMsg{Value: "hello"})
	if app.state != stateWaiting {
		t.Errorf("state after submit = %d, want %d (stateWaiting)", app.state, stateWaiting)
	}
}

func TestApp_StreamDone_TransitionsToReady(t *testing.T) {
	app := newTestApp(t)
	_, _ = app.Update(inputcomp.SubmitMsg{Value: "hello"})
	_, _ = app.Update(agentStreamDoneMsg{})
	if app.state != stateReady {
		t.Errorf("state after stream done = %d, want %d (stateReady)", app.state, stateReady)
	}
}

func TestApp_StreamDone_WhileWaiting_ShowsFallback(t *testing.T) {
	app := newTestApp(t)
	_, _ = app.Update(inputcomp.SubmitMsg{Value: "hello"})
	if app.state != stateWaiting {
		t.Fatalf("state after submit = %d, want stateWaiting", app.state)
	}

	_, _ = app.Update(agentStreamDoneMsg{})
	if app.state != stateReady {
		t.Errorf("state after stream done = %d, want stateReady", app.state)
	}

	view := app.View()
	if !strings.Contains(view.Content, "No response received") {
		t.Error("View should contain fallback message when stream completes without tokens")
	}
}

func TestApp_AgentError_TransitionsToReady(t *testing.T) {
	app := newTestApp(t)
	_, _ = app.Update(inputcomp.SubmitMsg{Value: "hello"})
	_, _ = app.Update(agentErrorMsg{err: fmt.Errorf("test error")})
	if app.state != stateReady {
		t.Errorf("state after error = %d, want %d (stateReady)", app.state, stateReady)
	}
}

func TestApp_CtrlC_CancelsWaiting(t *testing.T) {
	app := newTestApp(t)
	_, _ = app.Update(inputcomp.SubmitMsg{Value: "hello"})
	_, _ = app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if app.state != stateReady {
		t.Errorf("state after cancel = %d, want %d (stateReady)", app.state, stateReady)
	}
	if app.quitting {
		t.Error("canceling should not quit the app")
	}
}

func TestApp_Resize(t *testing.T) {
	app := newTestApp(t)
	_, _ = app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if app.width != 120 || app.height != 40 {
		t.Errorf("dimensions = %dx%d, want 120x40", app.width, app.height)
	}
	if app.renderer.Width() != 120 {
		t.Errorf("renderer width = %d, want 120", app.renderer.Width())
	}
}

func TestApp_Resize_UpdatesRendererWidth(t *testing.T) {
	app := newTestApp(t)

	tests := []struct {
		name  string
		width int
	}{
		{"narrow terminal", 40},
		{"standard terminal", 80},
		{"wide terminal", 200},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _ = app.Update(tea.WindowSizeMsg{Width: tt.width, Height: 24})
			if app.renderer.Width() != tt.width {
				t.Errorf("renderer width = %d, want %d", app.renderer.Width(), tt.width)
			}
		})
	}
}

func TestApp_Goodbye(t *testing.T) {
	app := newTestApp(t)
	app.quitting = true
	view := app.View()
	if !strings.Contains(view.Content, "Goodbye") {
		t.Errorf("quitting View() should contain 'Goodbye', got: %q", view.Content)
	}
}

func TestApp_StreamStartKeepsWaiting(t *testing.T) {
	app := newTestApp(t)
	_, _ = app.Update(inputcomp.SubmitMsg{Value: "hello"})

	ch := make(chan valueobject.StreamChunk, 2)
	ch <- valueobject.StreamChunk{Content: "hi"}
	ch <- valueobject.StreamChunk{Done: true}
	close(ch)

	_, _ = app.Update(agentStreamStartMsg{stream: ch})
	if app.state != stateWaiting {
		t.Errorf("state after stream start = %d, want %d (stateWaiting)", app.state, stateWaiting)
	}
}

func TestApp_FirstTokenTransitionsToStreaming(t *testing.T) {
	app := newTestApp(t)
	_, _ = app.Update(inputcomp.SubmitMsg{Value: "hello"})

	ch := make(chan valueobject.StreamChunk, 2)
	ch <- valueobject.StreamChunk{Content: "hi"}
	ch <- valueobject.StreamChunk{Done: true}
	close(ch)

	_, _ = app.Update(agentStreamStartMsg{stream: ch})
	_, _ = app.Update(agentTokenMsg{content: "hi", stream: ch})
	if app.state != stateStreaming {
		t.Errorf("state after first token = %d, want %d (stateStreaming)", app.state, stateStreaming)
	}
}

func TestApp_CtrlC_CancelsStreaming(t *testing.T) {
	app := newTestApp(t)
	app.state = stateStreaming
	canceled := false
	app.cancelStream = func() { canceled = true }

	_, _ = app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !canceled {
		t.Error("Ctrl+C during streaming should cancel the stream context")
	}
	if app.quitting {
		t.Error("canceling stream should not quit the app")
	}
}

func TestApp_FullStreamingLifecycle(t *testing.T) {
	app := newTestApp(t)

	_, _ = app.Update(inputcomp.SubmitMsg{Value: "hello"})
	if app.state != stateWaiting {
		t.Fatalf("state after submit = %d, want stateWaiting", app.state)
	}
	if app.cancelStream == nil {
		t.Fatal("cancelStream should be set after submit")
	}

	ch := make(chan valueobject.StreamChunk, 4)
	ch <- valueobject.StreamChunk{Content: "Hello "}
	ch <- valueobject.StreamChunk{Content: "World"}
	ch <- valueobject.StreamChunk{Done: true}
	close(ch)

	_, _ = app.Update(agentStreamStartMsg{stream: ch})
	if app.state != stateWaiting {
		t.Fatalf("state after stream start = %d, want stateWaiting", app.state)
	}

	_, _ = app.Update(agentTokenMsg{content: "Hello ", stream: ch})
	if app.state != stateStreaming {
		t.Errorf("state after first token = %d, want stateStreaming", app.state)
	}
	if app.streamRenderer.Content() != "Hello " {
		t.Errorf("stream content = %q, want %q", app.streamRenderer.Content(), "Hello ")
	}

	_, _ = app.Update(agentTokenMsg{content: "World", stream: ch})
	if app.streamRenderer.Content() != "Hello World" {
		t.Errorf("stream content = %q, want %q", app.streamRenderer.Content(), "Hello World")
	}

	_, _ = app.Update(agentStreamDoneMsg{})
	if app.state != stateReady {
		t.Errorf("state after done = %d, want stateReady", app.state)
	}
	if app.cancelStream != nil {
		t.Error("cancelStream should be nil after stream done")
	}
}

func TestApp_StreamRenderedMsg_UpdatesChatview(t *testing.T) {
	app := newTestApp(t)

	_, _ = app.Update(inputcomp.SubmitMsg{Value: "test"})

	ch := make(chan valueobject.StreamChunk, 2)
	ch <- valueobject.StreamChunk{Content: "initial"}
	ch <- valueobject.StreamChunk{Done: true}
	close(ch)

	_, _ = app.Update(agentStreamStartMsg{stream: ch})
	_, _ = app.Update(agentTokenMsg{content: "initial", stream: ch})

	_, _ = app.Update(render.StreamRenderedMsg{Rendered: "rendered content"})

	view := app.View()
	if !strings.Contains(view.Content, "rendered content") {
		t.Errorf("View() should contain rendered content after StreamRenderedMsg")
	}
}

func TestApp_StreamError_MidStream(t *testing.T) {
	app := newTestApp(t)

	_, _ = app.Update(inputcomp.SubmitMsg{Value: "test"})

	ch := make(chan valueobject.StreamChunk, 3)
	ch <- valueobject.StreamChunk{Content: "partial"}
	ch <- valueobject.StreamChunk{Err: fmt.Errorf("connection lost")}
	close(ch)

	_, _ = app.Update(agentStreamStartMsg{stream: ch})
	_, _ = app.Update(agentTokenMsg{content: "partial", stream: ch})
	if app.state != stateStreaming {
		t.Fatalf("state during stream = %d, want stateStreaming", app.state)
	}

	_, _ = app.Update(agentErrorMsg{err: fmt.Errorf("connection lost")})
	if app.state != stateReady {
		t.Errorf("state after error = %d, want stateReady", app.state)
	}
	if app.cancelStream != nil {
		t.Error("cancelStream should be nil after error")
	}
}

func TestApp_AgentError_CancelsContext(t *testing.T) {
	app := newTestApp(t)
	canceled := false
	app.cancelStream = func() { canceled = true }
	app.state = stateWaiting

	_, _ = app.Update(agentErrorMsg{err: fmt.Errorf("API error")})
	if !canceled {
		t.Error("handleAgentError should cancel the stream context")
	}
	if app.cancelStream != nil {
		t.Error("cancelStream should be nil after error handling")
	}
}

func TestApp_Submit_SetsCancelStream(t *testing.T) {
	app := newTestApp(t)

	_, _ = app.Update(inputcomp.SubmitMsg{Value: "test"})

	if app.cancelStream == nil {
		t.Error("cancelStream should be set in handleSubmit, not in the Cmd goroutine")
	}
}

func TestApp_View_DuringStreaming(t *testing.T) {
	app := newTestApp(t)
	_, _ = app.Update(inputcomp.SubmitMsg{Value: "question"})

	ch := make(chan valueobject.StreamChunk, 2)
	ch <- valueobject.StreamChunk{Content: "answer"}
	ch <- valueobject.StreamChunk{Done: true}
	close(ch)
	_, _ = app.Update(agentStreamStartMsg{stream: ch})
	_, _ = app.Update(agentTokenMsg{content: "answer", stream: ch})

	view := app.View()
	if view.Content == "" {
		t.Error("View() should not be empty during streaming")
	}
	if !view.AltScreen {
		t.Error("View() should use alt screen")
	}
}

func TestApp_Resize_DuringStreaming(t *testing.T) {
	app := newTestApp(t)
	app.state = stateStreaming

	_, _ = app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if app.width != 100 || app.height != 30 {
		t.Errorf("dimensions = %dx%d, want 100x30", app.width, app.height)
	}
	if app.state != stateStreaming {
		t.Errorf("resize should not change state, got %d", app.state)
	}
}

func TestApp_ReadStream_Token(t *testing.T) {
	app := newTestApp(t)
	ch := make(chan valueobject.StreamChunk, 2)
	ch <- valueobject.StreamChunk{Content: "token"}

	cmd := app.readStream(ch)
	msg := cmd()

	tokenMsg, ok := msg.(agentTokenMsg)
	if !ok {
		t.Fatalf("expected agentTokenMsg, got %T", msg)
	}
	if tokenMsg.content != "token" {
		t.Errorf("token content = %q, want %q", tokenMsg.content, "token")
	}
}

func TestApp_ReadStream_Done(t *testing.T) {
	app := newTestApp(t)
	ch := make(chan valueobject.StreamChunk, 1)
	ch <- valueobject.StreamChunk{Done: true}

	cmd := app.readStream(ch)
	msg := cmd()

	if _, ok := msg.(agentStreamDoneMsg); !ok {
		t.Fatalf("expected agentStreamDoneMsg, got %T", msg)
	}
}

func TestApp_ReadStream_Error(t *testing.T) {
	app := newTestApp(t)
	ch := make(chan valueobject.StreamChunk, 1)
	ch <- valueobject.StreamChunk{Err: fmt.Errorf("stream error")}

	cmd := app.readStream(ch)
	msg := cmd()

	errMsg, ok := msg.(agentErrorMsg)
	if !ok {
		t.Fatalf("expected agentErrorMsg, got %T", msg)
	}
	if errMsg.err.Error() != "stream error" {
		t.Errorf("error = %v, want 'stream error'", errMsg.err)
	}
}

func TestApp_ReadStream_ClosedChannel(t *testing.T) {
	app := newTestApp(t)
	ch := make(chan valueobject.StreamChunk)
	close(ch)

	cmd := app.readStream(ch)
	msg := cmd()

	if _, ok := msg.(agentStreamDoneMsg); !ok {
		t.Fatalf("expected agentStreamDoneMsg for closed channel, got %T", msg)
	}
}

func TestApp_SendToAgent_HappyPath(t *testing.T) {
	app := newTestApp(t)
	ctx := context.Background()

	cmd := app.sendToAgent(ctx, "hello")
	msg := cmd()

	if _, ok := msg.(agentStreamStartMsg); !ok {
		t.Fatalf("expected agentStreamStartMsg, got %T", msg)
	}
}

func TestApp_SendToAgent_Error(t *testing.T) {
	th, _ := theme.Get("dark")
	errSvc := &errorAgentService{err: fmt.Errorf("service down")}
	app, _ := NewApp(&AppConfig{
		Theme:       th,
		HistorySize: 10,
		AgentSvc:    errSvc,
	})
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	ctx := context.Background()
	cmd := app.sendToAgent(ctx, "fail")
	msg := cmd()

	errMsg, ok := msg.(agentErrorMsg)
	if !ok {
		t.Fatalf("expected agentErrorMsg, got %T", msg)
	}
	if errMsg.err.Error() != "service down" {
		t.Errorf("error = %v, want 'service down'", errMsg.err)
	}
}

type errorAgentService struct {
	err error
}

func (e *errorAgentService) Chat(_ context.Context, _ string, _ string) (<-chan valueobject.StreamChunk, error) {
	return nil, e.err
}

func (e *errorAgentService) Ask(_ context.Context, _ string) (string, error) {
	return "", e.err
}

func TestApp_ReadStream_ToolEvent(t *testing.T) {
	app := newTestApp(t)
	ch := make(chan valueobject.StreamChunk, 1)
	ch <- valueobject.StreamChunk{
		ToolEvent: &valueobject.ToolEvent{
			Kind:      valueobject.ToolEventCalling,
			Iteration: 1,
			Total:     50,
			ToolName:  "read_file",
		},
	}

	cmd := app.readStream(ch)
	msg := cmd()

	toolMsg, ok := msg.(agentToolEventMsg)
	if !ok {
		t.Fatalf("expected agentToolEventMsg, got %T", msg)
	}
	if toolMsg.event.ToolName != "read_file" {
		t.Errorf("tool name = %q, want %q", toolMsg.event.ToolName, "read_file")
	}
}

func TestApp_HandleToolEvent_UpdatesSpinner(t *testing.T) {
	app := newTestApp(t)
	_, _ = app.Update(inputcomp.SubmitMsg{Value: "hello"})

	ch := make(chan valueobject.StreamChunk, 1)
	ch <- valueobject.StreamChunk{Done: true}
	close(ch)

	_, _ = app.Update(agentToolEventMsg{
		event: valueobject.ToolEvent{
			Kind:      valueobject.ToolEventCalling,
			Iteration: 2,
			Total:     50,
			ToolName:  "write_file",
		},
		stream: ch,
	})

	if app.state != stateWaiting {
		t.Errorf("state during tool event = %d, want %d (stateWaiting)", app.state, stateWaiting)
	}
}

func TestApp_SlashClear(t *testing.T) {
	app := newTestApp(t)

	_, _ = app.Update(inputcomp.SubmitMsg{Value: "hello"})
	_, _ = app.Update(agentStreamDoneMsg{})

	initialMsgs := app.chatview.MessageCount()
	if initialMsgs == 0 {
		t.Fatal("expected at least 1 message before /clear")
	}

	_, _ = app.Update(inputcomp.SubmitMsg{Value: "/clear"})

	msgs := app.chatview.Messages()
	if len(msgs) != 1 {
		t.Fatalf("after /clear, expected 1 system message, got %d", len(msgs))
	}
	if !strings.Contains(msgs[0].Content, "Conversation cleared") {
		t.Errorf("expected 'Conversation cleared' message, got %q", msgs[0].Content)
	}
	if app.state != stateReady {
		t.Errorf("state after /clear = %d, want stateReady", app.state)
	}
}

func TestApp_SlashHelp(t *testing.T) {
	app := newTestApp(t)

	_, _ = app.Update(inputcomp.SubmitMsg{Value: "/help"})

	msgs := app.chatview.Messages()
	if len(msgs) == 0 {
		t.Fatal("expected at least 1 message after /help")
	}

	last := msgs[len(msgs)-1]
	if !strings.Contains(last.Content, "/clear") {
		t.Errorf("help message should list /clear, got %q", last.Content)
	}
	if !strings.Contains(last.Content, "/help") {
		t.Errorf("help message should list /help, got %q", last.Content)
	}
	if !strings.Contains(last.Content, "/rewind") {
		t.Errorf("help message should list /rewind, got %q", last.Content)
	}
	if !strings.Contains(last.Content, "/resume") {
		t.Errorf("help message should list /resume, got %q", last.Content)
	}
}

func TestApp_SlashUnknown(t *testing.T) {
	app := newTestApp(t)

	_, _ = app.Update(inputcomp.SubmitMsg{Value: "/nonexistent"})

	msgs := app.chatview.Messages()
	if len(msgs) == 0 {
		t.Fatal("expected at least 1 message after unknown command")
	}

	last := msgs[len(msgs)-1]
	if !strings.Contains(last.Content, "Unknown command") {
		t.Errorf("expected 'Unknown command' message, got %q", last.Content)
	}
	if !strings.Contains(last.Content, "/nonexistent") {
		t.Errorf("message should contain the unknown command name, got %q", last.Content)
	}
}

func TestApp_ViewFitsTerminal(t *testing.T) {
	app := newTestApp(t)

	view := app.View()
	lines := strings.Split(view.Content, "\n")

	if len(lines) != 24 {
		t.Errorf("View should render exactly 24 lines for a 24-row terminal, got %d", len(lines))
		for i, line := range lines {
			t.Logf("  Line %2d: %q", i, line)
		}
	}

	if !strings.Contains(view.Content, "liwaisi") {
		t.Error("View is missing the header")
	}
	if !strings.Contains(view.Content, "ready") {
		t.Error("View is missing the status bar")
	}
	if !strings.Contains(view.Content, "│") {
		t.Error("View is missing the input border")
	}
}
