package router

import (
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-liwaisi-os/internal/infrastructure/driving/http/handler"
)

// New initializes and configures the Echo router with necessary middleware and endpoints.
func New(healthHandler *handler.HealthHandler, serviceName string) *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	// Standard Echo middleware
	e.Use(middleware.Recover())
	e.Use(middleware.RequestID())

	// OpenTelemetry integration for HTTP paths
	if serviceName != "" {
		e.Use(otelecho.Middleware(serviceName))
	}

	// API Routing Group
	v1 := e.Group("/api/v1")
	{
		v1.GET("/health", healthHandler.GetHealth)
	}

	return e
}
