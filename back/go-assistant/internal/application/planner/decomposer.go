package planner

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/subagent"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/output"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// microTaskSpec is the JSON schema returned by the LLM decomposer.
type microTaskSpec struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Instruction string   `json:"instruction"`
	DependsOn   []string `json:"depends_on,omitempty"`
	ContextFrom []string `json:"context_from,omitempty"`
	ModelTier   string   `json:"model_tier,omitempty"`
}

const decomposerSystemPrompt = `You are a task decomposition expert.
Given a user task, decide which specialist subagents are needed and which
parts of the work can happen in parallel vs sequentially.

Return a JSON array of micro-tasks. Each element must have:
  "id"          – unique kebab-case identifier (no spaces)
  "description" – one-sentence summary for the human
  "instruction" – detailed instruction for the subagent
  "depends_on"  – list of ids that must complete first (omit if none)
  "context_from"– list of ids whose output to inject as context (omit if none)
  "model_tier"  – "fast", "balanced", or "capable" (omit to use default)

Rules:
- Return ONLY the JSON array, nothing else.
- If tasks are independent, omit depends_on entirely.
- Keep the plan flat; do not nest tasks.
- Minimize the number of sequential dependencies.`

// Decomposer uses an LLM to decompose a complex task into a PlanGraph.
type Decomposer struct {
	client      output.LLMClient
	model       string
	maxRetries  int
	recoveryCfg subagent.RecoveryConfig
}

// NewDecomposer creates a Decomposer backed by the given LLM client.
func NewDecomposer(client output.LLMClient, model string) *Decomposer {
	cfg := subagent.DefaultRecoveryConfig()
	return &Decomposer{
		client:      client,
		model:       model,
		maxRetries:  cfg.MaxLLMRetries,
		recoveryCfg: cfg,
	}
}

// Decompose sends the task to the LLM and returns a validated PlanGraph.
// It retries transient errors according to the default RecoveryConfig.
func (d *Decomposer) Decompose(ctx context.Context, task string) (*entity.PlanGraph, error) {
	userMsg := fmt.Sprintf("Task to decompose into micro-tasks:\n\n%s", task)

	graph, err := subagent.RetryLLMCall(ctx, d.recoveryCfg, func(ctx context.Context) (*entity.PlanGraph, error) {
		resp, err := d.client.Complete(ctx, &output.ChatRequest{
			Model: d.model,
			Messages: []entity.Message{
				entity.NewMessage(valueobject.RoleSystem, decomposerSystemPrompt),
				entity.NewMessage(valueobject.RoleUser, userMsg),
			},
			Temperature: 0.2,
			MaxTokens:   2048,
		})
		if err != nil {
			return nil, fmt.Errorf("decomposer LLM call: %w", err)
		}
		return d.parseResponse(resp.Content)
	})
	if err != nil {
		return nil, err
	}
	return graph, nil
}

// parseResponse converts the raw LLM JSON output into a validated PlanGraph.
func (d *Decomposer) parseResponse(raw string) (*entity.PlanGraph, error) {
	var specs []microTaskSpec
	if err := json.Unmarshal([]byte(raw), &specs); err != nil {
		return nil, fmt.Errorf("decomposer: invalid JSON from LLM: %w", err)
	}
	if len(specs) == 0 {
		return nil, fmt.Errorf("decomposer: LLM returned empty task list")
	}

	tasks := make([]*entity.MicroTask, 0, len(specs))
	for _, s := range specs {
		tier := valueobject.ModelTier(s.ModelTier)
		if tier != "" && !tier.IsValid() {
			tier = "" // fall back to default
		}
		mt := &entity.MicroTask{
			ID:          s.ID,
			Description: s.Description,
			Spec: entity.SubAgentSpec{
				Name:        s.ID,
				Description: s.Description,
				Instruction: s.Instruction,
				ModelTier:   tier,
			},
			DependsOn:   s.DependsOn,
			ContextFrom: s.ContextFrom,
		}
		tasks = append(tasks, mt)
	}

	graph := &entity.PlanGraph{Tasks: tasks}
	if err := graph.Validate(); err != nil {
		return nil, fmt.Errorf("decomposer: plan graph invalid: %w", err)
	}
	return graph, nil
}
