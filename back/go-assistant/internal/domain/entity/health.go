// Package entity contains domain entities for the go-assistant service.
package entity

import (
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// Health represents the overall health status of the service.
type Health struct {
	// Status is the aggregate health status of the service.
	Status valueobject.Status
	// Version is the current version of the service.
	Version string
	// Timestamp is the time when the health check was performed.
	Timestamp time.Time
	// Checks contains the health status of individual components.
	Checks map[string]valueobject.Status
}
