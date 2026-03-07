package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestLogger(t *testing.T) {
	e := echo.New()
	e.Use(Logger())
	e.GET("/test", func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestRecovery(t *testing.T) {
	tests := []struct {
		name         string
		handler      echo.HandlerFunc
		expectedCode int
	}{
		{
			name: "no panic returns normally",
			handler: func(c echo.Context) error {
				return c.String(http.StatusOK, "ok")
			},
			expectedCode: http.StatusOK,
		},
		{
			name: "panic is recovered with 500",
			handler: func(_ echo.Context) error {
				panic("test panic")
			},
			expectedCode: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			e.Use(Recovery())
			e.GET("/test", tt.handler)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			if rec.Code != tt.expectedCode {
				t.Errorf("expected status %d, got %d", tt.expectedCode, rec.Code)
			}
		})
	}
}
