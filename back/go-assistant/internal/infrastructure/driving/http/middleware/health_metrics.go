package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// HealthMetrics returns an Echo middleware that records OTel business metrics
// for health check requests: total count, duration histogram, and status counter.
func HealthMetrics() echo.MiddlewareFunc {
	meter := otel.Meter("go-assistant/health")

	checkCounter, err := meter.Int64Counter("health_check.total",
		metric.WithDescription("Total number of health checks performed"))
	if err != nil {
		slog.Warn("failed to create health_check.total counter", "error", err)
	}

	checkDuration, err := meter.Float64Histogram("health_check.duration",
		metric.WithDescription("Duration of health check in milliseconds"),
		metric.WithUnit("ms"))
	if err != nil {
		slog.Warn("failed to create health_check.duration histogram", "error", err)
	}

	checkStatus, err := meter.Int64Counter("health_check.status",
		metric.WithDescription("Health check results by status"))
	if err != nil {
		slog.Warn("failed to create health_check.status counter", "error", err)
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			ctx := c.Request().Context()

			checkCounter.Add(ctx, 1)

			handlerErr := next(c)

			durationMs := float64(time.Since(start).Microseconds()) / 1000.0
			checkDuration.Record(ctx, durationMs)

			status := "UP"
			if c.Response().Status >= http.StatusInternalServerError || handlerErr != nil {
				status = "DOWN"
			}
			checkStatus.Add(ctx, 1, metric.WithAttributes(
				attribute.String("status", status),
			))

			return handlerErr
		}
	}
}
