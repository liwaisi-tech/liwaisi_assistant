package commands

import (
	"context"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/input"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

const healthTimeout = 5 * time.Second

func newHealthCmd(svc input.HealthService) *cobra.Command {
	return &cobra.Command{
		Use:   "health",
		Short: "Check backend service health",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), healthTimeout)
			defer cancel()

			health, err := svc.GetHealth(ctx)
			if err != nil {
				return fmt.Errorf("health check failed: %w", err)
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(w, "STATUS\t%s\n", health.Status)
			fmt.Fprintf(w, "VERSION\t%s\n", health.Version)
			fmt.Fprintf(w, "TIME\t%s\n", health.Timestamp.Format(time.RFC3339))
			for name, status := range health.Checks {
				symbol := "✓"
				if status == valueobject.StatusDown {
					symbol = "✗"
				}
				fmt.Fprintf(w, "  %s %s\t%s\n", symbol, name, status)
			}
			return w.Flush()
		},
	}
}
