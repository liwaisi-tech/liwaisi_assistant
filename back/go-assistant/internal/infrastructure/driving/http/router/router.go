// Package router configures the Echo HTTP router and middleware for the go-assistant service.
package router

import (
	"time"

	"github.com/labstack/echo/v4"
	echoMW "github.com/labstack/echo/v4/middleware"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/http/handler"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/infrastructure/driving/http/middleware"
)

// New creates and configures a new Echo instance with routes and middleware.
// serviceName is used to identify this service in distributed traces.
func New(healthHandler *handler.HealthHandler, serviceName string) *echo.Echo {
	e := echo.New()
	e.HideBanner = true

	e.Use(otelecho.Middleware(serviceName))
	e.Use(middleware.Recovery())
	e.Use(middleware.Logger())
	e.Use(echoMW.ContextTimeout(5 * time.Second))

	api := e.Group("/api/v1")

	health := api.Group("", middleware.HealthMetrics())
	health.GET("/health", healthHandler.GetHealth)

	return e
}
