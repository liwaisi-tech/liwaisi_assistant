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

const teamAssemblerPrompt = `You are a team composition expert. Given a task, identify which specialist
roles would produce the best plan for decomposing and executing this work.

Apply the LLM-as-simulator principle: "What would be a good group of specialists
to solve this problem? What unique perspective would each bring?"

Always include at minimum:
- A Software Architect (system design, dependencies, structure)
- A QA Engineer (testing strategy, edge cases, validation)

Add additional roles ONLY when the task genuinely requires them. Examples:
- DevOps Engineer: for deployment, CI/CD, infrastructure tasks
- Security Engineer: for authentication, authorization, data protection
- UX/CLI Designer: for user-facing interface changes
- Database Engineer: for data modeling, migration, query optimization
- Performance Engineer: for optimization, benchmarking, profiling

Return ONLY valid JSON matching this schema:
{
  "needs_team": true,
  "confidence": 0.9,
  "roles": [
    {
      "name": "kebab-case-name",
      "perspective": "what this specialist focuses on",
      "instruction": "specific guidance for this role"
    }
  ],
  "reasoning": "brief explanation of why these roles were chosen"
}

Rules:
- Always set needs_team to true (the gate already filtered simple tasks).
- Include 2-5 roles. More roles means higher LLM cost with diminishing returns.
- Each role must have a unique perspective that adds value to the plan.`

// TeamAssembler implements Phase 1 of the improved planner pipeline.
// It identifies which specialist roles are needed to plan the given task,
// using the LLM-as-simulator philosophy from llms.txt.
type TeamAssembler struct {
	client      output.LLMClient
	model       string
	recoveryCfg subagent.RecoveryConfig
}

// NewTeamAssembler creates a TeamAssembler backed by the given LLM client.
func NewTeamAssembler(client output.LLMClient, model string) *TeamAssembler {
	return &TeamAssembler{
		client:      client,
		model:       model,
		recoveryCfg: subagent.DefaultRecoveryConfig(),
	}
}

// Assemble identifies the specialist roles needed for the task.
// It always returns at least Software Architect and QA Engineer roles.
func (a *TeamAssembler) Assemble(ctx context.Context, task string) (valueobject.TeamEvaluation, error) {
	eval, err := subagent.RetryLLMCall(ctx, a.recoveryCfg, func(ctx context.Context) (valueobject.TeamEvaluation, error) {
		resp, err := a.client.Complete(ctx, &output.ChatRequest{
			Model: a.model,
			Messages: []entity.Message{
				entity.NewMessage(valueobject.RoleSystem, teamAssemblerPrompt),
				entity.NewMessage(valueobject.RoleUser, task),
			},
			Temperature: 0.3,
			MaxTokens:   1024,
		})
		if err != nil {
			return valueobject.TeamEvaluation{}, fmt.Errorf("team assembler LLM call: %w", err)
		}
		return a.parseResponse(resp.Content)
	})
	if err != nil {
		return valueobject.TeamEvaluation{}, err
	}
	return eval, nil
}

// parseResponse extracts and validates the TeamEvaluation from the LLM output.
func (a *TeamAssembler) parseResponse(raw string) (valueobject.TeamEvaluation, error) {
	content := strings.TrimSpace(raw)
	content = stripJSONCodeFence(content)

	var eval valueobject.TeamEvaluation
	if err := json.Unmarshal([]byte(content), &eval); err != nil {
		return valueobject.TeamEvaluation{}, fmt.Errorf("team assembler: invalid JSON from LLM: %w", err)
	}

	// Enforce minimum team: architect + QA.
	eval = ensureMinimumTeam(eval)

	return eval, nil
}

// ensureMinimumTeam guarantees the evaluation always includes at least
// a Software Architect and a QA Engineer.
func ensureMinimumTeam(eval valueobject.TeamEvaluation) valueobject.TeamEvaluation {
	eval.NeedsTeam = true
	hasArchitect := false
	hasQA := false

	for _, r := range eval.Roles {
		lower := strings.ToLower(r.Name)
		if strings.Contains(lower, "architect") {
			hasArchitect = true
		}
		if strings.Contains(lower, "qa") || strings.Contains(lower, "quality") || strings.Contains(lower, "testing") {
			hasQA = true
		}
	}

	if !hasArchitect {
		eval.Roles = append(eval.Roles, valueobject.RoleSpec{
			Name:        "software-architect",
			Perspective: "system design, dependencies, and structural integrity",
			Instruction: "Evaluate the task from an architectural standpoint: identify components, interfaces, and dependencies.",
		})
	}
	if !hasQA {
		eval.Roles = append(eval.Roles, valueobject.RoleSpec{
			Name:        "qa-engineer",
			Perspective: "testing strategy, edge cases, and validation",
			Instruction: "Identify testing requirements, edge cases, and validation criteria for each micro-task.",
		})
	}

	return eval
}

// stripJSONCodeFence removes optional markdown code fences from LLM output.
func stripJSONCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```json") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	} else if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	return s
}
