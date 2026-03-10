package entity

import (
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-liwaisi-os/internal/domain/valueobject"
)

// Health represents the health status of the service at a specific point in time.
type Health struct {
	// Status is the overall health status of the service.
	Status valueobject.Status
	// Version is the current version of the service.
	Version string
	// Timestamp is the exact time the health check was performed.
	Timestamp time.Time
	// Checks contains the individual health status of dependencies (e.g., database).
	Checks map[string]valueobject.Status
}
