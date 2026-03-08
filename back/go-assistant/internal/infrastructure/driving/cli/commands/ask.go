package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/configs"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/input"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli/home"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli/render"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli/theme"
)

func newAskCmd(h *home.Home, agentFactory func() (input.AgentService, string, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "ask [query]",
		Short: "Send a single query and print the response",
		Long:  "Send a single query to the agent and print the Glamour-rendered response to stdout. Does not start a TUI session.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.Join(args, " ")

			cfg, err := configs.LoadCLIConfig(configPath(h))
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			themeName, _ := cmd.Flags().GetString("theme")
			if !cmd.Flags().Changed("theme") {
				themeName = cfg.Theme
			}
			th, err := theme.Get(themeName)
			if err != nil {
				th, err = theme.Get("dark")
				if err != nil {
					return fmt.Errorf("no theme available: %w", err)
				}
			}

			agentSvc, _, err := agentFactory()
			if err != nil {
				return err
			}

			response, err := agentSvc.Ask(cmd.Context(), query)
			if err != nil {
				return fmt.Errorf("agent query failed: %w", err)
			}

			r, rErr := render.NewRenderer(th.GlamourStyle(), 80)
			if rErr != nil {
				fmt.Fprintln(cmd.OutOrStdout(), response)
				return nil
			}

			rendered, rErr := r.Render(response)
			if rErr != nil {
				fmt.Fprintln(cmd.OutOrStdout(), response)
				return nil
			}

			fmt.Fprint(cmd.OutOrStdout(), rendered)
			return nil
		},
	}
}
