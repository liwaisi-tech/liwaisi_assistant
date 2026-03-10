package output

import (
	"context"
)

// HealthRepository defines the output port for checking the health of external dependencies.
type HealthRepository interface {
	// CheckHealth verifies the health of the underlying storage or dependent service.
	// It returns an error if the health check fails.
	CheckHealth(ctx context.Context) error
}
