package statusbar

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli/theme"
)

func newTestModel(t *testing.T) Model {
	t.Helper()
	th, err := theme.Get("dark")
	if err != nil {
		t.Fatalf("theme.Get: %v", err)
	}
	m := New(th)
	m.SetWidth(80)
	return m
}

func TestNew_Defaults(t *testing.T) {
	m := newTestModel(t)
	view := m.View()
	if !strings.Contains(view, "none") {
		t.Errorf("View() should contain default model name 'none', got: %q", view)
	}
}

func TestView_ContainsModelName(t *testing.T) {
	m := newTestModel(t)
	m.SetModel("gpt-4")
	view := m.View()
	if !strings.Contains(view, "gpt-4") {
		t.Errorf("View() should contain model name, got: %q", view)
	}
}

func TestView_ContainsSessionID(t *testing.T) {
	m := newTestModel(t)
	m.SetSessionID("abc-123")
	view := m.View()
	if !strings.Contains(view, "abc-123") {
		t.Errorf("View() should contain session ID, got: %q", view)
	}
}

func TestView_ShowsTokens(t *testing.T) {
	m := newTestModel(t)
	m.SetTokens(150)
	m.SetLatency(42)
	view := m.View()
	if !strings.Contains(view, "150") {
		t.Errorf("View() should contain token count, got: %q", view)
	}
	if !strings.Contains(view, "42ms") {
		t.Errorf("View() should contain latency, got: %q", view)
	}
}

func TestView_ShowsReady(t *testing.T) {
	m := newTestModel(t)
	view := m.View()
	if !strings.Contains(view, "ready") {
		t.Errorf("View() should show 'ready' when no tokens, got: %q", view)
	}
}

func TestUpdate_WindowResize(t *testing.T) {
	m := newTestModel(t)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if m2.width != 120 {
		t.Errorf("width = %d, want 120", m2.width)
	}
}
