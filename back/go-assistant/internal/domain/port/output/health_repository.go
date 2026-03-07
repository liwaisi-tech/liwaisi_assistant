// Package output defines the driven (output) ports for the go-assistant service.
package output

import "context"

// HealthRepository defines the driven port for checking the health of external dependencies.
type HealthRepository interface {
	// CheckHealth verifies the connectivity and health of the underlying data store.
	CheckHealth(ctx context.Context) error
}
