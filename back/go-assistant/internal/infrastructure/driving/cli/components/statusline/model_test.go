package statusline

import (
	"strings"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli/theme"
)

func newTestModel(t *testing.T) Model {
	t.Helper()

	// Force theme package init() to register built-in themes.
	_ = theme.Names()

	th, err := theme.Get("dark")
	if err != nil {
		t.Fatalf("theme.Get: %v", err)
	}
	return New(th)
}

func TestNew_HiddenByDefault(t *testing.T) {
	t.Parallel()

	m := newTestModel(t)
	if m.Visible() {
		t.Error("StatusLine should be hidden by default")
	}
}

func TestView_HiddenReturnsEmpty(t *testing.T) {
	t.Parallel()

	m := newTestModel(t)
	if view := m.View(); view != "" {
		t.Errorf("View() when hidden = %q, want empty", view)
	}
}

func TestShow(t *testing.T) {
	t.Parallel()

	m := newTestModel(t)
	cmd := m.Show()
	if !m.Visible() {
		t.Error("StatusLine should be visible after Show()")
	}
	if cmd == nil {
		t.Error("Show() should return a tick command")
	}
}

func TestHide(t *testing.T) {
	t.Parallel()

	m := newTestModel(t)
	m.Show()
	m.Hide()
	if m.Visible() {
		t.Error("StatusLine should be hidden after Hide()")
	}
}

func TestView_Activities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		activity     Activity
		wantContains []string
		wantMissing  []string
	}{
		{
			name: "thinking shows no agent name",
			activity: Activity{
				Kind:      valueobject.ToolEventThinking,
				Iteration: 1,
				Total:     50,
				Elapsed:   3 * time.Second,
			},
			wantContains: []string{"Thinking...", "[1/50]", "(3s)"},
			wantMissing:  []string{"@"},
		},
		{
			name: "calling shows tool name",
			activity: Activity{
				Kind:      valueobject.ToolEventCalling,
				Label:     "read_file",
				Iteration: 2,
				Total:     50,
				Elapsed:   5 * time.Second,
			},
			wantContains: []string{"Running", "read_file", "[2/50]", "(5s)"},
			wantMissing:  []string{"@"},
		},
		{
			name: "tool loaded shows category",
			activity: Activity{
				Kind:  valueobject.ToolEventToolLoaded,
				Label: "filemanagement (5 tools)",
			},
			wantContains: []string{"tools loaded:", "filemanagement (5 tools)"},
			wantMissing:  []string{"@"},
		},
		{
			name: "skill activated shows skill name",
			activity: Activity{
				Kind:  valueobject.ToolEventSkillActivated,
				Label: "code-review",
			},
			wantContains: []string{"skill activated:", "code-review"},
			wantMissing:  []string{"@"},
		},
		{
			name: "sub-agent started shows agent name",
			activity: Activity{
				Kind:      valueobject.ToolEventSubAgentStarted,
				AgentName: "code_reviewer",
			},
			wantContains: []string{"@code_reviewer", "started"},
		},
		{
			name: "sub-agent thinking shows agent and progress",
			activity: Activity{
				Kind:      valueobject.ToolEventSubAgentThinking,
				AgentName: "security_auditor",
				Iteration: 2,
				Total:     10,
				Elapsed:   4 * time.Second,
			},
			wantContains: []string{"@security_auditor", "thinking", "[2/10]", "(4s)"},
		},
		{
			name: "sub-agent tool call shows agent and tool",
			activity: Activity{
				Kind:      valueobject.ToolEventSubAgentToolCall,
				AgentName: "code_reviewer",
				Label:     "read_file",
				Iteration: 3,
				Total:     10,
				Elapsed:   4 * time.Second,
			},
			wantContains: []string{"@code_reviewer", "read_file", "[3/10]"},
		},
		{
			name: "sub-agent completed shows agent and elapsed",
			activity: Activity{
				Kind:      valueobject.ToolEventSubAgentCompleted,
				AgentName: "data_analyst",
				Label:     "completed",
				Elapsed:   12 * time.Second,
			},
			wantContains: []string{"@data_analyst", "completed", "(12s)"},
		},
		{
			name: "sub-agent canceled shows actual status",
			activity: Activity{
				Kind:      valueobject.ToolEventSubAgentCompleted,
				AgentName: "timeout_agent",
				Label:     "canceled",
				Elapsed:   5 * time.Second,
			},
			wantContains: []string{"@timeout_agent", "canceled", "(5s)"},
			wantMissing:  []string{"completed"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := newTestModel(t)
			m.Show()
			m.SetActivity(tt.activity)

			view := m.View()
			for _, s := range tt.wantContains {
				if !strings.Contains(view, s) {
					t.Errorf("View() missing %q in %q", s, view)
				}
			}
			for _, s := range tt.wantMissing {
				if strings.Contains(view, s) {
					t.Errorf("View() should not contain %q in %q", s, view)
				}
			}
		})
	}
}

func TestSetActivity_ClearActivity(t *testing.T) {
	t.Parallel()

	m := newTestModel(t)
	m.Show()

	m.SetActivity(Activity{
		Kind:      valueobject.ToolEventSubAgentToolCall,
		AgentName: "test_agent",
		Label:     "grep",
	})

	view := m.View()
	if !strings.Contains(view, "@test_agent") {
		t.Errorf("View() after SetActivity should contain agent name, got %q", view)
	}

	m.ClearActivity()
	view = m.View()
	if strings.Contains(view, "@test_agent") {
		t.Errorf("View() after ClearActivity should not contain agent name, got %q", view)
	}
}
