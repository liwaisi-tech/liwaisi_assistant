package input

import (
	"context"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-liwaisi-os/internal/domain/entity"
)

// HealthService defines the input port for retrieving the service's health status.
type HealthService interface {
	// GetHealth retrieves the current health status of the service and its dependencies.
	GetHealth(ctx context.Context) (*entity.Health, error)
}
