package commands

import (
	"fmt"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/env"
)

func newEnvCmd(store *env.Store) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Manage agent environment variables (secrets, API keys)",
	}

	cmd.AddCommand(
		newEnvSetCmd(store),
		newEnvListCmd(store),
		newEnvDeleteCmd(store),
		newEnvPathCmd(store),
	)

	return cmd
}

func newEnvSetCmd(store *env.Store) *cobra.Command {
	return &cobra.Command{
		Use:   "set <KEY>",
		Short: "Set an environment variable with hidden input",
		Long: "Securely set an environment variable. The value is read with hidden " +
			"terminal input (like sudo) so it never appears in shell history.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]

			fmt.Fprintf(cmd.OutOrStdout(), "Enter value for %s: ", key)
			value, err := term.ReadPassword(syscall.Stdin)
			fmt.Fprintln(cmd.OutOrStdout())
			if err != nil {
				return fmt.Errorf("reading input: %w", err)
			}

			trimmed := strings.TrimSpace(string(value))
			if trimmed == "" {
				return fmt.Errorf("value cannot be empty")
			}

			if err := store.Set(cmd.Context(), key, trimmed); err != nil {
				return err
			}

			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "✓ %s saved to %s\n", key, store.FilePath())
			fmt.Fprintf(w, "✓ Variable set in current process environment\n")
			return nil
		},
	}
}

func newEnvListCmd(store *env.Store) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured environment variable names (never values)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			keys, err := store.List(cmd.Context())
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()
			if len(keys) == 0 {
				fmt.Fprintf(w, "No environment variables configured.\n")
				fmt.Fprintf(w, "Set one with: liwaisi env set <KEY>\n")
				return nil
			}

			fmt.Fprintf(w, "Environment variables (from %s):\n", store.FilePath())
			for _, k := range keys {
				fmt.Fprintf(w, "  %-30s [set]\n", k)
			}
			fmt.Fprintf(w, "\n%d variable(s) configured.\n", len(keys))
			return nil
		},
	}
}

func newEnvDeleteCmd(store *env.Store) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <KEY>",
		Short: "Remove an environment variable",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]

			if err := store.Delete(cmd.Context(), key); err != nil {
				return err
			}

			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "✓ %s removed from %s\n", key, store.FilePath())
			fmt.Fprintf(w, "✓ Variable unset from current process environment\n")
			return nil
		},
	}
}

func newEnvPathCmd(store *env.Store) *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Show the env file location",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), store.FilePath())
			return nil
		},
	}
}
