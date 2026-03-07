package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/output"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// TeamDetector evaluates whether a task requires a multi-disciplinary team.
type TeamDetector interface {
	Evaluate(ctx context.Context, task string) (valueobject.TeamEvaluation, error)
}

// HybridTeamDetector uses deterministic heuristics first, falling back to
// LLM classification only when heuristics are ambiguous. This keeps the
// common case at zero LLM cost.
type HybridTeamDetector struct {
	clientFactory LLMClientFactory
}

// NewHybridTeamDetector creates a detector backed by the given LLM client
// factory for the fallback classifier.
func NewHybridTeamDetector(factory LLMClientFactory) *HybridTeamDetector {
	return &HybridTeamDetector{clientFactory: factory}
}

// Evaluate checks heuristics first, then falls back to LLM classification
// for ambiguous tasks.
func (d *HybridTeamDetector) Evaluate(ctx context.Context, task string) (valueobject.TeamEvaluation, error) {
	eval, definitive := heuristicCheck(task)
	if definitive {
		return eval, nil
	}
	return d.llmClassify(ctx, task)
}

// Compiled regex patterns for team detection heuristics.
var (
	explicitTeamPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(create|assemble|form|build|use)\s+(a\s+)?team\b`),
		regexp.MustCompile(`(?i)\bpanel\s+of\s+(experts?|specialists?|reviewers?)\b`),
		regexp.MustCompile(`(?i)\bgroup\s+of\s+(experts?|specialists?|reviewers?)\b`),
		regexp.MustCompile(`(?i)\bfrom\s+different\s+perspectives?\b`),
		regexp.MustCompile(`(?i)\bmultiple\s+(experts?|specialists?|reviewers?|agents?)\b`),
		regexp.MustCompile(`(?i)\bspawn\s+(multiple\s+)?agents?\b`),
	}

	multiRolePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(security|performance|architecture|testing|accessibility)\s+and\s+(security|performance|architecture|testing|accessibility)\b`),
		regexp.MustCompile(`(?i)\breview\s+from\s+\d+\s+(angles?|perspectives?|viewpoints?)\b`),
		regexp.MustCompile(`(?i)\bas\s+a\s+(security|performance|database|frontend|backend|devops|testing)\s+(expert|engineer|specialist|reviewer)\b`),
	}

	simpleTaskIndicators = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^fix\s+(this|the|a)\s+`),
		regexp.MustCompile(`(?i)^rename\s+`),
		regexp.MustCompile(`(?i)^add\s+a\s+comment\b`),
		regexp.MustCompile(`(?i)^update\s+(the\s+)?(readme|docs?|documentation)\b`),
		regexp.MustCompile(`(?i)^(delete|remove)\s+`),
		regexp.MustCompile(`(?i)^(run|execute)\s+`),
	}
)

// heuristicCheck applies deterministic keyword patterns to classify the task.
// Returns definitive=true when the check is confident enough to skip LLM.
func heuristicCheck(task string) (valueobject.TeamEvaluation, bool) {
	normalized := strings.TrimSpace(task)
	if normalized == "" {
		return valueobject.TeamEvaluation{
			NeedsTeam:  false,
			Confidence: 1.0,
			Reasoning:  "empty task",
		}, true
	}

	for _, p := range explicitTeamPatterns {
		if p.MatchString(normalized) {
			return valueobject.TeamEvaluation{
				NeedsTeam:  true,
				Confidence: 1.0,
				Reasoning:  "explicit team request detected",
			}, true
		}
	}

	for _, p := range multiRolePatterns {
		if p.MatchString(normalized) {
			return valueobject.TeamEvaluation{
				NeedsTeam:  true,
				Confidence: 0.8,
				Reasoning:  "multi-perspective or multi-role keywords detected",
			}, true
		}
	}

	for _, p := range simpleTaskIndicators {
		if p.MatchString(normalized) {
			return valueobject.TeamEvaluation{
				NeedsTeam:  false,
				Confidence: 1.0,
				Reasoning:  "simple single-step task",
			}, true
		}
	}

	if len(normalized) < 40 && !strings.Contains(strings.ToLower(normalized), "review") {
		return valueobject.TeamEvaluation{
			NeedsTeam:  false,
			Confidence: 0.7,
			Reasoning:  "short task unlikely to need a team",
		}, true
	}

	return valueobject.TeamEvaluation{}, false
}

const classifierPrompt = `You are a task complexity classifier. Your job is to determine whether a task
requires a multi-disciplinary team of specialists or can be handled by a single agent.

Apply the LLM-as-simulator principle: "What would be a good group of specialists
to solve this problem? What would they say?"

If the task genuinely benefits from multiple expert perspectives, return needs_team: true
with the roles. If a single focused agent can handle it well, return needs_team: false.

IMPORTANT: Most tasks (roughly 70%) do NOT need a team. Only recommend a team when
multiple distinct areas of expertise would produce a meaningfully better result.

Respond with ONLY valid JSON matching this schema:
{
  "needs_team": boolean,
  "confidence": number between 0 and 1,
  "roles": [
    {
      "name": "kebab-case-role-name",
      "perspective": "what this specialist focuses on",
      "instruction": "specific instruction for this specialist"
    }
  ],
  "reasoning": "brief explanation"
}

If needs_team is false, roles should be an empty array.

Task to evaluate:
`

// llmClassify uses a fast model to classify ambiguous tasks.
func (d *HybridTeamDetector) llmClassify(ctx context.Context, task string) (valueobject.TeamEvaluation, error) {
	client, model := d.clientFactory(valueobject.ModelTierFast)

	systemMsg := entity.NewMessage(valueobject.RoleSystem, classifierPrompt)
	userMsg := entity.NewMessage(valueobject.RoleUser, task)

	resp, err := client.Complete(ctx, &output.ChatRequest{
		Model:    model,
		Messages: []entity.Message{systemMsg, userMsg},
	})
	if err != nil {
		return valueobject.TeamEvaluation{}, fmt.Errorf("team classifier LLM call: %w", err)
	}

	content := strings.TrimSpace(resp.Content)
	content = stripCodeFence(content)

	var eval valueobject.TeamEvaluation
	if err := json.Unmarshal([]byte(content), &eval); err != nil {
		return valueobject.TeamEvaluation{
			NeedsTeam:  false,
			Confidence: 0.5,
			Reasoning:  "LLM response was not valid JSON; defaulting to single agent",
		}, nil
	}

	return eval, nil
}

// stripCodeFence removes optional markdown code fences from LLM output.
func stripCodeFence(s string) string {
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

// NoOpTeamDetector always returns NeedsTeam=false. Useful as a default
// when team detection is not configured.
type NoOpTeamDetector struct{}

// Evaluate always returns that no team is needed.
func (NoOpTeamDetector) Evaluate(_ context.Context, _ string) (valueobject.TeamEvaluation, error) {
	return valueobject.TeamEvaluation{
		NeedsTeam:  false,
		Confidence: 1.0,
		Reasoning:  "team detection not configured",
	}, nil
}
