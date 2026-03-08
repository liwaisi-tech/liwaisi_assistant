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

// DefaultScoreThreshold is the minimum evaluation score for a plan to pass.
const DefaultScoreThreshold = 0.7

// MaxRefinementIterations is the maximum number of decompose→evaluate loops.
const MaxRefinementIterations = 2

const evaluatorPromptTemplate = `You are a plan quality evaluator. Assess the following execution plan
for a given task, considering the team of experts involved.

## Task
%s

## Expert Team
%s

## Execution Plan (DAG of Micro-Tasks)
%s

## Evaluation Criteria
Score each dimension from 0.0 to 1.0:
1. **Completeness**: Does the plan cover all aspects of the task?
2. **Feasibility**: Are the micro-tasks realistic and achievable?
3. **Granularity**: Are tasks appropriately sized (not too broad, not too narrow)?
4. **Risk**: Are potential failure points identified and mitigated?

Return ONLY valid JSON matching this schema:
{
  "score": 0.85,
  "completeness": 0.9,
  "feasibility": 0.8,
  "granularity": 0.85,
  "risk": 0.85,
  "feedback": "specific, actionable feedback for improving the plan",
  "passed": true
}

Rules:
- "score" is the weighted average of all dimensions
- "passed" should be true if score >= 0.7, false otherwise
- "feedback" must provide specific improvement suggestions if score < 0.7
- If the plan is excellent, keep feedback brief and positive`

// PlanEvaluation contains the result of evaluating a plan's quality.
type PlanEvaluation struct {
	Score        float64 `json:"score"`
	Completeness float64 `json:"completeness"`
	Feasibility  float64 `json:"feasibility"`
	Granularity  float64 `json:"granularity"`
	Risk         float64 `json:"risk"`
	Feedback     string  `json:"feedback"`
	Passed       bool    `json:"passed"`
}

// PlanEvaluator implements Phase 3 of the improved planner pipeline.
// It scores the plan on completeness, feasibility, granularity, and risk.
// If the score falls below the threshold, feedback is provided for refinement.
type PlanEvaluator struct {
	client         output.LLMClient
	model          string
	scoreThreshold float64
	recoveryCfg    subagent.RecoveryConfig
}

// NewPlanEvaluator creates a PlanEvaluator with the default score threshold.
func NewPlanEvaluator(client output.LLMClient, model string) *PlanEvaluator {
	return &PlanEvaluator{
		client:         client,
		model:          model,
		scoreThreshold: DefaultScoreThreshold,
		recoveryCfg:    subagent.DefaultRecoveryConfig(),
	}
}

// Evaluate scores the plan and returns the evaluation result.
func (e *PlanEvaluator) Evaluate(
	ctx context.Context,
	task string,
	roles []valueobject.RoleSpec,
	graph *entity.PlanGraph,
) (PlanEvaluation, error) {
	teamSection := formatRolesForPrompt(roles)
	planSection := formatGraphForPrompt(graph)
	systemPrompt := fmt.Sprintf(evaluatorPromptTemplate, task, teamSection, planSection)

	eval, err := subagent.RetryLLMCall(ctx, e.recoveryCfg, func(ctx context.Context) (PlanEvaluation, error) {
		resp, err := e.client.Complete(ctx, &output.ChatRequest{
			Model: e.model,
			Messages: []entity.Message{
				entity.NewMessage(valueobject.RoleSystem, systemPrompt),
				entity.NewMessage(valueobject.RoleUser, "Evaluate this plan."),
			},
			Temperature: 0.1,
			MaxTokens:   1024,
		})
		if err != nil {
			return PlanEvaluation{}, fmt.Errorf("plan evaluator LLM call: %w", err)
		}
		return e.parseResponse(resp.Content)
	})
	if err != nil {
		return PlanEvaluation{}, err
	}

	// Override Passed based on our threshold, in case the LLM disagrees.
	eval.Passed = eval.Score >= e.scoreThreshold
	return eval, nil
}

// parseResponse extracts the PlanEvaluation from the LLM output.
func (e *PlanEvaluator) parseResponse(raw string) (PlanEvaluation, error) {
	content := strings.TrimSpace(raw)
	content = stripJSONCodeFence(content)

	var eval PlanEvaluation
	if err := json.Unmarshal([]byte(content), &eval); err != nil {
		return PlanEvaluation{}, fmt.Errorf("plan evaluator: invalid JSON from LLM: %w", err)
	}

	// Clamp score to [0, 1].
	if eval.Score < 0 {
		eval.Score = 0
	}
	if eval.Score > 1 {
		eval.Score = 1
	}

	return eval, nil
}

// formatGraphForPrompt converts a PlanGraph into a readable section for the prompt.
func formatGraphForPrompt(graph *entity.PlanGraph) string {
	if graph == nil || len(graph.Tasks) == 0 {
		return "(Empty plan)"
	}
	var sb strings.Builder
	for i, t := range graph.Tasks {
		deps := "none"
		if len(t.DependsOn) > 0 {
			deps = strings.Join(t.DependsOn, ", ")
		}
		sb.WriteString(fmt.Sprintf("%d. [%s] %s (depends on: %s)\n", i+1, t.ID, t.Description, deps))
	}
	return sb.String()
}
