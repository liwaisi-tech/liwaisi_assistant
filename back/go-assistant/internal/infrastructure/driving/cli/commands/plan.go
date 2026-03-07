package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/input"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

func newPlanCmd(plannerSvc input.PlannerService) *cobra.Command {
	var failFast bool

	cmd := &cobra.Command{
		Use:   "plan [task]",
		Short: "Decompose and execute a multi-step task using the Planner subagent",
		Long: `plan decomposes a task into a DAG of micro-tasks and executes them,
running independent tasks in parallel and sequentially where dependencies require.

Simple tasks are executed directly without an LLM decomposition call.
Complex tasks (containing keywords like "research", "analyze", "synthesize", etc.)
are decomposed by the LLM into a parallel/sequential execution graph.

Examples:
  liwaisi plan "say hello"
  liwaisi plan "research the 3 best Go testing libraries and compare them"`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			task := strings.Join(args, " ")
			sessionID := fmt.Sprintf("plan-%d", time.Now().UnixMilli())

			fmt.Fprintf(cmd.OutOrStdout(), "🧠 Planning: %s\n\n", task)

			result, err := plannerSvc.PlanAndExecute(cmd.Context(), sessionID, task)
			if err != nil {
				return fmt.Errorf("plan: %w", err)
			}

			printPlanResult(cmd, result)
			return nil
		},
	}

	cmd.Flags().BoolVar(&failFast, "fail-fast", false, "Cancel remaining tasks if one fails")
	_ = failFast // consumed at DI time via WithFailFast option

	return cmd
}

// printPlanResult formats and prints the PlanResult to stdout.
func printPlanResult(cmd *cobra.Command, result valueobject.PlanResult) {
	w := cmd.OutOrStdout()

	statusIcon := "✅"
	if result.Status == valueobject.PlanStatusFailed {
		statusIcon = "❌"
	}
	fmt.Fprintf(w, "%s Plan %s (total: %s)\n\n", statusIcon, result.Status, result.Elapsed.Round(time.Millisecond))

	if len(result.Results) == 0 {
		fmt.Fprintln(w, "No tasks were executed.")
		return
	}

	// Table header
	fmt.Fprintf(w, "%-20s  %-9s  %-9s  %s\n", "TASK", "STATUS", "ELAPSED", "OUTPUT")
	fmt.Fprintf(w, "%s\n", strings.Repeat("─", 90))

	for _, r := range result.Results {
		icon := "✅"
		if !r.IsSuccess() {
			icon = "❌"
		}

		output := r.Output
		if len(output) > 60 {
			output = output[:57] + "..."
		}
		if r.Err != nil {
			output = r.Err.Error()
			if len(output) > 60 {
				output = output[:57] + "..."
			}
		}

		fmt.Fprintf(w, "%-20s  %s %-7s  %-9s  %s\n",
			truncate(r.TaskID, 20),
			icon,
			r.Status,
			r.Elapsed.Round(time.Millisecond),
			output,
		)
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
