package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/input"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

func newPlanCmd(plannerFactory func() (input.PlannerService, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Manage and execute task plans",
		Long: `plan provides commands for creating, viewing, and executing multi-step task plans.

The planner uses a 3-phase pipeline:
  1. Team Assembly:    Identifies specialist roles needed for the task
  2. Decomposition:    Creates a DAG of micro-tasks informed by expert perspectives
  3. Evaluation:       Scores the plan and refines it if below threshold

Examples:
  liwaisi plan create "research the best Go testing libraries and compare them"
  liwaisi plan create "build a REST API" --execute
  liwaisi plan show
  liwaisi plan list
  liwaisi plan close
  liwaisi plan execute`,
	}

	// Backward compat: if a positional arg is passed without a subcommand,
	// treat it as "plan create --execute <task>".
	cmd.Args = cobra.ArbitraryArgs
	var failFast bool
	cmd.PersistentFlags().BoolVar(&failFast, "fail-fast", false, "Cancel remaining tasks if one fails")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			// Legacy usage: liwaisi plan "task"
			return runPlanCreate(cmd, args, plannerFactory, true, failFast)
		}
		return cmd.Help()
	}

	cmd.AddCommand(
		newPlanCreateCmd(plannerFactory),
		newPlanShowCmd(plannerFactory),
		newPlanCloseCmd(plannerFactory),
		newPlanListCmd(plannerFactory),
		newPlanExecuteCmd(plannerFactory),
	)

	return cmd
}

func newPlanCreateCmd(plannerFactory func() (input.PlannerService, error)) *cobra.Command {
	var execute bool

	cmd := &cobra.Command{
		Use:   "create [task]",
		Short: "Create a new task plan",
		Long: `Create a new plan by assembling a team, decomposing the task, and
evaluating the result. Use --execute to also run the plan immediately.

Examples:
  liwaisi plan create "research the best Go testing libraries"
  liwaisi plan create "build a REST API with auth" --execute`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			failFast, _ := cmd.Flags().GetBool("fail-fast")
			return runPlanCreate(cmd, args, plannerFactory, execute, failFast)
		},
	}

	cmd.Flags().BoolVar(&execute, "execute", false, "Execute the plan immediately after creation")

	return cmd
}

func runPlanCreate(cmd *cobra.Command, args []string, plannerFactory func() (input.PlannerService, error), execute, failFast bool) error {
	task := strings.Join(args, " ")
	sessionID := fmt.Sprintf("plan-%d", time.Now().UnixMilli())

	plannerSvc, err := plannerFactory()
	if err != nil {
		return err
	}

	if execute {
		fmt.Fprintf(cmd.OutOrStdout(), "🧠 Planning and executing: %s\n\n", task)

		var opts []input.PlannerServiceOption
		if failFast {
			opts = append(opts, input.WithFailFast())
		}

		result, err := plannerSvc.PlanAndExecute(cmd.Context(), sessionID, task, opts...)
		if err != nil {
			return fmt.Errorf("plan: %w", err)
		}
		printPlanResult(cmd, result)
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "🧠 Planning: %s\n\n", task)

	graph, err := plannerSvc.Plan(cmd.Context(), sessionID, task)
	if err != nil {
		return fmt.Errorf("plan create: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "✅ Plan created with %d micro-tasks (session: %s)\n\n", len(graph.Tasks), sessionID)
	printPlanGraph(cmd, graph)
	return nil
}

func newPlanShowCmd(plannerFactory func() (input.PlannerService, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "show [session-id]",
		Short: "Display plan details",
		Long:  "Show the details of a plan. If no session-id is given, shows the active plan.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			plannerSvc, err := plannerFactory()
			if err != nil {
				return err
			}

			sessionID := ""
			if len(args) > 0 {
				sessionID = args[0]
			}

			doc, err := plannerSvc.ShowPlan(cmd.Context(), sessionID)
			if err != nil {
				return fmt.Errorf("plan show: %w", err)
			}
			if doc == nil {
				fmt.Fprintln(cmd.OutOrStdout(), "No active plan found.")
				return nil
			}

			fmt.Fprint(cmd.OutOrStdout(), doc.ToMarkdown())
			return nil
		},
	}
}

func newPlanCloseCmd(plannerFactory func() (input.PlannerService, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "close [session-id]",
		Short: "Close a plan",
		Long:  "Close a plan, marking it as finished. If no session-id is given, closes the active plan.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			plannerSvc, err := plannerFactory()
			if err != nil {
				return err
			}

			sessionID := ""
			if len(args) > 0 {
				sessionID = args[0]
			}

			if err := plannerSvc.ClosePlan(cmd.Context(), sessionID); err != nil {
				return fmt.Errorf("plan close: %w", err)
			}

			fmt.Fprintln(cmd.OutOrStdout(), "✅ Plan closed.")
			return nil
		},
	}
}

func newPlanListCmd(plannerFactory func() (input.PlannerService, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all plans",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			plannerSvc, err := plannerFactory()
			if err != nil {
				return err
			}

			docs, err := plannerSvc.ListPlans(cmd.Context())
			if err != nil {
				return fmt.Errorf("plan list: %w", err)
			}

			w := cmd.OutOrStdout()
			if len(docs) == 0 {
				fmt.Fprintln(w, "No plans found.")
				return nil
			}

			fmt.Fprintf(w, "%-25s  %-10s  %-8s  %s\n", "SESSION", "STATUS", "SCORE", "TASK")
			fmt.Fprintf(w, "%s\n", strings.Repeat("─", 90))

			for _, doc := range docs {
				task := doc.Task
				if len(task) > 50 {
					task = task[:47] + "..."
				}
				fmt.Fprintf(w, "%-25s  %-10s  %-8.2f  %s\n",
					doc.SessionID,
					doc.Lifecycle.String(),
					doc.Score,
					task,
				)
			}

			return nil
		},
	}
}

func newPlanExecuteCmd(plannerFactory func() (input.PlannerService, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "execute [session-id]",
		Short: "Execute a persisted plan",
		Long:  "Execute a plan that was previously created. If no session-id is given, executes the active plan.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			plannerSvc, err := plannerFactory()
			if err != nil {
				return err
			}

			sessionID := ""
			if len(args) > 0 {
				sessionID = args[0]
			}

			doc, err := plannerSvc.ShowPlan(cmd.Context(), sessionID)
			if err != nil {
				return fmt.Errorf("plan execute: %w", err)
			}
			if doc == nil {
				return fmt.Errorf("no plan found to execute")
			}
			if doc.Graph == nil || len(doc.Graph.Tasks) == 0 {
				return fmt.Errorf("plan has no tasks to execute")
			}

			fmt.Fprintf(cmd.OutOrStdout(), "🚀 Executing plan: %s (%d tasks)\n\n", doc.SessionID, len(doc.Graph.Tasks))

			var opts []input.PlannerServiceOption
			failFast, _ := cmd.Flags().GetBool("fail-fast")
			if failFast {
				opts = append(opts, input.WithFailFast())
			}

			result, err := plannerSvc.Execute(cmd.Context(), doc.SessionID, doc.Graph, opts...)
			if err != nil {
				return fmt.Errorf("plan execute: %w", err)
			}
			printPlanResult(cmd, result)
			return nil
		},
	}

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

// printPlanGraph displays the task graph in a simple table.
func printPlanGraph(cmd *cobra.Command, graph *entity.PlanGraph) {
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "%-5s  %-20s  %-40s  %s\n", "#", "TASK ID", "DESCRIPTION", "DEPENDS ON")
	fmt.Fprintf(w, "%s\n", strings.Repeat("─", 90))

	for i, t := range graph.Tasks {
		deps := "—"
		if len(t.DependsOn) > 0 {
			deps = strings.Join(t.DependsOn, ", ")
		}
		desc := t.Description
		if len(desc) > 40 {
			desc = desc[:37] + "..."
		}
		fmt.Fprintf(w, "%-5d  %-20s  %-40s  %s\n", i+1, truncate(t.ID, 20), desc, deps)
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
