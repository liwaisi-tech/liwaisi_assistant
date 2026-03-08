package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/subagent"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/output"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

const enhancedDecomposerPromptTemplate = `You are a task decomposition expert working with a team of specialists.
Given a user task and the expert team assembled for it, decompose the task
into micro-tasks that form a directed acyclic graph (DAG).

## Expert Team
%s

## Instructions
Consider each expert's perspective when creating the decomposition:
- Incorporate their specific concerns and constraints
- Ensure the plan addresses each expert's domain requirements
- Create tasks that align with each expert's area of responsibility

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
- Minimize the number of sequential dependencies.
- Ensure every expert's perspective is reflected in at least one task.`

// EnhancedDecomposer implements Phase 2 of the improved planner pipeline.
// It enriches the decomposition prompt with each expert's perspective and
// constraints from the team assembled in Phase 1.
type EnhancedDecomposer struct {
	client      output.LLMClient
	model       string
	recoveryCfg subagent.RecoveryConfig
}

// NewEnhancedDecomposer creates an EnhancedDecomposer backed by the given LLM client.
func NewEnhancedDecomposer(client output.LLMClient, model string) *EnhancedDecomposer {
	cfg := subagent.DefaultRecoveryConfig()
	return &EnhancedDecomposer{
		client:      client,
		model:       model,
		recoveryCfg: cfg,
	}
}

// Decompose sends the task and team roles to the LLM and returns a validated PlanGraph.
// It retries transient errors according to the default RecoveryConfig.
func (d *EnhancedDecomposer) Decompose(ctx context.Context, task string, roles []valueobject.RoleSpec) (*entity.PlanGraph, error) {
	teamSection := formatRolesForPrompt(roles)
	systemPrompt := fmt.Sprintf(enhancedDecomposerPromptTemplate, teamSection)
	userMsg := fmt.Sprintf("Task to decompose into micro-tasks:\n\n%s", task)

	graph, err := subagent.RetryLLMCall(ctx, d.recoveryCfg, func(ctx context.Context) (*entity.PlanGraph, error) {
		resp, err := d.client.Complete(ctx, &output.ChatRequest{
			Model: d.model,
			Messages: []entity.Message{
				entity.NewMessage(valueobject.RoleSystem, systemPrompt),
				entity.NewMessage(valueobject.RoleUser, userMsg),
			},
			Temperature: 0.2,
			MaxTokens:   2048,
		})
		if err != nil {
			return nil, fmt.Errorf("enhanced decomposer LLM call: %w", err)
		}
		return d.parseResponse(resp.Content)
	})
	if err != nil {
		return nil, err
	}
	return graph, nil
}

// DecomposeWithFeedback performs decomposition incorporating evaluator feedback
// from a previous iteration. Used in the refinement loop (Phase 3 → Phase 2).
func (d *EnhancedDecomposer) DecomposeWithFeedback(ctx context.Context, task string, roles []valueobject.RoleSpec, feedback string) (*entity.PlanGraph, error) {
	teamSection := formatRolesForPrompt(roles)
	systemPrompt := fmt.Sprintf(enhancedDecomposerPromptTemplate, teamSection)
	userMsg := fmt.Sprintf("Task to decompose into micro-tasks:\n\n%s\n\n## Previous Evaluation Feedback\n\nThe previous plan was rejected. Address the following feedback:\n%s", task, feedback)

	graph, err := subagent.RetryLLMCall(ctx, d.recoveryCfg, func(ctx context.Context) (*entity.PlanGraph, error) {
		resp, err := d.client.Complete(ctx, &output.ChatRequest{
			Model: d.model,
			Messages: []entity.Message{
				entity.NewMessage(valueobject.RoleSystem, systemPrompt),
				entity.NewMessage(valueobject.RoleUser, userMsg),
			},
			Temperature: 0.3,
			MaxTokens:   2048,
		})
		if err != nil {
			return nil, fmt.Errorf("enhanced decomposer LLM call (with feedback): %w", err)
		}
		return d.parseResponse(resp.Content)
	})
	if err != nil {
		return nil, err
	}
	return graph, nil
}

// parseResponse converts the raw LLM JSON output into a validated PlanGraph.
func (d *EnhancedDecomposer) parseResponse(raw string) (*entity.PlanGraph, error) {
	content := strings.TrimSpace(raw)
	content = stripJSONCodeFence(content)

	var specs []microTaskSpec
	if err := json.Unmarshal([]byte(content), &specs); err != nil {
		return nil, fmt.Errorf("enhanced decomposer: invalid JSON from LLM: %w", err)
	}
	if len(specs) == 0 {
		return nil, fmt.Errorf("enhanced decomposer: LLM returned empty task list")
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
		return nil, fmt.Errorf("enhanced decomposer: plan graph invalid: %w", err)
	}
	return graph, nil
}

// formatRolesForPrompt converts a slice of RoleSpecs into a Markdown section
// for injection into the system prompt.
func formatRolesForPrompt(roles []valueobject.RoleSpec) string {
	if len(roles) == 0 {
		return "(No specific roles identified)"
	}
	var sb strings.Builder
	for i, r := range roles {
		sb.WriteString(fmt.Sprintf("%d. **%s** — %s\n", i+1, r.Name, r.Perspective))
		if r.Instruction != "" {
			sb.WriteString(fmt.Sprintf("   Constraint: %s\n", r.Instruction))
		}
	}
	return sb.String()
}
