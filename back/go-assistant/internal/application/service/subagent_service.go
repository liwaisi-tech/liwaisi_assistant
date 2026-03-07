package service

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/subagent"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/input"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// compile-time interface verification.
var _ input.SubAgentService = (*subagentService)(nil)

type subagentService struct {
	runner       *subagent.Runner
	router       *subagent.Router
	executor     tool.Executor
	cache        *subagent.Cache
	registry     *subagent.Registry
	detector     subagent.TeamDetector
	subagentsDir string
}

// SubAgentServiceOption configures optional subagentService behavior.
type SubAgentServiceOption func(*subagentService)

// WithSubAgentCache enables TTL-based caching for subagent specs.
func WithSubAgentCache(cache *subagent.Cache) SubAgentServiceOption {
	return func(s *subagentService) {
		s.cache = cache
	}
}

// WithSubAgentRegistry enables persistent SUBAGENT.md file-based storage.
func WithSubAgentRegistry(registry *subagent.Registry) SubAgentServiceOption {
	return func(s *subagentService) {
		s.registry = registry
	}
}

// WithTeamDetector enables team formation evaluation.
func WithTeamDetector(detector subagent.TeamDetector) SubAgentServiceOption {
	return func(s *subagentService) {
		s.detector = detector
	}
}

// WithSubAgentsDir sets the directory for persisting SUBAGENT.md files.
func WithSubAgentsDir(dir string) SubAgentServiceOption {
	return func(s *subagentService) {
		s.subagentsDir = dir
	}
}

// NewSubAgentService creates a SubAgentService backed by the given router,
// runner, and parent executor. Options can add caching, persistent registry,
// and team detection capabilities.
func NewSubAgentService(
	router *subagent.Router,
	runner *subagent.Runner,
	executor tool.Executor,
	opts ...SubAgentServiceOption,
) input.SubAgentService {
	s := &subagentService{
		runner:   runner,
		router:   router,
		executor: executor,
		detector: subagent.NoOpTeamDetector{},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Spawn runs a subagent with the given spec and task. If the spec has
// tool restrictions, a scoped view of the parent executor is created.
func (s *subagentService) Spawn(
	ctx context.Context,
	parentSessionID string,
	spec *entity.SubAgentSpec,
	task string,
) (valueobject.SubAgentResult, error) {
	if err := spec.Validate(); err != nil {
		return valueobject.SubAgentResult{}, fmt.Errorf("subagent spawn: %w", err)
	}

	executor := s.scopedExecutor(spec)

	result := s.runner.Run(ctx, spec, executor, task, parentSessionID)
	return result, nil
}

// SpawnParallel runs multiple subagents concurrently and returns all results.
// Individual failures are captured in each result's Err field; the method
// itself only returns an error for argument validation failures.
func (s *subagentService) SpawnParallel(
	ctx context.Context,
	parentSessionID string,
	specs []*entity.SubAgentSpec,
	tasks []string,
) ([]valueobject.SubAgentResult, error) {
	if len(specs) != len(tasks) {
		return nil, fmt.Errorf("subagent spawn_parallel: specs length (%d) != tasks length (%d)", len(specs), len(tasks))
	}

	results := make([]valueobject.SubAgentResult, len(specs))
	var wg sync.WaitGroup

	for i := range specs {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			spec := specs[idx]
			task := tasks[idx]

			if err := spec.Validate(); err != nil {
				results[idx] = valueobject.SubAgentResult{
					AgentName: spec.Name,
					Err:       fmt.Errorf("subagent spawn: %w", err),
					Status:    valueobject.SubAgentStatusFailed,
				}
				return
			}

			executor := s.scopedExecutor(spec)
			results[idx] = s.runner.Run(ctx, spec, executor, task, parentSessionID)
		}(i)
	}

	wg.Wait()
	return results, nil
}

// ListAgents returns all known subagent specs from the router and persistent
// registry (deduplicated), sorted by name.
func (s *subagentService) ListAgents() []entity.SubAgentSpec {
	seen := make(map[string]struct{})

	routerSpecs := s.router.List()
	result := make([]entity.SubAgentSpec, 0, len(routerSpecs))
	for i := range routerSpecs {
		seen[routerSpecs[i].Name] = struct{}{}
		result = append(result, routerSpecs[i])
	}

	if s.registry != nil {
		registrySpecs := s.registry.List()
		for i := range registrySpecs {
			if _, exists := seen[registrySpecs[i].Name]; !exists {
				seen[registrySpecs[i].Name] = struct{}{}
				result = append(result, registrySpecs[i])
			}
		}
	}

	sortSpecs(result)
	return result
}

// GetAgent returns a subagent spec by name, checking cache -> registry ->
// router in order. Cache hits skip further lookups; registry hits populate
// the cache for subsequent requests.
func (s *subagentService) GetAgent(name string) (entity.SubAgentSpec, bool) {
	if s.cache != nil {
		if spec, ok := s.cache.Get(name); ok {
			return spec, true
		}
	}

	if s.registry != nil {
		if spec, ok := s.registry.Lookup(name); ok {
			if s.cache != nil {
				s.cache.Put(&spec)
			}
			return spec, true
		}
	}

	return s.router.Get(name)
}

// EvaluateTeam determines whether a task requires a multi-disciplinary
// team of subagents.
func (s *subagentService) EvaluateTeam(ctx context.Context, task string) (valueobject.TeamEvaluation, error) {
	return s.detector.Evaluate(ctx, task)
}

// CreateAgent validates and persists a new subagent definition.
func (s *subagentService) CreateAgent(_ context.Context, spec *entity.SubAgentSpec) error {
	if err := spec.Validate(); err != nil {
		return fmt.Errorf("create agent: %w", err)
	}

	if s.registry != nil && s.subagentsDir != "" {
		if err := s.registry.Save(spec, s.subagentsDir); err != nil {
			return fmt.Errorf("create agent: %w", err)
		}
	}

	s.router.Register(spec)

	if s.cache != nil {
		s.cache.Put(spec)
	}

	return nil
}

// scopedExecutor creates a ScopedRegistry when the spec has tool restrictions,
// otherwise returns the parent executor unmodified.
func (s *subagentService) scopedExecutor(spec *entity.SubAgentSpec) tool.Executor {
	if !spec.HasToolRestrictions() {
		return s.executor
	}
	return tool.NewScopedRegistry(s.executor, spec.AllowedTools, spec.DeniedTools)
}

func sortSpecs(specs []entity.SubAgentSpec) {
	sort.Slice(specs, func(i, j int) bool {
		return specs[i].Name < specs[j].Name
	})
}
