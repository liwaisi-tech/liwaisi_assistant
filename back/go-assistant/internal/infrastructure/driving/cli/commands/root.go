// Package commands defines the Cobra command tree for the liwaisi CLI.
package commands

import (
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/env"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/memory"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/session"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/input"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/cli/home"
)

// Options holds the shared dependencies injected into subcommands.
type Options struct {
	Home           *home.Home
	HealthSvc      input.HealthService
	AgentFactory   func() (input.AgentService, string, error)
	PlannerFactory func() (input.PlannerService, error)
	Memory         *memory.ConversationMemory
	SessionStore   *session.Store
	EnvStore       *env.Store
	LogLevel       *slog.LevelVar
}

// NewRoot constructs the root cobra.Command with all subcommands attached.
func NewRoot(opts *Options) *cobra.Command {
	root := &cobra.Command{
		Use:   "liwaisi",
		Short: "AI agent assistant for the terminal",
		Long:  "liwaisi is an interactive AI agent assistant that runs directly in your terminal.",
		CompletionOptions: cobra.CompletionOptions{
			HiddenDefaultCmd: true,
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			verbose, _ := cmd.Flags().GetBool("verbose")
			if verbose && opts.LogLevel != nil {
				opts.LogLevel.Set(slog.LevelDebug)
				slog.Debug("verbose logging enabled")
			}
			return nil
		},
	}

	root.PersistentFlags().String("theme", "dark", "color theme (dark, light)")
	root.PersistentFlags().BoolP("verbose", "v", false, "enable verbose output")

	root.AddCommand(
		newVersionCmd(),
		newHealthCmd(opts.HealthSvc),
		newChatCmd(opts.Home, opts.AgentFactory, opts.Memory, opts.SessionStore),
		newAskCmd(opts.Home, opts.AgentFactory),
		newConfigCmd(opts.Home),
		newEnvCmd(opts.EnvStore),
		newPlanCmd(opts.PlannerFactory),
	)

	return root
}

// Execute builds the root command and runs it.
func Execute(opts *Options) error {
	return NewRoot(opts).Execute()
}
