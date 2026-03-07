// Package middleware contains Echo HTTP middleware for the go-assistant service.
package middleware

import (
	"log/slog"
	"time"

	"github.com/labstack/echo/v4"
)

// Logger returns an Echo middleware that logs incoming HTTP requests.
func Logger() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()

			err := next(c)

			// InfoContext propagates the trace/span ID from the request context
			// into log records when the otelslog bridge is active.
			slog.InfoContext(c.Request().Context(), "request",
				"method", c.Request().Method,
				"path", c.Request().URL.Path,
				"remote", c.Request().RemoteAddr,
				"status", c.Response().Status,
				"latency_ms", time.Since(start).Milliseconds(),
			)

			return err
		}
	}
}
