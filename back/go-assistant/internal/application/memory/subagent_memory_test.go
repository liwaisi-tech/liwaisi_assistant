package memory

import (
	"strings"
	"sync"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

func TestChildSessionID_Format(t *testing.T) {
	tests := []struct {
		name            string
		parentSessionID string
		agentName       string
		wantPrefix      string
	}{
		{
			name:            "standard inputs",
			parentSessionID: "parent-123",
			agentName:       "reviewer",
			wantPrefix:      "parent-123:subagent:reviewer:",
		},
		{
			name:            "parent with colons",
			parentSessionID: "session:abc:def",
			agentName:       "explorer",
			wantPrefix:      "session:abc:def:subagent:explorer:",
		},
		{
			name:            "empty parent session ID",
			parentSessionID: "",
			agentName:       "analyzer",
			wantPrefix:      ":subagent:analyzer:",
		},
		{
			name:            "empty agent name",
			parentSessionID: "parent-123",
			agentName:       "",
			wantPrefix:      "parent-123:subagent::",
		},
		{
			name:            "both empty",
			parentSessionID: "",
			agentName:       "",
			wantPrefix:      ":subagent::",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := ChildSessionID(tt.parentSessionID, tt.agentName)

			if !strings.HasPrefix(id, tt.wantPrefix) {
				t.Errorf("ChildSessionID(%q, %q) = %q, want prefix %q",
					tt.parentSessionID, tt.agentName, id, tt.wantPrefix)
			}

			if !strings.Contains(id, ":subagent:") {
				t.Errorf("ChildSessionID result %q missing :subagent: separator", id)
			}

			suffix := strings.TrimPrefix(id, tt.wantPrefix)
			if len(suffix) != 8 {
				t.Errorf("random suffix length = %d, want 8 (got suffix %q)", len(suffix), suffix)
			}
		})
	}
}

func TestChildSessionID_Uniqueness(t *testing.T) {
	const iterations = 100
	seen := make(map[string]struct{}, iterations)

	for i := 0; i < iterations; i++ {
		id := ChildSessionID("parent", "agent")
		if _, exists := seen[id]; exists {
			t.Fatalf("duplicate ChildSessionID on iteration %d: %q", i, id)
		}
		seen[id] = struct{}{}
	}
}

func TestChildSessionID_Parseable(t *testing.T) {
	id := ChildSessionID("my-session", "code-reviewer")
	parts := strings.SplitN(id, ":subagent:", 2)

	if len(parts) != 2 {
		t.Fatalf("cannot split %q on :subagent:, got %d parts", id, len(parts))
	}

	if parts[0] != "my-session" {
		t.Errorf("extracted parent ID = %q, want %q", parts[0], "my-session")
	}

	nameParts := strings.SplitN(parts[1], ":", 2)
	if len(nameParts) != 2 {
		t.Fatalf("cannot split agent suffix %q on ':', got %d parts", parts[1], len(nameParts))
	}
	if nameParts[0] != "code-reviewer" {
		t.Errorf("extracted agent name = %q, want %q", nameParts[0], "code-reviewer")
	}
}

func TestSubAgentMemoryFactory_CreateChildContext(t *testing.T) {
	tests := []struct {
		name           string
		parentMessages int
		spec           entity.SubAgentSpec
		wantMsgCount   int
		wantContext    bool
	}{
		{
			name:           "with context seeds system prompt and context message",
			parentMessages: 5,
			spec: entity.SubAgentSpec{
				Name:        "reviewer",
				Instruction: "You are a code reviewer",
				Context:     "Review this file: main.go",
			},
			wantMsgCount: 1,
			wantContext:  true,
		},
		{
			name:           "without context seeds only system prompt",
			parentMessages: 5,
			spec: entity.SubAgentSpec{
				Name:        "explorer",
				Instruction: "You explore codebases",
			},
			wantMsgCount: 0,
			wantContext:  false,
		},
		{
			name:           "empty context string treated as no context",
			parentMessages: 3,
			spec: entity.SubAgentSpec{
				Name:        "helper",
				Instruction: "You help with tasks",
				Context:     "",
			},
			wantMsgCount: 0,
			wantContext:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mem := NewConversationMemory()
			parentID := "parent-session"
			mem.GetOrCreate(parentID, "Parent system prompt")
			for i := 0; i < tt.parentMessages; i++ {
				msg := entity.NewMessage(valueobject.RoleUser, "parent message")
				mem.Append(parentID, &msg)
			}

			factory := NewSubAgentMemoryFactory(mem)
			childID := factory.CreateChildContext(parentID, &tt.spec)

			if childID == "" {
				t.Fatal("CreateChildContext returned empty child ID")
			}

			history := factory.ChildHistory(childID)
			if len(history) != tt.wantMsgCount {
				t.Errorf("child has %d messages, want %d", len(history), tt.wantMsgCount)
			}

			if tt.wantContext && len(history) > 0 {
				if history[0].Role != valueobject.RoleUser {
					t.Errorf("context message role = %q, want %q", history[0].Role, valueobject.RoleUser)
				}
				if history[0].Content != tt.spec.Context {
					t.Errorf("context message content = %q, want %q", history[0].Content, tt.spec.Context)
				}
			}

			parentHistory := mem.History(parentID)
			if len(parentHistory) != tt.parentMessages {
				t.Errorf("parent has %d messages, want %d", len(parentHistory), tt.parentMessages)
			}
		})
	}
}

func TestSubAgentMemoryFactory_SystemPrompt(t *testing.T) {
	mem := NewConversationMemory()
	factory := NewSubAgentMemoryFactory(mem)

	spec := entity.SubAgentSpec{
		Name:        "security-reviewer",
		Instruction: "You are a security expert. Analyze code for vulnerabilities.",
	}

	childID := factory.CreateChildContext("parent", &spec)

	mem.mu.RLock()
	conv, ok := mem.sessions[childID]
	mem.mu.RUnlock()

	if !ok {
		t.Fatal("child session not found in memory")
	}
	if conv.SystemPrompt != spec.Instruction {
		t.Errorf("child SystemPrompt = %q, want %q", conv.SystemPrompt, spec.Instruction)
	}
}

func TestSubAgentMemoryFactory_Isolation(t *testing.T) {
	mem := NewConversationMemory()
	parentID := "parent-session"
	mem.GetOrCreate(parentID, "Parent system prompt")

	for i := 0; i < 10; i++ {
		msg := entity.NewMessage(valueobject.RoleUser, "parent message")
		mem.Append(parentID, &msg)
		resp := entity.NewMessage(valueobject.RoleAssistant, "parent reply")
		mem.Append(parentID, &resp)
	}

	factory := NewSubAgentMemoryFactory(mem)
	spec := entity.SubAgentSpec{
		Name:        "child-agent",
		Instruction: "Child instruction",
		Context:     "Task context",
	}

	childID := factory.CreateChildContext(parentID, &spec)

	childHistory := factory.ChildHistory(childID)
	if len(childHistory) != 1 {
		t.Fatalf("child has %d messages, want 1 (context only)", len(childHistory))
	}

	for _, msg := range childHistory {
		if msg.Content == "parent message" || msg.Content == "parent reply" {
			t.Errorf("child history contains parent message: %q", msg.Content)
		}
	}

	parentHistory := mem.History(parentID)
	if len(parentHistory) != 20 {
		t.Errorf("parent history changed: got %d messages, want 20", len(parentHistory))
	}
}

func TestSubAgentMemoryFactory_ChildHistory(t *testing.T) {
	mem := NewConversationMemory()
	factory := NewSubAgentMemoryFactory(mem)

	spec := entity.SubAgentSpec{
		Name:        "test-agent",
		Instruction: "Test instruction",
		Context:     "Initial context",
	}

	childID := factory.CreateChildContext("parent", &spec)

	msg := entity.NewMessage(valueobject.RoleAssistant, "I'll analyze this")
	mem.Append(childID, &msg)
	msg2 := entity.NewMessage(valueobject.RoleUser, "What did you find?")
	mem.Append(childID, &msg2)

	history := factory.ChildHistory(childID)
	if len(history) != 3 {
		t.Fatalf("ChildHistory length = %d, want 3", len(history))
	}
	if history[0].Content != "Initial context" {
		t.Errorf("history[0].Content = %q, want %q", history[0].Content, "Initial context")
	}
	if history[1].Content != "I'll analyze this" {
		t.Errorf("history[1].Content = %q, want %q", history[1].Content, "I'll analyze this")
	}
	if history[2].Content != "What did you find?" {
		t.Errorf("history[2].Content = %q, want %q", history[2].Content, "What did you find?")
	}
}

func TestSubAgentMemoryFactory_CleanupChild(t *testing.T) {
	mem := NewConversationMemory()
	factory := NewSubAgentMemoryFactory(mem)

	spec := entity.SubAgentSpec{
		Name:        "ephemeral-agent",
		Instruction: "Temporary task",
		Context:     "Do something",
	}

	childID := factory.CreateChildContext("parent", &spec)

	if history := factory.ChildHistory(childID); history == nil {
		t.Fatal("child session should exist before cleanup")
	}

	factory.CleanupChild(childID)

	if history := factory.ChildHistory(childID); history != nil {
		t.Errorf("after CleanupChild, ChildHistory = %v, want nil", history)
	}

	if count := mem.MessageCount(childID); count != 0 {
		t.Errorf("after CleanupChild, MessageCount = %d, want 0", count)
	}
}

func TestSubAgentMemoryFactory_CleanupChild_DoesNotAffectParent(t *testing.T) {
	mem := NewConversationMemory()
	parentID := "parent-session"
	mem.GetOrCreate(parentID, "Parent prompt")
	msg := entity.NewMessage(valueobject.RoleUser, "parent msg")
	mem.Append(parentID, &msg)

	factory := NewSubAgentMemoryFactory(mem)
	spec := entity.SubAgentSpec{
		Name:        "temp",
		Instruction: "Temp instruction",
	}

	childID := factory.CreateChildContext(parentID, &spec)
	factory.CleanupChild(childID)

	parentHistory := mem.History(parentID)
	if len(parentHistory) != 1 {
		t.Errorf("parent history after child cleanup: got %d messages, want 1", len(parentHistory))
	}
}

func TestSubAgentMemoryFactory_MultipleChildren(t *testing.T) {
	mem := NewConversationMemory()
	factory := NewSubAgentMemoryFactory(mem)
	parentID := "parent"

	specs := []entity.SubAgentSpec{
		{Name: "reviewer", Instruction: "Review code", Context: "file1.go"},
		{Name: "tester", Instruction: "Write tests", Context: "file2.go"},
		{Name: "documenter", Instruction: "Write docs", Context: "file3.go"},
	}

	childIDs := make([]string, len(specs))
	for i := range specs {
		childIDs[i] = factory.CreateChildContext(parentID, &specs[i])
	}

	for i := 0; i < len(childIDs); i++ {
		for j := i + 1; j < len(childIDs); j++ {
			if childIDs[i] == childIDs[j] {
				t.Errorf("child IDs %d and %d are identical: %q", i, j, childIDs[i])
			}
		}
	}

	for i, childID := range childIDs {
		history := factory.ChildHistory(childID)
		if len(history) != 1 {
			t.Errorf("child %d has %d messages, want 1", i, len(history))
			continue
		}
		if history[0].Content != specs[i].Context {
			t.Errorf("child %d context = %q, want %q", i, history[0].Content, specs[i].Context)
		}
	}
}

func TestSubAgentMemoryFactory_ConcurrentCreation(t *testing.T) {
	mem := NewConversationMemory()
	factory := NewSubAgentMemoryFactory(mem)

	const goroutines = 50
	var wg sync.WaitGroup
	childIDs := make([]string, goroutines)
	errs := make([]error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			spec := entity.SubAgentSpec{
				Name:        "concurrent-agent",
				Instruction: "Do concurrent work",
				Context:     "task context",
			}
			childIDs[idx] = factory.CreateChildContext("parent", &spec)

			history := factory.ChildHistory(childIDs[idx])
			if len(history) != 1 {
				errs[idx] = &concurrentError{idx: idx, got: len(history)}
			}
		}(i)
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			t.Error(err)
		}
	}

	seen := make(map[string]struct{}, goroutines)
	for i, id := range childIDs {
		if _, exists := seen[id]; exists {
			t.Errorf("duplicate child ID at index %d: %q", i, id)
		}
		seen[id] = struct{}{}
	}
}

type concurrentError struct {
	idx int
	got int
}

func (e *concurrentError) Error() string {
	return "goroutine " + string(rune('0'+e.idx)) + ": child has wrong message count"
}

func TestSubAgentMemoryFactory_ConcurrentCleanup(t *testing.T) {
	mem := NewConversationMemory()
	factory := NewSubAgentMemoryFactory(mem)

	const goroutines = 50
	childIDs := make([]string, goroutines)
	for i := 0; i < goroutines; i++ {
		spec := entity.SubAgentSpec{
			Name:        "cleanup-agent",
			Instruction: "Cleanup work",
			Context:     "task",
		}
		childIDs[i] = factory.CreateChildContext("parent", &spec)
	}

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			factory.CleanupChild(childIDs[idx])
		}(i)
	}
	wg.Wait()

	for i, id := range childIDs {
		if history := factory.ChildHistory(id); history != nil {
			t.Errorf("child %d still has history after concurrent cleanup", i)
		}
	}
}
