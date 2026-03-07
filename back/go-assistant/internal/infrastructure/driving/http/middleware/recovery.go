package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/labstack/echo/v4"
)

// Recovery returns an Echo middleware that recovers from panics and returns a 500 response.
func Recovery() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			defer func() {
				if r := recover(); r != nil {
					slog.ErrorContext(c.Request().Context(), "panic recovered",
						"error", fmt.Sprintf("%v", r),
						"stack", string(debug.Stack()),
					)
					_ = c.JSON(http.StatusInternalServerError, map[string]string{
						"error": "internal server error",
					})
				}
			}()
			return next(c)
		}
	}
}
