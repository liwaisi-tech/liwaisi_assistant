package interactionpicker

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func testInteractions() []Interaction {
	return []Interaction{
		{Index: 0, Content: "What is Go?", Timestamp: time.Now().Add(-5 * time.Minute)},
		{Index: 2, Content: "How do channels work?", Timestamp: time.Now().Add(-3 * time.Minute)},
		{Index: 4, Content: "Show me a goroutine example", Timestamp: time.Now().Add(-1 * time.Minute)},
	}
}

func keyPress(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code}
}

func TestModel_Navigation(t *testing.T) {
	m := New(testInteractions(), nil, 80, 24)
	if m.cursor != 0 {
		t.Fatalf("initial cursor = %d, want 0", m.cursor)
	}

	m, _ = m.Update(keyPress('j'))
	if m.cursor != 1 {
		t.Errorf("after j, cursor = %d, want 1", m.cursor)
	}

	m, _ = m.Update(keyPress('j'))
	if m.cursor != 2 {
		t.Errorf("after j, cursor = %d, want 2", m.cursor)
	}

	m, _ = m.Update(keyPress('j'))
	if m.cursor != 2 {
		t.Errorf("after j at end, cursor = %d, want 2 (clamped)", m.cursor)
	}

	m, _ = m.Update(keyPress('k'))
	if m.cursor != 1 {
		t.Errorf("after k, cursor = %d, want 1", m.cursor)
	}
}

func TestModel_EnterEmitsSelected(t *testing.T) {
	m := New(testInteractions(), nil, 80, 24)

	m, _ = m.Update(keyPress('j'))
	_, cmd := m.Update(keyPress(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("Enter should emit a command")
	}

	msg := cmd()
	selected, ok := msg.(SelectedMsg)
	if !ok {
		t.Fatalf("expected SelectedMsg, got %T", msg)
	}
	if selected.Index != 2 {
		t.Errorf("Index = %d, want 2", selected.Index)
	}
}

func TestModel_EscEmitsCanceled(t *testing.T) {
	m := New(testInteractions(), nil, 80, 24)
	_, cmd := m.Update(keyPress(tea.KeyEscape))
	if cmd == nil {
		t.Fatal("Esc should emit a command")
	}

	msg := cmd()
	if _, ok := msg.(CanceledMsg); !ok {
		t.Fatalf("expected CanceledMsg, got %T", msg)
	}
}

func TestModel_ViewContainsContent(t *testing.T) {
	m := New(testInteractions(), nil, 80, 24)
	view := m.View()

	for _, inter := range testInteractions() {
		if !containsString(view, inter.Content) {
			t.Errorf("View should contain content %q", inter.Content)
		}
	}
}

func TestModel_ViewEmpty(t *testing.T) {
	m := New(nil, nil, 80, 24)
	view := m.View()
	if view != "No interactions to rewind to." {
		t.Errorf("View for empty interactions = %q, want %q", view, "No interactions to rewind to.")
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"short", "hello", 10, "hello"},
		{"exact", "hello", 5, "hello"},
		{"long", "this is a very long string that should be cut", 20, "this is a very lo..."},
		{"newlines", "line1\nline2", 20, "line1 line2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func containsString(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
