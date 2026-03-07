package subagent

import (
	"context"
	"fmt"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/output"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

func TestHeuristicCheck_ExplicitTeam(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		task      string
		wantTeam  bool
		wantConf  float64
		wantDefin bool
	}{
		{name: "create a team", task: "Create a team of reviewers to analyze this PR", wantTeam: true, wantConf: 1.0, wantDefin: true},
		{name: "assemble a team", task: "Assemble a team to review the architecture", wantTeam: true, wantConf: 1.0, wantDefin: true},
		{name: "panel of experts", task: "I need a panel of experts to evaluate security", wantTeam: true, wantConf: 1.0, wantDefin: true},
		{name: "group of specialists", task: "Form a group of specialists for this code review", wantTeam: true, wantConf: 1.0, wantDefin: true},
		{name: "from different perspectives", task: "Analyze this code from different perspectives", wantTeam: true, wantConf: 1.0, wantDefin: true},
		{name: "multiple agents", task: "Spawn multiple agents to handle this", wantTeam: true, wantConf: 1.0, wantDefin: true},
		{name: "use team", task: "Use a team to tackle this problem", wantTeam: true, wantConf: 1.0, wantDefin: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			eval, definitive := heuristicCheck(tt.task)
			if definitive != tt.wantDefin {
				t.Fatalf("definitive = %v, want %v", definitive, tt.wantDefin)
			}
			if eval.NeedsTeam != tt.wantTeam {
				t.Errorf("NeedsTeam = %v, want %v", eval.NeedsTeam, tt.wantTeam)
			}
			if eval.Confidence != tt.wantConf {
				t.Errorf("Confidence = %v, want %v", eval.Confidence, tt.wantConf)
			}
		})
	}
}

func TestHeuristicCheck_MultiRole(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		task     string
		wantTeam bool
		wantConf float64
	}{
		{name: "security and performance", task: "Review this for security and performance issues", wantTeam: true, wantConf: 0.8},
		{name: "as a security expert", task: "As a security expert, review this authentication module", wantTeam: true, wantConf: 0.8},
		{name: "review from 3 angles", task: "Review from 3 angles: security, performance, and maintainability", wantTeam: true, wantConf: 0.8},
		{name: "architecture and testing", task: "Evaluate architecture and testing coverage", wantTeam: true, wantConf: 0.8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			eval, definitive := heuristicCheck(tt.task)
			if !definitive {
				t.Fatal("expected definitive result")
			}
			if eval.NeedsTeam != tt.wantTeam {
				t.Errorf("NeedsTeam = %v, want %v", eval.NeedsTeam, tt.wantTeam)
			}
			if eval.Confidence != tt.wantConf {
				t.Errorf("Confidence = %v, want %v", eval.Confidence, tt.wantConf)
			}
		})
	}
}

func TestHeuristicCheck_SimpleTask(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		task string
	}{
		{name: "fix bug", task: "Fix this bug in the login handler"},
		{name: "rename variable", task: "Rename the foo variable"},
		{name: "add comment", task: "Add a comment to explain this function"},
		{name: "update readme", task: "Update the README with new instructions"},
		{name: "delete file", task: "Delete the old config file"},
		{name: "run tests", task: "Run the test suite"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			eval, definitive := heuristicCheck(tt.task)
			if !definitive {
				t.Fatal("expected definitive result for simple task")
			}
			if eval.NeedsTeam {
				t.Error("NeedsTeam = true, want false for simple task")
			}
			if eval.Confidence != 1.0 {
				t.Errorf("Confidence = %v, want 1.0", eval.Confidence)
			}
		})
	}
}

func TestHeuristicCheck_EmptyTask(t *testing.T) {
	t.Parallel()
	eval, definitive := heuristicCheck("")
	if !definitive {
		t.Fatal("expected definitive for empty task")
	}
	if eval.NeedsTeam {
		t.Error("NeedsTeam = true for empty task")
	}
}

func TestHeuristicCheck_ShortAmbiguous(t *testing.T) {
	t.Parallel()
	eval, definitive := heuristicCheck("hello world")
	if !definitive {
		t.Fatal("expected definitive for short non-review task")
	}
	if eval.NeedsTeam {
		t.Error("NeedsTeam should be false for very short task")
	}
	if eval.Confidence != 0.7 {
		t.Errorf("Confidence = %v, want 0.7 for short task", eval.Confidence)
	}
}

func TestHeuristicCheck_Ambiguous(t *testing.T) {
	t.Parallel()

	task := "Please analyze the authentication system in our codebase and provide a comprehensive report on potential improvements to the session management, token refresh flow, and error handling"
	_, definitive := heuristicCheck(task)
	if definitive {
		t.Error("expected ambiguous (non-definitive) result for complex non-keyword task")
	}
}

// classifierMockLLM implements output.LLMClient for team detector tests.
type classifierMockLLM struct {
	response string
	err      error
}

func (m *classifierMockLLM) Complete(_ context.Context, _ *output.ChatRequest) (*output.ChatResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &output.ChatResponse{Content: m.response}, nil
}

func (m *classifierMockLLM) CompleteStream(_ context.Context, _ *output.ChatRequest) (<-chan valueobject.StreamChunk, error) {
	return nil, fmt.Errorf("not implemented")
}

func TestLLMClassify_TeamNeeded(t *testing.T) {
	t.Parallel()

	mock := &classifierMockLLM{
		response: `{
			"needs_team": true,
			"confidence": 0.9,
			"roles": [
				{"name": "security-reviewer", "perspective": "security vulnerabilities", "instruction": "Focus on auth and input validation"},
				{"name": "performance-reviewer", "perspective": "performance bottlenecks", "instruction": "Focus on query optimization"}
			],
			"reasoning": "Complex review benefits from multiple perspectives"
		}`,
	}

	factory := func(_ valueobject.ModelTier) (output.LLMClient, string) {
		return mock, "test-model"
	}
	detector := NewHybridTeamDetector(factory)

	eval, err := detector.llmClassify(context.Background(), "Analyze the entire authentication system")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !eval.NeedsTeam {
		t.Error("NeedsTeam = false, want true")
	}
	if len(eval.Roles) != 2 {
		t.Errorf("len(Roles) = %d, want 2", len(eval.Roles))
	}
	if eval.Confidence != 0.9 {
		t.Errorf("Confidence = %v, want 0.9", eval.Confidence)
	}
}

func TestLLMClassify_NoTeamNeeded(t *testing.T) {
	t.Parallel()

	mock := &classifierMockLLM{
		response: `{"needs_team": false, "confidence": 0.95, "roles": [], "reasoning": "Simple bug fix"}`,
	}

	factory := func(_ valueobject.ModelTier) (output.LLMClient, string) {
		return mock, "test-model"
	}
	detector := NewHybridTeamDetector(factory)

	eval, err := detector.llmClassify(context.Background(), "Fix the null pointer")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eval.NeedsTeam {
		t.Error("NeedsTeam = true, want false")
	}
}

func TestLLMClassify_CodeFenceWrapped(t *testing.T) {
	t.Parallel()

	mock := &classifierMockLLM{
		response: "```json\n{\"needs_team\": true, \"confidence\": 0.85, \"roles\": [], \"reasoning\": \"fenced\"}\n```",
	}

	factory := func(_ valueobject.ModelTier) (output.LLMClient, string) {
		return mock, "test-model"
	}
	detector := NewHybridTeamDetector(factory)

	eval, err := detector.llmClassify(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !eval.NeedsTeam {
		t.Error("NeedsTeam = false after stripping code fence")
	}
}

func TestLLMClassify_InvalidJSON(t *testing.T) {
	t.Parallel()

	mock := &classifierMockLLM{
		response: "I think you should use a team because...",
	}

	factory := func(_ valueobject.ModelTier) (output.LLMClient, string) {
		return mock, "test-model"
	}
	detector := NewHybridTeamDetector(factory)

	eval, err := detector.llmClassify(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eval.NeedsTeam {
		t.Error("NeedsTeam should default to false on invalid JSON")
	}
	if eval.Confidence != 0.5 {
		t.Errorf("Confidence = %v, want 0.5 for invalid JSON fallback", eval.Confidence)
	}
}

func TestLLMClassify_LLMError(t *testing.T) {
	t.Parallel()

	mock := &classifierMockLLM{
		err: fmt.Errorf("API rate limited"),
	}

	factory := func(_ valueobject.ModelTier) (output.LLMClient, string) {
		return mock, "test-model"
	}
	detector := NewHybridTeamDetector(factory)

	_, err := detector.llmClassify(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error from LLM failure")
	}
}

func TestHybridTeamDetector_Evaluate_HeuristicShortCircuit(t *testing.T) {
	t.Parallel()

	callCount := 0
	mock := &classifierMockLLM{
		response: `{"needs_team": true, "confidence": 1.0, "roles": [], "reasoning": "should not be called"}`,
	}

	factory := func(_ valueobject.ModelTier) (output.LLMClient, string) {
		callCount++
		return mock, "test-model"
	}
	detector := NewHybridTeamDetector(factory)

	eval, err := detector.Evaluate(context.Background(), "Fix this bug in the parser")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eval.NeedsTeam {
		t.Error("NeedsTeam = true for simple task")
	}
	if callCount > 0 {
		t.Error("LLM should not be called for heuristically definitive tasks")
	}
}

func TestHybridTeamDetector_Evaluate_FallbackToLLM(t *testing.T) {
	t.Parallel()

	mock := &classifierMockLLM{
		response: `{"needs_team": false, "confidence": 0.8, "roles": [], "reasoning": "single agent sufficient"}`,
	}

	factory := func(_ valueobject.ModelTier) (output.LLMClient, string) {
		return mock, "test-model"
	}
	detector := NewHybridTeamDetector(factory)

	task := "Please analyze the authentication system in our codebase and provide a comprehensive report on potential improvements to the session management, token refresh flow, and error handling"
	eval, err := detector.Evaluate(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eval.Confidence != 0.8 {
		t.Errorf("Confidence = %v, want 0.8 from LLM", eval.Confidence)
	}
}

func TestNoOpTeamDetector(t *testing.T) {
	t.Parallel()

	var detector TeamDetector = NoOpTeamDetector{}
	eval, err := detector.Evaluate(context.Background(), "anything")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eval.NeedsTeam {
		t.Error("NoOp should never need a team")
	}
	if eval.Confidence != 1.0 {
		t.Errorf("Confidence = %v, want 1.0", eval.Confidence)
	}
}

func TestStripCodeFence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "json fence", input: "```json\n{\"key\": true}\n```", want: `{"key": true}`},
		{name: "plain fence", input: "```\n{\"key\": true}\n```", want: `{"key": true}`},
		{name: "no fence", input: `{"key": true}`, want: `{"key": true}`},
		{name: "whitespace", input: "  ```json\n{}\n```  ", want: "{}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := stripCodeFence(tt.input)
			if got != tt.want {
				t.Errorf("stripCodeFence(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// Compile-time interface checks.
var (
	_ TeamDetector = (*HybridTeamDetector)(nil)
	_ TeamDetector = NoOpTeamDetector{}
)
