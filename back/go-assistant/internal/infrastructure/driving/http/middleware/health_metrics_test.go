package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func setupTestMeter(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	t.Cleanup(func() {
		_ = mp.Shutdown(context.Background())
		otel.SetMeterProvider(nil)
	})
	return reader
}

func findMetric(rm metricdata.ResourceMetrics, name string) *metricdata.Metrics {
	for _, sm := range rm.ScopeMetrics {
		for i := range sm.Metrics {
			if sm.Metrics[i].Name == name {
				return &sm.Metrics[i]
			}
		}
	}
	return nil
}

func TestHealthMetrics_RecordsOnSuccess(t *testing.T) {
	reader := setupTestMeter(t)

	e := echo.New()
	e.Use(HealthMetrics())
	e.GET("/api/v1/health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "UP"})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("failed to collect metrics: %v", err)
	}

	if m := findMetric(rm, "health_check.total"); m == nil {
		t.Error("health_check.total metric not found")
	}

	if m := findMetric(rm, "health_check.duration"); m == nil {
		t.Error("health_check.duration metric not found")
	}

	if m := findMetric(rm, "health_check.status"); m == nil {
		t.Error("health_check.status metric not found")
	}
}

func TestHealthMetrics_RecordsDownOnServerError(t *testing.T) {
	reader := setupTestMeter(t)

	e := echo.New()
	e.Use(HealthMetrics())
	e.GET("/api/v1/health", func(c echo.Context) error {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"status": "DOWN"})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("failed to collect metrics: %v", err)
	}

	m := findMetric(rm, "health_check.status")
	if m == nil {
		t.Fatal("health_check.status metric not found")
	}

	sum, ok := m.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("expected Sum[int64], got %T", m.Data)
	}

	found := false
	for _, dp := range sum.DataPoints {
		for _, attr := range dp.Attributes.ToSlice() {
			if attr.Key == "status" && attr.Value.AsString() == "DOWN" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected status=DOWN attribute on health_check.status metric")
	}
}
