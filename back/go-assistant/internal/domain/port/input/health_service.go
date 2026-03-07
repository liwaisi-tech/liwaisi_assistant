// Package input defines the driving (input) ports for the go-assistant service.
package input

import (
	"context"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
)

// HealthService defines the driving port for health check operations.
type HealthService interface {
	// GetHealth returns the current health status of the service and its dependencies.
	GetHealth(ctx context.Context) (*entity.Health, error)
}
