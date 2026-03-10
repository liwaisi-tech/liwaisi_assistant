package handler

import (
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-liwaisi-os/internal/domain/port/input"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-liwaisi-os/internal/domain/valueobject"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-liwaisi-os/pkg/response"
)

// HealthHandler handles HTTP requests for health check endpoints.
type HealthHandler struct {
	healthService input.HealthService
}

// NewHealthHandler creates a new HealthHandler with the given HealthService.
func NewHealthHandler(svc input.HealthService) *HealthHandler {
	return &HealthHandler{
		healthService: svc,
	}
}

// GetHealth handles GET /api/v1/health requests.
func (h *HealthHandler) GetHealth(c echo.Context) error {
	health, err := h.healthService.GetHealth(c.Request().Context())
	if err != nil {
		slog.ErrorContext(c.Request().Context(), "health check failed", "error", err)
		return c.JSON(http.StatusServiceUnavailable, response.HealthResponse{
			Status: "DOWN",
		})
	}

	checks := make(map[string]string, len(health.Checks))
	for k, v := range health.Checks {
		checks[k] = v.String()
	}

	statusCode := http.StatusOK
	if health.Status != valueobject.StatusUp {
		statusCode = http.StatusServiceUnavailable
	}

	return c.JSON(statusCode, response.HealthResponse{
		Status:    health.Status.String(),
		Version:   health.Version,
		Timestamp: health.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
		Checks:    checks,
	})
}
