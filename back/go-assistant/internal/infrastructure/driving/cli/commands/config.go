package commands

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/configs"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli/home"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli/theme"
)

func newConfigCmd(h *home.Home) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "View or modify CLI configuration",
	}

	cmd.AddCommand(
		newConfigShowCmd(h),
		newConfigSetCmd(h),
	)

	return cmd
}

func configPath(h *home.Home) string {
	return filepath.Join(h.Path(home.Config), "liwaisi.yaml")
}

func newConfigShowCmd(h *home.Home) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Display current configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := configs.LoadCLIConfig(configPath(h))
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "theme:        %s\n", cfg.Theme)
			fmt.Fprintf(w, "history_size: %d\n", cfg.HistorySize)
			return nil
		},
	}
}

func newConfigSetCmd(h *home.Home) *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, value := args[0], args[1]

			path := configPath(h)
			cfg, err := configs.LoadCLIConfig(path)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			switch key {
			case "theme":
				if _, err := theme.Get(value); err != nil {
					return fmt.Errorf("invalid theme %q (available: %v)", value, theme.Names())
				}
				cfg.Theme = value
			case "history_size":
				n, err := strconv.Atoi(value)
				if err != nil {
					return fmt.Errorf("invalid value for history_size: %w", err)
				}
				if n <= 0 {
					return fmt.Errorf("history_size must be positive, got %d", n)
				}
				cfg.HistorySize = n
			default:
				return fmt.Errorf("unknown config key: %q (valid keys: theme, history_size)", key)
			}

			if err := configs.SaveCLIConfig(path, cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s = %s\n", key, value)
			return nil
		},
	}
}
