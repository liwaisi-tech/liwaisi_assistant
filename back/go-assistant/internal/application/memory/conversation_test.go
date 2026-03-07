package memory

import (
	"sync"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

func TestConversationMemory_GetOrCreate(t *testing.T) {
	tests := []struct {
		name         string
		sessionID    string
		systemPrompt string
		setupFunc    func(m *ConversationMemory)
		wantNew      bool
	}{
		{
			name:         "creates new session",
			sessionID:    "s1",
			systemPrompt: "You are helpful.",
			wantNew:      true,
		},
		{
			name:         "returns existing session",
			sessionID:    "s1",
			systemPrompt: "You are helpful.",
			setupFunc: func(m *ConversationMemory) {
				m.GetOrCreate("s1", "You are helpful.")
			},
			wantNew: false,
		},
		{
			name:         "different sessions are independent",
			sessionID:    "s2",
			systemPrompt: "Different prompt.",
			setupFunc: func(m *ConversationMemory) {
				m.GetOrCreate("s1", "You are helpful.")
			},
			wantNew: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewConversationMemory()
			if tt.setupFunc != nil {
				tt.setupFunc(m)
			}

			conv := m.GetOrCreate(tt.sessionID, tt.systemPrompt)
			if conv == nil {
				t.Fatal("GetOrCreate returned nil")
			}
			if conv.SessionID != tt.sessionID {
				t.Errorf("SessionID = %q, want %q", conv.SessionID, tt.sessionID)
			}
			if conv.SystemPrompt != tt.systemPrompt {
				t.Errorf("SystemPrompt = %q, want %q", conv.SystemPrompt, tt.systemPrompt)
			}

			conv2 := m.GetOrCreate(tt.sessionID, tt.systemPrompt)
			if conv2 != conv {
				t.Error("second GetOrCreate returned different pointer")
			}
		})
	}
}

func TestConversationMemory_AppendAndHistory(t *testing.T) {
	m := NewConversationMemory()
	m.GetOrCreate("s1", "prompt")

	msg1 := entity.NewMessage(valueobject.RoleUser, "hello")
	msg2 := entity.NewMessage(valueobject.RoleAssistant, "hi there")

	m.Append("s1", &msg1)
	m.Append("s1", &msg2)

	history := m.History("s1")
	if len(history) != 2 {
		t.Fatalf("History length = %d, want 2", len(history))
	}
	if history[0].Content != "hello" {
		t.Errorf("history[0].Content = %q, want %q", history[0].Content, "hello")
	}
	if history[1].Content != "hi there" {
		t.Errorf("history[1].Content = %q, want %q", history[1].Content, "hi there")
	}
}

func TestConversationMemory_AppendToNonexistent(t *testing.T) {
	m := NewConversationMemory()
	msg := entity.NewMessage(valueobject.RoleUser, "hello")
	m.Append("missing", &msg)

	history := m.History("missing")
	if history != nil {
		t.Errorf("History for missing session = %v, want nil", history)
	}
}

func TestConversationMemory_HistoryReturnsCopy(t *testing.T) {
	m := NewConversationMemory()
	m.GetOrCreate("s1", "prompt")
	msg := entity.NewMessage(valueobject.RoleUser, "hello")
	m.Append("s1", &msg)

	history := m.History("s1")
	history[0].Content = "mutated"

	original := m.History("s1")
	if original[0].Content != "hello" {
		t.Error("History did not return a defensive copy")
	}
}

func TestConversationMemory_Trim(t *testing.T) {
	tests := []struct {
		name     string
		messages []entity.Message
		keepLast int
		wantLen  int
		wantMsgs []string
	}{
		{
			name: "trim to 2 keeps last 2 user/assistant messages",
			messages: []entity.Message{
				{Role: valueobject.RoleUser, Content: "m1"},
				{Role: valueobject.RoleAssistant, Content: "m2"},
				{Role: valueobject.RoleUser, Content: "m3"},
				{Role: valueobject.RoleAssistant, Content: "m4"},
			},
			keepLast: 2,
			wantLen:  2,
			wantMsgs: []string{"m3", "m4"},
		},
		{
			name: "trim preserves system messages",
			messages: []entity.Message{
				{Role: valueobject.RoleSystem, Content: "sys"},
				{Role: valueobject.RoleUser, Content: "m1"},
				{Role: valueobject.RoleAssistant, Content: "m2"},
				{Role: valueobject.RoleUser, Content: "m3"},
			},
			keepLast: 1,
			wantLen:  2,
			wantMsgs: []string{"sys", "m3"},
		},
		{
			name: "trim with keepLast greater than messages is no-op",
			messages: []entity.Message{
				{Role: valueobject.RoleUser, Content: "m1"},
			},
			keepLast: 10,
			wantLen:  1,
			wantMsgs: []string{"m1"},
		},
		{
			name:     "trim empty conversation",
			messages: nil,
			keepLast: 5,
			wantLen:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewConversationMemory()
			m.GetOrCreate("s1", "prompt")
			for i := range tt.messages {
				m.Append("s1", &tt.messages[i])
			}

			m.Trim("s1", tt.keepLast)

			history := m.History("s1")
			if len(history) != tt.wantLen {
				t.Fatalf("Trim result length = %d, want %d", len(history), tt.wantLen)
			}
			for i, want := range tt.wantMsgs {
				if history[i].Content != want {
					t.Errorf("history[%d].Content = %q, want %q", i, history[i].Content, want)
				}
			}
		})
	}
}

func TestConversationMemory_Trim_PreservesToolGroups(t *testing.T) {
	toolCalls := []valueobject.ToolCall{{ID: "c1", Type: "function", Function: valueobject.FunctionCall{Name: "echo", Arguments: "{}"}}}
	tests := []struct {
		name     string
		messages []entity.Message
		keepLast int
		wantMsgs []string
	}{
		{
			name: "trim preserves complete tool group",
			messages: []entity.Message{
				{Role: valueobject.RoleUser, Content: "old"},
				{Role: valueobject.RoleAssistant, Content: "old reply"},
				{Role: valueobject.RoleUser, Content: "trigger"},
				{Role: valueobject.RoleAssistant, ToolCalls: toolCalls},
				{Role: valueobject.RoleTool, Content: `{"ok":true}`, ToolCallID: "c1"},
				{Role: valueobject.RoleAssistant, Content: "final"},
			},
			keepLast: 3,
			wantMsgs: []string{"trigger", "", `{"ok":true}`, "final"},
		},
		{
			name: "trim removes entire tool group at boundary",
			messages: []entity.Message{
				{Role: valueobject.RoleAssistant, ToolCalls: toolCalls},
				{Role: valueobject.RoleTool, Content: `{"ok":true}`, ToolCallID: "c1"},
				{Role: valueobject.RoleAssistant, Content: "final"},
			},
			keepLast: 1,
			wantMsgs: []string{"final"},
		},
		{
			name: "trim with multiple tool groups keeps last group",
			messages: []entity.Message{
				{Role: valueobject.RoleUser, Content: "q1"},
				{Role: valueobject.RoleAssistant, ToolCalls: toolCalls},
				{Role: valueobject.RoleTool, Content: `{"r":1}`, ToolCallID: "c1"},
				{Role: valueobject.RoleUser, Content: "q2"},
				{Role: valueobject.RoleAssistant, ToolCalls: toolCalls},
				{Role: valueobject.RoleTool, Content: `{"r":2}`, ToolCallID: "c1"},
				{Role: valueobject.RoleAssistant, Content: "done"},
			},
			keepLast: 2,
			wantMsgs: []string{"", `{"r":2}`, "done"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewConversationMemory()
			m.GetOrCreate("s1", "prompt")
			for i := range tt.messages {
				m.Append("s1", &tt.messages[i])
			}

			m.Trim("s1", tt.keepLast)

			history := m.History("s1")
			if len(history) != len(tt.wantMsgs) {
				t.Fatalf("Trim result length = %d, want %d", len(history), len(tt.wantMsgs))
			}
			for i, want := range tt.wantMsgs {
				if history[i].Content != want {
					t.Errorf("history[%d].Content = %q, want %q", i, history[i].Content, want)
				}
			}
		})
	}
}

func TestConversationMemory_Clear(t *testing.T) {
	tests := []struct {
		name      string
		sessionID string
		setup     func(m *ConversationMemory)
		wantLen   int
	}{
		{
			name:      "clears existing session",
			sessionID: "s1",
			setup: func(m *ConversationMemory) {
				m.GetOrCreate("s1", "prompt")
				msg := entity.NewMessage(valueobject.RoleUser, "hello")
				m.Append("s1", &msg)
				msg2 := entity.NewMessage(valueobject.RoleAssistant, "hi")
				m.Append("s1", &msg2)
			},
			wantLen: 0,
		},
		{
			name:      "clear nonexistent session is no-op",
			sessionID: "missing",
			setup:     func(_ *ConversationMemory) {},
			wantLen:   0,
		},
		{
			name:      "clear already empty session",
			sessionID: "s1",
			setup: func(m *ConversationMemory) {
				m.GetOrCreate("s1", "prompt")
			},
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewConversationMemory()
			tt.setup(m)
			m.Clear(tt.sessionID)
			history := m.History(tt.sessionID)
			if len(history) != tt.wantLen {
				t.Errorf("after Clear, len(History) = %d, want %d", len(history), tt.wantLen)
			}
		})
	}
}

func TestConversationMemory_MessageCount(t *testing.T) {
	tests := []struct {
		name      string
		sessionID string
		setup     func(m *ConversationMemory)
		want      int
	}{
		{
			name:      "returns count for existing session",
			sessionID: "s1",
			setup: func(m *ConversationMemory) {
				m.GetOrCreate("s1", "prompt")
				msg := entity.NewMessage(valueobject.RoleUser, "hello")
				m.Append("s1", &msg)
				msg2 := entity.NewMessage(valueobject.RoleAssistant, "hi")
				m.Append("s1", &msg2)
			},
			want: 2,
		},
		{
			name:      "returns 0 for nonexistent session",
			sessionID: "missing",
			setup:     func(_ *ConversationMemory) {},
			want:      0,
		},
		{
			name:      "returns 0 for empty session",
			sessionID: "s1",
			setup: func(m *ConversationMemory) {
				m.GetOrCreate("s1", "prompt")
			},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewConversationMemory()
			tt.setup(m)
			if got := m.MessageCount(tt.sessionID); got != tt.want {
				t.Errorf("MessageCount(%q) = %d, want %d", tt.sessionID, got, tt.want)
			}
		})
	}
}

func TestConversationMemory_Rewind(t *testing.T) {
	tests := []struct {
		name         string
		messages     []entity.Message
		keepMessages int
		wantLen      int
		wantMsgs     []string
	}{
		{
			name: "rewind to first 2 messages",
			messages: []entity.Message{
				{Role: valueobject.RoleUser, Content: "m1"},
				{Role: valueobject.RoleAssistant, Content: "m2"},
				{Role: valueobject.RoleUser, Content: "m3"},
				{Role: valueobject.RoleAssistant, Content: "m4"},
			},
			keepMessages: 2,
			wantLen:      2,
			wantMsgs:     []string{"m1", "m2"},
		},
		{
			name: "rewind with keepMessages >= len is no-op",
			messages: []entity.Message{
				{Role: valueobject.RoleUser, Content: "m1"},
			},
			keepMessages: 10,
			wantLen:      1,
			wantMsgs:     []string{"m1"},
		},
		{
			name: "rewind to 0 clears all",
			messages: []entity.Message{
				{Role: valueobject.RoleUser, Content: "m1"},
				{Role: valueobject.RoleAssistant, Content: "m2"},
			},
			keepMessages: 0,
			wantLen:      0,
		},
		{
			name:         "rewind empty conversation",
			messages:     nil,
			keepMessages: 5,
			wantLen:      0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewConversationMemory()
			m.GetOrCreate("s1", "prompt")
			for i := range tt.messages {
				m.Append("s1", &tt.messages[i])
			}

			m.Rewind("s1", tt.keepMessages)

			history := m.History("s1")
			if len(history) != tt.wantLen {
				t.Fatalf("Rewind result length = %d, want %d", len(history), tt.wantLen)
			}
			for i, want := range tt.wantMsgs {
				if history[i].Content != want {
					t.Errorf("history[%d].Content = %q, want %q", i, history[i].Content, want)
				}
			}
		})
	}
}

func TestConversationMemory_RewindNonexistentSession(t *testing.T) {
	m := NewConversationMemory()
	m.Rewind("missing", 5) // should not panic
}

func TestConversationMemory_ReplaceMessages(t *testing.T) {
	m := NewConversationMemory()
	m.GetOrCreate("s1", "prompt")
	msg := entity.NewMessage(valueobject.RoleUser, "old")
	m.Append("s1", &msg)

	newMsgs := []entity.Message{
		entity.NewMessage(valueobject.RoleUser, "new1"),
		entity.NewMessage(valueobject.RoleAssistant, "new2"),
	}
	m.ReplaceMessages("s1", newMsgs)

	history := m.History("s1")
	if len(history) != 2 {
		t.Fatalf("after ReplaceMessages, len(History) = %d, want 2", len(history))
	}
	if history[0].Content != "new1" || history[1].Content != "new2" {
		t.Errorf("ReplaceMessages did not replace correctly: %+v", history)
	}

	// Verify it's a copy, not a reference
	newMsgs[0].Content = "mutated"
	history2 := m.History("s1")
	if history2[0].Content != "new1" {
		t.Error("ReplaceMessages stored a reference instead of a copy")
	}
}

func TestConversationMemory_TrimNonexistentSession(t *testing.T) {
	m := NewConversationMemory()
	m.Trim("missing", 5) // should not panic
}

func TestConversationMemory_ConcurrentAccess(t *testing.T) {
	m := NewConversationMemory()
	m.GetOrCreate("s1", "prompt")

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(6)
		go func() {
			defer wg.Done()
			msg := entity.NewMessage(valueobject.RoleUser, "concurrent")
			m.Append("s1", &msg)
		}()
		go func() {
			defer wg.Done()
			_ = m.History("s1")
		}()
		go func() {
			defer wg.Done()
			m.Trim("s1", 50)
		}()
		go func() {
			defer wg.Done()
			_ = m.MessageCount("s1")
		}()
		go func() {
			defer wg.Done()
			m.Rewind("s1", 10)
		}()
		go func() {
			defer wg.Done()
			m.Clear("s1")
		}()
	}
	wg.Wait()
}

func TestConversationMemory_AppendCallback_FiresOnSuccess(t *testing.T) {
	m := NewConversationMemory()
	m.GetOrCreate("s1", "prompt")

	var called int
	var gotSessionID string
	var gotContent string
	m.SetOnAppend(func(sid string, msg *entity.Message) {
		called++
		gotSessionID = sid
		gotContent = msg.Content
	})

	msg := entity.NewMessage(valueobject.RoleUser, "hello")
	m.Append("s1", &msg)

	if called != 1 {
		t.Fatalf("callback called %d times, want 1", called)
	}
	if gotSessionID != "s1" {
		t.Errorf("callback sessionID = %q, want %q", gotSessionID, "s1")
	}
	if gotContent != "hello" {
		t.Errorf("callback content = %q, want %q", gotContent, "hello")
	}
}

func TestConversationMemory_AppendCallback_SkipsNonexistentSession(t *testing.T) {
	m := NewConversationMemory()

	var called int
	m.SetOnAppend(func(_ string, _ *entity.Message) {
		called++
	})

	msg := entity.NewMessage(valueobject.RoleUser, "hello")
	m.Append("nonexistent", &msg)

	if called != 0 {
		t.Errorf("callback called %d times for nonexistent session, want 0", called)
	}
}

func TestConversationMemory_Delete(t *testing.T) {
	tests := []struct {
		name      string
		sessionID string
		setup     func(m *ConversationMemory)
	}{
		{
			name:      "deletes existing session with messages",
			sessionID: "s1",
			setup: func(m *ConversationMemory) {
				m.GetOrCreate("s1", "prompt")
				msg := entity.NewMessage(valueobject.RoleUser, "hello")
				m.Append("s1", &msg)
			},
		},
		{
			name:      "deletes empty session",
			sessionID: "s1",
			setup: func(m *ConversationMemory) {
				m.GetOrCreate("s1", "prompt")
			},
		},
		{
			name:      "delete nonexistent session is no-op",
			sessionID: "missing",
			setup:     func(_ *ConversationMemory) {},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewConversationMemory()
			tt.setup(m)

			m.Delete(tt.sessionID)

			if history := m.History(tt.sessionID); history != nil {
				t.Errorf("after Delete, History(%q) = %v, want nil", tt.sessionID, history)
			}
			if count := m.MessageCount(tt.sessionID); count != 0 {
				t.Errorf("after Delete, MessageCount(%q) = %d, want 0", tt.sessionID, count)
			}
		})
	}
}

func TestConversationMemory_Delete_DoesNotAffectOtherSessions(t *testing.T) {
	m := NewConversationMemory()
	m.GetOrCreate("s1", "prompt1")
	m.GetOrCreate("s2", "prompt2")
	msg := entity.NewMessage(valueobject.RoleUser, "hello")
	m.Append("s1", &msg)
	m.Append("s2", &msg)

	m.Delete("s1")

	if history := m.History("s2"); len(history) != 1 {
		t.Errorf("after deleting s1, s2 History length = %d, want 1", len(history))
	}
}

func TestConversationMemory_Delete_AllowsRecreation(t *testing.T) {
	m := NewConversationMemory()
	m.GetOrCreate("s1", "old prompt")
	msg := entity.NewMessage(valueobject.RoleUser, "old message")
	m.Append("s1", &msg)

	m.Delete("s1")

	conv := m.GetOrCreate("s1", "new prompt")
	if conv.SystemPrompt != "new prompt" {
		t.Errorf("recreated session SystemPrompt = %q, want %q", conv.SystemPrompt, "new prompt")
	}
	if len(conv.Messages) != 0 {
		t.Errorf("recreated session has %d messages, want 0", len(conv.Messages))
	}
}
