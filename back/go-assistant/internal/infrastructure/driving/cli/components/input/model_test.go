package input

import (
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
	m := New(th, 10)
	m.SetWidth(80)
	return m
}

func TestNew(t *testing.T) {
	m := newTestModel(t)
	if m.Value() != "" {
		t.Errorf("Value() = %q, want empty", m.Value())
	}
}

func TestView_NotEmpty(t *testing.T) {
	m := newTestModel(t)
	view := m.View()
	if view == "" {
		t.Error("View() should not be empty")
	}
}

func TestUpdate_WindowResize(t *testing.T) {
	m := newTestModel(t)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if m2.width != 120 {
		t.Errorf("width = %d, want 120", m2.width)
	}
}

func TestHistory_PushAndNavigate(t *testing.T) {
	m := newTestModel(t)

	m.pushHistory("first")
	m.pushHistory("second")
	m.pushHistory("third")

	got, ok := m.historyUp()
	if !ok || got != "third" {
		t.Errorf("historyUp() = (%q, %v), want (third, true)", got, ok)
	}

	got, ok = m.historyUp()
	if !ok || got != "second" {
		t.Errorf("historyUp() = (%q, %v), want (second, true)", got, ok)
	}

	got, ok = m.historyDown()
	if !ok || got != "third" {
		t.Errorf("historyDown() = (%q, %v), want (third, true)", got, ok)
	}
}

func TestHistory_CapSize(t *testing.T) {
	m := newTestModel(t)
	for i := range 15 {
		m.pushHistory(string(rune('a' + i)))
	}
	if len(m.history) != 10 {
		t.Errorf("history length = %d, want 10 (capped)", len(m.history))
	}
}

func TestHistory_Empty(t *testing.T) {
	m := newTestModel(t)
	_, ok := m.historyUp()
	if ok {
		t.Error("historyUp() on empty history should return false")
	}
}
