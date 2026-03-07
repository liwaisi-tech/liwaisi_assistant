package sessionpicker

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/session"
)

func testSessions() []session.Summary {
	return []session.Summary{
		{SessionID: "s1", FirstUserMsg: "first question", MessageCount: 3, LastActivity: time.Now().Add(-time.Hour)},
		{SessionID: "s2", FirstUserMsg: "second question", MessageCount: 5, LastActivity: time.Now()},
		{SessionID: "s3", FirstUserMsg: "third question", MessageCount: 1, LastActivity: time.Now().Add(-24 * time.Hour)},
	}
}

func keyPress(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code}
}

func TestModel_Navigation(t *testing.T) {
	m := New(testSessions(), nil, 80, 24)
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
	m := New(testSessions(), nil, 80, 24)
	_, cmd := m.Update(keyPress(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("Enter should emit a command")
	}

	msg := cmd()
	selected, ok := msg.(SessionSelectedMsg)
	if !ok {
		t.Fatalf("expected SessionSelectedMsg, got %T", msg)
	}
	if selected.SessionID != "s1" {
		t.Errorf("SessionID = %q, want %q", selected.SessionID, "s1")
	}
}

func TestModel_EscEmitsCancelled(t *testing.T) {
	m := New(testSessions(), nil, 80, 24)
	_, cmd := m.Update(keyPress(tea.KeyEscape))
	if cmd == nil {
		t.Fatal("Esc should emit a command")
	}

	msg := cmd()
	if _, ok := msg.(CanceledMsg); !ok {
		t.Fatalf("expected CanceledMsg, got %T", msg)
	}
}

func TestModel_ViewContainsSessions(t *testing.T) {
	m := New(testSessions(), nil, 80, 24)
	view := m.View()

	for _, s := range testSessions() {
		if !containsString(view, s.SessionID) {
			t.Errorf("View should contain session ID %q", s.SessionID)
		}
	}
}

func TestModel_ViewEmpty(t *testing.T) {
	m := New(nil, nil, 80, 24)
	view := m.View()
	if view != "No sessions found." {
		t.Errorf("View for empty sessions = %q, want %q", view, "No sessions found.")
	}
}

func TestFormatAge(t *testing.T) {
	tests := []struct {
		name string
		t    time.Time
		want string
	}{
		{"zero time", time.Time{}, "unknown"},
		{"just now", time.Now().Add(-10 * time.Second), "just now"},
		{"minutes ago", time.Now().Add(-30 * time.Minute), "30m ago"},
		{"hours ago", time.Now().Add(-3 * time.Hour), "3h ago"},
		{"days ago", time.Now().Add(-48 * time.Hour), "2d ago"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatAge(tt.t)
			if got != tt.want {
				t.Errorf("formatAge() = %q, want %q", got, tt.want)
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
