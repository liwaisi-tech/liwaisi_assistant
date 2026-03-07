package input

import (
	"context"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// SubAgentService defines the input port for subagent orchestration.
// It handles spawning subagents, listing registered specs, retrieving
// individual specs by name, evaluating team formation, and creating
// new subagent definitions.
type SubAgentService interface {
	// Spawn runs a subagent with the given spec and task, returning the result.
	Spawn(ctx context.Context, parentSessionID string, spec *entity.SubAgentSpec, task string) (valueobject.SubAgentResult, error)

	// SpawnParallel runs multiple subagents concurrently and returns all results.
	// specs and tasks must have the same length. All subagents run to completion;
	// individual failures are captured in each result's Err field.
	SpawnParallel(ctx context.Context, parentSessionID string, specs []*entity.SubAgentSpec, tasks []string) ([]valueobject.SubAgentResult, error)

	// ListAgents returns all registered subagent specs, sorted by name.
	ListAgents() []entity.SubAgentSpec

	// GetAgent returns a registered subagent spec by name. It checks the
	// cache first, then the persistent registry, then pre-registered specs.
	GetAgent(name string) (entity.SubAgentSpec, bool)

	// EvaluateTeam determines whether a task requires a multi-disciplinary
	// team of subagents, using heuristics with LLM fallback.
	EvaluateTeam(ctx context.Context, task string) (valueobject.TeamEvaluation, error)

	// CreateAgent validates and persists a new subagent definition as a
	// SUBAGENT.md file in the subagents directory.
	CreateAgent(ctx context.Context, spec *entity.SubAgentSpec) error
}
