package chatview

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/glamour"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli/render"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli/theme"
)

func newTestModel(t *testing.T) Model {
	t.Helper()
	th, err := theme.Get("dark")
	if err != nil {
		t.Fatalf("theme.Get: %v", err)
	}
	r, err := render.NewRenderer(glamour.WithAutoStyle(), 80)
	if err != nil {
		t.Fatalf("render.NewRenderer: %v", err)
	}
	m := New(th, r)
	m.SetSize(80, 24)
	return m
}

func TestNew(t *testing.T) {
	m := newTestModel(t)
	if m.MessageCount() != 0 {
		t.Errorf("MessageCount() = %d, want 0", m.MessageCount())
	}
}

func TestAddMessage(t *testing.T) {
	m := newTestModel(t)

	tests := []struct {
		name    string
		msg     Message
		wantCnt int
	}{
		{
			name:    "first user message",
			msg:     Message{Role: User, Content: "Hello", Timestamp: time.Now()},
			wantCnt: 1,
		},
		{
			name:    "assistant reply",
			msg:     Message{Role: Assistant, Content: "Hi there!", Timestamp: time.Now()},
			wantCnt: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.AddMessage(tt.msg)
			if m.MessageCount() != tt.wantCnt {
				t.Errorf("MessageCount() = %d, want %d", m.MessageCount(), tt.wantCnt)
			}
		})
	}
}

func TestView_ContainsMessages(t *testing.T) {
	m := newTestModel(t)
	m.AddMessage(Message{Role: User, Content: "test message", Timestamp: time.Now()})

	view := m.View()
	if !strings.Contains(view, "You") {
		t.Errorf("View() missing user prefix:\n%s", view)
	}
}

func TestUpdate_WindowResize(t *testing.T) {
	m := newTestModel(t)
	m.AddMessage(Message{Role: User, Content: "hello", Timestamp: time.Now()})

	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	view := m2.View()
	if view == "" {
		t.Error("View() should not be empty after resize")
	}
}

func TestUpdateLastAssistant(t *testing.T) {
	m := newTestModel(t)
	m.AddMessage(Message{Role: User, Content: "question", Timestamp: time.Now()})
	m.AddMessage(Message{Role: Assistant, Content: "initial", Timestamp: time.Now()})

	m.UpdateLastAssistant("updated content")

	view := m.View()
	if !strings.Contains(view, "updated content") {
		t.Errorf("View() should contain updated content:\n%s", view)
	}
}
