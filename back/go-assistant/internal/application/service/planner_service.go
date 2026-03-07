package service

import (
	"context"
	"fmt"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/planner"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
)

type PlannerService struct {
	gate       *planner.PlanGate
	decomposer *planner.Decomposer
	scheduler  *entity.Scheduler
}

func NewPlannerService() *PlannerService {
	return &PlannerService{
		gate:       &planner.PlanGate{},
		decomposer: &planner.Decomposer{},
		scheduler:  &entity.Scheduler{},
	}
}

func (s *PlannerService) PlanAndExecute(ctx context.Context, task string) (entity.PlanResult, error) {
	if !s.gate.ShouldPlan(task) {
		return entity.PlanResult{}, fmt.Errorf("simple task, no planning needed")
	}

	graph, err := s.decomposer.Decompose(ctx, task)
	if err != nil {
		return entity.PlanResult{}, err
	}

	return s.scheduler.Execute(ctx, graph)
}
