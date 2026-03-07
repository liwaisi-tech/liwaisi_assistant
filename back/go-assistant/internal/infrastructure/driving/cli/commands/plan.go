package commands

import (
	"context"
	"fmt"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/service"
)

// PlanCommand represents the 'liwaisi plan' subcommand.
type PlanCommand struct {
	plannerService *service.PlannerService
}

func NewPlanCommand(ps *service.PlannerService) *PlanCommand {
	return &PlanCommand{plannerService: ps}
}

func (c *PlanCommand) Execute(ctx context.Context, task string) {
	fmt.Printf("Planning task: %s\n", task)
	result, err := c.plannerService.PlanAndExecute(ctx, task)
	if err != nil {
		fmt.Printf("Error planning: %v\n", err)
		return
	}
	fmt.Printf("Plan executed successfully. Tasks completed: %d\n", len(result.Results))
}
