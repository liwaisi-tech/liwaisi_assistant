// Package service contains application-layer use case implementations.
package service

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/input"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/output"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// healthService implements the input.HealthService port.
type healthService struct {
	healthRepo output.HealthRepository
	version    string
	tracer     trace.Tracer
}

// NewHealthService creates a new HealthService with the given HealthRepository and version string.
func NewHealthService(repo output.HealthRepository, version string) input.HealthService {
	return &healthService{
		healthRepo: repo,
		version:    version,
		tracer:     otel.Tracer("go-assistant/health"),
	}
}

// GetHealth returns the current health status of the service and its dependencies.
func (s *healthService) GetHealth(ctx context.Context) (*entity.Health, error) {
	ctx, span := s.tracer.Start(ctx, "healthService.GetHealth")
	defer span.End()

	checks := make(map[string]valueobject.Status)
	overallStatus := valueobject.StatusUp

	if err := s.healthRepo.CheckHealth(ctx); err != nil {
		checks["database"] = valueobject.StatusDown
		overallStatus = valueobject.StatusDown
		span.RecordError(err)
		slog.ErrorContext(ctx, "database health check failed", "error", err)
	} else {
		checks["database"] = valueobject.StatusUp
	}

	span.SetAttributes(attribute.String("health.status", overallStatus.String()))
	slog.InfoContext(ctx, "health check completed",
		"status", overallStatus.String(),
		"version", s.version,
	)

	return &entity.Health{
		Status:    overallStatus,
		Version:   s.version,
		Timestamp: time.Now().UTC(),
		Checks:    checks,
	}, nil
}
