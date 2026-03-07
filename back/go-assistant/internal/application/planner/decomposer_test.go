package planner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/output"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// --- mock LLM client for tests ---

type mockLLMClient struct {
	responses []string
	errs      []error
	calls     int
}

func (m *mockLLMClient) Complete(_ context.Context, _ *output.ChatRequest) (*output.ChatResponse, error) {
	idx := m.calls
	m.calls++
	if idx < len(m.errs) && m.errs[idx] != nil {
		return nil, m.errs[idx]
	}
	content := ""
	if idx < len(m.responses) {
		content = m.responses[idx]
	}
	return &output.ChatResponse{Content: content}, nil
}

func (m *mockLLMClient) CompleteStream(_ context.Context, _ *output.ChatRequest) (<-chan valueobject.StreamChunk, error) {
	return nil, fmt.Errorf("not implemented")
}

// --- helpers ---

func validPlanJSON() string {
	return `[
		{"id":"task-a","description":"First task","instruction":"Do A"},
		{"id":"task-b","description":"Second task","instruction":"Do B","depends_on":["task-a"],"context_from":["task-a"]}
	]`
}

func parallelPlanJSON() string {
	return `[
		{"id":"root","description":"Root","instruction":"Start"},
		{"id":"worker-1","description":"Worker 1","instruction":"Branch 1","depends_on":["root"]},
		{"id":"worker-2","description":"Worker 2","instruction":"Branch 2","depends_on":["root"]},
		{"id":"synth","description":"Synth","instruction":"Merge","depends_on":["worker-1","worker-2"]}
	]`
}

// --- tests ---

func TestDecomposer_Decompose_ValidJSON(t *testing.T) {
	mock := &mockLLMClient{responses: []string{validPlanJSON()}}
	d := NewDecomposer(mock, "test-model")

	graph, err := d.Decompose(context.Background(), "research something complex and synthesize findings")
	if err != nil {
		t.Fatalf("Decompose() error = %v", err)
	}
	if len(graph.Tasks) != 2 {
		t.Errorf("Tasks len = %d, want 2", len(graph.Tasks))
	}
	if graph.Tasks[0].ID != "task-a" {
		t.Errorf("Tasks[0].ID = %q, want \"task-a\"", graph.Tasks[0].ID)
	}
	if graph.Tasks[1].DependsOn[0] != "task-a" {
		t.Errorf("Tasks[1].DependsOn[0] = %q, want \"task-a\"", graph.Tasks[1].DependsOn[0])
	}
}

func TestDecomposer_Decompose_ParallelGraph_Valid(t *testing.T) {
	mock := &mockLLMClient{responses: []string{parallelPlanJSON()}}
	d := NewDecomposer(mock, "test-model")

	graph, err := d.Decompose(context.Background(), "research 2 topics then merge them into a report")
	if err != nil {
		t.Fatalf("Decompose() error = %v", err)
	}
	if len(graph.Tasks) != 4 {
		t.Errorf("Tasks len = %d, want 4", len(graph.Tasks))
	}
}

func TestDecomposer_Decompose_InvalidJSON_Error(t *testing.T) {
	mock := &mockLLMClient{responses: []string{"not valid json at all"}}
	d := NewDecomposer(mock, "test-model")
	// Disable retries so the test doesn't block.
	d.recoveryCfg.MaxLLMRetries = 0

	_, err := d.Decompose(context.Background(), "research and synthesize")
	if err == nil {
		t.Fatal("Decompose() expected error for invalid JSON, got nil")
	}
}

func TestDecomposer_Decompose_EmptyArray_Error(t *testing.T) {
	mock := &mockLLMClient{responses: []string{"[]"}}
	d := NewDecomposer(mock, "test-model")
	d.recoveryCfg.MaxLLMRetries = 0

	_, err := d.Decompose(context.Background(), "research and analyze")
	if err == nil {
		t.Fatal("Decompose() expected error for empty task list, got nil")
	}
}

func TestDecomposer_Decompose_RetryOnTransientError(t *testing.T) {
	transientErr := errors.New("rate limit exceeded")
	mock := &mockLLMClient{
		errs:      []error{transientErr, nil},
		responses: []string{"", validPlanJSON()},
	}
	d := NewDecomposer(mock, "test-model")
	d.recoveryCfg.MaxLLMRetries = 2
	d.recoveryCfg.BaseDelay = 0 // zero delay for tests

	graph, err := d.Decompose(context.Background(), "research something complex and synthesize")
	if err != nil {
		t.Fatalf("Decompose() after retry error = %v", err)
	}
	if mock.calls != 2 {
		t.Errorf("LLM calls = %d, want 2", mock.calls)
	}
	_ = graph
}

func TestDecomposer_Decompose_MaxRetriesExceeded(t *testing.T) {
	persistentErr := errors.New("rate limit exceeded")
	mock := &mockLLMClient{
		errs: []error{persistentErr, persistentErr, persistentErr},
	}
	d := NewDecomposer(mock, "test-model")
	d.recoveryCfg.MaxLLMRetries = 2
	d.recoveryCfg.BaseDelay = 0

	_, err := d.Decompose(context.Background(), "research and analyze something big")
	if err == nil {
		t.Fatal("Decompose() expected error after max retries, got nil")
	}
}

func TestDecomposer_ParseResponse_ModelTierInvalid_Ignored(t *testing.T) {
	// An invalid model_tier should be silently ignored (fallback to default)
	badTierJSON := `[{"id":"t1","description":"do","instruction":"do it","model_tier":"turbo"}]`
	mock := &mockLLMClient{responses: []string{badTierJSON}}
	d := NewDecomposer(mock, "test-model")

	graph, err := d.Decompose(context.Background(), "research and analyze something big here")
	if err != nil {
		t.Fatalf("Decompose() error = %v", err)
	}
	if graph.Tasks[0].Spec.ModelTier != "" {
		t.Errorf("ModelTier = %q, want empty (fallback)", graph.Tasks[0].Spec.ModelTier)
	}
}

func TestDecomposer_ParseResponse_CyclicGraph_Error(t *testing.T) {
	cyclicJSON, _ := json.Marshal([]microTaskSpec{
		{ID: "A", Description: "A", Instruction: "do a", DependsOn: []string{"B"}},
		{ID: "B", Description: "B", Instruction: "do b", DependsOn: []string{"A"}},
	})
	mock := &mockLLMClient{responses: []string{string(cyclicJSON)}}
	d := NewDecomposer(mock, "test-model")
	d.recoveryCfg.MaxLLMRetries = 0

	_, err := d.Decompose(context.Background(), "research and analyze complex data here")
	if err == nil {
		t.Fatal("Decompose() expected error for cyclic graph, got nil")
	}
}
